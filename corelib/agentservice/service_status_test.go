package agentservice

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDisabledUserCannotIssueOrUseToken(t *testing.T) {
	svc := newStatusTestService(t)
	tenant, user := createStatusTestUser(t, svc)
	if _, err := svc.CreateCredential(context.Background(), CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "key", APISecret: "secret"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	token, err := svc.IssueToken(context.Background(), IssueTokenInput{APIKey: "key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("IssueToken before disable: %v", err)
	}

	disabled := UserStatusDisabled
	if _, err := svc.UpdateUser(context.Background(), tenant.ID, user.ID, UpdateUserInput{Status: &disabled}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if _, err := svc.IssueToken(context.Background(), IssueTokenInput{APIKey: "key", APISecret: "secret"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("IssueToken after disable error = %v, want ErrForbidden", err)
	}
	if _, err := svc.Authenticate(token.AccessToken); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Authenticate after disable error = %v, want ErrForbidden", err)
	}
}

func TestDisabledTenantCannotUseToken(t *testing.T) {
	svc := newStatusTestService(t)
	tenant, user := createStatusTestUser(t, svc)
	if _, err := svc.CreateCredential(context.Background(), CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "key", APISecret: "secret"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	token, err := svc.IssueToken(context.Background(), IssueTokenInput{APIKey: "key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("IssueToken before disable: %v", err)
	}

	disabled := TenantStatusDisabled
	if _, err := svc.UpdateTenant(context.Background(), tenant.ID, UpdateTenantInput{Status: &disabled}); err != nil {
		t.Fatalf("UpdateTenant: %v", err)
	}
	if _, err := svc.Authenticate(token.AccessToken); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Authenticate after tenant disable error = %v, want ErrForbidden", err)
	}
}

func TestRevokedCredentialCannotIssueToken(t *testing.T) {
	svc := newStatusTestService(t)
	tenant, user := createStatusTestUser(t, svc)
	cred, err := svc.CreateCredential(context.Background(), CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	if _, err := svc.IssueToken(context.Background(), IssueTokenInput{APIKey: "key", APISecret: "secret"}); err != nil {
		t.Fatalf("IssueToken before revoke: %v", err)
	}
	if _, err := svc.RevokeCredential(context.Background(), tenant.ID, user.ID, cred.ID); err != nil {
		t.Fatalf("RevokeCredential: %v", err)
	}
	if _, err := svc.IssueToken(context.Background(), IssueTokenInput{APIKey: "key", APISecret: "secret"}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("IssueToken after revoke error = %v, want ErrUnauthorized", err)
	}
	items, err := svc.ListCredentials(context.Background(), tenant.ID, user.ID)
	if err != nil {
		t.Fatalf("ListCredentials: %v", err)
	}
	if len(items) != 1 || items[0].Status != CredentialStatusRevoked || items[0].SecretDigest != "" {
		t.Fatalf("credentials after revoke = %#v", items)
	}
}

func TestRotatedCredentialInvalidatesExistingToken(t *testing.T) {
	svc := newStatusTestService(t)
	tenant, user := createStatusTestUser(t, svc)
	cred, err := svc.CreateCredential(context.Background(), CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "rotate-key", APISecret: "secret-old"})
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	token, err := svc.IssueToken(context.Background(), IssueTokenInput{APIKey: "rotate-key", APISecret: "secret-old"})
	if err != nil {
		t.Fatalf("IssueToken before rotate: %v", err)
	}
	if _, err := svc.RotateCredentialSecret(context.Background(), tenant.ID, user.ID, cred.ID, RotateCredentialSecretInput{APISecret: "secret-new"}); err != nil {
		t.Fatalf("RotateCredentialSecret: %v", err)
	}
	if _, err := svc.Authenticate(token.AccessToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Authenticate after rotate error = %v, want ErrUnauthorized", err)
	}
	if _, err := svc.IssueToken(context.Background(), IssueTokenInput{APIKey: "rotate-key", APISecret: "secret-new"}); err != nil {
		t.Fatalf("IssueToken with new secret: %v", err)
	}
}

func TestCredentialRenameKeepsExistingTokenValid(t *testing.T) {
	svc := newStatusTestService(t)
	tenant, user := createStatusTestUser(t, svc)
	cred, err := svc.CreateCredential(context.Background(), CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "rename-key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	token, err := svc.IssueToken(context.Background(), IssueTokenInput{APIKey: "rename-key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("IssueToken before rename: %v", err)
	}
	newName := "Renamed API"
	if _, err := svc.UpdateCredential(context.Background(), tenant.ID, user.ID, cred.ID, UpdateCredentialInput{Name: &newName}); err != nil {
		t.Fatalf("UpdateCredential: %v", err)
	}
	if _, err := svc.Authenticate(token.AccessToken); err != nil {
		t.Fatalf("Authenticate after rename: %v", err)
	}
}

func newStatusTestService(t *testing.T) *Service {
	t.Helper()
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test", TokenTTL: time.Hour}, NewMemoryStore(), EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

func createStatusTestUser(t *testing.T, svc *Service) (*Tenant, *User) {
	t.Helper()
	tenant, err := svc.CreateTenant(context.Background(), CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return tenant, user
}

func TestCreateCredentialReturnsPlaintextKeyOnceButListsMasked(t *testing.T) {
	svc := newStatusTestService(t)
	tenant, user := createStatusTestUser(t, svc)
	cred, err := svc.CreateCredential(context.Background(), CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "plain-key-123", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	if cred.APIKey != "plain-key-123" {
		t.Fatalf("CreateCredential should return plaintext api_key once, got %#v", cred.APIKey)
	}
	if cred.APIKeyHash != "" {
		t.Fatalf("CreateCredential response should not expose api_key_hash, got %#v", cred.APIKeyHash)
	}
	items, err := svc.ListCredentials(context.Background(), tenant.ID, user.ID)
	if err != nil {
		t.Fatalf("ListCredentials: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ListCredentials len = %d", len(items))
	}
	if items[0].APIKey == "plain-key-123" || items[0].APIKey == "" {
		t.Fatalf("ListCredentials should return masked api_key, got %#v", items[0].APIKey)
	}
	if items[0].APIKeyHash != "" {
		t.Fatalf("ListCredentials should not expose api_key_hash, got %#v", items[0].APIKeyHash)
	}
}

func TestCredentialPepperProtectsSecretVerification(t *testing.T) {
	store := NewMemoryStore()
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test", TokenTTL: time.Hour, CredentialPepper: "pepper-secret-1234"}, store, EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService with pepper: %v", err)
	}
	tenant, user := createStatusTestUser(t, svc)
	if _, err := svc.CreateCredential(context.Background(), CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "key", APISecret: "secret"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	if _, err := svc.IssueToken(context.Background(), IssueTokenInput{APIKey: "key", APISecret: "secret"}); err != nil {
		t.Fatalf("IssueToken with pepper: %v", err)
	}
	withoutPepper, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test", TokenTTL: time.Hour}, store, EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService without pepper: %v", err)
	}
	if _, err := withoutPepper.IssueToken(context.Background(), IssueTokenInput{APIKey: "key", APISecret: "secret"}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("IssueToken without pepper error = %v, want ErrUnauthorized", err)
	}
}

func TestPepperedVerifierKeepsLegacyCompatibility(t *testing.T) {
	digest := HashSecret("secret")
	if !VerifySecretWithPepper("secret", digest, "pepper-secret-1234") {
		t.Fatalf("expected pepper-aware verifier to accept legacy non-peppered digest")
	}
}

func TestMemoryStoreDoesNotRetainPlaintextAPIKey(t *testing.T) {
	store := NewMemoryStore()
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test", TokenTTL: time.Hour}, store, EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, user := createStatusTestUser(t, svc)
	if _, err := svc.CreateCredential(context.Background(), CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "plain-key-123", APISecret: "secret"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	stored, err := store.GetCredentialByAPIKey("plain-key-123")
	if err != nil {
		t.Fatalf("GetCredentialByAPIKey: %v", err)
	}
	if stored.APIKey != "" {
		t.Fatalf("expected stored credential APIKey to be cleared, got %#v", stored.APIKey)
	}
	if stored.APIKeyHash == "" || stored.APIKeyPrefix == "" {
		t.Fatalf("expected stored credential hash/prefix, got %#v", stored)
	}
}
