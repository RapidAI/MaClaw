package llm

// CostTracker tracks LLM API usage costs per session and per day.
// Inspired by OpenHuman's agent/cost.rs + stop_hooks.rs.
//
// Features:
// - Per-session cost accumulation
// - Per-day cost accumulation with automatic date rollover
// - Configurable daily budget limit with warning/stop thresholds
// - Built-in price table for common models (extensible via config)
// - Thread-safe for concurrent agent loops

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// costTrackerGen assigns a unique generation per CostTracker so debounced
// daily snaps from concurrent Record cannot be confused across tracker instances.
var costTrackerGen atomic.Uint64

// Price defines the cost per million tokens for a model.
type Price struct {
	InputPerMToken  float64 // USD per 1M input tokens
	OutputPerMToken float64 // USD per 1M output tokens
}

// CostTracker accumulates LLM usage costs.
type CostTracker struct {
	mu             sync.Mutex
	sessionCost    float64
	sessionCalls   int
	dailyCost      float64
	dailyCalls     int
	dailyInputTok  int64
	dailyOutputTok int64
	dailyDate      string // "2006-01-02"
	priceTable     map[string]Price
	priceCache     map[string]Price // cache for prefix-matched models
	budgetLimit    float64          // daily budget in USD (0 = unlimited)
	warnRatio      float64          // warn when daily cost reaches this ratio of budget (default 0.8)
	// persistEnabled writes this process slot to stats/llm_cost_daily.json.
	persistEnabled bool
	// byModel is this process's daily spend keyed by model id.
	byModel map[string]ModelCostBucket
	// persistGen identifies this tracker for debounced snap monotonicity.
	persistGen uint64
}

// DefaultPriceTable contains pricing for common models.
// Prices in USD per million tokens. Updated as of 2026-05.
// Users can override via config.
var DefaultPriceTable = map[string]Price{
	// 智谱 GLM (converted from RMB at ~7.2 rate)
	"GLM-5.2":       {InputPerMToken: 0.69, OutputPerMToken: 2.08},
	"glm-5.1":       {InputPerMToken: 0.69, OutputPerMToken: 2.08},
	"glm-4-plus":    {InputPerMToken: 6.94, OutputPerMToken: 6.94},
	"glm-4-flash":   {InputPerMToken: 0.014, OutputPerMToken: 0.014},
	"glm-4v-plus":   {InputPerMToken: 1.39, OutputPerMToken: 1.39},
	// DeepSeek
	"deepseek-chat":     {InputPerMToken: 0.14, OutputPerMToken: 0.28},
	"deepseek-coder":    {InputPerMToken: 0.14, OutputPerMToken: 0.28},
	"deepseek-reasoner": {InputPerMToken: 0.55, OutputPerMToken: 2.19},
	"deepseek-v4-flash": {InputPerMToken: 0.07, OutputPerMToken: 0.14},
	// Anthropic Claude
	"claude-sonnet-4-20250514":    {InputPerMToken: 3.0, OutputPerMToken: 15.0},
	"claude-3-5-sonnet-20241022":  {InputPerMToken: 3.0, OutputPerMToken: 15.0},
	"claude-3-5-haiku-20241022":   {InputPerMToken: 0.80, OutputPerMToken: 4.0},
	// OpenAI
	"gpt-4o":      {InputPerMToken: 2.50, OutputPerMToken: 10.0},
	"gpt-4o-mini": {InputPerMToken: 0.15, OutputPerMToken: 0.60},
	"o3-mini":     {InputPerMToken: 1.10, OutputPerMToken: 4.40},
}

// NewCostTracker creates a tracker with the default price table.
// budgetLimit is the daily USD budget (0 = unlimited).
// Seeds today's process counters from durable fleet file when present.
func NewCostTracker(budgetLimit float64) *CostTracker {
	t := &CostTracker{
		priceTable:     DefaultPriceTable,
		priceCache:     make(map[string]Price),
		budgetLimit:    budgetLimit,
		warnRatio:      0.8,
		dailyDate:      time.Now().Format("2006-01-02"),
		persistEnabled: true,
		byModel:        make(map[string]ModelCostBucket),
		persistGen:     costTrackerGen.Add(1),
	}
	t.seedFromDisk()
	return t
}

// seedFromDisk loads this process instance's slot if still today (usually empty
// for a new pid). Does not import other processes' costs into session memory.
func (t *CostTracker) seedFromDisk() {
	if t == nil {
		return
	}
	view := LoadCostDailyFleet()
	if view.Date != t.dailyDate {
		return
	}
	// New process starts at 0; other instances remain on disk for fleet sum.
	_ = view
}

// Record records a single LLM call's token usage and returns the cost in USD.
func (t *CostTracker) Record(model string, inputTokens, outputTokens int) float64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()

	price, ok := t.priceTable[model]
	if !ok {
		// Check cache (includes negative cache for unknown models)
		if cached, hit := t.priceCache[model]; hit {
			price = cached
		} else {
			price = t.findPriceByPrefixLocked(model)
			t.priceCache[model] = price // cache result (zero Price for unknown)
		}
	}

	cost := float64(inputTokens)/1_000_000*price.InputPerMToken +
		float64(outputTokens)/1_000_000*price.OutputPerMToken

	// Date rollover
	today := time.Now().Format("2006-01-02")
	if today != t.dailyDate {
		t.dailyCost = 0
		t.dailyCalls = 0
		t.dailyInputTok = 0
		t.dailyOutputTok = 0
		t.byModel = make(map[string]ModelCostBucket)
		t.dailyDate = today
	}

	t.sessionCost += cost
	t.sessionCalls++
	t.dailyCost += cost
	t.dailyCalls++
	t.dailyInputTok += int64(inputTokens)
	t.dailyOutputTok += int64(outputTokens)
	if model == "" {
		model = "unknown"
	}
	if t.byModel == nil {
		t.byModel = make(map[string]ModelCostBucket)
	}
	mb := t.byModel[model]
	mb.CostUSD += cost
	mb.Calls++
	mb.InputTokens += int64(inputTokens)
	mb.OutputTokens += int64(outputTokens)
	t.byModel[model] = mb

	// Snapshot under lock; durable write after unlock keeps I/O off the critical path.
	persist := t.persistEnabled
	gen := t.persistGen
	date, dCost, dCalls, dIn, dOut := t.dailyDate, t.dailyCost, t.dailyCalls, t.dailyInputTok, t.dailyOutputTok
	byModelSnap := copyModelBuckets(t.byModel)
	t.mu.Unlock()

	if persist {
		persistDailyInstance(gen, date, dCost, dCalls, dIn, dOut, byModelSnap)
	}
	return cost
}

// SessionCost returns the accumulated cost for the current session.
func (t *CostTracker) SessionCost() float64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessionCost
}

// DailyCost returns the accumulated cost for today (this process only).
func (t *CostTracker) DailyCost() float64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rolloverIfNeeded()
	return t.dailyCost
}

// EffectiveDailyCost is max(this-process today, durable fleet today).
// Used for budget gates so GUI restarts and multi-process spend still count.
func (t *CostTracker) EffectiveDailyCost() float64 {
	if t == nil {
		return LoadCostDailyFleet().CostUSD
	}
	local := t.DailyCost()
	fleet := LoadCostDailyFleet()
	if fleet.CostUSD > local {
		return fleet.CostUSD
	}
	return local
}

// BudgetLimit returns the configured daily USD budget (0 = unlimited).
func (t *CostTracker) BudgetLimit() float64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.budgetLimit
}

// IsOverBudget returns true if effective daily cost exceeds the configured budget.
func (t *CostTracker) IsOverBudget() bool {
	if t == nil {
		return false
	}
	limit := t.BudgetLimit()
	if limit <= 0 {
		return false
	}
	return t.EffectiveDailyCost() >= limit
}

// ShouldWarn returns true if effective daily cost exceeds the warning threshold.
func (t *CostTracker) ShouldWarn() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	limit := t.budgetLimit
	ratio := t.warnRatio
	t.mu.Unlock()
	if limit <= 0 {
		return false
	}
	if ratio <= 0 {
		ratio = 0.8
	}
	return t.EffectiveDailyCost() >= limit*ratio
}

// SessionSummary returns a human-readable cost summary for the current session.
func (t *CostTracker) SessionSummary() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return fmt.Sprintf("session=$%.4f (%d calls)", t.sessionCost, t.sessionCalls)
}

// DailySummary returns a human-readable cost summary for today.
// When a budget is set, uses EffectiveDailyCost (fleet-aware).
func (t *CostTracker) DailySummary() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	t.rolloverIfNeeded()
	local := t.dailyCost
	calls := t.dailyCalls
	limit := t.budgetLimit
	t.mu.Unlock()

	cost := local
	if limit > 0 {
		if eff := t.EffectiveDailyCost(); eff > cost {
			cost = eff
		}
		return fmt.Sprintf("today=$%.4f/$%.2f (%d calls, %.0f%%)",
			cost, limit, calls, cost/limit*100)
	}
	return fmt.Sprintf("today=$%.4f (%d calls)", local, calls)
}

// BudgetGateMessage is the user-facing hard-stop text when over daily budget.
func (t *CostTracker) BudgetGateMessage() string {
	if t == nil {
		return "Daily LLM budget exceeded."
	}
	return fmt.Sprintf(
		"今日 LLM 预算已用尽（%s）。请在设置中提高 daily_llm_budget_usd，或使用 maclaw-cli cost 查看跨进程累计后再试。",
		t.DailySummary(),
	)
}

// ResetSession resets session-level counters (e.g. on new conversation).
func (t *CostTracker) ResetSession() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sessionCost > 0 {
		log.Printf("[cost] session ended: $%.4f (%d calls)", t.sessionCost, t.sessionCalls)
	}
	t.sessionCost = 0
	t.sessionCalls = 0
}

// SetBudget updates the daily budget limit.
func (t *CostTracker) SetBudget(usd float64) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.budgetLimit = usd
}

// SetPriceTable replaces the price table (e.g. from user config).
func (t *CostTracker) SetPriceTable(table map[string]Price) {
	if t == nil || table == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.priceTable = table
}

// AddPrice adds or updates a single model's pricing.
func (t *CostTracker) AddPrice(model string, price Price) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.priceTable == nil {
		t.priceTable = make(map[string]Price)
	}
	t.priceTable[model] = price
}

func (t *CostTracker) rolloverIfNeeded() {
	today := time.Now().Format("2006-01-02")
	if today != t.dailyDate {
		t.dailyCost = 0
		t.dailyCalls = 0
		t.dailyInputTok = 0
		t.dailyOutputTok = 0
		t.byModel = make(map[string]ModelCostBucket)
		t.dailyDate = today
	}
}

// ByModelDaily returns a copy of this process's model cost buckets for today.
func (t *CostTracker) ByModelDaily() map[string]ModelCostBucket {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rolloverIfNeeded()
	return copyModelBuckets(t.byModel)
}

// SetPersistEnabled toggles durable daily writes (tests may disable).
func (t *CostTracker) SetPersistEnabled(on bool) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.persistEnabled = on
}

// FlushDailyPersist writes the current process slot immediately (best-effort).
func (t *CostTracker) FlushDailyPersist() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if !t.persistEnabled {
		t.mu.Unlock()
		return
	}
	t.rolloverIfNeeded()
	gen := t.persistGen
	date, dCost, dCalls, dIn, dOut := t.dailyDate, t.dailyCost, t.dailyCalls, t.dailyInputTok, t.dailyOutputTok
	byModelSnap := copyModelBuckets(t.byModel)
	t.mu.Unlock()
	// Stage latest snapshot then force disk write (export / shutdown paths).
	persistDailyInstance(gen, date, dCost, dCalls, dIn, dOut, byModelSnap)
	_ = FlushDailyCostPersist()
}

func (t *CostTracker) findPriceByPrefixLocked(model string) Price {
	// Check if any price table key is a prefix of the model name.
	// This handles versioned models like "gpt-4o-2024-05-13" matching "gpt-4o".
	bestLen := 0
	var bestPrice Price
	for key, price := range t.priceTable {
		if len(key) > bestLen && len(key) <= len(model) && model[:len(key)] == key {
			bestLen = len(key)
			bestPrice = price
		}
	}
	return bestPrice // zero Price if no match (unknown model = free)
}
