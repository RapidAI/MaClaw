package guiapp

import (
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// WarmupTools pre-builds and caches the tool definitions so the first user
// message does not pay the cost of BuildAll.
// Safe to call from a background goroutine.
func (h *IMMessageHandler) WarmupTools() {
	// Startup warmup is a definition/cache operation, not a user turn. Do not
	// send the synthetic "warmup" message through the semantic or legacy
	// router: that path records a fake intent, applies stale skill constraints,
	// and can populate the routing experience cache with unrelated tools.
	// getTools() materializes the definitions and is sufficient to warm the
	// local registry without creating a route decision.
	_ = h.getTools()
	log.Println("[WarmupTools] tool definitions pre-warmed (routing skipped)")
}

// WarmupHTTPConn sends a lightweight probe request to the configured LLM
// endpoint so the underlying TCP+TLS connection is established and pooled
// before the first real chat request.
func (h *IMMessageHandler) WarmupHTTPConn() {
	cfg := h.getMaclawLLMConfig()
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	if baseURL == "" {
		return
	}
	key := strings.TrimSpace(cfg.Key)
	ua := cfg.UserAgent()
	modelEndpoints := llm.BuildOpenAIModelsEndpointCandidates(corelib.NormalizeGLMCodingPlanOpenAIBaseURL(baseURL, ua), cfg.Protocol)
	if len(modelEndpoints) == 0 {
		return
	}
	endpoint := modelEndpoints[0]
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", ua)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	corelib.SetCodeGenClientNameHeaderIfNeededWithName(req, cfg.UserAgent())
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := h.client.Do(req)
	if err != nil {
		log.Printf("[Warmup] HTTP connection warmup failed: %v", err)
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	log.Printf("[Warmup] HTTP connection warmed up (status=%d)", resp.StatusCode)
}
