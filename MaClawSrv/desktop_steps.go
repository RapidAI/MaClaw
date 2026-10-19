package main

import (
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// The step journal is the service database, shared by an agent turn and a
// schedule fire. It is not the scheduler and it is not the in-process tool
// journal from PR-13.
var (
	desktopStepsMu     sync.Mutex
	desktopStepsRunner agentservice.DesktopStepper
)

func bindDesktopSteps(steps agentservice.DesktopStepper) {
	desktopStepsMu.Lock()
	desktopStepsRunner = steps
	desktopStepsMu.Unlock()
}

func currentDesktopSteps() agentservice.DesktopStepper {
	desktopStepsMu.Lock()
	defer desktopStepsMu.Unlock()
	return desktopStepsRunner
}
