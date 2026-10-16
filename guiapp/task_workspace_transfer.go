package guiapp

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Task file transfer backs the task-list "转移到云端 / 转移到本地" actions.
//
// The move is deliberately split in two halves: the task record itself is
// created by the existing task-creation paths (cloud workspace task / local
// task), and these methods only move the *files* — local execution directory
// into the mounted cloud workspace cache (+ push), or cloud workspace files
// down into a user-picked local directory. Keeping task-record creation on the
// existing paths means leases, sidecars and tab sessions are handled exactly
// like any other task creation.
//
// Both methods report what they copied so the caller can tell the user how much
// moved without re-walking the tree.

// Guard rails for one transfer. A cloud workspace cache is synced file by file,
// so an accidental multi-gigabyte source (a whole repository with its build
// output) must fail loudly before anything is written rather than half-copy and
// leave a partially synced workspace behind.
const (
	taskTransferMaxFiles = 50000
	taskTransferMaxBytes = int64(5) << 30 // 5 GiB
)

var (
	errTaskTransferTooLarge = errors.New("task transfer source is too large")
	errTaskTransferTooDeep  = errors.New("task transfer link chain is too deep")
	// errTaskTransferNestedTarget guards copying a tree into a folder that
	// lives inside it. WalkDir reads directories as it goes, so a destination
	// nested in the source is re-walked: each level copies the previous level's
	// output again until the disk or the budget gives up.
	errTaskTransferNestedTarget = errors.New("task transfer destination is inside its source")
)

// taskTransferMaxDepth caps how many symlinked directories one transfer walks
// into. Cycles are caught by the visited set; this is the backstop for a long
// (but acyclic) chain that would otherwise recurse until the stack dies.
const taskTransferMaxDepth = 16

// TaskFileTransferResult describes one completed file transfer.
type TaskFileTransferResult struct {
	// SourceDir is where the files were read from.
	SourceDir string `json:"source_dir"`
	// TargetDir is where the files were written to.
	TargetDir string `json:"target_dir"`
	// WorkspaceID is set for transfers that involve a cloud workspace.
	WorkspaceID string `json:"workspace_id,omitempty"`
	// Files and Bytes count what actually landed in TargetDir. A source that
	// holds no regular file is a legitimate no-op (Files == 0).
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
}

// CopyTaskFilesToCloudWorkspace copies the source task's execution directory
// into the cloud workspace cache and pushes it to the Hub.
//
// The caller is expected to have created (or resumed) the cloud workspace task
// for workspaceID first — that call is what mounts the cache and takes the
// writer lease. This method therefore only needs the mount, not task identity.
func (a *App) CopyTaskFilesToCloudWorkspace(sourceProjectPath, workspaceID string) (TaskFileTransferResult, error) {
	result := TaskFileTransferResult{}
	sourceProjectPath = normalizeProjectSessionPath(sourceProjectPath)
	workspaceID = strings.TrimSpace(workspaceID)
	if sourceProjectPath == "" {
		return result, fmt.Errorf("source task is required")
	}
	if !validCloudWorkspaceCacheID(workspaceID) {
		return result, fmt.Errorf("workspace id is required")
	}
	result.WorkspaceID = workspaceID

	sourceDir := a.taskTransferSourceDir(sourceProjectPath)
	if sourceDir != "" {
		if a.isAppCloudWorkspaceCachePath(sourceDir) {
			// The source already lives in a cloud workspace cache. Copying it
			// into another cache would be a plain duplicate; the UI routes that
			// case to a workspace-level operation instead.
			return result, fmt.Errorf("source task already lives in a cloud workspace")
		}
		result.SourceDir = sourceDir
	}

	prepared, err := a.PrepareCloudWorkspace(workspaceID)
	if err != nil {
		return result, err
	}
	root := normalizeProjectSessionPath(prepared.LocalPath)
	if root == "" {
		return result, fmt.Errorf("cloud workspace cache path is empty")
	}
	result.TargetDir = root

	if sourceDir == "" {
		// Nothing to move: the workspace already holds whatever the task had.
		return result, nil
	}
	files, bytes, err := copyTaskTransferTree(sourceDir, root)
	result.Files = files
	result.Bytes = bytes
	if err != nil {
		return result, err
	}
	if files == 0 {
		return result, nil
	}
	if err := a.pushTaskTransferToCloudWorkspace(workspaceID, root); err != nil {
		return result, err
	}
	return result, nil
}

// CopyCloudWorkspaceTaskFilesToLocal copies a cloud workspace's files into a
// local directory picked by the user.
//
// Reading goes through SyncCloudWorkspaceFiles so a workspace that is not
// mounted in this process is hydrated from the Hub first, while a mounted
// writer keeps its local files as the source of truth (no pull can overwrite
// unsaved local work).
func (a *App) CopyCloudWorkspaceTaskFilesToLocal(sourceProjectPath, targetDir string) (TaskFileTransferResult, error) {
	result := TaskFileTransferResult{}
	sourceProjectPath = normalizeProjectSessionPath(sourceProjectPath)
	targetDir = normalizeProjectSessionPath(targetDir)
	if sourceProjectPath == "" {
		return result, fmt.Errorf("source task is required")
	}
	if targetDir == "" {
		return result, fmt.Errorf("target folder is required")
	}
	if a.isAppCloudWorkspaceCachePath(targetDir) {
		return result, fmt.Errorf("target folder cannot be a cloud workspace cache")
	}
	workspaceID := a.lookupCloudWorkspaceIDForProject(sourceProjectPath)
	if workspaceID == "" {
		return result, fmt.Errorf("source task is not a cloud workspace task")
	}
	result.WorkspaceID = workspaceID

	prepared, err := a.SyncCloudWorkspaceFiles(workspaceID)
	if err != nil {
		// Wrapped: the raw sync error is a transport detail, and the UI shows it
		// verbatim. Say which step failed so the message is actionable.
		return result, fmt.Errorf("reading the cloud workspace before copying: %w", err)
	}
	sourceDir := normalizeProjectSessionPath(prepared.LocalPath)
	if sourceDir == "" {
		return result, fmt.Errorf("cloud workspace cache path is empty")
	}
	result.SourceDir = sourceDir
	if sourceDir == targetDir || pathWithinDir(sourceDir, targetDir) {
		return result, fmt.Errorf("target folder cannot be inside the cloud workspace cache")
	}
	files, bytes, err := copyTaskTransferTree(sourceDir, targetDir)
	result.TargetDir = targetDir
	result.Files = files
	result.Bytes = bytes
	if err != nil {
		return result, err
	}
	return result, nil
}

// taskTransferSourceDir resolves the directory that holds a local task's files.
// Managed tasks keep their files beside the task metadata (tasks/<slug>/workspace)
// unless the user pinned an explicit working directory, which is then the real
// content location.
func (a *App) taskTransferSourceDir(projectPath string) string {
	dir := normalizeProjectSessionPath(a.recentTaskExecutionProjectPath(projectPath))
	if dir == "" {
		return ""
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return ""
	}
	return dir
}

// pushTaskTransferToCloudWorkspace commits freshly copied files to the Hub.
func (a *App) pushTaskTransferToCloudWorkspace(workspaceID, root string) error {
	ctx, cancel := a.cloudWorkspaceSyncContext()
	defer cancel()
	if _, err := a.pushCloudWorkspaceWithProgress(ctx, workspaceID, root, nil); err != nil {
		log.Printf("[task_transfer] push failed workspace=%s err=%v", workspaceID, err)
		return err
	}
	a.emitEvent(cloudWorkspaceFilesChangedEvent, map[string]string{
		"workspace_id": workspaceID,
		"path":         root,
	})
	return nil
}

// copyTaskTransferTree copies the contents of src into dst and returns the
// number of regular files written and their total size.
//
// The source is walked twice: a metadata-only pre-scan rejects an oversized
// directory before anything is written, then the copy pass runs. Symlinks that
// resolve outside src and broken links are skipped, and a directory reachable
// twice through links is only copied once so cycles cannot loop forever.
func copyTaskTransferTree(src, dst string) (int, int64, error) {
	walk := newTaskTransferWalk()
	if err := walk.copyTree(src, dst); err != nil {
		return walk.files, walk.bytes, err
	}
	return walk.files, walk.bytes, nil
}

// taskTransferWalk is the state one copy pass shares with the extra passes it
// starts for symlinked directories.
//
// It has to outlive a single call: a link pointing back at an ancestor resolves
// to a directory that has already been copied, and a per-call visited set would
// re-enter it on every level and recurse forever.
type taskTransferWalk struct {
	visited map[string]bool
	depth   int
	// audited* is the running budget charge; files/bytes is what was written.
	auditedFiles int
	auditedBytes int64
	files        int
	bytes        int64
	// max* default to the package budget and stay injectable for tests.
	maxFiles int
	maxBytes int64
}

func newTaskTransferWalk() *taskTransferWalk {
	return &taskTransferWalk{
		visited:  map[string]bool{},
		maxFiles: taskTransferMaxFiles,
		maxBytes: taskTransferMaxBytes,
	}
}

// claimDir reports whether dir may still be copied by this walk. Every
// directory entered is claimed, so the second arrival — a cycle, or two links
// onto the same folder — copies nothing instead of duplicating a subtree.
func (w *taskTransferWalk) claimDir(dir string) bool {
	// Normalised like every other path in this file so a drive-letter case
	// difference (Windows resolves links with its own casing) cannot sneak a
	// second copy of the same directory past the guard.
	key := normalizeProjectSessionPath(dir)
	if key == "" {
		key = filepath.Clean(dir)
	}
	if w.visited[key] {
		return false
	}
	w.visited[key] = true
	return true
}

func (w *taskTransferWalk) copyTree(src, dst string) error {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	dstAbs, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	srcAbs = filepath.Clean(srcAbs)
	dstAbs = filepath.Clean(dstAbs)
	// Refused before the budget scan: a destination inside its own source makes
	// the walk read directories it just wrote, so no amount of counting can
	// bound it. The cloud direction needs this as much as the local one — the
	// workspace cache sits under the data dir, and a task's execution directory
	// is allowed to be an ancestor of it.
	if dstAbs == srcAbs || pathWithinDir(srcAbs, dstAbs) {
		return errTaskTransferNestedTarget
	}
	// Budget, then destination, then copy: an oversized source must be refused
	// before the destination folder is created so a rejected transfer leaves
	// nothing behind. The budget is cumulative across every tree this walk
	// enters, so symlinked subtrees cannot each spend a full allowance.
	if err := w.auditDir(srcAbs); err != nil {
		return err
	}
	if err := os.MkdirAll(dstAbs, 0o755); err != nil {
		return err
	}
	// Claimed before the walk so a link that resolves back to this root is
	// recognised as already copied even at depth zero.
	w.claimDir(srcAbs)

	return filepath.WalkDir(srcAbs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			// A file that disappeared mid-walk is not a transfer failure.
			if os.IsNotExist(walkErr) || os.IsPermission(walkErr) {
				return nil
			}
			return walkErr
		}
		rel, err := filepath.Rel(srcAbs, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dstAbs, rel)
		if !pathWithinDir(dstAbs, target) {
			return fmt.Errorf("illegal copy target: %s", target)
		}
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return w.copyLink(srcAbs, path, target)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return w.copyFile(path, target, info.Mode().Perm())
	})
}

// auditDir charges this tree against the budget the whole transfer shares.
func (w *taskTransferWalk) auditDir(dir string) error {
	count, size, err := scanTaskTransferTree(dir)
	if err != nil {
		return err
	}
	w.auditedFiles += count
	w.auditedBytes += size
	if w.auditedFiles > w.maxFiles || w.auditedBytes > w.maxBytes {
		return errTaskTransferTooLarge
	}
	return nil
}

// scanTaskTransferTree returns how many regular files a tree holds and their
// total size. A single tree over either default limit stops the walk early so a
// huge source cannot cost a full traversal before being refused.
func scanTaskTransferTree(srcAbs string) (int, int64, error) {
	count := 0
	var bytes int64
	err := filepath.WalkDir(srcAbs, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) || os.IsPermission(walkErr) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		count++
		bytes += info.Size()
		if count > taskTransferMaxFiles || bytes > taskTransferMaxBytes {
			return errTaskTransferTooLarge
		}
		return nil
	})
	if errors.Is(err, errTaskTransferTooLarge) {
		return count, bytes, errTaskTransferTooLarge
	}
	return count, bytes, err
}

// copyLink dereferences a symlink and copies the target's content, but only
// while it still points inside the source tree.
func (w *taskTransferWalk) copyLink(srcAbs, path, target string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		// Broken link: nothing to copy.
		return nil
	}
	if !taskTransferLinkTargetAllowed(srcAbs, resolved) {
		return nil
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil
	}
	if info.IsDir() {
		if !w.claimDir(resolved) {
			// Already reached through another link (or through itself): copying
			// again would duplicate a whole subtree, or never stop.
			return nil
		}
		if err := w.descend(); err != nil {
			return err
		}
		defer w.ascend()
		return w.copyTree(resolved, target)
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	return w.copyFile(resolved, target, info.Mode().Perm())
}

// descend/ascend bound how many symlinked directories one transfer walks into.
func (w *taskTransferWalk) descend() error {
	if w.depth >= taskTransferMaxDepth {
		return errTaskTransferTooDeep
	}
	w.depth++
	return nil
}

func (w *taskTransferWalk) ascend() {
	if w.depth > 0 {
		w.depth--
	}
}

// taskTransferLinkTargetAllowed is the containment rule that keeps a symlink
// from pulling files outside the source tree into a cloud workspace. It is a
// named function rather than an inline pathWithinDir call so the rule can be
// tested on filesystems that refuse to create symlinks.
func taskTransferLinkTargetAllowed(srcAbs, resolved string) bool {
	if strings.TrimSpace(resolved) == "" {
		return false
	}
	return pathWithinDir(srcAbs, resolved)
}

func (w *taskTransferWalk) copyFile(src, dst string, perm os.FileMode) error {
	if perm == 0 {
		// A source file with no permissions at all would produce an unusable
		// copy; keep it owner-readable and writable instead.
		perm = 0o600
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := openTaskTransferDest(dst, perm)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	// Restore the source mode: the file may have been relaxed for writing.
	if err := os.Chmod(dst, perm); err != nil {
		return err
	}
	w.files++
	w.bytes += written
	return nil
}

// openTaskTransferDest truncates dst for writing, relaxing the mode first when
// a previous transfer left a read-only copy behind — otherwise a second move
// into the same workspace would fail on our own output.
func openTaskTransferDest(dst string, perm os.FileMode) (*os.File, error) {
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm|0o600)
	if err == nil || !os.IsPermission(err) {
		return out, err
	}
	if chmodErr := os.Chmod(dst, perm|0o600); chmodErr != nil {
		return nil, err
	}
	return os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm|0o600)
}
