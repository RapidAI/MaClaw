package agentservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestDeleteUserRemovesStateAndDirectories(t *testing.T) {
	svc := newStatusTestService(t)
	tenant, user := createStatusTestUser(t, svc)
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testDeleteLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if _, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{AgentID: "default"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := svc.CreateCredential(context.Background(), CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "key-delete-user", APISecret: "secret-delete-user"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}

	userRoot := svc.userRoot(tenant.ID, user.ID)
	if err := svc.DeleteUser(context.Background(), tenant.ID, user.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := svc.store.GetUser(tenant.ID, user.ID); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("GetUser error = %v, want ErrUserNotFound", err)
	}
	if _, err := os.Stat(userRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("user root should be removed, stat err = %v", err)
	}
}

func TestDeleteTenantRemovesStateAndDirectories(t *testing.T) {
	svc := newStatusTestService(t)
	tenant, user := createStatusTestUser(t, svc)
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testDeleteLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	if _, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "Instance"}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	tenantRoot := filepath.Join(svc.dataRoot, "tenants", slugID(tenant.ID))
	if err := svc.DeleteTenant(context.Background(), tenant.ID); err != nil {
		t.Fatalf("DeleteTenant: %v", err)
	}
	if _, err := svc.store.GetTenant(tenant.ID); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("GetTenant error = %v, want ErrTenantNotFound", err)
	}
	if _, err := os.Stat(tenantRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("tenant root should be removed, stat err = %v", err)
	}
}

func TestCredentialDetailUpdateAndRotate(t *testing.T) {
	svc := newStatusTestService(t)
	tenant, user := createStatusTestUser(t, svc)
	created, err := svc.CreateCredential(context.Background(), CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "Original", APIKey: "key-rotate", APISecret: "secret-old"})
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	fetched, err := svc.GetCredential(context.Background(), tenant.ID, user.ID, created.ID)
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	if fetched.APIKey == "" || fetched.APIKey == "key-rotate" || fetched.SecretDigest != "" {
		t.Fatalf("unexpected fetched credential: %#v", fetched)
	}
	updatedName := "Renamed"
	updated, err := svc.UpdateCredential(context.Background(), tenant.ID, user.ID, created.ID, UpdateCredentialInput{Name: &updatedName})
	if err != nil {
		t.Fatalf("UpdateCredential: %v", err)
	}
	if updated.Name != updatedName {
		t.Fatalf("updated name = %q, want %q", updated.Name, updatedName)
	}
	if _, err := svc.IssueToken(context.Background(), IssueTokenInput{APIKey: "key-rotate", APISecret: "secret-old"}); err != nil {
		t.Fatalf("IssueToken before rotate: %v", err)
	}
	rotated, err := svc.RotateCredentialSecret(context.Background(), tenant.ID, user.ID, created.ID, RotateCredentialSecretInput{APISecret: "secret-new"})
	if err != nil {
		t.Fatalf("RotateCredentialSecret: %v", err)
	}
	if rotated.SecretDigest != "" {
		t.Fatalf("rotated credential should be sanitized: %#v", rotated)
	}
	if _, err := svc.IssueToken(context.Background(), IssueTokenInput{APIKey: "key-rotate", APISecret: "secret-old"}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old secret error = %v, want ErrUnauthorized", err)
	}
	if _, err := svc.IssueToken(context.Background(), IssueTokenInput{APIKey: "key-rotate", APISecret: "secret-new"}); err != nil {
		t.Fatalf("IssueToken with new secret: %v", err)
	}
	rotatedKey, err := svc.RotateCredentialAPIKey(context.Background(), tenant.ID, user.ID, created.ID, RotateCredentialKeyInput{APIKey: "key-rotated"})
	if err != nil {
		t.Fatalf("RotateCredentialAPIKey: %v", err)
	}
	if rotatedKey.APIKey == "" || rotatedKey.APIKey == "key-rotated" || rotatedKey.SecretDigest != "" {
		t.Fatalf("rotated key credential should be sanitized: %#v", rotatedKey)
	}
	if _, err := svc.IssueToken(context.Background(), IssueTokenInput{APIKey: "key-rotate", APISecret: "secret-new"}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old key error = %v, want ErrUnauthorized", err)
	}
	if _, err := svc.IssueToken(context.Background(), IssueTokenInput{APIKey: "key-rotated", APISecret: "secret-new"}); err != nil {
		t.Fatalf("IssueToken with rotated key: %v", err)
	}
}

func testDeleteLLMConfig() corelib.AppConfig {
	return corelib.AppConfig{
		MaclawLLMUrl:   "https://llm.example/v1",
		MaclawLLMKey:   "test-key",
		MaclawLLMModel: "test-model",
	}
}
