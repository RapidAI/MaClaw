package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func sshAdapterResult(t *testing.T, handlerErr error) string {
	t.Helper()
	h := &IMMessageHandler{registry: NewToolRegistry()}
	h.semanticTrustedSSH = func(string, string) (string, error) {
		return "", handlerErr
	}
	callbacks := &sharedAgentLoopCallbacks{handler: h, userID: "user-1"}
	got := callbacks.executeTrustedSSH(tool.PlannedSelection{}, tool.CanonicalRequest{
		CanonicalJSON: []byte(`{"command":"echo hi"}`),
	})
	if !strings.Contains(got, handlerErr.Error()) {
		t.Fatalf("adapter did not reach the handler failure %q, got %q", handlerErr, got)
	}
	return got
}

// A session that ended after the command was written leaves no way to know
// whether the command ran. Until this fact had its own name it was classified
// correctly only by coincidence -- the adapter's list happens to treat every
// lost session as unknown -- so the name has to survive on its own merits.
func TestSSHDispatchedThenLostStaysUnknown(t *testing.T) {
	got := sshAdapterResult(t, fmt.Errorf("trusted_ssh_outcome_unobserved"))
	if !strings.HasPrefix(got, "[system unknown]") {
		t.Fatalf("dispatched-then-lost command = %q, want an unknown outcome", got)
	}
}

// Splitting the vocabulary must not have narrowed anything. These two still
// report unknown, which is over-cautious rather than unsafe, and the point of
// pinning them is that a later tightening should be a deliberate change with
// its own reasoning rather than a side effect of renaming one case.
func TestSSHLostSessionNamesAllStillReportUnknown(t *testing.T) {
	for _, name := range []string{
		"trusted_ssh_session_disconnected",
		"trusted_ssh_session_unavailable",
		"trusted_ssh_timeout",
	} {
		t.Run(name, func(t *testing.T) {
			got := sshAdapterResult(t, fmt.Errorf("%s", name))
			if !strings.HasPrefix(got, "[system unknown]") {
				t.Fatalf("%s = %q, want an unknown outcome", name, got)
			}
		})
	}
}
