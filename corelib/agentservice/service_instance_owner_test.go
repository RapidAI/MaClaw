package agentservice

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestInstanceOwnerFindsTheOwningPrincipal(t *testing.T) {
	var none *Service
	if _, err := none.InstanceOwner("x"); !errors.Is(err, ErrInstanceNotFound) {
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
	got, err := svc.InstanceOwner(inst.ID)
	if err != nil || got.TenantID != p.TenantID || got.UserID != p.UserID {
		t.Fatalf("owner = %+v %v", got, err)
	}
	if _, err := svc.InstanceOwner("inst_missing"); !errors.Is(err, ErrInstanceNotFound) {
		t.Fatalf("missing instance: %v", err)
	}
}

type errListTenants struct{ *MemoryStore }

func (errListTenants) ListTenants() ([]Tenant, error) {
	return nil, errors.New("db down")
}

func TestInstanceOwnerStoreErrorIsNotMissing(t *testing.T) {
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, errListTenants{NewMemoryStore()}, EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.InstanceOwner("inst_any")
	if err == nil || errors.Is(err, ErrInstanceNotFound) || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("store error = %v", err)
	}
}
