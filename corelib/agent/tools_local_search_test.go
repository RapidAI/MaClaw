package agent

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func searchArgsIndexReady(args map[string]interface{}) bool {
	base, err := resolveSearchBase(StringArg(args, "path"))
	if err != nil {
		return false
	}
	info, statErr := os.Stat(base)
	isDir := statErr == nil && info.IsDir()
	if statErr == nil && !info.IsDir() {
		base = filepath.Dir(base)
	}
	root, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	root = filepath.Clean(root)
	hidden := boolArg(args, "include_hidden", false)
	exclude := resolveSearchExcludePattern(args, base)
	if publishedSearchIndex(root, StringArg(args, "glob"), exclude, StringArg(args, "type"), hidden) != nil {
		return true
	}
	// A file search still waits for its own directory corpus. A directory
	// search can answer from a covering corpus before this exact exclude key
	// has one.
	return isDir && publishedCorpusCoveringDir(root, hidden, exclude, boolArg(args, "no_ignore", false)) != nil
}

func ripgrepWhenIndexed(t *testing.T, args map[string]interface{}) string {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		last = ToolRipgrep(args)
		if !searchArgsIndexReady(args) {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if boolArg(args, "stats", false) {
			if strings.Contains(last, "mode=indexed") {
				return last
			}
			time.Sleep(5 * time.Millisecond)
			continue
		}
		return ToolRipgrep(args)
	}
	t.Fatalf("search index was not used:\n%s", last)
	return last
}

func TestIntersectSortedSearchIDs(t *testing.T) {
	got := intersectSortedSearchIDs([]int{1, 3, 4, 8, 9}, []int{0, 3, 7, 8, 10})
	want := []int{3, 8}
	if len(got) != len(want) {
		t.Fatalf("intersectSortedSearchIDs length = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("intersectSortedSearchIDs[%d] = %d, want %d: %#v", i, got[i], want[i], got)
		}
	}
}

func TestSearchIndexRelativePath(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	child := filepath.Join(root, "pkg", "main.go")
	rel, ok := searchIndexRelativePath(root, child)
	if !ok || rel != filepath.Join("pkg", "main.go") {
		t.Fatalf("searchIndexRelativePath child = %q, %v; want pkg/main.go true", rel, ok)
	}
	rel, ok = searchIndexRelativePath(root, root)
	if !ok || rel != "." {
		t.Fatalf("searchIndexRelativePath root = %q, %v; want . true", rel, ok)
	}
	rel, ok = searchIndexRelativePath(root, filepath.Join(filepath.Dir(root), "outside.go"))
	if ok || rel != "" {
		t.Fatalf("searchIndexRelativePath outside root = %q, %v; want empty false", rel, ok)
	}
}
func TestToolFileReadLineRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolFileRead(map[string]interface{}{
		"path":       path,
		"start_line": float64(2),
		"end_line":   float64(3),
	})
	if !strings.Contains(out, "lines 2-3 of 4") || !strings.Contains(out, "two") || !strings.Contains(out, "three") {
		t.Fatalf("unexpected FileRead output:\n%s", out)
	}
	if strings.Contains(out, "one") || strings.Contains(out, "four") {
		t.Fatalf("FileRead included lines outside requested range:\n%s", out)
	}
}

func TestToolGlobDetailedCtxReturnsCancelled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := ToolGlobDetailedCtx(ctx, map[string]interface{}{
		"path":    dir,
		"pattern": "**/*.go",
	})
	if result.Outcome != SearchToolOutcomeError || !strings.Contains(result.Text, "cancelled") {
		t.Fatalf("expected cancelled Glob result, got outcome=%s text=%q", result.Outcome, result.Text)
	}
}

func TestToolRipgrepDetailedCtxReturnsCancelled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := ToolRipgrepDetailedCtx(ctx, map[string]interface{}{
		"path":    dir,
		"pattern": "package",
	})
	if result.Outcome != SearchToolOutcomeError || !strings.Contains(result.Text, "cancelled") {
		t.Fatalf("expected cancelled ripgrep result, got outcome=%s text=%q", result.Outcome, result.Text)
	}
}

func TestWalkSearchFilesFilteredCtxReturnsCancelledAfterVisitCancels(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := walkSearchFilesFilteredCtx(ctx, dir, "**/*.go", "", "", false, false, func(string, fs.DirEntry) error {
		cancel()
		return nil
	})
	if err != context.Canceled {
		t.Fatalf("walkSearchFilesFilteredCtx error = %v, want context.Canceled", err)
	}
}

func TestWalkSearchFilesFilteredCtxStopsImmediatelyAfterVisitCancels(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package test\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	visited := 0

	err := walkSearchFilesFilteredCtx(ctx, dir, "**/*.go", "", "", false, false, func(string, fs.DirEntry) error {
		visited++
		cancel()
		return nil
	})
	if err != context.Canceled {
		t.Fatalf("walkSearchFilesFilteredCtx error = %v, want context.Canceled", err)
	}
	if visited != 1 {
		t.Fatalf("visited %d files after cancellation, want 1", visited)
	}
}

func TestWalkSearchFilesFilteredCtxSingleFileReturnsCancelledAfterVisitCancels(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := walkSearchFilesFilteredCtx(ctx, path, "*.go", "", "", false, false, func(string, fs.DirEntry) error {
		cancel()
		return nil
	})
	if err != context.Canceled {
		t.Fatalf("walkSearchFilesFilteredCtx single-file error = %v, want context.Canceled", err)
	}
}

func TestToolGlobRecursivePatternMatchesRootFiles(t *testing.T) {
	dir := t.TempDir()
	rootFile := filepath.Join(dir, "main.go")
	nestedDir := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nestedFile := filepath.Join(nestedDir, "lib.go")
	for _, path := range []string{rootFile, nestedFile, filepath.Join(dir, "README.md")} {
		if err := os.WriteFile(path, []byte("package test\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ToolGlob(map[string]interface{}{
		"path":    dir,
		"pattern": "**/*.go",
	})
	if !strings.Contains(out, rootFile) || !strings.Contains(out, nestedFile) {
		t.Fatalf("Glob did not match expected files:\n%s", out)
	}
	if strings.Contains(out, "README.md") {
		t.Fatalf("Glob matched non-go file:\n%s", out)
	}
}

func TestToolGlobBasenamePatternMatchesNestedFiles(t *testing.T) {
	dir := t.TempDir()
	nestedDir := filepath.Join(dir, "docs", "guide")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rootFile := filepath.Join(dir, "README.md")
	nestedFile := filepath.Join(nestedDir, "intro.md")
	for _, path := range []string{rootFile, nestedFile, filepath.Join(nestedDir, "code.go")} {
		if err := os.WriteFile(path, []byte("content\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ToolGlob(map[string]interface{}{
		"path":    dir,
		"pattern": "*.md",
	})
	if !strings.Contains(out, rootFile) || !strings.Contains(out, nestedFile) {
		t.Fatalf("Glob basename pattern did not match markdown files at all depths:\n%s", out)
	}
	if strings.Contains(out, "code.go") {
		t.Fatalf("Glob basename pattern matched wrong extension:\n%s", out)
	}
}

func TestToolRipgrepSearchesWithGlobFilter(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	mdFile := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(goFile, []byte("package main\nfunc Target() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mdFile, []byte("Target should not appear with go glob\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "target",
		"glob":        "**/*.go",
		"max_results": float64(10),
	})
	if !strings.Contains(out, goFile+":2:") || !strings.Contains(out, "func Target") {
		t.Fatalf("ripgrep did not find expected match:\n%s", out)
	}
	if strings.Contains(out, mdFile) {
		t.Fatalf("ripgrep ignored glob filter:\n%s", out)
	}
}

func TestToolRipgrepDoubleStarGlobMatchesDirectChild(t *testing.T) {
	dir := t.TempDir()
	direct := filepath.Join(dir, "src", "main.go")
	nested := filepath.Join(dir, "src", "sub", "nested.go")
	other := filepath.Join(dir, "other", "main.go")
	for _, path := range []string{direct, nested, other} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package main\nfunc GlobStarDirect() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	matched, err := matchGlob("src/**/*.go", "src/main.go", false)
	if err != nil || !matched {
		t.Fatalf("src/**/*.go should match src/main.go: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("src/**/*.go", "other/main.go", false)
	if err != nil || matched {
		t.Fatalf("src/**/*.go should not match other/main.go: matched=%v err=%v", matched, err)
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "GlobStarDirect",
		"glob":        "src/**/*.go",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, direct) || !strings.Contains(out, nested) || strings.Contains(out, other) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("double-star glob missed a direct child or included another directory:\n%s", out)
	}
}

func TestToolRipgrepGlobStarIsSegment(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "main.go")
	direct := filepath.Join(dir, "src", "main.go")
	nested := filepath.Join(dir, "src", "pkg", "main.go")
	other := filepath.Join(dir, "lib", "main.go")
	for _, path := range []string{root, direct, nested, other} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package main\nfunc GlobStarSegment() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	matched, err := matchGlob("**.go", "src/main.go", false)
	if err != nil || !matched {
		t.Fatalf("**.go should match a base name in any directory: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("src/**.go", "src/main.go", false)
	if err != nil || !matched {
		t.Fatalf("src/**.go should match src/main.go: matched=%v err=%v", matched, err)
	}
	for _, rel := range []string{"src/pkg/main.go", "lib/main.go"} {
		matched, err = matchGlob("src/**.go", rel, false)
		if err != nil || matched {
			t.Fatalf("src/**.go should not match %s: matched=%v err=%v", rel, matched, err)
		}
	}
	matched, err = matchGlob("src/**", "src/pkg/main.go", false)
	if err != nil || !matched {
		t.Fatalf("src/** should match everything inside src: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("foo**.go", "dir/foobar.go", false)
	if err != nil || !matched {
		t.Fatalf("foo**.go should match base name foobar.go: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("foo**.go", "foo/main.go", false)
	if err != nil || matched {
		t.Fatalf("foo**.go should not cross a slash: matched=%v err=%v", matched, err)
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "GlobStarSegment",
		"glob":        "src/**.go",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, direct) || strings.Contains(out, nested) || strings.Contains(out, other) || strings.Contains(out, root) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("non-segment ** crossed a directory:\n%s", out)
	}
}

func TestToolRipgrepLeadingSlashAnchorsGlob(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "main.go")
	nested := filepath.Join(dir, "src", "main.go")
	deeper := filepath.Join(dir, "src", "sub", "main.go")
	for _, path := range []string{root, nested, deeper} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package main\nfunc LeadingSlashGlob() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	matched, err := matchGlob("/*.go", "main.go", false)
	if err != nil || !matched {
		t.Fatalf("/*.go should match main.go: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("/*.go", "src/main.go", false)
	if err != nil || matched {
		t.Fatalf("/*.go should not match src/main.go: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("/src/*.go", "src/main.go", false)
	if err != nil || !matched {
		t.Fatalf("/src/*.go should match src/main.go: matched=%v err=%v", matched, err)
	}
	for _, rel := range []string{"src/sub/main.go", "lib/src/main.go"} {
		matched, err = matchGlob("/src/*.go", rel, false)
		if err != nil || matched {
			t.Fatalf("/src/*.go should not match %s: matched=%v err=%v", rel, matched, err)
		}
	}
	matched, err = matchGlob("/Makefile", "Makefile", false)
	if err != nil || !matched {
		t.Fatalf("/Makefile should match the root file: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("/Makefile", "src/Makefile", false)
	if err != nil || matched {
		t.Fatalf("/Makefile should not match src/Makefile: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("/**/*.go", "src/sub/main.go", false)
	if err != nil || !matched {
		t.Fatalf("/**/*.go should match nested go files: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("*.go", "src/main.go", false)
	if err != nil || !matched {
		t.Fatalf("*.go should still match in any directory: matched=%v err=%v", matched, err)
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "LeadingSlashGlob",
		"glob":        "/*.go",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, root) || strings.Contains(out, nested) || strings.Contains(out, deeper) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("leading slash glob was not anchored at the root:\n%s", out)
	}
}

func TestToolRipgrepPathGlobFollowsOSCase(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "Src", "Main.GO")
	other := filepath.Join(dir, "other", "Main.GO")
	for _, path := range []string{file, other} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package main\nfunc PathGlobCaseToken() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wantFile := runtime.GOOS == "windows"
	matched, err := matchGlob("src/*.go", "Src/Main.GO", false)
	if err != nil || matched != wantFile {
		t.Fatalf("src/*.go match Src/Main.GO = %v, want %v (err=%v)", matched, wantFile, err)
	}
	matched, err = matchGlob("src/*.go", "other/Main.GO", false)
	if err != nil || matched {
		t.Fatalf("src/*.go should not match other/Main.GO: matched=%v err=%v", matched, err)
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "PathGlobCaseToken",
		"glob":        "src/*.go",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	gotFile := strings.Contains(out, file)
	if gotFile != wantFile || strings.Contains(out, other) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("path glob case filter want file=%v:\n%s", wantFile, out)
	}
}

func TestToolRipgrepBasenameGlobFollowsOSCase(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "Main.GO")
	other := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(file, []byte("package main\nfunc BaseGlobCaseToken() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("BaseGlobCaseToken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantFile := runtime.GOOS == "windows"
	for _, pattern := range []string{"*.go", "*.GO"} {
		matched, err := matchGlob(pattern, "Main.GO", false)
		if err != nil || matched != wantFile {
			t.Fatalf("%s match Main.GO = %v, want %v (err=%v)", pattern, matched, wantFile, err)
		}
	}
	matched, err := matchGlob("*.[ch]", "foo.C", false)
	if err != nil || matched != wantFile {
		t.Fatalf("*.[ch] match foo.C = %v, want %v (err=%v)", matched, wantFile, err)
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "BaseGlobCaseToken",
		"glob":        "*.go",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	gotFile := strings.Contains(out, file)
	if gotFile != wantFile || strings.Contains(out, other) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("basename glob case filter want file=%v:\n%s", wantFile, out)
	}
}

func TestToolRipgrepPathGlobCharacterClass(t *testing.T) {
	dir := t.TempDir()
	header := filepath.Join(dir, "src", "foo.h")
	source := filepath.Join(dir, "src", "sub", "foo.c")
	goFile := filepath.Join(dir, "src", "foo.go")
	for _, path := range []string{header, source, goFile} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package main\nfunc GlobClassToken() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"src/foo.h", "src/sub/foo.c"} {
		matched, err := matchGlob("src/**/*.[ch]", rel, false)
		if err != nil || !matched {
			t.Fatalf("src/**/*.[ch] should match %s: matched=%v err=%v", rel, matched, err)
		}
	}
	matched, err := matchGlob("src/*.[ch]", "src/foo.go", false)
	if err != nil || matched {
		t.Fatalf("src/*.[ch] should not match src/foo.go: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("src/*.[ch]", "src/sub/foo.c", false)
	if err != nil || matched {
		t.Fatalf("src/*.[ch] should not cross a directory: matched=%v err=%v", matched, err)
	}
	wantCase := runtime.GOOS == "windows"
	matched, err = matchGlob("src/*.[ch]", "src/foo.C", false)
	if err != nil || matched != wantCase {
		t.Fatalf("src/*.[ch] match src/foo.C = %v, want %v (err=%v)", matched, wantCase, err)
	}
	matched, err = matchGlob("src/*[.-0]*", "src/a.b", false)
	if err != nil || !matched {
		t.Fatalf("src/*[.-0]* should match src/a.b: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("src/*[.-0]*", "src/a/b", false)
	if err != nil || matched {
		t.Fatalf("src/*[.-0]* should not match across a slash: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("src/*.[!ch]", "src/foo.c", false)
	if err != nil || matched {
		t.Fatalf("src/*.[!ch] should not match src/foo.c: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("src/*.[!ch]", "src/foo.x", false)
	if err != nil || !matched {
		t.Fatalf("src/*.[!ch] should match src/foo.x: matched=%v err=%v", matched, err)
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "GlobClassToken",
		"glob":        "src/**/*.[ch]",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, header) || !strings.Contains(out, source) || strings.Contains(out, goFile) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("path character class glob filtered the wrong files:\n%s", out)
	}
}

func TestToolRipgrepBangClassGlob(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.x")
	nested := filepath.Join(dir, "src", "keep.x")
	skipC := filepath.Join(dir, "skip.c")
	skipH := filepath.Join(dir, "src", "skip.h")
	notes := filepath.Join(dir, "notes.md")
	for _, path := range []string{keep, nested, skipC, skipH, notes} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package main\nfunc BangClassToken() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"keep.x", "src/keep.x"} {
		matched, err := matchGlob("*.[!ch]", rel, false)
		if err != nil || !matched {
			t.Fatalf("*.[!ch] should match %s: matched=%v err=%v", rel, matched, err)
		}
		matched, err = matchGlob("**/*.[!ch]", rel, false)
		if err != nil || !matched {
			t.Fatalf("**/*.[!ch] should match %s: matched=%v err=%v", rel, matched, err)
		}
	}
	for _, rel := range []string{"skip.c", "src/skip.h", "notes.md"} {
		matched, err := matchGlob("*.[!ch]", rel, false)
		if err != nil || matched {
			t.Fatalf("*.[!ch] should not match %s: matched=%v err=%v", rel, matched, err)
		}
		matched, err = matchGlob("**/*.[!ch]", rel, false)
		if err != nil || matched {
			t.Fatalf("**/*.[!ch] should not match %s: matched=%v err=%v", rel, matched, err)
		}
	}
	wantUpper := runtime.GOOS != "windows"
	matched, err := matchGlob("*.[!ch]", "skip.C", false)
	if err != nil || matched != wantUpper {
		t.Fatalf("*.[!ch] match skip.C = %v, want %v (err=%v)", matched, wantUpper, err)
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "BangClassToken",
		"glob":        "**/*.[!ch]",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, keep) || !strings.Contains(out, nested) || strings.Contains(out, skipC) || strings.Contains(out, skipH) || strings.Contains(out, notes) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("bang class glob filtered the wrong files:\n%s", out)
	}
}

func TestToolRipgrepCaretClassGlob(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.x")
	nested := filepath.Join(dir, "src", "keep.x")
	skipC := filepath.Join(dir, "skip.c")
	skipH := filepath.Join(dir, "src", "skip.h")
	notes := filepath.Join(dir, "notes.md")
	for _, path := range []string{keep, nested, skipC, skipH, notes} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package main\nfunc CaretClassToken() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, pattern := range []string{"*.[^ch]", "**/*.[^ch]", "src/*.[^ch]"} {
		for _, rel := range []string{"keep.x", "src/keep.x"} {
			if pattern == "src/*.[^ch]" && rel == "keep.x" {
				continue
			}
			matched, err := matchGlob(pattern, rel, false)
			if err != nil || !matched {
				t.Fatalf("%s should match %s: matched=%v err=%v", pattern, rel, matched, err)
			}
		}
		for _, rel := range []string{"skip.c", "src/skip.h", "notes.md"} {
			matched, err := matchGlob(pattern, rel, false)
			if err != nil || matched {
				t.Fatalf("%s should not match %s: matched=%v err=%v", pattern, rel, matched, err)
			}
		}
	}
	// A ']' right after '[^' is a member. The class still negates the rest.
	matched, err := matchGlob("*.[^]a]", "keep.b", false)
	if err != nil || !matched {
		t.Fatalf("*.[^]a] should match keep.b: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("*.[^]a]", "keep.a", false)
	if err != nil || matched {
		t.Fatalf("*.[^]a] should not match keep.a: matched=%v err=%v", matched, err)
	}
	// [^] has no closer, so '^' stays a literal and the glob does not error.
	if _, err := matchGlob("*.[^]", "keep.x", false); err != nil {
		t.Fatalf("unclosed [^] returned an error: %v", err)
	}
	wantUpper := runtime.GOOS != "windows"
	matched, err = matchGlob("*.[^ch]", "skip.C", false)
	if err != nil || matched != wantUpper {
		t.Fatalf("*.[^ch] match skip.C = %v, want %v (err=%v)", matched, wantUpper, err)
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "CaretClassToken",
		"glob":        "**/*.[^ch]",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, keep) || !strings.Contains(out, nested) || strings.Contains(out, skipC) || strings.Contains(out, skipH) || strings.Contains(out, notes) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("caret class glob filtered the wrong files:\n%s", out)
	}
}

func TestToolRipgrepRepeatedBraceGlob(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "cmd", "main.go")
	rsFile := filepath.Join(dir, "lib", "sub", "main.rs")
	txtFile := filepath.Join(dir, "cmd", "main.txt")
	other := filepath.Join(dir, "src", "util.go")
	for _, path := range []string{goFile, rsFile, txtFile, other} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package main\nfunc BraceGlobToken() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"cmd/main.go", "lib/sub/main.rs"} {
		matched, err := matchGlob("{cmd,lib}/**/*.{go,rs}", rel, false)
		if err != nil || !matched {
			t.Fatalf("{cmd,lib}/**/*.{go,rs} should match %s: matched=%v err=%v", rel, matched, err)
		}
	}
	for _, rel := range []string{"cmd/main.txt", "src/util.go"} {
		matched, err := matchGlob("{cmd,lib}/**/*.{go,rs}", rel, false)
		if err != nil || matched {
			t.Fatalf("{cmd,lib}/**/*.{go,rs} should not match %s: matched=%v err=%v", rel, matched, err)
		}
	}
	for _, rel := range []string{"src/util.go", "cmd/main.go", "lib/main.go"} {
		matched, err := matchGlob("{src,{cmd,lib}}/**/*.go", rel, false)
		if err != nil || !matched {
			t.Fatalf("{src,{cmd,lib}}/**/*.go should match %s: matched=%v err=%v", rel, matched, err)
		}
	}
	matched, err := matchGlob("{src,{cmd,lib}}/**/*.go", "other/main.go", false)
	if err != nil || matched {
		t.Fatalf("nested brace glob should not match other/main.go: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("*.go,*.md", "src/util.go", false)
	if err != nil || !matched {
		t.Fatalf("comma outside braces should still split: matched=%v err=%v", matched, err)
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "BraceGlobToken",
		"glob":        "{cmd,lib}/**/*.{go,rs}",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, goFile) || !strings.Contains(out, rsFile) || strings.Contains(out, txtFile) || strings.Contains(out, other) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("repeated brace glob filtered the wrong files:\n%s", out)
	}
}

func TestToolRipgrepEmptyBraceAlternative(t *testing.T) {
	if got, want := expandSearchGlobBraces("file{,.go}", 0), []string{"file", "file.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("file{,.go} = %#v, want %#v", got, want)
	}
	if got, want := expandSearchGlobBraces("{src/,}main.go", 0), []string{"src/main.go", "main.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("{src/,}main.go = %#v, want %#v", got, want)
	}
	if got, want := expandSearchGlobBraces("x{{a,},b}", 0), []string{"xa", "x", "xb"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("x{{a,},b} = %#v, want %#v", got, want)
	}
	// A group with only empty alternatives is not "every file".
	if got, want := expandSearchGlobBraces("{,}", 0), []string{"{,}"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("{,} = %#v, want %#v", got, want)
	}
	for _, rel := range []string{"file", "file.go", "dir/file", "dir/file.go"} {
		matched, err := matchGlob("file{,.go}", rel, false)
		if err != nil || !matched {
			t.Fatalf("file{,.go} should match %s: matched=%v err=%v", rel, matched, err)
		}
	}
	matched, err := matchGlob("file{,.go}", "file.txt", false)
	if err != nil || matched {
		t.Fatalf("file{,.go} should not match file.txt: matched=%v err=%v", matched, err)
	}
	// The empty alternative is a basename glob, so main.go matches in any directory.
	// src/main.go alone does not, which is how the dropped alternative used to behave.
	for _, rel := range []string{"main.go", "src/main.go", "lib/main.go"} {
		matched, err := matchGlob("{src/,}main.go", rel, false)
		if err != nil || !matched {
			t.Fatalf("{src/,}main.go should match %s: matched=%v err=%v", rel, matched, err)
		}
	}
	matched, err = matchGlob("src/main.go", "main.go", false)
	if err != nil || matched {
		t.Fatalf("src/main.go alone should not match main.go: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("{src/,}main.go", "src/main.rs", false)
	if err != nil || matched {
		t.Fatalf("{src/,}main.go should not match src/main.rs: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("{,}", "main.go", false)
	if err != nil || matched {
		t.Fatalf("{,} should stay literal and not match main.go: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("{,}", "{,}", false)
	if err != nil || !matched {
		t.Fatalf("{,} should match a file named {,}: matched=%v err=%v", matched, err)
	}
	// Gitignore lines expand braces too. An empty alternative stays.
	matched, err = matchGlob("\x00file{,.go}", "file", false)
	if err != nil || !matched {
		t.Fatalf("gitignore file{,.go} should match file: matched=%v err=%v", matched, err)
	}

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Distinct directories so one path is not a prefix of another in the output.
	plain := filepath.Join(dir, "plain", "file")
	goFile := filepath.Join(dir, "src", "file.go")
	txtFile := filepath.Join(dir, "notes", "file.txt")
	for _, path := range []string{plain, goFile, txtFile} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("EmptyBraceToken\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "EmptyBraceToken",
		"glob":        "file{,.go}",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, plain) || !strings.Contains(out, goFile) || strings.Contains(out, txtFile) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("empty brace alternative filtered the wrong files:\n%s", out)
	}
}

func TestToolRipgrepSearchesWithMultipleGlobFilters(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	mdFile := filepath.Join(dir, "notes.md")
	txtFile := filepath.Join(dir, "notes.txt")
	for path, content := range map[string]string{
		goFile:  "package main\nfunc Target() {}\n",
		mdFile:  "Target in markdown\n",
		txtFile: "Target in text\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "Target",
		"glob":        "**/*.{go,md}",
		"output_mode": "files_with_matches",
		"max_results": float64(10),
	})
	if !strings.Contains(out, goFile) || !strings.Contains(out, mdFile) {
		t.Fatalf("ripgrep multi-glob missing expected files:\n%s", out)
	}
	if strings.Contains(out, txtFile) {
		t.Fatalf("ripgrep multi-glob matched excluded file:\n%s", out)
	}
}

func TestToolRipgrepSearchesWithTypeFilter(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	mdFile := filepath.Join(dir, "notes.md")
	for path, content := range map[string]string{
		goFile: "package main\nfunc Target() {}\n",
		mdFile: "Target in markdown\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "Target",
		"type":        "go",
		"output_mode": "files_with_matches",
		"max_results": float64(10),
	})
	if !strings.Contains(out, goFile) {
		t.Fatalf("ripgrep type filter missing go file:\n%s", out)
	}
	if strings.Contains(out, mdFile) {
		t.Fatalf("ripgrep type filter matched markdown file:\n%s", out)
	}
}

func TestToolRipgrepTypeFilterAppliesToFullScan(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	mdFile := filepath.Join(dir, "notes.md")
	for path, content := range map[string]string{
		goFile: "package main\nvar ID = 1\n",
		mdFile: "ID in markdown\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "ID",
		"type":        "md",
		"output_mode": "files_with_matches",
		"max_results": float64(10),
	})
	if !strings.Contains(out, mdFile) {
		t.Fatalf("ripgrep full-scan type filter missing markdown file:\n%s", out)
	}
	if strings.Contains(out, goFile) {
		t.Fatalf("ripgrep full-scan type filter matched go file:\n%s", out)
	}
}

func TestToolRipgrepSearchesWithAdditionalTypeAliases(t *testing.T) {
	dir := t.TempDir()
	protoFile := filepath.Join(dir, "service.proto")
	makeFile := filepath.Join(dir, "Makefile")
	cmakeFile := filepath.Join(dir, "CMakeLists.txt")
	gradleFile := filepath.Join(dir, "build.gradle.kts")
	otherFile := filepath.Join(dir, "notes.txt")
	for _, path := range []string{protoFile, makeFile, cmakeFile, gradleFile, otherFile} {
		if err := os.WriteFile(path, []byte("TargetBuildAlias\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "TargetBuildAlias",
		"type":        "proto,make,cmake,gradle",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	for _, want := range []string{protoFile, makeFile, cmakeFile, gradleFile} {
		if !strings.Contains(out, want) {
			t.Fatalf("ripgrep additional type aliases missing %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, otherFile) {
		t.Fatalf("ripgrep additional type aliases matched unrelated file:\n%s", out)
	}
	if !strings.Contains(out, "mode=indexed") {
		t.Fatalf("ripgrep additional type aliases should use index:\n%s", out)
	}
}

func TestToolRipgrepExcludeFilterAppliesToIndexedSearch(t *testing.T) {
	dir := t.TempDir()
	keepDir := filepath.Join(dir, "src")
	excludedDir := filepath.Join(dir, "generated")
	if err := os.MkdirAll(keepDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(excludedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	keepFile := filepath.Join(keepDir, "main.go")
	excludedFile := filepath.Join(excludedDir, "main.go")
	for path := range map[string]bool{keepFile: true, excludedFile: true} {
		if err := os.WriteFile(path, []byte("package main\nfunc Target() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "Target",
		"glob":        "**/*.go",
		"exclude":     "generated/**",
		"output_mode": "files_with_matches",
		"max_results": float64(10),
	})
	if !strings.Contains(out, keepFile) {
		t.Fatalf("ripgrep exclude filter missing kept file:\n%s", out)
	}
	if strings.Contains(out, excludedFile) {
		t.Fatalf("ripgrep exclude filter matched excluded file:\n%s", out)
	}
}

func TestToolRipgrepExcludeFilterAppliesToFullScan(t *testing.T) {
	dir := t.TempDir()
	keepFile := filepath.Join(dir, "keep.md")
	excludedFile := filepath.Join(dir, "skip.md")
	for _, path := range []string{keepFile, excludedFile} {
		if err := os.WriteFile(path, []byte("ID\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "ID",
		"exclude":     "skip.md",
		"output_mode": "files_with_matches",
		"max_results": float64(10),
	})
	if !strings.Contains(out, keepFile) {
		t.Fatalf("ripgrep full-scan exclude missing kept file:\n%s", out)
	}
	if strings.Contains(out, excludedFile) {
		t.Fatalf("ripgrep full-scan exclude matched excluded file:\n%s", out)
	}
}

func TestToolRipgrepCombinesExcludeAndExcludeGlob(t *testing.T) {
	dir := t.TempDir()
	keepFile := filepath.Join(dir, "keep.go")
	excludedByExclude := filepath.Join(dir, "generated.go")
	excludedByCompat := filepath.Join(dir, "snapshot.go")
	for _, path := range []string{keepFile, excludedByExclude, excludedByCompat} {
		if err := os.WriteFile(path, []byte("package main\nfunc TargetCombinedExclude() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":         dir,
		"pattern":      "TargetCombinedExclude",
		"glob":         "**/*.go",
		"exclude":      "generated.go",
		"exclude_glob": "snapshot.go",
		"output_mode":  "files_with_matches",
	})
	if !strings.Contains(out, keepFile) || strings.Contains(out, excludedByExclude) || strings.Contains(out, excludedByCompat) {
		t.Fatalf("ripgrep should combine exclude and exclude_glob:\n%s", out)
	}
}

func TestToolRipgrepRootGitignoreAppliesToIndexedSearch(t *testing.T) {
	dir := t.TempDir()
	keepFile := filepath.Join(dir, "src", "main.go")
	ignoredFile := filepath.Join(dir, "ignored_dir", "main.go")
	if err := os.MkdirAll(filepath.Dir(keepFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(ignoredFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored_dir/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keepFile, []byte("package main\nfunc TargetGitignore() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ignoredFile, []byte("package main\nfunc TargetGitignore() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "TargetGitignore",
		"glob":        "**/*.go",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, keepFile) || strings.Contains(out, ignoredFile) {
		t.Fatalf("ripgrep should apply supported .gitignore rules:\n%s", out)
	}
	if !strings.Contains(out, "mode=indexed") {
		t.Fatalf("ripgrep should stay on indexed path with .gitignore:\n%s", out)
	}
}

func TestToolRipgrepRootGitignoreAppliesToFullScan(t *testing.T) {
	dir := t.TempDir()
	keepFile := filepath.Join(dir, "keep.md")
	ignoredFile := filepath.Join(dir, "ignored.md")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{keepFile, ignoredFile} {
		if err := os.WriteFile(path, []byte("ID\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "ID",
		"output_mode": "files_with_matches",
	})
	if !strings.Contains(out, keepFile) || strings.Contains(out, ignoredFile) {
		t.Fatalf("ripgrep full-scan should apply supported .gitignore rules:\n%s", out)
	}
}

func TestToolRipgrepNoIgnoreIncludesIgnoredFiles(t *testing.T) {
	dir := t.TempDir()
	keepFile := filepath.Join(dir, "src", "main.go")
	ignoredFile := filepath.Join(dir, "ignored_dir", "main.go")
	for _, path := range []string{keepFile, ignoredFile} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package main\nfunc TargetNoIgnore() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored_dir/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "TargetNoIgnore",
		"glob":        "**/*.go",
		"output_mode": "files_with_matches",
		"no_ignore":   true,
		"stats":       true,
	})
	if !strings.Contains(out, keepFile) || !strings.Contains(out, ignoredFile) {
		t.Fatalf("ripgrep no_ignore should include ignored files:\n%s", out)
	}
	if !strings.Contains(out, "mode=indexed") {
		t.Fatalf("ripgrep no_ignore should still use local index:\n%s", out)
	}
}

func TestToolRipgrepRootGitignoreAnchoredPatternDoesNotIgnoreNestedPath(t *testing.T) {
	dir := t.TempDir()
	rootIgnored := filepath.Join(dir, "ignored.go")
	nestedKeep := filepath.Join(dir, "nested", "ignored.go")
	if err := os.MkdirAll(filepath.Dir(nestedKeep), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("/ignored.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{rootIgnored, nestedKeep} {
		if err := os.WriteFile(path, []byte("package main\nfunc TargetAnchoredIgnore() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "TargetAnchoredIgnore",
		"glob":        "**/*.go",
		"output_mode": "files_with_matches",
	})
	if !strings.Contains(out, nestedKeep) || strings.Contains(out, rootIgnored) {
		t.Fatalf("anchored .gitignore pattern should ignore only root path:\n%s", out)
	}
}

func TestToolRipgrepGitignoreDotSlashIsNotAnchor(t *testing.T) {
	patterns, ok := gitignoreExcludePatterns("./kept.go\n./*.log\nfoo/./b.go\n\\./escaped.go\n*.tmp\n")
	if !ok {
		t.Fatal("gitignoreExcludePatterns rejected a ./ pattern")
	}
	exclude := joinSearchIgnorePatterns(patterns)
	for _, rel := range []string{"kept.go", "a.log", "sub/a.log", "foo/b.go", "escaped.go"} {
		matched, err := matchGlob(exclude, rel, false)
		if err != nil || matched {
			t.Fatalf("./ gitignore pattern should not exclude %s: matched=%v err=%v exclude=%q", rel, matched, err, exclude)
		}
	}
	for _, rel := range []string{"a.tmp", "sub/a.tmp"} {
		matched, err := matchGlob(exclude, rel, false)
		if err != nil || !matched {
			t.Fatalf("*.tmp should still exclude %s: matched=%v err=%v", rel, matched, err)
		}
	}
	files, dirs, ok := gitignoreDirectPatterns("./kept.go\n./*.log\n*.tmp\n")
	if !ok || len(dirs) != 0 || len(files) != 1 || files[0] != "*.tmp" {
		t.Fatalf("parent direct patterns = %v %v ok=%v", files, dirs, ok)
	}
	// A user glob and a leading "/" anchor still use the "./" encoding.
	matched, err := matchGlob("./kept.go", "kept.go", false)
	if err != nil || !matched {
		t.Fatalf("user glob ./kept.go should match kept.go: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("./kept.go", "sub/kept.go", false)
	if err != nil || matched {
		t.Fatalf("user glob ./kept.go should not match sub/kept.go: matched=%v err=%v", matched, err)
	}
	anchored, ok := gitignoreExcludePatterns("/kept.go\n")
	if !ok {
		t.Fatal("anchored pattern rejected")
	}
	anchoredExclude := joinSearchIgnorePatterns(anchored)
	matched, err = matchGlob(anchoredExclude, "kept.go", false)
	if err != nil || !matched {
		t.Fatalf("/kept.go should exclude kept.go: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob(anchoredExclude, "sub/kept.go", false)
	if err != nil || matched {
		t.Fatalf("/kept.go should not exclude sub/kept.go: matched=%v err=%v", matched, err)
	}

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("./kept.go\n./*.log\n*.tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(dir, "kept.go")
	nestedKept := filepath.Join(dir, "sub", "kept.go")
	logFile := filepath.Join(dir, "drop.log")
	nestedLog := filepath.Join(dir, "sub", "drop.log")
	tmpFile := filepath.Join(dir, "drop.tmp")
	nestedTmp := filepath.Join(dir, "sub", "drop.tmp")
	for _, path := range []string{kept, nestedKept, logFile, nestedLog, tmpFile, nestedTmp} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("DotSlashIgnoreToken\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "DotSlashIgnoreToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	for _, path := range []string{kept, nestedKept, logFile, nestedLog} {
		if !strings.Contains(out, path) {
			t.Fatalf("./ gitignore hid %s:\n%s", path, out)
		}
	}
	if strings.Contains(out, tmpFile) || strings.Contains(out, nestedTmp) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("./ gitignore filtered the wrong files:\n%s", out)
	}
}

func TestToolRipgrepGitignoreDirGlobstarKeepsDirectFiles(t *testing.T) {
	assertIgnored := func(content string, rel string, want bool) {
		t.Helper()
		patterns, ok := gitignoreExcludePatterns(content)
		if !ok {
			t.Fatalf("gitignoreExcludePatterns rejected %q", content)
		}
		matched, err := matchGlob(joinSearchIgnorePatterns(patterns), rel, false)
		if err != nil || matched != want {
			t.Fatalf("%q on %s: matched=%v err=%v, want %v", content, rel, matched, err, want)
		}
	}
	// foo/**/ ignores directories under foo, not files directly in foo.
	assertIgnored("foo/**/\n", "foo/file.go", false)
	assertIgnored("foo/**/\n", "foo/sub/file.go", true)
	assertIgnored("foo/**/\n", "foo/sub/deep/file.go", true)
	assertIgnored("foo/**/\n", "root.go", false)
	assertIgnored("foo/***/\n", "foo/file.go", false)
	assertIgnored("foo/***/\n", "foo/sub/file.go", true)
	// **/ ignores every nested file and keeps a root file.
	assertIgnored("**/\n", "root.go", false)
	assertIgnored("**/\n", "foo/file.go", true)
	// These still hide the files directly inside foo.
	assertIgnored("foo/\n", "foo/file.go", true)
	assertIgnored("foo/**\n", "foo/file.go", true)
	assertIgnored("foo/*/\n", "foo/file.go", false)
	assertIgnored("foo/*/\n", "foo/sub/file.go", true)
	for _, rel := range []string{"foo/bar.go", "foo/sub/bar.go", "foo/a/b/bar.go"} {
		matched, err := matchGlob("foo/***/bar.go", rel, false)
		if err != nil || !matched {
			t.Fatalf("foo/***/bar.go should match %s: matched=%v err=%v", rel, matched, err)
		}
	}
	matched, err := matchGlob("foo/***/bar.go", "foo/baz.go", false)
	if err != nil || matched {
		t.Fatalf("foo/***/bar.go should not match foo/baz.go: matched=%v err=%v", matched, err)
	}

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("foo/**/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "root.go")
	direct := filepath.Join(dir, "foo", "file.go")
	nested := filepath.Join(dir, "foo", "sub", "file.go")
	for _, path := range []string{root, direct, nested} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("DirGlobstarToken\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "DirGlobstarToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, root) || !strings.Contains(out, direct) || strings.Contains(out, nested) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("foo/**/ filtered the wrong files:\n%s", out)
	}
}

func TestToolGlobRootGitignoreAnchoredDirectoryDoesNotIgnoreNestedDirectory(t *testing.T) {
	dir := t.TempDir()
	rootIgnored := filepath.Join(dir, "generated", "root.go")
	nestedKeep := filepath.Join(dir, "pkg", "generated", "keep.go")
	for _, path := range []string{rootIgnored, nestedKeep} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("/generated/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolGlob(map[string]interface{}{
		"path":    dir,
		"pattern": "**/*.go",
	})
	if !strings.Contains(out, nestedKeep) || strings.Contains(out, rootIgnored) {
		t.Fatalf("anchored .gitignore dir should ignore only root directory:\n%s", out)
	}
}

func TestToolGlobRootGitignoreAnchoredDirectoryWithoutSlashIgnoresContents(t *testing.T) {
	dir := t.TempDir()
	rootIgnored := filepath.Join(dir, "generated", "root.go")
	nestedKeep := filepath.Join(dir, "pkg", "generated", "keep.go")
	for _, path := range []string{rootIgnored, nestedKeep} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("/generated\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolGlob(map[string]interface{}{
		"path":    dir,
		"pattern": "**/*.go",
	})
	if !strings.Contains(out, nestedKeep) || strings.Contains(out, rootIgnored) {
		t.Fatalf("anchored .gitignore dir without slash should ignore root directory contents:\n%s", out)
	}
}

func TestToolGlobGitignoreNestedDirectoryWithoutSlashIgnoresContents(t *testing.T) {
	dir := t.TempDir()
	ignored := filepath.Join(dir, "pkg", "generated", "skip.go")
	keep := filepath.Join(dir, "pkg", "other", "keep.go")
	for _, path := range []string{ignored, keep} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("pkg/generated\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolGlob(map[string]interface{}{
		"path":    dir,
		"pattern": "**/*.go",
	})
	if !strings.Contains(out, keep) || strings.Contains(out, ignored) {
		t.Fatalf("nested .gitignore dir without slash should ignore directory contents:\n%s", out)
	}
}

func TestToolRipgrepRootIgnoreFilesApplyTogether(t *testing.T) {
	dir := t.TempDir()
	keepFile := filepath.Join(dir, "keep.go")
	ignoredByIgnore := filepath.Join(dir, "ignored-by-ignore.go")
	ignoredByGitInfo := filepath.Join(dir, "ignored-by-info.go")
	for _, path := range []string{keepFile, ignoredByIgnore, ignoredByGitInfo} {
		if err := os.WriteFile(path, []byte("package main\nfunc TargetRootIgnore() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".ignore"), []byte("ignored-by-ignore.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	infoDir := filepath.Join(dir, ".git", "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(infoDir, "exclude"), []byte("ignored-by-info.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "TargetRootIgnore",
		"glob":        "**/*.go",
		"output_mode": "files_with_matches",
	})
	if !strings.Contains(out, keepFile) || strings.Contains(out, ignoredByIgnore) || strings.Contains(out, ignoredByGitInfo) {
		t.Fatalf("ripgrep should apply .ignore and .git/info/exclude:\n%s", out)
	}
}

func TestGitignoreExcludePatternsDisableOnNegation(t *testing.T) {
	patterns, ok := gitignoreExcludePatterns("vendor/\n!important.txt\n")
	if ok || len(patterns) != 0 {
		t.Fatalf("gitignore negation should disable conservative conversion, ok=%v patterns=%#v", ok, patterns)
	}
}

func TestToolRipgrepGitignoreKeepsSpacesAndCommas(t *testing.T) {
	dir := t.TempDir()
	spaced := filepath.Join(dir, "my notes.txt")
	nested := filepath.Join(dir, "src", "my notes.txt")
	commaName := filepath.Join(dir, "foo,bar.txt")
	keep := filepath.Join(dir, "keep.txt")
	for _, path := range []string{spaced, nested, commaName, keep} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package main\nfunc GitignoreSpaceToken() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("my notes.txt\nfoo,bar.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exclude := resolveSearchExcludePattern(map[string]interface{}{}, dir)
	for _, rel := range []string{"my notes.txt", "src/my notes.txt", "foo,bar.txt"} {
		matched, err := matchGlob(exclude, rel, false)
		if err != nil || !matched {
			t.Fatalf("gitignore pattern should exclude %s: matched=%v err=%v exclude=%q", rel, matched, err, exclude)
		}
	}
	matched, err := matchGlob(exclude, "keep.txt", false)
	if err != nil || matched {
		t.Fatalf("gitignore pattern should not exclude keep.txt: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("*.txt *.md", "src/a.md", false)
	if err != nil || !matched {
		t.Fatalf("user glob should still split on spaces: matched=%v err=%v", matched, err)
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "GitignoreSpaceToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, keep) || strings.Contains(out, spaced) || strings.Contains(out, nested) || strings.Contains(out, commaName) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("gitignore space or comma pattern was split:\n%s", out)
	}
}

func TestToolRipgrepGitignoreUnclosedClassIsLiteral(t *testing.T) {
	dir := t.TempDir()
	literal := filepath.Join(dir, "foo[")
	other := filepath.Join(dir, "foo[bar.go")
	logFile := filepath.Join(dir, "debug.log")
	keep := filepath.Join(dir, "keep.go")
	for _, path := range []string{literal, other, logFile, keep} {
		if err := os.WriteFile(path, []byte("package main\nfunc GitignoreBracketToken() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\nfoo[\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	matched, err := matchGlob("foo[", "foo[", false)
	if err != nil || !matched {
		t.Fatalf("unclosed class should match the literal name: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("foo[", "foo[bar.go", false)
	if err != nil || matched {
		t.Fatalf("unclosed class should not match foo[bar.go: matched=%v err=%v", matched, err)
	}
	exclude := resolveSearchExcludePattern(map[string]interface{}{}, dir)
	matched, err = matchGlob(exclude, "debug.log", false)
	if err != nil || !matched {
		t.Fatalf("*.log should still exclude debug.log: matched=%v err=%v exclude=%q", matched, err, exclude)
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "GitignoreBracketToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	// foo[ is a prefix of foo[bar.go, so the paths have to match whole lines.
	found := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		found[strings.TrimSpace(line)] = true
	}
	if !found[keep] || !found[other] || found[literal] || found[logFile] || !strings.Contains(out, "mode=indexed") || strings.Contains(out, "search failed") {
		t.Fatalf("unclosed gitignore class broke the search:\n%s", out)
	}
}

func TestToolRipgrepGitignoreBackslashEscape(t *testing.T) {
	dir := t.TempDir()
	braceName := filepath.Join(dir, "{keep,drop}.txt")
	keep := filepath.Join(dir, "keep.txt")
	hash := filepath.Join(dir, "#hash.txt")
	bang := filepath.Join(dir, "!bang.txt")
	bracket := filepath.Join(dir, "dir", "[file].txt")
	bar := filepath.Join(dir, "bar.txt")
	tmp := filepath.Join(dir, "debug.tmp")
	for _, path := range []string{braceName, keep, hash, bang, bracket, bar, tmp} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package main\nfunc GitignoreEscapeToken() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ignore := "\\{keep,drop}.txt\n\\#hash.txt\n\\!bang.txt\ndir/\\[file].txt\nbar\\\n*.tmp\nfoo\\ \n\\*.log\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(ignore), 0o644); err != nil {
		t.Fatal(err)
	}
	exclude := resolveSearchExcludePattern(map[string]interface{}{}, dir)
	checks := []struct {
		rel  string
		want bool
	}{
		{"{keep,drop}.txt", true},
		{"keep.txt", false},
		{"drop.txt", false},
		{"#hash.txt", true},
		{"!bang.txt", true},
		{"dir/[file].txt", true},
		{"dir/file.txt", false},
		{"bar.txt", false},
		{"bar", false},
		{"debug.tmp", true},
		{"*.log", true},
		{"keep.log", false},
		{"src/*.log", true},
		{"foo ", true},
		{"foo", false},
	}
	for _, check := range checks {
		matched, err := matchGlob(exclude, check.rel, false)
		if err != nil || matched != check.want {
			t.Fatalf("gitignore %q matched=%v want=%v err=%v exclude=%q", check.rel, matched, check.want, err, exclude)
		}
	}
	backslashClass := searchGlobRegexp("[\\]")
	if !regexp.MustCompile(backslashClass).MatchString("\\") {
		t.Fatalf("character class should match a literal backslash: %s", backslashClass)
	}
	if runtime.GOOS == "windows" {
		matched, err := matchGlob(`src\*.go`, "src/main.go", false)
		if err != nil || !matched {
			t.Fatalf(`user glob src\*.go should match src/main.go: matched=%v err=%v`, matched, err)
		}
		matched, err = matchGlob(`src\*.go`, "src/pkg/main.go", false)
		if err != nil || matched {
			t.Fatalf(`user glob src\*.go should not cross another directory: matched=%v err=%v`, matched, err)
		}
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "GitignoreEscapeToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	found := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		found[strings.TrimSpace(line)] = true
	}
	if !found[keep] || !found[bar] || found[braceName] || found[hash] || found[bang] || found[bracket] || found[tmp] || !strings.Contains(out, "mode=indexed") || strings.Contains(out, "search failed") {
		t.Fatalf("gitignore backslash escape broke the search:\n%s", out)
	}
}

func TestToolRipgrepGitignoreClassEscape(t *testing.T) {
	assertIgnored := func(content, rel string, want bool) {
		t.Helper()
		patterns, ok := gitignoreExcludePatterns(content)
		if !ok {
			t.Fatalf("gitignoreExcludePatterns rejected %q", content)
		}
		matched, err := matchGlob(joinSearchIgnorePatterns(patterns), rel, false)
		if err != nil || matched != want {
			t.Fatalf("%q on %s: matched=%v err=%v, want %v", content, rel, matched, err, want)
		}
	}
	for _, rel := range []string{"a", "*", "b", "sub/a"} {
		assertIgnored("[a\\*b]\n", rel, true)
	}
	for _, rel := range []string{"c", "ab", "star"} {
		assertIgnored("[a\\*b]\n", rel, false)
	}
	for _, rel := range []string{"a", "-", "c"} {
		assertIgnored("[a\\-c]\n", rel, true)
	}
	assertIgnored("[a\\-c]\n", "b", false)
	for _, rel := range []string{"a", "]", "b"} {
		assertIgnored("[a\\]b]\n", rel, true)
	}
	assertIgnored("[a\\]b]\n", "c", false)
	assertIgnored("[^a\\]b]\n", "c", true)
	assertIgnored("[^a\\]b]\n", "x", true)
	for _, rel := range []string{"a", "]", "b"} {
		assertIgnored("[^a\\]b]\n", rel, false)
		assertIgnored("[!a\\]b]\n", rel, false)
	}
	assertIgnored("[!a\\]b]\n", "x", true)
	assertIgnored("[a-c]\n\\*.log\n", "b", true)
	assertIgnored("[a-c]\n\\*.log\n", "d", false)
	assertIgnored("[a-c]\n\\*.log\n", "keep.log", false)
	assertIgnored("\\[file].txt\n", "[file].txt", true)
	assertIgnored("\\[file].txt\n", "file.txt", false)

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("[a\\-c]\n[a\\]b]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "keep.go")
	other := filepath.Join(dir, "d")
	ignored := []string{
		filepath.Join(dir, "a"),
		filepath.Join(dir, "b"),
		filepath.Join(dir, "c"),
		filepath.Join(dir, "-"),
		filepath.Join(dir, "]"),
	}
	for _, path := range append([]string{keep, other}, ignored...) {
		if err := os.WriteFile(path, []byte("ClassEscapeToken\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "ClassEscapeToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, keep) || !strings.Contains(out, other) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("class escapes hid a file git keeps:\n%s", out)
	}
	for _, path := range ignored {
		if strings.Contains(out, path) {
			t.Fatalf("class escape kept %s:\n%s", path, out)
		}
	}
}

func TestToolRipgrepGitignorePosixClass(t *testing.T) {
	assertIgnored := func(content, rel string, want bool) {
		t.Helper()
		patterns, ok := gitignoreExcludePatterns(content)
		if !ok {
			t.Fatalf("gitignoreExcludePatterns rejected %q", content)
		}
		matched, err := matchGlob(joinSearchIgnorePatterns(patterns), rel, false)
		if err != nil || matched != want {
			t.Fatalf("%q on %q: matched=%v err=%v, want %v", content, rel, matched, err, want)
		}
	}
	assertGlob := func(pattern, rel string, want bool) {
		t.Helper()
		matched, err := matchGlob(pattern, rel, false)
		if err != nil || matched != want {
			t.Fatalf("glob %q on %q: matched=%v err=%v, want %v", pattern, rel, matched, err, want)
		}
	}
	for _, rel := range []string{"0", "5", "9"} {
		assertIgnored("[[:digit:]]\n", rel, true)
	}
	for _, rel := range []string{"a", "Z", "x", "ab"} {
		assertIgnored("[[:digit:]]\n", rel, false)
	}
	for _, rel := range []string{"a", "b", "Z", "x"} {
		assertIgnored("[[:alpha:]]\n", rel, true)
	}
	for _, rel := range []string{"0", "5", "9", "ab", "A1"} {
		assertIgnored("[[:alpha:]]\n", rel, false)
	}
	for _, rel := range []string{"a", "Z", "0", "5"} {
		assertIgnored("[[:alnum:]]\n", rel, true)
		assertIgnored("[[:digit:][:alpha:]]\n", rel, true)
	}
	assertIgnored("[[:alnum:]]\n", "!", false)
	assertIgnored("[[:digit:][:alpha:]]\n", "!", false)
	assertIgnored("[[:space:]]\n", " ", true)
	assertIgnored("[[:space:]]\n", "\t", true)
	assertIgnored("[[:space:]]\n", "\n", true)
	assertIgnored("[[:space:]]\n", "a", false)
	assertIgnored("[[:blank:]]\n", " ", true)
	assertIgnored("[[:blank:]]\n", "\t", true)
	assertIgnored("[[:blank:]]\n", "\n", false)
	assertIgnored("[[:blank:]]\n", "a", false)
	for _, rel := range []string{"!", "-", "_"} {
		assertIgnored("[[:punct:]]\n", rel, true)
	}
	for _, rel := range []string{"a", "0"} {
		assertIgnored("[[:punct:]]\n", rel, false)
	}
	for _, rel := range []string{"a", "5", "F", "f"} {
		assertIgnored("[[:xdigit:]]\n", rel, true)
	}
	for _, rel := range []string{"x", "Z"} {
		assertIgnored("[[:xdigit:]]\n", rel, false)
	}
	assertIgnored("[[:graph:]]\n", "!", true)
	assertIgnored("[[:graph:]]\n", " ", false)
	assertIgnored("[[:print:]]\n", " ", true)
	assertIgnored("[[:print:]]\n", "!", true)
	assertIgnored("[[:print:]]\n", "\n", false)
	assertIgnored("[[:cntrl:]]\n", "\n", true)
	assertIgnored("[[:cntrl:]]\n", "a", false)
	assertIgnored("[[:cntrl:]]\n", " ", false)
	assertIgnored("[[:foo:]]\n", "a", false)
	assertIgnored("[[:foo:]]\n", "[[:foo:]]", false)
	assertIgnored("[[:foo:]a]\n", "a", false)
	assertIgnored("[[:foo:]a]\n", "5", false)
	assertIgnored("src/[[:digit:]].go\n", "src/5.go", true)
	assertIgnored("src/[[:digit:]].go\n", "src/a.go", false)
	assertIgnored("src/[[:digit:]].go\n", "src/sub/5.go", false)
	assertIgnored("[![:digit:]]\n", "a", true)
	assertIgnored("[![:digit:]]\n", "Z", true)
	assertIgnored("[![:digit:]]\n", "5", false)
	assertIgnored("[![:digit:]]\n", "0", false)
	assertIgnored("[^[:digit:]]\n", "a", true)
	assertIgnored("[^[:digit:]]\n", "5", false)
	assertIgnored("[][:digit:]]\n", "]", true)
	assertIgnored("[][:digit:]]\n", "5", true)
	assertIgnored("[][:digit:]]\n", "a", false)
	assertIgnored("[][:digit:]]\n", "[", false)
	assertIgnored("[:digit]\n", "d", true)
	assertIgnored("[:digit]\n", ":", true)
	assertIgnored("[:digit]\n", "5", false)
	assertIgnored("[[:digit]\n", "[", true)
	assertIgnored("[[:digit]\n", "5", false)
	assertIgnored("[[:digit:\n", "[[:digit:", true)
	assertIgnored("[[:digit:\n", "5", false)
	if runtime.GOOS == "windows" {
		assertIgnored("[[:upper:]]\n", "a", true)
		assertIgnored("[[:upper:]]\n", "Z", true)
		assertIgnored("[[:lower:]]\n", "a", true)
		assertIgnored("[[:lower:]]\n", "Z", true)
	} else {
		assertIgnored("[[:upper:]]\n", "Z", true)
		assertIgnored("[[:upper:]]\n", "a", false)
		assertIgnored("[[:lower:]]\n", "a", true)
		assertIgnored("[[:lower:]]\n", "Z", false)
	}
	assertGlob("[[:digit:]],*.go", "5", true)
	assertGlob("[[:digit:]],*.go", "main.go", true)
	assertGlob("[[:digit:]],*.go", "abc", false)
	assertGlob("[[:digit:],a]", "5", true)
	assertGlob("[[:digit:],a]", "a", true)
	assertGlob("[[:digit:],a]", ",", true)
	assertGlob("[[:digit:],a]", "b", false)
	assertGlob("{[[:digit:]],a}", "5", true)
	assertGlob("{[[:digit:]],a}", "a", true)
	assertGlob("{[[:digit:]],a}", "b", false)
	assertGlob("{[[:digit:],a]}", "5", true)
	assertGlob("{[[:digit:],a]}", "a", true)
	assertGlob("{[[:digit:],a]}", ",", true)
	assertGlob("{[[:digit:],a]}", "b", false)
	assertGlob("foo[[:digit:]].txt", "foo5.txt", true)
	assertGlob("foo[[:digit:]].txt", "fooa.txt", false)
	assertGlob("foo[[:digit:]].txt", "foo.txt", false)
	assertGlob("**/a[[:punct:]]b", "a!b", true)
	assertGlob("**/a[[:punct:]]b", "a/b", false)
	assertGlob("**/a[[:graph:]]b", "a!b", true)
	assertGlob("**/a[[:graph:]]b", "a/b", false)
	assertGlob("**/a[[:print:]]b", "a b", true)
	assertGlob("**/a[[:print:]]b", "a/b", false)

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("[[:digit:]]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "keep.go")
	letter := filepath.Join(dir, "Z")
	ignored := []string{
		filepath.Join(dir, "5"),
		filepath.Join(dir, "9"),
	}
	for _, path := range append([]string{keep, letter}, ignored...) {
		if err := os.WriteFile(path, []byte("PosixClassToken\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "PosixClassToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, keep) || !strings.Contains(out, letter) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("posix class hid a file git keeps:\n%s", out)
	}
	for _, path := range ignored {
		if strings.Contains(out, path) {
			t.Fatalf("posix class kept %s:\n%s", path, out)
		}
	}
}

func TestToolRipgrepGitignoreSlashInClass(t *testing.T) {
	assertIgnored := func(content, rel string, want bool) {
		t.Helper()
		patterns, ok := gitignoreExcludePatterns(content)
		if !ok {
			t.Fatalf("gitignoreExcludePatterns rejected %q", content)
		}
		matched, err := matchGlob(joinSearchIgnorePatterns(patterns), rel, false)
		if err != nil || matched != want {
			t.Fatalf("%q on %q: matched=%v err=%v, want %v", content, rel, matched, err, want)
		}
	}
	assertGlob := func(pattern, rel string, want bool) {
		t.Helper()
		matched, err := matchGlob(pattern, rel, false)
		if err != nil || matched != want {
			t.Fatalf("glob %q on %q: matched=%v err=%v, want %v", pattern, rel, matched, err, want)
		}
	}
	for _, rel := range []string{"a", "b"} {
		assertIgnored("[a/b]\n", rel, true)
		assertGlob("[a/b]", rel, true)
	}
	for _, rel := range []string{"c", "[a/b]", "sub/a"} {
		assertIgnored("[a/b]\n", rel, false)
		assertGlob("[a/b]", rel, false)
	}
	assertGlob("[a/b]", "a/b", false)
	for _, rel := range []string{"a/b", "a/c", "b/a"} {
		assertIgnored("[a/b]\n", rel, true)
	}
	assertIgnored("[a/b]\n", "c/a", false)
	assertGlob("[a/b].txt", "a.txt", true)
	assertGlob("[a/b].txt", "b.txt", true)
	assertGlob("[a/b].txt", "c.txt", false)
	assertGlob("[a/b].txt", "sub/a.txt", false)
	assertGlob("[a/]", "a", true)
	assertGlob("[a/]", "b", false)
	assertGlob("[a/]", "sub/a", false)
	assertGlob("[/]", "a", false)
	assertGlob("[/]", "[/]", false)
	for _, rel := range []string{"a", "z", "5"} {
		assertGlob("[^/]", rel, true)
		assertGlob("[!/]", rel, true)
	}
	for _, rel := range []string{"ab", "a.txt", "sub/a"} {
		assertGlob("[^/]", rel, false)
		assertGlob("[!/]", rel, false)
	}
	assertGlob("[!a/b]", "c", true)
	assertGlob("[!a/b]", "a", false)
	assertGlob("[!a/b]", "b", false)
	assertGlob("[!a/b]", "sub/c", false)
	assertGlob("src/[a/b].go", "src/a.go", true)
	assertGlob("src/[a/b].go", "src/b.go", true)
	assertGlob("src/[a/b].go", "src/c.go", false)
	assertGlob("src/[a/b].go", "src/sub/a.go", false)
	assertGlob("[[:digit:]/]", "5", true)
	assertGlob("[[:digit:]/]", "a", false)
	assertGlob("[[:digit:]/]", "sub/5", false)
	assertGlob("src/*[.-0]*", "src/a.b", true)
	assertGlob("src/*[.-0]*", "src/a/b", false)

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("[a/b]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "keep.go")
	other := filepath.Join(dir, "c")
	nested := filepath.Join(dir, "sub", "a")
	ignored := []string{
		filepath.Join(dir, "a"),
		filepath.Join(dir, "b"),
	}
	for _, path := range append([]string{keep, other, nested}, ignored...) {
		if err := os.WriteFile(path, []byte("SlashClassToken\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "SlashClassToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	found := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		found[strings.TrimSpace(line)] = true
	}
	if !found[keep] || !found[other] || !found[nested] || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("slash in class hid a file git keeps:\n%s", out)
	}
	for _, path := range ignored {
		if found[path] {
			t.Fatalf("slash in class kept %s:\n%s", path, out)
		}
	}
}

func TestToolRipgrepGitignoreBackwardsRange(t *testing.T) {
	assertIgnored := func(content, rel string, want bool) {
		t.Helper()
		patterns, ok := gitignoreExcludePatterns(content)
		if !ok {
			t.Fatalf("gitignoreExcludePatterns rejected %q", content)
		}
		matched, err := matchGlob(joinSearchIgnorePatterns(patterns), rel, false)
		if err != nil || matched != want {
			t.Fatalf("%q on %q: matched=%v err=%v, want %v", content, rel, matched, err, want)
		}
	}
	assertGlob := func(pattern, rel string, want bool) {
		t.Helper()
		matched, err := matchGlob(pattern, rel, false)
		if err != nil || matched != want {
			t.Fatalf("glob %q on %q: matched=%v err=%v, want %v", pattern, rel, matched, err, want)
		}
	}
	assertIgnored("[z-a]\n", "z", true)
	assertIgnored("[z-a]\n", "sub/z", true)
	assertIgnored("[z-a]\n", "a", false)
	assertIgnored("[z-a]\n", "m", false)
	assertIgnored("[z-a]\n", "[z-a]", false)
	assertGlob("[z-a]", "z", true)
	assertGlob("[z-a]", "a", false)
	assertGlob("[b-a]", "b", true)
	assertGlob("[b-a]", "a", false)
	assertGlob("[a-c]", "b", true)
	assertGlob("[a-c]", "d", false)
	for _, rel := range []string{"z", "c", "-"} {
		assertGlob("[z-a-c]", rel, true)
	}
	for _, rel := range []string{"a", "m"} {
		assertGlob("[z-a-c]", rel, false)
	}
	assertGlob("[^z-a]", "a", true)
	assertGlob("[^z-a]", "m", true)
	assertGlob("[^z-a]", "z", false)
	assertGlob("[0-.]", "0", true)
	assertGlob("[0-.]", ".", false)
	assertGlob("src/[z-a].go", "src/z.go", true)
	assertGlob("src/[z-a].go", "src/a.go", false)
	assertGlob("src/[z-a].go", "src/sub/z.go", false)
	if runtime.GOOS == "windows" {
		assertGlob("[z-a]", "Z", true)
	} else {
		assertGlob("[z-a]", "Z", false)
	}

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("[z-a]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "keep.go")
	other := filepath.Join(dir, "a")
	ignored := []string{
		filepath.Join(dir, "z"),
		filepath.Join(dir, "sub", "z"),
	}
	for _, path := range append([]string{keep, other}, ignored...) {
		if err := os.WriteFile(path, []byte("BackwardsRangeToken\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "BackwardsRangeToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	found := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		found[strings.TrimSpace(line)] = true
	}
	if !found[keep] || !found[other] || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("backwards range hid a file git keeps:\n%s", out)
	}
	for _, path := range ignored {
		if found[path] {
			t.Fatalf("backwards range kept %s:\n%s", path, out)
		}
	}
}

func TestToolRipgrepGitignoreDashRangeEndpoint(t *testing.T) {
	assertIgnored := func(content, rel string, want bool) {
		t.Helper()
		patterns, ok := gitignoreExcludePatterns(content)
		if !ok {
			t.Fatalf("gitignoreExcludePatterns rejected %q", content)
		}
		matched, err := matchGlob(joinSearchIgnorePatterns(patterns), rel, false)
		if err != nil || matched != want {
			t.Fatalf("%q on %q: matched=%v err=%v, want %v", content, rel, matched, err, want)
		}
	}
	assertGlob := func(pattern, rel string, want bool) {
		t.Helper()
		matched, err := matchGlob(pattern, rel, false)
		if err != nil || matched != want {
			t.Fatalf("glob %q on %q: matched=%v err=%v, want %v", pattern, rel, matched, err, want)
		}
	}
	for _, rel := range []string{"a", "b", "sub/a", "sub/b"} {
		assertIgnored("[a--b]\n", rel, true)
	}
	for _, rel := range []string{"c", "-", "0", "m"} {
		assertIgnored("[a--b]\n", rel, false)
	}
	assertIgnored("[a--]\n", "a", true)
	assertIgnored("[a--]\n", "sub/a", true)
	assertIgnored("[a--]\n", "-", false)
	assertIgnored("[a--]\n", "b", false)
	for _, rel := range []string{"a", "b", "-", "0", "A", "C", "sub/a", "sub/-"} {
		assertIgnored("[--b]\n", rel, true)
	}
	// '-' through 'b' includes 'C' and 'M'. Windows folds case, so 'c' and
	// 'm' match. '{' is outside the span either way.
	if runtime.GOOS == "windows" {
		assertIgnored("[--b]\n", "c", true)
		assertIgnored("[--b]\n", "m", true)
	} else {
		assertIgnored("[--b]\n", "c", false)
		assertIgnored("[--b]\n", "m", false)
	}
	assertIgnored("[--b]\n", "{", false)
	assertIgnored("[--]\n", "-", true)
	assertIgnored("[--]\n", "sub/-", true)
	assertIgnored("[--]\n", "a", false)
	assertIgnored("[a-]\n", "a", true)
	assertIgnored("[a-]\n", "-", true)
	assertIgnored("[a-]\n", "sub/-", true)
	assertIgnored("[a-]\n", "b", false)
	assertGlob("[a-c]", "b", true)
	assertGlob("[a-c]", "d", false)

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("[a--b]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "keep.go")
	other := filepath.Join(dir, "c")
	ignored := []string{
		filepath.Join(dir, "a"),
		filepath.Join(dir, "b"),
		filepath.Join(dir, "sub", "a"),
	}
	for _, path := range append([]string{keep, other}, ignored...) {
		if err := os.WriteFile(path, []byte("DashRangeToken\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "DashRangeToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	found := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		found[strings.TrimSpace(line)] = true
	}
	if !found[keep] || !found[other] || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("dash range endpoint hid a file git keeps:\n%s", out)
	}
	for _, path := range ignored {
		if found[path] {
			t.Fatalf("dash range endpoint kept %s:\n%s", path, out)
		}
	}
}

func TestToolRipgrepGitignoreSlashPrevRange(t *testing.T) {
	assertIgnored := func(content, rel string, want bool) {
		t.Helper()
		patterns, ok := gitignoreExcludePatterns(content)
		if !ok {
			t.Fatalf("gitignoreExcludePatterns rejected %q", content)
		}
		matched, err := matchGlob(joinSearchIgnorePatterns(patterns), rel, false)
		if err != nil || matched != want {
			t.Fatalf("%q on %q: matched=%v err=%v, want %v", content, rel, matched, err, want)
		}
	}
	assertGlob := func(pattern, rel string, want bool) {
		t.Helper()
		matched, err := matchGlob(pattern, rel, false)
		if err != nil || matched != want {
			t.Fatalf("glob %q on %q: matched=%v err=%v, want %v", pattern, rel, matched, err, want)
		}
	}
	// [a/-b] keeps a, then ranges from '/' to b. The dash is the operator.
	for _, rel := range []string{"a", "b", "0", "9", "A", "C"} {
		assertIgnored("[a/-b]\n", rel, true)
		assertIgnored("[/-b]\n", rel, true)
		assertGlob("[a/-b]", rel, true)
	}
	for _, rel := range []string{"-", "{", "sub/0", "sub/a"} {
		assertIgnored("[a/-b]\n", rel, false)
		assertGlob("[a/-b]", rel, false)
	}
	assertGlob("[a/-b]", "a/x", false)
	assertIgnored("[a/-b]\n", "a/x", true)
	assertIgnored("[a/-b]\n", "0/x", true)
	assertIgnored("[a/-b]\n", "-/x", false)
	assertIgnored("[a//-b]\n", "0", true)
	assertIgnored("[a//-b]\n", "-", false)
	assertIgnored("[a/-]\n", "a", true)
	assertIgnored("[a/-]\n", "-", true)
	assertIgnored("[a/-]\n", "0", false)
	assertIgnored("[/-]\n", "-", true)
	assertIgnored("[/-]\n", "a", false)
	assertIgnored("[/-/]\n", "-", false)
	assertIgnored("[/-/]\n", "a", false)
	assertGlob("[/-/]", "[/-/]", false)
	assertIgnored("[a/-.]\n", "a", true)
	assertIgnored("[a/-.]\n", "-", false)
	assertIgnored("[a/-.]\n", ".", false)
	assertIgnored("[/-.]\n", "-", false)
	assertIgnored("[/-.]\n", ".", false)
	assertGlob("[/-.]", "[/-.]", false)
	assertIgnored("[a/b]\n", "a", true)
	assertIgnored("[a/b]\n", "0", false)
	assertGlob("[a[:digit:]-9]", "-", true)
	assertGlob("[a[:digit:]-9]", "a", true)
	assertGlob("[a[:digit:]-9]", "5", true)
	assertGlob("[a[:digit:]-9]", "b", false)
	assertGlob("[a-c]", "b", true)
	assertGlob("[a--b]", "a", true)
	assertGlob("[a--b]", "0", false)
	assertGlob("[z-a]", "z", true)
	assertGlob("[z-a]", "a", false)
	if runtime.GOOS == "windows" {
		for _, rel := range []string{"c", "m", "z"} {
			assertIgnored("[a/-b]\n", rel, true)
			assertIgnored("[/-b]\n", rel, true)
		}
		assertIgnored("[a/-b]\n", "c/x", true)
		assertIgnored("[a/b]\n", "A", true)
		assertIgnored("[a/-]\n", "A", true)
	} else {
		for _, rel := range []string{"c", "m", "z"} {
			assertIgnored("[a/-b]\n", rel, false)
			assertIgnored("[/-b]\n", rel, false)
		}
		assertIgnored("[a/-b]\n", "c/x", false)
		assertIgnored("[a/b]\n", "A", false)
		assertIgnored("[a/-]\n", "A", false)
	}

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("[a/-b]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "keep.go")
	nested := filepath.Join(dir, "sub", "0")
	ignored := []string{
		filepath.Join(dir, "0"),
		filepath.Join(dir, "a"),
		filepath.Join(dir, "b"),
	}
	for _, path := range append([]string{keep, nested}, ignored...) {
		if err := os.WriteFile(path, []byte("SlashPrevToken\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "SlashPrevToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	found := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		found[strings.TrimSpace(line)] = true
	}
	if !found[keep] || !found[nested] || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("slash prev range hid a file git keeps:\n%s", out)
	}
	for _, path := range ignored {
		if found[path] {
			t.Fatalf("slash prev range kept %s:\n%s", path, out)
		}
	}
}

func TestToolRipgrepGitignoreEscapedDashRange(t *testing.T) {
	assertIgnored := func(content, rel string, want bool) {
		t.Helper()
		patterns, ok := gitignoreExcludePatterns(content)
		if !ok {
			t.Fatalf("gitignoreExcludePatterns rejected %q", content)
		}
		matched, err := matchGlob(joinSearchIgnorePatterns(patterns), rel, false)
		if err != nil || matched != want {
			t.Fatalf("%q on %q: matched=%v err=%v, want %v", content, rel, matched, err, want)
		}
	}
	// An escaped dash is a member. [a\--c] then ranges from that dash through c.
	for _, rel := range []string{"a", "b", "c", "0", "9", "A", "-", "[", "]", "sub/a", "sub/0", "sub/-"} {
		assertIgnored("[a\\--c]\n", rel, true)
		assertIgnored("[\\--c]\n", rel, true)
	}
	assertIgnored("[a\\--c]\n", "{", false)
	assertIgnored("[^a\\--c]\n", "{", true)
	assertIgnored("[a\\--c]\n", "0/x", true)
	// '{' is outside the span. 'x' is outside it only when case is distinct,
	// so '{/x' is ignored on Windows and '{/{' is kept on both.
	assertIgnored("[a\\--c]\n", "{/{", false)
	if runtime.GOOS == "windows" {
		assertIgnored("[a\\--c]\n", "{/x", true)
	} else {
		assertIgnored("[a\\--c]\n", "{/x", false)
	}
	assertIgnored("[a\\-c]\n", "a", true)
	assertIgnored("[a\\-c]\n", "-", true)
	assertIgnored("[a\\-c]\n", "c", true)
	assertIgnored("[a\\-c]\n", "b", false)
	assertIgnored("[a\\-b-c]\n", "a", true)
	assertIgnored("[a\\-b-c]\n", "b", true)
	assertIgnored("[a\\-b-c]\n", "-", true)
	assertIgnored("[a\\-b-c]\n", "0", false)
	assertIgnored("[a\\--]\n", "a", true)
	assertIgnored("[a\\--]\n", "-", true)
	assertIgnored("[a\\--]\n", "b", false)
	assertIgnored("[a-\\-]\n", "a", true)
	assertIgnored("[a-\\-]\n", "-", false)
	assertIgnored("[a\\--\\]]\n", "a", true)
	assertIgnored("[a\\--\\]]\n", "0", true)
	assertIgnored("[a\\--\\]]\n", "]", true)
	assertIgnored("[a\\--\\]]\n", "[", true)
	assertIgnored("[a\\--\\]]\n", "-", true)
	assertIgnored("[a\\--c]x\n", "ax", true)
	assertIgnored("[a\\--c]x\n", "-x", true)
	assertIgnored("[a\\--c]x\n", "cx", true)
	if runtime.GOOS == "windows" {
		for _, rel := range []string{"d", "m"} {
			assertIgnored("[a\\--c]\n", rel, true)
			assertIgnored("[^a\\--c]\n", rel, false)
			assertIgnored("[a\\--\\]]\n", rel, true)
		}
		assertIgnored("[a\\--\\]]\n", "b", true)
		assertIgnored("[a\\--c]x\n", "dx", true)
	} else {
		for _, rel := range []string{"d", "m"} {
			assertIgnored("[a\\--c]\n", rel, false)
			assertIgnored("[^a\\--c]\n", rel, true)
			assertIgnored("[a\\--\\]]\n", rel, false)
		}
		assertIgnored("[a\\--\\]]\n", "b", false)
		assertIgnored("[a\\--c]x\n", "dx", false)
	}

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("[a\\--c]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "keep.go")
	other := filepath.Join(dir, "notes.txt")
	ignored := []string{
		filepath.Join(dir, "0"),
		filepath.Join(dir, "a"),
		filepath.Join(dir, "-"),
		filepath.Join(dir, "c"),
		filepath.Join(dir, "sub", "a"),
	}
	for _, path := range append([]string{keep, other}, ignored...) {
		if err := os.WriteFile(path, []byte("EscapedDashToken\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "EscapedDashToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	found := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		found[strings.TrimSpace(line)] = true
	}
	if !found[keep] || !found[other] || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("escaped dash range hid a file git keeps:\n%s", out)
	}
	for _, path := range ignored {
		if found[path] {
			t.Fatalf("escaped dash range kept %s:\n%s", path, out)
		}
	}
}

func TestToolRipgrepGitignoreClassRangeEndpoint(t *testing.T) {
	assertIgnored := func(content, rel string, want bool) {
		t.Helper()
		patterns, ok := gitignoreExcludePatterns(content)
		if !ok {
			t.Fatalf("gitignoreExcludePatterns rejected %q", content)
		}
		matched, err := matchGlob(joinSearchIgnorePatterns(patterns), rel, false)
		if err != nil || matched != want {
			t.Fatalf("%q on %q: matched=%v err=%v, want %v", content, rel, matched, err, want)
		}
	}
	// Git 2.55, ignorecase false. A range ending at ']' or '-' is a real span.
	// Textual emit would close the class early or reread the dash.
	for _, rel := range []string{"A", "B", "Z", "[", "]", "sub/A", "sub/B", "B/x"} {
		assertIgnored("[A-\\]]\n", rel, true)
		assertIgnored("[0-\\]]\n", rel, true)
	}
	assertIgnored("[A-\\]]\n", "0", false)
	assertIgnored("[A-\\]]\n", "-", false)
	assertIgnored("[A-\\]]\n", "{", false)
	assertIgnored("[A-\\]]\n", "{/0", false)
	assertIgnored("[A-\\]]\n", "{/{", false)
	assertIgnored("[0-\\]]\n", "0", true)
	assertIgnored("[0-\\]]\n", "sub/0", true)
	assertIgnored("[0-\\]]\n", "-", false)
	assertIgnored("[0-\\]]\n", "{", false)
	assertIgnored("[0-\\]]\n", "{/{", false)
	for _, rel := range []string{"a", "b", "]", "sub/a"} {
		assertIgnored("[a\\]-z]\n", rel, true)
	}
	assertIgnored("[a\\]-z]\n", "0", false)
	assertIgnored("[a\\]-z]\n", "[", false)
	assertIgnored("[a\\]-z]\n", "{", false)
	assertIgnored("[a\\]-z]\n", "[/0", false)
	for _, rel := range []string{"a", "-", " ", "!", "sub/a"} {
		assertIgnored("[a -\\-]\n", rel, true)
	}
	assertIgnored("[a -\\-]\n", "0", false)
	assertIgnored("[a -\\-]\n", "b", false)
	assertIgnored("[a -\\-]\n", "0/x", false)
	for _, rel := range []string{"0", "-", "{", " ", "!", "0/x"} {
		assertIgnored("[^A-\\]]\n", rel, true)
	}
	for _, rel := range []string{"A", "B", "Z", "[", "]", "sub/A"} {
		assertIgnored("[^A-\\]]\n", rel, false)
	}
	// A literal ']' that is not a range endpoint still uses the text class.
	for _, rel := range []string{"]", "a", "b"} {
		assertIgnored("[a\\]b]\n", rel, true)
	}
	assertIgnored("[a\\]b]\n", "c", false)
	for _, rel := range []string{"a", "b", "c", "]"} {
		assertIgnored("[a-c\\]]\n", rel, true)
	}
	assertIgnored("[a-c\\]]\n", "d", false)
	assertIgnored("[a\\-b-c]\n", "a", true)
	assertIgnored("[a\\-b-c]\n", "-", true)
	assertIgnored("[a\\-b-c]\n", "b", true)
	assertIgnored("[a\\-b-c]\n", "c", true)
	assertIgnored("[a\\-b-c]\n", "0", false)
	if runtime.GOOS == "windows" {
		for _, rel := range []string{"a", "b", "sub/a", "sub/b"} {
			assertIgnored("[A-\\]]\n", rel, true)
			assertIgnored("[0-\\]]\n", rel, true)
			assertIgnored("[^A-\\]]\n", rel, false)
		}
		for _, rel := range []string{"A", "B", "Z", "sub/A", "sub/B"} {
			assertIgnored("[a\\]-z]\n", rel, true)
		}
		assertIgnored("[a -\\-]\n", "A", true)
		assertIgnored("[a -\\-]\n", "sub/A", true)
		assertIgnored("[^A-\\]]\n", "A/x", false)
	} else {
		for _, rel := range []string{"a", "b", "sub/a", "sub/b"} {
			assertIgnored("[A-\\]]\n", rel, false)
			assertIgnored("[0-\\]]\n", rel, false)
		}
		for _, rel := range []string{"A", "B", "Z", "sub/A"} {
			assertIgnored("[a\\]-z]\n", rel, false)
		}
		for _, rel := range []string{"a", "b", "sub/a"} {
			assertIgnored("[^A-\\]]\n", rel, true)
		}
		assertIgnored("[a -\\-]\n", "A", false)
		assertIgnored("[a -\\-]\n", "sub/A", false)
		assertIgnored("[^A-\\]]\n", "A/x", true)
	}

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("[A-\\]]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "keep.go")
	other := filepath.Join(dir, "0")
	brace := filepath.Join(dir, "{")
	ignored := []string{
		filepath.Join(dir, "A"),
		filepath.Join(dir, "B"),
		filepath.Join(dir, "]"),
		filepath.Join(dir, "sub", "A"),
	}
	for _, path := range append([]string{keep, other, brace}, ignored...) {
		if err := os.WriteFile(path, []byte("ClassRangeEndToken\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "ClassRangeEndToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	found := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		found[strings.TrimSpace(line)] = true
	}
	if !found[keep] || !found[other] || !found[brace] || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("class range endpoint hid a file git keeps:\n%s", out)
	}
	for _, path := range ignored {
		if found[path] {
			t.Fatalf("class range endpoint kept %s:\n%s", path, out)
		}
	}
}

func TestToolRipgrepGitignorePosixClassEscape(t *testing.T) {
	assertIgnored := func(content, rel string, want bool) {
		t.Helper()
		patterns, ok := gitignoreExcludePatterns(content)
		if !ok {
			t.Fatalf("gitignoreExcludePatterns rejected %q", content)
		}
		matched, err := matchGlob(joinSearchIgnorePatterns(patterns), rel, false)
		if err != nil || matched != want {
			t.Fatalf("%q on %q: matched=%v err=%v, want %v", content, rel, matched, err, want)
		}
	}
	// Git 2.55. The ']' of [:digit:] is not the class closer, so \ ] is a
	// member of the same class. An unknown name matches nothing.
	for _, rel := range []string{"]", "0", "5", "9", "sub/]", "sub/5", "5/x"} {
		assertIgnored("[[:digit:]\\]]\n", rel, true)
		assertIgnored("[][:digit:]\\]]\n", rel, true)
	}
	for _, rel := range []string{"a", "Z", "[", "{", "-", "!", "5x", "{/{"} {
		assertIgnored("[[:digit:]\\]]\n", rel, false)
	}
	for _, rel := range []string{"-", "0", "5", "9", "sub/-", "sub/5"} {
		assertIgnored("[[:digit:]\\-]\n", rel, true)
		assertIgnored("[[:digit:]\\-9]\n", rel, true)
	}
	assertIgnored("[[:digit:]\\-]\n", "]", false)
	assertIgnored("[[:digit:]\\-9]\n", "]", false)
	assertIgnored("[[:digit:]\\-9]\n", "a", false)
	for _, rel := range []string{"]", "0", "5", "9", "a", "sub/a", "sub/5"} {
		assertIgnored("[a[:digit:]\\]]\n", rel, true)
		assertIgnored("[a\\][:digit:]]\n", rel, true)
	}
	assertIgnored("[a[:digit:]\\]]\n", "Z", false)
	assertIgnored("[a[:digit:]\\]]\n", "[", false)
	for _, rel := range []string{"5", "]", "a", "[[:foo:]]", "5/x"} {
		assertIgnored("[[:foo:]\\]]\n", rel, false)
	}
	for _, rel := range []string{"-", "!", "[", "{", "a", "Z", "sub/a", "sub/Z"} {
		assertIgnored("[^[:digit:]\\]]\n", rel, true)
		assertIgnored("[![:digit:]\\]]\n", rel, true)
	}
	for _, rel := range []string{"]", "0", "5", "9", "sub/5"} {
		assertIgnored("[^[:digit:]\\]]\n", rel, false)
		assertIgnored("[![:digit:]\\]]\n", rel, false)
	}
	for _, rel := range []string{"]", "Z", "sub/Z"} {
		assertIgnored("[[:upper:]\\]]\n", rel, true)
	}
	for _, rel := range []string{"A", "Z", "[", "]", "0", "5", "9", "sub/Z", "sub/5", "5/x", "{/5"} {
		assertIgnored("[A-\\][:digit:]]\n", rel, true)
	}
	for _, rel := range []string{"{", "!", "-", "{/{"} {
		assertIgnored("[A-\\][:digit:]]\n", rel, false)
	}
	assertIgnored("[[:digit:][:alpha:]\\]]\n", "5", true)
	assertIgnored("[[:digit:][:alpha:]\\]]\n", "a", true)
	assertIgnored("[[:digit:][:alpha:]\\]]\n", "Z", true)
	assertIgnored("[[:digit:][:alpha:]\\]]\n", "]", true)
	assertIgnored("[[:digit:][:alpha:]\\]]\n", "!", false)
	assertIgnored("[[:digit:]\\]]x\n", "5x", true)
	assertIgnored("[[:digit:]\\]]x\n", "]x", true)
	assertIgnored("[[:digit:]\\]]x\n", "ax", false)
	assertIgnored("[[:digit:]\\]]x\n", "5", false)
	if runtime.GOOS == "windows" {
		assertIgnored("[a[:digit:]\\]]\n", "A", true)
		assertIgnored("[[:upper:]\\]]\n", "a", true)
		assertIgnored("[A-\\][:digit:]]\n", "a", true)
		assertIgnored("[A-\\][:digit:]]\n", "{/a", true)
	} else {
		assertIgnored("[a[:digit:]\\]]\n", "A", false)
		assertIgnored("[[:upper:]\\]]\n", "a", false)
		assertIgnored("[A-\\][:digit:]]\n", "a", false)
		assertIgnored("[A-\\][:digit:]]\n", "{/a", false)
	}

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("[A-\\][:digit:]]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "keep.go")
	brace := filepath.Join(dir, "{")
	ignored := []string{
		filepath.Join(dir, "0"),
		filepath.Join(dir, "5"),
		filepath.Join(dir, "Z"),
		filepath.Join(dir, "]"),
		filepath.Join(dir, "sub", "Z"),
	}
	for _, path := range append([]string{keep, brace}, ignored...) {
		if err := os.WriteFile(path, []byte("PosixEscToken\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "PosixEscToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	found := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		found[strings.TrimSpace(line)] = true
	}
	if !found[keep] || !found[brace] || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("posix class escape hid a file git keeps:\n%s", out)
	}
	for _, path := range ignored {
		if found[path] {
			t.Fatalf("posix class escape kept %s:\n%s", path, out)
		}
	}
}

func TestToolRipgrepGitignoreEscapedBang(t *testing.T) {
	assertIgnored := func(content, rel string, want bool) {
		t.Helper()
		patterns, ok := gitignoreExcludePatterns(content)
		if !ok {
			t.Fatalf("gitignoreExcludePatterns rejected %q", content)
		}
		matched, err := matchGlob(joinSearchIgnorePatterns(patterns), rel, false)
		if err != nil || matched != want {
			t.Fatalf("%q on %q: matched=%v err=%v, want %v", content, rel, matched, err, want)
		}
	}
	// Git 2.55. An escaped '!' or '^' is a member. Putting it first in the
	// class text makes the second parse treat it as negation.
	for _, rel := range []string{"!", "a", "sub/!", "sub/a", "!/x"} {
		assertIgnored("[\\!a]\n", rel, true)
		assertIgnored("[a\\!]\n", rel, true)
	}
	for _, rel := range []string{"b", "z", "0", "^", "{/{"} {
		assertIgnored("[\\!a]\n", rel, false)
		assertIgnored("[a\\!]\n", rel, false)
	}
	for _, rel := range []string{"^", "a", "sub/^", "sub/a"} {
		assertIgnored("[\\^a]\n", rel, true)
		assertIgnored("[a\\^]\n", rel, true)
	}
	assertIgnored("[\\^a]\n", "!", false)
	assertIgnored("[\\^a]\n", "b", false)
	assertIgnored("[\\!]\n", "!", true)
	assertIgnored("[\\!]\n", "a", false)
	assertIgnored("[\\^]\n", "^", true)
	assertIgnored("[\\^]\n", "!", false)
	for _, rel := range []string{"!", "a", "b", "c", "sub/b"} {
		assertIgnored("[\\!a-c]\n", rel, true)
	}
	for _, rel := range []string{"0", "z", "^", "{"} {
		assertIgnored("[\\!a-c]\n", rel, false)
	}
	for _, rel := range []string{"^", "a", "d", "z", "sub/z", "{/z"} {
		assertIgnored("[\\^-z]\n", rel, true)
	}
	for _, rel := range []string{"0", "!", "-", "{", "{/0"} {
		assertIgnored("[\\^-z]\n", rel, false)
	}
	for _, rel := range []string{"!", "-", "0", "a", "b", "c", "[", "]", "^", "sub/0"} {
		assertIgnored("[\\!-c]\n", rel, true)
	}
	assertIgnored("[\\!-c]\n", "{", false)
	assertIgnored("[\\!-c]\n", "{/{", false)
	for _, rel := range []string{"b", "0", "z", "sub/b"} {
		assertIgnored("[!a\\!]\n", rel, true)
		assertIgnored("[^a\\^]\n", rel, true)
	}
	assertIgnored("[!a\\!]\n", "^", true)
	assertIgnored("[^a\\^]\n", "!", true)
	assertIgnored("[!a\\!]\n", "a", false)
	assertIgnored("[!a\\!]\n", "!", false)
	assertIgnored("[^a\\^]\n", "a", false)
	assertIgnored("[^a\\^]\n", "^", false)
	if runtime.GOOS == "windows" {
		assertIgnored("[\\!a]\n", "A", true)
		assertIgnored("[\\!a-c]\n", "A", true)
		assertIgnored("[\\^-z]\n", "A", true)
		assertIgnored("[\\^-z]\n", "{/A", true)
		assertIgnored("[\\!-c]\n", "z", true)
		assertIgnored("[\\!-c]\n", "d", true)
		assertIgnored("[!a\\!]\n", "A", false)
		assertIgnored("[^a\\^]\n", "A", false)
	} else {
		assertIgnored("[\\!a]\n", "A", false)
		assertIgnored("[\\!a-c]\n", "A", false)
		assertIgnored("[\\^-z]\n", "A", false)
		assertIgnored("[\\^-z]\n", "{/A", false)
		assertIgnored("[\\!-c]\n", "z", false)
		assertIgnored("[\\!-c]\n", "d", false)
		assertIgnored("[!a\\!]\n", "A", true)
		assertIgnored("[^a\\^]\n", "A", true)
	}

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("[\\!a]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "keep.go")
	other := filepath.Join(dir, "b")
	brace := filepath.Join(dir, "{")
	ignored := []string{
		filepath.Join(dir, "!"),
		filepath.Join(dir, "a"),
		filepath.Join(dir, "sub", "a"),
		filepath.Join(dir, "sub", "!"),
	}
	for _, path := range append([]string{keep, other, brace}, ignored...) {
		if err := os.WriteFile(path, []byte("BangEscToken\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "BangEscToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	found := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		found[strings.TrimSpace(line)] = true
	}
	if !found[keep] || !found[other] || !found[brace] || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("escaped bang hid a file git keeps:\n%s", out)
	}
	for _, path := range ignored {
		if found[path] {
			t.Fatalf("escaped bang kept %s:\n%s", path, out)
		}
	}
}

func TestToolRipgrepGitignoreClassCaseFold(t *testing.T) {
	assertIgnored := func(content, rel string, want bool) {
		t.Helper()
		patterns, ok := gitignoreExcludePatterns(content)
		if !ok {
			t.Fatalf("gitignoreExcludePatterns rejected %q", content)
		}
		matched, err := matchGlob(joinSearchIgnorePatterns(patterns), rel, false)
		if err != nil || matched != want {
			t.Fatalf("%q on %q: matched=%v err=%v, want %v", content, rel, matched, err, want)
		}
	}
	// Holds with ignorecase on and off. [0-A] is 0x30 through 0x41.
	// [A-\\] is 0x41 through '\\'. [B-b] is 0x42 through 'b'. Folded
	// letters such as 'a' under [0-A] are checked per OS below.
	for _, rel := range []string{"0", "9", ";", "@", "A", "sub/0", "sub/A"} {
		assertIgnored("[0-A]\n", rel, true)
	}
	for _, rel := range []string{"B", "[", "]", "\\", "sub/["} {
		assertIgnored("[0-A]\n", rel, false)
	}
	for _, rel := range []string{"A", "B", "Z", "[", "\\", "sub/B", "sub/["} {
		assertIgnored("[A-\\\\]\n", rel, true)
	}
	for _, rel := range []string{"]", "^", "{", "sub/]"} {
		assertIgnored("[A-\\\\]\n", rel, false)
	}
	for _, rel := range []string{"B", "C", "Z", "a", "[", "]", "^", "_", "`", "sub/a", "sub/["} {
		assertIgnored("[B-b]\n", rel, true)
	}
	for _, rel := range []string{"{", "0", "sub/{"} {
		assertIgnored("[B-b]\n", rel, false)
	}
	assertIgnored("[A-C]\n", "B", true)
	assertIgnored("[A-C]\n", "D", false)
	assertIgnored("[a-c]\n", "b", true)
	assertIgnored("[a-c]\n", "d", false)
	if runtime.GOOS == "windows" {
		// core.ignorecase true. The text is folded and the class bounds
		// stay put, so [0-A] gains 'a' and [A-\\] gains the lower letters.
		// Lowercasing the pattern first would also hide '[' under [0-A].
		for _, rel := range []string{"a", "sub/a"} {
			assertIgnored("[0-A]\n", rel, true)
		}
		for _, rel := range []string{"a", "b", "z", "sub/a", "sub/z"} {
			assertIgnored("[A-\\\\]\n", rel, true)
		}
		for _, rel := range []string{"A", "c", "C", "z", "sub/A", "sub/c"} {
			assertIgnored("[B-b]\n", rel, true)
		}
		assertIgnored("[A-C]\n", "a", true)
		assertIgnored("[a-c]\n", "B", true)
	} else {
		assertIgnored("[0-A]\n", "a", false)
		assertIgnored("[0-A]\n", "sub/a", false)
		assertIgnored("[A-\\\\]\n", "a", false)
		assertIgnored("[A-\\\\]\n", "b", false)
		assertIgnored("[A-\\\\]\n", "z", false)
		assertIgnored("[A-\\\\]\n", "sub/a", false)
		assertIgnored("[A-\\\\]\n", "sub/z", false)
		assertIgnored("[B-b]\n", "A", false)
		assertIgnored("[B-b]\n", "c", false)
		assertIgnored("[B-b]\n", "z", false)
		assertIgnored("[B-b]\n", "sub/A", false)
		assertIgnored("[B-b]\n", "sub/c", false)
		assertIgnored("[A-C]\n", "a", false)
		assertIgnored("[a-c]\n", "B", false)
	}

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("[0-A]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "keep.go")
	bracket := filepath.Join(dir, "[")
	nestedBracket := filepath.Join(dir, "sub", "[")
	zero := filepath.Join(dir, "0")
	nestedZero := filepath.Join(dir, "sub", "0")
	lower := filepath.Join(dir, "a")
	for _, path := range []string{keep, bracket, nestedBracket, zero, nestedZero, lower} {
		if err := os.WriteFile(path, []byte("ClassFoldToken\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "ClassFoldToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	found := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		found[strings.TrimSpace(line)] = true
	}
	if !found[keep] || !found[bracket] || !found[nestedBracket] || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("class case fold hid a file outside [0-A]:\n%s", out)
	}
	for _, path := range []string{zero, nestedZero} {
		if found[path] {
			t.Fatalf("class case fold kept %s:\n%s", path, out)
		}
	}
	if runtime.GOOS == "windows" {
		if found[lower] {
			t.Fatalf("windows [0-A] kept %s:\n%s", lower, out)
		}
	} else if !found[lower] {
		t.Fatalf("case-sensitive [0-A] hid %s:\n%s", lower, out)
	}
}

func TestToolRipgrepBraceCommaInsideClass(t *testing.T) {
	dir := t.TempDir()
	keepA := filepath.Join(dir, "a.txt")
	keepB := filepath.Join(dir, "b.txt")
	keepC := filepath.Join(dir, "c.txt")
	skip := filepath.Join(dir, "ab.txt")
	other := filepath.Join(dir, "d.txt")
	for _, path := range []string{keepA, keepB, keepC, skip, other} {
		if err := os.WriteFile(path, []byte("package main\nfunc BraceClassToken() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"a.txt", "b.txt", "c.txt"} {
		matched, err := matchGlob("{[a,b],c}.txt", rel, false)
		if err != nil || !matched {
			t.Fatalf("{[a,b],c}.txt should match %s: matched=%v err=%v", rel, matched, err)
		}
	}
	for _, rel := range []string{"ab.txt", "d.txt"} {
		matched, err := matchGlob("{[a,b],c}.txt", rel, false)
		if err != nil || matched {
			t.Fatalf("{[a,b],c}.txt should not match %s: matched=%v err=%v", rel, matched, err)
		}
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "BraceClassToken",
		"glob":        "{[a,b],c}.txt",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, keepA) || !strings.Contains(out, keepB) || !strings.Contains(out, keepC) || strings.Contains(out, skip) || strings.Contains(out, other) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("comma inside a brace class was split:\n%s", out)
	}
}

func TestSplitSearchGlobCommaRespectsClass(t *testing.T) {
	for _, rel := range []string{"xa", "x,", "x]", "main.go"} {
		matched, err := matchGlob("*[],a],*.go", rel, false)
		if err != nil || !matched {
			t.Fatalf("*[],a],*.go should match %s: matched=%v err=%v", rel, matched, err)
		}
	}
	matched, err := matchGlob("*[],a],*.go", "xb", false)
	if err != nil || matched {
		t.Fatalf("comma inside [] was split out of the class: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("foo[,*.go", "foo[", false)
	if err != nil || !matched {
		t.Fatalf("unclosed bracket should stay a literal alternative: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("foo[,*.go", "main.go", false)
	if err != nil || !matched {
		t.Fatalf("unclosed bracket swallowed the next alternative: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("foo[,*.go", "foo[bar.go", false)
	if err != nil || !matched {
		t.Fatalf("*.go alternative should still match foo[bar.go: matched=%v err=%v", matched, err)
	}
	matched, err = matchGlob("foo[,*.go", "other.txt", false)
	if err != nil || matched {
		t.Fatalf("unclosed bracket kept a name that is neither alternative: matched=%v err=%v", matched, err)
	}
}

func TestToolRipgrepNestedGitignoreStaysInItsDirectory(t *testing.T) {
	dir := t.TempDir()
	token := "package main\nfunc NestedIgnoreToken() {}\n"
	keep := filepath.Join(dir, "keep.log")
	drop := filepath.Join(dir, "src", "drop.log")
	deep := filepath.Join(dir, "src", "sub", "drop.log")
	keepGo := filepath.Join(dir, "src", "keep.go")
	generated := filepath.Join(dir, "src", "generated.go")
	generatedKeep := filepath.Join(dir, "src", "sub", "generated.go")
	spaced := filepath.Join(dir, "src", "my notes.txt")
	spacedKeep := filepath.Join(dir, "other", "my notes.txt")
	otherDrop := filepath.Join(dir, "other", "drop.log")
	tmp := filepath.Join(dir, "src", "debug.tmp")
	skipped := filepath.Join(dir, "src", "skip", "hidden.go")
	for _, path := range []string{keep, drop, deep, keepGo, generated, generatedKeep, spaced, spacedKeep, otherDrop, tmp, skipped} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", ".gitignore"), []byte("*.log\n/generated.go\nmy notes.txt\nskip/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A negation disables only this directory's ignore file.
	if err := os.WriteFile(filepath.Join(dir, "other", ".gitignore"), []byte("*.log\n!keep.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "NestedIgnoreToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	found := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		found[strings.TrimSpace(line)] = true
	}
	for _, path := range []string{keep, keepGo, generatedKeep, spacedKeep, otherDrop} {
		if !found[path] {
			t.Fatalf("nested gitignore hid %s:\n%s", path, out)
		}
	}
	for _, path := range []string{drop, deep, generated, spaced, tmp, skipped} {
		if found[path] {
			t.Fatalf("nested gitignore kept %s:\n%s", path, out)
		}
	}
	if !strings.Contains(out, "mode=indexed") || strings.Contains(out, "search failed") {
		t.Fatalf("nested gitignore broke indexed search:\n%s", out)
	}
	globOut := ToolGlob(map[string]interface{}{
		"path":    dir,
		"pattern": "**/*.go",
	})
	if !strings.Contains(globOut, keepGo) || !strings.Contains(globOut, generatedKeep) || strings.Contains(globOut, generated) || strings.Contains(globOut, skipped) {
		t.Fatalf("glob ignored nested gitignore:\n%s", globOut)
	}
	ignoredOff := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "NestedIgnoreToken",
		"no_ignore":   true,
		"output_mode": "files_with_matches",
	})
	if !strings.Contains(ignoredOff, drop) || !strings.Contains(ignoredOff, tmp) {
		t.Fatalf("no_ignore should keep nested and root ignored files:\n%s", ignoredOff)
	}
}

func TestToolRipgrepParentGitignoreAppliesInSubdirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n/only_root.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "info", "exclude"), []byte("from-info.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "src")
	drop := filepath.Join(src, "drop.log")
	deep := filepath.Join(src, "sub", "drop.log")
	keep := filepath.Join(src, "keep.go")
	onlyRoot := filepath.Join(src, "only_root.go")
	fromInfo := filepath.Join(src, "from-info.go")
	for _, path := range []string{drop, deep, keep, onlyRoot, fromInfo} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package main\nfunc ParentIgnoreToken() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        src,
		"pattern":     "ParentIgnoreToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	found := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		found[strings.TrimSpace(line)] = true
	}
	if !found[keep] || !found[onlyRoot] || found[drop] || found[deep] || found[fromInfo] || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("parent gitignore was not applied inside the subdirectory:\n%s", out)
	}
}

func TestToolRipgrepDirectoryUsesAncestorCorpus(t *testing.T) {
	dir, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Clean(dir)
	src := filepath.Join(dir, "src")
	vendor := filepath.Join(dir, "vendor")
	mod := filepath.Join(dir, "mod")
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(mod, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\nvendor/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	token := "package main\nfunc AncestorCorpusToken() {}\n"
	sibling := filepath.Join(dir, "sibling.go")
	hit := filepath.Join(src, "hit.go")
	skipLog := filepath.Join(src, "skip.log")
	kept := filepath.Join(vendor, "kept.go")
	own := filepath.Join(mod, "own.go")
	for _, path := range []string{sibling, hit, skipLog, kept, own} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	keys := []string{
		searchIndexCorpusKey(dir, resolveSearchExcludePattern(nil, dir), false),
		searchIndexCorpusKey(src, resolveSearchExcludePattern(nil, src), false),
		searchIndexCorpusKey(vendor, resolveSearchExcludePattern(nil, vendor), false),
		searchIndexCorpusKey(mod, resolveSearchExcludePattern(nil, mod), false),
	}
	t.Cleanup(func() {
		for _, key := range keys {
			dropSearchCorpus(key)
		}
	})

	rootOut := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "AncestorCorpusToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(rootOut, sibling) || !strings.Contains(rootOut, hit) || !strings.Contains(rootOut, own) || strings.Contains(rootOut, skipLog) || strings.Contains(rootOut, kept) {
		t.Fatalf("root corpus:\n%s", rootOut)
	}

	subOut := ToolRipgrep(map[string]interface{}{
		"path":        src,
		"pattern":     "AncestorCorpusToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(subOut, hit) || strings.Contains(subOut, sibling) || strings.Contains(subOut, skipLog) || strings.Contains(subOut, own) || !strings.Contains(subOut, "mode=indexed") {
		t.Fatalf("subdirectory should use the ancestor corpus:\n%s", subOut)
	}
	srcKey := keys[1]
	searchBuildMu.Lock()
	srcSlot := searchBuildSlots[srcKey]
	searchBuildMu.Unlock()
	if srcSlot != nil {
		t.Fatal("subdirectory search scheduled its own corpus")
	}

	_, vendorOK, vendorStats := indexedSearchCandidates(vendor, "AncestorCorpusToken", "", resolveSearchExcludePattern(nil, vendor), "", false, false)
	if vendorOK || vendorStats.fallbackReason != "index_unavailable" {
		t.Fatalf("ignored directory used the ancestor corpus: ok=%v reason=%s", vendorOK, vendorStats.fallbackReason)
	}
	_, modOK, modStats := indexedSearchCandidates(mod, "AncestorCorpusToken", "", resolveSearchExcludePattern(nil, mod), "", false, false)
	if modOK || modStats.fallbackReason != "index_unavailable" {
		t.Fatalf("nested repository used the ancestor corpus: ok=%v reason=%s", modOK, modStats.fallbackReason)
	}

	ignored := ToolRipgrep(map[string]interface{}{
		"path":        src,
		"pattern":     "AncestorCorpusToken",
		"output_mode": "files_with_matches",
		"stats":       true,
		"no_ignore":   true,
	})
	if !strings.Contains(ignored, skipLog) || !strings.Contains(ignored, "mode=full_scan") {
		t.Fatalf("no_ignore should not reuse the gitignore corpus:\n%s", ignored)
	}
}

func TestToolRipgrepUserExcludeUsesGitignoreCorpus(t *testing.T) {
	dir, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Clean(dir)
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	token := "package main\nfunc SameRootUserExcludeToken() {}\n"
	keep := filepath.Join(dir, "keep.go")
	note := filepath.Join(dir, "note.md")
	skipLog := filepath.Join(dir, "skip.log")
	for _, path := range []string{keep, note, skipLog} {
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	userArgs := map[string]interface{}{
		"path":        dir,
		"pattern":     "SameRootUserExcludeToken",
		"exclude":     "*.md",
		"output_mode": "files_with_matches",
		"stats":       true,
	}
	noIgnoreArgs := map[string]interface{}{
		"path":        dir,
		"pattern":     "SameRootUserExcludeToken",
		"output_mode": "files_with_matches",
		"stats":       true,
		"no_ignore":   true,
	}
	var keys []string
	t.Cleanup(func() {
		for _, key := range keys {
			dropSearchCorpus(key)
		}
	})
	keys = append(keys,
		searchIndexCorpusKey(dir, resolveSearchExcludePattern(nil, dir), false),
		searchIndexCorpusKey(dir, resolveSearchExcludePattern(userArgs, dir), false),
		searchIndexCorpusKey(dir, resolveSearchExcludePattern(noIgnoreArgs, dir), false),
	)

	rootOut := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "SameRootUserExcludeToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(rootOut, keep) || !strings.Contains(rootOut, note) || strings.Contains(rootOut, skipLog) || !strings.Contains(rootOut, "mode=indexed") {
		t.Fatalf("gitignore corpus:\n%s", rootOut)
	}

	if !searchArgsIndexReady(userArgs) {
		t.Fatal("user exclude should be ready from the gitignore corpus")
	}
	userOut := ToolRipgrep(userArgs)
	if !strings.Contains(userOut, keep) || strings.Contains(userOut, note) || strings.Contains(userOut, skipLog) || !strings.Contains(userOut, "mode=indexed") {
		t.Fatalf("user exclude should reuse the gitignore corpus:\n%s", userOut)
	}
	searchBuildMu.Lock()
	userSlot := searchBuildSlots[keys[1]]
	searchBuildMu.Unlock()
	if userSlot != nil {
		t.Fatal("user exclude scheduled its own corpus")
	}

	ignored := ToolRipgrep(noIgnoreArgs)
	if !strings.Contains(ignored, skipLog) || !strings.Contains(ignored, "mode=full_scan") {
		t.Fatalf("no_ignore should not reuse the gitignore corpus:\n%s", ignored)
	}
}

func TestToolRipgrepUserDirectoryExcludeUsesGitignoreCorpus(t *testing.T) {
	dir, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Clean(dir)
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	token := "package main\nfunc UserDirExcludeToken() {}\n"
	keep := filepath.Join(dir, "keep.go")
	nested := filepath.Join(dir, "drafts", "sub", "kept.go")
	skipLog := filepath.Join(dir, "skip.log")
	for _, path := range []string{keep, nested, skipLog} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	userArgs := map[string]interface{}{
		"path":        dir,
		"pattern":     "UserDirExcludeToken",
		"exclude":     "drafts/",
		"output_mode": "files_with_matches",
		"stats":       true,
	}
	var keys []string
	t.Cleanup(func() {
		for _, key := range keys {
			dropSearchCorpus(key)
		}
	})
	keys = append(keys,
		searchIndexCorpusKey(dir, resolveSearchExcludePattern(nil, dir), false),
		searchIndexCorpusKey(dir, resolveSearchExcludePattern(userArgs, dir), false),
	)

	rootOut := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "UserDirExcludeToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(rootOut, keep) || !strings.Contains(rootOut, nested) || strings.Contains(rootOut, skipLog) || !strings.Contains(rootOut, "mode=indexed") {
		t.Fatalf("gitignore corpus:\n%s", rootOut)
	}

	if !searchArgsIndexReady(userArgs) {
		t.Fatal("directory exclude should be ready from the gitignore corpus")
	}
	userOut := ToolRipgrep(userArgs)
	if !strings.Contains(userOut, keep) || strings.Contains(userOut, nested) || strings.Contains(userOut, skipLog) || !strings.Contains(userOut, "mode=indexed") {
		t.Fatalf("directory exclude should reuse the gitignore corpus:\n%s", userOut)
	}
	searchBuildMu.Lock()
	userSlot := searchBuildSlots[keys[1]]
	searchBuildMu.Unlock()
	if userSlot != nil {
		t.Fatal("directory exclude scheduled its own corpus")
	}
}

func TestToolRipgrepChangedGitignoreDoesNotUseStaleCorpus(t *testing.T) {
	dir, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Clean(dir)
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	ignorePath := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(ignorePath, []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	token := "package main\nfunc ChangedGitignoreCorpusToken() {}\n"
	keep := filepath.Join(dir, "keep.go")
	skipLog := filepath.Join(dir, "skip.log")
	skipTmp := filepath.Join(dir, "skip.tmp")
	for _, path := range []string{keep, skipLog, skipTmp} {
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldExclude := resolveSearchExcludePattern(nil, dir)
	var keys []string
	t.Cleanup(func() {
		for _, key := range keys {
			dropSearchCorpus(key)
		}
	})
	keys = append(keys, searchIndexCorpusKey(dir, oldExclude, false))

	rootOut := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "ChangedGitignoreCorpusToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(rootOut, keep) || !strings.Contains(rootOut, skipTmp) || strings.Contains(rootOut, skipLog) || !strings.Contains(rootOut, "mode=indexed") {
		t.Fatalf("gitignore corpus:\n%s", rootOut)
	}

	if err := os.WriteFile(ignorePath, []byte("*.tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stamped := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(ignorePath, stamped, stamped); err != nil {
		t.Fatal(err)
	}
	changedExclude := resolveSearchExcludePattern(nil, dir)
	if changedExclude == oldExclude || strings.HasSuffix(changedExclude, oldExclude) {
		t.Fatalf("changed gitignore exclude %q still matches stale %q", changedExclude, oldExclude)
	}
	keys = append(keys, searchIndexCorpusKey(dir, changedExclude, false))
	changedArgs := map[string]interface{}{
		"path":        dir,
		"pattern":     "ChangedGitignoreCorpusToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	}
	if publishedSearchIndex(dir, "", oldExclude, "", false) == nil {
		t.Fatal("stale gitignore corpus should still be published")
	}
	if searchArgsIndexReady(changedArgs) {
		t.Fatal("a changed gitignore should not reuse the old corpus")
	}
	changed := ToolRipgrep(changedArgs)
	if !strings.Contains(changed, keep) || !strings.Contains(changed, skipLog) || strings.Contains(changed, skipTmp) || !strings.Contains(changed, "mode=full_scan") {
		t.Fatalf("changed gitignore should full-scan:\n%s", changed)
	}
}

func dropSearchCorpus(key string) {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		searchBuildMu.Lock()
		slot := searchBuildSlots[key]
		running := slot != nil && slot.running
		searchBuildMu.Unlock()
		if !running {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	forgetSearchBuildSlot(key)
	searchIndexCache.Lock()
	delete(searchIndexCache.byRoot, key)
	searchIndexCache.Unlock()
}

func TestParentIgnoreScopesReuseUntilStampChanges(t *testing.T) {
	dir, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Clean(dir)
	if err := os.MkdirAll(filepath.Join(dir, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	ignore := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(ignore, []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(src, "keep.go")
	if err := os.WriteFile(keep, []byte("package main\nfunc ParentIgnoreCacheToken() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		parentIgnoreCache.Lock()
		delete(parentIgnoreCache.byRoot, searchPathKey(src))
		parentIgnoreCache.Unlock()
	})

	before := parentIgnoreCollects.Load()
	first := parentIgnoreScopes(src)
	if parentIgnoreCollects.Load() != before+1 {
		t.Fatalf("collects = %d, want %d", parentIgnoreCollects.Load(), before+1)
	}
	if !parentScopeDropsLog(first) {
		t.Fatalf("scopes = %#v", first)
	}
	if scopes := parentIgnoreScopes(src); parentIgnoreCollects.Load() != before+1 || !parentScopeDropsLog(scopes) {
		t.Fatal("unchanged parent ignore should reuse the parsed scopes")
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":    keep,
		"pattern": "ParentIgnoreCacheToken",
		"stats":   true,
	})
	if !strings.Contains(out, "ParentIgnoreCacheToken") {
		t.Fatalf("file search should see keep.go:\n%s", out)
	}
	if parentIgnoreCollects.Load() != before+1 {
		t.Fatal("file search should reuse the parent ignore scopes")
	}

	if err := os.WriteFile(ignore, []byte("*.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stamped := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(ignore, stamped, stamped); err != nil {
		t.Fatal(err)
	}
	if parentScopeDropsLog(parentIgnoreScopes(src)) || parentIgnoreCollects.Load() != before+2 {
		t.Fatal("a changed gitignore stamp should read the rules again")
	}
	hidden := ToolRipgrep(map[string]interface{}{
		"path":    keep,
		"pattern": "ParentIgnoreCacheToken",
		"stats":   true,
	})
	if strings.Contains(hidden, "ParentIgnoreCacheToken") || !strings.Contains(hidden, "no matches found") {
		t.Fatalf("updated parent gitignore should hide the file:\n%s", hidden)
	}
}

func parentScopeDropsLog(scopes []parentIgnoreScope) bool {
	for _, scope := range scopes {
		excluded, err := parentScopeExcludes(scope, "drop.log", false)
		if err == nil && excluded {
			return true
		}
	}
	return false
}

func TestRootIgnoreFileReuseUntilStampChanges(t *testing.T) {
	dir, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Clean(dir)
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	ignore := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(ignore, []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		rootIgnoreCache.Lock()
		delete(rootIgnoreCache.byDir, searchPathKey(dir))
		rootIgnoreCache.Unlock()
		ignoreFileCache.Lock()
		delete(ignoreFileCache.byPath, searchPathKey(ignore))
		delete(ignoreFileCache.byPath, searchPathKey(filepath.Join(dir, ".ignore")))
		delete(ignoreFileCache.byPath, searchPathKey(filepath.Join(dir, ".git", "info", "exclude")))
		ignoreFileCache.Unlock()
	})

	before := ignoreFileReads.Load()
	first := rootIgnoreExcludePattern(dir)
	if !strings.Contains(first, "*.log") {
		t.Fatalf("pattern = %q", first)
	}
	readOnce := ignoreFileReads.Load()
	if readOnce <= before {
		t.Fatal("first call should read the gitignore")
	}
	if second := rootIgnoreExcludePattern(dir); second != first || ignoreFileReads.Load() != readOnce {
		t.Fatal("unchanged directory ignore should not be read again")
	}

	// Dropping the joined pattern still reuses the file bytes, including a
	// cached miss for .ignore and info/exclude.
	rootIgnoreCache.Lock()
	delete(rootIgnoreCache.byDir, searchPathKey(dir))
	rootIgnoreCache.Unlock()
	if again := rootIgnoreExcludePattern(dir); again != first || ignoreFileReads.Load() != readOnce {
		t.Fatal("ignore file bytes should be reused when the directory result is dropped")
	}

	file := filepath.Join(dir, "a.go")
	if err := os.WriteFile(file, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if third := rootIgnoreExcludePattern(file); third != first || ignoreFileReads.Load() != readOnce {
		t.Fatal("a file path should use the directory cache")
	}
	if err := os.WriteFile(ignore, []byte("*.tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stamped := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(ignore, stamped, stamped); err != nil {
		t.Fatal(err)
	}
	fourth := rootIgnoreExcludePattern(dir)
	if fourth == first || !strings.Contains(fourth, "*.tmp") || ignoreFileReads.Load() == readOnce {
		t.Fatalf("pattern = %q reads = %d", fourth, ignoreFileReads.Load())
	}
}

func TestWorktreeGitFileStopsParentIgnore(t *testing.T) {
	// Sibling worktree: no parent .git directory. The .git file is the root.
	// Without that boundary the worktree .gitignore is dropped.
	sibling := t.TempDir()
	wt := filepath.Join(sibling, "wt")
	if err := os.MkdirAll(filepath.Join(wt, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /unused\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(wt, "sub", "keep.go")
	drop := filepath.Join(wt, "sub", "drop.log")
	for _, path := range []string{keep, drop} {
		if err := os.WriteFile(path, []byte("package main\nfunc WorktreeIgnoreToken() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wtAbs, err := filepath.Abs(wt)
	if err != nil {
		t.Fatal(err)
	}
	if got := searchWarmRoot(keep); got != filepath.Clean(wtAbs) {
		t.Fatalf("warm root = %s, want worktree %s", got, filepath.Clean(wtAbs))
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        filepath.Join(wt, "sub"),
		"pattern":     "WorktreeIgnoreToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, keep) || strings.Contains(out, drop) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("worktree gitignore was not applied:\n%s", out)
	}

	// Nested gitlink: the outer repository's rules stop at the inner .git file.
	outer := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outer, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outer, ".gitignore"), []byte("*.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(outer, "mod")
	if err := os.MkdirAll(filepath.Join(inner, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, ".git"), []byte("gitdir: /unused\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	innerKeep := filepath.Join(inner, "sub", "keep.go")
	innerDrop := filepath.Join(inner, "sub", "drop.log")
	for _, path := range []string{innerKeep, innerDrop} {
		if err := os.WriteFile(path, []byte("package main\nfunc NestedGitlinkToken() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	innerAbs, err := filepath.Abs(inner)
	if err != nil {
		t.Fatal(err)
	}
	if got := searchWarmRoot(innerKeep); got != filepath.Clean(innerAbs) {
		t.Fatalf("warm root = %s, want gitlink %s", got, filepath.Clean(innerAbs))
	}
	nested := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        filepath.Join(inner, "sub"),
		"pattern":     "NestedGitlinkToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(nested, innerKeep) || strings.Contains(nested, innerDrop) || !strings.Contains(nested, "mode=indexed") {
		t.Fatalf("outer gitignore crossed the gitlink:\n%s", nested)
	}
}

func TestWorktreeGitdirInfoExclude(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real.git")
	wt := filepath.Join(base, "wt")
	if err := os.MkdirAll(filepath.Join(real, "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wt, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "info", "exclude"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: ../real.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := searchGitInfoExcludeFile(wt), filepath.Join(real, "info", "exclude"); got != want {
		t.Fatalf("info/exclude = %s, want %s", got, want)
	}
	absWT := filepath.Join(base, "abs")
	if err := os.MkdirAll(absWT, 0o755); err != nil {
		t.Fatal(err)
	}
	absGit, err := filepath.Abs(real)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(absWT, ".git"), []byte("gitdir: "+absGit+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := searchGitInfoExcludeFile(absWT), filepath.Join(absGit, "info", "exclude"); got != want {
		t.Fatalf("absolute gitdir info/exclude = %s, want %s", got, want)
	}
	bogus := filepath.Join(base, "bogus")
	if err := os.MkdirAll(bogus, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bogus, ".git"), []byte("not a gitfile\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := searchGitInfoExcludeFile(bogus); got != "" {
		t.Fatalf("non-gitfile should not resolve info/exclude: %s", got)
	}

	token := "package main\nfunc GitdirExcludeToken() {}\n"
	keep := filepath.Join(wt, "sub", "keep.go")
	drop := filepath.Join(wt, "sub", "drop.log")
	rootDrop := filepath.Join(wt, "root.log")
	for _, path := range []string{keep, drop, rootDrop} {
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	search := func(path string) string {
		t.Helper()
		return ripgrepWhenIndexed(t, map[string]interface{}{
			"path":        path,
			"pattern":     "GitdirExcludeToken",
			"output_mode": "files_with_matches",
			"stats":       true,
		})
	}
	sub := search(filepath.Join(wt, "sub"))
	if !strings.Contains(sub, keep) || strings.Contains(sub, drop) || !strings.Contains(sub, "mode=indexed") {
		t.Fatalf("subdirectory search missed the gitdir info/exclude:\n%s", sub)
	}
	fromRoot := search(wt)
	if !strings.Contains(fromRoot, keep) || strings.Contains(fromRoot, drop) || strings.Contains(fromRoot, rootDrop) || !strings.Contains(fromRoot, "mode=indexed") {
		t.Fatalf("worktree root search missed the gitdir info/exclude:\n%s", fromRoot)
	}

	// The outer repository's info/exclude stops at the inner gitfile.
	// The inner gitdir's own info/exclude still applies.
	outer := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outer, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outer, ".git", "info", "exclude"), []byte("*.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	innerGit := filepath.Join(outer, "inner.git")
	inner := filepath.Join(outer, "mod")
	if err := os.MkdirAll(filepath.Join(innerGit, "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(inner, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(innerGit, "info", "exclude"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, ".git"), []byte("gitdir: ../inner.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	innerKeep := filepath.Join(inner, "sub", "keep.go")
	innerDrop := filepath.Join(inner, "sub", "drop.log")
	for _, path := range []string{innerKeep, innerDrop} {
		if err := os.WriteFile(path, []byte("package main\nfunc NestedGitdirToken() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	nested := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        filepath.Join(inner, "sub"),
		"pattern":     "NestedGitdirToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(nested, innerKeep) || strings.Contains(nested, innerDrop) || !strings.Contains(nested, "mode=indexed") {
		t.Fatalf("gitdir info/exclude crossed repositories:\n%s", nested)
	}

	t.Run("symlink", func(t *testing.T) {
		linkBase := t.TempDir()
		realDir := filepath.Join(linkBase, "real.git")
		linkWT := filepath.Join(linkBase, "wt")
		if err := os.MkdirAll(filepath.Join(realDir, "info"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(linkWT, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(realDir, "info", "exclude"), []byte("*.log\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(realDir, filepath.Join(linkWT, ".git")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if got, want := searchGitInfoExcludeFile(linkWT), filepath.Join(linkWT, ".git", "info", "exclude"); got != want {
			t.Fatalf("symlink info/exclude = %s, want %s", got, want)
		}
		linkKeep := filepath.Join(linkWT, "sub", "keep.go")
		linkDrop := filepath.Join(linkWT, "sub", "drop.log")
		for _, path := range []string{linkKeep, linkDrop} {
			if err := os.WriteFile(path, []byte("package main\nfunc SymlinkGitExcludeToken() {}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		out := ripgrepWhenIndexed(t, map[string]interface{}{
			"path":        filepath.Join(linkWT, "sub"),
			"pattern":     "SymlinkGitExcludeToken",
			"output_mode": "files_with_matches",
			"stats":       true,
		})
		if !strings.Contains(out, linkKeep) || strings.Contains(out, linkDrop) || !strings.Contains(out, "mode=indexed") {
			t.Fatalf("symlink .git did not follow to info/exclude:\n%s", out)
		}
	})
}

func TestWorktreeCommondirInfoExclude(t *testing.T) {
	// Linked worktree: gitdir points at .git/worktrees/wt, and commondir
	// points back at the main git directory. info/exclude lives there.
	// A file at the per-worktree git dir must not win.
	base := t.TempDir()
	common := filepath.Join(base, "repo", ".git")
	wtGit := filepath.Join(common, "worktrees", "wt")
	wt := filepath.Join(base, "wt")
	if err := os.MkdirAll(filepath.Join(common, "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wtGit, "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wt, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(common, "info", "exclude"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtGit, "info", "exclude"), []byte("*.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtGit, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: ../repo/.git/worktrees/wt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := searchGitInfoExcludeFile(wt), filepath.Join(common, "info", "exclude"); got != want {
		t.Fatalf("commondir info/exclude = %s, want %s", got, want)
	}
	absGit := filepath.Join(base, "abs.git")
	if err := os.MkdirAll(absGit, 0o755); err != nil {
		t.Fatal(err)
	}
	absCommon, err := filepath.Abs(common)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(absGit, "commondir"), []byte(absCommon+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	absWT := filepath.Join(base, "abs-wt")
	if err := os.MkdirAll(absWT, 0o755); err != nil {
		t.Fatal(err)
	}
	absGitDir, err := filepath.Abs(absGit)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(absWT, ".git"), []byte("gitdir: "+absGitDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := searchGitInfoExcludeFile(absWT), filepath.Join(absCommon, "info", "exclude"); got != want {
		t.Fatalf("absolute commondir info/exclude = %s, want %s", got, want)
	}
	badGit := filepath.Join(base, "bad.git")
	badWT := filepath.Join(base, "bad-wt")
	if err := os.MkdirAll(filepath.Join(badGit, "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(badWT, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badGit, "info", "exclude"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badGit, "commondir"), []byte("../no-such-dir\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badWT, ".git"), []byte("gitdir: ../bad.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := searchGitInfoExcludeFile(badWT); got != "" {
		t.Fatalf("broken commondir fell back to the per-worktree exclude: %s", got)
	}

	token := "package main\nfunc CommondirExcludeToken() {}\n"
	keep := filepath.Join(wt, "sub", "keep.go")
	drop := filepath.Join(wt, "sub", "drop.log")
	for _, path := range []string{keep, drop} {
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        filepath.Join(wt, "sub"),
		"pattern":     "CommondirExcludeToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, keep) || strings.Contains(out, drop) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("linked worktree did not use the common info/exclude:\n%s", out)
	}
	fromRoot := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        wt,
		"pattern":     "CommondirExcludeToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(fromRoot, keep) || strings.Contains(fromRoot, drop) || !strings.Contains(fromRoot, "mode=indexed") {
		t.Fatalf("worktree root search did not use the common info/exclude:\n%s", fromRoot)
	}
}

func TestNestedRepoInfoExcludeAppliesFromAncestor(t *testing.T) {
	// Searching the outer repository must apply a nested gitfile's
	// info/exclude under that repo only. The outer tree's own logs stay.
	outer := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outer, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	modGit := filepath.Join(outer, "mod.git")
	mod := filepath.Join(outer, "mod")
	if err := os.MkdirAll(filepath.Join(modGit, "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(mod, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modGit, "info", "exclude"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod, ".git"), []byte("gitdir: ../mod.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	token := "package main\nfunc NestedRepoExcludeToken() {}\n"
	rootKeep := filepath.Join(outer, "keep.go")
	rootLog := filepath.Join(outer, "notes.log")
	innerKeep := filepath.Join(mod, "sub", "keep.go")
	innerDrop := filepath.Join(mod, "sub", "drop.log")
	for _, path := range []string{rootKeep, rootLog, innerKeep, innerDrop} {
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        outer,
		"pattern":     "NestedRepoExcludeToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, rootKeep) || !strings.Contains(out, rootLog) || !strings.Contains(out, innerKeep) || strings.Contains(out, innerDrop) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("ancestor search missed the nested info/exclude:\n%s", out)
	}
	ignored := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        outer,
		"pattern":     "NestedRepoExcludeToken",
		"output_mode": "files_with_matches",
		"stats":       true,
		"no_ignore":   true,
	})
	if !strings.Contains(ignored, innerDrop) || !strings.Contains(ignored, "mode=indexed") {
		t.Fatalf("no_ignore should still return the nested log:\n%s", ignored)
	}
}

func TestToolRipgrepExplicitIgnoredDirectoryStaysSearchable(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("src/\n*.log\ngenerated/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	token := "package main\nfunc ExplicitIgnoreToken() {}\n"
	rootGo := filepath.Join(dir, "root.go")
	keep := filepath.Join(dir, "src", "keep.go")
	drop := filepath.Join(dir, "src", "drop.log")
	nested := filepath.Join(dir, "src", "sub", "nested.go")
	generated := filepath.Join(dir, "src", "generated", "out.go")
	other := filepath.Join(dir, "other", "src", "other.go")
	for _, path := range []string{rootGo, keep, drop, nested, generated, other} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	search := func(path string) map[string]bool {
		t.Helper()
		out := ripgrepWhenIndexed(t, map[string]interface{}{
			"path":        path,
			"pattern":     "ExplicitIgnoreToken",
			"output_mode": "files_with_matches",
			"stats":       true,
		})
		found := map[string]bool{}
		for _, line := range strings.Split(out, "\n") {
			found[strings.TrimSpace(line)] = true
		}
		if !strings.Contains(out, "mode=indexed") || strings.Contains(out, "search failed") {
			t.Fatalf("explicit ignored directory search failed:\n%s", out)
		}
		return found
	}
	inside := search(filepath.Join(dir, "src"))
	if !inside[keep] || !inside[nested] || inside[drop] || inside[generated] || inside[other] || inside[rootGo] {
		t.Fatalf("search inside src hid files that only the directory rule matched: %#v", inside)
	}
	deeper := search(filepath.Join(dir, "src", "sub"))
	if !deeper[nested] || deeper[keep] || deeper[drop] || deeper[generated] {
		t.Fatalf("search inside src/sub applied the parent directory rule to children: %#v", deeper)
	}
	fromRoot := search(dir)
	if !fromRoot[rootGo] || fromRoot[keep] || fromRoot[nested] || fromRoot[drop] || fromRoot[generated] || fromRoot[other] {
		t.Fatalf("repo search should still apply the directory rule: %#v", fromRoot)
	}
}

func TestToolRipgrepExplicitSkipDirStaysSearchable(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	token := "package main\nfunc SkipDirToken() {}\n"
	rootGo := filepath.Join(dir, "keep.go")
	// vendor is a skip name on the way down, not the directory the user named.
	skipped := filepath.Join(dir, "vendor", "nested", "lib.go")
	for _, path := range []string{rootGo, skipped} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rootOut := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "SkipDirToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(rootOut, rootGo) || strings.Contains(rootOut, skipped) || !strings.Contains(rootOut, "mode=indexed") {
		t.Fatalf("repo index should list keep.go and skip vendor:\n%s", rootOut)
	}
	inside := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        filepath.Dir(skipped),
		"pattern":     "SkipDirToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(inside, skipped) || strings.Contains(inside, "search failed") {
		t.Fatalf("naming a directory under vendor should still find its file:\n%s", inside)
	}
}

func TestToolRipgrepExplicitReparseDirStaysSearchable(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	outside := filepath.Join(root, "outside", "nested")
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	token := "package main\nfunc ReparseCoverToken() {}\n"
	keep := filepath.Join(project, "keep.go")
	secret := filepath.Join(outside, "secret.go")
	for _, path := range []string{keep, secret} {
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(project, "link")
	if runtime.GOOS == "windows" {
		out, err := exec.Command("cmd", "/c", "mklink", "/J", link, filepath.Dir(outside)).CombinedOutput()
		if err != nil {
			t.Skipf("junction not available: %v (%s)", err, out)
		}
	} else if err := os.Symlink(filepath.Dir(outside), link); err != nil {
		t.Skipf("directory symlink not available: %v", err)
	}
	named := filepath.Join(link, "nested")
	rootOut := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        project,
		"pattern":     "ReparseCoverToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(rootOut, keep) || strings.Contains(rootOut, secret) || !strings.Contains(rootOut, "mode=indexed") {
		t.Fatalf("project index should list keep.go and not walk the link:\n%s", rootOut)
	}
	inside := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        named,
		"pattern":     "ReparseCoverToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if strings.Contains(inside, "search failed") || (!strings.Contains(inside, secret) && !strings.Contains(inside, filepath.Join(named, "secret.go"))) {
		t.Fatalf("naming a directory through a junction should still find its file:\n%s", inside)
	}
}

func TestToolRipgrepUnlistedDirStaysSearchable(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	token := "package main\nfunc UnlistedCoverToken() {}\n"
	keep := filepath.Join(dir, "keep.go")
	secret := filepath.Join(dir, "locked", "secret.go")
	for _, path := range []string{keep, secret} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lockedAbs, err := filepath.Abs(filepath.Dir(secret))
	if err != nil {
		t.Fatal(err)
	}
	lockedAbs = filepath.Clean(lockedAbs)
	previousWalk := searchIndexWalk
	t.Cleanup(func() { searchIndexWalk = previousWalk })
	searchIndexWalk = func(ctx context.Context, walkRoot string, fn fs.WalkDirFunc) error {
		return previousWalk(ctx, walkRoot, func(path string, d os.DirEntry, err error) error {
			if err == nil && d != nil && d.IsDir() && searchPathEqual(path, lockedAbs) {
				if callErr := fn(path, d, nil); callErr != nil {
					return callErr
				}
				if callErr := fn(path, d, os.ErrPermission); callErr != nil && callErr != filepath.SkipDir {
					return callErr
				}
				return filepath.SkipDir
			}
			return fn(path, d, err)
		})
	}
	rootOut := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "UnlistedCoverToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(rootOut, keep) || !strings.Contains(rootOut, secret) || !strings.Contains(rootOut, "mode=indexed") {
		t.Fatalf("a directory the build could not list is still readable, so the repo search must return its file:\n%s", rootOut)
	}
	searchIndexWalk = previousWalk
	inside := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        filepath.Dir(secret),
		"pattern":     "UnlistedCoverToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if strings.Contains(inside, "search failed") || !strings.Contains(inside, secret) {
		t.Fatalf("naming the directory the build could not list should still find its file:\n%s", inside)
	}
}

func TestToolRipgrepUnlistedChildListingKeepsSiblings(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	token := "package main\nfunc UnlistedPartialToken() {}\n"
	keep := filepath.Join(dir, "keep.go")
	early := filepath.Join(dir, "locked", "a_early.go")
	later := filepath.Join(dir, "locked", "z_later.go")
	hidden := filepath.Join(dir, "locked", "nested", "hidden.go")
	for _, path := range []string{keep, early, later, hidden} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lockedAbs, err := filepath.Abs(filepath.Dir(early))
	if err != nil {
		t.Fatal(err)
	}
	lockedAbs = filepath.Clean(lockedAbs)
	nestedAbs := filepath.Join(lockedAbs, "nested")
	previousWalk := searchIndexWalk
	t.Cleanup(func() { searchIndexWalk = previousWalk })
	searchIndexWalk = func(ctx context.Context, walkRoot string, fn fs.WalkDirFunc) error {
		return previousWalk(ctx, walkRoot, func(path string, d os.DirEntry, err error) error {
			if err == nil && d != nil && d.IsDir() && searchPathEqual(path, lockedAbs) {
				if callErr := fn(path, d, nil); callErr != nil {
					return callErr
				}
				if callErr := fn(path, d, os.ErrPermission); callErr != nil && callErr != filepath.SkipDir {
					return callErr
				}
				return filepath.SkipDir
			}
			return fn(path, d, err)
		})
	}
	args := map[string]interface{}{
		"path":        dir,
		"pattern":     "UnlistedPartialToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	}
	first := ripgrepWhenIndexed(t, args)
	if !strings.Contains(first, keep) || !strings.Contains(first, "mode=indexed") {
		t.Fatalf("corpus should publish around the unlisted directory:\n%s", first)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	idx := publishedSearchIndex(root, "", resolveSearchExcludePattern(args, root), "", false)
	if idx == nil || len(idx.unlisted) == 0 {
		t.Fatal("unlisted directory was not recorded on the published corpus")
	}
	for _, path := range idx.files {
		if strings.Contains(path, "a_early.go") || strings.Contains(path, "z_later.go") || strings.Contains(path, "hidden.go") {
			t.Fatalf("build indexed a file under the unlisted directory: %s", path)
		}
	}
	searchIndexWalk = previousWalk
	previousBatch := readSearchDirBatch
	t.Cleanup(func() { readSearchDirBatch = previousBatch })
	readSearchDirBatch = func(f *os.File, n int) ([]os.DirEntry, error) {
		if f != nil && searchPathEqual(f.Name(), nestedAbs) {
			return nil, os.ErrPermission
		}
		return f.ReadDir(n)
	}
	out := ToolRipgrep(args)
	if !strings.Contains(out, early) || !strings.Contains(out, later) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("files already read under a partly listable directory must stay in the indexed result:\n%s", out)
	}
	if strings.Contains(out, hidden) {
		t.Fatalf("a child that still cannot be listed must stay out of the result:\n%s", out)
	}
}

func TestToolRipgrepListableChildOfUnlistedDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	token := "package main\nfunc ListableChildToken() {}\n"
	keep := filepath.Join(dir, "keep.go")
	secret := filepath.Join(dir, "locked", "nested", "secret.go")
	for _, path := range []string{keep, secret} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lockedAbs, err := filepath.Abs(filepath.Join(dir, "locked"))
	if err != nil {
		t.Fatal(err)
	}
	lockedAbs = filepath.Clean(lockedAbs)
	previousWalk := searchIndexWalk
	t.Cleanup(func() { searchIndexWalk = previousWalk })
	searchIndexWalk = func(ctx context.Context, walkRoot string, fn fs.WalkDirFunc) error {
		return previousWalk(ctx, walkRoot, func(path string, d os.DirEntry, err error) error {
			if err == nil && d != nil && d.IsDir() && searchPathEqual(path, lockedAbs) {
				if callErr := fn(path, d, nil); callErr != nil {
					return callErr
				}
				if callErr := fn(path, d, os.ErrPermission); callErr != nil && callErr != filepath.SkipDir {
					return callErr
				}
				return filepath.SkipDir
			}
			return fn(path, d, err)
		})
	}
	args := map[string]interface{}{
		"path":        dir,
		"pattern":     "ListableChildToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	}
	first := ripgrepWhenIndexed(t, args)
	if !strings.Contains(first, keep) || !strings.Contains(first, "mode=indexed") {
		t.Fatalf("corpus should publish around the unlisted directory:\n%s", first)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	idx := publishedSearchIndex(root, "", resolveSearchExcludePattern(args, root), "", false)
	if idx == nil || len(idx.unlisted) == 0 {
		t.Fatal("unlisted directory was not recorded on the published corpus")
	}
	for _, path := range idx.files {
		if strings.Contains(path, "secret.go") {
			t.Fatalf("build indexed a file under the unlisted directory: %s", path)
		}
	}
	searchIndexWalk = previousWalk
	previousBatch := readSearchDirBatch
	t.Cleanup(func() { readSearchDirBatch = previousBatch })
	readSearchDirBatch = func(f *os.File, n int) ([]os.DirEntry, error) {
		if f != nil && searchPathEqual(f.Name(), lockedAbs) {
			return nil, os.ErrPermission
		}
		return f.ReadDir(n)
	}
	out := ToolRipgrep(map[string]interface{}{
		"path":        filepath.Dir(secret),
		"pattern":     "ListableChildToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, secret) || !strings.Contains(out, "fallback=unlisted_directory") {
		t.Fatalf("a listable directory inside an unlistable parent should be walked:\n%s", out)
	}
}

func TestToolRipgrepCancelledUnlistedListingStops(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	token := "package main\nfunc CancelledListingToken() {}\n"
	keep := filepath.Join(dir, "keep.go")
	secret := filepath.Join(dir, "locked", "secret.go")
	for _, path := range []string{keep, secret} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lockedAbs, err := filepath.Abs(filepath.Join(dir, "locked"))
	if err != nil {
		t.Fatal(err)
	}
	lockedAbs = filepath.Clean(lockedAbs)
	previousWalk := searchIndexWalk
	t.Cleanup(func() { searchIndexWalk = previousWalk })
	searchIndexWalk = func(ctx context.Context, walkRoot string, fn fs.WalkDirFunc) error {
		return previousWalk(ctx, walkRoot, func(path string, d os.DirEntry, err error) error {
			if err == nil && d != nil && d.IsDir() && searchPathEqual(path, lockedAbs) {
				if callErr := fn(path, d, nil); callErr != nil {
					return callErr
				}
				if callErr := fn(path, d, os.ErrPermission); callErr != nil && callErr != filepath.SkipDir {
					return callErr
				}
				return filepath.SkipDir
			}
			return fn(path, d, err)
		})
	}
	args := map[string]interface{}{
		"path":        dir,
		"pattern":     "CancelledListingToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	}
	first := ripgrepWhenIndexed(t, args)
	if !strings.Contains(first, keep) || !strings.Contains(first, "mode=indexed") {
		t.Fatalf("corpus should publish around the unlisted directory:\n%s", first)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	idx := publishedSearchIndex(root, "", resolveSearchExcludePattern(args, root), "", false)
	if idx == nil || len(idx.unlisted) == 0 {
		t.Fatal("unlisted directory was not recorded on the published corpus")
	}
	searchIndexWalk = previousWalk

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	reads := 0
	previousBatch := readSearchDirBatch
	t.Cleanup(func() { readSearchDirBatch = previousBatch })
	readSearchDirBatch = func(f *os.File, n int) ([]os.DirEntry, error) {
		if f == nil || !searchPathEqual(f.Name(), lockedAbs) {
			return f.ReadDir(n)
		}
		reads++
		if reads == 1 {
			entries, _ := f.ReadDir(n)
			// Nil, not EOF. An empty or EOF batch is a finished listing, and
			// the probe would leave before the next read. The directory has a
			// file, so this batch must keep the probe in its loop.
			cancel()
			if len(entries) == 0 {
				t.Errorf("locked listing returned no entries")
			}
			return entries, nil
		}
		<-release
		return f.ReadDir(n)
	}
	searchArgs := map[string]interface{}{
		"path":        lockedAbs,
		"pattern":     "CancelledListingToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	}
	done := make(chan SearchToolResult, 1)
	go func() {
		done <- ToolRipgrepDetailedCtx(ctx, searchArgs)
	}()
	select {
	case result := <-done:
		if !strings.Contains(result.Text, "search cancelled") {
			t.Fatalf("cancelling during the unlisted listing should stop the search:\n%s", result.Text)
		}
		if reads != 1 {
			t.Fatalf("cancelled listing read locked %d times", reads)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled unlisted listing did not stop")
	}
}

func TestToolRipgrepUnlistableChildKeepsIndexedFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	token := "package main\nfunc PreservedNestedToken() {}\n"
	keep := filepath.Join(dir, "keep.go")
	secret := filepath.Join(dir, "locked", "nested", "secret.go")
	for _, path := range []string{keep, secret} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	args := map[string]interface{}{
		"path":        dir,
		"pattern":     "PreservedNestedToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	}
	first := ripgrepWhenIndexed(t, args)
	if !strings.Contains(first, secret) || !strings.Contains(first, "mode=indexed") {
		t.Fatalf("warmup missed the nested file:\n%s", first)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	lockedAbs, err := filepath.Abs(filepath.Join(dir, "locked"))
	if err != nil {
		t.Fatal(err)
	}
	lockedAbs = filepath.Clean(lockedAbs)
	nestedAbs := filepath.Join(lockedAbs, "nested")
	previousWalk := searchIndexWalk
	t.Cleanup(func() { searchIndexWalk = previousWalk })
	searchIndexWalk = func(ctx context.Context, walkRoot string, fn fs.WalkDirFunc) error {
		return previousWalk(ctx, walkRoot, func(path string, d os.DirEntry, err error) error {
			if err == nil && d != nil && d.IsDir() && searchPathEqual(path, lockedAbs) {
				if callErr := fn(path, d, nil); callErr != nil {
					return callErr
				}
				if callErr := fn(path, d, os.ErrPermission); callErr != nil && callErr != filepath.SkipDir {
					return callErr
				}
				return filepath.SkipDir
			}
			return fn(path, d, err)
		})
	}
	exclude := resolveSearchExcludePattern(args, root)
	if !reconcilePublishedCorpus(root, exclude, false) {
		t.Fatal("unlistable directory failed reconcile")
	}
	searchIndexWalk = previousWalk
	idx := publishedSearchIndex(root, "", exclude, "", false)
	if idx == nil || len(idx.unlisted) == 0 {
		t.Fatal("reconcile did not record the unlisted directory")
	}
	kept := false
	for path := range idx.fileSet {
		if strings.Contains(path, "secret.go") {
			kept = true
			break
		}
	}
	if !kept {
		t.Fatal("reconcile dropped the indexed file inside the unlisted directory")
	}
	previousBatch := readSearchDirBatch
	t.Cleanup(func() { readSearchDirBatch = previousBatch })
	readSearchDirBatch = func(f *os.File, n int) ([]os.DirEntry, error) {
		if f != nil && searchPathEqual(f.Name(), nestedAbs) {
			return nil, os.ErrPermission
		}
		return f.ReadDir(n)
	}
	out := ToolRipgrep(map[string]interface{}{
		"path":        filepath.Dir(secret),
		"pattern":     "PreservedNestedToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, secret) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("an unlistable child should keep the files already indexed:\n%s", out)
	}
}

func TestToolRipgrepPartialUnlistedDirKeepsIndexedAndNewFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	token := "package main\nfunc PreservedPartialToken() {}\n"
	keep := filepath.Join(dir, "keep.go")
	secret := filepath.Join(dir, "locked", "nested", "secret.go")
	for _, path := range []string{keep, secret} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	args := map[string]interface{}{
		"path":        dir,
		"pattern":     "PreservedPartialToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	}
	first := ripgrepWhenIndexed(t, args)
	if !strings.Contains(first, secret) || !strings.Contains(first, "mode=indexed") {
		t.Fatalf("warmup missed the nested file:\n%s", first)
	}
	visible := filepath.Join(dir, "locked", "a_visible.go")
	later := filepath.Join(dir, "locked", "z_later.go")
	for _, path := range []string{visible, later} {
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	lockedAbs, err := filepath.Abs(filepath.Join(dir, "locked"))
	if err != nil {
		t.Fatal(err)
	}
	lockedAbs = filepath.Clean(lockedAbs)
	previousWalk := searchIndexWalk
	t.Cleanup(func() { searchIndexWalk = previousWalk })
	searchIndexWalk = func(ctx context.Context, walkRoot string, fn fs.WalkDirFunc) error {
		return previousWalk(ctx, walkRoot, func(path string, d os.DirEntry, err error) error {
			if err == nil && d != nil && d.IsDir() && searchPathEqual(path, lockedAbs) {
				if callErr := fn(path, d, nil); callErr != nil {
					return callErr
				}
				if callErr := fn(path, d, os.ErrPermission); callErr != nil && callErr != filepath.SkipDir {
					return callErr
				}
				return filepath.SkipDir
			}
			return fn(path, d, err)
		})
	}
	exclude := resolveSearchExcludePattern(args, root)
	if !reconcilePublishedCorpus(root, exclude, false) {
		t.Fatal("unlistable directory failed reconcile")
	}
	searchIndexWalk = previousWalk
	idx := publishedSearchIndex(root, "", exclude, "", false)
	if idx == nil || len(idx.unlisted) == 0 {
		t.Fatal("reconcile did not record the unlisted directory")
	}
	keptSecret := false
	for path := range idx.fileSet {
		if strings.Contains(path, "a_visible.go") || strings.Contains(path, "z_later.go") {
			t.Fatalf("reconcile indexed a file it could not list: %s", path)
		}
		if strings.Contains(path, "secret.go") {
			keptSecret = true
		}
	}
	if !keptSecret {
		t.Fatal("reconcile dropped the indexed file inside the unlisted directory")
	}
	previousBatch := readSearchDirBatch
	t.Cleanup(func() { readSearchDirBatch = previousBatch })
	readSearchDirBatch = func(f *os.File, n int) ([]os.DirEntry, error) {
		if f == nil || !searchPathEqual(f.Name(), lockedAbs) {
			return f.ReadDir(n)
		}
		entries, readErr := f.ReadDir(n)
		if readErr != nil {
			return nil, readErr
		}
		kept := entries[:0]
		for _, entry := range entries {
			if entry.Name() == "a_visible.go" {
				kept = append(kept, entry)
			}
		}
		return kept, os.ErrPermission
	}
	out := ToolRipgrep(map[string]interface{}{
		"path":        lockedAbs,
		"pattern":     "PreservedPartialToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, secret) || !strings.Contains(out, visible) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("a partial listing should keep indexed files and files already read:\n%s", out)
	}
	if strings.Contains(out, later) || strings.Contains(out, keep) {
		t.Fatalf("a file the partial listing did not reach should stay out of the result:\n%s", out)
	}
}

func TestToolRipgrepUnlistedInnerDirStaysSearchable(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	token := "package main\nfunc UnlistedInnerToken() {}\n"
	keep := filepath.Join(dir, "keep.go")
	nested := filepath.Join(dir, "locked", "nested")
	secret := filepath.Join(nested, "secret.go")
	for _, path := range []string{keep, secret} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	locked := filepath.Dir(nested)
	if runtime.GOOS == "windows" {
		who, err := exec.Command("whoami").Output()
		if err != nil {
			t.Skipf("whoami: %v", err)
		}
		user := strings.TrimSpace(string(who))
		t.Cleanup(func() {
			_ = exec.Command("icacls", locked, "/remove:d", user).Run()
		})
		out, err := exec.Command("icacls", locked, "/deny", user+":(RD)").CombinedOutput()
		if err != nil {
			t.Skipf("icacls deny: %v (%s)", err, out)
		}
	} else if err := os.Chmod(locked, 0o111); err != nil {
		t.Skipf("chmod: %v", err)
	} else {
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	}
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("directory list was not denied")
	}
	if _, err := os.ReadDir(nested); err != nil {
		t.Skipf("inner directory should stay listable: %v", err)
	}
	rootOut := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "UnlistedInnerToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(rootOut, keep) || !strings.Contains(rootOut, "mode=indexed") {
		t.Fatalf("repo index should publish around the unlistable directory:\n%s", rootOut)
	}
	inside := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        nested,
		"pattern":     "UnlistedInnerToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if strings.Contains(inside, "search failed") || !strings.Contains(inside, secret) {
		t.Fatalf("naming a listable directory under an unlistable parent should still find its file:\n%s", inside)
	}
}

func TestToolRipgrepUnlistedDirKeepsPartialListing(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	token := "package main\nfunc UnlistedPartialRootToken() {}\n"
	early := filepath.Join(dir, "locked", "a_early.go")
	later := filepath.Join(dir, "locked", "z_later.go")
	hidden := filepath.Join(dir, "locked", "nested", "hidden.go")
	for _, path := range []string{filepath.Join(dir, "keep.go"), early, later, hidden} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lockedAbs, err := filepath.Abs(filepath.Dir(early))
	if err != nil {
		t.Fatal(err)
	}
	lockedAbs = filepath.Clean(lockedAbs)
	nestedAbs := filepath.Join(lockedAbs, "nested")
	previousWalk := searchIndexWalk
	t.Cleanup(func() { searchIndexWalk = previousWalk })
	searchIndexWalk = func(ctx context.Context, walkRoot string, fn fs.WalkDirFunc) error {
		return previousWalk(ctx, walkRoot, func(path string, d os.DirEntry, err error) error {
			if err == nil && d != nil && d.IsDir() && searchPathEqual(path, lockedAbs) {
				if callErr := fn(path, d, nil); callErr != nil {
					return callErr
				}
				if callErr := fn(path, d, os.ErrPermission); callErr != nil && callErr != filepath.SkipDir {
					return callErr
				}
				return filepath.SkipDir
			}
			return fn(path, d, err)
		})
	}
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "UnlistedPartialRootToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	}); !strings.Contains(out, "mode=indexed") {
		t.Fatalf("corpus should publish around the unlisted directory:\n%s", out)
	}
	searchIndexWalk = previousWalk
	previousBatch := readSearchDirBatch
	t.Cleanup(func() { readSearchDirBatch = previousBatch })
	readSearchDirBatch = func(f *os.File, n int) ([]os.DirEntry, error) {
		if f != nil && searchPathEqual(f.Name(), nestedAbs) {
			return nil, os.ErrPermission
		}
		entries, err := f.ReadDir(n)
		if f != nil && searchPathEqual(f.Name(), lockedAbs) && (err == nil || err == io.EOF) {
			return entries, os.ErrPermission
		}
		return entries, err
	}
	out := ToolRipgrep(map[string]interface{}{
		"path":        lockedAbs,
		"pattern":     "UnlistedPartialRootToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, early) || !strings.Contains(out, later) || strings.Contains(out, "search failed") {
		t.Fatalf("files already read from the named directory must stay in the result:\n%s", out)
	}
	if strings.Contains(out, hidden) {
		t.Fatalf("a child that still cannot be listed must stay out of the result:\n%s", out)
	}
}

func TestToolRipgrepUnreadFileStaysSearchable(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	token := "package main\nfunc UnreadFileToken() {}\n"
	keep := filepath.Join(dir, "keep.go")
	secret := filepath.Join(dir, "secret.go")
	for _, path := range []string{keep, secret} {
		if err := os.WriteFile(path, []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	denySearchFileRead(t, secret)
	if _, err := os.ReadFile(secret); err == nil {
		t.Skip("file read was not denied")
	}
	args := map[string]interface{}{
		"path":        dir,
		"pattern":     "UnreadFileToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	}
	rootOut := ripgrepWhenIndexed(t, args)
	if !strings.Contains(rootOut, keep) || !strings.Contains(rootOut, "mode=indexed") {
		t.Fatalf("repo index should publish around the unreadable file:\n%s", rootOut)
	}
	exclude := resolveSearchExcludePattern(args, dir)
	idx := publishedSearchIndex(dir, "", exclude, "", false)
	if idx == nil {
		t.Fatal("indexed search did not publish a corpus")
	}
	for path := range idx.fileSet {
		if strings.Contains(path, "secret.go") {
			t.Fatalf("corpus indexed a file it could not read: %s", path)
		}
	}
	recorded := false
	for _, path := range idx.unread {
		if strings.Contains(path, "secret.go") {
			recorded = true
		}
	}
	if !recorded {
		t.Fatal("corpus dropped a file it listed but could not read")
	}
	allowSearchFileRead(t, secret)
	if _, err := os.ReadFile(secret); err != nil {
		t.Fatalf("file should be readable again: %v", err)
	}
	out := ToolRipgrep(args)
	if strings.Contains(out, "search failed") || !strings.Contains(out, secret) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("a file the corpus saw but could not read must be searched once it is readable:\n%s", out)
	}
	if !reconcilePublishedCorpus(dir, exclude, false) {
		t.Fatal("reconcile should index the file once it can be read")
	}
	idx = publishedSearchIndex(dir, "", exclude, "", false)
	if idx == nil {
		t.Fatal("reconcile unpublished the corpus")
	}
	indexedSecret := false
	for path := range idx.fileSet {
		if strings.Contains(path, "secret.go") {
			indexedSecret = true
		}
	}
	if !indexedSecret {
		t.Fatal("reconcile left the readable file out of the corpus")
	}
	for _, path := range idx.unread {
		if strings.Contains(path, "secret.go") {
			t.Fatalf("readable file should leave the unread list: %s", path)
		}
	}
	again := ToolRipgrep(args)
	if strings.Contains(again, "search failed") || !strings.Contains(again, secret) || !strings.Contains(again, "mode=indexed") {
		t.Fatalf("the file should stay searchable after it is indexed:\n%s", again)
	}
}

func denySearchFileRead(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		user := searchFileACLUser(t)
		out, err := exec.Command("icacls", path, "/deny", user+":(R)").CombinedOutput()
		if err != nil {
			t.Skipf("icacls deny: %v (%s)", err, out)
		}
		t.Cleanup(func() {
			_ = exec.Command("icacls", path, "/remove:d", user).Run()
		})
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mode := info.Mode().Perm()
	if err := os.Chmod(path, 0); err != nil {
		t.Skipf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, mode) })
}

func allowSearchFileRead(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		user := searchFileACLUser(t)
		out, err := exec.Command("icacls", path, "/remove:d", user).CombinedOutput()
		if err != nil {
			t.Fatalf("icacls remove deny: %v (%s)", err, out)
		}
		return
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
}

func searchFileACLUser(t *testing.T) string {
	t.Helper()
	who, err := exec.Command("whoami").Output()
	if err != nil {
		t.Skipf("whoami: %v", err)
	}
	user := strings.TrimSpace(string(who))
	if user == "" {
		t.Skip("whoami returned no user")
	}
	return user
}

func TestToolRipgrepStaleUnreadableFileStaysSearchable(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	base := "package main\nfunc StaleBaseToken() {}\n"
	keep := filepath.Join(dir, "keep.go")
	paths := []string{keep}
	for _, name := range []string{"a.go", "b.go", "c.go", "d.go"} {
		paths = append(paths, filepath.Join(dir, name))
	}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte(base), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	warm := map[string]interface{}{
		"path":        dir,
		"pattern":     "StaleBaseToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	}
	if out := ripgrepWhenIndexed(t, warm); !strings.Contains(out, keep) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("repo index should publish keep.go:\n%s", out)
	}
	if err := os.WriteFile(keep, []byte("package main\nfunc NewStaleToken() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	denySearchFileRead(t, keep)
	if _, err := os.ReadFile(keep); err == nil {
		t.Skip("file read was not denied")
	}
	exclude := resolveSearchExcludePattern(warm, dir)
	if !reconcilePublishedCorpus(dir, exclude, false) {
		t.Fatal("reconcile did not finish while the file was unreadable")
	}
	idx := publishedSearchIndex(dir, "", exclude, "", false)
	if idx == nil {
		t.Fatal("reconcile unpublished the corpus")
	}
	found := false
	for path := range idx.fileSet {
		if strings.Contains(path, "keep.go") {
			found = true
		}
	}
	if !found {
		t.Fatal("reconcile dropped the file it could not re-read")
	}
	allowSearchFileRead(t, keep)
	if _, err := os.ReadFile(keep); err != nil {
		t.Fatalf("file should be readable again: %v", err)
	}
	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "NewStaleToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if strings.Contains(out, "search failed") || !strings.Contains(out, keep) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("a file the corpus could not re-read must be searched once it is readable:\n%s", out)
	}
	if !reconcilePublishedCorpus(dir, exclude, false) {
		t.Fatal("reconcile should index the file once it can be read")
	}
	idx = publishedSearchIndex(dir, "", exclude, "", false)
	if idx == nil {
		t.Fatal("reconcile unpublished the corpus after the file was readable")
	}
	for _, path := range idx.unread {
		if strings.Contains(path, "keep.go") {
			t.Fatalf("readable file should leave the unread list: %s", path)
		}
	}
	again := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "NewStaleToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if strings.Contains(again, "search failed") || !strings.Contains(again, keep) || !strings.Contains(again, "mode=indexed") {
		t.Fatalf("the file should stay searchable after its new contents are indexed:\n%s", again)
	}
}

func TestToolRipgrepUnreadFileSameMetaStaysSearchable(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	base := "package main\nfunc StaleBaseToken() {}\n"
	finalText := "package main\nfunc SingleMissZZZZ() {}\n"
	if len(base) != len(finalText) {
		t.Fatalf("fixture lengths differ: %d %d", len(base), len(finalText))
	}
	keep := filepath.Join(dir, "keep.go")
	paths := []string{keep}
	for _, name := range []string{"a.go", "b.go", "c.go", "d.go"} {
		paths = append(paths, filepath.Join(dir, name))
	}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte(base), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	warm := map[string]interface{}{
		"path":        dir,
		"pattern":     "StaleBaseToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	}
	if out := ripgrepWhenIndexed(t, warm); !strings.Contains(out, keep) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("repo index should publish keep.go:\n%s", out)
	}
	info, err := os.Stat(keep)
	if err != nil {
		t.Fatal(err)
	}
	origSize := info.Size()
	origTime := info.ModTime()
	if err := os.WriteFile(keep, []byte("package main\nfunc X() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	denySearchFileRead(t, keep)
	if _, err := os.ReadFile(keep); err == nil {
		t.Skip("file read was not denied")
	}
	exclude := resolveSearchExcludePattern(warm, dir)
	if !reconcilePublishedCorpus(dir, exclude, false) {
		t.Fatal("reconcile did not finish while the file was unreadable")
	}
	allowSearchFileRead(t, keep)
	if err := os.WriteFile(keep, []byte(finalText), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(keep, origTime, origTime); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(keep)
	if err != nil {
		t.Fatal(err)
	}
	idx := publishedSearchIndex(dir, "", exclude, "", false)
	if idx == nil {
		t.Fatal("corpus missing after the unreadable reconcile")
	}
	onUnread := false
	for _, path := range idx.unread {
		if strings.Contains(path, "keep.go") {
			onUnread = true
		}
	}
	if !onUnread {
		t.Fatal("unreadable rewrite should stay on the unread list")
	}
	var meta localSearchFileMeta
	var metaOK bool
	for path, item := range idx.fileMeta {
		if strings.Contains(path, "keep.go") {
			meta = item
			metaOK = true
		}
	}
	if !metaOK {
		t.Fatal("keep.go missing from file meta")
	}
	if info.Size() != origSize || meta.size != info.Size() || !meta.modTime.Equal(info.ModTime()) {
		t.Fatalf("fixture is meta-stale size file=%d indexed=%d mtime file=%v indexed=%v; the file search would read it for that reason", info.Size(), meta.size, info.ModTime(), meta.modTime)
	}
	out := ToolRipgrep(map[string]interface{}{
		"path":        keep,
		"pattern":     "SingleMissZZZZ",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if strings.Contains(out, "search failed") || !strings.Contains(out, keep) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("a file on the unread list must be read even when size and mtime match the snapshot:\n%s", out)
	}
	if !reconcilePublishedCorpus(dir, exclude, false) {
		t.Fatal("reconcile should read the unread file once it is readable")
	}
	idx = publishedSearchIndex(dir, "", exclude, "", false)
	if idx == nil {
		t.Fatal("reconcile unpublished the corpus")
	}
	for _, path := range idx.unread {
		if strings.Contains(path, "keep.go") {
			t.Fatalf("a readable unread file should leave the list after reconcile: %s", path)
		}
	}
	healed := ToolRipgrep(map[string]interface{}{
		"path":        keep,
		"pattern":     "SingleMissZZZZ",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if strings.Contains(healed, "search failed") || !strings.Contains(healed, keep) || !strings.Contains(healed, "mode=indexed") {
		t.Fatalf("reconcile should index the bytes it could not read before:\n%s", healed)
	}
}

func TestParentDirectPatternKeepsEscapedTrailingSpace(t *testing.T) {
	files, dirs, ok := gitignoreDirectPatterns("foo\\ \n")
	if !ok || len(dirs) != 0 || len(files) != 1 || files[0] != "foo " {
		t.Fatalf("direct pattern = %#v dirs=%#v ok=%v", files, dirs, ok)
	}
	pattern := joinSearchIgnorePatterns(files)
	matched, err := matchGlob(pattern, "foo ", false)
	if err != nil || !matched {
		t.Fatalf("escaped trailing space should match: matched=%v err=%v pattern=%q", matched, err, pattern)
	}
	matched, err = matchGlob(pattern, "foo", false)
	if err != nil || matched {
		t.Fatalf("trimmed name should not match the escaped space: matched=%v err=%v", matched, err)
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("foo\\ \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := withTreeSearchIgnore(context.Background(), src)
	excluded, err := treeIgnoreExcludes(ctx, "foo ", false)
	if err != nil || !excluded {
		t.Fatalf("parent scope should exclude the spaced name: excluded=%v err=%v", excluded, err)
	}
	excluded, err = treeIgnoreExcludes(ctx, "foo", false)
	if err != nil || excluded {
		t.Fatalf("parent scope should keep the trimmed name: excluded=%v err=%v", excluded, err)
	}
	matched, err = matchGlob(" *.go ", "main.go", false)
	if err != nil || !matched {
		t.Fatalf("user glob should still trim surrounding spaces: matched=%v err=%v", matched, err)
	}
}

func TestToolRipgrepFixedStringMatchesRegexMetacharacters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nvalue := \"target[0]+literal\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":         dir,
		"pattern":      "target[0]+literal",
		"fixed_string": true,
		"output_mode":  "files_with_matches",
		"stats":        true,
	})
	if !strings.Contains(out, path) {
		t.Fatalf("ripgrep fixed_string missed literal metacharacters:\n%s", out)
	}
	if !strings.Contains(out, "mode=indexed") {
		t.Fatalf("ripgrep fixed_string should use literal index candidates:\n%s", out)
	}
}

func TestToolRipgrepFixedStringUnicodeUsesIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(path, []byte("本地检索能力已经启用\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":         dir,
		"pattern":      "本地检索能力",
		"fixed_string": true,
		"output_mode":  "files_with_matches",
		"stats":        true,
	})
	if !strings.Contains(out, path) {
		t.Fatalf("ripgrep unicode fixed_string missed match:\n%s", out)
	}
	if !strings.Contains(out, "mode=indexed") {
		t.Fatalf("ripgrep unicode fixed_string should use local index:\n%s", out)
	}
}

func TestToolRipgrepRegexUnicodeLiteralUsesIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(path, []byte("大仓库检索能力已经启用\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "大仓库检索能力",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, path) {
		t.Fatalf("ripgrep unicode regex literal missed match:\n%s", out)
	}
	if !strings.Contains(out, "mode=indexed") {
		t.Fatalf("ripgrep unicode regex literal should use local index:\n%s", out)
	}
}

func TestToolRipgrepShortFixedStringFallsBackToFullScan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "operators.txt")
	if err := os.WriteFile(path, []byte("a++\nb--\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":         dir,
		"pattern":      "++",
		"fixed_string": true,
		"stats":        true,
	})
	if !strings.Contains(out, path+":1:a++") {
		t.Fatalf("ripgrep short fixed_string fallback missed match:\n%s", out)
	}
	for _, want := range []string{"mode=full_scan", "fallback=no_required_literal"} {
		if !strings.Contains(out, want) {
			t.Fatalf("ripgrep short fixed_string should report %q:\n%s", want, out)
		}
	}
}

func TestToolRipgrepRegexEscapedLiteralUsesIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nconst value = \"Target.Name\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     `Target\.Name`,
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, path) {
		t.Fatalf("ripgrep regex escaped literal missed match:\n%s", out)
	}
	if !strings.Contains(out, "mode=indexed") {
		t.Fatalf("ripgrep regex escaped literal should use index:\n%s", out)
	}
}

func TestToolRipgrepRegexCharClassUsesLiteralPrefixCandidate(t *testing.T) {
	dir := t.TempDir()
	match := filepath.Join(dir, "match.go")
	nonMatch := filepath.Join(dir, "nonmatch.go")
	if err := os.WriteFile(match, []byte("package main\nvar _ = TargetZ\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nonMatch, []byte("package main\nvar _ = Target1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     `Target[A-Z]`,
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, match) {
		t.Fatalf("ripgrep regex char class missed matching file:\n%s", out)
	}
	if strings.Contains(out, nonMatch) {
		t.Fatalf("ripgrep regex char class returned non-matching candidate:\n%s", out)
	}
	if !strings.Contains(out, "mode=indexed") {
		t.Fatalf("ripgrep regex char class should use literal-prefix index candidates:\n%s", out)
	}
}

func TestToolRipgrepWholeWordDoesNotMatchSubstring(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nvar Target = 1\nvar TargetExtra = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "Target",
		"whole_word":  true,
		"output_mode": "content",
	})
	if !strings.Contains(out, "var Target = 1") {
		t.Fatalf("ripgrep whole_word missed exact word:\n%s", out)
	}
	if strings.Contains(out, "TargetExtra") {
		t.Fatalf("ripgrep whole_word matched substring:\n%s", out)
	}
}

func TestToolRipgrepLineRegexpMatchesWholeLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.txt")
	if err := os.WriteFile(path, []byte("status=ok\nprefix status=ok\nstatus=ok suffix\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":         dir,
		"pattern":      "status=ok",
		"fixed_string": true,
		"line_regexp":  true,
		"output_mode":  "content",
	})
	if !strings.Contains(out, path+":1:status=ok") {
		t.Fatalf("ripgrep line_regexp missed exact line:\n%s", out)
	}
	if strings.Contains(out, "prefix status=ok") || strings.Contains(out, "status=ok suffix") {
		t.Fatalf("ripgrep line_regexp matched partial line:\n%s", out)
	}
}

func TestToolGlobSupportsCommaSeparatedPatterns(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	mdFile := filepath.Join(dir, "notes.md")
	txtFile := filepath.Join(dir, "notes.txt")
	for _, path := range []string{goFile, mdFile, txtFile} {
		if err := os.WriteFile(path, []byte("content\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ToolGlob(map[string]interface{}{
		"path":    dir,
		"pattern": "*.go,*.md",
	})
	if !strings.Contains(out, goFile) || !strings.Contains(out, mdFile) {
		t.Fatalf("Glob comma patterns missing expected files:\n%s", out)
	}
	if strings.Contains(out, txtFile) {
		t.Fatalf("Glob comma patterns matched excluded file:\n%s", out)
	}
}

func TestToolGlobSupportsExcludeFilter(t *testing.T) {
	dir := t.TempDir()
	keepFile := filepath.Join(dir, "keep.go")
	excludedFile := filepath.Join(dir, "generated.go")
	for _, path := range []string{keepFile, excludedFile} {
		if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ToolGlob(map[string]interface{}{
		"path":    dir,
		"pattern": "*.go",
		"exclude": "generated.go",
	})
	if !strings.Contains(out, keepFile) {
		t.Fatalf("Glob exclude missing kept file:\n%s", out)
	}
	if strings.Contains(out, excludedFile) {
		t.Fatalf("Glob exclude matched excluded file:\n%s", out)
	}
}

func TestToolGlobSupportsExcludeGlobAlias(t *testing.T) {
	dir := t.TempDir()
	keepFile := filepath.Join(dir, "keep.go")
	excludedFile := filepath.Join(dir, "snapshot.go")
	for _, path := range []string{keepFile, excludedFile} {
		if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ToolGlob(map[string]interface{}{
		"path":         dir,
		"pattern":      "*.go",
		"exclude_glob": "snapshot.go",
	})
	if !strings.Contains(out, keepFile) {
		t.Fatalf("Glob exclude_glob missing kept file:\n%s", out)
	}
	if strings.Contains(out, excludedFile) {
		t.Fatalf("Glob exclude_glob matched excluded file:\n%s", out)
	}
}

func TestToolGlobSupportsTypeFilter(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	mdFile := filepath.Join(dir, "notes.md")
	vueFile := filepath.Join(dir, "component.vue")
	for _, path := range []string{goFile, mdFile, vueFile} {
		if err := os.WriteFile(path, []byte("content\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ToolGlob(map[string]interface{}{
		"path":    dir,
		"pattern": "**/*",
		"type":    "go,.vue",
	})
	if !strings.Contains(out, goFile) || !strings.Contains(out, vueFile) {
		t.Fatalf("Glob type filter missing expected files:\n%s", out)
	}
	if strings.Contains(out, mdFile) {
		t.Fatalf("Glob type filter matched excluded markdown file:\n%s", out)
	}
}

func TestToolGlobSupportsAdditionalTypeAliases(t *testing.T) {
	dir := t.TempDir()
	protoFile := filepath.Join(dir, "service.proto")
	kotlinFile := filepath.Join(dir, "Main.kt")
	dockerFile := filepath.Join(dir, "Dockerfile")
	makeFile := filepath.Join(dir, "Makefile")
	cmakeFile := filepath.Join(dir, "CMakeLists.txt")
	gradleFile := filepath.Join(dir, "build.gradle.kts")
	otherFile := filepath.Join(dir, "notes.txt")
	for _, path := range []string{protoFile, kotlinFile, dockerFile, makeFile, cmakeFile, gradleFile, otherFile} {
		if err := os.WriteFile(path, []byte("content\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ToolGlob(map[string]interface{}{
		"path":    dir,
		"pattern": "**/*",
		"type":    "proto,kotlin,dockerfile,make,cmake,gradle",
	})
	for _, want := range []string{protoFile, kotlinFile, dockerFile, makeFile, cmakeFile, gradleFile} {
		if !strings.Contains(out, want) {
			t.Fatalf("Glob additional type aliases missing %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, otherFile) {
		t.Fatalf("Glob additional type aliases matched unrelated file:\n%s", out)
	}
}

func TestToolGlobUsesRootGitignore(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "src", "keep.go")
	skip := filepath.Join(dir, "ignored_dir", "skip.go")
	if err := os.MkdirAll(filepath.Dir(keep), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(skip), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored_dir/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skip, []byte("package vendor\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolGlob(map[string]interface{}{
		"path":    dir,
		"pattern": "**/*.go",
	})
	if !strings.Contains(out, keep) || strings.Contains(out, skip) {
		t.Fatalf("glob should apply supported .gitignore rules:\n%s", out)
	}
}

func TestToolGlobNoIgnoreDisablesRootGitignore(t *testing.T) {
	dir := t.TempDir()
	skip := filepath.Join(dir, "ignored_dir", "skip.go")
	if err := os.MkdirAll(filepath.Dir(skip), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored_dir/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skip, []byte("package vendor\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolGlob(map[string]interface{}{
		"path":      dir,
		"pattern":   "**/*.go",
		"no_ignore": true,
	})
	if !strings.Contains(out, skip) {
		t.Fatalf("glob no_ignore should include .gitignore paths:\n%s", out)
	}
}

func TestToolGlobSkipsHiddenPathsByDefault(t *testing.T) {
	dir := t.TempDir()
	visible := filepath.Join(dir, "visible.go")
	hidden := filepath.Join(dir, ".github", "workflow.go")
	if err := os.MkdirAll(filepath.Dir(hidden), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{visible, hidden} {
		if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ToolGlob(map[string]interface{}{
		"path":    dir,
		"pattern": "**/*.go",
	})
	if !strings.Contains(out, visible) || strings.Contains(out, hidden) {
		t.Fatalf("glob should skip hidden paths by default:\n%s", out)
	}
}

func TestToolGlobIncludeHiddenFindsHiddenPaths(t *testing.T) {
	dir := t.TempDir()
	hidden := filepath.Join(dir, ".github", "workflow.go")
	if err := os.MkdirAll(filepath.Dir(hidden), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hidden, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolGlob(map[string]interface{}{
		"path":           dir,
		"pattern":        "**/*.go",
		"include_hidden": true,
	})
	if !strings.Contains(out, hidden) {
		t.Fatalf("glob include_hidden should include hidden paths:\n%s", out)
	}
}

func TestToolGlobSkipsSymlinksByDefault(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.go")
	link := filepath.Join(dir, "linked.go")
	if err := os.WriteFile(target, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable on this platform/configuration: %v", err)
	}

	out := ToolGlob(map[string]interface{}{
		"path":    dir,
		"pattern": "**/*.go",
	})
	if !strings.Contains(out, target) || strings.Contains(out, link) {
		t.Fatalf("glob should skip symlinks by default:\n%s", out)
	}
}

func TestToolGlobFollowsJunctionRootButNotNestedJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junctions are Windows-specific")
	}
	root := t.TempDir()
	project := filepath.Join(root, "project")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(project, "keep.go")
	secret := filepath.Join(outside, "secret.go")
	if err := os.WriteFile(keep, []byte("package keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte("package secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(project, "link")
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput()
	if err != nil {
		t.Skipf("junction not available: %v (%s)", err, out)
	}

	inside := ToolGlob(map[string]interface{}{
		"path":    project,
		"pattern": "**/*.go",
	})
	if !strings.Contains(inside, keep) {
		t.Fatalf("glob of the project should find keep.go:\n%s", inside)
	}
	if strings.Contains(inside, secret) || strings.Contains(inside, filepath.Join(link, "secret.go")) {
		t.Fatalf("glob of the project should not descend into a nested junction:\n%s", inside)
	}

	named := ToolGlob(map[string]interface{}{
		"path":    link,
		"pattern": "**/*.go",
	})
	if !strings.Contains(named, secret) && !strings.Contains(named, filepath.Join(link, "secret.go")) {
		t.Fatalf("glob of a junction root should find the target file:\n%s", named)
	}
}

func TestToolGlobSkipsCommonGeneratedDirsByDefault(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "src", "keep.go")
	skipTarget := filepath.Join(dir, "target", "debug", "skip.go")
	skipPycache := filepath.Join(dir, "__pycache__", "skip.go")
	skipVenv := filepath.Join(dir, ".venv", "skip.go")
	for _, path := range []string{keep, skipTarget, skipPycache, skipVenv} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ToolGlob(map[string]interface{}{
		"path":           dir,
		"pattern":        "**/*.go",
		"include_hidden": true,
	})
	if !strings.Contains(out, keep) {
		t.Fatalf("glob generated-dir skip missing kept file:\n%s", out)
	}
	for _, skipped := range []string{skipTarget, skipPycache, skipVenv} {
		if strings.Contains(out, skipped) {
			t.Fatalf("glob should skip generated dir file %s:\n%s", skipped, out)
		}
	}
}

func TestToolRipgrepFilesWithMatchesOutputMode(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.go")
	second := filepath.Join(dir, "second.go")
	other := filepath.Join(dir, "notes.md")
	for path, content := range map[string]string{
		first:  "package main\nfunc Target() {}\n",
		second: "package main\nfunc TargetAgain() {}\n",
		other:  "Target in markdown\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "Target",
		"glob":        "**/*.go",
		"output_mode": "files_with_matches",
		"max_results": float64(10),
	})
	if !strings.Contains(out, first) || !strings.Contains(out, second) {
		t.Fatalf("ripgrep files_with_matches missing expected files:\n%s", out)
	}
	if strings.Contains(out, other) || strings.Contains(out, ":2:") {
		t.Fatalf("ripgrep files_with_matches returned content or wrong file:\n%s", out)
	}
}

func TestToolRipgrepCountOutputMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("Target\nTarget\nOther\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "Target",
		"output_mode": "count",
		"max_results": float64(10),
	})
	if !strings.Contains(out, path+":2") {
		t.Fatalf("ripgrep count output = %q, want file count", out)
	}
}

func TestToolRipgrepStatsAreOptIn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc Target() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":    dir,
		"pattern": "Target",
	})
	if strings.Contains(out, "search_stats:") {
		t.Fatalf("ripgrep emitted stats without opt-in:\n%s", out)
	}
}

func TestToolRipgrepStatsIncludeIndexedSearchData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc Target() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":    dir,
		"pattern": "Target",
		"stats":   true,
	})
	for _, want := range []string{"search_stats:", "mode=indexed", "indexed_files=", "candidates=", "searched=", "candidate_ms=", "scan_ms=", "total_ms="} {
		if !strings.Contains(out, want) {
			t.Fatalf("ripgrep stats missing %q:\n%s", want, out)
		}
	}
}

func TestToolRipgrepStatsIncludeFullScanData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nvar ID = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":           dir,
		"pattern":        "ID",
		"case_sensitive": true,
		"stats":          true,
	})
	for _, want := range []string{"search_stats:", "mode=full_scan", "searched=1", "fallback=no_required_literal", "candidate_ms=", "scan_ms=", "total_ms="} {
		if !strings.Contains(out, want) {
			t.Fatalf("ripgrep full-scan stats missing %q:\n%s", want, out)
		}
	}
}

func TestToolRipgrepSingleFilePathFallsBackWithoutIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc TargetSingleFile() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":    path,
		"pattern": "TargetSingleFile",
		"stats":   true,
	})
	if !strings.Contains(out, path+":2:func TargetSingleFile() {}") {
		t.Fatalf("ripgrep single-file path missed match:\n%s", out)
	}
	for _, want := range []string{"mode=full_scan", "searched=1", "fallback=base_not_directory"} {
		if !strings.Contains(out, want) {
			t.Fatalf("ripgrep single-file path should report %q:\n%s", want, out)
		}
	}
}

func TestToolRipgrepFallsBackWhenRegexHasNoRequiredLiteral(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codes.txt")
	if err := os.WriteFile(path, []byte("ticket ABC123 is open\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":    dir,
		"pattern": `[A-Z]{3}[0-9]+`,
		"stats":   true,
	})
	if !strings.Contains(out, path+":1:ticket ABC123 is open") {
		t.Fatalf("ripgrep no-literal regex fallback missed match:\n%s", out)
	}
	for _, want := range []string{"mode=full_scan", "fallback=no_required_literal"} {
		if !strings.Contains(out, want) {
			t.Fatalf("ripgrep no-literal regex should report %q:\n%s", want, out)
		}
	}
}

func TestToolRipgrepStatsReportIndexRebuild(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.go")
	if err := os.WriteFile(first, []byte("package main\nfunc TargetOne() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":    dir,
		"pattern": "TargetOne",
	}); !strings.Contains(out, first) {
		t.Fatalf("initial indexed search missing first file:\n%s", out)
	}

	second := filepath.Join(dir, "second.go")
	if err := os.WriteFile(second, []byte("package main\nfunc TargetTwo() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	NoteSearchIndexDirty(second)
	out := ToolRipgrep(map[string]interface{}{
		"path":    dir,
		"pattern": "TargetTwo",
		"stats":   true,
	})
	if !strings.Contains(out, second) || !strings.Contains(out, "mode=indexed") || !strings.Contains(out, "rebuilt=false") {
		t.Fatalf("noted dirty file should be searched from the index without a query rebuild:\n%s", out)
	}
}

func TestToolRipgrepSkipsHiddenPathsByDefault(t *testing.T) {
	dir := t.TempDir()
	visible := filepath.Join(dir, "visible.go")
	hidden := filepath.Join(dir, ".github", "workflow.go")
	if err := os.MkdirAll(filepath.Dir(hidden), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(visible, []byte("package main\nfunc TargetHiddenDefault() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hidden, []byte("package main\nfunc TargetHiddenDefault() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "TargetHiddenDefault",
		"glob":        "**/*.go",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, visible) || strings.Contains(out, hidden) {
		t.Fatalf("ripgrep should skip hidden paths by default:\n%s", out)
	}
	if !strings.Contains(out, "mode=indexed") {
		t.Fatalf("ripgrep hidden default search should still use index:\n%s", out)
	}
}

func TestToolRipgrepIncludeHiddenUsesSeparateIndexedScope(t *testing.T) {
	dir := t.TempDir()
	visible := filepath.Join(dir, "visible.go")
	hidden := filepath.Join(dir, ".github", "workflow.go")
	if err := os.MkdirAll(filepath.Dir(hidden), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(visible, []byte("package main\nfunc TargetHiddenScope() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hidden, []byte("package main\nfunc TargetHiddenScope() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "TargetHiddenScope",
		"glob":        "**/*.go",
		"output_mode": "files_with_matches",
	}); strings.Contains(out, hidden) {
		t.Fatalf("initial no-hidden search should not include hidden path:\n%s", out)
	}

	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":           dir,
		"pattern":        "TargetHiddenScope",
		"glob":           "**/*.go",
		"output_mode":    "files_with_matches",
		"include_hidden": true,
		"stats":          true,
	})
	if !strings.Contains(out, visible) || !strings.Contains(out, hidden) {
		t.Fatalf("ripgrep include_hidden should include hidden indexed paths:\n%s", out)
	}
	if !strings.Contains(out, "mode=indexed") {
		t.Fatalf("ripgrep include_hidden should use indexed hidden scope:\n%s", out)
	}
}

func TestToolRipgrepSkipsSymlinksByDefault(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.go")
	link := filepath.Join(dir, "linked.go")
	if err := os.WriteFile(target, []byte("package main\nfunc TargetSymlinkSkip() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable on this platform/configuration: %v", err)
	}

	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "TargetSymlinkSkip",
		"glob":        "**/*.go",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, target) || strings.Contains(out, link) {
		t.Fatalf("ripgrep should skip symlinks by default:\n%s", out)
	}
	if !strings.Contains(out, "mode=indexed") {
		t.Fatalf("ripgrep symlink search should still use index:\n%s", out)
	}
}

func TestToolRipgrepIndexedSkipsFileReplacedBySymlink(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "repo")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("package main\nfunc ReplacedByLinkToken() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "outside.go")
	if err := os.WriteFile(outside, []byte("package main\nfunc ReplacedByLinkToken() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "ReplacedByLinkToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	}); !strings.Contains(out, file) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("warmup missed file:\n%s", out)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, file); err != nil {
		t.Skipf("symlink unavailable on this platform/configuration: %v", err)
	}
	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "ReplacedByLinkToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if strings.Contains(out, file) || strings.Contains(out, outside) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("indexed search followed a symlink that replaced a file:\n%s", out)
	}
}

func TestToolRipgrepSkipsCommonGeneratedDirsByDefault(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "src", "keep.go")
	skipTarget := filepath.Join(dir, "target", "debug", "skip.go")
	skipGradle := filepath.Join(dir, ".gradle", "skip.go")
	for _, path := range []string{keep, skipTarget, skipGradle} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package main\nfunc TargetGeneratedDirSkip() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":           dir,
		"pattern":        "TargetGeneratedDirSkip",
		"glob":           "**/*.go",
		"output_mode":    "files_with_matches",
		"include_hidden": true,
		"stats":          true,
	})
	if !strings.Contains(out, keep) {
		t.Fatalf("ripgrep generated-dir skip missing kept file:\n%s", out)
	}
	for _, skipped := range []string{skipTarget, skipGradle} {
		if strings.Contains(out, skipped) {
			t.Fatalf("ripgrep should skip generated dir file %s:\n%s", skipped, out)
		}
	}
	if !strings.Contains(out, "mode=indexed") {
		t.Fatalf("ripgrep generated-dir skip should still use index:\n%s", out)
	}
}

func TestToolRipgrepSkipsBinaryFiles(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(binaryPath, []byte{'T', 'a', 'r', 'g', 'e', 't', 0, 'x'}, 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":    dir,
		"pattern": "Target",
		"stats":   true,
	})
	if strings.Contains(out, binaryPath) || !strings.Contains(out, "no matches found") || !strings.Contains(out, "searched=0") {
		t.Fatalf("ripgrep should skip binary files, got:\n%s", out)
	}
}

func TestToolRipgrepRejectsInvalidOutputMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc Target() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "Target",
		"output_mode": "json",
	})
	if !strings.Contains(out, "invalid output_mode") {
		t.Fatalf("ripgrep invalid output_mode = %q, want validation error", out)
	}
}

func TestToolRipgrepFindsMatchInLongAllowedLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "long.txt")
	content := strings.Repeat("a", 1024*1024+128) + "Target\n"
	if len(content) > MaxSearchFileSize {
		t.Fatalf("test content exceeds MaxSearchFileSize")
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "Target",
		"max_results": float64(1),
	})
	if !strings.Contains(out, path+":1:") {
		t.Fatalf("ripgrep missed match in long allowed line:\n%s", out)
	}
}

func TestSearchFileLinesWithModeReturnsScannerError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "too-long.txt")
	content := "Target" + strings.Repeat("a", MaxSearchFileSize+1)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var matches []string
	err := searchFileLinesWithMode(path, regexp.MustCompile("Target"), 1, "content", 0, 0, &matches, map[string]bool{}, map[string]int{})
	if err == nil {
		t.Fatal("searchFileLinesWithMode returned nil error for oversized scanner token")
	}
}

func TestToolRipgrepContentContextAndOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("before\nTarget\nmiddle\nTarget\nend\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "Target",
		"context":     float64(1),
		"offset":      float64(1),
		"max_results": float64(3),
	})
	if strings.Contains(out, path+"-1:") {
		t.Fatalf("ripgrep offset did not skip first context line:\n%s", out)
	}
	if !strings.Contains(out, path+":2:Target") || !strings.Contains(out, path+"-3:middle") {
		t.Fatalf("ripgrep context output missing expected lines:\n%s", out)
	}
}

func TestToolRipgrepNegativeOffsetIsIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc Target() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "Target",
		"offset":      float64(-200),
		"max_results": float64(1),
	})
	if !strings.Contains(out, path+":2:") {
		t.Fatalf("ripgrep negative offset should be ignored:\n%s", out)
	}
}

func TestToolRipgrepLocalIndexIncludesNewDirtyFiles(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.go")
	if err := os.WriteFile(first, []byte("package main\nfunc TargetOne() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "TargetOne",
		"output_mode": "files_with_matches",
	}); !strings.Contains(out, first) {
		t.Fatalf("initial indexed search missing first file:\n%s", out)
	}

	second := filepath.Join(dir, "second.go")
	if err := os.WriteFile(second, []byte("package main\nfunc TargetTwo() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	NoteSearchIndexDirty(second)
	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "TargetTwo",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, "mode=indexed") {
		t.Fatalf("noted new file should stay on the index:\n%s", out)
	}
	if !strings.Contains(out, second) {
		t.Fatalf("indexed search missed new dirty file:\n%s", out)
	}
}

func TestToolRipgrepLocalIndexIncludesModifiedDirtyFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc InitialSymbol() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "InitialSymbol",
		"output_mode": "files_with_matches",
	}); !strings.Contains(out, path) {
		t.Fatalf("initial indexed search missing file:\n%s", out)
	}

	if err := os.WriteFile(path, []byte("package main\nfunc AddedAfterIndexBuild() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	NoteSearchIndexDirty(path)
	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "AddedAfterIndexBuild",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, path) {
		t.Fatalf("indexed search missed modified dirty file:\n%s", out)
	}
	if !strings.Contains(out, "mode=indexed") {
		t.Fatalf("modified dirty file search should still use index overlay:\n%s", out)
	}
}

func TestToolRipgrepLocalIndexIncludesModifiedFileWithOlderModTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc InitialTimestampSymbol() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "InitialTimestampSymbol",
		"output_mode": "files_with_matches",
	}); !strings.Contains(out, path) {
		t.Fatalf("initial indexed search missing file:\n%s", out)
	}

	oldTime := time.Now().Add(-time.Hour)
	if err := os.WriteFile(path, []byte("package main\nfunc AddedDespiteOlderTimestamp() {}\nvar Padding = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	NoteSearchIndexDirty(path)

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "AddedDespiteOlderTimestamp",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, path) {
		t.Fatalf("indexed search missed modified file with older mtime:\n%s", out)
	}
	if !strings.Contains(out, "mode=indexed") {
		t.Fatalf("older-mtime dirty file search should still use index overlay:\n%s", out)
	}
}

func TestToolRipgrepLocalIndexDetectsSameMetadataContentChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	initial := []byte("package main\nfunc InitialSameMeta() {}\n")
	updated := []byte("package main\nfunc UpdatedSameMeta() {}\n")
	if len(initial) != len(updated) {
		t.Fatalf("test fixture lengths differ: initial=%d updated=%d", len(initial), len(updated))
	}
	if err := os.WriteFile(path, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	futureTime := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, futureTime, futureTime); err != nil {
		t.Fatal(err)
	}
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "InitialSameMeta",
		"output_mode": "files_with_matches",
	}); !strings.Contains(out, path) {
		t.Fatalf("initial indexed search missing file:\n%s", out)
	}

	if err := os.WriteFile(path, updated, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, futureTime, futureTime); err != nil {
		t.Fatal(err)
	}
	NoteSearchIndexDirty(path)
	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "UpdatedSameMeta",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, path) {
		t.Fatalf("indexed search missed same-metadata content change:\n%s", out)
	}
	if !strings.Contains(out, "mode=indexed") {
		t.Fatalf("same-metadata content change should still use index overlay:\n%s", out)
	}
}

func TestToolRipgrepLocalIndexSkipsDeletedIndexedFiles(t *testing.T) {
	dir := t.TempDir()
	deleted := filepath.Join(dir, "deleted.go")
	kept := filepath.Join(dir, "kept.go")
	for _, path := range []string{deleted, kept} {
		if err := os.WriteFile(path, []byte("package main\nfunc TargetDeletedOverlay() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "TargetDeletedOverlay",
		"output_mode": "files_with_matches",
	}); !strings.Contains(out, deleted) || !strings.Contains(out, kept) {
		t.Fatalf("initial indexed search missing files:\n%s", out)
	}

	if err := os.Remove(deleted); err != nil {
		t.Fatal(err)
	}
	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "TargetDeletedOverlay",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, kept) {
		t.Fatalf("indexed search missing kept file after delete:\n%s", out)
	}
	if strings.Contains(out, deleted) {
		t.Fatalf("indexed search returned deleted file:\n%s", out)
	}
	if !strings.Contains(out, "mode=indexed") {
		t.Fatalf("deleted-file search should still use local index:\n%s", out)
	}
}

func TestToolRipgrepRebuildsStaleIndexWhenDirtyRatioIsHigh(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.go")
	if err := os.WriteFile(first, []byte("package main\nfunc TargetOne() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "TargetOne",
		"output_mode": "files_with_matches",
	})
	if !strings.Contains(out, first) {
		t.Fatalf("initial indexed search missing first file:\n%s", out)
	}

	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}

	second := filepath.Join(dir, "second.go")
	if err := os.WriteFile(second, []byte("package main\nfunc TargetTwo() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	NoteSearchIndexDirty(second)
	out = ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "TargetTwo",
		"output_mode": "files_with_matches",
	})
	if !strings.Contains(out, second) {
		t.Fatalf("rebuilt indexed search missed new file:\n%s", out)
	}
	if !reconcilePublishedCorpus(root, "", false) {
		t.Fatal("reconcile did not publish the new file")
	}

	searchIndexCache.Lock()
	idx := searchIndexCache.byRoot[searchIndexCacheKey(filepath.Clean(root), "", "", "", false)]
	searchIndexCache.Unlock()
	if idx == nil || !idx.fileSet[second] {
		t.Fatalf("expected rebuilt index to include %s", second)
	}
}

func TestToolRipgrepLocalIndexPreservesCaseSensitiveExactSearch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc TargetCase() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":           dir,
		"pattern":        "TargetCase",
		"case_sensitive": true,
	})
	if !strings.Contains(out, path+":2:") {
		t.Fatalf("case-sensitive indexed search missed exact match:\n%s", out)
	}
}

func TestToolRipgrepLocalIndexCaseSensitiveFiltersCaseMismatchedCandidates(t *testing.T) {
	dir := t.TempDir()
	match := filepath.Join(dir, "match.go")
	mismatch := filepath.Join(dir, "mismatch.go")
	if err := os.WriteFile(match, []byte("package main\nfunc TargetCase() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mismatch, []byte("package main\nfunc targetcase() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":           dir,
		"pattern":        "TargetCase",
		"case_sensitive": true,
		"output_mode":    "files_with_matches",
		"stats":          true,
	})
	if !strings.Contains(out, match) {
		t.Fatalf("case-sensitive indexed search missed exact-case file:\n%s", out)
	}
	if strings.Contains(out, mismatch) {
		t.Fatalf("case-sensitive indexed search returned case-mismatched candidate:\n%s", out)
	}
	if !strings.Contains(out, "mode=indexed") {
		t.Fatalf("case-sensitive search should use local index candidates:\n%s", out)
	}
}

func TestToolRipgrepLocalIndexSupportsCaseInsensitiveCandidates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc TargetCase() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":    dir,
		"pattern": "targetcase",
		"stats":   true,
	})
	if !strings.Contains(out, path+":2:") {
		t.Fatalf("case-insensitive indexed search missed mixed-case match:\n%s", out)
	}
	if !strings.Contains(out, "mode=indexed") {
		t.Fatalf("case-insensitive search should use local index:\n%s", out)
	}
}

func TestToolRipgrepLocalIndexDoesNotOverConstrainAlternation(t *testing.T) {
	dir := t.TempDir()
	alpha := filepath.Join(dir, "alpha.go")
	beta := filepath.Join(dir, "beta.go")
	if err := os.WriteFile(alpha, []byte("package main\nfunc AlphaOnly() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(beta, []byte("package main\nfunc BetaOnly() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "AlphaOnly|BetaOnly",
		"output_mode": "files_with_matches",
	})
	if !strings.Contains(out, alpha) || !strings.Contains(out, beta) {
		t.Fatalf("indexed search over-constrained alternation:\n%s", out)
	}
}

func TestTrigramQueryAlternationKeepsEachBranch(t *testing.T) {
	query := trigramQueryForPattern("AlphaOne|AlphaTwo", false)
	if !query.selective() || len(query.clauses) != 2 {
		t.Fatalf("alternation query = %+v, want 2 clauses", query)
	}
	joined := trigramQueryForPattern("prefix(AlphaOne|AlphaTwo)suffix", false)
	if !joined.selective() || len(joined.clauses) != 2 {
		t.Fatalf("distributed alternation query = %+v, want 2 clauses", joined)
	}
	for _, clause := range joined.clauses {
		if !containsTrigram(clause, "pre") || !containsTrigram(clause, "suf") {
			t.Fatalf("branch lost a required trigram: %#v", clause)
		}
	}
	if trigramQueryForPattern("ab|AlphaOnly", false).selective() {
		t.Fatal("a short alternative must make the OR match every file")
	}
	if trigramQueryForPattern(`[A-Z]{3}[0-9]+`, false).selective() {
		t.Fatal("a character class must not filter the index")
	}
	if !trigramQueryForPattern(`Target[A-Z]`, false).selective() {
		t.Fatal("a literal next to a character class must still filter")
	}
	folded := trigramQueryForPattern("Target|target", false)
	if !folded.selective() || len(folded.clauses) != 1 {
		t.Fatalf("case-folded alternation query = %+v, want 1 clause", folded)
	}
	wide := trigramQueryForPattern("a*b*Target", false)
	if !wide.selective() || len(wide.clauses) != 1 || !containsTrigram(wide.clauses[0], "tar") {
		t.Fatalf("leading wildcards dropped Target: %+v", wide)
	}
	joinedLiteral := trigramQueryForPattern("a{3}", false)
	if !joinedLiteral.selective() || !containsTrigram(joinedLiteral.clauses[0], "aaa") {
		t.Fatalf("a{3} dropped the joined literal: %+v", joinedLiteral)
	}
	if trigramQueryForPattern("a{2}", false).selective() {
		t.Fatal("a{2} has no trigram and must not filter")
	}
	if trigramQueryForPattern("a+b+a", false).selective() {
		t.Fatal("a+b+a can match abba and must not require aba")
	}
	repeated := trigramQueryForPattern("(ab){2}", false)
	if !repeated.selective() || !containsTrigram(repeated.clauses[0], "aba") {
		t.Fatalf("(ab){2} dropped the boundary trigram: %+v", repeated)
	}
	plus := trigramQueryForPattern("abc+", false)
	if !plus.selective() || !containsTrigram(plus.clauses[0], "abc") {
		t.Fatalf("abc+ dropped the required prefix: %+v", plus)
	}
	spanning := trigramQueryForPattern("abc+def", false)
	if !spanning.selective() || len(spanning.clauses) != 1 {
		t.Fatalf("abc+def query = %+v", spanning)
	}
	for _, trigram := range []string{"abc", "cde", "def"} {
		if !containsTrigram(spanning.clauses[0], trigram) {
			t.Fatalf("abc+def missing %q: %+v", trigram, spanning)
		}
	}
	if !trigramQueryForPattern("foo{2}", false).selective() || !containsTrigram(trigramQueryForPattern("foo{2}", false).clauses[0], "foo") {
		t.Fatal("foo{2} must require foo")
	}
}

func TestTrigramQueryAgreesWithRegexp(t *testing.T) {
	texts := []string{
		"abc", "abcc", "abccc", "abcdef", "abccdef", "abbc", "abba", "aba",
		"aaa", "AAA", "aa", "aaaa", "abab", "ababab", "ababcd", "xxabab",
		"fooo", "foo", "Target", "target", "TARGET", "TargetZ",
		"sood", "SOOD", "\u017Food", "\u017FOOD",
		"AlphaOne", "prefixAlphaOnesuffix", "prefixAlphaTwosuffix",
		"abxy", "xyab", "xyabab", "ticket ABC123 is open",
		"Target.Name", "\u00c9cole", "\u00e9cole", "Ecole",
	}
	patterns := []string{
		"abc+", "abc+def", "a{3}", "a{2}", "(ab){2}", "(ab){2,}",
		"foo{2}", "a+b+a", "ab+c", "(ab)+xy", "xy(ab)+",
		"a*b*Target", "Target|target", `[A-Z]{3}[0-9]+`,
		"Target[A-Z]", "sood", "\u017Food", "ID",
		`Target\.Name`, `\bTarget\b`,
		"prefix(AlphaOne|AlphaTwo)suffix", "Alpha(Beta)?",
		"foo{2}", "(?:ab)cd",
	}
	for _, pattern := range patterns {
		query := trigramQueryForPattern(pattern, false)
		folded, foldErr := regexp.Compile("(?i:" + pattern + ")")
		exact, exactErr := regexp.Compile(pattern)
		for _, text := range texts {
			foldedHit := foldErr == nil && folded.MatchString(text)
			exactHit := exactErr == nil && exact.MatchString(text)
			if !foldedHit && !exactHit {
				continue
			}
			if !trigramQueryMatchesText(query, text) {
				t.Fatalf("pattern %q dropped %q query=%+v", pattern, text, query)
			}
		}
	}
}

func trigramQueryMatchesText(query trigramQuery, text string) bool {
	if !query.selective() {
		return true
	}
	have := contentTrigrams(text)
	for _, clause := range query.clauses {
		ok := true
		for _, trigram := range clause {
			if !have[trigram] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func TestContentTrigramsFollowRegexpCaseFold(t *testing.T) {
	longS := contentTrigrams("\u017Food")
	ascii := contentTrigrams("sood")
	if len(longS) == 0 || len(longS) != len(ascii) {
		t.Fatalf("long s trigrams = %#v, ascii = %#v", longS, ascii)
	}
	for trigram := range longS {
		if !ascii[trigram] {
			t.Fatalf("long s trigram %q missing from ascii fold %#v", trigram, ascii)
		}
	}
	upper := contentTrigrams("\u00c9cole")
	lower := contentTrigrams("\u00e9cole")
	plain := contentTrigrams("Ecole")
	if len(upper) == 0 || len(upper) != len(lower) {
		t.Fatalf("accent fold %#v vs %#v", upper, lower)
	}
	for trigram := range upper {
		if !lower[trigram] {
			t.Fatalf("accent trigram %q not shared: %#v vs %#v", trigram, upper, lower)
		}
	}
	if len(upper) == len(plain) {
		same := true
		for trigram := range upper {
			if !plain[trigram] {
				same = false
				break
			}
		}
		if same {
			t.Fatal("accented letters collapsed into ASCII e")
		}
	}
	if contentTrigrams("\u0130ab")["iab"] {
		t.Fatal("dotted capital I must not fold to i; regexp (?i) does not match them")
	}
}

func containsTrigram(clause []string, trigram string) bool {
	for _, item := range clause {
		if item == trigram {
			return true
		}
	}
	return false
}

func TestToolRipgrepAlternationUsesIndex(t *testing.T) {
	dir := t.TempDir()
	alpha := filepath.Join(dir, "alpha.go")
	beta := filepath.Join(dir, "beta.go")
	prefix := filepath.Join(dir, "prefix.go")
	if err := os.WriteFile(alpha, []byte("package main\nfunc AlphaOne() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(beta, []byte("package main\nfunc AlphaTwo() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prefix, []byte("package main\nfunc Alpha() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "AlphaOne|AlphaTwo",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, alpha) || !strings.Contains(out, beta) || strings.Contains(out, prefix) {
		t.Fatalf("alternation index candidates:\n%s", out)
	}
	for _, want := range []string{"mode=indexed", "indexed_files=3", "candidates=2", "searched=2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("alternation search missing %q:\n%s", want, out)
		}
	}
}

func TestToolRipgrepUnfilterableAlternationFullScansAndWarmsIndex(t *testing.T) {
	dir := t.TempDir()
	short := filepath.Join(dir, "short.go")
	long := filepath.Join(dir, "long.go")
	if err := os.WriteFile(short, []byte("package main\nvar ab = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(long, []byte("package main\nfunc TargetWarm() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":    dir,
		"pattern": "ab|TargetWarm",
		"stats":   true,
	})
	if !strings.Contains(out, short) || !strings.Contains(out, long) {
		t.Fatalf("unfilterable alternation missed a branch:\n%s", out)
	}
	for _, want := range []string{"mode=full_scan", "fallback=no_required_literal", "searched=2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("unfilterable alternation missing %q:\n%s", want, out)
		}
	}

	warmed := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "TargetWarm",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(warmed, long) || strings.Contains(warmed, short) || !strings.Contains(warmed, "mode=indexed") {
		t.Fatalf("unfilterable search should still warm the corpus:\n%s", warmed)
	}
}

func TestToolRipgrepJoinedLiteralUsesIndex(t *testing.T) {
	dir := t.TempDir()
	hit := filepath.Join(dir, "hit.go")
	repeat := filepath.Join(dir, "repeat.go")
	longS := filepath.Join(dir, "longs.go")
	miss := filepath.Join(dir, "miss.go")
	if err := os.WriteFile(hit, []byte("package hit\nfunc f() { return abcdef }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repeat, []byte("package repeat\nvar token = abab\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(longS, []byte("package longs\nvar token = soodle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(miss, []byte("package miss\nvar token = abba\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "abc+def",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, hit) || strings.Contains(out, miss) || strings.Contains(out, repeat) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("abc+def index candidates:\n%s", out)
	}

	repeated := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "(ab){2}",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(repeated, repeat) || strings.Contains(repeated, miss) || !strings.Contains(repeated, "mode=indexed") {
		t.Fatalf("(ab){2} index candidates:\n%s", repeated)
	}

	folded := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "\u017Foodle",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(folded, longS) || strings.Contains(folded, miss) || !strings.Contains(folded, "mode=indexed") {
		t.Fatalf("long s should match an ASCII file through the index:\n%s", folded)
	}
}

func TestToolRipgrepSingleFileSearchWarmsDirectoryIndex(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "render.cpp")
	other := filepath.Join(dir, "other.cpp")
	if err := os.WriteFile(file, []byte("void draw() { GameOverToken(); }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("void other() { return; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":    file,
		"pattern": "GameOverToken",
		"stats":   true,
	})
	if !strings.Contains(out, "GameOverToken") {
		t.Fatalf("single-file search missed match:\n%s", out)
	}
	for _, want := range []string{"mode=full_scan", "searched=1", "fallback=base_not_directory"} {
		if !strings.Contains(out, want) {
			t.Fatalf("single-file search missing %q:\n%s", want, out)
		}
	}

	warmed := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "GameOverToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(warmed, file) || strings.Contains(warmed, other) || !strings.Contains(warmed, "mode=indexed") {
		t.Fatalf("single-file search should warm the repository index:\n%s", warmed)
	}
}

func TestToolRipgrepFilePathUsesPublishedCorpus(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	hit := filepath.Join(dir, "render.cpp")
	miss := filepath.Join(dir, "audio.cpp")
	if err := os.WriteFile(hit, []byte("void draw() { GameOverToken(); }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(miss, []byte("void play() { return; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	warmed := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "GameOverToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(warmed, hit) || strings.Contains(warmed, miss) || !strings.Contains(warmed, "mode=indexed") {
		t.Fatalf("directory corpus:\n%s", warmed)
	}

	hitOut := ToolRipgrep(map[string]interface{}{
		"path":    hit,
		"pattern": "GameOverToken",
		"stats":   true,
	})
	if !strings.Contains(hitOut, "GameOverToken") || !strings.Contains(hitOut, "mode=indexed") || !strings.Contains(hitOut, "searched=1") {
		t.Fatalf("file hit should use the published corpus:\n%s", hitOut)
	}

	missOut := ToolRipgrep(map[string]interface{}{
		"path":    miss,
		"pattern": "GameOverToken",
		"stats":   true,
	})
	if strings.Contains(missOut, "play()") || !strings.Contains(missOut, "mode=indexed") || !strings.Contains(missOut, "searched=0") {
		t.Fatalf("clean file miss should not be read:\n%s", missOut)
	}
	if runtime.GOOS == "windows" {
		foldedMiss := filepath.Join(dir, "AUDIO.CPP")
		foldedOut := ToolRipgrep(map[string]interface{}{
			"path":    foldedMiss,
			"pattern": "GameOverToken",
			"stats":   true,
		})
		if !strings.Contains(foldedOut, "mode=indexed") || !strings.Contains(foldedOut, "searched=0") || strings.Contains(foldedOut, "fallback=") {
			t.Fatalf("case-folded path should use the corpus without reading:\n%s", foldedOut)
		}
	}

	if err := os.WriteFile(miss, []byte("void play() { GameOverToken(); }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := ToolRipgrep(map[string]interface{}{
		"path":    miss,
		"pattern": "GameOverToken",
		"stats":   true,
	})
	if !strings.Contains(stale, "GameOverToken") || !strings.Contains(stale, "mode=indexed") || !strings.Contains(stale, "searched=1") {
		t.Fatalf("changed file metadata should be read:\n%s", stale)
	}

	absent := ToolRipgrep(map[string]interface{}{
		"path":    hit,
		"pattern": "AbsentNoteToken",
		"stats":   true,
	})
	if !strings.Contains(absent, "mode=indexed") || !strings.Contains(absent, "searched=0") {
		t.Fatalf("clean trigram miss should skip the read:\n%s", absent)
	}
	notePath := hit
	if runtime.GOOS == "windows" {
		notePath = filepath.Join(dir, "RENDER.CPP")
	}
	NoteSearchIndexDirty(notePath)
	noted := ToolRipgrep(map[string]interface{}{
		"path":    hit,
		"pattern": "AbsentNoteToken",
		"stats":   true,
	})
	if strings.Contains(noted, "void draw()") || !strings.Contains(noted, "mode=indexed") || !strings.Contains(noted, "searched=1") {
		t.Fatalf("dirty file should be read even when the corpus trigrams miss:\n%s", noted)
	}

	extra := filepath.Join(dir, "extra.cpp")
	if err := os.WriteFile(extra, []byte("void extra() { GameOverToken(); }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unknown := ToolRipgrep(map[string]interface{}{
		"path":    extra,
		"pattern": "GameOverToken",
		"stats":   true,
	})
	if !strings.Contains(unknown, "GameOverToken") || !strings.Contains(unknown, "fallback=base_not_directory") {
		t.Fatalf("file missing from the corpus should still be read:\n%s", unknown)
	}

	short := ToolRipgrep(map[string]interface{}{
		"path":    hit,
		"pattern": "ID",
		"stats":   true,
	})
	if !strings.Contains(short, "fallback=base_not_directory") {
		t.Fatalf("unfilterable file pattern should read the file:\n%s", short)
	}

	globOut := ToolRipgrep(map[string]interface{}{
		"path":    hit,
		"pattern": "GameOverToken",
		"glob":    "*.h",
		"stats":   true,
	})
	if strings.Contains(globOut, "GameOverToken") || !strings.Contains(globOut, "mode=indexed") || !strings.Contains(globOut, "searched=0") {
		t.Fatalf("basename glob miss should stay indexed and unread:\n%s", globOut)
	}
}

func TestTrigramCandidatesIncludeChecksOnePosting(t *testing.T) {
	texts := []string{
		"void draw() { GameOverToken(); }\n",
		"void play() { return; }\n",
		"other GameOverToken here\n",
	}
	idx := &localSearchIndex{
		files:    make([]string, len(texts)),
		postings: map[string][]int{},
	}
	for id, text := range texts {
		idx.files[id] = "file-" + string(rune('a'+id))
		for tri := range contentTrigrams(text) {
			idx.postings[tri] = append(idx.postings[tri], id)
		}
	}
	for _, pattern := range []string{"GameOverToken", "GameOverToken|return", "AbsentNoteToken", "draw"} {
		query := trigramQueryForPattern(pattern, false)
		if !query.selective() {
			t.Fatalf("%s should be selective", pattern)
		}
		ids := candidateIDsForTrigramQuery(idx, query)
		have := make(map[int]bool, len(ids))
		for _, id := range ids {
			have[id] = true
		}
		for id := range idx.files {
			if got := trigramCandidatesInclude(idx, query, id); got != have[id] {
				t.Fatalf("pattern %s id %d include=%v candidates=%v", pattern, id, got, ids)
			}
		}
	}
	live := trigramQuery{clauses: [][]string{{}}}
	if !trigramCandidatesInclude(idx, live, 0) || !trigramCandidatesInclude(idx, live, 2) {
		t.Fatal("empty clause should match a live id")
	}
	idx.files[1] = ""
	if trigramCandidatesInclude(idx, live, 1) {
		t.Fatal("empty clause should not match a tombstone")
	}
	if trigramCandidatesInclude(idx, trigramQuery{}, 0) {
		t.Fatal("a query with no clauses matches nothing")
	}
}

func TestFileIDInIndexMapAgreesWithSlice(t *testing.T) {
	dir, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Clean(dir)
	first := filepath.Join(dir, "a.go")
	second := filepath.Join(dir, "b.go")
	if err := os.WriteFile(first, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("package b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, ok := buildLocalSearchIndex(dir, "", "", "", false)
	if !ok || idx == nil || idx.fileIDs == nil {
		t.Fatal("build should publish a file id map")
	}
	if _, found := fileIDInIndex(idx, filepath.Join(dir, "missing.go")); found {
		t.Fatal("a file the corpus does not list should miss without a slice scan")
	}
	ids := map[string]int{}
	for _, path := range []string{first, second} {
		id, found := fileIDInIndex(idx, path)
		if !found || id < 0 || id >= len(idx.files) || idx.files[id] != path {
			t.Fatalf("path %s id %d found %v", path, id, found)
		}
		if idx.fileIDs[searchPathKey(path)] != id {
			t.Fatalf("map for %s = %d, want %d", path, idx.fileIDs[searchPathKey(path)], id)
		}
		ids[path] = id
	}
	if _, found := ids[idx.files[0]]; !found || idx.files[0] == "" {
		t.Fatal("file id 0 should be one of the built files")
	}
	idx.fileIDs[searchPathKey(first)] = ids[second]
	got, found := fileIDInIndex(idx, first)
	if !found || got != ids[first] {
		t.Fatalf("stale map id %d found %v, want %d", got, found, ids[first])
	}
	if runtime.GOOS == "windows" {
		folded := filepath.Join(dir, strings.ToUpper(filepath.Base(second)))
		got, found = fileIDInIndex(idx, folded)
		if !found || got != ids[second] {
			t.Fatalf("folded path %s id %d found %v, want %d", folded, got, found, ids[second])
		}
	}
}

func TestSearchIndexPublishedCorpusForFileSkipsCorpusThatOmitsFile(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	src := filepath.Join(repo, "src")
	file := filepath.Join(src, "keep.go")
	now := time.Now()
	corpus := func(root string, files ...string) *localSearchIndex {
		set := make(map[string]bool, len(files))
		for _, path := range files {
			set[path] = true
		}
		return &localSearchIndex{
			root:         root,
			complete:     true,
			kind:         searchIndexCorpusKind,
			builtAt:      now,
			reconciledAt: now,
			usedAt:       now.Add(24 * time.Hour),
			files:        append([]string(nil), files...),
			fileSet:      set,
		}
	}
	keyLong := "test-published-corpus-long"
	keyShort := "test-published-corpus-short"
	longerOmits := corpus(src, filepath.Join(src, "other.go"))
	shorterLists := corpus(repo, filepath.Join(repo, "first.go"), file)
	searchIndexCache.Lock()
	searchIndexCache.byRoot[keyLong] = longerOmits
	searchIndexCache.byRoot[keyShort] = shorterLists
	searchIndexCache.Unlock()
	t.Cleanup(func() {
		searchIndexCache.Lock()
		delete(searchIndexCache.byRoot, keyLong)
		delete(searchIndexCache.byRoot, keyShort)
		searchIndexCache.Unlock()
	})

	if got, id := publishedCorpusForFile(file, false, ""); got != shorterLists || id != 1 {
		t.Fatalf("got root %q id %d, want the ancestor that lists the file at id 1", corpusRoot(got), id)
	}
	if runtime.GOOS == "windows" {
		folded := filepath.Join(src, "KEEP.GO")
		if got, id := publishedCorpusForFile(folded, false, ""); got != shorterLists || id != 1 {
			t.Fatalf("case-folded path got root %q id %d", corpusRoot(got), id)
		}
	}
	longerLists := corpus(src, filepath.Join(src, "other.go"), file)
	searchIndexCache.Lock()
	searchIndexCache.byRoot[keyLong] = longerLists
	searchIndexCache.Unlock()
	if got, id := publishedCorpusForFile(file, false, ""); got != longerLists || id != 1 {
		t.Fatalf("got root %q id %d, want the longest ancestor that lists the file", corpusRoot(got), id)
	}

	plain := corpus(repo, filepath.Join(repo, "first.go"), file)
	withIgnore := corpus(repo, filepath.Join(repo, "first.go"), file)
	withIgnore.exclude = "\x00*.log"
	searchIndexCache.Lock()
	searchIndexCache.byRoot[keyLong] = plain
	searchIndexCache.byRoot[keyShort] = withIgnore
	searchIndexCache.Unlock()
	if got, id := publishedCorpusForFile(file, false, "\x00*.log"); got != withIgnore || id != 1 {
		t.Fatalf("got exclude %q id %d, want the corpus whose exclude key matches", corpusExclude(got), id)
	}
	if got, id := publishedCorpusForFile(file, false, ""); got != plain || id != 1 {
		t.Fatalf("got exclude %q id %d, want the corpus with an empty exclude key", corpusExclude(got), id)
	}

	older := corpus(repo, filepath.Join(repo, "first.go"), file)
	newerCorpus := corpus(repo, filepath.Join(repo, "first.go"), file)
	older.exclude = "other"
	newerCorpus.exclude = "other"
	older.reconciledAt = now.Add(-time.Minute)
	newerCorpus.reconciledAt = now
	searchIndexCache.Lock()
	searchIndexCache.byRoot[keyLong] = older
	searchIndexCache.byRoot[keyShort] = newerCorpus
	searchIndexCache.Unlock()
	if got, id := publishedCorpusForFile(file, false, ""); got != newerCorpus || id != 1 {
		t.Fatalf("got reconciled %s id %d, want the newer corpus", corpusReconciled(got), id)
	}

	olderMatch := corpus(repo, filepath.Join(repo, "first.go"), file)
	newerOther := corpus(repo, filepath.Join(repo, "first.go"), file)
	olderMatch.reconciledAt = now.Add(-time.Minute)
	newerOther.exclude = "other"
	newerOther.reconciledAt = now
	searchIndexCache.Lock()
	searchIndexCache.byRoot[keyLong] = newerOther
	searchIndexCache.byRoot[keyShort] = olderMatch
	searchIndexCache.Unlock()
	if got, id := publishedCorpusForFile(file, false, ""); got != olderMatch || id != 1 {
		t.Fatalf("got exclude %q reconciled %s, want the matching exclude over a newer twin", corpusExclude(got), corpusReconciled(got))
	}

	newerMatch := corpus(repo, filepath.Join(repo, "first.go"), file)
	newerMatch.reconciledAt = now
	olderMatch = corpus(repo, filepath.Join(repo, "first.go"), file)
	olderMatch.reconciledAt = now.Add(-time.Minute)
	searchIndexCache.Lock()
	searchIndexCache.byRoot[keyLong] = olderMatch
	searchIndexCache.byRoot[keyShort] = newerMatch
	searchIndexCache.Unlock()
	if got, id := publishedCorpusForFile(file, false, ""); got != newerMatch || id != 1 {
		t.Fatalf("got reconciled %s id %d, want the newer corpus when both excludes match", corpusReconciled(got), id)
	}

	unclean := corpus(repo+string(filepath.Separator), filepath.Join(repo, "first.go"), file)
	unclean.exclude = "other"
	unclean.reconciledAt = now.Add(-time.Minute)
	searchIndexCache.Lock()
	searchIndexCache.byRoot[keyLong] = unclean
	searchIndexCache.byRoot[keyShort] = newerCorpus
	searchIndexCache.Unlock()
	if got, id := publishedCorpusForFile(file, false, ""); got != newerCorpus || id != 1 {
		t.Fatalf("got root %q reconciled %s, want cleaned length so a trailing separator is not a longer root", corpusRoot(got), corpusReconciled(got))
	}

	searchIndexCache.Lock()
	searchIndexCache.byRoot[keyLong] = longerOmits
	searchIndexCache.byRoot[keyShort] = corpus(repo, filepath.Join(repo, "root.go"))
	searchIndexCache.Unlock()
	if got, id := publishedCorpusForFile(file, false, ""); got != nil || id != 0 {
		t.Fatalf("got root %q id %d, want no corpus when none lists the file", corpusRoot(got), id)
	}
}

func corpusRoot(idx *localSearchIndex) string {
	if idx == nil {
		return ""
	}
	return idx.root
}

func corpusExclude(idx *localSearchIndex) string {
	if idx == nil {
		return ""
	}
	return idx.exclude
}

func corpusReconciled(idx *localSearchIndex) string {
	if idx == nil {
		return ""
	}
	return idx.reconciledAt.Format(time.RFC3339Nano)
}

func TestFileWarmSkipsPublishedCorpus(t *testing.T) {
	dir, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Clean(dir)
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "a.go")
	if err := os.WriteFile(file, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	warmDir := fileWarmDirectory(file)
	if warmDir != searchPathKey(dir) {
		t.Fatalf("warm dir %q, want %q", warmDir, searchPathKey(dir))
	}
	exclude := resolveSearchExcludePattern(nil, dir)
	if exclude == "" {
		t.Fatal("gitignore should be part of the warm exclude")
	}
	storeFileWarm(warmDir, false, dir, exclude)
	cacheKey := searchIndexCorpusKey(dir, exclude, false)
	t.Cleanup(func() {
		waitFileWarmDiscovery(t, warmDir, false)
		forgetSearchBuildSlot(cacheKey)
		searchIndexCache.Lock()
		delete(searchIndexCache.byRoot, cacheKey)
		searchIndexCache.Unlock()
		fileWarmMemos.Lock()
		delete(fileWarmMemos.byDir, fileWarmKey{dir: warmDir, hidden: false})
		delete(fileWarmMemos.byRoot, fileWarmKey{dir: searchPathKey(dir), hidden: false})
		delete(fileWarmMemos.busy, fileWarmKey{dir: warmDir, hidden: false})
		fileWarmMemos.Unlock()
	})

	markSearchBuildFailed(cacheKey, false)
	if warmSearchIndexForPath(file, false) {
		t.Fatal("failed build still cooling should not walk or reread gitignore")
	}
	forgetSearchBuildSlot(cacheKey)

	if !tryStartSearchBuild(cacheKey) {
		t.Fatal("build slot")
	}
	if warmSearchIndexForPath(file, false) {
		t.Fatal("in-flight build should not walk or reread gitignore")
	}
	finishSearchBuild(cacheKey, false, false)

	if !warmSearchIndexForPath(file, false) {
		t.Fatal("unpublished memo should still discover the corpus")
	}
	if warmSearchIndexForPath(file, false) {
		t.Fatal("discovery already running should not start another walk")
	}
	waitFileWarmDiscovery(t, warmDir, false)

	idx, ok := buildLocalSearchIndex(dir, "", exclude, "", false)
	if !ok || idx == nil {
		t.Fatal("build")
	}
	publishSearchIndex(cacheKey, idx, searchIndexCorpusKind)
	if warmSearchIndexForPath(file, false) {
		t.Fatal("published memo should not walk or reread gitignore")
	}
	if runtime.GOOS == "windows" {
		folded := filepath.Join(strings.ToUpper(dir), "a.go")
		if fileWarmDirectory(folded) != warmDir {
			t.Fatalf("folded warm dir %q, want %q", fileWarmDirectory(folded), warmDir)
		}
		if warmSearchIndexForPath(folded, false) {
			t.Fatal("case-folded directory should reuse the warm memo")
		}
	}

	fileWarmMemos.Lock()
	memo := fileWarmMemos.byDir[fileWarmKey{dir: warmDir, hidden: false}]
	memo.at = time.Now().Add(-searchIndexCacheTTL - time.Second)
	fileWarmMemos.byDir[fileWarmKey{dir: warmDir, hidden: false}] = memo
	fileWarmMemos.Unlock()
	if !warmSearchIndexForPath(file, false) {
		t.Fatal("expired memo should discover the corpus again")
	}
	waitFileWarmDiscovery(t, warmDir, false)
}

func TestFileWarmReusesRootExcludeAcrossDirectories(t *testing.T) {
	dir, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Clean(dir)
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	ignorePath := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(ignorePath, []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	subA := filepath.Join(dir, "a")
	subB := filepath.Join(dir, "b")
	if err := os.Mkdir(subA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(subB, 0o755); err != nil {
		t.Fatal(err)
	}
	fileA := filepath.Join(subA, "a.go")
	fileB := filepath.Join(subB, "b.go")
	if err := os.WriteFile(fileA, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte("package b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	original := resolveSearchExcludePattern(nil, dir)
	if original == "" {
		t.Fatal("gitignore should be part of the warm exclude")
	}
	warmA := fileWarmDirectory(fileA)
	warmB := fileWarmDirectory(fileB)
	cacheKey := searchIndexCorpusKey(dir, original, false)
	t.Cleanup(func() {
		waitFileWarmDiscovery(t, warmA, false)
		waitFileWarmDiscovery(t, warmB, false)
		forgetSearchBuildSlot(cacheKey)
		searchIndexCache.Lock()
		delete(searchIndexCache.byRoot, cacheKey)
		searchIndexCache.Unlock()
		fileWarmMemos.Lock()
		delete(fileWarmMemos.byDir, fileWarmKey{dir: warmA, hidden: false})
		delete(fileWarmMemos.byDir, fileWarmKey{dir: warmB, hidden: false})
		delete(fileWarmMemos.byRoot, fileWarmKey{dir: searchPathKey(dir), hidden: false})
		fileWarmMemos.Unlock()
	})

	if !warmSearchIndexForPath(fileA, false) {
		t.Fatal("first directory should discover the git root")
	}
	waitFileWarmDiscovery(t, warmA, false)
	if err := os.WriteFile(ignorePath, []byte("*.tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stamped := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(ignorePath, stamped, stamped); err != nil {
		t.Fatal(err)
	}
	changed := resolveSearchExcludePattern(nil, dir)
	if changed == original {
		t.Fatal("a different gitignore should change the exclude key")
	}
	if !warmSearchIndexForPath(fileB, false) {
		t.Fatal("a new directory still walks to its git boundary")
	}
	waitFileWarmDiscovery(t, warmB, false)
	memo, ok := loadFileWarm(warmB, false)
	if !ok || memo.exclude != original || !searchPathEqual(memo.root, dir) {
		t.Fatalf("reused root %q exclude %q, want %q %q", memo.root, memo.exclude, dir, original)
	}
}

func TestFileWarmSubmoduleKeepsOwnRoot(t *testing.T) {
	dir, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Clean(dir)
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sub, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, ".gitignore"), []byte("*.tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	parentFile := filepath.Join(dir, "a.go")
	subFile := filepath.Join(sub, "b.go")
	if err := os.WriteFile(parentFile, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subFile, []byte("package b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	parentExclude := resolveSearchExcludePattern(nil, dir)
	subExclude := resolveSearchExcludePattern(nil, sub)
	if parentExclude == "" || subExclude == "" || parentExclude == subExclude {
		t.Fatalf("parent exclude %q and submodule exclude %q should both be set and differ", parentExclude, subExclude)
	}
	warmParent := fileWarmDirectory(parentFile)
	warmSub := fileWarmDirectory(subFile)
	parentKey := searchIndexCorpusKey(dir, parentExclude, false)
	subKey := searchIndexCorpusKey(sub, subExclude, false)
	t.Cleanup(func() {
		waitFileWarmDiscovery(t, warmParent, false)
		waitFileWarmDiscovery(t, warmSub, false)
		forgetSearchBuildSlot(parentKey)
		forgetSearchBuildSlot(subKey)
		searchIndexCache.Lock()
		delete(searchIndexCache.byRoot, parentKey)
		delete(searchIndexCache.byRoot, subKey)
		searchIndexCache.Unlock()
		fileWarmMemos.Lock()
		delete(fileWarmMemos.byDir, fileWarmKey{dir: warmParent, hidden: false})
		delete(fileWarmMemos.byDir, fileWarmKey{dir: warmSub, hidden: false})
		delete(fileWarmMemos.byRoot, fileWarmKey{dir: searchPathKey(dir), hidden: false})
		delete(fileWarmMemos.byRoot, fileWarmKey{dir: searchPathKey(sub), hidden: false})
		fileWarmMemos.Unlock()
	})

	if !warmSearchIndexForPath(parentFile, false) {
		t.Fatal("parent file should discover the outer root")
	}
	waitFileWarmDiscovery(t, warmParent, false)
	if !warmSearchIndexForPath(subFile, false) {
		t.Fatal("submodule file should discover its own root")
	}
	waitFileWarmDiscovery(t, warmSub, false)
	memo, ok := loadFileWarm(warmSub, false)
	if !ok || !searchPathEqual(memo.root, sub) || memo.exclude != subExclude {
		t.Fatalf("submodule root %q exclude %q, want %q %q", memo.root, memo.exclude, sub, subExclude)
	}
}

func TestFileWarmKnownDirectoryReschedulesWithoutRediscovery(t *testing.T) {
	dir, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Clean(dir)
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "a.go")
	if err := os.WriteFile(file, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	warmDir := fileWarmDirectory(file)
	exclude := resolveSearchExcludePattern(nil, dir)
	storeFileWarm(warmDir, false, dir, exclude)
	cacheKey := searchIndexCorpusKey(dir, exclude, false)
	t.Cleanup(func() {
		waitFileWarmDiscovery(t, warmDir, false)
		forgetSearchBuildSlot(cacheKey)
		searchIndexCache.Lock()
		delete(searchIndexCache.byRoot, cacheKey)
		searchIndexCache.Unlock()
		fileWarmMemos.Lock()
		delete(fileWarmMemos.byDir, fileWarmKey{dir: warmDir, hidden: false})
		delete(fileWarmMemos.byRoot, fileWarmKey{dir: searchPathKey(dir), hidden: false})
		fileWarmMemos.Unlock()
	})

	if !warmSearchIndexForPath(file, false) {
		t.Fatal("a remembered directory whose corpus is gone should schedule it again")
	}
	waitFileWarmDiscovery(t, warmDir, false)
	if _, ok := loadRootWarm(dir, false); ok {
		t.Fatal("rescheduling a known directory should not walk or reread gitignore")
	}
}

func waitFileWarmDiscovery(t *testing.T, dir string, hidden bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for fileWarmDiscoveryRunning(dir, hidden) {
		if time.Now().After(deadline) {
			t.Fatal("warm discovery did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSingleFileWarmUsesDirectoryExcludeKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "render.cpp")
	if err := os.WriteFile(file, []byte("void draw() { WarmExcludeNeedle(); }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "noise.log"), []byte("WarmExcludeNeedle\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":    file,
		"pattern": "WarmExcludeNeedle",
		"stats":   true,
	})
	if !strings.Contains(out, "fallback=base_not_directory") {
		t.Fatalf("single-file search missing fallback:\n%s", out)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	exclude := resolveSearchExcludePattern(nil, dir)
	if exclude == "" {
		t.Fatal("gitignore should be part of the directory exclude key")
	}
	deadline := time.Now().Add(8 * time.Second)
	for publishedSearchIndex(root, "", exclude, "", false) == nil {
		if time.Now().After(deadline) {
			t.Fatal("file warmup published a different exclude key than a directory search")
		}
		time.Sleep(5 * time.Millisecond)
	}
	warmed := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "WarmExcludeNeedle",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(warmed, file) || strings.Contains(warmed, "noise.log") || !strings.Contains(warmed, "mode=indexed") {
		t.Fatalf("warmed directory search:\n%s", warmed)
	}
}

func TestLiteralSearchTermsExtractsAlternationCommonPrefix(t *testing.T) {
	terms := literalSearchTerms("AlphaOne|AlphaTwo")
	if len(terms) == 0 || terms[0] != "Alpha" {
		t.Fatalf("literalSearchTerms common prefix = %#v, want Alpha", terms)
	}
}

func TestLiteralSearchTermsExtractsAlternationCommonSuffix(t *testing.T) {
	terms := literalSearchTerms("ReadAlpha|WriteAlpha")
	if len(terms) == 0 || terms[0] != "Alpha" {
		t.Fatalf("literalSearchTerms common suffix = %#v, want Alpha", terms)
	}
}

func TestLiteralSearchTermsFromTextDedupesUnicodeTerms(t *testing.T) {
	terms := literalSearchTermsFromText("本地检索 本地检索 Target Target")
	want := []string{"Target", "本地检索"}
	if len(terms) != len(want) {
		t.Fatalf("literalSearchTermsFromText = %#v, want %#v", terms, want)
	}
	for i := range want {
		if terms[i] != want[i] {
			t.Fatalf("literalSearchTermsFromText = %#v, want %#v", terms, want)
		}
	}
}

func TestLiteralSearchTermsFromTextDedupesCaseVariants(t *testing.T) {
	terms := literalSearchTermsFromText("Target target TARGET Other")
	want := []string{"Target", "Other"}
	if len(terms) != len(want) {
		t.Fatalf("literalSearchTermsFromText = %#v, want %#v", terms, want)
	}
	for i := range want {
		if terms[i] != want[i] {
			t.Fatalf("literalSearchTermsFromText = %#v, want %#v", terms, want)
		}
	}
}

func TestLiteralSearchTermsFromTextDedupesUnicodeCaseVariants(t *testing.T) {
	terms := literalSearchTermsFromText("Äpfel äpfel Other")
	want := []string{"Äpfel", "Other"}
	if len(terms) != len(want) {
		t.Fatalf("literalSearchTermsFromText = %#v, want %#v", terms, want)
	}
	for i := range want {
		if terms[i] != want[i] {
			t.Fatalf("literalSearchTermsFromText = %#v, want %#v", terms, want)
		}
	}
}

func TestContentTrigramsBytesMatchesStringEntryPoint(t *testing.T) {
	input := "Alpha_123 beta 中文Target"
	fromString := contentTrigrams(input)
	fromBytes := contentTrigramsBytes([]byte(input))
	if len(fromString) != len(fromBytes) {
		t.Fatalf("trigram count mismatch: string=%#v bytes=%#v", fromString, fromBytes)
	}
	for trigram := range fromString {
		if !fromBytes[trigram] {
			t.Fatalf("byte trigram set missing %q: string=%#v bytes=%#v", trigram, fromString, fromBytes)
		}
	}
}

func TestIndexedSearchFindsASCIINeedleBesideNonASCII(t *testing.T) {
	ascii := contentTrigrams("NeedleBesideHan")
	mixed := contentTrigrams("NeedleBesideHan 注释")
	if !ascii["nee"] || !mixed["nee"] {
		t.Fatalf("ASCII trigram nee missing beside non-ASCII: ascii=%v mixed=%v", ascii["nee"], mixed["nee"])
	}
	dir := t.TempDir()
	hit := filepath.Join(dir, "hit.go")
	miss := filepath.Join(dir, "miss.go")
	if err := os.WriteFile(hit, []byte("package main\n// 注释\nfunc NeedleBesideHan() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(miss, []byte("package main\n// 注释\nfunc OtherSymbol() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "NeedleBesideHan",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, hit) || strings.Contains(out, miss) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("indexed search missed an ASCII needle beside non-ASCII:\n%s", out)
	}
}

func TestContentTrigramsBytesASCIIFastPathNormalizesTerms(t *testing.T) {
	got := contentTrigramsBytes([]byte("Target-Value TARGET_value"))
	for _, want := range []string{"tar", "arg", "val", "get", "t_v"} {
		if !got[want] {
			t.Fatalf("contentTrigramsBytes missing %q from ASCII input: %#v", want, got)
		}
	}
	if got["t-v"] {
		t.Fatalf("contentTrigramsBytes included separator trigram: %#v", got)
	}
}

func TestToolRipgrepLocalIndexDoesNotRequireOptionalGroup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc Alpha() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "Alpha(Beta)?",
		"output_mode": "files_with_matches",
	})
	if !strings.Contains(out, path) {
		t.Fatalf("indexed search required optional group:\n%s", out)
	}
}

func TestToolRipgrepLocalIndexNarrowsCandidateSet(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 40; i++ {
		path := filepath.Join(dir, "pkg", fmt.Sprintf("file_%02d.go", i))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		content := "package pkg\nfunc CommonSymbol() {}\n"
		if i == 17 {
			content += "func RareNeedleForCandidateNarrowing() {}\n"
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "RareNeedleForCandidateNarrowing",
		"glob":        "**/*.go",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	for _, want := range []string{"mode=indexed", "indexed_files=40", "candidates=1", "searched=1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("indexed search did not narrow candidates with %q:\n%s", want, out)
		}
	}
}

func TestToolRipgrepFallsBackToFullScanWhenIndexWouldTruncate(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a_first.go")
	second := filepath.Join(dir, "z_second.go")
	if err := os.WriteFile(first, []byte("package main\nfunc Other() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("package main\nfunc TargetAfterLimit() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	originalLimit := maxIndexedSearchFiles
	maxIndexedSearchFiles = 1
	defer func() { maxIndexedSearchFiles = originalLimit }()

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "TargetAfterLimit",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, second) {
		t.Fatalf("ripgrep missed file beyond truncated index limit:\n%s", out)
	}
	if !strings.Contains(out, "mode=full_scan") {
		t.Fatalf("ripgrep should fall back to full scan when index truncates:\n%s", out)
	}
	if !strings.Contains(out, "fallback=index_unavailable") {
		t.Fatalf("ripgrep should report index fallback reason when index truncates:\n%s", out)
	}
}

func TestToolRipgrepScopedIndexIgnoresNonMatchingFilesBeforeLimit(t *testing.T) {
	dir := t.TempDir()
	ignored := filepath.Join(dir, "a_ignored.txt")
	target := filepath.Join(dir, "z_target.go")
	if err := os.WriteFile(ignored, []byte("TargetInIgnoredText\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("package main\nfunc TargetScopedIndex() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	originalLimit := maxIndexedSearchFiles
	maxIndexedSearchFiles = 1
	defer func() { maxIndexedSearchFiles = originalLimit }()

	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "TargetScopedIndex",
		"glob":        "**/*.go",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, target) {
		t.Fatalf("ripgrep scoped index missed matching file:\n%s", out)
	}
	if strings.Contains(out, ignored) {
		t.Fatalf("ripgrep scoped index included ignored file:\n%s", out)
	}
	if !strings.Contains(out, "mode=indexed") {
		t.Fatalf("ripgrep should build an indexed scoped search:\n%s", out)
	}
}

func TestLocalSearchIndexDirtyCandidatesIncludesEqualModTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc Target() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	builtAt := time.Now().Truncate(time.Second)
	if err := os.Chtimes(path, builtAt, builtAt); err != nil {
		t.Fatal(err)
	}

	idx := &localSearchIndex{
		root:    dir,
		builtAt: builtAt,
		fileSet: map[string]bool{path: true},
	}
	files := idx.dirtyCandidateFiles("", "", "", false)
	if len(files) != 1 || files[0] != path {
		t.Fatalf("dirty candidates = %#v, want equal-modtime file", files)
	}
}

func TestLocalSearchIndexDirtyCandidatesTreatsSameFutureMetadataAsClean(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc TargetFutureClean() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	futureTime := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, futureTime, futureTime); err != nil {
		t.Fatal(err)
	}

	idx, ok := buildLocalSearchIndex(dir, "", "", "", false)
	if !ok {
		t.Fatal("buildLocalSearchIndex failed")
	}
	files := idx.dirtyCandidateFiles("", "", "", false)
	if len(files) != 0 {
		t.Fatalf("dirty candidates = %#v, want unchanged future-metadata file treated as clean", files)
	}
}

func TestBuildLocalSearchIndexCtxReturnsFalseWhenCancelled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc TargetCancelledIndex() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if idx, ok := buildLocalSearchIndexCtx(ctx, dir, "", "", "", false); ok || idx != nil {
		t.Fatalf("buildLocalSearchIndexCtx returned idx=%#v ok=%v, want cancelled failure", idx, ok)
	}
}

func TestIndexedSearchCandidatesCtxReportsCancelled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc TargetCancelledCandidates() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	candidates, ok, stats := indexedSearchCandidatesCtx(ctx, dir, "TargetCancelledCandidates", "", "", "", false, false, false)
	if ok || len(candidates) != 0 || stats.fallbackReason != "cancelled" {
		t.Fatalf("indexedSearchCandidatesCtx = ok=%v candidates=%#v stats=%+v, want cancelled fallback", ok, candidates, stats)
	}
}

func TestRebuildCachedSearchIndexForScopeCtxSkipsCacheWhenCancelled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc TargetCancelledRebuild() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if idx, ok := rebuildCachedSearchIndexForScopeCtx(ctx, dir, "", "", "", false); ok || idx != nil {
		t.Fatalf("rebuildCachedSearchIndexForScopeCtx returned idx=%#v ok=%v, want cancelled failure", idx, ok)
	}
	key := searchIndexCacheKey(filepath.Clean(dir), "", "", "", false)
	searchIndexCache.Lock()
	_, cached := searchIndexCache.byRoot[key]
	searchIndexCache.Unlock()
	if cached {
		t.Fatalf("cancelled rebuild cached index for %s", key)
	}
}

func TestLocalSearchIndexDirtyCandidatesCtxReturnsEmptyWhenCancelled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc TargetDirtyCancelled() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := &localSearchIndex{
		root:    dir,
		builtAt: time.Now().Add(-time.Hour),
		fileSet: map[string]bool{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if files := idx.dirtyCandidateFilesCtx(ctx, "", "", "", false); len(files) != 0 {
		t.Fatalf("dirtyCandidateFilesCtx returned %#v, want empty cancelled result", files)
	}
}

func TestLocalSearchIndexDirtyCandidatesReportsIncompleteWhenRootMissing(t *testing.T) {
	dir := t.TempDir()
	idx := &localSearchIndex{
		root:    filepath.Join(dir, "missing"),
		builtAt: time.Now(),
		fileSet: map[string]bool{},
	}

	files, complete := idx.dirtyCandidateFilesWithStatusCtx(context.Background(), "", "", "", false)
	if complete || len(files) != 0 {
		t.Fatalf("dirtyCandidateFilesWithStatusCtx = files=%#v complete=%v, want incomplete empty result", files, complete)
	}
}

func TestSearchIndexCachePrunesExpiredAndLRU(t *testing.T) {
	now := time.Now()
	searchIndexCache.Lock()
	original := searchIndexCache.byRoot
	defer func() {
		searchIndexCache.byRoot = original
		searchIndexCache.Unlock()
	}()
	searchIndexCache.byRoot = map[string]*localSearchIndex{
		"expired": {
			root:    "expired",
			builtAt: now.Add(-searchIndexCacheTTL - time.Second),
			usedAt:  now,
		},
		"old": {
			root:    "old",
			builtAt: now,
			usedAt:  now.Add(-4 * time.Minute),
		},
		"mid": {
			root:    "mid",
			builtAt: now,
			usedAt:  now.Add(-3 * time.Minute),
		},
		"newer": {
			root:    "newer",
			builtAt: now,
			usedAt:  now.Add(-2 * time.Minute),
		},
		"newest": {
			root:    "newest",
			builtAt: now,
			usedAt:  now.Add(-time.Minute),
		},
		"fresh": {
			root:    "fresh",
			builtAt: now,
			usedAt:  now,
		},
	}
	pruneSearchIndexCacheLocked(now)
	got := searchIndexCache.byRoot

	if _, ok := got["expired"]; ok {
		t.Fatalf("expired index was not pruned: %#v", got)
	}
	if _, ok := got["old"]; ok {
		t.Fatalf("least recently used index was not pruned: %#v", got)
	}
	for _, root := range []string{"mid", "newer", "newest", "fresh"} {
		if _, ok := got[root]; !ok {
			t.Fatalf("expected %q to remain after pruning: %#v", root, got)
		}
	}
}

func TestShouldRebuildSearchIndexForDirtyRatioOrCount(t *testing.T) {
	idx := &localSearchIndex{files: make([]string, 100)}
	if shouldRebuildSearchIndex(idx, 0) {
		t.Fatal("empty dirty set should not rebuild")
	}
	if shouldRebuildSearchIndex(idx, 10) {
		t.Fatal("small dirty ratio should not rebuild")
	}
	if !shouldRebuildSearchIndex(idx, 25) {
		t.Fatal("dirty ratio at threshold should rebuild")
	}
	if !shouldRebuildSearchIndex(&localSearchIndex{files: make([]string, 10000)}, maxDirtySearchFiles+1) {
		t.Fatal("dirty count above absolute threshold should rebuild")
	}
}

func TestToolRipgrepWalkDeadlineIsNotAFinishedSearch(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 700; i++ {
		name := filepath.Join(dir, fmt.Sprintf("%04d.txt", i))
		body := ""
		if i == 1 {
			body = "EARLY_MARKER\n"
		}
		if i == 699 {
			body = "LATE_MARKER\n"
		}
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	previous := maxSearchWalkTime
	maxSearchWalkTime = -time.Nanosecond
	defer func() { maxSearchWalkTime = previous }()

	// A literal pattern is answered from the search index, which never
	// consults the walk deadline. A pattern with no literal term takes the walk.
	got := ToolRipgrepDetailed(map[string]interface{}{
		"path":    dir,
		"pattern": ".",
	})
	if got.Outcome != SearchToolOutcomeMatched {
		t.Fatalf("partial walk outcome=%v text=%q", got.Outcome, got.Text)
	}
	if !strings.Contains(got.Text, "EARLY_MARKER") || strings.Contains(got.Text, "LATE_MARKER") || !strings.Contains(got.Text, searchWalkStoppedFooter) {
		t.Fatalf("deadline must keep the early hit and say the tree was not finished:\n%s", got.Text)
	}
	if strings.Contains(got.Text, "reached max_results=") {
		t.Fatalf("a time stop is not a full page:\n%s", got.Text)
	}
}

func TestToolRipgrepUnlistedWalkDeadlineKeepsIndexedAndStops(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "keep.go")
	if err := os.WriteFile(keep, []byte("package main\nconst KEEP_MARKER = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(dir, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	lockedAbs, err := filepath.Abs(locked)
	if err != nil {
		t.Fatal(err)
	}
	lockedAbs = filepath.Clean(lockedAbs)
	previousWalk := searchIndexWalk
	t.Cleanup(func() { searchIndexWalk = previousWalk })
	searchIndexWalk = func(ctx context.Context, walkRoot string, fn fs.WalkDirFunc) error {
		return previousWalk(ctx, walkRoot, func(path string, d os.DirEntry, err error) error {
			if err == nil && d != nil && d.IsDir() && searchPathEqual(path, lockedAbs) {
				if callErr := fn(path, d, nil); callErr != nil {
					return callErr
				}
				if callErr := fn(path, d, os.ErrPermission); callErr != nil && callErr != filepath.SkipDir {
					return callErr
				}
				return filepath.SkipDir
			}
			return fn(path, d, err)
		})
	}
	args := map[string]interface{}{
		"path":    dir,
		"pattern": "MARKER",
		"stats":   true,
	}
	warm := ripgrepWhenIndexed(t, args)
	if !strings.Contains(warm, "KEEP_MARKER") || !strings.Contains(warm, "mode=indexed") {
		t.Fatalf("warmup should answer KEEP_MARKER from the index:\n%s", warm)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	exclude := resolveSearchExcludePattern(args, root)
	idx := publishedSearchIndex(root, "", exclude, "", false)
	if idx == nil || len(idx.unlisted) == 0 {
		t.Fatal("warmup did not record the unlisted directory")
	}
	searchIndexWalk = previousWalk
	for i := 0; i < 700; i++ {
		name := filepath.Join(locked, fmt.Sprintf("%04d.txt", i))
		body := ""
		if i == 1 {
			body = "EARLY_MARKER\n"
		}
		if i == 699 {
			body = "LATE_MARKER\n"
		}
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	previous := maxSearchWalkTime
	maxSearchWalkTime = -time.Nanosecond
	defer func() { maxSearchWalkTime = previous }()

	got := ToolRipgrepDetailed(args)
	if got.Outcome != SearchToolOutcomeMatched {
		t.Fatalf("partial unlisted walk outcome=%v text=%q", got.Outcome, got.Text)
	}
	if !strings.Contains(got.Text, "KEEP_MARKER") || !strings.Contains(got.Text, "EARLY_MARKER") || strings.Contains(got.Text, "LATE_MARKER") {
		t.Fatalf("deadline must keep the indexed file and the early listing, and omit the file the walk did not reach:\n%s", got.Text)
	}
	if !strings.Contains(got.Text, searchWalkStoppedFooter) || !strings.Contains(got.Text, "mode=indexed") {
		t.Fatalf("a stopped unlisted walk stays indexed and says the tree was not finished:\n%s", got.Text)
	}
	if strings.Contains(got.Text, "reached max_results=") {
		t.Fatalf("a time stop is not a full page:\n%s", got.Text)
	}
}

func TestToolRipgrepUnlistedWalkSkipsExcludedDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "keep.go")
	if err := os.WriteFile(keep, []byte("package main\nconst KEEP_MARKER = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(dir, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	lockedAbs, err := filepath.Abs(locked)
	if err != nil {
		t.Fatal(err)
	}
	lockedAbs = filepath.Clean(lockedAbs)
	previousWalk := searchIndexWalk
	t.Cleanup(func() { searchIndexWalk = previousWalk })
	searchIndexWalk = func(ctx context.Context, walkRoot string, fn fs.WalkDirFunc) error {
		return previousWalk(ctx, walkRoot, func(path string, d os.DirEntry, err error) error {
			if err == nil && d != nil && d.IsDir() && searchPathEqual(path, lockedAbs) {
				if callErr := fn(path, d, nil); callErr != nil {
					return callErr
				}
				if callErr := fn(path, d, os.ErrPermission); callErr != nil && callErr != filepath.SkipDir {
					return callErr
				}
				return filepath.SkipDir
			}
			return fn(path, d, err)
		})
	}
	args := map[string]interface{}{
		"path":    dir,
		"pattern": "MARKER",
		"exclude": "locked/drafts/",
		"stats":   true,
	}
	warm := ripgrepWhenIndexed(t, args)
	if !strings.Contains(warm, "KEEP_MARKER") || !strings.Contains(warm, "mode=indexed") {
		t.Fatalf("warmup should answer KEEP_MARKER from the index:\n%s", warm)
	}
	searchIndexWalk = previousWalk
	drafts := filepath.Join(locked, "drafts")
	if err := os.MkdirAll(drafts, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 700; i++ {
		body := ""
		if i == 1 {
			body = "DRAFTS_MARKER\n"
		}
		name := filepath.Join(drafts, fmt.Sprintf("%04d.txt", i))
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	visible := filepath.Join(locked, "visible.go")
	if err := os.WriteFile(visible, []byte("package main\nconst VISIBLE_MARKER = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	previous := maxSearchWalkTime
	maxSearchWalkTime = -time.Nanosecond
	defer func() { maxSearchWalkTime = previous }()

	got := ToolRipgrepDetailed(args)
	if got.Outcome != SearchToolOutcomeMatched {
		t.Fatalf("excluded unlisted walk outcome=%v text=%q", got.Outcome, got.Text)
	}
	if !strings.Contains(got.Text, "KEEP_MARKER") || !strings.Contains(got.Text, "VISIBLE_MARKER") || strings.Contains(got.Text, "DRAFTS_MARKER") {
		t.Fatalf("an excluded directory must not consume the unlisted walk:\n%s", got.Text)
	}
	if !strings.Contains(got.Text, "mode=indexed") || strings.Contains(got.Text, searchWalkStoppedFooter) {
		t.Fatalf("skipping the excluded directory finishes the unlisted walk:\n%s", got.Text)
	}
}

func TestToolRipgrepExactPageDoesNotClaimAnotherPage(t *testing.T) {
	dir := t.TempDir()
	var body strings.Builder
	for i := 0; i < 4; i++ {
		fmt.Fprintf(&body, "HIT%d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	exact := ToolRipgrepDetailed(map[string]interface{}{
		"path":        dir,
		"pattern":     "HIT",
		"max_results": float64(4),
	})
	if strings.Contains(exact.Text, "reached max_results=") || !strings.Contains(exact.Text, "HIT3") {
		t.Fatalf("a page that ends on the last hit must not invite offset:\n%s", exact.Text)
	}
	more := ToolRipgrepDetailed(map[string]interface{}{
		"path":        dir,
		"pattern":     "HIT",
		"max_results": float64(3),
	})
	if !strings.Contains(more.Text, "reached max_results=3") || !strings.Contains(more.Text, "HIT2") || strings.Contains(more.Text, "HIT3") {
		t.Fatalf("one hit past the page must stay pageable and stay off this page:\n%s", more.Text)
	}
}

func TestToolRipgrepCountOffsetSkipsOnce(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("hit\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	got := ToolRipgrepDetailed(map[string]interface{}{
		"path":        dir,
		"pattern":     "hit",
		"output_mode": "count",
		"max_results": float64(2),
		"offset":      float64(1),
	})
	if strings.Contains(got.Text, paths[0]) || !strings.Contains(got.Text, paths[1]) || !strings.Contains(got.Text, paths[2]) {
		t.Fatalf("count offset should skip one file, not two:\n%s", got.Text)
	}
	if strings.Contains(got.Text, "reached max_results=") {
		t.Fatalf("the last count page must not invite another offset:\n%s", got.Text)
	}
}

func TestToolRipgrepSortedPageKeepsLaterPath(t *testing.T) {
	dir := t.TempDir()
	// "a" is visited before "a-z.txt", but "a-z.txt" sorts first because '-' < separator.
	sooner := filepath.Join(dir, "a-z.txt")
	later := filepath.Join(dir, "a", "z.txt")
	if sooner >= later {
		t.Fatalf("fixture is not in sort order: %q >= %q", sooner, later)
	}
	for _, path := range []string{sooner, later} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("hit\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []string{"files_with_matches", "count"} {
		t.Run(mode, func(t *testing.T) {
			first := ToolRipgrepDetailed(map[string]interface{}{
				"path":        dir,
				"pattern":     "hit",
				"output_mode": mode,
				"max_results": float64(1),
			})
			if !strings.Contains(first.Text, sooner) || strings.Contains(first.Text, later) || !strings.Contains(first.Text, "reached max_results=1") {
				t.Fatalf("first sorted page:\n%s", first.Text)
			}
			second := ToolRipgrepDetailed(map[string]interface{}{
				"path":        dir,
				"pattern":     "hit",
				"output_mode": mode,
				"max_results": float64(1),
				"offset":      float64(1),
			})
			if strings.Contains(second.Text, sooner) || !strings.Contains(second.Text, later) || strings.Contains(second.Text, "reached max_results=") {
				t.Fatalf("second sorted page:\n%s", second.Text)
			}
		})
	}
	indexed := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "hit",
		"output_mode": "files_with_matches",
		"max_results": float64(1),
		"stats":       true,
	})
	if !strings.Contains(indexed, "mode=indexed") || !strings.Contains(indexed, sooner) || strings.Contains(indexed, later) {
		t.Fatalf("indexed first page:\n%s", indexed)
	}
	indexedNext := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "hit",
		"output_mode": "files_with_matches",
		"max_results": float64(1),
		"offset":      float64(1),
		"stats":       true,
	})
	if !strings.Contains(indexedNext, "mode=indexed") || strings.Contains(indexedNext, sooner) || !strings.Contains(indexedNext, later) {
		t.Fatalf("indexed second page:\n%s", indexedNext)
	}
}
