package scheduler

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Task type constants.
const (
	TaskTypeReminder = "reminder" // 提醒类：错过就跳过，等下次
	TaskTypeProcess  = "process"  // 处理类：错过且距下次>1h则补做
)

// missedRunCatchUpThreshold is the minimum gap between a missed run and the
// next scheduled run that triggers a catch-up execution for "process" tasks.
const missedRunCatchUpThreshold = 1 * time.Hour

// defaultExecutionTimeout is the maximum time a single task execution is
// allowed to run before being forcefully cancelled. This prevents runaway
// agent loops from permanently blocking the scheduled slot.
const defaultExecutionTimeout = 30 * time.Minute

// ScheduledTask represents a single scheduled task.
type ScheduledTask struct {
	ID string `json:"id"`
	// BotProfileID owns a task created by a profile-bound Lansenger agent.
	// It scopes task execution and management; empty is a legacy/desktop task.
	BotProfileID    string `json:"bot_profile_id,omitempty"`
	Name            string `json:"name"`
	Action          string `json:"action"`                     // what the agent should do (natural language)
	Hour            int    `json:"hour"`                       // 0-23
	Minute          int    `json:"minute"`                     // 0-59
	DayOfWeek       int    `json:"day_of_week"`                // -1=every day, 0=Sun..6=Sat
	DayOfMonth      int    `json:"day_of_month"`               // -1=any, 1-31
	IntervalMinutes int    `json:"interval_minutes,omitempty"` // >0: repeat every N minutes (overrides Hour/Minute for scheduling)
	StartDate       string `json:"start_date,omitempty"`       // "2006-01-02", empty=no limit
	EndDate         string `json:"end_date,omitempty"`         // "2006-01-02", empty=no limit
	TaskType        string `json:"task_type,omitempty"`        // "reminder" (default) or "process"
	// Delivery optionally pushes the agent result to an IM channel target
	// (e.g. Lansenger group or user). Nil/disabled = no structured push.
	Delivery   *TaskDelivery `json:"delivery,omitempty"`
	Status     string        `json:"status"` // "active", "paused", "expired"
	CreatedAt  time.Time     `json:"created_at"`
	LastRunAt  *time.Time    `json:"last_run_at,omitempty"`
	NextRunAt  *time.Time    `json:"next_run_at,omitempty"`
	RunCount   int           `json:"run_count"`
	LastResult string        `json:"last_result,omitempty"`
	LastError  string        `json:"last_error,omitempty"`
}

// TaskExecutor is called when a task fires. It receives a context (cancelled
// on timeout or shutdown) and the task action. Implementations MUST respect
// ctx.Done() to allow graceful cancellation of long-running tasks.
type TaskExecutor func(ctx context.Context, task *ScheduledTask) (result string, err error)

// Manager manages scheduled tasks with JSON persistence
// and a background ticker that fires due tasks.
type Manager struct {
	mu              sync.RWMutex
	tasks           []ScheduledTask
	path            string
	stopCh          chan struct{}
	running         bool
	executor        TaskExecutor
	onChange        func()   // optional callback after task state changes (fire/expire)
	pendingCatchUps []string // task IDs that need catch-up fire on Start()

	// runningTasks tracks in-flight executor goroutines so they can be
	// cancelled on Stop(). Key is task ID, value is the cancel function.
	runningTasks map[string]context.CancelFunc
}

// NewManager creates a manager persisting to the given path.
func NewManager(path string) (*Manager, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("scheduler: resolve path: %w", err)
	}
	m := &Manager{
		tasks:        make([]ScheduledTask, 0),
		path:         absPath,
		stopCh:       make(chan struct{}),
		runningTasks: make(map[string]context.CancelFunc),
	}
	if err := m.load(); err != nil {
		return nil, err
	}
	// Remove expired tasks and recalculate next run times.
	now := time.Now()
	var active []ScheduledTask
	for i := range m.tasks {
		if m.tasks[i].Status == "expired" || m.isExpired(&m.tasks[i], now) {
			continue // drop expired tasks
		}
		if m.tasks[i].Status == "active" {
			next := m.calcNext(&m.tasks[i], now)
			m.tasks[i].NextRunAt = next
		}
		active = append(active, m.tasks[i])
	}
	m.tasks = active
	_ = m.save()

	// Collect process-type tasks that missed a run and need catch-up.
	// We do this after save so the task list is clean. The actual catch-up
	// fires are deferred until Start() is called (executor must be set first).
	m.pendingCatchUps = m.detectMissedRuns(now)

	return m, nil
}

// SetExecutor sets the callback invoked when a task fires.
func (m *Manager) SetExecutor(fn TaskExecutor) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executor = fn
}

// StartWithExecutor atomically sets the executor and starts the background
// scheduler. This is the recommended initialization method — it eliminates
// the race between Start() and SetExecutor() where catch-up runs fire with
// a nil executor because Start() was called before SetExecutor().
//
// Usage:
//
//	mgr, _ := scheduler.NewManager(path)
//	mgr.StartWithExecutor(myExecutor)
func (m *Manager) StartWithExecutor(fn TaskExecutor) {
	m.SetExecutor(fn)
	m.Start()
}

// SetOnChange sets an optional callback invoked after task state changes
// (e.g. after a task fires or expires), useful for notifying the frontend.
func (m *Manager) SetOnChange(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onChange = fn
}

// Start begins the background scheduler (checks every 30s).
func (m *Manager) Start() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	catchUps := m.pendingCatchUps
	m.pendingCatchUps = nil
	executor := m.executor
	m.mu.Unlock()

	// Fire catch-up runs for process-type tasks that missed execution.
	for _, id := range catchUps {
		go m.fireByID(id, executor)
	}

	go m.loop()
}

// Stop halts the scheduler and cancels all in-flight task executions.
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.running {
		close(m.stopCh)
		m.running = false
	}
	// Cancel active executions, but leave their lease entries in place until the
	// goroutines finish their state writes. Otherwise a manual TriggerNow can
	// start a duplicate task while the cancelled execution is still unwinding.
	for _, cancel := range m.runningTasks {
		cancel()
	}
	m.mu.Unlock()
}

func (m *Manager) loop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.tick()
		}
	}
}

func (m *Manager) tick() {
	now := time.Now()
	m.mu.RLock()
	var dueIDs []string
	for _, t := range m.tasks {
		if t.Status == "active" && t.NextRunAt != nil && !now.Before(*t.NextRunAt) {
			dueIDs = append(dueIDs, t.ID)
		}
	}
	executor := m.executor
	m.mu.RUnlock()

	for _, id := range dueIDs {
		go m.fireByID(id, executor)
	}

	// Auto-delete expired tasks so they don't clutter the list.
	m.purgeExpired(now)
}

// purgeExpired removes tasks whose status is "expired" or whose EndDate has
// passed while they were active/paused. Returns true if any tasks were removed.
func (m *Manager) purgeExpired(now time.Time) bool {
	m.mu.Lock()
	n := 0
	for i := range m.tasks {
		expired := m.tasks[i].Status == "expired"
		if !expired && m.isExpired(&m.tasks[i], now) {
			expired = true
		}
		if !expired {
			m.tasks[n] = m.tasks[i]
			n++
		}
	}
	removed := len(m.tasks) - n
	if removed == 0 {
		m.mu.Unlock()
		return false
	}
	m.tasks = m.tasks[:n]
	_ = m.save()
	cb := m.onChange
	m.mu.Unlock()
	if cb != nil {
		cb()
	}
	return true
}

// computeExecutionTimeout returns the timeout for a task execution.
// Strategy: 80% of the interval (so the task finishes before the next run),
// clamped to [defaultExecutionTimeout, 2h].
func computeExecutionTimeout(task *ScheduledTask) time.Duration {
	timeout := defaultExecutionTimeout
	if task.IntervalMinutes > 0 {
		intervalTimeout := time.Duration(float64(task.IntervalMinutes)*0.8) * time.Minute
		if intervalTimeout > timeout {
			timeout = intervalTimeout
		}
	}
	const maxTimeout = 2 * time.Hour
	if timeout > maxTimeout {
		return maxTimeout
	}
	return timeout
}

func (m *Manager) fireByID(id string, executor TaskExecutor) {
	// Atomically claim the task. Manual runs retain NextRunAt, so checking it
	// alone is insufficient: use runningTasks as the common execution lease.
	m.mu.Lock()
	var taskCopy *ScheduledTask
	for i, t := range m.tasks {
		if t.ID == id {
			if _, running := m.runningTasks[id]; running {
				m.mu.Unlock()
				return
			}
			if t.NextRunAt == nil {
				// Already claimed by another goroutine.
				m.mu.Unlock()
				return
			}
			cp := t // copy the struct
			// Delivery is a pointer — deep-copy so fire-time Normalize cannot
			// mutate the persisted task (and concurrent Update stays isolated).
			cp.Delivery = CloneTaskDelivery(t.Delivery)
			taskCopy = &cp
			m.tasks[i].NextRunAt = nil // prevent double-fire
			break
		}
	}
	if taskCopy == nil {
		m.mu.Unlock()
		return
	}

	timeout := computeExecutionTimeout(taskCopy)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	// Register before releasing the task lock, closing the window where a
	// manual run or another due tick could start the same task concurrently.
	m.runningTasks[id] = cancel
	m.mu.Unlock()

	// Ensure cleanup of the running task entry and context.
	defer func() {
		cancel()
		m.mu.Lock()
		delete(m.runningTasks, id)
		m.mu.Unlock()
	}()

	// Execute outside lock (with panic recovery).
	var result, errStr string
	if executor != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					errStr = fmt.Sprintf("panic: %v", r)
				}
			}()
			res, err := executor(ctx, taskCopy)
			result = res
			if err != nil {
				errStr = err.Error()
			}
		}()
	} else {
		result = "no executor configured"
	}

	// If the context was cancelled (timeout or shutdown), annotate when missing.
	errStr = annotateExecErrStr(ctx, errStr, timeout)

	// Update state under lock.
	now := time.Now()
	m.mu.Lock()
	for i := range m.tasks {
		if m.tasks[i].ID != id {
			continue
		}
		m.tasks[i].LastRunAt = &now
		m.tasks[i].RunCount++
		m.tasks[i].LastResult = TruncateLastResult(result)
		m.tasks[i].LastError = errStr

		if m.isExpired(&m.tasks[i], now) {
			// Remove the expired task in-place instead of keeping it around.
			m.tasks = append(m.tasks[:i], m.tasks[i+1:]...)
		} else if m.tasks[i].Status == "active" {
			m.tasks[i].NextRunAt = m.calcNext(&m.tasks[i], now)
		} else {
			// A task can be paused while its executor is still unwinding. Keep
			// it paused; completion may record the result but must not revive it.
			m.tasks[i].NextRunAt = nil
		}
		break
	}
	_ = m.save()
	cb := m.onChange
	m.mu.Unlock()

	if cb != nil {
		cb()
	}
}

// ---------------------------------------------------------------------------
// CRUD
// ---------------------------------------------------------------------------

// Add creates a new scheduled task and returns its ID.
func (m *Manager) Add(t ScheduledTask) (string, error) {
	if t.Name == "" {
		return "", fmt.Errorf("scheduler: name is required")
	}
	if t.Action == "" {
		return "", fmt.Errorf("scheduler: action is required")
	}
	if t.Hour < 0 || t.Hour > 23 {
		return "", fmt.Errorf("scheduler: hour must be 0-23")
	}
	if t.Minute < 0 || t.Minute > 59 {
		return "", fmt.Errorf("scheduler: minute must be 0-59")
	}
	if t.TaskType != "" && t.TaskType != TaskTypeReminder && t.TaskType != TaskTypeProcess {
		return "", fmt.Errorf("scheduler: task_type must be %q or %q", TaskTypeReminder, TaskTypeProcess)
	}
	if t.IntervalMinutes < 0 {
		return "", fmt.Errorf("scheduler: interval_minutes must be >= 0")
	}
	if t.Delivery != nil {
		t.Delivery.Normalize()
		if err := t.Delivery.Validate(); err != nil {
			return "", err
		}
		if err := t.Delivery.EnsureResolved(); err != nil {
			return "", err
		}
		// Persist nil when disabled so old clients stay clean.
		if !t.Delivery.Enabled {
			t.Delivery = nil
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	t.ID = generateID()
	t.Status = "active"
	t.CreatedAt = now
	t.NextRunAt = m.calcNext(&t, now)

	if m.isExpired(&t, now) {
		return "", fmt.Errorf("scheduler: end_date is already in the past")
	}

	m.tasks = append(m.tasks, t)
	if err := m.save(); err != nil {
		return "", err
	}
	return t.ID, nil
}

// List returns all tasks.
func (m *Manager) List() []ScheduledTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ScheduledTask, len(m.tasks))
	for i, t := range m.tasks {
		cp := t
		// Isolate Delivery pointer so callers cannot mutate the store via Normalize.
		cp.Delivery = CloneTaskDelivery(t.Delivery)
		out[i] = cp
	}
	return out
}

// Get returns a task by ID, or nil if not found.
func (m *Manager) Get(id string) *ScheduledTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, t := range m.tasks {
		if t.ID == id {
			cp := t
			cp.Delivery = CloneTaskDelivery(t.Delivery)
			return &cp
		}
	}
	return nil
}

// Delete removes a task by ID.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, t := range m.tasks {
		if t.ID == id {
			if cancel, running := m.runningTasks[id]; running {
				cancel()
				return fmt.Errorf("scheduler: task %q is running; cancellation requested", id)
			}
			m.tasks = append(m.tasks[:i], m.tasks[i+1:]...)
			return m.save()
		}
	}
	return fmt.Errorf("scheduler: task %q not found", id)
}

// DeleteByName removes a task by name (first match).
func (m *Manager) DeleteByName(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, t := range m.tasks {
		if t.Name == name {
			m.tasks = append(m.tasks[:i], m.tasks[i+1:]...)
			return m.save()
		}
	}
	return fmt.Errorf("scheduler: task named %q not found", name)
}

// Pause pauses a task.
func (m *Manager) Pause(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.tasks {
		if m.tasks[i].ID == id {
			if cancel, running := m.runningTasks[id]; running {
				cancel()
			}
			m.tasks[i].Status = "paused"
			m.tasks[i].NextRunAt = nil
			return m.save()
		}
	}
	return fmt.Errorf("scheduler: task %q not found", id)
}

// Resume resumes a paused task.
func (m *Manager) Resume(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for i := range m.tasks {
		if m.tasks[i].ID == id {
			if m.isExpired(&m.tasks[i], now) {
				// Task has expired while paused — remove it.
				m.tasks = append(m.tasks[:i], m.tasks[i+1:]...)
				_ = m.save()
				return fmt.Errorf("scheduler: task %q has expired (end_date passed) and was removed", id)
			}
			m.tasks[i].Status = "active"
			m.tasks[i].NextRunAt = m.calcNext(&m.tasks[i], now)
			return m.save()
		}
	}
	return fmt.Errorf("scheduler: task %q not found", id)
}

// ClearAll removes all tasks.
func (m *Manager) ClearAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks = m.tasks[:0]
	return m.save()
}

// Update modifies a task by ID. Only non-zero/non-empty fields in args are applied.
func (m *Manager) Update(id string, args map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.tasks {
		if m.tasks[i].ID != id {
			continue
		}
		if v, ok := args["name"].(string); ok && v != "" {
			m.tasks[i].Name = v
		}
		if v, ok := args["action"].(string); ok && v != "" {
			m.tasks[i].Action = v
		}
		// Accept float64 (JSON) and int (Go tooling / re-marshaled maps).
		if h, ok := coerceInt(args["hour"]); ok && h >= 0 && h <= 23 {
			m.tasks[i].Hour = h
		}
		if mn, ok := coerceInt(args["minute"]); ok && mn >= 0 && mn <= 59 {
			m.tasks[i].Minute = mn
		}
		if dow, ok := coerceInt(args["day_of_week"]); ok {
			m.tasks[i].DayOfWeek = dow
		}
		if dom, ok := coerceInt(args["day_of_month"]); ok {
			m.tasks[i].DayOfMonth = dom
		}
		if iv, ok := coerceInt(args["interval_minutes"]); ok {
			if iv < 0 {
				return fmt.Errorf("scheduler: interval_minutes must be >= 0")
			}
			m.tasks[i].IntervalMinutes = iv
		}
		if v, ok := args["start_date"].(string); ok {
			m.tasks[i].StartDate = v
		}
		if v, ok := args["end_date"].(string); ok {
			m.tasks[i].EndDate = v
		}
		if v, ok := args["task_type"].(string); ok {
			if v != "" && v != TaskTypeReminder && v != TaskTypeProcess {
				return fmt.Errorf("scheduler: task_type must be %q or %q", TaskTypeReminder, TaskTypeProcess)
			}
			m.tasks[i].TaskType = v
		}
		if raw, ok := args["delivery"]; ok {
			// Explicit null / empty clears delivery.
			if raw == nil {
				m.tasks[i].Delivery = nil
			} else {
				d, err := ParseDeliveryFromAny(raw)
				if err != nil {
					return err
				}
				if d == nil || !d.Enabled {
					m.tasks[i].Delivery = nil
				} else {
					if err := d.EnsureResolved(); err != nil {
						return err
					}
					m.tasks[i].Delivery = d
				}
			}
		}
		now := time.Now()
		if m.tasks[i].Status == "active" {
			m.tasks[i].NextRunAt = m.calcNext(&m.tasks[i], now)
		}
		return m.save()
	}
	return fmt.Errorf("scheduler: task %q not found", id)
}

// ---------------------------------------------------------------------------
// TriggerNow / fireManual
// ---------------------------------------------------------------------------

// TriggerNow immediately executes a task regardless of its schedule.
// The task must be in "active" status. Execution happens asynchronously
// in a goroutine (same as scheduled fires); the method returns immediately.
func (m *Manager) TriggerNow(id string) error {
	m.mu.Lock()
	var taskCopy *ScheduledTask
	for _, t := range m.tasks {
		if t.ID == id {
			cp := t
			cp.Delivery = CloneTaskDelivery(t.Delivery)
			taskCopy = &cp
			break
		}
	}
	executor := m.executor
	if taskCopy == nil {
		m.mu.Unlock()
		return fmt.Errorf("task %s not found", id)
	}
	if taskCopy.Status != "active" {
		m.mu.Unlock()
		return fmt.Errorf("task %s is not active (status=%s)", id, taskCopy.Status)
	}
	if _, running := m.runningTasks[id]; running {
		m.mu.Unlock()
		return fmt.Errorf("task %s is already running", id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), computeExecutionTimeout(taskCopy))
	// Claim before starting the goroutine. A second IM command can arrive before
	// the first goroutine begins, so registering inside fireManual is too late.
	m.runningTasks[id] = cancel
	m.mu.Unlock()

	// Fire asynchronously so the UI gets an immediate response.
	go m.fireManual(taskCopy, executor, ctx, cancel)
	return nil
}

// fireManual executes a task triggered manually. Unlike fireByID it does not
// check NextRunAt (the task may not be due yet).
func (m *Manager) fireManual(taskCopy *ScheduledTask, executor TaskExecutor, ctx context.Context, cancel context.CancelFunc) {
	if taskCopy == nil {
		return
	}
	id := taskCopy.ID
	timeout := computeExecutionTimeout(taskCopy)
	defer func() {
		cancel()
		m.mu.Lock()
		delete(m.runningTasks, id)
		m.mu.Unlock()
	}()

	var result, errStr string
	if executor != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					errStr = fmt.Sprintf("panic: %v", r)
				}
			}()
			res, err := executor(ctx, taskCopy)
			result = res
			if err != nil {
				errStr = err.Error()
			}
		}()
	} else {
		result = "no executor configured"
	}

	errStr = annotateExecErrStr(ctx, errStr, timeout)

	now := time.Now()
	m.mu.Lock()
	for i := range m.tasks {
		if m.tasks[i].ID != id {
			continue
		}
		m.tasks[i].LastRunAt = &now
		m.tasks[i].RunCount++
		m.tasks[i].LastResult = TruncateLastResult(result)
		m.tasks[i].LastError = errStr
		break
	}
	_ = m.save()
	cb := m.onChange
	m.mu.Unlock()

	if cb != nil {
		cb()
	}
}

// annotateExecErrStr fills or augments LastError when the scheduler aborted
// (timeout/cancel). If the executor already returned an error string that does
// not mention the abort, append a short marker so operators still see the timeout.
func annotateExecErrStr(ctx context.Context, errStr string, timeout time.Duration) string {
	if ctx == nil {
		return errStr
	}
	cerr := ctx.Err()
	if cerr == nil {
		return errStr
	}
	var mark string
	switch {
	case errors.Is(cerr, context.DeadlineExceeded):
		mark = fmt.Sprintf("execution timed out after %v", timeout)
	case errors.Is(cerr, context.Canceled):
		mark = "execution cancelled (shutdown)"
	default:
		return errStr
	}
	if errStr == "" {
		return mark
	}
	// Avoid duplicating common abort phrases already present in executor errors.
	lower := strings.ToLower(errStr)
	if strings.Contains(lower, "timed out") ||
		strings.Contains(lower, "deadline exceeded") || // errors.Is → Error() text
		strings.Contains(lower, "deadlineexceeded") ||
		strings.Contains(lower, "canceled") ||
		strings.Contains(lower, "cancelled") ||
		strings.Contains(errStr, "超时") ||
		strings.Contains(errStr, "中断") {
		return errStr
	}
	return errStr + "; " + mark
}

// ---------------------------------------------------------------------------
// Missed-run detection (catch-up for "process" tasks)
// ---------------------------------------------------------------------------

// detectMissedRuns checks active tasks for missed executions. For "process"
// type tasks, if the gap between now and the next scheduled run exceeds
// missedRunCatchUpThreshold (1 hour), the task ID is returned for catch-up.
// "reminder" tasks (or empty TaskType) are always skipped — miss is OK.
func (m *Manager) detectMissedRuns(now time.Time) []string {
	var ids []string
	for i := range m.tasks {
		t := &m.tasks[i]
		if t.Status != "active" || t.TaskType != TaskTypeProcess {
			continue
		}
		// Determine the most recent time this task should have run before now.
		missed := m.lastScheduledBefore(t, now)
		if missed == nil {
			continue
		}
		// If the task already ran at or after that time, no miss.
		if t.LastRunAt != nil && !t.LastRunAt.Before(*missed) {
			continue
		}
		// Task never ran, or last run was before the missed slot → missed.
		// Check if the gap to the next run is large enough to warrant catch-up.
		if t.NextRunAt != nil && t.NextRunAt.Sub(now) > missedRunCatchUpThreshold {
			ids = append(ids, t.ID)
		}
	}
	return ids
}

// lastScheduledBefore returns the most recent scheduled execution time
// strictly before `before` for the given task, or nil if none exists.
func (m *Manager) lastScheduledBefore(t *ScheduledTask, before time.Time) *time.Time {
	// Interval mode: walk backwards from `before` in interval steps.
	if t.IntervalMinutes > 0 {
		interval := time.Duration(t.IntervalMinutes) * time.Minute
		anchor := time.Date(before.Year(), before.Month(), before.Day(), t.Hour, t.Minute, 0, 0, time.Local)
		if t.LastRunAt != nil {
			anchor = *t.LastRunAt
		}
		if !anchor.Before(before) {
			// anchor is in the future; walk back
			elapsed := anchor.Sub(before)
			steps := int(elapsed/interval) + 1
			anchor = anchor.Add(-time.Duration(steps) * interval)
		}
		// Now anchor <= before. Find the latest anchor + k*interval < before.
		candidate := anchor
		for candidate.Add(interval).Before(before) {
			candidate = candidate.Add(interval)
		}
		if candidate.Before(before) && m.matchesDay(t, candidate) && m.inDateRange(t, candidate) {
			return &candidate
		}
		return nil
	}

	// Fixed-time mode: walk backwards day by day (up to 400 days).
	for d := 0; d < 400; d++ {
		day := before.AddDate(0, 0, -d)
		candidate := time.Date(day.Year(), day.Month(), day.Day(), t.Hour, t.Minute, 0, 0, time.Local)
		if !candidate.Before(before) {
			continue
		}
		if m.matchesDay(t, candidate) && m.inDateRange(t, candidate) {
			return &candidate
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Schedule calculation
// ---------------------------------------------------------------------------

// calcNext computes the next execution time after `after`.
func (m *Manager) calcNext(t *ScheduledTask, after time.Time) *time.Time {
	// Interval mode: repeat every N minutes.
	if t.IntervalMinutes > 0 {
		return m.calcNextInterval(t, after)
	}

	// Fixed-time mode: run once per matching day at Hour:Minute.
	candidate := time.Date(after.Year(), after.Month(), after.Day(), t.Hour, t.Minute, 0, 0, time.Local)

	// If candidate is not after `after`, move to tomorrow.
	if !candidate.After(after) {
		candidate = candidate.AddDate(0, 0, 1)
	}

	// Scan up to 400 days to find a matching day.
	for i := 0; i < 400; i++ {
		if m.matchesDay(t, candidate) && m.inDateRange(t, candidate) {
			return &candidate
		}
		candidate = candidate.AddDate(0, 0, 1)
	}
	return nil // no future match found
}

// calcNextInterval computes the next execution time for interval-based tasks.
// The anchor is LastRunAt (if available) or today's Hour:Minute. The next run
// is the earliest anchor + k*interval that is strictly after `after` and falls
// on a matching day within the date range.
func (m *Manager) calcNextInterval(t *ScheduledTask, after time.Time) *time.Time {
	if t.IntervalMinutes <= 0 {
		return nil // defensive: should not reach here
	}
	interval := time.Duration(t.IntervalMinutes) * time.Minute

	// Determine anchor: last run, or today's start time.
	anchor := time.Date(after.Year(), after.Month(), after.Day(), t.Hour, t.Minute, 0, 0, time.Local)
	if t.LastRunAt != nil {
		anchor = *t.LastRunAt
	}

	// Fast-forward: find the first anchor + k*interval > after.
	candidate := anchor
	if !candidate.After(after) {
		// How many intervals to skip?
		elapsed := after.Sub(candidate)
		steps := int(elapsed/interval) + 1
		candidate = candidate.Add(time.Duration(steps) * interval)
	}

	// Scan up to 400 days worth of candidates.
	limit := after.AddDate(0, 0, 400)
	for candidate.Before(limit) {
		if m.matchesDay(t, candidate) && m.inDateRange(t, candidate) {
			return &candidate
		}
		candidate = candidate.Add(interval)
	}
	return nil
}

func (m *Manager) matchesDay(t *ScheduledTask, d time.Time) bool {
	if t.DayOfMonth > 0 && d.Day() != t.DayOfMonth {
		return false
	}
	if t.DayOfWeek >= 0 && int(d.Weekday()) != t.DayOfWeek {
		return false
	}
	return true
}

func (m *Manager) inDateRange(t *ScheduledTask, d time.Time) bool {
	day := d.Format("2006-01-02")
	if t.StartDate != "" && day < t.StartDate {
		return false
	}
	if t.EndDate != "" && day > t.EndDate {
		return false
	}
	return true
}

func (m *Manager) isExpired(t *ScheduledTask, now time.Time) bool {
	if t.EndDate == "" {
		return false
	}
	today := now.Format("2006-01-02")
	return today > t.EndDate
}

// ---------------------------------------------------------------------------
// Persistence
// ---------------------------------------------------------------------------

func (m *Manager) load() error {
	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("scheduler: read %s: %w", m.path, err)
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, &m.tasks)
}

func (m *Manager) save() error {
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("scheduler: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(m.tasks, "", "  ")
	if err != nil {
		return fmt.Errorf("scheduler: marshal: %w", err)
	}
	// Atomic write: write to temp file then rename to avoid corruption on crash.
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("scheduler: write tmp: %w", err)
	}
	return os.Rename(tmp, m.path)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// generateID produces a unique ID from the current timestamp and a random suffix.
func generateID() string {
	var buf [2]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("%d-%04x", time.Now().UnixNano(), int(buf[0])<<8|int(buf[1]))
}

// TruncateStr truncates s to at most maxLen runes (including "..." when shortened).
func TruncateStr(s string, maxLen int) string {
	return truncateRunes(s, maxLen)
}

// coerceInt accepts JSON float64, int, int64, and json.Number-like strings.
func coerceInt(v interface{}) (int, bool) {
	if v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float32:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i), true
		}
		if f, err := n.Float64(); err == nil {
			return int(f), true
		}
		return 0, false
	case string:
		raw := strings.TrimSpace(n)
		if i, err := strconv.Atoi(raw); err == nil {
			return i, true
		}
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return int(f), true
		}
		return 0, false
	default:
		return 0, false
	}
}

// FormatInterval returns a human-readable string for IntervalMinutes.
// ≥1440 → "N天", ≥60 → "N小时", otherwise "N分钟".
// Fractional values are shown when not evenly divisible (e.g. "1.5小时").
func FormatInterval(minutes int) string {
	if minutes <= 0 {
		return ""
	}
	if minutes >= 1440 {
		days := minutes / 1440
		rem := minutes % 1440
		if rem == 0 {
			return fmt.Sprintf("%d天", days)
		}
		return fmt.Sprintf("%.1f天", float64(minutes)/1440.0)
	}
	if minutes >= 60 {
		hours := minutes / 60
		rem := minutes % 60
		if rem == 0 {
			return fmt.Sprintf("%d小时", hours)
		}
		return fmt.Sprintf("%.1f小时", float64(minutes)/60.0)
	}
	return fmt.Sprintf("%d分钟", minutes)
}
