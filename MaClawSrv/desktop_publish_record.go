package main

import (
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime/promptparts"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// desktopPublishRecord is the single source of truth for "did this run
// publish, where, and is it complete". Only action=publish (and a run's own
// verified result page) writes it; the coach, the guards, a repeated publish
// call and the reply all read it instead of parsing result or error text.
type desktopPublishRecord struct {
	// Site: the profile id the record is for. A request naming several
	// sites keeps one record per site (see desktopPublishUseSite).
	Site    string               `json:"site,omitempty"`
	State   browser.PublishState `json:"state"`
	URL     string               `json:"url,omitempty"`
	PostID  string               `json:"post_id,omitempty"`
	Missing string               `json:"missing,omitempty"`
	Title   string               `json:"title,omitempty"`
	Calls   int                  `json:"calls"`
	// PlannedImages: the most picture files any publish call of the run
	// passed; later calls may not pass fewer.
	PlannedImages int `json:"planned_images,omitempty"`
	// Evidence: what proved the post (response, redirect, list_api, list).
	Evidence string `json:"evidence,omitempty"`
	// Notice: a blocking notice the page showed.
	Notice string `json:"notice,omitempty"`
	// Pending: the site says the post is under review (审核中).
	Pending string `json:"pending,omitempty"`
	// Errors and NetLog: the last calls' errors and the page's writes,
	// truncated, kept so a failed publish can be diagnosed afterwards.
	// Drafts: editor pages this run's calls ended on (their own drafts).
	// Earlier: an earlier run of the same request published this site.
	Earlier bool      `json:"earlier,omitempty"`
	Drafts  []string  `json:"drafts,omitempty"`
	Errors  []string  `json:"errors,omitempty"`
	NetLog  []string  `json:"net_log,omitempty"`
	Updated time.Time `json:"updated"`
}

// Limits for the diagnostics kept in a record.
const (
	desktopRecordErrMax    = 3
	desktopRecordErrRunes  = 600
	desktopRecordNetLogMax = 40
)

// posted: the submit went out (live, live but incomplete, or unconfirmed).
// A draft_only run saved a draft and posted nothing.
func (r desktopPublishRecord) posted() bool {
	return r.State == browser.PublishLive || r.State == browser.PublishLiveIncomplete || r.State == browser.PublishSubmitted
}

// settled: nothing more to do on this record's site — the submit went out,
// or the page shows a blocking notice only the person can clear.
func (r desktopPublishRecord) settled() bool {
	return r.posted() || (r.State == browser.PublishNotSubmitted && r.Notice != "")
}

var (
	desktopPublishRecordsMu sync.Mutex
	desktopPublishRecords   = map[string]*desktopPublishRecord{} // run id -> current site's record
	// desktopPublishDone: the records of the other sites a multi-site run
	// already worked on (run id -> records), guarded by the same lock.
	desktopPublishDone = map[string][]desktopPublishRecord{}
)

// desktopPublishUseSite makes site the run's current record: the current
// one of another site is kept aside, and an earlier record of this site
// comes back. A single-site run never switches.
func desktopPublishUseSite(runID, site string) {
	if runID == "" || site == "" {
		return
	}
	desktopPublishRecordsMu.Lock()
	defer desktopPublishRecordsMu.Unlock()
	cur := desktopPublishRecords[runID]
	if cur != nil && (cur.Site == site || cur.Site == "") {
		cur.Site = site
		return
	}
	done := desktopPublishDone[runID]
	var back *desktopPublishRecord
	for i := range done {
		if done[i].Site == site {
			c := done[i]
			back = &c
			done = append(done[:i:i], done[i+1:]...)
			break
		}
	}
	if cur != nil {
		done = append(done, *cur)
	}
	if len(done) > 0 {
		desktopPublishDone[runID] = done
	} else {
		delete(desktopPublishDone, runID)
	}
	if back != nil {
		desktopPublishRecords[runID] = back
	} else {
		delete(desktopPublishRecords, runID)
	}
}

// desktopPublishParkCurrent sets the current record aside (its site is
// settled and the run goes on to the next requested site): the run then has
// no current record, so the guards and the phase plan treat the next site
// as a fresh publish.
func desktopPublishParkCurrent(runID string) {
	desktopPublishRecordsMu.Lock()
	defer desktopPublishRecordsMu.Unlock()
	if r := desktopPublishRecords[runID]; r != nil {
		desktopPublishDone[runID] = append(desktopPublishDone[runID], *r)
		delete(desktopPublishRecords, runID)
	}
}

// desktopPublishRunRecords: every record of the run, the earlier sites'
// first and the current one last.
func desktopPublishRunRecords(runID string) []desktopPublishRecord {
	desktopPublishRecordsMu.Lock()
	defer desktopPublishRecordsMu.Unlock()
	out := append([]desktopPublishRecord(nil), desktopPublishDone[runID]...)
	if r := desktopPublishRecords[runID]; r != nil {
		out = append(out, *r)
	}
	return out
}

// desktopPublishRunTake returns every record of the run and forgets them.
func desktopPublishRunTake(runID string) []desktopPublishRecord {
	desktopPublishRecordsMu.Lock()
	defer desktopPublishRecordsMu.Unlock()
	out := append([]desktopPublishRecord(nil), desktopPublishDone[runID]...)
	if r := desktopPublishRecords[runID]; r != nil {
		out = append(out, *r)
	}
	delete(desktopPublishDone, runID)
	delete(desktopPublishRecords, runID)
	return out
}

// desktopPublishRecordOf returns a copy of the run's record (ok=false: none).
func desktopPublishRecordOf(runID string) (desktopPublishRecord, bool) {
	desktopPublishRecordsMu.Lock()
	defer desktopPublishRecordsMu.Unlock()
	r, ok := desktopPublishRecords[runID]
	if !ok {
		return desktopPublishRecord{}, false
	}
	return *r, true
}

// desktopPublishRecordTake returns the record and forgets it (turn end).
func desktopPublishRecordTake(runID string) (desktopPublishRecord, bool) {
	desktopPublishRecordsMu.Lock()
	defer desktopPublishRecordsMu.Unlock()
	r, ok := desktopPublishRecords[runID]
	if !ok {
		return desktopPublishRecord{}, false
	}
	delete(desktopPublishRecords, runID)
	delete(desktopPublishDone, runID)
	return *r, true
}

func desktopPublishRecordUpdate(scope agentruntime.Scope, fn func(r *desktopPublishRecord)) desktopPublishRecord {
	if scope.RunID == "" {
		r := desktopPublishRecord{}
		fn(&r)
		return r
	}
	desktopPublishRecordsMu.Lock()
	r, ok := desktopPublishRecords[scope.RunID]
	if !ok {
		r = &desktopPublishRecord{}
		desktopPublishRecords[scope.RunID] = r
	}
	fn(r)
	r.Updated = time.Now()
	out := *r
	desktopPublishRecordsMu.Unlock()
	desktopEmit(scope, desktopEvent{Event: "publish_record", Action: "publish", Result: string(out.State), Reason: strings.TrimSpace(out.URL + " " + out.PostID + " " + out.Missing)})
	return out
}

// desktopRecordPublish stores what a publish run observed. A link the site
// lists only by id becomes the public post link when site data knows its
// pattern.
func desktopRecordPublish(scope agentruntime.Scope, spec browser.PublishSpec, rep *browser.PublishReport, runErr ...error) desktopPublishRecord {
	rec := desktopPublishRecordUpdate(scope, func(r *desktopPublishRecord) {
		r.Calls++
		r.Title = spec.Title
		if r.Site == "" {
			if p := browser.ProfileForURL(spec.EditorURL); p != nil {
				r.Site = p.ID
			}
		}
		if len(spec.Files) > r.PlannedImages {
			r.PlannedImages = len(spec.Files)
		}
		if len(runErr) > 0 && runErr[0] != nil {
			e := []rune(runErr[0].Error())
			if len(e) > desktopRecordErrRunes {
				e = append(e[:desktopRecordErrRunes], '…')
			}
			r.Errors = append(r.Errors, string(e))
			if len(r.Errors) > desktopRecordErrMax {
				r.Errors = r.Errors[len(r.Errors)-desktopRecordErrMax:]
			}
		}
		if rep != nil && rep.EditorPage != "" && !desktopStrIn(r.Drafts, rep.EditorPage) {
			r.Drafts = append(r.Drafts, rep.EditorPage)
			if len(r.Drafts) > 4 {
				r.Drafts = r.Drafts[len(r.Drafts)-4:]
			}
		}
		if rep != nil {
			r.Evidence, r.Notice, r.Pending = rep.Evidence, rep.Notice, rep.Pending
			r.NetLog = rep.NetLog
			if len(r.NetLog) > desktopRecordNetLogMax {
				r.NetLog = r.NetLog[len(r.NetLog)-desktopRecordNetLogMax:]
			}
		}
		if rep == nil {
			if r.State == "" {
				r.State = browser.PublishNotSubmitted
			}
			return
		}
		r.State, r.Missing, r.PostID = rep.State, rep.Missing, rep.PostID
		r.URL = rep.ResultURL
		if r.State == browser.PublishSubmitted || r.State == browser.PublishNotSubmitted || r.State == browser.PublishDraftOnly || r.State == browser.PublishNoEditor {
			// Not confirmed: no link of an earlier call or another post
			// may stand for this one (stale success URLs leaked before).
			r.URL = ""
		}
		if rep.PostID != "" && (r.State == browser.PublishLive || r.State == browser.PublishLiveIncomplete) {
			// A public article URL that already names this id stays. The
			// template is filled when the report has no such link (an empty
			// URL, a creator-console page, or a profile link beside the
			// title). A template that still cannot be filled does not keep
			// that other link.
			if browser.PostURLHasID(r.URL, rep.PostID) {
				r.URL = browser.PublicPostURL(r.URL)
			} else if link := desktopPostURLFor(spec, rep.PostID, rep.Account); link != "" {
				r.URL = link
			} else if strings.Contains(desktopPostURLTemplate(spec), "{id}") {
				r.URL = ""
			}
		}
	})

	desktopSideNotePublish(scope.RunID, spec, rep)
	if rec.posted() {
		// The post outlives the run: a resumed or new run must not post it again.
		desktopLedgerAdd(desktopLedgerKey(scope), desktopLedgerEntry{Kind: "posted", Host: desktopLedgerHost(firstNonEmptyStr(spec.EditorURL, rec.URL)), Title: spec.Title, URL: rec.URL, Run: scope.RunID, Detail: string(rec.State)})
	}
	return rec
}

// desktopRecordVerifiedPage: a run that verified its own result page (a
// submit, then a read-back of the filled text on a new page).
func desktopRecordVerifiedPage(scope agentruntime.Scope, u string) {
	u = strings.SplitN(strings.TrimSpace(u), "?", 2)[0]
	if !strings.HasPrefix(u, "http") {
		return
	}
	desktopPublishRecordUpdate(scope, func(r *desktopPublishRecord) {
		if r.posted() && r.URL != "" {
			return
		}
		r.State, r.URL = browser.PublishLive, u
	})
}

// desktopPublishRecordText is what the model reads after a publish call:
// the state, the link and the one next step that state allows.
func desktopPublishRecordText(r desktopPublishRecord) string {
	if promptparts.Enabled() {
		return desktopPublishRecordFacts(r)
	}
	return desktopPublishRecordLegacyText(r)
}

// desktopPublishRecordFacts is the same state as facts only: no sentence for
// the person, so the bot words the reply itself (promptparts.Facts).
func desktopPublishRecordFacts(r desktopPublishRecord) string {
	state, next, evidence := string(r.State), "final_answer", ""
	switch r.State {
	case browser.PublishLive:
		evidence = "read_back"
	case browser.PublishLiveIncomplete:
		evidence = "read_back"
	case browser.PublishSubmitted:
		// Not a success: the reply must say it is not confirmed and has
		// no public link (run_6840f5a1 was reported as 已提交发布).
		evidence = "submit_clicked_unconfirmed_not_published"
	case browser.PublishDraftOnly:
		next = "publish"
	default:
		state = string(browser.PublishNotSubmitted)
		next = "publish"
		if r.Notice != "" {
			next = "final_answer"
		}
	}
	return promptparts.Fact("PUBLISH_RESULT", "state", state, "link", r.URL, "missing", r.Missing, "notice", r.Notice, "pending", r.Pending, "evidence", evidence, "next_allowed", next)
}

// desktopPublishNextSiteText follows a settled record when the request
// names more sites: the run is not done, the next site is.
func desktopPublishNextSiteText(r desktopPublishRecord, pend []*browser.SiteProfile) string {
	names := make([]string, 0, len(pend))
	for _, p := range pend {
		names = append(names, desktopSiteName(p))
	}
	next := pend[0]
	if promptparts.Enabled() {
		return promptparts.Fact("PUBLISH_RESULT", "site", r.Site, "state", string(r.State), "link", r.URL, "missing", r.Missing, "notice", r.Notice,
			"sites_left", strings.Join(names, ","), "next_allowed", "publish", "next_site", desktopSiteName(next), "next_editor_url", next.EditorURL)
	}
	return "PUBLISH STATE: " + string(r.State) + " on " + r.Site + " " + r.URL + " — this site is done. The task also asks for " + strings.Join(names, ", ") +
		": do not touch the post above; now run action=publish for " + desktopSiteName(next) + " with editor_url=" + next.EditorURL + " and the same title, body and files (fit that site's limits)."
}

// desktopPublishRecordLegacyText is the pre-style-layer wording
// (MACLAW_STYLE_LAYER=off).
func desktopPublishRecordLegacyText(r desktopPublishRecord) string {
	link := r.URL
	switch r.State {
	case browser.PublishLive:
		if r.Pending != "" {
			return "PUBLISH STATE: published (" + r.Pending + ") — " + link + ". The site took the post and says it is under review; it may not be public yet. Reply once with this link and say it is under review; do nothing else on the site."
		}
		return "PUBLISH STATE: published — " + link + ". The post is published and read back. Reply to the user once with this link; do nothing else on the site."
	case browser.PublishLiveIncomplete:
		return "PUBLISH STATE: live_incomplete — " + link + " is published but " + r.Missing + ". Do not publish again and do not call it complete: reply with the link and say exactly what is missing."
	case browser.PublishSubmitted:
		where := link
		if where == "" {
			where = "the site"
		}
		return "PUBLISH STATE: submitted_unconfirmed — the submit was clicked on " + where + " but no success page, post id or listed post proves it is published; it may only be a draft. Do not publish again. Reply that it is NOT confirmed published and there is no public link; never say it was published or submitted successfully."
	case browser.PublishDraftOnly:
		return "PUBLISH STATE: draft_only — the click only saved a draft; nothing was posted. Look at what the page asks for (a settings panel, a required field, a notice) and run action=publish again with the full title, body and files."
	}
	if r.Notice != "" {
		return "PUBLISH STATE: not_submitted — nothing was posted; the page shows a blocking notice: " + r.Notice + ". Tell the user exactly this; do not retry until it is resolved."
	}
	return "PUBLISH STATE: not_submitted — nothing was posted. Fix what the failed step names and run action=publish again with the full title, body and files."
}

func firstNonEmptyStr(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func desktopStrIn(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
