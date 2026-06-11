package llm

import "strings"

type responsesEndpointKind string

const (
	responsesEndpointOpenAI responsesEndpointKind = "openai"
	responsesEndpointCodex  responsesEndpointKind = "codex_subscription"
)

func classifyResponsesEndpointKind(rawURL string) responsesEndpointKind {
	if strings.Contains(strings.ToLower(strings.TrimSpace(rawURL)), "chatgpt.com") {
		return responsesEndpointCodex
	}
	return responsesEndpointOpenAI
}

// IsCodexSubscriptionEndpoint reports whether the endpoint needs Codex
// subscription-specific Responses API paths and headers.
func IsCodexSubscriptionEndpoint(rawURL string) bool {
	return classifyResponsesEndpointKind(rawURL) == responsesEndpointCodex
}

// BuildResponsesEndpoint appends the correct Responses API path for the
// configured endpoint kind.
func BuildResponsesEndpoint(rawURL string) string {
	endpoint := strings.TrimRight(rawURL, "/")
	if IsCodexSubscriptionEndpoint(endpoint) {
		if !strings.HasSuffix(endpoint, "/codex/responses") {
			endpoint = strings.TrimSuffix(endpoint, "/codex")
			endpoint += "/codex/responses"
		}
		return endpoint
	}
	if strings.HasSuffix(endpoint, "/responses") {
		return endpoint
	}
	if strings.HasSuffix(endpoint, "/v1") {
		return endpoint + "/responses"
	}
	return endpoint + "/v1/responses"
}
