package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	securitysvc "github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

type testSystemSettings struct {
	data map[string]string
}

func (m *testSystemSettings) Set(_ context.Context, key, valueJSON string) error {
	m.data[key] = valueJSON
	return nil
}

func (m *testSystemSettings) Get(_ context.Context, key string) (string, error) {
	return m.data[key], nil
}

func newSecurityHandlerTestService(t *testing.T, dbName string) (*securitysvc.SecurityService, *securitysvc.SecurityStore) {
	t.Helper()
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: filepath.Join(t.TempDir(), dbName)})
	if err != nil {
		t.Fatalf("new sqlite provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	secStore := securitysvc.NewSecurityStore(provider.Write)
	ctx := t.Context()
	if err := secStore.InitSchema(ctx); err != nil {
		t.Fatalf("init security schema: %v", err)
	}
	if err := secStore.InitRootGroup(ctx); err != nil {
		t.Fatalf("init root group: %v", err)
	}
	return securitysvc.NewSecurityService(secStore, &testSystemSettings{data: map[string]string{}}, nil), secStore
}

func TestUpdateGroupPolicyHandlerRejectsInvalidValues(t *testing.T) {
	svc, secStore := newSecurityHandlerTestService(t, "security-policy-invalid.db")
	root, err := secStore.GetRootGroup(t.Context())
	if err != nil || root == nil {
		t.Fatalf("get root: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/admin/security/groups/"+root.ID+"/policy", bytes.NewBufferString(`{"policy":{"sandbox_mode":"strict"}}`))
	req.SetPathValue("id", root.ID)
	rr := httptest.NewRecorder()
	UpdateGroupPolicyHandler(svc).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid policy status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSecurityUserEffectivePolicyResponseIncludesGroupPathAndSources(t *testing.T) {
	svc, secStore := newSecurityHandlerTestService(t, "security-handler.db")
	ctx := t.Context()

	root, err := secStore.GetRootGroup(ctx)
	if err != nil {
		t.Fatalf("get root: %v", err)
	}
	if root == nil || root.ID == "" {
		t.Fatalf("missing root group")
	}
	if err := svc.UpdateGroupPolicy(ctx, root.ID, map[string]interface{}{"gossip_enabled": false}); err != nil {
		t.Fatalf("set root policy: %v", err)
	}
	child, err := svc.CreateGroup(ctx, "Legal", root.ID)
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if err := svc.UpdateGroupPolicy(ctx, child.ID, map[string]interface{}{"guardrail_mode": "strict"}); err != nil {
		t.Fatalf("set child policy: %v", err)
	}
	if err := svc.AssignUser(ctx, "lawyer@example.com", child.ID); err != nil {
		t.Fatalf("assign user: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/security/users/lawyer@example.com/effective-policy", nil)
	req.SetPathValue("email", "lawyer@example.com")
	rr := httptest.NewRecorder()
	GetUserEffectivePolicyHandler(svc).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("effective policy status = %d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		GroupID   string `json:"group_id"`
		GroupPath []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"group_path"`
		Policy struct {
			GossipEnabled bool   `json:"gossip_enabled"`
			GuardrailMode string `json:"guardrail_mode"`
		} `json:"policy"`
		GroupPolicy struct {
			Items map[string]struct {
				Source      string `json:"source"`
				SourceGroup string `json:"source_group"`
				SourceName  string `json:"source_name"`
			} `json:"items"`
		} `json:"group_policy"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode effective policy: %v body=%s", err, rr.Body.String())
	}
	if payload.GroupID != child.ID {
		t.Fatalf("group_id = %q, want %q", payload.GroupID, child.ID)
	}
	if len(payload.GroupPath) != 2 || payload.GroupPath[0].ID != root.ID || payload.GroupPath[1].ID != child.ID || payload.GroupPath[1].Name != "Legal" {
		t.Fatalf("unexpected group_path: %#v", payload.GroupPath)
	}
	if payload.Policy.GossipEnabled || payload.Policy.GuardrailMode != "strict" {
		t.Fatalf("unexpected policy: %#v", payload.Policy)
	}
	if payload.GroupPolicy.Items["gossip_enabled"].Source != "inherited" || payload.GroupPolicy.Items["gossip_enabled"].SourceGroup != root.ID {
		t.Fatalf("unexpected gossip source: %#v", payload.GroupPolicy.Items["gossip_enabled"])
	}
	if payload.GroupPolicy.Items["guardrail_mode"].Source != "self" || payload.GroupPolicy.Items["guardrail_mode"].SourceGroup != child.ID {
		t.Fatalf("unexpected guardrail source: %#v", payload.GroupPolicy.Items["guardrail_mode"])
	}
}

func TestGetSecuritySettingsHandlerIncludesDefaultGroupName(t *testing.T) {
	svc, secStore := newSecurityHandlerTestService(t, "security-settings-handler.db")
	ctx := t.Context()
	root, err := secStore.GetRootGroup(ctx)
	if err != nil {
		t.Fatalf("get root: %v", err)
	}
	child, err := svc.CreateGroup(ctx, "New Users", root.ID)
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if err := svc.SetDefaultGroup(ctx, child.ID); err != nil {
		t.Fatalf("set default group: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/security/settings", nil)
	rr := httptest.NewRecorder()
	GetSecuritySettingsHandler(svc).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("settings status = %d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		DefaultGroupID   string `json:"default_group_id"`
		DefaultGroupName string `json:"default_group_name"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode settings: %v body=%s", err, rr.Body.String())
	}
	if payload.DefaultGroupID != child.ID || payload.DefaultGroupName != "New Users" {
		t.Fatalf("unexpected default group payload: %#v", payload)
	}
}
