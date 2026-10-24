package main

import (
	"regexp"
	"sync"
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
	Avoid     map[string]bool
}

// Site data only: which request words name which site, and its pages.
var desktopTaskSites = []struct {
	re                         *regexp.Regexp
	site, editor, verify, edit string
}{
	{regexp.MustCompile(`(?i)知乎|zhihu`), "zhihu", "https://zhuanlan.zhihu.com/write", "", "{url}/edit"},
	{regexp.MustCompile(`(?i)小红书|xiaohongshu|rednote`), "xiaohongshu", "https://creator.xiaohongshu.com/publish/publish?from=menu", "https://creator.xiaohongshu.com/new/note-manager", ""},
}

func desktopTaskSpecOf(request string) desktopTaskSpec {
	s := desktopTaskSpec{Avoid: desktopAvoidIDs(request)}
	if !desktopPublishAskRe.MatchString(request) {
		return s
	}
	for _, t := range desktopTaskSites {
		if t.re.MatchString(request) {
			s.Site, s.EditorURL, s.VerifyURL, s.EditURL = t.site, t.editor, t.verify, t.edit
			break
		}
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
