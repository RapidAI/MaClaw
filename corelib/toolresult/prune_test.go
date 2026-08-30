package toolresult

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneOlderThanRemovesStaleHandlesAndEmptyDirs(t *testing.T) {
	root := t.TempDir()

	staleDir := filepath.Join(root, "session-old")
	freshDir := filepath.Join(root, "session-fresh")
	emptyStaleDir := filepath.Join(root, "session-emptied")
	for _, dir := range []string{staleDir, freshDir, emptyStaleDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	staleFile := filepath.Join(staleDir, "old.txt")
	freshFile := filepath.Join(freshDir, "new.txt")
	emptiedFile := filepath.Join(emptyStaleDir, "gone.txt")
	for _, f := range []string{staleFile, freshFile, emptiedFile} {
		if err := os.WriteFile(f, []byte("payload"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-20 * 24 * time.Hour)
	for _, f := range []string{staleFile, emptiedFile} {
		if err := os.Chtimes(f, old, old); err != nil {
			t.Fatal(err)
		}
	}

	result, err := PruneOlderThan(root, 14*24*time.Hour)
	if err != nil {
		t.Fatalf("PruneOlderThan: %v", err)
	}
	if result.RemovedFiles != 2 {
		t.Fatalf("RemovedFiles = %d, want 2", result.RemovedFiles)
	}
	if result.FreedBytes != int64(2*len("payload")) {
		t.Fatalf("FreedBytes = %d, want %d", result.FreedBytes, 2*len("payload"))
	}
	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Fatalf("stale handle should be removed")
	}
	if _, err := os.Stat(freshFile); err != nil {
		t.Fatalf("fresh handle should be kept: %v", err)
	}
	// staleDir and emptyStaleDir are now empty and must be gone; freshDir and
	// root must stay.
	for _, dir := range []string{staleDir, emptyStaleDir} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("emptied session dir should be removed: %s", dir)
		}
	}
	if _, err := os.Stat(freshDir); err != nil {
		t.Fatalf("non-empty session dir should be kept: %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("root must never be removed: %v", err)
	}
}

func TestPruneOlderThanMissingRootIsNotAnError(t *testing.T) {
	result, err := PruneOlderThan(filepath.Join(t.TempDir(), "does-not-exist"), time.Hour)
	if err != nil {
		t.Fatalf("missing root should not error: %v", err)
	}
	if result.RemovedFiles != 0 {
		t.Fatalf("RemovedFiles = %d, want 0", result.RemovedFiles)
	}
}

func TestPruneOlderThanNonPositiveAgeIsNoOp(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "h.txt")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := PruneOlderThan(root, 0)
	if err != nil {
		t.Fatalf("PruneOlderThan: %v", err)
	}
	if result.RemovedFiles != 0 {
		t.Fatalf("RemovedFiles = %d, want 0", result.RemovedFiles)
	}
	if _, err := os.Stat(f); err != nil {
		t.Fatalf("no-op prune must keep files: %v", err)
	}
}
