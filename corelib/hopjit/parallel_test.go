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
	store := NewMemoryPersistence()
	engine := NewExecutionEngine(store, WithSpecProvider(provider))
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
	if len(result.Snapshot.Inflight) != 0 {
		t.Fatalf("inflight=%+v", result.Snapshot.Inflight)
	}
	for _, call := range result.Snapshot.Calls {
		snapshot, err := store.LoadSnapshot(context.Background(), call.RunID)
		if err != nil || snapshot.TerminalState == "" {
			t.Fatalf("child %s was not finalized: snapshot=%+v err=%v", call.RunID, snapshot, err)
		}
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

func TestParallelInflightMissingChildFailsInsteadOfStalling(t *testing.T) {
	store := NewMemoryPersistence()
	ctx := context.Background()
	parent := NewExecutionEngine(store, WithSpecProvider(MapSpecProvider{"a": childSpecForParallelTest("child-a")}))
	parentInit, err := parent.Init(ctx, `# Spec: 子快照丢失
Goal: 恢复失败必须明确终止
## Steps
1. [call a parallel] 子调用
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.LoadSnapshot(ctx, parentInit.RunID)
	if err != nil {
		t.Fatal(err)
	}
	normalizeSnapshotMaps(&snapshot)
	snapshot.Calls["1"] = CallState{RunID: "run-missing", Callee: "a"}
	snapshot.Inflight["1"] = InflightCall{StepID: "1", ChildRunID: "run-missing", Status: "inflight"}
	snapshot.StepStates["1"] = StepRunning
	snapshot.StateSeq++
	if err := store.SaveSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	restarted := NewExecutionEngine(store, WithSpecProvider(MapSpecProvider{"a": childSpecForParallelTest("child-a")}))
	if err := restarted.Resume(ctx, parentInit.RunID); err != nil {
		t.Fatal(err)
	}
	next, err := restarted.Next(ctx)
	if err != nil || next.Status != NextFailed || next.Failure == nil || len(next.Snapshot.Inflight) != 0 {
		t.Fatalf("next=%+v err=%v", next, err)
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

func TestInflightWaitHonorsCancellation(t *testing.T) {
	engine := NewExecutionEngine(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := engine.waitForInflight(ctx); err != context.Canceled {
		t.Fatalf("err=%v", err)
	}
}

func TestParallelChildPauseRestartsDriver(t *testing.T) {
	provider := MapSpecProvider{"child": `# Spec: 并行子暂停
Goal: 等待回答后继续
Outputs:
- result % text
## Steps
1. [ask] 输入答案
  + → answer % text
2. [reason] 返回答案
  - ← answer
  + → result % text
3. [exit]
`}
	store := NewMemoryPersistence()
	engine := NewExecutionEngine(store, WithSpecProvider(provider))
	ctx := context.Background()
	init, err := engine.Init(ctx, `# Spec: 并行暂停恢复
Goal: 恢复并行子调用
Outputs:
- report % text
## Steps
1. [call child parallel] 等待子问题
  + → report: result
2. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	caller := &reuseTestCaller{execute: func(step StepReady) (map[string]any, error) {
		return map[string]any{"result": step.Inputs["answer"]}, nil
	}}
	result, err := DriveReuse(ctx, engine, caller)
	if err != nil || result.Status != NextPaused || result.Pause == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	restarted := NewExecutionEngine(store, WithSpecProvider(provider))
	if err := restarted.Resume(ctx, init.RunID); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Complete(ctx, result.Pause.StepID, map[string]any{"answer": "继续"}); err != nil {
		t.Fatal(err)
	}
	resumeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	result, err = DriveReuse(resumeCtx, restarted, caller)
	if err != nil || result.Status != NextCompleted || result.Outputs["report"] != "继续" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestParallelLoopOlderCompletionKeepsCurrentIterationRunning(t *testing.T) {
	provider := MapSpecProvider{"child": `# Spec: 旧迭代竞态
Goal: 返回单项
Inputs:
- document % text
Outputs:
- result % text
## Steps
1. [reason] 处理
  - ← document
  + → result % text
`}
	engine := NewExecutionEngine(nil, WithSpecProvider(provider))
	_, err := engine.Init(context.Background(), `# Spec: 并行迭代状态
Goal: 旧迭代完成不得收束新迭代
Inputs:
- items % [text]
Outputs:
- reports % [text]
## Steps
1. [loop for-each item in items, collect report into reports]
  + → reports % [text]
  1.1. [call child(document: item) parallel] 单项处理
    + → report: result
2. [exit]
`, map[string]any{"items": []string{"A", "B"}})
	if err != nil {
		t.Fatal(err)
	}
	secondStarted := make(chan struct{}, 1)
	releaseSecond := make(chan struct{})
	caller := &reuseTestCaller{executeContext: func(ctx context.Context, step StepReady) (map[string]any, error) {
		item := fmt.Sprint(step.Inputs["document"])
		if item == "B" {
			select {
			case secondStarted <- struct{}{}:
			default:
			}
			select {
			case <-releaseSecond:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return map[string]any{"result": "报告" + item}, nil
	}}
	type driveResult struct {
		result ReuseResult
		err    error
	}
	done := make(chan driveResult, 1)
	go func() {
		result, err := DriveReuse(context.Background(), engine, caller)
		done <- driveResult{result: result, err: err}
	}()
	select {
	case <-secondStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("second iteration did not start")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		snapshot, err := engine.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := snapshot.LoopResults["1"]["1"]; ok {
			if snapshot.StepStates["1.1"] != StepRunning || !snapshotHasInflight(snapshot, "1.1#2") {
				t.Fatalf("older iteration closed current step: %+v", snapshot)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first iteration was not reaped")
		}
		time.Sleep(time.Millisecond)
	}
	close(releaseSecond)
	select {
	case outcome := <-done:
		items, ok := valueSlice(outcome.result.Outputs["reports"])
		if outcome.err != nil || outcome.result.Status != NextCompleted || !ok || len(items) != 2 || items[0] != "报告A" || items[1] != "报告B" {
			t.Fatalf("result=%+v err=%v", outcome.result, outcome.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("parallel loop did not finish")
	}
}

func snapshotHasInflight(snapshot EngineSnapshot, id string) bool {
	_, ok := snapshot.Inflight[id]
	return ok
}

func TestParallelCallLoopCollectsMappedOutput(t *testing.T) {
	provider := MapSpecProvider{"child": `# Spec: 映射输出
Goal: 返回单项结果
Inputs:
- document % text
Outputs:
- result % text
## Steps
1. [reason] 处理
  - ← document
  + → result % text
2. [exit]
`}
	engine := NewExecutionEngine(nil, WithSpecProvider(provider))
	_, err := engine.Init(context.Background(), `# Spec: 并行映射收集
Goal: 收集 call 映射后的值
Inputs:
- items % [text]
Outputs:
- reports % [text]
## Steps
1. [loop for-each item in items, collect report into reports]
  + → reports % [text]
  1.1. [call child(document: item) parallel] 单项处理
    + → report: result
2. [exit]
`, map[string]any{"items": []string{"A", "B"}})
	if err != nil {
		t.Fatal(err)
	}
	caller := &reuseTestCaller{execute: func(step StepReady) (map[string]any, error) {
		return map[string]any{"result": fmt.Sprintf("报告%v", step.Inputs["document"])}, nil
	}}
	result, err := DriveReuse(context.Background(), engine, caller)
	if err != nil {
		t.Fatal(err)
	}
	items, ok := valueSlice(result.Outputs["reports"])
	if result.Status != NextCompleted || !ok || len(items) != 2 || items[0] != "报告A" || items[1] != "报告B" {
		t.Fatalf("result=%+v", result)
	}
}

func TestMixedParallelLoopCollectsSerialOutput(t *testing.T) {
	provider := MapSpecProvider{"child": `# Spec: 混合循环子调用
Goal: 生成中间值
Inputs:
- document % text
Outputs:
- result % text
## Steps
1. [reason] 处理
  - ← document
  + → result % text
`}
	engine := NewExecutionEngine(nil, WithSpecProvider(provider))
	_, err := engine.Init(context.Background(), `# Spec: 混合并行循环
Goal: 串行输出仍需收集
Inputs:
- items % [text]
Outputs:
- reports % [text]
## Steps
1. [loop for-each item in items, collect report into reports]
  + → reports % [text]
  1.1. [call child(document: item) parallel] 并行中间值
    + → temporary: result
  1.2. [reason] 串行报告
    - ← item
    + → report % text
2. [exit]
`, map[string]any{"items": []string{"A", "B"}})
	if err != nil {
		t.Fatal(err)
	}
	caller := &reuseTestCaller{execute: func(step StepReady) (map[string]any, error) {
		if step.StepID == "1.2" {
			return map[string]any{"report": fmt.Sprintf("报告%v", step.Inputs["item"])}, nil
		}
		return map[string]any{"result": step.Inputs["document"]}, nil
	}}
	result, err := DriveReuse(context.Background(), engine, caller)
	if err != nil {
		t.Fatal(err)
	}
	items, ok := valueSlice(result.Outputs["reports"])
	if result.Status != NextCompleted || !ok || len(items) != 2 || items[0] != "报告A" || items[1] != "报告B" {
		t.Fatalf("result=%+v", result)
	}
}

func TestParallelDispatchDoesNotPassSerialStep(t *testing.T) {
	engine := NewExecutionEngine(nil, WithSpecProvider(MapSpecProvider{"child": childSpecForParallelTest("child")}))
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 并行顺序边界
Goal: 串行步骤前不得派发后续并行调用
Outputs:
- report % text
## Steps
1. [reason] 先生成
  + → report % text
2. [call child parallel] 后执行
3. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	next, err := engine.Next(ctx)
	if err != nil || next.Step == nil || next.Step.StepID != "1" {
		t.Fatalf("next=%+v err=%v", next, err)
	}
	if len(next.Snapshot.Calls) != 0 || len(next.Snapshot.Inflight) != 0 {
		t.Fatalf("parallel call crossed serial step: %+v", next.Snapshot)
	}
}

func TestParallelChildPausesAreAnsweredOneAtATime(t *testing.T) {
	child := `# Spec: 并行提问
Goal: 返回回答
Outputs:
- result % text
## Steps
1. [ask] 输入答案
  + → result % text
2. [exit]
`
	engine := NewExecutionEngine(nil, WithSpecProvider(MapSpecProvider{"a": child, "b": child}))
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 多个并行暂停
Goal: 逐个回答所有子问题
Outputs:
- left % text
- right % text
## Steps
1. [call a parallel] 左问题
  + → left: result
2. [call b parallel] 右问题
  + → right: result
3. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	result, err := DriveReuse(runCtx, engine, &reuseTestCaller{})
	outputByRun := map[string]string{}
	for stepID, call := range result.Snapshot.Calls {
		if stepID == "1" {
			outputByRun[call.RunID] = "left"
		} else if stepID == "2" {
			outputByRun[call.RunID] = "right"
		}
	}
	answered := map[string]string{}
	for answers := 0; err == nil && result.Status == NextPaused; answers++ {
		if answers == 2 || result.Pause == nil || result.Pause.ChildRunID == "" {
			t.Fatalf("unexpected pause result=%+v", result)
		}
		answer := fmt.Sprintf("回答%d", answers+1)
		answered[outputByRun[result.Pause.ChildRunID]] = answer
		if err = engine.Complete(ctx, result.Pause.StepID, map[string]any{"result": answer}); err == nil {
			result, err = DriveReuse(runCtx, engine, &reuseTestCaller{})
		}
	}
	if err != nil || result.Status != NextCompleted || len(answered) != 2 || answered["left"] == answered["right"] ||
		result.Outputs["left"] != answered["left"] || result.Outputs["right"] != answered["right"] {
		t.Fatalf("result=%+v answered=%v err=%v", result, answered, err)
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
