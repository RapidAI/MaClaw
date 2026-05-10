package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

type groupDiscussionProposalStatusKind string

const (
	groupDiscussionProposalStatusUnknown    groupDiscussionProposalStatusKind = ""
	groupDiscussionProposalStatusOpen       groupDiscussionProposalStatusKind = groupDiscussionProposalStatusKind(a2a.ProposalOpen)
	groupDiscussionProposalStatusAccepted   groupDiscussionProposalStatusKind = groupDiscussionProposalStatusKind(a2a.ProposalAccepted)
	groupDiscussionProposalStatusRejected   groupDiscussionProposalStatusKind = groupDiscussionProposalStatusKind(a2a.ProposalRejected)
	groupDiscussionProposalStatusSuperseded groupDiscussionProposalStatusKind = groupDiscussionProposalStatusKind(a2a.ProposalSuperseded)
)

func normalizeGroupDiscussionProposalStatus(status string) groupDiscussionProposalStatusKind {
	switch groupDiscussionProposalStatusKind(strings.ToLower(strings.TrimSpace(status))) {
	case groupDiscussionProposalStatusOpen:
		return groupDiscussionProposalStatusOpen
	case groupDiscussionProposalStatusAccepted:
		return groupDiscussionProposalStatusAccepted
	case groupDiscussionProposalStatusRejected:
		return groupDiscussionProposalStatusRejected
	case groupDiscussionProposalStatusSuperseded:
		return groupDiscussionProposalStatusSuperseded
	default:
		return groupDiscussionProposalStatusUnknown
	}
}

func (status groupDiscussionProposalStatusKind) IsOpen() bool {
	return status == groupDiscussionProposalStatusUnknown || status == groupDiscussionProposalStatusOpen
}

func (status groupDiscussionProposalStatusKind) IsFinal() bool {
	switch status {
	case groupDiscussionProposalStatusAccepted, groupDiscussionProposalStatusRejected, groupDiscussionProposalStatusSuperseded:
		return true
	default:
		return false
	}
}

type groupDiscussionSessionStatusKind string

const (
	groupDiscussionSessionStatusUnknown   groupDiscussionSessionStatusKind = ""
	groupDiscussionSessionStatusOpen      groupDiscussionSessionStatusKind = groupDiscussionSessionStatusKind(a2a.SessionOpen)
	groupDiscussionSessionStatusEscalated groupDiscussionSessionStatusKind = groupDiscussionSessionStatusKind(a2a.SessionEscalated)
)

func normalizeGroupDiscussionSessionStatus(status string) groupDiscussionSessionStatusKind {
	switch groupDiscussionSessionStatusKind(strings.ToLower(strings.TrimSpace(status))) {
	case groupDiscussionSessionStatusOpen:
		return groupDiscussionSessionStatusOpen
	case groupDiscussionSessionStatusEscalated:
		return groupDiscussionSessionStatusEscalated
	default:
		return groupDiscussionSessionStatusKind(strings.ToLower(strings.TrimSpace(status)))
	}
}

func (status groupDiscussionSessionStatusKind) IsSetAndNotOpen() bool {
	return status != groupDiscussionSessionStatusUnknown && status != groupDiscussionSessionStatusOpen
}

func (status groupDiscussionSessionStatusKind) IsOpen() bool {
	return status == groupDiscussionSessionStatusOpen
}
