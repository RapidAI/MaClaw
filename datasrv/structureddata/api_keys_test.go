package structureddata

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestAPIKeyPolicyRejectsInvalidExplicitRole(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_1", UserID: "admin_1", Role: "data_admin"}

	if _, err := svc.CreateAPIKeyPolicy(context.Background(), p, CreateAPIKeyPolicyInput{
		ID:     "bad_role",
		UserID: "agent_bad",
		Role:   "owner",
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateAPIKeyPolicy invalid role error=%v, want ErrInvalidInput", err)
	}
}

func TestAPIKeyPolicyUpdateKeepsRoleWhenOmitted(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_1", UserID: "admin_1", Role: "data_admin"}
	created, err := svc.CreateAPIKeyPolicy(context.Background(), p, CreateAPIKeyPolicyInput{
		ID:             "auditor_key",
		UserID:         "agent_auditor",
		Role:           "data_auditor",
		AllowedReports: []string{"finance.expense_by_department"},
		AllowRawData:   true,
		AllowSensitive: true,
		ExpiresAt:      "2030-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("CreateAPIKeyPolicy: %v", err)
	}
	updated, err := svc.UpdateAPIKeyPolicy(context.Background(), p, created.Policy.ID, UpdateAPIKeyPolicyInput{
		Note: strPtr("updated without role"),
	})
	if err != nil {
		t.Fatalf("UpdateAPIKeyPolicy without role: %v", err)
	}
	if updated.Role != "data_auditor" {
		t.Fatalf("UpdateAPIKeyPolicy without role changed role to %q", updated.Role)
	}
	if updated.UserID != "agent_auditor" || !updated.AllowRawData || !updated.AllowSensitive || updated.ExpiresAt == nil || !containsString(updated.AllowedReports, "finance.expense_by_department") {
		t.Fatalf("UpdateAPIKeyPolicy without fields should preserve existing policy: %#v", updated)
	}
	allowSensitive := false
	empty := ""
	updated, err = svc.UpdateAPIKeyPolicy(context.Background(), p, created.Policy.ID, UpdateAPIKeyPolicyInput{
		AllowedReports: []string{},
		AllowSensitive: &allowSensitive,
		UserID:         &empty,
		Note:           &empty,
	})
	if err != nil {
		t.Fatalf("UpdateAPIKeyPolicy explicit clear: %v", err)
	}
	if updated.AllowSensitive || len(updated.AllowedReports) != 0 || !updated.AllowRawData || updated.UserID != "" || updated.Note != "" {
		t.Fatalf("explicit update should clear only requested fields and preserve others: %#v", updated)
	}
	if _, err := svc.UpdateAPIKeyPolicy(context.Background(), p, created.Policy.ID, UpdateAPIKeyPolicyInput{
		UserID: strPtr("agent_auditor"),
		Role:   "owner",
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpdateAPIKeyPolicy invalid role error=%v, want ErrInvalidInput", err)
	}
}

func strPtr(value string) *string {
	return &value
}
