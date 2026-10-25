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
