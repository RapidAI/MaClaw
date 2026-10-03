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
	"github.com/RapidAI/CodeClaw/corelib/workbuddy"
)

var workBuddyPersist struct {
	mu   sync.RWMutex
	save func(ctx context.Context, id string, cred workbuddy.AccountCredential) error
	load func(ctx context.Context, id string) (*llmpool.ProviderConfig, error)
}

var workBuddyRefreshGuards sync.Map

func (s *Service) registerWorkBuddyPersister() {
	if s == nil {
		return
	}
	workBuddyPersist.mu.Lock()
	workBuddyPersist.save = s.saveWorkBuddyCredential
	workBuddyPersist.load = s.GetProvider
	workBuddyPersist.mu.Unlock()
}

// WorkBuddyMaclawConfig builds the account context the WorkBuddy upstream
// requires. The bool is false for every other provider.
func WorkBuddyMaclawConfig(provider *llmpool.ProviderConfig, model string) (corelib.MaclawLLMConfig, bool) {
	return workBuddyMaclawConfig(provider, model)
}

func workBuddyMaclawConfig(provider *llmpool.ProviderConfig, model string) (corelib.MaclawLLMConfig, bool) {
	profile, ok := workBuddyProfile(provider)
	if !ok {
		return corelib.MaclawLLMConfig{}, false
	}
	model = strings.TrimSpace(model)
	if model == "" && len(provider.Models) == 1 {
		model = strings.TrimSpace(provider.Models[0])
	}
	cfg := corelib.MaclawLLMConfig{
		URL:                   profile.ChatURL,
		Key:                   strings.TrimSpace(provider.APIKey),
		Model:                 model,
		Protocol:              "openai",
		ProviderName:          profile.Name,
		TimeoutSec:            provider.UpstreamTimeoutSec,
		WorkBuddyOrigin:       profile.Origin,
		WorkBuddyUserID:       strings.TrimSpace(provider.WorkBuddyUserID),
		WorkBuddyEnterpriseID: strings.TrimSpace(provider.WorkBuddyEnterpriseID),
		WorkBuddyDomain:       strings.TrimSpace(provider.WorkBuddyDomain),
		WorkBuddyRefreshToken: strings.TrimSpace(provider.WorkBuddyRefreshToken),
	}
	// Account logins refresh and send identity headers. A static API key stays
	// a bearer token. Marking it oauth would make ApplyHeaders describe the
	// key as an account that has no user, enterprise, or department.
	if workBuddyAccountHeaders(provider) {
		cfg.AuthType = "oauth"
	}
	return cfg, true
}

// workBuddyAccountHeaders reports a logged-in WorkBuddy account. Token Bank
// shares are API keys on the same host. ApplyHeaders emits absence markers
// for an empty account, and those markers are not part of the body translation.
// The probe that already succeeds does not send them.
func workBuddyAccountHeaders(provider *llmpool.ProviderConfig) bool {
	if provider == nil {
		return false
	}
	if strings.TrimSpace(provider.AuthKind) == llmpool.ProviderAuthWorkBuddy {
		return true
	}
	return strings.TrimSpace(provider.WorkBuddyUserID) != "" ||
		strings.TrimSpace(provider.WorkBuddyEnterpriseID) != "" ||
		strings.TrimSpace(provider.WorkBuddyDomain) != "" ||
		strings.TrimSpace(provider.WorkBuddyRefreshToken) != ""
}

// workBuddyProfile selects the edition whose chat translation this provider
// must use. An account login is recognized by auth_kind. A Token Bank share
// is only an API key on the WorkBuddy host, and that host still rejects the
// unmodified client body, so the URL is enough.
func workBuddyProfile(provider *llmpool.ProviderConfig) (workbuddy.Profile, bool) {
	if provider == nil {
		return workbuddy.Profile{}, false
	}
	if strings.TrimSpace(provider.AuthKind) == llmpool.ProviderAuthWorkBuddy {
		if profile, ok := workbuddy.ProfileByEdition(provider.WorkBuddyEdition); ok {
			return profile, true
		}
		if profile, ok := workbuddy.ProfileByURL(provider.APIURL); ok {
			return profile, true
		}
		return workbuddy.ProfileByName(provider.Name)
	}
	return workbuddy.ProfileByURL(provider.APIURL)
}

func workBuddyCredential(provider *llmpool.ProviderConfig) workbuddy.AccountCredential {
	if provider == nil {
		return workbuddy.AccountCredential{}
	}
	return workbuddy.AccountCredential{
		AccessToken:  strings.TrimSpace(provider.APIKey),
		RefreshToken: strings.TrimSpace(provider.WorkBuddyRefreshToken),
		ExpiresAt:    provider.WorkBuddyExpiresAt,
		Domain:       strings.TrimSpace(provider.WorkBuddyDomain),
		UserID:       strings.TrimSpace(provider.WorkBuddyUserID),
		EnterpriseID: strings.TrimSpace(provider.WorkBuddyEnterpriseID),
	}
}

func refreshWorkBuddyProvider(ctx context.Context, provider *llmpool.ProviderConfig) *llmpool.ProviderConfig {
	if provider == nil || strings.TrimSpace(provider.AuthKind) != llmpool.ProviderAuthWorkBuddy {
		return provider
	}
	if !workBuddyCredential(provider).NeedsRefresh(time.Now()) {
		return provider
	}
	guard := workBuddyRefreshGuard(provider.ID)
	guard.Lock()
	defer guard.Unlock()
	current := provider
	if latest := loadWorkBuddyProvider(ctx, provider.ID); latest != nil && latest.AuthKind == llmpool.ProviderAuthWorkBuddy {
		current = latest
	}
	cred := workBuddyCredential(current)
	if !cred.NeedsRefresh(time.Now()) {
		return current
	}
	profile, ok := workbuddy.ProfileByEdition(current.WorkBuddyEdition)
	if !ok {
		profile, ok = workbuddy.ProfileByURL(current.APIURL)
	}
	if !ok {
		return current
	}
	next, err := workbuddy.Refresh(ctx, profile, cred, nil)
	if err != nil {
		log.Printf("[llm] workbuddy refresh provider=%s: %v", current.ID, err)
		return current
	}
	updated := *current
	updated.APIKey = next.AccessToken
	updated.WorkBuddyRefreshToken = next.RefreshToken
	updated.WorkBuddyExpiresAt = next.ExpiresAt
	if strings.TrimSpace(next.Domain) != "" {
		updated.WorkBuddyDomain = next.Domain
	}
	if err := saveWorkBuddyCredential(ctx, updated.ID, next); err != nil {
		log.Printf("[llm] workbuddy persist provider=%s: %v", updated.ID, err)
	}
	return &updated
}

func workBuddyRefreshGuard(id string) *sync.Mutex {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		id = "-"
	}
	guard, _ := workBuddyRefreshGuards.LoadOrStore(id, &sync.Mutex{})
	return guard.(*sync.Mutex)
}

func loadWorkBuddyProvider(ctx context.Context, id string) *llmpool.ProviderConfig {
	workBuddyPersist.mu.RLock()
	load := workBuddyPersist.load
	workBuddyPersist.mu.RUnlock()
	if load == nil {
		return nil
	}
	got, err := load(ctx, id)
	if err != nil || got == nil {
		return nil
	}
	return got
}

func saveWorkBuddyCredential(ctx context.Context, id string, cred workbuddy.AccountCredential) error {
	workBuddyPersist.mu.RLock()
	save := workBuddyPersist.save
	workBuddyPersist.mu.RUnlock()
	if save == nil {
		return nil
	}
	return save(ctx, id, cred)
}

func (s *Service) saveWorkBuddyCredential(ctx context.Context, id string, cred workbuddy.AccountCredential) error {
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
	if current.AuthKind != llmpool.ProviderAuthWorkBuddy {
		return nil
	}
	current.APIKey = cred.AccessToken
	if strings.TrimSpace(cred.RefreshToken) != "" {
		current.WorkBuddyRefreshToken = cred.RefreshToken
	}
	if cred.ExpiresAt > 0 {
		current.WorkBuddyExpiresAt = cred.ExpiresAt
	}
	if strings.TrimSpace(cred.Domain) != "" {
		current.WorkBuddyDomain = cred.Domain
	}
	next := cloneRegistry(reg)
	next.Providers[idx] = current
	return s.persistRegistry(ctx, next)
}

type workBuddyHeaderTripper struct {
	base http.RoundTripper
	cfg  corelib.MaclawLLMConfig
}

func (t workBuddyHeaderTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req != nil {
		workbuddy.ApplyHeaders(req.Header, t.cfg)
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

func workBuddyHTTPClient(client *http.Client, cfg corelib.MaclawLLMConfig, accountHeaders bool) *http.Client {
	if client == nil {
		client = corelib.NewLLMEndpointHTTPClient(cfg)
	}
	wrapped := workbuddy.WrapClient(client)
	if !accountHeaders {
		return wrapped
	}
	clone := *wrapped
	clone.Transport = workBuddyHeaderTripper{base: wrapped.Transport, cfg: cfg}
	return &clone
}

func tryForwardWorkBuddy(ctx context.Context, client *http.Client, provider *llmpool.ProviderConfig, body map[string]any, upstreamModel, responseModel string, retry bool) (*providerForwardResponse, bool, error) {
	if _, ok := workBuddyMaclawConfig(provider, upstreamModel); !ok {
		return nil, false, nil
	}
	provider = refreshWorkBuddyProvider(ctx, provider)
	cfg, ok := workBuddyMaclawConfig(provider, upstreamModel)
	if !ok {
		return nil, false, nil
	}
	client = workBuddyHTTPClient(client, cfg, workBuddyAccountHeaders(provider))
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

func prepareWorkBuddyStream(ctx context.Context, client *http.Client, provider *llmpool.ProviderConfig, upstreamModel string) (*http.Client, *llmpool.ProviderConfig, func(*http.Request)) {
	if _, ok := workBuddyMaclawConfig(provider, upstreamModel); !ok {
		return client, provider, nil
	}
	provider = refreshWorkBuddyProvider(ctx, provider)
	cfg, ok := workBuddyMaclawConfig(provider, upstreamModel)
	if !ok {
		return client, provider, nil
	}
	if client == nil {
		client = corelib.NewLLMEndpointHTTPClient(cfg)
	}
	client = workbuddy.WrapClient(client)
	if !workBuddyAccountHeaders(provider) {
		return client, provider, nil
	}
	return client, provider, func(req *http.Request) {
		if req != nil {
			workbuddy.ApplyHeaders(req.Header, cfg)
		}
	}
}
