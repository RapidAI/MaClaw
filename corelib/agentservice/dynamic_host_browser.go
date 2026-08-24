package agentservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostBrowserProviderID     = "core-browser"
	reviewedHostBrowserImplementation = "bound-session"
	reviewedHostBrowserAdapterName    = "host_browser_control_web"
	reviewedHostBrowserDefaultTimeout = 30 * time.Second
)

type reviewedHostBrowserController interface {
	ControlReviewedHostBrowser(ctx context.Context, principal Principal, action, url string) (string, error)
}

func reviewedHostBrowserInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"type": "string"},
			"url":    map[string]interface{}{"type": "string"},
		},
		"required":             []string{"action"},
		"additionalProperties": false,
	}
}

func reviewedHostBrowserContractDigest() string {
	return coretool.SchemaDigest([]byte("browser.control.web:v1:host-browser"))
}

// ProjectReviewedHostBrowserProvider projects the host-owned browser action.
// It is not a Skill/MCP discovery entry and must not import GUI browser. The
// closed schema accepts action and optional url. Cookies, headers, and login
// state cannot be injected. No driver means the provider is not attached.
// Timeout or disconnect is unknown.
func ProjectReviewedHostBrowserProvider(controller reviewedHostBrowserController) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if controller == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host browser controller is unavailable")
	}
	parameters := reviewedHostBrowserInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host browser schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostBrowserContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-browser-action-url-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostBrowserAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostBrowserProviderID,
			ImplementationID: reviewedHostBrowserImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityBrowserControl,
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostBrowser(controller)}, nil
}

func AttachReviewedHostBrowserProvider(catalog DynamicSemanticCatalog, controller reviewedHostBrowserController) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostBrowserProvider(controller)
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

func executeReviewedHostBrowser(controller reviewedHostBrowserController) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if controller == nil {
			return "", fmt.Errorf("host_browser_session_unavailable")
		}
		action, url, err := reviewedHostBrowserArgsAllowed(args)
		if err != nil {
			return "", err
		}
		return controller.ControlReviewedHostBrowser(ctx, principal, action, url)
	}
}

func reviewedHostBrowserArgsAllowed(args map[string]interface{}) (action, url string, err error) {
	if len(args) > 2 {
		return "", "", fmt.Errorf("host_browser_arguments_rejected")
	}
	hasAction := false
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", "", fmt.Errorf("host_browser_arguments_rejected")
		}
		switch key {
		case "action":
			action, hasAction = value, true
		case "url":
			url = value
		default:
			return "", "", fmt.Errorf("host_browser_arguments_rejected")
		}
	}
	action = strings.ToLower(strings.TrimSpace(action))
	url = strings.TrimSpace(url)
	if !hasAction {
		return "", "", fmt.Errorf("host_browser_action_required")
	}
	switch action {
	case "navigate", "snapshot":
	default:
		return "", "", fmt.Errorf("host_browser_action_rejected")
	}
	if action == "navigate" && url == "" {
		return "", "", fmt.Errorf("host_browser_url_required")
	}
	if strings.Contains(strings.ToLower(url), "cookie") || strings.Contains(action, "cookie") {
		return "", "", fmt.Errorf("host_browser_cookie_rejected")
	}
	return action, url, nil
}

func reviewedHostBrowserResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("host_browser_delivery_token")
	}
	if strings.Contains(strings.ToLower(text), "set-cookie") {
		return "", fmt.Errorf("host_browser_cookie_rejected")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("host_browser_empty")
	}
	return text, nil
}

func (c *coreAgentCallbacks) ControlReviewedHostBrowser(ctx context.Context, principal Principal, action, url string) (string, error) {
	if c == nil || c.trustedBrowser == nil || c.delegateChild || c.runtimeReadOnlyChild {
		return "", fmt.Errorf("host_browser_session_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_browser_principal_mismatch")
	}
	args := map[string]interface{}{"action": action}
	if strings.TrimSpace(url) != "" {
		args["url"] = url
	}
	action, url, err := reviewedHostBrowserArgsAllowed(args)
	if err != nil {
		return "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, reviewedHostBrowserDefaultTimeout)
	defer cancel()
	out, err := c.trustedBrowser(runCtx, principal, action, url)
	if runCtx.Err() != nil {
		return "", fmt.Errorf("host_browser_timeout")
	}
	if err != nil {
		code := strings.TrimSpace(err.Error())
		if dynamicHostObservedExternalUnknown(code) {
			return "", err
		}
		lower := strings.ToLower(code)
		// A driver that already separated "dispatched, answer lost" from
		// "never dispatched" must not have that distinction discarded here.
		// None of the rungs below match an unobserved name, so without this
		// the code would fall through and be reported as a definite failure --
		// the one verdict that invites repeating an effect that may hold.
		if strings.Contains(lower, "unobserved") {
			return "", fmt.Errorf("host_browser_outcome_unobserved")
		}
		if strings.Contains(lower, "timeout") {
			return "", fmt.Errorf("host_browser_timeout")
		}
		if strings.Contains(lower, "disconnect") {
			return "", fmt.Errorf("host_browser_session_disconnected")
		}
		if strings.Contains(lower, "unavailable") {
			return "", fmt.Errorf("host_browser_session_unavailable")
		}
		return "", err
	}
	return reviewedHostBrowserResultProjection(out)
}
