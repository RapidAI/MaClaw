package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	cloudWorkspaceTagPrefix              = "cloud_workspace:"
	cloudWorkspaceDefaultTenantID        = "tenant_default"
	cloudWorkspaceHeartbeatInterval      = 30 * time.Second
	cloudWorkspaceShutdownReleaseTimeout = 8 * time.Second
	cloudWorkspaceWatchDebounce          = time.Second
)

// PreparedCloudWorkspace is the PrepareCloudWorkspace result.
// LocalPath is the cache working directory; WorkspaceID is never inferred from the path.
type PreparedCloudWorkspace struct {
	LocalPath   string `json:"local_path"`
	WorkspaceID string `json:"workspace_id"`
}

type cloudWorkspaceAcquireOutcome struct {
	LeaseID   string `json:"lease_id"`
	ExpiresAt string `json:"expires_at"`
	Acquired  string `json:"acquired"`
}

type cloudWorkspaceInUseError struct {
	HolderMachineID   string
	HolderMachineName string
	ExpiresAt         string
}

func (e *cloudWorkspaceInUseError) Error() string {
	if e == nil {
		return "云端工作区占用中（其他设备）"
	}
	if name := strings.TrimSpace(e.HolderMachineName); name != "" {
		return fmt.Sprintf("云端工作区占用中（其他设备：%s）", name)
	}
	return "云端工作区占用中（其他设备）"
}

type cloudWorkspaceHeldMount struct {
	WorkspaceID string
	LeaseID     string
	LocalPath   string
	TenantID    string
	ReadOnly    bool

	hbCancel  context.CancelFunc
	watcher   *fsnotify.Watcher
	pushTimer *time.Timer
	mu        sync.Mutex
}

var (
	cloudWorkspaceMountMu   sync.Mutex
	cloudWorkspaceMounts    = map[string]*cloudWorkspaceHeldMount{}
	cloudWorkspaceByLocal   = map[string]string{}
	cloudWorkspacePrepareMu sync.Mutex

	cloudWorkspaceBackgroundDisabled     bool
	cloudWorkspaceConfirmStealFn         func(holder string) bool
	cloudWorkspaceConfirmDiscardDirtyFn  func() bool
	cloudWorkspaceHeartbeatIntervalValue = cloudWorkspaceHeartbeatInterval
)

func resetCloudWorkspaceMounts() {
	cloudWorkspaceMountMu.Lock()
	mounts := cloudWorkspaceMounts
	cloudWorkspaceMounts = map[string]*cloudWorkspaceHeldMount{}
	cloudWorkspaceByLocal = map[string]string{}
	cloudWorkspaceMountMu.Unlock()
	for _, mount := range mounts {
		stopCloudWorkspaceMount(mount)
	}
}

func cloudWorkspaceTag(workspaceID string) string {
	return cloudWorkspaceTagPrefix + strings.TrimSpace(workspaceID)
}

func cloudWorkspaceIDFromTags(tags []string) string {
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if strings.HasPrefix(tag, cloudWorkspaceTagPrefix) {
			return strings.TrimSpace(strings.TrimPrefix(tag, cloudWorkspaceTagPrefix))
		}
	}
	return ""
}

func rememberCloudWorkspaceLocalPath(localPath, workspaceID string) {
	localPath = normalizeProjectSessionPath(localPath)
	workspaceID = strings.TrimSpace(workspaceID)
	if localPath == "" || workspaceID == "" {
		return
	}
	cloudWorkspaceMountMu.Lock()
	defer cloudWorkspaceMountMu.Unlock()
	cloudWorkspaceByLocal[localPath] = workspaceID
}

func lookupCloudWorkspaceIDByLocalPath(localPath string) string {
	localPath = normalizeProjectSessionPath(localPath)
	if localPath == "" {
		return ""
	}
	cloudWorkspaceMountMu.Lock()
	defer cloudWorkspaceMountMu.Unlock()
	return cloudWorkspaceByLocal[localPath]
}

func (a *App) cloudWorkspaceTenantID() string {
	cfg, err := a.LoadConfig()
	if err != nil {
		return cloudWorkspaceDefaultTenantID
	}
	tenantID := strings.TrimSpace(cfg.RemoteTenantID)
	if tenantID == "" {
		return cloudWorkspaceDefaultTenantID
	}
	return tenantID
}

func (a *App) cloudWorkspaceCachePath(tenantID, workspaceID string) string {
	return filepath.Join(a.GetDataDir(), "cloud-workspaces", tenantID, workspaceID)
}

func parseCloudWorkspaceInUse(data []byte) *cloudWorkspaceInUseError {
	var payload struct {
		Error             json.RawMessage `json:"error"`
		Code              string          `json:"code"`
		HolderMachineID   string          `json:"holder_machine_id"`
		HolderMachineName string          `json:"holder_machine_name"`
		ExpiresAt         string          `json:"expires_at"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return &cloudWorkspaceInUseError{}
	}
	code := strings.TrimSpace(payload.Code)
	if code == "" {
		var errCode string
		if json.Unmarshal(payload.Error, &errCode) == nil {
			code = errCode
		}
	}
	if code != "" && code != "CLOUD_WORKSPACE_IN_USE" {
		return nil
	}
	return &cloudWorkspaceInUseError{
		HolderMachineID:   payload.HolderMachineID,
		HolderMachineName: payload.HolderMachineName,
		ExpiresAt:         payload.ExpiresAt,
	}
}

func (a *App) acquireCloudWorkspaceLease(ctx context.Context, workspaceID string, force bool) (*cloudWorkspaceAcquireOutcome, error) {
	data, status, err := a.cloudWorkspaceHubRequest(ctx, http.MethodPost, cloudWorkspaceLeasesPath(workspaceID), map[string]any{
		"force": force,
	})
	if err != nil {
		return nil, err
	}
	if status == http.StatusConflict {
		if inUse := parseCloudWorkspaceInUse(data); inUse != nil {
			return nil, inUse
		}
		return nil, cloudWorkspaceAPIError(status, data)
	}
	if status >= 300 {
		return nil, cloudWorkspaceAPIError(status, data)
	}
	var out cloudWorkspaceAcquireOutcome
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("invalid cloud workspace lease response: %w", err)
	}
	if strings.TrimSpace(out.LeaseID) == "" {
		return nil, fmt.Errorf("invalid cloud workspace lease response: missing lease_id")
	}
	return &out, nil
}

func (a *App) deleteCloudWorkspaceLease(ctx context.Context, workspaceID, leaseID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	leaseID = strings.TrimSpace(leaseID)
	if workspaceID == "" || leaseID == "" {
		return nil
	}
	data, status, err := a.cloudWorkspaceHubRequest(ctx, http.MethodDelete, cloudWorkspaceLeasePath(workspaceID, leaseID), nil)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound || status == http.StatusConflict {
		return nil
	}
	if status >= 300 {
		return cloudWorkspaceAPIError(status, data)
	}
	return nil
}

func (a *App) confirmCloudWorkspaceSteal(holder string) bool {
	if cloudWorkspaceConfirmStealFn != nil {
		return cloudWorkspaceConfirmStealFn(holder)
	}
	if a == nil || a.ctx == nil {
		return false
	}
	msg := "该云端工作区正被其他设备占用。强制占用将中断对方会话，是否继续？"
	if strings.TrimSpace(holder) != "" {
		msg = fmt.Sprintf("该云端工作区正被占用（%s）。强制占用将中断对方会话，是否继续？", holder)
	}
	sel, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         "云端工作区占用中",
		Message:       msg,
		Buttons:       []string{"强制占用", "取消"},
		DefaultButton: "取消",
		CancelButton:  "取消",
	})
	if err != nil {
		return false
	}
	return sel == "强制占用"
}

func (a *App) confirmCloudWorkspaceDiscardDirty() bool {
	if cloudWorkspaceConfirmDiscardDirtyFn != nil {
		return cloudWorkspaceConfirmDiscardDirtyFn()
	}
	if a == nil || a.ctx == nil {
		return true
	}
	sel, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         "本地缓存与云端不一致",
		Message:       "本地缓存与云端版本不一致。默认丢弃本地更改并拉取云端文件；取消将释放租约且不打开。",
		Buttons:       []string{"丢弃并拉取", "取消"},
		DefaultButton: "丢弃并拉取",
		CancelButton:  "取消",
	})
	if err != nil {
		return false
	}
	return sel != "取消"
}

func stopCloudWorkspaceMount(mount *cloudWorkspaceHeldMount) {
	if mount == nil {
		return
	}
	mount.mu.Lock()
	defer mount.mu.Unlock()
	if mount.hbCancel != nil {
		mount.hbCancel()
		mount.hbCancel = nil
	}
	if mount.pushTimer != nil {
		mount.pushTimer.Stop()
		mount.pushTimer = nil
	}
	if mount.watcher != nil {
		_ = mount.watcher.Close()
		mount.watcher = nil
	}
}

func applyCloudWorkspaceStolen(mount *cloudWorkspaceHeldMount) {
	if mount == nil {
		return
	}
	mount.mu.Lock()
	mount.ReadOnly = true
	if mount.pushTimer != nil {
		mount.pushTimer.Stop()
		mount.pushTimer = nil
	}
	w := mount.watcher
	mount.watcher = nil
	if mount.hbCancel != nil {
		mount.hbCancel()
		mount.hbCancel = nil
	}
	mount.mu.Unlock()
	if w != nil {
		_ = w.Close()
	}
	log.Printf("[cloud_workspace] lease stolen workspace=%s; cache is read-only, skip push", mount.WorkspaceID)
}

func (a *App) startCloudWorkspaceHeartbeat(mount *cloudWorkspaceHeldMount) {
	if cloudWorkspaceBackgroundDisabled || mount == nil || strings.TrimSpace(mount.LeaseID) == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	mount.mu.Lock()
	if mount.hbCancel != nil {
		mount.hbCancel()
	}
	mount.hbCancel = cancel
	interval := cloudWorkspaceHeartbeatIntervalValue
	if interval <= 0 {
		interval = cloudWorkspaceHeartbeatInterval
	}
	leaseID := mount.LeaseID
	workspaceID := mount.WorkspaceID
	mount.mu.Unlock()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				hbCtx, hbCancel := context.WithTimeout(context.Background(), cloudWorkspaceRequestTimeout)
				data, status, err := a.cloudWorkspaceHubRequest(hbCtx, http.MethodPost, cloudWorkspaceLeaseHeartbeatPath(workspaceID, leaseID), nil)
				hbCancel()
				if err != nil {
					log.Printf("[cloud_workspace] heartbeat failed workspace=%s err=%v", workspaceID, err)
					continue
				}
				if status == http.StatusConflict {
					applyCloudWorkspaceStolen(mount)
					return
				}
				if status >= 300 {
					log.Printf("[cloud_workspace] heartbeat status=%d workspace=%s body=%s", status, workspaceID, strings.TrimSpace(string(data)))
				}
			}
		}
	}()
}

func addCloudWorkspaceWatchRecursive(w *fsnotify.Watcher, root string) error {
	if w == nil {
		return nil
	}
	return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if rel != "." && cloudworkspaceIgnoreDir(rel) {
			return filepath.SkipDir
		}
		return w.Add(p)
	})
}

func cloudworkspaceIgnoreDir(rel string) bool {
	return strings.EqualFold(rel, cloudWorkspaceCacheStateDir) ||
		strings.HasPrefix(strings.ToLower(rel), cloudWorkspaceCacheStateDir+"/")
}

func (a *App) scheduleCloudWorkspacePush(mount *cloudWorkspaceHeldMount) {
	if mount == nil {
		return
	}
	mount.mu.Lock()
	defer mount.mu.Unlock()
	if mount.ReadOnly {
		return
	}
	if mount.pushTimer != nil {
		mount.pushTimer.Stop()
	}
	mount.pushTimer = time.AfterFunc(cloudWorkspaceWatchDebounce, func() {
		mount.mu.Lock()
		readOnly := mount.ReadOnly
		root := mount.LocalPath
		id := mount.WorkspaceID
		mount.mu.Unlock()
		if readOnly || strings.TrimSpace(root) == "" {
			return
		}
		ctx, cancel := a.cloudWorkspaceLongContext(2 * time.Minute)
		defer cancel()
		if _, err := a.cloudWorkspaceProtocol(id).Push(ctx, root); err != nil {
			log.Printf("[cloud_workspace] watch push failed workspace=%s err=%v", id, err)
		}
	})
}

func (a *App) startCloudWorkspaceWatcher(mount *cloudWorkspaceHeldMount) {
	if cloudWorkspaceBackgroundDisabled || mount == nil {
		return
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("[cloud_workspace] watcher create failed workspace=%s err=%v", mount.WorkspaceID, err)
		return
	}
	if err := addCloudWorkspaceWatchRecursive(w, mount.LocalPath); err != nil {
		log.Printf("[cloud_workspace] watcher add failed workspace=%s err=%v", mount.WorkspaceID, err)
		_ = w.Close()
		return
	}
	mount.mu.Lock()
	if mount.watcher != nil {
		_ = mount.watcher.Close()
	}
	mount.watcher = w
	root := mount.LocalPath
	mount.mu.Unlock()
	go func() {
		for {
			select {
			case event, ok := <-w.Events:
				if !ok {
					return
				}
				rel, relErr := filepath.Rel(root, event.Name)
				if relErr == nil {
					rel = filepath.ToSlash(rel)
					if cloudworkspaceIgnoreDir(rel) || strings.HasPrefix(strings.ToLower(rel), cloudWorkspaceCacheStateDir+"/") {
						continue
					}
				}
				if event.Has(fsnotify.Create) {
					if info, statErr := os.Stat(event.Name); statErr == nil && info.IsDir() {
						_ = w.Add(event.Name)
					}
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
					a.scheduleCloudWorkspacePush(mount)
				}
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				if err != nil {
					log.Printf("[cloud_workspace] watcher error workspace=%s err=%v", mount.WorkspaceID, err)
				}
			}
		}
	}()
}

func storeCloudWorkspaceMount(mount *cloudWorkspaceHeldMount) {
	if mount == nil {
		return
	}
	cloudWorkspaceMountMu.Lock()
	prev := cloudWorkspaceMounts[mount.WorkspaceID]
	if prev == mount {
		prev = nil
	}
	cloudWorkspaceMounts[mount.WorkspaceID] = mount
	if local := normalizeProjectSessionPath(mount.LocalPath); local != "" {
		cloudWorkspaceByLocal[local] = mount.WorkspaceID
	}
	cloudWorkspaceMountMu.Unlock()
	if prev != nil {
		stopCloudWorkspaceMount(prev)
	}
}

func takeCloudWorkspaceMount(workspaceID string) *cloudWorkspaceHeldMount {
	workspaceID = strings.TrimSpace(workspaceID)
	cloudWorkspaceMountMu.Lock()
	defer cloudWorkspaceMountMu.Unlock()
	mount := cloudWorkspaceMounts[workspaceID]
	delete(cloudWorkspaceMounts, workspaceID)
	if mount != nil {
		local := normalizeProjectSessionPath(mount.LocalPath)
		if local != "" && cloudWorkspaceByLocal[local] == workspaceID {
			delete(cloudWorkspaceByLocal, local)
		}
	}
	return mount
}

func listHeldCloudWorkspaceIDs() []string {
	cloudWorkspaceMountMu.Lock()
	defer cloudWorkspaceMountMu.Unlock()
	out := make([]string, 0, len(cloudWorkspaceMounts))
	for id := range cloudWorkspaceMounts {
		out = append(out, id)
	}
	return out
}

func lookupHeldCloudWorkspace(workspaceID string) *cloudWorkspaceHeldMount {
	workspaceID = strings.TrimSpace(workspaceID)
	cloudWorkspaceMountMu.Lock()
	defer cloudWorkspaceMountMu.Unlock()
	return cloudWorkspaceMounts[workspaceID]
}

func (a *App) lookupCloudWorkspaceIDForProject(projectPath string) string {
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return ""
	}
	if result, ok := lookupCloudWorkspaceTaskByPath(projectPath); ok {
		if id := cloudWorkspaceIDFromTags(result.Tags); id != "" {
			return id
		}
	}
	if id := lookupCloudWorkspaceIDByLocalPath(projectPath); id != "" {
		return id
	}
	if wd := a.recentTaskWorkingDir(projectPath); wd != "" {
		if id := lookupCloudWorkspaceIDByLocalPath(wd); id != "" {
			return id
		}
	}
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return ""
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return ""
	}
	rec := pi.Get(projectPath)
	if rec == nil {
		return ""
	}
	if id := cloudWorkspaceIDFromTags(rec.Tags); id != "" {
		return id
	}
	if wd := recentTaskWorkingDirFromTags(rec.Tags); wd != "" {
		return lookupCloudWorkspaceIDByLocalPath(wd)
	}
	return ""
}

func (a *App) releaseCloudWorkspaceForProjectPath(projectPath string) {
	id := a.lookupCloudWorkspaceIDForProject(projectPath)
	if id == "" {
		return
	}
	ctx, cancel := a.cloudWorkspaceLongContext(2 * time.Minute)
	defer cancel()
	if err := a.releaseCloudWorkspace(ctx, id, true); err != nil {
		log.Printf("[cloud_workspace] release failed workspace=%s path=%q err=%v", id, projectPath, err)
	}
}

func (a *App) releaseCloudWorkspace(ctx context.Context, workspaceID string, bestEffort bool) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil
	}
	mount := takeCloudWorkspaceMount(workspaceID)
	if mount == nil {
		return nil
	}
	stopCloudWorkspaceMount(mount)
	mount.mu.Lock()
	readOnly := mount.ReadOnly
	root := mount.LocalPath
	leaseID := mount.LeaseID
	mount.mu.Unlock()
	if !readOnly && strings.TrimSpace(root) != "" {
		if _, err := a.cloudWorkspaceProtocol(workspaceID).Push(ctx, root); err != nil {
			log.Printf("[cloud_workspace] release push failed workspace=%s err=%v", workspaceID, err)
			if !bestEffort {
				storeCloudWorkspaceMount(mount)
				a.startCloudWorkspaceHeartbeat(mount)
				a.startCloudWorkspaceWatcher(mount)
				return err
			}
		}
	}
	if err := a.deleteCloudWorkspaceLease(ctx, workspaceID, leaseID); err != nil {
		if !bestEffort {
			return err
		}
		log.Printf("[cloud_workspace] release delete lease failed workspace=%s err=%v", workspaceID, err)
	}
	return nil
}

// ReleaseCloudWorkspace pushes the cache (unless stolen/read-only), writes state.json, then DELETE /leases/{id}.
func (a *App) ReleaseCloudWorkspace(workspaceID string) error {
	ctx, cancel := a.cloudWorkspaceLongContext(2 * time.Minute)
	defer cancel()
	return a.releaseCloudWorkspace(ctx, workspaceID, false)
}

func (a *App) releaseAllCloudWorkspaces() {
	ctx, cancel := context.WithTimeout(context.Background(), cloudWorkspaceShutdownReleaseTimeout)
	defer cancel()
	for _, id := range listHeldCloudWorkspaceIDs() {
		_ = a.releaseCloudWorkspace(ctx, id, true)
	}
}

// PrepareCloudWorkspace acquires the exclusive lease, mounts the local cache, and syncs files.
func (a *App) PrepareCloudWorkspace(workspaceID string) (PreparedCloudWorkspace, error) {
	return a.prepareCloudWorkspace(workspaceID, false)
}

func (a *App) prepareCloudWorkspace(workspaceID string, force bool) (PreparedCloudWorkspace, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return PreparedCloudWorkspace{}, fmt.Errorf("workspace id is required")
	}
	cloudWorkspacePrepareMu.Lock()
	defer cloudWorkspacePrepareMu.Unlock()

	tenantID := a.cloudWorkspaceTenantID()
	localPath := normalizeProjectSessionPath(a.cloudWorkspaceCachePath(tenantID, workspaceID))
	if err := os.MkdirAll(localPath, 0o700); err != nil {
		return PreparedCloudWorkspace{}, err
	}

	leaseCtx, leaseCancel := a.cloudWorkspaceRequestContext()
	defer leaseCancel()
	outcome, err := a.acquireCloudWorkspaceLease(leaseCtx, workspaceID, force)
	if err != nil {
		var inUse *cloudWorkspaceInUseError
		if asCloudWorkspaceInUse(err, &inUse) && !force {
			if !a.confirmCloudWorkspaceSteal(inUse.HolderMachineName) {
				return PreparedCloudWorkspace{}, inUse
			}
			forceCtx, forceCancel := a.cloudWorkspaceRequestContext()
			defer forceCancel()
			outcome, err = a.acquireCloudWorkspaceLease(forceCtx, workspaceID, true)
			if err != nil {
				return PreparedCloudWorkspace{}, err
			}
			force = true
		} else {
			return PreparedCloudWorkspace{}, err
		}
	}
	if outcome == nil {
		return PreparedCloudWorkspace{}, fmt.Errorf("cloud workspace lease acquire failed")
	}

	releaseLease := func() {
		ctx, cancel := a.cloudWorkspaceRequestContext()
		defer cancel()
		_ = a.deleteCloudWorkspaceLease(ctx, workspaceID, outcome.LeaseID)
	}

	proto := a.cloudWorkspaceProtocol(workspaceID)
	syncCtx, syncCancel := a.cloudWorkspaceLongContext(2 * time.Minute)
	defer syncCancel()

	acquired := strings.TrimSpace(outcome.Acquired)
	if acquired == cloudWorkspaceAcquiredRenewed {
		if _, err := proto.Push(syncCtx, localPath); err != nil {
			releaseLease()
			return PreparedCloudWorkspace{}, err
		}
	} else {
		remote, err := proto.Transport.GetManifest(syncCtx)
		if err != nil {
			releaseLease()
			return PreparedCloudWorkspace{}, err
		}
		dirty, dirtyErr := cloudWorkspaceCacheDirty(localPath, remote, force)
		if dirtyErr != nil {
			releaseLease()
			return PreparedCloudWorkspace{}, dirtyErr
		}
		if dirty && !a.confirmCloudWorkspaceDiscardDirty() {
			releaseLease()
			return PreparedCloudWorkspace{}, fmt.Errorf("已取消打开云端工作区")
		}
		pulled, err := proto.Pull(syncCtx, localPath)
		if err != nil {
			releaseLease()
			return PreparedCloudWorkspace{}, err
		}
		rev := ""
		if pulled != nil {
			rev = pulled.Revision
		} else if remote != nil {
			rev = remote.Revision
		}
		if err := writeCloudWorkspaceLocalState(localPath, rev); err != nil {
			releaseLease()
			return PreparedCloudWorkspace{}, err
		}
	}

	mount := &cloudWorkspaceHeldMount{
		WorkspaceID: workspaceID,
		LeaseID:     outcome.LeaseID,
		LocalPath:   localPath,
		TenantID:    tenantID,
	}
	storeCloudWorkspaceMount(mount)
	a.startCloudWorkspaceHeartbeat(mount)
	a.startCloudWorkspaceWatcher(mount)
	return PreparedCloudWorkspace{LocalPath: localPath, WorkspaceID: workspaceID}, nil
}

func asCloudWorkspaceInUse(err error, target **cloudWorkspaceInUseError) bool {
	if err == nil || target == nil {
		return false
	}
	inUse, ok := err.(*cloudWorkspaceInUseError)
	if !ok {
		return false
	}
	*target = inUse
	return true
}
