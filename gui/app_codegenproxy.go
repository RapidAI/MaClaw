package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codegenproxy"
)

var (
	codegenProxyServer *codegenproxy.Server
	codegenProxyMu     sync.Mutex
	codegenProxyCancel context.CancelFunc
)

const codegenProxyAddr = ":5001"

// StartCodeGenProxy starts the local Anthropic→OpenAI protocol conversion proxy
// for CodeGen. This allows Claude Code to communicate with CodeGen's OpenAI API
// via the Anthropic Messages protocol.
func (a *App) StartCodeGenProxy(upstreamURL, apiKey string) (string, error) {
	codegenProxyMu.Lock()
	defer codegenProxyMu.Unlock()

	if codegenProxyServer != nil {
		// Update upstream config on the running server
		codegenProxyServer.SetUpstream(upstreamURL, apiKey)
		return "already running (upstream updated)", nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	srv := codegenproxy.NewServer(codegenProxyAddr)
	srv.SetUpstream(upstreamURL, apiKey)

	startErr := make(chan error, 1)
	go func() {
		err := srv.Start(ctx)
		startErr <- err
		codegenProxyMu.Lock()
		codegenProxyServer = nil
		codegenProxyCancel = nil
		codegenProxyMu.Unlock()
	}()

	select {
	case err := <-startErr:
		cancel()
		if err != nil {
			return "", fmt.Errorf("CodeGen 代理启动失败: %w", err)
		}
		return "", fmt.Errorf("CodeGen 代理启动失败: 服务器意外退出")
	case <-time.After(300 * time.Millisecond):
		codegenProxyServer = srv
		codegenProxyCancel = cancel
		return "started on " + codegenProxyAddr, nil
	}
}

// StopCodeGenProxy stops the local CodeGen protocol conversion proxy.
func (a *App) StopCodeGenProxy() string {
	codegenProxyMu.Lock()
	defer codegenProxyMu.Unlock()

	if codegenProxyCancel != nil {
		codegenProxyCancel()
		codegenProxyCancel = nil
	}
	if codegenProxyServer != nil {
		codegenProxyServer.Stop()
		codegenProxyServer = nil
	}
	return "stopped"
}

// IsCodeGenProxyRunning returns whether the CodeGen proxy server is running.
func (a *App) IsCodeGenProxyRunning() bool {
	codegenProxyMu.Lock()
	defer codegenProxyMu.Unlock()
	return codegenProxyServer != nil
}

// ensureCodeGenProxyIfNeeded starts the CodeGen proxy if the current
// MaClaw LLM provider is "CodeGen" with SSO auth and the proxy is not running.
// Called during app startup.
func (a *App) ensureCodeGenProxyIfNeeded() {
	data := a.GetMaclawLLMProviders()

	// Find the CodeGen SSO provider
	var codegenProvider *MaclawLLMProvider
	for i := range data.Providers {
		if data.Providers[i].Name == codegenProviderName && data.Providers[i].AuthType == "sso" {
			codegenProvider = &data.Providers[i]
			break
		}
	}
	if codegenProvider == nil {
		return
	}
	if codegenProvider.URL == "" || codegenProvider.Key == "" {
		return
	}

	if a.IsCodeGenProxyRunning() {
		// Just update upstream in case credentials changed
		codegenProxyMu.Lock()
		if codegenProxyServer != nil {
			codegenProxyServer.SetUpstream(codegenProvider.URL, codegenProvider.Key)
		}
		codegenProxyMu.Unlock()
		return
	}

	result, err := a.StartCodeGenProxy(codegenProvider.URL, codegenProvider.Key)
	if err != nil {
		log.Printf("[CodeGen Proxy] auto-start failed: %v", err)
	} else {
		log.Printf("[CodeGen Proxy] auto-start: %s", result)
	}
}
