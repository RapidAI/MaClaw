package browser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const richBody = `## 一、问题
大模型推理太慢，**核心瓶颈是 KV 缓存**。

[[img:1|图1：整体框架]]

## 二、方法
- **分层缓存**：热数据留在显存
- 冷数据下放内存

[[img:2|图2：实验结果]]

## 三、评价
**潜力很大**，但还要看开源复现。arXiv: https://arxiv.org/abs/2610.00001`

func richImages(t *testing.T) []string {
	src := fixtureImage(t)
	b, _ := os.ReadFile(src)
	var out []string
	for _, n := range []string{"fig1.png", "fig2.png"} {
		p := filepath.Join(t.TempDir(), n)
		_ = os.WriteFile(p, b, 0o644)
		out = append(out, p)
	}
	return out
}

func TestParseRichBody(t *testing.T) {
	d := parseRichBody(richBody, 2)
	if len(d.anchors) != 2 || d.anchors[1] != 1 || d.headings != 3 || len(d.bold) != 3 {
		t.Fatalf("doc %+v", d)
	}
	if !strings.Contains(d.segments[1], "<em>图1：整体框架</em>") || !strings.Contains(d.segments[0], "<strong>核心瓶颈是 KV 缓存</strong>") {
		t.Fatalf("segments %q", d.segments)
	}
	if strings.Contains(d.plain, "[[img") || !strings.Contains(d.plain, "【核心瓶颈是 KV 缓存】") || !strings.Contains(d.plain, "• ") {
		t.Fatalf("plain %q", d.plain)
	}
	if p := parseRichBody("第一行\n第二行", 0); p.rich || p.plain != "第一行\n第二行" {
		t.Fatalf("plain body changed: %+v", p)
	}
}

// Zhihu-like rich editor: headings and bold survive, each picture sits at
// its anchor, and the published page is re-read for both.
func TestPublishFlowRichBodyImagesAtAnchors(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit")
	rep, err := RunPublish(d, PublishSpec{Files: richImages(t), Title: "论文解读：分层 KV 缓存", Body: richBody, StepBudget: 20 * time.Second})
	t.Log(rep.String())
	if err != nil {
		t.Fatal(err)
	}
	h := site.published[0]["html"]
	i1, i2 := strings.Index(h, "<img"), strings.LastIndex(h, "<img")
	if i1 < 0 || i1 == i2 || i1 < strings.Index(h, "KV 缓存") || i1 > strings.Index(h, "二、方法") || i2 < strings.Index(h, "冷数据") || i2 > strings.Index(h, "三、评价") {
		t.Fatalf("pictures out of place: %s", h)
	}
	if !strings.Contains(h, "<strong>") || !strings.Contains(h, "<h3>") {
		t.Fatalf("formatting lost: %s", h)
	}
	if !strings.Contains(rep.String(), "published page: 2 picture(s)") {
		t.Fatalf("no published-page check: %s", rep.String())
	}
}

// An editor that drops formatting fails the read-back before submit.
func TestPublishFlowRichBodyLostBoldFails(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?plainpaste=1")
	rep, err := RunPublish(d, PublishSpec{Files: richImages(t), Title: "t 粗体", Body: richBody, StepBudget: 10 * time.Second})
	if err == nil || rep.Stuck != "readback" || !strings.Contains(rep.String(), "bold") {
		t.Fatalf("err=%v rep=%s", err, rep.String())
	}
	if len(site.published) != 0 {
		t.Fatal("published anyway")
	}
}

// An editor that puts pictures in the wrong place fails the read-back.
func TestPublishFlowRichBodyMisplacedImageFails(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?imgtop=1")
	rep, err := RunPublish(d, PublishSpec{Files: richImages(t), Title: "t 位置", Body: richBody, StepBudget: 10 * time.Second})
	if err == nil || rep.Stuck != "readback" || !strings.Contains(rep.String(), "picture") {
		t.Fatalf("err=%v rep=%s", err, rep.String())
	}
	if len(site.published) != 0 {
		t.Fatal("published anyway")
	}
}

// XHS-like: pictures go to the gallery input in order, the body is plain
// text with bold shown as 【…】 and no anchors left.
func TestPublishFlowXHSLikeStructuredBody(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "xhs_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit")
	rep, err := RunPublish(d, PublishSpec{Files: richImages(t), Title: "分层 KV 缓存｜速读", Body: richBody, VerifyURL: srv.URL + "/notes", StepBudget: 20 * time.Second})
	t.Log(rep.String())
	if err != nil {
		t.Fatal(err)
	}
	b := site.published[0]["body"]
	if !strings.Contains(b, "【核心瓶颈是 KV 缓存】") || strings.Contains(b, "[[img") || strings.Contains(b, "**") {
		t.Fatalf("body %q", b)
	}
	if !strings.Contains(rep.String(), "2 file") && !strings.Contains(rep.String(), "upload: ok") {
		t.Fatalf("upload %s", rep.String())
	}
}

// A page that freezes after the publish click must not invite a second publish.
func TestPublishFlowSubmitHangSaysClicked(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, _ := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?hang=1")
	rep, err := RunPublish(d, PublishSpec{Title: "t 卡住", Body: "正文 **重点**", StepBudget: 5 * time.Second})
	if err == nil || rep.Stuck != "submit" || !strings.Contains(err.Error(), "WAS clicked") || !strings.Contains(err.Error(), "do NOT click publish") {
		t.Fatalf("err=%v rep=%s", err, rep.String())
	}
}

// Pictures the site never stored (still a data:/blob: source) fail the
// read-back before submit instead of vanishing from the published post.
func TestPublishFlowRichBodyUnstoredPicturesFail(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?noupload=1")
	rep, err := RunPublish(d, PublishSpec{Files: richImages(t), Title: "t 未上传", Body: richBody, StepBudget: 60 * time.Second})
	if err == nil || rep.Stuck != "readback" || !strings.Contains(rep.String(), "has not stored them") {
		t.Fatalf("err=%v rep=%s", err, rep.String())
	}
	if len(site.published) != 0 {
		t.Fatal("published anyway")
	}
}

// An editor that keeps the quote style after a quoted line swallows the body;
// the read-back must catch it before submit.
func TestPublishFlowRichBodyStickyQuoteFails(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?stickyquote=1")
	body := "> 一句话：**分层缓存**更快。\n\n## 一、背景\n\n长上下文推理里 **KV 缓存**占满显存，这是主要瓶颈。\n\n## 二、方法\n\n把冷数据放到主机内存，热数据留在显存，**吞吐提升 2 倍**。"
	rep, err := RunPublish(d, PublishSpec{Title: "t 引用", Body: body, StepBudget: 20 * time.Second})
	if err == nil || rep.Stuck != "readback" || !strings.Contains(rep.String(), "quote block") {
		t.Fatalf("err=%v rep=%s", err, rep.String())
	}
	if len(site.published) != 0 {
		t.Fatal("published anyway")
	}
}

// A note editor that keeps no formatting at all gets the plain form with
// 【】 key points instead of a failed read-back.
func TestPublishFlowPlainEditorGetsMarkedPlainText(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?plainpaste=1")
	body := "## 一、背景\n\n长上下文推理里 **KV 缓存**占满显存。\n\n## 二、方法\n\n冷热分层，**吞吐提升 2 倍**。"
	rep, err := RunPublish(d, PublishSpec{Title: "t 纯文本", Body: body, StepBudget: 20 * time.Second})
	if err != nil {
		t.Fatalf("err=%v rep=%s", err, rep.String())
	}
	if len(site.published) != 1 || !strings.Contains(site.published[0]["html"]+site.published[0]["text"], "【") {
		t.Fatalf("published %v", site.published)
	}
}
