package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codegenproxy"
	"github.com/RapidAI/CodeClaw/corelib/qoder"
	"github.com/RapidAI/CodeClaw/corelib/workbuddy"
)

// vendorCredential is the account material a Qoder or WorkBuddy login persists.
type vendorCredential struct {
	Mode         string
	BaseURL      string
	AccessToken  string
	RefreshToken string
	Email        string
	UserID       string
	EnterpriseID string
	Domain       string
	ExpiresAt    int64
	ModelID      string
}

func (a *App) applyVendorLogin(login vendorCredential) (Status, error) {
	if strings.TrimSpace(login.AccessToken) == "" {
		return Status{}, errors.New("登录未返回访问令牌")
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	s, err := loadSettings()
	if err != nil {
		return Status{}, err
	}
	if s.ActiveAuthMode != "" && s.ActiveAuthMode != login.Mode {
		return Status{}, fmt.Errorf("当前登录方式已切换，请在%s下重新登录", authModeLabel(login.Mode))
	}
	snapshotActiveProfile(&s)
	s.ActiveAuthMode = login.Mode
	s.AccessToken = strings.TrimSpace(login.AccessToken)
	s.BaseURL = strings.TrimRight(strings.TrimSpace(login.BaseURL), "/")
	if s.AuthProfiles == nil {
		s.AuthProfiles = map[string]AuthProfile{}
	}
	profile := s.AuthProfiles[login.Mode]
	profile.RefreshToken = strings.TrimSpace(login.RefreshToken)
	profile.UserID = strings.TrimSpace(login.UserID)
	profile.EnterpriseID = strings.TrimSpace(login.EnterpriseID)
	profile.Domain = strings.TrimSpace(login.Domain)
	profile.ExpiresAt = login.ExpiresAt
	s.AuthProfiles[login.Mode] = profile
	if email := strings.TrimSpace(login.Email); email != "" {
		s.Email = email
	}
	// Mode switch fills the built-in default before any credential exists.
	// That placeholder is not a user choice, so a login-supplied model replaces
	// it while the catalog is still empty. A model chosen from a fetched list stays.
	placeholderModel := strings.TrimSpace(s.ModelID) == "" || (len(s.Models) == 0 && strings.TrimSpace(s.ModelID) == defaultAuthModelID(login.Mode))
	if placeholderModel {
		if id := normalizeModelID(login.ModelID); id != "" {
			s.ModelID = id
		} else if def := defaultAuthModelID(login.Mode); def != "" && strings.TrimSpace(s.ModelID) == "" {
			s.ModelID = def
		}
	} else if len(s.Models) > 0 && !modelExistsInList(s.ModelID, s.Models) {
		if def := defaultAuthModelID(login.Mode); def != "" {
			s.ModelID = def
			s.Models = nil
		}
	}
	s = normalizeSettings(s)
	if err := a.applySettingsWithRestart(s); err != nil {
		return Status{}, err
	}
	go a.refreshModelsForActiveMode()
	return a.Status()
}

// refreshVendorCredential renews a Qoder or WorkBuddy access token that is
// inside the renewal window. A missing expiry is left alone so a settings save
// does not call the network. The error is returned without the upstream body
// so a token echoed by the server cannot land in the log.
func refreshVendorCredential(s Settings) (Settings, bool, error) {
	mode := strings.TrimSpace(s.ActiveAuthMode)
	if s.AuthProfiles == nil {
		return s, false, nil
	}
	profile := s.AuthProfiles[mode]
	if strings.TrimSpace(profile.RefreshToken) == "" || !vendorAccessDue(mode, profile.ExpiresAt, time.Now()) {
		return s, false, nil
	}
	switch mode {
	case AuthModeQoderCN, AuthModeQoderGlobal:
		qp, ok := qoderProfileForMode(mode)
		if !ok {
			return s, false, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		token, err := qoder.Refresh(ctx, qp, profile.RefreshToken, qoder.MachineID())
		if err != nil || strings.TrimSpace(token.AccessToken) == "" {
			return s, false, errors.New("Qoder 登录续期失败")
		}
		s.AccessToken = token.AccessToken
		profile.RefreshToken = preferText(token.RefreshToken, profile.RefreshToken)
		profile.UserID = preferText(token.UserID, profile.UserID)
		if token.ExpiresAt > 0 {
			profile.ExpiresAt = token.ExpiresAt
		} else {
			// The server omitted a new expiry. Keep the old near-expiry stamp
			// and every later chat would rotate the token again.
			profile.ExpiresAt = time.Now().Add(time.Hour).Unix()
		}
		if name := strings.TrimSpace(token.UserName); name != "" {
			s.Email = name
		}
	case AuthModeWorkBuddyCN, AuthModeWorkBuddyGlobal:
		wp, ok := workbuddyProfileForMode(mode)
		if !ok {
			return s, false, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		next, err := workbuddy.Refresh(ctx, wp, workbuddy.AccountCredential{
			AccessToken:  s.AccessToken,
			RefreshToken: profile.RefreshToken,
			ExpiresAt:    profile.ExpiresAt,
			Domain:       profile.Domain,
			UserID:       profile.UserID,
			EnterpriseID: profile.EnterpriseID,
		}, nil)
		if err != nil || strings.TrimSpace(next.AccessToken) == "" {
			return s, false, errors.New("WorkBuddy 登录续期失败")
		}
		s.AccessToken = next.AccessToken
		profile.RefreshToken = preferText(next.RefreshToken, profile.RefreshToken)
		profile.UserID = preferText(next.UserID, profile.UserID)
		profile.EnterpriseID = preferText(next.EnterpriseID, profile.EnterpriseID)
		profile.Domain = preferText(next.Domain, profile.Domain)
		if next.ExpiresAt > 0 {
			profile.ExpiresAt = next.ExpiresAt
		} else {
			profile.ExpiresAt = time.Now().Add(2 * time.Hour).Unix()
		}
		if name := strings.TrimSpace(next.Nickname); name != "" {
			s.Email = name
		}
	default:
		return s, false, nil
	}
	s.AuthProfiles[mode] = profile
	return normalizeSettings(s), true, nil
}

func vendorAccessDue(mode string, expiresAt int64, now time.Time) bool {
	if expiresAt <= 0 {
		return false
	}
	leeway := 10 * time.Minute
	if mode == AuthModeWorkBuddyCN || mode == AuthModeWorkBuddyGlobal {
		leeway = 5 * time.Minute
	}
	return !now.Before(time.Unix(expiresAt, 0).Add(-leeway))
}

// rotateVendorCredential persists a near-expiry vendor token and points the
// running proxy at the new access token. Callers must not hold settingsMu.
func (a *App) rotateVendorCredential() (Settings, error) {
	if a == nil {
		return Settings{}, errors.New("CodexProxy 未就绪")
	}
	a.vendorRefreshMu.Lock()
	defer a.vendorRefreshMu.Unlock()
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	s, err := loadSettings()
	if err != nil {
		return Settings{}, err
	}
	next, changed, refreshErr := refreshVendorCredential(s)
	if refreshErr != nil {
		return s, refreshErr
	}
	if !changed {
		return s, nil
	}
	if err := writeSettings(next); err != nil {
		return s, err
	}
	a.mu.Lock()
	srv := a.server
	a.mu.Unlock()
	if srv != nil {
		srv.SetUpstreamWithClientName(next.BaseURL, next.AccessToken, proxyClientName(next))
	}
	return next, nil
}

func preferText(next, prev string) string {
	if strings.TrimSpace(next) != "" {
		return strings.TrimSpace(next)
	}
	return prev
}

func configureVendorUpstream(a *App, server *codegenproxy.Server, s Settings) {
	if server == nil {
		return
	}
	account := AuthProfile{}
	if s.AuthProfiles != nil {
		account = s.AuthProfiles[s.ActiveAuthMode]
	}
	cred := rotatingCredential{
		mode:         s.ActiveAuthMode,
		access:       s.AccessToken,
		refresh:      account.RefreshToken,
		expires:      account.ExpiresAt,
		userID:       account.UserID,
		enterpriseID: account.EnterpriseID,
		domain:       account.Domain,
		rotator:      a,
	}
	server.SetUpstreamExtraHeaders(nil)
	switch s.ActiveAuthMode {
	case AuthModeQoderCN, AuthModeQoderGlobal:
		profile, ok := qoderProfileForMode(s.ActiveAuthMode)
		if !ok {
			return
		}
		httpClient := qoder.WrapClientWithEdition(&http.Client{}, account.UserID, profile.StoreID)
		server.SetOpenAIUpstreamClient(&qoderChatUpstream{
			profile: profile,
			client:  httpClient,
			cred:    &cred,
		})
	case AuthModeWorkBuddyCN, AuthModeWorkBuddyGlobal:
		profile, ok := workbuddyProfileForMode(s.ActiveAuthMode)
		if !ok {
			return
		}
		server.SetOpenAIUpstreamClient(&workbuddyChatUpstream{
			profile: profile,
			client:  workbuddy.WrapClient(&http.Client{}),
			cred:    &cred,
		})
	}
}

// rotatingCredential keeps the access token a chat will send and renews it
// shortly before it expires. The renewal is serialized by App.rotateVendorCredential.
type rotatingCredential struct {
	mu             sync.Mutex
	mode           string
	access         string
	refresh        string
	expires        int64
	userID         string
	enterpriseID   string
	domain         string
	retryNotBefore time.Time
	rotator        vendorRotator
}

type vendorRotator interface {
	rotateVendorCredential() (Settings, error)
}

func (r *rotatingCredential) maybeRotate() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.rotator == nil || !vendorAccessDue(r.mode, r.expires, time.Now()) || time.Now().Before(r.retryNotBefore) {
		r.mu.Unlock()
		return
	}
	rotator := r.rotator
	r.mu.Unlock()
	settings, err := rotator.rotateVendorCredential()
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		r.retryNotBefore = time.Now().Add(30 * time.Second)
		return
	}
	if settings.ActiveAuthMode != r.mode {
		return
	}
	profile := settings.AuthProfiles[r.mode]
	if access := strings.TrimSpace(settings.AccessToken); access != "" {
		r.access = access
	}
	r.refresh = preferText(profile.RefreshToken, r.refresh)
	r.userID = preferText(profile.UserID, r.userID)
	r.enterpriseID = preferText(profile.EnterpriseID, r.enterpriseID)
	r.domain = preferText(profile.Domain, r.domain)
	if profile.ExpiresAt > 0 {
		r.expires = profile.ExpiresAt
	}
	r.retryNotBefore = time.Time{}
}

func (r *rotatingCredential) snapshot(fallback string) (access, refresh, userID, enterprise, domain string) {
	if r == nil {
		return fallback, "", "", "", ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	access = r.access
	if strings.TrimSpace(access) == "" {
		access = fallback
	}
	return access, r.refresh, r.userID, r.enterpriseID, r.domain
}

type qoderChatUpstream struct {
	profile  qoder.Profile
	client   *http.Client
	cred     *rotatingCredential
	mu       sync.Mutex
	models   []byte
	modelsAt time.Time
}

func (u *qoderChatUpstream) DoModels(ctx context.Context, _, apiKey, _ string) (*http.Response, error) {
	u.cred.maybeRotate()
	access, _, userID, _, _ := u.cred.snapshot(apiKey)
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.models) > 0 && time.Since(u.modelsAt) < 10*time.Minute {
		return jsonHTTPResponse(http.StatusOK, append([]byte(nil), u.models...)), nil
	}
	options, err := fetchQoderModels(ctx, u.profile, userID, access)
	if err != nil || len(options) == 0 {
		if len(u.models) > 0 {
			return jsonHTTPResponse(http.StatusOK, append([]byte(nil), u.models...)), nil
		}
		options = []ModelOption{{ID: qoder.DefaultModel, Name: qoder.DefaultModel}}
		return jsonHTTPResponse(http.StatusOK, openAIModelList(options)), nil
	}
	body := openAIModelList(options)
	u.models = append([]byte(nil), body...)
	u.modelsAt = time.Now()
	return jsonHTTPResponse(http.StatusOK, append([]byte(nil), body...)), nil
}

func (u *qoderChatUpstream) DoChatCompletions(ctx context.Context, endpoint, apiKey, clientName string, body []byte, accept string, stream bool) (*http.Response, error) {
	u.cred.maybeRotate()
	access, _, _, _, _ := u.cred.snapshot(apiKey)
	return postChat(ctx, u.client, endpoint, access, clientName, body, accept, stream, nil)
}

type workbuddyChatUpstream struct {
	profile workbuddy.Profile
	client  *http.Client
	cred    *rotatingCredential
	mu      sync.Mutex
	models  []byte
}

func (u *workbuddyChatUpstream) DoModels(context.Context, string, string, string) (*http.Response, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.models) == 0 {
		options := workbuddyModelOptions(workbuddy.Allowlist(u.profile.ID))
		if len(options) == 0 && u.profile.DefaultModel != "" {
			options = []ModelOption{{ID: u.profile.DefaultModel, Name: u.profile.DefaultModel}}
		}
		u.models = openAIModelList(options)
	}
	return jsonHTTPResponse(http.StatusOK, append([]byte(nil), u.models...)), nil
}

func (u *workbuddyChatUpstream) DoChatCompletions(ctx context.Context, endpoint, apiKey, clientName string, body []byte, accept string, stream bool) (*http.Response, error) {
	u.cred.maybeRotate()
	access, refresh, userID, enterprise, domain := u.cred.snapshot(apiKey)
	body = codegenproxy.StripCodexClientMarkers(endpoint, body)
	return postChat(ctx, u.client, endpoint, access, clientName, body, accept, stream, func(h http.Header) {
		clearCodexIdentityHeaders(h)
		h.Set(workbuddy.MarkerHeader, "1")
		h.Set("User-Agent", workbuddy.UserAgent)
		h.Set("X-Product", workbuddy.Product)
		h.Set("X-Requested-With", "XMLHttpRequest")
		if origin := strings.TrimSpace(u.profile.Origin); origin != "" {
			h.Set("Origin", origin)
			h.Set("Referer", strings.TrimRight(origin, "/")+"/")
		}
		setHeaderOrAbsent(h, "X-User-Id", "X-No-User-Id", userID)
		setHeaderOrAbsent(h, "X-Enterprise-Id", "X-No-Enterprise-Id", enterprise)
		if strings.TrimSpace(domain) != "" {
			h.Set("X-Domain", strings.TrimSpace(domain))
		} else {
			h.Set("X-No-Department-Info", "1")
		}
		if token := strings.TrimSpace(refresh); token != "" {
			h.Set("X-Refresh-Token", token)
		}
	})
}

func postChat(ctx context.Context, client *http.Client, endpoint, apiKey, clientName string, body []byte, accept string, stream bool, extra func(http.Header)) (*http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if ua := strings.TrimSpace(clientName); ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	if stream || strings.TrimSpace(accept) == "" {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", accept)
	}
	if extra != nil {
		extra(req.Header)
	}
	return client.Do(req)
}

func setHeaderOrAbsent(h http.Header, name, absentName, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		h.Set(name, value)
		h.Del(absentName)
		return
	}
	h.Set(absentName, "1")
}

func openAIModelList(models []ModelOption) []byte {
	data := make([]map[string]string, 0, len(models))
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		data = append(data, map[string]string{"id": id, "object": "model", "owned_by": "upstream"})
	}
	raw, err := json.Marshal(map[string]any{"object": "list", "data": data})
	if err != nil {
		return []byte(`{"object":"list","data":[]}`)
	}
	return raw
}

func jsonHTTPResponse(status int, payload []byte) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Status:        http.StatusText(status),
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(bytes.NewReader(payload)),
		ContentLength: int64(len(payload)),
	}
}
