package memory

import (
	"context"
	"errors"
	"testing"
)

type budgetTestLLM struct {
	calls int
}

func (l *budgetTestLLM) ChatCall([]map[string]string) (string, error) {
	l.calls++
	return "ok", nil
}

func (l *budgetTestLLM) IsConfigured() bool { return true }

func TestChatCallWithContextConsumesLLMBudget(t *testing.T) {
	llm := &budgetTestLLM{}
	budget := NewLLMCallBudget(1)
	ctx := WithLLMCallBudget(context.Background(), budget)

	if _, err := chatCallWithContext(ctx, llm, []map[string]string{{"role": "user", "content": "one"}}); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if _, err := chatCallWithContext(ctx, llm, []map[string]string{{"role": "user", "content": "two"}}); !errors.Is(err, ErrLLMCallBudgetExhausted) {
		t.Fatalf("second call err = %v, want budget exhausted", err)
	}
	if llm.calls != 1 {
		t.Fatalf("llm calls = %d, want 1", llm.calls)
	}
	used, left, exhausted := budget.Snapshot()
	if used != 1 || left != 0 || !exhausted {
		t.Fatalf("budget snapshot used=%d left=%d exhausted=%v, want 1/0/true", used, left, exhausted)
	}
}
