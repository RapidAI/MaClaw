package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestLongJobOutcomeHidesTheRawTail(t *testing.T) {
	d100Env(t)
	dir := t.TempDir()
	tail := "epoch 3/3 [██████████] 100%\nBEST_VAL_METRICS {\"f1\": 0.81, \"auc\": 0.77}\n"
	var said []map[string]any
	prevSay := desktopLongJobSayFunc
	t.Cleanup(func() { desktopLongJobSayFunc = prevSay })
	desktopLongJobSayFunc = func(_ context.Context, body map[string]any) error {
		said = append(said, body)
		return nil
	}
	var prompts []string
	prevSend := desktopLongJobOutcomeSend
	t.Cleanup(func() { desktopLongJobOutcomeSend = prevSend })
	desktopLongJobOutcomeSend = func(_ context.Context, _ *desktopLongJob, prompt string) (string, error) {
		prompts = append(prompts, prompt)
		return "run_sum", nil
	}
	j := &desktopLongJob{
		ID: "job_done", InstanceID: "inst", SessionID: "sess", Status: "done", Alive: false,
		Dir: dir, LastTail: tail, Started: time.Now().Add(-time.Hour),
	}
	if err := desktopLongJobSave(j); err != nil {
		t.Fatal(err)
	}
	if err := desktopLongJobTalkDefault(context.Background(), j, longJobResume); err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || !strings.Contains(prompts[0], "BEST_VAL_METRICS") || !strings.Contains(prompts[0], "不要把日志原文") {
		t.Fatalf("prompt %v", prompts)
	}
	for _, body := range said {
		text, _ := body["text"].(string)
		if strings.Contains(text, "BEST_VAL_METRICS") || strings.Contains(text, "████") || strings.Contains(text, "下面是最后的输出") {
			t.Fatalf("a finished job was shown: %q", text)
		}
	}
	if len(desktopProcessOf(j.ID)) == 0 {
		t.Fatal("the exit was not kept for the loop")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "completion.json"))
	if err != nil || !strings.Contains(string(raw), "BEST_VAL_METRICS") {
		t.Fatalf("record %s %v", raw, err)
	}
}

func TestLongJobOutcomeOneSummaryPerGoal(t *testing.T) {
	d100Env(t)
	var prompts []string
	prevSend := desktopLongJobOutcomeSend
	t.Cleanup(func() { desktopLongJobOutcomeSend = prevSend })
	desktopLongJobOutcomeSend = func(_ context.Context, _ *desktopLongJob, prompt string) (string, error) {
		prompts = append(prompts, prompt)
		return "run_one", nil
	}
	a := &desktopLongJob{ID: "job_a", InstanceID: "inst", SessionID: "same", Status: "done", LastTail: "TAIL_ALPHA_METRIC 0.81", Started: time.Now().Add(-2 * time.Hour)}
	b := &desktopLongJob{ID: "job_b", InstanceID: "inst", SessionID: "same", Status: "failed", Alive: false, PID: "4242", LastTail: "TAIL_BETA_METRIC boom", Started: time.Now().Add(-time.Hour)}
	if err := desktopLongJobSave(a); err != nil {
		t.Fatal(err)
	}
	if err := desktopLongJobSave(b); err != nil {
		t.Fatal(err)
	}
	if err := desktopLongJobScheduleOutcome(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if err := desktopLongJobScheduleOutcome(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 {
		t.Fatalf("summaries %d", len(prompts))
	}
	if !strings.Contains(prompts[0], "TAIL_ALPHA_METRIC") || !strings.Contains(prompts[0], "TAIL_BETA_METRIC") || !strings.Contains(prompts[0], "exit_kind=no_exit") {
		t.Fatalf("prompt %s", prompts[0])
	}
	if got := desktopLongJobGet(b.ID); got.OutcomeRun != "run_one" {
		t.Fatalf("second job %+v", got)
	}
}

func TestLongJobOutcomeContinuesAfterTheBlockingRun(t *testing.T) {
	d100Env(t)
	svc, store, p, inst := longJobOwnerService(t)
	prevSvc := desktopLongJobService
	t.Cleanup(func() { desktopLongJobService = prevSvc })
	desktopLongJobService = svc
	if err := store.SaveRun(agentservice.Run{
		ID: "run_busy", TenantID: p.TenantID, UserID: p.UserID, InstanceID: inst.ID,
		SessionID: "sess", Status: agentservice.RunStatusRunning, StartedAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	var prompts []string
	prevSend := desktopLongJobOutcomeSend
	t.Cleanup(func() { desktopLongJobOutcomeSend = prevSend })
	desktopLongJobOutcomeSend = func(_ context.Context, _ *desktopLongJob, prompt string) (string, error) {
		prompts = append(prompts, prompt)
		return "run_next", nil
	}
	metric := 0.81
	j := &desktopLongJob{
		ID: "job_go", InstanceID: inst.ID, SessionID: "sess", Status: "done", Alive: false,
		PrincipalTenant: p.TenantID, PrincipalUser: p.UserID,
		Decision: "continue", DecisionReason: "再训一轮", RoundMetric: &metric, RoundMetricName: "f1",
		LastTail: "TAIL_CONTINUE_METRIC f1=0.81", Started: time.Now().Add(-time.Hour),
	}
	if err := desktopLongJobSave(j); err != nil {
		t.Fatal(err)
	}
	if err := desktopLongJobScheduleOutcome(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 0 {
		t.Fatalf("started while a run was going: %d", len(prompts))
	}
	if got := desktopLongJobGet(j.ID); got.OutcomeState != "queued" || got.OutcomeWait != "run_busy" {
		t.Fatalf("queue %+v", got)
	}
	busy := agentservice.Run{
		ID: "run_busy", TenantID: p.TenantID, UserID: p.UserID, InstanceID: inst.ID,
		SessionID: "sess", Status: agentservice.RunStatusSucceeded, StartedAt: time.Now().Add(-time.Minute),
	}
	if err := store.SaveRun(busy); err != nil {
		t.Fatal(err)
	}
	desktopLongJobReleaseOutcomes("run_busy")
	if len(prompts) != 1 || !strings.Contains(prompts[0], "自己启动下一轮") || strings.Contains(prompts[0], "下面是最后的输出") {
		t.Fatalf("prompts %v", prompts)
	}
	if !strings.Contains(prompts[0], "TAIL_CONTINUE_METRIC") {
		t.Fatal("model context lost the log")
	}
	if _, refuse := desktopLongJobIterationGate(desktopLongJobGet(j.ID)); refuse != "" {
		t.Fatalf("continue refused: %s", refuse)
	}
}

func TestLongJobOutcomeAsksInsteadOfContinuing(t *testing.T) {
	d100Env(t)
	var prompts []string
	prevSend := desktopLongJobOutcomeSend
	t.Cleanup(func() { desktopLongJobOutcomeSend = prevSend })
	desktopLongJobOutcomeSend = func(_ context.Context, _ *desktopLongJob, prompt string) (string, error) {
		prompts = append(prompts, prompt)
		return "run_ask", nil
	}
	var said []map[string]any
	prevSay := desktopLongJobSayFunc
	t.Cleanup(func() { desktopLongJobSayFunc = prevSay })
	desktopLongJobSayFunc = func(_ context.Context, body map[string]any) error {
		said = append(said, body)
		return nil
	}
	metric := 0.4
	j := &desktopLongJob{
		ID: "job_ask", InstanceID: "inst", SessionID: "sess", Status: "failed", Alive: false,
		Decision: "ask_user", DecisionReason: "换数据集要这个人定", RoundMetric: &metric, RoundMetricName: "f1",
		LastTail: "TAIL_ASK_LOG failed to read shard", Started: time.Now().Add(-time.Hour),
	}
	if err := desktopLongJobSave(j); err != nil {
		t.Fatal(err)
	}
	if err := desktopLongJobTalkDefault(context.Background(), j, longJobResume); err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || !strings.Contains(prompts[0], "不要自己开下一轮") || !strings.Contains(prompts[0], "TAIL_ASK_LOG") {
		t.Fatalf("prompt %v", prompts)
	}
	if _, refuse := desktopLongJobIterationGate(j); refuse == "" {
		t.Fatal("ask_user was allowed to start another round")
	}
	for _, body := range said {
		text, _ := body["text"].(string)
		kind, _ := body["message_kind"].(string)
		if kind == desktopMsgProcess {
			continue
		}
		if strings.Contains(text, "TAIL_ASK_LOG") {
			t.Fatalf("raw log was an outcome: %q", text)
		}
	}
}

func TestWatchFollowSaysTheStructuredSummaryNotTheLog(t *testing.T) {
	d100Env(t)
	prevSleep := desktopLongJobSleep
	desktopLongJobSleep = func(context.Context, time.Duration) bool { return true }
	t.Cleanup(func() { desktopLongJobSleep = prevSleep })
	runID := "run_struct_follow"
	desktopRecStateMu.Lock()
	desktopRecRunSteps[runID] = true
	desktopRecStateMu.Unlock()
	t.Cleanup(func() {
		desktopRecStateMu.Lock()
		delete(desktopRecRunSteps, runID)
		desktopRecStateMu.Unlock()
	})
	metric := 0.91
	if err := desktopLongJobSave(&desktopLongJob{
		ID: "job_struct", SourceRun: runID, Status: "done",
		RoundMetric: &metric, RoundMetricName: "F1",
	}); err != nil {
		t.Fatal(err)
	}
	raw := "下面是最后的输出\nBEST_VAL_METRICS {\"f1\": 0.1}\n" + strings.Repeat("epoch log\n", 20)
	got := desktopLongJobDeliveredOutcome(runID, agentservice.Message{Content: raw})
	for _, bad := range []string{"BEST_VAL", "下面是最后的输出", "epoch log"} {
		if strings.Contains(got, bad) {
			t.Fatalf("process log became the result %q in %q", bad, got)
		}
	}
	if !strings.Contains(got, "做成了") || !strings.Contains(got, "F1") {
		t.Fatalf("summary %q", got)
	}
	plain := "训练变慢，会超过时限。"
	if kept := desktopLongJobDeliveredOutcome("run_no_steps_follow", agentservice.Message{Content: plain}); kept != plain {
		t.Fatalf("a reply with no structured finish was replaced: %q", kept)
	}

	svc, store, p, inst := longJobOwnerService(t)
	prevSvc := desktopLongJobService
	t.Cleanup(func() { desktopLongJobService = prevSvc })
	desktopLongJobService = svc
	session, err := svc.CreateSession(context.Background(), p, inst.ID, agentservice.CreateSessionInput{Title: "watch"})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := session.ID
	if err := store.SaveMessage(agentservice.Message{
		ID: "msg_struct", SessionID: sessionID, TenantID: p.TenantID, UserID: p.UserID, InstanceID: inst.ID,
		Role: agentservice.MessageRoleAssistant, Content: raw, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	done := time.Now()
	if err := store.SaveRun(agentservice.Run{
		ID: runID, TenantID: p.TenantID, UserID: p.UserID, InstanceID: inst.ID, SessionID: sessionID,
		AssistantMessageID: "msg_struct", Status: agentservice.RunStatusSucceeded,
		StartedAt: done.Add(-time.Minute), CompletedAt: &done,
	}); err != nil {
		t.Fatal(err)
	}
	status, reply, err := desktopLongJobRunGetter(context.Background(), p, inst.ID, sessionID, runID)
	if err != nil || status != agentservice.RunStatusSucceeded {
		t.Fatalf("getter %s %v", status, err)
	}
	for _, bad := range []string{"BEST_VAL", "下面是最后的输出", "epoch log"} {
		if strings.Contains(reply, bad) {
			t.Fatalf("the watch getter returned the process log %q in %q", bad, reply)
		}
	}
	if !strings.Contains(reply, "做成了") {
		t.Fatalf("getter reply %q", reply)
	}
	var sent []map[string]any
	oldSay := desktopLongJobSayFunc
	t.Cleanup(func() { desktopLongJobSayFunc = oldSay })
	desktopLongJobSayFunc = func(_ context.Context, body map[string]any) error {
		sent = append(sent, body)
		return nil
	}
	watch := &desktopLongJob{ID: "job_wstruct", InstanceID: inst.ID, SessionID: sessionID, UserID: p.UserID, TenantID: p.TenantID}
	desktopLongJobFollowRun(context.Background(), p, watch, runID, longJobResume)
	if len(sent) != 1 {
		t.Fatalf("sent %v", sent)
	}
	text, _ := sent[0]["text"].(string)
	if sent[0]["message_kind"] != desktopMsgOutcome {
		t.Fatalf("kind %v", sent[0]["message_kind"])
	}
	for _, bad := range []string{"BEST_VAL", "下面是最后的输出", "epoch log"} {
		if strings.Contains(text, bad) {
			t.Fatalf("follow posted the process log %q in %q", bad, text)
		}
	}
	if !strings.Contains(text, "做成了") {
		t.Fatalf("follow text %q", text)
	}
}
