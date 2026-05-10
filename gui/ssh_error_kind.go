package main

import "strings"

type sshErrorKind int

const (
	sshErrorOther sshErrorKind = iota
	sshErrorAuthentication
)

func classifySSHError(err error) sshErrorKind {
	if err == nil {
		return sshErrorOther
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "unable to authenticate"),
		strings.Contains(lower, "handshake failed"),
		strings.Contains(lower, "permission denied"),
		strings.Contains(lower, "no supported methods"):
		return sshErrorAuthentication
	default:
		return sshErrorOther
	}
}
