package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

var recentTaskForkMu sync.Mutex

const (
	taskManagementTag        = "task_management"
	taskUserCreatedTag       = "task_user_created"
	taskUserSavedTag         = "task_user_saved"
	taskForkedTag            = "forked_task"
	taskLegacyManualTag      = "manual_task"
	taskLegacyRecentTag      = "recent_task"
	taskSourceTagPrefix      = "source:"
	taskSavedConversationTag = "saved_conversation"
	// taskCodingDevTag marks tasks created with the "programming / coding" option.
	// Used by the GUI to open the task in coding-agent mode.
	taskCodingDevTag = "coding_dev"
	// taskRemoteCodingDevTag marks pure remote (SSH) coding environments.
	taskRemoteCodingDevTag     = "remote_coding_dev"
	taskRemoteHostTagPrefix    = "remote_host:"
	taskRemoteUserTagPrefix    = "remote_user:"
	taskRemotePortTagPrefix    = "remote_port:"
	taskRemoteWorkDirTagPrefix = "remote_workdir:"
	// taskSourceCodingWorkflowTag marks remote/local tasks created from the multi-phase coding workflow form.
	taskSourceCodingWorkflowTag = taskSourceTagPrefix + "coding_workflow"
)

// NormalizeCreateTaskMode maps free-form mode strings from the UI/API to a
// canonical create-task mode. Empty / unknown values mean ordinary chat task.
func NormalizeCreateTaskMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "coding_dev", "coding", "programming", "code":
		return taskCodingDevTag
	case "remote_coding_dev", "remote_coding", "remote_programming", "remote_code":
		return taskRemoteCodingDevTag
	default:
		return ""
	}
}

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
	rest := strings.TrimPrefix(ownerID, desktopUserID+":")
	// Expert sessions ("desktop-user:expert:<id>") carry an expert id, not a
	// project path — never let downstream code treat it as a directory.
	if strings.HasPrefix(rest, "expert:") {
		return ""
	}
	return normalizeProjectSessionPath(rest)
}

// ProjectSearchResult is the frontend-facing search result type.
// Exported as a Wails binding return type.
type ProjectSearchResult struct {
	ID              string                  `json:"id"`           // ProjectPath as stable ID
	Name            string                  `json:"name"`         // Human-readable project name
	ProjectPath     string                  `json:"project_path"` // Canonical absolute path
	WorkingDir      string                  `json:"working_dir,omitempty"`
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

type ProjectConversationHistoryItem struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// ProjectWorkflowState is a compact workflow snapshot attached to a
// task-management artifact. Active snapshots can be continued with ProjectPath;
// terminal snapshots preserve the outcome of recent work.
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

	scene, ok := a.sceneRecordForProjectPath(projectPath, projectSceneMap(a.memoryStore.SceneIndex(100)))
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
		if scene, ok := a.sceneRecordForProjectPath(rec.ProjectPath, scenesByPath); ok {
			enrichProjectSearchResultWithScene(&result, scene)
		}
		result.ActiveWorkflow = a.activeWorkflowForProject(projectWorkflowProjectPathForRecord(rec))
		results = append(results, result)
	}

	return results
}

// ListTasks returns the user-visible task management list. Unlike SearchProjects,
// this intentionally excludes automatic project/memory sediment and fork rows.
func (a *App) ListTasks(limit int) []ProjectSearchResult {
	return a.SearchTasks("", limit)
}

// SearchTasks searches only explicit task-management records.
func (a *App) SearchTasks(query string, limit int) []ProjectSearchResult {
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return nil
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return nil
	}
	if limit <= 0 {
		limit = 50
	}
	searchLimit := limit * 4
	if searchLimit < 50 {
		searchLimit = 50
	}
	var records []memory.ProjectRecord
	if strings.TrimSpace(query) == "" {
		// Filter before limiting so recent automatic memory entries cannot crowd
		// explicit tasks out of the sidebar while keeping the list work bounded.
		records = pi.ListRecentMatching(limit, isTaskManagementRecord)
	} else {
		records = pi.SearchMatching(query, limit, isTaskManagementRecord)
	}
	scenesByPath := projectSceneMap(a.memoryStore.SceneIndex(searchLimit * 2))
	results := make([]ProjectSearchResult, 0, len(records))
	for _, rec := range records {
		if !isTaskManagementRecord(rec) {
			continue
		}
		if pi.IsHidden(rec.ProjectPath) || pi.IsArchived(rec.ProjectPath) {
			continue
		}
		result := projectRecordToSearchResult(pi, rec)
		if scene, ok := a.sceneRecordForProjectPath(rec.ProjectPath, scenesByPath); ok {
			enrichProjectSearchResultWithScene(&result, scene)
		}
		result.ActiveWorkflow = a.activeWorkflowForProject(projectWorkflowProjectPathForRecord(rec))
		results = append(results, result)
		if len(results) >= limit {
			break
		}
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
	if !projectRecordHasTag(rec, taskForkedTag) {
		return normalizeRecentTaskPathKey(rec.ProjectPath)
	}
	for _, tag := range rec.Tags {
		if strings.HasPrefix(tag, taskSourceTagPrefix) {
			return normalizeRecentTaskPathKey(strings.TrimPrefix(tag, taskSourceTagPrefix))
		}
	}
	return normalizeRecentTaskPathKey(rec.ProjectPath)
}

func preferRecentTaskRecord(candidate, current memory.ProjectRecord) bool {
	candidateFork := projectRecordHasTag(candidate, taskForkedTag)
	currentFork := projectRecordHasTag(current, taskForkedTag)
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
		WorkingDir:   recentTaskWorkingDirFromTags(rec.Tags),
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

func mergeProjectSceneRecords(targetPath string, primary, secondary memory.SceneRecord) memory.SceneRecord {
	merged := primary
	merged.ProjectPath = targetPath
	if strings.TrimSpace(merged.Name) == "" {
		merged.Name = secondary.Name
	}
	if merged.EntryCount < secondary.EntryCount {
		merged.EntryCount = secondary.EntryCount
	}
	if merged.LastActivity.Before(secondary.LastActivity) {
		merged.LastActivity = secondary.LastActivity
	}
	if strings.TrimSpace(merged.Preview) == "" {
		merged.Preview = secondary.Preview
	}

	appendUniqueStrings := func(dst []string, src []string) []string {
		seen := make(map[string]struct{}, len(dst))
		for _, item := range dst {
			seen[item] = struct{}{}
		}
		for _, item := range src {
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			dst = append(dst, item)
		}
		return dst
	}
	merged.WorkflowTypes = appendUniqueStrings(append([]string(nil), merged.WorkflowTypes...), secondary.WorkflowTypes)
	merged.Tags = appendUniqueStrings(append([]string(nil), merged.Tags...), secondary.Tags)
	merged.SourceURLs = appendUniqueStrings(append([]string(nil), merged.SourceURLs...), secondary.SourceURLs)

	seenArtifacts := make(map[string]struct{}, len(merged.RecentArtifacts))
	artifactKey := func(artifact memory.SceneArtifact) string {
		return strings.TrimSpace(artifact.SourceURL) + "\x00" + strings.TrimSpace(artifact.Title)
	}
	artifacts := append([]memory.SceneArtifact(nil), merged.RecentArtifacts...)
	for _, artifact := range artifacts {
		seenArtifacts[artifactKey(artifact)] = struct{}{}
	}
	for _, artifact := range secondary.RecentArtifacts {
		key := artifactKey(artifact)
		if _, ok := seenArtifacts[key]; ok {
			continue
		}
		seenArtifacts[key] = struct{}{}
		artifacts = append(artifacts, artifact)
	}
	sort.SliceStable(artifacts, func(i, j int) bool {
		return artifacts[i].UpdatedAt.After(artifacts[j].UpdatedAt)
	})
	if len(artifacts) > 5 {
		artifacts = artifacts[:5]
	}
	merged.RecentArtifacts = artifacts
	return merged
}

func (a *App) sceneRecordForProjectPath(projectPath string, scenesByPath map[string]memory.SceneRecord) (memory.SceneRecord, bool) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" || len(scenesByPath) == 0 {
		return memory.SceneRecord{}, false
	}
	taskScene, hasTaskScene := scenesByPath[projectPath]
	executionProjectPath := a.recentTaskExecutionProjectPath(projectPath)
	if executionProjectPath == "" || executionProjectPath == projectPath {
		if hasTaskScene {
			return taskScene, true
		}
		return memory.SceneRecord{}, false
	}
	executionScene, hasExecutionScene := scenesByPath[executionProjectPath]
	switch {
	case hasExecutionScene && hasTaskScene:
		return mergeProjectSceneRecords(projectPath, executionScene, taskScene), true
	case hasExecutionScene:
		executionScene.ProjectPath = projectPath
		return executionScene, true
	case hasTaskScene:
		taskScene.ProjectPath = projectPath
		return taskScene, true
	default:
		return memory.SceneRecord{}, false
	}
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
// in task management immediately and after restart.
func (a *App) CreateRecentTask(name string) ProjectSearchResult {
	return a.CreateRecentTaskWithWorkingDir(name, "")
}

// CreateTask creates a user-visible task-management record.
func (a *App) CreateTask(name, workingDir string) ProjectSearchResult {
	return a.CreateTaskWithMode(name, workingDir, "")
}

// CreateTaskWithMode creates a user-visible task-management record with an
// optional execution mode. mode="coding_dev" / "remote_coding_dev" (or aliases)
// tags the task for coding-agent routing when the GUI reopens it.
func (a *App) CreateTaskWithMode(name, workingDir, mode string) ProjectSearchResult {
	taskName := normalizeRecentTaskName(name)
	if taskName == "" {
		return ProjectSearchResult{}
	}
	tags := []string{taskManagementTag, taskUserCreatedTag}
	if normalized := NormalizeCreateTaskMode(mode); normalized != "" {
		tags = append(tags, normalized)
	}
	return a.createTaskRecordWithWorkingDir(taskName, "", tags, normalizeRecentTaskWorkingDir(workingDir), false)
}

// sanitizeTaskMetadataTagValue strips characters that break multi-line or
// control-bearing tag values. Colons are kept: remote_* tags are parsed via
// fixed prefixes (TrimPrefix), so IPv6 hosts and Windows-style paths remain valid.
func sanitizeTaskMetadataTagValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.Map(func(r rune) rune {
		if r < 32 {
			return -1
		}
		return r
	}, value)
	return strings.TrimSpace(value)
}

// normalizeSSHHostInput trims/sanitizes host and unwraps [IPv6] bracket form
// so tags and SSH connect share the same host string.
func normalizeSSHHostInput(host string) string {
	host = sanitizeTaskMetadataTagValue(host)
	if len(host) >= 2 && host[0] == '[' && host[len(host)-1] == ']' {
		return strings.TrimSpace(host[1 : len(host)-1])
	}
	return host
}

// looksLikeAbsoluteProjectPathTag reports local absolute paths used as task-dir
// identity tags (not remote_host:/remote_workdir: meta).
func looksLikeAbsoluteProjectPathTag(tag string) bool {
	tag = strings.TrimSpace(tag)
	if tag == "" || isRemoteCodingMetaTag(tag) {
		return false
	}
	if strings.HasPrefix(tag, "/") {
		return true
	}
	// Windows drive path: D:\… or D:/…
	if len(tag) >= 3 && tag[1] == ':' && (tag[2] == '\\' || tag[2] == '/') {
		return true
	}
	return false
}

// shouldKeepTaskTagOnRemoteMetaUpdate limits which project-index tags are copied
// onto the task artifact during SSH meta edit. Index tags are a union across
// entries; copying everything would pollute the task row with sediment/output tags.
func shouldKeepTaskTagOnRemoteMetaUpdate(tag string) bool {
	tag = strings.TrimSpace(tag)
	if tag == "" || isRemoteCodingMetaTag(tag) {
		return false
	}
	switch tag {
	case taskLegacyManualTag, taskLegacyRecentTag,
		taskManagementTag, taskUserCreatedTag, taskUserSavedTag,
		taskRemoteCodingDevTag, taskCodingDevTag, taskSavedConversationTag:
		return true
	}
	if strings.HasPrefix(tag, recentTaskWorkingDirTagPrefix) ||
		strings.HasPrefix(tag, taskSourceTagPrefix) {
		return true
	}
	return looksLikeAbsoluteProjectPathTag(tag)
}

// RemoteCodingTaskMeta is non-sensitive SSH metadata for a remote pure-coding task.
// Password is never stored or returned.
type RemoteCodingTaskMeta struct {
	Host    string `json:"host"`
	User    string `json:"user"`
	Port    int    `json:"port"`
	WorkDir string `json:"work_dir"`
}

// GetRemoteCodingTaskMeta returns host/user/port/workdir tags for a remote coding task.
func (a *App) GetRemoteCodingTaskMeta(projectPath string) (RemoteCodingTaskMeta, error) {
	meta := RemoteCodingTaskMeta{Port: 22}
	if a == nil {
		return meta, fmt.Errorf("app unavailable")
	}
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return meta, fmt.Errorf("project path is required")
	}
	a.ensureMemoryStore()
	host, user, workDir, port := a.remoteCodingMetaFromTaskTags(projectPath)
	// Sticky fallback only when index tags are entirely missing (e.g. race before
	// index rebuild). Use existing hub only — do not cold-init for a read.
	if host == "" && user == "" && workDir == "" {
		if hub := a.hubClient(); hub != nil {
			if handler := hub.ensureIMHandler(); handler != nil {
				mem := handler.getStickyCodingWorkbenchMemory(projectSessionOwnerID(projectPath))
				host = strings.TrimSpace(mem.RemoteHost)
				user = strings.TrimSpace(mem.RemoteUser)
				workDir = strings.TrimSpace(mem.RemoteWorkDir)
				if workDir == "" {
					workDir = strings.TrimSpace(mem.RemoteProjectDir)
				}
				if port <= 0 && mem.RemotePort > 0 {
					port = mem.RemotePort
				}
			}
		}
	}
	if port <= 0 {
		port = 22
	}
	meta.Host = host
	meta.User = user
	meta.Port = port
	meta.WorkDir = workDir
	return meta, nil
}

// remoteCodingMetaTagPrefixes are SSH metadata tags that must be replaced (not
// unioned) when updating a remote coding task.
var remoteCodingMetaTagPrefixes = []string{
	taskRemoteHostTagPrefix,
	taskRemoteUserTagPrefix,
	taskRemotePortTagPrefix,
	taskRemoteWorkDirTagPrefix,
}

func isRemoteCodingMetaTag(tag string) bool {
	for _, p := range remoteCodingMetaTagPrefixes {
		if p != "" && strings.HasPrefix(tag, p) {
			return true
		}
	}
	return false
}

func buildRemoteCodingMetaTags(host, user, workDir string, port int) []string {
	if port <= 0 || port >= 65536 {
		port = 22
	}
	return []string{
		taskRemoteHostTagPrefix + host,
		taskRemoteUserTagPrefix + user,
		fmt.Sprintf("%s%d", taskRemotePortTagPrefix, port),
		taskRemoteWorkDirTagPrefix + workDir,
	}
}

// taskArtifactIdentityPath picks the exact path tag form stored on the record
// so Upsert identity tags match the original createTaskRecord tags even when
// callers pass a slash-normalized project path.
func taskArtifactIdentityPath(projectPath string, tags []string) string {
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return projectPath
	}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if !looksLikeAbsoluteProjectPathTag(tag) {
			continue
		}
		if normalizeProjectSessionPath(tag) == projectPath {
			return tag
		}
	}
	return projectPath
}

// mergeTagsReplacePrefixed returns desired tags first, then any existing tags
// that are not already present and do not match dropPrefixes. Used so remote
// meta keys (remote_host:/remote_workdir:…) are replaced instead of unioned —
// default mergeTags would keep both and alphabetical order can surface stale values.
func mergeTagsReplacePrefixed(existing, desired []string, dropPrefixes []string) []string {
	isDropped := func(tag string) bool {
		for _, p := range dropPrefixes {
			if p != "" && strings.HasPrefix(tag, p) {
				return true
			}
		}
		return false
	}
	out := make([]string, 0, len(desired)+len(existing))
	seen := make(map[string]struct{}, len(desired)+len(existing))
	for _, t := range desired {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	for _, t := range existing {
		t = strings.TrimSpace(t)
		if t == "" || isDropped(t) {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

// upsertRemoteWorkDirContentLine updates or appends "Remote work directory: …"
// without wiping the rest of task.md / artifact content.
func upsertRemoteWorkDirContentLine(content, workDir string) string {
	workDir = strings.TrimSpace(workDir)
	const prefix = "Remote work directory:"
	if content == "" {
		if workDir == "" {
			return ""
		}
		return prefix + " " + workDir + "\n"
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			lines[i] = prefix + " " + workDir
			return strings.Join(lines, "\n")
		}
	}
	trimmed := strings.TrimRight(content, "\n")
	return trimmed + "\n\n" + prefix + " " + workDir + "\n"
}

// UpdateRemoteCodingTaskMeta updates non-sensitive SSH metadata tags on a remote
// coding task. Password is never persisted; use TestRemoteSSHConnection / PrepareRemoteCodingEnvironment.
func (a *App) UpdateRemoteCodingTaskMeta(projectPath, sshHost, sshUser, workDir string, sshPort int) error {
	if a == nil {
		return fmt.Errorf("app unavailable")
	}
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return fmt.Errorf("project path is required")
	}
	host := normalizeSSHHostInput(sshHost)
	user := sanitizeTaskMetadataTagValue(sshUser)
	// Workdir keeps path separators and colons; strip control/newlines only.
	remoteWorkDir := sanitizeTaskMetadataTagValue(workDir)
	if host == "" || user == "" || remoteWorkDir == "" {
		return fmt.Errorf("主机、用户名和远程工作目录均为必填")
	}
	if sshPort <= 0 || sshPort >= 65536 {
		sshPort = 22
	}
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return fmt.Errorf("memory store unavailable")
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return fmt.Errorf("project index unavailable")
	}
	rec := pi.Get(projectPath)
	if rec == nil {
		return fmt.Errorf("task not found: %s", projectPath)
	}
	if !projectRecordHasTag(*rec, taskRemoteCodingDevTag) {
		return fmt.Errorf("not a remote coding task")
	}

	// Identity must match createTaskRecordWithWorkingDir: manual + recent + taskDir.
	// Prefer the exact path tag form already on the record (slash variants).
	identityPath := taskArtifactIdentityPath(projectPath, rec.Tags)
	identity := []string{taskLegacyManualTag, taskLegacyRecentTag, identityPath}
	newRemoteTags := buildRemoteCodingMetaTags(host, user, remoteWorkDir, sshPort)
	updatedTags := append([]string(nil), identity...)
	seen := map[string]struct{}{identity[0]: {}, identity[1]: {}, identity[2]: {}}
	for _, tag := range rec.Tags {
		tag = strings.TrimSpace(tag)
		if !shouldKeepTaskTagOnRemoteMetaUpdate(tag) {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		updatedTags = append(updatedTags, tag)
	}
	if _, ok := seen[taskRemoteCodingDevTag]; !ok {
		updatedTags = append(updatedTags, taskRemoteCodingDevTag)
		seen[taskRemoteCodingDevTag] = struct{}{}
	}
	for _, t := range newRemoteTags {
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		updatedTags = append(updatedTags, t)
	}

	// Preserve existing task.md body; only refresh the remote workdir line.
	taskFile := filepath.Join(projectPath, "task.md")
	content := ""
	if data, err := os.ReadFile(taskFile); err == nil {
		content = string(data)
	}
	if strings.TrimSpace(content) == "" {
		title := strings.TrimSpace(rec.Name)
		if title == "" {
			title = "remote coding task"
		}
		content = fmt.Sprintf("# %s\n\nCreated from task management.\n", title)
	}
	content = upsertRemoteWorkDirContentLine(content, remoteWorkDir)
	sourceURL := taskFile
	if _, err := os.Stat(sourceURL); err != nil {
		sourceURL = projectPath
	}
	title := strings.TrimSpace(rec.Name)
	if title == "" {
		title = "remote coding task"
	}
	_, err := a.memoryStore.UpsertTaskArtifact(memory.TaskArtifactUpsertOptions{
		Title:            title,
		Content:          content,
		Tags:             updatedTags,
		IdentityTagCount: 3,
		SourceURL:        sourceURL,
		SourceType:       "manual",
		MergeExistingTags: func(existing, desired []string) []string {
			return mergeTagsReplacePrefixed(existing, desired, remoteCodingMetaTagPrefixes)
		},
	})
	if err != nil {
		return err
	}
	// Persist task.md only after the memory index accepted the update, so a
	// failed Upsert cannot leave disk ahead of the task record.
	if err := os.WriteFile(taskFile, []byte(content), 0o644); err != nil {
		log.Printf("[project_search] UpdateRemoteCodingTaskMeta write task.md failed: %v", err)
	}
	flushErr := a.memoryStore.Flush()
	// ProjectIndex.IndexEntry only merge-appends tags; force-replace remote meta
	// even if flush failed so GetRemoteCodingTaskMeta / sidebar stay coherent.
	pi.ReplacePrefixedTags(projectPath, remoteCodingMetaTagPrefixes, newRemoteTags)
	a.emitProjectIndexChanged(projectPath)
	// Best-effort sticky mirror for open sessions — do not cold-init hub for a
	// metadata edit when no assistant session is loaded. Drop RemoteSessionID
	// when coordinates change so reconnect uses the new host/user/port/workdir.
	if hubClient := a.hubClient(); hubClient != nil {
		if handler := hubClient.ensureIMHandler(); handler != nil {
			userID := projectSessionOwnerID(projectPath)
			handler.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
				if mem.Kind != "" && mem.Kind != "remote" {
					return
				}
				// Invalidate live SSH session only when known sticky coords change.
				if sid := strings.TrimSpace(mem.RemoteSessionID); sid != "" {
					if (mem.RemoteHost != "" && mem.RemoteHost != host) ||
						(mem.RemoteUser != "" && mem.RemoteUser != user) ||
						(mem.RemotePort > 0 && mem.RemotePort != sshPort) ||
						(mem.RemoteWorkDir != "" && mem.RemoteWorkDir != remoteWorkDir) {
						mem.RemoteSessionID = ""
					}
				}
				mem.Kind = "remote"
				mem.RemoteHost = host
				mem.RemoteUser = user
				mem.RemotePort = sshPort
				mem.RemoteWorkDir = remoteWorkDir
				mem.RemoteProjectDir = remoteWorkDir
			})
		}
	}
	return flushErr
}

// TestRemoteSSHConnection verifies SSH credentials and that workDir exists on the host.
// Password is used only for this call and is not persisted.
// Returns a short success message or an error.
func (a *App) TestRemoteSSHConnection(sshHost, sshUser, sshPassword, workDir string, sshPort int) (string, error) {
	if a == nil {
		return "", fmt.Errorf("app unavailable")
	}
	host := normalizeSSHHostInput(sshHost)
	user := sanitizeTaskMetadataTagValue(sshUser)
	password := strings.TrimSpace(sshPassword)
	remoteWorkDir := sanitizeTaskMetadataTagValue(workDir)
	if host == "" || user == "" || password == "" {
		return "", fmt.Errorf("主机、用户名和密码均为必填")
	}
	if sshPort <= 0 || sshPort >= 65536 {
		sshPort = 22
	}
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return "", fmt.Errorf("AI assistant not initialized")
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil {
		return "", fmt.Errorf("message handler unavailable")
	}
	if handler.ensureSSHManager() == nil {
		return "", fmt.Errorf("SSH 会话管理器不可用")
	}
	sessionID, failReason := handler.findOrCreateSSHSessionWithAuth(user, host, sshPort, password, "", "")
	if sessionID == "" {
		return "", errors.New(sshConnectFailureMessage(user, host, sshPort, failReason))
	}
	// Optional workdir existence check (non-fatal for empty workDir).
	if remoteWorkDir != "" {
		cmd := fmt.Sprintf(
			`if [ -d %s ]; then echo __MACLAW_DIR_OK__; elif [ -e %s ]; then echo __MACLAW_NOT_DIR__; else echo __MACLAW_DIR_MISS__; fi`,
			remoteShellQuote(remoteWorkDir), remoteShellQuote(remoteWorkDir),
		)
		out := strings.TrimSpace(handler.sshExec(map[string]interface{}{
			"session_id":   sessionID,
			"command":      cmd,
			"wait_seconds": float64(15),
		}))
		if strings.Contains(out, "__MACLAW_DIR_MISS__") {
			return "", fmt.Errorf("SSH 已连通，但工作目录不存在: %s", remoteWorkDir)
		}
		if strings.Contains(out, "__MACLAW_NOT_DIR__") {
			return "", fmt.Errorf("SSH 已连通，但路径不是目录: %s", remoteWorkDir)
		}
		if !strings.Contains(out, "__MACLAW_DIR_OK__") {
			// Soft-warn: connection works but dir check inconclusive.
			return fmt.Sprintf("SSH 已连通 %s@%s:%d（工作目录检查未确认）", user, host, sshPort), nil
		}
	}
	return fmt.Sprintf("SSH 连接成功：%s@%s:%d", user, host, sshPort), nil
}

// normalizeRemoteWorkDirKey normalizes remote workdirs for equality checks.
func normalizeRemoteWorkDirKey(workDir string) string {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		return ""
	}
	// Remote paths are almost always POSIX; still trim both separators.
	workDir = strings.TrimRight(workDir, "/\\")
	return workDir
}

// FindRemoteCodingTaskByMeta returns the most recent remote_coding_dev task whose
// non-secret SSH tags match host+user+workdir. Port is ignored for matching so
// reconnects after port changes still merge. Empty result if none.
func (a *App) FindRemoteCodingTaskByMeta(sshHost, sshUser, workDir string) ProjectSearchResult {
	if a == nil {
		return ProjectSearchResult{}
	}
	host := normalizeSSHHostInput(sshHost)
	user := sanitizeTaskMetadataTagValue(sshUser)
	wantWD := normalizeRemoteWorkDirKey(workDir)
	if host == "" || user == "" || wantWD == "" {
		return ProjectSearchResult{}
	}
	a.ensureMemoryStore()
	// Keep this lookup window aligned with the ACP remote-task picker. Otherwise
	// a previously created remote task can fall outside this search window and a
	// retry from VS Code would create a duplicate record.
	tasks := a.ListTasks(1000)
	for _, task := range tasks {
		if !projectRecordHasTagLike(task.Tags, taskRemoteCodingDevTag) {
			continue
		}
		th, tu, tw, _ := a.remoteCodingMetaFromTaskTags(task.ProjectPath)
		if !strings.EqualFold(normalizeSSHHostInput(th), host) {
			continue
		}
		if !strings.EqualFold(sanitizeTaskMetadataTagValue(tu), user) {
			continue
		}
		if normalizeRemoteWorkDirKey(tw) != wantWD {
			continue
		}
		return task
	}
	return ProjectSearchResult{}
}

// projectRecordHasTagLike reports whether tags contain exact tag (trimmed).
func projectRecordHasTagLike(tags []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, tag := range tags {
		if strings.TrimSpace(tag) == want {
			return true
		}
	}
	return false
}

// CreateRemoteCodingTask creates a remote-coding task record with non-sensitive
// SSH metadata tags (host/user/port/workdir). The password is never persisted;
// call PrepareRemoteCodingEnvironment to connect and arm one-shot execution.
// Note: keep this signature fixed-arity — Wails cannot bind variadic methods.
func (a *App) CreateRemoteCodingTask(name, sshHost, sshUser, workDir string, sshPort int) ProjectSearchResult {
	return a.createRemoteCodingTaskWithTags(name, sshHost, sshUser, workDir, sshPort)
}

// createRemoteCodingTaskWithTags is the internal variadic form; extraTags may
// include origin markers such as source:coding_workflow.
func (a *App) createRemoteCodingTaskWithTags(name, sshHost, sshUser, workDir string, sshPort int, extraTags ...string) ProjectSearchResult {
	taskName := normalizeRecentTaskName(name)
	if taskName == "" {
		return ProjectSearchResult{}
	}
	host := normalizeSSHHostInput(sshHost)
	user := sanitizeTaskMetadataTagValue(sshUser)
	remoteWorkDir := sanitizeTaskMetadataTagValue(workDir)
	if host == "" || user == "" || remoteWorkDir == "" {
		return ProjectSearchResult{}
	}
	if sshPort <= 0 || sshPort >= 65536 {
		sshPort = 22
	}
	tags := []string{
		taskManagementTag,
		taskUserCreatedTag,
		taskRemoteCodingDevTag,
	}
	tags = append(tags, buildRemoteCodingMetaTags(host, user, remoteWorkDir, sshPort)...)
	for _, t := range extraTags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		tags = append(tags, t)
	}
	return a.createTaskRecordWithWorkingDir(taskName, "", tags, "", false)
}

// CreateRecentTaskWithWorkingDir creates a standalone task record with an
// optional execution working directory.
func (a *App) CreateRecentTaskWithWorkingDir(name, workingDir string) ProjectSearchResult {
	return a.CreateTaskWithMode(name, workingDir, "")
}

// PrepareLocalCodingEnvironment arms a one-shot local CodingSubAgent execution
// for the task project session. The next AI message on that project session
// runs pure-coding SubAgent execution (with source preview file events).
// executionDir is the directory tools should operate in; when empty, the task
// project path is used.
func (a *App) PrepareLocalCodingEnvironment(projectPath, executionDir string) error {
	if a == nil {
		return fmt.Errorf("app unavailable")
	}
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return fmt.Errorf("project path is required")
	}
	execDir := normalizeRecentTaskWorkingDir(executionDir)
	if execDir == "" {
		execDir = a.recentTaskExecutionProjectPath(projectPath)
	}
	if execDir == "" {
		execDir = projectPath
	}

	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return fmt.Errorf("AI assistant not initialized")
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil {
		return fmt.Errorf("message handler unavailable")
	}

	userID := projectSessionOwnerID(projectPath)
	handler.pendingTemplateRemoteCoding.Delete(userID)
	handler.pendingV2SubAgentExecution.Store(userID, true)
	handler.pendingTemplateCodingProjectPath.Store(userID, execDir)
	handler.workflowAgentLoopMarker.Store(userID, true)
	// Create-task pure coding: default workspace trust (path auto-allow; high-risk still prompts).
	// Does not persist subagent_full_access to config.
	handler.setStickyCodingSessionPermissionMode(userID, "workspace", "local", execDir)
	// If input-box already has global full control, upgrade sticky to full.
	handler.syncStickyCodingFullAccessFromGlobal(userID, "local", execDir, a.isSubAgentFullAccessGranted())
	log.Printf("[local-coding-env] prepared project=%s execDir=%s session_full_access=true", projectPath, execDir)
	return nil
}

// PrepareRemoteCodingEnvironment connects to the remote host over SSH and arms
// a one-shot RemoteCodingSubAgent execution for the given task project path.
// The next AI assistant message on that project session will run remote coding
// with the right-hand source preview enabled (pure remote coding workbench).
// Password is used only for this connect call and is not persisted.
func (a *App) PrepareRemoteCodingEnvironment(projectPath, sshHost, sshUser, sshPassword, workDir string, sshPort int) error {
	return a.prepareRemoteCodingEnvironment(projectPath, sshHost, sshUser, sshPassword, workDir, sshPort, "")
}

func (a *App) prepareRemoteCodingEnvironment(projectPath, sshHost, sshUser, sshPassword, workDir string, sshPort int, hostKeyFingerprint string) error {
	if a == nil {
		return fmt.Errorf("app unavailable")
	}
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return fmt.Errorf("project path is required")
	}
	host := normalizeSSHHostInput(sshHost)
	user := sanitizeTaskMetadataTagValue(sshUser)
	password := strings.TrimSpace(sshPassword)
	remoteWorkDir := sanitizeTaskMetadataTagValue(workDir)
	if host == "" || user == "" || password == "" || remoteWorkDir == "" {
		return fmt.Errorf("主机、用户名、密码和远程工作目录均为必填")
	}
	if sshPort <= 0 || sshPort >= 65536 {
		sshPort = 22
	}

	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return fmt.Errorf("AI assistant not initialized")
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil {
		return fmt.Errorf("message handler unavailable")
	}
	if handler.ensureSSHManager() == nil {
		return fmt.Errorf("SSH 会话管理器不可用")
	}

	sessionID, failReason := handler.findOrCreateSSHSessionWithPinnedAuth(user, host, sshPort, password, "", "", strings.TrimSpace(hostKeyFingerprint))
	if sessionID == "" {
		return errors.New(sshConnectFailureMessage(user, host, sshPort, failReason))
	}

	userID := projectSessionOwnerID(projectPath)
	handler.pendingTemplateCodingProjectPath.Delete(userID)
	handler.pendingV2SubAgentExecution.Store(userID, true)
	handler.pendingTemplateRemoteCoding.Store(userID, remoteCodingTemplateContext{
		SessionID:  sessionID,
		WorkDir:    remoteWorkDir,
		ProjectDir: remoteWorkDir,
	})
	handler.workflowAgentLoopMarker.Store(userID, true)
	// Same default workspace posture as local pure coding.
	handler.setStickyCodingSessionPermissionMode(userID, "workspace", "remote", remoteWorkDir)
	handler.syncStickyCodingFullAccessFromGlobal(userID, "remote", remoteWorkDir, a.isSubAgentFullAccessGranted())
	handler.bindStickyRemoteCodingContext(userID, remoteCodingTemplateContext{
		SessionID:  sessionID,
		WorkDir:    remoteWorkDir,
		ProjectDir: remoteWorkDir,
	}, host, user, sshPort)
	log.Printf("[remote-coding-env] prepared project=%s session=%s host=%s@%s:%d workdir=%s session_full_access=true", projectPath, sessionID, user, host, sshPort, remoteWorkDir)
	return nil
}

// CodingWorkbenchStatus describes pure-coding workbench arm/reconnect state for a task tab.
type CodingWorkbenchStatus struct {
	Kind                  string `json:"kind"` // "local" | "remote" | ""
	Armed                 bool   `json:"armed"`
	NeedsReconnect        bool   `json:"needs_reconnect"`
	TurnCount             int    `json:"turn_count"`
	SessionFullAccess     bool   `json:"session_full_access"`
	SessionHighRiskAccess bool   `json:"session_high_risk_access"`
	SessionPlan           string `json:"session_plan,omitempty"`
	// ExecutionPlan is the latest multi-step auto plan (markdown T1/T2…).
	ExecutionPlan string `json:"execution_plan,omitempty"`
	// PlanMode: auto | approve | off
	PlanMode string `json:"plan_mode,omitempty"`
	// PendingApproval is true when a multi-step plan awaits /plan approve.
	PendingApproval bool `json:"pending_approval,omitempty"`
	// StepStatuses is live Todo status for the active plan.
	StepStatuses []codingWorkbenchStepStatus `json:"step_statuses,omitempty"`
	// ProjectInstructions sources (AGENTS.md etc.).
	ProjectInstructionSources []string `json:"project_instruction_sources,omitempty"`
	// CheckpointLabel is the last saved checkpoint name.
	CheckpointLabel string `json:"checkpoint_label,omitempty"`
	// Session token/cost observability.
	SessionInputTokens   int     `json:"session_input_tokens,omitempty"`
	SessionOutputTokens  int     `json:"session_output_tokens,omitempty"`
	SessionEstCostRMB    float64 `json:"session_est_cost_rmb,omitempty"`
	LastTurnInputTokens  int     `json:"last_turn_input_tokens,omitempty"`
	LastTurnOutputTokens int     `json:"last_turn_output_tokens,omitempty"`
	LastTurnEstCostRMB   float64 `json:"last_turn_est_cost_rmb,omitempty"`
	// BackgroundVerify holds last async /bg test result summary.
	BackgroundVerify string `json:"background_verify,omitempty"`
	// WorktreeMode: auto | always | off
	WorktreeMode string `json:"worktree_mode,omitempty"`
	// WorktreeNotes recent isolation/merge lines.
	WorktreeNotes []string `json:"worktree_notes,omitempty"`
	// ConflictCount is number of kept isolation trees after failed merges.
	ConflictCount int `json:"conflict_count,omitempty"`
	// Conflicts brief list for UI (id + step + path).
	Conflicts []codingWorkbenchConflict `json:"conflicts,omitempty"`
	// ConflictActiveID / ConflictSelected / ConflictFocusFile restore conflict panel UI.
	ConflictActiveID  string   `json:"conflict_active_id,omitempty"`
	ConflictSelected  []string `json:"conflict_selected,omitempty"`
	ConflictFocusFile string   `json:"conflict_focus_file,omitempty"`
	// ConflictLog recent adopt/keep/base/discard lines for UI/slash.
	ConflictLog []string `json:"conflict_log,omitempty"`
	// Last model route for pure-coding observability.
	RouteModel  string `json:"route_model,omitempty"`
	RouteSource string `json:"route_source,omitempty"`
	RouteTask   string `json:"route_task,omitempty"`
	RouteReason string `json:"route_reason,omitempty"`
	// RoutePref: auto | primary | reasoning | vision
	RoutePref string `json:"route_pref,omitempty"`
	// RouteCapabilities maps each route pref to ModelRouter availability / model.
	RouteCapabilities []codingRouteCapability `json:"route_capabilities,omitempty"`
	// CheckpointFiles are paths recorded at the last checkpoint (for UI).
	CheckpointFiles []string `json:"checkpoint_files,omitempty"`
	// CheckpointSnapshots is count of restorable file content snapshots.
	CheckpointSnapshots int `json:"checkpoint_snapshots,omitempty"`
	// CheckpointHistory is current + prior checkpoints (current first).
	CheckpointHistory []codingCheckpointListEntry `json:"checkpoint_history,omitempty"`
	// HooksActive is true when .maclaw/hooks.json defines at least one command.
	HooksActive bool `json:"hooks_active,omitempty"`
	// HooksPhases lists lifecycle phases that have commands (pre_step, post_turn, …).
	HooksPhases []string `json:"hooks_phases,omitempty"`
	// HooksCommandCount is total non-empty hook commands across phases.
	HooksCommandCount int `json:"hooks_command_count,omitempty"`
	// HooksFailOnError mirrors hooks.json fail_on_error.
	HooksFailOnError bool   `json:"hooks_fail_on_error,omitempty"`
	LastSummary      string `json:"last_summary,omitempty"`
	RemoteHost       string `json:"remote_host,omitempty"`
	RemoteUser       string `json:"remote_user,omitempty"`
	RemotePort       int    `json:"remote_port,omitempty"`
	RemoteWorkDir    string `json:"remote_work_dir,omitempty"`
	RemoteSessionID  string `json:"remote_session_id,omitempty"`
	Message          string `json:"message,omitempty"`
}

func (a *App) remoteCodingMetaFromTaskTags(projectPath string) (host, user, workDir string, port int) {
	// Do not cold-init the memory store here: status/reconnect prefill can call
	// this on every tab focus. Callers that need index tags (Ensure) already
	// ensure the store before classification.
	if a == nil || a.memoryStore == nil {
		return "", "", "", 0
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return "", "", "", 0
	}
	rec := pi.Get(projectPath)
	if rec == nil {
		return "", "", "", 0
	}
	port = 22
	for _, tag := range rec.Tags {
		tag = strings.TrimSpace(tag)
		switch {
		case strings.HasPrefix(tag, taskRemoteHostTagPrefix):
			host = strings.TrimSpace(strings.TrimPrefix(tag, taskRemoteHostTagPrefix))
		case strings.HasPrefix(tag, taskRemoteUserTagPrefix):
			user = strings.TrimSpace(strings.TrimPrefix(tag, taskRemoteUserTagPrefix))
		case strings.HasPrefix(tag, taskRemoteWorkDirTagPrefix):
			// workdir tags are sanitized (colons→_); store best-effort display value.
			workDir = strings.TrimSpace(strings.TrimPrefix(tag, taskRemoteWorkDirTagPrefix))
		case strings.HasPrefix(tag, taskRemotePortTagPrefix):
			if p, err := strconv.Atoi(strings.TrimPrefix(tag, taskRemotePortTagPrefix)); err == nil && p > 0 && p < 65536 {
				port = p
			}
		}
	}
	return host, user, workDir, port
}

func (a *App) codingWorkbenchStatusFromHandler(projectPath string, handler *IMMessageHandler) CodingWorkbenchStatus {
	st := CodingWorkbenchStatus{}
	if handler == nil {
		st.Message = "message handler unavailable"
		return st
	}
	userID := projectSessionOwnerID(projectPath)
	// Read live in-memory sticky only. Do NOT force-flush debounced step-status
	// writes on every status poll — that defeats coalescing during multi-step runs.
	// Durability is covered by the 450ms debounce timer and shutdown flush.
	mem := handler.getStickyCodingWorkbenchMemory(userID)
	st.Kind = strings.TrimSpace(mem.Kind)
	st.TurnCount = mem.TurnCount
	st.SessionFullAccess = mem.SessionFullAccess
	st.SessionHighRiskAccess = mem.SessionHighRiskAccess
	st.SessionPlan = strings.TrimSpace(mem.SessionPlan)
	st.ExecutionPlan = strings.TrimSpace(mem.ExecutionPlan)
	st.PlanMode = normalizeCodingPlanMode(mem.PlanMode)
	st.PendingApproval = strings.TrimSpace(mem.PendingPlanJSON) != ""
	if len(mem.StepStatuses) > 0 {
		st.StepStatuses = append([]codingWorkbenchStepStatus(nil), mem.StepStatuses...)
	}
	if len(mem.ProjectInstructionSources) > 0 {
		st.ProjectInstructionSources = append([]string(nil), mem.ProjectInstructionSources...)
	}
	st.CheckpointLabel = strings.TrimSpace(mem.CheckpointLabel)
	st.SessionInputTokens = mem.SessionInputTokens
	st.SessionOutputTokens = mem.SessionOutputTokens
	st.SessionEstCostRMB = mem.SessionEstCostRMB
	st.LastTurnInputTokens = mem.LastTurnInputTokens
	st.LastTurnOutputTokens = mem.LastTurnOutputTokens
	st.LastTurnEstCostRMB = mem.LastTurnEstCostRMB
	st.BackgroundVerify = strings.TrimSpace(mem.BackgroundVerifySummary)
	st.WorktreeMode = normalizeCodingWorktreeMode(mem.WorktreeMode)
	if len(mem.WorktreeNotes) > 0 {
		st.WorktreeNotes = append([]string(nil), mem.WorktreeNotes...)
	}
	// Drop local conflict records whose worktree dirs vanished (cheap Stat).
	// Throttled: status polls often; Ensure/List still force full prune.
	// Remote isolate probe is deferred to Ensure / List (needs live SSH).
	if len(mem.WorktreeConflicts) > 0 {
		if n := handler.pruneDeadStickyCodingConflictsThrottled(userID, false, stickyConflictPruneStatusInterval); n > 0 {
			mem = handler.getStickyCodingWorkbenchMemory(userID)
		}
	}
	if n := len(mem.WorktreeConflicts); n > 0 {
		st.ConflictCount = n
		st.Conflicts = append([]codingWorkbenchConflict(nil), mem.WorktreeConflicts...)
	}
	st.ConflictActiveID = strings.TrimSpace(mem.ConflictActiveID)
	if len(mem.ConflictSelected) > 0 {
		st.ConflictSelected = append([]string(nil), mem.ConflictSelected...)
	}
	st.ConflictFocusFile = strings.TrimSpace(mem.ConflictFocusFile)
	if len(mem.ConflictLog) > 0 {
		st.ConflictLog = append([]string(nil), mem.ConflictLog...)
	}
	// Drop stale active id if conflict list no longer contains it (response + sticky).
	if st.ConflictActiveID != "" {
		found := false
		for _, c := range st.Conflicts {
			if c.ID == st.ConflictActiveID {
				found = true
				break
			}
		}
		if !found {
			st.ConflictActiveID = ""
			st.ConflictSelected = nil
			st.ConflictFocusFile = ""
			handler.setStickyCodingConflictUIState(userID, "", "", nil)
		} else if len(st.ConflictSelected) > 0 {
			// Filter multi-select to paths still present on the active conflict.
			var remain []string
			for _, c := range st.Conflicts {
				if c.ID == st.ConflictActiveID {
					remain = c.Files
					break
				}
			}
			if len(remain) > 0 {
				keep := map[string]struct{}{}
				for _, f := range remain {
					keep[filepath.ToSlash(strings.TrimSpace(f))] = struct{}{}
				}
				filtered := st.ConflictSelected[:0]
				for _, s := range st.ConflictSelected {
					if _, ok := keep[s]; ok {
						filtered = append(filtered, s)
					}
				}
				if len(filtered) != len(st.ConflictSelected) {
					st.ConflictSelected = append([]string(nil), filtered...)
					handler.setStickyCodingConflictUIState(userID, st.ConflictActiveID, st.ConflictFocusFile, st.ConflictSelected)
				}
			}
		}
	}
	st.RouteModel = strings.TrimSpace(mem.LastRouteModel)
	st.RouteSource = strings.TrimSpace(mem.LastRouteSource)
	st.RouteTask = strings.TrimSpace(mem.LastRouteTask)
	st.RouteReason = strings.TrimSpace(mem.LastRouteReason)
	st.RoutePref = normalizeCodingRoutePref(mem.RoutePref)
	if caps := handler.codingRouteCapabilities(); len(caps) > 0 {
		st.RouteCapabilities = caps
	}
	// Checkpoint list from the same sticky snapshot already loaded (one parse).
	if cur, hasCur, histRaw := stickyCodingCheckpointsFromMem(mem); hasCur || len(histRaw) > 0 {
		var hist []codingCheckpointListEntry
		if hasCur {
			hist = append(hist, codingCheckpointToListEntry(cur, true))
			if st.CheckpointLabel == "" {
				st.CheckpointLabel = cur.Label
			}
			if len(cur.Files) > 0 {
				st.CheckpointFiles = append([]string(nil), cur.Files...)
			}
			st.CheckpointSnapshots = codingCheckpointSnapshotCount(cur)
		}
		for i := len(histRaw) - 1; i >= 0; i-- {
			hist = append(hist, codingCheckpointToListEntry(histRaw[i], false))
		}
		st.CheckpointHistory = hist
	}
	// Project-local lifecycle hooks (.maclaw/hooks.json) — single load + one summary pass.
	hooks := loadCodingWorkbenchHooks(projectPath)
	if phases, n := codingWorkbenchHooksSummary(hooks); n > 0 {
		st.HooksActive = true
		st.HooksPhases = phases
		st.HooksCommandCount = n
		st.HooksFailOnError = hooks.FailOnError
	}
	st.LastSummary = strings.TrimSpace(mem.LastSummary)
	st.RemoteHost = strings.TrimSpace(mem.RemoteHost)
	st.RemoteUser = strings.TrimSpace(mem.RemoteUser)
	st.RemotePort = mem.RemotePort
	st.RemoteWorkDir = strings.TrimSpace(mem.RemoteWorkDir)
	if st.RemoteWorkDir == "" {
		st.RemoteWorkDir = strings.TrimSpace(mem.RemoteProjectDir)
	}
	st.RemoteSessionID = strings.TrimSpace(mem.RemoteSessionID)

	// Task tags are the durable pure-coding marker (history list restore).
	// When a record exists without coding tags, force Kind empty even if sticky
	// still claims local/remote (sticky pollution guard).
	// Use an already-open store only; status polls must not cold-init memory
	// (ensureMemoryStore -> LoadConfig can re-enter path-bound locks).
	if a != nil && a.memoryStore != nil {
		if pi := a.memoryStore.ProjectIndex(); pi != nil {
			if rec := pi.Get(projectPath); rec != nil {
				if projectRecordHasTag(*rec, taskRemoteCodingDevTag) {
					st.Kind = "remote"
				} else if projectRecordHasTag(*rec, taskCodingDevTag) {
					st.Kind = "local"
				} else {
					st.Kind = ""
				}
			}
		}
	}

	// Fill non-secret remote meta from task tags when sticky is incomplete.
	if st.Kind == "remote" || st.Kind == "" {
		th, tu, tw, tp := a.remoteCodingMetaFromTaskTags(projectPath)
		if st.RemoteHost == "" {
			st.RemoteHost = th
		}
		if st.RemoteUser == "" {
			st.RemoteUser = tu
		}
		if st.RemoteWorkDir == "" {
			st.RemoteWorkDir = tw
		}
		if st.RemotePort <= 0 {
			st.RemotePort = tp
		}
	}

	switch st.Kind {
	case "remote":
		st.Armed = handler.hasPendingTemplateSubAgentExecution(userID)
		if _, ok := handler.pendingTemplateRemoteCoding.Load(userID); ok {
			st.Armed = true
		}
		sessionID := st.RemoteSessionID
		alive := sessionID != "" && handler.sshSessionAlive(sessionID)
		if !alive {
			st.Armed = false
			st.NeedsReconnect = true
			if sessionID == "" {
				st.Message = "SSH session not connected; reconnect required"
			} else {
				st.Message = "SSH session expired; reconnect required"
			}
		}
	case "local":
		st.Armed = handler.hasPendingTemplateSubAgentExecution(userID)
		if _, ok := handler.pendingTemplateCodingProjectPath.Load(userID); ok {
			st.Armed = true
		}
	}
	return st
}

// GetCodingWorkbenchStatus returns arm/reconnect state for a pure coding task without changing it.
func (a *App) GetCodingWorkbenchStatus(projectPath string) CodingWorkbenchStatus {
	st := CodingWorkbenchStatus{}
	if a == nil {
		st.Message = "app unavailable"
		return st
	}
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		st.Message = "project path is required"
		return st
	}
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		st.Message = "AI assistant not initialized"
		return st
	}
	handler := hubClient.ensureIMHandler()
	return a.codingWorkbenchStatusFromHandler(projectPath, handler)
}

// EnsureCodingWorkbenchArmed reloads sticky coding memory and re-arms local/remote
// SubAgent routing when the user reopens a pure coding task (after restart or
// tab close). For remote tasks, SSH session must still be alive; otherwise the
// status reports NeedsReconnect so the UI can collect a password and call
// PrepareRemoteCodingEnvironment.
func (a *App) EnsureCodingWorkbenchArmed(projectPath string) (CodingWorkbenchStatus, error) {
	st := CodingWorkbenchStatus{}
	if a == nil {
		return st, fmt.Errorf("app unavailable")
	}
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return st, fmt.Errorf("project path is required")
	}
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return st, fmt.Errorf("AI assistant not initialized")
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil {
		return st, fmt.Errorf("message handler unavailable")
	}

	userID := projectSessionOwnerID(projectPath)
	// Force disk cold-load into the in-memory map.
	mem := handler.getStickyCodingWorkbenchMemory(userID)

	// Classification policy for history restore:
	// 1) When a project index record exists, task tags are authoritative
	//    (prevents sticky kind pollution from arming ordinary chat tasks).
	// 2) When the index misses (async flush lag), sticky kind is a soft fallback.
	kind := ""
	taskTitle := ""
	foundRecord := false
	a.ensureMemoryStore()
	if a.memoryStore != nil {
		if pi := a.memoryStore.ProjectIndex(); pi != nil {
			if rec := pi.Get(projectPath); rec != nil {
				foundRecord = true
				taskTitle = strings.TrimSpace(rec.Name)
				if projectRecordHasTag(*rec, taskRemoteCodingDevTag) {
					kind = "remote"
				} else if projectRecordHasTag(*rec, taskCodingDevTag) {
					kind = "local"
				}
			}
		}
	}
	if !foundRecord {
		kind = strings.TrimSpace(mem.Kind)
	}

	// Not a pure coding workbench: do not arm CodingSubAgent (avoids hijacking
	// ordinary chat tasks opened from the history list). Clear any leftover
	// sticky/pending coding bindings from prior pollution or mis-classification.
	if kind != "local" && kind != "remote" {
		handler.clearStickyCodingEnvironment(userID)
		st = a.codingWorkbenchStatusFromHandler(projectPath, handler)
		st.Kind = ""
		st.Armed = false
		st.NeedsReconnect = false
		st.Message = "not a pure coding workbench task"
		return st, nil
	}

	// Compaction/restart recovery: seed session plan from last request or task
	// title when empty so multi-turn continuity still has an overall goal.
	if strings.TrimSpace(mem.SessionPlan) == "" {
		seed := strings.TrimSpace(mem.LastUserText)
		if seed == "" {
			seed = taskTitle
		}
		if seed != "" {
			handler.setStickyCodingSessionPlan(userID, seed)
			mem = handler.getStickyCodingWorkbenchMemory(userID)
		}
	}

	// Persist kind + remote meta into sticky so later cold-loads stay classified.
	// Skip the disk write when nothing material changed (hot resume path).
	// Important: do not interleave setSticky* helpers then storeSticky(mem) with a
	// stale mem snapshot — that can clobber fields (e.g. Kind after RoutePref seed).
	stickyDirty := mem.Kind != kind
	mem.Kind = kind
	if kind == "remote" {
		th, tu, tw, tp := a.remoteCodingMetaFromTaskTags(projectPath)
		if strings.TrimSpace(mem.RemoteHost) == "" && th != "" {
			mem.RemoteHost = th
			stickyDirty = true
		}
		if strings.TrimSpace(mem.RemoteUser) == "" && tu != "" {
			mem.RemoteUser = tu
			stickyDirty = true
		}
		if strings.TrimSpace(mem.RemoteWorkDir) == "" && tw != "" {
			mem.RemoteWorkDir = tw
			mem.RemoteProjectDir = tw
			stickyDirty = true
		}
		if mem.RemotePort <= 0 && tp > 0 {
			mem.RemotePort = tp
			stickyDirty = true
		}
	}
	// Seed sticky route pref from global config when session never set one.
	// Apply onto the local mem snapshot only (single store below).
	if strings.TrimSpace(mem.RoutePref) == "" {
		if cfg, err := a.LoadConfig(); err == nil {
			if raw := strings.TrimSpace(cfg.CodingRoutePref); raw != "" {
				mem.RoutePref = normalizeCodingRoutePref(raw)
				stickyDirty = true
			}
		}
	}
	if stickyDirty {
		handler.storeStickyCodingWorkbenchMemory(userID, mem)
	}

	switch kind {
	case "remote":
		sessionID := strings.TrimSpace(mem.RemoteSessionID)
		workDir := strings.TrimSpace(mem.RemoteWorkDir)
		projectDir := strings.TrimSpace(mem.RemoteProjectDir)
		if workDir == "" {
			workDir = projectDir
		}
		if projectDir == "" {
			projectDir = workDir
		}
		if sessionID == "" || (handler.ensureSSHManager() != nil && !handler.sshSessionAlive(sessionID)) {
			log.Printf("[coding-env] ensure armed: remote reconnect required project=%s session=%q", projectPath, sessionID)
			st = a.codingWorkbenchStatusFromHandler(projectPath, handler)
			st.Kind = "remote"
			st.Armed = false
			st.NeedsReconnect = true
			if st.Message == "" {
				st.Message = "SSH session not connected; reconnect required"
			}
			return st, nil
		}
		if workDir == "" {
			st = a.codingWorkbenchStatusFromHandler(projectPath, handler)
			st.Kind = "remote"
			st.NeedsReconnect = true
			st.Message = "remote work directory missing; reconnect required"
			return st, nil
		}
		handler.rearmStickyRemoteCodingEnvironment(userID, remoteCodingTemplateContext{
			SessionID:  sessionID,
			WorkDir:    workDir,
			ProjectDir: projectDir,
		})
		// Do not re-seed path trust when the user explicitly chose "请求授权".
		if !mem.SessionFullAccess && !stickyCodingPermissionIsRequest(mem.SessionPermissionMode) {
			handler.setStickyCodingSessionPermissionMode(userID, "workspace", "remote", projectDir)
		}
		// Align sticky with input-box global full control when already granted
		// (no-op when session mode is explicit request).
		handler.syncStickyCodingFullAccessFromGlobal(userID, "remote", projectDir, a.isSubAgentFullAccessGranted())
		// Drop remote isolate records whose dirs no longer exist on the host.
		if n := handler.pruneDeadStickyCodingConflicts(userID, true); n > 0 {
			log.Printf("[coding-env] ensure armed: pruned %d dead isolation conflicts project=%s", n, projectPath)
		}
		log.Printf("[coding-env] ensure armed remote project=%s session=%s turns=%d plan=%q",
			projectPath, sessionID, mem.TurnCount, truncateRunesV2(mem.SessionPlan, 60))
		st = a.codingWorkbenchStatusFromHandler(projectPath, handler)
		st.Kind = "remote"
		st.Armed = true
		st.NeedsReconnect = false
		return st, nil
	case "local":
		execDir := strings.TrimSpace(mem.ProjectPath)
		if execDir == "" {
			execDir = a.recentTaskExecutionProjectPath(projectPath)
		}
		if execDir == "" {
			execDir = projectPath
		}
		handler.rearmStickyLocalCodingEnvironment(userID, execDir)
		if !mem.SessionFullAccess && !stickyCodingPermissionIsRequest(mem.SessionPermissionMode) {
			handler.setStickyCodingSessionPermissionMode(userID, "workspace", "local", execDir)
		}
		handler.syncStickyCodingFullAccessFromGlobal(userID, "local", execDir, a.isSubAgentFullAccessGranted())
		if n := handler.pruneDeadStickyCodingConflicts(userID, false); n > 0 {
			log.Printf("[coding-env] ensure armed: pruned %d dead isolation conflicts project=%s", n, projectPath)
		}
		// Re-open an existing pure-coding task: push known/on-disk sources into
		// the right-hand preview so the panel is not blank after generation.
		// Only bootstrap after at least one turn (or sticky files exist) to avoid
		// scanning empty new projects on every tab focus.
		mem = handler.getStickyCodingWorkbenchMemory(userID)
		stickyFiles := uniqueSortedSubAgentStrings(append(append([]string{}, mem.FilesModified...), mem.FilesCreated...))
		// Sticky-only on arm (no directory scan): tab focus must stay cheap.
		// End-of-turn emit allows scan when sticky is empty.
		if len(stickyFiles) > 0 {
			// Route with tab projectPath (managed task dir), read files from execDir.
			emitCodingWorkbenchSourcePreview(
				a,
				codingWorkbenchPreviewRestoreSessionID(userID),
				execDir,
				nil,
				nil,
				stickyFiles,
				false, // allowScan: never walk the tree on tab focus
				false, // forceOpen: do not hijack an active coding-workbench session
				projectPath,
			)
		}
		log.Printf("[coding-env] ensure armed local project=%s execDir=%s turns=%d plan=%q",
			projectPath, execDir, mem.TurnCount, truncateRunesV2(mem.SessionPlan, 60))
		st = a.codingWorkbenchStatusFromHandler(projectPath, handler)
		st.Kind = "local"
		st.Armed = true
		st.NeedsReconnect = false
		return st, nil
	default:
		return a.codingWorkbenchStatusFromHandler(projectPath, handler), nil
	}
}

// GetCodingWorkbenchPermission returns the effective permission tier for a pure
// coding task tab: "request" | "workspace" | "full".
func (a *App) GetCodingWorkbenchPermission(projectPath string) string {
	if a == nil {
		return "request"
	}
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return "request"
	}
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return "request"
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil {
		return "request"
	}
	return handler.codingWorkbenchPermissionMode(projectSessionOwnerID(projectPath), a.isSubAgentFullAccessGranted())
}

// SetCodingWorkbenchPermission sets the pure-coding permission tier for a task.
// Modes: "request" (interactive), "workspace" (session path trust), "full" (global full access).
func (a *App) SetCodingWorkbenchPermission(projectPath, mode string) error {
	if a == nil {
		return fmt.Errorf("app unavailable")
	}
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return fmt.Errorf("project path is required")
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "request", "workspace", "full", "ask":
	default:
		return fmt.Errorf("unknown permission mode %q (want request|workspace|full)", mode)
	}
	if mode == "ask" {
		mode = "request"
	}

	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return fmt.Errorf("AI assistant not initialized")
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil {
		return fmt.Errorf("message handler unavailable")
	}
	userID := projectSessionOwnerID(projectPath)

	// Permission UI must not overwrite sticky ProjectPath (often the execution
	// workspace dir for local pure coding). Empty projectPath keeps existing value.
	switch mode {
	case "full":
		// Path + high-risk session trust (one sticky write), and persist global full-access.
		handler.setStickyCodingSessionPermissionMode(userID, "full", "", "")
		a.persistSubAgentFullAccess()
	case "workspace":
		// Path trust only: clear global full-access and high-risk session grant.
		if a.isSubAgentFullAccessGranted() {
			if err := a.clearSubAgentFullAccess(); err != nil {
				log.Printf("[coding-env] clear global full access failed: %v", err)
			}
		}
		handler.setStickyCodingSessionPermissionMode(userID, "workspace", "", "")
	case "request":
		if a.isSubAgentFullAccessGranted() {
			if err := a.clearSubAgentFullAccess(); err != nil {
				log.Printf("[coding-env] clear global full access failed: %v", err)
			}
		}
		// Explicit request mode so Ensure does not re-seed workspace path trust.
		handler.setStickyCodingSessionPermissionMode(userID, "request", "", "")
	}
	return nil
}

// SetCodingWorkbenchSessionPlan updates the durable multi-turn plan/goal for a
// pure coding task. Empty plan clears the stored goal.
func (a *App) SetCodingWorkbenchSessionPlan(projectPath, plan string) error {
	if a == nil {
		return fmt.Errorf("app unavailable")
	}
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return fmt.Errorf("project path is required")
	}
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return fmt.Errorf("AI assistant not initialized")
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil {
		return fmt.Errorf("message handler unavailable")
	}
	userID := projectSessionOwnerID(projectPath)
	handler.setStickyCodingSessionPlan(userID, plan)
	return nil
}

// GetCodingWorkbenchPlanMode returns auto | approve | off for a pure-coding task.
func (a *App) GetCodingWorkbenchPlanMode(projectPath string) string {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil || handler == nil {
		return codingPlanModeAuto
	}
	return handler.getStickyCodingPlanMode(userID)
}

// SetCodingWorkbenchPlanMode sets multi-step plan policy: auto | approve | off.
func (a *App) SetCodingWorkbenchPlanMode(projectPath, mode string) error {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return err
	}
	handler.setStickyCodingPlanMode(userID, mode)
	return nil
}

// ApproveCodingWorkbenchPlan promotes a pending multi-step plan for the next
// coding turn. Prefer sending `/plan approve` in the chat (executes immediately).
// This binding only marks the plan approved so the next message can pick it up.
func (a *App) ApproveCodingWorkbenchPlan(projectPath string) error {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return err
	}
	if _, ok := handler.promotePendingToApprovedCodingPlan(userID); !ok {
		return fmt.Errorf("no pending plan to approve")
	}
	return nil
}

// RejectCodingWorkbenchPlan clears a pending multi-step plan.
func (a *App) RejectCodingWorkbenchPlan(projectPath string) error {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return err
	}
	handler.clearStickyPendingCodingPlan(userID)
	handler.clearStickyCodingExecutionPlan(userID)
	handler.clearStickyCodingStepStatuses(userID)
	return nil
}

// codingWorkbenchPendingPlanDTO is UI-safe pending plan payload.
type codingWorkbenchPendingPlanDTO struct {
	UserText  string `json:"user_text,omitempty"`
	Markdown  string `json:"markdown,omitempty"`
	StepCount int    `json:"step_count,omitempty"`
	CreatedAt int64  `json:"created_at,omitempty"`
}

// GetCodingWorkbenchPendingPlan returns the awaiting-approval multi-step plan.
func (a *App) GetCodingWorkbenchPendingPlan(projectPath string) (codingWorkbenchPendingPlanDTO, error) {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return codingWorkbenchPendingPlanDTO{}, err
	}
	pending, ok := handler.loadStickyPendingCodingPlan(userID)
	if !ok {
		return codingWorkbenchPendingPlanDTO{}, fmt.Errorf("no pending plan")
	}
	return codingWorkbenchPendingPlanDTO{
		UserText:  pending.UserText,
		Markdown:  pending.Markdown,
		StepCount: len(pending.Tasks),
		CreatedAt: pending.CreatedAt,
	}, nil
}

// UpdateCodingWorkbenchPendingPlan rewrites the pending plan from edited markdown.
// Does not execute; user still needs /plan approve (or UI Approve).
func (a *App) UpdateCodingWorkbenchPendingPlan(projectPath, markdown string) (codingWorkbenchPendingPlanDTO, error) {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return codingWorkbenchPendingPlanDTO{}, err
	}
	updated, err := handler.replaceStickyPendingCodingPlanMarkdown(userID, markdown)
	if err != nil {
		return codingWorkbenchPendingPlanDTO{}, err
	}
	return codingWorkbenchPendingPlanDTO{
		UserText:  updated.UserText,
		Markdown:  updated.Markdown,
		StepCount: len(updated.Tasks),
		CreatedAt: updated.CreatedAt,
	}, nil
}

// SaveCodingWorkbenchCheckpoint stores a pure-coding session checkpoint.
func (a *App) SaveCodingWorkbenchCheckpoint(projectPath, label string) (string, error) {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return "", err
	}
	cp := handler.saveStickyCodingCheckpoint(userID, label)
	return cp.Label, nil
}

// RestoreCodingWorkbenchCheckpoint restores session/execution plan from the last checkpoint.
// When restoreFiles is true, also writes back snapshotted text file content.
func (a *App) RestoreCodingWorkbenchCheckpoint(projectPath string) (string, error) {
	return a.RestoreCodingWorkbenchCheckpointEx(projectPath, false)
}

// RestoreCodingWorkbenchCheckpointEx restores plan, and optionally file snapshots.
func (a *App) RestoreCodingWorkbenchCheckpointEx(projectPath string, restoreFiles bool) (string, error) {
	return a.RestoreCodingWorkbenchCheckpointByLabel(projectPath, "", restoreFiles)
}

// RestoreCodingWorkbenchCheckpointByLabel restores a named checkpoint (or current when label empty).
func (a *App) RestoreCodingWorkbenchCheckpointByLabel(projectPath, label string, restoreFiles bool) (string, error) {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return "", err
	}
	cp, ok := handler.restoreStickyCodingCheckpointByLabel(userID, label, false)
	if !ok {
		if strings.TrimSpace(label) != "" {
			return "", fmt.Errorf("checkpoint not found: %s", label)
		}
		return "", fmt.Errorf("no checkpoint to restore")
	}
	if !restoreFiles {
		return cp.Label, nil
	}
	restored, skipped, ferr := handler.applyCodingCheckpointFileSnapshots(userID, cp, nil)
	if ferr != nil {
		return "", fmt.Errorf("plan restored (%s); files: %w", cp.Label, ferr)
	}
	return fmt.Sprintf("%s (files restored=%d skipped=%d)", cp.Label, restored, skipped), nil
}

// ListCodingWorkbenchCheckpoints returns current + history checkpoint summaries.
func (a *App) ListCodingWorkbenchCheckpoints(projectPath string) []codingCheckpointListEntry {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return nil
	}
	return handler.listStickyCodingCheckpoints(userID)
}

// KeepMainCodingWorkbenchConflict keeps main-tree versions for selected conflict files.
// filesCSV empty means keep main for all (discard isolation).
func (a *App) KeepMainCodingWorkbenchConflict(projectPath, conflictID, filesCSV string) (string, error) {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return "", err
	}
	var files []string
	for _, f := range strings.Split(filesCSV, ",") {
		f = strings.TrimSpace(f)
		if f != "" {
			files = append(files, f)
		}
	}
	return handler.keepMainCodingConflictFiles(userID, conflictID, files)
}

// AdoptBaseCodingWorkbenchConflict writes merge-base content for selected files onto main.
// filesCSV empty means all remaining conflict files.
func (a *App) AdoptBaseCodingWorkbenchConflict(projectPath, conflictID, filesCSV string) (string, error) {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return "", err
	}
	var files []string
	for _, f := range strings.Split(filesCSV, ",") {
		f = strings.TrimSpace(f)
		if f != "" {
			files = append(files, f)
		}
	}
	return handler.adoptBaseCodingConflictFiles(userID, conflictID, files)
}

// codingConflictResolveCSV splits optional comma-separated paths.
func codingConflictResolveCSV(filesCSV string) []string {
	var files []string
	for _, f := range strings.Split(filesCSV, ",") {
		f = strings.TrimSpace(f)
		if f != "" {
			files = append(files, f)
		}
	}
	return files
}

// ResolveCodingWorkbenchConflict applies a batch resolve action to a conflict.
// action: "adopt" | "keep" | "base" — filesCSV empty means all remaining files.
func (a *App) ResolveCodingWorkbenchConflict(projectPath, conflictID, action, filesCSV string) (string, error) {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return "", err
	}
	files := codingConflictResolveCSV(filesCSV)
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "adopt", "theirs":
		if len(files) == 0 {
			return handler.adoptCodingWorkbenchConflict(userID, conflictID)
		}
		return handler.adoptCodingWorkbenchConflictFiles(userID, conflictID, files)
	case "keep", "main", "ours":
		return handler.keepMainCodingConflictFiles(userID, conflictID, files)
	case "base", "adopt-base", "take-base":
		return handler.adoptBaseCodingConflictFiles(userID, conflictID, files)
	default:
		return "", fmt.Errorf("unknown resolve action %q (use adopt|keep|base)", action)
	}
}

// mapConflictPreviewSideToAction maps preview pane side → resolve action.
func mapConflictPreviewSideToAction(side string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "main", "ours", "keep":
		return "keep", nil
	case "theirs", "isolate", "worktree", "wt", "adopt":
		return "adopt", nil
	case "base", "adopt-base", "take-base":
		return "base", nil
	default:
		return "", fmt.Errorf("unknown preview side %q (use main|theirs|base)", side)
	}
}

// ApplyCodingWorkbenchConflictPreviewSide writes the previewed side for one file
// onto the main tree: main→keep, theirs→adopt, base→adopt-base.
func (a *App) ApplyCodingWorkbenchConflictPreviewSide(projectPath, conflictID, relPath, side string) (string, error) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return "", fmt.Errorf("file path required")
	}
	action, err := mapConflictPreviewSideToAction(side)
	if err != nil {
		return "", err
	}
	return a.ResolveCodingWorkbenchConflict(projectPath, conflictID, action, relPath)
}

// WriteCodingWorkbenchConflictFileContent writes edited text to the main tree for one
// conflict path and removes it from remaining conflict files (manual merge).
func (a *App) WriteCodingWorkbenchConflictFileContent(projectPath, conflictID, relPath, content string) (string, error) {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return "", err
	}
	return handler.writeCodingConflictFileContent(userID, conflictID, relPath, content)
}

// GetCodingWorkbenchRouteMap returns ModelRouter capability table for pure-coding UI/settings.
// projectPath may be empty — then capabilities come from the global App ModelRouter.
func (a *App) GetCodingWorkbenchRouteMap(projectPath string) []codingRouteCapability {
	if strings.TrimSpace(projectPath) != "" {
		handler, _, err := a.codingWorkbenchHandlerForProject(projectPath)
		if err == nil && handler != nil {
			return handler.codingRouteCapabilities()
		}
	}
	// Settings panel / no active task: still resolve against live App router.
	h := &IMMessageHandler{app: a}
	return h.codingRouteCapabilities()
}

// PruneCodingWorkbenchCheckpoints drops non-current checkpoint sidecars for the task user.
func (a *App) PruneCodingWorkbenchCheckpoints(projectPath string) (string, error) {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return "", err
	}
	userN, orphanN := handler.pruneStickyCodingCheckpointSidecars(userID)
	st := collectCodingCheckpointSidecarStats(userID, "")
	if cp, ok := handler.loadStickyCodingCheckpoint(userID); ok {
		st = collectCodingCheckpointSidecarStats(userID, cp.Label)
	}
	return fmt.Sprintf("pruned user=%d orphan=%d · %s", userN, orphanN, formatCodingCheckpointSidecarStatsLine(st)), nil
}

// GetCodingWorkbenchCheckpointSidecarStats returns disk usage of checkpoint sidecars.
// projectPath may be empty for global-only stats.
func (a *App) GetCodingWorkbenchCheckpointSidecarStats(projectPath string) codingCheckpointSidecarStats {
	userID := ""
	keep := ""
	if strings.TrimSpace(projectPath) != "" {
		if handler, uid, err := a.codingWorkbenchHandlerForProject(projectPath); err == nil && handler != nil {
			userID = uid
			if cp, ok := handler.loadStickyCodingCheckpoint(userID); ok {
				keep = cp.Label
			}
		}
	}
	return collectCodingCheckpointSidecarStats(userID, keep)
}

// SetCodingWorkbenchConflictUIState remembers active conflict + multi-select for the task tab.
func (a *App) SetCodingWorkbenchConflictUIState(projectPath, activeID, focusFile, selectedCSV string) error {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return err
	}
	handler.setStickyCodingConflictUIState(userID, activeID, focusFile, codingConflictResolveCSV(selectedCSV))
	return nil
}

// GetCodingWorkbenchConflictFilePreview returns a longer content peek for one conflict file side.
// side: main | theirs | base
func (a *App) GetCodingWorkbenchConflictFilePreview(projectPath, conflictID, relPath, side string) (codingConflictFilePreview, error) {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return codingConflictFilePreview{}, err
	}
	return handler.getCodingConflictFilePreview(userID, conflictID, relPath, side, codingConflictPreviewMaxBytes)
}

// GetCodingWorkbenchConflictFileTriple returns main/theirs/base peeks for side-by-side UI.
func (a *App) GetCodingWorkbenchConflictFileTriple(projectPath, conflictID, relPath string) (codingConflictFileTriple, error) {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return codingConflictFileTriple{}, err
	}
	return handler.getCodingConflictFileTriple(userID, conflictID, relPath, codingConflictPreviewMaxBytes)
}

// ClearCodingWorkbenchConflictLog clears the conflict resolve audit trail.
func (a *App) ClearCodingWorkbenchConflictLog(projectPath string) error {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return err
	}
	handler.clearStickyCodingConflictLog(userID)
	return nil
}

// ExportCodingWorkbenchConflictLog exports the audit trail as markdown and folds
// a summary into worktree notes. Returns the markdown body.
func (a *App) ExportCodingWorkbenchConflictLog(projectPath string) (string, error) {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return "", err
	}
	md, n := handler.exportStickyCodingConflictLog(userID)
	if n == 0 {
		return "", fmt.Errorf("no conflict log entries")
	}
	return md, nil
}

// OpenCodingWorkbenchConflictFile opens main or isolate-side file in the OS default app / explorer.
// side: "main" | "theirs" (default main).
func (a *App) OpenCodingWorkbenchConflictFile(projectPath, conflictID, relPath, side string) error {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return err
	}
	c, ok := handler.findStickyCodingConflict(userID, conflictID)
	if !ok {
		return fmt.Errorf("conflict not found: %s", conflictID)
	}
	relPath = filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(relPath, "./")))
	if relPath == "" || strings.Contains(relPath, "..") {
		return fmt.Errorf("invalid file path")
	}
	side = strings.ToLower(strings.TrimSpace(side))
	var root string
	switch side {
	case "theirs", "isolate", "worktree", "wt":
		root = strings.TrimSpace(c.Path)
	default:
		root = resolveConflictGitRoot(c, userID, handler)
		if root == "" {
			root = strings.TrimSpace(c.MainProject)
		}
		if root == "" {
			root = strings.TrimSpace(handler.getStickyCodingWorkbenchMemory(userID).ProjectPath)
		}
	}
	if root == "" {
		return fmt.Errorf("cannot resolve root for side %q", side)
	}
	abs := filepath.Clean(filepath.Join(root, filepath.FromSlash(relPath)))
	if !isPathInsideRoot(root, abs) {
		return fmt.Errorf("path escapes conflict root")
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("file not found: %s", relPath)
	}
	return a.OpenFileOrShowInFolder(abs)
}

// RunCodingWorkbenchBackgroundVerify starts async /bg test for the pure-coding task.
func (a *App) RunCodingWorkbenchBackgroundVerify(projectPath string) (string, error) {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return "", err
	}
	// Prefer sticky ProjectPath (workspace exec dir) over task folder.
	execDir := ""
	if mem := handler.getStickyCodingWorkbenchMemory(userID); strings.TrimSpace(mem.ProjectPath) != "" {
		execDir = strings.TrimSpace(mem.ProjectPath)
	}
	if execDir == "" {
		execDir = a.recentTaskExecutionProjectPath(projectPath)
	}
	if execDir == "" {
		execDir = projectPath
	}
	return handler.startCodingWorkbenchBackgroundVerify(userID, execDir)
}

// GetCodingWorkbenchWorktreeMode returns auto | always | off.
func (a *App) GetCodingWorkbenchWorktreeMode(projectPath string) string {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil || handler == nil {
		return codingWorktreeModeAuto
	}
	return handler.getStickyCodingWorktreeMode(userID)
}

// SetCodingWorkbenchWorktreeMode sets git worktree isolation policy.
func (a *App) SetCodingWorkbenchWorktreeMode(projectPath, mode string) error {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return err
	}
	handler.setStickyCodingWorktreeMode(userID, mode)
	return nil
}

// GetCodingWorkbenchRoutePref returns auto | primary | reasoning | vision.
func (a *App) GetCodingWorkbenchRoutePref(projectPath string) string {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil || handler == nil {
		return codingRoutePrefAuto
	}
	return handler.getStickyCodingRoutePref(userID)
}

// SetCodingWorkbenchRoutePref sets pure-coding model preference.
// When CodingRoutePrefMirror is enabled in AppConfig, also updates the global
// default so new sessions inherit the last session choice.
func (a *App) SetCodingWorkbenchRoutePref(projectPath, pref string) error {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return err
	}
	pref = normalizeCodingRoutePref(pref)
	handler.setStickyCodingRoutePref(userID, pref)
	// Optional reverse sync: session → global default.
	// Avoid PatchConfigFields when unchanged (prevents extra disk + router churn).
	if cfg, lerr := a.LoadConfig(); lerr == nil && cfg.CodingRoutePrefMirror {
		if normalizeCodingRoutePref(cfg.CodingRoutePref) != pref {
			if _, perr := a.PatchConfigFields(map[string]interface{}{
				"coding_route_pref": pref,
			}); perr != nil {
				log.Printf("[coding-env] mirror route pref to config: %v", perr)
			}
		}
	}
	return nil
}

// ListCodingWorkbenchConflicts returns kept isolation conflicts for UI.
// Prunes dead local worktrees and (when SSH is live) dead remote isolates.
func (a *App) ListCodingWorkbenchConflicts(projectPath string) []codingWorkbenchConflict {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil || handler == nil {
		return nil
	}
	// probeRemote=true: drop vanished remote isolate dirs when session alive.
	_ = handler.pruneDeadStickyCodingConflicts(userID, true)
	return handler.listStickyCodingConflicts(userID)
}

// GetCodingWorkbenchConflictDiffs returns per-file main vs isolate previews.
func (a *App) GetCodingWorkbenchConflictDiffs(projectPath, conflictID string) []codingConflictFileDiff {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil || handler == nil {
		return nil
	}
	diffs, _, derr := handler.getCodingConflictFileDiffs(userID, conflictID, 30)
	if derr != nil {
		return nil
	}
	return diffs
}

// AdoptCodingWorkbenchConflict adopts all or selected files from a conflict.
// filesCSV is comma-separated relative paths; empty means all files.
func (a *App) AdoptCodingWorkbenchConflict(projectPath, conflictID, filesCSV string) (string, error) {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return "", err
	}
	var files []string
	for _, f := range strings.Split(filesCSV, ",") {
		f = strings.TrimSpace(f)
		if f != "" {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return handler.adoptCodingWorkbenchConflict(userID, conflictID)
	}
	return handler.adoptCodingWorkbenchConflictFiles(userID, conflictID, files)
}

// DiscardCodingWorkbenchConflict drops a kept isolation conflict.
func (a *App) DiscardCodingWorkbenchConflict(projectPath, conflictID string) (string, error) {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return "", err
	}
	return handler.discardCodingWorkbenchConflict(userID, conflictID)
}

// DiscardAllCodingWorkbenchConflicts drops every kept isolation conflict for the task.
func (a *App) DiscardAllCodingWorkbenchConflicts(projectPath string) (string, error) {
	handler, userID, err := a.codingWorkbenchHandlerForProject(projectPath)
	if err != nil {
		return "", err
	}
	return handler.discardAllStickyCodingConflicts(userID)
}

// codingWorkbenchHandlerForProject returns the IM handler + session owner for a task path.
func (a *App) codingWorkbenchHandlerForProject(projectPath string) (*IMMessageHandler, string, error) {
	if a == nil {
		return nil, "", fmt.Errorf("app unavailable")
	}
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return nil, "", fmt.Errorf("project path is required")
	}
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return nil, "", fmt.Errorf("AI assistant not initialized")
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil {
		return nil, "", fmt.Errorf("message handler unavailable")
	}
	return handler, projectSessionOwnerID(projectPath), nil
}

// SaveCurrentChatAsTask saves the current main assistant conversation as a
// task-management entry that can be reopened in a project tab later.
func (a *App) SaveCurrentChatAsTask(name string) ProjectSearchResult {
	taskName := normalizeRecentTaskName(name)
	if taskName == "" {
		taskName = a.SuggestCurrentTaskName()
	}
	if taskName == "" {
		taskName = "Saved task"
	}
	content := fmt.Sprintf("# %s\n\nSaved from current main assistant conversation.\nSaved at: %s", recentTaskDisplayTitle(taskName), time.Now().Format(time.RFC3339))
	workingDir := a.currentTaskManagementWorkingDir()
	created := a.createTaskRecordWithWorkingDir(taskName, content, []string{taskManagementTag, taskUserSavedTag, taskSavedConversationTag}, workingDir, false)
	if created.ProjectPath == "" {
		return created
	}
	a.saveCurrentConversationForTask(created.ProjectPath, workingDir)
	return created
}

func (a *App) saveCurrentConversationForTask(taskProjectPath, executionProjectPath string) {
	taskProjectPath = normalizeProjectSessionPath(taskProjectPath)
	executionProjectPath = normalizeProjectSessionPath(executionProjectPath)
	if taskProjectPath == "" {
		return
	}
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil || handler.memory == nil {
		return
	}
	sourceEntries := handler.memory.Load(desktopUserID)
	if len(sourceEntries) == 0 {
		return
	}
	handler.memory.Save(projectSessionOwnerID(taskProjectPath), sourceEntries)
	log.Printf("[project_search] SaveCurrentChatAsTask copied entries=%d task=%q execution_project=%q", len(sourceEntries), taskProjectPath, executionProjectPath)
}

// SuggestCurrentTaskName derives a short default name from the current main
// assistant conversation. The frontend lets the user edit this before saving.
func (a *App) SuggestCurrentTaskName() string {
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return "Saved task"
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil || handler.memory == nil {
		return "Saved task"
	}
	entries := handler.memory.Load(desktopUserID)
	for _, entry := range entries {
		role := strings.TrimSpace(strings.ToLower(entry.Role))
		if role != "user" {
			continue
		}
		content := stringifyProjectConversationContent(entry.Content)
		if content == "" {
			continue
		}
		return recentTaskDisplayTitle(content)
	}
	return "Saved task"
}

// ResumeTask keeps a task-management named API for callers that should not
// depend on the older project-search terminology.
func (a *App) ResumeTask(taskPath string) string {
	return a.ResumeProject(taskPath)
}

func (a *App) currentTaskManagementWorkingDir() string {
	if a == nil {
		return ""
	}
	if workflowDir := normalizeRecentTaskWorkingDir(a.GetWorkflowWorkingDir()); workflowDir != "" {
		return workflowDir
	}
	// Prefer an explicit top-bar working_directory when the user customized it.
	// If still on the system default workspace, fall back to CurrentProject so
	// Projects-list users keep a meaningful task working dir without having set
	// working_directory. Agent/workflow "项目路径" still uses EffectiveWorkingDirForOwner.
	desktopDir := normalizeRecentTaskWorkingDir(a.EffectiveDesktopWorkingDir())
	defaultWS := normalizeRecentTaskWorkingDir(corelib.WorkspaceDir())
	if desktopDir != "" && !strings.EqualFold(desktopDir, defaultWS) {
		return desktopDir
	}
	if projectPath := normalizeRecentTaskWorkingDir(a.GetCurrentProjectPath()); projectPath != "" {
		return projectPath
	}
	return desktopDir
}

// ForkRecentTask returns the independent task/session for a managed task. The
// first open creates that session; later opens of the same source return the
// same visible fork instead of creating duplicate task rows.
func (a *App) ForkRecentTask(sourceProjectPath string) ProjectSearchResult {
	started := time.Now()
	sourceProjectPath = strings.TrimSpace(sourceProjectPath)
	if sourceProjectPath == "" {
		return ProjectSearchResult{}
	}

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
			if rec := pi.Get(sourceProjectPath); rec != nil && projectRecordHasTag(*rec, taskForkedTag) {
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

		workingDir := a.recentTaskWorkingDir(sourceProjectPath)
		content := fmt.Sprintf("# %s\n\nOpened from task management.\nSource task: %s\nFork ID: %d", taskName, sourceProjectPath, time.Now().UnixNano())
		created := a.createTaskRecordWithWorkingDir(taskName, content, []string{taskForkedTag, taskSourceTagPrefix + sourceProjectPath}, workingDir, false)
		if created.ProjectPath == "" {
			log.Printf("[project_search] ForkRecentTask create failed source=%q elapsed=%s", sourceProjectPath, time.Since(started).Round(time.Millisecond))
			return created
		}
		copyStarted := time.Now()
		func(source, target string) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[project_search] ForkRecentTask sync copy panic source=%q fork=%q panic=%v elapsed=%s", source, target, r, time.Since(copyStarted).Round(time.Millisecond))
				}
			}()
			a.copyProjectConversation(source, target)
			log.Printf("[project_search] ForkRecentTask sync copy complete source=%q fork=%q elapsed=%s", source, target, time.Since(copyStarted).Round(time.Millisecond))
		}(sourceProjectPath, created.ProjectPath)
		a.emitProjectIndexChanged(sourceProjectPath)
		a.emitProjectIndexChanged(created.ProjectPath)
		log.Printf("[project_search] ForkRecentTask created independent fork source=%q fork=%q critical_elapsed=%s lock_elapsed=%s", sourceProjectPath, created.ProjectPath, time.Since(started).Round(time.Millisecond), time.Since(lockStarted).Round(time.Millisecond))
		return created
	}()
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
		if !projectRecordHasTag(rec, taskForkedTag) {
			continue
		}
		if pi.IsHidden(rec.ProjectPath) || pi.IsArchived(rec.ProjectPath) {
			continue
		}
		for _, tag := range rec.Tags {
			if strings.HasPrefix(tag, taskSourceTagPrefix) && normalizeRecentTaskPathKey(strings.TrimPrefix(tag, taskSourceTagPrefix)) == sourceKey {
				clone := rec
				return &clone
			}
		}
	}
	return nil
}

const recentTaskWorkingDirTagPrefix = "working_dir:"

func normalizeRecentTaskWorkingDir(workingDir string) string {
	workingDir = strings.TrimSpace(workingDir)
	if workingDir == "" {
		return ""
	}
	normalized, _, err := normalizeWorkflowProjectPath(workingDir)
	if err != nil {
		log.Printf("[project_search] invalid task working directory %q: %v", workingDir, err)
		return ""
	}
	if !filepath.IsAbs(normalized) {
		log.Printf("[project_search] invalid task working directory %q: path must be absolute", workingDir)
		return ""
	}
	return normalizeProjectSessionPath(normalized)
}

func recentTaskWorkingDirTag(workingDir string) string {
	workingDir = normalizeRecentTaskWorkingDir(workingDir)
	if workingDir == "" {
		return ""
	}
	return recentTaskWorkingDirTagPrefix + workingDir
}

func recentTaskWorkingDirFromTags(tags []string) string {
	for _, tag := range tags {
		if value := strings.TrimSpace(strings.TrimPrefix(tag, recentTaskWorkingDirTagPrefix)); value != tag {
			return normalizeRecentTaskWorkingDir(value)
		}
	}
	return ""
}

func (a *App) recentTaskWorkingDir(projectPath string) string {
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return ""
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
	return recentTaskWorkingDirFromTags(rec.Tags)
}

func (a *App) recentTaskExecutionProjectPath(projectPath string) string {
	projectPath = normalizeProjectSessionPath(projectPath)
	// Priority 1: in-memory override (set immediately by SetTabWorkingDir UI action).
	// This ensures the new directory takes effect on the next agent loop without
	// waiting for memory flush + ProjectIndex rebuild.
	if override, ok := a.tabWorkingDirOverrides.Load(projectPath); ok {
		if dir, _ := override.(string); dir != "" {
			return dir
		}
	}
	// Priority 2: persistent workingDir tag from memory store.
	if workingDir := a.recentTaskWorkingDir(projectPath); workingDir != "" {
		return workingDir
	}
	// Priority 3: for managed task directories, use workspace/ subdirectory
	// to isolate tool outputs from task metadata (task.md, conversation).
	// No custom working_dir here (priority 2 already returned if set).
	if a.isManagedRecentTaskWorkspacePath(projectPath) {
		_ = ensureManagedTaskWorkspaceDir(projectPath, lastPathComponent(projectPath), "")
		return filepath.Join(projectPath, "workspace")
	}
	return projectPath
}

func (a *App) ensureRecentTaskExecutionWorkingDir(taskProjectPath, executionProjectPath string) error {
	taskProjectPath = normalizeProjectSessionPath(taskProjectPath)
	executionProjectPath, created, err := ensureAbsoluteDirectoryPath(executionProjectPath, "task working directory")
	if err != nil {
		return err
	}
	if taskProjectPath == "" || executionProjectPath == "" || taskProjectPath == executionProjectPath {
		return nil
	}
	if created {
		log.Printf("[project_search] created task working directory task=%q working_dir=%q", taskProjectPath, executionProjectPath)
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

func isTaskManagementRecord(rec memory.ProjectRecord) bool {
	if projectRecordHasTag(rec, taskForkedTag) {
		return false
	}
	if projectRecordHasTag(rec, taskManagementTag) ||
		projectRecordHasTag(rec, taskUserCreatedTag) ||
		projectRecordHasTag(rec, taskUserSavedTag) {
		return true
	}
	// Backward-compatible display for tasks the user explicitly created before
	// task-management tags existed.
	return projectRecordHasTag(rec, taskLegacyManualTag) && projectRecordHasTag(rec, taskLegacyRecentTag)
}

func projectWorkflowProjectPathForRecord(rec memory.ProjectRecord) string {
	if !projectRecordHasTag(rec, taskForkedTag) {
		return rec.ProjectPath
	}
	for _, tag := range rec.Tags {
		if strings.HasPrefix(tag, taskSourceTagPrefix) {
			if source := strings.TrimSpace(strings.TrimPrefix(tag, taskSourceTagPrefix)); source != "" {
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
	if a == nil || projectPath == "" {
		return nil
	}
	lookup := func(path string) *ProjectWorkflowState {
		path = normalizeProjectSessionPath(path)
		if path == "" {
			return nil
		}
		ownerID := projectSessionOwnerID(path)
		var terminalV2 *ProjectWorkflowState
		if a.workflowV2 != nil && a.workflowV2.store != nil {
			// The task list is a recent-work snapshot, so terminal V2 workflows
			// belong here too. GetActive intentionally hides them from runtime
			// routing, whereas the store keeps the latest state for this task.
			if state, err := a.workflowV2.store.Load(ownerID); err == nil && state != nil {
				snapshot := projectWorkflowStateFromV2(state)
				if state.Status == v2.StatusActive {
					return snapshot
				}
				terminalV2 = snapshot
			}
		}
		if a.workflowEngine != nil {
			if state := a.workflowEngine.GetActiveWorkflow(ownerID); state != nil {
				return &ProjectWorkflowState{
					ID:            state.ID,
					Type:          string(state.Type),
					Phase:         state.CurrentPhase,
					Status:        string(state.Status),
					ProjectPath:   strings.TrimSpace(state.ProjectPath),
					PendingReview: strings.TrimSpace(state.PendingReviewPhaseID) != "",
				}
			}
		}
		return terminalV2
	}
	if state := lookup(projectPath); state != nil {
		return state
	}
	if executionProjectPath := a.recentTaskExecutionProjectPath(projectPath); executionProjectPath != "" && executionProjectPath != projectPath {
		return lookup(executionProjectPath)
	}
	if a.workflowDisabled.Load() {
		return nil
	}
	return nil
}

func projectWorkflowStateFromV2(state *v2.WorkflowState) *ProjectWorkflowState {
	if state == nil {
		return nil
	}
	phaseID := ""
	if phase := state.ActivePhase(); phase != nil {
		phaseID = phase.ID
	} else if len(state.Phases) > 0 && state.Status != v2.StatusActive {
		// Terminal workflows advance beyond their final phase, so ActivePhase is
		// nil. Preserve that last phase as useful context in the recent task list.
		phaseID = state.Phases[len(state.Phases)-1].ID
	}
	return &ProjectWorkflowState{
		ID:          state.ID,
		Type:        state.Type,
		ProjectPath: state.ProjectPath,
		Phase:       phaseID,
		Status:      string(state.Status),
		// A cancelled workflow can retain its phase's waiting-confirm status in
		// storage. Terminal status takes precedence over a stale review marker.
		PendingReview: state.Status == v2.StatusActive && state.IsWaitingConfirm(),
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
		log.Printf("[project_search] repair managed task workspace failed project=%q err=%v", projectPath, err)
		return false
	}
	name := normalizeRecentTaskName(taskName)
	if name == "" {
		name = lastPathComponent(projectPath)
	}
	taskFile := filepath.Join(projectPath, "task.md")
	if _, err := os.Stat(taskFile); os.IsNotExist(err) {
		content := fmt.Sprintf("# %s\n\nRecovered task workspace.\nProject path: %s\n", name, projectPath)
		if writeErr := os.WriteFile(taskFile, []byte(content), 0o644); writeErr != nil {
			log.Printf("[project_search] repair managed task task.md failed project=%q err=%v", projectPath, writeErr)
		}
	}
	log.Printf("[project_search] repaired managed task workspace project=%q", projectPath)
	return true
}

func (a *App) createTaskRecord(taskName, taskContent string, extraTags []string, flushSync ...bool) ProjectSearchResult {
	return a.createTaskRecordWithWorkingDir(taskName, taskContent, extraTags, "", flushSync...)
}

func (a *App) createTaskRecordWithWorkingDir(taskName, taskContent string, extraTags []string, workingDir string, flushSync ...bool) ProjectSearchResult {
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return ProjectSearchResult{}
	}

	displayTitle := recentTaskDisplayTitle(taskName)
	now := time.Now()
	taskDir := filepath.Join(a.GetDataDir(), "tasks", fmt.Sprintf("%s-%d", recentTaskSlug(displayTitle), now.UnixNano()))
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		log.Printf("[project_search] CreateRecentTask mkdir failed: %v", err)
		return ProjectSearchResult{}
	}
	// Prepare workspace/ at create-time so the path bar shows a real execution
	// directory immediately (and default sandbox is self-explanatory when empty).
	if err := ensureManagedTaskWorkspaceDir(taskDir, displayTitle, workingDir); err != nil {
		log.Printf("[project_search] CreateRecentTask workspace prepare failed task=%q err=%v", taskDir, err)
	}
	taskFile := filepath.Join(taskDir, "task.md")
	if strings.TrimSpace(taskContent) == "" {
		taskContent = fmt.Sprintf("# %s\n\n%s\n\nCreated from task management.\nTask ID: %d", displayTitle, taskName, now.UnixNano())
	}
	if workingDir != "" && !strings.Contains(taskContent, "\nWorking directory:") {
		taskContent += "\nWorking directory: " + workingDir
	}
	if err := os.WriteFile(taskFile, []byte(taskContent), 0o644); err != nil {
		log.Printf("[project_search] CreateRecentTask write task file failed: %v", err)
		return ProjectSearchResult{}
	}
	tags := append([]string{taskLegacyManualTag, taskLegacyRecentTag, taskDir}, extraTags...)
	if workingDir != "" {
		tags = append(tags, recentTaskWorkingDirTag(workingDir))
	}

	_, err := a.memoryStore.UpsertTaskArtifact(memory.TaskArtifactUpsertOptions{
		Title:            displayTitle,
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
		return ProjectSearchResult{ID: taskDir, Name: displayTitle, ProjectPath: taskDir, WorkingDir: workingDir, LastActivity: now.Format(time.RFC3339), EntryCount: 1, HasOutput: true}
	}
	if rec := pi.Get(taskDir); rec != nil {
		return projectRecordToSearchResult(pi, *rec)
	}
	return ProjectSearchResult{ID: taskDir, Name: displayTitle, ProjectPath: taskDir, WorkingDir: workingDir, LastActivity: now.Format(time.RFC3339), EntryCount: 1, HasOutput: true}
}

func (a *App) copyProjectConversation(sourceProjectPath, targetProjectPath string) {
	started := time.Now()
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
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

func stringifyProjectConversationContent(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case []byte:
		return strings.TrimSpace(string(v))
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return strings.TrimSpace(fmt.Sprint(v))
		}
		return strings.TrimSpace(string(data))
	}
}

// LoadProjectConversationHistory returns the visible conversation history for a
// project-scoped task session. This is used by the frontend when reopening a
// managed task fork so the chat transcript can be restored from the backend
// conversation memory that ForkRecentTask already copied.
func (a *App) LoadProjectConversationHistory(projectPath string) []ProjectConversationHistoryItem {
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return []ProjectConversationHistoryItem{}
	}
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return []ProjectConversationHistoryItem{}
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil || handler.memory == nil {
		return []ProjectConversationHistoryItem{}
	}
	entries := handler.memory.Load(projectSessionOwnerID(projectPath))
	if len(entries) == 0 {
		return []ProjectConversationHistoryItem{}
	}
	items := make([]ProjectConversationHistoryItem, 0, len(entries))
	for _, entry := range entries {
		role := strings.TrimSpace(strings.ToLower(entry.Role))
		switch role {
		case "user", "assistant", "system", "error":
		default:
			continue
		}
		content := stringifyProjectConversationContent(entry.Content)
		reasoning := strings.TrimSpace(entry.ReasoningContent)
		if content == "" && reasoning == "" {
			continue
		}
		items = append(items, ProjectConversationHistoryItem{
			Role:             role,
			Content:          content,
			ReasoningContent: reasoning,
		})
	}
	return items
}

func (a *App) emitProjectIndexChanged(projectPath string) {
	if a.ctx == nil {
		return
	}
	a.emitEvent(EventProjectIndexChanged, projectPath)
	a.emitEvent(EventTasksChanged, nil)
}

func (a *App) emitProjectTaskClosed(projectPath string) {
	if a.ctx == nil {
		return
	}
	a.emitEvent(EventProjectTaskClosed, projectPath)
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
	hubClient := a.ensureHubClient()
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
	hubClient := a.ensureHubClient()
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

// recentTaskDisplayTitle extracts a short display title from a multi-line task command.
// Uses the first non-empty line, truncated to 80 rune for sidebar display.
func recentTaskDisplayTitle(fullCommand string) string {
	for _, line := range strings.Split(fullCommand, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			runes := []rune(trimmed)
			if len(runes) > 80 {
				return string(runes[:80]) + "…"
			}
			return trimmed
		}
	}
	return fullCommand
}

// recentTaskSlug builds a filesystem-safe directory slug from a task title.
// Keeps ASCII letters/digits and Unicode letters/numbers (so Chinese titles stay
// recognizable). Maps whitespace/punctuation to '-' and strips Windows-invalid
// path characters. Caps length by runes (not bytes) so CJK is not over-truncated.
func recentTaskSlug(name string) string {
	const maxRunes = 40
	var b strings.Builder
	lastWasSeparator := false
	runeCount := 0
	for _, r := range strings.TrimSpace(name) {
		// Normalize ASCII case only; keep CJK and other letters as-is.
		if r >= 'A' && r <= 'Z' {
			r = r - 'A' + 'a'
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', unicode.IsLetter(r), unicode.IsNumber(r):
			// Reject Windows-illegal filename characters even if classified oddly.
			if r == '<' || r == '>' || r == ':' || r == '"' || r == '/' || r == '\\' || r == '|' || r == '?' || r == '*' {
				if b.Len() > 0 && !lastWasSeparator {
					b.WriteByte('-')
					lastWasSeparator = true
				}
				continue
			}
			b.WriteRune(r)
			runeCount++
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
		if runeCount >= maxRunes {
			break
		}
	}
	slug := strings.Trim(b.String(), "-_")
	if slug == "" {
		return "task"
	}
	// Windows device names cannot be directory names (CON, PRN, AUX, NUL, COMn, LPTn).
	if isWindowsReservedDeviceName(slug) {
		return slug + "-task"
	}
	return slug
}

func isWindowsReservedDeviceName(name string) bool {
	base := strings.ToUpper(strings.TrimSpace(name))
	// Allow "com1-foo" style; only exact reserved tokens (optionally with extension).
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch base {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	default:
		return false
	}
}

// ensureManagedTaskWorkspaceDir creates taskDir/workspace and, when no custom
// working directory was chosen, seeds a short README so the empty sandbox is
// obvious in Explorer and to the agent.
func ensureManagedTaskWorkspaceDir(taskDir, displayTitle, customWorkingDir string) error {
	wsDir := filepath.Join(taskDir, "workspace")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		return err
	}
	if strings.TrimSpace(customWorkingDir) != "" {
		return nil
	}
	readme := filepath.Join(wsDir, "README.md")
	if _, err := os.Stat(readme); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	title := strings.TrimSpace(displayTitle)
	if title == "" {
		title = "task"
	}
	content := fmt.Sprintf(`# %s — 任务工作区

这是托管任务的**默认执行目录**（空沙箱）。

- 工具相对路径、本目录下的搜索都只看到这里的内容。
- 若要处理本机已有项目/标书材料，请在创建任务时选择「任务目录」，或使用**绝对路径**调用 office(read_document) 等工具。
- 任务元数据在上一级目录的 task.md。

Task folder: %s
`, title, taskDir)
	return os.WriteFile(readme, []byte(content), 0o644)
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
		a.emitEvent("config-changed", *updatedCfg)
	}

	// 1. Cancel any active V2 workflow (user is switching projects).
	if a.workflowV2 != nil {
		if state := a.workflowV2.machine.GetActive(userID); state != nil {
			a.workflowV2.machine.Cancel(userID)
		}
	}

	// 2. Clear conversation memory AND per-user session state (drift detector,
	//    orchestrator, steering context, etc.). This mirrors what /new does.
	//    We do this on the backend so the frontend's subsequent clearHistory()
	//    is a no-op (idempotent Clear on already-empty memory).
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
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

	msg := "已切换到任务：" + projectName
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
		msg += "\n" + projectPath
	}
	return msg
}

// RenameTask sets a user-defined display name for a task-management item.
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

// PinTask pins or unpins a task-management item.
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

// HideTask removes a task from task management (soft delete).
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
		return fmt.Sprintf("已恢复项目会话：%s", projectName)
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

	return fmt.Sprintf("已打开项目：%s\n%s\n\n请问需要我做什么？", projectName, projectPath)
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

	contextPath := a.recentTaskExecutionProjectPath(projectPath)
	if contextPath == "" {
		contextPath = projectPath
	}
	contextData := a.memoryStore.ProjectContextForHost(contextPath, 1)
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
	sb.WriteString(fmt.Sprintf("项目：%s\n路径：%s\n\n", projectName, projectPath))

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
		// Clean up working directory override — prevents stale overrides from
		// leaking into future sessions that reuse the same projectPath.
		a.tabWorkingDirOverrides.Delete(normalizeProjectSessionPath(projectPath))
		a.cancelProjectTaskLoop(projectPath)
		if workingDir := a.recentTaskWorkingDir(projectPath); workingDir != "" && !strings.EqualFold(workingDir, projectPath) {
			a.cancelProjectTaskLoop(workingDir)
		}
	}
	log.Printf("[CloseProjectTabSession] tab=%s closed project=%q", tabID, projectPath)
}

// SetTabWorkingDir changes the effective working directory for a project tab.
// Called immediately from the frontend directory switcher UI. The new directory
// takes effect on the next agent loop via the in-memory tabWorkingDirOverrides
// cache (no delay waiting for memory flush).
//
// For the Local Tab, pass tabID="" and the change updates config.WorkingDirectory.
// Active workflow state.ProjectPath for the matching session owner is also synced
// so phase prompts ("项目路径") stay aligned with the top bar.
// This is a Wails binding method.
func (a *App) SetTabWorkingDir(tabID, newDir string) error {
	newDir = strings.TrimSpace(newDir)
	if newDir == "" {
		return fmt.Errorf("directory path is empty")
	}
	newDir = filepath.Clean(newDir)

	// Validate: must be an absolute path.
	if !filepath.IsAbs(newDir) {
		return fmt.Errorf("directory path must be absolute: %s", newDir)
	}

	// Create if not exists.
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		return fmt.Errorf("cannot create directory %s: %w", newDir, err)
	}

	tabID = strings.TrimSpace(tabID)

	// Local Tab: update global config.
	if tabID == "" {
		corelib.SetWorkspaceDir(newDir)
		_, _ = a.PatchConfigFields(map[string]interface{}{"working_directory": newDir})
		a.syncActiveWorkflowWorkingDir(desktopUserID, newDir)
		log.Printf("[SetTabWorkingDir] local tab working dir updated to %q", newDir)
		return nil
	}

	// Project Tab: resolve projectPath from tabID, then write override.
	projectPath := ""
	if cached, ok := a.tabProjectPaths.Load(tabID); ok {
		projectPath, _ = cached.(string)
	}
	if projectPath == "" {
		return fmt.Errorf("tab %s has no associated project path", tabID)
	}
	projectPath = normalizeProjectSessionPath(projectPath)

	// Write to in-memory cache — takes effect immediately on next agent loop.
	a.tabWorkingDirOverrides.Store(projectPath, newDir)

	// Keep any active workflow for this project tab on the same directory.
	a.syncActiveWorkflowWorkingDir(projectSessionOwnerID(projectPath), newDir)

	// Persist to task record asynchronously (update workingDir tag).
	go func() {
		if err := a.persistTaskWorkingDir(projectPath, newDir); err != nil {
			log.Printf("[SetTabWorkingDir] persist failed tab=%s project=%q dir=%q err=%v", tabID, projectPath, newDir, err)
		}
	}()

	log.Printf("[SetTabWorkingDir] tab=%s project=%q dir=%q (immediate + async persist)", tabID, projectPath, newDir)
	return nil
}

// syncActiveWorkflowWorkingDir updates the ProjectPath of an active workflow for
// ownerID so workflow phase prompts match the top-bar / tool working directory.
// No-op when there is no active workflow for that owner.
// Emits at most one workflow:workdir_set event per call.
func (a *App) syncActiveWorkflowWorkingDir(ownerID, dir string) {
	if a == nil {
		return
	}
	ownerID = strings.TrimSpace(ownerID)
	dir = normalizeProjectSessionPath(dir)
	if ownerID == "" || dir == "" {
		return
	}

	updated := false
	now := time.Now()

	if a.workflowV2 != nil && a.workflowV2.machine != nil {
		if state := a.workflowV2.machine.GetActive(ownerID); state != nil {
			if normalizeProjectSessionPath(state.ProjectPath) != dir {
				state.ProjectPath = dir
				state.UpdatedAt = now
				updated = true
				if a.workflowV2.store != nil {
					if err := a.workflowV2.store.Save(state); err != nil {
						log.Printf("[syncActiveWorkflowWorkingDir] persist v2 project path owner=%s dir=%q err=%v", ownerID, dir, err)
					}
				}
			}
		}
	}
	if a.workflowEngine != nil {
		if state := a.workflowEngine.GetActiveWorkflow(ownerID); state != nil {
			if normalizeProjectSessionPath(state.ProjectPath) != dir {
				state.ProjectPath = dir
				state.UpdatedAt = now
				updated = true
			}
		}
		// Keep adapter cache in sync without double-emitting workflow:workdir_set.
		if adapter, ok := a.workflowEngine.GetCallbacks().(*GUIWorkflowAdapter); ok && adapter != nil {
			if normalizeProjectSessionPath(adapter.GetWorkingDir()) != dir {
				if updated {
					adapter.setWorkingDirCache(dir)
				} else {
					adapter.SetWorkingDir(ownerID, dir)
				}
			}
		}
	}

	if updated && a.ctx != nil {
		a.emitEvent("workflow:workdir_set", map[string]string{
			"user_id": ownerID,
			"path":    dir,
		})
	}
}

// EffectiveWorkingDirForOwner returns the working directory that tools, system
// prompts, workflow phase headers ("项目路径"), and the ProjectDirBar all share
// for a given session owner.
//
// Resolution order (single source of truth):
//  1. Project-tab owner (desktop-user:{projectPath}) → recentTaskExecutionProjectPath
//     (honors SetTabWorkingDir overrides and managed-task workspace/)
//  2. Local / unknown owner → corelib.EffectiveWorkspaceDir()
//     (config.working_directory / top-bar local path — NOT config.Projects)
//
// GetCurrentProjectPath (Projects list / CurrentProject) is intentionally NOT
// used here: it is a separate "configured project" concept and diverging from
// the top-bar working directory is the root cause of agents listing the wrong tree.
func (a *App) EffectiveWorkingDirForOwner(ownerID string) string {
	if projectPath := projectPathFromSessionOwnerID(ownerID); projectPath != "" {
		if a != nil {
			if dir := strings.TrimSpace(a.recentTaskExecutionProjectPath(projectPath)); dir != "" {
				return normalizeProjectSessionPath(dir)
			}
		}
		return projectPath
	}
	return normalizeProjectSessionPath(corelib.EffectiveWorkspaceDir())
}

// EffectiveDesktopWorkingDir is the local-tab working directory shown in
// ProjectDirBar and used as tool cwd when the session owner has no project path.
func (a *App) EffectiveDesktopWorkingDir() string {
	return a.EffectiveWorkingDirForOwner(desktopUserID)
}

// GetTabWorkingDir returns the current effective working directory for a tab.
// Returns the directory path and whether it is a system default (not user-specified).
// This is a Wails binding method.
func (a *App) GetTabWorkingDir(tabID string) map[string]interface{} {
	tabID = strings.TrimSpace(tabID)

	var dir string
	isDefault := true

	if tabID == "" {
		// Local Tab: use global EffectiveWorkspaceDir.
		dir = a.EffectiveDesktopWorkingDir()
		// Normalize both sides so Windows drive-letter casing does not flip the badge.
		isDefault = normalizeProjectSessionPath(dir) == normalizeProjectSessionPath(corelib.WorkspaceDir())
	} else {
		// Project Tab: resolve via the same chain that tools use.
		projectPath := ""
		if cached, ok := a.tabProjectPaths.Load(tabID); ok {
			projectPath, _ = cached.(string)
		}
		if projectPath != "" {
			projectPath = normalizeProjectSessionPath(projectPath)
			dir = a.EffectiveWorkingDirForOwner(projectSessionOwnerID(projectPath))
			// It's "default" if it's a system-managed workspace path (not user-specified).
			isDefault = a.isManagedRecentTaskWorkspacePath(dir) ||
				(a.isManagedRecentTaskWorkspacePath(projectPath) && dir == filepath.Join(projectPath, "workspace"))
		}
	}

	if dir == "" {
		dir = a.EffectiveDesktopWorkingDir()
	}

	return map[string]interface{}{
		"path":       dir,
		"is_default": isDefault,
	}
}

// persistTaskWorkingDir updates the persistent workingDir tag for a task.
func (a *App) persistTaskWorkingDir(projectPath, newDir string) error {
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return fmt.Errorf("memory store unavailable")
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return fmt.Errorf("project index unavailable")
	}
	rec := pi.Get(projectPath)
	if rec == nil {
		return fmt.Errorf("project record not found for %s", projectPath)
	}

	// Remove old workingDir tag and add new one.
	newTag := recentTaskWorkingDirTag(newDir)
	var updatedTags []string
	for _, tag := range rec.Tags {
		if !strings.HasPrefix(tag, recentTaskWorkingDirTagPrefix) {
			updatedTags = append(updatedTags, tag)
		}
	}
	updatedTags = append(updatedTags, newTag)

	// UpsertTaskArtifact requires non-empty Content (empty = no-op).
	// Re-use the existing record's content to trigger an update.
	content := strings.TrimSpace(rec.Name)
	if content == "" {
		content = "task working directory updated"
	}

	_, err := a.memoryStore.UpsertTaskArtifact(memory.TaskArtifactUpsertOptions{
		Title:            rec.Name,
		Content:          content,
		Tags:             updatedTags,
		IdentityTagCount: 3,
		SourceType:       "working_dir_update",
	})
	if err != nil {
		return err
	}
	return a.memoryStore.Flush()
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
	executionProjectPath := a.recentTaskExecutionProjectPath(projectPath)
	log.Printf("[SendMessageForTab] route tab=%q project=%q execution_project=%q text_len=%d", tabID, projectPath, executionProjectPath, len(trimmedText))

	// Delegate to the existing SendAIAssistantMessage with project_path set.
	// This auto-synthesizes per-project userID (desktop-user:{projectPath})
	// and all downstream components isolate by userID.
	return a.SendAIAssistantMessage(AIAssistantSendRequest{
		Text:         text,
		ProjectPath:  projectPath,
		EventScopeID: tabID,
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

// SaveProjectTabConversation persists the conversation history for a project tab
// to the backend session file. Called from frontend on debounced history changes.
// Pass an empty slice to clear persisted conversation.
// This is a Wails binding method.
func (a *App) SaveProjectTabConversation(tabID string, conversation []interface{}) {
	tabID = strings.TrimSpace(tabID)
	if tabID == "" {
		return
	}
	persist := a.ensureProjectTabSessionPersist()
	session, err := persist.LoadSession(tabID)
	if err != nil || session == nil {
		if len(conversation) == 0 {
			return // Nothing to clear if session doesn't exist
		}
		// Session file doesn't exist yet — create it with conversation.
		session = &TabSessionData{
			TabID:        tabID,
			Conversation: conversation,
			CreatedAt:    time.Now().UTC().Format(time.RFC3339),
		}
		if cached, ok := a.tabProjectPaths.Load(tabID); ok {
			session.ProjectPath, _ = cached.(string)
		}
	} else {
		session.Conversation = conversation
	}
	if err := persist.SaveSession(session); err != nil {
		log.Printf("[SaveProjectTabConversation] tab=%s err=%v", tabID, err)
	}
}

// LoadProjectTabConversation loads persisted conversation history for a tab
// from the backend session file. Returns nil if no data exists.
// This is a Wails binding method.
func (a *App) LoadProjectTabConversation(tabID string) []interface{} {
	tabID = strings.TrimSpace(tabID)
	if tabID == "" {
		return nil
	}
	persist := a.ensureProjectTabSessionPersist()
	session, err := persist.LoadSession(tabID)
	if err != nil || session == nil {
		return nil
	}
	if len(session.Conversation) == 0 {
		return nil
	}
	return session.Conversation
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
	case v2.WorkflowCoding:
		return "编码"
	case v2.WorkflowProductDesign:
		return "产品设计"
	case v2.WorkflowPresentationDesign:
		return "PPT 设计"
	case v2.WorkflowInnovation:
		return "创新"
	case v2.WorkflowBusinessPlan:
		return "商业计划"
	case v2.WorkflowTesting:
		return "测试"
	case v2.WorkflowLiteratureReview:
		return "文献综述"
	case v2.WorkflowResearchReport:
		return "研究报告"
	case v2.WorkflowExperimentDesign:
		return "实验设计"
	case v2.WorkflowGrantProposal:
		return "基金申请"
	case v2.WorkflowPaperWriting:
		return "论文写作"
	case v2.WorkflowProjectProposal:
		return "项目提案"
	case v2.WorkflowEventPlanning:
		return "活动策划"
	case v2.WorkflowCompetitiveAnalysis:
		return "竞品分析"
	case v2.WorkflowBidResponse:
		return "招投标"
	case v2.WorkflowBidReview:
		return "标书检查"
	case v2.WorkflowContractReview:
		return "合同审查"
	case v2.WorkflowDueDiligence:
		return "尽职调查"
	case v2.WorkflowComplianceAudit:
		return "合规审计"
	case v2.WorkflowPatentAnalysis:
		return "专利分析"
	case v2.WorkflowPatentApplication:
		return "中国专利申请"
	case v2.WorkflowUSPatentApplication:
		return "美国专利申请"
	case v2.WorkflowGaokaoApplication:
		return "高考志愿填报"
	case v2.WorkflowPaperReproduction:
		return "论文复现"
	case v2.WorkflowMaintenance:
		return "运维"
	case v2.WorkflowChangjiangScholar:
		return "长江学者申报"
	case v2.WorkflowChangjiangScholarReview:
		return "长江学者评审"
	case v2.WorkflowNSFCDistinguishedYouth:
		return "杰青"
	case v2.WorkflowNSFCExcellentYouth:
		return "优青"
	case v2.WorkflowNSFCYouth:
		return "青基"
	case v2.WorkflowNSFCGeneral:
		return "面上"
	case v2.WorkflowNSFCKey:
		return "重点"
	default:
		return wfType
	}
}
