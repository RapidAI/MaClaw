package browser

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Fix 1+2: the click opens a settings panel with required tags and
// category; the run fills both, presses the panel's own button, and calls
// it published only on the site's answer (article id), not the toast.
func TestPublishFlowSettingsPanelRequiredFields(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "panel_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit")
	rep, err := RunPublish(d, PublishSpec{Title: "面板 测试 标题", Body: "正文第一段。\n\n第二段。", Tags: []string{"AI"}, StepBudget: 10 * time.Second})
	t.Log(rep.String(), rep.NetLog)
	if err != nil {
		t.Fatal(err)
	}
	if rep.State != PublishLive || rep.Evidence != "response" || rep.PostID != "777001" {
		t.Fatalf("state=%s evidence=%q id=%q", rep.State, rep.Evidence, rep.PostID)
	}
	site.mu.Lock()
	defer site.mu.Unlock()
	if len(site.published) != 1 || site.published[0]["tags"] != "AI" || site.published[0]["category"] == "" {
		t.Fatalf("published %+v", site.published)
	}
}

// Fix 1+2: a click that only autosaves a draft is draft_only, not
// submitted or published.
func TestPublishFlowDraftWriteIsDraftOnly(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "panel_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?draftonly=1")
	rep, err := RunPublish(d, PublishSpec{Title: "草稿 标题", Body: "正文。", StepBudget: 6 * time.Second})
	if err == nil || rep.State != PublishDraftOnly {
		t.Fatalf("err=%v state=%s rep=%s", err, rep.State, rep.String())
	}
	if len(site.published) != 0 {
		t.Fatal("published")
	}
}

// Fix 4 / steering (a): the image goes through the input inside the
// editor's iframe (which uploads), not the outer decoy that only makes
// local previews; the upload is confirmed by the site's answer.
func TestPublishFlowUploadsThroughTheEditorFrameInput(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "iframe_upload_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit")
	rep, err := RunPublish(d, PublishSpec{Files: []string{fixtureImage(t)}, Title: "框架 上传", Body: "正文在 iframe 里。", StepBudget: 15 * time.Second})
	t.Log(rep.String())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rep.String(), "the site stored them") {
		t.Fatalf("upload not confirmed by the network: %s", rep.String())
	}
	site.mu.Lock()
	defer site.mu.Unlock()
	if len(site.published) != 1 || !strings.Contains(site.published[0]["html"], "/img/") || rep.Evidence != "redirect" {
		t.Fatalf("published %+v evidence %q", site.published, rep.Evidence)
	}
}

// Fix 3: a title input without a box is filled from script focus.
func TestPublishFlowHiddenTitleFilled(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "hidden_title_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit")
	rep, err := RunPublish(d, PublishSpec{Title: "隐藏 标题 测试", TitleSelector: "#t", Body: "正文内容。", StepBudget: 10 * time.Second})
	t.Log(rep.String())
	if err != nil {
		t.Fatal(err)
	}
	site.mu.Lock()
	defer site.mu.Unlock()
	if len(site.published) != 1 || site.published[0]["title"] != "隐藏 标题 测试" {
		t.Fatalf("published %+v", site.published)
	}
}

// Fix 5+6: a captcha class name with no visible challenge is no captcha;
// a notice inside a same-origin iframe is reported by the page flags and
// by publish.
func TestBlockingNoticeInFrameAndNoClassOnlyCaptcha(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, _ := fixtureServer(t, "notice_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit")
	raw, err := d.Send("Runtime.evaluate", map[string]interface{}{"expression": browserPageFlagsScript, "returnByValue": true})
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	_ = json.Unmarshal(raw, &out)
	var payload struct {
		Flags BrowserPageFlags `json:"page_flags"`
	}
	if err := json.Unmarshal([]byte(out.Result.Value), &payload); err != nil {
		t.Fatalf("%v %s", err, raw)
	}
	if payload.Flags.CaptchaWidget || payload.Flags.Captcha {
		t.Fatalf("class-only captcha flagged: %+v", payload.Flags)
	}
	if !strings.Contains(payload.Flags.BlockingNotice, "转正答题后才能发布专栏") {
		t.Fatalf("notice %+v", payload.Flags)
	}
	rep, err := RunPublish(d, PublishSpec{Title: "t", Body: "b", StepBudget: 3 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "转正答题") || rep.Notice == "" || rep.State != PublishNoEditor {
		t.Fatalf("err=%v state=%s notice=%q", err, rep.State, rep.Notice)
	}
}

// A3 / BILI-cv53625427: a decoy input in the editor frame that only makes
// local previews is a failed upload; the real input is tried next and the
// post carries only stored pictures.
func TestPublishFlowDecoyPreviewIsNotAnUpload(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "decoy_preview_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit")
	rep, err := RunPublish(d, PublishSpec{Files: []string{fixtureImage(t)}, Title: "诱饵 上传", Body: "正文在 iframe 里。", MinImages: 1, StepBudget: 15 * time.Second})
	t.Log(rep.String())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rep.String(), "not an upload") || !strings.Contains(rep.String(), "the site stored them") {
		t.Fatalf("decoy not rejected or real upload not confirmed: %s", rep.String())
	}
	site.mu.Lock()
	defer site.mu.Unlock()
	if len(site.published) != 1 || !strings.Contains(site.published[0]["html"], "/img/") || strings.Contains(site.published[0]["html"], "blob:") {
		t.Fatalf("published %+v", site.published)
	}
}

// With only the preview-only input the image step fails and nothing is
// published.
func TestPublishFlowPreviewOnlyFailsTheImageStep(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "decoy_preview_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?nogood=1")
	rep, err := RunPublish(d, PublishSpec{Files: []string{fixtureImage(t)}, Title: "诱饵 上传", Body: "正文。", MinImages: 1, StepBudget: 10 * time.Second})
	t.Log(rep.String())
	if err == nil || rep.State == PublishLive || rep.State == PublishSubmitted {
		t.Fatalf("published with missing pictures: %v %s", err, rep.State)
	}
	site.mu.Lock()
	defer site.mu.Unlock()
	if len(site.published) != 0 {
		t.Fatalf("published %+v", site.published)
	}
}

// A site verified to upload at submit (lazy_upload) may keep previews
// local, but the submit answer must then name remote pictures.
func TestPublishFlowLazyUploadNeedsRemotePictures(t *testing.T) {
	addr := startFixtureChrome(t)
	for _, c := range []struct {
		q  string
		ok bool
	}{{"lazy=1", true}, {"lazy=2", false}} {
		srv, _ := fixtureServer(t, "decoy_preview_like.html")
		d := fixtureTab(t, addr, srv.URL+"/edit?"+c.q)
		rep, err := RunPublish(d, PublishSpec{Files: []string{fixtureImage(t)}, Title: "懒 上传", Body: "正文。", MinImages: 1, LazyUpload: true, StepBudget: 10 * time.Second})
		t.Log(c.q, rep.String(), rep.NetLog)
		if c.ok && (err != nil || rep.State != PublishLive) {
			t.Fatalf("%s: %v %s", c.q, err, rep.State)
		}
		if !c.ok && (err == nil || rep.State == PublishLive) {
			t.Fatalf("%s: unconfirmed lazy pictures passed: %s", c.q, rep.State)
		}
	}
}
