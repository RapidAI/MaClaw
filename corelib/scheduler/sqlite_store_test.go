package scheduler

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestImportLegacyTasksOnceDoesNotResurrectClearedTasks(t *testing.T) {
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
	store, err := OpenSQLiteTaskStore(scheduleDBPath(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.close)
	n, err := ImportLegacyTasksOnce(path, store)
	if err != nil || n != 1 {
		t.Fatalf("import n=%d err=%v", n, err)
	}
	if err := store.SaveScheduledTasks(nil); err != nil {
		t.Fatal(err)
	}
	again, err := ImportLegacyTasksOnce(path, store)
	if err != nil || again != 0 {
		t.Fatalf("second import n=%d err=%v", again, err)
	}
	loaded, err := store.LoadScheduledTasks()
	if err != nil || len(loaded) != 0 {
		t.Fatalf("cleared tasks came back: %+v err=%v", loaded, err)
	}
	kept, err := os.ReadFile(path)
	if err != nil || string(kept) != string(raw) {
		t.Fatal("scheduled_tasks.json was rewritten or removed")
	}
}

func TestScheduledTaskJSONColumnRebuildsToColumns(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "scheduled_tasks.db")
	legacyJSON := filepath.Join(dir, "scheduled_tasks.json")
	if err := os.WriteFile(legacyJSON, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 5, 2, 3, 4, 5, 0, time.UTC)
	task := ScheduledTask{
		ID: "morning", Name: "Morning", Action: "read memory",
		Hour: 8, Minute: 15, DayOfWeek: -1, DayOfMonth: -1, Status: "active",
		CreatedAt: created,
		Delivery: &TaskDelivery{
			Enabled: true, Channel: "lansenger", On: "success",
			Targets: []DeliveryTarget{{
				Kind: "group", GroupID: "g1", GroupName: "ops", MentionAll: true,
				MentionUserIDs: []string{"u1", "u2"},
			}},
		},
	}
	raw, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE scheduled_tasks (
		id TEXT PRIMARY KEY,
		position INTEGER NOT NULL,
		task_json BLOB NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO scheduled_tasks(id, position, task_json, updated_at) VALUES(?,?,?,?)`, task.ID, 3, raw, "stamp"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := OpenSQLiteTaskStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.close)
	loaded, err := store.LoadScheduledTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].ID != "morning" || loaded[0].DayOfWeek != -1 || loaded[0].Action != "read memory" {
		t.Fatalf("rebuilt task = %+v", loaded)
	}
	if loaded[0].Delivery == nil || !loaded[0].Delivery.Enabled || len(loaded[0].Delivery.Targets) != 1 {
		t.Fatalf("delivery = %+v", loaded[0].Delivery)
	}
	target := loaded[0].Delivery.Targets[0]
	if target.GroupID != "g1" || !target.MentionAll || len(target.MentionUserIDs) != 2 || target.MentionUserIDs[0] != "u1" {
		t.Fatalf("target = %+v", target)
	}
	checkDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer checkDB.Close()
	cols, err := sqliteTableColumns(checkDB, "scheduled_tasks")
	if err != nil {
		t.Fatal(err)
	}
	if cols["task_json"] || !cols["action"] || !cols["day_of_week"] {
		t.Fatalf("columns = %#v", cols)
	}
	kept, err := os.ReadFile(legacyJSON)
	if err != nil || string(kept) != "[]" {
		t.Fatal("scheduled_tasks.json was rewritten or removed")
	}
}

func TestSaveScheduledTaskRowsDuplicateIDKeepsLastInPlace(t *testing.T) {
	db := openSchedulerTestDB(t)
	tasks := []ScheduledTask{
		{
			ID: "a", Name: "old", Action: "one", DayOfWeek: -1, DayOfMonth: -1,
			Delivery: &TaskDelivery{Enabled: true, Channel: "lansenger", Targets: []DeliveryTarget{{Kind: "user", UserID: "old-user"}}},
		},
		{ID: "b", Name: "keep", Action: "two", DayOfWeek: -1, DayOfMonth: -1},
		{
			ID: "a", Name: "new", Action: "three", DayOfWeek: -1, DayOfMonth: -1,
			Delivery: &TaskDelivery{Enabled: true, Channel: "lansenger", Targets: []DeliveryTarget{{Kind: "user", UserID: "new-user"}}},
		},
	}
	if err := SaveScheduledTaskRows(db, tasks, true); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadScheduledTaskRows(db, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 || loaded[0].ID != "a" || loaded[0].Name != "new" || loaded[0].Action != "three" || loaded[1].ID != "b" {
		t.Fatalf("loaded = %+v", loaded)
	}
	if loaded[0].Delivery == nil || len(loaded[0].Delivery.Targets) != 1 || loaded[0].Delivery.Targets[0].UserID != "new-user" {
		t.Fatalf("delivery = %+v", loaded[0].Delivery)
	}
}

func TestScheduledTaskJSONRebuildKeepsLastDuplicateID(t *testing.T) {
	db := openSchedulerTestDB(t)
	if _, err := db.Exec(`CREATE TABLE scheduled_tasks (
		id TEXT PRIMARY KEY,
		position INTEGER NOT NULL,
		task_json BLOB NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	insert := func(rowID string, position int, task ScheduledTask, stamp string) {
		t.Helper()
		raw, err := json.Marshal(task)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO scheduled_tasks(id, position, task_json, updated_at) VALUES(?,?,?,?)`, rowID, position, raw, stamp); err != nil {
			t.Fatal(err)
		}
	}
	insert("r0", 0, ScheduledTask{ID: "same", Name: "old", Action: "a", DayOfWeek: -1, DayOfMonth: -1}, "stamp-old")
	insert("r1", 1, ScheduledTask{ID: "other", Name: "keep", Action: "c", DayOfWeek: -1, DayOfMonth: -1}, "stamp-other")
	insert("r2", 2, ScheduledTask{ID: "same", Name: "new", Action: "b", DayOfWeek: -1, DayOfMonth: -1}, "stamp-new")

	loaded, err := LoadScheduledTaskRows(db, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 || loaded[0].ID != "same" || loaded[0].Name != "new" || loaded[0].Action != "b" || loaded[1].ID != "other" {
		t.Fatalf("rebuilt = %+v", loaded)
	}
	cols, err := sqliteTableColumns(db, "scheduled_tasks")
	if err != nil {
		t.Fatal(err)
	}
	if cols["task_json"] {
		t.Fatal("task_json column survived a duplicate-id rebuild")
	}
}

func openSchedulerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "scheduled_tasks.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}
