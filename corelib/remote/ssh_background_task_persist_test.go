package remote

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSetPersistDir_SavesAndLoadsActiveTasks(t *testing.T) {
	dir := t.TempDir()
	mgr := NewSSHBackgroundTaskManager(NewSSHSessionManager(nil))

	// Add a running task manually (simulate Submit without actual SSH)
	mgr.tasks["bg_111_1"] = &SSHBackgroundTask{
		TaskID:    "bg_111_1",
		OwnerID:   "owner-a",
		SessionID: "ssh_root@api.example.com:22_1",
		Command:   "pip install torch",
		LogFile:   "/tmp/maclaw_bg_bg_111_1.log",
		PIDFile:   "/tmp/maclaw_bg_bg_111_1.pid",
		Status:    SSHBackgroundTaskStatusRunning,
		PID:       "12345",
		StartedAt: time.Now().Add(-5 * time.Minute),
	}
	// Add a completed task (should not be restored)
	mgr.tasks["bg_111_2"] = &SSHBackgroundTask{
		TaskID:    "bg_111_2",
		SessionID: "ssh_root@api.example.com:22_1",
		Command:   "echo done",
		LogFile:   "/tmp/maclaw_bg_bg_111_2.log",
		PIDFile:   "/tmp/maclaw_bg_bg_111_2.pid",
		Status:    SSHBackgroundTaskStatusCompleted,
		PID:       "12346",
		StartedAt: time.Now().Add(-1 * time.Hour),
	}

	// Trigger persist
	mgr.persistDir = dir
	mgr.saveToDisk()

	// Verify file was written
	path := filepath.Join(dir, "ssh_bg_tasks.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("persist file not created: %v", err)
	}

	// Create a new manager and load from disk
	mgr2 := NewSSHBackgroundTaskManager(NewSSHSessionManager(nil))
	mgr2.SetMirrorDir(filepath.Join(dir, "mirrors"))
	mgr2.SetPersistDir(dir)

	// Only the running task should be restored
	if len(mgr2.tasks) != 1 {
		t.Fatalf("expected 1 restored task, got %d", len(mgr2.tasks))
	}
	restored, ok := mgr2.tasks["bg_111_1"]
	if !ok {
		t.Fatal("running task bg_111_1 was not restored")
	}
	if restored.Command != "pip install torch" {
		t.Fatalf("restored command = %q, want 'pip install torch'", restored.Command)
	}
	if restored.PID != "12345" {
		t.Fatalf("restored PID = %q, want '12345'", restored.PID)
	}
	if restored.Status != SSHBackgroundTaskStatusRunning {
		t.Fatalf("restored status = %q, want 'running'", restored.Status)
	}
	if restored.OwnerID != "owner-a" {
		t.Fatalf("restored owner = %q, want owner-a", restored.OwnerID)
	}
	if restored.MirrorFile != filepath.Join(dir, "mirrors", "bg_111_1.log") {
		t.Fatalf("restored mirror = %q", restored.MirrorFile)
	}
}

func TestSetPersistDir_DoesNotOverwriteExistingInMemoryTask(t *testing.T) {
	dir := t.TempDir()

	// Pre-write a persisted file with a task
	reg := persistedRegistry{
		Tasks: []persistedTask{
			{
				TaskID:    "bg_222_1",
				SessionID: "ssh_user@host:22_1",
				Command:   "old command from disk",
				Status:    SSHBackgroundTaskStatusRunning,
				PID:       "99999",
				StartedAt: time.Now().Add(-10 * time.Minute),
			},
		},
		UpdatedAt: time.Now(),
	}
	data, _ := json.MarshalIndent(reg, "", "  ")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "ssh_bg_tasks.json"), data, 0o644)

	// Create a manager with a task already in memory using same ID
	mgr := NewSSHBackgroundTaskManager(NewSSHSessionManager(nil))
	mgr.tasks["bg_222_1"] = &SSHBackgroundTask{
		TaskID:  "bg_222_1",
		Command: "fresh in-memory command",
		Status:  SSHBackgroundTaskStatusRunning,
	}

	// Load from disk should NOT overwrite the in-memory task
	mgr.SetPersistDir(dir)

	task := mgr.tasks["bg_222_1"]
	if task.Command != "fresh in-memory command" {
		t.Fatalf("in-memory task was overwritten by disk, command = %q", task.Command)
	}
}

func TestExtractHostIDFromSessionID(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"ssh_root@api.example.com:22_1", "root@api.example.com:22"},
		{"ssh_root@api.example.com:22_15", "root@api.example.com:22"},
		{"ssh_user@192.168.1.1:2222_3", "user@192.168.1.1:2222"},
		{"custom_session", "custom_session"}, // no ssh_ prefix
		{"ssh_root@host:22", "root@host:22"}, // no counter suffix
	}
	for _, tc := range cases {
		got := extractHostIDFromSessionID(tc.input)
		if got != tc.want {
			t.Errorf("extractHostIDFromSessionID(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestSignalPersist_NoPersistDirIsNoop(t *testing.T) {
	mgr := NewSSHBackgroundTaskManager(NewSSHSessionManager(nil))
	// Should not panic or block when persistDir is empty
	mgr.signalPersist()
}

func TestSaveToDisk_ExpiresOldNonActiveTasks(t *testing.T) {
	dir := t.TempDir()
	mgr := NewSSHBackgroundTaskManager(NewSSHSessionManager(nil))
	mgr.persistDir = dir

	// Add an old completed task (>24h)
	mgr.tasks["bg_old_1"] = &SSHBackgroundTask{
		TaskID:    "bg_old_1",
		Command:   "ancient task",
		Status:    SSHBackgroundTaskStatusCompleted,
		StartedAt: time.Now().Add(-48 * time.Hour),
		LastCheck: time.Now().Add(-48 * time.Hour),
	}
	// Add a recent completed task (<24h)
	mgr.tasks["bg_recent_1"] = &SSHBackgroundTask{
		TaskID:    "bg_recent_1",
		Command:   "recent task",
		Status:    SSHBackgroundTaskStatusCompleted,
		StartedAt: time.Now().Add(-1 * time.Hour),
	}
	// Add a running task
	mgr.tasks["bg_run_1"] = &SSHBackgroundTask{
		TaskID:    "bg_run_1",
		Command:   "running task",
		Status:    SSHBackgroundTaskStatusRunning,
		StartedAt: time.Now().Add(-30 * time.Hour),
	}

	mgr.saveToDisk()

	// Read the persisted file and check
	data, err := os.ReadFile(filepath.Join(dir, "ssh_bg_tasks.json"))
	if err != nil {
		t.Fatalf("read persist file: %v", err)
	}
	var reg persistedRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Old completed task should NOT be persisted; recent completed and running should be
	taskIDs := make(map[string]bool)
	for _, pt := range reg.Tasks {
		taskIDs[pt.TaskID] = true
	}
	if taskIDs["bg_old_1"] {
		t.Error("old completed task should not be persisted")
	}
	if !taskIDs["bg_recent_1"] {
		t.Error("recent completed task should be persisted")
	}
	if !taskIDs["bg_run_1"] {
		t.Error("running task should always be persisted")
	}
}

func TestFindAlternateSession_MatchesSameHost(t *testing.T) {
	sshMgr := NewSSHSessionManager(nil)
	mgr := NewSSHBackgroundTaskManager(sshMgr)

	// Manually inject a session into the manager (simulates a connected session)
	sshMgr.mu.Lock()
	sshMgr.sessions["ssh_root@api.example.com:22_2"] = &SSHManagedSession{
		ID:     "ssh_root@api.example.com:22_2",
		Status: SessionRunning,
		Summary: SSHSessionSummary{
			SessionID: "ssh_root@api.example.com:22_2",
			HostID:    "root@api.example.com:22",
			Status:    string(SessionRunning),
		},
	}
	sshMgr.mu.Unlock()

	// Task with old session ID pointing to same host
	task := &SSHBackgroundTask{
		TaskID:    "bg_test_1",
		SessionID: "ssh_root@api.example.com:22_1", // old session, no longer exists
		PID:       "12345",
	}

	alt := mgr.findAlternateSession(task)
	if alt == nil {
		t.Fatal("findAlternateSession should find the session on same host")
	}
	if alt.ID != "ssh_root@api.example.com:22_2" {
		t.Fatalf("found session ID = %q, want ssh_root@api.example.com:22_2", alt.ID)
	}
}

func TestFindAlternateSession_NoMatchDifferentHost(t *testing.T) {
	sshMgr := NewSSHSessionManager(nil)
	mgr := NewSSHBackgroundTaskManager(sshMgr)

	// Session on a different host
	sshMgr.mu.Lock()
	sshMgr.sessions["ssh_user@other.host:22_1"] = &SSHManagedSession{
		ID:     "ssh_user@other.host:22_1",
		Status: SessionRunning,
		Summary: SSHSessionSummary{
			SessionID: "ssh_user@other.host:22_1",
			HostID:    "user@other.host:22",
			Status:    string(SessionRunning),
		},
	}
	// Add a second session so "single running session" fallback doesn't trigger
	sshMgr.sessions["ssh_admin@third.host:22_1"] = &SSHManagedSession{
		ID:     "ssh_admin@third.host:22_1",
		Status: SessionRunning,
		Summary: SSHSessionSummary{
			SessionID: "ssh_admin@third.host:22_1",
			HostID:    "admin@third.host:22",
			Status:    string(SessionRunning),
		},
	}
	sshMgr.mu.Unlock()

	// Task for a host that has no running session
	task := &SSHBackgroundTask{
		TaskID:    "bg_test_2",
		SessionID: "ssh_root@api.example.com:22_1",
		PID:       "99999",
	}

	alt := mgr.findAlternateSession(task)
	if alt != nil {
		t.Fatalf("findAlternateSession should return nil when multiple non-matching sessions exist, got %s", alt.ID)
	}
}

func TestFindAlternateSession_SingleSessionFallback(t *testing.T) {
	sshMgr := NewSSHSessionManager(nil)
	mgr := NewSSHBackgroundTaskManager(sshMgr)

	// Only one running session, different host — should be used as fallback
	sshMgr.mu.Lock()
	sshMgr.sessions["ssh_user@only.host:22_1"] = &SSHManagedSession{
		ID:     "ssh_user@only.host:22_1",
		Status: SessionRunning,
		Summary: SSHSessionSummary{
			SessionID: "ssh_user@only.host:22_1",
			HostID:    "user@only.host:22",
			Status:    string(SessionRunning),
		},
	}
	sshMgr.mu.Unlock()

	task := &SSHBackgroundTask{
		TaskID:    "bg_test_3",
		SessionID: "ssh_root@api.example.com:22_1",
		PID:       "55555",
	}

	alt := mgr.findAlternateSession(task)
	if alt == nil {
		t.Fatal("findAlternateSession should use single-session fallback")
	}
	if alt.ID != "ssh_user@only.host:22_1" {
		t.Fatalf("fallback session ID = %q, want ssh_user@only.host:22_1", alt.ID)
	}
}

func TestLoadPersistedTasks_CorruptedFileDoesNotCrash(t *testing.T) {
	dir := t.TempDir()
	// Write corrupted JSON
	os.WriteFile(filepath.Join(dir, "ssh_bg_tasks.json"), []byte(`{"tasks": [INVALID`), 0o644)

	mgr := NewSSHBackgroundTaskManager(NewSSHSessionManager(nil))
	// Should not panic, just log and return empty
	mgr.SetPersistDir(dir)

	if len(mgr.tasks) != 0 {
		t.Fatalf("corrupted file should result in empty tasks, got %d", len(mgr.tasks))
	}
}

func TestLoadPersistedTasks_NullTasksArray(t *testing.T) {
	dir := t.TempDir()
	// Write valid JSON with null tasks
	os.WriteFile(filepath.Join(dir, "ssh_bg_tasks.json"), []byte(`{"tasks": null, "updated_at": "2026-01-01T00:00:00Z"}`), 0o644)

	mgr := NewSSHBackgroundTaskManager(NewSSHSessionManager(nil))
	mgr.SetPersistDir(dir)

	if len(mgr.tasks) != 0 {
		t.Fatalf("null tasks should result in empty map, got %d", len(mgr.tasks))
	}
}

func TestFindDuplicateActiveTask_ExactMatch(t *testing.T) {
	mgr := NewSSHBackgroundTaskManager(NewSSHSessionManager(nil))
	mgr.tasks["bg_1"] = &SSHBackgroundTask{
		TaskID:  "bg_1",
		Command: "docker pull registry.example.com/app:latest",
		Status:  SSHBackgroundTaskStatusRunning,
	}

	dup := mgr.findDuplicateActiveTask("docker pull registry.example.com/app:latest")
	if dup == nil {
		t.Fatal("should find exact duplicate")
	}
	if dup.TaskID != "bg_1" {
		t.Fatalf("duplicate task ID = %q, want bg_1", dup.TaskID)
	}
}

func TestFindDuplicateActiveTask_CompletedNotMatched(t *testing.T) {
	mgr := NewSSHBackgroundTaskManager(NewSSHSessionManager(nil))
	mgr.tasks["bg_1"] = &SSHBackgroundTask{
		TaskID:  "bg_1",
		Command: "pip install torch",
		Status:  SSHBackgroundTaskStatusCompleted, // not active
	}

	dup := mgr.findDuplicateActiveTask("pip install torch")
	if dup != nil {
		t.Fatal("completed task should not be considered a duplicate")
	}
}

func TestFindDuplicateActiveTask_NormalizedMatch(t *testing.T) {
	mgr := NewSSHBackgroundTaskManager(NewSSHSessionManager(nil))
	// Task stored with sudo fallback wrapper
	mgr.tasks["bg_1"] = &SSHBackgroundTask{
		TaskID:  "bg_1",
		Command: "echo '[maclaw] 已降级; sudo token 获取失败 (无密码)，已自动降级' && apt-get update",
		Status:  SSHBackgroundTaskStatusRunning,
	}

	// LLM submits the bare command (without wrapper)
	dup := mgr.findDuplicateActiveTask("apt-get update")
	if dup == nil {
		t.Fatal("should match after normalization strips echo wrapper")
	}
}

func TestFindDuplicateActiveTask_DifferentCommand(t *testing.T) {
	mgr := NewSSHBackgroundTaskManager(NewSSHSessionManager(nil))
	mgr.tasks["bg_1"] = &SSHBackgroundTask{
		TaskID:  "bg_1",
		Command: "git clone https://github.com/user/repo.git",
		Status:  SSHBackgroundTaskStatusRunning,
	}

	dup := mgr.findDuplicateActiveTask("pip install torch")
	if dup != nil {
		t.Fatal("different commands should not match")
	}
}

func TestNormalizeCommandForDedup(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"pip install torch", "pip install torch"},
		{"sudo pip install torch", "pip install torch"},
		{"sudo -n pip install torch", "pip install torch"},
		{"echo '[maclaw] hint' && apt-get update", "apt-get update"},
		{"", ""},
		{"  sudo -n   docker pull img  ", "docker pull img"},
		// Environment variable prefix stripping
		{"SSHPASS='secret' sshpass -e ssh root@host", "sshpass -e ssh root@host"},
		{"SSHPASS='C-?9Z0Q?3rpFq%6f' timeout 60 sshpass -e ssh -o Pref=password root@host", "sshpass -e ssh -o Pref=password root@host"},
		// Timeout prefix stripping
		{"timeout 120 git clone https://github.com/user/repo", "git clone https://github.com/user/repo"},
		{"timeout 15 scp file.tar user@host:/tmp/", "scp file.tar user@host:/tmp/"},
		// Combined: env + timeout + sudo
		{"MY_VAR=123 timeout 30 sudo -n docker pull img:latest", "docker pull img:latest"},
	}
	for _, tc := range cases {
		got := normalizeCommandForDedup(tc.input)
		if got != tc.want {
			t.Errorf("normalizeCommandForDedup(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
