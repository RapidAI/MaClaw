package browser

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// testdata/publish/csdn_caret.html keeps its own caret (only trusted
// keys/mouse/input move it) and toggles a picture link off when a picture is
// inserted on a selection already inside ![..](..) — the mechanism behind
// run_c5f76641 (1 of 5 pictures kept). The default flow must stop at the
// first lost link with measured facts; the same flow with native key chords
// before each picture (a strategy the model proposes, not a site rule) must
// keep every picture; a trial must not submit.

func caretSpec(t *testing.T, addr string) (PublishSpec, *fixtureSite2) {
	srv, site := csdnServerPage(t, "csdn_caret.html")
	s := csdnSpec(srv, csdnFiles(t))
	s.Deadline = time.Now().Add(4 * time.Minute)
	return s, &fixtureSite2{site}
}

type fixtureSite2 struct{ *csdnFixture }

func (f *fixtureSite2) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.published)
}

func TestCaretEditorDefaultFlowStopsAtFirstLostPicture(t *testing.T) {
	addr := startFixtureChrome(t)
	s, site := caretSpec(t, addr)
	d := fixtureTab(t, addr, "about:blank")
	t0 := time.Now()
	rep, err := RunPublish(d, s)
	if err == nil {
		t.Fatalf("default flow passed on a caret-keeping editor:\n%s", rep)
	}
	if !strings.Contains(err.Error()+rep.String(), "regression") || rep.Facts["regression"] != 1 || rep.Facts["lost_item"] != 1 {
		t.Fatalf("no measured regression: err=%v facts=%v\n%s", err, rep.Facts, rep)
	}
	if strings.Contains(strings.ToLower(rep.String()), "csdn") && strings.Contains(rep.String(), "does not allow") {
		t.Fatalf("blames the site: %s", rep)
	}
	if site.count() != 0 {
		t.Fatal("submitted after a lost picture")
	}
	if !strings.Contains(rep.String(), "[facts]") {
		t.Fatalf("no facts line:\n%s", rep)
	}
	t.Logf("%v\n%s", err, rep)
	if time.Since(t0) > 90*time.Second {
		t.Fatalf("took %s to notice", time.Since(t0))
	}
}

func TestCaretEditorNativeKeysKeepEveryPicture(t *testing.T) {
	addr := startFixtureChrome(t)
	s, site := caretSpec(t, addr)
	s.InsertPrepKeys = []string{"ctrl+End", "Enter"}
	d := fixtureTab(t, addr, "about:blank")
	rep, err := RunPublish(d, s)
	if err != nil || rep.State != PublishLive {
		t.Fatalf("state=%s err=%v\n%s", rep.State, err, rep)
	}
	if rep.Facts["pictures_kept"] != 4 || rep.Facts["published"] != 1 {
		t.Fatalf("facts %v", rep.Facts)
	}
	if site.count() != 1 {
		t.Fatalf("published %d times", site.count())
	}
	site.mu.Lock()
	body, _ := site.published[0]["markdowncontent"].(string)
	site.mu.Unlock()
	if n := len(markdownImageURLs(body)); n != 4 {
		t.Fatalf("published body has %d picture links:\n%s", n, body)
	}
}

func TestBeforeSubmitRefusalLeavesNothingSubmitted(t *testing.T) {
	addr := startFixtureChrome(t)
	s, site := caretSpec(t, addr)
	s.InsertPrepKeys = []string{"ctrl+End", "Enter"}
	calls := 0
	s.BeforeSubmit = func() error { calls++; return errors.New("attempt lease lost") }
	d := fixtureTab(t, addr, "about:blank")
	rep, err := RunPublish(d, s)
	if err == nil || !strings.Contains(err.Error()+rep.String(), "not submitted") || rep.State == PublishLive {
		t.Fatalf("state=%s err=%v\n%s", rep.State, err, rep)
	}
	if calls != 1 || site.count() != 0 {
		t.Fatalf("calls=%d published=%d", calls, site.count())
	}
}

func TestCaretEditorTrialDoesNotSubmit(t *testing.T) {
	addr := startFixtureChrome(t)
	s, site := caretSpec(t, addr)
	s.InsertPrepKeys = []string{"ctrl+End", "Enter"}
	s.TrialPictures = 2
	d := fixtureTab(t, addr, "about:blank")
	rep, err := RunPublish(d, s)
	if err != nil {
		t.Fatalf("trial: %v\n%s", err, rep)
	}
	if !rep.Trial || rep.State == PublishLive || rep.Facts["trial"] != 1 || rep.Facts["pictures_kept"] != 2 {
		t.Fatalf("trial=%v state=%s facts=%v\n%s", rep.Trial, rep.State, rep.Facts, rep)
	}
	if site.count() != 0 {
		t.Fatal("a trial submitted")
	}
}

func TestParseKeyChordAllowsOnlyCaretKeys(t *testing.T) {
	for _, ok := range []string{"End", "ctrl+End", "shift+ArrowDown", "Enter", "alt+PageDown"} {
		if _, _, err := ParseKeyChord(ok); err != nil {
			t.Fatalf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"Delete", "ctrl+a", "Backspace", "meta+End", "ctrl+Enter", "alt+Enter", "ctrl+Enter;rm", ""} {
		if _, _, err := ParseKeyChord(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}
