package clawnet

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// AutoTaskPicker monitors the ClawNet network and automatically picks
// up tasks to execute via the maClaw agent, earning credits upon completion.
type AutoTaskPicker struct {
	mu       sync.Mutex
	running  bool
	stopCh   chan struct{}
	pollMu   sync.Mutex
	client   *Client
	hubURL   string
	executor AutoTaskExecutor
	onChange func()

	pollInterval   time.Duration
	maxConcurrent  int
	minReward      float64
	autoEnabled    bool
	preferredTags  []string
	lang           string // "zh" or "en" for error localisation

	activeTasks    map[string]*autoTaskRun
	completedCount int
	failedCount    int
	totalEarned    float64
	lastPollAt     time.Time
	lastError      string
}

type autoTaskRun struct {
	TaskID    string    `json:"task_id"`
	Title     string    `json:"title"`
	Reward    float64   `json:"reward"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
	Result    string    `json:"result,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// AutoTaskExecutor is called to execute a task.
type AutoTaskExecutor func(taskTitle, taskDescription string) (result string, err error)

// NewAutoTaskPicker creates a new auto task picker.
func NewAutoTaskPicker(client *Client, hubURL string) *AutoTaskPicker {
	return &AutoTaskPicker{
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

func (p *AutoTaskPicker) SetExecutor(fn AutoTaskExecutor) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.executor = fn
}

func (p *AutoTaskPicker) SetOnChange(fn func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onChange = fn
}

// SetLang sets the language for error messages ("zh" or "en").
func (p *AutoTaskPicker) SetLang(lang string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lang = lang
}

func (p *AutoTaskPicker) Configure(enabled bool, pollMinutes int, minReward float64, tags []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.autoEnabled = enabled
	if pollMinutes > 0 {
		p.pollInterval = time.Duration(pollMinutes) * time.Minute
	}
	p.minReward = minReward
	p.preferredTags = tags
}

func (p *AutoTaskPicker) IsEnabled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.autoEnabled
}

func (p *AutoTaskPicker) Start() {
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

func (p *AutoTaskPicker) Stop() {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return
	}
	p.running = false
	close(p.stopCh)
	p.mu.Unlock()
}

func (p *AutoTaskPicker) GetStatus() map[string]interface{} {
	p.mu.Lock()
	defer p.mu.Unlock()

	active := make([]map[string]interface{}, 0, len(p.activeTasks))
	for _, r := range p.activeTasks {
		active = append(active, map[string]interface{}{
			"task_id": r.TaskID, "title": r.Title, "reward": r.Reward,
			"status": r.Status, "started_at": r.StartedAt.Format(time.RFC3339),
		})
	}
	return map[string]interface{}{
		"enabled": p.autoEnabled, "running": p.running,
		"poll_interval": int(p.pollInterval.Minutes()), "min_reward": p.minReward,
		"preferred_tags": p.preferredTags, "active_tasks": active,
		"completed_count": p.completedCount, "failed_count": p.failedCount,
		"total_earned": p.totalEarned,
		"last_poll_at": formatTimeOrEmpty(p.lastPollAt), "last_error": p.lastError,
	}
}

func formatTimeOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func (p *AutoTaskPicker) loop() {
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

func (p *AutoTaskPicker) pollAndPickTask() {
	if !p.pollMu.TryLock() {
		return
	}
	defer p.pollMu.Unlock()

	p.mu.Lock()
	p.lastPollAt = time.Now()
	p.lastError = ""
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
	if !client.IsRunning() {
		return
	}

	tasks, err := p.discoverTasks(client, hubURL)
	if err != nil {
		p.setError(fmt.Sprintf("task discovery failed: %v", err))
		return
	}
	if len(tasks) == 0 {
		return
	}

	selected := p.selectTask(tasks, minReward, preferredTags)
	if selected == nil {
		return
	}
	go p.executeTask(client, selected, executor)
}

func (p *AutoTaskPicker) discoverTasks(client *Client, hubURL string) ([]Task, error) {
	matched, err := client.MatchTasks()
	if err == nil && len(matched) > 0 {
		var open []Task
		for _, t := range matched {
			if t.TaskStatus == "open" {
				open = append(open, t)
			}
		}
		if len(open) > 0 {
			return open, nil
		}
	}
	if hubURL == "" {
		return nil, nil
	}
	netTasks, err := client.BrowseHubTasks(hubURL)
	if err != nil {
		return nil, err
	}
	var open []Task
	for _, t := range netTasks {
		if t.TaskStatus == "open" {
			open = append(open, t)
		}
	}
	return open, nil
}

func (p *AutoTaskPicker) selectTask(tasks []Task, minReward float64, preferredTags []string) *Task {
	if len(tasks) == 0 {
		return nil
	}
	var candidates []Task
	for _, t := range tasks {
		if t.Reward >= minReward {
			candidates = append(candidates, t)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	p.mu.Lock()
	activeIDs := make(map[string]bool)
	for id := range p.activeTasks {
		activeIDs[id] = true
	}
	p.mu.Unlock()

	var filtered []Task
	for _, t := range candidates {
		if !activeIDs[t.ID] {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) == 0 {
		return nil
	}

	tagSet := make(map[string]bool)
	for _, tag := range preferredTags {
		tagSet[strings.ToLower(tag)] = true
	}

	type scored struct {
		task  Task
		score float64
	}
	var scoredTasks []scored
	for _, t := range filtered {
		s := t.Reward
		for _, tag := range t.Tags {
			if tagSet[strings.ToLower(tag)] {
				s += 50
			}
		}
		scoredTasks = append(scoredTasks, scored{task: t, score: s})
	}

	best := scoredTasks[0]
	for _, st := range scoredTasks[1:] {
		if st.score > best.score || (st.score == best.score && rand.Float64() > 0.5) {
			best = st
		}
	}
	result := best.task
	return &result
}

func (p *AutoTaskPicker) executeTask(client *Client, task *Task, executor AutoTaskExecutor) {
	p.mu.Lock()
	lang := p.lang
	p.mu.Unlock()

	run := &autoTaskRun{
		TaskID: task.ID, Title: task.Title, Reward: task.Reward,
		Status: "claiming", StartedAt: time.Now(),
	}
	p.mu.Lock()
	p.activeTasks[task.ID] = run
	p.mu.Unlock()
	p.notifyChange()

	if err := client.ClaimTask(task.ID); err != nil {
		if bidErr := client.BidOnTask(task.ID, 0, "maClaw auto-pickup: I can help with this"); bidErr != nil {
			p.failTask(run, FormatTaskError("claim", err, lang)+", "+FormatTaskError("bid", bidErr, lang))
			return
		}
	}

	p.setRunStatus(run, "executing")
	description := task.Title
	if task.Description != "" {
		description = task.Title + "\n\n" + task.Description
	}
	result, err := executor(task.Title, description)
	if err != nil {
		p.failTask(run, FormatTaskError("execution", err, lang))
		return
	}
	if result == "" {
		result = "Task completed successfully."
	}
	p.mu.Lock()
	run.Result = result
	p.mu.Unlock()

	p.setRunStatus(run, "submitting")
	if err := client.SubmitTaskResult(task.ID, result); err != nil {
		p.failTask(run, FormatTaskError("submit", err, lang))
		return
	}

	p.mu.Lock()
	run.Status = "done"
	p.completedCount++
	p.totalEarned += task.Reward
	delete(p.activeTasks, task.ID)
	p.mu.Unlock()
	p.notifyChange()
	fmt.Printf("[auto-task-picker] ✅ completed task %q (reward: %.0f🐚)\n", task.Title, task.Reward)
}

// PickAndExecuteTask manually picks a specific task by ID.
func (p *AutoTaskPicker) PickAndExecuteTask(taskID string) (result map[string]interface{}) {
	defer func() {
		if r := recover(); r != nil {
			result = map[string]interface{}{"ok": false, "error": fmt.Sprintf("panic: %v", r)}
		}
	}()

	p.mu.Lock()
	client := p.client
	executor := p.executor
	lang := p.lang
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

	task, err := client.GetTask(taskID)
	if err != nil {
		msg := FormatTaskError("get_task", err, lang)
		return map[string]interface{}{"ok": false, "error": msg}
	}
	normalizedStatus := strings.ToLower(task.TaskStatus)
	if normalizedStatus != "open" && normalizedStatus != "settled" && normalizedStatus != "" {
		return map[string]interface{}{"ok": false, "error": fmt.Sprintf("task status is '%s', only 'open' tasks can be picked", task.TaskStatus)}
	}

	run := &autoTaskRun{
		TaskID: task.ID, Title: task.Title, Reward: task.Reward,
		Status: "claiming", StartedAt: time.Now(),
	}
	p.mu.Lock()
	p.activeTasks[task.ID] = run
	p.mu.Unlock()
	p.notifyChange()

	if claimErr := client.ClaimTask(task.ID); claimErr != nil {
		if bidErr := client.BidOnTask(task.ID, 0, "manual pickup"); bidErr != nil {
			msg := FormatTaskError("claim", claimErr, lang) + ", " + FormatTaskError("bid", bidErr, lang)
			p.failTask(run, msg)
			return map[string]interface{}{"ok": false, "error": FormatTaskError("claim", claimErr, lang)}
		}
	}

	p.setRunStatus(run, "executing")
	description := task.Title
	if task.Description != "" {
		description = task.Title + "\n\n" + task.Description
	}
	execResult, execErr := executor(task.Title, description)
	if execErr != nil {
		msg := FormatTaskError("execution", execErr, lang)
		p.failTask(run, msg)
		return map[string]interface{}{"ok": false, "error": msg}
	}
	if execResult == "" {
		execResult = "Task completed successfully."
	}
	p.mu.Lock()
	run.Result = execResult
	p.mu.Unlock()

	p.setRunStatus(run, "submitting")
	if submitErr := client.SubmitTaskResult(task.ID, execResult); submitErr != nil {
		msg := FormatTaskError("submit", submitErr, lang)
		p.failTask(run, msg)
		return map[string]interface{}{"ok": false, "error": msg, "result": execResult}
	}

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

func (p *AutoTaskPicker) setRunStatus(run *autoTaskRun, status string) {
	p.mu.Lock()
	run.Status = status
	p.mu.Unlock()
	p.notifyChange()
}

func (p *AutoTaskPicker) failTask(run *autoTaskRun, errMsg string) {
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

func (p *AutoTaskPicker) setError(msg string) {
	p.mu.Lock()
	p.lastError = msg
	p.mu.Unlock()
}

func (p *AutoTaskPicker) notifyChange() {
	p.mu.Lock()
	fn := p.onChange
	p.mu.Unlock()
	if fn != nil {
		fn()
	}
}
