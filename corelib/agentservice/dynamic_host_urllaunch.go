package agentservice

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostURLLaunchProviderID     = "core-urllaunch"
	reviewedHostURLLaunchImplementation = "local"
	reviewedHostURLLaunchAdapterName    = "host_system_launch_url"
)

type reviewedHostURLLauncher interface {
	OpenReviewedHostURL(ctx context.Context, principal Principal, rawURL string) (string, error)
}

type reviewedHostURLOpener interface {
	OpenURL(ctx context.Context, rawURL string) error
}

func reviewedHostURLLaunchInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"url"},
		"additionalProperties": false,
	}
}

func reviewedHostURLLaunchContractDigest() string {
	return coretool.SchemaDigest([]byte("system.launch.local:v1:host-urllaunch-https"))
}

func reviewedHostURLLauncherReady(opener reviewedHostURLOpener) bool {
	if opener == nil {
		return false
	}
	if ready, ok := opener.(reviewedHostSpeechReadiness); ok {
		return ready.Ready()
	}
	return true
}

// ProjectReviewedHostURLLaunchProvider projects the host-owned public URL
// opener. It is not a Skill/MCP discovery entry and must not import GUI
// open. The closed schema accepts url only. Path, target, app, channel,
// and destination stay out. Applications and folders stay unpublished.
// This is not a send and not browser.control.web.
func ProjectReviewedHostURLLaunchProvider(launcher reviewedHostURLLauncher) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if launcher == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host url launcher is unavailable")
	}
	parameters := reviewedHostURLLaunchInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host url launch schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostURLLaunchContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-urllaunch-https-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostURLLaunchAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostURLLaunchProviderID,
			ImplementationID: reviewedHostURLLaunchImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilitySystemLaunch,
			Qualifiers: map[string]string{QualifierLaunchKind: LaunchKindURL},
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostURLLaunch(launcher)}, nil
}

func AttachReviewedHostURLLaunchProvider(catalog DynamicSemanticCatalog, launcher reviewedHostURLLauncher) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostURLLaunchProvider(launcher)
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

func executeReviewedHostURLLaunch(launcher reviewedHostURLLauncher) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if launcher == nil {
			return "", fmt.Errorf("host_url_launch_unavailable")
		}
		rawURL, err := reviewedHostURLLaunchArgsAllowed(args)
		if err != nil {
			return "", err
		}
		return launcher.OpenReviewedHostURL(ctx, principal, rawURL)
	}
}

func reviewedHostURLLaunchArgsAllowed(args map[string]interface{}) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("host_url_launch_arguments_rejected")
	}
	raw, ok := args["url"]
	if !ok {
		return "", fmt.Errorf("host_url_launch_arguments_rejected")
	}
	rawURL, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("host_url_launch_arguments_rejected")
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("host_url_launch_url_required")
	}
	if _, err := reviewedHostPublicLaunchURL(rawURL); err != nil {
		return "", err
	}
	return rawURL, nil
}

func reviewedHostPublicLaunchURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed == nil {
		return "", fmt.Errorf("host_url_launch_url_rejected")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("host_url_launch_url_rejected")
	}
	if parsed.User != nil || strings.TrimSpace(parsed.Host) == "" {
		return "", fmt.Errorf("host_url_launch_url_rejected")
	}
	if parsed.Opaque != "" {
		return "", fmt.Errorf("host_url_launch_url_rejected")
	}
	return parsed.String(), nil
}

func (c *coreAgentCallbacks) OpenReviewedHostURL(ctx context.Context, principal Principal, rawURL string) (string, error) {
	if c == nil || !reviewedHostURLLauncherReady(c.urlLauncher) {
		return "", fmt.Errorf("host_url_launch_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_url_launch_principal_mismatch")
	}
	canonical, err := reviewedHostPublicLaunchURL(rawURL)
	if err != nil {
		return "", err
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}
	if err := c.urlLauncher.OpenURL(ctx, canonical); err != nil {
		return "", err
	}
	return "URL opened with the system handler. This is not a send.", nil
}

type reviewedHostNativeURLLauncher struct{}

func (reviewedHostNativeURLLauncher) Ready() bool {
	ok, _ := remote.DetectDisplayServer()
	return ok
}

func (reviewedHostNativeURLLauncher) OpenURL(ctx context.Context, rawURL string) error {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	canonical, err := reviewedHostPublicLaunchURL(rawURL)
	if err != nil {
		return err
	}
	return reviewedHostStartSystemHandler(canonical)
}

// WireReviewedHostNativeURLLauncher attaches the host-owned public URL
// opener. Ready() is the plan-time gate: headless hosts stay unpublished.
func WireReviewedHostNativeURLLauncher(e *CoreAgentExecutor) {
	if e == nil {
		return
	}
	e.SetReviewedHostURLLauncher(reviewedHostNativeURLLauncher{})
}

func (e *CoreAgentExecutor) SetReviewedHostURLLauncher(opener reviewedHostURLOpener) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.urlLauncher = opener
	e.mu.Unlock()
}

func (e *CoreAgentExecutor) getReviewedHostURLLauncher() reviewedHostURLOpener {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.urlLauncher
}
