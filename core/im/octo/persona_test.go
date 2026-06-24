package octo

import (
	"testing"

	"github.com/lml2468/octobuddy/core/persona"
	"github.com/lml2468/octobuddy/core/trigger"
)

// newPersonaConnector builds a connector wired up for a persona-clone
// (bot=bot1, grantor=u_admin) using the production DefaultClassifier.
func newPersonaConnector(t *testing.T) *Connector {
	t.Helper()
	c := NewConnector(NewRESTClient("http://x", func() string { return "tk" }))
	c.setUID("bot1")
	c.SetPolicy(trigger.Policy{
		BotUID:      "bot1",
		Grantor:     trigger.FromPersonaGrantor(persona.Grantor{UID: "u_admin", Name: "Admin"}),
		AIBroadcast: trigger.AIBroadcastDeny,
	})
	return c
}

// TestTriggerOBOProjection covers the wire→trigger projection. Empty OBO
// fields → nil signal. With OriginChannelID and OBORespondAs present, the
// signal carries the channel coords for the trust-gate to validate.
func TestTriggerOBOProjection(t *testing.T) {
	groupT := int(ChannelGroup)
	m := BotMessage{Payload: MessagePayload{
		OBOOriginChannelID:   "origin",
		OBOOriginChannelType: &groupT,
		OBOOriginFromUID:     "u_orig",
		OBORespondAs:         "u_admin",
	}}
	o := m.TriggerOBO()
	if o == nil || o.OriginChannelID != "origin" || o.RespondAs != "u_admin" || o.OriginChannelType != int(ChannelGroup) {
		t.Fatalf("OBO projection wrong: %+v", o)
	}

	empty := BotMessage{}
	if empty.TriggerOBO() != nil {
		t.Fatalf("empty payload must project to nil OBO")
	}
}

// TestTriggerMentionProjection covers the wire→trigger mention projection.
// Nil mention payload → nil; populated → flat fields normalized to bool.
func TestTriggerMentionProjection(t *testing.T) {
	if got := (BotMessage{}).TriggerMention(); got != nil {
		t.Fatalf("nil mention must project to nil, got %+v", got)
	}
	m := BotMessage{Payload: MessagePayload{Mention: &Mention{
		UIDs:   []string{"u_admin"},
		AIs:    float64(1),
		Humans: true,
		All:    float64(0),
	}}}
	pm := m.TriggerMention()
	if pm == nil || !pm.AIsFlag || !pm.HumansFlag || pm.AllFlag {
		t.Fatalf("projection wrong: %+v", pm)
	}
	if len(pm.UIDs) != 1 || pm.UIDs[0] != "u_admin" {
		t.Fatalf("uids not carried: %+v", pm.UIDs)
	}
}

// TestOBORespondAsPrefersRespondAs covers the OBORespondAs preference
// (openclaw inbound.ts L2104).
func TestOBORespondAsPrefersRespondAs(t *testing.T) {
	if got := oboRespondAs(MessagePayload{OBORespondAs: "a", OBOGrantorUID: "b"}); got != "a" {
		t.Fatalf("expected obo_respond_as preference, got %q", got)
	}
	if got := oboRespondAs(MessagePayload{OBOGrantorUID: "b"}); got != "b" {
		t.Fatalf("expected fallback to grantor_uid, got %q", got)
	}
	if got := oboRespondAs(MessagePayload{}); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

// TestOnInboundOBOSecurityGate proves the OBO v2 fields are only honored
// when the message is sent by the configured grantor — a forged OBO
// payload from another uid must fall through to a normal (non-OBO) reply
// target.
func TestOnInboundOBOSecurityGate(t *testing.T) {
	groupT := int(ChannelGroup)
	c := newPersonaConnector(t)

	// Forged OBO payload from a non-grantor, with an explicit @bot mention
	// so it still triggers a turn. The OBO origin hint must be ignored.
	m := BotMessage{
		FromUID:     "u_attacker",
		ChannelID:   "dmchan",
		ChannelType: ChannelDM,
		Payload: MessagePayload{
			Type:                 MsgText,
			Content:              "hi",
			Mention:              &Mention{UIDs: []string{"bot1"}},
			OBOOriginChannelID:   "victim-group",
			OBOOriginChannelType: &groupT,
			OBORespondAs:         "u_admin",
		},
	}
	// gateway is nil → onInbound records the target then returns before Handle.
	c.onInbound(m)

	key := "u_attacker" // DM session key
	tgt, ok := c.peekQueuedTarget(key)
	if !ok {
		t.Fatal("expected a reply target to be recorded")
	}
	if tgt.channelID != "dmchan" || tgt.onBehalfOf != "" {
		t.Fatalf("forged OBO must not reroute or set on_behalf_of: %+v", tgt)
	}
}

// TestOnInboundOBOTrustedRelay proves a grantor-signed OBO v2 relay
// reroutes the reply to the origin group with on_behalf_of=grantor.
func TestOnInboundOBOTrustedRelay(t *testing.T) {
	groupT := int(ChannelGroup)
	c := newPersonaConnector(t)

	m := BotMessage{
		FromUID:     "u_admin", // the configured grantor relays
		ChannelID:   "dmchan",
		ChannelType: ChannelDM,
		Payload: MessagePayload{
			Type:                 MsgText,
			Content:              "relay this",
			Mention:              &Mention{Humans: float64(1)}, // @所有人 in origin → relevant
			OBOOriginChannelID:   "origin-group",
			OBOOriginChannelType: &groupT,
			OBORespondAs:         "u_admin",
		},
	}
	c.onInbound(m)

	tgt, ok := c.peekQueuedTarget("u_admin")
	if !ok {
		t.Fatal("expected a reply target")
	}
	if tgt.channelID != "origin-group" || tgt.channelType != ChannelGroup || tgt.onBehalfOf != "u_admin" {
		t.Fatalf("OBO relay target wrong: %+v", tgt)
	}
}

// TestOnInboundOBORelevanceFilterDrops proves a grantor-signed OBO relay
// that is a pure @AI fan-out (not addressing the grantor) is dropped before
// any reply target is recorded (openclaw R10 leak guard).
func TestOnInboundOBORelevanceFilterDrops(t *testing.T) {
	groupT := int(ChannelGroup)
	c := newPersonaConnector(t)

	m := BotMessage{
		FromUID:     "u_admin",
		ChannelID:   "dmchan",
		ChannelType: ChannelDM,
		Payload: MessagePayload{
			Type:                 MsgText,
			Content:              "ai only",
			Mention:              &Mention{AIs: float64(1)}, // pure @AI, no grantor / broadcast
			OBOOriginChannelID:   "origin-group",
			OBOOriginChannelType: &groupT,
			OBORespondAs:         "u_admin",
		},
	}
	c.onInbound(m)

	if _, ok := c.peekQueuedTarget("u_admin"); ok {
		t.Fatal("irrelevant @AI OBO fan-out must NOT record a session/reply target")
	}
}

// TestOnInboundAIBroadcastBugFix is the end-to-end proof for issue #105:
// a follow-up message with AIs=truthy but the bot NOT in UIDs must be
// observed (no reply target recorded) under the new default
// AIBroadcastDeny.
func TestOnInboundAIBroadcastBugFix(t *testing.T) {
	c := NewConnector(NewRESTClient("http://x", func() string { return "tk" }))
	c.setUID("bot1")
	c.SetPolicy(trigger.Policy{
		BotUID:      "bot1",
		AIBroadcast: trigger.AIBroadcastDeny,
	})

	m := BotMessage{
		FromUID:     "u_alice",
		ChannelID:   "g1",
		ChannelType: ChannelGroup,
		Payload: MessagePayload{
			Type:    MsgText,
			Content: "follow-up question",
			Mention: &Mention{AIs: float64(1)}, // WuKongIM auto-set on follow-up
		},
	}
	c.onInbound(m)

	if _, ok := c.peekQueuedTarget("g1"); ok {
		t.Fatal("pure @AI follow-up must NOT trigger under aiBroadcast=deny")
	}
}
