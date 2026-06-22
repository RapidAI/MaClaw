package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApprovalRolesHandlersAreTenantScoped(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	payload := []byte(`{"roles":[{"scopeType":"function","scopeId":"finance","scopeName":"财务","roleCode":"finance_approver","roleName":"财务审批员","executionMode":"digital_review","assignees":[{"subjectType":"user","subjectId":"finance@example.com","displayName":"财务王"}]}]}`)

	putReq := httptest.NewRequest(http.MethodPut, "/api/admin/security/approval-roles", bytes.NewReader(payload))
	putReq = putReq.WithContext(tenantAdminContext(putReq.Context(), "tenant-a"))
	putRR := httptest.NewRecorder()
	UpdateApprovalRolesHandler(settings)(putRR, putReq)
	if putRR.Code != http.StatusOK {
		t.Fatalf("put status = %d body=%s", putRR.Code, putRR.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/admin/security/approval-roles", nil)
	getReq = getReq.WithContext(tenantAdminContext(getReq.Context(), "tenant-a"))
	getRR := httptest.NewRecorder()
	GetApprovalRolesHandler(settings)(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", getRR.Code, getRR.Body.String())
	}
	var body approvalRoleStore
	if err := json.Unmarshal(getRR.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if len(body.Roles) != 1 || body.Roles[0].ID != "role:function:finance:finance_approver" || body.Roles[0].Assignees[0].SubjectID != "finance@example.com" {
		t.Fatalf("unexpected roles: %#v", body.Roles)
	}

	otherReq := httptest.NewRequest(http.MethodGet, "/api/admin/security/approval-roles", nil)
	otherReq = otherReq.WithContext(tenantAdminContext(otherReq.Context(), "tenant-b"))
	otherRR := httptest.NewRecorder()
	GetApprovalRolesHandler(settings)(otherRR, otherReq)
	if otherRR.Code != http.StatusOK {
		t.Fatalf("other get status = %d body=%s", otherRR.Code, otherRR.Body.String())
	}
	var other approvalRoleStore
	if err := json.Unmarshal(otherRR.Body.Bytes(), &other); err != nil {
		t.Fatalf("decode other response: %v", err)
	}
	if len(other.Roles) != 0 {
		t.Fatalf("tenant-b roles = %#v, want empty", other.Roles)
	}
}
