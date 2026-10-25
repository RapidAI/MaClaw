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
	d := parseRichBody(richBody, 2, "【$1】")
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
	rep, err := RunPublish(d, PublishSpec{Files: richImages(t), Title: "分层 KV 缓存｜速读", Body: richBody, PlainBold: "【$1】", VerifyURL: srv.URL + "/notes", StepBudget: 20 * time.Second})
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
	if err == nil || rep.Stuck != "submit" || rep.State != PublishSubmitted {
		t.Fatalf("err=%v rep=%s", err, rep.String())
	}
}

// Pictures the site never stored (still a data:/blob: source) fail the
// read-back before submit instead of vanishing from the published post.
func TestPublishFlowRichBodyUnstoredPicturesFail(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?noupload=1")
	rep, err := RunPublish(d, PublishSpec{Files: richImages(t), Title: "t 未上传", Body: richBody, StepBudget: 30 * time.Second})
	if err == nil || rep.Stuck != "readback" || !strings.Contains(rep.String(), "upload failed") {
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
	rep, err := RunPublish(d, PublishSpec{Title: "t 纯文本", Body: body, PlainBold: "【$1】", StepBudget: 20 * time.Second})
	if err != nil {
		t.Fatalf("err=%v rep=%s", err, rep.String())
	}
	if len(site.published) != 1 || !strings.Contains(site.published[0]["html"]+site.published[0]["text"], "【") {
		t.Fatalf("published %v", site.published)
	}
}

// A refused submit reports what the page said, so the next try can fix it.
func TestPublishFlowSubmitRefusalQuotesPage(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?toolong=1")
	_, err := RunPublish(d, PublishSpec{Title: "t 太长", Body: "正文 **重点**", StepBudget: 10 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "正文超出字数限制") || len(site.published) != 0 {
		t.Fatalf("err=%v", err)
	}
}

// A post page whose JS is busy right after the submit (heavy page, dialog)
// must still count as published from the browser's own URL.
func TestPublishFlowSubmitBusyPostPageConfirms(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?busypost=1")
	rep, err := RunPublish(d, PublishSpec{Title: "t 忙页", Body: "正文 **重点**", StepBudget: 8 * time.Second})
	if len(site.published) != 1 {
		t.Fatalf("published %d", len(site.published))
	}
	for _, s := range rep.Steps {
		if s.Name == "submit" && (!s.OK || s.TookMS > 6000) {
			t.Fatalf("submit %+v err=%v", s, err)
		}
	}
}

// A published page that shows its pictures as lazy-load placeholders (data:
// src, real URL in a data- attribute) has stored them: no false failure.
func TestPublishFlowRichBodyLazyPublishedPicturesPass(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?lazypost=1")
	rep, err := RunPublish(d, PublishSpec{Files: richImages(t), Title: "论文解读：懒加载", Body: richBody, StepBudget: 20 * time.Second})
	if err != nil {
		t.Fatalf("err=%v rep=%s", err, rep.String())
	}
	if !strings.Contains(site.published[0]["html"], "data-actualsrc") || !strings.Contains(rep.String(), "published page: 2 picture(s)") {
		t.Fatalf("fixture not lazy or no check: %s", rep.String())
	}
}

// Pictures stored only after several seconds each still pass the read-back.
func TestPublishFlowRichBodySlowPictureStorePasses(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?slowupload=1")
	rep, err := RunPublish(d, PublishSpec{Files: richImages(t), Title: "论文解读：慢上传", Body: richBody, StepBudget: 12 * time.Second})
	if err != nil || len(site.published) != 1 {
		t.Fatalf("err=%v rep=%s", err, rep.String())
	}
}

// An editor that publishes its server-saved draft loses the last picture
// when the submit beats the save; the run waits for the page's own save
// request after the last edit instead of a fixed pause.
func TestPublishFlowRichBodyWaitsForServerDraftSave(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?serverdraft=1")
	rep, err := RunPublish(d, PublishSpec{Files: richImages(t), Title: "论文解读：草稿保存", Body: richBody, StepBudget: 10 * time.Second})
	if err != nil || rep.State != PublishLive || strings.Count(site.published[0]["html"], "<img") != 2 {
		t.Fatalf("err=%v rep=%s html=%q", err, rep.String(), site.published[0]["html"])
	}
}

// A failed picture upload is named from the page's own request at once,
// not after a timeout, and nothing is submitted.
func TestPublishFlowRichBodyUploadFailureNamed(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?noupload=1")
	start := time.Now()
	rep, err := RunPublish(d, PublishSpec{Files: richImages(t), Title: "论文解读：上传失败", Body: richBody, StepBudget: 10 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") || rep.State != PublishNotSubmitted || len(site.published) != 0 || time.Since(start) > 40*time.Second {
		t.Fatalf("err=%v took=%s rep=%s", err, time.Since(start), rep.String())
	}
}

// A picture missing from the live post is put back through the post's edit
// page, and the run ends live.
func TestPublishFlowRichBodyMissingPictureRepaired(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?droppic=1")
	rep, err := RunPublish(d, PublishSpec{Files: richImages(t), Title: "论文解读：补图", Body: richBody, EditURL: srv.URL + "/edit?load=1", StepBudget: 10 * time.Second})
	if err != nil || rep.State != PublishLive || len(site.published) != 2 || !strings.Contains(rep.String(), "repaired 1") {
		t.Fatalf("err=%v published=%d rep=%s", err, len(site.published), rep.String())
	}
}

// Without an edit page the post stays live but incomplete, never "live".
func TestPublishFlowRichBodyDroppedPictureIsIncomplete(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?droppic=1")
	rep, err := RunPublish(d, PublishSpec{Files: richImages(t), Title: "论文解读：丢图", Body: richBody, StepBudget: 10 * time.Second})
	if err == nil || len(site.published) != 1 || rep.State != PublishLiveIncomplete || rep.ResultURL == "" || !strings.Contains(err.Error(), "picture 2 missing") {
		t.Fatalf("err=%v rep=%s", err, rep.String())
	}
}

// A tab left on another page (the last run's post) is not the editor: the
// run opens EditorURL itself and publishes there.
func TestPublishFlowOpensTheEditorItself(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/success")
	rep, err := RunPublish(d, PublishSpec{Files: richImages(t), Title: "论文解读：自己打开编辑器", Body: richBody, EditorURL: srv.URL + "/edit", StepBudget: 10 * time.Second})
	if err != nil || rep.State != PublishLive || len(site.published) != 1 {
		t.Fatalf("err=%v rep=%s", err, rep.String())
	}
}

// With no editor on the page and none to open, the outcome is no_editor,
// named, and nothing is filled or submitted.
func TestPublishFlowNoEditorIsNamed(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/success")
	rep, err := RunPublish(d, PublishSpec{Title: "t", Body: "b", StepBudget: 3 * time.Second})
	if err == nil || rep.State != PublishNoEditor || !strings.Contains(err.Error(), "no editor on this page") || len(site.published) != 0 {
		t.Fatalf("err=%v rep=%s", err, rep.String())
	}
}

// The run's own deadline bounds every step: a picture that is never stored
// ends the run with its report before the caller's hard limit.
func TestPublishFlowDeadlineBoundsTheRun(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, _ := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?slowupload=1")
	start := time.Now()
	rep, err := RunPublish(d, PublishSpec{Files: richImages(t), Title: "t 截止", Body: richBody, StepBudget: 30 * time.Second, Deadline: time.Now().Add(8 * time.Second)})
	if err == nil || time.Since(start) > 15*time.Second || rep.State != PublishNotSubmitted {
		t.Fatalf("err=%v took=%s rep=%s", err, time.Since(start), rep.String())
	}
}

// An editor that polls an upload's processing state with reads after the
// upload request finished is still working on the picture, not idle.
func TestPublishFlowRichBodyUploadPollingIsNotIdle(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?pollupload=1")
	rep, err := RunPublish(d, PublishSpec{Files: richImages(t), Title: "t 轮询", Body: richBody, StepBudget: 10 * time.Second})
	if err != nil || rep.State != PublishLive || len(site.published) != 1 {
		t.Fatalf("err=%v rep=%s", err, rep.String())
	}
}
