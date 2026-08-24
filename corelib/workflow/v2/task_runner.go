package v2

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// TaskRunStatus is the result status of a single task execution.
type TaskRunStatus string

const (
	TaskPassed  TaskRunStatus = "passed"
	TaskFailed  TaskRunStatus = "failed"
	TaskSkipped TaskRunStatus = "skipped"
)

// TaskRunResult is the outcome of running a single task.
type TaskRunResult struct {
	TaskIndex     int           `json:"task_index"`
	Title         string        `json:"title"`
	Status        TaskRunStatus `json:"status"`
	Summary       string        `json:"summary,omitempty"`
	FilesCreated  []string      `json:"files_created,omitempty"`
	FilesModified []string      `json:"files_modified,omitempty"`
	Error         string        `json:"error,omitempty"`
	// RuntimeTaskID is an opaque reference to the durable coding execution
	// ledger. It deliberately excludes commands, prompts, credentials and any
	// replay payload; host code uses it only to prepare an explicit recovery.
	RuntimeTaskID string `json:"runtime_task_id,omitempty"`
	// RuntimeHandoff marks a parent attempt that admitted read-only children.
	// It is intentionally separate from TaskSkipped: a host must offer an
	// explicit, fresh review attempt after the Runtime has delivered bounded
	// child results. It never authorizes TaskRunner to replay the old attempt.
	RuntimeHandoff bool          `json:"runtime_handoff,omitempty"`
	Duration       time.Duration `json:"duration"`
}

// TaskRunnerConfig holds configuration for the task runner.
type TaskRunnerConfig struct {
	ProjectPath     string
	RequirementsCtx string // truncated requirements summary
	DesignCtx       string // truncated design summary
	MaxRetries      int    // per-task retry limit; 0 means do not retry
	// AllowAutomaticRetries must be explicitly enabled only for a phase proven
	// free of side effects. Coding tasks may write files or invoke shell/SSH,
	// so their safe default is no automatic replay.
	AllowAutomaticRetries bool
	TDDMode               bool // if true, each task runs in two phases: test-first → implement
	// MaxParallel is the max concurrent tasks in a ready wave (default 1 = sequential).
	// Only tasks that are simultaneously dependency-ready run together; writers should
	// keep MaxParallel low (2–3). SubAgentFunc must be concurrency-safe when > 1.
	MaxParallel int
	// WaveSize is set by the runner for each SubAgent invocation: how many tasks
	// are running in the current ready wave (1 for sequential). SubAgent hosts
	// may use this to decide isolation (e.g. git worktree only when WaveSize>1).
	WaveSize int
	// CanRunParallelWave is an optional host-owned, fail-closed admission
	// predicate. It is evaluated on the dependency-ready wave before any
	// goroutine starts. A false result serializes that wave; it must never be
	// treated as permission to fall back to concurrent shared-workspace writes.
	CanRunParallelWave func([]*TaskItem) bool
}

// SubAgentFunc is the function signature for running a single task with a SubAgent.
// The caller provides the implementation (GUI provides the real SubAgent, tests provide mocks).
type SubAgentFunc func(ctx context.Context, task *TaskItem, config TaskRunnerConfig, onToken func(string), onProgress func(string)) *TaskRunResult

// TaskRunner orchestrates the execution of parsed tasks using SubAgents.
type TaskRunner struct {
	config       TaskRunnerConfig
	subAgentFunc SubAgentFunc
	results      []TaskRunResult
}

func NewTaskRunner(config TaskRunnerConfig, subAgentFunc SubAgentFunc) *TaskRunner {
	if config.MaxRetries < 0 {
		config.MaxRetries = 0
	}
	return &TaskRunner{
		config:       config,
		subAgentFunc: subAgentFunc,
	}
}

// RunAll executes all tasks respecting dependencies.
// When MaxParallel <= 1, tasks run sequentially (original behavior).
// When MaxParallel > 1, each wave of dependency-ready unfinished tasks runs
// concurrently (up to MaxParallel), then the next wave is scheduled.
func (r *TaskRunner) RunAll(ctx context.Context, tasks []*TaskItem, onToken func(string), onProgress func(string)) []TaskRunResult {
	r.results = make([]TaskRunResult, len(tasks))
	if r.config.MaxParallel <= 1 {
		return r.runAllSequential(ctx, tasks, onToken, onProgress)
	}
	return r.runAllWaves(ctx, tasks, onToken, onProgress)
}

func (r *TaskRunner) runAllSequential(ctx context.Context, tasks []*TaskItem, onToken func(string), onProgress func(string)) []TaskRunResult {
	for i, task := range tasks {
		select {
		case <-ctx.Done():
			for j := i; j < len(tasks); j++ {
				if r.results[j].Status != "" {
					continue
				}
				r.results[j] = TaskRunResult{
					TaskIndex: tasks[j].Index,
					Title:     tasks[j].Title,
					Status:    TaskSkipped,
					Error:     "cancelled",
				}
			}
			return r.results
		default:
		}

		if !r.dependenciesMet(task, tasks) {
			r.results[i] = TaskRunResult{
				TaskIndex: task.Index,
				Title:     task.Title,
				Status:    TaskSkipped,
				Error:     "dependency not met",
			}
			if onProgress != nil {
				onProgress(fmt.Sprintf("T%d: %s — 跳过（依赖未满足）", task.Index, task.Title))
			}
			continue
		}

		if onProgress != nil {
			onProgress(fmt.Sprintf("T%d/%d: %s", task.Index, len(tasks), task.Title))
		}
		if onToken != nil {
			onToken(fmt.Sprintf("\n\n---\n### T%d: %s\n\n", task.Index, task.Title))
		}

		r.config.WaveSize = 1
		result := r.runWithRetry(ctx, task, onToken, onProgress)
		r.results[i] = result

		if onProgress != nil {
			mark := "[OK]"
			if result.Status == TaskFailed {
				mark = "[ERR]"
			}
			onProgress(fmt.Sprintf("%s T%d: %s — %s", mark, task.Index, task.Title, result.Status))
		}
	}
	return r.results
}

func (r *TaskRunner) runAllWaves(ctx context.Context, tasks []*TaskItem, onToken func(string), onProgress func(string)) []TaskRunResult {
	maxP := r.config.MaxParallel
	if maxP < 1 {
		maxP = 1
	}
	done := make([]bool, len(tasks))
	remaining := len(tasks)

	for remaining > 0 {
		select {
		case <-ctx.Done():
			for i := range tasks {
				if !done[i] {
					r.results[i] = TaskRunResult{
						TaskIndex: tasks[i].Index,
						Title:     tasks[i].Title,
						Status:    TaskSkipped,
						Error:     "cancelled",
					}
					done[i] = true
				}
			}
			return r.results
		default:
		}

		// Collect ready indices (deps met, not done). Skip permanently if deps failed.
		var ready []int
		for i, task := range tasks {
			if done[i] {
				continue
			}
			if r.dependenciesFailed(task, tasks) {
				r.results[i] = TaskRunResult{
					TaskIndex: task.Index,
					Title:     task.Title,
					Status:    TaskSkipped,
					Error:     "dependency not met",
				}
				done[i] = true
				remaining--
				if onProgress != nil {
					onProgress(fmt.Sprintf("T%d: %s — 跳过（依赖未满足）", task.Index, task.Title))
				}
				continue
			}
			if r.dependenciesMet(task, tasks) {
				ready = append(ready, i)
			}
		}
		if remaining <= 0 {
			break
		}
		if len(ready) == 0 {
			// Deadlock / circular deps — mark leftover skipped.
			for i := range tasks {
				if !done[i] {
					r.results[i] = TaskRunResult{
						TaskIndex: tasks[i].Index,
						Title:     tasks[i].Title,
						Status:    TaskSkipped,
						Error:     "dependency not met",
					}
					done[i] = true
					remaining--
				}
			}
			break
		}
		if len(ready) > maxP {
			ready = ready[:maxP]
		}

		waveSize := len(ready)
		if waveSize > 1 && r.config.CanRunParallelWave != nil {
			wave := make([]*TaskItem, 0, waveSize)
			for _, i := range ready {
				wave = append(wave, tasks[i])
			}
			if !r.config.CanRunParallelWave(wave) {
				// Preserve deterministic plan order when a host cannot prove the
				// whole ready group is concurrency-safe.
				ready = ready[:1]
				waveSize = 1
			}
		}
		r.config.WaveSize = waveSize

		if waveSize == 1 {
			i := ready[0]
			task := tasks[i]
			if onProgress != nil {
				onProgress(fmt.Sprintf("T%d/%d: %s", task.Index, len(tasks), task.Title))
			}
			if onToken != nil {
				onToken(fmt.Sprintf("\n\n---\n### T%d: %s\n\n", task.Index, task.Title))
			}
			result := r.runWithRetry(ctx, task, onToken, onProgress)
			r.results[i] = result
			done[i] = true
			remaining--
			if onProgress != nil {
				mark := "[OK]"
				if result.Status == TaskFailed {
					mark = "[ERR]"
				}
				onProgress(fmt.Sprintf("%s T%d: %s — %s", mark, task.Index, task.Title, result.Status))
			}
			continue
		}

		// Parallel wave (WaveSize already set for SubAgent isolation decisions).
		if onProgress != nil {
			names := make([]string, 0, len(ready))
			for _, i := range ready {
				names = append(names, fmt.Sprintf("T%d", tasks[i].Index))
			}
			onProgress(fmt.Sprintf("并行执行: %s", strings.Join(names, ", ")))
		}
		var mu sync.Mutex
		var wg sync.WaitGroup
		// Serialize onToken/onProgress for UI safety.
		safeToken := onToken
		safeProgress := onProgress
		if onToken != nil {
			safeToken = func(delta string) {
				mu.Lock()
				defer mu.Unlock()
				onToken(delta)
			}
		}
		if onProgress != nil {
			safeProgress = func(msg string) {
				mu.Lock()
				defer mu.Unlock()
				onProgress(msg)
			}
		}
		for _, idx := range ready {
			i := idx
			task := tasks[i]
			wg.Add(1)
			go func() {
				defer wg.Done()
				if safeToken != nil {
					safeToken(fmt.Sprintf("\n\n---\n### T%d: %s\n\n", task.Index, task.Title))
				}
				if safeProgress != nil {
					safeProgress(fmt.Sprintf("T%d/%d: %s (parallel)", task.Index, len(tasks), task.Title))
				}
				result := r.runWithRetry(ctx, task, safeToken, safeProgress)
				mu.Lock()
				r.results[i] = result
				done[i] = true
				remaining--
				if onProgress != nil {
					mark := "[OK]"
					if result.Status == TaskFailed {
						mark = "[ERR]"
					}
					onProgress(fmt.Sprintf("%s T%d: %s — %s", mark, task.Index, task.Title, result.Status))
				}
				mu.Unlock()
			}()
		}
		wg.Wait()
	}
	return r.results
}

// dependenciesFailed is true when any declared dependency finished as failed/skipped.
func (r *TaskRunner) dependenciesFailed(task *TaskItem, allTasks []*TaskItem) bool {
	if len(task.DependsOn) == 0 {
		return false
	}
	for _, depIdx := range task.DependsOn {
		for i, t := range allTasks {
			if t.Index != depIdx {
				continue
			}
			if i >= len(r.results) {
				return false
			}
			st := r.results[i].Status
			if st == TaskFailed || st == TaskSkipped {
				return true
			}
			break
		}
	}
	return false
}

// runWithRetry runs a task with retries on failure.
// When TDDMode is enabled, splits execution into two phases:
//
//	Phase 1 (test-first): SubAgent generates test cases only (no implementation)
//	Phase 2 (implement): SubAgent writes implementation and runs tests
//
// Transient errors (HTTP 502/503/504/429, network timeouts) get extra retries
// with exponential backoff before counting as a permanent failure.
func (r *TaskRunner) runWithRetry(ctx context.Context, task *TaskItem, onToken func(string), onProgress func(string)) TaskRunResult {
	// TDD Mode: run test-first phase before implementation
	if r.config.TDDMode {
		testTask := &TaskItem{
			Index:       task.Index,
			Title:       task.Title + " (tests)",
			Description: "[TEST-ONLY PHASE] Write test cases for this task. Do NOT write implementation code.\n\nOriginal task: " + task.Title + "\n\n" + task.Description + "\n\nRequirements:\n1. Create test file(s)\n2. Write tests covering main functionality\n3. Run tests to confirm they FAIL (red light - not yet implemented)\n4. Do NOT write the implementation being tested",
			Files:       task.Files,
			DependsOn:   task.DependsOn,
		}
		if onProgress != nil {
			onProgress(fmt.Sprintf("T%d: generating tests (TDD red phase)", task.Index))
		}
		if onToken != nil {
			onToken("\n\n#### TDD Red Phase: Writing Tests\n\n")
		}
		testResult := r.runSingleAttempt(ctx, testTask, onToken, onProgress)
		// A skipped red phase is a control-plane handoff (for example, scope
		// approval, cancellation, or a Runtime waiting_child transition), not a
		// failed test that the green phase may repair. Starting implementation in
		// that state could create a new writer while the prior Runtime parent has
		// deliberately released its lease for child review.
		if testResult != nil && testResult.Status == TaskSkipped {
			result := *testResult
			// The runner reports one logical task, even though the red phase uses a
			// synthetic prompt title internally.
			result.Title = task.Title
			return result
		}
		if testResult != nil && testResult.Status == TaskFailed {
			if onProgress != nil {
				onProgress(fmt.Sprintf("T%d: test generation failed, continuing to implementation", task.Index))
			}
		}
		// Now run implementation with instruction to make tests pass
		task = &TaskItem{
			Index:       task.Index,
			Title:       task.Title,
			Description: "[IMPLEMENTATION PHASE - Make tests pass]\n\n" + task.Description + "\n\nNote: Test cases were generated in the previous step. Now write implementation code to make ALL tests pass (green light).\nAfter implementation, run tests to confirm they all pass.",
			Files:       task.Files,
			DependsOn:   task.DependsOn,
		}
		if onProgress != nil {
			onProgress(fmt.Sprintf("T%d: implementing (TDD green phase)", task.Index))
		}
		if onToken != nil {
			onToken("\n\n#### TDD Green Phase: Implementation\n\n")
		}
	}

	return r.runSingleTaskWithRetry(ctx, task, onToken, onProgress)
}

// runSingleAttempt runs a task with at most one retry for transient errors.
func (r *TaskRunner) runSingleAttempt(ctx context.Context, task *TaskItem, onToken func(string), onProgress func(string)) *TaskRunResult {
	result := r.subAgentFunc(ctx, task, r.config, onToken, onProgress)
	if r.config.AllowAutomaticRetries && result != nil && result.Status == TaskFailed && isTransientTaskError(result.Error) {
		// One retry for transient errors in test generation phase
		select {
		case <-ctx.Done():
			return result
		case <-time.After(3 * time.Second):
		}
		if onProgress != nil {
			onProgress(fmt.Sprintf("T%d: test phase transient error, retrying once", task.Index))
		}
		if retry := r.subAgentFunc(ctx, task, r.config, onToken, onProgress); retry != nil {
			return retry
		}
	}
	return result
}

// runSingleTaskWithRetry runs a task with retries (the original retry logic).
func (r *TaskRunner) runSingleTaskWithRetry(ctx context.Context, task *TaskItem, onToken func(string), onProgress func(string)) TaskRunResult {
	var lastResult *TaskRunResult
	maxRetries := r.config.MaxRetries
	if !r.config.AllowAutomaticRetries {
		maxRetries = 0
	}
	const maxTransientRetries = 5

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			if onProgress != nil {
				onProgress(fmt.Sprintf("T%d: 重试 (%d/%d)", task.Index, attempt, maxRetries))
			}
			// Exponential backoff: 3s, 6s, 12s...
			backoff := time.Duration(3<<(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: TaskSkipped, Error: "cancelled"}
			case <-time.After(backoff):
			}
		}

		start := time.Now()
		result := r.subAgentFunc(ctx, task, r.config, onToken, onProgress)
		if result == nil {
			result = &TaskRunResult{
				TaskIndex: task.Index,
				Title:     task.Title,
				Status:    TaskFailed,
				Error:     "SubAgent returned nil",
			}
		}
		result.Duration = time.Since(start)
		lastResult = result

		if result.Status == TaskPassed {
			return *result
		}

		// Don't retry on cancellation
		if ctx.Err() != nil {
			result.Status = TaskSkipped
			result.Error = "cancelled"
			return *result
		}

		// For transient errors (502/503/504/429/timeout), allow extra retries beyond MaxRetries
		if r.config.AllowAutomaticRetries && isTransientTaskError(result.Error) && attempt == maxRetries && maxRetries < maxTransientRetries {
			if onProgress != nil {
				onProgress(fmt.Sprintf("T%d: 临时网络错误，额外重试...", task.Index))
			}
			maxRetries++
		}
	}

	return *lastResult
}

// dependenciesMet checks if all dependencies of a task have passed.
func (r *TaskRunner) dependenciesMet(task *TaskItem, allTasks []*TaskItem) bool {
	// If task has no declared dependencies, always proceed
	if len(task.DependsOn) == 0 {
		return true
	}
	for _, depIdx := range task.DependsOn {
		found := false
		for i, t := range allTasks {
			if t.Index == depIdx {
				if i >= len(r.results) || r.results[i].Status != TaskPassed {
					return false
				}
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// FinalReport is the user-visible finish after all task results are known.
func (r *TaskRunner) FinalReport() string {
	parts := make([]string, 0, len(r.results))
	passed, failed := 0, 0
	for _, result := range r.results {
		switch result.Status {
		case TaskPassed:
			passed++
		case TaskFailed:
			failed++
		}
		summary := strings.TrimSpace(result.Summary)
		err := strings.TrimSpace(result.Error)
		title := strings.TrimSpace(result.Title)
		switch result.Status {
		case TaskFailed:
			if summary != "" && err != "" && !strings.Contains(summary, err) {
				parts = append(parts, summary+"\n\n"+err)
			} else if summary != "" {
				parts = append(parts, summary)
			} else if title != "" && err != "" {
				parts = append(parts, title+" did not finish.\n\n"+err)
			} else if err != "" {
				parts = append(parts, err)
			} else if title != "" {
				parts = append(parts, title+" did not finish.")
			}
		case TaskSkipped:
			if summary != "" {
				parts = append(parts, summary)
			} else if title != "" {
				parts = append(parts, "Skipped "+title+".")
			}
		default:
			if summary != "" {
				parts = append(parts, summary)
			} else if title != "" {
				parts = append(parts, "Finished "+title+".")
			}
		}
	}
	if len(r.results) > 1 && failed > 0 && passed > 0 {
		parts = append(parts, fmt.Sprintf("Finished %d of %d steps; %d still failed.", passed, len(r.results), failed))
	}
	return strings.Join(parts, "\n\n")
}

// --- SubAgent security helpers ---

// ValidateWritePath checks if a write operation targets a path within the project.
func ValidateWritePath(targetPath, projectPath string) error {
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("invalid target path: %w", err)
	}
	absProject, err := filepath.Abs(projectPath)
	if err != nil {
		return fmt.Errorf("invalid project path: %w", err)
	}
	rel, err := filepath.Rel(absProject, absTarget)
	if err != nil {
		return fmt.Errorf("cannot determine relative path: %w", err)
	}
	if strings.HasPrefix(rel, "..") {
		return fmt.Errorf("write path %s is outside project directory %s", targetPath, projectPath)
	}
	return nil
}

// EnsureProjectDir creates the project directory if it doesn't exist.
func EnsureProjectDir(projectPath string) error {
	info, err := os.Stat(projectPath)
	if err == nil && info.IsDir() {
		return nil
	}
	log.Printf("[workflow-v2] creating project directory: %s", projectPath)
	return os.MkdirAll(projectPath, 0o755)
}

// IsDangerousCommand checks if a bash command is destructive.
func IsDangerousCommand(cmd string) bool {
	lower := strings.ToLower(strings.TrimSpace(cmd))
	dangerous := []string{
		"rm -rf /",
		"rm -rf /*",
		"rmdir /s /q c:",
		"format c:",
		"del /s /q c:",
		"rd /s /q c:",
		"mkfs",
		"> /dev/sda",
	}
	for _, d := range dangerous {
		if strings.Contains(lower, d) {
			return true
		}
	}
	return false
}

// isTransientTaskError checks if a task error message indicates a temporary
// infrastructure problem (gateway errors, rate limits, network timeouts) that
// is worth retrying rather than a permanent task failure.
func isTransientTaskError(errMsg string) bool {
	if errMsg == "" {
		return false
	}
	lower := strings.ToLower(errMsg)
	transientPatterns := []string{
		"502", "503", "504", "429",
		"bad gateway", "service unavailable", "gateway timeout",
		"rate limit", "too many requests",
		"timeout", "timed out",
		"connection refused", "connection reset",
		"eof", "broken pipe",
		"temporary failure", "network",
	}
	for _, p := range transientPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}
