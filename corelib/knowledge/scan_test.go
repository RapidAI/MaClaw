package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestScanDirectoryFiltersAndDedups(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.md"), []byte("alpha"))
	mustWrite(t, filepath.Join(root, "b.md"), []byte("alpha"))
	mustWrite(t, filepath.Join(root, "c.exe"), []byte("bin"))
	mustWrite(t, filepath.Join(root, ".hidden.txt"), []byte("hidden"))
	mustWrite(t, filepath.Join(root, "big.txt"), []byte("123456789"))
	mustWrite(t, filepath.Join(root, "nested", "d.txt"), []byte("nested"))

	res, items, err := ScanDirectory(context.Background(), DirectoryImportRequest{
		RootPath:     root,
		Recursive:    true,
		IncludeExts:  []string{".md", ".txt"},
		MaxFileBytes: 6,
	}, nil)
	if err != nil {
		t.Fatalf("ScanDirectory returned error: %v", err)
	}
	if res.QueuedFiles != 2 {
		t.Fatalf("queued = %d, want 2", res.QueuedFiles)
	}
	if res.DuplicateFiles != 1 {
		t.Fatalf("duplicates = %d, want 1", res.DuplicateFiles)
	}
	if res.SkippedFiles != 4 {
		t.Fatalf("skipped = %d, want 4 (duplicate, hidden, too large, unsupported)", res.SkippedFiles)
	}
	statuses := map[string]bool{}
	for _, item := range items {
		statuses[item.Status] = true
	}
	for _, status := range []string{ItemStatusQueued, ItemStatusSkippedDuplicate, ItemStatusSkippedHidden, ItemStatusSkippedTooLarge, ItemStatusSkippedType} {
		if !statuses[status] {
			t.Fatalf("missing status %s in %#v", status, statuses)
		}
	}
	// ExtCounts covers supported include-ext files (queued + size/dup skips), not unsupported/hidden.
	if res.ExtCounts[".md"] != 2 {
		t.Fatalf("ext_counts .md = %d, want 2 (a.md + b.md duplicate)", res.ExtCounts[".md"])
	}
	if res.ExtCounts[".txt"] != 2 {
		t.Fatalf("ext_counts .txt = %d, want 2 (big.txt + nested/d.txt)", res.ExtCounts[".txt"])
	}
	if _, ok := res.ExtCounts[".exe"]; ok {
		t.Fatalf("ext_counts should not include unsupported .exe")
	}
}

func TestScanDirectoryHonorsRecursiveFalse(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.md"), []byte("alpha"))
	mustWrite(t, filepath.Join(root, "nested", "d.txt"), []byte("nested"))

	res, _, err := ScanDirectory(context.Background(), DirectoryImportRequest{
		RootPath:     root,
		Recursive:    false,
		IncludeExts:  []string{".md", ".txt"},
		MaxFileBytes: 1024,
	}, nil)
	if err != nil {
		t.Fatalf("ScanDirectory returned error: %v", err)
	}
	if res.QueuedFiles != 1 {
		t.Fatalf("queued = %d, want only root file", res.QueuedFiles)
	}
}

func TestScanDirectoryHonorsExcludeGlobs(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "keep.md"), []byte("keep"))
	mustWrite(t, filepath.Join(root, "skip.tmp"), []byte("tmp"))
	mustWrite(t, filepath.Join(root, "vendor", "skip.md"), []byte("vendor"))

	res, items, err := ScanDirectory(context.Background(), DirectoryImportRequest{
		RootPath:     root,
		Recursive:    true,
		IncludeExts:  []string{".md", ".tmp"},
		ExcludeGlobs: []string{"*.tmp", "vendor/**"},
		MaxFileBytes: 1024,
	}, nil)
	if err != nil {
		t.Fatalf("ScanDirectory returned error: %v", err)
	}
	if res.QueuedFiles != 1 || res.SkippedFiles != 1 {
		t.Fatalf("queued=%d skipped=%d, want queued=1 skipped=1", res.QueuedFiles, res.SkippedFiles)
	}
	statuses := map[string]int{}
	for _, item := range items {
		statuses[item.Status]++
	}
	if statuses[ItemStatusSkippedExcluded] != 1 {
		t.Fatalf("excluded status count=%d in %#v", statuses[ItemStatusSkippedExcluded], items)
	}
}

func TestScanFilesHonorsExcludeGlobs(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "keep.md")
	skip := filepath.Join(root, "draft.tmp")
	mustWrite(t, keep, []byte("keep"))
	mustWrite(t, skip, []byte("skip"))

	res, items, err := ScanFiles(context.Background(), DirectoryImportRequest{
		RootPath:     root,
		IncludeExts:  []string{".md", ".tmp"},
		ExcludeGlobs: []string{"*.tmp"},
		MaxFileBytes: 1024,
	}, []string{keep, skip}, nil)
	if err != nil {
		t.Fatalf("ScanFiles returned error: %v", err)
	}
	if res.QueuedFiles != 1 || res.SkippedFiles != 1 {
		t.Fatalf("queued=%d skipped=%d, want queued=1 skipped=1", res.QueuedFiles, res.SkippedFiles)
	}
	if len(items) != 2 {
		t.Fatalf("items=%d, want 2", len(items))
	}
}

func TestNormalizeDirectoryImportRequestSplitsIncludeExts(t *testing.T) {
	req := NormalizeDirectoryImportRequest(DirectoryImportRequest{
		IncludeExts: []string{" MD，.txt；csv、md "},
	})
	want := []string{".md", ".txt", ".csv"}
	if len(req.IncludeExts) != len(want) {
		t.Fatalf("IncludeExts = %#v, want %#v", req.IncludeExts, want)
	}
	for i := range want {
		if req.IncludeExts[i] != want[i] {
			t.Fatalf("IncludeExts = %#v, want %#v", req.IncludeExts, want)
		}
	}
}

func TestScanFilesSplitsMultilineFilePathInputs(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "a.md")
	second := filepath.Join(root, "b.txt")
	mustWrite(t, first, []byte("alpha"))
	mustWrite(t, second, []byte("beta"))

	res, items, err := ScanFiles(context.Background(), DirectoryImportRequest{
		RootPath:     root,
		IncludeExts:  []string{".md", ".txt"},
		MaxFileBytes: 1024,
	}, []string{first + "\n\t" + second}, nil)
	if err != nil {
		t.Fatalf("ScanFiles returned error: %v", err)
	}
	if res.QueuedFiles != 2 {
		t.Fatalf("queued = %d, want 2", res.QueuedFiles)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
}

func TestScanFilesDoesNotSplitCommaOrSemicolonFileNames(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a,b;c.md")
	mustWrite(t, path, []byte("alpha"))

	res, items, err := ScanFiles(context.Background(), DirectoryImportRequest{
		RootPath:     root,
		IncludeExts:  []string{".md"},
		MaxFileBytes: 1024,
	}, []string{path}, nil)
	if err != nil {
		t.Fatalf("ScanFiles returned error: %v", err)
	}
	if res.QueuedFiles != 1 {
		t.Fatalf("queued = %d, want 1", res.QueuedFiles)
	}
	if len(items) != 1 || items[0].FilePath != path {
		t.Fatalf("items = %#v, want file path %q", items, path)
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
