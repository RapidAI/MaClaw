package im

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// ---------------------------------------------------------------------------
// IMTaskQueue — per-user background task queue for IM message processing
// ---------------------------------------------------------------------------

// DefaultQueueCapacity is the max number of pending tasks per user.
const DefaultQueueCapacity = 5

// taskIDCounter generates unique task IDs.
var taskIDCounter atomic.Uint64

// IMTask represents a single queued message that needs device routing.
type IMTask struct {
	ID       uint64
	TenantID string
	UserID   string
	// DispatchKey optionally selects an execution lane while UserID remains the
	// authoritative identity used for permissions, audit records, and delivery.
	// Hardware uses this to keep each bound device FIFO without making devices
	// owned by the same person wait on one another.
	DispatchKey        string
	PlatformName       string
	PlatformUID        string
	ReplyTarget        string
	MessageID          string // platform inbound id for reply decoration correlation
	MessageType        string
	Text               string
	Attachments        []MessageAttachment
	ClientCapabilities *agent.ClientCapabilities
	ClientTools        []agent.ClientToolDefinition
	ClientToolContext  *agent.ClientToolContext
	// StartMenu describes a confirmed /startmenu launch. The desktop client uses
	// it to create a task record and open a dedicated AI assistant tab before
	// executing the prompt.
	StartMenu  *StartMenuTaskLaunch
	EnqueuedAt time.Time
}

// StartMenuTaskLaunch contains only non-sensitive task setup information.
// Remote passwords are deliberately never synced from the welcome templates.
type StartMenuTaskLaunch struct {
	Title     string              `json:"title"`
	TaskText  string              `json:"task_text"`
	AgentMode string              `json:"agent_mode,omitempty"`
	CodingEnv *startMenuCodingEnv `json:"coding_env,omitempty"`
	Platform  string              `json:"platform"`
	TargetUID string              `json:"target_uid"`
}

type startMenuContextKey struct{}

func withStartMenuTask(ctx context.Context, launch *StartMenuTaskLaunch) context.Context {
	if launch == nil {
		return ctx
	}
	return context.WithValue(ctx, startMenuContextKey{}, launch)
}

func startMenuTaskFromContext(ctx context.Context) *StartMenuTaskLaunch {
	launch, _ := ctx.Value(startMenuContextKey{}).(*StartMenuTaskLaunch)
	return launch
}

// TaskQueueStats holds per-user queue statistics.
type TaskQueueStats struct {
	Pending  int
	Running  bool
	Capacity int
}

// userQueue is a per-user bounded task queue with a dedicated worker goroutine.
type userQueue struct {
	mu       sync.Mutex
	tasks    []*IMTask
	capacity int
	running  bool          // true when the worker is processing a task
	notify   chan struct{} // signals the worker that a new task is available
	stopCh   chan struct{}
	stopped  bool
}

func newUserQueue(capacity int) *userQueue {
	return &userQueue{
		capacity: capacity,
		notify:   make(chan struct{}, 1),
		stopCh:   make(chan struct{}),
	}
}

// enqueue adds a task. Returns (position, true) on success or (0, false) if full.
// Position is 1-based (1 = will run next, 2 = second in line, etc.).
func (q *userQueue) enqueue(task *IMTask) (int, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.tasks) >= q.capacity {
		return 0, false
	}
	q.tasks = append(q.tasks, task)
	pos := len(q.tasks)
	// Non-blocking signal to worker.
	select {
	case q.notify <- struct{}{}:
	default:
	}
	return pos, true
}

// dequeue pops the next task, or returns nil if empty.
func (q *userQueue) dequeue() *IMTask {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.tasks) == 0 {
		q.running = false
		return nil
	}
	task := q.tasks[0]
	q.tasks = q.tasks[1:]
	q.running = true
	return task
}

// stats returns current queue statistics.
func (q *userQueue) stats() TaskQueueStats {
	q.mu.Lock()
	defer q.mu.Unlock()
	return TaskQueueStats{
		Pending:  len(q.tasks),
		Running:  q.running,
		Capacity: q.capacity,
	}
}

// stop signals the worker to exit.
func (q *userQueue) stop() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.stopped {
		q.stopped = true
		close(q.stopCh)
	}
}

// hasWork returns true if there are pending tasks (used by worker drain check).
func (q *userQueue) hasWork() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.tasks) > 0
}

// ---------------------------------------------------------------------------
// IMTaskDispatcher — manages per-user queues and worker goroutines
// ---------------------------------------------------------------------------

// TaskExecutor is the function signature for executing a queued task.
// It receives the task and must deliver the result asynchronously
// (via deliverProgress / sendResponse). It should block until done.
type TaskExecutor func(ctx context.Context, task *IMTask) (*GenericResponse, error)

// IMTaskDispatcher manages per-user background task queues.
type IMTaskDispatcher struct {
	mu       sync.Mutex
	queues   map[string]*userQueue // userID → queue
	capacity int
	executor TaskExecutor

	// resultDelivery pushes the final response to the user via IM.
	resultDelivery func(ctx context.Context, userID, platformName, platformUID, replyTarget string, resp *GenericResponse)

	// idleTimeout controls how long an empty queue's worker stays alive.
	idleTimeout time.Duration
}

// NewIMTaskDispatcher creates a dispatcher with the given per-user capacity.
func NewIMTaskDispatcher(capacity int, executor TaskExecutor, delivery func(ctx context.Context, userID, platformName, platformUID, replyTarget string, resp *GenericResponse)) *IMTaskDispatcher {
	if capacity <= 0 {
		capacity = DefaultQueueCapacity
	}
	return &IMTaskDispatcher{
		queues:         make(map[string]*userQueue),
		capacity:       capacity,
		executor:       executor,
		resultDelivery: delivery,
		idleTimeout:    5 * time.Minute,
	}
}

// Enqueue adds a task for the user. Returns a user-facing status message.
// If the queue is full, returns an error response.
func (d *IMTaskDispatcher) Enqueue(task *IMTask) *GenericResponse {
	task.ID = taskIDCounter.Add(1)
	task.TenantID = normalizeTenantID(task.TenantID)
	task.EnqueuedAt = time.Now()
	queueKey := taskRuntimeQueueKey(task)

	d.mu.Lock()
	q, exists := d.queues[queueKey]
	if !exists {
		q = newUserQueue(d.capacity)
		d.queues[queueKey] = q
		go d.runWorker(queueKey, q)
	}
	d.mu.Unlock()

	pos, ok := q.enqueue(task)
	if !ok {
		return &GenericResponse{
			StatusCode: 429,
			StatusIcon: "info",
			Title:      "队列已满",
			Body:       fmt.Sprintf("当前有 %d 个任务排队中，请稍后再发。", d.capacity),
		}
	}

	stats := q.stats()
	if stats.Running {
		// There's a task currently executing; this one is queued.
		return &GenericResponse{
			StatusCode: 202,
			StatusIcon: "info",
			Title:      "已排队",
			Body:       fmt.Sprintf("已收到，排在第 %d 位，前面还有 %d 个任务。完成后会推送结果。", pos, pos-1),
		}
	}
	// Worker is idle, this task will start immediately.
	return &GenericResponse{
		StatusCode: 202,
		StatusIcon: "busy",
		Title:      "处理中",
		Body:       "已收到，正在处理…",
	}
}

// Stats returns queue statistics for a user.
func (d *IMTaskDispatcher) Stats(userID string) TaskQueueStats {
	return d.StatsForTenant("", userID)
}

func (d *IMTaskDispatcher) StatsForTenant(tenantID, userID string) TaskQueueStats {
	return d.statsForQueueKey(tenantUserRuntimeKey(tenantID, userID))
}

// StatsForTask returns queue statistics for the execution lane selected for a
// task. Callers should use it after Enqueue when the task has a DispatchKey.
func (d *IMTaskDispatcher) StatsForTask(task *IMTask) TaskQueueStats {
	if task == nil {
		return TaskQueueStats{Capacity: d.capacity}
	}
	return d.statsForQueueKey(taskRuntimeQueueKey(task))
}

func (d *IMTaskDispatcher) statsForQueueKey(queueKey string) TaskQueueStats {
	d.mu.Lock()
	q, exists := d.queues[queueKey]
	d.mu.Unlock()
	if !exists {
		return TaskQueueStats{Capacity: d.capacity}
	}
	return q.stats()
}

func taskRuntimeQueueKey(task *IMTask) string {
	if task == nil {
		return tenantUserRuntimeKey("", "")
	}
	if dispatchKey := strings.TrimSpace(task.DispatchKey); dispatchKey != "" {
		return tenantUserRuntimeKey(task.TenantID, "dispatch:"+dispatchKey)
	}
	return tenantUserRuntimeKey(task.TenantID, task.UserID)
}

// runWorker is the per-user worker goroutine. It processes tasks sequentially
// and exits after idleTimeout of inactivity.
func (d *IMTaskDispatcher) runWorker(userID string, q *userQueue) {
	log.Printf("[TaskDispatcher] worker started for user=%s", userID)
	defer func() {
		// Drain check: if tasks were enqueued between our last dequeue()
		// returning nil and this cleanup, re-register the queue and spawn
		// a new worker so those tasks aren't lost.
		d.mu.Lock()
		if q.hasWork() {
			// Tasks arrived after we decided to exit — respawn.
			go d.runWorker(userID, q)
			d.mu.Unlock()
			log.Printf("[TaskDispatcher] worker respawned for user=%s (drain check)", userID)
			return
		}
		delete(d.queues, userID)
		d.mu.Unlock()
		log.Printf("[TaskDispatcher] worker exited for user=%s", userID)
	}()

	idleTimer := time.NewTimer(d.idleTimeout)
	defer idleTimer.Stop()

	for {
		task := q.dequeue()
		if task != nil {
			// Reset idle timer while working.
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}

			d.executeTask(task)

			// Reset idle timer after task completion.
			idleTimer.Reset(d.idleTimeout)
			continue
		}

		// No tasks — wait for signal or idle timeout.
		select {
		case <-q.notify:
			continue
		case <-idleTimer.C:
			return
		case <-q.stopCh:
			return
		}
	}
}

// executeTask runs a single task and delivers the result.
// Panics in the executor are recovered so the worker goroutine survives.
func (d *IMTaskDispatcher) executeTask(task *IMTask) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultAgentTimeout+30*time.Second)
	defer cancel()
	ctx = WithTenant(ctx, task.TenantID)
	// Preserve inbound identity so gateway replies can pair @ / quote / refMsgId.
	ctx = WithReplyMeta(ctx, task.PlatformUID, task.MessageID)

	log.Printf("[TaskDispatcher] executing task #%d for user=%s text_len=%d", task.ID, task.UserID, len(task.Text))

	var resp *GenericResponse
	var err error

	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[TaskDispatcher] PANIC in task #%d for user=%s: %v", task.ID, task.UserID, r)
				err = fmt.Errorf("内部错误: %v", r)
			}
		}()
		resp, err = d.executor(ctx, task)
	}()

	if err != nil {
		resp = &GenericResponse{
			StatusCode: 500,
			StatusIcon: "error",
			Title:      "任务失败",
			Body:       fmt.Sprintf("处理失败: %s", err.Error()),
		}
	}
	if resp == nil {
		// Deferred response (e.g. media buffered) — nothing to deliver.
		return
	}

	d.resultDelivery(ctx, task.UserID, task.PlatformName, task.PlatformUID, task.ReplyTarget, resp)
}

// Shutdown stops all worker goroutines.
func (d *IMTaskDispatcher) Shutdown() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, q := range d.queues {
		q.stop()
	}
}
