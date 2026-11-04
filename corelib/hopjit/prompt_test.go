package hopjit

import (
	"context"
	"strings"
	"testing"
)

func TestPromptAssemblerIncludesRetryFeedbackAndPriorOutput(t *testing.T) {
	engine := NewExecutionEngine(nil)
	ctx := context.Background()
	_, err := engine.Init(ctx, `# Spec: 重试上下文
Goal: 修订时保留反馈
Constraints:
- 不得丢失原稿
Outputs:
- report % text
## Steps
1. [subtask retry=1]
  + → report % text
  1.1. [reason] 生成报告
    + → report % text
  1.2. [check] 核验报告
    - ← report
    + → accepted % bool
    + → feedback % text
2. [exit]
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	mustNextStep(t, engine, "1.1")
	if err := engine.Complete(ctx, "1.1", map[string]any{"report": "原稿"}); err != nil {
		t.Fatal(err)
	}
	mustNextStep(t, engine, "1.2")
	if err := engine.Complete(ctx, "1.2", map[string]any{"accepted": false, "feedback": "需要补充证据"}); err != nil {
		t.Fatal(err)
	}
	next := mustNextStep(t, engine, "1.1")
	if !strings.Contains(next.Step.Context.RetryFeedback, "需要补充证据") {
		t.Fatalf("context=%+v", next.Step.Context)
	}
	if next.Step.Context.PriorOutputs["report"] != "原稿" {
		t.Fatalf("prior outputs=%+v", next.Step.Context.PriorOutputs)
	}
	rendered := next.Step.Context.Render()
	for _, want := range []string{"修订时保留反馈", "不得丢失原稿", "重试反馈", "输出声明"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered context missing %q:\n%s", want, rendered)
		}
	}
}

func TestPromptAssemblerIncludesLoopIteration(t *testing.T) {
	engine := NewExecutionEngine(nil)
	_, err := engine.Init(context.Background(), `# Spec: 循环上下文
Goal: 显示轮次
## Steps
1. [loop max=2]
  1.1. [reason] 当前轮
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	next := mustNextStep(t, engine, "1.1")
	if !strings.Contains(next.Step.Context.IterationHistory, "iteration=1") {
		t.Fatalf("context=%+v", next.Step.Context)
	}
}

func TestPromptAssemblerBudgetFailsWithoutDroppingContent(t *testing.T) {
	engine := NewExecutionEngine(nil)
	_, err := engine.Init(context.Background(), `# Spec: 预算
Goal: 超长上下文必须显式失败
## Steps
1. [reason] 长上下文
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	next := mustNextStep(t, engine, "1")
	_, err = AssemblePromptContextWithBudget(next.Snapshot, *next.Step, PromptBudget{MaxEstimatedTokens: 1})
	if err == nil || !strings.Contains(err.Error(), "PROMPT_CONTEXT_BUDGET_EXCEEDED") {
		t.Fatalf("err=%v", err)
	}
}
