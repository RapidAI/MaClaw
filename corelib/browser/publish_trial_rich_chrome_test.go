package browser

import (
	"testing"
	"time"
)

// A test (trial_pictures) on a rich editor must report what it measured:
// run_854d0758 (Zhihu) got only trial=1, so every hypothesis expecting
// pictures_kept>=3 was "refuted" by a missing fact.

func TestRichEditorTrialReportsPicturesKept(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit")
	rep, err := RunPublish(d, PublishSpec{Files: richImages(t), Title: "论文解读：试插", Body: richBody, StepBudget: 20 * time.Second, TrialPictures: 2})
	t.Log(rep.String())
	if err != nil {
		t.Fatal(err)
	}
	want := float64(len(richImages(t)))
	if !rep.Trial || rep.Facts["trial"] != 1 || rep.Facts["pictures_wanted"] != want || rep.Facts["pictures_kept"] != want {
		t.Fatalf("facts %v", rep.Facts)
	}
	if len(site.published) != 0 {
		t.Fatal("a trial submitted")
	}
}

func TestRichEditorTrialCountsUnstoredPicturesAsNotKept(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "rich_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit?noupload=1")
	rep, err := RunPublish(d, PublishSpec{Files: richImages(t), Title: "论文解读：试插未存", Body: richBody, StepBudget: 20 * time.Second, TrialPictures: 2})
	t.Log(rep.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rep.Facts["pictures_kept"]; !ok || rep.Facts["pictures_kept"] >= rep.Facts["pictures_wanted"] {
		t.Fatalf("unstored pictures counted as kept: %v", rep.Facts)
	}
	if len(site.published) != 0 {
		t.Fatal("a trial submitted")
	}
}
