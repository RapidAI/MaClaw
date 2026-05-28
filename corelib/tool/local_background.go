package tool

// LocalBackgroundTaskManager manages long-running processes on the local
// machine. It mirrors the SSH BackgroundTaskManager pattern:
//
//   Submit(command) → task_id + log_file + PID  (non-blocking)
//   Check(task_id)  → status + log tail          (non-blocking)
//   Wait(task_id, timeout) → blocks until done    (blocking)
//   Kill(task_id)   → terminate process           (non-blocking)
//   List()          → all tasks                   (non-blocking)
//
// This replaces the "open" fire-and-forget pattern and the "bash" blocking
// pattern for long-running commands. The agent can submit a task, do other
// work, and check back later — or wait with a timeout.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/backgroundrole"
)

// LocalBackgroundTask represents a process running in the background.
type LocalBackgroundTask struct {
	mu        sync.Mutex
	TaskID    string                    `json:"task_id"`
	Command   string                    `json:"command"`
	TaskRole  string                    `json:"task_role,omitempty"`
	WorkDir   string                    `json:"work_dir,omitempty"`
	LogFile   string                    `json:"log_file"`
	PID       int                       `json:"pid"`
	Status    LocalBackgroundTaskStatus `json:"status"` // running, completed, failed, killed
	ExitCode  int                       `json:"exit_code"`
	StartedAt time.Time                 `json:"started_at"`
	EndedAt   time.Time                 `json:"ended_at,omitempty"`

	cmd    *exec.Cmd
	cancel context.CancelFunc
	doneC  chan struct{} // closed when process exits
}

// Lock acquires the task mutex.
func (t *LocalBackgroundTask) Lock() { t.mu.Lock() }

// Unlock releases the task mutex.
func (t *LocalBackgroundTask) Unlock() { t.mu.Unlock() }

// LocalBackgroundTaskManager manages local background tasks.
type LocalBackgroundTaskManager struct {
	mu      sync.RWMutex
	tasks   map[string]*LocalBackgroundTask
	counter atomic.Int64
	logDir  string // directory for task log files
}

// NewLocalBackgroundTaskManager creates a new manager.
// logDir is the directory where task log files are stored.
func NewLocalBackgroundTaskManager(logDir string) *LocalBackgroundTaskManager {
	if logDir == "" {
		logDir = filepath.Join(os.TempDir(), "maclaw_bg_tasks")
	}
	_ = os.MkdirAll(logDir, 0755)
	return &LocalBackgroundTaskManager{
		tasks:  make(map[string]*LocalBackgroundTask),
		logDir: logDir,
	}
}

// Submit starts a command in the background and returns immediately.
// The command's stdout/stderr are redirected to a log file.
// Returns the task with PID and log file path.
func (m *LocalBackgroundTaskManager) Submit(command, workDir string) (*LocalBackgroundTask, error) {
	return m.SubmitWithRole(command, workDir, "")
}

func (m *LocalBackgroundTaskManager) SubmitWithRole(command, workDir, role string) (*LocalBackgroundTask, error) {
	if command == "" {
		return nil, fmt.Errorf("command is empty")
	}

	if rejection, rejected := RejectRawSSHCommand(command); rejected {
		return nil, fmt.Errorf("%s", rejection)
	}
	role = NormalizeBackgroundTaskRole(role, command)

	seq := m.counter.Add(1)
	taskID := fmt.Sprintf("local_%d_%d", time.Now().Unix(), seq)

	logFile := filepath.Join(m.logDir, taskID+".log")

	// Create log file
	lf, err := os.Create(logFile)
	if err != nil {
		return nil, fmt.Errorf("create log file: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	var shellName string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		shellName = "powershell"
		shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command",
			"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " + command}
	} else {
		shellName = "bash"
		shellArgs = []string{"-c", command}
	}

	cmd := CommandContext(ctx, shellName, shellArgs...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	cmd.Env = AppendUTF8Env(os.Environ())
	cmd.Stdout = lf
	cmd.Stderr = lf

	if err := cmd.Start(); err != nil {
		lf.Close()
		cancel()
		return nil, fmt.Errorf("start command: %w", err)
	}

	task := &LocalBackgroundTask{
		TaskID:    taskID,
		Command:   command,
		TaskRole:  role,
		WorkDir:   workDir,
		LogFile:   logFile,
		PID:       cmd.Process.Pid,
		Status:    LocalBackgroundTaskStatusRunning,
		ExitCode:  -1,
		StartedAt: time.Now(),
		cmd:       cmd,
		cancel:    cancel,
		doneC:     make(chan struct{}),
	}

	// Monitor process exit in background goroutine
	go func() {
		err := cmd.Wait()
		lf.Close()

		task.mu.Lock()
		task.EndedAt = time.Now()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				task.ExitCode = exitErr.ExitCode()
			}
			if ctx.Err() != nil {
				task.Status = LocalBackgroundTaskStatusKilled
			} else {
				task.Status = LocalBackgroundTaskStatusFailed
			}
		} else {
			task.ExitCode = 0
			task.Status = LocalBackgroundTaskStatusCompleted
		}
		task.mu.Unlock()
		close(task.doneC)
	}()

	m.mu.Lock()
	m.tasks[taskID] = task
	m.mu.Unlock()

	return task, nil
}

// LocalTaskStatus is the result of Check or Wait.
type LocalTaskStatus struct {
	TaskID   string                    `json:"task_id"`
	Command  string                    `json:"command"`
	TaskRole string                    `json:"task_role,omitempty"`
	Status   LocalBackgroundTaskStatus `json:"status"`
	PID      int                       `json:"pid"`
	ExitCode int                       `json:"exit_code"`
	Elapsed  string                    `json:"elapsed"`
	LogTail  string                    `json:"log_tail"`
	LogSize  int64                     `json:"log_size"`
}

// Check returns the current status and log tail of a task without blocking.
func (m *LocalBackgroundTaskManager) Check(taskID string, tailLines int) (*LocalTaskStatus, error) {
	m.mu.RLock()
	task, ok := m.tasks[taskID]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("task %s not found", taskID)
	}

	if tailLines <= 0 {
		tailLines = 50
	}

	task.mu.Lock()
	status := task.Status
	exitCode := task.ExitCode
	startedAt := task.StartedAt
	pid := task.PID
	command := task.Command
	taskRole := task.TaskRole
	task.mu.Unlock()

	elapsed := time.Since(startedAt).Round(time.Second).String()

	logTail, logSize := readLogTail(task.LogFile, tailLines)

	return &LocalTaskStatus{
		TaskID:   taskID,
		Command:  command,
		TaskRole: taskRole,
		Status:   status,
		PID:      pid,
		ExitCode: exitCode,
		Elapsed:  elapsed,
		LogTail:  logTail,
		LogSize:  logSize,
	}, nil
}

// Wait blocks until the task completes, the timeout expires, or the context
// is cancelled. Returns the final status with log tail.
func (m *LocalBackgroundTaskManager) Wait(ctx context.Context, taskID string, timeout time.Duration, tailLines int) (*LocalTaskStatus, error) {
	m.mu.RLock()
	task, ok := m.tasks[taskID]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("task %s not found", taskID)
	}

	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-task.doneC:
		// Process exited
	case <-timer.C:
		// Timeout — process still running
	case <-ctx.Done():
		// Caller cancelled (e.g. user pressed stop)
	}

	return m.Check(taskID, tailLines)
}

// Kill terminates a running task.
func (m *LocalBackgroundTaskManager) Kill(taskID string) error {
	m.mu.RLock()
	task, ok := m.tasks[taskID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}

	task.mu.Lock()
	if !task.Status.IsRunning() {
		task.mu.Unlock()
		return nil // already done
	}
	task.mu.Unlock()

	task.cancel()
	// Wait briefly for cleanup
	select {
	case <-task.doneC:
	case <-time.After(3 * time.Second):
		// Force kill if cancel didn't work
		if task.cmd.Process != nil {
			_ = task.cmd.Process.Kill()
		}
	}
	return nil
}

// List returns all tasks.
func (m *LocalBackgroundTaskManager) List() []*LocalBackgroundTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*LocalBackgroundTask, 0, len(m.tasks))
	for _, t := range m.tasks {
		t.mu.Lock()
		snapshot := &LocalBackgroundTask{
			TaskID:    t.TaskID,
			Command:   t.Command,
			TaskRole:  t.TaskRole,
			WorkDir:   t.WorkDir,
			LogFile:   t.LogFile,
			PID:       t.PID,
			Status:    t.Status,
			ExitCode:  t.ExitCode,
			StartedAt: t.StartedAt,
			EndedAt:   t.EndedAt,
		}
		t.mu.Unlock()
		result = append(result, snapshot)
	}
	return result
}

func NormalizeBackgroundTaskRole(role, command string) string {
	return backgroundrole.Normalize(role, command)
}

// Cleanup removes completed/failed tasks older than maxAge.
func (m *LocalBackgroundTaskManager) Cleanup(maxAge time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	for id, task := range m.tasks {
		task.mu.Lock()
		done := !task.Status.IsRunning()
		ended := task.EndedAt
		task.mu.Unlock()
		if done && !ended.IsZero() && ended.Before(cutoff) {
			_ = os.Remove(task.LogFile)
			delete(m.tasks, id)
		}
	}
}

// readLogTail reads the last N lines of a log file.
// For large files (>256KB), reads only the tail portion to avoid OOM.
func readLogTail(path string, n int) (string, int64) {
	info, err := os.Stat(path)
	if err != nil {
		return "", 0
	}
	size := info.Size()
	if size == 0 {
		return "", 0
	}

	const maxReadSize = 256 * 1024 // 256KB — enough for ~5000 lines

	f, err := os.Open(path)
	if err != nil {
		return "", size
	}
	defer f.Close()

	readSize := size
	if readSize > maxReadSize {
		readSize = maxReadSize
		// Seek to (size - maxReadSize)
		if _, err := f.Seek(size-readSize, 0); err != nil {
			return "", size
		}
	}

	buf := make([]byte, readSize)
	nRead, err := f.Read(buf)
	if err != nil || nRead == 0 {
		return "", size
	}
	data := string(buf[:nRead])

	// If we seeked into the middle, skip the first partial line
	if readSize < size {
		if idx := strings.IndexByte(data, '\n'); idx >= 0 {
			data = data[idx+1:]
		}
	}

	lines := strings.Split(data, "\n")
	if len(lines) <= n {
		return data, size
	}
	return strings.Join(lines[len(lines)-n:], "\n"), size
}
