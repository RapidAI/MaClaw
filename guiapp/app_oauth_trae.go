package guiapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"github.com/RapidAI/CodeClaw/corelib/trae"

	"github.com/pkg/browser"
)

// ─────────────────────────────────────────────────────────────────────────────
// Trae OAuth — Wails Bindings (国内/国际 浏览器回调登录)
// ─────────────────────────────────────────────────────────────────────────────

// TraeDeviceInfo is returned to the frontend for the browser redirect login.
type TraeDeviceInfo struct {
	AuthURL       string `json:"auth_url"`
	BrowserOpened bool   `json:"browser_opened"`
}

// Start owns the flight slot (the local listener binds before Start returns).
type traeLoginFlow struct {
	ctx        context.Context
	profile    trae.Profile
	session    *trae.LoginSession
	generation uint64
	finish     func()
	claim      func(func() error) error
}

// StartTraeOAuth begins the browser device login for 国内版 or 国际版. It opens
// the authorization page immediately; the frontend then calls WaitTraeOAuth
// to block until the user completes it.
func (a *App) StartTraeOAuth(edition string) (TraeDeviceInfo, error) {
	profile, ok := trae.ProfileByEdition(edition)
	if !ok {
		return TraeDeviceInfo{}, fmt.Errorf("未知的 Trae 版本 %q", edition)
	}
	if cfg, err := a.LoadConfig(); err == nil {
		oauth.ApplyProxyFromAppConfig(cfg)
	}
	// The callback window is bounded by the login lifetime; give the slot a
	// small buffer on top so a slow browser open cannot eat it all.
	parent, finish, claim := a.beginOAuthFlow(trae.LoginLifetime + 45*time.Second)
	a.oauthMu.Lock()
	ownedGen := a.oauthGeneration
	a.oauthMu.Unlock()
	session, loginURL, err := trae.StartLogin(profile, trae.CallbackPort)
	if err != nil {
		finish()
		return TraeDeviceInfo{}, fmt.Errorf("Trae 登录初始化失败: %w", err)
	}
	if !trae.IsApprovalURL(loginURL) {
		session.Cancel()
		finish()
		return TraeDeviceInfo{}, fmt.Errorf("Trae 授权地址无效")
	}
	flow := &traeLoginFlow{
		ctx:        parent,
		profile:    profile,
		session:    session,
		generation: ownedGen,
		finish:     finish,
		claim:      claim,
	}
	// The flow and its generation slot must be published together, and only
	// while the flow context is still alive. A concurrent second login
	// cancels this parent before bumping the global slot.
	a.oauthMu.Lock()
	if parent.Err() != nil {
		a.oauthMu.Unlock()
		session.Cancel()
		finish()
		return TraeDeviceInfo{}, fmt.Errorf("Trae 登录已取消或超时")
	}
	previous := a.traeLogin
	a.traeLogin = flow
	a.traeOwnedGen = ownedGen
	a.oauthMu.Unlock()
	if previous != nil {
		previous.session.Cancel()
		if previous.finish != nil {
			previous.finish()
		}
	}
	info := TraeDeviceInfo{AuthURL: loginURL}
	if err := browser.OpenURL(loginURL); err != nil {
		log.Printf("[trae] browser open failed: %s", strings.TrimSpace(browserOpenReason(err)))
	} else {
		info.BrowserOpened = true
	}
	return info, nil
}

// WaitTraeOAuth blocks until the browser redirect lands and the token
// exchange completes or the login is cancelled.
func (a *App) WaitTraeOAuth() (string, error) {
	a.oauthMu.Lock()
	flow := a.traeLogin
	a.traeLogin = nil
	a.oauthMu.Unlock()
	if flow == nil || flow.ctx == nil || flow.session == nil || flow.finish == nil || flow.claim == nil {
		return "", fmt.Errorf("没有正在进行的 Trae 登录")
	}
	defer func() {
		flow.session.Cancel()
		flow.finish()
		a.clearTraeOwnedGen(flow.generation)
	}()
	details, err := flow.session.Wait(flow.ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("Trae 登录已取消或超时")
		}
		return "", fmt.Errorf("Trae 登录失败: %w", err)
	}
	token, err := flow.session.Resolve(flow.ctx, flow.profile, details)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("Trae 登录已取消或超时")
		}
		return "", fmt.Errorf("Trae 登录失败: %w", err)
	}
	if strings.TrimSpace(token.UserID) == "" || strings.TrimSpace(token.AccessToken) == "" {
		// The display name rides the account id; a missing id means the
		// upstream stripped the account details. Best effort fill.
		if user, name, _, infoErr := trae.GetUserInfo(flow.ctx, flow.profile, token); infoErr == nil {
			token.UserID = strings.TrimSpace(user)
			token.DisplayName = strings.TrimSpace(name)
		} else {
			log.Printf("[trae] provider=%s user info unavailable: %v", flow.profile.Name, infoErr)
		}
	}
	if err := flow.claim(func() error {
		return a.saveTraeLogin(flow.profile, token)
	}); err != nil {
		return "", err
	}
	return a.oauthLoginSuccessMessage(flow.profile.Name, flow.profile.Name+" 登录成功")
}

// CancelTraeOAuth stops an in-progress Trae device login.
func (a *App) CancelTraeOAuth() {
	a.oauthMu.Lock()
	flow := a.traeLogin
	owns := a.traeOwnedGen != 0 && a.traeOwnedGen == a.oauthGeneration
	if flow != nil && flow.generation != a.oauthGeneration {
		flow = nil
	} else if flow != nil {
		a.traeLogin = nil
	}
	var cancel context.CancelFunc
	if owns && flow == nil && a.oauthCancel != nil {
		a.oauthGeneration++
		cancel = a.oauthCancel
		a.oauthCancel = nil
		a.traeOwnedGen = 0
	} else if owns {
		a.traeOwnedGen = 0
	}
	a.oauthMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if flow != nil {
		flow.session.Cancel()
		if flow.finish != nil {
			flow.finish()
		}
	}
}

func (a *App) clearTraeOwnedGen(gen uint64) {
	a.oauthMu.Lock()
	if a.traeOwnedGen == gen {
		a.traeOwnedGen = 0
	}
	a.oauthMu.Unlock()
}

// saveTraeLogin persists a completed Trae login into the provider snapshot
// and the credential store.
func (a *App) saveTraeLogin(profile trae.Profile, token *trae.Token) error {
	if token == nil || strings.TrimSpace(token.AccessToken) == "" {
		return fmt.Errorf("%s 登录未返回访问令牌", profile.Name)
	}
	// The catalog default rides the built-in profile default; the login flow
	// populates the model input from the SOLO catalog when the user asks.
	defaultModel := profile.DefaultModel
	data := a.GetMaclawLLMProviders()
	for i, p := range data.Providers {
		if !corelib.MaclawLLMProviderNameEqual(p.Name, profile.Name) || !normalizeMaclawLLMAuthTypeKind(p.AuthType).IsOAuth() {
			continue
		}
		data.Providers[i].URL = profile.ChatHost
		data.Providers[i].Protocol = "openai"
		data.Providers[i].AuthType = "oauth"
		if strings.TrimSpace(data.Providers[i].Model) == "" {
			data.Providers[i].Model = defaultModel
		}
		if data.Providers[i].ContextLength <= 0 {
			data.Providers[i].ContextLength = trae.DefaultContextLength
		}
		data.Providers[i].Key = token.AccessToken
		data.Providers[i].OAuthAccessToken = token.AccessToken
		data.Providers[i].RefreshToken = token.RefreshToken
		data.Providers[i].TokenExpiresAt = token.ExpiresAt
		// A fresh login has not passed the post-login model probe yet.
		data.Providers[i].ConnectionTestPassed = false
		if err := a.SaveMaclawLLMProviders(data.Providers, profile.Name); err != nil {
			return fmt.Errorf("保存 %s 登录配置失败: %w", profile.Name, err)
		}
		if a.credentialStore != nil {
			stored := &oauth.StoredCredential{
				Type:           "oauth",
				AccessToken:    token.AccessToken,
				RawAccessToken: token.AccessToken,
				RefreshToken:   token.RefreshToken,
				ExpiresAt:      token.ExpiresAt,
				UserID:         token.UserID,
				Email:          token.DisplayName,
				EnterpriseID:   token.EnterpriseID,
				// The device pair the refresh token was minted against.
				MachineID: token.MachineID,
				DeviceID:  token.DeviceID,
			}
			if err := a.credentialStore.Modify(profile.StoreID, func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
				return stored, nil
			}); err != nil {
				// The provider record already carries the token; the store
				// copy can catch up on the next refresh.
				log.Printf("[trae] credential store save failed: %v", err)
			}
		}
		return nil
	}
	return fmt.Errorf("未找到 %s provider", profile.Name)
}
