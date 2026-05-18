package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ProjectSearchResult is the frontend-facing search result type.
// Exported as a Wails binding return type.
type ProjectSearchResult struct {
	ID              string                  `json:"id"`            // ProjectPath as stable ID
	Name            string                  `json:"name"`          // Human-readable project name
	ProjectPath     string                  `json:"project_path"`  // Canonical absolute path
	WorkflowType    string                  `json:"workflow_type"` // e.g. "coding", "product_design"
	Preview         string                  `json:"preview"`       // Short content preview (~150 chars)
	Tags            []string                `json:"tags"`          // Union of all entry tags
	LastActivity    string                  `json:"last_activity"` // RFC3339 formatted timestamp
	EntryCount      int                     `json:"entry_count"`   // Number of memory entries
	Pinned          bool                    `json:"pinned"`        // Whether the task is pinned to top
	Archived        bool                    `json:"archived"`      // Whether the task is archived
	SourceURLs      []string                `json:"source_urls,omitempty"`
	RecentArtifacts []ProjectSearchArtifact `json:"recent_artifacts,omitempty"`
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
		return &ProjectSceneDetail{ProjectPath: projectPath, Name: lastPathComponent(projectPath)}, nil
	}
	return projectSceneDetailFromRecord(scene), nil
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

	scenesByPath := projectSceneMap(a.memoryStore.SceneIndex(limit * 2))

	results := make([]ProjectSearchResult, 0, len(records))
	for _, rec := range records {
		result := projectRecordToSearchResult(pi, rec)
		enrichProjectSearchResultWithScene(&result, scenesByPath[rec.ProjectPath])
		results = append(results, result)
	}

	return results
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
	taskContent := fmt.Sprintf("# %s\n\nCreated from recent tasks.\nTask ID: %d", taskName, now.UnixNano())
	if err := os.WriteFile(taskFile, []byte(taskContent), 0o644); err != nil {
		log.Printf("[project_search] CreateRecentTask write task file failed: %v", err)
		return ProjectSearchResult{}
	}

	entry := memory.Entry{
		Title:      taskName,
		Content:    taskContent,
		Category:   memory.CategoryTaskArtifact,
		Tags:       []string{"manual_task", "recent_task"},
		SourceURL:  taskFile,
		SourceType: "manual",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := a.memoryStore.Save(entry); err != nil {
		log.Printf("[project_search] CreateRecentTask save failed: %v", err)
		return ProjectSearchResult{}
	}
	a.triggerMemoryPipelineSoon(45 * time.Second)
	if err := a.memoryStore.Flush(); err != nil {
		log.Printf("[project_search] CreateRecentTask flush failed: %v", err)
	}
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "project-index:changed", taskDir)
		runtime.EventsEmit(a.ctx, "tasks:changed", nil)
	}

	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return ProjectSearchResult{ID: taskDir, Name: taskName, ProjectPath: taskDir, LastActivity: now.Format(time.RFC3339), EntryCount: 1}
	}
	if rec := pi.Get(taskDir); rec != nil {
		return projectRecordToSearchResult(pi, *rec)
	}
	return ProjectSearchResult{ID: taskDir, Name: taskName, ProjectPath: taskDir, LastActivity: now.Format(time.RFC3339), EntryCount: 1}
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
	targetUserID := fmt.Sprintf("desktop-user:%s", projectPath)
	handler.memory.Save(targetUserID, sourceEntries)
	log.Printf("[project_search] ForkConversationToProject: copied %d entries from %s to %s",
		len(sourceEntries), desktopUserID, targetUserID)
}

func normalizeRecentTaskName(name string) string {
	normalized := strings.Join(strings.Fields(name), " ")
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
	// config.current_project stays in sync. Without this, a subsequent
	// frontend SaveConfig would overwrite the backend's correct
	// current_project with the stale value (same race pattern as #11/#23).
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
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return
	}
	pi.SetHidden(projectPath, true)
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
// This uses LoadConfig → merge → SaveConfig to avoid overwriting concurrent
// config changes (same pattern as #11 / #23 config race fix).
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
			cfg.CurrentProject = p.Id
			if err := a.SaveConfig(cfg); err != nil {
				log.Printf("[project_search] switchCurrentProjectByPath: SaveConfig failed: %v", err)
				return nil
			}
			log.Printf("[project_search] switchCurrentProjectByPath: switched to project %q (id=%s)", p.Name, p.Id)
			return &cfg
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
	if tabID == "" || projectPath == "" {
		return ""
	}

	persist := a.ensureProjectTabSessionPersist()

	// Cache the tabID → projectPath mapping in memory for fast lookup.
	a.tabProjectPaths.Store(tabID, projectPath)

	// Check if session already exists on disk — if so, just return a welcome-back message.
	existing, err := persist.LoadSession(tabID)
	if err == nil && existing != nil {
		return fmt.Sprintf("📂 已恢复项目会话：%s", lastPathComponent(projectPath))
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
			index.Tabs[i].LastActiveAt = now.Unix()
			index.Tabs[i].Archived = false // Un-archive: tab is being re-opened
			found = true
			break
		}
	}
	if !found {
		projectName := lastPathComponent(projectPath)
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

	return fmt.Sprintf("📂 已打开项目：%s\n📁 %s\n\n请问需要我做什么？", lastPathComponent(projectPath), projectPath)
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

	// Recall task_artifact entries for this project (strict project filter).
	artifacts := a.memoryStore.RecallDynamicStrict(
		"task artifact progress",
		memory.CategoryTaskArtifact,
		projectPath,
	)

	// Recall project_knowledge entries for this project (strict project filter).
	knowledge := a.memoryStore.RecallDynamicStrict(
		"project knowledge",
		memory.CategoryProjectKnowledge,
		projectPath,
	)

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

	scene := projectSceneMap(a.memoryStore.SceneIndex(20))[projectPath]

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
	if tabID == "" {
		return
	}

	persist := a.ensureProjectTabSessionPersist()

	// Mark the tab as archived in the index so it won't be restored on startup.
	index, err := persist.LoadIndex()
	if err != nil {
		log.Printf("[CloseProjectTabSession] LoadIndex failed: %v", err)
		return
	}

	for i, entry := range index.Tabs {
		if entry.ID == tabID {
			index.Tabs[i].LastActiveAt = time.Now().Unix()
			index.Tabs[i].Archived = true
			break
		}
	}

	if err := persist.SaveIndex(index); err != nil {
		log.Printf("[CloseProjectTabSession] SaveIndex failed: %v", err)
	}

	log.Printf("[CloseProjectTabSession] tab=%s archived", tabID)
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
	if tabID == "" {
		return nil, fmt.Errorf("tabID is required")
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("message text is required")
	}

	// Look up the project path — first from in-memory cache, then fall back to disk.
	var projectPath string
	if cached, ok := a.tabProjectPaths.Load(tabID); ok {
		projectPath = cached.(string)
	}

	if projectPath == "" {
		persist := a.ensureProjectTabSessionPersist()
		session, err := persist.LoadSession(tabID)
		if err == nil && session != nil {
			projectPath = session.ProjectPath
		}

		if projectPath == "" {
			// Fallback: look up from index.
			index, err := persist.LoadIndex()
			if err == nil && index != nil {
				for _, entry := range index.Tabs {
					if entry.ID == tabID {
						projectPath = entry.ProjectPath
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
	if projectPath == "" && strings.TrimSpace(projectPathHint) != "" {
		projectPath = strings.TrimSpace(projectPathHint)
		a.tabProjectPaths.Store(tabID, projectPath)
		log.Printf("[SendMessageForTab] self-healed tab %s → %s (from hint)", tabID, projectPath)
	}

	if projectPath == "" {
		return nil, fmt.Errorf("no project path found for tab %s", tabID)
	}

	// Delegate to the existing SendAIAssistantMessage with project_path set.
	// This auto-synthesizes per-project userID (desktop-user:{projectPath})
	// and all downstream components isolate by userID.
	return a.SendAIAssistantMessage(AIAssistantSendRequest{
		Text:        text,
		ProjectPath: projectPath,
	})
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
	for _, entry := range index.Tabs {
		if !entry.Archived {
			active = append(active, entry)
		}
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
		a.projectTabSessionPersist = NewProjectTabSessionPersist()
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
