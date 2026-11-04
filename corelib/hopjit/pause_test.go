package hopjit

import (
	"context"
	"path/filepath"
	"testing"
)

func TestExecutionEngineAskPauseSurvivesRestart(t *testing.T) {
	store, err := NewSQLitePersistence(filepath.Join(t.TempDir(), "hopjit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	engine := NewExecutionEngine(store)
	init, err := engine.Init(ctx, `# Spec: 数据询问
Goal: 获取用户数据
Outputs:
- answer % line
## Steps
1. [ask] 输入名称
  + → answer % line
  > 请提供名称。
2. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	next, err := engine.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != NextPaused || next.Pause == nil || next.Pause.StepID != "1" || next.Pause.Question != "请提供名称。" {
		t.Fatalf("next = %+v", next)
	}
	restarted := NewExecutionEngine(store)
	if err := restarted.Resume(ctx, init.RunID); err != nil {
		t.Fatal(err)
	}
	next, err = restarted.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != NextPaused || next.Pause == nil || next.Pause.Question != "请提供名称。" {
		t.Fatalf("resumed next = %+v", next)
	}
	if err := restarted.Complete(ctx, "1", map[string]any{"answer": "HopJIT"}); err != nil {
		t.Fatal(err)
	}
	next, err = restarted.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != NextCompleted || next.Outputs["answer"] != "HopJIT" {
		t.Fatalf("next = %+v", next)
	}
}

func TestExecutionEngineConfirmRejectDoesNotRetry(t *testing.T) {
	engine := NewExecutionEngine(nil)
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 审批拒绝
Goal: 拒绝后终止
Outputs:
- report % text
## Steps
1. [subtask retry=3]
  1.1. [confirm require_human=true] 审批
    + → approved % bool
  1.2. [reason] 后续工作
    + → report % text
2. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	next, err := engine.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != NextPaused || next.Pause == nil || !next.Pause.RequireHuman {
		t.Fatalf("next = %+v", next)
	}
	if err := engine.Complete(ctx, "1.1", map[string]any{"approved": false}); err != nil {
		t.Fatal(err)
	}
	next, err = engine.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != NextFailed || next.Snapshot.RetryCounters["1"] != 0 || next.Snapshot.StepStates["1.2"] != StepSkipped {
		t.Fatalf("next = %+v", next)
	}
}

func TestExecutionEngineConfirmApprovalContinues(t *testing.T) {
	engine := NewExecutionEngine(nil)
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 审批通过
Goal: 批准后继续
Outputs:
- report % text
## Steps
1. [confirm] 审批
  + → approved % bool
2. [reason] 生成报告
  + → report % text
3. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	next, err := engine.Next(ctx)
	if err != nil || next.Status != NextPaused {
		t.Fatalf("next = %+v err=%v", next, err)
	}
	if err := engine.Complete(ctx, "1", map[string]any{"approved": true}); err != nil {
		t.Fatal(err)
	}
	mustNextStep(t, engine, "2")
	if err := engine.Complete(ctx, "2", map[string]any{"report": "完成"}); err != nil {
		t.Fatal(err)
	}
	next, err = engine.Next(ctx)
	if err != nil || next.Status != NextCompleted {
		t.Fatalf("next = %+v err=%v", next, err)
	}
}
