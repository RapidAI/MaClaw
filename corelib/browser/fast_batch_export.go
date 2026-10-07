package browser

import "fmt"

// RunAgentFastBatch runs same-page steps on an existing agent session and
// observes once at the end. It is the host entry used by MaClawSrv so a model
// turn does not click, wait, and re-observe one control at a time.
func RunAgentFastBatch(agentSession *BrowserAgentSession, steps []StepSpec, description string) (*TaskState, error) {
	return runAgentFastBatch(agentSession, steps, description, false)
}

// RunAgentFastBatchUntilPerson is the cloud-desktop batch. It stops before
// typing or clicking once the page is a login wall, MFA, or captcha, so the
// person can take the desktop. The local GUI browser agent does not use this.
func RunAgentFastBatchUntilPerson(agentSession *BrowserAgentSession, steps []StepSpec, description string) (*TaskState, error) {
	return runAgentFastBatch(agentSession, steps, description, true)
}

func runAgentFastBatch(agentSession *BrowserAgentSession, steps []StepSpec, description string, pauseForPerson bool) (*TaskState, error) {
	if agentSession == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	supervisor := NewBrowserTaskSupervisor(nil, nil, nil, func() (*Session, error) {
		if agentSession.session == nil {
			return nil, fmt.Errorf("browser session not connected")
		}
		return agentSession.session, nil
	}, nil)
	supervisor.agentSessionFn = func() (*BrowserAgentSession, error) {
		return agentSession, nil
	}
	return supervisor.Execute(TaskSpec{
		Description:    description,
		Steps:          steps,
		MaxRetries:     0,
		FastBatch:      true,
		PauseForPerson: pauseForPerson,
	})
}
