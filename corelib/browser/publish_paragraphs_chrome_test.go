package browser

import (
	"strings"
	"testing"
	"time"
)

// Written the way the model often does: single line breaks only.
const paragraphBody = "MaClaw 是一个桌面 AI 助手。\n## 技术特性\n第一段：多 Bot 协作，每个 Bot 有自己的桌面。\n第二段：发布流程自动核对标题和正文。"

// An article keeps its paragraphs: what lands in the editor is split the
// way the body was written, not one block.
func TestPublishKeepsParagraphs(t *testing.T) {
	addr := startFixtureChrome(t)
	for _, page := range []string{"csdn_md_like.html", "pm_like.html"} {
		srv, site := fixtureServer(t, page)
		d := fixtureTab(t, addr, srv.URL+"/edit")
		body := paragraphBody
		if page == "pm_like.html" { // its result page renders no headings
			body = strings.Replace(body, "## ", "", 1)
		}
		rep, err := RunPublish(d, PublishSpec{Title: "MaClaw 介绍与技术特性", Body: body, StepBudget: 20 * time.Second})
		t.Logf("%s %+v", page, rep)
		if err != nil {
			t.Fatalf("%s: %v", page, err)
		}
		if len(site.published) != 1 {
			t.Fatalf("%s published %+v", page, site.published)
		}
		body = site.published[0]["body"]
		t.Logf("%s body=%q", page, body)
		if !strings.Contains(body, "多 Bot 协作") || strings.Count(body, "\n\n") < 3 || strings.Contains(body, "桌面 AI 助手。 ") {
			t.Fatalf("%s lost its paragraphs: %q", page, body)
		}
	}
}
