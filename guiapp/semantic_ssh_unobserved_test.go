package guiapp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestTrustedSSHWaitResultKeepsFinishedOutput(t *testing.T) {
	got, err := trustedSSHWaitResult([]string{"ok", "root@host:~#"}, remote.SessionRunning, context.DeadlineExceeded, "")
	if err != nil || got != "ok\nroot@host:~#" {
		t.Fatalf("finished output at the deadline = %q err=%v", got, err)
	}
	_, err = trustedSSHWaitResult([]string{remote.SSHWaitTimeoutShellBusyNotice}, remote.SessionRunning, nil, "")
	if err == nil || !strings.Contains(err.Error(), "trusted_ssh_outcome_unobserved") {
		t.Fatalf("busy shell = %v", err)
	}
	_, err = trustedSSHWaitResult([]string{"partial", "[maclaw] 命令执行超时，已发送 Ctrl+C 中断"}, remote.SessionRunning, context.DeadlineExceeded, "")
	if err == nil || !strings.Contains(err.Error(), "trusted_ssh_timeout") || strings.Contains(err.Error(), "outcome_unobserved") {
		t.Fatalf("recovered timeout = %v", err)
	}
	_, err = trustedSSHWaitResult([]string{"uptime", "partial", "[maclaw] 命令执行超时，已发送 Ctrl+C 中断"}, remote.SessionRunning, context.DeadlineExceeded, "uptime")
	if err == nil || !strings.Contains(err.Error(), "partial") || strings.Contains(err.Error(), "uptime") {
		t.Fatalf("timeout echo = %v", err)
	}
	_, err = trustedSSHWaitResult(nil, remote.SessionError, context.DeadlineExceeded, "")
	if err == nil || !strings.Contains(err.Error(), "trusted_ssh_outcome_unobserved") || strings.Contains(err.Error(), "trusted_ssh_timeout") {
		t.Fatalf("dead session at the deadline = %v", err)
	}
}

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
func TestSSHTimeoutIsSettledFailure(t *testing.T) {
	got := sshAdapterResult(t, fmt.Errorf("trusted_ssh_timeout"))
	if !strings.HasPrefix(got, "[system rejected]") || strings.HasPrefix(got, "[system unknown]") {
		t.Fatalf("timeout = %q, want a settled rejection so the next command stays available", got)
	}
}

// No bound session means the command was never written. Classifying that as
// unknown froze the selection as if the command might have run, so a later
// connect could not close it.
func TestSSHMissingSessionBeforeWriteStaysRetractable(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	callbacks := &sharedAgentLoopCallbacks{handler: h, userID: "user-1"}
	got := callbacks.executeTrustedSSH(tool.PlannedSelection{}, tool.CanonicalRequest{
		CanonicalJSON: []byte(`{"command":"echo hi"}`),
	})
	if !tool.ArgumentRejectionWithoutEffect(tool.SelectionExecutionResult{Result: got}) || strings.HasPrefix(got, "[system unknown]") || !strings.Contains(got, "trusted_ssh_session_unavailable") {
		t.Fatalf("missing session before write = %q", got)
	}
}

// A raw ssh line is a policy denial. It must stay a recorded rejection, once,
// so the same bypass is not offered again as a fresh call.
func TestSSHRawCommandStaysRecordedRejection(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	callbacks := &sharedAgentLoopCallbacks{handler: h, userID: "user-1"}
	got := callbacks.executeTrustedSSH(tool.PlannedSelection{}, tool.CanonicalRequest{
		CanonicalJSON: []byte(`{"command":"ssh user@host uptime"}`),
	})
	want, tripped := tool.RejectRawSSHCommand("ssh user@host uptime")
	if !tripped || got != want || tool.ArgumentRejectionWithoutEffect(tool.SelectionExecutionResult{Result: got}) {
		t.Fatalf("raw ssh = %q", got)
	}
}

func TestRecordedSSHRefusalAfterEffectDropsTheUnexecutedMarker(t *testing.T) {
	got := recordedSSHRefusalAfterEffect("[system rejected] arguments_rejected: trusted_ssh_remote_path_required")
	if tool.ArgumentRejectionWithoutEffect(tool.SelectionExecutionResult{Result: got}) || !strings.Contains(got, "trusted_ssh_remote_path_required") {
		t.Fatalf("post-effect refusal = %q", got)
	}
}

func TestSSHLostSessionNamesAllStillReportUnknown(t *testing.T) {
	for _, name := range []string{
		"trusted_ssh_session_disconnected",
		"trusted_ssh_session_unavailable",
	} {
		t.Run(name, func(t *testing.T) {
			got := sshAdapterResult(t, fmt.Errorf("%s", name))
			if !strings.HasPrefix(got, "[system unknown]") {
				t.Fatalf("%s = %q, want an unknown outcome", name, got)
			}
		})
	}
}
