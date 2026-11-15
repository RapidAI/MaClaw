package main

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestPostedNoticeTreatsTheQueryAsTheSameLink(t *testing.T) {
	dir := t.TempDir()
	old := desktopLedgerDir
	desktopLedgerDir = func() string { return dir }
	t.Cleanup(func() { desktopLedgerDir = old })
	run := "run-posted-query"
	scope := agentruntime.Scope{TenantID: "t", UserID: "u", InstanceID: "i", RunID: run}
	raw := "https://zhuanlan.zhihu.com/p/2105161721778278920?just_published=2"
	desktopLedgerAdd(desktopLedgerKey(scope), desktopLedgerEntry{
		Kind: "posted", Host: "zhuanlan.zhihu.com", Title: "综述", URL: raw, Run: run, Detail: "published",
	})
	bare := "知乎：已发布\n链接：https://zhuanlan.zhihu.com/p/2105161721778278920"
	if got := desktopPostedNotice(run, bare); got != "" {
		t.Fatalf("same article listed twice: %q", got)
	}
	got := desktopPostedNotice(run, "还没有链接")
	if !strings.Contains(got, "https://zhuanlan.zhihu.com/p/2105161721778278920") || strings.Contains(got, "just_published") {
		t.Fatalf("missing link not named as the article: %q", got)
	}
	desktopLedgerAdd(desktopLedgerKey(scope), desktopLedgerEntry{
		Kind: "posted", Host: "editor.csdn.net", Title: "综述", URL: "https://editor.csdn.net/md/", Run: run, Detail: "published",
	})
	if strings.Contains(desktopPostedNotice(run, "还没有链接"), "editor.csdn.net") {
		t.Fatal("editor page announced as the article")
	}
	short := "https://zhuanlan.zhihu.com/p/21"
	desktopLedgerAdd(desktopLedgerKey(scope), desktopLedgerEntry{
		Kind: "posted", Host: "zhuanlan.zhihu.com", Title: "另一篇", URL: short, Run: run, Detail: "published",
	})
	longer := "正文里只有 https://zhuanlan.zhihu.com/p/2105161721778278920"
	desktopLedgerAdd(desktopLedgerKey(scope), desktopLedgerEntry{
		Kind: "posted", Host: "editor.csdn.net", Title: "另一篇", URL: "https://zhuanlan.zhihu.com/p/99", Run: run, Detail: "found_in_list",
	})
	notice := desktopPostedNotice(run, longer)
	if !strings.Contains(notice, short) {
		t.Fatalf("a longer id hid the other article: %s", notice)
	}
	if strings.Contains(notice, "/p/99") {
		t.Fatal("another site's link was announced for csdn")
	}
}

func TestRunningNoticeKeepsTheJobOutOfTheSentence(t *testing.T) {
	notice := desktopOutcomeNotice("running", "后台在跑：cd x && ./snake --demo（任务 fae1faef，running）", "")
	if strings.Contains(notice, "依据") || strings.Contains(notice, "fae1faef") || strings.Contains(notice, "snake") {
		t.Fatalf("notice leaked the job: %q", notice)
	}
}

func TestWatchdogLoopWarnThenStop(t *testing.T) {
	scope := agentruntime.Scope{RunID: "run_wd_loop"}
	defer desktopWatchdogEnd(scope.RunID)
	args := map[string]any{"action": "click", "selector": "#x"}
	warned := false
	for i := 1; i <= watchdogLoopStop; i++ {
		w, err := desktopWatchdogBefore(scope, "click", args)
		if w != "" {
			warned = true
		}
		if i < watchdogLoopStop && err != nil {
			t.Fatalf("step %d stopped early", i)
		}
		if i == watchdogLoopStop && err != errWatchdogStop {
			t.Fatalf("not stopped at %d", i)
		}
	}
	if !warned {
		t.Fatal("no warning before stop")
	}
	if r, ok := desktopWatchdogVerdict(scope.RunID); !ok || r == "" {
		t.Fatal("no verdict")
	}
	if r, ok := desktopPublishSettled(scope.RunID); !ok || r == "" {
		t.Fatal("run goal does not end the run")
	}
}

func TestWatchdogErrors(t *testing.T) {
	scope := agentruntime.Scope{RunID: "run_wd_err"}
	defer desktopWatchdogEnd(scope.RunID)
	for i := 0; i < watchdogErrStop; i++ {
		desktopWatchdogBefore(scope, "click", map[string]any{"i": i})
		desktopWatchdogAfter(scope, watchdogError("boom"))
	}
	if _, ok := desktopWatchdogVerdict(scope.RunID); !ok {
		t.Fatal("consecutive errors not stopped")
	}
}

func TestEnsureReceipt(t *testing.T) {
	dir := t.TempDir()
	old := desktopLedgerDir
	desktopLedgerDir = func() string { return dir }
	defer func() { desktopLedgerDir = old }()
	scope := agentruntime.Scope{TenantID: "t", UserID: "u1"}
	run := &agentservice.Run{ID: "run_rc", Status: agentservice.RunStatusFailed}
	m := desktopEnsureReceipt(scope, run, nil, nil, false)
	if m == nil || m.Content == "" {
		t.Fatal("failed run got no receipt")
	}
	run2 := &agentservice.Run{ID: "run_rc2", Status: agentservice.RunStatusCancelled}
	if m := desktopEnsureReceipt(scope, run2, nil, nil, false); m != nil {
		t.Fatal("cancelled run got a second reply")
	}
	keep := &agentservice.Message{Content: "done"}
	if m := desktopEnsureReceipt(scope, run, keep, nil, false); m.Content != "done" {
		t.Fatal("reply overwritten")
	}
}

func TestLedgerPendingAndRecent(t *testing.T) {
	now := time.Now()
	es := []desktopLedgerEntry{{Kind: "publish_takeover", Host: "creator.xiaohongshu.com", At: now.Add(-time.Hour)}}
	if _, ok := desktopLedgerPendingCheck(es, "creator.xiaohongshu.com", now); !ok {
		t.Fatal("takeover not pending")
	}
	es = append(es, desktopLedgerEntry{Kind: "posted", Host: "creator.xiaohongshu.com", Title: "春日 穿搭", At: now})
	if _, ok := desktopLedgerPendingCheck(es, "creator.xiaohongshu.com", now); ok {
		t.Fatal("settled still pending")
	}
	if _, ok := desktopLedgerRecentPost(es, "creator.xiaohongshu.com", "春日穿搭", now); !ok {
		t.Fatal("same title not found")
	}
	if _, ok := desktopLedgerRecentPost(es, "creator.xiaohongshu.com", "另一篇", now); ok {
		t.Fatal("other title blocked")
	}
}
