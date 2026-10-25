package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// Every final publish state ends the run with its reply; none lets the model
// keep going (XHS-33/34 looped 80+ steps in "submitted").
func TestPublishSettledEndsTheRun(t *testing.T) {
	if agentservice.RunGoalReached == nil {
		t.Fatal("hook not installed")
	}
	if _, ok := desktopPublishSettled("run_none"); ok {
		t.Fatal("no record must not stop the run")
	}
	cases := map[browser.PublishState]string{
		browser.PublishLive:           "链接：https://x.test/p/1",
		browser.PublishLiveIncomplete: "不完整",
		browser.PublishSubmitted:      "没能确认",
	}
	for st, want := range cases {
		id := "run_goal_" + string(st)
		desktopPublishRecordsMu.Lock()
		desktopPublishRecords[id] = &desktopPublishRecord{State: st, URL: "https://x.test/p/1?x=1", Title: "T", Missing: "图2"}
		desktopPublishRecordsMu.Unlock()
		r, ok := agentservice.RunGoalReached(id)
		if !ok || !strings.Contains(r, want) {
			t.Fatalf("%s: ok=%v reply=%q", st, ok, r)
		}
		if st == browser.PublishSubmitted && strings.Contains(r, "链接") {
			t.Fatalf("unconfirmed post must carry no link: %q", r)
		}
	}
	desktopPublishRecordsMu.Lock()
	desktopPublishRecords["run_goal_ns"] = &desktopPublishRecord{State: browser.PublishNotSubmitted}
	desktopPublishRecordsMu.Unlock()
	if _, ok := desktopPublishSettled("run_goal_ns"); ok {
		t.Fatal("not_submitted is not final: the run may still publish")
	}
}
