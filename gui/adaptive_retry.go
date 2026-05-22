package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// FailureCategory classifies a failed tool or LLM operation.
type FailureCategory string

type RetryAction string

const (
	FailureTransient   FailureCategory = "transient"
	FailureNetwork     FailureCategory = "network"
	FailurePeriodLimit FailureCategory = "period_limit"
	FailurePermission  FailureCategory = "permission"
	FailureArgs        FailureCategory = "args"
	FailureLogic       FailureCategory = "logic"
	FailureUnknown     FailureCategory = "unknown"

	// Kept as alias for backward compatibility in tests and trace logs.
	FailureRateLimit = FailureTransient
)

const (
	RetryActionRetry   RetryAction = "retry"
	RetryActionFix     RetryAction = "fix"
	RetryActionSkip    RetryAction = "skip"
	RetryActionDisable RetryAction = "disable"
)

func (category FailureCategory) String() string {
	return string(category)
}

const (
	adaptiveRetryReviewThreshold            = 3
	adaptiveRetryReviewedFailureCountPrefix = "reviewed_failure_count:"
	defaultMaxFailures                      = 5
	maxNetworkRetries                       = 3
	maxTransientRetries                     = 3
	baseRetryDelay                          = 1 * time.Second
	baseTransientDelay                      = 5 * time.Second
)

// RetryDecision describes the next retry action.
type RetryDecision struct {
	Action       RetryAction
	Delay        time.Duration
	ErrorContext string
	Attempt      int
	ProviderName string
	Model        string
	WireAPI      string
}

// AdaptiveRetry classifies failures and chooses retry behavior.
type AdaptiveRetry struct {
	failureCounts       map[string]int
	failureStreaks      map[string]int
	lastFailureCategory map[string]FailureCategory
	maxFailures         int
	disabledTools       map[string]bool
	recorder            *TrajectoryRecorder
	memoryStore         *memory.Store
}

// NewAdaptiveRetry creates an adaptive retry controller.
func NewAdaptiveRetry(recorder *TrajectoryRecorder) *AdaptiveRetry {
	return &AdaptiveRetry{
		failureCounts:       make(map[string]int),
		failureStreaks:      make(map[string]int),
		lastFailureCategory: make(map[string]FailureCategory),
		maxFailures:         defaultMaxFailures,
		disabledTools:       make(map[string]bool),
		recorder:            recorder,
	}
}

func (r *AdaptiveRetry) ensureState() {
	if r.failureCounts == nil {
		r.failureCounts = make(map[string]int)
	}
	if r.failureStreaks == nil {
		r.failureStreaks = make(map[string]int)
	}
	if r.lastFailureCategory == nil {
		r.lastFailureCategory = make(map[string]FailureCategory)
	}
	if r.disabledTools == nil {
		r.disabledTools = make(map[string]bool)
	}
	if r.maxFailures <= 0 {
		r.maxFailures = defaultMaxFailures
	}
}

func normalizeAdaptiveRetryCategory(category FailureCategory) FailureCategory {
	categoryText := strings.TrimSpace(category.String())
	if categoryText == "" {
		return FailureUnknown
	}
	return FailureCategory(categoryText)
}

func adaptiveRetryCategoryKey(toolName, category string) string {
	return strings.TrimSpace(toolName) + "::" + strings.TrimSpace(category)
}

// SetMemoryStore configures optional long-term memory persistence for retry evidence.
func (r *AdaptiveRetry) SetMemoryStore(store *memory.Store) {
	if r == nil {
		return
	}
	r.memoryStore = store
}

// Classify maps an error to a retry failure category.
func (r *AdaptiveRetry) Classify(toolName string, err error) FailureCategory {
	return classifyAdaptiveRetryFailure(err)
}

// Decide chooses a retry strategy for a failure category and attempt count.
func (r *AdaptiveRetry) Decide(toolName string, category FailureCategory, attempt int) RetryDecision {
	if r == nil {
		return RetryDecision{Action: RetryActionSkip, ErrorContext: "adaptive retry is not configured", Attempt: attempt}
	}
	r.ensureState()
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		toolName = "unknown_tool"
	}
	category = normalizeAdaptiveRetryCategory(category)
	if r.failureCounts[toolName] >= r.maxFailures {
		return RetryDecision{
			Action:       RetryActionDisable,
			ErrorContext: fmt.Sprintf("tool %s failed %d times and has been disabled; use an alternative path", toolName, r.failureCounts[toolName]),
			Attempt:      attempt,
		}
	}

	switch category {
	case FailurePeriodLimit:
		return RetryDecision{
			Action:       RetryActionSkip,
			ErrorContext: fmt.Sprintf("MaClaw period quota is exhausted for tool %s; wait for quota recovery or switch provider", toolName),
			Attempt:      attempt,
		}
	case FailureTransient:
		if attempt >= maxTransientRetries {
			return RetryDecision{
				Action:       RetryActionSkip,
				ErrorContext: fmt.Sprintf("tool %s transient server error retry limit reached (%d); skip", toolName, maxTransientRetries),
				Attempt:      attempt,
			}
		}
		return RetryDecision{
			Action:  RetryActionRetry,
			Delay:   baseTransientDelay * time.Duration(1<<uint(attempt)),
			Attempt: attempt,
		}
	case FailureNetwork:
		if attempt >= maxNetworkRetries {
			return RetryDecision{
				Action:       RetryActionSkip,
				ErrorContext: fmt.Sprintf("tool %s network retry limit reached (%d); skip", toolName, maxNetworkRetries),
				Attempt:      attempt,
			}
		}
		return RetryDecision{
			Action:  RetryActionRetry,
			Delay:   baseRetryDelay * time.Duration(1<<uint(attempt)),
			Attempt: attempt,
		}
	case FailureArgs, FailureLogic:
		return RetryDecision{
			Action:       RetryActionFix,
			ErrorContext: fmt.Sprintf("tool %s failed with %s; adjust arguments or logic before retrying", toolName, string(category)),
			Attempt:      attempt,
		}
	case FailurePermission:
		return RetryDecision{
			Action:       RetryActionSkip,
			ErrorContext: fmt.Sprintf("tool %s failed because of permission or authentication; fix credentials before retrying", toolName),
			Attempt:      attempt,
		}
	default:
		if attempt >= 1 {
			return RetryDecision{
				Action:       RetryActionSkip,
				ErrorContext: fmt.Sprintf("tool %s failed with an unknown error after %d attempts; skip", toolName, attempt),
				Attempt:      attempt,
			}
		}
		return RetryDecision{
			Action:  RetryActionRetry,
			Delay:   baseRetryDelay,
			Attempt: attempt,
		}
	}
}

// RecordFailure records a failed retry decision.
func (r *AdaptiveRetry) RecordFailure(toolName string, category FailureCategory, decision RetryDecision) {
	if r == nil {
		return
	}
	r.ensureState()
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		toolName = "unknown_tool"
	}
	category = normalizeAdaptiveRetryCategory(category)
	r.failureCounts[toolName]++
	key := adaptiveRetryCategoryKey(toolName, category.String())
	if r.lastFailureCategory[toolName] == category {
		r.failureStreaks[key]++
	} else {
		r.failureStreaks[key] = 1
		r.lastFailureCategory[toolName] = category
	}
	if r.failureCounts[toolName] >= r.maxFailures {
		r.disabledTools[toolName] = true
	}
	r.persistFailureMemory(toolName, category, decision)
	if r.recorder == nil {
		return
	}
	content := fmt.Sprintf("tool=%s category=%s action=%s attempt=%d",
		toolName, string(category), decision.Action, decision.Attempt)
	r.recorder.Record("system", content, nil, "", "adaptive_retry")
}

func (r *AdaptiveRetry) persistFailureMemory(toolName string, category FailureCategory, decision RetryDecision) {
	if r == nil || r.memoryStore == nil {
		return
	}
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		toolName = "unknown_tool"
	}
	categoryText := strings.TrimSpace(category.String())
	if categoryText == "" {
		categoryText = FailureUnknown.String()
	}
	count := r.failureStreaks[adaptiveRetryCategoryKey(toolName, categoryText)]
	if count == 0 {
		count = r.failureCounts[toolName]
	}
	observedAt := time.Now().UTC().Format(time.RFC3339)
	content := formatAdaptiveRetryMemoryContent(toolName, categoryText, count, decision, r.disabledTools[toolName], observedAt, observedAt)
	entryID := adaptiveRetryMemoryID(toolName, categoryText)
	existing := r.memoryStore.SearchDirectByID(entryID)
	var existingTags []string
	if len(existing) > 0 {
		existingTags = existing[0].Tags
		firstObservedAt := adaptiveRetryFirstObservedAt(existing[0].Content, observedAt)
		content = formatAdaptiveRetryMemoryContent(toolName, categoryText, count, decision, r.disabledTools[toolName], firstObservedAt, observedAt)
		content = adaptiveRetryAppendPreservedReviewAudit(content, existing[0].Content)
	}
	tags := adaptiveRetryMemoryTags(toolName, categoryText, count, decision, r.disabledTools[toolName], existingTags)
	if _, err := r.memoryStore.UpsertProjectKnowledge(memory.ProjectKnowledgeUpsertOptions{
		ID:                entryID,
		Title:             "Adaptive retry: " + toolName + " / " + categoryText,
		Content:           content,
		Tags:              tags,
		SourceType:        string(experienceTraceSourceToolUsage),
		SourceURL:         "experience://adaptive_retry/" + adaptiveRetrySafeID(toolName) + "/" + adaptiveRetrySafeID(categoryText),
		MergeExistingTags: adaptiveRetryMergeMemoryTags,
	}); err != nil {
		log.Printf("[adaptive-retry] failed to upsert retry memory %s: %v", entryID, err)
	}
}

func adaptiveRetryMergeMemoryTags(existing, desired []string) []string {
	merged := append([]string(nil), desired...)
	for _, tag := range existing {
		tag = strings.TrimSpace(tag)
		if tag == "" || adaptiveRetryVolatileMemoryTag(tag) {
			continue
		}
		merged = append(merged, tag)
	}
	return normalizeUsageMemoryTags(merged)
}

func adaptiveRetryVolatileMemoryTag(tag string) bool {
	switch {
	case tag == experienceReviewRequiredTag,
		tag == experienceReviewResolvedTag,
		strings.HasPrefix(tag, experienceReviewStatusTagPrefix),
		strings.HasPrefix(tag, experienceReviewedAtTagPrefix),
		strings.HasPrefix(tag, adaptiveRetryReviewedFailureCountPrefix),
		normalizeExperienceReviewLifecycleTagKind(tag).IsStateTag():
		return true
	case strings.HasPrefix(tag, "failure_count:"),
		strings.HasPrefix(tag, "action:"),
		strings.HasPrefix(tag, "provider:"),
		strings.HasPrefix(tag, "model:"),
		strings.HasPrefix(tag, "wire_api:"):
		return true
	default:
		return false
	}
}

func adaptiveRetryMemoryTags(toolName, categoryText string, count int, decision RetryDecision, disabled bool, existingTags []string) []string {
	tags := []string{experienceTraceKindToolRecoveryPattern.String(), "adaptive_retry", "tool:" + toolName, "category:" + categoryText, "action:" + string(decision.Action), fmt.Sprintf("failure_count:%d", count)}
	if provider := adaptiveRetrySafeTagValue(decision.ProviderName); provider != "" {
		tags = append(tags, "provider:"+provider)
	}
	if model := adaptiveRetrySafeTagValue(decision.Model); model != "" {
		tags = append(tags, "model:"+model)
	}
	if wireAPI := adaptiveRetrySafeTagValue(decision.WireAPI); wireAPI != "" {
		tags = append(tags, "wire_api:"+wireAPI)
	}
	if adaptiveRetryShouldRequireReview(categoryText, existingTags, count, disabled, decision.Action) {
		tags = append(tags, experienceReviewRequiredTag)
	} else {
		tags = append(tags, adaptiveRetryPreservedReviewTags(existingTags)...)
	}
	if disabled {
		tags = append(tags, "disabled")
	}
	return normalizeUsageMemoryTags(tags)
}

func adaptiveRetryShouldRequireReview(categoryText string, existingTags []string, count int, disabled bool, action RetryAction) bool {
	if disabled || action == RetryActionDisable {
		return true
	}
	if count < adaptiveRetryReviewThreshold {
		return false
	}
	if adaptiveRetryIsRetryableNoiseCategory(categoryText) && action == RetryActionRetry {
		return false
	}
	reviewedCount := adaptiveRetryReviewedFailureCount(existingTags)
	if reviewedCount > 0 && experienceTraceReviewResolved(existingTags) && count < reviewedCount+adaptiveRetryReviewThreshold {
		return false
	}
	return true
}

func adaptiveRetryIsRetryableNoiseCategory(categoryText string) bool {
	switch FailureCategory(strings.TrimSpace(categoryText)) {
	case FailureTransient, FailureNetwork:
		return true
	default:
		return false
	}
}

func adaptiveRetryPreservedReviewTags(tags []string) []string {
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || tag == experienceReviewRequiredTag {
			continue
		}
		if tag == experienceReviewResolvedTag || strings.HasPrefix(tag, experienceReviewStatusTagPrefix) || strings.HasPrefix(tag, experienceReviewedAtTagPrefix) || strings.HasPrefix(tag, adaptiveRetryReviewedFailureCountPrefix) || normalizeExperienceReviewLifecycleTagKind(tag).IsStateTag() {
			result = append(result, tag)
		}
	}
	return result
}

func adaptiveRetryReviewedFailureCount(tags []string) int {
	for _, tag := range tags {
		if !strings.HasPrefix(tag, adaptiveRetryReviewedFailureCountPrefix) {
			continue
		}
		var count int
		if _, err := fmt.Sscanf(strings.TrimPrefix(tag, adaptiveRetryReviewedFailureCountPrefix), "%d", &count); err == nil && count > 0 {
			return count
		}
	}
	return 0
}

func adaptiveRetryFirstObservedAt(content, fallback string) string {
	const prefix = "- First observed at:"
	for _, line := range strings.Split(content, "\n") {
		if value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), prefix)); value != strings.TrimSpace(line) && value != "" {
			return value
		}
	}
	return fallback
}

func adaptiveRetryAppendPreservedReviewAudit(content, previousContent string) string {
	const marker = "Experience review record:"
	idx := strings.Index(previousContent, marker)
	if idx < 0 {
		return content
	}
	audit := strings.TrimSpace(previousContent[idx:])
	if audit == "" || strings.Contains(content, marker) {
		return content
	}
	return strings.TrimSpace(content) + "\n\n" + audit
}

func formatAdaptiveRetryMemoryContent(toolName, category string, count int, decision RetryDecision, disabled bool, firstObservedAt, lastObservedAt string) string {
	var b strings.Builder
	b.WriteString("Adaptive retry failure evidence")
	fmt.Fprintf(&b, "\n- Tool: %s", toolName)
	fmt.Fprintf(&b, "\n- Failure category: %s", category)
	fmt.Fprintf(&b, "\n- Failure count: %d", count)
	if strings.TrimSpace(firstObservedAt) != "" {
		fmt.Fprintf(&b, "\n- First observed at: %s", strings.TrimSpace(firstObservedAt))
	}
	if strings.TrimSpace(lastObservedAt) != "" {
		fmt.Fprintf(&b, "\n- Last observed at: %s", strings.TrimSpace(lastObservedAt))
	}
	fmt.Fprintf(&b, "\n- Decision: %s", decision.Action)
	fmt.Fprintf(&b, "\n- Attempt: %d", decision.Attempt)
	if strings.TrimSpace(decision.ProviderName) != "" {
		fmt.Fprintf(&b, "\n- Provider: %s", strings.TrimSpace(decision.ProviderName))
	}
	if strings.TrimSpace(decision.Model) != "" {
		fmt.Fprintf(&b, "\n- Model: %s", strings.TrimSpace(decision.Model))
	}
	if strings.TrimSpace(decision.WireAPI) != "" {
		fmt.Fprintf(&b, "\n- Wire API: %s", strings.TrimSpace(decision.WireAPI))
	}
	if decision.Delay > 0 {
		fmt.Fprintf(&b, "\n- Delay: %s", decision.Delay)
	}
	if strings.TrimSpace(decision.ErrorContext) != "" {
		fmt.Fprintf(&b, "\n- Error context: %s", strings.TrimSpace(decision.ErrorContext))
	}
	if disabled {
		b.WriteString("\n- Disabled: true")
	}
	b.WriteString("\nSafety: retry evidence only; this memory records a failed tool/LLM recovery decision and does not authorize automatic execution, routing changes, credential changes, or skill installation.")
	return strings.TrimSpace(b.String())
}

func adaptiveRetryMemoryID(toolName, category string) string {
	return "adaptive-retry-" + adaptiveRetrySafeID(toolName) + "-" + adaptiveRetrySafeID(category)
}

func adaptiveRetrySafeTagValue(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, " ", "_")
	value = safeFilenameRe.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-_")
	if len(value) > 64 {
		return value[:64]
	}
	return value
}

func adaptiveRetrySafeID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "unknown"
	}
	value = safeFilenameRe.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "unknown"
	}
	if len(value) > 64 {
		return value[:64]
	}
	return value
}

// IsDisabled reports whether a tool has been disabled after repeated failures.
func (r *AdaptiveRetry) IsDisabled(toolName string) bool {
	if r == nil {
		return false
	}
	r.ensureState()
	return r.disabledTools[strings.TrimSpace(toolName)]
}
