package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestVerifiedResultIsGeneric(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-stale", InstanceID: "i", SessionID: "s", RunID: "run-stale"}
	st := desktopRunFor(scope)
	if st == nil {
		t.Skip("no run state")
	}
	// An earlier result open before this run filled anything is not verified.
	if n := desktopProgressNote(scope, "script", "script ok: url https://forum.example/t/99; title x 发布成功", nil); strings.Contains(n, "Verified") {
		t.Fatalf("stale page verified: %q", n)
	}
	desktopProgressNote(scope, "navigate", "navigated to https://forum.example/compose", nil)
	desktopNotePublishAttempt(scope, "batch", map[string]any{"plan": []any{
		map[string]any{"action": "rich_fill", "text": "# Weekly notes\nFirst line of the body here"},
		map[string]any{"action": "click", "text": "Publish"},
	}})
	// Still on the editor: not verified even if the text shows.
	if n := desktopProgressNote(scope, "script", "url https://forum.example/compose Weeklynotes", nil); strings.Contains(n, "Verified") {
		t.Fatalf("editor verified: %q", n)
	}
	// Moved, but read-back lacks the text: hint, not verified.
	if n := desktopProgressNote(scope, "probe", "probed page (https://forum.example/t/100) other", nil); strings.Contains(n, "Verified") || !strings.Contains(n, "Read it back") {
		t.Fatalf("moved note: %q", n)
	}
	if n := desktopProgressNote(scope, "script", "url https://forum.example/t/100; title Weekly notes; body First line", nil); !strings.Contains(n, "Verified") {
		t.Fatalf("own result not verified: %q", n)
	}
}

func TestNoProgressStillLetsApprovedPublishThrough(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-np", InstanceID: "i", SessionID: "s", RunID: "run-np"}
	st, p := desktopProgressFor(scope)
	if st == nil {
		t.Skip("no run state")
	}
	p.idle, p.lastBody = 99, 2822
	if err := desktopNoProgressCheckFull(scope, "script", true, false); err == nil || !strings.Contains(err.Error(), "press 发布 now") {
		t.Fatalf("side step err = %v", err)
	}
	if err := desktopNoProgressCheckFull(scope, "script", true, true); err != nil {
		t.Fatalf("publish step refused: %v", err)
	}
	if err := desktopNoProgressCheckFull(scope, "script", false, true); err == nil {
		t.Fatal("unapproved publish passed")
	}
}

func TestAfterPublishRefusesUnaskedDocument(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-afterpub", RunID: "run_afterpub"}
	if err := desktopAfterPublishCheck(scope, "app_open", map[string]any{"program": "libreoffice"}); err != nil {
		t.Fatalf("before publish: %v", err)
	}
	st, p := desktopProgressFor(scope)
	if st == nil {
		t.Skip("no run state")
	}
	st.mu.Lock()
	p.publishSeen = true
	st.mu.Unlock()
	if err := desktopAfterPublishCheck(scope, "app_open", map[string]any{"program": "libreoffice"}); err == nil {
		t.Fatal("libreoffice after publish allowed")
	}
	if err := desktopAfterPublishCheck(scope, "bash", map[string]any{"command": "python3 make.py out.docx"}); err == nil {
		t.Fatal("docx bash after publish allowed")
	}
	if err := desktopAfterPublishCheck(scope, "deliver", map[string]any{"path": "a.docx"}); err != nil {
		t.Fatalf("deliver refused: %v", err)
	}
	if err := desktopAfterPublishCheck(scope, "bash", map[string]any{"command": "ls"}); err != nil {
		t.Fatalf("plain bash refused: %v", err)
	}
}

func TestRichFocusScriptNotAWrite(t *testing.T) {
	if desktopScriptWritesRichBody(desktopRichFocusScript) || desktopScriptClicksPublish(desktopRichFocusScript) || desktopScriptSubmits(desktopRichFocusScript) {
		t.Fatal("rich focus script is refused by the guard")
	}
}

func TestNavigateClearsPageBoundBlocks(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u", RunID: "run_failnav"}
	st := desktopRunFor(scope)
	if st == nil {
		t.Skip("no run state in this build")
	}
	for i := 0; i < 2; i++ {
		desktopFailClassRecord(scope, "script", map[string]any{"script": "x"}, "", errors.New("page script failed: not found"))
	}
	if desktopFailClassCheck(scope, "script", map[string]any{"script": "y"}) == nil {
		t.Fatal("script not blocked after two failures")
	}
	desktopFailClassRecord(scope, "navigate", map[string]any{"url": "https://e.com/p/1"}, "navigated", nil)
	if err := desktopFailClassCheck(scope, "script", map[string]any{"script": "y"}); err != nil {
		t.Fatalf("script still blocked after navigate: %v", err)
	}
}

func TestAfterPublishRefusesUnaskedDeliver(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u", RunID: "run_deliver_guard"}
	if err := desktopAfterPublishDeliverCheck(scope, "x"); err != nil {
		t.Fatalf("before publish: %v", err)
	}
	st, p := desktopProgressFor(scope)
	if st == nil {
		t.Skip("no run state")
	}
	st.mu.Lock()
	p.publishSeen = true
	st.mu.Unlock()
	if err := desktopAfterPublishDeliverCheck(scope, "做10.18的 Hugging Face Daily Papers 摘要，发到知乎上"); err == nil {
		t.Fatal("unasked deliver after publish must be refused")
	}
	if err := desktopAfterPublishDeliverCheck(scope, "发到知乎上，再给我一个 docx 文件"); err != nil {
		t.Fatalf("asked file must pass: %v", err)
	}
}

func TestDriftGuardKeepsBrowserTasksInTheBrowser(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-drift", RunID: "run-drift"}
	st, p := desktopProgressFor(scope)
	if st == nil {
		t.Skip("no run state")
	}
	req := "做10.18的 Hugging Face Daily Papers 摘要，发到知乎上"
	if err := desktopDriftCheck(scope, "app_open", map[string]any{"program": "libreoffice"}, "整理一下这些数据"); err != nil {
		t.Fatalf("non-web task before any page: %v", err)
	}
	st.mu.Lock()
	p.lastURL = "https://site.example/write"
	st.mu.Unlock()
	if desktopDriftCheck(scope, "app_open", map[string]any{"program": "libreoffice"}, req) == nil {
		t.Fatal("office app allowed in browser task")
	}
	if desktopDriftCheck(scope, "bash", map[string]any{"command": "pandoc a.md -o a.docx"}, req) == nil {
		t.Fatal("docx allowed")
	}
	if err := desktopDriftCheck(scope, "bash", map[string]any{"command": "convert -size 1080x1440 xc:white cover.png"}, req); err != nil {
		t.Fatalf("image for upload refused: %v", err)
	}
	if err := desktopDriftCheck(scope, "app_open", map[string]any{"program": "chromium"}, req); err != nil {
		t.Fatalf("browser refused: %v", err)
	}
	if err := desktopDriftCheck(scope, "bash", map[string]any{"command": "pandoc a.md -o a.docx"}, req+"，再给我一个 docx 文件"); err != nil {
		t.Fatalf("asked file refused: %v", err)
	}
}

func TestDriftGuardNoCopyAfterPublish(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-drift2", RunID: "run-drift2"}
	st, p := desktopProgressFor(scope)
	if st == nil {
		t.Skip("no run state")
	}
	st.mu.Lock()
	p.lastURL, p.publishSeen = "https://site.example/p/1", true
	st.mu.Unlock()
	req := "做10.18的摘要，发到知乎上"
	if desktopDriftCheck(scope, "file_write", map[string]any{"path": "/home/desktop/Desktop/稿.md"}, req) == nil {
		t.Fatal("desktop copy allowed after publish")
	}
	if err := desktopDriftCheck(scope, "file_write", map[string]any{"path": "/tmp/x.md"}, req); err != nil {
		t.Fatal(err)
	}
}

func TestDriftGuardWebTaskBeforeFirstPage(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-drift3", RunID: "run-drift3"}
	if st, _ := desktopProgressFor(scope); st == nil {
		t.Skip("no run state")
	}
	if desktopDriftCheck(scope, "app_open", map[string]any{"program": "eog"}, "做一张封面图，在小红书发一篇图文笔记") == nil {
		t.Fatal("image viewer allowed in a publish task")
	}
	if err := desktopDriftCheck(scope, "app_open", map[string]any{"program": "eog"}, "打开图片查看器看看这张图"); err != nil {
		t.Fatal(err)
	}
}
