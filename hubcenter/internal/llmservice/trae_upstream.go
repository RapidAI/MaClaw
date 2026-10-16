package llmservice

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/corelib/trae"
)

// TraeMaclawConfig builds the account context the Trae upstream requires.
// The bool is false for every other provider.
func TraeMaclawConfig(provider *llmpool.ProviderConfig, model string) (corelib.MaclawLLMConfig, bool) {
	return traeMaclawConfig(provider, model)
}

// TraeMaclawConfigFromConfig reports whether a caller that already built a
// MaclawLLMConfig must wrap its client with the SOLO translation transport.
func TraeMaclawConfigFromConfig(cfg corelib.MaclawLLMConfig) bool {
	return trae.Matches(cfg.ProviderName, cfg.URL)
}

func traeMaclawConfig(provider *llmpool.ProviderConfig, model string) (corelib.MaclawLLMConfig, bool) {
	profile, ok := traeProfile(provider)
	if !ok {
		return corelib.MaclawLLMConfig{}, false
	}
	model = strings.TrimSpace(model)
	if model == "" && len(provider.Models) == 1 {
		model = strings.TrimSpace(provider.Models[0])
	}
	cfg := corelib.MaclawLLMConfig{
		URL:            trae.CanonicalChatURL(profile, profile.ChatHost),
		Key:            strings.TrimSpace(provider.APIKey),
		Model:          model,
		Protocol:       "openai",
		ProviderName:   profile.Name,
		TimeoutSec:     provider.UpstreamTimeoutSec,
		TraeMachineID:  strings.TrimSpace(provider.TraeMachineID),
		TraeDeviceID:   strings.TrimSpace(provider.TraeDeviceID),
		TraeUserID:     strings.TrimSpace(provider.TraeUserID),
		TraeRefreshToken: strings.TrimSpace(provider.TraeRefreshToken),
	}
	cfg.AuthType = "oauth"
	return cfg, true
}

// traeProfile selects the realm whose SOLO chat this provider must use. An
// account login is recognized by auth_kind; a static key is only routed by the
// chat host, which is unique per realm.
func traeProfile(provider *llmpool.ProviderConfig) (trae.Profile, bool) {
	if provider == nil {
		return trae.Profile{}, false
	}
	if strings.TrimSpace(provider.AuthKind) == llmpool.ProviderAuthTrae {
		if profile, ok := trae.ProfileByEdition(provider.TraeEdition); ok {
			return profile, true
		}
		if profile, ok := trae.ProfileByName(provider.Name); ok {
			return profile, true
		}
		return trae.Profile{}, false
	}
	return trae.ProfileByChatHost(provider.APIURL)
}

func traeCredentialOf(provider *llmpool.ProviderConfig) traeCredential {
	if provider == nil {
		return traeCredential{}
	}
	return traeCredential{
		AccessToken:  strings.TrimSpace(provider.APIKey),
		RefreshToken: strings.TrimSpace(provider.TraeRefreshToken),
		ExpiresAt:    provider.TraeExpiresAt,
		UserID:       strings.TrimSpace(provider.TraeUserID),
		MachineID:    strings.TrimSpace(provider.TraeMachineID),
		DeviceID:     strings.TrimSpace(provider.TraeDeviceID),
	}
}

func traeCredentialNeedsRefresh(cred traeCredential, now time.Time) bool {
	if cred.ExpiresAt <= 0 {
		return false
	}
	return cred.ExpiresAt-300 <= now.Unix()
}

func refreshTraeProvider(ctx context.Context, provider *llmpool.ProviderConfig) *llmpool.ProviderConfig {
	if provider == nil || strings.TrimSpace(provider.AuthKind) != llmpool.ProviderAuthTrae {
		return provider
	}
	cred := traeCredentialOf(provider)
	if !traeCredentialNeedsRefresh(cred, time.Now()) {
		return provider
	}
	guard := traeRefreshGuard(provider.ID)
	guard.Lock()
	defer guard.Unlock()
	current := provider
	if latest := loadTraeProvider(ctx, provider.ID); latest != nil && latest.AuthKind == llmpool.ProviderAuthTrae {
		current = latest
	}
	cred = traeCredentialOf(current)
	if !traeCredentialNeedsRefresh(cred, time.Now()) {
		return current
	}
	profile, ok := traeProfile(current)
	if !ok {
		return current
	}
	token, err := trae.Refresh(ctx, profile, cred.RefreshToken, cred.MachineID, cred.DeviceID)
	if err != nil {
		log.Printf("[llm] trae refresh provider=%s: %v", current.ID, err)
		return current
	}
	next := *current
	next.APIKey = token.AccessToken
	next.TraeRefreshToken = token.RefreshToken
	next.TraeExpiresAt = token.ExpiresAt
	if strings.TrimSpace(token.MachineID) != "" {
		next.TraeMachineID = token.MachineID
	}
	if strings.TrimSpace(token.DeviceID) != "" {
		next.TraeDeviceID = token.DeviceID
	}
	if strings.TrimSpace(token.UserID) != "" {
		next.TraeUserID = token.UserID
	}
	if err := saveTraeCredential(ctx, next.ID, traeCredential{
		AccessToken:  next.APIKey,
		RefreshToken: next.TraeRefreshToken,
		ExpiresAt:    next.TraeExpiresAt,
		UserID:       next.TraeUserID,
		MachineID:    next.TraeMachineID,
		DeviceID:     next.TraeDeviceID,
	}); err != nil {
		log.Printf("[llm] trae persist provider=%s: %v", next.ID, err)
	}
	return &next
}

func traeRefreshGuard(id string) *sync.Mutex {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		id = "-"
	}
	guard, _ := traeRefreshGuards.LoadOrStore(id, &sync.Mutex{})
	return guard.(*sync.Mutex)
}

func loadTraeProvider(ctx context.Context, id string) *llmpool.ProviderConfig {
	traePersistMu.RLock()
	load := traePersistLoad
	traePersistMu.RUnlock()
	if load == nil {
		return nil
	}
	got, err := load(ctx, id)
	if err != nil || got == nil {
		return nil
	}
	return got
}

func saveTraeCredential(ctx context.Context, id string, cred traeCredential) error {
	traePersistMu.RLock()
	save := traePersistSave
	traePersistMu.RUnlock()
	if save == nil {
		return nil
	}
	return save(ctx, id, cred)
}

func (s *Service) saveTraeCredential(ctx context.Context, id string, cred traeCredential) error {
	if s == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" || strings.TrimSpace(cred.AccessToken) == "" {
		return nil
	}
	defer s.lockRegistryWrite()()
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return err
	}
	idx := providerIndex(reg, id)
	if idx < 0 {
		return nil
	}
	current := reg.Providers[idx]
	if current.AuthKind != llmpool.ProviderAuthTrae {
		return nil
	}
	current.APIKey = cred.AccessToken
	if strings.TrimSpace(cred.RefreshToken) != "" {
		current.TraeRefreshToken = cred.RefreshToken
	}
	if cred.ExpiresAt > 0 {
		current.TraeExpiresAt = cred.ExpiresAt
	}
	if strings.TrimSpace(cred.MachineID) != "" {
		current.TraeMachineID = cred.MachineID
	}
	if strings.TrimSpace(cred.DeviceID) != "" {
		current.TraeDeviceID = cred.DeviceID
	}
	if strings.TrimSpace(cred.UserID) != "" {
		current.TraeUserID = cred.UserID
	}
	next := cloneRegistry(reg)
	next.Providers[idx] = current
	return s.persistRegistry(ctx, next)
}

type traeHeaderTripper struct {
	base http.RoundTripper
	cfg  corelib.MaclawLLMConfig
}

func (t traeHeaderTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req != nil {
		// The device pair the refresh token was minted against must win over
		// the claims-derived fallback the transport would otherwise pick.
		trae.ApplyHeaders(req.Header, t.cfg.TraeMachineID, t.cfg.TraeDeviceID, t.cfg.TraeUserID)
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

// TraeHTTPClient wraps the shared endpoint client with the SOLO translation and
// the stored device pair. The transport's own claims-derived fallback cannot
// know which machine the refresh token was minted against, so every caller
// must use this rather than the bare trae.WrapClient.
func TraeHTTPClient(client *http.Client, cfg corelib.MaclawLLMConfig) *http.Client {
	return traeHTTPClient(client, cfg)
}

func traeHTTPClient(client *http.Client, cfg corelib.MaclawLLMConfig) *http.Client {
	if client == nil {
		client = corelib.NewLLMEndpointHTTPClient(cfg)
	}
	wrapped := trae.WrapClient(client)
	clone := *wrapped
	clone.Transport = traeHeaderTripper{base: wrapped.Transport, cfg: cfg}
	return &clone
}

func tryForwardTrae(ctx context.Context, client *http.Client, provider *llmpool.ProviderConfig, body map[string]any, upstreamModel, responseModel string, retry bool) (*providerForwardResponse, bool, error) {
	if _, ok := traeMaclawConfig(provider, upstreamModel); !ok {
		return nil, false, nil
	}
	provider = refreshTraeProvider(ctx, provider)
	cfg, ok := traeMaclawConfig(provider, upstreamModel)
	if !ok {
		return nil, false, nil
	}
	client = traeHTTPClient(client, cfg)
	var (
		respBody   []byte
		statusCode int
		err        error
	)
	if retry {
		respBody, statusCode, _, err = corelib.ForwardOpenAICompatRequestWithRetry(ctx, cfg, body, client, responseModel)
	} else {
		fwd := make(map[string]any, len(body))
		for k, v := range body {
			fwd[k] = v
		}
		respBody, statusCode, err = corelib.ForwardOpenAICompatRequest(ctx, cfg, fwd, client, responseModel)
	}
	if err != nil {
		if statusCode >= 400 && statusCode <= 599 {
			if len(respBody) == 0 {
				respBody = []byte(err.Error())
			}
			return &providerForwardResponse{StatusCode: statusCode, Body: respBody}, true, nil
		}
		return nil, true, fmt.Errorf("forward to %s: %w", provider.ID, err)
	}
	return &providerForwardResponse{StatusCode: statusCode, Body: respBody}, true, nil
}

func prepareTraeStream(ctx context.Context, client *http.Client, provider *llmpool.ProviderConfig, upstreamModel string) (*http.Client, *llmpool.ProviderConfig, func(*http.Request)) {
	if _, ok := traeMaclawConfig(provider, upstreamModel); !ok {
		return client, provider, nil
	}
	provider = refreshTraeProvider(ctx, provider)
	cfg, ok := traeMaclawConfig(provider, upstreamModel)
	if !ok {
		return client, provider, nil
	}
	if client == nil {
		client = corelib.NewLLMEndpointHTTPClient(cfg)
	}
	wrapped := trae.WrapClient(client)
	clone := *wrapped
	clone.Transport = traeHeaderTripper{base: wrapped.Transport, cfg: cfg}
	return &clone, provider, nil
}
