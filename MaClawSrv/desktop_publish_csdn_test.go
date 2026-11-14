package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// run_6840f5a1: a CSDN submit without proof was answered 已提交发布 and the
// run counted as a success; the srv receipt would even say 已经发出去了.
func TestUnconfirmedCSDNIsNotReportedAsPublished(t *testing.T) {
	r := desktopPublishRecord{Site: "csdn", State: browser.PublishSubmitted, Title: "VFlash"}
	reply := desktopReplyTemplate(r)
	if !strings.HasPrefix(reply, desktopUnconfirmedReply) || strings.Contains(reply, "已提交发布") || strings.Contains(reply, "链接：") {
		t.Fatalf("reply claims more than it knows: %q", reply)
	}
	meta := map[string]string{}
	desktopDropAskAfterPost(reply, meta)
	if meta["publish_state"] != "submitted_unconfirmed" {
		t.Fatalf("meta %v", meta)
	}
	entries := []desktopLedgerEntry{{Kind: "posted", Run: "run-x", Title: "VFlash", Detail: string(browser.PublishSubmitted)}}
	if _, ok := desktopLedgerRunPost(entries, "run-x"); ok {
		t.Fatal("an unconfirmed submit must not become the 已经发出去了 receipt")
	}
	t.Setenv("MACLAW_STYLE_LAYER", "off")
	if s := desktopPublishRecordText(r); !strings.Contains(s, "NOT confirmed") || strings.Contains(s, "Reply that it was submitted") {
		t.Fatalf("legacy text: %q", s)
	}
}

func TestCSDNIdAndAccountBecomeThePublicLink(t *testing.T) {
	scope := agentruntime.Scope{RunID: "run-csdn-account"}
	spec := browser.PublishSpec{Title: "排名", EditorURL: "https://editor.csdn.net/md/", VerifyURL: "https://mp.csdn.net/mp_blog/manage/article"}
	rec := desktopRecordPublish(scope, spec, &browser.PublishReport{State: browser.PublishLive, PostID: "170775764", Account: "znsoft", Evidence: "redirect", Pending: "审核"})
	want := "https://blog.csdn.net/znsoft/article/details/170775764"
	if rec.URL != want {
		t.Fatalf("url %q", rec.URL)
	}
	reply := desktopReplyTemplate(rec)
	if !strings.Contains(reply, want) || strings.Contains(reply, "没拿到公开链接") {
		t.Fatalf("reply %q", reply)
	}
	desktopPublishRecordTake(scope.RunID)
	// The page already named this article. A different account cookie must
	// not replace that link.
	scope = agentruntime.Scope{RunID: "run-csdn-keep-link"}
	kept := desktopRecordPublish(scope, spec, &browser.PublishReport{State: browser.PublishLive, PostID: "170775764", Account: "other", ResultURL: want, Evidence: "redirect"})
	if kept.URL != want {
		t.Fatalf("replaced a public link with %q", kept.URL)
	}
	desktopPublishRecordTake(scope.RunID)
	// A longer id that merely contains these digits is not this article.
	scope = agentruntime.Scope{RunID: "run-csdn-longer-id"}
	longer := desktopRecordPublish(scope, spec, &browser.PublishReport{State: browser.PublishLive, PostID: "170775764", Account: "znsoft", ResultURL: "https://blog.csdn.net/other/article/details/1707757640", Evidence: "redirect"})
	if longer.URL != want {
		t.Fatalf("longer id kept: %q", longer.URL)
	}
	desktopPublishRecordTake(scope.RunID)
	// The avatar next to the title is not the article. With the account
	// known it is rebuilt; without an account it is not kept.
	scope = agentruntime.Scope{RunID: "run-csdn-profile"}
	rebuilt := desktopRecordPublish(scope, spec, &browser.PublishReport{State: browser.PublishLive, PostID: "170775764", Account: "znsoft", ResultURL: "https://blog.csdn.net/znsoft", Evidence: "list"})
	if rebuilt.URL != want {
		t.Fatalf("profile link with account: %q", rebuilt.URL)
	}
	desktopPublishRecordTake(scope.RunID)
	scope = agentruntime.Scope{RunID: "run-csdn-profile-anon"}
	anon := desktopRecordPublish(scope, spec, &browser.PublishReport{State: browser.PublishLive, PostID: "170775764", ResultURL: "https://blog.csdn.net/znsoft", Evidence: "list"})
	if anon.URL != "" {
		t.Fatalf("profile link without account kept: %q", anon.URL)
	}
	desktopPublishRecordTake(scope.RunID)
}

func TestCSDNPublishedWithoutPublicLinkIsNotDone(t *testing.T) {
	dir := t.TempDir()
	old := desktopLedgerDir
	desktopLedgerDir = func() string { return dir }
	t.Cleanup(func() { desktopLedgerDir = old })
	run := "run-csdn-id-only"
	p := browser.ProfileForURL("https://editor.csdn.net/md/")
	if p == nil || p.PostURL == "" {
		t.Fatal("csdn profile")
	}
	desktopLedgerAdd("k-"+run, desktopLedgerEntry{Kind: "posted", Host: "editor.csdn.net", Run: run, Detail: string(browser.PublishLive)})
	if got := desktopSiteDoneWithoutLink(run, p); got != "" {
		t.Fatalf("id without a public link counted as done: %q", got)
	}
	bare := &browser.SiteProfile{ID: "bare", EditorURL: "https://editor.example/write"}
	desktopLedgerAdd("k-bare-nolink", desktopLedgerEntry{Kind: "posted", Host: "editor.example", Run: "run-bare-nolink", Detail: string(browser.PublishLive)})
	if got := desktopSiteDoneWithoutLink("run-bare-nolink", bare); got == "" {
		t.Fatal("a site with no public-url template should still count")
	}
}

func TestCSDNUnderReviewReplyAndNoStaleLink(t *testing.T) {
	r := desktopPublishRecord{Site: "csdn", State: browser.PublishLive, Title: "VFlash", URL: "https://blog.csdn.net/znsoft/article/details/170300001", Pending: "审核中"}
	if reply := desktopReplyTemplate(r); !strings.Contains(reply, "审核中") || !strings.Contains(reply, r.URL) {
		t.Fatalf("%q", reply)
	}
	scope := agentruntime.Scope{RunID: "run-csdn-stale"}
	spec := browser.PublishSpec{Title: "VFlash", EditorURL: "https://editor.csdn.net/md/", VerifyURL: "https://mp.csdn.net/mp_blog/manage/article"}
	rec := desktopRecordPublish(scope, spec, &browser.PublishReport{State: browser.PublishSubmitted, ResultURL: "https://mp.csdn.net/mp_blog/creation/success/169744427", PostID: "169744427"})
	if rec.URL != "" {
		t.Fatalf("unconfirmed record keeps a link: %q", rec.URL)
	}
	desktopPublishRecordTake(scope.RunID)
	rec = desktopRecordPublish(agentruntime.Scope{RunID: "run-csdn-live"}, spec, &browser.PublishReport{State: browser.PublishLive, ResultURL: "https://blog.csdn.net/znsoft/article/details/170300001", PostID: "170300001", Pending: "审核中", Evidence: "redirect"})
	if rec.URL != "https://blog.csdn.net/znsoft/article/details/170300001" || rec.Pending != "审核中" {
		t.Fatalf("%+v", rec)
	}
	desktopPublishRecordTake("run-csdn-live")
}
