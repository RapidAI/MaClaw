package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// syncAgentNetTools dynamically registers or unregisters AgentNet tools
// based on whether the AgentNet daemon is currently running.
func (h *IMMessageHandler) syncAgentNetTools() {
	if h.registry == nil {
		return
	}
	running := h.getAgentNetClient() != nil && h.getAgentNetClient().IsRunning()
	_, hasSearch := h.registry.Get("agentnet_search")

	if running && !hasSearch {
		h.registry.Register(RegisteredTool{
			Name:        "agentnet_search",
			Description: "Search AgentNet P2P knowledge entries and return matching items.",
			Category:    ToolCategoryBuiltin,
			Tags:        []string{"agentnet", "search", "knowledge", "p2p"},
			Status:      RegToolAvailable,
			InputSchema: map[string]interface{}{
				"query": map[string]string{"type": "string", "description": "Search query."},
			},
			Required: []string{"query"},
			Source:   "agentnet",
			Handler:  func(args map[string]interface{}) string { return h.toolAgentNetSearch(args) },
		})
		h.registry.Register(RegisteredTool{
			Name:        "agentnet_publish",
			Description: "Publish a knowledge entry to AgentNet P2P.",
			Category:    ToolCategoryBuiltin,
			Tags:        []string{"agentnet", "publish", "knowledge", "p2p"},
			Status:      RegToolAvailable,
			InputSchema: map[string]interface{}{
				"title": map[string]string{"type": "string", "description": "Knowledge title."},
				"body":  map[string]string{"type": "string", "description": "Knowledge body in Markdown."},
			},
			Required: []string{"title", "body"},
			Source:   "agentnet",
			Handler:  func(args map[string]interface{}) string { return h.toolAgentNetPublish(args) },
		})
	} else if !running && hasSearch {
		h.registry.Unregister("agentnet_search")
		h.registry.Unregister("agentnet_publish")
	}
}

// WarmupTools pre-builds and caches the tool definitions so the first user
// message does not pay the cost of syncAgentNetTools + BuildAll.
// Safe to call from a background goroutine.
func (h *IMMessageHandler) WarmupTools() {
	allTools := h.getTools()
	if h.toolRouter != nil {
		_ = h.routeTools("warmup", allTools)
		log.Println("[WarmupTools] tool routing cache pre-warmed")
	}
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
	endpoint := baseURL + "/models"
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", ua)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
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
