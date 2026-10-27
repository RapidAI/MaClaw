package guiapp

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// taskCopyTag marks a user-initiated copy. Cloud workspaces are otherwise
	// 1:1 with a sidebar row; this tag keeps the copy visible and stops it
	// from becoming the workspace's transcript owner.
	taskCopyTag = "task_copy"
	// taskCopyNamePrefix is the display prefix required for a copied task.
	taskCopyNamePrefix = "（副本）"
)

// DuplicateTask forks a task: the copy keeps the source configuration, its
// working directory, and the full conversation (chat transcript plus coding
// workbench continuity). The display name is "（副本）" plus the source name.
func (a *App) DuplicateTask(sourceProjectPath string) (ProjectSearchResult, error) {
	zero := ProjectSearchResult{}
	if a == nil {
		return zero, fmt.Errorf("app unavailable")
	}
	sourceProjectPath = normalizeProjectSessionPath(sourceProjectPath)
	if sourceProjectPath == "" {
		return zero, fmt.Errorf("task path is required")
	}
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return zero, fmt.Errorf("memory store not initialized")
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return zero, fmt.Errorf("task index unavailable")
	}
	rec := pi.Get(sourceProjectPath)
	if rec == nil || strings.TrimSpace(rec.ProjectPath) == "" {
		return zero, fmt.Errorf("task not found")
	}
	if pi.IsHidden(sourceProjectPath) || pi.IsArchived(sourceProjectPath) {
		return zero, fmt.Errorf("task not found")
	}

	sourceName := strings.TrimSpace(pi.GetDisplayName(sourceProjectPath))
	if sourceName == "" {
		sourceName = strings.TrimSpace(rec.Name)
	}
	if sourceName == "" {
		sourceName = stripTaskDirTimestampSuffix(lastPathComponent(sourceProjectPath))
	}
	name := duplicateTaskDisplayName(sourceName)
	workingDir := a.duplicateTaskWorkingDir(sourceProjectPath)
	created := a.createTaskRecordWithWorkingDir(name, "", taskConfigTagsForDuplicate(rec.Tags), workingDir, true)
	if strings.TrimSpace(created.ProjectPath) == "" {
		return zero, fmt.Errorf("task copy failed")
	}
	if renamed := strings.TrimSpace(a.RenameTask(created.ProjectPath, name)); renamed != "" {
		created.Name = renamed
	}
	if err := copyTaskCodingConfig(sourceProjectPath, created.ProjectPath, workingDir); err != nil {
		log.Printf("[task-copy] coding config copy failed source=%q copy=%q err=%v", sourceProjectPath, created.ProjectPath, err)
	}
	// The copy is a fork, not a blank task: the conversation must be available
	// the moment the new tab opens.
	a.copyProjectConversation(sourceProjectPath, created.ProjectPath)
	if updated := pi.Get(created.ProjectPath); updated != nil {
		created = a.projectRecordToSearchResult(pi, *updated)
	}
	log.Printf("[task-copy] created source=%q copy=%q name=%q", sourceProjectPath, created.ProjectPath, created.Name)
	return created, nil
}

// duplicateTaskWorkingDir is the directory the copy executes in.
// An explicit working_dir tag is copied as-is. A task with no tag runs in
// its own workspace/ sandbox; the copy must keep that same folder instead of
// starting an empty sandbox under the new task directory.
func (a *App) duplicateTaskWorkingDir(sourceProjectPath string) string {
	if dir := a.recentTaskWorkingDir(sourceProjectPath); dir != "" {
		return dir
	}
	return a.managedTaskExecutionDirForDisplay(sourceProjectPath)
}

func duplicateTaskDisplayName(sourceName string) string {
	sourceName = strings.TrimSpace(sourceName)
	if sourceName == "" {
		sourceName = "任务"
	}
	name := taskNameForCreate(taskCopyNamePrefix+sourceName, true)
	if name == "" {
		return taskCopyNamePrefix + "任务"
	}
	return name
}

// taskConfigTagsForDuplicate keeps the settings that define how a task runs
// and drops chat lineage, saved transcripts, and the source task directory.
func taskConfigTagsForDuplicate(tags []string) []string {
	out := make([]string, 0, len(tags)+3)
	seen := make(map[string]struct{}, len(tags)+3)
	add := func(tag string) {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return
		}
		if _, ok := seen[tag]; ok {
			return
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	for _, tag := range tags {
		if shouldCopyTaskConfigTag(tag) {
			add(tag)
		}
	}
	add(taskManagementTag)
	add(taskUserCreatedTag)
	add(taskCopyTag)
	return out
}

func shouldCopyTaskConfigTag(tag string) bool {
	tag = strings.TrimSpace(tag)
	if tag == "" || looksLikeAbsoluteProjectPathTag(tag) {
		return false
	}
	switch tag {
	case taskLegacyManualTag, taskLegacyRecentTag, taskForkedTag, taskSavedConversationTag:
		return false
	case taskManagementTag, taskUserCreatedTag, taskUserSavedTag,
		taskCodingDevTag, taskRemoteCodingDevTag:
		return true
	}
	if isRemoteCodingMetaTag(tag) {
		return true
	}
	if strings.HasPrefix(tag, recentTaskWorkingDirTagPrefix) {
		// Re-applied from the working-dir argument so the copy gets one tag.
		return false
	}
	if strings.HasPrefix(tag, cloudWorkspaceTagPrefix) || strings.HasPrefix(tag, latexEntryTagPrefix) {
		return true
	}
	// A shared workspace stays read-only or writable on the copy. Dropping the
	// read tag makes the copy request a write lease.
	if tag == cloudWorkspaceShareReadTagPrefix || tag == cloudWorkspaceShareWriteTagPrefix {
		return true
	}
	if owner, ok := strings.CutPrefix(tag, cloudWorkspaceSharedFromPrefix); ok {
		return strings.TrimSpace(owner) != ""
	}
	if rest, ok := strings.CutPrefix(tag, taskSourceTagPrefix); ok {
		switch {
		case strings.HasPrefix(rest, "expert:"):
			return strings.TrimSpace(strings.TrimPrefix(rest, "expert:")) != ""
		case rest == "coding_workflow", rest == "remote_ops_diagnosis":
			return true
		default:
			return false
		}
	}
	return false
}

func projectResultIsTaskCopy(result ProjectSearchResult) bool {
	for _, tag := range result.Tags {
		if strings.TrimSpace(tag) == taskCopyTag {
			return true
		}
	}
	return false
}

func (a *App) projectHasTaskCopyTag(projectPath string) bool {
	if a == nil || a.memoryStore == nil {
		return false
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return false
	}
	rec := pi.Get(normalizeProjectSessionPath(projectPath))
	return rec != nil && projectRecordHasTag(*rec, taskCopyTag)
}

// copyTaskCodingConfig copies workbench settings (permission, plan, route,
// worktree, remote target) and the conversation context a fork continues
// from. Ephemeral in-flight state (pending plan, checkpoints, live remote
// session id, token counters) stays with the source.
func copyTaskCodingConfig(sourceDir, targetDir, workingDir string) error {
	data, err := os.ReadFile(filepath.Join(sourceDir, stickyCodingMemoryFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	var mem stickyCodingWorkbenchMemory
	if err := json.Unmarshal(data, &mem); err != nil {
		return err
	}
	if !codingConfigWorthCopying(mem) {
		return nil
	}
	cfg := stickyCodingWorkbenchMemory{
		Kind:                      mem.Kind,
		ProjectPath:               retargetCopiedCodingProjectPath(sourceDir, targetDir, workingDir, mem.ProjectPath),
		LastUserText:              mem.LastUserText,
		LastSummary:               mem.LastSummary,
		SessionPlan:               mem.SessionPlan,
		ExecutionPlan:             mem.ExecutionPlan,
		RequirementRestatement:    mem.RequirementRestatement,
		FilesModified:             append([]string(nil), mem.FilesModified...),
		FilesCreated:              append([]string(nil), mem.FilesCreated...),
		TurnCount:                 mem.TurnCount,
		SessionFullAccess:         mem.SessionFullAccess,
		SessionHighRiskAccess:     mem.SessionHighRiskAccess,
		SessionPermissionMode:     mem.SessionPermissionMode,
		ApprovedDirs:              append([]string(nil), mem.ApprovedDirs...),
		PlanMode:                  mem.PlanMode,
		ProjectInstructions:       mem.ProjectInstructions,
		ProjectInstructionSources: append([]string(nil), mem.ProjectInstructionSources...),
		RoutePref:                 mem.RoutePref,
		WorktreeMode:              mem.WorktreeMode,
		RemoteWorkDir:             mem.RemoteWorkDir,
		RemoteProjectDir:          mem.RemoteProjectDir,
		RemoteHost:                mem.RemoteHost,
		RemoteUser:                mem.RemoteUser,
		RemotePort:                mem.RemotePort,
		UpdatedAtUnix:             time.Now().Unix(),
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(targetDir, stickyCodingMemoryFileName), encoded)
}

func codingConfigWorthCopying(mem stickyCodingWorkbenchMemory) bool {
	if strings.TrimSpace(mem.Kind) != "" || strings.TrimSpace(mem.RoutePref) != "" ||
		strings.TrimSpace(mem.PlanMode) != "" || strings.TrimSpace(mem.WorktreeMode) != "" ||
		strings.TrimSpace(mem.SessionPermissionMode) != "" {
		return true
	}
	if mem.SessionFullAccess || mem.SessionHighRiskAccess || len(mem.ApprovedDirs) > 0 {
		return true
	}
	// A fork keeps the conversation context even when no permission or route
	// settings were ever written.
	if strings.TrimSpace(mem.LastUserText) != "" || strings.TrimSpace(mem.LastSummary) != "" ||
		strings.TrimSpace(mem.SessionPlan) != "" || strings.TrimSpace(mem.ExecutionPlan) != "" ||
		strings.TrimSpace(mem.RequirementRestatement) != "" || strings.TrimSpace(mem.ProjectInstructions) != "" ||
		mem.TurnCount > 0 || len(mem.FilesModified) > 0 || len(mem.FilesCreated) > 0 {
		return true
	}
	return strings.TrimSpace(mem.RemoteHost) != "" || strings.TrimSpace(mem.RemoteWorkDir) != ""
}

func retargetCopiedCodingProjectPath(sourceTask, targetTask, workingDir, recorded string) string {
	recordedNorm := normalizeProjectSessionPath(recorded)
	if recordedNorm == "" || copiedPathWithin(sourceTask, recordedNorm) {
		if dir := normalizeProjectSessionPath(workingDir); dir != "" {
			return dir
		}
		return normalizeProjectSessionPath(targetTask)
	}
	return recordedNorm
}

func copiedPathWithin(parent, child string) bool {
	parent = normalizeProjectSessionPath(parent)
	child = normalizeProjectSessionPath(child)
	if parent == "" || child == "" {
		return false
	}
	if strings.EqualFold(parent, child) {
		return true
	}
	prefix := parent
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	return strings.HasPrefix(strings.ToLower(child), strings.ToLower(prefix))
}
