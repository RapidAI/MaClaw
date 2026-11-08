package llmservice

import (
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/corelib/lobsterai"
	"github.com/RapidAI/CodeClaw/corelib/workbuddy"
)

// MemberProbeConfig keeps a vendor account on that vendor's own request
// shape. A static key keeps the provider's saved User-Agent when the vendor
// config did not already choose one. Account logins still replace that header
// in their own ApplyHeaders. TimeoutSec on cfg is kept. The probe does not
// refresh tokens; the live forward path does that before it wraps.
//
// The local /test and a remote stream probe both call this, so the request
// the receipt describes is the one the vendor transport signs.
func MemberProbeConfig(cfg corelib.MaclawLLMConfig, provider *llmpool.ProviderConfig) corelib.MaclawLLMConfig {
	if provider == nil {
		return cfg
	}
	if wb, ok := WorkBuddyMaclawConfig(provider, cfg.Model); ok {
		wb.TimeoutSec = cfg.TimeoutSec
		cfg = wb
	}
	if qd, ok := QoderMaclawConfig(provider, cfg.Model); ok {
		qd.TimeoutSec = cfg.TimeoutSec
		cfg = qd
	}
	if tr, ok := TraeMaclawConfig(provider, cfg.Model); ok {
		tr.TimeoutSec = cfg.TimeoutSec
		cfg = tr
	}
	if lb, ok := LobsterAIMaclawConfig(provider, cfg.Model); ok {
		lb.TimeoutSec = cfg.TimeoutSec
		cfg = lb
	}
	if strings.TrimSpace(cfg.AgentType) == "" {
		cfg.AgentType = RequestAgentType(provider.AgentType)
	}
	// Only a Devin provider gets its name copied. The status check otherwise
	// sees an empty name and misses a provider that is Devin by name alone.
	if strings.TrimSpace(cfg.ProviderName) == "" && IsDevinProvider(provider.Name, firstNonEmpty(provider.APIURL, cfg.URL)) {
		cfg.ProviderName = strings.TrimSpace(provider.Name)
	}
	return WithMemberUpstreamOverrides(cfg, provider)
}

// WrapMemberProbeClient applies the vendor transports in the same order as
// the local probe. A client that matches none of them is returned unchanged.
// The receipt is the body built before this wrap; RoundTrip may still rewrite
// the bytes that leave the process.
func WrapMemberProbeClient(client *http.Client, cfg corelib.MaclawLLMConfig) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	if workbuddy.Matches(cfg) {
		client = workbuddy.WrapClient(client)
	}
	// Qoder signs with the provider's uid, and Trae stamps the device pair the
	// refresh token was minted against. The bare wrappers fall back to a
	// desktop credential store a server process does not have.
	if QoderMaclawConfigFromConfig(cfg) {
		client = QoderHTTPClient(client, cfg)
	}
	if TraeMaclawConfigFromConfig(cfg) {
		client = TraeHTTPClient(client, cfg)
	}
	if lobsterai.Matches(cfg.ProviderName, cfg.URL) {
		client = lobsterai.WrapClient(client)
	}
	return DevinHTTPClient(client, cfg.ProviderName, cfg.URL)
}
