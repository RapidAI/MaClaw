package session

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDeleteSessionRequiresExistingRow(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Delete("missing"); err == nil {
		t.Fatal("deleting a missing session should fail")
	}
	if err := store.Persist(SessionDocument{SessionID: "present", Timestamp: time.Now(), Platform: "gui", FullText: "content"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("present"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("present"); err == nil {
		t.Fatal("deleting the same session twice should fail")
	}
}
