package main

type skillVerificationStatusKind string

const (
	skillVerificationStatusMissingEntry      skillVerificationStatusKind = "missing_entry"
	skillVerificationStatusVerifiedSuccess   skillVerificationStatusKind = "verified_success"
	skillVerificationStatusFailed            skillVerificationStatusKind = "failed"
	skillVerificationStatusNeedsRuntimeProof skillVerificationStatusKind = "needs_runtime_proof"
	skillVerificationStatusStaticChecked     skillVerificationStatusKind = "static_checked"
)

func (status skillVerificationStatusKind) String() string {
	return string(status)
}
