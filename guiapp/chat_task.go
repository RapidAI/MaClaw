package guiapp

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

const (
	chatTaskLLMCaller    = "chat_task_loop"
	chatTaskResultSource = "chat_task_result"
	chatTaskEventName    = "ai-assistant-task"
	chatTaskStoreFile    = "chat-tasks.json"
	// A short reply, at most two inline lookups, then the closing sentence.
	// A third tool call is rejected and has to be handed off.
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
	CreatedAtUnix int64    `json:"created_at_unix"`
	UpdatedAtUnix int64    `json:"updated_at_unix"`
	Recovered     bool     `json:"recovered,omitempty"`
}

type AIAssistantChatTaskDispatchRequest struct {
	Text        string `json:"text"`
	ProjectPath string `json:"project_path,omitempty"`
	SessionKey  string `json:"session_key,omitempty"`
	Lang        string `json:"lang,omitempty"`
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
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Recovered     bool

	loop       *LoopContext
	answerCh   chan string
	cancelCh   chan struct{}
	cancelOnce sync.Once
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
}

type chatTaskDisk struct {
	Tasks []AIAssistantChatTask `json:"tasks"`
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
		Title:      chatTaskTitle(prompt),
		Prompt:     prompt,
		Status:     chatTaskQueued,
		CreatedAt:  now,
		UpdatedAt:  now,
		answerCh:   make(chan string, 1),
		cancelCh:   make(chan struct{}),
	}
	r.tasks[id] = task
	snap := task.snapshotLocked()
	r.mu.Unlock()
	r.persist()
	r.emit(snap)
	go r.run(task)
	return snap, nil
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
		if sessionKey != "" && task.SessionKey != sessionKey {
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
		if task.SessionKey == sessionKey && !task.Status.terminal() {
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
	if task.Status != chatTaskNeedsInput {
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
	note := "[用户补充] " + text
	if loop != nil {
		loop.TryRequestReplan(func() {
			if r.handler != nil {
				r.handler.accumulateInjection(memoryKey, note)
			}
		})
		return nil
	}
	if r.handler != nil {
		r.handler.accumulateInjection(memoryKey, note)
	}
	return nil
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
	if h.memory != nil {
		if snap := h.memory.Load(task.SessionKey); len(snap) > 0 {
			copied := append([]agent.ConversationEntry(nil), snap...)
			h.memory.Save(task.MemoryKey, copied)
		}
	}
	maxIter := h.getMaclawAgentMaxIterations()
	if maxIter <= 0 {
		maxIter = 30
	}
	loop := NewLoopContext("chat-task-"+task.ID, maxIter, h.taskClient)
	loop.LLMCaller = chatTaskLLMCaller
	loop.ChatTaskID = task.ID
	loop.UserID = task.MemoryKey
	loop.Platform = desktopPlatform
	loop.Description = task.Title
	h.bindChatTaskLoop(task.ID, loop)
	resp := h.HandleIMMessageWithExistingLoop(IMUserMessage{
		RequestID: "chat-task-" + task.ID,
		UserID:    task.MemoryKey,
		Platform:  desktopPlatform,
		Text:      task.Prompt,
	}, loop, func(text string) {
		milestone, ok := chatTaskProgressMilestone(text)
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
	return chatTaskResultFromAgentResponse(resp, loop)
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
	if text == "" && len(artifacts) == 0 {
		return chatTaskRunResult{Err: fmt.Errorf("ended without a verified result")}
	}
	return chatTaskRunResult{Text: text, Artifacts: artifacts}
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
	task.Progress = text
	task.UpdatedAt = time.Now()
	snap := task.snapshotLocked()
	r.mu.Unlock()
	r.emit(snap)
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

func (r *chatTaskRegistry) persist() {
	if r == nil || r.storePath == "" {
		return
	}
	// Serialize the snapshot and the rename. Otherwise two persists can list
	// in order A, B and write in order B, A, dropping the newer snapshot.
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	blob, err := json.MarshalIndent(chatTaskDisk{Tasks: r.List("")}, "", "  ")
	if err != nil {
		log.Printf("[chat-task] persist marshal: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(r.storePath), 0o755); err != nil {
		log.Printf("[chat-task] persist mkdir: %v", err)
		return
	}
	tmp := r.storePath + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o644); err != nil {
		log.Printf("[chat-task] persist write: %v", err)
		return
	}
	if err := os.Rename(tmp, r.storePath); err != nil {
		log.Printf("[chat-task] persist rename: %v", err)
	}
}

func (r *chatTaskRegistry) load() {
	if r == nil || r.storePath == "" {
		return
	}
	blob, err := os.ReadFile(r.storePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[chat-task] load: %v", err)
		}
		return
	}
	var disk chatTaskDisk
	if err := json.Unmarshal(blob, &disk); err != nil {
		log.Printf("[chat-task] load parse: %v", err)
		return
	}
	changed := false
	r.mu.Lock()
	for _, snap := range disk.Tasks {
		rec := recordFromSnapshot(snap)
		if !rec.Status.terminal() {
			rec.Status = chatTaskInterrupted
			rec.Progress = "Was running when the app stopped. It was not resumed."
			rec.Recovered = true
			rec.Question = ""
			changed = true
		}
		rec.answerCh = make(chan string, 1)
		rec.cancelCh = make(chan struct{})
		r.tasks[rec.ID] = rec
	}
	r.mu.Unlock()
	if changed {
		r.persist()
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
		if task.SessionKey == sessionKey && !task.Status.terminal() {
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
	b.WriteString("Answer quick questions and at most two trivial lookups here. Multi-step work, long tool sequences, computer use, files, research, office documents, and batch edits go to dispatch_background_task. After it starts, one plain sentence and stop. Do not keep doing that work here.\n")
	b.WriteString("A follow-up or correction to work already running goes to steer_background_task for that same work. Do not start a second one for it. If more than one is running and it is unclear which, ask which one.\n")
	b.WriteString("If the user wants running work stopped, cancelled, or held, in any language or phrasing, call stop_all_work before you answer. Do not start new work in that turn. You may say the work stopped only by repeating the tool's say field, and only when idle is true. If idle is false, repeat say and do not claim it has stopped. Never say the work stopped without calling the tool. A change to the content (\"stop using bold\", \"stop the music in the video\") is not a request to stop the work. If you are unsure, call stop_all_work with intent pause instead of continuing silently.\n")
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
		Text:           formatChatTaskStatus(tasks, desktopChatChinese(msg.Lang, msg.Text)),
		SessionKey:     sessionKey,
		ResponseSource: "chat_task_status",
	}, true
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
	next := loop.Iteration() + 1
	if next < 1 {
		next = 1
	}
	loop.SetMaxIterations(next)
	return toolExecutionResult{
		Text:     "Started: " + snap.Title + ". Tell the user in one plain sentence, then stop. Do not continue the work in this turn. Do not mention ids, tool names, or lanes.",
		ToolName: "dispatch_background_task",
		Outcome:  toolOutcomeSucceeded,
	}
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
	next := loop.Iteration() + 1
	if next < 1 {
		next = 1
	}
	loop.SetMaxIterations(next)
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

func formatChatTaskStatus(tasks []AIAssistantChatTask, chinese bool) string {
	var b strings.Builder
	for _, task := range tasks {
		title := strings.TrimSpace(task.Title)
		if title == "" {
			title = "这件事"
			if !chinese {
				title = "that work"
			}
		}
		switch chatTaskStatus(task.Status) {
		case chatTaskNeedsInput:
			if chinese {
				fmt.Fprintf(&b, "%s在等你：%s\n", title, truncateChatTaskText(task.Question, 160))
			} else {
				fmt.Fprintf(&b, "%s is waiting for you: %s\n", title, truncateChatTaskText(task.Question, 160))
			}
		case chatTaskDone:
			if chinese {
				fmt.Fprintf(&b, "%s已经做好。", title)
			} else {
				fmt.Fprintf(&b, "%s is done.", title)
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
				fmt.Fprintf(&b, "%s failed.", title)
			}
			if detail != "" {
				fmt.Fprintf(&b, " %s", truncateChatTaskText(detail, 160))
			}
			b.WriteByte('\n')
		case chatTaskCanceled:
			if chinese {
				fmt.Fprintf(&b, "%s已停下。\n", title)
			} else {
				fmt.Fprintf(&b, "%s was stopped.\n", title)
			}
		case chatTaskInterrupted:
			if chinese {
				fmt.Fprintf(&b, "%s上次中断了，没有自动重开。\n", title)
			} else {
				fmt.Fprintf(&b, "%s was still running when the app stopped. It was not resumed.\n", title)
			}
		default:
			if chinese {
				fmt.Fprintf(&b, "%s还在做。", title)
			} else {
				fmt.Fprintf(&b, "%s is still running.", title)
			}
			if task.Progress != "" {
				fmt.Fprintf(&b, " %s", truncateChatTaskText(task.Progress, 160))
			}
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
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
