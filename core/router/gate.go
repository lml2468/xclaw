package router

import (
	"context"
)

// Decision is the outcome of routing a message.
type Decision int

const (
	// Accepted: trigger said reply; gates passed; handler ran.
	Accepted Decision = iota
	// Observed: trigger said observation-only; gateway recorded into
	// groupctx as background, no reply. Distinct from a drop — the message
	// IS part of the conversation, just not addressed.
	Observed
	// DroppedUnroutable: missing identity (no from_uid for DM, no
	// channel_id for group). Silent.
	DroppedUnroutable
	// DroppedTooLong: text exceeds MaxContentByte. Gateway responds with
	// the oversized friendly reply.
	DroppedTooLong
	// DroppedBot: blocklist or G14 bot-loop guard. Silent.
	DroppedBot
	// DroppedInvariantBreak: programming-error signal. The router was
	// called with a message that doesn't satisfy its precondition
	// (msg.ShouldReply() must be true). After #117 this is the SINGLE
	// remaining defensive check (the gateway no longer pre-branches
	// observations / OBO-irrelevant — the connector does that). Silent
	// drop rather than panic so one programming bug doesn't crash every
	// bot in the daemon.
	DroppedInvariantBreak
	// RateLimited: rejected; first rejection of this window, gateway
	// notifies the user.
	RateLimited
	// RateLimitedSilent: rejected but already notified this window — stay
	// silent.
	RateLimitedSilent
)

// String returns the lowercase tag for the routing outcome (used in logs
// and metrics). Keep these stable — operators grep for them.
func (d Decision) String() string {
	switch d {
	case Accepted:
		return "accepted"
	case Observed:
		return "observed"
	case DroppedUnroutable:
		return "unroutable"
	case DroppedTooLong:
		return "too_long"
	case DroppedBot:
		return "bot"
	case DroppedInvariantBreak:
		return "invariant_break"
	case RateLimited:
		return "rate_limited"
	case RateLimitedSilent:
		return "rate_limited_silent"
	default:
		return "unknown"
	}
}

// Handler processes an accepted message under the session lock.
type Handler func(ctx context.Context, sessionKey string, msg InboundMessage) error

// RouteAndHandle applies all gates, then dispatches reply-warranting
// messages to handler under the per-session lock. Returns the gate
// decision; handler errors are returned too.
//
// Gate ordering: SessionKey (unroutable) → blocklist/loop guards →
// size → rate-limit → handler under lock.
//
// PRECONDITION: msg.ShouldReply() must be true. The gateway's Handle
// contract enforces this structurally (its callers — connector
// dispatchInbound, cron fire, REPL, control-bus — only invoke for reply-
// warranting messages, and after #117 the gateway no longer pre-branches
// observations / OBO-irrelevant). The check below is the SINGLE
// remaining defensive layer; it returns DroppedInvariantBreak silently
// rather than panicking so one programming bug doesn't crash every bot
// in the daemon. Observation messages are recorded into groupctx by the
// connector / gateway.Observe inline, never through this path.
func (r *Router) RouteAndHandle(ctx context.Context, msg InboundMessage, handler Handler) (Decision, error) {
	key, err := msg.SessionKey()
	if err != nil {
		return DroppedUnroutable, nil
	}
	if !msg.ShouldReply() {
		return DroppedInvariantBreak, nil
	}

	// Blocklist / bot-loop guards.
	if decision := r.dmGate(msg); decision != Accepted {
		return decision, nil
	}
	if decision := r.groupBotGuards(msg); decision != Accepted {
		return decision, nil
	}

	// Content size gate.
	if len(msg.Text) > r.cfg.MaxContentByte {
		return DroppedTooLong, nil
	}

	// Rate limiting (cron fires bypass it — operator-scheduled).
	if !msg.IsCron() {
		if allowed, notify := r.allow(key, msg.FromUID); !allowed {
			if notify {
				return RateLimited, nil
			}
			return RateLimitedSilent, nil
		}
	}

	// Serialize within the session.
	e := r.acquire(key)
	defer r.release(e)

	if err := handler(ctx, key, msg); err != nil {
		return Accepted, err
	}
	return Accepted, nil
}

func (r *Router) dmGate(msg InboundMessage) Decision {
	if msg.ChannelType != ChannelDM || msg.IsCron() {
		return Accepted
	}
	if r.blocklisted[msg.FromUID] {
		return DroppedBot
	}
	if r.looksLikeBot(msg.FromUID) && !r.allowedBots[msg.FromUID] {
		return DroppedBot
	}
	return Accepted
}

// groupBotGuards applies the group-channel blocklist + bot-loop guard.
// Unlike the legacy groupGate, it does NOT own the mention-vs-observe
// decision — that lives in trigger.Classify.
//
// The loop guard fires only when the trigger reason is "ambiguous
// addressing" (issue #105 fix after live-traffic regression: pre-
// refactor the legacy gate only fired the loop guard inside mention-free
// channels; a wider "all non-explicit reply reasons" gate dropped
// legitimate peer-bot @-grantor mentions and peer-bot quote-replies in
// normal groups).
//
// Which reasons count as ambiguous is the trigger package's call (see
// trigger.Reason.IsAmbiguousAddressing). The router no longer pattern-
// matches a specific Reason constant — a future addressing-ambiguous
// reason added to the trigger pipeline (webhook with no @, scheduled-
// mention, …) becomes loop-guarded automatically without touching gate.go.
//
// Blocklist runs regardless of trigger reason (operator-driven hard drop).
func (r *Router) groupBotGuards(msg InboundMessage) Decision {
	if msg.ChannelType != ChannelGroup || msg.IsCron() {
		return Accepted
	}
	// Hard-drop blocklisted senders entirely (even if @-mentioned).
	if r.blocklisted[msg.FromUID] {
		return DroppedBot
	}
	// Unambiguous addressing (explicit @bot, persona, reply-to-bot, @AI)
	// is intentional engagement — let peer-bot senders through.
	if msg.Trigger == nil || !msg.Trigger.Reason.IsAmbiguousAddressing() {
		return Accepted
	}
	if r.looksLikeBot(msg.FromUID) && !r.allowedBots[msg.FromUID] {
		return DroppedBot
	}
	return Accepted
}
