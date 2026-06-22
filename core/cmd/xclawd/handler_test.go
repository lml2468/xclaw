package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lml2468/xclaw/core/config"
	"github.com/lml2468/xclaw/core/control"
	"github.com/lml2468/xclaw/core/control/wire"
)

// TestSecretInjectHandler verifies the multi-bot secret.inject command routes a
// secret into the addressed bot's in-memory store (and errors on unknown
// bot/kind). It only touches the secrets field, so gateway/store can be nil.
func TestSecretInjectHandler(t *testing.T) {
	reg := newBotRegistry(nil)
	sec := &secretStore{}
	bot := &botRuntime{cfg: config.Resolved{BotID: "b1"}, secrets: sec}
	bot.target = &botTarget{id: "b1", secrets: sec}
	reg.add(bot)
	h := makeMultiBotHandler(context.Background(), reg, time.Now())

	inject := func(b control.SecretInjectBody) (any, error) {
		raw, _ := json.Marshal(b)
		return h("secret.inject", raw)
	}

	if _, err := inject(control.SecretInjectBody{BotID: "b1", Kind: wire.SecretKindOcto, Value: "bf_new"}); err != nil {
		t.Fatalf("inject octo: %v", err)
	}
	if bot.secrets.OctoToken() != "bf_new" {
		t.Fatalf("octo token not stored: %q", bot.secrets.OctoToken())
	}

	if _, err := inject(control.SecretInjectBody{BotID: "b1", Kind: wire.SecretKindGateway, Value: "sk_g"}); err != nil {
		t.Fatalf("inject gateway: %v", err)
	}
	if bot.secrets.GatewayToken() != "sk_g" {
		t.Fatalf("gateway token not stored: %q", bot.secrets.GatewayToken())
	}

	if _, err := inject(control.SecretInjectBody{BotID: "nope", Kind: wire.SecretKindOcto, Value: "x"}); err == nil {
		t.Fatal("unknown bot should error")
	}
	if _, err := inject(control.SecretInjectBody{BotID: "b1", Kind: "bogus", Value: "x"}); err == nil {
		t.Fatal("unknown kind should error")
	}
}
