package main

import (
	"context"
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// ─────────────────────────────────────────────────────────────────────────────
// Anthropic OAuth — Wails Bindings
// ─────────────────────────────────────────────────────────────────────────────

// AnthropicOAuthInfo is returned to the frontend to display the authorization URL.
type AnthropicOAuthInfo struct {
	AuthURL string `json:"auth_url"`
}

// StartAnthropicOAuth begins the Anthropic OAuth flow.
// Returns the authorization URL — the frontend should open it in the browser
// and show an input field for the user to paste the authorization code.
func (a *App) StartAnthropicOAuth() (AnthropicOAuthInfo, error) {
	params, err := oauth.PrepareAnthropicOAuth()
	if err != nil {
		return AnthropicOAuthInfo{}, fmt.Errorf("Anthropic OAuth 准备失败: %w", err)
	}

	// Store params for CompleteAnthropicOAuth
	a.oauthMu.Lock()
	a.anthropicOAuthParams = params
	a.oauthMu.Unlock()

	return AnthropicOAuthInfo{AuthURL: params.AuthURL}, nil
}

// CompleteAnthropicOAuth finishes the Anthropic OAuth flow with the authorization
// code that the user copied from the browser callback page.
func (a *App) CompleteAnthropicOAuth(code string) (string, error) {
	a.oauthMu.Lock()
	params := a.anthropicOAuthParams
	a.anthropicOAuthParams = nil
	a.oauthMu.Unlock()

	if params == nil {
		return "", fmt.Errorf("没有正在进行的 Anthropic OAuth 流程，请先调用 StartAnthropicOAuth")
	}

	result, err := oauth.CompleteAnthropicOAuth(params, code)
	if err != nil {
		return "", fmt.Errorf("Anthropic OAuth 认证失败: %w", err)
	}

	// Update the Anthropic provider in config
	data := a.GetMaclawLLMProviders()
	for i, p := range data.Providers {
		if p.Name == "Anthropic" && normalizeMaclawLLMAuthTypeKind(p.AuthType).IsOAuth() {
			data.Providers[i] = oauth.ApplyTokenResult(p, result)
			if err := a.SaveMaclawLLMProviders(data.Providers, "Anthropic"); err != nil {
				return "", fmt.Errorf("保存 Anthropic OAuth 配置失败: %w", err)
			}
			// Save to credential store
			a.saveOAuthResultToStore("Anthropic", result)
			return a.oauthLoginSuccessMessage("Anthropic", "Anthropic OAuth 登录成功")
		}
	}
	return "", fmt.Errorf("未找到 Anthropic provider")
}

// ─────────────────────────────────────────────────────────────────────────────
// GitHub Copilot OAuth — Wails Bindings (Device Code Flow)
// ─────────────────────────────────────────────────────────────────────────────

// GitHubCopilotDeviceInfo is returned to the frontend for the device code flow.
type GitHubCopilotDeviceInfo struct {
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
}

// StartGitHubCopilotOAuth begins the GitHub Copilot device code flow.
// Returns user_code and verification_uri — the frontend should display these
// to the user and then call WaitGitHubCopilotOAuth to wait for completion.
func (a *App) StartGitHubCopilotOAuth() (GitHubCopilotDeviceInfo, error) {
	deviceResp, err := oauth.RequestGitHubDeviceCode()
	if err != nil {
		return GitHubCopilotDeviceInfo{}, fmt.Errorf("GitHub Copilot 设备码请求失败: %w", err)
	}

	// Start background polling
	ctx, cancel := context.WithTimeout(context.Background(), oauth.GitHubCopilotTimeout)

	a.oauthMu.Lock()
	a.oauthCancel = cancel
	a.copilotDeviceCode = deviceResp.DeviceCode
	a.copilotPollInterval = deviceResp.Interval
	a.copilotPollCtx = ctx
	a.oauthMu.Unlock()

	return GitHubCopilotDeviceInfo{
		UserCode:        deviceResp.UserCode,
		VerificationURI: deviceResp.VerificationURI,
	}, nil
}

// WaitGitHubCopilotOAuth blocks until the user completes the device code flow
// or the flow times out / is cancelled. The frontend calls this after showing
// the user_code and verification_uri.
func (a *App) WaitGitHubCopilotOAuth() (string, error) {
	a.oauthMu.Lock()
	ctx := a.copilotPollCtx
	deviceCode := a.copilotDeviceCode
	interval := a.copilotPollInterval
	a.oauthMu.Unlock()

	if ctx == nil || deviceCode == "" {
		return "", fmt.Errorf("没有正在进行的 GitHub Copilot OAuth 流程")
	}

	defer func() {
		a.oauthMu.Lock()
		if a.oauthCancel != nil {
			a.oauthCancel()
		}
		a.oauthCancel = nil
		a.copilotDeviceCode = ""
		a.copilotPollCtx = nil
		a.oauthMu.Unlock()
	}()

	// Poll until user authorizes
	githubToken, err := oauth.PollGitHubDeviceCode(ctx, deviceCode, interval)
	if err != nil {
		return "", fmt.Errorf("GitHub Copilot 认证失败: %w", err)
	}

	// Exchange for Copilot API token to verify subscription
	copilotResp, err := oauth.ExchangeGitHubTokenForCopilot(githubToken)
	if err != nil {
		return "", fmt.Errorf("GitHub 认证成功但 Copilot 订阅不可用: %w", err)
	}

	// Update the GitHub Copilot provider in config
	data := a.GetMaclawLLMProviders()
	for i, p := range data.Providers {
		if p.Name == "GitHub Copilot" && normalizeMaclawLLMAuthTypeKind(p.AuthType).IsOAuth() {
			data.Providers[i].Key = copilotResp.Token        // Short-lived Copilot token for immediate use
			data.Providers[i].OAuthAccessToken = githubToken // Long-lived GitHub token for refresh
			data.Providers[i].TokenExpiresAt = copilotResp.ExpiresAt
			data.Providers[i].RefreshToken = githubToken // GitHub token serves as refresh mechanism
			if err := a.SaveMaclawLLMProviders(data.Providers, "GitHub Copilot"); err != nil {
				return "", fmt.Errorf("保存 GitHub Copilot 配置失败: %w", err)
			}
			// Save to credential store with proper Copilot-specific structure:
			// AccessToken = GitHub token (long-lived, for re-exchange)
			// RawAccessToken = Copilot API token (short-lived, for actual API calls)
			if a.credentialStore != nil {
				_ = a.credentialStore.Modify("github-copilot", func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
					return &oauth.StoredCredential{
						Type:           "oauth",
						AccessToken:    githubToken,       // GitHub token (for re-exchange)
						RawAccessToken: copilotResp.Token, // Copilot API token (for actual API calls)
						ExpiresAt:      copilotResp.ExpiresAt,
					}, nil
				})
			}
			return a.oauthLoginSuccessMessage("GitHub Copilot", "GitHub Copilot 登录成功")
		}
	}
	return "", fmt.Errorf("未找到 GitHub Copilot provider")
}

// CancelGitHubCopilotOAuth cancels an in-progress device code flow.
func (a *App) CancelGitHubCopilotOAuth() {
	a.oauthMu.Lock()
	defer a.oauthMu.Unlock()
	if a.oauthCancel != nil {
		a.oauthCancel()
		a.oauthCancel = nil
	}
	a.copilotDeviceCode = ""
	a.copilotPollCtx = nil
}

// ─────────────────────────────────────────────────────────────────────────────
// resolveProviderKeyFromStore enhanced for GitHub Copilot
// ─────────────────────────────────────────────────────────────────────────────

// resolveGitHubCopilotKey reads the Copilot API token from credential store.
// For Copilot, the "key" sent to the API is the short-lived Copilot token
// (stored in RawAccessToken), not the long-lived GitHub token (stored in AccessToken).
func (a *App) resolveGitHubCopilotKey() string {
	if a.credentialStore == nil {
		return ""
	}
	cred, err := a.credentialStore.Read("github-copilot")
	if err != nil || cred == nil {
		return ""
	}
	// For Copilot: RawAccessToken is the short-lived API token
	if cred.RawAccessToken != "" {
		return cred.RawAccessToken
	}
	return cred.AccessToken
}

// ensureOAuthTokenForCurrentProvider: ensureOAuthToken already dispatches via
// credentialStoreProviderID which maps "Anthropic" → "anthropic" and
// "GitHub Copilot" → "github-copilot". No additional wiring needed.
