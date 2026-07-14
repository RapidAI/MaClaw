package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// codingWorkbenchConflict is a kept isolation tree after a failed merge.
type codingWorkbenchConflict struct {
	ID          string   `json:"id"`
	StepIndex   int      `json:"step_index"`
	Branch      string   `json:"branch,omitempty"`
	Path        string   `json:"path"` // worktree root or remote isolate dir
	ProjectPath string   `json:"project_path,omitempty"`
	GitRoot     string   `json:"git_root,omitempty"`
	MainProject string   `json:"main_project,omitempty"`
	Error       string   `json:"error,omitempty"`
	Kind        string   `json:"kind"` // local_worktree | remote_isolate
	Files       []string `json:"files,omitempty"`
	CreatedAt   int64    `json:"created_at"`
}

func (h *IMMessageHandler) listStickyCodingConflicts(userID string) []codingWorkbenchConflict {
	if h == nil {
		return nil
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	return append([]codingWorkbenchConflict(nil), mem.WorktreeConflicts...)
}

// stickyCodingConflictLogAppend mutates mem with one audit line (caller holds RMW).
func stickyCodingConflictLogAppend(mem *stickyCodingWorkbenchMemory, line string) {
	if mem == nil {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	entry := time.Now().Format("15:04:05") + " " + truncateRunesForSubAgent(line, 200)
	mem.ConflictLog = append(mem.ConflictLog, entry)
	if len(mem.ConflictLog) > stickyCodingConflictLogMax {
		mem.ConflictLog = mem.ConflictLog[len(mem.ConflictLog)-stickyCodingConflictLogMax:]
	}
}

// stickyCodingConflictUIPrune mutates selection/focus against remaining paths.
func stickyCodingConflictUIPrune(mem *stickyCodingWorkbenchMemory, remaining []string) {
	if mem == nil {
		return
	}
	keep := map[string]struct{}{}
	for _, f := range remaining {
		f = filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(f, "./")))
		if f != "" {
			keep[f] = struct{}{}
		}
	}
	if len(mem.ConflictSelected) > 0 {
		out := mem.ConflictSelected[:0]
		for _, s := range mem.ConflictSelected {
			if _, ok := keep[s]; ok {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			mem.ConflictSelected = nil
		} else {
			mem.ConflictSelected = append([]string(nil), out...)
		}
	}
	if mem.ConflictFocusFile != "" {
		if _, ok := keep[mem.ConflictFocusFile]; !ok {
			mem.ConflictFocusFile = ""
		}
	}
}

func (h *IMMessageHandler) storeStickyCodingConflict(userID string, c codingWorkbenchConflict) {
	h.storeStickyCodingConflictOpts(userID, c, "", nil)
}

// storeStickyCodingConflictOpts upserts a conflict and optionally appends a log
// line and prunes UI multi-select in the same sticky RMW (one disk write).
func (h *IMMessageHandler) storeStickyCodingConflictOpts(userID string, c codingWorkbenchConflict, logLine string, pruneRemaining []string) {
	if h == nil || userID == "" || strings.TrimSpace(c.Path) == "" {
		return
	}
	if c.ID == "" {
		c.ID = fmt.Sprintf("c%d-%d", c.StepIndex, time.Now().UnixNano()%1e9)
	}
	if c.CreatedAt == 0 {
		c.CreatedAt = time.Now().Unix()
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		out := make([]codingWorkbenchConflict, 0, len(mem.WorktreeConflicts)+1)
		for _, x := range mem.WorktreeConflicts {
			if filepath.Clean(x.Path) == filepath.Clean(c.Path) || (c.ID != "" && x.ID == c.ID) {
				continue
			}
			out = append(out, x)
		}
		out = append(out, c)
		if len(out) > 8 {
			out = out[len(out)-8:]
		}
		mem.WorktreeConflicts = out
		if pruneRemaining != nil {
			stickyCodingConflictUIPrune(mem, pruneRemaining)
		}
		if logLine != "" {
			stickyCodingConflictLogAppend(mem, logLine)
		}
	})
}

func (h *IMMessageHandler) removeStickyCodingConflict(userID, idOrPath string) (codingWorkbenchConflict, bool) {
	return h.removeStickyCodingConflictOpts(userID, idOrPath, "")
}

func (h *IMMessageHandler) removeStickyCodingConflictOpts(userID, idOrPath, logLine string) (codingWorkbenchConflict, bool) {
	if h == nil {
		return codingWorkbenchConflict{}, false
	}
	idOrPath = strings.TrimSpace(idOrPath)
	var found codingWorkbenchConflict
	ok := false
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		out := make([]codingWorkbenchConflict, 0, len(mem.WorktreeConflicts))
		for _, x := range mem.WorktreeConflicts {
			if x.ID == idOrPath || filepath.Clean(x.Path) == filepath.Clean(idOrPath) ||
				(idOrPath != "" && strings.HasSuffix(x.Path, idOrPath)) {
				found = x
				ok = true
				continue
			}
			out = append(out, x)
		}
		if ok {
			mem.WorktreeConflicts = out
			// Atomically clear conflict panel memory when the active conflict is removed.
			if mem.ConflictActiveID == found.ID || mem.ConflictActiveID == idOrPath ||
				(found.Path != "" && mem.ConflictActiveID == found.Path) {
				mem.ConflictActiveID = ""
				mem.ConflictSelected = nil
				mem.ConflictFocusFile = ""
			}
			if logLine != "" {
				stickyCodingConflictLogAppend(mem, logLine)
			}
		}
	})
	return found, ok
}

func (h *IMMessageHandler) recordLocalWorktreeConflict(userID string, wt *codingWorkbenchWorktree, mainProject, errText string) {
	if wt == nil {
		return
	}
	files := listWorktreeChangedFiles(wt.Path)
	h.storeStickyCodingConflict(userID, codingWorkbenchConflict{
		StepIndex:   wt.StepIndex,
		Branch:      wt.Branch,
		Path:        wt.Path,
		ProjectPath: wt.ProjectPath,
		GitRoot:     wt.GitRoot,
		MainProject: mainProject,
		Error:       truncateRunesForSubAgent(errText, 400),
		Kind:        "local_worktree",
		Files:       files,
	})
	// Lifecycle hook: notify project-local scripts when isolation merge fails.
	projectPath := strings.TrimSpace(wt.ProjectPath)
	if projectPath == "" {
		projectPath = strings.TrimSpace(mainProject)
	}
	if h != nil {
		h.fireCodingOnConflictHook(userID, projectPath)
	}
}

func listWorktreeChangedFiles(wtPath string) []string {
	wtPath = strings.TrimSpace(wtPath)
	if wtPath == "" {
		return nil
	}
	// Prefer tip commit file list; fall back to porcelain.
	out, err := remote.RunGitOutput(wtPath, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD")
	if err != nil || strings.TrimSpace(out) == "" {
		out, _ = remote.RunGitOutput(wtPath, "status", "--porcelain")
		return parseGitPorcelainPaths(out, 40)
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		files = append(files, line)
		if len(files) >= 40 {
			break
		}
	}
	return files
}

// parseGitPorcelainPaths extracts paths from `git status --porcelain` lines.
// Handles "XY path", "XY orig -> path", and paths with spaces (path starts at index 3).
func parseGitPorcelainPaths(out string, limit int) []string {
	if limit <= 0 {
		limit = 40
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 4 {
			continue
		}
		// Format: XY<space>path  or  XY<space>orig -> path
		pathPart := line[3:]
		if i := strings.Index(pathPart, " -> "); i >= 0 {
			pathPart = pathPart[i+4:]
		}
		pathPart = strings.TrimSpace(pathPart)
		// Quoted paths from git: "path with spaces"
		pathPart = strings.Trim(pathPart, "\"")
		if pathPart == "" {
			continue
		}
		files = append(files, pathPart)
		if len(files) >= limit {
			break
		}
	}
	return files
}

func (h *IMMessageHandler) findStickyCodingConflict(userID, idOrPath string) (codingWorkbenchConflict, bool) {
	idOrPath = strings.TrimSpace(idOrPath)
	for _, c := range h.listStickyCodingConflicts(userID) {
		if c.ID == idOrPath || filepath.Clean(c.Path) == filepath.Clean(idOrPath) ||
			(idOrPath != "" && strings.HasSuffix(c.Path, idOrPath)) {
			return c, true
		}
	}
	return codingWorkbenchConflict{}, false
}

// codingConflictFileDiff is a side-by-side-ish summary for one conflicted path.
type codingConflictFileDiff struct {
	Path      string `json:"path"`
	Status    string `json:"status"` // modified | added | deleted | same | missing
	MainHead  string `json:"main_head,omitempty"`
	TheirHead string `json:"their_head,omitempty"`
	// BaseHead is merge-base content when available (true 3-way preview).
	BaseHead string `json:"base_head,omitempty"`
	// Unified is a compact unified-style snippet (truncated).
	Unified string `json:"unified,omitempty"`
	// ThreeWay is a compact base/main/theirs marker preview when base exists.
	ThreeWay string `json:"three_way,omitempty"`
}

// codingConflictFilePreview is a longer single-side content peek for the UI expander.
type codingConflictFilePreview struct {
	Path      string `json:"path"`
	Side      string `json:"side"` // main | theirs | base
	Content   string `json:"content,omitempty"`
	Bytes     int    `json:"bytes,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Missing   bool   `json:"missing,omitempty"`
}

// codingConflictFileTriple is a side-by-side main/theirs/base snapshot for one path.
type codingConflictFileTriple struct {
	Path   string                    `json:"path"`
	Main   codingConflictFilePreview `json:"main"`
	Theirs codingConflictFilePreview `json:"theirs"`
	Base   codingConflictFilePreview `json:"base"`
}

const codingConflictPreviewMaxBytes = 24 * 1024
// codingConflictWriteMaxBytes is the max body for manual/base write (4× preview).
const codingConflictWriteMaxBytes = codingConflictPreviewMaxBytes * 4

// getCodingConflictFileDiffs returns per-file main vs worktree previews.
func (h *IMMessageHandler) getCodingConflictFileDiffs(userID, idOrPath string, maxFiles int) ([]codingConflictFileDiff, codingWorkbenchConflict, error) {
	c, ok := h.findStickyCodingConflict(userID, idOrPath)
	if !ok {
		return nil, codingWorkbenchConflict{}, fmt.Errorf("conflict not found: %s", idOrPath)
	}
	if maxFiles <= 0 {
		maxFiles = 20
	}
	if c.Kind == "remote_isolate" {
		// Remote: list files only (no local content).
		files := c.Files
		if len(files) == 0 {
			files = listRemoteIsolateChangedFiles(h, userID, c)
		}
		out := make([]codingConflictFileDiff, 0, len(files))
		for i, f := range files {
			if i >= maxFiles {
				break
			}
			out = append(out, codingConflictFileDiff{Path: f, Status: "remote", Unified: "(remote file — use adopt to merge via SSH)"})
		}
		return out, c, nil
	}
	wtPath := c.Path
	if _, err := os.Stat(wtPath); err != nil {
		return nil, c, fmt.Errorf("conflict path missing: %s", wtPath)
	}
	gitRoot := resolveConflictGitRoot(c, userID, h)
	files := c.Files
	if len(files) == 0 {
		files = listWorktreeChangedFiles(wtPath)
	}
	out := make([]codingConflictFileDiff, 0, len(files))
	for i, rel := range files {
		if i >= maxFiles {
			break
		}
		rel = filepath.ToSlash(strings.TrimSpace(rel))
		if rel == "" {
			continue
		}
		out = append(out, buildLocalConflictFileDiffWithBranch(gitRoot, wtPath, c.Branch, rel))
	}
	return out, c, nil
}

func resolveConflictGitRoot(c codingWorkbenchConflict, userID string, h *IMMessageHandler) string {
	if g := strings.TrimSpace(c.GitRoot); g != "" {
		return g
	}
	main := strings.TrimSpace(c.MainProject)
	if main == "" && h != nil {
		main = strings.TrimSpace(h.getStickyCodingWorkbenchMemory(userID).ProjectPath)
	}
	if main != "" {
		if root, ok := remote.DetectGitWorkspaceRoot(main); ok {
			return root
		}
		return main
	}
	return ""
}

func buildLocalConflictFileDiff(gitRoot, wtPath, rel string) codingConflictFileDiff {
	return buildLocalConflictFileDiffWithBranch(gitRoot, wtPath, "", rel)
}

// buildLocalConflictFileDiffWithBranch includes merge-base when branch/gitRoot known.
func buildLocalConflictFileDiffWithBranch(gitRoot, wtPath, branch, rel string) codingConflictFileDiff {
	d := codingConflictFileDiff{Path: rel}
	mainPath := filepath.Join(gitRoot, filepath.FromSlash(rel))
	theirPath := filepath.Join(wtPath, filepath.FromSlash(rel))
	mainData, mainErr := os.ReadFile(mainPath)
	theirData, theirErr := os.ReadFile(theirPath)

	// Best-effort merge-base content for 3-way preview.
	if base, ok := readConflictMergeBaseBlob(gitRoot, wtPath, branch, rel); ok {
		d.BaseHead = truncateRunesForSubAgent(base, 300)
	}

	switch {
	case os.IsNotExist(mainErr) && theirErr == nil:
		d.Status = "added"
		d.TheirHead = truncateRunesForSubAgent(string(theirData), 400)
		d.Unified = "+ (new file in worktree)\n" + prefixLines(truncateRunesForSubAgent(string(theirData), 600), "+ ")
	case mainErr == nil && os.IsNotExist(theirErr):
		d.Status = "deleted"
		d.MainHead = truncateRunesForSubAgent(string(mainData), 400)
		d.Unified = "- (deleted in worktree)\n" + prefixLines(truncateRunesForSubAgent(string(mainData), 600), "- ")
	case mainErr != nil && theirErr != nil:
		d.Status = "missing"
		d.Unified = "both sides missing / unreadable"
	case string(mainData) == string(theirData):
		d.Status = "same"
		d.Unified = "(identical)"
	default:
		d.Status = "modified"
		d.MainHead = truncateRunesForSubAgent(string(mainData), 300)
		d.TheirHead = truncateRunesForSubAgent(string(theirData), 300)
		d.Unified = simpleUnifiedSnippet(string(mainData), string(theirData), 24)
	}
	if d.BaseHead != "" && d.Status == "modified" {
		d.ThreeWay = formatThreeWaySnippet(d.BaseHead, string(mainData), string(theirData), 18)
	}
	return d
}

// readConflictMergeBaseBlob returns file content at the merge-base of main HEAD
// and the isolation branch/worktree tip. Empty when git is unavailable.
func readConflictMergeBaseBlob(gitRoot, wtPath, branch, rel string) (string, bool) {
	gitRoot = strings.TrimSpace(gitRoot)
	rel = strings.TrimSpace(strings.TrimPrefix(filepath.ToSlash(rel), "./"))
	if gitRoot == "" || rel == "" {
		return "", false
	}
	other := strings.TrimSpace(branch)
	if other == "" && strings.TrimSpace(wtPath) != "" {
		// Resolve worktree HEAD as the "theirs" side for merge-base.
		if tip, err := remote.RunGitOutput(wtPath, "rev-parse", "HEAD"); err == nil {
			other = strings.TrimSpace(tip)
		}
	}
	if other == "" {
		// Fall back to common ancestor of HEAD and HEAD@{1} is too weak; skip.
		return "", false
	}
	base, err := remote.RunGitOutput(gitRoot, "merge-base", "HEAD", other)
	if err != nil {
		return "", false
	}
	base = strings.TrimSpace(base)
	if base == "" {
		return "", false
	}
	blob, err := remote.RunGitOutput(gitRoot, "show", base+":"+rel)
	if err != nil {
		return "", false
	}
	return blob, true
}

// formatThreeWaySnippet produces a compact base / main / theirs preview.
func formatThreeWaySnippet(base, main, their string, maxLines int) string {
	if maxLines <= 0 {
		maxLines = 18
	}
	ba := strings.Split(base, "\n")
	ma := strings.Split(main, "\n")
	th := strings.Split(their, "\n")
	n := len(ba)
	if len(ma) > n {
		n = len(ma)
	}
	if len(th) > n {
		n = len(th)
	}
	var b strings.Builder
	b.WriteString("<<< base / main / theirs (changed lines)\n")
	shown := 0
	for i := 0; i < n && shown < maxLines; i++ {
		var bb, mm, tt string
		if i < len(ba) {
			bb = ba[i]
		}
		if i < len(ma) {
			mm = ma[i]
		}
		if i < len(th) {
			tt = th[i]
		}
		if bb == mm && mm == tt {
			continue
		}
		b.WriteString(fmt.Sprintf("L%d:\n", i+1))
		if bb != mm || bb != tt {
			b.WriteString("  base:  ")
			b.WriteString(truncateRunesForSubAgent(bb, 100))
			b.WriteString("\n")
			b.WriteString("  main:  ")
			b.WriteString(truncateRunesForSubAgent(mm, 100))
			b.WriteString("\n")
			b.WriteString("  theirs:")
			b.WriteString(truncateRunesForSubAgent(tt, 100))
			b.WriteString("\n")
			shown++
		}
	}
	if shown == 0 {
		return ""
	}
	if n > maxLines {
		b.WriteString("…\n")
	}
	return b.String()
}

func prefixLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

// simpleUnifiedSnippet is a cheap line-oriented diff preview (not a full Myers diff).
func simpleUnifiedSnippet(main, their string, maxLines int) string {
	ma := strings.Split(main, "\n")
	th := strings.Split(their, "\n")
	// Align by index for short files; enough for review UI.
	n := len(ma)
	if len(th) > n {
		n = len(th)
	}
	var b strings.Builder
	shown := 0
	for i := 0; i < n && shown < maxLines; i++ {
		var a, t string
		if i < len(ma) {
			a = ma[i]
		}
		if i < len(th) {
			t = th[i]
		}
		if a == t {
			continue
		}
		if a != "" {
			b.WriteString("- ")
			b.WriteString(truncateRunesForSubAgent(a, 120))
			b.WriteString("\n")
			shown++
		}
		if t != "" && shown < maxLines {
			b.WriteString("+ ")
			b.WriteString(truncateRunesForSubAgent(t, 120))
			b.WriteString("\n")
			shown++
		}
	}
	if b.Len() == 0 {
		return "(no line-level delta in preview window)"
	}
	if n > maxLines {
		b.WriteString("…\n")
	}
	return b.String()
}

func listRemoteIsolateChangedFiles(h *IMMessageHandler, userID string, c codingWorkbenchConflict) []string {
	if h == nil {
		return nil
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	sid := strings.TrimSpace(mem.RemoteSessionID)
	if sid == "" {
		return nil
	}
	src := strings.TrimSpace(c.MainProject)
	if src == "" {
		src = strings.TrimSpace(mem.RemoteProjectDir)
	}
	if src == "" {
		src = strings.TrimSpace(mem.RemoteWorkDir)
	}
	// Best-effort: list files newer/different via diff -rq (may be heavy).
	cmd := fmt.Sprintf(
		`diff -rq %s %s 2>/dev/null | head -n 40 | sed -n 's/.*: //p; s/Files .* and \(.*\) differ/\1/p' | head -n 40; echo __END__`,
		remoteShellQuote(src),
		remoteShellQuote(c.Path),
	)
	// Simpler: just find files under isolate excluding .git
	cmd = fmt.Sprintf(
		`cd %s 2>/dev/null && find . -type f -not -path './.git/*' | head -n 40; echo __END__`,
		remoteShellQuote(c.Path),
	)
	out := h.sshExec(map[string]interface{}{
		"session_id":   sid,
		"command":      cmd,
		"wait_seconds": float64(30),
	})
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[maclaw]") || line == "__END__" {
			continue
		}
		line = strings.TrimPrefix(line, "./")
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}

// adoptCodingWorkbenchConflict force-merges all files from a kept isolation tree.
func (h *IMMessageHandler) adoptCodingWorkbenchConflict(userID, idOrPath string) (string, error) {
	return h.adoptCodingWorkbenchConflictFiles(userID, idOrPath, nil)
}

// adoptCodingWorkbenchConflictFiles merges selected files (nil/empty = all).
// Partial adopt keeps the conflict entry with remaining files.
func (h *IMMessageHandler) adoptCodingWorkbenchConflictFiles(userID, idOrPath string, onlyFiles []string) (string, error) {
	c, ok := h.findStickyCodingConflict(userID, idOrPath)
	if !ok {
		// allow path-only adopt
		c = codingWorkbenchConflict{Path: strings.TrimSpace(idOrPath), Kind: "local_worktree"}
		if c.Path == "" {
			return "", fmt.Errorf("conflict id/path required")
		}
	}
	if c.Kind == "remote_isolate" {
		return h.adoptRemoteCodingConflict(userID, c, onlyFiles)
	}
	wtPath := c.Path
	if _, err := os.Stat(wtPath); err != nil {
		return "", fmt.Errorf("conflict path missing: %s", wtPath)
	}
	gitRoot := resolveConflictGitRoot(c, userID, h)
	if gitRoot == "" {
		return "", fmt.Errorf("main project path unknown")
	}
	_ = remote.RunGit(wtPath, "add", "-A")
	_ = remote.RunGit(wtPath, "commit", "-m", "coding workbench conflict adopt", "--no-verify")
	files := c.Files
	if len(files) == 0 {
		files = listWorktreeChangedFiles(wtPath)
	}
	if len(onlyFiles) > 0 {
		files = filterFilesBySelection(files, onlyFiles)
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no changed files found in conflict worktree")
	}
	copied := 0
	for _, rel := range files {
		rel = filepath.FromSlash(strings.TrimSpace(rel))
		if rel == "" {
			continue
		}
		src := filepath.Join(wtPath, rel)
		dst := filepath.Join(gitRoot, rel)
		data, err := os.ReadFile(src)
		if err != nil {
			if os.IsNotExist(err) {
				_ = os.Remove(dst)
				copied++
				continue
			}
			return "", fmt.Errorf("read %s: %w", rel, err)
		}
		mode := os.FileMode(0o644)
		if info, stErr := os.Stat(src); stErr == nil {
			mode = info.Mode()
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(dst, data, mode); err != nil {
			return "", err
		}
		copied++
	}

	// Full adopt: cleanup. Partial: update remaining files on sticky entry.
	if len(onlyFiles) == 0 {
		id := c.ID
		if id == "" {
			id = c.Path
		}
		_, _ = h.removeStickyCodingConflictOpts(userID, id, fmt.Sprintf("adopt %s all (%d files)", c.ID, copied))
		_ = remote.RunGit(gitRoot, "worktree", "remove", "--force", wtPath)
		_ = os.RemoveAll(wtPath)
		if c.Branch != "" {
			_ = remote.RunGit(gitRoot, "branch", "-D", c.Branch)
		}
		_ = remote.RunGit(gitRoot, "worktree", "prune")
		msg := fmt.Sprintf("已采纳冲突 worktree（%d 个文件）→ `%s`\n原路径: %s", copied, gitRoot, wtPath)
		return msg, nil
	}
	// Partial: subtract adopted files from sticky.
	remaining := subtractFiles(c.Files, onlyFiles)
	if len(remaining) == 0 {
		remaining = subtractFiles(listWorktreeChangedFiles(wtPath), onlyFiles)
	}
	c.Files = remaining
	if len(remaining) == 0 {
		_, _ = h.removeStickyCodingConflictOpts(userID, c.ID, fmt.Sprintf("adopt %s partial %d files → cleared", c.ID, copied))
		_ = remote.RunGit(gitRoot, "worktree", "remove", "--force", wtPath)
		_ = os.RemoveAll(wtPath)
		if c.Branch != "" {
			_ = remote.RunGit(gitRoot, "branch", "-D", c.Branch)
		}
		return fmt.Sprintf("已部分采纳 %d 个文件；冲突已清空。", copied), nil
	}
	h.storeStickyCodingConflictOpts(userID, c,
		fmt.Sprintf("adopt %s partial %d files, remain %d", c.ID, copied, len(remaining)),
		remaining)
	return fmt.Sprintf("已部分采纳 %d 个文件，剩余 %d 个待处理。\n用 `/worktree diff %s` 继续查看。", copied, len(remaining), c.ID), nil
}

func filterFilesBySelection(all, only []string) []string {
	want := map[string]bool{}
	for _, f := range only {
		f = filepath.ToSlash(strings.TrimSpace(f))
		if f != "" {
			want[f] = true
			want[strings.TrimPrefix(f, "./")] = true
		}
	}
	var out []string
	for _, f := range all {
		key := filepath.ToSlash(strings.TrimSpace(f))
		if want[key] || want[strings.TrimPrefix(key, "./")] || want[filepath.Base(key)] {
			out = append(out, f)
		}
	}
	return out
}

func subtractFiles(all, remove []string) []string {
	drop := map[string]bool{}
	for _, f := range remove {
		f = filepath.ToSlash(strings.TrimSpace(f))
		drop[f] = true
		drop[strings.TrimPrefix(f, "./")] = true
		drop[filepath.Base(f)] = true
	}
	var out []string
	for _, f := range all {
		key := filepath.ToSlash(strings.TrimSpace(f))
		if drop[key] || drop[strings.TrimPrefix(key, "./")] || drop[filepath.Base(key)] {
			continue
		}
		out = append(out, f)
	}
	return out
}

// adoptRemoteCodingConflict merges a remote isolate back to main via SSH.
func (h *IMMessageHandler) adoptRemoteCodingConflict(userID string, c codingWorkbenchConflict, onlyFiles []string) (string, error) {
	mem := h.getStickyCodingWorkbenchMemory(userID)
	sid := strings.TrimSpace(mem.RemoteSessionID)
	if sid == "" {
		return "", fmt.Errorf("no active remote SSH session for adopt")
	}
	src := strings.TrimSpace(c.MainProject)
	if src == "" {
		src = strings.TrimSpace(mem.RemoteProjectDir)
	}
	if src == "" {
		src = strings.TrimSpace(mem.RemoteWorkDir)
	}
	if src == "" {
		return "", fmt.Errorf("remote main project dir unknown")
	}
	iso := &remoteCodingIsolate{
		SessionID:  sid,
		SourceDir:  src,
		IsolateDir: c.Path,
		StepIndex:  c.StepIndex,
		created:    true,
	}
	if len(onlyFiles) > 0 {
		// Selective remote copy of listed files.
		var parts []string
		for _, f := range onlyFiles {
			f = strings.TrimSpace(strings.TrimPrefix(filepath.ToSlash(f), "./"))
			if f == "" {
				continue
			}
			parts = append(parts, fmt.Sprintf(
				`mkdir -p %s/$(dirname %s) && cp -a %s/%s %s/%s`,
				remoteShellQuote(src), remoteShellQuote(f),
				remoteShellQuote(c.Path), remoteShellQuote(f),
				remoteShellQuote(src), remoteShellQuote(f),
			))
		}
		if len(parts) == 0 {
			return "", fmt.Errorf("no files selected")
		}
		cmd := "set -e; " + strings.Join(parts, "; ") + `; echo "__MACLAW_ISO_MERGE_OK__"`
		out := h.sshExec(map[string]interface{}{
			"session_id":   sid,
			"command":      cmd,
			"wait_seconds": float64(120),
		})
		if !strings.Contains(out, "__MACLAW_ISO_MERGE_OK__") {
			return "", fmt.Errorf("remote partial adopt failed: %s", truncateRunesForSubAgent(out, 300))
		}
		remaining := subtractFiles(c.Files, onlyFiles)
		c.Files = remaining
		if len(remaining) == 0 {
			_, _ = h.removeStickyCodingConflictOpts(userID, c.ID, fmt.Sprintf("adopt-remote %s partial %d → cleared", c.ID, len(onlyFiles)))
			iso.cleanup(h)
			return fmt.Sprintf("已通过 SSH 部分采纳 %d 个文件；冲突已清空。", len(onlyFiles)), nil
		}
		h.storeStickyCodingConflictOpts(userID, c,
			fmt.Sprintf("adopt-remote %s partial %d, remain %d", c.ID, len(onlyFiles), len(remaining)),
			remaining)
		return fmt.Sprintf("已通过 SSH 部分采纳 %d 个文件，剩余 %d 个。", len(onlyFiles), len(remaining)), nil
	}
	sum, err := iso.mergeBack(h)
	if err != nil {
		return "", err
	}
	_, _ = h.removeStickyCodingConflictOpts(userID, c.ID, fmt.Sprintf("adopt-remote %s all", c.ID))
	iso.cleanup(h)
	return "远程冲突已采纳：\n" + sum, nil
}

// adoptBaseCodingConflictFiles writes merge-base content onto the main tree for
// selected files (true 3-way "take base"), then drops them from the conflict list.
// empty onlyFiles means all remaining files. Local worktrees write via filesystem;
// remote_isolate uses SSH merge-base + base64 write.
func (h *IMMessageHandler) adoptBaseCodingConflictFiles(userID, idOrPath string, onlyFiles []string) (string, error) {
	c, ok := h.findStickyCodingConflict(userID, idOrPath)
	if !ok {
		return "", fmt.Errorf("conflict not found: %s", idOrPath)
	}
	if c.Kind == "remote_isolate" {
		return h.adoptBaseRemoteCodingConflictFiles(userID, c, onlyFiles)
	}
	gitRoot := resolveConflictGitRoot(c, userID, h)
	if gitRoot == "" {
		return "", fmt.Errorf("main project path unknown")
	}
	files := c.Files
	if len(files) == 0 {
		files = listWorktreeChangedFiles(c.Path)
	}
	if len(onlyFiles) > 0 {
		files = filterFilesBySelection(files, onlyFiles)
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no files to restore from merge-base")
	}
	restored := 0
	var missing []string
	for _, rel := range files {
		relSlash := filepath.ToSlash(strings.TrimSpace(rel))
		if relSlash == "" {
			continue
		}
		base, ok := readConflictMergeBaseBlob(gitRoot, c.Path, c.Branch, relSlash)
		if !ok {
			missing = append(missing, relSlash)
			continue
		}
		dst := filepath.Join(gitRoot, filepath.FromSlash(relSlash))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(dst, []byte(base), 0o644); err != nil {
			return "", fmt.Errorf("write base %s: %w", relSlash, err)
		}
		restored++
	}
	if restored == 0 {
		return "", fmt.Errorf("no merge-base content available for selected files")
	}
	// Drop resolved files from conflict remaining list.
	remaining := subtractFiles(c.Files, files)
	if len(c.Files) == 0 {
		remaining = subtractFiles(listWorktreeChangedFiles(c.Path), files)
	}
	msg := fmt.Sprintf("已从 merge-base 写回 **%d** 个文件到主树", restored)
	if len(missing) > 0 {
		msg += fmt.Sprintf("（%d 个无 base）", len(missing))
	}
	if len(remaining) == 0 {
		dmsg, err := h.discardCodingWorkbenchConflictOpts(userID, c.ID, fmt.Sprintf("base %s %d files → cleared", c.ID, restored))
		if err != nil {
			// Still report base writes even if cleanup fails.
			h.appendStickyCodingConflictLog(userID, fmt.Sprintf("base %s %d files → cleared (cleanup err)", c.ID, restored))
			return msg + "；冲突清理失败: " + err.Error(), nil
		}
		return msg + "；" + dmsg, nil
	}
	c.Files = remaining
	h.storeStickyCodingConflictOpts(userID, c,
		fmt.Sprintf("base %s %d files, remain %d", c.ID, restored, len(remaining)),
		remaining)
	return msg + fmt.Sprintf("，剩余 %d 个冲突文件。", len(remaining)), nil
}

// adoptBaseRemoteCodingConflictFiles restores merge-base blobs onto the remote
// main tree via SSH (readRemoteMergeBaseBlob + writeRemoteConflictFile).
func (h *IMMessageHandler) adoptBaseRemoteCodingConflictFiles(userID string, c codingWorkbenchConflict, onlyFiles []string) (string, error) {
	mem := h.getStickyCodingWorkbenchMemory(userID)
	sid := strings.TrimSpace(mem.RemoteSessionID)
	if sid == "" {
		return "", fmt.Errorf("no active remote SSH session for adopt base")
	}
	mainDir := remoteConflictMainDir(h, userID, c)
	if mainDir == "" {
		return "", fmt.Errorf("remote main dir unknown")
	}
	allFiles := c.Files
	if len(allFiles) == 0 {
		allFiles = listRemoteIsolateChangedFiles(h, userID, c)
	}
	files := allFiles
	if len(onlyFiles) > 0 {
		files = filterFilesBySelection(allFiles, onlyFiles)
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no files to restore from merge-base")
	}
	restored := 0
	var missing []string
	for _, rel := range files {
		relSlash := filepath.ToSlash(strings.TrimSpace(rel))
		if relSlash == "" || strings.Contains(relSlash, "..") {
			continue
		}
		body, miss := readRemoteMergeBaseBlob(h, sid, mainDir, c.Path, c.Branch, relSlash, codingConflictWriteMaxBytes)
		if miss {
			missing = append(missing, relSlash)
			continue
		}
		abs := strings.TrimSuffix(mainDir, "/") + "/" + relSlash
		if err := writeRemoteConflictFile(h, sid, abs, body); err != nil {
			return "", fmt.Errorf("remote write base %s: %w", relSlash, err)
		}
		restored++
	}
	if restored == 0 {
		return "", fmt.Errorf("no remote merge-base content available for selected files")
	}
	remaining := subtractFiles(allFiles, files)
	msg := fmt.Sprintf("已从远程 merge-base 写回 **%d** 个文件到主树", restored)
	if len(missing) > 0 {
		msg += fmt.Sprintf("（%d 个无 base）", len(missing))
	}
	if len(remaining) == 0 {
		dmsg, err := h.discardCodingWorkbenchConflictOpts(userID, c.ID, fmt.Sprintf("base-remote %s %d files → cleared", c.ID, restored))
		if err != nil {
			h.appendStickyCodingConflictLog(userID, fmt.Sprintf("base-remote %s %d files → cleared (cleanup err)", c.ID, restored))
			return msg + "；冲突清理失败: " + err.Error(), nil
		}
		return msg + "；" + dmsg, nil
	}
	c.Files = remaining
	h.storeStickyCodingConflictOpts(userID, c,
		fmt.Sprintf("base-remote %s %d files, remain %d", c.ID, restored, len(remaining)),
		remaining)
	return msg + fmt.Sprintf("，剩余 %d 个冲突文件。", len(remaining)), nil
}

// keepMainCodingConflictFiles keeps main-tree versions for selected files
// (drops them from the conflict remaining list without adopting isolate content).
// empty onlyFiles means keep main for all remaining → full discard of conflict.
func (h *IMMessageHandler) keepMainCodingConflictFiles(userID, idOrPath string, onlyFiles []string) (string, error) {
	c, ok := h.findStickyCodingConflict(userID, idOrPath)
	if !ok {
		return "", fmt.Errorf("conflict not found: %s", idOrPath)
	}
	// Empty selection or explicit "all" → discard whole conflict (main wins).
	if len(onlyFiles) == 0 {
		return h.discardCodingWorkbenchConflictOpts(userID, c.ID, fmt.Sprintf("keep %s all (discard isolate)", c.ID))
	}
	files := c.Files
	if len(files) == 0 {
		if c.Kind == "remote_isolate" {
			files = listRemoteIsolateChangedFiles(h, userID, c)
		} else {
			files = listWorktreeChangedFiles(c.Path)
		}
	}
	kept := filterFilesBySelection(files, onlyFiles)
	if len(kept) == 0 {
		return "", fmt.Errorf("no matching files to keep on main")
	}
	remaining := subtractFiles(files, onlyFiles)
	if len(remaining) == 0 {
		// All resolved as main — discard isolation tree.
		msg, err := h.discardCodingWorkbenchConflictOpts(userID, c.ID, fmt.Sprintf("keep %s %d files → cleared", c.ID, len(kept)))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已在主树保留 %d 个文件（未采纳隔离侧）。%s", len(kept), msg), nil
	}
	c.Files = remaining
	h.storeStickyCodingConflictOpts(userID, c,
		fmt.Sprintf("keep %s %d files, remain %d", c.ID, len(kept), len(remaining)),
		remaining)
	return fmt.Sprintf("已在主树保留 %d 个文件，剩余 %d 个冲突文件。", len(kept), len(remaining)), nil
}

// writeCodingConflictFileContent writes arbitrary text onto the main tree for one
// conflict file, then drops it from the remaining conflict list (manual merge).
// Supports local worktrees and remote_isolate (via SSH base64 write).
func (h *IMMessageHandler) writeCodingConflictFileContent(userID, idOrPath, relPath, content string) (string, error) {
	c, ok := h.findStickyCodingConflict(userID, idOrPath)
	if !ok {
		return "", fmt.Errorf("conflict not found: %s", idOrPath)
	}
	relPath = filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(relPath, "./")))
	if relPath == "" || strings.Contains(relPath, "..") {
		return "", fmt.Errorf("invalid file path")
	}
	if len(content) > codingConflictWriteMaxBytes {
		return "", fmt.Errorf("content too large (max %d bytes)", codingConflictWriteMaxBytes)
	}
	if c.Kind == "remote_isolate" {
		mem := h.getStickyCodingWorkbenchMemory(userID)
		sid := strings.TrimSpace(mem.RemoteSessionID)
		if sid == "" {
			return "", fmt.Errorf("no active remote SSH session for write")
		}
		mainDir := remoteConflictMainDir(h, userID, c)
		if mainDir == "" {
			return "", fmt.Errorf("remote main project dir unknown")
		}
		abs := strings.TrimSuffix(mainDir, "/") + "/" + relPath
		if err := writeRemoteConflictFile(h, sid, abs, content); err != nil {
			return "", err
		}
		return h.finishConflictFileResolved(userID, c, relPath, "write-remote")
	}
	gitRoot := resolveConflictGitRoot(c, userID, h)
	if gitRoot == "" {
		gitRoot = strings.TrimSpace(c.MainProject)
	}
	if gitRoot == "" {
		return "", fmt.Errorf("main project path unknown")
	}
	dst := filepath.Clean(filepath.Join(gitRoot, filepath.FromSlash(relPath)))
	if !isPathInsideRoot(gitRoot, dst) {
		return "", fmt.Errorf("path escapes main root")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", relPath, err)
	}
	return h.finishConflictFileResolved(userID, c, relPath, "write")
}

// finishConflictFileResolved removes one path from the conflict remaining list.
func (h *IMMessageHandler) finishConflictFileResolved(userID string, c codingWorkbenchConflict, relPath, action string) (string, error) {
	files := c.Files
	if len(files) == 0 {
		if c.Kind == "remote_isolate" {
			files = listRemoteIsolateChangedFiles(h, userID, c)
		} else {
			files = listWorktreeChangedFiles(c.Path)
		}
	}
	remaining := subtractFiles(files, []string{relPath})
	if len(remaining) == 0 {
		msg, err := h.discardCodingWorkbenchConflictOpts(userID, c.ID, fmt.Sprintf("%s %s %s → cleared", action, c.ID, relPath))
		if err != nil {
			h.appendStickyCodingConflictLog(userID, fmt.Sprintf("%s %s %s (cleared, cleanup err)", action, c.ID, relPath))
			return fmt.Sprintf("已写入 `%s`；冲突清理失败: %v", relPath, err), nil
		}
		return fmt.Sprintf("已写入 `%s` 到主树；%s", relPath, msg), nil
	}
	c.Files = remaining
	h.storeStickyCodingConflictOpts(userID, c,
		fmt.Sprintf("%s %s %s, remain %d", action, c.ID, relPath, len(remaining)),
		remaining)
	return fmt.Sprintf("已写入 `%s` 到主树，剩余 %d 个冲突文件。", relPath, len(remaining)), nil
}

// clearStickyCodingConflictLog drops the audit trail.
func (h *IMMessageHandler) clearStickyCodingConflictLog(userID string) {
	if h == nil || userID == "" {
		return
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.ConflictLog = nil
	})
}

// exportStickyCodingConflictLog copies the conflict audit trail into WorktreeNotes
// (visible on /worktree status) and returns a markdown export body.
func (h *IMMessageHandler) exportStickyCodingConflictLog(userID string) (string, int) {
	if h == nil || userID == "" {
		return "", 0
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if len(mem.ConflictLog) == 0 {
		return "", 0
	}
	var b strings.Builder
	b.WriteString("## Conflict resolve log export\n\n")
	for i := len(mem.ConflictLog) - 1; i >= 0; i-- {
		b.WriteString("- ")
		b.WriteString(mem.ConflictLog[i])
		b.WriteString("\n")
	}
	// Fold a compact summary into worktree notes for /worktree status.
	summary := fmt.Sprintf("conflict-log export (%d entries) @ %s",
		len(mem.ConflictLog), time.Now().Format("15:04:05"))
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.WorktreeNotes = append(m.WorktreeNotes, summary)
		if len(m.WorktreeNotes) > 12 {
			m.WorktreeNotes = m.WorktreeNotes[len(m.WorktreeNotes)-12:]
		}
	})
	return b.String(), len(mem.ConflictLog)
}

const stickyCodingConflictLogMax = 20

// appendStickyCodingConflictLog records a short audit line for conflict actions.
func (h *IMMessageHandler) appendStickyCodingConflictLog(userID, line string) {
	if h == nil || strings.TrimSpace(userID) == "" {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		stickyCodingConflictLogAppend(mem, line)
	})
}

// setStickyCodingConflictUIState remembers conflict panel focus for restore.
func (h *IMMessageHandler) setStickyCodingConflictUIState(userID, activeID, focusFile string, selected []string) {
	if h == nil || strings.TrimSpace(userID) == "" {
		return
	}
	activeID = strings.TrimSpace(activeID)
	focusFile = filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(focusFile, "./")))
	cleanSel := make([]string, 0, len(selected))
	seen := map[string]struct{}{}
	for _, s := range selected {
		s = filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(s, "./")))
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		cleanSel = append(cleanSel, s)
		if len(cleanSel) >= 40 {
			break
		}
	}
	// Debounce disk flush: UI multi-select / focus fires often; memory stays fresh.
	h.updateStickyCodingWorkbenchMemoryOpts(userID, false, func(mem *stickyCodingWorkbenchMemory) {
		mem.ConflictActiveID = activeID
		mem.ConflictFocusFile = focusFile
		if len(cleanSel) == 0 {
			mem.ConflictSelected = nil
		} else {
			mem.ConflictSelected = cleanSel
		}
	})
}

// getCodingConflictFileTriple loads main/theirs/base peeks for side-by-side UI.
func (h *IMMessageHandler) getCodingConflictFileTriple(userID, idOrPath, relPath string, maxBytes int) (codingConflictFileTriple, error) {
	relPath = filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(relPath, "./")))
	if relPath == "" {
		return codingConflictFileTriple{}, fmt.Errorf("invalid path")
	}
	if maxBytes <= 0 {
		maxBytes = codingConflictPreviewMaxBytes
	}
	// Smaller per-column budget for triple view.
	colMax := maxBytes / 2
	if colMax < 4096 {
		colMax = 4096
	}
	// Local: parallel peeks (cheap file/git reads). Remote: sequential to avoid
	// stacking three concurrent SSH execs on one session.
	c, ok := h.findStickyCodingConflict(userID, idOrPath)
	if ok && c.Kind == "remote_isolate" {
		main, err := h.getCodingConflictFilePreview(userID, idOrPath, relPath, "main", colMax)
		if err != nil {
			return codingConflictFileTriple{}, err
		}
		theirs, _ := h.getCodingConflictFilePreview(userID, idOrPath, relPath, "theirs", colMax)
		base, _ := h.getCodingConflictFilePreview(userID, idOrPath, relPath, "base", colMax)
		return codingConflictFileTriple{Path: relPath, Main: main, Theirs: theirs, Base: base}, nil
	}
	type sideRes struct {
		side string
		prev codingConflictFilePreview
		err  error
	}
	ch := make(chan sideRes, 3)
	for _, side := range []string{"main", "theirs", "base"} {
		side := side
		go func() {
			prev, err := h.getCodingConflictFilePreview(userID, idOrPath, relPath, side, colMax)
			ch <- sideRes{side: side, prev: prev, err: err}
		}()
	}
	var main, theirs, base codingConflictFilePreview
	var mainErr error
	for i := 0; i < 3; i++ {
		r := <-ch
		switch r.side {
		case "main":
			main, mainErr = r.prev, r.err
		case "theirs":
			theirs = r.prev
		case "base":
			base = r.prev
		}
	}
	if mainErr != nil {
		return codingConflictFileTriple{}, mainErr
	}
	return codingConflictFileTriple{Path: relPath, Main: main, Theirs: theirs, Base: base}, nil
}

// remoteConflictMainDir resolves remote main project dir for a conflict.
func remoteConflictMainDir(h *IMMessageHandler, userID string, c codingWorkbenchConflict) string {
	src := strings.TrimSpace(c.MainProject)
	if src != "" {
		return src
	}
	if h == nil {
		return ""
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if d := strings.TrimSpace(mem.RemoteProjectDir); d != "" {
		return d
	}
	return strings.TrimSpace(mem.RemoteWorkDir)
}

// extractRemoteBase64Payload joins base64 lines from SSH output. Hosts without
// `base64 -w0` wrap at 76 cols; taking only the last line would corrupt content.
// Skips blank lines, shell noise with spaces, and __MACLAW_* markers.
func extractRemoteBase64Payload(out string) string {
	var b strings.Builder
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.Contains(line, "__MACLAW") {
			continue
		}
		// Log/noise lines usually contain spaces; pure base64 does not.
		if strings.ContainsAny(line, " \t") {
			continue
		}
		if !isRemoteBase64Alphabet(line) {
			continue
		}
		b.WriteString(line)
	}
	return b.String()
}

func isRemoteBase64Alphabet(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '+', c == '/', c == '=', c == '-', c == '_':
			// std and url-safe
		default:
			return false
		}
	}
	return true
}

func decodeRemoteBase64Payload(out string) ([]byte, error) {
	payload := extractRemoteBase64Payload(out)
	if payload == "" {
		return nil, fmt.Errorf("empty base64 payload")
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err == nil {
		return raw, nil
	}
	// URL-safe variant used by some tools.
	if raw, err2 := base64.URLEncoding.DecodeString(payload); err2 == nil {
		return raw, nil
	}
	// RawStd without padding.
	if raw, err2 := base64.RawStdEncoding.DecodeString(strings.TrimRight(payload, "=")); err2 == nil {
		return raw, nil
	}
	return nil, err
}

func readRemoteConflictFile(h *IMMessageHandler, sid, absPath string, maxBytes int) (body string, missing bool) {
	if h == nil || sid == "" || absPath == "" {
		return "", true
	}
	// Portable base64; trim markers.
	cmd := fmt.Sprintf(
		`if [ -f %s ]; then (base64 -w0 %s 2>/dev/null || base64 %s 2>/dev/null); else echo __MACLAW_MISS__; fi`,
		remoteShellQuote(absPath), remoteShellQuote(absPath), remoteShellQuote(absPath),
	)
	out := strings.TrimSpace(h.sshExec(map[string]interface{}{
		"session_id":   sid,
		"command":      cmd,
		"wait_seconds": float64(20),
	}))
	if out == "" || strings.Contains(out, "__MACLAW_MISS__") {
		return "", true
	}
	raw, err := decodeRemoteBase64Payload(out)
	if err != nil {
		return "", true
	}
	// Cap at maxBytes when set (callers pass the intended budget, not half).
	if maxBytes > 0 && len(raw) > maxBytes {
		raw = raw[:maxBytes]
	}
	return string(raw), false
}

func writeRemoteConflictFile(h *IMMessageHandler, sid, absPath, content string) error {
	if h == nil || sid == "" || absPath == "" {
		return fmt.Errorf("remote session/path required")
	}
	// Reuse remote coding write helpers: python one-shot for small bodies,
	// chunked base64 for large ones (avoids PTY/ARG_MAX overflow from printf|base64).
	const largeThreshold = 32 * 1024
	ssh := func(cmd string, wait float64) string {
		return h.sshExec(map[string]interface{}{
			"session_id":   sid,
			"command":      cmd,
			"wait_seconds": wait,
		})
	}
	var out string
	if len(content) > largeThreshold {
		b64 := base64.StdEncoding.EncodeToString([]byte(content))
		tmpPath := fmt.Sprintf("/tmp/maclaw_cwrite_%d", time.Now().UnixNano())
		cleanup := func() { _ = ssh(fmt.Sprintf("rm -f %s", remoteShellQuote(tmpPath)), 10) }
		const chunkSize = 48 * 1024
		for i := 0; i < len(b64); i += chunkSize {
			end := i + chunkSize
			if end > len(b64) {
				end = len(b64)
			}
			chunkOut := ssh(remoteWriteFileLargeChunkCommand(tmpPath, b64[i:end], i > 0), 20)
			if remoteCodingToolResultLooksFailed(chunkOut) {
				cleanup()
				return fmt.Errorf("remote write chunk failed: %s", truncateRunesForSubAgent(chunkOut, 200))
			}
		}
		out = ssh(remoteWriteFileLargeDecodeCommand(absPath, tmpPath), 30)
		// Decode command already rm -f tmp on success; best-effort if decode failed.
		if !remoteWriteFileResultHasOK(out) {
			cleanup()
		}
	} else {
		out = ssh(remoteWriteFilePythonCommand(absPath, content), 30)
	}
	if !remoteWriteFileResultHasOK(out) {
		return fmt.Errorf("remote write failed: %s", truncateRunesForSubAgent(out, 200))
	}
	return nil
}

// buildRemoteMergeBaseCmd builds the SSH shell command that resolves merge-base
// content for rel between mainDir HEAD and the isolate tip (branch or isoDir HEAD).
// Exported shape is tested; markers: __MACLAW_MISS__ on failure, base64 payload on success.
func buildRemoteMergeBaseCmd(mainDir, isoDir, branch, rel string) string {
	// Prefer explicit branch tip when provided; fall back to isolate HEAD.
	var tipExpr string
	if b := strings.TrimSpace(branch); b != "" {
		tipExpr = fmt.Sprintf(`TIP=$(git -C %s rev-parse --verify %s 2>/dev/null); if [ -z "$TIP" ]; then TIP=$(git -C %s rev-parse HEAD 2>/dev/null); fi`,
			remoteShellQuote(mainDir), remoteShellQuote(b), remoteShellQuote(isoDir))
	} else {
		tipExpr = fmt.Sprintf(`TIP=$(git -C %s rev-parse HEAD 2>/dev/null)`, remoteShellQuote(isoDir))
	}
	// Pipe git show into base64 (avoids null-byte / binary breakage in shell vars).
	// cat-file -e distinguishes missing blob from empty content.
	return fmt.Sprintf(
		`set +e; MAIN=%s; REL=%s; %s; `+
			`cd "$MAIN" 2>/dev/null || { echo __MACLAW_MISS__; exit 0; }; `+
			`[ -z "$TIP" ] && { echo __MACLAW_MISS__; exit 0; }; `+
			`BASE=$(git merge-base HEAD "$TIP" 2>/dev/null); `+
			`[ -z "$BASE" ] && { echo __MACLAW_MISS__; exit 0; }; `+
			`git cat-file -e "$BASE:$REL" 2>/dev/null || { echo __MACLAW_MISS__; exit 0; }; `+
			`git show "$BASE:$REL" 2>/dev/null | (base64 -w0 2>/dev/null || base64)`,
		remoteShellQuote(mainDir), remoteShellQuote(rel), tipExpr,
	)
}

// readRemoteMergeBaseBlob loads file content at merge-base(HEAD, isolate tip) on the remote host.
// branch is optional; when empty, uses isolate worktree HEAD.
func readRemoteMergeBaseBlob(h *IMMessageHandler, sid, mainDir, isoDir, branch, rel string, maxBytes int) (body string, missing bool) {
	if h == nil || sid == "" || mainDir == "" || isoDir == "" || rel == "" {
		return "", true
	}
	cmd := buildRemoteMergeBaseCmd(mainDir, isoDir, branch, rel)
	out := strings.TrimSpace(h.sshExec(map[string]interface{}{
		"session_id":   sid,
		"command":      cmd,
		"wait_seconds": float64(25),
	}))
	if out == "" || strings.Contains(out, "__MACLAW_MISS__") {
		return "", true
	}
	raw, err := decodeRemoteBase64Payload(out)
	if err != nil {
		return "", true
	}
	if maxBytes > 0 && len(raw) > maxBytes {
		raw = raw[:maxBytes]
	}
	return string(raw), false
}

// getCodingConflictFilePreview returns a longer content peek for one side of a conflict file.
func (h *IMMessageHandler) getCodingConflictFilePreview(userID, idOrPath, relPath, side string, maxBytes int) (codingConflictFilePreview, error) {
	c, ok := h.findStickyCodingConflict(userID, idOrPath)
	if !ok {
		return codingConflictFilePreview{}, fmt.Errorf("conflict not found: %s", idOrPath)
	}
	relPath = filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(relPath, "./")))
	if relPath == "" || strings.Contains(relPath, "..") {
		return codingConflictFilePreview{}, fmt.Errorf("invalid path")
	}
	if maxBytes <= 0 {
		maxBytes = codingConflictPreviewMaxBytes
	}
	side = strings.ToLower(strings.TrimSpace(side))
	prev := codingConflictFilePreview{Path: relPath, Side: side}
	if c.Kind == "remote_isolate" {
		mem := h.getStickyCodingWorkbenchMemory(userID)
		sid := strings.TrimSpace(mem.RemoteSessionID)
		if sid == "" {
			prev.Missing = true
			prev.Content = "(remote isolate — reconnect SSH to preview)"
			return prev, nil
		}
		var abs string
		switch side {
		case "theirs", "isolate", "worktree", "wt":
			abs = strings.TrimSuffix(c.Path, "/") + "/" + relPath
			body, missing := readRemoteConflictFile(h, sid, abs, maxBytes)
			if missing {
				prev.Missing = true
				return prev, nil
			}
			return finalizeConflictPreview(prev, body, maxBytes), nil
		case "base":
			mainDir := remoteConflictMainDir(h, userID, c)
			if mainDir == "" {
				prev.Missing = true
				prev.Content = "(remote main dir unknown)"
				return prev, nil
			}
			body, missing := readRemoteMergeBaseBlob(h, sid, mainDir, c.Path, c.Branch, relPath, maxBytes)
			if missing {
				prev.Missing = true
				prev.Content = "(remote base not available)"
				return prev, nil
			}
			return finalizeConflictPreview(prev, body, maxBytes), nil
		default:
			mainDir := remoteConflictMainDir(h, userID, c)
			if mainDir == "" {
				prev.Missing = true
				prev.Content = "(remote main dir unknown)"
				return prev, nil
			}
			abs = strings.TrimSuffix(mainDir, "/") + "/" + relPath
			body, missing := readRemoteConflictFile(h, sid, abs, maxBytes)
			if missing {
				prev.Missing = true
				return prev, nil
			}
			return finalizeConflictPreview(prev, body, maxBytes), nil
		}
	}
	var body string
	switch side {
	case "theirs", "isolate", "worktree", "wt":
		abs := filepath.Clean(filepath.Join(c.Path, filepath.FromSlash(relPath)))
		if !isPathInsideRoot(c.Path, abs) {
			return prev, fmt.Errorf("path escapes isolate root")
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			prev.Missing = true
			return prev, nil
		}
		body = string(data)
	case "base":
		gitRoot := resolveConflictGitRoot(c, userID, h)
		base, ok := readConflictMergeBaseBlob(gitRoot, c.Path, c.Branch, relPath)
		if !ok {
			prev.Missing = true
			return prev, nil
		}
		body = base
	default: // main
		gitRoot := resolveConflictGitRoot(c, userID, h)
		if gitRoot == "" {
			gitRoot = strings.TrimSpace(c.MainProject)
		}
		if gitRoot == "" {
			return prev, fmt.Errorf("main root unknown")
		}
		abs := filepath.Clean(filepath.Join(gitRoot, filepath.FromSlash(relPath)))
		if !isPathInsideRoot(gitRoot, abs) {
			return prev, fmt.Errorf("path escapes main root")
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			prev.Missing = true
			return prev, nil
		}
		body = string(data)
	}
	return finalizeConflictPreview(prev, body, maxBytes), nil
}

func finalizeConflictPreview(prev codingConflictFilePreview, body string, maxBytes int) codingConflictFilePreview {
	// Avoid dumping binary garbage into the UI preview pane.
	if IsBinaryFile([]byte(body)) {
		prev.Bytes = len(body)
		prev.Content = "(binary file — use Open main / Open theirs)"
		prev.Truncated = true
		return prev
	}
	prev.Bytes = len(body)
	if len(body) > maxBytes {
		prev.Content = truncateRunesForSubAgent(body, maxBytes/2)
		if len(prev.Content) < len(body) {
			prev.Truncated = true
		}
	} else {
		prev.Content = body
	}
	return prev
}

// pruneStickyConflictUISelection drops selected/focus paths no longer in remaining.
func (h *IMMessageHandler) pruneStickyConflictUISelection(userID string, remaining []string) {
	if h == nil || userID == "" {
		return
	}
	h.updateStickyCodingWorkbenchMemoryOpts(userID, false, func(mem *stickyCodingWorkbenchMemory) {
		stickyCodingConflictUIPrune(mem, remaining)
	})
}

// discardCodingWorkbenchConflict drops a kept isolation tree.
func (h *IMMessageHandler) discardCodingWorkbenchConflict(userID, idOrPath string) (string, error) {
	return h.discardCodingWorkbenchConflictOpts(userID, idOrPath, "")
}

// discardCodingWorkbenchConflictOpts is discard with an optional audit log line.
// Empty logLine uses the default "discard [remote] <id>" text.
func (h *IMMessageHandler) discardCodingWorkbenchConflictOpts(userID, idOrPath, logLine string) (string, error) {
	// Peek kind for default log (cheap RAM list; no extra disk write).
	if strings.TrimSpace(logLine) == "" {
		if c0, ok0 := h.findStickyCodingConflict(userID, idOrPath); ok0 {
			if c0.Kind == "remote_isolate" {
				logLine = fmt.Sprintf("discard remote %s", c0.ID)
			} else {
				logLine = fmt.Sprintf("discard %s", c0.ID)
			}
		}
	}
	// Remove sticky record + audit log in one RMW; filesystem/SSH cleanup after.
	c, ok := h.removeStickyCodingConflictOpts(userID, idOrPath, logLine)
	if !ok {
		return "", fmt.Errorf("conflict not found: %s", idOrPath)
	}
	if c.Kind == "remote_isolate" {
		// Best-effort remote cleanup if session still sticky.
		mem := h.getStickyCodingWorkbenchMemory(userID)
		if sid := strings.TrimSpace(mem.RemoteSessionID); sid != "" && h != nil {
			cmd := fmt.Sprintf(`rm -rf -- %s; echo ok`, remoteShellQuote(c.Path))
			_ = h.sshExec(map[string]interface{}{
				"session_id":   sid,
				"command":      cmd,
				"wait_seconds": float64(30),
			})
		}
		return fmt.Sprintf("已丢弃远程隔离目录记录: %s", c.Path), nil
	}
	gitRoot := c.GitRoot
	if gitRoot == "" && c.MainProject != "" {
		if root, ok := remote.DetectGitWorkspaceRoot(c.MainProject); ok {
			gitRoot = root
		}
	}
	if gitRoot != "" {
		_ = remote.RunGit(gitRoot, "worktree", "remove", "--force", c.Path)
		if c.Branch != "" {
			_ = remote.RunGit(gitRoot, "branch", "-D", c.Branch)
		}
		_ = remote.RunGit(gitRoot, "worktree", "prune")
	}
	_ = os.RemoveAll(c.Path)
	return fmt.Sprintf("已丢弃冲突 worktree: %s", c.Path), nil
}

// discardAllStickyCodingConflicts discards every kept isolation conflict for the user.
// Returns a summary; empty list is success with a friendly message.
func (h *IMMessageHandler) discardAllStickyCodingConflicts(userID string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("handler unavailable")
	}
	conflicts := h.listStickyCodingConflicts(userID)
	if len(conflicts) == 0 {
		// Still clear any stale UI memory.
		h.setStickyCodingConflictUIState(userID, "", "", nil)
		return "当前没有待处理的隔离冲突。", nil
	}
	var okN, failN int
	var fails []string
	for _, c := range conflicts {
		id := strings.TrimSpace(c.ID)
		if id == "" {
			id = c.Path
		}
		if _, err := h.discardCodingWorkbenchConflict(userID, id); err != nil {
			failN++
			if len(fails) < 5 {
				fails = append(fails, fmt.Sprintf("%s: %v", id, err))
			}
			continue
		}
		okN++
	}
	// Guarantee UI memory is clear even if some discards failed.
	if failN == 0 || len(h.listStickyCodingConflicts(userID)) == 0 {
		h.setStickyCodingConflictUIState(userID, "", "", nil)
	}
	msg := fmt.Sprintf("已丢弃 %d 个隔离冲突", okN)
	if failN > 0 {
		msg += fmt.Sprintf("，%d 个失败", failN)
		if len(fails) > 0 {
			msg += "：" + strings.Join(fails, "; ")
		}
	}
	msg += "。"
	return msg, nil
}

// conflictArtifactAlive reports whether the isolation tree for c still exists.
// When probeRemote is false, remote_isolate entries are assumed alive (no SSH).
// When the remote session is missing/dead, remote entries are also assumed alive
// so we never drop a conflict we cannot verify.
func (h *IMMessageHandler) conflictArtifactAlive(userID string, c codingWorkbenchConflict, probeRemote bool) bool {
	path := strings.TrimSpace(c.Path)
	if path == "" {
		return false
	}
	if c.Kind == "remote_isolate" {
		if !probeRemote || h == nil {
			return true
		}
		mem := h.getStickyCodingWorkbenchMemory(userID)
		sid := strings.TrimSpace(mem.RemoteSessionID)
		if sid == "" || !h.sshSessionAlive(sid) {
			return true
		}
		cmd := fmt.Sprintf(
			`if [ -e %s ] || [ -d %s ]; then echo __MACLAW_ISO_ALIVE__; else echo __MACLAW_ISO_DEAD__; fi`,
			remoteShellQuote(path), remoteShellQuote(path),
		)
		out := h.sshExec(map[string]interface{}{
			"session_id":   sid,
			"command":      cmd,
			"wait_seconds": float64(15),
		})
		if strings.Contains(out, "__MACLAW_ISO_DEAD__") {
			return false
		}
		if strings.Contains(out, "__MACLAW_ISO_ALIVE__") {
			return true
		}
		// SSH error / ambiguous — keep the record.
		return true
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	return true
}

// stickyConflictPruneAt throttles local Stat prune on hot status polls.
var stickyConflictPruneAt sync.Map // userID -> time.Time

const stickyConflictPruneStatusInterval = 3 * time.Second

// pruneDeadStickyCodingConflicts drops conflict records whose isolation trees
// no longer exist. Local paths use Stat; remote_isolate is probed over SSH only
// when probeRemote is true and the sticky session is alive.
// Returns the number of records removed (artifact cleanup is skipped — gone).
func (h *IMMessageHandler) pruneDeadStickyCodingConflicts(userID string, probeRemote bool) int {
	return h.pruneDeadStickyCodingConflictsOpts(userID, probeRemote, 0)
}

// pruneDeadStickyCodingConflictsThrottled skips work when the same user was
// pruned within minInterval (status polls). Ensure/List should pass 0.
func (h *IMMessageHandler) pruneDeadStickyCodingConflictsThrottled(userID string, probeRemote bool, minInterval time.Duration) int {
	return h.pruneDeadStickyCodingConflictsOpts(userID, probeRemote, minInterval)
}

func (h *IMMessageHandler) pruneDeadStickyCodingConflictsOpts(userID string, probeRemote bool, minInterval time.Duration) int {
	if h == nil {
		return 0
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return 0
	}
	if minInterval > 0 && !probeRemote {
		if v, ok := stickyConflictPruneAt.Load(userID); ok {
			if t, ok := v.(time.Time); ok && time.Since(t) < minInterval {
				return 0
			}
		}
	}
	conflicts := h.listStickyCodingConflicts(userID)
	if len(conflicts) == 0 {
		stickyConflictPruneAt.Store(userID, time.Now())
		return 0
	}
	// Probe first (may Stat/SSH); then drop all dead in one sticky RMW.
	var dead []codingWorkbenchConflict
	for _, c := range conflicts {
		if h.conflictArtifactAlive(userID, c, probeRemote) {
			continue
		}
		dead = append(dead, c)
	}
	if len(dead) == 0 {
		stickyConflictPruneAt.Store(userID, time.Now())
		return 0
	}
	deadIDs := map[string]struct{}{}
	deadPaths := map[string]struct{}{}
	for _, c := range dead {
		if id := strings.TrimSpace(c.ID); id != "" {
			deadIDs[id] = struct{}{}
		}
		if p := filepath.Clean(strings.TrimSpace(c.Path)); p != "" {
			deadPaths[p] = struct{}{}
		}
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		out := make([]codingWorkbenchConflict, 0, len(mem.WorktreeConflicts))
		for _, x := range mem.WorktreeConflicts {
			if _, ok := deadIDs[x.ID]; ok {
				continue
			}
			if _, ok := deadPaths[filepath.Clean(x.Path)]; ok {
				continue
			}
			out = append(out, x)
		}
		mem.WorktreeConflicts = out
		// Clear panel focus if the active conflict was pruned.
		if mem.ConflictActiveID != "" {
			if _, ok := deadIDs[mem.ConflictActiveID]; ok {
				mem.ConflictActiveID = ""
				mem.ConflictSelected = nil
				mem.ConflictFocusFile = ""
			} else if _, ok := deadPaths[filepath.Clean(mem.ConflictActiveID)]; ok {
				mem.ConflictActiveID = ""
				mem.ConflictSelected = nil
				mem.ConflictFocusFile = ""
			}
		}
	})
	// Best-effort git bookkeeping for vanished local worktrees (outside RMW).
	for _, c := range dead {
		if c.Kind == "remote_isolate" {
			continue
		}
		gitRoot := c.GitRoot
		if gitRoot == "" && c.MainProject != "" {
			if root, ok := remote.DetectGitWorkspaceRoot(c.MainProject); ok {
				gitRoot = root
			}
		}
		if gitRoot != "" {
			_ = remote.RunGit(gitRoot, "worktree", "prune")
		}
	}
	stickyConflictPruneAt.Store(userID, time.Now())
	return len(dead)
}

func formatCodingConflictsMarkdown(conflicts []codingWorkbenchConflict) string {
	if len(conflicts) == 0 {
		return "当前没有待处理的 worktree/隔离 冲突。"
	}
	var b strings.Builder
	b.WriteString("## 待处理冲突\n\n")
	b.WriteString("预览 `/worktree diff <id>`；采纳 `/worktree adopt`；主树 `/worktree keep`；base `/worktree base`；丢弃 `/worktree discard <id|all>`。\n\n")
	for _, c := range conflicts {
		b.WriteString(fmt.Sprintf("### `%s` · T%d · %s\n", c.ID, c.StepIndex, c.Kind))
		b.WriteString(fmt.Sprintf("- **path**: `%s`\n", c.Path))
		if c.Branch != "" {
			b.WriteString(fmt.Sprintf("- **branch**: `%s`\n", c.Branch))
		}
		if c.Error != "" {
			b.WriteString(fmt.Sprintf("- **error**: %s\n", c.Error))
		}
		if len(c.Files) > 0 {
			b.WriteString("- **files**: ")
			shown := c.Files
			if len(shown) > 12 {
				shown = shown[:12]
			}
			b.WriteString(strings.Join(shown, ", "))
			if len(c.Files) > 12 {
				b.WriteString(fmt.Sprintf(" …(+%d)", len(c.Files)-12))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// encode/decode helper for tests / debug
func codingConflictsJSON(conflicts []codingWorkbenchConflict) string {
	raw, err := json.Marshal(conflicts)
	if err != nil {
		return "[]"
	}
	return string(raw)
}
