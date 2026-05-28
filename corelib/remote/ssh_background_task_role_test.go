package remote

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeSSHBackgroundTaskRoleDelegatesSharedClassifier(t *testing.T) {
	if got := normalizeSSHBackgroundTaskRole("", "sleep 60 && tail -20 /tmp/build.log"); got != "poll" {
		t.Fatalf("role = %q, want poll", got)
	}
}

func TestSSHBackgroundTaskManagerListTasksReturnsSnapshots(t *testing.T) {
	mgr := NewSSHBackgroundTaskManager(nil)
	startedAt := time.Now().Add(-time.Minute)
	mgr.tasks["bg_1"] = &SSHBackgroundTask{
		TaskID:    "bg_1",
		SessionID: "ssh_1",
		Command:   "docker build .",
		TaskRole:  "command",
		Status:    SSHBackgroundTaskStatusRunning,
		PID:       "123",
		StartedAt: startedAt,
	}

	tasks := mgr.ListTasks()
	if len(tasks) != 1 {
		t.Fatalf("ListTasks returned %d tasks, want 1", len(tasks))
	}
	tasks[0].Status = SSHBackgroundTaskStatusKilled

	again := mgr.ListTasks()
	if again[0].Status != SSHBackgroundTaskStatusRunning {
		t.Fatalf("ListTasks exposed mutable task pointer, status = %q", again[0].Status)
	}
	if !again[0].StartedAt.Equal(startedAt) || again[0].TaskRole != "command" {
		t.Fatalf("snapshot lost task metadata: %#v", again[0])
	}
}

func TestSSHBackgroundTaskStatusActive(t *testing.T) {
	for _, status := range []SSHBackgroundTaskStatus{SSHBackgroundTaskStatusPending, SSHBackgroundTaskStatusRunning} {
		if !status.IsActive() {
			t.Fatalf("%q should be active", status)
		}
	}
	for _, status := range []SSHBackgroundTaskStatus{SSHBackgroundTaskStatusCompleted, SSHBackgroundTaskStatusFailed, SSHBackgroundTaskStatusKilled, SSHBackgroundTaskStatusUnknown, ""} {
		if status.IsActive() {
			t.Fatalf("%q should not be active", status)
		}
	}
}

func TestSSHBackgroundTaskManagerRefreshTaskStatusAsyncRejectsUnavailableManager(t *testing.T) {
	mgr := NewSSHBackgroundTaskManager(nil)
	mgr.tasks["bg_1"] = &SSHBackgroundTask{TaskID: "bg_1", Status: SSHBackgroundTaskStatusRunning}
	if mgr.RefreshTaskStatusAsync("bg_1", 5) {
		t.Fatal("refresh without SSH manager should be rejected")
	}
	if mgr.RefreshTaskStatusAsync("missing", 5) {
		t.Fatal("refresh for missing task should be rejected")
	}
}

func TestSSHBackgroundTaskManagerRefreshTaskStatusAsyncMarksAttempt(t *testing.T) {
	mgr := NewSSHBackgroundTaskManager(NewSSHSessionManager(nil))
	mgr.tasks["bg_1"] = &SSHBackgroundTask{TaskID: "bg_1", SessionID: "missing", Status: SSHBackgroundTaskStatusRunning}
	if !mgr.RefreshTaskStatusAsync(" bg_1 ", 5) {
		t.Fatal("first refresh should be scheduled")
	}
	tasks := mgr.ListTasks()
	if len(tasks) != 1 || tasks[0].LastCheck.IsZero() {
		t.Fatalf("scheduled refresh should mark LastCheck, got %#v", tasks)
	}
}

func TestSSHBackgroundTaskManagerRefreshTaskStatusAsyncDeduplicatesInFlight(t *testing.T) {
	mgr := NewSSHBackgroundTaskManager(NewSSHSessionManager(nil))
	mgr.tasks["bg_1"] = &SSHBackgroundTask{TaskID: "bg_1", SessionID: "missing", Status: SSHBackgroundTaskStatusRunning}
	mgr.refreshing["bg_1"] = struct{}{}
	if mgr.RefreshTaskStatusAsync("bg_1", 5) {
		t.Fatal("in-flight refresh should be deduplicated")
	}
}

func TestBuildWriteBackgroundScriptCommandAvoidsHeredocDelimiterCollision(t *testing.T) {
	script := "echo before\nMACLAW_SCRIPT_EOF\nMACLAW_SCRIPT_EOF_1\necho 'after'\n"
	cmd := buildWriteBackgroundScriptCommand("/tmp/maclaw_bg_test.sh", script)

	if !strings.Contains(cmd, "<< 'MACLAW_SCRIPT_EOF_2'") {
		t.Fatalf("command should choose a non-colliding heredoc delimiter: %q", cmd)
	}
	if got := strings.Count(cmd, "\nMACLAW_SCRIPT_EOF_2\n"); got != 1 {
		t.Fatalf("command should contain exactly one closing delimiter line, count = %d, cmd = %q", got, cmd)
	}
	if !strings.Contains(cmd, "echo 'after'") {
		t.Fatalf("command should preserve script content without shell quoting changes: %q", cmd)
	}
}
