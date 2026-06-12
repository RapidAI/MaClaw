package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseBackgroundTaskExitCode(t *testing.T) {
	if got := parseBackgroundTaskExitCode("START\n---\nEXIT: 5\n"); got != 5 {
		t.Fatalf("exit code = %d, want 5", got)
	}
	if got := parseBackgroundTaskExitCode("EXIT: 0\n"); got != 0 {
		t.Fatalf("exit code = %d, want 0", got)
	}
	if got := parseBackgroundTaskExitCode("no exit marker"); got != 0 {
		t.Fatalf("exit code without marker = %d, want 0", got)
	}
	if _, ok := parseBackgroundTaskExitCodeKnown("no exit marker"); ok {
		t.Fatal("missing exit marker should not be known")
	}
	if code, ok := parseBackgroundTaskExitCodeKnown("EXIT: 0\n"); !ok || code != 0 {
		t.Fatalf("known exit code = %d/%v, want 0/true", code, ok)
	}
}

func TestSSHBackgroundTaskManagerWriteTaskMirror(t *testing.T) {
	dir := t.TempDir()
	mirrorFile := filepath.Join(dir, "bg_1.log")
	mgr := NewSSHBackgroundTaskManager(nil)
	mgr.writeTaskMirror(&BackgroundTaskStatus{
		TaskID:        "bg_1",
		OwnerID:       "owner-a",
		Command:       "python train.py",
		Status:        SSHBackgroundTaskStatusCompleted,
		ExitCode:      0,
		ExitCodeKnown: true,
		StartedAt:     time.Date(2026, 6, 12, 1, 2, 3, 0, time.UTC),
		Elapsed:       "3m",
		MirrorFile:    mirrorFile,
		LogTail:       "done\nEXIT: 0\n",
	})

	data, err := os.ReadFile(mirrorFile)
	if err != nil {
		t.Fatalf("read mirror: %v", err)
	}
	text := string(data)
	for _, want := range []string{"task_id: bg_1", "owner_id: owner-a", "status: completed", "exit_code: 0", "python train.py", "done"} {
		if !strings.Contains(text, want) {
			t.Fatalf("mirror missing %q in %q", want, text)
		}
	}
}
