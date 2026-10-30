package guiapp

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfirmationStoreSQLiteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ai_assistant_confirmation.json")
	now := time.Date(2026, 5, 1, 1, 2, 3, 0, time.UTC)
	raw, err := json.Marshal(confirmationSnapshot{Items: map[string]*pendingConfirmation{
		"u": {
			ID: "c1", UserID: "u", OriginalText: "do it", Status: confirmationStatusPending,
			CreatedAt: now, UpdatedAt: now,
			TargetPaths:    []string{"a", "b"},
			WorkflowGoals:  []string{"ship"},
			PlannedActions: []string{"edit"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	store := newAIConfirmationStore(path)
	got := store.get("u")
	if got == nil || got.OriginalText != "do it" || len(got.TargetPaths) != 2 || got.TargetPaths[1] != "b" || len(got.WorkflowGoals) != 1 || len(got.PlannedActions) != 1 {
		t.Fatalf("imported = %+v", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy json should be removed, stat err=%v", err)
	}
	dbPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".db"
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatal(err)
	}
	reloaded := newAIConfirmationStore(path)
	if reloaded.get("u") == nil || reloaded.get("u").ID != "c1" {
		t.Fatal("reload missed the sqlite row")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM confirmation_meta WHERE key = 'legacy_import'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	kept := newAIConfirmationStore(path)
	if kept.get("u") == nil || kept.get("u").OriginalText != "do it" {
		t.Fatal("corrupt json replaced sqlite rows")
	}

	dirStore := newAIConfirmationStore(t.TempDir())
	if dirStore.get("nobody") != nil {
		t.Fatal("directory store should start empty")
	}
	dirStore.set(&pendingConfirmation{UserID: "bob", OriginalText: "later", Status: confirmationStatusPending, TargetPaths: []string{"x"}})
	if got := newAIConfirmationStore(dirStore.storePath).get("bob"); got == nil || got.OriginalText != "later" || len(got.TargetPaths) != 1 {
		t.Fatalf("directory store reload = %+v", got)
	}
}

func TestConfirmationSaveLeavesUnreadLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ai_assistant_confirmation.json")
	now := time.Date(2026, 5, 1, 1, 2, 3, 0, time.UTC)
	raw, err := json.Marshal(confirmationSnapshot{Items: map[string]*pendingConfirmation{
		"u": {ID: "c1", UserID: "u", OriginalText: "do it", Status: confirmationStatusPending, CreatedAt: now, UpdatedAt: now},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveConfirmations(path, map[string]*pendingConfirmation{
		"bob": {UserID: "bob", OriginalText: "new", Status: confirmationStatusPending},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("save removed unread legacy json: %v", err)
	}
	imported := newAIConfirmationStore(path)
	if got := imported.get("u"); got == nil || got.OriginalText != "do it" {
		t.Fatalf("import after unread save = %+v", got)
	}
	if imported.get("bob") != nil {
		t.Fatal("unread save outranked the legacy file")
	}
}
