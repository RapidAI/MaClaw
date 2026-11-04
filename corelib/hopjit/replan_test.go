package hopjit

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutionEngineAdaptiveReplanRestoresContainer(t *testing.T) {
	engine := NewExecutionEngine(nil)
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 自适应修复
Goal: 失败后替换执行路径
Outputs:
- report % text
## Steps
1. [subtask adaptive retry=2] 可调整容器
  + → report % text
  1.1. [reason] 原路径
    + → report % text
2. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	mustNextStep(t, engine, "1.1")
	if err := engine.Fail(ctx, "1.1", Failure{Kind: FailureTool, Reason: "原路径不可用"}); err != nil {
		t.Fatal(err)
	}
	next, err := engine.Next(ctx)
	if err != nil || next.Status != NextAdaptiveNeeded || next.Snapshot.AdaptiveNeeded != "1" {
		t.Fatalf("next = %+v err=%v", next, err)
	}
	if err := engine.Replan(ctx, "1", `1. [reason] 新路径`); err == nil || !strings.Contains(err.Error(), "REPLAN_OUTPUT_MISSING") {
		t.Fatalf("missing output replan error = %v", err)
	}
	if err := engine.Replan(ctx, "1", `1. [reason] 新路径
  + → report % text`); err != nil {
		t.Fatal(err)
	}
	mustNextStep(t, engine, "1.1")
	if err := engine.Complete(ctx, "1.1", map[string]any{"report": "修复成功"}); err != nil {
		t.Fatal(err)
	}
	next, err = engine.Next(ctx)
	if err != nil || next.Status != NextCompleted || next.Outputs["report"] != "修复成功" {
		t.Fatalf("next = %+v err=%v", next, err)
	}
}

func TestExecutionEngineReplanPreservesFinalCheckAndForbidsCommit(t *testing.T) {
	engine := NewExecutionEngine(nil)
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 重规划边界
Goal: 保留最终核验
Outputs:
- report % text
## Steps
1. [subtask adaptive]
  + → report % text
  1.1. [reason] 原路径
    + → report % text
  1.2. [check final] 最终核验
    - ← report
    + → accepted % bool
    + → feedback % text
2. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	mustNextStep(t, engine, "1.1")
	if err := engine.Fail(ctx, "1.1", Failure{Kind: FailureError, Reason: "失败"}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Replan(ctx, "1", `1. [reason] 无最终核验
  + → report % text`); err == nil || !strings.Contains(err.Error(), "REPLAN_FINAL_CHECK_REQUIRED") {
		t.Fatalf("final check error = %v", err)
	}
	if err := engine.Replan(ctx, "1", `1. [commit] 动态提交
  + → report % text
2. [check final] 最终核验
  - ← report
  + → accepted % bool
  + → feedback % text`); err == nil || !strings.Contains(err.Error(), "REPLAN_COMMIT_FORBIDDEN") {
		t.Fatalf("commit error = %v", err)
	}
	if err := engine.Replan(ctx, "1", `1. [reason] 新路径
  + → report % text
2. [check final] 最终核验
  - ← report
  + → accepted % bool
  + → feedback % text`); err != nil {
		t.Fatal(err)
	}
	mustNextStep(t, engine, "1.1")
	if err := engine.Complete(ctx, "1.1", map[string]any{"report": "结果"}); err != nil {
		t.Fatal(err)
	}
	mustNextStep(t, engine, "1.2")
	if err := engine.Complete(ctx, "1.2", map[string]any{"accepted": true, "feedback": ""}); err != nil {
		t.Fatal(err)
	}
	next, err := engine.Next(ctx)
	if err != nil || next.Status != NextCompleted {
		t.Fatalf("next = %+v err=%v", next, err)
	}
}

func TestExecutionEngineRejectsDuplicateReplan(t *testing.T) {
	engine := NewExecutionEngine(nil)
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 重复重规划
Goal: 拒绝相同修复
## Steps
1. [subtask adaptive]
  1.1. [reason] 原路径
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan := "1. [reason] 相同的新路径"
	for attempt := 0; attempt < 2; attempt++ {
		mustNextStep(t, engine, "1.1")
		if err := engine.Fail(ctx, "1.1", Failure{Kind: FailureError, Reason: "失败"}); err != nil {
			t.Fatal(err)
		}
		if attempt == 0 {
			if err := engine.Replan(ctx, "1", plan); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := engine.Replan(ctx, "1", plan); err == nil || !strings.Contains(err.Error(), "REPLAN_DUPLICATE") {
			t.Fatalf("duplicate replan error = %v", err)
		}
	}
}

func TestExecutionEngineAdaptiveNeedSurvivesRestart(t *testing.T) {
	store, err := NewSQLitePersistence(filepath.Join(t.TempDir(), "hopjit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	engine := NewExecutionEngine(store)
	init, err := engine.Init(ctx, `# Spec: 恢复重规划
Goal: 重启后仍等待重规划
## Steps
1. [subtask adaptive]
  1.1. [reason] 失败步骤
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	mustNextStep(t, engine, "1.1")
	if err := engine.Fail(ctx, "1.1", Failure{Kind: FailureError, Reason: "失败"}); err != nil {
		t.Fatal(err)
	}
	restarted := NewExecutionEngine(store)
	if err := restarted.Resume(ctx, init.RunID); err != nil {
		t.Fatal(err)
	}
	next, err := restarted.Next(ctx)
	if err != nil || next.Status != NextAdaptiveNeeded || next.Snapshot.AdaptiveNeeded != "1" {
		t.Fatalf("next = %+v err=%v", next, err)
	}
}
