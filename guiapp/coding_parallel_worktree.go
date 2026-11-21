package guiapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// codingParallelTurn is one later implementation task that must not write the
// directory another live task already holds. A git repo gets a private
// worktree and a content merge. Anything else waits, then uses the primary.
type codingParallelTurn struct {
	handler              *IMMessageHandler
	userID               string
	taskPath             string
	primary              string
	execDir              string
	wt                   *codingWorkbenchWorktree
	baseSHA              string
	deferred             bool
	stayPut              bool
	waitPrimary          bool
	discardWorktree      bool
	remaining            int
	merged               bool
	children             int
	cleanupAfterChildren bool
	forgetAfterChildren  bool
	onProgress           func(string)
	runCtx               context.Context
	mu                   sync.Mutex
	childMu              sync.Mutex
}

var codingParallelTurns sync.Map // task path key -> *codingParallelTurn

func codingParallelTurnKey(taskPath string) string {
	return normalizeRecentTaskPathKey(taskPath)
}

func codingParallelTurnFor(taskPath string) *codingParallelTurn {
	value, ok := codingParallelTurns.Load(codingParallelTurnKey(taskPath))
	if !ok {
		return nil
	}
	turn, _ := value.(*codingParallelTurn)
	return turn
}

// codingParallelMappedDir returns the private checkout for a path inside the
// primary tree. A bound turn keeps that checkout after the earlier session
// leaves, until the merge finishes.
func codingParallelMappedDir(taskPath, dir string) (string, bool) {
	turn := codingParallelTurnFor(taskPath)
	if turn == nil {
		return "", false
	}
	original := dir
	dir = normalizeProjectSessionPath(dir)
	if dir == "" {
		return "", false
	}
	if turn.stayPut {
		return original, true
	}
	if turn.wt == nil || turn.wt.Path == "" {
		return "", false
	}
	if pathInsideWorkRoot(turn.wt.Path, dir) {
		return dir, true
	}
	root := turn.wt.GitRoot
	if root == "" {
		root = turn.primary
	}
	rel, inside := workRootRelative(root, dir)
	if !inside {
		return "", false
	}
	if rel == "." {
		// The caller asked for the git root. ProjectPath is only the
		// subdirectory the user opened; returning it would put a root file
		// inside that subdirectory, and the merge would land it there.
		return turn.wt.Path, true
	}
	// workRootRelative folds case for the lookup. The checkout is a real
	// tree, so the mapped path keeps the caller's spelling.
	mappedRel := filepath.FromSlash(rel)
	if preserved, err := filepath.Rel(filepath.Clean(root), filepath.Clean(dir)); err == nil && preserved != "." && !strings.HasPrefix(preserved, "..") {
		mappedRel = preserved
	}
	return filepath.Join(turn.wt.Path, mappedRel), true
}

// codingTurnPrimaryDir is the directory the task record says it edits.
// A detached checkout is an execution copy, not that directory.
func (a *App) codingTurnPrimaryDir(userID, presented string) string {
	presented = normalizeProjectSessionPath(presented)
	taskPath := projectPathFromSessionOwnerID(userID)
	if a == nil {
		return presented
	}
	if tagged := normalizeProjectSessionPath(a.codingTaskTaggedWorkRoot(taskPath)); tagged != "" && !isLocalCodingIdentityOrSandbox(taskPath, tagged) {
		return tagged
	}
	if claimed, ok := a.claimedWorkRoot(taskPath); ok {
		claimed = normalizeProjectSessionPath(claimed)
		if claimed != "" && !isLocalCodingIdentityOrSandbox(taskPath, claimed) {
			checkout := normalizeProjectSessionPath(a.exclusiveCheckoutDir(taskPath))
			if checkout == "" || !pathInsideWorkRoot(checkout, claimed) {
				return claimed
			}
		}
	}
	return presented
}

// codingParallelWorktreeCreate is the worktree allocator for a later
// implementation turn. Tests replace it to simulate a git lock.
var codingParallelWorktreeCreate = createCodingWorkbenchWorktree

// beginCodingParallelTurn decides where this local coding turn executes.
// Inquiry reads the primary tree. An operational turn waits when another
// session already holds it. An implementation turn takes a worktree when it
// can, and otherwise waits on the primary instead of writing beside the holder.
func (h *IMMessageHandler) beginCodingParallelTurn(userID, presented string, kind codingRequestKind, onProgress func(string)) *codingParallelTurn {
	if h == nil || h.app == nil {
		return nil
	}
	taskPath := normalizeProjectSessionPath(projectPathFromSessionOwnerID(userID))
	primary := h.app.codingTurnPrimaryDir(userID, presented)
	if taskPath == "" || primary == "" {
		return nil
	}
	holder, _ := h.app.earlierLiveWorkRootHolder(taskPath, primary)
	if holder == "" {
		return nil
	}
	turn := &codingParallelTurn{
		handler:    h,
		userID:     userID,
		taskPath:   taskPath,
		primary:    primary,
		execDir:    primary,
		onProgress: onProgress,
	}
	if kind == codingRequestInquiry {
		turn.stayPut = true
		codingParallelTurns.Store(codingParallelTurnKey(taskPath), turn)
		return turn
	}
	if kind != codingRequestImplementation {
		turn.stayPut = true
		turn.waitPrimary = true
		codingParallelTurns.Store(codingParallelTurnKey(taskPath), turn)
		if onProgress != nil {
			onProgress("同一目录上另一个任务还在使用，本任务等它结束后再开始")
		}
		return turn
	}
	wt, err := codingParallelWorktreeCreate(primary, 0, "parallel-task")
	if err != nil || wt == nil {
		turn.stayPut = true
		turn.waitPrimary = true
		codingParallelTurns.Store(codingParallelTurnKey(taskPath), turn)
		if onProgress != nil {
			if err != nil {
				onProgress("独立检出创建失败，本任务等另一个任务写完后再在主目录开始")
			} else {
				onProgress("这个目录不是 git 仓库，本任务等另一个任务写完后再开始，避免互相覆盖")
			}
		}
		return turn
	}
	base, baseErr := codingParallelGitText(wt.Path, "rev-parse", "HEAD")
	if baseErr != nil || strings.TrimSpace(base) == "" {
		wt.cleanup(false)
		turn.stayPut = true
		turn.waitPrimary = true
		codingParallelTurns.Store(codingParallelTurnKey(taskPath), turn)
		if onProgress != nil {
			onProgress("独立检出创建失败，本任务等另一个任务写完后再在主目录开始")
		}
		return turn
	}
	turn.wt = wt
	turn.baseSHA = strings.TrimSpace(base)
	turn.execDir = wt.ProjectPath
	turn.deferred = true
	codingParallelTurns.Store(codingParallelTurnKey(taskPath), turn)
	codingParallelRememberExec(turn)
	if onProgress != nil {
		onProgress("本任务在独立检出中执行，结束后合并回主目录")
	}
	return turn
}

func (t *codingParallelTurn) bindRunCtx(ctx context.Context) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.runCtx = ctx
	t.mu.Unlock()
}

func (t *codingParallelTurn) waitReady(ctx context.Context) error {
	if t == nil || !t.waitPrimary {
		return nil
	}
	return t.waitHolder(ctx)
}

func (t *codingParallelTurn) hasMerged() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.merged
}

// shellDir is where a hook or verify command of this turn runs. Those
// commands can write files. Before the merge they use the private checkout,
// so they do not write the directory the other task still holds. After the
// merge the combined bytes are on the primary and the checkout is about to
// be removed, so the command runs on the primary.
func (t *codingParallelTurn) shellDir(fallback string) string {
	if t == nil || !t.deferred {
		return fallback
	}
	if t.hasMerged() && strings.TrimSpace(t.primary) != "" {
		return t.primary
	}
	if strings.TrimSpace(t.execDir) != "" {
		return t.execDir
	}
	return fallback
}

// previewPaths points a finished merge at the primary tree. The worktree is
// where the turn wrote; the bytes a person opens are the ones that landed.
func (t *codingParallelTurn) previewPaths(execDir string, modified, created []string) (string, []string, []string) {
	if t == nil || !t.deferred || t.primary == "" || !t.hasMerged() {
		return execDir, modified, created
	}
	modified = t.remapLandedPaths(execDir, modified)
	created = t.remapLandedPaths(execDir, created)
	root := t.primary
	// A file beside the opened subdirectory is inside the git root. The
	// preview reads relative names from its root, so those names are made
	// absolute before the root widens. A path outside the git root does not
	// widen it: the subdirectory stays the root and its relative names stay
	// relative. A turn that only touched the subdirectory keeps that root.
	if t.wt != nil && previewPathsLeaveRoot(root, t.wt.GitRoot, modified, created) {
		modified = absolutizeUnder(t.primary, modified)
		created = absolutizeUnder(t.primary, created)
		root = t.wt.GitRoot
	}
	return root, modified, created
}

// previewPathsLeaveRoot reports a landed path that sits in the git repo and
// outside the opened subdirectory. A path outside the repo does not count.
func previewPathsLeaveRoot(opened, gitRoot string, groups ...[]string) bool {
	opened = filepath.Clean(strings.TrimSpace(opened))
	gitRoot = filepath.Clean(strings.TrimSpace(gitRoot))
	if opened == "" || opened == "." || gitRoot == "" || gitRoot == "." {
		return false
	}
	for _, group := range groups {
		for _, p := range group {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			full := p
			if !filepath.IsAbs(full) {
				full = filepath.Join(opened, filepath.FromSlash(full))
			}
			full = filepath.Clean(full)
			if !previewPathEscapes(opened, full) || previewPathEscapes(gitRoot, full) {
				continue
			}
			return true
		}
	}
	return false
}

func previewPathEscapes(root, full string) bool {
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return true
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func absolutizeUnder(root string, paths []string) []string {
	if strings.TrimSpace(root) == "" || len(paths) == 0 {
		return paths
	}
	out := make([]string, len(paths))
	for i, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" || filepath.IsAbs(p) {
			out[i] = p
			continue
		}
		out[i] = filepath.Join(root, filepath.FromSlash(p))
	}
	return out
}

// remapLandedPaths rewrites checkout paths onto the tree the merge wrote.
// A file beside the opened subdirectory is still inside the repo, so the
// pair is the worktree root and the git root. The checkout-to-primary pair
// is only the fallback when those roots were not recorded.
func (t *codingParallelTurn) remapLandedPaths(execDir string, paths []string) []string {
	if t == nil || len(paths) == 0 {
		return paths
	}
	from, to := execDir, t.primary
	if from == "" {
		from = t.execDir
	}
	if t.wt != nil && strings.TrimSpace(t.wt.Path) != "" && strings.TrimSpace(t.wt.GitRoot) != "" {
		from = t.wt.Path
		to = t.wt.GitRoot
	}
	if from == "" || to == "" {
		return paths
	}
	return remapWorktreePaths(paths, from, to)
}

func (t *codingParallelTurn) runtimeOptions(task *TaskItem) *guiCodingRuntimeOptions {
	options := defaultGUICodingRuntimeOptions(task, t.primary)
	if !t.deferred {
		return options
	}
	options.PolicyProjectRoot = t.primary
	options.DeferredWorktree = true
	options.WorkspaceIsolated = true
	options.RequireFinalDiffGate = true
	options.FinalizeWriter = t.finalize
	return options
}

func (t *codingParallelTurn) noteSteps(n int) {
	if t == nil || !t.deferred {
		return
	}
	t.mu.Lock()
	t.remaining = n
	t.mu.Unlock()
}

func (t *codingParallelTurn) finalize(result *CodingSubAgentResult) (bool, error) {
	if t == nil || !t.deferred {
		return true, nil
	}
	t.mu.Lock()
	if t.remaining > 0 {
		t.remaining--
	}
	run := t.remaining <= 0 && !t.merged
	ctx := t.runCtx
	t.mu.Unlock()
	if !run {
		return true, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return t.integrate(ctx, result)
}

func (t *codingParallelTurn) finish(ctx context.Context) {
	if t == nil {
		return
	}
	defer codingParallelTurns.Delete(codingParallelTurnKey(t.taskPath))
	if !t.deferred || t.wt == nil {
		return
	}
	t.mu.Lock()
	already := t.merged
	t.mu.Unlock()
	if !already {
		_, err := t.integrate(ctx, nil)
		if err != nil {
			t.mu.Lock()
			untouched := !t.merged
			t.mu.Unlock()
			// Cancel before any primary write drops the checkout. A conflict
			// or a file already written stays; deleting that checkout would
			// throw away the copy a person still has to review.
			if untouched && ctx != nil && ctx.Err() != nil {
				t.releaseExec(true)
				return
			}
			if untouched && t.handler != nil {
				t.handler.rememberCodingWorktree(t.userID, t.wt, "merge-failed")
			}
		}
	}
	t.mu.Lock()
	discard := t.discardWorktree
	t.mu.Unlock()
	// A clean merge removes the checkout. A detached read-only child is still
	// reading that directory, so the removal waits until the child returns.
	// A conflict keeps the directory for review and only drops the lookup.
	t.releaseExec(discard)
}

// codingParallelByExec finds the turn whose checkout a detached reader is
// still using. The tool map is removed when the turn returns; this one stays
// until the checkout is removed or deliberately kept.
var codingParallelByExec sync.Map

func codingParallelRememberExec(t *codingParallelTurn) {
	if t == nil || strings.TrimSpace(t.execDir) == "" {
		return
	}
	codingParallelByExec.Store(normalizeCodingRuntimeRef(t.execDir), t)
}

func codingParallelForgetExec(t *codingParallelTurn) {
	if t == nil || strings.TrimSpace(t.execDir) == "" {
		return
	}
	key := normalizeCodingRuntimeRef(t.execDir)
	if cur, ok := codingParallelByExec.Load(key); ok && cur == t {
		codingParallelByExec.Delete(key)
	}
}

// codingParallelTrackExec keeps the checkout alive for one detached read-only
// child. The child may use the project subdirectory, the worktree root, or a
// directory under that root. The returned function runs when the child returns.
func codingParallelTrackExec(projectPath string) func() {
	turn := codingParallelTurnForExec(projectPath)
	if turn == nil || turn.wt == nil {
		return func() {}
	}
	turn.childMu.Lock()
	turn.children++
	turn.childMu.Unlock()
	return turn.finishChild
}

// codingParallelTurnForExec finds the checkout a detached reader still uses.
// The ledger child is keyed by the project subdirectory. A reader of the
// worktree root, or of a directory under it, is the same checkout.
func codingParallelTurnForExec(projectPath string) *codingParallelTurn {
	if strings.TrimSpace(projectPath) == "" {
		return nil
	}
	if value, ok := codingParallelByExec.Load(normalizeCodingRuntimeRef(projectPath)); ok {
		turn, _ := value.(*codingParallelTurn)
		if turn != nil && turn.wt != nil {
			return turn
		}
	}
	var found *codingParallelTurn
	codingParallelByExec.Range(func(_, value any) bool {
		turn, _ := value.(*codingParallelTurn)
		if turn == nil || turn.wt == nil || strings.TrimSpace(turn.wt.Path) == "" {
			return true
		}
		if pathInsideWorkRoot(turn.wt.Path, projectPath) {
			found = turn
			return false
		}
		return true
	})
	return found
}

func (t *codingParallelTurn) releaseExec(deleteFiles bool) {
	if t == nil {
		return
	}
	t.childMu.Lock()
	if t.children > 0 {
		if deleteFiles {
			t.cleanupAfterChildren = true
		} else {
			t.forgetAfterChildren = true
		}
		t.childMu.Unlock()
		return
	}
	t.childMu.Unlock()
	codingParallelForgetExec(t)
	if deleteFiles && t.wt != nil {
		t.wt.cleanup(false)
	}
}

func (t *codingParallelTurn) finishChild() {
	if t == nil {
		return
	}
	t.childMu.Lock()
	if t.children > 0 {
		t.children--
	}
	left := t.children
	deleteFiles := left == 0 && t.cleanupAfterChildren
	forget := left == 0 && (t.cleanupAfterChildren || t.forgetAfterChildren)
	t.childMu.Unlock()
	if !forget {
		return
	}
	codingParallelForgetExec(t)
	if deleteFiles && t.wt != nil {
		t.wt.cleanup(false)
	}
}

func (t *codingParallelTurn) integrate(ctx context.Context, result *CodingSubAgentResult) (bool, error) {
	t.mu.Lock()
	if t.merged {
		t.mu.Unlock()
		return true, nil
	}
	t.merged = true
	wt := t.wt
	t.mu.Unlock()
	if wt == nil {
		return true, nil
	}
	if err := t.waitHolder(ctx); err != nil {
		t.mu.Lock()
		t.merged = false
		t.mu.Unlock()
		return false, err
	}
	summary, err := integrateCodingParallelWorktree(wt, t.baseSHA)
	if err != nil {
		// The diff failed before any primary write. Leave the merge unmarked
		// so finish can try once more. A failure after a file was written
		// stays marked: repeating the 3-way merge would apply that file twice.
		untouched := summary.Applied == 0 && len(summary.Conflicts) == 0
		if untouched {
			t.mu.Lock()
			t.merged = false
			t.mu.Unlock()
		} else if t.handler != nil {
			t.handler.rememberCodingWorktree(t.userID, wt, "merge-failed")
		}
		return false, err
	}
	if len(summary.Conflicts) > 0 {
		notice := fmt.Sprintf("自动合入 %d 个文件，还有 %d 个要人看", summary.Applied, len(summary.Conflicts))
		if t.onProgress != nil {
			t.onProgress(notice)
		}
		t.handler.storeStickyCodingConflict(t.userID, codingWorkbenchConflict{
			StepIndex:   wt.StepIndex,
			Branch:      wt.Branch,
			Path:        wt.Path,
			ProjectPath: wt.ProjectPath,
			GitRoot:     wt.GitRoot,
			MainProject: t.primary,
			Error:       fmt.Sprintf("%d 个文件无法自动合并", len(summary.Conflicts)),
			Kind:        "local_worktree",
			Files:       append([]string(nil), summary.Conflicts...),
		})
		wt.cleanup(true)
		if result != nil {
			result.FilesModified = summary.landedModified(result.FilesModified)
			result.FilesCreated = summary.landedModified(result.FilesCreated)
			t.rewriteLandedResultPaths(result)
		}
		return false, fmt.Errorf("%s", notice)
	}
	if t.onProgress != nil && summary.Applied > 0 {
		t.onProgress(fmt.Sprintf("已自动合入 %d 个文件", summary.Applied))
	}
	t.mu.Lock()
	t.discardWorktree = true
	t.mu.Unlock()
	t.rewriteLandedResultPaths(result)
	return true, nil
}

// rewriteLandedResultPaths points the turn's file list at the tree the merge
// wrote. An absolute checkout path would dangle once that checkout is removed.
func (t *codingParallelTurn) rewriteLandedResultPaths(result *CodingSubAgentResult) {
	if t == nil || result == nil {
		return
	}
	result.FilesModified = t.remapLandedPaths(t.execDir, result.FilesModified)
	result.FilesCreated = t.remapLandedPaths(t.execDir, result.FilesCreated)
}

func (t *codingParallelTurn) waitHolder(ctx context.Context) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if t == nil || t.handler == nil || t.handler.app == nil {
		return nil
	}
	for {
		// A stop must win even when the other task has already left.
		// Otherwise finish writes the primary for a turn the person cancelled.
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		holder, _ := t.handler.app.earlierLiveWorkRootHolder(t.taskPath, t.primary)
		if holder == "" {
			if ctx != nil && ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		}
		timer := time.NewTimer(500 * time.Millisecond)
		if ctx == nil {
			<-timer.C
			continue
		}
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type codingParallelIntegrateSummary struct {
	Applied       int
	Conflicts     []string
	landed        map[string]bool
	roots         []string
	projectPrefix string
}

func (s codingParallelIntegrateSummary) landedModified(paths []string) []string {
	if len(s.landed) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if codingParallelPathLanded(s.landed, s.roots, s.projectPrefix, path) {
			out = append(out, path)
		}
	}
	return out
}

// codingParallelPathLanded reports whether path is one of the git-relative
// files that were written onto the primary. A bare file name is not enough:
// sub/other.txt and other.txt are different files.
func codingParallelPathLanded(landed map[string]bool, roots []string, projectPrefix, path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if !filepath.IsAbs(path) {
		rel := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(path)), "./")
		if rel == "" || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
			return false
		}
		if codingParallelLandedKey(landed, rel) {
			return true
		}
		return projectPrefix != "" && codingParallelLandedKey(landed, projectPrefix+"/"+rel)
	}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || rel == "" || rel == ".." || strings.HasPrefix(rel, "../") {
			continue
		}
		if codingParallelLandedKey(landed, rel) {
			return true
		}
	}
	return false
}

func codingParallelLandedKey(landed map[string]bool, path string) bool {
	return landed[strings.ToLower(filepath.ToSlash(path))]
}

var codingParallelMergeLocks sync.Map

func codingParallelMergeLock(gitRoot string) func() {
	key := normalizeCodingRuntimeRef(gitRoot)
	value, _ := codingParallelMergeLocks.LoadOrStore(key, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// integrateCodingParallelWorktree copies worktree edits onto the primary
// working tree. A file the primary did not touch is taken from the worktree.
// A file both sides changed is merged with git merge-file. Overlapping hunks,
// binary files, and a delete against an edit stay on the worktree.
func integrateCodingParallelWorktree(wt *codingWorkbenchWorktree, baseSHA string) (codingParallelIntegrateSummary, error) {
	var summary codingParallelIntegrateSummary
	if wt == nil || wt.Path == "" || strings.TrimSpace(baseSHA) == "" {
		return summary, fmt.Errorf("parallel worktree merge is missing its base")
	}
	root := wt.GitRoot
	if root == "" {
		root = wt.Path
	}
	unlock := codingParallelMergeLock(root)
	defer unlock()
	changes, err := codingParallelChangedFiles(wt.Path, baseSHA)
	if err != nil {
		return summary, err
	}
	summary.landed = map[string]bool{}
	summary.roots = []string{root, wt.Path}
	if wt.ProjectPath != "" && wt.ProjectPath != wt.Path {
		summary.roots = append(summary.roots, wt.ProjectPath)
	}
	if rel, err := filepath.Rel(wt.Path, wt.ProjectPath); err == nil {
		rel = filepath.ToSlash(rel)
		if rel != "" && rel != "." && rel != ".." && !strings.HasPrefix(rel, "../") {
			summary.projectPrefix = rel
		}
	}
	for _, change := range changes {
		rel, ok := codingParallelRelPath(change.path)
		if !ok {
			log.Printf("[coding-parallel] skip worktree path outside the repository: %s", change.path)
			continue
		}
		change.path = rel
		applied, conflict, applyErr := applyCodingParallelFile(root, wt.Path, baseSHA, change)
		if applyErr != nil {
			// One file must not stop the others. Earlier files may already be
			// on the primary, so this merge cannot be run again from scratch.
			log.Printf("[coding-parallel] file %s stayed on the worktree: %v", change.path, applyErr)
			summary.Conflicts = append(summary.Conflicts, change.path)
			continue
		}
		if conflict {
			summary.Conflicts = append(summary.Conflicts, change.path)
			continue
		}
		if applied {
			summary.Applied++
			summary.landed[strings.ToLower(change.path)] = true
		}
	}
	return summary, nil
}

type codingParallelChange struct {
	path    string
	deleted bool
}

func codingParallelChangedFiles(worktree, baseSHA string) ([]codingParallelChange, error) {
	// Name-status must be git's own records. A configured external diff or
	// textconv can exit non-zero and drop the path list.
	raw, err := codingParallelGit(worktree, "diff", "--no-ext-diff", "--no-textconv", "--name-status", "-z", baseSHA)
	if err != nil {
		return nil, fmt.Errorf("worktree diff: %w", err)
	}
	changes := parseCodingParallelNameStatus(raw)
	others, err := codingParallelGit(worktree, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("worktree untracked files: %w", err)
	}
	seen := map[string]bool{}
	for _, change := range changes {
		seen[change.path] = true
	}
	for _, path := range bytes.Split(others, []byte{0}) {
		rel := filepath.ToSlash(string(path))
		if rel == "" || seen[rel] {
			continue
		}
		changes = append(changes, codingParallelChange{path: rel})
	}
	return changes, nil
}

func parseCodingParallelNameStatus(raw []byte) []codingParallelChange {
	parts := bytes.Split(raw, []byte{0})
	var changes []codingParallelChange
	for i := 0; i < len(parts); {
		if len(parts[i]) == 0 {
			i++
			continue
		}
		status := string(parts[i])
		i++
		if i >= len(parts) {
			break
		}
		first := filepath.ToSlash(string(parts[i]))
		i++
		if first == "" {
			continue
		}
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			if i >= len(parts) {
				break
			}
			second := filepath.ToSlash(string(parts[i]))
			i++
			// Rename removes the old path. Copy keeps it; deleting the source
			// would drop a file the worktree still has.
			if strings.HasPrefix(status, "R") {
				changes = append(changes, codingParallelChange{path: first, deleted: true})
			}
			if second != "" {
				changes = append(changes, codingParallelChange{path: second})
			}
			continue
		}
		changes = append(changes, codingParallelChange{
			path:    first,
			deleted: strings.HasPrefix(status, "D"),
		})
	}
	return changes
}

func applyCodingParallelFile(primaryRoot, worktree, baseSHA string, change codingParallelChange) (applied bool, conflict bool, err error) {
	rel := change.path
	primaryPath := filepath.Join(primaryRoot, filepath.FromSlash(rel))
	theirPath := filepath.Join(worktree, filepath.FromSlash(rel))
	// A symlink parent is not a normal directory. Writing or deleting through
	// it lands outside the tree the path string names.
	if !codingParallelWithinRoot(primaryRoot, primaryPath) || !codingParallelWithinRoot(worktree, theirPath) {
		return false, true, nil
	}
	base, baseOK, err := codingParallelBlob(primaryRoot, baseSHA, rel)
	if err != nil {
		return false, false, err
	}
	return applyCodingParallelFileOnce(primaryPath, theirPath, base, baseOK, change.deleted, true)
}

func applyCodingParallelFileOnce(primaryPath, theirPath string, base []byte, baseOK, deleted, allowRetry bool) (bool, bool, error) {
	var applied, conflict bool
	err := tool.WithTextFileMutation(primaryPath, func() error {
		if codingParallelIrregular(primaryPath) || codingParallelIrregular(theirPath) {
			// A directory or symlink is not a 3-way text merge. Keep the
			// primary bytes and let the remaining files land.
			conflict = true
			return nil
		}
		primary, primaryOK, readErr := readCodingParallelFile(primaryPath)
		if readErr != nil {
			return readErr
		}
		theirs, theirOK, readErr := readCodingParallelFile(theirPath)
		if readErr != nil {
			return readErr
		}
		if deleted || !theirOK {
			if !primaryOK {
				return nil
			}
			if baseOK && codingParallelSameText(primary, base) {
				if removeErr := codingParallelRemoveFile(primaryPath); removeErr != nil {
					return removeErr
				}
				applied = true
				return nil
			}
			conflict = true
			return nil
		}
		if primaryOK && codingParallelSameText(primary, theirs) {
			// Same text. A mode-only edit still has to land when the holder
			// left the bytes at the base: the worktree mode replaces it.
			// Once the holder has edited the text, keep the primary mode.
			if baseOK && codingParallelSameText(primary, base) {
				want := codingParallelPerm(theirPath)
				if want != codingParallelPerm(primaryPath) {
					if chmodErr := os.Chmod(primaryPath, want); chmodErr != nil {
						return chmodErr
					}
					applied = true
				}
			}
			return nil
		}
		if !primaryOK && !baseOK {
			if writeErr := writeCodingParallelFile(primaryPath, theirs, codingParallelPerm(theirPath)); writeErr != nil {
				return writeErr
			}
			applied = true
			return nil
		}
		if !primaryOK && baseOK {
			conflict = true
			return nil
		}
		if baseOK && codingParallelSameText(primary, base) {
			if writeErr := writeCodingParallelReplacing(primaryPath, theirs, codingParallelPerm(primaryPath)); writeErr != nil {
				return writeErr
			}
			applied = true
			return nil
		}
		if codingParallelBinary(base) || codingParallelBinary(primary) || codingParallelBinary(theirs) {
			conflict = true
			return nil
		}
		// The committed blob is LF. On Windows the working tree is often
		// CRLF, and merge-file would then conflict on every edited line.
		// Compare and merge the LF form, then write the primary's newline style.
		merged, clean, mergeErr := codingParallelMergeFile(codingParallelLF(base), codingParallelLF(primary), codingParallelLF(theirs))
		if mergeErr != nil {
			return mergeErr
		}
		if !clean {
			conflict = true
			return nil
		}
		if codingParallelUseCRLF(primary) {
			merged = codingParallelToCRLF(merged)
		}
		again, againOK, readErr := readCodingParallelFile(primaryPath)
		if readErr != nil {
			return readErr
		}
		if !againOK || !bytes.Equal(again, primary) {
			if allowRetry {
				return errCodingParallelRetry
			}
			conflict = true
			return nil
		}
		if writeErr := writeCodingParallelReplacing(primaryPath, merged, codingParallelPerm(primaryPath)); writeErr != nil {
			return writeErr
		}
		applied = true
		return nil
	})
	if errors.Is(err, errCodingParallelRetry) && allowRetry {
		return applyCodingParallelFileOnce(primaryPath, theirPath, base, baseOK, deleted, false)
	}
	return applied, conflict, err
}

var errCodingParallelRetry = errors.New("primary file changed during merge")

// codingParallelAllowWrite clears a read-only attribute. The merge has
// already decided this file is being replaced; a checkout bit must not turn
// that decision into a conflict.
func codingParallelAllowWrite(path string) error {
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0o200 != 0 {
		return nil
	}
	return os.Chmod(path, info.Mode().Perm()|0o200)
}

func codingParallelRemoveFile(path string) error {
	if err := codingParallelAllowWrite(path); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// writeCodingParallelReplacing writes a file the merge is allowed to replace.
// An existing read-only file is made writable for the write, then restored to
// the mode the caller asked to keep.
func writeCodingParallelReplacing(path string, content []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o644
	}
	if err := codingParallelAllowWrite(path); err != nil {
		return err
	}
	if err := writeCodingParallelFile(path, content, mode); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func writeCodingParallelFile(path string, content []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o644
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, mode)
}

// codingParallelPerm is the permission bits to keep on a file this merge
// writes. A new file takes the worktree mode so an executable script stays
// executable. An existing file is passed its own mode; the create flag is
// what applies when the file is being created.
func codingParallelPerm(path string) os.FileMode {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return 0o644
	}
	perm := info.Mode().Perm()
	if perm == 0 {
		return 0o644
	}
	return perm
}

func readCodingParallelFile(path string) ([]byte, bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return content, true, nil
}

func codingParallelSameText(left, right []byte) bool {
	return bytes.Equal(codingParallelLF(left), codingParallelLF(right))
}

func codingParallelLF(content []byte) []byte {
	if !bytes.Contains(content, []byte("\r\n")) {
		return content
	}
	return bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
}

func codingParallelUseCRLF(content []byte) bool {
	if !bytes.Contains(content, []byte("\r\n")) {
		return false
	}
	rest := bytes.ReplaceAll(content, []byte("\r\n"), nil)
	return !bytes.Contains(rest, []byte("\n"))
}

func codingParallelToCRLF(content []byte) []byte {
	if bytes.Contains(content, []byte("\r\n")) || !bytes.Contains(content, []byte("\n")) {
		return content
	}
	return bytes.ReplaceAll(content, []byte("\n"), []byte("\r\n"))
}

// codingParallelWithinRoot reports whether path stays inside root without
// following a symlink or a Windows junction. Missing tail components are
// allowed: the merge creates them. An existing link is not, because the
// write would follow it out of the tree.
func codingParallelWithinRoot(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	if rel == "." {
		return true
	}
	var names []string
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return false
		}
		names = append(names, part)
	}
	cur := root
	for i, part := range names {
		cur = filepath.Join(cur, part)
		info, statErr := os.Lstat(cur)
		if statErr != nil {
			// The merge creates a missing tail. Any other stat failure stays
			// closed: the path is not proven to be inside the tree.
			if errors.Is(statErr, os.ErrNotExist) {
				return true
			}
			return false
		}
		// A symlink or a Windows junction is not a real directory. Lstat
		// leaves ModeSymlink unset for a junction, but Readlink still
		// returns its target, and a write through that parent leaves the tree.
		if info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		if _, linkErr := os.Readlink(cur); linkErr == nil {
			return false
		}
		last := i == len(names)-1
		if !info.IsDir() && (!last || !info.Mode().IsRegular()) {
			return false
		}
	}
	return true
}

func codingParallelIrregular(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return !info.Mode().IsRegular()
}

func codingParallelRelPath(rel string) (string, bool) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", false
	}
	rel = filepath.Clean(filepath.FromSlash(rel))
	if rel == "." || filepath.IsAbs(rel) || strings.HasPrefix(rel, string(filepath.Separator)) {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func codingParallelBinary(content []byte) bool {
	n := len(content)
	if n > 8000 {
		n = 8000
	}
	return bytes.IndexByte(content[:n], 0) >= 0
}

func codingParallelBlob(repo, sha, rel string) ([]byte, bool, error) {
	// rev-parse --verify --quiet exits 1 with empty stderr both when the
	// path is absent and when the commit itself is missing. Confirm the
	// commit before treating that status as "not in the tree".
	spec := sha + ":" + rel
	if _, err := codingParallelGit(repo, "rev-parse", "--verify", "--quiet", spec); err != nil {
		if !codingParallelGitMissing(err) {
			return nil, false, err
		}
		if _, commitErr := codingParallelGit(repo, "rev-parse", "--verify", "--quiet", sha+"^{commit}"); commitErr != nil {
			return nil, false, commitErr
		}
		return nil, false, nil
	}
	out, err := codingParallelGit(repo, "cat-file", "blob", spec)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func codingParallelGitMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.TrimSpace(err.Error())
	return msg == "exit status 1" || strings.HasPrefix(msg, "exit status 1\n") || strings.HasPrefix(msg, "exit status 1 ")
}

func codingParallelMergeFile(base, ours, theirs []byte) ([]byte, bool, error) {
	dir, err := os.MkdirTemp("", "maclaw-merge-file-")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(dir)
	basePath := filepath.Join(dir, "base")
	oursPath := filepath.Join(dir, "ours")
	theirPath := filepath.Join(dir, "theirs")
	if err := os.WriteFile(basePath, base, 0o644); err != nil {
		return nil, false, err
	}
	if err := os.WriteFile(oursPath, ours, 0o644); err != nil {
		return nil, false, err
	}
	if err := os.WriteFile(theirPath, theirs, 0o644); err != nil {
		return nil, false, err
	}
	cmd := exec.Command("git", "merge-file", "-p", oursPath, basePath, theirPath)
	out, err := cmd.Output()
	if err == nil {
		return out, true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() > 0 {
		return nil, false, nil
	}
	return nil, false, err
}

func codingParallelGit(dir string, args ...string) ([]byte, error) {
	// --no-pager and color.ui=false keep script output stable. A pager.diff
	// or color.ui=always config would otherwise stall the merge or paint
	// status letters the parser does not understand.
	cmd := exec.Command("git", append([]string{"--no-pager", "-C", dir, "-c", "color.ui=false"}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
		"LC_ALL=C",
		"LANG=C",
	)
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		msg := strings.TrimSpace(string(exitErr.Stderr))
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return nil, err
}

func codingParallelGitText(dir string, args ...string) (string, error) {
	out, err := codingParallelGit(dir, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// codingSpawnCheckoutSnap is the deferred checkout before a worker runs.
// git stash create records tracked content without moving HEAD or the
// worktree. Untracked files are not in that commit, so their bytes are kept
// beside it. Rollback restores only the paths the worker changed.
type codingSpawnCheckoutSnap struct {
	dir       string
	gitRoot   string
	sha       string
	untracked map[string]codingSpawnUntrackedFile
}

type codingSpawnUntrackedFile struct {
	body   []byte
	link   string
	linked bool
}

type codingSpawnUntrackedDelta struct {
	path   string
	before codingSpawnUntrackedFile
	had    bool
}

func codingSpawnSnapshotCheckout(dir string) (*codingSpawnCheckoutSnap, error) {
	root, err := codingParallelGitText(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	root = filepath.Clean(root)
	sha, err := codingParallelGitText(root, "stash", "create")
	if err != nil {
		return nil, err
	}
	if sha == "" {
		sha, err = codingParallelGitText(root, "rev-parse", "HEAD")
		if err != nil {
			return nil, err
		}
	}
	untracked, err := codingSpawnReadUntracked(root)
	if err != nil {
		return nil, err
	}
	return &codingSpawnCheckoutSnap{dir: root, gitRoot: root, sha: sha, untracked: untracked}, nil
}

// codingSpawnProjectPaths rewrites repo-relative git names into the worker's
// project directory. A file outside that directory is not in the declared
// write-set, even when its repo-relative name matches a declared basename.
func codingSpawnProjectPaths(gitRoot, project string, changed []string) ([]string, error) {
	gitRoot = filepath.Clean(gitRoot)
	project = filepath.Clean(project)
	if project == "" || project == "." {
		project = gitRoot
	}
	out := make([]string, 0, len(changed))
	for _, rel := range changed {
		rel = filepath.ToSlash(strings.TrimSpace(rel))
		if rel == "" {
			continue
		}
		abs := filepath.Join(gitRoot, filepath.FromSlash(rel))
		projRel, err := filepath.Rel(project, abs)
		if err != nil {
			return nil, err
		}
		projRel = filepath.ToSlash(projRel)
		if projRel == ".." || strings.HasPrefix(projRel, "../") {
			return nil, fmt.Errorf("worktree changed undeclared path %q", rel)
		}
		out = append(out, projRel)
	}
	return out, nil
}

func codingSpawnSafeRel(rel string) (string, error) {
	cleaned, ok := codingParallelRelPath(rel)
	if !ok {
		return "", fmt.Errorf("checkout path %q escapes the repository", rel)
	}
	return cleaned, nil
}

func codingSpawnReadUntracked(root string) (map[string]codingSpawnUntrackedFile, error) {
	raw, err := codingParallelGit(root, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	out := map[string]codingSpawnUntrackedFile{}
	for _, part := range bytes.Split(raw, []byte{0}) {
		rawRel := strings.TrimSpace(filepath.ToSlash(string(part)))
		if rawRel == "" {
			continue
		}
		rel, relErr := codingSpawnSafeRel(rawRel)
		if relErr != nil {
			return nil, relErr
		}
		path := filepath.Join(root, filepath.FromSlash(rel))
		// Parents must be real directories. The final entry may be a symlink;
		// that link is recorded and is not followed.
		parent := filepath.Dir(path)
		if filepath.Clean(parent) != filepath.Clean(root) && !codingParallelWithinRoot(root, parent) {
			return nil, fmt.Errorf("untracked path %s escapes the checkout", rel)
		}
		info, statErr := os.Lstat(path)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}
			return nil, statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, linkErr := os.Readlink(path)
			if linkErr != nil {
				return nil, linkErr
			}
			out[rel] = codingSpawnUntrackedFile{link: target, linked: true}
			continue
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("untracked path %s is not a regular file", rel)
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		out[rel] = codingSpawnUntrackedFile{body: body}
	}
	return out, nil
}

func (s *codingSpawnCheckoutSnap) trackedChanges() ([]codingParallelChange, error) {
	if s == nil {
		return nil, fmt.Errorf("missing checkout snapshot")
	}
	raw, err := codingParallelGit(s.dir, "diff", "--no-ext-diff", "--no-textconv", "--name-status", "-z", s.sha)
	if err != nil {
		return nil, err
	}
	changes := parseCodingParallelNameStatus(raw)
	for i, change := range changes {
		rel, relErr := codingSpawnSafeRel(change.path)
		if relErr != nil {
			return nil, relErr
		}
		changes[i].path = rel
	}
	return changes, nil
}

func (s *codingSpawnCheckoutSnap) untrackedDeltas() ([]codingSpawnUntrackedDelta, error) {
	if s == nil {
		return nil, fmt.Errorf("missing checkout snapshot")
	}
	now, err := codingSpawnReadUntracked(s.dir)
	if err != nil {
		return nil, err
	}
	var deltas []codingSpawnUntrackedDelta
	seen := map[string]bool{}
	for path, before := range s.untracked {
		seen[path] = true
		after, has := now[path]
		if has && codingSpawnUntrackedSame(before, after) {
			continue
		}
		deltas = append(deltas, codingSpawnUntrackedDelta{path: path, before: before, had: true})
	}
	for path := range now {
		if seen[path] {
			continue
		}
		deltas = append(deltas, codingSpawnUntrackedDelta{path: path})
	}
	return deltas, nil
}

func codingSpawnUntrackedSame(left, right codingSpawnUntrackedFile) bool {
	if left.linked || right.linked {
		return left.linked && right.linked && left.link == right.link
	}
	return bytes.Equal(left.body, right.body)
}

func (s *codingSpawnCheckoutSnap) changedPaths() ([]string, error) {
	tracked, err := s.trackedChanges()
	if err != nil {
		return nil, err
	}
	deltas, err := s.untrackedDeltas()
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(tracked)+len(deltas))
	for _, change := range tracked {
		if change.path != "" {
			paths = append(paths, change.path)
		}
	}
	for _, delta := range deltas {
		if delta.path != "" {
			paths = append(paths, delta.path)
		}
	}
	return paths, nil
}

func (s *codingSpawnCheckoutSnap) rollback() error {
	if s == nil {
		return fmt.Errorf("missing checkout snapshot")
	}
	tracked, trackErr := s.trackedChanges()
	deltas, deltaErr := s.untrackedDeltas()
	var errs []error
	if trackErr != nil {
		errs = append(errs, trackErr)
	}
	if deltaErr != nil {
		errs = append(errs, deltaErr)
	}
	for _, change := range tracked {
		if err := s.restoreTracked(change); err != nil {
			errs = append(errs, err)
		}
	}
	for _, delta := range deltas {
		if err := s.restoreUntracked(delta); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *codingSpawnCheckoutSnap) restoreTracked(change codingParallelChange) error {
	rel, err := codingSpawnSafeRel(change.path)
	if err != nil {
		return err
	}
	exists, err := codingSpawnBlobInCommit(s.dir, s.sha, rel)
	if err != nil {
		return err
	}
	if exists {
		_, err = codingParallelGit(s.dir, "checkout", s.sha, "--", rel)
		return err
	}
	return codingSpawnRemovePath(s.dir, rel)
}

func codingSpawnBlobInCommit(dir, sha, rel string) (bool, error) {
	_, err := codingParallelGit(dir, "rev-parse", "--verify", "--quiet", sha+":"+rel)
	if err == nil {
		return true, nil
	}
	if codingParallelGitMissing(err) {
		return false, nil
	}
	return false, err
}

func codingSpawnContainPath(root, path string) error {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to touch %s outside the checkout", path)
	}
	parent := filepath.Dir(path)
	if parent != root && !codingParallelWithinRoot(root, parent) {
		return fmt.Errorf("refusing to touch %s outside the checkout", path)
	}
	return nil
}

func codingSpawnRemovePath(root, rel string) error {
	if _, err := codingParallelGit(root, "rm", "-f", "--ignore-unmatch", "--", rel); err != nil {
		return err
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := codingSpawnContainPath(root, path); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *codingSpawnCheckoutSnap) restoreUntracked(delta codingSpawnUntrackedDelta) error {
	rel, err := codingSpawnSafeRel(delta.path)
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, filepath.FromSlash(rel))
	if err := codingSpawnContainPath(s.dir, path); err != nil {
		return err
	}
	if !delta.had {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if delta.before.linked {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.Symlink(delta.before.link, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, delta.before.body, 0o644)
}
