package agent

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestConfirmationStoreSQLiteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "confirmations.json")
	now := time.Date(2026, 5, 1, 1, 2, 3, 0, time.UTC)
	raw, err := json.Marshal(confirmationSnapshot{Items: map[string]*PendingConfirmation{
		"u": {
			ID: "c1", UserID: "u", OriginalText: "do it", Status: ConfirmationStatusPending,
			CreatedAt: now, UpdatedAt: now,
			TargetPaths:    []string{"a", "b"},
			PlannedActions: []string{"edit"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewConfirmationStore(path)
	got := store.Get("u")
	if got == nil || got.OriginalText != "do it" || len(got.TargetPaths) != 2 || got.TargetPaths[1] != "b" || len(got.PlannedActions) != 1 {
		t.Fatalf("imported = %+v", got)
	}
	if !got.CreatedAt.Equal(now) || !got.UpdatedAt.Equal(now) {
		t.Fatalf("timestamps = %v / %v", got.CreatedAt, got.UpdatedAt)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy json should be removed, stat err=%v", err)
	}
	dbPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".db"
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatal(err)
	}
	reloaded := NewConfirmationStore(path)
	if reloaded.Get("u") == nil || reloaded.Get("u").ID != "c1" {
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
	kept := NewConfirmationStore(path)
	if kept.Get("u") == nil || kept.Get("u").OriginalText != "do it" {
		t.Fatal("corrupt json replaced sqlite rows")
	}

	dirStore := NewConfirmationStore(t.TempDir())
	if dirStore.Get("nobody") != nil {
		t.Fatal("directory store should start empty")
	}
	dirStore.Set(&PendingConfirmation{UserID: "bob", OriginalText: "later", Status: ConfirmationStatusPending, TargetPaths: []string{"x"}})
	if got := NewConfirmationStore(dirStore.storePath).Get("bob"); got == nil || got.OriginalText != "later" || len(got.TargetPaths) != 1 {
		t.Fatalf("directory store reload = %+v", got)
	}

	dirStore.Clear("bob")
	if NewConfirmationStore(dirStore.storePath).Get("bob") != nil {
		t.Fatal("clear did not remove the sqlite row")
	}
}

func TestConfirmationSaveLeavesUnreadLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "confirmations.json")
	now := time.Date(2026, 5, 1, 1, 2, 3, 0, time.UTC)
	raw, err := json.Marshal(confirmationSnapshot{Items: map[string]*PendingConfirmation{
		"u": {ID: "c1", UserID: "u", OriginalText: "do it", Status: ConfirmationStatusPending, CreatedAt: now, UpdatedAt: now},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveConfirmations(path, map[string]*PendingConfirmation{
		"bob": {UserID: "bob", OriginalText: "new", Status: ConfirmationStatusPending},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("save removed unread legacy json: %v", err)
	}
	imported := NewConfirmationStore(path)
	if got := imported.Get("u"); got == nil || got.OriginalText != "do it" {
		t.Fatalf("import after unread save = %+v", got)
	}
	if imported.Get("bob") != nil {
		t.Fatal("unread save outranked the legacy file")
	}
}
