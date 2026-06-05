package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// WarmupTools pre-builds and caches the tool definitions so the first user
// message does not pay the cost of BuildAll.
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
