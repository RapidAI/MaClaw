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
	"github.com/RapidAI/CodeClaw/corelib/lobsterai"
)

// LobsterAIMaclawConfig builds the account context the LobsterAI upstream
// requires. The bool is false for every other provider.
func LobsterAIMaclawConfig(provider *llmpool.ProviderConfig, model string) (corelib.MaclawLLMConfig, bool) {
	return lobsterAIMaclawConfig(provider, model)
}

func lobsterAIMaclawConfig(provider *llmpool.ProviderConfig, model string) (corelib.MaclawLLMConfig, bool) {
	if !lobsterAIMatches(provider) {
		return corelib.MaclawLLMConfig{}, false
	}
	model = strings.TrimSpace(model)
	if model == "" && len(provider.Models) == 1 {
		model = strings.TrimSpace(provider.Models[0])
	}
	cfg := corelib.MaclawLLMConfig{
		URL:                   lobsterai.CanonicalChatURL(provider.APIURL),
		Key:                   strings.TrimSpace(provider.APIKey),
		Model:                 model,
		Protocol:              "openai",
		ProviderName:          lobsterai.Name,
		TimeoutSec:            provider.UpstreamTimeoutSec,
		LobsterAISessionUUID:  strings.TrimSpace(provider.LobsterAISessionUUID),
		LobsterAIFirstKeyfrom: strings.TrimSpace(provider.LobsterAIFirstKeyfrom),
		LobsterAIUserID:       strings.TrimSpace(provider.LobsterAIUserID),
	}
	cfg.AuthType = "oauth"
	return cfg, true
}

func lobsterAIMatches(provider *llmpool.ProviderConfig) bool {
	if provider == nil {
		return false
	}
	return lobsterai.Matches(provider.Name, provider.APIURL)
}

func lobsterAICredentialOf(provider *llmpool.ProviderConfig) lobsterAICredential {
	if provider == nil {
		return lobsterAICredential{}
	}
	return lobsterAICredential{
		AccessToken:  strings.TrimSpace(provider.APIKey),
		RefreshToken: strings.TrimSpace(provider.LobsterAIRefreshToken),
		ExpiresAt:    provider.LobsterAIExpiresAt,
		UserID:       strings.TrimSpace(provider.LobsterAIUserID),
		SessionUUID:  strings.TrimSpace(provider.LobsterAISessionUUID),
		FirstKeyfrom: strings.TrimSpace(provider.LobsterAIFirstKeyfrom),
	}
}

func lobsterAICredentialNeedsRefresh(cred lobsterAICredential, now time.Time) bool {
	if cred.ExpiresAt <= 0 {
		return false
	}
	return cred.ExpiresAt-300 <= now.Unix()
}

func refreshLobsterAIProvider(ctx context.Context, provider *llmpool.ProviderConfig) *llmpool.ProviderConfig {
	if provider == nil || strings.TrimSpace(provider.AuthKind) != llmpool.ProviderAuthLobsterAI {
		return provider
	}
	cred := lobsterAICredentialOf(provider)
	if !lobsterAICredentialNeedsRefresh(cred, time.Now()) {
		return provider
	}
	guard := lobsterAIRefreshGuard(provider.ID)
	guard.Lock()
	defer guard.Unlock()
	current := provider
	if latest := loadLobsterAIProvider(ctx, provider.ID); latest != nil && latest.AuthKind == llmpool.ProviderAuthLobsterAI {
		current = latest
	}
	cred = lobsterAICredentialOf(current)
	if !lobsterAICredentialNeedsRefresh(cred, time.Now()) {
		return current
	}
	token, err := lobsterai.Refresh(ctx, cred.RefreshToken, cred.SessionUUID, cred.FirstKeyfrom, cred.UserID)
	if err != nil {
		log.Printf("[llm] lobsterai refresh provider=%s: %v", current.ID, err)
		return current
	}
	next := *current
	next.APIKey = token.AccessToken
	next.LobsterAIRefreshToken = token.RefreshToken
	next.LobsterAIExpiresAt = token.ExpiresAt
	if strings.TrimSpace(token.UUID) != "" {
		next.LobsterAISessionUUID = token.UUID
	}
	if strings.TrimSpace(token.FirstKeyfrom) != "" {
		next.LobsterAIFirstKeyfrom = token.FirstKeyfrom
	}
	if strings.TrimSpace(token.UserID) != "" {
		next.LobsterAIUserID = token.UserID
	}
	if err := saveLobsterAICredential(ctx, next.ID, lobsterAICredential{
		AccessToken:  next.APIKey,
		RefreshToken: next.LobsterAIRefreshToken,
		ExpiresAt:    next.LobsterAIExpiresAt,
		UserID:       next.LobsterAIUserID,
		SessionUUID:  next.LobsterAISessionUUID,
		FirstKeyfrom: next.LobsterAIFirstKeyfrom,
	}); err != nil {
		log.Printf("[llm] lobsterai persist provider=%s: %v", next.ID, err)
	}
	return &next
}

func lobsterAIRefreshGuard(id string) *sync.Mutex {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		id = "-"
	}
	guard, _ := lobsterAIRefreshGuards.LoadOrStore(id, &sync.Mutex{})
	return guard.(*sync.Mutex)
}

func loadLobsterAIProvider(ctx context.Context, id string) *llmpool.ProviderConfig {
	lobsterAIPersistMu.RLock()
	load := lobsterAIPersistLoad
	lobsterAIPersistMu.RUnlock()
	if load == nil {
		return nil
	}
	got, err := load(ctx, id)
	if err != nil || got == nil {
		return nil
	}
	return got
}

func saveLobsterAICredential(ctx context.Context, id string, cred lobsterAICredential) error {
	lobsterAIPersistMu.RLock()
	save := lobsterAIPersistSave
	lobsterAIPersistMu.RUnlock()
	if save == nil {
		return nil
	}
	return save(ctx, id, cred)
}

func (s *Service) saveLobsterAICredential(ctx context.Context, id string, cred lobsterAICredential) error {
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
	if current.AuthKind != llmpool.ProviderAuthLobsterAI {
		return nil
	}
	current.APIKey = cred.AccessToken
	if strings.TrimSpace(cred.RefreshToken) != "" {
		current.LobsterAIRefreshToken = cred.RefreshToken
	}
	if cred.ExpiresAt > 0 {
		current.LobsterAIExpiresAt = cred.ExpiresAt
	}
	if strings.TrimSpace(cred.SessionUUID) != "" {
		current.LobsterAISessionUUID = cred.SessionUUID
	}
	if strings.TrimSpace(cred.FirstKeyfrom) != "" {
		current.LobsterAIFirstKeyfrom = cred.FirstKeyfrom
	}
	if strings.TrimSpace(cred.UserID) != "" {
		current.LobsterAIUserID = cred.UserID
	}
	next := cloneRegistry(reg)
	next.Providers[idx] = current
	return s.persistRegistry(ctx, next)
}

func tryForwardLobsterAI(ctx context.Context, client *http.Client, provider *llmpool.ProviderConfig, body map[string]any, upstreamModel, responseModel string, retry bool) (*providerForwardResponse, bool, error) {
	if _, ok := lobsterAIMaclawConfig(provider, upstreamModel); !ok {
		return nil, false, nil
	}
	provider = refreshLobsterAIProvider(ctx, provider)
	cfg, ok := lobsterAIMaclawConfig(provider, upstreamModel)
	if !ok {
		return nil, false, nil
	}
	client = lobsterai.WrapClient(client)
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

func prepareLobsterAIStream(ctx context.Context, client *http.Client, provider *llmpool.ProviderConfig, upstreamModel string) (*http.Client, *llmpool.ProviderConfig, func(*http.Request)) {
	if _, ok := lobsterAIMaclawConfig(provider, upstreamModel); !ok {
		return client, provider, nil
	}
	provider = refreshLobsterAIProvider(ctx, provider)
	cfg, ok := lobsterAIMaclawConfig(provider, upstreamModel)
	if !ok {
		return client, provider, nil
	}
	if client == nil {
		client = corelib.NewLLMEndpointHTTPClient(cfg)
	}
	return lobsterai.WrapClient(client), provider, nil
}
