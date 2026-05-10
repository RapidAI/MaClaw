package agentservice

import "strings"

type providerEndpointKind string

const (
	providerEndpointUnknown providerEndpointKind = ""
	providerEndpointChatGPT providerEndpointKind = "chatgpt"
)

func classifyProviderEndpointKind(rawURL string) providerEndpointKind {
	if strings.Contains(strings.ToLower(strings.TrimSpace(rawURL)), "chatgpt.com") {
		return providerEndpointChatGPT
	}
	return providerEndpointUnknown
}

func (k providerEndpointKind) PrefersOAuthToken() bool {
	return k == providerEndpointChatGPT
}
