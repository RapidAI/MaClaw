package agentservice

import (
	"context"
	"fmt"
	"strings"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostWebSearchProviderID     = "core-websearch"
	reviewedHostWebSearchImplementation = "local"
	reviewedHostWebSearchAdapterName    = "host_information_search_web"
)

func reviewedHostWebSearchInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	}
}

func reviewedHostWebSearchContractDigest() string {
	return coretool.SchemaDigest([]byte("information.search.web:v1:host-websearch"))
}

// ProjectReviewedHostWebSearchProvider projects the host-owned web search.
// It is not a Skill/MCP discovery entry and must not import the GUI
// web_search catalog. The closed schema accepts only query; max_results,
// provider, engine, url, channel, and destination are rejected.
func ProjectReviewedHostWebSearchProvider(searcher reviewedHostWebSearcher) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if searcher == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host web search is unavailable")
	}
	parameters := reviewedHostWebSearchInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host web search schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostWebSearchContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-websearch-query-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostWebSearchAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostWebSearchProviderID,
			ImplementationID: reviewedHostWebSearchImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{
			{Capability: CapabilityInformationSearchWeb, Qualifiers: map[string]string{QualifierSearchFreshness: SearchFreshnessReference}, Quality: 1},
			{Capability: CapabilityInformationSearchWeb, Qualifiers: map[string]string{QualifierSearchFreshness: SearchFreshnessCurrent}, Quality: 1},
		},
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostWebSearch(searcher)}, nil
}

func AttachReviewedHostWebSearchProvider(catalog DynamicSemanticCatalog, searcher reviewedHostWebSearcher) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostWebSearchProvider(searcher)
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

func executeReviewedHostWebSearch(searcher reviewedHostWebSearcher) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if searcher == nil {
			return "", fmt.Errorf("host_web_search_unavailable")
		}
		if len(args) != 1 {
			return "", fmt.Errorf("host_web_search_arguments_rejected")
		}
		query, ok := args["query"].(string)
		if !ok {
			return "", fmt.Errorf("host_web_search_arguments_rejected")
		}
		query = strings.TrimSpace(query)
		if query == "" {
			return "", fmt.Errorf("host_web_search_query_required")
		}
		return searcher.SearchReviewedHostWeb(ctx, principal, query)
	}
}

func (c *coreAgentCallbacks) SearchReviewedHostWeb(ctx context.Context, principal Principal, query string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("host_web_search_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_web_search_principal_mismatch")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("host_web_search_query_required")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}
	return c.executeWebSearch(map[string]interface{}{"query": query}), nil
}
