package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

// Delivery must reuse the producer's own workspace write when the bytes are
// identical instead of stacking a synthetically named duplicate next to it
// (2026-08-28 birthday-deck turn: one delivery saved both
// 布偶宝宝5岁生日.pptx and attachment_022044_572.pptx — same bytes, two
// "文件已保存" lines).
func TestFindIdenticalWorkspaceFileReusesProducerWrite(t *testing.T) {
	workspace := t.TempDir()
	payload := []byte("pptx-bytes-布偶宝宝")
	if err := os.WriteFile(filepath.Join(workspace, "布偶宝宝5岁生日.pptx"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	// Same size but different content must not match.
	if err := os.WriteFile(filepath.Join(workspace, "same-size.pptx"), []byte("pptx-bytes-布偶宝孿"), 0o644); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(payload)
	got := findIdenticalWorkspaceFile(workspace, encoded)
	if got != filepath.Join(workspace, "布偶宝宝5岁生日.pptx") {
		t.Fatalf("must reuse the identical producer write, got %q", got)
	}
	if got := findIdenticalWorkspaceFile(workspace, base64.StdEncoding.EncodeToString([]byte("other"))); got != "" {
		t.Fatalf("unknown content must not match: %q", got)
	}
	if got := findIdenticalWorkspaceFile(filepath.Join(workspace, "missing"), encoded); got != "" {
		t.Fatalf("unreadable workspace must not match: %q", got)
	}
}
