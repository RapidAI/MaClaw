package hopjit

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

const engineReviewSpec = `# Spec: 文档评审
Id: document-review
Goal: 读取材料并产出经过核验的摘要
Inputs:
- document % text
Outputs:
- summary % text
## Steps
1. [reason] 生成摘要
  - ← document
  + → summary % text
  > 提取原文核心内容。
2. [check] 核验摘要
  - ← document, summary
  + → accepted % bool
  + → feedback % text
3. [exit] 交付摘要
`

func TestExecutionEngineRunsReadOnlyReview(t *testing.T) {
	engine := NewExecutionEngine(nil)
	ctx := context.Background()
	init, err := engine.Init(ctx, engineReviewSpec, map[string]any{"document": "HopJIT 管理步骤状态。"})
	if err != nil {
		t.Fatal(err)
	}
	if init.RunID == "" {
		t.Fatal("empty run id")
	}
	next, err := engine.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != NextStepReady || next.Step == nil || next.Step.StepID != "1" {
		t.Fatalf("first next = %+v", next)
	}
	if next.Step.Inputs["document"] != "HopJIT 管理步骤状态。" {
		t.Fatalf("inputs = %+v", next.Step.Inputs)
	}
	if err := engine.Complete(ctx, "1", map[string]any{"summary": "执行引擎管理状态"}); err != nil {
		t.Fatal(err)
	}
	next, err = engine.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != NextStepReady || next.Step.StepID != "2" || next.Step.Inputs["summary"] != "执行引擎管理状态" {
		t.Fatalf("check next = %+v", next)
	}
	if err := engine.Complete(ctx, "2", map[string]any{"accepted": true, "feedback": ""}); err != nil {
		t.Fatal(err)
	}
	next, err = engine.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != NextCompleted || next.Outputs["summary"] != "执行引擎管理状态" {
		t.Fatalf("final next = %+v", next)
	}
	if next.Snapshot.TerminalState != TerminalCompleted || next.Snapshot.StepStates["3"] != StepDone {
		t.Fatalf("snapshot = %+v", next.Snapshot)
	}
	if len(next.Snapshot.Events) < 6 {
		t.Fatalf("events = %+v", next.Snapshot.Events)
	}
}

func TestExecutionEngineRejectsIncompleteDeclaredOutputs(t *testing.T) {
	engine := NewExecutionEngine(nil)
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 空输出
Goal: 验证完备性
Outputs:
- report % text
## Steps
1. [reason] 生成报告
  + → report % text
2. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Next(ctx); err != nil {
		t.Fatal(err)
	}
	if err := engine.Complete(ctx, "1", map[string]any{"report": nil}); err != nil {
		t.Fatal(err)
	}
	next, err := engine.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != NextFailed || next.Failure == nil || !strings.Contains(next.Failure.Reason, "OUTPUT_INCOMPLETE") {
		t.Fatalf("next = %+v", next)
	}
}

func TestExecutionEngineCheckFailureIsNotSuccess(t *testing.T) {
	engine := NewExecutionEngine(nil)
	ctx := context.Background()
	if _, err := engine.Init(ctx, engineReviewSpec, map[string]any{"document": "原文"}); err != nil {
		t.Fatal(err)
	}
	mustNextStep(t, engine, "1")
	if err := engine.Complete(ctx, "1", map[string]any{"summary": "错误摘要"}); err != nil {
		t.Fatal(err)
	}
	mustNextStep(t, engine, "2")
	if err := engine.Complete(ctx, "2", map[string]any{"accepted": false, "feedback": "摘要与原文不符"}); err != nil {
		t.Fatal(err)
	}
	next, err := engine.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != NextFailed || next.Failure == nil || next.Failure.Reason != "摘要与原文不符" {
		t.Fatalf("next = %+v", next)
	}
	if next.Snapshot.StepStates["2"] != StepDone {
		t.Fatalf("check execution state = %s", next.Snapshot.StepStates["2"])
	}
}

func TestExecutionEngineResumeResetsRunningStep(t *testing.T) {
	for _, kind := range []string{"memory", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			var store PersistenceProvider
			var closeStore func()
			if kind == "sqlite" {
				sqliteStore, err := NewSQLitePersistence(filepath.Join(t.TempDir(), "hopjit.db"))
				if err != nil {
					t.Fatal(err)
				}
				store = sqliteStore
				closeStore = func() { _ = sqliteStore.Close() }
			} else {
				store = NewMemoryPersistence()
				closeStore = func() {}
			}
			defer closeStore()
			ctx := context.Background()
			engine := NewExecutionEngine(store)
			init, err := engine.Init(ctx, engineReviewSpec, map[string]any{"document": "原文"})
			if err != nil {
				t.Fatal(err)
			}
			mustNextStep(t, engine, "1")
			restarted := NewExecutionEngine(store)
			if err := restarted.Resume(ctx, init.RunID); err != nil {
				t.Fatal(err)
			}
			snapshot, err := restarted.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.StepStates["1"] != StepPending {
				t.Fatalf("running step after restart = %s", snapshot.StepStates["1"])
			}
			mustNextStep(t, restarted, "1")
			if err := restarted.Complete(ctx, "1", map[string]any{"summary": "摘要"}); err != nil {
				t.Fatal(err)
			}
			mustNextStep(t, restarted, "2")
		})
	}
}

func TestExecutionEngineAbortSkipsUnfinishedSteps(t *testing.T) {
	engine := NewExecutionEngine(nil)
	ctx := context.Background()
	if _, err := engine.Init(ctx, engineReviewSpec, nil); err == nil {
		t.Fatal("missing required input was accepted")
	}
	if _, err := engine.Init(ctx, engineReviewSpec, map[string]any{"document": "原文"}); err != nil {
		t.Fatal(err)
	}
	mustNextStep(t, engine, "1")
	if err := engine.Abort(ctx, "用户取消"); err != nil {
		t.Fatal(err)
	}
	next, err := engine.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != NextAborted || next.Snapshot.StepStates["2"] != StepSkipped {
		t.Fatalf("next = %+v", next)
	}
}

func TestExecutionEngineDoesNotSilentlySkipUnsupportedSteps(t *testing.T) {
	engine := NewExecutionEngine(nil)
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 未支持步骤
Goal: 响亮失败
## Steps
1. [call] 暂不支持调用
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	next, err := engine.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != NextFailed || next.Failure == nil || !strings.Contains(next.Failure.Reason, "UNSUPPORTED_STEP_TYPE") {
		t.Fatalf("next = %+v", next)
	}
}

func TestSQLitePersistenceRejectsStaleSnapshot(t *testing.T) {
	store, err := NewSQLitePersistence(filepath.Join(t.TempDir(), "hopjit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.SaveSnapshot(ctx, EngineSnapshot{RunID: "run", StateSeq: 1, FormatVersion: SnapshotFormatVersion}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSnapshot(ctx, EngineSnapshot{RunID: "run", StateSeq: 2, FormatVersion: SnapshotFormatVersion}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSnapshot(ctx, EngineSnapshot{RunID: "run", StateSeq: 1, FormatVersion: SnapshotFormatVersion}); !errors.Is(err, ErrStaleSnapshot) {
		t.Fatalf("stale save error = %v", err)
	}
	loaded, err := store.LoadSnapshot(ctx, "run")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.StateSeq != 2 {
		t.Fatalf("state seq = %d", loaded.StateSeq)
	}
}

func mustNextStep(t *testing.T, engine *ExecutionEngine, stepID string) NextResult {
	t.Helper()
	next, err := engine.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != NextStepReady || next.Step == nil || next.Step.StepID != stepID {
		t.Fatalf("Next() = %+v, want step %s", next, stepID)
	}
	return next
}
