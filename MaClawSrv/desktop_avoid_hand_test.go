package main

import (
	"strings"
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

func TestPublishPlanPhases(t *testing.T) {
	ed := "https://creator.xiaohongshu.com/publish/publish?from=menu"
	if p := desktopPublishPhaseOf(desktopPublishRecord{}, false, ed); p != phaseCompose {
		t.Fatal(p)
	}
	if p := desktopPublishPhaseOf(desktopPublishRecord{}, false, "https://huggingface.co/papers"); p != phaseResearch {
		t.Fatal(p)
	}
	for st, want := range map[browser.PublishState]desktopPublishPhase{browser.PublishLive: phaseLive, browser.PublishLiveIncomplete: phaseIncomplete, browser.PublishSubmitted: phaseSubmitted, browser.PublishNotSubmitted: phaseCompose} {
		if p := desktopPublishPhaseOf(desktopPublishRecord{State: st}, true, ed); p != want {
			t.Fatalf("%s: %s", st, p)
		}
	}
	click := map[string]any{"script": `[...document.querySelectorAll('button')].find(b=>b.innerText==="发布").click()`}
	cases := []struct {
		ph   desktopPublishPhase
		act  string
		args map[string]any
		ok   bool
	}{
		{phaseResearch, "screenshot", nil, false},
		{phaseResearch, "probe", nil, true},
		{phaseResearch, "bash", nil, true},
		{phaseCompose, "upload", nil, false},
		{phaseCompose, "rich_fill", nil, false},
		{phaseCompose, "app_run", nil, false},
		{phaseCompose, "script", click, false},
		{phaseCompose, "batch", map[string]any{"plan": []any{map[string]any{"action": "upload"}}}, false},
		{phaseCompose, "publish", nil, true},
		{phaseCompose, "bash", nil, true},
		{phaseResearch, "batch", map[string]any{"plan": []any{map[string]any{"action": "script", "script": click["script"]}}}, false},
		{phaseSubmitted, "probe", nil, true},
		{phaseSubmitted, "click", nil, false},
		{phaseLive, "probe", nil, false},
		{phaseLive, "screenshot", nil, false},
		{phaseIncomplete, "app_run", nil, false},
	}
	for _, c := range cases {
		err := desktopPublishPlanCheck(c.ph, c.act, c.args)
		if (err == nil) != c.ok {
			t.Fatalf("%s/%s: %v", c.ph, c.act, err)
		}
		if err != nil && !strings.Contains(err.Error(), "publish_plan("+string(c.ph)+")") {
			t.Fatal(err)
		}
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
