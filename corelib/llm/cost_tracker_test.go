package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

func newTestCostTracker(t *testing.T, budget float64) *CostTracker {
	t.Helper()
	dir := t.TempDir()
	maclawpath.SetBaseDir(dir)
	t.Cleanup(func() { maclawpath.SetBaseDir("") })
	ct := NewCostTracker(budget)
	// Persist into temp base only.
	return ct
}

func TestCostTracker_Record(t *testing.T) {
	ct := newTestCostTracker(t, 0)
	// glm-4-flash: $0.014/M input, $0.014/M output
	cost := ct.Record("glm-4-flash", 10000, 2000)
	// Expected: 10000/1M * 0.014 + 2000/1M * 0.014 = 0.00014 + 0.000028 = 0.000168
	if cost < 0.0001 || cost > 0.001 {
		t.Errorf("unexpected cost: $%.6f", cost)
	}
}

func TestCostTracker_SessionAccumulates(t *testing.T) {
	ct := newTestCostTracker(t, 0)
	ct.Record("glm-4-flash", 10000, 2000)
	ct.Record("glm-4-flash", 10000, 2000)
	if ct.SessionCost() == 0 {
		t.Error("session cost should accumulate")
	}
}

func TestCostTracker_ResetSession(t *testing.T) {
	ct := newTestCostTracker(t, 0)
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
	ct := newTestCostTracker(t, 0.001) // $0.001 budget
	if ct.IsOverBudget() {
		t.Error("should not be over budget initially")
	}
	// Record enough to exceed budget
	ct.Record("gpt-4o", 100000, 50000) // expensive model, lots of tokens
	if !ct.IsOverBudget() {
		t.Errorf("should be over budget, daily=$%.6f", ct.DailyCost())
	}
}

func TestCostTracker_IsOverBudget_FleetAware(t *testing.T) {
	dir := t.TempDir()
	maclawpath.SetBaseDir(dir)
	t.Cleanup(func() { maclawpath.SetBaseDir("") })

	today := time.Now().Format("2006-01-02")
	path := CostDailyStatsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file := CostDailyDiskFile{
		Date: today,
		Instances: map[string]CostInstanceSnapshot{
			"other-pid": {CostUSD: 5.0, Calls: 10},
		},
	}
	raw, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	// Fresh process: local $0 but fleet already $5 → trip $1 budget.
	ct := NewCostTracker(1.0)
	if ct.DailyCost() != 0 {
		t.Fatalf("local daily should be 0, got %v", ct.DailyCost())
	}
	if ct.EffectiveDailyCost() < 4.9 {
		t.Fatalf("effective=%v", ct.EffectiveDailyCost())
	}
	if !ct.IsOverBudget() {
		t.Fatal("fleet spend should trip budget after restart")
	}
	if !ct.ShouldWarn() {
		t.Fatal("should warn when fleet over warn ratio")
	}
	if msg := ct.BudgetGateMessage(); !strings.Contains(msg, "预算") {
		t.Fatalf("gate msg=%q", msg)
	}
}

func TestCostTracker_ShouldWarn(t *testing.T) {
	ct := newTestCostTracker(t, 1.0) // $1.00 budget
	// Record $0.85 worth
	ct.Record("gpt-4o", 200000, 50000) // ~$0.50 + $0.50 = ~$1.0
	if !ct.ShouldWarn() {
		t.Errorf("should warn at 80%% of budget, daily=$%.4f", ct.DailyCost())
	}
}

func TestCostTracker_NoBudget_NeverOverOrWarn(t *testing.T) {
	ct := newTestCostTracker(t, 0) // unlimited
	ct.Record("gpt-4o", 1000000, 1000000) // huge usage
	if ct.IsOverBudget() {
		t.Error("unlimited budget should never be over")
	}
	if ct.ShouldWarn() {
		t.Error("unlimited budget should never warn")
	}
}

func TestCostTracker_UnknownModel(t *testing.T) {
	ct := newTestCostTracker(t, 0)
	cost := ct.Record("some-unknown-model-xyz", 10000, 5000)
	if cost != 0 {
		t.Errorf("unknown model should have 0 cost, got $%.6f", cost)
	}
}

func TestCostTracker_SessionSummary(t *testing.T) {
	ct := newTestCostTracker(t, 0)
	ct.Record("glm-4-flash", 10000, 2000)
	ct.Record("glm-4-flash", 5000, 1000)
	summary := ct.SessionSummary()
	if !strings.Contains(summary, "session=") || !strings.Contains(summary, "2 calls") {
		t.Errorf("unexpected summary: %s", summary)
	}
}

func TestCostTracker_DailySummary_WithBudget(t *testing.T) {
	ct := newTestCostTracker(t, 5.0)
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
	ct := newTestCostTracker(t, 0)
	ct.AddPrice("my-custom-model", Price{InputPerMToken: 1.0, OutputPerMToken: 2.0})
	cost := ct.Record("my-custom-model", 1000000, 500000)
	// 1M * $1.0 + 0.5M * $2.0 = $1.0 + $1.0 = $2.0
	if cost < 1.9 || cost > 2.1 {
		t.Errorf("custom model cost should be ~$2.0, got $%.4f", cost)
	}
}

func TestCostDailyPersist_FleetSum(t *testing.T) {
	dir := t.TempDir()
	maclawpath.SetBaseDir(dir)
	t.Cleanup(func() { maclawpath.SetBaseDir("") })

	ct := NewCostTracker(0)
	ct.Record("gpt-4o-mini", 1_000_000, 0) // ~$0.15
	if err := FlushDailyCostPersist(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	path := CostDailyStatsPath()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected durable file: %v path=%s", err, path)
	}
	fleet := LoadCostDailyFleet()
	if fleet.Calls < 1 || fleet.CostUSD <= 0 {
		t.Fatalf("fleet=%+v", fleet)
	}
	if fleet.Instances != 1 {
		t.Fatalf("instances=%d", fleet.Instances)
	}
	if b, ok := fleet.ByModel["gpt-4o-mini"]; !ok || b.Calls < 1 || b.CostUSD <= 0 {
		t.Fatalf("by_model missing gpt-4o-mini: %+v", fleet.ByModel)
	}
	line := FormatCostDailyFleetLine()
	if !strings.Contains(line, "llm-cost today=") {
		t.Fatalf("line=%q", line)
	}
	// Multi-instance fleet sum (simulate two host-pid slots).
	file := CostDailyDiskFile{
		Date: fleet.Date,
		Instances: map[string]CostInstanceSnapshot{
			"a-1": {CostUSD: 0.5, Calls: 2},
			"b-2": {CostUSD: 1.5, Calls: 3},
		},
	}
	raw, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := LoadCostDailyFleet()
	if sum.CostUSD < 1.9 || sum.Calls != 5 || sum.Instances != 2 {
		t.Fatalf("sum=%+v", sum)
	}
}

func TestLoadCostDailyFleet_IncludesPendingSnap(t *testing.T) {
	dir := t.TempDir()
	maclawpath.SetBaseDir(dir)
	t.Cleanup(func() { maclawpath.SetBaseDir("") })

	ct := NewCostTracker(0)
	ct.Record("gpt-4o-mini", 1_000_000, 0)
	// No FlushDailyCostPersist — fleet read must still see debounced snap.
	fleet := LoadCostDailyFleet()
	if fleet.Calls < 1 || fleet.CostUSD <= 0 {
		t.Fatalf("pending snap not visible: %+v", fleet)
	}
	if fleet.Instances != 1 {
		t.Fatalf("instances=%d", fleet.Instances)
	}
}

func TestCostDailyPersist_ConcurrentRecords(t *testing.T) {
	dir := t.TempDir()
	maclawpath.SetBaseDir(dir)
	t.Cleanup(func() { maclawpath.SetBaseDir("") })

	ct := NewCostTracker(0)
	var wg sync.WaitGroup
	const n = 20
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			ct.Record("gpt-4o-mini", 10_000, 0)
		}()
	}
	wg.Wait()
	if err := FlushDailyCostPersist(); err != nil {
		t.Fatal(err)
	}
	// In-memory must have all n calls (mutex on CostTracker).
	if ct.DailyCost() <= 0 || ct.SessionCost() <= 0 {
		t.Fatalf("memory cost zero after concurrent records")
	}
	fleet := LoadCostDailyFleet()
	// Debounced snap holds last write totals for this process; must match n calls.
	if fleet.Calls != n {
		t.Fatalf("fleet.Calls=%d want %d (lost concurrent persist?)", fleet.Calls, n)
	}
}
