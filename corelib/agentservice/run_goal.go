package agentservice

// RunGoalReached lets the hosting server declare a run's goal settled (for
// example a publish whose outcome is final: live, incomplete, or submitted
// and not found in the post list). The loop then ends cleanly with reply as
// the answer instead of letting the model keep acting on a settled task.
var RunGoalReached func(runID string) (reply string, ok bool)

// EarlyStop implements agent.EarlyStopper.
func (c *coreAgentCallbacks) EarlyStop() (bool, string, string) {
	if RunGoalReached == nil || c.runID == "" {
		return false, "", ""
	}
	if reply, ok := RunGoalReached(c.runID); ok {
		return true, "", reply
	}
	return false, "", ""
}

// RunIterationGrant lets the hosting server give a run that is still making
// measurable progress more loop iterations when its iteration budget runs
// out, so a long task is ended by a stall, a loop or the person's stop, not
// by a fixed count. It returns 0 to let the budget end the turn.
var RunIterationGrant func(runID string, used, toolCalls int) int

// ExtendIterationBudget implements agent.IterationBudgetExtender.
func (c *coreAgentCallbacks) ExtendIterationBudget(used, toolCalls int) int {
	if c == nil || RunIterationGrant == nil || c.runID == "" {
		return 0
	}
	return RunIterationGrant(c.runID, used, toolCalls)
}
