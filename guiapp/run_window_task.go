package guiapp

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

const (
	runWindowPending   = "pending"
	runWindowRunning   = "running"
	runWindowCompleted = "completed"
	runWindowMissed    = "missed"
	runWindowFailed    = "failed"
	runWindowStopped   = "stopped"

	// A claim with no project yet is a launch lease. Past this, the launcher
	// died or never called back, so the row can be started again. Creating a
	// task can include workspace setup, so the lease stays longer than one poll.
	runWindowClaimLease = 10 * time.Minute
	// After the session is attached, the assistant tab may still be opening
	// before the agent loop exists. Keep asking for the pause until that
	// window passes, then treat a miss as nothing left in flight.
	runWindowPauseAttachGrace = 3 * time.Minute
)

// RunWindowDraft is the new-task wizard payload replayed when the window opens.
// Remote passwords are not stored.
type RunWindowDraft struct {
	TaskType           string            `json:"taskType"`
	ExpertID           string            `json:"expertId"`
	ExpertName         string            `json:"expertName"`
	WorkflowTemplateID string            `json:"workflowTemplateId"`
	WorkspaceKind      string            `json:"workspaceKind"`
	LocalPath          string            `json:"localPath"`
	CloudWorkspaceID   string            `json:"cloudWorkspaceId"`
	CloudName          string            `json:"cloudName"`
	RemoteHost         string            `json:"remoteHost"`
	RemotePort         int               `json:"remotePort"`
	RemoteUser         string            `json:"remoteUser"`
	RemoteWorkDir      string            `json:"remoteWorkDir"`
	WorkflowParams     map[string]string `json:"workflowParams,omitempty"`
	LatexTemplateID    string            `json:"latexTemplateId"`
	LatexTemplateName  string            `json:"latexTemplateName"`
}

// RunWindowTask is a one-shot new-task launch held until StartAt.
// EndAt is optional: empty means the task runs until its own work finishes.
type RunWindowTask struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Prompt         string         `json:"prompt"`
	InitialMessage string         `json:"initialMessage"`
	Draft          RunWindowDraft `json:"draft"`
	Status         string         `json:"status"`
	StartAt        time.Time      `json:"startAt"`
	EndAt          *time.Time     `json:"endAt,omitempty"`
	ProjectPath    string         `json:"projectPath,omitempty"`
	SessionUserID  string         `json:"sessionUserId,omitempty"`
	ClaimedAt      *time.Time     `json:"claimedAt,omitempty"`
	StartedAt      *time.Time     `json:"startedAt,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	LastError      string         `json:"lastError,omitempty"`
}

// RunWindowCreateRequest is the wizard payload for a scheduled new task.
type RunWindowCreateRequest struct {
	Name           string         `json:"name"`
	Prompt         string         `json:"prompt"`
	InitialMessage string         `json:"initialMessage"`
	Draft          RunWindowDraft `json:"draft"`
	StartAt        string         `json:"startAt"`
	EndAt          string         `json:"endAt"`
}

type runWindowManager struct {
	mu        sync.Mutex
	sweepMu   sync.Mutex
	path      string
	tasks     []RunWindowTask
	stopCh    chan struct{}
	startOnce sync.Once
	onChange  func()
	// pauseRun lets the in-flight request or subtask finish, then pauses.
	// accepted is false when no loop is running yet.
	pauseRun func(sessionUserID string) (accepted bool, err error)
	// stopRun cancels immediately. Used when the user deletes the plan row.
	stopRun func(sessionUserID string) error
	// workFinished reports that the attached session has stopped executing
	// this task. allowImplicit is set only when the row has no deadline;
	// an open window must not be declared done just because the loop has
	// not published yet. Nil keeps the row running.
	workFinished func(sessionUserID string, startedAt, now time.Time, allowImplicit bool) bool
}

func newRunWindowManager(path string) (*runWindowManager, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("运行时段任务存储路径为空")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	m := &runWindowManager{
		path:   abs,
		tasks:  []RunWindowTask{},
		stopCh: make(chan struct{}),
	}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

func (a *App) ensureRunWindowManager() {
	if a == nil {
		return
	}
	a.runWindowManagerMu.Lock()
	defer a.runWindowManagerMu.Unlock()
	if a.runWindowManager != nil {
		return
	}
	baseDir := a.getMaclawBaseDir()
	m, err := newRunWindowManager(filepath.Join(baseDir, "run_window_tasks.json"))
	if err != nil {
		log.Printf("[run-window] init failed: %v", err)
		return
	}
	m.onChange = func() { a.emitEvent("scheduled-tasks-changed") }
	m.pauseRun = func(sessionUserID string) (bool, error) {
		return a.pauseRunWindowAfterCurrent(sessionUserID)
	}
	m.stopRun = func(sessionUserID string) error {
		return a.stopRunWindowSession(sessionUserID)
	}
	m.workFinished = func(sessionUserID string, startedAt, now time.Time, allowImplicit bool) bool {
		return a.runWindowWorkFinished(sessionUserID, startedAt, now, allowImplicit)
	}
	m.start()
	a.runWindowManager = m
}

// stopRunWindowSession cancels the turn this window is attached to.
// CancelAIAssistantSessionForSession folds slash spellings onto one project
// id and cancels that id. A goal continuation is dispatched on the stored
// row, so its loop can be the other spelling. That loop is stopped here too.
func (a *App) stopRunWindowSession(sessionUserID string) error {
	sessionUserID = strings.TrimSpace(sessionUserID)
	if a == nil || sessionUserID == "" {
		return nil
	}
	_, err := a.CancelAIAssistantSessionForSession(sessionUserID)
	// Skip only an id this call already stopped. A failed cancel must still
	// be retried on every spelling, including the folded project id.
	skipped := ""
	if err == nil {
		if id, normErr := normalizeAIAssistantSessionUserID(sessionUserID); normErr == nil {
			skipped = id
		}
	}
	handler := a.runWindowHandler()
	if handler == nil {
		return err
	}
	if handler.stopOtherRunWindowLoops(sessionUserID, skipped) {
		return nil
	}
	return err
}

func (h *IMMessageHandler) stopOtherRunWindowLoops(sessionUserID, alreadyStopped string) bool {
	if h == nil {
		return false
	}
	alreadyStopped = strings.TrimSpace(alreadyStopped)
	stopped := false
	for _, id := range runWindowSessionIDs(sessionUserID) {
		if id == "" || id == alreadyStopped {
			continue
		}
		ctx, text, _ := h.loopContextForCancel(id)
		if ctx == nil && strings.TrimSpace(text) == "" {
			continue
		}
		if h.app != nil {
			h.app.revokeDesktopCodingTaskRelation(id)
		}
		if _, err := h.CancelSessionForUser(id); err == nil {
			stopped = true
		}
	}
	return stopped
}

func (a *App) pauseRunWindowAfterCurrent(sessionUserID string) (bool, error) {
	sessionUserID = strings.TrimSpace(sessionUserID)
	if a == nil || sessionUserID == "" {
		return false, nil
	}
	handler := a.runWindowHandler()
	if handler == nil {
		handler = a.ensureLocalIMHandler()
	}
	if handler == nil {
		return false, fmt.Errorf("agent handler not initialized")
	}
	if !handler.RequestPauseAfterCurrentUnit(sessionUserID) {
		log.Printf("[run-window] end reached with no in-flight request session=%s", sessionUserID)
		return false, nil
	}
	return true, nil
}

// noteRunWindowSessionSettled rechecks the row bound to this session after a
// turn ends. Other chats do not sweep, and the manager is left alone until
// the app has already started it.
func (a *App) noteRunWindowSessionSettled(sessionUserID string) {
	sessionUserID = strings.TrimSpace(sessionUserID)
	if a == nil || sessionUserID == "" {
		return
	}
	a.runWindowManagerMu.Lock()
	m := a.runWindowManager
	a.runWindowManagerMu.Unlock()
	if m == nil || !m.tracksRunningSession(sessionUserID) {
		return
	}
	m.sweep(time.Now())
}

// runWindowHandler returns the desktop agent handler when one is already
// running. It does not start Hub or interaction infra.
func (a *App) runWindowHandler() *IMMessageHandler {
	if a == nil {
		return nil
	}
	if h := a.imHandler; h != nil {
		return h
	}
	return a.hubClient().currentIMHandler()
}

// runWindowWorkFinished is true once the attached session is idle and its
// latest loop ended after this task started. allowImplicit covers a restart,
// which drops the in-memory end time: after the launch grace, a task with no
// deadline is no longer executing. An open deadline stays running until the
// loop is actually observed to end.
func (a *App) runWindowWorkFinished(sessionUserID string, startedAt, now time.Time, allowImplicit bool) bool {
	if a == nil {
		return false
	}
	handler := a.runWindowHandler()
	// An open window only completes when this process saw the loop end, so a
	// missing handler must stay running. Implicit completion is the restart
	// path and is the only one allowed to bring the handler up.
	if handler == nil && allowImplicit {
		handler = a.ensureLocalIMHandler()
	}
	return runWindowWorkFinishedOn(handler, a.goalContinuation, sessionUserID, startedAt, now, allowImplicit)
}

func runWindowWorkFinishedOn(handler *IMMessageHandler, continuation *GoalContinuationEngine, sessionUserID string, startedAt, now time.Time, allowImplicit bool) bool {
	sessionUserID = strings.TrimSpace(sessionUserID)
	if handler == nil || sessionUserID == "" || startedAt.IsZero() || now.Before(startedAt) {
		return false
	}
	if handler.runWindowSessionBusy(sessionUserID) {
		return false
	}
	if continuation != nil && continuation.blocksRunWindowCompletion(sessionUserID) {
		return false
	}
	if handler.runWindowWorkflowActive(sessionUserID) {
		return false
	}
	ended := handler.sessionLoopEndedAt(sessionUserID)
	if !ended.IsZero() && !ended.Before(startedAt) {
		return true
	}
	return allowImplicit && now.Sub(startedAt) > runWindowPauseAttachGrace
}

// runWindowSessionIDs is every spelling of one chat. A goal continuation is
// dispatched on the stored row, so its loop is published under
// desktop-user:F:\bird2 while the window is bound to desktop-user:F:/bird2.
// The maps stay where the turn wrote them. Readers look at both.
func runWindowSessionIDs(userID string) []string {
	userID = strings.TrimSpace(userID)
	ids := goalBarCandidateIDs(userID)
	if len(ids) == 0 && userID != "" {
		return []string{userID}
	}
	return ids
}

func runWindowSessionMatches(bound, notified string) bool {
	notified = strings.TrimSpace(notified)
	if notified == "" {
		return false
	}
	for _, id := range runWindowSessionIDs(bound) {
		if id == notified {
			return true
		}
	}
	return false
}

func (h *IMMessageHandler) runWindowSessionBusy(userID string) bool {
	if h == nil || strings.TrimSpace(userID) == "" {
		return false
	}
	// Pending text stays set until the request lock is released, which is
	// after the loop has already finished. It is not another turn yet.
	for _, id := range runWindowSessionIDs(userID) {
		if h.inFlightTurnForUser(id) != nil || h.getSessionLoopCtx(id) != nil {
			return true
		}
	}
	return false
}

func (h *IMMessageHandler) runWindowWorkflowActive(userID string) bool {
	if h == nil {
		return false
	}
	for _, id := range runWindowSessionIDs(userID) {
		if h.isWorkflowV2Active(id) {
			return true
		}
	}
	return false
}

func (h *IMMessageHandler) sessionLoopEndedAt(userID string) time.Time {
	if h == nil || strings.TrimSpace(userID) == "" {
		return time.Time{}
	}
	var latest time.Time
	for _, id := range runWindowSessionIDs(userID) {
		v, ok := h.sessionLoops.Load(id)
		if !ok {
			continue
		}
		state := v.(*sessionLoopState)
		state.stateMu.RLock()
		ended := state.endedAt
		state.stateMu.RUnlock()
		if ended.After(latest) {
			latest = ended
		}
	}
	return latest
}

func (h *IMMessageHandler) RequestPauseAfterCurrentUnit(userID string) bool {
	userID = strings.TrimSpace(userID)
	if h == nil || userID == "" {
		return false
	}
	accepted := false
	for _, id := range runWindowSessionIDs(userID) {
		ctx := h.getSessionLoopCtx(id)
		if ctx == nil {
			if turn := h.inFlightTurnForUser(id); turn != nil {
				ctx = turn.ctx
			}
		}
		if ctx == nil {
			continue
		}
		ctx.RequestPauseAfterCurrent()
		accepted = true
	}
	return accepted
}

func takeRunWindowPause(ctx *LoopContext) (string, bool) {
	if ctx == nil || !ctx.ConsumePauseAfterCurrent() {
		return "", false
	}
	ctx.SetLoopState(LoopStateStopped)
	text := "运行时段已结束。当前请求或小任务已完成，任务已暂停。"
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(ctx.Lang)), "en") {
		text = "The run window ended. The current request or subtask finished, and the task is paused."
	}
	log.Printf("[run-window] paused after current unit loop=%s user=%s", ctx.ID, ctx.UserID)
	return text, true
}

// runWindowPauseResponse stops the loop after the current unit. The reply text
// stays the answer already produced, so the client can keep the streamed
// message. The pause line is returned separately for the progress channel.
func runWindowPauseResponse(ctx *LoopContext, conversation []interface{}) (note string, resp *IMAgentResponse, paused bool) {
	note, paused = takeRunWindowPause(ctx)
	if !paused {
		return "", nil, false
	}
	text := note
	if prior := lastAssistantReply(conversation); prior != "" {
		text = prior
	}
	return note, &IMAgentResponse{Text: text, runWindowPaused: true}, true
}

func runWindowClaimStale(task *RunWindowTask, now time.Time) bool {
	if task == nil || task.ClaimedAt == nil {
		return true
	}
	return now.Sub(*task.ClaimedAt) > runWindowClaimLease
}

func (m *runWindowManager) reclaimAbandonedClaimLocked(task *RunWindowTask, now time.Time) {
	if task.EndAt != nil && !now.Before(*task.EndAt) {
		task.Status = runWindowFailed
		task.LastError = "启动超时，运行时段已结束"
		return
	}
	task.Status = runWindowPending
	task.ClaimedAt = nil
	task.LastError = ""
}

func runWindowPauseGraceElapsed(task *RunWindowTask, now time.Time) bool {
	if task == nil || task.StartedAt == nil {
		return true
	}
	return !now.Before(task.StartedAt.Add(runWindowPauseAttachGrace))
}

func lastAssistantReply(conversation []interface{}) string {
	for i := len(conversation) - 1; i >= 0; i-- {
		role, content, hasTools := conversationMessageView(conversation[i])
		if role != "assistant" || hasTools {
			continue
		}
		if text := strings.TrimSpace(content); text != "" {
			return text
		}
	}
	for i := len(conversation) - 1; i >= 0; i-- {
		role, content, _ := conversationMessageView(conversation[i])
		if role != "assistant" {
			continue
		}
		if text := strings.TrimSpace(content); text != "" {
			return text
		}
	}
	return ""
}

func conversationMessageView(item interface{}) (role, content string, hasTools bool) {
	switch msg := item.(type) {
	case llm.Message:
		return msg.Role, msg.Content, len(msg.ToolCalls) > 0
	case *llm.Message:
		if msg == nil {
			return "", "", false
		}
		return msg.Role, msg.Content, len(msg.ToolCalls) > 0
	case agent.ConversationEntry:
		return msg.Role, conversationContentText(msg.Content), msg.ToolCalls != nil && !emptyToolCalls(msg.ToolCalls)
	case map[string]string:
		return msg["role"], msg["content"], false
	case map[string]interface{}:
		role, _ = msg["role"].(string)
		content = conversationContentText(msg["content"])
		if calls, ok := msg["tool_calls"]; ok {
			hasTools = !emptyToolCalls(calls)
		}
		return role, content, hasTools
	default:
		return "", "", false
	}
}

func conversationContentText(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []interface{}:
		var b strings.Builder
		for _, item := range typed {
			part, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			text, _ := part["text"].(string)
			if text == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(text)
		}
		return b.String()
	default:
		return ""
	}
}

func emptyToolCalls(calls interface{}) bool {
	switch typed := calls.(type) {
	case nil:
		return true
	case []llm.ToolCall:
		return len(typed) == 0
	case []interface{}:
		return len(typed) == 0
	default:
		return false
	}
}

func (m *runWindowManager) start() {
	m.startOnce.Do(func() {
		go m.loop()
	})
}

func (m *runWindowManager) loop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	m.sweep(time.Now())
	for {
		select {
		case <-m.stopCh:
			return
		case now := <-ticker.C:
			m.sweep(now)
		}
	}
}

type runWindowPauseJob struct {
	id       string
	session  string
	accepted bool
	unwired  bool
}

type runWindowFinishCandidate struct {
	id            string
	session       string
	startedAt     time.Time
	allowImplicit bool
}

func (m *runWindowManager) tracksRunningSession(sessionUserID string) bool {
	sessionUserID = strings.TrimSpace(sessionUserID)
	if m == nil || sessionUserID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.tasks {
		task := &m.tasks[i]
		if task.Status == runWindowRunning && runWindowSessionMatches(task.SessionUserID, sessionUserID) {
			return true
		}
	}
	return false
}

func (m *runWindowManager) sweep(now time.Time) {
	if m == nil {
		return
	}
	// List, the 15s ticker, and turn-end notices can arrive together.
	// One sweep at a time keeps a pause from being requested twice.
	m.sweepMu.Lock()
	defer m.sweepMu.Unlock()
	var pauseJobs []runWindowPauseJob
	var finishJobs []runWindowFinishCandidate
	m.mu.Lock()
	changed := false
	for i := range m.tasks {
		task := &m.tasks[i]
		if task.Status == runWindowPending && task.EndAt != nil && !now.Before(*task.EndAt) {
			task.Status = runWindowMissed
			task.LastError = "运行时段已结束，任务未启动"
			changed = true
			continue
		}
		if task.Status != runWindowRunning {
			continue
		}
		if strings.TrimSpace(task.ProjectPath) == "" {
			if runWindowClaimStale(task, now) {
				m.reclaimAbandonedClaimLocked(task, now)
				changed = true
			}
			continue
		}
		session := strings.TrimSpace(task.SessionUserID)
		if session != "" && task.StartedAt != nil && m.workFinished != nil {
			finishJobs = append(finishJobs, runWindowFinishCandidate{
				id: task.ID, session: session, startedAt: *task.StartedAt,
				allowImplicit: task.EndAt == nil,
			})
		}
		if task.EndAt == nil || now.Before(*task.EndAt) || session == "" {
			continue
		}
		pauseJobs = append(pauseJobs, runWindowPauseJob{id: task.ID, session: session})
	}
	if changed {
		_ = m.saveLocked()
	}
	cb := m.onChange
	pauseRun := m.pauseRun
	workFinished := m.workFinished
	m.mu.Unlock()
	if changed && cb != nil {
		cb()
	}

	finished := map[string]bool{}
	for _, job := range finishJobs {
		if workFinished != nil && workFinished(job.session, job.startedAt, now, job.allowImplicit) {
			finished[job.id] = true
		}
	}
	for i := range pauseJobs {
		if finished[pauseJobs[i].id] {
			continue
		}
		if pauseRun == nil {
			pauseJobs[i].unwired = true
			continue
		}
		accepted, err := pauseRun(pauseJobs[i].session)
		if err != nil {
			log.Printf("[run-window] pause after current unit failed session=%s err=%v", pauseJobs[i].session, err)
			continue
		}
		pauseJobs[i].accepted = accepted
	}
	if len(finished) == 0 && len(pauseJobs) == 0 {
		return
	}

	m.mu.Lock()
	changed = false
	for id := range finished {
		task, _, err := m.find(id)
		if err != nil || task.Status != runWindowRunning {
			continue
		}
		task.Status = runWindowCompleted
		changed = true
		log.Printf("[run-window] work finished id=%s session=%s", task.ID, task.SessionUserID)
	}
	for _, job := range pauseJobs {
		if finished[job.id] {
			continue
		}
		task, _, err := m.find(job.id)
		if err != nil || task.Status != runWindowRunning {
			continue
		}
		if task.EndAt == nil || now.Before(*task.EndAt) {
			continue
		}
		// No loop yet, or the pause call failed. Leave the row running so the
		// next sweep can attach the flag once the request exists.
		if pauseRun != nil && !job.unwired && !job.accepted && !runWindowPauseGraceElapsed(task, now) {
			continue
		}
		task.Status = runWindowStopped
		changed = true
	}
	if changed {
		_ = m.saveLocked()
	}
	cb = m.onChange
	m.mu.Unlock()
	if changed && cb != nil {
		cb()
	}
}

func (m *runWindowManager) List() []RunWindowTask {
	m.sweep(time.Now())
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]RunWindowTask, 0, len(m.tasks))
	for _, task := range m.tasks {
		out = append(out, cloneRunWindowTask(task))
	}
	return out
}

func (m *runWindowManager) Get(id string) (RunWindowTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, _, err := m.find(id)
	if err != nil {
		return RunWindowTask{}, err
	}
	return cloneRunWindowTask(*task), nil
}

func (m *runWindowManager) Create(req RunWindowCreateRequest, now time.Time) (string, error) {
	name := truncateRunWindowName(firstNonEmpty(req.Name, req.Prompt))
	prompt := strings.TrimSpace(req.Prompt)
	if name == "" || prompt == "" {
		return "", fmt.Errorf("请先输入任务内容")
	}
	start, err := parseRunWindowTime(req.StartAt, true)
	if err != nil {
		return "", err
	}
	end, err := parseRunWindowTime(req.EndAt, false)
	if err != nil {
		return "", err
	}
	if err := validateRunWindowRange(*start, end, now, true); err != nil {
		return "", err
	}
	draft := normalizeRunWindowDraft(req.Draft)
	if err := validateRunWindowDraft(draft); err != nil {
		return "", err
	}
	initial := strings.TrimSpace(req.InitialMessage)
	if initial == "" {
		initial = prompt
	}
	task := RunWindowTask{
		ID:             newRunWindowID(),
		Name:           name,
		Prompt:         prompt,
		InitialMessage: initial,
		Draft:          draft,
		Status:         runWindowPending,
		StartAt:        *start,
		EndAt:          end,
		CreatedAt:      now,
	}
	m.mu.Lock()
	m.tasks = append(m.tasks, task)
	err = m.saveLocked()
	if err != nil {
		m.tasks = m.tasks[:len(m.tasks)-1]
		m.mu.Unlock()
		return "", err
	}
	cb := m.onChange
	m.mu.Unlock()
	if cb != nil {
		cb()
	}
	return task.ID, nil
}

func (m *runWindowManager) UpdateTimes(id, startRaw, endRaw string, now time.Time) error {
	start, err := parseRunWindowTime(startRaw, true)
	if err != nil {
		return err
	}
	end, err := parseRunWindowTime(endRaw, false)
	if err != nil {
		return err
	}
	m.mu.Lock()
	task, _, err := m.find(id)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	switch task.Status {
	case runWindowPending, runWindowMissed, runWindowFailed:
		sameStart := start.Truncate(time.Minute).Equal(task.StartAt.Truncate(time.Minute))
		if err := validateRunWindowRange(*start, end, now, !sameStart); err != nil {
			m.mu.Unlock()
			return err
		}
		task.StartAt = *start
		task.EndAt = end
		task.Status = runWindowPending
		task.LastError = ""
		task.ProjectPath = ""
		task.SessionUserID = ""
		task.ClaimedAt = nil
		task.StartedAt = nil
	case runWindowRunning:
		if end != nil && !end.After(task.StartAt) {
			m.mu.Unlock()
			return fmt.Errorf("结束时间需要晚于开始时间")
		}
		task.EndAt = end
	default:
		m.mu.Unlock()
		return fmt.Errorf("这个任务已经结束，不能再改时间")
	}
	err = m.saveLocked()
	cb := m.onChange
	m.mu.Unlock()
	if err != nil {
		return err
	}
	if cb != nil {
		cb()
	}
	return nil
}

func (m *runWindowManager) Delete(id string) error {
	m.mu.Lock()
	index := -1
	var session string
	var running bool
	for i := range m.tasks {
		if m.tasks[i].ID != id {
			continue
		}
		index = i
		running = m.tasks[i].Status == runWindowRunning
		session = strings.TrimSpace(m.tasks[i].SessionUserID)
		break
	}
	if index < 0 {
		m.mu.Unlock()
		return fmt.Errorf("任务不存在")
	}
	m.tasks = append(m.tasks[:index], m.tasks[index+1:]...)
	err := m.saveLocked()
	cb := m.onChange
	stopRun := m.stopRun
	m.mu.Unlock()
	if err != nil {
		return err
	}
	if cb != nil {
		cb()
	}
	if running && session != "" && stopRun != nil {
		if stopErr := stopRun(session); stopErr != nil {
			log.Printf("[run-window] delete cancel failed id=%s err=%v", id, stopErr)
		}
	}
	return nil
}

func (m *runWindowManager) Claim(id string, now time.Time) (RunWindowTask, error) {
	m.mu.Lock()
	task, _, err := m.find(id)
	if err != nil {
		m.mu.Unlock()
		return RunWindowTask{}, err
	}
	if task.Status != runWindowPending {
		m.mu.Unlock()
		return RunWindowTask{}, fmt.Errorf("任务不在等待开始")
	}
	if now.Before(task.StartAt) {
		m.mu.Unlock()
		return RunWindowTask{}, fmt.Errorf("还没到开始时间")
	}
	if task.EndAt != nil && !now.Before(*task.EndAt) {
		task.Status = runWindowMissed
		task.LastError = "运行时段已结束，任务未启动"
		_ = m.saveLocked()
		cb := m.onChange
		m.mu.Unlock()
		if cb != nil {
			cb()
		}
		return RunWindowTask{}, fmt.Errorf("运行时段已结束")
	}
	task.Status = runWindowRunning
	task.LastError = ""
	claimedAt := now
	task.ClaimedAt = &claimedAt
	err = m.saveLocked()
	if err != nil {
		task.Status = runWindowPending
		task.ClaimedAt = nil
		m.mu.Unlock()
		return RunWindowTask{}, err
	}
	cloned := cloneRunWindowTask(*task)
	cb := m.onChange
	m.mu.Unlock()
	if cb != nil {
		cb()
	}
	return cloned, nil
}

func (m *runWindowManager) Release(id, message string) error {
	return m.release(id, message, time.Now())
}

func (m *runWindowManager) release(id, message string, now time.Time) error {
	message = strings.TrimSpace(message)
	m.mu.Lock()
	task, _, err := m.find(id)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	changed := false
	if task.Status == runWindowRunning && strings.TrimSpace(task.ProjectPath) == "" {
		if task.EndAt != nil && !now.Before(*task.EndAt) {
			task.Status = runWindowMissed
			if message == "" {
				message = "运行时段已结束，任务未启动"
			}
		} else {
			task.Status = runWindowPending
		}
		task.LastError = message
		task.ClaimedAt = nil
		err = m.saveLocked()
		changed = err == nil
	}
	cb := m.onChange
	m.mu.Unlock()
	if err != nil {
		return err
	}
	if changed && cb != nil {
		cb()
	}
	return nil
}

func (m *runWindowManager) MarkStarted(id, projectPath, sessionUserID string) error {
	projectPath = strings.TrimSpace(projectPath)
	sessionUserID = strings.TrimSpace(sessionUserID)
	if projectPath == "" || sessionUserID == "" {
		return fmt.Errorf("任务启动结果不完整")
	}
	m.mu.Lock()
	task, _, err := m.find(id)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	if task.Status != runWindowRunning {
		m.mu.Unlock()
		return fmt.Errorf("任务不在执行中")
	}
	now := time.Now()
	task.ProjectPath = projectPath
	task.SessionUserID = sessionUserID
	task.StartedAt = &now
	task.LastError = ""
	endPassed := task.EndAt != nil && !now.Before(*task.EndAt)
	err = m.saveLocked()
	cb := m.onChange
	pauseRun := m.pauseRun
	m.mu.Unlock()
	if err != nil {
		return err
	}
	if cb != nil {
		cb()
	}
	if !endPassed {
		return nil
	}
	accepted := pauseRun == nil
	if pauseRun != nil {
		var pauseErr error
		accepted, pauseErr = pauseRun(sessionUserID)
		if pauseErr != nil {
			log.Printf("[run-window] pause after start failed session=%s err=%v", sessionUserID, pauseErr)
			return nil
		}
	}
	if !accepted {
		return nil
	}
	m.markStopped(id)
	return nil
}

func (m *runWindowManager) markStopped(id string) {
	m.mu.Lock()
	task, _, err := m.find(id)
	if err != nil || task.Status != runWindowRunning {
		m.mu.Unlock()
		return
	}
	task.Status = runWindowStopped
	err = m.saveLocked()
	cb := m.onChange
	m.mu.Unlock()
	if err != nil || cb == nil {
		return
	}
	cb()
}

func (m *runWindowManager) MarkFailed(id, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "启动失败"
	}
	m.mu.Lock()
	task, _, err := m.find(id)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	if task.Status != runWindowRunning && task.Status != runWindowPending {
		m.mu.Unlock()
		return nil
	}
	task.Status = runWindowFailed
	task.LastError = message
	err = m.saveLocked()
	cb := m.onChange
	m.mu.Unlock()
	if err != nil {
		return err
	}
	if cb != nil {
		cb()
	}
	return nil
}

func (m *runWindowManager) find(id string) (*RunWindowTask, int, error) {
	id = strings.TrimSpace(id)
	for i := range m.tasks {
		if m.tasks[i].ID == id {
			return &m.tasks[i], i, nil
		}
	}
	return nil, -1, fmt.Errorf("任务不存在")
}

func (a *App) ListRunWindowTasks() []RunWindowTask {
	a.ensureRunWindowManager()
	if a == nil || a.runWindowManager == nil {
		return nil
	}
	return a.runWindowManager.List()
}

func (a *App) CreateRunWindowTask(req RunWindowCreateRequest) (string, error) {
	a.ensureRunWindowManager()
	if a == nil || a.runWindowManager == nil {
		return "", fmt.Errorf("运行时段任务不可用")
	}
	return a.runWindowManager.Create(req, time.Now())
}

func (a *App) UpdateRunWindowTaskTimes(id, startAt, endAt string) error {
	a.ensureRunWindowManager()
	if a == nil || a.runWindowManager == nil {
		return fmt.Errorf("运行时段任务不可用")
	}
	return a.runWindowManager.UpdateTimes(id, startAt, endAt, time.Now())
}

func (a *App) DeleteRunWindowTask(id string) error {
	a.ensureRunWindowManager()
	if a == nil || a.runWindowManager == nil {
		return fmt.Errorf("运行时段任务不可用")
	}
	return a.runWindowManager.Delete(id)
}

func (a *App) ClaimRunWindowTask(id string) (RunWindowTask, error) {
	a.ensureRunWindowManager()
	if a == nil || a.runWindowManager == nil {
		return RunWindowTask{}, fmt.Errorf("运行时段任务不可用")
	}
	return a.runWindowManager.Claim(id, time.Now())
}

func (a *App) ReleaseRunWindowTask(id, message string) error {
	a.ensureRunWindowManager()
	if a == nil || a.runWindowManager == nil {
		return fmt.Errorf("运行时段任务不可用")
	}
	return a.runWindowManager.Release(id, message)
}

func (a *App) MarkRunWindowTaskStarted(id, projectPath string) error {
	a.ensureRunWindowManager()
	if a == nil || a.runWindowManager == nil {
		return fmt.Errorf("运行时段任务不可用")
	}
	task, err := a.runWindowManager.Get(id)
	if err != nil {
		return err
	}
	session := projectSessionOwnerID(projectPath)
	if expertID := strings.TrimSpace(task.Draft.ExpertID); expertID != "" {
		session = expertSessionUserID(expertID)
	}
	return a.runWindowManager.MarkStarted(id, projectPath, session)
}

func (a *App) MarkRunWindowTaskFailed(id, message string) error {
	a.ensureRunWindowManager()
	if a == nil || a.runWindowManager == nil {
		return fmt.Errorf("运行时段任务不可用")
	}
	return a.runWindowManager.MarkFailed(id, message)
}

func parseRunWindowTime(raw string, required bool) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if required {
			return nil, fmt.Errorf("请设置开始时间")
		}
		return nil, nil
	}
	layouts := []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02T15:04:05"}
	var parsed time.Time
	var err error
	for i, layout := range layouts {
		if i == 0 {
			parsed, err = time.Parse(layout, raw)
		} else {
			parsed, err = time.ParseInLocation(layout, raw, time.Local)
		}
		if err == nil {
			local := parsed.In(time.Local)
			return &local, nil
		}
	}
	if required {
		return nil, fmt.Errorf("开始时间无效")
	}
	return nil, fmt.Errorf("结束时间无效")
}

func validateRunWindowRange(start time.Time, end *time.Time, now time.Time, rejectPastStart bool) error {
	if rejectPastStart && start.Before(now.Add(-time.Minute)) {
		return fmt.Errorf("开始时间需要晚于当前时间")
	}
	if end != nil && !end.After(start) {
		return fmt.Errorf("结束时间需要晚于开始时间")
	}
	return nil
}

func normalizeRunWindowDraft(draft RunWindowDraft) RunWindowDraft {
	draft.TaskType = strings.ToLower(strings.TrimSpace(draft.TaskType))
	if draft.TaskType != "coding" {
		draft.TaskType = "chat"
	}
	draft.ExpertID = strings.TrimSpace(draft.ExpertID)
	draft.ExpertName = strings.TrimSpace(draft.ExpertName)
	draft.WorkflowTemplateID = strings.TrimSpace(draft.WorkflowTemplateID)
	draft.WorkspaceKind = strings.ToLower(strings.TrimSpace(draft.WorkspaceKind))
	if draft.WorkspaceKind != "cloud" && draft.WorkspaceKind != "remote" {
		draft.WorkspaceKind = "local"
	}
	draft.LocalPath = strings.TrimSpace(draft.LocalPath)
	draft.CloudWorkspaceID = strings.TrimSpace(draft.CloudWorkspaceID)
	draft.CloudName = strings.TrimSpace(draft.CloudName)
	draft.RemoteHost = strings.TrimSpace(draft.RemoteHost)
	draft.RemoteUser = strings.TrimSpace(draft.RemoteUser)
	draft.RemoteWorkDir = strings.TrimSpace(draft.RemoteWorkDir)
	draft.LatexTemplateID = strings.TrimSpace(draft.LatexTemplateID)
	draft.LatexTemplateName = strings.TrimSpace(draft.LatexTemplateName)
	if len(draft.WorkflowParams) == 0 {
		draft.WorkflowParams = nil
	}
	return draft
}

func validateRunWindowDraft(draft RunWindowDraft) error {
	workflowID := draft.WorkflowTemplateID
	if workflowID == "auto" {
		workflowID = ""
	}
	if draft.ExpertID != "" && workflowID != "" {
		return fmt.Errorf("专家和工作流不能同时指定")
	}
	return nil
}

func cloneRunWindowTask(task RunWindowTask) RunWindowTask {
	if task.EndAt != nil {
		end := *task.EndAt
		task.EndAt = &end
	}
	if task.ClaimedAt != nil {
		claimed := *task.ClaimedAt
		task.ClaimedAt = &claimed
	}
	if task.StartedAt != nil {
		started := *task.StartedAt
		task.StartedAt = &started
	}
	if task.Draft.WorkflowParams != nil {
		copied := make(map[string]string, len(task.Draft.WorkflowParams))
		for key, value := range task.Draft.WorkflowParams {
			copied[key] = value
		}
		task.Draft.WorkflowParams = copied
	}
	return task
}

func (m *runWindowManager) load() error {
	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	var tasks []RunWindowTask
	if err := json.Unmarshal(data, &tasks); err != nil {
		return fmt.Errorf("读取运行时段任务失败: %w", err)
	}
	if tasks == nil {
		tasks = []RunWindowTask{}
	}
	m.tasks = tasks
	return nil
}

func (m *runWindowManager) saveLocked() error {
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m.tasks, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, m.path)
}

func newRunWindowID() string {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("rw-%d-%x", time.Now().UnixNano(), buf)
}

func truncateRunWindowName(name string) string {
	name = strings.TrimSpace(name)
	runes := []rune(name)
	if len(runes) <= 80 {
		return name
	}
	return string(runes[:80])
}
