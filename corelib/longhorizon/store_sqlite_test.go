package longhorizon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTaskStateSQLiteRoundTrip(t *testing.T) {
	root := t.TempDir()
	state := &TaskState{
		TaskID:      "hz-1",
		UserGoal:    "ship it",
		Status:      StatusExecuting,
		RoundIndex:  2,
		MaxRounds:   25,
		ManagerNext: NextCLI,
		Policy:      PolicySnapshot{OwnerID: "u1", HorizonTaskID: "hz-1"},
		Rounds: []ManagedRound{{
			RoundIndex: 1,
			Next:       NextCLI,
			Goal:       "g",
			Acceptance: "a",
		}},
		Carryover: []string{"keep me"},
	}
	if err := SaveTaskState(root, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTaskState(root, "hz-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TaskID != "hz-1" || loaded.Status != StatusExecuting || len(loaded.Carryover) != 1 || loaded.Carryover[0] != "keep me" {
		t.Fatalf("loaded = %+v", loaded)
	}
	if _, err := os.Stat(horizonDBPath(root)); err != nil {
		t.Fatal(err)
	}
}

func TestTaskStateSQLiteFindIncomplete(t *testing.T) {
	root := t.TempDir()
	owner := "u1"
	old := &TaskState{
		TaskID: "hz-old", Status: StatusExecuting, RoundIndex: 1, MaxRounds: 25,
		Policy: PolicySnapshot{OwnerID: owner, HorizonTaskID: "hz-old"},
	}
	old.UpdatedAt = time.Now().UTC().Add(-time.Hour)
	if err := SaveTaskState(root, old); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	newer := &TaskState{
		TaskID: "hz-new", Status: StatusManaging, RoundIndex: 3, MaxRounds: 25,
		Policy:    PolicySnapshot{OwnerID: owner, HorizonTaskID: "hz-new"},
		Carryover: []string{"latest"},
	}
	if err := SaveTaskState(root, newer); err != nil {
		t.Fatal(err)
	}
	done := &TaskState{
		TaskID: "hz-done", Status: StatusDone, RoundIndex: 5, MaxRounds: 25, Completed: true,
		Policy: PolicySnapshot{OwnerID: owner, HorizonTaskID: "hz-done"},
	}
	if err := SaveTaskState(root, done); err != nil {
		t.Fatal(err)
	}
	other := &TaskState{
		TaskID: "hz-other", Status: StatusExecuting, RoundIndex: 1, MaxRounds: 25,
		Policy: PolicySnapshot{OwnerID: "u2", HorizonTaskID: "hz-other"},
	}
	if err := SaveTaskState(root, other); err != nil {
		t.Fatal(err)
	}

	got, err := FindIncompleteTask(root, owner)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.TaskID != "hz-new" {
		t.Fatalf("FindIncompleteTask = %+v", got)
	}
	if gotNone, err := FindIncompleteTask(root, "nobody"); err != nil || gotNone != nil {
		t.Fatalf("FindIncompleteTask(nobody) = %+v err=%v", gotNone, err)
	}
	if gotOther, err := FindIncompleteTask(root, "u2"); err != nil || gotOther == nil || gotOther.TaskID != "hz-other" {
		t.Fatalf("FindIncompleteTask(u2) = %+v err=%v", gotOther, err)
	}
}

func TestTaskStateSQLiteLegacyImport(t *testing.T) {
	root := t.TempDir()
	dir := TaskDir(root, "hz-legacy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := &TaskState{
		TaskID: "hz-legacy", Status: StatusExecuting, RoundIndex: 1, MaxRounds: 25,
		Policy:    PolicySnapshot{OwnerID: "u1", HorizonTaskID: "hz-legacy"},
		Carryover: []string{"from json"},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "task_state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadTaskState(root, "hz-legacy")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TaskID != "hz-legacy" || len(loaded.Carryover) != 1 || loaded.Carryover[0] != "from json" {
		t.Fatalf("legacy import = %+v", loaded)
	}
	if _, err := os.Stat(filepath.Join(dir, "task_state.json")); !os.IsNotExist(err) {
		t.Fatalf("legacy json should be removed, stat err=%v", err)
	}

	got, err := FindIncompleteTask(root, "u1")
	if err != nil || got == nil || got.TaskID != "hz-legacy" {
		t.Fatalf("FindIncompleteTask after import = %+v err=%v", got, err)
	}
}
