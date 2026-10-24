package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func TestAvoidListCheck(t *testing.T) {
	req := "随机挑一篇论文发布到我的小红书账号。不要选这些已经写过的：2610.30043 2610.24383。"
	if err := desktopAvoidListCheck("publish", map[string]any{"body": "论文 arXiv:2610.30043v1"}, req); err == nil {
		t.Fatal("listed paper must be refused")
	}
	if err := desktopAvoidListCheck("publish", map[string]any{"body": "论文 arXiv:2610.11111"}, req); err != nil {
		t.Fatalf("other paper: %v", err)
	}
	if err := desktopAvoidListCheck("publish", map[string]any{"body": "2610.30043"}, "写 2610.30043 的解读发到知乎"); err != nil {
		t.Fatalf("named paper (no avoid words): %v", err)
	}
}

func TestHandPublishCheck(t *testing.T) {
	req := "写一篇笔记并直接发布到我的小红书账号"
	ed := "https://creator.xiaohongshu.com/publish/publish?from=menu"
	if err := desktopHandPublishCheck("app_run", map[string]any{"steps": "click 900 700"}, req, ed, false); err == nil {
		t.Fatal("desktop click on editor before publish action must be refused")
	}
	if err := desktopHandPublishCheck("script", map[string]any{"script": `[...document.querySelectorAll('button')].find(b=>b.innerText==="发布").click()`}, req, ed, false); err == nil {
		t.Fatal("scripted publish click must be refused")
	}
	if err := desktopHandPublishCheck("script", map[string]any{"script": "document.title"}, req, ed, false); err != nil {
		t.Fatalf("plain script: %v", err)
	}
	if err := desktopHandPublishCheck("app_run", nil, req, ed, true); err != nil {
		t.Fatalf("after publish action: %v", err)
	}
	if err := desktopHandPublishCheck("app_run", nil, req, "https://huggingface.co/papers", false); err != nil {
		t.Fatalf("not an editor: %v", err)
	}
}

func TestPrepublishCheck(t *testing.T) {
	for _, a := range []string{"upload", "rich_fill", "screenshot", "app_run"} {
		if desktopPrepublishCheck(a, nil, 0) == nil {
			t.Fatalf("%s before publish must be refused", a)
		}
	}
	if desktopPrepublishCheck("probe", nil, 0) != nil {
		t.Fatal("first probe of the editor is allowed")
	}
	if desktopPrepublishCheck("probe", nil, 1) == nil {
		t.Fatal("second probe must be refused")
	}
	if desktopPrepublishCheck("batch", map[string]any{"plan": []any{map[string]any{"action": "upload"}}}, 0) == nil {
		t.Fatal("batch with an upload must be refused")
	}
	if desktopPrepublishCheck("bash", nil, 5) != nil || desktopPrepublishCheck("publish", nil, 5) != nil {
		t.Fatal("bash/publish must pass")
	}
}

func TestUnaskedFileCheckBeforePublish(t *testing.T) {
	req := "写一篇解读并直接发布到我的知乎专栏"
	if desktopUnaskedFileCheck("deliver", nil, req) == nil {
		t.Fatal("deliver must be refused")
	}
	if desktopUnaskedFileCheck("file_write", map[string]any{"path": "/home/desktop/Desktop/回执.md"}, req) == nil {
		t.Fatal("Desktop file must be refused")
	}
	if desktopUnaskedFileCheck("file_write", map[string]any{"path": "/tmp/zn/body.md"}, req) != nil {
		t.Fatal("scratch file must pass")
	}
	if desktopUnaskedFileCheck("deliver", nil, "发布到知乎，再给我一个 docx 文件") != nil {
		t.Fatal("asked file must pass")
	}
	if desktopUnaskedFileCheck("deliver", nil, "整理成文档给我") != nil {
		t.Fatal("non-publish task must pass")
	}
}

func TestDriftCheckPrepublishOnEditor(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u", RunID: "run_prepub"}
	st, p := desktopProgressFor(scope)
	if st == nil {
		t.Skip("no run state")
	}
	st.mu.Lock()
	p.lastURL = "https://creator.xiaohongshu.com/publish/publish?from=menu"
	st.mu.Unlock()
	req := "写一篇笔记并直接发布到我的小红书账号"
	if desktopDriftCheck(scope, "upload", map[string]any{}, req) == nil {
		t.Fatal("hand upload on the editor must be refused")
	}
	desktopPublishRecordUpdate(scope, func(r *desktopPublishRecord) {
		r.State, r.URL = browser.PublishLive, "https://www.xiaohongshu.com/explore/1"
	})
	if desktopDriftCheck(scope, "screenshot", map[string]any{}, req) == nil {
		t.Fatal("screenshot after a verified publish must be refused")
	}
}

func TestPublishTaskScreenshotCheck(t *testing.T) {
	ask := "写一篇小红书图文笔记并直接发布到我的小红书账号，发完把笔记链接回复给我。"
	if desktopPublishTaskScreenshotCheck("screenshot", ask, false) == nil {
		t.Fatal("screenshot before publish allowed on a publish task")
	}
	if desktopPublishTaskScreenshotCheck("screenshot", "帮我看看这个网页长什么样", false) != nil {
		t.Fatal("screenshot refused on a non-publish task")
	}
	if desktopPublishTaskScreenshotCheck("bash", ask, false) != nil {
		t.Fatal("bash refused")
	}
}
