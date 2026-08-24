package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedRepoInspectAdapter        = "semantic_inspect_trusted_repo"
	semanticTrustedRepoInspectImplementation = "trusted-repo-inspect-v1"
	semanticTrustedRepoInspectTimeout        = 10 * time.Second
	semanticTrustedRepoInspectMaxBytes       = 4000
)

func semanticUnpublishedLegacyRepoInspectProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityRepoInspectVCS {
			return true
		}
	}
	return false
}

func semanticTrustedRepoInspectDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedRepoInspectAdapter,
			"description": "Inspect the bound workspace git status together with unstaged and staged diffs. The repository is host-bound; no path, staged, or project fields are accepted.",
			"parameters":  semanticTrustedRepoInspectInvocationSchema(),
		},
	}
}

func semanticTrustedRepoInspectInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func semanticTrustedRepoInspectArgsAllowed(args map[string]interface{}) error {
	if len(args) == 0 {
		return nil
	}
	return fmt.Errorf("trusted_repo_inspect_arguments_rejected")
}

func (h *IMMessageHandler) inspectTrustedRepo(principalID string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_repo_inspect_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_repo_inspect_principal_required")
	}
	if h.semanticTrustedRepoInspect != nil {
		return h.semanticTrustedRepoInspect(principalID)
	}
	workspace := trustedPrincipalBoundWorkspace(h, principalID)
	if strings.TrimSpace(workspace) == "" {
		return "", fmt.Errorf("trusted_repo_inspect_unavailable")
	}
	return inspectTrustedRepoWorkspace(context.Background(), workspace)
}

func inspectTrustedRepoWorkspace(ctx context.Context, workspace string) (string, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "", fmt.Errorf("trusted_repo_inspect_unavailable")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "git is not available on this host.", nil
	}
	inside, err := runTrustedRepoGit(ctx, workspace, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(inside) != "true" {
		return "The workspace is not a git repository.", nil
	}
	status, err := runTrustedRepoGit(ctx, workspace, "status", "--porcelain=v1", "-b", "--untracked-files=all")
	if err != nil {
		return "", fmt.Errorf("git status: %w", err)
	}
	unstaged, err := runTrustedRepoGit(ctx, workspace, "diff", "--stat")
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}
	staged, err := runTrustedRepoGit(ctx, workspace, "diff", "--cached", "--stat")
	if err != nil {
		return "", fmt.Errorf("git diff --cached: %w", err)
	}
	var b strings.Builder
	b.WriteString("git status:\n")
	if strings.TrimSpace(status) == "" {
		b.WriteString("working tree clean\n")
	} else {
		b.WriteString(status)
		if !strings.HasSuffix(status, "\n") {
			b.WriteByte('\n')
		}
	}
	b.WriteString("\ngit diff --stat:\n")
	if strings.TrimSpace(unstaged) == "" {
		b.WriteString("no unstaged differences\n")
	} else {
		b.WriteString(unstaged)
		if !strings.HasSuffix(unstaged, "\n") {
			b.WriteByte('\n')
		}
	}
	b.WriteString("\ngit diff --cached --stat:\n")
	if strings.TrimSpace(staged) == "" {
		b.WriteString("no staged differences\n")
	} else {
		b.WriteString(staged)
		if !strings.HasSuffix(staged, "\n") {
			b.WriteByte('\n')
		}
	}
	return truncateTrustedRepoInspect(b.String()), nil
}

func runTrustedRepoGit(ctx context.Context, workspace string, args ...string) (string, error) {
	return runTrustedRepoGitWithin(ctx, workspace, semanticTrustedRepoInspectTimeout, trustedRepoGitEnv(), args...)
}

// runTrustedRepoGitWithin is the shared runner behind every trusted git call.
// Read-only inspection and a network-facing push need the same environment
// hygiene, window suppression, and stderr handling but very different time
// bounds, so the bound and the environment are parameters.
func runTrustedRepoGitWithin(ctx context.Context, workspace string, timeout time.Duration, env []string, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workspace
	cmd.Env = env
	hideCommandWindow(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	return stdout.String(), nil
}

func trustedRepoGitEnv() []string {
	out := make([]string, 0, 16)
	for _, env := range os.Environ() {
		key, _, _ := strings.Cut(env, "=")
		switch strings.ToUpper(strings.TrimSpace(key)) {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES":
			continue
		}
		out = append(out, env)
	}
	return out
}

func truncateTrustedRepoInspect(text string) string {
	if len(text) <= semanticTrustedRepoInspectMaxBytes {
		return text
	}
	return text[:semanticTrustedRepoInspectMaxBytes] + "\n...(truncated)"
}

func semanticTrustedRepoInspectResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_repo_inspect_delivery_token")
	}
	if strings.Contains(text, "git_status") || strings.Contains(text, "git_diff") || strings.Contains(text, "git_commit") {
		return "", fmt.Errorf("trusted_repo_inspect_legacy_name")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_repo_inspect_empty")
	}
	return text, nil
}
