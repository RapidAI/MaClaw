package main

import "strings"

type launchModeKind string

const (
	launchModeLocal   launchModeKind = "local"
	launchModeRemote  launchModeKind = "remote"
	launchModeInvalid launchModeKind = ""
)

func normalizeLaunchModeKind(value string) launchModeKind {
	switch launchModeKind(strings.ToLower(strings.TrimSpace(value))) {
	case launchModeLocal:
		return launchModeLocal
	case launchModeRemote:
		return launchModeRemote
	default:
		return launchModeInvalid
	}
}

func (mode launchModeKind) String() string {
	return string(mode)
}

func (mode launchModeKind) IsRemote() bool {
	return mode == launchModeRemote
}
