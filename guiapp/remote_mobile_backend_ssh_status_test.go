package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestMobileBackendSSHStatusProjection(t *testing.T) {
	session := &mobileBackendSSHSession{Status: "connected"}
	normalizeMobileBackendSSHStatus(session)
	if session.RuntimeStatus != agentruntime.JobStatusUnknown {
		t.Fatalf("session runtime status=%q, want unknown", session.RuntimeStatus)
	}
	task := &mobileBackendSSHTask{Status: "running"}
	normalizeMobileBackendSSHTaskStatus(task)
	if task.RuntimeStatus != agentruntime.JobStatusRunning {
		t.Fatalf("task runtime status=%q", task.RuntimeStatus)
	}
	operation := &mobileBackendSSHFileOperation{Status: "completed"}
	normalizeMobileBackendSSHFileStatus(operation)
	if operation.RuntimeStatus != agentruntime.JobStatusSucceeded {
		t.Fatalf("file runtime status=%q", operation.RuntimeStatus)
	}
}
