package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"path/filepath"
	"sort"
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
	mcpConfirmKindPlan         = "plan"
	mcpConfirmKindPlanReview   = "plan_review"
	mcpConfirmKindPlanDialogue = "plan_dialogue"
	mcpPlanConfirmHint         = "编程计划待确认，确认前不会改文件。请用 send_command 发送 /plan approve（开始实施）、/plan skip（直接执行）或 /plan reject（拒绝）。"
	mcpPlanReviewHint          = "编程计划还在评审，确认前不会改文件。评审结束后再 get_task，然后用 send_command 发送 /plan approve。"
	mcpPlanDialogueHint        = "规划还在等回复，确认前不会改文件。用 send_command 把要补充或修改的话发回这个任务。"
)

// mcpTaskView is the JSON text returned by create, get, stop, and list.
type mcpTaskView struct {
	ProjectPath      string   `json:"project_path"`
	Name             string   `json:"name,omitempty"`
	Mode             string   `json:"mode,omitempty"`
	Status           string   `json:"status"`
	Text             string   `json:"text,omitempty"`
	Error            string   `json:"error,omitempty"`
	Provider         string   `json:"provider,omitempty"`
	Model            string   `json:"model,omitempty"`
	RequestID        string   `json:"request_id,omitempty"`
	ConfirmationKind string   `json:"confirmation_kind,omitempty"`
	Commands         []string `json:"commands,omitempty"`
	// workspace is the directory tools are editing. Scope approvals name that
	// directory, which is not always the task record path. It stays off the wire.
	workspace string `json:"-"`
	// historyCount and historySeal identify the conversation when this run's
	// outcome was sealed. A later assistant reply has a higher count, or the
	// same count with different text. Registry text is formatted separately
	// from history, so it is not the identity of the turn.
	historyCount int    `json:"-"`
	historySeal  string `json:"-"`
}

type mcpRunRecord struct {
	ProjectPath      string
	Name             string
	Mode             string
	Status           string
	Text             string
	Error            string
	Provider         string
	Model            string
	RequestID        string
	ConfirmationKind string
	Commands         []string
	Workspace        string
	historyCount     int
	historySeal      string
	cancel           context.CancelFunc
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
		ProjectPath:      rec.ProjectPath,
		Name:             rec.Name,
		Mode:             rec.Mode,
		Status:           rec.Status,
		Text:             rec.Text,
		Error:            rec.Error,
		Provider:         rec.Provider,
		Model:            rec.Model,
		RequestID:        rec.RequestID,
		ConfirmationKind: rec.ConfirmationKind,
		Commands:         append([]string(nil), rec.Commands...),
		workspace:        rec.Workspace,
		historyCount:     rec.historyCount,
		historySeal:      rec.historySeal,
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
	return a.mcpStartPrompt(ctx, view, prompt, args.Wait, true)
}

func (a *App) mcpSendCommand(ctx context.Context, projectPath, command string, wait bool) (mcpTaskView, error) {
	if a == nil {
		return mcpTaskView{}, errors.New("没有找到这个任务")
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return mcpTaskView{}, errors.New("命令不能为空")
	}
	registryPath := normalizeProjectSessionPath(projectPath)
	reg, inRegistry := a.mcpViewFromRegistry(registryPath)
	if view, handled, err := a.mcpOperateLiveGate(projectPath, reg, inRegistry, command); handled {
		return view, err
	}
	if inRegistry && reg.Status == mcpStatusRunning {
		return mcpTaskView{}, errors.New("任务还在运行，先停止或等这轮结束再发命令")
	}
	path, err := a.remoteControlKnownTask(projectPath)
	if err != nil && !inRegistry {
		return mcpTaskView{}, err
	}
	if path == "" {
		path = registryPath
	}
	view := reg
	if !inRegistry {
		view = a.mcpTaskViewFromStore(path)
	} else if strings.TrimSpace(view.Mode) == "" || strings.TrimSpace(view.Name) == "" {
		stored := a.mcpTaskViewFromStore(path)
		if strings.TrimSpace(view.Mode) == "" {
			view.Mode = stored.Mode
		}
		if strings.TrimSpace(view.Name) == "" {
			view.Name = stored.Name
		}
	}
	view.ProjectPath = path
	if strings.TrimSpace(view.Mode) == "" {
		view.Mode = "chat"
	}
	if taskID, digest, confirmed, ok := mcpParseRecoveryCommand(command); ok {
		return a.mcpConfirmRecovery(ctx, view, command, taskID, digest, confirmed)
	}
	return a.mcpStartPrompt(ctx, view, command, wait, false)
}

func (a *App) mcpGetTask(ctx context.Context, projectPath string) (mcpTaskView, error) {
	path := normalizeProjectSessionPath(projectPath)
	if a == nil || path == "" {
		return mcpTaskView{}, errors.New("没有找到这个任务")
	}
	if view, ok := a.mcpViewFromRegistry(path); ok && view.Status == mcpStatusRunning {
		return a.mcpAnnotateRunningGates(view), nil
	}
	known, err := a.remoteControlKnownTask(path)
	reg, hasReg := a.mcpViewFromRegistry(path)
	if err != nil {
		if hasReg {
			return a.mcpApplyLiveConfirmation(reg), nil
		}
		return mcpTaskView{}, err
	}
	store := a.mcpTaskViewFromStore(known)
	if !hasReg {
		return a.mcpApplyLiveConfirmation(store), nil
	}
	return a.mcpApplyLiveConfirmation(mcpPreferTaskView(reg, store)), nil
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
			out = append(out, a.mcpAnnotateRunningGates(view))
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
			out = append(out, a.mcpApplyLiveConfirmation(mcpPreferTaskView(view, store)))
			continue
		}
		if store.Name == "" {
			store.Name = task.Name
		}
		if store.Mode == "" {
			store.Mode = mcpModeFromTags(task.Tags)
		}
		out = append(out, a.mcpApplyLiveConfirmation(store))
	}
	a.mcpRunsMu.Lock()
	extra := make([]mcpTaskView, 0)
	for path, rec := range a.mcpRuns {
		if _, ok := seen[path]; ok || rec == nil {
			continue
		}
		extra = append(extra, rec.view())
	}
	a.mcpRunsMu.Unlock()
	for _, view := range extra {
		if view.Status == mcpStatusRunning {
			view = a.mcpAnnotateRunningGates(view)
		} else {
			view = a.mcpApplyLiveConfirmation(view)
		}
		out = append(out, view)
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

func (a *App) mcpStartPrompt(parent context.Context, view mcpTaskView, prompt string, wait, startNew bool) (mcpTaskView, error) {
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
	bound := mcpBindActionCommand(prompt)

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		resp, err := a.RunAIAssistantProgrammingPrompt(runCtx, AIAssistantSendRequest{
			Text:                        bound.Text,
			ProjectPath:                 path,
			RequestID:                   requestID,
			Lang:                        a.CurrentLanguage,
			StartNewTask:                startNew || bound.StartNewTask,
			UIAction:                    bound.UIAction,
			ResumeSlotID:                bound.ResumeSlotID,
			DismissSlotID:               bound.DismissSlotID,
			ResumeSessionID:             bound.ResumeSessionID,
			DismissRecoverableSessionID: bound.DismissRecoverableSessionID,
			PromptProfile:               assistantHostMCP,
			TaskMode:                    view.Mode,
			HostRawText:                 !startNew || bound.Bound || mcpCommandSkipsWorkspaceWrap(prompt),
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
		var kind string
		var commands []string
		status, finalText, kind, commands = mcpConfirmationCallerNotice(status, finalText, resp)
		status, finalText, kind, commands = a.mcpNoteLiveConfirmation(path, status, finalText, kind, commands)
		if resp != nil {
			a.mirrorMCPFinal(requestID, userID, resp)
		}
		// Seal against the saved transcript, not resp.Text. The registry copy
		// can grow a workspace-file suffix after history is already stored.
		a.mcpSealConversation(path, requestID, a.mcpConversationMark(path))
		a.mcpFinishRun(path, requestID, status, finalText, errText, keepText, kind, commands)
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

// mcpBindActionCommand maps a GUI button command onto the same request
// fields the desktop composer sets. The handler reads those fields, not the
// raw token, for slot and session decisions.
type mcpActionBinding struct {
	Text                        string
	Bound                       bool
	UIAction                    bool
	StartNewTask                bool
	ResumeSlotID                string
	DismissSlotID               string
	ResumeSessionID             string
	DismissRecoverableSessionID string
}

func mcpBindActionCommand(command string) mcpActionBinding {
	command = strings.TrimSpace(command)
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return mcpActionBinding{Text: command}
	}
	arg := ""
	if len(fields) > 1 {
		arg = fields[1]
	}
	switch fields[0] {
	case "__confirm_execution__", "__cancel_execution__", "__record_post__", "__workflow_choice__", "__wf_review__":
		return mcpActionBinding{Text: command, Bound: true, UIAction: true}
	case "__resume_unfinished__":
		return mcpActionBinding{Text: "继续上次未完成任务", Bound: true, UIAction: true, ResumeSlotID: arg}
	case "__start_new_task__":
		return mcpActionBinding{Text: "开始一个新任务", Bound: true, UIAction: true, StartNewTask: true}
	case "__dismiss_unfinished__":
		return mcpActionBinding{Text: "忽略上次未完成任务", Bound: true, UIAction: true, StartNewTask: true, DismissSlotID: arg}
	case "__resume_session__":
		return mcpActionBinding{Text: "恢复会话", Bound: true, UIAction: true, ResumeSessionID: arg}
	case "__dismiss_recoverable_session__":
		return mcpActionBinding{Text: "忽略会话", Bound: true, UIAction: true, DismissRecoverableSessionID: arg}
	default:
		if strings.HasPrefix(fields[0], "__") {
			return mcpActionBinding{Text: command, Bound: true, UIAction: true}
		}
		return mcpActionBinding{Text: command}
	}
}

func mcpCommandSkipsWorkspaceWrap(text string) bool {
	text = normalizeImmediateCommandText(text)
	if text == "" {
		return false
	}
	if strings.HasPrefix(text, "__") || strings.HasPrefix(text, "/") {
		return true
	}
	if classifyImmediateIMCommand(text) != imCommandUnknown {
		return true
	}
	if _, _, ok := parseConfirmationActionCommand(text); ok {
		return true
	}
	if _, _, _, ok := mcpParseRecoveryCommand(text); ok {
		return true
	}
	return isSlotActionCommand(text)
}

func mcpParseRecoveryCommand(text string) (taskID, digest string, confirmed, ok bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) != 3 {
		return "", "", false, false
	}
	switch fields[0] {
	case "__coding_recovery_confirm__":
		return fields[1], fields[2], true, true
	case "__coding_recovery_decline__":
		return fields[1], fields[2], false, true
	default:
		return "", "", false, false
	}
}

func mcpCommandHead(text string) string {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func mcpParseScopeApprovalCommand(text string) (id, decision string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) != 3 || fields[0] != "__scope_approval__" {
		return "", "", false
	}
	switch fields[2] {
	case "allow_once", "allow_task", "allow_dir", "deny", "full_access":
		return fields[1], fields[2], true
	default:
		return "", "", false
	}
}

func mcpParseCriticalConfirmCommand(text string) (id string, confirmed, ok bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) < 3 || len(fields) > 4 || fields[0] != "__resolve_critical_confirm__" {
		return "", false, false
	}
	switch fields[2] {
	case "confirm":
		return fields[1], true, true
	case "reject":
		return fields[1], false, true
	default:
		return "", false, false
	}
}

func mcpScopeApprovalMatches(taskPath, workspace, approvalPath string) bool {
	approvalPath = normalizeProjectSessionPath(approvalPath)
	if approvalPath == "" {
		return false
	}
	taskPath = normalizeProjectSessionPath(taskPath)
	workspace = normalizeProjectSessionPath(workspace)
	return (taskPath != "" && strings.EqualFold(taskPath, approvalPath)) ||
		(workspace != "" && strings.EqualFold(workspace, approvalPath))
}

func (a *App) mcpTaskForGate(projectPath string, reg mcpTaskView, inRegistry bool) (mcpTaskView, error) {
	if inRegistry {
		if strings.TrimSpace(reg.ProjectPath) == "" {
			reg.ProjectPath = normalizeProjectSessionPath(projectPath)
		}
		return reg, nil
	}
	path, err := a.remoteControlKnownTask(projectPath)
	if err != nil {
		return mcpTaskView{}, err
	}
	view := a.mcpTaskViewFromStore(path)
	view.ProjectPath = path
	return view, nil
}

func (a *App) mcpOperateLiveGate(projectPath string, reg mcpTaskView, inRegistry bool, command string) (mcpTaskView, bool, error) {
	switch mcpCommandHead(command) {
	case "__scope_approval__":
		view, err := a.mcpResolveScopeCommand(projectPath, reg, inRegistry, command)
		return view, true, err
	case "__resolve_critical_confirm__":
		view, err := a.mcpResolveCriticalCommand(projectPath, reg, inRegistry, command)
		return view, true, err
	default:
		return mcpTaskView{}, false, nil
	}
}

func (a *App) mcpResolveScopeCommand(projectPath string, reg mcpTaskView, inRegistry bool, command string) (mcpTaskView, error) {
	id, decision, ok := mcpParseScopeApprovalCommand(command)
	if !ok {
		return mcpTaskView{}, errors.New("授权决定无效")
	}
	view, err := a.mcpTaskForGate(projectPath, reg, inRegistry)
	if err != nil {
		return mcpTaskView{}, err
	}
	var pending ScopeApprovalView
	found := false
	for _, item := range new(App).ListPendingScopeApprovals() {
		if item.ID == id {
			pending = item
			found = true
			break
		}
	}
	if !found {
		return mcpTaskView{}, errors.New("确认已过期或已处理")
	}
	if !mcpScopeApprovalMatches(view.ProjectPath, view.workspace, pending.ProjectPath) {
		return mcpTaskView{}, errors.New("这条授权确认不属于这个任务")
	}
	if !mcpScopeDecisionAllowed(pending.Kind, decision) {
		return mcpTaskView{}, errors.New("这个确认没有这个决定")
	}
	ResolveScopeApproval(id, decision)
	view.Text = strings.TrimSpace(view.Text + "\n\n已提交授权决定。")
	if view.Status == mcpStatusRunning {
		return a.mcpAnnotateRunningGates(view), nil
	}
	return view, nil
}

func (a *App) mcpResolveCriticalCommand(projectPath string, reg mcpTaskView, inRegistry bool, command string) (mcpTaskView, error) {
	id, confirmed, ok := mcpParseCriticalConfirmCommand(command)
	if !ok {
		return mcpTaskView{}, errors.New("安全确认命令无效")
	}
	view, err := a.mcpTaskForGate(projectPath, reg, inRegistry)
	if err != nil {
		return mcpTaskView{}, err
	}
	userID := desktopAIAssistantUserIDForProjectPath(view.ProjectPath)
	if !a.mcpCriticalConfirmPending(userID, id) {
		return mcpTaskView{}, errors.New("确认已过期或已处理")
	}
	handler := a.mcpReadyIMHandler()
	if handler == nil {
		return mcpTaskView{}, errors.New("确认已过期或已处理")
	}
	if err := handler.ResolveCriticalConfirm(id, confirmed); err != nil {
		return mcpTaskView{}, err
	}
	view.Text = strings.TrimSpace(view.Text + "\n\n已提交安全确认。")
	if view.Status == mcpStatusRunning {
		return a.mcpAnnotateRunningGates(view), nil
	}
	return view, nil
}

func (a *App) mcpCriticalConfirmPending(userID, id string) bool {
	for _, item := range a.mcpCriticalConfirmPrompts(userID) {
		if item.ID == id {
			return true
		}
	}
	return false
}

type mcpCriticalPrompt struct {
	ID     string
	Prompt string
}

func (a *App) mcpCriticalConfirmPrompts(userID string) []mcpCriticalPrompt {
	userID = strings.TrimSpace(userID)
	if userID == "" || userID == desktopUserID {
		return nil
	}
	handler := a.mcpReadyIMHandler()
	if handler == nil {
		return nil
	}
	var out []mcpCriticalPrompt
	handler.pendingCriticalConfirm.Range(func(key, value any) bool {
		id, _ := key.(string)
		entry, _ := value.(*pendingCriticalConfirmEntry)
		if id == "" || entry == nil || entry.Owner != userID {
			return true
		}
		prompt := strings.TrimSpace(entry.Prompt)
		if prompt == "" {
			prompt = "安全确认 " + id
		}
		out = append(out, mcpCriticalPrompt{ID: id, Prompt: prompt})
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// mcpScopeApprovalDecisions matches the buttons in ScopeApprovalDialog.
// Anything else is rejected before ResolveScopeApproval.
func mcpScopeApprovalDecisions(kind string) []string {
	switch kind {
	case commitShapeApprovalKind:
		return []string{"allow_once", "allow_task", "deny"}
	case localHighRiskApprovalKind, remoteHighRiskApprovalKind:
		return []string{"allow_once", "full_access", "deny"}
	default:
		return []string{"allow_dir", "full_access", "deny"}
	}
}

func mcpScopeDecisionAllowed(kind, decision string) bool {
	for _, item := range mcpScopeApprovalDecisions(kind) {
		if item == decision {
			return true
		}
	}
	return false
}

func (a *App) mcpAnnotateRunningGates(view mcpTaskView) mcpTaskView {
	if view.Status != mcpStatusRunning {
		return view
	}
	var commands []string
	extra := ""
	kind := ""
	for _, item := range new(App).ListPendingScopeApprovals() {
		if !mcpScopeApprovalMatches(view.ProjectPath, view.workspace, item.ProjectPath) {
			continue
		}
		line := strings.TrimSpace(item.Message)
		if line == "" {
			line = strings.TrimSpace(strings.TrimSpace(item.Tool) + " " + strings.TrimSpace(item.Path))
		}
		if line == "" {
			line = "授权确认 " + item.ID
		}
		if line != "" && !strings.Contains(view.Text, line) && !strings.Contains(extra, line) {
			if extra != "" {
				extra += "\n\n"
			}
			extra += line
		}
		for _, decision := range mcpScopeApprovalDecisions(item.Kind) {
			commands = mcpAppendCommand(commands, "__scope_approval__ "+item.ID+" "+decision)
		}
		if kind == "" {
			kind = "scope"
		}
	}
	for _, item := range a.mcpCriticalConfirmPrompts(desktopAIAssistantUserIDForProjectPath(view.ProjectPath)) {
		if item.Prompt != "" && !strings.Contains(view.Text, item.Prompt) && !strings.Contains(extra, item.Prompt) {
			if extra != "" {
				extra += "\n\n"
			}
			extra += item.Prompt
		}
		commands = mcpAppendCommand(commands, "__resolve_critical_confirm__ "+item.ID+" confirm")
		commands = mcpAppendCommand(commands, "__resolve_critical_confirm__ "+item.ID+" reject")
		if kind == "" {
			kind = "critical"
		}
	}
	if kind == "" {
		return view
	}
	view.ConfirmationKind = kind
	view.Commands = commands
	if extra != "" {
		view.Text = strings.TrimSpace(view.Text + "\n\n" + extra)
	}
	if hint := mcpConfirmationHint(kind); hint != "" && !strings.Contains(view.Text, "send_command") {
		view.Text = strings.TrimSpace(view.Text + "\n\n" + hint)
	}
	return view
}

func mcpPlanDecisionCommands() []string {
	return []string{"/plan approve", "/plan skip", "/plan reject"}
}

func mcpRecoveryCommands(review *CodingRuntimeRecoveryReview) []string {
	if review == nil {
		return nil
	}
	taskID := strings.TrimSpace(review.TaskID)
	digest := strings.TrimSpace(review.ReviewDigest)
	if taskID == "" || digest == "" || strings.ContainsAny(taskID, " \t") || strings.ContainsAny(digest, " \t") {
		return nil
	}
	return []string{
		"__coding_recovery_confirm__ " + taskID + " " + digest,
		"__coding_recovery_decline__ " + taskID + " " + digest,
	}
}

func mcpCommandOperable(command string) bool {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return false
	}
	// Trace is a desktop viewer. Supplement-focus only moves the caret.
	// Neither one decides the waiting turn.
	if fields[0] == "__view_trace__" {
		return false
	}
	return !(fields[0] == "__wf_review__" && len(fields) > 1 && fields[1] == "supplement_focus")
}

func mcpAppendCommand(dst []string, command string) []string {
	command = strings.TrimSpace(command)
	if command == "" || !mcpCommandOperable(command) {
		return dst
	}
	for _, have := range dst {
		if have == command {
			return dst
		}
	}
	return append(dst, command)
}

func mcpAppendActions(dst []string, actions []IMResponseAction) []string {
	for _, action := range actions {
		dst = mcpAppendCommand(dst, action.Command)
	}
	return dst
}

func mcpPlanConfirmation(resp *IMAgentResponse) bool {
	if resp == nil {
		return false
	}
	for _, action := range resp.Actions {
		switch strings.ToLower(strings.TrimSpace(action.Command)) {
		case "/plan approve", "/plan skip", "/plan reject":
			return true
		}
	}
	return false
}

// mcpConfirmationOffer is every decision the GUI would put on a button.
// commands are sent back unchanged through send_command.
func mcpConfirmationOffer(resp *IMAgentResponse) (kind string, commands []string) {
	if resp == nil {
		return "", nil
	}
	commands = mcpAppendActions(nil, resp.Actions)
	if resp.UnfinishedTask != nil {
		commands = mcpAppendActions(commands, resp.UnfinishedTask.Actions)
	}
	if resp.UnfinishedSlot != nil {
		commands = mcpAppendActions(commands, resp.UnfinishedSlot.Actions)
	}
	if resp.RecoverableSession != nil {
		commands = mcpAppendActions(commands, resp.RecoverableSession.Actions)
	}
	if resp.Confirmation != nil {
		if id := strings.TrimSpace(resp.Confirmation.ID); id != "" {
			commands = mcpAppendCommand(commands, buildConfirmationActionCommand(confirmationActionConfirm, id))
			commands = mcpAppendCommand(commands, buildConfirmationActionCommand(confirmationActionCancel, id))
		}
	}
	for _, command := range mcpRecoveryCommands(resp.CodingRuntimeRecovery) {
		commands = mcpAppendCommand(commands, command)
	}
	if mcpPlanConfirmation(resp) {
		for _, command := range mcpPlanDecisionCommands() {
			commands = mcpAppendCommand(commands, command)
		}
	}
	switch {
	case mcpPlanConfirmation(resp):
		kind = mcpConfirmKindPlan
	case resp.CodingRuntimeRecovery != nil:
		kind = "recovery"
	case resp.Confirmation != nil:
		kind = "confirm"
	case strings.TrimSpace(resp.AskUserQuestion) != "" || strings.TrimSpace(resp.AskUserSecretName) != "":
		kind = "ask"
	case resp.UnfinishedSlot != nil || resp.UnfinishedTask != nil:
		kind = "unfinished"
	case resp.RecoverableSession != nil:
		kind = "session"
	case resp.DesktopUserControl || strings.TrimSpace(resp.DesktopAttentionReason) != "":
		kind = "desktop"
	case len(commands) > 0:
		kind = "action"
	}
	return kind, commands
}

func mcpConfirmationHint(kind string) string {
	switch kind {
	case mcpConfirmKindPlan:
		return mcpPlanConfirmHint
	case mcpConfirmKindPlanReview:
		return mcpPlanReviewHint
	case mcpConfirmKindPlanDialogue:
		return mcpPlanDialogueHint
	case "recovery":
		return "编程恢复待确认，确认前不会沿用上一次执行。用 send_command 原样发送 commands 中的一条。"
	case "ask":
		return "需要你的回答。用 send_command 把回答原文发回这个任务。"
	case "desktop":
		return "需要人操作桌面。处理完后用 send_command 把下一步发回这个任务。"
	case "scope":
		return "任务卡在授权确认上，还在运行。用 send_command 原样发送 commands 中的一条，当前这轮会继续。"
	case "critical":
		return "有一条安全确认待处理，任务还在运行。用 send_command 原样发送 commands 中的一条。"
	case "confirm", "unfinished", "session", "action":
		return "这轮需要确认，结果还没出来。用 send_command 原样发送 commands 中的一条。"
	default:
		return ""
	}
}

func mcpConfirmationCallerNotice(status, text string, resp *IMAgentResponse) (string, string, string, []string) {
	if status != mcpStatusCompleted && status != mcpStatusNeedsConfirmation {
		return status, text, "", nil
	}
	kind, commands := mcpConfirmationOffer(resp)
	if kind == "" {
		return status, text, "", nil
	}
	status = mcpStatusNeedsConfirmation
	if resp != nil {
		if ask := strings.TrimSpace(resp.AskUserQuestion); kind == "ask" && ask != "" && !strings.Contains(text, ask) {
			text = strings.TrimSpace(text + "\n\n" + ask)
		}
		if name := strings.TrimSpace(resp.AskUserSecretName); name != "" && !strings.Contains(text, name) {
			text = strings.TrimSpace(text + "\n\n需要填写密钥 " + name + "。用 send_command 发送密钥的值。")
		}
		if kind == "desktop" {
			if reason := strings.TrimSpace(resp.DesktopAttentionReason); reason != "" && !strings.Contains(text, reason) {
				text = strings.TrimSpace(text + "\n\n" + reason)
			}
			if handoff := strings.TrimSpace(resp.DesktopHandoffURL); handoff != "" && !strings.Contains(text, handoff) {
				text = strings.TrimSpace(text + "\n" + handoff)
			}
		}
	}
	if hint := mcpConfirmationHint(kind); hint != "" && !strings.Contains(text, "send_command") {
		text = strings.TrimSpace(text + "\n\n" + hint)
	}
	return status, text, kind, commands
}

func (a *App) mcpConfirmRecovery(ctx context.Context, view mcpTaskView, command, taskID, digest string, confirmed bool) (mcpTaskView, error) {
	review, err := a.ConfirmCodingRuntimeRecovery(ctx, taskID, digest, confirmed)
	if err != nil {
		return mcpTaskView{}, err
	}
	text := "已拒绝恢复。上一次执行不会沿用。"
	if confirmed {
		text = "已确认恢复。上一次执行不会沿用。用 send_command 发送「继续」开始新的一轮。"
	}
	if review != nil && strings.TrimSpace(review.Summary) != "" {
		text = strings.TrimSpace(review.Summary) + "\n\n" + text
	}
	view.Status = mcpStatusCompleted
	view.Text = text
	view.ConfirmationKind = ""
	view.Commands = nil
	userID := desktopAIAssistantUserIDForProjectPath(view.ProjectPath)
	requestID := "mcp-" + randomHexID(8)
	view.RequestID = requestID
	a.mirrorMCPUser(command, requestID, userID, view.ProjectPath, view.Name, view.Mode)
	a.mcpStoreRun(&mcpRunRecord{
		ProjectPath:  view.ProjectPath,
		Name:         view.Name,
		Mode:         view.Mode,
		Status:       view.Status,
		Text:         view.Text,
		Provider:     view.Provider,
		Model:        view.Model,
		RequestID:    requestID,
		historyCount: view.historyCount,
		historySeal:  view.historySeal,
	})
	return view, nil
}

func (a *App) mcpNoteLiveConfirmation(path, status, text, kind string, commands []string) (string, string, string, []string) {
	view := a.mcpApplyLiveConfirmation(mcpTaskView{
		ProjectPath:      path,
		Status:           status,
		Text:             text,
		ConfirmationKind: kind,
		Commands:         commands,
	})
	return view.Status, view.Text, view.ConfirmationKind, view.Commands
}

func (a *App) mcpApplyLiveConfirmation(view mcpTaskView) mcpTaskView {
	switch view.Status {
	case mcpStatusRunning, mcpStatusFailed, mcpStatusCancelled:
		return view
	}
	handler := a.mcpReadyIMHandler()
	if handler == nil {
		return view
	}
	userID := desktopAIAssistantUserIDForProjectPath(view.ProjectPath)
	if userID == "" || userID == desktopUserID {
		return view
	}
	kind := view.ConfirmationKind
	commands := append([]string(nil), view.Commands...)
	extra := ""
	if handler.codingPlanReviewIsLive(userID) && kind == "" {
		kind = mcpConfirmKindPlanReview
		extra = mcpPlanReviewHint
	} else if mem := handler.getStickyCodingWorkbenchMemory(userID); handler.codingPlanHasPendingApproval(userID, mem) {
		if kind == "" || kind == mcpConfirmKindPlan {
			kind = mcpConfirmKindPlan
		}
		for _, command := range mcpPlanDecisionCommands() {
			commands = mcpAppendCommand(commands, command)
		}
		if md := strings.TrimSpace(mem.PendingPlanMarkdown); md != "" && !strings.Contains(view.Text, md) {
			extra = md
		}
		if !strings.Contains(view.Text, "send_command") {
			if extra != "" {
				extra += "\n\n"
			}
			extra += mcpPlanConfirmHint
		}
	} else if mem.PlanDialogue && kind == "" {
		kind = mcpConfirmKindPlanDialogue
		extra = mcpPlanDialogueHint
	}
	if handler.confirmationStore != nil {
		if item := handler.confirmationStore.get(userID); item != nil && strings.TrimSpace(item.ID) != "" {
			commands = mcpAppendCommand(commands, buildConfirmationActionCommand(confirmationActionConfirm, item.ID))
			commands = mcpAppendCommand(commands, buildConfirmationActionCommand(confirmationActionCancel, item.ID))
			if kind == "" {
				kind = "confirm"
			}
		}
	}
	if raw, ok := handler.pendingAskUser.Load(userID); ok {
		if state, _ := raw.(*pendingAskUserState); state != nil && strings.TrimSpace(state.Question) != "" {
			if kind == "" {
				kind = "ask"
			}
			if !strings.Contains(view.Text, state.Question) {
				if extra != "" {
					extra += "\n\n"
				}
				extra += state.Question
			}
			if len(state.Options) > 0 && !strings.Contains(view.Text, state.Options[0]) {
				extra = strings.TrimSpace(extra + "\n" + strings.Join(state.Options, "\n"))
			}
		}
	}
	if kind == "" {
		return view
	}
	view.Status = mcpStatusNeedsConfirmation
	view.ConfirmationKind = kind
	view.Commands = commands
	if extra != "" {
		view.Text = strings.TrimSpace(view.Text + "\n\n" + extra)
	}
	if hint := mcpConfirmationHint(kind); hint != "" && !strings.Contains(view.Text, "send_command") {
		view.Text = strings.TrimSpace(view.Text + "\n\n" + hint)
	}
	return view
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
	kind, commands := mcpConfirmationOffer(resp)
	return kind != "" || len(commands) > 0
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

func (a *App) mcpFinishRun(path, requestID, status, text, errText string, keepText bool, kind string, commands []string) {
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
	rec.ConfirmationKind = kind
	rec.Commands = append([]string(nil), commands...)
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

func (a *App) mcpSetRunWorkspace(path, requestID, workspace string) {
	if a == nil {
		return
	}
	path = normalizeProjectSessionPath(path)
	workspace = normalizeProjectSessionPath(workspace)
	if path == "" || workspace == "" {
		return
	}
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	if rec == nil || rec.RequestID != requestID || rec.Status != mcpStatusRunning {
		return
	}
	rec.Workspace = workspace
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
