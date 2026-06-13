package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

// TestClaudeDriverDrainsStdoutAndStderr spawns a fake "claude" that writes to
// both stdout (stream-json) and stderr, then exits non-zero. The driver must
// deliver events from both streams and close the channel cleanly — exercising
// the WaitGroup join that prevents a send-on-closed-channel panic when stderr
// emits around the time stdout reaches EOF.
func TestClaudeDriverDrainsStdoutAndStderr(t *testing.T) {
	bin := writeFakeBin(t, `
echo '{"type":"system","subtype":"init","session_id":"s1"}'
echo 'a warning on stderr' 1>&2
echo '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}'
exit 3
`)
	d := NewClaudeDriver(bin)

	ch, err := d.Query(context.Background(), Request{Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}

	var session, text, stderrErr, exitErr bool
	for ev := range ch { // must drain to a clean close (no panic)
		switch {
		case ev.Kind == KindSessionStarted && ev.SessionID == "s1":
			session = true
		case ev.Kind == KindTextDelta && ev.Text == "hi":
			text = true
		case ev.Kind == KindError && ev.Recoverable && ev.Err == "a warning on stderr":
			stderrErr = true
		case ev.Kind == KindError && strings.Contains(ev.Err, "claude exited"):
			exitErr = true
		}
	}
	if !session || !text {
		t.Fatalf("missing stdout events: session=%v text=%v", session, text)
	}
	if !stderrErr {
		t.Fatal("stderr line was not surfaced as a recoverable error event")
	}
	if !exitErr {
		t.Fatal("non-zero exit was not surfaced as an error event")
	}
}

func TestPartialDeltasSuppressCompleteDuplicate(t *testing.T) {
	// With --include-partial-messages, claude streams live text deltas, then a
	// final complete assistant block carrying the same full text. The driver must
	// stream the deltas and DROP the duplicate complete block (no double text).
	bin := writeFakeBin(t, `
echo '{"type":"system","subtype":"init","session_id":"s1"}'
echo '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hel"}}}'
echo '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}}'
echo '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"hello","usage":{"input_tokens":1,"output_tokens":1}}'
`)
	ch, err := NewClaudeDriver(bin).Query(context.Background(), Request{Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	var completeTextEvents int
	for ev := range ch {
		if ev.Kind == KindTextDelta {
			text += ev.Text
			if !ev.Partial {
				completeTextEvents++
			}
		}
	}
	if text != "hello" {
		t.Fatalf("streamed text = %q, want %q (no duplication)", text, "hello")
	}
	if completeTextEvents != 0 {
		t.Fatalf("complete assistant text must be suppressed when deltas streamed; got %d", completeTextEvents)
	}
}

func TestCompleteTextEmittedWhenNoDeltas(t *testing.T) {
	// Fallback: with no partial deltas (e.g. partials disabled), the complete
	// assistant text must still be emitted.
	bin := writeFakeBin(t, `
echo '{"type":"system","subtype":"init","session_id":"s1"}'
echo '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"plain"}]}}'
`)
	ch, err := NewClaudeDriver(bin).Query(context.Background(), Request{Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for ev := range ch {
		if ev.Kind == KindTextDelta {
			text += ev.Text
		}
	}
	if text != "plain" {
		t.Fatalf("complete text = %q, want %q", text, "plain")
	}
}
