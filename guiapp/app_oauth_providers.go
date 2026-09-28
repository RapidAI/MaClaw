package guiapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/kimicode"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"github.com/RapidAI/CodeClaw/corelib/workbuddy"

	"github.com/pkg/browser"
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
			// A new OAuth token is not eligible until the post-login probe passes.
			data.Providers[i].ConnectionTestPassed = false
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
			// The exchanged API credential must pass the post-login probe again.
			data.Providers[i].ConnectionTestPassed = false
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

// StartWorkBuddyOAuth opens the official WorkBuddy or CodeBuddy login page and
// waits until that edition's account is saved.
func (a *App) StartWorkBuddyOAuth(providerName string) (string, error) {
	providerName = strings.TrimSpace(providerName)
	profile, ok := workbuddy.ProfileByName(providerName)
	if !ok {
		return "", fmt.Errorf("未知的 WorkBuddy 服务商 %q", providerName)
	}
	ctx, finish, claimResult := a.beginOAuthFlow(5 * time.Minute)
	cred, err := workbuddy.RunLogin(ctx, profile, llmOrEnvProxy)
	if err != nil {
		finish()
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("%s 登录已取消或超时", profile.Name)
		}
		return "", fmt.Errorf("%s 登录失败: %w", profile.Name, err)
	}
	defer finish()
	if err := claimResult(func() error {
		return a.saveWorkBuddyLogin(profile, cred)
	}); err != nil {
		return "", err
	}
	return a.oauthLoginSuccessMessage(profile.Name, profile.Name+" 登录成功")
}

// CancelWorkBuddyOAuth cancels an in-progress WorkBuddy or CodeBuddy login.
func (a *App) CancelWorkBuddyOAuth() {
	a.cancelOAuthFlow()
}

func (a *App) saveWorkBuddyLogin(profile workbuddy.Profile, cred *workbuddy.AccountCredential) error {
	if cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
		return fmt.Errorf("%s 登录未返回访问令牌", profile.Name)
	}
	data := a.GetMaclawLLMProviders()
	for i, p := range data.Providers {
		if p.Name != profile.Name || !normalizeMaclawLLMAuthTypeKind(p.AuthType).IsOAuth() {
			continue
		}
		data.Providers[i].URL = profile.ChatURL
		data.Providers[i].Protocol = "openai"
		data.Providers[i].AuthType = "oauth"
		if strings.TrimSpace(data.Providers[i].Model) == "" {
			data.Providers[i].Model = profile.DefaultModel
		}
		if data.Providers[i].ContextLength <= 0 {
			data.Providers[i].ContextLength = profile.DefaultContext()
		}
		data.Providers[i].Key = cred.AccessToken
		data.Providers[i].OAuthAccessToken = cred.AccessToken
		data.Providers[i].RefreshToken = cred.RefreshToken
		data.Providers[i].TokenExpiresAt = cred.ExpiresAt
		data.Providers[i].ConnectionTestPassed = false
		if err := a.SaveMaclawLLMProviders(data.Providers, profile.Name); err != nil {
			return fmt.Errorf("保存 %s 登录配置失败: %w", profile.Name, err)
		}
		if a.credentialStore != nil {
			stored := &oauth.StoredCredential{
				Type:           "oauth",
				AccessToken:    cred.AccessToken,
				RawAccessToken: cred.AccessToken,
				RefreshToken:   cred.RefreshToken,
				ExpiresAt:      cred.ExpiresAt,
				UserID:         cred.UserID,
				EnterpriseID:   cred.EnterpriseID,
				Domain:         cred.Domain,
				Email:          cred.Nickname,
			}
			if err := a.credentialStore.Modify(profile.StoreID, func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
				return stored, nil
			}); err != nil {
				return fmt.Errorf("保存 %s 登录凭据失败: %w", profile.Name, err)
			}
		}
		return nil
	}
	return fmt.Errorf("未找到 %s provider", profile.Name)
}

// KimiCodeDeviceInfo is returned to the frontend for the Kimi Code device login.
type KimiCodeDeviceInfo struct {
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	BrowserOpened           bool   `json:"browser_opened"`
}

// kimiCodeLogin is one in-flight device approval. claim persists the token
// only when this login is still the active OAuth flow.
type kimiCodeLogin struct {
	ctx        context.Context
	deviceCode string
	interval   int
	baseURL    string
	oauthHost  string
	generation uint64
	finish     func()
	claim      func(func() error) error
}

// StartKimiCodeOAuth begins Kimi Code's browser device login and opens the
// approval page. The frontend shows the user code, then calls WaitKimiCodeOAuth.
func (a *App) StartKimiCodeOAuth() (KimiCodeDeviceInfo, error) {
	if cfg, err := a.LoadConfig(); err == nil {
		oauth.ApplyProxyFromAppConfig(cfg)
	}
	// Own the single-flight slot before the device request so Cancel can abort
	// it, and so a caller that never reaches Wait does not leave a later login blocked.
	parent, finish, claim := a.beginOAuthFlow(kimicode.MaxLoginLifetime())
	a.oauthMu.Lock()
	ownedGen := a.oauthGeneration
	a.kimiOwnedGen = ownedGen
	a.oauthMu.Unlock()
	oauthHost := kimicode.OAuthHost()
	device, err := kimicode.RequestDeviceAuthorizationAt(parent, oauthHost)
	if err != nil {
		finish()
		a.clearKimiOwnedGen(ownedGen)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return KimiCodeDeviceInfo{}, fmt.Errorf("Kimi Code 登录已取消或超时")
		}
		return KimiCodeDeviceInfo{}, fmt.Errorf("Kimi Code 设备码请求失败: %w", err)
	}
	approvalURL := device.VerificationURIComplete
	if approvalURL == "" {
		approvalURL = device.VerificationURI
	}
	if !kimicode.IsApprovalURL(approvalURL) {
		finish()
		a.clearKimiOwnedGen(ownedGen)
		return KimiCodeDeviceInfo{}, fmt.Errorf("Kimi Code 授权地址无效")
	}
	if err := parent.Err(); err != nil {
		finish()
		a.clearKimiOwnedGen(ownedGen)
		return KimiCodeDeviceInfo{}, fmt.Errorf("Kimi Code 登录已取消或超时")
	}
	waitCtx, waitCancel := context.WithTimeout(parent, kimicode.LoginLifetime(device.ExpiresIn))
	flow := &kimiCodeLogin{
		ctx:        waitCtx,
		deviceCode: device.DeviceCode,
		interval:   device.Interval,
		baseURL:    kimicode.BaseURLForOAuthHost(oauthHost),
		oauthHost:  oauthHost,
		generation: ownedGen,
		finish: func() {
			waitCancel()
			finish()
		},
		claim: claim,
	}
	a.oauthMu.Lock()
	previous := a.kimiLogin
	a.kimiLogin = flow
	a.oauthMu.Unlock()
	if previous != nil && previous.finish != nil {
		previous.finish()
	}

	info := KimiCodeDeviceInfo{
		UserCode:                device.UserCode,
		VerificationURI:         device.VerificationURI,
		VerificationURIComplete: device.VerificationURIComplete,
	}
	if parent.Err() != nil {
		a.oauthMu.Lock()
		if a.kimiLogin == flow {
			a.kimiLogin = nil
		}
		a.oauthMu.Unlock()
		flow.finish()
		a.clearKimiOwnedGen(ownedGen)
		return KimiCodeDeviceInfo{}, fmt.Errorf("Kimi Code 登录已取消或超时")
	}
	if err := browser.OpenURL(approvalURL); err != nil {
		logKimiBrowserOpen(err)
	} else if parent.Err() == nil {
		info.BrowserOpened = true
	}
	if parent.Err() != nil {
		// Cancel may already have dropped this flow. Do not cancel whatever
		// login replaced it.
		a.oauthMu.Lock()
		if a.kimiLogin == flow {
			a.kimiLogin = nil
		}
		a.oauthMu.Unlock()
		flow.finish()
		a.clearKimiOwnedGen(ownedGen)
		return KimiCodeDeviceInfo{}, fmt.Errorf("Kimi Code 登录已取消或超时")
	}
	return info, nil
}

func (a *App) clearKimiOwnedGen(gen uint64) {
	a.oauthMu.Lock()
	if a.kimiOwnedGen == gen {
		a.kimiOwnedGen = 0
	}
	a.oauthMu.Unlock()
}

func logKimiBrowserOpen(err error) {
	if err == nil {
		return
	}
	// The approval URL contains the user code. Keep the failure reason only.
	log.Printf("[kimi-code] browser open failed: %s", browserOpenReason(err))
}

func browserOpenReason(err error) string {
	text := err.Error()
	if idx := strings.Index(text, "http"); idx >= 0 {
		return strings.TrimSpace(text[:idx])
	}
	return text
}

// WaitKimiCodeOAuth blocks until the browser approval finishes or is cancelled.
func (a *App) WaitKimiCodeOAuth() (string, error) {
	a.oauthMu.Lock()
	flow := a.kimiLogin
	a.kimiLogin = nil
	a.oauthMu.Unlock()
	if flow == nil || flow.ctx == nil || flow.deviceCode == "" || flow.finish == nil || flow.claim == nil {
		return "", fmt.Errorf("没有正在进行的 Kimi Code 登录")
	}
	defer func() {
		flow.finish()
		a.clearKimiOwnedGen(flow.generation)
	}()

	token, err := kimicode.PollUntilAuthorizedAt(flow.ctx, flow.deviceCode, flow.interval, flow.oauthHost)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("Kimi Code 登录已取消或超时")
		}
		return "", fmt.Errorf("Kimi Code 登录失败: %w", err)
	}
	if err := flow.claim(func() error {
		return a.saveKimiCodeLogin(token, flow.baseURL)
	}); err != nil {
		return "", err
	}
	return a.oauthLoginSuccessMessage(kimicode.Name, kimicode.Name+" 登录成功")
}

// CancelKimiCodeOAuth cancels an in-progress Kimi Code device login.
// A login that has not reached Wait is dropped here so it cannot be polled later.
func (a *App) CancelKimiCodeOAuth() {
	a.oauthMu.Lock()
	flow := a.kimiLogin
	owns := a.kimiOwnedGen != 0 && a.kimiOwnedGen == a.oauthGeneration
	// A stored flow from an older attempt must not hide the device request
	// that currently owns the single-flight slot.
	if flow != nil && flow.generation != a.oauthGeneration {
		flow = nil
	} else if flow != nil {
		a.kimiLogin = nil
	}
	var cancel context.CancelFunc
	if owns && flow == nil && a.oauthCancel != nil {
		// Wait owns the flow, or the device request has not stored one yet.
		// Cancel that context before releasing the lock so a newer login
		// cannot be the one that gets cancelled.
		a.oauthGeneration++
		cancel = a.oauthCancel
		a.oauthCancel = nil
		a.kimiOwnedGen = 0
	} else if owns && flow != nil {
		a.kimiOwnedGen = 0
	}
	a.oauthMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if flow != nil && flow.finish != nil {
		// This flow's own finish cancels its context. It does not bump the
		// global generation, so a login that already replaced it stays up.
		flow.finish()
	}
}

func (a *App) saveKimiCodeLogin(token kimicode.Token, baseURL string) error {
	if strings.TrimSpace(token.AccessToken) == "" {
		return fmt.Errorf("Kimi Code 登录未返回访问令牌")
	}
	data := a.GetMaclawLLMProviders()
	for i, p := range data.Providers {
		if !kimicode.IsProviderName(p.Name) || !normalizeMaclawLLMAuthTypeKind(p.AuthType).IsOAuth() {
			continue
		}
		data.Providers[i] = oauth.ApplyTokenResult(p, kimiCodeTokenResult(token))
		data.Providers[i].URL = kimicode.CanonicalBaseURL(baseURL)
		data.Providers[i].Protocol = "openai"
		data.Providers[i].AuthType = "oauth"
		data.Providers[i].ConnectionTestPassed = false
		if err := a.SaveMaclawLLMProviders(data.Providers, kimicode.Name); err != nil {
			return fmt.Errorf("保存 Kimi Code 登录配置失败: %w", err)
		}
		if a.credentialStore != nil {
			result := kimiCodeTokenResult(token)
			stored := &oauth.StoredCredential{
				Type:           "oauth",
				AccessToken:    result.AccessToken,
				RawAccessToken: result.RawAccessToken,
				RefreshToken:   result.RefreshToken,
				ExpiresAt:      data.Providers[i].TokenExpiresAt,
			}
			if err := a.credentialStore.Modify(kimicode.StoreID, func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
				return stored, nil
			}); err != nil {
				// The provider record already has the token. A later request
				// can copy it into the store, so a store glitch is not a failed login.
				log.Printf("[kimi-code] credential store save failed: %v", err)
			}
		}
		return nil
	}
	return fmt.Errorf("未找到 Kimi Code provider")
}

func kimiCodeTokenResult(token kimicode.Token) *oauth.TokenResult {
	return &oauth.TokenResult{
		AccessToken:    token.AccessToken,
		RawAccessToken: token.AccessToken,
		RefreshToken:   token.RefreshToken,
		ExpiresIn:      token.ExpiresIn,
	}
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
