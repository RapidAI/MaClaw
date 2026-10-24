package main

import (
	"regexp"
	"strings"
	"sync"
)

// desktopReplyWithLink returns the reply with the run's published link
// appended when the reply does not already contain it.
func desktopReplyWithLink(runID, content string) string {
	rec, ok := desktopPublishRecordTake(runID)
	link := rec.URL
	cand, hasCand := desktopEditLinks.LoadAndDelete(runID)
	if (!ok || link == "") && hasCand && desktopReplySaysPublishedRe.MatchString(content) {
		link, ok = cand.(string), true
	}
	if !ok || link == "" || runID == "" {
		return content
	}
	if strings.Contains(content, link) {
		return content
	}
	if strings.TrimSpace(content) == "" {
		return "已发布：" + link
	}
	return strings.TrimRight(content, "\n") + "\n\n链接：" + link
}

// A publish clicked by script on an editor page (…/p/123/edit) leaves the
// tab on the editor; ZN-5 read the post back elsewhere and replied
// "已发布" without a link. The editor URL minus /edit is the post, used only
// when the reply itself says it was published.
var (
	desktopEditLinks            sync.Map
	desktopEditSuffixRe         = regexp.MustCompile(`^(https?://[^?#]+?)/edit/?$`)
	desktopReplySaysPublishedRe = regexp.MustCompile(`(?i)已发布|发布成功|已发表|发表成功|\bpublished\b`)
)

func desktopNoteEditLink(runID, u string) {
	u = strings.SplitN(strings.TrimSpace(u), "?", 2)[0]
	if m := desktopEditSuffixRe.FindStringSubmatch(u); m != nil && runID != "" {
		desktopEditLinks.Store(runID, m[1])
	}
}
