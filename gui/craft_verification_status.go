package main

type craftVerificationStatus string

const (
	craftVerificationPassed           craftVerificationStatus = "passed"
	craftVerificationArtifactMissing  craftVerificationStatus = "artifact_missing"
	craftVerificationRuntimeMissing   craftVerificationStatus = "runtime_missing"
	craftVerificationExecutionFailed  craftVerificationStatus = "execution_failed"
	craftVerificationOutputSuspicious craftVerificationStatus = "output_suspicious"
)

func (status craftVerificationStatus) String() string {
	return string(status)
}
