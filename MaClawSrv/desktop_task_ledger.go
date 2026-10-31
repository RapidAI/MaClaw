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
	Kind    string    `json:"kind"` // posted | publish_takeover | handback | checked | run_end
	Host    string    `json:"host,omitempty"`
	Title   string    `json:"title,omitempty"`
	URL     string    `json:"url,omitempty"`
	Run     string    `json:"run,omitempty"`
	Detail  string    `json:"detail,omitempty"`
	Found   bool      `json:"found,omitempty"`
	At      time.Time `json:"at"`
}

type desktopLedgerFile struct {
	Entries []desktopLedgerEntry `json:"entries"`
}

const (
	desktopLedgerKeep    = 7 * 24 * time.Hour
	desktopLedgerMax     = 300
	desktopRepostWindow  = 24 * time.Hour
	desktopPendingWindow = 6 * time.Hour
)

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
	if e.At.IsZero() {
		e.At = time.Now()
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
	now := time.Now()
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
	return out
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
	now := time.Now()
	if !desktopArgBool(args, "republish") {
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
	if spec.VerifyURL == "" && spec.VerifyAPI == "" {
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
	if err != nil && pending {
		return "", true, fmt.Errorf("publish refused: could not read the post list to rule out a duplicate (%v); nothing was filled or posted", err)
	}
	return "", false, nil
}

// desktopLedgerTakeoverRefusal: no second "please press publish" while the
// first is unsettled or the post already went out.
func desktopLedgerTakeoverRefusal(scope agentruntime.Scope) string {
	entries := desktopLedgerEntries(desktopLedgerKey(scope))
	now := time.Now()
	host := desktopLedgerHost(desktopTaskSpecFor(scope.RunID).EditorURL)
	if e, ok := desktopLedgerPendingCheck(entries, host, now); ok && e.Kind == "publish_takeover" && e.Run != scope.RunID {
		return "request_takeover refused: the person was already asked to publish at " + e.At.Format("01-02 15:04") + " and it is not known whether they did. Check the post list first (publish with check_posted=true); never ask them to publish twice."
	}
	return ""
}
