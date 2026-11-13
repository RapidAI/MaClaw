package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func TestPublishDraftIdentityIsReusedAndOrphansAreNamed(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u", InstanceID: "i", RunID: "run_drafts"}
	t.Cleanup(func() { desktopPublishRunTake(scope.RunID) })
	first := "https://zhuanlan.zhihu.com/write?draft=one"
	second := "https://zhuanlan.zhihu.com/write?draft=two"
	desktopRecordPublish(scope, browser.PublishSpec{EditorURL: "https://zhuanlan.zhihu.com/write", Title: "第一篇"}, &browser.PublishReport{
		State: browser.PublishDraftOnly, EditorPage: first,
	})
	spec := browser.PublishSpec{EditorURL: "https://zhuanlan.zhihu.com/write"}
	if rec, ok := desktopPublishRecordOf(scope.RunID); ok && !spec.NewDraft {
		spec.DraftURL = rec.DraftIdentity
	}
	if spec.DraftURL != first {
		t.Fatalf("identity %s", spec.DraftURL)
	}
	desktopRecordPublish(scope, browser.PublishSpec{EditorURL: "https://zhuanlan.zhihu.com/write", NewDraft: true, Title: "第二篇"}, &browser.PublishReport{
		State: browser.PublishDraftOnly, EditorPage: second,
	})
	rec, ok := desktopPublishRecordOf(scope.RunID)
	if !ok || rec.DraftIdentity != first || len(rec.Orphans) != 1 || rec.Orphans[0] != second {
		t.Fatalf("record %+v", rec)
	}
	line := desktopRecordOutcomeLine(rec)
	if !strings.Contains(line, first) || !strings.Contains(line, "未使用的草稿") || !strings.Contains(line, second) {
		t.Fatalf("outcome %s", line)
	}
}

func TestPublishedArticleIsNotAnUnpublishedDraft(t *testing.T) {
	run := "run_csdn_published"
	t.Cleanup(func() {
		desktopPublishRunTake(run)
		desktopSideTake(run)
	})
	page := "https://editor.csdn.net/md?articleId=170737342"
	desktopSideNote(run, "blog.csdn.net：编辑器里留下了《摘要》（"+page+"），没有发布")
	scope := agentruntime.Scope{RunID: run}
	desktopRecordPublish(scope, browser.PublishSpec{Title: "摘要", EditorURL: "https://editor.csdn.net/md/"}, &browser.PublishReport{
		State: browser.PublishLive, PostID: "170737342", EditorPage: page,
		ResultURL: "https://blog.csdn.net/x/article/details/170737342",
	})
	_, notes := desktopSideTake(run)
	for _, note := range notes {
		if strings.Contains(note, "170737342") && strings.Contains(note, "没有发布") {
			t.Fatalf("published article listed as a draft: %s", note)
		}
	}
	content := "桌面工具调用全部报错。没有证据。发布成功。"
	desktopNoteUntrustedReply(run)
	reply, ok := desktopReconcilePublishReply(run, content)
	if !ok || !strings.Contains(reply, "已发布") || !strings.Contains(reply, "170737342") {
		t.Fatalf("reply %q", reply)
	}
	if strings.Contains(reply, "没有证据") || strings.Contains(reply, "全部报错") || strings.Contains(reply, "发布成功") {
		t.Fatalf("contradiction kept: %q", reply)
	}
	md := desktopTaskOutcomeMetadata(run, agentservice.Message{Content: content})
	if md[agentservice.TaskOutcomeReplyKey] != reply {
		t.Fatalf("metadata %#v", md)
	}
}
