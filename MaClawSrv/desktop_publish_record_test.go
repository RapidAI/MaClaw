package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// The record comes from the publish report's typed state, and a repeated
// publish call in the same run returns the record instead of posting again.
func TestPublishRecordFromReportAndIdempotent(t *testing.T) {
	scope := agentruntime.Scope{RunID: "run-rec-1"}
	spec := browser.PublishSpec{Title: "t", VerifyURL: "https://creator.xiaohongshu.com/new/note-manager"}
	r := desktopRecordPublish(scope, spec, &browser.PublishReport{State: browser.PublishLive, ResultURL: spec.VerifyURL, PostID: "6adbf6450000000024036add"})
	if r.URL != "https://www.xiaohongshu.com/explore/6adbf6450000000024036add" || !r.posted() || r.Calls != 1 {
		t.Fatalf("%+v", r)
	}
	out, err := desktopPublish(scope, nil, map[string]any{"title": "t", "body": "b"})
	if err != nil || !strings.Contains(out, "PUBLISH STATE: live") || !strings.Contains(out, r.URL) {
		t.Fatalf("second publish must return the record: %q %v", out, err)
	}
	if got, _ := desktopPublishRecordOf("run-rec-1"); got.Calls != 1 {
		t.Fatalf("second call ran the pipeline: %+v", got)
	}
}

func TestPublishRecordStatesTell(t *testing.T) {
	for st, want := range map[browser.PublishState]string{
		browser.PublishNotSubmitted:   "run action=publish again",
		browser.PublishSubmitted:      "Do not publish again",
		browser.PublishLiveIncomplete: "do not call it complete",
	} {
		if s := desktopPublishRecordText(desktopPublishRecord{State: st, Missing: "picture 7 missing"}); !strings.Contains(s, want) {
			t.Fatalf("%s: %q", st, s)
		}
	}
	if (desktopPublishRecord{State: browser.PublishNotSubmitted}).posted() {
		t.Fatal("not_submitted counts as posted")
	}
}
