package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// global sdk.env is the base; per-bot env adds/overrides per key (not whole
// replacement), and the gateway vars are mapped to the claude env names by
// DriverEnv.
func TestEnvPerKeyMergeAndGatewayVars(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	writeFile(t, cfg, `{
	  "apiUrl":"https://octo.example",
	  "sdk":{"env":{"SHARED_DEFAULT":"global","SHARED":"global"},
	         "gatewayBaseUrl":"https://gw.example/v1"},
	  "bots":[{"id":"alpha"}]
	}`)
	// per-bot adds its own OCTO_BOT_ID (a per-bot identity, never global) + a
	// GH_TOKEN, overrides SHARED, and sets its own gateway token.
	writeFile(t, filepath.Join(dir, "alpha", "config.json"), `{
	  "octoToken":"bf_a",
	  "sdk":{"env":{"OCTO_BOT_ID":"alpha-bot","GH_TOKEN":"ghp_x","SHARED":"perbot"},
	         "gatewayToken":"sk-ant-xyz"}
	}`)

	bots, err := Load(cfg)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	env := bots[0].SDK.Env
	if env["SHARED_DEFAULT"] != "global" {
		t.Fatalf("global env key lost: %v", env)
	}
	if env["OCTO_BOT_ID"] != "alpha-bot" {
		t.Fatalf("per-bot OCTO_BOT_ID missing: %v", env)
	}
	if env["GH_TOKEN"] != "ghp_x" {
		t.Fatalf("per-bot env key missing: %v", env)
	}
	if env["SHARED"] != "perbot" {
		t.Fatalf("per-bot should override SHARED, got %q", env["SHARED"])
	}

	// claude driver → ANTHROPIC_* names.
	de := bots[0].DriverEnv()
	joined := strings.Join(de, "\n")
	for _, want := range []string{
		"SHARED_DEFAULT=global", "OCTO_BOT_ID=alpha-bot", "GH_TOKEN=ghp_x", "SHARED=perbot",
		"ANTHROPIC_BASE_URL=https://gw.example/v1", "ANTHROPIC_AUTH_TOKEN=sk-ant-xyz",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("claude DriverEnv missing %q in:\n%s", want, joined)
		}
	}
}

func TestDriverEnvEmptyWhenUnset(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	writeFile(t, cfg, `{"apiUrl":"https://o","octoToken":"t"}`)
	bots, _ := Load(cfg)
	if len(bots[0].DriverEnv()) != 0 {
		t.Fatalf("expected empty DriverEnv, got %v", bots[0].DriverEnv())
	}
}
