package guiapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// cloudWorkspaceTaskSyncInterval is how often a signed-in machine refreshes
// the account's cloud task list and run status. The delay is intentional:
// one entitlement read covers every workspace, and a status write happens
// only when the writer lease is held and the snapshot actually changed.
const cloudWorkspaceTaskSyncInterval = 45 * time.Second

const cloudWorkspaceRunFieldMax = 64

type cloudWorkspaceRunSnapshot struct {
	Status        string
	Phase         string
	PendingReview bool
}

func (s cloudWorkspaceRunSnapshot) empty() bool {
	return strings.TrimSpace(s.Status) == "" && strings.TrimSpace(s.Phase) == "" && !s.PendingReview
}

func (s cloudWorkspaceRunSnapshot) signature() string {
	pending := "0"
	if s.PendingReview {
		pending = "1"
	}
	return strings.TrimSpace(s.Status) + "\x00" + strings.TrimSpace(s.Phase) + "\x00" + pending
}

type remoteCloudWorkspaceRunStatus struct {
	cloudWorkspaceRunSnapshot
	LeaseInUse bool
}

var (
	cloudWorkspaceRunMu          sync.Mutex
	cloudWorkspaceLocalRun       = map[string]cloudWorkspaceRunSnapshot{}
	cloudWorkspacePublishedRun   = map[string]string{}
	cloudWorkspaceRemoteRun      = map[string]remoteCloudWorkspaceRunStatus{}
	cloudWorkspaceSentSidecar    = map[string]map[string][]byte{}
	cloudWorkspacePublishGen     = map[string]uint64{}
	cloudWorkspaceTaskSyncOnce   sync.Once
	cloudWorkspaceTaskSyncActive atomic.Bool
)

func resetCloudWorkspaceRunPublishState() {
	cloudWorkspaceRunMu.Lock()
	cloudWorkspaceLocalRun = map[string]cloudWorkspaceRunSnapshot{}
	cloudWorkspacePublishedRun = map[string]string{}
	cloudWorkspaceRemoteRun = map[string]remoteCloudWorkspaceRunStatus{}
	cloudWorkspaceSentSidecar = map[string]map[string][]byte{}
	cloudWorkspacePublishGen = map[string]uint64{}
	cloudWorkspaceRunMu.Unlock()
}

// forgetCloudWorkspaceRunPublish drops the writer's memory of what it last
// sent. That memory is valid only while this process holds the lease. After
// the lease moves, another machine can change a sidecar, and keeping the old
// bytes would make the next hold skip a real update. The generation stops a
// PUT that was already in flight from writing the cache back.
func forgetCloudWorkspaceRunPublish(workspaceID string) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return
	}
	cloudWorkspaceRunMu.Lock()
	delete(cloudWorkspacePublishedRun, workspaceID)
	delete(cloudWorkspaceSentSidecar, workspaceID)
	cloudWorkspacePublishGen[workspaceID]++
	cloudWorkspaceRunMu.Unlock()
}

func cloudWorkspacePublishGeneration(workspaceID string) uint64 {
	workspaceID = strings.TrimSpace(workspaceID)
	cloudWorkspaceRunMu.Lock()
	defer cloudWorkspaceRunMu.Unlock()
	return cloudWorkspacePublishGen[workspaceID]
}

func cloudWorkspaceSentSidecarMatches(workspaceID, name string, data []byte) bool {
	workspaceID = strings.TrimSpace(workspaceID)
	name = strings.TrimSpace(name)
	cloudWorkspaceRunMu.Lock()
	sent := cloudWorkspaceSentSidecar[workspaceID][name]
	cloudWorkspaceRunMu.Unlock()
	return len(sent) > 0 && bytes.Equal(sent, data)
}

func rememberCloudWorkspaceSentSidecar(workspaceID, name string, data []byte, gen uint64) {
	workspaceID = strings.TrimSpace(workspaceID)
	name = strings.TrimSpace(name)
	if workspaceID == "" || name == "" || len(data) == 0 {
		return
	}
	cloudWorkspaceRunMu.Lock()
	defer cloudWorkspaceRunMu.Unlock()
	if cloudWorkspacePublishGen[workspaceID] != gen {
		return
	}
	sent := cloudWorkspaceSentSidecar[workspaceID]
	if sent == nil {
		sent = map[string][]byte{}
		cloudWorkspaceSentSidecar[workspaceID] = sent
	}
	sent[name] = append([]byte(nil), data...)
}

func cloudWorkspaceSentTaskSidecar(workspaceID string) (cloudWorkspaceTaskSidecar, bool) {
	workspaceID = strings.TrimSpace(workspaceID)
	cloudWorkspaceRunMu.Lock()
	sent := cloudWorkspaceSentSidecar[workspaceID][cloudWorkspaceSidecarTask]
	cloudWorkspaceRunMu.Unlock()
	if len(sent) == 0 {
		return cloudWorkspaceTaskSidecar{}, false
	}
	var task cloudWorkspaceTaskSidecar
	if err := json.Unmarshal(sent, &task); err != nil {
		return cloudWorkspaceTaskSidecar{}, false
	}
	return task, true
}

func clipCloudWorkspaceRunField(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= cloudWorkspaceRunFieldMax {
		return value
	}
	cut := value[:cloudWorkspaceRunFieldMax]
	for len(cut) > 0 && (cut[len(cut)-1]&0xC0) == 0x80 {
		cut = cut[:len(cut)-1]
	}
	if len(cut) > 0 && cut[len(cut)-1] >= 0xC0 {
		cut = cut[:len(cut)-1]
	}
	return cut
}

func snapshotFromWorkflow(state *ProjectWorkflowState) cloudWorkspaceRunSnapshot {
	if state == nil {
		return cloudWorkspaceRunSnapshot{}
	}
	return cloudWorkspaceRunSnapshot{
		Status:        clipCloudWorkspaceRunField(state.Status),
		Phase:         clipCloudWorkspaceRunField(state.Phase),
		PendingReview: state.PendingReview,
	}
}

func rememberCloudWorkspaceLocalRun(workspaceID string, state *ProjectWorkflowState) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return
	}
	cloudWorkspaceRunMu.Lock()
	cloudWorkspaceLocalRun[workspaceID] = snapshotFromWorkflow(state)
	cloudWorkspaceRunMu.Unlock()
}

func lookupCloudWorkspaceLocalRun(workspaceID string) (cloudWorkspaceRunSnapshot, bool) {
	workspaceID = strings.TrimSpace(workspaceID)
	cloudWorkspaceRunMu.Lock()
	defer cloudWorkspaceRunMu.Unlock()
	snap, ok := cloudWorkspaceLocalRun[workspaceID]
	return snap, ok
}

func rememberRemoteCloudWorkspaceRunStatuses(ent CloudWorkspaceEntitlement) {
	next := map[string]remoteCloudWorkspaceRunStatus{}
	record := func(ws CloudWorkspaceEntitlementWorkspace) {
		id := strings.TrimSpace(ws.ID)
		if id == "" {
			return
		}
		snap := cloudWorkspaceRunSnapshot{
			Status:        clipCloudWorkspaceRunField(ws.TaskRunStatus),
			Phase:         clipCloudWorkspaceRunField(ws.TaskRunPhase),
			PendingReview: ws.TaskRunPendingReview,
		}
		if snap.empty() {
			return
		}
		next[id] = remoteCloudWorkspaceRunStatus{cloudWorkspaceRunSnapshot: snap, LeaseInUse: ws.LeaseInUse}
	}
	for _, ws := range ent.Workspaces {
		record(ws)
	}
	for _, shared := range ent.Shared {
		record(shared.CloudWorkspaceEntitlementWorkspace)
	}
	cloudWorkspaceRunMu.Lock()
	cloudWorkspaceRemoteRun = next
	cloudWorkspaceRunMu.Unlock()
}

func remoteCloudWorkspaceRunSignature() string {
	cloudWorkspaceRunMu.Lock()
	defer cloudWorkspaceRunMu.Unlock()
	if len(cloudWorkspaceRemoteRun) == 0 {
		return ""
	}
	ids := make([]string, 0, len(cloudWorkspaceRemoteRun))
	for id := range cloudWorkspaceRemoteRun {
		ids = append(ids, id)
	}
	// Sort so two polls of the same projection compare equal.
	sort.Strings(ids)
	var b strings.Builder
	for _, id := range ids {
		snap := cloudWorkspaceRemoteRun[id]
		lease := "0"
		if snap.LeaseInUse {
			lease = "1"
		}
		b.WriteString(id)
		b.WriteByte('\n')
		b.WriteString(snap.signature())
		b.WriteByte('\n')
		b.WriteString(lease)
		b.WriteByte('\n')
	}
	return b.String()
}

func cloudWorkspaceIDFromSearchResult(result *ProjectSearchResult) string {
	if result == nil {
		return ""
	}
	if id := cloudWorkspaceIDFromPathString(result.WorkingDir); id != "" {
		return id
	}
	if id := cloudWorkspaceIDFromReadOnlyCachePath(result.WorkingDir); id != "" {
		return id
	}
	return cloudWorkspaceIDFromTags(result.Tags)
}

func workflowSessionIsExecuting(state *ProjectWorkflowState) bool {
	if state == nil || strings.TrimSpace(state.ProjectPath) == "" {
		return false
	}
	return taskSessionIsExecuting(state.ProjectPath, taskListLiveOwners())
}

func cloudWorkspaceRunStatusIsLive(snap cloudWorkspaceRunSnapshot) bool {
	state := &ProjectWorkflowState{Status: snap.Status, Phase: snap.Phase, PendingReview: snap.PendingReview}
	return projectWorkflowSnapshotActive(state) || taskListSnapshotAwaitsUser(state)
}

// overlayRemoteCloudWorkspaceRunStatus shows another machine's status when
// this machine is not already driving the row. A live remote status is kept
// only while that machine still holds the lease, so a crashed writer does
// not leave the row running until the next successful publish.
func cloudWorkspaceWriterHeld(workspaceID string) bool {
	mount := lookupHeldCloudWorkspace(workspaceID)
	if mount == nil {
		return false
	}
	mount.mu.Lock()
	defer mount.mu.Unlock()
	// releasing still owns the lease until the DELETE finishes. The row keeps
	// this machine's status through that window.
	return !mount.ReadOnly && !mount.isolatedReadOnly && !mount.stopped && strings.TrimSpace(mount.LocalPath) != ""
}

func overlayRemoteCloudWorkspaceRunStatus(workspaceID string, local *ProjectWorkflowState) *ProjectWorkflowState {
	workspaceID = strings.TrimSpace(workspaceID)
	// The writer, and a session that is actually executing here, already have
	// the snapshot that matters. A leftover "running" row on a machine that
	// no longer holds the lease must not hide the account status.
	if cloudWorkspaceWriterHeld(workspaceID) || workflowSessionIsExecuting(local) {
		return local
	}
	cloudWorkspaceRunMu.Lock()
	remote, ok := cloudWorkspaceRemoteRun[workspaceID]
	cloudWorkspaceRunMu.Unlock()
	if !ok || remote.empty() {
		return local
	}
	live := cloudWorkspaceRunStatusIsLive(remote.cloudWorkspaceRunSnapshot)
	if live && !remote.LeaseInUse {
		return local
	}
	out := &ProjectWorkflowState{
		Status:        remote.Status,
		Phase:         remote.Phase,
		PendingReview: remote.PendingReview,
	}
	if local != nil {
		out.ID = local.ID
		out.Type = local.Type
		out.ProjectPath = local.ProjectPath
	}
	return out
}

func writableCloudWorkspaceIDs() []string {
	cloudWorkspaceMountMu.Lock()
	mounts := make([]*cloudWorkspaceHeldMount, 0, len(cloudWorkspaceMounts))
	for _, mount := range cloudWorkspaceMounts {
		if mount != nil {
			mounts = append(mounts, mount)
		}
	}
	cloudWorkspaceMountMu.Unlock()
	ids := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		mount.mu.Lock()
		readOnly := mount.ReadOnly || mount.isolatedReadOnly || mount.releasing || mount.stopped || strings.TrimSpace(mount.LocalPath) == ""
		id := mount.WorkspaceID
		mount.mu.Unlock()
		if !readOnly {
			ids = append(ids, id)
		}
	}
	return ids
}

func (a *App) publishHeldCloudWorkspaceRunStatuses() bool {
	if a == nil {
		return false
	}
	ids := writableCloudWorkspaceIDs()
	if len(ids) == 0 {
		return false
	}
	// One workspace, not the whole sidebar. The AI tab may be hidden, so the
	// poll cannot wait for ListTasks to have noticed the latest run.
	a.rememberWriterCloudWorkspaceRuns(ids)
	return a.flushChangedCloudWorkspaceRunStatuses(ids)
}

// rememberWriterCloudWorkspaceRuns stores the same status the sidebar would
// show for each open writer workspace. It does not scan the task list.
func (a *App) rememberWriterCloudWorkspaceRuns(ids []string) {
	if a == nil || len(ids) == 0 {
		return
	}
	liveOwners := taskListLiveOwners()
	for _, id := range ids {
		result := a.findCanonicalCloudWorkspaceTask(id, false)
		path := normalizeProjectSessionPath(result.ProjectPath)
		if path == "" {
			continue
		}
		result.Tags = scrubCloudWorkspaceIdentityTags(result.Tags)
		a.applyTaskListWorkflow(&result, path, nil, nil, nil, nil, liveOwners)
	}
}

// publishCloudWorkspaceRunForRelease writes the writer's latest status before
// the lease is dropped. A file push must not do this: it would recompute the
// workflow on every sync, and an idle snapshot would clear a live run.
func (a *App) publishCloudWorkspaceRunForRelease(ctx context.Context, workspaceID string) error {
	if a == nil {
		return nil
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil
	}
	a.rememberWriterCloudWorkspaceRuns([]string{workspaceID})
	snap, ok := lookupCloudWorkspaceLocalRun(workspaceID)
	if !ok || !cloudWorkspaceRunReplacesStored(workspaceID, snap) {
		return nil
	}
	if err := a.flushCloudWorkspaceTaskRun(ctx, workspaceID, &snap); err != nil {
		return err
	}
	cloudWorkspaceRunMu.Lock()
	cloudWorkspacePublishedRun[workspaceID] = snap.signature()
	cloudWorkspaceRunMu.Unlock()
	return nil
}

// cloudWorkspaceRunReplacesStored reports whether snap should be written over
// the status already in task.json. A non-empty snapshot is the writer's
// current run. An empty one is written only to clear a live status this
// machine now owns.
func cloudWorkspaceRunReplacesStored(id string, snap cloudWorkspaceRunSnapshot) bool {
	if !snap.empty() {
		return true
	}
	cloudWorkspaceRunMu.Lock()
	defer cloudWorkspaceRunMu.Unlock()
	return shouldPublishCloudWorkspaceRunLocked(id, snap)
}

func (a *App) flushChangedCloudWorkspaceRunStatuses(ids []string) bool {
	type pendingPublish struct {
		id  string
		sig string
	}
	pending := make([]pendingPublish, 0, len(ids))
	snapshots := make(map[string]cloudWorkspaceRunSnapshot, len(ids))
	cloudWorkspaceRunMu.Lock()
	for _, id := range ids {
		id = strings.TrimSpace(id)
		snap, ok := cloudWorkspaceLocalRun[id]
		if !ok || !shouldPublishCloudWorkspaceRunLocked(id, snap) {
			continue
		}
		snapshots[id] = snap
		pending = append(pending, pendingPublish{id: id, sig: snap.signature()})
	}
	cloudWorkspaceRunMu.Unlock()
	if a == nil {
		return false
	}
	wrote := false
	for _, item := range pending {
		snap := snapshots[item.id]
		// File push and lease release hold this lock for the whole upload.
		// Waiting here would stall every other workspace's task sync behind
		// one large push. Skip this round; the signature stays unpublished
		// so the next quiet poll sends it. The push's own sidecar flush
		// still writes the status remembered above when it finishes.
		mount := lookupHeldCloudWorkspace(item.id)
		if mount == nil {
			continue
		}
		if !mount.syncMu.TryLock() {
			continue
		}
		if !cloudWorkspaceWriterHeld(item.id) {
			mount.syncMu.Unlock()
			continue
		}
		ctx, cancel := a.cloudWorkspaceRequestContext()
		err := a.flushCloudWorkspaceTaskRun(ctx, item.id, &snap)
		cancel()
		if err != nil {
			mount.syncMu.Unlock()
			if !errors.Is(err, errCloudWorkspaceTaskRunSkipped) {
				log.Printf("[cloud_workspace] task status publish failed workspace=%s err=%v", item.id, err)
			}
			continue
		}
		cloudWorkspaceRunMu.Lock()
		cloudWorkspacePublishedRun[item.id] = item.sig
		cloudWorkspaceRunMu.Unlock()
		mount.syncMu.Unlock()
		wrote = true
	}
	return wrote
}

// startCloudWorkspaceTaskSync polls the account's cloud tasks after startup.
// Tests call syncCloudWorkspaceTasksFromHub directly so a test process does
// not grow a background poller.
func (a *App) startCloudWorkspaceTaskSync(ctx interface{ Done() <-chan struct{} }) {
	if a == nil || ctx == nil || flag.Lookup("test.v") != nil {
		return
	}
	cloudWorkspaceTaskSyncOnce.Do(func() {
		go func() {
			timer := time.NewTimer(cloudWorkspaceTaskSyncInterval)
			defer timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
					a.syncCloudWorkspaceTasksFromHub()
					timer.Reset(cloudWorkspaceTaskSyncInterval)
				}
			}
		}()
	})
}

// shouldPublishCloudWorkspaceRunLocked reports whether this writer should send
// snap. An empty local snapshot must not erase a finished status just because
// the task was opened; it does replace a live status whose lease this machine
// now holds, so a restarted writer does not leave "running" behind.
func shouldPublishCloudWorkspaceRunLocked(id string, snap cloudWorkspaceRunSnapshot) bool {
	if cloudWorkspacePublishedRun[id] == snap.signature() {
		return false
	}
	if !snap.empty() || cloudWorkspacePublishedRun[id] != "" {
		return true
	}
	remote, ok := cloudWorkspaceRemoteRun[id]
	return ok && cloudWorkspaceRunStatusIsLive(remote.cloudWorkspaceRunSnapshot)
}

// syncCloudWorkspaceTasksFromHub pulls the account task list and run status.
// A machine that already has the rows pays for one entitlement request.
// Session sidecars are fetched only when a workspace is new on this machine.
func (a *App) syncCloudWorkspaceTasksFromHub() {
	if a == nil || !cloudWorkspaceTaskSyncActive.CompareAndSwap(false, true) {
		return
	}
	defer cloudWorkspaceTaskSyncActive.Store(false)
	before := remoteCloudWorkspaceRunSignature()
	ent := a.CloudWorkspaceEntitlement()
	if ent.HubUnavailable {
		return
	}
	if !ent.Enabled && len(ent.Shared) == 0 {
		return
	}
	a.publishHeldCloudWorkspaceRunStatuses()
	restored := a.restoreCloudWorkspaceTasks()
	// A local status change already refreshed the sidebar that observed it.
	// Tell the UI only when Hub has a new task or someone else's status.
	if len(restored) > 0 || remoteCloudWorkspaceRunSignature() != before {
		a.emitProjectIndexChanged("")
	}
}
