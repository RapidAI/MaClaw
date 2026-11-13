package guiapp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	// Read one extra byte so a response that does not fit is a clear failure
	// instead of a truncated JSON body reported as an unreadable result.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, remoteControlBodyLimit+1))
	if len(body) > remoteControlBodyLimit {
		return errors.New("远控结果太大")
	}
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

func (a *App) executeRemoteControl(op string, body json.RawMessage, grantIDs ...string) (any, error) {
	if a == nil {
		return nil, errors.New("远控尚未就绪")
	}
	grantID := ""
	if len(grantIDs) > 0 {
		grantID = strings.TrimSpace(grantIDs[0])
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
		detail, err := a.remoteControlTaskDetail(body)
		if err == nil {
			var req struct {
				ProjectPath string `json:"project_path"`
			}
			if json.Unmarshal(body, &req) == nil {
				a.remoteControlTouchWatch(grantID, req.ProjectPath)
			}
		}
		return detail, err
	case "create_task":
		var req struct {
			Name string `json:"name"`
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, errors.New("远控请求无效")
		}
		name := strings.TrimSpace(req.Name)
		if name == "" {
			return nil, errors.New("请填写任务名称")
		}
		// Only local modes make sense on a controlled machine: SSH remote
		// targets can not be configured through remote control.
		var mode string
		switch strings.ToLower(strings.TrimSpace(req.Mode)) {
		case "", "chat":
			mode = ""
		case "coding_dev", "coding", "code", "programming":
			mode = "coding_dev"
		default:
			return nil, errors.New("不支持的任务模式")
		}
		created := a.CreateTaskWithMode(name, "", mode)
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
		a.remoteControlTouchWatch(grantID, path)
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
	case "fetch_file":
		return a.remoteControlTaskFile(body)
	case "watch_task":
		return a.remoteControlWatchTask(grantID, body)
	case "unwatch_task":
		return a.remoteControlUnwatchTask(grantID, body)
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

// remoteControlBodyLimit is the largest Hub response this desktop will read.
const remoteControlBodyLimit = 8 << 20

// remoteControlFetchFileMaxBytes caps one attachment returned to the
// controller. The bytes cross the Hub as base64 inside a JSON result and the
// desktop reader stops at remoteControlBodyLimit, so the raw file must stay
// well below that ceiling.
const remoteControlFetchFileMaxBytes = 4 << 20

// remoteControlTaskFile reads one file this task produced and returns it to
// the controller. The path is accepted only when the task transcript recorded
// it as a delivered attachment, so a remote-control session cannot read
// arbitrary files on this computer.
func (a *App) remoteControlTaskFile(body json.RawMessage) (map[string]any, error) {
	var req struct {
		ProjectPath string `json:"project_path"`
		Path        string `json:"path"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, errors.New("远控请求无效")
	}
	path, err := a.remoteControlKnownTask(req.ProjectPath)
	if err != nil {
		return nil, err
	}
	want := remoteControlNormalizeFilePath(req.Path)
	if want == "" {
		return nil, errors.New("没有找到这个文件")
	}
	var resolved string
	for _, item := range a.LoadProjectConversationHistory(path) {
		for _, produced := range item.LocalFilePaths {
			if remoteControlNormalizeFilePath(produced) == want {
				resolved = produced
				break
			}
		}
		if resolved != "" {
			break
		}
	}
	// A recorded path is always absolute; a relative match would resolve
	// against this process's working directory, so fail closed instead.
	if resolved == "" || !filepath.IsAbs(resolved) {
		return nil, errors.New("没有找到这个文件")
	}
	// Lstat so a recorded path swapped for a symlink (or any special file)
	// after the turn cannot be used to read an unrelated target.
	info, err := os.Lstat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("这个文件已经不在了")
	}
	if info.Size() > remoteControlFetchFileMaxBytes {
		return nil, fmt.Errorf("文件太大（%.1f MB），暂不支持回传", float64(info.Size())/(1<<20))
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, errors.New("这个文件暂时读取失败")
	}
	if len(data) > remoteControlFetchFileMaxBytes {
		return nil, fmt.Errorf("文件太大（%.1f MB），暂不支持回传", float64(len(data))/(1<<20))
	}
	return map[string]any{
		"name":        filepath.Base(resolved),
		"size":        info.Size(),
		"data_base64": base64.StdEncoding.EncodeToString(data),
	}, nil
}

// SaveRemoteControlFile writes one attachment fetched from the controlled
// desktop into a dedicated folder under Downloads and returns the local path.
// It only sees a file name and bytes — the remote path never reaches this
// binding.
func (a *App) SaveRemoteControlFile(name, dataBase64 string) (string, error) {
	name = remoteControlSaveFileName(name)
	if strings.TrimSpace(dataBase64) == "" {
		return "", errors.New("远端没有返回文件内容")
	}
	if len(dataBase64) > remoteControlBodyLimit {
		return "", errors.New("远端文件太大")
	}
	data, err := base64.StdEncoding.DecodeString(dataBase64)
	if err != nil {
		return "", errors.New("远端文件内容无法读取")
	}
	downloads, err := a.GetDownloadsFolder()
	if err != nil || strings.TrimSpace(downloads) == "" {
		return "", errors.New("找不到下载文件夹")
	}
	// A dedicated folder keeps remote files grouped and prevents a same-named
	// download of the user's own from being mistaken for this attachment.
	dir := filepath.Join(downloads, "MaClaw-RemoteFiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", errors.New("找不到下载文件夹")
	}
	target := filepath.Join(dir, name)
	// A byte-identical download opens in place instead of stacking copies.
	if same, _ := remoteControlSameFile(target, data); same {
		return target, nil
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		if _, statErr := os.Stat(target); os.IsNotExist(statErr) {
			break
		}
		target = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
	}
	tmp, err := os.CreateTemp(dir, ".remote-*")
	if err != nil {
		return "", errors.New("保存文件失败")
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", errors.New("保存文件失败")
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", errors.New("保存文件失败")
	}
	if err := os.Rename(tmpPath, target); err != nil {
		os.Remove(tmpPath)
		return "", errors.New("保存文件失败")
	}
	return target, nil
}

// remoteControlSaveFileName reduces a remote-provided name to a safe Windows
// file name: basename only, no reserved characters, never empty.
func remoteControlSaveFileName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		return "remote-file"
	}
	// Windows device names stay reserved with any extension: NUL.txt cannot
	// be created either, so a remote-sent "con.pdf" would fail the rename.
	stem := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	switch stem {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		name = "_" + name
	}
	return name
}

// remoteControlSameFile reports whether target already holds exactly data.
func remoteControlSameFile(target string, data []byte) (bool, error) {
	info, err := os.Stat(target)
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(data)) {
		return false, err
	}
	existing, err := os.ReadFile(target)
	if err != nil {
		return false, err
	}
	return bytes.Equal(existing, data), nil
}

// remoteControlNormalizeFilePath compares a controller-supplied path against
// the recorded attachment paths. Comparisons stay case- and
// separator-insensitive so a Windows record still matches its own string.
func remoteControlNormalizeFilePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return strings.ToLower(filepath.Clean(path))
}

func trimRemoteControlText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
