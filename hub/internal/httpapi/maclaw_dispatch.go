package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

// maclawModule is set by the application layer during initialization.
// When set, the LLM endpoint handler checks if a request should be
// forwarded via the MaClaw Official provider.
var (
	maclawModuleMu sync.RWMutex
	maclawModule   *llmservice.MaClawModule
)

// SetMaClawModule sets the MaClaw integration module for the HTTP handlers.
func SetMaClawModule(m *llmservice.MaClawModule) {
	maclawModuleMu.Lock()
	defer maclawModuleMu.Unlock()
	maclawModule = m
}

// GetMaClawModule returns the current MaClaw integration module.
func GetMaClawModule() *llmservice.MaClawModule {
	maclawModuleMu.RLock()
	defer maclawModuleMu.RUnlock()
	return maclawModule
}

// IsMaClawProviderRequest returns true if the resolved provider ID is maclaw_official.
func IsMaClawProviderRequest(providerID string) bool {
	return strings.TrimSpace(strings.ToLower(providerID)) == llmservice.MaClawOfficialProviderID
}

// ForwardViaMaClaw forwards a request through the MaClaw Official provider (HubCenter proxy).
// Returns (responseBody, statusCode, error).
func ForwardViaMaClaw(ctx context.Context, body []byte, tenantID string, serviceGroupIDs ...string) ([]byte, int, error) {
	module := GetMaClawModule()
	if module == nil || module.Client == nil {
		return []byte(`{"error":{"message":"MaClaw official service is not configured"}}`), http.StatusServiceUnavailable, nil
	}
	return module.Client.Forward(ctx, body, tenantID, serviceGroupIDs...)
}

// ForwardStreamViaMaClaw forwards a streaming request through the MaClaw Official provider.
// The caller must close the returned response body.
func ForwardStreamViaMaClaw(ctx context.Context, body []byte, tenantID string, serviceGroupIDs ...string) (*http.Response, error) {
	module := GetMaClawModule()
	if module == nil || module.Client == nil {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"MaClaw official service is not configured"}}`)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	}
	return module.Client.ForwardStream(ctx, body, tenantID, serviceGroupIDs...)
}

// GetMaClawAccessControl returns the access control instance for permission checks.
func GetMaClawAccessControl() *llmservice.TenantLLMAccessControl {
	module := GetMaClawModule()
	if module == nil {
		return nil
	}
	return module.AccessCtrl
}

func currentMaClawAccessControl(fallback *llmservice.TenantLLMAccessControl) *llmservice.TenantLLMAccessControl {
	if fallback != nil {
		return fallback
	}
	return GetMaClawAccessControl()
}

// applyMaClawUpstreamTimeout updates the MaClaw Official provider client timeout
// from the registry's global UpstreamTimeoutSec value. Called after admin saves settings.
func applyMaClawUpstreamTimeout(reg *im.LLMProviderRegistry) {
	if reg == nil || reg.UpstreamTimeoutSec <= 0 {
		return
	}
	module := GetMaClawModule()
	if module == nil || module.Client == nil {
		return
	}
	module.Client.UpdateTimeout(reg.UpstreamTimeoutSec)
}
