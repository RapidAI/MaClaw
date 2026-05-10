package main

import "strings"

type gitBranchErrorKind int

const (
	gitBranchErrorUnknown gitBranchErrorKind = iota
	gitBranchErrorNotFoundOrCurrent
)

func classifyGitBranchError(err error) gitBranchErrorKind {
	if err == nil {
		return gitBranchErrorUnknown
	}
	msg := err.Error()
	if strings.Contains(msg, "not found") || strings.Contains(msg, "cannot delete branch") {
		return gitBranchErrorNotFoundOrCurrent
	}
	return gitBranchErrorUnknown
}

func (k gitBranchErrorKind) IgnorableDeleteFailure() bool {
	return k == gitBranchErrorNotFoundOrCurrent
}
