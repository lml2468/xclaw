// Package autostart manages the "launch at login" toggle on macOS via a per-user
// LaunchAgent plist under ~/Library/LaunchAgents/. Enabled when the plist
// exists; Enable() writes it and bootstraps it into the gui session so it loads
// immediately AND on next login; Disable() reverses both. The plist always
// points at the .app bundle the daemon is currently running from — moving the
// app or running from a dev path will refuse with a clear error.
//
// On non-darwin platforms every method is a safe no-op (Enabled → false,
// Enable → "unsupported on this OS") so callers can stay platform-agnostic.
package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
)

const label = "com.xclaw.desktop"

// plistPath is ~/Library/LaunchAgents/<label>.plist.
func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
}

// appBundlePath resolves the .app bundle the daemon is running from
// (/Applications/XClaw.app or wherever). Refuses if not inside a .app, so we
// don't autostart a dev-build binary at some ephemeral cache path.
func appBundlePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	// Expect: <bundle>.app/Contents/MacOS/<binary>
	macos := filepath.Dir(real)
	contents := filepath.Dir(macos)
	bundle := filepath.Dir(contents)
	if filepath.Base(macos) != "MacOS" || filepath.Base(contents) != "Contents" || filepath.Ext(bundle) != ".app" {
		return "", fmt.Errorf("not running inside a .app bundle: %s", real)
	}
	return bundle, nil
}

// Supported reports whether launch-at-login is implemented on this OS.
func Supported() bool { return runtime.GOOS == "darwin" }

// Enabled reports whether a LaunchAgent plist currently exists for XClaw.
// (We don't query launchctl — the plist being on disk IS the user-visible
// "enabled" state, and it survives reboots even if the agent isn't loaded.)
func Enabled() (bool, error) {
	if !Supported() {
		return false, nil
	}
	_, err := os.Stat(plistPath())
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

// Enable writes the LaunchAgent plist and bootstraps it into the user's gui
// domain so it loads immediately + on every subsequent login.
func Enable() error {
	if !Supported() {
		return fmt.Errorf("launch-at-login is only supported on macOS")
	}
	bundle, err := appBundlePath()
	if err != nil {
		return fmt.Errorf("resolve app bundle: %w", err)
	}
	exe := filepath.Join(bundle, "Contents", "MacOS", "xclaw")
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>ProcessType</key><string>Interactive</string>
</dict>
</plist>
`, label, exe)
	path := plistPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return err
	}
	// Reload: bootout any stale registration (might point at an old bundle
	// path), then bootstrap fresh. bootout exits non-zero when nothing is
	// loaded — that's fine, ignore.
	uid := strconv.Itoa(os.Getuid())
	_ = exec.Command("launchctl", "bootout", "gui/"+uid, path).Run()
	if out, err := exec.Command("launchctl", "bootstrap", "gui/"+uid, path).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w (output: %s)", err, string(out))
	}
	return nil
}

// Disable removes the LaunchAgent plist and unloads it from the gui domain.
// Idempotent: absent plist → nil.
func Disable() error {
	if !Supported() {
		return nil
	}
	path := plistPath()
	uid := strconv.Itoa(os.Getuid())
	_ = exec.Command("launchctl", "bootout", "gui/"+uid, path).Run() // ignore: maybe not loaded
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
