import { useCallback, useEffect, useMemo, useState, type CSSProperties } from "react";
import { GroupDiscussionAddProposal, GroupDiscussionAddReview, GroupDiscussionDecide, GroupDiscussionEscalate, GroupDiscussionGetConsultationDetail, GroupDiscussionGetWorkflowState, GroupDiscussionListExperts, GroupDiscussionListMine, GroupDiscussionSendInvitation, GroupDiscussionSendMessage, GroupDiscussionSetState, GroupDiscussionSummarizeResult } from "../../../wailsjs/go/main/App";
import { useDialog } from "../CustomDialog";
import { colors, radius } from "./styles";

type Props = { lang: string; enabled: boolean; onOpenExperienceTrace?: (focus?: string) => void };
type DiscussionSummary = { id?: string; role?: string; status?: string; topic?: string; question?: string; result_summary?: string; participant_ids?: string[]; message_count?: number; answer_count?: number; expected_answer_count?: number; ready_to_summarize?: boolean; readiness_reason?: string; created_at?: string; updated_at?: string };
type DiscussionMessage = { id?: string; from_id?: string; kind?: string; content?: string; created_at?: string; evidence?: string[] };
type DiscussionProposal = { id?: string; author_id?: string; title?: string; content?: string; goals?: string[]; constraints?: string[]; status?: string; risks?: string[] };
type DiscussionReview = { id?: string; proposal_id?: string; reviewer_id?: string; position?: string; comment?: string };
type DiscussionReviewSummary = { approvals?: number; rejections?: number; concerns?: number; abstains?: number; reviewed_by?: string[] };
type DiscussionDecision = { summary?: string; rationale?: string; decided_by?: string[]; rollback_on?: string[] };
type DiscussionEscalation = { raised_by?: string; reason?: string; target?: string; created_at?: string };
type DiscussionSession = { escalation?: DiscussionEscalation | null };
type DiscussionDetail = { discussion?: DiscussionSummary; session?: DiscussionSession | null; messages?: DiscussionMessage[]; proposals?: DiscussionProposal[]; reviews?: DiscussionReview[]; review_summaries?: Record<string, DiscussionReviewSummary>; decision?: DiscussionDecision | null };
type DiscussionWorkflowReadiness = { ready?: boolean; reason?: string };
type DiscussionWorkflowBlocker = { code?: string; severity?: string; message?: string; proposal_id?: string; proposal_ids?: string[]; participants?: string[]; count?: number };
type DiscussionProposalWorkflowState = { id?: string; title?: string; status?: string; review_summary?: DiscussionReviewSummary; review_count?: number; policy_satisfied?: boolean; blocking_reviews?: boolean; missing_reviewers?: string[]; blockers?: DiscussionWorkflowBlocker[] };
type DiscussionEscalationRoute = { status?: string; target?: string; reason?: string; suggested?: boolean; recommended_focus_context?: Record<string, unknown>; recommended_tool_call?: DiscussionToolCallSuggestion | null; triggers?: string[]; policy_evidence?: string[]; suggested_next_action_kind?: string; blocking_review_count?: number; decidable_proposal_count?: number; existing_escalation?: DiscussionEscalation | null; non_executing_boundary?: string };
type DiscussionToolCallSuggestion = { tool?: string; args?: Record<string, unknown>; recommended_focus_context?: Record<string, unknown>; discussion_focus_context?: Record<string, unknown>; non_executing?: boolean; non_executing_boundary?: string };
type DiscussionWorkflowActionDraft = { action_kind?: string; title?: string; summary?: string; recommended_focus_context?: Record<string, unknown>; suggested_next_action_kind?: string; proposal_id?: string; target_participants?: string[]; target_proposal_ids?: string[]; escalation_target?: string; escalation_reason?: string; evidence?: string[]; risk_boundaries?: string[]; checklist?: string[]; suggested_arguments?: Record<string, unknown>; recommended_tool_call?: DiscussionToolCallSuggestion | null; requires_confirmation?: boolean; non_executing_boundary?: string };
type DiscussionRollbackReadiness = { has_decision?: boolean; proposal_id?: string; decision_summary?: string; decision_rationale?: string; rollback_on?: string[]; matched_triggers?: string[]; unmatched_triggers?: string[]; evidence?: string[]; suggested?: boolean; recommended_focus_context?: Record<string, unknown>; suggested_next_action_kind?: string; suggested_next_action?: string; recommended_tool_call?: DiscussionToolCallSuggestion | null; non_executing_boundary?: string };
type DiscussionWorkflowState = { status?: string; readiness?: DiscussionWorkflowReadiness; message_count?: number; proposal_count?: number; review_count?: number; open_proposal_count?: number; decidable_proposal_count?: number; blocking_review_count?: number; missing_answer_participants?: string[]; workflow_blockers?: DiscussionWorkflowBlocker[]; has_decision?: boolean; has_escalation?: boolean; has_result?: boolean; proposals?: DiscussionProposalWorkflowState[]; suggested_next_action_kind?: string; suggested_next_action?: string; recommended_focus_context?: Record<string, unknown>; recommended_tool_call?: DiscussionToolCallSuggestion | null; escalation_route?: DiscussionEscalationRoute | null; rollback_readiness?: DiscussionRollbackReadiness | null; workflow_action_draft?: DiscussionWorkflowActionDraft | null; non_executing_boundary?: string };
type DiscussionSummaryResult = { summary?: string; rationale?: string; risks?: string[]; disagreements?: string[]; open_questions?: string[]; participant_contributions?: Record<string, string>; confidence?: number; answer_count?: number; used_llm?: boolean; submitted?: boolean; injected?: boolean; recommended_focus_context?: Record<string, unknown>; recommended_tool_call?: DiscussionToolCallSuggestion | null; non_executing_boundary?: string };
type GroupExpert = { agent_id?: string; display_name?: string; skills?: string[]; available?: boolean; discoverable?: boolean; contribution_score?: number; contribution_evidence?: number };
type DiscussionSafeHandoff = { focusContext?: Record<string, unknown> | null; recommendedToolCall?: DiscussionToolCallSuggestion | null; boundary?: string };
type DiscussionStateAction = "pause" | "resume" | "cancel";
type ProposalActionKind = "proposal" | "review" | "decide" | "escalate";

const copy = {
    en: { title: "Discussion History", desc: "Review current-Hub discussions that involve this MaClaw and jump to the matching experience trace.", all: "All", participated: "Participated", initiated: "Initiated", refresh: "Refresh", refreshing: "Refreshing...", view: "View", experience: "Experience", empty: "No discussions found", disabled: "Enable group discussion to load current-Hub history.", loading: "Loading...", messages: "Messages", proposals: "Proposals", reviews: "Reviews", decision: "Decision", result: "Result", close: "Close", topicFallback: "Untitled discussion", question: "Question", participants: "participants", answers: "answers", ready: "Ready", waiting: "Waiting", decidedBy: "Decided by", rollback: "Rollback", risks: "Risks", agent: "agent", proposal: "proposal", summarize: "Summarize", summarizing: "Summarizing...", summaryPreview: "Summary Preview", usedLLM: "LLM", confidence: "Confidence", contributions: "Contributions", previewOnly: "Preview only", inject: "Inject to Chat", injecting: "Injecting...", injected: "Injected", submit: "Submit Result", submitting: "Submitting...", submitted: "Submitted", confirmAction: "Confirm Action", confirmInject: "Inject this previewed discussion summary into the current chat? It may affect the assistant context.", confirmSubmit: "Submit this previewed summary as the shared Hub discussion result?", noWrite: "No chat/Hub delivery reported.", addMessage: "Add Message", messageKind: "Type", messageText: "Message", messagePlaceholder: "Write a focused follow-up, evidence, or objection for this discussion.", statement: "Statement", questionKind: "Question", answerKind: "Answer", evidenceKind: "Evidence", objectionKind: "Objection", sendMessage: "Send", sendingMessage: "Sending...", messageSent: "Message sent", confirmSend: "Send this message to the shared Hub discussion?", closedDiscussion: "This discussion is no longer open for new messages.", inviteExpert: "Invite Expert", loadExperts: "Load Experts", loadingExperts: "Loading experts...", noExperts: "No eligible experts found", inviteAs: "Invite as", observeRole: "Observe", speakRole: "Speak", reviewRole: "Review", invite: "Invite", inviting: "Inviting...", invited: "Invitation sent", confirmInvite: "Invite this expert into the shared Hub discussion?", stateControls: "State Controls", pause: "Pause", resume: "Resume", cancel: "Cancel", pausing: "Pausing...", resuming: "Resuming...", cancelling: "Cancelling...", paused: "Pause recorded", resumed: "Resume recorded", cancelled: "Discussion cancelled", confirmPause: "Record a pause marker for this shared Hub discussion?", confirmResume: "Record a resume marker for this shared Hub discussion?", confirmCancel: "Cancel this shared Hub discussion? It will be closed to new collaboration.", proposalActions: "Proposal Review", addProposal: "Add Proposal", proposalTitle: "Title", proposalTitlePlaceholder: "Proposal title", proposalContent: "Proposal", proposalPlaceholder: "Describe the concrete proposal.", proposalRisks: "Risks", proposalRisksPlaceholder: "Comma or newline separated risks", sendProposal: "Send Proposal", sendingProposal: "Sending proposal...", proposalSent: "Proposal sent", confirmProposal: "Send this proposal to the shared Hub discussion?", reviewProposal: "Review Proposal", noOpenProposals: "No open proposals yet", proposalSelect: "Proposal", reviewPosition: "Position", approve: "Approve", reject: "Reject", concern: "Concern", abstain: "Abstain", reviewComment: "Comment", reviewPlaceholder: "Add concise review reasoning.", sendReview: "Send Review", sendingReview: "Sending review...", reviewSent: "Review sent", confirmReview: "Submit this review to the shared Hub discussion?", decideProposal: "Decide Proposal", decisionSummary: "Decision Summary", decisionPlaceholder: "Optional final decision summary", decideAction: "Decide", deciding: "Deciding...", decisionRecorded: "Decision recorded", confirmDecide: "Record this proposal as the shared Hub decision? The discussion will be decided when policy is satisfied.", decisionRationale: "Decision Rationale", decisionRationalePlaceholder: "Why this proposal is accepted", decisionRollback: "Rollback Conditions", decisionRollbackPlaceholder: "Comma or newline separated rollback triggers", escalation: "Escalation", escalationReason: "Reason", escalationReasonPlaceholder: "Why this discussion needs escalation", escalationTarget: "Target", escalationTargetPlaceholder: "iworkercenter", escalateAction: "Escalate", escalating: "Escalating...", escalated: "Escalation recorded", confirmEscalate: "Escalate this shared Hub discussion? It will stop normal open collaboration.", reviewSummary: "Review summary", approvals: "approvals", rejections: "rejections", concerns: "concerns", abstains: "abstains" },
    zhHans: { title: "\u8ba8\u8bba\u5386\u53f2", desc: "\u67e5\u770b\u6b64 MaClaw \u53c2\u4e0e\u7684\u5f53\u524d Hub \u8ba8\u8bba\uff0c\u5e76\u8df3\u8f6c\u5230\u5bf9\u5e94\u7ecf\u9a8c\u8f68\u8ff9\u3002", all: "\u5168\u90e8", participated: "\u5df2\u53c2\u4e0e", initiated: "\u5df2\u53d1\u8d77", refresh: "\u5237\u65b0", refreshing: "\u5237\u65b0\u4e2d...", view: "\u67e5\u770b", experience: "\u7ecf\u9a8c", empty: "\u6682\u65e0\u8ba8\u8bba", disabled: "\u5f00\u542f\u7fa4\u7ec4\u8ba8\u8bba\u540e\u53ef\u52a0\u8f7d\u5f53\u524d Hub \u5386\u53f2\u3002", loading: "\u52a0\u8f7d\u4e2d...", messages: "\u6d88\u606f", proposals: "\u63d0\u6848", reviews: "\u8bc4\u5ba1", decision: "\u51b3\u7b56", result: "\u7ed3\u679c", close: "\u5173\u95ed", topicFallback: "\u672a\u547d\u540d\u8ba8\u8bba", question: "\u95ee\u9898", participants: "\u53c2\u4e0e\u8005", answers: "\u56de\u7b54", ready: "\u5df2\u5c31\u7eea", waiting: "\u7b49\u5f85\u4e2d", decidedBy: "\u51b3\u7b56\u4eba", rollback: "\u56de\u6eda", risks: "\u98ce\u9669", agent: "Agent", proposal: "\u63d0\u6848", summarize: "\u603b\u7ed3", summarizing: "\u603b\u7ed3\u4e2d...", summaryPreview: "\u603b\u7ed3\u9884\u89c8", usedLLM: "LLM", confidence: "\u7f6e\u4fe1\u5ea6", contributions: "\u53c2\u4e0e\u8d21\u732e", previewOnly: "\u4ec5\u9884\u89c8", inject: "\u6ce8\u5165\u804a\u5929", injecting: "\u6ce8\u5165\u4e2d...", injected: "\u5df2\u6ce8\u5165\u804a\u5929", submit: "\u63d0\u4ea4\u7ed3\u679c", submitting: "\u63d0\u4ea4\u4e2d...", submitted: "\u5df2\u63d0\u4ea4\u5230 Hub", confirmAction: "\u786e\u8ba4\u64cd\u4f5c", confirmInject: "\u5c06\u8fd9\u4e2a\u5df2\u9884\u89c8\u7684\u8ba8\u8bba\u603b\u7ed3\u6ce8\u5165\u5f53\u524d\u804a\u5929\uff1f\u5b83\u53ef\u80fd\u5f71\u54cd\u52a9\u624b\u4e0a\u4e0b\u6587\u3002", confirmSubmit: "\u5c06\u8fd9\u4e2a\u5df2\u9884\u89c8\u7684\u603b\u7ed3\u63d0\u4ea4\u4e3a\u5171\u4eab Hub \u8ba8\u8bba\u7ed3\u679c\uff1f", noWrite: "\u672a\u62a5\u544a\u804a\u5929/Hub \u6295\u9012\u3002", addMessage: "\u8865\u5145\u53d1\u8a00", messageKind: "\u7c7b\u578b", messageText: "\u5185\u5bb9", messagePlaceholder: "\u5199\u5165\u9762\u5411\u672c\u8f6e\u8ba8\u8bba\u7684\u8ffd\u95ee\u3001\u8bc1\u636e\u6216\u53cd\u5bf9\u610f\u89c1\u3002", statement: "\u9648\u8ff0", questionKind: "\u95ee\u9898", answerKind: "\u56de\u7b54", evidenceKind: "\u8bc1\u636e", objectionKind: "\u53cd\u5bf9", sendMessage: "\u53d1\u9001", sendingMessage: "\u53d1\u9001\u4e2d...", messageSent: "\u6d88\u606f\u5df2\u53d1\u9001", confirmSend: "\u5c06\u8fd9\u6761\u6d88\u606f\u53d1\u9001\u5230\u5171\u4eab Hub \u8ba8\u8bba\uff1f", closedDiscussion: "\u8be5\u8ba8\u8bba\u5df2\u4e0d\u518d\u63a5\u53d7\u65b0\u6d88\u606f\u3002", inviteExpert: "\u9080\u8bf7\u4e13\u5bb6", loadExperts: "\u52a0\u8f7d\u4e13\u5bb6", loadingExperts: "\u52a0\u8f7d\u4e13\u5bb6\u4e2d...", noExperts: "\u672a\u627e\u5230\u53ef\u9080\u8bf7\u4e13\u5bb6", inviteAs: "\u9080\u8bf7\u4e3a", observeRole: "\u65c1\u542c", speakRole: "\u53d1\u8a00", reviewRole: "\u8bc4\u5ba1", invite: "\u9080\u8bf7", inviting: "\u9080\u8bf7\u4e2d...", invited: "\u9080\u8bf7\u5df2\u53d1\u9001", confirmInvite: "\u5c06\u8fd9\u4f4d\u4e13\u5bb6\u9080\u8bf7\u5230\u5171\u4eab Hub \u8ba8\u8bba\uff1f", stateControls: "\u72b6\u6001\u63a7\u5236", pause: "\u6682\u505c", resume: "\u6062\u590d", cancel: "\u53d6\u6d88", pausing: "\u6682\u505c\u4e2d...", resuming: "\u6062\u590d\u4e2d...", cancelling: "\u53d6\u6d88\u4e2d...", paused: "\u5df2\u8bb0\u5f55\u6682\u505c", resumed: "\u5df2\u8bb0\u5f55\u6062\u590d", cancelled: "\u5df2\u53d6\u6d88\u8ba8\u8bba", confirmPause: "\u4e3a\u8fd9\u4e2a\u5171\u4eab Hub \u8ba8\u8bba\u8bb0\u5f55\u6682\u505c\u6807\u8bb0\uff1f", confirmResume: "\u4e3a\u8fd9\u4e2a\u5171\u4eab Hub \u8ba8\u8bba\u8bb0\u5f55\u6062\u590d\u6807\u8bb0\uff1f", confirmCancel: "\u53d6\u6d88\u8fd9\u4e2a\u5171\u4eab Hub \u8ba8\u8bba\uff1f\u5b83\u5c06\u5173\u95ed\u5e76\u4e0d\u518d\u63a5\u53d7\u65b0\u7684\u534f\u4f5c\u3002", proposalActions: "\u63d0\u6848\u8bc4\u5ba1", addProposal: "\u65b0\u589e\u63d0\u6848", proposalTitle: "\u6807\u9898", proposalTitlePlaceholder: "\u63d0\u6848\u6807\u9898", proposalContent: "\u63d0\u6848", proposalPlaceholder: "\u63cf\u8ff0\u53ef\u6267\u884c\u7684\u5177\u4f53\u63d0\u6848\u3002", proposalRisks: "\u98ce\u9669", proposalRisksPlaceholder: "\u7528\u9017\u53f7\u6216\u6362\u884c\u5206\u9694\u98ce\u9669", sendProposal: "\u53d1\u9001\u63d0\u6848", sendingProposal: "\u53d1\u9001\u63d0\u6848\u4e2d...", proposalSent: "\u63d0\u6848\u5df2\u53d1\u9001", confirmProposal: "\u5c06\u8fd9\u4e2a\u63d0\u6848\u53d1\u9001\u5230\u5171\u4eab Hub \u8ba8\u8bba\uff1f", reviewProposal: "\u8bc4\u5ba1\u63d0\u6848", noOpenProposals: "\u6682\u65e0\u53ef\u8bc4\u5ba1\u63d0\u6848", proposalSelect: "\u63d0\u6848", reviewPosition: "\u7acb\u573a", approve: "\u540c\u610f", reject: "\u62d2\u7edd", concern: "\u62c5\u5fe7", abstain: "\u5f03\u6743", reviewComment: "\u8bc4\u5ba1\u610f\u89c1", reviewPlaceholder: "\u8865\u5145\u7b80\u660e\u8bc4\u5ba1\u7406\u7531\u3002", sendReview: "\u53d1\u9001\u8bc4\u5ba1", sendingReview: "\u53d1\u9001\u8bc4\u5ba1\u4e2d...", reviewSent: "\u8bc4\u5ba1\u5df2\u53d1\u9001", confirmReview: "\u5c06\u8fd9\u6761\u8bc4\u5ba1\u63d0\u4ea4\u5230\u5171\u4eab Hub \u8ba8\u8bba\uff1f", decideProposal: "\u5f62\u6210\u51b3\u7b56", decisionSummary: "\u51b3\u7b56\u6458\u8981", decisionPlaceholder: "\u53ef\u9009\u7684\u6700\u7ec8\u51b3\u7b56\u6458\u8981", decideAction: "\u51b3\u7b56", deciding: "\u51b3\u7b56\u4e2d...", decisionRecorded: "\u51b3\u7b56\u5df2\u8bb0\u5f55", confirmDecide: "\u5c06\u6b64\u63d0\u6848\u8bb0\u5f55\u4e3a\u5171\u4eab Hub \u51b3\u7b56\uff1f\u6ee1\u8db3\u51b3\u7b56\u7b56\u7565\u540e\u8ba8\u8bba\u4f1a\u8fdb\u5165\u5df2\u51b3\u72b6\u6001\u3002", decisionRationale: "\u51b3\u7b56\u7406\u7531", decisionRationalePlaceholder: "\u4e3a\u4ec0\u4e48\u63a5\u53d7\u8fd9\u4e2a\u63d0\u6848", decisionRollback: "\u56de\u6eda\u6761\u4ef6", decisionRollbackPlaceholder: "\u7528\u9017\u53f7\u6216\u6362\u884c\u5206\u9694\u56de\u6eda\u89e6\u53d1\u6761\u4ef6", escalation: "\u5347\u7ea7", escalationReason: "\u539f\u56e0", escalationReasonPlaceholder: "\u4e3a\u4ec0\u4e48\u8be5\u8ba8\u8bba\u9700\u8981\u5347\u7ea7\u5904\u7406", escalationTarget: "\u76ee\u6807", escalationTargetPlaceholder: "iworkercenter", escalateAction: "\u5347\u7ea7", escalating: "\u5347\u7ea7\u4e2d...", escalated: "\u5347\u7ea7\u5df2\u8bb0\u5f55", confirmEscalate: "\u5347\u7ea7\u8fd9\u4e2a\u5171\u4eab Hub \u8ba8\u8bba\uff1f\u5b83\u5c06\u7ed3\u675f\u666e\u901a\u5f00\u653e\u534f\u4f5c\u3002", reviewSummary: "\u8bc4\u5ba1\u7edf\u8ba1", approvals: "\u540c\u610f", rejections: "\u62d2\u7edd", concerns: "\u62c5\u5fe7", abstains: "\u5f03\u6743" },
    zhHant: { title: "\u8a0e\u8ad6\u6b77\u53f2", desc: "\u67e5\u770b\u6b64 MaClaw \u53c3\u8207\u7684\u76ee\u524d Hub \u8a0e\u8ad6\uff0c\u4e26\u8df3\u8f49\u5230\u5c0d\u61c9\u7d93\u9a57\u8ecc\u8de1\u3002", all: "\u5168\u90e8", participated: "\u5df2\u53c3\u8207", initiated: "\u5df2\u767c\u8d77", refresh: "\u91cd\u65b0\u6574\u7406", refreshing: "\u6574\u7406\u4e2d...", view: "\u67e5\u770b", experience: "\u7d93\u9a57", empty: "\u66ab\u7121\u8a0e\u8ad6", disabled: "\u958b\u555f\u7fa4\u7d44\u8a0e\u8ad6\u5f8c\u53ef\u8f09\u5165\u76ee\u524d Hub \u6b77\u53f2\u3002", loading: "\u8f09\u5165\u4e2d...", messages: "\u8a0a\u606f", proposals: "\u63d0\u6848", reviews: "\u8a55\u5be9", decision: "\u6c7a\u7b56", result: "\u7d50\u679c", close: "\u95dc\u9589", topicFallback: "\u672a\u547d\u540d\u8a0e\u8ad6", question: "\u554f\u984c", participants: "\u53c3\u8207\u8005", answers: "\u56de\u7b54", ready: "\u5df2\u5c31\u7dd2", waiting: "\u7b49\u5f85\u4e2d", decidedBy: "\u6c7a\u7b56\u8005", rollback: "\u56de\u6efe", risks: "\u98a8\u96aa", agent: "Agent", proposal: "\u63d0\u6848", summarize: "\u7e3d\u7d50", summarizing: "\u7e3d\u7d50\u4e2d...", summaryPreview: "\u7e3d\u7d50\u9810\u89bd", usedLLM: "LLM", confidence: "\u7f6e\u4fe1\u5ea6", contributions: "\u53c3\u8207\u8ca2\u737b", previewOnly: "\u50c5\u9810\u89bd", inject: "\u6ce8\u5165\u804a\u5929", injecting: "\u6ce8\u5165\u4e2d...", injected: "\u5df2\u6ce8\u5165\u804a\u5929", submit: "\u63d0\u4ea4\u7d50\u679c", submitting: "\u63d0\u4ea4\u4e2d...", submitted: "\u5df2\u63d0\u4ea4\u5230 Hub", confirmAction: "\u78ba\u8a8d\u64cd\u4f5c", confirmInject: "\u5c07\u9019\u500b\u5df2\u9810\u89bd\u7684\u8a0e\u8ad6\u7e3d\u7d50\u6ce8\u5165\u76ee\u524d\u804a\u5929\uff1f\u5b83\u53ef\u80fd\u5f71\u97ff\u52a9\u7406\u4e0a\u4e0b\u6587\u3002", confirmSubmit: "\u5c07\u9019\u500b\u5df2\u9810\u89bd\u7684\u7e3d\u7d50\u63d0\u4ea4\u70ba\u5171\u4eab Hub \u8a0e\u8ad6\u7d50\u679c\uff1f", noWrite: "\u672a\u56de\u5831\u804a\u5929/Hub \u6295\u905e\u3002", addMessage: "\u88dc\u5145\u767c\u8a00", messageKind: "\u985e\u578b", messageText: "\u5167\u5bb9", messagePlaceholder: "\u5beb\u5165\u9762\u5411\u672c\u8f2a\u8a0e\u8ad6\u7684\u8ffd\u554f\u3001\u8b49\u64da\u6216\u53cd\u5c0d\u610f\u898b\u3002", statement: "\u9673\u8ff0", questionKind: "\u554f\u984c", answerKind: "\u56de\u7b54", evidenceKind: "\u8b49\u64da", objectionKind: "\u53cd\u5c0d", sendMessage: "\u767c\u9001", sendingMessage: "\u767c\u9001\u4e2d...", messageSent: "\u8a0a\u606f\u5df2\u767c\u9001", confirmSend: "\u5c07\u9019\u689d\u8a0a\u606f\u767c\u9001\u5230\u5171\u4eab Hub \u8a0e\u8ad6\uff1f", closedDiscussion: "\u8a72\u8a0e\u8ad6\u5df2\u4e0d\u518d\u63a5\u53d7\u65b0\u8a0a\u606f\u3002", inviteExpert: "\u9080\u8acb\u5c08\u5bb6", loadExperts: "\u8f09\u5165\u5c08\u5bb6", loadingExperts: "\u8f09\u5165\u5c08\u5bb6\u4e2d...", noExperts: "\u672a\u627e\u5230\u53ef\u9080\u8acb\u5c08\u5bb6", inviteAs: "\u9080\u8acb\u70ba", observeRole: "\u65c1\u807d", speakRole: "\u767c\u8a00", reviewRole: "\u8a55\u5be9", invite: "\u9080\u8acb", inviting: "\u9080\u8acb\u4e2d...", invited: "\u9080\u8acb\u5df2\u767c\u9001", confirmInvite: "\u5c07\u9019\u4f4d\u5c08\u5bb6\u9080\u8acb\u5230\u5171\u4eab Hub \u8a0e\u8ad6\uff1f", stateControls: "\u72c0\u614b\u63a7\u5236", pause: "\u66ab\u505c", resume: "\u6062\u5fa9", cancel: "\u53d6\u6d88", pausing: "\u66ab\u505c\u4e2d...", resuming: "\u6062\u5fa9\u4e2d...", cancelling: "\u53d6\u6d88\u4e2d...", paused: "\u5df2\u8a18\u9304\u66ab\u505c", resumed: "\u5df2\u8a18\u9304\u6062\u5fa9", cancelled: "\u5df2\u53d6\u6d88\u8a0e\u8ad6", confirmPause: "\u70ba\u9019\u500b\u5171\u4eab Hub \u8a0e\u8ad6\u8a18\u9304\u66ab\u505c\u6a19\u8a18\uff1f", confirmResume: "\u70ba\u9019\u500b\u5171\u4eab Hub \u8a0e\u8ad6\u8a18\u9304\u6062\u5fa9\u6a19\u8a18\uff1f", confirmCancel: "\u53d6\u6d88\u9019\u500b\u5171\u4eab Hub \u8a0e\u8ad6\uff1f\u5b83\u5c07\u95dc\u9589\u4e26\u4e0d\u518d\u63a5\u53d7\u65b0\u7684\u5354\u4f5c\u3002", proposalActions: "\u63d0\u6848\u8a55\u5be9", addProposal: "\u65b0\u589e\u63d0\u6848", proposalTitle: "\u6a19\u984c", proposalTitlePlaceholder: "\u63d0\u6848\u6a19\u984c", proposalContent: "\u63d0\u6848", proposalPlaceholder: "\u63cf\u8ff0\u53ef\u57f7\u884c\u7684\u5177\u9ad4\u63d0\u6848\u3002", proposalRisks: "\u98a8\u96aa", proposalRisksPlaceholder: "\u7528\u9017\u865f\u6216\u63db\u884c\u5206\u9694\u98a8\u96aa", sendProposal: "\u50b3\u9001\u63d0\u6848", sendingProposal: "\u50b3\u9001\u63d0\u6848\u4e2d...", proposalSent: "\u63d0\u6848\u5df2\u50b3\u9001", confirmProposal: "\u5c07\u9019\u500b\u63d0\u6848\u50b3\u9001\u5230\u5171\u4eab Hub \u8a0e\u8ad6\uff1f", reviewProposal: "\u8a55\u5be9\u63d0\u6848", noOpenProposals: "\u66ab\u7121\u53ef\u8a55\u5be9\u63d0\u6848", proposalSelect: "\u63d0\u6848", reviewPosition: "\u7acb\u5834", approve: "\u540c\u610f", reject: "\u62d2\u7d55", concern: "\u64d4\u6182", abstain: "\u68c4\u6b0a", reviewComment: "\u8a55\u5be9\u610f\u898b", reviewPlaceholder: "\u88dc\u5145\u7c21\u660e\u8a55\u5be9\u7406\u7531\u3002", sendReview: "\u50b3\u9001\u8a55\u5be9", sendingReview: "\u50b3\u9001\u8a55\u5be9\u4e2d...", reviewSent: "\u8a55\u5be9\u5df2\u50b3\u9001", confirmReview: "\u5c07\u9019\u689d\u8a55\u5be9\u63d0\u4ea4\u5230\u5171\u4eab Hub \u8a0e\u8ad6\uff1f", decideProposal: "\u5f62\u6210\u6c7a\u7b56", decisionSummary: "\u6c7a\u7b56\u6458\u8981", decisionPlaceholder: "\u53ef\u9078\u7684\u6700\u7d42\u6c7a\u7b56\u6458\u8981", decideAction: "\u6c7a\u7b56", deciding: "\u6c7a\u7b56\u4e2d...", decisionRecorded: "\u6c7a\u7b56\u5df2\u8a18\u9304", confirmDecide: "\u5c07\u6b64\u63d0\u6848\u8a18\u9304\u70ba\u5171\u4eab Hub \u6c7a\u7b56\uff1f\u6eff\u8db3\u6c7a\u7b56\u7b56\u7565\u5f8c\u8a0e\u8ad6\u6703\u9032\u5165\u5df2\u6c7a\u72c0\u614b\u3002", decisionRationale: "\u6c7a\u7b56\u7406\u7531", decisionRationalePlaceholder: "\u70ba\u4ec0\u9ebc\u63a5\u53d7\u9019\u500b\u63d0\u6848", decisionRollback: "\u56de\u6efe\u689d\u4ef6", decisionRollbackPlaceholder: "\u7528\u9017\u865f\u6216\u63db\u884c\u5206\u9694\u56de\u6efe\u89f8\u767c\u689d\u4ef6", escalation: "\u5347\u7d1a", escalationReason: "\u539f\u56e0", escalationReasonPlaceholder: "\u70ba\u4ec0\u9ebc\u8a72\u8a0e\u8ad6\u9700\u8981\u5347\u7d1a\u8655\u7406", escalationTarget: "\u76ee\u6a19", escalationTargetPlaceholder: "iworkercenter", escalateAction: "\u5347\u7d1a", escalating: "\u5347\u7d1a\u4e2d...", escalated: "\u5347\u7d1a\u5df2\u8a18\u9304", confirmEscalate: "\u5347\u7d1a\u9019\u500b\u5171\u4eab Hub \u8a0e\u8ad6\uff1f\u5b83\u5c07\u7d50\u675f\u666e\u901a\u958b\u653e\u5354\u4f5c\u3002", reviewSummary: "\u8a55\u5be9\u7d71\u8a08", approvals: "\u540c\u610f", rejections: "\u62d2\u7d55", concerns: "\u64d4\u6182", abstains: "\u68c4\u6b0a" },
};

export function GroupDiscussionHistoryPanel({ lang, enabled, onOpenExperienceTrace }: Props) {
    const c = lang === "zh-Hant" ? copy.zhHant : lang === "zh-Hans" ? copy.zhHans : copy.en;
    const { showConfirm } = useDialog();
    const [role, setRole] = useState("");
    const [items, setItems] = useState<DiscussionSummary[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [detailID, setDetailID] = useState("");
    const [detail, setDetail] = useState<DiscussionDetail | null>(null);
    const [detailLoading, setDetailLoading] = useState(false);

    const load = useCallback(async () => {
        if (!enabled) return;
        setLoading(true);
        setError("");
        try {
            const result = await GroupDiscussionListMine(role);
            setItems(Array.isArray(result) ? result : []);
        } catch (e) {
            setError(String(e));
            setItems([]);
        } finally {
            setLoading(false);
        }
    }, [enabled, role]);

    useEffect(() => { if (enabled) void load(); }, [enabled, load]);

    const sorted = useMemo(() => items.slice().sort((a, b) => timeValue(b.updated_at || b.created_at) - timeValue(a.updated_at || a.created_at)), [items]);
    const openExperience = useCallback((id?: string) => { const clean = String(id || "").trim(); if (clean) onOpenExperienceTrace?.("discussion:" + clean); }, [onOpenExperienceTrace]);
    const openDetail = useCallback(async (id?: string) => {
        const clean = String(id || "").trim();
        if (!clean) return;
        setDetailID(clean);
        setDetail(null);
        setDetailLoading(true);
        setError("");
        try {
            setDetail(await GroupDiscussionGetConsultationDetail(clean));
        } catch (e) {
            setError(String(e));
        } finally {
            setDetailLoading(false);
        }
    }, []);

    return <section style={sectionStyle}>
        <div style={headerStyle}>
            <div><h4 style={titleStyle}>{c.title}</h4><div style={descStyle}>{c.desc}</div></div>
            <div style={toolbarStyle}>
                <select value={role} onChange={(e) => setRole(e.target.value)} disabled={!enabled || loading} style={selectStyle} aria-label={c.title}>
                    <option value="">{c.all}</option><option value="participated">{c.participated}</option><option value="initiated">{c.initiated}</option>
                </select>
                <button type="button" onClick={() => void load()} disabled={!enabled || loading} style={{ ...buttonStyle, opacity: !enabled || loading ? 0.58 : 1, cursor: !enabled || loading ? "default" : "pointer" }}>{loading ? c.refreshing : c.refresh}</button>
            </div>
        </div>
        {!enabled && <div style={emptyStyle}>{c.disabled}</div>}
        {enabled && error && <div role="alert" style={errorStyle}>{error}</div>}
        {enabled && loading && sorted.length === 0 && <div style={emptyStyle}>{c.loading}</div>}
        {enabled && !loading && sorted.length === 0 && !error && <div style={emptyStyle}>{c.empty}</div>}
        {enabled && sorted.length > 0 && <div style={listStyle}>{sorted.map((item) => <DiscussionRow key={item.id || item.topic || item.question} item={item} c={c} lang={lang} onView={openDetail} onExperience={onOpenExperienceTrace ? openExperience : undefined} />)}</div>}
        {(detailID || detailLoading) && <DiscussionDetailModal c={c} lang={lang} id={detailID} detail={detail} loading={detailLoading} showConfirm={showConfirm} onDetailUpdate={setDetail} onClose={() => { setDetailID(""); setDetail(null); }} onExperience={onOpenExperienceTrace ? openExperience : undefined} />}
    </section>;
}

function DiscussionRow({ item, c, lang, onView, onExperience }: { item: DiscussionSummary; c: typeof copy.en; lang: string; onView: (id?: string) => void; onExperience?: (id?: string) => void }) {
    const title = item.topic || item.question || c.topicFallback;
    const meta = [item.status, item.role, item.participant_ids?.length ? String(item.participant_ids.length) + " " + c.participants : "", item.answer_count ? String(item.answer_count) + " " + c.answers : ""].filter(Boolean).join(" \u00b7 ");
    return <div style={rowStyle}>
        <button type="button" onClick={() => onView(item.id)} style={rowMainStyle}>
            <span style={rowTitleStyle}>{title}</span>
            {item.question && item.topic && <span style={rowQuestionStyle}>{c.question}: {item.question}</span>}
            <span style={rowMetaStyle}>{formatDate(item.updated_at || item.created_at, lang)}{meta ? " \u00b7 " + meta : ""}</span>
            {item.result_summary && <span style={rowSummaryStyle}>{item.result_summary}</span>}
        </button>
        <div style={rowActionsStyle}>
            <span style={item.ready_to_summarize ? readyBadgeStyle : waitingBadgeStyle}>{item.ready_to_summarize ? c.ready : c.waiting}</span>
            <button type="button" onClick={() => onView(item.id)} style={buttonStyle}>{c.view}</button>
            {onExperience && <button type="button" onClick={() => onExperience(item.id)} style={primaryButtonStyle}>{c.experience}</button>}
        </div>
    </div>;
}

function DiscussionDetailModal({ c, lang, id, detail, loading, showConfirm, onDetailUpdate, onClose, onExperience }: { c: typeof copy.en; lang: string; id: string; detail: DiscussionDetail | null; loading: boolean; showConfirm: (message: string, title?: string) => Promise<boolean>; onDetailUpdate: (detail: DiscussionDetail) => void; onClose: () => void; onExperience?: (id?: string) => void }) {
    const discussion = detail?.discussion || {};
    const title = discussion.topic || discussion.question || id || c.topicFallback;
    const hasSubmittedResult = Boolean(discussion.result_summary || detail?.decision);
    const discussionStatus = String(discussion.status || "open").toLowerCase();
    const terminalDiscussion = discussionStatus === "closed" || discussionStatus === "decided" || discussionStatus === "escalated";
    const discussionOpen = discussionStatus === "open" && !hasSubmittedResult;
    const proposalOptions = useMemo(() => (detail?.proposals || []).filter(isOpenProposal), [detail]);
    const [summary, setSummary] = useState<DiscussionSummaryResult | null>(null);
    const [summaryLoading, setSummaryLoading] = useState(false);
    const [summaryError, setSummaryError] = useState("");
    const [actionLoading, setActionLoading] = useState<"" | "inject" | "submit">("");
    const [actionMessage, setActionMessage] = useState("");
    const [messageKind, setMessageKind] = useState("statement");
    const [messageText, setMessageText] = useState("");
    const [messageLoading, setMessageLoading] = useState(false);
    const [messageError, setMessageError] = useState("");
    const [messageStatus, setMessageStatus] = useState("");
    const [experts, setExperts] = useState<GroupExpert[]>([]);
    const [expertsLoaded, setExpertsLoaded] = useState(false);
    const [expertsLoading, setExpertsLoading] = useState(false);
    const [inviteTarget, setInviteTarget] = useState("");
    const [inviteRole, setInviteRole] = useState("speak");
    const [inviteLoading, setInviteLoading] = useState(false);
    const [inviteError, setInviteError] = useState("");
    const [inviteStatus, setInviteStatus] = useState("");
    const [stateLoading, setStateLoading] = useState<"" | DiscussionStateAction>("");
    const [stateError, setStateError] = useState("");
    const [stateStatus, setStateStatus] = useState("");
    const [proposalTitle, setProposalTitle] = useState("");
    const [proposalContent, setProposalContent] = useState("");
    const [proposalRisks, setProposalRisks] = useState("");
    const [reviewProposalID, setReviewProposalID] = useState("");
    const [reviewPosition, setReviewPosition] = useState("approve");
    const [reviewComment, setReviewComment] = useState("");
    const [decisionProposalID, setDecisionProposalID] = useState("");
    const [decisionSummary, setDecisionSummary] = useState("");
    const [decisionRationale, setDecisionRationale] = useState("");
    const [decisionRollback, setDecisionRollback] = useState("");
    const [escalationReason, setEscalationReason] = useState("");
    const [escalationTarget, setEscalationTarget] = useState("iworkercenter");
    const [proposalActionLoading, setProposalActionLoading] = useState<"" | ProposalActionKind>("");
    const [proposalActionError, setProposalActionError] = useState("");
    const [proposalActionStatus, setProposalActionStatus] = useState("");
    const [workflowState, setWorkflowState] = useState<DiscussionWorkflowState | null>(null);
    const [workflowError, setWorkflowError] = useState("");
    const [escalationRoute, setEscalationRoute] = useState<DiscussionEscalationRoute | null>(null);
    const [escalationRouteError, setEscalationRouteError] = useState("");
    const workflowDraft = workflowState?.workflow_action_draft || null;
    const loadWorkflowState = useCallback(async () => {
        if (!id || !detail) return;
        setWorkflowError("");
        setEscalationRouteError("");
        try {
            const state = await GroupDiscussionGetWorkflowState(id);
            setWorkflowState(state);
            setEscalationRoute(state?.escalation_route || null);
        } catch (e) {
            setWorkflowError(String(e));
            setEscalationRoute(null);
        }
    }, [detail, id]);
    useEffect(() => { void loadWorkflowState(); }, [loadWorkflowState]);
    useEffect(() => {
        const first = proposalOptions[0]?.id || "";
        if (!proposalOptions.some((proposal) => proposal.id === reviewProposalID)) setReviewProposalID(first);
        if (!proposalOptions.some((proposal) => proposal.id === decisionProposalID)) setDecisionProposalID(first);
    }, [decisionProposalID, proposalOptions, reviewProposalID]);
    const summarize = useCallback(async () => {
        setSummaryLoading(true);
        setSummaryError("");
        setActionMessage("");
        try {
            setSummary(await GroupDiscussionSummarizeResult({ consultation_id: id, preview: true }));
        } catch (e) {
            setSummaryError(String(e));
        } finally {
            setSummaryLoading(false);
        }
    }, [id]);
    const runSummaryAction = useCallback(async (action: "inject" | "submit") => {
        if (!summary) return;
        const confirmed = await showConfirm(action === "inject" ? c.confirmInject : c.confirmSubmit, c.confirmAction);
        if (!confirmed) return;
        setActionLoading(action);
        setSummaryError("");
        setActionMessage("");
        try {
            const result = await GroupDiscussionSummarizeResult({ consultation_id: id, inject: action === "inject", submit: action === "submit" });
            setSummary(result);
            setActionMessage(action === "inject" ? (result.injected ? c.injected : c.noWrite) : (result.submitted ? c.submitted : c.noWrite));
        } catch (e) {
            setSummaryError(String(e));
        } finally {
            setActionLoading("");
        }
    }, [c.confirmAction, c.confirmInject, c.confirmSubmit, c.injected, c.noWrite, c.submitted, id, showConfirm, summary]);
    const sendDiscussionMessage = useCallback(async () => {
        const content = messageText.trim();
        if (!content || !discussionOpen) return;
        const confirmed = await showConfirm(c.confirmSend, c.confirmAction);
        if (!confirmed) return;
        setMessageLoading(true);
        setMessageError("");
        setMessageStatus("");
        try {
            await GroupDiscussionSendMessage(id, { kind: messageKind, content });
            setMessageText("");
            setMessageStatus(c.messageSent);
            onDetailUpdate(await GroupDiscussionGetConsultationDetail(id));
        } catch (e) {
            setMessageError(String(e));
        } finally {
            setMessageLoading(false);
        }
    }, [c.confirmAction, c.confirmSend, c.messageSent, discussionOpen, id, messageKind, messageText, onDetailUpdate, showConfirm]);
    const loadExperts = useCallback(async () => {
        setExpertsLoading(true);
        setInviteError("");
        setInviteStatus("");
        try {
            const result = await GroupDiscussionListExperts();
            const eligible = filterInviteExperts(Array.isArray(result) ? result : [], detail);
            setExperts(eligible);
            setExpertsLoaded(true);
            if (!eligible.some((item) => item.agent_id === inviteTarget)) setInviteTarget(eligible[0]?.agent_id || "");
        } catch (e) {
            setInviteError(String(e));
        } finally {
            setExpertsLoading(false);
        }
    }, [detail, inviteTarget]);
    const sendInvite = useCallback(async () => {
        if (!discussionOpen || !inviteTarget) return;
        const confirmed = await showConfirm(c.confirmInvite, c.confirmAction);
        if (!confirmed) return;
        setInviteLoading(true);
        setInviteError("");
        setInviteStatus("");
        try {
            await GroupDiscussionSendInvitation(id, { to_id: inviteTarget, role: inviteRole });
            setInviteStatus(c.invited);
            onDetailUpdate(await GroupDiscussionGetConsultationDetail(id));
        } catch (e) {
            setInviteError(String(e));
        } finally {
            setInviteLoading(false);
        }
    }, [c.confirmAction, c.confirmInvite, c.invited, discussionOpen, id, inviteRole, inviteTarget, onDetailUpdate, showConfirm]);
    const sendProposal = useCallback(async () => {
        const title = proposalTitle.trim();
        const content = proposalContent.trim();
        if (!discussionOpen || !title || !content) return;
        const confirmed = await showConfirm(c.confirmProposal, c.confirmAction);
        if (!confirmed) return;
        setProposalActionLoading("proposal");
        setProposalActionError("");
        setProposalActionStatus("");
        try {
            await GroupDiscussionAddProposal(id, { title, content, risks: splitList(proposalRisks) });
            setProposalTitle("");
            setProposalContent("");
            setProposalRisks("");
            setProposalActionStatus(c.proposalSent);
            onDetailUpdate(await GroupDiscussionGetConsultationDetail(id));
        } catch (e) {
            setProposalActionError(String(e));
        } finally {
            setProposalActionLoading("");
        }
    }, [c.confirmAction, c.confirmProposal, c.proposalSent, discussionOpen, id, onDetailUpdate, proposalContent, proposalRisks, proposalTitle, showConfirm]);
    const sendReview = useCallback(async () => {
        if (!discussionOpen || !reviewProposalID) return;
        const confirmed = await showConfirm(c.confirmReview, c.confirmAction);
        if (!confirmed) return;
        setProposalActionLoading("review");
        setProposalActionError("");
        setProposalActionStatus("");
        try {
            await GroupDiscussionAddReview(id, { proposal_id: reviewProposalID, position: reviewPosition, comment: reviewComment.trim() });
            setReviewComment("");
            setProposalActionStatus(c.reviewSent);
            onDetailUpdate(await GroupDiscussionGetConsultationDetail(id));
        } catch (e) {
            setProposalActionError(String(e));
        } finally {
            setProposalActionLoading("");
        }
    }, [c.confirmAction, c.confirmReview, c.reviewSent, discussionOpen, id, onDetailUpdate, reviewComment, reviewPosition, reviewProposalID, showConfirm]);
    const decideProposal = useCallback(async () => {
        if (!discussionOpen || !decisionProposalID) return;
        const confirmed = await showConfirm(c.confirmDecide, c.confirmAction);
        if (!confirmed) return;
        setProposalActionLoading("decide");
        setProposalActionError("");
        setProposalActionStatus("");
        try {
            await GroupDiscussionDecide(id, { proposal_id: decisionProposalID, summary: decisionSummary.trim(), rationale: decisionRationale.trim(), rollback_on: splitList(decisionRollback) });
            setDecisionSummary("");
            setDecisionRationale("");
            setDecisionRollback("");
            setProposalActionStatus(c.decisionRecorded);
            onDetailUpdate(await GroupDiscussionGetConsultationDetail(id));
        } catch (e) {
            setProposalActionError(String(e));
        } finally {
            setProposalActionLoading("");
        }
    }, [c.confirmAction, c.confirmDecide, c.decisionRecorded, decisionProposalID, decisionRationale, decisionRollback, decisionSummary, discussionOpen, id, onDetailUpdate, showConfirm]);
    const escalateDiscussion = useCallback(async () => {
        const reason = escalationReason.trim();
        if (!discussionOpen || !reason) return;
        const confirmed = await showConfirm(c.confirmEscalate, c.confirmAction);
        if (!confirmed) return;
        setProposalActionLoading("escalate");
        setProposalActionError("");
        setProposalActionStatus("");
        try {
            await GroupDiscussionEscalate(id, { reason, target: escalationTarget.trim() });
            setEscalationReason("");
            setProposalActionStatus(c.escalated);
            onDetailUpdate(await GroupDiscussionGetConsultationDetail(id));
        } catch (e) {
            setProposalActionError(String(e));
        } finally {
            setProposalActionLoading("");
        }
    }, [c.confirmAction, c.confirmEscalate, c.escalated, discussionOpen, escalationReason, escalationTarget, id, onDetailUpdate, showConfirm]);
    const applyEscalationRoute = useCallback(() => {
        if (!escalationRoute) return;
        setEscalationReason(String(escalationRoute.reason || "").trim());
        setEscalationTarget(String(escalationRoute.target || "iworkercenter").trim() || "iworkercenter");
    }, [escalationRoute]);
    const applyWorkflowDraft = useCallback(() => {
        if (!workflowDraft) return;
        if (workflowDraft.action_kind === "prepare_escalation") {
            setEscalationReason(String(workflowDraft.escalation_reason || workflowDraft.summary || "").trim());
            setEscalationTarget(String(workflowDraft.escalation_target || "iworkercenter").trim() || "iworkercenter");
        } else if (workflowDraft.action_kind === "record_decision") {
            const args = workflowDraft.suggested_arguments || {};
            setDecisionProposalID(String(workflowDraft.proposal_id || args.proposal_id || "").trim());
            setDecisionSummary(String(args.decision_summary || workflowDraft.title || "").trim());
            setDecisionRationale(String(args.rationale || "").trim());
            const rollback = args.rollback_on;
            setDecisionRollback(Array.isArray(rollback) ? rollback.map((item) => String(item || "").trim()).filter(Boolean).join("\n") : String(rollback || "").trim());
        } else if (workflowDraft.action_kind === "send_followup" || workflowDraft.action_kind === "collect_reviews" || workflowDraft.action_kind === "wait_for_answers") {
            const args = workflowDraft.suggested_arguments || {};
            setMessageKind(String(args.message_kind || "question").trim() || "question");
            setMessageText(String(args.content || workflowDraft.summary || "").trim());
        } else if (workflowDraft.action_kind === "preview_summary") {
            void summarize();
        }
    }, [summarize, workflowDraft]);
    const runStateAction = useCallback(async (action: DiscussionStateAction) => {
        const confirmMessage = action === "pause" ? c.confirmPause : action === "resume" ? c.confirmResume : c.confirmCancel;
        const confirmed = await showConfirm(confirmMessage, c.confirmAction);
        if (!confirmed) return;
        setStateLoading(action);
        setStateError("");
        setStateStatus("");
        try {
            await GroupDiscussionSetState(id, action);
            setStateStatus(action === "pause" ? c.paused : action === "resume" ? c.resumed : c.cancelled);
            onDetailUpdate(await GroupDiscussionGetConsultationDetail(id));
        } catch (e) {
            setStateError(String(e));
        } finally {
            setStateLoading("");
        }
    }, [c.cancelled, c.confirmAction, c.confirmCancel, c.confirmPause, c.confirmResume, c.paused, c.resumed, id, onDetailUpdate, showConfirm]);
    const actionDisabled = loading || summaryLoading || actionLoading !== "";
    const stateControlsVisible = !terminalDiscussion && !hasSubmittedResult;
    const stateDisabled = loading || stateLoading !== "";
    const proposalBusy = proposalActionLoading !== "";
    const proposalActionDisabled = !discussionOpen || proposalBusy;
    const proposalSubmitDisabled = proposalActionDisabled || proposalTitle.trim() === "" || proposalContent.trim() === "";
    const reviewSubmitDisabled = proposalActionDisabled || reviewProposalID === "";
    const decideSubmitDisabled = proposalActionDisabled || decisionProposalID === "";
    const escalateSubmitDisabled = proposalActionDisabled || escalationReason.trim() === "";
    const sendDisabled = !discussionOpen || messageLoading || messageText.trim() === "";
    const inviteDisabled = !discussionOpen || expertsLoading || inviteLoading || inviteTarget === "";
    const expertsSafeHandoff = expertsLoaded ? discussionExpertsSafeHandoff(experts) : null;
    return <div style={overlayStyle} onClick={onClose}>
        <div role="dialog" aria-modal="true" style={modalStyle} onClick={(e) => e.stopPropagation()}>
            <div style={modalHeaderStyle}>
                <div style={{ minWidth: 0 }}><h3 style={modalTitleStyle}>{title}</h3><div style={rowMetaStyle}>{formatDate(discussion.updated_at || discussion.created_at, lang)} {"\u00b7"} ID: {id}</div></div>
                <div style={{ display: "flex", gap: 8, flexShrink: 0, flexWrap: "wrap", justifyContent: "flex-end" }}><button type="button" onClick={() => void summarize()} disabled={loading || summaryLoading} style={{ ...primaryButtonStyle, opacity: loading || summaryLoading ? 0.62 : 1, cursor: loading || summaryLoading ? "default" : "pointer" }}>{summaryLoading ? c.summarizing : c.summarize}</button>{summary && <button type="button" onClick={() => void runSummaryAction("inject")} disabled={actionDisabled} style={{ ...primaryButtonStyle, opacity: actionDisabled ? 0.62 : 1, cursor: actionDisabled ? "default" : "pointer" }}>{actionLoading === "inject" ? c.injecting : c.inject}</button>}{summary && !hasSubmittedResult && <button type="button" onClick={() => void runSummaryAction("submit")} disabled={actionDisabled} style={{ ...primaryButtonStyle, opacity: actionDisabled ? 0.62 : 1, cursor: actionDisabled ? "default" : "pointer" }}>{actionLoading === "submit" ? c.submitting : c.submit}</button>}{onExperience && <button type="button" onClick={() => onExperience(id)} style={primaryButtonStyle}>{c.experience}</button>}<button type="button" onClick={onClose} style={buttonStyle}>{c.close}</button></div>
            </div>
            <div style={modalBodyStyle}>
                {loading && <div style={emptyStyle}>{c.loading}</div>}
                {summaryError && <div role="alert" style={errorStyle}>{summaryError}</div>}
                {actionMessage && <div role="status" style={successStyle}>{actionMessage}</div>}
                {messageError && <div role="alert" style={errorStyle}>{messageError}</div>}
                {messageStatus && <div role="status" style={successStyle}>{messageStatus}</div>}
                {inviteError && <div role="alert" style={errorStyle}>{inviteError}</div>}
                {inviteStatus && <div role="status" style={successStyle}>{inviteStatus}</div>}
                {stateError && <div role="alert" style={errorStyle}>{stateError}</div>}
                {stateStatus && <div role="status" style={successStyle}>{stateStatus}</div>}
                {proposalActionError && <div role="alert" style={errorStyle}>{proposalActionError}</div>}
                {proposalActionStatus && <div role="status" style={successStyle}>{proposalActionStatus}</div>}
                {!loading && detail && <>
                    {stateControlsVisible && <StateControls c={c} loading={stateLoading} disabled={stateDisabled} onAction={runStateAction} />}
                    <WorkflowStatePanel c={c} lang={lang} state={workflowState} error={workflowError} />
                    <EscalationRoutePanel c={c} lang={lang} route={escalationRoute} error={escalationRouteError} onApply={discussionOpen && escalationRoute?.suggested ? applyEscalationRoute : undefined} />
                    <RollbackReadinessPanel lang={lang} readiness={workflowState?.rollback_readiness || null} />
                    <WorkflowActionDraftPanel lang={lang} draft={workflowDraft} onApply={discussionOpen ? applyWorkflowDraft : undefined} />
                    {discussionOpen ? <>
                        <ProposalWorkflowPanel c={c} proposals={proposalOptions} title={proposalTitle} content={proposalContent} risks={proposalRisks} reviewProposalID={reviewProposalID} reviewPosition={reviewPosition} reviewComment={reviewComment} decisionProposalID={decisionProposalID} decisionSummary={decisionSummary} decisionRationale={decisionRationale} decisionRollback={decisionRollback} escalationReason={escalationReason} escalationTarget={escalationTarget} loading={proposalActionLoading} proposalDisabled={proposalSubmitDisabled} reviewDisabled={reviewSubmitDisabled} decideDisabled={decideSubmitDisabled} escalateDisabled={escalateSubmitDisabled} onTitleChange={setProposalTitle} onContentChange={setProposalContent} onRisksChange={setProposalRisks} onReviewProposalChange={setReviewProposalID} onReviewPositionChange={setReviewPosition} onReviewCommentChange={setReviewComment} onDecisionProposalChange={setDecisionProposalID} onDecisionSummaryChange={setDecisionSummary} onDecisionRationaleChange={setDecisionRationale} onDecisionRollbackChange={setDecisionRollback} onEscalationReasonChange={setEscalationReason} onEscalationTargetChange={setEscalationTarget} onProposal={sendProposal} onReview={sendReview} onDecide={decideProposal} onEscalate={escalateDiscussion} />
                        <MessageComposer c={c} kind={messageKind} text={messageText} loading={messageLoading} disabled={sendDisabled} onKindChange={setMessageKind} onTextChange={setMessageText} onSend={sendDiscussionMessage} />
                        <InvitePanel c={c} lang={lang} experts={experts} loaded={expertsLoaded} target={inviteTarget} role={inviteRole} loadingExperts={expertsLoading} inviting={inviteLoading} disabled={inviteDisabled} safeHandoff={expertsSafeHandoff} onLoad={loadExperts} onTargetChange={setInviteTarget} onRoleChange={setInviteRole} onInvite={sendInvite} />
                    </> : <div style={emptyStyle}>{c.closedDiscussion}</div>}
                    {summary && <>
                        <DetailBlock title={c.summaryPreview}>{formatSummaryPreview(c, summary)}</DetailBlock>
                        <DiscussionSafeHandoffBlock lang={lang} focusContext={summary.recommended_focus_context} recommendedToolCall={summary.recommended_tool_call} boundary={summary.non_executing_boundary} />
                    </>}
                    {discussion.question && <DetailBlock title={c.question}>{discussion.question}</DetailBlock>}
                    {(discussion.result_summary || detail.decision?.summary) && <DetailBlock title={c.result}>{discussion.result_summary || detail.decision?.summary || ""}</DetailBlock>}
                    {detail.decision && <DetailBlock title={c.decision}>{[detail.decision.rationale, listLine(c.decidedBy, detail.decision.decided_by), listLine(c.rollback, detail.decision.rollback_on)].filter(Boolean).join("\n")}</DetailBlock>}
                    {detail.session?.escalation && <DetailBlock title={c.escalation}>{[detail.session.escalation.reason, listLine(c.agent, [detail.session.escalation.raised_by || ""].filter(Boolean)), listLine(c.escalationTarget, [detail.session.escalation.target || ""].filter(Boolean))].filter(Boolean).join("\n")}</DetailBlock>}
                    {Array.isArray(detail.proposals) && detail.proposals.length > 0 && <ProposalDetailPanel c={c} proposals={detail.proposals} reviews={detail.reviews} summaries={detail.review_summaries} />}
                    {Array.isArray(detail.reviews) && detail.reviews.length > 0 && <DetailBlock title={c.reviews}>{detail.reviews.map((r) => (r.reviewer_id || c.agent) + (r.position ? " [" + r.position + "]" : "") + ": " + (r.comment || "")).join("\n")}</DetailBlock>}
                    <DetailBlock title={c.messages}>{(detail.messages || []).map((m) => formatDate(m.created_at, lang) + " " + (m.from_id || c.agent) + (m.kind ? " [" + m.kind + "]" : "") + "\n" + (m.content || "") + (m.evidence?.length ? "\nEvidence: " + m.evidence.join(", ") : "")).join("\n\n") || "-"}</DetailBlock>
                </>}
            </div>
        </div>
    </div>;
}

function WorkflowStatePanel({ c, lang, state, error }: { c: typeof copy.en; lang: string; state: DiscussionWorkflowState | null; error: string }) {
    if (!state && !error) return null;
    const proposals = Array.isArray(state?.proposals) ? state.proposals : [];
    const missingAnswers = Array.isArray(state?.missing_answer_participants) ? state.missing_answer_participants.filter(Boolean) : [];
    const blockers = Array.isArray(state?.workflow_blockers) ? state.workflow_blockers.filter((item) => item && (item.code || item.message)) : [];
    const ready = Boolean(state?.readiness?.ready);
    const next = [state?.suggested_next_action_kind, state?.suggested_next_action].filter(Boolean).join(": ");
    return <section style={composerStyle}>
        <div style={composerHeaderStyle}><h4 style={detailTitleStyle}>{localText(lang, "Workflow State", "\u534f\u4f5c\u72b6\u6001", "\u5354\u4f5c\u72c0\u614b")}</h4><span style={ready ? readyBadgeStyle : waitingBadgeStyle}>{ready ? c.ready : c.waiting}</span></div>
        {error && <div role="alert" style={errorStyle}>{error}</div>}
        {state && <div style={workflowGridStyle}>
            <WorkflowMetric label={localText(lang, "Messages", "\u6d88\u606f", "\u8a0a\u606f")} value={state.message_count || 0} />
            <WorkflowMetric label={c.proposals} value={state.proposal_count || 0} />
            <WorkflowMetric label={c.reviews} value={state.review_count || 0} />
            <WorkflowMetric label={localText(lang, "Open", "\u5f00\u653e", "\u958b\u653e")} value={state.open_proposal_count || 0} />
            <WorkflowMetric label={localText(lang, "Decidable", "\u53ef\u51b3\u7b56", "\u53ef\u6c7a\u7b56")} value={state.decidable_proposal_count || 0} />
            <WorkflowMetric label={localText(lang, "Blocking", "\u963b\u585e", "\u963b\u585e")} value={state.blocking_review_count || 0} />
        </div>}
        {state?.readiness?.reason && <div style={workflowTextStyle}>{state.readiness.reason}</div>}
        {next && <div style={workflowTextStyle}>{localText(lang, "Suggested next action", "\u5efa\u8bae\u540e\u7eed\u52a8\u4f5c", "\u5efa\u8b70\u5f8c\u7e8c\u52d5\u4f5c")}: {next}</div>}
        <DiscussionSafeHandoffBlock lang={lang} focusContext={state?.recommended_focus_context} recommendedToolCall={state?.recommended_tool_call} boundary={state?.non_executing_boundary} />
        {missingAnswers.length > 0 && <div style={workflowTextStyle}>{localText(lang, "Missing answers", "\u7f3a\u5c11\u7b54\u590d", "\u7f3a\u5c11\u7b54\u8986")}: {missingAnswers.join(", ")}</div>}
        {blockers.length > 0 && <div style={workflowBlockerListStyle}>{blockers.slice(0, 4).map((blocker, index) => <WorkflowBlockerRow key={(blocker.code || "blocker") + index} lang={lang} blocker={blocker} />)}</div>}
        {proposals.length > 0 && <div style={workflowProposalListStyle}>{proposals.slice(0, 3).map((proposal) => {
            const summary = proposal.review_summary || {};
            const missingReviewers = Array.isArray(proposal.missing_reviewers) ? proposal.missing_reviewers.filter(Boolean) : [];
            const proposalBlockers = Array.isArray(proposal.blockers) ? proposal.blockers.filter((item) => item && item.code) : [];
            return <div key={proposal.id || proposal.title} style={workflowProposalStyle}>
                <span style={proposal.policy_satisfied ? readyBadgeStyle : proposal.blocking_reviews ? waitingBadgeStyle : reviewMutedBadgeStyle}>{proposal.policy_satisfied ? c.ready : proposal.blocking_reviews ? localText(lang, "Blocked", "\u6709\u963b\u585e", "\u6709\u963b\u585e") : (proposal.status || c.waiting)}</span>
                <span style={workflowProposalTitleStyle}>{proposal.title || proposal.id || c.proposal}</span>
                <span style={workflowTextStyle}>{c.approvals}: {summary.approvals || 0} {"\u00b7"} {c.concerns}: {summary.concerns || 0} {"\u00b7"} {c.rejections}: {summary.rejections || 0}</span>
                {missingReviewers.length > 0 && <span style={workflowTextStyle}>{localText(lang, "Missing reviewers", "\u7f3a\u5c11\u8bc4\u5ba1\u8005", "\u7f3a\u5c11\u8a55\u5be9\u8005")}: {missingReviewers.join(", ")}</span>}
                {proposalBlockers.length > 0 && <span style={workflowTextStyle}>{localText(lang, "Blockers", "\u963b\u585e\u9879", "\u963b\u585e\u9805")}: {proposalBlockers.map((item) => item.code).join(", ")}</span>}
            </div>;
        })}</div>}
    </section>;
}
function WorkflowMetric({ label, value }: { label: string; value: number }) { return <div style={workflowMetricStyle}><span>{label}</span><strong>{value}</strong></div>; }

function discussionSafeJSONString(value: unknown): string {
    if (!value) return "";
    try {
        return JSON.stringify(value, null, 2);
    } catch {
        return String(value);
    }
}

function discussionSafeHandoffText(focusContext?: Record<string, unknown> | null, recommendedToolCall?: DiscussionToolCallSuggestion | null, boundary?: string): string {
    return [
        focusContext ? "recommended_focus_context:\n" + discussionSafeJSONString(focusContext) : "",
        recommendedToolCall ? "recommended_tool_call:\n" + discussionSafeJSONString(recommendedToolCall) : "",
        boundary ? "non_executing_boundary:\n" + boundary : "",
    ].filter(Boolean).join("\n\n");
}

function DiscussionSafeHandoffBlock({ lang, focusContext, recommendedToolCall, boundary }: { lang: string; focusContext?: Record<string, unknown> | null; recommendedToolCall?: DiscussionToolCallSuggestion | null; boundary?: string }) {
    const [copied, setCopied] = useState(false);
    const text = discussionSafeHandoffText(focusContext, recommendedToolCall, boundary);
    if (!text) return null;
    const copyHandoff = async () => {
        if (!navigator.clipboard?.writeText) return;
        try {
            await navigator.clipboard.writeText(text);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1200);
        } catch {
            setCopied(false);
        }
    };
    return <section style={detailBlockStyle}>
        <div style={composerHeaderStyle}>
            <h4 style={detailTitleStyle}>{localText(lang, "Safe Handoff", "\u5b89\u5168\u4ea4\u63a5", "\u5b89\u5168\u4ea4\u63a5")}</h4>
            <button type="button" onClick={copyHandoff} style={buttonStyle}>{copied ? localText(lang, "Copied", "\u5df2\u590d\u5236", "\u5df2\u8907\u88fd") : localText(lang, "Copy", "\u590d\u5236", "\u8907\u88fd")}</button>
        </div>
        <pre style={preStyle}>{text}</pre>
    </section>;
}

function WorkflowBlockerRow({ lang, blocker }: { lang: string; blocker: DiscussionWorkflowBlocker }) {
    const targets = [Array.isArray(blocker.proposal_ids) && blocker.proposal_ids.length > 0 ? blocker.proposal_ids.join(", ") : blocker.proposal_id, Array.isArray(blocker.participants) && blocker.participants.length > 0 ? blocker.participants.join(", ") : "", blocker.count ? String(blocker.count) : ""].filter(Boolean).join(" \u00b7 ");
    return <div style={workflowBlockerStyle}>
        <span style={workflowBlockerBadgeStyle}>{blocker.severity || blocker.code || localText(lang, "Blocker", "\u963b\u585e", "\u963b\u585e")}</span>
        <span style={workflowBlockerTextStyle}>{blocker.message || blocker.code}</span>
        {targets && <span style={workflowBlockerMetaStyle}>{targets}</span>}
    </div>;
}
function EscalationRoutePanel({ c, lang, route, error, onApply }: { c: typeof copy.en; lang: string; route: DiscussionEscalationRoute | null; error: string; onApply?: () => void }) {
    const [copied, setCopied] = useState(false);
    if (!route && !error) return null;
    const suggested = Boolean(route?.suggested);
    const triggers = Array.isArray(route?.triggers) ? route.triggers.filter(Boolean) : [];
    const policyEvidence = Array.isArray(route?.policy_evidence) ? route.policy_evidence.filter(Boolean) : [];
    const handoffText = discussionSafeHandoffText(route?.recommended_focus_context, route?.recommended_tool_call, route?.non_executing_boundary);
    const copyText = [
        route?.target ? c.escalationTarget + ": " + route.target : "",
        route?.reason,
        triggers.length > 0 ? localText(lang, "Triggers", "\u89e6\u53d1", "\u89f8\u767c") + ":\n" + triggers.map((item) => "- " + item).join("\n") : "",
        policyEvidence.length > 0 ? localText(lang, "Policy Evidence", "\u7b56\u7565\u8bc1\u636e", "\u7b56\u7565\u8b49\u64da") + ":\n" + policyEvidence.map((item) => "- " + item).join("\n") : "",
        handoffText,
    ].filter(Boolean).join("\n\n");
    const copyRoute = async () => {
        if (!copyText || !navigator.clipboard?.writeText) return;
        try {
            await navigator.clipboard.writeText(copyText);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1200);
        } catch {
            setCopied(false);
        }
    };
    return <section style={composerStyle}>
        <div style={composerHeaderStyle}>
            <h4 style={detailTitleStyle}>{localText(lang, "Escalation Route", "\u5347\u7ea7\u8def\u7531", "\u5347\u7d1a\u8def\u7531")}</h4>
            {route && <span style={suggested ? waitingBadgeStyle : readyBadgeStyle}>{suggested ? localText(lang, "Suggested", "\u5efa\u8bae\u5347\u7ea7", "\u5efa\u8b70\u5347\u7d1a") : localText(lang, "No Escalation", "\u6682\u4e0d\u5347\u7ea7", "\u66ab\u4e0d\u5347\u7d1a")}</span>}
        </div>
        {error && <div role="alert" style={errorStyle}>{error}</div>}
        {route && <>
            <div style={workflowGridStyle}>
                <WorkflowMetric label={localText(lang, "Blocking", "\u963b\u585e", "\u963b\u585e")} value={route.blocking_review_count || 0} />
                <WorkflowMetric label={localText(lang, "Decidable", "\u53ef\u51b3\u7b56", "\u53ef\u6c7a\u7b56")} value={route.decidable_proposal_count || 0} />
            </div>
            {route.target && <div style={workflowTextStyle}>{c.escalationTarget}: {route.target}</div>}
            {route.reason && <div style={workflowTextStyle}>{route.reason}</div>}
            {triggers.length > 0 && <div style={reviewSummaryBadgeRowStyle}>{triggers.map((trigger) => <span key={trigger} style={{ ...reviewCountBadgeStyle, ...reviewMutedBadgeStyle }}>{trigger}</span>)}</div>}
            {policyEvidence.length > 0 && <div style={workflowTextStyle}>{localText(lang, "Policy Evidence", "\u7b56\u7565\u8bc1\u636e", "\u7b56\u7565\u8b49\u64da") + ":\n" + policyEvidence.map((item) => "- " + item).join("\n")}</div>}
            <DiscussionSafeHandoffBlock lang={lang} focusContext={route.recommended_focus_context} recommendedToolCall={route.recommended_tool_call} boundary={route.non_executing_boundary} />
            {(onApply && route.reason) || copyText ? <div style={composerFooterStyle}>
                {onApply && route.reason && <button type="button" onClick={() => void onApply()} style={buttonStyle}>{localText(lang, "Use Suggestion", "\u5957\u7528\u5efa\u8bae", "\u5957\u7528\u5efa\u8b70")}</button>}
                {copyText && <button type="button" onClick={copyRoute} style={buttonStyle}>{copied ? localText(lang, "Copied", "\u5df2\u590d\u5236", "\u5df2\u8907\u88fd") : localText(lang, "Copy Route", "\u590d\u5236\u8def\u7531", "\u8907\u88fd\u8def\u7531")}</button>}
            </div> : null}
        </>}
    </section>;
}
function RollbackReadinessPanel({ lang, readiness }: { lang: string; readiness: DiscussionRollbackReadiness | null }) {
    const [copied, setCopied] = useState(false);
    if (!readiness?.has_decision || !Array.isArray(readiness.rollback_on) || readiness.rollback_on.length === 0) return null;
    const rollback = readiness.rollback_on.filter(Boolean);
    const matched = Array.isArray(readiness.matched_triggers) ? readiness.matched_triggers.filter(Boolean) : [];
    const unmatched = Array.isArray(readiness.unmatched_triggers) ? readiness.unmatched_triggers.filter(Boolean) : [];
    const evidence = Array.isArray(readiness.evidence) ? readiness.evidence.filter(Boolean) : [];
    const suggested = Boolean(readiness.suggested);
    const handoffText = discussionSafeHandoffText(readiness.recommended_focus_context, readiness.recommended_tool_call, readiness.non_executing_boundary);
    const copyText = [
        readiness.decision_summary,
        readiness.suggested_next_action_kind,
        readiness.suggested_next_action,
        rollback.length > 0 ? localText(lang, "Rollback triggers", "\u56de\u6eda\u89e6\u53d1\u6761\u4ef6", "\u56de\u6efe\u89f8\u767c\u689d\u4ef6") + ":\n" + rollback.map((item) => "- " + item).join("\n") : "",
        matched.length > 0 ? localText(lang, "Matched", "\u5df2\u547d\u4e2d", "\u5df2\u547d\u4e2d") + ":\n" + matched.map((item) => "- " + item).join("\n") : "",
        unmatched.length > 0 ? localText(lang, "Unmatched", "\u672a\u547d\u4e2d", "\u672a\u547d\u4e2d") + ":\n" + unmatched.map((item) => "- " + item).join("\n") : "",
        evidence.length > 0 ? localText(lang, "Evidence", "\u8bc1\u636e", "\u8b49\u64da") + ":\n" + evidence.map((item) => "- " + item).join("\n") : "",
        handoffText,
    ].filter(Boolean).join("\n\n");
    const copyReadiness = async () => {
        try {
            await navigator.clipboard?.writeText(copyText);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1200);
        } catch {
            setCopied(false);
        }
    };
    return <section style={composerStyle}>
        <div style={composerHeaderStyle}>
            <h4 style={detailTitleStyle}>{localText(lang, "Rollback Readiness", "\u56de\u6eda\u5c31\u7eea\u5ea6", "\u56de\u6efe\u5c31\u7dd2\u5ea6")}</h4>
            <span style={suggested ? waitingBadgeStyle : readyBadgeStyle}>{suggested ? localText(lang, "Review Needed", "\u9700\u590d\u6838", "\u9700\u8907\u6838") : localText(lang, "Monitoring", "\u76d1\u6d4b\u4e2d", "\u76e3\u6e2c\u4e2d")}</span>
        </div>
        <div style={workflowGridStyle}>
            <WorkflowMetric label={localText(lang, "Triggers", "\u89e6\u53d1\u6761\u4ef6", "\u89f8\u767c\u689d\u4ef6")} value={rollback.length} />
            <WorkflowMetric label={localText(lang, "Matched", "\u547d\u4e2d", "\u547d\u4e2d")} value={matched.length} />
            <WorkflowMetric label={localText(lang, "Unmatched", "\u672a\u547d\u4e2d", "\u672a\u547d\u4e2d")} value={unmatched.length} />
        </div>
        {readiness.decision_summary && <div style={workflowProposalTitleStyle}>{readiness.decision_summary}</div>}
        {readiness.suggested_next_action && <div style={workflowTextStyle}>{readiness.suggested_next_action}</div>}
        {matched.length > 0 && <div style={workflowTextStyle}>{localText(lang, "Matched triggers", "\u5df2\u547d\u4e2d\u89e6\u53d1", "\u5df2\u547d\u4e2d\u89f8\u767c") + ":\n" + matched.map((item) => "- " + item).join("\n")}</div>}
        {unmatched.length > 0 && <div style={workflowTextStyle}>{localText(lang, "Unmatched triggers", "\u672a\u547d\u4e2d\u89e6\u53d1", "\u672a\u547d\u4e2d\u89f8\u767c") + ":\n" + unmatched.map((item) => "- " + item).join("\n")}</div>}
        <DiscussionSafeHandoffBlock lang={lang} focusContext={readiness.recommended_focus_context} recommendedToolCall={readiness.recommended_tool_call} boundary={readiness.non_executing_boundary} />
        {copyText && <div style={composerFooterStyle}><button type="button" onClick={copyReadiness} style={buttonStyle}>{copied ? localText(lang, "Copied", "\u5df2\u590d\u5236", "\u5df2\u8907\u88fd") : localText(lang, "Copy Readiness", "\u590d\u5236\u5c31\u7eea\u5ea6", "\u8907\u88fd\u5c31\u7dd2\u5ea6")}</button></div>}
    </section>;
}
function WorkflowActionDraftPanel({ lang, draft, onApply }: { lang: string; draft: DiscussionWorkflowActionDraft | null; onApply?: () => void }) {
    const [copied, setCopied] = useState(false);
    if (!draft) return null;
    const checklist = Array.isArray(draft.checklist) ? draft.checklist.filter(Boolean) : [];
    const evidence = Array.isArray(draft.evidence) ? draft.evidence.filter(Boolean) : [];
    const boundaries = Array.isArray(draft.risk_boundaries) ? draft.risk_boundaries.filter(Boolean) : [];
    const targetParticipants = Array.isArray(draft.target_participants) ? draft.target_participants.filter(Boolean) : [];
    const targetProposalIDs = Array.isArray(draft.target_proposal_ids) ? draft.target_proposal_ids.filter(Boolean) : [];
    const rawArgs = draft.suggested_arguments || {};
    const args = draft.suggested_arguments ? JSON.stringify(draft.suggested_arguments) : "";
    const handoffText = discussionSafeHandoffText(draft.recommended_focus_context, draft.recommended_tool_call, draft.non_executing_boundary);
    const draftAction = String(rawArgs.action || "");
    const canApply = Boolean(onApply && (draft.action_kind === "prepare_escalation" || draft.action_kind === "record_decision" || draft.action_kind === "send_followup" || draft.action_kind === "collect_reviews" || draft.action_kind === "preview_summary" || (draft.action_kind === "wait_for_answers" && draftAction === "send_message")));
    const draftText = [
        draft.title,
        draft.summary,
        targetParticipants.length > 0 ? localText(lang, "Target participants", "\u76ee\u6807\u53c2\u4e0e\u8005", "\u76ee\u6a19\u53c3\u8207\u8005") + ": " + targetParticipants.join(", ") : "",
        targetProposalIDs.length > 0 ? localText(lang, "Target proposals", "\u76ee\u6807\u63d0\u6848", "\u76ee\u6a19\u63d0\u6848") + ": " + targetProposalIDs.join(", ") : "",
        evidence.length > 0 ? localText(lang, "Evidence", "\u8bc1\u636e", "\u8b49\u64da") + ":\n" + evidence.map((item) => "- " + item).join("\n") : "",
        boundaries.length > 0 ? localText(lang, "Boundaries", "\u8fb9\u754c", "\u908a\u754c") + ":\n" + boundaries.map((item) => "- " + item).join("\n") : "",
        checklist.length > 0 ? checklist.map((item) => "- " + item).join("\n") : "",
        args,
        handoffText,
    ].filter(Boolean).join("\n\n");
    const copyDraft = async () => {
        if (!draftText || !navigator.clipboard?.writeText) return;
        try {
            await navigator.clipboard.writeText(draftText);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1400);
        } catch {
            setCopied(false);
        }
    };
    return <section style={composerStyle}>
        <div style={composerHeaderStyle}>
            <h4 style={detailTitleStyle}>{localText(lang, "Workflow Draft", "\u5de5\u4f5c\u6d41\u8349\u7a3f", "\u5de5\u4f5c\u6d41\u8349\u7a3f")}</h4>
            <span style={draft.requires_confirmation ? waitingBadgeStyle : readyBadgeStyle}>{draft.action_kind || localText(lang, "draft", "\u8349\u7a3f", "\u8349\u7a3f")}</span>
        </div>
        {draft.title && <div style={workflowProposalTitleStyle}>{draft.title}</div>}
        {draft.summary && <div style={workflowTextStyle}>{draft.summary}</div>}
        {targetParticipants.length > 0 && <div style={workflowTextStyle}>{localText(lang, "Target participants", "\u76ee\u6807\u53c2\u4e0e\u8005", "\u76ee\u6a19\u53c3\u8207\u8005")}: {targetParticipants.join(", ")}</div>}
        {targetProposalIDs.length > 0 && <div style={workflowTextStyle}>{localText(lang, "Target proposals", "\u76ee\u6807\u63d0\u6848", "\u76ee\u6a19\u63d0\u6848")}: {targetProposalIDs.join(", ")}</div>}
        {evidence.length > 0 && <div style={workflowTextStyle}>{localText(lang, "Evidence", "\u8bc1\u636e", "\u8b49\u64da") + ":\n" + evidence.map((item) => "- " + item).join("\n")}</div>}
        {boundaries.length > 0 && <div style={workflowTextStyle}>{localText(lang, "Boundaries", "\u8fb9\u754c", "\u908a\u754c") + ":\n" + boundaries.map((item) => "- " + item).join("\n")}</div>}
        {checklist.length > 0 && <div style={workflowTextStyle}>{checklist.map((item) => "- " + item).join("\n")}</div>}
        {args && <div style={proposalDetailMetaStyle}>{args}</div>}
        <DiscussionSafeHandoffBlock lang={lang} focusContext={draft.recommended_focus_context} recommendedToolCall={draft.recommended_tool_call} boundary={draft.non_executing_boundary} />
        {(draftText || canApply) && <div style={composerFooterStyle}>
            {draftText && <button type="button" onClick={copyDraft} style={buttonStyle}>{copied ? localText(lang, "Copied", "\u5df2\u590d\u5236", "\u5df2\u8907\u88fd") : localText(lang, "Copy Draft", "\u590d\u5236\u8349\u7a3f", "\u8907\u88fd\u8349\u7a3f")}</button>}
            {canApply && <button type="button" onClick={() => void onApply?.()} style={buttonStyle}>{localText(lang, "Use Draft", "\u5957\u7528\u8349\u7a3f", "\u5957\u7528\u8349\u7a3f")}</button>}
        </div>}
    </section>;
}
function DetailBlock({ title, children }: { title: string; children: string }) { return <section style={detailBlockStyle}><h4 style={detailTitleStyle}>{title}</h4><pre style={preStyle}>{children}</pre></section>; }
function ProposalDetailPanel({ c, proposals, reviews, summaries }: { c: typeof copy.en; proposals: DiscussionProposal[]; reviews?: DiscussionReview[]; summaries?: Record<string, DiscussionReviewSummary> }) {
    return <section style={proposalDetailSectionStyle}>
        <h4 style={detailTitleStyle}>{c.proposals}</h4>
        <div style={proposalDetailListStyle}>{proposals.map((proposal) => {
            const summary = reviewSummaryView(proposal.id, reviews, summaries);
            return <article key={proposal.id || proposal.title || proposal.content} style={proposalDetailItemStyle}>
                <div style={proposalDetailHeaderStyle}>
                    <div style={proposalDetailTitleStyle}>{proposal.title || proposal.id || c.proposal}</div>
                    {proposal.status && <span style={waitingBadgeStyle}>{proposal.status}</span>}
                </div>
                {proposal.content && <div style={proposalDetailBodyStyle}>{proposal.content}</div>}
                {proposal.risks?.length ? <div style={proposalDetailMetaStyle}>{c.risks}: {proposal.risks.join(", ")}</div> : null}
                {summary && <div style={reviewSummaryBoxStyle} aria-label={c.reviewSummary}>
                    <div style={reviewSummaryBadgeRowStyle}>
                        <ReviewCountBadge label={c.approvals} value={summary.counts.approve} tone="success" />
                        <ReviewCountBadge label={c.concerns} value={summary.counts.concern} tone="warning" />
                        <ReviewCountBadge label={c.rejections} value={summary.counts.reject} tone="danger" />
                        <ReviewCountBadge label={c.abstains} value={summary.counts.abstain} tone="muted" />
                    </div>
                    {summary.reviewedBy.length > 0 && <div style={proposalDetailMetaStyle}>{c.agent}: {summary.reviewedBy.join(", ")}</div>}
                </div>}
            </article>;
        })}</div>
    </section>;
}
function ReviewCountBadge({ label, value, tone }: { label: string; value: number; tone: "success" | "warning" | "danger" | "muted" }) {
    const toneStyle = tone === "success" ? reviewSuccessBadgeStyle : tone === "warning" ? reviewWarningBadgeStyle : tone === "danger" ? reviewDangerBadgeStyle : reviewMutedBadgeStyle;
    return <span style={{ ...reviewCountBadgeStyle, ...toneStyle }}><strong>{value}</strong> {label}</span>;
}
function StateControls({ c, loading, disabled, onAction }: { c: typeof copy.en; loading: "" | DiscussionStateAction; disabled: boolean; onAction: (action: DiscussionStateAction) => void }) {
    const control = (action: DiscussionStateAction, label: string, busyLabel: string, style: CSSProperties = buttonStyle) => {
        const busy = loading === action;
        return <button type="button" onClick={() => void onAction(action)} disabled={disabled} style={{ ...style, opacity: disabled ? 0.62 : 1, cursor: disabled ? "default" : "pointer" }}>{busy ? busyLabel : label}</button>;
    };
    return <section style={composerStyle}>
        <div style={composerHeaderStyle}><h4 style={detailTitleStyle}>{c.stateControls}</h4><div style={stateControlActionsStyle}>{control("pause", c.pause, c.pausing)}{control("resume", c.resume, c.resuming)}{control("cancel", c.cancel, c.cancelling, dangerButtonStyle)}</div></div>
    </section>;
}
function ProposalWorkflowPanel({ c, proposals, title, content, risks, reviewProposalID, reviewPosition, reviewComment, decisionProposalID, decisionSummary, decisionRationale, decisionRollback, escalationReason, escalationTarget, loading, proposalDisabled, reviewDisabled, decideDisabled, escalateDisabled, onTitleChange, onContentChange, onRisksChange, onReviewProposalChange, onReviewPositionChange, onReviewCommentChange, onDecisionProposalChange, onDecisionSummaryChange, onDecisionRationaleChange, onDecisionRollbackChange, onEscalationReasonChange, onEscalationTargetChange, onProposal, onReview, onDecide, onEscalate }: { c: typeof copy.en; proposals: DiscussionProposal[]; title: string; content: string; risks: string; reviewProposalID: string; reviewPosition: string; reviewComment: string; decisionProposalID: string; decisionSummary: string; decisionRationale: string; decisionRollback: string; escalationReason: string; escalationTarget: string; loading: "" | ProposalActionKind; proposalDisabled: boolean; reviewDisabled: boolean; decideDisabled: boolean; escalateDisabled: boolean; onTitleChange: (value: string) => void; onContentChange: (value: string) => void; onRisksChange: (value: string) => void; onReviewProposalChange: (value: string) => void; onReviewPositionChange: (value: string) => void; onReviewCommentChange: (value: string) => void; onDecisionProposalChange: (value: string) => void; onDecisionSummaryChange: (value: string) => void; onDecisionRationaleChange: (value: string) => void; onDecisionRollbackChange: (value: string) => void; onEscalationReasonChange: (value: string) => void; onEscalationTargetChange: (value: string) => void; onProposal: () => void; onReview: () => void; onDecide: () => void; onEscalate: () => void }) {
    return <section style={composerStyle}>
        <div style={composerHeaderStyle}><h4 style={detailTitleStyle}>{c.proposalActions}</h4></div>
        <div style={proposalFormGridStyle}>
            <input value={title} onChange={(e) => onTitleChange(e.target.value)} placeholder={c.proposalTitlePlaceholder} disabled={loading !== ""} style={inputStyle} aria-label={c.proposalTitle} />
            <input value={risks} onChange={(e) => onRisksChange(e.target.value)} placeholder={c.proposalRisksPlaceholder} disabled={loading !== ""} style={inputStyle} aria-label={c.proposalRisks} />
        </div>
        <textarea value={content} onChange={(e) => onContentChange(e.target.value)} placeholder={c.proposalPlaceholder} rows={3} disabled={loading !== ""} style={textareaStyle} aria-label={c.proposalContent} />
        <div style={composerFooterStyle}><button type="button" onClick={() => void onProposal()} disabled={proposalDisabled} style={{ ...primaryButtonStyle, opacity: proposalDisabled ? 0.62 : 1, cursor: proposalDisabled ? "default" : "pointer" }}>{loading === "proposal" ? c.sendingProposal : c.sendProposal}</button></div>
        {proposals.length === 0 ? <div style={emptyStyle}>{c.noOpenProposals}</div> : <>
            <div style={proposalFormGridStyle}>
                <select value={reviewProposalID} onChange={(e) => onReviewProposalChange(e.target.value)} disabled={loading !== ""} style={selectStyle} aria-label={c.proposalSelect}>{proposals.map((proposal) => <option key={proposal.id} value={proposal.id || ""}>{proposalOptionLabel(c, proposal)}</option>)}</select>
                <select value={reviewPosition} onChange={(e) => onReviewPositionChange(e.target.value)} disabled={loading !== ""} style={selectStyle} aria-label={c.reviewPosition}><option value="approve">{c.approve}</option><option value="concern">{c.concern}</option><option value="reject">{c.reject}</option><option value="abstain">{c.abstain}</option></select>
                <input value={reviewComment} onChange={(e) => onReviewCommentChange(e.target.value)} placeholder={c.reviewPlaceholder} disabled={loading !== ""} style={inputStyle} aria-label={c.reviewComment} />
                <button type="button" onClick={() => void onReview()} disabled={reviewDisabled} style={{ ...primaryButtonStyle, opacity: reviewDisabled ? 0.62 : 1, cursor: reviewDisabled ? "default" : "pointer" }}>{loading === "review" ? c.sendingReview : c.sendReview}</button>
            </div>
            <div style={proposalFormGridStyle}>
                <select value={decisionProposalID} onChange={(e) => onDecisionProposalChange(e.target.value)} disabled={loading !== ""} style={selectStyle} aria-label={c.decideProposal}>{proposals.map((proposal) => <option key={proposal.id} value={proposal.id || ""}>{proposalOptionLabel(c, proposal)}</option>)}</select>
                <input value={decisionSummary} onChange={(e) => onDecisionSummaryChange(e.target.value)} placeholder={c.decisionPlaceholder} disabled={loading !== ""} style={inputStyle} aria-label={c.decisionSummary} />
                <input value={decisionRationale} onChange={(e) => onDecisionRationaleChange(e.target.value)} placeholder={c.decisionRationalePlaceholder} disabled={loading !== ""} style={inputStyle} aria-label={c.decisionRationale} />
                <input value={decisionRollback} onChange={(e) => onDecisionRollbackChange(e.target.value)} placeholder={c.decisionRollbackPlaceholder} disabled={loading !== ""} style={inputStyle} aria-label={c.decisionRollback} />
                <button type="button" onClick={() => void onDecide()} disabled={decideDisabled} style={{ ...primaryButtonStyle, opacity: decideDisabled ? 0.62 : 1, cursor: decideDisabled ? "default" : "pointer" }}>{loading === "decide" ? c.deciding : c.decideAction}</button>
            </div>
        </>}
        <div style={proposalFormGridStyle}>
            <input value={escalationReason} onChange={(e) => onEscalationReasonChange(e.target.value)} placeholder={c.escalationReasonPlaceholder} disabled={loading !== ""} style={inputStyle} aria-label={c.escalationReason} />
            <input value={escalationTarget} onChange={(e) => onEscalationTargetChange(e.target.value)} placeholder={c.escalationTargetPlaceholder} disabled={loading !== ""} style={inputStyle} aria-label={c.escalationTarget} />
            <button type="button" onClick={() => void onEscalate()} disabled={escalateDisabled} style={{ ...dangerButtonStyle, opacity: escalateDisabled ? 0.62 : 1, cursor: escalateDisabled ? "default" : "pointer" }}>{loading === "escalate" ? c.escalating : c.escalateAction}</button>
        </div>
    </section>;
}
function MessageComposer({ c, kind, text, loading, disabled, onKindChange, onTextChange, onSend }: { c: typeof copy.en; kind: string; text: string; loading: boolean; disabled: boolean; onKindChange: (value: string) => void; onTextChange: (value: string) => void; onSend: () => void }) {
    return <section style={composerStyle}>
        <div style={composerHeaderStyle}><h4 style={detailTitleStyle}>{c.addMessage}</h4><select value={kind} onChange={(e) => onKindChange(e.target.value)} disabled={loading} style={selectStyle} aria-label={c.messageKind}><option value="statement">{c.statement}</option><option value="question">{c.questionKind}</option><option value="answer">{c.answerKind}</option><option value="evidence">{c.evidenceKind}</option><option value="objection">{c.objectionKind}</option></select></div>
        <textarea value={text} onChange={(e) => onTextChange(e.target.value)} placeholder={c.messagePlaceholder} rows={3} disabled={loading} style={textareaStyle} aria-label={c.messageText} />
        <div style={composerFooterStyle}><button type="button" onClick={() => void onSend()} disabled={disabled} style={{ ...primaryButtonStyle, opacity: disabled ? 0.62 : 1, cursor: disabled ? "default" : "pointer" }}>{loading ? c.sendingMessage : c.sendMessage}</button></div>
    </section>;
}
function InvitePanel({ c, lang, experts, loaded, target, role, loadingExperts, inviting, disabled, safeHandoff, onLoad, onTargetChange, onRoleChange, onInvite }: { c: typeof copy.en; lang: string; experts: GroupExpert[]; loaded: boolean; target: string; role: string; loadingExperts: boolean; inviting: boolean; disabled: boolean; safeHandoff?: DiscussionSafeHandoff | null; onLoad: () => void; onTargetChange: (value: string) => void; onRoleChange: (value: string) => void; onInvite: () => void }) {
    const busy = loadingExperts || inviting;
    return <section style={composerStyle}>
        <div style={composerHeaderStyle}><h4 style={detailTitleStyle}>{c.inviteExpert}</h4><button type="button" onClick={() => void onLoad()} disabled={busy} style={{ ...buttonStyle, opacity: busy ? 0.62 : 1, cursor: busy ? "default" : "pointer" }}>{loadingExperts ? c.loadingExperts : c.loadExperts}</button></div>
        {loaded && experts.length === 0 && <div style={emptyStyle}>{c.noExperts}</div>}
        {loaded && safeHandoff && <DiscussionSafeHandoffBlock lang={lang} focusContext={safeHandoff.focusContext} recommendedToolCall={safeHandoff.recommendedToolCall} boundary={safeHandoff.boundary} />}
        {experts.length > 0 && <div style={inviteGridStyle}><select value={target} onChange={(e) => onTargetChange(e.target.value)} disabled={busy} style={selectStyle} aria-label={c.inviteExpert}>{experts.map((expert) => <option key={expert.agent_id} value={expert.agent_id || ""}>{expertLabel(expert)}</option>)}</select><select value={role} onChange={(e) => onRoleChange(e.target.value)} disabled={busy} style={selectStyle} aria-label={c.inviteAs}><option value="observe">{c.observeRole}</option><option value="speak">{c.speakRole}</option><option value="review">{c.reviewRole}</option></select><button type="button" onClick={() => void onInvite()} disabled={disabled} style={{ ...primaryButtonStyle, opacity: disabled ? 0.62 : 1, cursor: disabled ? "default" : "pointer" }}>{inviting ? c.inviting : c.invite}</button></div>}
    </section>;
}
function formatSummaryPreview(c: typeof copy.en, result: DiscussionSummaryResult): string {
    const lines: string[] = [];
    if (result.summary) lines.push(result.summary);
    if (result.rationale) lines.push("Rationale:\n" + result.rationale);
    if (result.risks?.length) lines.push(c.risks + ":\n" + result.risks.map((item) => "- " + item).join("\n"));
    if (result.disagreements?.length) lines.push("Disagreements:\n" + result.disagreements.map((item) => "- " + item).join("\n"));
    if (result.open_questions?.length) lines.push("Open questions:\n" + result.open_questions.map((item) => "- " + item).join("\n"));
    const contributions = result.participant_contributions || {};
    const names = Object.keys(contributions).sort();
    if (names.length > 0) lines.push(c.contributions + ":\n" + names.map((name) => "- " + name + ": " + contributions[name]).join("\n"));
    const state = result.submitted ? c.submitted : result.injected ? c.injected : c.previewOnly;
    const meta = [result.answer_count ? String(result.answer_count) + " " + c.answers : "", result.used_llm ? c.usedLLM : "", typeof result.confidence === "number" && result.confidence > 0 ? c.confidence + ": " + result.confidence.toFixed(2) : "", state].filter(Boolean);
    if (meta.length > 0) lines.push(meta.join(" \u00b7 "));
    return lines.filter(Boolean).join("\n\n") || "-";
}
function timeValue(value?: string): number { if (!value) return 0; const n = new Date(value).getTime(); return Number.isFinite(n) ? n : 0; }
function formatDate(value: string | undefined, lang: string): string { if (!value) return "-"; const locale = lang === "zh-Hans" ? "zh-CN" : lang === "zh-Hant" ? "zh-TW" : "en-US"; try { return new Date(value).toLocaleString(locale); } catch { return value; } }
function listLine(label: string, items?: string[]): string { return Array.isArray(items) && items.length > 0 ? label + ": " + items.join(", ") : ""; }
function splitList(value: string): string[] { return value.split(/[,\n]/).map((item) => item.trim()).filter(Boolean); }
function localText(lang: string, en: string, zhHans: string, zhHant?: string): string { return lang === "zh-Hant" ? (zhHant || zhHans) : lang === "zh-Hans" ? zhHans : en; }
function isOpenProposal(proposal: DiscussionProposal): boolean { const status = String(proposal.status || "open").toLowerCase(); return String(proposal.id || "").trim() !== "" && status === "open"; }
function proposalOptionLabel(c: typeof copy.en, proposal: DiscussionProposal): string { return (proposal.title || proposal.id || c.proposal) + (proposal.status ? " [" + proposal.status + "]" : ""); }
function reviewSummaryView(proposalID?: string, reviews?: DiscussionReview[], summaries?: Record<string, DiscussionReviewSummary>): { counts: { approve: number; reject: number; concern: number; abstain: number }; reviewedBy: string[] } | null {
    const id = String(proposalID || "").trim();
    if (!id) return null;
    const summary = summaries?.[id];
    if (summary) return { counts: { approve: summary.approvals || 0, reject: summary.rejections || 0, concern: summary.concerns || 0, abstain: summary.abstains || 0 }, reviewedBy: cleanReviewers(summary.reviewed_by) };
    if (!Array.isArray(reviews)) return null;
    const latest = new Map<string, string>();
    for (const review of reviews) if (review.proposal_id === id && review.reviewer_id) latest.set(review.reviewer_id, String(review.position || "abstain"));
    if (latest.size === 0) return null;
    const counts = { approve: 0, reject: 0, concern: 0, abstain: 0 };
    latest.forEach((position) => { if (position === "approve") counts.approve += 1; else if (position === "reject") counts.reject += 1; else if (position === "concern") counts.concern += 1; else counts.abstain += 1; });
    return { counts, reviewedBy: cleanReviewers(Array.from(latest.keys())) };
}
function cleanReviewers(reviewedBy?: string[]): string[] { return Array.isArray(reviewedBy) ? reviewedBy.map((item) => String(item || "").trim()).filter(Boolean).sort() : []; }
function filterInviteExperts(values: GroupExpert[], detail: DiscussionDetail | null): GroupExpert[] {
    const participants = new Set((detail?.discussion?.participant_ids || []).map((id) => String(id || "").trim()).filter(Boolean));
    return values.filter((expert) => {
        const id = String(expert.agent_id || "").trim();
        if (!id || participants.has(id)) return false;
        if (expert.discoverable === false || expert.available === false) return false;
        return true;
    }).sort((a, b) => String(a.display_name || a.agent_id || "").localeCompare(String(b.display_name || b.agent_id || "")));
}
function discussionExpertsSafeHandoff(experts: GroupExpert[]): DiscussionSafeHandoff {
    const first = experts[0] || {};
    const firstSkills = Array.isArray(first.skills) ? first.skills.map((item) => String(item || "").trim()).filter(Boolean) : [];
    const focusContext: Record<string, unknown> = {
        action_kind: "inspect_group_discussion_experts",
        reason: "read-only A2A expert discovery before ranking invitees or sending invitations",
        expert_count: experts.length,
    };
    if (first.agent_id) focusContext.recommended_agent_id = first.agent_id;
    if (first.display_name) focusContext.recommended_display_name = first.display_name;
    if (firstSkills.length > 0) focusContext.recommended_skills = firstSkills;
    const recommendedToolCall: DiscussionToolCallSuggestion = {
        tool: "group_discussion",
        args: { action: experts.length > 0 ? "rank_experts" : "list_experts" },
        recommended_focus_context: focusContext,
        discussion_focus_context: focusContext,
        non_executing: true,
        non_executing_boundary: "recommended expert-discovery follow-up only; it may rank experts or repeat discovery, and must not start a discussion, invite experts, send messages, mutate Hub state, mutate memory, or change routing",
    };
    return {
        focusContext,
        recommendedToolCall,
        boundary: "read-only expert discovery inspection; no discussion was started, no experts were invited, no messages were sent, no Hub state changed, no memory was promoted, and no routing changed",
    };
}
function expertLabel(expert: GroupExpert): string {
    const id = String(expert.agent_id || "").trim();
    const name = String(expert.display_name || "").trim();
    const skills = Array.isArray(expert.skills) && expert.skills.length > 0 ? " - " + expert.skills.slice(0, 3).join(", ") : "";
    return (name && name !== id ? name + " (" + id + ")" : id) + skills;
}

const sectionStyle: CSSProperties = { border: "1px solid " + colors.border, borderRadius: radius.xl, padding: "14px", background: colors.surface };
const headerStyle: CSSProperties = { display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 12, marginBottom: 12, flexWrap: "wrap" };
const titleStyle: CSSProperties = { fontSize: "0.88rem", fontWeight: 800, color: colors.text, margin: 0 };
const descStyle: CSSProperties = { fontSize: "0.72rem", color: colors.textMuted, lineHeight: 1.5, marginTop: 4 };
const toolbarStyle: CSSProperties = { display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" };
const selectStyle: CSSProperties = { border: "1px solid " + colors.border, borderRadius: radius.md, padding: "6px 9px", fontSize: "0.74rem", background: colors.bg, color: colors.text };
const inputStyle: CSSProperties = { ...selectStyle, minWidth: 0 };
const buttonStyle: CSSProperties = { border: "1px solid " + colors.border, borderRadius: radius.md, padding: "6px 10px", fontSize: "0.72rem", fontWeight: 700, background: colors.bg, color: colors.textSecondary, cursor: "pointer", whiteSpace: "nowrap" };
const primaryButtonStyle: CSSProperties = { ...buttonStyle, border: "1px solid " + colors.primary, background: colors.primaryLight, color: colors.primaryDark };
const dangerButtonStyle: CSSProperties = { ...buttonStyle, border: "1px solid " + colors.danger, background: colors.dangerBg, color: colors.danger };
const emptyStyle: CSSProperties = { border: "1px dashed " + colors.border, borderRadius: radius.md, padding: "14px", color: colors.textMuted, fontSize: "0.76rem", textAlign: "center" };
const errorStyle: CSSProperties = { border: "1px solid " + colors.danger, borderRadius: radius.md, padding: "10px", color: colors.danger, background: colors.dangerBg, fontSize: "0.74rem", marginBottom: 10 };
const successStyle: CSSProperties = { border: "1px solid " + colors.success, borderRadius: radius.md, padding: "8px 10px", color: colors.success, background: colors.successBg, fontSize: "0.74rem" };
const listStyle: CSSProperties = { display: "flex", flexDirection: "column", gap: 8, maxHeight: 360, overflowY: "auto" };
const rowStyle: CSSProperties = { border: "1px solid " + colors.borderLight, borderRadius: radius.md, background: colors.bg, padding: "9px 10px", display: "grid", gridTemplateColumns: "minmax(0, 1fr) auto", gap: 10, alignItems: "center" };
const rowMainStyle: CSSProperties = { display: "flex", flexDirection: "column", gap: 3, minWidth: 0, border: 0, background: "transparent", padding: 0, textAlign: "left", cursor: "pointer" };
const rowTitleStyle: CSSProperties = { color: colors.text, fontSize: "0.8rem", fontWeight: 800, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" };
const rowQuestionStyle: CSSProperties = { color: colors.textSecondary, fontSize: "0.72rem", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" };
const rowMetaStyle: CSSProperties = { color: colors.textMuted, fontSize: "0.66rem" };
const rowSummaryStyle: CSSProperties = { color: colors.textSecondary, fontSize: "0.72rem", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" };
const rowActionsStyle: CSSProperties = { display: "flex", gap: 6, alignItems: "center", flexWrap: "wrap", justifyContent: "flex-end" };
const stateControlActionsStyle: CSSProperties = { display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap", justifyContent: "flex-end" };
const proposalFormGridStyle: CSSProperties = { display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(160px, 1fr))", gap: 8, alignItems: "center" };
const composerStyle: CSSProperties = { border: "1px solid " + colors.borderLight, borderRadius: radius.md, background: colors.bg, padding: "10px 12px", display: "flex", flexDirection: "column", gap: 8 };
const composerHeaderStyle: CSSProperties = { display: "flex", alignItems: "center", justifyContent: "space-between", gap: 10, flexWrap: "wrap" };
const composerFooterStyle: CSSProperties = { display: "flex", justifyContent: "flex-end" };
const inviteGridStyle: CSSProperties = { display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(130px, 1fr))", gap: 8, alignItems: "center" };
const textareaStyle: CSSProperties = { border: "1px solid " + colors.border, borderRadius: radius.md, padding: "8px 10px", minHeight: 72, resize: "vertical", background: colors.surface, color: colors.text, fontFamily: "inherit", fontSize: "0.74rem", lineHeight: 1.5 };
const readyBadgeStyle: CSSProperties = { border: "1px solid " + colors.success, borderRadius: radius.pill, color: colors.success, background: colors.successBg, padding: "3px 8px", fontSize: "0.66rem", fontWeight: 800, whiteSpace: "nowrap" };
const waitingBadgeStyle: CSSProperties = { ...readyBadgeStyle, border: "1px solid " + colors.border, color: colors.textMuted, background: colors.surfaceMuted };
const overlayStyle: CSSProperties = { position: "fixed", inset: 0, background: "rgba(0,0,0,0.42)", display: "flex", alignItems: "center", justifyContent: "center", zIndex: 9999 };
const modalStyle: CSSProperties = { width: "min(860px, 92vw)", maxHeight: "82vh", borderRadius: radius.lg, background: colors.surface, boxShadow: "0 18px 52px rgba(0,0,0,0.22)", display: "flex", flexDirection: "column", overflow: "hidden" };
const modalHeaderStyle: CSSProperties = { display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 12, padding: "14px 16px", borderBottom: "1px solid " + colors.border };
const modalTitleStyle: CSSProperties = { margin: 0, color: colors.text, fontSize: "0.92rem", fontWeight: 900, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" };
const modalBodyStyle: CSSProperties = { padding: 14, overflowY: "auto", display: "flex", flexDirection: "column", gap: 10 };
const detailBlockStyle: CSSProperties = { border: "1px solid " + colors.borderLight, borderRadius: radius.md, background: colors.bg, padding: "10px 12px" };
const detailTitleStyle: CSSProperties = { margin: "0 0 6px", color: colors.textSecondary, fontSize: "0.72rem", fontWeight: 800 };
const preStyle: CSSProperties = { margin: 0, whiteSpace: "pre-wrap", wordBreak: "break-word", fontFamily: "inherit", lineHeight: 1.55, color: colors.text, fontSize: "0.74rem" };
const proposalDetailSectionStyle: CSSProperties = { display: "flex", flexDirection: "column", gap: 8 };
const proposalDetailListStyle: CSSProperties = { display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(240px, 1fr))", gap: 8 };
const proposalDetailItemStyle: CSSProperties = { border: "1px solid " + colors.borderLight, borderRadius: radius.md, background: colors.bg, padding: "10px 12px", display: "flex", flexDirection: "column", gap: 8, minWidth: 0 };
const proposalDetailHeaderStyle: CSSProperties = { display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 8 };
const proposalDetailTitleStyle: CSSProperties = { color: colors.text, fontSize: "0.78rem", fontWeight: 850, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", minWidth: 0 };
const proposalDetailBodyStyle: CSSProperties = { color: colors.textSecondary, fontSize: "0.74rem", lineHeight: 1.5, whiteSpace: "pre-wrap", wordBreak: "break-word" };
const proposalDetailMetaStyle: CSSProperties = { color: colors.textMuted, fontSize: "0.66rem", lineHeight: 1.45, wordBreak: "break-word" };
const reviewSummaryBoxStyle: CSSProperties = { display: "flex", flexDirection: "column", gap: 6 };
const reviewSummaryBadgeRowStyle: CSSProperties = { display: "flex", gap: 6, flexWrap: "wrap" };
const reviewCountBadgeStyle: CSSProperties = { borderRadius: radius.pill, padding: "2px 7px", fontSize: "0.66rem", lineHeight: 1.5, whiteSpace: "nowrap", border: "1px solid " + colors.border };
const reviewSuccessBadgeStyle: CSSProperties = { border: "1px solid " + colors.success, background: colors.successBg, color: colors.success };
const reviewWarningBadgeStyle: CSSProperties = { border: "1px solid " + colors.warning, background: colors.warningBg, color: colors.warning };
const reviewDangerBadgeStyle: CSSProperties = { border: "1px solid " + colors.danger, background: colors.dangerBg, color: colors.danger };
const reviewMutedBadgeStyle: CSSProperties = { border: "1px solid " + colors.border, background: colors.surfaceMuted, color: colors.textMuted };
const workflowGridStyle: CSSProperties = { display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(92px, 1fr))", gap: 6 };
const workflowMetricStyle: CSSProperties = { border: "1px solid " + colors.borderLight, borderRadius: radius.sm, background: colors.surface, padding: "5px 7px", display: "flex", justifyContent: "space-between", gap: 6, fontSize: "0.66rem", color: colors.textSecondary };
const workflowTextStyle: CSSProperties = { fontSize: "0.68rem", color: colors.textSecondary, lineHeight: 1.45, wordBreak: "break-word", whiteSpace: "pre-wrap" };
const workflowBlockerListStyle: CSSProperties = { display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))", gap: 6 };
const workflowBlockerStyle: CSSProperties = { border: "1px solid " + colors.warning, borderRadius: radius.sm, background: colors.warningBg, padding: "6px 8px", display: "flex", flexDirection: "column", gap: 3, minWidth: 0 };
const workflowBlockerBadgeStyle: CSSProperties = { alignSelf: "flex-start", border: "1px solid " + colors.warning, borderRadius: radius.pill, padding: "1px 6px", fontSize: "0.62rem", fontWeight: 800, color: colors.warning, background: colors.surface };
const workflowBlockerTextStyle: CSSProperties = { fontSize: "0.68rem", color: colors.textSecondary, lineHeight: 1.4, overflowWrap: "anywhere" };
const workflowBlockerMetaStyle: CSSProperties = { fontSize: "0.64rem", color: colors.textMuted, overflowWrap: "anywhere" };
const workflowProposalListStyle: CSSProperties = { display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))", gap: 6 };
const workflowProposalStyle: CSSProperties = { border: "1px solid " + colors.borderLight, borderRadius: radius.sm, background: colors.surface, padding: "6px 8px", display: "flex", flexDirection: "column", gap: 4, minWidth: 0 };
const workflowProposalTitleStyle: CSSProperties = { color: colors.text, fontSize: "0.7rem", fontWeight: 750, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" };
