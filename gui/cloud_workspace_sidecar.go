package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
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
	Conversation []interface{} `json:"conversation,omitempty"`
	InputText    string        `json:"input_text,omitempty"`
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
)

func resetCloudWorkspaceSidecarState() {
	cloudWorkspaceSidecarMu.Lock()
	defer cloudWorkspaceSidecarMu.Unlock()
	cloudWorkspacePendingSidecars = map[string]cloudWorkspaceSidecarBundle{}
	cloudWorkspacePendingSessions = map[string]*TabSessionData{}
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
	if payload.Conversation == nil && strings.TrimSpace(payload.InputText) == "" {
		return nil
	}
	return &TabSessionData{Conversation: payload.Conversation, InputText: payload.InputText}
}

func mergeCloudWorkspaceTabSession(dst, src *TabSessionData, tabID, projectPath string) {
	if dst == nil || src == nil {
		return
	}
	dst.TabID = tabID
	dst.ProjectPath = projectPath
	if src.Conversation != nil {
		dst.Conversation = src.Conversation
	}
	dst.InputText = src.InputText
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
		if err := a.putCloudWorkspaceSidecar(ctx, workspaceID, cloudWorkspaceSidecarWorkbench, data); err != nil {
			return err
		}
	}
	if data := readCloudWorkspaceSidecarFile(codingExecCheckpointFilePath(ownerID, projectPath)); len(data) > 0 {
		if int64(len(data)) > cloudWorkspaceSidecarMaxBytes {
			log.Printf("[cloud_workspace] skip oversized checkpoint sidecar workspace=%s size=%d", workspaceID, len(data))
		} else if err := a.putCloudWorkspaceSidecar(ctx, workspaceID, cloudWorkspaceSidecarCheckpoint, data); err != nil {
			return err
		}
	}
	if session := a.collectCloudWorkspaceTabSession(projectPath); session != nil {
		if raw := marshalCloudWorkspaceSessionSidecar(session); len(raw) > 0 {
			if err := a.putCloudWorkspaceSidecar(ctx, workspaceID, cloudWorkspaceSidecarSession, raw); err != nil {
				return err
			}
		}
	}
	result := a.findVisibleCloudWorkspaceTask(workspaceID)
	if strings.TrimSpace(result.Name) == "" && strings.TrimSpace(result.ProjectPath) == "" {
		if cached, ok := lookupCloudWorkspaceTask(workspaceID); ok {
			result = cached
		}
	}
	task := cloudWorkspaceTaskSidecar{
		Name: strings.TrimSpace(result.Name),
		Mode: cloudWorkspaceModeFromTags(result.Tags),
		Tag:  cloudWorkspaceTag(workspaceID),
	}
	if task.Name != "" || task.Mode != "" {
		raw, err := json.Marshal(task)
		if err != nil {
			return err
		}
		if err := a.putCloudWorkspaceSidecar(ctx, workspaceID, cloudWorkspaceSidecarTask, raw); err != nil {
			return err
		}
	}
	return nil
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
	man, err := a.cloudWorkspaceProtocol(workspaceID).Push(ctx, root)
	if err != nil {
		return nil, err
	}
	if err := a.flushCloudWorkspaceSidecars(ctx, workspaceID); err != nil {
		return man, err
	}
	return man, nil
}
