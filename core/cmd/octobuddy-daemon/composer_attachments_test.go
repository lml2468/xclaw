package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lml2468/octobuddy/core/control"
	"github.com/lml2468/octobuddy/core/gateway"
)

// pngHeader is the smallest "valid enough" PNG payload the image writer
// accepts. WriteSandboxImage only checks mime + size, not pixel data —
// these eight bytes are the PNG signature, sufficient for the test.
var pngHeader = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// TestComposerAttachmentsProducePromptFragments locks in the shape of the
// fragment a Composer send appends to its text — image hint after any file
// blocks, file blocks separated by newlines, all written to the same
// .octobuddy-media/ layout IM-inbound uses. Regression net on the wire shape +
// the renderer plumbing.
func TestComposerAttachmentsProducePromptFragments(t *testing.T) {
	cwdBase := t.TempDir()
	gw := gateway.New(nil, nil, nil, nil).WithSandbox(cwdBase, "")

	text := []byte("hello, world\n")
	got, err := materializeComposerAttachments(gw, "alice", []control.SessionAttachment{
		{Name: "note.txt", Kind: "file", Data: base64.StdEncoding.EncodeToString(text)},
		{Name: "shot.png", Kind: "image", Mime: "image/png", Data: base64.StdEncoding.EncodeToString(pngHeader)},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}

	if !strings.Contains(got, "[文件: note.txt]") {
		t.Errorf("missing file label in fragment:\n%s", got)
	}
	if !strings.Contains(got, "<file_content name=\"note.txt\" encoding=\"base64\"") {
		t.Errorf("text file should be inlined as <file_content>:\n%s", got)
	}
	if !strings.Contains(got, "[已下载图片到本地: .octobuddy-media/") {
		t.Errorf("missing image hint in fragment:\n%s", got)
	}
	if !strings.HasSuffix(got, " — 请用 Read 工具查看]") {
		t.Errorf("image hint should end with Read instruction, got:\n%s", got)
	}

	// Verify the image actually landed on disk under the Console session's cwd.
	cwd, _ := gw.SessionCwd(1 /*router.ChannelDM*/, "alice")
	entries, err := os.ReadDir(filepath.Join(cwd, ".octobuddy-media"))
	if err != nil {
		t.Fatalf("read media dir: %v", err)
	}
	var foundImage bool
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "-image.png") {
			foundImage = true
		}
	}
	if !foundImage {
		t.Errorf(".octobuddy-media missing the image file: %v", entries)
	}
}

// TestComposerAttachmentsRejectOverLimit refuses a send with too many
// attachments — the per-send count cap is the first defense against a
// runaway client.
func TestComposerAttachmentsRejectOverLimit(t *testing.T) {
	gw := gateway.New(nil, nil, nil, nil).WithSandbox(t.TempDir(), "")
	atts := make([]control.SessionAttachment, composerAttachmentLimit+1)
	for i := range atts {
		atts[i] = control.SessionAttachment{Name: "a", Kind: "file", Data: base64.StdEncoding.EncodeToString([]byte("x"))}
	}
	if _, err := materializeComposerAttachments(gw, "alice", atts); err == nil {
		t.Fatal("expected error for over-limit attachment count")
	}
}

// TestComposerAttachmentsRequireSandbox surfaces an error when the bot has
// no cwdBase configured — silently dropping attachments would lose user
// data; failing the send tells the operator immediately.
func TestComposerAttachmentsRequireSandbox(t *testing.T) {
	gw := gateway.New(nil, nil, nil, nil) // no WithSandbox
	_, err := materializeComposerAttachments(gw, "alice", []control.SessionAttachment{
		{Name: "x.txt", Kind: "file", Data: base64.StdEncoding.EncodeToString([]byte("y"))},
	})
	if err == nil || !strings.Contains(err.Error(), "sandbox") {
		t.Fatalf("expected sandbox-required error, got %v", err)
	}
}
