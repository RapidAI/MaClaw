package structureddata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHTTPServerRequiresBearerTokenAndHandlesRecords(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	server := NewHTTPServer(NewService(store, "sqlite"), "test-token-0123456789012345", "test")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	webBody := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(webBody, "MaClawDataSrv MIS") || !strings.Contains(webBody, `data-testid="language-switch"`) || !strings.Contains(webBody, `data-testid="tab-overview"`) || !strings.Contains(webBody, `data-testid="setup-checklist"`) || !strings.Contains(webBody, `data-testid="overview-health"`) || !strings.Contains(webBody, `data-testid="overview-coverage"`) || !strings.Contains(webBody, `data-testid="overview-domain-readiness"`) || !strings.Contains(webBody, `data-testid="overview-capabilities"`) || !strings.Contains(webBody, `data-testid="overview-intent-results"`) || !strings.Contains(webBody, `data-testid="overview-work-queue"`) || !strings.Contains(webBody, `data-testid="overview-integration-health"`) || !strings.Contains(webBody, `data-testid="overview-access-risk"`) || !strings.Contains(webBody, `data-testid="overview-readiness"`) || !strings.Contains(webBody, `data-testid="overview-recommendations"`) || !strings.Contains(webBody, `data-testid="overview-activity"`) || !strings.Contains(webBody, `data-testid="access-workspace-summary"`) || !strings.Contains(webBody, `data-testid="access-guide-grant-analytics"`) || !strings.Contains(webBody, `data-testid="access-agent-handoff"`) || !strings.Contains(webBody, `data-testid="generate-agent-handoff"`) || !strings.Contains(webBody, `data-testid="run-agent-readiness"`) || !strings.Contains(webBody, `data-testid="agent-readiness-result"`) || !strings.Contains(webBody, `data-testid="compare-access-policy"`) || !strings.Contains(webBody, `data-testid="access-policy-diff"`) || !strings.Contains(webBody, `data-testid="access-policy-risk"`) || !strings.Contains(webBody, `data-testid="admin-accounts"`) || !strings.Contains(webBody, `data-testid="admin-sessions"`) || !strings.Contains(webBody, `data-testid="refresh-admin-sessions"`) || !strings.Contains(webBody, `data-testid="create-admin-account"`) || !strings.Contains(webBody, `data-testid="update-admin-account"`) || !strings.Contains(webBody, `data-testid="governance-evidence-summary"`) || !strings.Contains(webBody, `data-testid="governance-evidence-summary-text"`) || !strings.Contains(webBody, `data-testid="copy-evidence-summary"`) || !strings.Contains(webBody, `data-testid="access-agent-purpose"`) || !strings.Contains(webBody, `data-testid="recommend-access-policy"`) || !strings.Contains(webBody, `data-testid="access-recommendation"`) || !strings.Contains(webBody, `data-testid="generate-agent-onboarding"`) || !strings.Contains(webBody, `data-testid="agent-onboarding-checklist"`) || !strings.Contains(webBody, `data-testid="generate-agent-packet"`) || !strings.Contains(webBody, `data-testid="agent-onboarding-packet"`) || !strings.Contains(webBody, `data-testid="download-agent-packet"`) || !strings.Contains(webBody, `data-testid="export-access-review"`) || !strings.Contains(webBody, `data-testid="refresh-evidence-summary"`) || !strings.Contains(webBody, `data-testid="download-evidence-summary"`) || !strings.Contains(webBody, `data-testid="export-evidence-pack"`) || !strings.Contains(webBody, "overview-grid") || !strings.Contains(webBody, "nav-group") {
		t.Fatalf("web console status=%d body=%s", w.Code, w.Body.String())
	}
	for _, want := range []string{
		`data-testid="refresh-login-tenants"`,
		`id="tenantOptions"`,
		`withButtonBusy`,
		`Refreshing tenants`,
		`Registering Hub`,
		`Pulling tenants`,
		`Creating admin`,
		`Updating admin`,
		`Revoking`,
		`data-testid="hub-registration-state"`,
		`data-testid="hub-registration-panel"`,
		`currentAdminScope`,
		`updateAdminControlScope`,
		`authSignature`,
		`classList.remove("global-admin-mode", "tenant-admin-mode")`,
		`state.currentAdminScope !== item.admin_scope`,
		`data-testid="hub-base-url"`,
		`data-testid="save-hub-registration"`,
		`data-testid="register-hub"`,
		`data-testid="sync-tenants-from-hub"`,
		`Virtual mail`,
	} {
		if !strings.Contains(webBody, want) {
			t.Fatalf("web console missing Hub registration/login tenant control %q", want)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("openapi status=%d body=%s", w.Code, w.Body.String())
	}
	var spec map[string]any
	if err := json.NewDecoder(w.Body).Decode(&spec); err != nil {
		t.Fatalf("decode openapi: %v", err)
	}
	if spec["openapi"] != "3.0.3" {
		t.Fatalf("unexpected openapi spec: %#v", spec)
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatalf("openapi paths missing: %#v", spec)
	}
	for _, path := range []string{
		"/api/v1/setup/status",
		"/api/v1/setup/tenants/sync",
		"/api/v1/setup/admin",
		"/api/v1/login",
		"/api/v1/data/capabilities",
		"/api/v1/data/domains",
		"/api/v1/data/domains/{domain}",
		"/api/v1/data/relationships",
		"/api/v1/data/intent/resolve",
		"/api/v1/data/inbox",
		"/api/v1/data/inbox/summary",
		"/api/v1/data/stats",
		"/api/v1/data/governance/evidence-pack",
		"/api/v1/data/governance/evidence-summary.txt",
		"/api/v1/data/maintenance/run",
		"/api/v1/data/templates/bootstrap",
		"/api/v1/data/events",
		"/api/v1/data/event-contracts",
		"/api/v1/data/event-contracts/{actionId}",
		"/api/v1/data/connectors",
		"/api/v1/data/connectors/health",
		"/api/v1/data/connectors/{connectorId}",
		"/api/v1/data/connectors/{connectorId}/test",
		"/api/v1/data/connectors/{connectorId}/config/validate",
		"/api/v1/data/connectors/{connectorId}/readiness",
		"/api/v1/data/connectors/{connectorId}/health",
		"/api/v1/data/connectors/{connectorId}/sync-state",
		"/api/v1/data/connectors/{connectorId}/sync-runs",
		"/api/v1/data/connectors/{connectorId}/sync-plan",
		"/api/v1/data/connectors/{connectorId}/sync-batch",
		"/api/v1/data/connectors/{connectorId}/config/patch",
		"/api/v1/data/connectors/{connectorId}/mappings/suggest",
		"/api/v1/data/connectors/{connectorId}/events/preview",
		"/api/v1/data/connectors/{connectorId}/events",
		"/api/v1/data/business-rules",
		"/api/v1/data/business-rules/evaluate",
		"/api/v1/data/audit",
		"/api/v1/data/audit/export.csv",
		"/api/v1/data/views",
		"/api/v1/data/views/{viewId}",
		"/api/v1/data/views/{viewId}/query",
		"/api/v1/data/dashboards",
		"/api/v1/data/dashboards/{dashboardId}",
		"/api/v1/data/dashboards/{dashboardId}/run",
		"/api/v1/data/quality-checks",
		"/api/v1/data/import-jobs",
		"/api/v1/data/import-jobs/{jobId}",
		"/api/v1/data/export-jobs",
		"/api/v1/data/export-jobs/{jobId}",
		"/api/v1/data/export-jobs/{jobId}/download",
		"/api/v1/data/operation-plans",
		"/api/v1/data/operation-plans/{planId}",
		"/api/v1/data/operation-plans/{planId}/review",
		"/api/v1/data/operation-plans/{planId}/apply",
		"/api/v1/data/operation-plans/{planId}/cancel",
		"/api/v1/data/approvals",
		"/api/v1/data/approvals/{approvalId}",
		"/api/v1/data/approvals/{approvalId}/review",
		"/api/v1/data/backups/{backupId}",
		"/api/v1/data/backups/{backupId}/download",
		"/api/v1/data/events/dead-letter",
		"/api/v1/data/events/dead-letter/{deadLetterId}",
		"/api/v1/data/events/dead-letter/{deadLetterId}/retry",
		"/api/v1/data/events/dead-letter/{deadLetterId}/resolve",
		"/api/v1/data/datasets/{datasetId}/quality/run",
		"/api/v1/data/datasets/{datasetId}/quality/runs",
		"/api/v1/data/datasets/{datasetId}/quality/runs/{runId}",
		"/api/v1/data/datasets/{datasetId}/schema-proposals",
		"/api/v1/data/datasets/{datasetId}/schema-proposals/{proposalId}",
		"/api/v1/data/datasets/{datasetId}/records/validate",
		"/api/v1/data/datasets/{datasetId}/records/batch",
		"/api/v1/data/datasets/{datasetId}/records/bulk-update",
		"/api/v1/data/datasets/{datasetId}/records/bulk-delete",
		"/api/v1/data/datasets/{datasetId}/records/{recordId}/restore",
		"/api/v1/data/datasets/{datasetId}/records/batch/jobs",
		"/api/v1/data/datasets/{datasetId}/records/import-template.csv",
		"/api/v1/data/datasets/{datasetId}/records/import.csv",
		"/api/v1/data/datasets/{datasetId}/records/import.csv/jobs",
		"/api/v1/data/datasets/{datasetId}/records/import.jsonl",
		"/api/v1/data/datasets/{datasetId}/records/import.jsonl/jobs",
		"/api/v1/data/datasets/{datasetId}/records/export.csv",
		"/api/v1/data/datasets/{datasetId}/records/export.jsonl",
		"/api/v1/data/datasets/{datasetId}/records/export.csv/jobs",
		"/api/v1/data/datasets/{datasetId}/records/export.jsonl/jobs",
		"/api/v1/data/datasets/{datasetId}/records/{recordId}/approvals",
		"/api/v1/data/datasets/{datasetId}/records/{recordId}/related",
		"/api/v1/data/datasets/{datasetId}/records/{recordId}/revisions",
		"/api/v1/data/datasets/{datasetId}/records/{recordId}/timeline",
	} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("openapi path %s missing", path)
		}
	}
	for _, route := range []struct {
		path   string
		method string
	}{
		{"/api/v1/data/admin/accounts", "get"},
		{"/api/v1/data/admin/accounts/{username}", "patch"},
		{"/api/v1/data/admin/sessions", "get"},
		{"/api/v1/data/admin/sessions/{sessionId}", "patch"},
		{"/api/v1/data/admin/sessions/{sessionId}", "delete"},
	} {
		if !openAPIOperationHasQueryParam(paths, route.path, route.method, "tenant") {
			t.Fatalf("openapi %s %s missing tenant query parameter: %#v", route.method, route.path, paths[route.path])
		}
	}
	specJSON, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal openapi spec: %v", err)
	}
	if !strings.Contains(string(specJSON), "summary_text") || !strings.Contains(string(specJSON), "evidence_sha256") || !strings.Contains(string(specJSON), "X-MaClaw-Evidence-ID") || !strings.Contains(string(specJSON), "X-MaClaw-Evidence-SHA256") || !strings.Contains(string(specJSON), "Export governance evidence pack") || !strings.Contains(string(specJSON), "Export governance evidence summary text") || !strings.Contains(string(specJSON), `"lang"`) {
		t.Fatalf("openapi governance evidence schema missing summary text: %s", string(specJSON))
	}
	for _, tc := range []struct {
		path   string
		method string
		status string
		field  string
	}{
		{path: "/api/v1/setup/status", method: "get", status: "200", field: "initialized"},
		{path: "/api/v1/setup/status", method: "get", status: "200", field: "mode"},
		{path: "/api/v1/data/admin/tenants", method: "get", status: "200", field: "items"},
		{path: "/api/v1/data/admin/tenants/sync", method: "post", status: "200", field: "tenants"},
		{path: "/api/v1/data/admin/hub-registration", method: "get", status: "200", field: "status"},
		{path: "/api/v1/data/admin/hub-registration", method: "post", status: "200", field: "status"},
		{path: "/api/v1/data/admin/hub-registration/register", method: "post", status: "200", field: "status"},
		{path: "/api/v1/data/admin/hub-registration/sync-tenants", method: "post", status: "200", field: "tenants"},
		{path: "/api/v1/setup/admin", method: "post", status: "201", field: "token"},
		{path: "/api/v1/setup/admin", method: "post", status: "201", field: "expires_at"},
		{path: "/api/v1/login", method: "post", status: "200", field: "token"},
		{path: "/api/v1/login", method: "post", status: "200", field: "expires_at"},
	} {
		if !openAPIOperationResponseHasProperty(paths, tc.path, tc.method, tc.status, tc.field) {
			t.Fatalf("openapi %s %s response %s missing %s: %#v", tc.method, tc.path, tc.status, tc.field, paths[tc.path])
		}
	}
	for _, path := range []string{
		"/api/v1/data/access/presets",
		"/api/v1/data/access/api-keys",
		"/api/v1/data/domains",
		"/api/v1/data/relationships",
		"/api/v1/data/templates",
		"/api/v1/data/backups",
		"/api/v1/data/events/dead-letter",
		"/api/v1/data/events",
		"/api/v1/data/event-contracts",
		"/api/v1/data/connectors",
		"/api/v1/data/connectors/health",
		"/api/v1/data/connectors/{connectorId}/sync-runs",
		"/api/v1/data/business-actions",
		"/api/v1/data/business-rules",
		"/api/v1/data/views",
		"/api/v1/data/dashboards",
		"/api/v1/data/reports",
		"/api/v1/data/quality-checks",
		"/api/v1/data/audit",
		"/api/v1/data/import-jobs",
		"/api/v1/data/export-jobs",
		"/api/v1/data/operation-plans",
		"/api/v1/data/approvals",
		"/api/v1/data/datasets",
		"/api/v1/data/datasets/{datasetId}/fields",
		"/api/v1/data/datasets/{datasetId}/schema-proposals",
		"/api/v1/data/datasets/{datasetId}/quality/runs",
		"/api/v1/data/datasets/{datasetId}/records",
		"/api/v1/data/datasets/{datasetId}/records/{recordId}/revisions",
	} {
		if !openAPIGetHasQueryParam(paths, path, "limit") || !openAPIGetHasQueryParam(paths, path, "before") || !openAPIGetHasQueryParam(paths, path, "before_id") {
			t.Fatalf("openapi path %s missing cursor pagination parameters: %#v", path, paths[path])
		}
		if !openAPIGetHasListResponseSchema(paths, path) {
			t.Fatalf("openapi path %s missing ListResponse response schema: %#v", path, paths[path])
		}
	}
	if !openAPIPostHasListResponseSchema(paths, "/api/v1/data/datasets/{datasetId}/records/query") {
		t.Fatalf("openapi records query path missing ListResponse response schema: %#v", paths["/api/v1/data/datasets/{datasetId}/records/query"])
	}
	for _, name := range []string{"q", "tag", "filter", "sort", "limit", "before", "before_id"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/datasets/{datasetId}/records/query", name) {
			t.Fatalf("openapi records query body missing %s: %#v", name, paths["/api/v1/data/datasets/{datasetId}/records/query"])
		}
	}
	for _, name := range []string{"template_ids", "domains", "skip_existing", "dry_run"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/templates/bootstrap", name) {
			t.Fatalf("openapi bootstrap templates body missing %s: %#v", name, paths["/api/v1/data/templates/bootstrap"])
		}
	}
	for _, name := range []string{"id", "domain", "name", "title", "description"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/templates/{templateId}/create", name) {
			t.Fatalf("openapi create from template body missing %s: %#v", name, paths["/api/v1/data/templates/{templateId}/create"])
		}
	}
	for _, name := range []string{"query", "domain", "limit"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/intent/resolve", name) {
			t.Fatalf("openapi intent resolve body missing %s: %#v", name, paths["/api/v1/data/intent/resolve"])
		}
	}
	for _, name := range []string{"record_id", "idempotency_key", "title", "tags", "data", "occurred_at", "dry_run"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/business-actions/{actionId}/execute", name) {
			t.Fatalf("openapi execute business action body missing %s: %#v", name, paths["/api/v1/data/business-actions/{actionId}/execute"])
		}
	}
	for _, name := range []string{"domain", "dataset_id", "business_action_id", "record_id", "dry_run", "data"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/business-rules/evaluate", name) {
			t.Fatalf("openapi business rule evaluate body missing %s: %#v", name, paths["/api/v1/data/business-rules/evaluate"])
		}
	}
	for _, name := range []string{"filter", "group_by", "metrics", "sort", "limit", "scan_limit"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/reports/{reportId}/run", name) {
			t.Fatalf("openapi report run body missing %s: %#v", name, paths["/api/v1/data/reports/{reportId}/run"])
		}
	}
	for _, name := range []string{"source", "event_type", "operation", "business_action_id", "dataset_id", "record_id", "idempotency_key", "title", "tags", "data", "occurred_at", "dry_run"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/events", name) {
			t.Fatalf("openapi ingest event body missing %s: %#v", name, paths["/api/v1/data/events"])
		}
	}
	for _, name := range []string{"resolution"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/events/dead-letter/{deadLetterId}/resolve", name) {
			t.Fatalf("openapi dead letter resolve body missing %s: %#v", name, paths["/api/v1/data/events/dead-letter/{deadLetterId}/resolve"])
		}
	}
	for _, path := range []string{"/api/v1/data/connectors", "/api/v1/data/connectors/{connectorId}"} {
		for _, name := range []string{"id", "domain", "name", "kind", "base_url", "auth_type", "token_ref", "enabled", "subscribed_actions", "config"} {
			if !openAPIRequestBodyHasProperty(paths, path, connectorUpsertMethod(path), name) {
				t.Fatalf("openapi connector upsert body missing %s for %s: %#v", name, path, paths[path])
			}
		}
	}
	for _, name := range []string{"sample_event"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/connectors/{connectorId}/readiness", name) {
			t.Fatalf("openapi connector readiness body missing %s: %#v", name, paths["/api/v1/data/connectors/{connectorId}/readiness"])
		}
	}
	for _, name := range []string{"status", "cursor", "checkpoint", "last_error", "message", "synced_records", "started_at", "finished_at"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/connectors/{connectorId}/sync-state", name) {
			t.Fatalf("openapi connector sync state body missing %s: %#v", name, paths["/api/v1/data/connectors/{connectorId}/sync-state"])
		}
	}
	for _, name := range []string{"sample_event", "first_page_events", "page_size", "cursor"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/connectors/{connectorId}/sync-plan", name) {
			t.Fatalf("openapi connector sync plan body missing %s: %#v", name, paths["/api/v1/data/connectors/{connectorId}/sync-plan"])
		}
	}
	for _, name := range []string{"events", "dry_run", "stop_on_error", "sync_state"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/connectors/{connectorId}/sync-batch", name) {
			t.Fatalf("openapi connector sync batch body missing %s: %#v", name, paths["/api/v1/data/connectors/{connectorId}/sync-batch"])
		}
	}
	for _, name := range []string{"patch", "dry_run"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/connectors/{connectorId}/config/patch", name) {
			t.Fatalf("openapi connector config patch body missing %s: %#v", name, paths["/api/v1/data/connectors/{connectorId}/config/patch"])
		}
	}
	for _, name := range []string{"business_action_id", "sample_data"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/connectors/{connectorId}/mappings/suggest", name) {
			t.Fatalf("openapi connector mapping suggest body missing %s: %#v", name, paths["/api/v1/data/connectors/{connectorId}/mappings/suggest"])
		}
	}
	for _, path := range []string{"/api/v1/data/connectors/{connectorId}/events/preview", "/api/v1/data/connectors/{connectorId}/events"} {
		for _, name := range []string{"source", "event_type", "operation", "business_action_id", "dataset_id", "record_id", "idempotency_key", "title", "tags", "data", "occurred_at", "dry_run"} {
			if !openAPIPostRequestBodyHasProperty(paths, path, name) {
				t.Fatalf("openapi connector event body missing %s for %s: %#v", name, path, paths[path])
			}
		}
	}
	for _, path := range []string{"/api/v1/data/datasets/{datasetId}/records/batch", "/api/v1/data/datasets/{datasetId}/records/batch/jobs"} {
		for _, name := range []string{"records", "dry_run"} {
			if !openAPIPostRequestBodyHasProperty(paths, path, name) {
				t.Fatalf("openapi batch import body missing %s for %s: %#v", name, path, paths[path])
			}
		}
		prop := openAPIRequestBodyProperty(paths, path, "post", "records")
		if got := openAPINumericValue(prop["maxItems"]); got != maxBatchImportRecords {
			t.Fatalf("openapi batch import maxItems=%#v for %s, want %d", prop["maxItems"], path, maxBatchImportRecords)
		}
	}
	for _, name := range []string{"query", "set", "unset", "title", "tags", "limit", "dry_run", "confirm", "reason"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/datasets/{datasetId}/records/bulk-update", name) {
			t.Fatalf("openapi bulk update body missing %s: %#v", name, paths["/api/v1/data/datasets/{datasetId}/records/bulk-update"])
		}
	}
	for _, name := range []string{"query", "limit", "dry_run", "confirm", "reason"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/datasets/{datasetId}/records/bulk-delete", name) {
			t.Fatalf("openapi bulk delete body missing %s: %#v", name, paths["/api/v1/data/datasets/{datasetId}/records/bulk-delete"])
		}
	}
	for _, name := range []string{"id", "domain", "name", "title", "description"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/datasets", name) {
			t.Fatalf("openapi create dataset body missing %s: %#v", name, paths["/api/v1/data/datasets"])
		}
	}
	for _, name := range []string{"title", "description"} {
		if !openAPIPatchRequestBodyHasProperty(paths, "/api/v1/data/datasets/{datasetId}", name) {
			t.Fatalf("openapi update dataset body missing %s: %#v", name, paths["/api/v1/data/datasets/{datasetId}"])
		}
	}
	for _, name := range []string{"fields"} {
		if !openAPIRequestBodyHasProperty(paths, "/api/v1/data/datasets/{datasetId}/fields", "put", name) {
			t.Fatalf("openapi upsert fields body missing %s: %#v", name, paths["/api/v1/data/datasets/{datasetId}/fields"])
		}
	}
	for _, name := range []string{"id", "title", "tags", "data", "source_id"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/datasets/{datasetId}/records", name) {
			t.Fatalf("openapi create record body missing %s: %#v", name, paths["/api/v1/data/datasets/{datasetId}/records"])
		}
	}
	for _, name := range []string{"title", "tags", "data"} {
		if !openAPIPatchRequestBodyHasProperty(paths, "/api/v1/data/datasets/{datasetId}/records/{recordId}", name) {
			t.Fatalf("openapi update record body missing %s: %#v", name, paths["/api/v1/data/datasets/{datasetId}/records/{recordId}"])
		}
	}
	for _, name := range []string{"data"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/datasets/{datasetId}/records/validate", name) {
			t.Fatalf("openapi validate record body missing %s: %#v", name, paths["/api/v1/data/datasets/{datasetId}/records/validate"])
		}
	}
	for _, name := range []string{"confirm", "revision_id", "reason"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/datasets/{datasetId}/records/{recordId}/restore", name) {
			t.Fatalf("openapi restore record body missing %s: %#v", name, paths["/api/v1/data/datasets/{datasetId}/records/{recordId}/restore"])
		}
	}
	for _, path := range []string{"/api/v1/data/datasets/{datasetId}/records/import.csv", "/api/v1/data/datasets/{datasetId}/records/import.csv/jobs"} {
		for _, name := range []string{"csv", "dry_run"} {
			if !openAPIPostRequestBodyHasProperty(paths, path, name) {
				t.Fatalf("openapi csv import body missing %s for %s: %#v", name, path, paths[path])
			}
		}
	}
	for _, path := range []string{"/api/v1/data/datasets/{datasetId}/records/import.jsonl", "/api/v1/data/datasets/{datasetId}/records/import.jsonl/jobs"} {
		for _, name := range []string{"jsonl", "dry_run"} {
			if !openAPIPostRequestBodyHasProperty(paths, path, name) {
				t.Fatalf("openapi jsonl import body missing %s for %s: %#v", name, path, paths[path])
			}
		}
	}
	for _, name := range []string{"sample_data", "reason"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/datasets/{datasetId}/schema-proposals", name) {
			t.Fatalf("openapi schema proposal body missing %s: %#v", name, paths["/api/v1/data/datasets/{datasetId}/schema-proposals"])
		}
	}
	for _, name := range []string{"proposal_id", "fields", "confirm", "reason"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/datasets/{datasetId}/schema-proposals/apply", name) {
			t.Fatalf("openapi apply schema proposal body missing %s: %#v", name, paths["/api/v1/data/datasets/{datasetId}/schema-proposals/apply"])
		}
	}
	for _, name := range []string{"filter", "group_by", "metrics", "sort", "limit", "scan_limit"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/datasets/{datasetId}/aggregate", name) {
			t.Fatalf("openapi aggregate body missing %s: %#v", name, paths["/api/v1/data/datasets/{datasetId}/aggregate"])
		}
	}
	for _, name := range []string{"checks", "limit", "include_warnings"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/datasets/{datasetId}/quality/run", name) {
			t.Fatalf("openapi quality run body missing %s: %#v", name, paths["/api/v1/data/datasets/{datasetId}/quality/run"])
		}
	}
	for _, path := range []string{"/api/v1/data/datasets/{datasetId}/records/export.csv", "/api/v1/data/datasets/{datasetId}/records/export.jsonl", "/api/v1/data/datasets/{datasetId}/records/export.csv/jobs", "/api/v1/data/datasets/{datasetId}/records/export.jsonl/jobs"} {
		for _, name := range []string{"q", "tag", "filter", "sort", "limit", "before", "before_id"} {
			if !openAPIPostRequestBodyHasProperty(paths, path, name) {
				t.Fatalf("openapi export body missing %s for %s: %#v", name, path, paths[path])
			}
		}
	}
	for path, wantMax := range map[string]int{
		"/api/v1/data/datasets/{datasetId}/records/query":             500,
		"/api/v1/data/views/{viewId}/query":                           500,
		"/api/v1/data/datasets/{datasetId}/records/export.csv":        5000,
		"/api/v1/data/datasets/{datasetId}/records/export.jsonl":      5000,
		"/api/v1/data/datasets/{datasetId}/records/export.csv/jobs":   50000,
		"/api/v1/data/datasets/{datasetId}/records/export.jsonl/jobs": 50000,
	} {
		prop := openAPIRequestBodyProperty(paths, path, "post", "limit")
		if got := openAPINumericValue(prop["maximum"]); got != wantMax {
			t.Fatalf("openapi %s limit maximum=%#v, want %d", path, prop["maximum"], wantMax)
		}
	}
	for _, name := range []string{"key_id", "resource_type", "resource_id"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/access/check", name) {
			t.Fatalf("openapi access check body missing %s: %#v", name, paths["/api/v1/data/access/check"])
		}
	}
	for _, name := range []string{"id", "key", "user_id", "role", "allowed_domains", "allowed_datasets", "allowed_actions", "allowed_views", "allowed_reports", "allowed_dashboards", "allow_raw_data", "allow_sensitive", "allow_admin", "note", "expires_at"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/access/api-keys", name) {
			t.Fatalf("openapi create api key body missing %s: %#v", name, paths["/api/v1/data/access/api-keys"])
		}
	}
	for _, name := range []string{"user_id", "role", "enabled", "allowed_domains", "allowed_datasets", "allowed_actions", "allowed_views", "allowed_reports", "allowed_dashboards", "allow_raw_data", "allow_sensitive", "allow_admin", "note", "expires_at"} {
		if !openAPIPatchRequestBodyHasProperty(paths, "/api/v1/data/access/api-keys/{keyId}", name) {
			t.Fatalf("openapi update api key body missing %s: %#v", name, paths["/api/v1/data/access/api-keys/{keyId}"])
		}
	}
	for _, name := range []string{"tasks"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/maintenance/run", name) {
			t.Fatalf("openapi maintenance body missing %s: %#v", name, paths["/api/v1/data/maintenance/run"])
		}
	}
	for _, name := range []string{"name", "note"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/backups", name) {
			t.Fatalf("openapi create backup body missing %s: %#v", name, paths["/api/v1/data/backups"])
		}
	}
	for _, name := range []string{"confirm", "reason"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/backups/{backupId}/restore", name) {
			t.Fatalf("openapi restore backup body missing %s: %#v", name, paths["/api/v1/data/backups/{backupId}/restore"])
		}
	}
	for _, name := range []string{"dataset_id", "operation", "summary", "risk_level", "request"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/operation-plans", name) {
			t.Fatalf("openapi create operation plan body missing %s: %#v", name, paths["/api/v1/data/operation-plans"])
		}
	}
	for _, path := range []string{"/api/v1/data/operation-plans/{planId}/review", "/api/v1/data/approvals/{approvalId}/review"} {
		for _, name := range []string{"decision", "reason"} {
			if !openAPIPostRequestBodyHasProperty(paths, path, name) {
				t.Fatalf("openapi review body missing %s for %s: %#v", name, path, paths[path])
			}
		}
	}
	for _, name := range []string{"workflow_node_id", "workflow_decision_id", "business_status", "result_status", "result_payload", "outputs", "artifacts"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/approvals/{approvalId}/review", name) {
			t.Fatalf("openapi approval review body missing %s: %#v", name, paths["/api/v1/data/approvals/{approvalId}/review"])
		}
	}
	for _, name := range []string{"confirm", "reason"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/operation-plans/{planId}/apply", name) {
			t.Fatalf("openapi apply operation plan body missing %s: %#v", name, paths["/api/v1/data/operation-plans/{planId}/apply"])
		}
	}
	for _, name := range []string{"kind", "priority", "summary", "request", "assigned_to", "due_at", "workflow_skill_id", "workflow_version", "workflow_instance_id", "workflow_node_id", "workflow_decision_id", "business_status", "result_status", "result_payload", "outputs", "artifacts"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/datasets/{datasetId}/records/{recordId}/approvals", name) {
			t.Fatalf("openapi create approval body missing %s: %#v", name, paths["/api/v1/data/datasets/{datasetId}/records/{recordId}/approvals"])
		}
	}
	if !openAPIPostHasBusinessViewResponseSchema(paths, "/api/v1/data/views/{viewId}/query") {
		t.Fatalf("openapi business view query path missing cursor response schema: %#v", paths["/api/v1/data/views/{viewId}/query"])
	}
	for _, name := range []string{"q", "tag", "filter", "sort", "limit", "before", "before_id"} {
		if !openAPIPostRequestBodyHasProperty(paths, "/api/v1/data/views/{viewId}/query", name) {
			t.Fatalf("openapi business view query body missing %s: %#v", name, paths["/api/v1/data/views/{viewId}/query"])
		}
	}
	assertOpenAPIMutatingRequestBodies(t, paths)
	assertOpenAPIDataOperationsHaveAuthErrors(t, paths)
	assertOpenAPIDownloadOperationsHaveHeaders(t, paths)
	if !openAPIGetHasQueryParam(paths, "/api/v1/data/datasets/{datasetId}/records/{recordId}/related", "limit") || !openAPIGetHasQueryParam(paths, "/api/v1/data/datasets/{datasetId}/records/{recordId}/related", "before_id") {
		t.Fatalf("openapi related records path missing cursor pagination parameters: %#v", paths["/api/v1/data/datasets/{datasetId}/records/{recordId}/related"])
	}
	if !openAPIGetHasRelatedRecordsSchema(paths, "/api/v1/data/datasets/{datasetId}/records/{recordId}/related") {
		t.Fatalf("openapi related records path missing response schema: %#v", paths["/api/v1/data/datasets/{datasetId}/records/{recordId}/related"])
	}
	for _, path := range []string{"/api/v1/data/inbox", "/api/v1/data/datasets/{datasetId}/records/{recordId}/timeline"} {
		if !openAPIGetHasQueryParam(paths, path, "limit") || !openAPIGetHasQueryParam(paths, path, "before") || !openAPIGetHasQueryParam(paths, path, "before_id") {
			t.Fatalf("openapi custom cursor path %s missing pagination parameters: %#v", path, paths[path])
		}
	}
	for path, names := range map[string][]string{
		"/api/v1/data/access/api-keys":                       {"q", "status", "enabled"},
		"/api/v1/data/business-rules":                        {"domain", "dataset_id", "business_action_id", "severity"},
		"/api/v1/data/relationships":                         {"dataset_id"},
		"/api/v1/data/inbox":                                 {"dataset_id", "type", "status", "include_ok"},
		"/api/v1/data/event-contracts":                       {"domain"},
		"/api/v1/data/connectors":                            {"domain", "kind", "enabled"},
		"/api/v1/data/connectors/health":                     {"domain", "kind", "enabled"},
		"/api/v1/data/events":                                {"dataset_id", "record_id", "source", "event_type", "business_action_id", "idempotency_key"},
		"/api/v1/data/events/dead-letter":                    {"status", "source", "event_type", "business_action_id", "dataset_id", "record_id", "idempotency_key"},
		"/api/v1/data/audit":                                 {"dataset_id", "action", "user_id", "target_type", "target_id", "q"},
		"/api/v1/data/import-jobs":                           {"dataset_id", "status"},
		"/api/v1/data/export-jobs":                           {"dataset_id", "status"},
		"/api/v1/data/operation-plans":                       {"dataset_id", "operation", "status"},
		"/api/v1/data/approvals":                             {"dataset_id", "record_id", "status", "kind", "workflow_skill_id", "workflow_instance_id", "business_status", "result_status", "assigned_to", "overdue"},
		"/api/v1/data/datasets/{datasetId}/schema-proposals": {"status"},
		"/api/v1/data/datasets/{datasetId}/records":          {"q", "tag"},
	} {
		for _, name := range names {
			if !openAPIGetHasQueryParam(paths, path, name) {
				t.Fatalf("openapi path %s missing filter query param %s: %#v", path, name, paths[path])
			}
		}
	}
	for path, names := range map[string][]string{
		"/api/v1/data/access/api-keys":   {"enabled"},
		"/api/v1/data/inbox":             {"include_ok"},
		"/api/v1/data/connectors":        {"enabled"},
		"/api/v1/data/connectors/health": {"enabled"},
		"/api/v1/data/approvals":         {"overdue"},
	} {
		for _, name := range names {
			if !openAPIGetQueryParamHasType(paths, path, name, "boolean") {
				t.Fatalf("openapi path %s query param %s should be boolean: %#v", path, name, paths[path])
			}
		}
	}
	for path, names := range map[string][]string{
		"/api/v1/data/access/api-keys":                       {"q", "status"},
		"/api/v1/data/business-rules":                        {"domain", "dataset_id", "business_action_id", "severity"},
		"/api/v1/data/event-contracts":                       {"domain"},
		"/api/v1/data/connectors":                            {"domain", "kind"},
		"/api/v1/data/connectors/health":                     {"domain", "kind"},
		"/api/v1/data/datasets/{datasetId}/schema-proposals": {"status"},
		"/api/v1/data/datasets/{datasetId}/records":          {"q", "tag"},
	} {
		for _, name := range names {
			if !openAPIGetQueryParamHasType(paths, path, name, "string") {
				t.Fatalf("openapi path %s query param %s should be string: %#v", path, name, paths[path])
			}
		}
	}
	if !openAPIGetHasInboxSchema(paths, "/api/v1/data/inbox") {
		t.Fatalf("openapi inbox path missing response schema: %#v", paths["/api/v1/data/inbox"])
	}
	if !openAPIGetHasTimelineSchema(paths, "/api/v1/data/datasets/{datasetId}/records/{recordId}/timeline") {
		t.Fatalf("openapi timeline path missing response schema: %#v", paths["/api/v1/data/datasets/{datasetId}/records/{recordId}/timeline"])
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets", nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("WWW-Authenticate"); got != `Bearer realm="MaClawDataSrv"` {
		t.Fatalf("expected bearer challenge header, got %q", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected JSON nosniff header, got %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/governance/evidence-pack?min_severity=medium", nil)
	authRole(req, "data_user")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected data_user governance evidence pack forbidden, status=%d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("forbidden response should be JSON, got content-type %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/governance/evidence-pack?min_severity=medium", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("governance evidence pack status=%d body=%s", w.Code, w.Body.String())
	}
	var evidence GovernanceEvidencePack
	if err := json.NewDecoder(w.Body).Decode(&evidence); err != nil {
		t.Fatalf("decode governance evidence pack: %v", err)
	}
	if w.Header().Get("X-MaClaw-Evidence-ID") != evidence.EvidenceID || w.Header().Get("X-MaClaw-Evidence-SHA256") != evidence.EvidenceSHA256 {
		t.Fatalf("governance evidence headers mismatch: id=%q sha=%q evidence=%#v", w.Header().Get("X-MaClaw-Evidence-ID"), w.Header().Get("X-MaClaw-Evidence-SHA256"), evidence)
	}
	if evidence.TenantID != "tenant_1" || evidence.EvidenceID == "" || len(evidence.EvidenceSHA256) != 64 || !strings.Contains(evidence.SummaryText, evidence.EvidenceID) || evidence.Summary.Status == "" || evidence.Summary.RiskLevel == "" || !strings.Contains(evidence.SummaryText, "Status:") || !strings.Contains(evidence.SummaryText, "Controls:") || len(evidence.Summary.Recommendations) == 0 || len(evidence.Summary.Controls) < 5 || evidence.Summary.SectionCount < 6 || evidence.Summary.OKSections < 6 || len(evidence.Sections) < 6 || !containsEvidenceSection(evidence.Sections, "service_stats") || !containsEvidenceSection(evidence.Sections, "access_review") || !containsEvidenceSection(evidence.Sections, "recent_audit") || !containsGovernanceControl(evidence.Summary.Controls, "recovery_backup") || !containsGovernanceControl(evidence.Summary.Controls, "scoped_access") || !containsGovernanceControlAction(evidence.Summary.Controls, "scoped_access", "access") {
		t.Fatalf("unexpected governance evidence pack: %#v", evidence)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/governance/evidence-pack?min_severity=medium&lang=zh", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("zh governance evidence pack status=%d body=%s", w.Code, w.Body.String())
	}
	var zhEvidence GovernanceEvidencePack
	if err := json.NewDecoder(w.Body).Decode(&zhEvidence); err != nil {
		t.Fatalf("decode zh governance evidence pack: %v", err)
	}
	if zhEvidence.EvidenceID == "" || len(zhEvidence.EvidenceSHA256) != 64 || !strings.Contains(zhEvidence.SummaryText, "\u6cbb\u7406\u8bc1\u636e\u6458\u8981") || !strings.Contains(zhEvidence.SummaryText, "\u8bc1\u636e ID:") || !strings.Contains(zhEvidence.SummaryText, "\u72b6\u6001:") || !strings.Contains(zhEvidence.SummaryText, "\u63a7\u5236\u9879:") {
		t.Fatalf("unexpected zh governance evidence summary: %s", zhEvidence.SummaryText)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/governance/evidence-summary.txt?min_severity=medium&lang=zh", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "text/plain") || w.Header().Get("X-MaClaw-Evidence-ID") == "" || len(w.Header().Get("X-MaClaw-Evidence-SHA256")) != 64 || !strings.Contains(w.Header().Get("Content-Disposition"), w.Header().Get("X-MaClaw-Evidence-ID")) || !strings.Contains(w.Body.String(), w.Header().Get("X-MaClaw-Evidence-ID")) || !strings.Contains(w.Body.String(), "\u6cbb\u7406\u8bc1\u636e\u6458\u8981") || !strings.Contains(w.Body.String(), "\u8bc1\u636e SHA256:") || !strings.Contains(w.Body.String(), "\u72b6\u6001:") {
		t.Fatalf("unexpected governance evidence summary text status=%d content-type=%s body=%s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected evidence summary nosniff header, got %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/templates", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list templates status=%d body=%s", w.Code, w.Body.String())
	}
	var templatesResp ListResponse[DatasetTemplate]
	if err := json.NewDecoder(w.Body).Decode(&templatesResp); err != nil {
		t.Fatalf("decode templates: %v", err)
	}
	templates := templatesResp.Items
	if len(templates) == 0 {
		t.Fatalf("expected templates")
	}
	for _, id := range []string{"procurement.purchase_orders", "inventory.items", "assets.fixed_assets"} {
		if !containsTemplate(templates, id) {
			t.Fatalf("expected enterprise template %s in %#v", id, templates)
		}
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/templates?limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first template cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstTemplatePage ListResponse[DatasetTemplate]
	if err := json.NewDecoder(w.Body).Decode(&firstTemplatePage); err != nil {
		t.Fatalf("decode first template cursor page: %v", err)
	}
	if len(firstTemplatePage.Items) != 1 || !firstTemplatePage.HasMore || firstTemplatePage.NextBeforeID == "" {
		t.Fatalf("unexpected first template cursor page: %#v", firstTemplatePage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/templates?limit=1&before_id="+url.QueryEscape(firstTemplatePage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second template cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondTemplatePage ListResponse[DatasetTemplate]
	if err := json.NewDecoder(w.Body).Decode(&secondTemplatePage); err != nil {
		t.Fatalf("decode second template cursor page: %v", err)
	}
	if len(secondTemplatePage.Items) == 0 || secondTemplatePage.Items[0].ID == firstTemplatePage.Items[0].ID {
		t.Fatalf("unexpected second template cursor page: first=%#v second=%#v", firstTemplatePage, secondTemplatePage)
	}

	body := bytes.NewBufferString(`{"id":"hr.staff","name":"staff","title":"Staff"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/templates/hr.employees/create", body)
	authRole(req, "data_user")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected data_user template create forbidden, status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"id":"hr.staff","name":"staff","title":"Staff"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/templates/hr.employees/create", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create template dataset status=%d body=%s", w.Code, w.Body.String())
	}
	var templated CreateFromTemplateResult
	if err := json.NewDecoder(w.Body).Decode(&templated); err != nil {
		t.Fatalf("decode template result: %v", err)
	}
	if templated.Dataset == nil || templated.Dataset.ID != "hr.staff" || len(templated.Fields) == 0 {
		t.Fatalf("unexpected template result: %#v", templated)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/capabilities", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("capabilities status=%d body=%s", w.Code, w.Body.String())
	}
	var capabilities DataCapabilities
	if err := json.NewDecoder(w.Body).Decode(&capabilities); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if capabilities.Service != "MaClawDataSrv" || !containsString(capabilities.Domains, "hr") || !containsString(capabilities.Domains, "procurement") || !containsString(capabilities.Domains, "inventory") || !containsString(capabilities.Domains, "assets") || len(capabilities.BusinessActions) == 0 || len(capabilities.Dashboards) == 0 || len(capabilities.Reports) == 0 {
		t.Fatalf("unexpected capabilities: %#v", capabilities)
	}
	if capabilities.Access.AuthenticatedBy == "" || !capabilities.Access.BusinessOperationFirst || capabilities.Access.VisibleCounts["business_actions"] == 0 || !containsString(capabilities.Access.RecommendedNextActions, "resolve_intent") || !containsString(capabilities.Access.Guardrails, "Prefer resolve_intent, business actions, business views, dashboards, reports, and aggregate APIs.") {
		t.Fatalf("expected agent access summary in capabilities: %#v", capabilities.Access)
	}
	if !containsAgentPlaybookToolCall(capabilities.AgentPlaybooks, "inventory.stock_update", "mis_data", "inventory.stock_update") {
		t.Fatalf("expected inventory stock update agent playbook with mis_data template: %#v", capabilities.AgentPlaybooks)
	}
	if !containsRelationship(capabilities.Relationships, "sales.orders", "customer_ref", "sales.customers") {
		t.Fatalf("expected sales order customer relationship in capabilities: %#v", capabilities.Relationships)
	}
	if !containsRelationship(capabilities.Relationships, "sales.orders", "opportunity_ref", "sales.opportunities") || !containsRelationship(capabilities.Relationships, "sales.opportunities", "customer_ref", "sales.customers") {
		t.Fatalf("expected sales opportunity relationships in capabilities: %#v", capabilities.Relationships)
	}
	if !containsRelationship(capabilities.Relationships, "sales.orders", "contact_ref", "sales.contacts") || !containsRelationship(capabilities.Relationships, "sales.contacts", "customer_ref", "sales.customers") {
		t.Fatalf("expected sales contact relationships in capabilities: %#v", capabilities.Relationships)
	}
	if !containsRelationship(capabilities.Relationships, "procurement.purchase_orders", "supplier_ref", "procurement.suppliers") {
		t.Fatalf("expected purchase order supplier relationship in capabilities: %#v", capabilities.Relationships)
	}
	if !containsRelationship(capabilities.Relationships, "inventory.movements", "item_ref", "inventory.items") {
		t.Fatalf("expected inventory movement item relationship in capabilities: %#v", capabilities.Relationships)
	}
	if !containsRelationship(capabilities.Relationships, "inventory.items", "warehouse_ref", "inventory.warehouses") || !containsRelationship(capabilities.Relationships, "inventory.movements", "to_warehouse_ref", "inventory.warehouses") {
		t.Fatalf("expected inventory warehouse relationships in capabilities: %#v", capabilities.Relationships)
	}
	if !containsRelationship(capabilities.Relationships, "hr.leave_requests", "employee_ref", "hr.employees") {
		t.Fatalf("expected HR leave employee relationship in capabilities: %#v", capabilities.Relationships)
	}
	if !containsRelationship(capabilities.Relationships, "finance.budgets", "department_ref", "company.departments") {
		t.Fatalf("expected finance budget department relationship in capabilities: %#v", capabilities.Relationships)
	}
	for _, id := range []string{"procurement.purchase_order_upsert", "inventory.stock_update", "assets.fixed_asset_upsert", "finance.budget_upsert"} {
		if !containsBusinessAction(capabilities.BusinessActions, id) {
			t.Fatalf("expected business action %s", id)
		}
	}
	for _, id := range []string{"procurement.purchase_order_tracker", "inventory.stock_overview", "assets.asset_register"} {
		if !containsBusinessView(capabilities.BusinessViews, id) {
			t.Fatalf("expected business view %s", id)
		}
	}
	for _, id := range []string{"procurement.po_status_summary", "inventory.quantity_by_warehouse", "assets.value_by_department"} {
		if !containsReport(capabilities.Reports, id) {
			t.Fatalf("expected report %s", id)
		}
	}
	for _, id := range []string{"company.overview", "sales.overview", "finance.overview"} {
		if !containsDashboard(capabilities.Dashboards, id) {
			t.Fatalf("expected dashboard %s", id)
		}
	}
	foundCSVTemplateAction := false
	foundInboxAction := false
	foundStatsAction := false
	foundJSONLAction := false
	foundMaintenanceAction := false
	foundDownloadBackupAction := false
	foundBulkUpdateAction := false
	foundBulkDeleteAction := false
	foundRestoreRecordAction := false
	foundOperationPlanAction := false
	foundApprovalAction := false
	foundDashboardAction := false
	foundBootstrapAction := false
	for _, action := range capabilities.ToolActions {
		if action.Action == "get_import_template_csv" {
			foundCSVTemplateAction = true
		}
		if action.Action == "get_inbox" {
			foundInboxAction = true
		}
		if action.Action == "get_stats" {
			foundStatsAction = true
		}
		if action.Action == "import_records_jsonl" {
			foundJSONLAction = true
		}
		if action.Action == "run_maintenance" {
			foundMaintenanceAction = true
		}
		if action.Action == "download_backup" {
			foundDownloadBackupAction = true
		}
		if action.Action == "bulk_update_records" {
			foundBulkUpdateAction = true
		}
		if action.Action == "bulk_delete_records" {
			foundBulkDeleteAction = true
		}
		if action.Action == "restore_record" {
			foundRestoreRecordAction = true
		}
		if action.Action == "create_operation_plan" {
			foundOperationPlanAction = true
		}
		if action.Action == "create_record_approval" {
			foundApprovalAction = true
		}
		if action.Action == "run_dashboard" {
			foundDashboardAction = true
		}
		if action.Action == "bootstrap_templates" {
			foundBootstrapAction = true
		}
	}
	if !foundCSVTemplateAction {
		t.Fatalf("expected get_import_template_csv capability: %#v", capabilities.ToolActions)
	}
	if !foundInboxAction {
		t.Fatalf("expected get_inbox capability: %#v", capabilities.ToolActions)
	}
	if !foundStatsAction {
		t.Fatalf("expected get_stats capability: %#v", capabilities.ToolActions)
	}
	if !foundJSONLAction {
		t.Fatalf("expected import_records_jsonl capability: %#v", capabilities.ToolActions)
	}
	if !foundMaintenanceAction {
		t.Fatalf("expected run_maintenance capability: %#v", capabilities.ToolActions)
	}
	if !foundDownloadBackupAction {
		t.Fatalf("expected download_backup capability: %#v", capabilities.ToolActions)
	}
	if !foundBulkUpdateAction {
		t.Fatalf("expected bulk_update_records capability: %#v", capabilities.ToolActions)
	}
	if !foundBulkDeleteAction {
		t.Fatalf("expected bulk_delete_records capability: %#v", capabilities.ToolActions)
	}
	if !foundRestoreRecordAction {
		t.Fatalf("expected restore_record capability: %#v", capabilities.ToolActions)
	}
	if !foundOperationPlanAction {
		t.Fatalf("expected create_operation_plan capability: %#v", capabilities.ToolActions)
	}
	if !foundApprovalAction {
		t.Fatalf("expected create_record_approval capability: %#v", capabilities.ToolActions)
	}
	if !foundDashboardAction {
		t.Fatalf("expected run_dashboard capability: %#v", capabilities.ToolActions)
	}
	if !foundBootstrapAction {
		t.Fatalf("expected bootstrap_templates capability: %#v", capabilities.ToolActions)
	}
	if !containsToolAction(capabilities.ToolActions, "list_relationships") {
		t.Fatalf("expected list_relationships capability: %#v", capabilities.ToolActions)
	}
	if !containsToolAction(capabilities.ToolActions, "list_connectors") || !containsToolAction(capabilities.ToolActions, "test_connector") || !containsToolAction(capabilities.ToolActions, "validate_connector_config") || !containsToolAction(capabilities.ToolActions, "check_connector_readiness") || !containsToolAction(capabilities.ToolActions, "plan_connector_sync") {
		t.Fatalf("expected connector capabilities: %#v", capabilities.ToolActions)
	}
	if !containsToolAction(capabilities.ToolActions, "list_business_rules") || !containsToolAction(capabilities.ToolActions, "evaluate_business_rules") || len(capabilities.BusinessRules) == 0 {
		t.Fatalf("expected business rule capabilities: %#v rules=%#v", capabilities.ToolActions, capabilities.BusinessRules)
	}
	if !containsToolAction(capabilities.ToolActions, "list_event_dead_letters") || !containsToolAction(capabilities.ToolActions, "retry_event_dead_letter") {
		t.Fatalf("expected event dead-letter capabilities: %#v", capabilities.ToolActions)
	}
	foundStaff := false
	for _, dataset := range capabilities.Datasets {
		if dataset.Dataset.ID == "hr.staff" && len(dataset.Fields) > 0 {
			foundStaff = true
			break
		}
	}
	if !foundStaff {
		t.Fatalf("expected hr.staff in capabilities: %#v", capabilities.Datasets)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/relationships?dataset_id=sales.orders", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("relationships status=%d body=%s", w.Code, w.Body.String())
	}
	var relationships ListResponse[DatasetRelationship]
	if err := json.NewDecoder(w.Body).Decode(&relationships); err != nil {
		t.Fatalf("decode relationships: %v", err)
	}
	if !containsRelationship(relationships.Items, "sales.orders", "customer_ref", "sales.customers") {
		t.Fatalf("expected sales order customer relationship: %#v", relationships)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/relationships?dataset_id=sales.orders&limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first relationship cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstRelationshipPage ListResponse[DatasetRelationship]
	if err := json.NewDecoder(w.Body).Decode(&firstRelationshipPage); err != nil {
		t.Fatalf("decode first relationship cursor page: %v", err)
	}
	if len(firstRelationshipPage.Items) != 1 || !firstRelationshipPage.HasMore || firstRelationshipPage.NextBeforeID == "" {
		t.Fatalf("unexpected first relationship cursor page: %#v", firstRelationshipPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/relationships?dataset_id=sales.orders&limit=1&before_id="+url.QueryEscape(firstRelationshipPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second relationship cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondRelationshipPage ListResponse[DatasetRelationship]
	if err := json.NewDecoder(w.Body).Decode(&secondRelationshipPage); err != nil {
		t.Fatalf("decode second relationship cursor page: %v", err)
	}
	if len(secondRelationshipPage.Items) == 0 || relationshipCursorKey(secondRelationshipPage.Items[0]) == relationshipCursorKey(firstRelationshipPage.Items[0]) {
		t.Fatalf("unexpected second relationship cursor page: first=%#v second=%#v", firstRelationshipPage, secondRelationshipPage)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/business-rules?business_action_id=finance.expense_submit", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("business rules status=%d body=%s", w.Code, w.Body.String())
	}
	var rules ListResponse[BusinessRuleDefinition]
	if err := json.NewDecoder(w.Body).Decode(&rules); err != nil {
		t.Fatalf("decode business rules: %v", err)
	}
	foundExpenseRule := false
	for _, rule := range rules.Items {
		if rule.ID == "finance.expense.approval_required" && rule.RequiresApproval && rule.RequiresDryRun {
			foundExpenseRule = true
			break
		}
	}
	if !foundExpenseRule {
		t.Fatalf("expected finance expense approval/dry-run rule: %#v", rules)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/business-rules?severity=urgent", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid business rule severity status=%d body=%s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/business-rules?severity=critical", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("critical business rule severity status=%d body=%s", w.Code, w.Body.String())
	}
	rules = ListResponse[BusinessRuleDefinition]{}
	if err := json.NewDecoder(w.Body).Decode(&rules); err != nil {
		t.Fatalf("decode critical business rules: %v", err)
	}
	if len(rules.Items) == 0 {
		t.Fatalf("expected critical business rules")
	}
	for _, rule := range rules.Items {
		if !strings.EqualFold(rule.Severity, "critical") {
			t.Fatalf("critical severity filter returned %#v", rule)
		}
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/business-rules?limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first business rule cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstRulePage ListResponse[BusinessRuleDefinition]
	if err := json.NewDecoder(w.Body).Decode(&firstRulePage); err != nil {
		t.Fatalf("decode first business rule cursor page: %v", err)
	}
	if len(firstRulePage.Items) != 1 || !firstRulePage.HasMore || firstRulePage.NextBeforeID == "" {
		t.Fatalf("unexpected first business rule cursor page: %#v", firstRulePage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/business-rules?limit=1&before_id="+url.QueryEscape(firstRulePage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second business rule cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondRulePage ListResponse[BusinessRuleDefinition]
	if err := json.NewDecoder(w.Body).Decode(&secondRulePage); err != nil {
		t.Fatalf("decode second business rule cursor page: %v", err)
	}
	if len(secondRulePage.Items) == 0 || secondRulePage.Items[0].ID == firstRulePage.Items[0].ID {
		t.Fatalf("unexpected second business rule cursor page: first=%#v second=%#v", firstRulePage, secondRulePage)
	}
	body = bytes.NewBufferString(`{"business_action_id":"finance.expense_submit","record_id":"EXP-001","data":{"expense_no":"EXP-001","applicant":"Alice","amount":120}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-rules/evaluate", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("business rule evaluation status=%d body=%s", w.Code, w.Body.String())
	}
	var ruleEval BusinessRuleEvaluation
	if err := json.NewDecoder(w.Body).Decode(&ruleEval); err != nil {
		t.Fatalf("decode business rule evaluation: %v", err)
	}
	if !ruleEval.RequiresApproval || !ruleEval.RequiresDryRun || !ruleEval.RequiresQuality || len(ruleEval.NextSteps) < 2 {
		t.Fatalf("expected governed finance next steps: %#v", ruleEval)
	}
	if ruleEval.RequiresAdmin || ruleEval.RequiresBackup {
		t.Fatalf("action-specific finance rules should not inherit global admin/backup rules: %#v", ruleEval)
	}
	if ruleEval.GovernanceStatus != "needs_review" || len(ruleEval.StatusReasons) == 0 {
		t.Fatalf("expected governed finance next steps: %#v", ruleEval)
	}
	if ruleEval.CanExecuteNow || ruleEval.RecommendedAction != "execute_business_action" {
		t.Fatalf("expected finance rule to recommend dry-run business action first: %#v", ruleEval)
	}
	if !containsGateStatus(ruleEval.GateStatuses, "dry_run", "pending") || !containsGateStatus(ruleEval.GateStatuses, "approval", "pending") {
		t.Fatalf("expected finance rule gate statuses: %#v", ruleEval.GateStatuses)
	}
	body = bytes.NewBufferString(`{"business_action_id":"finance.expense_submit","record_id":"EXP-HIGH-001","data":{"expense_no":"EXP-HIGH-001","applicant":"Alice","amount":75000}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-rules/evaluate", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("high-value expense rule evaluation status=%d body=%s", w.Code, w.Body.String())
	}
	var highExpenseRuleEval BusinessRuleEvaluation
	if err := json.NewDecoder(w.Body).Decode(&highExpenseRuleEval); err != nil {
		t.Fatalf("decode high-value expense rule evaluation: %v", err)
	}
	if !highExpenseRuleEval.RequiresBackup || !containsMatchedRule(highExpenseRuleEval.MatchedRules, "finance.expense.high_value_backup") {
		t.Fatalf("expected high-value expense backup rule: %#v", highExpenseRuleEval)
	}
	if highExpenseRuleEval.CanExecuteNow || highExpenseRuleEval.RecommendedAction != "create_backup" {
		t.Fatalf("expected high-value expense to recommend backup first: %#v", highExpenseRuleEval)
	}
	if !containsNextStepParam(highExpenseRuleEval.NextSteps, "create_backup", "note") || !containsNextStepParam(highExpenseRuleEval.NextSteps, "create_record_approval", "request") {
		t.Fatalf("expected executable rule next-step templates: %#v", highExpenseRuleEval.NextSteps)
	}
	if !containsNextStepParamValue(highExpenseRuleEval.NextSteps, "create_record_approval", "assigned_to", "finance_manager") {
		t.Fatalf("expected finance approval default approver: %#v", highExpenseRuleEval.NextSteps)
	}
	body = bytes.NewBufferString(`{"business_action_id":"sales.order_status_update","record_id":"SO-RISK-001","data":{"stage":"cancelled"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-rules/evaluate", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("risky sales status rule evaluation status=%d body=%s", w.Code, w.Body.String())
	}
	var riskySalesRuleEval BusinessRuleEvaluation
	if err := json.NewDecoder(w.Body).Decode(&riskySalesRuleEval); err != nil {
		t.Fatalf("decode risky sales rule evaluation: %v", err)
	}
	if !riskySalesRuleEval.RequiresApproval || !containsMatchedRule(riskySalesRuleEval.MatchedRules, "sales.order.status_risk_approval") {
		t.Fatalf("expected risky sales status approval rule: %#v", riskySalesRuleEval)
	}

	body = bytes.NewBufferString(`{"id":"EMP-001","title":"Alice","data":{"employee_no":"EMP-001","name":"Alice","mobile":"13800000000"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/hr.staff/records", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create sensitive record status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/hr.staff/records/EMP-001", nil)
	authRole(req, "data_user")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get masked record status=%d body=%s", w.Code, w.Body.String())
	}
	var masked Record
	if err := json.NewDecoder(w.Body).Decode(&masked); err != nil {
		t.Fatalf("decode masked record: %v", err)
	}
	if masked.Data["mobile"] != maskedValue {
		t.Fatalf("expected masked mobile, got %#v", masked.Data["mobile"])
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/hr.staff/records/EMP-001", nil)
	authRole(req, "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get admin record status=%d body=%s", w.Code, w.Body.String())
	}
	var unmasked Record
	if err := json.NewDecoder(w.Body).Decode(&unmasked); err != nil {
		t.Fatalf("decode unmasked record: %v", err)
	}
	if unmasked.Data["mobile"] != "13800000000" {
		t.Fatalf("expected unmasked mobile, got %#v", unmasked.Data["mobile"])
	}

	body = bytes.NewBufferString(`{"data":{"employee_no":"EMP-001","name":"Duplicate Alice"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/hr.staff/records/validate", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("validate duplicate employee status=%d body=%s", w.Code, w.Body.String())
	}
	var duplicateValidation ValidateRecordResult
	if err := json.NewDecoder(w.Body).Decode(&duplicateValidation); err != nil {
		t.Fatalf("decode duplicate validation: %v", err)
	}
	if duplicateValidation.Valid || len(duplicateValidation.Errors) == 0 {
		t.Fatalf("expected duplicate validation failure: %#v", duplicateValidation)
	}

	body = bytes.NewBufferString(`{"id":"EMP-002","title":"Duplicate Alice","data":{"employee_no":"EMP-001","name":"Duplicate Alice"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/hr.staff/records", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected duplicate employee_no failure, status=%d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("bad request response should be JSON, got content-type %q", got)
	}

	body = bytes.NewBufferString(`{"title":"Alice Updated","data":{"employee_no":"EMP-001","name":"Alice Updated","mobile":"13800000000"}}`)
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/data/datasets/hr.staff/records/EMP-001", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update same unique employee status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/hr.staff/records/EMP-001/revisions?limit=10", nil)
	authRole(req, "data_user")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list record revisions status=%d body=%s", w.Code, w.Body.String())
	}
	var revisions ListResponse[RecordRevision]
	if err := json.NewDecoder(w.Body).Decode(&revisions); err != nil {
		t.Fatalf("decode revisions: %v", err)
	}
	if len(revisions.Items) < 2 || revisions.Items[0].Data["mobile"] != maskedValue {
		t.Fatalf("unexpected masked revisions: %#v", revisions)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/hr.staff/records/EMP-001/revisions?limit=1", nil)
	authRole(req, "data_user")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first revision cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstRevisionPage ListResponse[RecordRevision]
	if err := json.NewDecoder(w.Body).Decode(&firstRevisionPage); err != nil {
		t.Fatalf("decode first revision cursor page: %v", err)
	}
	if len(firstRevisionPage.Items) != 1 || !firstRevisionPage.HasMore || firstRevisionPage.NextBefore == "" || firstRevisionPage.NextBeforeID == "" {
		t.Fatalf("unexpected first revision cursor page: %#v", firstRevisionPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/hr.staff/records/EMP-001/revisions?limit=1&before="+url.QueryEscape(firstRevisionPage.NextBefore)+"&before_id="+url.QueryEscape(firstRevisionPage.NextBeforeID), nil)
	authRole(req, "data_user")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second revision cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondRevisionPage ListResponse[RecordRevision]
	if err := json.NewDecoder(w.Body).Decode(&secondRevisionPage); err != nil {
		t.Fatalf("decode second revision cursor page: %v", err)
	}
	if len(secondRevisionPage.Items) == 0 || secondRevisionPage.Items[0].ID == firstRevisionPage.Items[0].ID {
		t.Fatalf("unexpected second revision cursor page: first=%#v second=%#v", firstRevisionPage, secondRevisionPage)
	}

	body = bytes.NewBufferString(`{"dry_run":true,"records":[{"id":"EMP-BATCH-1","data":{"employee_no":"EMP-BATCH-DUP","name":"Batch One"}},{"id":"EMP-BATCH-2","data":{"employee_no":"EMP-BATCH-DUP","name":"Batch Two"}}]}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/hr.staff/records/batch", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("batch duplicate dry-run status=%d body=%s", w.Code, w.Body.String())
	}
	var duplicateBatch BatchImportRecordsResult
	if err := json.NewDecoder(w.Body).Decode(&duplicateBatch); err != nil {
		t.Fatalf("decode duplicate batch: %v", err)
	}
	if duplicateBatch.Valid || len(duplicateBatch.Validations) != 2 || duplicateBatch.Validations[1].Valid {
		t.Fatalf("expected duplicate batch validation failure: %#v", duplicateBatch)
	}

	body = bytes.NewBufferString(`{"title":"Invalid staff","data":{"name":"Alice"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/hr.staff/records", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected required field validation failure, status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"domain":"sales","name":"orders","title":"Orders"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated && w.Code != http.StatusConflict {
		t.Fatalf("create dataset status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"source":"crm","event_type":"sales.order.updated","operation":"upsert_record","dataset_id":"sales.orders","record_id":"SO-2026-0001","idempotency_key":"crm:sales.orders:SO-2026-0001:v1","title":"CRM Order","tags":["crm"],"data":{"order_no":"SO-2026-0001","customer":"Globex","amount":1200,"stage":"confirmed"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/events", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ingest event status=%d body=%s", w.Code, w.Body.String())
	}
	var eventResult DataEventResult
	if err := json.NewDecoder(w.Body).Decode(&eventResult); err != nil {
		t.Fatalf("decode event result: %v", err)
	}
	if eventResult.Status != "created" || eventResult.RecordID != "SO-2026-0001" || eventResult.Record == nil || eventResult.Record.Data["customer"] != "Globex" {
		t.Fatalf("unexpected event result: %#v", eventResult)
	}

	body = bytes.NewBufferString(`{"source":"crm","event_type":"sales.order.updated","operation":"upsert_record","dataset_id":"sales.orders","record_id":"SO-2026-0001","idempotency_key":"crm:sales.orders:SO-2026-0001:v1","title":"CRM Order Duplicate","tags":["crm"],"data":{"customer":"ShouldNotApply","amount":9999,"stage":"bad_duplicate"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/events", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("duplicate ingest event status=%d body=%s", w.Code, w.Body.String())
	}
	var duplicateEvent DataEventResult
	if err := json.NewDecoder(w.Body).Decode(&duplicateEvent); err != nil {
		t.Fatalf("decode duplicate event result: %v", err)
	}
	if !duplicateEvent.Duplicate || duplicateEvent.Status != "duplicate" || duplicateEvent.OriginalStatus != "created" || duplicateEvent.Record == nil || duplicateEvent.Record.Data["customer"] != "Globex" {
		t.Fatalf("unexpected duplicate event result: %#v", duplicateEvent)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/events?dataset_id=sales.orders&idempotency_key=crm%3Asales.orders%3ASO-2026-0001%3Av1&limit=10", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list data events status=%d body=%s", w.Code, w.Body.String())
	}
	var dataEvents ListResponse[DataEventLog]
	if err := json.NewDecoder(w.Body).Decode(&dataEvents); err != nil {
		t.Fatalf("decode data events: %v", err)
	}
	if len(dataEvents.Items) != 1 || dataEvents.Items[0].ResultStatus != "created" || dataEvents.Items[0].IdempotencyKey != "crm:sales.orders:SO-2026-0001:v1" {
		t.Fatalf("unexpected data events: %#v", dataEvents)
	}

	body = bytes.NewBufferString(`{"source":"crm","business_action_id":"sales.order_status_update","record_id":"SO-2026-0001","idempotency_key":"crm:sales.order_status:SO-2026-0001:dry","dry_run":true,"data":{"stage":"lost","payment_status":"overdue"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/events", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("dry-run business event status=%d body=%s", w.Code, w.Body.String())
	}
	var dryRunBusinessEvent DataEventResult
	if err := json.NewDecoder(w.Body).Decode(&dryRunBusinessEvent); err != nil {
		t.Fatalf("decode dry-run business event result: %v", err)
	}
	if !dryRunBusinessEvent.DryRun || !dryRunBusinessEvent.Valid || dryRunBusinessEvent.Status != "dry_run" || dryRunBusinessEvent.BusinessAction != "sales.order_status_update" || dryRunBusinessEvent.Preview["stage"] != "lost" {
		t.Fatalf("unexpected dry-run business event result: %#v", dryRunBusinessEvent)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/events?idempotency_key=crm%3Asales.order_status%3ASO-2026-0001%3Adry&limit=10", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list dry-run data events status=%d body=%s", w.Code, w.Body.String())
	}
	var dryRunDataEvents ListResponse[DataEventLog]
	if err := json.NewDecoder(w.Body).Decode(&dryRunDataEvents); err != nil {
		t.Fatalf("decode dry-run data events: %v", err)
	}
	if len(dryRunDataEvents.Items) != 0 {
		t.Fatalf("dry-run event should not be logged: %#v", dryRunDataEvents)
	}

	body = bytes.NewBufferString(`{"source":"crm","business_action_id":"sales.order_status_update","record_id":"SO-2026-0001","idempotency_key":"crm:sales.order_status:SO-2026-0001:v2","data":{"stage":"won","payment_status":"paid"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/events", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("business event ingest status=%d body=%s", w.Code, w.Body.String())
	}
	var businessEvent DataEventResult
	if err := json.NewDecoder(w.Body).Decode(&businessEvent); err != nil {
		t.Fatalf("decode business event result: %v", err)
	}
	if businessEvent.BusinessAction != "sales.order_status_update" || businessEvent.DatasetID != "sales.orders" || businessEvent.Operation != "merge_record" || businessEvent.Record == nil || businessEvent.Record.Data["stage"] != "won" || businessEvent.Record.Data["customer"] != "Globex" {
		t.Fatalf("unexpected business event result: %#v", businessEvent)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/events?business_action_id=sales.order_status_update&limit=10", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list business data events status=%d body=%s", w.Code, w.Body.String())
	}
	var businessDataEvents ListResponse[DataEventLog]
	if err := json.NewDecoder(w.Body).Decode(&businessDataEvents); err != nil {
		t.Fatalf("decode business data events: %v", err)
	}
	if len(businessDataEvents.Items) != 1 || businessDataEvents.Items[0].BusinessAction != "sales.order_status_update" || businessDataEvents.Items[0].IdempotencyKey != "crm:sales.order_status:SO-2026-0001:v2" {
		t.Fatalf("unexpected business data events: %#v", businessDataEvents)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/events?dataset_id=sales.orders&limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first data event cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstDataEventPage ListResponse[DataEventLog]
	if err := json.NewDecoder(w.Body).Decode(&firstDataEventPage); err != nil {
		t.Fatalf("decode first data event cursor page: %v", err)
	}
	if len(firstDataEventPage.Items) != 1 || !firstDataEventPage.HasMore || firstDataEventPage.NextBefore == "" || firstDataEventPage.NextBeforeID == "" {
		t.Fatalf("unexpected first data event cursor page: %#v", firstDataEventPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/events?dataset_id=sales.orders&limit=1&before="+url.QueryEscape(firstDataEventPage.NextBefore)+"&before_id="+url.QueryEscape(firstDataEventPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second data event cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondDataEventPage ListResponse[DataEventLog]
	if err := json.NewDecoder(w.Body).Decode(&secondDataEventPage); err != nil {
		t.Fatalf("decode second data event cursor page: %v", err)
	}
	if len(secondDataEventPage.Items) == 0 || secondDataEventPage.Items[0].ID == firstDataEventPage.Items[0].ID {
		t.Fatalf("unexpected second data event cursor page: first=%#v second=%#v", firstDataEventPage, secondDataEventPage)
	}

	body = bytes.NewBufferString(`{"source":"crm","business_action_id":"sales.order_status_update","record_id":"SO-2026-0001","idempotency_key":"crm:sales.order_status:SO-2026-0001:v2","data":{"stage":"duplicate_should_not_apply","payment_status":"bad"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/events", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("duplicate business event ingest status=%d body=%s", w.Code, w.Body.String())
	}
	var duplicateBusinessEvent DataEventResult
	if err := json.NewDecoder(w.Body).Decode(&duplicateBusinessEvent); err != nil {
		t.Fatalf("decode duplicate business event result: %v", err)
	}
	if !duplicateBusinessEvent.Duplicate || duplicateBusinessEvent.BusinessAction != "sales.order_status_update" || duplicateBusinessEvent.Record == nil || duplicateBusinessEvent.Record.Data["stage"] != "won" {
		t.Fatalf("unexpected duplicate business event result: %#v", duplicateBusinessEvent)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/business-actions", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list business actions status=%d body=%s", w.Code, w.Body.String())
	}
	var actions ListResponse[BusinessAction]
	if err := json.NewDecoder(w.Body).Decode(&actions); err != nil {
		t.Fatalf("decode business actions: %v", err)
	}
	if len(actions.Items) == 0 {
		t.Fatalf("expected business actions")
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/business-actions?limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first business action cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstActionPage ListResponse[BusinessAction]
	if err := json.NewDecoder(w.Body).Decode(&firstActionPage); err != nil {
		t.Fatalf("decode first business action cursor page: %v", err)
	}
	if len(firstActionPage.Items) != 1 || !firstActionPage.HasMore || firstActionPage.NextBeforeID == "" {
		t.Fatalf("unexpected first business action cursor page: %#v", firstActionPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/business-actions?limit=1&before_id="+url.QueryEscape(firstActionPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second business action cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondActionPage ListResponse[BusinessAction]
	if err := json.NewDecoder(w.Body).Decode(&secondActionPage); err != nil {
		t.Fatalf("decode second business action cursor page: %v", err)
	}
	if len(secondActionPage.Items) == 0 || secondActionPage.Items[0].ID == firstActionPage.Items[0].ID {
		t.Fatalf("unexpected second business action cursor page: first=%#v second=%#v", firstActionPage, secondActionPage)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/event-contracts?domain=sales", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list event contracts status=%d body=%s", w.Code, w.Body.String())
	}
	var contracts ListResponse[EventContract]
	if err := json.NewDecoder(w.Body).Decode(&contracts); err != nil {
		t.Fatalf("decode event contracts: %v", err)
	}
	if len(contracts.Items) == 0 || contracts.Items[0].Endpoint != "/api/v1/data/events" || contracts.Items[0].ConnectorEndpoint != "/api/v1/data/connectors/{connector_id}/events" {
		t.Fatalf("unexpected event contracts: %#v", contracts)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/event-contracts?domain=sales&limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first event contract cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstContractPage ListResponse[EventContract]
	if err := json.NewDecoder(w.Body).Decode(&firstContractPage); err != nil {
		t.Fatalf("decode first event contract cursor page: %v", err)
	}
	if len(firstContractPage.Items) != 1 || !firstContractPage.HasMore || firstContractPage.NextBeforeID == "" {
		t.Fatalf("unexpected first event contract cursor page: %#v", firstContractPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/event-contracts?domain=sales&limit=1&before_id="+url.QueryEscape(firstContractPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second event contract cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondContractPage ListResponse[EventContract]
	if err := json.NewDecoder(w.Body).Decode(&secondContractPage); err != nil {
		t.Fatalf("decode second event contract cursor page: %v", err)
	}
	if len(secondContractPage.Items) == 0 || secondContractPage.Items[0].ID == firstContractPage.Items[0].ID {
		t.Fatalf("unexpected second event contract cursor page: first=%#v second=%#v", firstContractPage, secondContractPage)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/event-contracts/sales.order_upsert", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get event contract status=%d body=%s", w.Code, w.Body.String())
	}
	var eventContract EventContract
	if err := json.NewDecoder(w.Body).Decode(&eventContract); err != nil {
		t.Fatalf("decode event contract: %v", err)
	}
	if eventContract.BusinessAction != "sales.order_upsert" || eventContract.ConnectorEndpoint == "" || eventContract.DryRunBodyTemplate["dry_run"] != true || eventContract.CommitBodyTemplate["dry_run"] != false || eventContract.DryRunBodyTemplate["business_action_id"] != "sales.order_upsert" {
		t.Fatalf("unexpected event contract: %#v", eventContract)
	}

	body = bytes.NewBufferString(`{"id":"sales.crm","domain":"sales","name":"Sales CRM","kind":"crm","base_url":"https://crm.example.local","auth_type":"bearer","token_ref":"MIS_CRM_TOKEN","subscribed_actions":["sales.order_upsert","sales.order_status_update"],"config":{"owner":"sales-ops"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/connectors", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create connector status=%d body=%s", w.Code, w.Body.String())
	}
	var connector ExternalConnector
	if err := json.NewDecoder(w.Body).Decode(&connector); err != nil {
		t.Fatalf("decode connector: %v", err)
	}
	if connector.ID != "sales.crm" || connector.Domain != "sales" || !connector.Enabled || len(connector.SubscribedActions) != 2 {
		t.Fatalf("unexpected connector: %#v", connector)
	}
	body = bytes.NewBufferString(`{"id":"finance.erp","domain":"finance","name":"Finance ERP","kind":"erp","base_url":"https://erp.example.local","auth_type":"api_key","token_ref":"MIS_ERP_TOKEN","subscribed_actions":["finance.expense_submit"],"config":{"owner":"finance-ops"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/connectors", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create second connector status=%d body=%s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/connectors?limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first connector cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstConnectorPage ListResponse[ExternalConnector]
	if err := json.NewDecoder(w.Body).Decode(&firstConnectorPage); err != nil {
		t.Fatalf("decode first connector cursor page: %v", err)
	}
	if len(firstConnectorPage.Items) != 1 || !firstConnectorPage.HasMore || firstConnectorPage.NextBefore == "" || firstConnectorPage.NextBeforeID == "" {
		t.Fatalf("unexpected first connector cursor page: %#v", firstConnectorPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/connectors?limit=1&before="+url.QueryEscape(firstConnectorPage.NextBefore)+"&before_id="+url.QueryEscape(firstConnectorPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second connector cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondConnectorPage ListResponse[ExternalConnector]
	if err := json.NewDecoder(w.Body).Decode(&secondConnectorPage); err != nil {
		t.Fatalf("decode second connector cursor page: %v", err)
	}
	if len(secondConnectorPage.Items) == 0 || secondConnectorPage.Items[0].ID == firstConnectorPage.Items[0].ID {
		t.Fatalf("unexpected second connector cursor page: first=%#v second=%#v", firstConnectorPage, secondConnectorPage)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/connectors/sales.crm/test", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("test connector status=%d body=%s", w.Code, w.Body.String())
	}
	var connectorTest ConnectorTestResult
	if err := json.NewDecoder(w.Body).Decode(&connectorTest); err != nil {
		t.Fatalf("decode connector test: %v", err)
	}
	if !connectorTest.Valid || len(connectorTest.Bindings) != 2 || connectorTest.Bindings[0].Contract == nil {
		t.Fatalf("unexpected connector test: %#v", connectorTest)
	}

	body = bytes.NewBufferString(`{"business_action_id":"sales.order_upsert","record_id":"SO-CONN-0001","idempotency_key":"crm:conn:SO-CONN-0001:v1","data":{"order_no":"SO-CONN-0001","customer":"ConnectorCo","amount":8800}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/connectors/sales.crm/events", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ingest connector event status=%d body=%s", w.Code, w.Body.String())
	}
	var connectorEvent DataEventResult
	if err := json.NewDecoder(w.Body).Decode(&connectorEvent); err != nil {
		t.Fatalf("decode connector event: %v", err)
	}
	if connectorEvent.Source != "crm" || connectorEvent.BusinessAction != "sales.order_upsert" || connectorEvent.Record == nil || connectorEvent.Record.Data["customer"] != "ConnectorCo" {
		t.Fatalf("unexpected connector event result: %#v", connectorEvent)
	}

	body = bytes.NewBufferString(`{"business_action_id":"sales.order_upsert","sample_data":{"crm_id":"SO-SUG-0001","account":{"name":"SuggestCo"},"totals":{"amount":6600}}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/connectors/sales.crm/mappings/suggest", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("suggest connector mapping status=%d body=%s", w.Code, w.Body.String())
	}
	var suggestion ConnectorMappingSuggestion
	if err := json.NewDecoder(w.Body).Decode(&suggestion); err != nil {
		t.Fatalf("decode connector mapping suggestion: %v", err)
	}
	if suggestion.SuggestedMapping["order_no"] != "crm_id" || suggestion.SuggestedMapping["customer"] != "account.name" || suggestion.SuggestedMapping["amount"] != "totals.amount" || suggestion.ConfigPatch == nil {
		t.Fatalf("unexpected connector mapping suggestion: %#v", suggestion)
	}

	body = bytes.NewBufferString(`{"dry_run":true,"patch":{"field_mappings":{"sales.order_upsert":{"order_no":"crm_id","customer":"account.name","amount":"totals.amount"},"sales.order_status_update":{"stage":"stage"}}}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/connectors/sales.crm/config/patch", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("dry-run patch connector config status=%d body=%s", w.Code, w.Body.String())
	}
	var patchDryRun ConnectorConfigPatchResult
	if err := json.NewDecoder(w.Body).Decode(&patchDryRun); err != nil {
		t.Fatalf("decode dry-run connector config patch: %v", err)
	}
	if !patchDryRun.DryRun || patchDryRun.PatchedConfig["field_mappings"] == nil {
		t.Fatalf("unexpected dry-run connector config patch: %#v", patchDryRun)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/connectors/sales.crm/config/validate", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("validate connector config before patch status=%d body=%s", w.Code, w.Body.String())
	}
	var invalidConnectorConfig ConnectorConfigValidationResult
	if err := json.NewDecoder(w.Body).Decode(&invalidConnectorConfig); err != nil {
		t.Fatalf("decode invalid connector config validation: %v", err)
	}
	if invalidConnectorConfig.Valid || len(invalidConnectorConfig.Issues) == 0 {
		t.Fatalf("expected connector config validation issues before mapping patch: %#v", invalidConnectorConfig)
	}
	body = bytes.NewBufferString(`{"sample_event":{"business_action_id":"sales.order_upsert","data":{"crm_id":"SO-READY-BAD","account":{"name":"ReadyBadCo"},"totals":{"amount":100}}}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/connectors/sales.crm/readiness", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("readiness before patch status=%d body=%s", w.Code, w.Body.String())
	}
	var notReady ConnectorReadinessResult
	if err := json.NewDecoder(w.Body).Decode(&notReady); err != nil {
		t.Fatalf("decode not ready connector readiness: %v", err)
	}
	if notReady.Ready || notReady.Config == nil || notReady.Config.Valid {
		t.Fatalf("unexpected not ready connector readiness: %#v", notReady)
	}
	body = bytes.NewBufferString(`{"patch":{"field_mappings":{"sales.order_upsert":{"order_no":"crm_id","customer":"account.name","amount":"totals.amount"},"sales.order_status_update":{"stage":"stage"}}}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/connectors/sales.crm/config/patch", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("patch connector config status=%d body=%s", w.Code, w.Body.String())
	}
	var patchResult ConnectorConfigPatchResult
	if err := json.NewDecoder(w.Body).Decode(&patchResult); err != nil {
		t.Fatalf("decode connector config patch: %v", err)
	}
	if patchResult.DryRun || patchResult.PatchedConfig["field_mappings"] == nil || patchResult.PatchedConfig["owner"] != "sales-ops" {
		t.Fatalf("unexpected connector config patch: %#v", patchResult)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/connectors/sales.crm/config/validate", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("validate connector config status=%d body=%s", w.Code, w.Body.String())
	}
	var validConnectorConfig ConnectorConfigValidationResult
	if err := json.NewDecoder(w.Body).Decode(&validConnectorConfig); err != nil {
		t.Fatalf("decode connector config validation: %v", err)
	}
	foundOrderMapping := false
	for _, actionSummary := range validConnectorConfig.Actions {
		if actionSummary.ActionID == "sales.order_upsert" && len(actionSummary.MappedFields) >= 3 {
			foundOrderMapping = true
			break
		}
	}
	if !validConnectorConfig.Valid || len(validConnectorConfig.Issues) != 0 || !foundOrderMapping {
		t.Fatalf("unexpected connector config validation: %#v", validConnectorConfig)
	}
	body = bytes.NewBufferString(`{"sample_event":{"business_action_id":"sales.order_upsert","data":{"crm_id":"SO-READY-0001","account":{"name":"ReadyCo"},"totals":{"amount":7700}}}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/connectors/sales.crm/readiness", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("readiness status=%d body=%s", w.Code, w.Body.String())
	}
	var readiness ConnectorReadinessResult
	if err := json.NewDecoder(w.Body).Decode(&readiness); err != nil {
		t.Fatalf("decode connector readiness: %v", err)
	}
	if !readiness.Ready || readiness.Test == nil || readiness.Config == nil || readiness.Preview == nil || readiness.Preview.RecordID != "SO-READY-0001" {
		t.Fatalf("unexpected connector readiness: %#v", readiness)
	}
	body = bytes.NewBufferString(`{"sample_event":{"business_action_id":"sales.order_upsert","data":{"crm_id":"SO-PLAN-0001","account":{"name":"PlanCo"},"totals":{"amount":8800}}},"first_page_events":[{"business_action_id":"sales.order_upsert","data":{"crm_id":"SO-PLAN-0001","account":{"name":"PlanCo"},"totals":{"amount":8800}}}],"page_size":50,"cursor":"page-1"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/connectors/sales.crm/sync-plan", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("connector sync plan status=%d body=%s", w.Code, w.Body.String())
	}
	var syncPlan ConnectorSyncPlanResult
	if err := json.NewDecoder(w.Body).Decode(&syncPlan); err != nil {
		t.Fatalf("decode connector sync plan: %v", err)
	}
	if !syncPlan.Ready || syncPlan.Readiness == nil || syncPlan.DryRunBatch == nil || syncPlan.DryRunBatch.Failed != 0 || syncPlan.PageSize != 50 || syncPlan.Cursor != "page-1" || len(syncPlan.Steps) < 6 || len(syncPlan.Rollback) == 0 {
		t.Fatalf("unexpected connector sync plan: %#v", syncPlan)
	}
	if syncPlan.Steps[0].ToolCallTemplate["action"] != "get_connector_sync_state" || syncPlan.Steps[0].ToolCallTemplate["connector_id"] != "sales.crm" {
		t.Fatalf("expected connector sync plan tool call template, got %#v", syncPlan.Steps[0].ToolCallTemplate)
	}
	if syncPlan.Rollback[0].ToolCallTemplate["action"] != "update_connector_sync_state" || syncPlan.Rollback[0].ToolCallTemplate["connector_id"] != "sales.crm" {
		t.Fatalf("expected connector sync rollback tool call template, got %#v", syncPlan.Rollback[0].ToolCallTemplate)
	}
	body = bytes.NewBufferString(`{"page_size":501}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/connectors/sales.crm/sync-plan", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected oversized connector sync page_size rejection, status=%d body=%s", w.Code, w.Body.String())
	}
	body = bytes.NewBufferString(`{"business_action_id":"sales.order_upsert","data":{"crm_id":"SO-MAP-0001","account":{"name":"MappedCo"},"totals":{"amount":7700}}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/connectors/sales.crm/events/preview", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("preview mapped connector event status=%d body=%s", w.Code, w.Body.String())
	}
	var mappedPreview ConnectorEventPreview
	if err := json.NewDecoder(w.Body).Decode(&mappedPreview); err != nil {
		t.Fatalf("decode mapped connector preview: %v", err)
	}
	if !mappedPreview.MappingApplied || mappedPreview.RecordID != "SO-MAP-0001" || mappedPreview.IdempotencyKey != "crm:sales.order_upsert:SO-MAP-0001:v1" || mappedPreview.MappedData["customer"] != "MappedCo" || mappedPreview.DryRunResult == nil || !mappedPreview.DryRunResult.DryRun {
		t.Fatalf("unexpected mapped connector preview: %#v", mappedPreview)
	}

	body = bytes.NewBufferString(`{"business_action_id":"sales.order_upsert","data":{"crm_id":"SO-MAP-0001","account":{"name":"MappedCo"},"totals":{"amount":7700}}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/connectors/sales.crm/events", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ingest mapped connector event status=%d body=%s", w.Code, w.Body.String())
	}
	var mappedConnectorEvent DataEventResult
	if err := json.NewDecoder(w.Body).Decode(&mappedConnectorEvent); err != nil {
		t.Fatalf("decode mapped connector event: %v", err)
	}
	if mappedConnectorEvent.RecordID != "SO-MAP-0001" || mappedConnectorEvent.IdempotencyKey != "crm:sales.order_upsert:SO-MAP-0001:v1" || mappedConnectorEvent.Record == nil || mappedConnectorEvent.Record.Data["customer"] != "MappedCo" {
		t.Fatalf("unexpected mapped connector event result: %#v", mappedConnectorEvent)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/connectors/sales.crm/health", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("connector health status=%d body=%s", w.Code, w.Body.String())
	}
	var connectorHealth ConnectorHealth
	if err := json.NewDecoder(w.Body).Decode(&connectorHealth); err != nil {
		t.Fatalf("decode connector health: %v", err)
	}
	if connectorHealth.Source != "crm" || connectorHealth.RecentEvents == 0 || connectorHealth.LastEvent == nil || len(connectorHealth.Actions) == 0 {
		t.Fatalf("unexpected connector health: %#v", connectorHealth)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/connectors/health", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("connector health overview status=%d body=%s", w.Code, w.Body.String())
	}
	var connectorHealthOverview ListResponse[ConnectorHealth]
	if err := json.NewDecoder(w.Body).Decode(&connectorHealthOverview); err != nil {
		t.Fatalf("decode connector health overview: %v", err)
	}
	if len(connectorHealthOverview.Items) == 0 || connectorHealthOverview.Items[0].Connector.ID != "sales.crm" {
		t.Fatalf("unexpected connector health overview: %#v", connectorHealthOverview)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/connectors/health?limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first connector health cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstConnectorHealthPage ListResponse[ConnectorHealth]
	if err := json.NewDecoder(w.Body).Decode(&firstConnectorHealthPage); err != nil {
		t.Fatalf("decode first connector health cursor page: %v", err)
	}
	if len(firstConnectorHealthPage.Items) != 1 || !firstConnectorHealthPage.HasMore || firstConnectorHealthPage.NextBefore == "" || firstConnectorHealthPage.NextBeforeID == "" {
		t.Fatalf("unexpected first connector health cursor page: %#v", firstConnectorHealthPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/connectors/health?limit=1&before="+url.QueryEscape(firstConnectorHealthPage.NextBefore)+"&before_id="+url.QueryEscape(firstConnectorHealthPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second connector health cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondConnectorHealthPage ListResponse[ConnectorHealth]
	if err := json.NewDecoder(w.Body).Decode(&secondConnectorHealthPage); err != nil {
		t.Fatalf("decode second connector health cursor page: %v", err)
	}
	if len(secondConnectorHealthPage.Items) == 0 || secondConnectorHealthPage.Items[0].Connector.ID == firstConnectorHealthPage.Items[0].Connector.ID {
		t.Fatalf("unexpected second connector health cursor page: first=%#v second=%#v", firstConnectorHealthPage, secondConnectorHealthPage)
	}

	body = bytes.NewBufferString(`{"status":"failed","cursor":"page-42","checkpoint":{"since":"2026-05-05T00:00:00Z"},"last_error":"remote timeout","synced_records":12,"started_at":"2026-05-05T01:00:00Z","finished_at":"2026-05-05T01:05:00Z"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/connectors/sales.crm/sync-state", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update connector sync state status=%d body=%s", w.Code, w.Body.String())
	}
	var syncState ConnectorSyncState
	if err := json.NewDecoder(w.Body).Decode(&syncState); err != nil {
		t.Fatalf("decode connector sync state: %v", err)
	}
	if syncState.Status != "failed" || syncState.Cursor != "page-42" || syncState.LastError != "remote timeout" || syncState.SyncedRecords != 12 {
		t.Fatalf("unexpected connector sync state: %#v", syncState)
	}
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "invalid started_at", body: `{"started_at":"not-a-time"}`},
		{name: "invalid finished_at", body: `{"finished_at":"not-a-time"}`},
		{name: "negative synced_records", body: `{"synced_records":-1}`},
	} {
		body = bytes.NewBufferString(tc.body)
		req = httptest.NewRequest(http.MethodPost, "/api/v1/data/connectors/sales.crm/sync-state", body)
		auth(req)
		w = httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", tc.name, w.Code, w.Body.String())
		}
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/connectors/sales.crm/health", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("connector health with sync state status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&connectorHealth); err != nil {
		t.Fatalf("decode connector health with sync state: %v", err)
	}
	if connectorHealth.Status != "degraded" || connectorHealth.SyncState == nil || connectorHealth.SyncState.Cursor != "page-42" {
		t.Fatalf("unexpected connector health sync state: %#v", connectorHealth)
	}

	body = bytes.NewBufferString(`{"events":[{"business_action_id":"sales.order_upsert","data":{"crm_id":"SO-BATCH-0001","account":{"name":"BatchCo"},"totals":{"amount":9100}}},{"business_action_id":"finance.expense_submit","record_id":"EXP-BATCH-0001","idempotency_key":"crm:batch:EXP-BATCH-0001:v1","data":{"expense_no":"EXP-BATCH-0001","employee":"Mallory","amount":99}}],"sync_state":{"cursor":"page-43","finished_at":"2026-05-05T01:10:00Z"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/connectors/sales.crm/sync-batch", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("connector sync batch status=%d body=%s", w.Code, w.Body.String())
	}
	var connectorBatch ConnectorSyncBatchResult
	if err := json.NewDecoder(w.Body).Decode(&connectorBatch); err != nil {
		t.Fatalf("decode connector sync batch: %v", err)
	}
	if connectorBatch.Total != 2 || connectorBatch.Succeeded != 1 || connectorBatch.Failed != 1 || connectorBatch.Items[1].DeadLetter == nil || connectorBatch.SyncState == nil || connectorBatch.SyncState.Cursor != "page-43" || connectorBatch.SyncState.Status != "failed" || connectorBatch.Run == nil || connectorBatch.Run.Status != "failed" {
		t.Fatalf("unexpected connector sync batch: %#v", connectorBatch)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/connectors/sales.crm/sync-runs", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list connector sync runs status=%d body=%s", w.Code, w.Body.String())
	}
	var syncRuns ListResponse[ConnectorSyncRun]
	if err := json.NewDecoder(w.Body).Decode(&syncRuns); err != nil {
		t.Fatalf("decode connector sync runs: %v", err)
	}
	if len(syncRuns.Items) == 0 || syncRuns.Items[0].ID != connectorBatch.Run.ID || syncRuns.Items[0].Failed != 1 || syncRuns.Items[0].Cursor != "page-43" {
		t.Fatalf("unexpected connector sync runs: %#v", syncRuns)
	}
	for _, suffix := range []string{"0002", "0003"} {
		body = bytes.NewBufferString(fmt.Sprintf(`{"events":[{"business_action_id":"sales.order_upsert","data":{"crm_id":"SO-BATCH-%s","account":{"name":"BatchCo %s"},"totals":{"amount":1200}}}],"sync_state":{"cursor":"page-%s","finished_at":"2026-05-05T01:1%s:00Z"}}`, suffix, suffix, suffix, suffix[len(suffix)-1:]))
		req = httptest.NewRequest(http.MethodPost, "/api/v1/data/connectors/sales.crm/sync-batch", body)
		auth(req)
		w = httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("connector sync batch %s status=%d body=%s", suffix, w.Code, w.Body.String())
		}
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/connectors/sales.crm/sync-runs?limit=2", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first connector sync run cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstSyncRunPage ListResponse[ConnectorSyncRun]
	if err := json.NewDecoder(w.Body).Decode(&firstSyncRunPage); err != nil {
		t.Fatalf("decode first connector sync run cursor page: %v", err)
	}
	if len(firstSyncRunPage.Items) != 2 || !firstSyncRunPage.HasMore || firstSyncRunPage.NextBefore == "" || firstSyncRunPage.NextBeforeID == "" {
		t.Fatalf("unexpected first connector sync run cursor page: %#v", firstSyncRunPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/connectors/sales.crm/sync-runs?limit=2&before="+url.QueryEscape(firstSyncRunPage.NextBefore)+"&before_id="+url.QueryEscape(firstSyncRunPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second connector sync run cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondSyncRunPage ListResponse[ConnectorSyncRun]
	if err := json.NewDecoder(w.Body).Decode(&secondSyncRunPage); err != nil {
		t.Fatalf("decode second connector sync run cursor page: %v", err)
	}
	if len(secondSyncRunPage.Items) == 0 {
		t.Fatalf("expected second connector sync run cursor page to continue: %#v", secondSyncRunPage)
	}
	seenSyncRunIDs := map[string]struct{}{}
	for _, item := range firstSyncRunPage.Items {
		seenSyncRunIDs[item.ID] = struct{}{}
	}
	for _, item := range secondSyncRunPage.Items {
		if _, ok := seenSyncRunIDs[item.ID]; ok {
			t.Fatalf("connector sync run cursor repeated %s across pages: first=%#v second=%#v", item.ID, firstSyncRunPage, secondSyncRunPage)
		}
	}

	body = bytes.NewBufferString(`{"business_action_id":"finance.expense_submit","record_id":"EXP-CONN-0001","idempotency_key":"crm:conn:EXP-CONN-0001:v1","data":{"expense_no":"EXP-CONN-0001","employee":"Mallory","amount":99}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/connectors/sales.crm/events", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("unsubscribed connector event status=%d body=%s", w.Code, w.Body.String())
	}
	var blockedConnectorEvent map[string]any
	if err := json.NewDecoder(w.Body).Decode(&blockedConnectorEvent); err != nil {
		t.Fatalf("decode blocked connector event: %v", err)
	}
	if _, ok := blockedConnectorEvent["dead_letter"].(map[string]any); !ok {
		t.Fatalf("expected dead letter for blocked connector event: %#v", blockedConnectorEvent)
	}

	body = bytes.NewBufferString(`{"source":"crm","business_action_id":"sales.order_status_update","record_id":"SO-BAD-0001","idempotency_key":"crm:bad:SO-BAD-0001:v1","data":{"payment_status":"paid"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/events", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid event should create dead letter status=%d body=%s", w.Code, w.Body.String())
	}
	var failedEvent map[string]any
	if err := json.NewDecoder(w.Body).Decode(&failedEvent); err != nil {
		t.Fatalf("decode failed event: %v", err)
	}
	deadLetterMap, ok := failedEvent["dead_letter"].(map[string]any)
	if !ok || deadLetterMap["status"] != "open" || deadLetterMap["business_action_id"] != "sales.order_status_update" {
		t.Fatalf("expected dead letter in failed event response: %#v", failedEvent)
	}
	deadLetterID, _ := deadLetterMap["id"].(string)
	if deadLetterID == "" {
		t.Fatalf("missing dead letter id: %#v", failedEvent)
	}
	body = bytes.NewBufferString(`{"source":"crm","business_action_id":"sales.order_status_update","record_id":"SO-BAD-0002","idempotency_key":"crm:bad:SO-BAD-0002:v1","data":{"payment_status":"paid"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/events", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("second invalid event should create dead letter status=%d body=%s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/events/dead-letter?status=open&business_action_id=sales.order_status_update", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list dead letters status=%d body=%s", w.Code, w.Body.String())
	}
	var deadLetters ListResponse[DataEventDeadLetter]
	if err := json.NewDecoder(w.Body).Decode(&deadLetters); err != nil {
		t.Fatalf("decode dead letters: %v", err)
	}
	if !containsDeadLetterID(deadLetters.Items, deadLetterID) {
		t.Fatalf("unexpected dead letters: %#v", deadLetters)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/events/dead-letter?status=open&business_action_id=sales.order_status_update&limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first dead letter cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstDeadLetterPage ListResponse[DataEventDeadLetter]
	if err := json.NewDecoder(w.Body).Decode(&firstDeadLetterPage); err != nil {
		t.Fatalf("decode first dead letter cursor page: %v", err)
	}
	if len(firstDeadLetterPage.Items) != 1 || !firstDeadLetterPage.HasMore || firstDeadLetterPage.NextBefore == "" || firstDeadLetterPage.NextBeforeID == "" {
		t.Fatalf("unexpected first dead letter cursor page: %#v", firstDeadLetterPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/events/dead-letter?status=open&business_action_id=sales.order_status_update&limit=1&before="+url.QueryEscape(firstDeadLetterPage.NextBefore)+"&before_id="+url.QueryEscape(firstDeadLetterPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second dead letter cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondDeadLetterPage ListResponse[DataEventDeadLetter]
	if err := json.NewDecoder(w.Body).Decode(&secondDeadLetterPage); err != nil {
		t.Fatalf("decode second dead letter cursor page: %v", err)
	}
	if len(secondDeadLetterPage.Items) == 0 || secondDeadLetterPage.Items[0].ID == firstDeadLetterPage.Items[0].ID {
		t.Fatalf("unexpected second dead letter cursor page: first=%#v second=%#v", firstDeadLetterPage, secondDeadLetterPage)
	}
	body = bytes.NewBufferString(`{"resolution":"bad payload archived in test"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/events/dead-letter/"+deadLetterID+"/resolve", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve dead letter status=%d body=%s", w.Code, w.Body.String())
	}
	var resolvedDeadLetter DataEventDeadLetter
	if err := json.NewDecoder(w.Body).Decode(&resolvedDeadLetter); err != nil {
		t.Fatalf("decode resolved dead letter: %v", err)
	}
	if resolvedDeadLetter.Status != "resolved" || resolvedDeadLetter.Resolution == "" || resolvedDeadLetter.ResolvedAt.IsZero() {
		t.Fatalf("unexpected resolved dead letter: %#v", resolvedDeadLetter)
	}

	body = bytes.NewBufferString(`{"record_id":"SO-DRY-0001","dry_run":true,"data":{"order_no":"SO-DRY-0001","customer":"DryRunCo","amount":1234}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-actions/sales.order_upsert/execute", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("dry-run business action status=%d body=%s", w.Code, w.Body.String())
	}
	var dryRunAction ExecuteBusinessActionResult
	if err := json.NewDecoder(w.Body).Decode(&dryRunAction); err != nil {
		t.Fatalf("decode dry-run business action result: %v", err)
	}
	if !dryRunAction.DryRun || !dryRunAction.Valid || dryRunAction.Validation == nil || dryRunAction.Event != nil || dryRunAction.Preview["customer"] != "DryRunCo" {
		t.Fatalf("unexpected dry-run business action result: %#v", dryRunAction)
	}
	if dryRunAction.Rules == nil || dryRunAction.Rules.GovernanceStatus != "clear" {
		t.Fatalf("expected dry-run business action governance rules: %#v", dryRunAction.Rules)
	}
	body = bytes.NewBufferString(`{"record_id":"SO-HIGH-0001","dry_run":true,"data":{"order_no":"SO-HIGH-0001","customer":"HighValueCo","amount":150000}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-actions/sales.order_upsert/execute", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("high-value dry-run business action status=%d body=%s", w.Code, w.Body.String())
	}
	var highValueAction ExecuteBusinessActionResult
	if err := json.NewDecoder(w.Body).Decode(&highValueAction); err != nil {
		t.Fatalf("decode high-value dry-run business action result: %v", err)
	}
	if highValueAction.Rules == nil || highValueAction.Rules.GovernanceStatus != "needs_review" || !highValueAction.Rules.RequiresApproval || len(highValueAction.Rules.MatchedRules) != 1 || highValueAction.Rules.MatchedRules[0].ID != "sales.order.high_value_approval" {
		t.Fatalf("expected high-value sales order approval rule: %#v", highValueAction.Rules)
	}
	if highValueAction.Rules.CanExecuteNow || highValueAction.Rules.RecommendedAction != "run_quality_check" {
		t.Fatalf("expected high-value sales order dry-run to recommend quality check next: %#v", highValueAction.Rules)
	}
	if !containsGateStatus(highValueAction.Rules.GateStatuses, "dry_run", "complete") || !containsGateStatus(highValueAction.Rules.GateStatuses, "approval", "pending") {
		t.Fatalf("expected high-value sales order gate statuses: %#v", highValueAction.Rules.GateStatuses)
	}
	if len(highValueAction.Rules.RuleEvaluations) == 0 || !highValueAction.Rules.RuleEvaluations[0].Applies || len(highValueAction.Rules.RuleEvaluations[0].ConditionResults) == 0 || !highValueAction.Rules.RuleEvaluations[0].ConditionResults[0].Matched {
		t.Fatalf("expected explainable high-value rule condition result: %#v", highValueAction.Rules.RuleEvaluations)
	}
	body = bytes.NewBufferString(`{"record_id":"PAY-2026-05-E001","dry_run":true,"data":{"payroll_month":"2026-05","employee_no":"E001","employee_name":"Alice","department":"Sales","gross_pay":20000,"tax":3000,"net_pay":17000,"status":"draft"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-actions/hr.payroll_upsert/execute", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("payroll dry-run business action status=%d body=%s", w.Code, w.Body.String())
	}
	var payrollAction ExecuteBusinessActionResult
	if err := json.NewDecoder(w.Body).Decode(&payrollAction); err != nil {
		t.Fatalf("decode payroll dry-run business action result: %v", err)
	}
	if payrollAction.Rules == nil || !payrollAction.Rules.RequiresApproval || !payrollAction.Rules.RequiresBackup || payrollAction.Rules.ApprovalKind != "hr_payroll" || !containsMatchedRule(payrollAction.Rules.MatchedRules, "hr.payroll.approval_required") {
		t.Fatalf("expected payroll approval and backup rule: %#v", payrollAction.Rules)
	}
	if !containsNextStepParamValue(payrollAction.Rules.NextSteps, "create_record_approval", "assigned_to", "hr_manager") || !containsGateStatus(payrollAction.Rules.GateStatuses, "backup", "pending") {
		t.Fatalf("expected payroll governed next steps: %#v", payrollAction.Rules)
	}

	body = bytes.NewBufferString(`{"record_id":"LV-2026-0001","dry_run":true,"data":{"leave_no":"LV-2026-0001","employee_no":"E001","employee_name":"Alice","department":"Sales","leave_type":"annual","start_date":"2026-05-20","end_date":"2026-05-21","days":2,"status":"submitted"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-actions/hr.leave_request_upsert/execute", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("leave dry-run business action status=%d body=%s", w.Code, w.Body.String())
	}
	var leaveAction ExecuteBusinessActionResult
	if err := json.NewDecoder(w.Body).Decode(&leaveAction); err != nil {
		t.Fatalf("decode leave dry-run business action result: %v", err)
	}
	if leaveAction.Rules == nil || !leaveAction.Rules.RequiresApproval || leaveAction.Rules.ApprovalKind != "hr_leave" || !containsMatchedRule(leaveAction.Rules.MatchedRules, "hr.leave.approval_required") {
		t.Fatalf("expected leave approval rule: %#v", leaveAction.Rules)
	}
	body = bytes.NewBufferString(`{"record_id":"PAYMENT-2026-0001","dry_run":true,"data":{"payment_no":"PAYMENT-2026-0001","counterparty":"Acme","payment_type":"receivable","amount":8800,"currency":"CNY","method":"bank_transfer","status":"planned","payment_date":"2026-05-05"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-actions/finance.payment_upsert/execute", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("payment dry-run business action status=%d body=%s", w.Code, w.Body.String())
	}
	var paymentAction ExecuteBusinessActionResult
	if err := json.NewDecoder(w.Body).Decode(&paymentAction); err != nil {
		t.Fatalf("decode payment dry-run business action result: %v", err)
	}
	if paymentAction.Rules == nil || !paymentAction.Rules.RequiresApproval || !paymentAction.Rules.RequiresBackup || paymentAction.Rules.ApprovalKind != "finance_payment" || !containsMatchedRule(paymentAction.Rules.MatchedRules, "finance.payment.approval_required") {
		t.Fatalf("expected payment approval and backup rule: %#v", paymentAction.Rules)
	}
	if !containsNextStepParamValue(paymentAction.Rules.NextSteps, "create_record_approval", "assigned_to", "finance_manager") || !containsGateStatus(paymentAction.Rules.GateStatuses, "backup", "pending") {
		t.Fatalf("expected payment governed next steps: %#v", paymentAction.Rules)
	}
	body = bytes.NewBufferString(`{"record_id":"BUD-2026-SALES-TRAVEL","dry_run":true,"data":{"budget_no":"BUD-2026-SALES-TRAVEL","period":"2026","department":"Sales","category":"travel","budget_amount":120000,"committed_amount":20000,"actual_amount":15000,"currency":"CNY","owner":"Finance","status":"submitted"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-actions/finance.budget_upsert/execute", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("budget dry-run business action status=%d body=%s", w.Code, w.Body.String())
	}
	var budgetAction ExecuteBusinessActionResult
	if err := json.NewDecoder(w.Body).Decode(&budgetAction); err != nil {
		t.Fatalf("decode budget dry-run business action result: %v", err)
	}
	if budgetAction.Rules == nil || !budgetAction.Rules.RequiresApproval || budgetAction.Rules.ApprovalKind != "finance_budget" || !containsMatchedRule(budgetAction.Rules.MatchedRules, "finance.budget.approval_required") {
		t.Fatalf("expected budget approval rule: %#v", budgetAction.Rules)
	}
	body = bytes.NewBufferString(`{"record_id":"VCH-2026-0001","dry_run":true,"data":{"voucher_no":"VCH-2026-0001","period":"2026-05","voucher_type":"receipt","summary":"Receive customer payment","debit_total":8800,"credit_total":8800,"currency":"CNY","status":"draft","lines":[{"account_code":"1001","debit":8800,"credit":0},{"account_code":"6001","debit":0,"credit":8800}]}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-actions/finance.voucher_upsert/execute", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("voucher dry-run business action status=%d body=%s", w.Code, w.Body.String())
	}
	var voucherAction ExecuteBusinessActionResult
	if err := json.NewDecoder(w.Body).Decode(&voucherAction); err != nil {
		t.Fatalf("decode voucher dry-run business action result: %v", err)
	}
	if voucherAction.Rules == nil || !voucherAction.Rules.RequiresApproval || !voucherAction.Rules.RequiresBackup || voucherAction.Rules.ApprovalKind != "finance_voucher" || !containsMatchedRule(voucherAction.Rules.MatchedRules, "finance.voucher.approval_required") {
		t.Fatalf("expected voucher approval and backup rule: %#v", voucherAction.Rules)
	}
	if !containsNextStepParamValue(voucherAction.Rules.NextSteps, "create_record_approval", "assigned_to", "finance_manager") || !containsGateStatus(voucherAction.Rules.GateStatuses, "quality", "pending") {
		t.Fatalf("expected voucher governed next steps: %#v", voucherAction.Rules)
	}
	body = bytes.NewBufferString(`{"record_id":"VCH-BAD-0001","dry_run":true,"data":{"voucher_no":"VCH-BAD-0001","period":"2026-05","voucher_type":"receipt","debit_total":8800,"credit_total":7800,"currency":"CNY","status":"draft","lines":[{"account_code":"1001","debit":8800,"credit":0},{"account_code":"6001","debit":0,"credit":7800}]}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-actions/finance.voucher_upsert/execute", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("invalid voucher dry-run business action status=%d body=%s", w.Code, w.Body.String())
	}
	var invalidVoucherAction ExecuteBusinessActionResult
	if err := json.NewDecoder(w.Body).Decode(&invalidVoucherAction); err != nil {
		t.Fatalf("decode invalid voucher dry-run business action result: %v", err)
	}
	if invalidVoucherAction.Valid || invalidVoucherAction.Validation == nil || !containsString(invalidVoucherAction.Validation.Errors, "voucher debit_total must equal credit_total") || !containsString(invalidVoucherAction.Validation.Errors, "voucher line debit sum must equal line credit sum") {
		t.Fatalf("expected voucher balancing validation errors: %#v", invalidVoucherAction.Validation)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/records/SO-DRY-0001", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("dry-run should not create record, status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"record_id":"SO-DRY-0001","dry_run":true,"data":{"order_no":"SO-DRY-0001","customer":"DryRunCo","amount":"not-a-number"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-actions/sales.order_upsert/execute", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("invalid dry-run business action status=%d body=%s", w.Code, w.Body.String())
	}
	var invalidDryRunAction ExecuteBusinessActionResult
	if err := json.NewDecoder(w.Body).Decode(&invalidDryRunAction); err != nil {
		t.Fatalf("decode invalid dry-run business action result: %v", err)
	}
	if !invalidDryRunAction.DryRun || invalidDryRunAction.Valid || invalidDryRunAction.Validation == nil || len(invalidDryRunAction.Validation.Errors) == 0 {
		t.Fatalf("expected invalid dry-run result: %#v", invalidDryRunAction)
	}

	body = bytes.NewBufferString(`{"record_id":"SO-2026-0002","title":"Business Action Order","data":{"order_no":"SO-2026-0002","customer":"Initech","amount":4300}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-actions/sales.order_upsert/execute", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("execute business action status=%d body=%s", w.Code, w.Body.String())
	}
	var actionResult ExecuteBusinessActionResult
	if err := json.NewDecoder(w.Body).Decode(&actionResult); err != nil {
		t.Fatalf("decode business action result: %v", err)
	}
	if actionResult.Event == nil || actionResult.Event.Record == nil || actionResult.Event.Record.Data["customer"] != "Initech" {
		t.Fatalf("unexpected business action result: %#v", actionResult)
	}
	if actionResult.Rules == nil || actionResult.Rules.GovernanceStatus != "clear" {
		t.Fatalf("expected business action governance rules: %#v", actionResult.Rules)
	}

	body = bytes.NewBufferString(`{"record_id":"SO-2026-0002","idempotency_key":"business:sales.orders:SO-2026-0002:stage1","data":{"stage":"won","payment_status":"paid"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-actions/sales.order_status_update/execute", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("execute business status action status=%d body=%s", w.Code, w.Body.String())
	}
	var statusActionResult ExecuteBusinessActionResult
	if err := json.NewDecoder(w.Body).Decode(&statusActionResult); err != nil {
		t.Fatalf("decode status business action: %v", err)
	}
	if statusActionResult.Event == nil || statusActionResult.Event.Status != "updated" || statusActionResult.Event.Record == nil {
		t.Fatalf("unexpected business status action result: %#v", statusActionResult)
	}
	if statusActionResult.Event.Record.Data["customer"] != "Initech" || statusActionResult.Event.Record.Data["stage"] != "won" {
		t.Fatalf("status update should merge existing data, got %#v", statusActionResult.Event.Record.Data)
	}
	if amount, ok := numberFromAny(statusActionResult.Event.Record.Data["amount"]); !ok || amount != 4300 {
		t.Fatalf("status update lost amount, got %#v", statusActionResult.Event.Record.Data)
	}
	body = bytes.NewBufferString(`{"record_id":"SO-2026-0003","title":"Business View Cursor Order","data":{"order_no":"SO-2026-0003","customer":"Initech","amount":4400,"order_date":"2026-05-06"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-actions/sales.order_upsert/execute", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("execute business action for view cursor status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"q":"Initech","limit":10}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/views/sales.order_overview/query", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("query business view status=%d body=%s", w.Code, w.Body.String())
	}
	var viewResult BusinessViewResult
	if err := json.NewDecoder(w.Body).Decode(&viewResult); err != nil {
		t.Fatalf("decode business view result: %v", err)
	}
	if viewResult.View.ID != "sales.order_overview" || len(viewResult.Records) == 0 {
		t.Fatalf("unexpected business view result: %#v", viewResult)
	}
	if _, ok := viewResult.Records[0].Data["customer"]; !ok {
		t.Fatalf("business view should include projected customer field: %#v", viewResult.Records[0].Data)
	}
	if _, ok := viewResult.Records[0].Data["gross_margin"]; ok {
		t.Fatalf("business view should project only declared fields: %#v", viewResult.Records[0].Data)
	}
	body = bytes.NewBufferString(`{"q":"Initech","limit":501}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/views/sales.order_overview/query", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected oversized business view limit rejection, status=%d body=%s", w.Code, w.Body.String())
	}
	body = bytes.NewBufferString(`{"q":"Initech","limit":1}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/views/sales.order_overview/query", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first business view cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstBusinessViewPage BusinessViewResult
	if err := json.NewDecoder(w.Body).Decode(&firstBusinessViewPage); err != nil {
		t.Fatalf("decode first business view cursor page: %v", err)
	}
	if len(firstBusinessViewPage.Records) != 1 || !firstBusinessViewPage.HasMore || firstBusinessViewPage.NextBefore == "" || firstBusinessViewPage.NextBeforeID == "" {
		t.Fatalf("unexpected first business view cursor page: %#v", firstBusinessViewPage)
	}
	body = bytes.NewBufferString(fmt.Sprintf(`{"q":"Initech","limit":1,"before":%q,"before_id":%q}`, firstBusinessViewPage.NextBefore, firstBusinessViewPage.NextBeforeID))
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/views/sales.order_overview/query", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second business view cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondBusinessViewPage BusinessViewResult
	if err := json.NewDecoder(w.Body).Decode(&secondBusinessViewPage); err != nil {
		t.Fatalf("decode second business view cursor page: %v", err)
	}
	if len(secondBusinessViewPage.Records) == 0 || secondBusinessViewPage.Records[0].ID == firstBusinessViewPage.Records[0].ID {
		t.Fatalf("unexpected second business view cursor page: first=%#v second=%#v", firstBusinessViewPage, secondBusinessViewPage)
	}

	body = bytes.NewBufferString(`{"sample_data":{"delivery_status":"shipped","gross_margin":1200},"reason":"new sales operations fields observed"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/schema-proposals", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("schema proposal status=%d body=%s", w.Code, w.Body.String())
	}
	var proposal SchemaProposal
	if err := json.NewDecoder(w.Body).Decode(&proposal); err != nil {
		t.Fatalf("decode schema proposal: %v", err)
	}
	if proposal.ID == "" || proposal.Status != "pending" || len(proposal.Suggested) != 2 {
		t.Fatalf("unexpected schema proposal: %#v", proposal)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/schema-proposals?status=pending", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list schema proposals status=%d body=%s", w.Code, w.Body.String())
	}
	var proposals ListResponse[SchemaProposal]
	if err := json.NewDecoder(w.Body).Decode(&proposals); err != nil {
		t.Fatalf("decode schema proposal list: %v", err)
	}
	if len(proposals.Items) != 1 || proposals.Items[0].ID != proposal.ID {
		t.Fatalf("unexpected schema proposal list: %#v", proposals)
	}
	body = bytes.NewBufferString(`{"sample_data":{"fulfillment_eta":"2026-05-08"},"reason":"second schema proposal for cursor test"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/schema-proposals", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second schema proposal status=%d body=%s", w.Code, w.Body.String())
	}
	var secondProposal SchemaProposal
	if err := json.NewDecoder(w.Body).Decode(&secondProposal); err != nil {
		t.Fatalf("decode second schema proposal: %v", err)
	}
	if secondProposal.ID == "" || secondProposal.ID == proposal.ID || secondProposal.Status != "pending" {
		t.Fatalf("unexpected second schema proposal: first=%#v second=%#v", proposal, secondProposal)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/schema-proposals?status=pending&limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first schema proposal cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstProposalPage ListResponse[SchemaProposal]
	if err := json.NewDecoder(w.Body).Decode(&firstProposalPage); err != nil {
		t.Fatalf("decode first schema proposal cursor page: %v", err)
	}
	if len(firstProposalPage.Items) != 1 || !firstProposalPage.HasMore || firstProposalPage.NextBefore == "" || firstProposalPage.NextBeforeID == "" {
		t.Fatalf("unexpected first schema proposal cursor page: %#v", firstProposalPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/schema-proposals?status=pending&limit=1&before="+url.QueryEscape(firstProposalPage.NextBefore)+"&before_id="+url.QueryEscape(firstProposalPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second schema proposal cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondProposalPage ListResponse[SchemaProposal]
	if err := json.NewDecoder(w.Body).Decode(&secondProposalPage); err != nil {
		t.Fatalf("decode second schema proposal cursor page: %v", err)
	}
	if len(secondProposalPage.Items) == 0 || secondProposalPage.Items[0].ID == firstProposalPage.Items[0].ID {
		t.Fatalf("unexpected second schema proposal cursor page: first=%#v second=%#v", firstProposalPage, secondProposalPage)
	}

	applyBody, err := json.Marshal(ApplySchemaProposalInput{ProposalID: proposal.ID, Confirm: false, Reason: "confirm gate test"})
	if err != nil {
		t.Fatalf("marshal schema apply: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/schema-proposals/apply", bytes.NewReader(applyBody))
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected schema apply without confirm to fail, status=%d body=%s", w.Code, w.Body.String())
	}

	applyBody, err = json.Marshal(ApplySchemaProposalInput{ProposalID: proposal.ID, Confirm: true, Reason: "approved schema improvement"})
	if err != nil {
		t.Fatalf("marshal confirmed schema apply: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/schema-proposals/apply", bytes.NewReader(applyBody))
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("schema apply status=%d body=%s", w.Code, w.Body.String())
	}
	var applied ApplySchemaProposalResult
	if err := json.NewDecoder(w.Body).Decode(&applied); err != nil {
		t.Fatalf("decode schema apply: %v", err)
	}
	if !containsFieldDefinition(applied.Applied, "delivery_status") || !containsFieldDefinition(applied.Applied, "gross_margin") {
		t.Fatalf("unexpected schema apply result: %#v", applied)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/schema-proposals/"+proposal.ID, nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get applied schema proposal status=%d body=%s", w.Code, w.Body.String())
	}
	var appliedProposal SchemaProposal
	if err := json.NewDecoder(w.Body).Decode(&appliedProposal); err != nil {
		t.Fatalf("decode applied schema proposal: %v", err)
	}
	if appliedProposal.Status != "applied" || appliedProposal.AppliedAt == nil {
		t.Fatalf("unexpected applied schema proposal: %#v", appliedProposal)
	}

	body = bytes.NewBufferString(`{"data":{"customer":"BadCo","amount":100,"gross_margin":"not-a-number","new_extra":"kept-flexible"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/validate", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("validate record status=%d body=%s", w.Code, w.Body.String())
	}
	var validation ValidateRecordResult
	if err := json.NewDecoder(w.Body).Decode(&validation); err != nil {
		t.Fatalf("decode validation: %v", err)
	}
	if validation.Valid || len(validation.Errors) == 0 || !containsString(validation.UnknownFields, "new_extra") {
		t.Fatalf("unexpected validation result: %#v", validation)
	}

	body = bytes.NewBufferString(`{"title":"Invalid margin","data":{"customer":"BadCo","amount":100,"gross_margin":"not-a-number"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected type validation failure, status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"group_by":["stage"],"metrics":[{"name":"orders","op":"count"},{"name":"amount","op":"sum","field":"amount"}]}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/aggregate", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("aggregate status=%d body=%s", w.Code, w.Body.String())
	}
	var aggregate AggregateResult
	if err := json.NewDecoder(w.Body).Decode(&aggregate); err != nil {
		t.Fatalf("decode aggregate: %v", err)
	}
	if len(aggregate.Rows) == 0 {
		t.Fatalf("unexpected aggregate: %#v", aggregate)
	}

	body = bytes.NewBufferString(`{"group_by":["customer"],"metrics":[{"as":"total_amount","op":"sum","field":"amount"}],"sort":[{"field":"total_amount","direction":"desc"}],"limit":1,"scan_limit":1}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/aggregate", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("aggregate alias/sort status=%d body=%s", w.Code, w.Body.String())
	}
	aggregate = AggregateResult{}
	if err := json.NewDecoder(w.Body).Decode(&aggregate); err != nil {
		t.Fatalf("decode alias aggregate: %v", err)
	}
	if aggregate.ScanLimit != 1 || !aggregate.Truncated || len(aggregate.Rows) != 1 || aggregate.Rows[0]["total_amount"] == nil {
		t.Fatalf("unexpected alias aggregate: %#v", aggregate)
	}
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "limit", body: `{"metrics":[{"name":"orders","op":"count"}],"limit":501}`},
		{name: "scan limit", body: `{"metrics":[{"name":"orders","op":"count"}],"scan_limit":5001}`},
	} {
		body = bytes.NewBufferString(tc.body)
		req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/aggregate", body)
		auth(req)
		w = httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected oversized aggregate %s rejection, status=%d body=%s", tc.name, w.Code, w.Body.String())
		}
	}

	body = bytes.NewBufferString(`{"group_by":["customer"],"metrics":[{"name":"amount","op":"sum","field":"amount"}],"sort":[{"field":"missing","direction":"asc"}]}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/aggregate", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid aggregate sort field, status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/audit?dataset_id=sales.orders&limit=50", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("audit status=%d body=%s", w.Code, w.Body.String())
	}
	var audit ListResponse[AuditLog]
	if err := json.NewDecoder(w.Body).Decode(&audit); err != nil {
		t.Fatalf("decode audit: %v", err)
	}
	foundRecordCreate := false
	foundSchemaApply := false
	for _, entry := range audit.Items {
		if entry.Action == "record.create" && entry.DatasetID == "sales.orders" {
			foundRecordCreate = true
		}
		if entry.Action == "schema.proposal_apply" && entry.DatasetID == "sales.orders" {
			foundSchemaApply = true
		}
	}
	if !foundRecordCreate || !foundSchemaApply {
		t.Fatalf("expected audit logs, foundRecordCreate=%v foundSchemaApply=%v audit=%#v", foundRecordCreate, foundSchemaApply, audit)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/audit?dataset_id=sales.orders&limit=2", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first audit cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstAuditPage ListResponse[AuditLog]
	if err := json.NewDecoder(w.Body).Decode(&firstAuditPage); err != nil {
		t.Fatalf("decode first audit cursor page: %v", err)
	}
	if len(firstAuditPage.Items) != 2 || !firstAuditPage.HasMore || firstAuditPage.NextBefore == "" || firstAuditPage.NextBeforeID == "" {
		t.Fatalf("unexpected first audit cursor page: %#v", firstAuditPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/audit?dataset_id=sales.orders&limit=2&before="+url.QueryEscape(firstAuditPage.NextBefore)+"&before_id="+url.QueryEscape(firstAuditPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second audit cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondAuditPage ListResponse[AuditLog]
	if err := json.NewDecoder(w.Body).Decode(&secondAuditPage); err != nil {
		t.Fatalf("decode second audit cursor page: %v", err)
	}
	if len(secondAuditPage.Items) == 0 {
		t.Fatalf("expected second audit cursor page to continue: %#v", secondAuditPage)
	}
	seenAuditIDs := map[string]struct{}{}
	for _, item := range firstAuditPage.Items {
		seenAuditIDs[item.ID] = struct{}{}
	}
	for _, item := range secondAuditPage.Items {
		if _, ok := seenAuditIDs[item.ID]; ok {
			t.Fatalf("audit cursor repeated %s across pages: first=%#v second=%#v", item.ID, firstAuditPage, secondAuditPage)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/audit?dataset_id=sales.orders&user_id=user_1&target_type=record&target_id=SO-2026-0001&q=Created&limit=10", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("filtered audit status=%d body=%s", w.Code, w.Body.String())
	}
	var filteredAudit ListResponse[AuditLog]
	if err := json.NewDecoder(w.Body).Decode(&filteredAudit); err != nil {
		t.Fatalf("decode filtered audit: %v", err)
	}
	if len(filteredAudit.Items) == 0 || filteredAudit.Items[0].TargetID != "SO-2026-0001" || filteredAudit.Items[0].UserID != "user_1" {
		t.Fatalf("unexpected filtered audit: %#v", filteredAudit)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/records/SO-2026-0001/timeline?limit=20", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("record timeline status=%d body=%s", w.Code, w.Body.String())
	}
	var timeline RecordTimelineResult
	if err := json.NewDecoder(w.Body).Decode(&timeline); err != nil {
		t.Fatalf("decode record timeline: %v", err)
	}
	foundTimelineAudit := false
	foundTimelineEvent := false
	foundTimelineBusinessAction := false
	foundTimelineRevision := false
	for _, item := range timeline.Items {
		if item.Type == "audit" && item.Action == "record.create" {
			foundTimelineAudit = true
		}
		if item.Type == "event" && item.Action == "sales.order.updated" {
			foundTimelineEvent = true
		}
		if item.Type == "event" && item.Metadata["business_action_id"] == "sales.order_status_update" {
			foundTimelineBusinessAction = true
		}
		if item.Type == "revision" {
			foundTimelineRevision = true
		}
	}
	if timeline.DatasetID != "sales.orders" || timeline.RecordID != "SO-2026-0001" || !foundTimelineAudit || !foundTimelineEvent || !foundTimelineBusinessAction || !foundTimelineRevision {
		t.Fatalf("unexpected record timeline: %#v", timeline)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/records/SO-2026-0001/timeline?limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first timeline cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstTimelinePage RecordTimelineResult
	if err := json.NewDecoder(w.Body).Decode(&firstTimelinePage); err != nil {
		t.Fatalf("decode first timeline cursor page: %v", err)
	}
	if len(firstTimelinePage.Items) != 1 || !firstTimelinePage.HasMore || firstTimelinePage.NextBefore == "" || firstTimelinePage.NextBeforeID == "" {
		t.Fatalf("unexpected first timeline cursor page: %#v", firstTimelinePage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/records/SO-2026-0001/timeline?limit=1&before="+url.QueryEscape(firstTimelinePage.NextBefore)+"&before_id="+url.QueryEscape(firstTimelinePage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second timeline cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondTimelinePage RecordTimelineResult
	if err := json.NewDecoder(w.Body).Decode(&secondTimelinePage); err != nil {
		t.Fatalf("decode second timeline cursor page: %v", err)
	}
	if len(secondTimelinePage.Items) == 0 || secondTimelinePage.Items[0].ID == firstTimelinePage.Items[0].ID {
		t.Fatalf("unexpected second timeline cursor page: first=%#v second=%#v", firstTimelinePage, secondTimelinePage)
	}

	body = bytes.NewBufferString(`{"kind":"sales_order","priority":"high","assigned_to":"manager_1","due_at":"2020-01-01","summary":"Approve confirmed sales order","request":{"amount":1200},"workflow_skill_id":"approval.sales_order","workflow_version":"1.0.0","workflow_instance_id":"wf-so-1","workflow_node_id":"submit","workflow_decision_id":"dec-submit","business_status":"pending_manager","result_status":"requires_input","result_payload":{"approval_result":"requires_input","business_record":{"id":"SO-2026-0001","status":"pending_manager"}},"outputs":[{"type":"text","title":"Current state","text":"Waiting for manager"}],"artifacts":[{"id":"draft-pdf","name":"draft.pdf","uri":"artifact://draft","mime_type":"application/pdf","status":"draft"}]}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/SO-2026-0001/approvals", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create approval status=%d body=%s", w.Code, w.Body.String())
	}
	var approval RecordApproval
	if err := json.NewDecoder(w.Body).Decode(&approval); err != nil {
		t.Fatalf("decode approval: %v", err)
	}
	if approval.ID == "" || approval.Status != recordApprovalStatusPending || approval.DatasetID != "sales.orders" || approval.RecordID != "SO-2026-0001" || approval.Priority != "high" || approval.AssignedTo != "manager_1" || approval.DueAt.IsZero() {
		t.Fatalf("unexpected approval: %#v", approval)
	}
	if approval.WorkflowSkillID != "approval.sales_order" || approval.WorkflowInstanceID != "wf-so-1" || approval.BusinessStatus != "pending_manager" || approval.ResultStatus != "requires_input" {
		t.Fatalf("approval workflow fields were not persisted: %#v", approval)
	}
	if approval.ResultPayload["approval_result"] != "requires_input" || len(approval.Outputs) != 1 || approval.Outputs[0].Text != "Waiting for manager" || len(approval.Artifacts) != 1 || approval.Artifacts[0].Name != "draft.pdf" {
		t.Fatalf("approval result package was not persisted: %#v", approval)
	}
	body = bytes.NewBufferString(`{"kind":"sales_order","priority":"urgent","summary":"Invalid priority should fail","request":{"amount":1200}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/SO-2026-0001/approvals", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid approval priority status=%d body=%s", w.Code, w.Body.String())
	}
	body = bytes.NewBufferString(`{"kind":"sales_order","priority":"high","assigned_to":"manager_1","summary":"Duplicate approval should reuse","request":{"amount":1200}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/SO-2026-0001/approvals", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("reuse approval status=%d body=%s", w.Code, w.Body.String())
	}
	var reusedApproval RecordApproval
	if err := json.NewDecoder(w.Body).Decode(&reusedApproval); err != nil {
		t.Fatalf("decode reused approval: %v", err)
	}
	if reusedApproval.ID != approval.ID || !reusedApproval.Reused {
		t.Fatalf("expected pending approval reuse: original=%#v reused=%#v", approval, reusedApproval)
	}
	body = bytes.NewBufferString(`{"business_action_id":"sales.order_upsert","record_id":"SO-2026-0001","data":{"order_no":"SO-2026-0001","customer":"Acme","amount":150000}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-rules/evaluate", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("pending approval rule evaluation status=%d body=%s", w.Code, w.Body.String())
	}
	var pendingApprovalRuleEval BusinessRuleEvaluation
	if err := json.NewDecoder(w.Body).Decode(&pendingApprovalRuleEval); err != nil {
		t.Fatalf("decode pending approval rule evaluation: %v", err)
	}
	if pendingApprovalRuleEval.ApprovalID != approval.ID || pendingApprovalRuleEval.ApprovalStatus != recordApprovalStatusPending || !containsGateStatus(pendingApprovalRuleEval.GateStatuses, "approval", "pending") || !containsNextStepParamValue(pendingApprovalRuleEval.NextSteps, "get_record_approval", "approval_id", approval.ID) {
		t.Fatalf("expected pending approval to be reflected in rule evaluation: %#v", pendingApprovalRuleEval)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/inbox?dataset_id=sales.orders&type=approval&limit=10", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("inbox status=%d body=%s", w.Code, w.Body.String())
	}
	var inbox MISInboxResult
	if err := json.NewDecoder(w.Body).Decode(&inbox); err != nil {
		t.Fatalf("decode inbox: %v", err)
	}
	var inboxApproval *MISInboxItem
	for i := range inbox.Items {
		item := &inbox.Items[i]
		if item.Type == "approval" && item.ID == approval.ID && item.Status == recordApprovalStatusPending {
			inboxApproval = item
			break
		}
	}
	if inboxApproval == nil {
		t.Fatalf("expected approval in inbox: %#v", inbox)
	}
	if inboxApproval.Metadata["workflow_node_id"] != "submit" || inboxApproval.Metadata["result_status"] != "requires_input" {
		t.Fatalf("inbox approval missing workflow metadata: %#v", inboxApproval.Metadata)
	}
	if payload, ok := inboxApproval.Metadata["result_payload"].(map[string]any); !ok || payload["approval_result"] != "requires_input" {
		t.Fatalf("inbox approval missing result payload metadata: %#v", inboxApproval.Metadata)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/inbox/summary?dataset_id=sales.orders&type=approval&limit=10", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("inbox summary status=%d body=%s", w.Code, w.Body.String())
	}
	var inboxSummary MISInboxSummary
	if err := json.NewDecoder(w.Body).Decode(&inboxSummary); err != nil {
		t.Fatalf("decode inbox summary: %v", err)
	}
	if inboxSummary.Total == 0 || inboxSummary.ByType["approval"] == 0 || inboxSummary.High == 0 || inboxSummary.Overdue == 0 {
		t.Fatalf("unexpected inbox summary: %#v", inboxSummary)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/inbox?type=approvla", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid inbox type status=%d body=%s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/inbox/summary?status=pendng", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid inbox summary status filter status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/approvals?dataset_id=sales.orders&assigned_to=manager_1&overdue=true&limit=10", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("overdue approvals status=%d body=%s", w.Code, w.Body.String())
	}
	var overdueApprovals ListResponse[RecordApproval]
	if err := json.NewDecoder(w.Body).Decode(&overdueApprovals); err != nil {
		t.Fatalf("decode overdue approvals: %v", err)
	}
	if len(overdueApprovals.Items) != 1 || overdueApprovals.Items[0].ID != approval.ID {
		t.Fatalf("unexpected overdue approvals: %#v", overdueApprovals)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/approvals?dataset_id=sales.orders&record_id=SO-2026-0001&status=pending&limit=10", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list approvals status=%d body=%s", w.Code, w.Body.String())
	}
	var approvals ListResponse[RecordApproval]
	if err := json.NewDecoder(w.Body).Decode(&approvals); err != nil {
		t.Fatalf("decode approvals: %v", err)
	}
	if len(approvals.Items) != 1 || approvals.Items[0].ID != approval.ID {
		t.Fatalf("unexpected approvals: %#v", approvals)
	}
	if approvals.Items[0].ResultPayload["approval_result"] != "requires_input" || len(approvals.Items[0].Outputs) != 1 || len(approvals.Items[0].Artifacts) != 1 {
		t.Fatalf("approval list did not include result package: %#v", approvals.Items[0])
	}
	body = bytes.NewBufferString(`{"kind":"sales_discount","priority":"medium","assigned_to":"manager_1","summary":"Approve discount","request":{"discount":5}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/SO-2026-0001/approvals", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create second approval status=%d body=%s", w.Code, w.Body.String())
	}
	var secondApproval RecordApproval
	if err := json.NewDecoder(w.Body).Decode(&secondApproval); err != nil {
		t.Fatalf("decode second approval: %v", err)
	}
	if secondApproval.ID == "" || secondApproval.ID == approval.ID || secondApproval.Status != recordApprovalStatusPending {
		t.Fatalf("unexpected second approval: first=%#v second=%#v", approval, secondApproval)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/approvals?dataset_id=sales.orders&record_id=SO-2026-0001&status=pending&limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first approval cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstApprovalPage ListResponse[RecordApproval]
	if err := json.NewDecoder(w.Body).Decode(&firstApprovalPage); err != nil {
		t.Fatalf("decode first approval cursor page: %v", err)
	}
	if len(firstApprovalPage.Items) != 1 || !firstApprovalPage.HasMore || firstApprovalPage.NextBefore == "" || firstApprovalPage.NextBeforeID == "" {
		t.Fatalf("unexpected first approval cursor page: %#v", firstApprovalPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/approvals?dataset_id=sales.orders&record_id=SO-2026-0001&status=pending&limit=1&before="+url.QueryEscape(firstApprovalPage.NextBefore)+"&before_id="+url.QueryEscape(firstApprovalPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second approval cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondApprovalPage ListResponse[RecordApproval]
	if err := json.NewDecoder(w.Body).Decode(&secondApprovalPage); err != nil {
		t.Fatalf("decode second approval cursor page: %v", err)
	}
	if len(secondApprovalPage.Items) == 0 || secondApprovalPage.Items[0].ID == firstApprovalPage.Items[0].ID {
		t.Fatalf("unexpected second approval cursor page: first=%#v second=%#v", firstApprovalPage, secondApprovalPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/inbox?dataset_id=sales.orders&type=approval&limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first inbox cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstInboxPage MISInboxResult
	if err := json.NewDecoder(w.Body).Decode(&firstInboxPage); err != nil {
		t.Fatalf("decode first inbox cursor page: %v", err)
	}
	if len(firstInboxPage.Items) != 1 || !firstInboxPage.HasMore || firstInboxPage.NextBefore == "" || firstInboxPage.NextBeforeID == "" || firstInboxPage.Items[0].Type != "approval" {
		t.Fatalf("unexpected first inbox cursor page: %#v", firstInboxPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/inbox?dataset_id=sales.orders&type=approval&limit=1&before="+url.QueryEscape(firstInboxPage.NextBefore)+"&before_id="+url.QueryEscape(firstInboxPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second inbox cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondInboxPage MISInboxResult
	if err := json.NewDecoder(w.Body).Decode(&secondInboxPage); err != nil {
		t.Fatalf("decode second inbox cursor page: %v", err)
	}
	if len(secondInboxPage.Items) == 0 || secondInboxPage.Items[0].ID == firstInboxPage.Items[0].ID || secondInboxPage.Items[0].Type != "approval" {
		t.Fatalf("unexpected second inbox cursor page: first=%#v second=%#v", firstInboxPage, secondInboxPage)
	}

	body = bytes.NewBufferString(`{"decision":"approve","reason":"sales manager approved","workflow_node_id":"manager_review","workflow_decision_id":"dec-manager","business_status":"approved","result_status":"completed","result_payload":{"approval_result":"approved","business_status":"approved","business_record":{"id":"SO-2026-0001","status":"approved"},"text":"approved"},"outputs":[{"type":"text","title":"Approval","text":"Approved"},{"type":"artifact","artifact_id":"approval-pdf","artifact":{"id":"approval-pdf","name":"approval.pdf","uri":"artifact://approval/1","status":"ready"}}],"artifacts":[{"id":"approval-pdf","name":"approval.pdf","uri":"artifact://approval/1","status":"ready"}]}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/approvals/"+approval.ID+"/review", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("review approval status=%d body=%s", w.Code, w.Body.String())
	}
	var reviewedApproval RecordApproval
	if err := json.NewDecoder(w.Body).Decode(&reviewedApproval); err != nil {
		t.Fatalf("decode reviewed approval: %v", err)
	}
	if reviewedApproval.Status != recordApprovalStatusApproved || reviewedApproval.ReviewedBy == "" || reviewedApproval.ReviewedAt.IsZero() {
		t.Fatalf("unexpected reviewed approval: %#v", reviewedApproval)
	}
	if reviewedApproval.WorkflowNodeID != "manager_review" || reviewedApproval.WorkflowDecisionID != "dec-manager" || reviewedApproval.BusinessStatus != "approved" || reviewedApproval.ResultStatus != "completed" {
		t.Fatalf("reviewed approval workflow result was not persisted: %#v", reviewedApproval)
	}
	if reviewedApproval.ResultPayload["approval_result"] != "approved" || reviewedApproval.ResultPayload["text"] != "approved" || len(reviewedApproval.Outputs) != 2 || reviewedApproval.Outputs[1].Artifact == nil || reviewedApproval.Outputs[1].Artifact.Name != "approval.pdf" || len(reviewedApproval.Artifacts) != 1 {
		t.Fatalf("reviewed approval result package was not persisted: %#v", reviewedApproval)
	}
	gotReviewedApproval := RecordApproval{}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/approvals/"+reviewedApproval.ID, nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get reviewed approval status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&gotReviewedApproval); err != nil {
		t.Fatalf("decode reviewed approval detail: %v", err)
	}
	if gotReviewedApproval.ResultPayload["approval_result"] != "approved" || len(gotReviewedApproval.Outputs) != 2 || len(gotReviewedApproval.Artifacts) != 1 {
		t.Fatalf("reviewed approval detail lost result package: %#v", gotReviewedApproval)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/records/SO-2026-0001/timeline?limit=10", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("timeline after approval review status=%d body=%s", w.Code, w.Body.String())
	}
	var reviewedTimeline RecordTimelineResult
	if err := json.NewDecoder(w.Body).Decode(&reviewedTimeline); err != nil {
		t.Fatalf("decode reviewed timeline: %v", err)
	}
	var approvalTimelineItem *RecordTimelineItem
	for i := range reviewedTimeline.Items {
		item := &reviewedTimeline.Items[i]
		if item.Type == "approval" && item.ID == reviewedApproval.ID {
			approvalTimelineItem = item
			break
		}
	}
	if approvalTimelineItem == nil {
		t.Fatalf("timeline missing reviewed approval item: %#v", reviewedTimeline.Items)
	}
	if approvalTimelineItem.Metadata["workflow_node_id"] != "manager_review" || approvalTimelineItem.Metadata["result_status"] != "completed" {
		t.Fatalf("timeline approval missing workflow metadata: %#v", approvalTimelineItem.Metadata)
	}
	if payload, ok := approvalTimelineItem.Metadata["result_payload"].(map[string]any); !ok || payload["approval_result"] != "approved" {
		t.Fatalf("timeline approval missing result payload metadata: %#v", approvalTimelineItem.Metadata)
	}
	body = bytes.NewBufferString(`{"business_action_id":"sales.order_upsert","record_id":"SO-2026-0001","dry_run":true,"data":{"order_no":"SO-2026-0001","customer":"Acme","amount":150000}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-rules/evaluate", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("approved approval rule evaluation status=%d body=%s", w.Code, w.Body.String())
	}
	var approvedApprovalRuleEval BusinessRuleEvaluation
	if err := json.NewDecoder(w.Body).Decode(&approvedApprovalRuleEval); err != nil {
		t.Fatalf("decode approved approval rule evaluation: %v", err)
	}
	if approvedApprovalRuleEval.ApprovalID != approval.ID || approvedApprovalRuleEval.ApprovalStatus != recordApprovalStatusApproved || !containsGateStatus(approvedApprovalRuleEval.GateStatuses, "approval", "complete") {
		t.Fatalf("expected approved approval to complete approval gate: %#v", approvedApprovalRuleEval)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/records/SO-2026-0001/timeline?limit=20", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("approval timeline status=%d body=%s", w.Code, w.Body.String())
	}
	var approvalTimeline RecordTimelineResult
	if err := json.NewDecoder(w.Body).Decode(&approvalTimeline); err != nil {
		t.Fatalf("decode approval timeline: %v", err)
	}
	foundTimelineApproval := false
	for _, item := range approvalTimeline.Items {
		if item.Type == "approval" && item.ID == approval.ID && item.Action == recordApprovalStatusApproved {
			foundTimelineApproval = true
			break
		}
	}
	if !foundTimelineApproval {
		t.Fatalf("expected approval in timeline: %#v", approvalTimeline)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/audit/export.csv?dataset_id=sales.orders&user_id=user_1&target_type=record&target_id=SO-2026-0001&q=Created&limit=10", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("audit csv status=%d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "text/csv") {
		t.Fatalf("expected audit csv content type, got %q", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected audit csv nosniff header, got %q", got)
	}
	auditCSV := w.Body.String()
	if !strings.Contains(auditCSV, "id,created_at,tenant_id,user_id,action") || !strings.Contains(auditCSV, "SO-2026-0001") || !strings.Contains(auditCSV, "record.create") {
		t.Fatalf("unexpected audit csv: %s", auditCSV)
	}

	body = bytes.NewBufferString(`{"dry_run":true,"records":[{"id":"BATCH-BAD","data":{"customer":"BatchBad","gross_margin":"bad"}}]}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/batch", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("batch dry-run status=%d body=%s", w.Code, w.Body.String())
	}
	var dryRun BatchImportRecordsResult
	if err := json.NewDecoder(w.Body).Decode(&dryRun); err != nil {
		t.Fatalf("decode batch dry-run: %v", err)
	}
	if dryRun.Valid || !dryRun.DryRun || dryRun.Imported != 0 || len(dryRun.Validations) != 1 {
		t.Fatalf("unexpected batch dry-run: %#v", dryRun)
	}

	body = bytes.NewBufferString(`{"records":[{"id":"BATCH-OK-1","title":"Batch order 1","tags":["batch"],"data":{"customer":"BatchCo","amount":510,"gross_margin":50}},{"id":"BATCH-OK-2","title":"Batch order 2","tags":["batch"],"data":{"customer":"BatchCo","amount":620,"gross_margin":60}}]}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/batch", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("batch import status=%d body=%s", w.Code, w.Body.String())
	}
	var batch BatchImportRecordsResult
	if err := json.NewDecoder(w.Body).Decode(&batch); err != nil {
		t.Fatalf("decode batch import: %v", err)
	}
	if !batch.Valid || batch.Imported != 2 || len(batch.Records) != 2 {
		t.Fatalf("unexpected batch import: %#v", batch)
	}
	for _, tc := range []struct {
		name        string
		path        string
		contentType string
		body        string
	}{
		{name: "batch direct", path: "/api/v1/data/datasets/sales.orders/records/batch", contentType: "application/json", body: oversizedBatchImportJSON()},
		{name: "batch job", path: "/api/v1/data/datasets/sales.orders/records/batch/jobs", contentType: "application/json", body: oversizedBatchImportJSON()},
		{name: "csv direct", path: "/api/v1/data/datasets/sales.orders/records/import.csv", contentType: "application/json", body: oversizedCSVImportJSON()},
		{name: "csv job", path: "/api/v1/data/datasets/sales.orders/records/import.csv/jobs", contentType: "application/json", body: oversizedCSVImportJSON()},
		{name: "jsonl direct", path: "/api/v1/data/datasets/sales.orders/records/import.jsonl", contentType: "application/json", body: oversizedJSONLImportJSON()},
		{name: "jsonl job", path: "/api/v1/data/datasets/sales.orders/records/import.jsonl/jobs", contentType: "application/json", body: oversizedJSONLImportJSON()},
	} {
		body = bytes.NewBufferString(tc.body)
		req = httptest.NewRequest(http.MethodPost, tc.path, body)
		req.Header.Set("Content-Type", tc.contentType)
		auth(req)
		w = httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s oversized import status=%d body=%s", tc.name, w.Code, w.Body.String())
		}
	}

	body = bytes.NewBufferString(`{"csv":"id,order_no,customer,amount,stage,tags\nCSV-OK-1,CSV-OK-1,CsvCo,710,confirmed,\"csv|import\"\n","dry_run":true}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/import.csv", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("csv dry-run status=%d body=%s", w.Code, w.Body.String())
	}
	var csvDryRun BatchImportRecordsResult
	if err := json.NewDecoder(w.Body).Decode(&csvDryRun); err != nil {
		t.Fatalf("decode csv dry-run: %v", err)
	}
	if !csvDryRun.Valid || !csvDryRun.DryRun || csvDryRun.Imported != 0 {
		t.Fatalf("unexpected csv dry-run: %#v", csvDryRun)
	}

	body = bytes.NewBufferString("id,order_no,customer,amount,stage,tags\nCSV-OK-1,CSV-OK-1,CsvCo,710,confirmed,csv|import\n")
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/import.csv", body)
	req.Header.Set("Content-Type", "text/csv")
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("csv import status=%d body=%s", w.Code, w.Body.String())
	}
	var csvImport BatchImportRecordsResult
	if err := json.NewDecoder(w.Body).Decode(&csvImport); err != nil {
		t.Fatalf("decode csv import: %v", err)
	}
	if !csvImport.Valid || csvImport.Imported != 1 || csvImport.Records[0].Data["customer"] != "CsvCo" {
		t.Fatalf("unexpected csv import: %#v", csvImport)
	}

	body = bytes.NewBufferString(`{"jsonl":"{\"id\":\"JSONL-DRY-1\",\"data\":{\"order_no\":\"JSONL-DRY-1\",\"customer\":\"JsonlDry\",\"amount\":111,\"stage\":\"confirmed\"}}\n","dry_run":true}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/import.jsonl", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("jsonl dry-run status=%d body=%s", w.Code, w.Body.String())
	}
	var jsonlDryRun BatchImportRecordsResult
	if err := json.NewDecoder(w.Body).Decode(&jsonlDryRun); err != nil {
		t.Fatalf("decode jsonl dry-run: %v", err)
	}
	if !jsonlDryRun.Valid || !jsonlDryRun.DryRun || jsonlDryRun.Imported != 0 {
		t.Fatalf("unexpected jsonl dry-run: %#v", jsonlDryRun)
	}

	body = bytes.NewBufferString("{\"id\":\"JSONL-OK-1\",\"title\":\"Jsonl order\",\"tags\":[\"jsonl\"],\"data\":{\"order_no\":\"JSONL-OK-1\",\"customer\":\"JsonlCo\",\"amount\":920,\"stage\":\"confirmed\"}}\n")
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/import.jsonl", body)
	req.Header.Set("Content-Type", "application/x-ndjson")
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("jsonl import status=%d body=%s", w.Code, w.Body.String())
	}
	var jsonlImport BatchImportRecordsResult
	if err := json.NewDecoder(w.Body).Decode(&jsonlImport); err != nil {
		t.Fatalf("decode jsonl import: %v", err)
	}
	if !jsonlImport.Valid || jsonlImport.Imported != 1 || jsonlImport.Records[0].Data["customer"] != "JsonlCo" {
		t.Fatalf("unexpected jsonl import: %#v", jsonlImport)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/records/import-template.csv", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("csv template status=%d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "text/csv") {
		t.Fatalf("expected csv template content type, got %q", got)
	}
	csvTemplate := w.Body.String()
	if !strings.Contains(csvTemplate, "id,title,tags,source_id") || !strings.Contains(csvTemplate, "order_no") || !strings.Contains(csvTemplate, "amount") {
		t.Fatalf("unexpected csv template: %s", csvTemplate)
	}

	body = bytes.NewBufferString(`{"csv":"id,order_no,customer,amount,stage\nCSV-JOB-1,CSV-JOB-1,JobCo,810,confirmed\n","dry_run":false}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/import.csv/jobs", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("csv import job status=%d body=%s", w.Code, w.Body.String())
	}
	var importJob ImportJob
	if err := json.NewDecoder(w.Body).Decode(&importJob); err != nil {
		t.Fatalf("decode csv import job: %v", err)
	}
	if importJob.ID == "" || importJob.Status != importJobStatusQueued {
		t.Fatalf("unexpected queued import job: %#v", importJob)
	}
	var completedJob ImportJob
	for i := 0; i < 50; i++ {
		req = httptest.NewRequest(http.MethodGet, "/api/v1/data/import-jobs/"+importJob.ID, nil)
		auth(req)
		w = httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("get import job status=%d body=%s", w.Code, w.Body.String())
		}
		if err := json.NewDecoder(w.Body).Decode(&completedJob); err != nil {
			t.Fatalf("decode completed import job: %v", err)
		}
		if completedJob.Status == importJobStatusCompleted || completedJob.Status == importJobStatusFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if completedJob.Status != importJobStatusCompleted || completedJob.Imported != 1 || completedJob.Result == nil || completedJob.Result.Imported != 1 {
		t.Fatalf("unexpected completed import job: %#v", completedJob)
	}

	body = bytes.NewBufferString(`{"jsonl":"{\"id\":\"JSONL-JOB-1\",\"data\":{\"order_no\":\"JSONL-JOB-1\",\"customer\":\"JsonlJobCo\",\"amount\":930,\"stage\":\"confirmed\"}}\n","dry_run":false}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/import.jsonl/jobs", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("jsonl import job status=%d body=%s", w.Code, w.Body.String())
	}
	var jsonlJob ImportJob
	if err := json.NewDecoder(w.Body).Decode(&jsonlJob); err != nil {
		t.Fatalf("decode jsonl import job: %v", err)
	}
	if jsonlJob.ID == "" || jsonlJob.Kind != "jsonl" || jsonlJob.Total != 1 {
		t.Fatalf("unexpected queued jsonl import job: %#v", jsonlJob)
	}
	var completedJSONLJob ImportJob
	for i := 0; i < 50; i++ {
		req = httptest.NewRequest(http.MethodGet, "/api/v1/data/import-jobs/"+jsonlJob.ID, nil)
		auth(req)
		w = httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("get jsonl import job status=%d body=%s", w.Code, w.Body.String())
		}
		if err := json.NewDecoder(w.Body).Decode(&completedJSONLJob); err != nil {
			t.Fatalf("decode completed jsonl import job: %v", err)
		}
		if completedJSONLJob.Status == importJobStatusCompleted || completedJSONLJob.Status == importJobStatusFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if completedJSONLJob.Status != importJobStatusCompleted || completedJSONLJob.Imported != 1 || completedJSONLJob.Result == nil || completedJSONLJob.Result.Records[0].Data["customer"] != "JsonlJobCo" {
		t.Fatalf("unexpected completed jsonl import job: %#v", completedJSONLJob)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/import-jobs?dataset_id=sales.orders&limit=10", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list import jobs status=%d body=%s", w.Code, w.Body.String())
	}
	var importJobs ListResponse[ImportJob]
	if err := json.NewDecoder(w.Body).Decode(&importJobs); err != nil {
		t.Fatalf("decode import jobs: %v", err)
	}
	if len(importJobs.Items) == 0 || importJobs.Items[0].ID == "" {
		t.Fatalf("unexpected import jobs: %#v", importJobs)
	}

	body = bytes.NewBufferString(`{"records":[{"id":"BATCH-JOB-1","title":"Batch job order","tags":["job"],"data":{"customer":"BatchJobCo","amount":910,"stage":"confirmed"}}]}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/batch/jobs", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("batch import job status=%d body=%s", w.Code, w.Body.String())
	}
	var batchJob ImportJob
	if err := json.NewDecoder(w.Body).Decode(&batchJob); err != nil {
		t.Fatalf("decode batch import job: %v", err)
	}
	if batchJob.ID == "" || batchJob.Kind != "batch" || batchJob.Total != 1 {
		t.Fatalf("unexpected queued batch import job: %#v", batchJob)
	}
	var completedBatchJob ImportJob
	for i := 0; i < 50; i++ {
		req = httptest.NewRequest(http.MethodGet, "/api/v1/data/import-jobs/"+batchJob.ID, nil)
		auth(req)
		w = httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("get batch import job status=%d body=%s", w.Code, w.Body.String())
		}
		if err := json.NewDecoder(w.Body).Decode(&completedBatchJob); err != nil {
			t.Fatalf("decode completed batch import job: %v", err)
		}
		if completedBatchJob.Status == importJobStatusCompleted || completedBatchJob.Status == importJobStatusFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if completedBatchJob.Status != importJobStatusCompleted || completedBatchJob.Imported != 1 || completedBatchJob.Result == nil || completedBatchJob.Result.Records[0].Data["customer"] != "BatchJobCo" {
		t.Fatalf("unexpected completed batch import job: %#v", completedBatchJob)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/import-jobs?dataset_id=sales.orders&limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first import job cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstImportJobPage ListResponse[ImportJob]
	if err := json.NewDecoder(w.Body).Decode(&firstImportJobPage); err != nil {
		t.Fatalf("decode first import job cursor page: %v", err)
	}
	if len(firstImportJobPage.Items) != 1 || !firstImportJobPage.HasMore || firstImportJobPage.NextBefore == "" || firstImportJobPage.NextBeforeID == "" {
		t.Fatalf("unexpected first import job cursor page: %#v", firstImportJobPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/import-jobs?dataset_id=sales.orders&limit=1&before="+url.QueryEscape(firstImportJobPage.NextBefore)+"&before_id="+url.QueryEscape(firstImportJobPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second import job cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondImportJobPage ListResponse[ImportJob]
	if err := json.NewDecoder(w.Body).Decode(&secondImportJobPage); err != nil {
		t.Fatalf("decode second import job cursor page: %v", err)
	}
	if len(secondImportJobPage.Items) == 0 || secondImportJobPage.Items[0].ID == firstImportJobPage.Items[0].ID {
		t.Fatalf("unexpected second import job cursor page: first=%#v second=%#v", firstImportJobPage, secondImportJobPage)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/stats", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("stats status=%d body=%s", w.Code, w.Body.String())
	}
	var stats SystemStats
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if stats.SchemaVersion == 0 || stats.DatasetCount == 0 || stats.RecordCount == 0 || stats.ImportJobs[importJobStatusCompleted] < 2 {
		t.Fatalf("unexpected stats: %#v", stats)
	}

	body = bytes.NewBufferString(`{"tasks":["integrity_check","optimize"]}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/maintenance/run", body)
	authRole(req, "data_user")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected maintenance forbidden, status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"tasks":["integrity_check","optimize"]}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/maintenance/run", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("maintenance status=%d body=%s", w.Code, w.Body.String())
	}
	var maintenance MaintenanceResult
	if err := json.NewDecoder(w.Body).Decode(&maintenance); err != nil {
		t.Fatalf("decode maintenance: %v", err)
	}
	if !maintenance.Valid || len(maintenance.Tasks) != 2 || maintenance.Tasks[0].Status != "ok" {
		t.Fatalf("unexpected maintenance: %#v", maintenance)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/quality-checks", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list quality checks status=%d body=%s", w.Code, w.Body.String())
	}
	var checksResp ListResponse[QualityCheckDefinition]
	if err := json.NewDecoder(w.Body).Decode(&checksResp); err != nil {
		t.Fatalf("decode quality checks: %v", err)
	}
	checks := checksResp.Items
	if len(checks) == 0 {
		t.Fatalf("expected quality checks")
	}
	if !containsQualityCheck(checks, "relationship_refs") {
		t.Fatalf("expected relationship_refs quality check: %#v", checks)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/quality-checks?limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first quality check cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstQualityCheckPage ListResponse[QualityCheckDefinition]
	if err := json.NewDecoder(w.Body).Decode(&firstQualityCheckPage); err != nil {
		t.Fatalf("decode first quality check cursor page: %v", err)
	}
	if len(firstQualityCheckPage.Items) != 1 || !firstQualityCheckPage.HasMore || firstQualityCheckPage.NextBeforeID == "" {
		t.Fatalf("unexpected first quality check cursor page: %#v", firstQualityCheckPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/quality-checks?limit=1&before_id="+url.QueryEscape(firstQualityCheckPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second quality check cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondQualityCheckPage ListResponse[QualityCheckDefinition]
	if err := json.NewDecoder(w.Body).Decode(&secondQualityCheckPage); err != nil {
		t.Fatalf("decode second quality check cursor page: %v", err)
	}
	if len(secondQualityCheckPage.Items) == 0 || secondQualityCheckPage.Items[0].ID == firstQualityCheckPage.Items[0].ID {
		t.Fatalf("unexpected second quality check cursor page: first=%#v second=%#v", firstQualityCheckPage, secondQualityCheckPage)
	}

	body = bytes.NewBufferString(`{"include_warnings":true,"limit":100}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/quality/run", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("run quality check status=%d body=%s", w.Code, w.Body.String())
	}
	var quality QualityCheckResult
	if err := json.NewDecoder(w.Body).Decode(&quality); err != nil {
		t.Fatalf("decode quality check: %v", err)
	}
	if quality.DatasetID != "sales.orders" || quality.Scanned == 0 || len(quality.Issues) == 0 {
		t.Fatalf("unexpected quality check result: %#v", quality)
	}
	if quality.ID == "" || quality.IssueCount != len(quality.Issues) {
		t.Fatalf("expected persisted quality run metadata: %#v", quality)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/quality/runs?limit=10", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list quality runs status=%d body=%s", w.Code, w.Body.String())
	}
	var qualityRuns ListResponse[QualityCheckResult]
	if err := json.NewDecoder(w.Body).Decode(&qualityRuns); err != nil {
		t.Fatalf("decode quality runs: %v", err)
	}
	if len(qualityRuns.Items) == 0 || qualityRuns.Items[0].ID != quality.ID {
		t.Fatalf("unexpected quality runs: %#v", qualityRuns)
	}
	body = bytes.NewBufferString(`{"checks":["required_fields"],"include_warnings":true,"limit":100}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/quality/run", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second quality check status=%d body=%s", w.Code, w.Body.String())
	}
	var secondQuality QualityCheckResult
	if err := json.NewDecoder(w.Body).Decode(&secondQuality); err != nil {
		t.Fatalf("decode second quality check: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/quality/runs?limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first quality run cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstQualityPage ListResponse[QualityCheckResult]
	if err := json.NewDecoder(w.Body).Decode(&firstQualityPage); err != nil {
		t.Fatalf("decode first quality run cursor page: %v", err)
	}
	if len(firstQualityPage.Items) != 1 || !firstQualityPage.HasMore || firstQualityPage.NextBefore == "" || firstQualityPage.NextBeforeID == "" {
		t.Fatalf("unexpected first quality run cursor page: %#v", firstQualityPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/quality/runs?limit=1&before="+url.QueryEscape(firstQualityPage.NextBefore)+"&before_id="+url.QueryEscape(firstQualityPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second quality run cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondQualityPage ListResponse[QualityCheckResult]
	if err := json.NewDecoder(w.Body).Decode(&secondQualityPage); err != nil {
		t.Fatalf("decode second quality run cursor page: %v", err)
	}
	if len(secondQualityPage.Items) == 0 || secondQualityPage.Items[0].ID == firstQualityPage.Items[0].ID || secondQuality.ID == "" {
		t.Fatalf("unexpected second quality run cursor page: first=%#v second=%#v created=%#v", firstQualityPage, secondQualityPage, secondQuality)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/quality/runs/"+quality.ID, nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get quality run status=%d body=%s", w.Code, w.Body.String())
	}
	var qualityRun QualityCheckResult
	if err := json.NewDecoder(w.Body).Decode(&qualityRun); err != nil {
		t.Fatalf("decode quality run: %v", err)
	}
	if qualityRun.ID != quality.ID || qualityRun.IssueCount != quality.IssueCount {
		t.Fatalf("unexpected quality run: %#v", qualityRun)
	}

	body = bytes.NewBufferString(`{"q":"BatchCo","tag":"batch","limit":10}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/query", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("query batch status=%d body=%s", w.Code, w.Body.String())
	}
	var batchQuery ListResponse[Record]
	if err := json.NewDecoder(w.Body).Decode(&batchQuery); err != nil {
		t.Fatalf("decode batch query: %v", err)
	}
	if len(batchQuery.Items) != 2 {
		t.Fatalf("unexpected batch query: %#v", batchQuery)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/records?q=BatchCo&limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first record cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstRecordPage ListResponse[Record]
	if err := json.NewDecoder(w.Body).Decode(&firstRecordPage); err != nil {
		t.Fatalf("decode first record cursor page: %v", err)
	}
	if len(firstRecordPage.Items) != 1 || !firstRecordPage.HasMore || firstRecordPage.NextBefore == "" || firstRecordPage.NextBeforeID == "" {
		t.Fatalf("unexpected first record cursor page: %#v", firstRecordPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/records?q=BatchCo&limit=1&before="+url.QueryEscape(firstRecordPage.NextBefore)+"&before_id="+url.QueryEscape(firstRecordPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second record cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondRecordPage ListResponse[Record]
	if err := json.NewDecoder(w.Body).Decode(&secondRecordPage); err != nil {
		t.Fatalf("decode second record cursor page: %v", err)
	}
	if len(secondRecordPage.Items) == 0 || secondRecordPage.Items[0].ID == firstRecordPage.Items[0].ID {
		t.Fatalf("unexpected second record cursor page: first=%#v second=%#v", firstRecordPage, secondRecordPage)
	}

	body = bytes.NewBufferString(`{"q":"BatchCo","limit":1}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/query", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first record query cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstRecordQueryPage ListResponse[Record]
	if err := json.NewDecoder(w.Body).Decode(&firstRecordQueryPage); err != nil {
		t.Fatalf("decode first record query cursor page: %v", err)
	}
	if len(firstRecordQueryPage.Items) != 1 || !firstRecordQueryPage.HasMore || firstRecordQueryPage.NextBefore == "" || firstRecordQueryPage.NextBeforeID == "" {
		t.Fatalf("unexpected first record query cursor page: %#v", firstRecordQueryPage)
	}
	body = bytes.NewBufferString(fmt.Sprintf(`{"q":"BatchCo","limit":1,"before":%q,"before_id":%q}`, firstRecordQueryPage.NextBefore, firstRecordQueryPage.NextBeforeID))
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/query", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second record query cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondRecordQueryPage ListResponse[Record]
	if err := json.NewDecoder(w.Body).Decode(&secondRecordQueryPage); err != nil {
		t.Fatalf("decode second record query cursor page: %v", err)
	}
	if len(secondRecordQueryPage.Items) == 0 || secondRecordQueryPage.Items[0].ID == firstRecordQueryPage.Items[0].ID {
		t.Fatalf("unexpected second record query cursor page: first=%#v second=%#v", firstRecordQueryPage, secondRecordQueryPage)
	}

	body = bytes.NewBufferString(`{"q":"BatchCo","tag":"batch","limit":10}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/export.csv", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export csv status=%d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "text/csv") {
		t.Fatalf("expected text/csv content type, got %q", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected csv export nosniff header, got %q", got)
	}
	csvText := w.Body.String()
	if !strings.Contains(csvText, "id,title,tags") || !strings.Contains(csvText, "BATCH-OK-1") || !strings.Contains(csvText, "BatchCo") {
		t.Fatalf("unexpected csv export: %s", csvText)
	}
	for _, tc := range []struct {
		name string
		path string
		body string
	}{
		{name: "csv direct", path: "/api/v1/data/datasets/sales.orders/records/export.csv", body: `{"q":"BatchCo","limit":5001}`},
		{name: "jsonl direct", path: "/api/v1/data/datasets/sales.orders/records/export.jsonl", body: `{"q":"BatchCo","limit":5001}`},
		{name: "csv job", path: "/api/v1/data/datasets/sales.orders/records/export.csv/jobs", body: `{"q":"BatchCo","limit":50001}`},
		{name: "jsonl job", path: "/api/v1/data/datasets/sales.orders/records/export.jsonl/jobs", body: `{"q":"BatchCo","limit":50001}`},
	} {
		body = bytes.NewBufferString(tc.body)
		req = httptest.NewRequest(http.MethodPost, tc.path, body)
		auth(req)
		w = httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s export over limit status=%d body=%s", tc.name, w.Code, w.Body.String())
		}
	}

	body = bytes.NewBufferString(`{"q":"BatchCo","tag":"batch","limit":10}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/export.jsonl", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export jsonl status=%d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "application/x-ndjson") {
		t.Fatalf("expected application/x-ndjson content type, got %q", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected jsonl export nosniff header, got %q", got)
	}
	jsonlText := strings.TrimSpace(w.Body.String())
	if !strings.Contains(jsonlText, `"id":"BATCH-OK-1"`) || !strings.Contains(jsonlText, `"customer":"BatchCo"`) || !strings.Contains(jsonlText, `"data":`) {
		t.Fatalf("unexpected jsonl export: %s", jsonlText)
	}

	body = bytes.NewBufferString(`{"q":"BatchCo","tag":"batch","limit":10}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/export.jsonl/jobs", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("export jsonl job status=%d body=%s", w.Code, w.Body.String())
	}
	var exportJob ExportJob
	if err := json.NewDecoder(w.Body).Decode(&exportJob); err != nil {
		t.Fatalf("decode export job: %v", err)
	}
	if exportJob.ID == "" || exportJob.Format != "jsonl" || exportJob.Status != exportJobStatusQueued {
		t.Fatalf("unexpected queued export job: %#v", exportJob)
	}
	var completedExportJob ExportJob
	for i := 0; i < 50; i++ {
		req = httptest.NewRequest(http.MethodGet, "/api/v1/data/export-jobs/"+exportJob.ID, nil)
		auth(req)
		w = httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("get export job status=%d body=%s", w.Code, w.Body.String())
		}
		if err := json.NewDecoder(w.Body).Decode(&completedExportJob); err != nil {
			t.Fatalf("decode completed export job: %v", err)
		}
		if completedExportJob.Status == exportJobStatusCompleted || completedExportJob.Status == exportJobStatusFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if completedExportJob.Status != exportJobStatusCompleted || completedExportJob.Total != 2 || completedExportJob.Bytes == 0 || completedExportJob.DownloadPath == "" {
		t.Fatalf("unexpected completed export job: %#v", completedExportJob)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/export-jobs/"+exportJob.ID+"/download", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("download export job status=%d body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); !strings.Contains(got, `"id":"BATCH-OK-1"`) || !strings.Contains(got, `"customer":"BatchCo"`) {
		t.Fatalf("unexpected export job download: %s", got)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/export-jobs?dataset_id=sales.orders&limit=10", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list export jobs status=%d body=%s", w.Code, w.Body.String())
	}
	var exportJobs ListResponse[ExportJob]
	if err := json.NewDecoder(w.Body).Decode(&exportJobs); err != nil {
		t.Fatalf("decode export jobs: %v", err)
	}
	if len(exportJobs.Items) == 0 || exportJobs.Items[0].ResultText != "" || exportJobs.Items[0].DownloadPath == "" {
		t.Fatalf("unexpected export jobs: %#v", exportJobs)
	}
	body = bytes.NewBufferString(`{"q":"BatchCo","tag":"batch","limit":10}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/export.csv/jobs", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("export csv job status=%d body=%s", w.Code, w.Body.String())
	}
	var csvExportJob ExportJob
	if err := json.NewDecoder(w.Body).Decode(&csvExportJob); err != nil {
		t.Fatalf("decode csv export job: %v", err)
	}
	for i := 0; i < 50; i++ {
		req = httptest.NewRequest(http.MethodGet, "/api/v1/data/export-jobs/"+csvExportJob.ID, nil)
		auth(req)
		w = httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("get csv export job status=%d body=%s", w.Code, w.Body.String())
		}
		if err := json.NewDecoder(w.Body).Decode(&csvExportJob); err != nil {
			t.Fatalf("decode completed csv export job: %v", err)
		}
		if csvExportJob.Status == exportJobStatusCompleted || csvExportJob.Status == exportJobStatusFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/export-jobs?dataset_id=sales.orders&limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first export job cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstExportJobPage ListResponse[ExportJob]
	if err := json.NewDecoder(w.Body).Decode(&firstExportJobPage); err != nil {
		t.Fatalf("decode first export job cursor page: %v", err)
	}
	if len(firstExportJobPage.Items) != 1 || !firstExportJobPage.HasMore || firstExportJobPage.NextBefore == "" || firstExportJobPage.NextBeforeID == "" {
		t.Fatalf("unexpected first export job cursor page: %#v", firstExportJobPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/export-jobs?dataset_id=sales.orders&limit=1&before="+url.QueryEscape(firstExportJobPage.NextBefore)+"&before_id="+url.QueryEscape(firstExportJobPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second export job cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondExportJobPage ListResponse[ExportJob]
	if err := json.NewDecoder(w.Body).Decode(&secondExportJobPage); err != nil {
		t.Fatalf("decode second export job cursor page: %v", err)
	}
	if len(secondExportJobPage.Items) == 0 || secondExportJobPage.Items[0].ID == firstExportJobPage.Items[0].ID {
		t.Fatalf("unexpected second export job cursor page: first=%#v second=%#v", firstExportJobPage, secondExportJobPage)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/views?limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first business view cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstViewPage ListResponse[BusinessViewDefinition]
	if err := json.NewDecoder(w.Body).Decode(&firstViewPage); err != nil {
		t.Fatalf("decode first business view cursor page: %v", err)
	}
	if len(firstViewPage.Items) != 1 || !firstViewPage.HasMore || firstViewPage.NextBeforeID == "" {
		t.Fatalf("unexpected first business view cursor page: %#v", firstViewPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/views?limit=1&before_id="+url.QueryEscape(firstViewPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second business view cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondViewPage ListResponse[BusinessViewDefinition]
	if err := json.NewDecoder(w.Body).Decode(&secondViewPage); err != nil {
		t.Fatalf("decode second business view cursor page: %v", err)
	}
	if len(secondViewPage.Items) == 0 || secondViewPage.Items[0].ID == firstViewPage.Items[0].ID {
		t.Fatalf("unexpected second business view cursor page: first=%#v second=%#v", firstViewPage, secondViewPage)
	}
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/data/views?limit=%d", len(businessViews)), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("exact business view page status=%d body=%s", w.Code, w.Body.String())
	}
	var exactViewPage ListResponse[BusinessViewDefinition]
	if err := json.NewDecoder(w.Body).Decode(&exactViewPage); err != nil {
		t.Fatalf("decode exact business view page: %v", err)
	}
	if len(exactViewPage.Items) != len(businessViews) || exactViewPage.HasMore || exactViewPage.NextBeforeID != "" {
		t.Fatalf("exact business view page should not report more pages: %#v", exactViewPage)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/reports?limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first report cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstReportPage ListResponse[ReportDefinition]
	if err := json.NewDecoder(w.Body).Decode(&firstReportPage); err != nil {
		t.Fatalf("decode first report cursor page: %v", err)
	}
	if len(firstReportPage.Items) != 1 || !firstReportPage.HasMore || firstReportPage.NextBeforeID == "" {
		t.Fatalf("unexpected first report cursor page: %#v", firstReportPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/reports?limit=1&before_id="+url.QueryEscape(firstReportPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second report cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondReportPage ListResponse[ReportDefinition]
	if err := json.NewDecoder(w.Body).Decode(&secondReportPage); err != nil {
		t.Fatalf("decode second report cursor page: %v", err)
	}
	if len(secondReportPage.Items) == 0 || secondReportPage.Items[0].ID == firstReportPage.Items[0].ID {
		t.Fatalf("unexpected second report cursor page: first=%#v second=%#v", firstReportPage, secondReportPage)
	}
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/data/reports?limit=%d", len(reportDefinitions)), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("exact report page status=%d body=%s", w.Code, w.Body.String())
	}
	var exactReportPage ListResponse[ReportDefinition]
	if err := json.NewDecoder(w.Body).Decode(&exactReportPage); err != nil {
		t.Fatalf("decode exact report page: %v", err)
	}
	if len(exactReportPage.Items) != len(reportDefinitions) || exactReportPage.HasMore || exactReportPage.NextBeforeID != "" {
		t.Fatalf("exact report page should not report more pages: %#v", exactReportPage)
	}

	body = bytes.NewBufferString(`{"limit":500}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/reports/sales.order_summary_by_stage/run", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("run report status=%d body=%s", w.Code, w.Body.String())
	}
	var report ReportResult
	if err := json.NewDecoder(w.Body).Decode(&report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.Report.ID != "sales.order_summary_by_stage" || len(report.Result.Rows) == 0 {
		t.Fatalf("unexpected report: %#v", report)
	}
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "limit", body: `{"limit":501}`},
		{name: "scan limit", body: `{"scan_limit":5001}`},
	} {
		body = bytes.NewBufferString(tc.body)
		req = httptest.NewRequest(http.MethodPost, "/api/v1/data/reports/sales.order_summary_by_stage/run", body)
		auth(req)
		w = httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected oversized report %s rejection, status=%d body=%s", tc.name, w.Code, w.Body.String())
		}
	}

	body = bytes.NewBufferString(`{"record_id":"OPP-DASH-0001","data":{"opportunity_no":"OPP-DASH-0001","name":"Dashboard opportunity","customer":"BatchCo","amount":2500,"stage":"qualified","owner":"Sales Ops"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-actions/sales.opportunity_upsert/execute", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create dashboard opportunity status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"record_id":"CON-DASH-0001","data":{"contact_no":"CON-DASH-0001","name":"Dashboard Contact","customer":"BatchCo","role":"Buyer","owner":"Sales Ops","status":"active"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-actions/sales.contact_upsert/execute", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create dashboard contact status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/dashboards/sales.overview/run", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("run dashboard status=%d body=%s", w.Code, w.Body.String())
	}
	var dashboard DashboardResult
	if err := json.NewDecoder(w.Body).Decode(&dashboard); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	if dashboard.Dashboard.ID != "sales.overview" || dashboard.Stats == nil || dashboard.InboxSummary == nil || len(dashboard.Reports) == 0 || dashboard.Reports[0].Result == nil {
		t.Fatalf("unexpected dashboard: %#v", dashboard)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/dashboards?limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first dashboard cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstDashboardPage ListResponse[DashboardDefinition]
	if err := json.NewDecoder(w.Body).Decode(&firstDashboardPage); err != nil {
		t.Fatalf("decode first dashboard cursor page: %v", err)
	}
	if len(firstDashboardPage.Items) != 1 || !firstDashboardPage.HasMore || firstDashboardPage.NextBeforeID == "" {
		t.Fatalf("unexpected first dashboard cursor page: %#v", firstDashboardPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/dashboards?limit=1&before_id="+url.QueryEscape(firstDashboardPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second dashboard cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondDashboardPage ListResponse[DashboardDefinition]
	if err := json.NewDecoder(w.Body).Decode(&secondDashboardPage); err != nil {
		t.Fatalf("decode second dashboard cursor page: %v", err)
	}
	if len(secondDashboardPage.Items) == 0 || secondDashboardPage.Items[0].ID == firstDashboardPage.Items[0].ID {
		t.Fatalf("unexpected second dashboard cursor page: first=%#v second=%#v", firstDashboardPage, secondDashboardPage)
	}
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/data/dashboards?limit=%d", len(dashboardDefinitions)), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("exact dashboard page status=%d body=%s", w.Code, w.Body.String())
	}
	var exactDashboardPage ListResponse[DashboardDefinition]
	if err := json.NewDecoder(w.Body).Decode(&exactDashboardPage); err != nil {
		t.Fatalf("decode exact dashboard page: %v", err)
	}
	if len(exactDashboardPage.Items) != len(dashboardDefinitions) || exactDashboardPage.HasMore || exactDashboardPage.NextBeforeID != "" {
		t.Fatalf("exact dashboard page should not report more pages: %#v", exactDashboardPage)
	}

	body = bytes.NewBufferString(`{"title":"Order A","tags":["q1"],"data":{"customer":"Acme","amount":8800,"watchers":["Dana","Ops"],"approval":{"assigned_to":"Dana","status":"pending"}}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create record status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"q":"Acme","filter":{"field":"amount","op":"gte","value":8000},"limit":10}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/query", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("query status=%d body=%s", w.Code, w.Body.String())
	}
	var out ListResponse[Record]
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode query: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].Data["customer"] != "Acme" {
		t.Fatalf("unexpected query result: %#v", out)
	}

	body = bytes.NewBufferString(`{"filter":{"op":"and","filters":[{"field":"customer","op":"prefix","value":"ac"},{"field":"amount","op":"between","value":[8000,9000]},{"field":"customer","op":"exists"}]},"limit":10}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/query", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("advanced query status=%d body=%s", w.Code, w.Body.String())
	}
	out = ListResponse[Record]{}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode advanced query: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].Data["customer"] != "Acme" {
		t.Fatalf("unexpected advanced query result: %#v", out)
	}

	body = bytes.NewBufferString(`{"filter":{"op":"and","filters":[{"field":"approval.assigned_to","op":"eq","value":"dana"},{"field":"approval.status","op":"prefix","value":"pend"}]},"sort":[{"field":"approval.assigned_to","direction":"asc"}],"limit":10}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/query", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("nested query status=%d body=%s", w.Code, w.Body.String())
	}
	out = ListResponse[Record]{}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode nested query: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].Data["customer"] != "Acme" {
		t.Fatalf("unexpected nested query result: %#v", out)
	}

	body = bytes.NewBufferString(`{"filter":{"field":"watchers","op":"eq","value":"ops"},"limit":10}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/query", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("scalar array query status=%d body=%s", w.Code, w.Body.String())
	}
	out = ListResponse[Record]{}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode scalar array query: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].Data["customer"] != "Acme" {
		t.Fatalf("unexpected scalar array query result: %#v", out)
	}

	body = bytes.NewBufferString(`{"filter":{"field":"watchers","op":"not_in","value":["ops"]},"limit":10}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/query", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("scalar array not_in query status=%d body=%s", w.Code, w.Body.String())
	}
	out = ListResponse[Record]{}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode scalar array not_in query: %v", err)
	}
	if len(out.Items) != 0 {
		t.Fatalf("scalar array not_in should reject records with any matching element: %#v", out)
	}

	body = bytes.NewBufferString(`{"filter":{"field":"watchers","op":"exists"},"group_by":["watchers"],"metrics":[{"name":"orders","op":"count"}],"sort":[{"field":"watchers","direction":"asc"}],"limit":10}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/aggregate", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("scalar array aggregate status=%d body=%s", w.Code, w.Body.String())
	}
	var arrayAggregate AggregateResult
	if err := json.NewDecoder(w.Body).Decode(&arrayAggregate); err != nil {
		t.Fatalf("decode scalar array aggregate: %v", err)
	}
	if len(arrayAggregate.Rows) != 2 || arrayAggregate.Rows[0]["watchers"] != "Dana" || arrayAggregate.Rows[1]["watchers"] != "Ops" {
		t.Fatalf("unexpected scalar array aggregate: %#v", arrayAggregate)
	}

	body = bytes.NewBufferString(`{"metrics":[{"name":"distinct_watchers","op":"count_distinct","field":"watchers"}],"limit":10}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/aggregate", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("count distinct aggregate status=%d body=%s", w.Code, w.Body.String())
	}
	var distinctAggregate AggregateResult
	if err := json.NewDecoder(w.Body).Decode(&distinctAggregate); err != nil {
		t.Fatalf("decode count distinct aggregate: %v", err)
	}
	if len(distinctAggregate.Rows) != 1 || distinctAggregate.Rows[0]["distinct_watchers"] != float64(2) {
		t.Fatalf("unexpected count distinct aggregate: %#v", distinctAggregate)
	}

	body = bytes.NewBufferString(`{"filter":{"or":[{"field":"customer","op":"eq","value":"acme"},{"field":"amount","op":"lt","value":100}]},"limit":10}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/query", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("or query status=%d body=%s", w.Code, w.Body.String())
	}
	out = ListResponse[Record]{}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode or query: %v", err)
	}
	if len(out.Items) == 0 || out.Items[0].Data["customer"] != "Acme" {
		t.Fatalf("unexpected or query result: %#v", out)
	}

	body = bytes.NewBufferString(`{"query":{"filter":{"field":"customer","op":"eq","value":"Acme"},"limit":10},"set":{"stage":"won"},"dry_run":true,"reason":"test cleanup"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/bulk-update", body)
	authRole(req, "data_user")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected bulk update forbidden, status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"query":{"filter":{"field":"customer","op":"eq","value":"Acme"},"limit":10},"set":{"stage":"won"},"dry_run":true,"reason":"test cleanup"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/bulk-update", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("bulk update dry-run status=%d body=%s", w.Code, w.Body.String())
	}
	var bulkDryRun BulkUpdateRecordsResult
	if err := json.NewDecoder(w.Body).Decode(&bulkDryRun); err != nil {
		t.Fatalf("decode bulk dry-run: %v", err)
	}
	if !bulkDryRun.DryRun || !bulkDryRun.Valid || bulkDryRun.Total != 1 || bulkDryRun.Updated != 0 || bulkDryRun.Records[0].Data["stage"] != "won" {
		t.Fatalf("unexpected bulk dry-run: %#v", bulkDryRun)
	}

	body = bytes.NewBufferString(`{"query":{"filter":{"field":"customer","op":"eq","value":"Acme"},"limit":10},"set":{"stage":"won"},"dry_run":false}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/bulk-update", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected bulk update confirm error, status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"query":{"filter":{"field":"customer","op":"eq","value":"Acme"},"limit":10},"set":{"stage":"won"},"dry_run":false,"confirm":true,"reason":"test cleanup"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/bulk-update", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("bulk update apply status=%d body=%s", w.Code, w.Body.String())
	}
	var bulkApply BulkUpdateRecordsResult
	if err := json.NewDecoder(w.Body).Decode(&bulkApply); err != nil {
		t.Fatalf("decode bulk apply: %v", err)
	}
	if bulkApply.DryRun || !bulkApply.Valid || bulkApply.Updated != 1 {
		t.Fatalf("unexpected bulk apply: %#v", bulkApply)
	}

	body = bytes.NewBufferString(`{"filter":{"field":"stage","op":"eq","value":"won"},"limit":10}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/query", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("query after bulk update status=%d body=%s", w.Code, w.Body.String())
	}
	var bulkQuery ListResponse[Record]
	if err := json.NewDecoder(w.Body).Decode(&bulkQuery); err != nil {
		t.Fatalf("decode bulk query: %v", err)
	}
	foundBulkUpdated := false
	for _, item := range bulkQuery.Items {
		if item.Data["customer"] == "Acme" && item.Data["stage"] == "won" {
			foundBulkUpdated = true
			break
		}
	}
	if !foundBulkUpdated {
		t.Fatalf("unexpected bulk query: %#v", bulkQuery)
	}

	body = bytes.NewBufferString(`{"record_id":"SO-PLAN-UPDATE-1","idempotency_key":"test:operation-plan:update:1","data":{"order_no":"SO-PLAN-UPDATE-1","customer":"PlanCo","amount":1500,"stage":"draft","payment_status":"unpaid"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-actions/sales.order_upsert/execute", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create operation plan test record status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"dataset_id":"sales.orders","operation":"bulk_update_records","summary":"unsafe full scan plan","risk_level":"critical","request":{"query":{"limit":10},"set":{"stage":"confirmed"},"reason":"operation plan test"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/operation-plans", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected unscoped operation plan rejection, status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"dataset_id":"sales.orders","operation":"bulk_update_records","summary":"invalid risk","risk_level":"urgent","request":{"query":{"filter":{"field":"customer","op":"eq","value":"PlanCo"},"limit":10},"set":{"stage":"confirmed"},"reason":"operation plan test"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/operation-plans", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid operation plan risk status=%d body=%s", w.Code, w.Body.String())
	}

	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "query limit", body: `{"dataset_id":"sales.orders","operation":"bulk_update_records","summary":"oversized query limit","risk_level":"medium","request":{"query":{"filter":{"field":"customer","op":"eq","value":"PlanCo"},"limit":101},"set":{"stage":"confirmed"},"reason":"operation plan test"}}`},
		{name: "top-level limit", body: `{"dataset_id":"sales.orders","operation":"bulk_update_records","summary":"oversized top-level limit","risk_level":"medium","request":{"query":{"filter":{"field":"customer","op":"eq","value":"PlanCo"}},"limit":101,"set":{"stage":"confirmed"},"reason":"operation plan test"}}`},
	} {
		body = bytes.NewBufferString(tc.body)
		req = httptest.NewRequest(http.MethodPost, "/api/v1/data/operation-plans", body)
		auth(req)
		w = httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid operation plan %s status=%d body=%s", tc.name, w.Code, w.Body.String())
		}
	}

	body = bytes.NewBufferString(`{"dataset_id":"sales.orders","operation":"bulk_update_records","summary":"review PlanCo draft orders","risk_level":"high","request":{"query":{"filter":{"field":"customer","op":"eq","value":"PlanCo"},"limit":10},"set":{"stage":"confirmed"},"reason":"operation plan test"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/operation-plans", body)
	authRole(req, "data_user")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create operation plan as data_user status=%d body=%s", w.Code, w.Body.String())
	}
	var plan OperationPlan
	if err := json.NewDecoder(w.Body).Decode(&plan); err != nil {
		t.Fatalf("decode operation plan: %v", err)
	}
	if plan.ID == "" || plan.Status != operationPlanStatusPending || plan.Preview["matched"].(float64) != 1 {
		t.Fatalf("unexpected operation plan: %#v", plan)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/operation-plans?dataset_id=sales.orders&status=pending", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list operation plans status=%d body=%s", w.Code, w.Body.String())
	}
	var plans ListResponse[OperationPlan]
	if err := json.NewDecoder(w.Body).Decode(&plans); err != nil {
		t.Fatalf("decode operation plans: %v", err)
	}
	if len(plans.Items) == 0 || plans.Items[0].ID != plan.ID {
		t.Fatalf("unexpected operation plans: %#v", plans)
	}
	body = bytes.NewBufferString(`{"dataset_id":"sales.orders","operation":"bulk_update_records","summary":"review PlanCo draft orders again","risk_level":"medium","request":{"query":{"filter":{"field":"customer","op":"eq","value":"PlanCo"},"limit":10},"set":{"payment_status":"reviewed"},"reason":"operation plan cursor test"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/operation-plans", body)
	authRole(req, "data_user")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create second operation plan status=%d body=%s", w.Code, w.Body.String())
	}
	var secondPlan OperationPlan
	if err := json.NewDecoder(w.Body).Decode(&secondPlan); err != nil {
		t.Fatalf("decode second operation plan: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/operation-plans?dataset_id=sales.orders&status=pending&limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first operation plan cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstPlanPage ListResponse[OperationPlan]
	if err := json.NewDecoder(w.Body).Decode(&firstPlanPage); err != nil {
		t.Fatalf("decode first operation plan cursor page: %v", err)
	}
	if len(firstPlanPage.Items) != 1 || !firstPlanPage.HasMore || firstPlanPage.NextBefore == "" || firstPlanPage.NextBeforeID == "" {
		t.Fatalf("unexpected first operation plan cursor page: %#v", firstPlanPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/operation-plans?dataset_id=sales.orders&status=pending&limit=1&before="+url.QueryEscape(firstPlanPage.NextBefore)+"&before_id="+url.QueryEscape(firstPlanPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second operation plan cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondPlanPage ListResponse[OperationPlan]
	if err := json.NewDecoder(w.Body).Decode(&secondPlanPage); err != nil {
		t.Fatalf("decode second operation plan cursor page: %v", err)
	}
	if len(secondPlanPage.Items) == 0 || secondPlanPage.Items[0].ID == firstPlanPage.Items[0].ID {
		t.Fatalf("unexpected second operation plan cursor page: first=%#v second=%#v created=%#v", firstPlanPage, secondPlanPage, secondPlan)
	}

	body = bytes.NewBufferString(`{"confirm":true,"reason":"apply operation plan test"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/operation-plans/"+plan.ID+"/apply", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected operation plan approval required, status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"decision":"approve","reason":"approve operation plan test"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/operation-plans/"+plan.ID+"/review", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("review operation plan status=%d body=%s", w.Code, w.Body.String())
	}
	var reviewedPlan OperationPlan
	if err := json.NewDecoder(w.Body).Decode(&reviewedPlan); err != nil {
		t.Fatalf("decode reviewed operation plan: %v", err)
	}
	if reviewedPlan.Status != operationPlanStatusApproved || reviewedPlan.ReviewedBy == "" || reviewedPlan.ReviewedAt.IsZero() {
		t.Fatalf("unexpected reviewed operation plan: %#v", reviewedPlan)
	}

	body = bytes.NewBufferString(`{"confirm":true,"reason":"apply operation plan test"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/operation-plans/"+plan.ID+"/apply", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("apply operation plan status=%d body=%s", w.Code, w.Body.String())
	}
	var appliedPlan OperationPlanApplyResult
	if err := json.NewDecoder(w.Body).Decode(&appliedPlan); err != nil {
		t.Fatalf("decode applied operation plan: %v", err)
	}
	if appliedPlan.Plan.Status != operationPlanStatusApplied {
		t.Fatalf("unexpected applied operation plan: %#v", appliedPlan.Plan)
	}

	body = bytes.NewBufferString(`{"filter":{"field":"customer","op":"eq","value":"PlanCo"},"limit":10}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/query", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("query after operation plan status=%d body=%s", w.Code, w.Body.String())
	}
	var planQuery ListResponse[Record]
	if err := json.NewDecoder(w.Body).Decode(&planQuery); err != nil {
		t.Fatalf("decode operation plan query: %v", err)
	}
	if len(planQuery.Items) != 1 || planQuery.Items[0].Data["stage"] != "confirmed" {
		t.Fatalf("unexpected operation plan query: %#v", planQuery)
	}

	body = bytes.NewBufferString(`{"record_id":"SO-BULK-DELETE-1","idempotency_key":"test:bulk-delete:1","data":{"order_no":"SO-BULK-DELETE-1","customer":"DeleteCo","amount":500,"stage":"cancelled","payment_status":"unpaid"}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-actions/sales.order_upsert/execute", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create bulk delete test record status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"query":{"filter":{"field":"customer","op":"eq","value":"DeleteCo"},"limit":10},"dry_run":true,"reason":"test cleanup"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/bulk-delete", body)
	authRole(req, "data_user")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected bulk delete forbidden, status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"query":{"filter":{"field":"customer","op":"eq","value":"DeleteCo"},"limit":10},"dry_run":true,"reason":"test cleanup"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/bulk-delete", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("bulk delete dry-run status=%d body=%s", w.Code, w.Body.String())
	}
	var deleteDryRun BulkDeleteRecordsResult
	if err := json.NewDecoder(w.Body).Decode(&deleteDryRun); err != nil {
		t.Fatalf("decode bulk delete dry-run: %v", err)
	}
	if !deleteDryRun.DryRun || deleteDryRun.Total != 1 || deleteDryRun.Deleted != 0 || len(deleteDryRun.RecordIDs) != 1 {
		t.Fatalf("unexpected bulk delete dry-run: %#v", deleteDryRun)
	}

	body = bytes.NewBufferString(`{"query":{"filter":{"field":"customer","op":"eq","value":"DeleteCo"},"limit":10},"dry_run":false}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/bulk-delete", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected bulk delete confirm error, status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"query":{"filter":{"field":"customer","op":"eq","value":"DeleteCo"},"limit":10},"dry_run":false,"confirm":true,"reason":"test cleanup"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/bulk-delete", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("bulk delete apply status=%d body=%s", w.Code, w.Body.String())
	}
	var deleteApply BulkDeleteRecordsResult
	if err := json.NewDecoder(w.Body).Decode(&deleteApply); err != nil {
		t.Fatalf("decode bulk delete apply: %v", err)
	}
	if deleteApply.DryRun || deleteApply.Deleted != 1 {
		t.Fatalf("unexpected bulk delete apply: %#v", deleteApply)
	}

	body = bytes.NewBufferString(`{"filter":{"field":"customer","op":"eq","value":"DeleteCo"},"limit":10}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/query", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("query after bulk delete status=%d body=%s", w.Code, w.Body.String())
	}
	var deleteQuery ListResponse[Record]
	if err := json.NewDecoder(w.Body).Decode(&deleteQuery); err != nil {
		t.Fatalf("decode bulk delete query: %v", err)
	}
	if len(deleteQuery.Items) != 0 {
		t.Fatalf("unexpected bulk delete query: %#v", deleteQuery)
	}

	body = bytes.NewBufferString(`{"confirm":false,"reason":"restore test"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/SO-BULK-DELETE-1/restore", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected restore confirm error, status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"confirm":true,"reason":"restore test"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/SO-BULK-DELETE-1/restore", body)
	authRole(req, "data_user")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected restore forbidden, status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"confirm":true,"reason":"restore test"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/SO-BULK-DELETE-1/restore", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("restore record status=%d body=%s", w.Code, w.Body.String())
	}
	var restored Record
	if err := json.NewDecoder(w.Body).Decode(&restored); err != nil {
		t.Fatalf("decode restored record: %v", err)
	}
	if restored.ID != "SO-BULK-DELETE-1" || restored.Data["customer"] != "DeleteCo" {
		t.Fatalf("unexpected restored record: %#v", restored)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/records/SO-BULK-DELETE-1/revisions?limit=10", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("revisions after restore status=%d body=%s", w.Code, w.Body.String())
	}
	var restoreRevisions ListResponse[RecordRevision]
	if err := json.NewDecoder(w.Body).Decode(&restoreRevisions); err != nil {
		t.Fatalf("decode restore revisions: %v", err)
	}
	if len(restoreRevisions.Items) == 0 || restoreRevisions.Items[0].Action != "restore" {
		t.Fatalf("expected restore revision: %#v", restoreRevisions)
	}

	body = bytes.NewBufferString(`{"name":"before import","note":"agent checkpoint"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/backups", body)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create backup status=%d body=%s", w.Code, w.Body.String())
	}
	var backup BackupInfo
	if err := json.NewDecoder(w.Body).Decode(&backup); err != nil {
		t.Fatalf("decode backup: %v", err)
	}
	if backup.ID == "" || backup.SizeBytes <= 0 || backup.SHA256 == "" || backup.DownloadURL == "" {
		t.Fatalf("unexpected backup: %#v", backup)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/backups/"+backup.ID, nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get backup status=%d body=%s", w.Code, w.Body.String())
	}
	var backupMeta BackupInfo
	if err := json.NewDecoder(w.Body).Decode(&backupMeta); err != nil {
		t.Fatalf("decode backup metadata: %v", err)
	}
	if backupMeta.ID != backup.ID || backupMeta.SHA256 != backup.SHA256 || backupMeta.DownloadURL == "" {
		t.Fatalf("unexpected backup metadata: %#v", backupMeta)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/backups/"+backup.ID+"/download", nil)
	authRole(req, "data_user")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected download forbidden, status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/backups/"+backup.ID+"/download", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("download backup status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("X-MaClaw-Backup-SHA256") != backup.SHA256 || int64(w.Body.Len()) != backup.SizeBytes {
		t.Fatalf("unexpected backup download headers/body: sha=%q size=%d backup=%#v", w.Header().Get("X-MaClaw-Backup-SHA256"), w.Body.Len(), backup)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected backup download nosniff header, got %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/backups", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list backups status=%d body=%s", w.Code, w.Body.String())
	}
	var backups ListResponse[BackupInfo]
	if err := json.NewDecoder(w.Body).Decode(&backups); err != nil {
		t.Fatalf("decode backups: %v", err)
	}
	if len(backups.Items) != 1 || backups.Items[0].ID != backup.ID || backups.Items[0].SHA256 != backup.SHA256 || backups.Items[0].DownloadURL == "" {
		t.Fatalf("unexpected backups: %#v", backups)
	}
	for _, name := range []string{"before export", "before restore"} {
		body = bytes.NewBufferString(fmt.Sprintf(`{"name":%q,"note":"cursor checkpoint"}`, name))
		req = httptest.NewRequest(http.MethodPost, "/api/v1/data/backups", body)
		auth(req)
		w = httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create backup %q status=%d body=%s", name, w.Code, w.Body.String())
		}
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/backups?limit=2", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first backup cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstBackupPage ListResponse[BackupInfo]
	if err := json.NewDecoder(w.Body).Decode(&firstBackupPage); err != nil {
		t.Fatalf("decode first backup cursor page: %v", err)
	}
	if len(firstBackupPage.Items) != 2 || !firstBackupPage.HasMore || firstBackupPage.NextBefore == "" || firstBackupPage.NextBeforeID == "" {
		t.Fatalf("unexpected first backup cursor page: %#v", firstBackupPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/backups?limit=2&before="+url.QueryEscape(firstBackupPage.NextBefore)+"&before_id="+url.QueryEscape(firstBackupPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second backup cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondBackupPage ListResponse[BackupInfo]
	if err := json.NewDecoder(w.Body).Decode(&secondBackupPage); err != nil {
		t.Fatalf("decode second backup cursor page: %v", err)
	}
	if len(secondBackupPage.Items) == 0 {
		t.Fatalf("expected second backup cursor page to continue: %#v", secondBackupPage)
	}
	seenBackupIDs := map[string]struct{}{}
	for _, item := range firstBackupPage.Items {
		seenBackupIDs[item.ID] = struct{}{}
	}
	for _, item := range secondBackupPage.Items {
		if _, ok := seenBackupIDs[item.ID]; ok {
			t.Fatalf("backup cursor repeated %s across pages: first=%#v second=%#v", item.ID, firstBackupPage, secondBackupPage)
		}
	}
}

func TestSetupTenantSyncRequiresRegisteredHubAndWorksPublicly(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	server := NewHTTPServer(svc, "", "test")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/tenants/sync", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("public tenant sync before Hub registration status=%d body=%s", w.Code, w.Body.String())
	}

	req = jsonRequest(http.MethodPost, "/api/v1/setup/admin", InitializeAdminInput{Username: "admin", Password: "change-me-123"})
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("initialize admin status=%d body=%s", w.Code, w.Body.String())
	}
	var initResult InitializeAdminResult
	if err := json.NewDecoder(w.Body).Decode(&initResult); err != nil {
		t.Fatalf("decode init result: %v", err)
	}

	var publicKeyPEM string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/platform/providers/register":
			var body map[string]any
			readSignedJSON(t, r, &body)
			publicKeyPEM, _ = body["public_key"].(string)
			if strings.TrimSpace(publicKeyPEM) == "" {
				t.Fatalf("register body missing public_key: %#v", body)
			}
			verifyHubRequestSignature(t, r, publicKeyPEM)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		case "/api/platform/tenants/list":
			verifyHubRequestSignature(t, r, publicKeyPEM)
			writeJSON(w, http.StatusOK, map[string]any{"tenants": []map[string]any{{"id": "tenant-login", "name": "Login Tenant", "status": "active", "primary_domain": "login.example", "virtual_mail_domain": "login.data.example"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	for _, step := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodPost, "/api/v1/data/admin/hub-registration", SaveHubRegistrationInput{HubBaseURL: hub.URL, PlatformID: "datasrv-http", PlatformName: "DataSrv HTTP"}},
		{http.MethodPost, "/api/v1/data/admin/hub-registration/register", map[string]any{}},
	} {
		req = jsonRequest(step.method, step.path, step.body)
		req.Header.Set("Authorization", "Bearer "+initResult.Token)
		w = httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s status=%d body=%s", step.method, step.path, w.Code, w.Body.String())
		}
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/setup/tenants/sync", nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("public tenant sync after Hub registration status=%d body=%s", w.Code, w.Body.String())
	}
	var synced SyncHubTenantsResult
	if err := json.NewDecoder(w.Body).Decode(&synced); err != nil {
		t.Fatalf("decode synced tenants: %v", err)
	}
	if synced.Synced != 1 || synced.Tenants[0].ID != "tenant-login" || synced.Tenants[0].VirtualMailDomain != "login.data.example" {
		t.Fatalf("unexpected synced tenants: %#v", synced)
	}
}

func TestHTTPBooleanQueryParamsRejectInvalidValues(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	server := NewHTTPServer(NewService(store, "sqlite"), "root-token-0123456789012345", "test")

	for _, path := range []string{
		"/api/v1/data/access/api-keys?enabled=maybe",
		"/api/v1/data/connectors?enabled=maybe",
		"/api/v1/data/connectors/health?enabled=maybe",
		"/api/v1/data/approvals?overdue=maybe",
		"/api/v1/data/inbox?include_ok=maybe",
		"/api/v1/data/inbox/summary?include_ok=maybe",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		authBearer(req, "root-token-0123456789012345", "data_admin")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s, want 400", path, w.Code, w.Body.String())
		}
	}
	for _, tc := range []struct {
		path        string
		contentType string
		body        string
	}{
		{path: "/api/v1/data/datasets/sales.orders/records/import.csv?dry_run=maybe", contentType: "text/csv", body: "id,title\nSO-1,Order\n"},
		{path: "/api/v1/data/datasets/sales.orders/records/import.csv/jobs?dry_run=maybe", contentType: "text/csv", body: "id,title\nSO-1,Order\n"},
		{path: "/api/v1/data/datasets/sales.orders/records/import.jsonl?dry_run=maybe", contentType: "application/x-ndjson", body: "{\"id\":\"SO-1\"}\n"},
		{path: "/api/v1/data/datasets/sales.orders/records/import.jsonl/jobs?dry_run=maybe", contentType: "application/x-ndjson", body: "{\"id\":\"SO-1\"}\n"},
	} {
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", tc.contentType)
		authBearer(req, "root-token-0123456789012345", "data_admin")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s, want 400", tc.path, w.Code, w.Body.String())
		}
	}

	for _, path := range []string{
		"/api/v1/data/access/api-keys?enabled=false",
		"/api/v1/data/connectors?enabled=0",
		"/api/v1/data/connectors/health?enabled=true",
		"/api/v1/data/approvals?overdue=0",
		"/api/v1/data/inbox?include_ok=1",
		"/api/v1/data/inbox/summary?include_ok=false",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		authBearer(req, "root-token-0123456789012345", "data_admin")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s, want 200", path, w.Code, w.Body.String())
		}
	}
}

func TestHTTPEnumQueryParamsRejectInvalidValues(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	server := NewHTTPServer(NewService(store, "sqlite"), "root-token-0123456789012345", "test")

	for _, path := range []string{
		"/api/v1/data/approvals?status=pendng",
		"/api/v1/data/operation-plans?status=apprvoed",
		"/api/v1/data/operation-plans?operation=bulk_updat_records",
		"/api/v1/data/import-jobs?status=faild",
		"/api/v1/data/export-jobs?status=succes",
		"/api/v1/data/events/dead-letter?status=opne",
		"/api/v1/data/datasets/sales.orders/schema-proposals?status=pendng",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		authBearer(req, "root-token-0123456789012345", "data_admin")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s, want 400", path, w.Code, w.Body.String())
		}
	}

	for _, path := range []string{
		"/api/v1/data/approvals?status=pending",
		"/api/v1/data/operation-plans?status=approved&operation=bulk_update_records",
		"/api/v1/data/import-jobs?status=failed",
		"/api/v1/data/export-jobs?status=completed",
		"/api/v1/data/events/dead-letter?status=open",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		authBearer(req, "root-token-0123456789012345", "data_admin")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s, want 200", path, w.Code, w.Body.String())
		}
	}
}

func TestHTTPServerBootstrapsTemplates(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	server := NewHTTPServer(NewService(store, "sqlite"), "test-token-0123456789012345", "test")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/data/templates/bootstrap", bytes.NewBufferString(`{}`))
	authRole(req, "data_user")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected data_user bootstrap forbidden, status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/templates/bootstrap", bytes.NewBufferString(`{"dry_run":true,"domains":["sales","finance"]}`))
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("domain dry-run bootstrap templates status=%d body=%s", w.Code, w.Body.String())
	}
	var domainPreview BootstrapTemplatesResult
	if err := json.NewDecoder(w.Body).Decode(&domainPreview); err != nil {
		t.Fatalf("decode domain dry-run bootstrap: %v", err)
	}
	if len(domainPreview.WouldCreate) != 10 || len(domainPreview.Created) != 0 || len(domainPreview.Errors) != 0 {
		t.Fatalf("unexpected domain dry-run bootstrap result: %#v", domainPreview)
	}
	for _, tmpl := range domainPreview.WouldCreate {
		if tmpl.Domain != "sales" && tmpl.Domain != "finance" {
			t.Fatalf("domain dry-run included wrong domain template: %#v", tmpl)
		}
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/templates/bootstrap", bytes.NewBufferString(`{"dry_run":true}`))
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("dry-run bootstrap templates status=%d body=%s", w.Code, w.Body.String())
	}
	var preview BootstrapTemplatesResult
	if err := json.NewDecoder(w.Body).Decode(&preview); err != nil {
		t.Fatalf("decode dry-run bootstrap: %v", err)
	}
	if len(preview.WouldCreate) != len(datasetTemplates) || len(preview.Created) != 0 || len(preview.Errors) != 0 {
		t.Fatalf("unexpected dry-run bootstrap result: %#v", preview)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("dry-run bootstrap should not create dataset, status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/templates/bootstrap", bytes.NewBufferString(`{}`))
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("bootstrap templates status=%d body=%s", w.Code, w.Body.String())
	}
	var bootstrap BootstrapTemplatesResult
	if err := json.NewDecoder(w.Body).Decode(&bootstrap); err != nil {
		t.Fatalf("decode bootstrap: %v", err)
	}
	if len(bootstrap.Created) != len(datasetTemplates) || len(bootstrap.Errors) != 0 {
		t.Fatalf("unexpected bootstrap result: %#v", bootstrap)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets?limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first dataset cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstDatasetPage ListResponse[Dataset]
	if err := json.NewDecoder(w.Body).Decode(&firstDatasetPage); err != nil {
		t.Fatalf("decode first dataset cursor page: %v", err)
	}
	if len(firstDatasetPage.Items) != 1 || !firstDatasetPage.HasMore || firstDatasetPage.NextBefore == "" || firstDatasetPage.NextBeforeID == "" {
		t.Fatalf("unexpected first dataset cursor page: %#v", firstDatasetPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets?limit=1&before="+url.QueryEscape(firstDatasetPage.NextBefore)+"&before_id="+url.QueryEscape(firstDatasetPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second dataset cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondDatasetPage ListResponse[Dataset]
	if err := json.NewDecoder(w.Body).Decode(&secondDatasetPage); err != nil {
		t.Fatalf("decode second dataset cursor page: %v", err)
	}
	if len(secondDatasetPage.Items) == 0 || secondDatasetPage.Items[0].ID == firstDatasetPage.Items[0].ID {
		t.Fatalf("unexpected second dataset cursor page: first=%#v second=%#v", firstDatasetPage, secondDatasetPage)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/templates/bootstrap", bytes.NewBufferString(`{}`))
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("repeat bootstrap templates status=%d body=%s", w.Code, w.Body.String())
	}
	var repeated BootstrapTemplatesResult
	if err := json.NewDecoder(w.Body).Decode(&repeated); err != nil {
		t.Fatalf("decode repeated bootstrap: %v", err)
	}
	if len(repeated.Created) != 0 || len(repeated.Skipped) != len(datasetTemplates) || len(repeated.Errors) != 0 {
		t.Fatalf("unexpected repeated bootstrap result: %#v", repeated)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/fields", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list bootstrapped fields status=%d body=%s", w.Code, w.Body.String())
	}
	var fields ListResponse[FieldDefinition]
	if err := json.NewDecoder(w.Body).Decode(&fields); err != nil {
		t.Fatalf("decode bootstrapped fields: %v", err)
	}
	if !containsFieldDefinition(fields.Items, "order_no") || !containsFieldDefinition(fields.Items, "amount") || !containsFieldDefinition(fields.Items, "customer_ref") {
		t.Fatalf("unexpected bootstrapped fields: %#v", fields)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/fields?limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first field cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstFieldPage ListResponse[FieldDefinition]
	if err := json.NewDecoder(w.Body).Decode(&firstFieldPage); err != nil {
		t.Fatalf("decode first field cursor page: %v", err)
	}
	if len(firstFieldPage.Items) != 1 || !firstFieldPage.HasMore || firstFieldPage.NextBefore == "" || firstFieldPage.NextBeforeID == "" {
		t.Fatalf("unexpected first field cursor page: %#v", firstFieldPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.orders/fields?limit=1&before="+url.QueryEscape(firstFieldPage.NextBefore)+"&before_id="+url.QueryEscape(firstFieldPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second field cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondFieldPage ListResponse[FieldDefinition]
	if err := json.NewDecoder(w.Body).Decode(&secondFieldPage); err != nil {
		t.Fatalf("decode second field cursor page: %v", err)
	}
	if len(secondFieldPage.Items) == 0 || secondFieldPage.Items[0].ID == firstFieldPage.Items[0].ID {
		t.Fatalf("unexpected second field cursor page: first=%#v second=%#v", firstFieldPage, secondFieldPage)
	}
}

func TestValidateRecordDataSupportsBusinessReferenceTypes(t *testing.T) {
	fields := []FieldDefinition{
		{Key: "customer_ref", Type: "record_ref", Required: true, Config: map[string]any{"ref_dataset": "sales.customers"}},
		{Key: "owner_ref", Type: "person_ref"},
		{Key: "attachments", Type: "file_ref"},
		{Key: "count", Type: "integer"},
		{Key: "amount", Type: "money"},
	}
	result := validateRecordDataResult("sales.orders", fields, map[string]any{
		"customer_ref": "CUST-001",
		"owner_ref":    "EMP-001",
		"attachments":  "file-001",
		"count":        3,
		"amount":       12.5,
	})
	if !result.Valid {
		t.Fatalf("expected reference and business numeric fields to validate: %#v", result)
	}
	result = validateRecordDataResult("sales.orders", fields, map[string]any{
		"customer_ref": 42,
		"count":        3.5,
		"amount":       12.5,
	})
	if result.Valid || len(result.Errors) == 0 {
		t.Fatalf("expected invalid reference/integer errors: %#v", result)
	}
}

func TestValidateFinanceVoucherBalance(t *testing.T) {
	fields := []FieldDefinition{
		{Key: "voucher_no", Type: "string", Required: true},
		{Key: "period", Type: "string", Required: true},
		{Key: "voucher_type", Type: "string", Required: true},
		{Key: "debit_total", Type: "number", Required: true},
		{Key: "credit_total", Type: "number", Required: true},
		{Key: "lines", Type: "array"},
	}
	result := validateRecordDataResult("finance.vouchers", fields, map[string]any{
		"voucher_no":   "VCH-OK-0001",
		"period":       "2026-05",
		"voucher_type": "receipt",
		"debit_total":  8800,
		"credit_total": 8800,
		"lines": []any{
			map[string]any{"account_code": "1001", "debit": 8800, "credit": 0},
			map[string]any{"account_code": "6001", "debit": 0, "credit": 8800},
		},
	})
	if !result.Valid {
		t.Fatalf("expected balanced voucher to validate: %#v", result)
	}
	result = validateRecordDataResult("finance.vouchers", fields, map[string]any{
		"voucher_no":   "VCH-BAD-0001",
		"period":       "2026-05",
		"voucher_type": "receipt",
		"debit_total":  8800,
		"credit_total": 7800,
		"lines": []any{
			map[string]any{"account_code": "1001", "debit": 8800, "credit": 0},
			map[string]any{"account_code": "6001", "debit": 0, "credit": 7800},
		},
	})
	if result.Valid || !containsString(result.Errors, "voucher debit_total must equal credit_total") || !containsString(result.Errors, "voucher line debit sum must equal line credit sum") {
		t.Fatalf("expected voucher balance validation errors: %#v", result)
	}
}

func TestRunQualityCheckDetectsMissingRecordReferences(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	server := NewHTTPServer(svc, "test-token-0123456789012345", "test")
	ctx := context.Background()
	p := Principal{TenantID: "tenant_1", UserID: "user_1", Role: "data_admin"}
	if _, err := svc.CreateDatasetFromTemplate(ctx, p, "sales.customers", CreateFromTemplateInput{}); err != nil {
		t.Fatalf("CreateDatasetFromTemplate customers: %v", err)
	}
	if _, err := svc.CreateDatasetFromTemplate(ctx, p, "sales.orders", CreateFromTemplateInput{}); err != nil {
		t.Fatalf("CreateDatasetFromTemplate orders: %v", err)
	}
	if _, err := svc.CreateRecord(ctx, p, "sales.orders", CreateRecordInput{ID: "SO-REF-1", Data: map[string]any{"order_no": "SO-REF-1", "customer": "Missing Co", "customer_ref": "CUST-MISSING", "amount": 100}}); err != nil {
		t.Fatalf("CreateRecord order: %v", err)
	}
	result, err := svc.RunQualityCheck(ctx, p, "sales.orders", RunQualityCheckInput{Checks: []string{"relationship_refs"}, Limit: 100})
	if err != nil {
		t.Fatalf("RunQualityCheck missing ref: %v", err)
	}
	if result.Valid || !containsQualityIssue(result.Issues, "relationship_refs", "customer_ref") {
		t.Fatalf("expected relationship reference issue: %#v", result)
	}
	if _, err := svc.CreateRecord(ctx, p, "sales.customers", CreateRecordInput{ID: "CUST-MISSING", Data: map[string]any{"customer_no": "CUST-MISSING", "name": "Missing Co"}}); err != nil {
		t.Fatalf("CreateRecord customer: %v", err)
	}
	for _, orderID := range []string{"SO-REF-2", "SO-REF-3"} {
		if _, err := svc.CreateRecord(ctx, p, "sales.orders", CreateRecordInput{ID: orderID, Data: map[string]any{"order_no": orderID, "customer": "Missing Co", "customer_ref": "CUST-MISSING", "amount": 100}}); err != nil {
			t.Fatalf("CreateRecord %s: %v", orderID, err)
		}
	}
	result, err = svc.RunQualityCheck(ctx, p, "sales.orders", RunQualityCheckInput{Checks: []string{"relationship_refs"}, Limit: 100})
	if err != nil {
		t.Fatalf("RunQualityCheck resolved ref: %v", err)
	}
	if !result.Valid || containsQualityIssue(result.Issues, "relationship_refs", "customer_ref") {
		t.Fatalf("expected relationship reference issue resolved: %#v", result)
	}
	related, err := svc.GetRelatedRecords(ctx, p, "sales.orders", "SO-REF-1", QueryRelatedRecordsInput{Limit: 20})
	if err != nil {
		t.Fatalf("GetRelatedRecords order: %v", err)
	}
	if !containsRelatedRecord(related.Links, "outgoing", "sales.customers", "CUST-MISSING") {
		t.Fatalf("expected outgoing customer related record: %#v", related)
	}
	related, err = svc.GetRelatedRecords(ctx, p, "sales.customers", "CUST-MISSING", QueryRelatedRecordsInput{Limit: 20})
	if err != nil {
		t.Fatalf("GetRelatedRecords customer: %v", err)
	}
	if !containsRelatedRecord(related.Links, "incoming", "sales.orders", "SO-REF-1") {
		t.Fatalf("expected incoming sales order related record: %#v", related)
	}
	relatedPage, err := svc.GetRelatedRecords(ctx, p, "sales.customers", "CUST-MISSING", QueryRelatedRecordsInput{Limit: 2})
	if err != nil {
		t.Fatalf("GetRelatedRecords first page: %v", err)
	}
	if len(relatedPage.Links) != 2 || !relatedPage.HasMore || relatedPage.NextBeforeID == "" {
		t.Fatalf("unexpected related first page cursor: %#v", relatedPage)
	}
	relatedPage, err = svc.GetRelatedRecords(ctx, p, "sales.customers", "CUST-MISSING", QueryRelatedRecordsInput{Limit: 2, BeforeID: relatedPage.NextBeforeID})
	if err != nil {
		t.Fatalf("GetRelatedRecords second page: %v", err)
	}
	if len(relatedPage.Links) != 1 || !containsRelatedRecord(relatedPage.Links, "incoming", "sales.orders", "SO-REF-1") {
		t.Fatalf("related cursor should continue by stable link key: %#v", relatedPage)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.customers/records/CUST-MISSING/related?limit=2", nil)
	auth(req)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first related records cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstRelatedPage RelatedRecordsResult
	if err := json.NewDecoder(w.Body).Decode(&firstRelatedPage); err != nil {
		t.Fatalf("decode first related records cursor page: %v", err)
	}
	if firstRelatedPage.DatasetID != "sales.customers" || firstRelatedPage.RecordID != "CUST-MISSING" || len(firstRelatedPage.Links) != 2 || !firstRelatedPage.HasMore || firstRelatedPage.NextBeforeID == "" {
		t.Fatalf("unexpected first related records cursor page: %#v", firstRelatedPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/sales.customers/records/CUST-MISSING/related?limit=2&before_id="+url.QueryEscape(firstRelatedPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second related records cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondRelatedPage RelatedRecordsResult
	if err := json.NewDecoder(w.Body).Decode(&secondRelatedPage); err != nil {
		t.Fatalf("decode second related records cursor page: %v", err)
	}
	if len(secondRelatedPage.Links) != 1 || !containsRelatedRecord(secondRelatedPage.Links, "incoming", "sales.orders", "SO-REF-1") || containsRelatedRecord(firstRelatedPage.Links, "incoming", "sales.orders", "SO-REF-1") {
		t.Fatalf("unexpected second related records cursor page: first=%#v second=%#v", firstRelatedPage, secondRelatedPage)
	}
}

func TestHTTPServerListsBusinessDomainCatalogs(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	server := NewHTTPServer(NewService(store, "sqlite"), "test-token-0123456789012345", "test")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/data/domains", nil)
	auth(req)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list domains status=%d body=%s", w.Code, w.Body.String())
	}
	var domainsResp ListResponse[BusinessDomainCatalog]
	if err := json.NewDecoder(w.Body).Decode(&domainsResp); err != nil {
		t.Fatalf("decode domains: %v", err)
	}
	domains := domainsResp.Items
	if !containsDomainCatalog(domains, "sales") || !containsDomainCatalog(domains, "procurement") || !containsDomainCatalog(domains, "assets") {
		t.Fatalf("expected enterprise domain catalogs: %#v", domains)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/domains?limit=1", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first domain cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstDomainPage ListResponse[BusinessDomainCatalog]
	if err := json.NewDecoder(w.Body).Decode(&firstDomainPage); err != nil {
		t.Fatalf("decode first domain cursor page: %v", err)
	}
	if len(firstDomainPage.Items) != 1 || !firstDomainPage.HasMore || firstDomainPage.NextBeforeID == "" {
		t.Fatalf("unexpected first domain cursor page: %#v", firstDomainPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/domains?limit=1&before_id="+url.QueryEscape(firstDomainPage.NextBeforeID), nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second domain cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondDomainPage ListResponse[BusinessDomainCatalog]
	if err := json.NewDecoder(w.Body).Decode(&secondDomainPage); err != nil {
		t.Fatalf("decode second domain cursor page: %v", err)
	}
	if len(secondDomainPage.Items) == 0 || secondDomainPage.Items[0].Domain == firstDomainPage.Items[0].Domain {
		t.Fatalf("unexpected second domain cursor page: first=%#v second=%#v", firstDomainPage, secondDomainPage)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/domains/sales", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get sales domain status=%d body=%s", w.Code, w.Body.String())
	}
	var sales BusinessDomainCatalog
	if err := json.NewDecoder(w.Body).Decode(&sales); err != nil {
		t.Fatalf("decode sales domain: %v", err)
	}
	if sales.Domain != "sales" || sales.Initialized || len(sales.UseCases) == 0 || len(sales.Templates) != 4 || len(sales.BusinessActions) == 0 || len(sales.BusinessViews) == 0 || len(sales.Reports) == 0 || len(sales.Dashboards) == 0 {
		t.Fatalf("unexpected sales domain before bootstrap: %#v", sales)
	}
	if !containsDomainUseCase(sales.UseCases, "sales.record_order") {
		t.Fatalf("expected sales record order use case: %#v", sales.UseCases)
	}
	if !containsString(sales.MissingTemplates, "sales.orders") || !containsString(sales.MissingTemplates, "sales.customers") || !containsString(sales.MissingTemplates, "sales.contacts") || !containsString(sales.MissingTemplates, "sales.opportunities") {
		t.Fatalf("expected sales missing templates: %#v", sales.MissingTemplates)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/templates/bootstrap", bytes.NewBufferString(`{"domains":["sales"]}`))
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("bootstrap sales status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/domains/sales", nil)
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get bootstrapped sales domain status=%d body=%s", w.Code, w.Body.String())
	}
	var bootstrappedSales BusinessDomainCatalog
	if err := json.NewDecoder(w.Body).Decode(&bootstrappedSales); err != nil {
		t.Fatalf("decode bootstrapped sales domain: %v", err)
	}
	if !bootstrappedSales.Initialized || len(bootstrappedSales.Datasets) != 4 || len(bootstrappedSales.MissingTemplates) != 0 {
		t.Fatalf("unexpected sales domain after bootstrap: %#v", bootstrappedSales)
	}
}

func TestHTTPServerResolvesBusinessIntent(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	server := NewHTTPServer(NewService(store, "sqlite"), "test-token-0123456789012345", "test")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/data/intent/resolve", bytes.NewBufferString(`{"query":"\u5e93\u5b58\u9884\u8b66\uff0c\u66f4\u65b0\u4f4e\u5e93\u5b58\u6570\u91cf","limit":3}`))
	auth(req)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve intent status=%d body=%s", w.Code, w.Body.String())
	}
	var result ResolveBusinessIntentResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode resolve intent: %v", err)
	}
	if len(result.Matches) == 0 {
		t.Fatalf("expected intent matches: %#v", result)
	}
	if result.Matches[0].UseCase.ID != "inventory.stock_update" || result.Matches[0].UseCase.PreferredAction != "inventory.stock_update" {
		t.Fatalf("unexpected top intent match: %#v", result.Matches[0])
	}
	if !containsIntentStep(result.Matches[0].NextSteps, "bootstrap_templates", true) || !containsIntentStep(result.Matches[0].NextSteps, "execute_business_action", true) || !containsIntentStep(result.Matches[0].NextSteps, "run_report", false) {
		t.Fatalf("expected intent next steps for inventory stock update: %#v", result.Matches[0].NextSteps)
	}
	if !containsIntentStepRequiredField(result.Matches[0].NextSteps, "execute_business_action", "quantity") {
		t.Fatalf("expected quantity in intent action input contract: %#v", result.Matches[0].NextSteps)
	}
	if !containsIntentStepDataTemplateKey(result.Matches[0].NextSteps, "execute_business_action", "quantity") {
		t.Fatalf("expected quantity in intent action data template: %#v", result.Matches[0].NextSteps)
	}
	if !containsIntentStepBodyTemplate(result.Matches[0].NextSteps, "execute_business_action", "business_action_id", "inventory.stock_update") {
		t.Fatalf("expected business action id in intent body template: %#v", result.Matches[0].NextSteps)
	}
	if !containsIntentStepToolCallTemplate(result.Matches[0].NextSteps, "execute_business_action", "mis_data", "inventory.stock_update") {
		t.Fatalf("expected mis_data tool call template for intent: %#v", result.Matches[0].NextSteps)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/intent/resolve", bytes.NewBufferString(`{"query":"\u91c7\u8d2d\u72b6\u6001\u6536\u8d27","domain":"procurement","limit":2}`))
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve procurement intent status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode procurement intent: %v", err)
	}
	if len(result.Matches) == 0 || result.Matches[0].Domain != "procurement" || result.Matches[0].UseCase.PreferredAction != "procurement.purchase_order_status_update" {
		t.Fatalf("unexpected procurement intent match: %#v", result)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/intent/resolve", bytes.NewBufferString(`{"query":"add vendor supplier payment terms","domain":"procurement","limit":2}`))
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve supplier intent status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode supplier intent: %v", err)
	}
	if len(result.Matches) == 0 || result.Matches[0].Domain != "procurement" || result.Matches[0].UseCase.PreferredAction != "procurement.supplier_upsert" || result.Matches[0].UseCase.PreferredView != "procurement.supplier_directory" {
		t.Fatalf("unexpected supplier intent match: %#v", result)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/intent/resolve", bytes.NewBufferString(`{"query":"prepare payroll pay run for May","domain":"hr","limit":2}`))
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve payroll intent status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode payroll intent: %v", err)
	}
	if len(result.Matches) == 0 || result.Matches[0].Domain != "hr" || result.Matches[0].UseCase.PreferredAction != "hr.payroll_upsert" || result.Matches[0].UseCase.PreferredReport != "hr.payroll_status_summary" {
		t.Fatalf("unexpected payroll intent match: %#v", result)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/intent/resolve", bytes.NewBufferString(`{"query":"approve employee vacation leave request","domain":"hr","limit":2}`))
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve leave intent status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode leave intent: %v", err)
	}
	if len(result.Matches) == 0 || result.Matches[0].Domain != "hr" || result.Matches[0].UseCase.PreferredAction != "hr.leave_request_upsert" || result.Matches[0].UseCase.PreferredView != "hr.leave_request_review" {
		t.Fatalf("unexpected leave intent match: %#v", result)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/intent/resolve", bytes.NewBufferString(`{"query":"track accounts receivable payment cashflow","domain":"finance","limit":2}`))
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve payment intent status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode payment intent: %v", err)
	}
	if len(result.Matches) == 0 || result.Matches[0].Domain != "finance" || result.Matches[0].UseCase.PreferredAction != "finance.payment_upsert" || result.Matches[0].UseCase.PreferredView != "finance.payment_tracker" {
		t.Fatalf("unexpected payment intent match: %#v", result)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/intent/resolve", bytes.NewBufferString(`{"query":"review department budget control for travel","domain":"finance","limit":2}`))
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve budget intent status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode budget intent: %v", err)
	}
	if len(result.Matches) == 0 || result.Matches[0].Domain != "finance" || result.Matches[0].UseCase.PreferredAction != "finance.budget_upsert" || result.Matches[0].UseCase.PreferredView != "finance.budget_control" {
		t.Fatalf("unexpected budget intent match: %#v", result)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/intent/resolve", bytes.NewBufferString(`{"query":"post accounting voucher journal for May","domain":"finance","limit":2}`))
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve voucher intent status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode voucher intent: %v", err)
	}
	if len(result.Matches) == 0 || result.Matches[0].Domain != "finance" || result.Matches[0].UseCase.PreferredAction != "finance.voucher_upsert" || result.Matches[0].UseCase.PreferredView != "finance.voucher_register" {
		t.Fatalf("unexpected voucher intent match: %#v", result)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/intent/resolve", bytes.NewBufferString(`{"query":"create department cost center for operations","domain":"company","limit":2}`))
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve department intent status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode department intent: %v", err)
	}
	if len(result.Matches) == 0 || result.Matches[0].Domain != "company" || result.Matches[0].UseCase.PreferredAction != "company.department_upsert" || result.Matches[0].UseCase.PreferredView != "company.department_directory" {
		t.Fatalf("unexpected department intent match: %#v", result)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/intent/resolve", bytes.NewBufferString(`{"query":"record warehouse receipt inventory movement","domain":"inventory","limit":2}`))
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve inventory movement intent status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode inventory movement intent: %v", err)
	}
	if len(result.Matches) == 0 || result.Matches[0].Domain != "inventory" || result.Matches[0].UseCase.PreferredAction != "inventory.movement_record" || result.Matches[0].UseCase.PreferredView != "inventory.movement_ledger" {
		t.Fatalf("unexpected inventory movement intent match: %#v", result)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/intent/resolve", bytes.NewBufferString(`{"query":"create warehouse storage location","domain":"inventory","limit":2}`))
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve warehouse intent status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode warehouse intent: %v", err)
	}
	if len(result.Matches) == 0 || result.Matches[0].Domain != "inventory" || result.Matches[0].UseCase.PreferredAction != "inventory.warehouse_upsert" || result.Matches[0].UseCase.PreferredView != "inventory.warehouse_directory" {
		t.Fatalf("unexpected warehouse intent match: %#v", result)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/intent/resolve", bytes.NewBufferString(`{"query":"update sales pipeline opportunity deal","domain":"sales","limit":2}`))
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve sales opportunity intent status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode sales opportunity intent: %v", err)
	}
	if len(result.Matches) == 0 || result.Matches[0].Domain != "sales" || result.Matches[0].UseCase.PreferredAction != "sales.opportunity_upsert" || result.Matches[0].UseCase.PreferredView != "sales.opportunity_pipeline" {
		t.Fatalf("unexpected sales opportunity intent match: %#v", result)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/intent/resolve", bytes.NewBufferString(`{"query":"update customer contact phone owner","domain":"sales","limit":2}`))
	auth(req)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve sales contact intent status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode sales contact intent: %v", err)
	}
	if len(result.Matches) == 0 || result.Matches[0].Domain != "sales" || result.Matches[0].UseCase.PreferredAction != "sales.contact_upsert" || result.Matches[0].UseCase.PreferredView != "sales.contact_directory" {
		t.Fatalf("unexpected sales contact intent match: %#v", result)
	}
}

func TestHTTPServerAPIKeyPoliciesConstrainAgents(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	server := NewHTTPServerWithAPIKeys(svc, "root-token-0123456789012345", "test", []APIKeyPolicy{
		{ID: "sales-agent", Key: "sales-agent-key-012345678901", TenantID: "tenant_1", UserID: "agent_sales", Role: "data_user", AllowedActions: []string{"sales.order_upsert"}},
		{ID: "sales-report-agent", Key: "sales-report-key-012345678901", TenantID: "tenant_1", UserID: "agent_sales_report", Role: "data_user", AllowedReports: []string{"sales.order_summary_by_stage"}, AllowedViews: []string{"sales.order_overview"}, AllowedDashboards: []string{"sales.overview"}},
		{ID: "hr-auditor-agent", Key: "hr-auditor-key-012345678901", TenantID: "tenant_1", UserID: "agent_hr", Role: "data_auditor", AllowedDatasets: []string{"hr.payroll"}},
		{ID: "limited-admin-agent", Key: "limited-admin-key-012345678901", TenantID: "tenant_1", UserID: "agent_limited_admin", Role: "data_admin", AllowedDomains: []string{"sales"}},
	})

	body := bytes.NewBufferString(`{"record_id":"SO-KEY-0001","dry_run":true,"data":{"order_no":"SO-KEY-0001","customer":"KeyCo","amount":1200}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/data/business-actions/sales.order_upsert/execute", body)
	authBearer(req, "sales-agent-key-012345678901", "")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("sales scoped key should execute sales action, status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/capabilities", nil)
	authBearer(req, "sales-agent-key-012345678901", "")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("sales scoped capabilities status=%d body=%s", w.Code, w.Body.String())
	}
	var caps DataCapabilities
	if err := json.NewDecoder(w.Body).Decode(&caps); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if caps.APIKeyID != "sales-agent" || !containsString(caps.Domains, "sales") || containsString(caps.Domains, "finance") || containsBusinessAction(caps.BusinessActions, "finance.expense_submit") {
		t.Fatalf("sales scoped capabilities leaked unauthorized scope: %#v", caps)
	}
	if caps.Access.AuthenticatedBy != "api_key" || caps.Access.ScopeMode != "api_key_scoped" || caps.Access.RawDatasetAllowed || caps.Access.AdminAllowed || !containsString(caps.Access.AllowedActions, "sales.order_upsert") || caps.Access.VisibleCounts["business_actions"] != 1 {
		t.Fatalf("sales scoped capabilities returned wrong access summary: %#v", caps.Access)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/query", bytes.NewBufferString(`{"limit":1}`))
	authBearer(req, "sales-agent-key-012345678901", "")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("business action key should not read raw sales dataset without allow_raw_data, status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"record_id":"EXP-KEY-0001","dry_run":true,"data":{"expense_no":"EXP-KEY-0001","applicant":"Alice","amount":100}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-actions/finance.expense_submit/execute", body)
	authBearer(req, "sales-agent-key-012345678901", "")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("sales scoped key should not execute finance action, status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/templates/bootstrap", bytes.NewBufferString(`{"domains":["sales"]}`))
	authBearer(req, "limited-admin-key-012345678901", "")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("admin role without allow_admin should be forbidden, status=%d body=%s", w.Code, w.Body.String())
	}

	p := Principal{TenantID: "tenant_1", UserID: "setup", Role: "data_admin"}
	if _, err := svc.CreateDatasetFromTemplate(context.Background(), p, "sales.orders", CreateFromTemplateInput{}); err != nil {
		t.Fatalf("CreateDatasetFromTemplate sales orders: %v", err)
	}
	if _, err := svc.CreateRecord(context.Background(), p, "sales.orders", CreateRecordInput{ID: "SO-REPORT-1", Data: map[string]any{"order_no": "SO-REPORT-1", "customer": "ReportCo", "amount": 1500, "stage": "confirmed"}}); err != nil {
		t.Fatalf("CreateRecord sales order: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/access/presets", nil)
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list access presets status=%d body=%s", w.Code, w.Body.String())
	}
	var accessPresetsResp ListResponse[AccessPolicyPreset]
	if err := json.NewDecoder(w.Body).Decode(&accessPresetsResp); err != nil {
		t.Fatalf("decode access presets: %v", err)
	}
	accessPresets := accessPresetsResp.Items
	if !containsAccessPolicyPreset(accessPresets, "sales-operator") || !containsAccessPolicyPreset(accessPresets, "finance-reporter") {
		t.Fatalf("expected common access presets: %#v", accessPresets)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/access/presets?limit=1", nil)
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first access preset cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstAccessPresetPage ListResponse[AccessPolicyPreset]
	if err := json.NewDecoder(w.Body).Decode(&firstAccessPresetPage); err != nil {
		t.Fatalf("decode first access preset cursor page: %v", err)
	}
	if len(firstAccessPresetPage.Items) != 1 || !firstAccessPresetPage.HasMore || firstAccessPresetPage.NextBeforeID == "" {
		t.Fatalf("unexpected first access preset cursor page: %#v", firstAccessPresetPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/access/presets?limit=1&before_id="+url.QueryEscape(firstAccessPresetPage.NextBeforeID), nil)
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second access preset cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondAccessPresetPage ListResponse[AccessPolicyPreset]
	if err := json.NewDecoder(w.Body).Decode(&secondAccessPresetPage); err != nil {
		t.Fatalf("decode second access preset cursor page: %v", err)
	}
	if len(secondAccessPresetPage.Items) == 0 || secondAccessPresetPage.Items[0].ID == firstAccessPresetPage.Items[0].ID {
		t.Fatalf("unexpected second access preset cursor page: first=%#v second=%#v", firstAccessPresetPage, secondAccessPresetPage)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/reports/sales.order_summary_by_stage/run", bytes.NewBufferString(`{}`))
	authBearer(req, "sales-report-key-012345678901", "")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("report scoped key should run allowed sales report, status=%d body=%s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/datasets/sales.orders/records/query", bytes.NewBufferString(`{"limit":1}`))
	authBearer(req, "sales-report-key-012345678901", "")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("report scoped key should not read raw dataset, status=%d body=%s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/business-actions/sales.order_upsert/execute", bytes.NewBufferString(`{"record_id":"SO-REPORT-2","dry_run":true,"data":{"order_no":"SO-REPORT-2","customer":"ReportCo","amount":100}}`))
	authBearer(req, "sales-report-key-012345678901", "")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("report scoped key should not execute sales action, status=%d body=%s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"id":"finance-report-web","user_id":"agent_finance_report","role":"data_user","allowed_reports":["finance.expense_by_department"],"note":"finance reporting only"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/access/api-keys", body)
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("admin token should create scoped api key, status=%d body=%s", w.Code, w.Body.String())
	}
	var createdKey CreateAPIKeyPolicyResult
	if err := json.NewDecoder(w.Body).Decode(&createdKey); err != nil {
		t.Fatalf("decode created key: %v", err)
	}
	if createdKey.Key == "" || createdKey.Policy.ID != "finance-report-web" || !containsString(createdKey.Policy.AllowedReports, "finance.expense_by_department") {
		t.Fatalf("unexpected created API key policy: %#v", createdKey)
	}
	body = bytes.NewBufferString(`{"id":"expired-report-web","user_id":"agent_expired","role":"data_user","allowed_reports":["finance.expense_by_department"],"expires_at":"2000-01-01T00:00:00Z"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/access/api-keys", body)
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create expired managed api key status=%d body=%s", w.Code, w.Body.String())
	}
	var expiredKey CreateAPIKeyPolicyResult
	if err := json.NewDecoder(w.Body).Decode(&expiredKey); err != nil {
		t.Fatalf("decode expired key: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/capabilities", nil)
	authBearer(req, expiredKey.Key, "")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expired managed key should be unauthorized, status=%d body=%s", w.Code, w.Body.String())
	}
	soonExpiresAt := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339Nano)
	body = bytes.NewBufferString(fmt.Sprintf(`{"id":"soon-report-web","user_id":"agent_soon","role":"data_user","allowed_reports":["finance.expense_by_department"],"expires_at":%q}`, soonExpiresAt))
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/access/api-keys", body)
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create soon-expiring managed api key status=%d body=%s", w.Code, w.Body.String())
	}
	body = bytes.NewBufferString(`{"id":"raw-finance-web","user_id":"agent_raw","role":"data_user","allowed_domains":["finance"],"allowed_reports":["finance.expense_by_department"],"allow_raw_data":true,"note":"raw access under review"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/access/api-keys", body)
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create raw managed api key status=%d body=%s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/access/api-keys", nil)
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list managed api keys status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), createdKey.Key) {
		t.Fatalf("managed api key list must not expose secret: %s", w.Body.String())
	}
	var listedKeys ListResponse[APIKeyPolicyRecord]
	if err := json.Unmarshal(w.Body.Bytes(), &listedKeys); err != nil {
		t.Fatalf("decode managed api key list: %v", err)
	}
	if !containsAPIKeyPolicyStatus(listedKeys.Items, "finance-report-web", "active") || !containsAPIKeyPolicyStatus(listedKeys.Items, "expired-report-web", "expired") || !containsAPIKeyPolicyStatus(listedKeys.Items, "soon-report-web", "expiring_soon") {
		t.Fatalf("managed key list did not include expected statuses: %#v", listedKeys)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/access/api-keys?limit=2", nil)
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first managed api key cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var firstKeyPage ListResponse[APIKeyPolicyRecord]
	if err := json.Unmarshal(w.Body.Bytes(), &firstKeyPage); err != nil {
		t.Fatalf("decode first managed api key cursor page: %v", err)
	}
	if len(firstKeyPage.Items) != 2 || !firstKeyPage.HasMore || firstKeyPage.NextBefore == "" || firstKeyPage.NextBeforeID == "" {
		t.Fatalf("unexpected first managed api key cursor page: %#v", firstKeyPage)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/access/api-keys?limit=2&before="+url.QueryEscape(firstKeyPage.NextBefore)+"&before_id="+url.QueryEscape(firstKeyPage.NextBeforeID), nil)
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second managed api key cursor page status=%d body=%s", w.Code, w.Body.String())
	}
	var secondKeyPage ListResponse[APIKeyPolicyRecord]
	if err := json.Unmarshal(w.Body.Bytes(), &secondKeyPage); err != nil {
		t.Fatalf("decode second managed api key cursor page: %v", err)
	}
	if len(secondKeyPage.Items) == 0 {
		t.Fatalf("expected second managed api key cursor page to continue: %#v", secondKeyPage)
	}
	seenKeyIDs := map[string]struct{}{}
	for _, item := range firstKeyPage.Items {
		seenKeyIDs[item.ID] = struct{}{}
	}
	for _, item := range secondKeyPage.Items {
		if _, ok := seenKeyIDs[item.ID]; ok {
			t.Fatalf("managed api key cursor repeated %s across pages: first=%#v second=%#v", item.ID, firstKeyPage, secondKeyPage)
		}
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/access/api-keys?status=expired", nil)
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list expired managed api keys status=%d body=%s", w.Code, w.Body.String())
	}
	listedKeys = ListResponse[APIKeyPolicyRecord]{}
	if err := json.Unmarshal(w.Body.Bytes(), &listedKeys); err != nil {
		t.Fatalf("decode expired managed api key list: %v", err)
	}
	if !containsAPIKeyPolicyStatus(listedKeys.Items, "expired-report-web", "expired") || containsAPIKeyPolicyStatus(listedKeys.Items, "finance-report-web", "active") {
		t.Fatalf("expired managed key filter returned wrong keys: %#v", listedKeys)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/access/api-keys?status=expiring_soon", nil)
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list soon-expiring managed api keys status=%d body=%s", w.Code, w.Body.String())
	}
	listedKeys = ListResponse[APIKeyPolicyRecord]{}
	if err := json.Unmarshal(w.Body.Bytes(), &listedKeys); err != nil {
		t.Fatalf("decode soon-expiring managed api key list: %v", err)
	}
	if !containsAPIKeyPolicyStatus(listedKeys.Items, "soon-report-web", "expiring_soon") {
		t.Fatalf("soon-expiring managed key filter returned wrong keys: %#v", listedKeys)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/access/api-keys/finance-report-web/capabilities", nil)
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("preview managed api key capabilities status=%d body=%s", w.Code, w.Body.String())
	}
	var previewCaps DataCapabilities
	if err := json.NewDecoder(w.Body).Decode(&previewCaps); err != nil {
		t.Fatalf("decode managed api key capabilities: %v", err)
	}
	if previewCaps.APIKeyID != "finance-report-web" || !containsReport(previewCaps.Reports, "finance.expense_by_department") || containsBusinessAction(previewCaps.BusinessActions, "sales.order_upsert") {
		t.Fatalf("managed api key capabilities preview leaked wrong scope: %#v", previewCaps)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/access/check", bytes.NewBufferString(`{"key_id":"finance-report-web","resource_type":"report","resource_id":"finance.expense_by_department"}`))
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("check access allowed report status=%d body=%s", w.Code, w.Body.String())
	}
	var accessCheck AccessCheckResult
	if err := json.NewDecoder(w.Body).Decode(&accessCheck); err != nil {
		t.Fatalf("decode allowed access check: %v", err)
	}
	if !accessCheck.Allowed || accessCheck.APIKeyID != "finance-report-web" {
		t.Fatalf("expected allowed report access check: %#v", accessCheck)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/access/check", bytes.NewBufferString(`{"key_id":"finance-report-web","resource_type":"business_action","resource_id":"sales.order_upsert"}`))
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("check access denied action status=%d body=%s", w.Code, w.Body.String())
	}
	accessCheck = AccessCheckResult{}
	if err := json.NewDecoder(w.Body).Decode(&accessCheck); err != nil {
		t.Fatalf("decode denied access check: %v", err)
	}
	if accessCheck.Allowed {
		t.Fatalf("expected denied business action access check: %#v", accessCheck)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/access/review", nil)
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("review managed api key access status=%d body=%s", w.Code, w.Body.String())
	}
	var accessReview AccessReviewResult
	if err := json.NewDecoder(w.Body).Decode(&accessReview); err != nil {
		t.Fatalf("decode access review: %v", err)
	}
	if accessReview.Total == 0 || !containsAccessReviewCode(accessReview.Findings, "expired-report-web", "expired") || !containsAccessReviewCode(accessReview.Findings, "soon-report-web", "expiring_soon") {
		t.Fatalf("expected access review findings for expired and expiring keys: %#v", accessReview)
	}
	if accessReview.BySeverity["high"] == 0 || accessReview.BySeverity["medium"] == 0 || accessReview.Filtered != len(accessReview.Findings) {
		t.Fatalf("expected access review severity summary: %#v", accessReview)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/access/review?min_severity=high", nil)
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("review high managed api key access status=%d body=%s", w.Code, w.Body.String())
	}
	accessReview = AccessReviewResult{}
	if err := json.NewDecoder(w.Body).Decode(&accessReview); err != nil {
		t.Fatalf("decode high access review: %v", err)
	}
	if containsAccessReviewCode(accessReview.Findings, "soon-report-web", "expiring_soon") || !containsAccessReviewCode(accessReview.Findings, "expired-report-web", "expired") {
		t.Fatalf("expected high access review to filter medium findings: %#v", accessReview)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/access/remediation-plan?min_severity=high", nil)
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("plan high access remediation status=%d body=%s", w.Code, w.Body.String())
	}
	var remediation AccessRemediationPlan
	if err := json.NewDecoder(w.Body).Decode(&remediation); err != nil {
		t.Fatalf("decode high access remediation plan: %v", err)
	}
	if remediation.Total == 0 || !containsAccessRemediationAction(remediation.Items, "expired-report-web", "disable_key") || containsAccessRemediationAction(remediation.Items, "soon-report-web", "review_or_extend_expiration") {
		t.Fatalf("expected high remediation plan to include expired disable and filter soon-expiring item: %#v", remediation)
	}
	rawRestriction := findAccessRemediationAction(remediation.Items, "finance-report-web", "restrict_to_business_capabilities")
	if rawRestriction != nil {
		t.Fatalf("report-only key should not get raw data restriction remediation: %#v", rawRestriction)
	}
	rawRestriction = findAccessRemediationAction(remediation.Items, "raw-finance-web", "restrict_to_business_capabilities")
	if rawRestriction == nil {
		t.Fatalf("expected raw finance key remediation: %#v", remediation)
	}
	if rawRestriction.Payload["allow_raw_data"] != false {
		t.Fatalf("expected raw remediation payload to disable raw access: %#v", rawRestriction.Payload)
	}
	if _, ok := rawRestriction.Payload["allowed_reports"]; !ok {
		t.Fatalf("expected raw remediation payload to preserve allowed_reports: %#v", rawRestriction.Payload)
	}
	if _, ok := rawRestriction.Payload["role"]; !ok {
		t.Fatalf("expected raw remediation payload to preserve role: %#v", rawRestriction.Payload)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/reports/finance.expense_by_department/run", bytes.NewBufferString(`{}`))
	authBearer(req, createdKey.Key, "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusNotFound {
		t.Fatalf("managed report key should authenticate and reach report authorization, status=%d body=%s", w.Code, w.Body.String())
	}
	body = bytes.NewBufferString(`{"user_id":"agent_finance_report","role":"data_user","allowed_reports":["finance.invoice_status"],"note":"updated finance reporting only"}`)
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/data/access/api-keys/finance-report-web", body)
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update managed api key status=%d body=%s", w.Code, w.Body.String())
	}
	var updatedKey APIKeyPolicyRecord
	if err := json.NewDecoder(w.Body).Decode(&updatedKey); err != nil {
		t.Fatalf("decode updated managed api key: %v", err)
	}
	if updatedKey.UserID != "agent_finance_report" || updatedKey.Note != "updated finance reporting only" || !containsString(updatedKey.AllowedReports, "finance.invoice_status") {
		t.Fatalf("managed api key patch did not apply expected fields: %#v", updatedKey)
	}
	body = bytes.NewBufferString(`{"user_id":"","note":"","allowed_reports":[],"allow_raw_data":false,"expires_at":""}`)
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/data/access/api-keys/raw-finance-web", body)
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("clear managed api key optional fields status=%d body=%s", w.Code, w.Body.String())
	}
	var clearedKey APIKeyPolicyRecord
	if err := json.NewDecoder(w.Body).Decode(&clearedKey); err != nil {
		t.Fatalf("decode cleared managed api key: %v", err)
	}
	if clearedKey.UserID != "" || clearedKey.Note != "" || len(clearedKey.AllowedReports) != 0 || clearedKey.AllowRawData || clearedKey.ExpiresAt != nil {
		t.Fatalf("managed api key explicit empty patch should clear optional fields and false booleans: %#v", clearedKey)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/reports/finance.expense_by_department/run", bytes.NewBufferString(`{}`))
	authBearer(req, createdKey.Key, "")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("updated managed key should lose old report scope, status=%d body=%s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/data/access/api-keys/finance-report-web/rotate", bytes.NewBufferString(`{}`))
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("rotate managed api key status=%d body=%s", w.Code, w.Body.String())
	}
	var rotatedKey CreateAPIKeyPolicyResult
	if err := json.NewDecoder(w.Body).Decode(&rotatedKey); err != nil {
		t.Fatalf("decode rotated key: %v", err)
	}
	if rotatedKey.Key == "" || rotatedKey.Key == createdKey.Key {
		t.Fatalf("expected new rotated key secret")
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/capabilities", nil)
	authBearer(req, createdKey.Key, "")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("old rotated key should be unauthorized, status=%d body=%s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/capabilities", nil)
	authBearer(req, rotatedKey.Key, "")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("new rotated key should authenticate, status=%d body=%s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/access/api-keys/finance-report-web", nil)
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get managed api key after use status=%d body=%s", w.Code, w.Body.String())
	}
	var usedKey APIKeyPolicyRecord
	if err := json.NewDecoder(w.Body).Decode(&usedKey); err != nil {
		t.Fatalf("decode used key: %v", err)
	}
	if usedKey.LastUsedAt == nil || usedKey.LastUsedIP == "" {
		t.Fatalf("expected managed key last-used metadata, got %#v", usedKey)
	}
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/data/access/api-keys/finance-report-web", nil)
	authBearer(req, "root-token-0123456789012345", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("disable managed api key status=%d body=%s", w.Code, w.Body.String())
	}
	var disabledKey APIKeyPolicyRecord
	if err := json.NewDecoder(w.Body).Decode(&disabledKey); err != nil {
		t.Fatalf("decode disabled managed api key: %v", err)
	}
	if disabledKey.Status != "disabled" {
		t.Fatalf("expected disabled managed key status, got %#v", disabledKey)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/capabilities", nil)
	authBearer(req, rotatedKey.Key, "")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("disabled managed key should be unauthorized, status=%d body=%s", w.Code, w.Body.String())
	}

	if _, err := svc.CreateDatasetFromTemplate(context.Background(), p, "hr.payroll", CreateFromTemplateInput{}); err != nil {
		t.Fatalf("CreateDatasetFromTemplate payroll: %v", err)
	}
	if _, err := svc.CreateRecord(context.Background(), p, "hr.payroll", CreateRecordInput{ID: "PAY-KEY-1", Data: map[string]any{"payroll_month": "2026-05", "employee_no": "E001", "employee_name": "Alice", "gross_pay": 20000, "tax": 3000, "net_pay": 17000, "status": "draft"}}); err != nil {
		t.Fatalf("CreateRecord payroll: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/datasets/hr.payroll/records/PAY-KEY-1", nil)
	authBearer(req, "hr-auditor-key-012345678901", "data_admin")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("hr scoped key should read payroll, status=%d body=%s", w.Code, w.Body.String())
	}
	var payroll Record
	if err := json.NewDecoder(w.Body).Decode(&payroll); err != nil {
		t.Fatalf("decode payroll: %v", err)
	}
	if payroll.Data["gross_pay"] != maskedValue || payroll.Data["net_pay"] != maskedValue {
		t.Fatalf("API key without allow_sensitive must mask payroll fields: %#v", payroll.Data)
	}
}

func authBearer(req *http.Request, token string, role string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-MaClaw-Tenant-ID", "tenant_1")
	req.Header.Set("X-MaClaw-User-ID", "user_1")
	if role != "" {
		req.Header.Set("X-MaClaw-Role", role)
	}
}

func TestEffectiveLimitNormalizesPaginationBounds(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
	}{
		{raw: "", want: 0},
		{raw: "25", want: 25},
		{raw: " 25 ", want: 25},
		{raw: "10abc", want: 0},
		{raw: "abc10", want: 0},
	} {
		if got := parseLimit(tc.raw); got != tc.want {
			t.Fatalf("parseLimit(%q)=%d, want %d", tc.raw, got, tc.want)
		}
	}
	for _, tc := range []struct {
		name     string
		limit    int
		fallback int
		max      int
		want     int
	}{
		{name: "valid", limit: 25, fallback: 100, max: 500, want: 25},
		{name: "zero", limit: 0, fallback: 100, max: 500, want: 100},
		{name: "negative", limit: -1, fallback: 100, max: 500, want: 100},
		{name: "above max", limit: 501, fallback: 100, max: 500, want: 100},
		{name: "custom fallback", limit: 0, fallback: 200, max: 500, want: 200},
		{name: "default fallback", limit: 0, fallback: 0, max: 500, want: 100},
		{name: "default max", limit: 501, fallback: 100, max: 0, want: 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveLimit(tc.limit, tc.fallback, tc.max); got != tc.want {
				t.Fatalf("effectiveLimit(%d, %d, %d)=%d, want %d", tc.limit, tc.fallback, tc.max, got, tc.want)
			}
		})
	}
}

func openAPIGetHasInboxSchema(paths map[string]any, path string) bool {
	properties := openAPIGetResponseProperties(paths, path)
	if len(properties) == 0 {
		return false
	}
	_, hasItems := properties["items"]
	_, hasLimit := properties["limit"]
	_, hasMore := properties["has_more"]
	_, hasGeneratedAt := properties["generated_at"]
	_, hasNextBefore := properties["next_before"]
	_, hasNextBeforeID := properties["next_before_id"]
	return hasItems && hasLimit && hasMore && hasGeneratedAt && hasNextBefore && hasNextBeforeID
}

func openAPIGetHasTimelineSchema(paths map[string]any, path string) bool {
	properties := openAPIGetResponseProperties(paths, path)
	if len(properties) == 0 {
		return false
	}
	_, hasDatasetID := properties["dataset_id"]
	_, hasRecordID := properties["record_id"]
	_, hasItems := properties["items"]
	_, hasLimit := properties["limit"]
	_, hasMore := properties["has_more"]
	_, hasNextBefore := properties["next_before"]
	_, hasNextBeforeID := properties["next_before_id"]
	return hasDatasetID && hasRecordID && hasItems && hasLimit && hasMore && hasNextBefore && hasNextBeforeID
}

func openAPIGetHasRelatedRecordsSchema(paths map[string]any, path string) bool {
	properties := openAPIGetResponseProperties(paths, path)
	if len(properties) == 0 {
		return false
	}
	_, hasLinks := properties["links"]
	_, hasLimit := properties["limit"]
	_, hasMore := properties["has_more"]
	_, hasNextBeforeID := properties["next_before_id"]
	return hasLinks && hasLimit && hasMore && hasNextBeforeID
}

func openAPIGetHasListResponseSchema(paths map[string]any, path string) bool {
	properties := openAPIGetResponseProperties(paths, path)
	if len(properties) == 0 {
		return false
	}
	_, hasItems := properties["items"]
	_, hasLimit := properties["limit"]
	_, hasMore := properties["has_more"]
	_, hasNextBefore := properties["next_before"]
	_, hasNextBeforeID := properties["next_before_id"]
	return hasItems && hasLimit && hasMore && hasNextBefore && hasNextBeforeID
}

func openAPIPostHasListResponseSchema(paths map[string]any, path string) bool {
	properties := openAPIOperationResponseProperties(paths, path, "post")
	if len(properties) == 0 {
		return false
	}
	_, hasItems := properties["items"]
	_, hasLimit := properties["limit"]
	_, hasMore := properties["has_more"]
	_, hasNextBefore := properties["next_before"]
	_, hasNextBeforeID := properties["next_before_id"]
	return hasItems && hasLimit && hasMore && hasNextBefore && hasNextBeforeID
}

func openAPIPostHasBusinessViewResponseSchema(paths map[string]any, path string) bool {
	properties := openAPIOperationResponseProperties(paths, path, "post")
	if len(properties) == 0 {
		return false
	}
	_, hasView := properties["view"]
	_, hasRecords := properties["records"]
	_, hasLimit := properties["limit"]
	_, hasMore := properties["has_more"]
	_, hasNextBefore := properties["next_before"]
	_, hasNextBeforeID := properties["next_before_id"]
	return hasView && hasRecords && hasLimit && hasMore && hasNextBefore && hasNextBeforeID
}

func openAPIOperationResponseHasProperty(paths map[string]any, path string, method string, status string, name string) bool {
	properties := openAPIOperationStatusResponseProperties(paths, path, method, status)
	_, ok := properties[name]
	return ok
}

func openAPIGetResponseProperties(paths map[string]any, path string) map[string]any {
	return openAPIOperationResponseProperties(paths, path, "get")
}

func openAPIOperationResponseProperties(paths map[string]any, path string, method string) map[string]any {
	return openAPIOperationStatusResponseProperties(paths, path, method, "200")
}

func openAPIOperationStatusResponseProperties(paths map[string]any, path string, method string, status string) map[string]any {
	pathItem, ok := paths[path].(map[string]any)
	if !ok {
		return nil
	}
	operation, ok := pathItem[method].(map[string]any)
	if !ok {
		return nil
	}
	responses, ok := operation["responses"].(map[string]any)
	if !ok {
		return nil
	}
	okResp, ok := responses[status].(map[string]any)
	if !ok {
		return nil
	}
	content, ok := okResp["content"].(map[string]any)
	if !ok {
		return nil
	}
	jsonContent, ok := content["application/json"].(map[string]any)
	if !ok {
		return nil
	}
	schema, ok := jsonContent["schema"].(map[string]any)
	if !ok || schema["type"] != "object" {
		return nil
	}
	properties, _ := schema["properties"].(map[string]any)
	return properties
}

func openAPIPostRequestBodyHasProperty(paths map[string]any, path string, name string) bool {
	return openAPIRequestBodyHasProperty(paths, path, "post", name)
}

func openAPIPatchRequestBodyHasProperty(paths map[string]any, path string, name string) bool {
	return openAPIRequestBodyHasProperty(paths, path, "patch", name)
}

func connectorUpsertMethod(path string) string {
	if strings.Contains(path, "{connectorId}") {
		return "put"
	}
	return "post"
}

func assertOpenAPIMutatingRequestBodies(t *testing.T, paths map[string]any) {
	t.Helper()
	noBody := map[string]bool{
		"post /api/v1/data/access/api-keys/{keyId}/rotate":           true,
		"post /api/v1/data/events/dead-letter/{deadLetterId}/retry":  true,
		"post /api/v1/data/connectors/{connectorId}/test":            true,
		"post /api/v1/data/connectors/{connectorId}/config/validate": true,
		"post /api/v1/data/dashboards/{dashboardId}/run":             true,
		"post /api/v1/data/operation-plans/{planId}/cancel":          true,
	}
	for path, rawPathItem := range paths {
		pathItem, ok := rawPathItem.(map[string]any)
		if !ok {
			continue
		}
		for _, method := range []string{"post", "put", "patch"} {
			rawOperation, ok := pathItem[method]
			if !ok {
				continue
			}
			if noBody[method+" "+path] {
				continue
			}
			operation, ok := rawOperation.(map[string]any)
			if !ok {
				t.Fatalf("openapi operation %s %s has invalid shape: %#v", method, path, rawOperation)
			}
			if _, ok := operation["requestBody"].(map[string]any); !ok {
				t.Fatalf("openapi operation %s %s missing requestBody", method, path)
			}
		}
	}
}

func assertOpenAPIDataOperationsHaveAuthErrors(t *testing.T, paths map[string]any) {
	t.Helper()
	checked := 0
	for path, rawPathItem := range paths {
		if !strings.HasPrefix(path, "/api/v1/data/") {
			continue
		}
		pathItem, ok := rawPathItem.(map[string]any)
		if !ok {
			t.Fatalf("openapi path %s has invalid shape: %#v", path, rawPathItem)
		}
		for _, method := range []string{"get", "post", "put", "patch", "delete"} {
			rawOperation, ok := pathItem[method]
			if !ok {
				continue
			}
			operation, ok := rawOperation.(map[string]any)
			if !ok {
				t.Fatalf("openapi operation %s %s has invalid shape: %#v", method, path, rawOperation)
			}
			responses, ok := operation["responses"].(map[string]any)
			if !ok {
				t.Fatalf("openapi operation %s %s missing responses", method, path)
			}
			for _, status := range []string{"401", "403"} {
				if _, ok := responses[status]; !ok {
					t.Fatalf("openapi operation %s %s missing %s auth error response: %#v", method, path, status, responses)
				}
			}
			unauthorized, ok := responses["401"].(map[string]any)
			if !ok {
				t.Fatalf("openapi operation %s %s has invalid 401 response: %#v", method, path, responses["401"])
			}
			headers, ok := unauthorized["headers"].(map[string]any)
			if !ok {
				t.Fatalf("openapi operation %s %s missing 401 headers: %#v", method, path, unauthorized)
			}
			if _, ok := headers["WWW-Authenticate"]; !ok {
				t.Fatalf("openapi operation %s %s missing WWW-Authenticate header schema: %#v", method, path, headers)
			}
			if _, ok := headers["X-Content-Type-Options"]; !ok {
				t.Fatalf("openapi operation %s %s missing X-Content-Type-Options header schema: %#v", method, path, headers)
			}
			forbidden, ok := responses["403"].(map[string]any)
			if !ok {
				t.Fatalf("openapi operation %s %s has invalid 403 response: %#v", method, path, responses["403"])
			}
			forbiddenHeaders, ok := forbidden["headers"].(map[string]any)
			if !ok {
				t.Fatalf("openapi operation %s %s missing 403 headers: %#v", method, path, forbidden)
			}
			if _, ok := forbiddenHeaders["X-Content-Type-Options"]; !ok {
				t.Fatalf("openapi operation %s %s missing 403 X-Content-Type-Options header schema: %#v", method, path, forbiddenHeaders)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("openapi auth error scan found no data operations")
	}
}

func assertOpenAPIDownloadOperationsHaveHeaders(t *testing.T, paths map[string]any) {
	t.Helper()
	for route, metadata := range downloadOpenAPIMetadataByRoute() {
		method, path, ok := strings.Cut(route, " ")
		if !ok {
			t.Fatalf("invalid download OpenAPI route key %q", route)
		}
		pathItem, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("openapi download path %s missing or invalid: %#v", path, paths[path])
		}
		operation, ok := pathItem[method].(map[string]any)
		if !ok {
			t.Fatalf("openapi download operation %s %s missing or invalid: %#v", method, path, pathItem)
		}
		responses, ok := operation["responses"].(map[string]any)
		if !ok {
			t.Fatalf("openapi download operation %s %s missing responses: %#v", method, path, operation)
		}
		okResponse, ok := responses["200"].(map[string]any)
		if !ok {
			t.Fatalf("openapi download operation %s %s missing 200 response: %#v", method, path, responses)
		}
		headers, ok := okResponse["headers"].(map[string]any)
		if !ok {
			t.Fatalf("openapi download operation %s %s missing 200 headers: %#v", method, path, okResponse)
		}
		for _, name := range []string{"Content-Disposition", "X-Content-Type-Options"} {
			if _, ok := headers[name]; !ok {
				t.Fatalf("openapi download operation %s %s missing %s header schema: %#v", method, path, name, headers)
			}
		}
		if metadata.BackupChecksum {
			if _, ok := headers["X-MaClaw-Backup-SHA256"]; !ok {
				t.Fatalf("openapi backup download missing checksum header schema: %#v", headers)
			}
		}
		content, ok := okResponse["content"].(map[string]any)
		if !ok {
			t.Fatalf("openapi download operation %s %s missing 200 content: %#v", method, path, okResponse)
		}
		for _, contentType := range metadata.ContentTypes {
			if _, ok := content[contentType]; !ok {
				t.Fatalf("openapi download operation %s %s missing %s content schema: %#v", method, path, contentType, content)
			}
		}
	}
}

func openAPIRequestBodyHasProperty(paths map[string]any, path string, method string, name string) bool {
	return openAPIRequestBodyProperty(paths, path, method, name) != nil
}

func openAPIRequestBodyProperty(paths map[string]any, path string, method string, name string) map[string]any {
	pathItem, ok := paths[path].(map[string]any)
	if !ok {
		return nil
	}
	operation, ok := pathItem[method].(map[string]any)
	if !ok {
		return nil
	}
	requestBody, ok := operation["requestBody"].(map[string]any)
	if !ok {
		return nil
	}
	content, ok := requestBody["content"].(map[string]any)
	if !ok {
		return nil
	}
	jsonContent, ok := content["application/json"].(map[string]any)
	if !ok {
		return nil
	}
	schema, ok := jsonContent["schema"].(map[string]any)
	if !ok || schema["type"] != "object" {
		return nil
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil
	}
	prop, ok := properties[name].(map[string]any)
	if !ok {
		return nil
	}
	return prop
}

func openAPINumericValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func oversizedBatchImportJSON() string {
	var b strings.Builder
	b.WriteString(`{"records":[`)
	for i := 0; i <= maxBatchImportRecords; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"id":"OVER-%04d","data":{"order_no":"OVER-%04d","customer":"OverCo","amount":1}}`, i, i)
	}
	b.WriteString(`]}`)
	return b.String()
}

func oversizedCSVImportJSON() string {
	var b strings.Builder
	b.WriteString(`{"csv":"id,order_no,customer,amount\n`)
	for i := 0; i <= maxBatchImportRecords; i++ {
		fmt.Fprintf(&b, "CSV-OVER-%04d,CSV-OVER-%04d,OverCo,1\\n", i, i)
	}
	b.WriteString(`"}`)
	return b.String()
}

func oversizedJSONLImportJSON() string {
	var b strings.Builder
	b.WriteString(`{"jsonl":"`)
	for i := 0; i <= maxBatchImportRecords; i++ {
		fmt.Fprintf(&b, `{\"id\":\"JSONL-OVER-%04d\",\"data\":{\"order_no\":\"JSONL-OVER-%04d\",\"customer\":\"OverCo\",\"amount\":1}}\n`, i, i)
	}
	b.WriteString(`"}`)
	return b.String()
}

func openAPIGetHasQueryParam(paths map[string]any, path string, name string) bool {
	return openAPIGetQueryParam(paths, path, name) != nil
}

func openAPIGetQueryParamHasType(paths map[string]any, path string, name string, typ string) bool {
	param := openAPIGetQueryParam(paths, path, name)
	if param == nil {
		return false
	}
	schema, ok := param["schema"].(map[string]any)
	if !ok {
		return false
	}
	return schema["type"] == typ
}

func openAPIGetQueryParam(paths map[string]any, path string, name string) map[string]any {
	return openAPIOperationQueryParam(paths, path, "get", name)
}

func openAPIOperationHasQueryParam(paths map[string]any, path string, method string, name string) bool {
	return openAPIOperationQueryParam(paths, path, method, name) != nil
}

func openAPIOperationQueryParam(paths map[string]any, path string, method string, name string) map[string]any {
	pathItem, ok := paths[path].(map[string]any)
	if !ok {
		return nil
	}
	operation, ok := pathItem[strings.ToLower(method)].(map[string]any)
	if !ok {
		return nil
	}
	params, ok := operation["parameters"].([]any)
	if !ok {
		return nil
	}
	for _, raw := range params {
		param, ok := raw.(map[string]any)
		if ok && param["name"] == name && param["in"] == "query" {
			return param
		}
	}
	return nil
}

func containsIntentStep(items []BusinessIntentNextStep, action string, dryRun bool) bool {
	for _, item := range items {
		if item.Action == action && item.DryRun == dryRun {
			return true
		}
	}
	return false
}

func containsIntentStepRequiredField(items []BusinessIntentNextStep, action string, field string) bool {
	for _, item := range items {
		if item.Action != action {
			continue
		}
		if containsString(item.RequiredFields, field) {
			return true
		}
	}
	return false
}

func containsIntentStepDataTemplateKey(items []BusinessIntentNextStep, action string, key string) bool {
	for _, item := range items {
		if item.Action != action || item.DataTemplate == nil {
			continue
		}
		if _, ok := item.DataTemplate[key]; ok {
			return true
		}
	}
	return false
}

func containsIntentStepBodyTemplate(items []BusinessIntentNextStep, action string, key string, value any) bool {
	for _, item := range items {
		if item.Action != action || item.BodyTemplate == nil {
			continue
		}
		if item.BodyTemplate[key] == value {
			return true
		}
	}
	return false
}

func containsNextStepParam(items []BusinessIntentNextStep, action string, key string) bool {
	for _, item := range items {
		if item.Action != action || item.Params == nil {
			continue
		}
		if _, ok := item.Params[key]; ok {
			return true
		}
	}
	return false
}

func containsNextStepParamValue(items []BusinessIntentNextStep, action string, key string, value any) bool {
	for _, item := range items {
		if item.Action != action || item.Params == nil {
			continue
		}
		if fmt.Sprint(item.Params[key]) == fmt.Sprint(value) {
			return true
		}
	}
	return false
}

func containsGateStatus(items []BusinessRuleGateStatus, gate string, status string) bool {
	for _, item := range items {
		if item.Gate == gate && item.Status == status {
			return true
		}
	}
	return false
}

func containsIntentStepToolCallTemplate(items []BusinessIntentNextStep, action string, tool string, businessActionID string) bool {
	for _, item := range items {
		if item.Action != action || item.ToolCallTemplate == nil {
			continue
		}
		if item.ToolCallTemplate["tool"] != tool {
			continue
		}
		args, ok := item.ToolCallTemplate["args"].(map[string]any)
		if !ok {
			continue
		}
		if args["business_action_id"] == businessActionID {
			return true
		}
	}
	return false
}

func containsDomainCatalog(items []BusinessDomainCatalog, domain string) bool {
	for _, item := range items {
		if item.Domain == domain {
			return true
		}
	}
	return false
}

func containsDomainUseCase(items []BusinessDomainUseCase, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func containsAPIKeyPolicyStatus(items []APIKeyPolicyRecord, id, status string) bool {
	for _, item := range items {
		if item.ID == id && item.Status == status {
			return true
		}
	}
	return false
}

func containsAccessPolicyPreset(items []AccessPolicyPreset, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func containsAccessReviewCode(items []AccessReviewItem, keyID, code string) bool {
	for _, item := range items {
		if item.KeyID == keyID && containsString(item.Codes, code) {
			return true
		}
	}
	return false
}

func containsAccessRemediationAction(items []AccessRemediationItem, keyID, action string) bool {
	return findAccessRemediationAction(items, keyID, action) != nil
}

func findAccessRemediationAction(items []AccessRemediationItem, keyID, action string) *AccessRemediationItem {
	for i := range items {
		if items[i].KeyID == keyID && items[i].Action == action {
			return &items[i]
		}
	}
	return nil
}

func containsTemplate(items []DatasetTemplate, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func containsEvidenceSection(items []GovernanceEvidenceSection, name string) bool {
	for _, item := range items {
		if item.Name == name && item.OK {
			return true
		}
	}
	return false
}

func containsGovernanceControl(items []GovernanceControl, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func containsGovernanceControlAction(items []GovernanceControl, id string, target string) bool {
	for _, item := range items {
		if item.ID == id && item.ActionTarget == target && item.RecommendedAction != "" {
			return true
		}
	}
	return false
}

func containsBusinessAction(items []BusinessAction, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func containsToolAction(items []ToolActionCapability, action string) bool {
	for _, item := range items {
		if item.Action == action {
			return true
		}
	}
	return false
}

func containsMatchedRule(items []BusinessRuleDefinition, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func containsRelationship(items []DatasetRelationship, sourceDatasetID string, sourceField string, targetDatasetID string) bool {
	for _, item := range items {
		if item.SourceDatasetID == sourceDatasetID && item.SourceField == sourceField && item.TargetDatasetID == targetDatasetID {
			return true
		}
	}
	return false
}

func containsQualityCheck(items []QualityCheckDefinition, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func containsQualityIssue(items []QualityIssue, check string, field string) bool {
	for _, item := range items {
		if item.Check == check && item.Field == field {
			return true
		}
	}
	return false
}

func containsDeadLetterID(items []DataEventDeadLetter, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func containsRelatedRecord(items []RelatedRecordLink, direction string, datasetID string, recordID string) bool {
	for _, item := range items {
		if item.Direction == direction && item.Record != nil && item.Record.DatasetID == datasetID && item.Record.ID == recordID {
			return true
		}
	}
	return false
}

func containsAgentPlaybookToolCall(items []AgentBusinessPlaybook, id string, tool string, businessActionID string) bool {
	for _, item := range items {
		if item.ID != id {
			continue
		}
		for _, step := range item.Steps {
			if step.Action != "execute_business_action" || step.ToolCallTemplate == nil {
				continue
			}
			if step.ToolCallTemplate["tool"] != tool {
				continue
			}
			args, ok := step.ToolCallTemplate["args"].(map[string]any)
			if !ok {
				continue
			}
			if args["business_action_id"] == businessActionID {
				return true
			}
		}
	}
	return false
}

func containsBusinessView(items []BusinessViewDefinition, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func containsReport(items []ReportDefinition, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func containsFieldDefinition(items []FieldDefinition, key string) bool {
	for _, item := range items {
		if item.Key == key {
			return true
		}
	}
	return false
}

func containsDashboard(items []DashboardDefinition, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func TestWebConsoleIncludesCursorLoadMoreTargets(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	server := NewHTTPServer(NewService(store, "sqlite"), "test-token-0123456789012345", "test")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("web console status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		`data-testid="connector-sync-runs"`,
		`function preparePageParams(stateKey, params, loadMore)`,
		`state.connectorSyncRunNextBefore`,
		`listConnectorSyncRuns(loadMore = false)`,
		`appendLoadMoreButton(root, state.connectorSyncRunHasMore`,
		`state.accessKeyNextBefore`,
		`loadManagedAccessKeys(showStatus = true, loadMore = false)`,
		`appendLoadMoreButton(root, state.accessKeyHasMore`,
		`state.businessViewNextBefore`,
		`queryBusinessView(loadMore = false)`,
		`state.businessViewRecords = loadMore ?`,
		`appendLoadMoreButton(root, state.businessViewHasMore`,
		`state.recordNextBefore`,
		`queryRecords(loadMore = false)`,
		`state.records = loadMore ?`,
		`appendLoadMoreButton(root, state.recordHasMore`,
		`loadBusinessActions(loadMore = false)`,
		`appendLoadMoreButton(root, state.businessActionHasMore`,
		`data-testid="event-contract-table"`,
		`loadEventContracts(loadMore = false)`,
		`state.eventContracts = loadMore ?`,
		`appendLoadMoreButton(root, state.eventContractHasMore`,
		`loadBusinessRules(loadMore = false)`,
		`appendLoadMoreButton(root, state.businessRuleHasMore`,
		`loadBusinessViews(loadMore = false)`,
		`appendLoadMoreButton(root, state.businessListHasMore`,
		`loadDashboards(loadMore = false)`,
		`appendLoadMoreButton(root, state.dashboardHasMore`,
		`loadReports(loadMore = false)`,
		`appendLoadMoreButton(root, state.reportHasMore`,
		`loadQualityChecks(loadMore = false)`,
		`appendLoadMoreButton(root, state.qualityCheckHasMore`,
		`loadDomains(loadMore = false)`,
		`appendLoadMoreButton(root, state.domainHasMore`,
		`loadRelationships(loadMore = false)`,
		`appendLoadMoreButton(root, state.relationshipHasMore`,
		`loadDatasets(loadMore = false)`,
		`appendLoadMoreButton(root, state.datasetHasMore`,
		`loadConnectors(loadMore = false)`,
		`appendLoadMoreButton(root, state.connectorHasMore`,
		`loadAllConnectorHealth()`,
		`/api/v1/data/connectors/health?`,
		`last.has_more && beforeID && page < 20`,
		`data-testid="load-more-templates"`,
		`loadTemplates(loadMore = false)`,
		`state.templates = loadMore ?`,
		`state.templateNextBeforeID`,
		`data-testid="load-more-access-presets"`,
		`loadAccessCatalog(loadMorePresets = false)`,
		`state.accessPresets = loadMorePresets ?`,
		`state.accessPresetNextBeforeID`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("web console missing %q", want)
		}
	}
}

func auth(req *http.Request) {
	authRole(req, "data_admin")
}

func authRole(req *http.Request, role string) {
	req.Header.Set("Authorization", "Bearer test-token-0123456789012345")
	req.Header.Set("X-MaClaw-Tenant-ID", "tenant_1")
	req.Header.Set("X-MaClaw-User-ID", "user_1")
	if role != "" {
		req.Header.Set("X-MaClaw-Role", role)
	}
}
