package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// writeFakeBin writes an executable shell script and returns its path.
func writeFakeBin(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fakebin.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMergedEnvOverrides(t *testing.T) {
	t.Setenv("XCLAW_TEST_BASE", "from-os")
	env := mergedEnv([]string{"XCLAW_TEST_EXTRA=added", "XCLAW_TEST_BASE=overridden"})
	// os var present, extra added, and the override appears AFTER the os value
	// (exec uses the last occurrence).
	var sawBaseOS, sawBaseOverride, sawExtra bool
	lastBase := ""
	for _, e := range env {
		switch e {
		case "XCLAW_TEST_BASE=from-os":
			sawBaseOS = true
			lastBase = "from-os"
		case "XCLAW_TEST_BASE=overridden":
			sawBaseOverride = true
			lastBase = "overridden"
		case "XCLAW_TEST_EXTRA=added":
			sawExtra = true
		}
	}
	if !sawBaseOS || !sawBaseOverride || !sawExtra {
		t.Fatalf("missing entries: base-os=%v override=%v extra=%v", sawBaseOS, sawBaseOverride, sawExtra)
	}
	if lastBase != "overridden" {
		t.Fatalf("override must come last (win), last base = %q", lastBase)
	}
}

// TestClaudeDriverInjectsEnv spawns a fake "claude" that echoes an env var; the
// driver should have set it. The echoed line is not stream-json, so it surfaces
// as a KindSystem event — we just assert the value made it into the subprocess.
func TestClaudeDriverInjectsEnv(t *testing.T) {
	bin := writeFakeBin(t, `echo "GOT:$XCLAW_INJECTED"`)
	d := NewClaudeDriver(bin)
	d.Env = []string{"XCLAW_INJECTED=hello-env"}

	ch, err := d.Query(context.Background(), Request{Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for ev := range ch {
		if ev.Kind == KindSystem && ev.Text == "GOT:hello-env" {
			found = true
		}
		if ev.Raw == "GOT:hello-env" {
			found = true
		}
	}
	if !found {
		t.Fatal("injected env var did not reach the spawned CLI")
	}
}
