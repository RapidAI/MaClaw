package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

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

	out := ToolRipgrep(map[string]interface{}{
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

	out := ToolRipgrep(map[string]interface{}{
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

	out := ToolRipgrep(map[string]interface{}{
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

func TestToolRipgrepFixedStringMatchesRegexMetacharacters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nvalue := \"target[0]+literal\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
		"path":         dir,
		"pattern":      "target[0]+literal",
		"fixed_string": true,
		"output_mode":  "files_with_matches",
	})
	if !strings.Contains(out, path) {
		t.Fatalf("ripgrep fixed_string missed literal metacharacters:\n%s", out)
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

	out := ToolRipgrep(map[string]interface{}{
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

func TestToolRipgrepStatsReportIndexRebuild(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.go")
	if err := os.WriteFile(first, []byte("package main\nfunc TargetOne() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := ToolRipgrep(map[string]interface{}{
		"path":    dir,
		"pattern": "TargetOne",
	}); !strings.Contains(out, first) {
		t.Fatalf("initial indexed search missing first file:\n%s", out)
	}

	second := filepath.Join(dir, "second.go")
	if err := os.WriteFile(second, []byte("package main\nfunc TargetTwo() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := ToolRipgrep(map[string]interface{}{
		"path":    dir,
		"pattern": "TargetTwo",
		"stats":   true,
	})
	if !strings.Contains(out, second) || !strings.Contains(out, "rebuilt=true") {
		t.Fatalf("ripgrep stats did not report rebuild for dirty index:\n%s", out)
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

	out := ToolRipgrep(map[string]interface{}{
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

	out := ToolRipgrep(map[string]interface{}{
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

	out := ToolRipgrep(map[string]interface{}{
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

	out := ToolRipgrep(map[string]interface{}{
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
	if out := ToolRipgrep(map[string]interface{}{
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
	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "TargetTwo",
		"output_mode": "files_with_matches",
	})
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
	if out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "InitialSymbol",
		"output_mode": "files_with_matches",
	}); !strings.Contains(out, path) {
		t.Fatalf("initial indexed search missing file:\n%s", out)
	}

	if err := os.WriteFile(path, []byte("package main\nfunc AddedAfterIndexBuild() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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
	if out := ToolRipgrep(map[string]interface{}{
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
	if out := ToolRipgrep(map[string]interface{}{
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
	if out := ToolRipgrep(map[string]interface{}{
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
	out := ToolRipgrep(map[string]interface{}{
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
	out = ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "TargetTwo",
		"output_mode": "files_with_matches",
	})
	if !strings.Contains(out, second) {
		t.Fatalf("rebuilt indexed search missed new file:\n%s", out)
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

func TestToolRipgrepLocalIndexSupportsCaseInsensitiveCandidates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc TargetCase() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolRipgrep(map[string]interface{}{
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

	out := ToolRipgrep(map[string]interface{}{
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

	out := ToolRipgrep(map[string]interface{}{
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
