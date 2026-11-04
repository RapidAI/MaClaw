package hopjit

import (
	"context"
	"testing"
)

type callTestCaller struct{}

func (callTestCaller) ExecuteStep(_ context.Context, step StepReady) (map[string]any, error) {
	return map[string]any{"result": "处理" + step.Inputs["document"].(string)}, nil
}

func (callTestCaller) Replan(context.Context, EngineSnapshot) (string, error) {
	return "", nil
}

func TestCallRunsPersistentChildAndCollectsOutput(t *testing.T) {
	provider := MapSpecProvider{"child": `# Spec: 子规约
Goal: 处理输入
Inputs:
- document % text
Outputs:
- result % text
## Steps
1. [reason] 处理输入
  - ← document
  + → result % text
2. [exit]
`}
	engine := NewExecutionEngine(nil, WithSpecProvider(provider))
	_, err := engine.Init(context.Background(), `# Spec: 父规约
Goal: 调用子规约
Inputs:
- document % text
Outputs:
- report % text
## Steps
1. [call child(document: document)] 调用子规约
  + → report: result
2. [exit]
`, map[string]any{"document": "输入"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := DriveReuse(context.Background(), engine, callTestCaller{})
	if err != nil || result.Status != NextCompleted || result.Outputs["report"] != "处理输入" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(result.Snapshot.Calls) != 1 || result.Snapshot.Calls["1"].RunID == "" {
		t.Fatalf("calls=%+v", result.Snapshot.Calls)
	}
}

func TestCallPauseResumesChildBeforeCompletingParent(t *testing.T) {
	provider := MapSpecProvider{"child": `# Spec: 子暂停
Goal: 获取回答
Outputs:
- result % text
## Steps
1. [ask] 输入答案
  + → result % text
2. [exit]
`}
	store := NewMemoryPersistence()
	engine := NewExecutionEngine(store, WithSpecProvider(provider))
	init, err := engine.Init(context.Background(), `# Spec: 父暂停
Goal: 等待子暂停
Outputs:
- report % text
## Steps
1. [call child] 调用子暂停
  + → report: result
2. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := DriveReuse(context.Background(), engine, callTestCaller{})
	if err != nil || result.Status != NextPaused || result.Pause == nil || result.Pause.ChildRunID == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	restarted := NewExecutionEngine(store, WithSpecProvider(provider))
	if err := restarted.Resume(context.Background(), init.RunID); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Complete(context.Background(), result.Pause.StepID, map[string]any{"result": "回答"}); err != nil {
		t.Fatal(err)
	}
	result, err = DriveReuse(context.Background(), restarted, callTestCaller{})
	if err != nil || result.Status != NextCompleted || result.Outputs["report"] != "回答" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestCallMissingSpecFailsParent(t *testing.T) {
	engine := NewExecutionEngine(nil, WithSpecProvider(MapSpecProvider{}))
	_, err := engine.Init(context.Background(), `# Spec: 缺失子规约
Goal: 不得假绿
## Steps
1. [call missing] 缺失
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := DriveReuse(context.Background(), engine, callTestCaller{})
	if err != nil || result.Status != NextFailed {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
