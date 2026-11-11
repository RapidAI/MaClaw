package agentservice

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestInstanceOwnerFindsTheOwningPrincipal(t *testing.T) {
	var none *Service
	if _, ok := none.InstanceOwner("x"); ok {
		t.Fatal("nil service found an owner")
	}
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, nil, EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tenant, err := svc.CreateTenant(ctx, CreateTenantInput{Name: "tenant"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(ctx, CreateUserInput{TenantID: tenant.ID, Name: "user"})
	if err != nil {
		t.Fatal(err)
	}
	p := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(ctx, p, corelib.AppConfig{MaclawLLMUrl: "https://llm.example/v1", MaclawLLMKey: "key", MaclawLLMModel: "model"}); err != nil {
		t.Fatal(err)
	}
	inst, err := svc.CreateInstance(ctx, p, CreateInstanceInput{Name: "i"})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := svc.InstanceOwner(inst.ID)
	if !ok || got.TenantID != p.TenantID || got.UserID != p.UserID {
		t.Fatalf("owner = %+v %v", got, ok)
	}
	if _, ok := svc.InstanceOwner("inst_missing"); ok {
		t.Fatal("missing instance found")
	}
}
