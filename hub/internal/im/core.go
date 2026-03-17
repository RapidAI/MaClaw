package im

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Abstraction interfaces
// ---------------------------------------------------------------------------

// IdentityResolver abstracts the Identity_Service for user mapping.
type IdentityResolver interface {
	ResolveUser(ctx context.Context, platformName, platformUID string) (string, error)
}

// ---------------------------------------------------------------------------
// Rate limiter (token-bucket, 30 tokens/min per user)
// ---------------------------------------------------------------------------

const (
	rateLimitMaxTokens = 30
	rateLimitRefill    = time.Minute
)

// rateBucket is a simple per-user token bucket.
type rateBucket struct {
	tokens   int
	refillAt time.Time
}

// rateLimiter manages per-user rate limiting.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
	stopCh  chan struct{}
}

func newRateLimiter() *rateLimiter {
	rl := &rateLimiter{
		buckets: make(map[string]*rateBucket),
		stopCh:  make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

// cleanupLoop 定期清理过期的 rate limiter bucket，防止内存无限增长
func (rl *rateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.evictStale()
		case <-rl.stopCh:
			return
		}
	}
}

// evictStale 移除超过 10 分钟未活跃的 bucket
func (rl *rateLimiter) evictStale() {
	cutoff := time.Now().Add(-10 * time.Minute)
	rl.mu.Lock()
	defer rl.mu.Unlock()
	for uid, b := range rl.buckets {
		if b.refillAt.Before(cutoff) {
			delete(rl.buckets, uid)
		}
	}
}

// allow returns true if the user has remaining tokens. It refills the bucket
// if the refill interval has elapsed.
func (rl *rateLimiter) allow(userID string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[userID]
	now := time.Now()
	if !ok {
		rl.buckets[userID] = &rateBucket{
			tokens:   rateLimitMaxTokens - 1,
			refillAt: now.Add(rateLimitRefill),
		}
		return true
	}

	// Refill if interval elapsed.
	if now.After(b.refillAt) {
		b.tokens = rateLimitMaxTokens
		b.refillAt = now.Add(rateLimitRefill)
	}

	if b.tokens <= 0 {
		return false
	}
	b.tokens--
	return true
}

// ---------------------------------------------------------------------------
// Adapter — the IM Adapter Core (Agent Passthrough mode)
// ---------------------------------------------------------------------------

// Adapter is the central IM adapter that manages registered IM plugins,
// routes incoming messages through identity mapping, rate limiting, and
// then transparently relays to the MaClaw Agent via MessageRouter.
type Adapter struct {
	mu      sync.RWMutex
	plugins map[string]IMPlugin

	messageRouter *MessageRouter
	identity      IdentityResolver
	limiter       *rateLimiter
}

// NewAdapter creates a new IM Adapter with the given MessageRouter.
func NewAdapter(router *MessageRouter, identity IdentityResolver) *Adapter {
	return &Adapter{
		plugins:       make(map[string]IMPlugin),
		messageRouter: router,
		identity:      identity,
		limiter:       newRateLimiter(),
	}
}

// SetIdentityResolver replaces the identity resolver after construction.
// This is useful when the resolver depends on the adapter itself (e.g.
// PluginIdentityResolver).
func (a *Adapter) SetIdentityResolver(resolver IdentityResolver) {
	a.identity = resolver
}

// RegisterPlugin registers an IM plugin with the adapter.
// It validates that the plugin implements all required interface methods
// by checking that Name() returns a non-empty string.
func (a *Adapter) RegisterPlugin(plugin IMPlugin) error {
	name := plugin.Name()
	if name == "" {
		return fmt.Errorf("im: plugin Name() returned empty string, refusing to register")
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if _, exists := a.plugins[name]; exists {
		return fmt.Errorf("im: plugin %q already registered", name)
	}

	// Wire the message handler so the plugin routes messages to us.
	plugin.ReceiveMessage(func(msg IncomingMessage) {
		a.HandleMessage(context.Background(), msg)
	})

	a.plugins[name] = plugin
	log.Printf("[IM Adapter] registered plugin: %s", name)
	return nil
}

// GetPlugin returns the registered plugin by name, or nil.
func (a *Adapter) GetPlugin(name string) IMPlugin {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.plugins[name]
}

// HandleMessage is the main entry point called by IM plugins when they
// receive a message. It orchestrates the Agent Passthrough pipeline:
//
//  1. Identity mapping (platformUID → unifiedUserID)
//  2. Rate limiting (30 req/min per user)
//  3. Route to MaClaw Agent via MessageRouter
//  4. Response formatting & delivery based on CapabilityDeclaration
func (a *Adapter) HandleMessage(ctx context.Context, msg IncomingMessage) {
	plugin := a.GetPlugin(msg.PlatformName)
	if plugin == nil {
		log.Printf("[IM Adapter] no plugin registered for platform %q", msg.PlatformName)
		return
	}

	target := UserTarget{PlatformUID: msg.PlatformUID}

	// 1. Identity mapping
	unifiedID, err := a.identity.ResolveUser(ctx, msg.PlatformName, msg.PlatformUID)
	if err != nil {
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 403,
			StatusIcon: "🔒",
			Title:      "身份验证失败",
			Body:       fmt.Sprintf("无法识别您的身份，请先完成绑定。\n错误: %s", err.Error()),
		})
		return
	}
	msg.UnifiedUserID = unifiedID
	target.UnifiedUserID = unifiedID

	// 2. Rate limiting
	if !a.limiter.allow(unifiedID) {
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 429,
			StatusIcon: "⏳",
			Title:      "请求过于频繁",
			Body:       "您的操作频率已超过限制（每分钟 30 次），请稍后再试。",
		})
		return
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	// 3. Route to MaClaw Agent via MessageRouter
	resp, err := a.messageRouter.RouteToAgent(ctx, unifiedID, msg.PlatformName, text)
	if err != nil {
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 500,
			StatusIcon: "❌",
			Title:      "路由失败",
			Body:       fmt.Sprintf("无法将消息路由到 Agent: %s", err.Error()),
		})
		return
	}

	// 4. Format and deliver response
	a.sendResponse(ctx, plugin, target, resp)
}

// ---------------------------------------------------------------------------
// Response formatting & delivery (capability-based format selection)
// ---------------------------------------------------------------------------

// sendResponse delivers a GenericResponse to the user via the appropriate
// plugin method, choosing the best format based on CapabilityDeclaration.
//
// Strategy:
//   - If plugin supports rich cards → SendCard with OutgoingMessage
//   - Otherwise → SendText with FallbackText
func (a *Adapter) sendResponse(ctx context.Context, plugin IMPlugin, target UserTarget, resp *GenericResponse) {
	caps := plugin.Capabilities()
	out := resp.ToOutgoingMessage()

	if caps.SupportsRichCard {
		if err := plugin.SendCard(ctx, target, out); err != nil {
			log.Printf("[IM Adapter] SendCard failed for %s, falling back to text: %v", plugin.Name(), err)
			// Fallback to text on card send failure.
			_ = plugin.SendText(ctx, target, out.FallbackText)
		}
		return
	}

	// No rich card support — send plain text.
	text := out.FallbackText
	if text == "" {
		text = resp.ToFallbackText()
	}

	// Truncate if platform has a max text length.
	if caps.MaxTextLength > 0 && len(text) > caps.MaxTextLength {
		text = truncateAtLine(text, caps.MaxTextLength)
	}

	_ = plugin.SendText(ctx, target, text)
}

// truncateAtLine truncates text to maxLen at a line boundary and appends "…".
func truncateAtLine(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	// Reserve space for the ellipsis suffix.
	cutoff := maxLen - len("…")
	if cutoff < 0 {
		cutoff = 0
	}
	// Find the last newline before cutoff.
	idx := strings.LastIndex(text[:cutoff], "\n")
	if idx < 0 {
		idx = cutoff
	}
	return text[:idx] + "\n…"
}
