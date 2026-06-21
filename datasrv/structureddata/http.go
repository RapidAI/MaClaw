package structureddata

import (
	"crypto/subtle"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxBodyBytes int64 = 4 << 20

type HTTPServer struct {
	svc         *Service
	token       string
	apiKeys     []APIKeyPolicy
	mux         *http.ServeMux
	version     string
	rateLimiter *httpRateLimiter
}

func NewHTTPServer(svc *Service, token, version string) *HTTPServer {
	s := &HTTPServer{svc: svc, token: strings.TrimSpace(token), version: strings.TrimSpace(version), mux: http.NewServeMux(), rateLimiter: newHTTPRateLimiter()}
	s.routes()
	return s
}

func NewHTTPServerWithAPIKeys(svc *Service, token, version string, apiKeys []APIKeyPolicy) *HTTPServer {
	s := &HTTPServer{svc: svc, token: strings.TrimSpace(token), version: strings.TrimSpace(version), apiKeys: append([]APIKeyPolicy(nil), apiKeys...), mux: http.NewServeMux(), rateLimiter: newHTTPRateLimiter()}
	s.routes()
	return s
}

func (s *HTTPServer) Handler() http.Handler { return s.mux }

func (s *HTTPServer) routes() {
	s.mux.HandleFunc("GET /", s.handleWebConsole)
	s.mux.HandleFunc("GET /ui", s.handleWebConsole)
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /readyz", s.handleReady)
	s.mux.HandleFunc("GET /version", s.handleVersion)
	s.mux.HandleFunc("GET /api/v1/openapi.json", s.handleOpenAPI)
	s.mux.HandleFunc("GET /api/v1/setup/status", s.handleSetupStatus)
	s.mux.HandleFunc("POST /api/v1/setup/tenants/sync", s.withRateLimit(s.handleSetupSyncTenants))
	s.mux.HandleFunc("POST /api/v1/setup/admin", s.withRateLimit(s.handleInitializeAdmin))
	s.mux.HandleFunc("POST /api/v1/login", s.withRateLimit(s.handleLogin))
	s.mux.HandleFunc("GET /api/v1/data/capabilities", s.withAuth(s.handleCapabilities))
	s.mux.HandleFunc("GET /api/v1/data/admin/accounts", s.withAuth(s.handleListAdminAccounts))
	s.mux.HandleFunc("POST /api/v1/data/admin/accounts", s.withAuth(s.handleCreateAdminAccount))
	s.mux.HandleFunc("PATCH /api/v1/data/admin/accounts/{username}", s.withAuth(s.handleUpdateAdminAccount))
	s.mux.HandleFunc("GET /api/v1/data/admin/sessions", s.withAuth(s.handleListAdminSessions))
	s.mux.HandleFunc("PATCH /api/v1/data/admin/sessions/{sessionId}", s.withAuth(s.handleUpdateAdminSession))
	s.mux.HandleFunc("DELETE /api/v1/data/admin/sessions/{sessionId}", s.withAuth(s.handleRevokeAdminSession))
	s.mux.HandleFunc("GET /api/v1/data/admin/tenants", s.withAuth(s.handleListDataTenants))
	s.mux.HandleFunc("POST /api/v1/data/admin/tenants/sync", s.withAuth(s.handleSyncHubTenants))
	s.mux.HandleFunc("GET /api/v1/data/admin/hub-registration", s.withAuth(s.handleGetHubRegistration))
	s.mux.HandleFunc("POST /api/v1/data/admin/hub-registration", s.withAuth(s.handleSaveHubRegistration))
	s.mux.HandleFunc("POST /api/v1/data/admin/hub-registration/register", s.withAuth(s.handleRegisterHub))
	s.mux.HandleFunc("POST /api/v1/data/admin/hub-registration/sync-tenants", s.withAuth(s.handleSyncTenantsFromHub))
	s.mux.HandleFunc("GET /api/v1/data/access/presets", s.withAuth(s.handleListAccessPolicyPresets))
	s.mux.HandleFunc("GET /api/v1/data/access/review", s.withAuth(s.handleReviewAccess))
	s.mux.HandleFunc("GET /api/v1/data/access/remediation-plan", s.withAuth(s.handlePlanAccessRemediation))
	s.mux.HandleFunc("POST /api/v1/data/access/check", s.withAuth(s.handleCheckAccess))
	s.mux.HandleFunc("GET /api/v1/data/access/api-keys", s.withAuth(s.handleListAPIKeyPolicies))
	s.mux.HandleFunc("POST /api/v1/data/access/api-keys", s.withAuth(s.handleCreateAPIKeyPolicy))
	s.mux.HandleFunc("GET /api/v1/data/access/api-keys/{keyId}", s.withAuth(s.handleGetAPIKeyPolicy))
	s.mux.HandleFunc("PATCH /api/v1/data/access/api-keys/{keyId}", s.withAuth(s.handleUpdateAPIKeyPolicy))
	s.mux.HandleFunc("GET /api/v1/data/access/api-keys/{keyId}/capabilities", s.withAuth(s.handleGetAPIKeyPolicyCapabilities))
	s.mux.HandleFunc("POST /api/v1/data/access/api-keys/{keyId}/rotate", s.withAuth(s.handleRotateAPIKeyPolicy))
	s.mux.HandleFunc("DELETE /api/v1/data/access/api-keys/{keyId}", s.withAuth(s.handleDisableAPIKeyPolicy))
	s.mux.HandleFunc("GET /api/v1/data/domains", s.withAuth(s.handleListBusinessDomains))
	s.mux.HandleFunc("GET /api/v1/data/domains/{domain}", s.withAuth(s.handleGetBusinessDomain))
	s.mux.HandleFunc("GET /api/v1/data/business-objects", s.withAuth(s.handleListBusinessObjects))
	s.mux.HandleFunc("POST /api/v1/data/object-roles/resolve", s.withAuth(s.handleResolveObjectRole))
	s.mux.HandleFunc("GET /api/v1/data/app-installations", s.withAuth(s.handleListAppInstallations))
	s.mux.HandleFunc("POST /api/v1/data/app-installations", s.withAuth(s.handleCreateAppInstallation))
	s.mux.HandleFunc("GET /api/v1/data/app-installations/{appId}", s.withAuth(s.handleGetAppInstallation))
	s.mux.HandleFunc("PUT /api/v1/data/app-installations/{appId}", s.withAuth(s.handleUpdateAppInstallation))
	s.mux.HandleFunc("GET /api/v1/data/relationships", s.withAuth(s.handleListRelationships))
	s.mux.HandleFunc("POST /api/v1/data/intent/resolve", s.withAuth(s.handleResolveBusinessIntent))
	s.mux.HandleFunc("GET /api/v1/data/inbox", s.withAuth(s.handleMISInbox))
	s.mux.HandleFunc("GET /api/v1/data/inbox/summary", s.withAuth(s.handleMISInboxSummary))
	s.mux.HandleFunc("GET /api/v1/data/stats", s.withAuth(s.handleSystemStats))
	s.mux.HandleFunc("GET /api/v1/data/governance/evidence-pack", s.withAuth(s.handleGovernanceEvidencePack))
	s.mux.HandleFunc("GET /api/v1/data/governance/evidence-summary.txt", s.withAuth(s.handleGovernanceEvidenceSummaryText))
	s.mux.HandleFunc("POST /api/v1/data/maintenance/run", s.withAuth(s.handleRunMaintenance))
	s.mux.HandleFunc("GET /api/v1/data/templates", s.withAuth(s.handleListTemplates))
	s.mux.HandleFunc("POST /api/v1/data/templates/bootstrap", s.withAuth(s.handleBootstrapTemplates))
	s.mux.HandleFunc("GET /api/v1/data/templates/{templateId}", s.withAuth(s.handleGetTemplate))
	s.mux.HandleFunc("POST /api/v1/data/templates/{templateId}/create", s.withAuth(s.handleCreateDatasetFromTemplate))
	s.mux.HandleFunc("GET /api/v1/data/backups", s.withAuth(s.handleListBackups))
	s.mux.HandleFunc("POST /api/v1/data/backups", s.withAuth(s.handleCreateBackup))
	s.mux.HandleFunc("GET /api/v1/data/backups/{backupId}", s.withAuth(s.handleGetBackup))
	s.mux.HandleFunc("GET /api/v1/data/backups/{backupId}/download", s.withAuth(s.handleDownloadBackup))
	s.mux.HandleFunc("POST /api/v1/data/backups/{backupId}/restore", s.withAuth(s.handleRestoreBackup))
	s.mux.HandleFunc("GET /api/v1/data/events/dead-letter", s.withAuth(s.handleListEventDeadLetters))
	s.mux.HandleFunc("GET /api/v1/data/events/dead-letter/{deadLetterId}", s.withAuth(s.handleGetEventDeadLetter))
	s.mux.HandleFunc("POST /api/v1/data/events/dead-letter/{deadLetterId}/retry", s.withAuth(s.handleRetryEventDeadLetter))
	s.mux.HandleFunc("POST /api/v1/data/events/dead-letter/{deadLetterId}/resolve", s.withAuth(s.handleResolveEventDeadLetter))
	s.mux.HandleFunc("GET /api/v1/data/events", s.withAuth(s.handleListDataEvents))
	s.mux.HandleFunc("POST /api/v1/data/events", s.withAuth(s.handleIngestEvent))
	s.mux.HandleFunc("GET /api/v1/data/event-contracts", s.withAuth(s.handleListEventContracts))
	s.mux.HandleFunc("GET /api/v1/data/event-contracts/{actionId}", s.withAuth(s.handleGetEventContract))
	s.mux.HandleFunc("GET /api/v1/data/connectors", s.withAuth(s.handleListExternalConnectors))
	s.mux.HandleFunc("POST /api/v1/data/connectors", s.withAuth(s.handleCreateExternalConnector))
	s.mux.HandleFunc("GET /api/v1/data/connectors/health", s.withAuth(s.handleListConnectorHealth))
	s.mux.HandleFunc("GET /api/v1/data/connectors/{connectorId}", s.withAuth(s.handleGetExternalConnector))
	s.mux.HandleFunc("PUT /api/v1/data/connectors/{connectorId}", s.withAuth(s.handleUpdateExternalConnector))
	s.mux.HandleFunc("DELETE /api/v1/data/connectors/{connectorId}", s.withAuth(s.handleDeleteExternalConnector))
	s.mux.HandleFunc("POST /api/v1/data/connectors/{connectorId}/test", s.withAuth(s.handleTestExternalConnector))
	s.mux.HandleFunc("POST /api/v1/data/connectors/{connectorId}/config/validate", s.withAuth(s.handleValidateConnectorConfig))
	s.mux.HandleFunc("POST /api/v1/data/connectors/{connectorId}/readiness", s.withAuth(s.handleCheckConnectorReadiness))
	s.mux.HandleFunc("GET /api/v1/data/connectors/{connectorId}/health", s.withAuth(s.handleConnectorHealth))
	s.mux.HandleFunc("GET /api/v1/data/connectors/{connectorId}/sync-state", s.withAuth(s.handleGetConnectorSyncState))
	s.mux.HandleFunc("POST /api/v1/data/connectors/{connectorId}/sync-state", s.withAuth(s.handleUpdateConnectorSyncState))
	s.mux.HandleFunc("GET /api/v1/data/connectors/{connectorId}/sync-runs", s.withAuth(s.handleListConnectorSyncRuns))
	s.mux.HandleFunc("POST /api/v1/data/connectors/{connectorId}/sync-plan", s.withAuth(s.handlePlanConnectorSync))
	s.mux.HandleFunc("POST /api/v1/data/connectors/{connectorId}/sync-batch", s.withAuth(s.handleSyncConnectorBatch))
	s.mux.HandleFunc("POST /api/v1/data/connectors/{connectorId}/config/patch", s.withAuth(s.handlePatchConnectorConfig))
	s.mux.HandleFunc("POST /api/v1/data/connectors/{connectorId}/mappings/suggest", s.withAuth(s.handleSuggestConnectorMapping))
	s.mux.HandleFunc("POST /api/v1/data/connectors/{connectorId}/events/preview", s.withAuth(s.handlePreviewConnectorEvent))
	s.mux.HandleFunc("POST /api/v1/data/connectors/{connectorId}/events", s.withAuth(s.handleIngestConnectorEvent))
	s.mux.HandleFunc("GET /api/v1/data/business-actions", s.withAuth(s.handleListBusinessActions))
	s.mux.HandleFunc("GET /api/v1/data/business-actions/{actionId}", s.withAuth(s.handleGetBusinessAction))
	s.mux.HandleFunc("POST /api/v1/data/business-actions/{actionId}/execute", s.withAuth(s.handleExecuteBusinessAction))
	s.mux.HandleFunc("GET /api/v1/data/business-rules", s.withAuth(s.handleListBusinessRules))
	s.mux.HandleFunc("POST /api/v1/data/business-rules/evaluate", s.withAuth(s.handleEvaluateBusinessRules))
	s.mux.HandleFunc("GET /api/v1/data/views", s.withAuth(s.handleListBusinessViews))
	s.mux.HandleFunc("GET /api/v1/data/views/{viewId}", s.withAuth(s.handleGetBusinessView))
	s.mux.HandleFunc("POST /api/v1/data/views/{viewId}/query", s.withAuth(s.handleQueryBusinessView))
	s.mux.HandleFunc("GET /api/v1/data/dashboards", s.withAuth(s.handleListDashboards))
	s.mux.HandleFunc("GET /api/v1/data/dashboards/{dashboardId}", s.withAuth(s.handleGetDashboard))
	s.mux.HandleFunc("POST /api/v1/data/dashboards/{dashboardId}/run", s.withAuth(s.handleRunDashboard))
	s.mux.HandleFunc("GET /api/v1/data/reports", s.withAuth(s.handleListReports))
	s.mux.HandleFunc("GET /api/v1/data/reports/{reportId}", s.withAuth(s.handleGetReport))
	s.mux.HandleFunc("POST /api/v1/data/reports/{reportId}/run", s.withAuth(s.handleRunReport))
	s.mux.HandleFunc("GET /api/v1/data/quality-checks", s.withAuth(s.handleListQualityChecks))
	s.mux.HandleFunc("GET /api/v1/data/audit", s.withAuth(s.handleListAuditLogs))
	s.mux.HandleFunc("GET /api/v1/data/audit/export.csv", s.withAuth(s.handleExportAuditLogsCSV))
	s.mux.HandleFunc("GET /api/v1/data/import-jobs", s.withAuth(s.handleListImportJobs))
	s.mux.HandleFunc("GET /api/v1/data/import-jobs/{jobId}", s.withAuth(s.handleGetImportJob))
	s.mux.HandleFunc("GET /api/v1/data/export-jobs", s.withAuth(s.handleListExportJobs))
	s.mux.HandleFunc("GET /api/v1/data/export-jobs/{jobId}", s.withAuth(s.handleGetExportJob))
	s.mux.HandleFunc("GET /api/v1/data/export-jobs/{jobId}/download", s.withAuth(s.handleDownloadExportJob))
	s.mux.HandleFunc("GET /api/v1/data/operation-plans", s.withAuth(s.handleListOperationPlans))
	s.mux.HandleFunc("POST /api/v1/data/operation-plans", s.withAuth(s.handleCreateOperationPlan))
	s.mux.HandleFunc("GET /api/v1/data/operation-plans/{planId}", s.withAuth(s.handleGetOperationPlan))
	s.mux.HandleFunc("POST /api/v1/data/operation-plans/{planId}/review", s.withAuth(s.handleReviewOperationPlan))
	s.mux.HandleFunc("POST /api/v1/data/operation-plans/{planId}/apply", s.withAuth(s.handleApplyOperationPlan))
	s.mux.HandleFunc("POST /api/v1/data/operation-plans/{planId}/cancel", s.withAuth(s.handleCancelOperationPlan))
	s.mux.HandleFunc("GET /api/v1/data/approvals", s.withAuth(s.handleListRecordApprovals))
	s.mux.HandleFunc("GET /api/v1/data/approvals/{approvalId}", s.withAuth(s.handleGetRecordApproval))
	s.mux.HandleFunc("POST /api/v1/data/approvals/{approvalId}/review", s.withAuth(s.handleReviewRecordApproval))
	s.mux.HandleFunc("GET /api/v1/data/datasets", s.withAuth(s.handleListDatasets))
	s.mux.HandleFunc("POST /api/v1/data/datasets", s.withAuth(s.handleCreateDataset))
	s.mux.HandleFunc("GET /api/v1/data/datasets/{datasetId}", s.withAuth(s.handleGetDataset))
	s.mux.HandleFunc("PATCH /api/v1/data/datasets/{datasetId}", s.withAuth(s.handleUpdateDataset))
	s.mux.HandleFunc("DELETE /api/v1/data/datasets/{datasetId}", s.withAuth(s.handleDeleteDataset))
	s.mux.HandleFunc("GET /api/v1/data/datasets/{datasetId}/fields", s.withAuth(s.handleListFields))
	s.mux.HandleFunc("GET /api/v1/data/datasets/{datasetId}/schema-proposals", s.withAuth(s.handleListSchemaProposals))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/schema-proposals", s.withAuth(s.handleProposeSchema))
	s.mux.HandleFunc("GET /api/v1/data/datasets/{datasetId}/schema-proposals/{proposalId}", s.withAuth(s.handleGetSchemaProposal))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/schema-proposals/apply", s.withAuth(s.handleApplySchemaProposal))
	s.mux.HandleFunc("PUT /api/v1/data/datasets/{datasetId}/fields", s.withAuth(s.handleUpsertFields))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/aggregate", s.withAuth(s.handleAggregateRecords))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/quality/run", s.withAuth(s.handleRunQualityCheck))
	s.mux.HandleFunc("GET /api/v1/data/datasets/{datasetId}/quality/runs", s.withAuth(s.handleListQualityRuns))
	s.mux.HandleFunc("GET /api/v1/data/datasets/{datasetId}/quality/runs/{runId}", s.withAuth(s.handleGetQualityRun))
	s.mux.HandleFunc("GET /api/v1/data/datasets/{datasetId}/records", s.withAuth(s.handleListRecords))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/records", s.withAuth(s.handleCreateRecord))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/records/query", s.withAuth(s.handleQueryRecords))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/records/export.csv", s.withAuth(s.handleExportRecordsCSV))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/records/export.jsonl", s.withAuth(s.handleExportRecordsJSONL))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/records/export.csv/jobs", s.withAuth(s.handleStartCSVExportJob))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/records/export.jsonl/jobs", s.withAuth(s.handleStartJSONLExportJob))
	s.mux.HandleFunc("GET /api/v1/data/datasets/{datasetId}/records/import-template.csv", s.withAuth(s.handleImportTemplateCSV))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/records/validate", s.withAuth(s.handleValidateRecord))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/records/batch", s.withAuth(s.handleBatchImportRecords))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/records/bulk-update", s.withAuth(s.handleBulkUpdateRecords))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/records/bulk-delete", s.withAuth(s.handleBulkDeleteRecords))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/records/batch/jobs", s.withAuth(s.handleStartBatchImportJob))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/records/import.csv", s.withAuth(s.handleImportRecordsCSV))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/records/import.csv/jobs", s.withAuth(s.handleStartCSVImportJob))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/records/import.jsonl", s.withAuth(s.handleImportRecordsJSONL))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/records/import.jsonl/jobs", s.withAuth(s.handleStartJSONLImportJob))
	s.mux.HandleFunc("GET /api/v1/data/datasets/{datasetId}/records/{recordId}", s.withAuth(s.handleGetRecord))
	s.mux.HandleFunc("GET /api/v1/data/datasets/{datasetId}/records/{recordId}/related", s.withAuth(s.handleGetRelatedRecords))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/records/{recordId}/approvals", s.withAuth(s.handleCreateRecordApproval))
	s.mux.HandleFunc("GET /api/v1/data/datasets/{datasetId}/records/{recordId}/revisions", s.withAuth(s.handleListRecordRevisions))
	s.mux.HandleFunc("GET /api/v1/data/datasets/{datasetId}/records/{recordId}/timeline", s.withAuth(s.handleGetRecordTimeline))
	s.mux.HandleFunc("POST /api/v1/data/datasets/{datasetId}/records/{recordId}/restore", s.withAuth(s.handleRestoreRecord))
	s.mux.HandleFunc("PATCH /api/v1/data/datasets/{datasetId}/records/{recordId}", s.withAuth(s.handleUpdateRecord))
	s.mux.HandleFunc("DELETE /api/v1/data/datasets/{datasetId}/records/{recordId}", s.withAuth(s.handleDeleteRecord))
}

func (s *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *HTTPServer) handleReady(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.Ready(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"service": "MaClawDataSrv", "version": s.version})
}

func (s *HTTPServer) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, openAPISpec(s.version))
}

func (s *HTTPServer) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.SetupStatus(r.Context())
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleSetupSyncTenants(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.SyncTenantsFromHubPublic(r.Context())
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleInitializeAdmin(w http.ResponseWriter, r *http.Request) {
	var in InitializeAdminInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.InitializeAdmin(r.Context(), in)
	writeResult(w, http.StatusCreated, out, err)
}

func (s *HTTPServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in LoginInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.Login(r.Context(), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListDataTenants(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.ListDataTenants(r.Context(), p)
	writeResult(w, http.StatusOK, map[string]any{"items": out}, err)
}

func (s *HTTPServer) handleSyncHubTenants(w http.ResponseWriter, r *http.Request, p Principal) {
	var in SyncHubTenantsInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.SyncHubTenants(r.Context(), p, in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleGetHubRegistration(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetHubRegistrationStatus(r.Context(), p)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleSaveHubRegistration(w http.ResponseWriter, r *http.Request, p Principal) {
	in, ok := s.decodeSaveHubRegistrationInput(w, r, p)
	if !ok {
		return
	}
	out, err := s.svc.SaveHubRegistration(r.Context(), p, in)
	writeResult(w, http.StatusOK, out, err)
}

type saveHubRegistrationPatch struct {
	HubBaseURL        *string `json:"hub_base_url"`
	PlatformID        *string `json:"platform_id"`
	PlatformName      *string `json:"platform_name"`
	CallbackBaseURL   *string `json:"callback_base_url"`
	VirtualMailDomain *string `json:"virtual_mail_domain"`
}

func (s *HTTPServer) decodeSaveHubRegistrationInput(w http.ResponseWriter, r *http.Request, p Principal) (SaveHubRegistrationInput, bool) {
	if !principalIsGlobalAdmin(p) {
		writeResult(w, http.StatusOK, nil, ErrForbidden)
		return SaveHubRegistrationInput{}, false
	}
	var patch saveHubRegistrationPatch
	if !decodeJSON(w, r, &patch) {
		return SaveHubRegistrationInput{}, false
	}
	status, err := s.svc.GetHubRegistrationStatus(r.Context(), p)
	if err != nil {
		writeResult(w, http.StatusOK, nil, err)
		return SaveHubRegistrationInput{}, false
	}
	current := status.Status
	in := SaveHubRegistrationInput{
		HubBaseURL:        current.HubBaseURL,
		PlatformID:        current.PlatformID,
		PlatformName:      current.PlatformName,
		CallbackBaseURL:   current.CallbackBaseURL,
		VirtualMailDomain: current.VirtualMailDomain,
	}
	if patch.HubBaseURL != nil {
		in.HubBaseURL = *patch.HubBaseURL
	}
	if patch.PlatformID != nil {
		in.PlatformID = *patch.PlatformID
	}
	if patch.PlatformName != nil {
		in.PlatformName = *patch.PlatformName
	}
	if patch.CallbackBaseURL != nil {
		in.CallbackBaseURL = *patch.CallbackBaseURL
	}
	if patch.VirtualMailDomain != nil {
		in.VirtualMailDomain = *patch.VirtualMailDomain
	}
	return in, true
}

func (s *HTTPServer) handleRegisterHub(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.RegisterHub(r.Context(), p)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleSyncTenantsFromHub(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.SyncTenantsFromHub(r.Context(), p)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleCapabilities(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.Capabilities(r.Context(), p)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListAdminAccounts(w http.ResponseWriter, r *http.Request, p Principal) {
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant"))
	out, err := s.svc.ListAdminAccountsForPrincipal(r.Context(), p, tenantID)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleCreateAdminAccount(w http.ResponseWriter, r *http.Request, p Principal) {
	var in CreateAdminAccountInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.CreateAdminAccount(r.Context(), p, in)
	writeResult(w, http.StatusCreated, out, err)
}

func (s *HTTPServer) handleUpdateAdminAccount(w http.ResponseWriter, r *http.Request, p Principal) {
	var in UpdateAdminAccountInput
	if !decodeJSON(w, r, &in) {
		return
	}
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant"))
	if tenantID == "" {
		tenantID = p.TenantID
	}
	out, err := s.svc.UpdateAdminAccount(r.Context(), p, tenantID, r.PathValue("username"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListAdminSessions(w http.ResponseWriter, r *http.Request, p Principal) {
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant"))
	out, err := s.svc.ListAdminSessionsForPrincipal(r.Context(), p, tenantID)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleRevokeAdminSession(w http.ResponseWriter, r *http.Request, p Principal) {
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant"))
	if tenantID == "" {
		tenantID = p.TenantID
	}
	out, err := s.svc.RevokeAdminSession(r.Context(), p, tenantID, r.PathValue("sessionId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleUpdateAdminSession(w http.ResponseWriter, r *http.Request, p Principal) {
	var in UpdateAdminSessionInput
	if !decodeJSON(w, r, &in) {
		return
	}
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant"))
	if tenantID == "" {
		tenantID = p.TenantID
	}
	out, err := s.svc.UpdateAdminSession(r.Context(), p, tenantID, r.PathValue("sessionId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListAPIKeyPolicies(w http.ResponseWriter, r *http.Request, p Principal) {
	in := QueryAPIKeyPoliciesInput{
		Q:        strings.TrimSpace(r.URL.Query().Get("q")),
		Status:   strings.TrimSpace(r.URL.Query().Get("status")),
		Limit:    parseLimit(r.URL.Query().Get("limit")),
		Before:   strings.TrimSpace(r.URL.Query().Get("before")),
		BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id")),
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("enabled")); raw != "" {
		enabled, err := parseBoolQueryValue(raw)
		if err != nil {
			writeError(w, err)
			return
		}
		in.Enabled = &enabled
	}
	out, err := s.svc.ListAPIKeyPolicies(r.Context(), p, in)
	writeResult(w, http.StatusOK, apiKeyPolicyListResponse(out, in.Limit), err)
}

func (s *HTTPServer) handleListAccessPolicyPresets(w http.ResponseWriter, r *http.Request, p Principal) {
	in := QueryAccessPolicyPresetsInput{Limit: parseLimit(r.URL.Query().Get("limit")), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}
	out, err := s.svc.ListAccessPolicyPresets(r.Context(), p, in)
	writeResult(w, http.StatusOK, accessPolicyPresetListResponse(out, in.Limit), err)
}

func (s *HTTPServer) handleReviewAccess(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.ReviewAccess(r.Context(), p, AccessReviewInput{MinSeverity: strings.TrimSpace(r.URL.Query().Get("min_severity"))})
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handlePlanAccessRemediation(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.PlanAccessRemediation(r.Context(), p, AccessRemediationPlanInput{MinSeverity: strings.TrimSpace(r.URL.Query().Get("min_severity"))})
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleCheckAccess(w http.ResponseWriter, r *http.Request, p Principal) {
	var in AccessCheckInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.CheckAccess(r.Context(), p, in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleCreateAPIKeyPolicy(w http.ResponseWriter, r *http.Request, p Principal) {
	var in CreateAPIKeyPolicyInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.CreateAPIKeyPolicy(r.Context(), p, in)
	writeResult(w, http.StatusCreated, out, err)
}

func (s *HTTPServer) handleGetAPIKeyPolicy(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetAPIKeyPolicy(r.Context(), p, r.PathValue("keyId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleGetAPIKeyPolicyCapabilities(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetAPIKeyPolicyCapabilities(r.Context(), p, r.PathValue("keyId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleUpdateAPIKeyPolicy(w http.ResponseWriter, r *http.Request, p Principal) {
	var in UpdateAPIKeyPolicyInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateAPIKeyPolicy(r.Context(), p, r.PathValue("keyId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleRotateAPIKeyPolicy(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.RotateAPIKeyPolicySecret(r.Context(), p, r.PathValue("keyId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleDisableAPIKeyPolicy(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.DisableAPIKeyPolicy(r.Context(), p, r.PathValue("keyId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListBusinessDomains(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	in := QueryBusinessDomainsInput{Limit: listProbeLimit(limit, 100, 500), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}
	out, err := s.svc.ListBusinessDomains(r.Context(), p, in)
	writeResult(w, http.StatusOK, businessDomainListResponse(out, limit), err)
}

func (s *HTTPServer) handleGetBusinessDomain(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetBusinessDomain(r.Context(), p, r.PathValue("domain"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListBusinessObjects(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	in := QueryBusinessObjectsInput{
		AppID:       strings.TrimSpace(r.URL.Query().Get("app_id")),
		BlueprintID: strings.TrimSpace(r.URL.Query().Get("blueprint_id")),
		Domain:      strings.TrimSpace(r.URL.Query().Get("domain")),
		ObjectRole:  strings.TrimSpace(r.URL.Query().Get("object_role")),
		Limit:       listProbeLimit(limit, 100, 500),
		BeforeID:    strings.TrimSpace(r.URL.Query().Get("before_id")),
	}
	out, err := s.svc.ListBusinessObjects(r.Context(), p, in)
	writeResult(w, http.StatusOK, businessObjectListResponse(out, limit), err)
}

func (s *HTTPServer) handleResolveObjectRole(w http.ResponseWriter, r *http.Request, p Principal) {
	var in ResolveObjectRoleInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.ResolveObjectRole(r.Context(), p, in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListAppInstallations(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	in := QueryAppInstallationsInput{
		AppID:       strings.TrimSpace(r.URL.Query().Get("app_id")),
		BlueprintID: strings.TrimSpace(r.URL.Query().Get("blueprint_id")),
		Status:      strings.TrimSpace(r.URL.Query().Get("status")),
		Limit:       effectiveLimit(limit, 100, 500),
		Before:      strings.TrimSpace(r.URL.Query().Get("before")),
		BeforeID:    strings.TrimSpace(r.URL.Query().Get("before_id")),
	}
	out, err := s.svc.ListAppInstallations(r.Context(), p, in)
	writeResult(w, http.StatusOK, appInstallationListResponse(out, limit), err)
}

func (s *HTTPServer) handleCreateAppInstallation(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	var in UpsertAppInstallationInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpsertAppInstallation(r.Context(), p, "", in)
	writeResult(w, http.StatusCreated, out, err)
}

func (s *HTTPServer) handleGetAppInstallation(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetAppInstallation(r.Context(), p, r.PathValue("appId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleUpdateAppInstallation(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	var in UpsertAppInstallationInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpsertAppInstallation(r.Context(), p, r.PathValue("appId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListBusinessRules(w http.ResponseWriter, r *http.Request, p Principal) {
	in := QueryBusinessRulesInput{
		Domain:         strings.TrimSpace(r.URL.Query().Get("domain")),
		DatasetID:      strings.TrimSpace(r.URL.Query().Get("dataset_id")),
		BusinessAction: strings.TrimSpace(r.URL.Query().Get("business_action_id")),
		Severity:       strings.TrimSpace(r.URL.Query().Get("severity")),
		Limit:          parseLimit(r.URL.Query().Get("limit")),
		BeforeID:       strings.TrimSpace(r.URL.Query().Get("before_id")),
	}
	out, err := s.svc.ListBusinessRules(r.Context(), p, in)
	writeResult(w, http.StatusOK, businessRuleListResponse(out, in.Limit), err)
}

func (s *HTTPServer) handleEvaluateBusinessRules(w http.ResponseWriter, r *http.Request, p Principal) {
	var in EvaluateBusinessRulesInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.EvaluateBusinessRules(r.Context(), p, in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListRelationships(w http.ResponseWriter, r *http.Request, p Principal) {
	in := QueryRelationshipsInput{DatasetID: strings.TrimSpace(r.URL.Query().Get("dataset_id")), Limit: parseLimit(r.URL.Query().Get("limit")), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}
	out, err := s.svc.ListRelationships(r.Context(), p, in)
	writeResult(w, http.StatusOK, relationshipListResponse(out, in.Limit), err)
}

func (s *HTTPServer) handleResolveBusinessIntent(w http.ResponseWriter, r *http.Request, p Principal) {
	var in ResolveBusinessIntentInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.ResolveBusinessIntent(r.Context(), p, in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleMISInbox(w http.ResponseWriter, r *http.Request, p Principal) {
	in, err := inboxQueryFromRequest(r)
	if err != nil {
		writeError(w, err)
		return
	}
	out, err := s.svc.MISInbox(r.Context(), p, in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleMISInboxSummary(w http.ResponseWriter, r *http.Request, p Principal) {
	in, err := inboxQueryFromRequest(r)
	if err != nil {
		writeError(w, err)
		return
	}
	out, err := s.svc.MISInboxSummary(r.Context(), p, in)
	writeResult(w, http.StatusOK, out, err)
}

func inboxQueryFromRequest(r *http.Request) (QueryMISInboxInput, error) {
	includeOK, err := parseOptionalBoolQueryValue(r, "include_ok")
	if err != nil {
		return QueryMISInboxInput{}, err
	}
	return QueryMISInboxInput{
		Limit:     parseLimit(r.URL.Query().Get("limit")),
		DatasetID: strings.TrimSpace(r.URL.Query().Get("dataset_id")),
		Type:      strings.TrimSpace(r.URL.Query().Get("type")),
		Status:    strings.TrimSpace(r.URL.Query().Get("status")),
		IncludeOK: includeOK,
		Before:    strings.TrimSpace(r.URL.Query().Get("before")),
		BeforeID:  strings.TrimSpace(r.URL.Query().Get("before_id")),
	}, nil
}

func (s *HTTPServer) handleSystemStats(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.SystemStats(r.Context(), p)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleGovernanceEvidencePack(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GovernanceEvidencePack(r.Context(), p, governanceEvidencePackInputFromRequest(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeGovernanceEvidenceHeaders(w, out)
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleGovernanceEvidenceSummaryText(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GovernanceEvidencePack(r.Context(), p, governanceEvidencePackInputFromRequest(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeGovernanceEvidenceHeaders(w, out)
	writeDownloadHeaders(w, "text/plain; charset=utf-8", `attachment; filename="maclaw-datasrv-governance-evidence-summary-`+safeFilename(out.EvidenceID)+`.txt"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(out.SummaryText))
}

func writeGovernanceEvidenceHeaders(w http.ResponseWriter, pack *GovernanceEvidencePack) {
	if pack == nil {
		return
	}
	if pack.EvidenceID != "" {
		w.Header().Set("X-MaClaw-Evidence-ID", pack.EvidenceID)
	}
	if pack.EvidenceSHA256 != "" {
		w.Header().Set("X-MaClaw-Evidence-SHA256", pack.EvidenceSHA256)
	}
}

func governanceEvidencePackInputFromRequest(r *http.Request) GovernanceEvidencePackInput {
	return GovernanceEvidencePackInput{
		MinSeverity: strings.TrimSpace(r.URL.Query().Get("min_severity")),
		Lang:        strings.TrimSpace(r.URL.Query().Get("lang")),
	}
}

func (s *HTTPServer) handleRunMaintenance(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	var in RunMaintenanceInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.RunMaintenance(r.Context(), p, in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListTemplates(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	in := QueryTemplatesInput{Limit: listProbeLimit(limit, 100, 500), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}
	out, err := s.svc.ListTemplates(r.Context(), p, in)
	writeResult(w, http.StatusOK, templateListResponse(out, limit), err)
}

func (s *HTTPServer) handleGetTemplate(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetTemplate(r.Context(), p, r.PathValue("templateId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleBootstrapTemplates(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	var in BootstrapTemplatesInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.BootstrapTemplates(r.Context(), p, in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleCreateDatasetFromTemplate(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	var in CreateFromTemplateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.CreateDatasetFromTemplate(r.Context(), p, r.PathValue("templateId"), in)
	writeResult(w, http.StatusCreated, out, err)
}

func (s *HTTPServer) handleListBackups(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	in := QueryBackupsInput{Limit: limit, Before: strings.TrimSpace(r.URL.Query().Get("before")), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}
	out, err := s.svc.ListBackups(r.Context(), p, in)
	writeResult(w, http.StatusOK, backupListResponse(out, limit), err)
}

func (s *HTTPServer) handleCreateBackup(w http.ResponseWriter, r *http.Request, p Principal) {
	var in CreateBackupInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.CreateBackup(r.Context(), p, in)
	writeResult(w, http.StatusCreated, out, err)
}

func (s *HTTPServer) handleGetBackup(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetBackup(r.Context(), p, r.PathValue("backupId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleDownloadBackup(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	data, info, err := s.svc.ReadBackup(r.Context(), p, r.PathValue("backupId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeDownloadHeaders(w, "application/octet-stream", `attachment; filename="`+safeFilename(info.ID)+`.db"`)
	w.Header().Set("X-MaClaw-Backup-SHA256", info.SHA256)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *HTTPServer) handleRestoreBackup(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	var in RestoreBackupInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.RestoreBackup(r.Context(), p, r.PathValue("backupId"), in)
	writeResult(w, http.StatusOK, out, err)
}
func (s *HTTPServer) handleListBusinessActions(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	in := QueryBusinessActionsInput{Limit: listProbeLimit(limit, 100, 500), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}
	out, err := s.svc.ListBusinessActions(r.Context(), p, in)
	writeResult(w, http.StatusOK, businessActionListResponse(out, limit), err)
}

func (s *HTTPServer) handleListEventContracts(w http.ResponseWriter, r *http.Request, p Principal) {
	in := QueryEventContractsInput{Domain: strings.TrimSpace(r.URL.Query().Get("domain")), Limit: parseLimit(r.URL.Query().Get("limit")), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}
	out, err := s.svc.ListEventContracts(r.Context(), p, in.Domain, in)
	writeResult(w, http.StatusOK, eventContractListResponse(out, in.Limit), err)
}

func (s *HTTPServer) handleGetEventContract(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetEventContract(r.Context(), p, r.PathValue("actionId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListExternalConnectors(w http.ResponseWriter, r *http.Request, p Principal) {
	in, err := connectorQueryFromRequest(r)
	if err != nil {
		writeError(w, err)
		return
	}
	out, err := s.svc.ListExternalConnectors(r.Context(), p, in)
	writeResult(w, http.StatusOK, externalConnectorListResponse(out, in.Limit), err)
}

func (s *HTTPServer) handleListConnectorHealth(w http.ResponseWriter, r *http.Request, p Principal) {
	in, err := connectorQueryFromRequest(r)
	if err != nil {
		writeError(w, err)
		return
	}
	out, err := s.svc.ListConnectorHealth(r.Context(), p, in)
	writeResult(w, http.StatusOK, connectorHealthListResponse(out, in.Limit), err)
}

func connectorQueryFromRequest(r *http.Request) (QueryExternalConnectorsInput, error) {
	enabledRaw := strings.TrimSpace(r.URL.Query().Get("enabled"))
	var enabled *bool
	if enabledRaw != "" {
		value, err := parseBoolQueryValue(enabledRaw)
		if err != nil {
			return QueryExternalConnectorsInput{}, err
		}
		enabled = &value
	}
	return QueryExternalConnectorsInput{Domain: r.URL.Query().Get("domain"), Kind: r.URL.Query().Get("kind"), Enabled: enabled, Limit: parseLimit(r.URL.Query().Get("limit")), Before: strings.TrimSpace(r.URL.Query().Get("before")), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}, nil
}

func (s *HTTPServer) handleCreateExternalConnector(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	var in UpsertExternalConnectorInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpsertExternalConnector(r.Context(), p, "", in)
	writeResult(w, http.StatusCreated, out, err)
}

func (s *HTTPServer) handleGetExternalConnector(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetExternalConnector(r.Context(), p, r.PathValue("connectorId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleUpdateExternalConnector(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	var in UpsertExternalConnectorInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpsertExternalConnector(r.Context(), p, r.PathValue("connectorId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleDeleteExternalConnector(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	err := s.svc.DeleteExternalConnector(r.Context(), p, r.PathValue("connectorId"))
	writeResult(w, http.StatusOK, map[string]string{"status": "deleted"}, err)
}

func (s *HTTPServer) handleTestExternalConnector(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.TestExternalConnector(r.Context(), p, r.PathValue("connectorId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleValidateConnectorConfig(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.ValidateExternalConnectorConfig(r.Context(), p, r.PathValue("connectorId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleCheckConnectorReadiness(w http.ResponseWriter, r *http.Request, p Principal) {
	var in ConnectorReadinessInput
	if r.Body != nil && r.ContentLength != 0 {
		if !decodeJSON(w, r, &in) {
			return
		}
	}
	out, err := s.svc.CheckExternalConnectorReadiness(r.Context(), p, r.PathValue("connectorId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleConnectorHealth(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.ConnectorHealth(r.Context(), p, r.PathValue("connectorId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleGetConnectorSyncState(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetConnectorSyncState(r.Context(), p, r.PathValue("connectorId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleUpdateConnectorSyncState(w http.ResponseWriter, r *http.Request, p Principal) {
	var in UpdateConnectorSyncStateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateConnectorSyncState(r.Context(), p, r.PathValue("connectorId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListConnectorSyncRuns(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	in := QueryConnectorSyncRunsInput{Limit: limit, Before: strings.TrimSpace(r.URL.Query().Get("before")), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}
	out, err := s.svc.ListConnectorSyncRuns(r.Context(), p, r.PathValue("connectorId"), in)
	writeResult(w, http.StatusOK, connectorSyncRunListResponse(out, limit), err)
}

func (s *HTTPServer) handlePlanConnectorSync(w http.ResponseWriter, r *http.Request, p Principal) {
	var in ConnectorSyncPlanInput
	if r.Body != nil && r.ContentLength != 0 {
		if !decodeJSON(w, r, &in) {
			return
		}
	}
	out, err := s.svc.PlanConnectorSync(r.Context(), p, r.PathValue("connectorId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleSyncConnectorBatch(w http.ResponseWriter, r *http.Request, p Principal) {
	var in ConnectorSyncBatchInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.SyncConnectorBatch(r.Context(), p, r.PathValue("connectorId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handlePatchConnectorConfig(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	var in PatchConnectorConfigInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.PatchExternalConnectorConfig(r.Context(), p, r.PathValue("connectorId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleSuggestConnectorMapping(w http.ResponseWriter, r *http.Request, p Principal) {
	var in SuggestConnectorMappingInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.SuggestConnectorMapping(r.Context(), p, r.PathValue("connectorId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleIngestConnectorEvent(w http.ResponseWriter, r *http.Request, p Principal) {
	var in DataEventInput
	if !decodeJSON(w, r, &in) {
		return
	}
	connectorID := r.PathValue("connectorId")
	if strings.TrimSpace(in.Source) == "" {
		if connector, getErr := s.svc.GetExternalConnector(r.Context(), p, connectorID); getErr == nil && connector != nil {
			in.Source = firstNonEmpty(connector.Kind, connector.ID)
		}
	}
	out, err := s.svc.IngestConnectorEvent(r.Context(), p, connectorID, in)
	if err != nil && !in.DryRun {
		if deadLetter, dlqErr := s.svc.CreateDataEventDeadLetter(r.Context(), p, in, err); dlqErr == nil && deadLetter != nil {
			writeJSON(w, httpStatusForError(err), map[string]any{"error": err.Error(), "dead_letter": deadLetter})
			return
		}
	}
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handlePreviewConnectorEvent(w http.ResponseWriter, r *http.Request, p Principal) {
	var in DataEventInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.PreviewConnectorEvent(r.Context(), p, r.PathValue("connectorId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleGetBusinessAction(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetBusinessAction(r.Context(), p, r.PathValue("actionId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleExecuteBusinessAction(w http.ResponseWriter, r *http.Request, p Principal) {
	var in ExecuteBusinessActionInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.ExecuteBusinessAction(r.Context(), p, r.PathValue("actionId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListBusinessViews(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	in := QueryBusinessViewsInput{Limit: listProbeLimit(limit, 100, 500), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}
	out, err := s.svc.ListBusinessViews(r.Context(), p, in)
	writeResult(w, http.StatusOK, businessViewListResponse(out, limit), err)
}

func (s *HTTPServer) handleGetBusinessView(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetBusinessView(r.Context(), p, r.PathValue("viewId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleQueryBusinessView(w http.ResponseWriter, r *http.Request, p Principal) {
	var in QueryBusinessViewInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.QueryBusinessView(r.Context(), p, r.PathValue("viewId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListDashboards(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	in := QueryDashboardsInput{Limit: listProbeLimit(limit, 100, 500), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}
	out, err := s.svc.ListDashboards(r.Context(), p, in)
	writeResult(w, http.StatusOK, dashboardListResponse(out, limit), err)
}

func (s *HTTPServer) handleGetDashboard(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetDashboard(r.Context(), p, r.PathValue("dashboardId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleRunDashboard(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.RunDashboard(r.Context(), p, r.PathValue("dashboardId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleIngestEvent(w http.ResponseWriter, r *http.Request, p Principal) {
	var in DataEventInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.IngestEvent(r.Context(), p, in)
	if err != nil && !in.DryRun {
		if deadLetter, dlqErr := s.svc.CreateDataEventDeadLetter(r.Context(), p, in, err); dlqErr == nil && deadLetter != nil {
			writeJSON(w, httpStatusForError(err), map[string]any{"error": err.Error(), "dead_letter": deadLetter})
			return
		}
	}
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListDataEvents(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	in := QueryDataEventsInput{
		DatasetID:      strings.TrimSpace(r.URL.Query().Get("dataset_id")),
		RecordID:       strings.TrimSpace(r.URL.Query().Get("record_id")),
		Source:         strings.TrimSpace(r.URL.Query().Get("source")),
		EventType:      strings.TrimSpace(r.URL.Query().Get("event_type")),
		BusinessAction: strings.TrimSpace(r.URL.Query().Get("business_action_id")),
		IdempotencyKey: strings.TrimSpace(r.URL.Query().Get("idempotency_key")),
		Before:         strings.TrimSpace(r.URL.Query().Get("before")),
		BeforeID:       strings.TrimSpace(r.URL.Query().Get("before_id")),
		Limit:          limit,
	}
	out, err := s.svc.QueryDataEvents(r.Context(), p, in)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	writeResult(w, http.StatusOK, dataEventListResponse(out, limit), err)
}

func (s *HTTPServer) handleListEventDeadLetters(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	in := QueryDataEventDeadLettersInput{
		Status:         strings.TrimSpace(r.URL.Query().Get("status")),
		Source:         strings.TrimSpace(r.URL.Query().Get("source")),
		EventType:      strings.TrimSpace(r.URL.Query().Get("event_type")),
		BusinessAction: strings.TrimSpace(r.URL.Query().Get("business_action_id")),
		DatasetID:      strings.TrimSpace(r.URL.Query().Get("dataset_id")),
		RecordID:       strings.TrimSpace(r.URL.Query().Get("record_id")),
		IdempotencyKey: strings.TrimSpace(r.URL.Query().Get("idempotency_key")),
		Before:         strings.TrimSpace(r.URL.Query().Get("before")),
		BeforeID:       strings.TrimSpace(r.URL.Query().Get("before_id")),
		Limit:          limit,
	}
	out, err := s.svc.QueryDataEventDeadLetters(r.Context(), p, in)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	writeResult(w, http.StatusOK, dataEventDeadLetterListResponse(out, limit), err)
}

func (s *HTTPServer) handleGetEventDeadLetter(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetDataEventDeadLetter(r.Context(), p, r.PathValue("deadLetterId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleRetryEventDeadLetter(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	out, err := s.svc.RetryDataEventDeadLetter(r.Context(), p, r.PathValue("deadLetterId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleResolveEventDeadLetter(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	var in ResolveDataEventDeadLetterInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.ResolveDataEventDeadLetter(r.Context(), p, r.PathValue("deadLetterId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListReports(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	in := QueryReportsInput{Limit: listProbeLimit(limit, 100, 500), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}
	out, err := s.svc.ListReports(r.Context(), p, in)
	writeResult(w, http.StatusOK, reportListResponse(out, limit), err)
}

func (s *HTTPServer) handleGetReport(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetReport(r.Context(), p, r.PathValue("reportId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleRunReport(w http.ResponseWriter, r *http.Request, p Principal) {
	var in AggregateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.RunReport(r.Context(), p, r.PathValue("reportId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListQualityChecks(w http.ResponseWriter, r *http.Request, p Principal) {
	in := QueryQualityChecksInput{Limit: parseLimit(r.URL.Query().Get("limit")), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}
	out, err := s.svc.ListQualityChecks(r.Context(), p, in)
	writeResult(w, http.StatusOK, qualityCheckListResponse(out, in.Limit), err)
}

func (s *HTTPServer) handleListAuditLogs(w http.ResponseWriter, r *http.Request, p Principal) {
	limit, in := auditQueryFromRequest(r)
	out, err := s.svc.QueryAuditLogs(r.Context(), p, in)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	writeResult(w, http.StatusOK, auditLogListResponse(out, limit), err)
}

func (s *HTTPServer) handleExportAuditLogsCSV(w http.ResponseWriter, r *http.Request, p Principal) {
	_, in := auditQueryFromRequest(r)
	if in.Limit <= 0 || in.Limit > 5000 {
		in.Limit = 5000
	}
	out, err := s.svc.QueryAuditLogs(r.Context(), p, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeDownloadHeaders(w, "text/csv; charset=utf-8", `attachment; filename="audit.csv"`)
	w.WriteHeader(http.StatusOK)
	_ = writeAuditLogsCSV(w, out)
}

func auditQueryFromRequest(r *http.Request) (int, QueryAuditLogsInput) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	return limit, QueryAuditLogsInput{
		DatasetID:  strings.TrimSpace(r.URL.Query().Get("dataset_id")),
		Action:     strings.TrimSpace(r.URL.Query().Get("action")),
		UserID:     strings.TrimSpace(r.URL.Query().Get("user_id")),
		TargetType: strings.TrimSpace(r.URL.Query().Get("target_type")),
		TargetID:   strings.TrimSpace(r.URL.Query().Get("target_id")),
		Q:          strings.TrimSpace(r.URL.Query().Get("q")),
		Before:     strings.TrimSpace(r.URL.Query().Get("before")),
		BeforeID:   strings.TrimSpace(r.URL.Query().Get("before_id")),
		Limit:      limit,
	}
}

func (s *HTTPServer) handleListDatasets(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	in := QueryDatasetsInput{Limit: limit, Before: strings.TrimSpace(r.URL.Query().Get("before")), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}
	out, err := s.svc.ListDatasets(r.Context(), p, in)
	writeResult(w, http.StatusOK, datasetListResponse(out, limit), err)
}

func (s *HTTPServer) handleCreateDataset(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	var in CreateDatasetInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.CreateDataset(r.Context(), p, in)
	writeResult(w, http.StatusCreated, out, err)
}

func (s *HTTPServer) handleGetDataset(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetDataset(r.Context(), p, r.PathValue("datasetId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleUpdateDataset(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	var in UpdateDatasetInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateDataset(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleDeleteDataset(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	err := s.svc.DeleteDataset(r.Context(), p, r.PathValue("datasetId"))
	writeResult(w, http.StatusOK, map[string]string{"status": "deleted"}, err)
}

func (s *HTTPServer) handleListSchemaProposals(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	in := ListSchemaProposalsInput{Status: strings.TrimSpace(r.URL.Query().Get("status")), Limit: limit, Before: strings.TrimSpace(r.URL.Query().Get("before")), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}
	out, err := s.svc.ListSchemaProposals(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusOK, schemaProposalListResponse(out, limit), err)
}

func (s *HTTPServer) handleGetSchemaProposal(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetSchemaProposal(r.Context(), p, r.PathValue("datasetId"), r.PathValue("proposalId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleProposeSchema(w http.ResponseWriter, r *http.Request, p Principal) {
	var in SchemaProposalInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.ProposeSchema(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleApplySchemaProposal(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	var in ApplySchemaProposalInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.ApplySchemaProposal(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListFields(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	in := QueryFieldsInput{Limit: limit, Before: strings.TrimSpace(r.URL.Query().Get("before")), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}
	out, err := s.svc.ListFields(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusOK, fieldListResponse(out, limit), err)
}

func (s *HTTPServer) handleUpsertFields(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	var in UpsertFieldsInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpsertFields(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleAggregateRecords(w http.ResponseWriter, r *http.Request, p Principal) {
	var in AggregateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.AggregateRecords(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleRunQualityCheck(w http.ResponseWriter, r *http.Request, p Principal) {
	var in RunQualityCheckInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.RunQualityCheck(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListQualityRuns(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	out, err := s.svc.ListQualityRuns(r.Context(), p, r.PathValue("datasetId"), QueryQualityRunsInput{Limit: limit, Before: r.URL.Query().Get("before"), BeforeID: r.URL.Query().Get("before_id")})
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	writeResult(w, http.StatusOK, qualityRunListResponse(out, limit), err)
}

func (s *HTTPServer) handleGetQualityRun(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetQualityRun(r.Context(), p, r.PathValue("datasetId"), r.PathValue("runId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListRecords(w http.ResponseWriter, r *http.Request, p Principal) {
	in := QueryRecordsInput{Q: strings.TrimSpace(r.URL.Query().Get("q")), Tag: strings.TrimSpace(r.URL.Query().Get("tag")), Before: strings.TrimSpace(r.URL.Query().Get("before")), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id")), Limit: parseLimit(r.URL.Query().Get("limit"))}
	out, err := s.svc.QueryRecords(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusOK, listResponse(out, in), err)
}

func (s *HTTPServer) handleBatchImportRecords(w http.ResponseWriter, r *http.Request, p Principal) {
	var in BatchImportRecordsInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.BatchImportRecords(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleBulkUpdateRecords(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	var in BulkUpdateRecordsInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.BulkUpdateRecords(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleBulkDeleteRecords(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	var in BulkDeleteRecordsInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.BulkDeleteRecords(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleStartBatchImportJob(w http.ResponseWriter, r *http.Request, p Principal) {
	var in BatchImportRecordsInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.StartBatchImportJob(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusAccepted, out, err)
}

func (s *HTTPServer) handleImportRecordsCSV(w http.ResponseWriter, r *http.Request, p Principal) {
	in, ok := s.decodeImportCSVInput(w, r)
	if !ok {
		return
	}
	out, err := s.svc.ImportRecordsCSV(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleStartCSVImportJob(w http.ResponseWriter, r *http.Request, p Principal) {
	in, ok := s.decodeImportCSVInput(w, r)
	if !ok {
		return
	}
	out, err := s.svc.StartCSVImportJob(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusAccepted, out, err)
}

func (s *HTTPServer) handleImportRecordsJSONL(w http.ResponseWriter, r *http.Request, p Principal) {
	in, ok := s.decodeImportJSONLInput(w, r)
	if !ok {
		return
	}
	out, err := s.svc.ImportRecordsJSONL(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleStartJSONLImportJob(w http.ResponseWriter, r *http.Request, p Principal) {
	in, ok := s.decodeImportJSONLInput(w, r)
	if !ok {
		return
	}
	out, err := s.svc.StartJSONLImportJob(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusAccepted, out, err)
}

func (s *HTTPServer) decodeImportCSVInput(w http.ResponseWriter, r *http.Request) (ImportCSVInput, bool) {
	var in ImportCSVInput
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.Contains(contentType, "text/csv") || strings.Contains(contentType, "text/plain") {
		defer r.Body.Close()
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		data, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid csv body"})
			return ImportCSVInput{}, false
		}
		in.CSVText = string(data)
		dryRun, err := parseOptionalBoolQueryValue(r, "dry_run")
		if err != nil {
			writeError(w, err)
			return ImportCSVInput{}, false
		}
		in.DryRun = dryRun
		return in, true
	}
	if !decodeJSON(w, r, &in) {
		return ImportCSVInput{}, false
	}
	return in, true
}

func (s *HTTPServer) decodeImportJSONLInput(w http.ResponseWriter, r *http.Request) (ImportJSONLInput, bool) {
	var in ImportJSONLInput
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.Contains(contentType, "application/x-ndjson") || strings.Contains(contentType, "application/jsonl") || strings.Contains(contentType, "text/plain") {
		defer r.Body.Close()
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		data, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid jsonl body"})
			return ImportJSONLInput{}, false
		}
		in.JSONLText = string(data)
		dryRun, err := parseOptionalBoolQueryValue(r, "dry_run")
		if err != nil {
			writeError(w, err)
			return ImportJSONLInput{}, false
		}
		in.DryRun = dryRun
		return in, true
	}
	if !decodeJSON(w, r, &in) {
		return ImportJSONLInput{}, false
	}
	return in, true
}

func (s *HTTPServer) handleListImportJobs(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	in := QueryImportJobsInput{DatasetID: strings.TrimSpace(r.URL.Query().Get("dataset_id")), Status: strings.TrimSpace(r.URL.Query().Get("status")), Limit: limit, Before: strings.TrimSpace(r.URL.Query().Get("before")), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}
	out, err := s.svc.ListImportJobs(r.Context(), p, in)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	writeResult(w, http.StatusOK, importJobListResponse(out, limit), err)
}

func (s *HTTPServer) handleGetImportJob(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetImportJob(r.Context(), p, r.PathValue("jobId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListExportJobs(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	in := QueryExportJobsInput{DatasetID: strings.TrimSpace(r.URL.Query().Get("dataset_id")), Status: strings.TrimSpace(r.URL.Query().Get("status")), Limit: limit, Before: strings.TrimSpace(r.URL.Query().Get("before")), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}
	out, err := s.svc.ListExportJobs(r.Context(), p, in)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	writeResult(w, http.StatusOK, exportJobListResponse(out, limit), err)
}

func (s *HTTPServer) handleGetExportJob(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetExportJob(r.Context(), p, r.PathValue("jobId"))
	if out != nil {
		out.ResultText = ""
	}
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleDownloadExportJob(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetExportJob(r.Context(), p, r.PathValue("jobId"))
	if err != nil {
		writeError(w, err)
		return
	}
	if out.Status != exportJobStatusCompleted {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "export job is not completed"})
		return
	}
	disposition := `attachment; filename="` + safeFilename(out.DatasetID) + `.` + safeFilename(out.Format) + `"`
	switch out.Format {
	case "csv":
		writeDownloadHeaders(w, "text/csv; charset=utf-8", disposition)
	case "jsonl":
		writeDownloadHeaders(w, "application/x-ndjson; charset=utf-8", disposition)
	default:
		writeDownloadHeaders(w, "text/plain; charset=utf-8", disposition)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, out.ResultText)
}

func (s *HTTPServer) handleListOperationPlans(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	in := QueryOperationPlansInput{DatasetID: strings.TrimSpace(r.URL.Query().Get("dataset_id")), Operation: strings.TrimSpace(r.URL.Query().Get("operation")), Status: strings.TrimSpace(r.URL.Query().Get("status")), Before: strings.TrimSpace(r.URL.Query().Get("before")), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id")), Limit: limit}
	out, err := s.svc.ListOperationPlans(r.Context(), p, in)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	writeResult(w, http.StatusOK, operationPlanListResponse(out, limit), err)
}

func (s *HTTPServer) handleCreateOperationPlan(w http.ResponseWriter, r *http.Request, p Principal) {
	var in CreateOperationPlanInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.CreateOperationPlan(r.Context(), p, in)
	writeResult(w, http.StatusCreated, out, err)
}

func (s *HTTPServer) handleGetOperationPlan(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetOperationPlan(r.Context(), p, r.PathValue("planId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleReviewOperationPlan(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	var in ReviewOperationPlanInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.ReviewOperationPlan(r.Context(), p, r.PathValue("planId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleApplyOperationPlan(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	var in ApplyOperationPlanInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.ApplyOperationPlan(r.Context(), p, r.PathValue("planId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleCancelOperationPlan(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	out, err := s.svc.CancelOperationPlan(r.Context(), p, r.PathValue("planId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListRecordApprovals(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	overdue, err := parseOptionalBoolQueryValue(r, "overdue")
	if err != nil {
		writeError(w, err)
		return
	}
	in := QueryRecordApprovalsInput{
		DatasetID:          strings.TrimSpace(r.URL.Query().Get("dataset_id")),
		RecordID:           strings.TrimSpace(r.URL.Query().Get("record_id")),
		AppID:              strings.TrimSpace(r.URL.Query().Get("app_id")),
		BlueprintID:        strings.TrimSpace(r.URL.Query().Get("blueprint_id")),
		ObjectRole:         strings.TrimSpace(r.URL.Query().Get("object_role")),
		Status:             strings.TrimSpace(r.URL.Query().Get("status")),
		Kind:               strings.TrimSpace(r.URL.Query().Get("kind")),
		WorkflowSkillID:    strings.TrimSpace(r.URL.Query().Get("workflow_skill_id")),
		WorkflowInstanceID: strings.TrimSpace(r.URL.Query().Get("workflow_instance_id")),
		BusinessStatus:     strings.TrimSpace(r.URL.Query().Get("business_status")),
		ResultStatus:       strings.TrimSpace(r.URL.Query().Get("result_status")),
		AssignedTo:         strings.TrimSpace(r.URL.Query().Get("assigned_to")),
		CreatedBy:          strings.TrimSpace(r.URL.Query().Get("created_by")),
		ReviewedBy:         strings.TrimSpace(r.URL.Query().Get("reviewed_by")),
		Lane:               strings.TrimSpace(r.URL.Query().Get("lane")),
		Overdue:            overdue,
		Before:             strings.TrimSpace(r.URL.Query().Get("before")),
		BeforeID:           strings.TrimSpace(r.URL.Query().Get("before_id")),
		Limit:              limit,
	}
	out, err := s.svc.ListRecordApprovals(r.Context(), p, in)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	writeResult(w, http.StatusOK, recordApprovalListResponse(out, limit), err)
}

func (s *HTTPServer) handleGetRecordApproval(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetRecordApproval(r.Context(), p, r.PathValue("approvalId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleCreateRecordApproval(w http.ResponseWriter, r *http.Request, p Principal) {
	var in CreateRecordApprovalInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.CreateRecordApproval(r.Context(), p, r.PathValue("datasetId"), r.PathValue("recordId"), in)
	writeResult(w, http.StatusCreated, out, err)
}

func (s *HTTPServer) handleReviewRecordApproval(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	var in ReviewRecordApprovalInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.ReviewRecordApproval(r.Context(), p, r.PathValue("approvalId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleValidateRecord(w http.ResponseWriter, r *http.Request, p Principal) {
	var in ValidateRecordInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.ValidateRecord(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleCreateRecord(w http.ResponseWriter, r *http.Request, p Principal) {
	var in CreateRecordInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.CreateRecord(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusCreated, out, err)
}

func (s *HTTPServer) handleQueryRecords(w http.ResponseWriter, r *http.Request, p Principal) {
	var in QueryRecordsInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.QueryRecords(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusOK, listResponse(out, in), err)
}

func (s *HTTPServer) handleExportRecordsCSV(w http.ResponseWriter, r *http.Request, p Principal) {
	var in QueryRecordsInput
	if !decodeJSON(w, r, &in) {
		return
	}
	datasetID := r.PathValue("datasetId")
	limit, err := normalizeExportLimit(in.Limit, 5000)
	if err != nil {
		writeError(w, err)
		return
	}
	in.Limit = limit
	records, err := s.svc.queryRecordsForExport(r.Context(), p, datasetID, in, 5000)
	if err != nil {
		writeError(w, err)
		return
	}
	fields, err := s.svc.ListFields(r.Context(), p, datasetID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeDownloadHeaders(w, "text/csv; charset=utf-8", `attachment; filename="`+safeFilename(datasetID)+`.csv"`)
	w.WriteHeader(http.StatusOK)
	if err := writeRecordsCSV(w, fields, records); err != nil {
		return
	}
}

func (s *HTTPServer) handleExportRecordsJSONL(w http.ResponseWriter, r *http.Request, p Principal) {
	var in QueryRecordsInput
	if !decodeJSON(w, r, &in) {
		return
	}
	datasetID := r.PathValue("datasetId")
	limit, err := normalizeExportLimit(in.Limit, 5000)
	if err != nil {
		writeError(w, err)
		return
	}
	in.Limit = limit
	records, err := s.svc.queryRecordsForExport(r.Context(), p, datasetID, in, 5000)
	if err != nil {
		writeError(w, err)
		return
	}
	writeDownloadHeaders(w, "application/x-ndjson; charset=utf-8", `attachment; filename="`+safeFilename(datasetID)+`.jsonl"`)
	w.WriteHeader(http.StatusOK)
	_ = writeRecordsJSONL(w, records)
}

func (s *HTTPServer) handleStartCSVExportJob(w http.ResponseWriter, r *http.Request, p Principal) {
	var in StartExportJobInput
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Format = "csv"
	out, err := s.svc.StartExportJob(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusAccepted, out, err)
}

func (s *HTTPServer) handleStartJSONLExportJob(w http.ResponseWriter, r *http.Request, p Principal) {
	var in StartExportJobInput
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Format = "jsonl"
	out, err := s.svc.StartExportJob(r.Context(), p, r.PathValue("datasetId"), in)
	writeResult(w, http.StatusAccepted, out, err)
}

func (s *HTTPServer) handleImportTemplateCSV(w http.ResponseWriter, r *http.Request, p Principal) {
	datasetID := r.PathValue("datasetId")
	fields, err := s.svc.ListFields(r.Context(), p, datasetID)
	if err != nil {
		writeError(w, err)
		return
	}
	records, err := s.svc.QueryRecords(r.Context(), p, datasetID, QueryRecordsInput{Limit: 50})
	if err != nil {
		writeError(w, err)
		return
	}
	writeDownloadHeaders(w, "text/csv; charset=utf-8", `attachment; filename="`+safeFilename(datasetID)+`-import-template.csv"`)
	w.WriteHeader(http.StatusOK)
	if err := writeImportTemplateCSV(w, fields, records); err != nil {
		return
	}
}

func (s *HTTPServer) handleGetRecord(w http.ResponseWriter, r *http.Request, p Principal) {
	out, err := s.svc.GetRecord(r.Context(), p, r.PathValue("datasetId"), r.PathValue("recordId"))
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleGetRelatedRecords(w http.ResponseWriter, r *http.Request, p Principal) {
	in := QueryRelatedRecordsInput{Limit: parseLimit(r.URL.Query().Get("limit")), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}
	out, err := s.svc.GetRelatedRecords(r.Context(), p, r.PathValue("datasetId"), r.PathValue("recordId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleListRecordRevisions(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	in := QueryRecordRevisionsInput{Limit: limit, Before: strings.TrimSpace(r.URL.Query().Get("before")), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}
	out, err := s.svc.QueryRecordRevisions(r.Context(), p, r.PathValue("datasetId"), r.PathValue("recordId"), in)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	writeResult(w, http.StatusOK, recordRevisionListResponse(out, limit), err)
}

func (s *HTTPServer) handleGetRecordTimeline(w http.ResponseWriter, r *http.Request, p Principal) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	in := QueryRecordTimelineInput{Limit: limit, Before: strings.TrimSpace(r.URL.Query().Get("before")), BeforeID: strings.TrimSpace(r.URL.Query().Get("before_id"))}
	out, err := s.svc.GetRecordTimeline(r.Context(), p, r.PathValue("datasetId"), r.PathValue("recordId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleUpdateRecord(w http.ResponseWriter, r *http.Request, p Principal) {
	var in UpdateRecordInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateRecord(r.Context(), p, r.PathValue("datasetId"), r.PathValue("recordId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleRestoreRecord(w http.ResponseWriter, r *http.Request, p Principal) {
	if !requireAdmin(w, p) {
		return
	}
	var in RestoreRecordInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.RestoreRecord(r.Context(), p, r.PathValue("datasetId"), r.PathValue("recordId"), in)
	writeResult(w, http.StatusOK, out, err)
}

func (s *HTTPServer) handleDeleteRecord(w http.ResponseWriter, r *http.Request, p Principal) {
	err := s.svc.DeleteRecord(r.Context(), p, r.PathValue("datasetId"), r.PathValue("recordId"))
	writeResult(w, http.StatusOK, map[string]string{"status": "deleted"}, err)
}

func (s *HTTPServer) withAuth(next func(http.ResponseWriter, *http.Request, Principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(auth, "Bearer ") {
			writeUnauthorized(w)
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		policy, ok := s.matchAPIKey(token)
		if !ok {
			if dbPolicy, err := s.svc.FindAPIKeyPolicyBySecret(r.Context(), token); err == nil {
				policy = dbPolicy
				ok = true
			}
		}
		if !ok {
			if sessionPrincipal, err := s.svc.FindAdminSessionBySecret(r.Context(), token); err == nil {
				next(w, r, *sessionPrincipal)
				return
			}
		}
		matchedServiceToken := false
		if !ok && s.token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) == 1 {
			matchedServiceToken = true
		}
		if !ok && !matchedServiceToken {
			writeUnauthorized(w)
			return
		}
		tenantID := strings.TrimSpace(r.Header.Get("X-MaClaw-Tenant-ID"))
		if tenantID == "" {
			tenantID = "default"
		}
		userID := strings.TrimSpace(r.Header.Get("X-MaClaw-User-ID"))
		if userID == "" {
			userID = "maclaw"
		}
		role := strings.ToLower(strings.TrimSpace(r.Header.Get("X-MaClaw-Role")))
		if role == "" {
			role = "data_user"
		}
		p := Principal{TenantID: tenantID, UserID: userID, Role: role}
		if matchedServiceToken {
			adminScope := strings.ToLower(strings.TrimSpace(r.Header.Get("X-MaClaw-Admin-Scope")))
			if adminScope == "global" || adminScope == "tenant" {
				p.AdminScope = adminScope
			}
		}
		if policy != nil {
			p.APIKeyID = policy.ID
			p.Policy = policy
			if policy.TenantID != "" {
				p.TenantID = policy.TenantID
			}
			if policy.UserID != "" {
				p.UserID = policy.UserID
			}
			if policy.Role != "" {
				p.Role = policy.Role
			}
		}
		if !authorizeHTTPRequest(r, p) {
			writeError(w, ErrForbidden)
			return
		}
		if p.Policy != nil {
			s.svc.TouchAPIKeyPolicyUse(r.Context(), *p.Policy, clientIP(r), r.UserAgent())
		}
		next(w, r, p)
	}
}

func (s *HTTPServer) matchAPIKey(token string) (*APIKeyPolicy, bool) {
	for _, item := range s.apiKeys {
		if item.Key == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(item.Key)) == 1 {
			clone := item
			clone.Key = ""
			return &clone, true
		}
	}
	return nil, false
}

func clientIP(r *http.Request) string {
	for _, header := range []string{"X-Forwarded-For", "X-Real-IP"} {
		if raw := strings.TrimSpace(r.Header.Get(header)); raw != "" {
			if idx := strings.Index(raw, ","); idx >= 0 {
				raw = strings.TrimSpace(raw[:idx])
			}
			return raw
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func authorizeHTTPRequest(r *http.Request, p Principal) bool {
	if p.Policy == nil {
		return true
	}
	if domain := strings.TrimSpace(r.PathValue("domain")); domain != "" && !principalCanUseDomain(p, domain) {
		return false
	}
	if datasetID := requestDatasetID(r); datasetID != "" && !principalCanUseDataset(p, datasetID) {
		return false
	}
	if actionID := strings.TrimSpace(r.PathValue("actionId")); actionID != "" && strings.Contains(r.URL.Path, "/business-actions/") && !principalCanUseAction(p, actionID) {
		return false
	}
	if viewID := strings.TrimSpace(r.PathValue("viewId")); viewID != "" && strings.Contains(r.URL.Path, "/views/") && !principalCanUseView(p, viewID) {
		return false
	}
	if reportID := strings.TrimSpace(r.PathValue("reportId")); reportID != "" && strings.Contains(r.URL.Path, "/reports/") && !principalCanUseReport(p, reportID) {
		return false
	}
	if dashboardID := strings.TrimSpace(r.PathValue("dashboardId")); dashboardID != "" && strings.Contains(r.URL.Path, "/dashboards/") && !principalCanUseDashboard(p, dashboardID) {
		return false
	}
	if templateID := strings.TrimSpace(r.PathValue("templateId")); templateID != "" && !principalCanUseDataset(p, templateID) {
		return false
	}
	return true
}

func requestDatasetID(r *http.Request) string {
	for _, value := range []string{r.PathValue("datasetId"), strings.TrimSpace(r.URL.Query().Get("dataset_id"))} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return false
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return false
	}
	return true
}

func writeResult(w http.ResponseWriter, status int, out any, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, status, out)
}

func writeError(w http.ResponseWriter, err error) {
	status := httpStatusForError(err)
	if status == http.StatusUnauthorized {
		writeUnauthorized(w)
		return
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="MaClawDataSrv"`)
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": ErrUnauthorized.Error()})
}

func httpStatusForError(err error) int {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, ErrUnauthorized):
		status = http.StatusUnauthorized
	case errors.Is(err, ErrForbidden):
		status = http.StatusForbidden
	case errors.Is(err, ErrAdminNotFound), errors.Is(err, ErrSessionNotFound), errors.Is(err, ErrDatasetNotFound), errors.Is(err, ErrRecordNotFound), errors.Is(err, ErrBackupNotFound):
		status = http.StatusNotFound
	case errors.Is(err, ErrAlreadyExists):
		status = http.StatusConflict
	case errors.Is(err, ErrInvalidInput):
		status = http.StatusBadRequest
	}
	return status
}

func requireAdmin(w http.ResponseWriter, p Principal) bool {
	if principalCanAdmin(p) {
		return true
	}
	writeError(w, ErrForbidden)
	return false
}

func writeJSON(w http.ResponseWriter, status int, out any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(out)
}

func writeDownloadHeaders(w http.ResponseWriter, contentType, contentDisposition string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", contentDisposition)
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func parseLimit(raw string) int {
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return limit
}

func parseBoolQueryValue(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("%w: boolean query parameter must be true, false, 1, or 0", ErrInvalidInput)
	}
}

func parseOptionalBoolQueryValue(r *http.Request, name string) (bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return false, nil
	}
	value, err := parseBoolQueryValue(raw)
	if err != nil {
		return false, fmt.Errorf("%w: %s must be true, false, 1, or 0", ErrInvalidInput, name)
	}
	return value, nil
}

func effectiveLimit(limit, fallback, max int) int {
	if fallback <= 0 {
		fallback = 100
	}
	if max <= 0 {
		max = 500
	}
	if limit <= 0 || limit > max {
		return fallback
	}
	return limit
}

func listProbeLimit(limit, fallback, max int) int {
	return effectiveLimit(limit, fallback, max) + 1
}

func accessPolicyPresetListResponse(items []AccessPolicyPreset, limit int) ListResponse[AccessPolicyPreset] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[AccessPolicyPreset]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		out.NextBeforeID = items[len(items)-1].ID
	}
	return out
}

func businessDomainListResponse(items []BusinessDomainCatalog, limit int) ListResponse[BusinessDomainCatalog] {
	limit = effectiveLimit(limit, 100, 500)
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	out := ListResponse[BusinessDomainCatalog]{Items: items, Limit: limit, HasMore: hasMore}
	if out.HasMore && len(items) > 0 {
		out.NextBeforeID = items[len(items)-1].Domain
	}
	return out
}

func businessObjectListResponse(items []BusinessObjectCatalog, limit int) ListResponse[BusinessObjectCatalog] {
	limit = effectiveLimit(limit, 100, 500)
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	out := ListResponse[BusinessObjectCatalog]{Items: items, Limit: limit, HasMore: hasMore}
	if out.HasMore && len(items) > 0 {
		out.NextBeforeID = businessObjectCursorKey(items[len(items)-1])
	}
	return out
}

func appInstallationListResponse(items []AppInstallation, limit int) ListResponse[AppInstallation] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[AppInstallation]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		out.NextBefore = last.UpdatedAt
		out.NextBeforeID = last.ID
	}
	return out
}
func templateListResponse(items []DatasetTemplate, limit int) ListResponse[DatasetTemplate] {
	limit = effectiveLimit(limit, 100, 500)
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	out := ListResponse[DatasetTemplate]{Items: items, Limit: limit, HasMore: hasMore}
	if out.HasMore && len(items) > 0 {
		out.NextBeforeID = items[len(items)-1].ID
	}
	return out
}

func businessViewListResponse(items []BusinessViewDefinition, limit int) ListResponse[BusinessViewDefinition] {
	limit = effectiveLimit(limit, 100, 500)
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	out := ListResponse[BusinessViewDefinition]{Items: items, Limit: limit, HasMore: hasMore}
	if out.HasMore && len(items) > 0 {
		out.NextBeforeID = items[len(items)-1].ID
	}
	return out
}

func qualityCheckListResponse(items []QualityCheckDefinition, limit int) ListResponse[QualityCheckDefinition] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[QualityCheckDefinition]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		out.NextBeforeID = items[len(items)-1].ID
	}
	return out
}

func dashboardListResponse(items []DashboardDefinition, limit int) ListResponse[DashboardDefinition] {
	limit = effectiveLimit(limit, 100, 500)
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	out := ListResponse[DashboardDefinition]{Items: items, Limit: limit, HasMore: hasMore}
	if out.HasMore && len(items) > 0 {
		out.NextBeforeID = items[len(items)-1].ID
	}
	return out
}

func reportListResponse(items []ReportDefinition, limit int) ListResponse[ReportDefinition] {
	limit = effectiveLimit(limit, 100, 500)
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	out := ListResponse[ReportDefinition]{Items: items, Limit: limit, HasMore: hasMore}
	if out.HasMore && len(items) > 0 {
		out.NextBeforeID = items[len(items)-1].ID
	}
	return out
}
func eventContractListResponse(items []EventContract, limit int) ListResponse[EventContract] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[EventContract]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		out.NextBeforeID = items[len(items)-1].ID
	}
	return out
}
func businessActionListResponse(items []BusinessAction, limit int) ListResponse[BusinessAction] {
	limit = effectiveLimit(limit, 100, 500)
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	out := ListResponse[BusinessAction]{Items: items, Limit: limit, HasMore: hasMore}
	if out.HasMore && len(items) > 0 {
		out.NextBeforeID = items[len(items)-1].ID
	}
	return out
}
func fieldListResponse(items []FieldDefinition, limit int) ListResponse[FieldDefinition] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[FieldDefinition]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		out.NextBefore = last.UpdatedAt.Format(time.RFC3339Nano)
		out.NextBeforeID = last.ID
	}
	return out
}
func relationshipListResponse(items []DatasetRelationship, limit int) ListResponse[DatasetRelationship] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[DatasetRelationship]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		out.NextBeforeID = relationshipCursorKey(items[len(items)-1])
	}
	return out
}
func businessRuleListResponse(items []BusinessRuleDefinition, limit int) ListResponse[BusinessRuleDefinition] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[BusinessRuleDefinition]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		out.NextBeforeID = items[len(items)-1].ID
	}
	return out
}
func datasetListResponse(items []Dataset, limit int) ListResponse[Dataset] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[Dataset]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		out.NextBefore = last.UpdatedAt.Format(time.RFC3339Nano)
		out.NextBeforeID = last.ID
	}
	return out
}
func apiKeyPolicyListResponse(items []APIKeyPolicyRecord, limit int) ListResponse[APIKeyPolicyRecord] {
	limit = effectiveLimit(limit, 200, 500)
	out := ListResponse[APIKeyPolicyRecord]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		out.NextBefore = last.UpdatedAt.Format(time.RFC3339Nano)
		out.NextBeforeID = last.ID
	}
	return out
}
func externalConnectorListResponse(items []ExternalConnector, limit int) ListResponse[ExternalConnector] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[ExternalConnector]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		out.NextBefore = last.UpdatedAt.Format(time.RFC3339Nano)
		out.NextBeforeID = last.ID
	}
	return out
}

func connectorHealthListResponse(items []ConnectorHealth, limit int) ListResponse[ConnectorHealth] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[ConnectorHealth]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		out.NextBefore = last.Connector.UpdatedAt.Format(time.RFC3339Nano)
		out.NextBeforeID = last.Connector.ID
	}
	return out
}
func connectorSyncRunListResponse(items []ConnectorSyncRun, limit int) ListResponse[ConnectorSyncRun] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[ConnectorSyncRun]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		out.NextBefore = firstNonZeroTime(last.FinishedAt, last.StartedAt).Format(time.RFC3339Nano)
		out.NextBeforeID = last.ID
	}
	return out
}
func backupListResponse(items []BackupInfo, limit int) ListResponse[BackupInfo] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[BackupInfo]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		out.NextBefore = last.CreatedAt.Format(time.RFC3339Nano)
		out.NextBeforeID = last.ID
	}
	return out
}
func schemaProposalListResponse(items []SchemaProposal, limit int) ListResponse[SchemaProposal] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[SchemaProposal]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		out.NextBefore = last.CreatedAt.Format(time.RFC3339Nano)
		out.NextBeforeID = last.ID
	}
	return out
}
func recordRevisionListResponse(items []RecordRevision, limit int) ListResponse[RecordRevision] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[RecordRevision]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		out.NextBefore = last.CreatedAt.Format(time.RFC3339Nano)
		if last.RowID > 0 {
			out.NextBeforeID = fmt.Sprint(last.RowID)
		} else {
			out.NextBeforeID = last.ID
		}
	}
	return out
}
func qualityRunListResponse(items []QualityCheckResult, limit int) ListResponse[QualityCheckResult] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[QualityCheckResult]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		out.NextBefore = last.CreatedAt.Format(time.RFC3339Nano)
		out.NextBeforeID = last.ID
	}
	return out
}
func importJobListResponse(items []ImportJob, limit int) ListResponse[ImportJob] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[ImportJob]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		out.NextBefore = last.CreatedAt.Format(time.RFC3339Nano)
		out.NextBeforeID = last.ID
	}
	return out
}

func exportJobListResponse(items []ExportJob, limit int) ListResponse[ExportJob] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[ExportJob]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		out.NextBefore = last.CreatedAt.Format(time.RFC3339Nano)
		out.NextBeforeID = last.ID
	}
	return out
}

func operationPlanListResponse(items []OperationPlan, limit int) ListResponse[OperationPlan] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[OperationPlan]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		out.NextBefore = last.CreatedAt.Format(time.RFC3339Nano)
		out.NextBeforeID = last.ID
	}
	return out
}

func recordApprovalListResponse(items []RecordApproval, limit int) ListResponse[RecordApproval] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[RecordApproval]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		out.NextBefore = last.CreatedAt.Format(time.RFC3339Nano)
		out.NextBeforeID = last.ID
	}
	return out
}
func dataEventListResponse(items []DataEventLog, limit int) ListResponse[DataEventLog] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[DataEventLog]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		out.NextBefore = last.AppliedAt.Format(time.RFC3339Nano)
		out.NextBeforeID = last.ID
	}
	return out
}

func dataEventDeadLetterListResponse(items []DataEventDeadLetter, limit int) ListResponse[DataEventDeadLetter] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[DataEventDeadLetter]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		out.NextBefore = last.CreatedAt.Format(time.RFC3339Nano)
		out.NextBeforeID = last.ID
	}
	return out
}
func auditLogListResponse(items []AuditLog, limit int) ListResponse[AuditLog] {
	limit = effectiveLimit(limit, 100, 500)
	out := ListResponse[AuditLog]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		out.NextBefore = last.CreatedAt.Format(time.RFC3339Nano)
		out.NextBeforeID = last.ID
	}
	return out
}
func listResponse(items []Record, in QueryRecordsInput) ListResponse[Record] {
	limit := effectiveLimit(in.Limit, 100, 500)
	out := ListResponse[Record]{Items: items, Limit: limit, HasMore: len(items) == limit}
	if out.HasMore {
		out.NextBefore, out.NextBeforeID = recordPageCursor(items, in.Sort)
	}
	return out
}

func writeRecordsCSV(w io.Writer, fields []FieldDefinition, records []Record) error {
	fieldKeys := make([]string, 0, len(fields))
	known := map[string]struct{}{}
	for _, field := range fields {
		key := strings.TrimSpace(field.Key)
		if key == "" {
			continue
		}
		fieldKeys = append(fieldKeys, key)
		known[key] = struct{}{}
	}
	extras := map[string]struct{}{}
	for _, record := range records {
		for key := range record.Data {
			if _, ok := known[key]; !ok {
				extras[key] = struct{}{}
			}
		}
	}
	extraKeys := make([]string, 0, len(extras))
	for key := range extras {
		extraKeys = append(extraKeys, key)
	}
	sort.Strings(extraKeys)
	header := append([]string{"id", "title", "tags", "source_id", "created_at", "updated_at"}, fieldKeys...)
	header = append(header, extraKeys...)
	writer := csv.NewWriter(w)
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, record := range records {
		row := []string{
			record.ID,
			record.Title,
			strings.Join(record.Tags, "|"),
			record.SourceID,
			record.CreatedAt.Format(time.RFC3339Nano),
			record.UpdatedAt.Format(time.RFC3339Nano),
		}
		for _, key := range fieldKeys {
			row = append(row, csvCell(record.Data[key]))
		}
		for _, key := range extraKeys {
			row = append(row, csvCell(record.Data[key]))
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func writeRecordsJSONL(w io.Writer, records []Record) error {
	encoder := json.NewEncoder(w)
	for _, record := range records {
		out := BatchRecordInput{
			ID:       record.ID,
			Title:    record.Title,
			Tags:     record.Tags,
			Data:     record.Data,
			SourceID: record.SourceID,
		}
		if err := encoder.Encode(out); err != nil {
			return err
		}
	}
	return nil
}

func writeImportTemplateCSV(w io.Writer, fields []FieldDefinition, records []Record) error {
	header := []string{"id", "title", "tags", "source_id"}
	seen := map[string]struct{}{}
	for _, key := range header {
		seen[key] = struct{}{}
	}
	for _, field := range fields {
		key := strings.TrimSpace(field.Key)
		if key != "" {
			header = append(header, key)
			seen[key] = struct{}{}
		}
	}
	extra := make([]string, 0)
	for _, record := range records {
		for key := range record.Data {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	header = append(header, extra...)
	writer := csv.NewWriter(w)
	if err := writer.Write(header); err != nil {
		return err
	}
	writer.Flush()
	return writer.Error()
}

func csvCell(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return fmt.Sprint(value)
		}
		return string(data)
	}
}

func safeFilename(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "records"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", `"`, "_", "<", "_", ">", "_", "|", "_")
	return replacer.Replace(value)
}

// --- Rate Limiting ---

// httpRateLimiter provides per-IP token-bucket rate limiting for sensitive endpoints.
type httpRateLimiter struct {
	mu        sync.Mutex
	buckets   map[string]*rateBucket
	callCount int64
}

type rateBucket struct {
	tokens    float64
	lastCheck time.Time
}

const (
	rateLimitBurst       = 10  // max burst per IP
	rateLimitPerSec      = 2.0 // sustained requests per second per IP
	rateLimitMaxBuckets  = 1000
	rateLimitGCInterval  = 100 // run GC every N allow() calls
	rateLimitIdleTimeout = 5 * time.Minute
)

func newHTTPRateLimiter() *httpRateLimiter {
	return &httpRateLimiter{buckets: make(map[string]*rateBucket)}
}

func (rl *httpRateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.callCount++
	now := time.Now()
	if rl.callCount%rateLimitGCInterval == 0 || len(rl.buckets) > rateLimitMaxBuckets {
		rl.evictIdle(now)
	}
	b, ok := rl.buckets[ip]
	if !ok {
		b = &rateBucket{tokens: float64(rateLimitBurst), lastCheck: now}
		rl.buckets[ip] = b
	}
	elapsed := now.Sub(b.lastCheck).Seconds()
	b.tokens += elapsed * rateLimitPerSec
	if b.tokens > float64(rateLimitBurst) {
		b.tokens = float64(rateLimitBurst)
	}
	b.lastCheck = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (rl *httpRateLimiter) evictIdle(now time.Time) {
	threshold := now.Add(-rateLimitIdleTimeout)
	for ip, b := range rl.buckets {
		if b.lastCheck.Before(threshold) {
			delete(rl.buckets, ip)
		}
	}
}

func (s *HTTPServer) withRateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := extractClientIP(r)
		if !s.rateLimiter.allow(ip) {
			w.Header().Set("Retry-After", "1")
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"too many requests","retry_after":"1s"}`))
			return
		}
		next(w, r)
	}
}

// extractClientIP returns the client IP for rate limiting purposes.
// It uses RemoteAddr (TCP-level, unforgeable) by default. X-Forwarded-For and
// X-Real-IP are only used as fallback when RemoteAddr cannot be parsed (e.g.
// Unix socket), since these headers can be spoofed by clients not behind a
// trusted reverse proxy.
func extractClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	// Fallback for non-TCP transports (Unix sockets, etc.)
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		if ip, _, ok := strings.Cut(forwarded, ","); ok {
			return strings.TrimSpace(ip)
		}
		return forwarded
	}
	return r.RemoteAddr
}
