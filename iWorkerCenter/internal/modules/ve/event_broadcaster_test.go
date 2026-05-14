package ve

import (
	"sync"
	"testing"
	"time"
)

// mockClientSender records all messages sent to machines.
type mockClientSender struct {
	mu       sync.Mutex
	messages map[string][]any // machineID → messages
}

func newMockClientSender() *mockClientSender {
	return &mockClientSender{messages: make(map[string][]any)}
}

func (m *mockClientSender) SendToMachine(machineID string, msg any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages[machineID] = append(m.messages[machineID], msg)
	return nil
}

func (m *mockClientSender) getMessages(machineID string) []any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages[machineID]
}

func (m *mockClientSender) allMessages() map[string][]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string][]any)
	for k, v := range m.messages {
		result[k] = v
	}
	return result
}

// mockClientsProvider returns a fixed list of online machine IDs.
type mockClientsProvider struct {
	machineIDs []string
}

func (m *mockClientsProvider) ListOnlineMachineIDs() []string {
	return m.machineIDs
}

// --- Tests ---

func TestEventBroadcaster_BroadcastListUpdate(t *testing.T) {
	sender := newMockClientSender()
	clients := &mockClientsProvider{machineIDs: []string{"machine-1", "machine-2", "machine-3"}}
	eb := NewEventBroadcaster(sender, clients, nil)
	eb.SetThrottleMs(0) // disable throttle for testing

	eb.BroadcastListUpdate()

	// All 3 clients should receive the event
	for _, mid := range clients.machineIDs {
		msgs := sender.getMessages(mid)
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message for %s, got %d", mid, len(msgs))
		}
		msg := msgs[0].(map[string]any)
		if msg["type"] != VEEventListUpdate {
			t.Fatalf("expected type %q, got %q", VEEventListUpdate, msg["type"])
		}
		payload := msg["payload"].(map[string]any)
		if payload["reason"] != "list_changed" {
			t.Fatalf("expected reason 'list_changed', got %v", payload["reason"])
		}
	}
}

func TestEventBroadcaster_BroadcastStatusChange(t *testing.T) {
	sender := newMockClientSender()
	clients := &mockClientsProvider{machineIDs: []string{"machine-1", "machine-2"}}
	eb := NewEventBroadcaster(sender, clients, nil)
	eb.SetThrottleMs(0)

	eb.BroadcastStatusChange("ve-123", "online")

	for _, mid := range clients.machineIDs {
		msgs := sender.getMessages(mid)
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message for %s, got %d", mid, len(msgs))
		}
		msg := msgs[0].(map[string]any)
		if msg["type"] != VEEventStatusChange {
			t.Fatalf("expected type %q, got %q", VEEventStatusChange, msg["type"])
		}
		payload := msg["payload"].(map[string]any)
		if payload["ve_id"] != "ve-123" {
			t.Fatalf("expected ve_id 've-123', got %v", payload["ve_id"])
		}
		if payload["online_status"] != "online" {
			t.Fatalf("expected online_status 'online', got %v", payload["online_status"])
		}
	}
}

func TestEventBroadcaster_PushAuthRequest_OnlyToOwner(t *testing.T) {
	sender := newMockClientSender()
	clients := &mockClientsProvider{machineIDs: []string{"machine-1", "machine-2", "machine-3"}}
	eb := NewEventBroadcaster(sender, clients, nil)
	eb.SetThrottleMs(0)

	req := AuthorizationRequest{
		ID:                 "auth-1",
		RequesterName:      "Alice",
		RequesterMachineID: "machine-1",
		TargetVEID:         "ve-123",
		TargetVEName:       "Bob's VE",
		CreatedAt:          time.Now(),
		ExpiresAt:          time.Now().Add(60 * time.Second),
	}

	// Push to machine-2 (the VE owner)
	eb.PushAuthRequest("machine-2", req)

	// Only machine-2 should receive the auth request
	msgs := sender.getMessages("machine-2")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for machine-2, got %d", len(msgs))
	}
	msg := msgs[0].(map[string]any)
	if msg["type"] != VEEventAuthRequest {
		t.Fatalf("expected type %q, got %q", VEEventAuthRequest, msg["type"])
	}

	// machine-1 and machine-3 should NOT receive anything
	if len(sender.getMessages("machine-1")) != 0 {
		t.Fatal("machine-1 should not receive auth request")
	}
	if len(sender.getMessages("machine-3")) != 0 {
		t.Fatal("machine-3 should not receive auth request")
	}
}

func TestEventBroadcaster_BroadcastApproved_OnlyToOwner(t *testing.T) {
	sender := newMockClientSender()
	clients := &mockClientsProvider{machineIDs: []string{"machine-1", "machine-2"}}
	eb := NewEventBroadcaster(sender, clients, nil)
	eb.SetThrottleMs(0)

	eb.BroadcastApproved("machine-1", "ve-456")

	msgs := sender.getMessages("machine-1")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for machine-1, got %d", len(msgs))
	}
	msg := msgs[0].(map[string]any)
	if msg["type"] != VEEventApproved {
		t.Fatalf("expected type %q, got %q", VEEventApproved, msg["type"])
	}

	// machine-2 should NOT receive
	if len(sender.getMessages("machine-2")) != 0 {
		t.Fatal("machine-2 should not receive approved event")
	}
}

func TestEventBroadcaster_BroadcastGroupConfig(t *testing.T) {
	sender := newMockClientSender()
	clients := &mockClientsProvider{machineIDs: []string{"machine-1", "machine-2"}}
	eb := NewEventBroadcaster(sender, clients, nil)
	eb.SetThrottleMs(0)

	eb.BroadcastGroupConfig(8)

	for _, mid := range clients.machineIDs {
		msgs := sender.getMessages(mid)
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message for %s, got %d", mid, len(msgs))
		}
		msg := msgs[0].(map[string]any)
		if msg["type"] != VEEventGroupConfig {
			t.Fatalf("expected type %q, got %q", VEEventGroupConfig, msg["type"])
		}
		payload := msg["payload"].(map[string]any)
		if payload["max_group_participants"] != 8 {
			t.Fatalf("expected max_group_participants=8, got %v", payload["max_group_participants"])
		}
	}
}

func TestEventBroadcaster_Throttle(t *testing.T) {
	sender := newMockClientSender()
	clients := &mockClientsProvider{machineIDs: []string{"machine-1"}}
	eb := NewEventBroadcaster(sender, clients, nil)
	eb.SetThrottleMs(100) // 100ms throttle

	// First broadcast should go through
	eb.BroadcastListUpdate()
	if len(sender.getMessages("machine-1")) != 1 {
		t.Fatal("first broadcast should go through")
	}

	// Second broadcast within 100ms should be throttled
	eb.BroadcastListUpdate()
	if len(sender.getMessages("machine-1")) != 1 {
		t.Fatal("second broadcast within throttle window should be suppressed")
	}

	// Wait for throttle to expire
	time.Sleep(110 * time.Millisecond)

	// Third broadcast should go through
	eb.BroadcastListUpdate()
	if len(sender.getMessages("machine-1")) != 2 {
		t.Fatalf("expected 2 messages after throttle expired, got %d", len(sender.getMessages("machine-1")))
	}
}

func TestEventBroadcaster_ThrottlePerEventType(t *testing.T) {
	sender := newMockClientSender()
	clients := &mockClientsProvider{machineIDs: []string{"machine-1"}}
	eb := NewEventBroadcaster(sender, clients, nil)
	eb.SetThrottleMs(100)

	// list_update should go through
	eb.BroadcastListUpdate()
	// status_change is a different event type, should also go through
	eb.BroadcastStatusChange("ve-1", "online")

	msgs := sender.getMessages("machine-1")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (different event types), got %d", len(msgs))
	}
}

func TestEventBroadcaster_Wire_RegistryOnChange(t *testing.T) {
	sender := newMockClientSender()
	clients := &mockClientsProvider{machineIDs: []string{"machine-1"}}
	tmpFile := t.TempDir() + "/quota.enc"
	qs := NewQuotaStore([]byte("test-key-32-bytes-long-enough!!"), "hub-1", tmpFile)
	_ = qs.SaveQuota(10) // set quota so registration succeeds
	registry := NewRegistry(qs, "")
	presence := NewPresenceManager()
	authHandler := NewAuthHandler()

	eb := NewEventBroadcaster(sender, clients, registry)
	eb.SetThrottleMs(0)
	eb.Wire(registry, presence, authHandler)

	// Register a VE — should trigger onChange → BroadcastListUpdate
	_, err := registry.Register(VERegistrationRequest{
		OwnerMachineID: "machine-1",
		Name:           "Test VE",
		SkillDesc:      "Test skill",
		AccessPolicy:   PolicyPublic,
	})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	// Give the goroutine time to execute
	time.Sleep(50 * time.Millisecond)

	msgs := sender.getMessages("machine-1")
	if len(msgs) == 0 {
		t.Fatal("expected at least 1 broadcast after registry change")
	}
	msg := msgs[0].(map[string]any)
	if msg["type"] != VEEventListUpdate {
		t.Fatalf("expected type %q, got %q", VEEventListUpdate, msg["type"])
	}
}

func TestEventBroadcaster_Wire_PresenceOnStatusChange(t *testing.T) {
	sender := newMockClientSender()
	clients := &mockClientsProvider{machineIDs: []string{"machine-1"}}
	presence := NewPresenceManager()
	qs := NewQuotaStore([]byte("test-key-32-bytes-long-enough!!"), "hub-1", "")
	registry := NewRegistry(qs, "")
	authHandler := NewAuthHandler()

	eb := NewEventBroadcaster(sender, clients, registry)
	eb.SetThrottleMs(0)
	eb.Wire(registry, presence, authHandler)

	// Connect a VE instance — should trigger status change to "online"
	presence.OnWebSocketConnect("ve-1", "machine-2")

	// Give the goroutine time to execute
	time.Sleep(50 * time.Millisecond)

	msgs := sender.getMessages("machine-1")
	if len(msgs) == 0 {
		t.Fatal("expected at least 1 broadcast after presence change")
	}
	msg := msgs[0].(map[string]any)
	if msg["type"] != VEEventStatusChange {
		t.Fatalf("expected type %q, got %q", VEEventStatusChange, msg["type"])
	}
	payload := msg["payload"].(map[string]any)
	if payload["ve_id"] != "ve-1" {
		t.Fatalf("expected ve_id 've-1', got %v", payload["ve_id"])
	}
	if payload["online_status"] != "online" {
		t.Fatalf("expected online_status 'online', got %v", payload["online_status"])
	}
}

func TestEventBroadcaster_Wire_PresenceDisconnect(t *testing.T) {
	sender := newMockClientSender()
	clients := &mockClientsProvider{machineIDs: []string{"machine-1"}}
	presence := NewPresenceManager()
	qs := NewQuotaStore([]byte("test-key-32-bytes-long-enough!!"), "hub-1", "")
	registry := NewRegistry(qs, "")
	authHandler := NewAuthHandler()

	eb := NewEventBroadcaster(sender, clients, registry)
	eb.SetThrottleMs(0)
	eb.Wire(registry, presence, authHandler)

	// Connect then disconnect
	presence.OnWebSocketConnect("ve-1", "machine-2")
	time.Sleep(50 * time.Millisecond)

	// Clear messages from connect
	sender.mu.Lock()
	sender.messages = make(map[string][]any)
	sender.mu.Unlock()

	presence.OnWebSocketDisconnect("ve-1", "machine-2")
	time.Sleep(50 * time.Millisecond)

	msgs := sender.getMessages("machine-1")
	if len(msgs) == 0 {
		t.Fatal("expected broadcast after disconnect")
	}
	msg := msgs[0].(map[string]any)
	if msg["type"] != VEEventStatusChange {
		t.Fatalf("expected type %q, got %q", VEEventStatusChange, msg["type"])
	}
	payload := msg["payload"].(map[string]any)
	if payload["online_status"] != "offline" {
		t.Fatalf("expected online_status 'offline', got %v", payload["online_status"])
	}
}

func TestEventBroadcaster_NoClientsNoPanic(t *testing.T) {
	sender := newMockClientSender()
	clients := &mockClientsProvider{machineIDs: []string{}} // no clients
	eb := NewEventBroadcaster(sender, clients, nil)
	eb.SetThrottleMs(0)

	// Should not panic with empty client list
	eb.BroadcastListUpdate()
	eb.BroadcastStatusChange("ve-1", "online")
	eb.BroadcastGroupConfig(5)
}

func TestEventBroadcaster_NilSenderNoPanic(t *testing.T) {
	clients := &mockClientsProvider{machineIDs: []string{"machine-1"}}
	eb := NewEventBroadcaster(nil, clients, nil)
	eb.SetThrottleMs(0)

	// Should not panic with nil sender
	eb.BroadcastListUpdate()
	eb.PushAuthRequest("machine-1", AuthorizationRequest{})
}
