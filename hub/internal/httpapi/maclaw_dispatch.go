package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

// maclawModule is set by the application layer during initialization.
// When set, the LLM endpoint handler checks if a request should be
// forwarded via the MaClaw Official provider.
var maclawModule *llmservice.MaClawModule

// SetMaClawModule sets the MaClaw integration module for the HTTP handlers.
func SetMaClawModule(m *llmservice.MaClawModule) {
	maclawModule = m
}

// IsMaClawProviderRequest returns true if the resolved provider ID is maclaw_official.
func IsMaClawProviderRequest(providerID string) bool {
	return strings.TrimSpace(strings.ToLower(providerID)) == llmservice.MaClawOfficialProviderID
}

// ForwardViaMaClaw forwards a request through the MaClaw Official provider (HubCenter proxy).
// Returns (responseBody, statusCode, error).
func ForwardViaMaClaw(ctx context.Context, body []byte, tenantID string) ([]byte, int, error) {
	if maclawModule == nil || maclawModule.Client == nil {
		return nil, http.StatusServiceUnavailable, nil
	}
	return maclawModule.Client.Forward(ctx, body, tenantID)
}

// GetMaClawAccessControl returns the access control instance for permission checks.
func GetMaClawAccessControl() *llmservice.TenantLLMAccessControl {
	if maclawModule == nil {
		return nil
	}
	return maclawModule.AccessCtrl
}
