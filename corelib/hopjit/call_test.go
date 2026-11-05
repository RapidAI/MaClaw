package hopjit

import (
	"context"
	"errors"
	"strings"
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
	store := NewMemoryPersistence()
	engine := NewExecutionEngine(store, WithSpecProvider(provider), WithRunOwner("user-a"))
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
	child, err := store.LoadSnapshot(context.Background(), result.Snapshot.Calls["1"].RunID)
	if err != nil || child.OwnerID != "user-a" {
		t.Fatalf("child owner=%q err=%v", child.OwnerID, err)
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
	if err := restarted.Complete(context.Background(), "wrong-step", map[string]any{"result": "错误"}); err == nil {
		t.Fatal("wrong pause step id was accepted")
	}
	if err := restarted.Complete(context.Background(), result.Pause.StepID, map[string]any{"result": "回答"}); err != nil {
		t.Fatal(err)
	}
	result, err = DriveReuse(context.Background(), restarted, callTestCaller{})
	if err != nil || result.Status != NextCompleted || result.Outputs["report"] != "回答" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestAbortPausedCallFinalizesChild(t *testing.T) {
	provider := MapSpecProvider{"child": `# Spec: 子暂停
Goal: 等待回答
Outputs:
- result % text
## Steps
1. [ask] 输入答案
  + → result % text
`}
	store := NewMemoryPersistence()
	engine := NewExecutionEngine(store, WithSpecProvider(provider))
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 中止父调用
Goal: 中止时结束子运行
## Steps
1. [call child] 调用子暂停
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := DriveReuse(ctx, engine, callTestCaller{})
	if err != nil || result.Status != NextPaused || result.Pause == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	childRunID := result.Pause.ChildRunID
	if err := engine.Abort(ctx, "用户取消"); err != nil {
		t.Fatal(err)
	}
	child, err := store.LoadSnapshot(ctx, childRunID)
	if err != nil || child.TerminalState != TerminalAborted {
		t.Fatalf("child=%+v err=%v", child, err)
	}
}

func TestCallInsideLoopUsesFreshChildPerIteration(t *testing.T) {
	provider := MapSpecProvider{"child": `# Spec: 循环子调用
Goal: 处理单项
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
	_, err := engine.Init(context.Background(), `# Spec: 循环调用
Goal: 每项都由独立子运行处理
Inputs:
- items % [text]
Outputs:
- reports % [text]
## Steps
1. [loop for-each item in items, collect report into reports]
  + → reports % [text]
  1.1. [call child(document: item)] 处理单项
    + → report: result
2. [exit]
`, map[string]any{"items": []string{"A", "B"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := DriveReuse(context.Background(), engine, callTestCaller{})
	if err != nil {
		t.Fatal(err)
	}
	items, ok := valueSlice(result.Outputs["reports"])
	if result.Status != NextCompleted || !ok || len(items) != 2 || items[0] != "处理A" || items[1] != "处理B" {
		t.Fatalf("result=%+v", result)
	}
}

func TestCallFailureRetryStartsFreshChild(t *testing.T) {
	provider := MapSpecProvider{"child": childSpecForParallelTest("retry-child")}
	engine := NewExecutionEngine(nil, WithSpecProvider(provider))
	_, err := engine.Init(context.Background(), `# Spec: 调用重试
Goal: 容器重试必须重新调用
Outputs:
- result % text
## Steps
1. [subtask retry=1]
  + → result % text
  1.1. [call child] 调用
    + → result: result
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
		return map[string]any{"result": "成功"}, nil
	}}
	result, err := DriveReuse(context.Background(), engine, caller)
	if err != nil || result.Status != NextCompleted || result.Outputs["result"] != "成功" || calls != 2 {
		t.Fatalf("result=%+v calls=%d err=%v", result, calls, err)
	}
}

func TestCallFailureCanTriggerAdaptiveReplan(t *testing.T) {
	provider := MapSpecProvider{"child": childSpecForParallelTest("adaptive-child")}
	engine := NewExecutionEngine(nil, WithSpecProvider(provider))
	_, err := engine.Init(context.Background(), `# Spec: 调用自适应
Goal: 子调用失败后重规划
Outputs:
- result % text
## Steps
1. [subtask adaptive]
  + → result % text
  1.1. [call child] 调用
    + → result: result
2. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	caller := &reuseTestCaller{
		execute: func(StepReady) (map[string]any, error) {
			calls++
			if calls == 1 {
				return nil, errors.New("需要替代路径")
			}
			return map[string]any{"result": "重规划成功"}, nil
		},
		replan: func(EngineSnapshot) (string, error) {
			return "1. [reason] 替代路径\n  + → result % text", nil
		},
	}
	result, err := DriveReuse(context.Background(), engine, caller)
	if err != nil || result.Status != NextCompleted || result.Outputs["result"] != "重规划成功" || calls != 2 {
		t.Fatalf("result=%+v calls=%d err=%v", result, calls, err)
	}
}

func TestRecursiveCallFailsAtDepthLimit(t *testing.T) {
	const recursive = `# Spec: 自引用
Goal: 不得无限递归
## Steps
1. [call recursive] 调用自身
`
	engine := NewExecutionEngine(nil, WithSpecProvider(MapSpecProvider{"recursive": recursive}))
	if _, err := engine.Init(context.Background(), recursive, nil); err != nil {
		t.Fatal(err)
	}
	result, err := DriveReuse(context.Background(), engine, callTestCaller{})
	if err != nil || result.Status != NextFailed || result.Failure == nil || !strings.Contains(result.Failure.Reason, "CALL_DEPTH_LIMIT_EXCEEDED") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestResumePreservesCallDepthLimit(t *testing.T) {
	store := NewMemoryPersistence()
	ctx := context.Background()
	engine := NewExecutionEngine(store, WithSpecProvider(MapSpecProvider{"child": childSpecForParallelTest("child")}))
	init, err := engine.Init(ctx, `# Spec: 深层恢复
Goal: 恢复不得重置调用深度
## Steps
1. [call child] 继续调用
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.LoadSnapshot(ctx, init.RunID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.CallDepth = maxCallDepth
	snapshot.StateSeq++
	if err := store.SaveSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	restarted := NewExecutionEngine(store, WithSpecProvider(MapSpecProvider{"child": childSpecForParallelTest("child")}))
	if err := restarted.Resume(ctx, init.RunID); err != nil {
		t.Fatal(err)
	}
	next, err := restarted.Next(ctx)
	if err != nil || next.Status != NextFailed || next.Failure == nil || !strings.Contains(next.Failure.Reason, "CALL_DEPTH_LIMIT_EXCEEDED") {
		t.Fatalf("next=%+v err=%v", next, err)
	}
}

func TestCallUsesPinnedSpecAfterRestartWithoutProvider(t *testing.T) {
	store := NewMemoryPersistence()
	ctx := context.Background()
	child := childSpecForParallelTest("cached-child")
	engine := NewExecutionEngine(store, WithSpecProvider(MapSpecProvider{"child": child}))
	init, err := engine.Init(ctx, `# Spec: 离线恢复子规约
Goal: 使用快照中的子规约
Outputs:
- result % text
## Steps
1. [call child] 调用
  + → result: result
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.LoadSnapshot(ctx, init.RunID)
	if err != nil {
		t.Fatal(err)
	}
	normalizeSnapshotMaps(&snapshot)
	snapshot.ResolvedSpecs["child"] = child
	snapshot.StateSeq++
	if err := store.SaveSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	restarted := NewExecutionEngine(store)
	if err := restarted.Resume(ctx, init.RunID); err != nil {
		t.Fatal(err)
	}
	result, err := DriveReuse(ctx, restarted, &reuseTestCaller{execute: func(StepReady) (map[string]any, error) {
		return map[string]any{"result": "缓存恢复"}, nil
	}})
	if err != nil || result.Status != NextCompleted || result.Outputs["result"] != "缓存恢复" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestCallSpecIsPinnedAcrossLoopIterations(t *testing.T) {
	versions := []string{`# Spec: v1
Goal: v1
Outputs:
- result % text
## Steps
1. [act] 生成
  + → result % text
  > ` + "```hop_python" + `
  > result = "v1"
  > ` + "```" + `
`, `# Spec: v2
Goal: v2
Outputs:
- result % text
## Steps
1. [act] 生成
  + → result % text
  > ` + "```hop_python" + `
  > result = "v2"
  > ` + "```" + `
`}
	loads := 0
	provider := SpecProviderFunc(func(context.Context, string) (string, error) {
		version := versions[0]
		if loads > 0 {
			version = versions[1]
		}
		loads++
		return version, nil
	})
	engine := NewExecutionEngine(nil, WithSpecProvider(provider))
	_, err := engine.Init(context.Background(), `# Spec: 固定子规约版本
Goal: 同一 run 不得混用版本
Inputs:
- items % [text]
Outputs:
- reports % [text]
## Steps
1. [loop for-each item in items, collect report into reports]
  + → reports % [text]
  1.1. [call child] 调用
    + → report: result
`, map[string]any{"items": []string{"A", "B"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := DriveReuse(context.Background(), engine, &reuseTestCaller{execute: func(StepReady) (map[string]any, error) {
		t.Fatal("deterministic child called external caller")
		return nil, nil
	}})
	items, ok := valueSlice(result.Outputs["reports"])
	if err != nil || result.Status != NextCompleted || loads != 1 || !ok || len(items) != 2 || items[0] != "v1" || items[1] != "v1" {
		t.Fatalf("result=%+v loads=%d err=%v", result, loads, err)
	}
}

func TestFinalOutputTypeIsCheckedAfterCallMapping(t *testing.T) {
	provider := MapSpecProvider{"child": `# Spec: 任意输出
Goal: 返回动态值
Outputs:
- result % any
## Steps
1. [reason] 生成
  + → result % any
`}
	engine := NewExecutionEngine(nil, WithSpecProvider(provider))
	_, err := engine.Init(context.Background(), `# Spec: 映射类型校验
Goal: 最终输出必须符合声明
Outputs:
- report % text
## Steps
1. [call child] 调用
  + → report: result
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	caller := &reuseTestCaller{execute: func(StepReady) (map[string]any, error) {
		return map[string]any{"result": float64(1)}, nil
	}}
	result, err := DriveReuse(context.Background(), engine, caller)
	if err != nil || result.Status != NextFailed || result.Failure == nil || !strings.Contains(result.Failure.Reason, "OUTPUT_TYPE_MISMATCH") {
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
