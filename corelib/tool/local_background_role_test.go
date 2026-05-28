package tool

import (
	"testing"
	"time"
)

func TestNormalizeBackgroundTaskRoleDelegatesSharedClassifier(t *testing.T) {
	if got := NormalizeBackgroundTaskRole("", "sleep 60 && tail -20 /tmp/build.log"); got != "poll" {
		t.Fatalf("role = %q, want poll", got)
	}
}

func TestLocalBackgroundTaskManagerListReturnsSnapshots(t *testing.T) {
	mgr := NewLocalBackgroundTaskManager(t.TempDir())
	startedAt := time.Now().Add(-time.Minute)
	mgr.tasks["local_1"] = &LocalBackgroundTask{
		TaskID:    "local_1",
		Command:   "sleep 60 && tail build.log",
		TaskRole:  "poll",
		Status:    LocalBackgroundTaskStatusRunning,
		PID:       123,
		StartedAt: startedAt,
		ExitCode:  -1,
	}

	tasks := mgr.List()
	if len(tasks) != 1 {
		t.Fatalf("List returned %d tasks, want 1", len(tasks))
	}
	tasks[0].Status = LocalBackgroundTaskStatusKilled

	again := mgr.List()
	if again[0].Status != LocalBackgroundTaskStatusRunning {
		t.Fatalf("List exposed mutable task pointer, status = %q", again[0].Status)
	}
	if !again[0].StartedAt.Equal(startedAt) || again[0].TaskRole != "poll" {
		t.Fatalf("snapshot lost task metadata: %#v", again[0])
	}
}
