package main

import "github.com/RapidAI/CodeClaw/corelib/agentservice"

// A publish with a final state ends the run with its templated reply: the
// model gets no further turn in which to retry, wander or narrate.
func init() { agentservice.RunGoalReached = desktopPublishSettled }

func desktopPublishSettled(runID string) (string, bool) {
	if rec, ok := desktopPublishRecordOf(runID); ok {
		if r := desktopReplyTemplate(rec); r != "" {
			return r, true
		}
	}
	// The watchdog ended the run: its honest result is the answer.
	return desktopWatchdogVerdict(runID)
}
