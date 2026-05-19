package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codegenproxy"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	defaultListenAddress = "0.0.0.0:18086"
	defaultProxyAPIKey   = "tigerproxy-local-key"
)

type App struct {
	ctx       context.Context
	cancel    context.CancelFunc
	ssoCtx    context.Context`n`tssoCancel context.CancelFunc
	server    *codegenproxy.Server
	listen    string
	lastError string
	mu        sync.Mutex
	shown     bool
}

type Settings struct {
	ListenAddress string        `json:"listen_address"`
	APIKey        string        `json:"api_key"`
	AccessToken   string        `json:"access_token,omitempty"`
	BaseURL       string        `json:"base_url"`
	ModelID       string        `json:"model_id,omitempty"`
	Email         string        `json:"email,omitempty"`
	UpdatedAt     string        `json:"updated_at,omitempty"`
	Models        []ModelOption `json:"models,omitempty"`
}

type ModelOption struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextWindow int    `json:"context_window,omitempty"`
}

type Status struct {
	Settings     Settings `json:"settings"`
	Running      bool     `json:"running"`
	LastError    string   `json:"last_error,omitempty"`
	OpenAIURL    string   `json:"openai_url"`
	AnthropicURL string   `json:"anthropic_url"`
	HealthURL    string   `json:"health_url"`
	BindAddress  string   `json:"bind_address"`
	LANURLs      []string `json:"lan_urls"`
	LoggedIn     bool     `json:"logged_in"`
}

type LoginStartResult struct {
	LoginURL    string `json:"login_url"`
	CallbackURL string `json:"callback_url"`
}

func NewApp() *App {
	return &App{shown: true}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.startProxyFromDisk()
}

func (a *App) shutdown(ctx context.Context) {
	_ = ctx
	a.cancelSSOLogin()
	a.stopProxy()
}

func (a *App) LoadSettings() (Settings, error) {
	return loadSettings()
}

func (a *App) SaveSettings(s Settings) (Status, error) {
	cur, _ := loadSettings()
	if strings.TrimSpace(s.ListenAddress) == "" {
		s.ListenAddress = cur.ListenAddress
	}
	if strings.TrimSpace(s.AccessToken) == "" {
		s.AccessToken = cur.AccessToken
	}
	if strings.TrimSpace(s.BaseURL) == "" {
		s.BaseURL = cur.BaseURL
	}
	if strings.TrimSpace(s.ModelID) == "" {
		s.ModelID = cur.ModelID
	}
	if strings.TrimSpace(s.Email) == "" {
		s.Email = cur.Email
	}
	if len(s.Models) == 0 {
		s.Models = cur.Models
	}
	s = normalizeSettings(s)
	if err := a.restartProxy(s); err != nil {
		return Status{}, err
	}
	if err := writeSettings(s); err != nil {
		return Status{}, err
	}
	return a.Status()
}

func (a *App) LoginSSO() (Status, error) {
	ctx, cancel := context.WithTimeout(context.Background(), oauth.CodeGenTimeout)
	defer cancel()

	result, err := oauth.RunCodeGenSSOFlowWithCallback(ctx)
	if err != nil {
		return Status{}, err
	}

	s, _ := loadSettings()
	s.AccessToken = result.AccessToken
	s.BaseURL = result.BaseURL
	s.ModelID = result.ModelID
	s.Email = result.Email
	s.Models = modelOptionsFromOAuth(result.Models)
	if strings.TrimSpace(s.APIKey) == "" {
		s.APIKey = defaultProxyAPIKey
	}
	s = normalizeSettings(s)
	if err := a.restartProxy(s); err != nil {
		return Status{}, err
	}
	if err := writeSettings(s); err != nil {
		return Status{}, err
	}
	return a.Status()
}

func (a *App) StartSSOLogin() (LoginStartResult, error) {
	a.cancelSSOLogin()
	ctx, cancel := context.WithTimeout(context.Background(), oauth.CodeGenTimeout)
	a.mu.Lock()
	a.ssoCancel = cancel
	a.mu.Unlock()

	loginURL, callbackURL, err := oauth.StartCodeGenSSOCallbackServer(ctx)
	if err != nil {
		a.cancelSSOLogin()
		return LoginStartResult{}, err
	}
	if a.ctx != nil {
		runtime.BrowserOpenURL(a.ctx, loginURL)
	}
	return LoginStartResult{LoginURL: loginURL, CallbackURL: callbackURL}, nil
}

func (a *App) CompleteSSOLogin() (Status, error) {
	defer a.cancelSSOLogin()
	a.mu.Lock()`n`tctx := a.ssoCtx`n`ta.mu.Unlock()`n`tif ctx == nil {`n`t`tctx = context.Background()`n`t}`n`tresult, err := oauth.WaitForCodeGenSSOCallbackContext(ctx, oauth.CodeGenTimeout)
	if err != nil {
		return Status{}, err
	}
	s, _ := loadSettings()
	s.AccessToken = result.AccessToken
	s.BaseURL = result.BaseURL
	s.ModelID = result.ModelID
	s.Email = result.Email
	s.Models = modelOptionsFromOAuth(result.Models)
	s = normalizeSettings(s)
	if err := a.restartProxy(s); err != nil {
		return Status{}, err
	}
	if err := writeSettings(s); err != nil {
		return Status{}, err
	}
	return a.Status()
}

func (a *App) cancelSSOLogin() {
	a.mu.Lock()
	cancel := a.ssoCancel
	a.ssoCancel = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) Logout() (Status, error) {
	s, _ := loadSettings()
	s.AccessToken = ""
	s.Email = ""
	s.ModelID = ""
	s.Models = nil
	s = normalizeSettings(s)
	if err := a.restartProxy(s); err != nil {
		return Status{}, err
	}
	if err := writeSettings(s); err != nil {
		return Status{}, err
	}
	return a.Status()
}

func (a *App) Status() (Status, error) {
	s, err := loadSettings()
	if err != nil {
		return Status{}, err
	}
	hostURL := publicBaseURL(s.ListenAddress, "127.0.0.1")
	return Status{
		Settings:     scrubSettings(s),
		Running:      a.isRunning(),
		LastError:    a.getLastError(),
		OpenAIURL:    hostURL + "/v1",
		AnthropicURL: hostURL + "/anthropic/v1",
		HealthURL:    hostURL + "/health",
		BindAddress:  s.ListenAddress,
		LANURLs:      lanURLs(s.ListenAddress),
		LoggedIn:     strings.TrimSpace(s.AccessToken) != "",
	}, nil
}

func (a *App) GenerateAPIKey() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "tp-" + hex.EncodeToString(b), nil
}

func (a *App) OpenURL(url string) error {
	if a.ctx == nil {
		return nil
	}
	runtime.BrowserOpenURL(a.ctx, url)
	return nil
}

func (a *App) WindowHide() {
	if a.ctx == nil {
		return
	}
	runtime.WindowHide(a.ctx)
	a.setShown(false)
}

func (a *App) startProxyFromDisk() {
	s, err := loadSettings()
	if err != nil {
		return
	}
	_ = a.restartProxy(s)
}

func (a *App) restartProxy(s Settings) error {
	s = normalizeSettings(s)
	if _, err := normalizeListenAddress(s.ListenAddress); err != nil {
		return err
	}

	a.mu.Lock()
	sameListenAddress := a.server != nil && a.listen == s.ListenAddress
	a.mu.Unlock()
	if sameListenAddress {
		a.stopProxy()
	}

	ctx, cancel := context.WithCancel(context.Background())
	server := codegenproxy.NewServer(s.ListenAddress)
	server.SetClientAPIKey(s.APIKey)
	if strings.TrimSpace(s.AccessToken) != "" && strings.TrimSpace(s.BaseURL) != "" {
		server.SetUpstream(s.BaseURL, s.AccessToken)
	}
	listener, err := net.Listen("tcp", s.ListenAddress)
	if err != nil {
		cancel()
		return fmt.Errorf("listen %s: %w", s.ListenAddress, err)
	}

	if !sameListenAddress {
		a.stopProxy()
	}

	a.mu.Lock()
	a.cancel = cancel
	a.server = server
	a.listen = s.ListenAddress
	a.lastError = ""
	a.mu.Unlock()

	go func() {
		if err := server.Serve(ctx, listener); err != nil && ctx.Err() == nil {
			msg := fmt.Sprintf("TigerProxy server stopped: %v", err)
			fmt.Fprintln(os.Stderr, msg)
			a.mu.Lock()
			if a.server == server {
				a.server = nil
				a.cancel = nil
				a.listen = ""
				a.lastError = msg
			}
			a.mu.Unlock()
		}
	}()
	return nil
}

func (a *App) stopProxy() {
	a.mu.Lock()
	cancel := a.cancel
	server := a.server
	a.cancel = nil
	a.server = nil
	a.listen = ""
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if server != nil {
		server.Stop()
	}
}

func (a *App) isRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.server != nil
}

func (a *App) getLastError() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastError
}

func (a *App) setShown(v bool) {
	a.mu.Lock()
	a.shown = v
	a.mu.Unlock()
	UpdateTrayVisibility(v)
}

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".tigerproxy"), nil
}

func settingsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

func loadSettings() (Settings, error) {
	path, err := settingsPath()
	if err != nil {
		return Settings{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s := normalizeSettings(Settings{})
			_ = writeSettings(s)
			return s, nil
		}
		return Settings{}, err
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return Settings{}, err
	}
	return normalizeSettings(s), nil
}

func writeSettings(s Settings) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	s.UpdatedAt = time.Now().Format(time.RFC3339)
	data, err := json.MarshalIndent(normalizeSettings(s), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func normalizeSettings(s Settings) Settings {
	if strings.TrimSpace(s.ListenAddress) == "" {
		s.ListenAddress = defaultListenAddress
	}
	if strings.TrimSpace(s.APIKey) == "" {
		s.APIKey = defaultProxyAPIKey
	}
	if strings.TrimSpace(s.BaseURL) == "" {
		s.BaseURL = oauth.CodeGenBaseURL
	}
	if normalized, err := normalizeListenAddress(s.ListenAddress); err == nil {
		s.ListenAddress = normalized
	}
	s.APIKey = strings.TrimSpace(s.APIKey)
	s.AccessToken = strings.TrimSpace(s.AccessToken)
	s.BaseURL = strings.TrimRight(strings.TrimSpace(s.BaseURL), "/")
	s.ModelID = strings.TrimSpace(s.ModelID)
	s.Email = strings.TrimSpace(s.Email)
	s.Models = normalizeModelOptions(s.Models)
	return s
}

func normalizeListenAddress(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = defaultListenAddress
	}
	if strings.HasPrefix(addr, ":") {
		addr = "0.0.0.0" + addr
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		if strings.Count(addr, ":") == 0 {
			return "0.0.0.0:" + addr, nil
		}
		return "", fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	if strings.TrimSpace(port) == "" {
		return "", fmt.Errorf("invalid listen address %q: port is required", addr)
	}
	if strings.TrimSpace(host) == "" || strings.EqualFold(host, "localhost") || host == "*" {
		host = "0.0.0.0"
	} else if ip, err := netip.ParseAddr(host); err == nil {
		host = ip.String()
	}
	return net.JoinHostPort(host, port), nil
}

func scrubSettings(s Settings) Settings {
	if s.AccessToken != "" {
		s.AccessToken = "已保存"
	}
	return s
}

func publicBaseURL(listen, host string) string {
	_, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		port = "18086"
	}
	return "http://" + host + ":" + port
}

func lanURLs(listen string) []string {
	_, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		port = "18086"
	}
	var urls []string
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.To4() == nil {
				continue
			}
			urls = append(urls, "http://"+ip.String()+":"+port)
		}
	}
	sort.Strings(urls)
	return urls
}

func modelOptionsFromOAuth(models []oauth.CodeGenModel) []ModelOption {
	options := make([]ModelOption, 0, len(models))
	for _, model := range models {
		options = append(options, ModelOption{
			ID:            model.ID,
			Name:          model.Name,
			ContextWindow: model.ContextWindow,
		})
	}
	return normalizeModelOptions(options)
}

func normalizeModelOptions(models []ModelOption) []ModelOption {
	seen := map[string]bool{}
	out := make([]ModelOption, 0, len(models))
	for _, model := range models {
		model.ID = strings.TrimSpace(model.ID)
		model.Name = strings.TrimSpace(model.Name)
		model.ID = normalizeModelID(model.ID)
		if model.ID == "" || seen[model.ID] {
			continue
		}
		if model.Name == "" {
			model.Name = model.ID
		}
		seen[model.ID] = true
		out = append(out, model)
	}
	return out
}

func normalizeModelID(id string) string {
	id = strings.TrimSpace(id)
	if idx := strings.LastIndex(id, "/"); idx >= 0 && idx+1 < len(id) {
		return strings.TrimSpace(id[idx+1:])
	}
	return id
}

