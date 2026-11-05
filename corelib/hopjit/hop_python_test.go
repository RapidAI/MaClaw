package hopjit

import (
	"context"
	"strings"
	"testing"
)

func TestHopPythonActExecutesWithoutModelRound(t *testing.T) {
	engine := NewExecutionEngine(nil)
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 确定性执行
Goal: body 直接产出结果
Inputs:
- document % text
Outputs:
- summary % text
## Steps
1. [act] 整理文本
  - ← document
  + → summary % text
  > `+"```hop_python"+`
  > summary = strip(document)
  > `+"```"+`
2. [exit]
`, map[string]any{"document": "  结果  "})
	if err != nil {
		t.Fatal(err)
	}
	next, err := engine.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != NextCompleted || next.Outputs["summary"] != "结果" {
		t.Fatalf("next = %+v", next)
	}
}

func TestHopPythonUsesInjectedToolProvider(t *testing.T) {
	calls := 0
	provider := ToolProviderFunc(func(_ context.Context, name string, args map[string]any) (any, error) {
		calls++
		if name != "read" || args["path"] != "input.md" {
			t.Fatalf("call = %s %+v", name, args)
		}
		return "工具内容", nil
	})
	engine := NewExecutionEngine(nil, WithToolProvider(provider))
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 工具桥接
Goal: 通过 provider 读取
Outputs:
- content % text
## Steps
1. [act] 读取
  + → content % text
  > `+"```hop_python"+`
  > content = read(path: "input.md")
  > `+"```"+`
2. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	next, err := engine.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != NextCompleted || next.Outputs["content"] != "工具内容" || calls != 1 {
		t.Fatalf("next = %+v calls=%d", next, calls)
	}
}

func TestHopPythonConditionalBranch(t *testing.T) {
	engine := NewExecutionEngine(nil)
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 条件执行
Goal: 根据长度选择结果
Inputs:
- document % text
Outputs:
- result % text
## Steps
1. [act] 条件处理
  - ← document
  + → result % text
  > `+"```hop_python"+`
  > if len(document) > 3:
  >     result = upper(document)
  > else:
  >     result = lower(document)
  > `+"```"+`
2. [exit]
`, map[string]any{"document": "abcd"})
	if err != nil {
		t.Fatal(err)
	}
	next, err := engine.Next(ctx)
	if err != nil || next.Outputs["result"] != "ABCD" {
		t.Fatalf("next = %+v err=%v", next, err)
	}
}

func TestHopPythonHonorsCancellationWithoutToolCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := executeHopPython(ctx, "value = 1", map[string]VariableValue{}, nil); err != context.Canceled {
		t.Fatalf("err=%v", err)
	}
}

func TestCommitPreventsAncestorRetry(t *testing.T) {
	calls := 0
	provider := ToolProviderFunc(func(_ context.Context, name string, args map[string]any) (any, error) {
		calls++
		return "已提交", nil
	})
	engine := NewExecutionEngine(nil, WithToolProvider(provider))
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 提交退火
Goal: 提交后不得重跑
Outputs:
- report % text
## Steps
1. [subtask retry=2]
  1.1. [commit] 不可逆提交
    + → report % text
    > `+"```hop_python"+`
    > report = record(value: "once")
    > `+"```"+`
  1.2. [reason] 提交后失败
2. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	mustNextStep(t, engine, "1.2")
	if err := engine.Fail(ctx, "1.2", Failure{Kind: FailureError, Reason: "后续失败"}); err != nil {
		t.Fatal(err)
	}
	next, err := engine.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != NextFailed || calls != 1 || next.Snapshot.RetryCounters["1"] != 0 || !next.Snapshot.CommittedSteps["1.1"] {
		t.Fatalf("next = %+v calls=%d", next, calls)
	}
}

func TestCommitWithoutBodyAndLoopBodyAreRejected(t *testing.T) {
	for _, body := range []string{"", "for item in items:\n    report = item"} {
		engine := NewExecutionEngine(nil)
		spec := "# Spec: 非法提交\nGoal: 拒绝不安全形态\n## Steps\n1. [commit] 提交\n"
		if body != "" {
			spec += "  > ```hop_python\n  > " + strings.ReplaceAll(body, "\n", "\n  > ") + "\n  > ```\n"
		}
		_, err := engine.Init(context.Background(), spec, nil)
		if err == nil {
			t.Fatalf("body %q was accepted", body)
		}
	}
}

func TestResumeMarksUnfinishedCommitUnknown(t *testing.T) {
	store := NewMemoryPersistence()
	ctx := context.Background()
	engine := NewExecutionEngine(store)
	init, err := engine.Init(ctx, `# Spec: 未决提交
Goal: 崩溃后不得重放
## Steps
1. [commit] 提交
  > `+"```hop_python"+`
  > value = 1
  > `+"```"+`
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.LoadSnapshot(ctx, init.RunID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.StepStates["1"] = StepRunning
	snapshot.StateSeq++
	if err := store.SaveSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	restarted := NewExecutionEngine(store)
	if err := restarted.Resume(ctx, init.RunID); err != nil {
		t.Fatal(err)
	}
	next, err := restarted.Next(ctx)
	if err != nil || next.Status != NextFailed || next.Failure == nil || !strings.Contains(next.Failure.Reason, "COMMIT_OUTCOME_UNKNOWN") {
		t.Fatalf("next = %+v err=%v", next, err)
	}
}

func TestHopPythonRejectsFractionalModulo(t *testing.T) {
	if _, err := evalExpression(context.Background(), "1 % 0.5", nil, nil); err == nil {
		t.Fatal("fractional modulo was accepted")
	}
}

func TestHopPythonBooleanOperatorsShortCircuitTools(t *testing.T) {
	calls := 0
	provider := ToolProviderFunc(func(context.Context, string, map[string]any) (any, error) {
		calls++
		return true, nil
	})
	for _, test := range []struct {
		expression string
		want       bool
	}{
		{expression: "false and record()", want: false},
		{expression: "true or record()", want: true},
		{expression: "false and (1 / 0)", want: false},
	} {
		value, err := evalExpression(context.Background(), test.expression, nil, provider)
		if err != nil || value != test.want {
			t.Fatalf("expression=%q value=%v err=%v", test.expression, value, err)
		}
	}
	if calls != 0 {
		t.Fatalf("short-circuited tools executed %d times", calls)
	}
}
