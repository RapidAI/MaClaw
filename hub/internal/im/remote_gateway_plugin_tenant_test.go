package im

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type remoteGatewayTestSender struct {
	messages []map[string]any
}

func (s *remoteGatewayTestSender) SendToMachine(_ string, msg any) error {
	if m, ok := msg.(map[string]any); ok {
		s.messages = append(s.messages, m)
	}
	return nil
}

func TestRemoteGatewayEmailBindingResolvesUniqueTenant(t *testing.T) {
	sender := &remoteGatewayTestSender{}
	plugin := NewRemoteGatewayPlugin("telegram", sender, tenantEmailTestUsers{users: []*store.User{
		{ID: "u1", TenantID: "tenant_a", Email: "same@example.com"},
	}}, nil)
	plugin.owner = &gatewayOwner{MachineID: "machine-a"}

	plugin.handleEmailSubmit("platform-a", "same@example.com")

	plugin.pendingMu.Lock()
	pending := plugin.pending["platform-a"]
	plugin.pendingMu.Unlock()
	if pending == nil || pending.TenantID != "tenant_a" || pending.Email != "same@example.com" {
		t.Fatalf("pending binding = %#v", pending)
	}
}

func TestRemoteGatewayEmailBindingRejectsAmbiguousEmail(t *testing.T) {
	sender := &remoteGatewayTestSender{}
	plugin := NewRemoteGatewayPlugin("telegram", sender, tenantEmailTestUsers{users: []*store.User{
		{ID: "u1", TenantID: "tenant_a", Email: "same@example.com"},
		{ID: "u2", TenantID: "tenant_b", Email: "same@example.com"},
	}}, nil)
	plugin.owner = &gatewayOwner{MachineID: "machine-a"}

	plugin.handleEmailSubmit("platform-a", "same@example.com")

	plugin.pendingMu.Lock()
	_, hasPending := plugin.pending["platform-a"]
	plugin.pendingMu.Unlock()
	if hasPending {
		t.Fatal("ambiguous email should not create a pending binding")
	}
	if !remoteGatewaySentTextContains(sender.messages, "multiple tenants") {
		t.Fatalf("expected ambiguity reply, messages=%#v", sender.messages)
	}
}

func remoteGatewaySentTextContains(messages []map[string]any, needle string) bool {
	for _, msg := range messages {
		payload, _ := msg["payload"].(map[string]any)
		inner, _ := payload["payload"].(map[string]any)
		text, _ := inner["text"].(string)
		if strings.Contains(text, needle) {
			return true
		}

		data, _ := json.Marshal(msg)
		if strings.Contains(string(data), needle) {
			return true
		}
	}
	return false
}
