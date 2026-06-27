package octo

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestRESTGetUserInfo asserts the happy path: a 200 with {"name":"…"} returns
// the name; the request goes to the right URL with the uid query-escaped.
func TestRESTGetUserInfo(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		_, _ = w.Write([]byte(`{"uid":"u1","name":"Alice"}`))
	}))
	defer srv.Close()

	c := NewRESTClient(srv.URL, func() string { return "tk" })
	if got, err := c.GetUserInfo(t.Context(), "u1"); err != nil || got != "Alice" {
		t.Fatalf("GetUserInfo = %q, %v, want %q, nil", got, err, "Alice")
	}
	if want := "/v1/bot/user/info?uid=u1"; gotPath != want {
		t.Fatalf("path = %q, want %q", gotPath, want)
	}
}

// TestNameCacheResolvedHookFiresOnLazyFetch proves the resolved hook fires when
// a background fetch lands a non-empty name — the signal the daemon uses to
// re-broadcast session.upserted so a sidebar row that first painted with the
// bare id updates to the resolved name without waiting for a turn. The hook
// must carry the kind (user vs channel), the key, and the resolved name.
func TestNameCacheResolvedHookFiresOnLazyFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"uid":"u1","name":"Alice"}`))
	}))
	defer srv.Close()
	nc := newNameCache(NewRESTClient(srv.URL, func() string { return "tk" }))

	type ev struct {
		kind NameKind
		key  string
		name string
	}
	got := make(chan ev, 4)
	nc.SetResolvedHook(func(kind NameKind, key, name string) { got <- ev{kind, key, name} })

	// Cold miss returns "" and kicks the background fetch.
	if v := nc.ResolveUser("u1"); v != "" {
		t.Fatalf("first ResolveUser = %q, want empty (lazy fetch)", v)
	}
	select {
	case e := <-got:
		if e.kind != NameKindUser || e.key != "u1" || e.name != "Alice" {
			t.Fatalf("hook fired with %+v, want {NameKindUser u1 Alice}", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resolved hook never fired after lazy fetch")
	}
}

// TestNameCacheResolvedHookSkipsKnownAndEmpty proves the hook does NOT fire when
// the name didn't change: a LearnUser-seeded value re-confirmed by a fetch (no
// new info), and a 404→"" result (which must not clobber a row back to its id
// nor spam a broadcast). Notifying only on a real change keeps a sessions.list
// prewarm burst from re-broadcasting rows whose names were already known.
func TestNameCacheResolvedHookSkipsKnownAndEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound) // empty-name result
	}))
	defer srv.Close()
	nc := newNameCache(NewRESTClient(srv.URL, func() string { return "tk" }))

	var fired int32
	nc.SetResolvedHook(func(NameKind, string, string) { atomic.AddInt32(&fired, 1) })

	// A 404 fetch lands "" — must not fire the hook.
	nc.fetchUser("ghost")
	// A name already known via free-feed, re-stored with the same value — no change.
	nc.LearnUser("u2", "Bob")
	nc.storeName(NameKindUser, "u2", nc.users, "u:u2", "Bob", false)

	if n := atomic.LoadInt32(&fired); n != 0 {
		t.Fatalf("hook fired %d times for empty/unchanged names, want 0", n)
	}
}

// TestRESTGetUserInfo404 proves the documented 404→"" soft-degrade — a missing
// uid is a normal not-found, not an error to log.
func TestRESTGetUserInfo404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := NewRESTClient(srv.URL, func() string { return "tk" })
	// 404 is a genuine "no such uid": ("", nil), NOT a transient error — so the
	// caller negative-caches it for negativeTTL rather than the short errorTTL.
	if got, err := c.GetUserInfo(t.Context(), "u1"); got != "" || err != nil {
		t.Fatalf("GetUserInfo on 404 = %q, %v, want \"\", nil", got, err)
	}
}

// TestRESTGetGroupInfo asserts the happy path for groups.
func TestRESTGetGroupInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"group_no":"g1","name":"Engineering"}`))
	}))
	defer srv.Close()
	c := NewRESTClient(srv.URL, func() string { return "tk" })
	if got, err := c.GetGroupInfo(t.Context(), "g1"); err != nil || got != "Engineering" {
		t.Fatalf("GetGroupInfo = %q, %v, want %q, nil", got, err, "Engineering")
	}
}

// TestNameCacheLearnUserNoRESTCall asserts the free-feed contract: LearnUser
// seeds the cache directly so ResolveUser never issues a request. A test
// server that fails the test on any hit proves the inbound message stream
// short-circuits the lookup for the common case (sender names already arrive
// on every BotMessage).
func TestNameCacheLearnUserNoRESTCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected REST call: %s", r.URL.Path)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	nc := newNameCache(NewRESTClient(srv.URL, func() string { return "tk" }))
	nc.LearnUser("u1", "Alice")
	if got := nc.ResolveUser("u1"); got != "Alice" {
		t.Fatalf("ResolveUser after LearnUser = %q, want %q", got, "Alice")
	}
}

// TestNameCacheResolveChannelLazyFetch proves the miss-then-cache shape: the
// first ResolveChannel returns "" but kicks a REST fetch; once that completes,
// the cache holds the resolved name and the second call returns it without
// another request.
func TestNameCacheResolveChannelLazyFetch(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`{"group_no":"g1","name":"Eng"}`))
	}))
	defer srv.Close()
	nc := newNameCache(NewRESTClient(srv.URL, func() string { return "tk" }))

	if got := nc.ResolveChannel("g1"); got != "" {
		t.Fatalf("first ResolveChannel = %q, want empty (lazy fetch)", got)
	}
	// Wait for the background fetch to populate the cache. The fetch runs in a
	// goroutine with its own ctx; poll with a tight cap rather than sleep.
	deadline := time.Now().Add(2 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		if got = nc.ResolveChannel("g1"); got == "Eng" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got != "Eng" {
		t.Fatalf("ResolveChannel never resolved within deadline: %q", got)
	}
	// One more call should NOT re-hit the server — the entry is positive-cached
	// indefinitely. We may have raced a few extra calls during the poll loop;
	// the strict assertion is "≥1 hit", and the prior assertions prove caching
	// occurred (otherwise we'd loop until deadline with hits>>1).
	if atomic.LoadInt32(&hits) < 1 {
		t.Fatalf("expected ≥1 REST hit, got %d", hits)
	}
	before := atomic.LoadInt32(&hits)
	for i := 0; i < 5; i++ {
		nc.ResolveChannel("g1")
	}
	if got := atomic.LoadInt32(&hits); got != before {
		t.Fatalf("post-cache ResolveChannel issued extra REST calls: before=%d after=%d", before, got)
	}
}

// TestNameCacheResolveChannelThreadCompound proves a thread channel id
// "<groupNo>____<shortId>" fetches the THREAD's own name via the threads
// endpoint, AND simultaneously warms the parent group so a downstream
// composition (sidebar / chat-header) has both halves.
func TestNameCacheResolveChannelThreadCompound(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/v1/bot/groups/g1/threads/topic9":
			_, _ = w.Write([]byte(`{"name":"Bug Triage"}`))
		case "/v1/bot/groups/g1":
			_, _ = w.Write([]byte(`{"name":"Engineering"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	nc := newNameCache(NewRESTClient(srv.URL, func() string { return "tk" }))
	nc.ResolveChannel("g1" + ThreadIDSeparator + "topic9")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		thread := hits["/v1/bot/groups/g1/threads/topic9"]
		parent := hits["/v1/bot/groups/g1"]
		mu.Unlock()
		if thread > 0 && parent > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits["/v1/bot/groups/g1/threads/topic9"] != 1 {
		t.Fatalf("thread endpoint hit count = %d, want 1", hits["/v1/bot/groups/g1/threads/topic9"])
	}
	if hits["/v1/bot/groups/g1"] != 1 {
		t.Fatalf("parent group endpoint hit count = %d, want 1 (parent warmed in parallel)", hits["/v1/bot/groups/g1"])
	}
}

// TestNameCacheNegativeTTLReFetch proves a 404 (cached as empty name) re-fetches
// after the negative TTL elapses — a group that gets a name later eventually
// shows up without a daemon restart. We shrink negativeTTL for the test.
func TestNameCacheNegativeTTLReFetch(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	nc := newNameCache(NewRESTClient(srv.URL, func() string { return "tk" }))

	// Force the first fetch (and wait for it).
	nc.ResolveChannel("g1")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&hits) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if hits := atomic.LoadInt32(&hits); hits == 0 {
		t.Fatal("first fetch never occurred")
	}
	// Backdate the cached negative entry past the TTL so the next call re-fetches.
	nc.mu.Lock()
	e := nc.channels["g1"]
	e.fetchedAt = time.Now().Add(-2 * negativeTTL)
	nc.channels["g1"] = e
	nc.mu.Unlock()

	before := atomic.LoadInt32(&hits)
	nc.ResolveChannel("g1") // should kick a re-fetch
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&hits) == before {
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&hits); got <= before {
		t.Fatalf("expired negative entry did not re-fetch: hits stayed at %d", got)
	}
}
