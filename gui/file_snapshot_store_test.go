package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSnapshotStore_CaptureSnapshots_BasicRead(t *testing.T) {
	dir := t.TempDir()

	// Create a test file.
	content := "hello world\nline two\n"
	filePath := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSnapshotStore(50)
	store.CaptureSnapshots(dir, []string{"test.txt"})

	absPath := filepath.Clean(filepath.Join(dir, "test.txt"))
	snap, ok := store.GetSnapshot(absPath)
	if !ok {
		t.Fatal("expected snapshot to exist")
	}
	if snap.Content != content {
		t.Errorf("content mismatch: got %q, want %q", snap.Content, content)
	}
	if snap.Error != "" {
		t.Errorf("unexpected error: %s", snap.Error)
	}
	if snap.CapturedAt.IsZero() {
		t.Error("CapturedAt should not be zero")
	}
}

func TestFileSnapshotStore_CaptureSnapshots_AbsolutePath(t *testing.T) {
	dir := t.TempDir()

	content := "absolute path test"
	filePath := filepath.Join(dir, "abs.txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSnapshotStore(50)
	// Pass absolute path directly.
	store.CaptureSnapshots(dir, []string{filePath})

	snap, ok := store.GetSnapshot(filePath)
	if !ok {
		t.Fatal("expected snapshot to exist for absolute path")
	}
	if snap.Content != content {
		t.Errorf("content mismatch: got %q, want %q", snap.Content, content)
	}
}

func TestFileSnapshotStore_CaptureSnapshots_NotFound(t *testing.T) {
	dir := t.TempDir()

	store := NewFileSnapshotStore(50)
	store.CaptureSnapshots(dir, []string{"nonexistent.txt"})

	absPath := filepath.Clean(filepath.Join(dir, "nonexistent.txt"))
	snap, ok := store.GetSnapshot(absPath)
	if !ok {
		t.Fatal("expected snapshot entry to exist (with error)")
	}
	if snap.Error != "not_found" {
		t.Errorf("expected error 'not_found', got %q", snap.Error)
	}
	if snap.Content != "" {
		t.Errorf("expected empty content for not_found, got %q", snap.Content)
	}
}

func TestFileSnapshotStore_CaptureSnapshots_FileTooLarge(t *testing.T) {
	dir := t.TempDir()

	// Create a file just over 2MB.
	filePath := filepath.Join(dir, "large.bin")
	largeContent := make([]byte, maxSnapshotFileSize+1)
	for i := range largeContent {
		largeContent[i] = 'A'
	}
	if err := os.WriteFile(filePath, largeContent, 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSnapshotStore(50)
	store.CaptureSnapshots(dir, []string{"large.bin"})

	absPath := filepath.Clean(filepath.Join(dir, "large.bin"))
	snap, ok := store.GetSnapshot(absPath)
	if !ok {
		t.Fatal("expected snapshot entry to exist (with error)")
	}
	if snap.Error != "file_too_large" {
		t.Errorf("expected error 'file_too_large', got %q", snap.Error)
	}
}

func TestFileSnapshotStore_CaptureSnapshots_BinaryFile(t *testing.T) {
	dir := t.TempDir()

	// Create a file with null bytes (binary).
	filePath := filepath.Join(dir, "binary.dat")
	binaryContent := []byte("hello\x00world")
	if err := os.WriteFile(filePath, binaryContent, 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSnapshotStore(50)
	store.CaptureSnapshots(dir, []string{"binary.dat"})

	absPath := filepath.Clean(filepath.Join(dir, "binary.dat"))
	snap, ok := store.GetSnapshot(absPath)
	if !ok {
		t.Fatal("expected snapshot entry to exist (with error)")
	}
	if snap.Error != "binary" {
		t.Errorf("expected error 'binary', got %q", snap.Error)
	}
}

func TestFileSnapshotStore_CaptureSnapshots_MaxFilesLimit(t *testing.T) {
	dir := t.TempDir()

	// Create 60 files but set maxFiles to 5.
	for i := 0; i < 60; i++ {
		name := filepath.Join(dir, strings.Replace("file_XX.txt", "XX", string(rune('A'+i%26))+string(rune('0'+i/26)), 1))
		if err := os.WriteFile(name, []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Generate file paths.
	var paths []string
	for i := 0; i < 60; i++ {
		name := strings.Replace("file_XX.txt", "XX", string(rune('A'+i%26))+string(rune('0'+i/26)), 1)
		paths = append(paths, name)
	}

	store := NewFileSnapshotStore(5)
	store.CaptureSnapshots(dir, paths)

	if store.Len() != 5 {
		t.Errorf("expected 5 snapshots, got %d", store.Len())
	}
}

func TestFileSnapshotStore_CaptureSnapshots_DefaultMaxFiles(t *testing.T) {
	store := NewFileSnapshotStore(0)
	if store.maxFiles != defaultMaxSnapshotFiles {
		t.Errorf("expected default maxFiles=%d, got %d", defaultMaxSnapshotFiles, store.maxFiles)
	}
}

func TestFileSnapshotStore_CaptureSnapshots_SkipDuplicates(t *testing.T) {
	dir := t.TempDir()

	filePath := filepath.Join(dir, "dup.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSnapshotStore(50)
	// Pass the same file twice.
	store.CaptureSnapshots(dir, []string{"dup.txt", "dup.txt"})

	if store.Len() != 1 {
		t.Errorf("expected 1 snapshot (deduped), got %d", store.Len())
	}
}

func TestFileSnapshotStore_GetSnapshot_NotCaptured(t *testing.T) {
	store := NewFileSnapshotStore(50)

	_, ok := store.GetSnapshot("/some/path/that/was/never/captured.txt")
	if ok {
		t.Error("expected ok=false for uncaptured path")
	}
}

func TestFileSnapshotStore_Clear(t *testing.T) {
	dir := t.TempDir()

	filePath := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSnapshotStore(50)
	store.CaptureSnapshots(dir, []string{"file.txt"})

	if store.Len() != 1 {
		t.Fatalf("expected 1 snapshot before clear, got %d", store.Len())
	}

	store.Clear()

	if store.Len() != 0 {
		t.Errorf("expected 0 snapshots after clear, got %d", store.Len())
	}

	absPath := filepath.Clean(filepath.Join(dir, "file.txt"))
	_, ok := store.GetSnapshot(absPath)
	if ok {
		t.Error("expected snapshot to be gone after Clear()")
	}
}

func TestFileSnapshotStore_CaptureSnapshots_FileAtExact2MB(t *testing.T) {
	dir := t.TempDir()

	// Create a file exactly at 2MB — should be captured (limit is > 2MB).
	filePath := filepath.Join(dir, "exact2mb.txt")
	content := make([]byte, maxSnapshotFileSize)
	for i := range content {
		content[i] = 'B'
	}
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSnapshotStore(50)
	store.CaptureSnapshots(dir, []string{"exact2mb.txt"})

	absPath := filepath.Clean(filepath.Join(dir, "exact2mb.txt"))
	snap, ok := store.GetSnapshot(absPath)
	if !ok {
		t.Fatal("expected snapshot to exist for exactly 2MB file")
	}
	if snap.Error != "" {
		t.Errorf("expected no error for exactly 2MB file, got %q", snap.Error)
	}
	if len(snap.Content) != maxSnapshotFileSize {
		t.Errorf("expected content length %d, got %d", maxSnapshotFileSize, len(snap.Content))
	}
}

func TestFileSnapshotStore_CaptureSnapshots_EmptyFileList(t *testing.T) {
	dir := t.TempDir()

	store := NewFileSnapshotStore(50)
	store.CaptureSnapshots(dir, nil)

	if store.Len() != 0 {
		t.Errorf("expected 0 snapshots for nil file list, got %d", store.Len())
	}

	store.CaptureSnapshots(dir, []string{})

	if store.Len() != 0 {
		t.Errorf("expected 0 snapshots for empty file list, got %d", store.Len())
	}
}

func TestFileSnapshotStore_CaptureSnapshots_EmptyFile(t *testing.T) {
	dir := t.TempDir()

	filePath := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(filePath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSnapshotStore(50)
	store.CaptureSnapshots(dir, []string{"empty.txt"})

	absPath := filepath.Clean(filepath.Join(dir, "empty.txt"))
	snap, ok := store.GetSnapshot(absPath)
	if !ok {
		t.Fatal("expected snapshot to exist for empty file")
	}
	if snap.Error != "" {
		t.Errorf("expected no error for empty file, got %q", snap.Error)
	}
	if snap.Content != "" {
		t.Errorf("expected empty content, got %q", snap.Content)
	}
}
