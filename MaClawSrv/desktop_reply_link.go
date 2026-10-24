package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// desktopReplyWithLink gives a run that posted something its reply from the
// publish record, in one fixed form: title, link, and what is missing when
// the post is incomplete. The model's own text is used only when nothing
// was posted (it then explains what failed). This replaces filtering the
// model's narration of guards, tool states and scratch paths.
func desktopReplyWithLink(runID, content string) string {
	rec, ok := desktopPublishRecordTake(runID)
	if !ok || runID == "" {
		return content
	}
	if r := desktopReplyTemplate(rec); r != "" {
		return r
	}
	return content
}

func desktopReplyTemplate(r desktopPublishRecord) string {
	title := ""
	if t := strings.TrimSpace(r.Title); t != "" {
		title = "《" + t + "》"
	}
	if i := strings.IndexAny(r.URL, "?#"); i > 0 {
		r.URL = r.URL[:i]
	}
	switch r.State {
	case browser.PublishLive:
		if r.URL == "" {
			return ""
		}
		return "已发布" + title + "\n链接：" + r.URL
	case browser.PublishLiveIncomplete:
		if r.URL == "" {
			return ""
		}
		return "已发布" + title + "，但不完整：" + r.Missing + "\n链接：" + r.URL
	case browser.PublishSubmitted:
		s := "已提交发布" + title + "，但没能确认它已上线。"
		if r.URL != "" {
			s += "\n链接：" + r.URL
		}
		return s
	}
	return ""
}
