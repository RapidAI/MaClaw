package guiapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/lobsterai"
	"github.com/RapidAI/CodeClaw/corelib/oauth"

	"github.com/pkg/browser"
)

// ─────────────────────────────────────────────────────────────────────────────
// LobsterAI OAuth — Wails Bindings (浏览器回调登录)
// ─────────────────────────────────────────────────────────────────────────────

// LobsterAIOAuthInfo is returned to the frontend for the browser login.
type LobsterAIOAuthInfo struct {
	AuthURL       string `json:"auth_url"`
	BrowserOpened bool   `json:"browser_opened"`
}

// Start owns the flight slot (the local listener binds before Start returns).
type lobsterLoginFlow struct {
	ctx        context.Context
	session    *lobsterai.LoginSession
	generation uint64
	finish     func()
	claim      func(func() error) error
}

// StartLobsterAIOAuth begins the browser login and opens the portal page
// immediately; the frontend then calls WaitLobsterAIOAuth to block until the
// user completes it.
func (a *App) StartLobsterAIOAuth() (LobsterAIOAuthInfo, error) {
	if cfg, err := a.LoadConfig(); err == nil {
		oauth.ApplyProxyFromAppConfig(cfg)
	}
	parent, finish, claim := a.beginOAuthFlow(lobsterai.LoginLifetime + 45*time.Second)
	a.oauthMu.Lock()
	ownedGen := a.oauthGeneration
	a.oauthMu.Unlock()
	session, err := lobsterai.StartLogin(0)
	if err != nil {
		finish()
		return LobsterAIOAuthInfo{}, fmt.Errorf("LobsterAI 登录初始化失败: %w", err)
	}
	flow := &lobsterLoginFlow{
		ctx:        parent,
		session:    session,
		generation: ownedGen,
		finish:     finish,
		claim:      claim,
	}
	a.oauthMu.Lock()
	if parent.Err() != nil {
		a.oauthMu.Unlock()
		session.Cancel()
		finish()
		return LobsterAIOAuthInfo{}, fmt.Errorf("LobsterAI 登录已取消或超时")
	}
	previous := a.lobsterLogin
	a.lobsterLogin = flow
	a.lobsterOwnedGen = ownedGen
	a.oauthMu.Unlock()
	if previous != nil {
		previous.session.Cancel()
		if previous.finish != nil {
			previous.finish()
		}
	}
	if err := parent.Err(); err != nil {
		a.oauthMu.Lock()
		if a.lobsterLogin == flow {
			a.lobsterLogin = nil
		}
		a.oauthMu.Unlock()
		session.Cancel()
		finish()
		a.clearLobsterOwnedGen(ownedGen)
		return LobsterAIOAuthInfo{}, fmt.Errorf("LobsterAI 登录已取消或超时")
	}
	info := LobsterAIOAuthInfo{AuthURL: session.LoginURL}
	if err := browser.OpenURL(session.LoginURL); err != nil {
		log.Printf("[lobsterai] browser open failed: %s", strings.TrimSpace(browserOpenReason(err)))
	} else {
		info.BrowserOpened = true
	}
	return info, nil
}

// WaitLobsterAIOAuth blocks until the browser redirect lands and the token
// exchange completes or the login is cancelled.
func (a *App) WaitLobsterAIOAuth() (string, error) {
	a.oauthMu.Lock()
	flow := a.lobsterLogin
	a.lobsterLogin = nil
	a.oauthMu.Unlock()
	if flow == nil || flow.ctx == nil || flow.session == nil || flow.finish == nil || flow.claim == nil {
		return "", fmt.Errorf("没有正在进行的 LobsterAI 登录")
	}
	defer func() {
		flow.session.Cancel()
		flow.finish()
		a.clearLobsterOwnedGen(flow.generation)
	}()
	code, err := flow.session.Wait(flow.ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("LobsterAI 登录已取消或超时")
		}
		return "", fmt.Errorf("LobsterAI 登录失败: %w", err)
	}
	token, err := flow.session.Resolve(flow.ctx, code)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("LobsterAI 登录已取消或超时")
		}
		return "", fmt.Errorf("LobsterAI 登录失败: %w", err)
	}
	if err := flow.claim(func() error {
		return a.saveLobsterAILogin(token)
	}); err != nil {
		return "", err
	}
	return a.oauthLoginSuccessMessage(lobsterai.Name, lobsterai.Name+" 登录成功")
}

// CancelLobsterAIOAuth stops an in-progress LobsterAI login.
func (a *App) CancelLobsterAIOAuth() {
	a.oauthMu.Lock()
	flow := a.lobsterLogin
	owns := a.lobsterOwnedGen != 0 && a.lobsterOwnedGen == a.oauthGeneration
	if flow != nil && flow.generation != a.oauthGeneration {
		flow = nil
	} else if flow != nil {
		a.lobsterLogin = nil
	}
	var cancel context.CancelFunc
	if owns && flow == nil && a.oauthCancel != nil {
		a.oauthGeneration++
		cancel = a.oauthCancel
		a.oauthCancel = nil
		a.lobsterOwnedGen = 0
	} else if owns {
		a.lobsterOwnedGen = 0
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

func (a *App) clearLobsterOwnedGen(gen uint64) {
	a.oauthMu.Lock()
	if a.lobsterOwnedGen == gen {
		a.lobsterOwnedGen = 0
	}
	a.oauthMu.Unlock()
}

// saveLobsterAILogin persists a completed LobsterAI login into the provider
// snapshot and the credential store.
func (a *App) saveLobsterAILogin(token *lobsterai.Token) error {
	if token == nil || strings.TrimSpace(token.AccessToken) == "" {
		return fmt.Errorf("%s 登录未返回访问令牌", lobsterai.Name)
	}
	data := a.GetMaclawLLMProviders()
	for i, p := range data.Providers {
		if !lobsterai.IsProviderName(p.Name) || !normalizeMaclawLLMAuthTypeKind(p.AuthType).IsOAuth() {
			continue
		}
		data.Providers[i].URL = lobsterai.APIBase
		data.Providers[i].Protocol = "openai"
		data.Providers[i].AuthType = "oauth"
		if strings.TrimSpace(data.Providers[i].Model) == "" {
			data.Providers[i].Model = lobsterai.DefaultModel
		}
		if data.Providers[i].ContextLength <= 0 {
			data.Providers[i].ContextLength = lobsterai.DefaultContextLength
		}
		data.Providers[i].Key = token.AccessToken
		data.Providers[i].OAuthAccessToken = token.AccessToken
		data.Providers[i].RefreshToken = token.RefreshToken
		data.Providers[i].TokenExpiresAt = token.ExpiresAt
		// A fresh login has not passed the post-login model probe yet.
		data.Providers[i].ConnectionTestPassed = false
		if err := a.SaveMaclawLLMProviders(data.Providers, lobsterai.Name); err != nil {
			return fmt.Errorf("保存 %s 登录配置失败: %w", lobsterai.Name, err)
		}
		if a.credentialStore != nil {
			stored := &oauth.StoredCredential{
				Type:           "oauth",
				AccessToken:    token.AccessToken,
				RawAccessToken: token.AccessToken,
				RefreshToken:   token.RefreshToken,
				ExpiresAt:      token.ExpiresAt,
				UserID:         token.UserID,
				Email:          token.Nickname,
				UUID:           token.UUID,
				FirstKeyfrom:   token.FirstKeyfrom,
			}
			if err := a.credentialStore.Modify(lobsterai.StoreID, func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
				return stored, nil
			}); err != nil {
				log.Printf("[lobsterai] credential store save failed: %v", err)
			}
		}
		return nil
	}
	return fmt.Errorf("未找到 %s provider", lobsterai.Name)
}
