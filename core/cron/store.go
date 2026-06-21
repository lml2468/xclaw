// Cron task persistence — per-bot `<dataDir>/cron.json` (port of cron-store.ts).
//
// Holds the scheduled tasks a bot has registered. Read by both the control-bus
// create/list/delete handlers and the scheduler tick (~every 30s). Unlike
// cc-channel's single-threaded Node runtime, the Go daemon runs the scheduler
// and control handlers on different goroutines, so the read-modify-write is
// guarded by a per-store mutex (Go analogue of Node's implicit serialization);
// the atomic temp+rename additionally guarantees a reader never sees a partial
// file even across a crash.
package cron

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// ChannelKind mirrors router.ChannelType without importing it (the store stays a
// leaf package). 1 = DM, 2 = Group, matching router and octo.
type ChannelKind int

// Task is one scheduled task. Persisted as a plain JSON object.
type Task struct {
	// ID is a stable handle (uuid) used by cron.delete; not user-chosen.
	ID string `json:"id"`
	// Schedule is a 5-field cron expression OR a one-shot ISO datetime.
	Schedule string `json:"schedule"`
	// Recurring: true = re-schedule after each fire; false = delete after one fire.
	Recurring bool `json:"recurring"`
	// Prompt is injected as the synthetic message's text (≤ MaxPromptBytes).
	Prompt string `json:"prompt"`
	// Bound session coords — where the fired task runs and replies.
	ChannelID   string      `json:"channelId"`
	ChannelType ChannelKind `json:"channelType"`
	FromUID     string      `json:"fromUid"`
	FromName    string      `json:"fromName,omitempty"`
	// CreatedBy is the uid that registered the task (owner-gate source of truth).
	CreatedBy string `json:"createdBy"`
	// Enabled: the scheduler skips disabled tasks (kept for a future cron.disable).
	Enabled bool `json:"enabled"`
	// CreatedAt is the Unix ms of creation.
	CreatedAt int64 `json:"createdAt"`
	// LastRun is the Unix ms of the last fire, or 0 if never fired.
	LastRun int64 `json:"lastRun,omitempty"`
	// NextRun is the Unix ms of the next fire (the scheduler's due check), or 0
	// if none (an inert/exhausted task).
	NextRun int64 `json:"nextRun,omitempty"`
}

// Caps (port of cron-store.ts constants).
const (
	// MaxPromptBytes caps a task prompt (bytes).
	MaxPromptBytes = 2048
	// MaxTasksPerBot caps the number of tasks per bot.
	MaxTasksPerBot = 50
)

// Store loads/saves a single bot's cron.json.
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore returns a Store backed by the given cron.json path.
func NewStore(cronJSONPath string) *Store {
	return &Store{path: cronJSONPath}
}

// load parses cron.json. Returns an error on malformed JSON (loud, not silent).
// The caller holds s.mu.
func (s *Store) load() ([]Task, error) {
	raw, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return []Task{}, nil
	}
	if err != nil {
		return nil, err
	}
	var tasks []Task
	if err := json.Unmarshal(raw, &tasks); err != nil {
		return nil, fmt.Errorf("cron.json is malformed (%s): %w", s.path, err)
	}
	return tasks, nil
}

// save atomically writes the task array (temp file + rename). The caller holds s.mu.
func (s *Store) save(tasks []Task) error {
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.path, data, 0o600)
}

// writeAtomic writes data to path via path+".tmp" + fsync + rename so a power
// loss or process crash mid-write leaves either the old cron.json intact or a
// fully committed new file — never a half-written one. Removes the .tmp on
// any failure between WriteFile and Rename so the operator's data dir doesn't
// accumulate stale .tmp files.
func writeAtomic(path string, data []byte, perm os.FileMode) (err error) {
	tmp := path + ".tmp"
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load returns the bot's tasks ([] when the file is absent). Thread-safe.
func (s *Store) Load() ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// Update is an atomic read-modify-write: load the current tasks, apply mutator,
// persist the result, and return it. The whole sequence runs under s.mu, so a
// concurrent control-bus create/delete and a scheduler tick can never interleave
// mid-operation — eliminating the lost-update race. All cron mutations (create,
// delete, scheduler advance) go through this one method.
//
// mutator returns the next slice and a changed flag; when changed is false the
// write is skipped (e.g. an idle scheduler tick), avoiding a rewrite on every
// 30s tick.
func (s *Store) Update(mutator func(tasks []Task) (next []Task, changed bool)) ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.load()
	if err != nil {
		return nil, err
	}
	next, changed := mutator(current)
	if changed {
		if err := s.save(next); err != nil {
			return nil, err
		}
	}
	return next, nil
}
