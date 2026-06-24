package router

import (
	"context"
	"testing"

	"github.com/lml2468/octobuddy/core/trigger"
)

// mustRoute is the bot_guard tests' convenience wrapper. The trigger field
// drives whether the message would reply; routing concerns are layered on
// top (blocklist, loop guard, rate limit, size).
func mustRoute(t *testing.T, r *Router, m InboundMessage) Decision {
	t.Helper()
	d, err := r.RouteAndHandle(context.Background(), m,
		func(ctx context.Context, key string, m InboundMessage) error { return nil })
	if err != nil {
		t.Fatalf("route err: %v", err)
	}
	return d
}

// asReply builds a group inbound that the classifier would have marked as
// reply-warranting (e.g. mention_free_group). Use this for tests that
// verify the router's loop-guard / blocklist behavior in a
// would-reply scenario.
func asReply(uid string, reason trigger.Reason) InboundMessage {
	return InboundMessage{
		ChannelType: ChannelGroup, ChannelID: "free", FromUID: uid, Text: "hi",
		Trigger: &trigger.TriggerDecision{Reason: reason, Source: trigger.SourceUser},
	}
}

// asObservation builds a group inbound the classifier marked as
// observation-only. Routes to Observed without invoking the loop guard.
func asObservation(uid string) InboundMessage {
	return InboundMessage{
		ChannelType: ChannelGroup, ChannelID: "normal", FromUID: uid, Text: "hi",
		Trigger: &trigger.TriggerDecision{Reason: trigger.ReasonObservation, Source: trigger.SourceUser},
	}
}

// TestObservationGroupNotReplied: a classifier observation is gateway's
// job to record into groupctx — router refuses with DroppedInvariantBreak
// (distinct from DroppedOBOIrrelevant so audit triage isn't misled).
func TestObservationGroupNotReplied(t *testing.T) {
	r := New(Config{MaxPerMinute: 100})
	if d := mustRoute(t, r, asObservation("alice")); d != DroppedInvariantBreak {
		t.Fatalf("observation reaching router must be invariant_break, got %s", d)
	}
}

// TestMentionFreeReplyAccepted: a classifier-supplied mention_free_group
// decision runs the handler (router doesn't gate; mention-free lives in
// the trigger policy now).
func TestMentionFreeReplyAccepted(t *testing.T) {
	r := New(Config{MaxPerMinute: 100})
	if d := mustRoute(t, r, asReply("alice", trigger.ReasonMentionFreeGroup)); d != Accepted {
		t.Fatalf("mention-free reply must be Accepted, got %s", d)
	}
}

// TestMentionFreeBotLoopGuard: G14 — a mention-free reply from a
// bot-looking uid is loop-guard-dropped UNLESS the decision is explicit_bot
// (which is unambiguous intent), or the bot is in AllowedBotUids.
func TestMentionFreeBotLoopGuard(t *testing.T) {
	r := New(Config{
		MaxPerMinute:   100,
		KnownBotUids:   []string{"peer_known"},
		AllowedBotUids: []string{"trusted_bot"},
	})
	// human in a would-reply group → accepted
	if d := mustRoute(t, r, asReply("human", trigger.ReasonMentionFreeGroup)); d != Accepted {
		t.Fatalf("human in mention-free reply should be accepted, got %s", d)
	}
	if d := mustRoute(t, r, asReply("some_bot", trigger.ReasonMentionFreeGroup)); d != DroppedBot {
		t.Fatalf("_bot suffix should be loop-guard dropped, got %s", d)
	}
	if d := mustRoute(t, r, asReply("peer_known", trigger.ReasonMentionFreeGroup)); d != DroppedBot {
		t.Fatalf("knownBotUids should be loop-guard dropped, got %s", d)
	}
	if d := mustRoute(t, r, asReply("trusted_bot", trigger.ReasonMentionFreeGroup)); d != Accepted {
		t.Fatalf("allowedBotUids should bypass the loop guard, got %s", d)
	}
	// An explicit @mention from a bot bypasses the loop guard — direct
	// mention is unambiguous intent.
	if d := mustRoute(t, r, asReply("some_bot", trigger.ReasonExplicitBot)); d != Accepted {
		t.Fatalf("@-mentioned bot should be accepted, got %s", d)
	}
}

// TestDMBotLoopGuard: G14 — DMs from bot-looking uids are silently dropped
// unless whitelisted; humans are accepted.
func TestDMBotLoopGuard(t *testing.T) {
	r := New(Config{
		MaxPerMinute:   100,
		KnownBotUids:   []string{"peer_known"},
		AllowedBotUids: []string{"trusted_bot"},
	})
	dm := func(uid string) InboundMessage {
		return InboundMessage{ChannelType: ChannelDM, FromUID: uid, Text: "hi"}
	}
	if d := mustRoute(t, r, dm("human")); d != Accepted {
		t.Fatalf("human DM should be accepted, got %s", d)
	}
	if d := mustRoute(t, r, dm("evil_bot")); d != DroppedBot {
		t.Fatalf("_bot DM should be dropped, got %s", d)
	}
	if d := mustRoute(t, r, dm("peer_known")); d != DroppedBot {
		t.Fatalf("knownBotUids DM should be dropped, got %s", d)
	}
	if d := mustRoute(t, r, dm("trusted_bot")); d != Accepted {
		t.Fatalf("allowedBotUids DM should bypass, got %s", d)
	}
}

// TestSelfUIDTreatedAsBot: SelfUID is seeded into knownBotUids, so a
// would-reply self-message in a mention-free group is dropped.
func TestSelfUIDTreatedAsBot(t *testing.T) {
	r := New(Config{MaxPerMinute: 100, SelfUID: "me"})
	if d := mustRoute(t, r, asReply("me", trigger.ReasonMentionFreeGroup)); d != DroppedBot {
		t.Fatalf("self uid should be loop-guard dropped, got %s", d)
	}
}

// TestBotBlocklistDM: a blocklisted uid's DM is silently dropped (DM only).
func TestBotBlocklistDM(t *testing.T) {
	r := New(Config{MaxPerMinute: 100, BotBlocklist: []string{"blocked"}})
	if d := mustRoute(t, r, InboundMessage{ChannelType: ChannelDM, FromUID: "blocked", Text: "hi"}); d != DroppedBot {
		t.Fatalf("blocklisted DM should be dropped, got %s", d)
	}
	if d := mustRoute(t, r, InboundMessage{ChannelType: ChannelDM, FromUID: "ok", Text: "hi"}); d != Accepted {
		t.Fatalf("non-blocklisted DM should be accepted, got %s", d)
	}
}

// TestBotBlocklistGroupHardDrop: a blocklisted sender is dropped in a
// group even when the trigger would reply (the blocklist beats the
// mention gate). Also true in mention-free channels.
func TestBotBlocklistGroupHardDrop(t *testing.T) {
	r := New(Config{MaxPerMinute: 100, BotBlocklist: []string{"blocked"}})
	if d := mustRoute(t, r, asReply("blocked", trigger.ReasonExplicitBot)); d != DroppedBot {
		t.Fatalf("blocklisted explicit-bot group msg should be dropped, got %s", d)
	}
	if d := mustRoute(t, r, asReply("blocked", trigger.ReasonMentionFreeGroup)); d != DroppedBot {
		t.Fatalf("blocklisted mention-free group msg should be dropped, got %s", d)
	}
}

// TestNormalGroupStillRequiresMention: regression — without a trigger
// decision, the router refuses (gateway will have dispatched to Observe
// before reaching here in production).
func TestNormalGroupStillRequiresMention(t *testing.T) {
	r := New(Config{MaxPerMinute: 100})
	if d := mustRoute(t, r, InboundMessage{
		ChannelType: ChannelGroup, ChannelID: "g1", FromUID: "u1", Text: "hi"}); d != DroppedInvariantBreak {
		t.Fatalf("nil-trigger group at router must be invariant_break, got %s", d)
	}
	if d := mustRoute(t, r, asReply("u1", trigger.ReasonExplicitBot)); d != Accepted {
		t.Fatalf("normal group mentioned should be accepted, got %s", d)
	}
}

// TestCronFireBypassesBotGuards: a cron fire (Source==SourceCron) bypasses
// the blocklist + loop guard (operator-scheduled, authenticity is the
// caller's concern).
func TestCronFireBypassesBotGuards(t *testing.T) {
	r := New(Config{MaxPerMinute: 100, BotBlocklist: []string{"sys_bot"}})
	if d := mustRoute(t, r, InboundMessage{
		ChannelType: ChannelGroup, ChannelID: "g1", FromUID: "sys_bot",
		Source: trigger.SourceCron, Text: "tick"}); d != Accepted {
		t.Fatalf("cron fire should bypass bot guards + mention gate, got %s", d)
	}
}

// TestOBOIrrelevantNeverReachesRouter: an OBO-irrelevant decision is
// gateway.Handle's job to short-circuit before invoking the router (the
// R10 leak guard is a security boundary, not a routing concern). If the
// gateway ever regressed and let one through, the router's invariant-
// break path catches it — but the audit tag (DroppedOBOIrrelevant vs
// DroppedInvariantBreak) is set by the gateway, NOT here.
func TestOBOIrrelevantNeverReachesRouter(t *testing.T) {
	r := New(Config{MaxPerMinute: 100})
	called := false
	d, _ := r.RouteAndHandle(context.Background(),
		InboundMessage{
			ChannelType: ChannelGroup, ChannelID: "g1", FromUID: "u",
			Trigger: &trigger.TriggerDecision{Reason: trigger.ReasonOBOIrrelevant, Source: trigger.SourceUser},
		},
		func(ctx context.Context, key string, m InboundMessage) error { called = true; return nil })
	if d != DroppedInvariantBreak {
		t.Fatalf("OBO at router is a gateway-dispatch bug; want invariant_break, got %s", d)
	}
	if called {
		t.Fatalf("must not invoke handler: called=%v", called)
	}
}
