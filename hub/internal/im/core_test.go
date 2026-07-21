package im

import (
	"context"
	"fmt"
	"strings"
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
func (m *mockPlugin) SendFile(_ context.Context, _ UserTarget, _, _, _ string) error {
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

func (m *mockPlugin) lastText() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sentTexts) == 0 {
		return ""
	}
	return m.sentTexts[len(m.sentTexts)-1]
}

func (m *mockPlugin) textCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sentTexts)
}

type mockVoicePlugin struct {
	mockPlugin
	sentVoices []string
}

func (m *mockVoicePlugin) SendVoice(_ context.Context, _ UserTarget, voiceData, _, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentVoices = append(m.sentVoices, voiceData)
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

type mockTenantIdentity struct {
	tenantID string
	userID   string
	err      error
	seen     string
}

func (m *mockTenantIdentity) ResolveUser(ctx context.Context, platform, uid string) (string, error) {
	_, userID, err := m.ResolveUserWithTenant(ctx, platform, uid)
	return userID, err
}

func (m *mockTenantIdentity) ResolveUserWithTenant(ctx context.Context, platform, uid string) (string, string, error) {
	m.seen = tenantIDFromContext(ctx)
	if m.err != nil {
		return "", "", m.err
	}
	return m.tenantID, m.userID, nil
}

type mockDeviceFinder struct {
	machineID     string
	llmConfigured bool
	found         bool
	sentMessages  []any
	mu            sync.Mutex
	// For multi-machine tests.
	allMachines []OnlineMachineInfo
}

func (m *mockDeviceFinder) FindOnlineMachineForUser(_ context.Context, _ string) (string, bool, bool) {
	return m.machineID, m.llmConfigured, m.found
}

func (m *mockDeviceFinder) FindAllOnlineMachinesForUser(_ context.Context, _ string) []OnlineMachineInfo {
	if len(m.allMachines) > 0 {
		return m.allMachines
	}
	if !m.found {
		return nil
	}
	return []OnlineMachineInfo{{MachineID: m.machineID, Name: "default", LLMConfigured: m.llmConfigured}}
}

func (m *mockDeviceFinder) FindOnlineMachineByName(_ context.Context, _ string, name string) (string, bool) {
	for _, machine := range m.allMachines {
		if strings.EqualFold(machine.Name, name) {
			return machine.MachineID, true
		}
	}
	if m.found && strings.EqualFold(name, "default") {
		return m.machineID, true
	}
	return "", false
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
	resetIncomingDedup()
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
	if !containsStr(plugin.sentTexts[0], "尚未绑定账号") {
		t.Fatalf("unexpected response: %s", plugin.sentTexts[0])
	}
}

func TestHandleMessage_RateLimited(t *testing.T) {
	resetIncomingDedup()
	plugin := &mockPlugin{name: "test", caps: CapabilityDeclaration{SupportsRichCard: false}}
	df := &mockDeviceFinder{machineID: "m1", llmConfigured: true, found: true}
	router := NewMessageRouter(df)
	defer router.Stop()

	adapter := NewAdapter(router, &mockIdentity{})
	_ = adapter.RegisterPlugin(plugin)

	// Exhaust rate limit for unified_uid1.
	adapter.limiter.mu.Lock()
	adapter.limiter.buckets[tenantUserRuntimeKey("", "unified_uid1")] = &rateBucket{
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

func TestHandleMessage_UsesIncomingTenantHintForIdentity(t *testing.T) {
	resetIncomingDedup()
	plugin := &mockPlugin{name: "test", caps: CapabilityDeclaration{SupportsRichCard: false}}
	df := &mockDeviceFinder{machineID: "m1", llmConfigured: true, found: true}
	router := NewMessageRouter(df)
	defer router.Stop()

	identity := &mockTenantIdentity{tenantID: "tenant_a", userID: "u1"}
	adapter := NewAdapter(router, identity)
	_ = adapter.RegisterPlugin(plugin)
	adapter.limiter.mu.Lock()
	adapter.limiter.buckets[tenantUserRuntimeKey("tenant_a", "u1")] = &rateBucket{
		tokens:   0,
		refillAt: time.Now().Add(1 * time.Minute),
	}
	adapter.limiter.mu.Unlock()

	adapter.HandleMessage(context.Background(), IncomingMessage{
		TenantID:     "tenant_a",
		PlatformName: "test",
		PlatformUID:  "uid1",
		Text:         "hello",
	})

	if identity.seen != "tenant_a" {
		t.Fatalf("expected resolver to see tenant_a, got %q", identity.seen)
	}
	plugin.mu.Lock()
	rateLimited := len(plugin.sentTexts) > 0 && containsStr(plugin.sentTexts[0], "请求过于频繁")
	plugin.mu.Unlock()
	if !rateLimited {
		t.Fatal("expected tenant_a rate limit response")
	}
	if !adapter.limiter.allow(tenantUserRuntimeKey("tenant_b", "u1")) {
		t.Fatal("expected tenant_b user runtime bucket to remain independent")
	}
}

func TestIncomingDedupKeyIncludesTenant(t *testing.T) {
	resetIncomingDedup()
	msgA := IncomingMessage{TenantID: "tenant_a", PlatformName: "test", PlatformUID: "uid1", MessageID: "msg-1", Text: "hello"}
	msgB := IncomingMessage{TenantID: "tenant_b", PlatformName: "test", PlatformUID: "uid1", MessageID: "msg-1", Text: "hello"}

	if incomingDedupKey(msgA) == incomingDedupKey(msgB) {
		t.Fatalf("dedup key should include tenant: %q", incomingDedupKey(msgA))
	}
	if isDuplicateIncoming(msgA) {
		t.Fatal("first tenant_a message should not be duplicate")
	}
	if isDuplicateIncoming(msgB) {
		t.Fatal("same platform message id in tenant_b should not be duplicate")
	}
	if !isDuplicateIncoming(msgA) {
		t.Fatal("second tenant_a message should be duplicate")
	}
}

func TestHandleMessage_DeviceOffline(t *testing.T) {
	resetIncomingDedup()
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
	resetIncomingDedup()
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
	resetIncomingDedup()
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

func TestHandleMessage_StartMenuConfirmRejectsBroadcastWithoutConsumingState(t *testing.T) {
	resetIncomingDedup()
	plugin := &mockPlugin{name: "test", caps: CapabilityDeclaration{SupportsRichCard: false}}
	df := &mockDeviceFinder{allMachines: []OnlineMachineInfo{
		{MachineID: "m1", Name: "one", LLMConfigured: true},
		{MachineID: "m2", Name: "two", LLMConfigured: true},
	}}
	router := NewMessageRouter(df)
	defer router.Stop()
	adapter := NewAdapter(router, &mockIdentity{})
	defer close(adapter.limiter.stopCh)
	if err := adapter.RegisterPlugin(plugin); err != nil {
		t.Fatal(err)
	}

	adapter.startMenu = newStartMenuService(&StartMenuTemplateStore{})
	key := tenantUserRuntimeKey("", "unified_uid1")
	adapter.startMenu.states[key] = &startMenuState{
		Templates: []startMenuTemplate{{Title: "Deploy", Body: "deploy"}},
		Selected:  0,
		Confirm:   true,
		UpdatedAt: time.Now(),
	}
	router.mu.Lock()
	router.selectedMachine[key] = broadcastMachineID
	router.mu.Unlock()

	adapter.HandleMessage(context.Background(), IncomingMessage{
		PlatformName: "test",
		PlatformUID:  "uid1",
		Text:         "/confirm",
	})

	if !strings.Contains(plugin.lastText(), "请先选择一台设备") {
		t.Fatalf("expected broadcast warning, got %q", plugin.lastText())
	}
	if !adapter.startMenu.awaitingConfirmation("", "unified_uid1") {
		t.Fatal("broadcast rejection must preserve the confirmed shortcut state")
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

func TestSendResponseSendsWeixinVoiceAsPrimary(t *testing.T) {
	plugin := &mockVoicePlugin{mockPlugin: mockPlugin{
		name: "weixin",
		caps: CapabilityDeclaration{SupportsVoice: true, SupportsFile: true, SupportsMarkdown: false},
	}}
	adapter := &Adapter{}
	adapter.sendResponse(context.Background(), plugin, UserTarget{PlatformUID: "user-1"}, &GenericResponse{
		Body:          "读完啦",
		VoiceData:     "dm9pY2U=",
		VoiceFileName: "voice.wav",
		VoiceMimeType: "audio/wav",
	})
	if len(plugin.sentVoices) != 1 {
		t.Fatalf("sentVoices = %d, want 1", len(plugin.sentVoices))
	}
	if len(plugin.sentTexts) != 0 {
		t.Fatalf("sentTexts = %d, want 0", len(plugin.sentTexts))
	}
}

func TestSendResponseKeepsVoiceAfterTextForOtherIM(t *testing.T) {
	plugin := &mockVoicePlugin{mockPlugin: mockPlugin{
		name: "telegram",
		caps: CapabilityDeclaration{SupportsVoice: true, SupportsFile: true, SupportsMarkdown: false},
	}}
	adapter := &Adapter{}
	adapter.sendResponse(context.Background(), plugin, UserTarget{PlatformUID: "user-1"}, &GenericResponse{
		Body:          "读完啦",
		VoiceData:     "dm9pY2U=",
		VoiceFileName: "voice.ogg",
		VoiceMimeType: "audio/ogg",
	})
	if len(plugin.sentTexts) != 1 {
		t.Fatalf("sentTexts = %d, want 1", len(plugin.sentTexts))
	}
	if len(plugin.sentVoices) != 1 {
		t.Fatalf("sentVoices = %d, want 1", len(plugin.sentVoices))
	}
}

func TestDeliverSingleResponseSendsWeixinVoiceAsPrimary(t *testing.T) {
	plugin := &mockVoicePlugin{mockPlugin: mockPlugin{
		name: "weixin",
		caps: CapabilityDeclaration{SupportsVoice: true, SupportsFile: true, SupportsMarkdown: false},
	}}
	adapter := &Adapter{}
	adapter.deliverSingleResponse(context.Background(), plugin, UserTarget{PlatformUID: "user-1"}, &GenericResponse{
		Body:          "审核后文本",
		VoiceData:     "dm9pY2U=",
		VoiceFileName: "voice.wav",
		VoiceMimeType: "audio/wav",
	})
	if len(plugin.sentVoices) != 1 {
		t.Fatalf("sentVoices = %d, want 1", len(plugin.sentVoices))
	}
	if len(plugin.sentTexts) != 0 {
		t.Fatalf("sentTexts = %d, want 0", len(plugin.sentTexts))
	}
}

func TestDeliverSingleResponseKeepsVoiceAfterTextForOtherIM(t *testing.T) {
	plugin := &mockVoicePlugin{mockPlugin: mockPlugin{
		name: "telegram",
		caps: CapabilityDeclaration{SupportsVoice: true, SupportsFile: true, SupportsMarkdown: false},
	}}
	adapter := &Adapter{}
	adapter.deliverSingleResponse(context.Background(), plugin, UserTarget{PlatformUID: "user-1"}, &GenericResponse{
		Body:          "审核后文本",
		VoiceData:     "dm9pY2U=",
		VoiceFileName: "voice.ogg",
		VoiceMimeType: "audio/ogg",
	})
	if len(plugin.sentTexts) != 1 {
		t.Fatalf("sentTexts = %d, want 1", len(plugin.sentTexts))
	}
	if len(plugin.sentVoices) != 1 {
		t.Fatalf("sentVoices = %d, want 1", len(plugin.sentVoices))
	}
}

func TestIsIncomingVoiceMessageUsesStructuralModality(t *testing.T) {
	if !isIncomingVoiceMessage(IncomingMessage{MessageType: "voice"}) {
		t.Fatal("voice message type was not recognized")
	}
	if !isIncomingVoiceMessage(IncomingMessage{Attachments: []MessageAttachment{{Type: "audio"}}}) {
		t.Fatal("audio attachment was not recognized")
	}
	if isIncomingVoiceMessage(IncomingMessage{MessageType: "text", Text: "发我一段语音"}) {
		t.Fatal("text content must not be treated as voice modality")
	}
}

func TestAdapterTenantPluginOverridesSharedPluginForDelivery(t *testing.T) {
	adapter := NewAdapter(nil, nil)
	shared := &mockPlugin{name: "qqbot"}
	tenant := &mockPlugin{name: "qqbot"}
	if err := adapter.RegisterPlugin(shared); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if err := adapter.RegisterTenantPlugin("tenant_a", tenant); err != nil {
		t.Fatalf("RegisterTenantPlugin: %v", err)
	}

	adapter.DeliverResponse(WithTenant(context.Background(), "tenant_a"), "qqbot", "user-1", "platform-1", &GenericResponse{Body: "tenant reply"})
	if tenant.lastText() != "tenant reply" {
		t.Fatalf("tenant plugin text = %q", tenant.lastText())
	}
	if shared.textCount() != 0 {
		t.Fatalf("shared plugin received tenant delivery")
	}

	adapter.DeliverResponse(context.Background(), "qqbot", "user-1", "platform-1", &GenericResponse{Body: "global reply"})
	if shared.lastText() != "global reply" {
		t.Fatalf("shared plugin text = %q", shared.lastText())
	}
}

func TestAdapterUnregisterTenantPluginFallsBackToSharedPlugin(t *testing.T) {
	adapter := NewAdapter(nil, nil)
	shared := &mockPlugin{name: "qqbot"}
	tenant := &mockPlugin{name: "qqbot"}
	if err := adapter.RegisterPlugin(shared); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if err := adapter.RegisterTenantPlugin("tenant_a", tenant); err != nil {
		t.Fatalf("RegisterTenantPlugin: %v", err)
	}
	removed := adapter.UnregisterTenantPlugin("tenant_a", "qqbot")
	if removed != tenant {
		t.Fatalf("removed plugin = %#v, want tenant plugin", removed)
	}

	adapter.DeliverResponse(WithTenant(context.Background(), "tenant_a"), "qqbot", "user-1", "platform-1", &GenericResponse{Body: "fallback reply"})
	if tenant.textCount() != 0 {
		t.Fatalf("tenant plugin received delivery after unregister")
	}
	if shared.lastText() != "fallback reply" {
		t.Fatalf("shared plugin text = %q", shared.lastText())
	}
}

func TestRegisterTenantPluginInjectsTenantHintForInboundMessages(t *testing.T) {
	identity := &mockTenantIdentity{tenantID: "tenant_a", userID: "user-1"}
	adapter := NewAdapter(NewMessageRouter(&mockDeviceFinder{machineID: "machine-1", found: true}), identity)
	tenant := &mockPlugin{name: "qqbot"}
	if err := adapter.RegisterTenantPlugin("tenant_a", tenant); err != nil {
		t.Fatalf("RegisterTenantPlugin: %v", err)
	}

	tenant.mu.Lock()
	handler := tenant.handler
	tenant.mu.Unlock()
	if handler == nil {
		t.Fatal("tenant plugin handler was not registered")
	}
	handler(IncomingMessage{PlatformName: "qqbot", PlatformUID: "platform-1", MessageType: "text", Text: "hello"})
	if identity.seen != "tenant_a" {
		t.Fatalf("identity saw tenant %q, want tenant_a", identity.seen)
	}
}
