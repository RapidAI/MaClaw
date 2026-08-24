package agentservice

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostSystemLaunchProviderID     = "core-doclaunch"
	reviewedHostSystemLaunchImplementation = "local"
	reviewedHostSystemLaunchAdapterName    = "host_system_launch_document"
)

type reviewedHostSystemLauncher interface {
	OpenReviewedHostDocument(ctx context.Context, principal Principal, path string) (string, error)
}

type reviewedHostDocumentLauncher interface {
	OpenDocument(ctx context.Context, absPath string) error
}

func reviewedHostSystemLaunchInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func reviewedHostSystemLaunchContractDigest() string {
	return coretool.SchemaDigest([]byte("system.launch.local:v1:host-doclaunch-document-path"))
}

func reviewedHostDocumentLauncherReady(launcher reviewedHostDocumentLauncher) bool {
	if launcher == nil {
		return false
	}
	if ready, ok := launcher.(reviewedHostSpeechReadiness); ok {
		return ready.Ready()
	}
	return true
}

// ProjectReviewedHostSystemLaunchProvider projects the host-owned document
// opener. It is not a Skill/MCP discovery entry and must not import GUI
// open. The closed schema accepts path only. Target, url, app, channel,
// and destination stay out. This is not a send and not app_launch.
func ProjectReviewedHostSystemLaunchProvider(launcher reviewedHostSystemLauncher) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if launcher == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host document launcher is unavailable")
	}
	parameters := reviewedHostSystemLaunchInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host system launch schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostSystemLaunchContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-doclaunch-document-path-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostSystemLaunchAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostSystemLaunchProviderID,
			ImplementationID: reviewedHostSystemLaunchImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilitySystemLaunch,
			Qualifiers: map[string]string{QualifierLaunchKind: LaunchKindDocument},
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostSystemLaunch(launcher)}, nil
}

func AttachReviewedHostSystemLaunchProvider(catalog DynamicSemanticCatalog, launcher reviewedHostSystemLauncher) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostSystemLaunchProvider(launcher)
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

func executeReviewedHostSystemLaunch(launcher reviewedHostSystemLauncher) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if launcher == nil {
			return "", fmt.Errorf("host_system_launch_unavailable")
		}
		path, err := reviewedHostSystemLaunchArgsAllowed(args)
		if err != nil {
			return "", err
		}
		return launcher.OpenReviewedHostDocument(ctx, principal, path)
	}
}

func reviewedHostSystemLaunchArgsAllowed(args map[string]interface{}) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("host_system_launch_arguments_rejected")
	}
	raw, ok := args["path"]
	if !ok {
		return "", fmt.Errorf("host_system_launch_arguments_rejected")
	}
	path, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("host_system_launch_arguments_rejected")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("host_system_launch_path_required")
	}
	return path, nil
}

func (c *coreAgentCallbacks) OpenReviewedHostDocument(ctx context.Context, principal Principal, path string) (string, error) {
	if c == nil || strings.TrimSpace(c.workspace) == "" || !reviewedHostDocumentLauncherReady(c.documentLauncher) {
		return "", fmt.Errorf("host_system_launch_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_system_launch_principal_mismatch")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("host_system_launch_path_required")
	}
	if reviewedHostSystemLaunchPathRejected(path) {
		return "", fmt.Errorf("host_system_launch_path_rejected")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}
	absPath, err := c.resolveWorkspacePath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("host_system_launch_path_missing")
	}
	if info.IsDir() {
		return "", fmt.Errorf("host_system_launch_path_is_directory")
	}
	if _, _, ok := reviewedHostDeliverableDocument(filepath.Base(absPath), ""); !ok {
		return "", fmt.Errorf("host_system_launch_document_required")
	}
	if err := c.documentLauncher.OpenDocument(ctx, absPath); err != nil {
		return "", err
	}
	display := reviewedHostWorkspaceRelative(c.workspace, absPath, path)
	return "Document opened with the system handler (" + display + "). This is not a send.", nil
}

func reviewedHostSystemLaunchPathRejected(path string) bool {
	lower := strings.ToLower(strings.TrimSpace(path))
	return strings.Contains(lower, "://") || strings.HasPrefix(lower, "mailto:")
}

type reviewedHostNativeDocumentLauncher struct{}

func (reviewedHostNativeDocumentLauncher) Ready() bool {
	ok, _ := remote.DetectDisplayServer()
	return ok
}

func (reviewedHostNativeDocumentLauncher) OpenDocument(ctx context.Context, absPath string) error {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	absPath = strings.TrimSpace(absPath)
	if absPath == "" || !filepath.IsAbs(absPath) {
		return fmt.Errorf("host_system_launch_unavailable")
	}
	return reviewedHostStartSystemHandler(absPath)
}

func reviewedHostStartSystemHandler(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("host_system_launch_unavailable")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// WireReviewedHostNativeDocumentLauncher attaches the host-owned document
// opener. Ready() is the plan-time gate: headless hosts stay unpublished.
func WireReviewedHostNativeDocumentLauncher(e *CoreAgentExecutor) {
	if e == nil {
		return
	}
	e.SetReviewedHostDocumentLauncher(reviewedHostNativeDocumentLauncher{})
}

func (e *CoreAgentExecutor) SetReviewedHostDocumentLauncher(launcher reviewedHostDocumentLauncher) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.documentLauncher = launcher
	e.mu.Unlock()
}

func (e *CoreAgentExecutor) getReviewedHostDocumentLauncher() reviewedHostDocumentLauncher {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.documentLauncher
}
