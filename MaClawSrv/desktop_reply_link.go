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
