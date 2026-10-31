package guiapp

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

const (
	chatTaskLLMCaller    = "chat_task_loop"
	chatTaskResultSource = "chat_task_result"
	chatTaskEventName    = "ai-assistant-task"
	chatTaskStoreFile    = "chat-tasks.json"
	// Kept so a test can still prove an explicit budget clamps. Desktop chat
	// does not assign it: a call count is not a stop.
	desktopChatForegroundIterationBudget = 4
	desktopChatInlineToolRounds          = 2
	chatTaskPerSessionLimit              = 3
)

type chatTaskStatus string

const (
	chatTaskQueued      chatTaskStatus = "queued"
	chatTaskRunning     chatTaskStatus = "running"
	chatTaskNeedsInput  chatTaskStatus = "needs_input"
	chatTaskDone        chatTaskStatus = "done"
	chatTaskFailed      chatTaskStatus = "failed"
	chatTaskCanceled    chatTaskStatus = "canceled"
	chatTaskInterrupted chatTaskStatus = "interrupted"
)

// AIAssistantChatTask is the desktop-facing snapshot of one background chat task.
// The running loop keeps its own conversation under desktop-user:task:<id>
// (or the project session plus :task:<id>) so it cannot replace the chat transcript.
type AIAssistantChatTask struct {
	ID            string   `json:"id"`
	SessionKey    string   `json:"session_key"`
	Title         string   `json:"title"`
	Prompt        string   `json:"prompt,omitempty"`
	Status        string   `json:"status"`
	Progress      string   `json:"progress,omitempty"`
	Question      string   `json:"question,omitempty"`
	ResultText    string   `json:"result_text,omitempty"`
	Error         string   `json:"error,omitempty"`
	ArtifactPaths []string `json:"artifact_paths,omitempty"`
	WhereWeAre    string   `json:"where_we_are,omitempty"`
	NextStep      string   `json:"next_step,omitempty"`
	OpenProblems  string   `json:"open_problems,omitempty"`
	ProgressPath  string   `json:"progress_path,omitempty"`
	CreatedAtUnix int64    `json:"created_at_unix"`
	UpdatedAtUnix int64    `json:"updated_at_unix"`
	Recovered     bool     `json:"recovered,omitempty"`
}

type AIAssistantChatTaskDispatchRequest struct {
	Text        string `json:"text"`
	ProjectPath string `json:"project_path,omitempty"`
	SessionKey  string `json:"session_key,omitempty"`
	Lang        string `json:"lang,omitempty"`
	// Title is the label the person sees. Empty uses the start of Text.
	Title string `json:"title,omitempty"`
	// Computer marks the one computer-use helper for Screen.
	Computer bool   `json:"-"`
	Screen   string `json:"-"`
}

type chatTaskRecord struct {
	ID            string
	SessionKey    string
	MemoryKey     string
	Title         string
	Prompt        string
	Status        chatTaskStatus
	Progress      string
	Question      string
	ResultText    string
	Error         string
	ArtifactPaths []string
	WhereWeAre    string
	NextStep      string
	OpenProblems  string
	ProgressPath  string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Recovered     bool

	loop         *LoopContext
	resumePrompt string
	answerCh     chan string
	cancelCh     chan struct{}
	cancelOnce   sync.Once
	Computer     bool
	Screen       string
}

type chatTaskRunResult struct {
	Text      string
	Artifacts []string
	Err       error
}

type chatTaskExecutor func(h *IMMessageHandler, task *chatTaskRecord) chatTaskRunResult

type chatTaskRegistry struct {
	handler   *IMMessageHandler
	storePath string
	executor  chatTaskExecutor

	mu        sync.Mutex
	persistMu sync.Mutex
	tasks     map[string]*chatTaskRecord
	nextID    uint64

	dbOnce sync.Once
	db     *sql.DB
	dbErr  error

	// taskSig is the last committed row signature. The first persist reads the
	// table once; later persists write only rows whose signature changed.
	// The map is replaced after the transaction commits.
	taskSigReady bool
	taskSig      map[string]chatTaskSig
	// tasksLoaded is set only after a complete read. An unread registry must
	// not persist, because an empty list would delete every stored row.
	tasksLoaded bool
}

type chatTaskDisk struct {
	Tasks      []AIAssistantChatTask `json:"tasks"`
	Generation int64                 `json:"generation,omitempty"`
}

func chatTaskMemoryOwner(sessionKey, taskID string) string {
	return strings.TrimSpace(sessionKey) + ":task:" + strings.TrimSpace(taskID)
}

func isChatTaskMemoryOwner(userID string) bool {
	return strings.Contains(userID, ":task:")
}

func chatTaskSessionAllowed(sessionKey string, codingWorkbench bool) bool {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" || codingWorkbench || isChatTaskMemoryOwner(sessionKey) {
		return false
	}
	if sessionKey == desktopUserID {
		return true
	}
	if !strings.HasPrefix(sessionKey, desktopUserID+":") {
		return false
	}
	rest := strings.TrimPrefix(sessionKey, desktopUserID+":")
	if strings.HasPrefix(rest, "expert:") || strings.HasPrefix(rest, "acp:") || strings.HasPrefix(rest, "task:") {
		return false
	}
	return true
}

func (s chatTaskStatus) terminal() bool {
	switch s {
	case chatTaskDone, chatTaskFailed, chatTaskCanceled, chatTaskInterrupted:
		return true
	default:
		return false
	}
}

func (h *IMMessageHandler) ensureChatTasks() *chatTaskRegistry {
	if h == nil {
		return nil
	}
	h.chatTasksOnce.Do(func() {
		dir := ""
		if h.app != nil {
			dir = strings.TrimSpace(h.app.GetDataDir())
		}
		h.chatTasks = newChatTaskRegistry(h, dir)
	})
	return h.chatTasks
}

func newChatTaskRegistry(h *IMMessageHandler, dataDir string) *chatTaskRegistry {
	path := ""
	if strings.TrimSpace(dataDir) != "" {
		path = filepath.Join(dataDir, chatTaskStoreFile)
	}
	reg := &chatTaskRegistry{
		handler:   h,
		storePath: path,
		tasks:     map[string]*chatTaskRecord{},
	}
	reg.load()
	return reg
}

func (r *chatTaskRegistry) Dispatch(req AIAssistantChatTaskDispatchRequest) (AIAssistantChatTask, error) {
	if r == nil {
		return AIAssistantChatTask{}, fmt.Errorf("chat tasks are unavailable")
	}
	prompt := strings.TrimSpace(req.Text)
	if prompt == "" {
		return AIAssistantChatTask{}, fmt.Errorf("task text is empty")
	}
	sessionKey := resolveChatTaskSessionKey(req.SessionKey, req.ProjectPath)
	if !chatTaskSessionAllowed(sessionKey, r.codingWorkbench(sessionKey)) {
		return AIAssistantChatTask{}, fmt.Errorf("background chat tasks are only available in the local assistant and project chat")
	}
	now := time.Now()
	r.mu.Lock()
	if r.activeCountLocked(sessionKey) >= chatTaskPerSessionLimit {
		r.mu.Unlock()
		return AIAssistantChatTask{}, fmt.Errorf("this chat already has %d background tasks", chatTaskPerSessionLimit)
	}
	r.nextID++
	id := fmt.Sprintf("ct-%d-%d", now.UnixNano(), r.nextID)
	task := &chatTaskRecord{
		ID:         id,
		SessionKey: sessionKey,
		MemoryKey:  chatTaskMemoryOwner(sessionKey, id),
		Title:      chatTaskDispatchTitle(req.Title, prompt),
		Prompt:     prompt,
		Status:     chatTaskQueued,
		CreatedAt:  now,
		UpdatedAt:  now,
		answerCh:   make(chan string, 1),
		cancelCh:   make(chan struct{}),
		Computer:   req.Computer,
		Screen:     strings.TrimSpace(req.Screen),
	}
	r.tasks[id] = task
	snap := task.snapshotLocked()
	r.mu.Unlock()
	r.persist()
	r.emit(snap)
	go r.run(task)
	return snap, nil
}

func (r *chatTaskRegistry) Resume(taskID, text string) (AIAssistantChatTask, error) {
	if r == nil {
		return AIAssistantChatTask{}, fmt.Errorf("chat tasks are unavailable")
	}
	r.mu.Lock()
	task := r.tasks[strings.TrimSpace(taskID)]
	if task == nil {
		r.mu.Unlock()
		return AIAssistantChatTask{}, fmt.Errorf("chat task not found")
	}
	if task.Status != chatTaskInterrupted {
		r.mu.Unlock()
		return AIAssistantChatTask{}, fmt.Errorf("chat task is not interrupted")
	}
	task.resumePrompt = chatTaskResumePrompt(task, text)
	task.Status = chatTaskQueued
	task.Question = ""
	task.Error = ""
	task.loop = nil
	task.answerCh = make(chan string, 1)
	task.cancelCh = make(chan struct{})
	task.cancelOnce = sync.Once{}
	task.UpdatedAt = time.Now()
	snap := task.snapshotLocked()
	r.mu.Unlock()
	r.persist()
	r.emit(snap)
	go r.run(task)
	return snap, nil
}

func chatTaskResumePrompt(task *chatTaskRecord, text string) string {
	if task == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("Continue the original background task from its saved conversation and progress. Do not restart it or repeat completed side effects.\nOriginal task: ")
	b.WriteString(task.Prompt)
	if task.WhereWeAre != "" {
		b.WriteString("\nCurrent progress: ")
		b.WriteString(task.WhereWeAre)
	}
	if task.NextStep != "" {
		b.WriteString("\nNext step: ")
		b.WriteString(task.NextStep)
	}
	if task.OpenProblems != "" {
		b.WriteString("\nOpen problems: ")
		b.WriteString(task.OpenProblems)
	}
	if task.Question != "" {
		b.WriteString("\nQuestion waiting before restart: ")
		b.WriteString(task.Question)
	}
	if strings.TrimSpace(text) != "" {
		b.WriteString("\nUser continuation/answer: ")
		b.WriteString(strings.TrimSpace(text))
	}
	return b.String()
}

func (r *chatTaskRegistry) List(sessionKey string) []AIAssistantChatTask {
	if r == nil {
		return nil
	}
	sessionKey = strings.TrimSpace(sessionKey)
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]AIAssistantChatTask, 0, len(r.tasks))
	for _, task := range r.tasks {
		if sessionKey != "" && !sameChatSession(task.SessionKey, sessionKey) {
			continue
		}
		out = append(out, task.snapshotLocked())
	}
	return out
}

func (r *chatTaskRegistry) Cancel(taskID string) error {
	task, loop, snap, ok := r.finishLocked(taskID, chatTaskCanceled, "", nil, "")
	if !ok {
		if task == nil {
			return fmt.Errorf("chat task not found")
		}
		return nil
	}
	r.publishTerminal(task, loop, snap, chatTaskCanceled)
	return nil
}

func (r *chatTaskRegistry) CancelSession(sessionKey string) {
	if r == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	r.mu.Lock()
	var ids []string
	for id, task := range r.tasks {
		if sameChatSession(task.SessionKey, sessionKey) && !task.Status.terminal() {
			ids = append(ids, id)
		}
	}
	r.mu.Unlock()
	for _, id := range ids {
		_ = r.Cancel(id)
	}
}

func (r *chatTaskRegistry) Answer(taskID, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("answer is empty")
	}
	r.mu.Lock()
	task := r.tasks[strings.TrimSpace(taskID)]
	if task == nil {
		r.mu.Unlock()
		return fmt.Errorf("chat task not found")
	}
	interrupted := task.Status == chatTaskInterrupted && strings.TrimSpace(task.Question) != ""
	r.mu.Unlock()
	if interrupted {
		_, err := r.Resume(taskID, text)
		return err
	}
	r.mu.Lock()
	task = r.tasks[strings.TrimSpace(taskID)]
	if task == nil || task.Status != chatTaskNeedsInput {
		r.mu.Unlock()
		return fmt.Errorf("chat task is not waiting for input")
	}
	select {
	case task.answerCh <- text:
	default:
		r.mu.Unlock()
		return fmt.Errorf("chat task already has a pending answer")
	}
	task.Status = chatTaskRunning
	task.Question = ""
	task.UpdatedAt = time.Now()
	snap := task.snapshotLocked()
	r.mu.Unlock()
	r.persist()
	r.emit(snap)
	return nil
}

func (r *chatTaskRegistry) Steer(taskID, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("steer text is empty")
	}
	r.mu.Lock()
	task := r.tasks[strings.TrimSpace(taskID)]
	if task == nil {
		r.mu.Unlock()
		return fmt.Errorf("chat task not found")
	}
	if task.Status == chatTaskInterrupted {
		r.mu.Unlock()
		_, err := r.Resume(taskID, text)
		return err
	}
	if task.Status.terminal() {
		r.mu.Unlock()
		return fmt.Errorf("chat task is not running")
	}
	if task.Status == chatTaskNeedsInput {
		r.mu.Unlock()
		return r.Answer(taskID, text)
	}
	loop := task.loop
	memoryKey := task.MemoryKey
	r.mu.Unlock()
	if !r.deliverSteer(loop, memoryKey, text) {
		return fmt.Errorf("chat task is not running")
	}
	return nil
}

type chatTaskOpenContinue int

const (
	// chatTaskOpenContinueNone means the caller may start a task. Nothing is
	// running, or the only open task has already committed its reply and
	// will not read a steer.
	chatTaskOpenContinueNone chatTaskOpenContinue = iota
	// chatTaskOpenContinueSteered means the single running task received
	// the continuation.
	chatTaskOpenContinueSteered
	// chatTaskOpenContinueHeld means the host must not guess: more than
	// one task is open, or the only one is waiting for the user.
	chatTaskOpenContinueHeld
)

// continueOpenChatTask attaches a host continuation to the single running
// task. A task that is waiting for the user is not answered. The person's
// steer path still answers that question; this one must not.
func (r *chatTaskRegistry) continueOpenChatTask(sessionKey, text, title string) (chatTaskOpenContinue, error) {
	if r == nil {
		return chatTaskOpenContinueHeld, fmt.Errorf("chat tasks are unavailable")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return chatTaskOpenContinueHeld, fmt.Errorf("steer text is empty")
	}
	sessionKey = strings.TrimSpace(sessionKey)
	r.mu.Lock()
	var open []*chatTaskRecord
	for _, task := range r.tasks {
		if sameChatSession(task.SessionKey, sessionKey) && !task.Status.terminal() {
			open = append(open, task)
		}
	}
	if len(open) == 0 {
		r.mu.Unlock()
		return chatTaskOpenContinueNone, nil
	}
	if len(open) != 1 || open[0].Status == chatTaskNeedsInput {
		r.mu.Unlock()
		return chatTaskOpenContinueHeld, nil
	}
	task := open[0]
	taskID := task.ID
	loop := task.loop
	memoryKey := task.MemoryKey
	r.mu.Unlock()
	// A loop that has committed its final reply will not read a steer. Tell
	// the caller nothing took the note, so it can start a task. Returning
	// success here would tell the user the work is continuing after the note
	// was dropped.
	if !r.deliverSteer(loop, memoryKey, text) {
		return chatTaskOpenContinueNone, nil
	}
	if title = strings.TrimSpace(title); title != "" {
		r.renameOpenTask(taskID, title)
	}
	return chatTaskOpenContinueSteered, nil
}

func (r *chatTaskRegistry) renameOpenTask(id, title string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	current := r.tasks[id]
	if current == nil || current.Status.terminal() {
		r.mu.Unlock()
		return
	}
	current.Title = title
	current.UpdatedAt = time.Now()
	snap := current.snapshotLocked()
	r.mu.Unlock()
	r.persist()
	r.emit(snap)
}

func (r *chatTaskRegistry) deliverSteer(loop *LoopContext, memoryKey, text string) bool {
	if r == nil || r.handler == nil {
		return false
	}
	note := "[用户补充] " + text
	if loop == nil {
		r.handler.accumulateInjection(memoryKey, note)
		return true
	}
	if loop.IsCancelled() {
		return false
	}
	_, accepted := loop.TryRequestReplan(func() {
		r.handler.accumulateInjection(memoryKey, note)
	})
	return accepted
}

func (r *chatTaskRegistry) requestInput(taskID, question string) (string, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return "", fmt.Errorf("question is empty")
	}
	r.mu.Lock()
	task := r.tasks[strings.TrimSpace(taskID)]
	if task == nil || task.Status.terminal() {
		r.mu.Unlock()
		return "", fmt.Errorf("chat task is not running")
	}
	task.Status = chatTaskNeedsInput
	task.Question = question
	task.UpdatedAt = time.Now()
	ch := task.answerCh
	loop := task.loop
	cancelWait := task.cancelCh
	snap := task.snapshotLocked()
	r.mu.Unlock()
	r.persist()
	r.emit(snap)
	var loopCancel <-chan struct{}
	if loop != nil {
		loopCancel = loop.CancelC
	}
	select {
	case ans, ok := <-ch:
		if !ok || strings.TrimSpace(ans) == "" {
			return "", fmt.Errorf("canceled")
		}
		return strings.TrimSpace(ans), nil
	case <-cancelWait:
		return "", fmt.Errorf("canceled")
	case <-loopCancel:
		return "", fmt.Errorf("canceled")
	}
}

func (r *chatTaskRegistry) run(task *chatTaskRecord) {
	defer func() {
		if rec := recover(); rec != nil {
			r.finish(task.ID, chatTaskRunResult{Err: fmt.Errorf("chat task panic: %v", rec)})
		}
	}()
	if r.isTerminal(task.ID) {
		return
	}
	var slot *LoopContext
	releaseSlot := func() {
		if slot == nil || r.handler == nil || r.handler.bgManager == nil {
			return
		}
		r.handler.bgManager.Complete(slot.ID)
		slot = nil
	}
	if r.handler != nil && r.handler.bgManager != nil {
		acquired, waitC := r.handler.bgManager.SpawnOrQueue(SlotKindChatTask, task.MemoryKey, task.Title, 1)
		slot = acquired
		if slot == nil && waitC != nil {
			slot = r.waitChatTaskSlot(task, waitC)
			if slot == nil {
				return
			}
		}
	}
	if r.isTerminal(task.ID) || !r.mark(task.ID, chatTaskRunning, "") {
		releaseSlot()
		return
	}
	defer releaseSlot()
	r.storeTaskProgress(task.ID, task.Title, firstProgressLine(task.Prompt), desktopbot.ProgressOpenNone)
	exec := r.executor
	if exec == nil {
		exec = (*IMMessageHandler).executeChatTaskAgentLoop
	}
	r.finish(task.ID, exec(r.handler, task))
}

// waitChatTaskSlot blocks until a chat-task slot opens, the task is canceled,
// or the queue wait expires. A nil return means the caller must not start work;
// the task is already terminal or has been finished with a slot error.
func (r *chatTaskRegistry) waitChatTaskSlot(task *chatTaskRecord, waitC <-chan *LoopContext) *LoopContext {
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	var cancelWait <-chan struct{}
	if task != nil {
		cancelWait = task.cancelCh
	}
	select {
	case slot := <-waitC:
		return slot
	case <-cancelWait:
		if slot := r.abandonQueuedSlot(waitC); slot != nil && r.handler != nil && r.handler.bgManager != nil {
			r.handler.bgManager.Complete(slot.ID)
		}
		return nil
	case <-timer.C:
		if slot := r.abandonQueuedSlot(waitC); slot != nil {
			return slot
		}
		if !r.isTerminal(task.ID) {
			r.finish(task.ID, chatTaskRunResult{Err: fmt.Errorf("no background slot")})
		}
		return nil
	}
}

// abandonQueuedSlot drops a queued waiter. Complete hands the next slot off
// after releasing its lock, so CancelPending can miss a task that was already
// dequeued. A slot waiting in the buffer is returned to the caller. A slot
// that arrives after this returns is completed by a short drain so it cannot
// occupy the chat-task limit forever.
func (r *chatTaskRegistry) abandonQueuedSlot(waitC <-chan *LoopContext) *LoopContext {
	if r == nil || r.handler == nil || r.handler.bgManager == nil || waitC == nil {
		return nil
	}
	mgr := r.handler.bgManager
	mgr.CancelPending(SlotKindChatTask, waitC)
	select {
	case slot := <-waitC:
		return slot
	default:
	}
	go func() {
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		select {
		case slot := <-waitC:
			if slot != nil {
				mgr.Complete(slot.ID)
			}
		case <-timer.C:
		}
	}()
	return nil
}

func (h *IMMessageHandler) executeChatTaskAgentLoop(task *chatTaskRecord) chatTaskRunResult {
	if h == nil || task == nil {
		return chatTaskRunResult{Err: fmt.Errorf("chat task handler is missing")}
	}
	h.openHelperMemory(task, false)
	text := task.Prompt
	if strings.TrimSpace(task.resumePrompt) != "" {
		text = task.resumePrompt
	}
	var lastLoop *LoopContext
	var lastResp *IMAgentResponse
	for wave := 0; wave < sessionRoundContinuationMaxWaves; wave++ {
		if wave > 0 {
			h.bindPendingMaxRoundsSlot(task.MemoryKey)
			text = sessionRoundContinuationPrompt(task.Prompt)
		}
		maxIter := h.getMaclawAgentMaxIterations()
		if maxIter <= 0 {
			maxIter = 30
		}
		loop := NewLoopContext(fmt.Sprintf("chat-task-%s-%d", task.ID, wave), maxIter, h.taskClient)
		loop.LLMCaller = chatTaskLLMCaller
		loop.ChatTaskID = task.ID
		if task.Computer {
			loop.ComputerHelper = true
			loop.ComputerUseActive = true
			loop.ComputerUseGateSettled = true
			loop.ComputerUseFresh = true
			loop.ComputerUseRoutingText = task.Prompt
		}
		loop.UserID = task.MemoryKey
		loop.Platform = desktopPlatform
		loop.Description = task.Title
		h.bindChatTaskLoop(task.ID, loop)
		lastLoop = loop
		lastResp = h.HandleIMMessageWithExistingLoop(IMUserMessage{
			RequestID: fmt.Sprintf("chat-task-%s-%d", task.ID, wave),
			UserID:    task.MemoryKey,
			Platform:  desktopPlatform,
			Text:      text,
		}, loop, func(progress string) {
			milestone, ok := chatTaskProgressMilestone(progress)
			if !ok {
				return
			}
			if reg := h.ensureChatTasks(); reg != nil {
				reg.noteProgress(task.ID, milestone)
			}
		}, nil, nil, nil)
		if loop.IsCancelled() {
			return chatTaskRunResult{Err: fmt.Errorf("canceled")}
		}
		if lastResp == nil || !lastResp.sessionRoundContinue {
			return chatTaskResultFromAgentResponse(lastResp, loop)
		}
	}
	if lastResp != nil {
		lastResp.sessionRoundContinue = false
		lastResp.Text = sessionRoundManualContinueText
	}
	return chatTaskResultFromAgentResponse(lastResp, lastLoop)
}

func chatTaskResultFromAgentResponse(resp *IMAgentResponse, loop *LoopContext) chatTaskRunResult {
	if resp == nil {
		return chatTaskRunResult{Err: fmt.Errorf("chat task returned no result")}
	}
	text := strings.TrimSpace(resp.Text)
	artifacts := chatTaskArtifacts(resp, loop)
	if errText := strings.TrimSpace(resp.Error); errText != "" {
		return chatTaskRunResult{Err: fmt.Errorf("%s", errText), Text: text, Artifacts: artifacts}
	}
	shown, err := desktopbot.CollapseHelperReport(text, artifacts)
	if err != nil {
		return chatTaskRunResult{Err: err, Artifacts: artifacts}
	}
	return chatTaskRunResult{Text: shown, Artifacts: artifacts}
}

// openHelperMemory starts a helper without the main session.
// A new id stays empty. The same id keeps the helper's own entries.
func (h *IMMessageHandler) openHelperMemory(task *chatTaskRecord, sameID bool) {
	if h == nil || h.memory == nil || task == nil {
		return
	}
	var own []string
	if sameID {
		for _, entry := range h.memory.Load(task.MemoryKey) {
			text, _ := entry.Content.(string)
			if strings.TrimSpace(text) != "" {
				own = append(own, text)
			}
		}
	}
	_ = desktopbot.HelperStartEntries(nil, own, sameID)
}

func (h *IMMessageHandler) bindChatTaskLoop(taskID string, loop *LoopContext) {
	reg := h.ensureChatTasks()
	if reg == nil || loop == nil {
		return
	}
	reg.mu.Lock()
	task := reg.tasks[taskID]
	if task == nil {
		reg.mu.Unlock()
		loop.Cancel()
		return
	}
	task.loop = loop
	alreadyStopped := task.Status.terminal()
	reg.mu.Unlock()
	// Cancel can win before the loop pointer is published. Stop the loop
	// here so a late bind cannot keep running after the task is terminal.
	if alreadyStopped {
		loop.Cancel()
	}
}

func (r *chatTaskRegistry) mark(id string, status chatTaskStatus, progress string) bool {
	r.mu.Lock()
	task := r.tasks[id]
	if task == nil || task.Status.terminal() {
		r.mu.Unlock()
		return false
	}
	task.Status = status
	if strings.TrimSpace(progress) != "" {
		task.Progress = truncateChatTaskText(progress, 240)
	}
	task.UpdatedAt = time.Now()
	snap := task.snapshotLocked()
	r.mu.Unlock()
	r.persist()
	r.emit(snap)
	return true
}

func (r *chatTaskRegistry) noteProgress(id, text string) {
	text = truncateChatTaskText(strings.TrimSpace(text), 240)
	if text == "" {
		return
	}
	r.mu.Lock()
	task := r.tasks[id]
	if task == nil || task.Status.terminal() {
		r.mu.Unlock()
		return
	}
	next := task.NextStep
	open := task.OpenProblems
	r.mu.Unlock()
	r.storeTaskProgress(id, text, next, open)
	r.mu.Lock()
	task = r.tasks[id]
	if task == nil || task.Status.terminal() {
		r.mu.Unlock()
		return
	}
	task.Progress = text
	task.UpdatedAt = time.Now()
	snap := task.snapshotLocked()
	r.mu.Unlock()
	r.emit(snap)
}

func (r *chatTaskRegistry) storeTaskProgress(id, where, next, open string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	task := r.tasks[id]
	if task == nil {
		r.mu.Unlock()
		return
	}
	if strings.TrimSpace(where) != "" {
		task.WhereWeAre = where
	}
	if strings.TrimSpace(next) != "" {
		task.NextStep = next
	}
	if strings.TrimSpace(open) != "" {
		task.OpenProblems = open
	} else if strings.TrimSpace(task.OpenProblems) == "" {
		task.OpenProblems = desktopbot.ProgressOpenNone
	}
	sections := desktopbot.Sections{WhereWeAre: task.WhereWeAre, NextStep: task.NextStep, OpenProblems: task.OpenProblems}
	root := ""
	if strings.TrimSpace(r.storePath) != "" {
		root = filepath.Dir(r.storePath)
	}
	r.mu.Unlock()
	if root == "" {
		return
	}
	path, cleaned, err := writeLocalTaskProgress(root, id, false, sections)
	if err != nil {
		return
	}
	r.mu.Lock()
	if current := r.tasks[id]; current != nil {
		current.WhereWeAre = cleaned.WhereWeAre
		current.NextStep = cleaned.NextStep
		current.OpenProblems = cleaned.OpenProblems
		current.ProgressPath = path
	}
	r.mu.Unlock()
}

func (r *chatTaskRegistry) finish(id string, result chatTaskRunResult) {
	status := chatTaskDone
	errText := ""
	if result.Err != nil {
		if isChatTaskCancelErr(result.Err) {
			status = chatTaskCanceled
		} else {
			status = chatTaskFailed
			errText = result.Err.Error()
		}
	}
	task, loop, snap, ok := r.finishLocked(id, status, result.Text, result.Artifacts, errText)
	if !ok {
		return
	}
	r.publishTerminal(task, loop, snap, status)
}

func (r *chatTaskRegistry) finishLocked(id string, status chatTaskStatus, text string, artifacts []string, errText string) (*chatTaskRecord, *LoopContext, AIAssistantChatTask, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task := r.tasks[strings.TrimSpace(id)]
	if task == nil {
		return nil, nil, AIAssistantChatTask{}, false
	}
	if task.Status.terminal() {
		return task, nil, task.snapshotLocked(), false
	}
	task.Status = status
	task.ResultText = strings.TrimSpace(text)
	task.Error = strings.TrimSpace(errText)
	if len(artifacts) > 0 {
		task.ArtifactPaths = chatTaskUniquePaths(artifacts)
	}
	task.Question = ""
	task.UpdatedAt = time.Now()
	return task, task.loop, task.snapshotLocked(), true
}

func (r *chatTaskRegistry) publishTerminal(task *chatTaskRecord, loop *LoopContext, snap AIAssistantChatTask, status chatTaskStatus) {
	if task != nil && task.Computer {
		desktopbot.ReleaseComputerScreen(task.Screen, computerHelperLeaseID(task.Screen))
	}
	if task != nil {
		task.signalCancel()
	}
	// Successful and failed finishes have already returned from the executor.
	// Canceling that loop marks the completed turn cancelled. Only a user
	// cancel should stop a loop that may still be inside the agent.
	if status == chatTaskCanceled && loop != nil {
		loop.Cancel()
	}
	r.persist()
	r.emit(snap)
	if status == chatTaskDone {
		r.deliverResult(snap)
	}
}

func (task *chatTaskRecord) signalCancel() {
	if task == nil || task.cancelCh == nil {
		return
	}
	task.cancelOnce.Do(func() {
		close(task.cancelCh)
	})
}

func (r *chatTaskRegistry) deliverResult(snap AIAssistantChatTask) {
	if r.handler != nil && r.handler.memory != nil && snap.SessionKey != "" {
		r.handler.memory.Append(snap.SessionKey, agent.ConversationEntry{
			Role:    "assistant",
			Content: formatChatTaskResultNote(snap.Title, snap.ResultText),
		})
	}
	if r.handler == nil || r.handler.app == nil {
		return
	}
	resp := &IMAgentResponse{
		Text:           snap.ResultText,
		SessionKey:     snap.SessionKey,
		RequestID:      "chat-task-result-" + snap.ID,
		ResponseSource: chatTaskResultSource,
		ChatTaskID:     snap.ID,
		ChatTaskKind:   "task_result",
		LocalFilePaths: append([]string(nil), snap.ArtifactPaths...),
	}
	if len(snap.ArtifactPaths) == 1 {
		resp.LocalFilePath = snap.ArtifactPaths[0]
	}
	r.handler.app.emitAIAssistantResponse(resp.RequestID, resp)
}

func (r *chatTaskRegistry) emit(snap AIAssistantChatTask) {
	if r == nil || r.handler == nil || r.handler.app == nil {
		return
	}
	payload, err := json.Marshal(snap)
	if err != nil {
		return
	}
	r.handler.app.emitEvent(chatTaskEventName, string(payload))
}

// persist writes the task list. SQLite is the durable store. The JSON file is
// written only when the database cannot be opened or the transaction fails.
// The bool reports whether the SQLite write committed.
func (r *chatTaskRegistry) persist() bool {
	if r == nil || r.storePath == "" {
		return false
	}
	// Serialize the snapshot and the rename. Otherwise two persists can list
	// in order A, B and write in order B, A, dropping the newer snapshot.
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	if !r.tasksLoaded {
		log.Printf("[chat-task] persist skipped: task list was not loaded")
		return false
	}
	tasks := r.List("")
	stored, genErr := r.chatSnapshotGen()
	if genErr != nil {
		log.Printf("[chat-task] snapshot gen: %v", genErr)
	}
	nextGen := int64(0)
	if genErr == nil {
		// The commit and the fallback file share one counter. A leftover file
		// from before this commit stays at or below the stored generation.
		nextGen = nextChatTaskGeneration(stored, r.chatTaskFileGenerations()...)
	}
	if err := r.persistSQLite(tasks, nextGen); err == nil {
		for _, path := range r.chatTaskSnapshotPaths() {
			_ = os.Remove(path)
		}
		return true
	} else if r.db != nil {
		log.Printf("[chat-task] sqlite persist: %v", err)
	}
	if genErr != nil {
		return false
	}
	blob, err := json.MarshalIndent(chatTaskDisk{Tasks: tasks, Generation: nextGen}, "", "  ")
	if err != nil {
		log.Printf("[chat-task] persist marshal: %v", err)
		return false
	}
	if err := os.MkdirAll(filepath.Dir(r.storePath), 0o755); err != nil {
		log.Printf("[chat-task] persist mkdir: %v", err)
		return false
	}
	// Publish the snapshot by renaming one complete file. A crash after that
	// rename still leaves a document the next open imports, including when the
	// import flag is still set and chat-tasks.json was not replaced yet.
	if err := publishChatTaskFallback(r.storePath, blob); err != nil {
		log.Printf("[chat-task] fallback snapshot: %v", err)
	}
	if err := publishAtomicFile(r.storePath, blob); err != nil {
		log.Printf("[chat-task] persist write: %v", err)
		return false
	}
	if r.db != nil {
		_, _ = r.db.Exec(`DELETE FROM chat_task_meta WHERE key=?`, chatTaskLegacyMetaKey)
	}
	return false
}

// publishChatTaskFallback makes jsonPath+".fallback" the durable snapshot.
// The bytes are the task document. Load also accepts a complete temp file
// and chat-tasks.json, and keeps the highest generation.
func publishChatTaskFallback(jsonPath string, blob []byte) error {
	return publishAtomicFile(chatTaskFallbackMarker(jsonPath), blob)
}

// publishAtomicFile replaces dest with a complete document. The new bytes go
// to dest+".writing" first. A previous complete ".writing" file is moved to
// ".writing.prior" before that write, so the earlier snapshot stays readable
// until the new file is complete. Load reads both names, plus a leftover
// dest+".tmp" from an older process.
func publishAtomicFile(dest string, blob []byte) error {
	staged := dest + ".writing"
	if err := parkValidSnapshot(staged); err != nil {
		return err
	}
	if err := os.WriteFile(staged, blob, 0o644); err != nil {
		return err
	}
	if err := os.Rename(staged, dest); err == nil {
		return nil
	}
	// Windows refuses to rename onto an existing file. staged is already a
	// complete snapshot, so removing dest cannot lose the new bytes.
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(staged, dest)
}

func parkValidSnapshot(path string) error {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	_, pathGen, ok := chatTaskSnapshotBytes(blob)
	if !ok {
		return nil
	}
	park := path + ".prior"
	if parkBlob, readErr := os.ReadFile(park); readErr == nil {
		if _, parkGen, pok := chatTaskSnapshotBytes(parkBlob); pok && parkGen >= pathGen {
			return nil
		}
		if err := os.Remove(park); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return os.Rename(path, park)
}

func nextChatTaskGeneration(stored int64, fileGens ...int64) int64 {
	next := stored + 1
	for _, gen := range fileGens {
		if gen+1 > next {
			next = gen + 1
		}
	}
	if next < 1 {
		return 1
	}
	return next
}

func chatTaskFallbackMarker(jsonPath string) string {
	return jsonPath + ".fallback"
}

func (r *chatTaskRegistry) load() {
	if r == nil || r.storePath == "" {
		return
	}
	if err := r.openChatTaskDB(); err != nil {
		log.Printf("[chat-task] sqlite open: %v", err)
		if snaps, _, ok := r.newestChatTaskSnapshot(); ok {
			r.tasksLoaded = true
			r.installChatTaskSnapshots(snaps, false)
		}
		return
	}
	if r.consumeChatTaskFallback() {
		return
	}
	// A missing or unreadable legacy file is not an empty task list. Loading
	// it as empty would delete the rows already stored in SQLite.
	r.loadSQLiteTasks()
	if !r.chatTaskJSONExists() && r.tasksLoaded {
		if err := r.setChatTaskMeta(chatTaskLegacyMetaKey, "1"); err != nil {
			log.Printf("[chat-task] legacy import mark: %v", err)
		}
	}
}

// consumeChatTaskFallback imports the newest on-disk snapshot when its
// generation is ahead of the committed snapshot_gen. chat-tasks.json,
// the fallback file, and a leftover fallback temp file are one pool.
func (r *chatTaskRegistry) consumeChatTaskFallback() bool {
	snaps, gen, ok := r.newestChatTaskSnapshot()
	if !ok {
		return false
	}
	stored, err := r.chatSnapshotGen()
	if err != nil {
		log.Printf("[chat-task] snapshot gen: %v", err)
		return false
	}
	if !chatTaskFallbackIsNewer(gen, stored, r.chatTaskLegacyImported(), r.chatTasksPresent()) {
		return false
	}
	r.tasksLoaded = true
	r.installChatTaskSnapshots(snaps, true)
	if r.chatTaskLegacyImported() {
		_ = os.Remove(chatTaskFallbackMarker(r.storePath))
		_ = os.Remove(chatTaskFallbackMarker(r.storePath) + ".tmp")
	}
	return true
}

// chatTaskFallbackIsNewer reports whether a snapshot file should replace the
// SQLite rows. A positive generation wins when it is greater than snapshot_gen.
// Generation 0 is a pre-counter document. It is the first import when SQLite
// has no snapshot_gen, the import flag is unset, and the task table is empty.
func chatTaskFallbackIsNewer(fileGen, storedGen int64, legacyImported, rowsPresent bool) bool {
	if fileGen > storedGen {
		return true
	}
	return fileGen == 0 && storedGen == 0 && !legacyImported && !rowsPresent
}

func (r *chatTaskRegistry) newestChatTaskSnapshot() ([]AIAssistantChatTask, int64, bool) {
	var best []AIAssistantChatTask
	var bestGen int64
	found := false
	for _, path := range r.chatTaskSnapshotPaths() {
		blob, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		snaps, gen, ok := chatTaskSnapshotBytes(blob)
		if !ok {
			continue
		}
		if found && gen <= bestGen {
			continue
		}
		best = snaps
		bestGen = gen
		found = true
	}
	return best, bestGen, found
}

func (r *chatTaskRegistry) chatTaskSnapshotPaths() []string {
	var paths []string
	for _, base := range []string{chatTaskFallbackMarker(r.storePath), r.storePath} {
		paths = append(paths, base, base+".tmp", base+".writing", base+".writing.prior")
	}
	return paths
}

func (r *chatTaskRegistry) chatTaskFileGenerations() []int64 {
	var gens []int64
	for _, path := range r.chatTaskSnapshotPaths() {
		blob, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		_, gen, ok := chatTaskSnapshotBytes(blob)
		if !ok {
			continue
		}
		gens = append(gens, gen)
	}
	return gens
}

func (r *chatTaskRegistry) chatTaskJSONExists() bool {
	if r == nil || r.storePath == "" {
		return false
	}
	_, err := os.Stat(r.storePath)
	return err == nil
}

func chatTaskSnapshotBytes(blob []byte) ([]AIAssistantChatTask, int64, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(blob, &raw); err != nil {
		return nil, 0, false
	}
	tasksRaw, ok := raw["tasks"]
	if !ok || string(tasksRaw) == "null" {
		return nil, 0, false
	}
	var tasks []AIAssistantChatTask
	if err := json.Unmarshal(tasksRaw, &tasks); err != nil {
		return nil, 0, false
	}
	if tasks == nil {
		tasks = []AIAssistantChatTask{}
	}
	var gen int64
	if g, ok := raw["generation"]; ok && string(g) != "null" {
		if err := json.Unmarshal(g, &gen); err != nil {
			return nil, 0, false
		}
	}
	return tasks, gen, true
}

func (r *chatTaskRegistry) loadSQLiteTasks() {
	snaps, err := r.selectChatTasks()
	if err != nil {
		log.Printf("[chat-task] sqlite load: %v", err)
		return
	}
	r.tasksLoaded = true
	r.installChatTaskSnapshots(snaps, false)
}

func (r *chatTaskRegistry) installChatTaskSnapshots(snaps []AIAssistantChatTask, markImported bool) {
	changed := false
	r.mu.Lock()
	for _, snap := range snaps {
		rec := recordFromSnapshot(snap)
		if !rec.Status.terminal() {
			rec.Status = chatTaskInterrupted
			rec.Recovered = true
			if next := resumeTaskNextStep(rec); next != "" {
				rec.NextStep = next
			}
			changed = true
		}
		// Interrupted is already terminal, so a task saved by an older restart
		// never enters the block above. Progress is still a step: a status
		// sentence or a silence line hides the card's own explanation.
		if rec.Status == chatTaskInterrupted {
			if cleaned := interruptedChatTaskProgress(rec.Progress); cleaned != rec.Progress {
				rec.Progress = cleaned
				changed = true
			}
		}
		rec.answerCh = make(chan string, 1)
		rec.cancelCh = make(chan struct{})
		r.tasks[rec.ID] = rec
	}
	r.mu.Unlock()
	if !changed && !markImported {
		return
	}
	if r.persist() && markImported {
		if err := r.setChatTaskMeta(chatTaskLegacyMetaKey, "1"); err != nil {
			log.Printf("[chat-task] legacy import mark: %v", err)
		}
	}
}

func (r *chatTaskRegistry) activeCount(sessionKey string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.activeCountLocked(sessionKey)
}

func (r *chatTaskRegistry) activeCountLocked(sessionKey string) int {
	n := 0
	for _, task := range r.tasks {
		if sameChatSession(task.SessionKey, sessionKey) && !task.Status.terminal() {
			n++
		}
	}
	return n
}

func (r *chatTaskRegistry) isTerminal(id string) bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	task := r.tasks[id]
	return task == nil || task.Status.terminal()
}

func (r *chatTaskRegistry) codingWorkbench(sessionKey string) bool {
	if r == nil || r.handler == nil {
		return false
	}
	return r.handler.sessionIsCodingWorkbench(sessionKey)
}

func (h *IMMessageHandler) sessionIsCodingWorkbench(sessionKey string) bool {
	if h == nil {
		return false
	}
	if h.isPureCodingWorkbenchSession(sessionKey) {
		return true
	}
	if h.app != nil {
		return h.app.isDesktopCodingWorkbenchSession(sessionKey, projectPathFromSessionOwnerID(sessionKey))
	}
	return false
}

func (h *IMMessageHandler) chatTurnMayDispatch(msg IMUserMessage, workflowAgentLoop, codingWorkbench bool) bool {
	if h == nil || workflowAgentLoop || codingWorkbench || msg.IsBackground || isChatTaskMemoryOwner(msg.UserID) {
		return false
	}
	platform := strings.TrimSpace(msg.Platform)
	if platform != "" && platform != desktopPlatform {
		return false
	}
	sessionKey := strings.TrimSpace(msg.UserID)
	if sessionKey == "" {
		sessionKey = desktopUserID
	}
	return chatTaskSessionAllowed(sessionKey, false)
}

func (h *IMMessageHandler) chatTaskDispatchGuidance(sessionKey string) string {
	var b strings.Builder
	b.WriteString("\n\n[Chat]\n")
	b.WriteString("You are the conversation. Match the user's language. Be concise and warm. Lead with the answer. Never show task ids, tool names, or internal lane names.\n")
	b.WriteString("The first thing the user sees in this turn is a short reply, before any tool call: the answer when it is quick, otherwise one sentence that names the first step.\n")
	b.WriteString("Do the work in this turn. Keep calling tools until it is verified or you are actually blocked. A count of tool calls is not a reason to stop, and it is not a reason to ask the user for another message. Desktop control does not run here; a helper starts on its own when the request needs it. Keep doing the rest of the task here.\n")
	b.WriteString("A follow-up or correction to work already running goes to steer_background_task for that same work. Do not start a second one for it. If more than one is running and it is unclear which, ask which one.\n")
	b.WriteString("If the user wants running work stopped, cancelled, or held, in any language or phrasing, call stop_all_work before you answer. Do not start new work in that turn. You may say the work stopped only by repeating the tool's say field, and only when idle is true. If idle is false, repeat say and do not claim it has stopped. Never say the work stopped without calling the tool. A change to the content (\"stop using bold\", \"stop the music in the video\") is not a request to stop the work. Never call stop_all_work because you are unsure how to continue, because a capability is missing, or merely to end your turn; it answers the person's own stop or hold request only. If you are unsure whether they meant the running work, call it with intent pause instead of continuing silently.\n")
	b.WriteString("Ask only when a wrong guess would delete, send, purchase, or submit something, or when ambiguity would materially change the result. Otherwise decide and proceed.\n")
	b.WriteString("Never say work is done unless it was verified. If it is still running, blocked, or failed, say that. Do not invent results.\n")
	b.WriteString("When a finished result appears in this chat, deliver it in plain words. Do not add work you did not see.\n")
	if reg := h.ensureChatTasks(); reg != nil {
		var open []AIAssistantChatTask
		for _, task := range reg.List(sessionKey) {
			if chatTaskStatusOpen(task.Status) {
				open = append(open, task)
			}
		}
		if len(open) > 0 {
			b.WriteString("Work already running (titles only; never repeat these labels as ids):\n")
			for _, task := range open {
				fmt.Fprintf(&b, "- %s (%s)", task.Title, task.Status)
				if task.Progress != "" && task.Status != string(chatTaskNeedsInput) {
					fmt.Fprintf(&b, " — %s", truncateChatTaskText(task.Progress, 160))
				}
				if task.Question != "" {
					fmt.Fprintf(&b, " — waiting: %s", truncateChatTaskText(task.Question, 160))
				}
				b.WriteByte('\n')
			}
			b.WriteString("steer_background_task updates one of these. Omit task_id when only one is running; pass its title when several are.\n")
		}
	}
	return b.String()
}

func chatTaskWorkerGuidance() string {
	return "\n\n[Background work]\n" +
		"Do the work. Your final message is what the person reads. Use their language, plain words, and lead with the result.\n" +
		"Progress is for a real change only: a file is ready, you are blocked, or a step failed. Do not narrate each command.\n" +
		"Call chat_task_request_input only when a wrong guess would delete, send, purchase, or submit something, or when ambiguity would materially change the result. Otherwise decide and proceed.\n" +
		"Never say the work is done unless you verified it. If you are blocked or it failed, say that actual state. Do not invent results.\n" +
		"Do not mention task ids, tool names, or internal lanes. Do not start another task.\n"
}

func (h *IMMessageHandler) tryChatTaskStatusReply(msg IMUserMessage) (*IMAgentResponse, bool) {
	if h == nil || msg.IsBackground || isChatTaskMemoryOwner(msg.UserID) {
		return nil, false
	}
	if strings.TrimSpace(msg.Platform) != "" && msg.Platform != desktopPlatform {
		return nil, false
	}
	sessionKey := strings.TrimSpace(msg.UserID)
	if sessionKey == "" {
		sessionKey = desktopUserID
	}
	if !chatTaskSessionAllowed(sessionKey, h.sessionIsCodingWorkbench(sessionKey)) {
		return nil, false
	}
	if !looksLikeChatTaskStatusQuestion(msg.Text) {
		return nil, false
	}
	reg := h.ensureChatTasks()
	if reg == nil {
		return nil, false
	}
	tasks := reg.List(sessionKey)
	if len(tasks) == 0 {
		return nil, false
	}
	return &IMAgentResponse{
		Text:           formatChatTaskStatus(tasks, msg.Lang, desktopChatChinese(msg.Lang, msg.Text)),
		SessionKey:     sessionKey,
		ResponseSource: "chat_task_status",
	}, true
}

func chatTaskControlName(name string) bool {
	switch strings.TrimSpace(name) {
	case chatTaskToolDispatch, chatTaskToolSteer, chatTaskToolStop, chatTaskToolRequestInput:
		return true
	default:
		return false
	}
}

// legacyCatalogOwnsTool reports whether the reviewed legacy snapshot may
// accept or reject this name. Host conversation controls are not adapter
// provisions. The handler decides, after the same execution pipeline as any
// other host tool: cancel, argument size, ACP, and progress.
func legacyCatalogOwnsTool(name string) bool {
	return !chatTaskControlName(name)
}

func (h *IMMessageHandler) executeChatTaskToolIfAny(policyUserID, name string, args map[string]interface{}) (toolExecutionResult, bool) {
	switch strings.TrimSpace(name) {
	case "dispatch_background_task":
		return h.dispatchBackgroundTaskTool(policyUserID, args), true
	case "steer_background_task":
		return h.steerBackgroundTaskTool(policyUserID, args), true
	case "chat_task_request_input":
		return h.chatTaskRequestInputTool(policyUserID, args), true
	case "stop_all_work":
		return h.stopAllWorkTool(policyUserID, args), true
	default:
		return toolExecutionResult{}, false
	}
}

func (h *IMMessageHandler) dispatchBackgroundTaskTool(policyUserID string, args map[string]interface{}) toolExecutionResult {
	loop := h.getSessionLoopCtx(policyUserID)
	if loop != nil && loop.ChatTaskID != "" {
		return chatTaskToolRejected("dispatch_background_task", "a background chat task cannot start another one")
	}
	if loop == nil || !loop.OfferChatTaskDispatch {
		return chatTaskToolRejected("dispatch_background_task", "this turn cannot hand work to a background chat task")
	}
	if loop.chatTaskHandoffSent() {
		return chatTaskToolRejected("dispatch_background_task", "this turn already handed the work off")
	}
	request := chatTaskArg(args, "request", "task", "text", "prompt")
	if request == "" {
		return chatTaskToolRejected("dispatch_background_task", "request is required")
	}
	sessionKey := strings.TrimSpace(policyUserID)
	if sessionKey == "" && loop != nil {
		sessionKey = loop.UserID
	}
	reg := h.ensureChatTasks()
	if reg == nil {
		return chatTaskToolRejected("dispatch_background_task", "chat tasks are unavailable")
	}
	snap, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{Text: request, SessionKey: sessionKey})
	if err != nil {
		return chatTaskToolRejected("dispatch_background_task", err.Error())
	}
	// The handoff flag closes the rest of this turn, including later calls
	// in the same response. The semantic loop snapshots its ceiling at
	// start, and a desktop chat round restores ForegroundChatBudget, so
	// lowering MaxIterations here would not stop anything.
	loop.markChatTaskHandoffSent()
	return toolExecutionResult{
		Text:     "Started: " + snap.Title + ". Tell the user in one plain sentence, then stop. Do not continue the work in this turn. Do not mention ids, tool names, or lanes.",
		ToolName: "dispatch_background_task",
		Outcome:  toolOutcomeSucceeded,
	}
}

// continueSpentForegroundChat continues work a desktop chat turn refused to
// keep inline, when the model stopped without calling dispatch or steer.
// One open task gets the continuation. Several open tasks, or one that is
// waiting for the user, are left alone. Nothing running starts a task.
// The worker does not see this chat, so the prompt carries the workspace,
// the user text, and the endings of this turn's recent tool results.
// A dead-loop stop is left alone.
func (h *IMMessageHandler) continueSpentForegroundChat(loop *LoopContext, sessionKey, userText string, result agent.LoopResult) (string, bool) {
	if h == nil || loop == nil || result.HardExit {
		return "", false
	}
	// The flag is the explicit close. A turn that ran out of iterations, or
	// that ended by asking the user to send another message, is the same
	// unfinished work. A dead-loop stop is neither.
	if !loop.foregroundChatNeedsHostHandoff() && !loop.foregroundChatEndedUnfinished(result) {
		return "", false
	}
	// The same-turn nudge already refuses another call while this result is
	// unobserved. Starting a worker here would issue that command again.
	if agent.LatestToolResultUnsettled(result.HistoryDelta) {
		return "", false
	}
	if errText := strings.TrimSpace(result.Error); errText != "" && errText != "max iterations reached" {
		return "", false
	}
	reg := h.ensureChatTasks()
	if reg == nil {
		return "", false
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" && loop != nil {
		sessionKey = strings.TrimSpace(loop.UserID)
	}
	earlier := h.chatTaskEarlierUserRequest(sessionKey, userText)
	prompt := foregroundChatContinuationPrompt(h.foregroundChatContinuationWorkspace(sessionKey), userText, earlier, result.HistoryDelta)
	// A bare 「继续」 must not replace the running card's name with the folder.
	// A new sentence does: that is the work the card should show.
	steerTitle := ""
	if named := continuationTitleText(userText); named != "" && !continuationCueOnly(named) {
		steerTitle = chatTaskTitle(named)
	}
	switch kind, err := reg.continueOpenChatTask(sessionKey, prompt, steerTitle); {
	case err != nil:
		log.Printf("[chat-task] inline handoff failed session=%q err=%v", sessionKey, err)
		return "", false
	case kind == chatTaskOpenContinueSteered:
		loop.markChatTaskHandoffSent()
		return foregroundChatContinuationNote(userText), true
	case kind == chatTaskOpenContinueHeld:
		log.Printf("[chat-task] inline handoff left the running work session=%q", sessionKey)
		return "", false
	}
	if _, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{
		Text:       prompt,
		SessionKey: sessionKey,
		Title:      foregroundChatContinuationTitle(sessionKey, userText, earlier),
	}); err != nil {
		log.Printf("[chat-task] inline handoff failed session=%q err=%v", sessionKey, err)
		return "", false
	}
	loop.markChatTaskHandoffSent()
	return foregroundChatContinuationNote(userText), true
}

// foregroundChatContinuationWorkspace is the directory the background worker's
// tools will use. A managed task keeps task.md at the session project path
// and runs tools in workspace/ (or its working_dir tag). The prompt has to
// name that execution directory.
func (h *IMMessageHandler) foregroundChatContinuationWorkspace(sessionKey string) string {
	if h != nil {
		if dir := strings.TrimSpace(h.projectTabWorkDirForOwner(sessionKey)); dir != "" {
			return dir
		}
	}
	return projectPathFromSessionOwnerID(sessionKey)
}

func foregroundChatContinuationPrompt(workspace, userText, earlier string, delta []agent.ConversationEntry) string {
	var b strings.Builder
	b.WriteString("Continue the user's task in this workspace. Finish the remaining work here. Do not ask the user to send another message.\n")
	if workspace = strings.TrimSpace(workspace); workspace != "" {
		b.WriteString("\nWorkspace:\n")
		b.WriteString(workspace)
		b.WriteString("\n")
	}
	request := chatTaskUserRequestText(userText)
	if earlier = strings.TrimSpace(earlier); earlier != "" && earlier != request {
		b.WriteString("\nEarlier request:\n")
		b.WriteString(truncateChatTaskText(earlier, 1200))
		b.WriteString("\n")
	}
	b.WriteString("\nUser request:\n")
	b.WriteString(truncateChatTaskText(request, 1200))
	b.WriteString("\n\nTool results from this turn:\n")
	var kept []agent.ConversationEntry
	for _, entry := range delta {
		if entry.Role != "tool" {
			continue
		}
		body := strings.TrimSpace(chatTaskContentText(entry.Content))
		if body == "" || foregroundLookupDenial(body) {
			continue
		}
		kept = append(kept, entry)
	}
	if len(kept) > 6 {
		kept = kept[len(kept)-6:]
	}
	if len(kept) == 0 {
		b.WriteString("(none)\n")
	}
	for _, entry := range kept {
		name := strings.TrimSpace(entry.ToolName)
		if name == "" {
			name = "tool"
		}
		fmt.Fprintf(&b, "- %s (%s): %s\n", name, strings.TrimSpace(entry.ToolOutcome), chatTaskResultExcerpt(chatTaskContentText(entry.Content), 480))
	}
	b.WriteString("\nDo not repeat a send, delete, deliver, or schedule action that a tool result already shows succeeded.\n")
	return placeContinuationFileGrant(b.String(), earlier)
}

// placeContinuationFileGrant puts the live picker marker last. Expansion keeps
// the briefing before that marker and treats the text after it as leftover
// picker instructions. A marker left in the middle would drop tool results
// that mention read_document. A follow-up that attaches no file still grants
// the earlier file, so the worker reads it again. A new file stays the grant.
func placeContinuationFileGrant(prompt, earlier string) string {
	paths := agent.SelectedLocalFilePathsFromPrompt(prompt)
	if len(paths) == 0 {
		paths = selectedPathsUnderHistoricalMarker(earlier)
	}
	idx := agent.CurrentLocalFilePathPromptIndex(prompt)
	if idx < 0 && len(paths) == 0 {
		return prompt
	}
	if idx >= 0 {
		prompt = prompt[:idx] + prompt[idx+len(agent.FilePathPromptPrefix):]
	}
	if len(paths) == 0 {
		return prompt
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(prompt, "\n"))
	b.WriteString("\n\n")
	b.WriteString(agent.FilePathPromptPrefix)
	b.WriteByte('\n')
	for _, p := range paths {
		b.WriteString(p)
		b.WriteByte('\n')
	}
	return b.String()
}

// selectedPathsUnderHistoricalMarker reads the reference-only path list. The
// shared picker parser ignores that marker, and this continuation is the host
// granting the same file again when the user did not attach a new one.
func selectedPathsUnderHistoricalMarker(text string) []string {
	if !strings.Contains(text, agent.FilePathPromptPrefixHistorical) {
		return nil
	}
	swapped := strings.Replace(text, agent.FilePathPromptPrefixHistorical, agent.FilePathPromptPrefix, 1)
	return agent.SelectedLocalFilePathsFromPrompt(swapped)
}

// chatTaskResultExcerpt keeps the start and the end of a tool result.
// Command output puts the outcome on the last lines, so a head-only cut
// drops the fact the worker needs.
func chatTaskResultExcerpt(text string, maxRunes int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if maxRunes <= 0 || utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	const joiner = " … "
	joinLen := utf8.RuneCountInString(joiner)
	budget := maxRunes - joinLen
	if budget < 8 {
		return truncateChatTaskText(text, maxRunes)
	}
	head := budget / 4
	if head < 40 && budget >= 80 {
		head = 40
	}
	if head < 1 {
		head = 1
	}
	if head >= budget {
		head = budget / 2
		if head < 1 {
			head = 1
		}
	}
	tail := budget - head
	runes := []rune(text)
	if head+tail >= len(runes) {
		return text
	}
	return string(runes[:head]) + joiner + string(runes[len(runes)-tail:])
}

// chatTaskContentText keeps the text of a stored message. Desktop chat saves
// an image-bearing turn as content blocks, and the image payload stays out of
// the worker prompt.
func chatTaskContentText(content interface{}) string {
	return conversationContentText(content)
}

// chatTaskUserRequestText is the user wording stored on a turn. Host lines
// that point at image bytes on the original message do not apply to this
// text-only prompt. A message that is only those lines is not a request.
func chatTaskUserRequestText(content interface{}) string {
	text := strings.TrimSpace(chatTaskContentText(content))
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, visionFirstInstruction, "")
	return agent.StripAttachedImageInstructions(text)
}

// chatTaskPromptToolNames reads the tool names this host wrote into a
// continuation prompt. Those calls already ran; the worker keeps them.
func chatTaskPromptToolNames(prompt string) []string {
	var names []string
	seen := map[string]bool{}
	for _, line := range strings.Split(prompt, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "- "))
		open := strings.IndexByte(rest, '(')
		if open <= 0 {
			continue
		}
		name := strings.TrimSpace(rest[:open])
		if name == "" || name == "tool" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// chatTaskEarlierUserRequest is the latest real user message before a short
// follow-up. The worker does not see this chat, and a line such as a bare
// continuation is not the task.
func (h *IMMessageHandler) chatTaskEarlierUserRequest(sessionKey, current string) string {
	if h == nil || h.memory == nil || !continuationCarriesEarlierRequest(current) {
		return ""
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	return chatTaskEarlierUserRequest(h.memory.Load(sessionKey), current)
}

// continuationCarriesEarlierRequest reports that this message does not state
// a new task. Only the user's own words count. The picker file block and the
// image note are not part of that length.
func continuationCarriesEarlierRequest(current string) bool {
	return shortContinuationText(continuationWording(current))
}

func continuationWording(current string) string {
	text := chatTaskUserRequestText(current)
	if cut := continuationFileBlockIndex(text); cut >= 0 {
		text = text[:cut]
	}
	return strings.TrimSpace(text)
}

// continuationFileBlockIndex is the start of a file-picker block. The live
// marker and the historical marker are the same block: an earlier request is
// stored with the historical form, and that form is not a task name.
func continuationFileBlockIndex(text string) int {
	cut := -1
	for _, marker := range []string{agent.FilePathPromptPrefix, agent.FilePathPromptPrefixHistorical} {
		if i := strings.Index(text, marker); i >= 0 && (cut < 0 || i < cut) {
			cut = i
		}
	}
	return cut
}

func chatTaskEarlierUserRequest(entries []agent.ConversationEntry, current string) string {
	currentRequest := chatTaskUserRequestText(current)
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.Role != "user" {
			continue
		}
		text := chatTaskUserRequestText(entry.Content)
		if text == "" || text == currentRequest {
			continue
		}
		if strings.HasPrefix(text, "[系统]") || strings.HasPrefix(text, "[用户补充]") {
			continue
		}
		// This line is history for the worker. A current picker marker here is
		// the first selected upload, ahead of a file on this message, and the
		// extract body is not attached to the worker.
		return agent.AnnotateHistoryAttachmentText(text)
	}
	return ""
}

func chatTaskWorkerLoop(ctx *LoopContext) bool {
	return ctx != nil && strings.TrimSpace(ctx.ChatTaskID) != ""
}

func foregroundLookupDenial(body string) bool {
	return strings.Contains(body, chatTaskHandoffRequiredMessage) ||
		strings.Contains(body, chatTaskAlreadyHandedOffMessage) ||
		strings.Contains(body, chatTaskComputerUseHandoffMessage)
}

func (h *IMMessageHandler) steerBackgroundTaskTool(policyUserID string, args map[string]interface{}) toolExecutionResult {
	loop := h.getSessionLoopCtx(policyUserID)
	if loop != nil && loop.ChatTaskID != "" {
		return chatTaskToolRejected("steer_background_task", "a background chat task cannot steer another one")
	}
	if loop == nil || !loop.OfferChatTaskDispatch {
		return chatTaskToolRejected("steer_background_task", "this turn cannot update a background chat task")
	}
	note := chatTaskArg(args, "note", "request", "text", "prompt")
	if note == "" {
		return chatTaskToolRejected("steer_background_task", "note is required")
	}
	sessionKey := strings.TrimSpace(policyUserID)
	if sessionKey == "" {
		sessionKey = loop.UserID
	}
	reg := h.ensureChatTasks()
	if reg == nil {
		return chatTaskToolRejected("steer_background_task", "chat tasks are unavailable")
	}
	task, err := reg.openTaskForSteer(sessionKey, chatTaskArg(args, "task_id", "id"), chatTaskArg(args, "title"))
	if err != nil {
		return chatTaskToolRejected("steer_background_task", err.Error())
	}
	if err := reg.Steer(task.ID, note); err != nil {
		return chatTaskToolRejected("steer_background_task", err.Error())
	}
	// Same stop as dispatch: the flag, not the iteration ceiling.
	loop.markChatTaskHandoffSent()
	return toolExecutionResult{
		Text:     "Update sent to \"" + task.Title + "\". Tell the user in one plain sentence, then stop. Do not start another task and do not mention ids, tool names, or lanes.",
		ToolName: "steer_background_task",
		Outcome:  toolOutcomeSucceeded,
	}
}

func (h *IMMessageHandler) chatTaskRequestInputTool(policyUserID string, args map[string]interface{}) toolExecutionResult {
	loop := h.getSessionLoopCtx(policyUserID)
	if loop == nil || loop.ChatTaskID == "" {
		return chatTaskToolRejected("chat_task_request_input", "only a background chat task can ask the user")
	}
	question := chatTaskArg(args, "question", "prompt", "text")
	if question == "" {
		return chatTaskToolRejected("chat_task_request_input", "question is required")
	}
	reg := h.ensureChatTasks()
	if reg == nil {
		return chatTaskToolRejected("chat_task_request_input", "chat tasks are unavailable")
	}
	answer, err := reg.requestInput(loop.ChatTaskID, question)
	if err != nil {
		return chatTaskToolRejected("chat_task_request_input", err.Error())
	}
	return toolExecutionResult{
		Text:     "User answered: " + answer,
		ToolName: "chat_task_request_input",
		Outcome:  toolOutcomeSucceeded,
	}
}

func (a *App) DispatchAIAssistantChatTask(req AIAssistantChatTaskDispatchRequest) (*AIAssistantChatTask, error) {
	handler, err := a.chatTaskHandler()
	if err != nil {
		return nil, err
	}
	snap, err := handler.ensureChatTasks().Dispatch(req)
	if err != nil {
		return nil, err
	}
	return &snap, nil
}

func (a *App) ListAIAssistantChatTasks(sessionKey string) ([]AIAssistantChatTask, error) {
	handler, err := a.chatTaskHandler()
	if err != nil {
		return nil, err
	}
	return handler.ensureChatTasks().List(sessionKey), nil
}

func (a *App) ResumeAIAssistantChatTask(taskID, text string) (*AIAssistantChatTask, error) {
	handler, err := a.chatTaskHandler()
	if err != nil {
		return nil, err
	}
	snap, err := handler.ensureChatTasks().Resume(taskID, text)
	if err != nil {
		return nil, err
	}
	return &snap, nil
}

func (a *App) CancelAIAssistantChatTask(taskID string) error {
	handler, err := a.chatTaskHandler()
	if err != nil {
		return err
	}
	return handler.ensureChatTasks().Cancel(taskID)
}

func (a *App) SteerAIAssistantChatTask(taskID, text string) error {
	handler, err := a.chatTaskHandler()
	if err != nil {
		return err
	}
	return handler.ensureChatTasks().Steer(taskID, text)
}

func (a *App) AnswerAIAssistantChatTask(taskID, text string) error {
	handler, err := a.chatTaskHandler()
	if err != nil {
		return err
	}
	return handler.ensureChatTasks().Answer(taskID, text)
}

func (a *App) chatTaskHandler() (*IMMessageHandler, error) {
	if a == nil {
		return nil, fmt.Errorf("app is nil")
	}
	a.ensureInteractionInfra()
	hub := a.ensureHubClient()
	if hub == nil {
		return nil, fmt.Errorf("AI assistant not initialized")
	}
	handler := hub.ensureIMHandler()
	if handler == nil {
		return nil, fmt.Errorf("AI assistant handler not initialized")
	}
	return handler, nil
}

func (task *chatTaskRecord) snapshotLocked() AIAssistantChatTask {
	return AIAssistantChatTask{
		ID:            task.ID,
		SessionKey:    task.SessionKey,
		Title:         task.Title,
		Prompt:        task.Prompt,
		Status:        string(task.Status),
		Progress:      task.Progress,
		Question:      task.Question,
		ResultText:    task.ResultText,
		Error:         task.Error,
		ArtifactPaths: append([]string(nil), task.ArtifactPaths...),
		WhereWeAre:    task.WhereWeAre,
		NextStep:      task.NextStep,
		OpenProblems:  task.OpenProblems,
		ProgressPath:  task.ProgressPath,
		CreatedAtUnix: task.CreatedAt.Unix(),
		UpdatedAtUnix: task.UpdatedAt.Unix(),
		Recovered:     task.Recovered,
	}
}

func recordFromSnapshot(snap AIAssistantChatTask) *chatTaskRecord {
	created := time.Unix(snap.CreatedAtUnix, 0)
	updated := time.Unix(snap.UpdatedAtUnix, 0)
	if created.IsZero() {
		created = time.Now()
	}
	if updated.IsZero() {
		updated = created
	}
	return &chatTaskRecord{
		ID:            snap.ID,
		SessionKey:    snap.SessionKey,
		MemoryKey:     chatTaskMemoryOwner(snap.SessionKey, snap.ID),
		Title:         snap.Title,
		Prompt:        snap.Prompt,
		Status:        chatTaskStatus(snap.Status),
		Progress:      snap.Progress,
		Question:      snap.Question,
		ResultText:    snap.ResultText,
		Error:         snap.Error,
		ArtifactPaths: append([]string(nil), snap.ArtifactPaths...),
		WhereWeAre:    snap.WhereWeAre,
		NextStep:      snap.NextStep,
		OpenProblems:  snap.OpenProblems,
		ProgressPath:  snap.ProgressPath,
		CreatedAt:     created,
		UpdatedAt:     updated,
		Recovered:     snap.Recovered,
	}
}

func resolveChatTaskSessionKey(sessionKey, projectPath string) string {
	if key := strings.TrimSpace(sessionKey); key != "" {
		return key
	}
	return projectSessionOwnerID(projectPath)
}

// sameChatSession reports one chat. A tab path and a stored owner differ by
// slash, drive letter, and a trailing separator. Expert and ACP keys are not
// file paths and stay as written.
func sameChatSession(a, b string) bool {
	return canonicalChatSessionKey(a) == canonicalChatSessionKey(b)
}

func canonicalChatSessionKey(key string) string {
	key = strings.TrimSpace(key)
	const prefix = "desktop-user:"
	if !strings.HasPrefix(key, prefix) {
		return key
	}
	rest := strings.TrimPrefix(key, prefix)
	if rest == "" || strings.HasPrefix(rest, "expert:") || strings.HasPrefix(rest, "acp:") || strings.HasPrefix(rest, "task:") {
		if rest == "" {
			return "desktop-user"
		}
		return prefix + rest
	}
	cleaned := normalizeProjectSessionPath(rest)
	if cleaned == "" {
		return "desktop-user"
	}
	return prefix + cleaned
}

func resumeTaskNextStep(task *chatTaskRecord) string {
	if task == nil {
		return ""
	}
	fileText := ""
	if path := strings.TrimSpace(task.ProgressPath); path != "" {
		if body, err := os.ReadFile(path); err == nil {
			fileText = string(body)
		}
	}
	return desktopbot.ResumeFromNextStep(fileText, desktopbot.Sections{
		WhereWeAre:   task.WhereWeAre,
		NextStep:     task.NextStep,
		OpenProblems: task.OpenProblems,
	})
}

func firstProgressLine(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = text[:i]
	}
	return truncateChatTaskText(text, 200)
}

func chatTaskDispatchTitle(title, prompt string) string {
	if strings.TrimSpace(title) != "" {
		return chatTaskTitle(title)
	}
	return chatTaskTitle(prompt)
}

// foregroundChatContinuationTitle names the work this handoff will do.
// The window already shows the managed-task folder. A new request must not
// reuse that folder. A continuation cue uses the earlier request, then the
// folder. A file with no words is this handoff, so its name wins over the
// folder. On a chat with no folder, a cue that only brings a file uses that
// file name; the cue line remains when there is no file. The title is never
// empty: Dispatch would otherwise put the worker prompt on the card.
func foregroundChatContinuationTitle(sessionKey, userText, earlier string) string {
	if request := continuationTitleText(userText); request != "" && !continuationCueOnly(request) {
		return chatTaskTitle(request)
	}
	if prior := continuationTitleText(earlier); prior != "" && !continuationCueOnly(prior) {
		return chatTaskTitle(prior)
	}
	folder := managedTaskTitleFromSessionPath(projectPathFromSessionOwnerID(sessionKey))
	// A cue keeps the managed task the window already shows. With no folder,
	// the file is the work and the cue line is not.
	if request := continuationTitleText(userText); request != "" {
		if folder != "" {
			return folder
		}
		if base := continuationAttachedTitle(userText); base != "" {
			return chatTaskTitle(base)
		}
		if base := continuationAttachedTitle(earlier); base != "" {
			return chatTaskTitle(base)
		}
		return chatTaskTitle(request)
	}
	if base := continuationAttachedTitle(userText); base != "" {
		return chatTaskTitle(base)
	}
	if folder != "" {
		return folder
	}
	if base := continuationAttachedTitle(earlier); base != "" {
		return chatTaskTitle(base)
	}
	// The worker prompt is host instructions. Dispatch treats an empty title
	// as "use the prompt", which would put that text on the card.
	return continuationBareTitle(userText + earlier)
}

// continuationAttachedTitle is the file this handoff is about when the user
// did not write a line. The picker block itself is not a title. An earlier
// message stores the reference-only marker; that file is still the work.
func continuationAttachedTitle(text string) string {
	paths := agent.SelectedLocalFilePathsFromPrompt(text)
	if len(paths) == 0 {
		paths = selectedPathsUnderHistoricalMarker(text)
	}
	if len(paths) == 0 {
		return ""
	}
	base := filepath.Base(paths[0])
	if base == "" || base == "." || base == string(filepath.Separator) {
		return ""
	}
	return base
}

func continuationBareTitle(text string) string {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return "这项工作"
		}
	}
	return "This work"
}

// continuationTitleText is the user's own line that names this handoff.
// The file-picker block is not part of it. A leading continuation cue does
// not hide a later line that names the work. When every line is a cue, the
// first cue is returned so the caller can fall through to the earlier request.
func continuationTitleText(text string) string {
	wording := continuationWording(text)
	if wording == "" {
		return ""
	}
	var first string
	for _, line := range strings.Split(wording, "\n") {
		line = firstProgressLine(line)
		if line == "" {
			continue
		}
		if first == "" {
			first = line
		}
		if !continuationCueOnly(line) {
			return line
		}
	}
	return first
}

// continuationCueOnly is a line that asks to keep going and does not say what
// the next piece of work is. Courtesy and the continuation word peel off.
// "请继续" is a cue. "继续做配图版" still names the work.
func continuationCueOnly(text string) bool {
	text = strings.TrimSpace(text)
	// Longer words first, so "继续做配图版" peels "继续做" and keeps "配图版".
	prefixes := []string{
		"请你", "請你", "请", "請", "麻烦你", "麻煩你", "麻烦", "麻煩", "帮我", "幫我",
		"please", "pls",
		"继续一下", "繼續一下", "继续做", "繼續做", "再继续", "再繼續", "然后继续", "然後繼續", "继续", "繼續",
		"接着做", "接著做", "接着", "接著", "然后", "然後",
		"keep going", "carry on", "go on", "continue",
	}
	for {
		folded := strings.ToLower(strings.TrimSpace(text))
		// Separators do not name the work. "请继续，" is still only a cue.
		folded = strings.Trim(folded, " \t。.!！?？~…吧啊呀呢了，,、；;：:")
		if folded == "" {
			return true
		}
		peeled := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(folded, prefix) {
				text = strings.TrimSpace(folded[len(prefix):])
				peeled = true
				break
			}
		}
		if !peeled {
			return false
		}
	}
}

// applyChatTaskWorkerExecutionProfile keeps a background chat task on the
// full agent. The worker prompt is host text plus tool results, and a lookup
// label would give it the same short budget the foreground turn just left.
func applyChatTaskWorkerExecutionProfile(ctx *LoopContext) {
	if ctx == nil || strings.TrimSpace(ctx.ChatTaskID) == "" {
		return
	}
	ctx.Runtime.Execution = fullExecutionProfile("background chat task")
	ctx.semanticTurnAnswerOnly = false
}

func chatTaskTitle(prompt string) string {
	title := truncateChatTaskText(strings.TrimSpace(prompt), 72)
	if title == "" {
		return "This work"
	}
	return title
}

func truncateChatTaskText(text string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	runes := []rune(text)
	if maxRunes > len(runes) {
		return text
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + "…"
}

func formatChatTaskResultNote(title, result string) string {
	result = truncateChatTaskText(strings.TrimSpace(result), 800)
	if result != "" {
		return result
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	return title
}

func formatChatTaskStatus(tasks []AIAssistantChatTask, lang string, chinese bool) string {
	var b strings.Builder
	for _, task := range tasks {
		title := strings.TrimSpace(task.Title)
		if title == "" {
			title = "这件事"
			if !chinese {
				title = desktopChatCopy(lang, false, "that work", "这件事")
			}
		}
		switch chatTaskStatus(task.Status) {
		case chatTaskNeedsInput:
			if chinese {
				fmt.Fprintf(&b, "%s在等你：%s\n", title, truncateChatTaskText(task.Question, 160))
			} else {
				fmt.Fprintf(&b, desktopChatCopy(lang, false, "%s is waiting for you: %s\n", "%s在等你：%s\n"), title, truncateChatTaskText(task.Question, 160))
			}
		case chatTaskDone:
			if chinese {
				fmt.Fprintf(&b, "%s已经做好。", title)
			} else {
				fmt.Fprintf(&b, desktopChatCopy(lang, false, "%s is done.", "%s已经做好。"), title)
			}
			if task.ResultText != "" {
				fmt.Fprintf(&b, " %s", truncateChatTaskText(task.ResultText, 160))
			}
			b.WriteByte('\n')
		case chatTaskFailed:
			detail := task.Error
			if detail == "" {
				detail = task.ResultText
			}
			if chinese {
				fmt.Fprintf(&b, "%s没做成。", title)
			} else {
				fmt.Fprintf(&b, desktopChatCopy(lang, false, "%s failed.", "%s没做成。"), title)
			}
			if detail != "" {
				fmt.Fprintf(&b, " %s", truncateChatTaskText(detail, 160))
			}
			b.WriteByte('\n')
		case chatTaskCanceled:
			if chinese {
				fmt.Fprintf(&b, "%s已停下。\n", title)
			} else {
				fmt.Fprintf(&b, desktopChatCopy(lang, false, "%s was stopped.\n", "%s已停下。\n"), title)
			}
		case chatTaskInterrupted:
			if chinese {
				fmt.Fprintf(&b, "%s上次中断了，没有自动重开。\n", title)
			} else {
				fmt.Fprintf(&b, desktopChatCopy(lang, false, "%s was still running when the app stopped. It was not resumed.\n", "%s上次中断了，没有自动重开。\n"), title)
			}
		default:
			if chinese {
				fmt.Fprintf(&b, "%s还在做。", title)
			} else {
				fmt.Fprintf(&b, desktopChatCopy(lang, false, "%s is still running.", "%s还在做。"), title)
			}
			if task.Progress != "" {
				fmt.Fprintf(&b, " %s", truncateChatTaskText(task.Progress, 160))
			}
			b.WriteByte('\n')
		}
		if line := chatTaskProgressAnswer(task); line != "" {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// chatTaskProgressAnswer reads the three sections for a progress question.
// The sentence is the answer itself. SendToUser stays empty, so this is not a new progress bubble.
func chatTaskProgressAnswer(task AIAssistantChatTask) string {
	copy := desktopbot.Sections{WhereWeAre: task.WhereWeAre, NextStep: task.NextStep, OpenProblems: task.OpenProblems}
	if strings.TrimSpace(copy.WhereWeAre) == "" && strings.TrimSpace(copy.NextStep) == "" && strings.TrimSpace(copy.OpenProblems) == "" {
		return ""
	}
	fileText := ""
	awake := false
	if path := strings.TrimSpace(task.ProgressPath); path != "" {
		if body, err := os.ReadFile(path); err == nil {
			fileText = string(body)
			awake = true
		}
	}
	read := desktopbot.ReadProgress(fileText, copy, awake)
	return read.Text
}

func looksLikeChatTaskStatusQuestion(text string) bool {
	text = strings.TrimSpace(strings.ToLower(text))
	if text == "" || utf8.RuneCountInString(text) > 80 {
		return false
	}
	needles := []string{"进度", "怎么样", "做完", "好了吗", "完成了吗", "task status", "how's", "how is", "how are", "where are we", "background task", "还在跑", "后台任务"}
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func chatTaskStatusOpen(status string) bool {
	return !chatTaskStatus(status).terminal()
}

func chatTaskArg(args map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if args == nil {
			return ""
		}
		if text, ok := args[key].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func chatTaskToolRejected(name, reason string) toolExecutionResult {
	return toolExecutionResult{
		Text:        "[system rejected] " + reason,
		ToolName:    name,
		Outcome:     toolOutcomeFailed,
		FailureKind: toolFailurePolicyRejected,
	}
}

func chatTaskArtifacts(resp *IMAgentResponse, loop *LoopContext) []string {
	if resp == nil && loop == nil {
		return nil
	}
	var paths []string
	if resp != nil {
		paths = append(paths, resp.LocalFilePaths...)
		paths = append(paths, resp.LocalFilePath)
	}
	if loop != nil {
		paths = append(paths, loop.WorkflowWrittenFiles...)
	}
	return chatTaskUniquePaths(paths)
}

func chatTaskUniquePaths(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isChatTaskCancelErr(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "canceled") || strings.Contains(text, "cancelled")
}
