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
	"time"
)

// Price defines the cost per million tokens for a model.
type Price struct {
	InputPerMToken  float64 // USD per 1M input tokens
	OutputPerMToken float64 // USD per 1M output tokens
}

// CostTracker accumulates LLM usage costs.
type CostTracker struct {
	mu           sync.Mutex
	sessionCost  float64
	sessionCalls int
	dailyCost    float64
	dailyCalls   int
	dailyDate    string // "2006-01-02"
	priceTable   map[string]Price
	priceCache   map[string]Price // cache for prefix-matched models
	budgetLimit  float64          // daily budget in USD (0 = unlimited)
	warnRatio    float64          // warn when daily cost reaches this ratio of budget (default 0.8)
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
func NewCostTracker(budgetLimit float64) *CostTracker {
	return &CostTracker{
		priceTable:  DefaultPriceTable,
		priceCache:  make(map[string]Price),
		budgetLimit: budgetLimit,
		warnRatio:   0.8,
		dailyDate:   time.Now().Format("2006-01-02"),
	}
}

// Record records a single LLM call's token usage and returns the cost in USD.
func (t *CostTracker) Record(model string, inputTokens, outputTokens int) float64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()

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
		t.dailyDate = today
	}

	t.sessionCost += cost
	t.sessionCalls++
	t.dailyCost += cost
	t.dailyCalls++

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

// DailyCost returns the accumulated cost for today.
func (t *CostTracker) DailyCost() float64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rolloverIfNeeded()
	return t.dailyCost
}

// IsOverBudget returns true if daily cost exceeds the configured budget.
func (t *CostTracker) IsOverBudget() bool {
	if t == nil || t.budgetLimit <= 0 {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rolloverIfNeeded()
	return t.dailyCost >= t.budgetLimit
}

// ShouldWarn returns true if daily cost exceeds the warning threshold.
func (t *CostTracker) ShouldWarn() bool {
	if t == nil || t.budgetLimit <= 0 {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rolloverIfNeeded()
	return t.dailyCost >= t.budgetLimit*t.warnRatio
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
func (t *CostTracker) DailySummary() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rolloverIfNeeded()
	if t.budgetLimit > 0 {
		return fmt.Sprintf("today=$%.4f/$%.2f (%d calls, %.0f%%)",
			t.dailyCost, t.budgetLimit, t.dailyCalls, t.dailyCost/t.budgetLimit*100)
	}
	return fmt.Sprintf("today=$%.4f (%d calls)", t.dailyCost, t.dailyCalls)
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
		t.dailyDate = today
	}
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
