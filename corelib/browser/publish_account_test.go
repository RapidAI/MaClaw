package browser

import (
	"strings"
	"testing"
)

func TestAccountFromCookiesReadsTheWholeSite(t *testing.T) {
	hosts := []string{"mp.csdn.net", "editor.csdn.net", "blog.csdn.net"}
	cookies := []namedCookie{
		{Name: "UserName", Value: "other", Domain: ".example.com"},
		{Name: "username", Value: "znsoft", Domain: "passport.csdn.net"},
	}
	if got := accountFromCookies(cookies, "UserName", hosts); got != "znsoft" {
		t.Fatalf("passport host cookie = %q", got)
	}
	cookies = append(cookies, namedCookie{Name: "UserName", Value: "sitewide", Domain: ".csdn.net"})
	if got := accountFromCookies(cookies, "UserName", hosts); got != "sitewide" {
		t.Fatalf("site-wide cookie should win, got %q", got)
	}
	if accountFromCookies(cookies, "UserName", []string{"editor.other.test"}) != "" {
		t.Fatal("a csdn cookie matched another site")
	}
	if accountFromCookies([]namedCookie{{Name: "UserName", Value: "a/b", Domain: ".csdn.net"}}, "UserName", hosts) != "" {
		t.Fatal("a value that cannot sit in a URL path was kept")
	}
	split := []namedCookie{
		{Name: "UserName", Value: "znsoft", Domain: "passport.csdn.net"},
		{Name: "UserName", Value: "tracker", Domain: "ads.csdn.net"},
	}
	if accountFromCookies(split, "UserName", hosts) != "" {
		t.Fatal("two different account cookies were guessed")
	}
}

func TestAccountFromLinks(t *testing.T) {
	tmpl := "https://blog.csdn.net/{user}/article/details/{id}"
	if got := accountFromLinks([]string{"https://blog.csdn.net/znsoft/article/details/170775764"}, tmpl, ""); got != "znsoft" {
		t.Fatalf("article link = %q", got)
	}
	if got := accountFromLinks([]string{
		"https://blog.csdn.net/nav/watchers",
		"https://blog.csdn.net/znsoft",
	}, tmpl, ""); got != "znsoft" {
		t.Fatalf("profile link = %q", got)
	}
	if got := accountFromLinks([]string{"https://blog.csdn.net/znsoft", "https://blog.csdn.net/other"}, tmpl, ""); got != "" {
		t.Fatalf("two profiles guessed %q", got)
	}
	if got := accountFromLinks([]string{"https://mp.csdn.net/mp_blog/creation/success/170775764"}, tmpl, "170775764"); got != "" {
		t.Fatalf("console page yielded %q", got)
	}
	mixed := []string{
		"https://blog.csdn.net/other/article/details/999",
		"https://blog.csdn.net/znsoft/article/details/170775764",
	}
	if got := accountFromLinks(mixed, tmpl, ""); got != "" {
		t.Fatalf("two articles guessed %q", got)
	}
	if got := accountFromLinks(mixed, tmpl, "170775764"); got != "znsoft" {
		t.Fatalf("this post's link = %q", got)
	}
	// One other article plus this account's profile. The other article is
	// not a guess once the post id is known.
	if got := accountFromLinks([]string{
		"https://blog.csdn.net/other/article/details/999",
		"https://blog.csdn.net/znsoft",
	}, tmpl, "170775764"); got != "znsoft" {
		t.Fatalf("profile beside another article = %q", got)
	}
	if got := accountFromLinks([]string{"https://blog.csdn.net/other/article/details/999"}, tmpl, "170775764"); got != "" {
		t.Fatalf("someone else's article = %q", got)
	}
}

func TestPostURLHasIDIsAPathSegment(t *testing.T) {
	id := "170775764"
	if !PostURLHasID("https://blog.csdn.net/znsoft/article/details/170775764?spm=1", id) {
		t.Fatal("article path")
	}
	if !PostURLHasID("https://blog.csdn.net/passportuser/article/details/170775764", id) {
		t.Fatal("account name passportuser")
	}
	if PostURLHasID("https://blog.csdn.net/znsoft/article/details/1707757640", id) {
		t.Fatal("longer id matched")
	}
	if PostURLHasID("https://blog.csdn.net/other/article/details/1?ref=170775764", id) {
		t.Fatal("id in the query matched")
	}
	if PostURLHasID("https://mp.csdn.net/mp_blog/creation/success/170775764", id) {
		t.Fatal("console page matched")
	}
	if linkForPost([]string{"https://blog.csdn.net/znsoft/article/details/1707757640", "https://blog.csdn.net/znsoft/article/details/170775764"}, id) != "https://blog.csdn.net/znsoft/article/details/170775764" {
		t.Fatal("linkForPost")
	}
	tmpl := "https://blog.csdn.net/{user}/article/details/{id}"
	if acceptResultURL(tmpl, id, "https://blog.csdn.net/znsoft") {
		t.Fatal("profile link accepted as the article")
	}
	if !acceptResultURL(tmpl, id, "https://blog.csdn.net/znsoft/article/details/170775764") {
		t.Fatal("article link rejected")
	}
	if !acceptResultURL("", "", "https://zhuanlan.zhihu.com/p/1") {
		t.Fatal("a site without an id template rejected its link")
	}
	if acceptResultURL(tmpl, "", "https://blog.csdn.net/znsoft") {
		t.Fatal("profile link accepted before the id is known")
	}
	if !acceptResultURL(tmpl, "", "https://blog.csdn.net/znsoft/article/details/170775764") {
		t.Fatal("article-shaped link rejected before the id is known")
	}
	article := "https://blog.csdn.net/znsoft/article/details/170775764"
	links := []string{"https://blog.csdn.net/znsoft", article}
	if got := pickResultURL(tmpl, id, true, "https://mp.csdn.net/mp_blog/manage/article", links[0], links); got != article {
		t.Fatalf("avatar hid the article: %q", got)
	}
	if got := pickResultURL(tmpl, id, true, "https://mp.csdn.net/mp_blog/manage/article", links[0], nil); got != "" {
		t.Fatalf("avatar stored while the article link was absent: %q", got)
	}
	home := "https://blog.csdn.net/znsoft"
	if got := resolvePostLink(tmpl, id, home, "znsoft"); got != article {
		t.Fatalf("homepage from the submit answer kept: %q", got)
	}
	if got := resolvePostLink(tmpl, id, article, "other"); got != article {
		t.Fatalf("article link replaced: %q", got)
	}
	if got := resolvePostLink(tmpl, id, home, ""); got != "" {
		t.Fatalf("homepage kept without an account: %q", got)
	}
}

func TestRestorePublishIdentityKeepsThePublicLink(t *testing.T) {
	p := ProfileForURL("https://editor.csdn.net/md/")
	if p == nil || !strings.Contains(p.PostURL, "{user}") || p.UserCookie != "UserName" {
		t.Fatalf("csdn profile %+v", p)
	}
	var spec PublishSpec
	poisoned := *p
	poisoned.VerifyURL = "https://blog.csdn.net/"
	poisoned.SuccessURL = `^https://mp\.csdn\.net/mp_blog/creation/success/\d+`
	poisoned.PostURL = ""
	poisoned.UserCookie = ""
	spec.VerifyURL = "https://blog.csdn.net/"
	poisoned.Apply(&spec)
	p.Apply(&spec)
	RestorePublishIdentity(p, &spec)
	if spec.VerifyURL != p.VerifyURL {
		t.Fatalf("verify url %q", spec.VerifyURL)
	}
	if SuccessPatternCaptures(spec.SuccessURL) < 1 || spec.SuccessURL != p.SuccessURL {
		t.Fatalf("success url %q", spec.SuccessURL)
	}
	if spec.PostURL != p.PostURL || spec.UserCookie != "UserName" {
		t.Fatalf("identity %+v", spec)
	}
	if PublishURLIsBareOrigin("https://mp.csdn.net/mp_blog/manage/article") || !PublishURLIsBareOrigin("https://blog.csdn.net/") || !PublishURLIsBareOrigin("https://blog.csdn.net/?spm=1") {
		t.Fatal("bare origin")
	}
}
