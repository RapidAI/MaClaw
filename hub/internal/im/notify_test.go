package im

import (
	"context"
	"errors"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

type notifyTestPlugin struct {
	name     string
	bindings map[string]map[string]string
	sentUIDs []string
	fileUIDs []string
	fileErr  error
}

func (p *notifyTestPlugin) Name() string                         { return p.name }
func (p *notifyTestPlugin) ReceiveMessage(func(IncomingMessage)) {}
func (p *notifyTestPlugin) SendText(_ context.Context, target UserTarget, _ string) error {
	p.sentUIDs = append(p.sentUIDs, target.PlatformUID)
	return nil
}
func (p *notifyTestPlugin) SendCard(context.Context, UserTarget, OutgoingMessage) error { return nil }
func (p *notifyTestPlugin) SendImage(context.Context, UserTarget, string, string) error { return nil }

func (p *notifyTestPlugin) SendFile(_ context.Context, target UserTarget, _, _, _ string) error {
	p.fileUIDs = append(p.fileUIDs, target.PlatformUID)
	return p.fileErr
}

func TestTargetedFileFailureDoesNotSendMisleadingCaption(t *testing.T) {
	plugin := &notifyTestPlugin{name: "feishu", fileErr: errors.New("upload failed")}
	adapter := NewAdapter(NewMessageRouter(nil), nil)
	if err := adapter.RegisterPlugin(plugin); err != nil {
		t.Fatal(err)
	}
	sender := NewProactiveSender(NewNotifyBroadcaster(adapter, nil), nil)
	err := sender.SendProactiveFileToTarget(context.Background(), "tenant_default", "u", agent.IMFileDeliveryTarget{
		Channel: "feishu", GroupID: "chat-9",
	}, "ZGF0YQ==", "report.pdf", "application/pdf", "报告已发送")
	if err == nil {
		t.Fatal("expected file upload error")
	}
	if len(plugin.sentUIDs) != 0 {
		t.Fatalf("caption must not be sent before failed media: %v", plugin.sentUIDs)
	}
}
func (p *notifyTestPlugin) ResolveUser(context.Context, string) (string, error) { return "", nil }
func (p *notifyTestPlugin) Capabilities() CapabilityDeclaration {
	return CapabilityDeclaration{SupportsFile: true}
}
func (p *notifyTestPlugin) Start(context.Context) error { return nil }
func (p *notifyTestPlugin) Stop(context.Context) error  { return nil }

func (p *notifyTestPlugin) LookupByEmail(email string) string {
	return p.LookupByTenantEmail("tenant_default", email)
}

func (p *notifyTestPlugin) LookupByTenantEmail(tenantID, email string) string {
	if p.bindings == nil || p.bindings[tenantID] == nil {
		return ""
	}
	return p.bindings[tenantID][email]
}

type notifyTestUsers struct {
	tenantID string
	email    string
}

func (u notifyTestUsers) GetEmail(_ context.Context, tenantID, _ string) (string, error) {
	if tenantID != u.tenantID {
		return "", context.Canceled
	}
	return u.email, nil
}

func TestNotifyBroadcasterUsesTenantScopedBinding(t *testing.T) {
	plugin := &notifyTestPlugin{name: "feishu", bindings: map[string]map[string]string{
		"tenant_a": {"same@example.com": "uid-a"},
		"tenant_b": {"same@example.com": "uid-b"},
	}}
	adapter := NewAdapter(NewMessageRouter(nil), nil)
	if err := adapter.RegisterPlugin(plugin); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}

	b := NewNotifyBroadcaster(adapter, nil)
	b.BroadcastTextForTenant(context.Background(), "tenant_b", "same@example.com", "subject", "body")

	if len(plugin.sentUIDs) != 1 || plugin.sentUIDs[0] != "uid-b" {
		t.Fatalf("expected tenant_b uid only, got %#v", plugin.sentUIDs)
	}
}

func TestProactiveSenderUsesTenantScopedBinding(t *testing.T) {
	plugin := &notifyTestPlugin{name: "feishu", bindings: map[string]map[string]string{
		"tenant_a": {"same@example.com": "uid-a"},
		"tenant_b": {"same@example.com": "uid-b"},
	}}
	adapter := NewAdapter(NewMessageRouter(nil), nil)
	if err := adapter.RegisterPlugin(plugin); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	sender := NewProactiveSender(NewNotifyBroadcaster(adapter, nil), notifyTestUsers{tenantID: "tenant_a", email: "same@example.com"})

	if err := sender.SendProactiveMessage(context.Background(), "tenant_a", "user-1", "hello"); err != nil {
		t.Fatalf("SendProactiveMessage: %v", err)
	}
	if len(plugin.sentUIDs) != 1 || plugin.sentUIDs[0] != "uid-a" {
		t.Fatalf("expected tenant_a uid only, got %#v", plugin.sentUIDs)
	}
}

func TestProactiveSenderTargetsOneFileConversation(t *testing.T) {
	feishu := &notifyTestPlugin{name: "feishu"}
	qq := &notifyTestPlugin{name: "qqbot"}
	adapter := NewAdapter(NewMessageRouter(nil), nil)
	if err := adapter.RegisterPlugin(feishu); err != nil {
		t.Fatal(err)
	}
	if err := adapter.RegisterPlugin(qq); err != nil {
		t.Fatal(err)
	}
	sender := NewProactiveSender(NewNotifyBroadcaster(adapter, nil), nil)
	err := sender.SendProactiveFileToTarget(context.Background(), "tenant_default", "u", agent.IMFileDeliveryTarget{
		Channel: "feishu", GroupID: "chat-9",
	}, "ZGF0YQ==", "report.pdf", "application/pdf", "")
	if err != nil {
		t.Fatalf("SendProactiveFileToTarget: %v", err)
	}
	if len(feishu.fileUIDs) != 1 || feishu.fileUIDs[0] != "chat-9" {
		t.Fatalf("feishu targets=%v", feishu.fileUIDs)
	}
	if len(qq.fileUIDs) != 0 {
		t.Fatalf("qq must not receive targeted file: %v", qq.fileUIDs)
	}
}
