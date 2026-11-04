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
	for _, command := range []string{"/hop help", "/hop usage", "/hop --help", "/hop -h"} {
		action, _, ok := parseHopJITCommand(command)
		if !ok || action != "usage" {
			t.Fatalf("command=%q action=%s ok=%v", command, action, ok)
		}
	}
	action, payload, ok = parseHopJITCommand("/hop compile 生成摘要")
	if !ok || action != "compile" || payload != "生成摘要" {
		t.Fatalf("action=%s payload=%q ok=%v", action, payload, ok)
	}
	if _, _, ok := parseHopJITCommand("/hope test"); ok {
		t.Fatal("non-hop command was intercepted")
	}
}

func TestParseHopJITRunPayloadWithInputs(t *testing.T) {
	spec, inputs, err := parseHopJITRunPayload(`--inputs {"document":"包含 -- 分隔符的文本","count":2} -- # Spec: 输入测试
Goal: 验证输入
## Steps
1. [reason] 处理`)
	if err != nil || !strings.HasPrefix(spec, "# Spec:") || inputs["document"] != "包含 -- 分隔符的文本" || inputs["count"] != float64(2) {
		t.Fatalf("spec=%q inputs=%v err=%v", spec, inputs, err)
	}
	payload, inputs, err := parseHopJITRunPayload("生成摘要")
	if err != nil || payload != "生成摘要" || inputs != nil {
		t.Fatalf("payload=%q inputs=%v err=%v", payload, inputs, err)
	}
	for _, invalid := range []string{
		`--inputs {`,
		`--inputs null -- # Spec: x`,
		`--inputs {} # Spec: x`,
		`--inputs {} -- 普通任务`,
	} {
		if _, _, err := parseHopJITRunPayload(invalid); err == nil {
			t.Fatalf("invalid payload %q was accepted", invalid)
		}
	}
}

func TestHopCommandUsesExistingImmediateDispatcher(t *testing.T) {
	handler := &IMMessageHandler{}
	for _, command := range []string{"/hop", "/hop help"} {
		resp, handled := handler.handleImmediateIMCommand(IMUserMessage{Text: command}, command, nil, nil)
		if !handled || resp == nil || !strings.Contains(resp.Text, "/hop resume") || !strings.Contains(resp.Text, "/hop compile") {
			t.Fatalf("command=%q resp=%+v handled=%v", command, resp, handled)
		}
	}
}

func TestHopJITRunOwnerAuthorization(t *testing.T) {
	snapshot := hopjit.EngineSnapshot{OwnerID: "user-a"}
	if err := authorizeHopJITSnapshot(snapshot, hopJITExecutionIdentity{PrincipalID: "user-a"}); err != nil {
		t.Fatal(err)
	}
	for _, identity := range []hopJITExecutionIdentity{{PrincipalID: "user-b"}, {}} {
		if err := authorizeHopJITSnapshot(snapshot, identity); err != hopjit.ErrRunAccessDenied {
			t.Fatalf("identity=%+v err=%v", identity, err)
		}
	}
	if err := authorizeHopJITSnapshot(hopjit.EngineSnapshot{}, hopJITExecutionIdentity{PrincipalID: "user-a"}); err != hopjit.ErrRunAccessDenied {
		t.Fatalf("legacy snapshot was visible to an authenticated command: %v", err)
	}
}

func structSpecForHopProjection() (spec hopspec.SpecAST) {
	spec.ID = "confirm-test"
	spec.Goal = "确认测试"
	return spec
}
