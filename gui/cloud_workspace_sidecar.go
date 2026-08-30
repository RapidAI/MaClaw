package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	cloudWorkspaceSidecarSession    = "session.json"
	cloudWorkspaceSidecarTask       = "task.json"
	cloudWorkspaceSidecarWorkbench  = "coding_workbench.json"
	cloudWorkspaceSidecarCheckpoint = "coding_exec_checkpoint.json"
)

var cloudWorkspaceSidecarNames = []string{
	cloudWorkspaceSidecarSession,
	cloudWorkspaceSidecarTask,
	cloudWorkspaceSidecarWorkbench,
	cloudWorkspaceSidecarCheckpoint,
}

type cloudWorkspaceTaskSidecar struct {
	Name string `json:"name"`
	Mode string `json:"mode"`
	Tag  string `json:"tag"`
}

// cloudWorkspaceSessionSidecar is the Hub session.json payload: conversation
// plus draft input, with the local tab_id / project_path stripped.
type cloudWorkspaceSessionSidecar struct {
	Conversation []interface{} `json:"conversation"`
	InputText    string        `json:"input_text,omitempty"`
	ClearedAt    int64         `json:"cleared_at,omitempty"`
}

const maxCloudWorkspaceConversationEntries = 4000

// mergeCloudWorkspaceSessionHistory performs a deterministic union of local
// and remote turns. Frontend snapshots are whole-document replacements, so
// fetching and merging immediately before PUT prevents two machines saving at
// nearly the same time from erasing each other's history.
func mergeCloudWorkspaceSessionHistory(local, remote *TabSessionData) *TabSessionData {
	if local == nil && remote == nil {
		return nil
	}
	out := &TabSessionData{}
	if local != nil && local.ConversationClearedAt > out.ConversationClearedAt {
		out.ConversationClearedAt = local.ConversationClearedAt
	}
	if remote != nil && remote.ConversationClearedAt > out.ConversationClearedAt {
		out.ConversationClearedAt = remote.ConversationClearedAt
	}
	includeRemote := remote != nil
	includeLocal := local != nil
	if local != nil && remote != nil {
		switch {
		case local.ConversationClearedAt > remote.ConversationClearedAt:
			includeRemote = false
		case remote.ConversationClearedAt > local.ConversationClearedAt:
			includeLocal = false
		}
	}
	if includeRemote {
		out.Conversation = append(out.Conversation, remote.Conversation...)
		out.InputText = remote.InputText
	}
	if includeLocal {
		out.Conversation = append(out.Conversation, local.Conversation...)
		if strings.TrimSpace(local.InputText) != "" || !includeRemote {
			out.InputText = local.InputText
		}
	}
	seen := make(map[[32]byte]struct{}, len(out.Conversation))
	uniq := out.Conversation[:0]
	for _, item := range out.Conversation {
		raw, err := json.Marshal(item)
		if err != nil {
			continue
		}
		key := sha256.Sum256(raw)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		uniq = append(uniq, item)
	}
	// Preserve conversation order across machines. Most frontend entries carry
	// a millisecond timestamp; when absent, retain stable insertion order.
	sort.SliceStable(uniq, func(i, j int) bool {
		ti, oki := conversationEntryTimestamp(uniq[i])
		tj, okj := conversationEntryTimestamp(uniq[j])
		if oki && okj && ti != tj {
			return ti < tj
		}
		return false
	})
	if len(uniq) > maxCloudWorkspaceConversationEntries {
		uniq = uniq[len(uniq)-maxCloudWorkspaceConversationEntries:]
	}
	out.Conversation = uniq
	return out
}

func conversationEntryTimestamp(v interface{}) (int64, bool) {
	raw, err := json.Marshal(v)
	if err != nil {
		return 0, false
	}
	var obj map[string]interface{}
	if json.Unmarshal(raw, &obj) != nil {
		return 0, false
	}
	for _, key := range []string{"timestamp", "created_at", "createdAt", "ts"} {
		if n, ok := obj[key].(float64); ok && n > 0 {
			return int64(n), true
		}
	}
	return 0, false
}

func cloudWorkspaceSessionHistoryEqual(a, b *TabSessionData) bool {
	if a == nil || b == nil {
		return a == b
	}
	// Compare canonicalized histories so timestamp ordering/legacy insertion
	// order does not cause a no-op release to rewrite the remote sidecar.
	ca := mergeCloudWorkspaceSessionHistory(a, nil)
	cb := mergeCloudWorkspaceSessionHistory(b, nil)
	ra, _ := json.Marshal(ca.Conversation)
	rb, _ := json.Marshal(cb.Conversation)
	return string(ra) == string(rb) && a.InputText == b.InputText && a.ConversationClearedAt == b.ConversationClearedAt
}

var cloudWorkspaceSidecarMaxBytes = int64(cloudWorkspaceObjectMaxBytes)

type cloudWorkspaceSidecarBundle struct {
	Task       cloudWorkspaceTaskSidecar
	Session    *TabSessionData
	Workbench  []byte
	Checkpoint []byte
}

var (
	cloudWorkspaceSidecarMu       sync.Mutex
	cloudWorkspacePendingSidecars = map[string]cloudWorkspaceSidecarBundle{}
	cloudWorkspacePendingSessions = map[string]*TabSessionData{}
	cloudWorkspaceSessionFlushMu  sync.Mutex
	cloudWorkspaceSessionFlushes  = map[string]*sync.Mutex{}
)

func resetCloudWorkspaceSidecarState() {
	cloudWorkspaceSidecarMu.Lock()
	cloudWorkspacePendingSidecars = map[string]cloudWorkspaceSidecarBundle{}
	cloudWorkspacePendingSessions = map[string]*TabSessionData{}
	cloudWorkspaceSidecarMu.Unlock()
	// Keep the lock map intact: replacing it while a flush is waiting could
	// create two mutexes for the same workspace and reintroduce races.
}

func lockCloudWorkspaceSessionFlush(workspaceID string) func() {
	workspaceID = strings.TrimSpace(workspaceID)
	cloudWorkspaceSessionFlushMu.Lock()
	m := cloudWorkspaceSessionFlushes[workspaceID]
	if m == nil {
		m = &sync.Mutex{}
		cloudWorkspaceSessionFlushes[workspaceID] = m
	}
	cloudWorkspaceSessionFlushMu.Unlock()
	m.Lock()
	return m.Unlock
}

func storeCloudWorkspacePendingSidecars(workspaceID string, bundle cloudWorkspaceSidecarBundle) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return
	}
	cloudWorkspaceSidecarMu.Lock()
	defer cloudWorkspaceSidecarMu.Unlock()
	cloudWorkspacePendingSidecars[workspaceID] = bundle
}

func peekCloudWorkspacePendingSidecars(workspaceID string) (cloudWorkspaceSidecarBundle, bool) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return cloudWorkspaceSidecarBundle{}, false
	}
	cloudWorkspaceSidecarMu.Lock()
	defer cloudWorkspaceSidecarMu.Unlock()
	bundle, ok := cloudWorkspacePendingSidecars[workspaceID]
	return bundle, ok
}

func takeCloudWorkspacePendingSidecars(workspaceID string) (cloudWorkspaceSidecarBundle, bool) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return cloudWorkspaceSidecarBundle{}, false
	}
	cloudWorkspaceSidecarMu.Lock()
	defer cloudWorkspaceSidecarMu.Unlock()
	bundle, ok := cloudWorkspacePendingSidecars[workspaceID]
	delete(cloudWorkspacePendingSidecars, workspaceID)
	return bundle, ok
}

func storeCloudWorkspacePendingSession(projectPath string, session *TabSessionData) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" || session == nil {
		return
	}
	cloudWorkspaceSidecarMu.Lock()
	defer cloudWorkspaceSidecarMu.Unlock()
	cloudWorkspacePendingSessions[projectPath] = session
}

func takeCloudWorkspacePendingSession(projectPath string) *TabSessionData {
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return nil
	}
	cloudWorkspaceSidecarMu.Lock()
	defer cloudWorkspaceSidecarMu.Unlock()
	session := cloudWorkspacePendingSessions[projectPath]
	delete(cloudWorkspacePendingSessions, projectPath)
	return session
}

func cloudWorkspaceModeFromTags(tags []string) string {
	for _, tag := range tags {
		switch strings.TrimSpace(tag) {
		case taskCodingDevTag, taskRemoteCodingDevTag:
			return strings.TrimSpace(tag)
		}
	}
	return ""
}

func (a *App) cloudWorkspaceTaskProjectPath(workspaceID string) string {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return ""
	}
	if result, ok := lookupCloudWorkspaceTask(workspaceID); ok {
		if path := normalizeProjectSessionPath(result.ProjectPath); path != "" {
			return path
		}
	}
	return normalizeProjectSessionPath(a.findVisibleCloudWorkspaceTask(workspaceID).ProjectPath)
}

func (a *App) putCloudWorkspaceSidecarLimited(ctx context.Context, workspaceID, name string, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if int64(len(data)) > cloudWorkspaceSidecarMaxBytes {
		log.Printf("[cloud_workspace] skip oversized sidecar workspace=%s name=%s size=%d", workspaceID, name, len(data))
		return nil
	}
	return a.putCloudWorkspaceSidecar(ctx, workspaceID, name, data)
}

func (a *App) putCloudWorkspaceSidecar(ctx context.Context, workspaceID, name string, data []byte) error {
	if a == nil || len(data) == 0 {
		return nil
	}
	resp, status, err := a.cloudWorkspaceHubDo(ctx, http.MethodPut, cloudWorkspaceSidecarPath(workspaceID, name), cloudWorkspaceHTTPOptions{
		timeout:     cloudWorkspaceTransferTimeout(int64(len(data))),
		maxRead:     cloudWorkspaceResponseMaxSize,
		accept:      "application/json",
		contentType: "application/octet-stream",
		rawBody:     data,
	})
	if err != nil {
		return err
	}
	if status >= 300 {
		return cloudWorkspaceAPIError(status, resp)
	}
	return nil
}

func (a *App) getCloudWorkspaceSidecar(ctx context.Context, workspaceID, name string) ([]byte, error) {
	if a == nil {
		return nil, fmt.Errorf("cloud workspace sync unavailable")
	}
	data, status, err := a.cloudWorkspaceHubDo(ctx, http.MethodGet, cloudWorkspaceSidecarPath(workspaceID, name), cloudWorkspaceHTTPOptions{
		timeout: 60 * time.Second,
		maxRead: cloudWorkspaceObjectMaxBytes,
		accept:  "application/octet-stream",
	})
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, nil
	}
	if status >= 300 {
		return nil, cloudWorkspaceAPIError(status, data)
	}
	return data, nil
}

func readCloudWorkspaceSidecarFile(path string) []byte {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return nil
	}
	return data
}

func (a *App) collectCloudWorkspaceTabSession(projectPath string) *TabSessionData {
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return nil
	}
	persist := a.ensureProjectTabSessionPersist()
	if persist == nil {
		return nil
	}
	index, err := persist.LoadIndex()
	if err != nil || index == nil {
		return nil
	}
	liveID, archivedID := "", ""
	var liveAt, archivedAt int64
	for _, entry := range index.Tabs {
		if normalizeProjectSessionPath(entry.ProjectPath) != projectPath {
			continue
		}
		if entry.Archived {
			if archivedID == "" || entry.LastActiveAt >= archivedAt {
				archivedID = entry.ID
				archivedAt = entry.LastActiveAt
			}
			continue
		}
		if liveID == "" || entry.LastActiveAt >= liveAt {
			liveID = entry.ID
			liveAt = entry.LastActiveAt
		}
	}
	tabID := liveID
	if tabID == "" {
		tabID = archivedID
	}
	if tabID == "" && a != nil {
		a.tabProjectPaths.Range(func(key, value any) bool {
			id, _ := key.(string)
			path, _ := value.(string)
			if strings.TrimSpace(id) == "" {
				return true
			}
			if normalizeProjectSessionPath(path) != projectPath {
				return true
			}
			tabID = id
			return false
		})
	}
	if tabID == "" {
		return nil
	}
	session, err := persist.LoadSession(tabID)
	if err != nil {
		return nil
	}
	return session
}

func marshalCloudWorkspaceSessionSidecar(session *TabSessionData) []byte {
	if session == nil {
		return nil
	}
	raw, err := json.Marshal(cloudWorkspaceSessionSidecar{
		Conversation: session.Conversation,
		InputText:    session.InputText,
		ClearedAt:    session.ConversationClearedAt,
	})
	if err != nil || len(raw) == 0 {
		return nil
	}
	return raw
}

func parseCloudWorkspaceSessionSidecar(data []byte) *TabSessionData {
	if len(data) == 0 {
		return nil
	}
	var payload cloudWorkspaceSessionSidecar
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	return &TabSessionData{Conversation: payload.Conversation, InputText: payload.InputText, ConversationClearedAt: payload.ClearedAt}
}

func mergeCloudWorkspaceTabSession(dst, src *TabSessionData, tabID, projectPath string) {
	if dst == nil || src == nil {
		return
	}
	dst.TabID = tabID
	dst.ProjectPath = projectPath
	// Keep turns already present on this machine while incorporating the cloud
	// snapshot.  This matters when a reopen races a debounced local save.
	merged := mergeCloudWorkspaceSessionHistory(dst, src)
	if merged != nil {
		dst.Conversation = merged.Conversation
		if strings.TrimSpace(merged.InputText) != "" || strings.TrimSpace(dst.InputText) == "" {
			dst.InputText = merged.InputText
		}
	}
	dst.ScrollTop = src.ScrollTop
}

func (a *App) restoreCloudWorkspaceTabSession(projectPath string, session *TabSessionData) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" || session == nil {
		return
	}
	persist := a.ensureProjectTabSessionPersist()
	if persist == nil {
		storeCloudWorkspacePendingSession(projectPath, session)
		return
	}
	index, _ := persist.LoadIndex()
	tabID := ""
	if index != nil {
		for _, entry := range index.Tabs {
			if entry.Archived {
				continue
			}
			if normalizeProjectSessionPath(entry.ProjectPath) == projectPath {
				tabID = entry.ID
				break
			}
		}
	}
	if tabID != "" {
		existing, err := persist.LoadSession(tabID)
		if err != nil {
			existing = nil
		}
		if existing == nil {
			existing = &TabSessionData{TabID: tabID, ProjectPath: projectPath, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
		}
		mergeCloudWorkspaceTabSession(existing, session, tabID, projectPath)
		if err := persist.SaveSession(existing); err != nil {
			log.Printf("[cloud_workspace] restore session failed project=%q err=%v", projectPath, err)
		}
		return
	}
	storeCloudWorkspacePendingSession(projectPath, session)
}

func (a *App) flushCloudWorkspaceSidecars(ctx context.Context, workspaceID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil
	}
	projectPath := a.cloudWorkspaceTaskProjectPath(workspaceID)
	if projectPath == "" {
		return nil
	}
	ownerID := projectSessionOwnerID(projectPath)
	if data := readCloudWorkspaceSidecarFile(stickyCodingMemoryFilePath(ownerID)); len(data) > 0 {
		if err := a.putCloudWorkspaceSidecarLimited(ctx, workspaceID, cloudWorkspaceSidecarWorkbench, data); err != nil {
			return err
		}
	}
	if data := readCloudWorkspaceSidecarFile(codingExecCheckpointFilePath(ownerID, projectPath)); len(data) > 0 {
		if err := a.putCloudWorkspaceSidecarLimited(ctx, workspaceID, cloudWorkspaceSidecarCheckpoint, data); err != nil {
			return err
		}
	}
	if err := a.flushCloudWorkspaceSession(ctx, workspaceID, projectPath); err != nil {
		return err
	}
	return a.flushCloudWorkspaceTaskSidecar(ctx, workspaceID)
}

func (a *App) cloudWorkspaceTaskSidecarPayload(workspaceID string) cloudWorkspaceTaskSidecar {
	result := a.findVisibleCloudWorkspaceTask(workspaceID)
	if strings.TrimSpace(result.Name) == "" && strings.TrimSpace(result.ProjectPath) == "" {
		if cached, ok := lookupCloudWorkspaceTask(workspaceID); ok {
			result = cached
		}
	}
	return cloudWorkspaceTaskSidecar{
		Name: strings.TrimSpace(result.Name),
		Mode: cloudWorkspaceModeFromTags(result.Tags),
		Tag:  cloudWorkspaceTag(workspaceID),
	}
}

func (a *App) flushCloudWorkspaceTaskSidecar(ctx context.Context, workspaceID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil
	}
	task := a.cloudWorkspaceTaskSidecarPayload(workspaceID)
	if task.Name == "" && task.Mode == "" {
		return nil
	}
	raw, err := json.Marshal(task)
	if err != nil {
		return err
	}
	return a.putCloudWorkspaceSidecar(ctx, workspaceID, cloudWorkspaceSidecarTask, raw)
}

func (a *App) flushCloudWorkspaceTaskSidecarBestEffort(workspaceID string) {
	workspaceID = strings.TrimSpace(workspaceID)
	if a == nil || workspaceID == "" {
		return
	}
	ctx, cancel := a.cloudWorkspaceRequestContext()
	defer cancel()
	if err := a.flushCloudWorkspaceTaskSidecar(ctx, workspaceID); err != nil {
		log.Printf("[cloud_workspace] task sidecar flush failed workspace=%s err=%v", workspaceID, err)
	}
}

// flushCloudWorkspaceSessionBestEffort publishes the latest conversation
// snapshot as soon as a project tab saves it.  File watchers only observe
// workspace files, so without this hook an idle chat would remain local until
// lease release and could not be resumed from another machine.
func (a *App) flushCloudWorkspaceSessionBestEffort(projectPath string) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if a == nil || projectPath == "" {
		return
	}
	workspaceID := a.lookupCloudWorkspaceIDForProject(projectPath)
	if workspaceID == "" {
		return
	}
	ctx, cancel := a.cloudWorkspaceRequestContext()
	defer cancel()
	if err := a.flushCloudWorkspaceSession(ctx, workspaceID, projectPath); err != nil {
		log.Printf("[cloud_workspace] session sidecar flush failed workspace=%s err=%v", workspaceID, err)
	}
}

// flushCloudWorkspaceSession publishes only conversation state.  Keeping this
// path separate from full workspace pushes avoids re-uploading large coding
// checkpoints on every debounced chat-history update.
func (a *App) flushCloudWorkspaceSession(ctx context.Context, workspaceID, projectPath string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	projectPath = normalizeProjectSessionPath(projectPath)
	if workspaceID == "" || projectPath == "" {
		return nil
	}
	session := a.collectCloudWorkspaceTabSession(projectPath)
	if session == nil {
		return nil
	}
	unlock := lockCloudWorkspaceSessionFlush(workspaceID)
	defer unlock()
	remoteSession := (*TabSessionData)(nil)
	remoteRaw, err := a.getCloudWorkspaceSidecar(ctx, workspaceID, cloudWorkspaceSidecarSession)
	if err != nil {
		return err
	}
	if len(remoteRaw) > 0 {
		remoteSession = parseCloudWorkspaceSessionSidecar(remoteRaw)
		if remoteSession == nil {
			return fmt.Errorf("invalid cloud workspace session history")
		}
		// A local clear is an explicit fence and must not be undone by merging
		// the previous remote transcript back into the new snapshot.
		if session.ConversationClearedAt > 0 && session.ConversationClearedAt >= remoteSession.ConversationClearedAt {
			remoteSession = nil
		} else {
			session = mergeCloudWorkspaceSessionHistory(session, remoteSession)
		}
	}
	raw := marshalCloudWorkspaceSessionSidecar(session)
	if len(raw) == 0 || cloudWorkspaceSessionHistoryEqual(session, remoteSession) {
		return nil
	}
	return a.putCloudWorkspaceSidecarLimited(ctx, workspaceID, cloudWorkspaceSidecarSession, raw)
}

// refreshCloudWorkspaceSidecars fetches the cloud snapshot even when the
// workspace lease is already held by this process.  PrepareCloudWorkspace's
// fast path intentionally skips the pull; an explicit task reopen must still
// refresh conversation history from other machines.
func (a *App) refreshCloudWorkspaceSidecars(workspaceID, projectPath string) {
	workspaceID = strings.TrimSpace(workspaceID)
	projectPath = normalizeProjectSessionPath(projectPath)
	if a == nil || workspaceID == "" || projectPath == "" {
		return
	}
	ctx, cancel := a.cloudWorkspaceSyncContext()
	defer cancel()
	a.fetchCloudWorkspaceSidecars(ctx, workspaceID)
	a.applyCloudWorkspaceSidecars(workspaceID, projectPath)
}

func (a *App) fetchCloudWorkspaceSidecars(ctx context.Context, workspaceID string) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" || a == nil {
		return
	}
	var bundle cloudWorkspaceSidecarBundle
	for _, name := range cloudWorkspaceSidecarNames {
		data, err := a.getCloudWorkspaceSidecar(ctx, workspaceID, name)
		if err != nil {
			log.Printf("[cloud_workspace] get sidecar failed workspace=%s name=%s err=%v", workspaceID, name, err)
			continue
		}
		if len(data) == 0 {
			continue
		}
		switch name {
		case cloudWorkspaceSidecarWorkbench:
			bundle.Workbench = data
		case cloudWorkspaceSidecarCheckpoint:
			bundle.Checkpoint = data
		case cloudWorkspaceSidecarSession:
			session := parseCloudWorkspaceSessionSidecar(data)
			if session == nil {
				log.Printf("[cloud_workspace] session sidecar json workspace=%s err=invalid", workspaceID)
				continue
			}
			bundle.Session = session
		case cloudWorkspaceSidecarTask:
			var task cloudWorkspaceTaskSidecar
			if err := json.Unmarshal(data, &task); err != nil {
				log.Printf("[cloud_workspace] task sidecar json workspace=%s err=%v", workspaceID, err)
				continue
			}
			bundle.Task = task
		}
	}
	storeCloudWorkspacePendingSidecars(workspaceID, bundle)
}

func (a *App) applyCloudWorkspaceSidecars(workspaceID, projectPath string) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return
	}
	bundle, ok := takeCloudWorkspacePendingSidecars(workspaceID)
	if !ok {
		return
	}
	ownerID := projectSessionOwnerID(projectPath)
	if len(bundle.Workbench) > 0 {
		path := stickyCodingMemoryFilePath(ownerID)
		if path != "" {
			if err := atomicWriteFile(path, bundle.Workbench); err != nil {
				log.Printf("[cloud_workspace] restore sticky failed path=%q err=%v", path, err)
			}
		}
	}
	if len(bundle.Checkpoint) > 0 {
		path := codingExecCheckpointFilePath(ownerID, projectPath)
		if path != "" {
			if err := atomicWriteFile(path, bundle.Checkpoint); err != nil {
				log.Printf("[cloud_workspace] restore checkpoint failed path=%q err=%v", path, err)
			}
		}
	}
	if bundle.Session != nil {
		a.restoreCloudWorkspaceTabSession(projectPath, bundle.Session)
	}
}

func (a *App) cloudWorkspaceTaskIdentity(workspaceID, name, mode string) (string, string) {
	if bundle, ok := peekCloudWorkspacePendingSidecars(workspaceID); ok {
		if strings.TrimSpace(bundle.Task.Name) != "" {
			name = bundle.Task.Name
		}
		if strings.TrimSpace(bundle.Task.Mode) != "" {
			mode = bundle.Task.Mode
		}
	}
	return name, mode
}

func (a *App) pushCloudWorkspace(ctx context.Context, workspaceID, root string) (*cloudWorkspaceManifest, error) {
	proto := a.cloudWorkspaceProtocol(workspaceID)
	// Prefer v2 per-file operations for multi-machine mounts. Legacy manifest
	// replacement remains the compatibility fallback for older Hubs.
	if err := proto.PushOperations(ctx, root); err == nil {
		man, getErr := proto.Transport.GetManifest(ctx)
		if getErr != nil {
			return nil, getErr
		}
		if err := a.flushCloudWorkspaceSidecars(ctx, workspaceID); err != nil {
			return man, err
		}
		return man, nil
	} else if !errors.Is(err, errCloudWorkspaceV2Unavailable) {
		return nil, err
	}
	man, err := proto.Push(ctx, root)
	if err != nil {
		return nil, err
	}
	if err := a.flushCloudWorkspaceSidecars(ctx, workspaceID); err != nil {
		return man, err
	}
	return man, nil
}
