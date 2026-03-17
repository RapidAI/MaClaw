package im

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

type mockPlugin struct {
	name      string
	caps      CapabilityDeclaration
	sentTexts []string
	sentCards []OutgoingMessage
	handler   func(msg IncomingMessage)
	mu        sync.Mutex
}

func (m *mockPlugin) Name() string                        { return m.name }
func (m *mockPlugin) Start(_ context.Context) error       { return nil }
func (m *mockPlugin) Stop(_ context.Context) error        { return nil }
func (m *mockPlugin) Capabilities() CapabilityDeclaration { return m.caps }
func (m *mockPlugin) ResolveUser(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (m *mockPlugin) SendImage(_ context.Context, _ UserTarget, _ string, _ string) error {
	return nil
}

func (m *mockPlugin) ReceiveMessage(handler func(msg IncomingMessage)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handler = handler
}

func (m *mockPlugin) SendText(_ context.Context, _ UserTarget, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentTexts = append(m.sentTexts, text)
	return nil
}

func (m *mockPlugin) SendCard(_ context.Context, _ UserTarget, card OutgoingMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentCards = append(m.sentCards, card)
	return nil
}

type mockIdentity struct {
	resolveFunc func(ctx context.Context, platform, uid string) (string, error)
}

func (m *mockIdentity) ResolveUser(ctx context.Context, platform, uid string) (string, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(ctx, platform, uid)
	}
	return "unified_" + uid, nil
}

type mockDeviceFinder struct {
	machineID     string
	llmConfigured bool
	found         bool
	sentMessages  []any
	mu            sync.Mutex
}

func (m *mockDeviceFinder) FindOnlineMachineForUser(_ context.Context, _ string) (string, bool, bool) {
	return m.machineID, m.llmConfigured, m.found
}

func (m *mockDeviceFinder) SendToMachine(_ string, msg any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentMessages = append(m.sentMessages, msg)
	return nil
}

// helper to build a basic adapter with a registered plugin and mock device finder.
func setupAdapter(plugin *mockPlugin, df *mockDeviceFinder) *Adapter {
	router := NewMessageRouter(df)
	identity := &mockIdentity{}
	adapter := NewAdapter(router, identity)
	_ = adapter.RegisterPlugin(plugin)
	return adapter
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRegisterPlugin_Success(t *testing.T) {
	df := &mockDeviceFinder{}
	router := NewMessageRouter(df)
	adapter := NewAdapter(router, &mockIdentity{})
	plugin := &mockPlugin{name: "test"}
	if err := adapter.RegisterPlugin(plugin); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := adapter.GetPlugin("test"); got == nil {
		t.Fatal("expected plugin to be registered")
	}
	router.Stop()
}

func TestRegisterPlugin_EmptyName(t *testing.T) {
	df := &mockDeviceFinder{}
	router := NewMessageRouter(df)
	adapter := NewAdapter(router, &mockIdentity{})
	plugin := &mockPlugin{name: ""}
	if err := adapter.RegisterPlugin(plugin); err == nil {
		t.Fatal("expected error for empty plugin name")
	}
	router.Stop()
}

func TestRegisterPlugin_Duplicate(t *testing.T) {
	df := &mockDeviceFinder{}
	router := NewMessageRouter(df)
	adapter := NewAdapter(router, &mockIdentity{})
	_ = adapter.RegisterPlugin(&mockPlugin{name: "dup"})
	if err := adapter.RegisterPlugin(&mockPlugin{name: "dup"}); err == nil {
		t.Fatal("expected error for duplicate plugin")
	}
	router.Stop()
}

func TestRateLimiter_AllowsUpToMax(t *testing.T) {
	rl := newRateLimiter()
	for i := 0; i < rateLimitMaxTokens; i++ {
		if !rl.allow("user1") {
			t.Fatalf("expected allow at request %d", i+1)
		}
	}
	// 31st request should be denied.
	if rl.allow("user1") {
		t.Fatal("expected rate limit to deny 31st request")
	}
}

func TestRateLimiter_RefillsAfterInterval(t *testing.T) {
	rl := newRateLimiter()
	// Exhaust tokens.
	for i := 0; i < rateLimitMaxTokens; i++ {
		rl.allow("user1")
	}
	// Manually set refillAt to the past.
	rl.mu.Lock()
	rl.buckets["user1"].refillAt = time.Now().Add(-1 * time.Second)
	rl.mu.Unlock()

	if !rl.allow("user1") {
		t.Fatal("expected allow after refill")
	}
}

func TestRateLimiter_IndependentUsers(t *testing.T) {
	rl := newRateLimiter()
	// Exhaust user1.
	for i := 0; i < rateLimitMaxTokens; i++ {
		rl.allow("user1")
	}
	// user2 should still be allowed.
	if !rl.allow("user2") {
		t.Fatal("expected user2 to be allowed independently")
	}
}

func TestHandleMessage_IdentityFailure(t *testing.T) {
	plugin := &mockPlugin{name: "test", caps: CapabilityDeclaration{SupportsRichCard: false}}
	df := &mockDeviceFinder{machineID: "m1", llmConfigured: true, found: true}
	router := NewMessageRouter(df)
	defer router.Stop()

	identity := &mockIdentity{
		resolveFunc: func(_ context.Context, _, _ string) (string, error) {
			return "", fmt.Errorf("unbound user")
		},
	}
	adapter := NewAdapter(router, identity)
	_ = adapter.RegisterPlugin(plugin)

	adapter.HandleMessage(context.Background(), IncomingMessage{
		PlatformName: "test",
		PlatformUID:  "uid1",
		Text:         "hello",
	})

	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if len(plugin.sentTexts) == 0 {
		t.Fatal("expected error response")
	}
	if !containsStr(plugin.sentTexts[0], "身份验证失败") {
		t.Fatalf("unexpected response: %s", plugin.sentTexts[0])
	}
}

func TestHandleMessage_RateLimited(t *testing.T) {
	plugin := &mockPlugin{name: "test", caps: CapabilityDeclaration{SupportsRichCard: false}}
	df := &mockDeviceFinder{machineID: "m1", llmConfigured: true, found: true}
	router := NewMessageRouter(df)
	defer router.Stop()

	adapter := NewAdapter(router, &mockIdentity{})
	_ = adapter.RegisterPlugin(plugin)

	// Exhaust rate limit for unified_uid1.
	adapter.limiter.mu.Lock()
	adapter.limiter.buckets["unified_uid1"] = &rateBucket{
		tokens:   0,
		refillAt: time.Now().Add(1 * time.Minute),
	}
	adapter.limiter.mu.Unlock()

	adapter.HandleMessage(context.Background(), IncomingMessage{
		PlatformName: "test",
		PlatformUID:  "uid1",
		Text:         "hello",
	})

	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if len(plugin.sentTexts) == 0 {
		t.Fatal("expected rate limit response")
	}
	if !containsStr(plugin.sentTexts[0], "请求过于频繁") {
		t.Fatalf("unexpected response: %s", plugin.sentTexts[0])
	}
}

func TestHandleMessage_DeviceOffline(t *testing.T) {
	plugin := &mockPlugin{name: "test", caps: CapabilityDeclaration{SupportsRichCard: false}}
	df := &mockDeviceFinder{found: false} // no online device
	router := NewMessageRouter(df)
	defer router.Stop()

	adapter := NewAdapter(router, &mockIdentity{})
	_ = adapter.RegisterPlugin(plugin)

	adapter.HandleMessage(context.Background(), IncomingMessage{
		PlatformName: "test",
		PlatformUID:  "uid1",
		Text:         "hello",
	})

	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if len(plugin.sentTexts) == 0 {
		t.Fatal("expected offline response")
	}
	if !containsStr(plugin.sentTexts[0], "设备不在线") {
		t.Fatalf("unexpected response: %s", plugin.sentTexts[0])
	}
}

func TestHandleMessage_LLMNotConfigured(t *testing.T) {
	plugin := &mockPlugin{name: "test", caps: CapabilityDeclaration{SupportsRichCard: false}}
	df := &mockDeviceFinder{machineID: "m1", llmConfigured: false, found: true}
	router := NewMessageRouter(df)
	defer router.Stop()

	adapter := NewAdapter(router, &mockIdentity{})
	_ = adapter.RegisterPlugin(plugin)

	adapter.HandleMessage(context.Background(), IncomingMessage{
		PlatformName: "test",
		PlatformUID:  "uid1",
		Text:         "hello",
	})

	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if len(plugin.sentTexts) == 0 {
		t.Fatal("expected LLM not configured response")
	}
	if !containsStr(plugin.sentTexts[0], "Agent 未就绪") {
		t.Fatalf("unexpected response: %s", plugin.sentTexts[0])
	}
}

func TestHandleMessage_AgentResponse(t *testing.T) {
	plugin := &mockPlugin{name: "test", caps: CapabilityDeclaration{SupportsRichCard: true}}
	df := &mockDeviceFinder{machineID: "m1", llmConfigured: true, found: true}
	router := NewMessageRouter(df)
	defer router.Stop()

	adapter := NewAdapter(router, &mockIdentity{})
	_ = adapter.RegisterPlugin(plugin)

	// Send message in a goroutine since RouteToAgent blocks.
	go func() {
		adapter.HandleMessage(context.Background(), IncomingMessage{
			PlatformName: "test",
			PlatformUID:  "uid1",
			Text:         "查看会话",
		})
	}()

	// Wait a bit for the message to be routed and pending request created.
	time.Sleep(50 * time.Millisecond)

	// Find the pending request and simulate agent response.
	router.mu.Lock()
	var reqID string
	for id := range router.pendingReqs {
		reqID = id
		break
	}
	router.mu.Unlock()

	if reqID == "" {
		t.Fatal("expected a pending request")
	}

	router.HandleAgentResponse(reqID, &AgentResponse{
		Text: "当前有 3 个活跃会话。",
	})

	// Wait for the response to be delivered.
	time.Sleep(100 * time.Millisecond)

	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if len(plugin.sentCards) == 0 {
		t.Fatal("expected card response from agent")
	}
	if !containsStr(plugin.sentCards[0].FallbackText, "3 个活跃会话") {
		t.Fatalf("unexpected response: %+v", plugin.sentCards[0])
	}
}

func TestTruncateAtLine(t *testing.T) {
	text := "line1\nline2\nline3\nline4"
	result := truncateAtLine(text, 15)
	if len(result) > 15+5 { // some tolerance for the ellipsis
		t.Fatalf("truncated text too long: %q", result)
	}
	if !containsStr(result, "…") {
		t.Fatalf("expected ellipsis in truncated text: %q", result)
	}
}

// helper
func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
