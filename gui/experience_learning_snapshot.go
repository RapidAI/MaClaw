package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/session"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	experienceSnapshotRoutingHintLimit  = 8
	experienceSnapshotSkillNudgeLimit   = 8
	experienceSnapshotRecoveryLimit     = 8
	experienceSnapshotUsagePatternLimit = 8
	experienceSnapshotTraceDetailLimit  = 20
	experienceSnapshotSessionTraceLimit = 6
)

// ExperienceTraceDetail is a read-only detail row for a distilled learning signal.
type ExperienceTraceDetail struct {
	ID                 string   `json:"id"`
	Kind               string   `json:"kind"`
	Title              string   `json:"title"`
	Summary            string   `json:"summary,omitempty"`
	Detail             string   `json:"detail,omitempty"`
	SourceType         string   `json:"source_type,omitempty"`
	SourceURL          string   `json:"source_url,omitempty"`
	SourceTraceID      string   `json:"source_trace_id,omitempty"`
	Tags               []string `json:"tags,omitempty"`
	Evidence           int      `json:"evidence,omitempty"`
	Confidence         float64  `json:"confidence,omitempty"`
	Impact             string   `json:"impact,omitempty"`
	ReviewRequired     bool     `json:"review_required,omitempty"`
	ReviewAction       string   `json:"review_action,omitempty"`
	ReviewStatus       string   `json:"review_status,omitempty"`
	NextActionKind     string   `json:"next_action_kind,omitempty"`
	NextAction         string   `json:"next_action,omitempty"`
	ReviewedAt         string   `json:"reviewed_at,omitempty"`
	Reviewer           string   `json:"reviewer,omitempty"`
	ReviewNote         string   `json:"review_note,omitempty"`
	ReviewCount        int      `json:"review_count,omitempty"`
	FollowUpStatus     string   `json:"follow_up_status,omitempty"`
	FollowUpActionKind string   `json:"follow_up_action_kind,omitempty"`
	FollowUpAt         string   `json:"follow_up_at,omitempty"`
	FollowUpActor      string   `json:"follow_up_actor,omitempty"`
	FollowUpNote       string   `json:"follow_up_note,omitempty"`
	FollowUpCount      int      `json:"follow_up_count,omitempty"`
	UpdatedAt          string   `json:"updated_at,omitempty"`
}

// ExperienceNextActionSummary groups read-only manual follow-up guidance by kind.
type ExperienceNextActionSummary struct {
	Kind            string `json:"kind"`
	Count           int    `json:"count"`
	LatestTraceID   string `json:"latest_trace_id,omitempty"`
	LatestTitle     string `json:"latest_title,omitempty"`
	LatestAction    string `json:"latest_action,omitempty"`
	LatestUpdatedAt string `json:"latest_updated_at,omitempty"`
}

// ExperienceReviewSummary groups review-gated signals by normalized status.
type ExperienceReviewSummary struct {
	Status           string `json:"status"`
	Count            int    `json:"count"`
	RequiredCount    int    `json:"required_count,omitempty"`
	LatestTraceID    string `json:"latest_trace_id,omitempty"`
	LatestTitle      string `json:"latest_title,omitempty"`
	LatestKind       string `json:"latest_kind,omitempty"`
	LatestAction     string `json:"latest_action,omitempty"`
	LatestReviewer   string `json:"latest_reviewer,omitempty"`
	LatestNote       string `json:"latest_note,omitempty"`
	LatestReviewedAt string `json:"latest_reviewed_at,omitempty"`
	LatestUpdatedAt  string `json:"latest_updated_at,omitempty"`
}

// ExperienceFollowUpSummary groups recorded manual follow-up audit outcomes.
type ExperienceFollowUpSummary struct {
	Status             string `json:"status"`
	Count              int    `json:"count"`
	TriggeredRollback  bool   `json:"triggered_rollback,omitempty"`
	TriggeredCount     int    `json:"triggered_count,omitempty"`
	LatestTraceID      string `json:"latest_trace_id,omitempty"`
	LatestTitle        string `json:"latest_title,omitempty"`
	RecommendedTraceID string `json:"recommended_trace_id,omitempty"`
	RecommendedTitle   string `json:"recommended_title,omitempty"`
	RecommendedReason  string `json:"recommended_reason,omitempty"`
	LatestActionKind   string `json:"latest_action_kind,omitempty"`
	LatestNote         string `json:"latest_note,omitempty"`
	LatestUpdatedAt    string `json:"latest_updated_at,omitempty"`
}

// ExperienceFollowUpActionSummary groups recorded manual follow-up audit outcomes
// by the original action kind so draft-review evidence can be inspected directly.
type ExperienceFollowUpActionSummary struct {
	Kind               string         `json:"kind"`
	Count              int            `json:"count"`
	StatusCounts       map[string]int `json:"status_counts,omitempty"`
	TriggeredRollback  bool           `json:"triggered_rollback,omitempty"`
	TriggeredCount     int            `json:"triggered_count,omitempty"`
	LatestTraceID      string         `json:"latest_trace_id,omitempty"`
	LatestTitle        string         `json:"latest_title,omitempty"`
	RecommendedTraceID string         `json:"recommended_trace_id,omitempty"`
	RecommendedTitle   string         `json:"recommended_title,omitempty"`
	RecommendedReason  string         `json:"recommended_reason,omitempty"`
	LatestStatus       string         `json:"latest_status,omitempty"`
	LatestNote         string         `json:"latest_note,omitempty"`
	LatestUpdatedAt    string         `json:"latest_updated_at,omitempty"`
}

// ExperienceLearningSnapshot is a read-only view of the conservative signals
// MaClaw has distilled from memory maintenance and tool usage traces.
type ExperienceLearningSnapshot struct {
	RoutingHints                    []coretool.ToolRoutingHint         `json:"routing_hints"`
	SkillNudgeCandidates            []coretool.ToolSkillNudgeCandidate `json:"skill_nudge_candidates"`
	RecoveryPatterns                []coretool.ToolRecoveryPattern     `json:"recovery_patterns"`
	UsagePatterns                   []coretool.UsagePattern            `json:"usage_patterns"`
	TraceDetails                    []ExperienceTraceDetail            `json:"trace_details"`
	MemoryExperience                *memory.ExperienceDistillResult    `json:"memory_experience,omitempty"`
	TraceKindCounts                 map[string]int                     `json:"trace_kind_counts"`
	TraceSourceCounts               map[string]int                     `json:"trace_source_counts"`
	ReviewStatusCounts              map[string]int                     `json:"review_status_counts"`
	NextActionKindCounts            map[string]int                     `json:"next_action_kind_counts"`
	FollowUpStatusCounts            map[string]int                     `json:"follow_up_status_counts"`
	FollowUpActionKindCounts        map[string]int                     `json:"follow_up_action_kind_counts"`
	ReviewSummaries                 []ExperienceReviewSummary          `json:"review_summaries"`
	NextActionSummaries             []ExperienceNextActionSummary      `json:"next_action_summaries"`
	FollowUpSummaries               []ExperienceFollowUpSummary        `json:"follow_up_summaries"`
	FollowUpActionSummaries         []ExperienceFollowUpActionSummary  `json:"follow_up_action_summaries"`
	TraceDetailCount                int                                `json:"trace_detail_count"`
	RoutingHintCount                int                                `json:"routing_hint_count"`
	SkillNudgeCount                 int                                `json:"skill_nudge_count"`
	RecoveryPatternCount            int                                `json:"recovery_pattern_count"`
	UsagePatternCount               int                                `json:"usage_pattern_count"`
	ProtectedMemoryCount            int                                `json:"protected_memory_count"`
	ReviewRequiredTraceCount        int                                `json:"review_required_trace_count"`
	NextActionTraceCount            int                                `json:"next_action_trace_count"`
	FollowUpTraceCount              int                                `json:"follow_up_trace_count"`
	LayeredMemoryRecommended        bool                               `json:"layered_memory_recommended"`
	LayeredMemoryReason             string                             `json:"layered_memory_reason,omitempty"`
	MemoryMaintenanceRecommendation string                             `json:"memory_maintenance_recommendation,omitempty"`
	MemoryMaintenanceBoundary       string                             `json:"memory_maintenance_boundary,omitempty"`
}

// GetExperienceLearningSnapshot returns the current learning signals for UI
// detail views. It does not mutate routing, memory, or skills.
func (a *App) GetExperienceLearningSnapshot() ExperienceLearningSnapshot {
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		a.ensureMemoryStore()
	}
	snapshot := buildExperienceLearningSnapshot(a.usageTracker, a.memoryStore)
	a.ensureSessionStore()
	if a.sessionSearchStore != nil {
		if summaries, err := a.sessionSearchStore.ListRecent(experienceSnapshotSessionTraceLimit); err == nil {
			sessionDetails := buildExperienceSessionTraceDetails(summaries)
			snapshot.TraceDetails = append(snapshot.TraceDetails, sessionDetails...)
			snapshot.ReviewRequiredTraceCount += countReviewRequiredExperienceTraces(sessionDetails)
			snapshot.TraceDetailCount += len(sessionDetails)
			addExperienceTraceCounts(snapshot.TraceKindCounts, sessionDetails, experienceTraceKind)
			addExperienceTraceCounts(snapshot.TraceSourceCounts, sessionDetails, experienceTraceSource)
			addExperienceTraceCounts(snapshot.ReviewStatusCounts, sessionDetails, experienceTraceReviewStatus)
			addExperienceTraceCounts(snapshot.NextActionKindCounts, sessionDetails, experienceTraceNextActionKind)
			addExperienceTraceCounts(snapshot.FollowUpStatusCounts, sessionDetails, experienceTraceFollowUpStatus)
			addExperienceTraceCounts(snapshot.FollowUpActionKindCounts, sessionDetails, experienceTraceFollowUpActionKind)
			snapshot.NextActionTraceCount += countNextActionExperienceTraces(sessionDetails)
			snapshot.FollowUpTraceCount += countFollowUpExperienceTraces(sessionDetails)
		}
	}
	return snapshot
}

func buildExperienceLearningSnapshot(tracker *coretool.UsageTracker, mem *memory.Store) ExperienceLearningSnapshot {
	return buildExperienceLearningSnapshotWithTraceLimit(tracker, mem, experienceSnapshotTraceDetailLimit)
}

func buildExperienceLearningSnapshotWithTraceLimit(tracker *coretool.UsageTracker, mem *memory.Store, traceDetailLimit int) ExperienceLearningSnapshot {
	snapshot := ExperienceLearningSnapshot{
		RoutingHints:             []coretool.ToolRoutingHint{},
		SkillNudgeCandidates:     []coretool.ToolSkillNudgeCandidate{},
		RecoveryPatterns:         []coretool.ToolRecoveryPattern{},
		UsagePatterns:            []coretool.UsagePattern{},
		TraceDetails:             []ExperienceTraceDetail{},
		TraceKindCounts:          map[string]int{},
		TraceSourceCounts:        map[string]int{},
		ReviewStatusCounts:       map[string]int{},
		NextActionKindCounts:     map[string]int{},
		FollowUpStatusCounts:     map[string]int{},
		FollowUpActionKindCounts: map[string]int{},
		ReviewSummaries:          []ExperienceReviewSummary{},
		NextActionSummaries:      []ExperienceNextActionSummary{},
		FollowUpSummaries:        []ExperienceFollowUpSummary{},
		FollowUpActionSummaries:  []ExperienceFollowUpActionSummary{},
	}

	if tracker != nil {
		routingHints := tracker.DistillRoutingHints(14, 3)
		snapshot.RoutingHintCount = len(routingHints)
		snapshot.RoutingHints = limitRoutingHints(routingHints, experienceSnapshotRoutingHintLimit)

		skillNudges := tracker.DistillSkillNudgeCandidates(30, 3)
		snapshot.SkillNudgeCount = len(skillNudges)
		snapshot.SkillNudgeCandidates = limitSkillNudges(skillNudges, experienceSnapshotSkillNudgeLimit)

		recoveryPatterns := tracker.DistillRecoveryPatterns(30, 3)
		snapshot.RecoveryPatternCount = len(recoveryPatterns)
		snapshot.RecoveryPatterns = limitRecoveryPatterns(recoveryPatterns, experienceSnapshotRecoveryLimit)

		patterns := tracker.ExtractPatterns(14)
		sort.Slice(patterns, func(i, j int) bool {
			if patterns[i].SuccessRate != patterns[j].SuccessRate {
				return patterns[i].SuccessRate > patterns[j].SuccessRate
			}
			if patterns[i].Count != patterns[j].Count {
				return patterns[i].Count > patterns[j].Count
			}
			return patterns[i].ToolName < patterns[j].ToolName
		})
		snapshot.UsagePatternCount = len(patterns)
		snapshot.UsagePatterns = limitUsagePatterns(patterns, experienceSnapshotUsagePatternLimit)
	}

	if mem != nil {
		distiller := memory.NewExperienceDistiller()
		result := distiller.Analyze(mem.List("", ""))
		snapshot.MemoryExperience = &result
		snapshot.ProtectedMemoryCount = result.ProtectedCandidates
		snapshot.LayeredMemoryRecommended = result.LayeredRecommended
		snapshot.LayeredMemoryReason = result.Reason
		snapshot.MemoryMaintenanceRecommendation = experienceMemoryMaintenanceRecommendation(result)
		snapshot.MemoryMaintenanceBoundary = "read-only memory maintenance snapshot; no compression, promotion, deletion, or rewrite was performed"
	}

	traceDetails := collectExperienceTraceDetails(snapshot, mem)
	snapshot.TraceDetailCount = len(traceDetails)
	snapshot.TraceKindCounts = countExperienceTracesBy(traceDetails, experienceTraceKind)
	snapshot.TraceSourceCounts = countExperienceTracesBy(traceDetails, experienceTraceSource)
	snapshot.ReviewStatusCounts = countExperienceTracesBy(traceDetails, experienceTraceReviewStatus)
	snapshot.NextActionKindCounts = countExperienceTracesBy(traceDetails, experienceTraceNextActionKind)
	snapshot.FollowUpStatusCounts = countExperienceTracesBy(traceDetails, experienceTraceFollowUpStatus)
	snapshot.FollowUpActionKindCounts = countExperienceTracesBy(traceDetails, experienceTraceFollowUpActionKind)
	snapshot.ReviewSummaries = buildExperienceReviewSummaries(traceDetails)
	snapshot.NextActionSummaries = buildExperienceNextActionSummaries(traceDetails)
	snapshot.FollowUpSummaries = buildExperienceFollowUpSummaries(traceDetails)
	snapshot.FollowUpActionSummaries = buildExperienceFollowUpActionSummaries(traceDetails)
	snapshot.ReviewRequiredTraceCount = countReviewRequiredExperienceTraces(traceDetails)
	snapshot.NextActionTraceCount = countNextActionExperienceTraces(traceDetails)
	snapshot.FollowUpTraceCount = countFollowUpExperienceTraces(traceDetails)
	snapshot.TraceDetails = limitExperienceTraceDetails(traceDetails, traceDetailLimit)
	return snapshot
}

func countExperienceTracesBy(details []ExperienceTraceDetail, keyFn func(ExperienceTraceDetail) string) map[string]int {
	counts := map[string]int{}
	addExperienceTraceCounts(counts, details, keyFn)
	return counts
}

func addExperienceTraceCounts(counts map[string]int, details []ExperienceTraceDetail, keyFn func(ExperienceTraceDetail) string) {
	if counts == nil {
		return
	}
	for _, detail := range details {
		if key := strings.TrimSpace(keyFn(detail)); key != "" {
			counts[key]++
		}
	}
}

func experienceTraceKind(detail ExperienceTraceDetail) string {
	return strings.TrimSpace(detail.Kind)
}

func experienceTraceSource(detail ExperienceTraceDetail) string {
	return strings.TrimSpace(detail.SourceType)
}

func experienceTraceReviewStatus(detail ExperienceTraceDetail) string {
	return strings.TrimSpace(detail.ReviewStatus)
}

func experienceTraceNextActionKind(detail ExperienceTraceDetail) string {
	return strings.TrimSpace(detail.NextActionKind)
}

func experienceTraceFollowUpStatus(detail ExperienceTraceDetail) string {
	return strings.TrimSpace(detail.FollowUpStatus)
}

func experienceTraceFollowUpActionKind(detail ExperienceTraceDetail) string {
	return strings.TrimSpace(detail.FollowUpActionKind)
}

func countReviewRequiredExperienceTraces(details []ExperienceTraceDetail) int {
	count := 0
	for _, detail := range details {
		if detail.ReviewRequired {
			count++
		}
	}
	return count
}

func countNextActionExperienceTraces(details []ExperienceTraceDetail) int {
	count := 0
	for _, detail := range details {
		if strings.TrimSpace(detail.NextAction) != "" || strings.TrimSpace(detail.NextActionKind) != "" {
			count++
		}
	}
	return count
}

func countFollowUpExperienceTraces(details []ExperienceTraceDetail) int {
	count := 0
	for _, detail := range details {
		if strings.TrimSpace(detail.FollowUpStatus) != "" {
			count++
		}
	}
	return count
}

func buildExperienceReviewSummaries(details []ExperienceTraceDetail) []ExperienceReviewSummary {
	byStatus := map[string]*ExperienceReviewSummary{}
	for _, detail := range details {
		status := experienceTraceReviewSummaryStatus(detail)
		if status == "" {
			continue
		}
		summary := byStatus[status]
		if summary == nil {
			summary = &ExperienceReviewSummary{Status: status}
			byStatus[status] = summary
		}
		summary.Count++
		if detail.ReviewRequired {
			summary.RequiredCount++
		}
		candidateUpdated := firstNonEmptyExperienceString(detail.ReviewedAt, detail.UpdatedAt)
		if newerExperienceTrace(candidateUpdated, summary.LatestUpdatedAt) {
			summary.LatestTraceID = detail.ID
			summary.LatestTitle = detail.Title
			summary.LatestKind = detail.Kind
			summary.LatestAction = firstNonEmptyExperienceString(detail.ReviewAction, detail.NextAction)
			summary.LatestReviewer = detail.Reviewer
			summary.LatestNote = detail.ReviewNote
			summary.LatestReviewedAt = detail.ReviewedAt
			summary.LatestUpdatedAt = candidateUpdated
		}
	}
	summaries := make([]ExperienceReviewSummary, 0, len(byStatus))
	for _, summary := range byStatus {
		summaries = append(summaries, *summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		if ri, rj := experienceReviewSummaryRank(summaries[i].Status), experienceReviewSummaryRank(summaries[j].Status); ri != rj {
			return ri < rj
		}
		if summaries[i].Count != summaries[j].Count {
			return summaries[i].Count > summaries[j].Count
		}
		if summaries[i].LatestUpdatedAt != summaries[j].LatestUpdatedAt {
			return summaries[i].LatestUpdatedAt > summaries[j].LatestUpdatedAt
		}
		return summaries[i].Status < summaries[j].Status
	})
	return summaries
}

func experienceTraceReviewSummaryStatus(detail ExperienceTraceDetail) string {
	if status := strings.TrimSpace(detail.ReviewStatus); status != "" {
		return status
	}
	if detail.ReviewRequired {
		return "required"
	}
	return ""
}

func experienceReviewSummaryRank(status string) int {
	switch strings.TrimSpace(status) {
	case "required":
		return 0
	case experienceReviewOutcomeDeferred:
		return 1
	case experienceReviewOutcomeApproved:
		return 2
	case experienceReviewOutcomeRejected:
		return 3
	default:
		return 9
	}
}

func buildExperienceNextActionSummaries(details []ExperienceTraceDetail) []ExperienceNextActionSummary {
	byKind := map[string]*ExperienceNextActionSummary{}
	for _, detail := range details {
		kind := strings.TrimSpace(detail.NextActionKind)
		if kind == "" && strings.TrimSpace(detail.NextAction) != "" {
			kind = "manual_follow_up"
		}
		if kind == "" {
			continue
		}
		summary := byKind[kind]
		if summary == nil {
			summary = &ExperienceNextActionSummary{Kind: kind}
			byKind[kind] = summary
		}
		summary.Count++
		if newerExperienceTrace(detail.UpdatedAt, summary.LatestUpdatedAt) {
			summary.LatestTraceID = detail.ID
			summary.LatestTitle = detail.Title
			summary.LatestAction = detail.NextAction
			summary.LatestUpdatedAt = detail.UpdatedAt
		}
	}
	summaries := make([]ExperienceNextActionSummary, 0, len(byKind))
	for _, summary := range byKind {
		summaries = append(summaries, *summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Count != summaries[j].Count {
			return summaries[i].Count > summaries[j].Count
		}
		if summaries[i].LatestUpdatedAt != summaries[j].LatestUpdatedAt {
			return summaries[i].LatestUpdatedAt > summaries[j].LatestUpdatedAt
		}
		return summaries[i].Kind < summaries[j].Kind
	})
	return summaries
}

func buildExperienceFollowUpSummaries(details []ExperienceTraceDetail) []ExperienceFollowUpSummary {
	byStatus := map[string]*ExperienceFollowUpSummary{}
	for _, detail := range details {
		status := strings.TrimSpace(detail.FollowUpStatus)
		if status == "" {
			continue
		}
		summary := byStatus[status]
		if summary == nil {
			summary = &ExperienceFollowUpSummary{Status: status}
			byStatus[status] = summary
		}
		summary.Count++
		if experienceTriggeredRollbackEvidence(detail) {
			summary.TriggeredRollback = true
			summary.TriggeredCount++
		}
		candidateUpdated := firstNonEmptyExperienceString(detail.FollowUpAt, detail.UpdatedAt)
		if newerExperienceTrace(candidateUpdated, summary.LatestUpdatedAt) {
			summary.LatestTraceID = detail.ID
			summary.LatestTitle = detail.Title
			summary.LatestActionKind = firstNonEmptyExperienceString(detail.FollowUpActionKind, detail.NextActionKind)
			summary.LatestNote = detail.FollowUpNote
			summary.LatestUpdatedAt = candidateUpdated
			if experienceTriggeredRollbackEvidence(detail) {
				summary.RecommendedTraceID = detail.ID
				summary.RecommendedTitle = detail.Title
				summary.RecommendedReason = "matched rollback-trigger evidence is present in this follow-up audit trail"
			}
		}
		if summary.RecommendedTraceID == "" && experienceTriggeredRollbackEvidence(detail) {
			summary.RecommendedTraceID = detail.ID
			summary.RecommendedTitle = detail.Title
			summary.RecommendedReason = "matched rollback-trigger evidence is present in this follow-up audit trail"
		}
	}
	summaries := make([]ExperienceFollowUpSummary, 0, len(byStatus))
	for _, summary := range byStatus {
		summaries = append(summaries, *summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Count != summaries[j].Count {
			return summaries[i].Count > summaries[j].Count
		}
		if summaries[i].LatestUpdatedAt != summaries[j].LatestUpdatedAt {
			return summaries[i].LatestUpdatedAt > summaries[j].LatestUpdatedAt
		}
		return summaries[i].Status < summaries[j].Status
	})
	return summaries
}

func buildExperienceFollowUpActionSummaries(details []ExperienceTraceDetail) []ExperienceFollowUpActionSummary {
	byKind := map[string]*ExperienceFollowUpActionSummary{}
	for _, detail := range details {
		kind := strings.TrimSpace(detail.FollowUpActionKind)
		if kind == "" {
			continue
		}
		summary := byKind[kind]
		if summary == nil {
			summary = &ExperienceFollowUpActionSummary{Kind: kind, StatusCounts: map[string]int{}}
			byKind[kind] = summary
		}
		summary.Count++
		if status := strings.TrimSpace(detail.FollowUpStatus); status != "" {
			summary.StatusCounts[status]++
		}
		if experienceTriggeredRollbackEvidence(detail) {
			summary.TriggeredRollback = true
			summary.TriggeredCount++
		}
		candidateUpdated := firstNonEmptyExperienceString(detail.FollowUpAt, detail.UpdatedAt)
		if newerExperienceTrace(candidateUpdated, summary.LatestUpdatedAt) {
			summary.LatestTraceID = detail.ID
			summary.LatestTitle = detail.Title
			summary.LatestStatus = detail.FollowUpStatus
			summary.LatestNote = detail.FollowUpNote
			summary.LatestUpdatedAt = candidateUpdated
			if experienceTriggeredRollbackEvidence(detail) {
				summary.RecommendedTraceID = detail.ID
				summary.RecommendedTitle = detail.Title
				summary.RecommendedReason = "matched rollback-trigger evidence is present in this follow-up action queue"
			}
		}
		if summary.RecommendedTraceID == "" && experienceTriggeredRollbackEvidence(detail) {
			summary.RecommendedTraceID = detail.ID
			summary.RecommendedTitle = detail.Title
			summary.RecommendedReason = "matched rollback-trigger evidence is present in this follow-up action queue"
		}
	}
	summaries := make([]ExperienceFollowUpActionSummary, 0, len(byKind))
	for _, summary := range byKind {
		summaries = append(summaries, *summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Count != summaries[j].Count {
			return summaries[i].Count > summaries[j].Count
		}
		if summaries[i].LatestUpdatedAt != summaries[j].LatestUpdatedAt {
			return summaries[i].LatestUpdatedAt > summaries[j].LatestUpdatedAt
		}
		return summaries[i].Kind < summaries[j].Kind
	})
	return summaries
}

func newerExperienceTrace(candidate, current string) bool {
	candidate = strings.TrimSpace(candidate)
	current = strings.TrimSpace(current)
	if current == "" {
		return true
	}
	if candidate == "" {
		return false
	}
	return candidate > current
}

func limitRoutingHints(values []coretool.ToolRoutingHint, limit int) []coretool.ToolRoutingHint {
	if limit > 0 && len(values) > limit {
		return append([]coretool.ToolRoutingHint(nil), values[:limit]...)
	}
	return append([]coretool.ToolRoutingHint(nil), values...)
}

func limitSkillNudges(values []coretool.ToolSkillNudgeCandidate, limit int) []coretool.ToolSkillNudgeCandidate {
	if limit > 0 && len(values) > limit {
		return append([]coretool.ToolSkillNudgeCandidate(nil), values[:limit]...)
	}
	return append([]coretool.ToolSkillNudgeCandidate(nil), values...)
}

func limitRecoveryPatterns(values []coretool.ToolRecoveryPattern, limit int) []coretool.ToolRecoveryPattern {
	if limit > 0 && len(values) > limit {
		return append([]coretool.ToolRecoveryPattern(nil), values[:limit]...)
	}
	return append([]coretool.ToolRecoveryPattern(nil), values...)
}

func limitUsagePatterns(values []coretool.UsagePattern, limit int) []coretool.UsagePattern {
	if limit > 0 && len(values) > limit {
		return append([]coretool.UsagePattern(nil), values[:limit]...)
	}
	return append([]coretool.UsagePattern(nil), values...)
}

func buildExperienceTraceDetails(snapshot ExperienceLearningSnapshot, mem *memory.Store, limit int) []ExperienceTraceDetail {
	return limitExperienceTraceDetails(collectExperienceTraceDetails(snapshot, mem), limit)
}

func collectExperienceTraceDetails(snapshot ExperienceLearningSnapshot, mem *memory.Store) []ExperienceTraceDetail {
	details := make([]ExperienceTraceDetail, 0)
	for _, hint := range snapshot.RoutingHints {
		details = append(details, traceDetailFromRoutingHint(hint))
	}
	for _, nudge := range snapshot.SkillNudgeCandidates {
		details = append(details, traceDetailFromSkillNudge(nudge))
	}
	for _, pattern := range snapshot.RecoveryPatterns {
		details = append(details, traceDetailFromRecoveryPattern(pattern))
	}
	for _, pattern := range snapshot.UsagePatterns {
		details = append(details, traceDetailFromUsagePattern(pattern))
	}
	if mem != nil {
		entries := mem.List("", "")
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].UpdatedAt.After(entries[j].UpdatedAt)
		})
		for _, entry := range entries {
			if detail, ok := traceDetailFromMemoryEntry(entry); ok {
				details = append(details, detail)
			}
		}
	}
	prioritizeExperienceTraceDetails(details)
	return details
}

func limitExperienceTraceDetails(details []ExperienceTraceDetail, limit int) []ExperienceTraceDetail {
	if limit > 0 && len(details) > limit {
		return append([]ExperienceTraceDetail(nil), details[:limit]...)
	}
	return append([]ExperienceTraceDetail(nil), details...)
}

func prioritizeExperienceTraceDetails(details []ExperienceTraceDetail) {
	sort.SliceStable(details, func(i, j int) bool {
		return experienceTracePriority(details[i]) < experienceTracePriority(details[j])
	})
}

func experienceTracePriority(detail ExperienceTraceDetail) int {
	switch detail.Kind {
	case "a2a_conflict_review", "a2a_rollback_review":
		return 0
	case "skill_nudge_review", "skill_nudge_candidate":
		return 1
	default:
		if detail.ReviewRequired {
			return 2
		}
		if strings.TrimSpace(detail.NextActionKind) != "" || strings.TrimSpace(detail.NextAction) != "" {
			return 2
		}
		if strings.TrimSpace(detail.FollowUpStatus) != "" {
			return 2
		}
		return 3
	}
}

func traceDetailFromRoutingHint(hint coretool.ToolRoutingHint) ExperienceTraceDetail {
	id := "routing:" + firstNonEmptyExperienceString(hint.ContextKey, strings.Join(hint.QueryTokens, ":"), strings.Join(hint.PreferTools, ":"))
	summary := strings.TrimSpace(hint.Description)
	if summary == "" {
		summary = fmt.Sprintf("Prefer %s and avoid %s for %s.", strings.Join(hint.PreferTools, ", "), strings.Join(hint.AvoidTools, ", "), firstNonEmptyExperienceString(hint.ContextKey, "this context"))
	}
	return ExperienceTraceDetail{
		ID:         id,
		Kind:       "routing_hint",
		Title:      firstNonEmptyExperienceString(hint.ContextKey, "Routing hint"),
		Summary:    summary,
		Detail:     joinExperienceDetailLines("Task type: "+hint.TaskType, "Tokens: "+strings.Join(hint.QueryTokens, ", "), "Prefer tools: "+strings.Join(hint.PreferTools, ", "), "Avoid tools: "+strings.Join(hint.AvoidTools, ", "), "Recovery tools: "+strings.Join(hint.RecoveryTools, ", ")),
		SourceType: "tool_usage",
		Evidence:   hint.Evidence,
		Confidence: hint.Confidence,
		Impact:     "May apply only a bounded routing-score adjustment; it never authorizes tool execution by itself.",
	}
}

func traceDetailFromSkillNudge(nudge coretool.ToolSkillNudgeCandidate) ExperienceTraceDetail {
	id := "skill_nudge:" + firstNonEmptyExperienceString(nudge.ContextKey, nudge.SuggestedName, strings.Join(nudge.ToolSequence, ":"))
	return ExperienceTraceDetail{
		ID:             id,
		Kind:           "skill_nudge_candidate",
		Title:          firstNonEmptyExperienceString(nudge.SuggestedName, nudge.ContextKey, "Skill candidate"),
		Summary:        strings.TrimSpace(nudge.Description),
		Detail:         joinExperienceDetailLines("Task type: "+nudge.TaskType, "Tokens: "+strings.Join(nudge.QueryTokens, ", "), "Sequence: "+strings.Join(nudge.ToolSequence, " -> ")),
		SourceType:     "tool_usage",
		Evidence:       nudge.Evidence,
		Confidence:     nudge.Confidence,
		Impact:         "Review candidate only; no skill is created, installed, or run automatically.",
		ReviewRequired: true,
		ReviewAction:   "Inspect the repeated tool sequence and create or update a skill only after confirming it is safe and reusable.",
	}
}

func traceDetailFromRecoveryPattern(pattern coretool.ToolRecoveryPattern) ExperienceTraceDetail {
	id := "recovery:" + firstNonEmptyExperienceString(pattern.ContextKey, pattern.FailedTool+":"+pattern.RecoveryTool)
	return ExperienceTraceDetail{
		ID:         id,
		Kind:       "tool_recovery_pattern",
		Title:      firstNonEmptyExperienceString(pattern.FailedTool, "Tool") + " -> " + firstNonEmptyExperienceString(pattern.RecoveryTool, "Recovery"),
		Summary:    strings.TrimSpace(pattern.Description),
		Detail:     joinExperienceDetailLines("Context: "+pattern.ContextKey, "Task type: "+pattern.TaskType, "Error class: "+pattern.ErrorClass, "Tokens: "+strings.Join(pattern.QueryTokens, ", "), "Sequence: "+strings.Join(pattern.ToolSequence, " -> ")),
		SourceType: "tool_usage",
		Evidence:   pattern.Evidence,
		Confidence: pattern.Confidence,
		Impact:     "Explains a repeated recovery flow for project memory; normal routing and safety checks still apply.",
	}
}

func traceDetailFromUsagePattern(pattern coretool.UsagePattern) ExperienceTraceDetail {
	return ExperienceTraceDetail{
		ID:         "usage:" + firstNonEmptyExperienceString(pattern.ToolName, strings.Join(pattern.TopTokens, ":")),
		Kind:       "usage_pattern",
		Title:      firstNonEmptyExperienceString(pattern.ToolName, "Usage pattern"),
		Summary:    strings.TrimSpace(pattern.Description),
		Detail:     joinExperienceDetailLines("Top tokens: " + strings.Join(pattern.TopTokens, ", ")),
		SourceType: "tool_usage",
		Evidence:   pattern.Count,
		Confidence: pattern.SuccessRate,
		Impact:     "Read-only usage observation; it is weaker than a routing hint or recovery pattern.",
	}
}

func buildExperienceSessionTraceDetails(summaries []session.SessionSummary) []ExperienceTraceDetail {
	if len(summaries) == 0 {
		return nil
	}
	if len(summaries) > experienceSnapshotSessionTraceLimit {
		summaries = summaries[:experienceSnapshotSessionTraceLimit]
	}
	details := make([]ExperienceTraceDetail, 0, len(summaries))
	for _, summary := range summaries {
		if detail, ok := traceDetailFromSessionSummary(summary); ok {
			details = append(details, detail)
		}
	}
	return details
}

func traceDetailFromSessionSummary(summary session.SessionSummary) (ExperienceTraceDetail, bool) {
	sessionID := strings.TrimSpace(summary.SessionID)
	if sessionID == "" {
		return ExperienceTraceDetail{}, false
	}
	tags := []string{"session:" + sessionID}
	if platform := strings.TrimSpace(summary.Platform); platform != "" {
		tags = append(tags, "platform:"+platform)
	}
	return ExperienceTraceDetail{
		ID:         "session:" + sessionID,
		Kind:       "session_history",
		Title:      firstNonEmptyExperienceString(summary.Topic, "Session "+sessionID),
		Summary:    "Searchable session transcript available as source material for future distillation.",
		Detail:     "Session history item: " + firstNonEmptyExperienceString(summary.Topic, sessionID),
		SourceType: "session_history",
		SourceURL:  "session://" + sessionID,
		Tags:       tags,
		Evidence:   summary.TextLen,
		Impact:     "Trace source only; it does not alter memory, routing, or skills by itself.",
		UpdatedAt:  summary.Timestamp,
	}, true
}

func traceDetailFromMemoryEntry(entry memory.Entry) (ExperienceTraceDetail, bool) {
	kind := ""
	impact := ""
	reviewRequired := false
	reviewResolved := experienceTraceReviewResolved(entry.Tags)
	switch {
	case hasTag(entry.Tags, groupDiscussionConflictTag):
		kind = "a2a_conflict_review"
		impact = "Requires review before either conflicting A2A result becomes durable project policy."
		reviewRequired = !reviewResolved
		if reviewResolved {
			impact = "Reviewed A2A conflict signal; it remains source evidence and no automatic project policy change was made."
		}
	case hasTag(entry.Tags, groupDiscussionRollbackTag):
		kind = "a2a_rollback_review"
		impact = "Rollback triggers were captured from an A2A decision; review them before treating rollback execution as authorized."
		if hasTag(entry.Tags, groupDiscussionRollbackTriggered) {
			impact = "Current A2A evidence matches one or more rollback triggers; review them before any workflow draft treats rollback as manually actionable."
		}
		reviewRequired = !reviewResolved
		if reviewResolved {
			impact = "Reviewed A2A rollback signal; it remains source evidence and no rollback execution was authorized automatically."
		}
	case hasTag(entry.Tags, "skill_nudge_candidate"):
		kind = "skill_nudge_review"
		impact = "Repeated tool sequence suggests a reusable skill candidate; review it before creating or updating any skill."
		reviewRequired = !reviewResolved
		if reviewResolved {
			impact = "Reviewed tool self-evolution candidate; it remains source evidence and no skill was created automatically."
		}
	case hasTag(entry.Tags, "has_escalation"):
		kind = "a2a_escalation_evidence"
		impact = "A2A escalation evidence was captured for manual handoff; it does not route or resolve the escalation automatically."
	case hasTag(entry.Tags, experienceDraftReviewTag):
		kind = experienceDraftReviewTraceKind(entry.Tags)
		impact = "Manual review evidence for a non-executing experience-learning draft; it does not rewrite memory, change routing, write files, install skills, or run tools."
	case entry.SourceType == groupDiscussionMemorySourceType || hasTag(entry.Tags, groupDiscussionResultTag):
		kind = "a2a_discussion_result"
		impact = "Project memory distilled from a completed current-Hub A2A discussion."
	case entry.SourceType == "tool_usage" || hasExperienceAnyTag(entry.Tags, "usage_pattern", "usage_routing_hint", "skill_nudge_candidate", "tool_recovery_pattern"):
		kind = "tool_memory"
		impact = "Memory-backed tool learning signal; used as context, not as automatic permission."
	default:
		return ExperienceTraceDetail{}, false
	}
	reviewAudit := experienceReviewAuditFromContent(entry.Content)
	reviewStatus := experienceReviewStatusFromTags(entry.Tags, reviewRequired)
	nextActionKind, nextAction := experienceTraceNextAction(kind, reviewStatus, reviewRequired)
	followUpAudit := experienceFollowUpAuditFromContent(entry.Content)
	followUpStatus := experienceFollowUpStatusFromTags(entry.Tags)
	if kind == "a2a_rollback_review" && hasTag(entry.Tags, groupDiscussionRollbackTriggered) && reviewStatus == "required" {
		nextActionKind = "review_triggered_rollback_signal"
		nextAction = "Current A2A evidence already matches rollback conditions; review the matched triggers first, then open a non-executing rollback workflow draft if approved."
	}
	if experienceFollowUpResolved(entry.Tags) {
		nextActionKind = ""
		nextAction = ""
	}
	return ExperienceTraceDetail{
		ID:                 "memory:" + firstNonEmptyExperienceString(entry.ID, shortGroupDiscussionHash(entry.Content)),
		Kind:               kind,
		Title:              firstNonEmptyExperienceString(entry.Title, memoryTraceTitle(entry), "Experience memory"),
		Summary:            firstLineExperienceText(entry.Content, 220),
		Detail:             strings.TrimSpace(entry.Content),
		SourceType:         entry.SourceType,
		SourceURL:          entry.SourceURL,
		SourceTraceID:      experienceDraftReviewSourceTraceID(entry.Content),
		Tags:               append([]string(nil), entry.Tags...),
		Impact:             impact,
		ReviewRequired:     reviewRequired,
		ReviewAction:       reviewActionForExperienceTrace(kind, reviewRequired),
		ReviewStatus:       reviewStatus,
		NextActionKind:     nextActionKind,
		NextAction:         nextAction,
		ReviewedAt:         firstNonEmptyExperienceString(reviewAudit.ReviewedAt, experienceTraceReviewedAt(entry.Tags)),
		Reviewer:           reviewAudit.Reviewer,
		ReviewNote:         reviewAudit.Note,
		ReviewCount:        reviewAudit.Count,
		FollowUpStatus:     followUpStatus,
		FollowUpActionKind: followUpAudit.ActionKind,
		FollowUpAt:         firstNonEmptyExperienceString(followUpAudit.At, experienceFollowUpAt(entry.Tags)),
		FollowUpActor:      followUpAudit.Actor,
		FollowUpNote:       followUpAudit.Note,
		FollowUpCount:      followUpAudit.Count,
		UpdatedAt:          formatExperienceTime(entry.UpdatedAt),
	}, true
}

func experienceDraftReviewTraceKind(tags []string) string {
	switch {
	case hasTag(tags, experienceDraftKindMaintenance):
		return "memory_maintenance_draft_review"
	case hasTag(tags, experienceDraftKindRouting):
		return "routing_adjustment_draft_review"
	case hasTag(tags, experienceDraftKindSkill):
		return "skill_draft_review"
	case hasTag(tags, experienceDraftKindRollback):
		return "rollback_workflow_draft_review"
	case hasTag(tags, experienceDraftKindEscalation):
		return "escalation_brief_review"
	case hasTag(tags, experienceDraftKindConflict):
		return "conflict_reconciliation_draft_review"
	default:
		return "experience_draft_review"
	}
}

func experienceDraftReviewSourceTraceID(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if strings.HasPrefix(trimmed, "- Source trace:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "- Source trace:"))
		}
	}
	return ""
}

func reviewActionForExperienceTrace(kind string, reviewRequired bool) string {
	if !reviewRequired {
		return ""
	}
	switch kind {
	case "a2a_conflict_review":
		return "Compare the conflicting A2A memories and decide which result, if any, should remain durable project policy."
	case "a2a_rollback_review":
		return "Validate each rollback trigger and convert it into a human-approved rollback workflow before any execution path uses it."
	case "skill_nudge_review":
		return "Inspect the repeated tool sequence and create or update a skill only after confirming it is safe and reusable."
	default:
		return ""
	}
}

func experienceTraceNextAction(kind, status string, reviewRequired bool) (string, string) {
	if status == "" && reviewRequired {
		status = "required"
	}
	if status == "required" {
		return "review_signal", reviewActionForExperienceTrace(kind, true)
	}
	switch kind {
	case "a2a_conflict_review":
		switch status {
		case experienceReviewOutcomeApproved:
			return "resolve_a2a_conflict_manually", "Use this approved conflict signal to open a manual reconciliation task before changing durable project policy."
		case experienceReviewOutcomeRejected:
			return "keep_rejected_conflict_evidence", "Keep the rejected conflict as audit evidence and avoid promoting either conflicting result from this signal."
		case experienceReviewOutcomeDeferred:
			return "collect_a2a_conflict_evidence", "Collect missing owner evidence, then review the conflict again before changing project memory or policy."
		}
	case "a2a_rollback_review":
		switch status {
		case experienceReviewOutcomeApproved:
			return "draft_rollback_workflow", "Draft a human-approved rollback workflow from these triggers; execution remains manual until separately authorized."
		case experienceReviewOutcomeRejected:
			return "block_rollback_use", "Treat these rollback triggers as rejected evidence and do not use them for rollback execution."
		case experienceReviewOutcomeDeferred:
			return "collect_rollback_evidence", "Collect validation evidence for each rollback trigger, then review again before any execution path can use it."
		}
	case "skill_nudge_review":
		switch status {
		case experienceReviewOutcomeApproved:
			return "draft_skill_manually", "Open a manual skill draft from the approved repeated tool sequence; do not install, update, or run it automatically."
		case experienceReviewOutcomeRejected:
			return "suppress_skill_candidate", "Keep the rejected skill candidate as evidence and avoid using it to create or update a skill."
		case experienceReviewOutcomeDeferred:
			return "collect_skill_evidence", "Collect more successful executions before deciding whether the tool sequence deserves a manual skill draft."
		}
	case "a2a_escalation_evidence":
		return "prepare_escalation_brief", "Prepare a manual escalation handoff brief from the captured reason, target, raiser, and discussion evidence."
	}
	return "", ""
}

func experienceTraceReviewResolved(tags []string) bool {
	if hasTag(tags, experienceReviewResolvedTag) {
		return true
	}
	for _, tag := range tags {
		if !strings.HasPrefix(tag, experienceReviewStatusTagPrefix) {
			continue
		}
		switch strings.TrimSpace(strings.TrimPrefix(tag, experienceReviewStatusTagPrefix)) {
		case "approved", "rejected":
			return true
		}
	}
	return false
}

func experienceReviewStatusFromTags(tags []string, required bool) string {
	for _, tag := range tags {
		if !strings.HasPrefix(tag, experienceReviewStatusTagPrefix) {
			continue
		}
		status := strings.TrimSpace(strings.TrimPrefix(tag, experienceReviewStatusTagPrefix))
		switch status {
		case experienceReviewOutcomeApproved, experienceReviewOutcomeRejected, experienceReviewOutcomeDeferred:
			return status
		}
	}
	if required {
		return "required"
	}
	return ""
}

func experienceTraceReviewedAt(tags []string) string {
	for _, tag := range tags {
		if !strings.HasPrefix(tag, "reviewed_at:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(tag, "reviewed_at:"))
		if len(raw) == 8 {
			return raw[:4] + "-" + raw[4:6] + "-" + raw[6:]
		}
		return raw
	}
	return ""
}

func experienceFollowUpStatusFromTags(tags []string) string {
	for _, tag := range tags {
		if !strings.HasPrefix(tag, experienceFollowUpStatusTagPrefix) {
			continue
		}
		status := strings.TrimSpace(strings.TrimPrefix(tag, experienceFollowUpStatusTagPrefix))
		switch status {
		case experienceFollowUpOutcomeCompleted, experienceFollowUpOutcomeBlocked, experienceFollowUpOutcomeDeferred:
			return status
		}
	}
	return ""
}

func experienceFollowUpResolved(tags []string) bool {
	return hasTag(tags, experienceFollowUpResolvedTag)
}

func experienceFollowUpAt(tags []string) string {
	for _, tag := range tags {
		if !strings.HasPrefix(tag, "followup_at:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(tag, "followup_at:"))
		if len(raw) == 8 {
			return raw[:4] + "-" + raw[4:6] + "-" + raw[6:]
		}
		return raw
	}
	return ""
}

type experienceReviewAudit struct {
	Reviewer   string
	Note       string
	ReviewedAt string
	Count      int
}

type experienceFollowUpAudit struct {
	ActionKind string
	Actor      string
	Note       string
	At         string
	Count      int
}

func experienceReviewAuditFromContent(content string) experienceReviewAudit {
	const marker = "Experience review record:"
	audit := experienceReviewAudit{Count: strings.Count(content, marker)}
	if audit.Count == 0 {
		return audit
	}
	idx := strings.LastIndex(content, marker)
	if idx < 0 {
		return audit
	}
	lines := strings.Split(content[idx+len(marker):], "\n")
	noteLines := make([]string, 0, 2)
	capturingNote := false
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			capturingNote = false
			key, value, ok := strings.Cut(strings.TrimPrefix(trimmed, "- "), ":")
			if !ok {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "reviewer":
				audit.Reviewer = strings.TrimSpace(value)
			case "reviewed at":
				audit.ReviewedAt = formatExperienceReviewAuditTime(value)
			case "note":
				noteLines = append(noteLines[:0], strings.TrimSpace(value))
				capturingNote = true
			}
			continue
		}
		if capturingNote {
			noteLines = append(noteLines, strings.TrimSpace(line))
		}
	}
	audit.Note = strings.TrimSpace(strings.Join(noteLines, "\n"))
	return audit
}

func experienceFollowUpAuditFromContent(content string) experienceFollowUpAudit {
	const marker = "Experience follow-up record:"
	audit := experienceFollowUpAudit{Count: strings.Count(content, marker)}
	if audit.Count == 0 {
		return audit
	}
	idx := strings.LastIndex(content, marker)
	if idx < 0 {
		return audit
	}
	lines := strings.Split(content[idx+len(marker):], "\n")
	noteLines := make([]string, 0, 2)
	capturingNote := false
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			capturingNote = false
			key, value, ok := strings.Cut(strings.TrimPrefix(trimmed, "- "), ":")
			if !ok {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "action kind":
				audit.ActionKind = strings.TrimSpace(value)
			case "actor":
				audit.Actor = strings.TrimSpace(value)
			case "recorded at":
				audit.At = formatExperienceReviewAuditTime(value)
			case "note":
				noteLines = append(noteLines[:0], strings.TrimSpace(value))
				capturingNote = true
			}
			continue
		}
		if capturingNote {
			noteLines = append(noteLines, strings.TrimSpace(line))
		}
	}
	audit.Note = strings.TrimSpace(strings.Join(noteLines, "\n"))
	return audit
}

func formatExperienceReviewAuditTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC().Format(time.RFC3339)
	}
	if len(value) == 8 {
		return value[:4] + "-" + value[4:6] + "-" + value[6:]
	}
	return value
}

func hasExperienceAnyTag(tags []string, targets ...string) bool {
	for _, target := range targets {
		if hasTag(tags, target) {
			return true
		}
	}
	return false
}

func memoryTraceTitle(entry memory.Entry) string {
	for _, tag := range entry.Tags {
		if strings.HasPrefix(tag, "discussion:") {
			return "A2A discussion " + strings.TrimPrefix(tag, "discussion:")
		}
		if tag == groupDiscussionConflictTag {
			return "A2A conflict review"
		}
		if tag == groupDiscussionRollbackTag {
			return "A2A rollback review"
		}
		if tag == "tool_recovery_pattern" {
			return "Tool recovery memory"
		}
		if tag == "usage_routing_hint" {
			return "Routing hint memory"
		}
		if tag == "skill_nudge_candidate" {
			return "Skill candidate memory"
		}
	}
	return ""
}

func firstLineExperienceText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if idx := strings.IndexByte(value, '\n'); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	return truncateExperienceText(value, limit)
}

func truncateExperienceText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}

func joinExperienceDetailLines(values ...string) string {
	lines := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || strings.HasSuffix(value, ":") {
			continue
		}
		lines = append(lines, value)
	}
	return strings.Join(lines, "\n")
}

func firstNonEmptyExperienceString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func formatExperienceTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
