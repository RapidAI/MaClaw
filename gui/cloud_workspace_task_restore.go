package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

var (
	cloudWorkspaceRestoreMu    sync.Mutex
	cloudWorkspaceRestoreRunMu sync.Mutex
	cloudWorkspaceRestoreGen   atomic.Uint64
)

func bumpCloudWorkspaceRestoreGen() {
	cloudWorkspaceRestoreGen.Add(1)
}

type cloudWorkspaceDismissedTasks struct {
	IDs []string `json:"ids"`
}

func (a *App) cloudWorkspaceDismissedPath() string {
	if a == nil {
		return ""
	}
	return filepath.Join(a.GetDataDir(), "cloud-workspaces", "dismissed-task-ids.json")
}

func (a *App) loadDismissedCloudWorkspaceTasks() map[string]struct{} {
	out := map[string]struct{}{}
	path := a.cloudWorkspaceDismissedPath()
	if path == "" {
		return out
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return out
	}
	var payload cloudWorkspaceDismissedTasks
	if err := json.Unmarshal(data, &payload); err != nil {
		return out
	}
	for _, id := range payload.IDs {
		id = strings.TrimSpace(id)
		if id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

func (a *App) saveDismissedCloudWorkspaceTasks(ids map[string]struct{}) {
	path := a.cloudWorkspaceDismissedPath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("[cloud_workspace] dismissed mkdir failed path=%q err=%v", path, err)
		return
	}
	payload := cloudWorkspaceDismissedTasks{IDs: make([]string, 0, len(ids))}
	for id := range ids {
		if strings.TrimSpace(id) != "" {
			payload.IDs = append(payload.IDs, id)
		}
	}
	sort.Strings(payload.IDs)
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if err := atomicWriteFile(path, data); err != nil {
		log.Printf("[cloud_workspace] dismissed write failed path=%q err=%v", path, err)
	}
}

func (a *App) dismissCloudWorkspaceTask(workspaceID string) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return
	}
	cloudWorkspaceRestoreMu.Lock()
	defer cloudWorkspaceRestoreMu.Unlock()
	ids := a.loadDismissedCloudWorkspaceTasks()
	if _, ok := ids[workspaceID]; ok {
		return
	}
	ids[workspaceID] = struct{}{}
	a.saveDismissedCloudWorkspaceTasks(ids)
}

func (a *App) clearDismissedCloudWorkspaceTask(workspaceID string) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return
	}
	cloudWorkspaceRestoreMu.Lock()
	defer cloudWorkspaceRestoreMu.Unlock()
	ids := a.loadDismissedCloudWorkspaceTasks()
	if _, ok := ids[workspaceID]; !ok {
		return
	}
	delete(ids, workspaceID)
	a.saveDismissedCloudWorkspaceTasks(ids)
}

func (a *App) dismissCloudWorkspaceTaskByPath(projectPath string) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return
	}
	id := a.lookupCloudWorkspaceIDForProject(projectPath)
	if id == "" {
		if result, ok := lookupCloudWorkspaceTaskByPath(projectPath); ok {
			id = cloudWorkspaceIDFromTags(result.Tags)
		}
	}
	if id == "" {
		a.ensureMemoryStore()
		if a.memoryStore != nil {
			if pi := a.memoryStore.ProjectIndex(); pi != nil {
				if rec := pi.Get(projectPath); rec != nil {
					id = cloudWorkspaceIDFromTags(rec.Tags)
				}
			}
		}
	}
	if id != "" {
		a.dismissCloudWorkspaceTask(id)
	}
}

func (a *App) cloudWorkspaceRestoreIdentity(ws CloudWorkspaceEntitlementWorkspace) (string, string) {
	name := strings.TrimSpace(ws.TaskName)
	mode := strings.TrimSpace(ws.TaskMode)
	// Empty mode is a valid chat task. Only fetch task.json when the name is missing.
	if name == "" {
		ctx, cancel := a.cloudWorkspaceRequestContext()
		defer cancel()
		data, err := a.getCloudWorkspaceSidecar(ctx, ws.ID, cloudWorkspaceSidecarTask)
		if err != nil {
			log.Printf("[cloud_workspace] restore task sidecar workspace=%s err=%v", ws.ID, err)
		} else if len(data) > 0 {
			var task cloudWorkspaceTaskSidecar
			if err := json.Unmarshal(data, &task); err != nil {
				log.Printf("[cloud_workspace] restore task sidecar json workspace=%s err=%v", ws.ID, err)
			} else {
				name = strings.TrimSpace(task.Name)
				if mode == "" {
					mode = strings.TrimSpace(task.Mode)
				}
			}
		}
	}
	if name == "" {
		name = strings.TrimSpace(ws.Name)
	}
	return name, mode
}

func (a *App) restoreCloudWorkspaceTaskRecord(ws CloudWorkspaceEntitlementWorkspace, name, mode string) ProjectSearchResult {
	workspaceID := strings.TrimSpace(ws.ID)
	if workspaceID == "" {
		return ProjectSearchResult{}
	}
	name = normalizeRecentTaskName(name)
	if name == "" {
		return ProjectSearchResult{}
	}
	workingDir := a.cloudWorkspaceCachePath(a.cloudWorkspaceTenantID(), workspaceID)
	tags := []string{taskManagementTag, taskUserCreatedTag, cloudWorkspaceTag(workspaceID)}
	if normalized := NormalizeCreateTaskMode(mode); normalized != "" {
		tags = append(tags, normalized)
	}
	result := a.createTaskRecordWithWorkingDir(name, "", tags, workingDir, false)
	if strings.TrimSpace(result.ProjectPath) == "" {
		return ProjectSearchResult{}
	}
	return a.bindPreparedCloudWorkspaceTask(workspaceID, result, workingDir)
}

func (a *App) cloudWorkspaceTaskFlags(result ProjectSearchResult) (hidden, archived bool) {
	path := normalizeProjectSessionPath(result.ProjectPath)
	if path == "" {
		return false, false
	}
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return false, false
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return false, false
	}
	return pi.IsHidden(path), pi.IsArchived(path)
}

func (a *App) unhideCloudWorkspaceTask(workspaceID string, existing ProjectSearchResult) ProjectSearchResult {
	path := normalizeProjectSessionPath(existing.ProjectPath)
	if path == "" {
		return ProjectSearchResult{}
	}
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return existing
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return existing
	}
	pi.SetHidden(path, false)
	workingDir := strings.TrimSpace(existing.WorkingDir)
	if workingDir == "" {
		workingDir = a.cloudWorkspaceCachePath(a.cloudWorkspaceTenantID(), workspaceID)
	}
	if rec := pi.Get(path); rec != nil {
		existing = projectRecordToSearchResult(pi, *rec)
	}
	bound := a.bindPreparedCloudWorkspaceTask(workspaceID, existing, workingDir)
	return bound
}

func (a *App) hideLocalCloudWorkspaceTask(workspaceID string, dismiss bool) {
	workspaceID = strings.TrimSpace(workspaceID)
	if a == nil || workspaceID == "" {
		return
	}
	// Do not boot the memory store: entitlement-only callers have no local task list.
	if a.memoryStore == nil {
		return
	}
	cloudWorkspaceRestoreMu.Lock()
	existing := a.findLocalCloudWorkspaceTask(workspaceID, true)
	path := normalizeProjectSessionPath(existing.ProjectPath)
	if path == "" {
		cloudWorkspaceRestoreMu.Unlock()
		return
	}
	if dismiss {
		ids := a.loadDismissedCloudWorkspaceTasks()
		if _, ok := ids[workspaceID]; !ok {
			ids[workspaceID] = struct{}{}
			a.saveDismissedCloudWorkspaceTasks(ids)
		}
	}
	a.ensureMemoryStore()
	if a.memoryStore != nil {
		if pi := a.memoryStore.ProjectIndex(); pi != nil {
			pi.SetHidden(path, true)
		}
	}
	cloudWorkspaceRestoreMu.Unlock()
	a.releaseCloudWorkspaceForProjectPath(path)
	forgetCloudWorkspaceTaskByPath(path)
	a.cancelProjectTaskLoop(path)
	a.emitProjectIndexChanged(path)
	a.emitProjectTaskClosed(path)
}

// hideOtherLocalCloudWorkspaceTasks hides leftover local rows for workspaceID
// except exceptPath. DeleteTask uses this when an in-flight restore created a
// replacement row for the same workspace while the original path was being removed.
func (a *App) hideOtherLocalCloudWorkspaceTasks(workspaceID, exceptPath string) {
	workspaceID = strings.TrimSpace(workspaceID)
	exceptPath = normalizeProjectSessionPath(exceptPath)
	if a == nil || workspaceID == "" {
		return
	}
	if a.memoryStore == nil {
		return
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return
	}
	want := cloudWorkspaceTag(workspaceID)
	cloudWorkspaceRestoreMu.Lock()
	var extras []string
	for _, rec := range pi.ListAllMatching(func(candidate memory.ProjectRecord) bool {
		return projectRecordHasTag(candidate, taskManagementTag) && projectRecordHasTag(candidate, want)
	}) {
		path := normalizeProjectSessionPath(rec.ProjectPath)
		if path == "" || path == exceptPath || pi.IsArchived(path) || pi.IsHidden(path) {
			continue
		}
		pi.SetHidden(path, true)
		extras = append(extras, path)
	}
	cloudWorkspaceRestoreMu.Unlock()
	for _, path := range extras {
		forgetCloudWorkspaceTaskByPath(path)
		a.cancelProjectTaskLoop(path)
		a.emitProjectIndexChanged(path)
		a.emitProjectTaskDeleted(path)
		a.emitProjectTaskClosed(path)
	}
}

func (a *App) entitlementForCloudWorkspaceRestore() CloudWorkspaceEntitlement {
	ent, ok := loadCloudWorkspaceEntitlementCache()
	if !ok || !ent.Enabled || ent.HubUnavailable {
		ent = a.CloudWorkspaceEntitlement()
	}
	return ent
}

// RestoreCloudWorkspaceTasks materializes Hub cloud workspaces into the local
// task list without acquiring leases. A new machine therefore sees the same
// cloud tasks after login. HideTask on this machine is remembered locally so a
// retry can rebind the same workspace; DeleteTask also soft-deletes the Hub
// workspace (7-day restore), so a deleted task does not linger as a named blank slot.
func (a *App) RestoreCloudWorkspaceTasks() []ProjectSearchResult {
	if a == nil {
		return nil
	}
	cloudWorkspaceRestoreRunMu.Lock()
	defer cloudWorkspaceRestoreRunMu.Unlock()
	return a.restoreCloudWorkspaceTasks()
}

func (a *App) restoreCloudWorkspaceTasks() []ProjectSearchResult {
	ent := a.entitlementForCloudWorkspaceRestore()
	if !ent.Enabled || ent.HubUnavailable {
		return nil
	}
	a.ensureMemoryStore()
	gen := cloudWorkspaceRestoreGen.Load()

	pending := make([]CloudWorkspaceEntitlementWorkspace, 0, len(ent.Workspaces))
	restored := make([]ProjectSearchResult, 0, len(ent.Workspaces))
	unhid := 0
	cloudWorkspaceRestoreMu.Lock()
	dismissed := a.loadDismissedCloudWorkspaceTasks()
	for _, ws := range ent.Workspaces {
		workspaceID := strings.TrimSpace(ws.ID)
		if workspaceID == "" {
			continue
		}
		if _, skip := dismissed[workspaceID]; skip {
			continue
		}
		existing := a.findLocalCloudWorkspaceTask(workspaceID, true)
		if strings.TrimSpace(existing.ProjectPath) == "" {
			pending = append(pending, ws)
			continue
		}
		hidden, archived := a.cloudWorkspaceTaskFlags(existing)
		if archived {
			continue
		}
		if hidden {
			restored = append(restored, a.unhideCloudWorkspaceTask(workspaceID, existing))
			unhid++
			continue
		}
		if strings.TrimSpace(existing.WorkingDir) == "" {
			existing.WorkingDir = a.cloudWorkspaceCachePath(a.cloudWorkspaceTenantID(), workspaceID)
		}
		rememberCloudWorkspaceTask(workspaceID, existing)
	}
	cloudWorkspaceRestoreMu.Unlock()
	if unhid > 0 {
		a.emitProjectIndexChanged("")
		unhid = 0
	}

	for _, ws := range pending {
		if cloudWorkspaceRestoreGen.Load() != gen {
			break
		}
		name, mode := a.cloudWorkspaceRestoreIdentity(ws)
		if cloudWorkspaceRestoreGen.Load() != gen {
			break
		}
		cloudWorkspaceRestoreMu.Lock()
		workspaceID := strings.TrimSpace(ws.ID)
		if _, skip := a.loadDismissedCloudWorkspaceTasks()[workspaceID]; skip {
			cloudWorkspaceRestoreMu.Unlock()
			continue
		}
		if existing := a.findLocalCloudWorkspaceTask(workspaceID, true); strings.TrimSpace(existing.ProjectPath) != "" {
			hidden, archived := a.cloudWorkspaceTaskFlags(existing)
			if archived {
				cloudWorkspaceRestoreMu.Unlock()
				continue
			}
			if hidden {
				restored = append(restored, a.unhideCloudWorkspaceTask(workspaceID, existing))
				unhid++
			} else {
				rememberCloudWorkspaceTask(workspaceID, existing)
			}
			cloudWorkspaceRestoreMu.Unlock()
			continue
		}
		if cloudWorkspaceRestoreGen.Load() != gen {
			cloudWorkspaceRestoreMu.Unlock()
			break
		}
		created := a.restoreCloudWorkspaceTaskRecord(ws, name, mode)
		cloudWorkspaceRestoreMu.Unlock()
		if strings.TrimSpace(created.ProjectPath) == "" {
			log.Printf("[cloud_workspace] restore task failed workspace=%s", workspaceID)
			continue
		}
		restored = append(restored, created)
	}
	if unhid > 0 {
		a.emitProjectIndexChanged("")
	}
	return restored
}
