package agentservice

// RunGoalReached lets the hosting server declare a run's goal settled (for
// example a publish whose outcome is final: live, incomplete, or submitted
// and not found in the post list). The loop then ends cleanly with reply as
// the answer instead of letting the model keep acting on a settled task.
var RunGoalReached func(runID string) (reply string, ok bool)

// EarlyStop implements agent.EarlyStopper.
func (c *coreAgentCallbacks) EarlyStop() (bool, string, string) {
	if RunGoalReached == nil || c.runtimeRequest.RunID == "" {
		return false, "", ""
	}
	if reply, ok := RunGoalReached(c.runtimeRequest.RunID); ok {
		return true, "", reply
	}
	return false, "", ""
}
