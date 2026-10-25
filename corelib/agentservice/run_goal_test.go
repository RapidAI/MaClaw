package agentservice

import "testing"

// EarlyStop reads the run id every run carries, not one set only for
// attempts with a runtime store (ZN-25 kept acting after a settled publish).
func TestEarlyStopUsesTheRequestRunID(t *testing.T) {
	old := RunGoalReached
	defer func() { RunGoalReached = old }()
	RunGoalReached = func(id string) (string, bool) { return "done " + id, id == "run_x" }
	c := &coreAgentCallbacks{runID: "run_x"}
	if stop, code, text := c.EarlyStop(); !stop || code != "" || text != "done run_x" {
		t.Fatalf("stop=%v code=%q text=%q", stop, code, text)
	}
	if stop, _, _ := (&coreAgentCallbacks{}).EarlyStop(); stop {
		t.Fatal("no run id must not stop")
	}
}
