package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestDesktopSpeculateAllowlist(t *testing.T) {
	for _, ok := range []string{"check_login", "check_posted", "prepare_draft", "snapshot_page"} {
		if !desktopSpeculateAllowed(ok) {
			t.Fatalf("expected allowed %q", ok)
		}
	}
	for _, bad := range []string{"publish", "pay", "delete", "submit", "click_publish", "checkout", ""} {
		if desktopSpeculateAllowed(bad) {
			t.Fatalf("expected refused %q", bad)
		}
		msg := desktopSpeculateRefuse(bad)
		if !strings.Contains(msg, "refused") {
			t.Fatalf("refuse msg for %q: %q", bad, msg)
		}
	}
}

func TestDesktopSpeculateQuietNoSession(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-spec", RunID: "run_spec_1"}
	out, err := desktopSpeculateQuiet(scope, nil, "check_login", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("expected quiet empty, got %q", out)
	}
}

func TestDesktopSpeculatePrepareDraftPersists(t *testing.T) {
	dir := t.TempDir()
	old := desktopLedgerDir
	desktopLedgerDir = func() string { return dir }
	defer func() { desktopLedgerDir = old }()

	scope := agentruntime.Scope{TenantID: "t", UserID: "u-draft", RunID: "run_draft_1"}
	// Force a stable key by stubbing via a real user key path: desktopUserKey needs valid ids.
	out, err := desktopSpeculateQuiet(scope, nil, "prepare_draft", map[string]any{
		"title": "春日穿搭",
		"body":  "第一段内容。\n\n第二段内容。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "draft kept") {
		t.Fatalf("expected draft note, got %q", out)
	}
	key := desktopLedgerKey(scope)
	if key == "" {
		t.Fatal("empty ledger key")
	}
	es := desktopLedgerEntries(key)
	var found bool
	for _, e := range es {
		if e.Kind == "draft" && e.Title == "春日穿搭" && strings.Contains(e.Body, "第一段") {
			found = true
		}
	}
	if !found {
		t.Fatalf("draft not in ledger: %+v (dir %s)", es, filepath.Join(dir))
	}
	prompt := desktopLedgerPromptText(scope)
	if !strings.Contains(prompt, "DRAFT") || !strings.Contains(prompt, "春日穿搭") {
		t.Fatalf("prompt missing draft: %q", prompt)
	}
}

func TestDesktopLedgerPageStateSurvivesInPrompt(t *testing.T) {
	dir := t.TempDir()
	old := desktopLedgerDir
	desktopLedgerDir = func() string { return dir }
	defer func() { desktopLedgerDir = old }()
	oldNow := desktopLedgerNow
	now := time.Date(2026, 11, 3, 9, 0, 0, 0, time.Local)
	desktopLedgerNow = func() time.Time { return now }
	defer func() { desktopLedgerNow = oldNow }()

	scope := agentruntime.Scope{TenantID: "t", UserID: "u-page", RunID: "run_page_1"}
	desktopLedgerRememberProgress(scope, "https://creator.xiaohongshu.com/publish/publish", "标题A", "正文已填一段", "page_state")
	prompt := desktopLedgerPromptText(scope)
	if !strings.Contains(prompt, "PAGE STATE") || !strings.Contains(prompt, "creator.xiaohongshu.com") {
		t.Fatalf("prompt missing page state: %q", prompt)
	}
	if !strings.Contains(prompt, "正文已填") {
		t.Fatalf("prompt missing body: %q", prompt)
	}
}

func TestDesktopSpeculateForbiddenNeverRuns(t *testing.T) {
	_, err := desktopSpeculateQuiet(agentruntime.Scope{}, nil, "publish", nil)
	if err == nil || !strings.Contains(err.Error(), "irreversible") {
		t.Fatalf("expected irreversible refuse, got %v", err)
	}
}
