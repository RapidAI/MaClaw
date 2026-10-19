package scheduler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// memoryTaskStore is the service database stand-in. One manager reads it.
type memoryTaskStore struct {
	mu    sync.Mutex
	tasks []ScheduledTask
}

func (s *memoryTaskStore) LoadScheduledTasks() ([]ScheduledTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ScheduledTask, len(s.tasks))
	copy(out, s.tasks)
	return out, nil
}

func (s *memoryTaskStore) SaveScheduledTasks(tasks []ScheduledTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append([]ScheduledTask(nil), tasks...)
	return nil
}

func TestDesktopBotEventCases(t *testing.T) {
	t.Run("schedule_0800_no_computer", func(t *testing.T) {
		store := &memoryTaskStore{}
		mgr, err := NewManagerFromStore(store)
		if err != nil {
			t.Fatal(err)
		}
		id, err := mgr.Add(ScheduledTask{
			Name: "morning", Action: "read memory",
			Hour: 8, Minute: 0, DayOfWeek: -1, DayOfMonth: -1,
		})
		if err != nil {
			t.Fatal(err)
		}
		task := mgr.Get(id)
		if task == nil || task.NextRunAt == nil || task.NextRunAt.Hour() != 8 || task.NextRunAt.Minute() != 0 {
			t.Fatalf("next = %+v", task)
		}
		due := *task.NextRunAt

		var mu sync.Mutex
		calls := 0
		sessionCalls := 0
		mgr.SetExecutor(func(_ context.Context, got *ScheduledTask) (string, error) {
			mu.Lock()
			calls++
			mu.Unlock()
			if got.Action != "read memory" {
				t.Errorf("action = %q", got.Action)
			}
			return "无变化", nil
		})
		mgr.FireDue(due)

		deadline := time.Now().Add(2 * time.Second)
		for {
			mu.Lock()
			n := calls
			mu.Unlock()
			got := mgr.Get(id)
			if n == 1 && mgr.Inflight() == 0 && got != nil && got.RunCount == 1 && got.LastResult == "无变化" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("calls=%d inflight=%d task=%+v", n, mgr.Inflight(), got)
			}
			time.Sleep(5 * time.Millisecond)
		}
		mgr.FireDue(due)
		quiet := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(quiet) {
			mu.Lock()
			n := calls
			mu.Unlock()
			if n != 1 || mgr.Inflight() != 0 || sessionCalls != 0 {
				t.Fatalf("second fire calls=%d inflight=%d session=%d", n, mgr.Inflight(), sessionCalls)
			}
			time.Sleep(10 * time.Millisecond)
		}
		delivery := &TaskDelivery{
			Enabled: true,
			Channel: DeliveryChannelLansenger,
			Targets: []DeliveryTarget{{Kind: DeliveryKindGroup, GroupID: "g"}},
			On:      DeliveryOnAlways,
		}
		if delivery.ShouldDeliver(nil, "无变化") {
			t.Fatal("无变化 was sent to the user")
		}
	})

	t.Run("legacy_json_imports_once", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "scheduled_tasks.json")
		raw, err := json.Marshal([]ScheduledTask{{
			ID: "legacy-1", Name: "morning", Action: "read memory",
			Hour: 8, Minute: 0, DayOfWeek: -1, DayOfMonth: -1, Status: "active",
		}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		store := &memoryTaskStore{}
		n, err := ImportLegacyTasks(path, store)
		if err != nil || n != 1 {
			t.Fatalf("import n=%d err=%v", n, err)
		}
		kept, err := os.ReadFile(path)
		if err != nil || string(kept) != string(raw) {
			t.Fatal("scheduled_tasks.json was rewritten or removed")
		}
		again, err := ImportLegacyTasks(path, store)
		if err != nil || again != 0 {
			t.Fatalf("second import n=%d err=%v", again, err)
		}
		kept, err = os.ReadFile(path)
		if err != nil || string(kept) != string(raw) {
			t.Fatal("the pre-migration file changed")
		}
		mgr, err := NewManagerFromStore(store)
		if err != nil {
			t.Fatal(err)
		}
		list := mgr.List()
		if len(list) != 1 || list[0].ID != "legacy-1" || list[0].Hour != 8 {
			t.Fatalf("rows = %+v", list)
		}
		kept, err = os.ReadFile(path)
		if err != nil || string(kept) != string(raw) {
			t.Fatal("the service database wrote back to the json file")
		}
	})
}
