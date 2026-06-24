package router

import (
	"context"
)

// Decision is the outcome of routing a message.
type Decision int

const (
	Accepted              Decision = iota // gates passed; handler ran.
	Observed                              // observation-only; recorded into groupctx.
	DroppedUnroutable                     // missing identity (no from_uid for DM, no channel_id for group).
	DroppedTooLong                        // text exceeds MaxContentByte; gateway sends the oversized reply.
	DroppedBot                            // blocklist or bot-loop guard.
	DroppedInvariantBreak                 // caller violated the ShouldReply precondition.
	RateLimited                           // first rejection of this window; notify once.
	RateLimitedSilent                     // already notified this window.
)

// String returns the lowercase tag for logs / metrics. Stable across
// releases — operators grep for these.
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

// RouteAndHandle applies all gates then dispatches reply-warranting
// messages to handler under the per-session lock.
//
// Gate ordering: SessionKey (unroutable) → blocklist/loop guards →
// size → rate-limit → handler under lock.
//
// PRECONDITION: msg.ShouldReply() must be true. Gateway.Handle's
// contract enforces this structurally. The check below is the single
// remaining defensive layer and silently returns DroppedInvariantBreak
// rather than panicking so one caller bug can't crash every bot.
func (r *Router) RouteAndHandle(ctx context.Context, msg InboundMessage, handler Handler) (Decision, error) {
	key, err := msg.SessionKey()
	if err != nil {
		return DroppedUnroutable, nil
	}
	if !msg.ShouldReply() {
		return DroppedInvariantBreak, nil
	}

	if decision := r.dmGate(msg); decision != Accepted {
		return decision, nil
	}
	if decision := r.groupBotGuards(msg); decision != Accepted {
		return decision, nil
	}

	if len(msg.Text) > r.cfg.MaxContentByte {
		return DroppedTooLong, nil
	}

	// Cron fires bypass rate-limit (operator-scheduled).
	if !msg.IsCron() {
		if allowed, notify := r.allow(key, msg.FromUID); !allowed {
			if notify {
				return RateLimited, nil
			}
			return RateLimitedSilent, nil
		}
	}

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
// The bot-loop guard only fires when the trigger reason is "ambiguous
// addressing" (the trigger package's call — see
// trigger.Reason.IsAmbiguousAddressing). Explicit @bot / persona /
// reply-to-bot / @AI carry clear intent and let peer-bot senders pass.
// Blocklist fires regardless of trigger reason.
func (r *Router) groupBotGuards(msg InboundMessage) Decision {
	if msg.ChannelType != ChannelGroup || msg.IsCron() {
		return Accepted
	}
	if r.blocklisted[msg.FromUID] {
		return DroppedBot
	}
	if msg.Trigger == nil || !msg.Trigger.Reason.IsAmbiguousAddressing() {
		return Accepted
	}
	if r.looksLikeBot(msg.FromUID) && !r.allowedBots[msg.FromUID] {
		return DroppedBot
	}
	return Accepted
}
