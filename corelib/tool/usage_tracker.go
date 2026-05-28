package tool

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

// UsageRecord records a single tool invocation outcome.
type UsageRecord struct {
	ToolName     string    `json:"tool_name"`
	QueryTokens  []string  `json:"query_tokens"`
	Success      bool      `json:"success"`
	FollowUp     string    `json:"follow_up,omitempty"` // "continue", "retry", "abandon"
	Timestamp    time.Time `json:"timestamp"`
	TaskType     string    `json:"task_type,omitempty"`
	ToolSequence []string  `json:"tool_sequence,omitempty"`
	ErrorClass   string    `json:"error_class,omitempty"`
	RetryCount   int       `json:"retry_count,omitempty"`
	RecoveryTool string    `json:"recovery_tool,omitempty"`
	FinalOutcome string    `json:"final_outcome,omitempty"`
}

// ToolExperience is the richer input form used by the experience learning
// layer. It remains optional; legacy callers can continue to use Record and
// RecordOutcome.
type ToolExperience struct {
	ToolName     string
	QueryTokens  []string
	Success      bool
	FollowUp     string
	Timestamp    time.Time
	TaskType     string
	ToolSequence []string
	ErrorClass   string
	RetryCount   int
	RecoveryTool string
	FinalOutcome string
	EventContext lifecycle.EventContext
}

// UsageTracker maintains a rolling window of tool usage history.
type UsageTracker struct {
	mu        sync.RWMutex
	saveMu    sync.Mutex
	records   []UsageRecord
	path      string
	maxItems  int
	eventSink lifecycle.EventSink
}

// NewUsageTracker creates or loads a UsageTracker from the given path.
func NewUsageTracker(path string) (*UsageTracker, error) {
	t := &UsageTracker{
		records:  make([]UsageRecord, 0, 256),
		path:     path,
		maxItems: 2000,
	}
	if err := t.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("usage_tracker: load: %w", err)
	}
	return t, nil
}

// DefaultUsageTrackerPath returns ~/.maclaw/data/tool_usage.json.
func DefaultUsageTrackerPath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".maclaw", "data", "tool_usage.json")
}

// Record logs a tool invocation result. Safe for concurrent use.
// queryTokens should be the BM25-tokenized user message (top tokens).
func (t *UsageTracker) Record(toolName string, queryTokens []string, success bool) {
	t.RecordOutcome(toolName, queryTokens, success, "")
}

// RecordOutcome records a tool invocation with richer outcome information.
// followUp indicates what the model did next: "continue" (moved on), "retry"
// (called same tool again), or "abandon" (gave up on the approach).
func (t *UsageTracker) RecordOutcome(toolName string, queryTokens []string, success bool, followUp string) {
	t.RecordExperience(ToolExperience{ToolName: toolName, QueryTokens: queryTokens, Success: success, FollowUp: followUp})
}

func (t *UsageTracker) RecordOutcomeWithContext(toolName string, queryTokens []string, success bool, followUp string, eventContext lifecycle.EventContext) {
	t.RecordExperience(ToolExperience{ToolName: toolName, QueryTokens: queryTokens, Success: success, FollowUp: followUp, EventContext: eventContext})
}

// SetExperienceEventSink connects tool outcomes to the shared experience
// lifecycle. The tracker remains usable without a sink; callers can wire one
// when they want retrieval/tool attribution in the same trace stream.
func (t *UsageTracker) SetExperienceEventSink(sink lifecycle.EventSink) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.eventSink = sink
	t.mu.Unlock()
}

// RecordExperience records a richer tool outcome for later routing and
// self-evolution distillation.
func (t *UsageTracker) RecordExperience(exp ToolExperience) {
	tokens := normalizeUsageQueryTokens(exp.QueryTokens)
	ts := exp.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	r := UsageRecord{
		ToolName:     strings.TrimSpace(exp.ToolName),
		QueryTokens:  tokens,
		Timestamp:    ts,
		Success:      exp.Success,
		FollowUp:     strings.TrimSpace(exp.FollowUp),
		TaskType:     strings.TrimSpace(exp.TaskType),
		ToolSequence: normalizeUsageStringSlice(exp.ToolSequence, 8),
		ErrorClass:   strings.TrimSpace(exp.ErrorClass),
		RetryCount:   exp.RetryCount,
		RecoveryTool: strings.TrimSpace(exp.RecoveryTool),
		FinalOutcome: strings.TrimSpace(exp.FinalOutcome),
	}
	t.mu.Lock()
	t.records = append(t.records, r)
	if len(t.records) > t.maxItems {
		excess := len(t.records) - t.maxItems
		t.records = t.records[excess:]
	}
	snapshot := make([]UsageRecord, len(t.records))
	copy(snapshot, t.records)
	sink := t.eventSink
	t.mu.Unlock()
	if sink != nil {
		sink.RecordExperienceEvent(exp.EventContext.Apply(lifecycle.Event{
			EventType:  lifecycle.EventToolCallFinished,
			ToolName:   r.ToolName,
			Query:      strings.Join(r.QueryTokens, " "),
			Reason:     r.FollowUp,
			Outcome:    usageRecordOutcome(r),
			ErrorClass: r.ErrorClass,
			CreatedAt:  r.Timestamp,
		}))
	}
	_ = t.saveSnapshot(snapshot)
}

func usageRecordOutcome(r UsageRecord) string {
	if r.FinalOutcome != "" {
		return r.FinalOutcome
	}
	if r.Success {
		return "success"
	}
	return "failure"
}

func usageRecordSucceeded(r UsageRecord) bool {
	switch strings.ToLower(strings.TrimSpace(r.FinalOutcome)) {
	case "success", "succeeded", "ok", "completed", "complete", "recovered", "resolved":
		return true
	case "failed", "failure", "abandoned", "abandon", "uncertain", "unknown":
		return false
	}
	return r.Success
}

func usageRecordFailed(r UsageRecord) bool {
	switch strings.ToLower(strings.TrimSpace(r.FinalOutcome)) {
	case "failed", "failure", "abandoned", "abandon":
		return true
	case "success", "succeeded", "ok", "completed", "complete", "recovered", "resolved", "uncertain", "unknown":
		return false
	}
	return !r.Success
}

func usageRecordDecisive(r UsageRecord) bool {
	return usageRecordSucceeded(r) || usageRecordFailed(r)
}

func normalizeUsageQueryTokens(queryTokens []string) []string {
	tokens := make([]string, 0, 5)
	for _, tok := range queryTokens {
		trimmed := strings.TrimSpace(tok)
		if trimmed == "" {
			continue
		}
		tokens = append(tokens, trimmed)
		if len(tokens) >= 5 {
			break
		}
	}
	return tokens
}

func normalizeUsageStringSlice(values []string, limit int) []string {
	if limit <= 0 {
		limit = len(values)
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// OutcomeScore returns a [0,1] quality score for a tool based on recent outcomes.
// Considers success rate, retry frequency, and abandon rate over the last 7 days.
// A tool with high success and low retry/abandon rates scores close to 1.0.
func (t *UsageTracker) OutcomeScore(toolName string) float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	score, _ := t.outcomeScoreWithCount(toolName, nil)
	return score
}

// ContextOutcomeScore returns a [0,1] quality score for a tool in the context
// of the current query. Unlike OutcomeScore which computes a global score,
// this method only considers records whose QueryTokens overlap with the given
// queryTokens (Jaccard similarity > contextOutcomeMinJaccard). This allows the
// Router to distinguish "ssh is great for server monitoring" from "ssh is
// mediocre for deployment" based on historical outcomes in similar contexts.
//
// When no context-matching records are found, falls back to the global
// OutcomeScore for backward compatibility.
//
// Inspired by Memento-Skills' behavioral utility routing: the value of a tool
// depends on the task context, not just its global success rate.
func (t *UsageTracker) ContextOutcomeScore(toolName string, queryTokens []string) float64 {
	if len(queryTokens) == 0 {
		return t.OutcomeScore(toolName)
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	querySet := make(map[string]bool, len(queryTokens))
	for _, tok := range queryTokens {
		querySet[tok] = true
	}

	// Try context-specific score first.
	contextScore, contextTotal := t.outcomeScoreWithCount(toolName, querySet)

	// Not enough context-matching records - fall back to global score.
	if contextTotal < contextOutcomeMinRecords {
		score, _ := t.outcomeScoreWithCount(toolName, nil)
		return score
	}

	return contextScore
}

// contextOutcomeMinJaccard is the minimum Jaccard similarity between the
// current query tokens and a record's tokens for the record to be considered
// "same context". 0.2 is deliberately low - even a single overlapping token
// out of 5 (Jaccard=0.2) is a meaningful signal.
const contextOutcomeMinJaccard = 0.2

// contextOutcomeMinRecords is the minimum number of context-matching records
// required before ContextOutcomeScore uses context-specific stats. Below this
// threshold, the global OutcomeScore is used to avoid noisy estimates.
const contextOutcomeMinRecords = 3

// outcomeScoreWithCount computes the outcome score and record count from
// records matching toolName, optionally filtered by querySet (Jaccard > threshold).
// When querySet is nil, all records for the tool are considered (global score).
// Returns (score, totalRecords). Caller must hold t.mu.RLock.
func (t *UsageTracker) outcomeScoreWithCount(toolName string, querySet map[string]bool) (float64, int) {
	cutoff := time.Now().AddDate(0, 0, -7)
	var total, successes, retries, abandons int

	for _, r := range t.records {
		if r.ToolName != toolName || r.Timestamp.Before(cutoff) {
			continue
		}
		if querySet != nil {
			sim := jaccardTokens(querySet, r.QueryTokens)
			if sim < contextOutcomeMinJaccard {
				continue
			}
		}
		if !usageRecordDecisive(r) {
			continue
		}
		total++
		if usageRecordSucceeded(r) {
			successes++
		}
		switch r.FollowUp {
		case "retry":
			retries++
		case "abandon":
			abandons++
		}
	}

	if total == 0 {
		return 0, 0
	}

	successRate := float64(successes) / float64(total)
	retryPenalty := float64(retries) / float64(total) * 0.3
	abandonPenalty := float64(abandons) / float64(total) * 0.5

	score := successRate - retryPenalty - abandonPenalty
	return clampFloat(score, 0, 1), total
}

// ExperienceScore returns a [0,1] score for a tool given the current query tokens.
//
// The score estimates contextual utility, not global popularity. Only records
// with query-token overlap contribute evidence, each record is weighted by
// similarity and recency, and the final utility is shrunk by evidence volume so
// one lucky call cannot dominate routing.
func (t *UsageTracker) ExperienceScore(toolName string, queryTokens []string) float64 {
	if len(queryTokens) == 0 {
		return 0
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	now := time.Now()
	var utilitySum float64
	var evidenceWeight float64
	var matched int
	var scanned int

	querySet := make(map[string]bool, len(queryTokens))
	for _, tok := range queryTokens {
		querySet[tok] = true
	}

	for i := len(t.records) - 1; i >= 0; i-- {
		r := t.records[i]
		if r.ToolName != toolName {
			continue
		}
		scanned++
		// Stop scanning after 200 matches to bound computation.
		if scanned > 200 {
			break
		}

		// Jaccard similarity between query tokens and record tokens.
		overlap := jaccardTokens(querySet, r.QueryTokens)
		if overlap == 0 {
			continue
		}
		matched++

		// Recency decay.
		hours := now.Sub(r.Timestamp).Hours()
		if hours < 0 {
			hours = 0
		}
		recency := math.Exp(-0.01 * hours)

		evidence := overlap * recency
		evidenceWeight += evidence
		utilitySum += evidence * usageOutcomeWeight(r)
	}

	if matched == 0 || evidenceWeight == 0 {
		return 0
	}

	utility := utilitySum / evidenceWeight
	if utility <= 0 {
		return 0
	}

	// Confidence rises with corroborating, similar, recent evidence. The curve is
	// intentionally conservative: one exact recent success scores about 0.28,
	// while repeated consistent successes approach 1.0.
	confidence := 1 - math.Exp(-evidenceWeight/3.0)
	return clampFloat(utility*confidence, 0, 1)
}

func usageOutcomeWeight(r UsageRecord) float64 {
	if !usageRecordDecisive(r) {
		return 0
	}
	if usageRecordSucceeded(r) {
		switch r.FollowUp {
		case "retry":
			return 0.6
		case "abandon":
			return 0.2
		default:
			return 1.0
		}
	}
	switch r.FollowUp {
	case "retry":
		return -0.45
	case "abandon":
		return -0.6
	default:
		return -0.3
	}
}

// jaccardTokens computes Jaccard similarity between a set and a token slice.
func jaccardTokens(querySet map[string]bool, recordTokens []string) float64 {
	if len(querySet) == 0 || len(recordTokens) == 0 {
		return 0
	}
	recSet := make(map[string]bool, len(recordTokens))
	for _, tok := range recordTokens {
		recSet[tok] = true
	}
	var intersection int
	for tok := range querySet {
		if recSet[tok] {
			intersection++
		}
	}
	if intersection == 0 {
		return 0
	}
	union := len(querySet) + len(recSet) - intersection
	return float64(intersection) / float64(union)
}

func (t *UsageTracker) load() error {
	if t.path == "" {
		return nil
	}
	data, err := os.ReadFile(t.path)
	if err != nil {
		return err
	}
	var records []UsageRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return fmt.Errorf("usage_tracker: parse: %w", err)
	}
	t.mu.Lock()
	t.records = records
	t.mu.Unlock()
	return nil
}

func (t *UsageTracker) save() {
	t.mu.RLock()
	snapshot := make([]UsageRecord, len(t.records))
	copy(snapshot, t.records)
	t.mu.RUnlock()
	_ = t.saveSnapshot(snapshot)
}

// Save persists the current usage records to disk. Safe for concurrent use.
func (t *UsageTracker) Save() error {
	t.mu.RLock()
	snapshot := make([]UsageRecord, len(t.records))
	copy(snapshot, t.records)
	t.mu.RUnlock()
	return t.saveSnapshot(snapshot)
}

func (t *UsageTracker) saveSnapshot(records []UsageRecord) error {
	if t.path == "" {
		return nil
	}
	data, err := json.Marshal(records)
	if err != nil {
		return err
	}
	dir := filepath.Dir(t.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	t.saveMu.Lock()
	defer t.saveMu.Unlock()

	// Atomic write: temp file + rename.
	tmp := t.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, t.path)
}

// UsagePattern describes a high-frequency successful tool usage pattern.
type UsagePattern struct {
	ToolName    string   `json:"tool_name"`
	TopTokens   []string `json:"top_tokens"`
	SuccessRate float64  `json:"success_rate"`
	Count       int      `json:"count"`
	Description string   `json:"description"`
}

// ToolRoutingHint is a conservative distilled suggestion from recent tool
// experience. Hints are evidence for routing/self-evolution; they are not direct
// authorization to run tools.
type ToolRoutingHint struct {
	ContextKey    string   `json:"context_key"`
	TaskType      string   `json:"task_type,omitempty"`
	QueryTokens   []string `json:"query_tokens,omitempty"`
	PreferTools   []string `json:"prefer_tools,omitempty"`
	AvoidTools    []string `json:"avoid_tools,omitempty"`
	RecoveryTools []string `json:"recovery_tools,omitempty"`
	Evidence      int      `json:"evidence"`
	Confidence    float64  `json:"confidence"`
	Description   string   `json:"description,omitempty"`
}

// ToolSkillNudgeCandidate is a conservative suggestion that a repeated,
// successful multi-tool sequence may deserve a reusable skill. It is only a
// review candidate; callers must still ask the user or a higher-level policy
// before creating or running a skill.
type ToolSkillNudgeCandidate struct {
	ContextKey    string   `json:"context_key"`
	TaskType      string   `json:"task_type,omitempty"`
	QueryTokens   []string `json:"query_tokens,omitempty"`
	ToolSequence  []string `json:"tool_sequence"`
	Evidence      int      `json:"evidence"`
	SuccessRate   float64  `json:"success_rate"`
	Confidence    float64  `json:"confidence"`
	SuggestedName string   `json:"suggested_name,omitempty"`
	Description   string   `json:"description,omitempty"`
}

// ToolRecoveryPattern is a conservative distilled pattern for recovering from
// repeated tool failures. It is explanatory project knowledge, not permission to
// bypass normal routing or safety checks.
type ToolRecoveryPattern struct {
	ContextKey   string   `json:"context_key"`
	TaskType     string   `json:"task_type,omitempty"`
	QueryTokens  []string `json:"query_tokens,omitempty"`
	FailedTool   string   `json:"failed_tool"`
	ErrorClass   string   `json:"error_class,omitempty"`
	RecoveryTool string   `json:"recovery_tool"`
	ToolSequence []string `json:"tool_sequence,omitempty"`
	Evidence     int      `json:"evidence"`
	SuccessRate  float64  `json:"success_rate"`
	Confidence   float64  `json:"confidence"`
	Description  string   `json:"description,omitempty"`
}

// DistillRoutingHints aggregates recent rich usage records into conservative
// routing hints. The method deliberately requires repeated evidence so one lucky
// success or isolated failure cannot steer future routing.
func (t *UsageTracker) DistillRoutingHints(windowDays int, minRecords int) []ToolRoutingHint {
	if windowDays <= 0 {
		windowDays = 7
	}
	if minRecords <= 0 {
		minRecords = 3
	}
	cutoff := time.Now().AddDate(0, 0, -windowDays)
	type perTool struct {
		total     int
		successes int
		failures  int
	}
	type contextAccum struct {
		taskType string
		tokens   map[string]int
		tools    map[string]*perTool
		recovery map[string]int
		evidence int
	}
	contexts := map[string]*contextAccum{}

	t.mu.RLock()
	for _, record := range t.records {
		if record.Timestamp.Before(cutoff) || strings.TrimSpace(record.ToolName) == "" {
			continue
		}
		if !usageRecordDecisive(record) {
			continue
		}
		key, taskType := usageRoutingContextKey(record)
		if key == "" {
			continue
		}
		ctx, ok := contexts[key]
		if !ok {
			ctx = &contextAccum{taskType: taskType, tokens: map[string]int{}, tools: map[string]*perTool{}, recovery: map[string]int{}}
			contexts[key] = ctx
		}
		ctx.evidence++
		for _, tok := range record.QueryTokens {
			if tok = strings.TrimSpace(tok); tok != "" {
				ctx.tokens[tok]++
			}
		}
		stats, ok := ctx.tools[record.ToolName]
		if !ok {
			stats = &perTool{}
			ctx.tools[record.ToolName] = stats
		}
		stats.total++
		if usageRecordSucceeded(record) {
			stats.successes++
		} else {
			stats.failures++
		}
		if record.RecoveryTool != "" {
			ctx.recovery[record.RecoveryTool]++
		}
	}
	t.mu.RUnlock()

	hints := make([]ToolRoutingHint, 0, len(contexts))
	for key, ctx := range contexts {
		if ctx.evidence < minRecords {
			continue
		}
		var prefer []string
		var avoid []string
		for toolName, stats := range ctx.tools {
			if stats.total < minRecords {
				continue
			}
			successRate := float64(stats.successes) / float64(stats.total)
			failureRate := float64(stats.failures) / float64(stats.total)
			if successRate >= 0.75 {
				prefer = append(prefer, toolName)
			}
			if failureRate >= 0.60 {
				avoid = append(avoid, toolName)
			}
		}
		sort.Strings(prefer)
		sort.Strings(avoid)
		recovery := topUsageStrings(ctx.recovery, 3)
		if len(prefer) == 0 && len(avoid) == 0 && len(recovery) == 0 {
			continue
		}
		tokens := topUsageStrings(ctx.tokens, 5)
		confidence := clampFloat(1-math.Exp(-float64(ctx.evidence)/10.0), 0, 1)
		descParts := []string{}
		if len(prefer) > 0 {
			descParts = append(descParts, "prefer "+strings.Join(prefer, ", "))
		}
		if len(avoid) > 0 {
			descParts = append(descParts, "avoid "+strings.Join(avoid, ", "))
		}
		if len(recovery) > 0 {
			descParts = append(descParts, "recover with "+strings.Join(recovery, ", "))
		}
		hints = append(hints, ToolRoutingHint{
			ContextKey:    key,
			TaskType:      ctx.taskType,
			QueryTokens:   tokens,
			PreferTools:   prefer,
			AvoidTools:    avoid,
			RecoveryTools: recovery,
			Evidence:      ctx.evidence,
			Confidence:    confidence,
			Description:   strings.Join(descParts, "; "),
		})
	}
	sort.Slice(hints, func(i, j int) bool {
		if hints[i].Evidence != hints[j].Evidence {
			return hints[i].Evidence > hints[j].Evidence
		}
		return hints[i].ContextKey < hints[j].ContextKey
	})
	return hints
}

// DistillSkillNudgeCandidates aggregates repeated successful tool sequences
// into conservative skill review candidates. It requires multiple records with
// the same context and sequence, ignores single-tool calls, and only emits
// candidates with high success rate so noisy traces do not create skill churn.
func (t *UsageTracker) DistillSkillNudgeCandidates(windowDays int, minRecords int) []ToolSkillNudgeCandidate {
	if windowDays <= 0 {
		windowDays = 14
	}
	if minRecords <= 0 {
		minRecords = 3
	}
	cutoff := time.Now().AddDate(0, 0, -windowDays)
	type sequenceAccum struct {
		contextKey string
		taskType   string
		sequence   []string
		tokens     map[string]int
		total      int
		successes  int
	}
	sequences := map[string]*sequenceAccum{}

	t.mu.RLock()
	for _, record := range t.records {
		if record.Timestamp.Before(cutoff) || len(record.ToolSequence) < 2 {
			continue
		}
		key, taskType := usageRoutingContextKey(record)
		if key == "" {
			continue
		}
		sequence := normalizeUsageStringSlice(record.ToolSequence, 8)
		if len(sequence) < 2 {
			continue
		}
		seqKey := key + "|seq:" + strings.Join(sequence, ">")
		acc, ok := sequences[seqKey]
		if !ok {
			acc = &sequenceAccum{contextKey: key, taskType: taskType, sequence: sequence, tokens: map[string]int{}}
			sequences[seqKey] = acc
		}
		if !usageRecordDecisive(record) {
			continue
		}
		acc.total++
		if usageRecordSucceeded(record) {
			acc.successes++
		}
		for _, tok := range record.QueryTokens {
			if tok = strings.TrimSpace(tok); tok != "" {
				acc.tokens[tok]++
			}
		}
	}
	t.mu.RUnlock()

	candidates := make([]ToolSkillNudgeCandidate, 0, len(sequences))
	for _, acc := range sequences {
		if acc.total < minRecords {
			continue
		}
		successRate := float64(acc.successes) / float64(acc.total)
		if successRate < 0.80 {
			continue
		}
		tokens := topUsageStrings(acc.tokens, 5)
		confidence := clampFloat(successRate*(1-math.Exp(-float64(acc.total)/8.0)), 0, 1)
		description := fmt.Sprintf("Repeated successful sequence %s for [%s] (%d/%d successes in last %d days)", strings.Join(acc.sequence, " -> "), strings.Join(tokens, ", "), acc.successes, acc.total, windowDays)
		candidates = append(candidates, ToolSkillNudgeCandidate{
			ContextKey:    acc.contextKey,
			TaskType:      acc.taskType,
			QueryTokens:   tokens,
			ToolSequence:  append([]string(nil), acc.sequence...),
			Evidence:      acc.total,
			SuccessRate:   successRate,
			Confidence:    confidence,
			SuggestedName: suggestedSkillNudgeName(acc.taskType, tokens, acc.sequence),
			Description:   description,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Confidence != candidates[j].Confidence {
			return candidates[i].Confidence > candidates[j].Confidence
		}
		if candidates[i].Evidence != candidates[j].Evidence {
			return candidates[i].Evidence > candidates[j].Evidence
		}
		return strings.Join(candidates[i].ToolSequence, ">") < strings.Join(candidates[j].ToolSequence, ">")
	})
	return candidates
}

// DistillRecoveryPatterns aggregates repeated successful recovery flows. A
// pattern is emitted only when the same failed-tool -> recovery-tool pair has
// enough evidence and usually ends in a recovered/completed outcome.
func (t *UsageTracker) DistillRecoveryPatterns(windowDays int, minRecords int) []ToolRecoveryPattern {
	if windowDays <= 0 {
		windowDays = 14
	}
	if minRecords <= 0 {
		minRecords = 3
	}
	cutoff := time.Now().AddDate(0, 0, -windowDays)
	type recoveryAccum struct {
		contextKey   string
		taskType     string
		failedTool   string
		errorClass   string
		recoveryTool string
		sequence     []string
		tokens       map[string]int
		total        int
		successes    int
	}
	patterns := map[string]*recoveryAccum{}

	t.mu.RLock()
	for _, record := range t.records {
		if record.Timestamp.Before(cutoff) {
			continue
		}
		failedTool := strings.TrimSpace(record.ToolName)
		recoveryTool := strings.TrimSpace(record.RecoveryTool)
		if failedTool == "" || recoveryTool == "" {
			continue
		}
		key, taskType := usageRoutingContextKey(record)
		if key == "" {
			continue
		}
		errorClass := strings.ToLower(strings.TrimSpace(record.ErrorClass))
		patternKey := key + "|failed:" + failedTool + "|recover:" + recoveryTool + "|error:" + errorClass
		acc, ok := patterns[patternKey]
		if !ok {
			acc = &recoveryAccum{contextKey: key, taskType: taskType, failedTool: failedTool, errorClass: errorClass, recoveryTool: recoveryTool, tokens: map[string]int{}}
			patterns[patternKey] = acc
		}
		acc.total++
		if recoveryOutcomeSucceeded(record) {
			acc.successes++
		}
		for _, tok := range record.QueryTokens {
			if tok = strings.TrimSpace(tok); tok != "" {
				acc.tokens[tok]++
			}
		}
		sequence := normalizeUsageStringSlice(record.ToolSequence, 8)
		if len(sequence) > len(acc.sequence) {
			acc.sequence = sequence
		}
	}
	t.mu.RUnlock()

	out := make([]ToolRecoveryPattern, 0, len(patterns))
	for _, acc := range patterns {
		if acc.total < minRecords {
			continue
		}
		successRate := float64(acc.successes) / float64(acc.total)
		if successRate < 0.75 {
			continue
		}
		sequence := append([]string(nil), acc.sequence...)
		if len(sequence) < 2 {
			sequence = []string{acc.failedTool, acc.recoveryTool}
		}
		tokens := topUsageStrings(acc.tokens, 5)
		confidence := clampFloat(successRate*(1-math.Exp(-float64(acc.total)/8.0)), 0, 1)
		desc := fmt.Sprintf("When %s fails", acc.failedTool)
		if acc.errorClass != "" {
			desc += " with " + acc.errorClass
		}
		desc += fmt.Sprintf(", recover with %s (%d/%d recovered in last %d days)", acc.recoveryTool, acc.successes, acc.total, windowDays)
		if len(tokens) > 0 {
			desc += " for [" + strings.Join(tokens, ", ") + "]"
		}
		out = append(out, ToolRecoveryPattern{
			ContextKey:   acc.contextKey,
			TaskType:     acc.taskType,
			QueryTokens:  tokens,
			FailedTool:   acc.failedTool,
			ErrorClass:   acc.errorClass,
			RecoveryTool: acc.recoveryTool,
			ToolSequence: sequence,
			Evidence:     acc.total,
			SuccessRate:  successRate,
			Confidence:   confidence,
			Description:  desc,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		if out[i].Evidence != out[j].Evidence {
			return out[i].Evidence > out[j].Evidence
		}
		if out[i].ContextKey != out[j].ContextKey {
			return out[i].ContextKey < out[j].ContextKey
		}
		return out[i].FailedTool+out[i].RecoveryTool < out[j].FailedTool+out[j].RecoveryTool
	})
	return out
}

func recoveryOutcomeSucceeded(record UsageRecord) bool {
	switch strings.ToLower(strings.TrimSpace(record.FinalOutcome)) {
	case "recovered", "completed", "complete", "success", "succeeded", "ok", "resolved":
		return true
	case "failed", "failure", "abandoned", "abandon", "uncertain", "unknown":
		return false
	default:
		return record.Success
	}
}
func usageRoutingContextKey(record UsageRecord) (key string, taskType string) {
	taskType = strings.ToLower(strings.TrimSpace(record.TaskType))
	if taskType != "" {
		return "task:" + taskType, taskType
	}
	tokens := normalizeUsageQueryTokens(record.QueryTokens)
	if len(tokens) == 0 {
		return "", ""
	}
	if len(tokens) > 3 {
		tokens = tokens[:3]
	}
	return "tokens:" + strings.Join(tokens, ","), ""
}

func suggestedSkillNudgeName(taskType string, tokens []string, sequence []string) string {
	parts := make([]string, 0, 4)
	if taskType = strings.TrimSpace(taskType); taskType != "" {
		parts = append(parts, taskType)
	} else {
		for _, tok := range tokens {
			if tok = strings.TrimSpace(tok); tok != "" {
				parts = append(parts, tok)
			}
			if len(parts) >= 2 {
				break
			}
		}
	}
	if len(parts) == 0 && len(sequence) > 0 {
		parts = append(parts, sequence[0])
	}
	parts = append(parts, "tool", "flow")
	name := strings.ToLower(strings.Join(parts, "-"))
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		keep := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if keep {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "tool-flow"
	}
	if len(out) > 48 {
		out = strings.Trim(out[:48], "-")
	}
	return out
}

const maxRoutingHintAdjustment = 0.08

type RoutingHintAdjustmentExplanation struct {
	ToolName         string   `json:"tool_name"`
	QueryTokens      []string `json:"query_tokens,omitempty"`
	Adjustment       float64  `json:"adjustment"`
	Direction        string   `json:"direction"`
	MatchingRecords  int      `json:"matching_records"`
	Successes        int      `json:"successes,omitempty"`
	Failures         int      `json:"failures,omitempty"`
	RecoveryEvidence int      `json:"recovery_evidence,omitempty"`
	SuccessRate      float64  `json:"success_rate,omitempty"`
	FailureRate      float64  `json:"failure_rate,omitempty"`
	Reasons          []string `json:"reasons,omitempty"`
}

// RoutingHintAdjustment returns a small bounded score adjustment for the
// current tool in a similar query context. It is intentionally smaller than the
// main retrieval and experience signals so distilled hints can nudge routing
// without dominating it.
func (t *UsageTracker) RoutingHintAdjustment(toolName string, queryTokens []string) float64 {
	return t.ExplainRoutingHintAdjustment(toolName, queryTokens).Adjustment
}

func (t *UsageTracker) ExplainRoutingHintAdjustment(toolName string, queryTokens []string) RoutingHintAdjustmentExplanation {
	toolName = strings.TrimSpace(toolName)
	queryTokens = normalizeUsageQueryTokens(queryTokens)
	explanation := RoutingHintAdjustmentExplanation{
		ToolName:    toolName,
		QueryTokens: append([]string(nil), queryTokens...),
		Direction:   "neutral",
	}
	if toolName == "" || len(queryTokens) == 0 {
		if toolName == "" {
			explanation.Reasons = append(explanation.Reasons, "missing_tool")
		}
		if len(queryTokens) == 0 {
			explanation.Reasons = append(explanation.Reasons, "missing_query_tokens")
		}
		return explanation
	}
	querySet := make(map[string]bool, len(queryTokens))
	for _, tok := range queryTokens {
		if tok = strings.TrimSpace(tok); tok != "" {
			querySet[tok] = true
		}
	}
	if len(querySet) == 0 {
		explanation.Reasons = append(explanation.Reasons, "missing_query_tokens")
		return explanation
	}

	cutoff := time.Now().AddDate(0, 0, -14)
	var total, successes, failures int
	var recoveryEvidence int

	t.mu.RLock()
	for _, record := range t.records {
		if record.Timestamp.Before(cutoff) {
			continue
		}
		sim := jaccardTokens(querySet, record.QueryTokens)
		if sim < contextOutcomeMinJaccard {
			continue
		}
		if record.ToolName == toolName {
			if !usageRecordDecisive(record) {
				continue
			}
			total++
			if usageRecordSucceeded(record) {
				successes++
			} else {
				failures++
			}
		}
		if record.RecoveryTool == toolName {
			recoveryEvidence++
		}
	}
	t.mu.RUnlock()

	var adjustment float64
	explanation.MatchingRecords = total
	explanation.Successes = successes
	explanation.Failures = failures
	explanation.RecoveryEvidence = recoveryEvidence
	if total >= contextOutcomeMinRecords {
		successRate := float64(successes) / float64(total)
		failureRate := float64(failures) / float64(total)
		explanation.SuccessRate = successRate
		explanation.FailureRate = failureRate
		confidence := 1 - math.Exp(-float64(total)/6.0)
		if successRate >= 0.75 {
			adjustment += maxRoutingHintAdjustment * confidence
			explanation.Reasons = append(explanation.Reasons, "context_success_rate")
		}
		if failureRate >= 0.60 {
			adjustment -= maxRoutingHintAdjustment * confidence
			explanation.Reasons = append(explanation.Reasons, "context_failure_rate")
		}
	}
	if recoveryEvidence >= contextOutcomeMinRecords {
		confidence := 1 - math.Exp(-float64(recoveryEvidence)/6.0)
		adjustment += (maxRoutingHintAdjustment * 0.6) * confidence
		explanation.Reasons = append(explanation.Reasons, "recovery_tool_evidence")
	}
	explanation.Adjustment = clampFloat(adjustment, -maxRoutingHintAdjustment, maxRoutingHintAdjustment)
	if explanation.Adjustment > 0 {
		explanation.Direction = "prefer"
	} else if explanation.Adjustment < 0 {
		explanation.Direction = "avoid"
	}
	if len(explanation.Reasons) == 0 {
		explanation.Reasons = append(explanation.Reasons, "insufficient_matching_evidence")
	}
	return explanation
}
func topUsageStrings(counts map[string]int, limit int) []string {
	if len(counts) == 0 || limit == 0 {
		return nil
	}
	type item struct {
		value string
		count int
	}
	items := make([]item, 0, len(counts))
	for value, count := range counts {
		value = strings.TrimSpace(value)
		if value == "" || count <= 0 {
			continue
		}
		items = append(items, item{value: value, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count != items[j].count {
			return items[i].count > items[j].count
		}
		return items[i].value < items[j].value
	})
	if limit < 0 || limit > len(items) {
		limit = len(items)
	}
	out := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, items[i].value)
	}
	return out
}

// contextToolStats scans records whose QueryTokens overlap with queryTokens
// (Jaccard > contextOutcomeMinJaccard) and returns per-tool success/failure
// counts. excludeTool is excluded from results (pass "" to include all).
// Caller must NOT hold t.mu - this method acquires the lock internally.
func (t *UsageTracker) contextToolStats(queryTokens []string, excludeTool string) map[string]*toolCtxAccum {
	t.mu.RLock()
	defer t.mu.RUnlock()

	querySet := make(map[string]bool, len(queryTokens))
	for _, tok := range queryTokens {
		querySet[tok] = true
	}

	m := make(map[string]*toolCtxAccum)
	for _, r := range t.records {
		if r.ToolName == excludeTool {
			continue
		}
		sim := jaccardTokens(querySet, r.QueryTokens)
		if sim < contextOutcomeMinJaccard {
			continue
		}
		if !usageRecordDecisive(r) {
			continue
		}
		s, ok := m[r.ToolName]
		if !ok {
			s = &toolCtxAccum{}
			m[r.ToolName] = s
		}
		s.total++
		if usageRecordSucceeded(r) {
			s.successes++
		} else {
			s.failures++
		}
	}
	return m
}

// toolCtxAccum accumulates per-tool stats during a context scan.
type toolCtxAccum struct {
	total     int
	successes int
	failures  int
}

// ContextFailureStats returns per-tool failure statistics for records whose
// QueryTokens overlap with the given queryTokens (Jaccard > contextOutcomeMinJaccard).
// Only tools with at least minRecords matching records are included.
func (t *UsageTracker) ContextFailureStats(queryTokens []string, minRecords int) []ToolContextStats {
	if len(queryTokens) == 0 {
		return nil
	}
	m := t.contextToolStats(queryTokens, "")
	var result []ToolContextStats
	for name, s := range m {
		if s.total < minRecords {
			continue
		}
		result = append(result, ToolContextStats{
			ToolName:    name,
			Total:       s.total,
			Failures:    s.failures,
			FailureRate: float64(s.failures) / float64(s.total),
		})
	}
	return result
}

// ContextSuccessAlternatives returns tools that have succeeded in contexts
// similar to queryTokens, excluding excludeTool. Only tools with at least
// minRecords matching records and successRate >= minSuccessRate are included.
func (t *UsageTracker) ContextSuccessAlternatives(queryTokens []string, excludeTool string, minRecords int, minSuccessRate float64) []ToolContextStats {
	if len(queryTokens) == 0 {
		return nil
	}
	m := t.contextToolStats(queryTokens, excludeTool)
	var result []ToolContextStats
	for name, s := range m {
		if s.total < minRecords {
			continue
		}
		rate := float64(s.successes) / float64(s.total)
		if rate < minSuccessRate {
			continue
		}
		result = append(result, ToolContextStats{
			ToolName:    name,
			Total:       s.total,
			Successes:   s.successes,
			SuccessRate: rate,
		})
	}
	return result
}

// ToolContextStats holds per-tool statistics in a specific query context.
type ToolContextStats struct {
	ToolName    string  `json:"tool_name"`
	Total       int     `json:"total"`
	Successes   int     `json:"successes,omitempty"`
	Failures    int     `json:"failures,omitempty"`
	SuccessRate float64 `json:"success_rate,omitempty"`
	FailureRate float64 `json:"failure_rate,omitempty"`
}

// ExtractPatterns scans records from the last windowDays and returns
// patterns for tools with success rate > 80% and count > 5.
func (t *UsageTracker) ExtractPatterns(windowDays int) []UsagePattern {
	t.mu.RLock()
	defer t.mu.RUnlock()

	cutoff := time.Now().AddDate(0, 0, -windowDays)

	// Group by tool name.
	type toolStats struct {
		total   int
		success int
		tokens  map[string]int // token frequency
	}
	stats := make(map[string]*toolStats)

	for _, r := range t.records {
		if r.Timestamp.Before(cutoff) {
			continue
		}
		if !usageRecordDecisive(r) {
			continue
		}
		s, ok := stats[r.ToolName]
		if !ok {
			s = &toolStats{tokens: make(map[string]int)}
			stats[r.ToolName] = s
		}
		s.total++
		if usageRecordSucceeded(r) {
			s.success++
		}
		for _, tok := range r.QueryTokens {
			s.tokens[tok]++
		}
	}

	var patterns []UsagePattern
	for toolName, s := range stats {
		if s.total < 5 {
			continue
		}
		rate := float64(s.success) / float64(s.total)
		if rate < 0.8 {
			continue
		}

		// Extract top-5 tokens by frequency.
		type tokenFreq struct {
			token string
			freq  int
		}
		var tfs []tokenFreq
		for tok, freq := range s.tokens {
			tfs = append(tfs, tokenFreq{tok, freq})
		}
		sort.Slice(tfs, func(i, j int) bool { return tfs[i].freq > tfs[j].freq })
		topN := 5
		if len(tfs) < topN {
			topN = len(tfs)
		}
		topTokens := make([]string, topN)
		for i := 0; i < topN; i++ {
			topTokens[i] = tfs[i].token
		}

		desc := fmt.Sprintf("Tool %s is stable for [%s] tasks (success rate %.0f%%, last %d days %d calls)",
			toolName, strings.Join(topTokens, ", "), rate*100, windowDays, s.total)

		patterns = append(patterns, UsagePattern{
			ToolName:    toolName,
			TopTokens:   topTokens,
			SuccessRate: rate,
			Count:       s.total,
			Description: desc,
		})
	}

	return patterns
}
