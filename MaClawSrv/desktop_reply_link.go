package main

import (
	"regexp"
	"strings"
	"sync"
)

// A verified action=publish names the link the reply must carry. XHS-11 and
// XHS-17 published and verified, then ended on a reply without it; the turn
// end adds the link when the reply left it out.
var (
	desktopPublishedLinks sync.Map // run id -> link
	desktopPublishLinkRe  = regexp.MustCompile(`with this link: (https?://\S+)`)
)

func desktopNotePublishedLink(runID, out string) {
	if runID == "" {
		return
	}
	if m := desktopPublishLinkRe.FindStringSubmatch(out); m != nil {
		desktopPublishedLinks.Store(runID, strings.TrimRight(m[1], "。.,，)"))
	}
}

// desktopReplyWithLink returns the reply with the run's published link
// appended when the reply does not already contain it.
func desktopReplyWithLink(runID, content string) string {
	v, ok := desktopPublishedLinks.LoadAndDelete(runID)
	cand, hasCand := desktopEditLinks.LoadAndDelete(runID)
	if !ok && hasCand && desktopReplySaysPublishedRe.MatchString(content) {
		v, ok = cand, true
	}
	if !ok || runID == "" {
		return content
	}
	link := v.(string)
	if strings.Contains(content, link) {
		return content
	}
	if strings.TrimSpace(content) == "" {
		return "已发布：" + link
	}
	return strings.TrimRight(content, "\n") + "\n\n链接：" + link
}

// desktopNotePublishedURL records a result page the run verified itself (a
// script click on 发布 then a read-back), unless publish already named one.
func desktopNotePublishedURL(runID, u string) {
	u = strings.SplitN(strings.TrimSpace(u), "?", 2)[0]
	if runID == "" || !strings.HasPrefix(u, "http") {
		return
	}
	desktopPublishedLinks.LoadOrStore(runID, u)
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
