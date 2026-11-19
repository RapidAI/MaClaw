package scheduler

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestHoldBlocksTheDueSlotUntilRelease(t *testing.T) {
	mgr, err := NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Stop)
	id, err := mgr.Add(ScheduledTask{
		Name: "天气", Action: "查询并生成PDF", DesktopBotID: "bot_4",
		IntervalMinutes: 5, DayOfWeek: -1, DayOfMonth: -1, Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := mgr.Hold("missing"); ok {
		t.Fatal("held an unknown id")
	}
	due := time.Now().Add(-time.Minute)
	mgr.mu.Lock()
	for i := range mgr.tasks {
		if mgr.tasks[i].ID == id {
			mgr.tasks[i].NextRunAt = &due
		}
	}
	mgr.mu.Unlock()
	release, ok := mgr.Hold(id)
	if !ok || release == nil {
		t.Fatal("hold")
	}
	if _, again := mgr.Hold(id); again {
		t.Fatal("second hold")
	}
	if err := mgr.TriggerNow(id); err == nil {
		t.Fatal("manual run started during the hold")
	}
	var mu sync.Mutex
	var n int
	mgr.SetExecutor(func(context.Context, *ScheduledTask) (string, error) {
		mu.Lock()
		n++
		mu.Unlock()
		return "ran", nil
	})
	mgr.FireDue(time.Now())
	time.Sleep(150 * time.Millisecond)
	mu.Lock()
	fired := n
	mu.Unlock()
	if fired != 0 || mgr.Get(id).RunCount != 0 || !mgr.Running(id) {
		t.Fatalf("held slot fired=%d count=%d running=%v", fired, mgr.Get(id).RunCount, mgr.Running(id))
	}
	release()
	release()
	if mgr.Running(id) {
		t.Fatal("release did not drop the hold")
	}
	mgr.FireDue(time.Now())
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		got := n
		mu.Unlock()
		if got == 1 && mgr.Inflight() == 0 && mgr.Get(id).RunCount == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("after release fired=%d count=%d inflight=%d", got, mgr.Get(id).RunCount, mgr.Inflight())
		}
		time.Sleep(5 * time.Millisecond)
	}
}
