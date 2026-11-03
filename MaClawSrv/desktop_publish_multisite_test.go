package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

const multiSiteReq = "目标：把最新日榜最热论文做成图文综述，发到知乎、CSDN和小红书。三个平台都要发出。"

func TestProfilesForRequestOrder(t *testing.T) {
	var ids []string
	for _, p := range browser.ProfilesForRequest(multiSiteReq) {
		ids = append(ids, p.ID)
	}
	if strings.Join(ids, ",") != "zhihu,csdn,xiaohongshu" {
		t.Fatalf("sites = %v", ids)
	}
	if n := len(browser.ProfilesForRequest("写一篇文章发到 CSDN")); n != 1 {
		t.Fatalf("single site: %d", n)
	}
}

func TestBodyFileRef(t *testing.T) {
	for in, want := range map[string]string{
		"$(cat /tmp/wf/body.md)":  "/tmp/wf/body.md",
		"`cat /tmp/a.md`":         "/tmp/a.md",
		"@/tmp/wf/body.md":        "/tmp/wf/body.md",
		"file:///tmp/x.txt":       "/tmp/x.txt",
		"/tmp/wf/body.md":         "/tmp/wf/body.md",
		"~/素材/review.md":          "~/素材/review.md",
		"## 标题\n\n正文 /tmp/a.md 里": "",
		"一段正文":                    "",
	} {
		if got := desktopBodyFileRef(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestMultiSitePublishGoesOn(t *testing.T) {
	run := "run_multisite_test"
	scope := agentruntime.Scope{RunID: run}
	desktopNoteTaskSpec(run, multiSiteReq)
	defer desktopTaskSpecs.Delete(run)
	defer desktopPublishRunTake(run)

	if s := desktopTaskSpecFor(run); s.Site != "zhihu" {
		t.Fatalf("first site %q", s.Site)
	}
	post := func(site, url string) string {
		desktopPublishUseSite(run, site)
		r := desktopPublishRecordUpdate(scope, func(r *desktopPublishRecord) {
			r.Site, r.State, r.URL, r.Title = site, browser.PublishLive, url, "Wave Forcing"
		})
		return desktopPublishStepText(scope, r)
	}
	// The model chose CSDN first (its editor was open).
	out := post("csdn", "https://blog.csdn.net/x/article/details/1")
	if !strings.Contains(out, "publish") || !strings.Contains(out, "知乎") {
		t.Fatalf("next-site text: %s", out)
	}
	if _, ok := desktopPublishRecordOf(run); ok {
		t.Fatal("settled record should be set aside")
	}
	if _, done := desktopPublishSettled(run); done {
		t.Fatal("run must go on to the other sites")
	}
	if s := desktopTaskSpecFor(run); s.Site != "zhihu" {
		t.Fatalf("next site %q", s.Site)
	}
	// Publishing CSDN again returns its record, not a second post.
	desktopPublishUseSite(run, "csdn")
	if r, ok := desktopPublishRecordOf(run); !ok || !r.posted() {
		t.Fatal("csdn record lost")
	}
	post("zhihu", "https://zhuanlan.zhihu.com/p/2")
	if _, done := desktopPublishSettled(run); done {
		t.Fatal("xiaohongshu still to do")
	}
	post("xiaohongshu", "https://www.xiaohongshu.com/explore/3")
	reply, done := desktopPublishSettled(run)
	if !done {
		t.Fatal("all sites settled")
	}
	for _, w := range []string{"知乎：已发布", "CSDN：已发布", "小红书：已发布", "details/1", "/p/2", "explore/3"} {
		if !strings.Contains(reply, w) {
			t.Fatalf("reply misses %q: %s", w, reply)
		}
	}
	if strings.Index(reply, "知乎") > strings.Index(reply, "CSDN") {
		t.Fatalf("reply not in request order: %s", reply)
	}
}

func TestMultiSiteReplyNamesMissingSite(t *testing.T) {
	run := "run_multisite_missing"
	scope := agentruntime.Scope{RunID: run}
	desktopNoteTaskSpec(run, multiSiteReq)
	defer desktopTaskSpecs.Delete(run)
	desktopPublishUseSite(run, "csdn")
	r := desktopPublishRecordUpdate(scope, func(r *desktopPublishRecord) {
		r.Site, r.State, r.URL = "csdn", browser.PublishLive, "https://blog.csdn.net/x/article/details/1"
	})
	desktopPublishStepText(scope, r)
	got := desktopReplyWithLink(run, "知乎要登录，停住了。")
	if !strings.Contains(got, "CSDN：已发布") || !strings.Contains(got, "知乎：没发出去") || !strings.Contains(got, "知乎要登录") {
		t.Fatalf("reply: %s", got)
	}
}
