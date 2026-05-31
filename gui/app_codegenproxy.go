package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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
	if err := a.ensureWorkflowAllowsRemoteToolCall("bash", map[string]interface{}{"command": "start codegen proxy", "upstream_url": upstreamURL}); err != nil {
		return "", err
	}
	codegenProxyMu.Lock()
	defer codegenProxyMu.Unlock()

	if codegenProxyServer != nil {
		// Update upstream config on the running server
		codegenProxyServer.SetUpstream(upstreamURL, apiKey)
		log.Printf("[codegen-proxy] upstream updated on running proxy: url=%s", upstreamURL)
		return "already running (upstream updated)", nil
	}

	log.Printf("[codegen-proxy] ▶ Starting CodeGen proxy: addr=%s, upstream=%s, time=%s",
		codegenProxyAddr, upstreamURL, time.Now().Format(time.RFC3339))

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
		if err != nil {
			log.Printf("[codegen-proxy] ◼ proxy server exited with error: %v", err)
		} else {
			log.Printf("[codegen-proxy] ◼ proxy server exited normally")
		}
	}()

	select {
	case err := <-startErr:
		cancel()
		if err != nil {
			log.Printf("[codegen-proxy] ✖ startup failed: %v", err)
			return "", fmt.Errorf("CodeGen 代理启动失败: %w", err)
		}
		log.Printf("[codegen-proxy] ✖ server exited unexpectedly during startup")
		return "", fmt.Errorf("CodeGen 代理启动失败: 服务器意外退出")
	case <-time.After(300 * time.Millisecond):
		codegenProxyServer = srv
		codegenProxyCancel = cancel
		log.Printf("[codegen-proxy] ✔ proxy started successfully on %s", codegenProxyAddr)
		return "started on " + codegenProxyAddr, nil
	}
}

// StopCodeGenProxy stops the local CodeGen protocol conversion proxy.
func (a *App) StopCodeGenProxy() string {
	codegenProxyMu.Lock()
	defer codegenProxyMu.Unlock()

	log.Printf("[codegen-proxy] ◼ Stopping CodeGen proxy, time=%s", time.Now().Format(time.RFC3339))

	if codegenProxyCancel != nil {
		codegenProxyCancel()
		codegenProxyCancel = nil
	}
	if codegenProxyServer != nil {
		codegenProxyServer.Stop()
		codegenProxyServer = nil
	}
	log.Printf("[codegen-proxy] ◼ CodeGen proxy stopped")
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
	var codegenProvider *corelib.MaclawLLMProvider
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
