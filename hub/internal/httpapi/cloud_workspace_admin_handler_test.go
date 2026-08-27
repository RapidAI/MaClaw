package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
)

type memoryCloudWorkspaceSettings map[string]string

func (m memoryCloudWorkspaceSettings) Set(ctx context.Context, key, valueJSON string) error {
	m[key] = valueJSON
	return nil
}

func (m memoryCloudWorkspaceSettings) Get(ctx context.Context, key string) (string, error) {
	return m[key], nil
}

type fakeCloudWorkspaceOrg struct {
	tree    *security.GroupTreeNode
	members map[string][]string
}

func (f *fakeCloudWorkspaceOrg) GetUserGroupID(ctx context.Context, email string) (string, error) {
	return "", nil
}

func (f *fakeCloudWorkspaceOrg) GetGroupByID(ctx context.Context, id string) (*security.SecurityGroup, error) {
	return nil, nil
}

func (f *fakeCloudWorkspaceOrg) GetGroupTree(ctx context.Context) (*security.GroupTreeNode, error) {
	if f == nil {
		return nil, nil
	}
	return f.tree, nil
}

func (f *fakeCloudWorkspaceOrg) ListGroupMembers(ctx context.Context, groupID string) ([]string, error) {
	if f == nil || f.members == nil {
		return nil, nil
	}
	return f.members[groupID], nil
}

func newCloudWorkspaceTestService() *cloudworkspace.Service {
	org := &fakeCloudWorkspaceOrg{
		tree: &security.GroupTreeNode{
			ID: "root",
			Children: []*security.GroupTreeNode{
				{ID: "eng", Children: []*security.GroupTreeNode{{ID: "backend"}}},
				{ID: "sales"},
			},
		},
		members: map[string][]string{
			"eng":     {"eng@x.com"},
			"backend": {"dev@x.com"},
			"sales":   {"sales@x.com"},
		},
	}
	return &cloudworkspace.Service{
		System: memoryCloudWorkspaceSettings{},
		Groups: org,
		Org:    org,
	}
}

func TestGetCloudWorkspaceSettingsDefaultOff(t *testing.T) {
	svc := newCloudWorkspaceTestService()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cloud-workspaces/settings", nil)
	GetCloudWorkspaceSettingsAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got cloudworkspace.SettingsView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Mode != cloudworkspace.ModeOff {
		t.Fatalf("mode=%q want off", got.Mode)
	}
	if got.Quota != 5 {
		t.Fatalf("quota=%d want 5", got.Quota)
	}
	if got.MaxWorkspaceBytes != 2<<30 {
		t.Fatalf("max_workspace_bytes=%d", got.MaxWorkspaceBytes)
	}
	if got.TenantMaxTotalBytes != 50<<30 {
		t.Fatalf("tenant_max_total_bytes=%d", got.TenantMaxTotalBytes)
	}
	if got.Preview.OverQuotaUsers == nil || len(got.Preview.OverQuotaUsers) != 0 {
		t.Fatalf("over_quota_users=%v", got.Preview.OverQuotaUsers)
	}
	if got.Preview.UsedBytes != 0 || got.Preview.DepartmentCount != 0 || got.Preview.UserCount != 0 {
		t.Fatalf("preview=%+v", got.Preview)
	}
}

func TestPutCloudWorkspaceSettingsRejectsDepartmentsWithoutIDs(t *testing.T) {
	svc := newCloudWorkspaceTestService()
	body, _ := json.Marshal(map[string]any{"mode": "departments", "department_ids": []string{}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/admin/cloud-workspaces/settings", bytes.NewReader(body))
	PutCloudWorkspaceSettingsAdminHandler(svc, nil)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != "INVALID_INPUT" {
		t.Fatalf("code=%q body=%s", payload.Code, rec.Body.String())
	}
}

func TestPutCloudWorkspaceSettingsClampsQuotaAndReturnsPreview(t *testing.T) {
	svc := newCloudWorkspaceTestService()
	body, _ := json.Marshal(map[string]any{
		"mode":           "departments",
		"quota":          99,
		"department_ids": []string{"eng"},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/admin/cloud-workspaces/settings", bytes.NewReader(body))
	PutCloudWorkspaceSettingsAdminHandler(svc, nil)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got cloudworkspace.SettingsView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Quota != 10 {
		t.Fatalf("quota=%d want 10", got.Quota)
	}
	if got.Preview.DepartmentCount != 2 {
		t.Fatalf("department_count=%d want 2", got.Preview.DepartmentCount)
	}
	if got.Preview.UserCount != 2 {
		t.Fatalf("user_count=%d want 2", got.Preview.UserCount)
	}
}

func TestPutCloudWorkspaceSettingsZeroQuotaClampsToOne(t *testing.T) {
	svc := newCloudWorkspaceTestService()
	body, _ := json.Marshal(map[string]any{"mode": "all_users", "quota": 0})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/admin/cloud-workspaces/settings", bytes.NewReader(body))
	PutCloudWorkspaceSettingsAdminHandler(svc, nil)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got cloudworkspace.SettingsView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Quota != 1 {
		t.Fatalf("quota=%d want 1", got.Quota)
	}
	if got.Mode != cloudworkspace.ModeAllUsers {
		t.Fatalf("mode=%q", got.Mode)
	}
}
