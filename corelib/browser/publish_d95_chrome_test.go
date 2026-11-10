package browser

import (
	"strings"
	"testing"
)

// run_5ecb9d6d (papers 11-11): the restored tab showed an old draft
// (/md?articleId=…), a pasted picture wiped the earlier ones, and the post
// went out without a cover. A new post opens a fresh editor, pictures go in
// one at a time through the editor's own picture input, and the panel's
// empty cover input gets the first picture.
func TestPublishD95FreshEditorPictureInputAndCover(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := csdnServerPage(t, "csdn_md_input.html")
	d := fixtureTab(t, addr, srv.URL+"/md?articleId=170404442")
	files := csdnFiles(t)[:3]
	spec := csdnSpec(srv, files)
	spec.Body = strings.Replace(spec.Body, "\n\n[[img:4]]", "", 1)
	rep, err := RunPublish(d, spec)
	if err != nil {
		t.Fatalf("publish: %v\n%s", err, rep)
	}
	if rep.State != PublishLive {
		t.Fatalf("state %s\n%s", rep.State, rep)
	}
	if rep.Facts["fresh_editor"] != 1 {
		t.Fatalf("the old draft tab was reused: %v", rep.Facts)
	}
	site.mu.Lock()
	defer site.mu.Unlock()
	if len(site.published) != 1 {
		t.Fatalf("published %d times", len(site.published))
	}
	m := site.published[0]
	body, _ := m["markdowncontent"].(string)
	if title, _ := m["title"].(string); title != spec.Title || strings.Contains(body, "old draft") {
		t.Fatalf("old draft leaked: title=%q body=%q", title, body)
	}
	if n := len(markdownImageURLs(body)); n != len(files) {
		t.Fatalf("published body keeps %d of %d pictures:\n%s", n, len(files), body)
	}
	if cover, _ := m["cover"].(string); cover == "" {
		t.Fatalf("cover not set\n%s", rep)
	}
	if rep.Facts["cover_set"] != 1 {
		t.Fatalf("cover fact missing: %v", rep.Facts)
	}
}

// This run's own draft (a failed call left /md?articleId=N) is gone on with.
func TestPublishD95OwnDraftKept(t *testing.T) {
	spec := PublishSpec{EditorURL: "https://editor.csdn.net/md/"}
	cur := "https://editor.csdn.net/md?articleId=1"
	if !foreignDraft(cur, spec) {
		t.Fatal("restored draft must be replaced")
	}
	spec.OwnDrafts = []string{cur}
	if foreignDraft(cur, spec) {
		t.Fatal("own draft must be kept")
	}
	spec.OwnDrafts = nil
	spec.ReuseDraft = true
	if foreignDraft(cur, spec) {
		t.Fatal("asked-for draft must be kept")
	}
	if foreignDraft("https://editor.csdn.net/md/#x", PublishSpec{EditorURL: "https://editor.csdn.net/md/"}) {
		t.Fatal("the plain editor is not a draft")
	}
}

func TestPublishD95TitleProblem(t *testing.T) {
	for _, bad := range []string{"![## 一、先说结论", "## 一、先说结论", "# 标题", "标题](http://x)", "a\nb"} {
		if TitleProblem(bad) == "" {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, ok := range []string{"HF 日榜第一 SplatAnything", "#话题 也可以", "C# 入门"} {
		if why := TitleProblem(ok); why != "" {
			t.Errorf("%q refused: %s", ok, why)
		}
	}
}
