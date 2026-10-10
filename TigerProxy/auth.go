package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/kimicode"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"github.com/pkg/browser"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type AuthModeInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Hint string `json:"hint"`
	Kind string `json:"kind"`
	Icon string `json:"icon"`
}

func authModeCatalog() []AuthModeInfo {
	return []AuthModeInfo{
		{ID: AuthModeSSO, Name: "CodeGen SSO", Hint: "现有企业登录，功能保持不变", Kind: "sso", Icon: "SSO"},
		{ID: AuthModeCustomOpenAI, Name: "自定义 OpenAI API", Hint: "手动填写 URL / API Key，可列出模型", Kind: "apikey", Icon: "API"},
		{ID: AuthModeOpenAIOAuth, Name: "OpenAI 账号登录", Hint: "浏览器 OAuth，登录 ChatGPT / OpenAI", Kind: "oauth", Icon: "OA"},
		{ID: AuthModeXAIOAuth, Name: "xAI Grok 登录", Hint: "浏览器 OAuth，使用 xAI 账号", Kind: "oauth", Icon: "GK"},
		{ID: AuthModeAnthropicOAuth, Name: "Claude Code 登录", Hint: "Max/Pro 走账号登录，否则填写 API Key", Kind: "oauth_code", Icon: "CC"},
		{ID: AuthModeZhipuCoding, Name: "智谱编程", Hint: "预置 OpenAI 兼容端点，填写 API Key", Kind: "apikey", Icon: "智"},
		{ID: AuthModeKimiWeb, Name: "Kimi Code 登录", Hint: "浏览器授权登录 Kimi Code，无需 API Key", Kind: "kimi_device", Icon: "K"},
	}
}

func (a *App) AuthModes() []AuthModeInfo {
	return authModeCatalog()
}

func authModeLabel(mode string) string {
	for _, item := range authModeCatalog() {
		if item.ID == mode {
			return item.Name
		}
	}
	if mode == "" {
		return ""
	}
	return mode
}

func validAuthMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case AuthModeSSO, AuthModeCustomOpenAI, AuthModeOpenAIOAuth, AuthModeXAIOAuth, AuthModeAnthropicOAuth, AuthModeZhipuCoding, AuthModeKimiWeb:
		return true
	default:
		return false
	}
}

func defaultAuthBaseURL(mode string) string {
	switch mode {
	case AuthModeSSO:
		return oauth.CodeGenBaseURL
	case AuthModeOpenAIOAuth:
		return openaiOfficialURL
	case AuthModeXAIOAuth:
		return xaiOfficialURL
	case AuthModeAnthropicOAuth:
		return anthropicOfficialURL
	case AuthModeZhipuCoding:
		return zhipuCodingDefaultURL
	case AuthModeKimiWeb:
		return kimiCodingDefaultURL()
	default:
		return ""
	}
}

func looksLikeCodeGenBaseURL(base string) bool {
	base = strings.ToLower(strings.TrimRight(strings.TrimSpace(base), "/"))
	if base == "" {
		return true
	}
	if base == strings.ToLower(strings.TrimRight(oauth.CodeGenBaseURL, "/")) {
		return true
	}
	return strings.Contains(base, "codegen")
}

func canonicalAuthBaseURL(mode string) string {
	switch mode {
	case AuthModeSSO, AuthModeOpenAIOAuth, AuthModeXAIOAuth, AuthModeAnthropicOAuth:
		return strings.TrimRight(defaultAuthBaseURL(mode), "/")
	default:
		return ""
	}
}

func defaultAuthModelID(mode string) string {
	switch mode {
	case AuthModeZhipuCoding:
		return zhipuCodingDefaultModel
	case AuthModeKimiWeb:
		return kimiCodingDefaultModel()
	case AuthModeAnthropicOAuth:
		return "claude-sonnet-4-5"
	default:
		return ""
	}
}

// kimiCodingDefaultURL is the Kimi Code API that pairs with the device-flow
// auth host currently configured.
func kimiCodingDefaultURL() string {
	return kimicode.BaseURLForOAuthHost(kimicode.OAuthHost())
}

// kimiCodingDefaultModel is the managed Kimi Code alias used before a catalog
// fetch succeeds.
func kimiCodingDefaultModel() string {
	return kimicode.DefaultModel
}

func snapshotActiveProfile(s *Settings) {
	if s == nil || strings.TrimSpace(s.ActiveAuthMode) == "" {
		return
	}
	if s.AuthProfiles == nil {
		s.AuthProfiles = map[string]AuthProfile{}
	}
	prev := s.AuthProfiles[s.ActiveAuthMode]
	s.AuthProfiles[s.ActiveAuthMode] = AuthProfile{
		AccessToken:  s.AccessToken,
		RefreshToken: prev.RefreshToken,
		APIKey:       s.AccessToken,
		BaseURL:      s.BaseURL,
		Email:        s.Email,
		ModelID:      s.ModelID,
		Models:       s.Models,
	}
}

func applyAuthProfile(s *Settings, mode string) {
	if s == nil {
		return
	}
	s.ActiveAuthMode = mode
	profile := AuthProfile{}
	if s.AuthProfiles != nil {
		profile = s.AuthProfiles[mode]
	}
	token := strings.TrimSpace(profile.AccessToken)
	if token == "" {
		token = strings.TrimSpace(profile.APIKey)
	}
	s.AccessToken = token
	if canon := canonicalAuthBaseURL(mode); canon != "" {
		s.BaseURL = canon
	} else {
		s.BaseURL = strings.TrimRight(strings.TrimSpace(profile.BaseURL), "/")
		if s.BaseURL == "" {
			s.BaseURL = strings.TrimRight(defaultAuthBaseURL(mode), "/")
		}
	}
	if profileHasCredential(profile) {
		s.Email = strings.TrimSpace(profile.Email)
		s.ModelID = strings.TrimSpace(profile.ModelID)
		s.Models = profile.Models
		if s.ModelID == "" {
			s.ModelID = defaultAuthModelID(mode)
		}
	} else {
		s.Email = ""
		s.Models = nil
		s.ModelID = defaultAuthModelID(mode)
	}
}

func authProfileReady(s Settings) bool {
	if strings.TrimSpace(s.ActiveAuthMode) == "" {
		return false
	}
	if strings.TrimSpace(s.AccessToken) == "" {
		return false
	}
	switch s.ActiveAuthMode {
	case AuthModeCustomOpenAI, AuthModeZhipuCoding:
		return strings.TrimSpace(s.BaseURL) != ""
	default:
		// Kimi Code pins its coding endpoint, so the token alone is enough.
		return true
	}
}

func profileHasCredential(p AuthProfile) bool {
	return strings.TrimSpace(p.AccessToken) != "" || strings.TrimSpace(p.APIKey) != ""
}

func needsOnboarding(s Settings) bool {
	if strings.TrimSpace(s.ActiveAuthMode) != "" {
		return false
	}
	if strings.TrimSpace(s.AccessToken) != "" {
		return false
	}
	for _, profile := range s.AuthProfiles {
		if profileHasCredential(profile) {
			return false
		}
	}
	return true
}

func readyAuthModes(s Settings) []string {
	var out []string
	for _, info := range authModeCatalog() {
		if profileHasCredential(s.AuthProfiles[info.ID]) {
			out = append(out, info.ID)
		}
	}
	return out
}

// SelectAuthMode is used on first-run (and when a switch lands on a method
// that still needs credentials). It does not relaunch the process.
func (a *App) SelectAuthMode(mode string) (Status, error) {
	mode = strings.TrimSpace(mode)
	if !validAuthMode(mode) {
		return Status{}, fmt.Errorf("未知的登录方式")
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	s, err := loadSettings()
	if err != nil {
		return Status{}, err
	}
	a.cancelPendingLogins()
	snapshotActiveProfile(&s)
	applyAuthProfile(&s, mode)
	s = normalizeSettings(s)
	if err := a.applySettingsWithRestart(s); err != nil {
		return Status{}, err
	}
	if s.ActiveAuthMode == AuthModeSSO {
		go a.refreshModelsIfLoggedIn()
	}
	return a.Status()
}

func (a *App) cancelPendingLogins() {
	a.logout()
	a.mu.Lock()
	a.anthropicOAuth = nil
	a.mu.Unlock()
}

// SwitchAuthMode changes the active login method. If that method already has
// credentials, CodexProxy relaunches so the new upstream takes effect.
func (a *App) SwitchAuthMode(mode string) (Status, error) {
	mode = strings.TrimSpace(mode)
	if !validAuthMode(mode) {
		return Status{}, fmt.Errorf("未知的登录方式")
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	s, err := loadSettings()
	if err != nil {
		return Status{}, err
	}
	if s.ActiveAuthMode == mode {
		status, err := a.Status()
		return status, err
	}
	a.cancelPendingLogins()
	snapshotActiveProfile(&s)
	applyAuthProfile(&s, mode)
	s = normalizeSettings(s)
	ready := authProfileReady(s)
	if err := a.applySettingsWithRestart(s); err != nil {
		return Status{}, err
	}
	if s.ActiveAuthMode == AuthModeSSO {
		go a.refreshModelsIfLoggedIn()
	}
	if ready {
		if err := a.scheduleRelaunch(); err != nil {
			return Status{}, err
		}
		status, err := a.Status()
		if err != nil {
			return status, err
		}
		status.RelaunchScheduled = true
		return status, nil
	}
	a.setRelaunchAfterAuth(true)
	return a.Status()
}

// setRelaunchAfterAuth arms or disarms the pending relaunch. The flag is
// written by the login paths and read by statusMaybeRelaunch, which can run on
// a different goroutine once applyOAuthResult returns.
func (a *App) setRelaunchAfterAuth(v bool) {
	a.mu.Lock()
	a.relaunchAfterAuth = v
	a.mu.Unlock()
}

// takeRelaunchAfterAuth reports whether a relaunch is pending, clearing it so
// two concurrent callers cannot each schedule a restart.
func (a *App) takeRelaunchAfterAuth() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.relaunchAfterAuth {
		return false
	}
	a.relaunchAfterAuth = false
	return true
}

func (a *App) statusMaybeRelaunch() (Status, error) {
	if a.takeRelaunchAfterAuth() && authProfileReadyMustLoad() {
		if err := a.scheduleRelaunch(); err != nil {
			return Status{}, err
		}
		status, err := a.Status()
		if err != nil {
			return status, err
		}
		status.RelaunchScheduled = true
		return status, nil
	}
	return a.Status()
}

func authProfileReadyMustLoad() bool {
	s, err := loadSettings()
	if err != nil {
		return false
	}
	return authProfileReady(s)
}

func (a *App) RelaunchApp() (Status, error) {
	if err := a.scheduleRelaunch(); err != nil {
		return Status{}, err
	}
	status, err := a.Status()
	if err != nil {
		return status, err
	}
	status.RelaunchScheduled = true
	return status, nil
}

func (a *App) StartOpenAIOAuth() (LoginStartResult, error) {
	a.cancelOpenAILogin()
	cfg := oauth.DefaultConfig()
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	server := oauth.NewCallbackServer()
	if err := server.StartOnPort(cfg.CallbackPath, oauth.DefaultCallbackPort); err != nil {
		cancel()
		return LoginStartResult{}, fmt.Errorf("OpenAI 回调需要 localhost:%d，端口被占用：%w", oauth.DefaultCallbackPort, err)
	}
	redirectURI := fmt.Sprintf("http://localhost:%d%s", server.Port(), cfg.CallbackPath)
	params, err := oauth.PrepareHeadlessOAuthWithRedirectURI(cfg, redirectURI)
	if err != nil {
		server.Stop()
		cancel()
		return LoginStartResult{}, err
	}
	a.mu.Lock()
	a.openaiServer = server
	a.openaiParams = params
	a.openaiCfg = cfg
	a.openaiCtx = ctx
	a.openaiCancel = cancel
	a.openaiDone = false
	a.mu.Unlock()
	log.Printf("[codexproxy] OpenAI callback listening on %s", redirectURI)
	if err := openExternalURL(a.ctx, params.AuthURL); err != nil {
		a.cancelOpenAILogin()
		return LoginStartResult{}, err
	}
	return LoginStartResult{LoginURL: params.AuthURL, CallbackURL: redirectURI}, nil
}

func (a *App) CompleteOpenAIOAuth() (Status, error) {
	a.mu.Lock()
	server := a.openaiServer
	ctx := a.openaiCtx
	params := a.openaiParams
	cfg := a.openaiCfg
	a.mu.Unlock()
	if server == nil || params == nil {
		return Status{}, fmt.Errorf("请先点击 OpenAI 登录")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	code, err := server.WaitForCodeCtx(ctx)
	if err != nil {
		return a.openAIStatusIfDone(err)
	}
	result, err := oauth.ExchangeCodeCtx(ctx, cfg, code, params.Verifier, params.RedirectURI)
	if err != nil {
		return a.openAIStatusIfDone(err)
	}
	return a.finishOpenAI(result)
}

func (a *App) CompleteOpenAIOAuthWithCode(code string) (Status, error) {
	a.mu.Lock()
	params := a.openaiParams
	cfg := a.openaiCfg
	ctx := a.openaiCtx
	a.mu.Unlock()
	if params == nil {
		return Status{}, fmt.Errorf("请先点击 OpenAI 登录")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := oauth.CompleteHeadlessOAuth(cfg, params, code)
	if err != nil {
		return a.openAIStatusIfDone(err)
	}
	return a.finishOpenAI(result)
}

func (a *App) openAIStatusIfDone(err error) (Status, error) {
	a.mu.Lock()
	done := a.openaiDone
	a.mu.Unlock()
	if done {
		return a.Status()
	}
	return Status{}, err
}

func (a *App) finishOpenAI(result *oauth.TokenResult) (Status, error) {
	a.mu.Lock()
	if a.openaiDone {
		a.mu.Unlock()
		return a.Status()
	}
	a.openaiDone = true
	cancel := a.openaiCancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	status, err := a.applyOAuthResult(AuthModeOpenAIOAuth, openaiOfficialURL, result)
	a.cancelOpenAILogin()
	return status, err
}

func (a *App) cancelOpenAILogin() {
	a.mu.Lock()
	cancel := a.openaiCancel
	server := a.openaiServer
	a.openaiCancel = nil
	a.openaiCtx = nil
	a.openaiServer = nil
	a.openaiParams = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if server != nil {
		server.Stop()
	}
}

func (a *App) StartXAIOAuth() (LoginStartResult, error) {
	a.cancelXAILogin()
	ctx, cancel := context.WithTimeout(context.Background(), oauth.XAIConfig().Timeout)
	session, err := oauth.PrepareXAIOAuthFlowCtx(ctx)
	if err != nil {
		cancel()
		return LoginStartResult{}, err
	}
	a.mu.Lock()
	a.xaiSession = session
	a.xaiCtx = ctx
	a.xaiCancel = cancel
	a.xaiDone = false
	a.mu.Unlock()
	log.Printf("[codexproxy] xAI callback listening on %s", session.RedirectURI())
	if err := openExternalURL(a.ctx, session.AuthorizationURL()); err != nil {
		a.cancelXAILogin()
		return LoginStartResult{}, err
	}
	go a.watchXAIClipboard(ctx)
	return LoginStartResult{LoginURL: session.AuthorizationURL(), CallbackURL: session.RedirectURI()}, nil
}

func (a *App) watchXAIClipboard(ctx context.Context) {
	if a.ctx == nil {
		return
	}
	baseline, _ := runtime.ClipboardGetText(a.ctx)
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			text, err := runtime.ClipboardGetText(a.ctx)
			if err != nil || text == "" || text == baseline {
				continue
			}
			if !oauth.LooksLikeOAuthAuthCode(text) {
				baseline = text
				continue
			}
			if _, err := a.CompleteXAIOAuthWithCode(text); err != nil {
				a.mu.Lock()
				done := a.xaiDone
				a.mu.Unlock()
				if done {
					return
				}
				log.Printf("[codexproxy] clipboard xAI code ignored: %v", err)
				baseline = text
				continue
			}
			return
		}
	}
}

func openExternalURL(wailsCtx context.Context, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("empty url")
	}
	if err := browser.OpenURL(raw); err != nil {
		log.Printf("[codexproxy] system browser open failed, fallback to Wails: %v", err)
		if wailsCtx == nil {
			return err
		}
		runtime.BrowserOpenURL(wailsCtx, raw)
	}
	return nil
}

func (a *App) CompleteXAIOAuth() (Status, error) {
	a.mu.Lock()
	session := a.xaiSession
	ctx := a.xaiCtx
	a.mu.Unlock()
	if session == nil {
		return Status{}, fmt.Errorf("请先点击 xAI Grok 登录")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := session.WaitForCompletionCtx(ctx)
	if err != nil {
		a.mu.Lock()
		done := a.xaiDone
		a.mu.Unlock()
		if done {
			return a.Status()
		}
		return Status{}, err
	}
	return a.finishXAI(result)
}

func (a *App) CompleteXAIOAuthWithCode(code string) (Status, error) {
	a.mu.Lock()
	session := a.xaiSession
	ctx := a.xaiCtx
	a.mu.Unlock()
	if session == nil {
		return Status{}, fmt.Errorf("请先点击 xAI Grok 登录")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := session.CompleteWithCode(ctx, code)
	if err != nil {
		a.mu.Lock()
		done := a.xaiDone
		a.mu.Unlock()
		if done {
			return a.Status()
		}
		return Status{}, err
	}
	return a.finishXAI(result)
}

func (a *App) finishXAI(result *oauth.TokenResult) (Status, error) {
	a.mu.Lock()
	if a.xaiDone {
		a.mu.Unlock()
		return a.Status()
	}
	a.xaiDone = true
	cancel := a.xaiCancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	status, err := a.applyOAuthResult(AuthModeXAIOAuth, xaiOfficialURL, result)
	a.cancelXAILogin()
	return status, err
}

func (a *App) cancelXAILogin() {
	a.mu.Lock()
	cancel := a.xaiCancel
	session := a.xaiSession
	a.xaiCancel = nil
	a.xaiCtx = nil
	a.xaiSession = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if session != nil {
		session.Close()
	}
}

func (a *App) StartAnthropicOAuth() (string, error) {
	params, err := oauth.PrepareAnthropicOAuth()
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.anthropicOAuth = params
	a.mu.Unlock()
	if err := openExternalURL(a.ctx, params.AuthURL); err != nil {
		return "", fmt.Errorf("无法打开 Claude.ai 登录页: %w", err)
	}
	return params.AuthURL, nil
}

func (a *App) CompleteAnthropicOAuth(code string) (Status, error) {
	a.mu.Lock()
	params := a.anthropicOAuth
	a.mu.Unlock()
	if params == nil {
		return Status{}, fmt.Errorf("请先点击 Claude Code 登录")
	}
	code = strings.TrimSpace(code)
	if i := strings.Index(code, "#"); i >= 0 {
		code = strings.TrimSpace(code[:i])
	}
	result, err := oauth.CompleteAnthropicOAuth(params, code)
	if err != nil {
		return Status{}, err
	}
	a.mu.Lock()
	a.anthropicOAuth = nil
	a.mu.Unlock()
	return a.applyOAuthResult(AuthModeAnthropicOAuth, anthropicOfficialURL, result)
}

func (a *App) applyOAuthResult(mode, baseURL string, result *oauth.TokenResult) (Status, error) {
	token := ""
	if result != nil {
		token = strings.TrimSpace(result.AccessToken)
		if mode == AuthModeOpenAIOAuth && strings.TrimSpace(result.RawAccessToken) != "" {
			token = strings.TrimSpace(result.RawAccessToken)
		}
	}
	if token == "" {
		return Status{}, fmt.Errorf("OAuth 未返回 access token")
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	s, err := loadSettings()
	if err != nil {
		return Status{}, err
	}
	if s.ActiveAuthMode != "" && s.ActiveAuthMode != mode {
		return a.Status()
	}
	snapshotActiveProfile(&s)
	s.ActiveAuthMode = mode
	s.AccessToken = token
	if canon := canonicalAuthBaseURL(mode); canon != "" {
		s.BaseURL = canon
	} else {
		s.BaseURL = strings.TrimRight(baseURL, "/")
	}
	if s.AuthProfiles == nil {
		s.AuthProfiles = map[string]AuthProfile{}
	}
	profile := s.AuthProfiles[mode]
	profile.RefreshToken = result.RefreshToken
	s.AuthProfiles[mode] = profile
	if def := defaultAuthModelID(mode); def != "" && !modelExistsInList(s.ModelID, s.Models) {
		s.ModelID = def
		s.Models = nil
	}
	s = normalizeSettings(s)
	if err := a.applySettingsWithRestart(s); err != nil {
		return Status{}, err
	}
	go a.refreshModelsForActiveMode()
	return a.statusMaybeRelaunch()
}

func (a *App) SaveUpstreamCredential(baseURL, apiKey, modelID string) (Status, error) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	s, err := loadSettings()
	if err != nil {
		return Status{}, err
	}
	if strings.TrimSpace(s.ActiveAuthMode) == "" {
		return Status{}, fmt.Errorf("请先选择登录方式")
	}
	if strings.TrimSpace(baseURL) != "" {
		s.BaseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	}
	if strings.TrimSpace(apiKey) != "" {
		s.AccessToken = strings.TrimSpace(apiKey)
	}
	if strings.TrimSpace(modelID) != "" {
		s.ModelID = strings.TrimSpace(modelID)
	}
	s = normalizeSettings(s)
	if err := a.applySettingsWithRestart(s); err != nil {
		return Status{}, err
	}
	return a.statusMaybeRelaunch()
}

func (a *App) ListUpstreamModels(baseURL, apiKey string) ([]ModelOption, error) {
	s, err := loadSettings()
	if err != nil {
		return nil, err
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = s.BaseURL
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" || apiKey == "已保存" {
		apiKey = s.AccessToken
	}
	if baseURL == "" {
		return nil, fmt.Errorf("请先填写上游 Base URL")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("请先填写上游 API Key")
	}
	models, err := fetchModelsForMode(s.ActiveAuthMode, baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(baseURL, s.BaseURL) && apiKey == s.AccessToken {
		a.updateModelsForCurrentToken(apiKey, models)
	}
	return models, nil
}

func (a *App) refreshModelsForActiveMode() {
	s, err := loadSettings()
	if err != nil || strings.TrimSpace(s.AccessToken) == "" || strings.TrimSpace(s.BaseURL) == "" {
		return
	}
	models, err := fetchModelsForMode(s.ActiveAuthMode, s.BaseURL, s.AccessToken)
	if err != nil || len(models) == 0 {
		return
	}
	if a.updateModelsForCurrentToken(s.AccessToken, models) && a.ctx != nil {
		runtime.EventsEmit(a.ctx, "models-refreshed", nil)
	}
}

func fetchModelsForMode(mode, baseURL, apiKey string) ([]ModelOption, error) {
	if mode == AuthModeSSO {
		models, _, err := oauth.FetchCodeGenModels(apiKey)
		if err != nil {
			return nil, err
		}
		return modelOptionsFromOAuth(models), nil
	}
	if mode == AuthModeAnthropicOAuth {
		return fetchAnthropicModels(baseURL, apiKey)
	}
	headers := map[string]string{}
	if mode == AuthModeXAIOAuth {
		headers["X-XAI-Token-Auth"] = "xai-grok-cli"
	}
	return fetchOpenAICompatibleModels(baseURL, apiKey, headers)
}

func fetchAnthropicModels(baseURL, apiKey string) ([]ModelOption, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("列出 Claude 模型失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("列出 Claude 模型失败 (HTTP %d)", resp.StatusCode)
	}
	var parsed struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Name        string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("解析 Claude 模型列表失败: %w", err)
	}
	options := make([]ModelOption, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(item.DisplayName)
		if name == "" {
			name = strings.TrimSpace(item.Name)
		}
		if name == "" {
			name = id
		}
		options = append(options, ModelOption{ID: id, Name: name})
	}
	if len(options) == 0 {
		return nil, fmt.Errorf("Anthropic 未返回可用模型")
	}
	return normalizeModelOptions(options), nil
}

func fetchOpenAICompatibleModels(baseURL, apiKey string, extraHeaders map[string]string) ([]ModelOption, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	for key, value := range extraHeaders {
		req.Header.Set(key, value)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("列出模型失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("列出模型失败 (HTTP %d)", resp.StatusCode)
	}
	var parsed struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %w", err)
	}
	options := make([]ModelOption, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = id
		}
		options = append(options, ModelOption{ID: id, Name: name})
	}
	if len(options) == 0 {
		return nil, fmt.Errorf("上游未返回可用模型")
	}
	return normalizeModelOptions(options), nil
}
