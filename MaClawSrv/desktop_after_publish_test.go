package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// After action=publish verified the post, two read-back steps pass and the
// third is refused so the run replies instead of hunting.
func TestAfterVerifiedPublishStopsTheHunt(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-ap", InstanceID: "i", SessionID: "s", RunID: "run-ap"}
	st, _ := desktopProgressFor(scope)
	if st == nil {
		t.Skip("no run state")
	}
	desktopProgressNote(scope, "publish", "verify_result: ok\nPUBLISHED and verified. Reply to the user once with this link: https://x.example/p/1", nil)
	req := "在小红书发布一篇笔记"
	for i := 0; i < 2; i++ {
		if err := desktopDriftCheck(scope, "script", nil, req); err != nil {
			t.Fatalf("read-back %d refused: %v", i, err)
		}
	}
	if desktopDriftCheck(scope, "probe", nil, req) == nil {
		t.Fatal("third step after a verified publish must be refused")
	}
}

func TestPostURLForListedID(t *testing.T) {
	if got := desktopPostURLFor("https://creator.xiaohongshu.com/new/note-manager", "6adbf6450000000024036add"); got != "https://www.xiaohongshu.com/explore/6adbf6450000000024036add" {
		t.Fatal(got)
	}
	if desktopPostURLFor("https://example.com/list", "1") != "" {
		t.Fatal("unknown host must give no link")
	}
}
