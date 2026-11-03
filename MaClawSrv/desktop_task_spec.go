package main

import (
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// desktopTaskSpec is what a run's request asks for, read once from the
// request and kept per run: the target site's editor, where a new post is
// listed, how a published post is edited, and the papers to avoid. The
// publish action takes its targets from here instead of the model having to
// find the editor (XHS-30 called publish on the previous run's Zhihu post).
type desktopTaskSpec struct {
	Site      string
	EditorURL string
	VerifyURL string
	EditURL   string
	Profile   *browser.SiteProfile
	// Sites: every site the request names, in the order it names them. A
	// run asked to post to several sites goes through them one by one: the
	// fields above are the site being worked on (desktopTaskSpecFor).
	Sites []*browser.SiteProfile
	Avoid map[string]bool
}

func (s desktopTaskSpec) withSite(p *browser.SiteProfile) desktopTaskSpec {
	if p != nil {
		s.Site, s.EditorURL, s.VerifyURL, s.EditURL, s.Profile = p.ID, p.EditorURL, p.VerifyURL, p.EditURL, p
	}
	return s
}

// site returns the requested site with this profile id (nil: not asked).
func (s desktopTaskSpec) site(id string) *browser.SiteProfile {
	for _, p := range s.Sites {
		if p != nil && p.ID == id {
			return p
		}
	}
	return nil
}

func desktopTaskSpecOf(request string) desktopTaskSpec {
	s := desktopTaskSpec{Avoid: desktopAvoidIDs(request)}
	if !desktopPublishAskRe.MatchString(request) {
		return s
	}
	// The target site's pages come from its loadable profile
	// (corelib/browser/siteprofiles); no profile leaves them empty and the
	// publish action runs on generic discovery.
	s.Sites = browser.ProfilesForRequest(request)
	if len(s.Sites) > 0 {
		return s.withSite(s.Sites[0])
	}
	return s.withSite(browser.ProfileForRequest(request))
}

var desktopTaskSpecs sync.Map // run id -> desktopTaskSpec

// desktopNoteTaskSpec keeps the spec of a run's request (first one wins).
func desktopNoteTaskSpec(runID, request string) {
	if runID != "" && request != "" {
		desktopTaskSpecs.LoadOrStore(runID, desktopTaskSpecOf(request))
	}
}

func desktopTaskSpecBase(runID string) desktopTaskSpec {
	if v, ok := desktopTaskSpecs.Load(runID); ok {
		return v.(desktopTaskSpec)
	}
	return desktopTaskSpec{}
}

// desktopTaskSpecFor is the run's spec for the site it works on now: the
// current record's site while it is unsettled, else the first requested
// site without a settled record.
func desktopTaskSpecFor(runID string) desktopTaskSpec {
	s := desktopTaskSpecBase(runID)
	if len(s.Sites) < 2 {
		return s
	}
	if r, ok := desktopPublishRecordOf(runID); ok && !r.settled() {
		if p := s.site(r.Site); p != nil {
			return s.withSite(p)
		}
	}
	if pend := desktopTaskSitesPending(runID); len(pend) > 0 {
		return s.withSite(pend[0])
	}
	return s
}

// desktopTaskSitesPending: the requested sites (of a run naming two or
// more) that have no settled record yet, in request order.
func desktopTaskSitesPending(runID string) []*browser.SiteProfile {
	s := desktopTaskSpecBase(runID)
	if len(s.Sites) < 2 {
		return nil
	}
	settled := map[string]bool{}
	for _, r := range desktopPublishRunRecords(runID) {
		if r.settled() {
			settled[r.Site] = true
		}
	}
	var out []*browser.SiteProfile
	for _, p := range s.Sites {
		if !settled[p.ID] {
			out = append(out, p)
		}
	}
	return out
}

// desktopSiteName is how the person names the site (its first request word).
func desktopSiteName(p *browser.SiteProfile) string {
	if p == nil {
		return ""
	}
	if len(p.RequestWords) > 0 && p.RequestWords[0] != "" {
		return p.RequestWords[0]
	}
	return p.ID
}
