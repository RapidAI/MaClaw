package agentservice

import (
	"context"
	"fmt"
	"strings"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostWebFetchProviderID     = "core-webfetch"
	reviewedHostWebFetchImplementation = "local"
	reviewedHostWebFetchAdapterName    = "host_information_fetch_web"
)

func reviewedHostWebFetchInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"url"},
		"additionalProperties": false,
	}
}

func reviewedHostWebFetchContractDigest() string {
	return coretool.SchemaDigest([]byte("information.fetch.web:v1:host-webfetch"))
}

// ProjectReviewedHostWebFetchProvider projects the host-owned single-URL
// fetch. It is not a Skill/MCP discovery entry and must not import the GUI
// web_fetch catalog or artifact.acquire.remote. The closed schema accepts
// only url; save_path, channel, and destination are rejected.
func ProjectReviewedHostWebFetchProvider(fetcher reviewedHostWebFetcher) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if fetcher == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host web fetch is unavailable")
	}
	parameters := reviewedHostWebFetchInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host web fetch schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostWebFetchContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-webfetch-url-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostWebFetchAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostWebFetchProviderID,
			ImplementationID: reviewedHostWebFetchImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityWebFetch,
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostWebFetch(fetcher)}, nil
}

func AttachReviewedHostWebFetchProvider(catalog DynamicSemanticCatalog, fetcher reviewedHostWebFetcher) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostWebFetchProvider(fetcher)
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

func executeReviewedHostWebFetch(fetcher reviewedHostWebFetcher) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if fetcher == nil {
			return "", fmt.Errorf("host_web_fetch_unavailable")
		}
		if len(args) != 1 {
			return "", fmt.Errorf("host_web_fetch_arguments_rejected")
		}
		rawURL, ok := args["url"].(string)
		if !ok {
			return "", fmt.Errorf("host_web_fetch_arguments_rejected")
		}
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			return "", fmt.Errorf("host_web_fetch_url_required")
		}
		return fetcher.FetchReviewedHostWeb(ctx, principal, rawURL)
	}
}

func (c *coreAgentCallbacks) FetchReviewedHostWeb(ctx context.Context, principal Principal, rawURL string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("host_web_fetch_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_web_fetch_principal_mismatch")
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("host_web_fetch_url_required")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}
	return c.executeWebFetch(map[string]interface{}{"url": rawURL}), nil
}
