package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resolvedTempDir returns a temp directory with symlinks resolved,
// ensuring NormalizeFilePathForEvent works correctly on Windows.
func resolvedTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return filepath.Clean(dir)
	}
	return resolved
}

func TestDiffComputer_ModifiedFile(t *testing.T) {
	dir := resolvedTempDir(t)

	// Create original file and capture snapshot.
	originalContent := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	filePath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(filePath, []byte(originalContent), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSnapshotStore(50)
	store.CaptureSnapshots(dir, []string{"main.go"})

	// Modify the file on disk.
	modifiedContent := "line 1\nline 2 modified\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(filePath, []byte(modifiedContent), 0644); err != nil {
		t.Fatal(err)
	}

	dc := NewDiffComputer()
	diffs := dc.ComputeFileDiffs(dir, store, []string{"main.go"}, nil)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}

	d := diffs[0]
	if d.Path != "main.go" {
		t.Errorf("expected path 'main.go', got %q", d.Path)
	}
	if d.ChangeType != "modified" {
		t.Errorf("expected change type 'modified', got %q", d.ChangeType)
	}
	if d.Language != "go" {
		t.Errorf("expected language 'go', got %q", d.Language)
	}
	if d.Error != "" {
		t.Errorf("unexpected error: %s", d.Error)
	}
	if !strings.Contains(d.Diff, "-line 2") {
		t.Errorf("expected diff to contain '-line 2', got:\n%s", d.Diff)
	}
	if !strings.Contains(d.Diff, "+line 2 modified") {
		t.Errorf("expected diff to contain '+line 2 modified', got:\n%s", d.Diff)
	}
	if d.Truncated {
		t.Error("expected Truncated=false for small diff")
	}
}

func TestDiffComputer_CreatedFile(t *testing.T) {
	dir := resolvedTempDir(t)

	content := "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
	filePath := filepath.Join(dir, "new_file.go")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSnapshotStore(50)
	dc := NewDiffComputer()
	diffs := dc.ComputeFileDiffs(dir, store, nil, []string{"new_file.go"})

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}

	d := diffs[0]
	if d.Path != "new_file.go" {
		t.Errorf("expected path 'new_file.go', got %q", d.Path)
	}
	if d.ChangeType != "added" {
		t.Errorf("expected change type 'added', got %q", d.ChangeType)
	}
	if d.Language != "go" {
		t.Errorf("expected language 'go', got %q", d.Language)
	}
	if d.Error != "" {
		t.Errorf("unexpected error: %s", d.Error)
	}
	// All content lines should be additions (no deletions).
	hasAdditions := false
	for _, line := range strings.Split(d.Diff, "\n") {
		if line == "" || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "@@") {
			continue
		}
		if strings.HasPrefix(line, "+") {
			hasAdditions = true
		} else if strings.HasPrefix(line, "-") {
			t.Errorf("unexpected deletion line in created file diff: %q", line)
			break
		}
	}
	if !hasAdditions {
		t.Error("expected at least one addition line in created file diff")
	}
}

func TestDiffComputer_DeletedFile(t *testing.T) {
	dir := resolvedTempDir(t)

	content := "line A\nline B\nline C\n"
	filePath := filepath.Join(dir, "deleted.ts")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSnapshotStore(50)
	store.CaptureSnapshots(dir, []string{"deleted.ts"})

	// Delete the file from disk.
	if err := os.Remove(filePath); err != nil {
		t.Fatal(err)
	}

	dc := NewDiffComputer()
	// File is in filesModified but no longer exists on disk -> treated as deleted.
	diffs := dc.ComputeFileDiffs(dir, store, []string{"deleted.ts"}, nil)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}

	d := diffs[0]
	if d.Path != "deleted.ts" {
		t.Errorf("expected path 'deleted.ts', got %q", d.Path)
	}
	if d.ChangeType != "deleted" {
		t.Errorf("expected change type 'deleted', got %q", d.ChangeType)
	}
	if d.Language != "typescript" {
		t.Errorf("expected language 'typescript', got %q", d.Language)
	}
	// All content lines should be deletions (no additions).
	hasDeletions := false
	for _, line := range strings.Split(d.Diff, "\n") {
		if line == "" || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "@@") {
			continue
		}
		if strings.HasPrefix(line, "-") {
			hasDeletions = true
		} else if strings.HasPrefix(line, "+") {
			t.Errorf("unexpected addition line in deleted file diff: %q", line)
			break
		}
	}
	if !hasDeletions {
		t.Error("expected at least one deletion line in deleted file diff")
	}
}

func TestDiffComputer_EmptyResult_NoChanges(t *testing.T) {
	dir := resolvedTempDir(t)

	store := NewFileSnapshotStore(50)
	dc := NewDiffComputer()
	diffs := dc.ComputeFileDiffs(dir, store, nil, nil)

	if len(diffs) != 0 {
		t.Errorf("expected 0 diffs for no changes, got %d", len(diffs))
	}
}

func TestDiffComputer_EmptyResult_NoActualChange(t *testing.T) {
	dir := resolvedTempDir(t)

	content := "unchanged content\n"
	filePath := filepath.Join(dir, "same.txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSnapshotStore(50)
	store.CaptureSnapshots(dir, []string{"same.txt"})

	dc := NewDiffComputer()
	diffs := dc.ComputeFileDiffs(dir, store, []string{"same.txt"}, nil)

	if len(diffs) != 0 {
		t.Errorf("expected 0 diffs for unchanged file, got %d", len(diffs))
	}
}

func TestDiffComputer_BinaryFile(t *testing.T) {
	dir := resolvedTempDir(t)

	binaryContent := []byte("hello\x00world\x00binary")
	filePath := filepath.Join(dir, "image.png")
	if err := os.WriteFile(filePath, binaryContent, 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSnapshotStore(50)
	store.CaptureSnapshots(dir, []string{"image.png"})

	// Modify the binary file.
	if err := os.WriteFile(filePath, []byte("different\x00binary"), 0644); err != nil {
		t.Fatal(err)
	}

	dc := NewDiffComputer()
	diffs := dc.ComputeFileDiffs(dir, store, []string{"image.png"}, nil)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}

	d := diffs[0]
	if d.Diff != "Binary file changed" {
		t.Errorf("expected 'Binary file changed', got %q", d.Diff)
	}
	if d.ChangeType != "modified" {
		t.Errorf("expected change type 'modified', got %q", d.ChangeType)
	}
}

func TestDiffComputer_BinaryCreatedFile(t *testing.T) {
	dir := resolvedTempDir(t)

	binaryContent := []byte("PNG\x00\x00header")
	filePath := filepath.Join(dir, "output.bin")
	if err := os.WriteFile(filePath, binaryContent, 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSnapshotStore(50)
	dc := NewDiffComputer()
	diffs := dc.ComputeFileDiffs(dir, store, nil, []string{"output.bin"})

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}

	d := diffs[0]
	if d.Diff != "Binary file changed" {
		t.Errorf("expected 'Binary file changed', got %q", d.Diff)
	}
	if d.ChangeType != "added" {
		t.Errorf("expected change type 'added', got %q", d.ChangeType)
	}
}

func TestDiffComputer_Truncation(t *testing.T) {
	dir := resolvedTempDir(t)

	// Create a file with many lines to trigger truncation.
	var lines []string
	for i := 0; i < 600; i++ {
		lines = append(lines, "line content here for testing purposes")
	}
	content := strings.Join(lines, "\n") + "\n"
	filePath := filepath.Join(dir, "large.py")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSnapshotStore(50)
	dc := NewDiffComputer()
	diffs := dc.ComputeFileDiffs(dir, store, nil, []string{"large.py"})

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}

	d := diffs[0]
	if !d.Truncated {
		t.Error("expected Truncated=true for large diff")
	}
	if d.TotalLines <= 500 {
		t.Errorf("expected TotalLines > 500, got %d", d.TotalLines)
	}
	if d.Language != "python" {
		t.Errorf("expected language 'python', got %q", d.Language)
	}
	// Verify the truncation marker is present.
	if !strings.Contains(d.Diff, "[truncated:") {
		t.Errorf("expected truncation marker in diff")
	}
	// Count actual lines in the truncated diff (before the marker line).
	diffLines := strings.Split(d.Diff, "\n")
	nonMarkerLines := 0
	for _, l := range diffLines {
		if strings.HasPrefix(l, "... [truncated:") {
			break
		}
		nonMarkerLines++
	}
	if nonMarkerLines != 500 {
		t.Errorf("expected exactly 500 lines before truncation marker, got %d", nonMarkerLines)
	}
}

func TestDiffComputer_DeletedFileFromSnapshot(t *testing.T) {
	dir := resolvedTempDir(t)

	content := "will be deleted\n"
	filePath := filepath.Join(dir, "remove_me.js")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSnapshotStore(50)
	store.CaptureSnapshots(dir, []string{"remove_me.js"})

	// Delete the file.
	if err := os.Remove(filePath); err != nil {
		t.Fatal(err)
	}

	dc := NewDiffComputer()
	// File is NOT in filesModified or filesCreated, but is in snapshot and missing on disk.
	diffs := dc.ComputeFileDiffs(dir, store, nil, nil)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff for deleted file from snapshot, got %d", len(diffs))
	}

	d := diffs[0]
	if d.ChangeType != "deleted" {
		t.Errorf("expected change type 'deleted', got %q", d.ChangeType)
	}
	if d.Language != "javascript" {
		t.Errorf("expected language 'javascript', got %q", d.Language)
	}
	if !strings.Contains(d.Diff, "-will be deleted") {
		t.Errorf("expected deletion line in diff, got:\n%s", d.Diff)
	}
}

func TestDiffComputer_NormalizesPaths(t *testing.T) {
	dir := resolvedTempDir(t)

	// Create a file in a subdirectory.
	subDir := filepath.Join(dir, "src", "pkg")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "package pkg\n"
	filePath := filepath.Join(subDir, "handler.go")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSnapshotStore(50)
	dc := NewDiffComputer()
	diffs := dc.ComputeFileDiffs(dir, store, nil, []string{filepath.Join("src", "pkg", "handler.go")})

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}

	d := diffs[0]
	// Path should use forward slashes.
	if d.Path != "src/pkg/handler.go" {
		t.Errorf("expected normalized path 'src/pkg/handler.go', got %q", d.Path)
	}
}

func TestInferLanguage(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"main.go", "go"},
		{"app.ts", "typescript"},
		{"component.tsx", "typescriptreact"},
		{"script.js", "javascript"},
		{"app.py", "python"},
		{"style.css", "css"},
		{"config.yaml", "yaml"},
		{"config.yml", "yaml"},
		{"data.json", "json"},
		{"README.md", "markdown"},
		{"query.sql", "sql"},
		{"build.sh", "shellscript"},
		{"Makefile", "plaintext"},
		{"noext", "plaintext"},
		{"file.unknown", "plaintext"},
		{"page.html", "html"},
		{"app.vue", "vue"},
		{"lib.rs", "rust"},
		{"Main.java", "java"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := inferLanguage(tt.path)
			if got != tt.expected {
				t.Errorf("inferLanguage(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}
