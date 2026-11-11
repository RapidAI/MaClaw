package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// desktopTaskLedger is the task state that must outlive one run, one Hub
// resume, a desktop container restart and a MaClawSrv restart: what this
// desktop already posted, when the person was handed a filled editor to
// publish, what came back. It lives in MaClawSrv's data root, outside the
// desktop container, so the container is only the execution environment
// (Super-agent prototype: compute, state and execution kept apart).
//
// Run-scoped records (desktopPublishRecord) end with their run; a resume
// after a hand-back is a new run and used to start blind (run_a34d625d
// refilled a post the person had already published and asked to publish
// again). Every publish path reads this ledger first.
type desktopLedgerEntry struct {
	Kind   string    `json:"kind"` // posted | publish_takeover | handback | checked | run_end | page_state | draft | login_state
	Host   string    `json:"host,omitempty"`
	Title  string    `json:"title,omitempty"`
	URL    string    `json:"url,omitempty"`
	Run    string    `json:"run,omitempty"`
	Detail string    `json:"detail,omitempty"`
	Body   string    `json:"body,omitempty"`  // draft / form text kept outside the container
	Field  string    `json:"field,omitempty"` // stable locator of the form field a draft was typed into
	Found  bool      `json:"found,omitempty"`
	At     time.Time `json:"at"`
}

type desktopLedgerFile struct {
	Entries []desktopLedgerEntry `json:"entries"`
}

const (
	desktopLedgerKeep      = 7 * 24 * time.Hour
	desktopLedgerMax       = 300
	desktopRepostWindow    = 24 * time.Hour
	desktopPendingWindow   = 6 * time.Hour
	desktopPageStateWindow = 6 * time.Hour
	desktopDraftBodyMax    = 2000
)

// desktopLedgerNow is overridable in tests.
var desktopLedgerNow = time.Now

var desktopLedgerMu sync.Mutex

// desktopLedgerDir is overridable in tests.
var desktopLedgerDir = func() string { return filepath.Join(defaultDataRoot(), "desktop-ledger") }

func desktopLedgerKey(scope agentruntime.Scope) string {
	key, err := desktopUserKey(desktopOwner(scope))
	if err != nil {
		return ""
	}
	return key
}

func desktopLedgerPath(key string) string {
	sum := sha1.Sum([]byte(key))
	return filepath.Join(desktopLedgerDir(), hex.EncodeToString(sum[:10])+".json")
}

func desktopLedgerLoadLocked(key string) desktopLedgerFile {
	var f desktopLedgerFile
	raw, err := os.ReadFile(desktopLedgerPath(key))
	if err == nil {
		_ = json.Unmarshal(raw, &f)
	}
	return f
}

func desktopLedgerAdd(key string, e desktopLedgerEntry) {
	if strings.TrimSpace(key) == "" {
		return
	}
	if e.Run != "" {
		desktopLedgerRunKeys.Store(e.Run, key)
	}
	if e.At.IsZero() {
		e.At = desktopLedgerNow()
	}
	desktopLedgerMu.Lock()
	defer desktopLedgerMu.Unlock()
	f := desktopLedgerLoadLocked(key)
	keep := f.Entries[:0]
	for _, x := range f.Entries {
		if time.Since(x.At) < desktopLedgerKeep {
			keep = append(keep, x)
		}
	}
	keep = append(keep, e)
	if len(keep) > desktopLedgerMax {
		keep = keep[len(keep)-desktopLedgerMax:]
	}
	f.Entries = keep
	raw, err := json.Marshal(f)
	if err != nil {
		return
	}
	path := desktopLedgerPath(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}

func desktopLedgerEntries(key string) []desktopLedgerEntry {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	desktopLedgerMu.Lock()
	defer desktopLedgerMu.Unlock()
	return desktopLedgerLoadLocked(key).Entries
}

func desktopLedgerHost(u string) string {
	u = strings.TrimSpace(u)
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	if i := strings.IndexAny(u, "/?#"); i >= 0 {
		u = u[:i]
	}
	return strings.TrimPrefix(strings.ToLower(u), "www.")
}

func desktopSameTitle(a, b string) bool {
	n := func(s string) string { return strings.Join(strings.Fields(s), "") }
	a, b = n(a), n(b)
	return a != "" && a == b
}

// desktopLedgerRecentPost is a post this desktop made on host with this
// title inside the repost window (title "" matches any post on host).
func desktopLedgerRecentPost(entries []desktopLedgerEntry, host, title string, now time.Time) (desktopLedgerEntry, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Kind != "posted" || now.Sub(e.At) > desktopRepostWindow {
			continue
		}
		if host != "" && e.Host != "" && e.Host != host {
			continue
		}
		if desktopSameTitle(e.Title, title) {
			return e, true
		}
	}
	return desktopLedgerEntry{}, false
}

// desktopLedgerPendingCheck: the person was handed a filled editor to
// publish, and nothing since (a post, a list check) says whether they did.
// Until a check settles it, a second publish would risk a duplicate post.
func desktopLedgerPendingCheck(entries []desktopLedgerEntry, host string, now time.Time) (desktopLedgerEntry, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if now.Sub(e.At) > desktopPendingWindow {
			return desktopLedgerEntry{}, false
		}
		if host != "" && e.Host != "" && e.Host != host {
			continue
		}
		switch e.Kind {
		case "posted":
			if e.Title == "" {
				// Seen posted after a takeover, title unknown: which
				// article it was still needs a check.
				return e, true
			}
			return desktopLedgerEntry{}, false
		case "checked":
			return desktopLedgerEntry{}, false
		case "publish_takeover":
			return e, true
		}
	}
	return desktopLedgerEntry{}, false
}

// desktopLedgerPromptText is what a new run (a resume included) reads about
// this desktop before it acts: recent posts and an unsettled publish.
func desktopLedgerPromptText(scope agentruntime.Scope) string {
	entries := desktopLedgerEntries(desktopLedgerKey(scope))
	if len(entries) == 0 {
		return ""
	}
	now := desktopLedgerNow()
	var lines []string
	for _, e := range entries {
		if e.Kind == "posted" && now.Sub(e.At) <= desktopRepostWindow {
			l := "posted on " + e.Host + " at " + e.At.Format("01-02 15:04")
			if e.Title != "" {
				l += " title 「" + e.Title + "」"
			}
			if e.URL != "" {
				l += " link " + e.URL
			}
			lines = append(lines, l)
		}
	}
	if len(lines) > 5 {
		lines = lines[len(lines)-5:]
	}
	out := ""
	if len(lines) > 0 {
		out = "TASK LEDGER (this desktop, kept outside it): " + strings.Join(lines, "; ") + ". A post listed here is out: never publish it again; report its link. "
	}
	if p, ok := desktopLedgerPendingCheck(entries, "", now); ok {
		out += "TASK LEDGER: at " + p.At.Format("15:04") + " the person was handed the filled editor on " + p.Host + " to publish, and it is not known whether they did. Before any publish step call action=publish with check_posted=true and the same title: it reads the account's post list and writes nothing. "
	}
	out += desktopLedgerProgressPrompt(entries, now)
	return out
}

// desktopLedgerProgressPrompt surfaces the latest page/draft/login state that
// outlives a container restart or hand-back, so a resume does not start blind.
func desktopLedgerProgressPrompt(entries []desktopLedgerEntry, now time.Time) string {
	var draft, page, login desktopLedgerEntry
	var haveDraft, havePage, haveLogin bool
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if now.Sub(e.At) > desktopPageStateWindow {
			break
		}
		switch e.Kind {
		case "draft":
			if !haveDraft {
				draft, haveDraft = e, true
			}
		case "page_state":
			if !havePage {
				page, havePage = e, true
			}
		case "login_state":
			if !haveLogin {
				login, haveLogin = e, true
			}
		}
		if haveDraft && havePage && haveLogin {
			break
		}
	}
	var parts []string
	if havePage {
		l := "PAGE STATE (kept outside the desktop, survives restart/hand-back): last URL " + page.URL
		if page.Title != "" {
			l += " title 「" + page.Title + "」"
		}
		if page.Body != "" {
			l += " form/editor text starts 「" + desktopLedgerBodyPreview(page.Body) + "」"
		}
		l += ". Resume from this page; do not clear a filled editor."
		parts = append(parts, l)
	}
	if haveDraft {
		l := "DRAFT (kept outside the desktop)"
		if draft.Host != "" {
			l += " for " + draft.Host
		}
		if draft.Title != "" {
			l += " title 「" + draft.Title + "」"
		}
		if draft.Body != "" {
			l += " body starts 「" + desktopLedgerBodyPreview(draft.Body) + "」"
		}
		l += ". Prefer this text over inventing a new draft; nothing was published from it alone."
		parts = append(parts, l)
	}
	if haveLogin && login.Detail != "" && login.Detail != "ok" {
		parts = append(parts, "LOGIN STATE (kept outside the desktop): "+login.Detail+" on "+login.Host+". Do not fill until signed in.")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ") + " "
}

func desktopLedgerBodyPreview(body string) string {
	body = strings.Join(strings.Fields(body), " ")
	runes := []rune(body)
	if len(runes) > 120 {
		return string(runes[:120]) + "…"
	}
	return body
}

func desktopLedgerCapBody(body string) string {
	body = strings.TrimSpace(body)
	runes := []rune(body)
	if len(runes) > desktopDraftBodyMax {
		return string(runes[:desktopDraftBodyMax])
	}
	return body
}

// desktopLedgerRememberProgress writes page/draft state under dataRoot so a
// hand-back, offline stretch, or container restart does not wipe progress.
// kind is "page_state" or "draft" (empty defaults from which fields are set).
func desktopLedgerRememberProgress(scope agentruntime.Scope, url, title, body, kind string) {
	key := desktopLedgerKey(scope)
	if key == "" {
		return
	}
	url = strings.TrimSpace(url)
	title = strings.TrimSpace(title)
	body = desktopLedgerCapBody(body)
	kind = strings.TrimSpace(kind)
	if kind == "" {
		if body != "" || title != "" {
			kind = "draft"
		} else if url != "" {
			kind = "page_state"
		} else {
			return
		}
	}
	if url == "" && title == "" && body == "" {
		return
	}
	host := desktopLedgerHost(firstNonEmptyStr(url, desktopTaskSpecFor(scope.RunID).EditorURL))
	desktopLedgerAdd(key, desktopLedgerEntry{
		Kind:   kind,
		Host:   host,
		Title:  title,
		URL:    url,
		Body:   body,
		Run:    scope.RunID,
		Detail: kind,
	})
}

// desktopPublishLedgerGuard runs before any publish fills a form. A post of
// the same title already on record is returned instead of posting again; an
// unsettled hand-off (the person was asked to press publish, or a post was
// seen without its title) is settled by a read-only look at the post list
// first. args republish=true (the person explicitly asked to post it again)
// skips the record, never the check.
func desktopPublishLedgerGuard(scope agentruntime.Scope, session *browser.BrowserAgentSession, spec browser.PublishSpec, args map[string]any) (string, bool, error) {
	key := desktopLedgerKey(scope)
	entries := desktopLedgerEntries(key)
	host := desktopLedgerHost(firstNonEmptyStr(spec.EditorURL, session.CurrentURL()))
	now := desktopLedgerNow()
	if att, th, ok := desktopRecPendingRecheck(scope); ok {
		return desktopPublishStoppedRecheck(scope, session, spec, args, entries, host, now, att, th)
	}
	if !desktopArgBool(args, "republish") {
		// A submit that was never confirmed is not proof the post is up
		// (run_6fa834e0: CSDN refused the near-empty post, the ledger said
		// submitted_unconfirmed and would have blocked the real one): the
		// account's post list decides.
		if e, ok := desktopLedgerRecentPost(entries, host, spec.Title, now); ok && e.Detail == string(browser.PublishSubmitted) && (spec.VerifyURL != "" || spec.VerifyAPI != "") {
			rep, err := browser.FindPosted(session.PublishDriver(), spec)
			if err != nil {
				return "", true, fmt.Errorf("publish refused: an earlier submit of 「%s」 was not confirmed and the post list could not be read to rule out a duplicate (%v); nothing was filled or posted", spec.Title, err)
			}
			found := rep != nil && rep.State == browser.PublishLive
			link := ""
			if rep != nil {
				link = rep.ResultURL
			}
			desktopLedgerAdd(key, desktopLedgerEntry{Kind: "checked", Host: host, Title: spec.Title, URL: link, Run: scope.RunID, Found: found, Detail: "unconfirmed_recheck"})
			if found {
				desktopLedgerAdd(key, desktopLedgerEntry{Kind: "posted", Host: host, Title: spec.Title, URL: link, Run: scope.RunID, Detail: "found_in_list"})
				return "publish skipped: 「" + spec.Title + "」 is already in the account's post list, link " + link + ". Nothing was filled or posted. Report that it is live.", true, nil
			}
			return "", false, nil
		}
		if e, ok := desktopLedgerRecentPost(entries, host, spec.Title, now); ok {
			out := "publish skipped: this desktop already posted 「" + e.Title + "」 on " + e.Host + " at " + e.At.Format("01-02 15:04") + " (task ledger)"
			if e.URL != "" {
				out += ", link " + e.URL
			}
			return out + ". Nothing was filled or posted. Tell the person it is already up (with the link); post again only if they explicitly ask, with republish=true.", true, nil
		}
	}
	_, pending := desktopLedgerPendingCheck(entries, host, now)
	if !pending && !desktopArgBool(args, "check_posted") {
		return "", false, nil
	}
	checkOnly := desktopArgBool(args, "check_posted")
	if spec.VerifyURL == "" && spec.VerifyAPI == "" {
		if checkOnly {
			// check_posted is read-only: without a post list to read it
			// answers, it never falls through to filling or submitting.
			return "", true, fmt.Errorf("publish check_posted is read-only and needs verify_url (the account's post list) to look for 「%s」; nothing was filled or posted", spec.Title)
		}
		if pending {
			return "", true, fmt.Errorf("publish refused: the person was handed this editor to publish and it is not known whether they did; open the account's post list, look for 「%s」, then call publish again with check_posted=true and verify_url set (nothing was filled or posted)", spec.Title)
		}
		return "", false, nil
	}
	rep, err := browser.FindPosted(session.PublishDriver(), spec)
	found := err == nil && rep != nil && rep.State == browser.PublishLive
	link := ""
	if rep != nil {
		link = rep.ResultURL
	}
	desktopLedgerAdd(key, desktopLedgerEntry{Kind: "checked", Host: host, Title: spec.Title, URL: link, Run: scope.RunID, Found: found})
	if found {
		desktopLedgerAdd(key, desktopLedgerEntry{Kind: "posted", Host: host, Title: spec.Title, URL: link, Run: scope.RunID, Detail: "found_in_list"})
		out := "publish skipped: 「" + spec.Title + "」 is already in the account's post list (it went out earlier)"
		if link != "" {
			out += ", link " + link
		}
		return out + ". Nothing was filled or posted. Report that it is live.", true, nil
	}
	if err != nil && (pending || checkOnly) {
		return "", true, fmt.Errorf("publish refused: could not read the post list to rule out a duplicate (%v); nothing was filled or posted", err)
	}
	if checkOnly {
		// Read-only, papers 11-11 run_50546ca1: a check_posted call with
		// only a title went on to fill and submit the editor.
		return "publish check_posted: 「" + spec.Title + "」 is not in the account's post list (read-only check). Nothing was filled or posted. To post it, call publish again without check_posted, with the full body and picture files.", true, nil
	}
	return "", false, nil
}

// desktopLedgerTakeoverRefusal: no second "please press publish" while the
// first is unsettled or the post already went out.
func desktopLedgerTakeoverRefusal(scope agentruntime.Scope) string {
	entries := desktopLedgerEntries(desktopLedgerKey(scope))
	now := desktopLedgerNow()
	host := desktopLedgerHost(desktopTaskSpecFor(scope.RunID).EditorURL)
	if e, ok := desktopLedgerPendingCheck(entries, host, now); ok && e.Kind == "publish_takeover" && e.Run != scope.RunID {
		return "request_takeover refused: the person was already asked to publish at " + e.At.Format("01-02 15:04") + " and it is not known whether they did. Check the post list first (publish with check_posted=true); never ask them to publish twice."
	}
	return ""
}

// desktopLedgerRunKeys: run id -> the ledger key its entries went to (this
// process), so a run's completion hook can read its posts by run id.
var desktopLedgerRunKeys sync.Map

func desktopLedgerKeysForRun(runID string) []string {
	if v, ok := desktopLedgerRunKeys.Load(runID); ok {
		return []string{v.(string)}
	}
	return nil
}

// desktopPublishStoppedRecheck: an earlier run of the same instruction
// clicked submit and never saw the result, so its goal was stopped. When
// the person asks again, the account's post list decides: the earlier post
// (by its own title, even if this call renames it) is there → report it,
// nothing is posted; it is not there → this attempt may post. Without a
// readable post list nothing is filled.
func desktopPublishStoppedRecheck(scope agentruntime.Scope, session *browser.BrowserAgentSession, spec browser.PublishSpec, args map[string]any, entries []desktopLedgerEntry, host string, now time.Time, att *desktopRecAttempt, titleHash string) (string, bool, error) {
	if spec.VerifyURL == "" && spec.VerifyAPI == "" {
		return "", true, fmt.Errorf("publish refused: an earlier run's submit on this site was never confirmed; pass verify_url (the account's post list) so it can be ruled out first (nothing was filled or posted)")
	}
	titles := []string{}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Kind != "posted" || now.Sub(e.At) > desktopRepostWindow || (host != "" && e.Host != "" && e.Host != host) {
			continue
		}
		if titleHash != "" && desktopRecHash(e.Title) == titleHash {
			titles = append(titles, e.Title)
			break
		}
	}
	if len(titles) == 0 {
		if titleHash != "" && titleHash != desktopRecHash(spec.Title) {
			return "", true, fmt.Errorf("publish refused: an earlier run's submit on this site was never confirmed and its title is not on record, so a duplicate cannot be ruled out; open the account's post list, tell the person what is there (nothing was filled or posted)")
		}
		titles = append(titles, spec.Title)
	}
	if !desktopSameTitle(titles[0], spec.Title) {
		titles = append(titles, spec.Title)
	}
	for _, t := range titles {
		look := spec
		look.Title = t
		rep, err := browser.FindPosted(session.PublishDriver(), look)
		if err != nil {
			return "", true, fmt.Errorf("publish refused: an earlier run's submit of 「%s」 was never confirmed and the post list could not be read to rule out a duplicate (%v); nothing was filled or posted", t, err)
		}
		found := rep != nil && rep.State == browser.PublishLive
		link := ""
		if rep != nil {
			link = rep.ResultURL
		}
		desktopLedgerAdd(desktopLedgerKey(scope), desktopLedgerEntry{Kind: "checked", Host: host, Title: t, URL: link, Run: scope.RunID, Found: found, Detail: "stopped_recheck"})
		if found {
			desktopLedgerAdd(desktopLedgerKey(scope), desktopLedgerEntry{Kind: "posted", Host: host, Title: t, URL: link, Run: scope.RunID, Detail: "found_in_list"})
			out := "publish skipped: the earlier submit 「" + t + "」 did go out — it is in the account's post list"
			if link != "" {
				out += ", link " + link
			}
			return out + ". Nothing was filled or posted. Report that it is live.", true, nil
		}
	}
	if desktopArgBool(args, "check_posted") {
		return "publish check_posted: the earlier unconfirmed submit 「" + titles[0] + "」 is not in the account's post list (read-only check). Nothing was filled or posted. To post it, call publish again without check_posted, with the full body and picture files.", true, nil
	}
	desktopRecStateMu.Lock()
	att.recheckOK = true
	desktopRecStateMu.Unlock()
	return "", false, nil
}
