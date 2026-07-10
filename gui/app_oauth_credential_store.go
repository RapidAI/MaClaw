package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// ─────────────────────────────────────────────────────────────────────────────
// Credential Store integration — bridging corelib/oauth.CredentialStore
// with the GUI App's provider system.
// ─────────────────────────────────────────────────────────────────────────────

// credentialStoreProviderID maps a MaclawLLMProvider name to the credential
// store's provider ID. This is the single mapping point.
func credentialStoreProviderID(provider corelib.MaclawLLMProvider) string {
	kind := normalizeMaclawLLMAuthTypeKind(provider.AuthType)
	switch {
	case provider.Name == "OpenAI" && kind.IsOAuth():
		return "openai"
	case provider.Name == "Anthropic" && kind.IsOAuth():
		return "anthropic"
	case provider.Name == "GitHub Copilot" && kind.IsOAuth():
		return "github-copilot"
	case provider.Name == codegenProviderName && provider.AuthType == "sso":
		return "codegen"
	default:
		return ""
	}
}

// migrateOAuthCredentialsOnStartup migrates OAuth credentials from config.json
// to the independent credential store. Only migrates if credential store
// doesn't already have the credential (idempotent).
func (a *App) migrateOAuthCredentialsOnStartup(config corelib.AppConfig) {
	if a.credentialStore == nil {
		return
	}

	var sources []oauth.MigrationSource
	for _, p := range config.MaclawLLMProviders {
		storeID := credentialStoreProviderID(p)
		if storeID == "" || p.Key == "" {
			continue
		}
		src := oauth.MigrationSource{
			ProviderID:     storeID,
			AccessToken:    p.Key,
			RawAccessToken: p.OAuthAccessToken,
			RefreshToken:   p.RefreshToken,
			ExpiresAt:      p.TokenExpiresAt,
		}
		kind := normalizeMaclawLLMAuthTypeKind(p.AuthType)
		if kind.IsOAuth() {
			src.Type = "oauth"
		} else {
			src.Type = "sso"
		}
		sources = append(sources, src)
	}

	if len(sources) == 0 {
		return
	}
	migrated := oauth.MigrateFromConfig(a.credentialStore, sources)
	if migrated > 0 {
		log.Printf("[credential-store] startup migration: migrated %d credential(s) from config.json", migrated)
	}
}

// ensureOAuthTokenViaStore checks if the current OAuth provider's token needs
// refresh and does so through the CredentialStore (serialized, no config.json
// contention). Falls back to the legacy path if credential store is nil.
//
// This is called from ensureOAuthToken (app_maclaw_llm.go).
func (a *App) ensureOAuthTokenViaStore(provider corelib.MaclawLLMProvider, providerIdx int) error {
	storeID := credentialStoreProviderID(provider)
	if storeID == "" || a.credentialStore == nil {
		return nil // not an OAuth provider or store not initialized
	}

	// Track whether sync is needed (avoid IO inside Modify's fn).
	var syncCred *oauth.StoredCredential

	err := a.credentialStore.Modify(storeID, func(old *oauth.StoredCredential) (*oauth.StoredCredential, error) {
		// If store has no credential, try to use config.json fields as source
		if old == nil {
			if provider.Key == "" {
				return nil, nil // nothing to work with
			}
			// Bootstrap from config.json fields (first-time migration at refresh time)
			old = &oauth.StoredCredential{
				Type:           "oauth",
				AccessToken:    provider.Key,
				RawAccessToken: provider.OAuthAccessToken,
				RefreshToken:   provider.RefreshToken,
				ExpiresAt:      provider.TokenExpiresAt,
			}
		}

		// Check if refresh is needed
		if !old.IsExpired() {
			return old, nil // still valid, no change
		}

		if old.RefreshToken == "" {
			return old, fmt.Errorf("refresh_token is empty, please re-login (%s OAuth)", provider.Name)
		}

		// Dispatch refresh based on provider type
		var result *oauth.TokenResult
		var refreshErr error
		switch storeID {
		case "openai":
			cfg := oauth.DefaultConfig()
			result, refreshErr = oauth.RefreshAccessToken(cfg, old.RefreshToken)
		case "anthropic":
			result, refreshErr = oauth.RefreshAnthropicToken(old.RefreshToken)
		case "github-copilot":
			// GitHub Copilot: the "refresh" is re-exchanging the long-lived GitHub token
			// for a new short-lived Copilot API token.
			copilotResp, exchangeErr := oauth.ExchangeGitHubTokenForCopilot(old.AccessToken)
			if exchangeErr != nil {
				return old, fmt.Errorf("copilot token exchange failed: %w", exchangeErr)
			}
			updated := &oauth.StoredCredential{
				Type:           old.Type,
				AccessToken:    old.AccessToken,   // GitHub token (long-lived)
				RawAccessToken: copilotResp.Token, // Copilot API token (short-lived)
				RefreshToken:   old.RefreshToken,
				ExpiresAt:      copilotResp.ExpiresAt,
			}
			syncCred = updated
			log.Printf("[credential-store] refreshed %s copilot token (expires_at=%d)", storeID, copilotResp.ExpiresAt)
			return updated, nil
		default:
			return old, fmt.Errorf("unknown OAuth provider for refresh: %s", storeID)
		}

		if refreshErr != nil {
			return old, fmt.Errorf("token refresh failed: %w", refreshErr)
		}

		// Build updated credential
		updated := &oauth.StoredCredential{
			Type:           old.Type,
			AccessToken:    result.AccessToken,
			RawAccessToken: result.RawAccessToken,
			RefreshToken:   old.RefreshToken, // preserve old if new is empty
			ExpiresAt:      time.Now().Unix() + int64(result.ExpiresIn),
		}
		if result.RefreshToken != "" {
			updated.RefreshToken = result.RefreshToken
		}
		syncCred = updated
		log.Printf("[credential-store] refreshed %s token (expires_in=%d)", storeID, result.ExpiresIn)
		return updated, nil
	})

	// Sync back to config.json OUTSIDE of Modify (no lock nesting risk).
	if err == nil && syncCred != nil {
		a.syncCredentialToConfig(provider.Name, syncCred, providerIdx)
	}
	return err
}

// syncCredentialToConfig writes the refreshed credential back to config.json
// for backward compatibility with TUI which still reads from config.json.
// This is a "dual write" during the migration period.
func (a *App) syncCredentialToConfig(providerName string, cred *oauth.StoredCredential, providerIdx int) {
	data := a.GetMaclawLLMProviders()
	if providerIdx >= len(data.Providers) {
		return
	}
	p := data.Providers[providerIdx]
	if p.Name != providerName {
		// Index shifted, find by name
		for i, pp := range data.Providers {
			if pp.Name == providerName && (normalizeMaclawLLMAuthTypeKind(pp.AuthType).IsOAuth() || pp.AuthType == "sso") {
				providerIdx = i
				p = pp
				break
			}
		}
	}
	p.Key = cred.AccessToken
	p.OAuthAccessToken = cred.RawAccessToken
	if cred.RefreshToken != "" {
		p.RefreshToken = cred.RefreshToken
	}
	p.TokenExpiresAt = cred.ExpiresAt
	data.Providers[providerIdx] = p
	if err := a.SaveMaclawLLMProviders(data.Providers, data.Current); err != nil {
		log.Printf("[credential-store] sync to config.json failed: %v", err)
	}
}

// resolveProviderKeyFromStore reads the access token from the credential store.
// Falls back to the provider's Key field if store has no entry.
func (a *App) resolveProviderKeyFromStore(provider corelib.MaclawLLMProvider) string {
	legacyCodexJWT := func() string {
		return provider.CodexSubscriptionOAuthToken()
	}
	if a.credentialStore == nil {
		if token := legacyCodexJWT(); token != "" {
			return token
		}
		return provider.Key
	}
	storeID := credentialStoreProviderID(provider)
	if storeID == "" {
		return provider.Key
	}

	// GitHub Copilot special handling: API calls use the short-lived Copilot token
	if storeID == "github-copilot" {
		if key := a.resolveGitHubCopilotKey(); key != "" {
			return key
		}
		return provider.Key
	}

	cred, err := a.credentialStore.Read(storeID)
	if err != nil || cred == nil {
		if token := legacyCodexJWT(); token != "" {
			return token
		}
		return provider.Key // fallback
	}
	if storeID == "openai" && provider.IsCodexSubscriptionOAuthProvider() && cred.RawAccessToken != "" {
		// Older versions stored an exchanged sk- key in AccessToken. The ChatGPT
		// Codex backend requires the OAuth JWT retained in RawAccessToken.
		return cred.RawAccessToken
	}
	if storeID == "openai" && provider.IsCodexSubscriptionOAuthProvider() && strings.HasPrefix(cred.AccessToken, "sk-") {
		if token := legacyCodexJWT(); token != "" {
			return token
		}
	}
	return cred.AccessToken
}

// saveOAuthResultToStore saves a fresh OAuth login result to the credential store
// and syncs back to config.json for backward compatibility.
func (a *App) saveOAuthResultToStore(providerName string, result *oauth.TokenResult) {
	if a.credentialStore == nil {
		return
	}
	storeID := ""
	switch providerName {
	case "OpenAI":
		storeID = "openai"
	case "Anthropic":
		storeID = "anthropic"
	case "GitHub Copilot":
		storeID = "github-copilot"
	case codegenProviderName:
		storeID = "codegen"
	default:
		return
	}

	cred := &oauth.StoredCredential{
		Type:           "oauth",
		AccessToken:    result.AccessToken,
		RawAccessToken: result.RawAccessToken,
		RefreshToken:   result.RefreshToken,
		ExpiresAt:      time.Now().Unix() + int64(result.ExpiresIn),
	}
	if err := a.credentialStore.Modify(storeID, func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
		return cred, nil
	}); err != nil {
		log.Printf("[credential-store] save %s OAuth result failed: %v", storeID, err)
	}
}
