package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestDeviceAgentBindingStoreExpertInstanceAndVoice(t *testing.T) {
	ctx := context.Background()
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	p := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(ctx, p, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	store := newSrvDeviceAgentBindingStore(dataRoot)
	if _, err := store.ensure(p, "living-room"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if _, err := store.upsertExpert(p, srvHardwareExpert{ID: "storyteller", Name: "Storyteller", SystemPrompt: "Tell age-appropriate stories.", Tools: []string{"web_search"}}); err != nil {
		t.Fatalf("upsertExpert: %v", err)
	}
	binding, err := store.update(p, "living-room", srvHardwareBindingUpdate{AssistantMode: "expert", ExpertID: "storyteller", InitialPrompt: "Use a warm, short style.", TTSVoiceID: "zf_xiaoxiao", Version: 1})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if binding.Version != 2 || binding.ExpertID != "storyteller" || binding.TTSVoiceID != "zf_xiaoxiao" {
		t.Fatalf("binding = %#v", binding)
	}
	_, instance, err := store.ensureInstance(ctx, svc, p, "living-room")
	if err != nil {
		t.Fatalf("ensureInstance: %v", err)
	}
	if instance.Metadata["device_binding_id"] != "living-room" || instance.Metadata["hardware_expert_system_prompt"] != "Tell age-appropriate stories." || instance.Metadata["hardware_initial_prompt"] != "Use a warm, short style." {
		t.Fatalf("instance metadata = %#v", instance.Metadata)
	}
	view := store.view(p, binding, "zm_yunxi")
	if view.EffectiveTTSVoiceID != "zf_xiaoxiao" || view.Status != "ready" || view.ExpertName != "Storyteller" {
		t.Fatalf("view = %#v", view)
	}
}

func TestDeviceAgentBindingRejectsUnverifiedExpertAndUnsupportedVoice(t *testing.T) {
	store := newSrvDeviceAgentBindingStore(t.TempDir())
	p := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	if _, err := store.ensure(p, "device"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if _, err := store.update(p, "device", srvHardwareBindingUpdate{AssistantMode: "expert", ExpertID: "missing", Version: 1}); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("missing expert error = %v", err)
	}
	if _, err := store.update(p, "device", srvHardwareBindingUpdate{AssistantMode: "general", TTSVoiceID: "unknown", Version: 1}); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("unsupported voice error = %v", err)
	}
}

func TestDeviceAgentBindingIsOwnerScopedAndUnpairRequiresFreshPairing(t *testing.T) {
	store := newSrvDeviceAgentBindingStore(t.TempDir())
	ownerA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-a"}
	ownerB := agentservice.Principal{TenantID: "tenant-b", UserID: "user-b"}
	if _, err := store.ensure(ownerA, "shared-client"); err != nil {
		t.Fatalf("ensure owner A: %v", err)
	}
	if _, ok := store.get(ownerB, "shared-client"); ok {
		t.Fatal("another owner must not read this hardware binding")
	}
	if _, err := store.update(ownerA, "shared-client", srvHardwareBindingUpdate{AssistantMode: "general", InitialPrompt: "private policy", Version: 1}); err != nil {
		t.Fatalf("update: %v", err)
	}
	removed, found, err := store.delete(ownerA, "shared-client")
	if err != nil || !found {
		t.Fatalf("delete = (%#v, %v, %v)", removed, found, err)
	}
	if _, err := store.ensure(ownerA, "shared-client"); err == nil || !strings.Contains(err.Error(), "unpaired") {
		t.Fatalf("stale device traffic must fail closed, got %v", err)
	}
	fresh, err := store.activate(ownerA, "shared-client")
	if err != nil {
		t.Fatalf("activate after an explicit fresh pairing: %v", err)
	}
	if fresh.InitialPrompt != "" || fresh.InstanceID != "" || fresh.Version != 1 {
		t.Fatalf("fresh pairing inherited removed runtime state: %#v", fresh)
	}
}

func TestDeviceAgentBindingDeletedExpertBecomesDegraded(t *testing.T) {
	store := newSrvDeviceAgentBindingStore(t.TempDir())
	p := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	if _, err := store.ensure(p, "device"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if _, err := store.upsertExpert(p, srvHardwareExpert{ID: "support", Name: "Support", SystemPrompt: "Support only."}); err != nil {
		t.Fatalf("upsert expert: %v", err)
	}
	binding, err := store.update(p, "device", srvHardwareBindingUpdate{AssistantMode: "expert", ExpertID: "support", Version: 1})
	if err != nil {
		t.Fatalf("set expert binding: %v", err)
	}
	if deleted, err := store.deleteExpert(p, "support"); err != nil || !deleted {
		t.Fatalf("delete expert = (%v, %v)", deleted, err)
	}
	if view := store.view(p, binding, "zf_xiaoyi"); view.Status != "degraded" {
		t.Fatalf("deleted expert should degrade binding, view = %#v", view)
	}
	if _, err := store.instanceMetadata(p, binding); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("deleted expert must fail closed while composing runtime policy, got %v", err)
	}
}

func TestDeviceAgentBindingClearsOnlyMatchingStaleInstanceReference(t *testing.T) {
	store := newSrvDeviceAgentBindingStore(t.TempDir())
	p := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	if _, err := store.ensure(p, "device"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := store.setInstance(p, "device", "inst-live"); err != nil {
		t.Fatalf("set instance: %v", err)
	}
	if err := store.clearInstanceIf(p, "device", "inst-other"); err != nil {
		t.Fatalf("clear mismatched instance: %v", err)
	}
	if got, _ := store.get(p, "device"); got.InstanceID != "inst-live" {
		t.Fatalf("mismatched clear changed binding: %#v", got)
	}
	if err := store.clearInstanceIf(p, "device", "inst-live"); err != nil {
		t.Fatalf("clear stale instance: %v", err)
	}
	if got, _ := store.get(p, "device"); got.InstanceID != "" {
		t.Fatalf("stale instance reference was not cleared: %#v", got)
	}
}
