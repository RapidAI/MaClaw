package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedRepoMutateAdapter        = "semantic_mutate_trusted_repo"
	semanticTrustedRepoMutateImplementation = "trusted-repo-mutate-v1"
	semanticTrustedRepoMutateTimeout        = 30 * time.Second
	// A push crosses the network, so it needs a wider bound than a local
	// commit. Reading the receipt back is a much smaller round trip.
	semanticTrustedRepoPushTimeout    = 2 * time.Minute
	semanticTrustedRepoReceiptTimeout = 30 * time.Second
)

func semanticUnpublishedLegacyRepoMutateProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityRepoMutateVCS {
			return true
		}
	}
	return false
}

func semanticTrustedRepoMutateDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedRepoMutateAdapter,
			"description": "Commit or push the bound workspace repository. Push requires a remote receipt.",
			"parameters":  semanticTrustedRepoMutateInvocationSchema(),
		},
	}
}

func semanticTrustedRepoMutateInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action":  map[string]interface{}{"type": "string"},
			"message": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"action"},
		"additionalProperties": false,
	}
}

func semanticTrustedRepoMutateArgsAllowed(args map[string]interface{}) (action, message string, err error) {
	if len(args) > 2 {
		return "", "", fmt.Errorf("trusted_repo_mutate_arguments_rejected")
	}
	hasAction := false
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", "", fmt.Errorf("trusted_repo_mutate_arguments_rejected")
		}
		switch key {
		case "action":
			action, hasAction = value, true
		case "message":
			message = value
		default:
			return "", "", fmt.Errorf("trusted_repo_mutate_arguments_rejected")
		}
	}
	action = strings.ToLower(strings.TrimSpace(action))
	message = strings.TrimSpace(message)
	if !hasAction {
		return "", "", fmt.Errorf("trusted_repo_mutate_action_required")
	}
	switch action {
	case "commit", "push":
	default:
		return "", "", fmt.Errorf("trusted_repo_mutate_action_rejected")
	}
	if action == "commit" && message == "" {
		return "", "", fmt.Errorf("trusted_repo_mutate_message_required")
	}
	return action, message, nil
}

func (h *IMMessageHandler) mutateTrustedRepo(principalID, action, message string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_repo_mutate_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_repo_mutate_principal_required")
	}
	if h.semanticTrustedRepoMutate != nil {
		return h.semanticTrustedRepoMutate(principalID, action, message)
	}
	workspace := trustedPrincipalBoundWorkspace(h, principalID)
	if strings.TrimSpace(workspace) == "" {
		return "", fmt.Errorf("trusted_repo_mutate_workspace_unavailable")
	}
	ctx := context.Background()
	switch action {
	case "commit":
		return commitTrustedRepo(ctx, workspace, message)
	case "push":
		return pushTrustedRepo(ctx, workspace)
	default:
		return "", fmt.Errorf("trusted_repo_mutate_action_rejected")
	}
}

// commitTrustedRepo stages tracked modifications and commits them, treating a
// moved HEAD rather than a zero exit status as the receipt.
func commitTrustedRepo(ctx context.Context, workspace, message string) (string, error) {
	// An unborn branch has no HEAD to read, which is a valid starting state for
	// the first commit rather than a failure.
	before, _ := runTrustedRepoGit(ctx, workspace, "rev-parse", "HEAD")
	before = strings.TrimSpace(before)

	// Stage tracked modifications, deletions included. Untracked files are
	// deliberately left alone: the caller supplies only a message, so sweeping
	// in whatever else happens to sit in the workspace would commit content no
	// one named.
	if _, err := runTrustedRepoGitWithin(ctx, workspace, semanticTrustedRepoMutateTimeout, trustedRepoGitEnv(), "add", "-u"); err != nil {
		return "", err
	}
	// Decide "is there anything to commit" by reading the index rather than by
	// matching git's refusal text, which is localised and would silently stop
	// being recognised on a non-English host.
	staged, err := runTrustedRepoGit(ctx, workspace, "diff", "--cached", "--name-only")
	if err != nil {
		return "", fmt.Errorf("trusted_repo_mutate_stage_unobserved")
	}
	if strings.TrimSpace(staged) == "" {
		return "", fmt.Errorf("trusted_repo_mutate_nothing_to_commit")
	}

	if _, err := runTrustedRepoGitWithin(ctx, workspace, semanticTrustedRepoMutateTimeout, trustedRepoGitEnv(), "commit", "-m", message); err != nil {
		return "", err
	}
	digest, err := runTrustedRepoGit(ctx, workspace, "rev-parse", "HEAD")
	digest = strings.TrimSpace(digest)
	if err != nil || digest == "" {
		return "", fmt.Errorf("trusted_repo_mutate_head_unobserved")
	}
	// The receipt is that HEAD moved, not that the command exited zero.
	if digest == before {
		return "", fmt.Errorf("trusted_repo_mutate_head_unobserved")
	}
	return "commit " + digest, nil
}

// pushTrustedRepo publishes the bound workspace branch and then observes the
// remote ref to decide whether the effect landed.
//
// A push is an external effect, so the push command's own exit status is not a
// receipt: a command that fails may still have moved the ref, and one that
// succeeds may have raced another writer. The authoritative answer comes from
// reading the remote ref back and comparing it with the commit that was pushed.
// When that read cannot be completed the outcome is reported as unknown rather
// than as success or failure, because an unknown external effect must not be
// replayed.
func pushTrustedRepo(ctx context.Context, workspace string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("trusted_repo_mutate_unavailable")
	}
	branch, err := runTrustedRepoGit(ctx, workspace, "rev-parse", "--abbrev-ref", "HEAD")
	branch = strings.TrimSpace(branch)
	if err != nil || branch == "" || branch == "HEAD" {
		return "", fmt.Errorf("trusted_repo_mutate_branch_unresolved")
	}
	local, err := runTrustedRepoGit(ctx, workspace, "rev-parse", "HEAD")
	local = strings.TrimSpace(local)
	if err != nil || local == "" {
		return "", fmt.Errorf("trusted_repo_mutate_head_unobserved")
	}
	upstream, upstreamErr := runTrustedRepoGit(ctx, workspace, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	remote, remoteBranch, ok := splitTrustedRepoUpstream(upstream)
	if upstreamErr != nil || !ok {
		// Creating an upstream decides where this repository publishes. That is
		// an operator decision about remote state, and a receipt can only
		// observe such a decision, never make it.
		return "", fmt.Errorf("trusted_repo_mutate_upstream_unset")
	}

	_, pushErr := runTrustedRepoGitWithin(ctx, workspace, semanticTrustedRepoPushTimeout, trustedRepoGitRemoteEnv(), "push")
	observed, observeErr := observeTrustedRemoteRef(ctx, workspace, remote, remoteBranch)
	switch {
	case observeErr != nil:
		return "", fmt.Errorf("trusted_repo_mutate_push_receipt_unknown")
	case observed == local:
		return "push " + local + " -> " + remote + "/" + remoteBranch, nil
	case pushErr != nil:
		// The remote still holds its previous commit and the command reported
		// failure, so this is an observed non-effect rather than an unknown.
		return "", fmt.Errorf("trusted_repo_mutate_push_rejected")
	default:
		return "", fmt.Errorf("trusted_repo_mutate_push_receipt_unknown")
	}
}

// observeTrustedRemoteRef reads the commit the remote currently holds for the
// branch. This is the receipt: it comes from the remote itself rather than from
// anything the local push reported.
func observeTrustedRemoteRef(ctx context.Context, workspace, remote, branch string) (string, error) {
	out, err := runTrustedRepoGitWithin(ctx, workspace, semanticTrustedRepoReceiptTimeout, trustedRepoGitRemoteEnv(),
		"ls-remote", "--exit-code", remote, "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) == 0 || strings.TrimSpace(fields[0]) == "" {
		return "", fmt.Errorf("trusted_repo_mutate_push_receipt_unknown")
	}
	return strings.TrimSpace(fields[0]), nil
}

// splitTrustedRepoUpstream splits `origin/feature/x` into its remote and its
// branch. Git forbids `/` in a remote name, so the first separator is the
// boundary even when the branch itself contains one.
func splitTrustedRepoUpstream(upstream string) (remote, branch string, ok bool) {
	upstream = strings.TrimSpace(upstream)
	if upstream == "" {
		return "", "", false
	}
	remote, branch, found := strings.Cut(upstream, "/")
	remote, branch = strings.TrimSpace(remote), strings.TrimSpace(branch)
	if !found || remote == "" || branch == "" {
		return "", "", false
	}
	return remote, branch, true
}

// trustedRepoGitRemoteEnv keeps a network git call from blocking on an
// interactive credential prompt. Without this a push against a repository the
// host cannot authenticate to would hold the turn open until its timeout
// instead of failing immediately.
func trustedRepoGitRemoteEnv() []string {
	env := trustedRepoGitEnv()
	out := make([]string, 0, len(env)+2)
	sshCommandSet := false
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(strings.TrimSpace(key)) {
		case "GIT_TERMINAL_PROMPT":
			continue
		case "GIT_SSH_COMMAND":
			sshCommandSet = true
		}
		out = append(out, entry)
	}
	out = append(out, "GIT_TERMINAL_PROMPT=0")
	if !sshCommandSet {
		out = append(out, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	return out
}

func semanticTrustedRepoMutateResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_repo_mutate_delivery_token")
	}
	if strings.Contains(text, "git_commit") || strings.Contains(text, "git_push") {
		return "", fmt.Errorf("trusted_repo_mutate_legacy_name")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_repo_mutate_empty")
	}
	return text, nil
}
