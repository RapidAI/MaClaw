package main

import "strings"

type misTransactionStateKind string

const (
	misTransactionStateUnknown            misTransactionStateKind = ""
	misTransactionStateCollecting         misTransactionStateKind = "collecting"
	misTransactionStateValidating         misTransactionStateKind = "validating"
	misTransactionStateAwaitingValidation misTransactionStateKind = "awaiting_validation"
	misTransactionStateValidationFailed   misTransactionStateKind = "validation_failed"
	misTransactionStateAwaitingCommit     misTransactionStateKind = "awaiting_commit"
	misTransactionStateCommitFailed       misTransactionStateKind = "commit_failed"
	misTransactionStateCommitted          misTransactionStateKind = "committed"
)

func normalizeMISTransactionStateKind(value string) misTransactionStateKind {
	switch misTransactionStateKind(strings.TrimSpace(value)) {
	case misTransactionStateCollecting:
		return misTransactionStateCollecting
	case misTransactionStateValidating:
		return misTransactionStateValidating
	case misTransactionStateAwaitingValidation:
		return misTransactionStateAwaitingValidation
	case misTransactionStateValidationFailed:
		return misTransactionStateValidationFailed
	case misTransactionStateAwaitingCommit:
		return misTransactionStateAwaitingCommit
	case misTransactionStateCommitFailed:
		return misTransactionStateCommitFailed
	case misTransactionStateCommitted:
		return misTransactionStateCommitted
	default:
		return misTransactionStateUnknown
	}
}

func (kind misTransactionStateKind) String() string {
	return string(kind)
}

func (kind misTransactionStateKind) IsRecoverable() bool {
	switch kind {
	case misTransactionStateUnknown,
		misTransactionStateCollecting,
		misTransactionStateValidating,
		misTransactionStateAwaitingValidation,
		misTransactionStateValidationFailed,
		misTransactionStateAwaitingCommit,
		misTransactionStateCommitFailed:
		return true
	default:
		return false
	}
}
