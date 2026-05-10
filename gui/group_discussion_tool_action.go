package main

import "strings"

type groupDiscussionToolAction string

const (
	groupDiscussionToolActionUnknown             groupDiscussionToolAction = ""
	groupDiscussionToolActionStatus              groupDiscussionToolAction = "status"
	groupDiscussionToolActionListExperts         groupDiscussionToolAction = "list_experts"
	groupDiscussionToolActionRankExperts         groupDiscussionToolAction = "rank_experts"
	groupDiscussionToolActionListMine            groupDiscussionToolAction = "list_mine"
	groupDiscussionToolActionGetDiscussion       groupDiscussionToolAction = "get_discussion"
	groupDiscussionToolActionGetDetail           groupDiscussionToolAction = "get_detail"
	groupDiscussionToolActionWorkflowState       groupDiscussionToolAction = "workflow_state"
	groupDiscussionToolActionWorkflowActionDraft groupDiscussionToolAction = "workflow_action_draft"
	groupDiscussionToolActionEscalationRoute     groupDiscussionToolAction = "escalation_route"
	groupDiscussionToolActionRollbackReadiness   groupDiscussionToolAction = "rollback_readiness"
	groupDiscussionToolActionReadiness           groupDiscussionToolAction = "readiness"
	groupDiscussionToolActionSummarizeResult     groupDiscussionToolAction = "summarize_result"
	groupDiscussionToolActionCleanupStale        groupDiscussionToolAction = "cleanup_stale"
	groupDiscussionToolActionProcessInvites      groupDiscussionToolAction = "process_invites"
	groupDiscussionToolActionSuggest             groupDiscussionToolAction = "suggest"
	groupDiscussionToolActionStartAuthorized     groupDiscussionToolAction = "start_authorized"
	groupDiscussionToolActionSendMessage         groupDiscussionToolAction = "send_message"
	groupDiscussionToolActionAddProposal         groupDiscussionToolAction = "add_proposal"
	groupDiscussionToolActionAddReview           groupDiscussionToolAction = "add_review"
	groupDiscussionToolActionDecide              groupDiscussionToolAction = "decide"
	groupDiscussionToolActionEscalate            groupDiscussionToolAction = "escalate"
	groupDiscussionToolActionSubmitResult        groupDiscussionToolAction = "submit_result"
	groupDiscussionToolActionSetState            groupDiscussionToolAction = "set_state"
)

func normalizeGroupDiscussionToolAction(value string) groupDiscussionToolAction {
	switch groupDiscussionToolAction(strings.ToLower(strings.TrimSpace(value))) {
	case groupDiscussionToolActionStatus:
		return groupDiscussionToolActionStatus
	case groupDiscussionToolActionListExperts:
		return groupDiscussionToolActionListExperts
	case groupDiscussionToolActionRankExperts:
		return groupDiscussionToolActionRankExperts
	case groupDiscussionToolActionListMine:
		return groupDiscussionToolActionListMine
	case groupDiscussionToolActionGetDiscussion:
		return groupDiscussionToolActionGetDiscussion
	case groupDiscussionToolActionGetDetail:
		return groupDiscussionToolActionGetDetail
	case groupDiscussionToolActionWorkflowState:
		return groupDiscussionToolActionWorkflowState
	case groupDiscussionToolActionWorkflowActionDraft:
		return groupDiscussionToolActionWorkflowActionDraft
	case groupDiscussionToolActionEscalationRoute:
		return groupDiscussionToolActionEscalationRoute
	case groupDiscussionToolActionRollbackReadiness:
		return groupDiscussionToolActionRollbackReadiness
	case groupDiscussionToolActionReadiness:
		return groupDiscussionToolActionReadiness
	case groupDiscussionToolActionSummarizeResult:
		return groupDiscussionToolActionSummarizeResult
	case groupDiscussionToolActionCleanupStale:
		return groupDiscussionToolActionCleanupStale
	case groupDiscussionToolActionProcessInvites:
		return groupDiscussionToolActionProcessInvites
	case groupDiscussionToolActionSuggest:
		return groupDiscussionToolActionSuggest
	case groupDiscussionToolActionStartAuthorized:
		return groupDiscussionToolActionStartAuthorized
	case groupDiscussionToolActionSendMessage:
		return groupDiscussionToolActionSendMessage
	case groupDiscussionToolActionAddProposal:
		return groupDiscussionToolActionAddProposal
	case groupDiscussionToolActionAddReview:
		return groupDiscussionToolActionAddReview
	case groupDiscussionToolActionDecide:
		return groupDiscussionToolActionDecide
	case groupDiscussionToolActionEscalate:
		return groupDiscussionToolActionEscalate
	case groupDiscussionToolActionSubmitResult:
		return groupDiscussionToolActionSubmitResult
	case groupDiscussionToolActionSetState:
		return groupDiscussionToolActionSetState
	default:
		return groupDiscussionToolActionUnknown
	}
}
