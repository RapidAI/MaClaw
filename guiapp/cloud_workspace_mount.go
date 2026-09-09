package guiapp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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

	"github.com/fsnotify/fsnotify"

	"github.com/RapidAI/CodeClaw/corelib/cloudworkspaceignore"
)

const (
	cloudWorkspaceTagPrefix              = "cloud_workspace:"
	cloudWorkspaceDefaultTenantID        = "tenant_default"
	cloudWorkspaceHeartbeatInterval      = 30 * time.Second
	cloudWorkspaceShutdownReleaseTimeout = 8 * time.Second
	cloudWorkspaceWatchDebounce          = time.Second
	cloudWorkspaceFilesChangedEvent      = "cloud-workspace-files-changed"
	cloudWorkspaceSyncProgressEvent      = "cloud-workspace-sync-progress"
)

// PreparedCloudWorkspace is the PrepareCloudWorkspace result.
// LocalPath is the cache working directory; WorkspaceID is never inferred from the path.
type PreparedCloudWorkspace struct {
	LocalPath   string `json:"local_path"`
	WorkspaceID string `json:"workspace_id"`
}

type cloudWorkspaceAcquireOutcome struct {
	LeaseID          string `json:"lease_id"`
	ExpiresAt        string `json:"expires_at"`
	Acquired         string `json:"acquired"`
	FencingToken     int64  `json:"fencing_token"`
	ClientInstanceID string `json:"client_instance_id,omitempty"`
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
	WorkspaceID           string
	LeaseID               string
	FencingToken          int64
	LeaseExpiresAt        string
	LastCommittedRevision string
	processLockPath       string
	processLockOwner      string
	LocalPath             string
	TenantID              string
	ReadOnly              bool
	isolatedReadOnly      bool
	reconcileRequired     bool
	// Shared is retained for compatibility with older in-memory callers. The
	// v1-sequential protocol never creates a shared writer mount.
	Shared bool

	hbCancel  context.CancelFunc
	watcher   *fsnotify.Watcher
	pushTimer *time.Timer
	// bandwidthTimer schedules the single window-reset retry after an hourly
	// bandwidth 429; it is coalesced and stopped with the mount lifecycle.
	bandwidthTimer *time.Timer
	mu             sync.Mutex
	// syncMu serializes Pull/Push/sidecar flush/Release for this workspace.
	// Different workspaces use different mutexes and may proceed concurrently.
	syncMu    sync.Mutex
	releasing bool
	// syncCancel cancels the currently running background reconcile/upload.
	// syncRunning/syncPending implement a one-item coalescing queue: watcher
	// bursts never create an unbounded goroutine backlog, while a change that
	// arrives during an upload schedules exactly one follow-up pass.
	syncCancel  context.CancelFunc
	syncRunning bool
	syncPending bool
	syncDone    chan struct{}
	// stopped prevents a callback that is just finishing while the mount is
	// being detached from scheduling a new debounce timer after its process
	// lock has been released.
	stopped bool
}

// acquireCloudWorkspaceProcessLock provides a cross-process writer guard for
// a workspace cache. O_EXCL makes the handoff atomic; a stale lock is reclaimed
// only after a conservative TTL so a crashed GUI cannot brick the workspace.
func acquireCloudWorkspaceProcessLock(path string) (string, error) {
	lockPath, _, err := acquireCloudWorkspaceProcessLockOwned(path)
	return lockPath, err
}

func acquireCloudWorkspaceProcessLockOwned(path string) (string, string, error) {
	// The lock lives under .maclaw-cloud.  Validate/create that directory
	// component-by-component before MkdirAll so a damaged profile cannot turn
	// the lock operation itself into a symlink traversal.
	cacheRoot := normalizeProjectSessionPath(path)
	if cacheRoot == "" {
		return "", "", fmt.Errorf("cloud workspace cache path is empty")
	}
	if err := ensureCloudWorkspaceCacheDirectory(cacheRoot, filepath.Join(cacheRoot, cloudWorkspaceCacheStateDir)); err != nil {
		return "", "", err
	}
	path = filepath.Join(path, ".maclaw-cloud", "writer.lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", "", err
	}
	owner, err := newCloudWorkspaceProcessLockOwner()
	if err != nil {
		return "", "", err
	}
	guard, err := acquireCloudWorkspaceProcessLockGuard(path)
	if err != nil {
		return "", "", err
	}
	defer releaseCloudWorkspaceProcessLockGuard(guard)
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			if _, writeErr := fmt.Fprintf(f, "%s\n", owner); writeErr != nil {
				_ = f.Close()
				_ = os.Remove(path)
				return "", "", writeErr
			}
			if closeErr := f.Close(); closeErr != nil {
				_ = os.Remove(path)
				return "", "", closeErr
			}
			return path, owner, nil
		}
		if !os.IsExist(err) {
			return "", "", err
		}
		info, statErr := os.Stat(path)
		if statErr != nil || time.Since(info.ModTime()) < 2*cloudWorkspaceHeartbeatInterval {
			return "", "", fmt.Errorf("cloud workspace is already open by another process")
		}
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return "", "", fmt.Errorf("stale cloud workspace lock: %w", removeErr)
		}
	}
	return "", "", fmt.Errorf("cloud workspace process lock unavailable")
}

func newCloudWorkspaceProcessLockOwner() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate cloud workspace process lock owner: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func releaseCloudWorkspaceProcessLock(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	guard, err := acquireCloudWorkspaceProcessLockGuard(path)
	if err != nil {
		return
	}
	defer releaseCloudWorkspaceProcessLockGuard(guard)
	_ = os.Remove(path)
}

// releaseCloudWorkspaceProcessLockOwned only removes the lock when the file
// still contains this process's owner token. This prevents a delayed cleanup
// from deleting a successor's lock after stale-lock takeover.
func releaseCloudWorkspaceProcessLockOwned(path, owner string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	if strings.TrimSpace(owner) == "" {
		releaseCloudWorkspaceProcessLock(path)
		return
	}
	guard, err := acquireCloudWorkspaceProcessLockGuard(path)
	if err != nil {
		// A concurrent acquire/takeover owns the guard.  Failing closed is safer
		// than risking removal of a successor's lock file.
		return
	}
	defer releaseCloudWorkspaceProcessLockGuard(guard)
	raw, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(raw)) != owner {
		return
	}
	_ = os.Remove(path)
}

// touchCloudWorkspaceProcessLock refreshes the liveness timestamp written by
// acquireCloudWorkspaceProcessLock. A process lock is intentionally
// recoverable after a crash, but a lock that is only created once would look
// stale after a long-lived GUI session even while its lease heartbeat is
// healthy. The heartbeat calls this helper periodically so another process
// cannot reclaim an active cache merely because it has been open for a long
// time.
func touchCloudWorkspaceProcessLock(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	now := time.Now()
	return os.Chtimes(path, now, now)
}

func touchCloudWorkspaceProcessLockOwned(path, owner string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if strings.TrimSpace(owner) == "" {
		return touchCloudWorkspaceProcessLock(path)
	}
	guard, err := acquireCloudWorkspaceProcessLockGuard(path)
	if err != nil {
		return err
	}
	defer releaseCloudWorkspaceProcessLockGuard(guard)
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(raw)) != owner {
		return fmt.Errorf("cloud workspace process lock owner changed")
	}
	return touchCloudWorkspaceProcessLock(path)
}

// acquireCloudWorkspaceProcessLockGuard serializes lock-file inspection,
// stale takeover and owner-scoped cleanup across cooperating GUI processes.
// O_EXCL gives us an atomic guard without relying on platform-specific flock
// APIs; the same conservative TTL makes a crashed guard recoverable.
func acquireCloudWorkspaceProcessLockGuard(lockPath string) (string, error) {
	guardPath := lockPath + ".guard"
	// Heartbeat and release can legitimately overlap for a few milliseconds;
	// retry briefly before treating the guard as unavailable.  This avoids
	// demoting a healthy writer (or leaking its lock) because of a transient
	// in-process race while still keeping the operation bounded.
	for attempt := 0; attempt < 8; attempt++ {
		f, err := os.OpenFile(guardPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			if closeErr := f.Close(); closeErr != nil {
				_ = os.Remove(guardPath)
				return "", closeErr
			}
			return guardPath, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
		info, statErr := os.Stat(guardPath)
		if statErr != nil || time.Since(info.ModTime()) < 2*cloudWorkspaceHeartbeatInterval {
			if attempt < 7 {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			return "", fmt.Errorf("cloud workspace process lock guard unavailable")
		}
		if removeErr := os.Remove(guardPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return "", fmt.Errorf("stale cloud workspace lock guard: %w", removeErr)
		}
	}
	return "", fmt.Errorf("cloud workspace process lock guard unavailable")
}

func releaseCloudWorkspaceProcessLockGuard(guardPath string) {
	if strings.TrimSpace(guardPath) != "" {
		_ = os.Remove(guardPath)
	}
}

var (
	cloudWorkspaceMountMu        sync.Mutex
	cloudWorkspaceMounts         = map[string]*cloudWorkspaceHeldMount{}
	cloudWorkspaceByLocal        = map[string]string{}
	cloudWorkspacePrepareLocksMu sync.Mutex
	cloudWorkspacePrepareLocks   = map[string]*sync.Mutex{}

	cloudWorkspaceBackgroundDisabled     bool
	cloudWorkspaceConfirmStealFn         func(holder string) bool
	cloudWorkspaceConfirmDiscardDirtyFn  func() bool
	cloudWorkspaceHeartbeatIntervalValue = cloudWorkspaceHeartbeatInterval
)

func lockCloudWorkspacePrepare(workspaceID string) func() {
	workspaceID = strings.TrimSpace(workspaceID)
	cloudWorkspacePrepareLocksMu.Lock()
	lock := cloudWorkspacePrepareLocks[workspaceID]
	if lock == nil {
		lock = &sync.Mutex{}
		cloudWorkspacePrepareLocks[workspaceID] = lock
	}
	cloudWorkspacePrepareLocksMu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func resetCloudWorkspaceMounts() {
	cloudWorkspaceMountMu.Lock()
	mounts := cloudWorkspaceMounts
	cloudWorkspaceMounts = map[string]*cloudWorkspaceHeldMount{}
	cloudWorkspaceByLocal = map[string]string{}
	cloudWorkspaceMountMu.Unlock()
	for _, mount := range mounts {
		stopCloudWorkspaceMount(mount)
	}
	resetCloudWorkspaceSidecarState()
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
	return safeCloudWorkspaceCacheComponent(cfg.RemoteTenantID, cloudWorkspaceDefaultTenantID, "tenant")
}

func (a *App) cloudWorkspaceCachePath(tenantID, workspaceID string) string {
	tenantID = safeCloudWorkspaceCacheComponent(tenantID, cloudWorkspaceDefaultTenantID, "tenant")
	workspaceID = safeCloudWorkspaceCacheComponent(workspaceID, "workspace_invalid", "workspace")
	return filepath.Join(a.GetDataDir(), "cloud-workspaces", tenantID, workspaceID)
}

func (a *App) cloudWorkspaceReadOnlyCachePath(tenantID, workspaceID string) string {
	tenantID = safeCloudWorkspaceCacheComponent(tenantID, cloudWorkspaceDefaultTenantID, "tenant")
	workspaceID = safeCloudWorkspaceCacheComponent(workspaceID, "workspace_invalid", "workspace")
	return filepath.Join(a.GetDataDir(), "cloud-workspaces-readonly", tenantID, workspaceID, cloudWorkspaceClientInstanceID())
}

// safeCloudWorkspaceCacheComponent keeps every locally derived cache path in
// a single path segment. Hub-issued IDs are normally ASCII cws_*/tenant_*
// values; hashing an unexpected tenant value preserves isolation without
// placing untrusted separators or Windows device syntax in the filesystem.
// Public workspace operations still reject invalid IDs before reaching this
// defense-in-depth fallback.
func safeCloudWorkspaceCacheComponent(value, fallback, kind string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if validCloudWorkspaceCacheID(value) {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	return kind + "_sha256_" + hex.EncodeToString(sum[:8])
}

func isCloudWorkspaceCachePath(dataDir, path string) bool {
	root := normalizeProjectSessionPath(filepath.Join(dataDir, "cloud-workspaces"))
	return cloudWorkspacePathInsideRoot(root, path)
}

func cloudWorkspacePathInsideRoot(root, path string) bool {
	root = normalizeProjectSessionPath(root)
	path = normalizeProjectSessionPath(path)
	if root == "" || path == "" {
		return false
	}
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

func cloudWorkspacePathResolvesInsideRoot(root, path string) bool {
	resolvedRoot, rootErr := filepath.EvalSymlinks(root)
	resolvedPath, pathErr := filepath.EvalSymlinks(path)
	if rootErr != nil || pathErr != nil {
		return false
	}
	return cloudWorkspacePathInsideRoot(resolvedRoot, resolvedPath)
}

// ensureCloudWorkspaceCacheDirectory creates a cache path one component at a
// time and refuses symlink/reparse-point components.  A lexical path check is
// insufficient here: an attacker (or a damaged profile) could replace the
// tenant directory with a symlink between MkdirAll and Pull/Push and redirect
// the working tree outside the dedicated cloud-workspace root.
func ensureCloudWorkspaceCacheDirectory(root, target string) error {
	root = normalizeProjectSessionPath(root)
	target = normalizeProjectSessionPath(target)
	if root == "" || target == "" || !cloudWorkspacePathInsideRoot(root, target) {
		return fmt.Errorf("cloud workspace cache path escapes its root")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return fmt.Errorf("cloud workspace cache root is not a directory")
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("cloud workspace cache path escapes its root")
	}
	if rel == "." {
		return nil
	}
	current := root
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			if mkErr := os.Mkdir(current, 0o700); mkErr != nil && !os.IsExist(mkErr) {
				return mkErr
			}
			info, statErr = os.Lstat(current)
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("cloud workspace cache path contains an unsafe component")
		}
		if !cloudWorkspacePathResolvesInsideRoot(root, current) {
			return fmt.Errorf("cloud workspace cache path resolves outside its root")
		}
	}
	return nil
}

func cloudWorkspaceCacheDirectoryExistsAndSafe(root, target string) error {
	root = normalizeProjectSessionPath(root)
	target = normalizeProjectSessionPath(target)
	if root == "" || target == "" || !cloudWorkspacePathInsideRoot(root, target) {
		return fmt.Errorf("cloud workspace cache path escapes its root")
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return fmt.Errorf("cloud workspace cache root is not a directory")
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("cloud workspace cache path escapes its root")
	}
	if rel == "." {
		return nil
	}
	current := root
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !cloudWorkspacePathResolvesInsideRoot(root, current) {
			return fmt.Errorf("cloud workspace cache path contains an unsafe component")
		}
	}
	return nil
}

func (a *App) isAppCloudWorkspaceCachePath(path string) bool {
	if a == nil {
		return false
	}
	return isCloudWorkspaceCachePath(a.GetDataDir(), path)
}

// cloudWorkspaceExecutionDir is this task's cache mount (or a directory inside it).
func (a *App) cloudWorkspaceExecutionDir(projectPath string) string {
	if a == nil {
		return ""
	}
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return ""
	}
	canonical := ""
	if id := a.lookupCloudWorkspaceIDForProject(projectPath); id != "" {
		canonical = normalizeProjectSessionPath(a.cloudWorkspaceCachePath(a.cloudWorkspaceTenantID(), id))
	}
	if wd := a.recentTaskWorkingDir(projectPath); canonical != "" && cloudWorkspacePathInsideRoot(canonical, wd) {
		return wd
	}
	if canonical != "" {
		return canonical
	}
	if a.isAppCloudWorkspaceCachePath(projectPath) {
		return projectPath
	}
	return ""
}

func (a *App) seedCloudWorkspaceTabWorkingDir(session *TabSessionData, projectPath string) bool {
	if session == nil {
		return false
	}
	cloudDir := a.cloudWorkspaceExecutionDir(projectPath)
	if cloudDir == "" {
		return false
	}
	current := normalizeProjectSessionPath(session.WorkingDir)
	if cloudWorkspacePathInsideRoot(cloudDir, current) {
		return false
	}
	session.WorkingDir = cloudDir
	return true
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
	body := map[string]any{"force": force}
	// Acquire is already transactionally repeatable for the same live instance:
	// a lost response is recovered as a renew of the same lease id/epoch. A
	// payload-derived idempotency key would instead replay a released or expired
	// lease for 24 hours, preventing recovery from a network partition.
	data, status, err := a.cloudWorkspaceHubDo(ctx, http.MethodPost, cloudWorkspaceLeasesPath(workspaceID), cloudWorkspaceHTTPOptions{jsonBody: body, accept: "application/json"})
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
	return a.deleteCloudWorkspaceLeaseWithRevisionAndToken(ctx, workspaceID, leaseID, "", 0)
}

func (a *App) deleteCloudWorkspaceLeaseWithRevision(ctx context.Context, workspaceID, leaseID, lastRevision string) error {
	return a.deleteCloudWorkspaceLeaseWithRevisionAndToken(ctx, workspaceID, leaseID, lastRevision, 0)
}

func (a *App) deleteCloudWorkspaceLeaseWithRevisionAndToken(ctx context.Context, workspaceID, leaseID, lastRevision string, fencingToken int64) error {
	workspaceID = strings.TrimSpace(workspaceID)
	leaseID = strings.TrimSpace(leaseID)
	if workspaceID == "" || leaseID == "" {
		return nil
	}
	headers := map[string]string{}
	if strings.TrimSpace(lastRevision) == "" {
		if mount := lookupHeldCloudWorkspace(workspaceID); mount != nil {
			mount.mu.Lock()
			lastRevision = mount.LastCommittedRevision
			mount.mu.Unlock()
		}
	}
	if strings.TrimSpace(lastRevision) != "" {
		headers["X-Cloud-Workspace-Last-Revision"] = lastRevision
	}
	if fencingToken <= 0 {
		if mount := lookupHeldCloudWorkspace(workspaceID); mount != nil {
			mount.mu.Lock()
			fencingToken = mount.FencingToken
			mount.mu.Unlock()
		}
	}
	if fencingToken > 0 {
		headers["X-Cloud-Workspace-Fencing"] = strconv.FormatInt(fencingToken, 10)
	}
	headers["Idempotency-Key"] = cloudWorkspaceIdempotencyKey("lease-release-"+leaseID, []byte(lastRevision))
	data, status, err := a.cloudWorkspaceHubDo(ctx, http.MethodDelete, cloudWorkspaceLeasePath(workspaceID, leaseID), cloudWorkspaceHTTPOptions{headers: headers, accept: "application/json"})
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
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
	msg := "该云端工作区正被其他设备占用。强制占用将中断对方会话，是否继续？"
	if strings.TrimSpace(holder) != "" {
		msg = fmt.Sprintf("该云端工作区正被占用（%s）。强制占用将中断对方会话，是否继续？", holder)
	}
	return a.askFrontendConfirm("云端工作区占用中", msg, "强制占用", "取消", true, false)
}

func (a *App) confirmCloudWorkspaceDiscardDirty() bool {
	if cloudWorkspaceConfirmDiscardDirtyFn != nil {
		return cloudWorkspaceConfirmDiscardDirtyFn()
	}
	return a.askFrontendConfirm(
		"本地缓存与云端不一致",
		"本地文件与云端版本不一致。采用云端会覆盖本机未同步的更改；取消将释放租约且不打开。",
		"采用云端",
		"取消",
		true,
		false,
	)
}

func (a *App) confirmCloudWorkspaceKeepLocal() bool {
	if cloudWorkspaceConfirmDiscardDirtyFn != nil {
		return cloudWorkspaceConfirmDiscardDirtyFn()
	}
	return a.askFrontendConfirm(
		"本地缓存与云端不一致",
		"本地文件与云端版本不一致。保留本地会把当前文件同步到云端；取消将释放租约且不打开。",
		"保留本地并同步",
		"取消",
		false,
		true,
	)
}

func stopCloudWorkspaceMount(mount *cloudWorkspaceHeldMount) {
	stopCloudWorkspaceMountWithLock(mount, false)
}

// stopCloudWorkspaceMountForRecovery stops background activity but keeps the
// local writer lock. ReleaseCloudWorkspace uses this variant while flushing;
// if a network/CAS failure occurs and the mount is restored, another GUI
// process on the same machine must not acquire the cache concurrently.
func stopCloudWorkspaceMountForRecovery(mount *cloudWorkspaceHeldMount) {
	stopCloudWorkspaceMountWithLock(mount, true)
}

func stopCloudWorkspaceMountWithLock(mount *cloudWorkspaceHeldMount, preserveProcessLock bool) {
	if mount == nil {
		return
	}
	mount.mu.Lock()
	mount.stopped = true
	done := mount.syncDone
	if mount.syncCancel != nil {
		mount.syncCancel()
		mount.syncCancel = nil
	}
	mount.syncPending = false
	mount.mu.Unlock()
	// Do not release a writer lock while a canceled callback is still inside
	// pushCloudWorkspace.  Otherwise a successor process could acquire the
	// same cache before the old callback has stopped issuing requests.
	if done != nil {
		<-done
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
	if mount.bandwidthTimer != nil {
		mount.bandwidthTimer.Stop()
		mount.bandwidthTimer = nil
	}
	if mount.watcher != nil {
		_ = mount.watcher.Close()
		mount.watcher = nil
	}
	if !preserveProcessLock && mount.processLockPath != "" {
		releaseCloudWorkspaceProcessLockOwned(mount.processLockPath, mount.processLockOwner)
		mount.processLockPath = ""
		mount.processLockOwner = ""
	}
	if mount.isolatedReadOnly && strings.TrimSpace(mount.LocalPath) != "" {
		localPath := normalizeProjectSessionPath(mount.LocalPath)
		root := cloudWorkspaceCacheRootForPath(localPath, "cloud-workspaces-readonly")
		if root != "" && cloudWorkspacePathResolvesInsideRoot(root, localPath) {
			_ = os.RemoveAll(localPath)
		} else {
			// A replaced tenant/workspace component may point outside the
			// read-only root. Never follow it during cleanup; ForceDelete or a
			// later explicit purge will surface the unsafe path to the caller.
			log.Printf("[cloud_workspace] refusing unsafe read-only cache cleanup path=%q", localPath)
		}
	}
}

func cloudWorkspaceCacheRootForPath(path, rootName string) string {
	path = normalizeProjectSessionPath(path)
	rootName = strings.TrimSpace(rootName)
	if path == "" || rootName == "" {
		return ""
	}
	for current := path; current != ""; {
		if filepath.Base(current) == rootName {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return ""
}

// restoreCloudWorkspaceMountAfterReleaseFailure re-arms a mount that was
// quiesced for the release flush but had to remain held after a transient
// network/CAS failure.  stopCloudWorkspaceMountForRecovery marks it stopped;
// forgetting to clear that bit would leave the lease alive but silently stop
// watcher-driven uploads forever.
func (a *App) restoreCloudWorkspaceMountAfterReleaseFailure(mount *cloudWorkspaceHeldMount) {
	if mount == nil {
		return
	}
	mount.mu.Lock()
	mount.stopped = false
	readOnly := mount.ReadOnly
	mount.releasing = false
	mount.mu.Unlock()
	if !readOnly {
		a.startCloudWorkspaceHeartbeat(mount)
		a.startCloudWorkspaceWatcher(mount)
	}
}

func applyCloudWorkspaceStolen(mount *cloudWorkspaceHeldMount) {
	applyCloudWorkspaceStolenWithWait(mount, true)
}

// applyCloudWorkspaceStolenNoWait is used by the sync callback itself.  The
// callback owns syncDone until it returns, so waiting for that channel from
// inside the callback would deadlock forever.  Heartbeat/fencing callers use
// the public waiting form above to ensure no upload remains in flight before a
// successor can reuse the cache.
func applyCloudWorkspaceStolenNoWait(mount *cloudWorkspaceHeldMount) {
	applyCloudWorkspaceStolenWithWait(mount, false)
}

func applyCloudWorkspaceStolenWithWait(mount *cloudWorkspaceHeldMount, waitForSync bool) {
	if mount == nil {
		return
	}
	mount.mu.Lock()
	mount.ReadOnly = true
	mount.stopped = true
	if mount.pushTimer != nil {
		mount.pushTimer.Stop()
		mount.pushTimer = nil
	}
	if mount.bandwidthTimer != nil {
		mount.bandwidthTimer.Stop()
		mount.bandwidthTimer = nil
	}
	if mount.syncCancel != nil {
		mount.syncCancel()
		mount.syncCancel = nil
	}
	mount.syncPending = false
	done := mount.syncDone
	w := mount.watcher
	mount.watcher = nil
	if mount.hbCancel != nil {
		mount.hbCancel()
		mount.hbCancel = nil
	}
	mount.mu.Unlock()
	if waitForSync && done != nil {
		<-done
	}
	if w != nil {
		_ = w.Close()
	}
	log.Printf("[cloud_workspace] lease stolen workspace=%s; cache is read-only, skip push", mount.WorkspaceID)
}

func applyCloudWorkspaceLeaseExpired(mount *cloudWorkspaceHeldMount) {
	if mount == nil {
		return
	}
	applyCloudWorkspaceStolen(mount)
	log.Printf("[cloud_workspace] lease expired while offline workspace=%s; cache is read-only", mount.WorkspaceID)
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
		beat := func() bool {
			mount.mu.Lock()
			processLockPath := mount.processLockPath
			processLockOwner := mount.processLockOwner
			mount.mu.Unlock()
			if err := touchCloudWorkspaceProcessLockOwned(processLockPath, processLockOwner); err != nil {
				// Losing the local lock means another process may have reclaimed
				// this cache. Stop all writes before continuing the remote lease
				// heartbeat; the next explicit Prepare will reconcile safely.
				log.Printf("[cloud_workspace] process lock heartbeat failed workspace=%s err=%v", workspaceID, err)
				mount.mu.Lock()
				// Do not let cleanup remove a lock file that may now belong to
				// the successor process which reclaimed this cache.
				mount.processLockPath = ""
				mount.processLockOwner = ""
				mount.mu.Unlock()
				applyCloudWorkspaceStolen(mount)
				return false
			}
			hbCtx, hbCancel := context.WithTimeout(context.Background(), cloudWorkspaceRequestTimeout)
			data, status, err := a.cloudWorkspaceHubRequest(hbCtx, http.MethodPost, cloudWorkspaceLeaseHeartbeatPath(workspaceID, leaseID), nil)
			hbCancel()
			if err != nil {
				log.Printf("[cloud_workspace] heartbeat failed workspace=%s err=%v", workspaceID, err)
				mount.mu.Lock()
				expiresAt := mount.LeaseExpiresAt
				mount.mu.Unlock()
				if deadline, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(expiresAt)); parseErr == nil && !time.Now().UTC().Before(deadline) {
					applyCloudWorkspaceLeaseExpired(mount)
					return false
				}
				return true
			}
			if status == http.StatusConflict {
				applyCloudWorkspaceStolen(mount)
				return false
			}
			if status >= 300 {
				log.Printf("[cloud_workspace] heartbeat status=%d workspace=%s body=%s", status, workspaceID, strings.TrimSpace(string(data)))
				mount.mu.Lock()
				expiresAt := mount.LeaseExpiresAt
				mount.mu.Unlock()
				if deadline, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(expiresAt)); parseErr == nil && !time.Now().UTC().Before(deadline) {
					applyCloudWorkspaceLeaseExpired(mount)
					return false
				}
				return true
			}
			var renewed cloudWorkspaceAcquireOutcome
			if json.Unmarshal(data, &renewed) == nil && strings.TrimSpace(renewed.ExpiresAt) != "" {
				mount.mu.Lock()
				mount.LeaseExpiresAt = renewed.ExpiresAt
				mount.mu.Unlock()
			}
			return true
		}
		if !beat() {
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !beat() {
					return
				}
			}
		}
	}()
}

func addCloudWorkspaceWatchRecursive(w *fsnotify.Watcher, root string) error {
	if w == nil {
		return nil
	}
	cloudignore, err := cloudworkspaceignore.ReadCloudignore(root)
	if err != nil {
		return err
	}
	return addCloudWorkspaceWatchFrom(w, root, root, cloudworkspaceignore.NewMatcher(cloudignore))
}

func addCloudWorkspaceWatchFrom(w *fsnotify.Watcher, workspaceRoot, start string, matcher *cloudworkspaceignore.Matcher) error {
	if w == nil {
		return nil
	}
	if matcher == nil {
		matcher = cloudworkspaceignore.NewMatcher("")
	}
	return filepath.WalkDir(start, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(workspaceRoot, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if rel != "." && matcher.ShouldIgnore(rel, true) {
			return filepath.SkipDir
		}
		return w.Add(p)
	})
}

func cloudWorkspaceWatchIgnored(rel string, isDir bool, matcher *cloudworkspaceignore.Matcher) bool {
	rel = strings.TrimSpace(filepath.ToSlash(rel))
	if rel == "" || rel == "." {
		return false
	}
	if matcher == nil {
		matcher = cloudworkspaceignore.NewMatcher("")
	}
	return matcher.ShouldIgnore(rel, isDir)
}

func (a *App) scheduleCloudWorkspacePush(mount *cloudWorkspaceHeldMount) {
	if mount == nil {
		return
	}
	mount.mu.Lock()
	if mount.ReadOnly || mount.releasing || mount.stopped {
		mount.mu.Unlock()
		return
	}
	if mount.syncRunning {
		mount.syncPending = true
		mount.mu.Unlock()
		return
	}
	if mount.pushTimer != nil {
		mount.pushTimer.Stop()
	}
	mount.pushTimer = time.AfterFunc(cloudWorkspaceWatchDebounce, func() {
		mount.syncMu.Lock()
		defer mount.syncMu.Unlock()
		done := make(chan struct{})
		mount.mu.Lock()
		mount.pushTimer = nil
		readOnly := mount.ReadOnly
		releasing := mount.releasing
		root := mount.LocalPath
		id := mount.WorkspaceID
		if readOnly || releasing || strings.TrimSpace(root) == "" {
			mount.mu.Unlock()
			return
		}
		mount.syncRunning = true
		mount.syncPending = false
		mount.syncDone = done
		mount.mu.Unlock()
		defer func() {
			// Keep syncDone published until every callback action (including a
			// coalesced follow-up decision) has completed.  A detacher that observes
			// a nil channel in this small tail window could otherwise release the
			// process lock while this callback is still able to schedule work.
			close(done)
			mount.mu.Lock()
			if mount.syncDone == done {
				mount.syncDone = nil
			}
			mount.mu.Unlock()
		}()
		a.emitEvent(cloudWorkspaceFilesChangedEvent, map[string]string{
			"workspace_id": id,
			"path":         root,
		})
		ctx, cancel := a.cloudWorkspaceSyncContext()
		mount.mu.Lock()
		mount.syncCancel = cancel
		mount.mu.Unlock()
		var lastProgressAt time.Time
		progress := func(p cloudWorkspaceSyncProgress) {
			now := time.Now()
			// Large trees can produce tens of thousands of scan callbacks.  Keep
			// the UI observable without turning progress itself into an event
			// flood; terminal state is emitted separately below.
			if !lastProgressAt.IsZero() && now.Sub(lastProgressAt) < 100*time.Millisecond && p.FilesDone%100 != 0 {
				return
			}
			lastProgressAt = now
			payload := map[string]any{"workspace_id": id, "phase": p.Phase, "files_done": p.FilesDone, "files_total": p.FilesTotal, "bytes_done": p.BytesDone, "bytes_total": p.BytesTotal, "cancelable": p.Cancelable, "status": "running"}
			a.emitEvent(cloudWorkspaceSyncProgressEvent, payload)
		}
		_, err := a.pushCloudWorkspaceWithProgress(ctx, id, root, progress)
		cancel()
		mount.mu.Lock()
		mount.syncCancel = nil
		mount.syncRunning = false
		pending := mount.syncPending
		mount.syncPending = false
		mount.mu.Unlock()
		if err != nil {
			log.Printf("[cloud_workspace] watch push failed workspace=%s err=%v", id, err)
			if errors.Is(err, errCloudWorkspaceFenced) {
				applyCloudWorkspaceStolenNoWait(mount)
			}
			status := "failed"
			if errors.Is(err, context.Canceled) {
				status = "canceled"
			}
			a.emitEvent(cloudWorkspaceSyncProgressEvent, map[string]any{"workspace_id": id, "phase": "done", "status": status, "error": err.Error(), "cancelable": false})
			mount.mu.Lock()
			mount.reconcileRequired = true
			mount.mu.Unlock()
			if strings.TrimSpace(root) != "" && !mount.isolatedReadOnly {
				if _, persistErr := markCloudWorkspaceLocalReconcileRequired(root, err.Error()); persistErr != nil {
					log.Printf("[cloud_workspace] persist canceled reconcile marker failed workspace=%s err=%v", id, persistErr)
				}
			}
		} else {
			a.emitEvent(cloudWorkspaceSyncProgressEvent, map[string]any{"workspace_id": id, "phase": "done", "status": "completed", "cancelable": false})
			mount.mu.Lock()
			mount.reconcileRequired = false
			mount.mu.Unlock()
		}
		if pending || err != nil {
			if delay, limited := cloudWorkspaceBandwidthRetryDelay(err); limited {
				// Hourly bandwidth quota hit: keep reconcile_required, skip the
				// immediate pending reschedule, and fire exactly one retry after
				// the server-provided window reset instead of a hot error loop.
				mount.mu.Lock()
				if mount.bandwidthTimer != nil {
					mount.bandwidthTimer.Stop()
					mount.bandwidthTimer = nil
				}
				if !mount.stopped && !mount.releasing && !mount.ReadOnly {
					mount.bandwidthTimer = time.AfterFunc(delay, func() {
						mount.mu.Lock()
						mount.bandwidthTimer = nil
						mount.mu.Unlock()
						a.scheduleCloudWorkspacePush(mount)
					})
				}
				mount.mu.Unlock()
			} else if pending {
				a.scheduleCloudWorkspacePush(mount)
			}
		}
	})
	mount.mu.Unlock()
}

// CancelCloudWorkspaceSync asks the active writer's background sync to stop at
// its next cancellable boundary.  It is deliberately advisory: an in-flight
// HTTP request may finish or return its own timeout, but no follow-up upload
// starts after cancellation.
func (a *App) CancelCloudWorkspaceSync(workspaceID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if !validCloudWorkspaceCacheID(workspaceID) {
		return fmt.Errorf("workspace id is required")
	}
	mount := lookupHeldCloudWorkspace(workspaceID)
	if mount == nil {
		return fmt.Errorf("cloud workspace is not mounted")
	}
	mount.mu.Lock()
	if mount.pushTimer != nil {
		mount.pushTimer.Stop()
		mount.pushTimer = nil
	}
	if mount.bandwidthTimer != nil {
		mount.bandwidthTimer.Stop()
		mount.bandwidthTimer = nil
	}
	mount.syncPending = false
	cancel := mount.syncCancel
	mount.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// markCloudWorkspaceReconcileRequired records that fsnotify may have dropped
// events (for example queue overflow or a watcher backend error). A full
// manifest scan is scheduled while the writer is still active; this makes the
// failure explicit and self-healing instead of silently losing a file change.
func (a *App) markCloudWorkspaceReconcileRequired(mount *cloudWorkspaceHeldMount, reason error) {
	if mount == nil {
		return
	}
	mount.mu.Lock()
	wasRequired := mount.reconcileRequired
	mount.reconcileRequired = true
	readOnly := mount.ReadOnly
	isolatedReadOnly := mount.isolatedReadOnly
	root := mount.LocalPath
	id := mount.WorkspaceID
	mount.mu.Unlock()
	reasonText := ""
	if reason != nil {
		reasonText = reason.Error()
		log.Printf("[cloud_workspace] watcher requires reconciliation workspace=%s err=%v", id, reason)
	}
	// Keep the marker in the writer cache across process restarts.  A read-only
	// cache is isolated and deleted on close, so persisting there would create
	// a misleading durable signal for a directory that is not a writer source.
	if !isolatedReadOnly && strings.TrimSpace(root) != "" {
		// Serialize the state-file update with Push/Pull/Release.  Without this
		// short critical section a watcher error racing a successful commit could
		// have its marker overwritten by the commit's baseline write (or vice
		// versa), making the restart decision nondeterministic.
		mount.syncMu.Lock()
		if _, persistErr := markCloudWorkspaceLocalReconcileRequired(root, reasonText); persistErr != nil {
			log.Printf("[cloud_workspace] persist reconcile marker failed workspace=%s err=%v", id, persistErr)
		}
		mount.syncMu.Unlock()
	}
	if !wasRequired && !readOnly && a != nil {
		// A toast makes the self-healing transition visible even when the cloud
		// file tree is not currently focused.  It is informational only: the
		// mount remains writable while the queued full scan is attempted.
		a.ShowToast("云端工作区检测到文件变化可能丢失，正在重新扫描", "warning")
	}
	if !readOnly && strings.TrimSpace(root) != "" {
		event := map[string]string{
			"workspace_id":       id,
			"path":               root,
			"reconcile_required": "true",
		}
		if reasonText != "" {
			event["reconcile_reason"] = reasonText
		}
		a.emitEvent(cloudWorkspaceFilesChangedEvent, event)
		a.scheduleCloudWorkspacePush(mount)
	}
}

func (a *App) startCloudWorkspaceWatcher(mount *cloudWorkspaceHeldMount) {
	if cloudWorkspaceBackgroundDisabled || mount == nil {
		return
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		a.markCloudWorkspaceReconcileRequired(mount, err)
		return
	}
	if err := addCloudWorkspaceWatchRecursive(w, mount.LocalPath); err != nil {
		a.markCloudWorkspaceReconcileRequired(mount, err)
		_ = w.Close()
		return
	}
	cloudignore, _ := cloudworkspaceignore.ReadCloudignore(mount.LocalPath)
	matcher := cloudworkspaceignore.NewMatcher(cloudignore)
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
				if relErr != nil {
					continue
				}
				rel = filepath.ToSlash(rel)
				info, statErr := os.Stat(event.Name)
				isDir := statErr == nil && info.IsDir()
				if strings.EqualFold(filepath.Base(rel), cloudworkspaceignore.FileName) {
					if text, readErr := cloudworkspaceignore.ReadCloudignore(root); readErr == nil {
						matcher = cloudworkspaceignore.NewMatcher(text)
					}
				}
				if cloudWorkspaceWatchIgnored(rel, isDir, matcher) {
					continue
				}
				if event.Has(fsnotify.Create) && isDir {
					if watchErr := addCloudWorkspaceWatchFrom(w, root, event.Name, matcher); watchErr != nil {
						a.markCloudWorkspaceReconcileRequired(mount, watchErr)
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
					a.markCloudWorkspaceReconcileRequired(mount, err)
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

func heldCloudWorkspacePath(workspaceID string) (string, bool) {
	return heldCloudWorkspaceLocalPath(workspaceID, false)
}

func heldWritableCloudWorkspacePath(workspaceID string) (string, bool) {
	return heldCloudWorkspaceLocalPath(workspaceID, true)
}

func heldCloudWorkspaceLocalPath(workspaceID string, writableOnly bool) (string, bool) {
	mount := lookupHeldCloudWorkspace(workspaceID)
	if mount == nil {
		return "", false
	}
	mount.mu.Lock()
	defer mount.mu.Unlock()
	if writableOnly && mount.ReadOnly {
		return "", false
	}
	path := strings.TrimSpace(mount.LocalPath)
	if path == "" {
		return "", false
	}
	return path, true
}

func cloudWorkspaceIdentityFromResult(result ProjectSearchResult) string {
	if id := cloudWorkspaceIDFromPathString(result.WorkingDir); id != "" {
		return id
	}
	if id := cloudWorkspaceIDFromTags(result.Tags); id != "" {
		return id
	}
	return cloudWorkspaceIDFromPathString(result.ProjectPath)
}

func (a *App) lookupCloudWorkspaceIDForProject(projectPath string) string {
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return ""
	}
	if result, ok := lookupCloudWorkspaceTaskByPath(projectPath); ok {
		if id := cloudWorkspaceIdentityFromResult(result); id != "" {
			return id
		}
	}
	if id := lookupCloudWorkspaceIDByLocalPath(projectPath); id != "" {
		return id
	}
	if id := cloudWorkspaceIDFromPathString(projectPath); id != "" {
		return id
	}
	if wd := a.recentTaskWorkingDir(projectPath); wd != "" {
		if id := lookupCloudWorkspaceIDByLocalPath(wd); id != "" {
			return id
		}
		if id := cloudWorkspaceIDFromPathString(wd); id != "" {
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
	return primaryCloudWorkspaceID(*rec)
}

func (a *App) releaseCloudWorkspaceForProjectPath(projectPath string) {
	id := a.lookupCloudWorkspaceIDForProject(projectPath)
	if id == "" {
		return
	}
	ctx, cancel := a.cloudWorkspaceSyncContext()
	defer cancel()
	if err := a.releaseCloudWorkspace(ctx, id, false); err != nil {
		log.Printf("[cloud_workspace] release failed workspace=%s path=%q err=%v", id, projectPath, err)
	}
}

func (a *App) releaseCloudWorkspace(ctx context.Context, workspaceID string, deleteLeaseOnPushFail bool) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil
	}
	mount := takeCloudWorkspaceMount(workspaceID)
	if mount == nil {
		return nil
	}
	mount.syncMu.Lock()
	defer mount.syncMu.Unlock()
	mount.mu.Lock()
	mount.releasing = true
	mount.mu.Unlock()
	// Keep the mount discoverable while flushing so cloudWorkspaceHubDo can
	// attach the lease/session/fencing headers to every PUT. The workspace
	// sync lock serializes a second release/prepare attempt; removing the map
	// entry before Push would make an otherwise valid writer appear tokenless
	// and be rejected as FENCED.
	storeCloudWorkspaceMount(mount)
	stopCloudWorkspaceMountForRecovery(mount)
	mount.mu.Lock()
	readOnly := mount.ReadOnly
	root := mount.LocalPath
	leaseID := mount.LeaseID
	lastRevision := mount.LastCommittedRevision
	fencingToken := mount.FencingToken
	mount.mu.Unlock()
	if !readOnly && strings.TrimSpace(root) != "" {
		if man, err := a.cloudWorkspaceProtocol(workspaceID).Push(ctx, root); err != nil {
			log.Printf("[cloud_workspace] release push failed workspace=%s err=%v", workspaceID, err)
			if !deleteLeaseOnPushFail {
				storeCloudWorkspaceMount(mount)
				a.restoreCloudWorkspaceMountAfterReleaseFailure(mount)
				return err
			}
		} else {
			mount.mu.Lock()
			if man != nil {
				mount.LastCommittedRevision = man.Revision
				lastRevision = man.Revision
			}
			mount.mu.Unlock()
			if err := a.flushCloudWorkspaceSidecars(ctx, workspaceID); err != nil {
				log.Printf("[cloud_workspace] release sidecar flush failed workspace=%s err=%v", workspaceID, err)
				if !deleteLeaseOnPushFail {
					storeCloudWorkspaceMount(mount)
					a.restoreCloudWorkspaceMountAfterReleaseFailure(mount)
					return err
				}
				// Shutdown/discard mode intentionally gives up on the flush. The
				// mount is already detached, so do not leak its process lock.  Seal
				// the still-local cache before detaching when static protection is
				// enabled; the unsynced bytes remain recoverable after the lease TTL.
				if cloudWorkspaceCacheEncryptionEnabled(a) {
					if sealErr := sealCloudWorkspaceCache(a, root, mount.TenantID, workspaceID); sealErr != nil {
						log.Printf("[cloud_workspace] shutdown cache seal after sidecar failure failed workspace=%s err=%v", workspaceID, sealErr)
					}
				}
				mount.mu.Lock()
				processLockPath := mount.processLockPath
				processLockOwner := mount.processLockOwner
				mount.processLockPath = ""
				mount.processLockOwner = ""
				mount.mu.Unlock()
				if processLockPath != "" {
					releaseCloudWorkspaceProcessLockOwned(processLockPath, processLockOwner)
				}
				if current := lookupHeldCloudWorkspace(workspaceID); current == mount {
					_ = takeCloudWorkspaceMount(workspaceID)
				}
				return err
			}
		}
	}
	sealed := false
	if !readOnly && cloudWorkspaceCacheEncryptionEnabled(a) && strings.TrimSpace(root) != "" {
		if err := sealCloudWorkspaceCache(a, root, mount.TenantID, workspaceID); err != nil {
			log.Printf("[cloud_workspace] cache seal failed workspace=%s err=%v", workspaceID, err)
			storeCloudWorkspaceMount(mount)
			a.restoreCloudWorkspaceMountAfterReleaseFailure(mount)
			return fmt.Errorf("seal local cloud workspace cache: %w", err)
		}
		sealed = true
	}
	if err := a.deleteCloudWorkspaceLeaseWithRevisionAndToken(ctx, workspaceID, leaseID, lastRevision, fencingToken); err != nil {
		if sealed && !deleteLeaseOnPushFail {
			// The lease is still ours after a failed DELETE. Restore plaintext
			// before re-arming the writer; retrying against encrypted files would
			// make the task runtime fail in surprising ways.
			if unsealErr := unsealCloudWorkspaceCache(a, root, mount.TenantID, workspaceID); unsealErr != nil {
				storeCloudWorkspaceMount(mount)
				return fmt.Errorf("release cloud workspace lease: %v (cache unseal failed: %w)", err, unsealErr)
			}
		}
		if !deleteLeaseOnPushFail {
			// The lease may still be active. Keep the mount and heartbeat so a
			// transient network failure cannot orphan a writer session.
			storeCloudWorkspaceMount(mount)
			a.restoreCloudWorkspaceMountAfterReleaseFailure(mount)
			return err
		}
		log.Printf("[cloud_workspace] release delete lease failed workspace=%s err=%v", workspaceID, err)
	}
	// The lease is either durably released or this shutdown path is explicitly
	// discarding the failure. In both cases no further writer may use this local
	// cache, so release the process lock after the remote decision—not before.
	mount.mu.Lock()
	processLockPath := mount.processLockPath
	processLockOwner := mount.processLockOwner
	mount.processLockPath = ""
	mount.processLockOwner = ""
	mount.mu.Unlock()
	if processLockPath != "" {
		releaseCloudWorkspaceProcessLockOwned(processLockPath, processLockOwner)
	}
	if current := lookupHeldCloudWorkspace(workspaceID); current == mount {
		_ = takeCloudWorkspaceMount(workspaceID)
	}
	mount.mu.Lock()
	mount.releasing = false
	mount.mu.Unlock()
	return nil
}

// ReleaseCloudWorkspace pushes the cache (unless stolen/read-only), writes state.json, then DELETE /leases/{id}.
func (a *App) ReleaseCloudWorkspace(workspaceID string) error {
	ctx, cancel := a.cloudWorkspaceSyncContext()
	defer cancel()
	return a.releaseCloudWorkspace(ctx, workspaceID, false)
}

func (a *App) releaseAllCloudWorkspaces() {
	ctx, cancel := context.WithTimeout(context.Background(), cloudWorkspaceShutdownReleaseTimeout)
	defer cancel()
	for _, id := range listHeldCloudWorkspaceIDs() {
		_ = a.releaseCloudWorkspace(ctx, id, true)
	}
	_ = a.revokeCloudWorkspaceInstanceSession(ctx)
}

func validCloudWorkspaceCacheID(workspaceID string) bool {
	if workspaceID == "" || len(workspaceID) > 128 || workspaceID == "." || workspaceID == ".." || workspaceID != filepath.Base(workspaceID) {
		return false
	}
	if strings.TrimSpace(workspaceID) != workspaceID || strings.HasSuffix(workspaceID, ".") || strings.ContainsAny(workspaceID, `/\:`) {
		return false
	}
	for _, r := range workspaceID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	base := workspaceID
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return false
	default:
		return true
	}
}

func cloudWorkspaceDirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// CloudWorkspaceCacheDir returns the local cache directory if this machine
// already has one. It does not contact Hub or pull files.
func (a *App) CloudWorkspaceCacheDir(workspaceID string) (PreparedCloudWorkspace, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if !validCloudWorkspaceCacheID(workspaceID) {
		return PreparedCloudWorkspace{}, fmt.Errorf("workspace id is required")
	}
	out := PreparedCloudWorkspace{WorkspaceID: workspaceID}
	if path, ok := heldCloudWorkspacePath(workspaceID); ok && cloudWorkspaceDirExists(path) {
		out.LocalPath = path
		return out, nil
	}
	if a == nil {
		return out, nil
	}
	if localPath := a.lookupOnDiskCloudWorkspaceCache(workspaceID); localPath != "" {
		out.LocalPath = localPath
	}
	return out, nil
}

func (a *App) lookupOnDiskCloudWorkspaceCache(workspaceID string) string {
	root := normalizeProjectSessionPath(filepath.Join(a.GetDataDir(), "cloud-workspaces"))
	preferred := normalizeProjectSessionPath(a.cloudWorkspaceCachePath(a.cloudWorkspaceTenantID(), workspaceID))
	if cloudWorkspaceDirExists(preferred) && cloudWorkspacePathResolvesInsideRoot(root, preferred) {
		return preferred
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, ent := range entries {
		if !ent.IsDir() || ent.Type()&os.ModeSymlink != 0 {
			continue
		}
		candidate := normalizeProjectSessionPath(filepath.Join(root, ent.Name(), workspaceID))
		if candidate == preferred {
			continue
		}
		if cloudWorkspaceDirExists(candidate) && cloudWorkspacePathResolvesInsideRoot(root, candidate) {
			return candidate
		}
	}
	return ""
}

// SyncCloudWorkspaceFiles mounts the cache if needed and pulls the remote
// manifest. Event replay is intentionally disabled for v1-sequential.
func (a *App) SyncCloudWorkspaceFiles(workspaceID string) (PreparedCloudWorkspace, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if !validCloudWorkspaceCacheID(workspaceID) {
		return PreparedCloudWorkspace{}, fmt.Errorf("workspace id is required")
	}
	prepared := PreparedCloudWorkspace{WorkspaceID: workspaceID}
	if mount := lookupHeldCloudWorkspace(workspaceID); mount != nil {
		mount.mu.Lock()
		prepared.LocalPath = mount.LocalPath
		readOnly := mount.ReadOnly
		mount.mu.Unlock()
		// A writer already has the current lease and is the source of truth for
		// this process. A manual browse sync may still hydrate an empty cache,
		// but must never overwrite dirty writer files.
		if !readOnly {
			empty, emptyErr := cloudWorkspaceUserTreeEmpty(prepared.LocalPath)
			if emptyErr != nil || !empty {
				a.emitEvent(cloudWorkspaceFilesChangedEvent, map[string]string{
					"workspace_id": workspaceID,
					"path":         prepared.LocalPath,
				})
				return prepared, emptyErr
			}
		}
	} else {
		got, err := a.PrepareCloudWorkspaceReadOnly(workspaceID)
		if err != nil {
			return got, err
		}
		prepared = got
	}
	if strings.TrimSpace(prepared.LocalPath) == "" {
		return prepared, fmt.Errorf("cloud workspace cache path is empty")
	}
	if syncErr := a.syncHeldCloudWorkspaceFiles(workspaceID, prepared.LocalPath); syncErr != nil {
		log.Printf("[cloud_workspace] browse sync failed id=%s err=%v", workspaceID, syncErr)
		return prepared, syncErr
	}
	a.emitEvent(cloudWorkspaceFilesChangedEvent, map[string]string{
		"workspace_id": workspaceID,
		"path":         prepared.LocalPath,
	})
	return prepared, nil
}

// PrepareCloudWorkspaceReadOnly hydrates a local cache without acquiring the
// writer lease. It is the supported cross-device collaboration path: readers
// may coexist, while only PrepareCloudWorkspace can enable local writes.
func (a *App) PrepareCloudWorkspaceReadOnly(workspaceID string) (PreparedCloudWorkspace, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if !validCloudWorkspaceCacheID(workspaceID) {
		return PreparedCloudWorkspace{}, fmt.Errorf("workspace id is required")
	}
	if path, ok := heldCloudWorkspacePath(workspaceID); ok {
		return PreparedCloudWorkspace{LocalPath: path, WorkspaceID: workspaceID}, nil
	}
	unlock := lockCloudWorkspacePrepare(workspaceID)
	defer unlock()
	if path, ok := heldCloudWorkspacePath(workspaceID); ok {
		return PreparedCloudWorkspace{LocalPath: path, WorkspaceID: workspaceID}, nil
	}
	tenantID := a.cloudWorkspaceTenantID()
	localPath := normalizeProjectSessionPath(a.cloudWorkspaceReadOnlyCachePath(tenantID, workspaceID))
	if err := ensureCloudWorkspaceCacheDirectory(filepath.Join(a.GetDataDir(), "cloud-workspaces-readonly"), localPath); err != nil {
		return PreparedCloudWorkspace{}, err
	}
	ctx, cancel := a.cloudWorkspaceSyncContext()
	defer cancel()
	proto := a.cloudWorkspaceProtocol(workspaceID)
	pulled, err := proto.Pull(ctx, localPath)
	if err != nil {
		a.recordCloudWorkspaceAudit(cloudWorkspaceAuditEvent{WorkspaceID: workspaceID, Operation: "download", Outcome: "failed", Detail: err.Error()})
		return PreparedCloudWorkspace{}, err
	}
	if pulled != nil {
		a.recordCloudWorkspaceAudit(cloudWorkspaceAuditEvent{WorkspaceID: workspaceID, Operation: "download", Revision: pulled.Revision, Files: len(pulled.Entries), Bytes: totalEntryBytes(pulled.Entries)})
	}
	a.fetchCloudWorkspaceSidecars(ctx, workspaceID)
	if pulled == nil {
		pulled = &cloudWorkspaceManifest{}
	}
	mount := &cloudWorkspaceHeldMount{
		WorkspaceID:      workspaceID,
		LocalPath:        localPath,
		TenantID:         tenantID,
		ReadOnly:         true,
		isolatedReadOnly: true,
	}
	storeCloudWorkspaceMount(mount)
	return PreparedCloudWorkspace{LocalPath: localPath, WorkspaceID: workspaceID}, nil
}

func (a *App) syncHeldCloudWorkspaceFiles(workspaceID, localPath string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	localPath = normalizeProjectSessionPath(localPath)
	if a == nil || !validCloudWorkspaceCacheID(workspaceID) || localPath == "" {
		return fmt.Errorf("workspace id is required")
	}
	mount := lookupHeldCloudWorkspace(workspaceID)
	if mount != nil {
		mount.syncMu.Lock()
		defer mount.syncMu.Unlock()
		mount.mu.Lock()
		releasing := mount.releasing
		mount.mu.Unlock()
		if releasing {
			return fmt.Errorf("cloud workspace is releasing")
		}
	}
	ctx, cancel := a.cloudWorkspaceSyncContext()
	defer cancel()
	proto := a.cloudWorkspaceProtocol(workspaceID)
	manifest, pullErr := proto.Pull(ctx, localPath)
	if pullErr != nil {
		a.recordCloudWorkspaceAudit(cloudWorkspaceAuditEvent{WorkspaceID: workspaceID, Operation: "download", Outcome: "failed", Detail: pullErr.Error()})
	}
	if pullErr == nil && manifest != nil {
		a.recordCloudWorkspaceAudit(cloudWorkspaceAuditEvent{WorkspaceID: workspaceID, Operation: "download", Revision: manifest.Revision, Files: len(manifest.Entries), Bytes: totalEntryBytes(manifest.Entries)})
	}
	return pullErr
}

func cloudWorkspaceUserTreeEmpty(root string) (bool, error) {
	cloudignore, err := cloudworkspaceignore.ReadCloudignore(root)
	if err != nil {
		return false, err
	}
	matcher := cloudworkspaceignore.NewMatcher(cloudignore)
	empty := true
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) || os.IsPermission(walkErr) {
				return nil
			}
			return walkErr
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if matcher.ShouldIgnore(rel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			if os.IsNotExist(infoErr) || os.IsPermission(infoErr) {
				return nil
			}
			return infoErr
		}
		if info.Mode().IsRegular() {
			empty = false
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return empty, nil
}

// PrepareCloudWorkspace acquires the exclusive lease, mounts the local cache, and syncs files.
func (a *App) PrepareCloudWorkspace(workspaceID string) (PreparedCloudWorkspace, error) {
	return a.prepareCloudWorkspace(workspaceID, false)
}

func (a *App) prepareCloudWorkspace(workspaceID string, force bool) (PreparedCloudWorkspace, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if !validCloudWorkspaceCacheID(workspaceID) {
		return PreparedCloudWorkspace{}, fmt.Errorf("workspace id is required")
	}
	if !force {
		if path, ok := heldWritableCloudWorkspacePath(workspaceID); ok {
			return PreparedCloudWorkspace{LocalPath: path, WorkspaceID: workspaceID}, nil
		}
		// A network partition or fencing response leaves the existing cache
		// mounted read-only and deliberately retains its process lock. A later
		// explicit Prepare is the recovery boundary: detach that stale mount,
		// release its local lock, and acquire a fresh Hub fencing epoch while
		// preserving the user files for the normal dirty/base revision check.
		if old := lookupHeldCloudWorkspace(workspaceID); old != nil {
			old.mu.Lock()
			readOnly := old.ReadOnly
			old.mu.Unlock()
			if readOnly {
				if current := takeCloudWorkspaceMount(workspaceID); current != nil {
					stopCloudWorkspaceMount(current)
				}
			}
		}
	}
	if force {
		if old := takeCloudWorkspaceMount(workspaceID); old != nil {
			stopCloudWorkspaceMount(old)
		}
	}

	tenantID := a.cloudWorkspaceTenantID()
	localPath := normalizeProjectSessionPath(a.cloudWorkspaceCachePath(tenantID, workspaceID))
	if err := ensureCloudWorkspaceCacheDirectory(filepath.Join(a.GetDataDir(), "cloud-workspaces"), localPath); err != nil {
		return PreparedCloudWorkspace{}, err
	}
	processLockPath, processLockOwner, err := acquireCloudWorkspaceProcessLockOwned(localPath)
	if err != nil {
		return PreparedCloudWorkspace{}, err
	}
	lockHeld := true
	defer func() {
		if lockHeld {
			releaseCloudWorkspaceProcessLockOwned(processLockPath, processLockOwner)
		}
	}()
	// A previously released writer cache may be sealed at rest. Preflight the
	// key without decrypting before lease acquire: a lease conflict must not
	// leave a cache plaintext merely because this process tried to inspect it.
	cacheWasSealed := cloudWorkspaceCacheSealMarkerExists(localPath)
	if cacheWasSealed {
		if _, _, keyErr := cloudWorkspaceCacheKey(tenantID, workspaceID, false); keyErr != nil {
			return PreparedCloudWorkspace{}, keyErr
		}
	}

	// Serialize only this workspace; unrelated workspaces may prepare in parallel.
	prepareUnlock := lockCloudWorkspacePrepare(workspaceID)
	defer prepareUnlock()
	leaseCtx, leaseCancel := a.cloudWorkspaceRequestContext()
	defer leaseCancel()
	outcome, err := a.acquireCloudWorkspaceLease(leaseCtx, workspaceID, force)
	if err != nil {
		var inUse *cloudWorkspaceInUseError
		if asCloudWorkspaceInUse(err, &inUse) && !force {
			// A live lease is a read-only/waiting state. Fencing the current
			// writer is allowed only after an explicit user confirmation.
			if !a.confirmCloudWorkspaceSteal(inUse.HolderMachineName) {
				return PreparedCloudWorkspace{}, err
			}
			outcome, err = a.acquireCloudWorkspaceLease(leaseCtx, workspaceID, true)
		}
		if err != nil {
			return PreparedCloudWorkspace{}, err
		}
	}
	if outcome == nil {
		return PreparedCloudWorkspace{}, fmt.Errorf("cloud workspace lease acquire failed")
	}

	mount := &cloudWorkspaceHeldMount{
		WorkspaceID:           workspaceID,
		LeaseID:               outcome.LeaseID,
		FencingToken:          outcome.FencingToken,
		LeaseExpiresAt:        outcome.ExpiresAt,
		LastCommittedRevision: "",
		LocalPath:             localPath,
		TenantID:              tenantID,
		processLockPath:       processLockPath,
		processLockOwner:      processLockOwner,
	}
	lockHeld = false
	// Publish the lease identity before the initial upload/pull so every write
	// request carries the fencing token. The mount is removed again by
	// failPrepare if reconciliation cannot complete.
	storeCloudWorkspaceMount(mount)
	// Heartbeat must run during dialogs and Pull/Push: a max-size sync can exceed LeaseTTL.
	a.startCloudWorkspaceHeartbeat(mount)
	acquired := strings.TrimSpace(outcome.Acquired)
	failPrepare := func(err error) (PreparedCloudWorkspace, error) {
		// Any failure after acquiring an epoch means the local tree has not been
		// proven to match the remote baseline (including a cancelled Pull or a
		// file-changing-during-download guard).  Persist the marker before
		// detaching so the next explicit handoff performs a full reconcile.
		if strings.TrimSpace(localPath) != "" && !mount.isolatedReadOnly && !(cacheWasSealed && cloudWorkspaceCacheSealMarkerExists(localPath)) {
			mount.mu.Lock()
			mount.reconcileRequired = true
			mount.mu.Unlock()
			if _, persistErr := markCloudWorkspaceLocalReconcileRequired(localPath, err.Error()); persistErr != nil {
				log.Printf("[cloud_workspace] persist prepare reconcile marker failed workspace=%s err=%v", workspaceID, persistErr)
			}
		}
		if current := lookupHeldCloudWorkspace(workspaceID); current == mount {
			_ = takeCloudWorkspaceMount(workspaceID)
		}
		stopCloudWorkspaceMount(mount)
		if acquired != cloudWorkspaceAcquiredRenewed {
			ctx, cancel := a.cloudWorkspaceRequestContext()
			defer cancel()
			_ = a.deleteCloudWorkspaceLease(ctx, workspaceID, outcome.LeaseID)
		}
		return PreparedCloudWorkspace{}, err
	}
	if err := unsealCloudWorkspaceCache(a, localPath, tenantID, workspaceID); err != nil {
		return failPrepare(err)
	}
	// Restore the durable watcher marker before any initial sync.  The normal
	// cache plan still decides pull vs push from the remote revision and local
	// baseline; this bit only guarantees that a complete scan is performed and
	// is not forgotten after a process restart.
	if localState, stateErr := readCloudWorkspaceLocalState(localPath); stateErr == nil {
		mount.mu.Lock()
		mount.reconcileRequired = localState.ReconcileRequired
		mount.mu.Unlock()
	} else {
		return failPrepare(stateErr)
	}

	proto := a.cloudWorkspaceProtocol(workspaceID)
	syncKind := cloudWorkspaceSyncPull
	if acquired == cloudWorkspaceAcquiredRenewed {
		syncKind = cloudWorkspaceSyncPush
	}
	if acquired != cloudWorkspaceAcquiredRenewed {
		peekCtx, peekCancel := a.cloudWorkspaceSyncContext()
		remote, remoteErr := proto.Transport.GetManifest(peekCtx)
		if remoteErr != nil {
			peekCancel()
			return failPrepare(remoteErr)
		}
		kind, planErr := cloudWorkspaceCacheSyncPlanContext(peekCtx, localPath, remote, force)
		peekCancel()
		if planErr != nil {
			return failPrepare(planErr)
		}
		if kind == cloudWorkspaceSyncConflict {
			if force {
				if !a.confirmCloudWorkspaceDiscardDirty() {
					return failPrepare(fmt.Errorf("已取消打开云端工作区"))
				}
				kind = cloudWorkspaceSyncPull
			} else if !a.confirmCloudWorkspaceKeepLocal() {
				return failPrepare(fmt.Errorf("已取消打开云端工作区"))
			} else {
				kind = cloudWorkspaceSyncPush
			}
		}
		if kind == cloudWorkspaceSyncNone {
			kind = cloudWorkspaceSyncPull
		}
		syncKind = kind
		log.Printf("[cloud_workspace] prepare sync workspace=%s acquired=%s kind=%s", workspaceID, acquired, syncKind)
	}

	syncCtx, syncCancel := a.cloudWorkspaceSyncContext()
	defer syncCancel()
	if syncKind == cloudWorkspaceSyncPush {
		if man, err := proto.Push(syncCtx, localPath); err != nil {
			return failPrepare(err)
		} else if man != nil {
			mount.mu.Lock()
			mount.LastCommittedRevision = man.Revision
			mount.mu.Unlock()
		}
		if err := a.flushCloudWorkspaceSidecars(syncCtx, workspaceID); err != nil {
			return failPrepare(err)
		}
	} else {
		pulled, err := proto.Pull(syncCtx, localPath)
		if err != nil {
			a.recordCloudWorkspaceAudit(cloudWorkspaceAuditEvent{WorkspaceID: workspaceID, Operation: "download", Outcome: "failed", Detail: err.Error()})
			return failPrepare(err)
		}
		rev := ""
		if pulled != nil {
			rev = pulled.Revision
			mount.mu.Lock()
			mount.LastCommittedRevision = rev
			mount.mu.Unlock()
		}
		if pulled != nil {
			a.recordCloudWorkspaceAudit(cloudWorkspaceAuditEvent{WorkspaceID: workspaceID, Operation: "download", Revision: pulled.Revision, Files: len(pulled.Entries), Bytes: totalEntryBytes(pulled.Entries)})
		}
		if err := writeCloudWorkspaceLocalState(localPath, rev); err != nil {
			return failPrepare(err)
		}
		a.fetchCloudWorkspaceSidecars(syncCtx, workspaceID)
	}

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
