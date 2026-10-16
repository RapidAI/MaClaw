package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestSessionSummaryRuntimeStatusProjection(t *testing.T) {
	cases := map[string]agentruntime.JobStatus{
		string(SessionStarting):     agentruntime.JobStatusRunning,
		string(SessionRunning):      agentruntime.JobStatusRunning,
		string(SessionBusy):         agentruntime.JobStatusRunning,
		string(SessionWaitingInput): agentruntime.JobStatusPending,
		string(SessionError):        agentruntime.JobStatusFailed,
		string(SessionExited):       agentruntime.JobStatusUnknown,
		"legacy":                    agentruntime.JobStatusUnknown,
	}
	for status, want := range cases {
		if got := sessionSummaryRuntimeStatus(status); got != want {
			t.Errorf("sessionSummaryRuntimeStatus(%q)=%q, want %q", status, got, want)
		}
	}
	summary := SessionSummary{Status: string(SessionRunning)}
	sanitizeSessionSummary(&summary)
	if summary.RuntimeStatus != agentruntime.JobStatusRunning {
		t.Fatalf("sanitized runtime status=%q, want running", summary.RuntimeStatus)
	}
}

func TestRemoteSessionViewProjectsRuntimeStatus(t *testing.T) {
	session := &RemoteSession{ID: "sess-1", Status: SessionError, Summary: SessionSummary{Status: string(SessionError)}}
	view := toRemoteSessionView(session)
	if view.RuntimeStatus != agentruntime.JobStatusFailed {
		t.Fatalf("view runtime status=%q, want failed", view.RuntimeStatus)
	}
	if view.Summary.RuntimeStatus != agentruntime.JobStatusFailed {
		t.Fatalf("summary runtime status=%q, want failed", view.Summary.RuntimeStatus)
	}
}
