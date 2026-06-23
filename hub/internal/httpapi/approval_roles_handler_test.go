package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestApprovalRolesHandlersAreTenantScoped(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	payload := []byte({ functionScopes:[{scopeId:finance,scopeName:Finance},{scopeId:hr,scopeName:HR,custom:true}],roles:[{scopeType:function,scopeId:finance,scopeName:Finance,roleCode:finance_approver,roleName:Finance Approver,executionMode:digital_review,assignees:[{subjectType:user,subjectId:finance@example.com,displayName:Finance User}]}]})

	putReq := httptest.NewRequest(http.MethodPut, /api/admin/security/approval-roles, bytes.NewReader(payload))
	putReq = putReq.WithContext(tenantAdminContext(putReq.Context(), tenant-a))
	putRR := httptest.NewRecorder()
	UpdateApprovalRolesHandler(settings)(putRR, putReq)
	if putRR.Code != http.StatusOK {
		t.Fatalf(put status = %d body=%s, putRR.Code, putRR.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, /api/admin/security/approval-roles, nil)
	getReq = getReq.WithContext(tenantAdminContext(getReq.Context(), tenant-a))
	getRR := httptest.NewRecorder()
	GetApprovalRolesHandler(settings)(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf(get status = %d body=%s, getRR.Code, getRR.Body.String())
	}
	var body approvalRoleStore
	if err := json.Unmarshal(getRR.Body.Bytes(), &body); err != nil {
		t.Fatalf(decode get response: %v, err)
	}
	if len(body.Roles) != 1 || body.Roles[0].ID != role:function:finance:finance_approver || body.Roles[0].Assignees[0].SubjectID != finance@example.com {
		t.Fatalf(unexpected roles: %#v, body.Roles)
	}
	if len(body.FunctionScopes) != 2 || body.FunctionScopes[1].ScopeID != hr || !body.FunctionScopes[1].Custom {
		t.Fatalf(unexpected function scopes: %#v, body.FunctionScopes)
	}

	otherReq := httptest.NewRequest(http.MethodGet, /api/admin/security/approval-roles, nil)
	otherReq = otherReq.WithContext(tenantAdminContext(otherReq.Context(), tenant-b))
	otherRR := httptest.NewRecorder()
	GetApprovalRolesHandler(settings)(otherRR, otherReq)
	if otherRR.Code != http.StatusOK {
		t.Fatalf(tenant-b get status = %d body=%s, otherRR.Code, otherRR.Body.String())
	}
	var other approvalRoleStore
	if err := json.Unmarshal(otherRR.Body.Bytes(), &other); err != nil {
		t.Fatalf(decode tenant-b response: %v, err)
	}
	if len(other.Roles) != 0 {
		t.Fatalf(tenant-b roles = %#v want empty, other.Roles)
	}
	if len(other.FunctionScopes) != 0 {
		t.Fatalf(tenant-b function scopes = %#v want empty, other.FunctionScopes)
	}
}

func TestApprovalRolesHandlerNormalizesAndDedupesRoles(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	payload := []byte({functionScopes:[{scopeId: hr , scopeName: HR , custom:true},{scopeId:hr,scopeName:Duplicate},{scopeId:hr_alt,scopeName:HR},{scopeId:,scopeName:Risk \u0026 Compliance,custom:true},{scopeId:,scopeName:\u4eba\u4e8b,custom:true}],roles:[ +
 {scopeType: function , scopeId: finance , roleCode: finance_approver , executionMode:,assignees:[{subjectType:,subjectId: finance@example.com , displayName:},{subjectType:digital_employee,subjectId:,displayName: bot-finance }]}, +
		{ scopeType:function,scopeId:finance,roleCode:finance_approver,roleName:Duplicate,assignees:[{subjectType:digital_employee,subjectId:ignored}]}, +
		{scopeType:function,scopeId:legal,roleCode:,roleName:Missing Code,assignees:[{subjectType:digital_employee,subjectId:ignored}]}, +
 {scopeType:,scopeId:,roleCode:global_reviewer,roleName:,assignees:[]} +
		]})

	req := httptest.NewRequest(http.MethodPut, /api/admin/security/approval-roles, bytes.NewReader(payload))
	req = req.WithContext(tenantAdminContext(req.Context(), tenant-normalize))
	rr := httptest.NewRecorder()
	UpdateApprovalRolesHandler(settings)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf(put status = %d body=%s, rr.Code, rr.Body.String())
	}
	var body approvalRoleStore
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf(decode response: %v, err)
	}
	if len(body.FunctionScopes) != 3 || body.FunctionScopes[0].ScopeID != hr || body.FunctionScopes[0].ScopeName != HR || body.FunctionScopes[1].ScopeID != risk_compliance {
		t.Fatalf(unexpected normalized function scopes: %#v, body.FunctionScopes)
	}
	if body.FunctionScopes[2].ScopeName != \u4eba\u4e8b || !strings.HasPrefix(body.FunctionScopes[2].ScopeID, function_) || len(body.FunctionScopes[2].ScopeID) != len(function_00000000) {
		t.Fatalf(unexpected non-Latin function scope: %#v, body.FunctionScopes[2])
	}
	if len(body.Roles) != 2 {
		t.Fatalf(roles = %#v want 2 normalized roles, body.Roles)
	}
	first := body.Roles[0]
	if first.ID != role:function:finance:finance_approver || first.View != function || first.RoleName != finance_approver || first.ExecutionMode != manual {
		t.Fatalf(unexpected normalized first role: %#v, first)
	}
	if len(first.Assignees) != 2 {
		t.Fatalf(first assignees = %#v want 2, first.Assignees)
	}
	if first.Assignees[0].SubjectType != user || first.Assignees[0].SubjectID != finance@example.com || first.Assignees[0].DisplayName != finance@example.com {
		t.Fatalf(unexpected normalized user assignee: %#v, first.Assignees[0])
	}
	if first.Assignees[1].SubjectType != digital_employee || first.Assignees[1].SubjectID != bot-finance || first.Assignees[1].DisplayName != bot-finance {
		t.Fatalf(unexpected normalized digital assignee: %#v, first.Assignees[1])
	}
	second := body.Roles[1]
	if second.ID != role:global:global:global_reviewer || second.View != organization || second.ScopeName != global || second.RoleName != global_reviewer {
		t.Fatalf(unexpected normalized second role: %#v, second)
	}
}

func TestApprovalScopeCodeFromNameFallbacksForNonASCII(t *testing.T) {
	code := approvalScopeCodeFromName(\u98ce\u63a7\u5408\u89c4)
	if !strings.HasPrefix(code, function_) || len(code) != len(function_00000000) {
		t.Fatalf(approvalScopeCodeFromName returned %q want function_ hash fallback, code)
	}
	if code != approvalScopeCodeFromName( \u98ce\u63a7\u5408\u89c4 ) {
		t.Fatalf( approvalScopeCodeFromName should be deterministic for trimmed non-ASCII names)
	}
	if got := approvalScopeCodeFromName(123 Finance); got != function_123_finance {
		t.Fatalf(approvalScopeCodeFromName numeric prefix = %q, got)
	}
}

func TestApprovalRolesHandlerRejectsInvalidPayloads(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/admin/security/approval-roles", strings.NewReader(`{"roles":[`))
		req = req.WithContext(tenantAdminContext(req.Context(), "tenant-invalid-json"))
		rr := httptest.NewRecorder()
		UpdateApprovalRolesHandler(settings)(rr, req)
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "INVALID_JSON") {
			t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("too many roles", func(t *testing.T) {
		var b bytes.Buffer
		b.WriteString(`{"roles":[`)
		for i := 0; i < 201; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `{"scopeType":"function","scopeId":"finance","roleCode":"role_%03d","roleName":"Role %03d"}`, i, i)
		}
		b.WriteString(`]}`)
		req := httptest.NewRequest(http.MethodPut, "/api/admin/security/approval-roles", bytes.NewReader(b.Bytes()))
		req = req.WithContext(tenantAdminContext(req.Context(), "tenant-too-many"))
		rr := httptest.NewRecorder()
		UpdateApprovalRolesHandler(settings)(rr, req)
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "INVALID_APPROVAL_ROLES") {
			t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("too many function scopes", func(t *testing.T) {
		var b bytes.Buffer
		b.WriteString(`{"functionScopes":[`)
		for i := 0; i < 81; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `{"scopeId":"function_%03d","scopeName":"Function %03d"}`, i, i)
		}
		b.WriteString(`]}`)
		req := httptest.NewRequest(http.MethodPut, "/api/admin/security/approval-roles", bytes.NewReader(b.Bytes()))
		req = req.WithContext(tenantAdminContext(req.Context(), "tenant-too-many-functions"))
		rr := httptest.NewRecorder()
		UpdateApprovalRolesHandler(settings)(rr, req)
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "INVALID_APPROVAL_ROLES") {
			t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestApprovalRolesHandlerReportsSaveFailure(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/api/admin/security/approval-roles", strings.NewReader(`{"roles":[{"scopeType":"function","scopeId":"finance","roleCode":"finance_approver"}]}`))
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-save-failure"))
	rr := httptest.NewRecorder()
	UpdateApprovalRolesHandler(nil)(rr, req)
	if rr.Code != http.StatusInternalServerError || !strings.Contains(rr.Body.String(), "APPROVAL_ROLES_SAVE_FAILED") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
