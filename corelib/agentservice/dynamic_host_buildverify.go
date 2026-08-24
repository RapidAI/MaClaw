package agentservice

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostBuildVerifyProviderID     = "core-buildverify"
	reviewedHostBuildVerifyImplementation = "local"
	reviewedHostBuildVerifyAdapterName    = "host_build_verify_local"
	reviewedHostBuildVerifyTimeout        = 10 * time.Minute
)

type reviewedHostBuildVerifier interface {
	RunReviewedHostBuildVerify(ctx context.Context, principal Principal, task, target string) (string, error)
}

func reviewedHostBuildVerifyInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"task": map[string]interface{}{"type": "string", "enum": coretool.BuildVerifyTasks()},
			// A subdirectory is the one narrowing that means the same thing for
			// every project kind. Per-tool package selectors would each need
			// their own syntax, and that syntax is where a command line creeps
			// back into the model's hands.
			"target": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"task"},
		"additionalProperties": false,
	}
}

func reviewedHostBuildVerifyContractDigest() string {
	return coretool.SchemaDigest([]byte("build.verify.local:v1:host-buildverify"))
}

// ProjectReviewedHostBuildVerifyProvider projects the host-owned verification
// task. The closed schema accepts a reviewed task name and an optional
// workspace subdirectory; command, args, env, timeout_seconds, project_path
// and working_dir are rejected, because the host owns the command line. That
// is the whole reason this provider exists next to the shell one: a plan can
// grant build/test/lint without granting the arbitrary local execution that
// would carry file and repository mutation along with it. The host process
// waits for exit, so the handler result is the local completion receipt.
func ProjectReviewedHostBuildVerifyProvider(verifier reviewedHostBuildVerifier) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if verifier == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host build verifier is unavailable")
	}
	parameters := reviewedHostBuildVerifyInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host build verify schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostBuildVerifyContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-buildverify-task-target-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostBuildVerifyAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostBuildVerifyProviderID,
			ImplementationID: reviewedHostBuildVerifyImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityBuildVerify,
			Quality:    1,
		}},
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostBuildVerify(verifier)}, nil
}

func AttachReviewedHostBuildVerifyProvider(catalog DynamicSemanticCatalog, verifier reviewedHostBuildVerifier) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostBuildVerifyProvider(verifier)
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

func executeReviewedHostBuildVerify(verifier reviewedHostBuildVerifier) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if verifier == nil {
			return "", fmt.Errorf("host_build_verify_unavailable")
		}
		task, target, err := reviewedHostBuildVerifyArgsAllowed(args)
		if err != nil {
			return "", err
		}
		return verifier.RunReviewedHostBuildVerify(ctx, principal, task, target)
	}
}

func reviewedHostBuildVerifyArgsAllowed(args map[string]interface{}) (task, target string, err error) {
	if len(args) > 2 {
		return "", "", fmt.Errorf("host_build_verify_arguments_rejected")
	}
	hasTask := false
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", "", fmt.Errorf("host_build_verify_arguments_rejected")
		}
		switch key {
		case "task":
			task, hasTask = strings.TrimSpace(value), true
		case "target":
			target = strings.TrimSpace(value)
		default:
			return "", "", fmt.Errorf("host_build_verify_arguments_rejected")
		}
	}
	if !hasTask {
		return "", "", fmt.Errorf("host_build_verify_task_required")
	}
	// Re-checked here rather than trusted from the schema. A boundary that
	// exists only in a document the model is shown is not a boundary.
	if !coretool.BuildVerifyTaskAllowed(task) {
		return "", "", fmt.Errorf("host_build_verify_task_rejected")
	}
	return task, target, nil
}

func (c *coreAgentCallbacks) RunReviewedHostBuildVerify(ctx context.Context, principal Principal, task, target string) (string, error) {
	// Deliberately not gated on canUseLocalBash. An instance that withholds
	// the shell capability is exactly the case this provider exists to serve;
	// reusing the shell gate here would make the narrowed grant unobtainable
	// wherever it matters most.
	if c == nil || strings.TrimSpace(c.workspace) == "" {
		return "", fmt.Errorf("host_build_verify_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_build_verify_principal_mismatch")
	}
	if !coretool.BuildVerifyTaskAllowed(task) {
		return "", fmt.Errorf("host_build_verify_task_rejected")
	}
	runDir, escaped, notDir := coretool.BuildVerifyWorkspaceSubdir(c.workspace, target)
	if escaped {
		return "", fmt.Errorf("host_build_verify_target_rejected")
	}
	if notDir {
		return "", fmt.Errorf("host_build_verify_target_not_a_directory")
	}
	kind, ok := coretool.BuildVerifyProjectKind(c.workspace, runDir)
	if !ok {
		return "", fmt.Errorf("host_build_verify_project_unrecognised")
	}
	argv, ok := coretool.BuildVerifyCommand(kind, task)
	if !ok {
		return "", fmt.Errorf("host_build_verify_task_unsupported")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, reviewedHostBuildVerifyTimeout)
	defer cancel()
	// Executed directly, never through a shell. There is no command string for
	// anything to be injected into, so the reviewed argv table is the complete
	// set of programs this capability can start.
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Dir = runDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	if runCtx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("host_build_verify_timeout")
	}
	out := coretool.BuildVerifyProjection(stdout.String(), stderr.String())
	// A failing build or test answers the question that was asked. Reporting
	// it as an adapter error would hide the diagnostics that are the point.
	if runErr != nil {
		return strings.TrimSpace(out + "\n" + runErr.Error()), nil
	}
	if strings.TrimSpace(out) == "" {
		return task + " passed", nil
	}
	return out, nil
}
