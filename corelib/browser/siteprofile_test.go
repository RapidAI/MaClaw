package browser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSiteProfilesLoadAndMatch(t *testing.T) {
	if err := SiteProfilesError(); err != nil {
		t.Fatal(err)
	}
	if p := ProfileForRequest("直接发布到我的知乎专栏"); p == nil || p.ID != "zhihu" || p.EditURL == "" {
		t.Fatalf("zhihu: %+v", p)
	}
	x := ProfileForRequest("写一篇笔记，发布到我的小红书账号")
	if x == nil || x.ID != "xiaohongshu" || x.VerifyURL == "" || x.PostURLFor("abc") == "" {
		t.Fatalf("xhs: %+v", x)
	}
	if ProfileForURL("https://creator.xiaohongshu.com/new/note-manager") != x {
		t.Fatal("host suffix match")
	}
	if ProfileForRequest("post this to https://blog.example.org/wp-admin") != nil {
		t.Fatal("unknown site must have no profile")
	}
}

func TestSiteProfileApplyKeepsExplicitFields(t *testing.T) {
	p := &SiteProfile{ID: "t", Hosts: []string{"t.test"}, EditorURL: "https://t.test/new", SubmitTexts: []string{"Go"}, SuccessTexts: []string{"Done"}}
	s := PublishSpec{EditorURL: "https://t.test/mine"}
	p.Apply(&s)
	if s.EditorURL != "https://t.test/mine" || len(s.SubmitTexts) != 1 || s.SuccessTexts[0] != "Done" {
		t.Fatalf("%+v", s)
	}
	var nilp *SiteProfile
	nilp.Apply(&s) // no profile: no change, no panic
}

func TestSiteProfileDirOverridesAndRejectsBadFiles(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.json"), []byte(`{"id":"wp","hosts":["blog.test"],"submit_texts":["Publish"]}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "b.json"), []byte(`{"id":"bad","hosts":["x.test"],"nonsense":1}`), 0o644)
	ps, err := loadProfiles(dir)
	if err == nil {
		t.Fatal("unknown field must be reported")
	}
	found := false
	for _, p := range ps {
		found = found || p.ID == "wp"
		if p.ID == "bad" {
			t.Fatal("bad profile loaded")
		}
	}
	if !found {
		t.Fatal("dir profile not loaded")
	}
}

func TestCSDNProfileRedirectsRichEditorAndBuildsPublicLink(t *testing.T) {
	p := ProfileForURL("https://mp.csdn.net/mp_blog/creation/editor/new")
	if p == nil || p.ID != "csdn" {
		t.Fatalf("mp.csdn.net must use the csdn profile: %+v", p)
	}
	s := PublishSpec{EditorURL: "https://mp.csdn.net/mp_blog/creation/editor"}
	p.Apply(&s)
	if s.EditorURL != "https://editor.csdn.net/md/" || s.BodyKind != "markdown" || s.UserCookie != "UserName" || len(s.PendingTexts) == 0 {
		t.Fatalf("rich editor not redirected to the markdown one: %+v", s)
	}
	s = PublishSpec{EditorURL: "https://editor.csdn.net/md?articleId=170286347"}
	p.Apply(&s)
	if s.EditorURL != "https://editor.csdn.net/md?articleId=170286347" {
		t.Fatalf("own draft must stay: %q", s.EditorURL)
	}
	if got := PostURLFrom(s.PostURL, "170300001", "znsoft"); got != "https://blog.csdn.net/znsoft/article/details/170300001" {
		t.Fatalf("post url %q", got)
	}
	if PostURLFrom(s.PostURL, "170300001", "") != "" || p.PostURLFor("1") != "" {
		t.Fatal("a link without the account name must not be built")
	}
	if id := successPostID(p.SuccessURL, "https://mp.csdn.net/mp_blog/creation/success/170300001"); id != "170300001" {
		t.Fatalf("id %q", id)
	}
	if !IsPostResultURL("https://mp.csdn.net/mp_blog/creation/success/169744427") {
		t.Fatal("success page must count as an earlier post's result page")
	}
}

func TestMarkdownImageURLsAndBodyComplete(t *testing.T) {
	txt := "a\n![在这里插入图片描述](https://i-blog.csdnimg.cn/direct/a.png)\n![x](https://i-blog.csdnimg.cn/direct/a.png)\n![](blob:xyz)\n<img src=\"https://i-blog.csdnimg.cn/direct/b.jpeg\">"
	if got := markdownImageURLs(txt); len(got) != 2 {
		t.Fatalf("distinct remote links: %v", got)
	}
	if bodyComplete("只有开头二十三个字", strings.Repeat("正文", 200)) == nil {
		t.Fatal("a truncated body must fail")
	}
	if err := bodyComplete(strings.Repeat("正文", 200)+"![](https://x/y.png)", strings.Repeat("正文", 200)); err != nil {
		t.Fatal(err)
	}
}
