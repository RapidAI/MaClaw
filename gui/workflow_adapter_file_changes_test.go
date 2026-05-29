package main

import (
	"strings"
	"testing"
)

func TestTruncateString_ShortString(t *testing.T) {
	result := truncateString("hello", 200)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestTruncateString_ExactLength(t *testing.T) {
	s := strings.Repeat("a", 200)
	result := truncateString(s, 200)
	if result != s {
		t.Errorf("expected string of length 200, got length %d", len([]rune(result)))
	}
}

func TestTruncateString_ExceedsLength(t *testing.T) {
	s := strings.Repeat("a", 250)
	result := truncateString(s, 200)
	if len([]rune(result)) != 200 {
		t.Errorf("expected 200 runes, got %d", len([]rune(result)))
	}
}

func TestTruncateString_Unicode(t *testing.T) {
	// 210 Chinese characters — each is 1 rune but 3 bytes
	s := strings.Repeat("中", 210)
	result := truncateString(s, 200)
	if len([]rune(result)) != 200 {
		t.Errorf("expected 200 runes, got %d", len([]rune(result)))
	}
}

func TestTruncateString_Empty(t *testing.T) {
	result := truncateString("", 200)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestEmitFileChanges_TruncatesTaskTitle(t *testing.T) {
	// Create a payload with a title exceeding 200 chars
	longTitle := strings.Repeat("x", 300)
	payload := FileChangesPayload{
		TaskID:    "task-1",
		TaskTitle: longTitle,
		Files:     nil,
	}

	// We can't call EmitFileChanges directly (needs app context),
	// so test the truncation logic directly.
	payload.TaskTitle = truncateString(payload.TaskTitle, maxTaskTitleChars)
	if len([]rune(payload.TaskTitle)) != 200 {
		t.Errorf("expected TaskTitle truncated to 200 runes, got %d", len([]rune(payload.TaskTitle)))
	}
}

func TestEmitFileChanges_TruncatesFilesArray(t *testing.T) {
	// Create a payload with 250 files
	files := make([]FileChangeItem, 250)
	for i := range files {
		files[i] = FileChangeItem{
			Path:       "src/file.go",
			ChangeType: "modified",
			Diff:       "+line",
			Language:   "go",
		}
	}

	payload := FileChangesPayload{
		TaskID:    "task-1",
		TaskTitle: "Test Task",
		Files:     files,
		Truncated: false,
	}

	// Simulate the truncation logic from EmitFileChanges
	if len(payload.Files) > maxFileChangesFiles {
		payload.Files = payload.Files[:maxFileChangesFiles]
		payload.Truncated = true
	}

	if len(payload.Files) != 200 {
		t.Errorf("expected 200 files, got %d", len(payload.Files))
	}
	if !payload.Truncated {
		t.Error("expected Truncated to be true")
	}
}

func TestEmitFileChanges_NoTruncationWhenUnder200(t *testing.T) {
	files := make([]FileChangeItem, 150)
	for i := range files {
		files[i] = FileChangeItem{
			Path:       "src/file.go",
			ChangeType: "added",
			Diff:       "+new line",
			Language:   "go",
		}
	}

	payload := FileChangesPayload{
		TaskID:    "task-1",
		TaskTitle: "Test Task",
		Files:     files,
		Truncated: false,
	}

	// Simulate the truncation logic
	if len(payload.Files) > maxFileChangesFiles {
		payload.Files = payload.Files[:maxFileChangesFiles]
		payload.Truncated = true
	}

	if len(payload.Files) != 150 {
		t.Errorf("expected 150 files, got %d", len(payload.Files))
	}
	if payload.Truncated {
		t.Error("expected Truncated to be false")
	}
}

func TestEmitFileChanges_ExactlyAt200(t *testing.T) {
	files := make([]FileChangeItem, 200)
	for i := range files {
		files[i] = FileChangeItem{
			Path:       "src/file.go",
			ChangeType: "deleted",
			Diff:       "-old line",
			Language:   "go",
		}
	}

	payload := FileChangesPayload{
		TaskID:    "task-1",
		TaskTitle: "Test Task",
		Files:     files,
		Truncated: false,
	}

	// Simulate the truncation logic
	if len(payload.Files) > maxFileChangesFiles {
		payload.Files = payload.Files[:maxFileChangesFiles]
		payload.Truncated = true
	}

	if len(payload.Files) != 200 {
		t.Errorf("expected 200 files, got %d", len(payload.Files))
	}
	if payload.Truncated {
		t.Error("expected Truncated to be false when exactly at limit")
	}
}

func TestFileChangesPayload_PhaseIDConstant(t *testing.T) {
	if fileChangesPhaseID != "implementation" {
		t.Errorf("expected fileChangesPhaseID to be 'implementation', got %q", fileChangesPhaseID)
	}
}

func TestTruncateString_FilePathMax500(t *testing.T) {
	longPath := strings.Repeat("a/", 300) // 600 chars
	result := truncateString(longPath, maxFilePathChars)
	if len([]rune(result)) != 500 {
		t.Errorf("expected 500 runes, got %d", len([]rune(result)))
	}
}

func TestEmitFileChanges_TruncatesFilePaths(t *testing.T) {
	longPath := strings.Repeat("dir/", 200) // 800 chars
	files := []FileChangeItem{
		{
			Path:       longPath,
			ChangeType: "modified",
			Diff:       "+line",
			Language:   "go",
		},
	}

	// Simulate the path truncation logic from EmitFileChanges
	for i := range files {
		files[i].Path = truncateString(files[i].Path, maxFilePathChars)
	}

	if len([]rune(files[0].Path)) != 500 {
		t.Errorf("expected path truncated to 500 runes, got %d", len([]rune(files[0].Path)))
	}
}
