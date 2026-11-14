package browser

// The site's own success page (a profile's success_url, e.g. CSDN's
// mp.csdn.net/mp_blog/creation/success/<id>) proves the post went out; the
// public link comes from the page's own link to the post, else from the
// profile's post_url template ({id} from the success URL, {user} from the
// account cookie). A success page that only says it is under review keeps
// that in the report (Pending) so the reply does not call it public.

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var successDigitsRe = regexp.MustCompile(`\d{5,}`)

// successPostID: the success_url's first capture group, else the URL's
// last long number.
func successPostID(successURL, u string) string {
	if re, err := regexp.Compile(successURL); err == nil {
		if m := re.FindStringSubmatch(u); len(m) > 1 && m[1] != "" {
			return m[1]
		}
	}
	path := strings.SplitN(u, "?", 2)[0]
	all := successDigitsRe.FindAllString(path, -1)
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1]
}

// waitSuccessURL polls the tab for up to d for a success_url page the run
// had not seen before its click ("" when none came).
func (p *publishRun) waitSuccessURL(d time.Duration) string {
	if p.spec.SuccessURL == "" {
		return ""
	}
	re, err := regexp.Compile(p.spec.SuccessURL)
	if err != nil {
		return ""
	}
	end := time.Now().Add(d)
	for {
		if u := p.pageURL(); u != "" && !p.seen[u] && re.MatchString(u) {
			return u
		}
		if time.Now().After(end) {
			return ""
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// successPagePost reads the success page u: post id, public link, review
// state. ok=false when no public link could be resolved (the caller then
// reads the account's post list for it).
func (p *publishRun) successPagePost(u string) (string, bool) {
	id := successPostID(p.spec.SuccessURL, u)
	if id == "" {
		return "", false
	}
	p.rep.PostID = id
	link := linkForPost(p.pageLinks(), id)
	how := "the page's link"
	if link == "" && p.spec.PostURL != "" {
		link = PostURLFrom(p.spec.PostURL, id, p.accountName(u))
		how = "post_url"
	}
	if link == "" {
		return "", false
	}
	p.rep.ResultURL = strings.SplitN(link, "#", 2)[0]
	txt, _ := p.evalWithin("document.body ? document.body.innerText.slice(0, 4000) : ''", 3*time.Second)
	if m := saysAny(txt+" "+p.toastSeen, p.spec.PendingTexts); m != "" {
		p.rep.Pending = m
	}
	out := fmt.Sprintf("the page is the site's success page %s for post %s; public link %s (%s)", u, id, p.rep.ResultURL, how)
	if p.rep.Pending != "" {
		out += "; the site says " + p.rep.Pending + " (under review, may not be public yet)"
	}
	return out, true
}

// publishConsoleLink matches a creator console or success page. It is anchored
// to those hosts and paths so an article whose account name contains
// "editor" or "passport" still counts as public.
var publishConsoleLink = regexp.MustCompile(`(?i)(?:^https?://(?:mp\.csdn\.net|editor\.csdn\.net|creator\.xiaohongshu\.com|passport\.[^/]+)|/mp_blog/|/creation/(?:success|editor)|zhuanlan\.zhihu\.com/write(?:/|$))`)

// PublicPostURL keeps an http(s) article URL and drops creator-console
// pages. "" when raw is empty or is a console page.
func PublicPostURL(raw string) string { return publishPublicLink(raw) }

// publishPublicLink keeps an http(s) article URL and drops creator-console
// pages (CSDN's /mp_blog/creation/success/<id> is one of those).
func publishPublicLink(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return ""
	}
	if publishConsoleLink.MatchString(strings.SplitN(raw, "?", 2)[0]) {
		return ""
	}
	return strings.SplitN(raw, "#", 2)[0]
}

// PostURLHasID reports that raw is a public article URL whose path has id
// as its own segment. A longer id, or the same digits in a query, does not
// count. A creator-console page does not count either.
func PostURLHasID(raw, id string) bool {
	pub := publishPublicLink(raw)
	id = strings.TrimSpace(id)
	if pub == "" || id == "" {
		return false
	}
	u, err := url.Parse(pub)
	if err != nil {
		return false
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == id {
			return true
		}
	}
	return false
}

// acceptResultURL: a list or landing page may link to a tag or a profile
// next to the title. When the template includes {id}, the link has to name
// that id once it is known, and it has to be at least as specific as the
// template. A profile or a homepage is shorter than
// /{user}/article/details/{id}, so it is not the article.
func acceptResultURL(postURL, postID, result string) bool {
	if result == "" {
		return false
	}
	if !strings.Contains(postURL, "{id}") {
		return true
	}
	if postID != "" && !PostURLHasID(result, postID) {
		return false
	}
	return pathSegments(result) >= pathSegments(postURL)
}

func pathSegments(raw string) int {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return 0
	}
	n := 0
	for _, seg := range strings.Split(u.Path, "/") {
		if seg != "" {
			n++
		}
	}
	return n
}

// resolvePostLink keeps a response or list link that is this article, and
// otherwise builds one from the template. A submit answer often carries the
// account homepage in a url field next to the new id; that homepage is not
// the post.
func resolvePostLink(postURL, id, link, user string) string {
	link = publishPublicLink(link)
	if acceptResultURL(postURL, id, link) {
		return link
	}
	return publishPublicLink(PostURLFrom(postURL, id, user))
}

// pickResultURL chooses the public article link on a page that shows the
// title. links are the page's anchors, so an avatar that sits before the
// title does not hide the article once its id is known. anchor is the
// title's own link, or the first link in a block that merely contains the
// title. The page URL itself is the post only for a non-list landing page;
// a list still tries it last, and a console page fails the public-link check.
func pickResultURL(postURL, postID string, listed bool, pageURL, anchor string, links []string) string {
	if link := linkForPost(links, postID); link != "" {
		return link
	}
	cands := []string{}
	if listed {
		cands = append(cands, anchor)
	}
	cands = append(cands, pageURL)
	for _, c := range cands {
		c = publishPublicLink(c)
		if acceptResultURL(postURL, postID, c) {
			return c
		}
	}
	return ""
}

// linkForPost is the page's own public link for this post.
func linkForPost(links []string, id string) string {
	for _, raw := range links {
		if PostURLHasID(raw, id) {
			return publishPublicLink(raw)
		}
	}
	return ""
}

// accountName is the signed-in name the public URL template needs. extra
// is the page already open. The cookie is often host-only on another host
// of the same site (CSDN's UserName lives on passport/www, not on
// editor.csdn.net or the success page), so a per-URL getCookies plus
// document.cookie misses it. The jar for the whole site is tried next,
// then a profile link on the page (https://blog.csdn.net/<account>).
func (p *publishRun) accountName(extra ...string) string {
	if p.spec.UserCookie == "" {
		return ""
	}
	if p.rep != nil && p.rep.Account != "" {
		return p.rep.Account
	}
	seen := map[string]bool{}
	pages := append(append([]string{}, extra...), p.spec.EditorURL)
	var tried []string
	for _, page := range pages {
		page = strings.TrimSpace(page)
		if page == "" || seen[page] {
			continue
		}
		seen[page] = true
		tried = append(tried, page)
	}
	// The whole-site jar first: a host-only cookie on passport.csdn.net is
	// the account, and it is not sent to the editor or the success page.
	if v := p.accountFromAllCookies(tried); v != "" {
		return p.rememberAccount(v)
	}
	for _, page := range tried {
		if v := cookieAccount(p.cookieJar(page, p.spec.UserCookie)); v != "" {
			return p.rememberAccount(v)
		}
	}
	if v := cookieAccount(p.cookieDocument(p.spec.UserCookie)); v != "" {
		return p.rememberAccount(v)
	}
	if v := p.accountFromPageLinks(); v != "" {
		return p.rememberAccount(v)
	}
	return ""
}

func (p *publishRun) rememberAccount(v string) string {
	if p.rep != nil {
		p.rep.Account = v
	}
	return v
}

// accountFromAllCookies reads Network.getAllCookies and keeps a cookie of
// the configured name whose domain belongs to this site. getCookies(page)
// only returns cookies that would be sent to that exact URL.
func (p *publishRun) accountFromAllCookies(pages []string) string {
	raw, err := p.send("Network.getAllCookies", map[string]interface{}{})
	if err != nil || len(raw) == 0 {
		return ""
	}
	var out struct {
		Cookies []struct {
			Name   string `json:"name"`
			Value  string `json:"value"`
			Domain string `json:"domain"`
		} `json:"cookies"`
	}
	if json.Unmarshal(raw, &out) != nil {
		return ""
	}
	cookies := make([]namedCookie, 0, len(out.Cookies))
	for _, c := range out.Cookies {
		cookies = append(cookies, namedCookie{Name: c.Name, Value: c.Value, Domain: c.Domain})
	}
	hosts := make([]string, 0, len(pages)+2)
	for _, page := range pages {
		if u, err := url.Parse(page); err == nil && u.Hostname() != "" {
			hosts = append(hosts, u.Hostname())
		}
	}
	for _, raw := range []string{p.spec.PostURL, p.spec.EditorURL} {
		if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
			hosts = append(hosts, u.Hostname())
		}
	}
	return accountFromCookies(cookies, p.spec.UserCookie, hosts)
}

const publishProfileLinksJS = `(() => JSON.stringify([...document.querySelectorAll('a[href]')].map(a => a.href).slice(0, 400)))`

func (p *publishRun) pageLinks() []string {
	here := p.pageURL()
	if p.pageLinksOK && p.pageLinksAt == here {
		return p.pageLinkList
	}
	raw, err := p.evalWithin(publishProfileLinksJS, 3*time.Second)
	if err != nil {
		return nil
	}
	var links []string
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &links) != nil {
		return nil
	}
	p.pageLinksAt = here
	p.pageLinksOK = true
	p.pageLinkList = links
	return links
}

// pageLinksNow reads the anchors again. A list page renders its article
// links after the first scrape, and the cache is keyed only by the URL.
func (p *publishRun) pageLinksNow() []string {
	p.pageLinksOK = false
	return p.pageLinks()
}

func (p *publishRun) accountFromPageLinks() string {
	if !strings.Contains(p.spec.PostURL, "{user}") {
		return ""
	}
	id := ""
	if p.rep != nil {
		id = p.rep.PostID
	}
	return accountFromLinks(p.pageLinks(), p.spec.PostURL, id)
}

// namedCookie is the part of a browser cookie the account lookup needs.
type namedCookie struct {
	Name   string
	Value  string
	Domain string
}

// accountFromCookies returns the account name stored under name on this
// site. hosts are the editor, the success page and the public post host;
// a cookie on any host of that site counts (passport.csdn.net's UserName
// is the blog.csdn.net account). A cookie whose domain is the site itself
// (.csdn.net) wins over one set on a single subdomain.
func accountFromCookies(cookies []namedCookie, name string, hosts []string) string {
	name = strings.TrimSpace(name)
	roots := map[string]bool{}
	for _, h := range hosts {
		if r := siteRoot(h); r != "" {
			roots[r] = true
		}
	}
	if name == "" || len(roots) == 0 {
		return ""
	}
	// One distinct value per tier. Two different UserName cookies on the
	// same site are not a guess (a tracking subdomain must not beat the
	// account, and must not be picked when the account cookie is absent).
	siteWide, covered, other := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	for _, c := range cookies {
		if !strings.EqualFold(strings.TrimSpace(c.Name), name) {
			continue
		}
		v := cookieAccount(c.Value)
		if v == "" || !roots[siteRoot(c.Domain)] {
			continue
		}
		d := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(c.Domain)), ".")
		switch {
		case roots[d]:
			siteWide[v] = struct{}{}
		case cookieCovers(d, hosts):
			covered[v] = struct{}{}
		default:
			other[v] = struct{}{}
		}
	}
	for _, set := range []map[string]struct{}{siteWide, covered, other} {
		if len(set) == 1 {
			for v := range set {
				return v
			}
		}
	}
	return ""
}

func cookieCovers(domain string, hosts []string) bool {
	domain = strings.TrimPrefix(strings.ToLower(domain), ".")
	if domain == "" {
		return false
	}
	for _, h := range hosts {
		h = strings.ToLower(h)
		if h == domain || strings.HasSuffix(h, "."+domain) {
			return true
		}
	}
	return false
}

// siteRoot is the registrable-ish suffix shared by a site's hosts
// (mp.csdn.net and passport.csdn.net are both csdn.net). An IP is its own
// site.
func siteRoot(host string) string {
	host = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return host
	}
	return parts[len(parts)-2] + "." + parts[len(parts)-1]
}

// cookieAccount is a cookie value that can stand in a public URL path.
func cookieAccount(v string) string {
	v = strings.TrimSpace(v)
	if u, err := url.QueryUnescape(v); err == nil {
		v = strings.TrimSpace(u)
	}
	if v == "" || len(v) > 64 || strings.ContainsAny(v, "/?# \t{}") {
		return ""
	}
	return v
}

// accountFromLinks reads {user} out of links shaped like the post_url
// template. An article link (…/article/details/<id>) wins. A single profile
// link (https://blog.csdn.net/<account>) is used when that is the only
// one; two different profiles are not a guess. Site sections such as /nav
// are not accounts.
func accountFromLinks(links []string, tmpl, id string) string {
	u, err := url.Parse(strings.TrimSpace(tmpl))
	if err != nil || u.Hostname() == "" || !strings.Contains(u.Path, "{user}") {
		return ""
	}
	wantHost := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	userAt := -1
	for i, part := range parts {
		if part == "{user}" {
			userAt = i
			break
		}
	}
	if userAt < 0 {
		return ""
	}
	var profiles []string
	seen := map[string]bool{}
	articles := map[string]struct{}{}
	onlyArticle := ""
	for _, raw := range links {
		user, full, linkID := userInPostLink(raw, wantHost, parts, userAt)
		if user == "" {
			continue
		}
		if full {
			// Known id: only the link for this post. A single other
			// article on the page is not this account.
			if id != "" {
				if linkID == id {
					return user
				}
				continue
			}
			articles[user] = struct{}{}
			onlyArticle = user
			continue
		}
		if !seen[user] {
			seen[user] = true
			profiles = append(profiles, user)
		}
	}
	if len(articles) == 1 {
		return onlyArticle
	}
	if len(articles) == 0 && len(profiles) == 1 {
		return profiles[0]
	}
	return ""
}

func userInPostLink(raw, wantHost string, parts []string, userAt int) (user string, article bool, id string) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", false, ""
	}
	if strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.") != wantHost {
		return "", false, ""
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if userAt >= len(segs) {
		return "", false, ""
	}
	name := segs[userAt]
	if cookieAccount(name) == "" {
		return "", false, ""
	}
	if len(segs) == len(parts) {
		gotID := ""
		for i, part := range parts {
			switch part {
			case "{user}":
				continue
			case "{id}":
				if segs[i] == "" {
					return "", false, ""
				}
				gotID = segs[i]
			default:
				if !strings.EqualFold(segs[i], part) {
					return "", false, ""
				}
			}
		}
		return name, true, gotID
	}
	if len(segs) == userAt+1 && plausibleProfile(name) {
		return name, false, ""
	}
	return "", false, ""
}

func plausibleProfile(name string) bool {
	if cookieAccount(name) == "" {
		return false
	}
	digits := true
	for _, r := range name {
		if r < '0' || r > '9' {
			digits = false
			break
		}
	}
	if digits {
		return false
	}
	switch strings.ToLower(name) {
	case "article", "articles", "nav", "rank", "category", "tag", "tags", "search", "download", "resources", "ask", "edu", "vip", "help", "about", "login", "passport", "mp", "editor", "watchers", "collect", "comment", "phoenix", "link", "company", "blog":
		return false
	}
	return true
}

// cookieJar reads the browser cookie store for pageURL, including HttpOnly
// cookies that would be sent there. It does not read document.cookie.
func (p *publishRun) cookieJar(pageURL, name string) string {
	if name == "" || strings.TrimSpace(pageURL) == "" {
		return ""
	}
	raw, err := p.send("Network.getCookies", map[string]interface{}{"urls": []string{pageURL}})
	if err != nil {
		return ""
	}
	var out struct {
		Cookies []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"cookies"`
	}
	if json.Unmarshal(raw, &out) != nil {
		return ""
	}
	for _, c := range out.Cookies {
		if c.Name == name && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

// cookieDocument reads the open page's document.cookie. HttpOnly cookies
// are absent, and the value belongs to the page that is open, not to the
// URL passed to the jar.
func (p *publishRun) cookieDocument(name string) string {
	if name == "" {
		return ""
	}
	v, _ := p.evalWithin(`((n) => { const m = document.cookie.match(new RegExp('(?:^|;\\s*)' + n + '=([^;]*)')); return m ? decodeURIComponent(m[1]) : ''; })(`+jsq(name)+`)`, 3*time.Second)
	return strings.TrimSpace(v)
}
