package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestWaitPollSecondsAndNoteNeedsAJob(t *testing.T) {
	if desktopWaitPollSeconds("sleep 85; tail -n 3 train.log") != 85 || desktopWaitPollSeconds("python x.py") != 0 {
		t.Fatal("sleep parse")
	}
	if n := desktopWaitPollNote(agentruntime.Scope{RunID: "run-waitpoll-nojob"}, "sleep 85; tail log"); n != "" || strings.Contains(n, "job_status") {
		t.Fatalf("note without a job: %q", n)
	}
}
