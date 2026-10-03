package workbuddy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/browser"
)

const (
	refreshLeeway = 5 * time.Minute
	loginLifetime = 5 * time.Minute
)

// ErrLoginTimeout means the browser login session expired before completion.
var ErrLoginTimeout = errors.New("登录会话已过期，请重新发起登录")

type statusError struct {
	Status int
}

func (e *statusError) Error() string {
	if e == nil {
		return "上游 HTTP 错误"
	}
	return fmt.Sprintf("上游 HTTP %d", e.Status)
}

// pendingHTTPStatus reports statuses that mean "login is not finished yet"
// or a transient upstream failure. Other 4xx responses are configuration errors.
func pendingHTTPStatus(status int) bool {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
		return true
	default:
		return status >= 500
	}
}

// ProxyFunc selects a proxy for one upstream request. Nil follows the environment.
type ProxyFunc func(*http.Request) (*url.URL, error)

// AccountCredential is one signed-in WorkBuddy or CodeBuddy account.
type AccountCredential struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64
	Domain       string
	UserID       string
	EnterpriseID string
	Nickname     string
}

// NeedsRefresh reports whether the access token should be renewed.
func (c AccountCredential) NeedsRefresh(now time.Time) bool {
	if c.ExpiresAt <= 0 {
		return false
	}
	return !now.Before(time.Unix(c.ExpiresAt, 0).Add(-refreshLeeway))
}

type apiError struct {
	Code int
	Msg  string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("上游返回 code=%d msg=%s", e.Code, e.Msg)
}

type loginSession struct {
	state   string
	authURL string
	expires time.Time
	client  *http.Client
	close   func()
	profile Profile
}

// LoginSession is one in-flight browser login. The same session must poll
// until the account finishes, because the upstream ties the state to cookies.
type LoginSession = loginSession

// BeginLogin starts a browser login and returns the authorization URL.
// It does not open a browser; the caller shows AuthURL to the operator.
func BeginLogin(ctx context.Context, profile Profile, proxy ProxyFunc) (*LoginSession, error) {
	return startLogin(ctx, profile, proxy)
}

// AuthURL is the page the operator must open to approve the login.
func (s *LoginSession) AuthURL() string {
	if s == nil {
		return ""
	}
	return s.authURL
}

// Poll waits once for the upstream to finish the login.
func (s *LoginSession) Poll(ctx context.Context) (*AccountCredential, bool, error) {
	if s == nil {
		return nil, false, errors.New("login session is nil")
	}
	return s.poll(ctx)
}

// Close releases the login HTTP client.
func (s *LoginSession) Close() {
	if s == nil || s.close == nil {
		return
	}
	s.close()
	s.close = nil
}

// RunLogin opens the official authorization page and waits until the account
// finishes signing in or ctx ends.
func RunLogin(ctx context.Context, profile Profile, proxy ProxyFunc) (*AccountCredential, error) {
	session, err := startLogin(ctx, profile, proxy)
	if err != nil {
		return nil, err
	}
	defer session.close()
	if err := openBrowser(session.authURL); err != nil {
		return nil, fmt.Errorf("打开登录页失败: %w", err)
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		cred, pending, err := session.poll(ctx)
		if err != nil {
			return nil, err
		}
		if cred != nil {
			return cred, nil
		}
		if !pending {
			return nil, errors.New("登录未完成")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func startLogin(ctx context.Context, profile Profile, proxy ProxyFunc) (*loginSession, error) {
	client, cleanup, err := newLoginClient(proxy)
	if err != nil {
		return nil, err
	}
	owned := false
	defer func() {
		if !owned {
			cleanup()
		}
	}()
	loginCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	raw, err := call(loginCtx, client, profile, http.MethodPost,
		profile.endpoint("/v2/plugin/auth/state?platform="+url.QueryEscape(Platform)),
		nil, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, fmt.Errorf("发起登录失败: %w", err)
	}
	var st struct {
		State   string `json:"state"`
		AuthURL string `json:"authUrl"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("登录响应解析失败: %w", err)
	}
	if st.State == "" || st.AuthURL == "" {
		return nil, errors.New("上游未返回登录链接")
	}
	owned = true
	return &loginSession{
		state:   st.State,
		authURL: st.AuthURL,
		expires: time.Now().Add(loginLifetime),
		client:  client,
		close:   cleanup,
		profile: profile,
	}, nil
}

func (s *loginSession) poll(ctx context.Context) (*AccountCredential, bool, error) {
	if time.Now().After(s.expires) {
		return nil, false, ErrLoginTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	raw, err := call(reqCtx, s.client, s.profile, http.MethodGet,
		s.profile.endpoint("/v2/plugin/auth/token?state="+url.QueryEscape(s.state)),
		nil, nil)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, err
		}
		var apiErr *apiError
		if errors.As(err, &apiErr) {
			return nil, true, nil
		}
		var statusErr *statusError
		if errors.As(err, &statusErr) && !pendingHTTPStatus(statusErr.Status) {
			return nil, false, err
		}
		return nil, true, nil
	}
	var tok tokenPayload
	if err := json.Unmarshal(raw, &tok); err != nil || strings.TrimSpace(tok.AccessToken) == "" {
		return nil, true, nil
	}
	acctCtx, acctCancel := context.WithTimeout(ctx, 15*time.Second)
	defer acctCancel()
	var acct accountPayload
	acctRaw, errAcct := call(acctCtx, s.client, s.profile, http.MethodGet,
		s.profile.endpoint("/v2/plugin/login/account?state="+url.QueryEscape(s.state)),
		func(r *http.Request) {
			commonHeaders(r, s.profile)
			r.Header.Set("Authorization", "Bearer "+tok.AccessToken)
		}, nil)
	if errAcct == nil {
		_ = json.Unmarshal(acctRaw, &acct)
	}
	cred := credentialFromToken(tok, acct)
	return &cred, false, nil
}

// Refresh exchanges a refresh token for a new access token.
func Refresh(ctx context.Context, profile Profile, cred AccountCredential, proxy ProxyFunc) (AccountCredential, error) {
	if strings.TrimSpace(cred.RefreshToken) == "" {
		return cred, errors.New("凭据缺少 refreshToken，请重新登录")
	}
	client, cleanup, err := newAPIClient(proxy)
	if err != nil {
		return cred, err
	}
	defer cleanup()
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	raw, err := call(reqCtx, client, profile, http.MethodPost,
		profile.endpoint("/v2/plugin/auth/token/refresh"),
		func(r *http.Request) {
			commonHeaders(r, profile)
			r.Header.Set("X-Refresh-Token", cred.RefreshToken)
			if cred.EnterpriseID != "" {
				r.Header.Set("X-Enterprise-Id", cred.EnterpriseID)
			}
			r.Header.Set("X-Auth-Refresh-Source", profile.ID)
		}, nil)
	if err != nil {
		return cred, fmt.Errorf("刷新令牌失败: %w", err)
	}
	var tok tokenPayload
	if err := json.Unmarshal(raw, &tok); err != nil || strings.TrimSpace(tok.AccessToken) == "" {
		return cred, errors.New("刷新令牌失败: 上游未返回 accessToken")
	}
	cred.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		cred.RefreshToken = tok.RefreshToken
	}
	if tok.Domain != "" {
		cred.Domain = tok.Domain
	}
	cred.ExpiresAt = expiryUnix(tok.ExpiresIn)
	return cred, nil
}

type tokenPayload struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int64  `json:"expiresIn"`
	Domain       string `json:"domain"`
}

type accountPayload struct {
	UID          string `json:"uid"`
	EnterpriseID string `json:"enterpriseId"`
	Nickname     string `json:"nickname"`
}

func credentialFromToken(tok tokenPayload, acct accountPayload) AccountCredential {
	cred := AccountCredential{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    expiryUnix(tok.ExpiresIn),
		Domain:       tok.Domain,
		UserID:       strings.TrimSpace(acct.UID),
		EnterpriseID: strings.TrimSpace(acct.EnterpriseID),
		Nickname:     strings.TrimSpace(acct.Nickname),
	}
	return cred
}

func expiryUnix(expiresIn int64) int64 {
	if expiresIn <= 0 {
		return time.Now().Add(2 * time.Hour).Unix()
	}
	return time.Now().Add(time.Duration(expiresIn) * time.Second).Unix()
}

func (p Profile) endpoint(path string) string {
	return strings.TrimRight(p.APIRoot, "/") + path
}

func newLoginClient(proxy ProxyFunc) (*http.Client, func(), error) {
	client, cleanup, err := newAPIClient(proxy)
	if err != nil {
		return nil, nil, err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	client.Jar = jar
	return client, cleanup, nil
}

func newAPIClient(proxy ProxyFunc) (*http.Client, func(), error) {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   20 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
	}
	if proxy != nil {
		transport.Proxy = proxy
	}
	return &http.Client{Transport: transport}, transport.CloseIdleConnections, nil
}

// FetchModels reads the live model catalog from /v3/config.
func FetchModels(ctx context.Context, profile Profile, cred AccountCredential, proxy ProxyFunc) ([]UpstreamModel, error) {
	if strings.TrimSpace(cred.AccessToken) == "" {
		return nil, errors.New("missing access token")
	}
	client, cleanup, err := newAPIClient(proxy)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	raw, err := call(ctx, client, profile, http.MethodGet, profile.endpoint("/v3/config"), func(r *http.Request) {
		commonHeaders(r, profile)
		r.Header.Set("Authorization", "Bearer "+cred.AccessToken)
		r.Header.Set("X-Product", Product)
		if cred.UserID != "" {
			r.Header.Set("X-User-Id", cred.UserID)
		} else {
			r.Header.Set("X-No-User-Id", "1")
		}
		if cred.EnterpriseID != "" {
			r.Header.Set("X-Enterprise-Id", cred.EnterpriseID)
		} else {
			r.Header.Set("X-No-Enterprise-Id", "1")
		}
		if cred.Domain != "" {
			r.Header.Set("X-Domain", cred.Domain)
		}
		if cred.RefreshToken != "" {
			r.Header.Set("X-Refresh-Token", cred.RefreshToken)
		}
	}, nil)
	if err != nil {
		return nil, err
	}
	var data struct {
		Models []UpstreamModel `json:"models"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return data.Models, nil
}

func commonHeaders(req *http.Request, profile Profile) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Origin", profile.Origin)
	req.Header.Set("Referer", strings.TrimRight(profile.Origin, "/")+"/")
	req.Header.Set("User-Agent", UserAgent)
}

type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func call(ctx context.Context, client *http.Client, profile Profile, method, rawURL string, hdr func(*http.Request), body io.Reader) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	if hdr != nil {
		hdr(req)
	} else {
		commonHeaders(req, profile)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 400 {
		return nil, &statusError{Status: resp.StatusCode}
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("响应解析失败: %w", err)
	}
	if env.Code != 0 {
		return nil, &apiError{Code: env.Code, Msg: env.Msg}
	}
	return env.Data, nil
}

// openBrowser is replaced in tests.
var openBrowser = browser.OpenURL
