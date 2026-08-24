package httpapi

import (
	"context"
	"errors"
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

// hubCenterServiceGroupIDs translates Hub-only virtual groups before forwarding
// to HubCenter. ve-service is the virtual-employee binding name exposed by Hub;
// it is not a HubCenter billing/routing group. HubCenter's system-free alias
// resolves the request against the tenant's active compute entitlement (redeem
// in this deployment), while leaving the VE-side configuration unchanged.
func hubCenterServiceGroupIDs(serviceGroupIDs []string) []string {
	ids := append([]string(nil), serviceGroupIDs...)
	for i := range ids {
		if strings.EqualFold(strings.TrimSpace(ids[i]), "ve-service") {
			ids[i] = "system-free"
		}
	}
	return ids
}

// ForwardViaMaClaw forwards a request through the MaClaw Official provider (HubCenter proxy).
// Returns (responseBody, statusCode, error).
func ForwardViaMaClaw(ctx context.Context, body []byte, tenantID string, serviceGroupIDs ...string) ([]byte, int, error) {
	result, err := ForwardViaMaClawDetailed(ctx, body, tenantID, serviceGroupIDs...)
	return result.Body, result.StatusCode, err
}

// ForwardViaMaClawDetailed is ForwardViaMaClaw plus HubCenter billing headers.
func ForwardViaMaClawDetailed(ctx context.Context, body []byte, tenantID string, serviceGroupIDs ...string) (llmservice.OfficialForwardResult, error) {
	module := GetMaClawModule()
	if module == nil || module.Client == nil {
		return llmservice.OfficialForwardResult{
			Body:       []byte(`{"error":{"message":"MaClaw official service is not configured"}}`),
			StatusCode: http.StatusServiceUnavailable,
		}, nil
	}
	result, err := module.Client.ForwardDetailed(ctx, body, tenantID, hubCenterServiceGroupIDs(serviceGroupIDs)...)
	if err == nil {
		// The response header carries the authenticated HubCenter base-price
		// snapshot for non-streaming calls. Record the complete fact here rather
		// than only the legacy multiplier, so Hub settles with input/output
		// pricing and applies its service-group multiplier exactly once.
		noteOfficialCreditMultiplierFromHeader(ctx, result.Header)
		// Keep the legacy fields for a HubCenter that has not yet emitted headers.
		noteOfficialBilling(ctx, result.CreditMultiplier, result.ProviderID)
	}
	return result, err
}

// QuoteViaMaClaw asks HubCenter to freeze its provider route and directional
// time-of-use price. The opaque token it returns is valid only for the paired
// request and is never recorded in Hub's billing ledger.
func QuoteViaMaClaw(ctx context.Context, body []byte, tenantID string, serviceGroupIDs ...string) (llmservice.OfficialPricingQuote, error) {
	module := GetMaClawModule()
	if module == nil || module.Client == nil {
		return llmservice.OfficialPricingQuote{}, errors.New("MaClaw official service is not configured")
	}
	return module.Client.Quote(ctx, body, tenantID, hubCenterServiceGroupIDs(serviceGroupIDs)...)
}

// ReconcileMaClawBillingAttempt retrieves a completed official attempt's
// non-sensitive billing snapshot. It is used only for sent reservations whose
// original response could not be observed by Hub.
func ReconcileMaClawBillingAttempt(ctx context.Context, tenantID, requestID string) (llmservice.OfficialBillingAttempt, int, error) {
	module := GetMaClawModule()
	if module == nil || module.Client == nil {
		return llmservice.OfficialBillingAttempt{}, 0, errors.New("MaClaw official service is not configured")
	}
	return module.Client.BillingAttempt(ctx, tenantID, requestID)
}

func ForwardViaMaClawDetailedWithQuote(ctx context.Context, quote llmservice.OfficialPricingQuote, body []byte, tenantID string, serviceGroupIDs ...string) (llmservice.OfficialForwardResult, error) {
	module := GetMaClawModule()
	if module == nil || module.Client == nil {
		return llmservice.OfficialForwardResult{NoUpstreamDispatch: true}, errors.New("MaClaw official service is not configured")
	}
	result, err := module.Client.ForwardDetailedWithQuote(ctx, quote, body, tenantID, hubCenterServiceGroupIDs(serviceGroupIDs)...)
	if err == nil {
		noteOfficialCreditMultiplierFromHeader(ctx, result.Header)
		noteOfficialBilling(ctx, result.CreditMultiplier, result.ProviderID)
	}
	return result, err
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
	resp, err := module.Client.ForwardStream(ctx, body, tenantID, hubCenterServiceGroupIDs(serviceGroupIDs)...)
	if err != nil {
		return resp, err
	}
	return noteOfficialStreamHeaders(ctx, resp), nil
}

// ForwardStreamViaMaClawWithQuote pins streaming calls as well as ordinary
// calls. HubCenter's result trailer remains the final usage fact used for
// settlement; the opaque quote token never leaves this function boundary.
func ForwardStreamViaMaClawWithQuote(ctx context.Context, body []byte, tenantID string, serviceGroupIDs ...string) (*http.Response, error) {
	module := GetMaClawModule()
	if module == nil || module.Client == nil {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"MaClaw official service is not configured"}}`)), Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
	}
	quote, err := module.Client.Quote(ctx, body, tenantID, hubCenterServiceGroupIDs(serviceGroupIDs)...)
	if err != nil {
		if strings.Contains(err.Error(), "quote HTTP 404") {
			return ForwardStreamViaMaClaw(ctx, body, tenantID, serviceGroupIDs...)
		}
		return nil, err
	}
	return ForwardStreamViaMaClawWithExistingQuote(ctx, quote, body, tenantID, serviceGroupIDs...)
}

// ForwardStreamViaMaClawWithExistingQuote forwards using the request-scoped
// quote obtained at Hub admission; it intentionally never creates a second
// quote with a potentially different provider or time-of-use price.
func ForwardStreamViaMaClawWithExistingQuote(ctx context.Context, quote llmservice.OfficialPricingQuote, body []byte, tenantID string, serviceGroupIDs ...string) (*http.Response, error) {
	module := GetMaClawModule()
	if module == nil || module.Client == nil {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"MaClaw official service is not configured"}}`)), Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
	}
	resp, err := module.Client.ForwardStreamWithQuote(ctx, quote, body, tenantID, hubCenterServiceGroupIDs(serviceGroupIDs)...)
	if err != nil {
		return resp, err
	}
	return noteOfficialStreamHeaders(ctx, resp), nil
}

func noteOfficialStreamHeaders(ctx context.Context, resp *http.Response) *http.Response {
	if resp == nil {
		return nil
	}
	noteOfficialCreditMultiplierFromHeader(ctx, resp.Header)
	if resp.Body == nil {
		noteOfficialCreditMultiplierFromHeader(ctx, resp.Trailer)
		return resp
	}
	resp.Body = &officialStreamBody{
		ReadCloser: resp.Body,
		onEOF: func() {
			noteOfficialCreditMultiplierFromHeader(ctx, resp.Trailer)
		},
	}
	return resp
}

type officialStreamBody struct {
	io.ReadCloser
	once  sync.Once
	onEOF func()
}

func (b *officialStreamBody) Read(p []byte) (int, error) {
	if b == nil || b.ReadCloser == nil {
		return 0, io.EOF
	}
	n, err := b.ReadCloser.Read(p)
	if errors.Is(err, io.EOF) {
		b.note()
	}
	return n, err
}

func (b *officialStreamBody) Close() error {
	if b == nil {
		return nil
	}
	var err error
	if b.ReadCloser != nil {
		err = b.ReadCloser.Close()
	}
	b.note()
	return err
}

func (b *officialStreamBody) note() {
	if b == nil || b.onEOF == nil {
		return
	}
	b.once.Do(b.onEOF)
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
