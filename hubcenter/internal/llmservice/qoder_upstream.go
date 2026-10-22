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
	"github.com/RapidAI/CodeClaw/corelib/qoder"
)

var (
	qoderPersistMu   sync.RWMutex
	qoderPersistSave func(ctx context.Context, id string, cred qoderCredential) error
	qoderPersistLoad func(ctx context.Context, id string) (*llmpool.ProviderConfig, error)

	traePersistMu   sync.RWMutex
	traePersistSave func(ctx context.Context, id string, cred traeCredential) error
	traePersistLoad func(ctx context.Context, id string) (*llmpool.ProviderConfig, error)

	lobsterAIPersistMu   sync.RWMutex
	lobsterAIPersistSave func(ctx context.Context, id string, cred lobsterAICredential) error
	lobsterAIPersistLoad func(ctx context.Context, id string) (*llmpool.ProviderConfig, error)
)

// registerVendorPersisters wires the refresh persistence hooks for Qoder, Trae
// and LobsterAI. NewService calls it once.
func (s *Service) registerVendorPersisters() {
	if s == nil {
		return
	}
	qoderPersistMu.Lock()
	qoderPersistSave = s.saveQoderCredential
	qoderPersistLoad = s.GetProvider
	qoderPersistMu.Unlock()
	traePersistMu.Lock()
	traePersistSave = s.saveTraeCredential
	traePersistLoad = s.GetProvider
	traePersistMu.Unlock()
	lobsterAIPersistMu.Lock()
	lobsterAIPersistSave = s.saveLobsterAICredential
	lobsterAIPersistLoad = s.GetProvider
	lobsterAIPersistMu.Unlock()
}

// qoderCredential is the token pair plus identity a Qoder refresh persists.
type qoderCredential struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64
	UserID       string
}

// traeCredential is the token pair plus identity a Trae refresh persists.
type traeCredential struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64
	UserID       string
	MachineID    string
	DeviceID     string
}

// lobsterAICredential is the token pair plus session identity a LobsterAI
// refresh persists.
type lobsterAICredential struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64
	UserID       string
	SessionUUID  string
	FirstKeyfrom string
}

var (
	qoderRefreshGuards     sync.Map
	traeRefreshGuards      sync.Map
	lobsterAIRefreshGuards sync.Map
)

// QoderMaclawConfig builds the account context the Qoder upstream requires.
// The bool is false for every other provider.
func QoderMaclawConfig(provider *llmpool.ProviderConfig, model string) (corelib.MaclawLLMConfig, bool) {
	return qoderMaclawConfig(provider, model)
}

// QoderMaclawConfigFromConfig reports whether a caller that already built a
// MaclawLLMConfig must wrap its client with the Qoder translation transport.
// The transport resolves the signing identity itself when the caller has none.
func QoderMaclawConfigFromConfig(cfg corelib.MaclawLLMConfig) bool {
	return qoder.Matches(cfg)
}

func qoderMaclawConfig(provider *llmpool.ProviderConfig, model string) (corelib.MaclawLLMConfig, bool) {
	profile, ok := qoderProfile(provider)
	if !ok {
		return corelib.MaclawLLMConfig{}, false
	}
	model = strings.TrimSpace(model)
	if model == "" && len(provider.Models) == 1 {
		model = strings.TrimSpace(provider.Models[0])
	}
	cfg := corelib.MaclawLLMConfig{
		URL:            qoder.CanonicalChatURL(profile.ChatURL),
		Key:            strings.TrimSpace(provider.APIKey),
		Model:          model,
		Protocol:       "openai",
		ProviderName:   profile.Name,
		TimeoutSec:     provider.UpstreamTimeoutSec,
		QoderMachineID: strings.TrimSpace(provider.QoderMachineID),
		QoderUserID:    strings.TrimSpace(provider.QoderUserID),
	}
	cfg.AuthType = "oauth"
	return WithMemberUpstreamOverrides(cfg, provider), true
}

// qoderProfile selects the edition whose signed gateway this provider must use.
// An account login is recognized by auth_kind; a static key on the shared chat
// front is enough on its own, because both editions publish the same URL.
func qoderProfile(provider *llmpool.ProviderConfig) (qoder.Profile, bool) {
	if provider == nil {
		return qoder.Profile{}, false
	}
	if strings.TrimSpace(provider.AuthKind) == llmpool.ProviderAuthQoder {
		if profile, ok := qoder.ProfileByEdition(provider.QoderEdition); ok {
			return profile, true
		}
		if profile, ok := qoder.ProfileByName(provider.Name); ok {
			return profile, true
		}
		return qoder.Profile{}, false
	}
	// Both editions publish the same chat front, so a URL alone cannot pick
	// one: an api_key provider on it stays unclassified.
	return qoder.Profile{}, false
}

func qoderCredentialOf(provider *llmpool.ProviderConfig) qoderCredential {
	if provider == nil {
		return qoderCredential{}
	}
	return qoderCredential{
		AccessToken:  strings.TrimSpace(provider.APIKey),
		RefreshToken: strings.TrimSpace(provider.QoderRefreshToken),
		ExpiresAt:    provider.QoderExpiresAt,
		UserID:       strings.TrimSpace(provider.QoderUserID),
	}
}

// qoderCredentialNeedsRefresh reports whether the access token is close enough
// to expiry that a refresh should ride the request. A zero expiry means the
// server never reported one; the token is then used until it fails.
func qoderCredentialNeedsRefresh(cred qoderCredential, now time.Time) bool {
	if cred.ExpiresAt <= 0 {
		return false
	}
	return cred.ExpiresAt-300 <= now.Unix()
}

func refreshQoderProvider(ctx context.Context, provider *llmpool.ProviderConfig) *llmpool.ProviderConfig {
	if provider == nil || strings.TrimSpace(provider.AuthKind) != llmpool.ProviderAuthQoder {
		return provider
	}
	cred := qoderCredentialOf(provider)
	if !qoderCredentialNeedsRefresh(cred, time.Now()) {
		return provider
	}
	guard := qoderRefreshGuard(provider.ID)
	guard.Lock()
	defer guard.Unlock()
	current := provider
	if latest := loadQoderProvider(ctx, provider.ID); latest != nil && latest.AuthKind == llmpool.ProviderAuthQoder {
		current = latest
	}
	cred = qoderCredentialOf(current)
	if !qoderCredentialNeedsRefresh(cred, time.Now()) {
		return current
	}
	profile, ok := qoderProfile(current)
	if !ok {
		return current
	}
	token, err := qoder.Refresh(ctx, profile, cred.RefreshToken, qoder.MachineID())
	if err != nil {
		log.Printf("[llm] qoder refresh provider=%s: %v", current.ID, err)
		return current
	}
	next := *current
	next.APIKey = token.AccessToken
	next.QoderRefreshToken = token.RefreshToken
	next.QoderExpiresAt = token.ExpiresAt
	if strings.TrimSpace(token.UserID) != "" {
		next.QoderUserID = token.UserID
	}
	if err := saveQoderCredential(ctx, next.ID, qoderCredential{
		AccessToken:  next.APIKey,
		RefreshToken: next.QoderRefreshToken,
		ExpiresAt:    next.QoderExpiresAt,
		UserID:       next.QoderUserID,
	}); err != nil {
		log.Printf("[llm] qoder persist provider=%s: %v", next.ID, err)
	}
	return &next
}

func qoderRefreshGuard(id string) *sync.Mutex {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		id = "-"
	}
	guard, _ := qoderRefreshGuards.LoadOrStore(id, &sync.Mutex{})
	return guard.(*sync.Mutex)
}

func loadQoderProvider(ctx context.Context, id string) *llmpool.ProviderConfig {
	qoderPersistMu.RLock()
	load := qoderPersistLoad
	qoderPersistMu.RUnlock()
	if load == nil {
		return nil
	}
	got, err := load(ctx, id)
	if err != nil || got == nil {
		return nil
	}
	return got
}

func saveQoderCredential(ctx context.Context, id string, cred qoderCredential) error {
	qoderPersistMu.RLock()
	save := qoderPersistSave
	qoderPersistMu.RUnlock()
	if save == nil {
		return nil
	}
	return save(ctx, id, cred)
}

func (s *Service) saveQoderCredential(ctx context.Context, id string, cred qoderCredential) error {
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
	if current.AuthKind != llmpool.ProviderAuthQoder {
		return nil
	}
	current.APIKey = cred.AccessToken
	if strings.TrimSpace(cred.RefreshToken) != "" {
		current.QoderRefreshToken = cred.RefreshToken
	}
	if cred.ExpiresAt > 0 {
		current.QoderExpiresAt = cred.ExpiresAt
	}
	if strings.TrimSpace(cred.UserID) != "" {
		current.QoderUserID = cred.UserID
	}
	next := cloneRegistry(reg)
	next.Providers[idx] = current
	return s.persistRegistry(ctx, next)
}

// QoderHTTPClient wraps the shared endpoint client with the Qoder translation.
// corelib's own entrypoint resolves the signing uid from a desktop file store,
// which a server process does not have, so the uid comes from the provider row.
// Every caller that talks to a Qoder upstream must use this, not the bare
// qoder.WrapClient, or the signed wire falls back to the store lookup.
func QoderHTTPClient(client *http.Client, cfg corelib.MaclawLLMConfig) *http.Client {
	return qoderHTTPClient(client, cfg)
}

func qoderHTTPClient(client *http.Client, cfg corelib.MaclawLLMConfig) *http.Client {
	if client == nil {
		client = corelib.NewLLMEndpointHTTPClient(cfg)
	}
	return qoder.WrapClientWithEdition(client, cfg.QoderUserID, qoderStoreID(cfg))
}

// qoderStoreID maps the provider name back to the credential-store edition id
// the transport expects ("qoder-cn" / "qoder-global").
func qoderStoreID(cfg corelib.MaclawLLMConfig) string {
	profile, ok := qoder.ProfileByName(cfg.ProviderName)
	if !ok {
		return ""
	}
	return profile.StoreID
}

func tryForwardQoder(ctx context.Context, client *http.Client, provider *llmpool.ProviderConfig, body map[string]any, upstreamModel, responseModel string, retry bool) (*providerForwardResponse, bool, error) {
	if _, ok := qoderMaclawConfig(provider, upstreamModel); !ok {
		return nil, false, nil
	}
	provider = refreshQoderProvider(ctx, provider)
	cfg, ok := qoderMaclawConfig(provider, upstreamModel)
	if !ok {
		return nil, false, nil
	}
	client = qoderHTTPClient(client, cfg)
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

func prepareQoderStream(ctx context.Context, client *http.Client, provider *llmpool.ProviderConfig, upstreamModel string) (*http.Client, *llmpool.ProviderConfig, func(*http.Request)) {
	if _, ok := qoderMaclawConfig(provider, upstreamModel); !ok {
		return client, provider, nil
	}
	provider = refreshQoderProvider(ctx, provider)
	cfg, ok := qoderMaclawConfig(provider, upstreamModel)
	if !ok {
		return client, provider, nil
	}
	return qoderHTTPClient(client, cfg), provider, nil
}
