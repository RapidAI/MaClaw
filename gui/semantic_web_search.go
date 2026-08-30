package main

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

const (
	semanticTrustedWebSearchAdapter        = "semantic_search_trusted_web"
	semanticTrustedWebSearchImplementation = "trusted-web-search-v1"
	semanticTrustedWebSearchCapability     = tool.CapabilityID("information.search.web")
	semanticTrustedWebSearchMaxResults     = 8
)

func semanticUnpublishedLegacyWebSearchProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == semanticTrustedWebSearchCapability {
			return true
		}
	}
	return false
}

func semanticTrustedWebSearchDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedWebSearchAdapter,
			"description": "Search public web information. Only query is accepted.",
			"parameters":  semanticTrustedWebSearchInvocationSchema(),
		},
	}
}

func semanticTrustedWebSearchInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	}
}

func semanticTrustedWebSearchArgsAllowed(args map[string]interface{}) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("trusted_web_search_arguments_rejected")
	}
	raw, ok := args["query"]
	if !ok {
		return "", fmt.Errorf("trusted_web_search_arguments_rejected")
	}
	query, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("trusted_web_search_arguments_rejected")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("trusted_web_search_query_required")
	}
	return query, nil
}

func (h *IMMessageHandler) searchTrustedWeb(principalID, query string, publicNetworkOnly bool) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_web_search_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_web_search_principal_required")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("trusted_web_search_query_required")
	}
	if h.semanticTrustedWebSearch != nil {
		return h.semanticTrustedWebSearch(principalID, query)
	}
	ctx, cancel := h.imToolContext()
	defer cancel()
	if publicNetworkOnly {
		ctx = websearch.WithPublicNetworkOnly(ctx)
	}
	response, err := websearch.SearchWithStrategyCtx(ctx, query, semanticTrustedWebSearchMaxResults, h.getWebSearchStrategy())
	if err != nil {
		return "", err
	}
	return semanticTrustedWebSearchProjection(query, response), nil
}

func semanticTrustedWebSearchProjection(query string, response websearch.SearchResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Public web results for %q (%d):\n\n", query, len(response.Results))
	if response.Degraded {
		b.WriteString("Some search engines were unavailable; results include a browser fallback. If the target site is missing, open it with the browser tool.\n\n")
	}
	for i, result := range response.Results {
		fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, strings.TrimSpace(result.Title), strings.TrimSpace(result.URL))
		if snippet := strings.TrimSpace(result.Snippet); snippet != "" {
			fmt.Fprintf(&b, "   %s\n", snippet)
		}
		b.WriteByte('\n')
	}
	if len(response.Results) == 0 {
		b.WriteString("No public web results.\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func semanticTrustedWebSearchResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_web_search_delivery_token")
	}
	// No tool-name token scan here either: see
	// semanticTrustedWebFetchResultProjection. Search results legitimately
	// discuss tools by name; the router, not a literal scan, is the boundary.
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_web_search_empty")
	}
	return text, nil
}
