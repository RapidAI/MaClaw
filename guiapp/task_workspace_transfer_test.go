package guiapp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeTransferTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// requireTransferSymlink creates a symlink or skips the test. Some Windows
// setups (and sandboxes that emulate the syscall) silently materialize the link
// as a plain copy instead, which would make a symlink assertion meaningless.
func requireTransferSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Skipf("filesystem materializes symlinks as copies instead of links")
	}
}

func TestCopyTaskTransferTreeCopiesNestedFiles(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeTransferTestFile(t, filepath.Join(src, "task.md"), "task")
	writeTransferTestFile(t, filepath.Join(src, "workspace", "src", "main.go"), "package main")
	writeTransferTestFile(t, filepath.Join(src, "workspace", "README.md"), "readme")

	files, bytes, err := copyTaskTransferTree(filepath.Join(src, "workspace"), filepath.Join(dst, "out"))
	if err != nil {
		t.Fatalf("copyTaskTransferTree: %v", err)
	}
	if files != 2 {
		t.Fatalf("files = %d, want 2", files)
	}
	if want := int64(len("package main") + len("readme")); bytes != want {
		t.Fatalf("bytes = %d, want %d", bytes, want)
	}
	got, err := os.ReadFile(filepath.Join(dst, "out", "src", "main.go"))
	if err != nil {
		t.Fatalf("read copied file: %v", err)
	}
	if string(got) != "package main" {
		t.Fatalf("copied content = %q", string(got))
	}
	// The source keeps its files: the caller deletes them only after the user
	// confirms removing the source task.
	if _, err := os.Stat(filepath.Join(src, "task.md")); err != nil {
		t.Fatalf("source task.md should be untouched: %v", err)
	}
}

func TestCopyTaskTransferTreeRefusesDestinationInsideSource(t *testing.T) {
	// A destination nested in its own source is the one shape the budget cannot
	// bound: WalkDir reads directories as it writes them, so every level copies
	// the previous level's output again.
	src := t.TempDir()
	writeTransferTestFile(t, filepath.Join(src, "keep.txt"), "keep")

	if _, _, err := copyTaskTransferTree(src, filepath.Join(src, "nested")); !errors.Is(err, errTaskTransferNestedTarget) {
		t.Fatalf("nested destination err = %v, want errTaskTransferNestedTarget", err)
	}
	if _, _, err := copyTaskTransferTree(src, src); !errors.Is(err, errTaskTransferNestedTarget) {
		t.Fatalf("same directory err = %v, want errTaskTransferNestedTarget", err)
	}
	// Refused before anything is written, so no stray folder is left behind.
	if entries, err := os.ReadDir(src); err != nil || len(entries) != 1 {
		t.Fatalf("source should only hold keep.txt, got %v (err=%v)", entries, err)
	}
}

func TestCopyTaskTransferTreeCountsEmptySourceAsNoOp(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	files, bytes, err := copyTaskTransferTree(src, filepath.Join(dst, "out"))
	if err != nil {
		t.Fatalf("copyTaskTransferTree: %v", err)
	}
	if files != 0 || bytes != 0 {
		t.Fatalf("files=%d bytes=%d, want 0/0", files, bytes)
	}
}

func TestTaskTransferLinkTargetAllowedRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeTransferTestFile(t, filepath.Join(src, "keep.txt"), "keep")
	outside := filepath.Join(root, "outside")
	writeTransferTestFile(t, filepath.Join(outside, "secret.txt"), "secret")

	cases := []struct {
		name     string
		resolved string
		allowed  bool
	}{
		{"inside", filepath.Join(src, "keep.txt"), true},
		{"nested inside", filepath.Join(src, "deep", "keep.txt"), true},
		{"self", src, true},
		{"sibling", outside, false},
		{"escape via parent", filepath.Join(src, "..", "outside"), false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		if got := taskTransferLinkTargetAllowed(src, tc.resolved); got != tc.allowed {
			t.Fatalf("%s: allowed = %v, want %v", tc.name, got, tc.allowed)
		}
	}
}

func TestCopyTaskTransferTreeSkipsLinksOutsideSource(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	writeTransferTestFile(t, filepath.Join(src, "keep.txt"), "keep")
	outside := filepath.Join(root, "outside")
	writeTransferTestFile(t, filepath.Join(outside, "secret.txt"), "secret")
	requireTransferSymlink(t, outside, filepath.Join(src, "escaped"))
	requireTransferSymlink(t, filepath.Join(root, "missing"), filepath.Join(src, "broken.txt"))

	files, _, err := copyTaskTransferTree(src, dst)
	if err != nil {
		t.Fatalf("copyTaskTransferTree: %v", err)
	}
	if files != 1 {
		t.Fatalf("files = %d, want 1 (only keep.txt)", files)
	}
	if _, err := os.Stat(filepath.Join(dst, "escaped", "secret.txt")); err == nil {
		t.Fatalf("link escaping the source tree must not be copied")
	}
}

func TestCopyTaskTransferTreeStopsAtLinkCycle(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeTransferTestFile(t, filepath.Join(src, "a.txt"), "a")
	// loop/ points back at its own parent, so a naive recursive copy would
	// never finish.
	requireTransferSymlink(t, src, filepath.Join(src, "loop"))

	files, _, err := copyTaskTransferTree(src, filepath.Join(root, "dst"))
	if err != nil {
		t.Fatalf("copyTaskTransferTree: %v", err)
	}
	if files < 1 {
		t.Fatalf("files = %d, want at least 1", files)
	}
}

// The symlink tests above skip on filesystems without symlink support (most
// Windows setups), so the cycle guard is also asserted through the state it
// keeps — this runs everywhere.
func TestTaskTransferWalkClaimsEachDirectoryOnce(t *testing.T) {
	root := t.TempDir()
	w := newTaskTransferWalk()
	if !w.claimDir(root) {
		t.Fatal("first claim must succeed")
	}
	if w.claimDir(root) {
		t.Fatal("second claim must be refused: re-entering a directory is what loops forever")
	}
	// Trailing separators and dot segments must not smuggle in a second copy.
	if w.claimDir(filepath.Join(root, "sub", "..")) {
		t.Fatal("claim must be path-normalised")
	}
}

func TestTaskTransferWalkClaimsSourceRootBeforeCopying(t *testing.T) {
	// copyTree must claim its own root, otherwise a nested pass started for a
	// link back onto that root ("loop -> .") would copy it again from scratch
	// and recurse forever.
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "dst")
	writeTransferTestFile(t, filepath.Join(src, "a.txt"), "a")

	w := newTaskTransferWalk()
	if err := w.copyTree(src, dst); err != nil {
		t.Fatalf("copyTree: %v", err)
	}
	if !w.visited[filepath.Clean(src)] {
		t.Fatalf("copyTree must claim the source root, got keys %v", w.visited)
	}
	if w.files != 1 {
		t.Fatalf("files = %d, want 1", w.files)
	}
}

func TestTaskTransferWalkRefusesDeepLinkChains(t *testing.T) {
	w := newTaskTransferWalk()
	for i := 0; i < taskTransferMaxDepth; i++ {
		if err := w.descend(); err != nil {
			t.Fatalf("descend %d: %v", i, err)
		}
	}
	if err := w.descend(); !errors.Is(err, errTaskTransferTooDeep) {
		t.Fatalf("err = %v, want errTaskTransferTooDeep once the ceiling is reached", err)
	}
	w.ascend()
	if err := w.descend(); err != nil {
		t.Fatalf("ascend must free a level: %v", err)
	}
	// A guard that never decrements would wedge after the first link chain.
	w.ascend()
	for i := 0; i < taskTransferMaxDepth; i++ {
		w.ascend()
	}
	if w.depth != 0 {
		t.Fatalf("depth = %d, want 0 after unwinding", w.depth)
	}
}

func TestCopyTaskTransferTreeOverwritesReadOnlyDestination(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeTransferTestFile(t, filepath.Join(src, "a.txt"), "newer")
	writeTransferTestFile(t, filepath.Join(dst, "a.txt"), "older")
	if err := os.Chmod(filepath.Join(dst, "a.txt"), 0o444); err != nil {
		t.Skipf("chmod unsupported: %v", err)
	}

	files, _, err := copyTaskTransferTree(src, dst)
	if err != nil {
		t.Fatalf("second transfer must overwrite a read-only copy: %v", err)
	}
	if files != 1 {
		t.Fatalf("files = %d, want 1", files)
	}
	got, err := os.ReadFile(filepath.Join(dst, "a.txt"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "newer" {
		t.Fatalf("content = %q, want %q", got, "newer")
	}
}

func TestTaskTransferWalkBudgetIsCumulativeAcrossTrees(t *testing.T) {
	// Two trees that each fit must still be refused once their combined size
	// passes the ceiling — otherwise every symlinked subtree could spend a full
	// allowance and the "one transfer" budget would not mean anything.
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	writeTransferTestFile(t, filepath.Join(first, "a.bin"), "0123456789")
	writeTransferTestFile(t, filepath.Join(second, "b.bin"), "0123456789")

	w := newTaskTransferWalk()
	w.maxBytes = 15
	if err := w.auditDir(first); err != nil {
		t.Fatalf("first tree should fit: %v", err)
	}
	if err := w.auditDir(second); !errors.Is(err, errTaskTransferTooLarge) {
		t.Fatalf("combined trees must exceed the budget, got %v", err)
	}
}

func TestTaskTransferWalkRefusesOverBudgetTree(t *testing.T) {
	// Driven through copyTree, not a standalone audit helper: the budget only
	// means something if the guard the transfer actually runs rejects.
	src := t.TempDir()
	writeTransferTestFile(t, filepath.Join(src, "a.txt"), "0123456789")
	writeTransferTestFile(t, filepath.Join(src, "b.txt"), "0123456789")

	withinBudget := newTaskTransferWalk()
	withinBudget.maxBytes = 25
	if err := withinBudget.copyTree(src, filepath.Join(t.TempDir(), "ok")); err != nil {
		t.Fatalf("tree within budget should copy: %v", err)
	}

	overFiles := newTaskTransferWalk()
	overFiles.maxFiles = 1
	if err := overFiles.copyTree(src, filepath.Join(t.TempDir(), "files")); !errors.Is(err, errTaskTransferTooLarge) {
		t.Fatalf("file budget err = %v, want errTaskTransferTooLarge", err)
	}

	// Byte ceiling, and refused before the destination folder is created.
	overBudgetDst := filepath.Join(t.TempDir(), "bytes")
	overBytes := newTaskTransferWalk()
	overBytes.maxBytes = 5
	if err := overBytes.copyTree(src, overBudgetDst); !errors.Is(err, errTaskTransferTooLarge) {
		t.Fatalf("byte budget err = %v, want errTaskTransferTooLarge", err)
	}
	if _, err := os.Stat(overBudgetDst); !os.IsNotExist(err) {
		t.Fatalf("an over-budget copy must not create the destination, stat err = %v", err)
	}
}

func TestTaskTransferWalkKeepsDefaultBudget(t *testing.T) {
	w := newTaskTransferWalk()
	if w.maxFiles != taskTransferMaxFiles || w.maxBytes != taskTransferMaxBytes {
		t.Fatalf("default budget = %d files / %d bytes, want %d / %d",
			w.maxFiles, w.maxBytes, taskTransferMaxFiles, taskTransferMaxBytes)
	}
	src := t.TempDir()
	writeTransferTestFile(t, filepath.Join(src, "a.txt"), "a")
	if err := w.copyTree(src, filepath.Join(t.TempDir(), "out")); err != nil {
		t.Fatalf("copyTree with the shipped budget: %v", err)
	}
}

// The exported entry points reject bad arguments before touching the disk, so
// these run against a zero-value App: every branch below returns on validation.
func TestCopyTaskFilesToCloudWorkspaceValidatesArguments(t *testing.T) {
	app := &App{}
	src := t.TempDir()
	for _, workspaceID := range []string{"", "   ", "../escape", "a/b", `a\b`, "CON", "cws."} {
		if _, err := app.CopyTaskFilesToCloudWorkspace(src, workspaceID); err == nil {
			t.Fatalf("workspace id %q must be rejected", workspaceID)
		}
	}
	if _, err := app.CopyTaskFilesToCloudWorkspace("", "cws_valid"); err == nil {
		t.Fatal("an empty source task must be rejected")
	}
	// A rejected argument must not have produced a result at all.
	if _, err := app.CopyTaskFilesToCloudWorkspace(src, ""); err == nil {
		t.Fatal("expected an error for a blank workspace id")
	}
}

func TestCopyCloudWorkspaceTaskFilesToLocalValidatesArguments(t *testing.T) {
	app := &App{}
	dst := t.TempDir()
	if _, err := app.CopyCloudWorkspaceTaskFilesToLocal("", dst); err == nil {
		t.Fatal("an empty source task must be rejected")
	}
	if _, err := app.CopyCloudWorkspaceTaskFilesToLocal(t.TempDir(), ""); err == nil {
		t.Fatal("an empty target folder must be rejected")
	}
	if _, err := app.CopyCloudWorkspaceTaskFilesToLocal("   ", "  "); err == nil {
		t.Fatal("blank paths must be rejected")
	}
}
