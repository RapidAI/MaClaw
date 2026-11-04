package hopjit

import (
	"context"
	"testing"
)

func TestExecutionEngineRetriesOnlyOwningSubtask(t *testing.T) {
	engine := NewExecutionEngine(nil)
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 容器重试
Goal: 失败后重跑所属容器
Outputs:
- report % text
## Steps
1. [subtask retry=2] 可重试容器
  1.1. [reason] 生成报告
    + → report % text
2. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	mustNextStep(t, engine, "1.1")
	if err := engine.Fail(ctx, "1.1", Failure{Kind: FailureTool, Reason: "临时故障"}); err != nil {
		t.Fatal(err)
	}
	next := mustNextStep(t, engine, "1.1")
	if next.Snapshot.RetryCounters["1"] != 1 || len(next.Snapshot.RetryHistory["1"]) != 1 {
		t.Fatalf("retry state = %+v", next.Snapshot)
	}
	if err := engine.Complete(ctx, "1.1", map[string]any{"report": "成功"}); err != nil {
		t.Fatal(err)
	}
	next, err = engine.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != NextCompleted || next.Outputs["report"] != "成功" {
		t.Fatalf("next = %+v", next)
	}
}

func TestExecutionEngineCheckFailureRetriesContainer(t *testing.T) {
	engine := NewExecutionEngine(nil)
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 核验重试
Goal: 核验失败后重做
Outputs:
- report % text
## Steps
1. [subtask retry=1]
  1.1. [reason] 生成
    + → report % text
  1.2. [check] 核验
    - ← report
    + → accepted % bool
    + → feedback % text
2. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	mustNextStep(t, engine, "1.1")
	if err := engine.Complete(ctx, "1.1", map[string]any{"report": "初稿"}); err != nil {
		t.Fatal(err)
	}
	mustNextStep(t, engine, "1.2")
	if err := engine.Complete(ctx, "1.2", map[string]any{"accepted": false, "feedback": "需要修订"}); err != nil {
		t.Fatal(err)
	}
	next := mustNextStep(t, engine, "1.1")
	if next.Snapshot.RetryCounters["1"] != 1 || next.Snapshot.TerminalState != "" {
		t.Fatalf("next = %+v", next)
	}
}

func TestExecutionEngineRunsOnFailAfterRetryExhaustion(t *testing.T) {
	engine := NewExecutionEngine(nil)
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 失败兜底
Goal: 耗尽后走兜底
Outputs:
- report % text
## Steps
1. [subtask retry=0] 主路径
  1.1. [reason] 主路径失败
  1.2. [on_fail] 兜底
    1.2.1. [reason] 生成降级报告
      + → report % text
2. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	mustNextStep(t, engine, "1.1")
	if err := engine.Fail(ctx, "1.1", Failure{Kind: FailureDeterministic, Reason: "无法完成"}); err != nil {
		t.Fatal(err)
	}
	next := mustNextStep(t, engine, "1.2.1")
	if next.Snapshot.RetryCounters["1"] != 0 || !next.Snapshot.OnFailActive["1"] {
		t.Fatalf("fallback state = %+v", next.Snapshot)
	}
	if err := engine.Complete(ctx, "1.2.1", map[string]any{"report": "降级结果"}); err != nil {
		t.Fatal(err)
	}
	next, err = engine.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != NextCompleted || next.Outputs["report"] != "降级结果" {
		t.Fatalf("next = %+v", next)
	}
}
