package hopjit

import (
	"context"
	"errors"
	"testing"
)

type reuseTestCaller struct {
	execute func(StepReady) (map[string]any, error)
	replan  func(EngineSnapshot) (string, error)
}

func (c *reuseTestCaller) ExecuteStep(_ context.Context, step StepReady) (map[string]any, error) {
	return c.execute(step)
}

func (c *reuseTestCaller) Replan(_ context.Context, snapshot EngineSnapshot) (string, error) {
	return c.replan(snapshot)
}

func TestDriveReuseCompletesCallerSteps(t *testing.T) {
	engine := NewExecutionEngine(nil)
	_, err := engine.Init(context.Background(), `# Spec: Reuse
Goal: 调用方执行
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
	caller := &reuseTestCaller{execute: func(step StepReady) (map[string]any, error) {
		return map[string]any{"report": "由 " + step.StepID + " 生成"}, nil
	}}
	result, err := DriveReuse(context.Background(), engine, caller)
	if err != nil || result.Status != NextCompleted || result.Outputs["report"] != "由 1 生成" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestDriveReuseRetriesCallerFailure(t *testing.T) {
	engine := NewExecutionEngine(nil)
	_, err := engine.Init(context.Background(), `# Spec: ReuseRetry
Goal: 临时失败后重试
Outputs:
- report % text
## Steps
1. [subtask retry=1]
  + → report % text
  1.1. [reason] 生成
    + → report % text
2. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	caller := &reuseTestCaller{execute: func(StepReady) (map[string]any, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("临时失败")
		}
		return map[string]any{"report": "成功"}, nil
	}}
	result, err := DriveReuse(context.Background(), engine, caller)
	if err != nil || result.Status != NextCompleted || calls != 2 {
		t.Fatalf("result=%+v calls=%d err=%v", result, calls, err)
	}
}

func TestDriveReuseReturnsPause(t *testing.T) {
	engine := NewExecutionEngine(nil)
	_, err := engine.Init(context.Background(), `# Spec: ReusePause
Goal: 等待回答
## Steps
1. [ask] 问题
  + → answer % text
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	caller := &reuseTestCaller{execute: func(StepReady) (map[string]any, error) {
		t.Fatal("paused step called caller")
		return nil, nil
	}}
	result, err := DriveReuse(context.Background(), engine, caller)
	if err != nil || result.Status != NextPaused || result.Pause == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
