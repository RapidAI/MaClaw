package llm

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

func bindOpenCodeSession(ctx context.Context, cfg corelib.MaclawLLMConfig, messages any) corelib.MaclawLLMConfig {
	cfg = corelib.BindOpenCodeSessionID(ctx, cfg)
	if strings.TrimSpace(cfg.SessionID) != "" || !corelib.ShouldAttachOpenCodeSession(cfg) {
		return cfg
	}
	trace, _ := RequestTraceFromContext(ctx)
	cfg.SessionID = corelib.StableOpenCodeSessionID(trace.OwnerID, trace.LoopID, corelib.OpenCodeConversationSeedFromMessages(messages))
	return cfg
}

func bindOpenCodeSessionFromJSONBody(ctx context.Context, cfg corelib.MaclawLLMConfig, body []byte) corelib.MaclawLLMConfig {
	var payload map[string]any
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return bindOpenCodeSession(ctx, cfg, nil)
	}
	return bindOpenCodeSession(ctx, cfg, corelib.OpenCodeConversationSeed(payload))
}
