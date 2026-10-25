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
	Avoid     map[string]bool
}

func desktopTaskSpecOf(request string) desktopTaskSpec {
	s := desktopTaskSpec{Avoid: desktopAvoidIDs(request)}
	if !desktopPublishAskRe.MatchString(request) {
		return s
	}
	// The target site's pages come from its loadable profile
	// (corelib/browser/siteprofiles); no profile leaves them empty and the
	// publish action runs on generic discovery.
	if p := browser.ProfileForRequest(request); p != nil {
		s.Site, s.EditorURL, s.VerifyURL, s.EditURL, s.Profile = p.ID, p.EditorURL, p.VerifyURL, p.EditURL, p
	}
	return s
}

var desktopTaskSpecs sync.Map // run id -> desktopTaskSpec

// desktopNoteTaskSpec keeps the spec of a run's request (first one wins).
func desktopNoteTaskSpec(runID, request string) {
	if runID != "" && request != "" {
		desktopTaskSpecs.LoadOrStore(runID, desktopTaskSpecOf(request))
	}
}

func desktopTaskSpecFor(runID string) desktopTaskSpec {
	if v, ok := desktopTaskSpecs.Load(runID); ok {
		return v.(desktopTaskSpec)
	}
	return desktopTaskSpec{}
}
