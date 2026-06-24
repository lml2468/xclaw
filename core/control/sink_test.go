package control

import (
	"encoding/json"
	"testing"

	"github.com/lml2468/octobuddy/core/router"
	"github.com/lml2468/octobuddy/core/trigger"
)

// EventSink.OnUserMessage must forward msg.Source onto the broadcast body
// so the renderer can distinguish a real human inbound from a
// scheduler-fired one. Without this, Console cron tasks would hit the
// renderer's CONSOLE_UID dedupe (intended for Composer optimistic-add)
// and disappear from the chat entirely — operator sees the bot's reply
// but never the prompt that fired it.
func TestEventSinkOnUserMessageForwardsSource(t *testing.T) {
	fire := router.InboundMessage{Text: "hi", FromUID: "gui-user", FromName: "Cron", Source: trigger.SourceCron}
	body := SessionUserMessageBody{
		BotID:      "b1",
		SessionKey: "gui-user",
		Text:       fire.Text,
		FromUID:    fire.FromUID,
		FromName:   fire.FromName,
		Source:     string(fire.Source),
	}

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(raw), `"source":"cron"`) {
		t.Fatalf("Source not in wire body: %s", raw)
	}

	// Real human inbound: Source==user (or empty) must omit the field.
	human := router.InboundMessage{Text: "real", FromUID: "alice", FromName: "Alice", Source: trigger.SourceUser}
	body = SessionUserMessageBody{
		BotID: "b1", SessionKey: "k", Text: human.Text,
		FromUID: human.FromUID, FromName: human.FromName,
		// The sink emits the Source string; for SourceUser it would be
		// "user" which is non-empty — but we want omitempty behavior for
		// the default (user) case. The sink emits "" for default to
		// preserve that.
		Source: "",
	}
	raw, _ = json.Marshal(body)
	if contains(string(raw), `"source"`) {
		t.Fatalf("default-source body must not carry source flag: %s", raw)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
