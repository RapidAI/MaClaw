package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
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
	RecommendedFocusContext map[string]interface{} `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall     map[string]interface{} `json:"recommended_tool_call,omitempty"`
	NonExecutingBoundary    string                 `json:"non_executing_boundary,omitempty"`
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
	return finalizeExperienceSkillDraft(buildExperienceSkillDraft(detail)), nil
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
	content := appendExperienceFollowUpRecord(entry.Content, status, detail.NextActionKind, req.Note, a.defaultExperienceReviewReviewer(req.Actor), now)
	tags := applyExperienceFollowUpTags(entry.Tags, status, now)
	if err := a.memoryStore.Update(entry.ID, content, entry.Category, tags); err != nil {
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
	memoryID := fmt.Sprintf("experience-draft-%s-%d", strings.ReplaceAll(kind, "_", "-"), now.UnixNano())
	content := appendExperienceFollowUpRecord(buildExperienceDraftReviewContent(req, kind), status, kind, req.Note, a.defaultExperienceReviewReviewer(req.Actor), now)
	tags := applyExperienceFollowUpTags([]string{experienceDraftReviewTag, kind, "non_executing_draft_review"}, status, now)
	entry := memory.Entry{
		ID:         memoryID,
		Title:      "Experience draft review: " + experienceDraftReviewTitle(kind),
		Content:    content,
		Category:   memory.CategoryProjectKnowledge,
		Tags:       tags,
		SourceType: experienceLearningSourceType,
		SourceURL:  "experience://draft/" + kind,
	}
	if err := a.memoryStore.Save(entry); err != nil {
		return ExperienceDraftReviewRecord{}, err
	}
	traceID := "memory:" + memoryID
	a.emitEvent("memory:experience-draft-reviewed", map[string]string{
		"trace_id": traceID,
		"kind":     kind,
		"status":   status,
	})
	sourceTraceID := strings.TrimSpace(req.SourceTraceID)
	return finalizeExperienceDraftReviewRecord(ExperienceDraftReviewRecord{
		TraceID:                 traceID,
		MemoryID:                memoryID,
		Kind:                    kind,
		Status:                  status,
		SourceTraceID:           sourceTraceID,
		RecommendedFocusContext: a.experienceRecommendedFocusContextForTrace(sourceTraceID, "manual draft review recorded for source experience trace"),
		RecommendedToolCall:     experienceTraceInspectionRecommendedToolCall("memory:"+entry.ID, entry.Title, "manual draft review audit record"),
		NonExecutingBoundary:    "draft review audit record only; the reviewed draft was not executed, memory was not rewritten beyond audit evidence, routing was not changed, files were not written, tools were not run, rollback was not executed, notifications were not sent, and skills were not installed",
	}), nil
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
	sequence := experienceSkillDraftSequence(detail.Detail, detail.Tags)
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
