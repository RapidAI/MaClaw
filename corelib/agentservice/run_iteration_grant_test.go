package agentservice

import "testing"

func TestExtendIterationBudgetAsksTheHost(t *testing.T) {
	prev := RunIterationGrant
	t.Cleanup(func() { RunIterationGrant = prev })
	RunIterationGrant = nil
	if n := (&coreAgentCallbacks{runID: "run_x"}).ExtendIterationBudget(300, 120); n != 0 {
		t.Fatalf("no host hook granted %d", n)
	}
	var gotRun string
	RunIterationGrant = func(runID string, used, toolCalls int) int { gotRun = runID; return 100 }
	if n := (&coreAgentCallbacks{runID: "run_x"}).ExtendIterationBudget(300, 120); n != 100 || gotRun != "run_x" {
		t.Fatalf("grant=%d run=%q", n, gotRun)
	}
	if n := (&coreAgentCallbacks{}).ExtendIterationBudget(300, 120); n != 0 {
		t.Fatalf("run-less callbacks granted %d", n)
	}
}
