package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

type mobileBackendSSHSession struct {
	SessionID         string   `json:"session_id"`
	ServerProfileID   string   `json:"server_profile_id"`
	BackendSessionID  string   `json:"backend_session_id"`
	Status            string   `json:"status"`
	State             string   `json:"state"`
	Message           string   `json:"message"`
	RecentOutput      string   `json:"recent_output"`
	OutputChunk       string   `json:"output_chunk"`
	OutputSeq         int64    `json:"output_seq"`
	PendingInput      []string `json:"pending_input"`
	PendingInputCount int      `json:"pending_input_count"`
	ClaimedBy         string   `json:"claimed_by"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
}

type mobileBackendSSHSessionClaimResponse struct {
	Status  string                   `json:"status"`
	Session *mobileBackendSSHSession `json:"session"`
}

type mobileBackendSSHTask struct {
	TaskID           string `json:"task_id"`
	SessionID        string `json:"session_id"`
	BackendSessionID string `json:"backend_session_id"`
	Action           string `json:"action"`
	Command          string `json:"command"`
	Status           string `json:"status"`
	Message          string `json:"message"`
	LogTail          string `json:"log_tail"`
	ExitCode         *int   `json:"exit_code"`
	TailLines        int    `json:"tail_lines"`
	TimeoutSeconds   int    `json:"timeout"`
	ClaimedBy        string `json:"claimed_by"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

type mobileBackendSSHTaskClaimResponse struct {
	Status string                `json:"status"`
	Task   *mobileBackendSSHTask `json:"task"`
}

type mobileBackendSSHFileOperation struct {
	OperationID      string `json:"operation_id"`
	SessionID        string `json:"session_id"`
	BackendSessionID string `json:"backend_session_id"`
	Action           string `json:"action"`
	LocalPath        string `json:"local_path"`
	RemotePath       string `json:"remote_path"`
	Status           string `json:"status"`
	Message          string `json:"message"`
	BytesTransferred int64  `json:"bytes_transferred"`
	DownloadURL      string `json:"download_url"`
	ClaimedBy        string `json:"claimed_by"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

type mobileBackendSSHFileOperationClaimResponse struct {
	Status    string                         `json:"status"`
	Operation *mobileBackendSSHFileOperation `json:"operation"`
}

func (c *RemoteHubClient) pollMobileBackendSSHSessionsOnce() {
	claim, err := c.claimMobileBackendSSHSession()
	if err != nil {
		log.Printf("[hub-client] mobile backend ssh session claim failed: %v", err)
		return
	}
	if claim == nil || claim.Session == nil || strings.TrimSpace(claim.Session.SessionID) == "" {
		return
	}
	c.processMobileBackendSSHSession(*claim.Session)
}

func (c *RemoteHubClient) claimMobileBackendSSHSession() (*mobileBackendSSHSessionClaimResponse, error) {
	var out mobileBackendSSHSessionClaimResponse
	if err := c.doMobileDigitalEmployeeTaskRequest(context.Background(), http.MethodPost, "/api/mobile/ssh/sessions/claim", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *RemoteHubClient) updateMobileBackendSSHSession(sessionID string, payload map[string]any) (*mobileBackendSSHSession, error) {
	var out mobileBackendSSHSession
	path := "/api/mobile/ssh/sessions/" + url.PathEscape(strings.TrimSpace(sessionID)) + "/worker"
	if err := c.doMobileDigitalEmployeeTaskRequest(context.Background(), http.MethodPatch, path, payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *RemoteHubClient) claimMobileBackendSSHTask() (*mobileBackendSSHTaskClaimResponse, error) {
	var out mobileBackendSSHTaskClaimResponse
	if err := c.doMobileDigitalEmployeeTaskRequest(context.Background(), http.MethodPost, "/api/mobile/ssh/tasks/claim", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *RemoteHubClient) updateMobileBackendSSHTask(taskID string, payload map[string]any) (*mobileBackendSSHTask, error) {
	var out struct {
		Task mobileBackendSSHTask `json:"task"`
	}
	path := "/api/mobile/ssh/tasks/" + url.PathEscape(strings.TrimSpace(taskID)) + "/worker"
	if err := c.doMobileDigitalEmployeeTaskRequest(context.Background(), http.MethodPatch, path, payload, &out); err != nil {
		return nil, err
	}
	return &out.Task, nil
}

func (c *RemoteHubClient) claimMobileBackendSSHFileOperation() (*mobileBackendSSHFileOperationClaimResponse, error) {
	var out mobileBackendSSHFileOperationClaimResponse
	if err := c.doMobileDigitalEmployeeTaskRequest(context.Background(), http.MethodPost, "/api/mobile/ssh/files/claim", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *RemoteHubClient) updateMobileBackendSSHFileOperation(operationID string, payload map[string]any) (*mobileBackendSSHFileOperation, error) {
	var out struct {
		Operation mobileBackendSSHFileOperation `json:"operation"`
	}
	path := "/api/mobile/ssh/files/" + url.PathEscape(strings.TrimSpace(operationID)) + "/worker"
	if err := c.doMobileDigitalEmployeeTaskRequest(context.Background(), http.MethodPatch, path, payload, &out); err != nil {
		return nil, err
	}
	return &out.Operation, nil
}

func (c *RemoteHubClient) pollMobileBackendSSHTasksOnce() {
	claim, err := c.claimMobileBackendSSHTask()
	if err != nil {
		log.Printf("[hub-client] mobile backend ssh task claim failed: %v", err)
		return
	}
	if claim == nil || claim.Task == nil || strings.TrimSpace(claim.Task.TaskID) == "" {
		return
	}
	c.processMobileBackendSSHTask(*claim.Task)
}

func (c *RemoteHubClient) pollMobileBackendSSHFileOperationsOnce() {
	claim, err := c.claimMobileBackendSSHFileOperation()
	if err != nil {
		log.Printf("[hub-client] mobile backend ssh file operation claim failed: %v", err)
		return
	}
	if claim == nil || claim.Operation == nil || strings.TrimSpace(claim.Operation.OperationID) == "" {
		return
	}
	c.processMobileBackendSSHFileOperation(*claim.Operation)
}

func (c *RemoteHubClient) processMobileBackendSSHTask(task mobileBackendSSHTask) {
	taskID := strings.TrimSpace(task.TaskID)
	if taskID == "" {
		return
	}
	handler := c.ensureIMHandler()
	if handler == nil {
		_, _ = c.updateMobileBackendSSHTask(taskID, map[string]any{
			"status":  "failed",
			"message": "MaClaw desktop agent is not available for backend SSH task handling.",
		})
		return
	}
	mgr := handler.ensureSSHManager()
	if mgr == nil || handler.bgTaskMgr == nil {
		_, _ = c.updateMobileBackendSSHTask(taskID, map[string]any{
			"status":  "failed",
			"message": "SSH background task manager is not available.",
		})
		return
	}
	localSessionID := mobileBackendLocalSessionID(task.SessionID, task.BackendSessionID)
	if _, ok := mgr.Get(localSessionID); !ok {
		_, _ = c.updateMobileBackendSSHTask(taskID, map[string]any{
			"status":             "agent_claimed",
			"backend_session_id": localSessionID,
			"message":            "Waiting for MaClaw desktop to attach the backend SSH session before starting this task.",
		})
		return
	}

	status := strings.ToLower(strings.TrimSpace(task.Status))
	ownerID := mobileBackendSSHTaskOwner(task.SessionID)
	switch status {
	case "kill_requested":
		localTaskID, ok := c.mobileBackendSSHCoreTaskID(taskID)
		if !ok {
			_, _ = c.updateMobileBackendSSHTask(taskID, map[string]any{
				"status":  "failed",
				"message": "Mobile backend SSH task mapping was not found on this MaClaw desktop.",
			})
			return
		}
		if err := handler.bgTaskMgr.KillTaskForOwner(localTaskID, ownerID); err != nil {
			_, _ = c.updateMobileBackendSSHTask(taskID, map[string]any{
				"status":  "failed",
				"message": err.Error(),
			})
			return
		}
		result, err := handler.bgTaskMgr.CheckTaskForOwner(localTaskID, mobileBackendSSHTailLines(task.TailLines), ownerID)
		if err != nil {
			_, _ = c.updateMobileBackendSSHTask(taskID, map[string]any{
				"status":  "killed",
				"message": err.Error(),
			})
			return
		}
		_, _ = c.updateMobileBackendSSHTask(taskID, mobileBackendSSHTaskStatusPayload(localSessionID, result, "Task killed by mobile request."))
	case "wait_requested":
		localTaskID, ok := c.mobileBackendSSHCoreTaskID(taskID)
		if !ok {
			_, _ = c.updateMobileBackendSSHTask(taskID, map[string]any{
				"status":  "failed",
				"message": "Mobile backend SSH task mapping was not found on this MaClaw desktop.",
			})
			return
		}
		result, err := waitMobileBackendSSHBackgroundTask(handler.bgTaskMgr, localTaskID, ownerID, task.TimeoutSeconds, mobileBackendSSHTailLines(task.TailLines))
		if err != nil {
			_, _ = c.updateMobileBackendSSHTask(taskID, map[string]any{
				"status":  "failed",
				"message": err.Error(),
			})
			return
		}
		_, _ = c.updateMobileBackendSSHTask(taskID, mobileBackendSSHTaskStatusPayload(localSessionID, result, "Task status refreshed by MaClaw desktop."))
	case "running":
		localTaskID, ok := c.mobileBackendSSHCoreTaskID(taskID)
		if !ok {
			_, _ = c.updateMobileBackendSSHTask(taskID, map[string]any{
				"status":  "failed",
				"message": "Mobile backend SSH task mapping was not found on this MaClaw desktop.",
			})
			return
		}
		result, err := handler.bgTaskMgr.CheckTaskForOwner(localTaskID, mobileBackendSSHTailLines(task.TailLines), ownerID)
		if err != nil {
			_, _ = c.updateMobileBackendSSHTask(taskID, map[string]any{
				"status":  "failed",
				"message": err.Error(),
			})
			return
		}
		_, _ = c.updateMobileBackendSSHTask(taskID, mobileBackendSSHTaskStatusPayload(localSessionID, result, "Task status refreshed by MaClaw desktop."))
	default:
		command := strings.TrimSpace(task.Command)
		if command == "" {
			_, _ = c.updateMobileBackendSSHTask(taskID, map[string]any{
				"status":  "failed",
				"message": "backend SSH background task command is required.",
			})
			return
		}
		_, _ = c.updateMobileBackendSSHTask(taskID, map[string]any{
			"status":             "running",
			"backend_session_id": localSessionID,
			"message":            "Starting backend SSH background task through MaClaw desktop.",
		})
		bgTask, err := handler.bgTaskMgr.SubmitWithOwner(localSessionID, command, "mobile_server_maintenance", ownerID)
		if err != nil {
			_, _ = c.updateMobileBackendSSHTask(taskID, map[string]any{
				"status":             "failed",
				"backend_session_id": localSessionID,
				"message":            err.Error(),
			})
			return
		}
		c.mobileBackendSSHTasks.Store(taskID, bgTask.TaskID)
		result, err := handler.bgTaskMgr.CheckTaskForOwner(bgTask.TaskID, mobileBackendSSHTailLines(task.TailLines), ownerID)
		if err != nil {
			_, _ = c.updateMobileBackendSSHTask(taskID, map[string]any{
				"status":             "running",
				"backend_session_id": localSessionID,
				"message":            fmt.Sprintf("Background task %s started; status check failed: %v", bgTask.TaskID, err),
			})
			return
		}
		_, _ = c.updateMobileBackendSSHTask(taskID, mobileBackendSSHTaskStatusPayload(localSessionID, result, "Backend SSH background task is managed by MaClaw desktop."))
	}
}

func (c *RemoteHubClient) processMobileBackendSSHFileOperation(operation mobileBackendSSHFileOperation) {
	operationID := strings.TrimSpace(operation.OperationID)
	if operationID == "" {
		return
	}
	handler := c.ensureIMHandler()
	if handler == nil {
		_, _ = c.updateMobileBackendSSHFileOperation(operationID, map[string]any{
			"status":  "failed",
			"message": "MaClaw desktop agent is not available for backend SSH file operation handling.",
		})
		return
	}
	mgr := handler.ensureSSHManager()
	if mgr == nil {
		_, _ = c.updateMobileBackendSSHFileOperation(operationID, map[string]any{
			"status":  "failed",
			"message": "SSH session manager is not available.",
		})
		return
	}
	localSessionID := mobileBackendLocalSessionID(operation.SessionID, operation.BackendSessionID)
	if _, ok := mgr.Get(localSessionID); !ok {
		_, _ = c.updateMobileBackendSSHFileOperation(operationID, map[string]any{
			"status":             "agent_claimed",
			"backend_session_id": localSessionID,
			"message":            "Waiting for MaClaw desktop to attach the backend SSH session before running this file operation.",
		})
		return
	}
	_, _ = c.updateMobileBackendSSHFileOperation(operationID, map[string]any{
		"status":             "running",
		"backend_session_id": localSessionID,
		"message":            "Running backend SSH file operation through MaClaw desktop.",
	})

	action := strings.ToLower(strings.TrimSpace(operation.Action))
	var result string
	var err error
	switch action {
	case "upload", "download":
		if strings.TrimSpace(operation.LocalPath) == "" || strings.TrimSpace(operation.RemotePath) == "" {
			err = fmt.Errorf("%s requires both local_path and remote_path", action)
			break
		}
		result, err = mgr.SFTPTransfer(localSessionID, action, operation.LocalPath, operation.RemotePath)
	case "stat", "list":
		result, err = c.runMobileBackendSSHReadOnlyFileCommand(mgr, localSessionID, action, operation.RemotePath)
	default:
		err = fmt.Errorf("unsupported backend SSH file operation %q", operation.Action)
	}
	if err != nil {
		_, _ = c.updateMobileBackendSSHFileOperation(operationID, map[string]any{
			"status":             "failed",
			"backend_session_id": localSessionID,
			"message":            err.Error(),
		})
		return
	}
	_, _ = c.updateMobileBackendSSHFileOperation(operationID, map[string]any{
		"status":             "completed",
		"backend_session_id": localSessionID,
		"local_path":         operation.LocalPath,
		"remote_path":        operation.RemotePath,
		"message":            result,
	})
}

func (c *RemoteHubClient) processMobileBackendSSHSession(session mobileBackendSSHSession) {
	sessionID := strings.TrimSpace(session.SessionID)
	if sessionID == "" {
		return
	}
	cfg, err := c.app.LoadConfig()
	if err != nil {
		_, _ = c.updateMobileBackendSSHSession(sessionID, map[string]any{
			"status":  "failed",
			"state":   "config_error",
			"message": err.Error(),
		})
		return
	}
	host, ok := resolveMobileBackendSSHHost(cfg.SSHHosts, session.ServerProfileID)
	if !ok {
		_, _ = c.updateMobileBackendSSHSession(sessionID, map[string]any{
			"status":  "failed",
			"state":   "profile_not_found",
			"message": fmt.Sprintf("SSH server profile %q is not configured on this MaClaw desktop.", strings.TrimSpace(session.ServerProfileID)),
		})
		return
	}
	handler := c.ensureIMHandler()
	if handler == nil {
		_, _ = c.updateMobileBackendSSHSession(sessionID, map[string]any{
			"status":  "failed",
			"state":   "agent_unavailable",
			"message": "MaClaw desktop agent is not available for backend SSH session handling.",
		})
		return
	}
	mgr := handler.ensureSSHManager()
	if mgr == nil {
		_, _ = c.updateMobileBackendSSHSession(sessionID, map[string]any{
			"status":  "failed",
			"state":   "ssh_manager_unavailable",
			"message": "SSH session manager is not available.",
		})
		return
	}

	localSessionID := strings.TrimSpace(session.BackendSessionID)
	if localSessionID == "" {
		localSessionID = "mobile-ssh:" + sessionID
	}
	status := strings.ToLower(strings.TrimSpace(session.Status))
	if status == "close_requested" {
		mgr.RemoveSession(localSessionID)
		c.mobileBackendSSHOutput.Delete(sessionID)
		_, _ = c.updateMobileBackendSSHSession(sessionID, map[string]any{
			"status":              "closed",
			"state":               "closed",
			"backend_session_id":  localSessionID,
			"message":             "Backend SSH session closed by MaClaw desktop.",
			"clear_pending_input": true,
		})
		return
	}
	if status == "interrupt_requested" {
		if err := mgr.InterruptByID(localSessionID); err != nil {
			_, _ = c.updateMobileBackendSSHSession(sessionID, map[string]any{
				"status":             "failed",
				"state":              "interrupt_failed",
				"backend_session_id": localSessionID,
				"message":            err.Error(),
			})
			return
		}
		time.Sleep(300 * time.Millisecond)
	}

	managed, exists := mgr.Get(localSessionID)
	if !exists || managed == nil {
		_, _ = c.updateMobileBackendSSHSession(sessionID, map[string]any{
			"status":  "connecting",
			"state":   "connecting",
			"message": "MaClaw desktop is creating the backend SSH session.",
		})
		spec := remote.SSHSessionSpec{
			SessionID:  localSessionID,
			HostConfig: mobileBackendSSHHostConfig(host),
			Cols:       120,
			Rows:       40,
		}
		managed, err = mgr.Create(spec)
		if err != nil {
			_, _ = c.updateMobileBackendSSHSession(sessionID, map[string]any{
				"status":  "failed",
				"state":   "connect_failed",
				"message": err.Error(),
			})
			return
		}
		localSessionID = managed.ID
		time.Sleep(500 * time.Millisecond)
	} else if status == "reconnect_requested" {
		if err := mgr.ReconnectByID(localSessionID); err != nil {
			_, _ = c.updateMobileBackendSSHSession(sessionID, map[string]any{
				"status":             "failed",
				"state":              "reconnect_failed",
				"backend_session_id": localSessionID,
				"message":            err.Error(),
			})
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	applied := 0
	for _, input := range session.PendingInput {
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		if _, err := mgr.WriteInputChecked(localSessionID, input); err != nil {
			_, _ = c.updateMobileBackendSSHSession(sessionID, map[string]any{
				"status":              "failed",
				"state":               "input_failed",
				"backend_session_id":  localSessionID,
				"message":             err.Error(),
				"applied_input_count": applied,
			})
			return
		}
		applied++
	}
	preview := ""
	if current, ok := mgr.Get(localSessionID); ok && current != nil {
		preview = strings.Join(current.PreviewTail(20), "\n")
	}
	outputChunk := c.mobileBackendSSHOutputChunk(sessionID, preview)
	update := map[string]any{
		"status":             "connected",
		"state":              "running",
		"backend_session_id": localSessionID,
		"message":            "Backend SSH session is managed by MaClaw desktop.",
		"recent_output":      preview,
	}
	if outputChunk != "" {
		update["output_chunk"] = outputChunk
	}
	if applied > 0 {
		update["applied_input_count"] = applied
	}
	_, _ = c.updateMobileBackendSSHSession(sessionID, update)
}

func (c *RemoteHubClient) mobileBackendSSHOutputChunk(sessionID, preview string) string {
	sessionID = strings.TrimSpace(sessionID)
	if c == nil || sessionID == "" {
		return ""
	}
	if previous, ok := c.mobileBackendSSHOutput.Load(sessionID); ok {
		prev, _ := previous.(string)
		if preview == prev {
			return ""
		}
		c.mobileBackendSSHOutput.Store(sessionID, preview)
		if strings.HasPrefix(preview, prev) {
			return strings.TrimPrefix(preview, prev)
		}
		return preview
	}
	c.mobileBackendSSHOutput.Store(sessionID, preview)
	return preview
}

func mobileBackendLocalSessionID(mobileSessionID, backendSessionID string) string {
	if id := strings.TrimSpace(backendSessionID); id != "" {
		return id
	}
	return "mobile-ssh:" + strings.TrimSpace(mobileSessionID)
}

func mobileBackendSSHTaskOwner(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "mobile"
	}
	return "mobile:" + sessionID
}

func (c *RemoteHubClient) mobileBackendSSHCoreTaskID(mobileTaskID string) (string, bool) {
	if c == nil {
		return "", false
	}
	value, ok := c.mobileBackendSSHTasks.Load(strings.TrimSpace(mobileTaskID))
	if !ok {
		return "", false
	}
	taskID, ok := value.(string)
	taskID = strings.TrimSpace(taskID)
	return taskID, ok && taskID != ""
}

func mobileBackendSSHTailLines(value int) int {
	if value <= 0 {
		return 80
	}
	if value > 200 {
		return 200
	}
	return value
}

func mobileBackendSSHTimeoutSeconds(value int) int {
	if value <= 0 {
		return 60
	}
	if value < 5 {
		return 5
	}
	if value > 600 {
		return 600
	}
	return value
}

func waitMobileBackendSSHBackgroundTask(bgTaskMgr *remote.SSHBackgroundTaskManager, taskID, ownerID string, timeoutSeconds, tailLines int) (*remote.BackgroundTaskStatus, error) {
	if bgTaskMgr == nil {
		return nil, fmt.Errorf("SSH background task manager is not available")
	}
	timeout := mobileBackendSSHTimeoutSeconds(timeoutSeconds)
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	var last *remote.BackgroundTaskStatus
	for {
		result, err := bgTaskMgr.CheckTaskForOwner(taskID, tailLines, ownerID)
		if err != nil {
			return nil, err
		}
		last = result
		if result == nil || !result.Status.IsActive() {
			return result, nil
		}
		if time.Now().Add(5 * time.Second).After(deadline) {
			return last, nil
		}
		time.Sleep(5 * time.Second)
	}
}

func mobileBackendSSHTaskStatusPayload(backendSessionID string, result *remote.BackgroundTaskStatus, message string) map[string]any {
	payload := map[string]any{
		"status":             "unknown",
		"backend_session_id": strings.TrimSpace(backendSessionID),
		"message":            strings.TrimSpace(message),
	}
	if result == nil {
		return payload
	}
	status := result.Status.String()
	if status == "" {
		status = "unknown"
	}
	payload["status"] = status
	payload["log_tail"] = result.LogTail
	if result.ExitCodeKnown {
		payload["exit_code"] = result.ExitCode
	}
	if strings.TrimSpace(message) == "" {
		payload["message"] = fmt.Sprintf("Background task status: %s", status)
	}
	return payload
}

func (c *RemoteHubClient) runMobileBackendSSHReadOnlyFileCommand(mgr *remote.SSHSessionManager, sessionID, action, remotePath string) (string, error) {
	if mgr == nil {
		return "", fmt.Errorf("SSH session manager is not available")
	}
	remotePath = strings.TrimSpace(remotePath)
	if remotePath == "" {
		return "", fmt.Errorf("%s requires remote_path", action)
	}
	session, ok := mgr.Get(sessionID)
	if !ok || session == nil {
		return "", fmt.Errorf("ssh session %s not found", sessionID)
	}
	quoted := mobileBackendShellQuote(remotePath)
	var cmd string
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "stat":
		cmd = "stat " + quoted + " 2>/dev/null || ls -ld " + quoted
	case "list":
		cmd = "ls -la " + quoted
	default:
		return "", fmt.Errorf("unsupported read-only file operation %q", action)
	}
	linesBefore := session.LineCount()
	if _, err := mgr.WriteInputChecked(sessionID, cmd); err != nil {
		return "", err
	}
	lines, _ := mgr.WaitForOutput(sessionID, linesBefore, 10*time.Second)
	return strings.TrimSpace(strings.Join(lines, "\n")), nil
}

func mobileBackendShellQuote(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func resolveMobileBackendSSHHost(hosts []corelib.SSHHostEntry, profileID string) (corelib.SSHHostEntry, bool) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return corelib.SSHHostEntry{}, false
	}
	needle := strings.ToLower(profileID)
	for _, host := range hosts {
		if strings.TrimSpace(host.Host) == "" || strings.TrimSpace(host.User) == "" {
			continue
		}
		cfg := mobileBackendSSHHostConfig(host)
		candidates := []string{
			host.Label,
			host.Host,
			cfg.SSHHostID(),
			fmt.Sprintf("%s@%s", strings.TrimSpace(host.User), strings.TrimSpace(host.Host)),
		}
		for _, candidate := range candidates {
			if strings.ToLower(strings.TrimSpace(candidate)) == needle {
				return host, true
			}
		}
	}
	return corelib.SSHHostEntry{}, false
}

func mobileBackendSSHHostConfig(host corelib.SSHHostEntry) remote.SSHHostConfig {
	cfg := remote.SSHHostConfig{
		Host:       strings.TrimSpace(host.Host),
		Port:       host.Port,
		User:       strings.TrimSpace(host.User),
		AuthMethod: strings.TrimSpace(host.AuthMethod),
		KeyPath:    strings.TrimSpace(host.KeyPath),
		Label:      strings.TrimSpace(host.Label),
	}
	cfg.Defaults()
	return cfg
}
