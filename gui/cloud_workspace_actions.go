package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

var (
	cloudWorkspaceDialogMu sync.Mutex
	cloudWorkspaceTaskByID = map[string]ProjectSearchResult{}
)

func resetCloudWorkspaceDialogMocks() {
	cloudWorkspaceBackgroundDisabled = true
	cloudWorkspaceConfirmStealFn = nil
	cloudWorkspaceConfirmDiscardDirtyFn = nil
	resetCloudWorkspaceMounts()
	cloudWorkspaceRestoreGen.Store(0)
	cloudWorkspaceDialogMu.Lock()
	defer cloudWorkspaceDialogMu.Unlock()
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

func lookupCloudWorkspaceTaskByPath(projectPath string) (ProjectSearchResult, bool) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return ProjectSearchResult{}, false
	}
	cloudWorkspaceDialogMu.Lock()
	defer cloudWorkspaceDialogMu.Unlock()
	for _, result := range cloudWorkspaceTaskByID {
		if normalizeProjectSessionPath(result.ProjectPath) == projectPath {
			return result, true
		}
	}
	return ProjectSearchResult{}, false
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
	case "CLOUD_WORKSPACE_LEASE_REQUIRED":
		return fmt.Errorf("云端工作区租约无效，请重新打开")
	case "CLOUD_WORKSPACE_REVISION_CONFLICT":
		return fmt.Errorf("云端工作区版本冲突，请重试")
	case "CLOUD_WORKSPACE_VOLUME_FULL":
		return fmt.Errorf("云端工作区存储空间不足")
	case "CLOUD_WORKSPACE_SIZE":
		return fmt.Errorf("已超过单个云端工作区容量")
	case "CLOUD_WORKSPACE_TENANT_DISK":
		return fmt.Errorf("已超过租户云端工作区总容量")
	case "CLOUD_WORKSPACE_IN_USE":
		return fmt.Errorf("云端工作区占用中（其他设备）")
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

func (a *App) stampCloudWorkspaceDeleted(out CloudWorkspaceDeletedWorkspace) CloudWorkspaceDeletedWorkspace {
	if out.DeletedAt == "" {
		out.DeletedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if out.PurgeAfter == "" {
		if t, parseErr := time.Parse(time.RFC3339, out.DeletedAt); parseErr == nil {
			out.PurgeAfter = t.Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339)
		}
	}
	return out
}

// deleteCloudWorkspaceOnHub DELETE /api/v1/cloud-workspaces/{id} without touching the local task row.
// Callers that also delete/hide the local record must not go through DeleteCloudWorkspace, which emits close before delete.
// Does not bump the restore generation: that would abort in-flight RestoreCloudWorkspaceTasks
// for other workspaces. The local dismiss tombstone already skips the deleted id.
func (a *App) deleteCloudWorkspaceOnHub(id string) (CloudWorkspaceDeletedWorkspace, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return CloudWorkspaceDeletedWorkspace{}, fmt.Errorf("workspace id is required")
	}
	ctx, cancel := a.cloudWorkspaceRequestContext()
	defer cancel()
	data, status, err := a.cloudWorkspaceHubRequest(ctx, http.MethodDelete, cloudWorkspaceItemPath(id), nil)
	if err != nil {
		return CloudWorkspaceDeletedWorkspace{}, err
	}
	if status == http.StatusNotFound {
		resetCloudWorkspaceEntitlementCache()
		return a.stampCloudWorkspaceDeleted(CloudWorkspaceDeletedWorkspace{ID: id}), nil
	}
	if status >= 300 {
		return CloudWorkspaceDeletedWorkspace{}, cloudWorkspaceAPIError(status, data)
	}
	row, decodeErr := decodeCloudWorkspaceHubRow(data)
	if decodeErr != nil {
		return CloudWorkspaceDeletedWorkspace{}, decodeErr
	}
	resetCloudWorkspaceEntitlementCache()
	return a.stampCloudWorkspaceDeleted(row.toDeleted()), nil
}

// DeleteCloudWorkspace DELETE /api/v1/cloud-workspaces/{id} (7-day restore window).
func (a *App) DeleteCloudWorkspace(id string) (CloudWorkspaceDeletedWorkspace, error) {
	out, err := a.deleteCloudWorkspaceOnHub(id)
	if err != nil {
		return CloudWorkspaceDeletedWorkspace{}, err
	}
	// Hide the local row without a tombstone so a 7-day workspace restore can unhide it.
	a.hideLocalCloudWorkspaceTask(id, false)
	return out, nil
}

// ForceDeleteCloudWorkspace permanently removes a previously deleted workspace and its remote files.
func (a *App) ForceDeleteCloudWorkspace(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("workspace id is required")
	}
	ctx, cancel := a.cloudWorkspaceRequestContext()
	defer cancel()
	// Stop any local mount and flush its pending changes before the remote
	// object store is purged. This also prevents a background sync from racing
	// the permanent delete request.
	if task, ok := lookupCloudWorkspaceTask(id); ok {
		a.releaseCloudWorkspaceForProjectPath(task.ProjectPath)
	}
	data, status, err := a.cloudWorkspaceHubRequest(ctx, http.MethodDelete, cloudWorkspaceItemPath(id)+"/purge", nil)
	if err != nil {
		return err
	}
	if status >= 300 {
		return cloudWorkspaceAPIError(status, data)
	}
	resetCloudWorkspaceEntitlementCache()
	a.hideLocalCloudWorkspaceTask(id, true)
	localPath := a.cloudWorkspaceCachePath(a.cloudWorkspaceTenantID(), id)
	if a.isAppCloudWorkspaceCachePath(localPath) {
		if err := os.RemoveAll(localPath); err != nil {
			return fmt.Errorf("remove local cloud workspace cache: %w", err)
		}
	}
	return nil
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
	bumpCloudWorkspaceRestoreGen()
	a.clearDismissedCloudWorkspaceTask(id)
	resetCloudWorkspaceEntitlementCache()
	return row.toActive(), nil
}

func (a *App) cloudWorkspaceTaskIsResumable(result ProjectSearchResult) bool {
	path := normalizeProjectSessionPath(result.ProjectPath)
	if path == "" {
		return false
	}
	if _, err := os.Stat(filepath.Clean(path)); err != nil {
		return false
	}
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return true
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return true
	}
	return !pi.IsHidden(path) && !pi.IsArchived(path)
}

func (a *App) findLocalCloudWorkspaceTask(workspaceID string, includeHidden bool) ProjectSearchResult {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return ProjectSearchResult{}
	}
	if result, ok := lookupCloudWorkspaceTask(workspaceID); ok {
		path := normalizeProjectSessionPath(result.ProjectPath)
		if path != "" {
			_, statErr := os.Stat(filepath.Clean(path))
			inIndex := false
			a.ensureMemoryStore()
			if a.memoryStore != nil {
				if pi := a.memoryStore.ProjectIndex(); pi != nil {
					inIndex = pi.Get(path) != nil
				}
			}
			if statErr == nil || inIndex {
				if includeHidden || a.cloudWorkspaceTaskIsResumable(result) {
					return result
				}
			}
		}
	}
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return ProjectSearchResult{}
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return ProjectSearchResult{}
	}
	want := cloudWorkspaceTag(workspaceID)
	hidden := ProjectSearchResult{}
	for _, rec := range pi.ListAllMatching(func(candidate memory.ProjectRecord) bool {
		return projectRecordHasTag(candidate, taskManagementTag) && projectRecordHasTag(candidate, want)
	}) {
		result := projectRecordToSearchResult(pi, rec)
		if pi.IsArchived(rec.ProjectPath) {
			continue
		}
		if pi.IsHidden(rec.ProjectPath) {
			if includeHidden && strings.TrimSpace(hidden.ProjectPath) == "" {
				hidden = result
			}
			continue
		}
		return result
	}
	return hidden
}

func (a *App) findVisibleCloudWorkspaceTask(workspaceID string) ProjectSearchResult {
	return a.findLocalCloudWorkspaceTask(workspaceID, false)
}

func (a *App) bindPreparedCloudWorkspaceTask(workspaceID string, result ProjectSearchResult, localPath string) ProjectSearchResult {
	localPath = normalizeProjectSessionPath(localPath)
	if localPath != "" {
		result.WorkingDir = localPath
	}
	rememberCloudWorkspaceTask(workspaceID, result)
	rememberCloudWorkspaceLocalPath(localPath, workspaceID)
	return result
}

// CreateTaskWithCloudWorkspace prepares the cache mount then creates a task tagged cloud_workspace:{id}.
// workingDir is ignored: PrepareCloudWorkspace returns LocalPath as the working directory.
func (a *App) CreateTaskWithCloudWorkspace(name, workingDir, mode, workspaceID string) (ProjectSearchResult, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return ProjectSearchResult{}, fmt.Errorf("workspace id is required")
	}
	prepared, err := a.PrepareCloudWorkspace(workspaceID)
	if err != nil {
		log.Printf("[cloud_workspace] prepare failed id=%s err=%v", workspaceID, err)
		return ProjectSearchResult{}, err
	}
	if strings.TrimSpace(prepared.LocalPath) == "" {
		return ProjectSearchResult{}, fmt.Errorf("cloud workspace cache path is empty")
	}
	if existing := a.findVisibleCloudWorkspaceTask(workspaceID); strings.TrimSpace(existing.ProjectPath) != "" {
		bound := a.bindPreparedCloudWorkspaceTask(workspaceID, existing, prepared.LocalPath)
		a.applyCloudWorkspaceSidecars(workspaceID, bound.ProjectPath)
		return bound, nil
	}
	name, mode = a.cloudWorkspaceTaskIdentity(workspaceID, name, mode)
	taskName := normalizeRecentTaskName(name)
	if taskName == "" {
		return ProjectSearchResult{}, fmt.Errorf("task name is required")
	}
	tags := []string{taskManagementTag, taskUserCreatedTag, cloudWorkspaceTag(workspaceID)}
	if normalized := NormalizeCreateTaskMode(mode); normalized != "" {
		tags = append(tags, normalized)
	}
	result := a.createTaskRecordWithWorkingDir(taskName, "", tags, prepared.LocalPath, false)
	if strings.TrimSpace(result.ProjectPath) != "" {
		a.clearDismissedCloudWorkspaceTask(workspaceID)
		bound := a.bindPreparedCloudWorkspaceTask(workspaceID, result, prepared.LocalPath)
		a.applyCloudWorkspaceSidecars(workspaceID, bound.ProjectPath)
		a.flushCloudWorkspaceTaskSidecarBestEffort(workspaceID)
		return bound, nil
	}
	return result, fmt.Errorf("创建云端工作区任务失败")
}

// ResumeCloudWorkspaceTask returns the 1:1 task for workspaceID (process map or cloud_workspace: tag)
// after re-running PrepareCloudWorkspace so tab/sidebar reopen holds the exclusive lease.
func (a *App) ResumeCloudWorkspaceTask(workspaceID string) (ProjectSearchResult, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return ProjectSearchResult{}, nil
	}
	existing := a.findVisibleCloudWorkspaceTask(workspaceID)
	if strings.TrimSpace(existing.ProjectPath) == "" {
		return ProjectSearchResult{}, nil
	}
	prepared, err := a.PrepareCloudWorkspace(workspaceID)
	if err != nil {
		log.Printf("[cloud_workspace] resume prepare failed id=%s err=%v", workspaceID, err)
		return ProjectSearchResult{}, err
	}
	if strings.TrimSpace(prepared.LocalPath) == "" {
		return ProjectSearchResult{}, fmt.Errorf("cloud workspace cache path is empty")
	}
	bound := a.bindPreparedCloudWorkspaceTask(workspaceID, existing, prepared.LocalPath)
	a.applyCloudWorkspaceSidecars(workspaceID, bound.ProjectPath)
	return bound, nil
}
