package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var recentTaskForkMu sync.Mutex

func normalizeProjectSessionPath(projectPath string) string {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return ""
	}
	if strings.HasPrefix(projectPath, "/") {
		return path.Clean(projectPath)
	}
	cleaned := filepath.Clean(projectPath)
	if strings.HasSuffix(cleaned, string(filepath.Separator)+".") {
		cleaned = filepath.Clean(cleaned[:len(cleaned)-2])
	}
	if len(cleaned) >= 2 && cleaned[1] == ':' {
		cleaned = strings.ToUpper(cleaned[:1]) + cleaned[1:]
	}
	return cleaned
}

func projectSessionOwnerID(projectPath string) string {
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return desktopUserID
	}
	return fmt.Sprintf("desktop-user:%s", projectPath)
}

func projectPathFromSessionOwnerID(ownerID string) string {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" || ownerID == desktopUserID || !strings.HasPrefix(ownerID, desktopUserID+":") {
		return ""
	}
	return normalizeProjectSessionPath(strings.TrimPrefix(ownerID, desktopUserID+":"))
}

// ProjectSearchResult is the frontend-facing search result type.
// Exported as a Wails binding return type.
type ProjectSearchResult struct {
	ID              string                  `json:"id"`            // ProjectPath as stable ID
	Name            string                  `json:"name"`          // Human-readable project name
	ProjectPath     string                  `json:"project_path"`  // Canonical absolute path
	WorkflowType    string                  `json:"workflow_type"` // e.g. "coding", "product_design"
	ActiveWorkflow  *ProjectWorkflowState   `json:"active_workflow,omitempty"`
	Preview         string                  `json:"preview"`       // Short content preview (~150 chars)
	Tags            []string                `json:"tags"`          // Union of all entry tags
	LastActivity    string                  `json:"last_activity"` // RFC3339 formatted timestamp
	EntryCount      int                     `json:"entry_count"`   // Number of memory entries
	HasOutput       bool                    `json:"has_output"`    // Whether the task has tangible output
	Pinned          bool                    `json:"pinned"`        // Whether the task is pinned to top
	Archived        bool                    `json:"archived"`      // Whether the task is archived
	SourceURLs      []string                `json:"source_urls,omitempty"`
	RecentArtifacts []ProjectSearchArtifact `json:"recent_artifacts,omitempty"`
}

// ProjectWorkflowState is a compact pointer to an unfinished workflow attached
// to a recent task artifact. Continue the workflow with ProjectPath; normal
// opens may use an independent fork for unconstrained follow-up work.
type ProjectWorkflowState struct {
	ID            string `json:"id,omitempty"`
	Type          string `json:"type,omitempty"`
	Phase         string `json:"phase,omitempty"`
	Status        string `json:"status,omitempty"`
	ProjectPath   string `json:"project_path,omitempty"`
	PendingReview bool   `json:"pending_review,omitempty"`
}

// ProjectSearchArtifact is the small source-backed artifact summary attached
// to task search results. Full content stays in SourceURL-backed refs.
type ProjectSearchArtifact struct {
	Title      string `json:"title,omitempty"`
	SourceType string `json:"source_type,omitempty"`
	SourceURL  string `json:"source_url,omitempty"`
	SourceHint string `json:"source_hint,omitempty"`
	Preview    string `json:"preview,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
}

// ProjectSceneDetail exposes the scene/task navigation data for one project.
// It is intentionally compact: sources are pointers for drill-down, not bodies.
type ProjectSceneDetail struct {
	ProjectPath     string                  `json:"project_path"`
	Name            string                  `json:"name,omitempty"`
	ActiveWorkflow  *ProjectWorkflowState   `json:"active_workflow,omitempty"`
	WorkflowTypes   []string                `json:"workflow_types,omitempty"`
	Tags            []string                `json:"tags,omitempty"`
	SourceURLs      []string                `json:"source_urls,omitempty"`
	RecentArtifacts []ProjectSearchArtifact `json:"recent_artifacts,omitempty"`
	EntryCount      int                     `json:"entry_count"`
	LastActivity    string                  `json:"last_activity,omitempty"`
	Preview         string                  `json:"preview,omitempty"`
}

// GetProjectScene returns source-backed navigation details for one project.
// This is a Wails binding for task detail and evidence drill-down UIs.
func (a *App) GetProjectScene(projectPath string) (*ProjectSceneDetail, error) {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return nil, fmt.Errorf("projectPath is required")
	}
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return nil, fmt.Errorf("memory store not initialized")
	}

	scene, ok := projectSceneMap(a.memoryStore.SceneIndex(100))[projectPath]
	if !ok {
		return &ProjectSceneDetail{ProjectPath: projectPath, Name: lastPathComponent(projectPath), ActiveWorkflow: a.activeWorkflowForRecentTaskPath(projectPath)}, nil
	}
	detail := projectSceneDetailFromRecord(scene)
	detail.ActiveWorkflow = a.activeWorkflowForRecentTaskPath(projectPath)
	return detail, nil
}

// SearchProjects searches the project index for projects matching the query.
// Returns up to `limit` results sorted by relevance (or recency if query is empty).
// This is a Wails binding method called from the frontend search box.
func (a *App) SearchProjects(query string, limit int) []ProjectSearchResult {
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return nil
	}

	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return nil
	}

	if limit <= 0 {
		limit = 10
	}

	var records []memory.ProjectRecord
	if query == "" {
		records = pi.ListRecent(limit)
	} else {
		records = pi.Search(query, limit)
	}
	records = collapseRecentTaskForkRecords(records)

	scenesByPath := projectSceneMap(a.memoryStore.SceneIndex(limit * 2))

	results := make([]ProjectSearchResult, 0, len(records))
	for _, rec := range records {
		result := projectRecordToSearchResult(pi, rec)
		enrichProjectSearchResultWithScene(&result, scenesByPath[rec.ProjectPath])
		result.ActiveWorkflow = a.activeWorkflowForProject(projectWorkflowProjectPathForRecord(rec))
		results = append(results, result)
	}

	return results
}

func collapseRecentTaskForkRecords(records []memory.ProjectRecord) []memory.ProjectRecord {
	if len(records) <= 1 {
		return records
	}
	bySource := make(map[string]memory.ProjectRecord, len(records))
	order := make([]string, 0, len(records))
	for _, rec := range records {
		key := recentTaskLineageKey(rec)
		if key == "" {
			key = normalizeRecentTaskPathKey(rec.ProjectPath)
		}
		if key == "" {
			key = rec.ProjectPath
		}
		current, exists := bySource[key]
		if !exists {
			bySource[key] = rec
			order = append(order, key)
			continue
		}
		if preferRecentTaskRecord(rec, current) {
			bySource[key] = rec
		}
	}
	out := make([]memory.ProjectRecord, 0, len(order))
	for _, key := range order {
		out = append(out, bySource[key])
	}
	return out
}

func recentTaskLineageKey(rec memory.ProjectRecord) string {
	if !projectRecordHasTag(rec, "forked_task") {
		return normalizeRecentTaskPathKey(rec.ProjectPath)
	}
	for _, tag := range rec.Tags {
		if strings.HasPrefix(tag, "source:") {
			return normalizeRecentTaskPathKey(strings.TrimPrefix(tag, "source:"))
		}
	}
	return normalizeRecentTaskPathKey(rec.ProjectPath)
}

func preferRecentTaskRecord(candidate, current memory.ProjectRecord) bool {
	candidateFork := projectRecordHasTag(candidate, "forked_task")
	currentFork := projectRecordHasTag(current, "forked_task")
	if candidateFork != currentFork {
		return candidateFork
	}
	return candidate.LastActivity.After(current.LastActivity)
}

func projectRecordToSearchResult(pi *memory.ProjectIndex, rec memory.ProjectRecord) ProjectSearchResult {
	r := ProjectSearchResult{
		ID:           rec.ProjectPath,
		Name:         rec.Name,
		ProjectPath:  rec.ProjectPath,
		WorkflowType: rec.WorkflowType,
		Preview:      rec.Preview,
		Tags:         rec.Tags,
		LastActivity: rec.LastActivity.Format(time.RFC3339),
		EntryCount:   rec.EntryCount,
		HasOutput:    rec.HasOutput,
		Pinned:       pi.IsPinned(rec.ProjectPath),
		Archived:     pi.IsArchived(rec.ProjectPath),
	}

	// Generate a human-readable name when the index couldn't extract one.
	// Priority: user custom name > extracted name > preview-based summary > directory name.
	if custom := pi.CustomName(rec.ProjectPath); custom != "" {
		r.Name = custom
	} else if r.Name == "" {
		r.Name = deriveTaskName(rec)
	}
	return r
}

func projectSceneMap(scenes []memory.SceneRecord) map[string]memory.SceneRecord {
	if len(scenes) == 0 {
		return nil
	}
	byPath := make(map[string]memory.SceneRecord, len(scenes))
	for _, scene := range scenes {
		if scene.ProjectPath == "" {
			continue
		}
		byPath[scene.ProjectPath] = scene
	}
	return byPath
}

func projectSceneDetailFromRecord(scene memory.SceneRecord) *ProjectSceneDetail {
	detail := &ProjectSceneDetail{
		ProjectPath:   scene.ProjectPath,
		Name:          scene.Name,
		WorkflowTypes: append([]string(nil), scene.WorkflowTypes...),
		Tags:          append([]string(nil), scene.Tags...),
		SourceURLs:    append([]string(nil), scene.SourceURLs...),
		EntryCount:    scene.EntryCount,
		Preview:       scene.Preview,
	}
	if !scene.LastActivity.IsZero() {
		detail.LastActivity = scene.LastActivity.Format(time.RFC3339)
	}
	for _, artifact := range scene.RecentArtifacts {
		item := ProjectSearchArtifact{
			Title:      artifact.Title,
			SourceType: artifact.SourceType,
			SourceURL:  artifact.SourceURL,
			SourceHint: projectTabSourceHint(artifact.SourceURL),
			Preview:    artifact.Preview,
		}
		if !artifact.UpdatedAt.IsZero() {
			item.UpdatedAt = artifact.UpdatedAt.Format(time.RFC3339)
		}
		detail.RecentArtifacts = append(detail.RecentArtifacts, item)
	}
	return detail
}

func enrichProjectSearchResultWithScene(result *ProjectSearchResult, scene memory.SceneRecord) {
	if result == nil || scene.ProjectPath == "" {
		return
	}
	result.SourceURLs = append([]string(nil), scene.SourceURLs...)
	for _, artifact := range scene.RecentArtifacts {
		item := ProjectSearchArtifact{
			Title:      artifact.Title,
			SourceType: artifact.SourceType,
			SourceURL:  artifact.SourceURL,
			SourceHint: projectTabSourceHint(artifact.SourceURL),
			Preview:    artifact.Preview,
		}
		if !artifact.UpdatedAt.IsZero() {
			item.UpdatedAt = artifact.UpdatedAt.Format(time.RFC3339)
		}
		result.RecentArtifacts = append(result.RecentArtifacts, item)
	}
}

// CreateRecentTask creates a lightweight standalone task record so it appears
// in the recent task list immediately and after restart.
func (a *App) CreateRecentTask(name string) ProjectSearchResult {
	taskName := normalizeRecentTaskName(name)
	if taskName == "" {
		return ProjectSearchResult{}
	}
	return a.createRecentTaskRecord(taskName, "", nil, false)
}

// ForkRecentTask returns the independent task/session for a recent task. The
// first open creates that session; later opens of the same source return the
// same visible fork instead of creating duplicate recent-task rows.
func (a *App) ForkRecentTask(sourceProjectPath string) ProjectSearchResult {
	started := time.Now()
	sourceProjectPath = strings.TrimSpace(sourceProjectPath)
	if sourceProjectPath == "" {
		return ProjectSearchResult{}
	}

	var copySourcePath string
	var copyTargetPath string
	result := func() ProjectSearchResult {
		recentTaskForkMu.Lock()
		defer recentTaskForkMu.Unlock()
		lockStarted := time.Now()
		log.Printf("[project_search] ForkRecentTask requested source=%q", sourceProjectPath)

		a.ensureMemoryStore()
		if a.memoryStore == nil {
			log.Printf("[project_search] ForkRecentTask skipped source=%q reason=memory_store_unavailable elapsed=%s", sourceProjectPath, time.Since(started).Round(time.Millisecond))
			return ProjectSearchResult{}
		}
		if pi := a.memoryStore.ProjectIndex(); pi != nil {
			if rec := pi.Get(sourceProjectPath); rec != nil && strings.TrimSpace(rec.ProjectPath) != "" {
				sourceProjectPath = rec.ProjectPath
			}
		}
		pi := a.memoryStore.ProjectIndex()
		if pi != nil && (pi.IsHidden(sourceProjectPath) || pi.IsArchived(sourceProjectPath)) {
			log.Printf("[project_search] ForkRecentTask skipped source=%q reason=closed_task elapsed=%s", sourceProjectPath, time.Since(started).Round(time.Millisecond))
			return ProjectSearchResult{}
		}
		if pi != nil {
			if rec := pi.Get(sourceProjectPath); rec != nil && projectRecordHasTag(*rec, "forked_task") {
				a.ensureRecentTaskWorkspace(rec.ProjectPath, rec.Name)
				log.Printf("[project_search] ForkRecentTask reuse existing fork source=%q fork=%q reason=source_is_fork elapsed=%s", sourceProjectPath, rec.ProjectPath, time.Since(started).Round(time.Millisecond))
				return projectRecordToSearchResult(pi, *rec)
			}
			if existing := findVisibleForkForSource(pi, sourceProjectPath); existing != nil {
				a.ensureRecentTaskWorkspace(existing.ProjectPath, existing.Name)
				log.Printf("[project_search] ForkRecentTask reuse existing fork source=%q fork=%q elapsed=%s", sourceProjectPath, existing.ProjectPath, time.Since(started).Round(time.Millisecond))
				return projectRecordToSearchResult(pi, *existing)
			}
		}

		taskName := ""
		if pi != nil {
			taskName = pi.GetDisplayName(sourceProjectPath)
		}
		if taskName == "" {
			taskName = lastPathComponent(sourceProjectPath)
		}
		taskName = normalizeRecentTaskName(taskName)
		if taskName == "" {
			taskName = "Forked task"
		}

		content := fmt.Sprintf("# %s\n\nForked from recent task.\nSource task: %s\nFork ID: %d", taskName, sourceProjectPath, time.Now().UnixNano())
		created := a.createRecentTaskRecord(taskName, content, []string{"forked_task", "source:" + sourceProjectPath}, false)
		if created.ProjectPath == "" {
			log.Printf("[project_search] ForkRecentTask create failed source=%q elapsed=%s", sourceProjectPath, time.Since(started).Round(time.Millisecond))
			return created
		}
		copySourcePath = sourceProjectPath
		copyTargetPath = created.ProjectPath
		a.emitProjectIndexChanged(sourceProjectPath)
		a.emitProjectIndexChanged(created.ProjectPath)
		log.Printf("[project_search] ForkRecentTask created independent fork source=%q fork=%q critical_elapsed=%s lock_elapsed=%s", sourceProjectPath, created.ProjectPath, time.Since(started).Round(time.Millisecond), time.Since(lockStarted).Round(time.Millisecond))
		return created
	}()

	if copySourcePath != "" && copyTargetPath != "" {
		go func(source, target string) {
			copyStarted := time.Now()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[project_search] ForkRecentTask async copy panic source=%q fork=%q panic=%v elapsed=%s", source, target, r, time.Since(copyStarted).Round(time.Millisecond))
				}
			}()
			a.copyProjectConversation(source, target)
			log.Printf("[project_search] ForkRecentTask async copy complete source=%q fork=%q elapsed=%s", source, target, time.Since(copyStarted).Round(time.Millisecond))
		}(copySourcePath, copyTargetPath)
	}
	return result
}

func findVisibleForkForSource(pi *memory.ProjectIndex, sourceProjectPath string) *memory.ProjectRecord {
	if pi == nil || strings.TrimSpace(sourceProjectPath) == "" {
		return nil
	}
	sourceKey := normalizeRecentTaskPathKey(sourceProjectPath)
	for _, rec := range pi.ListRecent(1000) {
		if rec.ProjectPath == sourceProjectPath {
			continue
		}
		if !projectRecordHasTag(rec, "forked_task") {
			continue
		}
		if pi.IsHidden(rec.ProjectPath) || pi.IsArchived(rec.ProjectPath) {
			continue
		}
		for _, tag := range rec.Tags {
			if strings.HasPrefix(tag, "source:") && normalizeRecentTaskPathKey(strings.TrimPrefix(tag, "source:")) == sourceKey {
				clone := rec
				return &clone
			}
		}
	}
	return nil
}

func projectRecordHasTag(rec memory.ProjectRecord, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, tag := range rec.Tags {
		if strings.TrimSpace(tag) == target {
			return true
		}
	}
	return false
}

func projectWorkflowProjectPathForRecord(rec memory.ProjectRecord) string {
	if !projectRecordHasTag(rec, "forked_task") {
		return rec.ProjectPath
	}
	for _, tag := range rec.Tags {
		if strings.HasPrefix(tag, "source:") {
			if source := strings.TrimSpace(strings.TrimPrefix(tag, "source:")); source != "" {
				return source
			}
		}
	}
	return rec.ProjectPath
}

func (a *App) activeWorkflowForRecentTaskPath(projectPath string) *ProjectWorkflowState {
	if a == nil {
		return nil
	}
	if a.memoryStore != nil {
		if pi := a.memoryStore.ProjectIndex(); pi != nil {
			if rec := pi.Get(projectPath); rec != nil {
				return a.activeWorkflowForProject(projectWorkflowProjectPathForRecord(*rec))
			}
		}
	}
	return a.activeWorkflowForProject(projectPath)
}

func (a *App) activeWorkflowForProject(projectPath string) *ProjectWorkflowState {
	projectPath = normalizeProjectSessionPath(projectPath)
	if a == nil || projectPath == "" || a.workflowEngine == nil || a.workflowDisabled.Load() {
		return nil
	}
	ws := a.workflowEngine.GetActiveWorkflow(projectSessionOwnerID(projectPath))
	if ws == nil {
		return nil
	}
	workflowProjectPath := normalizeProjectSessionPath(ws.ProjectPath)
	if workflowProjectPath == "" {
		workflowProjectPath = projectPath
	}
	return &ProjectWorkflowState{
		ID:            ws.ID,
		Type:          string(ws.Type),
		Phase:         ws.CurrentPhase,
		Status:        string(ws.Status),
		ProjectPath:   workflowProjectPath,
		PendingReview: strings.TrimSpace(ws.PendingReviewPhaseID) != "",
	}
}

func normalizeRecentTaskPathKey(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
}

func (a *App) isManagedRecentTaskWorkspacePath(projectPath string) bool {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return false
	}
	pathKey := normalizeRecentTaskPathKey(projectPath)
	if pathKey == "" {
		return false
	}
	dataTaskRoot := normalizeRecentTaskPathKey(filepath.Join(a.GetDataDir(), "tasks"))
	if dataTaskRoot != "" && (pathKey == dataTaskRoot || strings.HasPrefix(pathKey, dataTaskRoot+"/")) {
		return true
	}
	defaultTaskRoot := normalizeRecentTaskPathKey(filepath.Join(corelib.MaclawDefaultBaseDir(), "data", "tasks"))
	return defaultTaskRoot != "" && (pathKey == defaultTaskRoot || strings.HasPrefix(pathKey, defaultTaskRoot+"/"))
}

func (a *App) ensureRecentTaskWorkspace(projectPath, taskName string) bool {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" || !a.isManagedRecentTaskWorkspacePath(projectPath) {
		return false
	}
	if info, err := os.Stat(projectPath); err == nil && info.IsDir() {
		return true
	}
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		log.Printf("[project_search] repair recent-task workspace failed project=%q err=%v", projectPath, err)
		return false
	}
	name := normalizeRecentTaskName(taskName)
	if name == "" {
		name = lastPathComponent(projectPath)
	}
	taskFile := filepath.Join(projectPath, "task.md")
	if _, err := os.Stat(taskFile); os.IsNotExist(err) {
		content := fmt.Sprintf("# %s\n\nRecovered recent-task workspace.\nProject path: %s\n", name, projectPath)
		if writeErr := os.WriteFile(taskFile, []byte(content), 0o644); writeErr != nil {
			log.Printf("[project_search] repair recent-task task.md failed project=%q err=%v", projectPath, writeErr)
		}
	}
	log.Printf("[project_search] repaired recent-task workspace project=%q", projectPath)
	return true
}

func (a *App) createRecentTaskRecord(taskName, taskContent string, extraTags []string, flushSync ...bool) ProjectSearchResult {
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return ProjectSearchResult{}
	}

	now := time.Now()
	taskDir := filepath.Join(a.GetDataDir(), "tasks", fmt.Sprintf("%s-%d", recentTaskSlug(taskName), now.UnixNano()))
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		log.Printf("[project_search] CreateRecentTask mkdir failed: %v", err)
		return ProjectSearchResult{}
	}
	taskFile := filepath.Join(taskDir, "task.md")
	if strings.TrimSpace(taskContent) == "" {
		taskContent = fmt.Sprintf("# %s\n\nCreated from recent tasks.\nTask ID: %d", taskName, now.UnixNano())
	}
	if err := os.WriteFile(taskFile, []byte(taskContent), 0o644); err != nil {
		log.Printf("[project_search] CreateRecentTask write task file failed: %v", err)
		return ProjectSearchResult{}
	}
	tags := append([]string{"manual_task", "recent_task", taskDir}, extraTags...)

	_, err := a.memoryStore.UpsertTaskArtifact(memory.TaskArtifactUpsertOptions{
		Title:            taskName,
		Content:          taskContent,
		Tags:             tags,
		IdentityTagCount: 3,
		SourceURL:        taskFile,
		SourceType:       "manual",
	})
	if err != nil {
		log.Printf("[project_search] CreateRecentTask save failed: %v", err)
		return ProjectSearchResult{}
	}
	a.triggerMemoryPipelineSoon(45 * time.Second)
	shouldFlushSync := true
	if len(flushSync) > 0 {
		shouldFlushSync = flushSync[0]
	}
	if shouldFlushSync {
		if err := a.memoryStore.Flush(); err != nil {
			log.Printf("[project_search] CreateRecentTask flush failed: %v", err)
		}
	} else {
		store := a.memoryStore
		go func(projectPath string) {
			started := time.Now()
			if err := store.Flush(); err != nil {
				log.Printf("[project_search] async task flush failed project=%q err=%v elapsed=%s", projectPath, err, time.Since(started).Round(time.Millisecond))
				return
			}
			log.Printf("[project_search] async task flush complete project=%q elapsed=%s", projectPath, time.Since(started).Round(time.Millisecond))
		}(taskDir)
	}
	a.emitProjectIndexChanged(taskDir)

	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return ProjectSearchResult{ID: taskDir, Name: taskName, ProjectPath: taskDir, LastActivity: now.Format(time.RFC3339), EntryCount: 1, HasOutput: true}
	}
	if rec := pi.Get(taskDir); rec != nil {
		return projectRecordToSearchResult(pi, *rec)
	}
	return ProjectSearchResult{ID: taskDir, Name: taskName, ProjectPath: taskDir, LastActivity: now.Format(time.RFC3339), EntryCount: 1, HasOutput: true}
}

func (a *App) copyProjectConversation(sourceProjectPath, targetProjectPath string) {
	started := time.Now()
	a.ensureInteractionInfra()
	hubClient := a.hubClient()
	if hubClient == nil {
		log.Printf("[project_search] ForkRecentTask copy skipped source=%q fork=%q reason=hub_client_unavailable elapsed=%s", sourceProjectPath, targetProjectPath, time.Since(started).Round(time.Millisecond))
		return
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil || handler.memory == nil {
		log.Printf("[project_search] ForkRecentTask copy skipped source=%q fork=%q reason=memory_unavailable elapsed=%s", sourceProjectPath, targetProjectPath, time.Since(started).Round(time.Millisecond))
		return
	}
	sourceUserID := projectSessionOwnerID(sourceProjectPath)
	targetUserID := projectSessionOwnerID(targetProjectPath)
	sourceEntries := handler.memory.Load(sourceUserID)
	if len(sourceEntries) == 0 {
		log.Printf("[project_search] ForkRecentTask copy skipped source=%q fork=%q reason=no_entries elapsed=%s", sourceProjectPath, targetProjectPath, time.Since(started).Round(time.Millisecond))
		return
	}
	handler.memory.Save(targetUserID, sourceEntries)
	log.Printf("[project_search] ForkRecentTask copied entries=%d source_user=%q target_user=%q elapsed=%s", len(sourceEntries), sourceUserID, targetUserID, time.Since(started).Round(time.Millisecond))
}

func (a *App) emitProjectIndexChanged(projectPath string) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, EventProjectIndexChanged, projectPath)
	runtime.EventsEmit(a.ctx, EventTasksChanged, nil)
}

func (a *App) emitProjectTaskClosed(projectPath string) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, EventProjectTaskClosed, projectPath)
}

func (a *App) cancelProjectTaskLoop(projectPath string) {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return
	}
	projectPath = normalizeProjectSessionPath(projectPath)
	ownerID := projectSessionOwnerID(projectPath)
	if a.localMCPManager != nil {
		a.localMCPManager.StopOwner(ownerID)
	}
	hubClient := a.hubClient()
	if hubClient == nil {
		log.Printf("[project_search] cancel project task loop skipped project=%q reason=hub_client_unavailable", projectPath)
		return
	}
	if cancelProjectTaskLoopForHandler(hubClient.ensureIMHandler(), projectPath) {
		log.Printf("[project_search] cancel project task loop requested project=%q", projectPath)
	} else {
		log.Printf("[project_search] cancel project task loop skipped project=%q reason=no_active_loop", projectPath)
	}
}

func cancelProjectTaskLoopForHandler(handler *IMMessageHandler, projectPath string) bool {
	projectPath = strings.TrimSpace(projectPath)
	if handler == nil || projectPath == "" {
		return false
	}
	userID := projectSessionOwnerID(projectPath)
	if !handler.hasActiveLoopForUser(userID) {
		return false
	}
	go func() {
		taskText, err := handler.CancelSessionForUser(userID)
		if err != nil {
			log.Printf("[project_search] cancel project task loop failed user=%q err=%v", userID, err)
			return
		}
		log.Printf("[project_search] cancel project task loop done user=%q task=%q", userID, truncateForLog(taskText, 120))
	}()
	return true
}

// ForkConversationToProject copies the current local tab's conversation history
// into a new project-scoped session. This enables the "fork current chat" feature:
// the user's existing conversation becomes the starting context for the new project tab.
//
// This is a Wails binding method called from the frontend after CreateRecentTask.
func (a *App) ForkConversationToProject(projectPath string) {
	if projectPath == "" {
		return
	}
	a.ensureInteractionInfra()
	hubClient := a.hubClient()
	if hubClient == nil {
		return
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil {
		return
	}

	// Load current local tab conversation.
	sourceEntries := handler.memory.Load(desktopUserID)
	if len(sourceEntries) == 0 {
		return
	}

	// Save to the project-scoped session.
	targetUserID := projectSessionOwnerID(projectPath)
	handler.memory.Save(targetUserID, sourceEntries)
	log.Printf("[project_search] ForkConversationToProject: copied %d entries from %s to %s",
		len(sourceEntries), desktopUserID, targetUserID)
}

func normalizeRecentTaskName(name string) string {
	normalized := strings.Join(strings.Fields(name), " ")
	if isGenericSedimentRequest(normalized) {
		return ""
	}
	runes := []rune(normalized)
	if len(runes) > 120 {
		normalized = string(runes[:120])
	}
	return normalized
}

func recentTaskSlug(name string) string {
	var b strings.Builder
	lastWasSeparator := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastWasSeparator = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastWasSeparator = false
		case r == '-' || r == '_':
			if b.Len() > 0 && !lastWasSeparator {
				b.WriteRune(r)
				lastWasSeparator = true
			}
		case b.Len() > 0 && !lastWasSeparator:
			b.WriteByte('-')
			lastWasSeparator = true
		}
		if b.Len() >= 40 {
			break
		}
	}
	slug := strings.Trim(b.String(), "-_")
	if slug == "" {
		return "task"
	}
	return slug
}

// ResumeProject switches the current context to the specified project.
// It clears the current conversation and cancels any active workflow,
// preparing a clean slate for the target project.
//
// Project context injection happens naturally through the existing proactive
// recall mechanism (appendProactiveRecall in im_system_prompt.go): when the
// user sends the next message, RecallDynamic will find the project's
// project_knowledge and task_artifact entries and inject them into the
// system prompt. No explicit seed entry is needed.
//
// Returns a human-readable summary for the frontend to display.
func (a *App) ResumeProject(projectPath string) string {
	if projectPath == "" {
		return ""
	}

	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return "记忆系统未初始化"
	}

	userID := desktopUserID

	// 0. Update config.CurrentProject so that GetCurrentProjectPath() returns
	//    the target project. Without this, all downstream consumers
	//    (appendProactiveRecall → RecallDynamic, workflow adapter, tool
	//    execution, etc.) would still resolve to the OLD project path,
	//    causing cross-project context contamination.
	//
	// Also notify frontend of the config change so its cached
	// config.current_project stays in sync. Without this, a subsequent full
	// frontend save could overwrite the backend's correct current_project
	// with a stale value (same race pattern as #11/#23).
	if updatedCfg := a.switchCurrentProjectByPath(projectPath); updatedCfg != nil && a.ctx != nil {
		runtime.EventsEmit(a.ctx, "config-changed", *updatedCfg)
	}

	// 1. Cancel any active workflow (user is switching projects).
	//    Do this BEFORE clearing conversation — CancelWorkflow may
	//    reference conversation state.
	if a.workflowEngine != nil {
		ws := a.workflowEngine.GetActiveWorkflow(userID)
		if ws != nil {
			_ = a.workflowEngine.CancelWorkflow(userID)
		}
	}

	// 2. Clear conversation memory AND per-user session state (drift detector,
	//    orchestrator, steering context, etc.). This mirrors what /new does.
	//    We do this on the backend so the frontend's subsequent clearHistory()
	//    is a no-op (idempotent Clear on already-empty memory).
	a.ensureInteractionInfra()
	hubClient := a.hubClient()
	if hubClient != nil {
		handler := hubClient.ensureIMHandler()
		if handler != nil {
			handler.memory.Clear(userID)
			handler.clearPerUserSessionState(userID)
		}
	}

	// 3. Resolve project name for the display message.
	//    GetDisplayName returns the custom name if the user renamed the task,
	//    otherwise the auto-generated name from entry content.
	pi := a.memoryStore.ProjectIndex()
	var projectName string
	if pi != nil {
		projectName = pi.GetDisplayName(projectPath)
	}
	if projectName == "" {
		projectName = lastPathComponent(projectPath)
	}

	log.Printf("[project_search] ResumeProject: path=%q, name=%q", projectPath, projectName)

	// Note: the frontend also calls clearHistory() which invokes
	// ClearAIAssistantHistory. That's a second Clear on already-empty memory —
	// idempotent, no harm done.

	msg := "🔖 已切换到任务：" + projectName
	// Only show the path line if it looks like a real user project path.
	// Suppress for:
	//   - Inferred paths like "\path.dirname" from tag fragments
	//   - Synthetic standalone task paths under {dataDir}/tasks/ (hash-based,
	//     meaningless to the user)
	showPath := memory.LooksLikeFilePath(projectPath)
	if showPath {
		dataDir := a.GetDataDir()
		if dataDir != "" && strings.HasPrefix(strings.ToLower(filepath.Clean(projectPath)), strings.ToLower(filepath.Clean(dataDir))) {
			showPath = false
		}
	}
	if showPath {
		msg += "\n📁 " + projectPath
	}
	return msg
}

// RenameTask sets a user-defined display name for a task in the recent tasks list.
// Pass empty name to revert to the auto-generated name.
// This is a Wails binding method called from the frontend inline editor.
func (a *App) RenameTask(projectPath, newName string) string {
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return ""
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return ""
	}
	pi.SetCustomName(projectPath, newName)
	return pi.GetDisplayName(projectPath)
}

// PinTask pins or unpins a task in the recent tasks list.
// Pinned tasks appear at the top of the list.
func (a *App) PinTask(projectPath string, pinned bool) {
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return
	}
	pi.SetPinned(projectPath, pinned)
}

// HideTask removes a task from the recent tasks list (soft delete).
// The underlying memory entries are preserved — only the list visibility is affected.
func (a *App) HideTask(projectPath string) {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		log.Printf("[project_search] HideTask skipped reason=empty_path")
		return
	}
	log.Printf("[project_search] HideTask requested path=%q", projectPath)
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		log.Printf("[project_search] HideTask skipped path=%q reason=memory_store_unavailable", projectPath)
		return
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		log.Printf("[project_search] HideTask skipped path=%q reason=project_index_unavailable", projectPath)
		return
	}
	pi.SetHidden(projectPath, true)
	log.Printf("[project_search] HideTask hidden path=%q", projectPath)
	a.cancelProjectTaskLoop(projectPath)
	a.emitProjectIndexChanged(projectPath)
	a.emitProjectTaskClosed(projectPath)
}

// switchCurrentProjectByPath updates config.CurrentProject to match the
// given project path. If the path matches an existing ProjectConfig entry,
// that entry's ID becomes CurrentProject. If no match is found (e.g. the
// project was discovered from memory but not in the config list), the
// config is not modified — GetCurrentProjectPath() will fall back to the
// first project in the list, which is acceptable.
//
// Returns the updated config on success (for event emission), or nil if
// no change was needed or an error occurred.
//
// This patches only current_project so concurrent config changes are preserved.
func (a *App) switchCurrentProjectByPath(projectPath string) *corelib.AppConfig {
	if projectPath == "" {
		return nil
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		log.Printf("[project_search] switchCurrentProjectByPath: LoadConfig failed: %v", err)
		return nil
	}

	cleanTarget := filepath.Clean(projectPath)
	for _, p := range cfg.Projects {
		if strings.EqualFold(filepath.Clean(p.Path), cleanTarget) {
			if cfg.CurrentProject == p.Id {
				return nil // already current, no-op
			}
			patched, err := a.PatchConfigFields(map[string]interface{}{"current_project": p.Id})
			if err != nil {
				log.Printf("[project_search] switchCurrentProjectByPath: PatchConfigFields failed: %v", err)
				return nil
			}
			log.Printf("[project_search] switchCurrentProjectByPath: switched to project %q (id=%s)", p.Name, p.Id)
			return &patched
		}
	}
	log.Printf("[project_search] switchCurrentProjectByPath: no matching project config for path %q", projectPath)
	return nil
}

// lastPathComponent returns the last directory component of a path.
func lastPathComponent(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			if i < len(p)-1 {
				return p[i+1:]
			}
		}
	}
	return p
}

// ---------------------------------------------------------------------------
// ArchiveProject triggers the full archive flow for a project:
//  1. Collect project entries from memory
//  2. LLM summarize into experience
//  3. Save experience as globally-recallable project_knowledge
//  4. Mark project as archived in ProjectIndex
//
// This is a Wails binding method called from the frontend context menu.
func (a *App) ArchiveProject(projectPath string) (*ArchiveResult, error) {
	if projectPath == "" {
		return nil, fmt.Errorf("project path is required")
	}

	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return nil, fmt.Errorf("记忆系统未初始化")
	}

	pi := a.memoryStore.ProjectIndex()

	// Get the LLM caller from the IMMessageHandler (may be nil if not ready).
	var llmCaller archiveLLMCaller
	handler := a.ensureLocalIMHandler()
	if handler != nil {
		llmCaller = handler
	}

	svc := NewArchiveService(a.memoryStore, llmCaller, pi)
	result, err := svc.Archive(context.Background(), ArchiveRequest{
		ProjectPath: projectPath,
	})
	if err != nil {
		return nil, err
	}

	// Flush memory store to persist the archived state and experience entry.
	if flushErr := a.memoryStore.Flush(); flushErr != nil {
		log.Printf("[ArchiveProject] flush failed: %v", flushErr)
	}

	log.Printf("[ArchiveProject] path=%q archived=%v experience=%v",
		projectPath, result.Archived, result.ExperienceExtracted)
	a.emitProjectIndexChanged(projectPath)
	if result.Archived {
		a.cancelProjectTaskLoop(projectPath)
		a.emitProjectTaskClosed(projectPath)
		log.Printf("[ArchiveProject] emitted task close path=%q", projectPath)
	}

	return result, nil
}

// GetArchivedExperience retrieves the archived experience summary for a project.
// Returns the experience text if found, or an empty string if no archived
// experience exists for the given project path.
// This is a Wails binding method called from the frontend read-only panel.
func (a *App) GetArchivedExperience(projectPath string) (string, error) {
	if projectPath == "" {
		return "", fmt.Errorf("project path is required")
	}

	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return "", fmt.Errorf("记忆系统未初始化")
	}

	// Search for the archived_experience entry matching this project path.
	a.memoryStore.RLock()
	defer a.memoryStore.RUnlock()

	normalizedPath := strings.ToLower(strings.ReplaceAll(projectPath, "\\", "/"))

	for _, e := range a.memoryStore.Entries() {
		cat := memory.MapToCanonical(e.Category)
		if cat != memory.CategoryProjectKnowledge {
			continue
		}
		// Check for "archived_experience" tag + matching project path in tags.
		hasArchiveTag := false
		hasPathTag := false
		for _, tag := range e.Tags {
			if tag == "archived_experience" {
				hasArchiveTag = true
			}
			normalizedTag := strings.ToLower(strings.ReplaceAll(tag, "\\", "/"))
			if normalizedTag == normalizedPath {
				hasPathTag = true
			}
		}
		if hasArchiveTag && hasPathTag {
			return e.Content, nil
		}
	}

	return "", nil
}

// ---------------------------------------------------------------------------
// Project Tab Session Wails Bindings
// ---------------------------------------------------------------------------

// CreateProjectTabSession creates a new project tab session entry in the index
// and returns an initial context message summarizing the project state.
// If a session already exists for this tabID, it loads and returns its context.
// This is a Wails binding method called from the frontend when a Project Tab is created.
func (a *App) CreateProjectTabSession(tabID, projectPath string) string {
	tabID = strings.TrimSpace(tabID)
	rawProjectPath := strings.TrimSpace(projectPath)
	projectPath = normalizeProjectSessionPath(projectPath)
	if tabID == "" || projectPath == "" {
		return ""
	}
	if rawProjectPath != projectPath {
		log.Printf("[CreateProjectTabSession] normalized project path tab=%q raw=%q normalized=%q", tabID, rawProjectPath, projectPath)
	}
	if a.isProjectTaskClosed(projectPath) {
		log.Printf("[CreateProjectTabSession] skipped closed task tab=%q project=%q", tabID, projectPath)
		return ""
	}

	persist := a.ensureProjectTabSessionPersist()

	// Cache the tabID → projectPath mapping in memory for fast lookup.
	a.tabProjectPaths.Store(tabID, projectPath)

	projectName := a.projectTabDisplayName(projectPath)
	a.ensureRecentTaskWorkspace(projectPath, projectName)

	// Check if session already exists on disk — if so, just return a welcome-back message.
	existing, err := persist.LoadSession(tabID)
	if err == nil && existing != nil {
		a.upsertProjectTabIndexEntry(persist, tabID, projectPath, time.Now())
		return fmt.Sprintf("📂 已恢复项目会话：%s", projectName)
	}

	// Create new session entry in the index.
	now := time.Now()
	index, err := persist.LoadIndex()
	if err != nil {
		log.Printf("[CreateProjectTabSession] LoadIndex failed: %v", err)
		index = &TabIndex{Tabs: []TabIndexEntry{}}
	}

	// Dedup: don't add if tabID already in index. Un-archive if previously closed.
	found := false
	for i, entry := range index.Tabs {
		if entry.ID == tabID {
			index.Tabs[i].Title = projectName
			index.Tabs[i].ProjectPath = projectPath
			index.Tabs[i].LastActiveAt = now.Unix()
			index.Tabs[i].Archived = false // Un-archive: tab is being re-opened
			found = true
			break
		}
	}
	if !found {
		index.Tabs = append(index.Tabs, TabIndexEntry{
			ID:           tabID,
			Type:         "project",
			Title:        projectName,
			ProjectPath:  projectPath,
			LastActiveAt: now.Unix(),
			Archived:     false,
		})
	}

	if err := persist.SaveIndex(index); err != nil {
		log.Printf("[CreateProjectTabSession] SaveIndex failed: %v", err)
	}

	// Save an initial empty session file.
	session := &TabSessionData{
		TabID:        tabID,
		ProjectPath:  projectPath,
		Conversation: []interface{}{},
		ScrollTop:    0,
		InputText:    "",
		CreatedAt:    now.UTC().Format(time.RFC3339),
		LastActiveAt: now.UTC().Format(time.RFC3339),
	}
	if err := persist.SaveSession(session); err != nil {
		log.Printf("[CreateProjectTabSession] SaveSession failed: %v", err)
	}

	// Build initial context message from long-term memory.
	contextMsg := a.buildProjectTabContextMessage(projectPath)
	if contextMsg != "" {
		return contextMsg
	}

	return fmt.Sprintf("📂 已打开项目：%s\n📁 %s\n\n请问需要我做什么？", projectName, projectPath)
}

func (a *App) projectTabDisplayName(projectPath string) string {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return "Task"
	}
	a.ensureMemoryStore()
	if a.memoryStore != nil {
		if pi := a.memoryStore.ProjectIndex(); pi != nil {
			if name := strings.TrimSpace(pi.GetDisplayName(projectPath)); name != "" {
				return normalizeRecentTaskName(name)
			}
		}
	}
	if name := normalizeRecentTaskName(lastPathComponent(projectPath)); name != "" {
		return name
	}
	return "Task"
}

func (a *App) upsertProjectTabIndexEntry(persist *ProjectTabSessionPersist, tabID, projectPath string, now time.Time) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if persist == nil || strings.TrimSpace(tabID) == "" || projectPath == "" {
		return
	}
	index, err := persist.LoadIndex()
	if err != nil {
		log.Printf("[CreateProjectTabSession] LoadIndex failed: %v", err)
		index = &TabIndex{Tabs: []TabIndexEntry{}}
	}
	projectName := a.projectTabDisplayName(projectPath)
	for i, entry := range index.Tabs {
		if entry.ID == tabID {
			index.Tabs[i].Title = projectName
			index.Tabs[i].ProjectPath = projectPath
			index.Tabs[i].LastActiveAt = now.Unix()
			index.Tabs[i].Archived = false
			if err := persist.SaveIndex(index); err != nil {
				log.Printf("[CreateProjectTabSession] SaveIndex failed: %v", err)
			}
			return
		}
	}
	index.Tabs = append(index.Tabs, TabIndexEntry{
		ID:           tabID,
		Type:         "project",
		Title:        projectName,
		ProjectPath:  projectPath,
		LastActiveAt: now.Unix(),
		Archived:     false,
	})
	if err := persist.SaveIndex(index); err != nil {
		log.Printf("[CreateProjectTabSession] SaveIndex failed: %v", err)
	}
}

// buildProjectTabContextMessage recalls project-related entries from memory
// using strict project filtering (RecallDynamicStrict) and formats them into
// an initial context message for the project tab.
//
// The summary includes: project name, recent progress, key artifact paths.
// Uses strictProject=true to ensure only entries tagged with the current
// projectPath are returned — other projects' knowledge is excluded.
func (a *App) buildProjectTabContextMessage(projectPath string) string {
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return ""
	}

	projectName := lastPathComponent(projectPath)

	contextData := a.memoryStore.ProjectContextForHost(projectPath, 1)
	artifacts := contextData.TaskArtifacts
	knowledge := contextData.ProjectKnowledge

	// Merge and deduplicate by ID.
	seen := make(map[string]bool, len(artifacts)+len(knowledge))
	var entries []memory.Entry
	for _, e := range artifacts {
		if !seen[e.ID] {
			seen[e.ID] = true
			entries = append(entries, e)
		}
	}
	for _, e := range knowledge {
		if !seen[e.ID] {
			seen[e.ID] = true
			entries = append(entries, e)
		}
	}

	if len(entries) == 0 {
		return ""
	}

	scene := contextData.Scene

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📂 项目：%s\n📁 路径：%s\n\n", projectName, projectPath))

	// --- Section: Recent Progress (from task_artifact entries) ---
	if len(artifacts) > 0 {
		sb.WriteString("**最近进展：**\n")
		limit := 5
		if len(artifacts) < limit {
			limit = len(artifacts)
		}
		for i := 0; i < limit; i++ {
			e := artifacts[i]
			label := e.Title
			if label == "" {
				label = projectTabTruncateContent(e.Content, 150)
			}
			sb.WriteString(fmt.Sprintf("- %s\n", label))
		}
		sb.WriteString("\n")
	}

	// --- Section: Key Artifact Paths ---
	artifactPaths := projectTabExtractPaths(entries)
	artifactPaths = projectTabAppendUniquePaths(artifactPaths, scene.SourceURLs)
	if len(artifactPaths) > 0 {
		sb.WriteString("**关键产出物：**\n")
		limit := 8
		if len(artifactPaths) < limit {
			limit = len(artifactPaths)
		}
		for i := 0; i < limit; i++ {
			sb.WriteString(fmt.Sprintf("- `%s`\n", artifactPaths[i]))
		}
		sb.WriteString("\n")
	}

	// --- Section: Recent Artifact Sources ---
	if len(scene.RecentArtifacts) > 0 {
		sb.WriteString("**最近产物来源：**\n")
		limit := 5
		if len(scene.RecentArtifacts) < limit {
			limit = len(scene.RecentArtifacts)
		}
		for i := 0; i < limit; i++ {
			artifact := scene.RecentArtifacts[i]
			label := artifact.Title
			if label == "" {
				label = artifact.Preview
			}
			if label == "" {
				label = artifact.SourceURL
			}
			if artifact.SourceURL != "" {
				sb.WriteString(fmt.Sprintf("- %s (%s)\n", label, projectTabSourceRefHint(artifact.SourceURL)))
			} else {
				sb.WriteString(fmt.Sprintf("- %s\n", label))
			}
		}
		sb.WriteString("\n")
	}

	// --- Section: Project Knowledge ---
	if len(knowledge) > 0 {
		sb.WriteString("**项目知识：**\n")
		limit := 3
		if len(knowledge) < limit {
			limit = len(knowledge)
		}
		for i := 0; i < limit; i++ {
			e := knowledge[i]
			label := e.Title
			if label == "" {
				label = projectTabTruncateContent(e.Content, 150)
			}
			sb.WriteString(fmt.Sprintf("- %s\n", label))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("请问需要我继续做什么？")
	return sb.String()
}

// projectTabTruncateContent truncates content to maxRunes, replacing newlines
// with spaces and appending "..." if truncated.
func projectTabTruncateContent(s string, maxRunes int) string {
	// Replace newlines with spaces for single-line display.
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// projectTabExtractPaths extracts file paths from entry SourceURL fields and tags.
func projectTabExtractPaths(entries []memory.Entry) []string {
	seen := make(map[string]bool)
	var paths []string

	for _, e := range entries {
		// Check SourceURL first (most reliable source of artifact paths).
		if e.SourceURL != "" && looksLikeFilePathForContext(e.SourceURL) {
			if !seen[e.SourceURL] {
				seen[e.SourceURL] = true
				paths = append(paths, e.SourceURL)
			}
		}
		// Check tags for file paths (excluding the project path itself).
		for _, tag := range e.Tags {
			if looksLikeFilePathForContext(tag) && !seen[tag] {
				// Skip tags that are just the project root path.
				if strings.Contains(tag, ".") || strings.Count(tag, string([]rune{filepath.Separator})) > 3 {
					seen[tag] = true
					paths = append(paths, tag)
				}
			}
		}
	}

	return paths
}

func projectTabSourceHint(sourceURL string) string {
	if sourceURL != "" && memory.LooksLikeFilePath(sourceURL) {
		return "full: read_file"
	}
	return ""
}

func projectTabSourceRefHint(sourceURL string) string {
	if sourceURL == "" {
		return ""
	}
	hint := projectTabSourceHint(sourceURL)
	if hint != "" {
		return fmt.Sprintf("`%s`; %s", sourceURL, hint)
	}
	return fmt.Sprintf("`%s`", sourceURL)
}

func projectTabAppendUniquePaths(paths []string, candidates []string) []string {
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		seen[path] = true
	}
	for _, candidate := range candidates {
		if candidate == "" || !looksLikeFilePathForContext(candidate) || seen[candidate] {
			continue
		}
		seen[candidate] = true
		paths = append(paths, candidate)
	}
	return paths
}

// CloseProjectTabSession persists the current session state for a project tab
// and updates the index. Called when the user closes a Project Tab.
// Marks the tab as archived so it won't be restored on next startup.
// This is a Wails binding method.
func (a *App) CloseProjectTabSession(tabID string) {
	tabID = strings.TrimSpace(tabID)
	if tabID == "" {
		return
	}

	persist := a.ensureProjectTabSessionPersist()
	var projectPath string
	if cached, ok := a.tabProjectPaths.Load(tabID); ok {
		projectPath, _ = cached.(string)
	}

	// Mark the tab as archived in the index so it won't be restored on startup.
	index, err := persist.LoadIndex()
	if err != nil {
		log.Printf("[CloseProjectTabSession] LoadIndex failed: %v", err)
	} else if index != nil {
		found := false
		for i, entry := range index.Tabs {
			if entry.ID == tabID {
				found = true
				if strings.TrimSpace(projectPath) == "" {
					projectPath = entry.ProjectPath
				}
				index.Tabs[i].LastActiveAt = time.Now().Unix()
				index.Tabs[i].Archived = true
				break
			}
		}

		if err := persist.SaveIndex(index); err != nil {
			log.Printf("[CloseProjectTabSession] SaveIndex failed: %v", err)
		}
		log.Printf("[CloseProjectTabSession] tab=%s archived found=%v project=%q", tabID, found, projectPath)
	}

	if strings.TrimSpace(projectPath) == "" {
		if session, err := persist.LoadSession(tabID); err == nil && session != nil {
			projectPath = session.ProjectPath
		} else if err != nil {
			log.Printf("[CloseProjectTabSession] LoadSession failed tab=%s err=%v", tabID, err)
		}
	}

	a.tabProjectPaths.Delete(tabID)
	if strings.TrimSpace(projectPath) != "" {
		a.cancelProjectTaskLoop(projectPath)
	}
	log.Printf("[CloseProjectTabSession] tab=%s closed project=%q", tabID, projectPath)
}

// SendMessageForTab routes a message to the project-specific session identified
// by tabID. It delegates to the existing SendAIAssistantMessage with the
// project_path from the tab's session, enabling per-project isolation.
//
// projectPathHint is an optional fallback: if the tab session hasn't been
// registered yet (race between CreateProjectTabSession and this call), the
// hint allows self-healing — the mapping is established on-the-fly and cached
// for future calls.
//
// This is a Wails binding method.
func (a *App) SendMessageForTab(tabID, text, projectPathHint string) (*IMAgentResponse, error) {
	tabID = strings.TrimSpace(tabID)
	if tabID == "" {
		return nil, fmt.Errorf("tabID is required")
	}
	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return nil, fmt.Errorf("message text is required")
	}
	rawProjectPathHint := strings.TrimSpace(projectPathHint)
	projectPathHint = normalizeProjectSessionPath(projectPathHint)
	if rawProjectPathHint != "" && rawProjectPathHint != projectPathHint {
		log.Printf("[SendMessageForTab] normalized hint tab=%q raw=%q normalized=%q", tabID, rawProjectPathHint, projectPathHint)
	}
	log.Printf("[SendMessageForTab] send requested tab=%q hint=%q text_len=%d", tabID, projectPathHint, len(trimmedText))

	// Look up the project path — first from in-memory cache, then fall back to disk.
	var projectPath string
	if cached, ok := a.tabProjectPaths.Load(tabID); ok {
		projectPath = normalizeProjectSessionPath(cached.(string))
	}

	if projectPath == "" {
		persist := a.ensureProjectTabSessionPersist()
		session, err := persist.LoadSession(tabID)
		if err == nil && session != nil {
			projectPath = normalizeProjectSessionPath(session.ProjectPath)
		}

		if projectPath == "" {
			// Fallback: look up from index.
			index, err := persist.LoadIndex()
			if err == nil && index != nil {
				for _, entry := range index.Tabs {
					if entry.ID == tabID {
						projectPath = normalizeProjectSessionPath(entry.ProjectPath)
						break
					}
				}
			}
		}

		// Populate cache for future calls.
		if projectPath != "" {
			a.tabProjectPaths.Store(tabID, projectPath)
		}
	}

	// Self-healing: if all lookups failed but the frontend provided a hint,
	// use it and register the mapping so future calls succeed without the hint.
	if projectPath == "" && projectPathHint != "" {
		projectPath = projectPathHint
		a.tabProjectPaths.Store(tabID, projectPath)
		log.Printf("[SendMessageForTab] self-healed tab %s → %s (from hint)", tabID, projectPath)
	}

	if projectPath == "" {
		log.Printf("[SendMessageForTab] send rejected tab=%q reason=no_project_path", tabID)
		return nil, fmt.Errorf("no project path found for tab %s", tabID)
	}
	if a.isProjectTaskClosed(projectPath) {
		log.Printf("[SendMessageForTab] send rejected tab=%q project=%q reason=closed_task", tabID, projectPath)
		a.cancelProjectTaskLoop(projectPath)
		return nil, fmt.Errorf("project task is closed: %s", projectPath)
	}
	log.Printf("[SendMessageForTab] route tab=%q project=%q text_len=%d", tabID, projectPath, len(trimmedText))

	// Delegate to the existing SendAIAssistantMessage with project_path set.
	// This auto-synthesizes per-project userID (desktop-user:{projectPath})
	// and all downstream components isolate by userID.
	return a.SendAIAssistantMessage(AIAssistantSendRequest{
		Text:        text,
		ProjectPath: projectPath,
	})
}

func (a *App) isProjectTaskClosed(projectPath string) bool {
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return false
	}
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return false
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return false
	}
	if rec := pi.Get(projectPath); rec != nil && strings.TrimSpace(rec.ProjectPath) != "" {
		projectPath = rec.ProjectPath
	}
	return pi.IsHidden(projectPath) || pi.IsArchived(projectPath)
}

// maxRestoredTabs is the maximum number of project tabs restored on startup.
// Only the most recently active tabs are returned to prevent tab bar overflow.
const maxRestoredTabs = 5

// LoadProjectTabIndex returns the saved tab index for frontend restoration
// on app startup. Returns at most maxRestoredTabs non-archived entries,
// sorted by LastActiveAt descending (most recent first).
// This is a Wails binding method.
func (a *App) LoadProjectTabIndex() []TabIndexEntry {
	persist := a.ensureProjectTabSessionPersist()
	index, err := persist.LoadIndex()
	if err != nil {
		log.Printf("[LoadProjectTabIndex] LoadIndex failed: %v", err)
		return []TabIndexEntry{}
	}
	if index == nil || len(index.Tabs) == 0 {
		return []TabIndexEntry{}
	}

	// Filter non-archived entries and sort by LastActiveAt descending.
	var active []TabIndexEntry
	var projectIndex *memory.ProjectIndex
	a.ensureMemoryStore()
	if a.memoryStore != nil {
		projectIndex = a.memoryStore.ProjectIndex()
	}
	for _, entry := range index.Tabs {
		if entry.Archived {
			continue
		}
		if entry.ProjectPath != "" {
			entry.Title = a.projectTabDisplayName(entry.ProjectPath)
		}
		if projectIndex != nil && entry.ProjectPath != "" {
			if projectIndex.IsHidden(entry.ProjectPath) || projectIndex.IsArchived(entry.ProjectPath) {
				log.Printf("[LoadProjectTabIndex] skip closed task tab=%q project=%q", entry.ID, entry.ProjectPath)
				continue
			}
		}
		active = append(active, entry)
	}
	if len(active) == 0 {
		return []TabIndexEntry{}
	}

	// Sort: most recently active first (standard library sort).
	sort.Slice(active, func(i, j int) bool {
		return active[i].LastActiveAt > active[j].LastActiveAt
	})

	// Return at most maxRestoredTabs.
	if len(active) > maxRestoredTabs {
		active = active[:maxRestoredTabs]
	}
	return active
}

// ---------------------------------------------------------------------------

// ensureProjectTabSessionPersist returns the shared ProjectTabSessionPersist
// instance from the App struct, lazily initializing it if nil.
func (a *App) ensureProjectTabSessionPersist() *ProjectTabSessionPersist {
	if a.projectTabSessionPersist == nil {
		a.projectTabSessionPersist = NewProjectTabSessionPersistForBaseDir(a.getMaclawBaseDir())
	}
	return a.projectTabSessionPersist
}

// deriveTaskName generates a human-readable task name from a ProjectRecord
// when extractTitle couldn't find a good name from entry content.
// Uses preview text + workflow type + directory name as fallback layers.
func deriveTaskName(rec memory.ProjectRecord) string {
	// Layer 1: use preview if it's meaningful (not just a path or metadata).
	if rec.Preview != "" && !memory.LooksLikeFilePath(rec.Preview) {
		// Truncate to a reasonable display length.
		name := rec.Preview
		if runes := []rune(name); len(runes) > 40 {
			name = string(runes[:40]) + "..."
		}
		return name
	}

	// Layer 2: workflow type + directory name (e.g. "编码: steave2").
	dir := lastPathComponent(rec.ProjectPath)
	if rec.WorkflowType != "" {
		label := workflowTypeLabel(rec.WorkflowType)
		if label != "" {
			return label + ": " + dir
		}
	}

	// Layer 3: bare directory name.
	return dir
}

// workflowTypeLabel returns a short Chinese label for a workflow type.
func workflowTypeLabel(wfType string) string {
	switch normalizeWorkflowType(wfType) {
	case workflow.WorkflowCoding:
		return "编码"
	case workflow.WorkflowProductDesign:
		return "产品设计"
	case workflow.WorkflowPresentationDesign:
		return "PPT 设计"
	case workflow.WorkflowInnovation:
		return "创新"
	case workflow.WorkflowBusinessPlan:
		return "商业计划"
	case workflow.WorkflowTesting:
		return "测试"
	case workflow.WorkflowLiteratureReview:
		return "文献综述"
	case workflow.WorkflowResearchReport:
		return "研究报告"
	case workflow.WorkflowExperimentDesign:
		return "实验设计"
	case workflow.WorkflowGrantProposal:
		return "基金申请"
	case workflow.WorkflowPaperWriting:
		return "论文写作"
	case workflow.WorkflowProjectProposal:
		return "项目提案"
	case workflow.WorkflowEventPlanning:
		return "活动策划"
	case workflow.WorkflowCompetitiveAnalysis:
		return "竞品分析"
	case workflow.WorkflowBidResponse:
		return "招投标"
	case workflow.WorkflowContractReview:
		return "合同审查"
	case workflow.WorkflowDueDiligence:
		return "尽职调查"
	case workflow.WorkflowComplianceAudit:
		return "合规审计"
	case workflow.WorkflowPatentAnalysis:
		return "专利分析"
	default:
		return wfType
	}
}
