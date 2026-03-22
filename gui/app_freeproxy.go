package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"runtime"
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
	freeProxyAddr     = ":10099"
	chromeDebugURL    = "http://localhost:9222"
	freeProviderName  = "免费"
)

// StartFreeProxy starts the local free proxy server.
// It is safe to call multiple times — only one instance runs.
func (a *App) StartFreeProxy() string {
	freeProxyMu.Lock()
	defer freeProxyMu.Unlock()

	if freeProxyServer != nil {
		return "already running"
	}

	ctx, cancel := context.WithCancel(context.Background())
	freeProxyCancel = cancel
	freeProxyServer = freeproxy.NewServer(freeProxyAddr, chromeDebugURL)

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

// ensureFreeProxyIfNeeded starts the free proxy server if the current
// LLM provider is "免费" and the server is not already running.
func (a *App) ensureFreeProxyIfNeeded() {
	data := a.GetMaclawLLMProviders()
	if data.Current == freeProviderName {
		a.StartFreeProxy() // idempotent — returns "already running" if active
	}
}

// DetectChrome returns the Chrome executable path if found, or "" if not found.
func (a *App) DetectChrome() string {
	if runtime.GOOS == "windows" {
		candidates := []string{
			os.Getenv("ProgramFiles") + `\Google\Chrome\Application\chrome.exe`,
			os.Getenv("ProgramFiles(x86)") + `\Google\Chrome\Application\chrome.exe`,
			os.Getenv("LocalAppData") + `\Google\Chrome\Application\chrome.exe`,
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	// macOS / Linux: try PATH
	if p, err := exec.LookPath("google-chrome"); err == nil {
		return p
	}
	if p, err := exec.LookPath("chrome"); err == nil {
		return p
	}
	if runtime.GOOS == "darwin" {
		p := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// LaunchChromeDebug launches Chrome with --remote-debugging-port=9222.
// If the debug port is already reachable, it skips launching.
// Returns the Chrome path used, or an error.
func (a *App) LaunchChromeDebug() (string, error) {
	// Check if debug port is already reachable
	conn, err := net.DialTimeout("tcp", "localhost:9222", 2*time.Second)
	if err == nil {
		conn.Close()
		return "(already running)", nil
	}

	chromePath := a.DetectChrome()
	if chromePath == "" {
		return "", fmt.Errorf("Chrome not found")
	}
	cmd := exec.Command(chromePath, "--remote-debugging-port=9222")
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("launch Chrome: %w", err)
	}
	// Detach — don't wait for Chrome to exit
	go func() { cmd.Wait() }()
	log.Printf("[freeproxy] launched Chrome with debug port: %s", chromePath)
	return chromePath, nil
}
