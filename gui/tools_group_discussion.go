package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// registerGroupDiscussionTools exposes current-Hub-only MaClaw group discussion
// to the agent loop. Starting a discussion is intentionally split from suggesting
// one: start_authorized must only be called after explicit human authorization.
func registerGroupDiscussionTools(registry *ToolRegistry, app *App, handler *IMMessageHandler) {
	if registry == nil || app == nil {
		return
	}
	registry.Register(RegisteredTool{
		Name:        "group_discussion",
		Description: "Use current-Hub-only MaClaw group discussion for difficult tasks. Actions: status, list_experts, rank_experts, list_mine, get_discussion, get_detail, workflow_state, workflow_action_draft, escalation_route, rollback_readiness, readiness, summarize_result, cleanup_stale, process_invites, suggest, start_authorized, send_message, add_proposal, add_review, decide, escalate, submit_result, set_state. status, list_experts, list_mine, get_discussion, get_detail, suggest, rank_experts, readiness, summarize_result with preview=true, cleanup_stale with dry_run=true, workflow_state, workflow_action_draft, escalation_route, and rollback_readiness expose recommended_focus_context, recommended_tool_call, and non_executing_boundary while preserving their normal result object; recommended_tool_call outputs include recommended_focus_context and discussion_focus_context with consultation, action, proposal, participant, trigger, rollback, escalation, summary-preview, cleanup-preview, and selected-invitee targets for safe non-executing handoff. Must ask the human before start_authorized unless local policy explicitly permits same-security-group free discussion.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"group", "discussion", "consultation", "expert", "maclaw", "hub", "collaboration"},
		Priority:    8,
		Status:      RegToolAvailable,
		Required:    []string{"action"},
		InputSchema: map[string]interface{}{
			"action":            map[string]interface{}{"type": "string", "description": "status | list_experts | rank_experts | list_mine | get_discussion | get_detail | workflow_state | workflow_action_draft | escalation_route | rollback_readiness | readiness | summarize_result | cleanup_stale | process_invites | suggest | start_authorized | send_message | add_proposal | add_review | decide | escalate | submit_result | set_state"},
			"topic":             map[string]interface{}{"type": "string", "description": "Short discussion topic"},
			"question":          map[string]interface{}{"type": "string", "description": "The concrete problem for other MaClaw experts"},
			"context_summary":   map[string]interface{}{"type": "string", "description": "Minimal context summary; do not include sensitive/raw context unless policy allows"},
			"skills_wanted":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Desired expert skills"},
			"risk_level":        map[string]interface{}{"type": "string", "description": "low | medium | high"},
			"invitee_ids":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional explicit expert agent IDs. Empty means auto-select up to three eligible experts."},
			"role":              map[string]interface{}{"type": "string", "description": "observe | speak | review"},
			"trusted":           map[string]interface{}{"type": "boolean", "description": "Whether this invite is trusted by local policy"},
			"consultation_id":   map[string]interface{}{"type": "string", "description": "Discussion/consultation ID"},
			"content":           map[string]interface{}{"type": "string", "description": "Discussion message content"},
			"message_kind":      map[string]interface{}{"type": "string", "description": "For send_message: statement | question | answer | evidence | objection"},
			"proposal_id":       map[string]interface{}{"type": "string", "description": "For add_review/decide: target proposal ID"},
			"proposal_title":    map[string]interface{}{"type": "string", "description": "For add_proposal: proposal title"},
			"proposal_content":  map[string]interface{}{"type": "string", "description": "For add_proposal: concrete proposal content"},
			"author_id":         map[string]interface{}{"type": "string", "description": "Optional proposal author; defaults to local remote machine ID"},
			"reviewer_id":       map[string]interface{}{"type": "string", "description": "Optional reviewer; defaults to local remote machine ID"},
			"review_position":   map[string]interface{}{"type": "string", "description": "For add_review: approve | reject | concern | abstain"},
			"decision_summary":  map[string]interface{}{"type": "string", "description": "For decide: optional final decision summary"},
			"rollback_on":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "For decide: optional rollback triggers"},
			"raised_by":         map[string]interface{}{"type": "string", "description": "For escalate: optional raising agent; defaults to local remote machine ID"},
			"escalation_reason": map[string]interface{}{"type": "string", "description": "For escalate: why the discussion needs escalation"},
			"escalation_target": map[string]interface{}{"type": "string", "description": "For escalate: target owner, defaults to iworkercenter"},
			"rollback_evidence": map[string]interface{}{"type": "string", "description": "For rollback_readiness: optional human-provided evidence text to match against decision rollback triggers"},
			"goals":             map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "For add_proposal: optional goals"},
			"constraints":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "For add_proposal: optional constraints"},
			"summary":           map[string]interface{}{"type": "string", "description": "Final result summary"},
			"rationale":         map[string]interface{}{"type": "string", "description": "Reasoning/rationale summary"},
			"risks":             map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Known risks or caveats"},
			"state_action":      map[string]interface{}{"type": "string", "description": "pause | resume | cancel"},
			"submit":            map[string]interface{}{"type": "boolean", "description": "For summarize_result, submit the synthesized result to the Hub discussion"},
			"inject":            map[string]interface{}{"type": "boolean", "description": "For summarize_result, inject the synthesized result into the active AI assistant loop"},
			"force":             map[string]interface{}{"type": "boolean", "description": "For summarize_result, allow summarizing before the readiness gate is satisfied"},
			"preview":           map[string]interface{}{"type": "boolean", "description": "For summarize_result, return the synthesized result without submitting, injecting, or promoting memory"},
			"dry_run":           map[string]interface{}{"type": "boolean", "description": "For cleanup_stale, list stale discussions without cancelling them"},
			"role_filter":       map[string]interface{}{"type": "string", "description": "Optional list_mine role filter"},
		},
		Source: "builtin:group_discussion",
		Handler: func(args map[string]interface{}) string {
			return dispatchGroupDiscussionTool(app, handler, args)
		},
	})
}

func dispatchGroupDiscussionTool(app *App, handler *IMMessageHandler, args map[string]interface{}) string {
	switch strings.TrimSpace(strings.ToLower(stringVal(args, "action"))) {
	case "status":
		status := app.GroupDiscussionStatus()
		return groupDiscussionResultWithSafeHandoff(map[string]interface{}{"status": status}, status.RecommendedFocusContext, status.RecommendedToolCall, firstNonEmptyGroupString(status.NonExecutingBoundary, groupDiscussionStatusNonExecutingBoundary), nil)
	case "list_experts":
		experts, err := app.GroupDiscussionListExperts()
		focusContext := groupDiscussionExpertsFocusContext(experts)
		return groupDiscussionResultWithSafeHandoff(map[string]interface{}{"experts": experts}, focusContext, groupDiscussionExpertsToolCall(focusContext), groupDiscussionExpertsNonExecutingBoundary, err)
	case "rank_experts":
		ranking, err := app.GroupDiscussionRankExperts(groupDiscussionRequestFromArgs(app, args))
		return groupDiscussionResultWithSafeHandoff(map[string]interface{}{"expert_ranking": ranking}, ranking.RecommendedFocusContext, ranking.RecommendedToolCall, ranking.NonExecutingBoundary, err)
	case "list_mine":
		discussions, err := app.GroupDiscussionListMine(stringVal(args, "role_filter"))
		focusContext := groupDiscussionListMineFocusContext(discussions, stringVal(args, "role_filter"))
		return groupDiscussionResultWithSafeHandoff(map[string]interface{}{"discussions": discussions}, focusContext, groupDiscussionListMineToolCall(focusContext), groupDiscussionListMineNonExecutingBoundary, err)
	case "get_discussion":
		discussion, err := app.GroupDiscussionGetConsultation(stringVal(args, "consultation_id"))
		focusContext := groupDiscussionSummaryFocusContext(discussion, stringVal(args, "consultation_id"))
		return groupDiscussionResultWithSafeHandoff(map[string]interface{}{"discussion": discussion}, focusContext, groupDiscussionSummaryToolCall(focusContext), groupDiscussionSummaryNonExecutingBoundary, err)
	case "get_detail":
		detail, err := app.GroupDiscussionGetConsultationDetail(stringVal(args, "consultation_id"))
		focusContext := groupDiscussionDetailFocusContext(detail, stringVal(args, "consultation_id"))
		return groupDiscussionResultWithSafeHandoff(map[string]interface{}{"discussion_detail": detail}, focusContext, groupDiscussionDetailToolCall(focusContext), groupDiscussionDetailNonExecutingBoundary, err)
	case "workflow_state":
		state, err := app.GroupDiscussionGetWorkflowState(stringVal(args, "consultation_id"))
		return groupDiscussionResultWithSafeHandoff(map[string]interface{}{"workflow_state": state}, state.RecommendedFocusContext, state.RecommendedToolCall, state.NonExecutingBoundary, err)
	case "workflow_action_draft":
		draft, err := app.GroupDiscussionBuildWorkflowActionDraft(stringVal(args, "consultation_id"))
		return groupDiscussionResultWithSafeHandoff(map[string]interface{}{"workflow_action_draft": draft}, draft.RecommendedFocusContext, draft.RecommendedToolCall, draft.NonExecutingBoundary, err)
	case "escalation_route":
		suggestion, err := app.GroupDiscussionSuggestEscalationRoute(stringVal(args, "consultation_id"))
		return groupDiscussionResultWithSafeHandoff(map[string]interface{}{"escalation_route": suggestion}, suggestion.RecommendedFocusContext, suggestion.RecommendedToolCall, suggestion.NonExecutingBoundary, err)
	case "rollback_readiness":
		readiness, err := app.GroupDiscussionGetRollbackReadiness(stringVal(args, "consultation_id"), firstNonEmptyGroupString(stringVal(args, "rollback_evidence"), stringVal(args, "evidence"), stringVal(args, "content")))
		return groupDiscussionResultWithSafeHandoff(map[string]interface{}{"rollback_readiness": readiness}, readiness.RecommendedFocusContext, readiness.RecommendedToolCall, readiness.NonExecutingBoundary, err)
	case "readiness":
		readiness, err := app.GroupDiscussionGetReadiness(stringVal(args, "consultation_id"))
		return groupDiscussionResultWithSafeHandoff(map[string]interface{}{"readiness": readiness}, readiness.RecommendedFocusContext, readiness.RecommendedToolCall, readiness.NonExecutingBoundary, err)
	case "summarize_result":
		preview := groupDiscussionBool(args["preview"])
		result, err := app.GroupDiscussionSummarizeResult(GroupDiscussionSummarizeRequest{ConsultationID: stringVal(args, "consultation_id"), Submit: groupDiscussionBool(args["submit"]), Inject: groupDiscussionBool(args["inject"]), Force: groupDiscussionBool(args["force"]), Preview: preview})
		if preview {
			return groupDiscussionResultWithSafeHandoff(map[string]interface{}{"summary": result}, result.RecommendedFocusContext, result.RecommendedToolCall, result.NonExecutingBoundary, err)
		}
		return groupDiscussionResult(map[string]interface{}{"summary": result}, err)
	case "cleanup_stale":
		dryRun := groupDiscussionBool(args["dry_run"])
		result, err := app.GroupDiscussionCleanupStale(GroupDiscussionStaleCleanupRequest{DryRun: dryRun})
		if dryRun {
			focusContext := groupDiscussionStaleCleanupFocusContext(result)
			return groupDiscussionResultWithSafeHandoff(map[string]interface{}{"cleanup": result}, focusContext, groupDiscussionStaleCleanupToolCall(focusContext), groupDiscussionStaleCleanupNonExecutingBoundary, err)
		}
		return groupDiscussionResult(map[string]interface{}{"cleanup": result}, err)
	case "process_invites":
		invites, err := app.GroupDiscussionProcessPendingInvites()
		return groupDiscussionResult(map[string]interface{}{"pending_invites": invites}, err)
	case "suggest":
		return groupDiscussionSuggest(app, args)
	case "start_authorized":
		if err := groupDiscussionAuthorizeStartGate(app, handler, args); err != nil {
			return groupDiscussionResult(nil, err)
		}
		result, err := app.GroupDiscussionStartAuthorizedConsultation(GroupDiscussionAuthorizedStartRequest{
			Request:    groupDiscussionRequestFromArgs(app, args),
			InviteeIDs: groupDiscussionStringSlice(args["invitee_ids"]),
			Role:       a2a.GroupRole(strings.TrimSpace(stringVal(args, "role"))),
			Trusted:    groupDiscussionBool(args["trusted"]),
		})
		return groupDiscussionResult(result, err)
	case "send_message":
		consultationID := strings.TrimSpace(stringVal(args, "consultation_id"))
		msg := a2a.GroupDiscussionMessage{Kind: groupDiscussionMessageKind(stringVal(args, "message_kind")), Content: strings.TrimSpace(stringVal(args, "content")), CreatedAt: time.Now()}
		return groupDiscussionResult(map[string]interface{}{"sent": true, "consultation_id": consultationID}, app.GroupDiscussionSendMessage(consultationID, msg))
	case "add_proposal":
		consultationID := strings.TrimSpace(stringVal(args, "consultation_id"))
		proposal := a2a.Proposal{AuthorID: strings.TrimSpace(stringVal(args, "author_id")), Title: strings.TrimSpace(firstNonEmptyGroupString(stringVal(args, "proposal_title"), stringVal(args, "title"))), Content: strings.TrimSpace(firstNonEmptyGroupString(stringVal(args, "proposal_content"), stringVal(args, "content"))), Goals: groupDiscussionStringSlice(args["goals"]), Constraints: groupDiscussionStringSlice(args["constraints"]), Risks: groupDiscussionStringSlice(args["risks"]), CreatedAt: time.Now()}
		return groupDiscussionResult(map[string]interface{}{"proposal_added": true, "consultation_id": consultationID}, app.GroupDiscussionAddProposal(consultationID, proposal))
	case "add_review":
		consultationID := strings.TrimSpace(stringVal(args, "consultation_id"))
		review := a2a.Review{ProposalID: strings.TrimSpace(stringVal(args, "proposal_id")), ReviewerID: strings.TrimSpace(stringVal(args, "reviewer_id")), Position: groupDiscussionReviewPosition(stringVal(args, "review_position")), Comment: strings.TrimSpace(stringVal(args, "content")), CreatedAt: time.Now()}
		return groupDiscussionResult(map[string]interface{}{"review_added": true, "consultation_id": consultationID, "proposal_id": review.ProposalID}, app.GroupDiscussionAddReview(consultationID, review))
	case "decide":
		consultationID := strings.TrimSpace(stringVal(args, "consultation_id"))
		decision := a2a.Decision{
			ProposalID: strings.TrimSpace(stringVal(args, "proposal_id")),
			Summary:    strings.TrimSpace(firstNonEmptyGroupString(stringVal(args, "decision_summary"), stringVal(args, "summary"))),
			Rationale:  strings.TrimSpace(stringVal(args, "rationale")),
			RollbackOn: groupDiscussionStringSlice(args["rollback_on"]),
			CreatedAt:  time.Now(),
		}
		return groupDiscussionResult(map[string]interface{}{"decision_recorded": true, "consultation_id": consultationID, "proposal_id": decision.ProposalID}, app.GroupDiscussionDecide(consultationID, decision))
	case "escalate":
		consultationID := strings.TrimSpace(stringVal(args, "consultation_id"))
		escalation := a2a.Escalation{RaisedBy: strings.TrimSpace(stringVal(args, "raised_by")), Reason: strings.TrimSpace(firstNonEmptyGroupString(stringVal(args, "escalation_reason"), stringVal(args, "reason"), stringVal(args, "content"))), Target: strings.TrimSpace(firstNonEmptyGroupString(stringVal(args, "escalation_target"), stringVal(args, "target"))), CreatedAt: time.Now()}
		escalation = normalizeGroupDiscussionEscalation(escalation, "")
		return groupDiscussionResult(map[string]interface{}{"escalated": true, "consultation_id": consultationID, "target": escalation.Target}, app.GroupDiscussionEscalate(consultationID, escalation))
	case "submit_result":
		consultationID := strings.TrimSpace(stringVal(args, "consultation_id"))
		result := a2a.GroupDiscussionResult{Summary: strings.TrimSpace(stringVal(args, "summary")), Rationale: strings.TrimSpace(stringVal(args, "rationale")), Risks: groupDiscussionStringSlice(args["risks"]), CreatedAt: time.Now()}
		return groupDiscussionResult(map[string]interface{}{"submitted": true, "consultation_id": consultationID}, app.GroupDiscussionSubmitResult(consultationID, result))
	case "set_state":
		consultationID := strings.TrimSpace(stringVal(args, "consultation_id"))
		action := strings.TrimSpace(stringVal(args, "state_action"))
		return groupDiscussionResult(map[string]interface{}{"updated": true, "consultation_id": consultationID, "state_action": action}, app.GroupDiscussionSetState(consultationID, action))
	default:
		return "unsupported group_discussion action; use status, list_experts, rank_experts, list_mine, get_discussion, get_detail, workflow_state, workflow_action_draft, escalation_route, rollback_readiness, readiness, summarize_result, cleanup_stale, process_invites, suggest, start_authorized, send_message, add_proposal, add_review, decide, escalate, submit_result, or set_state"
	}
}

type GroupDiscussionWorkflowState struct {
	ConsultationID            string                                    `json:"consultation_id"`
	Topic                     string                                    `json:"topic,omitempty"`
	Question                  string                                    `json:"question,omitempty"`
	Status                    string                                    `json:"status,omitempty"`
	Readiness                 GroupDiscussionReadiness                  `json:"readiness"`
	MessageCount              int                                       `json:"message_count"`
	ProposalCount             int                                       `json:"proposal_count"`
	ReviewCount               int                                       `json:"review_count"`
	OpenProposalCount         int                                       `json:"open_proposal_count"`
	DecidableProposalCount    int                                       `json:"decidable_proposal_count"`
	BlockingReviewCount       int                                       `json:"blocking_review_count"`
	MissingAnswerParticipants []string                                  `json:"missing_answer_participants,omitempty"`
	WorkflowBlockers          []GroupDiscussionWorkflowBlocker          `json:"workflow_blockers,omitempty"`
	HasDecision               bool                                      `json:"has_decision"`
	HasEscalation             bool                                      `json:"has_escalation"`
	HasResult                 bool                                      `json:"has_result"`
	Decision                  *a2a.Decision                             `json:"decision,omitempty"`
	Escalation                *a2a.Escalation                           `json:"escalation,omitempty"`
	Proposals                 []GroupDiscussionProposalWorkflowState    `json:"proposals,omitempty"`
	SuggestedNextActionKind   string                                    `json:"suggested_next_action_kind,omitempty"`
	SuggestedNextAction       string                                    `json:"suggested_next_action,omitempty"`
	RecommendedFocusContext   map[string]interface{}                    `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall       *GroupDiscussionToolCallSuggestion        `json:"recommended_tool_call,omitempty"`
	EscalationRoute           *GroupDiscussionEscalationRouteSuggestion `json:"escalation_route,omitempty"`
	RollbackReadiness         *GroupDiscussionRollbackReadiness         `json:"rollback_readiness,omitempty"`
	ActionDraft               *GroupDiscussionWorkflowActionDraft       `json:"workflow_action_draft,omitempty"`
	NonExecutingBoundary      string                                    `json:"non_executing_boundary"`
}

type GroupDiscussionProposalWorkflowState struct {
	ID               string                           `json:"id"`
	Title            string                           `json:"title,omitempty"`
	AuthorID         string                           `json:"author_id,omitempty"`
	Status           string                           `json:"status,omitempty"`
	ReviewSummary    a2a.ReviewSummary                `json:"review_summary"`
	ReviewCount      int                              `json:"review_count"`
	PolicySatisfied  bool                             `json:"policy_satisfied"`
	BlockingReviews  bool                             `json:"blocking_reviews"`
	MissingReviewers []string                         `json:"missing_reviewers,omitempty"`
	Blockers         []GroupDiscussionWorkflowBlocker `json:"blockers,omitempty"`
}

type GroupDiscussionWorkflowBlocker struct {
	Code         string   `json:"code"`
	Severity     string   `json:"severity,omitempty"`
	Message      string   `json:"message"`
	ProposalID   string   `json:"proposal_id,omitempty"`
	ProposalIDs  []string `json:"proposal_ids,omitempty"`
	Participants []string `json:"participants,omitempty"`
	Count        int      `json:"count,omitempty"`
}

type GroupDiscussionEscalationRouteSuggestion struct {
	ConsultationID          string                             `json:"consultation_id"`
	Status                  string                             `json:"status,omitempty"`
	Target                  string                             `json:"target"`
	Reason                  string                             `json:"reason"`
	Suggested               bool                               `json:"suggested"`
	RecommendedFocusContext map[string]interface{}             `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall     *GroupDiscussionToolCallSuggestion `json:"recommended_tool_call,omitempty"`
	Triggers                []string                           `json:"triggers,omitempty"`
	PolicyEvidence          []string                           `json:"policy_evidence,omitempty"`
	SuggestedNextActionKind string                             `json:"suggested_next_action_kind,omitempty"`
	BlockingReviewCount     int                                `json:"blocking_review_count,omitempty"`
	DecidableProposalCount  int                                `json:"decidable_proposal_count,omitempty"`
	ExistingEscalation      *a2a.Escalation                    `json:"existing_escalation,omitempty"`
	NonExecutingBoundary    string                             `json:"non_executing_boundary"`
}

type GroupDiscussionRollbackReadiness struct {
	ConsultationID          string                             `json:"consultation_id"`
	HasDecision             bool                               `json:"has_decision"`
	ProposalID              string                             `json:"proposal_id,omitempty"`
	DecisionSummary         string                             `json:"decision_summary,omitempty"`
	DecisionRationale       string                             `json:"decision_rationale,omitempty"`
	RollbackOn              []string                           `json:"rollback_on,omitempty"`
	MatchedTriggers         []string                           `json:"matched_triggers,omitempty"`
	UnmatchedTriggers       []string                           `json:"unmatched_triggers,omitempty"`
	Evidence                []string                           `json:"evidence,omitempty"`
	Suggested               bool                               `json:"suggested"`
	RecommendedFocusContext map[string]interface{}             `json:"recommended_focus_context,omitempty"`
	SuggestedNextActionKind string                             `json:"suggested_next_action_kind,omitempty"`
	SuggestedNextAction     string                             `json:"suggested_next_action,omitempty"`
	RecommendedToolCall     *GroupDiscussionToolCallSuggestion `json:"recommended_tool_call,omitempty"`
	NonExecutingBoundary    string                             `json:"non_executing_boundary"`
}

type GroupDiscussionWorkflowActionDraft struct {
	ConsultationID          string                             `json:"consultation_id"`
	ActionKind              string                             `json:"action_kind"`
	Title                   string                             `json:"title"`
	Summary                 string                             `json:"summary"`
	RecommendedFocusContext map[string]interface{}             `json:"recommended_focus_context,omitempty"`
	SuggestedNextActionKind string                             `json:"suggested_next_action_kind,omitempty"`
	ProposalID              string                             `json:"proposal_id,omitempty"`
	TargetParticipants      []string                           `json:"target_participants,omitempty"`
	TargetProposalIDs       []string                           `json:"target_proposal_ids,omitempty"`
	EscalationTarget        string                             `json:"escalation_target,omitempty"`
	EscalationReason        string                             `json:"escalation_reason,omitempty"`
	Evidence                []string                           `json:"evidence,omitempty"`
	RiskBoundaries          []string                           `json:"risk_boundaries,omitempty"`
	Checklist               []string                           `json:"checklist,omitempty"`
	SuggestedArguments      map[string]interface{}             `json:"suggested_arguments,omitempty"`
	RecommendedToolCall     *GroupDiscussionToolCallSuggestion `json:"recommended_tool_call,omitempty"`
	RequiresConfirmation    bool                               `json:"requires_confirmation"`
	NonExecutingBoundary    string                             `json:"non_executing_boundary"`
}

type GroupDiscussionToolCallSuggestion struct {
	Tool                    string                 `json:"tool"`
	Args                    map[string]interface{} `json:"args,omitempty"`
	RecommendedFocusContext map[string]interface{} `json:"recommended_focus_context,omitempty"`
	DiscussionFocusContext  map[string]interface{} `json:"discussion_focus_context,omitempty"`
	NonExecuting            bool                   `json:"non_executing"`
	NonExecutingBoundary    string                 `json:"non_executing_boundary"`
}

func groupDiscussionWorkflowState(detail a2a.HubDiscussionDetail) GroupDiscussionWorkflowState {
	state := groupDiscussionWorkflowStateCore(detail)
	route := groupDiscussionEscalationRouteSuggestionFromState(detail, state)
	state.EscalationRoute = &route
	rollback := groupDiscussionRollbackReadiness(detail, "")
	if rollback.HasDecision || len(rollback.RollbackOn) > 0 {
		state.RollbackReadiness = &rollback
	}
	draft := groupDiscussionWorkflowActionDraft(detail, state)
	state.ActionDraft = &draft
	state.RecommendedFocusContext = groupDiscussionWorkflowStateFocusContext(state)
	state.RecommendedToolCall = groupDiscussionWorkflowStateToolCall(state)
	state.RecommendedToolCall = normalizeGroupDiscussionSafeToolCall(state.RecommendedToolCall, state.RecommendedFocusContext, state.NonExecutingBoundary)
	return state
}

func groupDiscussionWorkflowStateCore(detail a2a.HubDiscussionDetail) GroupDiscussionWorkflowState {
	readiness := groupDiscussionReadiness(detail)
	readiness.ConsultationID = strings.TrimSpace(detail.Discussion.ID)
	status := firstNonEmptyGroupString(readiness.Status, detail.Discussion.Status)
	decision := groupDiscussionDecision(detail)
	escalation := groupDiscussionEscalation(detail)
	proposals := groupDiscussionProposalWorkflowStates(detail)
	state := GroupDiscussionWorkflowState{
		ConsultationID:            strings.TrimSpace(detail.Discussion.ID),
		Topic:                     strings.TrimSpace(detail.Discussion.Topic),
		Question:                  strings.TrimSpace(detail.Discussion.Question),
		Status:                    status,
		Readiness:                 readiness,
		MessageCount:              len(detail.Messages),
		ProposalCount:             len(proposals),
		ReviewCount:               len(detail.Reviews),
		MissingAnswerParticipants: missingGroupDiscussionAnswerParticipants(detail),
		HasDecision:               decision != nil,
		HasEscalation:             escalation != nil,
		HasResult:                 strings.TrimSpace(detail.Discussion.ResultSummary) != "" || decision != nil,
		Decision:                  decision,
		Escalation:                escalation,
		Proposals:                 proposals,
		NonExecutingBoundary:      "read-only workflow state; no proposal, review, decision, escalation, message, result submission, or discussion state change was performed",
	}
	for _, proposal := range proposals {
		if proposal.Status == "" || proposal.Status == string(a2a.ProposalOpen) {
			state.OpenProposalCount++
		}
		if proposal.PolicySatisfied {
			state.DecidableProposalCount++
		}
		if proposal.BlockingReviews {
			state.BlockingReviewCount += proposal.ReviewSummary.Concerns + proposal.ReviewSummary.Rejections
		}
	}
	state.WorkflowBlockers = groupDiscussionWorkflowBlockers(state)
	state.SuggestedNextActionKind, state.SuggestedNextAction = groupDiscussionWorkflowNextAction(state)
	return state
}

func groupDiscussionWorkflowStateFocusContext(state GroupDiscussionWorkflowState) map[string]interface{} {
	consultationID := strings.TrimSpace(state.ConsultationID)
	if consultationID == "" {
		return nil
	}
	ctx := map[string]interface{}{
		"consultation_id": consultationID,
		"action_kind":     "workflow_state",
		"status":          strings.TrimSpace(state.Status),
		"reason":          strings.TrimSpace(firstNonEmptyGroupString(state.SuggestedNextAction, state.Readiness.Reason, "read-only A2A workflow state inspection")),
	}
	if next := strings.TrimSpace(state.SuggestedNextActionKind); next != "" {
		ctx["suggested_next_action_kind"] = next
	}
	if len(state.MissingAnswerParticipants) > 0 {
		ctx["target_participants"] = append([]string{}, state.MissingAnswerParticipants...)
	}
	if state.DecidableProposalCount > 0 {
		ctx["decidable_proposal_count"] = state.DecidableProposalCount
	}
	if state.BlockingReviewCount > 0 {
		ctx["blocking_review_count"] = state.BlockingReviewCount
	}
	return ctx
}

func groupDiscussionWorkflowStateToolCall(state GroupDiscussionWorkflowState) *GroupDiscussionToolCallSuggestion {
	consultationID := strings.TrimSpace(state.ConsultationID)
	if consultationID == "" {
		return nil
	}
	focusContext := groupDiscussionWorkflowStateFocusContext(state)
	return &GroupDiscussionToolCallSuggestion{
		Tool:                    "group_discussion",
		Args:                    map[string]interface{}{"action": "workflow_state", "consultation_id": consultationID},
		RecommendedFocusContext: focusContext,
		DiscussionFocusContext:  focusContext,
		NonExecuting:            true,
		NonExecutingBoundary:    "recommended group_discussion workflow state inspection only; it must not send messages, invite experts, add proposals or reviews, decide, escalate, submit results, change discussion state, mutate memory, or change routing",
	}
}

func groupDiscussionEscalationRouteSuggestion(detail a2a.HubDiscussionDetail) GroupDiscussionEscalationRouteSuggestion {
	return groupDiscussionEscalationRouteSuggestionFromState(detail, groupDiscussionWorkflowStateCore(detail))
}

func groupDiscussionEscalationRouteSuggestionFromState(detail a2a.HubDiscussionDetail, state GroupDiscussionWorkflowState) GroupDiscussionEscalationRouteSuggestion {
	policy := groupDiscussionEscalationRoutePolicy(detail)
	suggestion := GroupDiscussionEscalationRouteSuggestion{
		ConsultationID:          strings.TrimSpace(state.ConsultationID),
		Status:                  strings.TrimSpace(state.Status),
		Target:                  defaultGroupDiscussionEscalationTarget(),
		SuggestedNextActionKind: strings.TrimSpace(state.SuggestedNextActionKind),
		BlockingReviewCount:     state.BlockingReviewCount,
		DecidableProposalCount:  state.DecidableProposalCount,
		NonExecutingBoundary:    "read-only escalation route suggestion; no escalation was sent and no discussion state changed",
	}
	finalize := func() GroupDiscussionEscalationRouteSuggestion {
		suggestion.RecommendedFocusContext = groupDiscussionEscalationRouteFocusContext(suggestion)
		suggestion.RecommendedToolCall = groupDiscussionEscalationRouteToolCall(suggestion)
		suggestion.RecommendedToolCall = normalizeGroupDiscussionSafeToolCall(suggestion.RecommendedToolCall, suggestion.RecommendedFocusContext, suggestion.NonExecutingBoundary)
		return suggestion
	}
	if escalation := groupDiscussionEscalation(detail); escalation != nil {
		suggestion.Target = firstNonEmptyGroupString(escalation.Target, suggestion.Target)
		suggestion.Reason = firstNonEmptyGroupString(escalation.Reason, "Discussion is already escalated; wait for the escalation owner before changing state.")
		suggestion.Triggers = []string{"existing_escalation"}
		suggestion.ExistingEscalation = escalation
		return finalize()
	}
	if state.HasDecision || state.HasResult {
		suggestion.Reason = "A result or decision already exists; inspect rationale and rollback constraints before escalating."
		suggestion.Triggers = []string{"result_available"}
		return finalize()
	}
	if state.BlockingReviewCount > 0 {
		suggestion.Suggested = true
		suggestion.Target = firstNonEmptyGroupString(policy.Target, suggestion.Target)
		suggestion.Reason = groupDiscussionRouteReason(policy, fmt.Sprintf("%d blocking proposal review(s) need an owner decision before this discussion should be finalized.", state.BlockingReviewCount))
		suggestion.Triggers = append(append([]string{}, policy.Triggers...), "blocking_reviews")
		suggestion.PolicyEvidence = groupDiscussionEscalationPolicyEvidence(policy)
		if state.DecidableProposalCount > 0 {
			suggestion.Triggers = append(suggestion.Triggers, "mixed_decidable_and_blocking_reviews")
		}
		suggestion.Triggers = dedupeGroupDiscussionStrings(suggestion.Triggers)
		return finalize()
	}
	switch strings.ToLower(strings.TrimSpace(state.Status)) {
	case string(a2a.SessionEscalated):
		suggestion.Reason = "Discussion status is already escalated; wait for the escalation owner before changing state."
		suggestion.Triggers = []string{"status_escalated"}
		return finalize()
	case string(a2a.SessionClosed):
		suggestion.Suggested = true
		suggestion.Target = firstNonEmptyGroupString(policy.Target, suggestion.Target)
		suggestion.Reason = groupDiscussionRouteReason(policy, "Discussion is closed without a result or decision; route to the owner for final disposition.")
		suggestion.Triggers = dedupeGroupDiscussionStrings(append(append([]string{}, policy.Triggers...), "closed_without_result"))
		suggestion.PolicyEvidence = groupDiscussionEscalationPolicyEvidence(policy)
		return finalize()
	}
	switch {
	case state.ProposalCount > 0:
		suggestion.Reason = "Open proposals still need review or decision work before escalation is useful."
		suggestion.Triggers = []string{"proposal_reviews_pending"}
	case state.Readiness.Ready:
		suggestion.Reason = "Expert answers are ready for summary or decision; preview the result before escalating."
		suggestion.Triggers = []string{"summary_ready"}
	case state.Readiness.AnswerCount > 0:
		suggestion.Reason = "Some expert answers are present; send a targeted follow-up or wait for remaining answers before escalation."
		suggestion.Triggers = []string{"partial_answers"}
	default:
		suggestion.Reason = "No blocking signal yet; wait for expert answers before escalating."
		suggestion.Triggers = []string{"waiting_for_expert_answers"}
	}
	return finalize()
}

func groupDiscussionEscalationRouteFocusContext(route GroupDiscussionEscalationRouteSuggestion) map[string]interface{} {
	consultationID := strings.TrimSpace(route.ConsultationID)
	if consultationID == "" {
		return nil
	}
	actionKind := "review_escalation_route"
	if route.Suggested {
		actionKind = "prepare_escalation"
	}
	ctx := map[string]interface{}{
		"consultation_id": consultationID,
		"action_kind":     actionKind,
		"reason":          strings.TrimSpace(route.Reason),
	}
	if target := strings.TrimSpace(route.Target); target != "" {
		ctx["escalation_target"] = target
	}
	if len(route.Triggers) > 0 {
		ctx["triggers"] = append([]string{}, route.Triggers...)
	}
	if next := strings.TrimSpace(route.SuggestedNextActionKind); next != "" {
		ctx["suggested_next_action_kind"] = next
	}
	return ctx
}

func groupDiscussionEscalationRouteToolCall(route GroupDiscussionEscalationRouteSuggestion) *GroupDiscussionToolCallSuggestion {
	consultationID := strings.TrimSpace(route.ConsultationID)
	if consultationID == "" {
		return nil
	}
	focusContext := groupDiscussionEscalationRouteFocusContext(route)
	return &GroupDiscussionToolCallSuggestion{
		Tool:                    "group_discussion",
		Args:                    map[string]interface{}{"action": "escalation_route", "consultation_id": consultationID},
		RecommendedFocusContext: focusContext,
		DiscussionFocusContext:  focusContext,
		NonExecuting:            true,
		NonExecutingBoundary:    "recommended group_discussion escalation route inspection only; it must not escalate, decide, submit results, send messages, invite experts, change discussion state, mutate memory, or change routing",
	}
}

type groupDiscussionEscalationRoutePolicyResult struct {
	Target          string
	Reason          string
	Triggers        []string
	MatchedKeywords []string
}

func groupDiscussionEscalationRoutePolicy(detail a2a.HubDiscussionDetail) groupDiscussionEscalationRoutePolicyResult {
	haystack := groupDiscussionEscalationPolicyText(detail)
	for _, policy := range []struct {
		target   string
		reason   string
		trigger  string
		keywords []string
	}{
		{
			target:   "security-governance",
			reason:   "Discussion metadata includes security or compliance signals; route to the governance owner.",
			trigger:  "security_or_compliance_metadata",
			keywords: []string{"security", "credential", "secret", "privacy", "compliance", "legal", "permission", "auth", "token", "tenant", "encryption", "pii"},
		},
		{
			target:   "release-owner",
			reason:   "Discussion metadata includes release or rollback signals; route to the release owner.",
			trigger:  "release_or_rollback_metadata",
			keywords: []string{"rollback", "rollout", "release", "deployment", "deploy", "production", "prod", "incident", "outage", "error budget", "slo"},
		},
		{
			target:   "experience-learning-owner",
			reason:   "Discussion metadata includes tool routing or self-evolution signals; route to the experience-learning owner.",
			trigger:  "self_evolution_metadata",
			keywords: []string{"tool routing", "self-evolution", "self evolution", "routing hint", "skill nudge", "skill", "router", "automation"},
		},
	} {
		matches := groupDiscussionMatchedKeywords(haystack, policy.keywords)
		if len(matches) > 0 {
			return groupDiscussionEscalationRoutePolicyResult{
				Target:          policy.target,
				Reason:          policy.reason,
				Triggers:        []string{policy.trigger},
				MatchedKeywords: matches,
			}
		}
	}
	return groupDiscussionEscalationRoutePolicyResult{}
}

func groupDiscussionEscalationPolicyText(detail a2a.HubDiscussionDetail) string {
	parts := []string{
		detail.Discussion.Topic,
		detail.Discussion.Question,
		detail.Discussion.ResultSummary,
	}
	if detail.Session != nil {
		parts = append(parts, detail.Session.Topic, detail.Session.Goal)
		if detail.Session.Decision != nil {
			parts = append(parts, detail.Session.Decision.Summary, detail.Session.Decision.Rationale)
			parts = append(parts, detail.Session.Decision.RollbackOn...)
		}
		if detail.Session.Escalation != nil {
			parts = append(parts, detail.Session.Escalation.Reason, detail.Session.Escalation.Target)
		}
	}
	for _, proposal := range append(append([]a2a.Proposal{}, detail.Proposals...), groupDiscussionSessionProposals(detail)...) {
		parts = append(parts, proposal.Title, proposal.Content)
		parts = append(parts, proposal.Goals...)
		parts = append(parts, proposal.Constraints...)
		parts = append(parts, proposal.Risks...)
	}
	for _, review := range append(append([]a2a.Review{}, detail.Reviews...), groupDiscussionSessionReviews(detail)...) {
		parts = append(parts, review.Comment)
	}
	for _, message := range detail.Messages {
		parts = append(parts, message.Content)
		parts = append(parts, message.Evidence...)
	}
	return strings.ToLower(strings.Join(parts, "\n"))
}

func groupDiscussionSessionProposals(detail a2a.HubDiscussionDetail) []a2a.Proposal {
	if detail.Session == nil {
		return nil
	}
	return detail.Session.Proposals
}

func groupDiscussionSessionReviews(detail a2a.HubDiscussionDetail) []a2a.Review {
	if detail.Session == nil {
		return nil
	}
	return detail.Session.Reviews
}

func groupDiscussionMatchedKeywords(haystack string, needles []string) []string {
	matches := make([]string, 0, len(needles))
	for _, needle := range needles {
		needle = strings.ToLower(strings.TrimSpace(needle))
		if needle == "" {
			continue
		}
		if strings.Contains(haystack, needle) {
			matches = append(matches, needle)
		}
	}
	matches = dedupeGroupDiscussionStrings(matches)
	if len(matches) > 6 {
		return matches[:6]
	}
	return matches
}

func groupDiscussionRouteReason(policy groupDiscussionEscalationRoutePolicyResult, reason string) string {
	if strings.TrimSpace(policy.Reason) == "" {
		return reason
	}
	return strings.TrimSpace(policy.Reason + " " + strings.TrimSpace(reason))
}

func groupDiscussionEscalationPolicyEvidence(policy groupDiscussionEscalationRoutePolicyResult) []string {
	if strings.TrimSpace(policy.Target) == "" {
		return nil
	}
	evidence := []string{"policy_target: " + strings.TrimSpace(policy.Target)}
	if strings.TrimSpace(policy.Reason) != "" {
		evidence = append(evidence, "policy_reason: "+strings.TrimSpace(policy.Reason))
	}
	if len(policy.Triggers) > 0 {
		evidence = append(evidence, "policy_triggers: "+strings.Join(dedupeGroupDiscussionStrings(policy.Triggers), ", "))
	}
	if len(policy.MatchedKeywords) > 0 {
		evidence = append(evidence, "matched_keywords: "+strings.Join(dedupeGroupDiscussionStrings(policy.MatchedKeywords), ", "))
	}
	return dedupeGroupDiscussionStrings(evidence)
}

func groupDiscussionWorkflowActionDraft(detail a2a.HubDiscussionDetail, state GroupDiscussionWorkflowState) GroupDiscussionWorkflowActionDraft {
	consultationID := strings.TrimSpace(firstNonEmptyGroupString(state.ConsultationID, detail.Discussion.ID))
	route := state.EscalationRoute
	if route == nil {
		computed := groupDiscussionEscalationRouteSuggestionFromState(detail, state)
		route = &computed
	}
	draft := GroupDiscussionWorkflowActionDraft{
		ConsultationID:          consultationID,
		SuggestedNextActionKind: strings.TrimSpace(state.SuggestedNextActionKind),
		RequiresConfirmation:    true,
		NonExecutingBoundary:    "read-only workflow action draft; no Hub state changed and no tool was executed",
		RiskBoundaries:          groupDiscussionWorkflowDraftBoundaries(true),
	}
	if state.HasEscalation {
		draft.ActionKind = "review_escalation_handoff"
		draft.Title = "Review escalation owner handoff"
		draft.Summary = "This discussion is already escalated; inspect the escalation owner, reason, and current state before taking follow-up action."
		draft.RequiresConfirmation = false
		draft.RiskBoundaries = groupDiscussionWorkflowDraftBoundaries(false)
		draft.Evidence = groupDiscussionEscalationStateEvidence(state)
		draft.Checklist = []string{
			"Inspect the escalation target and reason.",
			"Wait for the escalation owner or record a human-approved follow-up outside this draft.",
			"Do not send another escalation from this advisory draft.",
		}
		draft.SuggestedArguments = map[string]interface{}{"action": "get_detail", "consultation_id": consultationID}
		return finalizeGroupDiscussionWorkflowActionDraft(draft)
	}
	rollbackReadiness := state.RollbackReadiness
	if rollbackReadiness == nil {
		computed := groupDiscussionRollbackReadiness(detail, "")
		if computed.HasDecision || len(computed.RollbackOn) > 0 {
			rollbackReadiness = &computed
		}
	}
	if (state.HasDecision || state.HasResult) && rollbackReadiness != nil && rollbackReadiness.Suggested {
		draft.ActionKind = "prepare_rollback_review"
		draft.Title = "Prepare rollback review"
		draft.Summary = "Rollback triggers are matched by current discussion evidence; review them and prepare a human-approved rollback workflow without executing rollback here."
		draft.RequiresConfirmation = false
		draft.RiskBoundaries = groupDiscussionWorkflowDraftBoundaries(false)
		draft.Evidence = groupDiscussionRollbackWorkflowDraftEvidence(*rollbackReadiness)
		draft.Checklist = []string{
			"Inspect the matched rollback triggers and supporting evidence.",
			"Draft a human-approved rollback workflow outside this advisory step.",
			"Do not execute rollback, close the discussion, or rewrite memory from this draft.",
		}
		draft.SuggestedArguments = map[string]interface{}{"action": "rollback_readiness", "consultation_id": consultationID}
		return finalizeGroupDiscussionWorkflowActionDraft(draft)
	}
	if state.HasDecision || state.HasResult {
		draft.ActionKind = "review_result_reuse"
		draft.Title = "Review existing result"
		draft.Summary = "A result or decision already exists; inspect rationale, review evidence, and rollback triggers before reusing it."
		draft.RequiresConfirmation = false
		draft.RiskBoundaries = groupDiscussionWorkflowDraftBoundaries(false)
		draft.Evidence = groupDiscussionResultDraftEvidence(state)
		draft.Checklist = []string{
			"Inspect decision rationale and rollback triggers.",
			"Compare proposal review totals before reusing the result.",
			"Create a separate human-approved follow-up if the result needs revision.",
		}
		draft.SuggestedArguments = map[string]interface{}{"action": "get_detail", "consultation_id": consultationID}
		return finalizeGroupDiscussionWorkflowActionDraft(draft)
	}
	if route != nil && route.Suggested {
		draft.ActionKind = "prepare_escalation"
		draft.Title = "Prepare escalation to " + firstNonEmptyGroupString(route.Target, defaultGroupDiscussionEscalationTarget())
		draft.Summary = strings.TrimSpace(route.Reason)
		draft.EscalationTarget = firstNonEmptyGroupString(route.Target, defaultGroupDiscussionEscalationTarget())
		draft.EscalationReason = strings.TrimSpace(route.Reason)
		draft.Evidence = groupDiscussionEscalationDraftEvidence(state, route)
		draft.Checklist = []string{
			"Review blocking proposal concerns or closed-without-result evidence.",
			"Confirm the escalation owner and reason with the human.",
			"Only after confirmation, call group_discussion with action=escalate.",
		}
		draft.SuggestedArguments = map[string]interface{}{"action": "escalate", "consultation_id": consultationID, "escalation_reason": draft.EscalationReason, "escalation_target": draft.EscalationTarget}
		return finalizeGroupDiscussionWorkflowActionDraft(draft)
	}
	if proposal := firstDecidableGroupDiscussionProposal(state); proposal != nil {
		draft.ActionKind = "record_decision"
		draft.Title = "Prepare decision for " + firstNonEmptyGroupString(proposal.Title, proposal.ID)
		draft.Summary = "A proposal has enough non-blocking approvals; prepare a decision rationale and rollback triggers before recording it."
		draft.ProposalID = strings.TrimSpace(proposal.ID)
		draft.TargetProposalIDs = []string{draft.ProposalID}
		draft.Evidence = groupDiscussionDecisionDraftEvidence(*proposal)
		draft.Checklist = []string{
			"Inspect the proposal content and review summary.",
			"Write a concise decision rationale.",
			"List rollback conditions before recording the decision.",
		}
		draft.SuggestedArguments = map[string]interface{}{
			"action":              "decide",
			"consultation_id":     consultationID,
			"proposal_id":         draft.ProposalID,
			"target_proposal_ids": draft.TargetProposalIDs,
			"decision_summary":    firstNonEmptyGroupString(proposal.Title, proposal.ID),
			"rationale":           groupDiscussionDecisionRationaleDraft(*proposal),
			"rollback_on":         groupDiscussionDecisionRollbackDraft(*proposal),
		}
		return finalizeGroupDiscussionWorkflowActionDraft(draft)
	}
	if state.ProposalCount > 0 {
		draft.ActionKind = "collect_reviews"
		draft.Title = "Prepare proposal review request"
		draft.Summary = "Open proposals need participant review before a decision can be recorded."
		draft.TargetParticipants = groupDiscussionReviewRequestParticipants(state)
		draft.TargetProposalIDs = groupDiscussionReviewRequestProposalIDs(state)
		draft.Evidence = groupDiscussionReviewCollectionEvidence(detail, state)
		draft.Checklist = []string{
			"Identify open proposals without enough review coverage.",
			"Ask missing reviewers for approve, reject, concern, or abstain positions.",
			"Ask for confirmation before sending the review request.",
		}
		draft.SuggestedArguments = map[string]interface{}{"action": "send_message", "consultation_id": consultationID, "message_kind": "question", "content": groupDiscussionReviewRequestContent(detail, state), "target_participants": draft.TargetParticipants, "target_proposal_ids": draft.TargetProposalIDs}
		return finalizeGroupDiscussionWorkflowActionDraft(draft)
	}
	if state.Readiness.Ready {
		draft.ActionKind = "preview_summary"
		draft.Title = "Preview discussion summary"
		draft.Summary = "Expert answers are ready; preview the layered summary before injecting or submitting any result."
		draft.Evidence = groupDiscussionReadinessDraftEvidence(state)
		draft.Checklist = []string{
			"Generate a preview-only summary.",
			"Inspect risks, disagreements, open questions, and participant contributions.",
			"Ask for confirmation before injecting to chat or submitting to Hub.",
		}
		draft.SuggestedArguments = map[string]interface{}{"action": "summarize_result", "consultation_id": consultationID, "preview": true}
		return finalizeGroupDiscussionWorkflowActionDraft(draft)
	}
	if state.Readiness.AnswerCount > 0 {
		draft.ActionKind = "send_followup"
		draft.Title = "Prepare targeted follow-up"
		draft.Summary = "Some expert answers are present, but the discussion is not ready; ask a focused follow-up or wait for missing answers."
		draft.TargetParticipants = append([]string{}, state.MissingAnswerParticipants...)
		draft.Evidence = groupDiscussionFollowupDraftEvidence(detail, state)
		draft.Checklist = []string{
			"Identify which participant or issue is missing.",
			"Keep the follow-up scoped to the shared Hub context.",
			"Ask for confirmation before sending the message.",
		}
		draft.SuggestedArguments = map[string]interface{}{"action": "send_message", "consultation_id": consultationID, "message_kind": "question", "content": groupDiscussionFollowupContentDraft(detail, state), "target_participants": draft.TargetParticipants}
		return finalizeGroupDiscussionWorkflowActionDraft(draft)
	}
	draft.ActionKind = "wait_for_answers"
	missingAnswers := missingGroupDiscussionAnswerParticipants(detail)
	if len(missingAnswers) > 0 {
		draft.Title = "Prepare expert answer reminder"
		draft.Summary = "No expert answer has landed yet; prepare a scoped reminder for missing expected answerers."
		draft.TargetParticipants = append([]string{}, missingAnswers...)
		draft.RequiresConfirmation = true
		draft.RiskBoundaries = groupDiscussionWorkflowDraftBoundaries(true)
		draft.Evidence = groupDiscussionWaitForAnswersDraftEvidence(detail, state)
		draft.Checklist = []string{
			"Identify expected answerers who have not replied.",
			"Keep the reminder scoped to the shared Hub question.",
			"Ask for confirmation before sending the reminder.",
		}
		draft.SuggestedArguments = map[string]interface{}{"action": "send_message", "consultation_id": consultationID, "message_kind": "question", "content": groupDiscussionInitialAnswerRequestContent(detail, state), "target_participants": draft.TargetParticipants}
		return finalizeGroupDiscussionWorkflowActionDraft(draft)
	}
	draft.Title = "Wait for expert answers"
	draft.Summary = "No actionable discussion evidence is ready yet; wait for expert answers before summarizing, deciding, or escalating."
	draft.RequiresConfirmation = false
	draft.RiskBoundaries = groupDiscussionWorkflowDraftBoundaries(false)
	draft.Evidence = groupDiscussionReadinessDraftEvidence(state)
	draft.Checklist = []string{"Monitor discussion readiness.", "Avoid summarizing or escalating without evidence."}
	return finalizeGroupDiscussionWorkflowActionDraft(draft)
}

func finalizeGroupDiscussionWorkflowActionDraft(draft GroupDiscussionWorkflowActionDraft) GroupDiscussionWorkflowActionDraft {
	draft.RecommendedFocusContext = groupDiscussionWorkflowDraftFocusContext(draft)
	draft.RecommendedToolCall = groupDiscussionRecommendedToolCallForDraft(draft)
	draft.RecommendedToolCall = normalizeGroupDiscussionSafeToolCall(draft.RecommendedToolCall, draft.RecommendedFocusContext, draft.NonExecutingBoundary)
	return draft
}

func groupDiscussionRecommendedToolCallForDraft(draft GroupDiscussionWorkflowActionDraft) *GroupDiscussionToolCallSuggestion {
	consultationID := strings.TrimSpace(draft.ConsultationID)
	if consultationID == "" {
		return nil
	}
	args := map[string]interface{}{"action": "workflow_action_draft", "consultation_id": consultationID}
	switch strings.TrimSpace(draft.ActionKind) {
	case "review_escalation_handoff":
		args["action"] = "get_detail"
	case "prepare_rollback_review":
		args["action"] = "rollback_readiness"
	case "review_result_reuse":
		args["action"] = "get_detail"
		for _, item := range draft.Evidence {
			if strings.HasPrefix(strings.TrimSpace(item), "rollback_on:") {
				args["action"] = "rollback_readiness"
				break
			}
		}
	case "preview_summary":
		args["action"] = "summarize_result"
		args["preview"] = true
	}
	boundary := "recommended group_discussion inspection or draft-building call only; it must not send messages, decide, escalate, submit results, invite experts, change discussion state, or mutate Hub state"
	if strings.TrimSpace(draft.NonExecutingBoundary) != "" {
		boundary = strings.TrimSpace(draft.NonExecutingBoundary) + "; " + boundary
	}
	focusContext := groupDiscussionWorkflowDraftFocusContext(draft)
	return &GroupDiscussionToolCallSuggestion{
		Tool:                    "group_discussion",
		Args:                    args,
		RecommendedFocusContext: focusContext,
		DiscussionFocusContext:  focusContext,
		NonExecuting:            true,
		NonExecutingBoundary:    boundary,
	}
}

func groupDiscussionWorkflowDraftFocusContext(draft GroupDiscussionWorkflowActionDraft) map[string]interface{} {
	consultationID := strings.TrimSpace(draft.ConsultationID)
	actionKind := strings.TrimSpace(draft.ActionKind)
	reason := strings.TrimSpace(firstNonEmptyGroupString(draft.Summary, draft.Title))
	if consultationID == "" && actionKind == "" && reason == "" {
		return nil
	}
	context := map[string]interface{}{
		"consultation_id": consultationID,
		"action_kind":     actionKind,
		"reason":          reason,
	}
	if proposalID := strings.TrimSpace(draft.ProposalID); proposalID != "" {
		context["proposal_id"] = proposalID
	}
	if len(draft.TargetParticipants) > 0 {
		context["target_participants"] = append([]string(nil), draft.TargetParticipants...)
	}
	if len(draft.TargetProposalIDs) > 0 {
		context["target_proposal_ids"] = append([]string(nil), draft.TargetProposalIDs...)
	}
	if target := strings.TrimSpace(draft.EscalationTarget); target != "" {
		context["escalation_target"] = target
	}
	return context
}

func groupDiscussionWorkflowDraftBoundaries(requiresConfirmation bool) []string {
	boundaries := []string{
		"Draft is advisory only and must not mutate Hub state by itself.",
		"Use only context already shared in the current-Hub discussion.",
	}
	if requiresConfirmation {
		boundaries = append(boundaries, "State-changing actions require explicit human confirmation before execution.")
	}
	return boundaries
}

func groupDiscussionDecision(detail a2a.HubDiscussionDetail) *a2a.Decision {
	if detail.Decision != nil {
		decision := *detail.Decision
		return &decision
	}
	if detail.Session == nil || detail.Session.Decision == nil {
		return nil
	}
	decision := *detail.Session.Decision
	return &decision
}

func groupDiscussionRollbackReadiness(detail a2a.HubDiscussionDetail, externalEvidence string) GroupDiscussionRollbackReadiness {
	consultationID := strings.TrimSpace(detail.Discussion.ID)
	readiness := GroupDiscussionRollbackReadiness{
		ConsultationID:       consultationID,
		NonExecutingBoundary: "read-only rollback readiness check; no rollback executed, no Hub state changed, no routing changed, and no memory was rewritten",
	}
	finalize := func() GroupDiscussionRollbackReadiness {
		readiness.RecommendedFocusContext = groupDiscussionRollbackReadinessFocusContext(readiness)
		readiness.RecommendedToolCall = groupDiscussionRollbackReadinessToolCall(readiness)
		readiness.RecommendedToolCall = normalizeGroupDiscussionSafeToolCall(readiness.RecommendedToolCall, readiness.RecommendedFocusContext, readiness.NonExecutingBoundary)
		return readiness
	}
	decision := groupDiscussionDecision(detail)
	if decision == nil {
		readiness.SuggestedNextActionKind = "no_decision"
		readiness.SuggestedNextAction = "Wait for a decision before checking rollback readiness."
		return finalize()
	}
	readiness.HasDecision = true
	readiness.ProposalID = strings.TrimSpace(decision.ProposalID)
	readiness.DecisionSummary = strings.TrimSpace(decision.Summary)
	readiness.DecisionRationale = strings.TrimSpace(decision.Rationale)
	readiness.RollbackOn = dedupeGroupDiscussionStrings(decision.RollbackOn)
	if len(readiness.RollbackOn) == 0 {
		readiness.SuggestedNextActionKind = "no_rollback_conditions"
		readiness.SuggestedNextAction = "Decision has no rollback triggers; inspect the result before reuse."
		readiness.Evidence = groupDiscussionRollbackReadinessEvidence(readiness, nil, nil, false)
		return finalize()
	}
	evidenceText, hasExternalEvidence := groupDiscussionRollbackEvidenceText(detail, externalEvidence)
	for _, trigger := range readiness.RollbackOn {
		if groupDiscussionRollbackTriggerMatched(evidenceText, trigger) {
			readiness.MatchedTriggers = append(readiness.MatchedTriggers, trigger)
			continue
		}
		readiness.UnmatchedTriggers = append(readiness.UnmatchedTriggers, trigger)
	}
	readiness.MatchedTriggers = dedupeGroupDiscussionStrings(readiness.MatchedTriggers)
	readiness.UnmatchedTriggers = dedupeGroupDiscussionStrings(readiness.UnmatchedTriggers)
	readiness.Suggested = len(readiness.MatchedTriggers) > 0
	if readiness.Suggested {
		readiness.SuggestedNextActionKind = "prepare_owner_approved_rollback_review"
		readiness.SuggestedNextAction = "Review matched rollback triggers and prepare a human-approved rollback workflow; do not execute rollback from this readiness check."
	} else {
		readiness.SuggestedNextActionKind = "monitor_rollback_conditions"
		readiness.SuggestedNextAction = "No rollback trigger is matched by the available evidence; keep monitoring or provide more evidence."
	}
	readiness.Evidence = groupDiscussionRollbackReadinessEvidence(readiness, readiness.MatchedTriggers, readiness.UnmatchedTriggers, hasExternalEvidence)
	return finalize()
}

func groupDiscussionRollbackReadinessToolCall(readiness GroupDiscussionRollbackReadiness) *GroupDiscussionToolCallSuggestion {
	consultationID := strings.TrimSpace(readiness.ConsultationID)
	if consultationID == "" {
		return nil
	}
	focusContext := groupDiscussionRollbackReadinessFocusContext(readiness)
	return &GroupDiscussionToolCallSuggestion{
		Tool:                    "group_discussion",
		Args:                    map[string]interface{}{"action": "rollback_readiness", "consultation_id": consultationID},
		RecommendedFocusContext: focusContext,
		DiscussionFocusContext:  focusContext,
		NonExecuting:            true,
		NonExecutingBoundary:    "recommended group_discussion rollback readiness inspection only; it must not execute rollback, decide, escalate, submit results, send messages, invite experts, change discussion state, mutate memory, or change routing",
	}
}

func groupDiscussionRollbackReadinessFocusContext(readiness GroupDiscussionRollbackReadiness) map[string]interface{} {
	consultationID := strings.TrimSpace(readiness.ConsultationID)
	if consultationID == "" {
		return nil
	}
	ctx := map[string]interface{}{
		"consultation_id": consultationID,
		"action_kind":     "rollback_readiness",
		"reason":          strings.TrimSpace(readiness.SuggestedNextAction),
	}
	if proposalID := strings.TrimSpace(readiness.ProposalID); proposalID != "" {
		ctx["proposal_id"] = proposalID
	}
	if len(readiness.MatchedTriggers) > 0 {
		ctx["matched_triggers"] = append([]string{}, readiness.MatchedTriggers...)
	}
	if len(readiness.UnmatchedTriggers) > 0 {
		ctx["unmatched_triggers"] = append([]string{}, readiness.UnmatchedTriggers...)
	}
	if next := strings.TrimSpace(readiness.SuggestedNextActionKind); next != "" {
		ctx["suggested_next_action_kind"] = next
	}
	return ctx
}

func groupDiscussionRollbackEvidenceText(detail a2a.HubDiscussionDetail, externalEvidence string) (string, bool) {
	parts := []string{
		detail.Discussion.Topic,
		detail.Discussion.Question,
		detail.Discussion.ResultSummary,
	}
	if decision := groupDiscussionDecision(detail); decision != nil {
		parts = append(parts, decision.Summary, decision.Rationale)
	}
	for _, proposal := range detail.Proposals {
		parts = append(parts, proposal.Title, proposal.Content)
		parts = append(parts, proposal.Goals...)
		parts = append(parts, proposal.Constraints...)
		parts = append(parts, proposal.Risks...)
	}
	for _, review := range detail.Reviews {
		parts = append(parts, review.Comment)
	}
	for _, message := range detail.Messages {
		parts = append(parts, message.Content)
		parts = append(parts, message.Evidence...)
	}
	externalEvidence = strings.TrimSpace(externalEvidence)
	if externalEvidence != "" {
		parts = append(parts, externalEvidence)
	}
	return strings.Join(parts, "\n"), externalEvidence != ""
}

func groupDiscussionRollbackTriggerMatched(evidenceText, trigger string) bool {
	evidenceKey := normalizeGroupDiscussionKey(evidenceText)
	triggerKey := normalizeGroupDiscussionKey(trigger)
	if evidenceKey == "" || triggerKey == "" {
		return false
	}
	if strings.Contains(evidenceKey, triggerKey) {
		return true
	}
	tokens := groupDiscussionRollbackSignalTokens(trigger)
	if len(tokens) == 0 {
		return false
	}
	evidenceTokens := map[string]struct{}{}
	for _, token := range groupDiscussionDecisionTokens(evidenceText) {
		evidenceTokens[token] = struct{}{}
	}
	matched := 0
	for _, token := range tokens {
		if _, ok := evidenceTokens[token]; ok {
			matched++
		}
	}
	return matched == len(tokens)
}

func groupDiscussionRollbackSignalTokens(trigger string) []string {
	stop := map[string]struct{}{
		"a": {}, "an": {}, "and": {}, "after": {}, "any": {}, "by": {}, "condition": {}, "conditions": {}, "deployment": {}, "deploy": {}, "evidence": {}, "for": {}, "human": {}, "if": {}, "in": {}, "invalidates": {}, "is": {}, "manual": {}, "new": {}, "of": {}, "on": {}, "or": {}, "owner": {}, "request": {}, "requests": {}, "review": {}, "reviewing": {}, "rollback": {}, "signals": {}, "the": {}, "to": {}, "trigger": {}, "triggers": {}, "when": {}, "with": {},
	}
	out := []string{}
	for _, token := range groupDiscussionDecisionTokens(trigger) {
		if _, skip := stop[token]; skip {
			continue
		}
		if len(token) < 3 {
			continue
		}
		out = append(out, token)
	}
	return dedupeGroupDiscussionStrings(out)
}

func groupDiscussionRollbackReadinessEvidence(readiness GroupDiscussionRollbackReadiness, matched []string, unmatched []string, hasExternalEvidence bool) []string {
	evidence := []string{}
	if strings.TrimSpace(readiness.DecisionSummary) != "" {
		evidence = append(evidence, "decision_summary: "+strings.TrimSpace(readiness.DecisionSummary))
	}
	if strings.TrimSpace(readiness.ProposalID) != "" {
		evidence = append(evidence, "decision_proposal: "+strings.TrimSpace(readiness.ProposalID))
	}
	if len(readiness.RollbackOn) > 0 {
		evidence = append(evidence, fmt.Sprintf("rollback_triggers: %d", len(readiness.RollbackOn)))
	}
	if len(matched) > 0 {
		evidence = append(evidence, "matched_triggers: "+strings.Join(dedupeGroupDiscussionStrings(matched), ", "))
	}
	if len(unmatched) > 0 {
		evidence = append(evidence, "unmatched_triggers: "+strings.Join(dedupeGroupDiscussionStrings(unmatched), ", "))
	}
	if hasExternalEvidence {
		evidence = append(evidence, "external_evidence: provided")
	}
	return dedupeGroupDiscussionStrings(evidence)
}

func groupDiscussionEscalationStateEvidence(state GroupDiscussionWorkflowState) []string {
	evidence := []string{}
	if state.Escalation != nil {
		escalation := state.Escalation
		if strings.TrimSpace(escalation.Target) != "" {
			evidence = append(evidence, "escalation_target: "+strings.TrimSpace(escalation.Target))
		}
		if strings.TrimSpace(escalation.RaisedBy) != "" {
			evidence = append(evidence, "raised_by: "+strings.TrimSpace(escalation.RaisedBy))
		}
		if strings.TrimSpace(escalation.Reason) != "" {
			evidence = append(evidence, "escalation_reason: "+strings.TrimSpace(escalation.Reason))
		}
	}
	if strings.TrimSpace(state.Status) != "" {
		evidence = append(evidence, "status: "+strings.TrimSpace(state.Status))
	}
	if state.BlockingReviewCount > 0 {
		evidence = append(evidence, fmt.Sprintf("blocking_reviews: %d", state.BlockingReviewCount))
	}
	return dedupeGroupDiscussionStrings(evidence)
}

func groupDiscussionResultDraftEvidence(state GroupDiscussionWorkflowState) []string {
	evidence := []string{}
	if state.Decision != nil {
		decision := state.Decision
		if strings.TrimSpace(decision.Summary) != "" {
			evidence = append(evidence, "decision_summary: "+strings.TrimSpace(decision.Summary))
		}
		if strings.TrimSpace(decision.Rationale) != "" {
			evidence = append(evidence, "decision_rationale: "+strings.TrimSpace(decision.Rationale))
		}
		if strings.TrimSpace(decision.ProposalID) != "" {
			evidence = append(evidence, "decision_proposal: "+strings.TrimSpace(decision.ProposalID))
		}
		if len(decision.RollbackOn) > 0 {
			evidence = append(evidence, "rollback_on: "+strings.Join(dedupeGroupDiscussionStrings(decision.RollbackOn), ", "))
		}
	}
	if state.HasResult {
		evidence = append(evidence, "result_available: true")
	}
	if state.DecidableProposalCount > 0 {
		evidence = append(evidence, fmt.Sprintf("decidable_proposals: %d", state.DecidableProposalCount))
	}
	if state.BlockingReviewCount > 0 {
		evidence = append(evidence, fmt.Sprintf("blocking_reviews: %d", state.BlockingReviewCount))
	}
	return dedupeGroupDiscussionStrings(evidence)
}

func groupDiscussionRollbackWorkflowDraftEvidence(readiness GroupDiscussionRollbackReadiness) []string {
	evidence := append([]string{}, readiness.Evidence...)
	if len(readiness.MatchedTriggers) > 0 {
		evidence = append(evidence, "matched_rollback_review: "+strings.Join(dedupeGroupDiscussionStrings(readiness.MatchedTriggers), ", "))
	}
	if len(readiness.UnmatchedTriggers) > 0 {
		evidence = append(evidence, "remaining_rollback_conditions: "+strings.Join(dedupeGroupDiscussionStrings(readiness.UnmatchedTriggers), ", "))
	}
	return dedupeGroupDiscussionStrings(evidence)
}

func groupDiscussionEscalationDraftEvidence(state GroupDiscussionWorkflowState, route *GroupDiscussionEscalationRouteSuggestion) []string {
	evidence := []string{}
	if route != nil {
		if len(route.Triggers) > 0 {
			evidence = append(evidence, "triggers: "+strings.Join(route.Triggers, ", "))
		}
		evidence = append(evidence, route.PolicyEvidence...)
		if strings.TrimSpace(route.Reason) != "" {
			evidence = append(evidence, "route_reason: "+strings.TrimSpace(route.Reason))
		}
	}
	if state.BlockingReviewCount > 0 {
		evidence = append(evidence, fmt.Sprintf("blocking_reviews: %d", state.BlockingReviewCount))
	}
	for _, proposal := range state.Proposals {
		if !proposal.BlockingReviews {
			continue
		}
		evidence = append(evidence, fmt.Sprintf("proposal %s has concerns=%d rejections=%d", firstNonEmptyGroupString(proposal.Title, proposal.ID), proposal.ReviewSummary.Concerns, proposal.ReviewSummary.Rejections))
	}
	return dedupeGroupDiscussionStrings(evidence)
}

func groupDiscussionDecisionDraftEvidence(proposal GroupDiscussionProposalWorkflowState) []string {
	evidence := []string{
		"proposal: " + firstNonEmptyGroupString(proposal.Title, proposal.ID),
		fmt.Sprintf("reviews: approvals=%d concerns=%d rejections=%d abstains=%d", proposal.ReviewSummary.Approvals, proposal.ReviewSummary.Concerns, proposal.ReviewSummary.Rejections, proposal.ReviewSummary.Abstains),
	}
	if len(proposal.ReviewSummary.ReviewedBy) > 0 {
		evidence = append(evidence, "reviewed_by: "+strings.Join(proposal.ReviewSummary.ReviewedBy, ", "))
	}
	if proposal.PolicySatisfied {
		evidence = append(evidence, "decision_policy: satisfied")
	}
	return dedupeGroupDiscussionStrings(evidence)
}

func groupDiscussionDecisionRationaleDraft(proposal GroupDiscussionProposalWorkflowState) string {
	title := firstNonEmptyGroupString(proposal.Title, proposal.ID, "proposal")
	summary := proposal.ReviewSummary
	parts := []string{
		fmt.Sprintf("%s satisfies the discussion decision policy with %d approval(s) and no blocking concern or rejection.", title, summary.Approvals),
	}
	if len(summary.ReviewedBy) > 0 {
		parts = append(parts, "Reviewed by: "+strings.Join(summary.ReviewedBy, ", ")+".")
	}
	return strings.Join(parts, " ")
}

func groupDiscussionDecisionRollbackDraft(proposal GroupDiscussionProposalWorkflowState) []string {
	title := firstNonEmptyGroupString(proposal.Title, proposal.ID, "proposal")
	return []string{
		"new blocking evidence invalidates " + title,
		"human owner requests rollback after reviewing deployment signals",
	}
}

func groupDiscussionReadinessDraftEvidence(state GroupDiscussionWorkflowState) []string {
	evidence := []string{
		fmt.Sprintf("answers: %d/%d", state.Readiness.AnswerCount, state.Readiness.ExpectedAnswerCount),
	}
	if reason := strings.TrimSpace(state.Readiness.Reason); reason != "" {
		evidence = append(evidence, "readiness: "+reason)
	}
	if state.Readiness.Ready {
		evidence = append(evidence, "ready_to_summarize: true")
	}
	return dedupeGroupDiscussionStrings(evidence)
}

func groupDiscussionFollowupDraftEvidence(detail a2a.HubDiscussionDetail, state GroupDiscussionWorkflowState) []string {
	evidence := groupDiscussionReadinessDraftEvidence(state)
	if missing := missingGroupDiscussionAnswerParticipants(detail); len(missing) > 0 {
		evidence = append(evidence, "missing_expected_answers: "+strings.Join(missing, ", "))
	}
	return dedupeGroupDiscussionStrings(evidence)
}

func groupDiscussionWaitForAnswersDraftEvidence(detail a2a.HubDiscussionDetail, state GroupDiscussionWorkflowState) []string {
	evidence := groupDiscussionReadinessDraftEvidence(state)
	if missing := missingGroupDiscussionAnswerParticipants(detail); len(missing) > 0 {
		evidence = append(evidence, "missing_expected_answers: "+strings.Join(missing, ", "))
	}
	if len(groupDiscussionParticipantIDs(detail)) > 0 {
		evidence = append(evidence, "participants: "+strings.Join(groupDiscussionParticipantIDs(detail), ", "))
	}
	return dedupeGroupDiscussionStrings(evidence)
}

func groupDiscussionReviewCollectionEvidence(detail a2a.HubDiscussionDetail, state GroupDiscussionWorkflowState) []string {
	evidence := []string{fmt.Sprintf("open_proposals: %d", state.OpenProposalCount)}
	for _, proposal := range groupDiscussionReviewRequestProposals(state) {
		line := fmt.Sprintf("proposal %s reviews=%d approvals=%d concerns=%d rejections=%d", firstNonEmptyGroupString(proposal.Title, proposal.ID), proposal.ReviewCount, proposal.ReviewSummary.Approvals, proposal.ReviewSummary.Concerns, proposal.ReviewSummary.Rejections)
		if missing := missingGroupDiscussionProposalReviewers(detail, proposal); len(missing) > 0 {
			line += " missing_reviewers=" + strings.Join(missing, ", ")
		}
		evidence = append(evidence, line)
	}
	return dedupeGroupDiscussionStrings(evidence)
}

func groupDiscussionReviewRequestContent(detail a2a.HubDiscussionDetail, state GroupDiscussionWorkflowState) string {
	subject := strings.TrimSpace(firstNonEmptyGroupString(state.Question, state.Topic, detail.Discussion.Question, detail.Discussion.Topic, "this discussion"))
	proposals := groupDiscussionReviewRequestProposals(state)
	if len(proposals) == 0 {
		return "Please review the open proposal(s) for " + subject + ". Respond with approve, reject, concern, or abstain and include concise reasoning."
	}
	proposal := proposals[0]
	title := firstNonEmptyGroupString(proposal.Title, proposal.ID, "the proposal")
	var b strings.Builder
	b.WriteString("Please review proposal ")
	b.WriteString(title)
	b.WriteString(" for ")
	b.WriteString(subject)
	b.WriteString(".")
	if missing := missingGroupDiscussionProposalReviewers(detail, proposal); len(missing) > 0 {
		b.WriteString(" Review still missing from: ")
		b.WriteString(strings.Join(missing, ", "))
		b.WriteString(".")
	}
	b.WriteString(" Respond with approve, reject, concern, or abstain, and include concise reasoning plus any rollback, security, or evidence gaps.")
	if len(proposals) > 1 {
		b.WriteString(fmt.Sprintf(" There are %d open proposals; start with this one before recording any decision.", len(proposals)))
	}
	return b.String()
}

func groupDiscussionReviewRequestProposals(state GroupDiscussionWorkflowState) []GroupDiscussionProposalWorkflowState {
	out := make([]GroupDiscussionProposalWorkflowState, 0, len(state.Proposals))
	for _, proposal := range state.Proposals {
		status := strings.ToLower(strings.TrimSpace(proposal.Status))
		if status == string(a2a.ProposalAccepted) || status == string(a2a.ProposalRejected) || status == string(a2a.ProposalSuperseded) {
			continue
		}
		if proposal.PolicySatisfied || proposal.BlockingReviews {
			continue
		}
		out = append(out, proposal)
	}
	return out
}

func groupDiscussionReviewRequestParticipants(state GroupDiscussionWorkflowState) []string {
	participants := []string{}
	for _, proposal := range groupDiscussionReviewRequestProposals(state) {
		participants = append(participants, proposal.MissingReviewers...)
	}
	return dedupeGroupDiscussionStrings(participants)
}

func groupDiscussionReviewRequestProposalIDs(state GroupDiscussionWorkflowState) []string {
	ids := []string{}
	for _, proposal := range groupDiscussionReviewRequestProposals(state) {
		if proposal.ID != "" {
			ids = append(ids, proposal.ID)
		}
	}
	return dedupeGroupDiscussionStrings(ids)
}

func missingGroupDiscussionProposalReviewers(detail a2a.HubDiscussionDetail, proposal GroupDiscussionProposalWorkflowState) []string {
	participants := groupDiscussionParticipantIDs(detail)
	if len(participants) == 0 {
		return nil
	}
	reviewed := map[string]struct{}{}
	for _, reviewer := range proposal.ReviewSummary.ReviewedBy {
		if reviewer = strings.TrimSpace(reviewer); reviewer != "" {
			reviewed[reviewer] = struct{}{}
		}
	}
	missing := make([]string, 0, len(participants))
	for _, participant := range participants {
		participant = strings.TrimSpace(participant)
		if participant == "" {
			continue
		}
		if participant == strings.TrimSpace(proposal.AuthorID) && len(participants) > 1 {
			continue
		}
		if _, ok := reviewed[participant]; ok {
			continue
		}
		missing = append(missing, participant)
	}
	return dedupeGroupDiscussionStrings(missing)
}

func groupDiscussionInitialAnswerRequestContent(detail a2a.HubDiscussionDetail, state GroupDiscussionWorkflowState) string {
	missing := missingGroupDiscussionAnswerParticipants(detail)
	subject := strings.TrimSpace(firstNonEmptyGroupString(state.Question, state.Topic, detail.Discussion.Question, detail.Discussion.Topic, "this discussion"))
	var b strings.Builder
	b.WriteString("Please add your expert answer for ")
	b.WriteString(subject)
	b.WriteString(".")
	if len(missing) > 0 {
		b.WriteString(" Expected answer still missing from: ")
		b.WriteString(strings.Join(missing, ", "))
		b.WriteString(".")
	}
	b.WriteString(" Focus on concrete recommendation, evidence, risks, objections, and any rollback or follow-up conditions.")
	return b.String()
}

func groupDiscussionFollowupContentDraft(detail a2a.HubDiscussionDetail, state GroupDiscussionWorkflowState) string {
	missing := missingGroupDiscussionAnswerParticipants(detail)
	subject := strings.TrimSpace(firstNonEmptyGroupString(state.Question, state.Topic, detail.Discussion.Question, detail.Discussion.Topic, "this discussion"))
	var b strings.Builder
	if len(missing) > 0 {
		b.WriteString("Please add the missing expert answer for ")
		b.WriteString(subject)
		b.WriteString(". Expected answer still missing from: ")
		b.WriteString(strings.Join(missing, ", "))
		b.WriteString(".")
	} else {
		b.WriteString("Please add a focused follow-up answer for ")
		b.WriteString(subject)
		b.WriteString(".")
	}
	if strings.TrimSpace(state.Readiness.Reason) != "" {
		b.WriteString(" Current readiness: ")
		b.WriteString(strings.TrimSpace(state.Readiness.Reason))
		b.WriteString(".")
	}
	b.WriteString(" Focus on concrete risks, objections, evidence, or next steps that are not already covered.")
	return b.String()
}

func missingGroupDiscussionAnswerParticipants(detail a2a.HubDiscussionDetail) []string {
	participants := groupDiscussionParticipantIDs(detail)
	if len(participants) > 1 {
		participants = participants[1:]
	}
	answered := map[string]struct{}{}
	for _, msg := range detail.Messages {
		if msg.Kind != a2a.MessageAnswer {
			continue
		}
		if fromID := strings.TrimSpace(msg.FromID); fromID != "" {
			answered[fromID] = struct{}{}
		}
	}
	missing := make([]string, 0, len(participants))
	for _, participant := range participants {
		if participant == "" {
			continue
		}
		if _, ok := answered[participant]; ok {
			continue
		}
		missing = append(missing, participant)
	}
	return dedupeGroupDiscussionStrings(missing)
}

func groupDiscussionParticipantIDs(detail a2a.HubDiscussionDetail) []string {
	ids := append([]string(nil), detail.Discussion.ParticipantIDs...)
	if len(ids) == 0 && detail.Session != nil {
		for _, participant := range detail.Session.Participants {
			ids = append(ids, participant.ID)
		}
	}
	return dedupeGroupDiscussionStrings(ids)
}

func firstDecidableGroupDiscussionProposal(state GroupDiscussionWorkflowState) *GroupDiscussionProposalWorkflowState {
	for i := range state.Proposals {
		proposal := &state.Proposals[i]
		if proposal.PolicySatisfied && !proposal.BlockingReviews && strings.TrimSpace(proposal.ID) != "" {
			return proposal
		}
	}
	return nil
}

func groupDiscussionProposalWorkflowStates(detail a2a.HubDiscussionDetail) []GroupDiscussionProposalWorkflowState {
	proposals := append([]a2a.Proposal(nil), detail.Proposals...)
	if len(proposals) == 0 && detail.Session != nil {
		proposals = append(proposals, detail.Session.Proposals...)
	}
	out := make([]GroupDiscussionProposalWorkflowState, 0, len(proposals))
	for _, proposal := range proposals {
		proposalID := strings.TrimSpace(proposal.ID)
		if proposalID == "" {
			continue
		}
		summary := groupDiscussionReviewSummaryFor(detail, proposalID)
		state := GroupDiscussionProposalWorkflowState{
			ID:              proposalID,
			Title:           strings.TrimSpace(proposal.Title),
			AuthorID:        strings.TrimSpace(proposal.AuthorID),
			Status:          strings.TrimSpace(string(proposal.Status)),
			ReviewSummary:   summary,
			ReviewCount:     summary.Approvals + summary.Rejections + summary.Concerns + summary.Abstains,
			PolicySatisfied: groupDiscussionProposalPolicySatisfied(detail, proposalID, summary),
			BlockingReviews: summary.Rejections > 0 || summary.Concerns > 0,
		}
		state.MissingReviewers = missingGroupDiscussionProposalReviewers(detail, state)
		state.Blockers = groupDiscussionProposalBlockers(state)
		out = append(out, state)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].PolicySatisfied != out[j].PolicySatisfied {
			return out[i].PolicySatisfied
		}
		if out[i].BlockingReviews != out[j].BlockingReviews {
			return !out[i].BlockingReviews
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func groupDiscussionWorkflowBlockers(state GroupDiscussionWorkflowState) []GroupDiscussionWorkflowBlocker {
	var blockers []GroupDiscussionWorkflowBlocker
	if state.HasEscalation {
		blockers = append(blockers, GroupDiscussionWorkflowBlocker{
			Code:     "existing_escalation",
			Severity: "terminal",
			Message:  "Discussion already has an escalation; wait for the escalation owner before changing collaboration state.",
			Count:    1,
		})
		return blockers
	}
	if state.HasDecision || state.HasResult {
		blockers = append(blockers, GroupDiscussionWorkflowBlocker{
			Code:     "result_available",
			Severity: "terminal",
			Message:  "Discussion already has a result or decision; inspect rationale and rollback constraints before reuse.",
			Count:    1,
		})
		return blockers
	}
	if state.BlockingReviewCount > 0 {
		blockers = append(blockers, GroupDiscussionWorkflowBlocker{
			Code:        "blocking_reviews",
			Severity:    "blocking",
			Message:     "One or more proposal reviews contain concerns or rejections that should be resolved before deciding.",
			ProposalIDs: groupDiscussionWorkflowProposalIDs(state, func(proposal GroupDiscussionProposalWorkflowState) bool { return proposal.BlockingReviews }),
			Count:       state.BlockingReviewCount,
		})
	}
	if state.ProposalCount > 0 && state.DecidableProposalCount == 0 {
		proposalIDs := groupDiscussionReviewRequestProposalIDs(state)
		participants := groupDiscussionReviewRequestParticipants(state)
		if len(proposalIDs) > 0 {
			blockers = append(blockers, GroupDiscussionWorkflowBlocker{
				Code:         "pending_proposal_reviews",
				Severity:     "waiting",
				Message:      "Open proposals still need non-blocking review coverage before a policy-satisfied decision is available.",
				ProposalIDs:  proposalIDs,
				Participants: participants,
				Count:        len(proposalIDs),
			})
		}
	}
	if state.ProposalCount == 0 && len(state.MissingAnswerParticipants) > 0 {
		blockers = append(blockers, GroupDiscussionWorkflowBlocker{
			Code:         "missing_answers",
			Severity:     "waiting",
			Message:      "Expected expert answers are still missing before summary or decision work should proceed.",
			Participants: append([]string{}, state.MissingAnswerParticipants...),
			Count:        len(state.MissingAnswerParticipants),
		})
	}
	if state.ProposalCount == 0 && !state.Readiness.Ready && state.Readiness.AnswerCount == 0 && len(blockers) == 0 {
		blockers = append(blockers, GroupDiscussionWorkflowBlocker{
			Code:     "waiting_for_answers",
			Severity: "waiting",
			Message:  "No expert answer has landed yet; wait for answers or send a scoped reminder.",
		})
	}
	return dedupeGroupDiscussionWorkflowBlockers(blockers)
}

func groupDiscussionProposalBlockers(proposal GroupDiscussionProposalWorkflowState) []GroupDiscussionWorkflowBlocker {
	var blockers []GroupDiscussionWorkflowBlocker
	if proposal.BlockingReviews {
		blockers = append(blockers, GroupDiscussionWorkflowBlocker{
			Code:       "proposal_blocking_reviews",
			Severity:   "blocking",
			Message:    "Proposal has concerns or rejections that should be resolved before it can be safely decided.",
			ProposalID: proposal.ID,
			Count:      proposal.ReviewSummary.Concerns + proposal.ReviewSummary.Rejections,
		})
	}
	if !proposal.PolicySatisfied && len(proposal.MissingReviewers) > 0 {
		blockers = append(blockers, GroupDiscussionWorkflowBlocker{
			Code:         "missing_reviewers",
			Severity:     "waiting",
			Message:      "Proposal still needs review coverage from expected participants.",
			ProposalID:   proposal.ID,
			Participants: append([]string{}, proposal.MissingReviewers...),
			Count:        len(proposal.MissingReviewers),
		})
	}
	return blockers
}

func groupDiscussionWorkflowProposalIDs(state GroupDiscussionWorkflowState, include func(GroupDiscussionProposalWorkflowState) bool) []string {
	var ids []string
	for _, proposal := range state.Proposals {
		if include != nil && !include(proposal) {
			continue
		}
		if id := strings.TrimSpace(proposal.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return dedupeGroupDiscussionStrings(ids)
}

func dedupeGroupDiscussionWorkflowBlockers(blockers []GroupDiscussionWorkflowBlocker) []GroupDiscussionWorkflowBlocker {
	seen := map[string]struct{}{}
	out := make([]GroupDiscussionWorkflowBlocker, 0, len(blockers))
	for _, blocker := range blockers {
		key := strings.Join([]string{blocker.Code, blocker.ProposalID, strings.Join(blocker.ProposalIDs, ","), strings.Join(blocker.Participants, ",")}, "|")
		if strings.TrimSpace(blocker.Code) == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, blocker)
	}
	return out
}

func groupDiscussionReviewSummaryFor(detail a2a.HubDiscussionDetail, proposalID string) a2a.ReviewSummary {
	proposalID = strings.TrimSpace(proposalID)
	if proposalID == "" {
		return a2a.ReviewSummary{}
	}
	if detail.ReviewSummaries != nil {
		if summary, ok := detail.ReviewSummaries[proposalID]; ok {
			return summary
		}
	}
	if detail.Session != nil {
		return detail.Session.ReviewSummary(proposalID)
	}
	latest := map[string]a2a.ReviewPosition{}
	for _, review := range detail.Reviews {
		if strings.TrimSpace(review.ProposalID) == proposalID && strings.TrimSpace(review.ReviewerID) != "" {
			latest[strings.TrimSpace(review.ReviewerID)] = review.Position
		}
	}
	summary := a2a.ReviewSummary{ReviewedBy: make([]string, 0, len(latest))}
	for reviewer, position := range latest {
		summary.ReviewedBy = append(summary.ReviewedBy, reviewer)
		switch position {
		case a2a.ReviewApprove:
			summary.Approvals++
		case a2a.ReviewReject:
			summary.Rejections++
		case a2a.ReviewConcern:
			summary.Concerns++
		case a2a.ReviewAbstain:
			summary.Abstains++
		}
	}
	sort.Strings(summary.ReviewedBy)
	return summary
}

func groupDiscussionProposalPolicySatisfied(detail a2a.HubDiscussionDetail, proposalID string, summary a2a.ReviewSummary) bool {
	if detail.Session != nil {
		return detail.Session.PolicySatisfied(proposalID)
	}
	if summary.Rejections > 0 || summary.Concerns > 0 {
		return false
	}
	participants := len(detail.Discussion.ParticipantIDs)
	if participants <= 0 {
		return false
	}
	return summary.Approvals > participants/2
}

func groupDiscussionWorkflowNextAction(state GroupDiscussionWorkflowState) (string, string) {
	switch {
	case state.HasEscalation:
		return "wait_for_escalation_owner", "Wait for the escalation target to resolve ownership or provide a decision path."
	case state.HasDecision || state.HasResult:
		return "result_available", "A result or decision is already available; inspect rationale and rollback constraints before reuse."
	case state.DecidableProposalCount > 0:
		return "decide_policy_satisfied_proposal", "A proposal has enough non-blocking approvals; record a decision with rationale and rollback triggers if appropriate."
	case state.BlockingReviewCount > 0:
		return "resolve_proposal_concerns", "Resolve proposal concerns or rejections before deciding."
	case state.ProposalCount > 0:
		return "collect_proposal_reviews", "Ask participants to review the open proposals."
	case state.Readiness.Ready:
		return "summarize_result_preview", "Preview a layered summary before submitting or injecting the result."
	case state.Readiness.AnswerCount > 0:
		return "send_followup_or_wait_for_more_answers", "Some expert answers are present; send a targeted follow-up or wait for remaining expected answers."
	default:
		return "wait_for_expert_answers", "Wait for expert answers before summarizing or deciding."
	}
}

func groupDiscussionAuthorizeStartGate(app *App, handler *IMMessageHandler, args map[string]interface{}) error {
	cfg, err := app.LoadConfig()
	if err != nil {
		return err
	}
	if !cfg.GroupDiscussion.ConfirmBeforeStart {
		return nil
	}
	risk := strings.ToLower(strings.TrimSpace(stringVal(args, "risk_level")))
	if risk == "" {
		risk = "medium"
	}
	maxRisk := strings.ToLower(strings.TrimSpace(cfg.GroupDiscussion.MaxRiskLevel))
	if maxRisk == "" {
		maxRisk = "medium"
	}
	if groupDiscussionRiskRank(risk) > groupDiscussionRiskRank(maxRisk) {
		return fmt.Errorf("group discussion risk %q exceeds local max risk %q", risk, maxRisk)
	}
	if cfg.GroupDiscussion.AllowSecurityGroupFreeDiscussion && strings.TrimSpace(cfg.GroupDiscussion.SecurityGroupID) != "" && groupDiscussionRiskRank(risk) <= groupDiscussionRiskRank("medium") {
		return nil
	}
	decision, err := groupDiscussionClassifyHumanAuthorization(app, handler, args)
	if err != nil {
		return fmt.Errorf("group discussion authorization could not be verified: %w", err)
	}
	if decision.Decision == "approve" && decision.Confidence >= 0.7 {
		return nil
	}
	if decision.Reason != "" {
		return fmt.Errorf("group discussion start is not authorized: %s", decision.Reason)
	}
	return fmt.Errorf("group discussion start requires explicit human approval")
}

func groupDiscussionRiskRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low":
		return 1
	case "medium", "":
		return 2
	case "high":
		return 3
	case "critical":
		return 4
	default:
		return 2
	}
}

type groupDiscussionAuthorizationDecision struct {
	Decision   string  `json:"decision"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

func groupDiscussionClassifyHumanAuthorization(app *App, handler *IMMessageHandler, args map[string]interface{}) (groupDiscussionAuthorizationDecision, error) {
	if app == nil || handler == nil {
		return groupDiscussionAuthorizationDecision{}, fmt.Errorf("assistant context is unavailable")
	}
	userText := strings.TrimSpace(handler.lastUserText)
	if userText == "" {
		return groupDiscussionAuthorizationDecision{}, fmt.Errorf("latest user message is empty")
	}
	cfg := app.GetMaclawLLMConfig()
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return groupDiscussionAuthorizationDecision{}, fmt.Errorf("MaClaw LLM is not configured")
	}
	payload := map[string]interface{}{
		"latest_user_message": userText,
		"discussion_request": map[string]interface{}{
			"topic":           strings.TrimSpace(stringVal(args, "topic")),
			"question":        strings.TrimSpace(stringVal(args, "question")),
			"context_summary": strings.TrimSpace(stringVal(args, "context_summary")),
			"risk_level":      strings.TrimSpace(stringVal(args, "risk_level")),
		},
	}
	payloadJSON, _ := json.Marshal(payload)
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": groupDiscussionAuthorizationPrompt},
		map[string]interface{}{"role": "user", "content": string(payloadJSON)},
	}
	client := handler.client
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second}
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic") {
		resp, err := handler.doAnthropicLLMRequest(cfg, messages, nil, client)
		if err != nil {
			return groupDiscussionAuthorizationDecision{}, err
		}
		return decodeGroupDiscussionAuthorizationDecision(firstLLMResponseText(resp))
	}
	return requestGroupDiscussionAuthorizationOpenAI(handler, cfg, messages, client)
}

func requestGroupDiscussionAuthorizationOpenAI(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client) (groupDiscussionAuthorizationDecision, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	responseFormat := map[string]interface{}{
		"type": "json_schema",
		"json_schema": map[string]interface{}{
			"name":   "group_discussion_authorization",
			"schema": groupDiscussionAuthorizationJSONSchema,
		},
	}
	req, body, endpoint, err := llm.NewOpenAIChatRequest(ctx, cfg, messages, llm.OpenAIChatRequestOptions{
		Stream:         false,
		ResponseFormat: responseFormat,
	})
	if err != nil {
		return groupDiscussionAuthorizationDecision{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return groupDiscussionAuthorizationDecision{}, fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return groupDiscussionAuthorizationDecision{}, dumpLLMContext(resp.StatusCode, "group discussion authorization request failed", body, handler.getTempDir())
	}
	parsedResp, err := llm.ParseNonStreamOpenAIResponse(resp)
	if err != nil {
		return groupDiscussionAuthorizationDecision{}, err
	}
	return decodeGroupDiscussionAuthorizationDecision(firstLLMResponseText(parsedResp))
}

func decodeGroupDiscussionAuthorizationDecision(content string) (groupDiscussionAuthorizationDecision, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	if content == "" {
		return groupDiscussionAuthorizationDecision{}, fmt.Errorf("empty authorization response")
	}
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		content = content[start : end+1]
	}
	var parsed groupDiscussionAuthorizationDecision
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return groupDiscussionAuthorizationDecision{}, err
	}
	parsed.Decision = strings.ToLower(strings.TrimSpace(parsed.Decision))
	if parsed.Decision != "approve" && parsed.Decision != "reject" && parsed.Decision != "unclear" {
		return groupDiscussionAuthorizationDecision{}, fmt.Errorf("unknown authorization decision %q", parsed.Decision)
	}
	if parsed.Confidence < 0 {
		parsed.Confidence = 0
	}
	if parsed.Confidence > 1 {
		parsed.Confidence = 1
	}
	parsed.Reason = strings.TrimSpace(parsed.Reason)
	return parsed, nil
}

var groupDiscussionAuthorizationJSONSchema = map[string]interface{}{
	"type":                 "object",
	"additionalProperties": false,
	"properties": map[string]interface{}{
		"decision": map[string]interface{}{
			"type": "string",
			"enum": []string{"approve", "reject", "unclear"},
		},
		"confidence": map[string]interface{}{
			"type":    "number",
			"minimum": 0,
			"maximum": 1,
		},
		"reason": map[string]interface{}{"type": "string"},
	},
	"required": []string{"decision", "confidence", "reason"},
}

const groupDiscussionAuthorizationPrompt = `You are a strict authorization classifier for MaClaw current-Hub group discussion.
Return only JSON matching the schema.
Task: decide whether the latest human message explicitly authorizes starting the proposed MaClaw group discussion now.
Labels:
- approve: the user clearly grants permission to start/invite/allow this group discussion now.
- reject: the user clearly refuses, cancels, forbids, or asks not to start it.
- unclear: anything else, including continuing development, discussing design, ambiguous agreement, or talking about inviting other MaClaws without clearly authorizing this start.
Rules:
- Do not rely on keywords alone; judge intent in context.
- The user may use Chinese, English, or mixed language.
- If there is doubt, choose unclear.
- Same-Hub/security policy is handled outside this classifier; classify only human authorization intent.`

func groupDiscussionRequestFromArgs(app *App, args map[string]interface{}) a2a.GroupConsultationRequest {
	cfg, _ := app.LoadConfig()
	return a2a.GroupConsultationRequest{
		FromID:         cfg.RemoteMachineID,
		Topic:          strings.TrimSpace(stringVal(args, "topic")),
		Question:       strings.TrimSpace(stringVal(args, "question")),
		ContextSummary: strings.TrimSpace(stringVal(args, "context_summary")),
		SkillsWanted:   groupDiscussionStringSlice(args["skills_wanted"]),
		RiskLevel:      strings.TrimSpace(stringVal(args, "risk_level")),
		MaxRounds:      groupDiscussionInt(args["max_rounds"]),
		TimeoutSeconds: groupDiscussionInt(args["timeout_seconds"]),
		CreatedAt:      time.Now(),
	}
}

func groupDiscussionSuggest(app *App, args map[string]interface{}) string {
	status := app.GroupDiscussionStatus()
	req := groupDiscussionRequestFromArgs(app, args)
	focusContext := groupDiscussionSuggestFocusContext(status, req)
	recommendedToolCall := groupDiscussionSuggestToolCall(status, req, focusContext)
	boundary := "discussion suggestion only; no discussion was started, no experts were invited, no messages were sent, and no Hub state changed"
	payload := map[string]interface{}{
		"enabled":                   status.Enabled,
		"discoverable":              status.Discoverable,
		"current_hub_only":          true,
		"topic":                     strings.TrimSpace(req.Topic),
		"question":                  strings.TrimSpace(req.Question),
		"context_summary":           strings.TrimSpace(req.ContextSummary),
		"skills_wanted":             dedupeGroupDiscussionStrings(req.SkillsWanted),
		"instruction":               "Ask the human for explicit permission before calling group_discussion(action=start_authorized). If permission is granted, use only the provided summary/minimal context and keep the discussion on the current Hub.",
		"recommended_focus_context": focusContext,
		"recommended_tool_call":     recommendedToolCall,
		"non_executing_boundary":    boundary,
	}
	if status.Error != "" {
		payload["error"] = status.Error
	}
	return groupDiscussionJSON(payload)
}

func groupDiscussionSuggestFocusContext(status GroupDiscussionStatus, req a2a.GroupConsultationRequest) map[string]interface{} {
	ctx := map[string]interface{}{
		"action_kind":      "suggest_group_discussion",
		"current_hub_only": true,
		"enabled":          status.Enabled,
		"discoverable":     status.Discoverable,
		"reason":           "pre-authorization A2A discussion suggestion; rank experts before requesting start_authorized",
	}
	if topic := strings.TrimSpace(req.Topic); topic != "" {
		ctx["topic"] = topic
	}
	if question := strings.TrimSpace(req.Question); question != "" {
		ctx["question"] = question
	}
	if risk := strings.TrimSpace(req.RiskLevel); risk != "" {
		ctx["risk_level"] = risk
	}
	if len(req.SkillsWanted) > 0 {
		ctx["skills_wanted"] = dedupeGroupDiscussionStrings(req.SkillsWanted)
	}
	if status.Error != "" {
		ctx["status_error"] = status.Error
	}
	return ctx
}

func groupDiscussionSuggestToolCall(status GroupDiscussionStatus, req a2a.GroupConsultationRequest, focusContext map[string]interface{}) *GroupDiscussionToolCallSuggestion {
	args := map[string]interface{}{"action": "rank_experts"}
	if topic := strings.TrimSpace(req.Topic); topic != "" {
		args["topic"] = topic
	}
	if question := strings.TrimSpace(req.Question); question != "" {
		args["question"] = question
	}
	if contextSummary := strings.TrimSpace(req.ContextSummary); contextSummary != "" {
		args["context_summary"] = contextSummary
	}
	if risk := strings.TrimSpace(req.RiskLevel); risk != "" {
		args["risk_level"] = risk
	}
	if len(req.SkillsWanted) > 0 {
		args["skills_wanted"] = dedupeGroupDiscussionStrings(req.SkillsWanted)
	}
	if !status.Enabled {
		args["action"] = "status"
	}
	return &GroupDiscussionToolCallSuggestion{
		Tool:                    "group_discussion",
		Args:                    args,
		RecommendedFocusContext: focusContext,
		DiscussionFocusContext:  focusContext,
		NonExecuting:            true,
		NonExecutingBoundary:    "recommended pre-authorization group discussion inspection only; it must not start a discussion, invite experts, send messages, mutate Hub state, mutate memory, or change routing",
	}
}

const groupDiscussionStatusNonExecutingBoundary = "read-only group discussion status inspection; no discussion was started, no experts were invited, no messages were sent, no results were submitted, no Hub state changed, no memory was promoted, and no routing changed"

func groupDiscussionStatusFocusContext(status GroupDiscussionStatus) map[string]interface{} {
	ctx := map[string]interface{}{
		"action_kind":        "inspect_group_discussion_status",
		"reason":             "read-only A2A discussion status inspection before choosing a safer follow-up action",
		"enabled":            status.Enabled,
		"discoverable":       status.Discoverable,
		"expert_count":       len(status.Experts),
		"discussion_count":   len(status.Discussions),
		"active_count":       status.ActiveDiscussionCount,
		"ready_count":        status.ReadyDiscussionCount,
		"waiting_count":      status.WaitingDiscussionCount,
		"stale_count":        status.StaleDiscussionCount,
		"pending_invite_cnt": len(status.PendingInvites),
	}
	if status.Error != "" {
		ctx["status_error"] = status.Error
	}
	if status.Profile != nil {
		if agentID := strings.TrimSpace(status.Profile.AgentID); agentID != "" {
			ctx["local_agent_id"] = agentID
		}
		if display := strings.TrimSpace(status.Profile.DisplayName); display != "" {
			ctx["local_display_name"] = display
		}
	}
	if len(status.Discussions) > 0 {
		first := status.Discussions[0]
		if id := strings.TrimSpace(first.ID); id != "" {
			ctx["recommended_consultation_id"] = id
		}
		if topic := strings.TrimSpace(first.Topic); topic != "" {
			ctx["recommended_topic"] = topic
		}
		if first.ReadyToSummarize {
			ctx["recommended_action_kind"] = "inspect_ready_discussion"
		} else {
			ctx["recommended_action_kind"] = "inspect_discussions"
		}
	} else if len(status.PendingInvites) > 0 {
		first := status.PendingInvites[0]
		ctx["recommended_action_kind"] = "review_pending_invites"
		if id := strings.TrimSpace(first.ID); id != "" {
			ctx["recommended_invite_id"] = id
		}
		if sessionID := strings.TrimSpace(first.SessionID); sessionID != "" {
			ctx["recommended_consultation_id"] = sessionID
		}
		if fromID := strings.TrimSpace(first.FromID); fromID != "" {
			ctx["recommended_from_id"] = fromID
		}
		if role := strings.TrimSpace(string(first.Role)); role != "" {
			ctx["recommended_role"] = role
		}
		if topic := strings.TrimSpace(first.Topic); topic != "" {
			ctx["recommended_topic"] = topic
		}
	} else if status.Enabled && len(status.Experts) > 0 {
		ctx["recommended_action_kind"] = "inspect_experts"
	} else {
		ctx["recommended_action_kind"] = "repeat_status"
	}
	return ctx
}

func groupDiscussionStatusToolCall(focusContext map[string]interface{}) *GroupDiscussionToolCallSuggestion {
	args := map[string]interface{}{"action": "status"}
	if consultationID, _ := focusContext["recommended_consultation_id"].(string); strings.TrimSpace(consultationID) != "" {
		if kind, _ := focusContext["recommended_action_kind"].(string); kind != "review_pending_invites" {
			args = map[string]interface{}{"action": "get_detail", "consultation_id": strings.TrimSpace(consultationID)}
		}
	} else if kind, _ := focusContext["recommended_action_kind"].(string); kind == "inspect_experts" {
		args = map[string]interface{}{"action": "list_experts"}
	}
	return &GroupDiscussionToolCallSuggestion{
		Tool:                    "group_discussion",
		Args:                    args,
		RecommendedFocusContext: focusContext,
		DiscussionFocusContext:  focusContext,
		NonExecuting:            true,
		NonExecutingBoundary:    "read-only group discussion status inspection; recommended status follow-up only; it may inspect experts, inspect discussion detail, or repeat status, and must not start a discussion, invite experts, send messages, submit results, mutate Hub state, mutate memory, or change routing",
	}
}

const groupDiscussionExpertsNonExecutingBoundary = "read-only expert discovery inspection; no discussion was started, no experts were invited, no messages were sent, no Hub state changed, no memory was promoted, and no routing changed"

func groupDiscussionExpertsFocusContext(experts []a2a.GroupProfile) map[string]interface{} {
	ctx := map[string]interface{}{
		"action_kind":  "inspect_group_discussion_experts",
		"reason":       "read-only A2A expert discovery before ranking invitees or requesting start authorization",
		"expert_count": len(experts),
	}
	if len(experts) > 0 {
		first := experts[0]
		if agentID := strings.TrimSpace(first.AgentID); agentID != "" {
			ctx["recommended_agent_id"] = agentID
		}
		if display := strings.TrimSpace(first.DisplayName); display != "" {
			ctx["recommended_display_name"] = display
		}
		if len(first.Skills) > 0 {
			ctx["recommended_skills"] = dedupeGroupDiscussionStrings(first.Skills)
		}
	}
	return ctx
}

func groupDiscussionExpertsToolCall(focusContext map[string]interface{}) *GroupDiscussionToolCallSuggestion {
	args := map[string]interface{}{"action": "list_experts"}
	if count, _ := focusContext["expert_count"].(int); count > 0 {
		args = map[string]interface{}{"action": "rank_experts"}
	}
	return &GroupDiscussionToolCallSuggestion{
		Tool:                    "group_discussion",
		Args:                    args,
		RecommendedFocusContext: focusContext,
		DiscussionFocusContext:  focusContext,
		NonExecuting:            true,
		NonExecutingBoundary:    "recommended expert-discovery follow-up only; it may rank experts or repeat discovery, and must not start a discussion, invite experts, send messages, mutate Hub state, mutate memory, or change routing",
	}
}

const groupDiscussionListMineNonExecutingBoundary = "read-only discussion list inspection; no messages were sent, no invitations were sent, no results were submitted, no Hub state changed, no memory was promoted, and no routing changed"

func groupDiscussionListMineFocusContext(discussions []a2a.HubDiscussionSummary, roleFilter string) map[string]interface{} {
	ctx := map[string]interface{}{
		"action_kind":      "list_discussions",
		"reason":           "read-only A2A discussion list inspection before selecting a discussion",
		"discussion_count": len(discussions),
	}
	if role := strings.TrimSpace(roleFilter); role != "" {
		ctx["role_filter"] = role
	}
	if len(discussions) > 0 {
		first := discussions[0]
		if id := strings.TrimSpace(first.ID); id != "" {
			ctx["recommended_consultation_id"] = id
		}
		if topic := strings.TrimSpace(first.Topic); topic != "" {
			ctx["recommended_topic"] = topic
		}
		if status := strings.TrimSpace(first.Status); status != "" {
			ctx["recommended_status"] = status
		}
	}
	return ctx
}

func groupDiscussionListMineToolCall(focusContext map[string]interface{}) *GroupDiscussionToolCallSuggestion {
	args := map[string]interface{}{"action": "list_mine"}
	if role, _ := focusContext["role_filter"].(string); strings.TrimSpace(role) != "" {
		args["role_filter"] = strings.TrimSpace(role)
	}
	if consultationID, _ := focusContext["recommended_consultation_id"].(string); strings.TrimSpace(consultationID) != "" {
		args = map[string]interface{}{"action": "get_detail", "consultation_id": strings.TrimSpace(consultationID)}
	}
	return &GroupDiscussionToolCallSuggestion{
		Tool:                    "group_discussion",
		Args:                    args,
		RecommendedFocusContext: focusContext,
		DiscussionFocusContext:  focusContext,
		NonExecuting:            true,
		NonExecutingBoundary:    "recommended discussion-list follow-up only; it may inspect a discussion detail or repeat the list, and must not send messages, invite experts, submit results, mutate Hub state, mutate memory, or change routing",
	}
}

const groupDiscussionSummaryNonExecutingBoundary = "read-only discussion summary inspection; no messages were sent, no invitations were sent, no results were submitted, no Hub state changed, no memory was promoted, and no routing changed"

func groupDiscussionSummaryFocusContext(discussion a2a.HubDiscussionSummary, fallbackID string) map[string]interface{} {
	consultationID := strings.TrimSpace(firstNonEmptyGroupString(discussion.ID, fallbackID))
	if consultationID == "" {
		return nil
	}
	ctx := map[string]interface{}{
		"consultation_id": consultationID,
		"action_kind":     "inspect_discussion_summary",
		"reason":          "read-only A2A discussion summary inspection before fetching full detail",
		"message_count":   discussion.MessageCount,
		"answer_count":    discussion.AnswerCount,
		"has_result":      strings.TrimSpace(discussion.ResultSummary) != "",
	}
	if status := strings.TrimSpace(discussion.Status); status != "" {
		ctx["status"] = status
	}
	if topic := strings.TrimSpace(discussion.Topic); topic != "" {
		ctx["topic"] = topic
	}
	if question := strings.TrimSpace(discussion.Question); question != "" {
		ctx["question"] = question
	}
	return ctx
}

func groupDiscussionSummaryToolCall(focusContext map[string]interface{}) *GroupDiscussionToolCallSuggestion {
	consultationID, _ := focusContext["consultation_id"].(string)
	consultationID = strings.TrimSpace(consultationID)
	if consultationID == "" {
		return nil
	}
	return &GroupDiscussionToolCallSuggestion{
		Tool:                    "group_discussion",
		Args:                    map[string]interface{}{"action": "get_detail", "consultation_id": consultationID},
		RecommendedFocusContext: focusContext,
		DiscussionFocusContext:  focusContext,
		NonExecuting:            true,
		NonExecutingBoundary:    "recommended discussion-summary follow-up only; it may fetch detail and must not send messages, invite experts, submit results, mutate Hub state, mutate memory, or change routing",
	}
}

const groupDiscussionStaleCleanupNonExecutingBoundary = "dry-run stale discussion cleanup preview; no discussions were cancelled, no messages were sent, no invitations were sent, no Hub state changed, no memory was promoted, and no routing changed"

func groupDiscussionStaleCleanupFocusContext(result GroupDiscussionStaleCleanupResult) map[string]interface{} {
	ctx := map[string]interface{}{
		"action_kind":       "cleanup_stale_preview",
		"reason":            "dry-run A2A stale discussion cleanup inspection before any cancellation",
		"dry_run":           result.DryRun,
		"timeout_seconds":   result.TimeoutSeconds,
		"stale_count":       len(result.Stale),
		"cancelled_count":   len(result.CancelledIDs),
		"cleanup_has_error": len(result.Errors) > 0,
	}
	if len(result.Stale) > 0 {
		ids := make([]string, 0, len(result.Stale))
		for _, discussion := range result.Stale {
			if id := strings.TrimSpace(discussion.ID); id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			ctx["stale_consultation_ids"] = ids
			ctx["recommended_consultation_id"] = ids[0]
		}
	}
	return ctx
}

func groupDiscussionStaleCleanupToolCall(focusContext map[string]interface{}) *GroupDiscussionToolCallSuggestion {
	args := map[string]interface{}{"action": "cleanup_stale", "dry_run": true}
	if consultationID, _ := focusContext["recommended_consultation_id"].(string); strings.TrimSpace(consultationID) != "" {
		args = map[string]interface{}{"action": "get_detail", "consultation_id": strings.TrimSpace(consultationID)}
	}
	return &GroupDiscussionToolCallSuggestion{
		Tool:                    "group_discussion",
		Args:                    args,
		RecommendedFocusContext: focusContext,
		DiscussionFocusContext:  focusContext,
		NonExecuting:            true,
		NonExecutingBoundary:    "recommended stale-cleanup preview follow-up only; it may inspect stale discussion detail or repeat dry-run cleanup, and must not cancel discussions, send messages, invite experts, submit results, mutate Hub state, mutate memory, or change routing",
	}
}

const groupDiscussionDetailNonExecutingBoundary = "read-only discussion detail inspection; no result was submitted, no chat was injected, no message was sent, no invitation was sent, no Hub state changed, no memory was promoted, and no routing changed"

func groupDiscussionDetailFocusContext(detail a2a.HubDiscussionDetail, fallbackID string) map[string]interface{} {
	consultationID := strings.TrimSpace(firstNonEmptyGroupString(detail.Discussion.ID, fallbackID))
	if consultationID == "" {
		return nil
	}
	ctx := map[string]interface{}{
		"consultation_id": consultationID,
		"action_kind":     "inspect_discussion_detail",
		"reason":          "read-only A2A discussion detail inspection before any submit/inject/message/invite/state mutation",
		"message_count":   len(detail.Messages),
		"proposal_count":  len(detail.Proposals),
		"review_count":    len(detail.Reviews),
		"has_result":      strings.TrimSpace(detail.Discussion.ResultSummary) != "" || detail.Decision != nil,
	}
	if status := strings.TrimSpace(detail.Discussion.Status); status != "" {
		ctx["status"] = status
	}
	if topic := strings.TrimSpace(detail.Discussion.Topic); topic != "" {
		ctx["topic"] = topic
	}
	if question := strings.TrimSpace(detail.Discussion.Question); question != "" {
		ctx["question"] = question
	}
	return ctx
}

func groupDiscussionDetailToolCall(focusContext map[string]interface{}) *GroupDiscussionToolCallSuggestion {
	consultationID, _ := focusContext["consultation_id"].(string)
	consultationID = strings.TrimSpace(consultationID)
	if consultationID == "" {
		return nil
	}
	return &GroupDiscussionToolCallSuggestion{
		Tool:                    "group_discussion",
		Args:                    map[string]interface{}{"action": "workflow_state", "consultation_id": consultationID},
		RecommendedFocusContext: focusContext,
		DiscussionFocusContext:  focusContext,
		NonExecuting:            true,
		NonExecutingBoundary:    "recommended discussion-detail follow-up only; it may inspect workflow state and must not submit results, inject chat, send messages, invite experts, mutate Hub state, mutate memory, or change routing",
	}
}

func groupDiscussionResult(value interface{}, err error) string {
	if err != nil {
		return groupDiscussionJSON(map[string]interface{}{"ok": false, "error": err.Error()})
	}
	return groupDiscussionJSON(map[string]interface{}{"ok": true, "result": value})
}

func groupDiscussionResultWithSafeHandoff(value map[string]interface{}, focusContext map[string]interface{}, recommendedToolCall *GroupDiscussionToolCallSuggestion, boundary string, err error) string {
	if err != nil {
		return groupDiscussionResult(value, err)
	}
	payload := map[string]interface{}{"ok": true, "result": value}
	if len(focusContext) > 0 {
		payload["recommended_focus_context"] = focusContext
	}
	if recommendedToolCall != nil {
		payload["recommended_tool_call"] = normalizeGroupDiscussionSafeToolCall(recommendedToolCall, focusContext, boundary)
	}
	if boundary = strings.TrimSpace(boundary); boundary != "" {
		payload["non_executing_boundary"] = boundary
	}
	return groupDiscussionJSON(payload)
}

func normalizeGroupDiscussionSafeToolCall(call *GroupDiscussionToolCallSuggestion, focusContext map[string]interface{}, boundary string) *GroupDiscussionToolCallSuggestion {
	if call == nil {
		return nil
	}
	normalized := *call
	if len(normalized.RecommendedFocusContext) == 0 && len(focusContext) > 0 {
		normalized.RecommendedFocusContext = focusContext
	}
	if len(normalized.DiscussionFocusContext) == 0 && len(normalized.RecommendedFocusContext) > 0 {
		normalized.DiscussionFocusContext = normalized.RecommendedFocusContext
	}
	normalized.NonExecuting = true
	if strings.TrimSpace(normalized.NonExecutingBoundary) == "" {
		normalized.NonExecutingBoundary = strings.TrimSpace(boundary)
	}
	return &normalized
}

func groupDiscussionJSON(value interface{}) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("group discussion result encode failed: %v", err)
	}
	return string(data)
}

func groupDiscussionStringSlice(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
		return out
	default:
		return nil
	}
}

func groupDiscussionBool(value interface{}) bool {
	b, _ := value.(bool)
	return b
}

func groupDiscussionMessageKind(value string) a2a.MessageKind {
	kind := a2a.MessageKind(strings.TrimSpace(strings.ToLower(value)))
	switch kind {
	case a2a.MessageQuestion, a2a.MessageAnswer, a2a.MessageEvidence, a2a.MessageObjection:
		return kind
	default:
		return a2a.MessageStatement
	}
}

func groupDiscussionReviewPosition(value string) a2a.ReviewPosition {
	position := a2a.ReviewPosition(strings.TrimSpace(strings.ToLower(value)))
	switch position {
	case a2a.ReviewApprove, a2a.ReviewReject, a2a.ReviewConcern, a2a.ReviewAbstain:
		return position
	default:
		return a2a.ReviewAbstain
	}
}

func groupDiscussionInt(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}
