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
		OwnerID:   "owner-a",
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
	if !again[0].StartedAt.Equal(startedAt) || again[0].TaskRole != "command" || again[0].OwnerID != "owner-a" {
		t.Fatalf("snapshot lost task metadata: %#v", again[0])
	}
}

func TestSSHBackgroundTaskManagerDuplicateDetectionRespectsOwner(t *testing.T) {
	mgr := NewSSHBackgroundTaskManager(nil)
	mgr.tasks["bg_owner_a"] = &SSHBackgroundTask{
		TaskID:  "bg_owner_a",
		OwnerID: "owner-a",
		Command: "docker build .",
		Status:  SSHBackgroundTaskStatusRunning,
	}

	if got := mgr.findDuplicateActiveTaskForOwner("docker build .", "owner-a"); got == nil || got.TaskID != "bg_owner_a" {
		t.Fatalf("same owner should match duplicate, got %#v", got)
	}
	if got := mgr.findDuplicateActiveTaskForOwner("docker build .", "owner-b"); got != nil {
		t.Fatalf("different owner should not match duplicate, got %#v", got)
	}
	if got := mgr.findDuplicateActiveTaskForOwner("docker build .", ""); got == nil || got.TaskID != "bg_owner_a" {
		t.Fatalf("legacy empty owner should see existing duplicate, got %#v", got)
	}
}

func TestSSHBackgroundTaskManagerOwnerAuthorization(t *testing.T) {
	mgr := NewSSHBackgroundTaskManager(nil)
	mgr.tasks["bg_owner_a"] = &SSHBackgroundTask{
		TaskID:  "bg_owner_a",
		OwnerID: "owner-a",
		Command: "docker build .",
		Status:  SSHBackgroundTaskStatusRunning,
	}
	mgr.tasks["bg_legacy"] = &SSHBackgroundTask{
		TaskID:  "bg_legacy",
		Command: "make test",
		Status:  SSHBackgroundTaskStatusRunning,
	}

	if err := mgr.AuthorizeTaskOwner("bg_owner_a", "owner-a"); err != nil {
		t.Fatalf("same owner should be allowed: %v", err)
	}
	if err := mgr.AuthorizeTaskOwner("bg_owner_a", "owner-b"); err == nil || !strings.Contains(err.Error(), "another runtime owner") {
		t.Fatalf("different owner should be rejected, got %v", err)
	}
	if err := mgr.AuthorizeTaskOwner("bg_owner_a", ""); err != nil {
		t.Fatalf("empty owner should preserve legacy access: %v", err)
	}
	if err := mgr.AuthorizeTaskOwner("bg_legacy", "owner-b"); err != nil {
		t.Fatalf("ownerless legacy task should remain accessible: %v", err)
	}
	if err := mgr.AuthorizeTaskOwner("missing", "owner-a"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing task should fail, got %v", err)
	}
}

func TestSSHBackgroundTaskManagerListTasksForOwnerFiltersAndKeepsLegacy(t *testing.T) {
	mgr := NewSSHBackgroundTaskManager(nil)
	mgr.tasks["bg_owner_a"] = &SSHBackgroundTask{TaskID: "bg_owner_a", OwnerID: "owner-a", Status: SSHBackgroundTaskStatusRunning}
	mgr.tasks["bg_owner_b"] = &SSHBackgroundTask{TaskID: "bg_owner_b", OwnerID: "owner-b", Status: SSHBackgroundTaskStatusRunning}
	mgr.tasks["bg_legacy"] = &SSHBackgroundTask{TaskID: "bg_legacy", Status: SSHBackgroundTaskStatusRunning}

	tasks := mgr.ListTasksForOwner("owner-a")
	ids := map[string]bool{}
	for _, task := range tasks {
		ids[task.TaskID] = true
	}
	if !ids["bg_owner_a"] || !ids["bg_legacy"] || ids["bg_owner_b"] || len(tasks) != 2 {
		t.Fatalf("filtered tasks = %#v", tasks)
	}
	if got := mgr.ListTasksForOwner(""); len(got) != 3 {
		t.Fatalf("empty owner should see all legacy tasks, got %d", len(got))
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

func TestSSHBackgroundTaskManagerRefreshTaskStatusAsyncForOwnerRejectsDifferentOwner(t *testing.T) {
	mgr := NewSSHBackgroundTaskManager(NewSSHSessionManager(nil))
	mgr.tasks["bg_1"] = &SSHBackgroundTask{TaskID: "bg_1", OwnerID: "owner-a", SessionID: "missing", Status: SSHBackgroundTaskStatusRunning}

	if mgr.RefreshTaskStatusAsyncForOwner("bg_1", 5, "owner-b") {
		t.Fatal("refresh should reject different owner")
	}
	if !mgr.RefreshTaskStatusAsyncForOwner("bg_1", 5, "owner-a") {
		t.Fatal("refresh should allow matching owner")
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
