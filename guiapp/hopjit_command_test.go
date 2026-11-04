package guiapp

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/hopjit"
	"github.com/RapidAI/CodeClaw/corelib/hopspec"
)

func TestProjectHopRunFromEngineSnapshot(t *testing.T) {
	engine := hopjit.NewExecutionEngine(nil)
	init, err := engine.Init(context.Background(), `# Spec: 投影
Id: projection-test
Goal: 验证 HopRun 投影
Outputs:
- report % text
## Steps
1. [reason] 生成
  + → report % text
2. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := engine.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	run := ProjectHopRun(snapshot)
	if run.RunID != init.RunID || run.Status != "running" || run.Remaining != 2 || run.Steps[0].Status != HopStepRunning {
		t.Fatalf("run=%+v", run)
	}
	if err := engine.Complete(context.Background(), "1", map[string]any{"report": "完成"}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = engine.Snapshot()
	run = ProjectHopRun(snapshot)
	if run.Status != "completed" || run.Remaining != 0 || run.Steps[0].Status != HopStepCompleted {
		t.Fatalf("run=%+v", run)
	}
}

func TestHopJITPauseResponseIncludesConfirmationActions(t *testing.T) {
	snapshot := hopjit.EngineSnapshot{
		RunID: "run-test",
		Spec:  structSpecForHopProjection(),
		Pause: &hopjit.PauseState{StepID: "1", Kind: "confirm", Question: "是否提交？", RequireHuman: true},
	}
	response := hopJITSnapshotResponse(snapshot)
	if response.Confirmation == nil || response.Confirmation.ID != "run-test" || len(response.Actions) != 2 {
		t.Fatalf("response=%+v", response)
	}
	if !strings.Contains(response.Actions[0].Command, `"approved":true`) || !strings.Contains(response.Actions[1].Command, `"approved":false`) {
		t.Fatalf("actions=%+v", response.Actions)
	}
}

func TestParseHopJITCommand(t *testing.T) {
	action, payload, ok := parseHopJITCommand("/hop resume run-1 {\"answer\":\"继续\"}")
	if !ok || action != "resume" {
		t.Fatalf("action=%s ok=%v", action, ok)
	}
	runID, answers, err := parseHopJITResume(payload)
	if err != nil || runID != "run-1" || answers != `{"answer":"继续"}` {
		t.Fatalf("runID=%s answers=%s err=%v", runID, answers, err)
	}
	if _, _, ok := parseHopJITCommand("/hope test"); ok {
		t.Fatal("non-hop command was intercepted")
	}
}

func TestHopCommandUsesExistingImmediateDispatcher(t *testing.T) {
	handler := &IMMessageHandler{}
	resp, handled := handler.handleImmediateIMCommand(IMUserMessage{Text: "/hop"}, "/hop", nil, nil)
	if !handled || resp == nil || !strings.Contains(resp.Text, "/hop resume") {
		t.Fatalf("resp=%+v handled=%v", resp, handled)
	}
}

func structSpecForHopProjection() (spec hopspec.SpecAST) {
	spec.ID = "confirm-test"
	spec.Goal = "确认测试"
	return spec
}
