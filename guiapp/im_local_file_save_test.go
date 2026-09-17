package guiapp

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

// Regression for the 2026-09-15 totality fix: isolated owners (expert
// sessions) previously had no workspace, so current-channel delivery fell
// back to the host artifact store. Now the provisioned per-owner session
// workspace is the user-facing landing directory — the delivery must land
// there, next to the files the semantic tools wrote.
func TestSaveFileDataForLocalDeliveryLandsInExpertSessionWorkspace(t *testing.T) {
	app := newProjectSearchTestApp(t)
	h := &IMMessageHandler{app: app}
	owner := expertSessionUserID("builtin-pptx-maker")
	payload := []byte("deck-bytes")
	encoded := base64.StdEncoding.EncodeToString(payload)

	saved, err := h.saveFileDataForLocalDelivery(owner, "deck.pptx", encoded)
	if err != nil {
		t.Fatalf("save=%q err=%v", saved, err)
	}
	workspace := trustedPrincipalBoundWorkspace(h, owner)
	if workspace == "" {
		t.Fatalf("expert owner must have a provisioned workspace")
	}
	if filepath.Dir(saved) != workspace {
		t.Fatalf("delivery must land in the provisioned workspace %q, got %q", workspace, saved)
	}
	if data, readErr := os.ReadFile(saved); readErr != nil || string(data) != string(payload) {
		t.Fatalf("delivered bytes mismatch: %v", readErr)
	}

	// A second delivery of identical bytes must reuse the existing file
	// instead of stacking a synthetically named duplicate.
	again, err := h.saveFileDataForLocalDelivery(owner, "attachment_1.pptx", encoded)
	if err != nil {
		t.Fatalf("second save=%q err=%v", again, err)
	}
	if again != saved {
		t.Fatalf("identical bytes must reuse the first delivery, got %q want %q", again, saved)
	}
}
