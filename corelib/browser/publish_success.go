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

// publishPostLinkJS finds the page's link to the post with that id (not
// an editor or console page).
const publishPostLinkJS = `((id) => { const bad = /edit|creation|manage|console|mp_blog|login|passport/i; const a = [...document.querySelectorAll('a[href]')].map(a => a.href).find(h => h.includes(id) && h !== location.href && /^https?:/.test(h) && !bad.test(h)); return a || ''; })`

// successPagePost reads the success page u: post id, public link, review
// state. ok=false when no public link could be resolved (the caller then
// reads the account's post list for it).
func (p *publishRun) successPagePost(u string) (string, bool) {
	id := successPostID(p.spec.SuccessURL, u)
	if id == "" {
		return "", false
	}
	link, _ := p.evalWithin(publishPostLinkJS+"("+jsq(id)+")", 3*time.Second)
	link = publishPublicLink(link)
	how := "the page's link"
	if link == "" && p.spec.PostURL != "" {
		link = PostURLFrom(p.spec.PostURL, id, p.accountName(u))
		how = "post_url"
	}
	if link == "" {
		return "", false
	}
	p.rep.PostID = id
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

// accountName is the signed-in name the public URL template needs. extra
// is the page already open. The cookie is often set on the editor host and
// is not sent to the success page on another host, so the editor is tried
// as well.
func (p *publishRun) accountName(extra ...string) string {
	if p.spec.UserCookie == "" {
		return ""
	}
	seen := map[string]bool{}
	pages := append(append([]string{}, extra...), p.spec.EditorURL)
	for _, page := range pages {
		page = strings.TrimSpace(page)
		if page == "" || seen[page] {
			continue
		}
		seen[page] = true
		if v := p.cookie(page, p.spec.UserCookie); v != "" {
			return v
		}
	}
	return ""
}

// cookie reads a cookie of the page's site by name: the browser's cookie
// store first (also HttpOnly ones), then document.cookie.
func (p *publishRun) cookie(pageURL, name string) string {
	if name == "" {
		return ""
	}
	if raw, err := p.send("Network.getCookies", map[string]interface{}{"urls": []string{pageURL}}); err == nil {
		var out struct {
			Cookies []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"cookies"`
		}
		if json.Unmarshal(raw, &out) == nil {
			for _, c := range out.Cookies {
				if c.Name == name && c.Value != "" {
					if v, err := url.QueryUnescape(c.Value); err == nil {
						return v
					}
					return c.Value
				}
			}
		}
	}
	v, _ := p.evalWithin(`((n) => { const m = document.cookie.match(new RegExp('(?:^|;\\s*)' + n + '=([^;]*)')); return m ? decodeURIComponent(m[1]) : ''; })(`+jsq(name)+`)`, 3*time.Second)
	return strings.TrimSpace(v)
}
