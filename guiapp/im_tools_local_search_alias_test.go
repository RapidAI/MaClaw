package guiapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalizeLocalFileSearchToolCallJSON(t *testing.T) {
	name, args, rewritten := canonicalizeLocalFileSearchToolCallJSON(
		"glob_file_search_tool",
		`{"path":"C:\\tmp\\book","glob_pattern":"**/*.{html,md,json}"}`,
	)
	if !rewritten {
		t.Fatal("expected glob_file_search_tool to be rewritten")
	}
	if name != "Glob" {
		t.Fatalf("canonical name = %q, want Glob", name)
	}
	if !strings.Contains(args, `"pattern":"**/*.{html,md,json}"`) {
		t.Fatalf("pattern alias missing: %q", args)
	}
	if !strings.Contains(args, `"path":"C:\\tmp\\book"`) {
		t.Fatalf("path missing: %q", args)
	}
}

func TestCanonicalizeLocalFileSearchToolCallJSON_SearchFiles(t *testing.T) {
	name, args, rewritten := canonicalizeLocalFileSearchToolCallJSON("search_files", `{"pattern":"*.md"}`)
	if !rewritten || name != "Glob" {
		t.Fatalf("search_files = name=%q rewritten=%v args=%s", name, rewritten, args)
	}
}

func TestCanonicalizeLocalFileSearchToolCallJSON_LowercaseGlob(t *testing.T) {
	name, args, rewritten := canonicalizeLocalFileSearchToolCallJSON("glob", `{"glob_pattern":"*.md"}`)
	if !rewritten || name != "Glob" {
		t.Fatalf("lowercase glob = name=%q rewritten=%v args=%s", name, rewritten, args)
	}
	if !strings.Contains(args, `"pattern":"*.md"`) {
		t.Fatalf("pattern alias missing: %q", args)
	}
}

func TestToolGlobFilesFindsPattern(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "page.md"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := (&IMMessageHandler{}).toolGlobFiles(context.Background(), map[string]interface{}{
		"path":         dir,
		"glob_pattern": "*.md",
	})
	if !strings.Contains(got, "page.md") {
		t.Fatalf("Glob missed temp markdown: %q", got)
	}
}

func TestBuiltinGlobAcceptsRuntimeOwner(t *testing.T) {
	registry := NewToolRegistry()
	registerBuiltinTools(registry, &IMMessageHandler{})
	glob, ok := registry.Get("Glob")
	if !ok || glob == nil {
		t.Fatal("Glob missing from builtin registry")
	}
	if !glob.RuntimePolicyOwnerArg {
		t.Fatal("Glob must accept runtime policy owner so cloud workspaces stay isolated")
	}
	if glob.HandlerCtx == nil {
		t.Fatal("Glob handler missing")
	}
}

func TestCanonicalizeLocalFileSearchToolCallJSON_LeavesReadFileAlone(t *testing.T) {
	name, args, rewritten := canonicalizeLocalFileSearchToolCallJSON("read_file", `{"path":"a.md","glob_pattern":"*.md"}`)
	if rewritten || name != "read_file" {
		t.Fatalf("read_file was rewritten: name=%q rewritten=%v args=%s", name, rewritten, args)
	}
	if strings.Contains(args, `"pattern"`) {
		t.Fatalf("read_file gained a glob pattern: %q", args)
	}
}
