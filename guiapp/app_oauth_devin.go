package guiapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/windsurf"

	"github.com/pkg/browser"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// DevinLoginInfo is returned when the Devin account page has been opened.
type DevinLoginInfo struct {
	AuthURL       string `json:"auth_url"`
	TokenPage     string `json:"token_page"`
	BrowserOpened bool   `json:"browser_opened"`
}

type devinLoginFlow struct {
	ctx        context.Context
	listener   *windsurf.LoginListener
	finish     func()
	superseded bool
}

// StartDevinLogin opens the same Devin account page the installed IDE uses.
// It returns as soon as the page is opened. WaitDevinLogin receives the callback.
func (a *App) StartDevinLogin() (DevinLoginInfo, error) {
	a.supersedeDevinLogin(nil)
	listener, err := windsurf.StartLogin()
	if err != nil {
		return DevinLoginInfo{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	flow := &devinLoginFlow{
		ctx:      ctx,
		listener: listener,
		finish: func() {
			cancel()
			listener.Close()
		},
	}
	a.oauthMu.Lock()
	previous := a.devinLogin
	if previous != nil {
		previous.superseded = true
	}
	a.devinLogin = flow
	a.oauthMu.Unlock()
	if previous != nil && previous.finish != nil {
		previous.finish()
	}
	info := DevinLoginInfo{AuthURL: listener.AuthURL(), TokenPage: windsurf.TokenPage}
	opened, openErr := openDevinLoginURL(a.ctx, listener.AuthURL())
	if openErr != nil {
		log.Printf("[Devin] browser open failed: %s", browserOpenReason(openErr))
	} else if ctx.Err() == nil {
		info.BrowserOpened = opened
	}
	if ctx.Err() != nil {
		a.dropDevinLogin(flow)
		return DevinLoginInfo{}, fmt.Errorf("Devin 登录已取消或超时")
	}
	return info, nil
}

// WaitDevinLogin blocks until the browser login finishes, then stores the session.
func (a *App) WaitDevinLogin() (string, error) {
	a.oauthMu.Lock()
	flow := a.devinLogin
	a.oauthMu.Unlock()
	if flow == nil || flow.listener == nil || flow.finish == nil {
		return "", fmt.Errorf("没有正在进行的 Devin 登录")
	}
	defer a.finishDevinLogin(flow)
	token, err := flow.listener.Wait(flow.ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, windsurf.ErrLoginCancelled) {
			return "", fmt.Errorf("Devin 登录已取消或超时")
		}
		return "", err
	}
	// The browser already handed back a one-time code. Finish exchanging it
	// even if the user hits cancel while that request is in flight.
	exchangeCtx, exchangeCancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer exchangeCancel()
	session, err := windsurf.ResolveToken(exchangeCtx, token)
	if err != nil {
		return "", err
	}
	saved, err := a.saveDevinLogin(flow, session)
	if err != nil {
		return "", err
	}
	if !saved {
		return "", fmt.Errorf("Devin 登录已取消或被新的登录替代")
	}
	return "Devin 登录成功", nil
}

// CompleteDevinLoginWithToken accepts the backup token shown by Devin or Windsurf.
func (a *App) CompleteDevinLoginWithToken(token string) (string, error) {
	token, err := windsurf.TokenFromPaste(token)
	if err != nil {
		return "", err
	}
	a.oauthMu.Lock()
	inflight := a.devinLogin
	a.oauthMu.Unlock()
	session, err := windsurf.ResolveToken(context.Background(), token)
	if err != nil {
		return "", err
	}
	// Only the browser login that was already running when the paste started
	// is stopped. A newer login opened while this exchange ran keeps waiting.
	a.supersedeDevinLogin(inflight)
	if _, err := a.saveDevinLogin(nil, session); err != nil {
		return "", err
	}
	return "Devin 登录成功", nil
}

// supersedeDevinLogin marks a browser login so its late exchange cannot
// overwrite a newer credential. A nil flow marks whatever is in progress.
func (a *App) supersedeDevinLogin(flow *devinLoginFlow) {
	a.oauthMu.Lock()
	if flow == nil {
		flow = a.devinLogin
	}
	if flow == nil || (a.devinLogin != nil && a.devinLogin != flow) {
		a.oauthMu.Unlock()
		return
	}
	flow.superseded = true
	if a.devinLogin == flow {
		a.devinLogin = nil
	}
	a.oauthMu.Unlock()
	if flow.finish != nil {
		flow.finish()
	}
}

// CancelDevinLogin stops an in-progress browser login.
func (a *App) CancelDevinLogin() {
	a.cancelDevinLogin()
}

func (a *App) cancelDevinLogin() {
	a.oauthMu.Lock()
	flow := a.devinLogin
	a.devinLogin = nil
	a.oauthMu.Unlock()
	if flow != nil && flow.finish != nil {
		flow.finish()
	}
}

func (a *App) devinLoginSuperseded(flow *devinLoginFlow) bool {
	if flow == nil {
		return false
	}
	a.oauthMu.Lock()
	defer a.oauthMu.Unlock()
	return flow.superseded
}

func (a *App) dropDevinLogin(flow *devinLoginFlow) {
	a.oauthMu.Lock()
	if a.devinLogin == flow {
		a.devinLogin = nil
	}
	a.oauthMu.Unlock()
	if flow != nil && flow.finish != nil {
		flow.finish()
	}
}

func (a *App) finishDevinLogin(flow *devinLoginFlow) {
	if flow != nil && flow.finish != nil {
		flow.finish()
	}
	a.oauthMu.Lock()
	if a.devinLogin == flow {
		a.devinLogin = nil
	}
	a.oauthMu.Unlock()
}

func (a *App) saveDevinLogin(flow *devinLoginFlow, session windsurf.Session) (bool, error) {
	apiKey := strings.TrimSpace(session.APIKey)
	if apiKey == "" {
		return false, fmt.Errorf("Devin 登录未返回访问令牌")
	}
	a.devinSaveMu.Lock()
	defer a.devinSaveMu.Unlock()
	if flow != nil && a.devinLoginSuperseded(flow) {
		return false, nil
	}
	data := a.GetMaclawLLMProviders()
	for i, provider := range data.Providers {
		if provider.Name != "Devin" {
			continue
		}
		data.Providers[i].AuthType = "oauth"
		data.Providers[i].Protocol = "openai"
		data.Providers[i].URL = windsurf.CanonicalAPIServer(apiKey, session.APIServerURL)
		data.Providers[i].Key = apiKey
		data.Providers[i].OAuthAccessToken = apiKey
		data.Providers[i].ConnectionTestPassed = false
		// An empty model cannot answer. Leave the active provider alone until
		// one is chosen, so the login does not replace a working chat model.
		current := strings.TrimSpace(data.Current)
		if strings.TrimSpace(data.Providers[i].Model) != "" || current == "" || current == "Devin" {
			current = "Devin"
		}
		if err := a.SaveMaclawLLMProviders(data.Providers, current); err != nil {
			return false, fmt.Errorf("保存 Devin 登录配置失败: %w", err)
		}
		return true, nil
	}
	return false, fmt.Errorf("未找到 Devin provider")
}

// openDevinLoginURL reports whether the system browser accepted the page.
// A Wails fallback is still attempted, but that path does not count as opened:
// the caller must show the address so the user can open it by hand.
func openDevinLoginURL(wailsCtx context.Context, raw string) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, fmt.Errorf("empty url")
	}
	if err := browser.OpenURL(raw); err != nil {
		if wailsCtx != nil {
			runtime.BrowserOpenURL(wailsCtx, raw)
		}
		return false, err
	}
	return true, nil
}
