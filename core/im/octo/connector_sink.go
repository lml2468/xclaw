package octo

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/lml2468/octobuddy/core/agent"
	"github.com/lml2468/octobuddy/core/router"
)

// --- gateway.Sink ---

// OnEvent drives the per-turn typing heartbeat and the optional tool-progress
// mirror. On the first activity (KindSessionStarted) it resets the per-turn
// tool-progress state and starts a 5s typing heartbeat (cc-channel-octo
// stream-relay.ts) — without this a long turn lets the indicator expire and the
// user thinks the bot died. KindToolUse mirrors a "🔧 Running <tool>(<params>)…"
// notice when tool-progress is on. KindTurnDone and a terminal (non-recoverable)
// KindError stop the heartbeat and clear the progress state, so a turn that
// errors out without a reply still cleans up. A recoverable KindError is a
// mid-turn warning (e.g. a stderr line in claude.go) and must NOT stop it.
func (c *Connector) OnEvent(sessionKey string, ev agent.AgentEvent) {
	switch {
	case ev.Kind == agent.KindSessionStarted:
		c.mu.Lock()
		if c.toolProgress {
			c.progress[sessionKey] = &toolProgressState{}
		}
		c.mu.Unlock()
		c.startTyping(sessionKey)
	case ev.Kind == agent.KindToolUse:
		c.maybeSendToolNotice(sessionKey, ev)
	case ev.Kind == agent.KindTurnDone, ev.Kind == agent.KindError && !ev.Recoverable:
		c.stopTyping(sessionKey)
		c.mu.Lock()
		delete(c.progress, sessionKey)
		c.mu.Unlock()
	}
}

// maybeSendToolNotice emits a "🔧 Running <tool>(<params>)…" notice for a
// KindToolUse event when tool-progress is on, collapsing consecutive identical
// notices and capping the count per turn. The dedup/cap decision is made under
// c.mu; the REST send happens after unlocking so a slow send never holds the
// connector lock (and never blocks the agent stream's other sessions).
func (c *Connector) maybeSendToolNotice(sessionKey string, ev agent.AgentEvent) {
	label := ev.ToolName
	if label == "" {
		return
	}
	if ev.ToolParams != "" {
		// ToolParams is already a whitespace-collapsed one-liner truncated to 120
		// chars by claude.go's truncateParams — mirrors MAX_TOOL_PARAM_CHARS.
		label += "(" + ev.ToolParams + ")"
	}

	c.mu.Lock()
	if !c.toolProgress {
		c.mu.Unlock()
		return
	}
	st := c.progress[sessionKey]
	if st == nil {
		// No KindSessionStarted seen for this session this turn — start fresh.
		st = &toolProgressState{}
		c.progress[sessionKey] = st
	}
	if label == st.lastNotice {
		c.mu.Unlock()
		return // collapse exact consecutive repeats
	}
	st.lastNotice = label
	if st.count >= maxToolNotices {
		c.mu.Unlock()
		return // capped — stay quiet for the rest of the turn
	}
	st.count++
	c.mu.Unlock()

	tgt, ok := c.target(sessionKey)
	if !ok {
		return
	}
	if _, err := c.rest.SendText(c.ctx(), tgt.channelID, tgt.channelType, "🔧 Running "+label+"…", nil, nil, false); err != nil {
		c.logf("send tool-progress for %s: %v", sessionKey, err)
	}
}

// OnReply delivers the assembled assistant reply back to the originating
// channel. It resolves @mentions (structured @[uid:name] + plain @name +
// @all/@所有人) ONCE over the full text — so splitting can never break a mention
// across segments — then splits into <=3500-UTF-16-unit segments, rebasing each
// entity's offset to segment-local before sending (api/stream-relay parity). For
// a persona clone replying as the grantor, each send carries on_behalf_of so the
// server presents it as the grantor (openclaw OBO). It also stops the typing
// heartbeat — the end-of-turn cleanup point (stream-relay.ts deliver finally).
//
// OnUserMessage is a no-op for the Octo connector: the inbound message arrived
// HERE in the first place (onInbound → enqueueTurn → gateway → runTurn → back
// to this sink). Re-sending it to the IM would echo the user's own message
// back to them. The control-bus EventSink is the one that actually surfaces
// user_message events to the GUI; this stub keeps the Sink interface honest.
func (c *Connector) OnUserMessage(string, router.InboundMessage) {}

// Empty reply → a no-response placeholder is sent instead of silently dropping
// the turn (cc-channel-octo index.ts behavior).
func (c *Connector) OnReply(sessionKey string, text string) {
	c.stopTyping(sessionKey)
	// The reply target is only needed through this turn's delivery; drop it
	// afterwards so the map doesn't accumulate one entry per distinct session
	// forever. The next inbound (or cron fire) re-registers it, and the router
	// serializes turns per session so nothing races this delete.
	defer func() { c.mu.Lock(); delete(c.targets, sessionKey); c.mu.Unlock() }()
	text = strings.TrimSpace(text)
	tgt, ok := c.target(sessionKey)
	if !ok {
		return
	}
	if text == "" {
		// No output from the agent: deliver a placeholder so the user isn't left
		// hanging. No mentions on a fixed system string.
		if _, err := c.rest.SendTextAs(c.ctx(), tgt.channelID, tgt.channelType, noResponseFallback, nil, nil, false, tgt.onBehalfOf); err != nil {
			c.logf("send no-response fallback to %s: %v", sessionKey, err)
		}
		return
	}

	// Resolve mentions against the channel roster. Plain @name resolution and the
	// member-validity downgrade only apply to group channels (DMs have no
	// roster); for DMs memberMap is nil and structured uids are trusted
	// (isValidUid=nil), matching cc-channel-octo's "omit memberMap/isValidUid in
	// DMs" path.
	var memberMap map[string]string
	var isValidUid func(string) bool
	if tgt.channelType == ChannelGroup && c.gateway != nil {
		memberMap = c.gateway.MemberMap(tgt.channelID)
		channelID := tgt.channelID
		isValidUid = func(uid string) bool { return c.gateway.IsMember(channelID, uid) }
	}
	res := resolveMentions(text, memberMap, isValidUid)

	// Protect each resolved @name span so splitMessageProtected won't cut through it.
	ranges := make([]protectedRange, 0, len(res.mentionEntries))
	for _, e := range res.mentionEntries {
		ranges = append(ranges, protectedRange{start: e.Offset, end: e.Offset + e.Length})
	}

	mentionAllConsumed := false
	for _, seg := range splitMessageProtected(res.finalContent, 3500, ranges) {
		segStart := seg.start
		segEnd := segStart + utf16Len(seg.text)
		// Entities fully inside this segment, rebased to segment-local offsets.
		var segEntities []MentionEntity
		var segUids []string
		for _, e := range res.mentionEntries {
			if e.Offset >= segStart && e.Offset+e.Length <= segEnd {
				segEntities = append(segEntities, MentionEntity{UID: e.UID, Offset: e.Offset - segStart, Length: e.Length})
				segUids = append(segUids, e.UID)
			}
		}
		// mentionAll applies to the FIRST segment only (stream-relay parity:
		// avoids re-broadcasting @所有人 on every segment of a long reply).
		useMentionAll := res.mentionAll && !mentionAllConsumed
		if useMentionAll {
			mentionAllConsumed = true
		}
		// SendTextAs carries on_behalf_of for a persona clone (empty otherwise).
		// A failed segment send means the user never receives that part of the
		// reply, so retry once on transient errors before giving up (M7) — the turn
		// is already "done", there's no other recovery path. A final failure is
		// logged distinctly as a DROPPED segment so it's greppable in ops.
		if err := c.sendReplySegment(tgt, seg.text, segUids, segEntities, useMentionAll); err != nil {
			c.logf("DROPPED reply segment for %s (user will not see it): %v", sessionKey, err)
		}
	}
}

// sendReplySegment sends one reply segment with a single bounded retry. The reply
// is the turn's only user-visible output, so a transient send failure (network
// blip) shouldn't silently lose it; one retry covers the common case. The
// client_msg_no is generated ONCE up-front and reused on retry so server-side
// dedup (keyed on client_msg_no) actually suppresses duplicate delivery — a
// fresh uuid per attempt defeated the dedup whenever a 5xx/timeout/TCP-reset
// happened AFTER the server committed but BEFORE the response reached us.
func (c *Connector) sendReplySegment(tgt replyTarget, text string, uids []string, entities []MentionEntity, mentionAll bool) error {
	msgNo := uuid.NewString()
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			sleep(c.ctx(), 500*time.Millisecond)
			if c.ctx().Err() != nil {
				return lastErr // shutting down — don't keep retrying
			}
		}
		if _, err := c.rest.SendTextAsWithMsgNo(c.ctx(), tgt.channelID, tgt.channelType, text, uids, entities, mentionAll, tgt.onBehalfOf, msgNo); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

// typingTicker holds the cancel hook and the done channel of one session's
// typing-heartbeat goroutine. stop cancels and waits for the goroutine to
// exit so there is never a leaked goroutine after a turn.
type typingTicker struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// startTyping begins (or re-arms) the typing heartbeat for a session. It is
// idempotent: a second KindSessionStarted for an already-typing session is a
// no-op so we never spawn two tickers for one turn. It fires one typing ping
// immediately, then a goroutine re-sends every typingInterval until the turn's
// stopTyping runs or the run context is cancelled.
func (c *Connector) startTyping(sessionKey string) {
	tgt, ok := c.target(sessionKey)
	if !ok {
		return
	}

	interval := c.typingInterval
	if interval <= 0 {
		interval = defaultTypingInterval
	}
	send := c.sendTyping
	if send == nil {
		send = c.rest.SendTyping
	}

	// Tie the heartbeat to the run context so a cancelled Run stops every ticker.
	ctx, cancel := context.WithCancel(c.ctx())

	c.mu.Lock()
	if _, exists := c.typers[sessionKey]; exists {
		c.mu.Unlock()
		cancel() // already ticking — drop the spare context
		return
	}
	tt := &typingTicker{cancel: cancel, done: make(chan struct{})}
	c.typers[sessionKey] = tt
	c.mu.Unlock()

	// Fire one immediately — don't wait for the first tick (stream-relay.ts:173).
	if err := send(ctx, tgt.channelID, tgt.channelType); err != nil && ctx.Err() == nil {
		c.logf("send typing for %s: %v", sessionKey, err)
	}

	go func() {
		defer close(tt.done)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := send(ctx, tgt.channelID, tgt.channelType); err != nil && ctx.Err() == nil {
					c.logf("send typing for %s: %v", sessionKey, err)
				}
			}
		}
	}()
}

// stopTyping ends a session's typing heartbeat and waits for its goroutine to
// exit. Safe to call when no ticker is active (no-op) and idempotent across the
// several turn-end paths (OnReply, KindTurnDone, KindError).
func (c *Connector) stopTyping(sessionKey string) {
	c.mu.Lock()
	tt := c.typers[sessionKey]
	delete(c.typers, sessionKey)
	c.mu.Unlock()
	if tt == nil {
		return
	}
	tt.cancel()
	<-tt.done
}

// noResponseFallback is sent when the agent produced no text (cc-channel-octo
// index.ts) so the user gets a reply rather than silence.
const noResponseFallback = "[No response generated. Please try rephrasing your question.]"

// target returns the stored reply target for a session key. It is a pure read:
// the issue-#98 thread reroute is applied ONCE when the target is registered (see
// onInbound), so calling this repeatedly per turn (tool-progress, typing, reply)
// no longer recomputes the reroute or re-logs it (L20).
func (c *Connector) target(sessionKey string) (replyTarget, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.targets[sessionKey]
	return t, ok
}

// peekQueuedTarget returns the target of the FIRST queued turn for sessionKey,
// or ok=false when no turn is queued. Test-only accessor: production callers
// read via c.target(sessionKey) (the map drainTurns mutates as it pops). This
// gives the persona-OBO tests a way to assert "onInbound enqueued a turn with
// THIS target" without re-introducing the racy in-onInbound map write that
// deleted.
func (c *Connector) peekQueuedTarget(sessionKey string) (replyTarget, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	q := c.turnQueues[sessionKey]
	if q == nil || len(q.pending) == 0 {
		return replyTarget{}, false
	}
	return q.pending[0].tgt, true
}
