package main

import (
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// desktopPublishRecord is the single source of truth for "did this run
// publish, where, and is it complete". Only action=publish (and a run's own
// verified result page) writes it; the coach, the guards, a repeated publish
// call and the reply all read it instead of parsing result or error text.
type desktopPublishRecord struct {
	State   browser.PublishState `json:"state"`
	URL     string               `json:"url,omitempty"`
	PostID  string               `json:"post_id,omitempty"`
	Missing string               `json:"missing,omitempty"`
	Title   string               `json:"title,omitempty"`
	Calls   int                  `json:"calls"`
	Updated time.Time            `json:"updated"`
}

// posted: the submit went out (live, live but incomplete, or unconfirmed).
func (r desktopPublishRecord) posted() bool {
	return r.State == browser.PublishLive || r.State == browser.PublishLiveIncomplete || r.State == browser.PublishSubmitted
}

var (
	desktopPublishRecordsMu sync.Mutex
	desktopPublishRecords   = map[string]*desktopPublishRecord{} // run id -> record
)

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
func desktopRecordPublish(scope agentruntime.Scope, spec browser.PublishSpec, rep *browser.PublishReport) desktopPublishRecord {
	return desktopPublishRecordUpdate(scope, func(r *desktopPublishRecord) {
		r.Calls++
		r.Title = spec.Title
		if rep == nil {
			if r.State == "" {
				r.State = browser.PublishNotSubmitted
			}
			return
		}
		r.State, r.Missing, r.PostID = rep.State, rep.Missing, rep.PostID
		r.URL = rep.ResultURL
		if rep.PostID != "" && spec.VerifyURL != "" {
			if link := desktopPostURLFor(spec.VerifyURL, rep.PostID); link != "" {
				r.URL = link
			}
		}
	})
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
	link := r.URL
	switch r.State {
	case browser.PublishLive:
		return "PUBLISH STATE: live — " + link + ". The post is published and read back. Reply to the user once with this link; do nothing else on the site."
	case browser.PublishLiveIncomplete:
		return "PUBLISH STATE: live_incomplete — " + link + " is published but " + r.Missing + ". Do not publish again and do not call it complete: reply with the link and say exactly what is missing."
	case browser.PublishSubmitted:
		where := link
		if where == "" {
			where = "the site"
		}
		return "PUBLISH STATE: submitted — the submit went out on " + where + " but the post was not confirmed. Do not publish again. Reply that it was submitted and could not be confirmed, with any link above."
	}
	return "PUBLISH STATE: not_submitted — nothing was posted. Fix what the failed step names and run action=publish again with the full title, body and files."
}
