package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestOpenAPIDocumentIsAvailable(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("openapi status = %d body = %s", w.Code, w.Body.String())
	}
	var doc struct {
		OpenAPI    string         `json:"openapi"`
		Paths      map[string]any `json:"paths"`
		Components struct {
			SecuritySchemes map[string]any `json:"securitySchemes"`
		} `json:"components"`
	}
	if err := json.NewDecoder(w.Body).Decode(&doc); err != nil {
		t.Fatalf("decode openapi: %v", err)
	}
	if doc.OpenAPI == "" {
		t.Fatalf("missing openapi version: %#v", doc)
	}
	if _, ok := doc.Paths["/api/v1/instances"]; !ok {
		t.Fatalf("expected instance path in openapi doc")
	}
	for _, path := range []string{
		"/api/platform/virtual-employees/{employeeId}/config",
		"/api/platform/source-users/runtime-status",
		"/api/platform/source-users/{sourceUserId}/runtime-status",
		"/api/platform/source-users/{sourceUserId}/assistant-instances",
		"/api/platform/source-users/{sourceUserId}/assistant-link",
		"/api/platform/source-users/{sourceUserId}/settings-link",
	} {
		if _, ok := doc.Paths[path]; !ok {
			t.Fatalf("expected source-user platform path %s in openapi doc", path)
		}
	}
	if _, ok := doc.Components.SecuritySchemes["bearerAuth"]; !ok {
		t.Fatalf("expected bearerAuth security scheme")
	}
	runsPath, ok := doc.Paths["/api/v1/instances/{instanceId}/runs"].(map[string]any)
	if !ok {
		t.Fatalf("expected runs path object")
	}
	getRunList, ok := runsPath["get"].(map[string]any)
	if !ok {
		t.Fatalf("expected GET runs operation")
	}
	params, ok := getRunList["parameters"].([]any)
	if !ok {
		t.Fatalf("expected parameters array on run list operation")
	}
	foundStatusEnum := false
	foundWaitingBool := false
	for _, item := range params {
		param, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := param["name"].(string)
		schema, _ := param["schema"].(map[string]any)
		switch name {
		case "status":
			enumValues, _ := schema["enum"].([]any)
			if len(enumValues) == 4 {
				foundStatusEnum = true
			}
		case "waiting_for_user":
			if schema["type"] == "boolean" {
				foundWaitingBool = true
			}
		}
	}
	if !foundStatusEnum {
		t.Fatalf("expected enum schema for run status query parameter")
	}
	if !foundWaitingBool {
		t.Fatalf("expected boolean schema for waiting_for_user query parameter")
	}
	metricsPath, ok := doc.Paths["/metrics"].(map[string]any)
	if !ok {
		t.Fatalf("expected metrics path object")
	}
	getMetrics, ok := metricsPath["get"].(map[string]any)
	if !ok {
		t.Fatalf("expected GET metrics operation")
	}
	if description, _ := getMetrics["description"].(string); !strings.Contains(description, "credential") {
		t.Fatalf("expected metrics description to mention credential gauges: %#v", getMetrics)
	}
	responses, ok := getMetrics["responses"].(map[string]any)
	if !ok {
		t.Fatalf("expected metrics responses object")
	}
	okResponse, ok := responses["200"].(map[string]any)
	if !ok {
		t.Fatalf("expected metrics 200 response")
	}
	content, ok := okResponse["content"].(map[string]any)
	if !ok {
		t.Fatalf("expected metrics response content")
	}
	if _, ok := content["text/plain"]; !ok {
		t.Fatalf("expected metrics text/plain response content: %#v", content)
	}
	credentialsPath, ok := doc.Paths["/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials"].(map[string]any)
	if !ok {
		t.Fatalf("expected credentials path object")
	}
	getCredentials, ok := credentialsPath["get"].(map[string]any)
	if !ok {
		t.Fatalf("expected GET credentials operation")
	}
	credentialParams, ok := getCredentials["parameters"].([]any)
	if !ok {
		t.Fatalf("expected credential list parameters")
	}
	foundCredentialStatus := false
	foundExpiredBool := false
	foundExpiringBool := false
	for _, item := range credentialParams {
		param, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := param["name"].(string)
		schema, _ := param["schema"].(map[string]any)
		switch name {
		case "status":
			enumValues, _ := schema["enum"].([]any)
			if len(enumValues) == 3 {
				foundCredentialStatus = true
			}
		case "expired":
			if schema["type"] == "boolean" {
				foundExpiredBool = true
			}
		case "expiring":
			if schema["type"] == "boolean" {
				foundExpiringBool = true
			}
		}
	}
	if !foundCredentialStatus || !foundExpiredBool || !foundExpiringBool {
		t.Fatalf("expected credential filters in OpenAPI: %#v", credentialParams)
	}
	summaryPath, ok := doc.Paths["/api/v1/admin/security/summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected security summary path object")
	}
	summaryGet, ok := summaryPath["get"].(map[string]any)
	if !ok {
		t.Fatalf("expected GET security summary operation")
	}
	summaryDescription, _ := summaryGet["description"].(string)
	if !strings.Contains(summaryDescription, "generated_at") || !strings.Contains(summaryDescription, "applied filters") {
		t.Fatalf("expected security summary description to mention generated_at and filters: %#v", summaryGet)
	}
	supportBundlePath, ok := doc.Paths["/api/v1/admin/sandbox/support-bundle"].(map[string]any)
	if !ok {
		t.Fatalf("expected sandbox support-bundle path object")
	}
	supportBundleGet, ok := supportBundlePath["get"].(map[string]any)
	if !ok {
		t.Fatalf("expected GET sandbox support-bundle operation")
	}
	supportBundleDescription, _ := supportBundleGet["description"].(string)
	if !strings.Contains(supportBundleDescription, "security_risks") || !strings.Contains(supportBundleDescription, "generated_at") || !strings.Contains(supportBundleDescription, "filters") || !strings.Contains(supportBundleDescription, "redacted data root") || !strings.Contains(supportBundleDescription, "redactions") {
		t.Fatalf("expected support bundle description to mention security risks generated_at and filters: %#v", supportBundleGet)
	}
	auditPath, ok := doc.Paths["/api/v1/admin/audit-events"].(map[string]any)
	if !ok {
		t.Fatalf("expected audit-events path object")
	}
	getAuditEvents, ok := auditPath["get"].(map[string]any)
	if !ok {
		t.Fatalf("expected GET audit-events operation")
	}
	auditDescription, _ := getAuditEvents["description"].(string)
	if !strings.Contains(auditDescription, "redacted") || !strings.Contains(auditDescription, "filters are applied before redaction") {
		t.Fatalf("expected audit-events description to document redaction: %#v", getAuditEvents)
	}
	auditParams, ok := getAuditEvents["parameters"].([]any)
	if !ok {
		t.Fatalf("expected audit-events parameters")
	}
	foundResourceID := false
	foundActorType := false
	foundActorTenant := false
	foundActorUser := false
	foundAuditSince := false
	foundAuditUntil := false
	for _, item := range auditParams {
		param, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := param["name"].(string)
		schema, _ := param["schema"].(map[string]any)
		switch name {
		case "resource_id":
			foundResourceID = true
		case "actor_type":
			foundActorType = true
		case "actor_tenant_id":
			foundActorTenant = true
		case "actor_user_id":
			foundActorUser = true
		case "since":
			if schema["format"] == "date-time" {
				foundAuditSince = true
			}
		case "until":
			if schema["format"] == "date-time" {
				foundAuditUntil = true
			}
		}
	}
	if !foundResourceID || !foundActorType || !foundActorTenant || !foundActorUser || !foundAuditSince || !foundAuditUntil {
		t.Fatalf("expected audit resource/actor/time filters in OpenAPI: %#v", auditParams)
	}
	riskPath, ok := doc.Paths["/api/v1/admin/security/risk-events"].(map[string]any)
	if !ok {
		t.Fatalf("expected risk-events path object")
	}
	getRiskEvents, ok := riskPath["get"].(map[string]any)
	if !ok {
		t.Fatalf("expected GET risk-events operation")
	}
	riskDescription, _ := getRiskEvents["description"].(string)
	if !strings.Contains(riskDescription, "generated_at") || !strings.Contains(riskDescription, "applied filters") || !strings.Contains(riskDescription, "redact sensitive metadata") {
		t.Fatalf("expected risk-events description to mention generated_at, filters, and redaction: %#v", getRiskEvents)
	}
	riskParams, ok := getRiskEvents["parameters"].([]any)
	if !ok {
		t.Fatalf("expected risk-events parameters")
	}
	foundRiskSeverity := false
	foundRiskKindDescription := false
	foundRiskTimeDescription := false
	for _, item := range riskParams {
		param, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := param["name"].(string)
		description, _ := param["description"].(string)
		schema, _ := param["schema"].(map[string]any)
		switch name {
		case "severity":
			enumValues, _ := schema["enum"].([]any)
			if len(enumValues) == 3 {
				foundRiskSeverity = true
			}
		case "kind":
			if strings.Contains(description, "Stable risk kind") && strings.Contains(description, "sandbox_failed") && strings.Contains(description, "case-insensitive") {
				foundRiskKindDescription = true
			}
		case "since":
			if strings.Contains(description, "before or equal") {
				foundRiskTimeDescription = true
			}
		}
	}
	if !foundRiskSeverity || !foundRiskKindDescription || !foundRiskTimeDescription {
		t.Fatalf("expected risk severity/kind/time filters in OpenAPI: %#v", riskParams)
	}
	ownerSandboxPath, ok := doc.Paths["/api/v1/admin/sandbox/config"].(map[string]any)
	if !ok {
		t.Fatalf("expected sandbox config path object")
	}
	ownerSandboxPut, ok := ownerSandboxPath["put"].(map[string]any)
	if !ok {
		t.Fatalf("expected PUT sandbox config operation")
	}
	if role, _ := ownerSandboxPut["x-maclaw-admin-role"].(string); role != "owner" {
		t.Fatalf("expected sandbox config PUT to require owner role: %#v", ownerSandboxPut)
	}
	ownerDescription, _ := ownerSandboxPut["description"].(string)
	if !strings.Contains(ownerDescription, "owner role") || !strings.Contains(ownerDescription, "root admin secret") {
		t.Fatalf("expected owner role description on sandbox config PUT: %#v", ownerSandboxPut)
	}
	ownerResponses, ok := ownerSandboxPut["responses"].(map[string]any)
	if !ok {
		t.Fatalf("expected owner route responses object")
	}
	if _, ok := ownerResponses["403"]; !ok {
		t.Fatalf("expected owner route to document 403: %#v", ownerResponses)
	}
	operatorSandboxPath, ok := doc.Paths["/api/v1/admin/sandbox/status"].(map[string]any)
	if !ok {
		t.Fatalf("expected sandbox status path object")
	}
	operatorSandboxGet, ok := operatorSandboxPath["get"].(map[string]any)
	if !ok {
		t.Fatalf("expected GET sandbox status operation")
	}
	if role, _ := operatorSandboxGet["x-maclaw-admin-role"].(string); role != "operator" {
		t.Fatalf("expected sandbox status GET to allow operator role: %#v", operatorSandboxGet)
	}
	recordsPath, ok := doc.Paths["/api/v1/records/{collection}"].(map[string]any)
	if !ok {
		t.Fatalf("expected records collection path object")
	}
	getRecords, ok := recordsPath["get"].(map[string]any)
	if !ok {
		t.Fatalf("expected GET records operation")
	}
	recordParams, ok := getRecords["parameters"].([]any)
	if !ok {
		t.Fatalf("expected records parameters")
	}
	foundTag := false
	foundQ := false
	for _, item := range recordParams {
		param, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := param["name"].(string)
		schema, _ := param["schema"].(map[string]any)
		if name == "tag" && schema["type"] == "string" {
			foundTag = true
		}
		if name == "q" && schema["type"] == "string" {
			foundQ = true
		}
	}
	if !foundTag || !foundQ {
		t.Fatalf("expected record tag and q filters in OpenAPI: %#v", recordParams)
	}
	for _, path := range []string{
		"/api/v1/admin/knowledge/stats",
		"/api/v1/admin/knowledge/sources",
		"/api/v1/admin/tenants/{tenantId}/knowledge",
		"/api/v1/admin/skill-sources/available",
		"/api/v1/admin/skill-sources/global",
		"/api/v1/admin/skill-sources/tenant/{id}",
		"/api/v1/admin/skill-sources/tenants/{tenantId}/users/{userId}",
		"/api/v1/admin/skill-sources/tenants/{tenantId}/users/{userId}/resolve",
	} {
		if _, ok := doc.Paths[path]; !ok {
			t.Fatalf("expected admin knowledge/skill path %s in openapi doc", path)
		}
	}
}

func TestOpenAPIKnowledgeFileImportsUseMultipart(t *testing.T) {
	doc := buildOpenAPISpec()
	paths, ok := doc["paths"].(map[string]map[string]any)
	if !ok {
		t.Fatalf("expected typed OpenAPI paths map")
	}
	for _, path := range []string{
		"/api/v1/knowledge/import/file",
		"/api/v1/admin/public-knowledge-libraries/{libraryId}/import/file",
	} {
		pathItem, ok := paths[path]
		if !ok {
			t.Fatalf("missing OpenAPI path %s", path)
		}
		op, ok := pathItem["post"].(map[string]any)
		if !ok {
			t.Fatalf("missing POST operation %s", path)
		}
		description, _ := op["description"].(string)
		if !strings.Contains(description, "one or more") || !strings.Contains(description, "async job") {
			t.Fatalf("file import description does not describe batch async import: %q", description)
		}
		requestBody, ok := op["requestBody"].(map[string]any)
		if !ok || requestBody["required"] != true {
			t.Fatalf("missing required request body for %s: %#v", path, op["requestBody"])
		}
		content, ok := requestBody["content"].(map[string]any)
		if !ok {
			t.Fatalf("missing request content for %s: %#v", path, requestBody)
		}
		multipart, ok := content["multipart/form-data"].(map[string]any)
		if !ok {
			t.Fatalf("file import must use multipart/form-data, got %#v", content)
		}
		schema, ok := multipart["schema"].(map[string]any)
		if !ok {
			t.Fatalf("missing multipart schema for %s: %#v", path, multipart)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("missing multipart properties for %s: %#v", path, schema)
		}
		fileProp, ok := properties["file"].(map[string]any)
		if !ok || fileProp["type"] != "array" || fileProp["maxItems"] != maxKnowledgeUploadFiles {
			t.Fatalf("file property should be bounded binary array for %s: %#v", path, fileProp)
		}
		items, ok := fileProp["items"].(map[string]any)
		if !ok || items["format"] != "binary" {
			t.Fatalf("file items should be binary for %s: %#v", path, fileProp)
		}
	}
}

func TestOpenAPICoversRegisteredAdminRoutes(t *testing.T) {
	source, err := os.ReadFile("http.go")
	if err != nil {
		t.Fatalf("read http.go: %v", err)
	}
	registered := map[string]bool{}
	re := regexp.MustCompile(`s\.mux\.HandleFunc\("([A-Z]+) ([^"]+)"`)
	for _, match := range re.FindAllStringSubmatch(string(source), -1) {
		method, path := match[1], match[2]
		if strings.HasPrefix(path, "/api/v1/admin/") {
			registered[method+" "+path] = true
		}
	}
	if len(registered) == 0 {
		t.Fatalf("expected registered admin routes in http.go")
	}
	documented := map[string]bool{}
	for _, route := range openAPIRoutes {
		if strings.HasPrefix(route.Path, "/api/v1/admin/") {
			documented[route.Method+" "+route.Path] = true
		}
	}
	for route := range registered {
		if !documented[route] {
			t.Fatalf("registered admin route missing from OpenAPI: %s", route)
		}
	}
	for route := range documented {
		if !registered[route] {
			t.Fatalf("OpenAPI admin route is not registered: %s", route)
		}
	}
}
func TestOpenAPIAdminMutationsAreOwnerOnlyUnlessExplicitlyAllowed(t *testing.T) {
	allowedOperatorOrPublic := map[string]bool{
		http.MethodPost + " /api/v1/admin/bootstrap/initialize":                              true,
		http.MethodPost + " /api/v1/admin/auth/login":                                        true,
		http.MethodPost + " /api/v1/admin/auth/logout":                                       true,
		http.MethodPost + " /api/v1/admin/auth/change-password":                              true,
		http.MethodPost + " /api/v1/admin/logs/search":                                       true,
		http.MethodPost + " /api/v1/admin/service-config/validate":                           true,
		http.MethodPost + " /api/v1/admin/tenants/{tenantId}/users/{userId}/config/validate": true,
		http.MethodPost + " /api/v1/admin/tenants/{tenantId}/users/{userId}/config/test":     true,
		http.MethodPost + " /api/v1/admin/sandbox/detect":                                    true,
		http.MethodPost + " /api/v1/admin/sandbox/smoke-test":                                true,
		http.MethodPost + " /api/v1/admin/sandbox/diagnose":                                  true,
		http.MethodPost + " /api/v1/admin/sandbox/profiles/{profileName}/validate":           true,
	}
	for _, route := range openAPIRoutes {
		if !strings.HasPrefix(route.Path, "/api/v1/admin/") {
			continue
		}
		switch route.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			continue
		}
		key := route.Method + " " + route.Path
		role := routeOpenAPIAdminRole(route)
		if allowedOperatorOrPublic[key] {
			if role == "owner" {
				t.Fatalf("explicitly allowed admin mutation should not be owner-only by default: %s", key)
			}
			continue
		}
		if role != "owner" {
			t.Fatalf("admin mutation must be owner-only unless explicitly allowed: %s role=%q", key, role)
		}
	}
}
func TestOpenAPIAdminRoleAnnotationsCoverCriticalRoutes(t *testing.T) {
	doc := buildOpenAPISpec()
	paths, ok := doc["paths"].(map[string]map[string]any)
	if !ok {
		t.Fatalf("expected typed OpenAPI paths map")
	}
	checks := []struct {
		method string
		path   string
		role   string
	}{
		{http.MethodGet, "/api/v1/admin/runtime/status", "operator"},
		{http.MethodPost, "/api/v1/admin/runtime/gc", "owner"},
		{http.MethodGet, "/api/v1/admin/jobs/{jobId}", "operator"},
		{http.MethodPost, "/api/v1/admin/jobs/{jobId}/cancel", "owner"},
		{http.MethodPost, "/api/v1/admin/logs/{sourceId}/rotate", "owner"},
		{http.MethodPatch, "/api/v1/admin/service-config/draft", "owner"},
		{http.MethodDelete, "/api/v1/admin/service-config/draft", "owner"},
		{http.MethodPost, "/api/v1/admin/service-config/validate", "operator"},
		{http.MethodPost, "/api/v1/admin/service-config/export-plan", "owner"},
		{http.MethodPut, "/api/v1/admin/sandbox/config", "owner"},
		{http.MethodPost, "/api/v1/admin/sandbox/rollback", "owner"},
		{http.MethodPost, "/api/v1/admin/sandbox/switch", "owner"},
		{http.MethodPost, "/api/v1/admin/sandbox/detect", "operator"},
		{http.MethodPost, "/api/v1/admin/sandbox/diagnose", "operator"},
		{http.MethodPut, "/api/v1/admin/sandbox/profiles/{profileName}", "owner"},
		{http.MethodDelete, "/api/v1/admin/sandbox/profiles/{profileName}", "owner"},
		{http.MethodDelete, "/api/v1/admin/sandbox/reports/{reportId}", "owner"},
		{http.MethodPost, "/api/v1/admin/sandbox/install", "owner"},
		{http.MethodPost, "/api/v1/admin/tenants", "owner"},
		{http.MethodPatch, "/api/v1/admin/tenants/{tenantId}", "owner"},
		{http.MethodPost, "/api/v1/admin/tenants/{tenantId}/pause", "owner"},
		{http.MethodPost, "/api/v1/admin/tenants/{tenantId}/resume", "owner"},
		{http.MethodDelete, "/api/v1/admin/tenants/{tenantId}", "owner"},
		{http.MethodGet, "/api/v1/admin/users", "operator"},
		{http.MethodPost, "/api/v1/admin/tenants/{tenantId}/users", "owner"},
		{http.MethodPatch, "/api/v1/admin/tenants/{tenantId}/users/{userId}", "owner"},
		{http.MethodPost, "/api/v1/admin/tenants/{tenantId}/users/{userId}/pause", "owner"},
		{http.MethodPost, "/api/v1/admin/tenants/{tenantId}/users/{userId}/resume", "owner"},
		{http.MethodDelete, "/api/v1/admin/tenants/{tenantId}/users/{userId}", "owner"},
		{http.MethodPut, "/api/v1/admin/tenants/{tenantId}/users/{userId}/config", "owner"},
		{http.MethodPost, "/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials", "owner"},
		{http.MethodPatch, "/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}", "owner"},
		{http.MethodPost, "/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}/rotate-secret", "owner"},
		{http.MethodPost, "/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}/rotate-key", "owner"},
		{http.MethodDelete, "/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}", "owner"},
		{http.MethodGet, "/api/v1/admin/export", "operator"},
		{http.MethodPost, "/api/v1/admin/import", "owner"},
		{http.MethodPost, "/api/v1/admin/snapshots", "owner"},
		{http.MethodPost, "/api/v1/admin/snapshots/prune", "owner"},
		{http.MethodPost, "/api/v1/admin/snapshots/{snapshotId}/restore", "owner"},
		{http.MethodDelete, "/api/v1/admin/snapshots/{snapshotId}", "owner"},
		{http.MethodGet, "/api/v1/admin/public-knowledge-libraries", "operator"},
		{http.MethodPost, "/api/v1/admin/public-knowledge-libraries", "owner"},
		{http.MethodGet, "/api/v1/admin/public-knowledge-libraries/{libraryId}/sources", "operator"},
		{http.MethodDelete, "/api/v1/admin/public-knowledge-libraries/{libraryId}", "owner"},
		{http.MethodPost, "/api/v1/admin/public-knowledge-libraries/{libraryId}/import/text", "owner"},
		{http.MethodPost, "/api/v1/admin/public-knowledge-libraries/{libraryId}/import/file", "owner"},
		{http.MethodPost, "/api/v1/admin/public-knowledge-libraries/{libraryId}/import/urls", "owner"},
		{http.MethodDelete, "/api/v1/admin/tenants/{tenantId}/knowledge", "owner"},
		{http.MethodPut, "/api/v1/admin/knowledge-access/cross-tenant", "owner"},
		{http.MethodPut, "/api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}", "owner"},
		{http.MethodDelete, "/api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}", "owner"},
		{http.MethodPost, "/api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}/public-libraries/{libraryId}", "owner"},
		{http.MethodDelete, "/api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}/public-libraries/{libraryId}", "owner"},
		{http.MethodPut, "/api/v1/admin/skill-sources/global", "owner"},
		{http.MethodPut, "/api/v1/admin/skill-sources/tenant/{id}", "owner"},
		{http.MethodDelete, "/api/v1/admin/skill-sources/tenant/{id}", "owner"},
		{http.MethodPut, "/api/v1/admin/skill-sources/tenants/{tenantId}/users/{userId}", "owner"},
		{http.MethodDelete, "/api/v1/admin/skill-sources/tenants/{tenantId}/users/{userId}", "owner"},
	}
	for _, tc := range checks {
		pathItem, ok := paths[tc.path]
		if !ok {
			t.Fatalf("missing OpenAPI path %s", tc.path)
		}
		op, ok := pathItem[strings.ToLower(tc.method)].(map[string]any)
		if !ok {
			t.Fatalf("missing OpenAPI operation %s %s", tc.method, tc.path)
		}
		if role, _ := op["x-maclaw-admin-role"].(string); role != tc.role {
			t.Fatalf("%s %s role = %q, want %q", tc.method, tc.path, role, tc.role)
		}
		responses, _ := op["responses"].(map[string]any)
		_, hasForbidden := responses["403"]
		if tc.role == "owner" && !hasForbidden {
			t.Fatalf("%s %s owner route missing 403 response", tc.method, tc.path)
		}
		if tc.role != "owner" && hasForbidden {
			t.Fatalf("%s %s non-owner route should not document owner-only 403", tc.method, tc.path)
		}
	}
}
func TestStructuredRecordsCRUDAndFiltering(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	body := strings.NewReader(`{"title":"March payroll","tags":["payroll","finance"],"data":{"amount":12000,"currency":"CNY","department":"R&D","items":[{"name":"base","amount":10000}]}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/records/finance", body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create record status = %d body = %s", w.Code, w.Body.String())
	}
	var created agentservice.StructuredRecord
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created record: %v", err)
	}
	if created.Collection != "finance" || created.Data["department"] != "R&D" || len(created.Tags) != 2 {
		t.Fatalf("unexpected created record: %#v", created)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/records/finance?tag=payroll&q=12000", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list records status = %d body = %s", w.Code, w.Body.String())
	}
	var listed struct {
		Items []agentservice.StructuredRecord `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list records: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].ID != created.ID {
		t.Fatalf("unexpected filtered records: %#v", listed.Items)
	}

	patchBody := strings.NewReader(`{"data":{"amount":13000,"currency":"CNY","department":"Finance"}}`)
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/records/finance/"+created.ID, patchBody)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update record status = %d body = %s", w.Code, w.Body.String())
	}
	var updated agentservice.StructuredRecord
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated record: %v", err)
	}
	if updated.Data["amount"] != float64(13000) || !updated.UpdatedAt.After(updated.CreatedAt) && !updated.UpdatedAt.Equal(updated.CreatedAt) {
		t.Fatalf("unexpected updated record: %#v", updated)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/records/hr/"+created.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected cross-collection 404, got %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/records/finance/"+created.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete record status = %d body = %s", w.Code, w.Body.String())
	}
}

type blockingExecutor struct {
	started chan string
	release chan struct{}
}

func (e *blockingExecutor) Execute(ctx context.Context, req agentservice.ExecuteRequest) (*agentservice.ExecuteResult, error) {
	if e.started != nil {
		select {
		case e.started <- req.Message.ID:
		default:
		}
	}
	if e.release == nil {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-e.release:
		return &agentservice.ExecuteResult{Content: "released", OutputType: "text/plain"}, nil
	}
}

func (e *blockingExecutor) DescribeCapabilities(ctx context.Context, req agentservice.ExecuteRequest) (*agentservice.AgentCapabilities, error) {
	_ = ctx
	_ = req
	return &agentservice.AgentCapabilities{Executor: "blocking", SupportsSessions: true}, nil
}

func TestGetAdminAlerts(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	readyInst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Ready Instance"})
	if err != nil {
		t.Fatalf("CreateInstance ready: %v", err)
	}
	unreadyInst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Unready Instance"})
	if err != nil {
		t.Fatalf("CreateInstance unready: %v", err)
	}
	unreadyInst.Status = agentservice.InstanceStatusStopped
	unreadyInst.Ready = false
	unreadyInst.ReadyReason = "config validation failed path=" + svc.DataRoot()
	unreadyInst.Readiness.Reason = "missing runtime path=" + svc.DataRoot()
	unreadyInst.UpdatedAt = time.Now().UTC()
	if err := store.SaveInstance(*unreadyInst); err != nil {
		t.Fatalf("SaveInstance unready: %v", err)
	}
	waitingRun := agentservice.Run{
		ID:             "run_wait",
		TenantID:       tenant.ID,
		UserID:         user.ID,
		InstanceID:     readyInst.ID,
		SessionID:      "sess_wait",
		Status:         agentservice.RunStatusSucceeded,
		ResponseSource: "ask_user",
		WaitingForUser: true,
		StartedAt:      time.Now().UTC().Add(-time.Minute),
	}
	failedRun := agentservice.Run{
		ID:         "run_fail",
		TenantID:   tenant.ID,
		UserID:     user.ID,
		InstanceID: readyInst.ID,
		SessionID:  "sess_fail",
		Status:     agentservice.RunStatusFailed,
		Error:      "boom",
		StartedAt:  time.Now().UTC().Add(-2 * time.Minute),
	}
	if err := store.SaveRun(waitingRun); err != nil {
		t.Fatalf("SaveRun waiting: %v", err)
	}
	if err := store.SaveRun(failedRun); err != nil {
		t.Fatalf("SaveRun failed: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/alerts?kind=failed_run&limit=1", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("alerts status = %d body = %s", w.Code, w.Body.String())
	}

	var alerts agentservice.AdminAlerts
	if err := json.NewDecoder(w.Body).Decode(&alerts); err != nil {
		t.Fatalf("decode alerts: %v", err)
	}
	if len(alerts.Items) != 1 {
		t.Fatalf("expected exactly one normalized alert item, got %#v", alerts.Items)
	}
	if alerts.Items[0].Kind != "failed_run" || alerts.Items[0].RunID != failedRun.ID {
		t.Fatalf("unexpected normalized alert item: %#v", alerts.Items[0])
	}
	if len(alerts.UnreadyInstances) != 0 {
		t.Fatalf("expected unready instances filtered out, got %#v", alerts.UnreadyInstances)
	}
	if len(alerts.WaitingRuns) != 0 {
		t.Fatalf("expected waiting runs filtered out, got %#v", alerts.WaitingRuns)
	}
	if len(alerts.FailedRuns) != 1 || alerts.FailedRuns[0].ID != failedRun.ID {
		t.Fatalf("expected failed run retained in legacy list, got %#v", alerts.FailedRuns)
	}
	if alerts.GeneratedAt.IsZero() {
		t.Fatalf("expected generated_at: %#v", alerts)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/alerts?kind=unready_instance", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("unready alerts status = %d body = %s", w.Code, w.Body.String())
	}
	alerts = agentservice.AdminAlerts{}
	if err := json.NewDecoder(w.Body).Decode(&alerts); err != nil {
		t.Fatalf("decode unready alerts: %v", err)
	}
	if len(alerts.UnreadyInstances) != 1 {
		t.Fatalf("expected one unready instance, got %#v", alerts.UnreadyInstances)
	}
	instJSON, err := json.Marshal(alerts.UnreadyInstances[0])
	if err != nil {
		t.Fatalf("marshal unready instance: %v", err)
	}
	if strings.Contains(string(instJSON), svc.DataRoot()) || alerts.UnreadyInstances[0].DataDir != "" || alerts.UnreadyInstances[0].RuntimeDir != "" || alerts.UnreadyInstances[0].Workspace != "" {
		t.Fatalf("expected admin alert instance paths to be redacted, got %s", instJSON)
	}
}

func TestGetAdminDashboard(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Hour)
	sess := agentservice.Session{ID: "sess_dash", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, AgentID: "default", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if err := store.SaveMessage(agentservice.Message{ID: "msg_dash_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleUser, Content: "hello", CreatedAt: now.Add(-2 * time.Hour)}); err != nil {
		t.Fatalf("SaveMessage recent: %v", err)
	}
	if err := store.SaveMessage(agentservice.Message{ID: "msg_dash_2", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleUser, Content: "older", CreatedAt: now.Add(-48 * time.Hour)}); err != nil {
		t.Fatalf("SaveMessage older: %v", err)
	}
	completed := now.Add(-90 * time.Minute)
	if err := store.SaveRun(agentservice.Run{ID: "run_dash_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Status: agentservice.RunStatusSucceeded, StartedAt: now.Add(-90 * time.Minute), CompletedAt: &completed}); err != nil {
		t.Fatalf("SaveRun recent: %v", err)
	}
	if err := store.SaveAuditEvent(agentservice.AuditEvent{ID: "audit_dash_1", TenantID: tenant.ID, UserID: user.ID, ActorType: "admin", Action: "dashboard.opened", ResourceType: "system", ResourceID: "dashboard", CreatedAt: now.Add(-30 * time.Minute)}); err != nil {
		t.Fatalf("SaveAuditEvent: %v", err)
	}
	if err := store.SaveAuditEvent(agentservice.AuditEvent{ID: "audit_dash_secret", TenantID: tenant.ID, UserID: user.ID, ActorType: "admin", Action: "dashboard.secret", ResourceType: "system", ResourceID: svc.DataRoot(), Metadata: map[string]string{"token": "dashboard-secret", "path": svc.DataRoot()}, CreatedAt: now.Add(-20 * time.Minute)}); err != nil {
		t.Fatalf("SaveAuditEvent secret: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/dashboard", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d body = %s", w.Code, w.Body.String())
	}
	var dashboard agentservice.AdminDashboard
	if err := json.NewDecoder(w.Body).Decode(&dashboard); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	if dashboard.Overview.Tenants != 1 || dashboard.Overview.Users != 1 {
		t.Fatalf("unexpected overview: %#v", dashboard.Overview)
	}
	foundDashboardAudit := false
	for _, event := range dashboard.RecentAuditEvents {
		if event.Action == "dashboard.opened" {
			foundDashboardAudit = true
			break
		}
	}
	if len(dashboard.RecentAuditEvents) == 0 || !foundDashboardAudit {
		t.Fatalf("unexpected recent audits: %#v", dashboard.RecentAuditEvents)
	}
	dashboardAuditJSON, err := json.Marshal(dashboard.RecentAuditEvents)
	if err != nil {
		t.Fatalf("marshal dashboard audits: %v", err)
	}
	if strings.Contains(string(dashboardAuditJSON), "dashboard-secret") || strings.Contains(string(dashboardAuditJSON), svc.DataRoot()) {
		t.Fatalf("dashboard recent audit events should be redacted, got %s", dashboardAuditJSON)
	}
	if len(dashboard.Last24Hours) != 24 || len(dashboard.Last7Days) != 7 {
		t.Fatalf("unexpected trend lengths: %#v", dashboard)
	}
	recentHourHasMessage := false
	recentDayHasMessage := false
	for _, point := range dashboard.Last24Hours {
		if point.Messages > 0 || point.Runs > 0 || point.AuditEvents > 0 {
			recentHourHasMessage = true
			break
		}
	}
	for _, point := range dashboard.Last7Days {
		if point.Messages > 0 || point.Runs > 0 || point.AuditEvents > 0 {
			recentDayHasMessage = true
			break
		}
	}
	if !recentHourHasMessage || !recentDayHasMessage || dashboard.GeneratedAt.IsZero() {
		t.Fatalf("unexpected dashboard trend payload: %#v", dashboard)
	}
}

func TestGetAdminAlertsIncludesCredentialExpiry(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	soon := time.Now().UTC().Add(48 * time.Hour)
	cred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "Soon", APIKey: "soon-key", APISecret: "soon-secret"})
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	if _, err := svc.UpdateCredential(context.Background(), tenant.ID, user.ID, cred.ID, agentservice.UpdateCredentialInput{ExpiresAt: &soon}); err != nil {
		t.Fatalf("UpdateCredential expires_at: %v", err)
	}
	far := time.Now().UTC().Add(30 * 24 * time.Hour)
	farCred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "Far", APIKey: "far-key", APISecret: "far-secret"})
	if err != nil {
		t.Fatalf("CreateCredential far: %v", err)
	}
	if _, err := svc.UpdateCredential(context.Background(), tenant.ID, user.ID, farCred.ID, agentservice.UpdateCredentialInput{ExpiresAt: &far}); err != nil {
		t.Fatalf("UpdateCredential far expires_at: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/alerts?kind=credential_expiring&credential_expiry_window_days=3", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("credential expiry alerts status = %d body = %s", w.Code, w.Body.String())
	}
	var alerts agentservice.AdminAlerts
	if err := json.NewDecoder(w.Body).Decode(&alerts); err != nil {
		t.Fatalf("decode credential expiry alerts: %v", err)
	}
	if len(alerts.Items) != 1 || alerts.Items[0].Kind != "credential_expiring" || alerts.Items[0].CredentialID != cred.ID {
		t.Fatalf("unexpected credential expiry alert items: %#v", alerts.Items)
	}
	if len(alerts.CredentialAlerts) != 1 || alerts.CredentialAlerts[0].ID != cred.ID {
		t.Fatalf("unexpected credential alert list: %#v", alerts.CredentialAlerts)
	}
	if alerts.CredentialAlerts[0].APISecret != "" || alerts.CredentialAlerts[0].SecretDigest != "" || alerts.CredentialAlerts[0].APIKeyHash != "" {
		t.Fatalf("credential alert should be sanitized: %#v", alerts.CredentialAlerts[0])
	}
}

func TestGetAdminInsights(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	baseNow := time.Now().UTC()
	tenantHot, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Hot Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant hot: %v", err)
	}
	tenantCold, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Cold Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant cold: %v", err)
	}
	maxInstances := 5
	if _, err := svc.UpdateTenant(context.Background(), tenantHot.ID, agentservice.UpdateTenantInput{MaxInstances: &maxInstances}); err != nil {
		t.Fatalf("UpdateTenant quota: %v", err)
	}
	userHot, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenantHot.ID, Name: "Busy User", Email: "busy@example.com"})
	if err != nil {
		t.Fatalf("CreateUser hot: %v", err)
	}
	userCold, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenantCold.ID, Name: "Dormant User", Email: "dormant@example.com"})
	if err != nil {
		t.Fatalf("CreateUser cold: %v", err)
	}
	maxMessages := 5
	if _, err := svc.UpdateUser(context.Background(), tenantHot.ID, userHot.ID, agentservice.UpdateUserInput{MaxMessages: &maxMessages}); err != nil {
		t.Fatalf("UpdateUser quota: %v", err)
	}
	hotPrincipal := agentservice.Principal{TenantID: tenantHot.ID, UserID: userHot.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), hotPrincipal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig hot: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), hotPrincipal, agentservice.CreateInstanceInput{Name: "Busy Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess := agentservice.Session{ID: "sess_insights", TenantID: tenantHot.ID, UserID: userHot.ID, InstanceID: inst.ID, AgentID: "default", CreatedAt: baseNow.Add(-2 * time.Hour), UpdatedAt: baseNow.Add(-time.Hour)}
	if err := store.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	for i := 0; i < 4; i++ {
		msgTime := baseNow.Add(-time.Duration(50-i) * time.Minute)
		if err := store.SaveMessage(agentservice.Message{ID: fmt.Sprintf("msg_insights_%d", i), TenantID: tenantHot.ID, UserID: userHot.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleUser, Content: "hello", CreatedAt: msgTime}); err != nil {
			t.Fatalf("SaveMessage %d: %v", i, err)
		}
	}
	completed := baseNow.Add(-20 * time.Minute)
	if err := store.SaveRun(agentservice.Run{ID: "run_insights_hot", TenantID: tenantHot.ID, UserID: userHot.ID, InstanceID: inst.ID, SessionID: sess.ID, Status: agentservice.RunStatusSucceeded, StartedAt: baseNow.Add(-25 * time.Minute), CompletedAt: &completed}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if err := store.SaveAuditEvent(agentservice.AuditEvent{ID: "audit_insights", TenantID: tenantHot.ID, UserID: userHot.ID, ActorType: "admin", Action: "insights.viewed", ResourceType: "system", ResourceID: "insights", CreatedAt: baseNow.Add(-10 * time.Minute)}); err != nil {
		t.Fatalf("SaveAuditEvent: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/insights?inactive_for_days=30&limit=5", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("insights status = %d body = %s", w.Code, w.Body.String())
	}
	var out agentservice.AdminInsights
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode insights: %v", err)
	}
	if out.GeneratedAt.IsZero() || out.InactiveCutoff.IsZero() {
		t.Fatalf("expected timestamps in insights: %#v", out)
	}
	if len(out.TopTenants) == 0 || out.TopTenants[0].TenantID != tenantHot.ID {
		t.Fatalf("expected hot tenant to rank first: %#v", out.TopTenants)
	}
	foundInactive := false
	for _, item := range out.InactiveUsers {
		if item.UserID == userCold.ID {
			foundInactive = true
			if item.Reason == "" {
				t.Fatalf("expected inactive reason: %#v", item)
			}
		}
	}
	if !foundInactive {
		t.Fatalf("expected dormant user in inactive list: %#v", out.InactiveUsers)
	}
	foundPressure := false
	for _, item := range out.QuotaPressure {
		if item.Scope == "user" && item.UserID == userHot.ID && item.Metric == "messages" {
			foundPressure = true
			if item.PressureRatio < 0.8 {
				t.Fatalf("expected pressure ratio >= 0.8: %#v", item)
			}
		}
	}
	if !foundPressure {
		t.Fatalf("expected quota pressure item: %#v", out.QuotaPressure)
	}
}

func TestGetAdminInsightsRejectsInvalidQuery(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/insights?inactive_for_days=soon", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body = %s", w.Code, w.Body.String())
	}
}

func TestGetAdminOverview(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	now := time.Now().UTC()
	sess := agentservice.Session{ID: "sess_overview", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, AgentID: "default", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if err := store.SaveMessage(agentservice.Message{ID: "msg_overview_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleUser, Content: "hello", CreatedAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("SaveMessage user: %v", err)
	}
	completed := now.Add(2 * time.Minute)
	if err := store.SaveRun(agentservice.Run{ID: "run_overview_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Status: agentservice.RunStatusSucceeded, StartedAt: now.Add(time.Minute), CompletedAt: &completed}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if err := store.SaveAuditEvent(agentservice.AuditEvent{ID: "audit_custom", TenantID: tenant.ID, UserID: user.ID, ActorType: "admin", Action: "overview.checked", ResourceType: "system", ResourceID: "overview", CreatedAt: now.Add(3 * time.Minute)}); err != nil {
		t.Fatalf("SaveAuditEvent: %v", err)
	}
	activeCred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "Active", APIKey: "overview-active", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential active: %v", err)
	}
	expiringAt := now.Add(48 * time.Hour)
	if _, err := svc.UpdateCredential(context.Background(), tenant.ID, user.ID, activeCred.ID, agentservice.UpdateCredentialInput{ExpiresAt: &expiringAt}); err != nil {
		t.Fatalf("UpdateCredential active expires_at: %v", err)
	}
	suspendedStatus := agentservice.CredentialStatusSuspended
	suspendedCred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "Suspended", APIKey: "overview-suspended", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential suspended: %v", err)
	}
	if _, err := svc.UpdateCredential(context.Background(), tenant.ID, user.ID, suspendedCred.ID, agentservice.UpdateCredentialInput{Status: &suspendedStatus}); err != nil {
		t.Fatalf("UpdateCredential suspended: %v", err)
	}
	revokedCred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "Revoked", APIKey: "overview-revoked", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential revoked: %v", err)
	}
	if _, err := svc.RevokeCredential(context.Background(), tenant.ID, user.ID, revokedCred.ID); err != nil {
		t.Fatalf("RevokeCredential: %v", err)
	}
	expiredCred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "Expired", APIKey: "overview-expired", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential expired: %v", err)
	}
	expiredAt := now.Add(-time.Hour)
	if _, err := svc.UpdateCredential(context.Background(), tenant.ID, user.ID, expiredCred.ID, agentservice.UpdateCredentialInput{ExpiresAt: &expiredAt}); err != nil {
		t.Fatalf("UpdateCredential expired expires_at: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/overview", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("overview status = %d body = %s", w.Code, w.Body.String())
	}
	var overview agentservice.AdminOverview
	if err := json.NewDecoder(w.Body).Decode(&overview); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	if overview.Tenants != 1 || overview.ActiveTenants != 1 || overview.Users != 1 || overview.ActiveUsers != 1 {
		t.Fatalf("unexpected tenant/user counts: %#v", overview)
	}
	if overview.Instances != 1 || overview.ReadyInstances != 1 || overview.Sessions != 1 || overview.Messages != 1 || overview.Runs != 1 {
		t.Fatalf("unexpected activity counts: %#v", overview)
	}
	if overview.RunsByStatus[agentservice.RunStatusSucceeded] != 1 || overview.AuditEvents == 0 {
		t.Fatalf("unexpected run/audit counts: %#v", overview)
	}
	if overview.Credentials != 4 || overview.ActiveCredentials != 2 || overview.SuspendedCredentials != 1 || overview.RevokedCredentials != 1 {
		t.Fatalf("unexpected credential status counts: %#v", overview)
	}
	if overview.ExpiringCredentials != 1 || overview.ExpiredCredentials != 1 {
		t.Fatalf("unexpected credential expiry counts: %#v", overview)
	}
	if overview.LastActivityAt == nil || overview.LastAuditAt == nil {
		t.Fatalf("expected last activity and last audit timestamps: %#v", overview)
	}
}

func TestTenantDeleteCheckReportsCountsAndBlockers(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, agentservice.CreateSessionInput{Title: "Demo"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "check-key", APISecret: "check-secret"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	now := time.Now().UTC()
	if err := store.SaveMessage(agentservice.Message{ID: "msg_delete_check", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleUser, Content: "hello", CreatedAt: now}); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	if err := store.SaveRun(agentservice.Run{ID: "run_delete_check", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Status: agentservice.RunStatusRunning, StartedAt: now}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+tenant.ID+"/delete-check", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tenant delete-check status = %d body = %s", w.Code, w.Body.String())
	}
	var out agentservice.TenantDeleteCheck
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode tenant delete-check: %v", err)
	}
	if out.CanDelete {
		t.Fatalf("expected tenant delete-check to be blocked: %#v", out)
	}
	if out.Users != 1 || out.Credentials != 1 || out.Instances != 1 || out.Sessions != 1 || out.Messages != 1 || out.Runs != 1 {
		t.Fatalf("unexpected tenant delete counts: %#v", out)
	}
	if len(out.Blockers) != 1 || out.Blockers[0].RunID != "run_delete_check" {
		t.Fatalf("unexpected tenant blockers: %#v", out.Blockers)
	}
}

func TestUserDeleteCheckAllowsIdleUser(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, agentservice.CreateSessionInput{Title: "Demo"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "idle-check-key", APISecret: "idle-check-secret"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	now := time.Now().UTC()
	if err := store.SaveMessage(agentservice.Message{ID: "msg_user_delete_check", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleUser, Content: "hello", CreatedAt: now}); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	completed := now.Add(time.Minute)
	if err := store.SaveRun(agentservice.Run{ID: "run_user_delete_check", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Status: agentservice.RunStatusSucceeded, StartedAt: now, CompletedAt: &completed}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/delete-check", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("user delete-check status = %d body = %s", w.Code, w.Body.String())
	}
	var out agentservice.UserDeleteCheck
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode user delete-check: %v", err)
	}
	if !out.CanDelete || len(out.Blockers) != 0 {
		t.Fatalf("expected user delete-check to allow deletion: %#v", out)
	}
	if out.Credentials != 1 || out.Instances != 1 || out.Sessions != 1 || out.Messages != 1 || out.Runs != 1 {
		t.Fatalf("unexpected user delete counts: %#v", out)
	}
}

func TestTenantRetirePlanReturnsDeleteCheckAndScopedExport(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, agentservice.CreateSessionInput{Title: "Demo"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	now := time.Now().UTC()
	if err := store.SaveMessage(agentservice.Message{ID: "msg_retire_plan", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleUser, Content: "hello", CreatedAt: now}); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	completed := now.Add(time.Minute)
	if err := store.SaveRun(agentservice.Run{ID: "run_retire_plan", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Status: agentservice.RunStatusSucceeded, StartedAt: now, CompletedAt: &completed}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+tenant.ID+"/retire-plan?include_audit=false", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tenant retire-plan status = %d body = %s", w.Code, w.Body.String())
	}
	var out agentservice.TenantRetirePlan
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode tenant retire-plan: %v", err)
	}
	if out.DeleteCheck.TenantID != tenant.ID || out.Export.Scope != "tenant" || out.Export.TenantID != tenant.ID {
		t.Fatalf("unexpected tenant retire-plan payload: %#v", out)
	}
	if out.Export.IncludeAudit {
		t.Fatalf("expected include_audit=false to be respected: %#v", out.Export)
	}
	if len(out.Export.Users) != 1 || out.Export.Users[0].User.ID != user.ID {
		t.Fatalf("unexpected tenant retire export users: %#v", out.Export.Users)
	}
}

func TestUserRetirePlanReturnsScopedExport(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user1, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User1"})
	if err != nil {
		t.Fatalf("CreateUser1: %v", err)
	}
	user2, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User2"})
	if err != nil {
		t.Fatalf("CreateUser2: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user1.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, agentservice.CreateSessionInput{Title: "Demo"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	now := time.Now().UTC()
	if err := store.SaveMessage(agentservice.Message{ID: "msg_user_retire_plan", TenantID: tenant.ID, UserID: user1.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleUser, Content: "hello", CreatedAt: now}); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user1.ID+"/retire-plan?include_messages=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("user retire-plan status = %d body = %s", w.Code, w.Body.String())
	}
	var out agentservice.UserRetirePlan
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode user retire-plan: %v", err)
	}
	if out.DeleteCheck.UserID != user1.ID || out.Export.Scope != "user" || out.Export.UserID != user1.ID {
		t.Fatalf("unexpected user retire-plan payload: %#v", out)
	}
	if len(out.Export.Users) != 1 || out.Export.Users[0].User.ID != user1.ID {
		t.Fatalf("expected only target user in export: %#v", out.Export.Users)
	}
	if len(out.Export.Users) == 1 && out.Export.Users[0].User.ID == user2.ID {
		t.Fatalf("unexpected second user in retire export: %#v", out.Export.Users)
	}
}

func TestDeleteTenantRequiresExplicitConfirmation(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/tenants/"+tenant.ID, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.GetTenant(context.Background(), tenant.ID); err != nil {
		t.Fatalf("tenant should still exist: %v", err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/tenants/"+tenant.ID+"?confirm=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 after confirm, got %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.GetTenant(context.Background(), tenant.ID); err == nil {
		t.Fatalf("expected tenant deleted after confirm")
	}
}

func TestDeleteUserRequiresExplicitConfirmation(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.GetUser(context.Background(), tenant.ID, user.ID); err != nil {
		t.Fatalf("user should still exist: %v", err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"?confirm=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 after confirm, got %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.GetUser(context.Background(), tenant.ID, user.ID); err == nil {
		t.Fatalf("expected user deleted after confirm")
	}
}

func TestProtectedTenantDeleteCheckAndDeleteBlocked(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant", DeleteProtected: true, DeleteProtectionReason: "managed by platform"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+tenant.ID+"/delete-check", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tenant protected delete-check status = %d body = %s", w.Code, w.Body.String())
	}
	var out agentservice.TenantDeleteCheck
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode tenant protected delete-check: %v", err)
	}
	if out.CanDelete || !out.DeleteProtected || out.DeleteProtectionReason != "managed by platform" {
		t.Fatalf("unexpected protected tenant delete-check: %#v", out)
	}
	if len(out.Blockers) == 0 || out.Blockers[0].Kind != "delete_protected" {
		t.Fatalf("expected delete_protected blocker: %#v", out.Blockers)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/tenants/"+tenant.ID+"?confirm=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for protected tenant delete, got %d body = %s", w.Code, w.Body.String())
	}
}

func TestProtectedUserBlocksUserAndTenantDelete(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User", DeleteProtected: true, DeleteProtectionReason: "billing owner"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/delete-check", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("protected user delete-check status = %d body = %s", w.Code, w.Body.String())
	}
	var userCheck agentservice.UserDeleteCheck
	if err := json.NewDecoder(w.Body).Decode(&userCheck); err != nil {
		t.Fatalf("decode protected user delete-check: %v", err)
	}
	if userCheck.CanDelete || !userCheck.DeleteProtected || userCheck.DeleteProtectionReason != "billing owner" {
		t.Fatalf("unexpected protected user delete-check: %#v", userCheck)
	}
	if len(userCheck.Blockers) == 0 || userCheck.Blockers[0].Kind != "delete_protected" {
		t.Fatalf("expected delete_protected user blocker: %#v", userCheck.Blockers)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+tenant.ID+"/delete-check", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tenant delete-check with protected user status = %d body = %s", w.Code, w.Body.String())
	}
	var tenantCheck agentservice.TenantDeleteCheck
	if err := json.NewDecoder(w.Body).Decode(&tenantCheck); err != nil {
		t.Fatalf("decode tenant delete-check with protected user: %v", err)
	}
	if tenantCheck.CanDelete {
		t.Fatalf("expected tenant delete-check to be blocked by protected user: %#v", tenantCheck)
	}
	found := false
	for _, blocker := range tenantCheck.Blockers {
		if blocker.Kind == "delete_protected" && blocker.UserID == user.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected protected user blocker in tenant delete-check: %#v", tenantCheck.Blockers)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"?confirm=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for protected user delete, got %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/tenants/"+tenant.ID+"?confirm=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for tenant delete blocked by protected user, got %d body = %s", w.Code, w.Body.String())
	}
}

func TestForceDeleteProtectedUserRequiresAdminSecretConfirmation(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Legacy User", DeleteProtected: true, DeleteProtectionReason: "legacy platform binding"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"?confirm=true&force=true", strings.NewReader(`{"admin_secret":"wrong"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong force secret, got %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.GetUser(context.Background(), tenant.ID, user.ID); err != nil {
		t.Fatalf("user should still exist after wrong force secret: %v", err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"?confirm=true&force=true", strings.NewReader(`{"admin_secret":"admin-secret"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for force delete, got %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.GetUser(context.Background(), tenant.ID, user.ID); err == nil {
		t.Fatalf("expected protected user deleted after force confirmation")
	}
}

func TestForceDeleteProtectedTenantDeletesProtectedUsers(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Protected Tenant", DeleteProtected: true, DeleteProtectionReason: "legacy platform tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Protected User", DeleteProtected: true, DeleteProtectionReason: "legacy platform user"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/tenants/"+tenant.ID+"?confirm=true&force=true", strings.NewReader(`{"admin_secret":"wrong"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong tenant force secret, got %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.GetTenant(context.Background(), tenant.ID); err != nil {
		t.Fatalf("tenant should still exist after wrong force secret: %v", err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/tenants/"+tenant.ID+"?confirm=true&force=true", strings.NewReader(`{"admin_secret":"admin-secret"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for force tenant delete, got %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.GetUser(context.Background(), tenant.ID, user.ID); err == nil {
		t.Fatalf("expected protected user deleted with tenant")
	}
	if _, err := svc.GetTenant(context.Background(), tenant.ID); err == nil {
		t.Fatalf("expected protected tenant deleted after force confirmation")
	}
}

func TestAdminCanListTenantsAndUsers(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest("GET", "/api/v1/admin/tenants", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list tenants status = %d body = %s", w.Code, w.Body.String())
	}
	var tenants struct {
		Items      []agentservice.Tenant `json:"items"`
		Limit      int                   `json:"limit"`
		HasMore    bool                  `json:"has_more"`
		NextBefore string                `json:"next_before"`
	}
	if err := json.NewDecoder(w.Body).Decode(&tenants); err != nil {
		t.Fatalf("decode tenants: %v", err)
	}
	if len(tenants.Items) != 1 || tenants.Items[0].ID != tenant.ID {
		t.Fatalf("tenants = %#v", tenants.Items)
	}
	if tenants.Limit != defaultPageLimit || tenants.HasMore || tenants.NextBefore != "" {
		t.Fatalf("unexpected tenant page meta: %#v", tenants)
	}

	req = httptest.NewRequest("GET", "/api/v1/admin/tenants/"+tenant.ID+"/users", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list users status = %d body = %s", w.Code, w.Body.String())
	}
	var users struct {
		Items      []agentservice.User `json:"items"`
		Limit      int                 `json:"limit"`
		HasMore    bool                `json:"has_more"`
		NextBefore string              `json:"next_before"`
	}
	if err := json.NewDecoder(w.Body).Decode(&users); err != nil {
		t.Fatalf("decode users: %v", err)
	}
	if len(users.Items) != 1 || users.Items[0].ID != user.ID {
		t.Fatalf("users = %#v", users.Items)
	}
	if users.Limit != defaultPageLimit || users.HasMore || users.NextBefore != "" {
		t.Fatalf("unexpected user page meta: %#v", users)
	}
}

func TestAdminListTenantsSupportsFilters(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	alpha, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Alpha Team"})
	if err != nil {
		t.Fatalf("CreateTenant alpha: %v", err)
	}
	beta, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Beta Team"})
	if err != nil {
		t.Fatalf("CreateTenant beta: %v", err)
	}
	disabled := agentservice.TenantStatusDisabled
	if _, err := svc.UpdateTenant(context.Background(), beta.ID, agentservice.UpdateTenantInput{Status: &disabled}); err != nil {
		t.Fatalf("UpdateTenant beta: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants?status=disabled", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list tenants by status = %d body = %s", w.Code, w.Body.String())
	}
	var byStatus struct {
		Items []agentservice.Tenant `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&byStatus); err != nil {
		t.Fatalf("decode byStatus: %v", err)
	}
	if len(byStatus.Items) != 1 || byStatus.Items[0].ID != beta.ID {
		t.Fatalf("unexpected tenants by status: %#v", byStatus.Items)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants?name=alpha", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list tenants by name = %d body = %s", w.Code, w.Body.String())
	}
	var byName struct {
		Items []agentservice.Tenant `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&byName); err != nil {
		t.Fatalf("decode byName: %v", err)
	}
	if len(byName.Items) != 1 || byName.Items[0].ID != alpha.ID {
		t.Fatalf("unexpected tenants by name: %#v", byName.Items)
	}
}

func TestAdminListAllUsersAcrossTenantsSupportsFilters(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenantA, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant A"})
	if err != nil {
		t.Fatalf("CreateTenant A: %v", err)
	}
	tenantB, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant B"})
	if err != nil {
		t.Fatalf("CreateTenant B: %v", err)
	}
	userA, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenantA.ID, Name: "Alpha User", Email: "alpha@example.com"})
	if err != nil {
		t.Fatalf("CreateUser A: %v", err)
	}
	userB, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenantB.ID, Name: "Beta User", Email: "beta@example.com"})
	if err != nil {
		t.Fatalf("CreateUser B: %v", err)
	}
	disabled := agentservice.UserStatusDisabled
	if _, err := svc.UpdateUser(context.Background(), tenantB.ID, userB.ID, agentservice.UpdateUserInput{Status: &disabled}); err != nil {
		t.Fatalf("UpdateUser B: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?name=alpha", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list all users by name status = %d body = %s", w.Code, w.Body.String())
	}
	var byName struct {
		Items []agentservice.User `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&byName); err != nil {
		t.Fatalf("decode byName: %v", err)
	}
	if len(byName.Items) != 1 || byName.Items[0].ID != userA.ID {
		t.Fatalf("unexpected all-users name filter: %#v", byName.Items)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?status=disabled", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list all users by status = %d body = %s", w.Code, w.Body.String())
	}
	var byStatus struct {
		Items []agentservice.User `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&byStatus); err != nil {
		t.Fatalf("decode byStatus: %v", err)
	}
	if len(byStatus.Items) != 1 || byStatus.Items[0].ID != userB.ID {
		t.Fatalf("unexpected all-users status filter: %#v", byStatus.Items)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?tenant_id="+tenantA.ID, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list all users by tenant = %d body = %s", w.Code, w.Body.String())
	}
	var byTenant struct {
		Items []agentservice.User `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&byTenant); err != nil {
		t.Fatalf("decode byTenant: %v", err)
	}
	if len(byTenant.Items) != 1 || byTenant.Items[0].TenantID != tenantA.ID {
		t.Fatalf("unexpected all-users tenant filter: %#v", byTenant.Items)
	}
}

func TestAdminListAllUsersRejectsInvalidStatus(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?status=paused", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminListUsersSupportsFilters(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	alpha, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Alpha User", Email: "alpha@example.com"})
	if err != nil {
		t.Fatalf("CreateUser alpha: %v", err)
	}
	beta, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Beta User", Email: "beta@example.com"})
	if err != nil {
		t.Fatalf("CreateUser beta: %v", err)
	}
	disabled := agentservice.UserStatusDisabled
	if _, err := svc.UpdateUser(context.Background(), tenant.ID, beta.ID, agentservice.UpdateUserInput{Status: &disabled}); err != nil {
		t.Fatalf("UpdateUser beta: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+tenant.ID+"/users?status=disabled", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list users by status = %d body = %s", w.Code, w.Body.String())
	}
	var byStatus struct {
		Items []agentservice.User `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&byStatus); err != nil {
		t.Fatalf("decode byStatus: %v", err)
	}
	if len(byStatus.Items) != 1 || byStatus.Items[0].ID != beta.ID {
		t.Fatalf("unexpected users by status: %#v", byStatus.Items)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+tenant.ID+"/users?name=alpha", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list users by name = %d body = %s", w.Code, w.Body.String())
	}
	var byName struct {
		Items []agentservice.User `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&byName); err != nil {
		t.Fatalf("decode byName: %v", err)
	}
	if len(byName.Items) != 1 || byName.Items[0].ID != alpha.ID {
		t.Fatalf("unexpected users by name: %#v", byName.Items)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+tenant.ID+"/users?email=alpha@example.com", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list users by email = %d body = %s", w.Code, w.Body.String())
	}
	var byEmail struct {
		Items []agentservice.User `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&byEmail); err != nil {
		t.Fatalf("decode byEmail: %v", err)
	}
	if len(byEmail.Items) != 1 || byEmail.Items[0].ID != alpha.ID {
		t.Fatalf("unexpected users by email: %#v", byEmail.Items)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	readyInst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Ready Instance"})
	if err != nil {
		t.Fatalf("CreateInstance ready: %v", err)
	}
	unreadyUser, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Unready User"})
	if err != nil {
		t.Fatalf("CreateUser unready: %v", err)
	}
	now := time.Now().UTC()
	metricExpiringCred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "Metric Expiring", APIKey: "metric-expiring", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential metric expiring: %v", err)
	}
	metricExpiringAt := now.Add(48 * time.Hour)
	if _, err := svc.UpdateCredential(context.Background(), tenant.ID, user.ID, metricExpiringCred.ID, agentservice.UpdateCredentialInput{ExpiresAt: &metricExpiringAt}); err != nil {
		t.Fatalf("UpdateCredential metric expiring: %v", err)
	}
	metricExpiredCred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "Metric Expired", APIKey: "metric-expired", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential metric expired: %v", err)
	}
	metricExpiredAt := now.Add(-time.Hour)
	if _, err := svc.UpdateCredential(context.Background(), tenant.ID, user.ID, metricExpiredCred.ID, agentservice.UpdateCredentialInput{ExpiresAt: &metricExpiredAt}); err != nil {
		t.Fatalf("UpdateCredential metric expired: %v", err)
	}
	metricSuspendedStatus := agentservice.CredentialStatusSuspended
	metricSuspendedCred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "Metric Suspended", APIKey: "metric-suspended", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential metric suspended: %v", err)
	}
	if _, err := svc.UpdateCredential(context.Background(), tenant.ID, user.ID, metricSuspendedCred.ID, agentservice.UpdateCredentialInput{Status: &metricSuspendedStatus}); err != nil {
		t.Fatalf("UpdateCredential metric suspended: %v", err)
	}
	metricRevokedCred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "Metric Revoked", APIKey: "metric-revoked", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential metric revoked: %v", err)
	}
	if _, err := svc.RevokeCredential(context.Background(), tenant.ID, user.ID, metricRevokedCred.ID); err != nil {
		t.Fatalf("RevokeCredential metric revoked: %v", err)
	}
	unreadyInst := agentservice.Instance{ID: "inst_unready_metric", TenantID: tenant.ID, UserID: unreadyUser.ID, Name: "Unready Instance", Status: agentservice.InstanceStatusReady, RuntimeDir: filepath.Join(t.TempDir(), "missing-runtime"), DataDir: filepath.Join(t.TempDir(), "missing-data"), Workspace: filepath.Join(t.TempDir(), "missing-workspace"), CreatedAt: now, UpdatedAt: now}
	if err := store.SaveInstance(unreadyInst); err != nil {
		t.Fatalf("SaveInstance unready: %v", err)
	}
	if err := store.SaveRun(agentservice.Run{ID: "run_waiting_metric", TenantID: tenant.ID, UserID: user.ID, InstanceID: readyInst.ID, SessionID: "sess_waiting_metric", Status: agentservice.RunStatusSucceeded, ResponseSource: "ask_user", WaitingForUser: true, StartedAt: now}); err != nil {
		t.Fatalf("SaveRun waiting: %v", err)
	}
	if err := store.SaveRun(agentservice.Run{ID: "run_failed_metric", TenantID: tenant.ID, UserID: user.ID, InstanceID: readyInst.ID, SessionID: "sess_failed_metric", Status: agentservice.RunStatusFailed, Error: "boom", StartedAt: now.Add(time.Second)}); err != nil {
		t.Fatalf("SaveRun failed: %v", err)
	}
	if err := store.SaveAuditEvent(agentservice.AuditEvent{ID: "audit_run_succeeded_metric", TenantID: tenant.ID, UserID: user.ID, Action: "run.succeeded", ResourceType: "run", ResourceID: "run_ok_metric", CreatedAt: now.Add(2 * time.Second)}); err != nil {
		t.Fatalf("SaveAuditEvent run.succeeded: %v", err)
	}
	if err := store.SaveAuditEvent(agentservice.AuditEvent{ID: "audit_run_failed_metric", TenantID: tenant.ID, UserID: user.ID, Action: "run.failed", ResourceType: "run", ResourceID: "run_failed_metric", CreatedAt: now.Add(3 * time.Second)}); err != nil {
		t.Fatalf("SaveAuditEvent run.failed: %v", err)
	}
	metricJob := server.jobs.createUserJob("skill.import", principal, func(ctx context.Context) (any, error) {
		return map[string]string{"status": "ok"}, nil
	})
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		job, ok := server.jobs.getUserJob(metricJob.ID, principal)
		if ok && job.Status == asyncJobStatusSucceeded {
			break
		}
	}
	if err := svc.RecordTokenAuthFailure(context.Background(), "metric-key", "203.0.113.10", "unauthorized"); err != nil {
		t.Fatalf("RecordTokenAuthFailure: %v", err)
	}
	if err := svc.RecordTokenRateLimit(context.Background(), "metric-key", "203.0.113.10"); err != nil {
		t.Fatalf("RecordTokenRateLimit: %v", err)
	}
	if err := store.SaveAuditEvent(agentservice.AuditEvent{ID: "audit_admin_auth_failed_metric", ActorType: "admin", Action: "admin.auth_failed", ResourceType: "admin_auth", ResourceID: "/api/v1/admin/runtime/status", CreatedAt: now.Add(4 * time.Second)}); err != nil {
		t.Fatalf("SaveAuditEvent admin.auth_failed: %v", err)
	}
	if err := store.SaveAuditEvent(agentservice.AuditEvent{ID: "audit_admin_owner_denied_metric", ActorType: "admin", Action: "admin.owner_required_failed", ResourceType: "admin_authorization", ResourceID: "/api/v1/admin/sandbox/switch", CreatedAt: now.Add(5 * time.Second)}); err != nil {
		t.Fatalf("SaveAuditEvent admin.owner_required_failed: %v", err)
	}
	if err := store.SaveAuditEvent(agentservice.AuditEvent{ID: "audit_admin_login_failed_metric", ActorType: "admin", Action: "admin.login_failed", ResourceType: "admin_user", ResourceID: "admin", CreatedAt: now.Add(6 * time.Second)}); err != nil {
		t.Fatalf("SaveAuditEvent admin.login_failed: %v", err)
	}
	if err := store.SaveAuditEvent(agentservice.AuditEvent{ID: "audit_admin_login_rate_limited_metric", ActorType: "admin", Action: "admin.login_rate_limited", ResourceType: "admin_user", ResourceID: "admin", CreatedAt: now.Add(7 * time.Second)}); err != nil {
		t.Fatalf("SaveAuditEvent admin.login_rate_limited: %v", err)
	}
	if err := store.SaveAuditEvent(agentservice.AuditEvent{ID: "audit_admin_password_change_failed_metric", ActorType: "admin", Action: "admin.password_change_failed", ResourceType: "admin_user", ResourceID: "admin", CreatedAt: now.Add(8 * time.Second)}); err != nil {
		t.Fatalf("SaveAuditEvent admin.password_change_failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("metrics status = %d body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "# TYPE maclaw_metrics_up gauge") ||
		!strings.Contains(body, "maclaw_metrics_up 1") ||
		!strings.Contains(body, "maclaw_tenants_total 1") ||
		!strings.Contains(body, "maclaw_users_total 2") ||
		!strings.Contains(body, "maclaw_credentials_total 4") ||
		!strings.Contains(body, "maclaw_credentials_by_status{status=\"active\"} 2") ||
		!strings.Contains(body, "maclaw_credentials_by_status{status=\"suspended\"} 1") ||
		!strings.Contains(body, "maclaw_credentials_by_status{status=\"revoked\"} 1") ||
		!strings.Contains(body, "maclaw_credentials_expired_total 1") ||
		!strings.Contains(body, "maclaw_credentials_expiring_total 1") ||
		!strings.Contains(body, "maclaw_instances_unready_total 1") ||
		!strings.Contains(body, "maclaw_auth_token_failed_total 1") ||
		!strings.Contains(body, "maclaw_auth_token_rate_limited_total 1") ||
		!strings.Contains(body, "maclaw_admin_auth_failed_total 1") ||
		!strings.Contains(body, "maclaw_admin_owner_denied_total 1") ||
		!strings.Contains(body, "maclaw_admin_login_failed_total 1") ||
		!strings.Contains(body, "maclaw_admin_login_rate_limited_total 1") ||
		!strings.Contains(body, "maclaw_admin_password_change_failed_total 1") ||
		!strings.Contains(body, "maclaw_runs_waiting_for_user_total 1") ||
		!strings.Contains(body, "maclaw_runs_failed_total 1") ||
		!strings.Contains(body, "maclaw_run_succeeded_events_total 1") ||
		!strings.Contains(body, "maclaw_run_failed_events_total 1") ||
		!strings.Contains(body, "maclaw_async_jobs_total{status=\"succeeded\"} 1") {
		t.Fatalf("unexpected metrics body: %s", body)
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "text/plain") {
		t.Fatalf("unexpected metrics content type: %s", got)
	}
	_ = unreadyInst
}
func TestReadyEndpoint(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("readyz status = %d body = %s", w.Code, w.Body.String())
	}
	var out map[string]string
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode readyz: %v", err)
	}
	if out["status"] != "ready" {
		t.Fatalf("unexpected readyz payload: %#v", out)
	}
}

func TestReadyEndpointReturnsUnavailableWhenDataRootMissing(t *testing.T) {
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if err := os.RemoveAll(dataRoot); err != nil {
		t.Fatalf("RemoveAll data root: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz missing data root status = %d body = %s", w.Code, w.Body.String())
	}
	var out map[string]string
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode not ready payload: %v", err)
	}
	if out["status"] != "not_ready" || out["error"] != "data root unavailable" {
		t.Fatalf("unexpected not ready payload: %#v", out)
	}
}

func TestReadyEndpointReturnsUnavailableWhenDataRootIsFile(t *testing.T) {
	root := t.TempDir()
	dataRoot := filepath.Join(root, "not-a-dir")
	if err := os.WriteFile(dataRoot, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile data root file: %v", err)
	}
	_, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err == nil {
		t.Fatalf("expected NewService to reject file data root")
	}
}

func TestCheckReadyDataRootRejectsFilePath(t *testing.T) {
	root := t.TempDir()
	dataRoot := filepath.Join(root, "not-a-dir")
	if err := os.WriteFile(dataRoot, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile data root file: %v", err)
	}
	if err := checkReadyDataRoot(dataRoot); err == nil || err.Error() != "data root is not a directory" {
		t.Fatalf("expected file data root to be rejected, got %v", err)
	}
}

func TestAdminSystemReadinessEndpoint(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/readiness", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin readiness status = %d body = %s", w.Code, w.Body.String())
	}
	var out readinessReport
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode admin readiness: %v", err)
	}
	if out.Status != "ready" || out.GeneratedAt.IsZero() || out.DataRoot == "" {
		t.Fatalf("unexpected readiness payload: %#v", out)
	}
	if len(out.Checks) < 4 {
		t.Fatalf("expected detailed checks, got %#v", out.Checks)
	}
	for _, check := range out.Checks {
		if check.Status != "pass" {
			t.Fatalf("expected all checks to pass, got %#v", out.Checks)
		}
	}
}

func TestAdminSystemReadinessEndpointReturnsUnavailableWhenDataRootMissing(t *testing.T) {
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if err := os.RemoveAll(dataRoot); err != nil {
		t.Fatalf("RemoveAll data root: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/readiness", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("admin readiness missing data root status = %d body = %s", w.Code, w.Body.String())
	}
	var out readinessReport
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode admin readiness unavailable: %v", err)
	}
	if out.Status != "not_ready" {
		t.Fatalf("unexpected readiness status: %#v", out)
	}
	failed := 0
	for _, check := range out.Checks {
		if check.Status == "fail" {
			failed++
		}
	}
	if failed == 0 {
		t.Fatalf("expected at least one failed check: %#v", out.Checks)
	}
}

func TestAdminSystemReadinessEndpointRequiresAdminSecret(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/readiness", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body = %s", w.Code, w.Body.String())
	}
}

func TestSystemEndpoints(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	for _, path := range []string{"/health", "/livez"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status = %d body = %s", path, w.Code, w.Body.String())
		}
		var out map[string]string
		if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if out["status"] == "" {
			t.Fatalf("missing status payload for %s: %#v", path, out)
		}
		if _, ok := out["data_root"]; ok {
			t.Fatalf("%s should not expose data_root: %#v", path, out)
		}
		if _, ok := out["path"]; ok {
			t.Fatalf("%s should not expose filesystem path: %#v", path, out)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("/version status = %d body = %s", w.Code, w.Body.String())
	}
	var version map[string]string
	if err := json.NewDecoder(w.Body).Decode(&version); err != nil {
		t.Fatalf("decode /version: %v", err)
	}
	if version["version"] == "" {
		t.Fatalf("unexpected version payload: %#v", version)
	}
}

func TestAdminCanCreateGeneratedCredentialOneTimeReveal(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	expiresAt := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	body := bytes.NewBufferString(fmt.Sprintf(`{"name":"Generated API","expires_at":"%s"}`, expiresAt.Format(time.RFC3339Nano)))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/credentials", body)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create generated credential status = %d body = %s", w.Code, w.Body.String())
	}
	var created agentservice.Credential
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode generated credential: %v", err)
	}
	if !strings.HasPrefix(created.APIKey, "mck_") || !strings.HasPrefix(created.APISecret, "mcs_") {
		t.Fatalf("expected generated key and secret once, got %#v", created)
	}
	if created.ExpiresAt == nil || !created.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expected create response to include expires_at %s, got %#v", expiresAt, created)
	}
	if _, err := svc.IssueToken(context.Background(), agentservice.IssueTokenInput{APIKey: created.APIKey, APISecret: created.APISecret}); err != nil {
		t.Fatalf("generated credential should issue token: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/credentials/"+created.ID, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get generated credential status = %d body = %s", w.Code, w.Body.String())
	}
	var fetched agentservice.Credential
	if err := json.NewDecoder(w.Body).Decode(&fetched); err != nil {
		t.Fatalf("decode fetched credential: %v", err)
	}
	if fetched.APISecret != "" || fetched.APIKey == created.APIKey || fetched.APIKeyHash != "" || fetched.SecretDigest != "" {
		t.Fatalf("expected fetched credential to be sanitized: %#v", fetched)
	}
	if fetched.ExpiresAt == nil || !fetched.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expected fetched credential to preserve expires_at %s, got %#v", expiresAt, fetched)
	}
}

func TestAdminCanFilterCredentials(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	now := time.Now().UTC()
	expiringAt := now.Add(48 * time.Hour)
	expiredAt := now.Add(-time.Hour)
	expiringCred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "Expiring", APIKey: "filter-expiring", APISecret: "secret", ExpiresAt: &expiringAt})
	if err != nil {
		t.Fatalf("CreateCredential expiring: %v", err)
	}
	expiredCred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "Expired", APIKey: "filter-expired", APISecret: "secret", ExpiresAt: &expiredAt})
	if err != nil {
		t.Fatalf("CreateCredential expired: %v", err)
	}
	suspendedStatus := agentservice.CredentialStatusSuspended
	suspendedCred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "Suspended", APIKey: "filter-suspended", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential suspended: %v", err)
	}
	if _, err := svc.UpdateCredential(context.Background(), tenant.ID, user.ID, suspendedCred.ID, agentservice.UpdateCredentialInput{Status: &suspendedStatus}); err != nil {
		t.Fatalf("UpdateCredential suspended: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret", nil)
	listCredentials := func(query string) []agentservice.Credential {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/credentials"+query, nil)
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("list credentials %q status = %d body = %s", query, w.Code, w.Body.String())
		}
		var out struct {
			Items []agentservice.Credential `json:"items"`
		}
		if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
			t.Fatalf("decode credentials %q: %v", query, err)
		}
		return out.Items
	}
	if got := listCredentials("?status=suspended"); len(got) != 1 || got[0].ID != suspendedCred.ID {
		t.Fatalf("unexpected suspended filter result: %#v", got)
	}
	if got := listCredentials("?expired=true"); len(got) != 1 || got[0].ID != expiredCred.ID {
		t.Fatalf("unexpected expired filter result: %#v", got)
	}
	if got := listCredentials("?expiring=true"); len(got) != 1 || got[0].ID != expiringCred.ID {
		t.Fatalf("unexpected expiring filter result: %#v", got)
	}
	if got := listCredentials("?status=active&expired=false&expiring=false"); len(got) != 0 {
		t.Fatalf("unexpected active non-expiring/non-expired filter result: %#v", got)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/credentials?status=paused", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid credential status filter status = %d body = %s", w.Code, w.Body.String())
	}
}
func TestAdminCanListAndRevokeCredentials(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	cred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest("GET", "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/credentials", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list credentials status = %d body = %s", w.Code, w.Body.String())
	}
	var listed struct {
		Items      []agentservice.Credential `json:"items"`
		Limit      int                       `json:"limit"`
		HasMore    bool                      `json:"has_more"`
		NextBefore string                    `json:"next_before"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode credentials: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].ID != cred.ID || listed.Items[0].Status != agentservice.CredentialStatusActive {
		t.Fatalf("credentials = %#v", listed.Items)
	}
	if listed.Limit != defaultPageLimit || listed.HasMore || listed.NextBefore != "" {
		t.Fatalf("unexpected credential page meta: %#v", listed)
	}

	req = httptest.NewRequest("DELETE", "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/credentials/"+cred.ID, bytes.NewReader(nil))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke credential status = %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.IssueToken(context.Background(), agentservice.IssueTokenInput{APIKey: "key", APISecret: "secret"}); err == nil {
		t.Fatalf("expected revoked credential to reject token issuance")
	}

	otherCred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API-2", APIKey: "key-2", APISecret: "secret-2"})
	if err != nil {
		t.Fatalf("CreateCredential second: %v", err)
	}
	body := bytes.NewBufferString(`{"status":"suspended"}`)
	req = httptest.NewRequest("PATCH", "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/credentials/"+otherCred.ID, body)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("suspend credential status = %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.IssueToken(context.Background(), agentservice.IssueTokenInput{APIKey: "key-2", APISecret: "secret-2"}); err == nil {
		t.Fatalf("expected suspended credential to reject token issuance")
	}
	for _, tc := range []struct {
		action       string
		credentialID string
	}{
		{action: "admin.credential_revoked", credentialID: cred.ID},
		{action: "admin.credential_updated", credentialID: otherCred.ID},
	} {
		events, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{TenantID: tenant.ID, UserID: user.ID, Action: tc.action, ResourceType: "credential", ResourceID: tc.credentialID})
		if err != nil {
			t.Fatalf("ListAuditEvents %s: %v", tc.action, err)
		}
		if len(events) != 1 {
			t.Fatalf("expected one %s audit event, got %#v", tc.action, events)
		}
		if events[0].Metadata["tenant_id"] != tenant.ID || events[0].Metadata["user_id"] != user.ID || events[0].Metadata["api_key_prefix"] == "" {
			t.Fatalf("unexpected credential audit metadata: %#v", events[0].Metadata)
		}
	}
}

func TestAdminCredentialExpireViaPatch(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	cred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "expire-http-key", APISecret: "expire-http-secret"})
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	expiresAt := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	body := bytes.NewBufferString(fmt.Sprintf(`{"expires_at":"%s"}`, expiresAt))
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/credentials/"+cred.ID, body)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expire credential patch status = %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.IssueToken(context.Background(), agentservice.IssueTokenInput{APIKey: "expire-http-key", APISecret: "expire-http-secret"}); err == nil {
		t.Fatalf("expected expired credential to reject token issuance")
	}
	body = bytes.NewBufferString(`{"clear_expires_at":true}`)
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/credentials/"+cred.ID, body)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("clear expire credential patch status = %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.IssueToken(context.Background(), agentservice.IssueTokenInput{APIKey: "expire-http-key", APISecret: "expire-http-secret"}); err != nil {
		t.Fatalf("expected cleared expiration to restore token issuance: %v", err)
	}
}
func TestAdminPaginationForTenantsUsersAndCredentials(t *testing.T) {
	ctx := context.Background()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	createdTenants := make([]agentservice.Tenant, 0, 3)
	for i := 1; i <= 3; i++ {
		tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: fmt.Sprintf("Tenant %d", i)})
		if err != nil {
			t.Fatalf("CreateTenant %d: %v", i, err)
		}
		createdTenants = append(createdTenants, *tenant)
		time.Sleep(2 * time.Millisecond)
	}

	targetTenant := createdTenants[2]
	createdUsers := make([]agentservice.User, 0, 3)
	for i := 1; i <= 3; i++ {
		user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: targetTenant.ID, Name: fmt.Sprintf("User %d", i)})
		if err != nil {
			t.Fatalf("CreateUser %d: %v", i, err)
		}
		createdUsers = append(createdUsers, *user)
		time.Sleep(2 * time.Millisecond)
	}

	targetUser := createdUsers[2]
	createdCredentials := make([]agentservice.Credential, 0, 3)
	for i := 1; i <= 3; i++ {
		cred, err := svc.CreateCredential(ctx, agentservice.CreateCredentialInput{
			TenantID:  targetTenant.ID,
			UserID:    targetUser.ID,
			Name:      fmt.Sprintf("Cred %d", i),
			APIKey:    fmt.Sprintf("key-%d", i),
			APISecret: fmt.Sprintf("secret-%d", i),
		})
		if err != nil {
			t.Fatalf("CreateCredential %d: %v", i, err)
		}
		createdCredentials = append(createdCredentials, *cred)
		time.Sleep(2 * time.Millisecond)
	}

	server := NewHTTPServer(svc, "admin-secret", nil)

	var tenantsPage struct {
		Items      []agentservice.Tenant `json:"items"`
		Limit      int                   `json:"limit"`
		HasMore    bool                  `json:"has_more"`
		NextBefore string                `json:"next_before"`
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants?limit=2", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tenant page status = %d body = %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&tenantsPage); err != nil {
		t.Fatalf("decode tenant page: %v", err)
	}
	if tenantsPage.Limit != 2 || !tenantsPage.HasMore || tenantsPage.NextBefore == "" {
		t.Fatalf("unexpected tenant page meta: %#v", tenantsPage)
	}
	if len(tenantsPage.Items) != 2 || tenantsPage.Items[0].ID != createdTenants[1].ID || tenantsPage.Items[1].ID != createdTenants[2].ID {
		t.Fatalf("unexpected tenant page items: %#v", tenantsPage.Items)
	}

	var tenantTail struct {
		Items      []agentservice.Tenant `json:"items"`
		Limit      int                   `json:"limit"`
		HasMore    bool                  `json:"has_more"`
		NextBefore string                `json:"next_before"`
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants?limit=2&before="+url.QueryEscape(tenantsPage.NextBefore), nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tenant tail status = %d body = %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&tenantTail); err != nil {
		t.Fatalf("decode tenant tail: %v", err)
	}
	if tenantTail.HasMore || tenantTail.NextBefore != "" || len(tenantTail.Items) != 1 || tenantTail.Items[0].ID != createdTenants[0].ID {
		t.Fatalf("unexpected tenant tail: %#v", tenantTail)
	}

	var usersPage struct {
		Items      []agentservice.User `json:"items"`
		Limit      int                 `json:"limit"`
		HasMore    bool                `json:"has_more"`
		NextBefore string              `json:"next_before"`
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+targetTenant.ID+"/users?limit=2", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("user page status = %d body = %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&usersPage); err != nil {
		t.Fatalf("decode user page: %v", err)
	}
	if usersPage.Limit != 2 || !usersPage.HasMore || usersPage.NextBefore == "" {
		t.Fatalf("unexpected user page meta: %#v", usersPage)
	}
	if len(usersPage.Items) != 2 || usersPage.Items[0].ID != createdUsers[1].ID || usersPage.Items[1].ID != createdUsers[2].ID {
		t.Fatalf("unexpected user page items: %#v", usersPage.Items)
	}

	var credentialsPage struct {
		Items      []agentservice.Credential `json:"items"`
		Limit      int                       `json:"limit"`
		HasMore    bool                      `json:"has_more"`
		NextBefore string                    `json:"next_before"`
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+targetTenant.ID+"/users/"+targetUser.ID+"/credentials?limit=2", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("credential page status = %d body = %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&credentialsPage); err != nil {
		t.Fatalf("decode credential page: %v", err)
	}
	if credentialsPage.Limit != 2 || !credentialsPage.HasMore || credentialsPage.NextBefore == "" {
		t.Fatalf("unexpected credential page meta: %#v", credentialsPage)
	}
	if len(credentialsPage.Items) != 2 || credentialsPage.Items[0].ID != createdCredentials[1].ID || credentialsPage.Items[1].ID != createdCredentials[2].ID {
		t.Fatalf("unexpected credential page items: %#v", credentialsPage.Items)
	}
}

func TestAdminCanListAuditEvents(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := store.SaveAuditEvent(agentservice.AuditEvent{ID: "audit_secret", TenantID: tenant.ID, UserID: user.ID, ActorType: "admin", Action: "secret.audit", ResourceType: "test", ResourceID: svc.DataRoot(), Metadata: map[string]string{"token": "audit-secret", "api_key": "audit-api-key", "path": svc.DataRoot()}, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("SaveAuditEvent secret: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest("GET", "/api/v1/admin/audit-events?tenant_id="+tenant.ID+"&action=user.created", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list audit events status = %d body = %s", w.Code, w.Body.String())
	}
	var events struct {
		Items []agentservice.AuditEvent `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&events); err != nil {
		t.Fatalf("decode audit events: %v", err)
	}
	if len(events.Items) != 1 || events.Items[0].Action != "user.created" || events.Items[0].UserID != user.ID {
		t.Fatalf("audit events = %#v", events.Items)
	}
	if events.Items[0].ActorType != "admin" || events.Items[0].ResourceType != "user" {
		t.Fatalf("unexpected audit event = %#v", events.Items[0])
	}
	req = httptest.NewRequest("GET", "/api/v1/admin/audit-events?action=secret.audit", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list secret audit events status = %d body = %s", w.Code, w.Body.String())
	}
	var secretEvents struct {
		Items []agentservice.AuditEvent `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&secretEvents); err != nil {
		t.Fatalf("decode secret audit events: %v", err)
	}
	secretAuditJSON, err := json.Marshal(secretEvents.Items)
	if err != nil {
		t.Fatalf("marshal secret audit events: %v", err)
	}
	if len(secretEvents.Items) != 1 || strings.Contains(string(secretAuditJSON), "audit-secret") || strings.Contains(string(secretAuditJSON), "audit-api-key") || strings.Contains(string(secretAuditJSON), svc.DataRoot()) {
		t.Fatalf("expected redacted secret audit event, got %s", secretAuditJSON)
	}
	cred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "audit-filter-key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	req = httptest.NewRequest("GET", "/api/v1/admin/audit-events?resource_id="+url.QueryEscape(cred.ID)+"&actor_type=admin", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list audit events by resource/actor status = %d body = %s", w.Code, w.Body.String())
	}
	var credentialEvents struct {
		Items []agentservice.AuditEvent `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&credentialEvents); err != nil {
		t.Fatalf("decode credential audit events: %v", err)
	}
	if len(credentialEvents.Items) != 1 || credentialEvents.Items[0].ResourceID != cred.ID || credentialEvents.Items[0].ActorType != "admin" || credentialEvents.Items[0].Action != "credential.created" {
		t.Fatalf("unexpected credential audit events = %#v", credentialEvents.Items)
	}
	base := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	if err := store.SaveAuditEvent(agentservice.AuditEvent{ID: "audit_window_old", TenantID: tenant.ID, UserID: user.ID, ActorType: "admin", Action: "window.old", ResourceType: "test", ResourceID: "old", CreatedAt: base}); err != nil {
		t.Fatalf("SaveAuditEvent old: %v", err)
	}
	if err := store.SaveAuditEvent(agentservice.AuditEvent{ID: "audit_window_mid", TenantID: tenant.ID, UserID: user.ID, ActorType: "admin", Action: "window.mid", ResourceType: "test", ResourceID: "mid", CreatedAt: base.Add(time.Hour)}); err != nil {
		t.Fatalf("SaveAuditEvent mid: %v", err)
	}
	if err := store.SaveAuditEvent(agentservice.AuditEvent{ID: "audit_window_new", TenantID: tenant.ID, UserID: user.ID, ActorType: "admin", Action: "window.new", ResourceType: "test", ResourceID: "new", CreatedAt: base.Add(2 * time.Hour)}); err != nil {
		t.Fatalf("SaveAuditEvent new: %v", err)
	}
	windowURL := "/api/v1/admin/audit-events?resource_type=test&since=" + url.QueryEscape(base.Add(30*time.Minute).Format(time.RFC3339Nano)) + "&until=" + url.QueryEscape(base.Add(90*time.Minute).Format(time.RFC3339Nano))
	req = httptest.NewRequest(http.MethodGet, windowURL, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list audit events by time window status = %d body = %s", w.Code, w.Body.String())
	}
	var windowEvents struct {
		Items []agentservice.AuditEvent `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&windowEvents); err != nil {
		t.Fatalf("decode audit time window events: %v", err)
	}
	if len(windowEvents.Items) != 1 || windowEvents.Items[0].ID != "audit_window_mid" {
		t.Fatalf("unexpected audit time window events = %#v", windowEvents.Items)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-events?since=not-time", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid audit since status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestListRunsFiltersByStatus(t *testing.T) {
	store := agentservice.NewMemoryStore()
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	_, err = svc.UpdateUserConfig(context.Background(), principal, testLLMConfig())
	if err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	now := time.Now().UTC()
	if err := store.SaveRun(agentservice.Run{ID: "run_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: "sess_1", Status: agentservice.RunStatusFailed, Error: "failed token=run-secret path=" + filepath.Join(dataRoot, "runs", "run_1.log"), Metadata: map[string]string{"api_secret": "run-meta-secret", "path": filepath.Join(dataRoot, "runs", "meta.json")}, StartedAt: now.Add(1 * time.Minute)}); err != nil {
		t.Fatalf("SaveRun failed: %v", err)
	}
	if err := store.SaveRun(agentservice.Run{ID: "run_2", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: "sess_2", Status: agentservice.RunStatusSucceeded, StartedAt: now.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("SaveRun succeeded: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest("GET", "/api/v1/instances/"+inst.ID+"/runs?status=failed", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list runs status = %d body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, leaked := range []string{"run-secret", "run-meta-secret", dataRoot} {
		if strings.Contains(body, leaked) {
			t.Fatalf("expected list runs response to redact %q, got %s", leaked, body)
		}
	}
	var runs struct {
		Items []agentservice.Run `json:"items"`
	}
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&runs); err != nil {
		t.Fatalf("decode runs: %v", err)
	}
	if len(runs.Items) != 1 || runs.Items[0].ID != "run_1" || runs.Items[0].Status != agentservice.RunStatusFailed {
		t.Fatalf("runs = %#v", runs.Items)
	}

	req = httptest.NewRequest("GET", "/api/v1/instances/"+inst.ID+"/runs/run_1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get run status = %d body = %s", w.Code, w.Body.String())
	}
	body = w.Body.String()
	for _, leaked := range []string{"run-secret", "run-meta-secret", dataRoot} {
		if strings.Contains(body, leaked) {
			t.Fatalf("expected get run response to redact %q, got %s", leaked, body)
		}
	}
}

func TestListRunsFiltersByResponseSourceAndWaitingForUser(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	_, err = svc.UpdateUserConfig(context.Background(), principal, testLLMConfig())
	if err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	now := time.Now().UTC()
	if err := store.SaveRun(agentservice.Run{ID: "run_wait", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: "sess_1", Status: agentservice.RunStatusSucceeded, ResponseSource: "ask_user", WaitingForUser: true, StartedAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("SaveRun wait: %v", err)
	}
	if err := store.SaveRun(agentservice.Run{ID: "run_done", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: "sess_2", Status: agentservice.RunStatusSucceeded, ResponseSource: "assistant", WaitingForUser: false, StartedAt: now.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("SaveRun done: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest("GET", "/api/v1/instances/"+inst.ID+"/runs?response_source=ask_user&waiting_for_user=true", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list runs status = %d body = %s", w.Code, w.Body.String())
	}
	var runs struct {
		Items []agentservice.Run `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&runs); err != nil {
		t.Fatalf("decode runs: %v", err)
	}
	if len(runs.Items) != 1 || runs.Items[0].ID != "run_wait" {
		t.Fatalf("runs = %#v", runs.Items)
	}
}

func TestListMessagesFiltersByRoleAndSince(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, agentservice.CreateSessionInput{Title: "Demo"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	base := time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)
	if err := store.SaveMessage(agentservice.Message{ID: "msg_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleUser, Content: "hello", CreatedAt: base.Add(1 * time.Minute)}); err != nil {
		t.Fatalf("SaveMessage msg_1: %v", err)
	}
	if err := store.SaveMessage(agentservice.Message{ID: "msg_2", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleAssistant, Content: "hi", CreatedAt: base.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("SaveMessage msg_2: %v", err)
	}
	if err := store.SaveMessage(agentservice.Message{ID: "msg_3", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleAssistant, Content: "followup", CreatedAt: base.Add(3 * time.Minute)}); err != nil {
		t.Fatalf("SaveMessage msg_3: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest("GET", "/api/v1/instances/"+inst.ID+"/sessions/"+sess.ID+"/messages?role=assistant&since=2026-04-24T10:02:00Z", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list messages status = %d body = %s", w.Code, w.Body.String())
	}
	var messages struct {
		Items []agentservice.Message `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&messages); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	if len(messages.Items) != 2 || messages.Items[0].ID != "msg_2" || messages.Items[1].ID != "msg_3" {
		t.Fatalf("messages = %#v", messages.Items)
	}
}

func TestSanitizeConfigTestResultRedactsEndpointSecrets(t *testing.T) {
	dataRoot := t.TempDir()
	result := &agentservice.ConfigTestResult{
		Success:    false,
		Endpoint:   "https://user:pass@example.test/v1?api_key=secret-key&trace=ok",
		Error:      "failed with token=secret-token path=" + dataRoot,
		Message:    "see " + dataRoot,
		Validation: &agentservice.ConfigValidationResult{Issues: []agentservice.ConfigValidationIssue{{Key: dataRoot, Message: "api_secret=secret-validation path=" + dataRoot}}},
	}
	got := sanitizeConfigTestResultForAPI(dataRoot, result)
	body := fmt.Sprintf("%#v", got)
	for _, leaked := range []string{dataRoot, filepath.ToSlash(dataRoot), "secret-key", "secret-token", "secret-validation", "pass"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("expected config test result to redact %q, got %s", leaked, body)
		}
	}
	if !strings.Contains(got.Endpoint, "trace=ok") {
		t.Fatalf("expected benign endpoint query to remain, got %q", got.Endpoint)
	}
}
func TestSanitizeConfigValidationRedactsPathsAndSecrets(t *testing.T) {
	dataRoot := t.TempDir()
	validation := agentservice.ConfigValidationResult{Issues: []agentservice.ConfigValidationIssue{{Key: dataRoot, Message: "api_key=secret-value path=" + dataRoot}}}
	got := sanitizeConfigValidationForAPI(dataRoot, validation)
	body := fmt.Sprintf("%#v", got)
	if strings.Contains(body, dataRoot) || strings.Contains(body, "secret-value") {
		t.Fatalf("expected config validation to be redacted, got %s", body)
	}
}
func TestGetInstanceSummary(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	inst.ReadyReason = "runtime path=" + svc.DataRoot()
	if err := store.SaveInstance(*inst); err != nil {
		t.Fatalf("SaveInstance readiness reason: %v", err)
	}
	now := time.Now().UTC()
	sess1 := agentservice.Session{ID: "sess_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, AgentID: "default", Metadata: map[string]string{"pending_ask_user": "true"}, CreatedAt: now, UpdatedAt: now}
	sess2ArchivedAt := now.Add(2 * time.Minute)
	sess2 := agentservice.Session{ID: "sess_2", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, AgentID: "default", Archived: true, ArchivedAt: &sess2ArchivedAt, CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(3 * time.Minute)}
	if err := store.SaveSession(sess1); err != nil {
		t.Fatalf("SaveSession sess1: %v", err)
	}
	if err := store.SaveSession(sess2); err != nil {
		t.Fatalf("SaveSession sess2: %v", err)
	}
	if err := store.SaveMessage(agentservice.Message{ID: "msg_user", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess1.ID, Role: agentservice.MessageRoleUser, Content: "hello", CreatedAt: now.Add(4 * time.Minute)}); err != nil {
		t.Fatalf("SaveMessage user: %v", err)
	}
	if err := store.SaveMessage(agentservice.Message{ID: "msg_assistant", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess1.ID, Role: agentservice.MessageRoleAssistant, Content: "hi", CreatedAt: now.Add(5 * time.Minute)}); err != nil {
		t.Fatalf("SaveMessage assistant: %v", err)
	}
	completed := now.Add(7 * time.Minute)
	if err := store.SaveRun(agentservice.Run{ID: "run_wait", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess1.ID, Status: agentservice.RunStatusSucceeded, WaitingForUser: true, StartedAt: now.Add(6 * time.Minute), CompletedAt: &completed}); err != nil {
		t.Fatalf("SaveRun wait: %v", err)
	}
	if err := store.SaveRun(agentservice.Run{ID: "run_failed", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess2.ID, Status: agentservice.RunStatusFailed, StartedAt: now.Add(8 * time.Minute)}); err != nil {
		t.Fatalf("SaveRun failed: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+inst.ID+"/summary", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("summary status = %d body = %s", w.Code, w.Body.String())
	}
	var summary agentservice.InstanceSummary
	if err := json.NewDecoder(w.Body).Decode(&summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.InstanceID != inst.ID || summary.Sessions != 2 || summary.ArchivedSessions != 1 || summary.WaitingSessions != 1 {
		t.Fatalf("unexpected session summary: %#v", summary)
	}
	if summary.Messages != 2 || summary.UserMessages != 1 || summary.AssistantMessages != 1 {
		t.Fatalf("unexpected message summary: %#v", summary)
	}
	if summary.Runs != 2 || summary.WaitingRuns != 1 || summary.RunsByStatus[agentservice.RunStatusSucceeded] != 1 || summary.RunsByStatus[agentservice.RunStatusFailed] != 1 {
		t.Fatalf("unexpected run summary: %#v", summary)
	}
	if summary.LastActivityAt == nil {
		t.Fatalf("expected last activity")
	}
	if strings.Contains(summary.ReadyReason, svc.DataRoot()) {
		t.Fatalf("expected summary ready_reason to be redacted: %#v", summary)
	}
}

func TestGetInstanceBootstrapRedactsLocalPaths(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance", Metadata: map[string]string{"note": "path=" + svc.DataRoot()}})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+inst.ID+"/bootstrap", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("bootstrap status = %d body = %s", w.Code, w.Body.String())
	}
	var bootstrap agentservice.InstanceBootstrap
	if err := json.NewDecoder(w.Body).Decode(&bootstrap); err != nil {
		t.Fatalf("decode bootstrap: %v", err)
	}
	body := w.Body.String()
	if bootstrap.DataDir != "" || bootstrap.RuntimeDir != "" || bootstrap.WorkspaceDir != "" || bootstrap.ConversationStorePath != "" || bootstrap.ConfirmationStorePath != "" || strings.Contains(body, svc.DataRoot()) {
		t.Fatalf("expected bootstrap paths to be redacted, got %s", body)
	}
}

func TestGetTenantSummary(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user1, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User One", Email: "one@example.com"})
	if err != nil {
		t.Fatalf("CreateUser user1: %v", err)
	}
	user2, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User Two", Email: "two@example.com"})
	if err != nil {
		t.Fatalf("CreateUser user2: %v", err)
	}
	tenantInstanceLimit := 5
	tenantMessageLimit := 10
	if _, err := svc.UpdateTenant(context.Background(), tenant.ID, agentservice.UpdateTenantInput{MaxInstances: &tenantInstanceLimit, MaxMessages: &tenantMessageLimit}); err != nil {
		t.Fatalf("UpdateTenant quota: %v", err)
	}
	user1SessionLimit := 3
	user1MessageLimit := 4
	if _, err := svc.UpdateUser(context.Background(), tenant.ID, user1.ID, agentservice.UpdateUserInput{MaxSessions: &user1SessionLimit, MaxMessages: &user1MessageLimit}); err != nil {
		t.Fatalf("UpdateUser user1 quota: %v", err)
	}
	disabled := agentservice.UserStatusDisabled
	if _, err := svc.UpdateUser(context.Background(), tenant.ID, user2.ID, agentservice.UpdateUserInput{Status: &disabled}); err != nil {
		t.Fatalf("UpdateUser user2: %v", err)
	}
	principal1 := agentservice.Principal{TenantID: tenant.ID, UserID: user1.ID}
	principal2 := agentservice.Principal{TenantID: tenant.ID, UserID: user2.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal1, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig user1: %v", err)
	}
	if _, err := svc.UpdateUserConfig(context.Background(), principal2, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig user2: %v", err)
	}
	inst1, err := svc.CreateInstance(context.Background(), principal1, agentservice.CreateInstanceInput{Name: "Instance One"})
	if err != nil {
		t.Fatalf("CreateInstance user1: %v", err)
	}
	inst2, err := svc.CreateInstance(context.Background(), principal2, agentservice.CreateInstanceInput{Name: "Instance Two"})
	if err != nil {
		t.Fatalf("CreateInstance user2: %v", err)
	}
	if _, err := svc.StopInstance(context.Background(), principal2, inst2.ID); err != nil {
		t.Fatalf("StopInstance user2: %v", err)
	}
	base := time.Date(2026, 4, 25, 9, 0, 0, 0, time.UTC)
	for _, sess := range []agentservice.Session{
		{ID: "sess_1", TenantID: tenant.ID, UserID: user1.ID, InstanceID: inst1.ID, AgentID: "default", CreatedAt: base, UpdatedAt: base.Add(2 * time.Minute)},
		{ID: "sess_2", TenantID: tenant.ID, UserID: user2.ID, InstanceID: inst2.ID, AgentID: "default", CreatedAt: base.Add(3 * time.Minute), UpdatedAt: base.Add(4 * time.Minute)},
	} {
		if err := store.SaveSession(sess); err != nil {
			t.Fatalf("SaveSession %s: %v", sess.ID, err)
		}
	}
	for _, msg := range []agentservice.Message{
		{ID: "msg_1", TenantID: tenant.ID, UserID: user1.ID, InstanceID: inst1.ID, SessionID: "sess_1", Role: agentservice.MessageRoleUser, Content: "hello", CreatedAt: base.Add(5 * time.Minute)},
		{ID: "msg_2", TenantID: tenant.ID, UserID: user1.ID, InstanceID: inst1.ID, SessionID: "sess_1", Role: agentservice.MessageRoleAssistant, Content: "hi", CreatedAt: base.Add(6 * time.Minute)},
		{ID: "msg_3", TenantID: tenant.ID, UserID: user2.ID, InstanceID: inst2.ID, SessionID: "sess_2", Role: agentservice.MessageRoleUser, Content: "ping", CreatedAt: base.Add(7 * time.Minute)},
	} {
		if err := store.SaveMessage(msg); err != nil {
			t.Fatalf("SaveMessage %s: %v", msg.ID, err)
		}
	}
	completed := base.Add(9 * time.Minute)
	for _, run := range []agentservice.Run{
		{ID: "run_1", TenantID: tenant.ID, UserID: user1.ID, InstanceID: inst1.ID, SessionID: "sess_1", Status: agentservice.RunStatusSucceeded, StartedAt: base.Add(6 * time.Minute), CompletedAt: &completed},
		{ID: "run_2", TenantID: tenant.ID, UserID: user2.ID, InstanceID: inst2.ID, SessionID: "sess_2", Status: agentservice.RunStatusFailed, StartedAt: base.Add(8 * time.Minute)},
	} {
		if err := store.SaveRun(run); err != nil {
			t.Fatalf("SaveRun %s: %v", run.ID, err)
		}
	}
	credentialNow := time.Now().UTC()
	activeCred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user1.ID, Name: "Active", APIKey: "summary-active", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential active: %v", err)
	}
	expiringAt := credentialNow.Add(48 * time.Hour)
	if _, err := svc.UpdateCredential(context.Background(), tenant.ID, user1.ID, activeCred.ID, agentservice.UpdateCredentialInput{ExpiresAt: &expiringAt}); err != nil {
		t.Fatalf("UpdateCredential active expires_at: %v", err)
	}
	suspendedStatus := agentservice.CredentialStatusSuspended
	suspendedCred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user1.ID, Name: "Suspended", APIKey: "summary-suspended", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential suspended: %v", err)
	}
	if _, err := svc.UpdateCredential(context.Background(), tenant.ID, user1.ID, suspendedCred.ID, agentservice.UpdateCredentialInput{Status: &suspendedStatus}); err != nil {
		t.Fatalf("UpdateCredential suspended: %v", err)
	}
	revokedCred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user1.ID, Name: "Revoked", APIKey: "summary-revoked", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential revoked: %v", err)
	}
	if _, err := svc.RevokeCredential(context.Background(), tenant.ID, user1.ID, revokedCred.ID); err != nil {
		t.Fatalf("RevokeCredential: %v", err)
	}
	expiredCred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user2.ID, Name: "Expired", APIKey: "summary-expired", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential expired: %v", err)
	}
	expiredAt := credentialNow.Add(-time.Hour)
	if _, err := svc.UpdateCredential(context.Background(), tenant.ID, user2.ID, expiredCred.ID, agentservice.UpdateCredentialInput{ExpiresAt: &expiredAt}); err != nil {
		t.Fatalf("UpdateCredential expired expires_at: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+tenant.ID+"/summary", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tenant summary status = %d body = %s", w.Code, w.Body.String())
	}
	var summary agentservice.TenantSummary
	if err := json.NewDecoder(w.Body).Decode(&summary); err != nil {
		t.Fatalf("decode tenant summary: %v", err)
	}
	if summary.TenantID != tenant.ID || summary.Users != 2 || summary.ActiveUsers != 1 || summary.DisabledUsers != 1 {
		t.Fatalf("unexpected tenant user summary: %#v", summary)
	}
	if summary.Instances != 2 || summary.ReadyInstances != 1 || summary.StoppedInstances != 1 {
		t.Fatalf("unexpected instance totals: %#v", summary)
	}
	if summary.Sessions != 2 || summary.Messages != 3 || summary.UserMessages != 2 || summary.AssistantMessages != 1 || summary.Runs != 2 {
		t.Fatalf("unexpected activity totals: %#v", summary)
	}
	if summary.RunsByStatus[agentservice.RunStatusSucceeded] != 1 || summary.RunsByStatus[agentservice.RunStatusFailed] != 1 {
		t.Fatalf("unexpected run statuses: %#v", summary)
	}
	if summary.Credentials != 4 || summary.ActiveCredentials != 2 || summary.SuspendedCredentials != 1 || summary.RevokedCredentials != 1 || summary.ExpiredCredentials != 1 || summary.ExpiringCredentials != 1 {
		t.Fatalf("unexpected credential totals: %#v", summary)
	}
	if len(summary.UserSummaries) != 2 || summary.LastActivityAt == nil {
		t.Fatalf("unexpected user breakdown: %#v", summary)
	}
	for _, userSummary := range summary.UserSummaries {
		if userSummary.DataDir != "" || strings.Contains(userSummary.DataDir, svc.DataRoot()) {
			t.Fatalf("expected tenant summary user data_dir to be redacted: %#v", userSummary)
		}
	}
	if summary.Quota.MaxInstances != 5 || summary.QuotaUsage.Instances.Limit != 5 || summary.QuotaUsage.Instances.Used != 2 {
		t.Fatalf("unexpected tenant quota snapshot: %#v", summary.QuotaUsage)
	}
	if summary.QuotaUsage.Instances.Remaining == nil || *summary.QuotaUsage.Instances.Remaining != 3 {
		t.Fatalf("unexpected tenant remaining instances: %#v", summary.QuotaUsage.Instances)
	}
	var user1Summary *agentservice.TenantUserSummary
	for i := range summary.UserSummaries {
		if summary.UserSummaries[i].UserID == user1.ID {
			user1Summary = &summary.UserSummaries[i]
			break
		}
	}
	if user1Summary == nil {
		t.Fatalf("missing user1 summary: %#v", summary.UserSummaries)
	}
	if user1Summary.Credentials != 3 || user1Summary.ActiveCredentials != 1 || user1Summary.SuspendedCredentials != 1 || user1Summary.RevokedCredentials != 1 || user1Summary.ExpiredCredentials != 0 || user1Summary.ExpiringCredentials != 1 {
		t.Fatalf("unexpected user1 credential summary: %#v", user1Summary)
	}
	if user1Summary.EffectiveQuota.MaxSessions != 3 || user1Summary.QuotaUsage.Sessions.Limit != 3 || user1Summary.QuotaUsage.Sessions.Used != 1 {
		t.Fatalf("unexpected user1 quota usage: %#v", user1Summary)
	}
	if user1Summary.QuotaUsage.Sessions.Remaining == nil || *user1Summary.QuotaUsage.Sessions.Remaining != 2 {
		t.Fatalf("unexpected user1 remaining sessions: %#v", user1Summary.QuotaUsage.Sessions)
	}
	if user1Summary.EffectiveQuota.MaxMessages != 4 || user1Summary.QuotaUsage.Messages.Remaining == nil || *user1Summary.QuotaUsage.Messages.Remaining != 2 {
		t.Fatalf("unexpected user1 message quota usage: %#v", user1Summary)
	}
}

func TestCreateInstanceReturnsTooManyRequestsWhenQuotaExceeded(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	one := 1
	if _, err := svc.UpdateUser(context.Background(), tenant.ID, user.ID, agentservice.UpdateUserInput{MaxInstances: &one}); err != nil {
		t.Fatalf("UpdateUser quota: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	body := `{"name":"first"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first create instance status = %d body = %s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/instances", bytes.NewBufferString(`{"name":"second"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second create instance status = %d body = %s", w.Code, w.Body.String())
	}
}
func TestGetUsageSummary(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	instanceLimit := 3
	sessionLimit := 4
	messageLimit := 5
	runLimit := 6
	if _, err := svc.UpdateUser(context.Background(), tenant.ID, user.ID, agentservice.UpdateUserInput{MaxInstances: &instanceLimit, MaxSessions: &sessionLimit, MaxMessages: &messageLimit, MaxRuns: &runLimit}); err != nil {
		t.Fatalf("UpdateUser quota: %v", err)
	}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	now := time.Now().UTC()
	sess := agentservice.Session{ID: "sess_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, AgentID: "default", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if err := store.SaveMessage(agentservice.Message{ID: "msg_user", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleUser, Content: "hello", CreatedAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("SaveMessage user: %v", err)
	}
	if err := store.SaveMessage(agentservice.Message{ID: "msg_assistant", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleAssistant, Content: "hi", CreatedAt: now.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("SaveMessage assistant: %v", err)
	}
	completed := now.Add(3 * time.Minute)
	if err := store.SaveRun(agentservice.Run{ID: "run_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Status: agentservice.RunStatusSucceeded, StartedAt: now.Add(time.Minute), CompletedAt: &completed}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	activeCred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "Usage Active", APIKey: "usage-active", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential active: %v", err)
	}
	expiringAt := now.Add(48 * time.Hour)
	if _, err := svc.UpdateCredential(context.Background(), tenant.ID, user.ID, activeCred.ID, agentservice.UpdateCredentialInput{ExpiresAt: &expiringAt}); err != nil {
		t.Fatalf("UpdateCredential active expires_at: %v", err)
	}
	suspendedStatus := agentservice.CredentialStatusSuspended
	suspendedCred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "Usage Suspended", APIKey: "usage-suspended", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential suspended: %v", err)
	}
	if _, err := svc.UpdateCredential(context.Background(), tenant.ID, user.ID, suspendedCred.ID, agentservice.UpdateCredentialInput{Status: &suspendedStatus}); err != nil {
		t.Fatalf("UpdateCredential suspended: %v", err)
	}
	expiredCred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "Usage Expired", APIKey: "usage-expired", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential expired: %v", err)
	}
	expiredAt := now.Add(-time.Hour)
	if _, err := svc.UpdateCredential(context.Background(), tenant.ID, user.ID, expiredCred.ID, agentservice.UpdateCredentialInput{ExpiresAt: &expiredAt}); err != nil {
		t.Fatalf("UpdateCredential expired expires_at: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest("GET", "/api/v1/usage/summary", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("usage summary status = %d body = %s", w.Code, w.Body.String())
	}
	var summary agentservice.UsageSummary
	if err := json.NewDecoder(w.Body).Decode(&summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.Instances != 1 || summary.Sessions != 1 || summary.Messages != 2 || summary.UserMessages != 1 || summary.AssistantMessages != 1 || summary.Runs != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	if summary.RunsByStatus[agentservice.RunStatusSucceeded] != 1 || summary.LastActivityAt == nil {
		t.Fatalf("summary run status/last activity = %#v", summary)
	}
	if summary.Credentials != 3 || summary.ActiveCredentials != 2 || summary.SuspendedCredentials != 1 || summary.RevokedCredentials != 0 || summary.ExpiredCredentials != 1 || summary.ExpiringCredentials != 1 {
		t.Fatalf("unexpected usage credential counters: %#v", summary)
	}
	if summary.Quota.MaxInstances != 3 || summary.Quota.MaxMessages != 5 || summary.QuotaUsage.Messages.Limit != 5 || summary.QuotaUsage.Messages.Used != 2 {
		t.Fatalf("unexpected usage quota snapshot: %#v", summary)
	}
	if summary.QuotaUsage.Messages.Remaining == nil || *summary.QuotaUsage.Messages.Remaining != 3 {
		t.Fatalf("unexpected usage message remaining: %#v", summary.QuotaUsage.Messages)
	}
	if summary.QuotaUsage.Runs.Remaining == nil || *summary.QuotaUsage.Runs.Remaining != 5 {
		t.Fatalf("unexpected usage run remaining: %#v", summary.QuotaUsage.Runs)
	}
	if summary.DataDir != "" || strings.Contains(summary.DataDir, svc.DataRoot()) {
		t.Fatalf("expected usage summary data_dir to be redacted: %#v", summary)
	}
}

func TestPaginateMessagesReturnsNewestWindowChronologically(t *testing.T) {
	base := time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC)
	items := []agentservice.Message{
		{ID: "msg_1", CreatedAt: base.Add(1 * time.Minute)},
		{ID: "msg_2", CreatedAt: base.Add(2 * time.Minute)},
		{ID: "msg_3", CreatedAt: base.Add(3 * time.Minute)},
	}

	page, err := parsePageQuery(httptest.NewRequest("GET", "/messages?limit=2", nil))
	if err != nil {
		t.Fatalf("parsePageQuery: %v", err)
	}
	got, meta := paginateMessages(items, page)

	if len(got) != 2 || got[0].ID != "msg_2" || got[1].ID != "msg_3" {
		t.Fatalf("unexpected page: %#v", got)
	}
	if !meta.HasMore || meta.NextBefore != got[0].CreatedAt.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected meta: %#v", meta)
	}
}

func TestParsePageQueryCapsLimitAndValidatesBefore(t *testing.T) {
	page, err := parsePageQuery(httptest.NewRequest("GET", "/instances?limit=999", nil))
	if err != nil {
		t.Fatalf("parsePageQuery: %v", err)
	}
	if page.Limit != maxPageLimit {
		t.Fatalf("limit = %d, want %d", page.Limit, maxPageLimit)
	}

	if _, err := parsePageQuery(httptest.NewRequest("GET", "/instances?before=not-time", nil)); err == nil {
		t.Fatalf("expected invalid before error")
	}
}

func testLLMConfig() corelib.AppConfig {
	return corelib.AppConfig{
		MaclawLLMUrl:   "https://llm.example/v1",
		MaclawLLMKey:   "test-key",
		MaclawLLMModel: "test-model",
	}
}

func TestRequestClientIPStripsSourcePort(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/auth/token", nil)
	req.RemoteAddr = "203.0.113.10:54321"
	if got := requestClientIP(req); got != "203.0.113.10" {
		t.Fatalf("requestClientIP = %q", got)
	}
}

func TestTokenRateLimitUsesClientIPNotSourcePort(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	server.authLimiter = newAuthLimiter(1, time.Minute)

	body := []byte(`{"api_key":"missing","api_secret":"wrong"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/token", bytes.NewReader(body))
	req.RemoteAddr = "198.51.100.20:10001"
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("first token attempt status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("POST", "/api/v1/auth/token", bytes.NewReader(body))
	req.RemoteAddr = "198.51.100.20:10099"
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second token attempt status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestFailedTokenAttemptCreatesAuditEvent(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest("POST", "/api/v1/auth/token", bytes.NewReader([]byte(`{"api_key":"missing-key","api_secret":"wrong"}`)))
	req.RemoteAddr = "203.0.113.9:40123"
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("token failure status = %d body = %s", w.Code, w.Body.String())
	}

	events, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: "auth.token_failed"})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 auth.token_failed event, got %#v", events)
	}
	if events[0].Metadata["remote_ip"] != "203.0.113.9" {
		t.Fatalf("unexpected remote_ip metadata = %#v", events[0].Metadata)
	}
	if events[0].Metadata["api_key_prefix"] != "missin" {
		t.Fatalf("unexpected api_key_prefix metadata = %#v", events[0].Metadata)
	}
}

func TestTokenFailureThresholdTriggersTemporaryLock(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	server.authLimiter = newAuthLimiter(100, time.Minute)

	for i := 0; i < 4; i++ {
		req := httptest.NewRequest("POST", "/api/v1/auth/token", bytes.NewReader([]byte(`{"api_key":"lock-key","api_secret":"wrong"}`)))
		req.RemoteAddr = "198.51.100.50:50000"
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d body = %s", i+1, w.Code, w.Body.String())
		}
	}

	req := httptest.NewRequest("POST", "/api/v1/auth/token", bytes.NewReader([]byte(`{"api_key":"lock-key","api_secret":"wrong"}`)))
	req.RemoteAddr = "198.51.100.50:50001"
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("threshold attempt status = %d body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Retry-After"); got == "" {
		t.Fatalf("expected Retry-After header")
	}
}

func TestGetInstanceCapabilities(t *testing.T) {
	executor := &agentservice.CoreAgentExecutor{}
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), executor)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest("GET", "/api/v1/instances/"+inst.ID+"/capabilities", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get capabilities status = %d body = %s", w.Code, w.Body.String())
	}
	var caps agentservice.AgentCapabilities
	if err := json.NewDecoder(w.Body).Decode(&caps); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if caps.Executor != "core_agent" || caps.SupportsSSH || !caps.SupportsAskUser {
		t.Fatalf("unexpected capabilities: %#v", caps)
	}
	if len(caps.Tools) == 0 {
		t.Fatalf("expected tools in capabilities")
	}
	if _, ok := caps.Metadata["workspace_dir"]; ok || strings.Contains(fmt.Sprintf("%#v", caps.Metadata), svc.DataRoot()) {
		t.Fatalf("expected capabilities metadata paths to be redacted: %#v", caps.Metadata)
	}
}

func TestListSkillsSupportsNameCursorPagination(t *testing.T) {
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	skillsRoot := filepath.Join(dataRoot, "tenants", tenant.ID, "users", user.ID, "skills")
	for _, name := range []string{"alpha", "bravo", "charlie"} {
		dir := filepath.Join(skillsRoot, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "KNOWLEDGE.md"), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	server := NewHTTPServer(svc, "admin-secret", nil)
	var page struct {
		Items      []corelib.NLSkillEntry `json:"items"`
		Limit      int                    `json:"limit"`
		HasMore    bool                   `json:"has_more"`
		NextBefore string                 `json:"next_before"`
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/skills?limit=2", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list skills page status = %d body = %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&page); err != nil {
		t.Fatalf("decode skills page: %v", err)
	}
	if page.Limit != 2 || !page.HasMore || page.NextBefore != "bravo" {
		t.Fatalf("unexpected skills page meta: %#v", page)
	}
	if len(page.Items) != 2 || page.Items[0].Name != "bravo" || page.Items[1].Name != "charlie" {
		t.Fatalf("unexpected skills page items: %#v", page.Items)
	}
	pageJSON, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal skills page: %v", err)
	}
	if strings.Contains(string(pageJSON), dataRoot) || strings.Contains(string(pageJSON), "skill_dir") {
		t.Fatalf("expected skill list to redact local skill dirs, got %s", pageJSON)
	}

	var tail struct {
		Items      []corelib.NLSkillEntry `json:"items"`
		Limit      int                    `json:"limit"`
		HasMore    bool                   `json:"has_more"`
		NextBefore string                 `json:"next_before"`
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/skills?limit=2&before="+url.QueryEscape(page.NextBefore), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list skills tail status = %d body = %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&tail); err != nil {
		t.Fatalf("decode skills tail: %v", err)
	}
	if tail.HasMore || tail.NextBefore != "" || len(tail.Items) != 1 || tail.Items[0].Name != "alpha" {
		t.Fatalf("unexpected skills tail: %#v", tail)
	}
}

func TestSanitizeSkillStepForAPIRedactsSensitiveMapKeys(t *testing.T) {
	dataRoot := t.TempDir()
	step := corelib.NLSkillStep{
		Action: "run",
		Params: map[string]interface{}{
			"api_secret": "plain-param-secret",
			"nested": map[string]interface{}{
				"auth_header": "Bearer nested-param-secret",
				"path":        dataRoot,
			},
			"notes": "token=inline-secret path=" + dataRoot,
		},
		Capture: map[string]string{
			"access_token": "plain-capture-secret",
			"summary":      "visible",
		},
	}
	got := sanitizeSkillStepForAPI(dataRoot, step)
	body := fmt.Sprintf("%#v", got)
	for _, leaked := range []string{"plain-param-secret", "nested-param-secret", "plain-capture-secret", "inline-secret", dataRoot, filepath.ToSlash(dataRoot)} {
		if strings.Contains(body, leaked) {
			t.Fatalf("expected skill step to redact %q, got %s", leaked, body)
		}
	}
	if got.Capture["summary"] != "visible" {
		t.Fatalf("expected benign capture to remain visible, got %#v", got.Capture)
	}
}

func TestGetSkillAndValidationRedactLocalPaths(t *testing.T) {
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	skillDir := filepath.Join(dataRoot, "tenants", tenant.ID, "users", user.ID, "skills", "secret-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skill: %v", err)
	}
	knowledge := "# secret skill\n\nUse token=skill-secret-token inside " + dataRoot + "\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(knowledge), 0o644); err != nil {
		t.Fatalf("WriteFile knowledge: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret", nil)
	for _, target := range []struct {
		method string
		path   string
		leaks  []string
	}{
		{method: http.MethodGet, path: "/api/v1/skills/secret-skill", leaks: []string{dataRoot, filepath.ToSlash(dataRoot), "skill-secret-token", "skill_dir"}},
		{method: http.MethodPost, path: "/api/v1/skills/secret-skill/validate", leaks: []string{dataRoot, filepath.ToSlash(dataRoot), "skill-secret-token"}},
	} {
		req := httptest.NewRequest(target.method, target.path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s status = %d body = %s", target.method, target.path, w.Code, w.Body.String())
		}
		body := w.Body.String()
		for _, leaked := range target.leaks {
			if strings.Contains(body, leaked) {
				t.Fatalf("expected %s to redact %q, got %s", target.path, leaked, body)
			}
		}
	}
}
func TestMCPRemoteServerCRUDAndTools(t *testing.T) {
	tenantID, userID, token, server := newMCPAuthenticatedServer(t)
	_ = tenantID
	_ = userID

	remoteMCP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode remote MCP request: %v", err)
		}
		if req["method"] == "initialize" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "session-1")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{}}}`))
			return
		}
		if req["method"] != "tools/list" {
			t.Fatalf("unexpected remote MCP method: %#v", req["method"])
		}
		if got := r.Header.Get("Authorization"); got != "Bearer remote-secret" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("X-MCP-Token"); got != "header-secret" {
			t.Fatalf("x-mcp-token = %q", got)
		}
		if got := r.Header.Get("Mcp-Session-Id"); got != "session-1" {
			t.Fatalf("session id = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"search_docs","description":"Search docs","inputSchema":{"type":"object"}}]}}`))
	}))
	defer remoteMCP.Close()

	body := fmt.Sprintf(`{"kind":"remote","name":"Docs MCP","endpoint_url":%q,"auth_type":"bearer","auth_secret":"remote-secret","headers":{"X-MCP-Token":"header-secret"}}`, remoteMCP.URL)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create remote MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var created agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created MCP server: %v", err)
	}
	if created.Kind != "remote" || created.Name != "Docs MCP" || created.EndpointURL != remoteMCP.URL {
		t.Fatalf("unexpected created remote MCP server: %#v", created)
	}
	if created.HasAuthSecret != true {
		t.Fatalf("expected auth secret marker on created remote MCP server: %#v", created)
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/v1/mcp/servers/"+created.ID, bytes.NewBufferString(`{"auth_secret":"******","headers":{"X-MCP-Token":"******"}}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("masked remote MCP update status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers/"+created.ID+"/health-check", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("remote MCP health status = %d body = %s", w.Code, w.Body.String())
	}
	var checked agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&checked); err != nil {
		t.Fatalf("decode checked MCP server: %v", err)
	}
	if checked.HealthStatus != "healthy" || !checked.Running || len(checked.Tools) != 1 {
		t.Fatalf("unexpected checked MCP server: %#v", checked)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/mcp/servers/"+created.ID+"/tools", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("remote MCP tools status = %d body = %s", w.Code, w.Body.String())
	}
	var tools struct {
		Items []agentservice.MCPToolView `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&tools); err != nil {
		t.Fatalf("decode remote MCP tools: %v", err)
	}
	if len(tools.Items) != 1 || tools.Items[0].Name != "search_docs" {
		t.Fatalf("unexpected remote MCP tools: %#v", tools.Items)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/mcp/servers/"+created.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete remote MCP status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestMCPServerViewsRedactLocalPathsAndSecrets(t *testing.T) {
	_, _, token, server := newMCPAuthenticatedServer(t)
	localRoot := t.TempDir()
	remoteEndpoint := "https://mcp.example.test/api?api_key=remote-query-secret&trace=ok"
	remoteBody := fmt.Sprintf(`{"kind":"remote","name":"Secret Remote","endpoint_url":%q,"auth_type":"bearer","auth_secret":"remote-auth-secret"}`, remoteEndpoint)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers", bytes.NewBufferString(remoteBody))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create remote MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var remoteView agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&remoteView); err != nil {
		t.Fatalf("decode remote MCP view: %v", err)
	}
	if strings.Contains(remoteView.EndpointURL, "remote-query-secret") || !strings.Contains(remoteView.EndpointURL, "trace=ok") {
		t.Fatalf("expected redacted endpoint URL with benign query retained: %#v", remoteView)
	}

	localCommand := filepath.Join(localRoot, "bin", "local-mcp")
	localBody := fmt.Sprintf(`{"kind":"local","name":"Secret Local","command":%q,"args":["--token=local-arg-secret","--path=%s"],"env":{"LOCAL_MCP_TOKEN":"env-secret"}}`, localCommand, filepath.ToSlash(localRoot))
	req = httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers", bytes.NewBufferString(localBody))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create local MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var localView agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&localView); err != nil {
		t.Fatalf("decode local MCP view: %v", err)
	}
	body := w.Body.String()
	for _, leaked := range []string{localRoot, filepath.ToSlash(localRoot), "local-arg-secret", "env-secret"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("expected MCP local view to redact %q, got %s", leaked, body)
		}
	}
	if localView.Command != filepath.Base(localCommand) {
		t.Fatalf("expected local command path basename, got %#v", localView)
	}
}

func TestMCPMarketplaceSearchAndInstall(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	marketPayload := `{"items":[{"id":"jira-mcp","capability_id":"jira-mcp","display_name":"Jira MCP","description":"Jira tools","version":"1.0.0","mcp":{"id":"jira-mcp","name":"Jira MCP","endpoint_url":"https://mcp.example/mcp","auth_type":"none"}}]}`
	market := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/api/capability-market/mcp?q=jira" && r.URL.RequestURI() != "/api/capability-market/mcp?q=jira-mcp" {
			t.Fatalf("unexpected marketplace request %s", r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(marketPayload))
	}))
	defer market.Close()
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{RemoteHubCenterURL: market.URL}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	tamperedDirect := agentservice.MCPCapabilitySummary{ID: "evil-mcp", CapabilityID: "evil-mcp", MetadataJSON: `{"mcp":{"id":"evil-mcp","name":"Evil MCP","endpoint_url":"https://evil.example/mcp","auth_type":"none"}}`}
	body, err := json.Marshal(tamperedDirect)
	if err != nil {
		t.Fatalf("marshal direct tampered item: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/market/install", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code == http.StatusCreated {
		t.Fatalf("direct tampered MCP market install should be rejected, body = %s", w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/mcp/market?q=jira", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("search mcp market status = %d body = %s", w.Code, w.Body.String())
	}
	var search struct {
		Items []agentservice.MCPCapabilitySummary `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&search); err != nil {
		t.Fatalf("decode market search: %v", err)
	}
	if len(search.Items) != 1 || search.Items[0].CapabilityID != "jira-mcp" || search.Items[0].Source != corelib.CapabilitySourceHubCenter {
		t.Fatalf("unexpected market search items: %#v", search.Items)
	}

	installItem := search.Items[0]
	installItem.MetadataJSON = `{"mcp":{"id":"jira-mcp","name":"Tampered MCP","endpoint_url":"https://evil.example/mcp","auth_type":"none"}}`
	body, err = json.Marshal(installItem)
	if err != nil {
		t.Fatalf("marshal market item: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/mcp/market/install", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("install mcp market status = %d body = %s", w.Code, w.Body.String())
	}
	var created agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode installed MCP: %v", err)
	}
	if created.Name != "Jira MCP" || created.EndpointURL != "https://mcp.example/mcp" || created.Source != corelib.MCPSourceMarket || created.Capability == nil || created.Capability.CapabilityID != "jira-mcp" {
		t.Fatalf("unexpected installed MCP: %#v", created)
	}

	marketPayload = `{"items":[{"id":"jira-mcp","capability_id":"jira-mcp","display_name":"Jira MCP","description":"Jira tools","version":"1.0.1","mcp":{"id":"jira-mcp","name":"Jira MCP","command":"npx","args":["-y","@example/jira-mcp"]}}]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/mcp/market/install", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("reinstall changed transport MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var changed agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&changed); err != nil {
		t.Fatalf("decode changed MCP: %v", err)
	}
	if changed.Kind != "local" || changed.Command != "npx" || changed.Capability == nil || changed.Capability.CapabilityID != "jira-mcp" {
		t.Fatalf("unexpected changed transport MCP: %#v", changed)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/mcp/servers", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Jira MCP") || !strings.Contains(w.Body.String(), string(corelib.MCPSourceMarket)) {
		t.Fatalf("list installed MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var listed struct {
		Items []agentservice.MCPServerView `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode listed MCP: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].Kind != "local" {
		t.Fatalf("expected changed transport to replace prior MCP entry, got %#v", listed.Items)
	}
}

func TestListMCPServersSupportsPagination(t *testing.T) {
	_, _, token, server := newMCPAuthenticatedServer(t)

	created := make([]agentservice.MCPServerView, 0, 3)
	for i := 1; i <= 3; i++ {
		body := fmt.Sprintf(`{"kind":"local","name":%q,"command":%q,"args":["-test.run=TestLocalMCPHelperProcess","--"],"env":{"GO_WANT_LOCAL_MCP_HELPER":"1"}}`, fmt.Sprintf("Local %d", i), os.Args[0])
		req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create mcp %d status = %d body = %s", i, w.Code, w.Body.String())
		}
		var item agentservice.MCPServerView
		if err := json.NewDecoder(w.Body).Decode(&item); err != nil {
			t.Fatalf("decode mcp %d: %v", i, err)
		}
		created = append(created, item)
		time.Sleep(1100 * time.Millisecond)
	}

	var page struct {
		Items      []agentservice.MCPServerView `json:"items"`
		Limit      int                          `json:"limit"`
		HasMore    bool                         `json:"has_more"`
		NextBefore string                       `json:"next_before"`
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/mcp/servers?limit=2", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list mcp page status = %d body = %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&page); err != nil {
		t.Fatalf("decode mcp page: %v", err)
	}
	if page.Limit != 2 || !page.HasMore || page.NextBefore == "" {
		t.Fatalf("unexpected mcp page meta: %#v", page)
	}
	if len(page.Items) != 2 || page.Items[0].ID != created[1].ID || page.Items[1].ID != created[2].ID {
		t.Fatalf("unexpected mcp page items: %#v", page.Items)
	}

	var tail struct {
		Items      []agentservice.MCPServerView `json:"items"`
		Limit      int                          `json:"limit"`
		HasMore    bool                         `json:"has_more"`
		NextBefore string                       `json:"next_before"`
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/mcp/servers?limit=2&before="+url.QueryEscape(page.NextBefore), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list mcp tail status = %d body = %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&tail); err != nil {
		t.Fatalf("decode mcp tail: %v", err)
	}
	if tail.HasMore || tail.NextBefore != "" || len(tail.Items) != 1 || tail.Items[0].ID != created[0].ID {
		t.Fatalf("unexpected mcp tail: %#v", tail)
	}
}

func TestMCPLocalServerStartAndStop(t *testing.T) {
	_, _, token, server := newMCPAuthenticatedServer(t)

	cmd := os.Args[0]
	body := fmt.Sprintf(`{"kind":"local","name":"Local Echo","command":%q,"args":["-test.run=TestLocalMCPHelperProcess","--"],"env":{"GO_WANT_LOCAL_MCP_HELPER":"1"}}`, cmd)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create local MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var created agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode local MCP server: %v", err)
	}
	if created.Kind != "local" || created.Command != filepath.Base(cmd) {
		t.Fatalf("unexpected local MCP create result: %#v", created)
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/v1/mcp/servers/"+created.ID, bytes.NewBufferString(`{"env":{"GO_WANT_LOCAL_MCP_HELPER":"******"}}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("masked local MCP update status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers/"+created.ID+"/start", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("start local MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var started agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&started); err != nil {
		t.Fatalf("decode started local MCP server: %v", err)
	}
	if !started.Running || started.HealthStatus != "running" {
		t.Fatalf("unexpected started local MCP server: %#v", started)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/mcp/servers/"+created.ID+"/tools", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("local MCP tools status = %d body = %s", w.Code, w.Body.String())
	}
	var tools struct {
		Items []agentservice.MCPToolView `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&tools); err != nil {
		t.Fatalf("decode local MCP tools: %v", err)
	}
	if len(tools.Items) != 1 || tools.Items[0].Name != "echo" {
		t.Fatalf("unexpected local MCP tools: %#v", tools.Items)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers/"+created.ID+"/stop", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("stop local MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var stopped agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&stopped); err != nil {
		t.Fatalf("decode stopped local MCP server: %v", err)
	}
	if stopped.Running || stopped.HealthStatus != "stopped" {
		t.Fatalf("unexpected stopped local MCP server: %#v", stopped)
	}
}

func newMCPAuthenticatedServer(t *testing.T) (string, string, string, *HTTPServer) {
	t.Helper()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	return tenant.ID, user.ID, token, NewHTTPServer(svc, "admin-secret", nil)
}

func TestLocalMCPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_LOCAL_MCP_HELPER") != "1" {
		return
	}
	defer os.Exit(0)
	reader := bufio.NewReader(os.Stdin)
	writer := os.Stdout
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var req map[string]any
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}
		method, _ := req["method"].(string)
		if method == "notifications/initialized" {
			continue
		}
		id := int(req["id"].(float64))
		switch method {
		case "initialize":
			_, _ = fmt.Fprintf(writer, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"protocolVersion\":\"2024-11-05\",\"capabilities\":{}}}\n", id)
		case "tools/list":
			_, _ = fmt.Fprintf(writer, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"tools\":[{\"name\":\"echo\",\"description\":\"Echo text\",\"inputSchema\":{\"type\":\"object\"}}]}}\n", id)
		default:
			_, _ = fmt.Fprintf(writer, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{}}\n", id)
		}
	}
}

func TestCancelRunEndpointCancelsRunningExecution(t *testing.T) {
	store := agentservice.NewMemoryStore()
	executor := &blockingExecutor{started: make(chan string, 1)}
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, executor)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	resultCh := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		body := bytes.NewBufferString(`{"content":"please block"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/messages", body)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		resultCh <- rec
	}()

	select {
	case <-executor.started:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for executor to start")
	}

	var runID string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := store.ListRuns(tenant.ID, user.ID, inst.ID)
		if err == nil && len(runs) > 0 {
			runID = runs[0].ID
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if runID == "" {
		t.Fatalf("expected run to be persisted")
	}

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/runs/"+runID+"/cancel", nil)
	cancelReq.Header.Set("Authorization", "Bearer "+token)
	cancelRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(cancelRec, cancelReq)
	if cancelRec.Code != http.StatusOK {
		t.Fatalf("cancel run status = %d body = %s", cancelRec.Code, cancelRec.Body.String())
	}
	var cancelled agentservice.Run
	if err := json.NewDecoder(cancelRec.Body).Decode(&cancelled); err != nil {
		t.Fatalf("decode cancelled run: %v", err)
	}
	if cancelled.Status != agentservice.RunStatusCancelled {
		t.Fatalf("expected cancelled run, got %#v", cancelled)
	}

	select {
	case rec := <-resultCh:
		if rec.Code != http.StatusConflict {
			t.Fatalf("send message status = %d body = %s", rec.Code, rec.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for send request to finish after cancel")
	}

	storedRun, err := store.GetRun(tenant.ID, user.ID, inst.ID, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if storedRun.Status != agentservice.RunStatusCancelled {
		t.Fatalf("stored run = %#v", storedRun)
	}
}

func TestUpdateInstanceEndpoint(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Old Name", Description: "old desc", Metadata: map[string]string{"tier": "dev"}})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	body := bytes.NewBufferString(`{"name":"Renamed Instance","description":"new desc","metadata":{"tier":"prod","region":"cn"}}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/instances/"+inst.ID, body)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update instance status = %d body = %s", w.Code, w.Body.String())
	}
	var updated agentservice.Instance
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated instance: %v", err)
	}
	if updated.Name != "Renamed Instance" || updated.Description != "new desc" {
		t.Fatalf("unexpected instance update: %#v", updated)
	}
	if len(updated.Metadata) != 2 || updated.Metadata["tier"] != "prod" || updated.Metadata["region"] != "cn" {
		t.Fatalf("unexpected metadata: %#v", updated.Metadata)
	}
	if updated.DataDir != "" || updated.RuntimeDir != "" || updated.Workspace != "" || strings.Contains(fmt.Sprintf("%#v", updated), svc.DataRoot()) {
		t.Fatalf("expected updated instance paths to be redacted: %#v", updated)
	}
}

func TestUpdateSessionEndpoint(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, agentservice.CreateSessionInput{Title: "Old", Metadata: map[string]string{"a": "1"}})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	body := bytes.NewBufferString(`{"title":"Renamed","metadata":{"env":"prod","region":"cn"}}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/instances/"+inst.ID+"/sessions/"+sess.ID, body)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update session status = %d body = %s", w.Code, w.Body.String())
	}
	var updated agentservice.Session
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated session: %v", err)
	}
	if updated.Title != "Renamed" {
		t.Fatalf("unexpected title: %#v", updated)
	}
	if len(updated.Metadata) != 2 || updated.Metadata["env"] != "prod" || updated.Metadata["region"] != "cn" {
		t.Fatalf("unexpected metadata: %#v", updated.Metadata)
	}
}

func TestArchiveSessionLifecycle(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, agentservice.CreateSessionInput{Title: "Demo"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/sessions/"+sess.ID+"/archive", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("archive session status = %d body = %s", w.Code, w.Body.String())
	}
	var archived agentservice.Session
	if err := json.NewDecoder(w.Body).Decode(&archived); err != nil {
		t.Fatalf("decode archived session: %v", err)
	}
	if !archived.Archived || archived.ArchivedAt == nil {
		t.Fatalf("expected archived session, got %#v", archived)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+inst.ID+"/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list sessions status = %d body = %s", w.Code, w.Body.String())
	}
	var listed struct {
		Items []agentservice.Session `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	if len(listed.Items) != 0 {
		t.Fatalf("expected archived session hidden from default list, got %#v", listed.Items)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+inst.ID+"/sessions?include_archived=true", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list archived sessions status = %d body = %s", w.Code, w.Body.String())
	}
	listed = struct {
		Items []agentservice.Session `json:"items"`
	}{}
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode archived sessions: %v", err)
	}
	if len(listed.Items) != 1 || !listed.Items[0].Archived {
		t.Fatalf("expected archived session in explicit list, got %#v", listed.Items)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/sessions/"+sess.ID+"/messages", bytes.NewBufferString(`{"content":"hello"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("post to archived session status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/sessions/"+sess.ID+"/restore", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("restore session status = %d body = %s", w.Code, w.Body.String())
	}
	var restored agentservice.Session
	if err := json.NewDecoder(w.Body).Decode(&restored); err != nil {
		t.Fatalf("decode restored session: %v", err)
	}
	if restored.Archived || restored.ArchivedAt != nil {
		t.Fatalf("expected restored session, got %#v", restored)
	}
}

func TestDeleteSessionRemovesMessagesAndRuns(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, agentservice.CreateSessionInput{Title: "Demo"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	now := time.Now().UTC()
	if err := store.SaveMessage(agentservice.Message{ID: "msg_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleUser, Content: "hello", CreatedAt: now}); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	if err := store.SaveRun(agentservice.Run{ID: "run_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Status: agentservice.RunStatusSucceeded, StartedAt: now}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/instances/"+inst.ID+"/sessions/"+sess.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete session status = %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.GetSession(context.Background(), principal, inst.ID, sess.ID); err == nil {
		t.Fatalf("expected deleted session to be missing")
	}
	msgs, err := store.ListMessages(sess.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected session messages deleted, got %#v", msgs)
	}
	if _, err := store.GetRun(tenant.ID, user.ID, inst.ID, "run_1"); err == nil {
		t.Fatalf("expected session runs deleted")
	}
}

func TestDeleteInstanceRemovesRuntimeAndChildren(t *testing.T) {
	store := agentservice.NewMemoryStore()
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, agentservice.CreateSessionInput{Title: "Demo"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	now := time.Now().UTC()
	if err := store.SaveRun(agentservice.Run{ID: "run_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Status: agentservice.RunStatusSucceeded, StartedAt: now}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/instances/"+inst.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete instance status = %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.GetInstance(context.Background(), principal, inst.ID); err == nil {
		t.Fatalf("expected deleted instance to be missing")
	}
	if _, err := svc.GetSession(context.Background(), principal, inst.ID, sess.ID); err == nil {
		t.Fatalf("expected child session to be removed")
	}
	if _, err := store.GetRun(tenant.ID, user.ID, inst.ID, "run_1"); err == nil {
		t.Fatalf("expected child run to be removed")
	}
	if _, err := os.Stat(inst.RuntimeDir); !os.IsNotExist(err) {
		t.Fatalf("expected runtime dir removed, stat err = %v", err)
	}
}

func TestRunEventsStreamPublishesRunningAndDoneSnapshots(t *testing.T) {
	executor := &blockingExecutor{started: make(chan string, 1), release: make(chan struct{})}
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), executor)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	httpSrv := httptest.NewServer(server.Handler())
	defer httpSrv.Close()

	resultCh := make(chan error, 1)
	go func() {
		body := bytes.NewBufferString(`{"content":"please stream"}`)
		req, _ := http.NewRequest(http.MethodPost, httpSrv.URL+"/api/v1/instances/"+inst.ID+"/messages", body)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			resultCh <- err
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			resultCh <- fmt.Errorf("send status=%d", resp.StatusCode)
			return
		}
		resultCh <- nil
	}()

	select {
	case <-executor.started:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for executor start")
	}

	var runID string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := svc.ListRuns(context.Background(), principal, inst.ID, agentservice.ListRunsInput{})
		if err == nil && len(runs) > 0 {
			runID = runs[0].ID
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if runID == "" {
		t.Fatalf("expected run id")
	}

	req, err := http.NewRequest(http.MethodGet, httpSrv.URL+"/api/v1/instances/"+inst.ID+"/runs/"+runID+"/events", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("unexpected content type: %s", got)
	}

	reader := bufio.NewReader(resp.Body)
	seenRunning := false
	seenDone := false
	for !seenDone {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("ReadString: %v", err)
		}
		if strings.HasPrefix(line, "data: ") {
			var envelope struct {
				Type     string `json:"type"`
				Snapshot struct {
					Run struct {
						Status string `json:"status"`
					} `json:"run"`
					AssistantMessage *struct {
						Content string `json:"content"`
					} `json:"assistant_message,omitempty"`
				} `json:"snapshot"`
			}
			if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data: "))), &envelope); err != nil {
				t.Fatalf("unmarshal envelope: %v", err)
			}
			if envelope.Type == "snapshot" && envelope.Snapshot.Run.Status == string(agentservice.RunStatusRunning) {
				if !seenRunning {
					seenRunning = true
					close(executor.release)
				}
			}
			if envelope.Type == "done" {
				seenDone = true
				if envelope.Snapshot.Run.Status != string(agentservice.RunStatusSucceeded) {
					t.Fatalf("expected succeeded done event, got %#v", envelope)
				}
				if envelope.Snapshot.AssistantMessage == nil || envelope.Snapshot.AssistantMessage.Content != "released" {
					t.Fatalf("expected final assistant message, got %#v", envelope)
				}
			}
		}
	}
	if !seenRunning {
		t.Fatalf("expected running snapshot before done")
	}
	if err := <-resultCh; err != nil {
		t.Fatalf("send request failed: %v", err)
	}
}

func TestSkillMarketAccountEndpointReturnsValidationError(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/skill-market/account", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("skill market account status = %d body = %s", w.Code, w.Body.String())
	}
	var out map[string]string
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode skill market account error: %v", err)
	}
	if out["error"] != "email is required" {
		t.Fatalf("unexpected error payload: %#v", out)
	}
}

func TestListRunsRejectsInvalidStatus(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+inst.ID+"/runs?status=done", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body = %s", w.Code, w.Body.String())
	}
}

func TestListMessagesRejectsInvalidRole(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, agentservice.CreateSessionInput{Title: "Demo"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+inst.ID+"/sessions/"+sess.ID+"/messages?role=tool", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body = %s", w.Code, w.Body.String())
	}
}

func TestListSessionsRejectsInvalidIncludeArchived(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+inst.ID+"/sessions?include_archived=maybe", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body = %s", w.Code, w.Body.String())
	}
}

func TestListRunsRejectsInvalidResponseSource(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+inst.ID+"/runs?response_source=assistant", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminListTenantsRejectsInvalidStatus(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants?status=paused", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminListUsersRejectsInvalidStatus(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+tenant.ID+"/users?status=paused", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body = %s", w.Code, w.Body.String())
	}
}

func TestCreateMCPServerRejectsInvalidAsyncFlag(t *testing.T) {
	_, _, token, server := newMCPAuthenticatedServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers?async=maybe", strings.NewReader(`{"name":"demo","transport":"stdio","command":"demo"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body = %s", w.Code, w.Body.String())
	}
}

func TestInstallSkillRejectsInvalidAsyncFlag(t *testing.T) {
	_, _, token, server := newAsyncSkillTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/skills/install?async=maybe", strings.NewReader(`{"source":"github.com/demo/skill"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminTenantAndUserPauseResumeRoutes(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	for _, tc := range []struct {
		path   string
		status string
	}{
		{"/api/v1/admin/tenants/" + tenant.ID + "/pause", "disabled"},
		{"/api/v1/admin/tenants/" + tenant.ID + "/resume", "active"},
	} {
		req := httptest.NewRequest(http.MethodPost, tc.path, nil)
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("tenant lifecycle %s status = %d body = %s", tc.path, w.Code, w.Body.String())
		}
		var out agentservice.Tenant
		if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
			t.Fatalf("decode tenant lifecycle: %v", err)
		}
		if string(out.Status) != tc.status {
			t.Fatalf("tenant status = %s, want %s", out.Status, tc.status)
		}
	}

	for _, tc := range []struct {
		path   string
		status string
	}{
		{"/api/v1/admin/tenants/" + tenant.ID + "/users/" + user.ID + "/pause", "disabled"},
		{"/api/v1/admin/tenants/" + tenant.ID + "/users/" + user.ID + "/resume", "active"},
	} {
		req := httptest.NewRequest(http.MethodPost, tc.path, nil)
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("user lifecycle %s status = %d body = %s", tc.path, w.Code, w.Body.String())
		}
		var out agentservice.User
		if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
			t.Fatalf("decode user lifecycle: %v", err)
		}
		if string(out.Status) != tc.status {
			t.Fatalf("user status = %s, want %s", out.Status, tc.status)
		}
	}

	for _, action := range []string{"admin.tenant_paused", "admin.tenant_resumed", "admin.user_paused", "admin.user_resumed"} {
		events, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: action})
		if err != nil {
			t.Fatalf("ListAuditEvents %s: %v", action, err)
		}
		if len(events) == 0 {
			t.Fatalf("expected audit event %s", action)
		}
	}
}

func TestAdminTenantAndUserCRUDAuditRoutes(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	doAdmin := func(method, path, body string, want int) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s status = %d, want %d body = %s", method, path, w.Code, want, w.Body.String())
		}
		return w
	}

	var tenant agentservice.Tenant
	if err := json.NewDecoder(doAdmin(http.MethodPost, "/api/v1/admin/tenants", `{"name":"Audit Tenant","delete_protected":false}`, http.StatusCreated).Body).Decode(&tenant); err != nil {
		t.Fatalf("decode tenant: %v", err)
	}
	doAdmin(http.MethodPatch, "/api/v1/admin/tenants/"+tenant.ID, `{"name":"Audit Tenant Updated"}`, http.StatusOK)

	var user agentservice.User
	if err := json.NewDecoder(doAdmin(http.MethodPost, "/api/v1/admin/tenants/"+tenant.ID+"/users", `{"name":"Audit User","email":"audit@example.test"}`, http.StatusCreated).Body).Decode(&user); err != nil {
		t.Fatalf("decode user: %v", err)
	}
	doAdmin(http.MethodPatch, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID, `{"name":"Audit User Updated"}`, http.StatusOK)
	doAdmin(http.MethodDelete, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"?confirm=true", "", http.StatusOK)
	doAdmin(http.MethodDelete, "/api/v1/admin/tenants/"+tenant.ID+"?confirm=true", "", http.StatusOK)

	for _, action := range []string{"admin.tenant_created", "admin.tenant_updated", "admin.tenant_deleted", "admin.user_created", "admin.user_updated", "admin.user_deleted"} {
		events, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{TenantID: tenant.ID, Action: action})
		if err != nil {
			t.Fatalf("ListAuditEvents %s: %v", action, err)
		}
		if len(events) == 0 {
			t.Fatalf("expected tenant-scoped audit event %s", action)
		}
	}
	for _, action := range []string{"admin.user_created", "admin.user_updated", "admin.user_deleted"} {
		events, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{TenantID: tenant.ID, UserID: user.ID, Action: action})
		if err != nil {
			t.Fatalf("ListAuditEvents user scoped %s: %v", action, err)
		}
		if len(events) == 0 {
			t.Fatalf("expected user-scoped audit event %s", action)
		}
	}
}

func TestAdminSessionActorRecordedOnAdminAudit(t *testing.T) {
	t.Setenv("MACLAW_ADMIN_SETUP_TOKEN", "")
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/bootstrap/initialize", bytes.NewBufferString(`{"username":"owner","password":"owner-password-123"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/login", bytes.NewBufferString(`{"username":"owner","password":"owner-password-123"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login status = %d body = %s", w.Code, w.Body.String())
	}
	var login struct {
		Token string `json:"token"`
		Admin struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"admin"`
	}
	if err := json.NewDecoder(w.Body).Decode(&login); err != nil {
		t.Fatalf("decode login: %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants", bytes.NewBufferString(`{"name":"Session Audited Tenant"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", login.Token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create tenant status = %d body = %s", w.Code, w.Body.String())
	}

	events, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: "admin.tenant_created"})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("expected admin tenant create audit event")
	}
	got := events[0]
	if got.ActorUser != login.Admin.ID {
		t.Fatalf("actor_user = %q, want %q", got.ActorUser, login.Admin.ID)
	}
	if got.Metadata["auth_type"] != "admin_session" || got.Metadata["admin_username"] != login.Admin.Username {
		t.Fatalf("unexpected admin audit metadata: %#v", got.Metadata)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-events?action=admin.tenant_created&actor_user_id="+url.QueryEscape(login.Admin.ID), nil)
	req.Header.Set("X-MaClaw-Admin-Secret", login.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list actor audit status = %d body = %s", w.Code, w.Body.String())
	}
	var actorEvents struct {
		Items []agentservice.AuditEvent `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&actorEvents); err != nil {
		t.Fatalf("decode actor audit events: %v", err)
	}
	if len(actorEvents.Items) != 1 || actorEvents.Items[0].ActorUser != login.Admin.ID {
		t.Fatalf("unexpected actor audit events = %#v", actorEvents.Items)
	}
}
