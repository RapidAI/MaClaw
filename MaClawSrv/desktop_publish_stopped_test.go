package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/recovery"
)

// papers 11-11 run_1e82770e: XHS submit went out unconfirmed and stopped the
// goal; the person sending the same instruction again must get a post-list
// check first, never a blind refusal and never a blind repost.
func TestStoppedPublishGoalIsRecheckedOnANewRun(t *testing.T) {
	req := d97Env(t)
	old := req
	old.Scope.RunID = recRunID("run-old")
	args := map[string]any{"action": "publish", "title": "短标题", "body": "正文", "editor_url": "https://write.example.org/new"}
	sk := desktopRecoveryScope(old.Scope)
	lineage, err := desktopRecLineage(old, old.Scope)
	if err != nil {
		t.Fatal(err)
	}
	goal := desktopRecPublishGoal(sk, lineage, desktopRecSite(old.Scope, args), "长标题")
	eng := desktopRecovery()
	spec, _ := json.Marshal(desktopRecPublishSpec{Pictures: 1, Body: 2, Title: desktopRecHash("长标题")})
	l, _, err := eng.Acquire(sk, recovery.AttemptRequest{Goal: goal, Surface: "site:x|publish", Method: "publish", Run: old.Scope.RunID, Consequential: true, Spec: string(spec)})
	if err != nil {
		t.Fatal(err)
	}
	if v := eng.Finish(sk, l.ID, recovery.Observation{Goal: goal, Method: "publish", Run: old.Scope.RunID, Side: "unconfirmed", Error: "no evidence"}); v.State != recovery.StateStopped {
		t.Fatalf("%+v", v)
	}
	// The same run is still refused.
	if _, err := desktopRecoveryBefore(old, old.Scope, "publish", args); err == nil || !strings.Contains(err.Error(), "never repeated") {
		t.Fatalf("same run: %v", err)
	}
	// Looking the post up is not another submit. Bot 5's check_posted was
	// refused here, so CSDN stayed unconfirmed.
	look := map[string]any{"action": "publish", "check_posted": true, "title": "短标题", "editor_url": "https://write.example.org/new", "verify_url": "https://write.example.org/list"}
	if _, err := desktopRecoveryBefore(old, old.Scope, "publish", look); err != nil {
		t.Fatalf("check_posted: %v", err)
	}
	// A new run passes the gate, marked for a recheck.
	req.Scope.RunID = recRunID("run-new")
	att, err := desktopRecoveryBefore(req, req.Scope, "publish", args)
	if err != nil || att == nil || !att.recheck || att.recheckTitle != desktopRecHash("长标题") {
		t.Fatalf("new run: %v %+v", err, att)
	}
	// Without a post list to read, nothing is filled.
	out, done, err := desktopPublishLedgerGuard(req.Scope, nil, browser.PublishSpec{Title: "短标题", EditorURL: "https://write.example.org/new"}, args)
	if !done || err == nil || !strings.Contains(err.Error(), "verify_url") {
		t.Fatalf("no list: %q %v %v", out, done, err)
	}
	// The acquire without a passing check is still refused.
	if _, err := desktopRecoveryStartPublish(req.Scope, &browser.PublishSpec{Title: "短标题", EditorURL: "https://write.example.org/new"}); err == nil {
		t.Fatal("acquired a stopped goal without a check")
	}
	// After a passing check the new run may post.
	att, _ = desktopRecoveryBefore(req, req.Scope, "publish", args)
	att.recheckOK = true
	got, err := desktopRecoveryStartPublish(req.Scope, &browser.PublishSpec{Title: "短标题", Body: "正文", Files: []string{"a.png"}, EditorURL: "https://write.example.org/new"})
	if err != nil || got == nil {
		t.Fatalf("after check: %v", err)
	}
	desktopRecoveryAbortPublish(req.Scope, got)
}

// An unconfirmed submit used to answer check_posted from the stored record
// and never open the post list (Bot 5, CSDN).
func TestCheckPostedReadsTheListAfterAnUnconfirmedSubmit(t *testing.T) {
	req := d97Env(t)
	desktopPublishRecordUpdate(req.Scope, func(r *desktopPublishRecord) {
		r.Title = "综述"
		r.Site = "csdn"
		r.State = browser.PublishSubmitted
	})
	out, err := desktopPublish(req.Scope, &browser.BrowserAgentSession{}, map[string]any{
		"action":       "publish",
		"check_posted": true,
		"title":        "综述",
		"editor_url":   "https://editor.csdn.net/md/",
		"verify_url":   "https://mp.csdn.net/mp_blog/manage/article",
	})
	if err == nil || !strings.Contains(err.Error(), "post list") {
		t.Fatalf("list was not read: out=%q err=%v", out, err)
	}
	rec, ok := desktopPublishRecordOf(req.Scope.RunID)
	if !ok || rec.State != browser.PublishSubmitted {
		t.Fatalf("record: ok=%v %+v", ok, rec)
	}
	desktopPublishRecordUpdate(req.Scope, func(r *desktopPublishRecord) {
		r.State = browser.PublishLive
		r.URL = "https://zhuanlan.zhihu.com/p/2105161721778278920"
	})
	live, err := desktopPublish(req.Scope, &browser.BrowserAgentSession{}, map[string]any{
		"action":       "publish",
		"check_posted": true,
		"title":        "综述",
		"editor_url":   "https://zhuanlan.zhihu.com/write",
	})
	if err != nil || !strings.Contains(live, "2105161721778278920") {
		t.Fatalf("live record should answer without the list: out=%q err=%v", live, err)
	}
}
