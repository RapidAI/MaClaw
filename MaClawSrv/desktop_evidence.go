package main

import (
	"log"
	"regexp"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

// Verified publish evidence from any tool path (papers 2026-11-11
// run_5ecb9d6d). The official publish could not reach the browser, so the
// bot published through a shell and its own DevTools client; the page became
// https://mp.csdn.net/mp_blog/creation/success/170476928 and a probe read the
// public article. The final reply still said 「本次没有发布或验证」 because only
// the publish action writes the publish ledger. The site's own success page
// (a profile's success_url) is the same proof whichever step saw it: it is
// recorded as a verified post of this run, the outcome follows it, and a
// reply that denies the post gets a correction in front of it.

var desktopEvidenceURLRe = regexp.MustCompile(`https?://[^\s"'<>()\[\]{}\x60,，。；]+`)

// desktopEvidenceActions can show where a page went (a shell transcript of
// the bot's own DevTools client included).
var desktopEvidenceActions = map[string]bool{"bash": true, "probe": true, "script": true, "navigate": true, "batch": true, "wait": true, "click": true, "press": true, "task_run": true, "skill_run": true}

var desktopEvidenceSeen sync.Map // run|url -> true

// desktopEvidenceProfiles: the sites this run publishes to.
func desktopEvidenceProfiles(runID string) []*browser.SiteProfile {
	base := desktopTaskSpecBase(runID)
	if len(base.Sites) > 0 {
		return base.Sites
	}
	if base.Profile != nil {
		return []*browser.SiteProfile{base.Profile}
	}
	return nil
}

// desktopPostURLPattern turns a post_url template into a regexp for one id.
func desktopPostURLPattern(tmpl, id string) *regexp.Regexp {
	if tmpl == "" || id == "" {
		return nil
	}
	q := regexp.QuoteMeta(tmpl)
	q = strings.ReplaceAll(q, regexp.QuoteMeta("{user}"), `[^/\s?#]+`)
	q = strings.ReplaceAll(q, regexp.QuoteMeta("{id}"), regexp.QuoteMeta(id))
	re, err := regexp.Compile(q)
	if err != nil {
		return nil
	}
	return re
}

// desktopObservePublishEvidence records a success page seen in a step's
// output as this run's verified post. It returns the public link ("" when
// nothing was found).
func desktopObservePublishEvidence(scope agentruntime.Scope, action, out string) string {
	if out == "" || scope.RunID == "" || !desktopEvidenceActions[action] {
		return ""
	}
	profiles := desktopEvidenceProfiles(scope.RunID)
	if len(profiles) == 0 {
		return ""
	}
	urls := desktopEvidenceURLRe.FindAllString(out, -1)
	if len(urls) == 0 {
		return ""
	}
	for _, p := range profiles {
		if p == nil || p.SuccessURL == "" {
			continue
		}
		re, err := regexp.Compile(p.SuccessURL)
		if err != nil {
			continue
		}
		for _, u := range urls {
			m := re.FindStringSubmatch(u)
			if m == nil {
				continue
			}
			id := ""
			if len(m) > 1 {
				id = m[1]
			}
			link := ""
			if pr := desktopPostURLPattern(p.PostURL, id); pr != nil {
				for _, cand := range urls {
					if pr.MatchString(cand) {
						link = pr.FindString(cand)
						break
					}
				}
			}
			if link == "" {
				link = browser.PostURLFrom(p.PostURL, id, "")
			}
			if link == "" {
				link = u
			}
			if desktopLedgerPostedByOther(desktopLedgerKey(scope), scope.RunID, link) {
				// An earlier run's post (a restored tab, a listing): not this
				// run's evidence.
				continue
			}
			if _, dup := desktopEvidenceSeen.LoadOrStore(scope.RunID+"|"+link, true); dup {
				return link
			}
			title := strings.TrimSpace(desktopTaskTitle(scope.RunID))
			host := desktopLedgerHost(firstNonEmptyStr(p.EditorURL, u))
			desktopLedgerAdd(desktopLedgerKey(scope), desktopLedgerEntry{Kind: "posted", Host: host, Title: title, URL: link, Run: scope.RunID, Detail: "observed_success_page"})
			log.Printf("[desktop-evidence] run=%s %s success page %s seen in %s output: verified post %s", scope.RunID, p.ID, u, action, link)
			return link
		}
	}
	return ""
}

// desktopTaskTitle: the title the run published under, when one is known.
func desktopTaskTitle(runID string) string {
	if r, ok := desktopPublishRecordOf(runID); ok {
		return r.Title
	}
	return ""
}

// desktopRunVerifiedPosts: hosts and links of this run's verified posts.
func desktopRunVerifiedPosts(runID string) []desktopLedgerEntry {
	var out []desktopLedgerEntry
	seen := map[string]bool{}
	for _, key := range desktopLedgerKeysForRun(runID) {
		for _, e := range desktopLedgerEntries(key) {
			if e.Kind != "posted" || e.Run != runID || e.Detail == "submitted_unconfirmed" || e.Detail == string(browser.PublishSubmitted) || e.URL == "" || seen[e.URL] {
				continue
			}
			seen[e.URL] = true
			out = append(out, e)
		}
	}
	return out
}

// desktopProfileHasHost: the post's host belongs to the site profile.
func desktopProfileHasHost(p *browser.SiteProfile, host string) bool {
	if p == nil || host == "" {
		return false
	}
	if desktopLedgerHost(p.EditorURL) == host || siteExpKey("https://"+host+"/") == p.ID {
		return true
	}
	for _, h := range p.Hosts {
		if strings.TrimPrefix(strings.ToLower(h), "www.") == host {
			return true
		}
	}
	return false
}

// desktopReconcileOutcome lets verified posts decide the publish part of
// the outcome: every requested site posted is done; some is partial.
func desktopReconcileOutcome(runID, outcome, why string) (string, string) {
	posts := desktopRunVerifiedPosts(runID)
	if len(posts) == 0 {
		return outcome, why
	}
	sites := desktopEvidenceProfiles(runID)
	var done, missing []string
	links := make([]string, 0, len(posts))
	for _, e := range posts {
		links = append(links, e.URL)
	}
	if len(sites) == 0 {
		// The requested sites are unknown: a measured partial stays; a gap
		// that may be another site's keeps its reason next to the post.
		switch outcome {
		case "", agentservice.TaskOutcomeDone:
			return agentservice.TaskOutcomeDone, "verified post: " + strings.Join(links, " ")
		case agentservice.TaskOutcomePartial:
			return outcome, why
		}
		return agentservice.TaskOutcomePartial, "verified post: " + strings.Join(links, " ") + "；" + why
	}
	for _, p := range sites {
		hit := ""
		for _, e := range posts {
			if desktopProfileHasHost(p, e.Host) {
				hit = e.URL
				break
			}
		}
		if hit != "" {
			done = append(done, p.ID+" "+hit)
		} else {
			missing = append(missing, p.ID)
		}
	}
	if len(done) == 0 {
		return outcome, why
	}
	if len(missing) == 0 {
		return agentservice.TaskOutcomeDone, "verified post: " + strings.Join(done, "; ")
	}
	return agentservice.TaskOutcomePartial, "done: " + strings.Join(done, "; ") + "；not done: " + strings.Join(missing, ", ")
}

// desktopReplyDeniesPost: the reply says the post did not go out.
var desktopReplyDeniesPost = regexp.MustCompile(`没有发布|未发布|没发布|没能发布|没有发出|没发出|发布失败|没有成功发布|未能发布|没有.{0,6}验证|未.{0,4}验证|not (been )?published|not posted`)

// desktopPostCorrection is the line put in front of a reply that denies a
// post this run verifiably published ("" when there is nothing to correct).
func desktopPostCorrection(runID, content string) string {
	if !desktopReplyDeniesPost.MatchString(content) {
		return ""
	}
	var lines []string
	for _, e := range desktopRunVerifiedPosts(runID) {
		line := "更正：已发布并确认 —— " + e.Host
		if e.Title != "" {
			line += "《" + e.Title + "》"
		}
		lines = append(lines, line+" "+e.URL)
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n（下面说没有发布的部分不对：站点的发布成功页已经确认了这篇。）"
}

// desktopRunForID finds a run's state by its id alone (nil when none).
func desktopRunForID(runID string) *desktopRunState {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil
	}
	var found *desktopRunState
	desktopTrajRuns.Range(func(k, v any) bool {
		if key, _ := k.(string); strings.HasSuffix(key, "\x00"+runID) {
			found, _ = v.(*desktopRunState)
			return false
		}
		return true
	})
	return found
}

// Stale questions. A takeover reason or a command question is meant for the
// person only while the run waits on it. A run that went on and did real
// work after it (desktopStaleAfter successful steps) no longer waits: the
// takeover text and the command question are dropped so the final reply
// does not show them.
const desktopStaleAfter = 2

var (
	desktopStaleMu    sync.Mutex
	desktopStaleSince = map[string]int{} // run -> successful steps since the last ask/takeover
)

// desktopNoteAskOrTakeover restarts the count for this run.
func desktopNoteAskOrTakeover(runID string) {
	desktopStaleMu.Lock()
	defer desktopStaleMu.Unlock()
	if len(desktopStaleSince) > 4096 {
		desktopStaleSince = map[string]int{}
	}
	desktopStaleSince[runID] = 0
}

// desktopNoteProgressAfterAsk counts a successful step and clears the
// stale takeover text and command question once the run clearly went on.
func desktopNoteProgressAfterAsk(scope agentruntime.Scope, action string, err error) {
	if err != nil || desktopEscapeAction(action) || scope.RunID == "" {
		return
	}
	desktopStaleMu.Lock()
	n, waiting := desktopStaleSince[scope.RunID]
	if waiting {
		n++
		desktopStaleSince[scope.RunID] = n
	}
	desktopStaleMu.Unlock()
	if !waiting || n < desktopStaleAfter {
		return
	}
	desktopStaleMu.Lock()
	delete(desktopStaleSince, scope.RunID)
	desktopStaleMu.Unlock()
	for _, s := range []agentruntime.Scope{scope, desktopOwner(scope)} {
		desktopAttention.Delete(desktopHandoffKey(s.TenantID, s.UserID, s.InstanceID))
		desktopbot.ForgetDangerAsk(desktopDangerAskKeys(s)...)
	}
	log.Printf("[desktop] run=%s went on after its question/takeover (%d steps): stale takeover text and command question dropped", scope.RunID, n)
}

// desktopLedgerPostedByOther: another run already recorded this post.
func desktopLedgerPostedByOther(key, runID, link string) bool {
	if key == "" || link == "" {
		return false
	}
	for _, e := range desktopLedgerEntries(key) {
		if e.Kind == "posted" && e.URL == link && e.Run != runID {
			return true
		}
	}
	return false
}
