package main

import (
	"os"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func finishRunSteps(t *testing.T, run string) {
	t.Helper()
	desktopRecStateMu.Lock()
	desktopRecRunSteps[run] = true
	desktopRecStateMu.Unlock()
	t.Cleanup(func() {
		desktopRecStateMu.Lock()
		delete(desktopRecRunSteps, run)
		desktopRecStateMu.Unlock()
		desktopClearFinish(run)
		desktopRunFactMu.Lock()
		delete(desktopRunFacts, run)
		desktopRunFactMu.Unlock()
		desktopContinueTried.Delete(run)
	})
}

func TestRunFinishSuccessStatesTheResult(t *testing.T) {
	d100Env(t)
	run := "run-finish-ok"
	finishRunSteps(t, run)
	metric := 0.91
	if err := desktopLongJobSave(&desktopLongJob{
		ID: "job_ok", SourceRun: run, Status: "done", RoundMetric: &metric, RoundMetricName: "f1",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := desktopFinishTurn(agentruntime.Scope{RunID: run}, map[string]any{"outcome": "done", "text": "分类可用。"}); err != nil {
		t.Fatal(err)
	}
	raw := `{"f1":0.91,"auc":0.8}` + "\nBEST_VAL_METRICS"
	md := desktopTaskOutcomeMetadata(run, agentservice.Message{Content: raw})
	if md[agentservice.TaskOutcomeKey] != agentservice.TaskOutcomeDone {
		t.Fatalf("outcome %v", md)
	}
	got := md["send_to_user"]
	if !strings.HasPrefix(got, "做成了。") || !strings.Contains(got, "f1 0.91") || !strings.Contains(got, "分类可用。") {
		t.Fatalf("sentence %q", got)
	}
	if strings.Contains(got, "BEST_VAL_METRICS") || strings.Contains(got, "{") {
		t.Fatalf("raw leaked %q", got)
	}
	if md["outcome_status"] != "success" || md["outcome_next"] != "stop" {
		t.Fatalf("fields %v", md)
	}
	body, err := os.ReadFile(md["outcome_record"])
	if err != nil || !strings.Contains(string(body), "BEST_VAL_METRICS") {
		t.Fatalf("record %s %v", body, err)
	}
}

func TestRunFinishFailureNamesTheUnblockCondition(t *testing.T) {
	run := "run-finish-login"
	finishRunSteps(t, run)
	desktopNoteRunBlocker(run, "login_wall")
	if _, err := desktopFinishTurn(agentruntime.Scope{RunID: run}, map[string]any{"outcome": "failed", "text": `{"err":"timeout"}`}); err != nil {
		t.Fatal(err)
	}
	md := desktopTaskOutcomeMetadata(run, agentservice.Message{Content: `{"err":"cdp timeout","log":"stack"}`})
	if md[agentservice.TaskOutcomeKey] != agentservice.TaskOutcomeBlocked {
		t.Fatalf("outcome %v", md)
	}
	got := md["send_to_user"]
	if !strings.HasPrefix(got, "没有做成。") || !strings.Contains(got, "卡在：登录") || !strings.Contains(got, "要继续需要：你在这个桌面完成登录或验证") {
		t.Fatalf("sentence %q", got)
	}
	if strings.Contains(got, "cdp") || strings.Contains(got, "{") {
		t.Fatalf("raw leaked %q", got)
	}
	if md["outcome_status"] != "failure" || md["outcome_blocker"] != "login" || md["outcome_next"] != "ask_user" {
		t.Fatalf("fields %v", md)
	}
	if strings.Contains(got, "我已经自己试过处理") {
		t.Fatalf("claimed a remedy that did not happen: %q", got)
	}
}

func TestRunFinishContinuesWhenTheBotCanFixIt(t *testing.T) {
	run := "run-finish-retry"
	finishRunSteps(t, run)
	desktopNoteRunBlocker(run, "infra_timeout")
	if _, err := desktopFinishTurn(agentruntime.Scope{RunID: run}, map[string]any{"outcome": "failed", "text": "浏览器没响应"}); err != nil {
		t.Fatal(err)
	}
	md := desktopTaskOutcomeMetadata(run, agentservice.Message{Content: "trace:\n" + strings.Repeat("x", 40)})
	if md["outcome_next"] != "continue" || md["outcome_blocker"] != "resource" || md["send_to_user"] != "" {
		t.Fatalf("should continue without a terminal sentence: %v", md)
	}
	child := "run-finish-retry-2"
	finishRunSteps(t, child)
	desktopContinueTried.Store(child, "resource")
	desktopNoteRunBlocker(child, "infra_timeout")
	if _, err := desktopFinishTurn(agentruntime.Scope{RunID: child}, map[string]any{"outcome": "failed", "text": "还是不行"}); err != nil {
		t.Fatal(err)
	}
	again := desktopTaskOutcomeMetadata(child, agentservice.Message{Content: `{"still":"down"}`})
	got := again["send_to_user"]
	if again["outcome_next"] != "ask_user" || again["outcome_remediated"] != "true" {
		t.Fatalf("second try %v", again)
	}
	if !strings.HasPrefix(got, "没有做成。") || !strings.Contains(got, "我已经自己试过处理。") || !strings.Contains(got, "要继续需要：") {
		t.Fatalf("sentence %q", got)
	}
	if strings.Contains(got, "still") || strings.Contains(got, "{") {
		t.Fatalf("raw leaked %q", got)
	}
}
