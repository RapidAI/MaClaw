package scheduler

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdmitOperationTreatsStatusAsRecordState(t *testing.T) {
	hourly := AdmitFields{
		Name: "每小时清理 rapidai 邮箱", TaskAction: "删除邮箱中的所有邮件",
		HasInterval: true, IntervalMinutes: 60, HasMinute: true, Minute: 0, Status: "active",
	}
	op, ok := AdmitOperation(hourly)
	if !ok || op != "create" {
		t.Fatalf("hourly create with status op=%q ok=%v", op, ok)
	}
	task := AdmitTask(hourly)
	if task.Hour != 0 || task.Minute != 0 || task.IntervalMinutes != 60 || task.DayOfWeek != -1 || task.DayOfMonth != -1 || task.Status != "active" {
		t.Fatalf("admit task=%#v", task)
	}
	if op, ok := AdmitOperation(AdmitFields{Status: "active"}); ok {
		t.Fatalf("status alone op=%q", op)
	}
	if op, ok := AdmitOperation(AdmitFields{ID: "task-1", Status: "paused"}); !ok || op != "update" {
		t.Fatalf("id plus status op=%q ok=%v", op, ok)
	}
	if op, ok := AdmitOperation(AdmitFields{ID: "task-1"}); !ok || op != "delete" {
		t.Fatalf("id alone op=%q ok=%v", op, ok)
	}
	if op, ok := AdmitOperation(AdmitFields{}); !ok || op != "list" {
		t.Fatalf("empty op=%q ok=%v", op, ok)
	}
	if _, ok := AdmitOperation(AdmitFields{Name: "n", TaskAction: "a", HasInterval: true, IntervalMinutes: 0}); ok {
		t.Fatal("zero interval is not a schedule")
	}
	if _, ok := AdmitOperation(AdmitFields{StartDate: "2026-10-20"}); ok {
		t.Fatal("a date alone is not an operation")
	}
	if field := AdmitRange(AdmitFields{HasHour: true, Hour: 25}); field != "hour" {
		t.Fatalf("hour range=%q", field)
	}
	if field := AdmitRange(AdmitFields{HasInterval: true, IntervalMinutes: 0}); field != "" {
		t.Fatalf("zero interval range=%q", field)
	}
	if field := AdmitRange(AdmitFields{HasDayOfMonth: true, DayOfMonth: -1}); field != "" {
		t.Fatalf("every-day-of-month range=%q", field)
	}
	patch := AdmitPatch(AdmitFields{Name: "n", TaskAction: "do", Status: "paused", HasMinute: true, Minute: 5})
	if patch["name"] != "n" || patch["action"] != "do" || patch["minute"] != 5 {
		t.Fatalf("patch=%#v", patch)
	}
	if _, ok := patch["status"]; ok {
		t.Fatal("status is the state argument, not a patch field")
	}
	parsed, err := ParseAdmit(map[string]interface{}{
		"name": "每小时清理 rapidai 邮箱", "task_action": " 删除邮件 ",
		"interval_minutes": float64(60), "minute": float64(0), "status": "active",
	})
	if err != nil || parsed.Name == "" || parsed.TaskAction != "删除邮件" || !parsed.HasInterval || parsed.IntervalMinutes != 60 || !parsed.HasMinute || parsed.HasHour {
		t.Fatalf("parse=%#v err=%v", parsed, err)
	}
	if _, err := ParseAdmit(map[string]interface{}{"hour": 1.5}); err == nil {
		t.Fatal("a fractional hour is not an integer field")
	}
	if _, err := ParseAdmit(map[string]interface{}{"group_name": "ops"}); err == nil {
		t.Fatal("an unknown key is not this record")
	}
	if op, field, ok := AdmitScreen(AdmitFields{Name: "n", TaskAction: "a", HasInterval: true, IntervalMinutes: -1}); ok || op != "" || field != "interval" {
		t.Fatalf("negative interval screen op=%q field=%q ok=%v", op, field, ok)
	}
	if op, field, ok := AdmitScreen(AdmitFields{Name: "n", TaskAction: "a", HasInterval: true, IntervalMinutes: 60, Status: "active"}); !ok || op != "create" || field != "" {
		t.Fatalf("hourly screen op=%q field=%q ok=%v", op, field, ok)
	}
	if op, field, ok := AdmitScreen(AdmitFields{Name: "n", TaskAction: "a", HasHour: true, Hour: 9, EndDate: "2026/10/20"}); ok || op != "" || field != "end_date" {
		t.Fatalf("slash date screen op=%q field=%q ok=%v", op, field, ok)
	}
	if field := AdmitRange(AdmitFields{StartDate: "2026-1-5"}); field != "start_date" {
		t.Fatalf("unpadded date range=%q", field)
	}
	if field := AdmitRange(AdmitFields{EndDate: "2026-02-31"}); field != "end_date" {
		t.Fatalf("impossible date range=%q", field)
	}
	if field := AdmitRange(AdmitFields{StartDate: "2024-02-29", EndDate: "2024-02-29"}); field != "" {
		t.Fatalf("leap day range=%q", field)
	}
	if op, field, ok := AdmitScreen(AdmitFields{Name: "n", TaskAction: "a", HasHour: true, Hour: 9, StartDate: "2026-12-01", EndDate: "2026-11-01"}); ok || op != "" || field != "date_range" {
		t.Fatalf("inverted window screen op=%q field=%q ok=%v", op, field, ok)
	}
}

func TestAddWritesInitialStatusBeforeTheTickerCanSeeIt(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Stop)
	fired := make(chan string, 2)
	manager.SetExecutor(func(_ context.Context, task *ScheduledTask) (string, error) {
		fired <- task.Name
		return "ok", nil
	})

	pausedID, err := manager.Add(ScheduledTask{
		Name: "held", Action: "wait", Hour: 0, Minute: 0, DayOfWeek: -1, DayOfMonth: -1, Status: "paused",
	})
	if err != nil {
		t.Fatal(err)
	}
	paused := manager.Get(pausedID)
	if paused == nil || paused.Status != "paused" || paused.NextRunAt != nil {
		t.Fatalf("paused add=%#v", paused)
	}
	if _, err := manager.Add(ScheduledTask{
		Name: "nope", Action: "wait", Hour: 1, Minute: 0, DayOfWeek: -1, DayOfMonth: -1, Status: "disabled",
	}); err == nil {
		t.Fatal("unknown status must not be stored as active")
	}

	activeID, err := manager.Add(ScheduledTask{
		Name: "go", Action: "run", Hour: 0, Minute: 0, DayOfWeek: -1, DayOfMonth: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(48 * time.Hour)
	due := dueTaskIDs(manager.List(), now)
	if len(due) != 1 || due[0] != activeID {
		t.Fatalf("due=%v, want only %s", due, activeID)
	}
	manager.FireDue(now)
	select {
	case name := <-fired:
		if name != "go" {
			t.Fatalf("fired %q, want the active task", name)
		}
	case <-time.After(time.Second):
		t.Fatal("active task was not due")
	}
	held := manager.Get(pausedID)
	if held == nil || held.Status != "paused" || held.NextRunAt != nil {
		t.Fatalf("paused row changed after the tick: %#v", held)
	}
}

func TestUpdateStateWritesFieldsAndStatusInOneSave(t *testing.T) {
	store := &countingTaskStore{}
	manager, err := NewManagerFromStore(store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Stop)
	id, err := manager.Add(ScheduledTask{
		Name: "go", Action: "run", Hour: 0, Minute: 0, DayOfWeek: -1, DayOfMonth: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	saves := store.saves
	if err := manager.UpdateState(id, map[string]interface{}{"action": "stop"}, "paused"); err != nil {
		t.Fatal(err)
	}
	if store.saves != saves+1 {
		t.Fatalf("status write saved %d times", store.saves-saves)
	}
	got := manager.Get(id)
	if got == nil || got.Action != "stop" || got.Status != "paused" || got.NextRunAt != nil {
		t.Fatalf("updated=%#v", got)
	}
	if due := dueTaskIDs(manager.List(), time.Now().Add(48*time.Hour)); len(due) != 0 {
		t.Fatalf("paused row is due: %v", due)
	}
	if err := manager.UpdateState(id, map[string]interface{}{"action": "nope"}, "disabled"); err == nil {
		t.Fatal("unknown status must fail before the write")
	}
	if again := manager.Get(id); again.Action != "stop" || again.Status != "paused" {
		t.Fatalf("rejected status changed the row: %#v", again)
	}
	saves = store.saves
	if err := manager.Resume(id); err != nil {
		t.Fatal(err)
	}
	if store.saves != saves+1 {
		t.Fatalf("resume saved %d times", store.saves-saves)
	}
	resumed := manager.Get(id)
	if resumed == nil || resumed.Status != "active" || resumed.NextRunAt == nil || resumed.Action != "stop" {
		t.Fatalf("resumed=%#v", resumed)
	}
}

func TestWriteTaskKeepsThePreviousRowUntilSaveSucceeds(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Stop)
	id, err := manager.Add(ScheduledTask{
		Name: "go", Action: "run", Hour: 9, Minute: 0, DayOfWeek: -1, DayOfMonth: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Update(id, map[string]interface{}{"name": "changed", "interval_minutes": -1}); err == nil {
		t.Fatal("negative interval must be rejected")
	}
	kept := manager.Get(id)
	if kept == nil || kept.Name != "go" || kept.IntervalMinutes != 0 || kept.Status != "active" {
		t.Fatalf("rejected edit published %#v", kept)
	}

	store := &rejectSaveStore{}
	manager, err = NewManagerFromStore(store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Stop)
	id, err = manager.Add(ScheduledTask{
		Name: "go", Action: "run", Hour: 9, Minute: 0, DayOfWeek: -1, DayOfMonth: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	store.reject = true
	if err := manager.UpdateState(id, map[string]interface{}{"action": "stop"}, "paused"); err == nil {
		t.Fatal("failed save must not report success")
	}
	kept = manager.Get(id)
	if kept == nil || kept.Action != "run" || kept.Status != "active" || kept.NextRunAt == nil {
		t.Fatalf("failed save published %#v", kept)
	}
	store.reject = false
	if err := manager.Pause(id); err != nil {
		t.Fatal(err)
	}
	if err := manager.Update(id, map[string]interface{}{"end_date": "2000-01-01"}); err != nil {
		t.Fatal(err)
	}
	store.reject = true
	if err := manager.Resume(id); err == nil {
		t.Fatal("failed save must not report the row removed")
	}
	kept = manager.Get(id)
	if kept == nil || kept.Status != "paused" || kept.EndDate != "2000-01-01" {
		t.Fatalf("failed expire removal published %#v", kept)
	}
}

func TestResumeRemovesAnExpiredPausedTask(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Stop)
	id, err := manager.Add(ScheduledTask{
		Name: "old", Action: "run", Hour: 9, Minute: 0, DayOfWeek: -1, DayOfMonth: -1, Status: "paused",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Update(id, map[string]interface{}{"end_date": "2000-01-01"}); err != nil {
		t.Fatal(err)
	}
	if manager.Get(id) == nil {
		t.Fatal("a paused row keeps a past end_date until resume")
	}
	err = manager.Resume(id)
	if err == nil || !strings.Contains(err.Error(), "has expired") {
		t.Fatalf("resume err=%v", err)
	}
	if manager.Get(id) != nil {
		t.Fatal("expired resume must remove the row")
	}
}

func TestAddDropsTheRowWhenSaveFails(t *testing.T) {
	store := &rejectSaveStore{}
	manager, err := NewManagerFromStore(store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Stop)
	store.reject = true
	if _, err := manager.Add(ScheduledTask{
		Name: "go", Action: "run", Hour: 0, Minute: 0, DayOfWeek: -1, DayOfMonth: -1,
	}); err == nil {
		t.Fatal("failed save must not report a created task")
	}
	if len(manager.List()) != 0 {
		t.Fatalf("failed save published %#v", manager.List())
	}
}

func TestFailedFireSaveKeepsTheDueTime(t *testing.T) {
	store := &rejectSaveStore{}
	manager, err := NewManagerFromStore(store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Stop)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	manager.SetExecutor(func(ctx context.Context, _ *ScheduledTask) (string, error) {
		started <- struct{}{}
		select {
		case <-release:
			return "ok", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	id, err := manager.Add(ScheduledTask{
		Name: "go", Action: "run", Hour: 9, Minute: 0, DayOfWeek: -1, DayOfMonth: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	task := manager.Get(id)
	if task == nil || task.NextRunAt == nil {
		t.Fatal("active row has no due time")
	}
	due := *task.NextRunAt
	store.reject = true
	manager.FireDue(due)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("due task did not start")
	}
	inflight := manager.Get(id)
	if inflight == nil || inflight.NextRunAt == nil || !inflight.NextRunAt.Equal(due) {
		t.Fatalf("claim published an unsaved schedule: %#v", inflight)
	}
	manager.mu.RLock()
	executor := manager.executor
	manager.mu.RUnlock()
	second := make(chan struct{})
	go func() {
		manager.fireByID(id, executor, due)
		close(second)
	}()
	select {
	case <-second:
	case <-time.After(time.Second):
		t.Fatal("second claim did not return while the first run held the lease")
	}
	select {
	case <-started:
		t.Fatal("second tick started a duplicate run")
	default:
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for manager.Inflight() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("run did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	got := manager.Get(id)
	if got == nil || got.Status != "active" || got.RunCount != 0 || got.NextRunAt == nil || !got.NextRunAt.Equal(due) {
		t.Fatalf("failed completion save stuck the row: %#v", got)
	}
	if ids := dueTaskIDs(manager.List(), due); len(ids) != 1 || ids[0] != id {
		t.Fatalf("row is not selectable after the failed save: %v", ids)
	}
}

func TestTickerDoesNotRunAClosedWindow(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Stop)
	fired := make(chan struct{}, 1)
	manager.SetExecutor(func(context.Context, *ScheduledTask) (string, error) {
		fired <- struct{}{}
		return "ok", nil
	})
	id, err := manager.Add(ScheduledTask{
		Name: "go", Action: "run", Hour: 9, Minute: 0, DayOfWeek: -1, DayOfMonth: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().AddDate(0, 0, -1)
	manager.mu.Lock()
	for i := range manager.tasks {
		if manager.tasks[i].ID != id {
			continue
		}
		manager.tasks[i].EndDate = past.Format("2006-01-02")
		due := past
		manager.tasks[i].NextRunAt = &due
	}
	manager.mu.Unlock()
	manager.FireDue(time.Now())
	select {
	case <-fired:
		t.Fatal("ticker ran a row whose end date had passed")
	default:
	}
	if manager.Get(id) != nil {
		t.Fatal("a closed window stayed on the list")
	}
}

func TestDeleteKeepsTheRowWhenSaveFails(t *testing.T) {
	store := &rejectSaveStore{}
	manager, err := NewManagerFromStore(store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Stop)
	id, err := manager.Add(ScheduledTask{
		Name: "go", Action: "run", Hour: 9, Minute: 0, DayOfWeek: -1, DayOfMonth: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	store.reject = true
	if err := manager.Delete(id); err == nil {
		t.Fatal("failed save must not report the row deleted")
	}
	kept := manager.Get(id)
	if kept == nil || kept.Name != "go" || kept.Status != "active" {
		t.Fatalf("failed delete published %#v", kept)
	}
	store.reject = false
	if err := manager.Delete(id); err != nil {
		t.Fatal(err)
	}
	if manager.Get(id) != nil {
		t.Fatal("delete must remove the row after the save")
	}
}

func TestScheduleWindowMatchesTheTickerAndAReload(t *testing.T) {
	past := time.Now().AddDate(0, 0, -2).Format("2006-01-02")
	store := &memoryTaskStore{tasks: []ScheduledTask{
		{ID: "paused-past", Name: "held", Action: "wait", Hour: 9, Status: "paused", EndDate: past, DayOfWeek: -1, DayOfMonth: -1},
		{ID: "active-past", Name: "due", Action: "run", Hour: 9, Status: "active", EndDate: past, DayOfWeek: -1, DayOfMonth: -1},
		{ID: "legacy", Name: "old", Action: "run", Hour: 9, Status: "expired", DayOfWeek: -1, DayOfMonth: -1},
	}}
	manager, err := NewManagerFromStore(store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Stop)
	if manager.Get("active-past") != nil || manager.Get("legacy") != nil {
		t.Fatalf("reload kept a closed row: %#v", manager.List())
	}
	paused := manager.Get("paused-past")
	if paused == nil || paused.Status != "paused" || paused.EndDate != past {
		t.Fatalf("reload dropped the paused row: %#v", paused)
	}
	manager.FireDue(time.Now())
	if manager.Get("paused-past") == nil {
		t.Fatal("the ticker removed a paused row with a passed end_date")
	}

	live, err := NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(live.Stop)
	activeID, err := live.Add(ScheduledTask{
		Name: "go", Action: "run", Hour: 9, Minute: 0, DayOfWeek: -1, DayOfMonth: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := live.Update(activeID, map[string]interface{}{"end_date": past}); err == nil || !strings.Contains(err.Error(), "has expired") {
		t.Fatalf("active past end_date err=%v", err)
	}
	if live.Get(activeID) != nil {
		t.Fatal("an active row must not stay stored after its window closed")
	}
	if _, err := live.Add(ScheduledTask{
		Name: "bad", Action: "run", Hour: 9, StartDate: "2026-12-01", EndDate: "2026-11-01", DayOfWeek: -1, DayOfMonth: -1,
	}); err == nil || !strings.Contains(err.Error(), "on or before") {
		t.Fatalf("inverted create err=%v", err)
	}
	pausedID, err := live.Add(ScheduledTask{
		Name: "held", Action: "wait", Hour: 9, Status: "paused", DayOfWeek: -1, DayOfMonth: -1,
		StartDate: "2026-12-01", EndDate: "2026-12-31",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := live.Update(pausedID, map[string]interface{}{"end_date": "2026/12/01"}); err == nil || !strings.Contains(err.Error(), "YYYY-MM-DD") {
		t.Fatalf("bad end_date err=%v", err)
	}
	if got := live.Get(pausedID); got == nil || got.EndDate != "2026-12-31" {
		t.Fatalf("rejected date published %#v", got)
	}
	if err := live.Update(pausedID, map[string]interface{}{"end_date": "2026-11-01"}); err == nil || !strings.Contains(err.Error(), "on or before") {
		t.Fatalf("inverted update err=%v", err)
	}
	if got := live.Get(pausedID); got == nil || got.EndDate != "2026-12-31" {
		t.Fatalf("inverted update published %#v", got)
	}
}

type countingTaskStore struct {
	memoryTaskStore
	saves int
}

func (s *countingTaskStore) SaveScheduledTasks(tasks []ScheduledTask) error {
	s.saves++
	return s.memoryTaskStore.SaveScheduledTasks(tasks)
}

type rejectSaveStore struct {
	memoryTaskStore
	reject bool
}

func (s *rejectSaveStore) SaveScheduledTasks(tasks []ScheduledTask) error {
	if s.reject {
		return fmt.Errorf("disk full")
	}
	return s.memoryTaskStore.SaveScheduledTasks(tasks)
}
