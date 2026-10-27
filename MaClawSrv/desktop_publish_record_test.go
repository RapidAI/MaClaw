package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime/promptparts"
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
	if err != nil || !strings.Contains(out, "PUBLISH_RESULT state=published") || !strings.Contains(out, r.URL) {
		t.Fatalf("second publish must return the record: %q %v", out, err)
	}
	if got, _ := desktopPublishRecordOf("run-rec-1"); got.Calls != 1 {
		t.Fatalf("second call ran the pipeline: %+v", got)
	}
}

func TestPublishRecordStatesTell(t *testing.T) {
	t.Setenv("MACLAW_STYLE_LAYER", "off")
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

func TestDesktopTaskSpecTargets(t *testing.T) {
	zn := desktopTaskSpecOf("随机选一篇论文写解读，直接发布到我的知乎专栏。不要选这些已经写过的论文：2610.11765。")
	if zn.Site != "zhihu" || zn.EditorURL == "" || zn.EditURL != "{url}/edit" || !zn.Avoid["2610.11765"] {
		t.Fatalf("%+v", zn)
	}
	x := desktopTaskSpecOf("写一篇笔记，直接发布到我的小红书账号")
	if x.Site != "xiaohongshu" || x.VerifyURL == "" || x.EditURL != "" {
		t.Fatalf("%+v", x)
	}
	if s := desktopTaskSpecOf("帮我看看知乎上关于这篇论文的讨论"); s.EditorURL != "" {
		t.Fatalf("not a publish task: %+v", s)
	}
	desktopNoteTaskSpec("run-ts", "发布到我的知乎专栏")
	desktopNoteTaskSpec("run-ts", "发布到我的小红书账号")
	if desktopTaskSpecFor("run-ts").Site != "zhihu" {
		t.Fatal("the first request of a run fixes its spec")
	}
}

// draft_only is not posted and asks for another full publish; the record
// keeps truncated errors, the net log and the evidence.
func TestPublishRecordDraftOnlyAndDiagnostics(t *testing.T) {
	if (desktopPublishRecord{State: browser.PublishDraftOnly}).posted() {
		t.Fatal("draft_only counts as posted")
	}
	if s := desktopPublishRecordText(desktopPublishRecord{State: browser.PublishDraftOnly}); !strings.Contains(s, "draft_only") || !(strings.Contains(s, "again") || strings.Contains(s, "next_allowed=publish")) {
		t.Fatal(s)
	}
	if s := desktopPublishRecordText(desktopPublishRecord{State: browser.PublishNotSubmitted, Notice: "转正答题后才能发布专栏"}); !strings.Contains(s, "转正答题") {
		t.Fatal(s)
	}
	scope := agentruntime.Scope{RunID: "run-rec-diag"}
	long := strings.Repeat("x", 2000)
	var r desktopPublishRecord
	for i := 0; i < 5; i++ {
		r = desktopRecordPublish(scope, browser.PublishSpec{Title: "t", Files: []string{"/a.png", "/b.png"}}, &browser.PublishReport{State: browser.PublishNotSubmitted, NetLog: []string{"POST /x 200"}, Evidence: ""}, errorString(long))
	}
	if len(r.Errors) != desktopRecordErrMax || len([]rune(r.Errors[0])) > desktopRecordErrRunes+1 || len(r.NetLog) != 1 || r.PlannedImages != 2 {
		t.Fatalf("%+v", r)
	}
	// A later call with fewer pictures is refused before anything runs.
	if _, err := desktopPublish(scope, nil, map[string]any{"title": "t", "body": "b", "files": []any{"/a.png"}}); err == nil || !strings.Contains(err.Error(), "planned 2 picture") {
		t.Fatalf("fewer pictures not refused: %v", err)
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }

// With the style layer on the record is facts only: the fields carry the
// state, link, what is missing and the one next action; no reply wording.
func TestPublishRecordFactsOnly(t *testing.T) {
	cases := []struct {
		r     desktopPublishRecord
		state string
		next  string
	}{
		{desktopPublishRecord{State: browser.PublishLive, URL: "https://a/p/1"}, "published", "final_answer"},
		{desktopPublishRecord{State: browser.PublishLiveIncomplete, URL: "https://a/p/1", Missing: "picture 7"}, "live_incomplete", "final_answer"},
		{desktopPublishRecord{State: browser.PublishSubmitted}, "submitted_unconfirmed", "final_answer"},
		{desktopPublishRecord{State: browser.PublishDraftOnly}, "draft_only", "publish"},
		{desktopPublishRecord{State: browser.PublishNotSubmitted}, "not_submitted", "publish"},
		{desktopPublishRecord{State: browser.PublishNotSubmitted, Notice: "实名认证"}, "not_submitted", "final_answer"},
	}
	for _, c := range cases {
		s := desktopPublishRecordText(c.r)
		key, f := promptparts.ParseFact(s)
		if key != "PUBLISH_RESULT" || f["state"] != c.state || f["next_allowed"] != c.next || f["link"] != c.r.URL || f["missing"] != c.r.Missing || f["notice"] != c.r.Notice {
			t.Fatalf("%+v -> %q %v", c.r, s, f)
		}
		if strings.Contains(strings.ToLower(s), "reply") || strings.Contains(s, "user") {
			t.Fatalf("facts carry reply wording: %q", s)
		}
	}
}

func TestFinishedPhaseRefusalIsFacts(t *testing.T) {
	err := desktopPublishPlanCheck(phaseLive, "click", nil)
	if err == nil {
		t.Fatal("live phase allowed a click")
	}
	_, f := promptparts.ParseFact(err.Error()[strings.Index(err.Error(), "REFUSED"):])
	if f["next_allowed"] != "final_answer" || f["phase"] != string(phaseLive) {
		t.Fatalf("%v", err)
	}
	if _, f := promptparts.ParseFact(desktopUnaskedFileAfterPublish().Error()[strings.Index(desktopUnaskedFileAfterPublish().Error(), "REFUSED"):]); f["reason"] != "unasked_file" {
		t.Fatal(f)
	}
}
