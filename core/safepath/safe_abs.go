// Absolute-path helpers for callers that work in absolute filesystem paths
// (the daemon's <home>/.xclaw/<id>/... layout, the desktop's <home>/Library
// /LaunchAgents/..., octocli's <home>/.xclaw/bin/...). When the path lies
// inside the operator's $HOME we route through the safepath dirfd walk so
// every component is O_NOFOLLOW-checked; otherwise the path is operator-
// trusted (config-supplied --config /opt/xclaw/foo or similar) and we fall
// back to the bare os.* primitive so legitimate OS-level symlinks like
// macOS /tmp → /private/tmp don't refuse on startup.
//
// factored from core/cmd/xclawd/safemkdir.go + the inline copies
// in octocli/autostart/configstore so the home-prefix policy + the
// macOS /tmp carve-out + the Windows case-insensitive compare all live in
// one place.

package safepath

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func randHex() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// underHome reports whether absPath is a descendant of $HOME. Returns the
// (home, relPath) pair when so. On Windows the prefix compare is case-
// insensitive (NTFS is case-preserving but case-insensitive), and both
// sides are normalized via filepath.Clean so a config-supplied
// `C:/Users/X/.xclaw/...` (forward-slash JSON-friendly) matches a
// $HOME of `C:\Users\X`.
func underHome(absPath string) (home, rel string, ok bool) {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return "", "", false
	}
	h = filepath.Clean(h)
	abs := filepath.Clean(absPath)
	withSep := h + string(os.PathSeparator)
	matches := strings.HasPrefix(abs, withSep)
	if !matches && runtime.GOOS == "windows" && len(abs) >= len(withSep) {
		// NTFS is case-preserving but case-insensitive: a config-supplied
		// `C:/Users/X/.xclaw/...` (forward-slash JSON-friendly) must match
		// a $HOME of `C:\Users\X`. Compare equal-length prefixes; an
		// EqualFold against an inequal-length string is always false.
		matches = strings.EqualFold(abs[:len(withSep)], withSep)
	}
	if !matches {
		return "", "", false
	}
	return h, filepath.ToSlash(abs[len(withSep):]), true
}

// SafeReadAbs reads absPath via SafeRead when absPath is under $HOME, else
// falls back to os.ReadFile (operator-trusted). cap is enforced in BOTH
// paths so an oversize file errors rather than truncates.
func SafeReadAbs(absPath string, cap int64) ([]byte, error) {
	if home, rel, ok := underHome(absPath); ok {
		return SafeRead(home, rel, cap)
	}
	// Operator-trusted path — bare read, but still cap-enforced so a
	// symlinked-to-huge-file can't OOM us.
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if cap <= 0 {
		return io.ReadAll(f)
	}
	buf, err := io.ReadAll(io.LimitReader(f, cap+1))
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > cap {
		return nil, errOversize{path: absPath, cap: cap}
	}
	return buf, nil
}

// errOversize matches the unix safe_unix.go pattern for the cap-exceeded
// error so callers' errors.Is / message format stays consistent across
// the SafeRead vs SafeReadAbs entry points.
type errOversize struct {
	path string
	cap  int64
}

func (e errOversize) Error() string {
	return "safepath: file " + e.path + " exceeds cap"
}

// SafeWriteAbs writes data to absPath atomically via SafeWrite when under
// $HOME (dirfd + renameat + leaf symlink refusal), else falls back to a
// bare temp+fsync+rename via os.OpenFile|O_EXCL. The fallback has no
// symlink defense at the parent chain (trust boundary == operator) but
// still fsyncs before rename so a crash mid-write on an operator-trusted
// alt path can't leave a zero-byte cron.json / config.json behind.
func SafeWriteAbs(absPath string, data []byte, perm os.FileMode) error {
	if home, rel, ok := underHome(absPath); ok {
		return SafeWrite(home, rel, data, perm)
	}
	dir := filepath.Dir(absPath)
	tmp := filepath.Join(dir, filepath.Base(absPath)+".tmp."+randHex())
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, werr := f.Write(data); werr != nil {
		f.Close()
		_ = os.Remove(tmp)
		return werr
	}
	if serr := f.Sync(); serr != nil {
		f.Close()
		_ = os.Remove(tmp)
		return serr
	}
	if cerr := f.Close(); cerr != nil {
		_ = os.Remove(tmp)
		return cerr
	}
	if rerr := os.Rename(tmp, absPath); rerr != nil {
		_ = os.Remove(tmp)
		return rerr
	}
	return nil
}

// SafeMkdirAllAbs creates absPath via SafeMkdirAll when under $HOME, else
// falls back to os.MkdirAll (operator-trusted). The home-prefix policy +
// macOS /tmp carve-out previously lived in core/cmd/xclawd/safemkdir.go;
// promoted to safepath in.
func SafeMkdirAllAbs(absPath string, perm os.FileMode) error {
	if home, rel, ok := underHome(absPath); ok {
		if rel == "" || rel == "." {
			return nil
		}
		return SafeMkdirAll(home, rel, perm)
	}
	return os.MkdirAll(absPath, perm)
}

// SafeRemoveAllAbs removes absPath via SafeRemoveAll when under $HOME, else
// falls back to os.RemoveAll. Symmetric counterpart to the other *Abs
// helpers; surfaces in configstore.SaveConfig for bot-dir prune.
func SafeRemoveAllAbs(absPath string) error {
	if home, rel, ok := underHome(absPath); ok {
		return SafeRemoveAll(home, rel)
	}
	return os.RemoveAll(absPath)
}
