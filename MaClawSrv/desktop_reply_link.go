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
	if runID == "" {
		return content
	}
	if base := desktopTaskSpecBase(runID); len(base.Sites) > 1 {
		pend := desktopTaskSitesPending(runID)
		recs := desktopPublishRunTake(runID)
		if len(recs) == 0 {
			return content
		}
		return desktopReplyMulti(base, recs, pend, content)
	}
	rec, ok := desktopPublishRecordTake(runID)
	if !ok {
		return content
	}
	if r := desktopReplyTemplate(rec); r != "" {
		return r
	}
	return content
}

// desktopReplyMulti is the reply of a run asked to post to several sites:
// one line per site in request order, then the model's own words when a
// site was not done (it explains what failed).
func desktopReplyMulti(base desktopTaskSpec, recs []desktopPublishRecord, pend []*browser.SiteProfile, content string) string {
	bySite := map[string]desktopPublishRecord{}
	for _, r := range recs {
		bySite[r.Site] = r
	}
	open := map[string]bool{}
	for _, p := range pend {
		open[p.ID] = true
	}
	var lines []string
	failed := false
	for _, p := range base.Sites {
		name := desktopSiteName(p)
		r, ok := bySite[p.ID]
		line := ""
		if ok {
			line = desktopReplyTemplate(r)
			if line == "" && r.Notice != "" {
				line = "没发出去，页面提示：" + r.Notice
			}
		}
		if line == "" {
			failed = true
			line = "没发出去"
		}
		lines = append(lines, name+"："+line)
	}
	out := strings.Join(lines, "\n")
	if failed || len(open) > 0 {
		if c := strings.TrimSpace(content); c != "" {
			out += "\n\n" + c
		}
	}
	return out
}

// desktopUnconfirmedReply opens the reply of a submit with no proof.
const desktopUnconfirmedReply = "没能确认发布成功"

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
		if r.Pending != "" {
			return "已发布" + title + "，站点显示" + r.Pending + "，审核通过后才对外公开\n链接：" + r.URL
		}
		return "已发布" + title + "\n链接：" + r.URL
	case browser.PublishLiveIncomplete:
		if r.URL == "" {
			return ""
		}
		return "已发布" + title + "，但不完整：" + r.Missing + "\n链接：" + r.URL
	case browser.PublishSubmitted:
		// Not found in the post list: no link, the URL is still the editor.
		return desktopUnconfirmedReply + title + "：点了发布，但没看到发布成功页，站点没返回文章 id 或地址，帖子列表里也没找到，可能只存成了草稿。拿不到对外地址，请到创作后台看一眼；我没有重复提交。"
	}
	return ""
}

// desktopDropAskAfterPost: a templated post reply is the whole answer; a
// question the model attached (ZN-22 asked to confirm a fix after the post
// was live) is dropped with it.
func desktopDropAskAfterPost(content string, meta map[string]string) {
	if meta != nil && strings.Contains(content, desktopUnconfirmedReply) {
		// The run itself ended, the post did not: the result carries it
		// so it is not shown or counted as a published post.
		meta["publish_state"] = "submitted_unconfirmed"
	}
	if meta == nil || !strings.HasPrefix(content, "已发布") && !strings.HasPrefix(content, desktopUnconfirmedReply) && !strings.Contains(content, "：已发布") {
		return
	}
	for k := range meta {
		if strings.HasPrefix(k, "ask_user_") {
			delete(meta, k)
		}
	}
}
