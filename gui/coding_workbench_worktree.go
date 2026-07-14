package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// Worktree modes for pure-coding multi-step execution.
//   - auto: isolate only when a parallel wave needs write-capable steps (default)
//   - always: every non-explore step runs in a git worktree then merges back
//   - off: never use worktrees
const (
	codingWorktreeModeAuto   = "auto"
	codingWorktreeModeAlways = "always"
	codingWorktreeModeOff    = "off"
)

// codingWorkbenchWorktree is one isolated git worktree for a plan step.
type codingWorkbenchWorktree struct {
	GitRoot    string
	Path       string // worktree checkout root
	ProjectPath string // effective project path inside worktree (may equal Path)
	Branch     string
	StepIndex  int
	Label      string
	created    bool
}

var codingWorkbenchWorktreeMu sync.Mutex

func normalizeCodingWorktreeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case codingWorktreeModeAlways, "on", "yes", "true":
		return codingWorktreeModeAlways
	case codingWorktreeModeOff, "none", "disable", "disabled", "false":
		return codingWorktreeModeOff
	case codingWorktreeModeAuto, "", "default":
		return codingWorktreeModeAuto
	default:
		return codingWorktreeModeAuto
	}
}

func (h *IMMessageHandler) getStickyCodingWorktreeMode(userID string) string {
	if h == nil {
		return codingWorktreeModeAuto
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	return normalizeCodingWorktreeMode(mem.WorktreeMode)
}

func (h *IMMessageHandler) setStickyCodingWorktreeMode(userID, mode string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	mode = normalizeCodingWorktreeMode(mode)
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.WorktreeMode = mode
	})
}

// shouldUseCodingWorktree decides whether a plan step should run in isolation.
// explore-only steps never need write isolation; implement/fix steps may.
//
// waveSize is the TaskRunner's actual concurrent wave size for this invocation
// (1 = alone, >1 = true parallel). Prefer this over dependsOn heuristics.
// maxParallel remains a cap hint when waveSize is 0 (unknown/legacy callers).
func shouldUseCodingWorktree(mode string, planned bool, title, description string, maxParallel, waveSize int, dependsOn []int) bool {
	mode = normalizeCodingWorktreeMode(mode)
	if mode == codingWorktreeModeOff {
		return false
	}
	// Explore/read-only steps stay on the main tree (cheap, no merge risk).
	if isCodingPlanExploreOnlyStep(title, description) {
		return false
	}
	if mode == codingWorktreeModeAlways {
		return true // isolate every write step
	}
	// auto: only when this step actually shares a parallel wave with others.
	if !planned {
		return false
	}
	if waveSize > 1 {
		return true
	}
	if waveSize == 1 {
		return false // runner says alone
	}
	// Legacy fallback when WaveSize not set: independent + MaxParallel>1.
	if maxParallel <= 1 {
		return false
	}
	return len(dependsOn) == 0
}

func isCodingPlanExploreOnlyStep(title, description string) bool {
	title = strings.ToLower(strings.TrimSpace(title))
	desc := strings.ToLower(strings.TrimSpace(description))
	if i := strings.Index(desc, "\n\n## overall request"); i >= 0 {
		desc = strings.TrimSpace(desc[:i])
	}
	blob := title + " " + desc
	for _, kw := range []string{
		"implement", "实现", "编码", "fix", "修复", "write", "edit",
		"verify", "test", "build", "验证", "测试", "构建", "编译", "验收",
	} {
		if strings.Contains(blob, kw) {
			return false
		}
	}
	for _, kw := range []string{
		"explor", "探查", "定位", "map ", "read", "阅读", "survey",
		"了解", "分析现状", "inspect", "locate",
	} {
		if strings.Contains(blob, kw) {
			return true
		}
	}
	return false
}

// createCodingWorkbenchWorktree creates a detached branch worktree under the
// system temp dir (maclaw-coding-worktrees). Returns nil,nil when the project
// is not a usable git repo (caller should fall back to main path).
func createCodingWorkbenchWorktree(projectPath string, stepIndex int, label string) (*codingWorkbenchWorktree, error) {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return nil, fmt.Errorf("empty project path")
	}
	abs, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, err
	}
	// Refuse to create worktrees inside another worktree path (nested isolation).
	if strings.Contains(filepath.ToSlash(abs), "maclaw-coding-worktrees") {
		return nil, fmt.Errorf("refusing nested coding worktree under %s", abs)
	}
	gitRoot, ok := remote.DetectGitWorkspaceRoot(abs)
	if !ok {
		return nil, nil // not a git repo
	}
	if !remote.GitRepoHasCommits(gitRoot) {
		return nil, nil
	}

	label = remote.SanitizeWorkspaceName(label)
	if label == "" {
		label = fmt.Sprintf("t%d", stepIndex)
	}
	id := fmt.Sprintf("%d-%d-%s", stepIndex, time.Now().UnixNano()%1e9, label)
	if len(id) > 48 {
		id = id[:48]
	}
	branch := "maclaw/coding-" + id
	rootDir := filepath.Join(os.TempDir(), "maclaw-coding-worktrees")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir worktree root: %w", err)
	}
	wtPath := filepath.Join(rootDir, remote.SanitizeWorkspaceName(id))

	codingWorkbenchWorktreeMu.Lock()
	defer codingWorkbenchWorktreeMu.Unlock()

	_ = os.RemoveAll(wtPath)
	// Best-effort: drop leftover branch from a previous crash with same id (rare).
	_ = remote.RunGit(gitRoot, "branch", "-D", branch)
	// Prune stale worktree metadata so "already registered" cannot block add.
	_ = remote.RunGit(gitRoot, "worktree", "prune")

	if err := remote.RunGit(gitRoot, "worktree", "add", "-b", branch, wtPath, "HEAD"); err != nil {
		// One retry after force-remove path + prune (Windows file lock races).
		_ = os.RemoveAll(wtPath)
		_ = remote.RunGit(gitRoot, "worktree", "prune")
		if err2 := remote.RunGit(gitRoot, "worktree", "add", "-b", branch, wtPath, "HEAD"); err2 != nil {
			return nil, fmt.Errorf("git worktree add: %w", err2)
		}
	}

	// Map project subpath if user opened a subdirectory of the repo.
	prepared := wtPath
	if rel, relErr := filepath.Rel(gitRoot, abs); relErr == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		prepared = filepath.Join(wtPath, rel)
		if err := os.MkdirAll(prepared, 0o755); err != nil {
			_ = remote.RunGit(gitRoot, "worktree", "remove", "--force", wtPath)
			_ = os.RemoveAll(wtPath)
			_ = remote.RunGit(gitRoot, "branch", "-D", branch)
			return nil, err
		}
	}

	log.Printf("[coding-worktree] created step=%d branch=%s path=%s", stepIndex, branch, prepared)
	return &codingWorkbenchWorktree{
		GitRoot:     gitRoot,
		Path:        wtPath,
		ProjectPath: prepared,
		Branch:      branch,
		StepIndex:   stepIndex,
		Label:       label,
		created:     true,
	}, nil
}

// mergeBack commits worktree changes (if any) and integrates into the main tree.
// Prefers cherry-pick when main is clean; otherwise copies changed files (main
// pure-coding worktrees are often dirty with uncommitted prior steps).
func (w *codingWorkbenchWorktree) mergeBack(mainProjectPath string) (merged bool, summary string, err error) {
	if w == nil || !w.created {
		return false, "", nil
	}
	codingWorkbenchWorktreeMu.Lock()
	defer codingWorkbenchWorktreeMu.Unlock()

	// Stage + commit in worktree if dirty.
	status, stErr := remote.RunGitOutput(w.Path, "status", "--porcelain")
	if stErr != nil {
		return false, "", fmt.Errorf("worktree status: %w", stErr)
	}
	if strings.TrimSpace(status) == "" {
		return false, "worktree clean (no file changes to merge)", nil
	}
	_ = remote.RunGit(w.Path, "add", "-A")
	msg := fmt.Sprintf("coding workbench T%d: %s", w.StepIndex, strings.TrimSpace(w.Label))
	if strings.TrimSpace(w.Label) == "" {
		msg = fmt.Sprintf("coding workbench T%d", w.StepIndex)
	}
	if err := remote.RunGit(w.Path, "commit", "-m", msg, "--no-verify"); err != nil {
		status2, _ := remote.RunGitOutput(w.Path, "status", "--porcelain")
		if strings.TrimSpace(status2) == "" {
			return false, "worktree changes not commit-able (ignored?)", nil
		}
		return false, "", fmt.Errorf("worktree commit: %w", err)
	}
	hash, _ := remote.RunGitOutput(w.Path, "rev-parse", "--short", "HEAD")
	hash = strings.TrimSpace(hash)

	mainRoot := w.GitRoot
	if mainProjectPath != "" {
		if root, ok := remote.DetectGitWorkspaceRoot(mainProjectPath); ok {
			mainRoot = root
		}
	}

	// Changed files in the worktree tip commit (vs parent).
	filesOut, filesErr := remote.RunGitOutput(w.Path, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD")
	if filesErr != nil || strings.TrimSpace(filesOut) == "" {
		// Fallback: all non-clean paths before commit already staged — use show.
		filesOut, _ = remote.RunGitOutput(w.Path, "show", "--pretty=format:", "--name-only", "HEAD")
	}
	var changed []string
	for _, line := range strings.Split(filesOut, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			changed = append(changed, line)
		}
	}
	if len(changed) == 0 {
		return false, "worktree commit had no file list", nil
	}

	mainStatus, _ := remote.RunGitOutput(mainRoot, "status", "--porcelain")
	mainClean := strings.TrimSpace(mainStatus) == ""
	if mainClean {
		if err := remote.RunGit(mainRoot, "cherry-pick", "--allow-empty", w.Branch); err != nil {
			_ = remote.RunGit(mainRoot, "cherry-pick", "--abort")
			// Fall through to file copy.
			log.Printf("[coding-worktree] cherry-pick failed, falling back to file copy: %v", err)
		} else {
			return true, fmt.Sprintf("merged worktree T%d (%s) via cherry-pick %s (%d files)", w.StepIndex, w.Branch, hash, len(changed)), nil
		}
	}

	// File-copy merge: map paths relative to git root into main checkout.
	copied := 0
	for _, rel := range changed {
		rel = filepath.FromSlash(rel)
		src := filepath.Join(w.Path, rel)
		dst := filepath.Join(mainRoot, rel)
		data, readErr := os.ReadFile(src)
		if readErr != nil {
			// Deleted file in worktree — remove on main if present.
			if os.IsNotExist(readErr) {
				_ = os.Remove(dst)
				copied++
				continue
			}
			return false, "", fmt.Errorf("read worktree file %s: %w", rel, readErr)
		}
		mode := os.FileMode(0o644)
		if info, stErr := os.Stat(src); stErr == nil {
			mode = info.Mode()
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return false, "", err
		}
		if err := os.WriteFile(dst, data, mode); err != nil {
			return false, "", fmt.Errorf("write main file %s: %w", rel, err)
		}
		copied++
	}
	return true, fmt.Sprintf("merged worktree T%d (%s) via file-copy %s (%d files, main dirty=%v)", w.StepIndex, w.Branch, hash, copied, !mainClean), nil
}

// cleanup removes the worktree and branch. Safe to call multiple times.
// If keepOnDisk is true, only detaches registration (for conflict inspection).
func (w *codingWorkbenchWorktree) cleanup(keepOnDisk bool) {
	if w == nil || !w.created {
		return
	}
	codingWorkbenchWorktreeMu.Lock()
	defer codingWorkbenchWorktreeMu.Unlock()
	if keepOnDisk {
		log.Printf("[coding-worktree] keep on disk for inspection: %s branch=%s", w.Path, w.Branch)
		w.created = false
		return
	}
	if w.GitRoot != "" && w.Path != "" {
		_ = remote.RunGit(w.GitRoot, "worktree", "remove", "--force", w.Path)
		_ = os.RemoveAll(w.Path)
		if w.Branch != "" {
			_ = remote.RunGit(w.GitRoot, "branch", "-D", w.Branch)
		}
		_ = remote.RunGit(w.GitRoot, "worktree", "prune")
	}
	w.created = false
	log.Printf("[coding-worktree] cleaned step=%d branch=%s", w.StepIndex, w.Branch)
}

// remapWorktreePaths rewrites absolute paths under worktree root to main project paths.
func remapWorktreePaths(paths []string, worktreeProject, mainProject string) []string {
	if len(paths) == 0 {
		return paths
	}
	worktreeProject = filepath.Clean(worktreeProject)
	mainProject = filepath.Clean(mainProject)
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		clean := filepath.Clean(p)
		if rel, err := filepath.Rel(worktreeProject, clean); err == nil && !strings.HasPrefix(rel, "..") {
			out = append(out, filepath.Join(mainProject, rel))
			continue
		}
		// Also try worktree root vs project subpath.
		out = append(out, p)
	}
	return uniqueSortedSubAgentStrings(out)
}

// listCodingWorkbenchWorktrees lists maclaw coding worktrees for a git root.
func listCodingWorkbenchWorktrees(projectPath string) (string, error) {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return "", fmt.Errorf("empty project path")
	}
	gitRoot, ok := remote.DetectGitWorkspaceRoot(projectPath)
	if !ok {
		return "Not a git repository — worktrees unavailable.", nil
	}
	out, err := remote.RunGitOutput(gitRoot, "worktree", "list", "--porcelain")
	if err != nil {
		// fallback plain list
		out, err = remote.RunGitOutput(gitRoot, "worktree", "list")
		if err != nil {
			return "", err
		}
	}
	var b strings.Builder
	b.WriteString("## Git worktrees\n\n")
	b.WriteString("```\n")
	b.WriteString(out)
	b.WriteString("\n```\n")
	// Highlight maclaw coding worktrees under temp.
	tmpRoot := filepath.Join(os.TempDir(), "maclaw-coding-worktrees")
	b.WriteString("\nCoding worktree root: `")
	b.WriteString(tmpRoot)
	b.WriteString("`\n")
	return b.String(), nil
}

// pruneCodingWorkbenchWorktrees removes orphaned maclaw/coding-* worktrees.
func pruneCodingWorkbenchWorktrees(projectPath string) (string, error) {
	projectPath = strings.TrimSpace(projectPath)
	gitRoot, ok := remote.DetectGitWorkspaceRoot(projectPath)
	if !ok {
		return "Not a git repository.", nil
	}
	codingWorkbenchWorktreeMu.Lock()
	defer codingWorkbenchWorktreeMu.Unlock()

	list, err := remote.RunGitOutput(gitRoot, "worktree", "list", "--porcelain")
	if err != nil {
		_ = remote.RunGit(gitRoot, "worktree", "prune")
		return "worktree prune requested (list failed).", nil
	}
	// Parse porcelain for worktree paths with maclaw/coding- branches.
	var removed []string
	var curPath, curBranch string
	flush := func() {
		if curPath == "" {
			return
		}
		isCoding := strings.Contains(curBranch, "maclaw/coding-") ||
			strings.Contains(filepath.ToSlash(curPath), "maclaw-coding-worktrees")
		if isCoding {
			_ = remote.RunGit(gitRoot, "worktree", "remove", "--force", curPath)
			_ = os.RemoveAll(curPath)
			if curBranch != "" {
				br := strings.TrimPrefix(curBranch, "refs/heads/")
				_ = remote.RunGit(gitRoot, "branch", "-D", br)
			}
			removed = append(removed, curPath)
		}
		curPath, curBranch = "", ""
	}
	for _, line := range strings.Split(list, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "worktree ") {
			flush()
			curPath = strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
		} else if strings.HasPrefix(line, "branch ") {
			curBranch = strings.TrimSpace(strings.TrimPrefix(line, "branch "))
		}
	}
	flush()
	_ = remote.RunGit(gitRoot, "worktree", "prune")
	if len(removed) == 0 {
		return "No maclaw coding worktrees to prune.", nil
	}
	return "Pruned coding worktrees:\n- " + strings.Join(removed, "\n- "), nil
}

func (h *IMMessageHandler) rememberCodingWorktree(userID string, wt *codingWorkbenchWorktree, note string) {
	if h == nil || wt == nil || userID == "" {
		return
	}
	entry := fmt.Sprintf("T%d %s → %s", wt.StepIndex, wt.Branch, wt.Path)
	if note != "" {
		entry = entry + " (" + note + ")"
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.WorktreeNotes = append(mem.WorktreeNotes, entry)
		if len(mem.WorktreeNotes) > 12 {
			mem.WorktreeNotes = mem.WorktreeNotes[len(mem.WorktreeNotes)-12:]
		}
	})
}
