package agentservice

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostRepoMutateProviderID     = "core-repomutate"
	reviewedHostRepoMutateImplementation = "local"
	reviewedHostRepoMutateAdapterName    = "host_repo_mutate_vcs"
	reviewedHostRepoMutateTimeout        = 30 * time.Second
	// A push crosses the network, so it needs a wider bound than a local
	// commit. Reading the receipt back is a much smaller round trip.
	reviewedHostRepoPushTimeout    = 2 * time.Minute
	reviewedHostRepoReceiptTimeout = 30 * time.Second
)

type reviewedHostRepoMutator interface {
	MutateReviewedHostRepo(ctx context.Context, principal Principal, action, message string) (string, error)
}

func reviewedHostRepoMutateInvocationSchema() map[string]interface{} {
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

func reviewedHostRepoMutateContractDigest() string {
	return coretool.SchemaDigest([]byte("repo.mutate.vcs:v1:host-repomutate"))
}

// ProjectReviewedHostRepoMutateProvider projects a quarantined workspace
// commit/push. It is not a Skill/MCP discovery entry and must not import GUI
// git_commit / git_push. The closed schema accepts action and optional
// message. project_path, channel, and destination are rejected. Commit is
// complete only when HEAD is observed. Push without a remote receipt is
// unknown.
func ProjectReviewedHostRepoMutateProvider(mutator reviewedHostRepoMutator) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if mutator == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host repo mutator is unavailable")
	}
	parameters := reviewedHostRepoMutateInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host repo mutate schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostRepoMutateContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-repomutate-action-message-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostRepoMutateAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostRepoMutateProviderID,
			ImplementationID: reviewedHostRepoMutateImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityRepoMutate,
			Quality:    1,
		}},
		Effects: []coretool.EffectClass{coretool.EffectExternalEffect},
		Ready:   true,
	}
	definition := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "dynamic_provider",
			"description": "",
			"parameters":  parameters,
		},
	}
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostRepoMutate(mutator)}, nil
}

func AttachReviewedHostRepoMutateProvider(catalog DynamicSemanticCatalog, mutator reviewedHostRepoMutator) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostRepoMutateProvider(mutator)
	if err != nil {
		return DynamicSemanticCatalog{}, err
	}
	if err := catalog.add(provider, definition, dynamicSemanticRuntimeBinding{
		provider: provider.Binding,
		host:     &host,
	}); err != nil {
		return DynamicSemanticCatalog{}, err
	}
	return catalog, nil
}

func executeReviewedHostRepoMutate(mutator reviewedHostRepoMutator) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if mutator == nil {
			return "", fmt.Errorf("host_repo_mutate_unavailable")
		}
		action, message, err := reviewedHostRepoMutateArgsAllowed(args)
		if err != nil {
			return "", err
		}
		return mutator.MutateReviewedHostRepo(ctx, principal, action, message)
	}
}

func reviewedHostRepoMutateArgsAllowed(args map[string]interface{}) (action, message string, err error) {
	if len(args) > 2 {
		return "", "", fmt.Errorf("host_repo_mutate_arguments_rejected")
	}
	hasAction := false
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", "", fmt.Errorf("host_repo_mutate_arguments_rejected")
		}
		switch key {
		case "action":
			action, hasAction = value, true
		case "message":
			message = value
		default:
			return "", "", fmt.Errorf("host_repo_mutate_arguments_rejected")
		}
	}
	action = strings.ToLower(strings.TrimSpace(action))
	message = strings.TrimSpace(message)
	if !hasAction {
		return "", "", fmt.Errorf("host_repo_mutate_action_required")
	}
	switch action {
	case "commit", "push":
	default:
		return "", "", fmt.Errorf("host_repo_mutate_action_rejected")
	}
	if action == "commit" && message == "" {
		return "", "", fmt.Errorf("host_repo_mutate_message_required")
	}
	return action, message, nil
}

func reviewedHostRepoMutateResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("host_repo_mutate_delivery_token")
	}
	if strings.Contains(text, "git_commit") || strings.Contains(text, "git_push") {
		return "", fmt.Errorf("host_repo_mutate_legacy_name")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("host_repo_mutate_empty")
	}
	return text, nil
}

func (c *coreAgentCallbacks) MutateReviewedHostRepo(ctx context.Context, principal Principal, action, message string) (string, error) {
	if c == nil || strings.TrimSpace(c.workspace) == "" || c.delegateChild || c.runtimeReadOnlyChild {
		return "", fmt.Errorf("host_repo_mutate_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_repo_mutate_principal_mismatch")
	}
	args := map[string]interface{}{"action": action}
	if strings.TrimSpace(message) != "" {
		args["message"] = message
	}
	action, message, err := reviewedHostRepoMutateArgsAllowed(args)
	if err != nil {
		return "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	switch action {
	case "commit":
		out, err := commitReviewedHostRepo(ctx, c.workspace, message)
		if err != nil {
			return "", err
		}
		return reviewedHostRepoMutateResultProjection(out)
	case "push":
		out, err := pushReviewedHostRepo(ctx, c.workspace)
		if err != nil {
			return "", err
		}
		return reviewedHostRepoMutateResultProjection(out)
	default:
		return "", fmt.Errorf("host_repo_mutate_action_rejected")
	}
}

// pushReviewedHostRepo publishes the bound workspace branch and then observes
// the remote ref to decide whether the effect landed.
//
// A push is an external effect, so the host cannot treat the push command's own
// exit status as a receipt: a command that fails may still have moved the ref,
// and a command that succeeds may have raced another writer. The authoritative
// answer comes from reading the remote ref back and comparing it to the commit
// that was pushed. When that read cannot be completed the outcome is reported
// as unknown, never as success and never as a plain failure, because an unknown
// external effect must not be replayed.
func pushReviewedHostRepo(ctx context.Context, workspace string) (string, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "", fmt.Errorf("host_repo_mutate_unavailable")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("host_repo_mutate_unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	branch, err := runReviewedHostGit(ctx, workspace, "rev-parse", "--abbrev-ref", "HEAD")
	branch = strings.TrimSpace(branch)
	if err != nil || branch == "" || branch == "HEAD" {
		return "", fmt.Errorf("host_repo_mutate_branch_unresolved")
	}
	local, err := runReviewedHostGit(ctx, workspace, "rev-parse", "HEAD")
	local = strings.TrimSpace(local)
	if err != nil || local == "" {
		return "", fmt.Errorf("host_repo_mutate_head_unobserved")
	}
	upstream, upstreamErr := runReviewedHostGit(ctx, workspace, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	remote, remoteBranch, ok := splitReviewedHostUpstream(upstream)
	if upstreamErr != nil || !ok {
		// Creating an upstream decides where this repository publishes. That is
		// a host/operator decision about remote state, and a receipt can only
		// observe such a decision, never make it.
		return "", fmt.Errorf("host_repo_mutate_upstream_unset")
	}

	_, pushErr := runReviewedHostGitWithin(ctx, workspace, reviewedHostRepoPushTimeout, reviewedHostGitRemoteEnv(), "push")
	observed, observeErr := observeReviewedHostRemoteRef(ctx, workspace, remote, remoteBranch)
	switch {
	case observeErr != nil:
		return "", fmt.Errorf("host_repo_mutate_push_receipt_unknown")
	case observed == local:
		return "push " + local + " -> " + remote + "/" + remoteBranch, nil
	case pushErr != nil:
		// The remote still holds its previous commit and the command reported
		// failure, so this is an observed non-effect rather than an unknown.
		return "", fmt.Errorf("host_repo_mutate_push_rejected")
	default:
		return "", fmt.Errorf("host_repo_mutate_push_receipt_unknown")
	}
}

// observeReviewedHostRemoteRef reads the commit the remote currently holds for
// the branch. This is the receipt: it is taken from the remote itself rather
// than from anything the local push reported.
func observeReviewedHostRemoteRef(ctx context.Context, workspace, remote, branch string) (string, error) {
	out, err := runReviewedHostGitWithin(ctx, workspace, reviewedHostRepoReceiptTimeout, reviewedHostGitRemoteEnv(),
		"ls-remote", "--exit-code", remote, "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) == 0 || strings.TrimSpace(fields[0]) == "" {
		return "", fmt.Errorf("host_repo_mutate_push_receipt_unknown")
	}
	return strings.TrimSpace(fields[0]), nil
}

// splitReviewedHostUpstream splits `origin/feature/x` into its remote and its
// branch. Git forbids `/` in a remote name, so the first separator is the
// boundary even when the branch itself contains one.
func splitReviewedHostUpstream(upstream string) (remote, branch string, ok bool) {
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

// reviewedHostGitRemoteEnv keeps a network git call from blocking on an
// interactive credential prompt. Without this a push against a repository the
// host cannot authenticate to would hold the turn open until its timeout
// instead of failing immediately.
func reviewedHostGitRemoteEnv() []string {
	env := reviewedHostGitEnv()
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

func commitReviewedHostRepo(ctx context.Context, workspace, message string) (string, error) {
	workspace = strings.TrimSpace(workspace)
	message = strings.TrimSpace(message)
	if workspace == "" {
		return "", fmt.Errorf("host_repo_mutate_unavailable")
	}
	if message == "" {
		return "", fmt.Errorf("host_repo_mutate_message_required")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("host_repo_mutate_unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, reviewedHostRepoMutateTimeout)
	defer cancel()
	// An unborn branch has no HEAD to read, which is a valid starting state for
	// the first commit rather than a failure.
	before, _ := runReviewedHostGit(runCtx, workspace, "rev-parse", "HEAD")
	before = strings.TrimSpace(before)

	// Stage tracked modifications, deletions included. Untracked files are
	// deliberately left alone: the caller supplies only a message, so sweeping
	// in whatever else happens to sit in the workspace would commit content no
	// one named.
	if _, err := runReviewedHostGit(runCtx, workspace, "add", "-u"); err != nil {
		if runCtx.Err() != nil {
			return "", runCtx.Err()
		}
		return "", err
	}
	// Decide "is there anything to commit" by reading the index rather than by
	// matching git's refusal text, which is localised and would silently stop
	// being recognised on a non-English host.
	staged, err := runReviewedHostGit(runCtx, workspace, "diff", "--cached", "--name-only")
	if err != nil {
		return "", fmt.Errorf("host_repo_mutate_stage_unobserved")
	}
	if strings.TrimSpace(staged) == "" {
		return "", fmt.Errorf("host_repo_mutate_nothing_to_commit")
	}

	if _, err := runReviewedHostGit(runCtx, workspace, "commit", "-m", message); err != nil {
		if runCtx.Err() != nil {
			return "", runCtx.Err()
		}
		return "", err
	}
	digest, err := runReviewedHostGit(runCtx, workspace, "rev-parse", "HEAD")
	digest = strings.TrimSpace(digest)
	if err != nil || digest == "" {
		return "", fmt.Errorf("host_repo_mutate_head_unobserved")
	}
	// The receipt is that HEAD moved, not that the command exited zero.
	if digest == before {
		return "", fmt.Errorf("host_repo_mutate_head_unobserved")
	}
	return "commit " + digest, nil
}
