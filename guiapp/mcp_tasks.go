package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	// mcpRunStallWindow is how long a round may sit with no token, progress,
	// tool event, or other host-visible activity before the session watchdog
	// fails it. It is not a wall clock on the round, and it is not the
	// per-request LLM deadline (DefaultLLMTimeoutSec / MaxAgentTimeoutSec).
	// A round that keeps producing activity is never cancelled by this window.
	mcpRunStallWindow          = 30 * time.Minute
	mcpConfirmKindPlan         = "plan"
	mcpConfirmKindPlanReview   = "plan_review"
	mcpConfirmKindPlanDialogue = "plan_dialogue"
	// Task location is where the task runs. local runs on this computer
	// (including an SSH programming workspace, and a cloud workspace used
	// only as that task's directory). cloud is a cloud agent. remote_device
	// is an authorized computer reached through remote control.
	mcpLocationLocal        = "local"
	mcpLocationCloud        = "cloud"
	mcpLocationRemoteDevice = "remote_device"
	mcpPlanConfirmHint      = "编程计划待确认，确认前不会改文件。请用 send_command 发送 /plan approve（开始实施）、/plan skip（直接执行）或 /plan reject（拒绝）。"
	mcpPlanReviewHint       = "编程计划还在评审，确认前不会改文件。评审结束后再 get_task，然后用 send_command 发送 /plan approve。"
	mcpPlanDialogueHint     = "规划还在等回复，确认前不会改文件。用 send_command 把要补充或修改的话发回这个任务。"
)

var (
	errMCPTaskRunning   = errors.New("任务还在运行，先停止或等这轮结束再发命令")
	errMCPHostWaitEnded = errors.New("主机等待已结束，尚未证实为超时")
	// errMCPRunStalled is a confirmed lack of host-visible progress. It is
	// not a user stop and not an unconfirmed host-wait ending.
	errMCPRunStalled = errors.New("这一轮没有进展")
	// mcpPromptPanicDetail is the recorded failure when the prompt goroutine
	// dies. The run can no longer continue, and leaving it running would reject
	// every later command.
	mcpPromptPanicDetail = "这一轮无法继续"
	// mcpProgrammingPromptFunc replaces the desktop prompt in tests. Production
	// leaves it nil. The value is captured before the round starts. A test
	// path does not load conversation history or the assistant UI database.
	mcpProgrammingPromptFunc func(a *App, ctx context.Context, req AIAssistantSendRequest, cb AIAssistantExternalCallbacks) (*IMAgentResponse, error)
	// mcpProviderResolveFunc replaces catalog lookup for send_command in tests.
	// Production leaves it nil and uses the same resolver as create_task.
	mcpProviderResolveFunc func(provider, model string) (string, string, error)
	// mcpMaterializeModelFunc replaces provider materialization in tests.
	// Production leaves it nil and loads the configured provider.
	mcpMaterializeModelFunc func(a *App, provider, model string) (corelib.MaclawLLMConfig, bool)
	// mcpCloudCreateFunc replaces creation of a local task whose working
	// directory is a cloud workspace. Production leaves it nil and uses
	// CreateTaskUnified. It is not the cloud-agent location.
	mcpCloudCreateFunc func(a *App, opts TaskCreateOptions) (UnifiedTaskCreateResult, error)
	// mcpCloudAgentCreateFunc, mcpCloudAgentGrantFunc, and mcpCloudAgentOpFunc
	// replace the cloud-agent instance, its control grant, and cloud_* ops in
	// tests. Production leaves them nil.
	mcpCloudAgentCreateFunc func(a *App, name, kind string) (CloudAgentRow, error)
	mcpCloudAgentGrantFunc  func(a *App, instanceID string) (string, error)
	mcpCloudAgentDeleteFunc func(a *App, instanceID string) error
	mcpCloudAgentOpFunc     func(a *App, grantID, op, body string) (string, error)
	// mcpRemoteDeviceOpFunc replaces the remote-device control call in tests.
	// Production leaves it nil and uses RemoteControlOp.
	mcpRemoteDeviceOpFunc func(a *App, grantID, op, body string) (string, error)
)

// mcpTaskView is the JSON text returned by create, get, stop, and list.
type mcpTaskView struct {
	ProjectPath         string              `json:"project_path"`
	Name                string              `json:"name,omitempty"`
	Mode                string              `json:"mode,omitempty"`
	Status              string              `json:"status"`
	Text                string              `json:"text,omitempty"`
	Error               string              `json:"error,omitempty"`
	Provider            string              `json:"provider,omitempty"`
	Model               string              `json:"model,omitempty"`
	RequestID           string              `json:"request_id,omitempty"`
	ConfirmationKind    string              `json:"confirmation_kind,omitempty"`
	ConfirmationID      string              `json:"confirmation_id,omitempty"`
	Commands            []string            `json:"commands,omitempty"`
	WorkingDir          string              `json:"working_dir,omitempty"`
	Round               int                 `json:"round,omitempty"`
	StartedAt           string              `json:"started_at,omitempty"`
	FinishedAt          string              `json:"finished_at,omitempty"`
	EndReason           string              `json:"end_reason,omitempty"`
	EndDetail           string              `json:"end_detail,omitempty"`
	Location            string              `json:"location,omitempty"`
	CloudWorkspaceID    string              `json:"cloud_workspace_id,omitempty"`
	GrantID             string              `json:"grant_id,omitempty"`
	RemoteHost          string              `json:"ssh_host,omitempty"`
	RemoteUser          string              `json:"ssh_user,omitempty"`
	RemotePort          int                 `json:"ssh_port,omitempty"`
	RemoteWorkDir       string              `json:"remote_work_dir,omitempty"`
	Steps               []mcpStepView       `json:"steps,omitempty"`
	Checks              []mcpCheckView      `json:"checks,omitempty"`
	RunID               string              `json:"run_id,omitempty"`
	CurrentRunID        string              `json:"current_run_id,omitempty"`
	TaskID              string              `json:"task_id,omitempty"`
	StatusVersion       int                 `json:"status_version,omitempty"`
	PlanID              string              `json:"plan_id,omitempty"`
	PlanVersion         int                 `json:"plan_version,omitempty"`
	ProviderRequested   string              `json:"provider_requested,omitempty"`
	ModelRequested      string              `json:"model_requested,omitempty"`
	ProviderEffective   string              `json:"provider_effective,omitempty"`
	ModelEffective      string              `json:"model_effective,omitempty"`
	WorkDirRequested    string              `json:"working_dir_requested,omitempty"`
	WorkingDirEffective string              `json:"working_dir_effective,omitempty"`
	ToolRoot            string              `json:"tool_root,omitempty"`
	SessionPath         string              `json:"session_path,omitempty"`
	StatusRaw           string              `json:"status_raw,omitempty"`
	ParentTaskID        string              `json:"parent_task_id,omitempty"`
	ChildTaskID         string              `json:"child_task_id,omitempty"`
	Children            []mcpChildView      `json:"children,omitempty"`
	Stop                *mcpStopView        `json:"stop,omitempty"`
	DryRun              bool                `json:"dry_run,omitempty"`
	ResumeOK            bool                `json:"resume_ok,omitempty"`
	Conflicts           []mcpResumeConflict `json:"conflicts,omitempty"`
	DirtyNote           string              `json:"dirty_note,omitempty"`
	// Outcome is the short decision for this command. run_id is the session
	// the caller queries after the round ends.
	Outcome string `json:"outcome,omitempty"`
	// workspace is the directory tools are editing. Scope approvals name that
	// directory, which is not always the task record path. Publish copies it
	// to WorkingDir. The unexported field stays the scope-match key.
	workspace string `json:"-"`
	// historyCount and historySeal identify the conversation when this run's
	// outcome was sealed. A later assistant reply has a higher count, or the
	// same count with different text. Registry text is formatted separately
	// from history, so it is not the identity of the turn.
	historyCount int    `json:"-"`
	historySeal  string `json:"-"`
}

type mcpRunRecord struct {
	ProjectPath        string
	Name               string
	Mode               string
	Status             string
	Text               string
	Error              string
	Provider           string
	Model              string
	RequestID          string
	ConfirmationKind   string
	Commands           []string
	Workspace          string
	Round              int
	StartedAt          time.Time
	FinishedAt         time.Time
	EndReason          string
	EndDetail          string
	ConfirmationID     string
	confirmFingerprint string
	logSeq             int
	logs               []mcpLogEvent
	historyCount       int
	historySeal        string
	RemoteHost         string
	RemoteUser         string
	RemotePort         int
	RemoteWorkDir      string
	Location           string
	CloudWorkspaceID   string
	GrantID            string
	Steps              []mcpStepView
	Checks             []mcpCheckView
	RunID              string
	StatusVersion      int
	ParentTaskID       string
	ChildTaskID        string
	Children           []mcpChildView
	Stop               *mcpStopView
	PlanID             string
	PlanVersion        int
	ProviderRequested  string
	ModelRequested     string
	ProviderEffective  string
	ModelEffective     string
	WorkDirRequested   string
	SessionPath        string
	cancel             context.CancelFunc
	// progressAt is the last host-visible activity on this round: a token,
	// a progress line, a tool event, or the moment the round started.
	// A bare __heartbeat__ is not activity. The session watchdog reads
	// this stamp. It is not a per-request LLM deadline.
	progressAt time.Time
	// hostWaitEnded marks a running record this process can no longer keep,
	// such as one reloaded after restart. A live run is not cancelled because
	// time passed, and this flag is not a confirmed execution timeout.
	hostWaitEnded bool
	// stalled is set only when the session watchdog confirms that this round
	// produced no host-visible activity for mcpRunStallWindow. A user stop
	// does not set it.
	stalled bool
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
		ProjectPath:         rec.ProjectPath,
		Name:                rec.Name,
		Mode:                rec.Mode,
		Status:              rec.Status,
		Text:                rec.Text,
		Error:               rec.Error,
		Provider:            rec.Provider,
		Model:               rec.Model,
		RequestID:           rec.RequestID,
		ConfirmationKind:    rec.ConfirmationKind,
		ConfirmationID:      rec.ConfirmationID,
		Commands:            append([]string(nil), rec.Commands...),
		WorkingDir:          rec.Workspace,
		Round:               rec.Round,
		StartedAt:           mcpFormatTime(rec.StartedAt),
		FinishedAt:          mcpFormatTime(rec.FinishedAt),
		EndReason:           rec.EndReason,
		EndDetail:           rec.EndDetail,
		Location:            rec.Location,
		CloudWorkspaceID:    rec.CloudWorkspaceID,
		GrantID:             rec.GrantID,
		RemoteHost:          rec.RemoteHost,
		RemoteUser:          rec.RemoteUser,
		RemotePort:          rec.RemotePort,
		RemoteWorkDir:       rec.RemoteWorkDir,
		Steps:               mcpCloneSteps(rec.Steps),
		Checks:              mcpCloneChecks(rec.Checks),
		RunID:               rec.RunID,
		CurrentRunID:        rec.RunID,
		TaskID:              rec.ProjectPath,
		StatusVersion:       rec.StatusVersion,
		PlanID:              rec.PlanID,
		PlanVersion:         rec.PlanVersion,
		ProviderRequested:   rec.ProviderRequested,
		ModelRequested:      rec.ModelRequested,
		ProviderEffective:   rec.ProviderEffective,
		ModelEffective:      rec.ModelEffective,
		WorkDirRequested:    rec.WorkDirRequested,
		WorkingDirEffective: rec.Workspace,
		ToolRoot:            rec.Workspace,
		SessionPath:         rec.SessionPath,
		ParentTaskID:        rec.ParentTaskID,
		ChildTaskID:         rec.ChildTaskID,
		Children:            mcpCloneChildren(rec.Children),
		Stop:                mcpCloneStop(rec.Stop),
		workspace:           rec.Workspace,
		historyCount:        rec.historyCount,
		historySeal:         rec.historySeal,
	}
}

func (a *App) mcpListModels(ctx context.Context) (mcpModelCatalog, error) {
	if a == nil {
		return mcpModelCatalog{}, errors.New("MCP 任务服务还没有准备好")
	}
	data := a.GetMaclawLLMProviders()
	return mcpModelCatalogFrom(data.Providers, data.Current), nil
}

func (a *App) mcpCreateTask(ctx context.Context, args mcpCreateArgs) (view mcpTaskView, err error) {
	if a == nil {
		return mcpTaskView{}, errors.New("MCP 任务服务还没有准备好")
	}
	prompt := strings.TrimSpace(args.Prompt)
	req, claimed, err := a.mcpClaimRequest(args.ClientRequestID, mcpRequestKindCreate, mcpCreateClaimKey(args), prompt)
	if err != nil {
		return mcpTaskView{}, err
	}
	if !claimed {
		return a.mcpReplayRequest(ctx, req)
	}
	accepted := false
	defer func() {
		if !accepted {
			a.mcpAbandonRequest(req, err)
			return
		}
		a.mcpCompleteRequest(req, err)
	}()
	view, err = a.mcpCreateTaskBody(ctx, args, prompt, func(path string) {
		accepted = true
		a.mcpBindRequest(req, path)
	})
	return view, err
}

func mcpCreateClaimKey(args mcpCreateArgs) string {
	location, locErr := mcpNormalizeLocation(args)
	if locErr == nil && location == mcpLocationCloud {
		return mcpClaimIdentity(
			strings.TrimSpace(args.Name),
			mcpLocationCloud,
			strings.TrimSpace(args.Mode),
			strings.TrimSpace(args.CloudWorkspaceID),
			strings.TrimSpace(args.GrantID),
			strings.TrimSpace(args.Provider),
			strings.TrimSpace(args.Model),
		)
	}
	if locErr == nil && location == mcpLocationRemoteDevice {
		return mcpClaimIdentity(
			strings.TrimSpace(args.Name),
			mcpLocationRemoteDevice,
			strings.TrimSpace(args.GrantID),
			strings.TrimSpace(args.Mode),
			strings.TrimSpace(args.WorkingDir),
			strings.TrimSpace(args.Provider),
			strings.TrimSpace(args.Model),
		)
	}
	if locErr != nil || (strings.TrimSpace(args.Location) != "" && location != mcpLocationLocal) {
		return mcpClaimIdentity(
			strings.TrimSpace(args.Location),
			strings.TrimSpace(args.CloudWorkspaceID),
			strings.TrimSpace(args.GrantID),
			strings.TrimSpace(args.Name),
			strings.TrimSpace(args.Mode),
			strings.TrimSpace(args.WorkingDir),
			strings.TrimSpace(args.Provider),
			strings.TrimSpace(args.Model),
		)
	}
	mode, modeErr := mcpEffectiveCreateMode(args)
	if mcpHasRemoteConnection(args) || (modeErr == nil && mode == taskRemoteCodingDevTag) {
		label := mode
		if label == "" {
			label = strings.TrimSpace(args.Mode)
		}
		host := strings.TrimSpace(args.SSHHost)
		user := strings.TrimSpace(args.SSHUser)
		remoteDir := mcpRemoteWorkDir(args)
		port := strconv.Itoa(args.SSHPort)
		if remote, err := mcpRemoteTarget(args); err == nil {
			host = remote.Host
			user = remote.User
			remoteDir = remote.WorkDir
			port = strconv.Itoa(remote.Port)
		}
		// The password arms SSH and is not part of the request identity.
		// A chat or local task that also carries SSH fields uses this same
		// identity, so it cannot replay a create that never saw those fields.
		return mcpClaimIdentity(
			strings.TrimSpace(args.Name),
			label,
			host,
			user,
			port,
			remoteDir,
			strings.TrimSpace(args.Provider),
			strings.TrimSpace(args.Model),
		)
	}
	dir := strings.TrimSpace(args.WorkingDir)
	if dir != "" {
		if normalized := normalizeRecentTaskWorkingDir(dir); normalized != "" {
			dir = normalized
		}
	}
	// A cloud workspace id is the working directory, so it belongs in the
	// same identity slot as a local folder. An empty location still hashes
	// the old way when this id is absent.
	if id := strings.TrimSpace(args.CloudWorkspaceID); id != "" {
		if dir == "" {
			dir = id
		} else {
			dir = dir + "\n" + id
		}
	}
	return mcpClaimIdentity(
		strings.TrimSpace(args.Name),
		strings.TrimSpace(args.Mode),
		dir,
		strings.TrimSpace(args.Provider),
		strings.TrimSpace(args.Model),
	)
}

func mcpCommandClaimKey(projectPath, command, confirmationID string) string {
	return mcpClaimIdentity(
		strings.TrimSpace(command),
		normalizeProjectSessionPath(projectPath),
		strings.TrimSpace(confirmationID),
	)
}

func mcpRetryClaimKey(projectPath, confirmationID string, ids []string) string {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	return mcpClaimIdentity(
		normalizeProjectSessionPath(projectPath),
		strings.Join(sorted, ","),
		strings.TrimSpace(confirmationID),
	)
}

func (a *App) mcpCreateTaskBody(ctx context.Context, args mcpCreateArgs, prompt string, accept func(path string)) (mcpTaskView, error) {
	if strings.TrimSpace(args.ArgError) != "" {
		return mcpTaskView{}, errors.New(args.ArgError)
	}
	name := taskNameForCreate(args.Name, true)
	if name == "" {
		name = taskNameForCreate(recentTaskDisplayTitle(prompt), false)
	}
	if name == "" {
		return mcpTaskView{}, errors.New("任务名称或内容不能为空")
	}
	location, mode, err := mcpResolveCreateTarget(args)
	if err != nil {
		return mcpTaskView{}, err
	}
	if location == mcpLocationRemoteDevice && (strings.TrimSpace(args.Provider) != "" || strings.TrimSpace(args.Model) != "") {
		return mcpTaskView{}, errors.New("远程设备上的模型由对方电脑决定，不要传 provider 或 model")
	}
	if location == mcpLocationCloud && (strings.TrimSpace(args.Provider) != "" || strings.TrimSpace(args.Model) != "") {
		return mcpTaskView{}, errors.New("云端 agent 的模型由云端实例决定，不要传 provider 或 model")
	}
	providerName, model, err := a.mcpResolveSelection(args.Provider, args.Model)
	if err != nil {
		return mcpTaskView{}, err
	}
	switch location {
	case mcpLocationRemoteDevice:
		return a.mcpCreateRemoteDeviceTask(ctx, args, prompt, name, mode, accept)
	case mcpLocationCloud:
		return a.mcpCreateCloudAgentTask(ctx, args, prompt, name, mode, accept)
	}
	if strings.TrimSpace(args.CloudWorkspaceID) != "" {
		return a.mcpCreateCloudDirectoryTask(ctx, args, prompt, name, providerName, model, accept)
	}
	if mode == taskRemoteCodingDevTag {
		return a.mcpCreateRemoteTask(ctx, args, prompt, name, mode, providerName, model, accept)
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
	a.recordMCPAccess(ctx, "create_task", firstNonEmpty(created.Name, name), created.ProjectPath)
	if accept != nil {
		accept(created.ProjectPath)
	}
	view := mcpTaskView{
		ProjectPath: created.ProjectPath,
		Name:        firstNonEmpty(created.Name, name),
		Mode:        mode,
		Location:    mcpLocationLocal,
		Status:      mcpStatusIdle,
		Provider:    providerName,
		Model:       model,
		workspace:   workDir,
	}
	if providerName != "" {
		a.mcpRememberPin(created.ProjectPath, providerName, model)
	}
	if prompt == "" {
		mark := a.mcpConversationMark(view.ProjectPath)
		view.historyCount = mark.count
		view.historySeal = mark.text
		a.mcpStoreRun(&mcpRunRecord{
			ProjectPath:      view.ProjectPath,
			Name:             view.Name,
			Mode:             view.Mode,
			Location:         view.Location,
			CloudWorkspaceID: view.CloudWorkspaceID,
			GrantID:          view.GrantID,
			Status:           mcpStatusIdle,
			Provider:         providerName,
			Model:            model,
			Workspace:        workDir,
			historyCount:     mark.count,
			historySeal:      mark.text,
		})
		return a.mcpPublishInstalledCreate(view), nil
	}
	return a.mcpStartPrompt(ctx, view, prompt, args.Wait, true, nil)
}

// mcpCreateRemoteTask creates the same SSH programming task as the new-task
// wizard and, once the host is armed, hands the prompt to RemoteCodingSubAgent.
// The password is used only for that connection.
func (a *App) mcpCreateRemoteTask(ctx context.Context, args mcpCreateArgs, prompt, name, mode, providerName, model string, accept func(path string)) (mcpTaskView, error) {
	remote, err := mcpRemoteTarget(args)
	if err != nil {
		return mcpTaskView{}, err
	}
	created, err := a.CreateTaskUnified(TaskCreateOptions{
		Name:   name,
		Mode:   taskRemoteCodingDevTag,
		Remote: remote,
	})
	if err != nil {
		return mcpTaskView{}, err
	}
	if strings.TrimSpace(created.ProjectPath) == "" {
		return mcpTaskView{}, errors.New("没有创建任务")
	}
	view := mcpTaskView{
		ProjectPath:   created.ProjectPath,
		Name:          firstNonEmpty(name, created.ProjectPath),
		Mode:          mode,
		Location:      mcpLocationLocal,
		Status:        mcpStatusIdle,
		Provider:      providerName,
		Model:         model,
		RemoteHost:    remote.Host,
		RemoteUser:    remote.User,
		RemotePort:    remote.Port,
		RemoteWorkDir: remote.WorkDir,
		WorkingDir:    remote.WorkDir,
		workspace:     remote.WorkDir,
	}
	if warning := strings.TrimSpace(created.Warning); warning != "" {
		// The record exists, but the remote agent is not armed. Leave the
		// request id free so the caller can correct the password and retry.
		view.Status = mcpStatusFailed
		view.Error = warning
		view.EndReason = mcpStatusFailed
		view.EndDetail = warning
		return view, nil
	}
	if providerName != "" {
		a.mcpRememberPin(created.ProjectPath, providerName, model)
	}
	a.recordMCPAccess(ctx, "create_task", view.Name, created.ProjectPath)
	if accept != nil {
		accept(created.ProjectPath)
	}
	if prompt == "" {
		mark := a.mcpConversationMark(view.ProjectPath)
		view.historyCount = mark.count
		view.historySeal = mark.text
		a.mcpStoreRun(mcpRunRecordFromView(view))
		return a.mcpPublishInstalledCreate(view), nil
	}
	return a.mcpStartPrompt(ctx, view, prompt, args.Wait, true, nil)
}

// mcpCreateCloudDirectoryTask creates a task on this computer whose working
// directory is a cloud workspace. The agent stays here. location=cloud is a
// cloud agent and does not call this.
func (a *App) mcpCreateCloudDirectoryTask(ctx context.Context, args mcpCreateArgs, prompt, name, providerName, model string, accept func(path string)) (mcpTaskView, error) {
	id := strings.TrimSpace(args.CloudWorkspaceID)
	opts := TaskCreateOptions{Name: name, Mode: "cloud", CloudWorkspaceID: id}
	if providerName != "" && model != "" {
		opts.LLMProviderID = providerName
		opts.LLMModel = model
	}
	created, err := a.mcpCreateCloudWorkspace(opts)
	if err != nil {
		return mcpTaskView{}, err
	}
	if strings.TrimSpace(created.ProjectPath) == "" {
		return mcpTaskView{}, errors.New("没有创建任务")
	}
	snap := a.remoteControlTaskSnapshot(created.ProjectPath)
	workDir := strings.TrimSpace(firstNonEmpty(snap.WorkingDir, snap.ExecutionDir))
	view := mcpTaskView{
		ProjectPath:      created.ProjectPath,
		Name:             firstNonEmpty(name, created.ProjectPath),
		Mode:             "chat",
		Location:         mcpLocationLocal,
		CloudWorkspaceID: id,
		Status:           mcpStatusIdle,
		Provider:         providerName,
		Model:            model,
		WorkingDir:       workDir,
		workspace:        workDir,
	}
	if warning := strings.TrimSpace(created.Warning); warning != "" {
		view.EndDetail = warning
	}
	if providerName != "" {
		a.mcpRememberPin(created.ProjectPath, providerName, model)
	}
	a.recordMCPAccess(ctx, "create_task", view.Name, created.ProjectPath)
	if accept != nil {
		accept(created.ProjectPath)
	}
	if prompt == "" {
		mark := a.mcpConversationMark(view.ProjectPath)
		view.historyCount = mark.count
		view.historySeal = mark.text
		a.mcpStoreRun(mcpRunRecordFromView(view))
		return a.mcpPublishInstalledCreate(view), nil
	}
	return a.mcpStartPrompt(ctx, view, prompt, args.Wait, true, nil)
}

func (a *App) mcpCreateCloudWorkspace(opts TaskCreateOptions) (UnifiedTaskCreateResult, error) {
	if mcpCloudCreateFunc != nil {
		return mcpCloudCreateFunc(a, opts)
	}
	return a.CreateTaskUnified(opts)
}

// mcpCreateRemoteDeviceTask creates the task on an authorized computer.
// The prompt is delivered with that computer's send_message. A failed
// delivery still returns the created task so the caller does not create
// another one.
func (a *App) mcpCreateRemoteDeviceTask(ctx context.Context, args mcpCreateArgs, prompt, name, mode string, accept func(path string)) (mcpTaskView, error) {
	grantID := strings.TrimSpace(args.GrantID)
	payload := map[string]any{"name": name, "mode": mode}
	if dir := strings.TrimSpace(args.WorkingDir); dir != "" {
		payload["work_dir"] = dir
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return mcpTaskView{}, err
	}
	raw, err := a.mcpRemoteDeviceOp(grantID, "create_task", string(body))
	if err != nil {
		return mcpTaskView{}, err
	}
	path, remoteName, workDir, err := mcpRemoteDeviceCreated(raw)
	if err != nil {
		return mcpTaskView{}, err
	}
	view := mcpTaskView{
		ProjectPath: path,
		Name:        firstNonEmpty(remoteName, name),
		Mode:        mode,
		Location:    mcpLocationRemoteDevice,
		GrantID:     grantID,
		Status:      mcpStatusIdle,
		WorkingDir:  workDir,
		workspace:   workDir,
	}
	if prompt != "" {
		message, marshalErr := json.Marshal(map[string]string{"project_path": path, "text": prompt})
		if marshalErr != nil {
			return mcpTaskView{}, marshalErr
		}
		if _, sendErr := a.mcpRemoteDeviceOp(grantID, "send_message", string(message)); sendErr != nil {
			view.Status = mcpStatusFailed
			view.Error = "任务已在远程设备创建，但首条指令未发出"
			view.EndReason = mcpStatusFailed
			view.EndDetail = sendErr.Error()
		} else {
			view.Status = mcpStatusRunning
			view.Text = "指令已发到远程设备。"
		}
	}
	a.recordMCPAccess(ctx, "create_task", view.Name, path)
	if accept != nil {
		accept(path)
	}
	// The device runs the round. The local record stays idle, or failed when
	// the first message never left, so a later send_command is not rejected
	// as still running on this computer. Do not read this computer's history:
	// that opens the hub for a path that belongs to the other computer.
	a.mcpStoreDelegated(view)
	return a.mcpPublishInstalledCreate(view), nil
}

func (a *App) mcpRemoteDeviceOp(grantID, op, body string) (string, error) {
	if mcpRemoteDeviceOpFunc != nil {
		return mcpRemoteDeviceOpFunc(a, grantID, op, body)
	}
	if a == nil {
		return "", errors.New("远程设备还没有准备好")
	}
	return a.RemoteControlOp(grantID, op, body)
}

func mcpRemoteDeviceCreated(raw string) (path, name, workDir string, err error) {
	var decoded struct {
		Task struct {
			ProjectPath string `json:"project_path"`
			Name        string `json:"name"`
			WorkingDir  string `json:"working_dir"`
		} `json:"task"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return "", "", "", errors.New("远程设备没有返回任务")
	}
	path = normalizeProjectSessionPath(decoded.Task.ProjectPath)
	if path == "" {
		return "", "", "", errors.New("没有在远程设备上创建任务")
	}
	return path, strings.TrimSpace(decoded.Task.Name), strings.TrimSpace(decoded.Task.WorkingDir), nil
}

func (a *App) mcpRefreshRemoteDeviceTask(view mcpTaskView) (mcpTaskView, error) {
	body, err := json.Marshal(map[string]string{"project_path": view.ProjectPath})
	if err != nil {
		return mcpTaskView{}, err
	}
	raw, err := a.mcpRemoteDeviceOp(view.GrantID, "get_task", string(body))
	if err != nil {
		return mcpTaskView{}, err
	}
	var decoded struct {
		Task struct {
			Name           string                `json:"name"`
			WorkingDir     string                `json:"working_dir"`
			ActiveWorkflow *ProjectWorkflowState `json:"active_workflow"`
		} `json:"task"`
		Messages []ProjectConversationHistoryItem `json:"messages"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return mcpTaskView{}, errors.New("远程设备没有返回任务")
	}
	mcpStripDelegatedBookkeeping(&view)
	text := mcpLastAssistantText(decoded.Messages)
	workflow := ""
	if decoded.Task.ActiveWorkflow != nil {
		workflow = decoded.Task.ActiveWorkflow.Status
	}
	if name := strings.TrimSpace(decoded.Task.Name); name != "" && name != view.ProjectPath {
		view.Name = name
	}
	if dir := strings.TrimSpace(decoded.Task.WorkingDir); dir != "" {
		view.WorkingDir = dir
		view.workspace = dir
	}
	// A newer user line means the other computer has not answered yet.
	// The previous assistant text would otherwise look like a finished round.
	if mcpTranscriptAwaitingReply(decoded.Messages) {
		view.Text = ""
		view.Status = mcpStatusRunning
	} else {
		view.Text = text
		view.Status = mcpDisplayStatus("", workflow, text)
		// result_status incomplete is the other computer's failed turn.
		// The sentence itself often does not match a local failure phrase.
		if view.Status == mcpStatusCompleted && mcpRemoteHistoryIncomplete(decoded.Messages) {
			view.Status = mcpStatusFailed
			view.Error = text
			view.EndReason = mcpStatusFailed
		}
	}
	view.Location = mcpLocationRemoteDevice
	return view, nil
}

func (a *App) mcpSendRemoteDeviceCommand(view mcpTaskView, command string) (mcpTaskView, error) {
	body, err := json.Marshal(map[string]string{"project_path": view.ProjectPath, "text": command})
	if err != nil {
		return mcpTaskView{}, err
	}
	if _, err := a.mcpRemoteDeviceOp(view.GrantID, "send_message", string(body)); err != nil {
		return mcpTaskView{}, err
	}
	a.mcpClearDelegatedBookkeeping(view.ProjectPath)
	view.Status = mcpStatusRunning
	view.Text = "指令已发到远程设备。"
	view.Error = ""
	view.EndReason = ""
	view.EndDetail = ""
	view.ConfirmationKind = ""
	view.ConfirmationID = ""
	view.Commands = nil
	return view, nil
}

func (a *App) mcpStopRemoteDevice(view mcpTaskView) (mcpTaskView, error) {
	body, err := json.Marshal(map[string]string{"project_path": view.ProjectPath})
	if err != nil {
		return mcpTaskView{}, err
	}
	if _, err := a.mcpRemoteDeviceOp(view.GrantID, "stop_task", string(body)); err != nil {
		return mcpTaskView{}, err
	}
	now := time.Now()
	view.Status = mcpStatusCancelled
	view.EndReason = mcpStatusCancelled
	view.FinishedAt = mcpFormatTime(now)
	view.Stop = mcpStopFor(mcpStatusCancelled, "", nil, context.Canceled, now)
	stored := mcpRunRecordFromView(view)
	stored.Status = mcpStatusCancelled
	stored.EndReason = mcpStatusCancelled
	stored.FinishedAt = now
	stored.Stop = mcpCloneStop(view.Stop)
	a.mcpStoreRun(stored)
	return view, nil
}

// mcpCreateCloudAgentTask creates a cloud agent and one task on it.
// A cloud workspace id is not an agent. The prompt goes out with
// cloud_send_message. A failed delivery still returns the created task.
func (a *App) mcpCreateCloudAgentTask(ctx context.Context, args mcpCreateArgs, prompt, name, mode string, accept func(path string)) (mcpTaskView, error) {
	kind := "session"
	if mode == taskCodingDevTag {
		kind = "coding"
	}
	row, err := a.mcpOpenCloudAgent(name, kind)
	if err != nil {
		return mcpTaskView{}, err
	}
	instanceID := strings.TrimSpace(row.ID)
	if instanceID == "" || strings.ContainsAny(instanceID, `/\`) {
		return mcpTaskView{}, errors.New("没有创建云端 agent")
	}
	grantID, err := a.mcpCloudAgentGrant(instanceID)
	if err != nil {
		a.mcpDiscardCloudAgent(instanceID)
		return mcpTaskView{}, err
	}
	grantID = strings.TrimSpace(grantID)
	if !mcpValidGrantID(grantID) {
		a.mcpDiscardCloudAgent(instanceID)
		return mcpTaskView{}, errors.New("没有打开云端控制")
	}
	body, err := json.Marshal(map[string]string{"title": name})
	if err != nil {
		a.mcpDiscardCloudAgent(instanceID)
		return mcpTaskView{}, err
	}
	raw, err := a.mcpCloudAgentOp(grantID, "cloud_create_task", string(body))
	if err != nil {
		a.mcpDiscardCloudAgent(instanceID)
		return mcpTaskView{}, err
	}
	taskID, err := mcpCloudAgentCreated(raw)
	if err != nil {
		a.mcpDiscardCloudAgent(instanceID)
		return mcpTaskView{}, err
	}
	path := mcpCloudAgentKey(instanceID, taskID)
	if path == "" {
		return mcpTaskView{}, errors.New("没有在云端 agent 上创建任务")
	}
	view := mcpTaskView{
		ProjectPath: path,
		Name:        firstNonEmpty(strings.TrimSpace(row.Name), name),
		Mode:        mode,
		Location:    mcpLocationCloud,
		GrantID:     grantID,
		Status:      mcpStatusIdle,
	}
	if prompt != "" {
		message, marshalErr := json.Marshal(map[string]string{
			"task_id":           taskID,
			"text":              prompt,
			"client_message_id": "mcp-create:" + taskID,
		})
		if marshalErr != nil {
			return mcpTaskView{}, marshalErr
		}
		if _, sendErr := a.mcpCloudAgentOp(grantID, "cloud_send_message", string(message)); sendErr != nil {
			view.Status = mcpStatusFailed
			view.Error = "任务已在云端 agent 创建，但首条指令未发出"
			view.EndReason = mcpStatusFailed
			view.EndDetail = sendErr.Error()
		} else {
			view.Status = mcpStatusRunning
			view.Text = "指令已发到云端 agent。"
		}
	}
	a.recordMCPAccess(ctx, "create_task", view.Name, path)
	if accept != nil {
		accept(path)
	}
	// The agent runs the round. The local record stays idle, or failed when
	// the first message never left, so a later send_command is not rejected
	// as still running on this computer.
	a.mcpStoreDelegated(view)
	return a.mcpPublishInstalledCreate(view), nil
}

func (a *App) mcpOpenCloudAgent(name, kind string) (CloudAgentRow, error) {
	if mcpCloudAgentCreateFunc != nil {
		return mcpCloudAgentCreateFunc(a, name, kind)
	}
	if a == nil {
		return CloudAgentRow{}, errors.New("云端 agent 还没有准备好")
	}
	return a.CreateCloudAgent(name, kind)
}

func (a *App) mcpCloudAgentGrant(instanceID string) (string, error) {
	if mcpCloudAgentGrantFunc != nil {
		return mcpCloudAgentGrantFunc(a, instanceID)
	}
	if a == nil {
		return "", errors.New("云端 agent 还没有准备好")
	}
	return a.OpenCloudAgentGrant(instanceID)
}

// mcpDiscardCloudAgent deletes an instance that has no task yet.
// The instance quota is small, and a retry would otherwise create another one.
func (a *App) mcpDiscardCloudAgent(instanceID string) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return
	}
	if mcpCloudAgentDeleteFunc != nil {
		_ = mcpCloudAgentDeleteFunc(a, instanceID)
		return
	}
	if a == nil {
		return
	}
	_ = a.DeleteCloudAgent(instanceID)
}

func (a *App) mcpCloudAgentOp(grantID, op, body string) (string, error) {
	if mcpCloudAgentOpFunc != nil {
		return mcpCloudAgentOpFunc(a, grantID, op, body)
	}
	if a == nil {
		return "", errors.New("云端 agent 还没有准备好")
	}
	return a.RemoteControlOp(grantID, op, body)
}

func mcpCloudAgentKey(instanceID, taskID string) string {
	instanceID = strings.TrimSpace(instanceID)
	taskID = strings.TrimSpace(taskID)
	if instanceID == "" || taskID == "" || strings.ContainsAny(instanceID, `/\`) || strings.ContainsAny(taskID, `/\`) {
		return ""
	}
	return normalizeProjectSessionPath(filepath.Join("cloud-agent", instanceID, taskID))
}

func mcpParseCloudAgentKey(projectPath string) (instanceID, taskID string, ok bool) {
	cleaned := filepath.ToSlash(normalizeProjectSessionPath(projectPath))
	parts := strings.Split(cleaned, "/")
	if len(parts) != 3 || parts[0] != "cloud-agent" || parts[1] == "" || parts[2] == "" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

func mcpCloudAgentCreated(raw string) (string, error) {
	var decoded struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return "", errors.New("云端 agent 没有返回任务")
	}
	taskID := strings.TrimSpace(decoded.TaskID)
	if taskID == "" || strings.ContainsAny(taskID, `/\`) {
		return "", errors.New("没有在云端 agent 上创建任务")
	}
	return taskID, nil
}

func mcpCloudAgentTranscript(lines []string, asError []bool, turnStatus string) (text, status string) {
	lastRole := ""
	lastAssistant := ""
	lastAssistantFailed := false
	for i, line := range lines {
		role, content, _ := strings.Cut(line, "\n")
		role = strings.TrimSpace(role)
		if role == "assistant" {
			lastAssistant = content
			lastAssistantFailed = i < len(asError) && asError[i]
		}
		lastRole = role
	}
	// A newer user line means the next turn has started. An older error
	// stays in the transcript and does not keep this read failed.
	// Cancel does not append a line, so the turn status is what ends it.
	if lastRole == "user" {
		if strings.EqualFold(strings.TrimSpace(turnStatus), "cancelled") {
			return "", mcpStatusCancelled
		}
		return "", mcpStatusRunning
	}
	if strings.TrimSpace(lastAssistant) == "" {
		return "", mcpStatusIdle
	}
	if lastAssistantFailed {
		return lastAssistant, mcpStatusFailed
	}
	return lastAssistant, mcpDisplayStatus("", "", lastAssistant)
}

func (a *App) mcpRefreshCloudAgentTask(view mcpTaskView) (mcpTaskView, error) {
	_, taskID, ok := mcpParseCloudAgentKey(view.ProjectPath)
	if !ok {
		return mcpTaskView{}, errors.New("没有找到这个云端任务")
	}
	body, err := json.Marshal(map[string]string{"task_id": taskID})
	if err != nil {
		return mcpTaskView{}, err
	}
	raw, err := a.mcpCloudAgentOp(view.GrantID, "cloud_get_task", string(body))
	if err != nil {
		return mcpTaskView{}, err
	}
	var decoded struct {
		Messages   []string `json:"messages"`
		AsError    []bool   `json:"as_error"`
		TurnStatus string   `json:"turn_status"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return mcpTaskView{}, errors.New("云端 agent 没有返回任务")
	}
	mcpStripDelegatedBookkeeping(&view)
	text, status := mcpCloudAgentTranscript(decoded.Messages, decoded.AsError, decoded.TurnStatus)
	view.Text = text
	view.Status = status
	view.Location = mcpLocationCloud
	if status == mcpStatusFailed {
		view.Error = text
		view.EndReason = mcpStatusFailed
	}
	// A commit ask lives on the watch snapshot, not in the transcript.
	// Only a finished coding answer can carry that card. A session agent
	// does not, and a failed or still-running transcript must not be covered
	// by a stale terminal.
	if status == mcpStatusCompleted && strings.TrimSpace(view.Mode) == taskCodingDevTag {
		if watched, watchErr := a.mcpCloudAgentOp(view.GrantID, "cloud_watch_task", string(body)); watchErr == nil {
			mcpApplyCloudAgentAsk(&view, watched)
		}
	}
	return view, nil
}

// mcpApplyCloudAgentAsk turns a commit card into needs_confirmation.
// cloud_get_task has already finished the turn, so the question would
// otherwise look like a completed answer and the caller would not send a choice.
func mcpApplyCloudAgentAsk(view *mcpTaskView, raw string) {
	if view == nil {
		return
	}
	question, confirmationID, commands, ok := mcpCloudAgentAsk(raw)
	if !ok {
		return
	}
	if question != "" && strings.TrimSpace(view.Text) == "" {
		view.Text = question
	}
	view.Status = mcpStatusNeedsConfirmation
	view.ConfirmationKind = "ask"
	if strings.Contains(confirmationID, "commit-approval") {
		view.ConfirmationKind = "commit"
	}
	view.ConfirmationID = confirmationID
	view.Commands = commands
	hint := "需要你的回答。用 send_command 把回答原文发回这个任务。"
	if view.ConfirmationKind == "commit" {
		hint = "云端编程要确认这次提交。用 send_command 原样发送 commands 中的一条。"
	}
	if !strings.Contains(view.Text, "send_command") {
		view.Text = strings.TrimSpace(view.Text + "\n\n" + hint)
	}
}

func mcpCloudAgentAsk(raw string) (question, confirmationID string, commands []string, ok bool) {
	var snap struct {
		Terminal json.RawMessage `json:"terminal"`
	}
	if err := json.Unmarshal([]byte(raw), &snap); err != nil || len(snap.Terminal) == 0 || string(snap.Terminal) == "null" {
		return "", "", nil, false
	}
	var terminal struct {
		Text    string `json:"text"`
		Ask     string `json:"ask_user_question"`
		Options string `json:"ask_user_options_json"`
		Confirm struct {
			ID string `json:"id"`
		} `json:"confirmation"`
		Actions []struct {
			Command string `json:"command"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(snap.Terminal, &terminal); err != nil {
		return "", "", nil, false
	}
	confirmationID = strings.TrimSpace(terminal.Confirm.ID)
	for _, action := range terminal.Actions {
		if command := strings.TrimSpace(action.Command); command != "" {
			commands = append(commands, command)
		}
	}
	if len(commands) == 0 && strings.TrimSpace(terminal.Options) != "" {
		var labels []string
		if err := json.Unmarshal([]byte(terminal.Options), &labels); err == nil {
			for _, label := range labels {
				if command := strings.TrimSpace(label); command != "" {
					commands = append(commands, command)
				}
			}
		}
	}
	question = strings.TrimSpace(firstNonEmpty(terminal.Ask, terminal.Text))
	if confirmationID == "" && question == "" && len(commands) == 0 {
		return "", "", nil, false
	}
	if confirmationID == "" {
		confirmationID = "cloud-ask"
	}
	// A plain text terminal is the finished answer, not an ask.
	if strings.TrimSpace(terminal.Ask) == "" && len(commands) == 0 && strings.TrimSpace(terminal.Confirm.ID) == "" {
		return "", "", nil, false
	}
	return question, confirmationID, commands, true
}

// mcpStripDelegatedBookkeeping drops the previous local stop or failure
// before a live read is applied. The caller puts back a stop that the
// transcript still cannot show.
func mcpStripDelegatedBookkeeping(view *mcpTaskView) {
	if view == nil {
		return
	}
	view.Error = ""
	view.EndReason = ""
	view.EndDetail = ""
	view.FinishedAt = ""
	view.Stop = nil
	view.ConfirmationKind = ""
	view.ConfirmationID = ""
	view.Commands = nil
}

// mcpKeepDelegatedBookkeeping keeps a local stop or a failed first send when
// the live read has nothing newer. A later assistant answer is shown as itself.
func mcpKeepDelegatedBookkeeping(saved, live mcpTaskView) mcpTaskView {
	if saved.Status == mcpStatusFailed && live.Status == mcpStatusIdle {
		live.Status = mcpStatusFailed
		live.Error = saved.Error
		live.EndReason = saved.EndReason
		live.EndDetail = saved.EndDetail
		live.Stop = nil
		return live
	}
	if saved.Status != mcpStatusCancelled {
		return live
	}
	// A live cancel has no assistant answer either. Keep the stored stop
	// so a later read does not mint a new one. A finished answer stays.
	switch live.Status {
	case mcpStatusRunning, mcpStatusIdle, mcpStatusCancelled:
		live.Status = mcpStatusCancelled
		live.EndReason = mcpStatusCancelled
		live.Stop = mcpCloneStop(saved.Stop)
		if strings.TrimSpace(saved.FinishedAt) != "" {
			live.FinishedAt = saved.FinishedAt
		}
		return live
	default:
		return live
	}
}

// mcpNoteDiscoveredCancel fills a stop when the other side cancelled and
// this computer did not record one. A stored stop is left as it is.
func mcpNoteDiscoveredCancel(live mcpTaskView) mcpTaskView {
	if live.Status != mcpStatusCancelled || live.Stop != nil {
		return live
	}
	now := time.Now()
	live.EndReason = mcpStatusCancelled
	if strings.TrimSpace(live.FinishedAt) == "" {
		live.FinishedAt = mcpFormatTime(now)
	}
	live.Stop = mcpStopFor(mcpStatusCancelled, "", nil, context.Canceled, now)
	return live
}

// mcpFinishDelegatedRead drops a stored first-send failure once the live
// transcript has moved past an empty book. The in-memory saved row is what
// the stop overlay still reads, so a cancel is not rewritten here.
func (a *App) mcpFinishDelegatedRead(saved, live mcpTaskView) mcpTaskView {
	if saved.Status == mcpStatusFailed && live.Status != mcpStatusIdle {
		a.mcpClearDelegatedBookkeeping(saved.ProjectPath)
	}
	return mcpNoteDiscoveredCancel(mcpKeepDelegatedBookkeeping(saved, live))
}

// mcpClearDelegatedBookkeeping lets the next get_task read the live task.
// The local record stays idle so a follow-up is not rejected as running here.
func (a *App) mcpClearDelegatedBookkeeping(path string) {
	if a == nil {
		return
	}
	path = normalizeProjectSessionPath(path)
	a.mcpRunsMu.Lock()
	rec := a.mcpRuns[path]
	changed := rec != nil && (rec.Status == mcpStatusCancelled || rec.Status == mcpStatusFailed)
	if changed {
		rec.Status = mcpStatusIdle
		rec.Text = ""
		rec.Error = ""
		rec.EndReason = ""
		rec.EndDetail = ""
		rec.FinishedAt = time.Time{}
		rec.Stop = nil
	}
	a.mcpRunsMu.Unlock()
	if changed {
		a.mcpPersistRun(path)
	}
}

// mcpStoreDelegated keeps the bookkeeping record for a cloud agent or a
// remote device. A failed first send stays failed so the same request id
// can replay that error. A running round is not stored: the other side is
// running it, and a local running record would reject the next command.
func (a *App) mcpStoreDelegated(view mcpTaskView) {
	if a == nil {
		return
	}
	view.historySeal = "delegated"
	stored := mcpRunRecordFromView(view)
	stored.Text = ""
	stored.Error = ""
	stored.EndReason = ""
	stored.EndDetail = ""
	stored.FinishedAt = time.Time{}
	stored.Stop = nil
	if view.Status == mcpStatusFailed {
		stored.Status = mcpStatusFailed
		stored.Error = view.Error
		stored.EndReason = view.EndReason
		stored.EndDetail = view.EndDetail
	} else {
		stored.Status = mcpStatusIdle
	}
	a.mcpStoreRun(stored)
}

func (a *App) mcpSendCloudAgentCommand(view mcpTaskView, command, clientRequestID string) (mcpTaskView, error) {
	_, taskID, ok := mcpParseCloudAgentKey(view.ProjectPath)
	if !ok {
		return mcpTaskView{}, errors.New("没有找到这个云端任务")
	}
	messageID := strings.TrimSpace(clientRequestID)
	if messageID == "" {
		messageID = "mcp:" + taskID + ":" + strconv.FormatInt(time.Now().UnixNano(), 36)
	} else {
		// The same request id must hit the same cloud message. A retry after
		// a lost response then does not start a second turn.
		messageID = "mcp:" + taskID + ":" + messageID
	}
	body, err := json.Marshal(map[string]string{
		"task_id":           taskID,
		"text":              command,
		"client_message_id": messageID,
	})
	if err != nil {
		return mcpTaskView{}, err
	}
	if _, err := a.mcpCloudAgentOp(view.GrantID, "cloud_send_message", string(body)); err != nil {
		return mcpTaskView{}, err
	}
	a.mcpClearDelegatedBookkeeping(view.ProjectPath)
	view.Status = mcpStatusRunning
	view.Text = "指令已发到云端 agent。"
	view.Error = ""
	view.EndReason = ""
	view.EndDetail = ""
	view.ConfirmationKind = ""
	view.ConfirmationID = ""
	view.Commands = nil
	return view, nil
}

func (a *App) mcpStopCloudAgent(view mcpTaskView) (mcpTaskView, error) {
	_, taskID, ok := mcpParseCloudAgentKey(view.ProjectPath)
	if !ok {
		return mcpTaskView{}, errors.New("没有找到这个云端任务")
	}
	body, err := json.Marshal(map[string]string{"task_id": taskID})
	if err != nil {
		return mcpTaskView{}, err
	}
	if _, err := a.mcpCloudAgentOp(view.GrantID, "cloud_stop_task", string(body)); err != nil {
		return mcpTaskView{}, err
	}
	now := time.Now()
	view.Status = mcpStatusCancelled
	view.EndReason = mcpStatusCancelled
	view.FinishedAt = mcpFormatTime(now)
	view.Stop = mcpStopFor(mcpStatusCancelled, "", nil, context.Canceled, now)
	stored := mcpRunRecordFromView(view)
	stored.Status = mcpStatusCancelled
	stored.EndReason = mcpStatusCancelled
	stored.FinishedAt = now
	stored.Stop = mcpCloneStop(view.Stop)
	a.mcpStoreRun(stored)
	return view, nil
}

func mcpRunMismatch(saved mcpTaskView, path, runID string) error {
	if runID != "" && runID != strings.TrimSpace(saved.RunID) && runID != strings.TrimSpace(saved.CurrentRunID) {
		return mcpErr(mcpCodeRunNotFound, "没有这一轮执行", path, runID)
	}
	return nil
}

func mcpIsCloudAgent(view mcpTaskView) bool {
	return strings.TrimSpace(view.Location) == mcpLocationCloud && strings.TrimSpace(view.GrantID) != ""
}

func mcpIsRemoteDevice(view mcpTaskView) bool {
	return strings.TrimSpace(view.Location) == mcpLocationRemoteDevice && strings.TrimSpace(view.GrantID) != ""
}

// mcpPublishInstalledCreate copies the run id commit just minted and binds
// that session to the in-flight create request.
func (a *App) mcpPublishInstalledCreate(view mcpTaskView) mcpTaskView {
	if saved, ok := a.mcpViewFromRegistry(view.ProjectPath); ok {
		if strings.TrimSpace(view.RunID) == "" {
			view.RunID = saved.RunID
			view.CurrentRunID = saved.CurrentRunID
			view.StatusVersion = saved.StatusVersion
			view.Round = saved.Round
		}
	}
	a.mcpNoteRequestRun(view.ProjectPath, view.RunID)
	return a.mcpPublishTaskView(view)
}

func mcpRunRecordFromView(view mcpTaskView) *mcpRunRecord {
	return &mcpRunRecord{
		ProjectPath:      view.ProjectPath,
		Name:             view.Name,
		Mode:             view.Mode,
		Status:           view.Status,
		Provider:         view.Provider,
		Model:            view.Model,
		Workspace:        view.workspace,
		Location:         view.Location,
		CloudWorkspaceID: view.CloudWorkspaceID,
		GrantID:          view.GrantID,
		RemoteHost:       view.RemoteHost,
		RemoteUser:       view.RemoteUser,
		RemotePort:       view.RemotePort,
		RemoteWorkDir:    view.RemoteWorkDir,
		RunID:            strings.TrimSpace(view.RunID),
		historyCount:     view.historyCount,
		historySeal:      view.historySeal,
	}
}

func mcpRemoteWorkDir(args mcpCreateArgs) string {
	if dir := strings.TrimSpace(args.RemoteWorkDir); dir != "" {
		return sanitizeTaskMetadataTagValue(dir)
	}
	return sanitizeTaskMetadataTagValue(args.WorkingDir)
}

// mcpRemoteTarget is the SSH target for one remote programming task.
// Port 0 means 22. The password stays on this struct and is not returned.
func mcpRemoteTarget(args mcpCreateArgs) (*RemoteTarget, error) {
	if strings.TrimSpace(args.ArgError) != "" {
		return nil, errors.New(args.ArgError)
	}
	host := normalizeSSHHostInput(args.SSHHost)
	user := sanitizeTaskMetadataTagValue(args.SSHUser)
	workDir := mcpRemoteWorkDir(args)
	password := strings.TrimSpace(args.SSHPassword)
	if host == "" || user == "" || password == "" || workDir == "" {
		return nil, errors.New("远程编程需要 ssh_host、ssh_user、ssh_password 和远端工作目录")
	}
	port := args.SSHPort
	if port == 0 {
		port = 22
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("ssh_port 必须是 1 到 65535")
	}
	return &RemoteTarget{
		Host:     host,
		Port:     port,
		User:     user,
		Password: password,
		WorkDir:  workDir,
	}, nil
}

func (a *App) mcpSendCommand(ctx context.Context, projectPath, command, confirmationID, clientRequestID string, wait bool) (view mcpTaskView, err error) {
	if a == nil {
		return mcpTaskView{}, errors.New("没有找到这个任务")
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return mcpTaskView{}, errors.New("命令不能为空")
	}
	// Load before the registry lookup. A cloud agent or remote device is only
	// in that registry; without the load, the first send after a restart
	// would start a prompt on this computer.
	a.mcpLoadDurable()
	registryPath := normalizeProjectSessionPath(projectPath)
	opts := mcpCallOptsFrom(ctx)
	// A scope or critical command continues the live round. A model change
	// would start a different round, so it is rejected before the confirmation
	// id is consumed and before a request id is claimed.
	if mcpCommandChangesModel(opts) && mcpCommandContinuesLiveRound(command) {
		return mcpTaskView{}, errors.New("续跑当前轮不能同时改模型")
	}
	if opts.HasExpected || opts.DryRun {
		a.mcpLoadDurable()
		current, known := a.mcpViewFromRegistry(registryPath)
		if err := mcpRejectStaleVersion(registryPath, current, known, opts); err != nil {
			return mcpTaskView{}, err
		}
		if opts.DryRun {
			// A running round rejects a model change before the catalog is
			// read. An unknown provider must still say the task is running.
			if mcpCommandChangesModel(opts) && known && current.Status == mcpStatusRunning {
				return mcpTaskView{}, errMCPTaskRunning
			}
			if mcpCommandChangesModel(opts) {
				name, chosen, err := a.mcpResolveCommandSelection(opts.Provider, opts.Model)
				if err != nil {
					return mcpTaskView{}, err
				}
				current = mcpStampCommandModel(current, name, chosen)
			}
			return a.mcpDryRunView(registryPath, current, known)
		}
	}
	req, claimed, err := a.mcpClaimRequest(clientRequestID, mcpRequestKindCommand, mcpCommandClaimKey(projectPath, command, confirmationID), "")
	if err != nil {
		return mcpTaskView{}, err
	}
	if !claimed {
		return a.mcpReplayRequest(ctx, req)
	}
	accepted := false
	defer func() {
		if !accepted {
			a.mcpAbandonRequest(req, err)
			return
		}
		a.mcpCompleteRequest(req, err)
	}()
	accept := func(path string) {
		accepted = true
		a.mcpBindRequest(req, path)
	}
	reg, inRegistry := a.mcpViewFromRegistry(registryPath)
	if inRegistry && mcpIsCloudAgent(reg) {
		if mcpCommandChangesModel(opts) {
			err = errors.New("云端 agent 的模型由云端实例决定，不要传 provider 或 model")
			return mcpTaskView{}, err
		}
		sent, sendErr := a.mcpSendCloudAgentCommand(reg, command, clientRequestID)
		if sendErr != nil {
			err = sendErr
			return mcpTaskView{}, err
		}
		accept(firstNonEmpty(reg.ProjectPath, registryPath))
		a.mcpNoteRequestRun(reg.ProjectPath, reg.RunID)
		return a.mcpReturnTask(sent), nil
	}
	if inRegistry && mcpIsRemoteDevice(reg) {
		if mcpCommandChangesModel(opts) {
			err = errors.New("远程设备上的模型由对方电脑决定，不要传 provider 或 model")
			return mcpTaskView{}, err
		}
		sent, sendErr := a.mcpSendRemoteDeviceCommand(reg, command)
		if sendErr != nil {
			err = sendErr
			return mcpTaskView{}, err
		}
		accept(firstNonEmpty(reg.ProjectPath, registryPath))
		a.mcpNoteRequestRun(reg.ProjectPath, reg.RunID)
		return a.mcpReturnTask(sent), nil
	}
	if live, err := a.mcpPrepareLiveGate(projectPath, reg, inRegistry, command); live {
		if err != nil {
			return mcpTaskView{}, err
		}
		if err := a.mcpRequireLiveConfirmation(projectPath, reg, inRegistry, confirmationID); err != nil {
			return mcpTaskView{}, err
		}
		accept(firstNonEmpty(reg.ProjectPath, registryPath))
		a.mcpNoteRequestRun(firstNonEmpty(reg.ProjectPath, registryPath), firstNonEmpty(reg.RunID, reg.CurrentRunID))
		view, _, err := a.mcpOperateLiveGate(projectPath, reg, inRegistry, command)
		if err != nil {
			return mcpTaskView{}, err
		}
		return a.mcpReturnTask(view), nil
	}
	if inRegistry && reg.Status == mcpStatusRunning {
		return mcpTaskView{}, errMCPTaskRunning
	}
	if err := a.mcpRequireListedConfirmation(projectPath, reg, inRegistry, command, confirmationID); err != nil {
		return mcpTaskView{}, err
	}
	path, err := a.remoteControlKnownTask(projectPath)
	if err != nil && !inRegistry {
		return mcpTaskView{}, err
	}
	if path == "" {
		path = registryPath
	}
	view = reg
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
		view, err = a.mcpConfirmRecovery(ctx, view, command, taskID, digest, confirmed)
		if err != nil {
			return mcpTaskView{}, err
		}
		a.recordMCPAccess(ctx, "send_command", view.Name, view.ProjectPath)
		accept(view.ProjectPath)
		a.mcpNoteRequestRun(view.ProjectPath, view.RunID)
		return a.mcpReturnTask(view), nil
	}
	if mcpCommandChangesModel(opts) {
		name, chosen, err := a.mcpResolveCommandSelection(opts.Provider, opts.Model)
		if err != nil {
			return mcpTaskView{}, err
		}
		view = mcpStampCommandModel(view, name, chosen)
	}
	return a.mcpStartPrompt(ctx, view, command, wait, false, func(path string) {
		accept(path)
		a.recordMCPAccess(ctx, "send_command", view.Name, path)
	})
}

func (a *App) mcpRetrySteps(ctx context.Context, projectPath string, rawIDs []string, confirmationID, clientRequestID string, wait bool) (view mcpTaskView, err error) {
	if a == nil {
		return mcpTaskView{}, errors.New("没有找到这个任务")
	}
	ids, normErr := mcpNormalizeStepIDs(rawIDs)
	if normErr != nil {
		return mcpTaskView{}, normErr
	}
	path := normalizeProjectSessionPath(projectPath)
	if path == "" {
		return mcpTaskView{}, errors.New("没有找到这个任务")
	}
	a.mcpLoadDurable()
	if reg, ok := a.mcpViewFromRegistry(path); ok && (mcpIsCloudAgent(reg) || mcpIsRemoteDevice(reg)) {
		return mcpTaskView{}, errors.New("这个任务不在本机执行，不能续跑步骤")
	}
	confirmationID = strings.TrimSpace(confirmationID)
	opts := mcpCallOptsFrom(ctx)
	if opts.HasExpected || opts.DryRun {
		a.mcpLoadDurable()
		current, known := a.mcpViewFromRegistry(path)
		if err := mcpRejectStaleVersion(path, current, known, opts); err != nil {
			return mcpTaskView{}, err
		}
		if opts.DryRun {
			return a.mcpDryRunView(path, current, known)
		}
	}
	req, claimed, claimErr := a.mcpClaimRequest(clientRequestID, mcpRequestKindRetry, mcpRetryClaimKey(path, confirmationID, ids), "")
	if claimErr != nil {
		return mcpTaskView{}, claimErr
	}
	if !claimed {
		return a.mcpReplayRequest(ctx, req)
	}
	accepted := false
	defer func() {
		if !accepted {
			a.mcpAbandonRequest(req, err)
			return
		}
		a.mcpCompleteRequest(req, err)
	}()
	reg, inRegistry := a.mcpViewFromRegistry(path)
	if inRegistry && reg.Status == mcpStatusRunning {
		err = errMCPTaskRunning
		return mcpTaskView{}, err
	}
	publishedID := ""
	// The plan card's confirmation id is stored on the session holder so the
	// next read keeps it. That id is not the retry credential.
	if inRegistry && !mcpUnsealedSessionHolder(reg) {
		publishedID = strings.TrimSpace(reg.ConfirmationID)
	}
	if publishedID != "" && publishedID != confirmationID {
		err = errors.New("确认编号不匹配或已失效")
		return mcpTaskView{}, err
	}
	if inRegistry {
		if rej := mcpRetryRejectedBySteps(reg.Steps, ids); rej != nil {
			err = rej
			return mcpTaskView{}, err
		}
	}
	handler := a.mcpReadyIMHandler()
	if handler == nil {
		err = errors.New("还没有可续跑的编码检查点")
		return mcpTaskView{}, err
	}
	if !inRegistry {
		known, knownErr := a.remoteControlKnownTask(projectPath)
		if knownErr != nil {
			err = knownErr
			return mcpTaskView{}, err
		}
		path = known
		reg.ProjectPath = path
		if strings.TrimSpace(reg.Mode) == "" {
			reg.Mode = taskCodingDevTag
		}
	}
	userID := projectSessionOwnerID(path)
	if userID == "" || userID == desktopUserID {
		err = errors.New("没有创建任务")
		return mcpTaskView{}, err
	}
	ready := false
	if cp, ok := handler.loadCodingExecCheckpoint(userID); ok {
		if _, selErr := codingExecSelectRetrySteps(cp.Tasks, cp.Results, ids); selErr != nil {
			err = selErr
			return mcpTaskView{}, err
		}
		ready = true
	}
	if !ready {
		mem := handler.getStickyCodingWorkbenchMemory(userID)
		if len(mem.StepStatuses) > 0 {
			if _, selErr := codingPlanSelectNamedSteps(mem, ids); selErr != nil {
				err = selErr
				return mcpTaskView{}, err
			}
			ready = true
		}
	}
	if !ready {
		err = errors.New("还没有可续跑的编码检查点")
		return mcpTaskView{}, err
	}
	handler.storeCodingExecRetrySteps(userID, ids)
	view = reg
	view.ProjectPath = path
	if strings.TrimSpace(view.Mode) == "" {
		view.Mode = taskCodingDevTag
	}
	started := false
	view, err = a.mcpStartPrompt(ctx, view, codingExecCmdRetryFailed(), wait, false, func(bound string) {
		started = true
		if publishedID != "" {
			_ = a.mcpConsumeConfirmation(bound, confirmationID)
		}
		accepted = true
		a.mcpBindRequest(req, bound)
		a.recordMCPAccess(ctx, "retry_steps", view.Name, bound)
	})
	if err != nil && !started {
		handler.storeCodingExecRetrySteps(userID, nil)
	}
	return view, err
}

func (a *App) mcpGetTask(ctx context.Context, projectPath, runID string) (mcpTaskView, error) {
	path := normalizeProjectSessionPath(projectPath)
	runID = strings.TrimSpace(runID)
	if a == nil || path == "" {
		return mcpTaskView{}, mcpErr(mcpCodeTaskNotFound, "没有找到这个任务", path, runID)
	}
	a.mcpLoadDurable()
	if saved, ok := a.mcpViewFromRegistry(path); ok && mcpIsCloudAgent(saved) {
		if err := mcpRunMismatch(saved, path, runID); err != nil {
			return mcpTaskView{}, err
		}
		view, err := a.mcpRefreshCloudAgentTask(saved)
		if err != nil {
			return mcpTaskView{}, err
		}
		return a.mcpReturnTask(a.mcpFinishDelegatedRead(saved, view)), nil
	}
	if saved, ok := a.mcpViewFromRegistry(path); ok && mcpIsRemoteDevice(saved) {
		if err := mcpRunMismatch(saved, path, runID); err != nil {
			return mcpTaskView{}, err
		}
		view, err := a.mcpRefreshRemoteDeviceTask(saved)
		if err != nil {
			return mcpTaskView{}, err
		}
		return a.mcpReturnTask(a.mcpFinishDelegatedRead(saved, view)), nil
	}
	if runID != "" {
		view, ok := a.mcpViewForRun(path, runID)
		if !ok {
			if a.mcpTaskKnown(path) {
				return mcpTaskView{}, mcpErr(mcpCodeRunNotFound, "没有这一轮执行", path, runID)
			}
			if a.mcpTaskExpiredOnly(path) {
				return mcpTaskView{}, mcpErr(mcpCodeTaskExpired, mcpTaskExpiredMessage, path, runID)
			}
			return mcpTaskView{}, mcpErr(mcpCodeTaskNotFound, "没有找到这个任务", path, runID)
		}
		if a.memoryStore != nil && mcpUnsealedSessionHolder(view) && a.mcpViewIsLiveRound(view) {
			if known, err := a.remoteControlKnownTask(path); err == nil {
				view = mcpProjectSessionHolder(view, a.mcpTaskViewFromStore(known), a.mcpLiveConfirmationPending(path))
			}
		}
		return a.mcpReturnTask(view), nil
	}
	if view, ok := a.mcpViewFromRegistry(path); ok && view.Status == mcpStatusRunning {
		return a.mcpReturnTask(view), nil
	}
	reg, hasReg := a.mcpViewFromRegistry(path)
	if a.memoryStore != nil {
		known, err := a.remoteControlKnownTask(path)
		if err != nil {
			if hasReg {
				return a.mcpReturnTask(reg), nil
			}
			if a.mcpTaskExpiredOnly(path) {
				return mcpTaskView{}, mcpErr(mcpCodeTaskExpired, mcpTaskExpiredMessage, path, "")
			}
			return mcpTaskView{}, mcpErr(mcpCodeTaskNotFound, "没有找到这个任务", path, "")
		}
		store := a.mcpTaskViewFromStore(known)
		if !hasReg {
			return a.mcpReturnTask(store), nil
		}
		if mcpUnsealedSessionHolder(reg) {
			return a.mcpReturnTask(mcpProjectSessionHolder(reg, store, a.mcpLiveConfirmationPending(path))), nil
		}
		return a.mcpReturnTask(mcpPreferTaskView(reg, store)), nil
	}
	if hasReg {
		return a.mcpReturnTask(reg), nil
	}
	if a.mcpTaskExpiredOnly(path) {
		return mcpTaskView{}, mcpErr(mcpCodeTaskExpired, mcpTaskExpiredMessage, path, "")
	}
	return mcpTaskView{}, mcpErr(mcpCodeTaskNotFound, "没有找到这个任务", path, "")
}

func (a *App) mcpStopTask(ctx context.Context, projectPath string) (mcpTaskView, error) {
	if a == nil {
		return mcpTaskView{}, errors.New("没有找到这个任务")
	}
	registryPath := normalizeProjectSessionPath(projectPath)
	// Same as send: the delegated task is not visible until the registry is loaded.
	a.mcpLoadDurable()
	opts := mcpCallOptsFrom(ctx)
	if saved, ok := a.mcpViewFromRegistry(registryPath); ok && (mcpIsCloudAgent(saved) || mcpIsRemoteDevice(saved)) {
		if opts.HasExpected {
			a.mcpLoadDurable()
			current, known := a.mcpViewFromRegistry(registryPath)
			if err := mcpRejectStaleVersion(registryPath, current, known, opts); err != nil {
				return mcpTaskView{}, err
			}
		}
		var stopped mcpTaskView
		var stopErr error
		if mcpIsCloudAgent(saved) {
			stopped, stopErr = a.mcpStopCloudAgent(saved)
		} else {
			stopped, stopErr = a.mcpStopRemoteDevice(saved)
		}
		if stopErr != nil {
			return mcpTaskView{}, stopErr
		}
		return a.mcpPublishTaskView(stopped), nil
	}
	if opts.HasExpected {
		a.mcpLoadDurable()
		current, known := a.mcpViewFromRegistry(registryPath)
		if err := mcpRejectStaleVersion(registryPath, current, known, opts); err != nil {
			return mcpTaskView{}, err
		}
	}
	path, err := a.remoteControlKnownTask(projectPath)
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
			return a.mcpPublishTaskView(view), nil
		}
		return mcpTaskView{}, stopErr
	}
	if view, ok := a.mcpViewFromRegistry(path); ok {
		if !mcpUnsealedSessionHolder(view) {
			return a.mcpPublishTaskView(view), nil
		}
		return a.mcpPublishTaskView(mcpProjectSessionHolder(view, a.mcpTaskViewFromStore(path), false)), nil
	}
	return a.mcpPublishTaskView(a.mcpTaskViewFromStore(path)), nil
}

func (a *App) mcpListTasks(ctx context.Context) ([]mcpTaskView, error) {
	if a == nil {
		return []mcpTaskView{}, nil
	}
	a.mcpLoadDurable()
	listed := a.ListTasks(50)
	seen := map[string]struct{}{}
	out := make([]mcpTaskView, 0, len(listed))
	for _, task := range listed {
		path := normalizeProjectSessionPath(task.ProjectPath)
		if path == "" {
			continue
		}
		seen[path] = struct{}{}
		// A cloud agent or remote device keeps its identity on this path.
		// The local conversation at the same string is a different task.
		if view, ok := a.mcpViewFromRegistry(path); ok && (mcpIsCloudAgent(view) || mcpIsRemoteDevice(view)) {
			if view.Name == "" {
				view.Name = task.Name
			}
			if view.Mode == "" {
				view.Mode = mcpModeFromTags(task.Tags)
			}
			out = append(out, a.mcpReturnTask(view))
			continue
		}
		if view, ok := a.mcpViewFromRegistry(path); ok && view.Status == mcpStatusRunning {
			if view.Name == "" {
				view.Name = task.Name
			}
			if view.Mode == "" {
				view.Mode = mcpModeFromTags(task.Tags)
			}
			out = append(out, a.mcpReturnTask(view))
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
			if mcpUnsealedSessionHolder(view) {
				out = append(out, a.mcpReturnTask(mcpProjectSessionHolder(view, store, a.mcpLiveConfirmationPending(path))))
				continue
			}
			out = append(out, a.mcpReturnTask(mcpPreferTaskView(view, store)))
			continue
		}
		if store.Name == "" {
			store.Name = task.Name
		}
		if store.Mode == "" {
			store.Mode = mcpModeFromTags(task.Tags)
		}
		out = append(out, a.mcpReturnTask(store))
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
		if a.memoryStore != nil && mcpUnsealedSessionHolder(view) {
			if _, err := a.remoteControlKnownTask(view.ProjectPath); err == nil {
				view = mcpProjectSessionHolder(view, a.mcpTaskViewFromStore(view.ProjectPath), a.mcpLiveConfirmationPending(view.ProjectPath))
			}
		}
		out = append(out, a.mcpReturnTask(view))
	}
	return out, nil
}

func mcpCommandChangesModel(opts mcpCallOpts) bool {
	return strings.TrimSpace(opts.Provider) != "" || strings.TrimSpace(opts.Model) != ""
}

func mcpCommandContinuesLiveRound(command string) bool {
	switch mcpCommandHead(command) {
	case "__scope_approval__", "__resolve_critical_confirm__":
		return true
	default:
		return false
	}
}

func mcpStampCommandModel(view mcpTaskView, provider, model string) mcpTaskView {
	view.Provider = provider
	view.Model = model
	view.ProviderRequested = provider
	view.ModelRequested = model
	view.ProviderEffective = provider
	view.ModelEffective = model
	return view
}

func (a *App) mcpResolveCommandSelection(provider, model string) (string, string, error) {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" && model == "" {
		return "", "", nil
	}
	if provider == "" {
		return "", "", errors.New("指定模型时必须同时指定服务商")
	}
	if mcpProviderResolveFunc != nil {
		return mcpProviderResolveFunc(provider, model)
	}
	return a.mcpResolveSelection(provider, model)
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

// mcpRunContext is the lifetime of one MCP prompt. It ends when the prompt
// returns, the user stops it, or a newer command replaces it. It has no
// deadline: wall-clock time does not decide whether the task can finish.
func mcpRunContext() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

func (a *App) mcpStartPrompt(parent context.Context, view mcpTaskView, prompt string, wait, startNew bool, bind func(path string)) (mcpTaskView, error) {
	view = mcpSealRemoteWorkspace(view)
	path := normalizeProjectSessionPath(view.ProjectPath)
	if path == "" {
		return mcpTaskView{}, errors.New("没有创建任务")
	}
	userID := desktopAIAssistantUserIDForProjectPath(path)
	if userID == "" || userID == desktopUserID {
		return mcpTaskView{}, errors.New("没有创建任务")
	}
	// Claim before reading history. A second follow-up then fails closed
	// without opening the conversation store or starting another prompt.
	token, err := a.mcpAcquireTurn(path, startNew)
	if err != nil {
		return mcpTaskView{}, err
	}
	defer a.mcpReleaseTurn(path, token)
	requestID := "mcp-" + randomHexID(8)
	// The session context has no wall-clock deadline. A single model request
	// still dies on its own timeout (DefaultLLMTimeoutSec, at most
	// MaxAgentTimeoutSec). A live command still uses MaxLongRunTimeoutSec.
	// This watchdog only ends the round when host-visible activity stops.
	runCtx, cancel := context.WithCancel(context.Background())
	watch := mcpNewStallWatch(mcpRunStallWindow, func() bool {
		return a.mcpExtendQuietRun(path, requestID, mcpRunStallWindow)
	}, func() {
		a.mcpNoteRunStalled(path, requestID)
		cancel()
	})
	stopStallWatch := watch.stop
	promptFn := mcpProgrammingPromptFunc
	mark := mcpConversationMark{}
	if promptFn == nil {
		mark = a.mcpConversationMark(path)
	}
	reqProvider := strings.TrimSpace(view.ProviderRequested)
	reqModel := strings.TrimSpace(view.ModelRequested)
	if reqProvider == "" && reqModel == "" {
		reqProvider = strings.TrimSpace(view.Provider)
		reqModel = strings.TrimSpace(view.Model)
	}
	effProvider, effModel := reqProvider, reqModel
	if effProvider == "" || effModel == "" {
		if pin, ok := a.mcpPinNames(path); ok {
			if effProvider == "" {
				effProvider = pin.ProviderName
			}
			if effModel == "" {
				effModel = pin.Model
			}
		}
	}
	rec := &mcpRunRecord{
		ProjectPath:       path,
		Name:              view.Name,
		Mode:              view.Mode,
		Status:            mcpStatusRunning,
		Provider:          effProvider,
		Model:             effModel,
		ProviderRequested: reqProvider,
		ModelRequested:    reqModel,
		ProviderEffective: effProvider,
		ModelEffective:    effModel,
		RequestID:         requestID,
		Workspace:         view.workspace,
		RemoteHost:        view.RemoteHost,
		RemoteUser:        view.RemoteUser,
		RemotePort:        view.RemotePort,
		RemoteWorkDir:     view.RemoteWorkDir,
		Location:          view.Location,
		CloudWorkspaceID:  view.CloudWorkspaceID,
		GrantID:           view.GrantID,
		Round:             a.mcpNextRound(path),
		RunID:             mcpNewRunID(),
		StatusVersion:     1,
		StartedAt:         time.Now(),
		Checks:            mcpCloneChecks(view.Checks),
		PlanID:            strings.TrimSpace(view.PlanID),
		PlanVersion:       view.PlanVersion,
		WorkDirRequested:  strings.TrimSpace(firstNonEmpty(view.WorkDirRequested, view.WorkingDir, view.workspace)),
		SessionPath:       strings.TrimSpace(view.SessionPath),
		historyCount:      mark.count,
		historySeal:       mark.text,
		cancel:            cancel,
		progressAt:        time.Now(),
	}
	prevCancel, dismiss, err := a.mcpCommitRun(rec, token, startNew, mcpCallOptsFrom(parent))
	if err != nil {
		stopStallWatch()
		cancel()
		return mcpTaskView{}, err
	}
	if prevCancel != nil {
		prevCancel()
	}
	if dismiss {
		a.mcpDismissPendingConfirmation(path)
	}
	if bind != nil {
		bind(path)
	}
	a.mcpNoteRequestRun(path, rec.RunID)
	// Drop the claim once the run is stored. Waiting for the prompt used to
	// keep it, so the next command was rejected after this run had already
	// finished and the waiter was only packaging the result.
	a.mcpReleaseTurn(path, token)
	if promptFn == nil {
		a.mirrorMCPUser(prompt, requestID, userID, path, view.Name, view.Mode)
	}
	bound := mcpBindActionCommand(prompt)

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer stopStallWatch()
		defer cancel()
		// Recover before cancel and close, so a waiter reads the failed record.
		defer func() {
			if recovered := recover(); recovered != nil {
				a.mcpFinishPromptPanic(path, requestID, recovered)
			}
		}()
		var resp *IMAgentResponse
		var err error
		req := AIAssistantSendRequest{
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
			HostRawText:                 mcpStartUsesRawText(startNew, view.Mode, prompt, bound.Bound),
		}
		cb := AIAssistantExternalCallbacks{
			OnToken: func(delta string) {
				a.mirrorMCPToken(requestID, userID, delta)
				if stored := mcpStoredRunDelta(delta); stored != "" {
					a.mcpAppendRunText(path, requestID, stored)
				}
			},
			OnProgress: func(text string) {
				a.mirrorMCPProgress(requestID, userID, text)
				a.mcpAppendLog(path, requestID, mcpLogEvent{Kind: "progress", Text: text})
			},
			OnToolEvent: func(ev ACPToolEvent) {
				a.mcpRecordToolEvent(path, requestID, ev)
			},
		}
		if promptFn != nil {
			resp, err = promptFn(a, runCtx, req, cb)
		} else {
			resp, err = a.RunAIAssistantProgrammingPrompt(runCtx, req, cb)
		}
		stopStallWatch()
		cause := runCtx.Err()
		if cause != nil && a.mcpRunStalled(path, requestID) {
			cause = errMCPRunStalled
		} else if cause != nil && a.mcpHostWaitEnded(path, requestID) {
			cause = errMCPHostWaitEnded
		}
		status, finalText, errText, keepText, steps := mcpRunOutcomeCause(err, cause, resp)
		steps = a.mcpStampStepFiles(path, steps)
		var kind string
		var commands []string
		status, finalText, kind, commands = mcpConfirmationCallerNotice(status, finalText, resp)
		status, finalText, kind, commands = a.mcpNoteLiveConfirmation(path, status, finalText, kind, commands)
		if resp != nil {
			a.mirrorMCPFinal(requestID, userID, resp)
		}
		// Seal against the saved transcript, not resp.Text. The registry copy
		// can grow a workspace-file suffix after history is already stored.
		// A test prompt does not open the conversation store.
		seal := mark
		if promptFn == nil {
			seal = a.mcpConversationMark(path)
		}
		a.mcpSealConversation(path, requestID, seal)
		a.mcpFinishRunCause(path, requestID, status, finalText, errText, keepText, kind, commands, steps, cause)
	}()

	view.ProjectPath = path
	view.Status = mcpStatusRunning
	view.RequestID = requestID
	view.Round = rec.Round
	view.RunID = rec.RunID
	view.CurrentRunID = rec.RunID
	view.TaskID = path
	view.StatusVersion = rec.StatusVersion
	view.StartedAt = mcpFormatTime(rec.StartedAt)
	view.Provider = rec.Provider
	view.Model = rec.Model
	view.ProviderRequested = rec.ProviderRequested
	view.ModelRequested = rec.ModelRequested
	view.ProviderEffective = rec.ProviderEffective
	view.ModelEffective = rec.ModelEffective
	view.WorkDirRequested = rec.WorkDirRequested
	view.SessionPath = rec.SessionPath
	view.workspace = rec.Workspace
	// This receipt is the new session, not the previous round's answer.
	view.Text = ""
	view.Error = ""
	view.FinishedAt = ""
	view.EndReason = ""
	view.EndDetail = ""
	view.ConfirmationKind = ""
	view.ConfirmationID = ""
	view.Commands = nil
	view.Steps = nil
	view.Stop = nil
	view.Children = nil
	view.ChildTaskID = ""
	if !wait {
		return a.mcpReturnTask(view), nil
	}
	if parent == nil {
		<-done
	} else {
		select {
		case <-done:
		case <-parent.Done():
			// The caller stopped waiting. The prompt keeps its own context.
			return a.mcpPromptResult(path, requestID, view.RunID, view), nil
		}
	}
	return a.mcpPromptResult(path, requestID, view.RunID, view), nil
}

// mcpPromptResult is this start's record after the prompt goroutine finishes,
// or after the caller stops waiting. The receipt captured at start still says
// running. A replacement archives this round and leaves the next run in the
// registry, so the receipt would claim the replaced round is still running.
func (a *App) mcpPromptResult(path, requestID, runID string, receipt mcpTaskView) mcpTaskView {
	if a == nil {
		return receipt
	}
	if finished, ok := a.mcpViewFromRegistry(path); ok && finished.RequestID == requestID {
		return a.mcpReturnTask(finished)
	}
	if archived, ok := a.mcpViewForRun(path, runID); ok && archived.RequestID == requestID {
		return a.mcpReturnTask(archived)
	}
	return a.mcpReturnTask(receipt)
}

// mcpRunStillWaiting is an MCP turn the client can still stop. A
// needs_confirmation finish has already dropped its cancel func, but the
// local card can still resume that prompt.
func mcpRunStillWaiting(status string) bool {
	return status == mcpStatusRunning || status == mcpStatusNeedsConfirmation
}

func (a *App) mcpReplaceRun(rec *mcpRunRecord) {
	cancel, dismiss, _ := a.mcpCommitRun(rec, 0, true, mcpCallOpts{})
	if cancel != nil {
		cancel()
	}
	if dismiss && a != nil && rec != nil {
		a.mcpDismissPendingConfirmation(rec.ProjectPath)
	}
}

// mcpAcquireTurn reserves the right to install the next run for path.
// replaceRunning is set for create, which cancels the turn already in progress.
// A follow-up loses when another follow-up holds the path or a run is already
// in progress, so the two cannot both pass a stale needs_confirmation read.
func (a *App) mcpAcquireTurn(path string, replaceRunning bool) (uint64, error) {
	if a == nil {
		return 0, errMCPTaskRunning
	}
	path = normalizeProjectSessionPath(path)
	if path == "" {
		return 0, errors.New("没有创建任务")
	}
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	if a.mcpTurnHeld == nil {
		a.mcpTurnHeld = map[string]uint64{}
	}
	if a.mcpTurnSeq == nil {
		a.mcpTurnSeq = map[string]uint64{}
	}
	if !replaceRunning {
		if a.mcpTurnHeld[path] != 0 {
			return 0, errMCPTaskRunning
		}
		if rec := a.mcpRuns[path]; rec != nil && rec.Status == mcpStatusRunning {
			return 0, errMCPTaskRunning
		}
	}
	next := a.mcpTurnSeq[path] + 1
	if next == 0 {
		next = 1
	}
	a.mcpTurnSeq[path] = next
	a.mcpTurnHeld[path] = next
	return next, nil
}

func (a *App) mcpReleaseTurn(path string, token uint64) {
	if a == nil || token == 0 {
		return
	}
	path = normalizeProjectSessionPath(path)
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	if a.mcpTurnHeld[path] == token {
		delete(a.mcpTurnHeld, path)
	}
}

// mcpCommitRun installs rec. token 0 is the unconditional store used by tests
// and idle create. A follow-up token installs only while it still owns the
// path, and replaceRunning false leaves a running record in place.
// opts is compared under this lock, before the previous round is archived
// or cancelled. A version that changed after the caller's earlier read does
// not install.
func (a *App) mcpCommitRun(rec *mcpRunRecord, token uint64, replaceRunning bool, opts mcpCallOpts) (context.CancelFunc, bool, error) {
	if a == nil || rec == nil {
		return nil, false, errors.New("没有创建任务")
	}
	path := normalizeProjectSessionPath(rec.ProjectPath)
	rec.ProjectPath = path
	persist := false
	a.mcpRunsMu.Lock()
	defer func() {
		if persist {
			a.mcpPersistRun(path)
		}
	}()
	defer a.mcpRunsMu.Unlock()
	if token != 0 && a.mcpTurnHeld[path] != token {
		return nil, false, errMCPTaskRunning
	}
	prev := a.mcpRuns[path]
	if prev != nil && prev.Status == mcpStatusRunning && !replaceRunning {
		return nil, false, errMCPTaskRunning
	}
	if opts.HasExpected {
		current := mcpTaskView{}
		if prev != nil {
			current.StatusVersion = prev.StatusVersion
			current.RunID = prev.RunID
			current.CurrentRunID = prev.RunID
		}
		if err := mcpRejectStaleVersion(path, current, prev != nil, opts); err != nil {
			return nil, false, err
		}
	}
	if a.mcpRuns == nil {
		a.mcpRuns = map[string]*mcpRunRecord{}
	}
	var cancel context.CancelFunc
	dismiss := false
	if strings.TrimSpace(rec.RunID) == "" {
		rec.RunID = mcpNewRunID()
	}
	if rec.StatusVersion <= 0 {
		rec.StatusVersion = 1
	}
	if prev := a.mcpRuns[path]; prev != nil {
		if mcpRunStillWaiting(prev.Status) && prev.RequestID != rec.RequestID {
			// The card belongs to the turn we are replacing. Clear it only after
			// the registry lock is released, and only from a handler that already
			// exists — building hub infra here can load the user config.
			now := time.Now()
			prev.Status = mcpStatusCancelled
			prev.FinishedAt = now
			prev.EndReason = mcpStatusCancelled
			prev.Stop = mcpStopFor(mcpStatusCancelled, "", prev.Steps, context.Canceled, now)
			mcpTouchLocked(prev)
			dismiss = true
			cancel = prev.cancel
			prev.cancel = nil
		}
		// Archive the round's final state. The clone owns its log slice, so
		// carrying the cursor onto the replacement does not rewrite this round.
		a.mcpArchiveRunLocked(prev)
		// The cursor is the task's, not the round's. A client holding
		// next_cursor must still see events from the replacement run.
		if rec.logSeq < prev.logSeq {
			rec.logSeq = prev.logSeq
		}
		if len(rec.logs) == 0 && len(prev.logs) > 0 {
			rec.logs = append([]mcpLogEvent(nil), prev.logs...)
		}
		// A new round must not reuse an older version. Callers that still
		// hold the previous status_version then conflict instead of matching
		// this round by accident.
		if rec.StatusVersion <= prev.StatusVersion {
			rec.StatusVersion = prev.StatusVersion + 1
		}
	}
	a.mcpRuns[path] = rec
	persist = true
	return cancel, dismiss, nil
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

// mcpStartUsesRawText keeps a remote programming prompt as the caller wrote
// it. The remote agent already knows the SSH directory; a local task-path
// preamble would point that turn at this computer.
func mcpStartUsesRawText(startNew bool, mode, prompt string, bound bool) bool {
	if strings.TrimSpace(mode) == taskRemoteCodingDevTag {
		return true
	}
	return !startNew || bound || mcpCommandSkipsWorkspaceWrap(prompt)
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

func mcpScopeApprovalMatches(taskPath, workspace, approvalPath string, dirs ...string) bool {
	approvalPath = normalizeProjectSessionPath(approvalPath)
	if approvalPath == "" {
		return false
	}
	taskPath = normalizeProjectSessionPath(taskPath)
	if taskPath != "" && strings.EqualFold(taskPath, approvalPath) {
		return true
	}
	candidates := make([]string, 0, 1+len(dirs))
	candidates = append(candidates, workspace)
	candidates = append(candidates, dirs...)
	for _, dir := range candidates {
		dir = normalizeProjectSessionPath(dir)
		if dir != "" && strings.EqualFold(dir, approvalPath) {
			return true
		}
	}
	return false
}

// mcpScopeApprovalMatchesTask matches the task path and every directory the
// record already carries. A path that exists only on a projected desktop
// snapshot is not one of these, so send_command does not treat it as a match.
func mcpScopeApprovalMatchesTask(view mcpTaskView, approvalPath string) bool {
	return mcpScopeApprovalMatches(view.ProjectPath, view.workspace, approvalPath, view.WorkingDir, view.WorkDirRequested, view.WorkingDirEffective)
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

func (a *App) mcpValidateScopeCommand(projectPath string, reg mcpTaskView, inRegistry bool, command string) error {
	id, decision, ok := mcpParseScopeApprovalCommand(command)
	if !ok {
		return errors.New("授权决定无效")
	}
	view, err := a.mcpTaskForGate(projectPath, reg, inRegistry)
	if err != nil {
		return err
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
		return errors.New("确认已过期或已处理")
	}
	if !mcpScopeApprovalMatchesTask(view, pending.ProjectPath) {
		return errors.New("这条授权确认不属于这个任务")
	}
	if !mcpScopeDecisionAllowed(pending.Kind, decision) {
		return errors.New("这个确认没有这个决定")
	}
	return nil
}

func (a *App) mcpResolveScopeCommand(projectPath string, reg mcpTaskView, inRegistry bool, command string) (mcpTaskView, error) {
	if err := a.mcpValidateScopeCommand(projectPath, reg, inRegistry, command); err != nil {
		return mcpTaskView{}, err
	}
	id, decision, _ := mcpParseScopeApprovalCommand(command)
	view, err := a.mcpTaskForGate(projectPath, reg, inRegistry)
	if err != nil {
		return mcpTaskView{}, err
	}
	if !deliverScopeApproval(id, decision) {
		return mcpTaskView{}, errors.New("确认已过期或已处理")
	}
	view.Text = strings.TrimSpace(view.Text + "\n\n已提交授权决定。")
	if view.Status == mcpStatusRunning {
		return a.mcpAnnotateRunningGates(view), nil
	}
	return view, nil
}

func (a *App) mcpValidateCriticalCommand(projectPath string, reg mcpTaskView, inRegistry bool, command string) error {
	id, _, ok := mcpParseCriticalConfirmCommand(command)
	if !ok {
		return errors.New("安全确认命令无效")
	}
	view, err := a.mcpTaskForGate(projectPath, reg, inRegistry)
	if err != nil {
		return err
	}
	userID := desktopAIAssistantUserIDForProjectPath(view.ProjectPath)
	if !a.mcpCriticalConfirmPending(userID, id) {
		return errors.New("确认已过期或已处理")
	}
	if a.mcpReadyIMHandler() == nil {
		return errors.New("确认已过期或已处理")
	}
	return nil
}

func (a *App) mcpResolveCriticalCommand(projectPath string, reg mcpTaskView, inRegistry bool, command string) (mcpTaskView, error) {
	if err := a.mcpValidateCriticalCommand(projectPath, reg, inRegistry, command); err != nil {
		return mcpTaskView{}, err
	}
	id, confirmed, _ := mcpParseCriticalConfirmCommand(command)
	view, err := a.mcpTaskForGate(projectPath, reg, inRegistry)
	if err != nil {
		return mcpTaskView{}, err
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

// mcpRunningGateOverlay is a desktop scope or critical confirmation. It is
// collected from the pending approvals, independent of the task status that
// is about to be published.
type mcpRunningGateOverlay struct {
	Kind     string
	Commands []string
	Extra    string
}

func (a *App) mcpCollectRunningGates(view mcpTaskView) mcpRunningGateOverlay {
	var overlay mcpRunningGateOverlay
	for _, item := range new(App).ListPendingScopeApprovals() {
		if !mcpScopeApprovalMatchesTask(view, item.ProjectPath) {
			continue
		}
		line := strings.TrimSpace(item.Message)
		if line == "" {
			line = strings.TrimSpace(strings.TrimSpace(item.Tool) + " " + strings.TrimSpace(item.Path))
		}
		if line == "" {
			line = "授权确认 " + item.ID
		}
		if line != "" && !mcpGateShownInDecision(view.Text, line) && !strings.Contains(overlay.Extra, line) {
			if overlay.Extra != "" {
				overlay.Extra += "\n\n"
			}
			overlay.Extra += line
		}
		for _, decision := range mcpScopeApprovalDecisions(item.Kind) {
			overlay.Commands = mcpAppendCommand(overlay.Commands, "__scope_approval__ "+item.ID+" "+decision)
		}
		if overlay.Kind == "" {
			overlay.Kind = "scope"
		}
	}
	for _, item := range a.mcpCriticalConfirmPrompts(desktopAIAssistantUserIDForProjectPath(view.ProjectPath)) {
		if item.Prompt != "" && !mcpGateShownInDecision(view.Text, item.Prompt) && !strings.Contains(overlay.Extra, item.Prompt) {
			if overlay.Extra != "" {
				overlay.Extra += "\n\n"
			}
			overlay.Extra += item.Prompt
		}
		overlay.Commands = mcpAppendCommand(overlay.Commands, "__resolve_critical_confirm__ "+item.ID+" confirm")
		overlay.Commands = mcpAppendCommand(overlay.Commands, "__resolve_critical_confirm__ "+item.ID+" reject")
		if overlay.Kind == "" {
			overlay.Kind = "critical"
		}
	}
	return overlay
}

func mcpApplyRunningGateOverlay(view mcpTaskView, overlay mcpRunningGateOverlay, replace bool) mcpTaskView {
	if overlay.Kind == "" {
		return view
	}
	if replace || strings.TrimSpace(view.ConfirmationKind) == "" {
		view.ConfirmationKind = overlay.Kind
	}
	if replace {
		view.Commands = overlay.Commands
	} else {
		for _, command := range overlay.Commands {
			view.Commands = mcpAppendCommand(view.Commands, command)
		}
	}
	if overlay.Extra != "" {
		view.Text = mcpInsertBeforePlanHint(view.Text, overlay.Extra)
	}
	if hint := mcpConfirmationHint(overlay.Kind); hint != "" && !strings.Contains(view.Text, "send_command") {
		view.Text = mcpInsertBeforePlanHint(view.Text, hint)
	}
	return view
}

// mcpInsertBeforePlanHint places a gate in the decision text. The outcome
// keeps that decision and drops the plan body in front of the confirm hint.
// A copy that exists only inside the plan is not already shown.
func mcpInsertBeforePlanHint(text, block string) string {
	block = strings.TrimSpace(block)
	text = strings.TrimSpace(text)
	if block == "" || mcpGateShownInDecision(text, block) {
		return text
	}
	for _, hint := range []string{mcpPlanConfirmHint, mcpPlanReviewHint, mcpPlanDialogueHint} {
		i := strings.Index(text, hint)
		if i < 0 {
			continue
		}
		head := mcpInsertBeforePlanOverlay(strings.TrimSpace(text[:i]), block)
		return strings.TrimSpace(head + "\n\n" + text[i:])
	}
	return strings.TrimSpace(text + "\n\n" + block)
}

// mcpInsertBeforePlanOverlay keeps the gate with the decision. The plan
// overlay stays the trailing body the outcome cut removes.
func mcpInsertBeforePlanOverlay(text, block string) string {
	blocks := mcpTextBlocks(text)
	at := mcpTrailingPlanOverlayStart(blocks)
	if at >= len(blocks) {
		return mcpJoinBlocks(text, block)
	}
	kept := make([]string, 0, len(blocks)+1)
	kept = append(kept, blocks[:at]...)
	kept = append(kept, block)
	kept = append(kept, blocks[at:]...)
	return strings.Join(kept, "\n\n")
}

func (a *App) mcpAnnotateRunningGates(view mcpTaskView) mcpTaskView {
	if view.Status != mcpStatusRunning {
		return view
	}
	return mcpApplyRunningGateOverlay(view, a.mcpCollectRunningGates(view), true)
}

func mcpPlanDecisionCommands() []string {
	return []string{"/plan approve", "/plan skip", "/plan reject"}
}

func mcpWithoutPlanDecisions(commands []string) []string {
	if len(commands) == 0 {
		return commands
	}
	drop := map[string]struct{}{}
	for _, command := range mcpPlanDecisionCommands() {
		drop[command] = struct{}{}
	}
	out := make([]string, 0, len(commands))
	for _, command := range commands {
		if _, ok := drop[command]; ok {
			continue
		}
		out = append(out, command)
	}
	return out
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
	case "review":
		return mcpReviewContinueHint
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
	path := normalizeProjectSessionPath(view.ProjectPath)
	token, err := a.mcpAcquireTurn(path, false)
	if err != nil {
		return mcpTaskView{}, err
	}
	defer a.mcpReleaseTurn(path, token)
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
	view.ConfirmationID = ""
	view.Commands = nil
	view.EndReason = mcpStatusCompleted
	view.EndDetail = ""
	var startedAt time.Time
	if ts, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(view.StartedAt)); parseErr == nil {
		startedAt = ts
	}
	finishedAt := time.Now()
	view.FinishedAt = mcpFormatTime(finishedAt)
	userID := desktopAIAssistantUserIDForProjectPath(view.ProjectPath)
	requestID := "mcp-" + randomHexID(8)
	view.RequestID = requestID
	a.mirrorMCPUser(command, requestID, userID, view.ProjectPath, view.Name, view.Mode)
	rec := &mcpRunRecord{
		ProjectPath:      view.ProjectPath,
		Name:             view.Name,
		Mode:             view.Mode,
		Status:           view.Status,
		Text:             view.Text,
		Provider:         view.Provider,
		Model:            view.Model,
		RequestID:        requestID,
		Workspace:        view.workspace,
		Location:         view.Location,
		CloudWorkspaceID: view.CloudWorkspaceID,
		GrantID:          view.GrantID,
		Round:            view.Round,
		StartedAt:        startedAt,
		FinishedAt:       finishedAt,
		EndReason:        mcpStatusCompleted,
		historyCount:     view.historyCount,
		historySeal:      view.historySeal,
	}
	prevCancel, dismiss, err := a.mcpCommitRun(rec, token, false, mcpCallOptsFrom(ctx))
	if err != nil {
		return mcpTaskView{}, err
	}
	if prevCancel != nil {
		prevCancel()
	}
	if dismiss {
		a.mcpDismissPendingConfirmation(path)
	}
	if strings.TrimSpace(view.RunID) == "" {
		view.RunID = rec.RunID
		view.CurrentRunID = rec.RunID
	}
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

// mcpSyncPendingPlanMarkdown shows the current plan once, immediately before
// the confirm hint. Without the hint the text is left alone so the first
// overlay is still appended. A paragraph that is not a plan stays.
func mcpSyncPendingPlanMarkdown(text, markdown string) (string, bool) {
	text = strings.TrimSpace(text)
	markdown = strings.TrimSpace(markdown)
	if markdown == "" {
		return text, false
	}
	at := strings.Index(text, mcpPlanConfirmHint)
	if at < 0 {
		return text, strings.Contains(text, markdown)
	}
	before := strings.TrimSpace(text[:at])
	after := strings.TrimSpace(text[at+len(mcpPlanConfirmHint):])
	before = mcpDropExactPlanMarkdown(before, markdown, false)
	after = mcpDropExactPlanMarkdown(after, markdown, true)
	// Finish appends the plan after the hint. That tail is the overlay.
	// The paragraphs before the hint are the decision and stay. When the
	// tail is not a plan, the overlay is the plan-shaped suffix in front.
	if mcpStartsWithPlanOverlay(after) {
		after = mcpDropPlanOverlay(after, true)
	} else {
		before = mcpDropPlanOverlay(before, false)
	}
	return mcpJoinBlocks(before, markdown, mcpPlanConfirmHint, after), true
}

func mcpDropExactPlanMarkdown(text, markdown string, leading bool) string {
	text = strings.TrimSpace(text)
	markdown = strings.TrimSpace(markdown)
	if text == "" || markdown == "" {
		return text
	}
	if text == markdown {
		return ""
	}
	if leading {
		prefix := markdown + "\n\n"
		if strings.HasPrefix(text, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(text, prefix))
		}
		return text
	}
	suffix := "\n\n" + markdown
	if strings.HasSuffix(text, suffix) {
		return strings.TrimSpace(strings.TrimSuffix(text, suffix))
	}
	return text
}

func mcpStartsWithPlanOverlay(text string) bool {
	blocks := mcpTextBlocks(text)
	return len(blocks) > 0 && mcpLooksLikePlanMarkdown(blocks[0])
}

func mcpDropPlanOverlay(text string, leading bool) string {
	blocks := mcpTextBlocks(text)
	if leading {
		return strings.Join(blocks[mcpLeadingPlanOverlayEnd(blocks):], "\n\n")
	}
	return strings.Join(blocks[:mcpTrailingPlanOverlayStart(blocks)], "\n\n")
}

// mcpTrailingPlanOverlayStart is the first block of the plan in front of the
// hint. The formatted plan begins at its last goal line and runs through the
// steps, including a goal or description paragraph. A "## " or numbered plan
// has no goal line, so only the plan-shaped suffix is the overlay.
func mcpTrailingPlanOverlayStart(blocks []string) int {
	for i := len(blocks) - 1; i >= 0; i-- {
		if mcpBlockStartsPlanGoal(blocks[i]) {
			return i
		}
	}
	start := len(blocks)
	for i := len(blocks) - 1; i >= 0; i-- {
		if !mcpLooksLikePlanMarkdown(blocks[i]) {
			break
		}
		start = i
	}
	return start
}

// mcpLeadingPlanOverlayEnd drops a plan that finish appended after the hint.
// A trailing paragraph that is not part of a following step stays, so an ask
// that was stored after a heading plan is still there.
func mcpLeadingPlanOverlayEnd(blocks []string) int {
	if len(blocks) == 0 || !mcpLooksLikePlanMarkdown(blocks[0]) {
		return 0
	}
	if mcpBlockStartsPlanGoal(blocks[0]) {
		return len(blocks)
	}
	end := 0
	for end < len(blocks) && mcpLooksLikePlanMarkdown(blocks[end]) {
		end++
	}
	for end < len(blocks) {
		if mcpLooksLikePlanMarkdown(blocks[end]) {
			end++
			continue
		}
		laterPlan := false
		for j := end + 1; j < len(blocks); j++ {
			if mcpLooksLikePlanMarkdown(blocks[j]) {
				laterPlan = true
				break
			}
		}
		if !laterPlan {
			break
		}
		end++
	}
	return end
}

func mcpBlockStartsPlanGoal(block string) bool {
	return strings.HasPrefix(mcpFirstLine(block), "**目标**")
}

func mcpFirstLine(block string) string {
	line := strings.TrimSpace(block)
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	return line
}

func mcpLooksLikePlanMarkdown(block string) bool {
	line := mcpFirstLine(block)
	if line == "" {
		return false
	}
	if strings.HasPrefix(line, "#") || mcpBlockStartsPlanGoal(block) {
		return true
	}
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(line) {
		return false
	}
	if line[i] != '.' && line[i] != ')' {
		return false
	}
	return line[i+1] == ' '
}

func mcpTextBlocks(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	parts := strings.Split(text, "\n\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func mcpJoinBlocks(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, "\n\n")
}

func (a *App) mcpApplyLiveConfirmation(view mcpTaskView) mcpTaskView {
	// A cloud agent and a remote device do not share this computer's plan card.
	if mcpIsCloudAgent(view) || mcpIsRemoteDevice(view) {
		return view
	}
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
	// A background review seals the turn as plan_review before the plan exists.
	// That kind must not stay forever: once the draft is confirmable, the three
	// plan commands replace it. While the review is still running, those
	// commands are withheld so a caller cannot approve a draft that is not ready.
	planKind := kind == "" || kind == mcpConfirmKindPlan || kind == mcpConfirmKindPlanReview || kind == mcpConfirmKindPlanDialogue
	if handler.codingPlanReviewIsLive(userID) {
		commands = mcpWithoutPlanDecisions(commands)
		if planKind {
			kind = mcpConfirmKindPlanReview
			if !strings.Contains(view.Text, mcpPlanReviewHint) {
				extra = mcpPlanReviewHint
			}
		}
	} else if mem := handler.getStickyCodingWorkbenchMemory(userID); handler.codingPlanHasPendingApproval(userID, mem) {
		if planKind {
			kind = mcpConfirmKindPlan
		}
		for _, command := range mcpPlanDecisionCommands() {
			commands = mcpAppendCommand(commands, command)
		}
		// Finish already stored the plan from that moment. Contains keeps a
		// longer previous plan, or appends the new one after the hint, so the
		// caller approves a plan it did not just read. Replace that overlay.
		if md := strings.TrimSpace(mem.PendingPlanMarkdown); md != "" {
			if synced, placed := mcpSyncPendingPlanMarkdown(view.Text, md); placed {
				view.Text = synced
			} else {
				extra = md
			}
		}
		if !strings.Contains(view.Text, "send_command") {
			if extra != "" {
				extra += "\n\n"
			}
			extra += mcpPlanConfirmHint
		}
	} else if mem.PlanDialogue && (kind == "" || kind == mcpConfirmKindPlanReview) {
		kind = mcpConfirmKindPlanDialogue
		if !strings.Contains(view.Text, "send_command") {
			extra = mcpPlanDialogueHint
		}
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
			question := strings.TrimSpace(state.Question)
			if !strings.Contains(view.Text, question) && !strings.Contains(extra, question) {
				if extra != "" {
					extra += "\n\n"
				}
				extra += question
			}
			// The first option often sits inside the question ("用 8080 还是
			// 9090"). That must not hide the options that are still missing.
			shown := view.Text + "\n" + extra
			var missing []string
			for _, opt := range state.Options {
				opt = strings.TrimSpace(opt)
				if opt == "" || strings.Contains(shown, opt) {
					continue
				}
				missing = append(missing, opt)
				shown += "\n" + opt
			}
			if len(missing) > 0 {
				extra = strings.TrimSpace(extra + "\n" + strings.Join(missing, "\n"))
			}
			switch normalizeAskUserInputKind(state.InputType) {
			case askUserInputChoice:
				for _, opt := range state.Options {
					commands = mcpAppendCommand(commands, strings.TrimSpace(opt))
				}
			case askUserInputConfirm:
				commands = mcpAppendCommand(commands, "confirm")
				commands = mcpAppendCommand(commands, "cancel")
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
func mcpRunOutcome(runErr error, cancelled bool, resp *IMAgentResponse) (status, text, errText string, keepText bool, steps []mcpStepView) {
	var cause error
	if cancelled {
		cause = context.Canceled
	}
	return mcpRunOutcomeCause(runErr, cause, resp)
}

func mcpRunOutcomeCause(runErr, cause error, resp *IMAgentResponse) (status, text, errText string, keepText bool, steps []mcpStepView) {
	status = mcpStatusCompleted
	keepText = true
	switch {
	case errors.Is(cause, errMCPRunStalled):
		// A quiet round failed because nothing arrived. That is not a cancel.
		status = mcpStatusFailed
		errText = errMCPRunStalled.Error()
	case errors.Is(cause, errMCPHostWaitEnded):
		status = mcpStatusCancelled
	case errors.Is(cause, context.DeadlineExceeded), errors.Is(cause, context.Canceled):
		status = mcpStatusCancelled
	case runErr != nil:
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
	steps = mcpStepsFromAgent(resp)
	// A failed or unfinished step is not a completed task, even when the
	// assistant text is non-empty and the response error is blank.
	if status == mcpStatusCompleted && len(steps) == 0 && mcpOutcomeBlocksCompletion(text, resp) {
		status = mcpStatusFailed
		steps = mcpStepsFromFinishText(text, resp)
	}
	status, errText, steps = mcpApplyStepGate(status, errText, steps)
	if text != "" {
		keepText = false
	}
	return status, text, errText, keepText, steps
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
	changed := false
	// A follow-up may already hold the install claim and not have stored its
	// run. Leaving that claim in place lets the commit land after this cancel
	// and start the turn that stop just rejected.
	dropClaim := rec == nil
	if rec != nil && mcpRunStillWaiting(rec.Status) {
		now := time.Now()
		rec.Status = mcpStatusCancelled
		rec.FinishedAt = now
		rec.EndReason = mcpStatusCancelled
		rec.EndDetail = ""
		rec.ConfirmationID = ""
		rec.confirmFingerprint = ""
		rec.ConfirmationKind = ""
		rec.Commands = nil
		rec.Stop = mcpStopFor(mcpStatusCancelled, "", rec.Steps, context.Canceled, now)
		mcpTouchLocked(rec)
		cancel = rec.cancel
		rec.cancel = nil
		dropClaim = true
		changed = true
	}
	if dropClaim {
		delete(a.mcpTurnHeld, path)
	}
	a.mcpRunsMu.Unlock()
	if changed {
		a.mcpPersistRun(path)
	}
	if cancel != nil {
		cancel()
	}
}

// mcpProgressExtendsRun reports host-visible activity. A blank line and the
// bare __heartbeat__ keep a transport alive; they are not progress, so they
// must not renew the stall window. A long tool call still counts when it
// emits a real progress line, including "command still running".
func mcpProgressExtendsRun(text string) bool {
	text = strings.TrimSpace(text)
	return text != "" && text != imHeartbeatMsg
}

// mcpNoteRunProgress records host-visible activity on a round that is still
// running. The session watchdog reads progressAt and keeps the round alive
// while activity arrives. A user stop, or a different request, is left alone.
func (a *App) mcpNoteRunProgress(path, requestID string) {
	if a == nil {
		return
	}
	path = normalizeProjectSessionPath(path)
	requestID = strings.TrimSpace(requestID)
	if path == "" || requestID == "" {
		return
	}
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	if rec == nil || rec.RequestID != requestID || rec.Status != mcpStatusRunning {
		return
	}
	rec.progressAt = time.Now()
}

// mcpStallWatch is the round's no-progress timer. Stop waits for a fired
// callback so that callback cannot Reset after the prompt has finished and
// leave a timer with no owner.
type mcpStallWatch struct {
	timer   *time.Timer
	mu      sync.Mutex
	done    chan struct{}
	running bool
	stopped bool
}

func mcpNewStallWatch(window time.Duration, active func() bool, stall func()) *mcpStallWatch {
	w := &mcpStallWatch{}
	var fire func()
	fire = func() {
		// Publish entry before reading stop. AfterFunc.Stop cannot tell a
		// callback that has not started from one that has already finished.
		w.beginFire()
		defer w.finishFire()
		w.mu.Lock()
		stopped := w.stopped
		w.mu.Unlock()
		if stopped {
			return
		}
		// Read activity before the stop decision. A slow progress check
		// must not hide a stop that already won, and it must not be
		// followed by a stall of a round the caller has already finished.
		stillActive := active != nil && active()
		w.mu.Lock()
		stopped = w.stopped
		// Reset only while this callback owns the timer. Reset from another
		// goroutine races with the fired callback, and a false return would
		// drop the only stall check for a round that is still producing
		// activity. A false return here is expected: this callback already
		// owns the expired timer.
		if !stopped && stillActive {
			w.timer.Reset(window)
		}
		w.mu.Unlock()
		if stopped || stillActive || stall == nil {
			return
		}
		stall()
	}
	w.timer = time.AfterFunc(window, fire)
	return w
}

func (w *mcpStallWatch) beginFire() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.running = true
	if w.done == nil {
		w.done = make(chan struct{})
	}
}

func (w *mcpStallWatch) finishFire() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.running = false
	if w.done != nil {
		close(w.done)
		w.done = nil
	}
}

func (w *mcpStallWatch) stop() {
	if w == nil || w.timer == nil {
		return
	}
	w.mu.Lock()
	w.stopped = true
	w.timer.Stop()
	// The stopped flag is published before this snapshot. A callback that
	// has not entered yet sees it and returns. A callback already inside
	// active() has published done, so this wait cannot return early.
	var done chan struct{}
	if w.running {
		done = w.done
	}
	w.mu.Unlock()
	if done != nil {
		<-done
	}
}

// mcpExtendQuietRun reports whether this waiting round still has host-visible
// activity inside window. The stall callback is the only caller, and it must
// not stamp progressAt: that stamp is the next check's only evidence, so
// writing now here would renew a quiet round forever. A missing stamp is not
// activity. A user stop, or a different request, is left alone.
func (a *App) mcpExtendQuietRun(path, requestID string, window time.Duration) bool {
	if a == nil || window <= 0 {
		return false
	}
	path = normalizeProjectSessionPath(path)
	requestID = strings.TrimSpace(requestID)
	if path == "" || requestID == "" {
		return false
	}
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	if rec == nil || rec.RequestID != requestID || rec.Status != mcpStatusRunning {
		return false
	}
	if rec.progressAt.IsZero() {
		return false
	}
	return !rec.progressAt.Before(time.Now().Add(-window))
}

// mcpRunMadeProgress reports whether this round has host-visible activity at
// or after since. A missing stamp is treated as progress: an older record
// must not be failed just because it predates the stall clock.
func (a *App) mcpRunMadeProgress(path, requestID string, since time.Time) bool {
	if a == nil {
		return false
	}
	path = normalizeProjectSessionPath(path)
	requestID = strings.TrimSpace(requestID)
	if path == "" || requestID == "" {
		return false
	}
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	if rec == nil || rec.RequestID != requestID || rec.Status != mcpStatusRunning {
		return false
	}
	if rec.progressAt.IsZero() {
		return true
	}
	return !rec.progressAt.Before(since)
}

// mcpNoteRunStalled records a confirmed lack of host-visible progress. A run
// that is no longer waiting, or a different request, is left alone so a user
// stop that already landed stays a user stop.
func (a *App) mcpNoteRunStalled(path, requestID string) {
	if a == nil {
		return
	}
	path = normalizeProjectSessionPath(path)
	requestID = strings.TrimSpace(requestID)
	if path == "" || requestID == "" {
		return
	}
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	if rec == nil || rec.RequestID != requestID || rec.Status != mcpStatusRunning {
		return
	}
	rec.stalled = true
}

func (a *App) mcpRunStalled(path, requestID string) bool {
	if a == nil {
		return false
	}
	path = normalizeProjectSessionPath(path)
	requestID = strings.TrimSpace(requestID)
	if path == "" || requestID == "" {
		return false
	}
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	return rec != nil && rec.RequestID == requestID && rec.stalled
}

// mcpFinishPromptPanic records that this prompt can no longer continue.
// A user stop or a replacement that already left running is left alone.
func (a *App) mcpFinishPromptPanic(path, requestID string, recovered any) {
	if a == nil || recovered == nil {
		return
	}
	log.Printf("[mcp-host] prompt panic path=%q request_id=%q panic=%v\n%s", path, requestID, recovered, debug.Stack())
	a.mcpFinishRunCause(path, requestID, mcpStatusFailed, "", mcpPromptPanicDetail, false, "", nil, nil, nil)
}

// mcpNoteHostWaitEnded records that this process can no longer keep the run.
// A live prompt does not call it: elapsed time is not a stop. A run that is
// no longer waiting, or a different request, is left alone so a user stop
// that already landed stays a user stop.
func (a *App) mcpNoteHostWaitEnded(path, requestID string) {
	if a == nil {
		return
	}
	path = normalizeProjectSessionPath(path)
	requestID = strings.TrimSpace(requestID)
	if path == "" || requestID == "" {
		return
	}
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	if rec == nil || rec.RequestID != requestID || rec.Status != mcpStatusRunning {
		return
	}
	rec.hostWaitEnded = true
}

func (a *App) mcpHostWaitEnded(path, requestID string) bool {
	if a == nil {
		return false
	}
	path = normalizeProjectSessionPath(path)
	requestID = strings.TrimSpace(requestID)
	if path == "" || requestID == "" {
		return false
	}
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	return rec != nil && rec.RequestID == requestID && rec.hostWaitEnded
}

func (a *App) mcpFinishRun(path, requestID, status, text, errText string, keepText bool, kind string, commands []string, steps []mcpStepView) {
	a.mcpFinishRunCause(path, requestID, status, text, errText, keepText, kind, commands, steps, nil)
}

func (a *App) mcpFinishRunCause(path, requestID, status, text, errText string, keepText bool, kind string, commands []string, steps []mcpStepView, cause error) {
	if a == nil {
		return
	}
	path = normalizeProjectSessionPath(path)
	persist := false
	a.mcpRunsMu.Lock()
	defer func() {
		if persist {
			a.mcpPersistRun(path)
		}
	}()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	if rec == nil || rec.RequestID != requestID || rec.Status != mcpStatusRunning {
		return
	}
	// Merge under the lock so a screenshot taken while the transcript is
	// sealed stays on the step. Completion follows the merged checklist:
	// a retry that only reports the step it reran must not hide another
	// step that is still failed. A new run starts with an empty checklist,
	// so this merge stays inside the round that already holds the steps.
	steps = mcpMergeSteps(rec.Steps, steps)
	status, errText, steps = mcpApplyStepGate(status, errText, steps)
	rec.Status = status
	rec.Error = errText
	rec.FinishedAt = time.Now()
	rec.EndReason = status
	rec.EndDetail = ""
	switch status {
	case mcpStatusFailed, mcpStatusWaitingReview:
		rec.EndDetail = errText
	case mcpStatusNeedsConfirmation:
		rec.EndDetail = kind
	}
	rec.Stop = mcpStopFor(status, errText, steps, cause, rec.FinishedAt)
	versionBefore := rec.StatusVersion
	if strings.TrimSpace(errText) != "" {
		mcpPushLogLocked(rec, mcpLogEvent{Kind: "error", Text: errText, At: mcpFormatTime(rec.FinishedAt)})
	}
	if rec.StatusVersion == versionBefore {
		mcpTouchLocked(rec)
	}
	if !keepText {
		rec.Text = text
	} else if notice := strings.TrimSpace(text); notice != "" && !strings.Contains(rec.Text, notice) {
		// An empty model reply keeps the token stream. A confirmation notice
		// built after that reply is not the stream, and dropping it leaves the
		// caller with no question to answer.
		rec.Text = strings.TrimSpace(rec.Text + "\n\n" + notice)
	}
	rec.ConfirmationKind = kind
	rec.Commands = append([]string(nil), commands...)
	rec.Steps = mcpCloneSteps(steps)
	rec.Children = a.mcpOverlayPendingChildrenLocked(mcpMergeChildren(rec.Children, mcpChildrenFromSteps(steps)))
	if strings.TrimSpace(rec.ChildTaskID) == "" && len(rec.Children) > 0 {
		rec.ChildTaskID = rec.Children[0].ChildTaskID
	}
	rec.ConfirmationID = ""
	rec.confirmFingerprint = ""
	rec.cancel = nil
	persist = true
}

// mcpStoredRunDelta is the piece that belongs in the polled run transcript.
// Reasoning stays on the thinking lane. A rejected-round marker clears the
// desktop bubble and must not be stored: it has no glyph, so the transcript
// would show a hollow square where the draft was dropped.
func mcpStoredRunDelta(delta string) string {
	if delta == "" || strings.HasPrefix(delta, "\x01") || strings.HasPrefix(delta, rejectedRoundContentToken) {
		return ""
	}
	return delta
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
	rec.progressAt = time.Now()
	mcpTouchLocked(rec)
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
	persist := false
	a.mcpRunsMu.Lock()
	defer func() {
		if persist {
			a.mcpPersistRun(path)
		}
	}()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	if rec == nil || rec.RequestID != requestID || rec.Status != mcpStatusRunning {
		return
	}
	// The stored directory is the remote tree scope approvals name. Replacing
	// it with this computer's task folder would hide that approval.
	if rec.Mode == taskRemoteCodingDevTag && strings.TrimSpace(rec.Workspace) != "" {
		return
	}
	if rec.Workspace == workspace {
		return
	}
	rec.Workspace = workspace
	mcpTouchLocked(rec)
	persist = true
}

// mcpSetRunSessionPath records the session directory only when it is a
// different folder from the tool workspace. Matching paths do not bump
// status_version a second time.
func (a *App) mcpSetRunSessionPath(path, requestID, sessionPath string) {
	if a == nil {
		return
	}
	path = normalizeProjectSessionPath(path)
	sessionPath = normalizeProjectSessionPath(sessionPath)
	if path == "" || sessionPath == "" {
		return
	}
	persist := false
	a.mcpRunsMu.Lock()
	defer func() {
		if persist {
			a.mcpPersistRun(path)
		}
	}()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	if rec == nil || rec.RequestID != requestID || rec.Status != mcpStatusRunning {
		return
	}
	if normalizeProjectSessionPath(rec.Workspace) == sessionPath || rec.SessionPath == sessionPath {
		return
	}
	rec.SessionPath = sessionPath
	mcpTouchLocked(rec)
	persist = true
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
	host, user, workDir, port := mcpRemoteMetaFromTags(snap.Tags)
	rawWorkflow := strings.TrimSpace(mcpWorkflowStatus(snap.ActiveWorkflow))
	status := mcpDisplayStatus("", rawWorkflow, text)
	view := mcpTaskView{
		ProjectPath:   path,
		Name:          name,
		Mode:          mcpModeFromTags(snap.Tags),
		Status:        status,
		Text:          text,
		RemoteHost:    host,
		RemoteUser:    user,
		RemotePort:    port,
		RemoteWorkDir: workDir,
		historyCount:  mcpAssistantReplyCount(items),
	}
	if status == mcpStatusBlockedUnknown {
		view.StatusRaw = rawWorkflow
	}
	if status == mcpStatusFailed && mcpTextBlocksCompletion(text) {
		view.Steps = mcpStepsFromFinishText(text, nil)
		view.Error = mcpStepFailureDetail(view.Steps, text, nil)
	}
	return mcpNoteCloudWorkspace(mcpSealRemoteWorkspace(mcpApplyNamedWorkingDir(view, snap.Tags)), snap.Tags)
}

func mcpRemoteMetaFromTags(tags []string) (host, user, workDir string, port int) {
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		switch {
		case strings.HasPrefix(tag, taskRemoteHostTagPrefix):
			host = strings.TrimSpace(strings.TrimPrefix(tag, taskRemoteHostTagPrefix))
		case strings.HasPrefix(tag, taskRemoteUserTagPrefix):
			user = strings.TrimSpace(strings.TrimPrefix(tag, taskRemoteUserTagPrefix))
		case strings.HasPrefix(tag, taskRemoteWorkDirTagPrefix):
			workDir = strings.TrimSpace(strings.TrimPrefix(tag, taskRemoteWorkDirTagPrefix))
		case strings.HasPrefix(tag, taskRemotePortTagPrefix):
			if parsed, err := strconv.Atoi(strings.TrimPrefix(tag, taskRemotePortTagPrefix)); err == nil && parsed > 0 && parsed < 65536 {
				port = parsed
			}
		}
	}
	return host, user, workDir, port
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
	if !ok {
		return corelib.MaclawLLMConfig{}, false
	}
	return a.mcpMaterializeNamedModel(pin.ProviderName, pin.Model)
}

// mcpLiveRunModel is the provider and model stored on the current round.
// A half-filled pair is not a selection. The caller must not hold mcpRunsMu.
func (a *App) mcpLiveRunModel(userID string) (string, string, bool) {
	path := projectPathFromSessionOwnerID(userID)
	if a == nil || path == "" {
		return "", "", false
	}
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	if rec == nil {
		return "", "", false
	}
	provider := strings.TrimSpace(firstNonEmpty(rec.ProviderEffective, rec.Provider))
	model := strings.TrimSpace(firstNonEmpty(rec.ModelEffective, rec.Model))
	if provider == "" || model == "" {
		return "", "", false
	}
	return provider, model, true
}

// mcpLoopModelConfig is the model a user-visible turn runs. send_command
// writes the new pair onto the round and does not update the create-time pin,
// so the live round wins. A complete pair that cannot be materialized does
// not fall through to the pin: that would keep running the model the caller
// just replaced. The pin remains the fallback only when the round has no pair.
func (a *App) mcpLoopModelConfig(userID string) (corelib.MaclawLLMConfig, bool) {
	if provider, model, ok := a.mcpLiveRunModel(userID); ok {
		return a.mcpMaterializeNamedModel(provider, model)
	}
	return a.mcpPinnedLLMConfig(userID)
}

func (a *App) mcpMaterializeNamedModel(provider, model string) (corelib.MaclawLLMConfig, bool) {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if a == nil || provider == "" || model == "" {
		return corelib.MaclawLLMConfig{}, false
	}
	if mcpMaterializeModelFunc != nil {
		return mcpMaterializeModelFunc(a, provider, model)
	}
	cfg, err := a.MaterializeProviderByName(provider)
	if err != nil {
		log.Printf("[mcp-host] model materialize failed provider=%q: %v", provider, err)
		return corelib.MaclawLLMConfig{}, false
	}
	cfg.Model = model
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
		return mcpHoldProjectedOutcome(mcpFillTaskView(store, reg), reg)
	}
	return mcpFillTaskView(reg, store)
}

// mcpHoldProjectedOutcome keeps the run projection when a later assistant
// sentence, or a task-list workflow that is still running, queued, or paused,
// would otherwise replace it. The sentence stays visible. waiting_review and
// a step list that has not passed stay the task outcome. That workflow flag
// is not a new MCP run: a new run is already the registry row.
func mcpHoldProjectedOutcome(view, reg mcpTaskView) mcpTaskView {
	upgrade := view.Status == mcpStatusCompleted || view.Status == mcpStatusIdle || view.Status == "" || view.Status == mcpStatusRunning
	holds := reg.Status == mcpStatusWaitingReview || reg.Status == mcpStatusNeedsConfirmation || reg.Status == mcpStatusCancelled || reg.Status == mcpStatusFailed || reg.Status == mcpStatusBlockedUnknown
	blocked := holds || (upgrade && reg.Status != mcpStatusCompleted && reg.Status != mcpStatusRunning && reg.Status != "" && mcpStepsBlockCompletion(reg.Steps))
	if !blocked {
		return view
	}
	view.Status = reg.Status
	view.Error = reg.Error
	if strings.TrimSpace(reg.EndReason) != "" {
		view.EndReason = reg.EndReason
		view.EndDetail = reg.EndDetail
	}
	if reg.Stop != nil {
		view.Stop = mcpCloneStop(reg.Stop)
	}
	if len(reg.Steps) > 0 {
		view.Steps = mcpCloneSteps(reg.Steps)
	}
	return view
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
	if strings.TrimSpace(primary.RunID) == "" {
		primary.RunID = fallback.RunID
	}
	if strings.TrimSpace(primary.CurrentRunID) == "" {
		primary.CurrentRunID = firstNonEmpty(fallback.CurrentRunID, fallback.RunID)
	}
	if strings.TrimSpace(primary.TaskID) == "" {
		primary.TaskID = firstNonEmpty(fallback.TaskID, fallback.ProjectPath)
	}
	if primary.StatusVersion == 0 {
		primary.StatusVersion = fallback.StatusVersion
	}
	if strings.TrimSpace(primary.PlanID) == "" {
		primary.PlanID = fallback.PlanID
	}
	if primary.PlanVersion == 0 {
		primary.PlanVersion = fallback.PlanVersion
	}
	if strings.TrimSpace(primary.ProviderRequested) == "" {
		primary.ProviderRequested = fallback.ProviderRequested
	}
	if strings.TrimSpace(primary.ModelRequested) == "" {
		primary.ModelRequested = fallback.ModelRequested
	}
	if strings.TrimSpace(primary.ProviderEffective) == "" {
		primary.ProviderEffective = fallback.ProviderEffective
	}
	if strings.TrimSpace(primary.ModelEffective) == "" {
		primary.ModelEffective = fallback.ModelEffective
	}
	if strings.TrimSpace(primary.WorkDirRequested) == "" {
		primary.WorkDirRequested = fallback.WorkDirRequested
	}
	if strings.TrimSpace(primary.WorkingDirEffective) == "" {
		primary.WorkingDirEffective = fallback.WorkingDirEffective
	}
	if strings.TrimSpace(primary.ToolRoot) == "" {
		primary.ToolRoot = fallback.ToolRoot
	}
	if strings.TrimSpace(primary.SessionPath) == "" {
		primary.SessionPath = fallback.SessionPath
	}
	if strings.TrimSpace(primary.StatusRaw) == "" {
		primary.StatusRaw = fallback.StatusRaw
	}
	if primary.Stop == nil {
		primary.Stop = mcpCloneStop(fallback.Stop)
	}
	if strings.TrimSpace(primary.ChildTaskID) == "" {
		primary.ChildTaskID = fallback.ChildTaskID
	}
	if strings.TrimSpace(primary.ParentTaskID) == "" {
		primary.ParentTaskID = fallback.ParentTaskID
	}
	if len(primary.Children) == 0 && len(fallback.Children) > 0 {
		primary.Children = mcpCloneChildren(fallback.Children)
	}
	if strings.TrimSpace(primary.workspace) == "" {
		primary.workspace = fallback.workspace
	}
	if strings.TrimSpace(primary.WorkingDir) == "" {
		primary.WorkingDir = fallback.WorkingDir
	}
	if primary.Round == 0 {
		primary.Round = fallback.Round
	}
	if strings.TrimSpace(primary.StartedAt) == "" {
		primary.StartedAt = fallback.StartedAt
	}
	if strings.TrimSpace(primary.FinishedAt) == "" {
		primary.FinishedAt = fallback.FinishedAt
	}
	if strings.TrimSpace(primary.EndReason) == "" {
		primary.EndReason = fallback.EndReason
		if strings.TrimSpace(primary.EndDetail) == "" {
			primary.EndDetail = fallback.EndDetail
		}
	}
	if strings.TrimSpace(primary.RemoteHost) == "" {
		primary.RemoteHost = fallback.RemoteHost
	}
	if strings.TrimSpace(primary.RemoteUser) == "" {
		primary.RemoteUser = fallback.RemoteUser
	}
	if primary.RemotePort == 0 {
		primary.RemotePort = fallback.RemotePort
	}
	if strings.TrimSpace(primary.RemoteWorkDir) == "" {
		primary.RemoteWorkDir = fallback.RemoteWorkDir
	}
	if strings.TrimSpace(primary.Location) == "" {
		primary.Location = fallback.Location
	}
	if strings.TrimSpace(primary.CloudWorkspaceID) == "" {
		primary.CloudWorkspaceID = fallback.CloudWorkspaceID
	}
	if strings.TrimSpace(primary.GrantID) == "" {
		primary.GrantID = fallback.GrantID
	}
	if len(primary.Steps) == 0 && len(fallback.Steps) > 0 && (primary.Status == mcpStatusFailed || mcpTextBlocksCompletion(primary.Text)) {
		primary.Steps = mcpCloneSteps(fallback.Steps)
	}
	if len(primary.Checks) == 0 && len(fallback.Checks) > 0 {
		primary.Checks = mcpCloneChecks(fallback.Checks)
	}
	return mcpSealRemoteWorkspace(primary)
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

func mcpHasRemoteConnection(args mcpCreateArgs) bool {
	return strings.TrimSpace(args.SSHHost) != "" ||
		strings.TrimSpace(args.SSHUser) != "" ||
		strings.TrimSpace(args.SSHPassword) != "" ||
		strings.TrimSpace(args.RemoteWorkDir) != "" ||
		args.SSHPort != 0
}

func mcpEffectiveCreateMode(args mcpCreateArgs) (string, error) {
	if mcpHasRemoteConnection(args) {
		switch strings.ToLower(strings.TrimSpace(args.Mode)) {
		case "", "remote_coding_dev", "remote_coding", "remote_programming", "remote_code", "remote":
			return taskRemoteCodingDevTag, nil
		case "chat", "conversation":
			return "", errors.New("会话任务不能带 SSH 连接；远程编程请用 remote_coding_dev")
		case "coding_dev", "coding", "code", "programming":
			return "", errors.New("本机编程任务不能带 SSH 连接；远程编程请用 remote_coding_dev")
		}
	}
	return mcpNormalizeTaskMode(args.Mode, args.WorkingDir)
}

func mcpSealRemoteWorkspace(view mcpTaskView) mcpTaskView {
	if strings.TrimSpace(view.Mode) != taskRemoteCodingDevTag {
		return view
	}
	if strings.TrimSpace(view.workspace) != "" {
		return view
	}
	dir := strings.TrimSpace(view.RemoteWorkDir)
	if dir == "" {
		return view
	}
	view.workspace = dir
	if strings.TrimSpace(view.WorkingDir) == "" {
		view.WorkingDir = dir
	}
	return view
}

func mcpViewWithTaskTags(view mcpTaskView, tags []string) mcpTaskView {
	if strings.TrimSpace(view.Mode) == "" && mcpModeFromTags(tags) == taskRemoteCodingDevTag {
		view.Mode = taskRemoteCodingDevTag
	}
	host, user, workDir, port := mcpRemoteMetaFromTags(tags)
	if strings.TrimSpace(view.RemoteHost) == "" {
		view.RemoteHost = host
	}
	if strings.TrimSpace(view.RemoteUser) == "" {
		view.RemoteUser = user
	}
	if view.RemotePort == 0 {
		view.RemotePort = port
	}
	if strings.TrimSpace(view.RemoteWorkDir) == "" {
		view.RemoteWorkDir = workDir
	}
	return mcpNoteCloudWorkspace(mcpSealRemoteWorkspace(mcpApplyNamedWorkingDir(view, tags)), tags)
}

// mcpApplyNamedWorkingDir copies the task's working_dir tag onto a local view
// that does not already name a directory. A remote tree keeps the SSH
// workdir. A task-folder sandbox is not an execution directory.
func mcpApplyNamedWorkingDir(view mcpTaskView, tags []string) mcpTaskView {
	if strings.TrimSpace(view.Mode) == taskRemoteCodingDevTag || strings.TrimSpace(view.RemoteHost) != "" {
		return view
	}
	if strings.TrimSpace(view.workspace) != "" {
		return view
	}
	dir := normalizeProjectSessionPath(recentTaskWorkingDirFromTags(tags))
	if dir == "" || isLocalCodingIdentityOrSandbox(view.ProjectPath, dir) {
		return view
	}
	view.workspace = dir
	if strings.TrimSpace(view.WorkingDir) == "" {
		view.WorkingDir = dir
	}
	return view
}

func mcpNormalizeLocation(args mcpCreateArgs) (string, error) {
	switch strings.ToLower(strings.TrimSpace(args.Location)) {
	case "":
		// A cloud workspace id is a directory. It does not select a place.
		if strings.TrimSpace(args.CloudWorkspaceID) != "" && strings.TrimSpace(args.GrantID) != "" {
			return "", errors.New("不能同时指定云端工作区和远程设备")
		}
		if strings.TrimSpace(args.GrantID) != "" {
			return mcpLocationRemoteDevice, nil
		}
		return mcpLocationLocal, nil
	case "local", "this", "this_computer", "本地", "本机":
		return mcpLocationLocal, nil
	case "cloud", "cloud_agent", "云端", "云端agent":
		return mcpLocationCloud, nil
	case "remote_device", "device", "machine", "remote_machine", "远程设备":
		return mcpLocationRemoteDevice, nil
	default:
		return "", errors.New("location 只能是 local、cloud 或 remote_device")
	}
}

func mcpValidGrantID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 128 || strings.Contains(id, "..") || strings.ContainsAny(id, "/\\?# \t") {
		return false
	}
	return true
}

// mcpResolveCreateTarget chooses where the task is created and which mode
// that place accepts. SSH programming stays on this computer.
func mcpResolveCreateTarget(args mcpCreateArgs) (string, string, error) {
	if strings.TrimSpace(args.ArgError) != "" {
		return "", "", errors.New(args.ArgError)
	}
	location, err := mcpNormalizeLocation(args)
	if err != nil {
		return "", "", err
	}
	switch location {
	case mcpLocationCloud:
		if mcpHasRemoteConnection(args) {
			return "", "", errors.New("云端 agent 不能带 SSH 连接")
		}
		if strings.TrimSpace(args.GrantID) != "" {
			return "", "", errors.New("云端 agent 不用 grant_id，授权在创建时打开")
		}
		if strings.TrimSpace(args.CloudWorkspaceID) != "" {
			return "", "", errors.New("云端工作区只是本机任务的工作目录，不要和云端 agent 一起传")
		}
		switch strings.ToLower(strings.TrimSpace(args.Mode)) {
		case "", "chat", "conversation":
			return mcpLocationCloud, "chat", nil
		case "coding_dev", "coding", "code", "programming":
			return mcpLocationCloud, taskCodingDevTag, nil
		case "remote_coding_dev", "remote_coding", "remote_programming", "remote_code", "remote":
			return "", "", errors.New("云端 agent 不能带远程编程")
		default:
			return "", "", errors.New("云端 agent 的 mode 只能是 chat 或 coding_dev")
		}
	case mcpLocationRemoteDevice:
		if mcpHasRemoteConnection(args) {
			return "", "", errors.New("远程设备任务不能带 SSH 连接")
		}
		if strings.TrimSpace(args.CloudWorkspaceID) != "" {
			return "", "", errors.New("远程设备任务不能指定云端工作区")
		}
		grantID := strings.TrimSpace(args.GrantID)
		if grantID == "" {
			return "", "", errors.New("远程设备任务需要 grant_id")
		}
		if !mcpValidGrantID(grantID) {
			return "", "", errors.New("grant_id 无效")
		}
		switch strings.ToLower(strings.TrimSpace(args.Mode)) {
		case "", "chat", "conversation":
			return mcpLocationRemoteDevice, "chat", nil
		case "coding_dev", "coding", "code", "programming":
			return mcpLocationRemoteDevice, taskCodingDevTag, nil
		default:
			return "", "", errors.New("远程设备上的 mode 只能是 chat 或 coding_dev")
		}
	default:
		if strings.TrimSpace(args.GrantID) != "" {
			return "", "", errors.New("本机任务不能带 grant_id；远程设备请把 location 设为 remote_device")
		}
		if id := strings.TrimSpace(args.CloudWorkspaceID); id != "" {
			if mcpHasRemoteConnection(args) {
				return "", "", errors.New("云端工作区不能带 SSH 连接")
			}
			if strings.TrimSpace(args.WorkingDir) != "" {
				return "", "", errors.New("工作目录只能指定一个")
			}
			if !validCloudWorkspaceCacheID(id) {
				return "", "", errors.New("cloud_workspace_id 无效")
			}
			switch strings.ToLower(strings.TrimSpace(args.Mode)) {
			case "", "chat", "conversation":
				return mcpLocationLocal, "chat", nil
			case "coding_dev", "coding", "code", "programming":
				return "", "", errors.New("云端工作区只作为会话的工作目录，编程任务请用本机绝对路径")
			case "remote_coding_dev", "remote_coding", "remote_programming", "remote_code", "remote":
				return "", "", errors.New("云端工作区不能带远程编程")
			default:
				return "", "", errors.New("使用云端工作区时 mode 只能是 chat")
			}
		}
		mode, err := mcpEffectiveCreateMode(args)
		if err != nil {
			return "", "", err
		}
		return mcpLocationLocal, mode, nil
	}
}

func mcpEnsureLocation(view mcpTaskView) mcpTaskView {
	switch strings.TrimSpace(view.Location) {
	case mcpLocationLocal, mcpLocationCloud, mcpLocationRemoteDevice:
		return view
	}
	if strings.TrimSpace(view.GrantID) != "" {
		view.Location = mcpLocationRemoteDevice
		return view
	}
	view.Location = mcpLocationLocal
	return view
}

// mcpNoteCloudWorkspace records a cloud workspace tag as the task's directory.
// The tag is not a cloud agent and does not change location.
func mcpNoteCloudWorkspace(view mcpTaskView, tags []string) mcpTaskView {
	if strings.TrimSpace(view.Location) == mcpLocationCloud || strings.TrimSpace(view.Location) == mcpLocationRemoteDevice {
		return view
	}
	if strings.TrimSpace(view.CloudWorkspaceID) != "" {
		return view
	}
	if id := cloudWorkspaceIDFromTags(tags); id != "" {
		view.CloudWorkspaceID = id
	}
	return view
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
	case "remote_coding_dev", "remote_coding", "remote_programming", "remote_code", "remote":
		return taskRemoteCodingDevTag, nil
	default:
		return "", errors.New("mode 只能是 chat、coding_dev 或 remote_coding_dev")
	}
}

func mcpDisplayStatus(registryStatus, workflowStatus, lastAssistant string) string {
	switch strings.TrimSpace(registryStatus) {
	case mcpStatusRunning, mcpStatusCancelled, mcpStatusFailed, mcpStatusCompleted, mcpStatusNeedsConfirmation, mcpStatusWaitingReview, mcpStatusIdle, mcpStatusBlockedUnknown:
		return strings.TrimSpace(registryStatus)
	}
	if strings.TrimSpace(registryStatus) != "" {
		return mcpStatusBlockedUnknown
	}
	if mcpTextAsksConfirmation(lastAssistant) {
		return mcpStatusNeedsConfirmation
	}
	switch strings.ToLower(strings.TrimSpace(workflowStatus)) {
	case "running", "queued", "paused":
		return mcpStatusRunning
	case "failed":
		return mcpStatusFailed
	case "completed", "done":
		if mcpTextAsksConfirmation(lastAssistant) {
			return mcpStatusNeedsConfirmation
		}
		if mcpTextBlocksCompletion(lastAssistant) {
			return mcpStatusFailed
		}
		return mcpStatusCompleted
	}
	if strings.TrimSpace(workflowStatus) != "" {
		return mcpStatusBlockedUnknown
	}
	if mcpTextBlocksCompletion(lastAssistant) {
		return mcpStatusFailed
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
	hasCoding := false
	for _, tag := range tags {
		switch strings.TrimSpace(tag) {
		case taskRemoteCodingDevTag:
			return taskRemoteCodingDevTag
		case taskCodingDevTag:
			hasCoding = true
		}
	}
	if hasCoding {
		return taskCodingDevTag
	}
	return "chat"
}

// mcpRemoteHistoryIncomplete is the other computer's failed turn. The last
// visible assistant carries result_status incomplete. A later person line
// is a new round and is not failed.
func mcpRemoteHistoryIncomplete(items []ProjectConversationHistoryItem) bool {
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if item.hostContinuation || item.planningEcho {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(item.Role))
		if role == "" {
			continue
		}
		if role != "assistant" {
			return false
		}
		return strings.EqualFold(strings.TrimSpace(item.ResultStatus), "incomplete")
	}
	return false
}

// mcpTranscriptAwaitingReply is true when the other computer has a new
// person line and has not answered it yet.
func mcpTranscriptAwaitingReply(items []ProjectConversationHistoryItem) bool {
	last := ""
	for _, item := range items {
		if item.hostContinuation || item.planningEcho {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(item.Role))
		if role == "" {
			continue
		}
		last = role
	}
	return last == "user"
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
