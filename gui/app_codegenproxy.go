package main

import "log"

// StartCodeGenProxy is retained for compatibility with older UI bindings, but
// the local Anthropic adapter is currently disabled. TigerClaw Code now points
// Claude Code directly at CodeGen's remote Anthropic-compatible endpoint.
func (a *App) StartCodeGenProxy(upstreamURL, apiKey string, clientName ...string) (string, error) {
	log.Printf("[CodeGen Proxy] start skipped: local Anthropic proxy disabled; using remote CodeGen endpoint")
	return "disabled: using remote CodeGen endpoint", nil
}

// StopCodeGenProxy is a no-op while the legacy local adapter is disabled.
func (a *App) StopCodeGenProxy() string {
	log.Printf("[CodeGen Proxy] stop skipped: local Anthropic proxy disabled")
	return "stopped"
}

// IsCodeGenProxyRunning is always false while the legacy local adapter is disabled.
func (a *App) IsCodeGenProxyRunning() bool {
	return false
}
