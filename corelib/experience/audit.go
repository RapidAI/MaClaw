package experience

import (
	"sort"
	"sync"
	"time"
)

const (
	defaultAuditMaxDecisions            = 20
	defaultAuditMaxReasonsPerDecision   = 4
	defaultAuditMaxUnsupportedPerReport = 6
	defaultAuditMaxSummaryItems         = 12
	defaultAuditMaxStringLength         = 160
	defaultAuditTrailLimit              = 50
)

const (
	AuditStatusCompleted    = "completed"
	AuditStatusNoCandidates = "no_candidates"
	AuditStatusFailed       = "failed"
)

const (
	AuditHealthStatusEmpty          = "empty"
	AuditHealthStatusHealthy        = "healthy"
	AuditHealthStatusNeedsAttention = "needs_attention"
	AuditHealthStatusNoSignal       = "no_signal"
	AuditHealthStatusFailing        = "failing"
)

const (
	AuditIssueNone                  = "none"
	AuditIssueNoRuns                = "no_runs"
	AuditIssueExtractionFailed      = "extraction_failed"
	AuditIssueNoReusableCandidates  = "no_reusable_candidates"
	AuditIssueNoSkillsLearned       = "no_skills_learned"
	AuditIssueUnsupportedEvidence   = "unsupported_evidence"
	AuditIssueHighSkipRate          = "high_skip_rate"
	AuditIssueSomeRunsFailed        = "some_runs_failed"
	AuditIssueQualityBelowThreshold = "quality_below_threshold"
	AuditIssueInsufficientEvidence  = "insufficient_evidence"
	AuditIssueExistingSkillBetter   = "existing_skill_better"
	AuditIssuePatternBudgetExceeded = "pattern_budget_exceeded"
	AuditIssueStoreWriteFailed      = "store_write_failed"
	AuditIssueUnsupportedStepAction = "unsupported_step_action"
)

// AuditOptions bounds the detailed extraction data exposed to UI/logging
// surfaces. The full Result remains available to the caller; audit views are
// deliberately compact and redacted.
type AuditOptions struct {
	MaxDecisions            int
	MaxReasonsPerDecision   int
	MaxUnsupportedPerReport int
	MaxSummaryItems         int
	MaxStringLength         int
}

// AuditEntry is a safe diagnostic record for one experience extraction run.
type AuditEntry struct {
	Timestamp   string        `json:"timestamp"`
	SessionID   string        `json:"session_id,omitempty"`
	Tool        string        `json:"tool,omitempty"`
	Title       string        `json:"title,omitempty"`
	ProjectPath string        `json:"project_path,omitempty"`
	Status      string        `json:"status,omitempty"`
	DurationMS  int64         `json:"duration_ms,omitempty"`
	Summary     ResultSummary `json:"summary"`
	Decisions   []Decision    `json:"decisions,omitempty"`
	Upserted    []string      `json:"upserted,omitempty"`
	Error       string        `json:"error,omitempty"`
}

type AuditContext struct {
	Timestamp  string
	SessionID  string
	Snapshot   SessionSnapshot
	DurationMS int64
}

func NewResultAuditEntry(ctx AuditContext, result Result, opts AuditOptions) AuditEntry {
	summary := result.AuditSummary(opts)
	entry := auditEntryBase(ctx)
	entry.Status = AuditStatus(summary)
	entry.Summary = summary
	entry.Decisions = result.AuditDecisions(opts)
	entry.Upserted = auditUpsertedNames(result, opts)
	return entry
}

func NewErrorAuditEntry(ctx AuditContext, err error, opts AuditOptions) AuditEntry {
	entry := auditEntryBase(ctx)
	entry.Status = AuditStatusFailed
	if err != nil {
		entry.Error = AuditText(err.Error(), normalizeAuditOptions(opts).MaxStringLength)
	}
	return entry
}

func auditEntryBase(ctx AuditContext) AuditEntry {
	timestamp := ctx.Timestamp
	if timestamp == "" {
		timestamp = time.Now().Format(time.RFC3339)
	}
	return AuditEntry{
		Timestamp:   timestamp,
		SessionID:   AuditText(ctx.SessionID, 0),
		Tool:        AuditText(ctx.Snapshot.Tool, 0),
		Title:       AuditText(ctx.Snapshot.Title, 0),
		ProjectPath: AuditText(ctx.Snapshot.ProjectPath, 0),
		DurationMS:  ctx.DurationMS,
	}
}

func auditUpsertedNames(result Result, opts AuditOptions) []string {
	opts = normalizeAuditOptions(opts)
	if len(result.Upserted) == 0 {
		return nil
	}
	limit := len(result.Upserted)
	if limit > opts.MaxDecisions {
		limit = opts.MaxDecisions
	}
	out := make([]string, 0, limit)
	for _, entry := range result.Upserted[:limit] {
		out = append(out, AuditText(entry.Name, opts.MaxStringLength))
	}
	return out
}

// AuditTrail stores recent audit entries in newest-first order. It is safe for
// concurrent appends and readers, and List always returns deep copies.
type AuditTrail struct {
	mu      sync.RWMutex
	limit   int
	opts    AuditOptions
	entries []AuditEntry
}

func NewAuditTrail(limit int) *AuditTrail {
	return NewAuditTrailWithOptions(limit, AuditOptions{})
}

func NewAuditTrailWithOptions(limit int, opts AuditOptions) *AuditTrail {
	if limit <= 0 {
		limit = defaultAuditTrailLimit
	}
	return &AuditTrail{limit: limit, opts: normalizeAuditOptions(opts)}
}

func (t *AuditTrail) Append(entry AuditEntry) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	limit := t.effectiveLimit()
	t.entries = append([]AuditEntry{sanitizeAuditEntry(entry, t.opts)}, t.entries...)
	if len(t.entries) > limit {
		t.entries = t.entries[:limit]
	}
}
func (t *AuditTrail) effectiveLimit() int {
	if t.limit <= 0 {
		return defaultAuditTrailLimit
	}
	return t.limit
}

func (t *AuditTrail) RecordResult(ctx AuditContext, result Result) {
	if t == nil {
		return
	}
	t.Append(NewResultAuditEntry(ctx, result, t.opts))
}

func (t *AuditTrail) RecordError(ctx AuditContext, err error) {
	if t == nil {
		return
	}
	t.Append(NewErrorAuditEntry(ctx, err, t.opts))
}

func (t *AuditTrail) List() []AuditEntry {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]AuditEntry, len(t.entries))
	for i, entry := range t.entries {
		out[i] = CloneAuditEntry(entry)
	}
	return out
}

type AuditHealth struct {
	Runs             int            `json:"runs"`
	Completed        int            `json:"completed"`
	NoCandidates     int            `json:"no_candidates"`
	Failed           int            `json:"failed"`
	TotalCandidates  int            `json:"total_candidates"`
	Registered       int            `json:"registered"`
	Updated          int            `json:"updated"`
	Skipped          int            `json:"skipped"`
	AvgDurationMS    int64          `json:"avg_duration_ms,omitempty"`
	LatestTimestamp  string         `json:"latest_timestamp,omitempty"`
	Status           string         `json:"status"`
	IssueCode        string         `json:"issue_code,omitempty"`
	PrimaryIssue     string         `json:"primary_issue,omitempty"`
	SuggestedAction  string         `json:"suggested_action,omitempty"`
	SkipReasons      map[string]int `json:"skip_reasons,omitempty"`
	UnsupportedSteps map[string]int `json:"unsupported_steps,omitempty"`
}

func (t *AuditTrail) Health() AuditHealth {
	if t == nil {
		return EmptyAuditHealth()
	}
	return SummarizeAuditEntries(t.List(), t.opts)
}

func EmptyAuditHealth() AuditHealth {
	return AuditHealth{
		Status:          AuditHealthStatusEmpty,
		IssueCode:       AuditIssueNoRuns,
		SuggestedAction: "run an eligible successful session before expecting learned skills",
	}
}

func SummarizeAuditEntries(entries []AuditEntry, opts AuditOptions) AuditHealth {
	opts = normalizeAuditOptions(opts)
	health := EmptyAuditHealth()
	var durationTotal int64
	var durationCount int64
	skipReasons := map[string]int{}
	unsupportedSteps := map[string]int{}
	for _, entry := range entries {
		health.Runs++
		if auditTimestampAfter(entry.Timestamp, health.LatestTimestamp) {
			health.LatestTimestamp = entry.Timestamp
		}
		switch effectiveAuditStatus(entry) {
		case AuditStatusFailed:
			health.Failed++
		case AuditStatusNoCandidates:
			health.NoCandidates++
		default:
			health.Completed++
		}
		health.TotalCandidates += entry.Summary.TotalCandidates
		health.Registered += entry.Summary.Registered
		health.Updated += entry.Summary.Updated
		health.Skipped += entry.Summary.Skipped
		if entry.DurationMS > 0 {
			durationTotal += entry.DurationMS
			durationCount++
		}
		mergeAuditCounts(skipReasons, entry.Summary.SkipReasons)
		mergeAuditCounts(unsupportedSteps, entry.Summary.UnsupportedSteps)
	}
	if durationCount > 0 {
		health.AvgDurationMS = durationTotal / durationCount
	}
	health.Status, health.IssueCode, health.PrimaryIssue, health.SuggestedAction = diagnoseAuditHealth(health, skipReasons, unsupportedSteps, opts)
	health.SkipReasons = auditStringIntMap(skipReasons, opts.MaxSummaryItems, opts.MaxStringLength)
	health.UnsupportedSteps = auditStringIntMap(unsupportedSteps, opts.MaxSummaryItems, opts.MaxStringLength)
	return health
}

func diagnoseAuditHealth(health AuditHealth, skipReasons map[string]int, unsupportedSteps map[string]int, opts AuditOptions) (string, string, string, string) {
	opts = normalizeAuditOptions(opts)
	if health.Runs == 0 {
		return AuditHealthStatusEmpty, AuditIssueNoRuns, "", "run an eligible successful session before expecting learned skills"
	}
	if health.Failed > 0 && health.Completed == 0 {
		return AuditHealthStatusFailing, AuditIssueExtractionFailed, "recent extraction runs failed", "check LLM connectivity and skill store write access"
	}
	if health.Registered+health.Updated == 0 {
		if reason, ok := topAuditCount(skipReasons); ok {
			return AuditHealthStatusNoSignal, auditIssueCodeForReason(reason), "no skills learned: " + AuditText(reason, opts.MaxStringLength), suggestAuditActionForReason(reason)
		}
		if step, ok := topAuditCount(unsupportedSteps); ok {
			return AuditHealthStatusNoSignal, AuditIssueUnsupportedEvidence, "unsupported evidence: " + AuditText(step, opts.MaxStringLength), "prefer supported skill actions or add evidence support for this step type"
		}
		if health.TotalCandidates == 0 {
			return AuditHealthStatusNoSignal, AuditIssueNoReusableCandidates, "no reusable candidates found", "capture a longer successful workflow with repeatable commands or important events"
		}
		return AuditHealthStatusNoSignal, AuditIssueNoSkillsLearned, "no skills learned", "inspect skip reasons and evidence coverage before adjusting thresholds"
	}
	if health.Failed > 0 {
		return AuditHealthStatusNeedsAttention, AuditIssueSomeRunsFailed, "some extraction runs failed", "review failed audit entries while keeping successful learned skills"
	}
	if health.Skipped > health.Registered+health.Updated {
		if reason, ok := topAuditCount(skipReasons); ok {
			return AuditHealthStatusNeedsAttention, auditIssueCodeForReason(reason), "high skip rate: " + AuditText(reason, opts.MaxStringLength), suggestAuditActionForReason(reason)
		}
		return AuditHealthStatusNeedsAttention, AuditIssueHighSkipRate, "high skip rate", "review skip reasons and tighten extraction prompts only if useful skills are being missed"
	}
	return AuditHealthStatusHealthy, AuditIssueNone, "", ""
}

func auditIssueCodeForReason(reason string) string {
	return classifyAuditReason(reason).IssueCode()
}

func suggestAuditActionForReason(reason string) string {
	return classifyAuditReason(reason).SuggestedAction()
}

func topAuditCount(counts map[string]int) (string, bool) {
	var topKey string
	var topValue int
	for key, value := range counts {
		if key == "" || value == 0 {
			continue
		}
		if topKey == "" || value > topValue || (value == topValue && key < topKey) {
			topKey = key
			topValue = value
		}
	}
	return topKey, topKey != ""
}

func auditTimestampAfter(candidate string, current string) bool {
	if candidate == "" {
		return false
	}
	if current == "" {
		return true
	}
	candidateTime, candidateErr := time.Parse(time.RFC3339, candidate)
	currentTime, currentErr := time.Parse(time.RFC3339, current)
	if candidateErr == nil && currentErr == nil {
		return candidateTime.After(currentTime)
	}
	return candidate > current
}

func mergeAuditCounts(dst map[string]int, src map[string]int) {
	for key, value := range src {
		if key == "" || value == 0 {
			continue
		}
		dst[key] += value
	}
}

func DefaultAuditOptions() AuditOptions {
	return AuditOptions{
		MaxDecisions:            defaultAuditMaxDecisions,
		MaxReasonsPerDecision:   defaultAuditMaxReasonsPerDecision,
		MaxUnsupportedPerReport: defaultAuditMaxUnsupportedPerReport,
		MaxSummaryItems:         defaultAuditMaxSummaryItems,
		MaxStringLength:         defaultAuditMaxStringLength,
	}
}

func normalizeAuditOptions(opts AuditOptions) AuditOptions {
	defaults := DefaultAuditOptions()
	if opts.MaxDecisions <= 0 {
		opts.MaxDecisions = defaults.MaxDecisions
	}
	if opts.MaxReasonsPerDecision <= 0 {
		opts.MaxReasonsPerDecision = defaults.MaxReasonsPerDecision
	}
	if opts.MaxUnsupportedPerReport <= 0 {
		opts.MaxUnsupportedPerReport = defaults.MaxUnsupportedPerReport
	}
	if opts.MaxSummaryItems <= 0 {
		opts.MaxSummaryItems = defaults.MaxSummaryItems
	}
	if opts.MaxStringLength <= 0 {
		opts.MaxStringLength = defaults.MaxStringLength
	}
	return opts
}

// AuditSummary returns a safe, bounded aggregate view for diagnostic surfaces.
func (r Result) AuditSummary(opts AuditOptions) ResultSummary {
	return r.Summary().AuditView(opts)
}

// AuditDecisions returns a safe, bounded decision list for diagnostic surfaces.
func (r Result) AuditDecisions(opts AuditOptions) []Decision {
	opts = normalizeAuditOptions(opts)
	limit := len(r.Decisions)
	if limit > opts.MaxDecisions {
		limit = opts.MaxDecisions
	}
	out := make([]Decision, 0, limit)
	for _, decision := range r.Decisions[:limit] {
		out = append(out, sanitizeAuditDecision(decision, opts))
	}
	return out
}

// AuditText redacts and truncates free-form audit text. maxLen <= 0 uses the
// package default.
func AuditText(value string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = defaultAuditMaxStringLength
	}
	return auditString(value, maxLen)
}

func AuditStatus(summary ResultSummary) string {
	if summary.TotalCandidates == 0 {
		return AuditStatusNoCandidates
	}
	return AuditStatusCompleted
}

func sanitizeAuditEntry(entry AuditEntry, opts AuditOptions) AuditEntry {
	opts = normalizeAuditOptions(opts)
	entry.Timestamp = AuditText(entry.Timestamp, opts.MaxStringLength)
	entry.SessionID = AuditText(entry.SessionID, opts.MaxStringLength)
	entry.Tool = AuditText(entry.Tool, opts.MaxStringLength)
	entry.Title = AuditText(entry.Title, opts.MaxStringLength)
	entry.ProjectPath = AuditText(entry.ProjectPath, opts.MaxStringLength)
	entry.Status = effectiveAuditStatus(entry)
	entry.Summary = entry.Summary.AuditView(opts)
	entry.Decisions = auditDecisionSlice(entry.Decisions, opts)
	entry.Upserted = auditStringSlice(entry.Upserted, opts.MaxDecisions, opts.MaxStringLength)
	entry.Error = AuditText(entry.Error, opts.MaxStringLength)
	return entry
}

func auditDecisionSlice(in []Decision, opts AuditOptions) []Decision {
	if len(in) == 0 {
		return nil
	}
	limit := len(in)
	if limit > opts.MaxDecisions {
		limit = opts.MaxDecisions
	}
	out := make([]Decision, 0, limit)
	for _, decision := range in[:limit] {
		out = append(out, sanitizeAuditDecision(decision, opts))
	}
	return out
}

func effectiveAuditStatus(entry AuditEntry) string {
	switch entry.Status {
	case AuditStatusCompleted, AuditStatusNoCandidates, AuditStatusFailed:
		return entry.Status
	}
	if entry.Error != "" {
		return AuditStatusFailed
	}
	return AuditStatus(entry.Summary)
}
func CloneAuditEntry(entry AuditEntry) AuditEntry {
	entry.Upserted = append([]string(nil), entry.Upserted...)
	entry.Decisions = append([]Decision(nil), entry.Decisions...)
	if entry.Summary.SkipReasons != nil {
		entry.Summary.SkipReasons = cloneStringIntMap(entry.Summary.SkipReasons)
	}
	if entry.Summary.UnsupportedSteps != nil {
		entry.Summary.UnsupportedSteps = cloneStringIntMap(entry.Summary.UnsupportedSteps)
	}
	for i := range entry.Decisions {
		entry.Decisions[i].Quality.Reasons = append([]string(nil), entry.Decisions[i].Quality.Reasons...)
		entry.Decisions[i].Evidence.Reasons = append([]string(nil), entry.Decisions[i].Evidence.Reasons...)
		entry.Decisions[i].Evidence.UnsupportedSteps = append([]string(nil), entry.Decisions[i].Evidence.UnsupportedSteps...)
	}
	return entry
}

func sanitizeAuditDecision(decision Decision, opts AuditOptions) Decision {
	decision.PatternName = auditString(decision.PatternName, opts.MaxStringLength)
	decision.Reason = auditString(decision.Reason, opts.MaxStringLength)
	decision.MatchedSkillName = auditString(decision.MatchedSkillName, opts.MaxStringLength)
	decision.Quality.Reasons = auditStringSlice(decision.Quality.Reasons, opts.MaxReasonsPerDecision, opts.MaxStringLength)
	decision.Evidence.Reasons = auditStringSlice(decision.Evidence.Reasons, opts.MaxReasonsPerDecision, opts.MaxStringLength)
	decision.Evidence.UnsupportedSteps = auditStringSlice(decision.Evidence.UnsupportedSteps, opts.MaxUnsupportedPerReport, opts.MaxStringLength)
	return decision
}

// AuditView returns a safe, bounded copy of the summary while preserving the
// complete numeric counters.
func (s ResultSummary) AuditView(opts AuditOptions) ResultSummary {
	opts = normalizeAuditOptions(opts)
	s.SkipReasons = auditStringIntMap(s.SkipReasons, opts.MaxSummaryItems, opts.MaxStringLength)
	s.UnsupportedSteps = auditStringIntMap(s.UnsupportedSteps, opts.MaxSummaryItems, opts.MaxStringLength)
	return s
}

func auditStringSlice(in []string, maxItems int, maxLen int) []string {
	if len(in) == 0 || maxItems <= 0 {
		return nil
	}
	limit := len(in)
	if limit > maxItems {
		limit = maxItems
	}
	out := make([]string, 0, limit)
	for _, value := range in[:limit] {
		out = append(out, auditString(value, maxLen))
	}
	return out
}

func auditStringIntMap(in map[string]int, maxItems int, maxLen int) map[string]int {
	if len(in) == 0 || maxItems <= 0 {
		return nil
	}
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if in[keys[i]] == in[keys[j]] {
			return keys[i] < keys[j]
		}
		return in[keys[i]] > in[keys[j]]
	})
	if len(keys) > maxItems {
		keys = keys[:maxItems]
	}
	out := make(map[string]int, len(keys))
	for _, key := range keys {
		out[auditString(key, maxLen)] += in[key]
	}
	return out
}

func cloneStringIntMap(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func auditString(value string, maxLen int) string {
	redacted := RedactExperienceText(value)
	if maxLen <= 0 {
		return redacted
	}
	runes := []rune(redacted)
	if len(runes) <= maxLen {
		return redacted
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}
