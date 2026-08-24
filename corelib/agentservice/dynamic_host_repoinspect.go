package agentservice

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostRepoInspectProviderID     = "core-repoinspect"
	reviewedHostRepoInspectImplementation = "local"
	reviewedHostRepoInspectAdapterName    = "host_repo_inspect_vcs"
	reviewedHostRepoInspectTimeout        = 10 * time.Second
	reviewedHostRepoInspectMaxBytes       = 4000
)

func reviewedHostRepoInspectInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func reviewedHostRepoInspectContractDigest() string {
	return coretool.SchemaDigest([]byte("repo.inspect.vcs:v1:host-repoinspect"))
}

// ProjectReviewedHostRepoInspectProvider projects the host-owned workspace
// git inspect. It is not a Skill/MCP discovery entry and must not import GUI
// git_status / git_diff / git_commit. The closed schema is empty: status and
// diffs are always read together, and the workspace is the only repository.
func ProjectReviewedHostRepoInspectProvider(inspector reviewedHostRepoInspector) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if inspector == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host repo inspector is unavailable")
	}
	parameters := reviewedHostRepoInspectInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host repo inspect schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostRepoInspectContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-repoinspect-empty-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostRepoInspectAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostRepoInspectProviderID,
			ImplementationID: reviewedHostRepoInspectImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityRepoInspect,
			Quality:    1,
		}},
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostRepoInspect(inspector)}, nil
}

func AttachReviewedHostRepoInspectProvider(catalog DynamicSemanticCatalog, inspector reviewedHostRepoInspector) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostRepoInspectProvider(inspector)
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

func executeReviewedHostRepoInspect(inspector reviewedHostRepoInspector) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if inspector == nil {
			return "", fmt.Errorf("host_repo_inspect_unavailable")
		}
		if len(args) != 0 {
			return "", fmt.Errorf("host_repo_inspect_arguments_rejected")
		}
		return inspector.InspectReviewedHostRepo(ctx, principal)
	}
}

func (c *coreAgentCallbacks) InspectReviewedHostRepo(ctx context.Context, principal Principal) (string, error) {
	if c == nil || strings.TrimSpace(c.workspace) == "" {
		return "", fmt.Errorf("host_repo_inspect_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_repo_inspect_principal_mismatch")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}
	return inspectReviewedHostRepo(ctx, c.workspace)
}

func inspectReviewedHostRepo(ctx context.Context, workspace string) (string, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "", fmt.Errorf("host_repo_inspect_unavailable")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "git is not available on this host.", nil
	}
	inside, err := runReviewedHostGit(ctx, workspace, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(inside) != "true" {
		return "The workspace is not a git repository.", nil
	}
	status, err := runReviewedHostGit(ctx, workspace, "status", "--porcelain=v1", "-b", "--untracked-files=all")
	if err != nil {
		return "", fmt.Errorf("git status: %w", err)
	}
	unstaged, err := runReviewedHostGit(ctx, workspace, "diff", "--stat")
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}
	staged, err := runReviewedHostGit(ctx, workspace, "diff", "--cached", "--stat")
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
	return truncateReviewedHostRepoInspect(b.String()), nil
}

func runReviewedHostGit(ctx context.Context, workspace string, args ...string) (string, error) {
	return runReviewedHostGitWithin(ctx, workspace, reviewedHostRepoInspectTimeout, reviewedHostGitEnv(), args...)
}

// runReviewedHostGitWithin is the shared runner behind every reviewed git call.
// Read-only inspection and a network-facing push need the same environment
// hygiene and stderr handling but very different time bounds, so the bound and
// the environment are parameters rather than constants.
func runReviewedHostGitWithin(ctx context.Context, workspace string, timeout time.Duration, env []string, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workspace
	cmd.Env = env
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

func reviewedHostGitEnv() []string {
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

func truncateReviewedHostRepoInspect(text string) string {
	if len(text) <= reviewedHostRepoInspectMaxBytes {
		return text
	}
	return text[:reviewedHostRepoInspectMaxBytes] + "\n...(truncated)"
}
