package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
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
	a.mu.Lock()
	previous := a.devinLogin
	if previous != nil {
		previous.superseded = true
	}
	a.devinLogin = flow
	a.mu.Unlock()
	if previous != nil && previous.finish != nil {
		previous.finish()
	}
	info := DevinLoginInfo{AuthURL: listener.AuthURL(), TokenPage: windsurf.TokenPage}
	opened, openErr := openDevinLoginURL(a.ctx, listener.AuthURL())
	if openErr != nil {
		log.Printf("[codexproxy] devin browser open failed: %s", browserOpenReason(openErr))
	} else if ctx.Err() == nil {
		info.BrowserOpened = opened
	}
	if ctx.Err() != nil {
		a.dropDevinLogin(flow)
		return DevinLoginInfo{}, a.devinLoginStopError(flow)
	}
	return info, nil
}

// WaitDevinLogin blocks until the browser login finishes, then stores the session.
func (a *App) WaitDevinLogin() (Status, error) {
	a.mu.Lock()
	flow := a.devinLogin
	a.mu.Unlock()
	if flow == nil || flow.listener == nil || flow.finish == nil {
		return Status{}, fmt.Errorf("没有正在进行的 Devin 登录")
	}
	defer a.finishDevinLogin(flow)
	token, err := flow.listener.Wait(flow.ctx)
	if err != nil {
		if a.devinLoginSuperseded(flow) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, windsurf.ErrLoginCancelled) {
			return Status{}, a.devinLoginStopError(flow)
		}
		return Status{}, err
	}
	// The browser already handed back a one-time code. Finish exchanging it
	// even if the user hits cancel while that request is in flight.
	exchangeCtx, exchangeCancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer exchangeCancel()
	session, err := windsurf.ResolveToken(exchangeCtx, token)
	if err != nil {
		return Status{}, err
	}
	status, saved, err := a.saveDevinLogin(flow, session)
	if err != nil {
		return Status{}, err
	}
	if !saved {
		return Status{}, fmt.Errorf("Devin 登录已取消或被新的登录替代")
	}
	return status, nil
}

// CompleteDevinLoginWithToken accepts the backup token shown by Devin or Windsurf.
// A pasted callback URL contributes only its auth parameter.
func (a *App) CompleteDevinLoginWithToken(token string) (Status, error) {
	token, err := windsurf.TokenFromPaste(token)
	if err != nil {
		return Status{}, err
	}
	a.mu.Lock()
	inflight := a.devinLogin
	a.mu.Unlock()
	session, err := windsurf.ResolveToken(context.Background(), token)
	if err != nil {
		return Status{}, err
	}
	// Only the browser login that was already running when the paste started
	// is stopped. A nil inflight must not be read as "whatever is current":
	// a login opened while this exchange ran has to keep waiting.
	if inflight != nil {
		a.supersedeDevinLogin(inflight)
	}
	status, _, err := a.saveDevinLogin(nil, session)
	return status, err
}

// CancelDevinLogin stops an in-progress browser login.
func (a *App) CancelDevinLogin() {
	a.cancelDevinLogin()
}

// supersedeDevinLogin marks a browser login so its late exchange cannot
// overwrite a newer credential. A nil flow marks whatever is in progress.
func (a *App) supersedeDevinLogin(flow *devinLoginFlow) {
	a.mu.Lock()
	if flow == nil {
		flow = a.devinLogin
	}
	if flow == nil || (a.devinLogin != nil && a.devinLogin != flow) {
		a.mu.Unlock()
		return
	}
	flow.superseded = true
	if a.devinLogin == flow {
		a.devinLogin = nil
	}
	a.mu.Unlock()
	if flow.finish != nil {
		flow.finish()
	}
}

func (a *App) cancelDevinLogin() {
	a.mu.Lock()
	flow := a.devinLogin
	a.devinLogin = nil
	a.mu.Unlock()
	if flow != nil && flow.finish != nil {
		flow.finish()
	}
}

func (a *App) devinLoginSuperseded(flow *devinLoginFlow) bool {
	if flow == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return flow.superseded
}

// devinLoginStopError tells a replaced login apart from one the user cancelled.
// Pasting a token while the browser is still opening replaces the login; that
// must not be reported as a timeout.
func (a *App) devinLoginStopError(flow *devinLoginFlow) error {
	if a.devinLoginSuperseded(flow) {
		return fmt.Errorf("Devin 登录已取消或被新的登录替代")
	}
	return fmt.Errorf("Devin 登录已取消或超时")
}

func (a *App) dropDevinLogin(flow *devinLoginFlow) {
	a.mu.Lock()
	if a.devinLogin == flow {
		a.devinLogin = nil
	}
	a.mu.Unlock()
	if flow != nil && flow.finish != nil {
		flow.finish()
	}
}

func (a *App) finishDevinLogin(flow *devinLoginFlow) {
	if flow != nil && flow.finish != nil {
		flow.finish()
	}
	a.mu.Lock()
	if a.devinLogin == flow {
		a.devinLogin = nil
	}
	a.mu.Unlock()
}

func (a *App) saveDevinLogin(flow *devinLoginFlow, session windsurf.Session) (Status, bool, error) {
	if strings.TrimSpace(session.APIKey) == "" {
		return Status{}, false, fmt.Errorf("Devin 登录未返回访问令牌")
	}
	a.devinSaveMu.Lock()
	defer a.devinSaveMu.Unlock()
	if flow != nil && a.devinLoginSuperseded(flow) {
		return Status{}, false, nil
	}
	status, err := a.applyDevinSession(session)
	if err != nil {
		return Status{}, false, err
	}
	return status, true, nil
}

func (a *App) applyDevinSession(session windsurf.Session) (Status, error) {
	if strings.TrimSpace(session.APIKey) == "" {
		return Status{}, fmt.Errorf("Devin 登录未返回访问令牌")
	}
	a.setRelaunchAfterAuth(true)
	host := windsurf.CanonicalAPIServer(session.APIKey, session.APIServerURL)
	status, err := a.applyOAuthResultLabeled(AuthModeDevin, host, &oauth.TokenResult{
		AccessToken:    session.APIKey,
		RawAccessToken: session.APIKey,
	}, session.Name, true)
	if err != nil || !status.RelaunchScheduled {
		a.setRelaunchAfterAuth(false)
	}
	return status, err
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

// persistDevinAccess stores a minted session token and the host that accepted
// the current key. It returns the token that should be treated as current.
func (a *App) persistDevinAccess(accessToken string) string {
	if minted, ok := windsurf.MintedSession(accessToken); ok && minted != accessToken {
		a.persistDevinCredential(accessToken, minted, windsurf.SelfServeAPIServer)
		return minted
	}
	a.persistDevinHost(accessToken)
	return accessToken
}

// persistDevinCredential replaces the saved API key with the session token
// the Devin API accepted. Callers must not hold settingsMu.
func (a *App) persistDevinCredential(oldKey, newKey, host string) {
	newKey = strings.TrimSpace(newKey)
	host = strings.TrimRight(strings.TrimSpace(host), "/")
	if newKey == "" || host == "" {
		return
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	current, err := loadSettings()
	if err != nil || current.ActiveAuthMode != AuthModeDevin || current.AccessToken != oldKey {
		return
	}
	if current.AccessToken == newKey && storedSettingsBaseURL() == host {
		return
	}
	current.AccessToken = newKey
	current.BaseURL = host
	_ = writeSettings(current)
}

// persistDevinHost stores the official host that accepted this key.
// Callers must not hold settingsMu. A codeium URL that rejected the key is
// replaced so the next launch and the settings screen use the working host.
func (a *App) persistDevinHost(accessToken string) {
	host := windsurf.NotedAPIServer(accessToken)
	if host == "" {
		return
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	current, err := loadSettings()
	if err != nil || current.ActiveAuthMode != AuthModeDevin || current.AccessToken != accessToken {
		return
	}
	if storedSettingsBaseURL() == host {
		return
	}
	current.BaseURL = host
	_ = writeSettings(current)
}

func storedSettingsBaseURL() string {
	path, err := settingsPath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var raw struct {
		BaseURL string `json:"base_url"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(raw.BaseURL), "/")
}

func fetchDevinModels(baseURL, apiKey string) ([]ModelOption, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	models, err := windsurf.ListModels(ctx, baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	options := make([]ModelOption, 0, len(models))
	for _, model := range models {
		options = append(options, ModelOption{ID: model.ID, Name: model.Name})
	}
	return normalizeModelOptions(options), nil
}
