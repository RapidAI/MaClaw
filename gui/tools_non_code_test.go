package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchFilesInProject_EmptyPath(t *testing.T) {
	result := searchFilesInProject("", "pattern", "")
	if !strings.Contains(result, "未指定") {
		t.Errorf("expected '未指定' message, got %q", result)
	}
}

func TestSearchFilesInProject_NoMatch(t *testing.T) {
	dir := t.TempDir()
	result := searchFilesInProject(dir, "nonexistent_pattern_xyz", "")
	if strings.Contains(result, "error") {
		t.Errorf("unexpected error: %s", result)
	}
}

func TestSearchFilesInProjectCtxCancelled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("needle\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := searchFilesInProjectCtx(ctx, dir, "needle", "")
	if !strings.Contains(result, "cancelled") {
		t.Fatalf("expected cancelled result, got %q", result)
	}
}

func TestRegisterNonCodeSearchFilesUsesContextHandler(t *testing.T) {
	registry := NewToolRegistry()
	app := &App{}
	registerNonCodeTools(registry, app)

	tool, ok := registry.Get("search_files")
	if !ok {
		t.Fatal("search_files not registered")
	}
	if tool.HandlerCtx == nil {
		t.Fatal("search_files should use HandlerCtx for cancellation")
	}
}

func TestCheckProjectHealthCtxCancelled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := checkProjectHealthCtx(ctx, dir)
	if !strings.Contains(result, "cancelled") {
		t.Fatalf("expected cancelled result, got %q", result)
	}
}

func TestRegisterNonCodeCheckHealthUsesContextHandler(t *testing.T) {
	registry := NewToolRegistry()
	app := &App{}
	registerNonCodeTools(registry, app)

	tool, ok := registry.Get("check_health")
	if !ok {
		t.Fatal("check_health not registered")
	}
	if tool.HandlerCtx == nil {
		t.Fatal("check_health should use HandlerCtx for cancellation")
	}
}

func TestCheckProjectHealth_EmptyPath(t *testing.T) {
	result := checkProjectHealth("")
	if !strings.Contains(result, "未指定") {
		t.Errorf("expected '未指定' message, got %q", result)
	}
}

func TestCheckProjectHealth_NoProject(t *testing.T) {
	dir := t.TempDir()
	result := checkProjectHealth(dir)
	if !strings.Contains(result, "未检测到") {
		t.Errorf("expected '未检测到' message, got %q", result)
	}
}

func TestRunGitCmd_InvalidDir(t *testing.T) {
	_, err := runGitCmd("/nonexistent_dir_xyz", "status")
	if err == nil {
		t.Error("expected error for invalid dir")
	}
}

func TestSearchFilesInProject_FindsMatch(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("line1\nfoo bar baz\nline3\n"), 0644)
	os.WriteFile(filepath.Join(dir, "binary.png"), []byte{0x89, 0x50, 0x4E, 0x47}, 0644)

	result := searchFilesInProject(dir, "foo bar", "")
	if !strings.Contains(result, "hello.txt") {
		t.Errorf("expected hello.txt in results, got %q", result)
	}
	if strings.Contains(result, "binary.png") {
		t.Errorf("binary file should be skipped, got %q", result)
	}
	if !strings.Contains(result, ":2:") {
		t.Errorf("expected line number 2, got %q", result)
	}
}

func TestSearchFilesInProject_RegexPattern(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "code.go"), []byte("func main() {}\nfunc helper() {}\n"), 0644)

	result := searchFilesInProject(dir, `func \w+\(\)`, "")
	if !strings.Contains(result, "code.go") {
		t.Errorf("expected code.go match, got %q", result)
	}
}

func TestSearchFilesInProject_FilePatternFilter(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.go"), []byte("package main\n"), 0644)
	os.WriteFile(filepath.Join(dir, "app.py"), []byte("import main\n"), 0644)

	result := searchFilesInProject(dir, "main", "*.go")
	if !strings.Contains(result, "app.go") {
		t.Errorf("expected app.go, got %q", result)
	}
	if strings.Contains(result, "app.py") {
		t.Errorf("app.py should be filtered out, got %q", result)
	}
}

func TestSearchFilesInProject_SkipsGitDir(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	os.MkdirAll(gitDir, 0755)
	os.WriteFile(filepath.Join(gitDir, "config"), []byte("secret_pattern\n"), 0644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("no match here\n"), 0644)

	result := searchFilesInProject(dir, "secret_pattern", "")
	if strings.Contains(result, "secret_pattern") {
		t.Errorf(".git should be skipped, got %q", result)
	}
}

func TestIsBinaryExtension(t *testing.T) {
	cases := []struct {
		name   string
		binary bool
	}{
		{"main.go", false},
		{"readme.md", false},
		{"photo.png", true},
		{"app.exe", true},
		{"archive.zip", true},
		{"data.json", false},
		{"doc.pdf", true},
		{"style.css", false},
		{"UPPER.PNG", true},
	}
	for _, tc := range cases {
		if got := isBinaryExtension(tc.name); got != tc.binary {
			t.Errorf("isBinaryExtension(%q) = %v, want %v", tc.name, got, tc.binary)
		}
	}
}

func TestIsOverlyBroadSearchPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := []struct {
		path  string
		broad bool
	}{
		{"/", true},
		{"D:\\workprj\\aicoder", false},
		{"D:\\专利申请测试1", false},
	}
	if home != "" {
		cases = append(cases, struct {
			path  string
			broad bool
		}{home, true})
	}
	for _, tc := range cases {
		if got := isOverlyBroadSearchPath(tc.path); got != tc.broad {
			t.Errorf("isOverlyBroadSearchPath(%q) = %v, want %v", tc.path, got, tc.broad)
		}
	}
}
