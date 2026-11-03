package main

import "github.com/RapidAI/CodeClaw/corelib/agentservice"

// A publish with a final state ends the run with its templated reply: the
// model gets no further turn in which to retry, wander or narrate.
func init() { agentservice.RunGoalReached = desktopPublishSettled }

func desktopPublishSettled(runID string) (string, bool) {
	// A request naming several sites is settled only when every site is.
	if base := desktopTaskSpecBase(runID); len(base.Sites) > 1 {
		if len(desktopTaskSitesPending(runID)) == 0 {
			if recs := desktopPublishRunRecords(runID); len(recs) > 0 {
				return desktopReplyMulti(base, recs, nil, ""), true
			}
		}
		return desktopWatchdogVerdict(runID)
	}
	if rec, ok := desktopPublishRecordOf(runID); ok {
		if r := desktopReplyTemplate(rec); r != "" {
			return r, true
		}
	}
	// The watchdog ended the run: its honest result is the answer.
	return desktopWatchdogVerdict(runID)
}
