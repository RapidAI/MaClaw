package knowledge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestHashScanCandidatesParallel(t *testing.T) {
	root := t.TempDir()
	const n = 24
	cands := make([]scanHashCandidate, n)
	for i := 0; i < n; i++ {
		path := filepath.Join(root, fmt.Sprintf("f%02d.md", i))
		mustWrite(t, path, []byte(fmt.Sprintf("content-%d-unique", i)))
		cands[i] = scanHashCandidate{path: path, rel: filepath.Base(path), kind: SourceKindMarkdown, size: 16}
	}
	if err := hashScanCandidates(context.Background(), cands, nil); err != nil {
		t.Fatalf("hashScanCandidates: %v", err)
	}
	seen := map[string]struct{}{}
	for i, c := range cands {
		if c.err != nil || c.hash == "" {
			t.Fatalf("candidate %d: hash=%q err=%v", i, c.hash, c.err)
		}
		if _, ok := seen[c.hash]; ok {
			t.Fatalf("unexpected hash collision at %d", i)
		}
		seen[c.hash] = struct{}{}
	}
	// Single-file path
	one := []scanHashCandidate{cands[0]}
	one[0].hash = ""
	if err := hashScanCandidates(context.Background(), one, nil); err != nil || one[0].hash == "" {
		t.Fatalf("single hash failed: %v %#v", err, one[0])
	}
}

func TestScanDirectoryProgressCallback(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 20; i++ {
		mustWrite(t, filepath.Join(root, fmt.Sprintf("p%02d.md", i)), []byte(fmt.Sprintf("scan-progress-%d", i)))
	}
	var phases []string
	var maxHashDone int
	_, _, err := ScanDirectoryProgress(context.Background(), DirectoryImportRequest{
		RootPath:     root,
		Recursive:    true,
		IncludeExts:  []string{".md"},
		MaxFileBytes: 1024,
	}, nil, func(phase string, done, total int, path string) {
		phases = append(phases, phase)
		if phase == "hash" && done > maxHashDone {
			maxHashDone = done
		}
	})
	if err != nil {
		t.Fatalf("ScanDirectoryProgress: %v", err)
	}
	hasWalk, hasHash := false, false
	for _, p := range phases {
		if p == "walk" {
			hasWalk = true
		}
		if p == "hash" {
			hasHash = true
		}
	}
	if !hasWalk || !hasHash {
		t.Fatalf("expected walk+hash phases, got %v", phases)
	}
	if maxHashDone < 20 {
		t.Fatalf("hash done=%d want >=20", maxHashDone)
	}
}

func TestStoreScanProgressCallbackSerializesParallelHashEvents(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 24; i++ {
		mustWrite(t, filepath.Join(root, fmt.Sprintf("p%02d.md", i)), []byte(fmt.Sprintf("scan-serial-%d", i)))
	}
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var inCallback atomic.Int32
	var overlaps atomic.Int32
	store.SetScanProgressCallback(func(string, int, int, string) {
		if inCallback.Add(1) != 1 {
			overlaps.Add(1)
		}
		inCallback.Add(-1)
	})
	if _, err := store.ScanDirectory(context.Background(), DirectoryImportRequest{
		RootPath: root, Recursive: true, IncludeExts: []string{".md"}, MaxFileBytes: 1024,
	}); err != nil {
		t.Fatalf("ScanDirectory: %v", err)
	}
	if got := overlaps.Load(); got != 0 {
		t.Fatalf("scan progress callback overlapped %d times", got)
	}
}

func TestScanDirectoryParallelHashDedups(t *testing.T) {
	root := t.TempDir()
	// Many files including duplicates — parallel hash must preserve first-wins dedupe.
	for i := 0; i < 20; i++ {
		content := "shared"
		if i%5 == 0 {
			content = fmt.Sprintf("unique-%d", i)
		}
		mustWrite(t, filepath.Join(root, fmt.Sprintf("n%02d.md", i)), []byte(content))
	}
	res, items, err := ScanDirectory(context.Background(), DirectoryImportRequest{
		RootPath:     root,
		Recursive:    true,
		IncludeExts:  []string{".md"},
		MaxFileBytes: 1024,
	}, nil)
	if err != nil {
		t.Fatalf("ScanDirectory: %v", err)
	}
	if res.QueuedFiles+res.DuplicateFiles != 20 {
		t.Fatalf("queued=%d dup=%d want sum 20", res.QueuedFiles, res.DuplicateFiles)
	}
	// 4 unique (0,5,10,15) + 1 first "shared" = 5 queued; rest dups
	if res.QueuedFiles != 5 {
		t.Fatalf("queued=%d want 5", res.QueuedFiles)
	}
	if res.DuplicateFiles != 15 {
		t.Fatalf("duplicates=%d want 15", res.DuplicateFiles)
	}
	queued := 0
	for _, it := range items {
		if it.Status == ItemStatusQueued {
			queued++
			if it.FileHash == "" {
				t.Fatalf("queued item missing hash: %#v", it)
			}
		}
	}
	if queued != 5 {
		t.Fatalf("queued items=%d", queued)
	}
}

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

func TestScanDirectoryCapsParserHeavyFormatsAtSharedExtractionLimit(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"large.csv", "large.xlsx", "large.pdf"} {
		path := filepath.Join(root, name)
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(32*1024*1024 + 1); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	res, items, err := ScanDirectory(context.Background(), DirectoryImportRequest{
		RootPath: root, Recursive: true, IncludeExts: []string{".csv", ".xlsx", ".pdf"},
		// Deliberately higher than the shared extraction ceiling.
		MaxFileBytes: DefaultMaxFileBytes,
	}, nil)
	if err != nil {
		t.Fatalf("ScanDirectory: %v", err)
	}
	if res.QueuedFiles != 0 || res.SkippedFiles != 3 {
		t.Fatalf("scan result = %#v, want three shared-limit skips", res)
	}
	for _, item := range items {
		if item.Status != ItemStatusSkippedTooLarge {
			t.Fatalf("item = %#v, want skipped_too_large", item)
		}
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

func TestNormalizeDirectoryImportRequestCopiesOfficeReadPolicy(t *testing.T) {
	emitMarkdown := true
	fallback := true
	policy := &agent.OfficeReadConfig{
		Engine:       "officeread",
		Formats:      []string{"docx"},
		Fallback:     &fallback,
		EmitMarkdown: &emitMarkdown,
	}
	normalized := NormalizeDirectoryImportRequest(DirectoryImportRequest{OfficeReadConfig: policy})
	policy.Formats[0] = "pptx"
	fallback = false
	emitMarkdown = false
	if normalized.OfficeReadConfig == nil || normalized.OfficeReadConfig.Formats[0] != "docx" || normalized.OfficeReadConfig.Fallback == nil || !*normalized.OfficeReadConfig.Fallback || normalized.OfficeReadConfig.EmitMarkdown == nil || !*normalized.OfficeReadConfig.EmitMarkdown {
		t.Fatalf("OfficeRead policy must be immutable request snapshot: %#v", normalized.OfficeReadConfig)
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
