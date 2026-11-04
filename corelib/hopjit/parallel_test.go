package hopjit

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

type parallelTestCaller struct {
	started atomic.Int32
	release chan struct{}
}

func (c *parallelTestCaller) ExecuteStep(ctx context.Context, step StepReady) (map[string]any, error) {
	if step.Goal == "child-a" || step.Goal == "child-b" {
		if c.started.Add(1) == 2 {
			close(c.release)
		}
		select {
		case <-c.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
			return nil, errors.New("parallel children did not overlap")
		}
		return map[string]any{"result": step.Goal}, nil
	}
	return map[string]any{"report": fmt.Sprintf("%v+%v", step.Inputs["left"], step.Inputs["right"])}, nil
}

func (c *parallelTestCaller) Replan(context.Context, EngineSnapshot) (string, error) { return "", nil }

func TestParallelCallsOverlapAndReapBeforeConsumer(t *testing.T) {
	provider := MapSpecProvider{
		"a": childSpecForParallelTest("child-a"),
		"b": childSpecForParallelTest("child-b"),
	}
	engine := NewExecutionEngine(nil, WithSpecProvider(provider))
	_, err := engine.Init(context.Background(), `# Spec: 并行父任务
Goal: 并行调用后汇总
Outputs:
- report % text
## Steps
1. [call a parallel] 左侧
  + → left: result
2. [call b parallel] 右侧
  + → right: result
3. [reason] 汇总
  - ← left, right
  + → report % text
4. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	caller := &parallelTestCaller{release: make(chan struct{})}
	result, err := DriveReuse(context.Background(), engine, caller)
	if err != nil || result.Status != NextCompleted || result.Outputs["report"] != "child-a+child-b" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(result.Snapshot.Inflight) != 0 || caller.started.Load() != 2 {
		t.Fatalf("inflight=%+v started=%d", result.Snapshot.Inflight, caller.started.Load())
	}
}

func TestParallelCallFailureCancelsSibling(t *testing.T) {
	provider := MapSpecProvider{
		"a": childSpecForParallelTest("child-a"),
		"b": childSpecForParallelTest("child-b"),
	}
	engine := NewExecutionEngine(nil, WithSpecProvider(provider))
	_, err := engine.Init(context.Background(), `# Spec: 并行失败
Goal: 一个失败后取消另一个
## Steps
1. [call a parallel] 失败
2. [call b parallel] 等待
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	caller := &parallelFailureCaller{}
	result, err := DriveReuse(context.Background(), engine, caller)
	if err != nil || result.Status != NextFailed {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

type parallelFailureCaller struct{}

func (parallelFailureCaller) ExecuteStep(ctx context.Context, step StepReady) (map[string]any, error) {
	if step.Goal == "child-a" {
		return nil, errors.New("child failed")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(2 * time.Second):
		return nil, errors.New("sibling was not cancelled")
	}
}

func (parallelFailureCaller) Replan(context.Context, EngineSnapshot) (string, error) { return "", nil }

type parallelRecoveryCaller struct{}

func (parallelRecoveryCaller) ExecuteStep(context.Context, StepReady) (map[string]any, error) {
	return map[string]any{"result": "recovered"}, nil
}
func (parallelRecoveryCaller) Replan(context.Context, EngineSnapshot) (string, error) { return "", nil }

func TestParallelInflightRecoversAfterRestart(t *testing.T) {
	provider := MapSpecProvider{"a": childSpecForParallelTest("child-a")}
	store := NewMemoryPersistence()
	ctx := context.Background()
	child := NewExecutionEngine(store)
	childInit, err := child.Init(ctx, provider["a"], nil)
	if err != nil {
		t.Fatal(err)
	}
	parent := NewExecutionEngine(store, WithSpecProvider(provider))
	parentInit, err := parent.Init(ctx, `# Spec: 并行恢复
Goal: 从 inflight 记录恢复
Outputs:
- report % text
## Steps
1. [call a parallel] 子调用
  + → report: result
2. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.LoadSnapshot(ctx, parentInit.RunID)
	if err != nil {
		t.Fatal(err)
	}
	normalizeSnapshotMaps(&snapshot)
	snapshot.Calls["1"] = CallState{RunID: childInit.RunID, Callee: "a"}
	snapshot.Inflight["1"] = InflightCall{StepID: "1", ChildRunID: childInit.RunID, Status: "inflight"}
	snapshot.StepStates["1"] = StepRunning
	snapshot.StateSeq++
	if err := store.SaveSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	restarted := NewExecutionEngine(store, WithSpecProvider(provider))
	if err := restarted.Resume(ctx, parentInit.RunID); err != nil {
		t.Fatal(err)
	}
	result, err := DriveReuse(ctx, restarted, parallelRecoveryCaller{})
	if err != nil || result.Status != NextCompleted || result.Outputs["report"] != "recovered" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

type parallelLoopCaller struct{}

func (parallelLoopCaller) ExecuteStep(_ context.Context, step StepReady) (map[string]any, error) {
	item := step.Inputs["item"]
	if item == "A" {
		time.Sleep(100 * time.Millisecond)
	} else {
		time.Sleep(20 * time.Millisecond)
	}
	return map[string]any{"report": fmt.Sprintf("报告%v", item)}, nil
}
func (parallelLoopCaller) Replan(context.Context, EngineSnapshot) (string, error) { return "", nil }

func TestParallelSubtaskLoopCollectsInIterationOrder(t *testing.T) {
	engine := NewExecutionEngine(nil)
	_, err := engine.Init(context.Background(), `# Spec: 并行循环
Goal: 并行处理后按序收集
Inputs:
- items % [text]
Outputs:
- reports % [text]
## Steps
1. [loop for-each item in items, collect report into reports]
  + → reports % [text]
  1.1. [subtask parallel] 单项处理
    - ← item
    + → report % text
    1.1.1. [reason] 生成报告
      - ← item
      + → report % text
2. [exit]
`, map[string]any{"items": []string{"A", "B"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := DriveReuse(context.Background(), engine, parallelLoopCaller{})
	if err != nil {
		t.Fatal(err)
	}
	items, ok := valueSlice(result.Outputs["reports"])
	if result.Status != NextCompleted || !ok || len(items) != 2 || items[0] != "报告A" || items[1] != "报告B" {
		t.Fatalf("result=%+v", result)
	}
	if len(result.Snapshot.Inflight) != 0 {
		t.Fatalf("inflight=%+v", result.Snapshot.Inflight)
	}
}

func childSpecForParallelTest(goal string) string {
	return `# Spec: 并行子任务
Goal: ` + goal + `
Outputs:
- result % text
## Steps
1. [reason] 生成结果
  + → result % text
2. [exit]
`
}
