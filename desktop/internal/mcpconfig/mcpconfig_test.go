package mcpconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func setHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestSaveLoadRoundTrip(t *testing.T) {
	home := setHome(t)
	const content = `{"mcpServers":{"echo":{"command":"node","args":["x.mjs"]}}}`
	if err := Save("b1", content); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Written to the bot's CLAUDE_CONFIG_DIR/.mcp.json.
	p := filepath.Join(home, ".octobuddy", "b1", ".claude", ".mcp.json")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("expected .mcp.json at %s: %v", p, err)
	}
	got, err := Load("b1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != content {
		t.Fatalf("round-trip mismatch:\n got %q\nwant %q", got, content)
	}
}

func TestLoadAbsentReturnsEmpty(t *testing.T) {
	setHome(t)
	got, err := Load("b1")
	if err != nil {
		t.Fatalf("Load absent: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty for absent config, got %q", got)
	}
}

func TestSaveEmptyDeletes(t *testing.T) {
	home := setHome(t)
	if err := Save("b1", `{"mcpServers":{}}`); err != nil {
		t.Fatalf("Save: %v", err)
	}
	p := filepath.Join(home, ".octobuddy", "b1", ".claude", ".mcp.json")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("precondition: file should exist: %v", err)
	}
	if err := Save("b1", "   \n"); err != nil {
		t.Fatalf("Save blank: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("blank content should delete .mcp.json, stat err = %v", err)
	}
	// Deleting an already-absent file is not an error.
	if err := Save("b1", ""); err != nil {
		t.Fatalf("Save empty on absent: %v", err)
	}
}

func TestSaveRejectsInvalid(t *testing.T) {
	setHome(t)
	cases := map[string]string{
		"not json":           `{not json`,
		"missing mcpServers": `{"other":{}}`,
		"mcpServers not obj": `{"mcpServers":[]}`,
	}
	for name, content := range cases {
		if err := Save("b1", content); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}
}

func TestInvalidBotID(t *testing.T) {
	setHome(t)
	if _, err := Load("../escape"); err == nil {
		t.Fatal("expected error for traversal bot id")
	}
	if err := Save("../escape", `{"mcpServers":{}}`); err == nil {
		t.Fatal("expected error for traversal bot id on save")
	}
}
