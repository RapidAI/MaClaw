package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/freeproxy"
)

var (
	freeProxyServer *freeproxy.Server
	freeProxyMu     sync.Mutex
	freeProxyCancel context.CancelFunc
)

const (
	freeProxyAddr    = ":10099"
	freeProviderName = "免费"
)

var freeProxyConfigOnce sync.Once
var freeProxyConfigPath string

// freeProxyConfigDir returns the directory for persisting dangbei auth data.
func freeProxyConfigDir() string {
	freeProxyConfigOnce.Do(func() {
		home, _ := os.UserHomeDir()
		freeProxyConfigPath = filepath.Join(home, ".maclaw", "freeproxy")
		os.MkdirAll(freeProxyConfigPath, 0700)
	})
	return freeProxyConfigPath
}

// StartFreeProxy starts the local free proxy server backed by 当贝 AI.
func (a *App) StartFreeProxy() string {
	freeProxyMu.Lock()
	defer freeProxyMu.Unlock()

	if freeProxyServer != nil {
		return "already running"
	}

	ctx, cancel := context.WithCancel(context.Background())
	freeProxyCancel = cancel
	freeProxyServer = freeproxy.NewServer(freeProxyAddr, freeProxyConfigDir())

	go func() {
		if err := freeProxyServer.Start(ctx); err != nil {
			log.Printf("[freeproxy] server error: %v", err)
		}
		freeProxyMu.Lock()
		freeProxyServer = nil
		freeProxyCancel = nil
		freeProxyMu.Unlock()
	}()

	return "started on " + freeProxyAddr
}

// StopFreeProxy stops the local free proxy server.
func (a *App) StopFreeProxy() string {
	freeProxyMu.Lock()
	defer freeProxyMu.Unlock()

	if freeProxyCancel != nil {
		freeProxyCancel()
		freeProxyCancel = nil
	}
	if freeProxyServer != nil {
		freeProxyServer.Stop()
		freeProxyServer = nil
	}
	return "stopped"
}

// IsFreeProxyRunning returns whether the free proxy server is running.
func (a *App) IsFreeProxyRunning() bool {
	freeProxyMu.Lock()
	defer freeProxyMu.Unlock()
	return freeProxyServer != nil
}

// IsDangbeiLoggedIn returns whether the user has a valid 当贝 AI cookie.
func (a *App) IsDangbeiLoggedIn() bool {
	freeProxyMu.Lock()
	srv := freeProxyServer
	freeProxyMu.Unlock()

	if srv != nil {
		return srv.Auth().HasAuth()
	}
	// Check persisted auth even if server isn't running
	auth := freeproxy.NewAuthStore(freeProxyConfigDir())
	auth.Load()
	return auth.HasAuth()
}

// DangbeiEnsureAuth checks if a valid persisted cookie exists.
// Returns "authenticated" if the cookie is valid, "need_login" otherwise.
// This allows the frontend to skip the browser login flow when a valid cookie exists.
func (a *App) DangbeiEnsureAuth() string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// If server is running and has a cookie, validate it directly
	freeProxyMu.Lock()
	srv := freeProxyServer
	freeProxyMu.Unlock()

	if srv != nil && srv.Auth().HasAuth() {
		if srv.Client().IsAuthenticated(ctx) {
			return "authenticated"
		}
		// Server cookie is stale — fall through to try persisted cookie
	}

	// Try loading persisted cookie from disk
	auth := freeproxy.NewAuthStore(freeProxyConfigDir())
	if err := auth.Load(); err != nil || !auth.HasAuth() {
		return "need_login"
	}

	// Skip re-validation if the persisted cookie is the same as the server's
	// (already proven invalid above)
	if srv != nil && srv.Auth().GetCookie() == auth.GetCookie() {
		return "need_login"
	}

	// Validate the persisted cookie via API
	client := freeproxy.NewDangbeiClient(auth)
	if !client.IsAuthenticated(ctx) {
		return "need_login"
	}

	// Cookie is valid — sync to running server if needed
	if srv != nil {
		srv.Auth().SetCookie(auth.GetCookie())
	}

	return "authenticated"
}

// ensureFreeProxyIfNeeded starts the free proxy server if the current
// LLM provider is "免费" and the server is not already running.
func (a *App) ensureFreeProxyIfNeeded() {
	data := a.GetMaclawLLMProviders()
	if data.Current == freeProviderName {
		a.StartFreeProxy()
	}
}

// DetectBrowser returns info about the detected browser (Chrome/Edge).
func (a *App) DetectBrowser() map[string]string {
	bi := freeproxy.DetectBrowser()
	if bi == nil {
		return map[string]string{"found": "false"}
	}
	return map[string]string{
		"found": "true",
		"name":  bi.Name,
		"path":  bi.Path,
	}
}

// DangbeiLogin launches a dedicated browser for the user to log in to 当贝 AI.
func (a *App) DangbeiLogin() error {
	return freeproxy.LoginViaBrowser()
}

// DangbeiFinishLogin extracts cookies from the browser after user login,
// saves them, and optionally starts the proxy.
// The debug port is auto-discovered from DevToolsActivePort — no parameter needed.
func (a *App) DangbeiFinishLogin() (string, error) {
	cookie, err := freeproxy.FinishLogin()
	if err != nil {
		return "", err
	}

	// Save cookie to the running server's auth store (if running)
	freeProxyMu.Lock()
	srv := freeProxyServer
	freeProxyMu.Unlock()

	if srv != nil {
		srv.Auth().SetCookie(cookie)
		srv.Auth().Save()
	} else {
		// Save to disk even if server isn't running yet
		auth := freeproxy.NewAuthStore(freeProxyConfigDir())
		auth.SetCookie(cookie)
		auth.Save()
	}

	return "登录成功", nil
}

// DetectChrome is kept for backward compatibility. Returns Chrome path or "".
func (a *App) DetectChrome() string {
	bi := freeproxy.DetectBrowser()
	if bi == nil {
		return ""
	}
	return bi.Path
}

// LaunchChromeDebug is kept for backward compatibility.
// Now launches a dedicated browser instance for 当贝 login.
func (a *App) LaunchChromeDebug() (string, error) {
	if err := a.DangbeiLogin(); err != nil {
		return "", err
	}
	return "browser launched", nil
}
