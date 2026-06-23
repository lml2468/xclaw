package octo

import (
	"context"
	"sync"
	"time"
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

// LearnUser records a uid→name pair observed for free on the inbound
// message stream. Overwrites any previously cached value — the IM server's
// latest BotMessage.FromName is authoritative.
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
	c.mu.Lock()
	e, ok := c.users[uid]
	fresh := ok && (e.name != "" || time.Since(e.fetchedAt) < negativeTTL)
	if fresh {
		c.mu.Unlock()
		return e.name
	}
	key := "u:" + uid
	if _, busy := c.inflight[key]; busy {
		c.mu.Unlock()
		return ""
	}
	c.inflight[key] = struct{}{}
	c.mu.Unlock()
	go c.fetchUser(uid)
	return ""
}

// ResolveChannel returns the cached display name for a group channel id (bare
// "<groupNo>" or thread compound "<groupNo>____<shortId>" — for threads we
// resolve the parent group name). "" if unknown; kicks a background REST
// fetch on miss.
func (c *nameCache) ResolveChannel(channelID string) string {
	if channelID == "" || c.rest == nil {
		return ""
	}
	groupNo := ExtractParentGroupNo(channelID)
	c.mu.Lock()
	e, ok := c.channels[groupNo]
	fresh := ok && (e.name != "" || time.Since(e.fetchedAt) < negativeTTL)
	if fresh {
		c.mu.Unlock()
		return e.name
	}
	key := "c:" + groupNo
	if _, busy := c.inflight[key]; busy {
		c.mu.Unlock()
		return ""
	}
	c.inflight[key] = struct{}{}
	c.mu.Unlock()
	go c.fetchChannel(groupNo)
	return ""
}

func (c *nameCache) fetchUser(uid string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	name := c.rest.GetUserInfo(ctx, uid)
	c.mu.Lock()
	c.users[uid] = nameEntry{name: name, fetchedAt: time.Now()}
	delete(c.inflight, "u:"+uid)
	c.mu.Unlock()
}

func (c *nameCache) fetchChannel(groupNo string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	name := c.rest.GetGroupInfo(ctx, groupNo)
	c.mu.Lock()
	c.channels[groupNo] = nameEntry{name: name, fetchedAt: time.Now()}
	delete(c.inflight, "c:"+groupNo)
	c.mu.Unlock()
}
