package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractJSONStringFieldFromRaw_Complete(t *testing.T) {
	raw := `{"path": "test.html", "content": "hello world", "mode": "overwrite"}`
	if got := extractJSONStringFieldFromRaw(raw, "path"); got != "test.html" {
		t.Fatalf("path: expected %q, got %q", "test.html", got)
	}
	if got := extractJSONStringFieldFromRaw(raw, "content"); got != "hello world" {
		t.Fatalf("content: expected %q, got %q", "hello world", got)
	}
	if got := extractJSONStringFieldFromRaw(raw, "mode"); got != "overwrite" {
		t.Fatalf("mode: expected %q, got %q", "overwrite", got)
	}
}

func TestExtractJSONStringFieldFromRaw_Truncated(t *testing.T) {
	// Simulates a JSON arg truncated mid-content (typical write_file truncation)
	raw := `{"path": "index.html", "content": "<!DOCTYPE html>\n<html>\n<head>\n<title>Test</title>\n</head>\n<body>\n<h1>Hel`
	path := extractJSONStringFieldFromRaw(raw, "path")
	if path != "index.html" {
		t.Fatalf("path: expected %q, got %q", "index.html", path)
	}
	content := extractJSONStringFieldFromRaw(raw, "content")
	if !strings.HasPrefix(content, "<!DOCTYPE html>") {
		t.Fatalf("content should start with DOCTYPE, got %q", content[:min(50, len(content))])
	}
	if !strings.HasSuffix(content, "<h1>Hel") {
		t.Fatalf("content should end with truncated text, got suffix %q", content[max(0, len(content)-20):])
	}
}

func TestExtractJSONStringFieldFromRaw_Escapes(t *testing.T) {
	raw := `{"path": "dir\\file.txt", "content": "line1\nline2\ttab"}`
	if got := extractJSONStringFieldFromRaw(raw, "path"); got != "dir\\file.txt" {
		t.Fatalf("path: expected %q, got %q", "dir\\file.txt", got)
	}
	content := extractJSONStringFieldFromRaw(raw, "content")
	if !strings.Contains(content, "\n") || !strings.Contains(content, "\t") {
		t.Fatalf("content should contain unescaped newline and tab, got %q", content)
	}
}

func TestExtractJSONStringFieldFromRaw_MissingField(t *testing.T) {
	raw := `{"path": "test.txt"}`
	if got := extractJSONStringFieldFromRaw(raw, "content"); got != "" {
		t.Fatalf("expected empty for missing field, got %q", got)
	}
}

func TestAttemptLoopPartialWriteFile_Success(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "output.html")
	// Build a truncated JSON with a long enough content
	content := strings.Repeat("A", 100)
	raw := `{"path": "` + strings.ReplaceAll(targetPath, `\`, `\\`) + `", "content": "` + content + `"`
	// note: no closing } — simulates truncation

	result := attemptLoopPartialWriteFile(raw)
	if result == nil {
		t.Fatal("expected partial write to succeed")
	}
	if result.Path != targetPath {
		t.Fatalf("path: expected %q, got %q", targetPath, result.Path)
	}
	if result.BytesWritten != 100 {
		t.Fatalf("bytes: expected 100, got %d", result.BytesWritten)
	}

	// Verify file was actually written
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(data) != content {
		t.Fatalf("file content mismatch")
	}
}

func TestAttemptLoopPartialWriteFile_RefusesOverwriteWithShorterContent(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "existing.txt")
	// Existing file has 200 bytes.
	os.WriteFile(targetPath, []byte(strings.Repeat("X", 200)), 0o644)

	// New truncated content is only 100 bytes — should refuse (regression).
	content := strings.Repeat("B", 100)
	raw := `{"path": "` + strings.ReplaceAll(targetPath, `\`, `\\`) + `", "content": "` + content + `"}`

	result := attemptLoopPartialWriteFile(raw)
	if result != nil {
		t.Fatal("should refuse to overwrite with shorter truncated content")
	}

	// Verify original content preserved
	data, _ := os.ReadFile(targetPath)
	if len(data) != 200 {
		t.Fatalf("existing file was modified, got %d bytes", len(data))
	}
}

func TestAttemptLoopPartialWriteFile_OverwritesWithLongerContent(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "partial.txt")
	// Existing file has 100 bytes from a previous partial write.
	os.WriteFile(targetPath, []byte(strings.Repeat("X", 100)), 0o644)

	// New truncated content is 200 bytes — should overwrite (more progress).
	content := strings.Repeat("Y", 200)
	raw := `{"path": "` + strings.ReplaceAll(targetPath, `\`, `\\`) + `", "content": "` + content + `"}`

	result := attemptLoopPartialWriteFile(raw)
	if result == nil {
		t.Fatal("should allow overwrite with longer truncated content")
	}
	if result.BytesWritten != 200 {
		t.Fatalf("expected 200 bytes written, got %d", result.BytesWritten)
	}

	// Verify new content replaced old
	data, _ := os.ReadFile(targetPath)
	if string(data) != content {
		t.Fatal("file not overwritten with new content")
	}
}

func TestAttemptLoopPartialWriteFile_AppendMode(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "append.txt")
	os.WriteFile(targetPath, []byte("first-"), 0o644)

	content := strings.Repeat("C", 100)
	raw := `{"path": "` + strings.ReplaceAll(targetPath, `\`, `\\`) + `", "content": "` + content + `", "mode": "append"}`

	result := attemptLoopPartialWriteFile(raw)
	if result == nil {
		t.Fatal("expected append partial write to succeed")
	}

	data, _ := os.ReadFile(targetPath)
	if !strings.HasPrefix(string(data), "first-") {
		t.Fatal("append should preserve existing content")
	}
	if len(data) != 6+100 {
		t.Fatalf("expected %d bytes, got %d", 106, len(data))
	}
}

func TestAttemptLoopPartialWriteFile_TooShort(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "short.txt")
	raw := `{"path": "` + strings.ReplaceAll(targetPath, `\`, `\\`) + `", "content": "hi"}`

	result := attemptLoopPartialWriteFile(raw)
	if result != nil {
		t.Fatal("should refuse content shorter than minimum")
	}
}

func TestTruncatedToolArgsLookup_ExactMatch(t *testing.T) {
	args := map[string]string{"write_file": `{"path":"x","content":"abc`}
	if got := truncatedToolArgsLookup(args, "write_file"); got == "" {
		t.Fatal("expected exact match to find args")
	}
}

func TestTruncatedToolArgsLookup_TrimmedKey(t *testing.T) {
	args := map[string]string{" write_file ": `{"path":"x","content":"abc`}
	if got := truncatedToolArgsLookup(args, "write_file"); got == "" {
		t.Fatal("expected trimmed key lookup to find args")
	}
}

func TestTruncatedToolArgsLookup_NotFound(t *testing.T) {
	args := map[string]string{"bash": `{"command":"echo hi"}`}
	if got := truncatedToolArgsLookup(args, "write_file"); got != "" {
		t.Fatalf("expected empty for non-matching tool, got %q", got)
	}
}

func TestTruncatedToolArgsLookup_NilMap(t *testing.T) {
	if got := truncatedToolArgsLookup(nil, "write_file"); got != "" {
		t.Fatal("expected empty for nil map")
	}
}

func TestResolvePartialWritePath_Absolute(t *testing.T) {
	if got := resolvePartialWritePath(`{"path": "C:\\Users\\test\\file.txt"}`); got == "" {
		t.Fatal("expected resolved absolute path")
	}
}

func TestResolvePartialWritePath_Relative(t *testing.T) {
	got := resolvePartialWritePath(`{"path": "output/file.txt"}`)
	if got == "" {
		t.Fatal("expected resolved relative path")
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute path, got %q", got)
	}
}

func TestResolvePartialWritePath_Empty(t *testing.T) {
	if got := resolvePartialWritePath(`{"content": "stuff"}`); got != "" {
		t.Fatalf("expected empty for missing path, got %q", got)
	}
}
