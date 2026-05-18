package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

const (
	experienceReviewOutcomeApproved = "approved"
	experienceReviewOutcomeRejected = "rejected"
	experienceReviewOutcomeDeferred = "deferred"
)

// ExperienceTraceReviewRequest records a human decision for a memory-backed
// learning trace. It never executes rollback or changes routing policy.
type ExperienceTraceReviewRequest struct {
	Outcome  string `json:"outcome"`
	Note     string `json:"note,omitempty"`
	Reviewer string `json:"reviewer,omitempty"`
}

type ExperienceTraceReviewRecord struct {
	TraceID                 string                 `json:"trace_id"`
	MemoryID                string                 `json:"memory_id"`
	Kind                    string                 `json:"kind"`
	Outcome                 string                 `json:"outcome"`
	RecommendedFocusContext map[string]interface{} `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall     map[string]interface{} `json:"recommended_tool_call,omitempty"`
	NonExecutingBoundary    string                 `json:"non_executing_boundary,omitempty"`
}

// ReviewExperienceTrace marks a memory-backed learning review signal as
// approved, rejected, or deferred. The operation is deliberately limited to
// tags and a review note so trace review stays auditable and non-executing.
func (a *App) ReviewExperienceTrace(traceID string, req ExperienceTraceReviewRequest) (ExperienceTraceReviewRecord, error) {
	outcome, err := normalizeExperienceReviewOutcome(req.Outcome)
	if err != nil {
		return ExperienceTraceReviewRecord{}, err
	}
	if a.memoryStore == nil {
		a.ensureInteractionInfra()
		a.ensureMemoryStore()
	}
	if a.memoryStore == nil {
		return ExperienceTraceReviewRecord{}, fmt.Errorf("memory store not initialized")
	}

	entry, reviewKind, err := findExperienceReviewMemoryEntry(a.memoryStore, traceID)
	if err != nil {
		return ExperienceTraceReviewRecord{}, err
	}

	now := time.Now().UTC()
	content := appendExperienceReviewRecord(entry.Content, outcome, reviewKind, req.Note, a.defaultExperienceReviewReviewer(req.Reviewer), now)
	tags := applyExperienceReviewTags(entry.Tags, reviewKind, outcome, now)
	if err := a.memoryStore.Update(entry.ID, content, entry.Category, tags); err != nil {
		return ExperienceTraceReviewRecord{}, err
	}
	a.emitEvent("memory:experience-reviewed", map[string]string{
		"trace_id":  traceID,
		"memory_id": entry.ID,
		"outcome":   outcome,
	})
	return ExperienceTraceReviewRecord{
		TraceID:                 traceID,
		MemoryID:                entry.ID,
		Kind:                    reviewKind,
		Outcome:                 outcome,
		RecommendedFocusContext: experienceFocusContextFromTraceTarget(traceID, entry.Title, "manual review outcome recorded for priority experience trace"),
		RecommendedToolCall:     experienceTraceInspectionRecommendedToolCall(traceID, entry.Title, "manual review outcome recorded for priority experience trace"),
		NonExecutingBoundary:    "manual review audit record only; no rollback ran, no skill was created or installed, no routing changed, no memory was rewritten beyond review audit evidence, no files were written, no tools were run, and no notifications were sent",
	}, nil
}

func (a *App) defaultExperienceReviewReviewer(value string) string {
	if reviewer := strings.TrimSpace(value); reviewer != "" {
		return reviewer
	}
	if a != nil {
		a.configMu.Lock()
		cfg := a.configCache
		ok := a.configCacheValid
		a.configMu.Unlock()
		if ok {
			if reviewer := firstNonEmptyGroupString(cfg.RemoteMachineID, cfg.RemoteClientID, cfg.RemoteEmail); reviewer != "" {
				return reviewer
			}
		}
	}
	return "local"
}

func normalizeExperienceReviewOutcome(value string) (string, error) {
	outcome := normalizeExperienceReviewOutcomeKind(value)
	if !outcome.IsKnown() {
		return "", fmt.Errorf("unknown review outcome %q", value)
	}
	return outcome.String(), nil
}

func findExperienceReviewMemoryEntry(store *memory.Store, traceID string) (memory.Entry, string, error) {
	if store == nil {
		return memory.Entry{}, "", fmt.Errorf("memory store not initialized")
	}
	traceID = strings.TrimSpace(traceID)
	if !strings.HasPrefix(traceID, "memory:") {
		return memory.Entry{}, "", fmt.Errorf("only memory-backed experience traces can be reviewed")
	}
	memoryID := strings.TrimSpace(strings.TrimPrefix(traceID, "memory:"))
	if memoryID == "" {
		return memory.Entry{}, "", fmt.Errorf("memory trace id is empty")
	}
	for _, entry := range store.List("", "") {
		if entry.ID != memoryID {
			continue
		}
		switch {
		case hasTag(entry.Tags, groupDiscussionConflictTag):
			return entry, experienceReviewKindConflict.String(), nil
		case hasTag(entry.Tags, groupDiscussionRollbackTag):
			return entry, experienceReviewKindRollback.String(), nil
		case hasTag(entry.Tags, experienceTraceKindSkillNudgeCandidate.String()):
			return entry, experienceReviewKindSkillNudge.String(), nil
		case hasTag(entry.Tags, experienceTraceKindToolRecoveryPattern.String()):
			return entry, experienceReviewKindToolRecovery.String(), nil
		default:
			return memory.Entry{}, "", fmt.Errorf("experience trace %q is not a reviewable learning signal", traceID)
		}
	}
	return memory.Entry{}, "", fmt.Errorf("experience trace %q not found", traceID)
}

func appendExperienceReviewRecord(content, outcome, reviewKind, note, reviewer string, now time.Time) string {
	content = strings.TrimSpace(content)
	note = truncateExperienceText(strings.TrimSpace(note), 800)
	reviewer = truncateExperienceText(strings.TrimSpace(reviewer), 160)
	if reviewer == "" {
		reviewer = "local"
	}
	var b strings.Builder
	if content != "" {
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	b.WriteString("Experience review record:")
	b.WriteString("\n- Kind: ")
	b.WriteString(reviewKind)
	b.WriteString("\n- Outcome: ")
	b.WriteString(outcome)
	b.WriteString("\n- Reviewer: ")
	b.WriteString(reviewer)
	b.WriteString("\n- Reviewed at: ")
	b.WriteString(now.Format(time.RFC3339))
	if note != "" {
		b.WriteString("\n- Note: ")
		b.WriteString(note)
	}
	b.WriteString("\n- Safety: recorded only; no skill, rollback, routing, or policy change was executed automatically.")
	return strings.TrimSpace(b.String())
}

func applyExperienceReviewTags(tags []string, reviewKind, outcome string, now time.Time) []string {
	result := withoutExperienceFollowUpStateTags(withoutExperienceReviewStateTags(tags))
	kind := normalizeExperienceReviewKind(reviewKind)

	result = append(result, experienceReviewStatusTagPrefix+outcome)
	switch outcome {
	case experienceReviewOutcomeDeferred:
		result = append(result, experienceReviewRequiredTag, experienceReviewLifecycleTagDeferred.String())
	default:
		result = append(result, experienceReviewResolvedTag, experienceReviewedAtTagPrefix+now.Format("20060102"))
		if kind == experienceReviewKindRollback {
			if outcome == experienceReviewOutcomeRejected {
				result = append(result, experienceReviewLifecycleTagRollbackRejected.String())
			} else {
				result = append(result, experienceReviewLifecycleTagRollbackReviewed.String())
			}
		}
		if kind == experienceReviewKindConflict {
			if outcome == experienceReviewOutcomeRejected {
				result = append(result, experienceReviewLifecycleTagConflictRejected.String())
			} else {
				result = append(result, experienceReviewLifecycleTagConflictReviewed.String())
			}
		}
		if kind == experienceReviewKindSkillNudge {
			if outcome == experienceReviewOutcomeRejected {
				result = append(result, experienceReviewLifecycleTagSkillNudgeRejected.String())
			} else {
				result = append(result, experienceReviewLifecycleTagSkillNudgeReviewed.String())
			}
		}
		if kind == experienceReviewKindToolRecovery {
			result = append(result, adaptiveRetryReviewedFailureCountPrefix+fmt.Sprintf("%d", adaptiveRetryCurrentFailureCount(tags)))
			if outcome == experienceReviewOutcomeRejected {
				result = append(result, experienceReviewLifecycleTagToolRecoveryRejected.String())
			} else {
				result = append(result, experienceReviewLifecycleTagToolRecoveryReviewed.String())
			}
		}
	}
	return normalizeUsageMemoryTags(result)
}

func resetExperienceReviewTagsForChangedContent(existing, incoming []string) []string {
	merged := mergeTags(existing, incoming)
	if !experienceReviewableTags(merged) {
		return normalizeUsageMemoryTags(merged)
	}
	tags := withoutExperienceFollowUpStateTags(withoutExperienceReviewStateTags(merged))
	tags = append(tags, experienceReviewRequiredTag)
	return normalizeUsageMemoryTags(tags)
}

func experienceReviewableTags(tags []string) bool {
	return hasTag(tags, groupDiscussionConflictTag) || hasTag(tags, groupDiscussionRollbackTag) || hasTag(tags, experienceTraceKindSkillNudgeCandidate.String()) || hasTag(tags, experienceTraceKindToolRecoveryPattern.String())
}

func adaptiveRetryCurrentFailureCount(tags []string) int {
	for _, tag := range tags {
		if !strings.HasPrefix(tag, "failure_count:") {
			continue
		}
		var count int
		if _, err := fmt.Sscanf(strings.TrimPrefix(tag, "failure_count:"), "%d", &count); err == nil && count > 0 {
			return count
		}
	}
	return 0
}

func withoutExperienceReviewStateTags(tags []string) []string {
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || tag == experienceReviewRequiredTag || tag == experienceReviewResolvedTag || normalizeExperienceReviewLifecycleTagKind(tag).IsStateTag() {
			continue
		}
		if strings.HasPrefix(tag, experienceReviewStatusTagPrefix) || strings.HasPrefix(tag, experienceReviewedAtTagPrefix) {
			continue
		}
		result = append(result, tag)
	}
	return result
}
