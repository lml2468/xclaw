package claudecli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestAssetNameFollowsAnthropicConvention(t *testing.T) {
	got := assetName()
	if !strings.HasPrefix(got, "claude-") {
		t.Fatalf("asset name must start with claude-: %q", got)
	}
	// The convention is dashes (not underscores like octo-cli), x64 (not
	// amd64), and a plain tag-less name (no version embedded).
	if strings.Contains(got, "_") {
		t.Fatalf("asset name must use dashes not underscores: %q", got)
	}
	if strings.Contains(got, "amd64") {
		t.Fatalf("asset name must map amd64 to x64: %q", got)
	}
}

func TestChecksumFor(t *testing.T) {
	// SHASUMS256.txt format: "<sha>  <filename>" (double-space, but
	// strings.Fields collapses anyway).
	sums := "abc123  claude-darwin-arm64.tar.gz\ndef456  claude-linux-x64.tar.gz\n"
	if got := checksumFor([]byte(sums), "claude-linux-x64.tar.gz"); got != "def456" {
		t.Errorf("checksumFor = %q want def456", got)
	}
	if got := checksumFor([]byte(sums), "missing.tar.gz"); got != "" {
		t.Errorf("checksumFor(missing) = %q want empty", got)
	}
}

func TestVerifyChecksum(t *testing.T) {
	archive := []byte("claude archive bytes")
	name := "claude-darwin-arm64.tar.gz"
	good := sha256hex(archive)
	sums := []byte(fmt.Sprintf("%s  %s\nfeed00  other.tar.gz\n", good, name))

	if err := verifyChecksum(sums, archive, name); err != nil {
		t.Fatalf("matching sha must pass: %v", err)
	}
	if err := verifyChecksum([]byte("feed00  other.tar.gz\n"), archive, name); err == nil {
		t.Fatal("missing entry must fail closed")
	}
	if err := verifyChecksum(nil, archive, name); err == nil {
		t.Fatal("empty sums must fail closed")
	}
	if err := verifyChecksum(sums, []byte("tampered"), name); err == nil {
		t.Fatal("sha mismatch must error")
	}
}

func TestExtractTarGz(t *testing.T) {
	want := []byte("#!/bin/sh\necho claude\n")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		data []byte
	}{
		{"LICENSE", []byte("nope")},
		{binName(), want}, // per-OS name so the lookup matches on Windows too
	} {
		_ = tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.data))})
		_, _ = tw.Write(f.data)
	}
	tw.Close()
	gz.Close()

	got, err := extractBinary(buf.Bytes(), "claude-darwin-arm64.tar.gz")
	if err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("extracted %q want %q", got, want)
	}
}

// TestExtractTarGzRefusesTraversal protects against a tampered archive
// shipping a binary entry with `../` in the path.
func TestExtractTarGzRefusesTraversal(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "../../" + binName(), Mode: 0o755, Size: 4})
	_, _ = tw.Write([]byte("evil"))
	tw.Close()
	gz.Close()

	if _, err := extractBinary(buf.Bytes(), "x.tar.gz"); err == nil {
		t.Fatal("traversal segment must not be extracted")
	}
}

// TestUpgradeFetchesAndVerifies points the package's HTTP seams at a
// stub server, runs Upgrade through the full path (latestRelease →
// download asset → download SHASUMS256.txt → verify → extract →
// install), and asserts the binary lands at BinPath with the right
// version sidecar.
func TestUpgradeFetchesAndVerifies(t *testing.T) {
	withHome(t, t.TempDir())

	body := []byte("#!/bin/sh\necho test claude\n")
	var arc bytes.Buffer
	gz := gzip.NewWriter(&arc)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: binName(), Mode: 0o755, Size: int64(len(body))})
	_, _ = tw.Write(body)
	tw.Close()
	gz.Close()

	asset := arc.Bytes()
	sums := []byte(fmt.Sprintf("%s  %s\n", sha256hex(asset), assetName()))
	tag := "v9.9.9-test"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			rel := ghRelease{TagName: tag}
			rel.Assets = append(rel.Assets, struct {
				Name string `json:"name"`
				URL  string `json:"browser_download_url"`
			}{Name: assetName(), URL: "http://" + r.Host + "/asset"})
			rel.Assets = append(rel.Assets, struct {
				Name string `json:"name"`
				URL  string `json:"browser_download_url"`
			}{Name: "SHASUMS256.txt", URL: "http://" + r.Host + "/sums"})
			_ = json.NewEncoder(w).Encode(rel)
		case r.URL.Path == "/asset":
			_, _ = w.Write(asset)
		case r.URL.Path == "/sums":
			_, _ = w.Write(sums)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	prevAPI, prevClient := apiBase, httpClient
	apiBase = srv.URL
	httpClient = srv.Client()
	t.Cleanup(func() { apiBase = prevAPI; httpClient = prevClient })

	got, err := Upgrade(context.Background())
	if err != nil {
		t.Fatalf("Upgrade: %v", err)
	}
	if got != tag {
		t.Errorf("returned tag = %q want %q", got, tag)
	}
	on, err := os.ReadFile(BinPath())
	if err != nil {
		t.Fatalf("BinPath: %v", err)
	}
	if !bytes.Equal(on, body) {
		t.Errorf("installed bytes mismatch")
	}
	if v := InstalledVersion(); v != tag {
		t.Errorf("InstalledVersion = %q want %q", v, tag)
	}
}

// TestUpgradeRefusesShaMismatch confirms a tampered archive is rejected
// before it lands on disk.
func TestUpgradeRefusesShaMismatch(t *testing.T) {
	withHome(t, t.TempDir())

	asset := []byte("the real archive")
	sums := []byte(fmt.Sprintf("%s  %s\n", sha256hex([]byte("DIFFERENT")), assetName()))
	tag := "v9.9.9-test"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			rel := ghRelease{TagName: tag}
			rel.Assets = append(rel.Assets, struct {
				Name string `json:"name"`
				URL  string `json:"browser_download_url"`
			}{Name: assetName(), URL: "http://" + r.Host + "/asset"})
			rel.Assets = append(rel.Assets, struct {
				Name string `json:"name"`
				URL  string `json:"browser_download_url"`
			}{Name: "SHASUMS256.txt", URL: "http://" + r.Host + "/sums"})
			_ = json.NewEncoder(w).Encode(rel)
		case r.URL.Path == "/asset":
			_, _ = w.Write(asset)
		case r.URL.Path == "/sums":
			_, _ = w.Write(sums)
		}
	}))
	defer srv.Close()

	prevAPI, prevClient := apiBase, httpClient
	apiBase = srv.URL
	httpClient = srv.Client()
	t.Cleanup(func() { apiBase = prevAPI; httpClient = prevClient })

	if _, err := Upgrade(context.Background()); err == nil {
		t.Fatal("Upgrade must refuse sha mismatch")
	}
	if _, err := os.Stat(BinPath()); err == nil {
		t.Fatal("Upgrade must not leave a binary on sha mismatch")
	}
}

// withHome redirects os.UserHomeDir into a temp dir for the test. Sets
// both HOME (POSIX) and USERPROFILE (Windows).
func withHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}
