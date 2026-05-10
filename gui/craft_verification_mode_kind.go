package main

import "strings"

type craftVerificationModeKind string

const (
	craftVerificationModeDefault          craftVerificationModeKind = ""
	craftVerificationModeArtifactRequired craftVerificationModeKind = "artifact_required"
)

func normalizeCraftVerificationModeKind(value string) craftVerificationModeKind {
	switch craftVerificationModeKind(strings.ToLower(strings.TrimSpace(value))) {
	case craftVerificationModeArtifactRequired:
		return craftVerificationModeArtifactRequired
	default:
		return craftVerificationModeDefault
	}
}

func (k craftVerificationModeKind) RequiresArtifact() bool {
	return k == craftVerificationModeArtifactRequired
}
