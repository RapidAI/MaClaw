package browser

import (
	"os"
	"path/filepath"
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
