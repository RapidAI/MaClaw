package main

import "strings"

type groupDiscussionWorkflowActionKind string

const (
	groupDiscussionWorkflowActionUnknown                 groupDiscussionWorkflowActionKind = ""
	groupDiscussionWorkflowActionReviewEscalationHandoff groupDiscussionWorkflowActionKind = "review_escalation_handoff"
	groupDiscussionWorkflowActionPrepareRollbackReview   groupDiscussionWorkflowActionKind = "prepare_rollback_review"
	groupDiscussionWorkflowActionReviewResultReuse       groupDiscussionWorkflowActionKind = "review_result_reuse"
	groupDiscussionWorkflowActionPrepareEscalation       groupDiscussionWorkflowActionKind = "prepare_escalation"
	groupDiscussionWorkflowActionRecordDecision          groupDiscussionWorkflowActionKind = "record_decision"
	groupDiscussionWorkflowActionCollectReviews          groupDiscussionWorkflowActionKind = "collect_reviews"
	groupDiscussionWorkflowActionPreviewSummary          groupDiscussionWorkflowActionKind = "preview_summary"
	groupDiscussionWorkflowActionSendFollowup            groupDiscussionWorkflowActionKind = "send_followup"
	groupDiscussionWorkflowActionWaitForAnswers          groupDiscussionWorkflowActionKind = "wait_for_answers"
)

func normalizeGroupDiscussionWorkflowActionKind(value string) groupDiscussionWorkflowActionKind {
	switch groupDiscussionWorkflowActionKind(strings.TrimSpace(value)) {
	case groupDiscussionWorkflowActionReviewEscalationHandoff:
		return groupDiscussionWorkflowActionReviewEscalationHandoff
	case groupDiscussionWorkflowActionPrepareRollbackReview:
		return groupDiscussionWorkflowActionPrepareRollbackReview
	case groupDiscussionWorkflowActionReviewResultReuse:
		return groupDiscussionWorkflowActionReviewResultReuse
	case groupDiscussionWorkflowActionPrepareEscalation:
		return groupDiscussionWorkflowActionPrepareEscalation
	case groupDiscussionWorkflowActionRecordDecision:
		return groupDiscussionWorkflowActionRecordDecision
	case groupDiscussionWorkflowActionCollectReviews:
		return groupDiscussionWorkflowActionCollectReviews
	case groupDiscussionWorkflowActionPreviewSummary:
		return groupDiscussionWorkflowActionPreviewSummary
	case groupDiscussionWorkflowActionSendFollowup:
		return groupDiscussionWorkflowActionSendFollowup
	case groupDiscussionWorkflowActionWaitForAnswers:
		return groupDiscussionWorkflowActionWaitForAnswers
	default:
		return groupDiscussionWorkflowActionUnknown
	}
}

func (kind groupDiscussionWorkflowActionKind) String() string {
	return string(kind)
}
