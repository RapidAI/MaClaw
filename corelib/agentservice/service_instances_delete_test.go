package agentservice

import (
	"context"
	"testing"
	"time"
)

func setupDeleteInstanceTest(t *testing.T) (svc *Service, store *MemoryStore, principal Principal, inst *Instance, sess *Session) {
	t.Helper()
	store = NewMemoryStore()
	var err error
	svc, err = NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, EchoExecutor{})
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
	principal = Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testDeleteLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err = svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err = svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "Demo"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return svc, store, principal, inst, sess
}

func TestDeleteInstanceCancelsRunningRunThenDeletes(t *testing.T) {
	svc, store, principal, inst, sess := setupDeleteInstanceTest(t)
	started := time.Now().UTC().Add(-time.Hour)
	if err := store.SaveRun(Run{ID: "run_busy", TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID, SessionID: sess.ID, Status: RunStatusRunning, StartedAt: started}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if err := svc.DeleteInstance(context.Background(), principal, inst.ID); err != nil {
		t.Fatalf("DeleteInstance: %v", err)
	}
	if _, err := svc.GetInstance(context.Background(), principal, inst.ID); err == nil {
		t.Fatalf("expected deleted instance to be missing")
	}
	if _, err := store.GetRun(principal.TenantID, principal.UserID, inst.ID, "run_busy"); err == nil {
		t.Fatalf("expected cancelled run to be removed with the instance")
	}
}

func TestDeleteInstanceCancelsJustAdmittedRunThenDeletes(t *testing.T) {
	svc, store, principal, inst, sess := setupDeleteInstanceTest(t)
	if err := store.SaveRun(Run{ID: "run_new", TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID, SessionID: sess.ID, Status: RunStatusRunning, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if err := svc.DeleteInstance(context.Background(), principal, inst.ID); err != nil {
		t.Fatalf("DeleteInstance: %v", err)
	}
	if _, err := svc.GetInstance(context.Background(), principal, inst.ID); err == nil {
		t.Fatalf("expected deleted instance to be missing")
	}
}
