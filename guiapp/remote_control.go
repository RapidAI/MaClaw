package guiapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// RemoteControlState is the task-list snapshot for this desktop.
type RemoteControlState struct {
	MachineID  string                 `json:"machine_id"`
	Machines   []RemoteControlMachine `json:"machines"`
	Grants     []RemoteControlGrant   `json:"grants"`
	Pending    []RemoteControlGrant   `json:"pending"`
	Invites    []RemoteControlGrant   `json:"invites"`
	Controlled []RemoteControlGrant   `json:"controlled"`
}

// RemoteControlMachine is another desktop of the signed-in account.
type RemoteControlMachine struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Online bool   `json:"online"`
}

// RemoteControlGrant is one authorized machine, waiting invite, or incoming control.
type RemoteControlGrant struct {
	ID          string `json:"id"`
	MachineID   string `json:"machine_id,omitempty"`
	MachineName string `json:"machine_name,omitempty"`
	Account     string `json:"account,omitempty"`
	Permission  string `json:"permission"`
	Status      string `json:"status"`
	SameAccount bool   `json:"same_account"`
	Online      bool   `json:"online"`
	FromMachine string `json:"from_machine,omitempty"`
}

// RemoteControlTaskDetail is the read-only view of one task on this desktop.
type RemoteControlTaskDetail struct {
	Task      ProjectSearchResult              `json:"task"`
	Messages  []ProjectConversationHistoryItem `json:"messages"`
	Artifacts []ProjectSearchArtifact          `json:"artifacts"`
}

func (a *App) remoteControlLogin() (hub, viewer, machineID string, err error) {
	hub, viewer, err = a.fileCompanionHubLogin()
	if err != nil {
		return "", "", "", err
	}
	cfg, cfgErr := a.LoadConfig()
	if cfgErr != nil {
		return "", "", "", errors.New("请先登录 MaClaw Hub")
	}
	machineID = strings.TrimSpace(cfg.RemoteMachineID)
	if machineID == "" {
		return "", "", "", errors.New("请先登录这台电脑")
	}
	return hub, viewer, machineID, nil
}

func (a *App) remoteControlJSON(method, path string, payload any, dest any) error {
	hub, viewer, machineID, err := a.remoteControlLogin()
	if err != nil {
		return err
	}
	var reader io.Reader
	if payload != nil {
		raw, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return marshalErr
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, hub+path, reader)
	if err != nil {
		return err
	}
	fileCompanionShareAuthorize(req, viewer, "")
	req.Header.Set("X-Machine-Id", machineID)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := fileCompanionDirectClient(25 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("暂时无法连接云端")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := fileCompanionShareMessage(body)
		if message == "" {
			message = "远控请求失败"
		}
		return errors.New(message)
	}
	if dest != nil && len(body) > 0 {
		if err := json.Unmarshal(body, dest); err != nil {
			return errors.New("远控结果无法读取")
		}
	}
	return nil
}

func emptyRemoteControlState(machineID string) RemoteControlState {
	return RemoteControlState{
		MachineID:  machineID,
		Machines:   []RemoteControlMachine{},
		Grants:     []RemoteControlGrant{},
		Pending:    []RemoteControlGrant{},
		Invites:    []RemoteControlGrant{},
		Controlled: []RemoteControlGrant{},
	}
}

// ListRemoteControlState returns tabs, waiting invites, and machines being controlled.
func (a *App) ListRemoteControlState() (RemoteControlState, error) {
	_, _, machineID, err := a.remoteControlLogin()
	if err != nil {
		return emptyRemoteControlState(""), nil
	}
	var state RemoteControlState
	if err := a.remoteControlJSON(http.MethodGet, "/api/remote-control/state", nil, &state); err != nil {
		return emptyRemoteControlState(machineID), err
	}
	if state.Machines == nil {
		state.Machines = []RemoteControlMachine{}
	}
	if state.Grants == nil {
		state.Grants = []RemoteControlGrant{}
	}
	if state.Pending == nil {
		state.Pending = []RemoteControlGrant{}
	}
	if state.Invites == nil {
		state.Invites = []RemoteControlGrant{}
	}
	if state.Controlled == nil {
		state.Controlled = []RemoteControlGrant{}
	}
	return state, nil
}

// ListRemoteControlMachines lists the other desktops of this account.
func (a *App) ListRemoteControlMachines() ([]RemoteControlMachine, error) {
	var payload struct {
		Machines []RemoteControlMachine `json:"machines"`
	}
	if err := a.remoteControlJSON(http.MethodGet, "/api/remote-control/machines", nil, &payload); err != nil {
		return nil, err
	}
	if payload.Machines == nil {
		return []RemoteControlMachine{}, nil
	}
	return payload.Machines, nil
}

// GrantRemoteControlSameAccount authorizes another desktop of this account.
func (a *App) GrantRemoteControlSameAccount(machineID, permission string) (RemoteControlGrant, error) {
	var payload struct {
		Grant RemoteControlGrant `json:"grant"`
	}
	err := a.remoteControlJSON(http.MethodPost, "/api/remote-control/same-account", map[string]string{
		"target_machine_id": machineID,
		"permission":        permission,
	}, &payload)
	return payload.Grant, err
}

// InviteRemoteControl asks another account to accept on one of their desktops.
func (a *App) InviteRemoteControl(account, permission string) (RemoteControlGrant, error) {
	var payload struct {
		Grant RemoteControlGrant `json:"grant"`
	}
	err := a.remoteControlJSON(http.MethodPost, "/api/remote-control/invites", map[string]string{
		"account":    account,
		"permission": permission,
	}, &payload)
	return payload.Grant, err
}

// AcceptRemoteControlInvite binds the invitation to this desktop.
func (a *App) AcceptRemoteControlInvite(grantID string) error {
	return a.remoteControlJSON(http.MethodPost, "/api/remote-control/grants/"+strings.TrimSpace(grantID)+"/accept", map[string]string{}, nil)
}

// RejectRemoteControlInvite declines an invitation and tells the inviter.
func (a *App) RejectRemoteControlInvite(grantID string) error {
	return a.remoteControlJSON(http.MethodPost, "/api/remote-control/grants/"+strings.TrimSpace(grantID)+"/reject", map[string]string{}, nil)
}

// IgnoreRemoteControlInvite hides an invitation without telling the inviter.
func (a *App) IgnoreRemoteControlInvite(grantID string) error {
	return a.remoteControlJSON(http.MethodPost, "/api/remote-control/grants/"+strings.TrimSpace(grantID)+"/ignore", map[string]string{}, nil)
}

// RevokeRemoteControl ends a grant this desktop controls or is controlled by.
func (a *App) RevokeRemoteControl(grantID string) error {
	return a.remoteControlJSON(http.MethodPost, "/api/remote-control/grants/"+strings.TrimSpace(grantID)+"/revoke", map[string]string{}, nil)
}

// RemoteControlOp runs one task operation on the authorized desktop.
func (a *App) RemoteControlOp(grantID, op, body string) (string, error) {
	payload := map[string]any{"op": op}
	if strings.TrimSpace(body) != "" {
		payload["body"] = json.RawMessage(body)
	}
	var decoded struct {
		Result json.RawMessage `json:"result"`
	}
	if err := a.remoteControlJSON(http.MethodPost, "/api/remote-control/grants/"+strings.TrimSpace(grantID)+"/op", payload, &decoded); err != nil {
		return "", err
	}
	if len(decoded.Result) == 0 {
		return "{}", nil
	}
	return string(decoded.Result), nil
}

func (a *App) executeRemoteControl(op string, body json.RawMessage) (any, error) {
	if a == nil {
		return nil, errors.New("远控尚未就绪")
	}
	switch op {
	case "list_tasks":
		tasks := a.ListTasks(remoteControlTaskLimit)
		if tasks == nil {
			tasks = []ProjectSearchResult{}
		}
		for i := range tasks {
			// The list only needs identity, status, and folders. Artifact
			// bodies stay on get_task so a long list does not cross the Hub.
			tasks[i].RecentArtifacts = nil
			tasks[i].SourceURLs = nil
		}
		return map[string]any{"tasks": tasks}, nil
	case "get_task":
		return a.remoteControlTaskDetail(body)
	case "create_task":
		var req struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, errors.New("远控请求无效")
		}
		name := strings.TrimSpace(req.Name)
		if name == "" {
			return nil, errors.New("请填写任务名称")
		}
		created := a.CreateTask(name, "")
		if strings.TrimSpace(created.ProjectPath) == "" {
			return nil, errors.New("没有建成这个任务")
		}
		return map[string]any{"task": created}, nil
	case "send_message":
		var req struct {
			ProjectPath string `json:"project_path"`
			Text        string `json:"text"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, errors.New("远控请求无效")
		}
		text := strings.TrimSpace(req.Text)
		path, err := a.remoteControlKnownTask(req.ProjectPath)
		if err != nil {
			return nil, err
		}
		if text == "" {
			return nil, errors.New("请填写要发送的内容")
		}
		if err := remoteControlMessageError(text); err != nil {
			return nil, err
		}
		var resp *IMAgentResponse
		var sendErr error
		func() {
			defer func() {
				if recover() != nil {
					sendErr = errors.New("对方暂时无法发送")
				}
			}()
			resp, sendErr = a.SendAIAssistantMessage(AIAssistantSendRequest{
				Text:          text,
				ProjectPath:   path,
				RemoteControl: true,
			})
		}()
		if sendErr != nil {
			return nil, sendErr
		}
		if resp != nil && strings.TrimSpace(resp.Error) != "" {
			return nil, errors.New(strings.TrimSpace(resp.Error))
		}
		return map[string]any{"accepted": true}, nil
	case "stop_task":
		var stopReq struct {
			ProjectPath string `json:"project_path"`
		}
		if err := json.Unmarshal(body, &stopReq); err != nil {
			return nil, errors.New("远控请求无效")
		}
		path, err := a.remoteControlKnownTask(stopReq.ProjectPath)
		if err != nil {
			return nil, err
		}
		if err := a.stopRemoteControlTask(path); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true}, nil
	case "rename_task":
		var req struct {
			ProjectPath string `json:"project_path"`
			Name        string `json:"name"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, errors.New("远控请求无效")
		}
		path, err := a.remoteControlKnownTask(req.ProjectPath)
		if err != nil {
			return nil, err
		}
		name := a.RenameTask(path, req.Name)
		if name == "" {
			return nil, errors.New("没有找到这个任务")
		}
		return map[string]any{"name": name}, nil
	case "pin_task":
		var req struct {
			ProjectPath string `json:"project_path"`
			Pinned      bool   `json:"pinned"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, errors.New("远控请求无效")
		}
		path, err := a.remoteControlKnownTask(req.ProjectPath)
		if err != nil {
			return nil, err
		}
		a.PinTask(path, req.Pinned)
		a.emitProjectIndexChanged(path)
		return map[string]any{"ok": true}, nil
	default:
		return nil, errors.New("不支持这个远控操作")
	}
}

func (a *App) remoteControlTaskDetail(body json.RawMessage) (RemoteControlTaskDetail, error) {
	var req struct {
		ProjectPath string `json:"project_path"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return RemoteControlTaskDetail{}, errors.New("远控请求无效")
	}
	path, err := a.remoteControlKnownTask(req.ProjectPath)
	if err != nil {
		return RemoteControlTaskDetail{}, err
	}
	task := a.remoteControlTaskSnapshot(path)
	messages := a.LoadProjectConversationHistory(path)
	if len(messages) > 40 {
		messages = messages[len(messages)-40:]
	}
	for i := range messages {
		messages[i].Content = trimRemoteControlText(messages[i].Content, 4000)
		messages[i].ReasoningContent = trimRemoteControlText(messages[i].ReasoningContent, 2000)
	}
	artifacts := append([]ProjectSearchArtifact(nil), task.RecentArtifacts...)
	for i := range artifacts {
		artifacts[i].Preview = trimRemoteControlText(artifacts[i].Preview, 500)
	}
	if artifacts == nil {
		artifacts = []ProjectSearchArtifact{}
	}
	task.RecentArtifacts = nil
	task.SourceURLs = nil
	if messages == nil {
		messages = []ProjectConversationHistoryItem{}
	}
	return RemoteControlTaskDetail{Task: task, Messages: messages, Artifacts: artifacts}, nil
}

// stopRemoteControlTask signals the task to stop and returns without waiting
// for the run to unwind. The Hub call gives up at 20s, while a loop can take
// longer than that to notice. An idle task is already stopped.
func (a *App) stopRemoteControlTask(path string) error {
	owner := projectSessionOwnerID(path)
	a.revokeDesktopCodingTaskRelation(owner)
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil || hubClient.ensureIMHandler() == nil {
		return errors.New("对方暂时无法停止")
	}
	_, err := hubClient.ensureIMHandler().RequestCancelSessionForUser(owner)
	if err == nil || remoteControlStopIsIdle(err) {
		return nil
	}
	return err
}

func remoteControlStopIsIdle(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no active session")
}

// remoteControlTaskSnapshot reads one task. The list endpoint already walked
// the whole sidebar, so opening a conversation must not do that again.
func (a *App) remoteControlTaskSnapshot(path string) ProjectSearchResult {
	fallback := ProjectSearchResult{ID: path, ProjectPath: path, Name: path}
	if a == nil || a.memoryStore == nil || a.memoryStore.ProjectIndex() == nil {
		return fallback
	}
	pi := a.memoryStore.ProjectIndex()
	rec := pi.Get(path)
	if rec == nil {
		return fallback
	}
	result := a.projectRecordToSearchResult(pi, *rec)
	if localCloudWorkspaceID(*rec) != "" || cloudWorkspaceIDFromPathString(result.WorkingDir) != "" {
		result.Tags = scrubCloudWorkspaceIdentityTags(result.Tags)
	}
	scenes := projectSceneMap(a.sceneIndexForTaskList(taskSearchLimit(remoteControlTaskLimit) * 2))
	if scene, ok := a.sceneRecordForProjectPath(rec.ProjectPath, scenes); ok {
		enrichProjectSearchResultWithScene(&result, scene)
	}
	if strings.TrimSpace(result.Name) == "" {
		result.Name = path
	}
	if strings.TrimSpace(result.ProjectPath) == "" {
		result.ProjectPath = path
	}
	return result
}

// remoteControlKnownTask accepts only a task this desktop already shows.
// Rename, pin, send, and stop must not create a record for an arbitrary path.
func (a *App) remoteControlKnownTask(path string) (string, error) {
	path = normalizeProjectSessionPath(path)
	if a == nil || path == "" {
		return "", errors.New("没有找到这个任务")
	}
	a.ensureMemoryStore()
	if a.memoryStore == nil || a.memoryStore.ProjectIndex() == nil {
		return "", errors.New("没有找到这个任务")
	}
	pi := a.memoryStore.ProjectIndex()
	rec := pi.Get(path)
	if rec == nil || !isTaskManagementRecord(*rec) || pi.IsHidden(rec.ProjectPath) || pi.IsArchived(rec.ProjectPath) || omittedFromTaskSidebar(*rec) {
		return "", errors.New("没有找到这个任务")
	}
	return rec.ProjectPath, nil
}

// remoteControlMessageError blocks desktop commands that SendAIAssistantMessage
// would run immediately on the controlled computer. A remote message starts
// that task's conversation only.
func remoteControlMessageError(text string) error {
	if isPassthroughSlashText(text) {
		return errors.New("远控只能发送任务消息，不能在对方电脑上运行本机命令")
	}
	switch classifyAgentViewControlMessage(text).Kind {
	case agentViewControlMessageSubmit, agentViewControlMessageDismiss:
		return errors.New("远控只能发送任务消息，不能操作对方的任务面板")
	default:
		return nil
	}
}

// remoteControlTaskLimit matches the sidebar's ListTasks(1000) so a remote
// tab does not drop tasks that this computer would still show.
const remoteControlTaskLimit = 1000

func trimRemoteControlText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
