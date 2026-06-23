package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// Env is per-bot only. Plain values stay reviewable in config.json; secret
// values are represented by secretRef and resolved from the runtime secret
// backend when building the agent process env.
func TestEnvPlainAndSecretRefs(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	writeFile(t, cfg, `{
	  "bots":[{
	    "id":"alpha",
	    "apiUrl":"https://octo.example",
	    "octoToken":"bf_a",
	    "agent":{
	      "gatewayBaseUrl":"https://gw.example/v1",
	      "gatewayToken":"sk-ant-xyz",
	      "env":{
	        "OCTO_BOT_ID":{"value":"alpha-bot"},
	        "GH_TOKEN":{"secretRef":"env/GH_TOKEN"}
	      }
	    }
	  }]
	}`)

	bots, err := Load(cfg)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	env := bots[0].Agent.Env
	if env["OCTO_BOT_ID"].Value != "alpha-bot" {
		t.Fatalf("plain per-bot env missing: %v", env)
	}
	if env["GH_TOKEN"].SecretRef != "env/GH_TOKEN" {
		t.Fatalf("secret env ref missing: %v", env)
	}

	de := bots[0].DriverEnv(bots[0].Agent.GatewayToken, "", func(ref string) string {
		if ref == "env/GH_TOKEN" {
			return "ghp_actual"
		}
		return ""
	})
	joined := strings.Join(de, "\n")
	for _, want := range []string{
		"OCTO_BOT_ID=alpha-bot", "GH_TOKEN=ghp_actual",
		"ANTHROPIC_BASE_URL=https://gw.example/v1", "ANTHROPIC_AUTH_TOKEN=sk-ant-xyz",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("claude DriverEnv missing %q in:\n%s", want, joined)
		}
	}
}

func TestDriverEnvSkipsMissingSecretRefs(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	writeFile(t, cfg, `{"bots":[{"id":"b","agent":{"env":{"GH_TOKEN":{"secretRef":"env/GH_TOKEN"}}}}]}`)

	bots, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range bots[0].DriverEnv("", "", nil) {
		if strings.HasPrefix(e, "GH_TOKEN=") {
			t.Fatalf("unresolved secretRef should not be injected; got %q", e)
		}
	}
}

func TestDriverEnvEmptyWhenUnset(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	writeFile(t, cfg, `{"bots":[{"id":"alpha","octoToken":"t"}]}`)
	bots, _ := Load(cfg)
	// With no gateway URL/token/env, the only DriverEnv entry is the isolation
	// var (CLAUDE_CONFIG_DIR -> the per-bot config root), on by default.
	env := bots[0].DriverEnv("", "", nil)
	if len(env) != 1 || env[0] != "CLAUDE_CONFIG_DIR="+bots[0].ClaudeConfigDir {
		t.Fatalf("expected only CLAUDE_CONFIG_DIR, got %v", env)
	}
}

func TestDriverEnvInheritUserConfigOptsOut(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	writeFile(t, cfg, `{"bots":[{"id":"alpha","octoToken":"t","agent":{"inheritUserConfig":true}}]}`)
	bots, _ := Load(cfg)
	if len(bots[0].DriverEnv("", "", nil)) != 0 {
		t.Fatalf("inheritUserConfig should suppress CLAUDE_CONFIG_DIR, got %v", bots[0].DriverEnv("", "", nil))
	}
}

// DriverEnv with a non-empty octo token injects the octo-cli companion
// credential (OCTO_BOT_TOKEN + OCTO_API_BASE_URL) so the spawned agent's
// octo-cli authenticates from env (the fallback path; the primary path is
// the on-disk profile written by octocli.Login).
func TestDriverEnvOctoFallbackVars(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	writeFile(t, cfg, `{"bots":[{"id":"alpha","apiUrl":"https://octo.example","octoToken":"t"}]}`)
	bots, err := Load(cfg)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	joined := strings.Join(bots[0].DriverEnv("", "bf_injected", nil), "\n")
	for _, want := range []string{
		"OCTO_BOT_TOKEN=bf_injected",
		"OCTO_API_BASE_URL=https://octo.example",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("DriverEnv missing %q in:\n%s", want, joined)
		}
	}

	joined = strings.Join(bots[0].DriverEnv("", "", nil), "\n")
	if strings.Contains(joined, "OCTO_BOT_TOKEN=") {
		t.Fatalf("empty octo token should omit OCTO_BOT_TOKEN, got:\n%s", joined)
	}
}
