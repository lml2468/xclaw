// Package router routes inbound messages to per-session handlers with three
// guarantees ported from cc-channel's session-router:
//
//  1. SessionKey derivation — DM is per-peer, group is per-channel (a group is
//     one shared session, not N private ones).
//  2. Per-session serial execution — messages within a session run FIFO; across
//     sessions they run concurrently (a mutex-per-key, the Go analogue of
//     cc-channel's promise-chain lock).
//  3. Rate limiting — three token buckets (global / per-user / per-session),
//     all must pass.
//
// It is agent- and IM-agnostic: the IM layer produces InboundMessage values.
package router

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ChannelType mirrors the IM channel kind. Only the DM/Group distinction matters
// for session-key derivation; concrete IMs map their own enums onto these.
type ChannelType int

const (
	ChannelDM    ChannelType = 1
	ChannelGroup ChannelType = 2
)

// AttachmentKind classifies an inbound attachment for materialization (gateway
// media-download helper). Mirrors the MessageType branches cc-channel-octo's
// inbound.ts feeds into downloadInboundImage / tryResolveFile.
type AttachmentKind int

const (
	// AttachmentImage is a still/animated image (PNG/JPEG/GIF/WebP). The gateway
	// downloads it into the session cwd so the agent's Read tool can open it.
	AttachmentImage AttachmentKind = iota
	// AttachmentFile is a generic file: inlined (small text) or downloaded to a
	// temp path (large/binary), per tryResolveFile semantics.
	AttachmentFile
)

// Attachment is one inbound media/file reference. The IM connector populates it
// from the payload (URL + type) WITHOUT downloading — it has no session cwd. The
// gateway materializes it after the cwd is resolved (see gateway/media.go),
// mirroring cc-channel-octo/src/inbound.ts + media-inbound.ts.
type Attachment struct {
	Kind AttachmentKind
	// URL is the fully-resolved, host-validated absolute download URL.
	URL string
	// Name is the (already-sanitized) display filename, used for the Read hint
	// and the <file_content name="…"> attribute. Empty for images.
	Name string
	// Size is the server-reported byte size when known (0 = unknown). Lets the
	// gateway skip downloading files known to exceed the cap.
	Size int64
}

// InboundMessage is the agent/IM-agnostic unit the router operates on.
type InboundMessage struct {
	FromUID     string
	FromName    string
	ChannelID   string
	ChannelType ChannelType
	SpaceID     string // optional isolation prefix (one bot = one space, usually "")
	Text        string
	// Attachments are inbound media/file references the gateway materializes into
	// the session cwd before driver.Query (downloaded images, inlined text files).
	// The connector fills these from the payload; it does not download.
	Attachments []Attachment
	// MessageSeq is the IM platform's per-channel monotonic message sequence (0
	// for synthetic/cron fires). Used by group-context answered/new segmentation
	// to advance the bot's last-reply cursor; never used for session-key derivation.
	MessageSeq int64
	// Mentioned reports whether the bot was addressed (group gate). DMs ignore it.
	Mentioned bool
	// CronFire marks an operator-scheduled synthetic message that bypasses the
	// mention gate and rate limiting (authenticity is the caller's concern).
	CronFire bool
}

// SessionKey derives the logical session identity. Returns an error for
// unroutable messages (missing identity) — callers treat that as a drop, never
// falling back to "" (which would collapse unrelated peers/channels into one
// shared session and leak history/memory).
func (m InboundMessage) SessionKey() (string, error) {
	switch m.ChannelType {
	case ChannelDM:
		if m.FromUID == "" {
			return "", fmt.Errorf("DM message has no from_uid")
		}
		if m.SpaceID != "" {
			return m.SpaceID + ":" + m.FromUID, nil
		}
		return m.FromUID, nil
	case ChannelGroup:
		if m.ChannelID == "" {
			return "", fmt.Errorf("group message has no channel_id")
		}
		return m.ChannelID, nil
	default:
		return "", fmt.Errorf("unknown channel type %d", m.ChannelType)
	}
}

// Config controls rate limiting and the bot-loop / mention-free gating ported
// from cc-channel-octo's session-router.ts (G12 mention-free groups, G14
// multi-bot loop guard, DM blocklist).
type Config struct {
	MaxPerMinute   int // per-session and per-user bucket size; default 5
	MaxContentByte int // reject longer text; default 32 KiB

	// SelfUID is this bot's own uid. It is treated as a known bot (so a relayed
	// self-message in a mention-free group can't trigger a self-loop) — mirrors
	// session-router.ts seeding knownBotUids with robotId. May be "".
	SelfUID string

	// MentionFreeGroups (G12) lists channel ids where the bot replies WITHOUT an
	// @mention. In those channels, unmentioned group messages are Accepted (still
	// size-gated and rate-limited) instead of DroppedNotMentioned.
	MentionFreeGroups []string

	// KnownBotUids (G14) are uids known to be bots, in addition to the `_bot`
	// suffix heuristic. Messages from bot-looking uids are silently dropped in DMs
	// and in mention-free groups to prevent bot↔bot reply loops.
	KnownBotUids []string

	// AllowedBotUids (G14) whitelists bot-looking uids that should be allowed
	// through the loop guard anyway (trusted bots).
	AllowedBotUids []string

	// BotBlocklist lists uids whose DMs are silently dropped (DM only), mirroring
	// session-router.ts's isBlockedBot DM filter.
	BotBlocklist []string
}

func (c Config) withDefaults() Config {
	if c.MaxPerMinute <= 0 {
		c.MaxPerMinute = 5
	}
	if c.MaxContentByte <= 0 {
		c.MaxContentByte = 32 * 1024
	}
	return c
}

// globalRateMultiplier: the global bucket is N× the per-session size.
const globalRateMultiplier = 10

// Router serializes per-session work and enforces rate limits.
type Router struct {
	cfg Config
	now func() time.Time

	// Precomputed sets from cfg (built once in New) for O(1) gating lookups.
	mentionFree map[string]bool // channel ids that don't require an @mention (G12)
	knownBots   map[string]bool // uids known to be bots (incl. SelfUID) (G14)
	allowedBots map[string]bool // bot-looking uids exempt from the loop guard (G14)
	blocklisted map[string]bool // uids whose DMs are dropped

	locksMu sync.Mutex
	locks   map[string]*lockEntry // one lock per session key (refcounted for eviction)

	rlMu    sync.Mutex
	global  *bucket
	perUser map[string]*bucket
	perSess map[string]*bucket
}

// lockEntry is a per-session serialization lock plus the bookkeeping Reap needs
// to evict it safely: refs counts in-flight holders, lastUse marks when the last
// holder released. An entry is only evictable when refs == 0 (no goroutine is
// blocked on or holding mu) and it has been idle past the reap threshold.
type lockEntry struct {
	mu      sync.Mutex
	refs    int       // in-flight acquirers; guarded by Router.locksMu
	lastUse time.Time // set on release; guarded by Router.locksMu
}

// New constructs a Router.
func New(cfg Config) *Router {
	cfg = cfg.withDefaults()
	knownBots := toSet(cfg.KnownBotUids)
	// session-router.ts seeds knownBotUids with the bot's own robotId.
	if cfg.SelfUID != "" {
		knownBots[cfg.SelfUID] = true
	}
	return &Router{
		cfg:         cfg,
		now:         time.Now,
		mentionFree: toSet(cfg.MentionFreeGroups),
		knownBots:   knownBots,
		allowedBots: toSet(cfg.AllowedBotUids),
		blocklisted: toSet(cfg.BotBlocklist),
		locks:       make(map[string]*lockEntry),
		perUser:     make(map[string]*bucket),
		perSess:     make(map[string]*bucket),
	}
}

// toSet builds a lookup set from a slice, skipping empty entries. Returns a
// non-nil map so reads need no nil check.
func toSet(vals []string) map[string]bool {
	s := make(map[string]bool, len(vals))
	for _, v := range vals {
		if v != "" {
			s[v] = true
		}
	}
	return s
}

// looksLikeBot mirrors session-router.ts looksLikeBot: a uid is bot-looking if
// it's a known bot uid (incl. SelfUID) or ends in the conventional `_bot`
// suffix. Heuristic, not authoritative — but catches the common bot↔bot loop.
func (r *Router) looksLikeBot(uid string) bool {
	if r.knownBots[uid] {
		return true
	}
	return strings.HasSuffix(uid, "_bot")
}

// SetClock overrides the time source (tests).
func (r *Router) SetClock(now func() time.Time) { r.now = now }

// Decision is the outcome of routing a message.
type Decision int

const (
	Accepted            Decision = iota
	DroppedUnroutable            // no session key
	DroppedNotMentioned          // group message without a mention
	DroppedTooLong               // exceeds MaxContentByte
	DroppedBot                   // silent drop: blocklisted DM or bot-loop guard (G14)
	RateLimited
)

func (d Decision) String() string {
	switch d {
	case Accepted:
		return "accepted"
	case DroppedUnroutable:
		return "unroutable"
	case DroppedNotMentioned:
		return "not_mentioned"
	case DroppedTooLong:
		return "too_long"
	case DroppedBot:
		return "bot"
	case RateLimited:
		return "rate_limited"
	default:
		return "unknown"
	}
}

// Handler processes an accepted message under the session lock.
type Handler func(ctx context.Context, sessionKey string, msg InboundMessage) error

// RouteAndHandle applies all gates, then — if accepted — runs handler while
// holding the per-session lock (so same-session messages are strictly
// serialized). Returns the gate decision; handler errors are returned too.
//
// Gate ordering mirrors session-router.ts processMessage: bot blocklist /
// loop-guard drops (silent, DroppedBot) come before the mention gate, which
// itself honours mention-free groups (G12) and a per-room loop guard (G14).
func (r *Router) RouteAndHandle(ctx context.Context, msg InboundMessage, handler Handler) (Decision, error) {
	key, err := msg.SessionKey()
	if err != nil {
		return DroppedUnroutable, nil
	}

	// DM blocklist + bot-loop guard (silent). Mirrors session-router.ts:
	// blocklisted DM senders, and DM senders that look like bots (unless
	// whitelisted) are dropped to prevent bot↔bot reply loops.
	if msg.ChannelType == ChannelDM && !msg.CronFire {
		if r.blocklisted[msg.FromUID] {
			return DroppedBot, nil
		}
		if r.looksLikeBot(msg.FromUID) && !r.allowedBots[msg.FromUID] {
			return DroppedBot, nil
		}
	}

	// Group gating.
	if msg.ChannelType == ChannelGroup && !msg.CronFire {
		// Hard-drop blocklisted senders entirely (even if @-mentioned).
		if r.blocklisted[msg.FromUID] {
			return DroppedBot, nil
		}
		// Mention gate. Skip unless mentioned OR the channel is mention-free (G12).
		if !msg.Mentioned {
			if !r.mentionFree[msg.ChannelID] {
				return DroppedNotMentioned, nil
			}
			// In a mention-free group there is no @mention gate to stop one bot
			// replying to another bot's plain text — drop bot-looking senders
			// (unless whitelisted) so two bots can't enter an unbounded loop.
			if r.looksLikeBot(msg.FromUID) && !r.allowedBots[msg.FromUID] {
				return DroppedBot, nil
			}
		}
	}

	// Content size gate.
	if len(msg.Text) > r.cfg.MaxContentByte {
		return DroppedTooLong, nil
	}

	// Rate limiting (cron fires bypass it — operator-scheduled).
	if !msg.CronFire && !r.allow(key, msg.FromUID) {
		return RateLimited, nil
	}

	// Serialize within the session.
	e := r.acquire(key)
	defer r.release(e)

	if err := handler(ctx, key, msg); err != nil {
		return Accepted, err
	}
	return Accepted, nil
}

// acquire returns the (locked) per-session entry for key, creating it if needed
// and bumping its in-flight refcount so Reap won't evict it mid-turn.
func (r *Router) acquire(key string) *lockEntry {
	r.locksMu.Lock()
	e, ok := r.locks[key]
	if !ok {
		e = &lockEntry{}
		r.locks[key] = e
	}
	e.refs++
	r.locksMu.Unlock()
	e.mu.Lock()
	return e
}

// release unlocks the entry and records the release time, dropping its refcount
// so it becomes eligible for reaping once idle.
func (r *Router) release(e *lockEntry) {
	e.mu.Unlock()
	r.locksMu.Lock()
	e.refs--
	e.lastUse = r.now()
	r.locksMu.Unlock()
}

// Reap evicts per-session locks and rate-limit buckets that have been idle
// longer than idle, bounding the three maps in a long-running daemon. A reaped
// entry is recreated in its initial state on the next turn for that key — which
// is equivalent to its idle state — so eviction never changes behavior. idle
// must exceed the rate-limit window (a bucket idle that long is necessarily full
// again). Returns the number of locks and buckets evicted.
func (r *Router) Reap(idle time.Duration) (locks, buckets int) {
	now := r.now()

	r.locksMu.Lock()
	for k, e := range r.locks {
		if e.refs == 0 && !e.lastUse.IsZero() && now.Sub(e.lastUse) > idle {
			delete(r.locks, k)
			locks++
		}
	}
	r.locksMu.Unlock()

	r.rlMu.Lock()
	for k, b := range r.perUser {
		if b.idleSince(now, idle) {
			delete(r.perUser, k)
			buckets++
		}
	}
	for k, b := range r.perSess {
		if b.idleSince(now, idle) {
			delete(r.perSess, k)
			buckets++
		}
	}
	r.rlMu.Unlock()
	return locks, buckets
}

// allow checks all three buckets; only consumes a token from each if ALL pass
// (peek-then-commit), mirroring cc-channel's checkAllRateLimits.
func (r *Router) allow(sessionKey, uid string) bool {
	r.rlMu.Lock()
	defer r.rlMu.Unlock()
	now := r.now()

	maxPer := r.cfg.MaxPerMinute
	if r.global == nil {
		r.global = newBucket(maxPer*globalRateMultiplier, time.Minute)
	}
	ub := r.perUser[uid]
	if ub == nil {
		ub = newBucket(maxPer, time.Minute)
		r.perUser[uid] = ub
	}
	sb := r.perSess[sessionKey]
	if sb == nil {
		sb = newBucket(maxPer, time.Minute)
		r.perSess[sessionKey] = sb
	}

	if !r.global.peek(now) || !ub.peek(now) || !sb.peek(now) {
		return false
	}
	r.global.take(now)
	ub.take(now)
	sb.take(now)
	// A turn got through — clear the debounce flags so the NEXT time any bucket
	// blocks, the user is notified again (cc-channel checkAllRateLimits clears
	// notified on the all-pass path).
	r.global.notified = false
	ub.notified = false
	sb.notified = false
	return true
}

// ShouldNotifyRateLimit reports whether a "请稍后再试" reply should be sent for a
// message the rate limiter just rejected, debouncing it to once per refill
// window. It marks the blocking bucket as notified (without consuming a token),
// mirroring cc-channel's per-bucket `notified` flag: the first rejection in a
// window returns true; subsequent rejections from the same blocker return false
// until a turn passes (allow clears the flag).
//
// Call this ONLY after allow has returned false for (sessionKey, uid); the
// buckets it inspects are the same ones allow created.
func (r *Router) ShouldNotifyRateLimit(sessionKey, uid string) bool {
	r.rlMu.Lock()
	defer r.rlMu.Unlock()
	now := r.now()

	// Identify the blocking bucket in the same precedence as allow's peek chain
	// (global → user → session). A bucket missing here means it was never
	// created, which can't happen on the post-rejection path, but guard anyway.
	switch {
	case r.global != nil && !r.global.peek(now):
		return markNotified(r.global)
	case r.perUser[uid] != nil && !r.perUser[uid].peek(now):
		return markNotified(r.perUser[uid])
	case r.perSess[sessionKey] != nil && !r.perSess[sessionKey].peek(now):
		return markNotified(r.perSess[sessionKey])
	}
	// No bucket is blocking anymore (refilled between rejection and this call):
	// nothing to notify about.
	return false
}

// markNotified sets the bucket's debounce flag and reports whether this was the
// first rejection of the current window (flag flipped from false to true).
func markNotified(b *bucket) bool {
	if b.notified {
		return false
	}
	b.notified = true
	return true
}
