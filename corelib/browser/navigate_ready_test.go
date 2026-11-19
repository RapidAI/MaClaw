package browser

import (
	"strings"
	"testing"
	"time"
)

func TestLoginWallURL(t *testing.T) {
	for _, u := range []string{"https://www.zhihu.com/signin?next=%2Fwrite", "https://www.amazon.com/ap/signin", "https://passport.example.com/x", "https://a.com/account/unhuman?type=captcha"} {
		if loginWallURL(u) == "" {
			t.Fatalf("%s should be a wall", u)
		}
	}
	for _, u := range []string{"https://zhuanlan.zhihu.com/write", "https://www.zhihu.com/"} {
		if loginWallURL(u) != "" {
			t.Fatalf("%s is not a wall", u)
		}
	}
}

func TestNavReadyNote(t *testing.T) {
	r := NavReadyReport{State: PageReadyState{URL: "https://zhuanlan.zhihu.com/write", ReadyState: "loading"}, Reloaded: true, Wall: "blank", Waited: 36 * time.Second}
	n := r.Note()
	for _, want := range []string{"final URL: https://zhuanlan.zhihu.com/write", "reloaded once", LoginWallMarker, "take over"} {
		if !strings.Contains(n, want) {
			t.Fatalf("missing %q in %q", want, n)
		}
	}
	ok := NavReadyReport{State: PageReadyState{URL: "https://x.com/", ReadyState: "complete", Interactive: 12}, Ready: true}
	if strings.Contains(ok.Note(), LoginWallMarker) {
		t.Fatal("ready page is not a wall")
	}
	login := NavReadyReport{State: PageReadyState{URL: "https://passport.jd.com/login"}, Wall: "login_form"}
	note := login.Note()
	if !strings.Contains(note, "submit_secret") || strings.Contains(note, "take over") {
		t.Fatalf("sign-in note = %q", note)
	}
	if reason, take := wallHandsPageOver("captcha"); !take || reason != "captcha" {
		t.Fatalf("captcha handoff %q %v", reason, take)
	}
	if _, take := wallHandsPageOver("login_form"); take {
		t.Fatal("a sign-in form must stay with submit_secret")
	}
}
