package guiapp

import (
	"context"
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
	result, err := h.runRemoteSetupCommand(context.Background(), sessionID, remoteCodingCopyIsolateCommand(projectDir, isolateDir), 120*time.Second)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("remote isolate create failed: %s", truncateRunesForSubAgent(formatRemoteSetupFailure(result), 400))
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
	result, err := h.runRemoteSetupCommand(context.Background(), sessionID, remoteGitWorktreeAddCommand(projectDir, branch, isolateDir), 90*time.Second)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("remote git worktree failed: %s", truncateRunesForSubAgent(formatRemoteSetupFailure(result), 300))
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
	result, err := h.runRemoteSetupCommand(context.Background(), r.SessionID, remoteGitWorktreeMergeCommand(r.IsolateDir, r.SourceDir, r.StepIndex, writes, start, end), 180*time.Second)
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 || !remoteIsolateMergeFrameComplete(result.Stdout, start, end) {
		return "", fmt.Errorf("remote worktree merge failed or returned an incomplete result frame: %s", truncateRunesForSubAgent(formatRemoteSetupFailure(result), 400))
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
	// A clean worktree is not an empty branch. Commits already on the isolate
	// are part of the merge: the success path deletes that branch. Their
	// paths join the uncommitted set before admission, and every commit since
	// the fork is cherry-picked. Uncommitted paths are committed through a
	// temporary index, so the worktree's own index is left untouched. git add
	// lists only those paths.
	// Git's per-file log goes to stderr. The exec channel keeps 1MB of stdout,
	// and that stream has to stay the marker frame.
	return fmt.Sprintf(
		`set -eu; WT=%s; SRC=%s; set -- %s; cd "$WT"; `+
			`test "$#" -gt 0; pending_file=$(mktemp); changes_file=$(mktemp); sorted_file=$(mktemp); commits_file=$(mktemp); tmp_index=$(mktemp); rm -f "$tmp_index"; trap 'rm -f "$pending_file" "$changes_file" "$sorted_file" "$commits_file" "$tmp_index" "$tmp_index.lock"' EXIT HUP INT TERM; `+
			`src_head=$(git -C "$SRC" rev-parse HEAD) || exit 1; base=$(git merge-base HEAD "$src_head") || exit 1; `+
			`git rev-list --reverse "$base"..HEAD > "$commits_file" || exit 1; `+
			`git -c core.quotePath=false diff --name-only HEAD > "$pending_file"; git -c core.quotePath=false ls-files --others --exclude-standard >> "$pending_file"; `+
			`LC_ALL=C sort -u -o "$sorted_file" "$pending_file"; cp "$sorted_file" "$pending_file"; cp "$sorted_file" "$changes_file"; `+
			`while IFS= read -r sha || [ -n "$sha" ]; do [ -n "$sha" ] || continue; git -c core.quotePath=false diff-tree --no-commit-id --name-only -r -m "$sha" >> "$changes_file" || exit 1; done < "$commits_file"; `+
			`LC_ALL=C sort -u -o "$sorted_file" "$changes_file"; cp "$sorted_file" "$changes_file"; `+
			`while IFS= read -r path || [ -n "$path" ]; do [ -n "$path" ] || continue; allowed=0; for claim in "$@"; do case "$claim" in */) case "$path" in "$claim"*) allowed=1 ;; esac ;; *) [ "$path" = "$claim" ] && allowed=1 ;; esac; done; [ "$allowed" -eq 1 ] || { echo "undeclared isolate write: $path" >&2; exit 42; }; done < "$changes_file"; `+
			`porcelain=$(git status --porcelain) || exit 1; if [ -n "$porcelain" ]; then [ -s "$pending_file" ] || { echo 'status is dirty but no paths were admitted' >&2; exit 1; }; GIT_INDEX_FILE=$tmp_index git read-tree HEAD || exit 1; while IFS= read -r path || [ -n "$path" ]; do [ -n "$path" ] || continue; GIT_INDEX_FILE=$tmp_index git add -- "$path" || exit 1; done < "$pending_file"; GIT_INDEX_FILE=$tmp_index git -c user.name=MaClaw -c user.email=maclaw@localhost -c commit.gpgsign=false commit -m "coding workbench T%d remote" --no-verify >&2 || exit 1; fi; `+
			`git rev-list --reverse "$base"..HEAD > "$commits_file" || exit 1; `+
			`cd "$SRC"; now=$(git rev-parse HEAD) || exit 1; [ "$now" = "$src_head" ] || { echo 'primary HEAD changed during isolate merge' >&2; exit 1; }; src_porcelain=$(git status --porcelain) || exit 1; if [ -n "$src_porcelain" ]; then echo 'primary checkout is dirty' >&2; exit 1; fi; `+
			`while IFS= read -r sha || [ -n "$sha" ]; do [ -n "$sha" ] || continue; if ! git -c user.name=MaClaw -c user.email=maclaw@localhost -c commit.gpgsign=false cherry-pick --allow-empty "$sha" >&2; then git cherry-pick --abort >&2 || true; git reset --hard "$src_head" >&2 || exit 1; exit 43; fi; done < "$commits_file"; `+
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
	result, err := h.runRemoteSetupCommand(context.Background(), r.SessionID, remoteIsolateDirtyCommand(r.IsolateDir), 20*time.Second)
	if err != nil || result.ExitCode != 0 {
		return false, false
	}
	return strings.TrimSpace(result.Stdout) != "", true
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
		cmd = remoteWorktreeCleanupCommand(r.SourceDir, r.IsolateDir)
	} else {
		cmd = fmt.Sprintf(`rm -rf -- %s`, remoteShellQuote(r.IsolateDir))
	}
	if result, err := h.runRemoteSetupCommand(context.Background(), r.SessionID, cmd, 30*time.Second); err != nil || result.ExitCode != 0 {
		detail := formatRemoteSetupFailure(result)
		if err != nil {
			detail = err.Error()
		}
		log.Printf("[remote-isolate] cleanup command failed step=%d dir=%s: %s", r.StepIndex, r.IsolateDir, detail)
	}
	r.created = false
	log.Printf("[remote-isolate] cleaned step=%d dir=%s", r.StepIndex, r.IsolateDir)
}

// remoteGitWorktreeAddCommand is an exec-channel script. Exit status 0 means
// the worktree exists. It is not typed into the login shell, so set -e cannot
// end the verified SSH session and the command text cannot be mistaken for
// success.
func remoteGitWorktreeAddCommand(projectDir, branch, isolateDir string) string {
	repo := remoteShellQuote(projectDir)
	branchQ := remoteShellQuote(branch)
	isolateQ := remoteShellQuote(isolateDir)
	// git -C keeps every step on the declared repository. A cd here would
	// only affect this process, but a later relative command would follow it.
	return fmt.Sprintf(
		`set -e
git -C %s rev-parse --is-inside-work-tree >/dev/null
git -C %s rev-parse --verify HEAD >/dev/null
git -C %s branch -D %s >/dev/null 2>&1 || true
rm -rf -- %s
git -C %s worktree add -b %s %s HEAD
`, repo, repo, repo, branchQ, isolateQ, repo, branchQ, isolateQ)
}

// remoteCodingCopyIsolateCommand copies a project as its own process.
// Exit status 0 means the copy exists.
func remoteCodingCopyIsolateCommand(projectDir, isolateDir string) string {
	return fmt.Sprintf(
		`set -e
SRC=%s
DST=%s
mkdir -p "$DST"
if command -v rsync >/dev/null 2>&1; then
  rsync -a "$SRC"/ "$DST"/ || cp -a "$SRC"/. "$DST"/
else
  cp -a "$SRC"/. "$DST"/
fi
`, remoteShellQuote(projectDir), remoteShellQuote(isolateDir))
}

// remoteWorktreeCleanupCommand removes one managed worktree. Exit status is
// whether that directory is gone. worktree prune often exits 0 after a
// failed remove, so it must not be the script's result.
func remoteWorktreeCleanupCommand(sourceDir, isolateDir string) string {
	return fmt.Sprintf(
		`set +e
SRC=%s
WT=%s
fail=0
BR=$(git -C "$WT" rev-parse --abbrev-ref HEAD 2>/dev/null)
git -C "$SRC" worktree remove --force "$WT" >/dev/null 2>&1
rm -rf -- "$WT" || fail=1
if [ -e "$WT" ]; then fail=1; fi
if [ -n "$BR" ]; then git -C "$SRC" branch -D "$BR" >/dev/null 2>&1 || true; fi
git -C "$SRC" worktree prune >/dev/null 2>&1 || true
exit "$fail"
`, remoteShellQuote(sourceDir), remoteShellQuote(isolateDir))
}

func remoteIsolateDirtyCommand(isolateDir string) string {
	return fmt.Sprintf(`git -C %s status --porcelain`, remoteShellQuote(isolateDir))
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
