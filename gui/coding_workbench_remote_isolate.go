package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"path"
	"strings"
	"time"
)

// remoteCodingIsolate is a temporary remote project copy for write isolation
// (SSH analogue of local git worktrees when the remote host is not using them).
type remoteCodingIsolate struct {
	SessionID  string
	SourceDir  string
	IsolateDir string
	StepIndex  int
	// DeclaredWrites is optional for legacy sequential workbench execution.
	// When present, automatic merge verifies the isolate's actual changed files
	// against this frozen scope before changing the primary checkout.
	DeclaredWrites []string
	created        bool
}

// createRemoteCodingIsolate creates an isolated remote workspace.
// Prefers `git worktree` when the remote project is a git repo with commits;
// full directory copy is used only when allowFullCopy is true (always mode).
func createRemoteCodingIsolate(h *IMMessageHandler, sessionID, projectDir string, stepIndex int, allowFullCopy bool, declaredWrites ...[]string) (*remoteCodingIsolate, error) {
	if h == nil {
		return nil, fmt.Errorf("nil handler")
	}
	sessionID = strings.TrimSpace(sessionID)
	projectDir = strings.TrimSpace(projectDir)
	if sessionID == "" || projectDir == "" {
		return nil, fmt.Errorf("missing session or project")
	}
	id := fmt.Sprintf("%d-%d", stepIndex, time.Now().UnixNano()%1e9)
	writes := remoteIsolateDeclaredWrites(declaredWrites...)

	// Try remote git worktree first (cheap, preferred).
	if iso, err := tryCreateRemoteGitWorktree(h, sessionID, projectDir, stepIndex, id); err == nil && iso != nil {
		iso.DeclaredWrites = writes
		return iso, nil
	} else if err != nil {
		log.Printf("[remote-isolate] git worktree unavailable step=%d: %v", stepIndex, err)
	}
	if !allowFullCopy {
		return nil, fmt.Errorf("remote git worktree unavailable and full copy disabled (auto mode)")
	}

	isolateDir := fmt.Sprintf("/tmp/maclaw-coding-%s", id)
	// Prefer cp -a; fall back to rsync if present.
	cmd := fmt.Sprintf(
		`set -e; SRC=%s; DST=%s; mkdir -p "$DST"; `+
			`if command -v rsync >/dev/null 2>&1; then rsync -a --exclude .git/objects/pack "$SRC"/ "$DST"/ 2>/dev/null || cp -a "$SRC"/. "$DST"/; `+
			`else cp -a "$SRC"/. "$DST"/; fi; `+
			`echo "__MACLAW_ISO_OK__"`,
		remoteShellQuote(projectDir),
		remoteShellQuote(isolateDir),
	)
	out := h.sshExec(map[string]interface{}{
		"session_id":   sessionID,
		"command":      cmd,
		"wait_seconds": float64(120),
	})
	if !strings.Contains(out, "__MACLAW_ISO_OK__") {
		return nil, fmt.Errorf("remote isolate create failed: %s", truncateRunesForSubAgent(out, 400))
	}
	log.Printf("[remote-isolate] created step=%d dir=%s session=%s", stepIndex, isolateDir, sessionID)
	return &remoteCodingIsolate{
		SessionID:      sessionID,
		SourceDir:      projectDir,
		IsolateDir:     isolateDir,
		StepIndex:      stepIndex,
		DeclaredWrites: writes,
		created:        true,
	}, nil
}

// tryCreateRemoteGitWorktree runs git worktree add on the remote host.
func tryCreateRemoteGitWorktree(h *IMMessageHandler, sessionID, projectDir string, stepIndex int, id string) (*remoteCodingIsolate, error) {
	branch := fmt.Sprintf("maclaw/coding-%s", id)
	isolateDir := fmt.Sprintf("/tmp/maclaw-wt-%s", id)
	cmd := fmt.Sprintf(
		`set -e; cd %s; `+
			`git rev-parse --is-inside-work-tree >/dev/null 2>&1; `+
			`git rev-parse --verify HEAD >/dev/null 2>&1; `+
			`ROOT=$(git rev-parse --show-toplevel); `+
			`git branch -D %s 2>/dev/null || true; `+
			`rm -rf -- %s; `+
			`git worktree add -b %s %s HEAD; `+
			`echo "__MACLAW_WT_OK__:$ROOT"`,
		remoteShellQuote(projectDir),
		remoteShellQuote(branch),
		remoteShellQuote(isolateDir),
		remoteShellQuote(branch),
		remoteShellQuote(isolateDir),
	)
	out := h.sshExec(map[string]interface{}{
		"session_id":   sessionID,
		"command":      cmd,
		"wait_seconds": float64(90),
	})
	if !strings.Contains(out, "__MACLAW_WT_OK__") {
		return nil, fmt.Errorf("remote git worktree failed: %s", truncateRunesForSubAgent(out, 300))
	}
	log.Printf("[remote-isolate] git worktree created step=%d branch=%s dir=%s", stepIndex, branch, isolateDir)
	return &remoteCodingIsolate{
		SessionID:  sessionID,
		SourceDir:  projectDir,
		IsolateDir: isolateDir,
		StepIndex:  stepIndex,
		created:    true,
		// tag as git worktree via path prefix maclaw-wt-
	}, nil
}

// mergeBack permits one controlled Git cherry-pick only. It intentionally has
// no rsync/cp fallback: copying into a primary directory can overwrite an
// unexpected change and cannot prove write-set conformance. Full-copy isolates
// remain review artifacts that must be adopted manually per file.
func (r *remoteCodingIsolate) mergeBack(h *IMMessageHandler, declaredWrites ...[]string) (string, error) {
	if r == nil || !r.created || h == nil {
		return "", nil
	}
	if !strings.Contains(r.IsolateDir, "maclaw-wt-") {
		return "", fmt.Errorf("remote full-copy isolate is not eligible for automatic merge; inspect and manually adopt the isolated files")
	}
	writes := remoteIsolateDeclaredWrites(declaredWrites...)
	if len(writes) == 0 {
		writes = append([]string(nil), r.DeclaredWrites...)
	}
	if err := validateRemoteIsolateWriteClaims(writes); err != nil {
		return "", err
	}
	start, end, err := remoteIsolateMergeMarkers()
	if err != nil {
		return "", err
	}
	out := h.sshExec(map[string]interface{}{
		"session_id": r.SessionID, "command": remoteGitWorktreeMergeCommand(r.IsolateDir, r.SourceDir, r.StepIndex, writes, start, end), "wait_seconds": float64(180),
	})
	if !remoteIsolateMergeFrameComplete(out, start, end) {
		return "", fmt.Errorf("remote worktree merge failed or returned an incomplete result frame: %s", truncateRunesForSubAgent(out, 400))
	}
	return fmt.Sprintf("merged remote git worktree T%d via controlled cherry-pick (%s → %s)", r.StepIndex, r.IsolateDir, r.SourceDir), nil
}

func remoteIsolateDeclaredWrites(values ...[]string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values[0]...)
}

func validateRemoteIsolateWriteClaims(writes []string) error {
	if len(writes) == 0 {
		// An isolate needs a precise merge boundary. Without it the SSH-side
		// "git add -A" could turn an arbitrary model edit into a primary-tree
		// cherry-pick.
		return fmt.Errorf("remote isolated merge requires at least one declared write path")
	}
	for _, write := range writes {
		// A trailing slash denotes a directory claim. All other normalization is
		// rejected rather than silently cleaned, so the frozen claim used for the
		// SSH-side admission check is exactly what was reviewed.
		trimmed := strings.TrimSpace(strings.ReplaceAll(write, "\\", "/"))
		if strings.HasSuffix(trimmed, "/") {
			trimmed = strings.TrimSuffix(trimmed, "/")
		}
		if _, err := normalizeRemoteIsolateRelativeFile(trimmed); err != nil {
			return fmt.Errorf("remote isolated merge has invalid declared write path %q: %w", write, err)
		}
	}
	return nil
}

// normalizeRemoteIsolateRelativeFile accepts one exact project-relative file
// path. It is shared by automatic merge claims and manual remote-conflict
// operations so a conflict action can never escape either remote tree through
// a lexical traversal such as "dir/../../outside".
func normalizeRemoteIsolateRelativeFile(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" || value == "." || strings.HasPrefix(value, "/") ||
		strings.ContainsAny(value, "*?[${\r\n\x00") {
		return "", fmt.Errorf("must be a plain relative path")
	}
	clean := path.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != value {
		return "", fmt.Errorf("path traversal or normalization is not allowed")
	}
	return clean, nil
}

// remoteIsolateClaimContainsFile reports whether a frozen file/directory claim
// authorizes one exact relative file. Directory claims retain their trailing
// slash semantics; a prefix without that slash is deliberately not a directory
// claim ("cmd" does not authorize "cmd/main.go").
func remoteIsolateClaimContainsFile(claim, file string) bool {
	file, err := normalizeRemoteIsolateRelativeFile(file)
	if err != nil {
		return false
	}
	claim = strings.TrimSpace(strings.ReplaceAll(claim, "\\", "/"))
	isDir := strings.HasSuffix(claim, "/")
	claim = strings.TrimSuffix(claim, "/")
	claim, err = normalizeRemoteIsolateRelativeFile(claim)
	if err != nil {
		return false
	}
	if isDir {
		return strings.HasPrefix(file, claim+"/")
	}
	return file == claim
}

func remoteIsolateFileWithinFrozenWriteScope(file string, claims []string) bool {
	for _, claim := range claims {
		if remoteIsolateClaimContainsFile(claim, file) {
			return true
		}
	}
	return false
}

// isManagedRemoteCodingIsolatePath is the destructive-operation boundary for
// remote artifacts. Conflict records are persisted locally and therefore must
// never be trusted as arbitrary remote rm/cp roots after restart. Creation only
// ever uses direct children of /tmp with one of these two prefixes.
func isManagedRemoteCodingIsolatePath(dir string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" || path.Clean(dir) != dir || path.Dir(dir) != "/tmp" {
		return false
	}
	base := path.Base(dir)
	return strings.HasPrefix(base, "maclaw-wt-") || strings.HasPrefix(base, "maclaw-coding-")
}

func remoteIsolateMergeMarkers() (string, string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", "", fmt.Errorf("generate remote isolate merge marker: %w", err)
	}
	base := "__MACLAW_REMOTE_ISOLATE_" + hex.EncodeToString(nonce[:])
	return base + "_BEGIN__", base + "_END__", nil
}

func remoteGitWorktreeMergeCommand(worktree, source string, stepIndex int, writes []string, markerStart, markerEnd string) string {
	quotedWrites := make([]string, 0, len(writes))
	for _, write := range writes {
		quotedWrites = append(quotedWrites, remoteShellQuote(strings.TrimSpace(strings.ReplaceAll(write, "\\", "/"))))
	}
	return fmt.Sprintf(
		`set -eu; WT=%s; SRC=%s; set -- %s; cd "$WT"; `+
			`test "$#" -gt 0; changes_file=$(mktemp); trap 'rm -f "$changes_file"' EXIT HUP INT TERM; `+
			`{ git diff --name-only HEAD; git ls-files --others --exclude-standard; } | LC_ALL=C sort -u > "$changes_file"; while IFS= read -r path || [ -n "$path" ]; do allowed=0; for claim in "$@"; do case "$claim" in */) case "$path" in "$claim"*) allowed=1 ;; esac ;; *) [ "$path" = "$claim" ] && allowed=1 ;; esac; done; [ "$allowed" -eq 1 ] || { echo "undeclared isolate write: $path" >&2; exit 42; }; done < "$changes_file"; `+
			`if [ -n "$(git status --porcelain)" ]; then git add -A; git commit -m "coding workbench T%d remote" --no-verify; commit=$(git rev-parse HEAD); else commit=""; fi; `+
			`cd "$SRC"; test -z "$(git status --porcelain)"; if [ -n "$commit" ]; then if ! git cherry-pick --allow-empty "$commit"; then git cherry-pick --abort || true; exit 43; fi; fi; `+
			`printf '\n%%s\n%%s\n' %s %s`,
		remoteShellQuote(worktree), remoteShellQuote(source), strings.Join(quotedWrites, " "), stepIndex, remoteShellQuote(markerStart), remoteShellQuote(markerEnd),
	)
}

func remoteIsolateMergeFrameComplete(output, markerStart, markerEnd string) bool {
	markerStart, markerEnd = strings.TrimSpace(markerStart), strings.TrimSpace(markerEnd)
	if markerStart == "" || markerEnd == "" || markerStart == markerEnd {
		return false
	}
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	begin := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == markerStart {
			begin = i
		}
	}
	if begin < 0 {
		return false
	}
	for _, line := range lines[begin+1:] {
		if strings.TrimSpace(line) == markerEnd {
			return true
		}
	}
	return false
}

func (r *remoteCodingIsolate) hasChanges(h *IMMessageHandler) (dirty bool, ok bool) {
	if r == nil || !r.created || h == nil || strings.TrimSpace(r.IsolateDir) == "" {
		return false, false
	}
	out := h.sshExec(map[string]interface{}{
		"session_id": r.SessionID,
		"command": fmt.Sprintf(
			`set -e; cd %s; if [ -n "$(git status --porcelain)" ]; then echo "__MACLAW_ISO_DIRTY__"; else echo "__MACLAW_ISO_CLEAN__"; fi`,
			remoteShellQuote(r.IsolateDir),
		),
		"wait_seconds": float64(20),
	})
	if strings.Contains(out, "__MACLAW_ISO_DIRTY__") {
		return true, true
	}
	if strings.Contains(out, "__MACLAW_ISO_CLEAN__") {
		return false, true
	}
	return false, false
}

func isolatedRemoteWorkerShouldKeepIsolate(iso *remoteCodingIsolate, h *IMMessageHandler, res *RemoteCodingSubAgentResult) bool {
	if res != nil && (len(res.FilesModified) > 0 || len(res.FilesCreated) > 0) {
		return true
	}
	dirty, probed := iso.hasChanges(h)
	if !probed {
		return true
	}
	return dirty
}

func (r *remoteCodingIsolate) cleanup(h *IMMessageHandler) {
	if r == nil || !r.created || h == nil {
		return
	}
	if !isManagedRemoteCodingIsolatePath(r.IsolateDir) {
		log.Printf("[remote-isolate] refusing cleanup of unmanaged path step=%d dir=%q", r.StepIndex, r.IsolateDir)
		return
	}
	isWT := strings.Contains(r.IsolateDir, "maclaw-wt-")
	var cmd string
	if isWT {
		cmd = fmt.Sprintf(
			`set +e; SRC=%s; WT=%s; cd "$SRC" 2>/dev/null; `+
				`BR=$(git -C "$WT" rev-parse --abbrev-ref HEAD 2>/dev/null); `+
				`git worktree remove --force "$WT" 2>/dev/null; rm -rf -- "$WT"; `+
				`[ -n "$BR" ] && git branch -D "$BR" 2>/dev/null; git worktree prune 2>/dev/null; echo "__MACLAW_ISO_RM__"`,
			remoteShellQuote(r.SourceDir),
			remoteShellQuote(r.IsolateDir),
		)
	} else {
		cmd = fmt.Sprintf(`rm -rf -- %s; echo "__MACLAW_ISO_RM__"`, remoteShellQuote(r.IsolateDir))
	}
	_ = h.sshExec(map[string]interface{}{
		"session_id":   r.SessionID,
		"command":      cmd,
		"wait_seconds": float64(30),
	})
	r.created = false
	log.Printf("[remote-isolate] cleaned step=%d dir=%s", r.StepIndex, r.IsolateDir)
}

// shouldUseRemoteCodingIsolate mirrors local worktree policy for remote hosts.
// dependsOn empty + planned multi-step can still isolate under auto via cheap
// git worktree; full dir-copy is reserved for always (see createRemoteCodingIsolate).
func shouldUseRemoteCodingIsolate(mode string, planned bool, title, description string, dependsOn []int) bool {
	mode = normalizeCodingWorktreeMode(mode)
	if mode == codingWorktreeModeOff {
		return false
	}
	if isCodingPlanExploreOnlyStep(title, description) {
		return false
	}
	if mode == codingWorktreeModeAlways {
		return true
	}
	// auto: only independent write steps (no deps) — sequential chain stays on main.
	return planned && len(dependsOn) == 0
}
