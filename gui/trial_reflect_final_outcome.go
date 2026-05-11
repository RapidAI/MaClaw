package main

type trialReflectFinalOutcome string

const (
	trialReflectFinalOutcomeRecoveredSuccess trialReflectFinalOutcome = "recovered_success"
	trialReflectFinalOutcomePartialSuccess   trialReflectFinalOutcome = "partial_success"
	trialReflectFinalOutcomeSuccess          trialReflectFinalOutcome = "success"
	trialReflectFinalOutcomeFailed           trialReflectFinalOutcome = "failed"
)

func (outcome trialReflectFinalOutcome) String() string {
	return string(outcome)
}

func classifyTrialReflectFinalOutcome(status TraceRunStatus, sawFailure, sawSuccess bool) trialReflectFinalOutcome {
	recovered := sawFailure && sawSuccess
	switch {
	case recovered && status == TraceRunStatusCompleted:
		return trialReflectFinalOutcomeRecoveredSuccess
	case status == TraceRunStatusCompleted && sawFailure:
		return trialReflectFinalOutcomePartialSuccess
	case status == TraceRunStatusCompleted:
		return trialReflectFinalOutcomeSuccess
	case status == TraceRunStatusFailed || status == TraceRunStatusCancelled || status == TraceRunStatusStopped || status == TraceRunStatusTimeout:
		return trialReflectFinalOutcomeFailed
	default:
		return trialReflectFinalOutcome(status)
	}
}
