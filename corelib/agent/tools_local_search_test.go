package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
