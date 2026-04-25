package agentservice

import (
	"context"
	"errors"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func quotaReadyService(t *testing.T) (*Service, Principal) {
	t.Helper()
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test"}, NewMemoryStore(), EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{MaclawLLMUrl: "https://llm.example/v1", MaclawLLMKey: "k", MaclawLLMModel: "m"}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	return svc, principal
}

func TestInstanceQuotaEnforced(t *testing.T) {
	svc, principal := quotaReadyService(t)
	one := 1
	if _, err := svc.UpdateUser(context.Background(), principal.TenantID, principal.UserID, UpdateUserInput{MaxInstances: &one}); err != nil {
		t.Fatalf("UpdateUser quota: %v", err)
	}
	if _, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "one"}); err != nil {
		t.Fatalf("CreateInstance one: %v", err)
	}
	if _, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "two"}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("CreateInstance two error = %v, want ErrQuotaExceeded", err)
	}
}

func TestSessionQuotaEnforced(t *testing.T) {
	svc, principal := quotaReadyService(t)
	inst, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "one"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	one := 1
	if _, err := svc.UpdateUser(context.Background(), principal.TenantID, principal.UserID, UpdateUserInput{MaxSessions: &one}); err != nil {
		t.Fatalf("UpdateUser quota: %v", err)
	}
	if _, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "s1"}); err != nil {
		t.Fatalf("CreateSession one: %v", err)
	}
	if _, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "s2"}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("CreateSession two error = %v, want ErrQuotaExceeded", err)
	}
}

func TestRunQuotaEnforced(t *testing.T) {
	svc, principal := quotaReadyService(t)
	inst, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "one"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "s1"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	one := 1
	high := 10
	if _, err := svc.UpdateUser(context.Background(), principal.TenantID, principal.UserID, UpdateUserInput{MaxRuns: &one, MaxMessages: &high}); err != nil {
		t.Fatalf("UpdateUser quota: %v", err)
	}
	if _, _, err := svc.PostMessage(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{Content: "hello"}); err != nil {
		t.Fatalf("PostMessage one: %v", err)
	}
	if _, _, err := svc.PostMessage(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{Content: "again"}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("PostMessage two error = %v, want ErrQuotaExceeded", err)
	}
}

func TestTenantQuotaOverridesUserQuota(t *testing.T) {
	svc, principal := quotaReadyService(t)
	one := 1
	two := 2
	if _, err := svc.UpdateTenant(context.Background(), principal.TenantID, UpdateTenantInput{MaxInstances: &one}); err != nil {
		t.Fatalf("UpdateTenant quota: %v", err)
	}
	if _, err := svc.UpdateUser(context.Background(), principal.TenantID, principal.UserID, UpdateUserInput{MaxInstances: &two}); err != nil {
		t.Fatalf("UpdateUser quota: %v", err)
	}
	if _, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "one"}); err != nil {
		t.Fatalf("CreateInstance one: %v", err)
	}
	if _, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "two"}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("CreateInstance two error = %v, want ErrQuotaExceeded", err)
	}
}
