package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestStaleZhihuArticleIsNotThisRunsResult(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-stale", InstanceID: "i", SessionID: "s", RunID: "run-stale"}
	st := desktopRunFor(scope)
	if st == nil {
		t.Skip("no run state")
	}
	n := desktopProgressNote(scope, "script", "script ok: url https://zhuanlan.zhihu.com/p/2096823748510991511; title x", nil)
	if strings.Contains(n, "appears published") || !strings.Contains(n, "earlier article") {
		t.Fatalf("stale tab note = %q", n)
	}
	desktopProgressNote(scope, "navigate", "navigated to https://zhuanlan.zhihu.com/write", nil)
	n = desktopProgressNote(scope, "probe", "probed page (https://zhuanlan.zhihu.com/p/2096823748510991999; title y", nil)
	if !strings.Contains(n, "appears published") {
		t.Fatalf("own article note = %q", n)
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
