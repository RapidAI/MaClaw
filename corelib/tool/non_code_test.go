package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchFilesInProjectCtxCancelled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := SearchFilesInProjectCtx(ctx, dir, "needle", "")
	if !strings.Contains(result, "cancelled") {
		t.Fatalf("expected cancelled result, got %q", result)
	}
}

func TestCheckProjectHealthCtxCancelled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := CheckProjectHealthCtx(ctx, dir)
	if !strings.Contains(result, "cancelled") {
		t.Fatalf("expected cancelled result, got %q", result)
	}
}
