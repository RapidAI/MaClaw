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
			return desktopReplyKeep(content)
		}
		return desktopReplyMulti(base, recs, pend, content)
	}
	rec, ok := desktopPublishRecordTake(runID)
	if !ok {
		return desktopReplyKeep(content)
	}
	if r := desktopReplyTemplate(rec); r != "" {
		return r
	}
	if rec.State == browser.PublishDraftOnly {
		// The page was still the editor. The model's own "已发布" plus that
		// URL is how CSDN's editor homepage was reported as the article.
		return "还在编辑器里，没有发布成功页，这篇还没发出去"
	}
	return desktopReplyKeep(content)
}

// desktopReplyKeep leaves an ordinary explanation alone. A reply that calls
// a creator-console or editor URL published is not sent on: that page is
// not an article, and there may be no publish record to replace the text.
func desktopReplyKeep(content string) string {
	if !desktopClaimsFalseLink(content) {
		return content
	}
	if cleaned := desktopStripConsoleURL(content); cleaned != "" {
		return cleaned
	}
	return "还在编辑器里，没有发布成功页，这篇还没发出去"
}

// desktopClaimsFalseLink is the false report "已发布" plus an editor or
// creator-console URL. A login line that merely contains 链接 and a
// passport address is not one: stripping it hid the sign-in page.
func desktopClaimsFalseLink(content string) bool {
	if !strings.Contains(content, "已发布") {
		return false
	}
	for _, u := range desktopURLRe.FindAllString(content, -1) {
		if browser.PublicPostURL(u) == "" || browser.PublishURLIsBareOrigin(u) {
			return true
		}
	}
	return false
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
			if line == "" && r.State == browser.PublishDraftOnly {
				failed = true
				line = "还在编辑器里，没有发布成功页，这篇还没发出去"
			}
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
		if c := desktopStripConsoleURL(content); c != "" {
			out += "\n\n" + c
		}
	}
	return out
}

// desktopStripConsoleURL drops creator-console and editor URLs from a reply.
// A line that was only "已发布" plus that URL is dropped with them, so a
// three-site answer cannot repeat https://editor.csdn.net/md/ under 没发出去.
func desktopStripConsoleURL(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		// Only a line that claims the post is up can lose a console URL.
		// The rest of the explanation, including a login address, stays.
		if !strings.Contains(line, "已发布") && !strings.HasPrefix(strings.TrimSpace(line), "链接") {
			lines = append(lines, line)
			continue
		}
		line = desktopURLRe.ReplaceAllStringFunc(line, func(u string) string {
			if browser.PublicPostURL(u) == "" || browser.PublishURLIsBareOrigin(u) {
				return ""
			}
			return u
		})
		line = strings.TrimSpace(line)
		line = strings.TrimRight(line, "：: ")
		line = strings.TrimSpace(line)
		if line == "" || line == "已发布" || line == "链接" {
			continue
		}
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
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
	// An editor or creator-console URL is not the article. Printing it as
	// 链接 made CSDN's https://editor.csdn.net/md/ look published, and the
	// run then stopped before Zhihu and Xiaohongshu.
	if r.URL != "" {
		pub := browser.PublicPostURL(r.URL)
		if pub == "" || browser.PublishURLIsBareOrigin(pub) {
			if r.PostID == "" && (r.State == browser.PublishLive || r.State == browser.PublishLiveIncomplete) {
				return ""
			}
			r.URL = ""
		} else {
			r.URL = pub
		}
	}
	earlier := ""
	if r.Earlier {
		earlier = "（之前那次已发布，这次没有重发）"
	}
	switch r.State {
	case browser.PublishLive:
		if r.URL == "" {
			// Live with no public link (csdn 11-11 22:22 gave only the
			// article id): it is published, not "没发出去".
			if r.PostID != "" {
				return "已发布" + title + earlier + "，站点只返回了文章 id " + r.PostID + "，没拿到公开链接，请在创作后台查看"
			}
			return "已发布" + title + earlier + "，没拿到公开链接，请在创作后台查看"
		}
		if r.Earlier {
			return "已发布" + title + earlier + "\n链接：" + r.URL
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
