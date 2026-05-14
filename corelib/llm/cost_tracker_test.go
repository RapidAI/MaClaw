package llm

import (
	"strings"
	"testing"
)

func TestCostTracker_Record(t *testing.T) {
	ct := NewCostTracker(0)
	// glm-4-flash: $0.014/M input, $0.014/M output
	cost := ct.Record("glm-4-flash", 10000, 2000)
	// Expected: 10000/1M * 0.014 + 2000/1M * 0.014 = 0.00014 + 0.000028 = 0.000168
	if cost < 0.0001 || cost > 0.001 {
		t.Errorf("unexpected cost: $%.6f", cost)
	}
}

func TestCostTracker_SessionAccumulates(t *testing.T) {
	ct := NewCostTracker(0)
	ct.Record("glm-4-flash", 10000, 2000)
	ct.Record("glm-4-flash", 10000, 2000)
	if ct.SessionCost() == 0 {
		t.Error("session cost should accumulate")
	}
}

func TestCostTracker_ResetSession(t *testing.T) {
	ct := NewCostTracker(0)
	ct.Record("glm-4-flash", 10000, 2000)
	ct.ResetSession()
	if ct.SessionCost() != 0 {
		t.Error("session cost should be 0 after reset")
	}
	// Daily cost should NOT be reset
	if ct.DailyCost() == 0 {
		t.Error("daily cost should persist after session reset")
	}
}

func TestCostTracker_IsOverBudget(t *testing.T) {
	ct := NewCostTracker(0.001) // $0.001 budget
	if ct.IsOverBudget() {
		t.Error("should not be over budget initially")
	}
	// Record enough to exceed budget
	ct.Record("gpt-4o", 100000, 50000) // expensive model, lots of tokens
	if !ct.IsOverBudget() {
		t.Errorf("should be over budget, daily=$%.6f", ct.DailyCost())
	}
}

func TestCostTracker_ShouldWarn(t *testing.T) {
	ct := NewCostTracker(1.0) // $1.00 budget
	// Record $0.85 worth
	ct.Record("gpt-4o", 200000, 50000) // ~$0.50 + $0.50 = ~$1.0
	if !ct.ShouldWarn() {
		t.Errorf("should warn at 80%% of budget, daily=$%.4f", ct.DailyCost())
	}
}

func TestCostTracker_NoBudget_NeverOverOrWarn(t *testing.T) {
	ct := NewCostTracker(0) // unlimited
	ct.Record("gpt-4o", 1000000, 1000000) // huge usage
	if ct.IsOverBudget() {
		t.Error("unlimited budget should never be over")
	}
	if ct.ShouldWarn() {
		t.Error("unlimited budget should never warn")
	}
}

func TestCostTracker_UnknownModel(t *testing.T) {
	ct := NewCostTracker(0)
	cost := ct.Record("some-unknown-model-xyz", 10000, 5000)
	if cost != 0 {
		t.Errorf("unknown model should have 0 cost, got $%.6f", cost)
	}
}

func TestCostTracker_SessionSummary(t *testing.T) {
	ct := NewCostTracker(0)
	ct.Record("glm-4-flash", 10000, 2000)
	ct.Record("glm-4-flash", 5000, 1000)
	summary := ct.SessionSummary()
	if !strings.Contains(summary, "session=") || !strings.Contains(summary, "2 calls") {
		t.Errorf("unexpected summary: %s", summary)
	}
}

func TestCostTracker_DailySummary_WithBudget(t *testing.T) {
	ct := NewCostTracker(5.0)
	ct.Record("glm-4-flash", 10000, 2000)
	summary := ct.DailySummary()
	if !strings.Contains(summary, "today=") {
		t.Errorf("unexpected summary: %s", summary)
	}
}

func TestCostTracker_NilSafe(t *testing.T) {
	var ct *CostTracker
	cost := ct.Record("gpt-4o", 1000, 500)
	if cost != 0 {
		t.Error("nil tracker should return 0")
	}
	if ct.SessionCost() != 0 {
		t.Error("nil tracker session cost should be 0")
	}
	if ct.IsOverBudget() {
		t.Error("nil tracker should not be over budget")
	}
	ct.ResetSession() // should not panic
}

func TestCostTracker_AddPrice(t *testing.T) {
	ct := NewCostTracker(0)
	ct.AddPrice("my-custom-model", Price{InputPerMToken: 1.0, OutputPerMToken: 2.0})
	cost := ct.Record("my-custom-model", 1000000, 500000)
	// 1M * $1.0 + 0.5M * $2.0 = $1.0 + $1.0 = $2.0
	if cost < 1.9 || cost > 2.1 {
		t.Errorf("custom model cost should be ~$2.0, got $%.4f", cost)
	}
}
