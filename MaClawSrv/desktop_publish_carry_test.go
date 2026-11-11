package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func TestReplyTemplateLiveWithoutLinkIsPublished(t *testing.T) {
	got := desktopReplyTemplate(desktopPublishRecord{State: browser.PublishLive, Title: "T", PostID: "170564208"})
	if !strings.HasPrefix(got, "已发布《T》") || !strings.Contains(got, "170564208") || strings.Contains(got, "没发出去") {
		t.Fatalf("reply = %q", got)
	}
	got = desktopReplyTemplate(desktopPublishRecord{State: browser.PublishLive, Title: "T", URL: "https://x/p/1", Earlier: true})
	if !strings.Contains(got, "之前那次已发布") || !strings.Contains(got, "https://x/p/1") {
		t.Fatalf("earlier reply = %q", got)
	}
}

func TestReplyMultiListsEarlierSiteAndLinklessPost(t *testing.T) {
	zh, cs := &browser.SiteProfile{ID: "zhihu", RequestWords: []string{"知乎"}}, &browser.SiteProfile{ID: "csdn", RequestWords: []string{"CSDN"}}
	xhs := &browser.SiteProfile{ID: "xiaohongshu", RequestWords: []string{"小红书"}}
	base := desktopTaskSpec{Sites: []*browser.SiteProfile{zh, cs, xhs}}
	recs := []desktopPublishRecord{
		{Site: "zhihu", State: browser.PublishLive, URL: "https://zhuanlan.zhihu.com/p/1", Earlier: true},
		{Site: "csdn", State: browser.PublishLive, PostID: "9"},
		{Site: "xiaohongshu", State: browser.PublishLive, URL: "https://www.xiaohongshu.com/explore/abc"},
	}
	got := desktopReplyMulti(base, recs, nil, "model words")
	if strings.Contains(got, "没发出去") || strings.Contains(got, "model words") {
		t.Fatalf("reply = %q", got)
	}
}

func TestPostCorrectionSkipsAPostTheReplyAlreadyLinks(t *testing.T) {
	if got := desktopPostCorrection("run_no_posts_here", "知乎：已发布 https://a/p/1\nCSDN：没发出去"); got != "" {
		t.Fatalf("correction without verified posts = %q", got)
	}
}

func TestSiteDoneWithoutLinkCountsCarriedSites(t *testing.T) {
	desktopPublishCarriedSites.Store("run_carry_t", []string{"zhihu"})
	defer desktopPublishCarriedSites.Delete("run_carry_t")
	if got := desktopSiteDoneWithoutLink("run_carry_t", &browser.SiteProfile{ID: "zhihu"}); got == "" {
		t.Fatal("carried site not done")
	}
	if got := desktopSiteDoneWithoutLink("run_carry_t", &browser.SiteProfile{ID: "csdn"}); got != "" {
		t.Fatalf("csdn = %q", got)
	}
}
