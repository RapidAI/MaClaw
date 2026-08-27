package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// PreparedCloudWorkspace is the mock PrepareCloudWorkspace result.
// Real Hub checkout/sync lands in a later PR.
type PreparedCloudWorkspace struct {
	LocalPath   string `json:"local_path"`
	WorkspaceID string `json:"workspace_id"`
}

var (
	cloudWorkspaceDialogMu    sync.Mutex
	cloudWorkspacePrepareDirs = map[string]string{}
	cloudWorkspaceTaskByID    = map[string]ProjectSearchResult{}
)

func resetCloudWorkspaceDialogMocks() {
	cloudWorkspaceDialogMu.Lock()
	defer cloudWorkspaceDialogMu.Unlock()
	cloudWorkspacePrepareDirs = map[string]string{}
	cloudWorkspaceTaskByID = map[string]ProjectSearchResult{}
}

func rememberCloudWorkspaceTask(workspaceID string, result ProjectSearchResult) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" || strings.TrimSpace(result.ProjectPath) == "" {
		return
	}
	cloudWorkspaceDialogMu.Lock()
	defer cloudWorkspaceDialogMu.Unlock()
	cloudWorkspaceTaskByID[workspaceID] = result
}

func lookupCloudWorkspaceTask(workspaceID string) (ProjectSearchResult, bool) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return ProjectSearchResult{}, false
	}
	cloudWorkspaceDialogMu.Lock()
	defer cloudWorkspaceDialogMu.Unlock()
	result, ok := cloudWorkspaceTaskByID[workspaceID]
	return result, ok
}

func forgetCloudWorkspaceTaskByPath(projectPath string) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return
	}
	cloudWorkspaceDialogMu.Lock()
	defer cloudWorkspaceDialogMu.Unlock()
	for id, result := range cloudWorkspaceTaskByID {
		if normalizeProjectSessionPath(result.ProjectPath) == projectPath {
			delete(cloudWorkspaceTaskByID, id)
		}
	}
}

type cloudWorkspaceHubRow struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	UsedBytes  int64  `json:"used_bytes"`
	UpdatedAt  string `json:"updated_at"`
	DeletedAt  string `json:"deleted_at"`
	PurgeAfter string `json:"purge_after"`
}

func (row cloudWorkspaceHubRow) toActive() CloudWorkspaceEntitlementWorkspace {
	return CloudWorkspaceEntitlementWorkspace{
		ID:        row.ID,
		Name:      row.Name,
		UsedBytes: row.UsedBytes,
		UpdatedAt: row.UpdatedAt,
	}
}

func (row cloudWorkspaceHubRow) toDeleted() CloudWorkspaceDeletedWorkspace {
	return CloudWorkspaceDeletedWorkspace{
		ID:         row.ID,
		Name:       row.Name,
		UsedBytes:  row.UsedBytes,
		UpdatedAt:  row.UpdatedAt,
		DeletedAt:  row.DeletedAt,
		PurgeAfter: row.PurgeAfter,
	}
}

func cloudWorkspaceAPIError(status int, data []byte) error {
	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(data, &payload)
	switch payload.Code {
	case "CLOUD_WORKSPACE_QUOTA":
		return fmt.Errorf("已达云端工作区配额")
	case "CLOUD_WORKSPACE_NAME_TAKEN":
		return fmt.Errorf("云端工作区名称已存在")
	case "CLOUD_WORKSPACE_FORBIDDEN":
		return fmt.Errorf("未开通云端工作区")
	case "NOT_FOUND":
		return fmt.Errorf("云端工作区不存在或已超过 7 天恢复期限")
	case "INVALID_INPUT":
		if strings.TrimSpace(payload.Message) != "" {
			return fmt.Errorf("%s", payload.Message)
		}
		return fmt.Errorf("云端工作区名称无效")
	}
	if msg := strings.TrimSpace(payload.Message); msg != "" {
		return fmt.Errorf("%s", msg)
	}
	return fmt.Errorf("hub returned %d", status)
}

func decodeCloudWorkspaceHubRow(data []byte) (cloudWorkspaceHubRow, error) {
	var row cloudWorkspaceHubRow
	if err := json.Unmarshal(data, &row); err != nil {
		return cloudWorkspaceHubRow{}, fmt.Errorf("invalid cloud workspace response: %w", err)
	}
	if strings.TrimSpace(row.ID) == "" {
		return cloudWorkspaceHubRow{}, fmt.Errorf("invalid cloud workspace response: missing id")
	}
	return row, nil
}

func (a *App) cloudWorkspaceMutate(method, path string, body any, okStatus ...int) (cloudWorkspaceHubRow, error) {
	ctx, cancel := a.cloudWorkspaceRequestContext()
	defer cancel()
	data, status, err := a.cloudWorkspaceHubRequest(ctx, method, path, body)
	if err != nil {
		return cloudWorkspaceHubRow{}, err
	}
	allowed := map[int]struct{}{http.StatusOK: {}, http.StatusCreated: {}}
	for _, code := range okStatus {
		allowed[code] = struct{}{}
	}
	if _, ok := allowed[status]; !ok || status >= 300 {
		return cloudWorkspaceHubRow{}, cloudWorkspaceAPIError(status, data)
	}
	return decodeCloudWorkspaceHubRow(data)
}

// CreateCloudWorkspace POST /api/v1/cloud-workspaces. Empty name lets Hub assign 工作区 N.
func (a *App) CreateCloudWorkspace(name string) (CloudWorkspaceEntitlementWorkspace, error) {
	row, err := a.cloudWorkspaceMutate(http.MethodPost, cloudWorkspaceCollectionPath, map[string]string{
		"name": strings.TrimSpace(name),
	}, http.StatusCreated)
	if err != nil {
		return CloudWorkspaceEntitlementWorkspace{}, err
	}
	return row.toActive(), nil
}

// RenameCloudWorkspace PATCH /api/v1/cloud-workspaces/{id}.
func (a *App) RenameCloudWorkspace(id, name string) (CloudWorkspaceEntitlementWorkspace, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return CloudWorkspaceEntitlementWorkspace{}, fmt.Errorf("workspace id is required")
	}
	row, err := a.cloudWorkspaceMutate(http.MethodPatch, cloudWorkspaceItemPath(id), map[string]string{
		"name": strings.TrimSpace(name),
	})
	if err != nil {
		return CloudWorkspaceEntitlementWorkspace{}, err
	}
	return row.toActive(), nil
}

// DeleteCloudWorkspace DELETE /api/v1/cloud-workspaces/{id} (7-day restore window).
func (a *App) DeleteCloudWorkspace(id string) (CloudWorkspaceDeletedWorkspace, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return CloudWorkspaceDeletedWorkspace{}, fmt.Errorf("workspace id is required")
	}
	row, err := a.cloudWorkspaceMutate(http.MethodDelete, cloudWorkspaceItemPath(id), nil)
	if err != nil {
		return CloudWorkspaceDeletedWorkspace{}, err
	}
	out := row.toDeleted()
	if out.DeletedAt == "" {
		out.DeletedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if out.PurgeAfter == "" {
		if t, parseErr := time.Parse(time.RFC3339, out.DeletedAt); parseErr == nil {
			out.PurgeAfter = t.Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339)
		}
	}
	return out, nil
}

// RestoreCloudWorkspace POST /api/v1/cloud-workspaces/{id}/restore.
func (a *App) RestoreCloudWorkspace(id string) (CloudWorkspaceEntitlementWorkspace, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return CloudWorkspaceEntitlementWorkspace{}, fmt.Errorf("workspace id is required")
	}
	row, err := a.cloudWorkspaceMutate(http.MethodPost, cloudWorkspaceRestorePath(id), nil)
	if err != nil {
		return CloudWorkspaceEntitlementWorkspace{}, err
	}
	return row.toActive(), nil
}

// PrepareCloudWorkspace is a mock: returns a process-local temp dir. Real sync is a later PR.
func (a *App) PrepareCloudWorkspace(workspaceID string) (PreparedCloudWorkspace, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return PreparedCloudWorkspace{}, fmt.Errorf("workspace id is required")
	}
	cloudWorkspaceDialogMu.Lock()
	defer cloudWorkspaceDialogMu.Unlock()
	if existing := strings.TrimSpace(cloudWorkspacePrepareDirs[workspaceID]); existing != "" {
		if info, err := os.Stat(existing); err == nil && info.IsDir() {
			return PreparedCloudWorkspace{LocalPath: existing, WorkspaceID: workspaceID}, nil
		}
	}
	dir, err := os.MkdirTemp("", "maclaw-cws-")
	if err != nil {
		return PreparedCloudWorkspace{}, err
	}
	cloudWorkspacePrepareDirs[workspaceID] = dir
	return PreparedCloudWorkspace{LocalPath: dir, WorkspaceID: workspaceID}, nil
}

// CreateTaskWithCloudWorkspace creates a local task so Open works. Tagging/sync is a later PR.
func (a *App) CreateTaskWithCloudWorkspace(name, workingDir, mode, workspaceID string) ProjectSearchResult {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return ProjectSearchResult{}
	}
	dir := strings.TrimSpace(workingDir)
	if prepared, err := a.PrepareCloudWorkspace(workspaceID); err == nil && strings.TrimSpace(prepared.LocalPath) != "" {
		dir = prepared.LocalPath
	}
	result := a.CreateTaskWithMode(name, dir, mode)
	if strings.TrimSpace(result.ProjectPath) != "" {
		rememberCloudWorkspaceTask(workspaceID, result)
	}
	return result
}

// ResumeCloudWorkspaceTask returns the process-local task bound 1:1 to workspaceID, if any.
func (a *App) ResumeCloudWorkspaceTask(workspaceID string) ProjectSearchResult {
	result, ok := lookupCloudWorkspaceTask(workspaceID)
	if !ok || strings.TrimSpace(result.ProjectPath) == "" {
		return ProjectSearchResult{}
	}
	if _, err := os.Stat(filepath.Clean(result.ProjectPath)); err != nil {
		return ProjectSearchResult{}
	}
	return result
}
