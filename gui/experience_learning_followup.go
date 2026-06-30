package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	experienceFollowUpOutcomeCompleted = "completed"
	experienceFollowUpOutcomeBlocked   = "blocked"
	experienceFollowUpOutcomeDeferred  = "deferred"
	experienceFollowUpResolvedTag      = "followup_resolved"
	experienceFollowUpStatusTagPrefix  = "followup_status:"
	experienceDraftReviewTag           = "experience_draft_review"
	experienceDraftKindMaintenance     = "memory_maintenance_draft"
	experienceDraftKindRouting         = "routing_adjustment_draft"
	experienceDraftKindSkill           = "skill_draft"
	experienceDraftKindRollback        = "rollback_workflow_draft"
	experienceDraftKindEscalation      = "escalation_brief"
	experienceDraftKindConflict        = "conflict_reconciliation_draft"
	experienceLearningSourceType       = "experience_learning"
	skillDraftExecutionStatusTagPrefix = "skill_draft_execution_status:"
	skillDraftExecutionAtTagPrefix     = "skill_draft_execution_at:"
	skillDraftExecutionPreviewed       = "previewed"
	skillDraftExecutionApplied         = "applied"
	skillDraftExecutionBlocked         = "blocked"
	skillDraftExecutionReopened        = "reopened"
	skillDraftExecutionClosed          = "closed"
)

type ExperienceTraceFollowUpDraft struct {
	TraceID                 string                 `json:"trace_id"`
	Kind                    string                 `json:"kind"`
	Title                   string                 `json:"title"`
	SourceURL               string                 `json:"source_url,omitempty"`
	RecommendedFocusContext map[string]interface{} `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall     map[string]interface{} `json:"recommended_tool_call,omitempty"`
	NonExecutingBoundary    string                 `json:"non_executing_boundary,omitempty"`
	ActionKind              string                 `json:"action_kind"`
	Action                  string                 `json:"action"`
	DraftTitle              string                 `json:"draft_title"`
	Draft                   string                 `json:"draft"`
	Checks                  []string               `json:"checks"`
}

type ExperienceSkillDraft struct {
	TraceID                 string                 `json:"trace_id"`
	Kind                    string                 `json:"kind"`
	Title                   string                 `json:"title"`
	SourceURL               string                 `json:"source_url,omitempty"`
	RecommendedFocusContext map[string]interface{} `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall     map[string]interface{} `json:"recommended_tool_call,omitempty"`
	SuggestedName           string                 `json:"suggested_name"`
	TaskType                string                 `json:"task_type,omitempty"`
	QueryTokens             []string               `json:"query_tokens,omitempty"`
	ToolSequence            []string               `json:"tool_sequence"`
	Evidence                string                 `json:"evidence,omitempty"`
	Description             string                 `json:"description,omitempty"`
	DraftMarkdown           string                 `json:"draft_markdown"`
	Checks                  []string               `json:"checks"`
	NonExecutingBoundary    string                 `json:"non_executing_boundary"`
}

type ExperienceBlockedSkillDraft struct {
	TraceID                 string                       `json:"trace_id"`
	Kind                    string                       `json:"kind"`
	Title                   string                       `json:"title"`
	SourceTraceID           string                       `json:"source_trace_id,omitempty"`
	DraftID                 string                       `json:"draft_id"`
	ExecutionStatus         string                       `json:"execution_status"`
	ExecutionNote           string                       `json:"execution_note,omitempty"`
	CurrentPlanMatched      bool                         `json:"current_plan_matched"`
	CurrentPlanActions      []map[string]string          `json:"current_plan_actions"`
	ReviewOptions           map[string]interface{}       `json:"review_options,omitempty"`
	ReviewAffordances       []ExperienceReviewAffordance `json:"review_affordances,omitempty"`
	RecommendedFocusContext map[string]interface{}       `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall     map[string]interface{}       `json:"recommended_tool_call,omitempty"`
	DraftMarkdown           string                       `json:"draft_markdown"`
	Checks                  []string                     `json:"checks"`
	NonExecutingBoundary    string                       `json:"non_executing_boundary"`
}

type ExperienceReviewAffordance struct {
	ID                   string                            `json:"id"`
	Label                string                            `json:"label"`
	Intent               string                            `json:"intent"`
	Variant              string                            `json:"variant,omitempty"`
	Description          string                            `json:"description,omitempty"`
	RequiredInputs       []ExperienceReviewAffordanceInput `json:"required_inputs,omitempty"`
	ToolCall             map[string]interface{}            `json:"tool_call,omitempty"`
	NonExecutingBoundary string                            `json:"non_executing_boundary"`
}

type ExperienceReviewAffordanceInput struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Placeholder string `json:"placeholder,omitempty"`
}

type ExperienceRollbackWorkflowDraft struct {
	TraceID                 string                 `json:"trace_id"`
	Kind                    string                 `json:"kind"`
	Title                   string                 `json:"title"`
	SourceURL               string                 `json:"source_url,omitempty"`
	RecommendedFocusContext map[string]interface{} `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall     map[string]interface{} `json:"recommended_tool_call,omitempty"`
	DecisionSummary         string                 `json:"decision_summary,omitempty"`
	DecisionRationale       string                 `json:"decision_rationale,omitempty"`
	RollbackTriggers        []string               `json:"rollback_triggers"`
	DraftMarkdown           string                 `json:"draft_markdown"`
	Checks                  []string               `json:"checks"`
	NonExecutingBoundary    string                 `json:"non_executing_boundary"`
}

type ExperienceEscalationBrief struct {
	TraceID                 string                 `json:"trace_id"`
	Kind                    string                 `json:"kind"`
	Title                   string                 `json:"title"`
	SourceURL               string                 `json:"source_url,omitempty"`
	RecommendedFocusContext map[string]interface{} `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall     map[string]interface{} `json:"recommended_tool_call,omitempty"`
	Reason                  string                 `json:"reason,omitempty"`
	Target                  string                 `json:"target,omitempty"`
	RaisedBy                string                 `json:"raised_by,omitempty"`
	BriefMarkdown           string                 `json:"brief_markdown"`
	Checks                  []string               `json:"checks"`
	NonExecutingBoundary    string                 `json:"non_executing_boundary"`
}

type ExperienceConflictReconciliationDraft struct {
	TraceID                 string                 `json:"trace_id"`
	Kind                    string                 `json:"kind"`
	Title                   string                 `json:"title"`
	SourceURL               string                 `json:"source_url,omitempty"`
	RecommendedFocusContext map[string]interface{} `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall     map[string]interface{} `json:"recommended_tool_call,omitempty"`
	Topic                   string                 `json:"topic,omitempty"`
	Question                string                 `json:"question,omitempty"`
	NewDiscussion           string                 `json:"new_discussion,omitempty"`
	NewSummary              string                 `json:"new_summary,omitempty"`
	ExistingMemory          string                 `json:"existing_memory,omitempty"`
	ExistingSummary         string                 `json:"existing_summary,omitempty"`
	DraftMarkdown           string                 `json:"draft_markdown"`
	Checks                  []string               `json:"checks"`
	NonExecutingBoundary    string                 `json:"non_executing_boundary"`
}

type ExperienceTraceFollowUpRequest struct {
	Status string `json:"status"`
	Note   string `json:"note,omitempty"`
	Actor  string `json:"actor,omitempty"`
}

type ExperienceTraceFollowUpRecord struct {
	TraceID                 string                 `json:"trace_id"`
	MemoryID                string                 `json:"memory_id"`
	Status                  string                 `json:"status"`
	ActionKind              string                 `json:"action_kind,omitempty"`
	RecommendedFocusContext map[string]interface{} `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall     map[string]interface{} `json:"recommended_tool_call,omitempty"`
	NonExecutingBoundary    string                 `json:"non_executing_boundary,omitempty"`
}

type ExperienceDraftReviewRequest struct {
	Kind                 string `json:"kind"`
	Status               string `json:"status"`
	SourceTraceID        string `json:"source_trace_id,omitempty"`
	DraftID              string `json:"draft_id,omitempty"`
	Query                string `json:"query,omitempty"`
	Note                 string `json:"note,omitempty"`
	Actor                string `json:"actor,omitempty"`
	DraftMarkdown        string `json:"draft_markdown,omitempty"`
	NonExecutingBoundary string `json:"non_executing_boundary,omitempty"`
}

type ExperienceDraftReviewRecord struct {
	TraceID                 string                 `json:"trace_id"`
	MemoryID                string                 `json:"memory_id"`
	Kind                    string                 `json:"kind"`
	Status                  string                 `json:"status"`
	SourceTraceID           string                 `json:"source_trace_id,omitempty"`
	DraftID                 string                 `json:"draft_id,omitempty"`
	RecommendedFocusContext map[string]interface{} `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall     map[string]interface{} `json:"recommended_tool_call,omitempty"`
	NonExecutingBoundary    string                 `json:"non_executing_boundary,omitempty"`
}

type blockedSkillDraftReviewResolutionUpdate struct {
	Entry    memory.Entry
	Required bool
}

func (a *App) BuildExperienceTraceFollowUp(traceID string) (ExperienceTraceFollowUpDraft, error) {
	if a.memoryStore == nil {
		a.ensureInteractionInfra()
		a.ensureMemoryStore()
	}
	if a.memoryStore == nil {
		return ExperienceTraceFollowUpDraft{}, fmt.Errorf("memory store not initialized")
	}
	entry, err := findExperienceMemoryEntryByTraceID(a.memoryStore, traceID)
	if err != nil {
		return ExperienceTraceFollowUpDraft{}, err
	}
	detail, ok := traceDetailFromMemoryEntry(entry)
	if !ok {
		return ExperienceTraceFollowUpDraft{}, fmt.Errorf("experience trace %q is not available", traceID)
	}
	if strings.TrimSpace(detail.NextActionKind) == "" || strings.TrimSpace(detail.NextAction) == "" {
		return ExperienceTraceFollowUpDraft{}, fmt.Errorf("experience trace %q has no follow-up action", traceID)
	}
	nextActionKind := normalizeExperienceGovernanceActionKind(detail.NextActionKind)
	if detail.ReviewRequired || nextActionKind.IsReviewSignal() {
		return ExperienceTraceFollowUpDraft{}, fmt.Errorf("experience trace %q must be reviewed before drafting a follow-up", traceID)
	}
	return finalizeExperienceTraceFollowUpDraft(buildExperienceTraceFollowUpDraft(detail)), nil
}

func (a *App) BuildExperienceSkillDraft(traceID string) (ExperienceSkillDraft, error) {
	if a.memoryStore == nil {
		a.ensureInteractionInfra()
		a.ensureMemoryStore()
	}
	if a.memoryStore == nil {
		return ExperienceSkillDraft{}, fmt.Errorf("memory store not initialized")
	}
	entry, _, err := findExperienceReviewMemoryEntry(a.memoryStore, traceID)
	if err != nil {
		return ExperienceSkillDraft{}, err
	}
	detail, ok := traceDetailFromMemoryEntry(entry)
	if !ok {
		return ExperienceSkillDraft{}, fmt.Errorf("experience trace %q is not available", traceID)
	}
	if normalizeExperienceTraceKind(detail.Kind) != experienceTraceKindSkillNudgeReview {
		return ExperienceSkillDraft{}, fmt.Errorf("experience trace %q is not a skill nudge review", traceID)
	}
	nextActionKind := normalizeExperienceGovernanceActionKind(detail.NextActionKind)
	if detail.ReviewRequired || detail.ReviewStatus != experienceReviewOutcomeApproved || nextActionKind != experienceGovernanceActionDraftSkillManually {
		return ExperienceSkillDraft{}, fmt.Errorf("experience trace %q must be approved and queued for manual skill drafting", traceID)
	}
	if experienceIsMergedBrowserOnlySequence(experienceSkillDraftSequence(detail.Detail, detail.Tags)) {
		return ExperienceSkillDraft{}, fmt.Errorf("experience trace %q is a legacy browser automation skill nudge; use the built-in browser tool instead of drafting a browser skill", traceID)
	}
	return finalizeExperienceSkillDraft(buildExperienceSkillDraft(detail)), nil
}

func (a *App) BuildExperienceSkillDraftFromUsageNudge(req ExperienceRoutingSignalQuery) ExperienceSkillDraft {
	result := ExperienceRoutingSignalResult{Query: normalizeExperienceRoutingSignalQuery(req)}
	if a != nil {
		result = a.QueryExperienceRoutingSignals(req)
	}
	var candidate *coretool.ToolSkillNudgeCandidate
	if len(result.SkillNudgeCandidates) > 0 {
		candidate = &result.SkillNudgeCandidates[0]
		if experienceIsMergedBrowserOnlySequence(candidate.ToolSequence) {
			candidate = nil
		}
	}
	return finalizeExperienceSkillDraft(buildExperienceSkillDraftFromUsageNudge(result.Query, candidate))
}

func (a *App) BuildExperienceBlockedSkillDraft(traceID string) (ExperienceBlockedSkillDraft, error) {
	if a.memoryStore == nil {
		a.ensureInteractionInfra()
		a.ensureMemoryStore()
	}
	if a.memoryStore == nil {
		return ExperienceBlockedSkillDraft{}, fmt.Errorf("memory store not initialized")
	}
	entry, err := findExperienceMemoryEntryByTraceID(a.memoryStore, traceID)
	if err != nil {
		return ExperienceBlockedSkillDraft{}, err
	}
	detail, ok := traceDetailFromMemoryEntry(entry)
	if !ok {
		return ExperienceBlockedSkillDraft{}, fmt.Errorf("experience trace %q is not available", traceID)
	}
	if detail.Kind != "skill_draft_review" || detail.FollowUpStatus != experienceFollowUpOutcomeCompleted || strings.TrimSpace(detail.DraftID) == "" {
		return ExperienceBlockedSkillDraft{}, fmt.Errorf("experience trace %q is not a completed skill draft review with draft id", traceID)
	}
	if detail.DraftExecutionStatus != skillDraftExecutionBlocked {
		return ExperienceBlockedSkillDraft{}, fmt.Errorf("experience trace %q is not a blocked skill draft execution", traceID)
	}
	return finalizeExperienceBlockedSkillDraft(a.buildExperienceBlockedSkillDraft(detail)), nil
}

func (a *App) RecordBlockedSkillDraftReview(traceID, resolution, replacementDraftID, note, actor string) (ExperienceDraftReviewRecord, error) {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return ExperienceDraftReviewRecord{}, fmt.Errorf("trace_id is required")
	}
	if a.memoryStore == nil {
		a.ensureInteractionInfra()
		a.ensureMemoryStore()
	}
	if a.memoryStore == nil {
		return ExperienceDraftReviewRecord{}, fmt.Errorf("memory store not initialized")
	}
	entry, err := findExperienceMemoryEntryByTraceID(a.memoryStore, traceID)
	if err != nil {
		return ExperienceDraftReviewRecord{}, err
	}
	detail, ok := traceDetailFromMemoryEntry(entry)
	if !ok || detail.Kind != "skill_draft_review" || detail.DraftExecutionStatus != skillDraftExecutionBlocked {
		return ExperienceDraftReviewRecord{}, fmt.Errorf("experience trace %q is not a blocked skill draft execution", traceID)
	}
	resolution = strings.ToLower(strings.TrimSpace(resolution))
	req := ExperienceDraftReviewRequest{
		Kind:          experienceDraftKindSkill,
		SourceTraceID: traceID,
		Actor:         actor,
	}
	switch resolution {
	case "reopen", "replace", "replacement":
		replacementDraftID = strings.TrimSpace(replacementDraftID)
		if replacementDraftID == "" {
			return ExperienceDraftReviewRecord{}, fmt.Errorf("replacement_draft_id is required when resolution=reopen")
		}
		req.Status = experienceFollowUpOutcomeCompleted
		req.DraftID = replacementDraftID
		req.Note = firstNonEmptyExperienceString(note, "repair evidence accepted; reopen dry preview with replacement draft")
	case "close", "closed", "reject", "stale":
		req.Status = experienceFollowUpOutcomeBlocked
		req.Note = firstNonEmptyExperienceString(note, "blocked skill draft approval closed as stale or rejected; do not retry without a new governance draft")
	default:
		return ExperienceDraftReviewRecord{}, fmt.Errorf("resolution must be reopen or close")
	}
	return a.RecordExperienceDraftReview(req)
}

func (a *App) ConfirmPreviewedSkillDraftReview(traceID string) (map[string]interface{}, error) {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return nil, fmt.Errorf("trace_id is required")
	}
	if a.memoryStore == nil {
		a.ensureInteractionInfra()
		a.ensureMemoryStore()
	}
	if a.memoryStore == nil {
		return nil, fmt.Errorf("memory store not initialized")
	}
	entry, err := findExperienceMemoryEntryByTraceID(a.memoryStore, traceID)
	if err != nil {
		return nil, err
	}
	detail, ok := traceDetailFromMemoryEntry(entry)
	if !ok || detail.Kind != "skill_draft_review" || detail.FollowUpStatus != experienceFollowUpOutcomeCompleted || strings.TrimSpace(detail.DraftID) == "" {
		return nil, fmt.Errorf("experience trace %q is not a completed skill draft review with draft id", traceID)
	}
	if detail.DraftExecutionStatus != skillDraftExecutionPreviewed {
		return nil, fmt.Errorf("experience trace %q is not waiting for preview confirmation", traceID)
	}
	raw := (&IMMessageHandler{app: a}).toolManageSkill(context.Background(), map[string]interface{}{
		"action":                    "execute_maintenance_plan",
		"dry_run":                   false,
		"confirm":                   true,
		"approved_review_trace_ids": []interface{}{traceID},
	}, nil)
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("confirm previewed skill draft review failed: %s", raw)
	}
	payload["review_trace_id"] = traceID
	if refreshed, err := findExperienceMemoryEntryByTraceID(a.memoryStore, traceID); err == nil {
		if refreshedDetail, ok := traceDetailFromMemoryEntry(refreshed); ok {
			payload["draft_execution_status"] = refreshedDetail.DraftExecutionStatus
			payload["draft_execution_queue"] = skillDraftExecutionQueueName(refreshedDetail.DraftExecutionStatus)
			payload["draft_id"] = refreshedDetail.DraftID
		}
	}
	return payload, nil
}

func (a *App) BuildExperienceRollbackWorkflowDraft(traceID string) (ExperienceRollbackWorkflowDraft, error) {
	if a.memoryStore == nil {
		a.ensureInteractionInfra()
		a.ensureMemoryStore()
	}
	if a.memoryStore == nil {
		return ExperienceRollbackWorkflowDraft{}, fmt.Errorf("memory store not initialized")
	}
	entry, _, err := findExperienceReviewMemoryEntry(a.memoryStore, traceID)
	if err != nil {
		return ExperienceRollbackWorkflowDraft{}, err
	}
	detail, ok := traceDetailFromMemoryEntry(entry)
	if !ok {
		return ExperienceRollbackWorkflowDraft{}, fmt.Errorf("experience trace %q is not available", traceID)
	}
	if normalizeExperienceTraceKind(detail.Kind) != experienceTraceKindA2ARollbackReview {
		return ExperienceRollbackWorkflowDraft{}, fmt.Errorf("experience trace %q is not an A2A rollback review", traceID)
	}
	nextActionKind := normalizeExperienceGovernanceActionKind(detail.NextActionKind)
	if detail.ReviewRequired || detail.ReviewStatus != experienceReviewOutcomeApproved || nextActionKind != experienceGovernanceActionDraftRollbackWorkflow {
		return ExperienceRollbackWorkflowDraft{}, fmt.Errorf("experience trace %q must be approved and queued for manual rollback workflow drafting", traceID)
	}
	return finalizeExperienceRollbackWorkflowDraft(buildExperienceRollbackWorkflowDraft(detail)), nil
}

func (a *App) BuildExperienceEscalationBrief(traceID string) (ExperienceEscalationBrief, error) {
	if a.memoryStore == nil {
		a.ensureInteractionInfra()
		a.ensureMemoryStore()
	}
	if a.memoryStore == nil {
		return ExperienceEscalationBrief{}, fmt.Errorf("memory store not initialized")
	}
	entry, err := findExperienceMemoryEntryByTraceID(a.memoryStore, traceID)
	if err != nil {
		return ExperienceEscalationBrief{}, err
	}
	detail, ok := traceDetailFromMemoryEntry(entry)
	if !ok {
		return ExperienceEscalationBrief{}, fmt.Errorf("experience trace %q is not available", traceID)
	}
	if normalizeExperienceTraceKind(detail.Kind) != experienceTraceKindA2AEscalationEvidence {
		return ExperienceEscalationBrief{}, fmt.Errorf("experience trace %q is not A2A escalation evidence", traceID)
	}
	nextActionKind := normalizeExperienceGovernanceActionKind(detail.NextActionKind)
	if detail.ReviewRequired || nextActionKind != experienceGovernanceActionPrepareEscalationBrief {
		return ExperienceEscalationBrief{}, fmt.Errorf("experience trace %q is not queued for escalation handoff briefing", traceID)
	}
	return finalizeExperienceEscalationBrief(buildExperienceEscalationBrief(detail)), nil
}

func (a *App) BuildExperienceConflictReconciliationDraft(traceID string) (ExperienceConflictReconciliationDraft, error) {
	if a.memoryStore == nil {
		a.ensureInteractionInfra()
		a.ensureMemoryStore()
	}
	if a.memoryStore == nil {
		return ExperienceConflictReconciliationDraft{}, fmt.Errorf("memory store not initialized")
	}
	entry, err := findExperienceMemoryEntryByTraceID(a.memoryStore, traceID)
	if err != nil {
		return ExperienceConflictReconciliationDraft{}, err
	}
	detail, ok := traceDetailFromMemoryEntry(entry)
	if !ok {
		return ExperienceConflictReconciliationDraft{}, fmt.Errorf("experience trace %q is not available", traceID)
	}
	if normalizeExperienceTraceKind(detail.Kind) != experienceTraceKindA2AConflictReview {
		return ExperienceConflictReconciliationDraft{}, fmt.Errorf("experience trace %q is not an A2A conflict review", traceID)
	}
	nextActionKind := normalizeExperienceGovernanceActionKind(detail.NextActionKind)
	if detail.ReviewRequired || detail.ReviewStatus != experienceReviewOutcomeApproved || nextActionKind != experienceGovernanceActionResolveA2AConflictManually {
		return ExperienceConflictReconciliationDraft{}, fmt.Errorf("experience trace %q must be approved and queued for manual conflict reconciliation", traceID)
	}
	return finalizeExperienceConflictReconciliationDraft(buildExperienceConflictReconciliationDraft(detail)), nil
}

func findExperienceMemoryEntryByTraceID(store *memory.Store, traceID string) (memory.Entry, error) {
	if store == nil {
		return memory.Entry{}, fmt.Errorf("memory store not initialized")
	}
	traceID = strings.TrimSpace(traceID)
	if !strings.HasPrefix(traceID, "memory:") {
		return memory.Entry{}, fmt.Errorf("only memory-backed experience traces are supported")
	}
	memoryID := strings.TrimSpace(strings.TrimPrefix(traceID, "memory:"))
	if memoryID == "" {
		return memory.Entry{}, fmt.Errorf("memory trace id is empty")
	}
	for _, entry := range store.List("", "") {
		if entry.ID == memoryID {
			return entry, nil
		}
	}
	return memory.Entry{}, fmt.Errorf("experience trace %q not found", traceID)
}

func (a *App) experienceRecommendedFocusContextForTrace(traceID, reason string) map[string]interface{} {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return nil
	}
	title := ""
	if a.memoryStore == nil {
		a.ensureInteractionInfra()
		a.ensureMemoryStore()
	}
	if a.memoryStore != nil {
		if entry, err := findExperienceMemoryEntryByTraceID(a.memoryStore, traceID); err == nil {
			if detail, ok := traceDetailFromMemoryEntry(entry); ok {
				title = detail.Title
			} else {
				title = entry.Title
			}
		}
	}
	return experienceFocusContextFromTraceTarget(traceID, title, reason)
}

func (a *App) RecordExperienceTraceFollowUp(traceID string, req ExperienceTraceFollowUpRequest) (ExperienceTraceFollowUpRecord, error) {
	status, err := normalizeExperienceFollowUpOutcome(req.Status)
	if err != nil {
		return ExperienceTraceFollowUpRecord{}, err
	}
	if a.memoryStore == nil {
		a.ensureInteractionInfra()
		a.ensureMemoryStore()
	}
	if a.memoryStore == nil {
		return ExperienceTraceFollowUpRecord{}, fmt.Errorf("memory store not initialized")
	}
	entry, err := findExperienceMemoryEntryByTraceID(a.memoryStore, traceID)
	if err != nil {
		return ExperienceTraceFollowUpRecord{}, err
	}
	detail, ok := traceDetailFromMemoryEntry(entry)
	if !ok {
		return ExperienceTraceFollowUpRecord{}, fmt.Errorf("experience trace %q is not available", traceID)
	}
	nextActionKind := normalizeExperienceGovernanceActionKind(detail.NextActionKind)
	if detail.ReviewRequired || nextActionKind == experienceGovernanceActionUnknown || nextActionKind.IsReviewSignal() {
		return ExperienceTraceFollowUpRecord{}, fmt.Errorf("experience trace %q has no recordable follow-up action", traceID)
	}
	now := time.Now().UTC()
	entry.Content = appendExperienceFollowUpRecord(entry.Content, status, detail.NextActionKind, req.Note, a.defaultExperienceReviewReviewer(req.Actor), now)
	entry.Tags = applyExperienceFollowUpTags(entry.Tags, status, now)
	if _, err := a.memoryStore.UpsertProjectKnowledge(memory.ProjectKnowledgeUpsertOptions{
		ID:          entry.ID,
		Title:       entry.Title,
		Content:     entry.Content,
		Tags:        entry.Tags,
		Scope:       entry.Scope,
		OwnerID:     entry.OwnerID,
		SourceType:  entry.SourceType,
		SourceURL:   entry.SourceURL,
		EvidenceIDs: entry.EvidenceIDs,
		RelatedIDs:  entry.RelatedIDs,
		DerivedKind: entry.DerivedKind,
		Boundary:    entry.Boundary,
	}); err != nil {
		return ExperienceTraceFollowUpRecord{}, err
	}
	a.emitEvent("memory:experience-followup-recorded", map[string]string{
		"trace_id":  traceID,
		"memory_id": entry.ID,
		"status":    status,
	})
	focusContext := experienceFocusContextFromTraceTarget(traceID, entry.Title, "follow-up audit recorded for priority experience trace")
	return finalizeExperienceTraceFollowUpRecord(ExperienceTraceFollowUpRecord{
		TraceID:                 traceID,
		MemoryID:                entry.ID,
		Status:                  status,
		ActionKind:              detail.NextActionKind,
		RecommendedFocusContext: focusContext,
		RecommendedToolCall:     experienceTraceInspectionRecommendedToolCall(traceID, entry.Title, "follow-up audit recorded for priority experience trace"),
		NonExecutingBoundary:    "follow-up audit record only; no queued draft was executed, no rollback ran, no memory was rewritten beyond audit evidence, no routing changed, no files were written, no tools were run, no notifications were sent, and no skills were installed",
	}), nil
}

// RecordExperienceDraftReview stores a manual review outcome for an overall
// experience-learning draft. It creates audit evidence only; no draft is
// executed, no memory is rewritten, and no router or skill state is changed.
func (a *App) RecordExperienceDraftReview(req ExperienceDraftReviewRequest) (ExperienceDraftReviewRecord, error) {
	kind, err := normalizeExperienceDraftReviewKind(req.Kind)
	if err != nil {
		return ExperienceDraftReviewRecord{}, err
	}
	status, err := normalizeExperienceFollowUpOutcome(req.Status)
	if err != nil {
		return ExperienceDraftReviewRecord{}, err
	}
	if a.memoryStore == nil {
		a.ensureInteractionInfra()
		a.ensureMemoryStore()
	}
	if a.memoryStore == nil {
		return ExperienceDraftReviewRecord{}, fmt.Errorf("memory store not initialized")
	}
	now := time.Now().UTC()
	sourceTraceID := strings.TrimSpace(req.SourceTraceID)
	draftID := strings.TrimSpace(req.DraftID)
	resolutionUpdate, err := a.prepareBlockedSkillDraftReviewResolution(sourceTraceID, status, draftID, req.Note, now)
	if err != nil {
		return ExperienceDraftReviewRecord{}, err
	}
	memoryID := fmt.Sprintf("experience-draft-%s-%d", strings.ReplaceAll(kind, "_", "-"), now.UnixNano())
	content := appendExperienceFollowUpRecord(buildExperienceDraftReviewContent(req, kind), status, kind, req.Note, a.defaultExperienceReviewReviewer(req.Actor), now)
	tags := applyExperienceFollowUpTags([]string{experienceDraftReviewTag, kind, "non_executing_draft_review"}, status, now)
	reviewEntry := memory.NewProjectKnowledgeEntry(memory.ProjectKnowledgeUpsertOptions{
		ID:         memoryID,
		Title:      "Experience draft review: " + experienceDraftReviewTitle(kind),
		Content:    content,
		Tags:       tags,
		SourceType: experienceLearningSourceType,
		SourceURL:  "experience://draft/" + kind,
	})
	if resolutionUpdate.Required {
		if err := a.memoryStore.UpsertEntriesByID([]memory.Entry{reviewEntry, resolutionUpdate.Entry}); err != nil {
			return ExperienceDraftReviewRecord{}, err
		}
	} else {
		if _, err := a.memoryStore.UpsertProjectKnowledge(memory.ProjectKnowledgeUpsertOptions{
			ID:         memoryID,
			Title:      reviewEntry.Title,
			Content:    content,
			Tags:       tags,
			SourceType: experienceLearningSourceType,
			SourceURL:  reviewEntry.SourceURL,
		}); err != nil {
			return ExperienceDraftReviewRecord{}, err
		}
	}
	traceID := "memory:" + memoryID
	a.emitEvent("memory:experience-draft-reviewed", map[string]string{
		"trace_id": traceID,
		"kind":     kind,
		"status":   status,
	})
	return finalizeExperienceDraftReviewRecord(ExperienceDraftReviewRecord{
		TraceID:                 traceID,
		MemoryID:                memoryID,
		Kind:                    kind,
		Status:                  status,
		SourceTraceID:           sourceTraceID,
		DraftID:                 draftID,
		RecommendedFocusContext: a.experienceRecommendedFocusContextForTrace(sourceTraceID, "manual draft review recorded for source experience trace"),
		RecommendedToolCall:     experienceDraftReviewRecordRecommendedToolCall(kind, status, draftID, "memory:"+memoryID),
		NonExecutingBoundary:    "draft review audit record only; the reviewed draft was not executed, memory was not rewritten beyond audit evidence, routing was not changed, files were not written, tools were not run, rollback was not executed, notifications were not sent, and skills were not installed",
	}), nil
}

func experienceDraftReviewRecordRecommendedToolCall(kind, status, draftID, traceID string) map[string]interface{} {
	if kind == experienceDraftKindSkill && status == experienceFollowUpOutcomeCompleted && strings.TrimSpace(draftID) != "" {
		return map[string]interface{}{
			"tool": "manage_skill",
			"args": map[string]interface{}{
				"action":                    "execute_maintenance_plan",
				"dry_run":                   true,
				"approved_review_trace_ids": []string{strings.TrimSpace(traceID)},
			},
			"recommended_focus_context": map[string]interface{}{
				"priority_trace_id": traceID,
				"draft_id":          strings.TrimSpace(draftID),
				"reason":            "approved skill governance draft is ready for explicit maintenance execution preview",
			},
			"non_executing":          true,
			"non_executing_boundary": "recommended dry-run only; caller must explicitly run manage_skill with confirm=true and approved_review_trace_ids before any skill metadata changes",
		}
	}
	return experienceTraceInspectionRecommendedToolCall(traceID, "Experience draft review: "+experienceDraftReviewTitle(kind), "manual draft review audit record")
}

func skillDraftExecutionQueueName(status string) string {
	switch strings.TrimSpace(status) {
	case skillDraftExecutionPreviewed:
		return "previewed_waiting_confirm"
	case skillDraftExecutionApplied:
		return "applied"
	case skillDraftExecutionBlocked:
		return "blocked"
	case skillDraftExecutionReopened:
		return "reopened"
	case skillDraftExecutionClosed:
		return "closed"
	default:
		return "approved_unpreviewed"
	}
}

func (a *App) prepareBlockedSkillDraftReviewResolution(sourceTraceID, status, draftID, note string, now time.Time) (blockedSkillDraftReviewResolutionUpdate, error) {
	sourceTraceID = strings.TrimSpace(sourceTraceID)
	if sourceTraceID == "" || a == nil || a.memoryStore == nil {
		return blockedSkillDraftReviewResolutionUpdate{}, nil
	}
	entry, err := findExperienceMemoryEntryByTraceID(a.memoryStore, sourceTraceID)
	if err != nil {
		return blockedSkillDraftReviewResolutionUpdate{}, nil
	}
	detail, ok := traceDetailFromMemoryEntry(entry)
	if !ok || detail.Kind != "skill_draft_review" || detail.DraftExecutionStatus != skillDraftExecutionBlocked {
		return blockedSkillDraftReviewResolutionUpdate{}, nil
	}
	executionStatus := ""
	switch status {
	case experienceFollowUpOutcomeCompleted:
		if strings.TrimSpace(draftID) != "" {
			executionStatus = skillDraftExecutionReopened
		}
	case experienceFollowUpOutcomeBlocked:
		executionStatus = skillDraftExecutionClosed
	}
	if executionStatus == "" {
		return blockedSkillDraftReviewResolutionUpdate{}, nil
	}
	auditNote := firstNonEmptyExperienceString(note, "blocked skill draft repair/evidence review recorded")
	entry.Content = appendSkillDraftExecutionRecord(entry.Content, executionStatus, auditNote, "experience_learning", now)
	entry.Tags = applySkillDraftExecutionTags(entry.Tags, executionStatus, now)
	if err := memory.ScanForInjection(entry.Content); err != nil {
		return blockedSkillDraftReviewResolutionUpdate{}, fmt.Errorf("blocked skill draft source audit rejected: %w", err)
	}
	return blockedSkillDraftReviewResolutionUpdate{Entry: entry, Required: true}, nil
}

func normalizeExperienceFollowUpOutcome(value string) (string, error) {
	outcome := normalizeExperienceFollowUpOutcomeKind(value)
	if !outcome.IsKnown() {
		return "", fmt.Errorf("unknown follow-up status %q", value)
	}
	return outcome.String(), nil
}

func normalizeExperienceDraftReviewKind(value string) (string, error) {
	kind := normalizeExperienceDraftKind(value)
	if !kind.IsKnown() {
		return "", fmt.Errorf("unknown experience draft kind %q", value)
	}
	return kind.String(), nil
}

func experienceDraftReviewTitle(kind string) string {
	return experienceDraftKind(strings.TrimSpace(kind)).Title()
}

func experienceDraftReviewRecommendedToolCall(kind string, focusContext map[string]interface{}, boundary, sourceTraceID, query string) map[string]interface{} {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return nil
	}
	args := map[string]interface{}{
		"action":     "record_draft_review",
		"draft_kind": kind,
	}
	if sourceTraceID = strings.TrimSpace(sourceTraceID); sourceTraceID != "" {
		args["source_trace_id"] = sourceTraceID
	} else if focusContext != nil {
		if focusTraceID, _ := focusContext["priority_trace_id"].(string); strings.TrimSpace(focusTraceID) != "" {
			args["source_trace_id"] = strings.TrimSpace(focusTraceID)
		}
	}
	if query = strings.TrimSpace(query); query != "" {
		args["query"] = query
	}
	if boundary = strings.TrimSpace(boundary); boundary != "" {
		args["non_executing_boundary"] = boundary
	}
	if len(focusContext) > 0 {
		args["recommended_focus_context"] = focusContext
	}
	return normalizeExperienceLearningRecommendedToolCall(map[string]interface{}{
		"tool":                      "experience_learning",
		"args":                      args,
		"recommended_focus_context": focusContext,
		"non_executing":             true,
		"non_executing_boundary":    "draft review audit template only; caller must explicitly add status and note before any audit evidence can be recorded, and it must not execute the draft, rewrite memory, change routing, install skills, send notifications, run rollback, write files, or run tools",
	}, focusContext, "")
}

func buildExperienceDraftReviewContent(req ExperienceDraftReviewRequest, kind string) string {
	var b strings.Builder
	b.WriteString("Experience draft review evidence:")
	b.WriteString("\n- Kind: ")
	b.WriteString(kind)
	if sourceTraceID := truncateExperienceText(req.SourceTraceID, 240); sourceTraceID != "" {
		b.WriteString("\n- Source trace: ")
		b.WriteString(sourceTraceID)
	}
	if draftID := truncateExperienceText(req.DraftID, 240); draftID != "" {
		b.WriteString("\n- Draft id: ")
		b.WriteString(draftID)
	}
	if query := truncateExperienceText(req.Query, 240); query != "" {
		b.WriteString("\n- Query: ")
		b.WriteString(query)
	}
	if boundary := truncateExperienceText(req.NonExecutingBoundary, 500); boundary != "" {
		b.WriteString("\n- Boundary: ")
		b.WriteString(boundary)
	}
	if draft := truncateExperienceText(req.DraftMarkdown, 2400); draft != "" {
		b.WriteString("\n\nDraft excerpt:\n")
		b.WriteString(draft)
	}
	b.WriteString("\n\nSafety: draft review evidence only; no memory compression, memory rewrite, routing change, file write, skill install, notification, rollback, or tool execution was performed.")
	return strings.TrimSpace(b.String())
}

func buildExperienceConflictReconciliationDraft(detail ExperienceTraceDetail) ExperienceConflictReconciliationDraft {
	topic := experienceSkillDraftField(detail.Detail, "Topic:")
	question := experienceSkillDraftField(detail.Detail, "Question:")
	newDiscussion := experienceSkillDraftField(detail.Detail, "New discussion:")
	newSummary := experienceSkillDraftField(detail.Detail, "New summary:")
	existingMemory := experienceSkillDraftField(detail.Detail, "Existing memory:")
	existingSummary := experienceSkillDraftField(detail.Detail, "Existing summary:")
	checks := []string{
		"Assign one accountable owner for the disputed decision or memory.",
		"Compare the new discussion summary against the existing memory and source evidence.",
		"Decide whether to keep existing memory, replace it, merge both, or mark both as context-only.",
		"Record the owner decision before updating durable memory or policy.",
	}
	boundary := "read-only conflict reconciliation draft only; no memory update, policy change, routing change, file write, or tool execution was performed"

	var b strings.Builder
	b.WriteString("# A2A Conflict Reconciliation Draft\n\n")
	writeExperienceDraftLine(&b, "Trace", firstNonEmptyExperienceString(detail.Title, detail.ID))
	writeExperienceDraftLine(&b, "Source", firstNonEmptyExperienceString(detail.SourceURL, detail.SourceType))
	writeExperienceDraftLine(&b, "Review status", detail.ReviewStatus)
	writeExperienceDraftLine(&b, "Topic", topic)
	writeExperienceDraftLine(&b, "Question", question)
	b.WriteString("\n## Compared Signals\n\n")
	writeExperienceDraftLine(&b, "New discussion", newDiscussion)
	writeExperienceDraftLine(&b, "New summary", newSummary)
	writeExperienceDraftLine(&b, "Existing memory", existingMemory)
	writeExperienceDraftLine(&b, "Existing summary", existingSummary)
	b.WriteString("\n## Manual Decision Options\n\n")
	b.WriteString("- Keep existing memory and archive the new signal as rejected evidence.\n")
	b.WriteString("- Replace existing memory after owner approval.\n")
	b.WriteString("- Merge both signals into a clarified memory with provenance.\n")
	b.WriteString("- Keep both as context-only evidence until more source data is available.\n")
	b.WriteString("\n## Draft Checklist\n\n")
	for _, check := range checks {
		b.WriteString("- [ ] ")
		b.WriteString(check)
		b.WriteString("\n")
	}
	if strings.TrimSpace(detail.Detail) != "" {
		b.WriteString("\n## Source Evidence\n\n```text\n")
		b.WriteString(truncateExperienceText(detail.Detail, 2400))
		b.WriteString("\n```\n")
	}
	b.WriteString("\n## Safety Boundary\n\n")
	b.WriteString("This draft is non-executing. It does not update memory, change project policy, alter routing, write files, or run tools automatically.\n")

	return finalizeExperienceConflictReconciliationDraft(ExperienceConflictReconciliationDraft{
		TraceID:                 detail.ID,
		Kind:                    detail.Kind,
		Title:                   detail.Title,
		SourceURL:               detail.SourceURL,
		RecommendedFocusContext: experienceFocusContextFromTraceTarget(detail.ID, detail.Title, "approved A2A conflict review queued for manual reconciliation"),
		RecommendedToolCall:     experienceTraceInspectionRecommendedToolCall(detail.ID, detail.Title, "inspect source conflict review trace before recording draft outcome"),
		Topic:                   topic,
		Question:                question,
		NewDiscussion:           newDiscussion,
		NewSummary:              newSummary,
		ExistingMemory:          existingMemory,
		ExistingSummary:         existingSummary,
		DraftMarkdown:           strings.TrimSpace(b.String()),
		Checks:                  checks,
		NonExecutingBoundary:    boundary,
	})
}

func buildExperienceEscalationBrief(detail ExperienceTraceDetail) ExperienceEscalationBrief {
	reason := experienceEscalationSectionField(detail.Detail, "Reason")
	target := experienceEscalationSectionField(detail.Detail, "Target")
	raisedBy := experienceEscalationSectionField(detail.Detail, "Raised by")
	checks := []string{
		"Confirm the target owner and handoff channel before sending the escalation.",
		"Attach the source discussion, reason, and any decision or rollback evidence.",
		"State the requested decision or action explicitly.",
		"Record the owner response as follow-up audit evidence.",
	}
	boundary := "read-only escalation brief only; no routing, notification, policy change, file write, or tool execution was performed"

	var b strings.Builder
	b.WriteString("# A2A Escalation Handoff Brief\n\n")
	writeExperienceDraftLine(&b, "Trace", firstNonEmptyExperienceString(detail.Title, detail.ID))
	writeExperienceDraftLine(&b, "Source", firstNonEmptyExperienceString(detail.SourceURL, detail.SourceType))
	writeExperienceDraftLine(&b, "Target", target)
	writeExperienceDraftLine(&b, "Raised by", raisedBy)
	writeExperienceDraftLine(&b, "Reason", reason)
	b.WriteString("\n## Requested Manual Action\n\n")
	b.WriteString("- Confirm ownership of the escalated discussion.\n")
	b.WriteString("- Decide whether the discussion needs an owner decision, more evidence, or cancellation.\n")
	b.WriteString("- Reply with the decision and any required follow-up constraints.\n")
	b.WriteString("\n## Handoff Checklist\n\n")
	for _, check := range checks {
		b.WriteString("- [ ] ")
		b.WriteString(check)
		b.WriteString("\n")
	}
	if strings.TrimSpace(detail.Detail) != "" {
		b.WriteString("\n## Source Evidence\n\n```text\n")
		b.WriteString(truncateExperienceText(detail.Detail, 2400))
		b.WriteString("\n```\n")
	}
	b.WriteString("\n## Safety Boundary\n\n")
	b.WriteString("This brief is non-executing. It does not send notifications, change escalation routing, update policy, write files, or run tools automatically.\n")

	return finalizeExperienceEscalationBrief(ExperienceEscalationBrief{
		TraceID:                 detail.ID,
		Kind:                    detail.Kind,
		Title:                   detail.Title,
		SourceURL:               detail.SourceURL,
		RecommendedFocusContext: experienceFocusContextFromTraceTarget(detail.ID, detail.Title, "A2A escalation evidence queued for handoff briefing"),
		RecommendedToolCall:     experienceTraceInspectionRecommendedToolCall(detail.ID, detail.Title, "inspect source escalation evidence before recording brief outcome"),
		Reason:                  reason,
		Target:                  target,
		RaisedBy:                raisedBy,
		BriefMarkdown:           strings.TrimSpace(b.String()),
		Checks:                  checks,
		NonExecutingBoundary:    boundary,
	})
}

func buildExperienceRollbackWorkflowDraft(detail ExperienceTraceDetail) ExperienceRollbackWorkflowDraft {
	triggers := experienceRollbackTriggers(detail.Detail)
	summary := experienceSkillDraftField(detail.Detail, "Summary:")
	rationale := firstNonEmptyExperienceString(experienceSkillDraftField(detail.Detail, "Decision rationale:"), experienceSkillDraftField(detail.Detail, "Rationale:"))
	checks := []string{
		"Confirm every rollback trigger with the accountable owner before use.",
		"Map each trigger to observable evidence, stop conditions, and manual approval gates.",
		"Define dry-run verification and recovery communication steps before any execution path is connected.",
		"Record the final owner decision as follow-up audit evidence after the workflow is accepted or rejected.",
	}
	boundary := "read-only rollback workflow draft only; no rollback execution, policy update, routing change, file write, or tool execution was performed"

	var b strings.Builder
	b.WriteString("# Manual Rollback Workflow Draft\n\n")
	writeExperienceDraftLine(&b, "Trace", firstNonEmptyExperienceString(detail.Title, detail.ID))
	writeExperienceDraftLine(&b, "Source", firstNonEmptyExperienceString(detail.SourceURL, detail.SourceType))
	writeExperienceDraftLine(&b, "Review status", detail.ReviewStatus)
	writeExperienceDraftLine(&b, "Decision summary", summary)
	writeExperienceDraftLine(&b, "Decision rationale", rationale)
	if len(triggers) > 0 {
		b.WriteString("\n## Rollback Triggers\n\n")
		for _, trigger := range triggers {
			b.WriteString("- ")
			b.WriteString(trigger)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n## Manual Workflow Skeleton\n\n")
	b.WriteString("1. Confirm that one rollback trigger is present and backed by current evidence.\n")
	b.WriteString("2. Identify the owner who can authorize rollback for this decision.\n")
	b.WriteString("3. Run a dry review of expected impact, affected agents, data, and user-visible behavior.\n")
	b.WriteString("4. Execute only the owner-approved manual rollback steps outside this draft.\n")
	b.WriteString("5. Record outcome, evidence links, and follow-up status back onto the experience trace.\n")
	b.WriteString("\n## Draft Checklist\n\n")
	for _, check := range checks {
		b.WriteString("- [ ] ")
		b.WriteString(check)
		b.WriteString("\n")
	}
	if strings.TrimSpace(detail.Detail) != "" {
		b.WriteString("\n## Source Evidence\n\n```text\n")
		b.WriteString(truncateExperienceText(detail.Detail, 2400))
		b.WriteString("\n```\n")
	}
	b.WriteString("\n## Safety Boundary\n\n")
	b.WriteString("This draft is non-executing. It does not execute rollback, change routing, update policy, write files, or run tools automatically.\n")

	return finalizeExperienceRollbackWorkflowDraft(ExperienceRollbackWorkflowDraft{
		TraceID:                 detail.ID,
		Kind:                    detail.Kind,
		Title:                   detail.Title,
		SourceURL:               detail.SourceURL,
		RecommendedFocusContext: experienceFocusContextFromTraceTarget(detail.ID, detail.Title, experienceRollbackDraftFocusReason(detail)),
		RecommendedToolCall:     experienceTraceInspectionRecommendedToolCall(detail.ID, detail.Title, "inspect source rollback review before recording workflow draft outcome"),
		DecisionSummary:         summary,
		DecisionRationale:       rationale,
		RollbackTriggers:        triggers,
		DraftMarkdown:           strings.TrimSpace(b.String()),
		Checks:                  checks,
		NonExecutingBoundary:    boundary,
	})
}

func buildExperienceSkillDraft(detail ExperienceTraceDetail) ExperienceSkillDraft {
	name := normalizeExperienceSkillDraftName(firstNonEmptyExperienceString(
		experienceSkillDraftNameFromContent(detail.Detail),
		experienceSkillDraftNameFromTags(detail.Tags),
		detail.Title,
		"experience_skill_draft",
	))
	taskType := experienceSkillDraftField(detail.Detail, "Task type:")
	tokens := experienceSkillDraftTokens(detail.Detail)
	sequence := normalizeExperienceBrowserToolSequence(experienceSkillDraftSequence(detail.Detail, detail.Tags))
	evidence := experienceSkillDraftEvidence(detail.Detail)
	description := firstNonEmptyExperienceString(detail.Summary, "Manual skill candidate distilled from an approved repeated tool sequence.")
	checks := []string{
		"Confirm the sequence is reusable across more than one task context.",
		"Remove secrets, local-only paths, account identifiers, and one-off assumptions.",
		"Define inputs, expected outputs, failure handling, and verification steps before writing any skill files.",
		"Run the skill tests manually before installation or publication.",
	}
	boundary := "read-only skill draft only; no file write, skill install, routing change, tool execution, or policy change was performed"

	var b strings.Builder
	b.WriteString("# Skill Draft: ")
	b.WriteString(name)
	b.WriteString("\n\n")
	b.WriteString("## Purpose\n\n")
	b.WriteString(description)
	b.WriteString("\n\n")
	b.WriteString("## Evidence\n\n")
	writeExperienceDraftLine(&b, "Trace", firstNonEmptyExperienceString(detail.Title, detail.ID))
	writeExperienceDraftLine(&b, "Source", firstNonEmptyExperienceString(detail.SourceURL, detail.SourceType))
	writeExperienceDraftLine(&b, "Review status", detail.ReviewStatus)
	writeExperienceDraftLine(&b, "Task type", taskType)
	writeExperienceDraftLine(&b, "Tokens", strings.Join(tokens, ", "))
	writeExperienceDraftLine(&b, "Sequence", strings.Join(sequence, " -> "))
	writeExperienceDraftLine(&b, "Evidence", evidence)
	b.WriteString("\n## Suggested Skill Shape\n\n")
	b.WriteString("- Name: ")
	b.WriteString(name)
	b.WriteString("\n")
	if taskType != "" {
		b.WriteString("- Trigger context: ")
		b.WriteString(taskType)
		b.WriteString("\n")
	}
	if len(sequence) > 0 {
		b.WriteString("- Core tool flow: ")
		b.WriteString(strings.Join(sequence, " -> "))
		b.WriteString("\n")
	}
	b.WriteString("- Inputs to define: task goal, project/path scope, required credentials, and verification target.\n")
	b.WriteString("- Outputs to define: changed artifacts, evidence summary, and safe failure message.\n")
	b.WriteString("\n## Draft Checklist\n\n")
	for _, check := range checks {
		b.WriteString("- [ ] ")
		b.WriteString(check)
		b.WriteString("\n")
	}
	if strings.TrimSpace(detail.Detail) != "" {
		b.WriteString("\n## Source Evidence\n\n```text\n")
		b.WriteString(truncateExperienceText(detail.Detail, 2400))
		b.WriteString("\n```\n")
	}
	b.WriteString("\n## Safety Boundary\n\n")
	b.WriteString("This draft is non-executing. It does not create files, install or update skills, change routing scores, execute tools, or change project policy automatically.\n")

	return finalizeExperienceSkillDraft(ExperienceSkillDraft{
		TraceID:                 detail.ID,
		Kind:                    detail.Kind,
		Title:                   detail.Title,
		SourceURL:               detail.SourceURL,
		RecommendedFocusContext: experienceFocusContextFromTraceTarget(detail.ID, detail.Title, "approved skill nudge queued for manual skill drafting"),
		RecommendedToolCall:     experienceTraceInspectionRecommendedToolCall(detail.ID, detail.Title, "inspect source skill nudge before recording skill draft outcome"),
		SuggestedName:           name,
		TaskType:                taskType,
		QueryTokens:             tokens,
		ToolSequence:            sequence,
		Evidence:                evidence,
		Description:             description,
		DraftMarkdown:           strings.TrimSpace(b.String()),
		Checks:                  checks,
		NonExecutingBoundary:    boundary,
	})
}

func (a *App) buildExperienceBlockedSkillDraft(detail ExperienceTraceDetail) ExperienceBlockedSkillDraft {
	checks := []string{
		"Confirm whether the original approved draft is still valid against the current skill maintenance plan.",
		"Identify why execution was blocked before retrying preview or requesting another approval.",
		"Collect missing evidence, repair the source skill metadata, or reject the approval as stale.",
		"Record a new draft review only after the reviewer accepts the repair or evidence plan.",
	}
	boundary := "read-only blocked skill draft repair/evidence draft; no skill metadata change, file write, tool execution, retry, or approval was performed"
	planActions, matched := a.blockedSkillDraftCurrentPlanActions(detail.DraftID)
	focus := experienceFocusContextFromTraceTarget(detail.ID, detail.Title, "blocked skill draft execution needs repair or additional evidence before re-approval")
	focus["draft_id"] = detail.DraftID
	focus["execution_status"] = detail.DraftExecutionStatus

	var b strings.Builder
	b.WriteString("# Blocked Skill Draft Repair/Evidence Draft\n\n")
	writeExperienceDraftLine(&b, "Trace", firstNonEmptyExperienceString(detail.Title, detail.ID))
	writeExperienceDraftLine(&b, "Source trace", detail.SourceTraceID)
	writeExperienceDraftLine(&b, "Draft id", detail.DraftID)
	writeExperienceDraftLine(&b, "Execution status", detail.DraftExecutionStatus)
	writeExperienceDraftLine(&b, "Execution note", detail.DraftExecutionNote)
	writeExperienceDraftLine(&b, "Current plan match", fmt.Sprintf("%t", matched))
	b.WriteString("\n## Current Maintenance Plan Diff\n\n")
	if len(planActions) == 0 {
		b.WriteString("- Reviewed draft id is not present in the current maintenance plan. Treat the approval as stale until a reviewer accepts a new draft.\n")
	} else {
		for _, action := range planActions {
			writeExperienceDraftLine(&b, "Action", action["action"])
			writeExperienceDraftLine(&b, "Skill", action["skill"])
			writeExperienceDraftLine(&b, "Status", action["status"])
			writeExperienceDraftLine(&b, "Reason", action["reason"])
			b.WriteString("\n")
		}
	}
	b.WriteString("## Reviewer Decision Required\n\n")
	b.WriteString("- Repair source skill metadata and rerun a dry preview.\n")
	b.WriteString("- Collect more evidence and keep the approval blocked.\n")
	b.WriteString("- Reject the stale approval and leave the trace blocked.\n")
	b.WriteString("- Create a new governance draft if the current plan differs from the reviewed draft id.\n")
	b.WriteString("\n## Draft Checklist\n\n")
	for _, check := range checks {
		b.WriteString("- [ ] ")
		b.WriteString(check)
		b.WriteString("\n")
	}
	b.WriteString("\n## Source Evidence\n\n```text\n")
	b.WriteString(truncateExperienceText(detail.Detail, 2400))
	b.WriteString("\n```\n")
	b.WriteString("\n## Safety Boundary\n\n")
	b.WriteString("This draft is non-executing. It does not retry maintenance execution, approve a draft, alter skills, write files, or run tools automatically.\n")

	reviewOptions := experienceBlockedSkillDraftReviewOptions(detail.ID, focus, boundary)
	return finalizeExperienceBlockedSkillDraft(ExperienceBlockedSkillDraft{
		TraceID:                 detail.ID,
		Kind:                    detail.Kind,
		Title:                   detail.Title,
		SourceTraceID:           detail.SourceTraceID,
		DraftID:                 detail.DraftID,
		ExecutionStatus:         detail.DraftExecutionStatus,
		ExecutionNote:           detail.DraftExecutionNote,
		CurrentPlanMatched:      matched,
		CurrentPlanActions:      planActions,
		ReviewOptions:           reviewOptions,
		ReviewAffordances:       experienceBlockedSkillDraftReviewAffordances(reviewOptions),
		RecommendedFocusContext: focus,
		RecommendedToolCall:     experienceBlockedSkillDraftRecommendedToolCall(detail.ID, focus, boundary),
		DraftMarkdown:           strings.TrimSpace(b.String()),
		Checks:                  checks,
		NonExecutingBoundary:    boundary,
	})
}

func experienceBlockedSkillDraftReviewOptions(traceID string, focusContext map[string]interface{}, boundary string) map[string]interface{} {
	return map[string]interface{}{
		"close": normalizeExperienceLearningRecommendedToolCall(map[string]interface{}{
			"tool": "experience_learning",
			"args": map[string]interface{}{
				"action":     "record_blocked_skill_draft_review",
				"trace_id":   traceID,
				"resolution": "close",
			},
			"recommended_focus_context": focusContext,
			"non_executing":             true,
			"non_executing_boundary":    "blocked skill draft close review template only; records closure audit and must not retry execution, approve a draft, alter skills, write files, or run tools",
		}, focusContext, boundary),
		"reopen": normalizeExperienceLearningRecommendedToolCall(map[string]interface{}{
			"tool": "experience_learning",
			"args": map[string]interface{}{
				"action":               "record_blocked_skill_draft_review",
				"trace_id":             traceID,
				"resolution":           "reopen",
				"replacement_draft_id": "REQUIRED_REPLACEMENT_DRAFT_ID",
			},
			"recommended_focus_context": focusContext,
			"non_executing":             true,
			"non_executing_boundary":    "blocked skill draft reopen review template only; replacement_draft_id is required and no skill metadata changes are applied",
		}, focusContext, boundary),
	}
}

func experienceBlockedSkillDraftReviewAffordances(options map[string]interface{}) []ExperienceReviewAffordance {
	return []ExperienceReviewAffordance{
		{
			ID:                   "close",
			Label:                "Close blocked draft",
			Intent:               "close_blocked_skill_draft",
			Variant:              "secondary",
			Description:          "Mark the blocked approval stale or rejected and remove it from the blocked repair queue.",
			ToolCall:             mapFromInterface(options["close"]),
			NonExecutingBoundary: "records closure audit only; does not retry execution, approve a draft, alter skills, write files, or run tools",
		},
		{
			ID:          "reopen",
			Label:       "Reopen with replacement draft",
			Intent:      "reopen_blocked_skill_draft",
			Variant:     "primary",
			Description: "Record a replacement draft id and return the repaired approval to the active preview queue.",
			RequiredInputs: []ExperienceReviewAffordanceInput{
				{
					Name:        "replacement_draft_id",
					Label:       "Replacement draft id",
					Type:        "text",
					Required:    true,
					Placeholder: "skill_draft:...",
				},
			},
			ToolCall:             mapFromInterface(options["reopen"]),
			NonExecutingBoundary: "records reopen audit only; replacement_draft_id is required and no skill metadata changes are applied",
		},
	}
}

func mapFromInterface(value interface{}) map[string]interface{} {
	if m, ok := value.(map[string]interface{}); ok {
		return m
	}
	return nil
}

func (a *App) blockedSkillDraftCurrentPlanActions(draftID string) ([]map[string]string, bool) {
	draftID = strings.TrimSpace(draftID)
	if a == nil || draftID == "" || a.skillExecutor == nil {
		return nil, false
	}
	a.skillExecutor.mu.RLock()
	skills := a.skillExecutor.loadSkills()
	a.skillExecutor.mu.RUnlock()
	_, result := cskill.ExecuteReviewedGovernanceDrafts(skills, cskill.GovernanceDraftExecutionOptions{DryRun: true, ReviewedDraftIDs: []string{draftID}})
	out := make([]map[string]string, 0, len(result.Actions))
	matched := false
	for _, action := range result.Actions {
		if action.Action != "reviewed_draft" {
			matched = true
		}
		out = append(out, map[string]string{
			"action": strings.TrimSpace(action.Action),
			"skill":  strings.TrimSpace(action.Skill),
			"status": strings.TrimSpace(action.Status),
			"reason": strings.TrimSpace(action.Reason),
		})
	}
	return out, matched
}

func experienceBlockedSkillDraftRecommendedToolCall(traceID string, focusContext map[string]interface{}, boundary string) map[string]interface{} {
	return normalizeExperienceLearningRecommendedToolCall(map[string]interface{}{
		"tool": "experience_learning",
		"args": map[string]interface{}{
			"action":   "record_blocked_skill_draft_review",
			"trace_id": traceID,
		},
		"recommended_focus_context": focusContext,
		"non_executing":             true,
		"non_executing_boundary":    "draft repair/evidence review template only; caller must add status and note, and it must not retry execution, approve a draft, alter skills, write files, or run tools",
	}, focusContext, boundary)
}

func buildExperienceSkillDraftFromUsageNudge(query ExperienceRoutingSignalQuery, candidate *coretool.ToolSkillNudgeCandidate) ExperienceSkillDraft {
	name := normalizeExperienceSkillDraftName("usage_sequence_skill")
	var tokens []string
	var sequence []string
	taskType := firstNonEmptyExperienceString(query.TaskType, "skill_execution")
	evidence := "no approved repeated tool sequence matched the query yet"
	description := "Manual skill candidate distilled from repeated successful tool usage."
	if candidate != nil {
		name = normalizeExperienceSkillDraftName(firstNonEmptyExperienceString(candidate.SuggestedName, strings.Join(candidate.QueryTokens, "-"), "usage_sequence_skill"))
		tokens = append([]string(nil), candidate.QueryTokens...)
		sequence = normalizeExperienceBrowserToolSequence(candidate.ToolSequence)
		taskType = firstNonEmptyExperienceString(query.TaskType, candidate.TaskType, taskType)
		evidence = fmt.Sprintf("evidence %d, success %.0f%%, confidence %.2f", candidate.Evidence, candidate.SuccessRate*100, candidate.Confidence)
		description = firstNonEmptyExperienceString(candidate.Description, description)
	}
	checks := []string{
		"Confirm this is reusable beyond the observed query tokens before creating files.",
		"Turn the observed tool sequence into explicit skill params instead of hard-coded paths or secrets.",
		"Add verification steps and failure handling before installing or publishing the skill.",
		"Record a manual draft review outcome before any write or install action.",
	}
	boundary := "read-only usage-sequence skill draft only; no file write, skill install, routing change, tool execution, or policy change was performed"

	var b strings.Builder
	b.WriteString("# Skill Draft: ")
	b.WriteString(name)
	b.WriteString("\n\n")
	b.WriteString("## Purpose\n\n")
	b.WriteString(description)
	b.WriteString("\n\n## Evidence\n\n")
	writeExperienceDraftLine(&b, "Source", "UsageTracker.DistillSkillNudgeCandidates")
	writeExperienceDraftLine(&b, "Task type", taskType)
	writeExperienceDraftLine(&b, "Query", query.Query)
	writeExperienceDraftLine(&b, "Tokens", strings.Join(tokens, ", "))
	writeExperienceDraftLine(&b, "Sequence", strings.Join(sequence, " -> "))
	writeExperienceDraftLine(&b, "Evidence", evidence)
	b.WriteString("\n## Suggested Skill Shape\n\n")
	b.WriteString("- Name: ")
	b.WriteString(name)
	b.WriteString("\n")
	if len(sequence) > 0 {
		b.WriteString("- Core tool flow: ")
		b.WriteString(strings.Join(sequence, " -> "))
		b.WriteString("\n")
	}
	b.WriteString("- Inputs to define: task goal, workspace/project scope, input files, credentials, and output target.\n")
	b.WriteString("- Outputs to define: artifact paths, evidence summary, and fallback message.\n")
	b.WriteString("\n## Draft Checklist\n\n")
	for _, check := range checks {
		b.WriteString("- [ ] ")
		b.WriteString(check)
		b.WriteString("\n")
	}
	b.WriteString("\n## Safety Boundary\n\n")
	b.WriteString("This draft is non-executing. It does not create files, install or update skills, change routing scores, execute tools, or change project policy automatically.\n")

	focusReason := "usage-sequence skill draft candidate"
	if candidate == nil {
		focusReason = "no matching skill nudge candidate; collect more evidence"
	}
	return ExperienceSkillDraft{
		Kind:                    experienceDraftKindSkill,
		Title:                   name,
		RecommendedFocusContext: map[string]interface{}{"reason": focusReason, "query": query.Query, "task_type": taskType},
		RecommendedToolCall: map[string]interface{}{
			"tool":                   "experience_learning",
			"args":                   map[string]interface{}{"action": "record_draft_review", "draft_kind": experienceDraftKindSkill, "query": query.Query},
			"non_executing":          true,
			"non_executing_boundary": boundary,
		},
		SuggestedName:        name,
		TaskType:             taskType,
		QueryTokens:          tokens,
		ToolSequence:         sequence,
		Evidence:             evidence,
		Description:          description,
		DraftMarkdown:        strings.TrimSpace(b.String()),
		Checks:               checks,
		NonExecutingBoundary: boundary,
	}
}

func experienceRollbackTriggers(content string) []string {
	lines := strings.Split(content, "\n")
	inSection := false
	var triggers []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.EqualFold(strings.TrimSuffix(trimmed, ":"), "Rollback on") {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "*") && strings.Contains(trimmed, ":") {
			break
		}
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "*"))
		if trimmed != "" {
			triggers = append(triggers, trimmed)
		}
		if len(triggers) >= 12 {
			break
		}
	}
	return triggers
}

func experienceEscalationSectionField(content, field string) string {
	lines := strings.Split(content, "\n")
	inSection := false
	prefix := strings.ToLower(strings.TrimSpace(field)) + ":"
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.EqualFold(strings.TrimSuffix(trimmed, ":"), "Escalation") {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "-") && strings.HasSuffix(trimmed, ":") {
			break
		}
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		if strings.HasPrefix(strings.ToLower(trimmed), prefix) {
			return strings.TrimSpace(trimmed[len(prefix):])
		}
	}
	return ""
}

func normalizeExperienceSkillDraftName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "experience_skill_draft"
	}
	var b strings.Builder
	lastSep := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastSep = false
			continue
		}
		if r == '_' || r == '-' || r == ' ' || r == '/' || r == ':' {
			if !lastSep && b.Len() > 0 {
				b.WriteByte('_')
				lastSep = true
			}
		}
	}
	name := strings.Trim(b.String(), "_")
	if name == "" {
		name = "experience_skill_draft"
	}
	first := name[0]
	if first >= '0' && first <= '9' {
		name = "skill_" + name
	}
	if len(name) > 80 {
		name = strings.TrimRight(name[:80], "_")
	}
	return name
}

func experienceSkillDraftNameFromContent(content string) string {
	return experienceSkillDraftFragmentAfter(content, "skill nudge candidate ")
}

func experienceSkillDraftNameFromTags(tags []string) string {
	for i, tag := range tags {
		if tag != experienceTraceKindSkillNudgeCandidate.String() || i+1 >= len(tags) {
			continue
		}
		for _, candidate := range tags[i+1:] {
			if experienceSkillDraftStateTag(candidate) {
				continue
			}
			return candidate
		}
	}
	return ""
}

func experienceSkillDraftSequence(content string, tags []string) []string {
	if sequence := splitExperienceSkillDraftList(experienceSkillDraftFragmentAfter(content, "sequence ")); len(sequence) > 0 {
		return sequence
	}
	for i, tag := range tags {
		if tag != experienceTraceKindSkillNudgeCandidate.String() {
			continue
		}
		start := i + 3
		if start > len(tags) {
			return nil
		}
		var sequence []string
		for _, candidate := range tags[start:] {
			if experienceSkillDraftStateTag(candidate) {
				continue
			}
			sequence = append(sequence, candidate)
			if len(sequence) >= 8 {
				break
			}
		}
		return sequence
	}
	return nil
}

func experienceSkillDraftTokens(content string) []string {
	fragment := experienceSkillDraftBracketField(content, "tokens [")
	return splitExperienceSkillDraftList(fragment)
}

func experienceSkillDraftEvidence(content string) string {
	return experienceSkillDraftFragmentAfter(content, "evidence ")
}

func experienceSkillDraftField(content, marker string) string {
	lower := strings.ToLower(content)
	markerLower := strings.ToLower(marker)
	idx := strings.Index(lower, markerLower)
	if idx < 0 {
		return ""
	}
	return trimExperienceSkillDraftFragment(content[idx+len(marker):])
}

func experienceSkillDraftFragmentAfter(content, marker string) string {
	lower := strings.ToLower(content)
	idx := strings.Index(lower, strings.ToLower(marker))
	if idx < 0 {
		return ""
	}
	return trimExperienceSkillDraftFragment(content[idx+len(marker):])
}

func experienceSkillDraftBracketField(content, marker string) string {
	lower := strings.ToLower(content)
	idx := strings.Index(lower, strings.ToLower(marker))
	if idx < 0 {
		return ""
	}
	rest := content[idx+len(marker):]
	end := strings.Index(rest, "]")
	if end >= 0 {
		rest = rest[:end]
	}
	return trimExperienceSkillDraftFragment(rest)
}

func trimExperienceSkillDraftFragment(value string) string {
	value = strings.TrimSpace(value)
	cut := len(value)
	for _, sep := range []string{"\n", ";"} {
		if idx := strings.Index(value, sep); idx >= 0 && idx < cut {
			cut = idx
		}
	}
	value = strings.TrimSpace(value[:cut])
	return strings.Trim(value, " .")
}

func splitExperienceSkillDraftList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	value = strings.ReplaceAll(value, " -> ", ",")
	value = strings.ReplaceAll(value, "->", ",")
	value = strings.ReplaceAll(value, "|", ",")
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(strings.TrimSpace(part), "[]")
		if part == "" {
			continue
		}
		result = append(result, part)
	}
	return result
}

func normalizeExperienceBrowserToolSequence(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = normalizeExperienceBrowserToolName(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func experienceIsMergedBrowserOnlySequence(values []string) bool {
	values = normalizeExperienceBrowserToolSequence(values)
	return len(values) == 1 && values[0] == "browser"
}

func experienceSkillDraftStateTag(tag string) bool {
	tag = strings.TrimSpace(tag)
	if tag == "" || tag == experienceReviewRequiredTag || tag == experienceReviewResolvedTag || normalizeExperienceReviewLifecycleTagKind(tag).IsStateTag() {
		return true
	}
	return strings.HasPrefix(tag, experienceReviewStatusTagPrefix) ||
		strings.HasPrefix(tag, experienceReviewedAtTagPrefix) ||
		strings.HasPrefix(tag, experienceFollowUpStatusTagPrefix) ||
		strings.HasPrefix(tag, "followup_") ||
		strings.HasPrefix(tag, "followup_at:")
}

func appendExperienceFollowUpRecord(content, status, actionKind, note, actor string, now time.Time) string {
	content = strings.TrimSpace(content)
	note = truncateExperienceText(strings.TrimSpace(note), 800)
	actor = truncateExperienceText(strings.TrimSpace(actor), 160)
	if actor == "" {
		actor = "local"
	}
	var b strings.Builder
	if content != "" {
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	b.WriteString("Experience follow-up record:")
	b.WriteString("\n- Action kind: ")
	b.WriteString(actionKind)
	b.WriteString("\n- Status: ")
	b.WriteString(status)
	b.WriteString("\n- Actor: ")
	b.WriteString(actor)
	b.WriteString("\n- Recorded at: ")
	b.WriteString(now.Format(time.RFC3339))
	if note != "" {
		b.WriteString("\n- Note: ")
		b.WriteString(note)
	}
	b.WriteString("\n- Safety: recorded only; no skill, rollback, routing, file, or policy change was executed automatically.")
	return strings.TrimSpace(b.String())
}

func appendSkillDraftExecutionRecord(content, status, note, actor string, now time.Time) string {
	content = strings.TrimSpace(content)
	note = truncateExperienceText(strings.TrimSpace(note), 800)
	actor = truncateExperienceText(strings.TrimSpace(actor), 160)
	if actor == "" {
		actor = "local"
	}
	var b strings.Builder
	if content != "" {
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	b.WriteString("Skill draft execution record:")
	b.WriteString("\n- Status: ")
	b.WriteString(status)
	b.WriteString("\n- Actor: ")
	b.WriteString(actor)
	b.WriteString("\n- Recorded at: ")
	b.WriteString(now.Format(time.RFC3339))
	if note != "" {
		b.WriteString("\n- Note: ")
		b.WriteString(note)
	}
	b.WriteString("\n- Safety: records skill governance draft execution state; no extra skill action is implied by this audit line.")
	return strings.TrimSpace(b.String())
}

func applyExperienceFollowUpTags(tags []string, status string, now time.Time) []string {
	result := withoutExperienceFollowUpStateTags(tags)
	result = append(result, experienceFollowUpStatusTagPrefix+status)
	switch status {
	case experienceFollowUpOutcomeDeferred:
		result = append(result, "followup_deferred")
	default:
		result = append(result, experienceFollowUpResolvedTag, "followup_at:"+now.Format("20060102"))
		if status == experienceFollowUpOutcomeCompleted {
			result = append(result, "followup_completed")
		} else {
			result = append(result, "followup_blocked")
		}
	}
	return normalizeUsageMemoryTags(result)
}

func withoutExperienceFollowUpStateTags(tags []string) []string {
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || tag == experienceFollowUpResolvedTag || tag == "followup_deferred" || tag == "followup_completed" || tag == "followup_blocked" {
			continue
		}
		if strings.HasPrefix(tag, experienceFollowUpStatusTagPrefix) || strings.HasPrefix(tag, "followup_at:") {
			continue
		}
		result = append(result, tag)
	}
	return result
}

func applySkillDraftExecutionTags(tags []string, status string, now time.Time) []string {
	result := make([]string, 0, len(tags)+3)
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || tag == "skill_draft_previewed" || tag == "skill_draft_applied" || tag == "skill_draft_blocked" || tag == "skill_draft_reopened" || tag == "skill_draft_closed" {
			continue
		}
		if strings.HasPrefix(tag, skillDraftExecutionStatusTagPrefix) || strings.HasPrefix(tag, skillDraftExecutionAtTagPrefix) {
			continue
		}
		result = append(result, tag)
	}
	result = append(result, skillDraftExecutionStatusTagPrefix+status, skillDraftExecutionAtTagPrefix+now.Format("20060102"))
	switch status {
	case skillDraftExecutionApplied:
		result = append(result, "skill_draft_applied")
	case skillDraftExecutionBlocked:
		result = append(result, "skill_draft_blocked")
	case skillDraftExecutionReopened:
		result = append(result, "skill_draft_reopened")
	case skillDraftExecutionClosed:
		result = append(result, "skill_draft_closed")
	default:
		result = append(result, "skill_draft_previewed")
	}
	return normalizeUsageMemoryTags(result)
}

func buildExperienceTraceFollowUpDraft(detail ExperienceTraceDetail) ExperienceTraceFollowUpDraft {
	triggeredRollback := experienceTriggeredRollbackEvidence(detail)
	checks := experienceFollowUpChecks(detail.NextActionKind, triggeredRollback)
	draftTitle := experienceFollowUpDraftTitle(detail.NextActionKind, triggeredRollback)
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(draftTitle)
	b.WriteString("\n\n")
	writeExperienceDraftLine(&b, "Trace", firstNonEmptyExperienceString(detail.Title, detail.ID))
	writeExperienceDraftLine(&b, "Source", firstNonEmptyExperienceString(detail.SourceURL, detail.SourceType))
	writeExperienceDraftLine(&b, "Review status", detail.ReviewStatus)
	writeExperienceDraftLine(&b, "Next action", detail.NextAction)
	b.WriteString("\n## Safety Boundary\n\n")
	b.WriteString("This is a manual follow-up draft only. It does not execute rollback, create or install skills, change routing scores, or update project policy automatically.\n")
	if len(checks) > 0 {
		b.WriteString("\n## Checklist\n\n")
		for _, check := range checks {
			b.WriteString("- [ ] ")
			b.WriteString(check)
			b.WriteString("\n")
		}
	}
	if detail.Detail != "" {
		b.WriteString("\n## Source Evidence\n\n```text\n")
		b.WriteString(truncateExperienceText(detail.Detail, 2400))
		b.WriteString("\n```\n")
	}
	return finalizeExperienceTraceFollowUpDraft(ExperienceTraceFollowUpDraft{
		TraceID:                 detail.ID,
		Kind:                    detail.Kind,
		Title:                   detail.Title,
		SourceURL:               detail.SourceURL,
		RecommendedFocusContext: experienceFocusContextFromTraceTarget(detail.ID, detail.Title, experienceFollowUpDraftFocusReason(detail, triggeredRollback)),
		RecommendedToolCall:     experienceTraceInspectionRecommendedToolCall(detail.ID, detail.Title, "inspect source trace before recording follow-up outcome"),
		NonExecutingBoundary:    "manual follow-up draft only; no rollback execution, skill creation or install, routing change, memory rewrite, file write, tool execution, notification, or project policy update was performed",
		ActionKind:              detail.NextActionKind,
		Action:                  detail.NextAction,
		DraftTitle:              draftTitle,
		Draft:                   strings.TrimSpace(b.String()),
		Checks:                  checks,
	})
}

func finalizeExperienceTraceFollowUpDraft(draft ExperienceTraceFollowUpDraft) ExperienceTraceFollowUpDraft {
	draft.RecommendedToolCall = normalizeExperienceLearningRecommendedToolCall(draft.RecommendedToolCall, draft.RecommendedFocusContext, draft.NonExecutingBoundary)
	return draft
}

func finalizeExperienceSkillDraft(draft ExperienceSkillDraft) ExperienceSkillDraft {
	draft.RecommendedToolCall = normalizeExperienceLearningRecommendedToolCall(draft.RecommendedToolCall, draft.RecommendedFocusContext, draft.NonExecutingBoundary)
	return draft
}

func finalizeExperienceBlockedSkillDraft(draft ExperienceBlockedSkillDraft) ExperienceBlockedSkillDraft {
	draft.RecommendedToolCall = normalizeExperienceLearningRecommendedToolCall(draft.RecommendedToolCall, draft.RecommendedFocusContext, draft.NonExecutingBoundary)
	return draft
}

func finalizeExperienceRollbackWorkflowDraft(draft ExperienceRollbackWorkflowDraft) ExperienceRollbackWorkflowDraft {
	draft.RecommendedToolCall = normalizeExperienceLearningRecommendedToolCall(draft.RecommendedToolCall, draft.RecommendedFocusContext, draft.NonExecutingBoundary)
	return draft
}

func finalizeExperienceEscalationBrief(brief ExperienceEscalationBrief) ExperienceEscalationBrief {
	brief.RecommendedToolCall = normalizeExperienceLearningRecommendedToolCall(brief.RecommendedToolCall, brief.RecommendedFocusContext, brief.NonExecutingBoundary)
	return brief
}

func finalizeExperienceConflictReconciliationDraft(draft ExperienceConflictReconciliationDraft) ExperienceConflictReconciliationDraft {
	draft.RecommendedToolCall = normalizeExperienceLearningRecommendedToolCall(draft.RecommendedToolCall, draft.RecommendedFocusContext, draft.NonExecutingBoundary)
	return draft
}

func finalizeExperienceTraceFollowUpRecord(record ExperienceTraceFollowUpRecord) ExperienceTraceFollowUpRecord {
	record.RecommendedToolCall = normalizeExperienceLearningRecommendedToolCall(record.RecommendedToolCall, record.RecommendedFocusContext, record.NonExecutingBoundary)
	return record
}

func finalizeExperienceDraftReviewRecord(record ExperienceDraftReviewRecord) ExperienceDraftReviewRecord {
	record.RecommendedToolCall = normalizeExperienceLearningRecommendedToolCall(record.RecommendedToolCall, record.RecommendedFocusContext, record.NonExecutingBoundary)
	return record
}

func experienceFollowUpDraftFocusReason(detail ExperienceTraceDetail, triggeredRollback bool) string {
	if triggeredRollback {
		return "triggered rollback follow-up draft for current priority trace"
	}
	if kind := strings.TrimSpace(detail.NextActionKind); kind != "" {
		return "manual follow-up draft for " + kind
	}
	return "manual follow-up draft for current priority trace"
}

func experienceRollbackDraftFocusReason(detail ExperienceTraceDetail) string {
	if experienceTriggeredRollbackEvidence(detail) {
		return "approved triggered rollback review queued for manual rollback workflow drafting"
	}
	return "approved rollback review queued for manual rollback workflow drafting"
}

func writeExperienceDraftLine(b *strings.Builder, label, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	b.WriteString("- ")
	b.WriteString(label)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\n")
}

func experienceFollowUpDraftTitle(actionKind string, triggeredRollback bool) string {
	switch normalizeExperienceGovernanceActionKind(actionKind) {
	case experienceGovernanceActionReviewTriggeredRollbackSignal:
		return "Triggered rollback review handoff"
	case experienceGovernanceActionResolveA2AConflictManually:
		return "Manual A2A conflict reconciliation draft"
	case experienceGovernanceActionKeepRejectedConflictEvidence:
		return "Rejected A2A conflict evidence note"
	case experienceGovernanceActionCollectA2AConflictEvidence:
		return "A2A conflict evidence collection draft"
	case experienceGovernanceActionDraftRollbackWorkflow:
		if triggeredRollback {
			return "Triggered rollback workflow handoff"
		}
		return "Manual rollback workflow draft"
	case experienceGovernanceActionBlockRollbackUse:
		return "Rejected rollback trigger note"
	case experienceGovernanceActionCollectRollbackEvidence:
		return "Rollback evidence collection draft"
	case experienceGovernanceActionDraftSkillManually:
		return "Manual skill draft brief"
	case experienceGovernanceActionSuppressSkillCandidate:
		return "Rejected skill candidate note"
	case experienceGovernanceActionCollectSkillEvidence:
		return "Skill evidence collection draft"
	case experienceGovernanceActionPrepareEscalationBrief:
		return "A2A escalation handoff brief"
	default:
		return "Manual experience follow-up draft"
	}
}

func experienceFollowUpChecks(actionKind string, triggeredRollback bool) []string {
	switch normalizeExperienceGovernanceActionKind(actionKind) {
	case experienceGovernanceActionReviewTriggeredRollbackSignal:
		return []string{"Reconfirm which current A2A evidence matched each rollback trigger.", "Name the owner who must approve or reject the rollback path.", "Record the owner decision before drafting or reusing any rollback workflow."}
	case experienceGovernanceActionResolveA2AConflictManually:
		return []string{"Identify the accountable owner for the disputed decision.", "Compare both sides against source evidence.", "Update durable memory or policy only after owner approval."}
	case experienceGovernanceActionKeepRejectedConflictEvidence:
		return []string{"Keep the rejected conflict attached as audit evidence.", "Do not promote either disputed claim from this signal.", "Reference the rejection if the same conflict reappears."}
	case experienceGovernanceActionCollectA2AConflictEvidence:
		return []string{"List the missing evidence or owner input.", "Collect source links or discussion excerpts.", "Re-run human review after evidence is available."}
	case experienceGovernanceActionDraftRollbackWorkflow:
		if triggeredRollback {
			return []string{"Reconfirm which current A2A evidence still matches each rollback trigger.", "Record the owner decision before drafting or reusing the rollback workflow.", "Keep execution blocked until the owner-approved rollback workflow is separately reviewed."}
		}
		return []string{"Validate each rollback trigger with an owner.", "Define the manual rollback steps and stop conditions.", "Run a dry review before any execution path uses the workflow."}
	case experienceGovernanceActionBlockRollbackUse:
		return []string{"Mark the triggers as rejected evidence.", "Do not wire these triggers into rollback execution.", "Capture the reason for future audits."}
	case experienceGovernanceActionCollectRollbackEvidence:
		return []string{"Collect validation evidence for every trigger.", "Confirm trigger ownership and expected impact.", "Review again before drafting an executable workflow."}
	case experienceGovernanceActionDraftSkillManually:
		return []string{"Confirm the repeated sequence is reusable outside the original task.", "Check for secrets, local paths, and environment assumptions.", "Draft or update the skill manually and run its tests before installation."}
	case experienceGovernanceActionSuppressSkillCandidate:
		return []string{"Keep the rejected candidate as evidence.", "Do not create or update a skill from this signal.", "Use the rejection reason when similar nudges recur."}
	case experienceGovernanceActionCollectSkillEvidence:
		return []string{"Collect more successful executions across contexts.", "Validate parameter and platform assumptions.", "Review again before drafting a skill manually."}
	case experienceGovernanceActionPrepareEscalationBrief:
		return []string{"Confirm the escalation target and accountable owner.", "Attach the discussion reason, source evidence, and requested manual action.", "Record the owner response as follow-up audit evidence."}
	default:
		return []string{"Confirm the action owner.", "Attach source evidence.", "Record the manual outcome after completion."}
	}
}

func experienceTriggeredRollbackEvidence(detail ExperienceTraceDetail) bool {
	if normalizeExperienceGovernanceActionKind(detail.NextActionKind) == experienceGovernanceActionReviewTriggeredRollbackSignal || normalizeExperienceGovernanceActionKind(detail.FollowUpActionKind) == experienceGovernanceActionReviewTriggeredRollbackSignal {
		return true
	}
	for _, tag := range detail.Tags {
		if strings.TrimSpace(tag) == groupDiscussionRollbackTriggered {
			return true
		}
	}
	return classifyExperienceEvidenceMarker(detail.Detail) == experienceEvidenceMarkerMatchedRollbackTriggers
}
