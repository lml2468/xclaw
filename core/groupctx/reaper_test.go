package groupctx

import (
	"testing"
	"time"
)

// TestReapIdleEvictsChannelsPastThreshold proves the reaper deletes the
// window + cursor + roster for a channel that hasn't been pushed to in
// longer than the threshold, while leaving freshly-touched channels
// alone. Bounds memory over the daemon's lifetime — the window was
// previously unbounded across channels (issue #105 follow-on).
func TestReapIdleEvictsChannelsPastThreshold(t *testing.T) {
	g := New(0)
	now := time.Now()
	g.SetClock(func() time.Time { return now })

	g.Push("old", "u1", "Alice", "stale", 1)
	g.Push("fresh", "u2", "Bob", "recent", 2)

	// Skip ahead 2h; "old" hasn't been touched, "fresh" gets a new push.
	now = now.Add(2 * time.Hour)
	g.Push("fresh", "u2", "Bob", "second", 3)

	if evicted := g.ReapIdle(time.Hour); evicted != 1 {
		t.Fatalf("expected 1 channel evicted, got %d", evicted)
	}
	if w := g.windowSnapshot("old"); w != nil {
		t.Fatalf("old channel must be evicted, got window=%v", w)
	}
	if w := g.windowSnapshot("fresh"); len(w) != 2 {
		t.Fatalf("fresh channel must survive with 2 messages, got %d", len(w))
	}
}

// TestReapIdleEvictsRosterOnlyChannel: a channel touched ONLY via
// LearnMember (no Push) still gets a lastTouch entry → ReapIdle can
// evict it past threshold. Regression for the inverse leak the code-
// review caught (pre-fix, LearnMember didn't touch and the channel was
// invisible to the reaper).
func TestReapIdleEvictsRosterOnlyChannel(t *testing.T) {
	g := New(0)
	now := time.Now()
	g.SetClock(func() time.Time { return now })

	g.LearnMember("roster-only", "u1", "Alice")
	now = now.Add(2 * time.Hour)
	if evicted := g.ReapIdle(time.Hour); evicted != 1 {
		t.Fatalf("roster-only channel must be reapable, got %d evictions", evicted)
	}
	if g.MemberMap("roster-only") != nil {
		t.Fatalf("roster-only channel's member map must be evicted with the channel")
	}
}

// TestReapIdleKeepsActivelyReadChannel: a channel that the bot is actively
// replying in (Cursor/SetCursor on every turn) but where no human has
// pushed in >threshold stays alive — reads bump lastTouch.
func TestReapIdleKeepsActivelyReadChannel(t *testing.T) {
	g := New(0)
	now := time.Now()
	g.SetClock(func() time.Time { return now })

	g.Push("active", "u", "A", "first", 1)
	// Two-hour gap with NO new push but constant cursor activity.
	now = now.Add(2 * time.Hour)
	g.SetCursor("active", 1)

	if evicted := g.ReapIdle(time.Hour); evicted != 0 {
		t.Fatalf("actively-cursor'd channel must not be evicted, got %d", evicted)
	}
}

// windowSnapshot is a tiny test helper that returns a copy of the
// channel's window for assertion. Kept here (not as a public method) so
// production code can't depend on it.
func (g *GroupContext) windowSnapshot(channelID string) []message {
	g.mu.Lock()
	defer g.mu.Unlock()
	if w, ok := g.windows[channelID]; ok {
		out := make([]message, len(w))
		copy(out, w)
		return out
	}
	return nil
}
