package main

type workflowToolFilterDecision string

const (
	workflowToolFilterNone                 workflowToolFilterDecision = "none"
	workflowToolFilterSkippedConfirmBypass workflowToolFilterDecision = "skipped(SkipNeedsConfirmGate)"
)

func (d workflowToolFilterDecision) String() string {
	return string(d)
}
