package llmservice

import (
	"context"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

func withOpenCodeSessionContext(ctx context.Context, req *ProxyRequest) context.Context {
	return corelib.WithOpenCodeSessionID(ctx, openCodeSessionIDForProxy(req))
}

func openCodeSessionIDForProxy(req *ProxyRequest) string {
	if req == nil {
		return corelib.ResolveOpenCodeSessionID("")
	}
	if req.Header != nil {
		if incoming := strings.TrimSpace(req.Header.Get(corelib.OpenCodeSessionHeader)); incoming != "" {
			return incoming
		}
	}
	return corelib.StableOpenCodeSessionID(req.HubID, req.TenantID, corelib.OpenCodeConversationSeed(req.Body))
}
