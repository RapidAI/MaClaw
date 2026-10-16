package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/kimicode"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// kimiRefreshTimeout bounds one silent refresh so a hung Kimi endpoint cannot
// stall proxy startup.
const kimiRefreshTimeout = 30 * time.Second

// kimiRefreshSkew is how early a token is refreshed before it actually expires.
const kimiRefreshSkew = 5 * time.Minute

var (
	// kimiTokenExpiry tracks when the active Kimi access token lapses. The
	// profile is persisted without an expiry field, so the deadline is kept in
	// memory for the lifetime of the process.
	kimiTokenExpiryMu sync.RWMutex
	kimiTokenExpiry   time.Time
)

func noteKimiTokenExpiry(token kimicode.Token) {
	kimiTokenExpiryMu.Lock()
	defer kimiTokenExpiryMu.Unlock()
	if token.ExpiresIn <= 0 {
		kimiTokenExpiry = time.Time{}
		return
	}
	kimiTokenExpiry = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
}

func kimiTokenExpiring() bool {
	kimiTokenExpiryMu.RLock()
	defer kimiTokenExpiryMu.RUnlock()
	if kimiTokenExpiry.IsZero() {
		return false
	}
	return time.Now().Add(kimiRefreshSkew).After(kimiTokenExpiry)
}

// refreshKimiTokenIfLoggedIn refreshes an in-use Kimi token that is close to
// expiry. The rotated credential is returned so the caller can persist it; the
// in-memory expiry marker is always updated when the token actually rotates.
func (a *App) refreshKimiTokenIfLoggedIn(s Settings) (Settings, bool) {
	if s.ActiveAuthMode != AuthModeKimiWeb || !a.isRunning() {
		return s, false
	}
	if !kimiTokenExpiring() {
		return s, false
	}
	refreshed := a.refreshKimiToken(s)
	if refreshed.AccessToken == s.AccessToken {
		return s, false
	}
	return refreshed, true
}

// KimiDeviceInfo is the pending browser approval returned to the frontend. The
// user code and URL are shown right away because the user cannot finish the
// browser login until they see them.
type KimiDeviceInfo struct {
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	BrowserOpened           bool   `json:"browser_opened"`
}

// kimiDeviceLogin is one in-flight Kimi Code device approval.
type kimiDeviceLogin struct {
	ctx        context.Context
	deviceCode string
	interval   int
	baseURL    string
	oauthHost  string
	finish     func()
}

// StartKimiWebLogin begins Kimi Code's browser device login and opens the
// approval page. The frontend shows the user code, then calls WaitKimiWebLogin.
func (a *App) StartKimiWebLogin() (KimiDeviceInfo, error) {
	a.cancelKimiLogin()
	parent, finish := context.WithTimeout(context.Background(), kimicode.MaxLoginLifetime())
	oauthHost := kimicode.OAuthHost()
	device, err := kimicode.RequestDeviceAuthorizationAt(parent, oauthHost)
	if err != nil {
		finish()
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return KimiDeviceInfo{}, fmt.Errorf("Kimi Code 登录已取消或超时")
		}
		return KimiDeviceInfo{}, fmt.Errorf("Kimi Code 设备码请求失败: %w", err)
	}
	approvalURL := device.VerificationURIComplete
	if approvalURL == "" {
		approvalURL = device.VerificationURI
	}
	if !kimicode.IsApprovalURL(approvalURL) {
		finish()
		return KimiDeviceInfo{}, fmt.Errorf("Kimi Code 授权地址无效")
	}
	if parent.Err() != nil {
		finish()
		return KimiDeviceInfo{}, fmt.Errorf("Kimi Code 登录已取消或超时")
	}
	waitCtx, waitCancel := context.WithTimeout(parent, kimicode.LoginLifetime(device.ExpiresIn))
	flow := &kimiDeviceLogin{
		ctx:        waitCtx,
		deviceCode: device.DeviceCode,
		interval:   device.Interval,
		baseURL:    kimicode.BaseURLForOAuthHost(oauthHost),
		oauthHost:  oauthHost,
		finish: func() {
			waitCancel()
			finish()
		},
	}
	a.mu.Lock()
	previous := a.kimiLogin
	a.kimiLogin = flow
	a.mu.Unlock()
	if previous != nil && previous.finish != nil {
		previous.finish()
	}

	info := KimiDeviceInfo{
		UserCode:                device.UserCode,
		VerificationURI:         device.VerificationURI,
		VerificationURIComplete: device.VerificationURIComplete,
	}
	// The approval URL carries the user code, so log only the failure reason.
	if err := openExternalURL(a.ctx, approvalURL); err != nil {
		logKimiBrowserOpen(err)
	} else if parent.Err() == nil {
		info.BrowserOpened = true
	}
	if parent.Err() != nil {
		// Cancel may already have dropped this flow; do not cancel whatever
		// login replaced it.
		a.dropKimiLogin(flow)
		return KimiDeviceInfo{}, fmt.Errorf("Kimi Code 登录已取消或超时")
	}
	return info, nil
}

func (a *App) dropKimiLogin(flow *kimiDeviceLogin) {
	a.mu.Lock()
	if a.kimiLogin == flow {
		a.kimiLogin = nil
	}
	a.mu.Unlock()
	if flow.finish != nil {
		flow.finish()
	}
}

func logKimiBrowserOpen(err error) {
	if err == nil {
		return
	}
	log.Printf("[codexproxy] kimi browser open failed: %s", browserOpenReason(err))
}

func browserOpenReason(err error) string {
	text := err.Error()
	if idx := strings.Index(text, "http"); idx >= 0 {
		return strings.TrimSpace(text[:idx])
	}
	return text
}

// WaitKimiWebLogin blocks until the browser approval finishes or is cancelled.
func (a *App) WaitKimiWebLogin() (Status, error) {
	a.mu.Lock()
	flow := a.kimiLogin
	a.kimiLogin = nil
	a.mu.Unlock()
	if flow == nil || flow.ctx == nil || flow.deviceCode == "" || flow.finish == nil {
		return Status{}, fmt.Errorf("没有正在进行的 Kimi Code 登录")
	}
	defer flow.finish()

	token, err := kimicode.PollUntilAuthorizedAt(flow.ctx, flow.deviceCode, flow.interval, flow.oauthHost)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Status{}, fmt.Errorf("Kimi Code 登录已取消或超时")
		}
		return Status{}, fmt.Errorf("Kimi Code 登录失败: %w", err)
	}
	return a.applyKimiToken(token, flow.baseURL)
}

// CancelKimiWebLogin cancels an in-progress Kimi Code device login. A login
// that has not reached Wait is dropped so it cannot be polled later.
func (a *App) CancelKimiWebLogin() {
	a.cancelKimiLogin()
}

func (a *App) cancelKimiLogin() {
	a.mu.Lock()
	flow := a.kimiLogin
	a.kimiLogin = nil
	a.mu.Unlock()
	if flow != nil && flow.finish != nil {
		// This flow's own finish only cancels its context, so a newer login
		// that already replaced it stays up.
		flow.finish()
	}
}

// applyKimiToken persists a Kimi Code access token as the active profile.
func (a *App) applyKimiToken(token kimicode.Token, baseURL string) (Status, error) {
	if strings.TrimSpace(token.AccessToken) == "" {
		return Status{}, fmt.Errorf("Kimi Code 登录未返回访问令牌")
	}
	result := &oauth.TokenResult{
		AccessToken:    token.AccessToken,
		RawAccessToken: token.AccessToken,
		RefreshToken:   token.RefreshToken,
		ExpiresIn:      token.ExpiresIn,
	}
	// A Kimi login always replaces the upstream, so it needs a restart to take
	// effect. relaunchAfterAuth is otherwise only set by SwitchAuthMode, which
	// would leave this login with a flag left over from the mode selection that
	// preceded it — and that stale flag can fire the restart at any later
	// moment, tearing the window down while the UI is still mid-request.
	a.setRelaunchAfterAuth(true)
	status, err := a.applyOAuthResult(AuthModeKimiWeb, kimicode.CanonicalBaseURL(baseURL), result)
	if err == nil {
		noteKimiTokenExpiry(token)
	}
	return status, err
}

// refreshKimiToken exchanges the saved refresh token for a fresh access token.
// It is a no-op for every mode but Kimi, and for a login that has no refresh
// token. The rotated refresh token is persisted with the new access token.
func (a *App) refreshKimiToken(s Settings) Settings {
	if s.ActiveAuthMode != AuthModeKimiWeb {
		return s
	}
	refreshToken := strings.TrimSpace(s.AuthProfiles[AuthModeKimiWeb].RefreshToken)
	if refreshToken == "" {
		return s
	}
	ctx, cancel := context.WithTimeout(context.Background(), kimiRefreshTimeout)
	defer cancel()
	token, err := kimicode.Refresh(ctx, refreshToken, s.BaseURL)
	if err != nil {
		// A rejected refresh needs a fresh browser login; keep the old
		// credential so the UI still shows the profile as configured.
		log.Printf("[codexproxy] kimi token refresh failed: %v", err)
		return s
	}
	s.AccessToken = token.AccessToken
	profile := s.AuthProfiles[AuthModeKimiWeb]
	profile.AccessToken = token.AccessToken
	profile.APIKey = token.AccessToken
	if strings.TrimSpace(token.RefreshToken) != "" {
		profile.RefreshToken = token.RefreshToken
	}
	s.AuthProfiles[AuthModeKimiWeb] = profile
	noteKimiTokenExpiry(token)
	return s
}