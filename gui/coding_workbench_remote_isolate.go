package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// remoteCodingIsolate is a temporary remote project copy for write isolation
// (SSH analogue of local git worktrees when the remote host is not using them).
type remoteCodingIsolate struct {
	SessionID   string
	SourceDir   string
	IsolateDir  string
	StepIndex   int
	created     bool
}

// createRemoteCodingIsolate creates an isolated remote workspace.
// Prefers `git worktree` when the remote project is a git repo with commits;
// full directory copy is used only when allowFullCopy is true (always mode).
func createRemoteCodingIsolate(h *IMMessageHandler, sessionID, projectDir string, stepIndex int, allowFullCopy bool) (*remoteCodingIsolate, error) {
	if h == nil {
		return nil, fmt.Errorf("nil handler")
	}
	sessionID = strings.TrimSpace(sessionID)
	projectDir = strings.TrimSpace(projectDir)
	if sessionID == "" || projectDir == "" {
		return nil, fmt.Errorf("missing session or project")
	}
	id := fmt.Sprintf("%d-%d", stepIndex, time.Now().UnixNano()%1e9)

	// Try remote git worktree first (cheap, preferred).
	if iso, err := tryCreateRemoteGitWorktree(h, sessionID, projectDir, stepIndex, id); err == nil && iso != nil {
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
		SessionID:  sessionID,
		SourceDir:  projectDir,
		IsolateDir: isolateDir,
		StepIndex:  stepIndex,
		created:    true,
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

// mergeBack integrates isolate changes back into source.
// For git worktrees (path contains maclaw-wt-): try commit + cherry-pick, else rsync.
// For directory copies: rsync/cp exclude .git.
func (r *remoteCodingIsolate) mergeBack(h *IMMessageHandler) (string, error) {
	if r == nil || !r.created || h == nil {
		return "", nil
	}
	isWT := strings.Contains(r.IsolateDir, "maclaw-wt-")
	if isWT {
		cmd := fmt.Sprintf(
			`set -e; WT=%s; SRC=%s; `+
				`cd "$WT"; `+
				`if [ -n "$(git status --porcelain 2>/dev/null)" ]; then git add -A; git commit -m "coding workbench T%d remote" --no-verify || true; fi; `+
				`BR=$(git rev-parse --abbrev-ref HEAD); `+
				`cd "$SRC"; `+
				`if [ -z "$(git status --porcelain 2>/dev/null)" ]; then `+
				`  if git cherry-pick --allow-empty "$BR"; then echo "__MACLAW_ISO_MERGE_OK__:cherry-pick"; exit 0; fi; `+
				`  git cherry-pick --abort 2>/dev/null || true; `+
				`fi; `+
				`if command -v rsync >/dev/null 2>&1; then rsync -a --exclude .git "$WT"/ "$SRC"/; `+
				`else cp -a "$WT"/. "$SRC"/; fi; `+
				`echo "__MACLAW_ISO_MERGE_OK__:rsync"`,
			remoteShellQuote(r.IsolateDir),
			remoteShellQuote(r.SourceDir),
			r.StepIndex,
		)
		out := h.sshExec(map[string]interface{}{
			"session_id":   r.SessionID,
			"command":      cmd,
			"wait_seconds": float64(180),
		})
		if !strings.Contains(out, "__MACLAW_ISO_MERGE_OK__") {
			return "", fmt.Errorf("remote worktree merge failed: %s", truncateRunesForSubAgent(out, 400))
		}
		mode := "rsync"
		if strings.Contains(out, "cherry-pick") {
			mode = "cherry-pick"
		}
		return fmt.Sprintf("merged remote git worktree T%d via %s (%s → %s)", r.StepIndex, mode, r.IsolateDir, r.SourceDir), nil
	}

	cmd := fmt.Sprintf(
		`set -e; SRC=%s; DST=%s; `+
			`if command -v rsync >/dev/null 2>&1; then rsync -a --exclude .git "$SRC"/ "$DST"/; `+
			`else cp -a "$SRC"/. "$DST"/; fi; `+
			`echo "__MACLAW_ISO_MERGE_OK__"`,
		remoteShellQuote(r.IsolateDir),
		remoteShellQuote(r.SourceDir),
	)
	out := h.sshExec(map[string]interface{}{
		"session_id":   r.SessionID,
		"command":      cmd,
		"wait_seconds": float64(120),
	})
	if !strings.Contains(out, "__MACLAW_ISO_MERGE_OK__") {
		return "", fmt.Errorf("remote isolate merge failed: %s", truncateRunesForSubAgent(out, 400))
	}
	return fmt.Sprintf("merged remote isolate T%d %s → %s", r.StepIndex, r.IsolateDir, r.SourceDir), nil
}

func (r *remoteCodingIsolate) cleanup(h *IMMessageHandler) {
	if r == nil || !r.created || h == nil {
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
