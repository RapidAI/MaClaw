package guiapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"github.com/RapidAI/CodeClaw/corelib/qoder"

	"github.com/pkg/browser"
)

// ─────────────────────────────────────────────────────────────────────────────
// Qoder OAuth — Wails Bindings (国内/国际 device login)
// ─────────────────────────────────────────────────────────────────────────────

// QoderDeviceInfo is returned to the frontend for the browser device login.
type QoderDeviceInfo struct {
	AuthURL       string `json:"auth_url"`
	BrowserOpened bool   `json:"browser_opened"`
}

// Start does not hit the network (the approval URL is computed locally), so
// the flow owns its flight slot here and Wait owns the polling below.
type qoderLoginFlow struct {
	ctx        context.Context
	profile    qoder.Profile
	machineID  string
	login      *qoder.DeviceLogin
	generation uint64
	finish     func()
	claim      func(func() error) error
}

// StartQoderOAuth begins the browser device login for 国内版 or 国际版.
// It opens the approval page immediately; the frontend then calls
// WaitQoderOAuth to block until the user completes it.
func (a *App) StartQoderOAuth(edition string) (QoderDeviceInfo, error) {
	profile, ok := qoder.ProfileByEdition(edition)
	if !ok {
		return QoderDeviceInfo{}, fmt.Errorf("未知的 Qoder 版本 %q", edition)
	}
	if cfg, err := a.LoadConfig(); err == nil {
		oauth.ApplyProxyFromAppConfig(cfg)
	}
	// The approval window is 5 minutes; give the flow slot a small buffer on
	// top so a slow browser open cannot eat the whole lifetime.
	parent, finish, claim := a.beginOAuthFlow(qoder.LoginLifetime() + 45*time.Second)
	a.oauthMu.Lock()
	ownedGen := a.oauthGeneration
	a.qoderOwnedGen = ownedGen
	a.oauthMu.Unlock()
	login, err := qoder.StartLogin(qoder.MachineID())
	if err != nil {
		finish()
		a.clearQoderOwnedGen(ownedGen)
		return QoderDeviceInfo{}, fmt.Errorf("Qoder 登录初始化失败: %w", err)
	}
	approvalURL := login.AuthURL(profile)
	if !qoder.IsApprovalURL(approvalURL) {
		finish()
		a.clearQoderOwnedGen(ownedGen)
		return QoderDeviceInfo{}, fmt.Errorf("Qoder 授权地址无效")
	}
	if err := parent.Err(); err != nil {
		finish()
		a.clearQoderOwnedGen(ownedGen)
		return QoderDeviceInfo{}, fmt.Errorf("Qoder 登录已取消或超时")
	}
	flow := &qoderLoginFlow{
		ctx:        parent,
		profile:    profile,
		login:      login,
		generation: ownedGen,
		finish:     finish,
		claim:      claim,
	}
	a.oauthMu.Lock()
	previous := a.qoderLogin
	a.qoderLogin = flow
	a.oauthMu.Unlock()
	if previous != nil && previous.finish != nil {
		// A newer login replaces any older in-flight one.
		previous.finish()
	}
	info := QoderDeviceInfo{AuthURL: approvalURL}
	if err := browser.OpenURL(approvalURL); err != nil {
		// The failure reason may embed the URL; log only the reason.
		log.Printf("[qoder] browser open failed: %s", strings.TrimSpace(err.Error()))
	} else {
		info.BrowserOpened = true
	}
	return info, nil
}

// WaitQoderOAuth blocks until the browser approval finishes or is cancelled.
func (a *App) WaitQoderOAuth() (string, error) {
	a.oauthMu.Lock()
	flow := a.qoderLogin
	a.qoderLogin = nil
	a.oauthMu.Unlock()
	if flow == nil || flow.ctx == nil || flow.login == nil || flow.finish == nil || flow.claim == nil {
		return "", fmt.Errorf("没有正在进行的 Qoder 登录")
	}
	defer func() {
		flow.finish()
		a.clearQoderOwnedGen(flow.generation)
	}()
	token, err := qoder.PollUntilAuthorized(flow.ctx, flow.profile, flow.login)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("Qoder 登录已取消或超时")
		}
		return "", fmt.Errorf("Qoder 登录失败: %w", err)
	}
	if err := flow.claim(func() error {
		return a.saveQoderLogin(flow.profile, token)
	}); err != nil {
		return "", err
	}
	return a.oauthLoginSuccessMessage(flow.profile.Name, flow.profile.Name+" 登录成功")
}

// CancelQoderOAuth stops an in-progress Qoder device login. A login that has
// not reached Wait is dropped here so it cannot poll later.
func (a *App) CancelQoderOAuth() {
	a.oauthMu.Lock()
	flow := a.qoderLogin
	owns := a.qoderOwnedGen != 0 && a.qoderOwnedGen == a.oauthGeneration
	// A stored flow from an older attempt must not hide the slot consumed by
	// the login that is starting now.
	if flow != nil && flow.generation != a.oauthGeneration {
		flow = nil
	} else if flow != nil {
		a.qoderLogin = nil
	}
	if owns {
		a.qoderOwnedGen = 0
	}
	a.oauthMu.Unlock()
	if flow != nil && flow.finish != nil {
		// This flow's own finish cancels its context only; a new login that
		// already replaced it keeps running.
		flow.finish()
	}
}

func (a *App) clearQoderOwnedGen(gen uint64) {
	a.oauthMu.Lock()
	if a.qoderOwnedGen == gen {
		a.qoderOwnedGen = 0
	}
	a.oauthMu.Unlock()
}

// saveQoderLogin persists a completed Qoder login into the provider snapshot
// and the credential store, then lets the normal post-login probe decide
// whether this provider is assignable.
func (a *App) saveQoderLogin(profile qoder.Profile, token qoder.Token) error {
	if strings.TrimSpace(token.AccessToken) == "" {
		return fmt.Errorf("Qoder 登录未返回访问令牌")
	}
	model := ""
	catalogCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, defaultKey, err := qoder.ListModels(catalogCtx, profile, token.AccessToken); err == nil && strings.TrimSpace(defaultKey) != "" {
		model = strings.TrimSpace(defaultKey)
	} else {
		log.Printf("[qoder] provider=%s model catalog unavailable, keeping the fallback default", profile.Name)
	}
	if model == "" {
		model = qoder.DefaultModel
	}
	data := a.GetMaclawLLMProviders()
	for i, p := range data.Providers {
		if p.Name != profile.Name || !normalizeMaclawLLMAuthTypeKind(p.AuthType).IsOAuth() {
			continue
		}
		data.Providers[i].URL = qoder.CanonicalChatURL(profile.ChatURL)
		data.Providers[i].Protocol = "openai"
		data.Providers[i].AuthType = "oauth"
		if strings.TrimSpace(data.Providers[i].Model) == "" {
			data.Providers[i].Model = model
		}
		if data.Providers[i].ContextLength <= 0 {
			data.Providers[i].ContextLength = qoder.DefaultContextWindows
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
				Email:          token.UserName,
			}
			if err := a.credentialStore.Modify(profile.StoreID, func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
				return stored, nil
			}); err != nil {
				// The provider record already carries the token; the store
				// copy can catch up on the next refresh.
				log.Printf("[qoder] credential store save failed: %v", err)
			}
		}
		return nil
	}
	return fmt.Errorf("未找到 %s provider", profile.Name)
}
