package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	mcpStatusRunning           = "running"
	mcpStatusCompleted         = "completed"
	mcpStatusFailed            = "failed"
	mcpStatusCancelled         = "cancelled"
	mcpStatusIdle              = "idle"
	mcpStatusNeedsConfirmation = "needs_confirmation"
	mcpRunTimeout              = 30 * time.Minute
)

// mcpTaskView is the JSON text returned by create, get, stop, and list.
type mcpTaskView struct {
	ProjectPath string `json:"project_path"`
	Name        string `json:"name,omitempty"`
	Mode        string `json:"mode,omitempty"`
	Status      string `json:"status"`
	Text        string `json:"text,omitempty"`
	Error       string `json:"error,omitempty"`
	Provider    string `json:"provider,omitempty"`
	Model       string `json:"model,omitempty"`
	RequestID   string `json:"request_id,omitempty"`
	// historyCount and historySeal identify the conversation when this run's
	// outcome was sealed. A later assistant reply has a higher count, or the
	// same count with different text. Registry text is formatted separately
	// from history, so it is not the identity of the turn.
	historyCount int    `json:"-"`
	historySeal  string `json:"-"`
}

type mcpRunRecord struct {
	ProjectPath  string
	Name         string
	Mode         string
	Status       string
	Text         string
	Error        string
	Provider     string
	Model        string
	RequestID    string
	historyCount int
	historySeal  string
	cancel       context.CancelFunc
}

type mcpModelPin struct {
	ProviderName string
	Model        string
}

func (rec *mcpRunRecord) view() mcpTaskView {
	if rec == nil {
		return mcpTaskView{}
	}
	return mcpTaskView{
		ProjectPath:  rec.ProjectPath,
		Name:         rec.Name,
		Mode:         rec.Mode,
		Status:       rec.Status,
		Text:         rec.Text,
		Error:        rec.Error,
		Provider:     rec.Provider,
		Model:        rec.Model,
		RequestID:    rec.RequestID,
		historyCount: rec.historyCount,
		historySeal:  rec.historySeal,
	}
}

func (a *App) mcpListModels(ctx context.Context) (mcpModelCatalog, error) {
	if a == nil {
		return mcpModelCatalog{}, errors.New("MCP 任务服务还没有准备好")
	}
	data := a.GetMaclawLLMProviders()
	return mcpModelCatalogFrom(data.Providers, data.Current), nil
}

func (a *App) mcpCreateTask(ctx context.Context, args mcpCreateArgs) (mcpTaskView, error) {
	if a == nil {
		return mcpTaskView{}, errors.New("MCP 任务服务还没有准备好")
	}
	prompt := strings.TrimSpace(args.Prompt)
	name := taskNameForCreate(args.Name, true)
	if name == "" {
		name = taskNameForCreate(recentTaskDisplayTitle(prompt), false)
	}
	if name == "" {
		return mcpTaskView{}, errors.New("任务名称或内容不能为空")
	}
	mode, err := mcpNormalizeTaskMode(args.Mode, args.WorkingDir)
	if err != nil {
		return mcpTaskView{}, err
	}
	providerName, model, err := a.mcpResolveSelection(args.Provider, args.Model)
	if err != nil {
		return mcpTaskView{}, err
	}
	workDir := ""
	if rawDir := strings.TrimSpace(args.WorkingDir); rawDir != "" {
		workDir = normalizeRecentTaskWorkingDir(rawDir)
		if workDir == "" {
			return mcpTaskView{}, errors.New("工作目录必须是本机绝对路径")
		}
	}
	createMode := ""
	if mode == taskCodingDevTag {
		createMode = taskCodingDevTag
	}
	created := a.CreateTaskWithMode(name, workDir, createMode)
	if strings.TrimSpace(created.ProjectPath) == "" {
		return mcpTaskView{}, errors.New("没有创建任务")
	}
	view := mcpTaskView{
		ProjectPath: created.ProjectPath,
		Name:        firstNonEmpty(created.Name, name),
		Mode:        mode,
		Status:      mcpStatusIdle,
		Provider:    providerName,
		Model:       model,
	}
	if providerName != "" {
		a.mcpRememberPin(created.ProjectPath, providerName, model)
	}
	if prompt == "" {
		mark := a.mcpConversationMark(view.ProjectPath)
		view.historyCount = mark.count
		view.historySeal = mark.text
		a.mcpStoreRun(&mcpRunRecord{
			ProjectPath:  view.ProjectPath,
			Name:         view.Name,
			Mode:         view.Mode,
			Status:       mcpStatusIdle,
			Provider:     providerName,
			Model:        model,
			historyCount: mark.count,
			historySeal:  mark.text,
		})
		return view, nil
	}
	return a.mcpStartPrompt(ctx, view, prompt, args.Wait)
}

func (a *App) mcpGetTask(ctx context.Context, projectPath string) (mcpTaskView, error) {
	path := normalizeProjectSessionPath(projectPath)
	if a == nil || path == "" {
		return mcpTaskView{}, errors.New("没有找到这个任务")
	}
	if view, ok := a.mcpViewFromRegistry(path); ok && view.Status == mcpStatusRunning {
		return view, nil
	}
	known, err := a.remoteControlKnownTask(path)
	reg, hasReg := a.mcpViewFromRegistry(path)
	if err != nil {
		if hasReg {
			return reg, nil
		}
		return mcpTaskView{}, err
	}
	store := a.mcpTaskViewFromStore(known)
	if !hasReg {
		return store, nil
	}
	return mcpPreferTaskView(reg, store), nil
}

func (a *App) mcpStopTask(ctx context.Context, projectPath string) (mcpTaskView, error) {
	if a == nil {
		return mcpTaskView{}, errors.New("没有找到这个任务")
	}
	path, err := a.remoteControlKnownTask(projectPath)
	registryPath := normalizeProjectSessionPath(projectPath)
	_, inRegistry := a.mcpViewFromRegistry(registryPath)
	if err != nil && !inRegistry {
		return mcpTaskView{}, err
	}
	if path == "" {
		path = registryPath
	}
	a.mcpCancelRun(path)
	a.mcpDismissPendingConfirmation(path)
	if stopErr := a.stopRemoteControlTask(path); stopErr != nil {
		if view, ok := a.mcpViewFromRegistry(path); ok && view.Status == mcpStatusCancelled {
			view.Error = stopErr.Error()
			return view, nil
		}
		return mcpTaskView{}, stopErr
	}
	if view, ok := a.mcpViewFromRegistry(path); ok {
		return view, nil
	}
	return a.mcpTaskViewFromStore(path), nil
}

func (a *App) mcpListTasks(ctx context.Context) ([]mcpTaskView, error) {
	if a == nil {
		return []mcpTaskView{}, nil
	}
	listed := a.ListTasks(50)
	seen := map[string]struct{}{}
	out := make([]mcpTaskView, 0, len(listed))
	for _, task := range listed {
		path := normalizeProjectSessionPath(task.ProjectPath)
		if path == "" {
			continue
		}
		seen[path] = struct{}{}
		if view, ok := a.mcpViewFromRegistry(path); ok && view.Status == mcpStatusRunning {
			if view.Name == "" {
				view.Name = task.Name
			}
			if view.Mode == "" {
				view.Mode = mcpModeFromTags(task.Tags)
			}
			out = append(out, view)
			continue
		}
		store := a.mcpTaskViewFromStore(path)
		if view, ok := a.mcpViewFromRegistry(path); ok {
			if store.Name == "" {
				store.Name = task.Name
			}
			if store.Mode == "" {
				store.Mode = mcpModeFromTags(task.Tags)
			}
			out = append(out, mcpPreferTaskView(view, store))
			continue
		}
		if store.Name == "" {
			store.Name = task.Name
		}
		if store.Mode == "" {
			store.Mode = mcpModeFromTags(task.Tags)
		}
		out = append(out, store)
	}
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	for path, rec := range a.mcpRuns {
		if _, ok := seen[path]; ok || rec == nil {
			continue
		}
		out = append(out, rec.view())
	}
	return out, nil
}

func (a *App) mcpResolveSelection(provider, model string) (string, string, error) {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" && model == "" {
		return "", "", nil
	}
	data := a.GetMaclawLLMProviders()
	return mcpResolveProviderModel(data.Providers, data.Current, provider, model)
}

func (a *App) mcpStartPrompt(parent context.Context, view mcpTaskView, prompt string, wait bool) (mcpTaskView, error) {
	path := normalizeProjectSessionPath(view.ProjectPath)
	if path == "" {
		return mcpTaskView{}, errors.New("没有创建任务")
	}
	userID := desktopAIAssistantUserIDForProjectPath(path)
	if userID == "" || userID == desktopUserID {
		return mcpTaskView{}, errors.New("没有创建任务")
	}
	requestID := "mcp-" + randomHexID(8)
	runCtx, cancel := context.WithTimeout(context.Background(), mcpRunTimeout)
	mark := a.mcpConversationMark(path)
	rec := &mcpRunRecord{
		ProjectPath:  path,
		Name:         view.Name,
		Mode:         view.Mode,
		Status:       mcpStatusRunning,
		Provider:     view.Provider,
		Model:        view.Model,
		RequestID:    requestID,
		historyCount: mark.count,
		historySeal:  mark.text,
		cancel:       cancel,
	}
	a.mcpReplaceRun(rec)
	a.mirrorMCPUser(prompt, requestID, userID, path, view.Name, view.Mode)

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		resp, err := a.RunAIAssistantProgrammingPrompt(runCtx, AIAssistantSendRequest{
			Text:          prompt,
			ProjectPath:   path,
			RequestID:     requestID,
			Lang:          a.CurrentLanguage,
			StartNewTask:  true,
			PromptProfile: assistantHostMCP,
			TaskMode:      view.Mode,
		}, AIAssistantExternalCallbacks{
			OnToken: func(delta string) {
				a.mirrorMCPToken(requestID, userID, delta)
				if delta != "" && !strings.HasPrefix(delta, "\x01") {
					a.mcpAppendRunText(path, requestID, delta)
				}
			},
			OnProgress: func(text string) {
				a.mirrorMCPProgress(requestID, userID, text)
			},
		})
		status, finalText, errText, keepText := mcpRunOutcome(err, runCtx.Err() != nil, resp)
		if resp != nil {
			a.mirrorMCPFinal(requestID, userID, resp)
		}
		// Seal against the saved transcript, not resp.Text. The registry copy
		// can grow a workspace-file suffix after history is already stored.
		a.mcpSealConversation(path, requestID, a.mcpConversationMark(path))
		a.mcpFinishRun(path, requestID, status, finalText, errText, keepText)
	}()

	view.ProjectPath = path
	view.Status = mcpStatusRunning
	view.RequestID = requestID
	if !wait {
		return view, nil
	}
	if parent == nil {
		<-done
	} else {
		select {
		case <-done:
		case <-parent.Done():
			return view, nil
		}
	}
	if finished, ok := a.mcpViewFromRegistry(path); ok && finished.RequestID == requestID {
		return finished, nil
	}
	return view, nil
}

// mcpRunStillWaiting is an MCP turn the client can still stop. A
// needs_confirmation finish has already dropped its cancel func, but the
// local card can still resume that prompt.
func mcpRunStillWaiting(status string) bool {
	return status == mcpStatusRunning || status == mcpStatusNeedsConfirmation
}

func (a *App) mcpReplaceRun(rec *mcpRunRecord) {
	if a == nil || rec == nil {
		return
	}
	path := normalizeProjectSessionPath(rec.ProjectPath)
	rec.ProjectPath = path
	var cancel context.CancelFunc
	dismiss := false
	a.mcpRunsMu.Lock()
	if a.mcpRuns == nil {
		a.mcpRuns = map[string]*mcpRunRecord{}
	}
	if prev := a.mcpRuns[path]; prev != nil && mcpRunStillWaiting(prev.Status) && prev.RequestID != rec.RequestID {
		// The card belongs to the turn we are replacing. Clear it only after
		// the registry lock is released, and only from a handler that already
		// exists — building hub infra here can load the user config.
		dismiss = true
		prev.Status = mcpStatusCancelled
		cancel = prev.cancel
		prev.cancel = nil
	}
	a.mcpRuns[path] = rec
	a.mcpRunsMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if dismiss {
		a.mcpDismissPendingConfirmation(path)
	}
}

func (a *App) mcpStoreRun(rec *mcpRunRecord) {
	a.mcpReplaceRun(rec)
}

// mcpRunOutcome is the status a client polls. A confirmation card or a
// recovery review ends this call before the work exists, so it must not
// look like a finished answer. Cancellation still wins.
func mcpRunOutcome(runErr error, cancelled bool, resp *IMAgentResponse) (status, text, errText string, keepText bool) {
	status = mcpStatusCompleted
	keepText = true
	if cancelled {
		status = mcpStatusCancelled
	} else if runErr != nil {
		status = mcpStatusFailed
		errText = runErr.Error()
	}
	if resp != nil {
		text = strings.TrimSpace(resp.Text)
		if respErr := strings.TrimSpace(resp.Error); respErr != "" && status != mcpStatusCancelled {
			status = mcpStatusFailed
			errText = respErr
		}
		if status == mcpStatusCompleted && mcpResponseWaitsOnUser(resp) {
			status = mcpStatusNeedsConfirmation
			if text == "" && resp.Confirmation != nil {
				text = strings.TrimSpace(resp.Confirmation.Summary)
			}
		}
	}
	if text != "" {
		keepText = false
	}
	return status, text, errText, keepText
}

func mcpResponseWaitsOnUser(resp *IMAgentResponse) bool {
	if resp == nil {
		return false
	}
	if resp.Confirmation != nil || resp.CodingRuntimeRecovery != nil {
		return true
	}
	return strings.TrimSpace(resp.AskUserQuestion) != ""
}

// mcpReadyIMHandler is the desktop handler when one is already running.
// Callers that only drop a card must not construct it: ensureHubClient
// loads config and can start the hub client.
func (a *App) mcpReadyIMHandler() *IMMessageHandler {
	if a == nil {
		return nil
	}
	if a.imHandler != nil {
		return a.imHandler
	}
	if hub := a.hubClient(); hub != nil {
		return hub.currentIMHandler()
	}
	return nil
}

func (a *App) mcpDismissPendingConfirmation(path string) {
	if a == nil {
		return
	}
	owner := projectSessionOwnerID(path)
	if owner == "" {
		return
	}
	handler := a.mcpReadyIMHandler()
	if handler == nil || handler.confirmationStore == nil {
		return
	}
	handler.confirmationStore.clear(owner)
}

func (a *App) mcpCancelRun(path string) {
	if a == nil {
		return
	}
	path = normalizeProjectSessionPath(path)
	a.mcpRunsMu.Lock()
	rec := a.mcpRuns[path]
	var cancel context.CancelFunc
	if rec != nil && mcpRunStillWaiting(rec.Status) {
		rec.Status = mcpStatusCancelled
		cancel = rec.cancel
		rec.cancel = nil
	}
	a.mcpRunsMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) mcpFinishRun(path, requestID, status, text, errText string, keepText bool) {
	if a == nil {
		return
	}
	path = normalizeProjectSessionPath(path)
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	if rec == nil || rec.RequestID != requestID || rec.Status != mcpStatusRunning {
		return
	}
	rec.Status = status
	rec.Error = errText
	if !keepText {
		rec.Text = text
	}
	rec.cancel = nil
}

func (a *App) mcpAppendRunText(path, requestID, delta string) {
	if a == nil || delta == "" {
		return
	}
	path = normalizeProjectSessionPath(path)
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	if rec == nil || rec.RequestID != requestID || rec.Status != mcpStatusRunning {
		return
	}
	rec.Text += delta
}

func (a *App) mcpViewFromRegistry(path string) (mcpTaskView, bool) {
	if a == nil {
		return mcpTaskView{}, false
	}
	path = normalizeProjectSessionPath(path)
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	if rec == nil {
		return mcpTaskView{}, false
	}
	return rec.view(), true
}

func (a *App) mcpTaskViewFromStore(path string) mcpTaskView {
	path = normalizeProjectSessionPath(path)
	snap := a.remoteControlTaskSnapshot(path)
	items := a.LoadProjectConversationHistory(path)
	text := mcpLastAssistantText(items)
	name := strings.TrimSpace(snap.Name)
	if name == path {
		name = ""
	}
	return mcpTaskView{
		ProjectPath:  path,
		Name:         name,
		Mode:         mcpModeFromTags(snap.Tags),
		Status:       mcpDisplayStatus("", mcpWorkflowStatus(snap.ActiveWorkflow), text),
		Text:         text,
		historyCount: mcpAssistantReplyCount(items),
	}
}

func (a *App) mcpRememberPin(projectPath, providerName, model string) {
	projectPath = normalizeProjectSessionPath(projectPath)
	providerName = strings.TrimSpace(providerName)
	model = strings.TrimSpace(model)
	if a == nil || projectPath == "" || providerName == "" || model == "" {
		return
	}
	userID := desktopAIAssistantUserIDForProjectPath(projectPath)
	if userID == "" || userID == desktopUserID {
		return
	}
	a.mcpPinsMu.Lock()
	defer a.mcpPinsMu.Unlock()
	if a.mcpPins == nil {
		a.mcpPins = map[string]mcpModelPin{}
	}
	a.mcpPins[userID] = mcpModelPin{ProviderName: providerName, Model: model}
}

func (a *App) mcpPinnedLLMConfig(userID string) (corelib.MaclawLLMConfig, bool) {
	userID = strings.TrimSpace(userID)
	if a == nil || userID == "" || userID == desktopUserID {
		return corelib.MaclawLLMConfig{}, false
	}
	a.mcpPinsMu.Lock()
	pin, ok := a.mcpPins[userID]
	a.mcpPinsMu.Unlock()
	if !ok || strings.TrimSpace(pin.ProviderName) == "" || strings.TrimSpace(pin.Model) == "" {
		return corelib.MaclawLLMConfig{}, false
	}
	cfg, err := a.MaterializeProviderByName(pin.ProviderName)
	if err != nil {
		log.Printf("[mcp-host] pin materialize failed user=%q provider=%q: %v", userID, pin.ProviderName, err)
		return corelib.MaclawLLMConfig{}, false
	}
	cfg.Model = pin.Model
	if strings.TrimSpace(cfg.Model) == "" {
		return corelib.MaclawLLMConfig{}, false
	}
	return cfg, true
}

// mcpPreferTaskView keeps an in-flight run, and a finished run whose saved
// transcript is still the sealed one. Registry text is not compared: a
// workspace-file suffix or visible-text cleanup must not look like a later
// GUI reply, and must not hide a failure.
func mcpPreferTaskView(reg, store mcpTaskView) mcpTaskView {
	if reg.Status == mcpStatusRunning {
		return reg
	}
	if mcpConversationMoved(reg, store) {
		return mcpFillTaskView(store, reg)
	}
	return mcpFillTaskView(reg, store)
}

func mcpConversationMoved(reg, store mcpTaskView) bool {
	if store.historyCount > reg.historyCount {
		return true
	}
	if store.historyCount != reg.historyCount {
		return false
	}
	return strings.TrimSpace(store.Text) != strings.TrimSpace(reg.historySeal)
}

func mcpFillTaskView(primary, fallback mcpTaskView) mcpTaskView {
	if strings.TrimSpace(primary.ProjectPath) == "" {
		primary.ProjectPath = fallback.ProjectPath
	}
	if strings.TrimSpace(primary.Name) == "" {
		primary.Name = fallback.Name
	}
	if strings.TrimSpace(primary.Mode) == "" {
		primary.Mode = fallback.Mode
	}
	if strings.TrimSpace(primary.Provider) == "" {
		primary.Provider = fallback.Provider
	}
	if strings.TrimSpace(primary.Model) == "" {
		primary.Model = fallback.Model
	}
	if strings.TrimSpace(primary.RequestID) == "" {
		primary.RequestID = fallback.RequestID
	}
	return primary
}

type mcpConversationMark struct {
	count int
	text  string
}

func (a *App) mcpConversationMark(path string) mcpConversationMark {
	if a == nil {
		return mcpConversationMark{}
	}
	items := a.LoadProjectConversationHistory(path)
	return mcpConversationMark{count: mcpAssistantReplyCount(items), text: mcpLastAssistantText(items)}
}

func (a *App) mcpSealConversation(path, requestID string, mark mcpConversationMark) {
	if a == nil || strings.TrimSpace(requestID) == "" {
		return
	}
	path = normalizeProjectSessionPath(path)
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	if rec == nil || rec.RequestID != requestID {
		return
	}
	rec.historyCount = mark.count
	rec.historySeal = mark.text
}

func mcpAssistantReplyCount(items []ProjectConversationHistoryItem) int {
	n := 0
	for _, item := range items {
		if item.hostContinuation || item.planningEcho {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(item.Role), "assistant") {
			continue
		}
		if strings.TrimSpace(item.Content) == "" {
			continue
		}
		n++
	}
	return n
}

func mcpNormalizeTaskMode(mode, workingDir string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "":
		if strings.TrimSpace(workingDir) != "" {
			return taskCodingDevTag, nil
		}
		return "chat", nil
	case "chat", "conversation":
		return "chat", nil
	case "coding_dev", "coding", "code", "programming":
		return taskCodingDevTag, nil
	default:
		return "", errors.New("mode 只能是 chat 或 coding_dev")
	}
}

func mcpDisplayStatus(registryStatus, workflowStatus, lastAssistant string) string {
	switch registryStatus {
	case mcpStatusRunning, mcpStatusCancelled, mcpStatusFailed, mcpStatusCompleted, mcpStatusNeedsConfirmation:
		return registryStatus
	}
	switch strings.ToLower(strings.TrimSpace(workflowStatus)) {
	case "running", "queued", "paused":
		return mcpStatusRunning
	case "failed":
		return mcpStatusFailed
	case "completed", "done":
		return mcpStatusCompleted
	}
	if strings.TrimSpace(lastAssistant) != "" {
		return mcpStatusCompleted
	}
	return mcpStatusIdle
}

func mcpWorkflowStatus(state *ProjectWorkflowState) string {
	if state == nil {
		return ""
	}
	return state.Status
}

func mcpModeFromTags(tags []string) string {
	for _, tag := range tags {
		if strings.TrimSpace(tag) == taskCodingDevTag {
			return taskCodingDevTag
		}
	}
	return "chat"
}

func mcpLastAssistantText(items []ProjectConversationHistoryItem) string {
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if item.hostContinuation || item.planningEcho {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(item.Role), "assistant") {
			continue
		}
		if text := strings.TrimSpace(item.Content); text != "" {
			return text
		}
	}
	return ""
}

func (a *App) mirrorMCPUser(text, requestID, sessionKey, projectPath, name, mode string) {
	if a == nil || strings.TrimSpace(requestID) == "" {
		return
	}
	uiSession := acpMirrorUISessionKey(projectPath, sessionKey)
	payload, err := json.Marshal(map[string]any{
		"source":       "mcp-host",
		"role":         "user",
		"text":         text,
		"name":         name,
		"mode":         mode,
		"request_id":   requestID,
		"session_key":  uiSession,
		"project_path": projectPath,
	})
	if err != nil {
		return
	}
	a.emitEvent("mcp-host-message", string(payload))
	display := mcpDisplayPrefix(projectPath) + text
	a.emitAIAssistantStreamEvent("ai-assistant-foreground-round-started", AIAssistantStreamEvent{
		RequestID:   requestID,
		Text:        text,
		SessionKey:  uiSession,
		DisplayText: display,
	})
	a.appendHostRoundToUIState(display, requestID, uiSession, "mcp-host", "mcp")
}

func (a *App) mirrorMCPToken(requestID, sessionKey, delta string) {
	if a == nil || delta == "" {
		return
	}
	a.emitAIAssistantStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		RequestID:  requestID,
		Text:       delta,
		SessionKey: acpMirrorUISessionKey("", sessionKey),
	})
}

func (a *App) mirrorMCPProgress(requestID, sessionKey, text string) {
	if a == nil || strings.TrimSpace(text) == "" {
		return
	}
	a.emitAIAssistantStreamEvent("ai-assistant-progress", AIAssistantStreamEvent{
		RequestID:  requestID,
		Text:       text,
		SessionKey: acpMirrorUISessionKey("", sessionKey),
	})
}

func (a *App) mirrorMCPFinal(requestID, sessionKey string, resp *IMAgentResponse) {
	if a == nil || resp == nil {
		return
	}
	uiSession := acpMirrorUISessionKey("", sessionKey)
	resp.RequestID = requestID
	resp.SessionKey = uiSession
	_ = a.emitAIAssistantResponse(requestID, resp)
	finalText := strings.TrimSpace(resp.Text)
	if finalText == "" {
		finalText = strings.TrimSpace(resp.Error)
	}
	if finalText != "" {
		a.patchACPAssistantUIState(requestID, finalText)
	}
}

func mcpDisplayPrefix(projectPath string) string {
	base := strings.TrimSpace(filepath.Base(projectPath))
	if projectPath == "" || base == "" || base == "." || base == string(filepath.Separator) {
		return "〔MCP〕"
	}
	return "〔MCP · " + base + "〕"
}
