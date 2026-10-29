package scheduler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNewManagerImportsLegacyJSONAndReloadsFromSQLite(t *testing.T) {
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

	manager, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Stop)
	list := manager.List()
	if len(list) != 1 || list[0].ID != "legacy-1" || list[0].Hour != 8 {
		t.Fatalf("imported rows = %+v", list)
	}
	kept, err := os.ReadFile(path)
	if err != nil || string(kept) != string(raw) {
		t.Fatal("scheduled_tasks.json was rewritten or removed")
	}
	if _, err := manager.Add(ScheduledTask{
		Name: "noon", Action: "check mail", Hour: 12, Minute: 30, DayOfWeek: -1, DayOfMonth: -1,
	}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reloaded.Stop)
	if len(reloaded.List()) != 2 {
		t.Fatalf("reloaded rows = %+v", reloaded.List())
	}
	kept, err = os.ReadFile(path)
	if err != nil || string(kept) != string(raw) {
		t.Fatal("a later save wrote back to the pre-migration file")
	}
}

func TestNewManagerDoesNotResurrectClearedTasks(t *testing.T) {
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
	manager, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.ClearAll(); err != nil {
		t.Fatal(err)
	}
	manager.Stop()

	reloaded, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reloaded.Stop)
	if len(reloaded.List()) != 0 {
		t.Fatalf("cleared tasks came back from scheduled_tasks.json: %+v", reloaded.List())
	}
	kept, err := os.ReadFile(path)
	if err != nil || string(kept) != string(raw) {
		t.Fatal("scheduled_tasks.json was rewritten or removed")
	}
}
