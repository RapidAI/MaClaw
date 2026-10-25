package browser

import (
	"strings"
	"testing"
	"time"
)

// Synthetic editors of other shapes than the two profiled sites; no site
// profile is used, so these run on generic discovery only.

func TestPublishGenericWPLikeAutosaveAndOldStatusAreNotAPublish(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "wp_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit")
	rep, err := RunPublish(d, PublishSpec{Title: "Generic block editor post", Body: fixtureBody, VerifyURL: srv.URL + "/notes", StepBudget: 20 * time.Second})
	t.Log(rep.String())
	if err != nil {
		t.Fatal(err)
	}
	site.mu.Lock()
	n := len(site.published)
	site.mu.Unlock()
	if n != 1 {
		t.Fatalf("published %d times; a pre-existing 'Published' label or the autosave URL must not end the submit", n)
	}
	if !strings.Contains(rep.String(), "Post published") && !strings.Contains(rep.String(), "page says") {
		t.Fatalf("submit should end on the page's new notice: %s", rep.String())
	}
}

func genericPublished(site *fixtureSite) []map[string]string {
	site.mu.Lock()
	defer site.mu.Unlock()
	return append([]map[string]string(nil), site.published...)
}

func TestPublishGenericPlainFormLabelTitleAndInputSubmit(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "plain_form_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit")
	rep, err := RunPublish(d, PublishSpec{Title: "A plain form topic", Body: fixtureBody, StepBudget: 15 * time.Second})
	t.Log(rep.String())
	if err != nil {
		t.Fatal(err)
	}
	pub := genericPublished(site)
	if len(pub) != 1 || pub[0]["title"] != "A plain form topic" || !strings.Contains(pub[0]["body"], "多模态检索新基准") {
		t.Fatalf("published %+v", pub)
	}
	if !strings.Contains(rep.ResultURL, "/p/123") || rep.State != PublishLive {
		t.Fatalf("result %q state %s", rep.ResultURL, rep.State)
	}
}

func TestPublishGenericMarkdownEditorInAFrame(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "iframe_md_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit")
	body := "## 今日要点\n\n1. **第一篇**：一种更快的推理方法。\n2. 第二篇：多模态检索新基准。"
	rep, err := RunPublish(d, PublishSpec{Title: "框架里的 Markdown 编辑器", Body: body, StepBudget: 15 * time.Second})
	t.Log(rep.String())
	if err != nil {
		t.Fatal(err)
	}
	pub := genericPublished(site)
	if len(pub) != 1 {
		t.Fatalf("published %+v", pub)
	}
	b := pub[0]["body"]
	if !strings.Contains(b, "## 今日要点") || !strings.Contains(b, "**第一篇**") || strings.Contains(b, "3. ") || strings.Count(b, "2. ") != 1 {
		t.Fatalf("markdown source not kept as written: %q", b)
	}
}

func TestPublishGenericGraphQLRefusalBehind200(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "graphql_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?refuse=1")
	rep, err := RunPublish(d, PublishSpec{Title: "A story", Body: fixtureBody, StepBudget: 10 * time.Second})
	t.Log(rep.String())
	if err == nil {
		t.Fatal("a refused publish must fail")
	}
	if len(genericPublished(site)) != 0 || !strings.Contains(err.Error(), "rate limit") || rep.State != PublishNotSubmitted {
		t.Fatalf("state %s err %v", rep.State, err)
	}
}

func TestPublishGenericGraphQLPublishNowPanel(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "graphql_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit")
	rep, err := RunPublish(d, PublishSpec{Title: "A story", Body: fixtureBody, StepBudget: 15 * time.Second})
	t.Log(rep.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(genericPublished(site)) != 1 {
		t.Fatal("not published once")
	}
}
