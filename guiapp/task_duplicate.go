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
	// taskCopyFilesUnavailable is returned when a read-only share became a
	// local task but its files could not be copied. The conversation is still
	// on the copy; the task list says the files are missing.
	taskCopyFilesUnavailable = "files_unavailable"
)

// DuplicateTask forks a task: the copy keeps the source configuration, its
// working directory, and the full conversation (chat transcript plus coding
// workbench continuity). The display name is "（副本）" plus the source name.
// A received read-only cloud share is the exception: the shared task stays
// the read-only view, and the copy is a local task with its own sandbox.
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
	readShare := cloudWorkspaceTaskIsReadShare(a.projectRecordToSearchResult(pi, *rec))
	workingDir := a.duplicateTaskWorkingDir(sourceProjectPath)
	cacheDir := ""
	tags := taskConfigTagsForDuplicate(rec.Tags)
	fileSource := ""
	hydrateFailed := false
	if readShare {
		// The shared task stays a read-only view of the conversation and
		// files. The copy is a local task the recipient can continue.
		cacheDir = workingDir
		fileSource, hydrateFailed = a.readShareDuplicateFileSource(sourceProjectPath, workingDir, rec.Tags)
		tags = tagsForLocalCopyOfReadShare(rec.Tags)
		workingDir = ""
	}
	created := a.createTaskRecordWithWorkingDir(name, "", tags, workingDir, true)
	if strings.TrimSpace(created.ProjectPath) == "" {
		return zero, fmt.Errorf("task copy failed")
	}
	if renamed := strings.TrimSpace(a.RenameTask(created.ProjectPath, name)); renamed != "" {
		created.Name = renamed
	}
	codingDir := workingDir
	relocateFrom := make([]string, 0, 2)
	copyNotice := ""
	if readShare {
		dest := filepath.Join(created.ProjectPath, "workspace")
		if err := copyReadShareFiles(fileSource, dest); err != nil {
			log.Printf("[task-copy] read-share file copy failed source=%q dest=%q err=%v", fileSource, dest, err)
			copyNotice = taskCopyFilesUnavailable
		}
		if readShareCopyNotice(fileSource, hydrateFailed, dest) != "" {
			copyNotice = taskCopyFilesUnavailable
		}
		codingDir = dest
		relocateFrom = readShareRelocateRoots(fileSource, cacheDir, dest)
	}
	if err := copyTaskCodingConfig(sourceProjectPath, created.ProjectPath, codingDir, relocateFrom...); err != nil {
		log.Printf("[task-copy] coding config copy failed source=%q copy=%q err=%v", sourceProjectPath, created.ProjectPath, err)
	}
	// The copy is a fork, not a blank task: the conversation must be available
	// the moment the new tab opens.
	a.copyProjectConversation(sourceProjectPath, created.ProjectPath)
	if updated := pi.Get(created.ProjectPath); updated != nil {
		created = a.projectRecordToSearchResult(pi, *updated)
	}
	created.CopyNotice = copyNotice
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

// readShareDuplicateFileSource is the tree copied into the local task.
// A configured Hub is pulled first so artifacts exist even when this machine
// has not opened the share yet. Otherwise the task's current directory is used.
// The bool is true when that pull failed and the caller fell back to whatever
// directory was already on disk.
func (a *App) readShareDuplicateFileSource(sourceProjectPath, workingDir string, tags []string) (string, bool) {
	hydrateFailed := false
	if id := cloudWorkspaceIDFromTags(tags); id != "" && a != nil {
		if _, _, _, hubErr := a.virtualRepositorySyncClient(); hubErr == nil {
			prepared, prepErr := a.PrepareCloudWorkspaceReadOnly(id)
			if prepErr != nil {
				log.Printf("[task-copy] read-share hydrate failed source=%q id=%s err=%v", sourceProjectPath, id, prepErr)
				hydrateFailed = true
			} else if path := strings.TrimSpace(prepared.LocalPath); path != "" {
				return path, false
			} else {
				hydrateFailed = true
			}
		}
	}
	workingDir = strings.TrimSpace(workingDir)
	if workingDir == "" {
		return "", hydrateFailed
	}
	info, err := os.Stat(workingDir)
	if err != nil || !info.IsDir() {
		return "", hydrateFailed
	}
	return workingDir, hydrateFailed
}

// readShareRelocateRoots are the directories whose recorded paths move into
// the new sandbox. A directory that already contains that sandbox is left out,
// because a second rewrite would nest the copied files under themselves.
func readShareRelocateRoots(fileSource, cacheDir, dest string) []string {
	roots := make([]string, 0, 2)
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		for _, existing := range roots {
			if normalizeProjectSessionPath(existing) == normalizeProjectSessionPath(path) {
				return
			}
		}
		// A root that already contains the new sandbox would rewrite the paths
		// just copied into that sandbox a second time.
		if copiedPathWithin(path, dest) && !strings.EqualFold(normalizeProjectSessionPath(path), normalizeProjectSessionPath(dest)) {
			return
		}
		roots = append(roots, path)
	}
	add(fileSource)
	add(cacheDir)
	if len(roots) == 0 {
		add(dest)
	}
	return roots
}

// readShareCopyNotice reports that the local task has the conversation but not
// the shared files. A failed pull that left only the empty-sandbox readme is
// the same outcome as a missing cache.
func readShareCopyNotice(fileSource string, hydrateFailed bool, dest string) string {
	if strings.TrimSpace(fileSource) == "" || (hydrateFailed && !workspaceHasCopiedArtifact(dest)) {
		return taskCopyFilesUnavailable
	}
	return ""
}

func workspaceHasCopiedArtifact(root string) bool {
	root = strings.TrimSpace(root)
	if root == "" {
		return false
	}
	found := false
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || found {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if path != root && (strings.EqualFold(name, ".maclaw-cloud") || strings.EqualFold(name, ".maclaw")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || strings.EqualFold(name, "README.md") {
			return nil
		}
		found = true
		return filepath.SkipAll
	})
	return found
}

// tagsForLocalCopyOfReadShare drops the cloud workspace identity. Keeping it
// would leave the copy read-only and still bound to the shared workspace.
func tagsForLocalCopyOfReadShare(tags []string) []string {
	copied := taskConfigTagsForDuplicate(tags)
	out := make([]string, 0, len(copied))
	for _, tag := range copied {
		tag = strings.TrimSpace(tag)
		if tag == "" || strings.HasPrefix(tag, cloudWorkspaceTagPrefix) || strings.HasPrefix(tag, "cloud_workspace_share:") || strings.HasPrefix(tag, cloudWorkspaceSharedFromPrefix) {
			continue
		}
		out = append(out, tag)
	}
	return out
}

// copyReadShareFiles fills the local sandbox with the shared artifacts.
// Cloud sync state stays in the cache: copying it would make the local task
// look like another checkout of the same workspace.
func copyReadShareFiles(fileSource, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	fileSource = strings.TrimSpace(fileSource)
	if fileSource == "" {
		return nil
	}
	if _, _, err := copyTaskTransferTree(fileSource, dest); err != nil {
		removeCopiedCloudSyncState(dest)
		return err
	}
	if err := removeCopiedCloudSyncState(dest); err != nil {
		return err
	}
	readme := filepath.Join(dest, "README.md")
	if _, err := os.Stat(filepath.Join(fileSource, "README.md")); os.IsNotExist(err) {
		if err := os.Remove(readme); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func removeCopiedCloudSyncState(dest string) error {
	var drop []string
	err := filepath.WalkDir(dest, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || !entry.IsDir() {
			return nil
		}
		if filepath.Clean(path) == filepath.Clean(dest) {
			return nil
		}
		name := entry.Name()
		if !strings.EqualFold(name, ".maclaw-cloud") && !strings.EqualFold(name, ".maclaw") {
			return nil
		}
		drop = append(drop, path)
		return filepath.SkipDir
	})
	if err != nil {
		return err
	}
	for _, path := range drop {
		if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
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
func copyTaskCodingConfig(sourceDir, targetDir, workingDir string, relocateFrom ...string) error {
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
	projectPath := retargetCopiedCodingProjectPath(sourceDir, targetDir, workingDir, mem.ProjectPath)
	filesModified := append([]string(nil), mem.FilesModified...)
	filesCreated := append([]string(nil), mem.FilesCreated...)
	approvedDirs := append([]string(nil), mem.ApprovedDirs...)
	instructionSources := append([]string(nil), mem.ProjectInstructionSources...)
	if roots := copiedRelocateRoots(relocateFrom); len(roots) > 0 {
		if dir := normalizeProjectSessionPath(workingDir); dir != "" {
			projectPath = dir
		}
		for _, from := range roots {
			filesModified = relocateCopiedPaths(filesModified, from, projectPath)
			filesCreated = relocateCopiedPaths(filesCreated, from, projectPath)
			approvedDirs = relocateCopiedPaths(approvedDirs, from, projectPath)
			instructionSources = relocateCopiedPaths(instructionSources, from, projectPath)
		}
	}
	cfg := stickyCodingWorkbenchMemory{
		Kind:                      mem.Kind,
		ProjectPath:               projectPath,
		LastUserText:              mem.LastUserText,
		LastSummary:               mem.LastSummary,
		SessionPlan:               mem.SessionPlan,
		ExecutionPlan:             mem.ExecutionPlan,
		RequirementRestatement:    mem.RequirementRestatement,
		FilesModified:             filesModified,
		FilesCreated:              filesCreated,
		TurnCount:                 mem.TurnCount,
		SessionFullAccess:         mem.SessionFullAccess,
		SessionHighRiskAccess:     mem.SessionHighRiskAccess,
		SessionPermissionMode:     mem.SessionPermissionMode,
		ApprovedDirs:              approvedDirs,
		PlanMode:                  mem.PlanMode,
		ProjectInstructions:       mem.ProjectInstructions,
		ProjectInstructionSources: instructionSources,
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

// copiedRelocateRoots drops empty and repeated cache directories. The hydrated
// cache and the task's working_dir tag can name the same folder.
func copiedRelocateRoots(roots []string) []string {
	out := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		root = normalizeProjectSessionPath(root)
		if root == "" {
			continue
		}
		key := strings.ToLower(root)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, root)
	}
	return out
}

// relocateCopiedPaths rewrites paths that live in the read-only cache so the
// local task does not keep editing that cache. Anything else stays as recorded.
func relocateCopiedPaths(paths []string, fromRoot, toRoot string) []string {
	fromRoot = normalizeProjectSessionPath(fromRoot)
	toRoot = normalizeProjectSessionPath(toRoot)
	if fromRoot == "" || toRoot == "" {
		return paths
	}
	out := make([]string, len(paths))
	for i, path := range paths {
		out[i] = relocateCopiedPath(path, fromRoot, toRoot)
	}
	return out
}

func relocateCopiedPath(path, fromRoot, toRoot string) string {
	pathNorm := normalizeProjectSessionPath(path)
	fromRoot = normalizeProjectSessionPath(fromRoot)
	toRoot = normalizeProjectSessionPath(toRoot)
	if pathNorm == "" || fromRoot == "" || toRoot == "" {
		return path
	}
	if strings.EqualFold(pathNorm, fromRoot) {
		return toRoot
	}
	if !copiedPathWithin(fromRoot, pathNorm) {
		return path
	}
	rel := copiedRelativePath(fromRoot, pathNorm)
	if copiedPathEscapes(rel) {
		return path
	}
	return filepath.Join(toRoot, rel)
}

// copiedRelativePath is the child path under parent. filepath.Rel keeps a
// path that differs only by letter case, which on Windows is the same folder,
// so the folded prefix is used when Rel steps out of the parent.
func copiedRelativePath(parent, child string) string {
	rel, err := filepath.Rel(parent, child)
	if err == nil && !copiedPathEscapes(rel) {
		return rel
	}
	prefix := parent
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	if len(child) < len(prefix) || !strings.EqualFold(child[:len(prefix)], prefix) {
		return ""
	}
	return child[len(prefix):]
}

func copiedPathEscapes(rel string) bool {
	return rel == "" || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
