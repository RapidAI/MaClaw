package im

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type remoteGatewayTestSender struct {
	machineIDs []string
	messages   []map[string]any
}

func (s *remoteGatewayTestSender) SendToMachine(machineID string, msg any) error {
	s.machineIDs = append(s.machineIDs, machineID)
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
	plugin.owners["tenant_a"] = &gatewayOwner{TenantID: "tenant_a", MachineID: "machine-a"}

	plugin.handleEmailSubmit("tenant_a", "platform-a", "same@example.com")

	plugin.pendingMu.Lock()
	pending := plugin.pending[remoteTenantPlatformKey("tenant_a", "platform-a")]
	plugin.pendingMu.Unlock()
	if pending == nil || pending.TenantID != "tenant_a" || pending.Email != "same@example.com" {
		t.Fatalf("pending binding = %#v", pending)
	}
}

func TestRemoteGatewayEmailBindingUsesMessageTenant(t *testing.T) {
	sender := &remoteGatewayTestSender{}
	plugin := NewRemoteGatewayPlugin("telegram", sender, tenantEmailTestUsers{users: []*store.User{
		{ID: "u1", TenantID: "tenant_a", Email: "same@example.com"},
		{ID: "u2", TenantID: "tenant_b", Email: "same@example.com"},
	}}, nil)
	plugin.owners["tenant_b"] = &gatewayOwner{TenantID: "tenant_b", MachineID: "machine-b"}

	plugin.handleEmailSubmit("tenant_b", "platform-a", "same@example.com")

	plugin.pendingMu.Lock()
	pending := plugin.pending[remoteTenantPlatformKey("tenant_b", "platform-a")]
	plugin.pendingMu.Unlock()
	if pending == nil || pending.TenantID != "tenant_b" || pending.Email != "same@example.com" {
		t.Fatalf("pending binding = %#v", pending)
	}
}

func TestRemoteGatewaySamePlatformUIDCanBindPerTenant(t *testing.T) {
	sender := &remoteGatewayTestSender{}
	plugin := NewRemoteGatewayPlugin("telegram", sender, tenantEmailTestUsers{users: []*store.User{
		{ID: "u1", TenantID: "tenant_a", Email: "same@example.com"},
		{ID: "u2", TenantID: "tenant_b", Email: "same@example.com"},
	}}, nil)

	plugin.BindTenantEmail("platform-a", "tenant_a", "same@example.com")
	plugin.BindTenantEmail("platform-a", "tenant_b", "same@example.com")

	tenantID, userID, err := plugin.ResolveUserWithTenant(WithTenant(context.Background(), "tenant_b"), "platform-a")
	if err != nil {
		t.Fatalf("resolve tenant B: %v", err)
	}
	if tenantID != "tenant_b" || userID != "u2" {
		t.Fatalf("resolved tenant/user = %q/%q", tenantID, userID)
	}
	if got := plugin.LookupByTenantEmail("tenant_a", "same@example.com"); got != "platform-a" {
		t.Fatalf("tenant A lookup = %q", got)
	}
	if got := plugin.LookupByTenantEmail("tenant_b", "same@example.com"); got != "platform-a" {
		t.Fatalf("tenant B lookup = %q", got)
	}
}

func TestRemoteGatewayLegacyPlainKeyValueTenantIsHonored(t *testing.T) {
	plugin := NewRemoteGatewayPlugin("telegram", &remoteGatewayTestSender{}, tenantEmailTestUsers{users: []*store.User{
		{ID: "u1", TenantID: "tenant_a", Email: "same@example.com"},
		{ID: "u2", TenantID: "tenant_b", Email: "same@example.com"},
	}}, nil)
	plugin.bindings["platform-a"] = `{"email":"same@example.com","tenant_id":"tenant_b"}`

	tenantID, userID, err := plugin.ResolveUserWithTenant(WithTenant(context.Background(), "tenant_b"), "platform-a")
	if err != nil {
		t.Fatalf("resolve tenant B: %v", err)
	}
	if tenantID != "tenant_b" || userID != "u2" {
		t.Fatalf("resolved tenant/user = %q/%q", tenantID, userID)
	}
	if got := plugin.LookupByTenantEmail("tenant_b", "same@example.com"); got != "platform-a" {
		t.Fatalf("tenant B lookup = %q", got)
	}
}

func TestRemoteGatewayLegacyPlainKeyValueTenantDoesNotLeakToDefaultTenant(t *testing.T) {
	plugin := NewRemoteGatewayPlugin("telegram", &remoteGatewayTestSender{}, tenantEmailTestUsers{users: []*store.User{
		{ID: "u1", TenantID: store.DefaultTenantID, Email: "same@example.com"},
		{ID: "u2", TenantID: "tenant_b", Email: "same@example.com"},
	}}, nil)
	plugin.bindings["platform-a"] = `{"email":"same@example.com","tenant_id":"tenant_b"}`

	tenantID, userID, err := plugin.ResolveUserWithTenant(WithTenant(context.Background(), store.DefaultTenantID), "platform-a")
	if err == nil {
		t.Fatalf("default tenant resolved legacy tenant binding as %q/%q", tenantID, userID)
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

func TestRemoteGatewayClaimsAndRepliesAreTenantScoped(t *testing.T) {
	sender := &remoteGatewayTestSender{}
	plugin := NewRemoteGatewayPlugin("telegram", sender, tenantEmailTestUsers{}, nil)

	ok, reason, _ := plugin.ClaimGatewayForTenant("tenant_a", "machine-a", "user-a")
	if !ok {
		t.Fatalf("tenant A claim failed: %s", reason)
	}
	ok, reason, _ = plugin.ClaimGatewayForTenant("tenant_b", "machine-b", "user-b")
	if !ok {
		t.Fatalf("tenant B claim failed: %s", reason)
	}
	if plugin.GatewayOwnerForTenant("tenant_a") != "machine-a" || plugin.GatewayOwnerForTenant("tenant_b") != "machine-b" {
		t.Fatalf("gateway owners not tenant scoped: a=%q b=%q", plugin.GatewayOwnerForTenant("tenant_a"), plugin.GatewayOwnerForTenant("tenant_b"))
	}

	if err := plugin.SendText(WithTenant(context.Background(), "tenant_b"), UserTarget{PlatformUID: "platform-a"}, "hello"); err != nil {
		t.Fatalf("send tenant B reply: %v", err)
	}
	if len(sender.machineIDs) != 1 || sender.machineIDs[0] != "machine-b" {
		t.Fatalf("sent machines = %#v, want machine-b", sender.machineIDs)
	}
	payload, _ := sender.messages[0]["payload"].(map[string]any)
	inner, _ := payload["payload"].(map[string]any)
	if payload["tenant_id"] != "tenant_b" || inner["tenant_id"] != "tenant_b" {
		t.Fatalf("reply tenant payload = outer:%#v inner:%#v", payload["tenant_id"], inner["tenant_id"])
	}
}

func TestRemoteGatewayReplyCarriesCachedChatType(t *testing.T) {
	sender := &remoteGatewayTestSender{}
	plugin := NewRemoteGatewayPlugin("lansenger", sender, tenantEmailTestUsers{}, nil)
	if ok, reason, _ := plugin.ClaimGatewayForTenant("tenant_a", "machine-a", "user-a"); !ok {
		t.Fatalf("claim failed: %s", reason)
	}
	plugin.rememberReplyRoute("tenant_a", "group-1", "group-1", "group")

	if err := plugin.SendText(WithTenant(context.Background(), "tenant_a"), UserTarget{PlatformUID: "group-1"}, "hello group"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("reply count = %d, want 1", len(sender.messages))
	}
	payload, _ := sender.messages[0]["payload"].(map[string]any)
	inner, _ := payload["payload"].(map[string]any)
	if got, _ := inner["chat_type"].(string); got != "group" {
		t.Fatalf("reply chat_type = %q, want group", got)
	}
}

func TestRemoteGatewayGroupReplyUsesConversationTargetNotSenderIdentity(t *testing.T) {
	sender := &remoteGatewayTestSender{}
	plugin := NewRemoteGatewayPlugin("lansenger", sender, tenantEmailTestUsers{}, nil)
	if ok, reason, _ := plugin.ClaimGatewayForTenant("tenant_a", "machine-a", "user-a"); !ok {
		t.Fatalf("claim failed: %s", reason)
	}
	plugin.rememberReplyRoute("tenant_a", "user-1", "group-1", "group")

	if err := plugin.SendText(WithTenant(context.Background(), "tenant_a"), UserTarget{PlatformUID: "user-1"}, "hello group"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	payload, _ := sender.messages[0]["payload"].(map[string]any)
	inner, _ := payload["payload"].(map[string]any)
	if got, _ := inner["platform_uid"].(string); got != "group-1" {
		t.Fatalf("reply target = %q, want group-1", got)
	}
	if got, _ := inner["chat_type"].(string); got != "group" {
		t.Fatalf("reply chat_type = %q, want group", got)
	}
}

func TestRemoteGatewayReplyCarriesSourceMessageMeta(t *testing.T) {
	sender := &remoteGatewayTestSender{}
	plugin := NewRemoteGatewayPlugin("lansenger", sender, tenantEmailTestUsers{}, nil)
	if ok, reason, _ := plugin.ClaimGatewayForTenant("tenant_a", "machine-a", "user-a"); !ok {
		t.Fatalf("claim failed: %s", reason)
	}
	plugin.rememberReplyRoute("tenant_a", "staff-1", "group-1", "group")

	ctx := WithReplyMeta(WithTenant(context.Background(), "tenant_a"), "staff-1", "mid-42")
	if err := plugin.SendText(ctx, UserTarget{PlatformUID: "staff-1"}, "answer"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("reply count = %d", len(sender.messages))
	}
	payload, _ := sender.messages[0]["payload"].(map[string]any)
	inner, _ := payload["payload"].(map[string]any)
	if got, _ := inner["source_message_id"].(string); got != "mid-42" {
		t.Fatalf("source_message_id = %q", got)
	}
	if got, _ := inner["sender_id"].(string); got != "staff-1" {
		t.Fatalf("sender_id = %q", got)
	}
	if got, _ := inner["platform_uid"].(string); got != "group-1" {
		t.Fatalf("platform_uid = %q, want group-1", got)
	}
}

func TestRemoteGatewayReplyTargetRejectsGroupFallbackToSender(t *testing.T) {
	if got := remoteGatewayReplyTarget("user-1", "", "group"); got != "" {
		t.Fatalf("group target = %q, want empty", got)
	}
	if got := remoteGatewayReplyTarget("user-1", "", "p2p"); got != "user-1" {
		t.Fatalf("private target = %q, want user-1", got)
	}
}

func TestRemoteGatewayGroupReplyRouteIsAvailableByConversationTarget(t *testing.T) {
	plugin := NewRemoteGatewayPlugin("lansenger", &remoteGatewayTestSender{}, tenantEmailTestUsers{}, nil)
	plugin.rememberReplyRoute("tenant_a", "user-1", "group-1", "group")
	if route := plugin.replyRoute("tenant_a", "group-1"); route.target != "group-1" || route.chatType != "group" {
		t.Fatalf("group conversation route = %#v", route)
	}
}

func TestRemoteGatewayLansengerGroupSuppressesIMDetail(t *testing.T) {
	plugin := NewRemoteGatewayPlugin("lansenger", &remoteGatewayTestSender{}, tenantEmailTestUsers{}, nil)
	plugin.rememberReplyRoute("tenant_a", "group-1", "group-1", "group")

	ctx := WithTenant(context.Background(), "tenant_a")
	if !plugin.SuppressGroupIMDetail(ctx, UserTarget{PlatformUID: "group-1"}) {
		t.Fatal("Lansenger group must suppress IM detail")
	}
	if plugin.SuppressGroupIMDetail(ctx, UserTarget{PlatformUID: "user-1"}) {
		t.Fatal("private target must not suppress IM detail")
	}
}

func TestRemoteGatewayLansengerGroupSuppressesIdentityPrompt(t *testing.T) {
	plugin := NewRemoteGatewayPlugin("lansenger", &remoteGatewayTestSender{}, tenantEmailTestUsers{}, nil)
	plugin.rememberReplyRoute("tenant_a", "user-1", "group-1", "group")
	ctx := WithTenant(context.Background(), "tenant_a")
	if !plugin.SuppressGroupIdentityPrompt(ctx, UserTarget{PlatformUID: "user-1"}) {
		t.Fatal("group identity prompt must be suppressed")
	}
	if plugin.SuppressGroupIdentityPrompt(ctx, UserTarget{PlatformUID: "user-2"}) {
		t.Fatal("private identity prompt must not be suppressed")
	}
}

func TestRemoteGatewayReleaseIsTenantScoped(t *testing.T) {
	plugin := NewRemoteGatewayPlugin("telegram", &remoteGatewayTestSender{}, tenantEmailTestUsers{}, nil)

	ok, reason, seqA := plugin.ClaimGatewayForTenant("tenant_a", "machine-shared", "user-a")
	if !ok {
		t.Fatalf("tenant A claim failed: %s", reason)
	}
	ok, reason, _ = plugin.ClaimGatewayForTenant("tenant_b", "machine-shared", "user-b")
	if !ok {
		t.Fatalf("tenant B claim failed: %s", reason)
	}

	plugin.ReleaseAllForTenantMachineBySeq("tenant_a", "machine-shared", map[string]uint64{"telegram": seqA})
	if got := plugin.GatewayOwnerForTenant("tenant_a"); got != "" {
		t.Fatalf("tenant A owner after release = %q, want empty", got)
	}
	if got := plugin.GatewayOwnerForTenant("tenant_b"); got != "machine-shared" {
		t.Fatalf("tenant B owner after tenant A release = %q, want machine-shared", got)
	}
}
