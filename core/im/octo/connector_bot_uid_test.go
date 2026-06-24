package octo

import (
	"testing"

	"github.com/lml2468/octobuddy/core/router"
	"github.com/lml2468/octobuddy/core/trigger"
)

// TestPolicyBotUIDOverriddenByRegisteredUID is the regression for the live
// group-chat verification finding: Policy.BotUID is seeded from the local
// config id (e.g. "pr-reviewer") at startup, but IM @-mention payloads
// carry the SERVER-registered uid (e.g. "27dcv...bot"). Without the
// override in prepareInboundTurn the classifier would never match an
// @bot mention in production and the bot would stay silent under
// AIBroadcastDeny.
//
// Test: register uid "registered_uid"; arrange a group inbound that
// @-mentions "registered_uid" (NOT the policy's configured BotUID
// "config_id"). The connector must enqueue the message as a reply turn
// — i.e. classify it as ExplicitBot via the runtime uid.
func TestPolicyBotUIDOverriddenByRegisteredUID(t *testing.T) {
	c := NewConnector(NewRESTClient("http://x", func() string { return "tk" }))
	c.setUID("registered_uid")
	c.SetPolicy(trigger.Policy{
		BotUID:      "config_id", // stale — startup seed, must be overridden
		AIBroadcast: trigger.AIBroadcastDeny,
	})

	m := BotMessage{
		FromUID:     "u_alice",
		ChannelID:   "g1",
		ChannelType: ChannelGroup,
		Payload: MessagePayload{
			Type:    MsgText,
			Content: "hi bot",
			Mention: &Mention{UIDs: []string{"registered_uid"}},
		},
	}
	c.onInbound(m)

	tgt, ok := c.peekQueuedTarget("g1")
	if !ok {
		t.Fatal("@bot using runtime uid must enqueue a reply target; classifier likely matched the stale config BotUID instead")
	}
	if tgt.channelID != "g1" {
		t.Fatalf("target channel wrong: %+v", tgt)
	}
}

// TestPolicyBotUIDStaleConfigStillObserved: a stale config BotUID that
// is NOT the registered uid AND not @-mentioned still observes (so we
// don't double-fire on the wrong path). Belt-and-suspenders for the
// override above.
func TestPolicyBotUIDStaleConfigStillObserved(t *testing.T) {
	c := NewConnector(NewRESTClient("http://x", func() string { return "tk" }))
	c.setUID("registered_uid")
	c.SetPolicy(trigger.Policy{
		BotUID:      "config_id",
		AIBroadcast: trigger.AIBroadcastDeny,
	})

	m := BotMessage{
		FromUID:     "u_alice",
		ChannelID:   "g1",
		ChannelType: ChannelGroup,
		Payload: MessagePayload{
			Type:    MsgText,
			Content: "no @ here",
			Mention: &Mention{UIDs: []string{"someone_else"}}, // ←not the bot
		},
	}
	c.onInbound(m)

	if _, ok := c.peekQueuedTarget("g1"); ok {
		t.Fatal("non-mention must not enqueue a reply target")
	}
	_ = router.ChannelGroup // keep import (used elsewhere in package)
}
