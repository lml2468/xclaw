package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/lml2468/xclaw/core/config"
	"github.com/lml2468/xclaw/core/control"
	"github.com/lml2468/xclaw/core/cron"
)

// TestCronControlHandlers exercises cron.create/list/delete over the multi-bot
// control handler: owner-gating, the not-enabled error, and round-trip create →
// list → delete. The Manager uses a fixed clock so the schedule is deterministic.
func TestCronControlHandlers(t *testing.T) {
	const owner = "owner-1"
	clk := time.Date(2026, 6, 9, 10, 0, 0, 0, time.Local)
	store := cron.NewStore(filepath.Join(t.TempDir(), "cron.json"))
	mgr := cron.NewManager(store, owner, func() time.Time { return clk })

	reg := newBotRegistry(nil)
	reg.add(&botRuntime{cfg: config.Resolved{BotID: "b1"}, cron: mgr})
	reg.add(&botRuntime{cfg: config.Resolved{BotID: "nocron"}}) // cron == nil
	h := makeMultiBotHandler(context.Background(), reg, time.Now())

	call := func(typ string, body any) (any, error) {
		raw, _ := json.Marshal(body)
		return h(typ, raw)
	}

	// Not enabled for the nocron bot.
	if _, err := call("cron.list", control.CronListBody{BotID: "nocron"}); err == nil {
		t.Fatal("cron.list on a bot without cron should error")
	}

	// Non-owner create is rejected.
	if _, err := call("cron.create", control.CronCreateBody{
		BotID: "b1", UID: "intruder", Schedule: "0 9 * * *", Prompt: "p",
	}); err == nil {
		t.Fatal("non-owner cron.create should error")
	}

	// Owner create (DM-bound: no channelId).
	res, err := call("cron.create", control.CronCreateBody{
		BotID: "b1", UID: owner, Schedule: "0 9 * * *", Prompt: "daily standup",
	})
	if err != nil {
		t.Fatalf("owner cron.create: %v", err)
	}
	info, ok := res.(control.CronTaskInfo)
	if !ok || info.ID == "" || info.NextRun == "" {
		t.Fatalf("unexpected create result: %#v", res)
	}

	// List shows the task.
	listRes, err := call("cron.list", control.CronListBody{BotID: "b1"})
	if err != nil {
		t.Fatalf("cron.list: %v", err)
	}
	tasks := listRes.([]control.CronTaskInfo)
	if len(tasks) != 1 || tasks[0].ID != info.ID {
		t.Fatalf("list mismatch: %#v", tasks)
	}

	// Non-owner delete is rejected.
	if _, err := call("cron.delete", control.CronDeleteBody{BotID: "b1", UID: "intruder", ID: info.ID}); err == nil {
		t.Fatal("non-owner cron.delete should error")
	}

	// Owner delete succeeds; list is then empty.
	if _, err := call("cron.delete", control.CronDeleteBody{BotID: "b1", UID: owner, ID: info.ID}); err != nil {
		t.Fatalf("owner cron.delete: %v", err)
	}
	listRes, _ = call("cron.list", control.CronListBody{BotID: "b1"})
	if n := len(listRes.([]control.CronTaskInfo)); n != 0 {
		t.Fatalf("expected empty list after delete, got %d", n)
	}
}
