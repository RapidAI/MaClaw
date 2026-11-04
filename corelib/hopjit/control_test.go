package hopjit

import (
	"context"
	"path/filepath"
	"testing"
)

func TestExecutionEngineCompletesSubtaskAfterChildren(t *testing.T) {
	engine := NewExecutionEngine(nil)
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 子任务
Goal: 验证容器推进
Outputs:
- report % text
## Steps
1. [subtask] 生成内容
  1.1. [reason] 生成报告
    + → report % text
2. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	mustNextStep(t, engine, "1.1")
	if err := engine.Complete(ctx, "1.1", map[string]any{"report": "完成"}); err != nil {
		t.Fatal(err)
	}
	next, err := engine.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != NextCompleted || next.Snapshot.StepStates["1"] != StepDone {
		t.Fatalf("next = %+v", next)
	}
}

func TestExecutionEngineSelectsBranchCase(t *testing.T) {
	for _, test := range []struct {
		mode string
		want string
	}{
		{mode: "fast", want: "快速路径"},
		{mode: "slow", want: "默认路径"},
	} {
		t.Run(test.mode, func(t *testing.T) {
			engine := NewExecutionEngine(nil)
			ctx := context.Background()
			_, err := engine.Init(ctx, `# Spec: 分支
Goal: 选择执行路径
Inputs:
- mode % line
Outputs:
- decision % text
## Steps
1. [branch] 选择路径
  1.1. [case(mode == "fast")] 快速路径
    1.1.1. [reason] 生成快速结果
      + → decision % text
  1.2. [case(else)] 默认路径
    1.2.1. [reason] 生成默认结果
      + → decision % text
2. [exit]
`, map[string]any{"mode": test.mode})
			if err != nil {
				t.Fatal(err)
			}
			next, err := engine.Next(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if next.Step == nil {
				t.Fatalf("no case step ready: %+v", next)
			}
			if err := engine.Complete(ctx, next.Step.StepID, map[string]any{"decision": test.want}); err != nil {
				t.Fatal(err)
			}
			next, err = engine.Next(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if next.Status != NextCompleted || next.Outputs["decision"] != test.want {
				t.Fatalf("next = %+v", next)
			}
		})
	}
}

func TestExecutionEngineForEachCollectsInOrder(t *testing.T) {
	engine := NewExecutionEngine(nil)
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 循环收集
Goal: 逐项生成并收集结果
Inputs:
- items % [text]
Outputs:
- reports % [text]
## Steps
1. [loop for-each item in items, collect report into reports] 逐项处理
  + → reports % [text]
  1.1. [subtask] 单项处理
    1.1.1. [reason] 生成单项报告
      - ← item
      + → report % text
2. [exit]
`, map[string]any{"items": []string{"A", "B"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []string{"A", "B"} {
		next := mustNextStep(t, engine, "1.1.1")
		if next.Step.Inputs["item"] != item {
			t.Fatalf("item = %v, want %s", next.Step.Inputs["item"], item)
		}
		if err := engine.Complete(ctx, "1.1.1", map[string]any{"report": "报告" + item}); err != nil {
			t.Fatal(err)
		}
	}
	next, err := engine.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	items, ok := valueSlice(next.Outputs["reports"])
	if !ok || len(items) != 2 || items[0] != "报告A" || items[1] != "报告B" {
		t.Fatalf("reports = %#v", next.Outputs["reports"])
	}
	if next.Snapshot.LoopCounters["1"] != 2 || next.Snapshot.StepStates["1"] != StepDone {
		t.Fatalf("loop state = %+v", next.Snapshot)
	}
}

func TestExecutionEngineHonorsLoopMaxAndBreak(t *testing.T) {
	t.Run("max", func(t *testing.T) {
		engine := NewExecutionEngine(nil)
		ctx := context.Background()
		_, err := engine.Init(ctx, `# Spec: 有界循环
Goal: 验证最大迭代数
Outputs:
- count % int
## Steps
1. [loop max=2] 最多两轮
  + → count % int
  1.1. [reason] 本轮计数
    + → count % int
2. [exit]
`, nil)
		if err != nil {
			t.Fatal(err)
		}
		for count := 1; count <= 2; count++ {
			mustNextStep(t, engine, "1.1")
			if err := engine.Complete(ctx, "1.1", map[string]any{"count": count}); err != nil {
				t.Fatal(err)
			}
		}
		next, err := engine.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		count, ok := numericValue(next.Outputs["count"])
		if next.Status != NextCompleted || !ok || count != 2 {
			t.Fatalf("next = %+v", next)
		}
	})
	t.Run("break", func(t *testing.T) {
		engine := NewExecutionEngine(nil)
		ctx := context.Background()
		_, err := engine.Init(ctx, `# Spec: 提前退出
Goal: 验证 break
## Steps
1. [loop max=5] 循环
  1.1. [reason] 只执行一轮
  1.2. [break]
2. [reason] 不应执行
`, nil)
		if err != nil {
			t.Fatal(err)
		}
		mustNextStep(t, engine, "1.1")
		if err := engine.Complete(ctx, "1.1", nil); err != nil {
			t.Fatal(err)
		}
		next, err := engine.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if next.Status != NextStepReady || next.Step == nil || next.Step.StepID != "2" {
			t.Fatalf("next = %+v", next)
		}
		if err := engine.Complete(ctx, "2", nil); err != nil {
			t.Fatal(err)
		}
		next, err = engine.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if next.Status != NextCompleted || next.Snapshot.LoopCounters["1"] != 1 {
			t.Fatalf("next = %+v", next)
		}
	})
}

func TestExecutionEngineResumeKeepsLoopPosition(t *testing.T) {
	store, err := NewSQLitePersistence(filepath.Join(t.TempDir(), "hopjit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	engine := NewExecutionEngine(store)
	init, err := engine.Init(ctx, `# Spec: 循环恢复
Goal: 恢复当前元素
Inputs:
- items % [text]
Outputs:
- reports % [text]
## Steps
1. [loop for-each item in items, collect report into reports]
  + → reports % [text]
  1.1. [reason] 处理元素
    - ← item
    + → report % text
2. [exit]
`, map[string]any{"items": []string{"A", "B"}})
	if err != nil {
		t.Fatal(err)
	}
	mustNextStep(t, engine, "1.1")
	if err := engine.Complete(ctx, "1.1", map[string]any{"report": "报告A"}); err != nil {
		t.Fatal(err)
	}
	mustNextStep(t, engine, "1.1")
	restarted := NewExecutionEngine(store)
	if err := restarted.Resume(ctx, init.RunID); err != nil {
		t.Fatal(err)
	}
	next := mustNextStep(t, restarted, "1.1")
	if next.Step.Inputs["item"] != "B" || next.Snapshot.LoopCounters["1"] != 2 {
		t.Fatalf("resumed next = %+v", next)
	}
	collected, ok := valueSlice(next.Snapshot.Variables["reports"].Value)
	if !ok || len(collected) != 1 || collected[0] != "报告A" {
		t.Fatalf("collected = %#v", next.Snapshot.Variables["reports"].Value)
	}
}

func TestExecutionEngineContinueStartsNextIteration(t *testing.T) {
	engine := NewExecutionEngine(nil)
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 继续循环
Goal: 验证 continue
Outputs:
- count % int
## Steps
1. [loop max=2]
  + → count % int
  1.1. [reason] 产生本轮结果
    + → count % int
  1.2. [continue]
2. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	for count := 1; count <= 2; count++ {
		mustNextStep(t, engine, "1.1")
		if err := engine.Complete(ctx, "1.1", map[string]any{"count": count}); err != nil {
			t.Fatal(err)
		}
	}
	next, err := engine.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	count, ok := numericValue(next.Outputs["count"])
	if next.Status != NextCompleted || !ok || count != 2 || next.Snapshot.LoopCounters["1"] != 2 {
		t.Fatalf("next = %+v", next)
	}
}

func TestConditionEvaluation(t *testing.T) {
	variables := map[string]VariableValue{
		"mode":  {Value: "fast", Assigned: true},
		"score": {Value: 3, Assigned: true},
		"empty": {Value: nil, Assigned: true},
	}
	for _, expression := range []string{
		`mode == "fast" and score >= 2`,
		`not (mode == "slow")`,
		`empty is None`,
	} {
		got, err := evalCondition(expression, variables)
		if err != nil || !got {
			t.Fatalf("evalCondition(%q) = %v, %v", expression, got, err)
		}
	}
	if _, err := evalCondition(`read("x")`, variables); err == nil {
		t.Fatal("tool-shaped condition was accepted")
	}
}
