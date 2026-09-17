package guiapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

var errCloudWorkspaceFenced = errors.New("cloud workspace writer fenced")

// errCloudWorkspaceBandwidthLimited matches the hourly transfer-quota 429 so
// the background sync loop can schedule one window-reset retry instead of a
// hot error loop.
var errCloudWorkspaceBandwidthLimited = errors.New("cloud workspace bandwidth limited")

// cloudWorkspaceBandwidthError carries the server-provided retry window.
type cloudWorkspaceBandwidthError struct {
	retryAfter time.Duration
}

func (e *cloudWorkspaceBandwidthError) Error() string {
	return "云端同步带宽已达本小时限额，窗口重置后自动重试"
}

func (e *cloudWorkspaceBandwidthError) Is(target error) bool {
	return target == errCloudWorkspaceBandwidthLimited
}

// cloudWorkspaceBandwidthRetryDelay extracts the retry window when err is an
// hourly bandwidth rejection.
func cloudWorkspaceBandwidthRetryDelay(err error) (time.Duration, bool) {
	var bw *cloudWorkspaceBandwidthError
	if !errors.As(err, &bw) || bw == nil {
		return 0, false
	}
	delay := bw.retryAfter
	if delay < time.Second {
		delay = time.Second
	}
	if delay > time.Hour {
		delay = time.Hour
	}
	return delay, true
}

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

// CloudWorkspaceTaskProvision is the Hub-side durable half of creating a
// cloud workspace task. The local task is committed separately and then the
// operation is acknowledged with CompleteCloudWorkspaceTaskProvision.
type CloudWorkspaceTaskProvision struct {
	OperationID  string `json:"operation_id"`
	WorkspaceID  string `json:"workspace_id"`
	CloudTaskID  string `json:"cloud_task_id"`
	DeviceTaskID string `json:"device_task_id,omitempty"`
	Name         string `json:"name,omitempty"`
	Mode         string `json:"mode,omitempty"`
	Tag          string `json:"tag,omitempty"`
	State        string `json:"state"`
	LastError    string `json:"last_error,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
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
		Code              string `json:"code"`
		Message           string `json:"message"`
		RetryAfterSeconds int64  `json:"retry_after_seconds"`
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
	case "FENCED":
		return fmt.Errorf("%w: 云端工作区会话已被接管，请切换为只读或重新接手", errCloudWorkspaceFenced)
	case "PROTOCOL_MISMATCH":
		return fmt.Errorf("云端工作区协议不兼容，请升级客户端")
	case "IDEMPOTENCY_KEY_REUSED":
		return fmt.Errorf("云端工作区请求幂等键已复用，已拒绝")
	case "IDEMPOTENCY_IN_PROGRESS":
		return fmt.Errorf("云端工作区请求正在处理中，请稍后重试")
	case "CLOUD_WORKSPACE_PROVISION_STATE":
		return fmt.Errorf("云端工作区任务编排状态不允许该操作")
	case "CLOUD_WORKSPACE_VOLUME_FULL":
		return fmt.Errorf("云端工作区存储空间不足")
	case "CLOUD_WORKSPACE_SIZE":
		return fmt.Errorf("已超过单个云端工作区容量")
	case "CLOUD_WORKSPACE_TENANT_DISK":
		return fmt.Errorf("已超过租户云端工作区总容量")
	case "CLOUD_WORKSPACE_BANDWIDTH":
		retry := time.Duration(payload.RetryAfterSeconds) * time.Second
		if retry <= 0 {
			retry = time.Minute
		}
		return &cloudWorkspaceBandwidthError{retryAfter: retry}
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
	opt := cloudWorkspaceHTTPOptions{timeout: cloudWorkspaceRequestTimeout, maxRead: cloudWorkspaceResponseMaxSize, accept: "application/json"}
	if body != nil {
		opt.jsonBody = body
		if raw, marshalErr := json.Marshal(body); marshalErr == nil {
			opt.headers = map[string]string{"Idempotency-Key": cloudWorkspaceIdempotencyKey(method+":"+path, raw)}
		}
	}
	if opt.headers == nil {
		opt.headers = map[string]string{"Idempotency-Key": cloudWorkspaceIdempotencyKey(method+":"+path, nil)}
	}
	data, status, err := a.cloudWorkspaceHubDo(ctx, method, path, opt)
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

// ProvisionCloudWorkspaceTask starts an idempotent Hub operation that creates
// a workspace and its unique task binding. It intentionally returns before a
// local task is created so callers can recover or compensate after a crash.
func (a *App) ProvisionCloudWorkspaceTask(name, mode, tag, deviceTaskID string) (CloudWorkspaceTaskProvision, error) {
	ctx, cancel := a.cloudWorkspaceRequestContext()
	defer cancel()
	body := map[string]string{
		"name": strings.TrimSpace(name), "mode": strings.TrimSpace(mode),
		"tag": strings.TrimSpace(tag), "device_task_id": strings.TrimSpace(deviceTaskID),
	}
	rawBody, _ := json.Marshal(body)
	data, status, err := a.cloudWorkspaceHubDo(ctx, http.MethodPost, cloudWorkspaceTaskProvisionPath, cloudWorkspaceHTTPOptions{
		accept: "application/json", jsonBody: body,
		headers: map[string]string{"Idempotency-Key": cloudWorkspaceIdempotencyKey("workspace-task-provision", rawBody)},
	})
	if err != nil {
		return CloudWorkspaceTaskProvision{}, err
	}
	if status >= 300 {
		return CloudWorkspaceTaskProvision{}, cloudWorkspaceAPIError(status, data)
	}
	var out CloudWorkspaceTaskProvision
	if err := json.Unmarshal(data, &out); err != nil || strings.TrimSpace(out.OperationID) == "" || strings.TrimSpace(out.WorkspaceID) == "" {
		if err == nil {
			err = fmt.Errorf("missing operation or workspace id")
		}
		return CloudWorkspaceTaskProvision{}, fmt.Errorf("invalid cloud workspace task provision response: %w", err)
	}
	return out, nil
}

// CloudWorkspaceTaskProvisionStatus polls a durable operation after a
// network timeout or process restart.
func (a *App) CloudWorkspaceTaskProvisionStatus(operationID string) (CloudWorkspaceTaskProvision, error) {
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		return CloudWorkspaceTaskProvision{}, fmt.Errorf("operation id is required")
	}
	ctx, cancel := a.cloudWorkspaceRequestContext()
	defer cancel()
	data, status, err := a.cloudWorkspaceHubDo(ctx, http.MethodGet, cloudWorkspaceTaskProvisionItemPath(operationID), cloudWorkspaceHTTPOptions{accept: "application/json"})
	if err != nil {
		return CloudWorkspaceTaskProvision{}, err
	}
	if status >= 300 {
		return CloudWorkspaceTaskProvision{}, cloudWorkspaceAPIError(status, data)
	}
	var out CloudWorkspaceTaskProvision
	if err := json.Unmarshal(data, &out); err != nil {
		return CloudWorkspaceTaskProvision{}, err
	}
	return out, nil
}

func (a *App) transitionCloudWorkspaceTaskProvision(operationID string, abort bool, reason string) (CloudWorkspaceTaskProvision, error) {
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		return CloudWorkspaceTaskProvision{}, fmt.Errorf("operation id is required")
	}
	ctx, cancel := a.cloudWorkspaceRequestContext()
	defer cancel()
	path := cloudWorkspaceTaskProvisionItemPath(operationID) + "/complete"
	verb := http.MethodPost
	if abort {
		path = cloudWorkspaceTaskProvisionItemPath(operationID) + "/abort"
	}
	body := any(nil)
	if abort {
		body = map[string]string{"reason": strings.TrimSpace(reason)}
	}
	// Resolve the durable operation before transitioning it so the request can
	// carry the exact lease epoch for its workspace. Operation ids are not
	// workspace ids, so cloudWorkspaceHubDo cannot infer these headers from the
	// URL alone.
	statusOp, err := a.CloudWorkspaceTaskProvisionStatus(operationID)
	if err != nil {
		return CloudWorkspaceTaskProvision{}, err
	}
	rawBody, _ := json.Marshal(map[string]any{"operation_id": operationID, "action": map[bool]string{true: "abort", false: "complete"}[abort], "reason": strings.TrimSpace(reason)})
	headers := map[string]string{"Idempotency-Key": cloudWorkspaceIdempotencyKey("workspace-task-transition", rawBody)}
	if mount := lookupHeldCloudWorkspace(statusOp.WorkspaceID); mount != nil {
		mount.mu.Lock()
		leaseID, fencingToken := mount.LeaseID, mount.FencingToken
		mount.mu.Unlock()
		if strings.TrimSpace(leaseID) != "" {
			headers["X-Cloud-Workspace-Session"] = leaseID
		}
		if fencingToken > 0 {
			headers["X-Cloud-Workspace-Fencing"] = strconv.FormatInt(fencingToken, 10)
		}
	}
	data, status, err := a.cloudWorkspaceHubDo(ctx, verb, path, cloudWorkspaceHTTPOptions{
		accept: "application/json", jsonBody: body,
		headers: headers,
	})
	if err != nil {
		return CloudWorkspaceTaskProvision{}, err
	}
	if status >= 300 {
		return CloudWorkspaceTaskProvision{}, cloudWorkspaceAPIError(status, data)
	}
	var out CloudWorkspaceTaskProvision
	if err := json.Unmarshal(data, &out); err != nil {
		return CloudWorkspaceTaskProvision{}, err
	}
	return out, nil
}

// CompleteCloudWorkspaceTaskProvision acknowledges a durable local task.
func (a *App) CompleteCloudWorkspaceTaskProvision(operationID string) (CloudWorkspaceTaskProvision, error) {
	return a.transitionCloudWorkspaceTaskProvision(operationID, false, "")
}

// AbortCloudWorkspaceTaskProvision compensates a failed local task creation.
func (a *App) AbortCloudWorkspaceTaskProvision(operationID, reason string) (CloudWorkspaceTaskProvision, error) {
	return a.transitionCloudWorkspaceTaskProvision(operationID, true, reason)
}

// CreateProvisionedCloudWorkspaceTask is the failure-safe high-level flow for
// callers that need a brand-new workspace and task in one user action. The
// Hub operation is left queryable on network errors; local failures trigger a
// durable compensating transition instead of silently leaking a quota slot.
func (a *App) CreateProvisionedCloudWorkspaceTask(name, mode, tag string) (ProjectSearchResult, error) {
	op, err := a.ProvisionCloudWorkspaceTask(name, mode, tag, "")
	if err != nil {
		return ProjectSearchResult{}, err
	}
	result, err := a.CreateTaskWithCloudWorkspace(name, "", mode, op.WorkspaceID)
	if err != nil {
		if _, abortErr := a.AbortCloudWorkspaceTaskProvision(op.OperationID, err.Error()); abortErr != nil {
			return ProjectSearchResult{}, fmt.Errorf("%v; compensation pending: %w", err, abortErr)
		}
		return ProjectSearchResult{}, err
	}
	if _, err := a.CompleteCloudWorkspaceTaskProvision(op.OperationID); err != nil {
		return ProjectSearchResult{}, fmt.Errorf("local task created but remote provisioning acknowledgement failed: %w", err)
	}
	return result, nil
}

// RequestCloudWorkspaceHandoff notifies the current writer that this device
// is waiting to take over. It does not force a lease takeover; the caller must
// still explicitly acquire after the writer releases or the lease expires.
func (a *App) RequestCloudWorkspaceHandoff(id string) error {
	id = strings.TrimSpace(id)
	if !validCloudWorkspaceCacheID(id) {
		return fmt.Errorf("workspace id is required")
	}
	ctx, cancel := a.cloudWorkspaceRequestContext()
	defer cancel()
	key := cloudWorkspaceIdempotencyKey("lease-handoff-request", []byte(id))
	data, status, err := a.cloudWorkspaceHubDo(ctx, http.MethodPost, cloudWorkspaceLeaseHandoffRequestPath(id), cloudWorkspaceHTTPOptions{
		accept:  "application/json",
		headers: map[string]string{"Idempotency-Key": key},
	})
	if err != nil {
		return err
	}
	if status >= 300 {
		return cloudWorkspaceAPIError(status, data)
	}
	return nil
}

// RenameCloudWorkspace PATCH /api/v1/cloud-workspaces/{id}.
func (a *App) RenameCloudWorkspace(id, name string) (CloudWorkspaceEntitlementWorkspace, error) {
	id = strings.TrimSpace(id)
	if !validCloudWorkspaceCacheID(id) {
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
	if !validCloudWorkspaceCacheID(id) {
		return CloudWorkspaceDeletedWorkspace{}, fmt.Errorf("workspace id is required")
	}
	ctx, cancel := a.cloudWorkspaceRequestContext()
	defer cancel()
	data, status, err := a.cloudWorkspaceHubDo(ctx, http.MethodDelete, cloudWorkspaceItemPath(id), cloudWorkspaceHTTPOptions{accept: "application/json", headers: map[string]string{"Idempotency-Key": cloudWorkspaceIdempotencyKey("workspace-delete", []byte(id))}})
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
	id = strings.TrimSpace(id)
	if !validCloudWorkspaceCacheID(id) {
		return CloudWorkspaceDeletedWorkspace{}, fmt.Errorf("workspace id is required")
	}
	// Soft delete is reversible, but it still must not discard local changes.
	// Release the active writer before changing the Hub row to deleted; the
	// Hub's lease-release CAS intentionally only accepts an active workspace.
	// If the final push/sidecar flush fails, leave the mount and workspace
	// untouched so the user can retry or export the dirty cache.
	task, taskKnown := lookupCloudWorkspaceTask(id)
	if taskKnown {
		// Stop an active Agent/coding loop before the final flush. Cancellation is
		// asynchronous, but revoking its semantic task relation immediately
		// prevents a new queued turn from starting while release is in progress.
		a.cancelProjectTaskLoop(task.ProjectPath)
	}
	if taskKnown || lookupHeldCloudWorkspace(id) != nil {
		releaseCtx, releaseCancel := a.cloudWorkspaceSyncContext()
		releaseErr := a.releaseCloudWorkspace(releaseCtx, id, false)
		releaseCancel()
		if releaseErr != nil {
			a.recordCloudWorkspaceAudit(cloudWorkspaceAuditEvent{WorkspaceID: id, Operation: "local_release", Outcome: "failed", Detail: releaseErr.Error()})
			return CloudWorkspaceDeletedWorkspace{}, fmt.Errorf("flush local cloud workspace before delete: %w", releaseErr)
		}
	}
	out, err := a.deleteCloudWorkspaceOnHub(id)
	if err != nil {
		a.recordCloudWorkspaceAudit(cloudWorkspaceAuditEvent{WorkspaceID: id, Operation: "remote_delete", Outcome: "failed", Detail: err.Error()})
		return CloudWorkspaceDeletedWorkspace{}, err
	}
	a.recordCloudWorkspaceAudit(cloudWorkspaceAuditEvent{WorkspaceID: id, Operation: "remote_delete"})
	// Hide the local row without a tombstone so a 7-day workspace restore can unhide it.
	a.hideLocalCloudWorkspaceTask(id, false)
	return out, nil
}

// ForceDeleteCloudWorkspace permanently removes a previously deleted workspace and its remote files.
func (a *App) ForceDeleteCloudWorkspace(id string) error {
	id = strings.TrimSpace(id)
	if !validCloudWorkspaceCacheID(id) {
		return fmt.Errorf("workspace id is required")
	}
	ctx, cancel := a.cloudWorkspaceRequestContext()
	defer cancel()
	// Stop any local mount and flush its pending changes before the remote
	// object store is purged. This also prevents a background sync from racing
	// the permanent delete request.
	task, taskKnown := lookupCloudWorkspaceTask(id)
	if taskKnown {
		a.cancelProjectTaskLoop(task.ProjectPath)
	}
	if taskKnown || lookupHeldCloudWorkspace(id) != nil {
		// A permanent purge is destructive.  Do not proceed while this process
		// still owns a writer mount whose final push/sidecar flush or lease
		// release failed: doing so would delete the only copy of unsynced local
		// changes.  The normal hide/release paths intentionally log-and-continue,
		// but ForceDelete must fail closed and leave the mount recoverable.
		releaseCtx, releaseCancel := a.cloudWorkspaceSyncContext()
		releaseErr := a.releaseCloudWorkspace(releaseCtx, id, false)
		releaseCancel()
		if releaseErr != nil {
			a.recordCloudWorkspaceAudit(cloudWorkspaceAuditEvent{WorkspaceID: id, Operation: "local_release", Outcome: "failed", Detail: releaseErr.Error()})
			return fmt.Errorf("flush local cloud workspace before purge: %w", releaseErr)
		}
	}
	data, status, err := a.cloudWorkspaceHubDo(ctx, http.MethodDelete, cloudWorkspaceItemPath(id)+"/purge", cloudWorkspaceHTTPOptions{accept: "application/json", headers: map[string]string{"Idempotency-Key": cloudWorkspaceIdempotencyKey("workspace-purge", []byte(id))}})
	if err != nil {
		a.recordCloudWorkspaceAudit(cloudWorkspaceAuditEvent{WorkspaceID: id, Operation: "remote_purge", Outcome: "failed", Detail: err.Error()})
		return err
	}
	if status >= 300 && status != http.StatusNotFound {
		a.recordCloudWorkspaceAudit(cloudWorkspaceAuditEvent{WorkspaceID: id, Operation: "remote_purge", Outcome: "failed", Detail: cloudWorkspaceAPIError(status, data).Error()})
		return cloudWorkspaceAPIError(status, data)
	}
	if status == http.StatusNotFound {
		// Purge is idempotent from the local privacy perspective: if Hub already
		// removed the workspace, we must still erase every local copy rather than
		// returning early and leaving readable data behind.
		a.recordCloudWorkspaceAudit(cloudWorkspaceAuditEvent{WorkspaceID: id, Operation: "remote_purge", Outcome: "already_gone"})
	} else {
		a.recordCloudWorkspaceAudit(cloudWorkspaceAuditEvent{WorkspaceID: id, Operation: "remote_purge"})
	}
	resetCloudWorkspaceEntitlementCache()
	a.hideLocalCloudWorkspaceTask(id, true)
	if err := a.purgeCloudWorkspaceLocalCaches(id); err != nil {
		a.recordCloudWorkspaceAudit(cloudWorkspaceAuditEvent{WorkspaceID: id, Operation: "cache_purge", Outcome: "failed", Detail: err.Error()})
		return err
	}
	a.recordCloudWorkspaceAudit(cloudWorkspaceAuditEvent{WorkspaceID: id, Operation: "cache_purge"})
	return nil
}

// purgeCloudWorkspaceLocalCaches removes every local representation of a
// workspace, including isolated read-only browse caches.  Purging only the
// canonical writer path leaves a readable copy behind whenever a user had
// browsed the workspace from another tab or process.  All targets are derived
// from the validated workspace id and checked against their dedicated roots.
func (a *App) purgeCloudWorkspaceLocalCaches(workspaceID string) error {
	if a == nil {
		return nil
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if !validCloudWorkspaceCacheID(workspaceID) {
		return fmt.Errorf("workspace id is required")
	}
	if mount := takeCloudWorkspaceMount(workspaceID); mount != nil {
		stopCloudWorkspaceMount(mount)
	}
	dataDir := a.GetDataDir()
	// A sealed cache is unreadable without its key.  Once every local copy is
	// removed, retire that per-workspace key as part of the privacy boundary;
	// otherwise a later recovery of a forgotten encrypted copy would still be
	// possible.  Track tenant directories explicitly because older profiles may
	// have been created under a different tenant than the current config.
	keyTenants := map[string]struct{}{}
	keyCleanupRequested := cloudWorkspaceCacheEncryptionEnabled(a)
	for _, rootName := range []string{"cloud-workspaces", "cloud-workspaces-readonly"} {
		root := normalizeProjectSessionPath(filepath.Join(dataDir, rootName))
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("list local cloud workspace caches: %w", err)
		}
		for _, tenant := range entries {
			if tenant.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("unsafe cloud workspace cache tenant path")
			}
			if !tenant.IsDir() {
				continue
			}
			tenantPath := normalizeProjectSessionPath(filepath.Join(root, tenant.Name()))
			// Never follow a tenant symlink/reparse point.  Returning an error
			// leaves the caller with an explicit privacy failure instead of
			// deleting an unrelated directory outside the cache root.
			if !cloudWorkspacePathResolvesInsideRoot(root, tenantPath) {
				return fmt.Errorf("unsafe cloud workspace cache tenant path")
			}
			candidate := normalizeProjectSessionPath(filepath.Join(root, tenant.Name(), workspaceID))
			if !cloudWorkspacePathInsideRoot(root, candidate) || filepath.Base(candidate) != workspaceID {
				continue
			}
			candidateInfo, statErr := os.Lstat(candidate)
			if os.IsNotExist(statErr) {
				continue
			} else if statErr != nil {
				return fmt.Errorf("inspect local cloud workspace cache: %w", statErr)
			}
			if candidateInfo.Mode()&os.ModeSymlink != 0 {
				if err := os.Remove(candidate); err != nil {
					return fmt.Errorf("remove unsafe cloud workspace cache link: %w", err)
				}
				continue
			}
			if !candidateInfo.IsDir() || !cloudWorkspacePathResolvesInsideRoot(root, candidate) {
				return fmt.Errorf("unsafe cloud workspace cache path")
			}
			if _, markerErr := os.Stat(cloudWorkspaceCacheSealMarkerPath(candidate)); markerErr == nil {
				keyCleanupRequested = true
			}
			keyTenants[tenant.Name()] = struct{}{}
			if err := os.RemoveAll(candidate); err != nil {
				return fmt.Errorf("remove local cloud workspace cache: %w", err)
			}
		}
	}
	if keyCleanupRequested {
		if len(keyTenants) == 0 {
			keyTenants[a.cloudWorkspaceTenantID()] = struct{}{}
		}
		for tenantID := range keyTenants {
			if err := deleteCloudWorkspaceCacheKey(tenantID, workspaceID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *App) cloudWorkspaceLocalCachesPresent(workspaceID string) bool {
	if a == nil || !validCloudWorkspaceCacheID(workspaceID) {
		return false
	}
	if lookupHeldCloudWorkspace(workspaceID) != nil {
		return true
	}
	for _, rootName := range []string{"cloud-workspaces", "cloud-workspaces-readonly"} {
		root := normalizeProjectSessionPath(filepath.Join(a.GetDataDir(), rootName))
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, tenant := range entries {
			candidate := filepath.Join(root, tenant.Name(), workspaceID)
			if _, err := os.Lstat(candidate); err == nil {
				return true
			}
		}
	}
	return false
}

// RestoreCloudWorkspace POST /api/v1/cloud-workspaces/{id}/restore.
func (a *App) RestoreCloudWorkspace(id string) (CloudWorkspaceEntitlementWorkspace, error) {
	id = strings.TrimSpace(id)
	if !validCloudWorkspaceCacheID(id) {
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

// localCloudWorkspaceID is the 1:1 identity used by restore, resume, and
// list collapse. Working_dir wins over accumulated cloud_workspace: tags.
func localCloudWorkspaceID(rec memory.ProjectRecord) string {
	if !projectRecordHasTag(rec, taskManagementTag) {
		return ""
	}
	return primaryCloudWorkspaceID(rec)
}

func recordIsLocalCloudWorkspaceTask(candidate memory.ProjectRecord, workspaceID string) bool {
	workspaceID = strings.TrimSpace(workspaceID)
	return workspaceID != "" && localCloudWorkspaceID(candidate) == workspaceID
}

func (a *App) findLocalCloudWorkspaceTask(workspaceID string, includeHidden bool) ProjectSearchResult {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return ProjectSearchResult{}
	}
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return ProjectSearchResult{}
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return ProjectSearchResult{}
	}
	hidden := ProjectSearchResult{}
	archived := ProjectSearchResult{}
	for _, rec := range pi.ListAllMatching(func(candidate memory.ProjectRecord) bool {
		return recordIsLocalCloudWorkspaceTask(candidate, workspaceID)
	}) {
		result := a.projectRecordToSearchResult(pi, rec)
		if pi.IsArchived(rec.ProjectPath) {
			if includeHidden && strings.TrimSpace(archived.ProjectPath) == "" {
				archived = result
			}
			continue
		}
		if pi.IsHidden(rec.ProjectPath) {
			if includeHidden && strings.TrimSpace(hidden.ProjectPath) == "" {
				hidden = result
			}
			continue
		}
		rememberCloudWorkspaceTask(workspaceID, result)
		return result
	}
	if includeHidden && strings.TrimSpace(hidden.ProjectPath) != "" {
		return hidden
	}
	if includeHidden && strings.TrimSpace(archived.ProjectPath) != "" {
		return archived
	}
	return ProjectSearchResult{}
}

func (a *App) findVisibleCloudWorkspaceTask(workspaceID string) ProjectSearchResult {
	return a.findLocalCloudWorkspaceTask(workspaceID, false)
}

func (a *App) visibleCloudWorkspaceTaskAt(workspaceID, projectPath string) ProjectSearchResult {
	workspaceID = strings.TrimSpace(workspaceID)
	projectPath = normalizeProjectSessionPath(projectPath)
	if workspaceID == "" || projectPath == "" {
		return ProjectSearchResult{}
	}
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return ProjectSearchResult{}
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return ProjectSearchResult{}
	}
	rec := pi.Get(projectPath)
	if rec == nil || pi.IsHidden(projectPath) || pi.IsArchived(projectPath) {
		return ProjectSearchResult{}
	}
	if !recordIsLocalCloudWorkspaceTask(*rec, workspaceID) {
		return ProjectSearchResult{}
	}
	return a.projectRecordToSearchResult(pi, *rec)
}

func (a *App) bindPreparedCloudWorkspaceTask(workspaceID string, result ProjectSearchResult, localPath string) ProjectSearchResult {
	localPath = normalizeProjectSessionPath(localPath)
	if localPath != "" {
		result.WorkingDir = localPath
	}
	result = a.sanitizeCloudWorkspaceTaskIdentity(result)
	rememberCloudWorkspaceTask(workspaceID, result)
	rememberCloudWorkspaceLocalPath(localPath, workspaceID)
	return result
}

func (a *App) sanitizeCloudWorkspaceTaskIdentity(result ProjectSearchResult) ProjectSearchResult {
	if !cloudWorkspaceTagsHaveEnvironmentOverlap(result.Tags) {
		return result
	}
	result.Tags = scrubCloudWorkspaceIdentityTags(result.Tags)
	path := normalizeProjectSessionPath(result.ProjectPath)
	if a == nil || path == "" || a.memoryStore == nil {
		return result
	}
	if pi := a.memoryStore.ProjectIndex(); pi != nil {
		pi.ReplacePrefixedTags(path, cloudWorkspaceIdentityDropPrefixes(), nil)
	}
	return result
}

// CreateTaskWithCloudWorkspace prepares the cache mount then creates a task tagged cloud_workspace:{id}.
// workingDir is ignored: PrepareCloudWorkspace returns LocalPath as the working directory.
func (a *App) CreateTaskWithCloudWorkspace(name, workingDir, mode, workspaceID string) (ProjectSearchResult, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if !validCloudWorkspaceCacheID(workspaceID) {
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
		a.hideOtherLocalCloudWorkspaceTasks(workspaceID, bound.ProjectPath)
		a.refreshCloudWorkspaceSidecars(workspaceID, bound.ProjectPath)
		return bound, nil
	}
	name, mode = a.cloudWorkspaceTaskIdentity(workspaceID, name, mode)
	taskName := normalizeRecentTaskName(name)
	if taskName == "" {
		return ProjectSearchResult{}, fmt.Errorf("task name is required")
	}
	tags := []string{taskManagementTag, taskUserCreatedTag, cloudWorkspaceTag(workspaceID)}
	if normalized := normalizeCloudWorkspaceTaskMode(mode); normalized != "" {
		tags = append(tags, normalized)
	}
	result := a.createTaskRecordWithWorkingDir(taskName, "", tags, prepared.LocalPath, false)
	if strings.TrimSpace(result.ProjectPath) != "" {
		a.clearDismissedCloudWorkspaceTask(workspaceID)
		bound := a.bindPreparedCloudWorkspaceTask(workspaceID, result, prepared.LocalPath)
		a.hideOtherLocalCloudWorkspaceTasks(workspaceID, bound.ProjectPath)
		a.applyCloudWorkspaceSidecars(workspaceID, bound.ProjectPath)
		a.flushCloudWorkspaceTaskSidecarBestEffort(workspaceID)
		return bound, nil
	}
	return result, fmt.Errorf("创建云端工作区任务失败")
}

// ResumeCloudWorkspaceTask returns the 1:1 task for workspaceID after
// re-running PrepareCloudWorkspace so tab/sidebar reopen holds the exclusive lease.
// projectPath, when it still names a visible row for this workspace, is kept so
// clicking an older duplicate does not switch the user onto a newer leftover row.
func (a *App) ResumeCloudWorkspaceTask(workspaceID, projectPath string) (ProjectSearchResult, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if !validCloudWorkspaceCacheID(workspaceID) {
		if workspaceID == "" {
			return ProjectSearchResult{}, nil
		}
		return ProjectSearchResult{}, fmt.Errorf("workspace id is required")
	}
	existing := a.visibleCloudWorkspaceTaskAt(workspaceID, projectPath)
	if strings.TrimSpace(existing.ProjectPath) == "" {
		existing = a.findVisibleCloudWorkspaceTask(workspaceID)
	}
	if strings.TrimSpace(existing.ProjectPath) == "" {
		return ProjectSearchResult{}, nil
	}
	alreadyHeld := false
	if path, ok := heldWritableCloudWorkspacePath(workspaceID); ok && strings.TrimSpace(path) != "" {
		alreadyHeld = true
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
	a.hideOtherLocalCloudWorkspaceTasks(workspaceID, bound.ProjectPath)
	if alreadyHeld {
		// Live mount in this process: watcher/heartbeat is the consistency
		// path. Re-fetching sidecars can overlay a stale Hub snapshot onto
		// an open tab after flush timeouts.
		return bound, nil
	}
	a.refreshCloudWorkspaceSidecars(workspaceID, bound.ProjectPath)
	return bound, nil
}
