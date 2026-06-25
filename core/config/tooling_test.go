package config

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSettingSourcesDefaultsToUser(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	writeFile(t, cfg, `{"bots":[{"id":"b","apiUrl":"https://x.example"}]}`)
	b := loadSingleBot(t, cfg)
	if !reflect.DeepEqual(b.Agent.SettingSources, []string{"user"}) {
		t.Fatalf("settingSources default = %v, want [user]", b.Agent.SettingSources)
	}
}

func TestSettingSourcesUserProjectAccepted(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	writeFile(t, cfg, `{"bots":[{"id":"b","apiUrl":"https://x.example","agent":{"settingSources":["user","project"]}}]}`)
	b := loadSingleBot(t, cfg)
	if !reflect.DeepEqual(b.Agent.SettingSources, []string{"user", "project"}) {
		t.Fatalf("settingSources = %v", b.Agent.SettingSources)
	}
}

func TestSettingSourcesRejectsLocal(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	writeFile(t, cfg, `{"bots":[{"id":"b","apiUrl":"https://x.example","agent":{"settingSources":["user","local"]}}]}`)
	if _, err := Load(cfg); err == nil {
		t.Fatal("expected error: settingSources 'local' must be rejected")
	}
}

func TestToolNameRejectsCommaAndSpace(t *testing.T) {
	for _, bad := range []string{`["Read,Bash"]`, `["Read Bash"]`} {
		dir := t.TempDir()
		cfg := filepath.Join(dir, "config.json")
		writeFile(t, cfg, `{"bots":[{"id":"b","apiUrl":"https://x.example","agent":{"tools":{"default":`+bad+`}}}]}`)
		if _, err := Load(cfg); err == nil {
			t.Fatalf("expected error for malformed tool name %s", bad)
		}
	}
}

func TestToolNameAcceptsMCPAndGlob(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	writeFile(t, cfg, `{"bots":[{"id":"b","apiUrl":"https://x.example","agent":{"tools":{"default":["Read","mcp__srv__do","mcp__*"],"channels":{"c1":["Bash"]}}}}]}`)
	b := loadSingleBot(t, cfg)
	if got, ok := b.Agent.Tools.Resolve("c1"); !ok || !reflect.DeepEqual(got, []string{"Bash"}) {
		t.Fatalf("channel resolve: %v ok=%v", got, ok)
	}
}

func TestToolPolicyResolve(t *testing.T) {
	var nilP *ToolPolicy
	if _, ok := nilP.Resolve("k"); ok {
		t.Fatal("nil policy must be unset")
	}
	if _, ok := (&ToolPolicy{}).Resolve("k"); ok {
		t.Fatal("nil Default must be unset")
	}

	p := &ToolPolicy{Default: []string{"Read"}, Channels: map[string][]string{"c1": {"Bash"}, "muz": {}}}
	if got, ok := p.Resolve("c1"); !ok || !reflect.DeepEqual(got, []string{"Bash"}) {
		t.Fatalf("channel override: %v ok=%v", got, ok)
	}
	if got, ok := p.Resolve("muz"); !ok || len(got) != 0 {
		t.Fatalf("muzzle (explicit empty) must be ok with no tools: %v ok=%v", got, ok)
	}
	if got, ok := p.Resolve("other"); !ok || !reflect.DeepEqual(got, []string{"Read"}) {
		t.Fatalf("fallthrough to default: %v ok=%v", got, ok)
	}
}

// TestToolPolicyEmptyDefaultSurvivesJSON is the regression for the muzzle case:
// an explicit empty Default (bot-level "no tools") must round-trip through JSON
// distinct from absent (nil = driver default). Default has no omitempty for
// exactly this reason.
func TestToolPolicyEmptyDefaultSurvivesJSON(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      *ToolPolicy
		wantNil bool
		wantLen int
	}{
		{"muzzle (empty)", &ToolPolicy{Default: []string{}}, false, 0},
		{"scoped", &ToolPolicy{Default: []string{"Read"}}, false, 1},
		{"unset", &ToolPolicy{Default: nil}, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			var got ToolPolicy
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if (got.Default == nil) != tc.wantNil {
				t.Fatalf("Default nil=%v, want %v (json=%s)", got.Default == nil, tc.wantNil, raw)
			}
			if len(got.Default) != tc.wantLen {
				t.Fatalf("len(Default)=%d, want %d (json=%s)", len(got.Default), tc.wantLen, raw)
			}
		})
	}
}
