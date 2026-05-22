package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func (h *IMMessageHandler) toolExperienceLearning(args map[string]interface{}) string {
	if h == nil || h.app == nil {
		return experienceLearningToolResult(nil, fmt.Errorf("experience learning is only available in the desktop app"))
	}
	switch normalizeExperienceLearningToolAction(stringVal(args, "action")) {
	case experienceLearningToolActionSnapshot:
		snapshot := h.app.GetExperienceLearningSnapshot()
		focusContext := experienceSnapshotFocusContext(snapshot)
		return experienceLearningToolResult(map[string]interface{}{
			"snapshot":                  snapshot,
			"recommended_focus_context": focusContext,
			"recommended_tool_call":     experienceSnapshotRecommendedToolCall(focusContext),
			"non_executing_boundary":    "read-only experience learning snapshot; no review approval, memory rewrite, routing change, rollback execution, file write, notification, tool execution, or skill install was performed",
		}, nil)
	case experienceLearningToolActionGovernanceSummary:
		summary := h.app.GetExperienceGovernanceSummary(ExperienceRoutingSignalQuery{
			TaskType: strings.TrimSpace(stringVal(args, "task_type")),
			Tool:     strings.TrimSpace(stringVal(args, "tool")),
			Query:    firstNonEmptyExperienceString(stringVal(args, "query"), stringVal(args, "q")),
			Limit:    intArg(args, "limit", 8),
		})
		return experienceLearningToolResult(map[string]interface{}{
			"governance_summary":             summary,
			"recommended_focus_context":      summary["recommended_focus_context"],
			"recommended_tool_call":          summary["recommended_tool_call"],
			"non_executing_boundary":         summary["non_executing_boundary"],
			"recommended_next_action":        summary["recommended_next_action"],
			"recommended_next_action_reason": summary["recommended_next_action_reason"],
		}, nil)
	case experienceLearningToolActionNextActions:
		snapshot := h.app.GetExperienceLearningSnapshot()
		recommendedAction, recommendedReason := experienceGovernanceRecommendedNextAction(snapshot, nil)
		return experienceLearningToolResult(map[string]interface{}{
			"recommended_next_action":        recommendedAction,
			"recommended_next_action_reason": recommendedReason,
			"recommended_focus":              experienceGovernanceRecommendedFocus(recommendedAction),
			"recommended_focus_context":      experienceGovernanceRecommendedToolCallFocusContext(recommendedAction, snapshot, recommendedReason),
			"recommended_tool_call":          experienceGovernanceRecommendedToolCall(recommendedAction, snapshot, nil, recommendedReason),
			"review_required_trace_count":    snapshot.ReviewRequiredTraceCount,
			"review_status_counts":           snapshot.ReviewStatusCounts,
			"review_summaries":               snapshot.ReviewSummaries,
			"next_action_trace_count":        snapshot.NextActionTraceCount,
			"next_action_counts":             snapshot.NextActionKindCounts,
			"next_action_summaries":          snapshot.NextActionSummaries,
			"follow_up_trace_count":          snapshot.FollowUpTraceCount,
			"follow_up_status_counts":        snapshot.FollowUpStatusCounts,
			"follow_up_action_counts":        snapshot.FollowUpActionKindCounts,
			"follow_up_summaries":            snapshot.FollowUpSummaries,
			"follow_up_action_summaries":     snapshot.FollowUpActionSummaries,
			"non_executing_boundary":         "read-only next-action governance inspection; no review approval, draft execution, memory rewrite, routing change, file write, tool execution, notification, rollback execution, or skill install was performed",
		}, nil)
	case experienceLearningToolActionQueues:
		limit := intArg(args, "limit", 20)
		snapshot := h.app.GetExperienceLearningSnapshot()
		recommendedAction, recommendedReason := experienceGovernanceRecommendedNextAction(snapshot, nil)
		reviewQueue := h.app.QueryExperienceTraceDetails(ExperienceTraceDetailQuery{Filter: experienceTraceQueryFilterReview.String(), Limit: limit})
		actionQueue := h.app.QueryExperienceTraceDetails(ExperienceTraceDetailQuery{Filter: experienceTraceQueryFilterManualActions.String(), Limit: limit})
		followUpQueue := h.app.QueryExperienceTraceDetails(ExperienceTraceDetailQuery{Filter: experienceTraceQueryFilterFollowUps.String(), Limit: limit})
		return experienceLearningToolResult(map[string]interface{}{
			"recommended_next_action":        recommendedAction,
			"recommended_next_action_reason": recommendedReason,
			"recommended_focus":              experienceGovernanceRecommendedFocus(recommendedAction),
			"recommended_focus_context":      experienceGovernanceRecommendedToolCallFocusContext(recommendedAction, snapshot, recommendedReason),
			"recommended_tool_call":          experienceGovernanceRecommendedToolCall(recommendedAction, snapshot, nil, recommendedReason),
			"limit":                          reviewQueue.Query.Limit,
			"review_required_trace_count":    snapshot.ReviewRequiredTraceCount,
			"review_status_counts":           snapshot.ReviewStatusCounts,
			"review_summaries":               snapshot.ReviewSummaries,
			"review_queue_count":             reviewQueue.Count,
			"review_queue":                   reviewQueue.Details,
			"next_action_trace_count":        snapshot.NextActionTraceCount,
			"next_action_counts":             snapshot.NextActionKindCounts,
			"next_action_summaries":          snapshot.NextActionSummaries,
			"next_action_queue_count":        actionQueue.Count,
			"next_action_queue":              actionQueue.Details,
			"follow_up_trace_count":          snapshot.FollowUpTraceCount,
			"follow_up_status_counts":        snapshot.FollowUpStatusCounts,
			"follow_up_action_counts":        snapshot.FollowUpActionKindCounts,
			"follow_up_summaries":            snapshot.FollowUpSummaries,
			"follow_up_action_summaries":     snapshot.FollowUpActionSummaries,
			"follow_up_queue_count":          followUpQueue.Count,
			"follow_up_queue":                followUpQueue.Details,
			"non_executing_boundary":         "read-only governance queue inspection; no review approval, draft execution, memory rewrite, routing change, file write, tool execution, notification, rollback execution, or skill install was performed",
		}, nil)
	case experienceLearningToolActionFollowUpActions:
		result := h.app.QueryExperienceFollowUpActions(ExperienceTraceDetailQuery{
			FollowUpStatus:        firstNonEmptyExperienceString(stringVal(args, "follow_up_status"), stringVal(args, "status")),
			FollowUpActionKind:    firstNonEmptyExperienceString(stringVal(args, "follow_up_action_kind"), stringVal(args, "action_kind")),
			TriggeredRollbackOnly: boolArg(args, "triggered_rollback_only", false) || boolArg(args, "triggered_rollback", false),
			Kind:                  strings.TrimSpace(stringVal(args, "kind")),
			SourceTraceID:         strings.TrimSpace(stringVal(args, "source_trace_id")),
			Query:                 firstNonEmptyExperienceString(stringVal(args, "query"), stringVal(args, "q")),
			Limit:                 intArg(args, "limit", 20),
		})
		return experienceLearningToolResult(map[string]interface{}{
			"follow_up_actions":         result,
			"recommended_trace_id":      result.RecommendedTraceID,
			"recommended_focus_context": result.RecommendedFocusContext,
			"recommended_tool_call":     result.RecommendedToolCall,
			"non_executing_boundary":    result.NonExecutingBoundary,
		}, nil)
	case experienceLearningToolActionRoutingSignals:
		query := ExperienceRoutingSignalQuery{
			TaskType: strings.TrimSpace(stringVal(args, "task_type")),
			Tool:     strings.TrimSpace(stringVal(args, "tool")),
			Query:    firstNonEmptyExperienceString(stringVal(args, "query"), stringVal(args, "q")),
			Limit:    intArg(args, "limit", 20),
		}
		result := h.app.QueryExperienceRoutingSignals(query)
		return experienceLearningToolResult(map[string]interface{}{
			"query":                     result.Query,
			"counts":                    result.Counts,
			"returned":                  result.Returned,
			"routing_hints":             result.RoutingHints,
			"recovery_patterns":         result.RecoveryPatterns,
			"skill_nudge_candidates":    result.SkillNudgeCandidates,
			"usage_patterns":            result.UsagePatterns,
			"score_adjustments":         result.ScoreAdjustments,
			"tool_candidates":           result.ToolCandidates,
			"routing_recommendation":    result.RoutingRecommendation,
			"recommended_focus_context": result.RecommendedFocusContext,
			"recommended_tool_call":     result.RecommendedToolCall,
			"non_executing_boundary":    result.NonExecutingBoundary,
		}, nil)
	case experienceLearningToolActionToolRecovery:
		result := h.app.QueryExperienceToolRecoverySummaries(ExperienceToolRecoveryQuery{
			Tool:       strings.TrimSpace(stringVal(args, "tool")),
			Category:   strings.TrimSpace(stringVal(args, "category")),
			ReviewOnly: boolArg(args, "review_only", false),
			Provider:   strings.TrimSpace(stringVal(args, "provider")),
			Model:      strings.TrimSpace(stringVal(args, "model")),
			WireAPI:    strings.TrimSpace(stringVal(args, "wire_api")),
			Limit:      intArg(args, "limit", 20),
		})
		return experienceLearningToolResult(map[string]interface{}{
			"tool_recovery":             result,
			"query":                     result.Query,
			"count":                     result.Count,
			"returned":                  result.Returned,
			"summaries":                 result.Summaries,
			"tool_counts":               result.ToolCounts,
			"provider_counts":           result.ProviderCounts,
			"model_counts":              result.ModelCounts,
			"wire_api_counts":           result.WireAPICounts,
			"category_counts":           result.CategoryCounts,
			"review_required_count":     result.ReviewRequiredCount,
			"disabled_count":            result.DisabledCount,
			"recommended_focus_context": result.RecommendedFocusContext,
			"recommended_tool_call":     result.RecommendedToolCall,
			"non_executing_boundary":    result.NonExecutingBoundary,
		}, nil)
	case experienceLearningToolActionBuildRoutingAdjustmentDraft:
		query := ExperienceRoutingSignalQuery{
			TaskType: strings.TrimSpace(stringVal(args, "task_type")),
			Tool:     strings.TrimSpace(stringVal(args, "tool")),
			Query:    firstNonEmptyExperienceString(stringVal(args, "query"), stringVal(args, "q")),
			Limit:    intArg(args, "limit", 12),
		}
		draft := h.app.BuildExperienceRoutingAdjustmentDraft(query)
		return experienceLearningToolResult(map[string]interface{}{
			"routing_adjustment_draft":  draft,
			"recommended_focus_context": draft.RecommendedFocusContext,
			"recommended_tool_call":     draft.RecommendedToolCall,
			"non_executing_boundary":    draft.NonExecutingBoundary,
		}, nil)
	case experienceLearningToolActionMemoryCandidates:
		query := ExperienceMemoryCandidateQuery{
			Reason: strings.TrimSpace(stringVal(args, "reason")),
			Source: strings.TrimSpace(stringVal(args, "source")),
			Query:  firstNonEmptyExperienceString(stringVal(args, "query"), stringVal(args, "q")),
			Limit:  intArg(args, "limit", 40),
		}
		result := h.app.QueryExperienceProtectedMemoryCandidates(query)
		return experienceLearningToolResult(map[string]interface{}{
			"query":                      result.Query,
			"scanned_entries":            result.ScannedEntries,
			"active_entries":             result.ActiveEntries,
			"total":                      result.Total,
			"count":                      result.Count,
			"returned":                   result.Returned,
			"reason_counts":              result.ReasonCounts,
			"source_counts":              result.SourceCounts,
			"layered_recommended":        result.LayeredRecommended,
			"layered_reason":             result.LayeredReason,
			"maintenance_recommendation": result.MaintenanceRecommendation,
			"recommended_focus_context":  result.RecommendedFocusContext,
			"recommended_tool_call":      result.RecommendedToolCall,
			"non_executing_boundary":     result.NonExecutingBoundary,
			"memory_candidates":          result.Candidates,
		}, nil)
	case experienceLearningToolActionBuildMemoryMaintenanceDraft:
		query := ExperienceMemoryCandidateQuery{
			Reason: strings.TrimSpace(stringVal(args, "reason")),
			Source: strings.TrimSpace(stringVal(args, "source")),
			Query:  firstNonEmptyExperienceString(stringVal(args, "query"), stringVal(args, "q")),
			Limit:  intArg(args, "limit", 24),
		}
		draft := h.app.BuildExperienceMemoryMaintenanceDraft(query)
		return experienceLearningToolResult(map[string]interface{}{
			"memory_maintenance_draft":  draft,
			"recommended_focus_context": draft.RecommendedFocusContext,
			"recommended_tool_call":     draft.RecommendedToolCall,
			"non_executing_boundary":    draft.NonExecutingBoundary,
		}, nil)
	case experienceLearningToolActionTraceDetails:
		query := ExperienceTraceDetailQuery{
			Filter:             strings.TrimSpace(stringVal(args, "filter")),
			ReviewStatus:       strings.TrimSpace(stringVal(args, "review_status")),
			ActionKind:         strings.TrimSpace(stringVal(args, "action_kind")),
			FollowUpStatus:     strings.TrimSpace(stringVal(args, "follow_up_status")),
			FollowUpActionKind: strings.TrimSpace(stringVal(args, "follow_up_action_kind")),
			Kind:               strings.TrimSpace(stringVal(args, "kind")),
			SourceType:         strings.TrimSpace(stringVal(args, "source_type")),
			TraceID:            strings.TrimSpace(stringVal(args, "trace_id")),
			SourceTraceID:      strings.TrimSpace(stringVal(args, "source_trace_id")),
			Query:              firstNonEmptyExperienceString(stringVal(args, "query"), stringVal(args, "q")),
			Limit:              intArg(args, "limit", 40),
		}
		result := h.app.QueryExperienceTraceDetails(query)
		return experienceLearningToolResult(map[string]interface{}{
			"trace_details":             result.Details,
			"count":                     result.Count,
			"returned":                  result.Returned,
			"total":                     result.Total,
			"query":                     result.Query,
			"recommended_trace_id":      result.RecommendedTraceID,
			"recommended_trace_title":   result.RecommendedTraceTitle,
			"recommended_reason":        result.RecommendedReason,
			"recommended_focus_context": result.RecommendedFocusContext,
			"recommended_tool_call":     result.RecommendedToolCall,
			"non_executing_boundary":    result.NonExecutingBoundary,
		}, nil)
	case experienceLearningToolActionBuildFollowUp:
		traceID := strings.TrimSpace(stringVal(args, "trace_id"))
		if traceID == "" {
			return experienceLearningToolResult(nil, fmt.Errorf("trace_id is required"))
		}
		draft, err := h.app.BuildExperienceTraceFollowUp(traceID)
		return experienceLearningToolResult(map[string]interface{}{
			"followup_draft":            draft,
			"trace_id":                  draft.TraceID,
			"recommended_focus_context": draft.RecommendedFocusContext,
			"recommended_tool_call":     draft.RecommendedToolCall,
			"non_executing_boundary":    draft.NonExecutingBoundary,
		}, err)
	case experienceLearningToolActionBuildSkillDraft:
		traceID := strings.TrimSpace(stringVal(args, "trace_id"))
		if traceID == "" {
			query := ExperienceRoutingSignalQuery{
				TaskType: strings.TrimSpace(stringVal(args, "task_type")),
				Tool:     strings.TrimSpace(stringVal(args, "tool")),
				Query:    firstNonEmptyExperienceString(stringVal(args, "query"), stringVal(args, "q")),
				Limit:    intArg(args, "limit", 5),
			}
			draft := h.app.BuildExperienceSkillDraftFromUsageNudge(query)
			return experienceLearningToolResult(map[string]interface{}{
				"skill_draft":               draft,
				"recommended_focus_context": draft.RecommendedFocusContext,
				"recommended_tool_call":     draft.RecommendedToolCall,
				"non_executing_boundary":    draft.NonExecutingBoundary,
			}, nil)
		}
		draft, err := h.app.BuildExperienceSkillDraft(traceID)
		return experienceLearningToolResult(map[string]interface{}{
			"skill_draft":               draft,
			"trace_id":                  draft.TraceID,
			"recommended_focus_context": draft.RecommendedFocusContext,
			"recommended_tool_call":     draft.RecommendedToolCall,
			"non_executing_boundary":    draft.NonExecutingBoundary,
		}, err)
	case experienceLearningToolActionBuildRollbackDraft:
		traceID := strings.TrimSpace(stringVal(args, "trace_id"))
		if traceID == "" {
			return experienceLearningToolResult(nil, fmt.Errorf("trace_id is required"))
		}
		draft, err := h.app.BuildExperienceRollbackWorkflowDraft(traceID)
		return experienceLearningToolResult(map[string]interface{}{
			"rollback_draft":            draft,
			"trace_id":                  draft.TraceID,
			"recommended_focus_context": draft.RecommendedFocusContext,
			"recommended_tool_call":     draft.RecommendedToolCall,
			"non_executing_boundary":    draft.NonExecutingBoundary,
		}, err)
	case experienceLearningToolActionBuildEscalationBrief:
		traceID := strings.TrimSpace(stringVal(args, "trace_id"))
		if traceID == "" {
			return experienceLearningToolResult(nil, fmt.Errorf("trace_id is required"))
		}
		brief, err := h.app.BuildExperienceEscalationBrief(traceID)
		return experienceLearningToolResult(map[string]interface{}{
			"escalation_brief":          brief,
			"trace_id":                  brief.TraceID,
			"recommended_focus_context": brief.RecommendedFocusContext,
			"recommended_tool_call":     brief.RecommendedToolCall,
			"non_executing_boundary":    brief.NonExecutingBoundary,
		}, err)
	case experienceLearningToolActionBuildConflictDraft:
		traceID := strings.TrimSpace(stringVal(args, "trace_id"))
		if traceID == "" {
			return experienceLearningToolResult(nil, fmt.Errorf("trace_id is required"))
		}
		draft, err := h.app.BuildExperienceConflictReconciliationDraft(traceID)
		return experienceLearningToolResult(map[string]interface{}{
			"conflict_draft":            draft,
			"trace_id":                  draft.TraceID,
			"recommended_focus_context": draft.RecommendedFocusContext,
			"recommended_tool_call":     draft.RecommendedToolCall,
			"non_executing_boundary":    draft.NonExecutingBoundary,
		}, err)
	case experienceLearningToolActionRecordFollowUp:
		traceID := strings.TrimSpace(stringVal(args, "trace_id"))
		if traceID == "" {
			return experienceLearningToolResult(nil, fmt.Errorf("trace_id is required"))
		}
		status := strings.TrimSpace(stringVal(args, "status"))
		if status == "" {
			return experienceLearningToolResult(nil, fmt.Errorf("status is required"))
		}
		req := ExperienceTraceFollowUpRequest{
			Status: status,
			Note:   strings.TrimSpace(stringVal(args, "note")),
			Actor:  strings.TrimSpace(stringVal(args, "actor")),
		}
		record, err := h.app.RecordExperienceTraceFollowUp(traceID, req)
		if err != nil {
			return experienceLearningToolResult(nil, err)
		}
		return experienceLearningToolResult(map[string]interface{}{
			"followup_record":           record,
			"trace_id":                  record.TraceID,
			"status":                    record.Status,
			"recommended_focus_context": record.RecommendedFocusContext,
			"recommended_tool_call":     record.RecommendedToolCall,
			"non_executing_boundary":    record.NonExecutingBoundary,
		}, nil)
	case experienceLearningToolActionRecordReview:
		traceID := strings.TrimSpace(stringVal(args, "trace_id"))
		if traceID == "" {
			return experienceLearningToolResult(nil, fmt.Errorf("trace_id is required"))
		}
		outcome := strings.TrimSpace(firstNonEmptyExperienceString(stringVal(args, "outcome"), stringVal(args, "status")))
		if outcome == "" {
			return experienceLearningToolResult(nil, fmt.Errorf("outcome is required"))
		}
		record, err := h.app.ReviewExperienceTrace(traceID, ExperienceTraceReviewRequest{
			Outcome:  outcome,
			Note:     strings.TrimSpace(stringVal(args, "note")),
			Reviewer: firstNonEmptyExperienceString(stringVal(args, "reviewer"), stringVal(args, "actor")),
		})
		if err != nil {
			return experienceLearningToolResult(nil, err)
		}
		return experienceLearningToolResult(map[string]interface{}{
			"review_record":             record,
			"trace_id":                  record.TraceID,
			"outcome":                   record.Outcome,
			"recommended_focus_context": record.RecommendedFocusContext,
			"recommended_tool_call":     record.RecommendedToolCall,
			"non_executing_boundary":    record.NonExecutingBoundary,
		}, nil)
	case experienceLearningToolActionRecordDraftReview:
		status := strings.TrimSpace(stringVal(args, "status"))
		if status == "" {
			return experienceLearningToolResult(nil, fmt.Errorf("status is required"))
		}
		req := ExperienceDraftReviewRequest{
			Kind:                 strings.TrimSpace(stringVal(args, "draft_kind")),
			Status:               status,
			SourceTraceID:        firstNonEmptyExperienceString(stringVal(args, "source_trace_id"), stringVal(args, "trace_id")),
			Query:                firstNonEmptyExperienceString(stringVal(args, "query"), stringVal(args, "q")),
			Note:                 strings.TrimSpace(stringVal(args, "note")),
			Actor:                strings.TrimSpace(stringVal(args, "actor")),
			DraftMarkdown:        strings.TrimSpace(stringVal(args, "draft_markdown")),
			NonExecutingBoundary: strings.TrimSpace(stringVal(args, "non_executing_boundary")),
		}
		if req.Kind == "" {
			req.Kind = strings.TrimSpace(stringVal(args, "kind"))
		}
		record, err := h.app.RecordExperienceDraftReview(req)
		return experienceLearningToolResult(map[string]interface{}{
			"draft_review":              record,
			"trace_id":                  record.TraceID,
			"status":                    record.Status,
			"recommended_focus_context": record.RecommendedFocusContext,
			"recommended_tool_call":     record.RecommendedToolCall,
			"non_executing_boundary":    record.NonExecutingBoundary,
		}, err)
	default:
		return experienceLearningToolResult(nil, fmt.Errorf("unsupported experience_learning action; use snapshot, governance_summary, next_actions, queues, follow_up_actions, routing_signals, tool_recovery/inspect_tool_recovery_governance/recovery_governance/tool_recovery_governance, build_routing_adjustment_draft, memory_candidates, build_memory_maintenance_draft, trace_details, build_followup, build_skill_draft, build_rollback_draft, build_escalation_brief, build_conflict_draft, record_followup, record_review, or record_draft_review"))
	}
}

func (a *App) GetExperienceGovernanceSummary(req ExperienceRoutingSignalQuery) map[string]interface{} {
	snapshot := ExperienceLearningSnapshot{
		TraceKindCounts:          map[string]int{},
		ReviewStatusCounts:       map[string]int{},
		NextActionKindCounts:     map[string]int{},
		FollowUpStatusCounts:     map[string]int{},
		FollowUpActionKindCounts: map[string]int{},
		ReviewSummaries:          []ExperienceReviewSummary{},
		NextActionSummaries:      []ExperienceNextActionSummary{},
		FollowUpSummaries:        []ExperienceFollowUpSummary{},
		FollowUpActionSummaries:  []ExperienceFollowUpActionSummary{},
	}
	var routingSignals *ExperienceRoutingSignalResult
	if a != nil {
		snapshot = a.GetExperienceLearningSnapshot()
		req = normalizeExperienceRoutingSignalQuery(req)
		if req.TaskType != "" || req.Tool != "" || req.Query != "" {
			result := a.QueryExperienceRoutingSignals(req)
			routingSignals = &result
		}
	}
	summary := experienceGovernanceSummary(snapshot, routingSignals)
	normalizeExperienceLearningSafeHandoff(summary)
	if block, ok := summary["memory"].(map[string]interface{}); ok {
		normalizeExperienceLearningSafeHandoff(block)
	}
	if block, ok := summary["routing_self_evolution"].(map[string]interface{}); ok {
		normalizeExperienceLearningSafeHandoff(block)
	}
	if block, ok := summary["a2a_discussion"].(map[string]interface{}); ok {
		normalizeExperienceLearningSafeHandoff(block)
	}
	return summary
}

func experienceSnapshotFocusContext(snapshot ExperienceLearningSnapshot) map[string]interface{} {
	ctx := map[string]interface{}{
		"action_kind":                 "inspect_governance_summary",
		"reason":                      "read-only experience snapshot; inspect governance summary before any review, draft, memory, routing, rollback, or skill action",
		"trace_detail_count":          snapshot.TraceDetailCount,
		"review_required_trace_count": snapshot.ReviewRequiredTraceCount,
		"next_action_trace_count":     snapshot.NextActionTraceCount,
		"follow_up_trace_count":       snapshot.FollowUpTraceCount,
		"protected_memory_count":      snapshot.ProtectedMemoryCount,
		"routing_hint_count":          snapshot.RoutingHintCount,
		"skill_nudge_count":           snapshot.SkillNudgeCount,
	}
	if snapshot.LayeredMemoryRecommended {
		ctx["layered_memory_recommended"] = true
	}
	return ctx
}

func experienceSnapshotRecommendedToolCall(focusContext map[string]interface{}) map[string]interface{} {
	return normalizeExperienceLearningRecommendedToolCall(map[string]interface{}{
		"tool":                      "experience_learning",
		"args":                      map[string]interface{}{"action": "governance_summary"},
		"recommended_focus_context": focusContext,
		"non_executing":             true,
		"non_executing_boundary":    "recommended governance summary inspection only; it must not approve reviews, execute rollback, rewrite memory, change routing, write files, send notifications, run tools, or install skills",
	}, focusContext, "")
}

func experienceGovernanceSummary(snapshot ExperienceLearningSnapshot, routingSignals *ExperienceRoutingSignalResult) map[string]interface{} {
	recommendedAction, recommendedReason := experienceGovernanceRecommendedNextAction(snapshot, routingSignals)
	memorySummary := map[string]interface{}{
		"protected_memory_count":            snapshot.ProtectedMemoryCount,
		"layered_memory_recommended":        snapshot.LayeredMemoryRecommended,
		"layered_memory_reason":             snapshot.LayeredMemoryReason,
		"memory_maintenance_recommendation": snapshot.MemoryMaintenanceRecommendation,
		"memory_maintenance_boundary":       snapshot.MemoryMaintenanceBoundary,
	}
	if snapshot.MemoryExperience != nil {
		memorySummary["active_entries"] = snapshot.MemoryExperience.ActiveEntries
		memorySummary["source_counts"] = snapshot.MemoryExperience.SourceCounts
		memorySummary["protected_reason_counts"] = snapshot.MemoryExperience.ProtectedReasonCounts
	}
	if snapshot.LayeredMemoryRecommended || snapshot.ProtectedMemoryCount > 0 {
		memoryAction := "inspect_memory_candidates"
		memoryReason := "protected memory anchors are available for maintenance planning"
		if snapshot.LayeredMemoryRecommended {
			memoryAction = "build_memory_maintenance_draft"
			memoryReason = "memory volume or protected evidence suggests retention-aware maintenance before lossy compression"
		}
		memoryToolCall := experienceGovernanceRecommendedToolCall(memoryAction, snapshot, nil, memoryReason)
		memorySummary["recommended_tool_call"] = memoryToolCall
		memorySummary["recommended_focus_context"] = memoryToolCall["recommended_focus_context"]
		memorySummary["non_executing_boundary"] = firstNonEmptyExperienceString(snapshot.MemoryMaintenanceBoundary, "read-only memory governance handoff; no compression, promotion, deletion, rewrite, routing change, file write, tool execution, notification, or skill install was performed")
	}
	routingSummary := map[string]interface{}{
		"routing_hint_count":       snapshot.RoutingHintCount,
		"skill_nudge_count":        snapshot.SkillNudgeCount,
		"recovery_pattern_count":   snapshot.RecoveryPatternCount,
		"usage_pattern_count":      snapshot.UsagePatternCount,
		"routing_hint_samples":     snapshot.RoutingHints,
		"skill_nudge_samples":      snapshot.SkillNudgeCandidates,
		"recovery_samples":         snapshot.RecoveryPatterns,
		"tool_recovery_summaries":  snapshot.ToolRecoverySummaries,
		"tool_recovery_governance": experienceToolRecoveryGovernanceFromSummaries(snapshot.ToolRecoverySummaries),
	}
	if routingSignals != nil {
		routingSummary["query"] = routingSignals.Query
		routingSummary["matched_counts"] = routingSignals.Counts
		routingSummary["tool_candidates"] = routingSignals.ToolCandidates
		routingSummary["score_adjustments"] = routingSignals.ScoreAdjustments
		routingSummary["recommendation"] = routingSignals.RoutingRecommendation
		routingSummary["recommended_focus_context"] = routingSignals.RecommendedFocusContext
		routingSummary["recommended_tool_call"] = routingSignals.RecommendedToolCall
		routingSummary["non_executing_boundary"] = routingSignals.NonExecutingBoundary
	}
	a2aSummary := map[string]interface{}{
		"discussion_results":           snapshot.TraceKindCounts["a2a_discussion_result"],
		"conflict_reviews":             snapshot.TraceKindCounts[experienceTraceKindA2AConflictReview.String()],
		"rollback_reviews":             snapshot.TraceKindCounts[experienceTraceKindA2ARollbackReview.String()],
		"escalation_evidence":          snapshot.TraceKindCounts[experienceTraceKindA2AEscalationEvidence.String()],
		"review_required_trace_count":  snapshot.ReviewRequiredTraceCount,
		"review_status_counts":         snapshot.ReviewStatusCounts,
		"review_summaries":             snapshot.ReviewSummaries,
		"next_action_summaries":        snapshot.NextActionSummaries,
		"follow_up_action_summaries":   snapshot.FollowUpActionSummaries,
		"follow_up_action_kind_counts": snapshot.FollowUpActionKindCounts,
	}
	if experienceGovernanceA2ASignalCount(snapshot) > 0 {
		a2aFocusContext, a2aToolCall := experienceGovernanceA2ASummaryHandoff(snapshot)
		a2aSummary["recommended_focus_context"] = a2aFocusContext
		a2aSummary["recommended_tool_call"] = a2aToolCall
		a2aSummary["non_executing_boundary"] = "read-only A2A governance inspection; no discussion was started, no invitations or messages were sent, no result was submitted, no chat injection or memory promotion occurred, no rollback was executed, and no routing change was performed"
	}
	queueSummary := map[string]interface{}{
		"trace_detail_count":         snapshot.TraceDetailCount,
		"next_action_trace_count":    snapshot.NextActionTraceCount,
		"next_action_kind_counts":    snapshot.NextActionKindCounts,
		"next_action_summaries":      snapshot.NextActionSummaries,
		"follow_up_trace_count":      snapshot.FollowUpTraceCount,
		"follow_up_status_counts":    snapshot.FollowUpStatusCounts,
		"follow_up_summaries":        snapshot.FollowUpSummaries,
		"follow_up_action_counts":    snapshot.FollowUpActionKindCounts,
		"follow_up_action_summaries": snapshot.FollowUpActionSummaries,
	}
	return map[string]interface{}{
		"recommended_next_action":        recommendedAction,
		"recommended_next_action_reason": recommendedReason,
		"recommended_focus":              experienceGovernanceRecommendedFocus(recommendedAction),
		"recommended_focus_context":      experienceGovernanceRecommendedToolCallFocusContext(recommendedAction, snapshot, recommendedReason),
		"recommended_tool_call":          experienceGovernanceRecommendedToolCall(recommendedAction, snapshot, routingSignals, recommendedReason),
		"memory":                         memorySummary,
		"routing_self_evolution":         routingSummary,
		"a2a_discussion":                 a2aSummary,
		"queues":                         queueSummary,
		"non_executing_boundary":         "read-only governance summary; no review approval, memory rewrite, routing change, rollback execution, file write, notification, tool execution, or skill install was performed",
	}
}

func experienceGovernanceA2ASignalCount(snapshot ExperienceLearningSnapshot) int {
	return snapshot.TraceKindCounts["a2a_discussion_result"] +
		snapshot.TraceKindCounts[experienceTraceKindA2AConflictReview.String()] +
		snapshot.TraceKindCounts[experienceTraceKindA2ARollbackReview.String()] +
		snapshot.TraceKindCounts[experienceTraceKindA2AEscalationEvidence.String()] +
		snapshot.ReviewRequiredTraceCount
}

func experienceGovernanceA2ASummaryHandoff(snapshot ExperienceLearningSnapshot) (map[string]interface{}, map[string]interface{}) {
	filter := "a2a"
	reason := "A2A discussion evidence is available for read-only governance inspection"
	if snapshot.ReviewRequiredTraceCount > 0 {
		filter = "review"
		reason = "A2A conflict or rollback evidence requires manual review before any follow-up work"
	}
	focusContext := map[string]interface{}{
		"trace_filter":                filter,
		"reason":                      reason,
		"review_required_trace_count": snapshot.ReviewRequiredTraceCount,
		"a2a_discussion_results":      snapshot.TraceKindCounts["a2a_discussion_result"],
		"a2a_conflict_reviews":        snapshot.TraceKindCounts[experienceTraceKindA2AConflictReview.String()],
		"a2a_rollback_reviews":        snapshot.TraceKindCounts[experienceTraceKindA2ARollbackReview.String()],
		"a2a_escalation_evidence":     snapshot.TraceKindCounts[experienceTraceKindA2AEscalationEvidence.String()],
	}
	return focusContext, map[string]interface{}{
		"tool": "experience_learning",
		"args": map[string]interface{}{
			"action": "trace_details",
			"filter": filter,
			"limit":  20,
		},
		"recommended_focus_context": focusContext,
		"non_executing":             true,
		"non_executing_boundary":    "recommended A2A trace inspection only; it must not start discussions, invite experts, send messages, submit results, inject chat, promote memory, execute rollback, or change routing",
	}
}

func experienceGovernanceRecommendedNextAction(snapshot ExperienceLearningSnapshot, routingSignals *ExperienceRoutingSignalResult) (string, string) {
	if experienceGovernanceHasScopedRoutingCandidates(routingSignals) {
		return experienceGovernanceActionReviewRoutingCandidates.String(), "matching bounded routing evidence is available for the current scoped query"
	}
	if experienceGovernanceHasNextAction(snapshot, experienceGovernanceActionReviewTriggeredRollbackSignal.String()) {
		return experienceGovernanceActionReviewTriggeredRollbackSignal.String(), "current A2A evidence already matches rollback conditions and should be reviewed before any draft workflow is used"
	}
	if snapshot.ReviewRequiredTraceCount > 0 {
		return experienceGovernanceActionReviewRequiredTraces.String(), "review-gated conflict, rollback, or self-evolution signals should be inspected before follow-up work"
	}
	if len(snapshot.NextActionSummaries) > 0 {
		kind := strings.TrimSpace(snapshot.NextActionSummaries[0].Kind)
		if kind != "" {
			return kind, "queued non-executing follow-up guidance is available for the highest-priority action kind"
		}
	}
	if routingSignals != nil && len(routingSignals.ToolCandidates) > 0 {
		return experienceGovernanceActionReviewRoutingCandidates.String(), "matching bounded routing evidence is available for the current query"
	}
	if snapshot.LayeredMemoryRecommended {
		return experienceGovernanceActionBuildMemoryMaintenanceDraft.String(), "memory volume or protected evidence suggests retention-aware maintenance before lossy compression"
	}
	if experienceGovernanceHasTriggeredRollbackFollowUps(snapshot) {
		return experienceGovernanceActionInspectTriggeredRollbackFollowups.String(), "owner-reviewed triggered rollback follow-up records exist and should be inspected before they fade into generic rollback audit history"
	}
	if snapshot.FollowUpTraceCount > 0 {
		return experienceGovernanceActionInspectFollowUpActions.String(), "manual follow-up outcomes exist and may explain why queued actions were closed, blocked, or deferred"
	}
	if snapshot.SkillNudgeCount > 0 {
		return experienceGovernanceActionInspectSkillNudgeCandidates.String(), "repeated tool sequences have self-evolution candidates that remain review-gated"
	}
	if len(snapshot.ToolRecoverySummaries) > 0 {
		return experienceGovernanceActionInspectToolRecoveryGovernance.String(), "tool recovery evidence exists and can be inspected without changing routing or retry policy"
	}
	if snapshot.RoutingHintCount > 0 || snapshot.RecoveryPatternCount > 0 || snapshot.UsagePatternCount > 0 {
		return experienceGovernanceActionInspectRoutingSignals.String(), "routing and recovery evidence exists, but no query-specific candidate was requested"
	}
	if snapshot.ProtectedMemoryCount > 0 {
		return experienceGovernanceActionInspectMemoryCandidates.String(), "protected memory anchors are available for maintenance planning"
	}
	return experienceGovernanceActionNormalOperation.String(), "no immediate governance queue or routing/self-evolution evidence requires attention"
}

func experienceGovernanceHasScopedRoutingCandidates(routingSignals *ExperienceRoutingSignalResult) bool {
	if routingSignals == nil || len(routingSignals.ToolCandidates) == 0 {
		return false
	}
	query := routingSignals.Query
	return strings.TrimSpace(query.TaskType) != "" || strings.TrimSpace(query.Tool) != "" || strings.TrimSpace(query.Query) != ""
}

func experienceGovernanceRecommendedFocus(action string) map[string]interface{} {
	action = strings.TrimSpace(action)
	actionKind := normalizeExperienceGovernanceActionKind(action)
	focus := map[string]interface{}{
		"action":        action,
		"non_executing": true,
	}
	switch actionKind {
	case experienceGovernanceActionReviewRequiredTraces, experienceGovernanceActionReviewSignal:
		focus["trace_filter"] = "review"
	case experienceGovernanceActionInspectTriggeredRollbackFollowups:
		focus["trace_filter"] = "followups"
		focus["follow_up_action_kind"] = experienceGovernanceActionDraftRollbackWorkflow.String()
		focus["triggered_rollback_only"] = true
	case experienceGovernanceActionInspectFollowUpActions:
		focus["trace_filter"] = "followups"
	case experienceGovernanceActionReviewRoutingCandidates, experienceGovernanceActionInspectRoutingSignals, experienceGovernanceActionInspectSkillNudgeCandidates, experienceGovernanceActionInspectToolRecoveryGovernance:
		focus["trace_filter"] = "tools"
	case experienceGovernanceActionBuildMemoryMaintenanceDraft, experienceGovernanceActionInspectMemoryCandidates, experienceGovernanceActionNormalOperation, experienceGovernanceActionUnknown:
		focus["trace_filter"] = "all"
	default:
		focus["trace_filter"] = "actions"
		focus["action_kind"] = action
	}
	return focus
}

func experienceGovernanceRecommendedToolCall(action string, snapshot ExperienceLearningSnapshot, routingSignals *ExperienceRoutingSignalResult, reasons ...string) map[string]interface{} {
	action = strings.TrimSpace(action)
	actionKind := normalizeExperienceGovernanceActionKind(action)
	reason := ""
	if len(reasons) > 0 {
		reason = strings.TrimSpace(reasons[0])
	}
	args := map[string]interface{}{}
	switch actionKind {
	case experienceGovernanceActionReviewTriggeredRollbackSignal:
		args["action"] = "trace_details"
		args["filter"] = "actions"
		args["action_kind"] = experienceGovernanceActionReviewTriggeredRollbackSignal.String()
		args["kind"] = experienceTraceKindA2ARollbackReview.String()
		args["limit"] = 20
	case experienceGovernanceActionInspectTriggeredRollbackFollowups:
		args["action"] = "follow_up_actions"
		if kind := experienceGovernanceTopTriggeredRollbackFollowUpActionKind(snapshot); kind != "" {
			args["follow_up_action_kind"] = kind
		}
		args["triggered_rollback_only"] = true
		args["kind"] = experienceTraceKindA2ARollbackReview.String()
		args["limit"] = 20
	case experienceGovernanceActionReviewRequiredTraces:
		args["action"] = "queues"
		args["limit"] = 20
	case experienceGovernanceActionInspectFollowUpActions:
		args["action"] = "follow_up_actions"
		if kind := experienceGovernanceTopFollowUpActionKind(snapshot); kind != "" {
			args["follow_up_action_kind"] = kind
		}
		if experienceGovernanceHasTriggeredRollbackFollowUps(snapshot) {
			args["kind"] = experienceTraceKindA2ARollbackReview.String()
		}
		args["limit"] = 20
	case experienceGovernanceActionReviewRoutingCandidates:
		args["action"] = "build_routing_adjustment_draft"
		if routingSignals != nil {
			query := routingSignals.Query
			if strings.TrimSpace(query.TaskType) != "" {
				args["task_type"] = strings.TrimSpace(query.TaskType)
			}
			if strings.TrimSpace(query.Tool) != "" {
				args["tool"] = strings.TrimSpace(query.Tool)
			}
			if strings.TrimSpace(query.Query) != "" {
				args["query"] = strings.TrimSpace(query.Query)
			}
			if query.Limit > 0 {
				args["limit"] = query.Limit
			}
		}
		if _, ok := args["limit"]; !ok {
			args["limit"] = 12
		}
	case experienceGovernanceActionInspectRoutingSignals:
		args["action"] = "routing_signals"
		args["limit"] = 20
	case experienceGovernanceActionInspectToolRecoveryGovernance:
		args["action"] = "tool_recovery"
		args["limit"] = 20
	case experienceGovernanceActionInspectSkillNudgeCandidates:
		args["action"] = "trace_details"
		args["filter"] = "tools"
		args["limit"] = 20
	case experienceGovernanceActionBuildMemoryMaintenanceDraft:
		args["action"] = "build_memory_maintenance_draft"
		args["limit"] = 24
	case experienceGovernanceActionInspectMemoryCandidates:
		args["action"] = "memory_candidates"
		args["limit"] = 40
	default:
		if actionKind.IsDraftBuildAction() {
			traceID := experienceGovernanceLatestNextActionTraceID(snapshot, action)
			if traceID != "" {
				args["trace_id"] = traceID
				args["action"] = actionKind.DraftToolAction()
				break
			}
			args["action"] = "trace_details"
			args["filter"] = "actions"
			args["action_kind"] = action
			args["limit"] = 20
			break
		}
		if actionKind.IsFollowUpBuildAction() {
			traceID := experienceGovernanceLatestNextActionTraceID(snapshot, action)
			if traceID != "" {
				args["action"] = "build_followup"
				args["trace_id"] = traceID
				break
			}
			args["action"] = "trace_details"
			args["filter"] = "actions"
			args["action_kind"] = action
			args["limit"] = 20
			break
		}
		if actionKind.IsNormalOrEmpty() {
			args["action"] = "snapshot"
		} else {
			args["action"] = "trace_details"
			args["filter"] = "actions"
			args["action_kind"] = action
			args["limit"] = 20
		}
	}
	boundary := "recommended inspection or draft-building call only; it must not approve reviews, execute rollback, rewrite memory, change routing, write files, send notifications, run tools, or install skills"
	if actionKind == experienceGovernanceActionInspectToolRecoveryGovernance {
		boundary = experienceToolRecoveryNonExecutingBoundary
	}
	call := map[string]interface{}{
		"tool":                   "experience_learning",
		"args":                   args,
		"non_executing":          true,
		"non_executing_boundary": boundary,
	}
	if context := experienceGovernanceRecommendedToolCallFocusContext(action, snapshot, reason); len(context) > 0 {
		call["governance_focus_context"] = context
		call["recommended_focus_context"] = context
	}
	return normalizeExperienceLearningRecommendedToolCall(call, call["recommended_focus_context"], call["non_executing_boundary"])
}

func experienceGovernanceRecommendedToolCallFocusContext(action string, snapshot ExperienceLearningSnapshot, reason string) map[string]interface{} {
	traceID := ""
	title := ""
	actionKind := normalizeExperienceGovernanceActionKind(action)
	switch {
	case actionKind == experienceGovernanceActionReviewTriggeredRollbackSignal:
		traceID, title = experienceGovernanceLatestNextActionTraceTarget(snapshot, action)
	case actionKind == experienceGovernanceActionInspectTriggeredRollbackFollowups:
		traceID, title, reason = experienceGovernanceTriggeredRollbackFollowUpTraceTarget(snapshot, reason)
	case actionKind == experienceGovernanceActionInspectToolRecoveryGovernance:
		return experienceGovernanceToolRecoveryFocusContext(snapshot, reason)
	case actionKind.NeedsPriorityTraceTarget():
		traceID, title = experienceGovernanceLatestNextActionTraceTarget(snapshot, action)
	}
	traceID = strings.TrimSpace(traceID)
	title = strings.TrimSpace(title)
	reason = strings.TrimSpace(reason)
	if traceID == "" && title == "" && reason == "" {
		return nil
	}
	return map[string]interface{}{
		"priority_trace_id":    traceID,
		"priority_trace_title": title,
		"reason":               reason,
	}
}

func experienceGovernanceToolRecoveryFocusContext(snapshot ExperienceLearningSnapshot, reason string) map[string]interface{} {
	governance := experienceToolRecoveryGovernanceFromSummaries(snapshot.ToolRecoverySummaries)
	ctx := map[string]interface{}{
		"action_kind":           experienceGovernanceActionInspectToolRecoveryGovernance.String(),
		"reason":                firstNonEmptyExperienceString(reason, "inspect repeated tool failure recovery windows before treating them as guidance or routing preference"),
		"count":                 governance["count"],
		"review_required_count": governance["review_required_count"],
		"disabled_count":        governance["disabled_count"],
		"non_executing":         true,
	}
	for _, key := range []string{"tool_counts", "provider_counts", "model_counts", "wire_api_counts", "category_counts"} {
		if counts, ok := governance[key].(map[string]int); ok && len(counts) > 0 {
			ctx[key] = counts
		}
	}
	if len(snapshot.ToolRecoverySummaries) > 0 {
		first := snapshot.ToolRecoverySummaries[0]
		ctx["recommended_trace_id"] = first.TraceID
		ctx["recommended_title"] = first.Title
		ctx["recommended_tool"] = first.ToolName
		ctx["recommended_category"] = first.Category
		if first.ProviderName != "" {
			ctx["recommended_provider"] = first.ProviderName
		}
		if first.Model != "" {
			ctx["recommended_model"] = first.Model
		}
		if first.WireAPI != "" {
			ctx["recommended_wire_api"] = first.WireAPI
		}
	}
	return ctx
}

func experienceFocusContextFromTraceTarget(traceID, title, reason string) map[string]interface{} {
	traceID = strings.TrimSpace(traceID)
	title = strings.TrimSpace(title)
	reason = strings.TrimSpace(reason)
	if traceID == "" && title == "" && reason == "" {
		return nil
	}
	return map[string]interface{}{
		"priority_trace_id":    traceID,
		"priority_trace_title": title,
		"reason":               reason,
	}
}

func experienceGovernanceLatestNextActionTraceTarget(snapshot ExperienceLearningSnapshot, action string) (string, string) {
	action = strings.TrimSpace(action)
	if action == "" {
		return "", ""
	}
	for _, summary := range snapshot.NextActionSummaries {
		if strings.TrimSpace(summary.Kind) == action {
			return strings.TrimSpace(summary.LatestTraceID), strings.TrimSpace(summary.LatestTitle)
		}
	}
	return "", ""
}

func experienceGovernanceTriggeredRollbackFollowUpTraceTarget(snapshot ExperienceLearningSnapshot, reason string) (string, string, string) {
	for _, summary := range snapshot.FollowUpActionSummaries {
		if summary.TriggeredRollback {
			return firstNonEmptyGroupString(summary.RecommendedTraceID, summary.LatestTraceID), firstNonEmptyGroupString(summary.RecommendedTitle, summary.LatestTitle), firstNonEmptyGroupString(reason, summary.RecommendedReason, summary.LatestNote)
		}
	}
	for _, summary := range snapshot.FollowUpSummaries {
		if summary.TriggeredRollback {
			return firstNonEmptyGroupString(summary.RecommendedTraceID, summary.LatestTraceID), firstNonEmptyGroupString(summary.RecommendedTitle, summary.LatestTitle), firstNonEmptyGroupString(reason, summary.RecommendedReason, summary.LatestNote)
		}
	}
	return "", "", strings.TrimSpace(reason)
}

func experienceGovernanceLatestNextActionTraceID(snapshot ExperienceLearningSnapshot, action string) string {
	action = strings.TrimSpace(action)
	if action == "" {
		return ""
	}
	for _, summary := range snapshot.NextActionSummaries {
		if strings.TrimSpace(summary.Kind) == action {
			return strings.TrimSpace(summary.LatestTraceID)
		}
	}
	return ""
}

func experienceGovernanceHasNextAction(snapshot ExperienceLearningSnapshot, action string) bool {
	action = strings.TrimSpace(action)
	if action == "" {
		return false
	}
	for _, summary := range snapshot.NextActionSummaries {
		if strings.TrimSpace(summary.Kind) == action {
			return true
		}
	}
	return false
}

func experienceGovernanceTopFollowUpActionKind(snapshot ExperienceLearningSnapshot) string {
	if kind := experienceGovernanceTopTriggeredRollbackFollowUpActionKind(snapshot); kind != "" {
		return kind
	}
	for _, summary := range snapshot.FollowUpActionSummaries {
		if kind := strings.TrimSpace(summary.Kind); kind != "" {
			return kind
		}
	}
	return ""
}

func experienceGovernanceHasTriggeredRollbackFollowUps(snapshot ExperienceLearningSnapshot) bool {
	return experienceGovernanceTopTriggeredRollbackFollowUpActionKind(snapshot) != ""
}

func experienceGovernanceTopTriggeredRollbackFollowUpActionKind(snapshot ExperienceLearningSnapshot) string {
	for _, summary := range snapshot.FollowUpActionSummaries {
		if summary.TriggeredRollback {
			return strings.TrimSpace(summary.Kind)
		}
	}
	return ""
}

type ExperienceMemoryCandidateQuery struct {
	Reason string `json:"reason,omitempty"`
	Source string `json:"source,omitempty"`
	Query  string `json:"query,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type ExperienceMemoryCandidateResult struct {
	Query                     ExperienceMemoryCandidateQuery            `json:"query"`
	ScannedEntries            int                                       `json:"scanned_entries"`
	ActiveEntries             int                                       `json:"active_entries"`
	Total                     int                                       `json:"total"`
	Count                     int                                       `json:"count"`
	Returned                  int                                       `json:"returned"`
	ReasonCounts              map[string]int                            `json:"reason_counts"`
	SourceCounts              map[string]int                            `json:"source_counts"`
	LayeredRecommended        bool                                      `json:"layered_recommended"`
	LayeredReason             string                                    `json:"layered_reason,omitempty"`
	MaintenanceRecommendation string                                    `json:"maintenance_recommendation"`
	RecommendedFocusContext   map[string]interface{}                    `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall       map[string]interface{}                    `json:"recommended_tool_call,omitempty"`
	NonExecutingBoundary      string                                    `json:"non_executing_boundary"`
	Candidates                []corememory.ProtectedExperienceCandidate `json:"candidates"`
}

type ExperienceMemoryMaintenanceDraft struct {
	Query                     ExperienceMemoryCandidateQuery            `json:"query"`
	RecommendedFocusContext   map[string]interface{}                    `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall       map[string]interface{}                    `json:"recommended_tool_call,omitempty"`
	LayeredRecommended        bool                                      `json:"layered_recommended"`
	LayeredReason             string                                    `json:"layered_reason,omitempty"`
	MaintenanceRecommendation string                                    `json:"maintenance_recommendation"`
	ProtectedTotal            int                                       `json:"protected_total"`
	ProtectedReturned         int                                       `json:"protected_returned"`
	ReasonCounts              map[string]int                            `json:"reason_counts,omitempty"`
	SourceCounts              map[string]int                            `json:"source_counts,omitempty"`
	RetentionAnchors          []corememory.ProtectedExperienceCandidate `json:"retention_anchors"`
	Checks                    []string                                  `json:"checks"`
	DraftMarkdown             string                                    `json:"draft_markdown"`
	NonExecutingBoundary      string                                    `json:"non_executing_boundary"`
}

func (a *App) QueryExperienceProtectedMemoryCandidates(req ExperienceMemoryCandidateQuery) ExperienceMemoryCandidateResult {
	req = normalizeExperienceMemoryCandidateQuery(req)
	result := ExperienceMemoryCandidateResult{
		Query:        req,
		ReasonCounts: map[string]int{},
		SourceCounts: map[string]int{},
		Candidates:   []corememory.ProtectedExperienceCandidate{},
	}
	if a == nil {
		return result
	}
	if a.memoryStore == nil {
		a.ensureInteractionInfra()
		a.ensureMemoryStore()
	}
	if a.memoryStore == nil {
		return result
	}
	entries := a.memoryStore.List("", "")
	distill := corememory.NewExperienceDistiller().AnalyzeWithSampleLimit(entries, 0)
	result.ScannedEntries = distill.ScannedEntries
	result.ActiveEntries = distill.ActiveEntries
	result.LayeredRecommended = distill.LayeredRecommended
	result.LayeredReason = distill.Reason
	result.MaintenanceRecommendation = experienceMemoryMaintenanceRecommendation(distill)
	result.NonExecutingBoundary = "read-only memory maintenance inspection; no memory compression, promotion, deletion, or rewrite was performed"
	candidates := []corememory.ProtectedExperienceCandidate{}
	for _, entry := range entries {
		if !entry.IsActive() {
			continue
		}
		candidate, ok := corememory.ProtectedExperienceCandidateForEntry(entry)
		if !ok {
			continue
		}
		result.Total++
		result.ReasonCounts[candidate.Reason]++
		result.SourceCounts[candidate.Source]++
		if !experienceMemoryCandidateMatches(candidate, req) {
			continue
		}
		candidates = append(candidates, candidate)
	}
	result.Count = len(candidates)
	sort.SliceStable(candidates, func(i, j int) bool {
		return experienceMemoryCandidateLess(candidates[i], candidates[j])
	})
	if len(candidates) > req.Limit {
		candidates = append([]corememory.ProtectedExperienceCandidate(nil), candidates[:req.Limit]...)
	} else {
		candidates = append([]corememory.ProtectedExperienceCandidate(nil), candidates...)
	}
	result.Returned = len(candidates)
	result.Candidates = candidates
	result.RecommendedFocusContext = experienceMemoryCandidatesFocusContext(result)
	result.RecommendedToolCall = experienceMemoryCandidatesRecommendedToolCall(result)
	return finalizeExperienceMemoryCandidateResult(result)
}

func (a *App) BuildExperienceMemoryMaintenanceDraft(req ExperienceMemoryCandidateQuery) ExperienceMemoryMaintenanceDraft {
	result := ExperienceMemoryCandidateResult{
		Query:        normalizeExperienceMemoryCandidateQuery(req),
		ReasonCounts: map[string]int{},
		SourceCounts: map[string]int{},
		Candidates:   []corememory.ProtectedExperienceCandidate{},
	}
	if a != nil {
		result = a.QueryExperienceProtectedMemoryCandidates(req)
	}
	return buildExperienceMemoryMaintenanceDraft(result)
}

func buildExperienceMemoryMaintenanceDraft(result ExperienceMemoryCandidateResult) ExperienceMemoryMaintenanceDraft {
	checks := []string{
		"Review pinned, instruction, A2A, tool-usage, Swarm, and high-strength anchors before any compression prompt runs.",
		"Preserve concrete names, paths, commands, errors, decisions, objections, and rollback constraints from the listed anchors.",
		"Keep conflict, rollback, escalation, and skill-nudge evidence review-gated; do not convert it into automatic policy or routing changes.",
		"After manual maintenance, record what was preserved, compressed, deferred, or rejected as auditable follow-up evidence.",
	}
	boundary := "read-only memory maintenance draft only; no memory compression, promotion, deletion, rewrite, file write, routing change, or tool execution was performed"

	var b strings.Builder
	b.WriteString("# Memory Maintenance Draft\n\n")
	writeExperienceDraftLine(&b, "Mode", experienceMemoryMaintenanceMode(result))
	writeExperienceDraftLine(&b, "Recommendation", result.MaintenanceRecommendation)
	writeExperienceDraftLine(&b, "Layered reason", result.LayeredReason)
	writeExperienceDraftLine(&b, "Protected candidates", fmt.Sprintf("%d total / %d returned", result.Total, result.Returned))
	writeExperienceDraftLine(&b, "Reason counts", experienceMemoryMaintenanceCountLine(result.ReasonCounts))
	writeExperienceDraftLine(&b, "Source counts", experienceMemoryMaintenanceCountLine(result.SourceCounts))
	if len(result.Candidates) > 0 {
		b.WriteString("\n## Retention Anchors\n\n")
		for i, candidate := range result.Candidates {
			writeExperienceMemoryMaintenanceAnchor(&b, i+1, candidate)
		}
	} else {
		b.WriteString("\n## Retention Anchors\n\n")
		b.WriteString("No protected anchors matched the current query. Confirm the query/filter before allowing lossy maintenance.\n")
	}
	b.WriteString("\n## Manual Checklist\n\n")
	for _, check := range checks {
		b.WriteString("- [ ] ")
		b.WriteString(check)
		b.WriteString("\n")
	}
	if prompt := strings.TrimSpace(corememory.FormatExperienceProtectionPrompt(result.Candidates)); prompt != "" {
		b.WriteString("\n## Prompt Anchor Block\n\n```text\n")
		b.WriteString(prompt)
		b.WriteString("\n```\n")
	}
	b.WriteString("\n## Safety Boundary\n\n")
	b.WriteString("This draft is non-executing. It does not compress, promote, delete, rewrite, or archive memory; it does not change routing, write files, install skills, or execute tools automatically.\n")

	return finalizeExperienceMemoryMaintenanceDraft(ExperienceMemoryMaintenanceDraft{
		Query:                     result.Query,
		RecommendedFocusContext:   experienceMemoryMaintenanceDraftFocusContext(result),
		RecommendedToolCall:       experienceDraftReviewRecommendedToolCall(experienceDraftKindMaintenance, experienceMemoryMaintenanceDraftFocusContext(result), boundary, "", result.Query.Query),
		LayeredRecommended:        result.LayeredRecommended,
		LayeredReason:             result.LayeredReason,
		MaintenanceRecommendation: result.MaintenanceRecommendation,
		ProtectedTotal:            result.Total,
		ProtectedReturned:         result.Returned,
		ReasonCounts:              result.ReasonCounts,
		SourceCounts:              result.SourceCounts,
		RetentionAnchors:          append([]corememory.ProtectedExperienceCandidate(nil), result.Candidates...),
		Checks:                    checks,
		DraftMarkdown:             strings.TrimSpace(b.String()),
		NonExecutingBoundary:      boundary,
	})
}

func experienceMemoryMaintenanceRecommendation(result corememory.ExperienceDistillResult) string {
	if result.LayeredRecommended {
		return "layered retention-aware maintenance is recommended before compression or promotion; inspect protected candidates and preserve concrete evidence"
	}
	if result.ProtectedCandidates > 0 {
		return "normal maintenance can proceed with protected experience anchors; inspect candidates before compressing high-risk content"
	}
	return "normal maintenance can proceed; no protected experience candidates were detected"
}

func experienceMemoryMaintenanceDraftFocusContext(result ExperienceMemoryCandidateResult) map[string]interface{} {
	reason := firstNonEmptyExperienceString(result.MaintenanceRecommendation, "memory maintenance draft should preserve protected experience anchors")
	if len(result.Candidates) == 0 {
		return experienceFocusContextFromTraceTarget("", "", reason)
	}
	candidate := result.Candidates[0]
	traceID := strings.TrimSpace(candidate.ID)
	if traceID != "" && !strings.HasPrefix(traceID, "memory:") {
		traceID = "memory:" + traceID
	}
	title := firstNonEmptyExperienceString(candidate.Title, candidate.Summary, traceID)
	return experienceFocusContextFromTraceTarget(traceID, title, reason)
}

func experienceMemoryCandidatesFocusContext(result ExperienceMemoryCandidateResult) map[string]interface{} {
	reason := firstNonEmptyExperienceString(result.MaintenanceRecommendation, "read-only memory candidate inspection should preserve protected experience anchors")
	if len(result.Candidates) == 0 {
		return experienceFocusContextFromTraceTarget("", "", reason)
	}
	candidate := result.Candidates[0]
	traceID := strings.TrimSpace(candidate.ID)
	if traceID != "" && !strings.HasPrefix(traceID, "memory:") {
		traceID = "memory:" + traceID
	}
	title := firstNonEmptyExperienceString(candidate.Title, candidate.Summary, traceID)
	ctx := experienceFocusContextFromTraceTarget(traceID, title, reason)
	if candidate.Reason != "" {
		ctx["reason_filter"] = candidate.Reason
	}
	if candidate.Source != "" {
		ctx["source"] = candidate.Source
	}
	if result.Query.Query != "" {
		ctx["query"] = result.Query.Query
	}
	return ctx
}

func experienceMemoryCandidatesRecommendedToolCall(result ExperienceMemoryCandidateResult) map[string]interface{} {
	args := map[string]interface{}{
		"action": "build_memory_maintenance_draft",
		"limit":  result.Query.Limit,
	}
	if result.Query.Reason != "" {
		args["reason"] = result.Query.Reason
	}
	if result.Query.Source != "" {
		args["source"] = result.Query.Source
	}
	if result.Query.Query != "" {
		args["query"] = result.Query.Query
	}
	call := map[string]interface{}{
		"tool":                      "experience_learning",
		"args":                      args,
		"recommended_focus_context": result.RecommendedFocusContext,
		"non_executing":             true,
		"non_executing_boundary":    "recommended memory maintenance draft-building call only; it must not compress, promote, delete, rewrite, or archive memory, change routing, write files, run tools, or install skills",
	}
	return normalizeExperienceLearningRecommendedToolCall(call, result.RecommendedFocusContext, "")
}

func experienceMemoryMaintenanceMode(result ExperienceMemoryCandidateResult) string {
	if result.LayeredRecommended {
		return "layered retention-aware maintenance"
	}
	return "normal retention-aware maintenance"
}

func experienceMemoryMaintenanceCountLine(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if strings.TrimSpace(key) == "" || counts[key] <= 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ", ")
}

func writeExperienceMemoryMaintenanceAnchor(b *strings.Builder, index int, candidate corememory.ProtectedExperienceCandidate) {
	if b == nil {
		return
	}
	title := firstNonEmptyExperienceString(candidate.Title, candidate.ID, "untitled")
	fmt.Fprintf(b, "%d. %s\n", index, title)
	writeExperienceDraftLine(b, "ID", candidate.ID)
	writeExperienceDraftLine(b, "Reason", candidate.Reason)
	writeExperienceDraftLine(b, "Source", candidate.Source)
	writeExperienceDraftLine(b, "Category", candidate.Category)
	if len(candidate.Tags) > 0 {
		writeExperienceDraftLine(b, "Tags", strings.Join(candidate.Tags, ", "))
	}
	writeExperienceDraftLine(b, "Summary", truncateExperienceText(candidate.Summary, 420))
	b.WriteString("\n")
}

func normalizeExperienceMemoryCandidateQuery(req ExperienceMemoryCandidateQuery) ExperienceMemoryCandidateQuery {
	req.Reason = strings.ToLower(strings.TrimSpace(req.Reason))
	req.Source = strings.ToLower(strings.TrimSpace(req.Source))
	req.Query = strings.ToLower(strings.TrimSpace(req.Query))
	if req.Limit <= 0 {
		req.Limit = 40
	}
	if req.Limit > 200 {
		req.Limit = 200
	}
	return req
}

func experienceMemoryCandidateMatches(candidate corememory.ProtectedExperienceCandidate, req ExperienceMemoryCandidateQuery) bool {
	if req.Reason != "" && strings.ToLower(strings.TrimSpace(candidate.Reason)) != req.Reason {
		return false
	}
	if req.Source != "" && strings.ToLower(strings.TrimSpace(candidate.Source)) != req.Source {
		return false
	}
	if req.Query != "" {
		values := []string{candidate.ID, candidate.Title, candidate.Summary, candidate.Category, candidate.Source, candidate.Reason}
		values = append(values, candidate.Tags...)
		if !experienceRoutingSignalContainsValue(values, req.Query) {
			return false
		}
	}
	return true
}

func experienceMemoryCandidateLess(a, b corememory.ProtectedExperienceCandidate) bool {
	if a.Pinned != b.Pinned {
		return a.Pinned
	}
	if ar, br := experienceMemoryCandidateReasonRank(a.Reason), experienceMemoryCandidateReasonRank(b.Reason); ar != br {
		return ar > br
	}
	if a.Strength != b.Strength {
		return a.Strength > b.Strength
	}
	if a.UpdatedAt != b.UpdatedAt {
		return a.UpdatedAt > b.UpdatedAt
	}
	return a.ID < b.ID
}

func experienceMemoryCandidateReasonRank(reason string) int {
	return normalizeExperienceMemoryReasonKind(reason).Rank()
}

const experienceToolRecoveryNonExecutingBoundary = "read-only tool recovery governance inspection; no review approval, memory rewrite, routing change, retry execution, credential change, file write, tool execution, notification, or skill install was performed"

type ExperienceToolRecoveryQuery struct {
	Tool       string `json:"tool,omitempty"`
	Category   string `json:"category,omitempty"`
	ReviewOnly bool   `json:"review_only,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Model      string `json:"model,omitempty"`
	WireAPI    string `json:"wire_api,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type ExperienceToolRecoveryQueryResult struct {
	Query                   ExperienceToolRecoveryQuery     `json:"query"`
	Count                   int                             `json:"count"`
	Returned                int                             `json:"returned"`
	ReviewRequiredCount     int                             `json:"review_required_count"`
	DisabledCount           int                             `json:"disabled_count"`
	ToolCounts              map[string]int                  `json:"tool_counts"`
	ProviderCounts          map[string]int                  `json:"provider_counts,omitempty"`
	ModelCounts             map[string]int                  `json:"model_counts,omitempty"`
	WireAPICounts           map[string]int                  `json:"wire_api_counts,omitempty"`
	CategoryCounts          map[string]int                  `json:"category_counts"`
	Summaries               []ExperienceToolRecoverySummary `json:"summaries"`
	RecommendedFocusContext map[string]interface{}          `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall     map[string]interface{}          `json:"recommended_tool_call,omitempty"`
	NonExecutingBoundary    string                          `json:"non_executing_boundary"`
}

func (a *App) QueryExperienceToolRecoverySummaries(req ExperienceToolRecoveryQuery) ExperienceToolRecoveryQueryResult {
	req.Tool = strings.TrimSpace(req.Tool)
	req.Category = strings.TrimSpace(req.Category)
	req.Provider = strings.TrimSpace(req.Provider)
	req.Model = strings.TrimSpace(req.Model)
	req.WireAPI = strings.TrimSpace(req.WireAPI)
	if req.Limit <= 0 {
		req.Limit = 20
	}
	snapshot := ExperienceLearningSnapshot{ToolRecoverySummaries: []ExperienceToolRecoverySummary{}}
	if a != nil {
		snapshot = a.GetExperienceLearningSnapshot()
	}
	filtered := make([]ExperienceToolRecoverySummary, 0, len(snapshot.ToolRecoverySummaries))
	toolCounts := map[string]int{}
	providerCounts := map[string]int{}
	modelCounts := map[string]int{}
	wireAPICounts := map[string]int{}
	categoryCounts := map[string]int{}
	reviewRequiredCount := 0
	disabledCount := 0
	for _, summary := range snapshot.ToolRecoverySummaries {
		if !experienceToolRecoveryFilterMatches(summary.ToolName, req.Tool) {
			continue
		}
		if !experienceToolRecoveryFilterMatches(summary.Category, req.Category) {
			continue
		}
		if !experienceToolRecoveryFilterMatches(summary.ProviderName, req.Provider) {
			continue
		}
		if !experienceToolRecoveryFilterMatches(summary.Model, req.Model) {
			continue
		}
		if !experienceToolRecoveryFilterMatches(summary.WireAPI, req.WireAPI) {
			continue
		}
		if req.ReviewOnly && !summary.ReviewRequired {
			continue
		}
		filtered = append(filtered, summary)
		toolCounts[firstNonEmptyExperienceString(summary.ToolName, "unknown_tool")]++
		if summary.ProviderName != "" {
			providerCounts[summary.ProviderName]++
		}
		if summary.Model != "" {
			modelCounts[summary.Model]++
		}
		if summary.WireAPI != "" {
			wireAPICounts[summary.WireAPI]++
		}
		categoryCounts[firstNonEmptyExperienceString(summary.Category, "unknown")]++
		if summary.ReviewRequired {
			reviewRequiredCount++
		}
		if summary.Disabled {
			disabledCount++
		}
	}
	returned := append([]ExperienceToolRecoverySummary(nil), filtered...)
	if req.Limit > 0 && len(returned) > req.Limit {
		returned = returned[:req.Limit]
	}
	result := ExperienceToolRecoveryQueryResult{
		Query:                req,
		Count:                len(filtered),
		Returned:             len(returned),
		ReviewRequiredCount:  reviewRequiredCount,
		DisabledCount:        disabledCount,
		ToolCounts:           toolCounts,
		ProviderCounts:       providerCounts,
		ModelCounts:          modelCounts,
		WireAPICounts:        wireAPICounts,
		CategoryCounts:       categoryCounts,
		Summaries:            returned,
		NonExecutingBoundary: experienceToolRecoveryNonExecutingBoundary,
	}
	result.RecommendedFocusContext = experienceToolRecoveryFocusContext(result)
	result.RecommendedToolCall = experienceToolRecoveryRecommendedToolCall(result)
	return result
}

func experienceToolRecoveryFilterMatches(value, query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return true
	}
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, query) {
		return true
	}
	return adaptiveRetrySafeTagValue(value) != "" && adaptiveRetrySafeTagValue(value) == adaptiveRetrySafeTagValue(query)
}

func experienceToolRecoveryGovernanceFromSummaries(summaries []ExperienceToolRecoverySummary) map[string]interface{} {
	result := map[string]interface{}{
		"count":                 len(summaries),
		"review_required_count": 0,
		"disabled_count":        0,
		"tool_counts":           map[string]int{},
		"provider_counts":       map[string]int{},
		"model_counts":          map[string]int{},
		"wire_api_counts":       map[string]int{},
		"category_counts":       map[string]int{},
	}
	toolCounts := result["tool_counts"].(map[string]int)
	providerCounts := result["provider_counts"].(map[string]int)
	modelCounts := result["model_counts"].(map[string]int)
	wireAPICounts := result["wire_api_counts"].(map[string]int)
	categoryCounts := result["category_counts"].(map[string]int)
	for _, summary := range summaries {
		toolCounts[firstNonEmptyExperienceString(summary.ToolName, "unknown_tool")]++
		if summary.ProviderName != "" {
			providerCounts[summary.ProviderName]++
		}
		if summary.Model != "" {
			modelCounts[summary.Model]++
		}
		if summary.WireAPI != "" {
			wireAPICounts[summary.WireAPI]++
		}
		categoryCounts[firstNonEmptyExperienceString(summary.Category, "unknown")]++
		if summary.ReviewRequired {
			result["review_required_count"] = result["review_required_count"].(int) + 1
		}
		if summary.Disabled {
			result["disabled_count"] = result["disabled_count"].(int) + 1
		}
	}
	return result
}

func experienceToolRecoveryFocusContext(result ExperienceToolRecoveryQueryResult) map[string]interface{} {
	ctx := map[string]interface{}{
		"action_kind":           "inspect_tool_recovery_governance",
		"reason":                "inspect repeated tool failure recovery windows before treating them as guidance or routing preference",
		"count":                 result.Count,
		"review_required_count": result.ReviewRequiredCount,
		"disabled_count":        result.DisabledCount,
		"non_executing":         true,
	}
	if result.Query.Tool != "" {
		ctx["tool"] = result.Query.Tool
	}
	if len(result.ToolCounts) > 0 {
		ctx["tool_counts"] = result.ToolCounts
	}
	if len(result.ProviderCounts) > 0 {
		ctx["provider_counts"] = result.ProviderCounts
	}
	if len(result.ModelCounts) > 0 {
		ctx["model_counts"] = result.ModelCounts
	}
	if len(result.WireAPICounts) > 0 {
		ctx["wire_api_counts"] = result.WireAPICounts
	}
	if result.Query.Category != "" {
		ctx["category"] = result.Query.Category
	}
	if len(result.CategoryCounts) > 0 {
		ctx["category_counts"] = result.CategoryCounts
	}
	if result.Query.Provider != "" {
		ctx["provider"] = result.Query.Provider
	}
	if result.Query.Model != "" {
		ctx["model"] = result.Query.Model
	}
	if result.Query.WireAPI != "" {
		ctx["wire_api"] = result.Query.WireAPI
	}
	if result.Query.ReviewOnly {
		ctx["review_only"] = true
	}
	if len(result.Summaries) > 0 {
		first := result.Summaries[0]
		ctx["recommended_trace_id"] = first.TraceID
		ctx["recommended_title"] = first.Title
		ctx["recommended_tool"] = first.ToolName
		ctx["recommended_category"] = first.Category
		if first.ProviderName != "" {
			ctx["recommended_provider"] = first.ProviderName
		}
		if first.Model != "" {
			ctx["recommended_model"] = first.Model
		}
		if first.WireAPI != "" {
			ctx["recommended_wire_api"] = first.WireAPI
		}
	}
	return ctx
}

func experienceToolRecoveryRecommendedToolCall(result ExperienceToolRecoveryQueryResult) map[string]interface{} {
	args := map[string]interface{}{
		"action": "tool_recovery",
		"limit":  result.Query.Limit,
	}
	if result.Query.Tool != "" {
		args["tool"] = result.Query.Tool
	}
	if result.Query.Category != "" {
		args["category"] = result.Query.Category
	}
	if result.Query.Provider != "" {
		args["provider"] = result.Query.Provider
	}
	if result.Query.Model != "" {
		args["model"] = result.Query.Model
	}
	if result.Query.WireAPI != "" {
		args["wire_api"] = result.Query.WireAPI
	}
	if result.Query.ReviewOnly || result.ReviewRequiredCount > 0 {
		args["review_only"] = true
	}
	focus := experienceToolRecoveryFocusContext(result)
	return normalizeExperienceLearningRecommendedToolCall(map[string]interface{}{
		"tool":                      "experience_learning",
		"args":                      args,
		"recommended_focus_context": focus,
		"non_executing":             true,
		"non_executing_boundary":    result.NonExecutingBoundary,
	}, focus, result.NonExecutingBoundary)
}

type ExperienceRoutingSignalQuery struct {
	TaskType string `json:"task_type,omitempty"`
	Tool     string `json:"tool,omitempty"`
	Query    string `json:"query,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type ExperienceRoutingSignalResult struct {
	Query                   ExperienceRoutingSignalQuery                `json:"query"`
	Counts                  map[string]int                              `json:"counts"`
	Returned                map[string]int                              `json:"returned"`
	RoutingHints            []coretool.ToolRoutingHint                  `json:"routing_hints"`
	RecoveryPatterns        []coretool.ToolRecoveryPattern              `json:"recovery_patterns"`
	SkillNudgeCandidates    []coretool.ToolSkillNudgeCandidate          `json:"skill_nudge_candidates"`
	UsagePatterns           []coretool.UsagePattern                     `json:"usage_patterns"`
	ScoreAdjustments        []coretool.RoutingHintAdjustmentExplanation `json:"score_adjustments"`
	ToolCandidates          []ExperienceRoutingToolCandidate            `json:"tool_candidates"`
	RoutingRecommendation   string                                      `json:"routing_recommendation"`
	RecommendedFocusContext map[string]interface{}                      `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall     map[string]interface{}                      `json:"recommended_tool_call,omitempty"`
	NonExecutingBoundary    string                                      `json:"non_executing_boundary"`
}

type ExperienceRoutingToolCandidate struct {
	ToolName       string   `json:"tool_name"`
	Adjustment     float64  `json:"adjustment"`
	Direction      string   `json:"direction"`
	Reasons        []string `json:"reasons,omitempty"`
	Recommendation string   `json:"recommendation"`
}

type ExperienceRoutingAdjustmentDraft struct {
	Query                   ExperienceRoutingSignalQuery                `json:"query"`
	RecommendedFocusContext map[string]interface{}                      `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall     map[string]interface{}                      `json:"recommended_tool_call,omitempty"`
	Counts                  map[string]int                              `json:"counts"`
	Returned                map[string]int                              `json:"returned"`
	ToolCandidates          []ExperienceRoutingToolCandidate            `json:"tool_candidates"`
	ScoreAdjustments        []coretool.RoutingHintAdjustmentExplanation `json:"score_adjustments"`
	RoutingRecommendation   string                                      `json:"routing_recommendation"`
	Checks                  []string                                    `json:"checks"`
	DraftMarkdown           string                                      `json:"draft_markdown"`
	NonExecutingBoundary    string                                      `json:"non_executing_boundary"`
}

func (a *App) QueryExperienceRoutingSignals(req ExperienceRoutingSignalQuery) ExperienceRoutingSignalResult {
	req = normalizeExperienceRoutingSignalQuery(req)
	result := ExperienceRoutingSignalResult{
		Query:                req,
		Counts:               map[string]int{},
		Returned:             map[string]int{},
		RoutingHints:         []coretool.ToolRoutingHint{},
		RecoveryPatterns:     []coretool.ToolRecoveryPattern{},
		SkillNudgeCandidates: []coretool.ToolSkillNudgeCandidate{},
		UsagePatterns:        []coretool.UsagePattern{},
		NonExecutingBoundary: "read-only routing and self-evolution evidence; no tool execution, routing change, file write, skill creation, skill install, notification, memory rewrite, or policy update was performed",
	}
	if a == nil {
		return result
	}
	tracker := a.usageTracker
	if tracker == nil {
		a.ensureInteractionInfra()
		tracker = a.usageTracker
	}
	if tracker == nil {
		return result
	}
	for _, hint := range tracker.DistillRoutingHints(14, 3) {
		if experienceRoutingHintMatches(hint, req) {
			result.Counts["routing_hints"]++
			if len(result.RoutingHints) < req.Limit {
				result.RoutingHints = append(result.RoutingHints, hint)
			}
		}
	}
	for _, pattern := range tracker.DistillRecoveryPatterns(30, 3) {
		if experienceRecoveryPatternMatches(pattern, req) {
			result.Counts["recovery_patterns"]++
			if len(result.RecoveryPatterns) < req.Limit {
				result.RecoveryPatterns = append(result.RecoveryPatterns, pattern)
			}
		}
	}
	for _, nudge := range tracker.DistillSkillNudgeCandidates(30, 3) {
		if experienceSkillNudgeMatches(nudge, req) {
			result.Counts["skill_nudge_candidates"]++
			if len(result.SkillNudgeCandidates) < req.Limit {
				result.SkillNudgeCandidates = append(result.SkillNudgeCandidates, nudge)
			}
		}
	}
	for _, pattern := range tracker.ExtractPatterns(14) {
		if experienceUsagePatternMatches(pattern, req) {
			result.Counts["usage_patterns"]++
			if len(result.UsagePatterns) < req.Limit {
				result.UsagePatterns = append(result.UsagePatterns, pattern)
			}
		}
	}
	result.Returned["routing_hints"] = len(result.RoutingHints)
	result.Returned["recovery_patterns"] = len(result.RecoveryPatterns)
	result.Returned["skill_nudge_candidates"] = len(result.SkillNudgeCandidates)
	result.Returned["usage_patterns"] = len(result.UsagePatterns)
	result.ScoreAdjustments = experienceRoutingScoreAdjustments(tracker, req, result)
	result.ToolCandidates = experienceRoutingToolCandidates(result.ScoreAdjustments)
	result.RoutingRecommendation = experienceRoutingRecommendation(result)
	result.RecommendedFocusContext = experienceRoutingSignalsFocusContext(result)
	result.RecommendedToolCall = experienceRoutingSignalsRecommendedToolCall(result)
	return finalizeExperienceRoutingSignalResult(result)
}

func (a *App) BuildExperienceRoutingAdjustmentDraft(req ExperienceRoutingSignalQuery) ExperienceRoutingAdjustmentDraft {
	result := ExperienceRoutingSignalResult{
		Query:    normalizeExperienceRoutingSignalQuery(req),
		Counts:   map[string]int{},
		Returned: map[string]int{},
	}
	if a != nil {
		result = a.QueryExperienceRoutingSignals(req)
	}
	return buildExperienceRoutingAdjustmentDraft(result)
}

func buildExperienceRoutingAdjustmentDraft(result ExperienceRoutingSignalResult) ExperienceRoutingAdjustmentDraft {
	checks := []string{
		"Compare every candidate against the current task, security policy, and explicit user intent before changing routing behavior.",
		"Treat positive adjustments as a bounded preference only; do not bypass tool permission or confirmation gates.",
		"Treat negative adjustments as a caution signal only; keep the tool available when direct task evidence is stronger.",
		"Escalate repeated skill-nudge candidates to manual skill drafting review instead of creating or installing skills automatically.",
		"Record the final routing decision as review or follow-up evidence before any persistent router or skill configuration change.",
	}
	boundary := "read-only routing adjustment draft only; no tool execution, routing change, file write, skill install, or policy update was performed"

	var b strings.Builder
	b.WriteString("# Routing Adjustment Draft\n\n")
	writeExperienceDraftLine(&b, "Task type", result.Query.TaskType)
	writeExperienceDraftLine(&b, "Tool filter", result.Query.Tool)
	writeExperienceDraftLine(&b, "Query", result.Query.Query)
	writeExperienceDraftLine(&b, "Recommendation", result.RoutingRecommendation)
	writeExperienceDraftLine(&b, "Evidence counts", experienceMemoryMaintenanceCountLine(result.Counts))
	writeExperienceDraftLine(&b, "Returned evidence", experienceMemoryMaintenanceCountLine(result.Returned))
	if len(result.ToolCandidates) > 0 {
		b.WriteString("\n## Candidate Adjustments\n\n")
		for i, candidate := range result.ToolCandidates {
			writeExperienceRoutingCandidateDraft(&b, i+1, candidate)
		}
	} else {
		b.WriteString("\n## Candidate Adjustments\n\n")
		b.WriteString("No bounded tool candidate crossed the reporting threshold. Use the normal router unless direct task evidence says otherwise.\n")
	}
	if len(result.ScoreAdjustments) > 0 {
		b.WriteString("\n## Score Evidence\n\n")
		for _, adjustment := range result.ScoreAdjustments {
			fmt.Fprintf(&b, "- %s: adjustment=%s direction=%s matching_records=%d successes=%d failures=%d recovery_evidence=%d reasons=%s\n",
				adjustment.ToolName,
				experienceRoutingAdjustmentString(adjustment.Adjustment),
				adjustment.Direction,
				adjustment.MatchingRecords,
				adjustment.Successes,
				adjustment.Failures,
				adjustment.RecoveryEvidence,
				strings.Join(adjustment.Reasons, ", "),
			)
		}
	}
	b.WriteString("\n## Manual Checklist\n\n")
	for _, check := range checks {
		b.WriteString("- [ ] ")
		b.WriteString(check)
		b.WriteString("\n")
	}
	b.WriteString("\n## Safety Boundary\n\n")
	b.WriteString("This draft is non-executing. It does not run tools, edit router weights, create or install skills, write files, send notifications, or change policy automatically.\n")

	return finalizeExperienceRoutingAdjustmentDraft(ExperienceRoutingAdjustmentDraft{
		Query:                   result.Query,
		RecommendedFocusContext: experienceRoutingAdjustmentDraftFocusContext(result),
		RecommendedToolCall:     experienceDraftReviewRecommendedToolCall(experienceDraftKindRouting, experienceRoutingAdjustmentDraftFocusContext(result), boundary, "", result.Query.Query),
		Counts:                  result.Counts,
		Returned:                result.Returned,
		ToolCandidates:          append([]ExperienceRoutingToolCandidate(nil), result.ToolCandidates...),
		ScoreAdjustments:        append([]coretool.RoutingHintAdjustmentExplanation(nil), result.ScoreAdjustments...),
		RoutingRecommendation:   result.RoutingRecommendation,
		Checks:                  checks,
		DraftMarkdown:           strings.TrimSpace(b.String()),
		NonExecutingBoundary:    boundary,
	})
}

func experienceRoutingAdjustmentDraftFocusContext(result ExperienceRoutingSignalResult) map[string]interface{} {
	parts := []string{"routing adjustment draft remains review-only"}
	if result.RoutingRecommendation != "" {
		parts = append(parts, result.RoutingRecommendation)
	}
	if result.Query.TaskType != "" {
		parts = append(parts, "task_type="+result.Query.TaskType)
	}
	if result.Query.Tool != "" {
		parts = append(parts, "tool="+result.Query.Tool)
	}
	if result.Query.Query != "" {
		parts = append(parts, "query="+result.Query.Query)
	}
	return experienceFocusContextFromTraceTarget("", "", strings.Join(parts, "; "))
}

func experienceRoutingSignalsFocusContext(result ExperienceRoutingSignalResult) map[string]interface{} {
	parts := []string{"read-only routing signal inspection"}
	if result.RoutingRecommendation != "" {
		parts = append(parts, result.RoutingRecommendation)
	}
	if result.Query.TaskType != "" {
		parts = append(parts, "task_type="+result.Query.TaskType)
	}
	if result.Query.Tool != "" {
		parts = append(parts, "tool="+result.Query.Tool)
	}
	if result.Query.Query != "" {
		parts = append(parts, "query="+result.Query.Query)
	}
	ctx := experienceFocusContextFromTraceTarget("", "", strings.Join(parts, "; "))
	if len(result.ToolCandidates) > 0 {
		ctx["tool"] = result.ToolCandidates[0].ToolName
		ctx["direction"] = result.ToolCandidates[0].Direction
	}
	if result.Query.TaskType != "" {
		ctx["task_type"] = result.Query.TaskType
	}
	if result.Query.Query != "" {
		ctx["query"] = result.Query.Query
	}
	return ctx
}

func experienceRoutingSignalsRecommendedToolCall(result ExperienceRoutingSignalResult) map[string]interface{} {
	args := map[string]interface{}{
		"action": "build_routing_adjustment_draft",
		"limit":  result.Query.Limit,
	}
	if result.Query.TaskType != "" {
		args["task_type"] = result.Query.TaskType
	}
	if result.Query.Tool != "" {
		args["tool"] = result.Query.Tool
	}
	if result.Query.Query != "" {
		args["query"] = result.Query.Query
	}
	return normalizeExperienceLearningRecommendedToolCall(map[string]interface{}{
		"tool":                      "experience_learning",
		"args":                      args,
		"recommended_focus_context": result.RecommendedFocusContext,
		"non_executing":             true,
		"non_executing_boundary":    "recommended routing adjustment draft-building call only; it must not run tools, change routing, write files, create or install skills, send notifications, or change policy",
	}, result.RecommendedFocusContext, "")
}

func normalizeExperienceRoutingSignalQuery(req ExperienceRoutingSignalQuery) ExperienceRoutingSignalQuery {
	req.TaskType = strings.ToLower(strings.TrimSpace(req.TaskType))
	req.Tool = strings.ToLower(strings.TrimSpace(req.Tool))
	req.Query = strings.ToLower(strings.TrimSpace(req.Query))
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Limit > 100 {
		req.Limit = 100
	}
	return req
}

func experienceRoutingHintMatches(hint coretool.ToolRoutingHint, req ExperienceRoutingSignalQuery) bool {
	values := []string{hint.ContextKey, hint.TaskType, hint.Description}
	values = append(values, hint.QueryTokens...)
	values = append(values, hint.PreferTools...)
	values = append(values, hint.AvoidTools...)
	values = append(values, hint.RecoveryTools...)
	tools := experienceRoutingSignalMergeTools(hint.PreferTools, hint.AvoidTools, hint.RecoveryTools)
	return experienceRoutingSignalValuesMatch(values, req, hint.TaskType, tools)
}

func experienceRecoveryPatternMatches(pattern coretool.ToolRecoveryPattern, req ExperienceRoutingSignalQuery) bool {
	values := []string{pattern.ContextKey, pattern.TaskType, pattern.FailedTool, pattern.ErrorClass, pattern.RecoveryTool, pattern.Description}
	values = append(values, pattern.QueryTokens...)
	values = append(values, pattern.ToolSequence...)
	return experienceRoutingSignalValuesMatch(values, req, pattern.TaskType, append([]string{pattern.FailedTool, pattern.RecoveryTool}, pattern.ToolSequence...))
}

func experienceSkillNudgeMatches(nudge coretool.ToolSkillNudgeCandidate, req ExperienceRoutingSignalQuery) bool {
	values := []string{nudge.ContextKey, nudge.TaskType, nudge.SuggestedName, nudge.Description}
	values = append(values, nudge.QueryTokens...)
	values = append(values, nudge.ToolSequence...)
	return experienceRoutingSignalValuesMatch(values, req, nudge.TaskType, nudge.ToolSequence)
}

func experienceUsagePatternMatches(pattern coretool.UsagePattern, req ExperienceRoutingSignalQuery) bool {
	values := []string{pattern.ToolName, pattern.Description}
	values = append(values, pattern.TopTokens...)
	return experienceRoutingSignalValuesMatch(values, req, "", []string{pattern.ToolName})
}

func experienceRoutingSignalValuesMatch(values []string, req ExperienceRoutingSignalQuery, taskType string, tools []string) bool {
	if req.TaskType != "" && strings.ToLower(strings.TrimSpace(taskType)) != req.TaskType {
		return false
	}
	if req.Tool != "" && !experienceRoutingSignalContainsValue(tools, req.Tool) {
		return false
	}
	if req.Query != "" && !experienceRoutingSignalContainsValue(values, req.Query) {
		return false
	}
	return true
}

func experienceRoutingSignalContainsValue(values []string, needle string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	if needle == "" {
		return true
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(strings.TrimSpace(value)), needle) {
			return true
		}
	}
	return false
}

func experienceRoutingSignalMergeTools(groups ...[]string) []string {
	var merged []string
	for _, group := range groups {
		merged = append(merged, group...)
	}
	return merged
}

func experienceRoutingScoreAdjustments(tracker *coretool.UsageTracker, req ExperienceRoutingSignalQuery, result ExperienceRoutingSignalResult) []coretool.RoutingHintAdjustmentExplanation {
	if tracker == nil {
		return nil
	}
	queryTokens := bm25.Tokenize(req.Query)
	if len(queryTokens) == 0 {
		queryTokens = experienceRoutingSignalTokensFromResult(result)
	}
	tools := experienceRoutingScoreCandidateTools(req, result)
	if len(tools) == 0 {
		return nil
	}
	adjustments := make([]coretool.RoutingHintAdjustmentExplanation, 0, len(tools))
	for _, toolName := range tools {
		explanation := tracker.ExplainRoutingHintAdjustment(toolName, queryTokens)
		if req.Tool != "" || explanation.Adjustment != 0 {
			adjustments = append(adjustments, explanation)
		}
	}
	sort.SliceStable(adjustments, func(i, j int) bool {
		left := experienceAbsFloat(adjustments[i].Adjustment)
		right := experienceAbsFloat(adjustments[j].Adjustment)
		if left != right {
			return left > right
		}
		return adjustments[i].ToolName < adjustments[j].ToolName
	})
	if len(adjustments) > req.Limit {
		adjustments = append([]coretool.RoutingHintAdjustmentExplanation(nil), adjustments[:req.Limit]...)
	}
	return adjustments
}

func experienceRoutingToolCandidates(adjustments []coretool.RoutingHintAdjustmentExplanation) []ExperienceRoutingToolCandidate {
	candidates := make([]ExperienceRoutingToolCandidate, 0, len(adjustments))
	for _, adjustment := range adjustments {
		direction := strings.TrimSpace(adjustment.Direction)
		if direction == "" {
			switch {
			case adjustment.Adjustment > 0:
				direction = "prefer"
			case adjustment.Adjustment < 0:
				direction = "avoid"
			default:
				direction = "neutral"
			}
		}
		candidates = append(candidates, ExperienceRoutingToolCandidate{
			ToolName:       adjustment.ToolName,
			Adjustment:     adjustment.Adjustment,
			Direction:      direction,
			Reasons:        append([]string{}, adjustment.Reasons...),
			Recommendation: experienceRoutingCandidateRecommendation(direction),
		})
	}
	return candidates
}

func experienceRoutingCandidateRecommendation(direction string) string {
	return normalizeExperienceRoutingDirectionKind(direction).Recommendation()
}

func experienceRoutingRecommendation(result ExperienceRoutingSignalResult) string {
	if len(result.ToolCandidates) > 0 {
		return "use tool_candidates as read-only bounded routing evidence; do not execute tools or create skills from this inspection alone"
	}
	if result.Counts["skill_nudge_candidates"] > 0 {
		return "review skill_nudge_candidates before drafting any skill; no skill should be created automatically"
	}
	if result.Counts["routing_hints"] > 0 || result.Counts["recovery_patterns"] > 0 || result.Counts["usage_patterns"] > 0 {
		return "matched experience evidence is available, but no score adjustment crossed the reporting threshold"
	}
	return "no matching routing or self-evolution evidence; use the normal router"
}

func writeExperienceRoutingCandidateDraft(b *strings.Builder, index int, candidate ExperienceRoutingToolCandidate) {
	if b == nil {
		return
	}
	fmt.Fprintf(b, "%d. %s\n", index, firstNonEmptyExperienceString(candidate.ToolName, "unknown_tool"))
	writeExperienceDraftLine(b, "Direction", candidate.Direction)
	writeExperienceDraftLine(b, "Adjustment", experienceRoutingAdjustmentString(candidate.Adjustment))
	writeExperienceDraftLine(b, "Reasons", strings.Join(candidate.Reasons, ", "))
	writeExperienceDraftLine(b, "Recommendation", candidate.Recommendation)
	b.WriteString("\n")
}

func experienceRoutingAdjustmentString(value float64) string {
	if value == 0 {
		return "0"
	}
	return fmt.Sprintf("%.4f", value)
}

func experienceRoutingScoreCandidateTools(req ExperienceRoutingSignalQuery, result ExperienceRoutingSignalResult) []string {
	if req.Tool != "" {
		return []string{req.Tool}
	}
	seen := map[string]struct{}{}
	var tools []string
	add := func(values ...string) {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			key := strings.ToLower(value)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			tools = append(tools, value)
		}
	}
	for _, hint := range result.RoutingHints {
		add(hint.PreferTools...)
		add(hint.AvoidTools...)
		add(hint.RecoveryTools...)
	}
	for _, pattern := range result.RecoveryPatterns {
		add(pattern.FailedTool, pattern.RecoveryTool)
		add(pattern.ToolSequence...)
	}
	for _, nudge := range result.SkillNudgeCandidates {
		add(nudge.ToolSequence...)
	}
	for _, pattern := range result.UsagePatterns {
		add(pattern.ToolName)
	}
	sort.Strings(tools)
	return tools
}

func experienceRoutingSignalTokensFromResult(result ExperienceRoutingSignalResult) []string {
	seen := map[string]struct{}{}
	var tokens []string
	add := func(values ...string) {
		for _, value := range values {
			for _, token := range bm25.Tokenize(value) {
				key := strings.ToLower(strings.TrimSpace(token))
				if key == "" {
					continue
				}
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				tokens = append(tokens, token)
				if len(tokens) >= 5 {
					return
				}
			}
		}
	}
	for _, hint := range result.RoutingHints {
		add(hint.QueryTokens...)
		add(hint.Description)
	}
	for _, pattern := range result.RecoveryPatterns {
		add(pattern.QueryTokens...)
		add(pattern.Description)
	}
	for _, nudge := range result.SkillNudgeCandidates {
		add(nudge.QueryTokens...)
		add(nudge.Description)
	}
	for _, pattern := range result.UsagePatterns {
		add(pattern.TopTokens...)
		add(pattern.Description)
	}
	return tokens
}

func experienceAbsFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

type ExperienceTraceDetailQuery struct {
	Filter                string `json:"filter,omitempty"`
	ReviewStatus          string `json:"review_status,omitempty"`
	ActionKind            string `json:"action_kind,omitempty"`
	FollowUpStatus        string `json:"follow_up_status,omitempty"`
	FollowUpActionKind    string `json:"follow_up_action_kind,omitempty"`
	TriggeredRollbackOnly bool   `json:"triggered_rollback_only,omitempty"`
	Kind                  string `json:"kind,omitempty"`
	SourceType            string `json:"source_type,omitempty"`
	TraceID               string `json:"trace_id,omitempty"`
	SourceTraceID         string `json:"source_trace_id,omitempty"`
	Query                 string `json:"query,omitempty"`
	Limit                 int    `json:"limit,omitempty"`
}

type ExperienceTraceDetailQueryResult struct {
	Query                   ExperienceTraceDetailQuery `json:"query"`
	Total                   int                        `json:"total"`
	Count                   int                        `json:"count"`
	Returned                int                        `json:"returned"`
	RecommendedTraceID      string                     `json:"recommended_trace_id,omitempty"`
	RecommendedTraceTitle   string                     `json:"recommended_trace_title,omitempty"`
	RecommendedReason       string                     `json:"recommended_reason,omitempty"`
	RecommendedFocusContext map[string]interface{}     `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall     map[string]interface{}     `json:"recommended_tool_call,omitempty"`
	NonExecutingBoundary    string                     `json:"non_executing_boundary"`
	Details                 []ExperienceTraceDetail    `json:"details"`
}

type ExperienceFollowUpActionAuditResult struct {
	Query                   ExperienceTraceDetailQuery        `json:"query"`
	Total                   int                               `json:"total"`
	Count                   int                               `json:"count"`
	Returned                int                               `json:"returned"`
	RecommendedTraceID      string                            `json:"recommended_trace_id,omitempty"`
	RecommendedTraceTitle   string                            `json:"recommended_trace_title,omitempty"`
	RecommendedReason       string                            `json:"recommended_reason,omitempty"`
	RecommendedFocusContext map[string]interface{}            `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall     map[string]interface{}            `json:"recommended_tool_call,omitempty"`
	FollowUpStatusCounts    map[string]int                    `json:"follow_up_status_counts"`
	FollowUpActionCounts    map[string]int                    `json:"follow_up_action_counts"`
	FollowUpSummaries       []ExperienceFollowUpSummary       `json:"follow_up_summaries"`
	FollowUpActionSummaries []ExperienceFollowUpActionSummary `json:"follow_up_action_summaries"`
	FollowUpDetails         []ExperienceTraceDetail           `json:"follow_up_details"`
	NonExecutingBoundary    string                            `json:"non_executing_boundary"`
}

func (a *App) QueryExperienceTraceDetails(req ExperienceTraceDetailQuery) ExperienceTraceDetailQueryResult {
	req = normalizeExperienceTraceDetailQuery(req)
	if a == nil {
		return ExperienceTraceDetailQueryResult{Query: req, Details: []ExperienceTraceDetail{}}
	}
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		a.ensureMemoryStore()
	}
	snapshot := buildExperienceLearningSnapshotWithTraceLimit(a.usageTracker, a.memoryStore, 0)
	details := append([]ExperienceTraceDetail(nil), snapshot.TraceDetails...)
	a.ensureSessionStore()
	if a.sessionSearchStore != nil {
		if summaries, err := a.sessionSearchStore.ListRecent(experienceSnapshotSessionTraceLimit); err == nil {
			details = append(details, buildExperienceSessionTraceDetails(summaries)...)
			prioritizeExperienceTraceDetails(details)
		}
	}
	total := len(details)
	filtered := make([]ExperienceTraceDetail, 0, len(details))
	for _, detail := range details {
		if experienceTraceDetailMatchesQuery(detail, req) {
			filtered = append(filtered, detail)
		}
	}
	count := len(filtered)
	if len(filtered) > req.Limit {
		filtered = append([]ExperienceTraceDetail(nil), filtered[:req.Limit]...)
	} else {
		filtered = append([]ExperienceTraceDetail(nil), filtered...)
	}
	recommendedTraceID, recommendedTraceTitle, recommendedReason := experienceRecommendedTraceTargetForQuery(req, filtered)
	focusContext := experienceFocusContextFromTraceTarget(recommendedTraceID, recommendedTraceTitle, recommendedReason)
	recommendedToolCall := map[string]interface{}(nil)
	if len(filtered) > 0 {
		recommendedToolCall = experienceTraceDetailRecommendedToolCall(filtered[0], focusContext)
	}
	return finalizeExperienceTraceDetailQueryResult(ExperienceTraceDetailQueryResult{
		Query:                   req,
		Total:                   total,
		Count:                   count,
		Returned:                len(filtered),
		RecommendedTraceID:      recommendedTraceID,
		RecommendedTraceTitle:   recommendedTraceTitle,
		RecommendedReason:       recommendedReason,
		RecommendedFocusContext: focusContext,
		RecommendedToolCall:     recommendedToolCall,
		NonExecutingBoundary:    "read-only trace detail inspection; no review approval, draft execution, memory rewrite, routing change, file write, tool execution, notification, rollback execution, or skill install was performed",
		Details:                 filtered,
	})
}

func experienceTraceDetailRecommendedToolCall(detail ExperienceTraceDetail, focusContext map[string]interface{}) map[string]interface{} {
	traceID := strings.TrimSpace(detail.ID)
	if traceID == "" {
		return nil
	}
	action := ""
	actionKind := normalizeExperienceGovernanceActionKind(detail.NextActionKind)
	switch {
	case actionKind.IsDraftBuildAction():
		action = actionKind.DraftToolAction()
	case actionKind.IsFollowUpBuildAction():
		action = "build_followup"
	default:
		return nil
	}
	return normalizeExperienceLearningRecommendedToolCall(map[string]interface{}{
		"tool":                      "experience_learning",
		"args":                      map[string]interface{}{"action": action, "trace_id": traceID},
		"recommended_focus_context": focusContext,
		"non_executing":             true,
		"non_executing_boundary":    "recommended trace-detail draft-building call only; it must not approve reviews, execute rollback, rewrite memory, change routing, write files, run tools, send notifications, or install skills",
	}, focusContext, "")
}

func experienceTraceInspectionRecommendedToolCall(traceID, traceTitle, reason string) map[string]interface{} {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return nil
	}
	focusContext := experienceFocusContextFromTraceTarget(traceID, traceTitle, reason)
	return normalizeExperienceLearningRecommendedToolCall(map[string]interface{}{
		"tool":                      "experience_learning",
		"args":                      map[string]interface{}{"action": "trace_details", "trace_id": traceID, "limit": 20},
		"recommended_focus_context": focusContext,
		"non_executing":             true,
		"non_executing_boundary":    "recommended trace inspection only; it must not approve reviews, execute rollback, rewrite memory, change routing, write files, run tools, send notifications, or install skills",
	}, focusContext, "")
}

func experienceRecommendedTraceTargetForQuery(req ExperienceTraceDetailQuery, details []ExperienceTraceDetail) (string, string, string) {
	if len(details) == 0 {
		return "", "", ""
	}
	detail := details[0]
	reason := "latest trace matching the current filter"
	if normalizeExperienceGovernanceActionKind(req.ActionKind) == experienceGovernanceActionReviewTriggeredRollbackSignal || req.TriggeredRollbackOnly {
		reason = "latest triggered rollback trace matching the current filter"
	} else if req.FollowUpActionKind != "" || req.FollowUpStatus != "" {
		reason = "latest follow-up trace matching the current filter"
	} else if req.ReviewStatus != "" || req.Filter == "review" || req.Filter == "reviewed" {
		reason = "latest review trace matching the current filter"
	}
	return strings.TrimSpace(detail.ID), strings.TrimSpace(detail.Title), reason
}

func (a *App) QueryExperienceFollowUpActions(req ExperienceTraceDetailQuery) ExperienceFollowUpActionAuditResult {
	req.Filter = "followups"
	if req.FollowUpActionKind == "" {
		req.FollowUpActionKind = req.ActionKind
	}
	req.ActionKind = ""
	details := ExperienceTraceDetailQueryResult{Query: normalizeExperienceTraceDetailQuery(req), Details: []ExperienceTraceDetail{}}
	snapshot := ExperienceLearningSnapshot{
		FollowUpStatusCounts:     map[string]int{},
		FollowUpActionKindCounts: map[string]int{},
		FollowUpSummaries:        []ExperienceFollowUpSummary{},
		FollowUpActionSummaries:  []ExperienceFollowUpActionSummary{},
	}
	if a != nil {
		details = a.QueryExperienceTraceDetails(req)
		snapshot = a.GetExperienceLearningSnapshot()
	}
	recommendedTraceID := ""
	recommendedTraceTitle := ""
	recommendedReason := ""
	if len(details.Details) > 0 {
		recommendedTraceID = details.Details[0].ID
		recommendedTraceTitle = details.Details[0].Title
		if req.TriggeredRollbackOnly {
			recommendedReason = "latest triggered rollback follow-up matching the current filter"
		} else {
			recommendedReason = "latest follow-up audit record matching the current filter"
		}
	}
	return finalizeExperienceFollowUpActionAuditResult(ExperienceFollowUpActionAuditResult{
		Query:                   details.Query,
		Total:                   details.Total,
		Count:                   details.Count,
		Returned:                details.Returned,
		RecommendedTraceID:      recommendedTraceID,
		RecommendedTraceTitle:   recommendedTraceTitle,
		RecommendedReason:       recommendedReason,
		RecommendedFocusContext: experienceFocusContextFromTraceTarget(recommendedTraceID, recommendedTraceTitle, recommendedReason),
		RecommendedToolCall:     experienceFollowUpActionsRecommendedToolCall(req, recommendedTraceID, recommendedTraceTitle, recommendedReason),
		FollowUpStatusCounts:    snapshot.FollowUpStatusCounts,
		FollowUpActionCounts:    snapshot.FollowUpActionKindCounts,
		FollowUpSummaries:       snapshot.FollowUpSummaries,
		FollowUpActionSummaries: snapshot.FollowUpActionSummaries,
		FollowUpDetails:         details.Details,
		NonExecutingBoundary:    "read-only follow-up action audit inspection; no draft execution, memory rewrite, routing change, file write, tool execution, notification, or skill install was performed",
	})
}

func experienceFollowUpActionsRecommendedToolCall(req ExperienceTraceDetailQuery, traceID, traceTitle, reason string) map[string]interface{} {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return nil
	}
	args := map[string]interface{}{
		"action":   "trace_details",
		"filter":   "followups",
		"trace_id": traceID,
		"limit":    20,
	}
	if req.FollowUpActionKind != "" {
		args["follow_up_action_kind"] = req.FollowUpActionKind
	}
	if req.FollowUpStatus != "" {
		args["follow_up_status"] = req.FollowUpStatus
	}
	if req.Kind != "" {
		args["kind"] = req.Kind
	}
	focusContext := experienceFocusContextFromTraceTarget(traceID, traceTitle, reason)
	return normalizeExperienceLearningRecommendedToolCall(map[string]interface{}{
		"tool":                      "experience_learning",
		"args":                      args,
		"recommended_focus_context": focusContext,
		"non_executing":             true,
		"non_executing_boundary":    "recommended follow-up audit trace inspection only; it must not approve reviews, execute rollback, rewrite memory, change routing, write files, run tools, send notifications, or install skills",
	}, focusContext, "")
}

func normalizeExperienceTraceDetailQuery(req ExperienceTraceDetailQuery) ExperienceTraceDetailQuery {
	req.Filter = strings.ToLower(strings.TrimSpace(req.Filter))
	req.ReviewStatus = strings.ToLower(strings.TrimSpace(req.ReviewStatus))
	req.ActionKind = strings.TrimSpace(req.ActionKind)
	req.FollowUpStatus = strings.ToLower(strings.TrimSpace(req.FollowUpStatus))
	req.FollowUpActionKind = strings.TrimSpace(req.FollowUpActionKind)
	req.Kind = strings.TrimSpace(req.Kind)
	req.SourceType = strings.TrimSpace(req.SourceType)
	req.TraceID = strings.TrimSpace(req.TraceID)
	req.SourceTraceID = strings.TrimSpace(req.SourceTraceID)
	req.Query = strings.TrimSpace(req.Query)
	if req.Limit <= 0 {
		req.Limit = 40
	}
	if req.Limit > 200 {
		req.Limit = 200
	}
	return req
}

func experienceTraceDetailMatchesQuery(detail ExperienceTraceDetail, req ExperienceTraceDetailQuery) bool {
	switch normalizeExperienceTraceQueryFilterKind(req.Filter) {
	case experienceTraceQueryFilterReview:
		if !detail.ReviewRequired {
			return false
		}
	case experienceTraceQueryFilterReviewed:
		if !isExperienceReviewResolvedStatus(detail.ReviewStatus) {
			return false
		}
	case experienceTraceQueryFilterActions:
		if strings.TrimSpace(detail.NextActionKind) == "" && strings.TrimSpace(detail.NextAction) == "" {
			return false
		}
	case experienceTraceQueryFilterManualActions:
		if kind := normalizeExperienceGovernanceActionKind(actionKindForExperienceTraceDetail(detail)); kind == experienceGovernanceActionUnknown || kind == experienceGovernanceActionReviewSignal {
			return false
		}
	case experienceTraceQueryFilterFollowUps:
		if strings.TrimSpace(detail.FollowUpStatus) == "" {
			return false
		}
	case experienceTraceQueryFilterA2A:
		if !normalizeExperienceTraceKind(detail.Kind).IsA2A() && detail.SourceType != groupDiscussionMemorySourceType {
			return false
		}
	case experienceTraceQueryFilterTools:
		if !normalizeExperienceTraceKind(detail.Kind).IsToolLearning() && !normalizeExperienceTraceSourceType(detail.SourceType).IsToolUsage() {
			return false
		}
	case experienceTraceQueryFilterSessions:
		if normalizeExperienceTraceKind(detail.Kind) != experienceTraceKindSessionHistory && !normalizeExperienceTraceSourceType(detail.SourceType).IsSessionHistory() {
			return false
		}
	case experienceTraceQueryFilterAll:
	case experienceTraceQueryFilterUnknown:
		return false
	}
	if req.ReviewStatus != "" && strings.ToLower(experienceTraceReviewSummaryStatus(detail).String()) != req.ReviewStatus {
		return false
	}
	if req.ActionKind != "" && actionKindForExperienceTraceDetail(detail) != req.ActionKind {
		return false
	}
	if req.FollowUpStatus != "" && strings.ToLower(strings.TrimSpace(detail.FollowUpStatus)) != req.FollowUpStatus {
		return false
	}
	if req.FollowUpActionKind != "" && strings.TrimSpace(detail.FollowUpActionKind) != req.FollowUpActionKind {
		return false
	}
	if req.TriggeredRollbackOnly && !experienceTriggeredRollbackEvidence(detail) {
		return false
	}
	if req.Kind != "" && !strings.EqualFold(strings.TrimSpace(detail.Kind), req.Kind) {
		return false
	}
	if req.SourceType != "" && !strings.EqualFold(strings.TrimSpace(detail.SourceType), req.SourceType) {
		return false
	}
	if req.TraceID != "" && strings.TrimSpace(detail.ID) != req.TraceID {
		return false
	}
	if req.SourceTraceID != "" && strings.TrimSpace(detail.SourceTraceID) != req.SourceTraceID {
		return false
	}
	if req.Query != "" && !experienceTraceDetailContains(detail, req.Query) {
		return false
	}
	return true
}

func isExperienceReviewResolvedStatus(status string) bool {
	return status == experienceReviewOutcomeApproved || status == experienceReviewOutcomeRejected
}

func actionKindForExperienceTraceDetail(detail ExperienceTraceDetail) string {
	if kind := strings.TrimSpace(detail.NextActionKind); kind != "" {
		return kind
	}
	if strings.TrimSpace(detail.NextAction) != "" {
		return experienceGovernanceActionManualFollowUp.String()
	}
	return ""
}

func experienceTraceDetailContains(detail ExperienceTraceDetail, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	values := []string{detail.ID, detail.Kind, detail.Title, detail.Summary, detail.Detail, detail.SourceType, detail.SourceURL, detail.SourceTraceID, detail.Impact, detail.ReviewAction, detail.NextAction, detail.Reviewer, detail.ReviewNote, detail.FollowUpActor, detail.FollowUpNote}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	for _, tag := range detail.Tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}
	return false
}

func experienceLearningToolResult(payload map[string]interface{}, err error) string {
	if payload == nil {
		payload = map[string]interface{}{}
	}
	if err != nil {
		payload["ok"] = false
		payload["error"] = err.Error()
	} else {
		payload["ok"] = true
	}
	normalizeExperienceLearningSafeHandoff(payload)
	data, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return fmt.Sprintf(`{"ok":false,"error":%q}`, marshalErr.Error())
	}
	return string(data)
}

func normalizeExperienceLearningSafeHandoff(payload map[string]interface{}) {
	if payload == nil {
		return
	}
	call, ok := payload["recommended_tool_call"].(map[string]interface{})
	if !ok || call == nil {
		return
	}
	payload["recommended_tool_call"] = normalizeExperienceLearningRecommendedToolCall(call, payload["recommended_focus_context"], payload["non_executing_boundary"])
}

func normalizeExperienceLearningRecommendedToolCall(call map[string]interface{}, focusContext interface{}, boundary interface{}) map[string]interface{} {
	if call == nil {
		return nil
	}
	normalized := make(map[string]interface{}, len(call)+3)
	for key, value := range call {
		normalized[key] = value
	}
	if _, ok := normalized["recommended_focus_context"]; !ok {
		if focusContext != nil {
			normalized["recommended_focus_context"] = focusContext
		}
	}
	if _, ok := normalized["governance_focus_context"]; !ok {
		if focus, ok := normalized["recommended_focus_context"]; ok && focus != nil {
			normalized["governance_focus_context"] = focus
		}
	}
	normalized["non_executing"] = true
	if boundaryValue, ok := normalized["non_executing_boundary"]; !ok || strings.TrimSpace(fmt.Sprint(boundaryValue)) == "" {
		if boundaryText := strings.TrimSpace(fmt.Sprint(boundary)); boundaryText != "" && boundaryText != "<nil>" {
			normalized["non_executing_boundary"] = boundaryText
		}
	}
	return normalized
}

func finalizeExperienceMemoryCandidateResult(result ExperienceMemoryCandidateResult) ExperienceMemoryCandidateResult {
	result.RecommendedToolCall = normalizeExperienceLearningRecommendedToolCall(result.RecommendedToolCall, result.RecommendedFocusContext, result.NonExecutingBoundary)
	return result
}

func finalizeExperienceMemoryMaintenanceDraft(draft ExperienceMemoryMaintenanceDraft) ExperienceMemoryMaintenanceDraft {
	draft.RecommendedToolCall = normalizeExperienceLearningRecommendedToolCall(draft.RecommendedToolCall, draft.RecommendedFocusContext, draft.NonExecutingBoundary)
	return draft
}

func finalizeExperienceRoutingSignalResult(result ExperienceRoutingSignalResult) ExperienceRoutingSignalResult {
	result.RecommendedToolCall = normalizeExperienceLearningRecommendedToolCall(result.RecommendedToolCall, result.RecommendedFocusContext, result.NonExecutingBoundary)
	return result
}

func finalizeExperienceRoutingAdjustmentDraft(draft ExperienceRoutingAdjustmentDraft) ExperienceRoutingAdjustmentDraft {
	draft.RecommendedToolCall = normalizeExperienceLearningRecommendedToolCall(draft.RecommendedToolCall, draft.RecommendedFocusContext, draft.NonExecutingBoundary)
	return draft
}

func finalizeExperienceTraceDetailQueryResult(result ExperienceTraceDetailQueryResult) ExperienceTraceDetailQueryResult {
	result.RecommendedToolCall = normalizeExperienceLearningRecommendedToolCall(result.RecommendedToolCall, result.RecommendedFocusContext, result.NonExecutingBoundary)
	return result
}

func finalizeExperienceFollowUpActionAuditResult(result ExperienceFollowUpActionAuditResult) ExperienceFollowUpActionAuditResult {
	result.RecommendedToolCall = normalizeExperienceLearningRecommendedToolCall(result.RecommendedToolCall, result.RecommendedFocusContext, result.NonExecutingBoundary)
	return result
}
