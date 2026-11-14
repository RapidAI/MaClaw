package scheduler

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestEligibleClockSkipsOtherRows(t *testing.T) {
	mgr, err := NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Stop)
	mgr.SetEligible(func(task *ScheduledTask) bool {
		return task != nil && task.DesktopBotID != ""
	})
	botID, err := mgr.Add(ScheduledTask{
		Name: "brief", Action: "做一份简报", DesktopBotID: "bot_1",
		Hour: 8, Minute: 0, DayOfWeek: -1, DayOfMonth: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := mgr.Add(ScheduledTask{
		Name: "mail", Action: "读邮件",
		Hour: 8, Minute: 0, DayOfWeek: -1, DayOfMonth: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	bot := mgr.Get(botID)
	other := mgr.Get(otherID)
	if bot == nil || bot.NextRunAt == nil || other == nil || other.NextRunAt == nil {
		t.Fatalf("bot=%+v other=%+v", bot, other)
	}
	kept := *other.NextRunAt
	var mu sync.Mutex
	var got []string
	mgr.SetExecutor(func(_ context.Context, task *ScheduledTask) (string, error) {
		mu.Lock()
		got = append(got, task.ID)
		mu.Unlock()
		return "ran", nil
	})
	mgr.FireDue(*bot.NextRunAt)
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		fired := mgr.Get(botID)
		if n == 1 && mgr.Inflight() == 0 && fired != nil && fired.RunCount == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("calls=%d inflight=%d fired=%+v", n, mgr.Inflight(), fired)
		}
		time.Sleep(5 * time.Millisecond)
	}
	skipped := mgr.Get(otherID)
	if skipped == nil || skipped.RunCount != 0 || skipped.NextRunAt == nil || !skipped.NextRunAt.Equal(kept) {
		t.Fatalf("other row moved: %+v", skipped)
	}
	mu.Lock()
	if len(got) != 1 || got[0] != botID {
		t.Fatalf("fired %v", got)
	}
	mu.Unlock()
	mgr.FireDue(*bot.NextRunAt)
	quiet := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(quiet) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n != 1 || mgr.Inflight() != 0 {
			t.Fatalf("second fire calls=%d inflight=%d", n, mgr.Inflight())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestUpdateKeepsTheNextRunWhenOnlyTheOrderChanges(t *testing.T) {
	mgr, err := NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Stop)
	id, err := mgr.Add(ScheduledTask{
		Name: "watch", Action: "看一眼", DesktopBotID: "bot_1",
		IntervalMinutes: 5, DayOfWeek: -1, DayOfMonth: -1, Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	before := mgr.Get(id)
	if before == nil || before.NextRunAt == nil {
		t.Fatal("no next run")
	}
	due := *before.NextRunAt
	if err := mgr.Update(id, map[string]interface{}{"action": "看一眼，有变化就说"}); err != nil {
		t.Fatal(err)
	}
	after := mgr.Get(id)
	if after == nil || after.Action != "看一眼，有变化就说" || after.NextRunAt == nil || !after.NextRunAt.Equal(due) || after.IntervalMinutes != 5 {
		t.Fatalf("order edit moved the slot: %+v", after)
	}
	if err := mgr.Update(id, map[string]interface{}{"interval_minutes": 15, "hour": 8, "minute": 30}); err != nil {
		t.Fatal(err)
	}
	moved := mgr.Get(id)
	if moved == nil || moved.IntervalMinutes != 15 || moved.Hour != 8 || moved.Minute != 30 || moved.NextRunAt == nil || !moved.NextRunAt.After(time.Now()) {
		t.Fatalf("period edit did not keep a future slot: %+v", moved)
	}
}
