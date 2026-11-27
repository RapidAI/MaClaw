package main

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/browser"
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
	m := desktopEnsureReceipt(scope, run, nil, nil, false, false)
	if m == nil || m.Content == "" {
		t.Fatal("failed run got no receipt")
	}
	run2 := &agentservice.Run{ID: "run_rc2", Status: agentservice.RunStatusCancelled}
	if m := desktopEnsureReceipt(scope, run2, nil, nil, false, false); m != nil {
		t.Fatal("cancelled run got a second reply")
	}
	keep := &agentservice.Message{Content: "done"}
	if m := desktopEnsureReceipt(scope, run, keep, nil, false, false); m.Content != "done" {
		t.Fatal("reply overwritten")
	}
}

func TestEnsureReceiptDoesNotClaimSuccessWithoutDelivery(t *testing.T) {
	dir := t.TempDir()
	old := desktopLedgerDir
	desktopLedgerDir = func() string { return dir }
	t.Cleanup(func() { desktopLedgerDir = old })
	scope := agentruntime.Scope{TenantID: "t", UserID: "u1"}
	empty := desktopEnsureReceipt(scope, &agentservice.Run{ID: "run_empty", Status: agentservice.RunStatusSucceeded}, nil, nil, false, false)
	if empty == nil || strings.Contains(empty.Content, "做完了") || empty.Metadata["task_outcome"] == agentservice.TaskOutcomeDone {
		t.Fatalf("empty success claimed done: %+v", empty)
	}
	if !strings.Contains(empty.Content, "没做完") {
		t.Fatalf("content=%q", empty.Content)
	}
	blank := &agentservice.Message{Attachments: []agent.MessageAttachment{{Type: "file"}}}
	blankRun := desktopEnsureReceipt(scope, &agentservice.Run{ID: "run_blank", Status: agentservice.RunStatusSucceeded}, blank, nil, false, true)
	if blankRun == nil || strings.Contains(blankRun.Content, "做完了") || blankRun.Metadata["task_outcome"] == agentservice.TaskOutcomeDone {
		t.Fatalf("empty attachment claimed done: %+v", blankRun)
	}
	nameOnly := &agentservice.Message{Attachments: []agent.MessageAttachment{{Type: "file", FileName: "report.pdf"}}}
	nameRun := desktopEnsureReceipt(scope, &agentservice.Run{ID: "run_name", Status: agentservice.RunStatusSucceeded, Metadata: map[string]string{"task_outcome": agentservice.TaskOutcomeDone}}, nameOnly, nil, false, true)
	if nameRun == nil || nameRun.Metadata["task_outcome"] == agentservice.TaskOutcomeDone || strings.Contains(nameRun.Metadata["send_to_user"], "report.pdf") {
		t.Fatalf("filename without bytes claimed delivered: %+v", nameRun)
	}
	shot := &agentservice.Message{Attachments: []agent.MessageAttachment{{Type: "image", FileName: "desktop.png", MimeType: "image/png", Data: "aGVsbG8="}}}
	shotRun := desktopEnsureReceipt(scope, &agentservice.Run{ID: "run_shot", Status: agentservice.RunStatusSucceeded}, shot, nil, false, false)
	if shotRun == nil || shotRun.Metadata["task_outcome"] == agentservice.TaskOutcomeDone {
		t.Fatalf("process screenshot claimed done: %+v", shotRun)
	}
	file := &agentservice.Message{Attachments: []agent.MessageAttachment{{Type: "file", FileName: "report.pdf", Data: "JVBERg==", Size: 12, Delivery: agent.AttachmentDeliveryFile}}}
	got := desktopEnsureReceipt(scope, &agentservice.Run{ID: "run_file", Status: agentservice.RunStatusSucceeded}, file, nil, false, true)
	if got == nil || !strings.Contains(got.Content, "report.pdf") || got.Metadata["task_outcome"] != agentservice.TaskOutcomeDone || !strings.Contains(got.Metadata["send_to_user"], "report.pdf") {
		t.Fatalf("delivered file was not acknowledged: %+v", got)
	}
	shotMarked := &agentservice.Message{Attachments: []agent.MessageAttachment{{Type: "image", FileName: "desktop.png", MimeType: "image/png", Data: "aGVsbG8=", Delivery: agent.AttachmentDeliveryShot}}}
	shotMarkedRun := desktopEnsureReceipt(scope, &agentservice.Run{ID: "run_shot_mark", Status: agentservice.RunStatusSucceeded}, shotMarked, nil, false, false)
	if shotMarkedRun == nil || shotMarkedRun.Metadata["task_outcome"] == agentservice.TaskOutcomeDone {
		t.Fatalf("a shot mark completed the task: %+v", shotMarkedRun)
	}
	failedScope := agentruntime.Scope{TenantID: "t", UserID: "u1"}
	noteParsedDeliverableContract("run_extract_fail", nil, true, "deliverables extraction was not json")
	failMsg := &agentservice.Message{Content: "报告已经发出去了。"}
	failRun := desktopEnsureReceipt(failedScope, &agentservice.Run{ID: "run_extract_fail", Status: agentservice.RunStatusSucceeded, Metadata: map[string]string{"task_outcome": agentservice.TaskOutcomeDone}}, failMsg, nil, false, true)
	if failRun == nil || failRun.Metadata["task_outcome"] != agentservice.TaskOutcomeUnverified {
		t.Fatalf("failed extraction was not unverified: %+v", failRun)
	}
	// The concrete cause stays off the person-facing sentence — it can carry a
	// local path or a token — and lives in task_outcome_evidence on the stored
	// receipt instead. A generic "deliverable extraction failed" is the `why`
	// the reply carries.
	if strings.Contains(failRun.Content, "not json") {
		t.Fatalf("concrete extraction cause leaked into the reply: %q", failRun.Content)
	}
	if ev := failRun.Metadata["task_outcome_evidence"]; !strings.Contains(ev, "not json") {
		t.Fatalf("concrete extraction cause lost from evidence: %q", ev)
	}
	blankScope := agentruntime.Scope{TenantID: "t", UserID: "u1"}
	noteParsedDeliverableContract("run_extract_blank", nil, true, "")
	blankMsg := &agentservice.Message{Content: "报告已经发出去了。"}
	blankExtraction := desktopEnsureReceipt(blankScope, &agentservice.Run{ID: "run_extract_blank", Status: agentservice.RunStatusSucceeded, Metadata: map[string]string{"task_outcome": agentservice.TaskOutcomeDone}}, blankMsg, nil, false, true)
	if blankExtraction == nil || blankExtraction.Metadata["task_outcome"] != agentservice.TaskOutcomeUnverified {
		t.Fatalf("failed extraction (no cause) was not unverified: %+v", blankExtraction)
	}
	asked := &agentservice.Message{Attachments: []agent.MessageAttachment{{Type: "file", FileName: "screen.png", MimeType: "image/png", Data: "aGVsbG8=", Delivery: agent.AttachmentDeliveryFile}}}
	askedRun := desktopEnsureReceipt(scope, &agentservice.Run{ID: "run_asked", Status: agentservice.RunStatusSucceeded}, asked, nil, false, false)
	if askedRun == nil || askedRun.Metadata["task_outcome"] != agentservice.TaskOutcomeDone || !strings.Contains(askedRun.Metadata["send_to_user"], "screen.png") {
		t.Fatalf("a delivered screenshot file was rejected: %+v", askedRun)
	}
	noteDesktopOutputContract("run_claim", []string{"一条已发布的结果"})
	claim := &agentservice.Message{Content: "已经全部发好了", Attachments: []agent.MessageAttachment{{Type: "image", FileName: "desktop.png", MimeType: "image/png", Data: "aGVsbG8="}}}
	claimRun := desktopEnsureReceipt(scope, &agentservice.Run{ID: "run_claim", Status: agentservice.RunStatusSucceeded}, claim, nil, false, false)
	if claimRun.Metadata["task_outcome"] != agentservice.TaskOutcomeUnverified || !strings.Contains(claimRun.Content, "已经全部发好了") || !strings.Contains(claimRun.Metadata["send_to_user"], desktopUnverifiedSentence) || strings.Contains(claimRun.Metadata["task_outcome"], agentservice.TaskOutcomeDone) {
		t.Fatalf("unverified claim became done: %+v", claimRun)
	}
	noteDesktopOutputSlots("run_ledger_outcome", []string{"file"})
	ledgerMsg := &agentservice.Message{Content: "完成了"}
	ledgerRun := desktopEnsureReceipt(scope, &agentservice.Run{ID: "run_ledger_outcome", Status: agentservice.RunStatusSucceeded, Metadata: map[string]string{"task_outcome": agentservice.TaskOutcomeDone}}, ledgerMsg, nil, false, false)
	if ledgerRun.Metadata["task_outcome"] != agentservice.TaskOutcomeUnverified {
		t.Fatalf("open contract was not reconciled: %+v", ledgerRun)
	}
	ledgerOK := false
	for _, entry := range desktopLedgerEntries(desktopLedgerKey(scope)) {
		if entry.Kind == "run_end" && entry.Run == "run_ledger_outcome" {
			ledgerOK = strings.Contains(entry.Detail, "outcome="+agentservice.TaskOutcomeUnverified) && !strings.Contains(entry.Detail, "outcome="+agentservice.TaskOutcomeDone)
		}
	}
	if !ledgerOK {
		t.Fatalf("run_end ledger kept the stale outcome: %+v", desktopLedgerEntries(desktopLedgerKey(scope)))
	}
	partial := &agentservice.Message{Content: "只做了一部分", Metadata: map[string]string{"task_outcome": agentservice.TaskOutcomePartial, "send_to_user": "只做了一部分"}, Attachments: []agent.MessageAttachment{{Type: "file", FileName: "note.txt", Data: "aGVsbG8=", Delivery: agent.AttachmentDeliveryFile}}}
	partialRun := desktopEnsureReceipt(scope, &agentservice.Run{ID: "run_partial", Status: agentservice.RunStatusSucceeded, Metadata: map[string]string{"task_outcome": agentservice.TaskOutcomePartial}}, partial, nil, false, false)
	if partialRun.Metadata["task_outcome"] != agentservice.TaskOutcomePartial || strings.Contains(partialRun.Content, "结果文件已经附") {
		t.Fatalf("a file washed a partial result: %+v", partialRun)
	}
	openScope := agentruntime.Scope{TenantID: "t", UserID: "u1", RunID: "run_open_publish"}
	desktopPublishRecordUpdate(openScope, func(r *desktopPublishRecord) { r.State = browser.PublishNotSubmitted })
	t.Cleanup(func() { desktopPublishRecordTake("run_open_publish") })
	washed := &agentservice.Message{Content: "已经全部发好了", Attachments: []agent.MessageAttachment{{Type: "file", FileName: "note.txt", Data: "aGVsbG8=", Delivery: agent.AttachmentDeliveryFile}}}
	washedRun := desktopEnsureReceipt(scope, &agentservice.Run{ID: "run_open_publish", Status: agentservice.RunStatusSucceeded}, washed, nil, false, false)
	if washedRun.Metadata["task_outcome"] != agentservice.TaskOutcomeUnverified {
		t.Fatalf("a file washed an unposted publish: %+v", washedRun)
	}
	liveScope := agentruntime.Scope{TenantID: "t", UserID: "u1", RunID: "run_live_publish"}
	desktopPublishRecordUpdate(liveScope, func(r *desktopPublishRecord) { r.State = browser.PublishLive })
	t.Cleanup(func() { desktopPublishRecordTake("run_live_publish") })
	live := &agentservice.Message{Content: "发出去了"}
	liveRun := desktopEnsureReceipt(scope, &agentservice.Run{ID: "run_live_publish", Status: agentservice.RunStatusSucceeded}, live, nil, false, false)
	if liveRun.Metadata["task_outcome"] != agentservice.TaskOutcomeDone || !strings.Contains(liveRun.Content, "发出去了") {
		t.Fatalf("verified publish was not done: %+v", liveRun)
	}
	answer := &agentservice.Message{Content: "巴黎是法国的首都。", Metadata: map[string]string{"task_outcome": agentservice.TaskOutcomeDone, "send_to_user": "巴黎是法国的首都。"}}
	answerRun := desktopEnsureReceipt(scope, &agentservice.Run{ID: "run_answer", Status: agentservice.RunStatusSucceeded, Metadata: map[string]string{"task_outcome": agentservice.TaskOutcomeDone}}, answer, nil, false, false)
	if answerRun.Content != "巴黎是法国的首都。" || answerRun.Metadata["task_outcome"] != agentservice.TaskOutcomeDone || answerRun.Metadata["send_to_user"] != "巴黎是法国的首都。" {
		t.Fatalf("text answer replaced: %+v", answerRun)
	}
	again := desktopEnsureReceipt(scope, &agentservice.Run{ID: "run_empty", Status: agentservice.RunStatusSucceeded}, empty, nil, false, false)
	if again.Content != empty.Content || strings.Count(again.Content, "没做完") != 1 {
		t.Fatalf("second receipt changed the sentence: %q", again.Content)
	}
}

func TestStatusLineKeepsRuntimeNoticeOnItsRun(t *testing.T) {
	t.Cleanup(func() {
		agentservice.ClearRunRuntimeStatus("run_notice_a")
		agentservice.ClearRunRuntimeStatus("run_notice_b")
	})
	agentservice.SetRunRuntimeStatus("run_notice_a", "已从「Primary/old」改用「Backup/new」，正在重新请求（第 1 次）。")
	agentservice.SetRunRuntimeStatus("run_notice_b", "重试 1/5：API 暂时不可用，2 秒后重试")
	lineA := desktopRunStatusLine("run_notice_a")
	lineB := desktopRunStatusLine("run_notice_b")
	if !strings.Contains(lineA, "Backup/new") || strings.Contains(lineA, "重试 1/5") {
		t.Fatalf("A=%q", lineA)
	}
	if !strings.Contains(lineB, "重试 1/5") || strings.Contains(lineB, "改用") {
		t.Fatalf("B=%q", lineB)
	}
	agentservice.ClearRunRuntimeStatus("run_notice_a")
	if strings.Contains(desktopRunStatusLine("run_notice_a"), "改用") {
		t.Fatal("cleared notice stayed on A")
	}
	if !strings.Contains(desktopRunStatusLine("run_notice_b"), "重试 1/5") {
		t.Fatal("clearing A dropped B")
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

func TestDesktopFileClaimCountsOwnMessageAndInfersTheSingleFileItem(t *testing.T) {
	runID := "run-file-claim-infer"
	desktopFileClaimMu.Lock()
	delete(desktopFileClaimsByRun, runID)
	desktopFileClaimMu.Unlock()
	desktopNoteFileClaim(runID, "", []byte("%PDF-1.4 report"))
	if got := desktopDeliveredFileCount(runID, nil); got != 1 {
		t.Fatalf("count=%d", got)
	}
	items := []agent.DesktopDeliverable{{ID: "file-1", Kind: desktopContractFile, Text: "报告"}}
	outcome, why := desktopAttributedVerdict(runID, &agentservice.Message{}, items)
	if outcome != agentservice.TaskOutcomeDone || why != "outputs delivered" {
		t.Fatalf("outcome=%q why=%q", outcome, why)
	}
}

func TestDesktopFileRefClaimAttributesAStoreDelivery(t *testing.T) {
	runID := "run-file-ref-claim"
	desktopFileClaimMu.Lock()
	delete(desktopFileClaimsByRun, runID)
	desktopFileClaimMu.Unlock()
	desktopNoteFileRefClaim(runID, "file-1", "bf_00000000000000000000001", "report.pdf")
	if got := desktopDeliveredFileCount(runID, nil); got != 1 {
		t.Fatalf("count=%d", got)
	}
	items := []agent.DesktopDeliverable{{ID: "file-1", Kind: desktopContractFile, Text: "报告"}}
	outcome, why := desktopAttributedVerdict(runID, &agentservice.Message{}, items)
	if outcome != agentservice.TaskOutcomeDone || why != "outputs delivered" {
		t.Fatalf("outcome=%q why=%q", outcome, why)
	}
}
