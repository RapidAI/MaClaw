package main

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/clawnet"
)

// ClawNetAutoTaskPicker monitors the ClawNet network and automatically picks
// up tasks to execute via the maClaw agent, earning credits upon completion.
//
// Flow: detect online → browse tasks → select best → claim → execute via agent → submit result
type ClawNetAutoTaskPicker struct {
	mu       sync.Mutex
	running  bool
	stopCh   chan struct{}
	pollMu   sync.Mutex // prevents concurrent pollAndPickTask invocations
	client   *ClawNetClient
	hubURL   string
	executor AutoTaskExecutor
	onChange func() // optional callback after state changes

	// Configuration
	pollInterval   time.Duration // how often to check for tasks (default 5min)
	maxConcurrent  int           // max tasks to run at once (default 1)
	minReward      float64       // minimum reward to consider (default 0)
	autoEnabled    bool          // whether auto-pickup is enabled
	preferredTags  []string      // preferred task tags for matching
	lang           string        // "zh" or "en" for error localisation

	// State
	activeTasks    map[string]*autoTaskRun // taskID -> run info
	completedCount int
	failedCount    int
	totalEarned    float64
	lastPollAt     time.Time
	lastError      string
}

// autoTaskRun tracks a single auto-picked task execution.
type autoTaskRun struct {
	TaskID    string    `json:"task_id"`
	Title     string    `json:"title"`
	Reward    float64   `json:"reward"`
	Status    string    `json:"status"` // "claiming", "executing", "submitting", "done", "failed"
	StartedAt time.Time `json:"started_at"`
	Result    string    `json:"result,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// AutoTaskExecutor is called to execute a task. It receives the task description
// and should return the result text. This is wired to the IM handler agent loop.
type AutoTaskExecutor func(taskTitle, taskDescription string) (result string, err error)

// NewClawNetAutoTaskPicker creates a new auto task picker.
func NewClawNetAutoTaskPicker(client *ClawNetClient, hubURL string) *ClawNetAutoTaskPicker {
	return &ClawNetAutoTaskPicker{
		client:        client,
		hubURL:        hubURL,
		pollInterval:  5 * time.Minute,
		maxConcurrent: 1,
		minReward:     0,
		autoEnabled:   false,
		activeTasks:   make(map[string]*autoTaskRun),
		stopCh:        make(chan struct{}),
	}
}

// SetExecutor sets the callback invoked to execute a picked task.
func (p *ClawNetAutoTaskPicker) SetExecutor(fn AutoTaskExecutor) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.executor = fn
}

// SetOnChange sets an optional callback for state changes.
func (p *ClawNetAutoTaskPicker) SetOnChange(fn func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onChange = fn
}

// SetLang sets the language for error messages ("zh" or "en").
func (p *ClawNetAutoTaskPicker) SetLang(lang string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lang = lang
}

// Configure updates picker settings.
func (p *ClawNetAutoTaskPicker) Configure(enabled bool, pollMinutes int, minReward float64, tags []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.autoEnabled = enabled
	if pollMinutes > 0 {
		p.pollInterval = time.Duration(pollMinutes) * time.Minute
	}
	p.minReward = minReward
	p.preferredTags = tags
}

// IsEnabled returns whether auto-pickup is enabled.
func (p *ClawNetAutoTaskPicker) IsEnabled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.autoEnabled
}

// Start begins the background polling loop.
func (p *ClawNetAutoTaskPicker) Start() {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.stopCh = make(chan struct{})
	p.mu.Unlock()

	go p.loop()
}

// Stop halts the background polling loop.
func (p *ClawNetAutoTaskPicker) Stop() {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return
	}
	p.running = false
	close(p.stopCh)
	p.mu.Unlock()
}

// GetStatus returns the current picker status for the frontend.
func (p *ClawNetAutoTaskPicker) GetStatus() map[string]interface{} {
	p.mu.Lock()
	defer p.mu.Unlock()

	active := make([]map[string]interface{}, 0, len(p.activeTasks))
	for _, r := range p.activeTasks {
		active = append(active, map[string]interface{}{
			"task_id":    r.TaskID,
			"title":      r.Title,
			"reward":     r.Reward,
			"status":     r.Status,
			"started_at": r.StartedAt.Format(time.RFC3339),
		})
	}

	return map[string]interface{}{
		"enabled":         p.autoEnabled,
		"running":         p.running,
		"poll_interval":   int(p.pollInterval.Minutes()),
		"min_reward":      p.minReward,
		"preferred_tags":  p.preferredTags,
		"active_tasks":    active,
		"completed_count": p.completedCount,
		"failed_count":    p.failedCount,
		"total_earned":    p.totalEarned,
		"last_poll_at":    formatTimeOrEmpty(p.lastPollAt),
		"last_error":      p.lastError,
	}
}

func formatTimeOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// loop is the main polling goroutine.
func (p *ClawNetAutoTaskPicker) loop() {
	// Initial delay: wait a bit before first poll to let the system settle.
	select {
	case <-time.After(30 * time.Second):
	case <-p.stopCh:
		return
	}

	for {
		p.mu.Lock()
		enabled := p.autoEnabled
		interval := p.pollInterval
		p.mu.Unlock()

		if enabled {
			p.pollAndPickTask()
		}

		select {
		case <-time.After(interval):
		case <-p.stopCh:
			return
		}
	}
}

// pollAndPickTask checks if ClawNet is online, browses tasks, and picks one.
// Protected by pollMu to prevent concurrent invocations from loop + TriggerNow.
func (p *ClawNetAutoTaskPicker) pollAndPickTask() {
	if !p.pollMu.TryLock() {
		return // another poll is already in progress
	}
	defer p.pollMu.Unlock()

	p.mu.Lock()
	p.lastPollAt = time.Now()
	p.lastError = ""

	// Check if we're already at max concurrent tasks.
	activeCount := 0
	for _, r := range p.activeTasks {
		if r.Status == "executing" || r.Status == "claiming" || r.Status == "submitting" {
			activeCount++
		}
	}
	if activeCount >= p.maxConcurrent {
		p.mu.Unlock()
		return
	}

	client := p.client
	hubURL := p.hubURL
	executor := p.executor
	minReward := p.minReward
	preferredTags := append([]string{}, p.preferredTags...)
	p.mu.Unlock()

	if executor == nil {
		p.setError("executor not configured")
		return
	}

	// Step 1: Check if ClawNet is online.
	if !client.IsRunning() {
		// Not an error — just not online yet. Don't spam lastError.
		return
	}

	// Step 2: Browse available tasks (try matched first, then network).
	tasks, err := p.discoverTasks(client, hubURL)
	if err != nil {
		p.setError(fmt.Sprintf("task discovery failed: %v", err))
		return
	}
	if len(tasks) == 0 {
		return // no tasks available, that's fine
	}

	// Step 3: Select the best task.
	selected := p.selectTask(tasks, minReward, preferredTags)
	if selected == nil {
		return // no suitable task found
	}

	// Step 4: Claim and execute the task.
	go p.executeTask(client, selected, executor)
}

// discoverTasks tries matched tasks first, then falls back to network browse.
func (p *ClawNetAutoTaskPicker) discoverTasks(client *ClawNetClient, hubURL string) ([]ClawNetTask, error) {
	// Try the match API first (returns tasks suited to our capabilities).
	matched, err := client.MatchTasks()
	if err == nil && len(matched) > 0 {
		var open []ClawNetTask
		for _, t := range matched {
			if t.Status == "open" {
				open = append(open, t)
			}
		}
		if len(open) > 0 {
			return open, nil
		}
	}

	// Fall back to browsing network tasks via Hub.
	if hubURL == "" {
		// No Hub configured — not an error, just no network tasks available.
		return nil, nil
	}
	netTasks, err := client.BrowseHubTasks(hubURL)
	if err != nil {
		return nil, err
	}
	var open []ClawNetTask
	for _, t := range netTasks {
		if t.Status == "open" {
			open = append(open, t)
		}
	}
	return open, nil
}

// selectTask picks the best task from the available list.
func (p *ClawNetAutoTaskPicker) selectTask(tasks []ClawNetTask, minReward float64, preferredTags []string) *ClawNetTask {
	if len(tasks) == 0 {
		return nil
	}

	// Filter by minimum reward.
	var candidates []ClawNetTask
	for _, t := range tasks {
		if t.Reward >= minReward {
			candidates = append(candidates, t)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	// Skip tasks we're already working on.
	p.mu.Lock()
	activeIDs := make(map[string]bool)
	for id := range p.activeTasks {
		activeIDs[id] = true
	}
	p.mu.Unlock()

	var filtered []ClawNetTask
	for _, t := range candidates {
		if !activeIDs[t.ID] {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) == 0 {
		return nil
	}

	// Score tasks: prefer matching tags and higher rewards.
	type scored struct {
		task  ClawNetTask
		score float64
	}
	tagSet := make(map[string]bool)
	for _, tag := range preferredTags {
		tagSet[strings.ToLower(tag)] = true
	}

	var scoredTasks []scored
	for _, t := range filtered {
		s := t.Reward // base score is the reward
		for _, tag := range t.Tags {
			if tagSet[strings.ToLower(tag)] {
				s += 50 // bonus for matching tags
			}
		}
		scoredTasks = append(scoredTasks, scored{task: t, score: s})
	}

	// Pick the highest scored task; break ties randomly.
	best := scoredTasks[0]
	for _, st := range scoredTasks[1:] {
		if st.score > best.score || (st.score == best.score && rand.Float64() > 0.5) {
			best = st
		}
	}

	result := best.task
	return &result
}

// executeTask claims a task, runs it through the agent, and submits the result.
func (p *ClawNetAutoTaskPicker) executeTask(client *ClawNetClient, task *ClawNetTask, executor AutoTaskExecutor) {
	p.mu.Lock()
	lang := p.lang
	p.mu.Unlock()

	run := &autoTaskRun{
		TaskID:    task.ID,
		Title:     task.Title,
		Reward:    task.Reward,
		Status:    "claiming",
		StartedAt: time.Now(),
	}

	p.mu.Lock()
	p.activeTasks[task.ID] = run
	p.mu.Unlock()
	p.notifyChange()

	// Step 1: Claim the task.
	if err := client.ClaimTask(task.ID); err != nil {
		// If claim fails, try bidding instead.
		if bidErr := client.BidOnTask(task.ID, 0, "maClaw auto-pickup: I can help with this"); bidErr != nil {
			p.failTask(run, clawnet.FormatTaskError("claim", err, lang)+", "+clawnet.FormatTaskError("bid", bidErr, lang))
			return
		}
	}

	// Step 2: Execute via agent.
	p.setRunStatus(run, "executing")

	description := task.Title
	if task.Description != "" {
		description = task.Title + "\n\n" + task.Description
	}

	result, err := executor(task.Title, description)
	if err != nil {
		p.failTask(run, clawnet.FormatTaskError("execution", err, lang))
		return
	}
	if result == "" {
		result = "Task completed successfully."
	}

	p.mu.Lock()
	run.Result = result
	p.mu.Unlock()

	// Step 3: Submit the result.
	p.setRunStatus(run, "submitting")

	if err := client.SubmitTaskResult(task.ID, result); err != nil {
		p.failTask(run, clawnet.FormatTaskError("submit", err, lang))
		return
	}

	// Success!
	p.mu.Lock()
	run.Status = "done"
	p.completedCount++
	p.totalEarned += task.Reward
	delete(p.activeTasks, task.ID)
	p.mu.Unlock()
	p.notifyChange()

	fmt.Printf("[auto-task-picker] ✅ completed task %q (reward: %.0f🐚)\n", task.Title, task.Reward)
}

// setRunStatus updates a run's status under the lock and notifies.
func (p *ClawNetAutoTaskPicker) setRunStatus(run *autoTaskRun, status string) {
	p.mu.Lock()
	run.Status = status
	p.mu.Unlock()
	p.notifyChange()
}

// failTask marks a task run as failed and cleans up.
func (p *ClawNetAutoTaskPicker) failTask(run *autoTaskRun, errMsg string) {
	p.mu.Lock()
	run.Status = "failed"
	run.Error = errMsg
	p.failedCount++
	delete(p.activeTasks, run.TaskID)
	p.lastError = errMsg
	p.mu.Unlock()
	p.notifyChange()

	fmt.Printf("[auto-task-picker] ❌ task %q failed: %s\n", run.Title, errMsg)
}

// setError records an error without failing a specific task.
func (p *ClawNetAutoTaskPicker) setError(msg string) {
	p.mu.Lock()
	p.lastError = msg
	p.mu.Unlock()
}

// notifyChange calls the onChange callback if set.
func (p *ClawNetAutoTaskPicker) notifyChange() {
	p.mu.Lock()
	fn := p.onChange
	p.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// PickAndExecuteTask manually picks a specific task by ID: claim → execute → submit.
// Runs synchronously (Wails dispatches each binding call in its own goroutine).
// Returns a result map with "ok", "error", and progress info suitable for the frontend.
func (p *ClawNetAutoTaskPicker) PickAndExecuteTask(taskID string) (result map[string]interface{}) {
	// Recover from panics in the executor so the Wails call always returns.
	defer func() {
		if r := recover(); r != nil {
			result = map[string]interface{}{"ok": false, "error": fmt.Sprintf("panic: %v", r)}
		}
	}()

	p.mu.Lock()
	client := p.client
	executor := p.executor
	lang := p.lang

	// Check if already working on this task.
	if _, exists := p.activeTasks[taskID]; exists {
		p.mu.Unlock()
		return map[string]interface{}{"ok": false, "error": "task is already being processed"}
	}
	p.mu.Unlock()

	if executor == nil {
		return map[string]interface{}{"ok": false, "error": "executor not configured"}
	}
	if !client.IsRunning() {
		return map[string]interface{}{"ok": false, "error": "ClawNet is not running"}
	}

	// Fetch the task details.
	task, err := client.GetTask(taskID)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": clawnet.FormatTaskError("get_task", err, lang)}
	}
	// Allow picking tasks that are "open" or have been settled locally but are
	// still open on the network (the Hub is the source of truth for network tasks).
	normalizedStatus := strings.ToLower(task.Status)
	if normalizedStatus != "open" && normalizedStatus != "settled" && normalizedStatus != "" {
		return map[string]interface{}{"ok": false, "error": fmt.Sprintf("task status is '%s', only 'open' tasks can be picked", task.Status)}
	}

	run := &autoTaskRun{
		TaskID:    task.ID,
		Title:     task.Title,
		Reward:    task.Reward,
		Status:    "claiming",
		StartedAt: time.Now(),
	}

	p.mu.Lock()
	p.activeTasks[task.ID] = run
	p.mu.Unlock()
	p.notifyChange()

	// Step 1: Claim the task.
	if claimErr := client.ClaimTask(task.ID); claimErr != nil {
		if bidErr := client.BidOnTask(task.ID, 0, "manual pickup"); bidErr != nil {
			msg := clawnet.FormatTaskError("claim", claimErr, lang) + ", " + clawnet.FormatTaskError("bid", bidErr, lang)
			p.failTask(run, msg)
			return map[string]interface{}{"ok": false, "error": clawnet.FormatTaskError("claim", claimErr, lang)}
		}
	}

	// Step 2: Execute via agent.
	p.setRunStatus(run, "executing")
	description := task.Title
	if task.Description != "" {
		description = task.Title + "\n\n" + task.Description
	}
	execResult, execErr := executor(task.Title, description)
	if execErr != nil {
		msg := clawnet.FormatTaskError("execution", execErr, lang)
		p.failTask(run, msg)
		return map[string]interface{}{"ok": false, "error": msg}
	}
	if execResult == "" {
		execResult = "Task completed successfully."
	}

	p.mu.Lock()
	run.Result = execResult
	p.mu.Unlock()

	// Step 3: Submit the result.
	p.setRunStatus(run, "submitting")
	if submitErr := client.SubmitTaskResult(task.ID, execResult); submitErr != nil {
		msg := clawnet.FormatTaskError("submit", submitErr, lang)
		p.failTask(run, msg)
		return map[string]interface{}{"ok": false, "error": msg, "result": execResult}
	}

	// Success!
	p.mu.Lock()
	run.Status = "done"
	p.completedCount++
	p.totalEarned += task.Reward
	delete(p.activeTasks, task.ID)
	p.mu.Unlock()
	p.notifyChange()

	fmt.Printf("[manual-task-picker] ✅ completed task %q (reward: %.0f🐚)\n", task.Title, task.Reward)
	return map[string]interface{}{"ok": true, "result": execResult}
}

