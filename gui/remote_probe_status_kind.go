package main

import "strings"

type remoteProbeStatusKind string

const (
	remoteProbeStatusUnknown  remoteProbeStatusKind = ""
	remoteProbeStatusNotFound remoteProbeStatusKind = "not_found"
	remoteProbeStatusBlocked  remoteProbeStatusKind = "blocked"
)

func normalizeRemoteProbeStatusKind(value string) remoteProbeStatusKind {
	switch remoteProbeStatusKind(strings.ToLower(strings.TrimSpace(value))) {
	case remoteProbeStatusNotFound:
		return remoteProbeStatusNotFound
	case remoteProbeStatusBlocked:
		return remoteProbeStatusBlocked
	default:
		return remoteProbeStatusUnknown
	}
}

func (kind remoteProbeStatusKind) ShouldClearActivation() bool {
	switch kind {
	case remoteProbeStatusNotFound, remoteProbeStatusBlocked:
		return true
	default:
		return false
	}
}
