package main

import "github.com/RapidAI/CodeClaw/corelib/agentservice"

// A publish with a final state ends the run with its templated reply: the
// model gets no further turn in which to retry, wander or narrate.
func init() { agentservice.RunGoalReached = desktopPublishSettled }

func desktopPublishSettled(runID string) (string, bool) {
	rec, ok := desktopPublishRecordOf(runID)
	if !ok {
		return "", false
	}
	r := desktopReplyTemplate(rec)
	return r, r != ""
}
