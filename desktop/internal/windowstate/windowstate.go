// Package windowstate persists the console window's last bounds in
// ~/.octobuddy/window.json so a relaunch restores it. Failures are
// best-effort: Load returns a zero State on any error; Save logs and
// swallows. Window state loss is a UX regression, not a correctness bug.
package windowstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lml2468/octobuddy/core/safepath"
	"github.com/lml2468/octobuddy/desktop/internal/configstore"
)

// State captures the bounds we restore. Negative or zero values mean
// "use default" to the caller.
type State struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

func (s State) IsZero() bool { return s == State{} }

// filePath returns ~/.octobuddy/window.json. configstore.Dir() returns
// a path even when os.UserHomeDir fails (swallowed error → Join with ""
// → ".octobuddy"); reject a non-absolute result so we don't write state
// into the daemon's CWD on a HOME-unset launchd / container.
func filePath() (string, error) {
	dir := configstore.Dir()
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("windowstate: cannot resolve absolute home dir (got %q)", dir)
	}
	return filepath.Join(dir, "window.json"), nil
}

// Load returns the persisted state, or (zero, nil) when the file
// doesn't exist. Corrupted JSON returns (zero, err); callers treat that
// as "use default" via IsZero.
func Load() (State, error) {
	path, err := filePath()
	if err != nil {
		return State{}, err
	}
	data, err := safepath.SafeReadAbs(path, 4<<10)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("windowstate: read: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("windowstate: decode: %w", err)
	}
	return s, nil
}

// Save persists the state, creating the parent dir if needed.
func Save(s State) error {
	path, err := filePath()
	if err != nil {
		return err
	}
	if err := safepath.SafeMkdirAllAbs(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("windowstate: mkdir: %w", err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("windowstate: encode: %w", err)
	}
	if err := safepath.SafeWriteAbs(path, data, 0o600); err != nil {
		return fmt.Errorf("windowstate: write: %w", err)
	}
	return nil
}
