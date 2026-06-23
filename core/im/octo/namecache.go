package octo

import (
	"context"
	"sync"
	"time"
)

// NameKind distinguishes the two id namespaces the name cache resolves: a DM
// peer uid vs a group / thread channel id. It rides the resolved hook so the
// daemon can map a freshly-resolved name back to the right kind of session.
type NameKind int

const (
	NameKindUser    NameKind = iota // a DM peer uid
	NameKindChannel                 // a group / thread channel id
)

// nameCache resolves uid→displayName and groupNo→channelName for the desktop
// sidebar / chat-bubble UX. Sender names arrive on every inbound message
// (BotMessage.FromName) and are seeded into the cache for free via LearnUser;
// channel names require a REST round-trip the first time a channel is seen,
// gated by a per-key singleflight so a burst doesn't multiply requests.
//
// Reads are non-blocking: ResolveUser/ResolveChannel always return ("", false)
// when the key isn't cached AND kick off a background REST fetch. The first
// caller (e.g. sessions.list at app start) sees IDs; subsequent calls see the
// resolved names. This shape avoids serializing the sessions.list response on
// N REST hops, while still converging within ~1s of the first listing.
//
// Negative results (404 / empty name) cache too, with a short TTL so a name
// that gets assigned later eventually shows up without a daemon restart.
type nameCache struct {
	rest *RESTClient

	mu       sync.Mutex
	users    map[string]nameEntry
	channels map[string]nameEntry
	inflight map[string]struct{} // dedup across goroutines; key = "u:"+uid or "c:"+groupNo
	// onResolved, when set, fires after a background/prewarm fetch lands a
	// non-empty name that newly differs from the cached value. Lets the daemon
	// re-broadcast session.upserted so a sidebar row that first painted with the
	// bare id picks up the resolved name without waiting for the next turn.
	onResolved func(NameKind, string, string)
}

type nameEntry struct {
	name      string
	fetchedAt time.Time
}

// negativeTTL bounds how long an empty result is cached. Short enough that
// renaming a group / setting a missing display name shows up within minutes
// without a daemon restart; long enough to absorb a session.list burst.
const negativeTTL = 5 * time.Minute

func newNameCache(rest *RESTClient) *nameCache {
	return &nameCache{
		rest:     rest,
		users:    map[string]nameEntry{},
		channels: map[string]nameEntry{},
		inflight: map[string]struct{}{},
	}
}

// SetResolvedHook registers the callback fired when a fetch resolves a name to
// a new non-empty value. Set once during bot setup, before Connect, so reads on
// the fetch goroutines never race a concurrent write.
func (c *nameCache) SetResolvedHook(fn func(NameKind, string, string)) {
	c.mu.Lock()
	c.onResolved = fn
	c.mu.Unlock()
}

// storeName records a freshly-fetched name under key, clears its inflight
// marker, and — when the name is non-empty and newly differs from the prior
// cached value — fires the resolved hook. Notifying only on a real change keeps
// a session-list prewarm burst from re-broadcasting rows whose names were
// already known (and a "" fetch result from clobbering a row back to its id).
// Shared by every fetch path (fetchUser/fetchGroup/fetchThread + prewarm) so
// the notify semantics live in exactly one place.
func (c *nameCache) storeName(kind NameKind, key string, bucket map[string]nameEntry, inflightKey, name string) {
	c.mu.Lock()
	prev := bucket[key].name
	bucket[key] = nameEntry{name: name, fetchedAt: time.Now()}
	delete(c.inflight, inflightKey)
	hook := c.onResolved
	c.mu.Unlock()
	if hook != nil && name != "" && name != prev {
		hook(kind, key, name)
	}
}

// LearnUser records a uid→name pair observed for free on the inbound
// message stream. Overwrites any previously cached value — the IM server's
// latest known display name is authoritative.
func (c *nameCache) LearnUser(uid, name string) {
	if uid == "" || name == "" {
		return
	}
	c.mu.Lock()
	c.users[uid] = nameEntry{name: name, fetchedAt: time.Now()}
	c.mu.Unlock()
}

// ResolveUser returns the cached display name for uid, or "" if unknown. A
// miss triggers a background REST fetch so a subsequent call can see the
// resolved value. Stale-negative entries (empty name past TTL) re-fetch.
func (c *nameCache) ResolveUser(uid string) string {
	if uid == "" || c.rest == nil {
		return ""
	}
	return c.resolveCachedOrKick(uid, c.users, "u:", c.fetchUser)
}

// ResolveChannel returns the cached display name for a channel id. For a bare
// "<groupNo>" it's the group's name; for a thread compound
// "<groupNo>____<shortId>" it's the THREAD's own name (the parent group's
// name is a separate ResolveChannel call on the parent id, composed at the
// projection layer). "" if unknown; kicks a background REST fetch on miss.
// For a thread miss the parent group is also kicked, so the projection
// layer's composition has both names by the next call.
func (c *nameCache) ResolveChannel(channelID string) string {
	if channelID == "" || c.rest == nil {
		return ""
	}
	if IsThreadChannelID(channelID) {
		// Warm the parent in parallel so a "ThreadName(GroupName)" composition
		// has both halves available — the parent fetch is independent and
		// short-circuits if already cached.
		c.kickIfMissing(ExtractParentGroupNo(channelID), c.channels, "c:", c.fetchGroup)
	}
	return c.resolveCachedOrKick(channelID, c.channels, "c:", c.dispatchChannelFetch(channelID))
}

// dispatchChannelFetch picks the right REST call for a given channel id:
// GetThreadInfo for compounds, GetGroupInfo for bare groups. Returned as a
// closure so kickIfMissing / resolveCachedOrKick don't need to branch.
func (c *nameCache) dispatchChannelFetch(channelID string) func(string) {
	if IsThreadChannelID(channelID) {
		return c.fetchThread
	}
	return c.fetchGroup
}

// resolveCachedOrKick checks the bucket for a fresh entry and returns its
// name; on miss it kicks the fetch via kickIfMissing and returns "".
func (c *nameCache) resolveCachedOrKick(
	key string,
	bucket map[string]nameEntry,
	prefix string,
	fetch func(string),
) string {
	c.mu.Lock()
	e, ok := bucket[key]
	fresh := ok && (e.name != "" || time.Since(e.fetchedAt) < negativeTTL)
	c.mu.Unlock()
	if fresh {
		return e.name
	}
	c.kickIfMissing(key, bucket, prefix, fetch)
	return ""
}

// kickIfMissing fires the given fetch on a background goroutine unless
// (a) the bucket already has a fresh entry or (b) another fetch is in
// flight for this key. Returns immediately; the goroutine populates the
// cache when it finishes.
func (c *nameCache) kickIfMissing(
	key string,
	bucket map[string]nameEntry,
	prefix string,
	fetch func(string),
) {
	c.mu.Lock()
	if e, ok := bucket[key]; ok {
		if e.name != "" || time.Since(e.fetchedAt) < negativeTTL {
			c.mu.Unlock()
			return
		}
	}
	if _, busy := c.inflight[prefix+key]; busy {
		c.mu.Unlock()
		return
	}
	c.inflight[prefix+key] = struct{}{}
	c.mu.Unlock()
	go fetch(key)
}

func (c *nameCache) fetchUser(uid string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c.storeName(NameKindUser, uid, c.users, "u:"+uid, c.rest.GetUserInfo(ctx, uid))
}

func (c *nameCache) fetchGroup(groupNo string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c.storeName(NameKindChannel, groupNo, c.channels, "c:"+groupNo, c.rest.GetGroupInfo(ctx, groupNo))
}

func (c *nameCache) fetchThread(channelID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	name := c.rest.GetThreadInfo(ctx, ExtractParentGroupNo(channelID), extractThreadShortID(channelID))
	c.storeName(NameKindChannel, channelID, c.channels, "c:"+channelID, name)
}

// PrewarmChannels synchronously resolves channel names for the given channel
// ids, capped by timeout. Used by sessions.list to dodge the cold-start
// "every row shows its bare id" first paint: ChannelName by itself returns
// "" on miss and kicks a background fetch the NEXT call sees populated —
// but the GUI only re-issues sessions.list when the bot is switched, so
// without prewarm names never appear on the first listing.
//
// Each id is fetched via the right endpoint per shape (GetGroupInfo for bare
// groups, GetThreadInfo for thread compounds). Thread ids ALSO warm their
// parent group_no so the "<ThreadName>(<GroupName>)" composition that
// projection layers do has both halves available at the same time.
//
// DM peer names are warmed by the sister PrewarmUsers — they need it too
// for any session that's had no inbound this restart (LearnUser only
// seeds from live inbound messages).
func (c *nameCache) PrewarmChannels(channelIDs []string, timeout time.Duration) {
	var groups, threads []string
	parents := map[string]struct{}{}
	for _, ch := range channelIDs {
		if ch == "" {
			continue
		}
		if IsThreadChannelID(ch) {
			threads = append(threads, ch)
			parents[ExtractParentGroupNo(ch)] = struct{}{}
		} else {
			groups = append(groups, ch)
		}
	}
	for p := range parents {
		groups = append(groups, p)
	}
	// One shared timeout — both phases run in parallel since they hit
	// independent endpoints and share the prewarmConcurrency pool.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		c.prewarm(groups, timeout, "c:", NameKindChannel, identity, c.channels, c.rest.GetGroupInfo)
	}()
	go func() {
		defer wg.Done()
		c.prewarm(threads, timeout, "c:", NameKindChannel, identity, c.channels, func(ctx context.Context, id string) string {
			return c.rest.GetThreadInfo(ctx, ExtractParentGroupNo(id), extractThreadShortID(id))
		})
	}()
	wg.Wait()
}

func identity(s string) string { return s }

// PrewarmUsers is the user-side sister of PrewarmChannels — synchronously
// fetches display names for any uids not already in cache. Sessions.list
// uses it so DM sidebar rows show the peer's name on first paint even when
// the session has had no inbound this restart to free-feed the cache.
func (c *nameCache) PrewarmUsers(uids []string, timeout time.Duration) {
	c.prewarm(uids, timeout, "u:", NameKindUser, identity, c.users, c.rest.GetUserInfo)
}

// prewarm is the shared body for PrewarmChannels/PrewarmUsers: dedup against
// the cache and the inflight set, then fire parallel fetches and wait up to
// `timeout` for them. The wait is decoupled from each fetch's deadline: the
// fetches use their OWN longer ctx (prewarmFetchTimeout) so a slow API that
// would have succeeded in 3 s isn't punished by the caller's 1.5 s wait
// budget — under the old shared-ctx shape every still-running goroutine saw
// the cancelled ctx, returned "", and poisoned the negative-cache for the
// full negativeTTL, sticking the sidebar at bare ids for minutes.
//
// Fetches run with at most prewarmConcurrency in flight at once so a session
// list with N rows doesn't fanout N parallel REST calls — a 50-group bot was
// otherwise firing 50 concurrent requests at the name service. Excess fetches
// queue on a buffered channel and run as slots free up.
func (c *nameCache) prewarm(
	keys []string,
	timeout time.Duration,
	prefix string,
	kind NameKind,
	normalize func(string) string,
	bucket map[string]nameEntry,
	fetch func(context.Context, string) string,
) {
	if len(keys) == 0 || c.rest == nil {
		return
	}
	want := make(map[string]struct{}, len(keys))
	c.mu.Lock()
	for _, k := range keys {
		if k == "" {
			continue
		}
		nk := normalize(k)
		if _, busy := c.inflight[prefix+nk]; busy {
			continue
		}
		if e, ok := bucket[nk]; ok {
			if e.name != "" || time.Since(e.fetchedAt) < negativeTTL {
				continue
			}
		}
		want[nk] = struct{}{}
		c.inflight[prefix+nk] = struct{}{}
	}
	c.mu.Unlock()
	if len(want) == 0 {
		return
	}

	done := make(chan struct{})
	sem := make(chan struct{}, prewarmConcurrency)
	var wg sync.WaitGroup
	for nk := range want {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// Fetch ctx is INDEPENDENT of the caller's wait budget — the
			// caller blocks on `done` for at most `timeout` and walks away,
			// but the fetch keeps running on its own deadline and the
			// result lands in the cache for the next caller.
			ctx, cancel := context.WithTimeout(context.Background(), prewarmFetchTimeout)
			defer cancel()
			c.storeName(kind, k, bucket, prefix+k, fetch(ctx, k))
		}(nk)
	}
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// prewarmConcurrency caps the in-flight prewarm fetches per nameCache. Picked
// to be enough to populate a typical session-list quickly without fanning out
// hundreds of REST calls at once on a bot with many groups.
const prewarmConcurrency = 8

// prewarmFetchTimeout bounds each individual REST call kicked by prewarm.
// Larger than the caller's wait budget by design (see prewarm doc): the
// caller walks away on timeout, the fetch keeps running and seeds the cache.
const prewarmFetchTimeout = 5 * time.Second
