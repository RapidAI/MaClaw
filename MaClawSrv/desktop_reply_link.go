package main

import "strings"

// desktopReplyWithLink returns the reply with the run's published link
// appended when the reply does not already contain it.
func desktopReplyWithLink(runID, content string) string {
	rec, ok := desktopPublishRecordTake(runID)
	link := rec.URL
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
