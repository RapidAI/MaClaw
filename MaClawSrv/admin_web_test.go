package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestAdminWebServesEmbeddedShell(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	assertAdminSecurityHeaders(t, w.Result())
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "MaClawSrv") {
		t.Fatalf("admin shell = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/app.js", nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	assertAdminSecurityHeaders(t, w.Result())
	body := w.Body.String()
	if strings.Contains(body, "style=") || strings.Contains(body, "\uFFFD") {
		t.Fatalf("admin app asset contains CSP-hostile inline style or replacement char")
	}
	if w.Code != http.StatusOK || !strings.Contains(body, "/api/v1/admin/bootstrap/status") || !strings.Contains(body, "createTenant") || !strings.Contains(body, "delete-check") || !strings.Contains(body, "createCredential") || !strings.Contains(body, "rotate-secret") || !strings.Contains(body, "createSnapshot") || !strings.Contains(body, "/api/v1/admin/audit-events") || !strings.Contains(body, "/api/v1/admin/security/summary") || !strings.Contains(body, "/api/v1/admin/security/risk-events") || !strings.Contains(body, "riskEventsTable") || !strings.Contains(body, "sandboxOut") || !strings.Contains(body, "t(\"view\")") || !strings.Contains(body, "t(\"empty\")") || !strings.Contains(body, "riskCountChips") || !strings.Contains(body, "riskKindCounts") || !strings.Contains(body, "riskSeverityCounts") || !strings.Contains(body, "bindRiskCountChips") || !strings.Contains(body, "data-risk-kind") || !strings.Contains(body, "data-risk-severity") || !strings.Contains(body, "loadRiskEventsFromFilters") || !strings.Contains(body, "validRiskTimeRange") || !strings.Contains(body, "validRiskLimit") || !strings.Contains(body, "invalidRiskLimit") || !strings.Contains(body, "generatedAt") || !strings.Contains(body, "risks.generated_at") || !strings.Contains(body, "risks.filters") || !strings.Contains(body, "limit=${limit}") || !strings.Contains(body, "min=\"1\"") || !strings.Contains(body, "max=\"500\"") || !strings.Contains(body, "riskTimeRangeInvalid") || !strings.Contains(body, "riskSeverity") || !strings.Contains(body, "riskKind") || !strings.Contains(body, "riskKindOptions") || !strings.Contains(body, "riskKindOptions(risks.kind_counts)") || !strings.Contains(body, "riskEventsList") || !strings.Contains(body, "loadRisks") || !strings.Contains(body, "clearRiskFilters") || !strings.Contains(body, "setRiskTimePreset") || !strings.Contains(body, "data-risk-preset") || !strings.Contains(body, "toLocalDateTimeInput") || !strings.Contains(body, "${riskFilterSummary(security)}") || !strings.Contains(body, "riskFilterSummary") || !strings.Contains(body, "riskFilterStatus") || !strings.Contains(body, "all risks") || !strings.Contains(body, "goToRiskOps") || !strings.Contains(body, "pendingRiskFilter") || !strings.Contains(body, "applyPendingRiskFilter") || !strings.Contains(body, "/api/v1/admin/support-bundle") || !strings.Contains(body, "serviceSupportBundle") || !strings.Contains(body, "serviceSupportBundleDownload") || !strings.Contains(body, "/api/v1/admin/runtime/gc") || !strings.Contains(body, "runRuntimeGC") || !strings.Contains(body, "/api/v1/admin/runtime/goroutines") || !strings.Contains(body, "viewGoroutines") || !strings.Contains(body, "downloadGoroutines") || !strings.Contains(body, "/api/v1/admin/runtime/profiles/heap") || !strings.Contains(body, "viewHeapProfile") || !strings.Contains(body, "downloadHeapProfile") || !strings.Contains(body, "/api/v1/admin/jobs") || !strings.Contains(body, "last_sandbox_report") || !strings.Contains(body, "data-job-cancel") || !strings.Contains(body, "/api/v1/admin/logs/errors/recent") || !strings.Contains(body, "/api/v1/admin/logs/search") || !strings.Contains(body, "/download?${q}") || !strings.Contains(body, "downloadLog") || !strings.Contains(body, "rotateLog") || !strings.Contains(body, "/rotate?confirm=true") || !strings.Contains(body, "searchAllLogs") || !strings.Contains(body, "recentLogTable") || !strings.Contains(body, "logIncludeWarn") || !strings.Contains(body, "logQuery") || !strings.Contains(body, "logLineTable") || !strings.Contains(body, "sandboxReportsTable") || !strings.Contains(body, "sandboxEventsTable") || !strings.Contains(body, "loadSandboxEventsFromFilters") || !strings.Contains(body, "sandboxEventStatus") || !strings.Contains(body, "sandboxProfilesTable") || !strings.Contains(body, "data-sandbox-profile-delete") || !strings.Contains(body, "deleteSandboxProfileConfirm") || !strings.Contains(body, "/api/v1/admin/sandbox/profiles") || !strings.Contains(body, "/api/v1/admin/sandbox/events") || !strings.Contains(body, "/api/v1/admin/sandbox/support-bundle") || !strings.Contains(body, "sandboxSupportBundle") || !strings.Contains(body, "sandboxSupportBundleView") || !strings.Contains(body, "sandboxSupportBundleDownload") || !strings.Contains(body, "support-bundle?download=true") || !strings.Contains(body, "b.security_risks") || !strings.Contains(body, "b.redactions") || !strings.Contains(body, "b.recent_log_errors") || !strings.Contains(body, "t(\"redactions\")") || !strings.Contains(body, "t(\"dataRoot\")") || !strings.Contains(body, "t(\"overviewHint\")") || !strings.Contains(body, "t(\"sandboxHint\")") || !strings.Contains(body, "t(\"logsHint\")") || !strings.Contains(body, "t(\"sources\")") || !strings.Contains(body, "t(\"open\")") || !strings.Contains(body, "t(\"lines\")") || !strings.Contains(body, "t(\"createTenant\")") || !strings.Contains(body, "t(\"knowledgeAccess\")") || !strings.Contains(body, "t(\"skillSources\")") || !strings.Contains(body, "t(\"restartRequired\")") || !strings.Contains(body, "t(\"warnings\")") || !strings.Contains(body, "t(\"configHint\")") || !strings.Contains(body, "t(\"tenantsHint\")") || !strings.Contains(body, "t(\"accountsHint\")") || !strings.Contains(body, "t(\"knowledgeHint\")") || !strings.Contains(body, "t(\"opsHint\")") || !strings.Contains(body, "t(\"manualApply\")") || !strings.Contains(body, "t(\"willExecute\")") || !strings.Contains(body, "loadLocales") || !strings.Contains(body, "/api/v1/admin/i18n/locales") || !strings.Contains(body, "state.locales") || !strings.Contains(body, "out.default_locale") || !strings.Contains(body, "localeOptions") || !strings.Contains(body, "state.me?.admin?.locale") || !strings.Contains(body, "b.data_root_name") || !strings.Contains(body, "b.data_root_redacted") || !strings.Contains(body, "bundle.security_risks?.recent") || !strings.Contains(body, "riskFilterSummary(risks)") || !strings.Contains(body, "data-sandbox-report-delete") || !strings.Contains(body, "deleteSandboxReportConfirm") || !strings.Contains(body, "landlock") || !strings.Contains(body, "/api/v1/admin/service-config/environment") || !strings.Contains(body, "/api/v1/admin/service-config/diff") || !strings.Contains(body, "clearCfgDraft") || !strings.Contains(body, "draft?confirm=true") || !strings.Contains(body, "buildConfigValues") || !strings.Contains(body, "cfgUse_") || !strings.Contains(body, "f.sensitive?\"\"") || !strings.Contains(body, "/api/v1/admin/knowledge-access/cross-tenant") || !strings.Contains(body, "/api/v1/admin/skill-sources/global") || !strings.Contains(body, "clearTenantKnowledge") || !strings.Contains(body, "/api/v1/admin/sandbox/install") || !strings.Contains(body, "installSandboxRun") || !strings.Contains(body, "/api/v1/admin/sandbox/switch") || !strings.Contains(body, "/api/v1/admin/sandbox/rollback") || !strings.Contains(body, "rollbackSandbox") || !strings.Contains(body, "sandboxRollbackConfirm") || !strings.Contains(body, "SNAPSHOT SECRETS") || !strings.Contains(body, "EXPORT SECRETS") || !strings.Contains(body, "IMPORT STATE") || !strings.Contains(body, "RESTORE SNAPSHOT") || !strings.Contains(body, "data-snapshot-restore-run") || !strings.Contains(body, "INSTALL SANDBOX") || !strings.Contains(body, "DISABLE SANDBOX") || !strings.Contains(body, "clearKnowledgeConfirm") || !strings.Contains(body, "deleteTenantConfirm") || !strings.Contains(body, "useSecret") || !strings.Contains(body, "auditActorUser") || !strings.Contains(body, "actor_user_id") || strings.Contains(body, "confirm_unsafe:mode===\"none\"") {
		t.Fatalf("admin app asset missing expected admin API wiring = %d body = %s", w.Code, body)
	}
}

func assertAdminSecurityHeaders(t *testing.T, resp *http.Response) {
	t.Helper()
	checks := map[string]string{
		"Cache-Control":          "no-store",
		"Referrer-Policy":        "no-referrer",
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	}
	for name, want := range checks {
		if got := resp.Header.Get(name); got != want {
			t.Fatalf("%s header = %q, want %q", name, got, want)
		}
	}
	csp := resp.Header.Get("Content-Security-Policy")
	for _, part := range []string{"default-src 'self'", "frame-ancestors 'none'", "base-uri 'none'"} {
		if !strings.Contains(csp, part) {
			t.Fatalf("Content-Security-Policy missing %q in %q", part, csp)
		}
	}
	if strings.Contains(csp, "'unsafe-inline'") {
		t.Fatalf("Content-Security-Policy should not allow inline script/style: %q", csp)
	}
}

func TestAdminWebServesVersionedAssets(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	for _, tc := range []struct {
		path   string
		marker string
	}{
		{path: "/admin/styles.css?v=light-admin-20260609", marker: "Light-first admin system"},
		{path: "/admin/app.js?v=light-admin-20260609", marker: "/api/v1/admin/bootstrap/status"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		assertAdminSecurityHeaders(t, w.Result())
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), tc.marker) {
			t.Fatalf("versioned admin asset %s = %d body = %s", tc.path, w.Code, w.Body.String())
		}
	}
}

func TestAdminWebIncludesTenantAuditShortcuts(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/admin/app.js", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	body := w.Body.String()
	for _, needle := range []string{"data-tenant-audit", "data-user-audit", "showTenantAudit", "/api/v1/admin/audit-events?${q}"} {
		if !strings.Contains(body, needle) {
			t.Fatalf("admin web missing %s", needle)
		}
	}
}

func TestAdminWebTenantUserPageContracts(t *testing.T) {
	bodyBytes, err := fs.ReadFile(adminWebFS, "admin_web/app.js")
	if err != nil {
		t.Fatalf("read admin app: %v", err)
	}
	body := string(bodyBytes)
	for _, needle := range []string{
		`function userTenantRows(users,tenants)`,
		`tenant_name:displayWithID`,
		`function actionKey(...parts)`,
		`function actionParts(value)`,
		`function userSearchColumns(){ return ["tenant_name","name","email","status","delete_protected","id"]; }`,
		`function tenantUserColumns(){ return ["name","email","status","delete_protected","id"]; }`,
		`function renderTenantUserSearch(rows)`,
		`tenantUserSearchResults`,
		`userSearchQuery`,
		`data-tenant-users`,
		`function showTenantUsersModal(tenant,rows)`,
		`tenant-users-modal`,
		`data-user-credentials`,
		`apiCredentials`,
		`credentialHelp`,
		`showAdminResult(t("deleteCheck")`,
		`showAdminResult(t("retirePlan")`,
		`document.querySelectorAll(".admin-result-modal").forEach`,
		`bindTenantActions(); applyOwnerGuards(); enhanceA11y();`,
		`bindCredentialActions(); applyOwnerGuards(); enhanceA11y();`,
		`document.body.appendChild(overlay); enhanceA11y();`,
		`bindRiskEventActions(risks.items||[]); enhanceA11y();`,
		`showInlineSummary("sandboxOut",t("events"),events); enhanceA11y();`,
		`clearTransientAdminModals`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("admin tenant/user page missing marker %s", needle)
		}
	}
	for _, forbidden := range []string{
		`id="tenantOut"`,
		`["tenant_id","id","name","email","status","delete_protected"]`,
		`.content:has(#createTenant)`,
		`data-user-credentials="${esc(x.tenant_id)}:${esc(x.id)}"`,
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("admin tenant/user page still exposes deprecated marker %s", forbidden)
		}
	}
}

func TestAdminWebClientConfigProxyScopeControls(t *testing.T) {
	bodyBytes, err := fs.ReadFile(adminWebFS, "admin_web/app.js")
	if err != nil {
		t.Fatalf("read admin app: %v", err)
	}
	body := string(bodyBytes)
	for _, needle := range []string{
		"clientProxyCodingTools",
		"default_proxy_scope_coding_tools",
		"readClientConfigFormBase",
		"function renderClientSearchProviderPicker",
		"data-client-search-provider",
		"data-client-search-key",
		"function readClientSearchProviders",
		"function renderClientSecurityModeTabs",
		"data-client-security-mode",
		"function bindClientSecurityModeTabs",
		"securityGuardrails",
		"cfg.web_search_providers=providers",
		"cfg.web_search_current_provider=selectedProvider?.type",
		`cfg.security_policy_mode=$("clientSecurityMode").value`,
		"searchProviderHelp",
		`Object.assign(en, { experienceDefaults: "User Interface" })`,
		`Object.assign(zh, { experienceDefaults: "用户界面" })`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("admin client config missing proxy scope marker %s", needle)
		}
	}
	for _, stale := range []string{
		`id="clientSearchProviders"`,
		`JSON.parse($("clientSearchProviders").value`,
		`t("webSearchProviders")}</label><textarea`,
		`id="clientUIMode"`,
		`id="clientWorkingDir"`,
		`id="clientVectorSearch"`,
		`id="clientASR"`,
		`id="clientTTS"`,
		`id="clientIMProgress"`,
		`cfg.ui_mode=$("clientUIMode").value`,
		`cfg.working_directory=$("clientWorkingDir").value.trim()`,
		`cfg.vector_search_enabled=$("clientVectorSearch").checked`,
		`cfg.asr_enabled=$("clientASR").checked`,
		`cfg.tts_enabled=$("clientTTS").checked`,
		`cfg.im_progress_nudge_enabled=$("clientIMProgress").checked`,
		`> IM progress nudges</label>`,
		`<select id="clientSecurityMode">`,
		`<label for="clientSecurityMode">${t("mode")}</label><select`,
	} {
		if strings.Contains(body, stale) {
			t.Fatalf("admin client config should not expose retired raw/experience control marker %s", stale)
		}
	}
}

func TestAdminWebProductizedAdminPanels(t *testing.T) {
	bodyBytes, err := fs.ReadFile(adminWebFS, "admin_web/app.js")
	if err != nil {
		t.Fatalf("read admin app: %v", err)
	}
	body := string(bodyBytes)
	for _, needle := range []string{
		`<span class="nav-icon">${navIcons[id]||""}</span><span class="nav-label">${esc(t(id))}</span>`,
		`moreOptions: "More settings"`,
		`manualRules: "Custom rules"`,
		`sandboxSecondaryHint`,
		`snapshotsHint`,
		`importPreviewHint`,
		`<details class="advanced-disclosure action-disclosure">`,
		`showInlineSummary("sandboxOut",t("save")`,
		`showInlineSummary("sandboxOut",t("profile")`,
		`showAdminResult(t("events"),byId.get(b.dataset.sandboxEvent)||{})`,
		`sectionHead(t("import"), t("importPreviewHint")`,
		`sectionHead(t("snapshots"), t("snapshotsHint")`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("admin productized UI missing marker %s", needle)
		}
	}
	for _, forbidden := range []string{
		`<span class="nav-meta">`,
		`id="clientCfgOut"`,
		`$("clientCfgOut").textContent=pretty`,
		"$(\"sandboxOut\").innerHTML=`<pre>${esc(pretty(events))}</pre>`",
		"$(\"sandboxOut\").innerHTML=`<pre>${esc(pretty(out))}</pre>`",
		"$(\"sandboxOut\").innerHTML=`<pre>${esc(pretty(await api(",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("admin productized UI still exposes raw marker %s", forbidden)
		}
	}
}

func TestAdminWebLocalizesAdminTableColumns(t *testing.T) {
	bodyBytes, err := fs.ReadFile(adminWebFS, "admin_web/app.js")
	if err != nil {
		t.Fatalf("read admin app: %v", err)
	}
	body := string(bodyBytes)
	for _, needle := range []string{
		`username:"username"`,
		`display_name:"displayName"`,
		`role:"role"`,
		`status:"status"`,
		`locale:"language"`,
		`last_login_at:"lastLoginAt"`,
		`active:"activeState"`,
		`created_at:"createdAt"`,
		`expires_at:"expiresAt"`,
		`remote_ip:"remoteIP"`,
		`id:"genericID"`,
		`email:"email"`,
		`key:"configKey"`,
		`user_id:"userID"`,
		`next_run_at:"nextRunAt"`,
		`last_error:"lastError"`,
		`report_id:"reportID"`,
		`effective_backend:"effectiveBackend"`,
		`resource_type:"resourceType"`,
		`resource_id:"resourceID"`,
		`smoke_status:"smokeStatus"`,
		`size_bytes:"sizeBytes"`,
		`modified_at:"modifiedAt"`,
		`delete_protected:"deleteProtectedColumn"`,
		`api_key_hint:"apiKeyHint"`,
		`profile:"profile"`,
		`raw:"raw"`,
		`reason:"reason"`,
		`summary:"summary"`,
		`latest_source_at:"latestSourceAt"`,
		`source_count:"sourceCount"`,
		`distilled_sources:"distilledSources"`,
		`function cellText(col,raw)`,
		`function optionLabel(value,col="status")`,
		`function localizedOptions(values,col="status")`,
		`active:name==="active"?"activeState":"activeStatus"`,
		`suspended:"suspendedStatus"`,
		`pending:"jobPending"`,
		`running:"jobRunning"`,
		`succeeded:"jobSucceeded"`,
		`pass:"statusPass"`,
		`warn:"statusWarn"`,
		`high:"severityHigh"`,
		`medium:"severityMedium"`,
		`low:"severityLow"`,
		`localizedOptions(["pending","running","succeeded","failed","canceled"])`,
		`localizedOptions(["auto","landlock","bwrap","nsjail","none"],"mode")`,
		`localizedOptions(["true","false"],"strict")`,
		`localizedOptions(["bwrap","landlock","nsjail"],"backend")`,
		`localizedOptions(["default","disabled","host"],"network")`,
		`localizedOptions(["error","warn","info"],"level")`,
		`localizedOptions(["pass","warn","fail"])`,
		`localizedOptions(["high","medium","low"],"severity")`,
		`t("riskTimeRangeInvalid")`,
		`t("requiresPrivilege")`,
		`t("redactedState")`,
		`owner:"owner"`,
		`operator:"operator"`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("admin app missing localized table marker %s", needle)
		}
	}
}

func TestAdminWebUsesTenantUserSelectors(t *testing.T) {
	bodyBytes, err := fs.ReadFile(adminWebFS, "admin_web/app.js")
	if err != nil {
		t.Fatalf("read admin app: %v", err)
	}
	body := string(bodyBytes)
	for _, needle := range []string{
		"function tenantSelect(",
		"function userSelect(",
		"function adminSelect(",
		"function tenantUserValue(",
		"syncTenantFromUser(\"knowledgeUser\",\"knowledgeTenant\")",
		"syncTenantFromUser(\"jobUser\",\"jobTenant\")",
		"syncTenantFromUser(\"auditUser\",\"auditTenant\")",
		"tenantSelect(\"knowledgeTenant\",tenantItems,\"\",true,\"selectTenant\")",
		"userSelect(\"knowledgeUser\",userItems,\"\",\"tenantUser\",true,\"selectUser\")",
		"tenantSelect(\"knowledgeScopeTenant\",tenantItems,\"\",true,\"selectTenant\")",
		"userSelect(\"knowledgeScopeUser\",userItems,\"\",\"tenantUser\",true,\"selectUser\")",
		"function appendKnowledgeScope()",
		"function displayWithID(label,id)",
		"knowledgeSourceDisplayHint",
		"knowledgeAccessTargetHint",
		"knowledgeScopeBuilderHint",
		"knowledgeTenantNames",
		"knowledgeUserNames",
		"function knowledgeScopeDisplay(scope)",
		"publicKnowledgeRows",
		"function publicKnowledgeOptionLabel(x)",
		"[\"name\",\"tenant_name\",\"source_count\",\"distilled_sources\",\"latest_source_at\"]",
		"tenant_name",
		"owner_name",
		"tenant_name:displayWithID(tenantNameByID.get(tenantID)||tenantID,tenantID)",
		"owner_name:displayWithID(ownerLabel,ownerID)",
		"skillPolicyPriorityTitle",
		"tenantOverrideHintDetailed",
		"userOverrideHintDetailed",
		"class=\"policy-flow\"",
		"function appendPublicKnowledgeScope(",
		"function renderKnowledgeScopePreview(",
		"knowledgeScopePreview",
		"knowledgePublicScopeLibrary",
		"addPublicKnowledgeScope",
		"configuredKnowledgeScopes",
		"effectiveKnowledgeAccess",
		"function publicKnowledgePanel(",
		"function bindPublicKnowledgeActions(",
		"function withPublicKnowledgeButton(",
		"async function watchAdminJob(",
		"/api/v1/admin/jobs/${encodeURIComponent(jobID)}",
		"function showKnowledgeOut(",
		"function knowledgeResultView(value)",
		"value.result&&typeof value.result===\"object\"?value.result:value",
		"processed_files",
		"importProcessed",
		"importWarnings",
		"function toastKnowledgeJobResult(job)",
		"toast(t('importStarted'))",
		"importCompleted",
		"importStillRunning",
		"importSource",
		"importTitle",
		"importKind",
		"publicKnowledgeBases",
		"importedKnowledge",
		"crawlDepth",
		"deletePublicKnowledgeConfirm",
		"importTextRequired",
		"importURLRequired",
		"/api/v1/admin/public-knowledge-libraries",
		"/sources",
		"/import/text",
		"/import/file",
		"/import/urls",
		"id=\"publicKnowledgeFile\" type=\"file\" multiple",
		"files.forEach(file=>form.append('file',file))",
		"data-public-kb-add",
		"data-public-kb-remove",
		"/public-libraries/${encodeURIComponent(library.id)}",
		"data-public-kb-delete",
		"source_count",
		"latest_source_at",
		"tenantSelect(\"skillTenant\",tenantItems,\"\",true,\"selectTenant\")",
		"userSelect(\"skillUser\",userItems,\"\",\"tenantUser\",true,\"selectUser\")",
		"/api/v1/admin/skill-sources/tenants/${encodeURIComponent(ids.tenant)}/users/${encodeURIComponent(ids.user)}",
		"tenantSelect(\"jobTenant\",tenantItems)",
		"userSelect(\"jobUser\",userItems,\"\",\"tenantUser\")",
		"withBusyButton(b,t(\"running\"),async()=>",
		"tenantSelect(\"auditTenant\",tenantItems)",
		"userSelect(\"auditUser\",userItems,\"\",\"tenantUser\")",
		"adminSelect(\"auditActorUser\",adminItems)",
		"/api/v1/admin/auth/users",
		"tenantSelect(\"exportTenant\",tenantItems)",
		"userSelect(\"exportUser\",userItems,\"\",\"tenantUser\")",
		"tenantSelect(\"snapshotTenant\",tenantItems)",
		"userSelect(\"snapshotUser\",userItems,\"\",\"tenantUser\")",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("admin web missing tenant/user selector marker %s", needle)
		}
	}
}

func TestAdminWebAIModelsUsesVoicePickerAndClearDownloadStates(t *testing.T) {
	bodyBytes, err := fs.ReadFile(adminWebFS, "admin_web/app.js")
	if err != nil {
		t.Fatalf("read admin app: %v", err)
	}
	body := string(bodyBytes)
	for _, needle := range []string{
		`async function saveDefaultClientConfig(mutator)`,
		`const latest=await api("/api/v1/admin/client-config/default")`,
		`const base={...(latest?.app_config||{})}`,
		`const ttsVoiceOptions=[`,
		`id:"zf_xiaoyi"`,
		`id:"zf_xiaoxiao"`,
		`id:"zm_yunxi"`,
		`id:"zm_yunyang"`,
		`function renderTTSVoiceOptions(current)`,
		`<select id="aiTTSVoice">${renderTTSVoiceOptions(cfg.tts_voice_id||"zf_xiaoyi")}</select>`,
		`ttsVoiceHint`,
		`localAICapabilitiesHint`,
		`sectionHead(t("localAICapabilities"), t("localAICapabilitiesHint")`,
		`downloadNow`,
		`backgroundDownloading`,
		`downloadQueued`,
		`function aiModelRuntimeState(model){ if(model?.ready)`,
		`model.ready?`,
		`model.path?`,
		`model.voice_path?`,
		`class="model-path"`,
		`function renderAIModelTestPanel(cfg)`,
		`async function withBusyButton(btn,label,fn)`,
		`withBusyButton($("refreshAIModelStatus"),t("running"),async()=>`,
		`embBtn.onclick=async()=>withBusyButton(embBtn,t("running"),async()=>`,
		`asrBtn.onclick=async()=>withBusyButton(asrBtn,t("running"),async()=>`,
		`ttsBtn.onclick=async()=>withBusyButton(ttsBtn,t("running"),async()=>`,
		`id="aiEmbeddingTestText"`,
		`id="runAIEmbeddingTest"`,
		`id="aiASRTestFile"`,
		`id="runAIASRTest"`,
		`id="aiTTSTestText"`,
		`id="runAITTSTest"`,
		`modelTest: "Model test"`,
		`embeddingTestText`,
		`asrTestHint`,
		`runTTSTest`,
		`/api/v1/admin/ai-models/embedding/embed`,
		`/api/v1/admin/ai-models/asr/transcribe`,
		`/api/v1/admin/ai-models/tts/synthesize`,
		`function aiAudioFormatFromFile(file)`,
		`async function postDownloadAdmin`,
		`parsed?.error||text||msg`,
		`data-ai-model-download="${esc(name)}"`,
		`btn.textContent=t("backgroundDownloading")`,
		`toast(t("downloadQueued"))`,
		`await saveDefaultClientConfig(base=>readAIModelsForm(base));`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("admin ai models page missing marker %s", needle)
		}
	}
	for _, stale := range []string{
		`<input id="aiTTSVoice" value="${esc(cfg.tts_voice_id||"")}">`,
		`const body=()=>readAIModelsForm(cfg);`,
		`id="aiPrimaryLLM"`,
		`id="aiAuxiliaryLLM"`,
		`id="aiKnowledgeVisionLLM"`,
		`id="aiProviderList"`,
		`id="aiAdvancedJSON"`,
		`id="aiKnowledgeIncludeImages"`,
		`<p class="helper-text">${esc(t("downloadPendingHint"))}</p>`,
		`cfg.knowledge_include_images=$("aiKnowledgeIncludeImages").checked;`,
	} {
		if strings.Contains(body, stale) {
			t.Fatalf("admin ai models page should not keep stale marker %s", stale)
		}
	}
	cssBytes, err := fs.ReadFile(adminWebFS, "admin_web/styles.css")
	if err != nil {
		t.Fatalf("read admin css: %v", err)
	}
	css := string(cssBytes)
	for _, needle := range []string{
		".ai-model-status-card {\n  display: flex;",
		"flex-direction: column;",
		"gap: 10px;",
		".ai-model-status-card p {\n  margin: 0;",
		".ai-model-status-card button {\n  margin-top: auto;",
	} {
		if !strings.Contains(css, needle) {
			t.Fatalf("admin ai models css missing aligned status card marker %s", needle)
		}
	}
}

func TestAdminWebKnowledgeImportDefaultsLiveInKnowledgePanel(t *testing.T) {
	bodyBytes, err := fs.ReadFile(adminWebFS, "admin_web/app.js")
	if err != nil {
		t.Fatalf("read admin app: %v", err)
	}
	body := string(bodyBytes)
	for _, needle := range []string{
		`api("/api/v1/admin/client-config/default").catch(e=>({error:e.message,app_config:{}}))`,
		`const sharedCfg=cfgResp.app_config||{};`,
		`publicKnowledgePanel(publicKnowledgeRows,tenantItems,sharedCfg)`,
		`function publicKnowledgePanel(libraries,tenants,sharedCfg)`,
		`<h3>${t("knowledgeImportDefaults")}</h3>`,
		`id="knowledgeIncludeImages"`,
		`id="saveKnowledgeImportDefaults"`,
		`knowledgeImportDefaultsHint`,
		`knowledgeImportDefaultsSaved`,
		`bindKnowledgeActions(availableSources,publicLibraries,sharedCfg)`,
		`function bindKnowledgeActions(sources,publicLibraries,sharedCfg)`,
		`await saveDefaultClientConfig(base=>({...base,knowledge_include_images:$('knowledgeIncludeImages')?.checked===true}));`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("admin knowledge page missing marker %s", needle)
		}
	}
}

func TestAdminWebIncludesOwnerRoleGuards(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/admin/app.js", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	body := w.Body.String()
	for _, needle := range []string{
		"function isOwner()",
		"state.me?.auth_type===\"admin_secret\"",
		"state.me?.admin?.role===\"owner\"",
		"function applyOwnerGuards()",
		"ownerOnly",
		"runRuntimeGC",
		"saveSandbox",
		"rollbackSandbox",
		"installSandboxRun",
		"createTenant",
		"createCredential",
		"saveAIModels",
		"saveKnowledgeImportDefaults",
		"createPublicKnowledge",
		"publicKnowledgeImportText",
		"publicKnowledgeImportFile",
		"publicKnowledgeImportURLs",
		"runImport",
		"createSnapshot",
		"[data-job-cancel]",
		"[data-tenant-status]",
		"[data-user-delete]",
		"[data-snapshot-delete]",
		"[data-public-kb-add]",
		"[data-public-kb-remove]",
		"[data-public-kb-delete]",
		"[data-ai-model-download]",
		"/api/v1/admin/auth/change-password",
		"/api/v1/admin/auth/users",
		"/api/v1/admin/auth/sessions",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("admin web missing owner guard marker %s", needle)
		}
	}
}

func TestAdminWebDownloadsUseAuthenticatedFetch(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/admin/app.js", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	body := w.Body.String()
	if strings.Contains(body, "window.open") {
		t.Fatalf("admin web downloads must not use window.open because Admin API auth requires headers")
	}
	for _, needle := range []string{
		"async function downloadAdmin",
		"async function textAdmin",
		"fetch(path,{headers:headers(false)})",
		"URL.createObjectURL(blob)",
		"content-disposition",
		"downloadAdmin(\"/api/v1/admin/runtime/goroutines?debug=2&download=true\"",
		"downloadAdmin(\"/api/v1/admin/runtime/profiles/heap?debug=1&gc=true&download=true\"",
		"downloadAdmin(\"/api/v1/admin/support-bundle?download=true\"",
		"downloadAdmin(\"/api/v1/admin/sandbox/support-bundle?download=true\"",
		"downloadAdmin(`/api/v1/admin/logs/${encodeURIComponent(id)}/download?${q}`",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("admin web missing authenticated download marker %s", needle)
		}
	}
}

func TestAdminWebAccessibilityContracts(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	shell := w.Body.String()
	for _, needle := range []string{
		`id="skipLink" class="skip-link" href="#loginPanel"`,
		`<body class="auth-screen">`,
		`<div id="app" class="app-shell auth-only">`,
		`<meta name="theme-color" content="#f4f7fb" media="(prefers-color-scheme: light)" />`,
		`<meta name="theme-color" content="#f4f7fb" media="(prefers-color-scheme: dark)" />`,
		`<link rel="stylesheet" href="/admin/styles.css?v=light-admin-20260609" />`,
		`<script src="/admin/app.js?v=light-admin-20260609"></script>`,
		`<nav id="nav" class="nav" aria-label="Admin sections">`,
		`<main id="main" class="main" tabindex="-1">`,
		`<section id="bootstrapPanel" class="panel hidden" tabindex="-1">`,
		`<section id="loginPanel" class="panel hidden" tabindex="-1">`,
	} {
		if !strings.Contains(shell, needle) {
			t.Fatalf("admin shell missing accessibility marker %s", needle)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/app.js", nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	app := w.Body.String()
	for _, needle := range []string{
		`el.setAttribute("role","status")`,
		`el.setAttribute("aria-live","polite")`,
		`pendingRequests: 0`,
		`sectionChanged: false`,
		`function setNetworkBusy(delta)`,
		`document.body.classList.toggle("is-fetching",state.pendingRequests>0)`,
		`function focusPrimaryInput(scope)`,
		`document.title=`,
		`document.onkeydown=(e)=>`,
		`buttons[n-1]?.click()`,
		`const sections = ["overview","sandbox","logs","config","clientConfig","aiModels","tenants","accounts","knowledge","ops"]`,
		`const f={overview,sandbox,logs,config,clientConfig,aiModels,tenants,accounts,knowledge,ops}[state.section] || overview;`,
		`async function aiModels(){`,
		`function renderAIModelStatusPanel(models)`,
		`id="aiModelStatusPanel"`,
		`id="aiLocalCapabilitiesPanel"`,
		`/api/v1/admin/ai-models/status`,
		`await refreshAIModelStatus(true)`,
		`bindAIModelStatusActions(); applyOwnerGuards(); enhanceA11y();`,
		`/api/v1/admin/ai-models/${encodeURIComponent(model)}/download`,
		`data-ai-model-download`,
		`decoder_ready`,
		`mp3_encoder_ready`,
		`const enabled=model.enabled!==false`,
		`${t("enabledState")} ${t("autoEnabled")}`,
		`modelRuntimeStatus: "Model runtime status"`,
		`localAICapabilities: "Local AI capabilities"`,
		`id="aiTTSAutoVoiceSummary"`,
		`id="saveAIModels"`,
		`const initialSection = sections.includes(location.hash.slice(1))`,
		`function setSection(id, updateHash=true)`,
		`function setAuthShell(on,target="loginPanel")`,
		`$("skipLink")?.setAttribute("href",active?` + "`#${target}`" + `:"#content")`,
		`classList.toggle("auth-only",active)`,
		`function authLocaleControl()`,
		`function bindAuthLocale(onChange)`,
		`function localeOptions(selected=state.locale)`,
		`function applyLocaleMetadata(out)`,
		`${selected===x.locale?"selected":""}`,
		`state.sectionChanged=state.section!==id`,
		`history.replaceState(null,"",`,
		`$("main")?.focus({preventScroll:true})`,
		`state.sectionChanged=false`,
		`window.addEventListener("hashchange"`,
		`focusPrimaryInput($("loginPanel"))`,
		`admin-auth-shell`,
		`admin-auth-card`,
		`admin-auth-hero`,
		`authLocaleSelect`,
		`bindAuthLocale(()=>renderLogin(focusMode))`,
		`bindAuthLocale(()=>renderBootstrap(status))`,
		`setAuthShell(true,"bootstrapPanel")`,
		`applyLocaleMetadata(bs)`,
		`role="tablist"`,
		`role="tab"`,
		`role="tabpanel"`,
		`maclaw.admin.loginMode`,
		`switchLoginMode`,
		`ArrowLeft`,
		`ArrowRight`,
		`tabindex="${accountActive?"0":"-1"}"`,
		`aria-hidden="${!accountActive}"`,
		`id="toggleSecret"`,
		`aria-pressed="false"`,
		`t("showSecret")`,
		`t("hideSecret")`,
		`t("requiredField")`,
		`const requireFields=`,
		`autocomplete="username" required`,
		`autocomplete="current-password" required`,
		`t("accountLogin")`,
		`t("secretLogin")`,
		`t("secretLoginHint")`,
		`focusPrimaryInput($("bootstrapPanel"))`,
		`<div class="empty-state" role="status">`,
		`<span aria-hidden="true"></span>`,
		`function enhanceA11y()`,
		`function modalDecision`,
		`function confirmPhrase`,
		`function forceDeleteSecret`,
		`confirmPhrase(t("exportSecretPrompt"),"EXPORT SECRETS")`,
		`forceDeleteTenantPrompt`,
		`force=true`,
		`content.setAttribute("role","region")`,
		`th.setAttribute("scope","col")`,
		`box.setAttribute("aria-labelledby",h.id)`,
		`wrap.setAttribute("tabindex","0")`,
		`wrap.setAttribute("role","region")`,
		`class="table-wrap log-table-wrap" tabindex="0" role="region"`,
		`badge.className=`,
	} {
		if !strings.Contains(app, needle) {
			t.Fatalf("admin app missing accessibility marker %s", needle)
		}
	}
	for _, stale := range []string{
		`id="reloadAIModels"`,
		`$("reloadAIModels").onclick`,
		`client-config-card">${sectionHead(t("sharedAIModels")`,
	} {
		if strings.Contains(app, stale) {
			t.Fatalf("admin ai models page should only keep runtime/capability panels; found stale marker %s", stale)
		}
	}
	for _, stale := range []string{
		`id="aiCurrentProvider"`,
		`id="aiProvidersJSON"`,
		`id="aiPromptCacheJSON"`,
		`id="aiModelsOut"`,
		`id="validateAIModels"`,
		`excluded:["screen_parsing_enabled"]`,
		`advanced routing settings`,
	} {
		if strings.Contains(app, stale) {
			t.Fatalf("admin AI models page should not expose retired marker %s", stale)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/styles.css", nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	css := w.Body.String()
	for _, needle := range []string{".skip-link", ".badge-on::before", ".badge-off::before", ".modal-backdrop", ".modal-actions", ".app-shell.auth-only .sidebar", ".app-shell.auth-only .topbar", ".app-shell.auth-only #content", ".admin-auth-shell", ".auth-locale", ".client-search-layout", ".client-search-provider.active", ".client-search-detail", ".client-mode-tabs", ".client-mode-tab.active", `.nav button[data-section="aiModels"]::before`} {
		if !strings.Contains(css, needle) {
			t.Fatalf("admin css missing accessibility marker %s", needle)
		}
	}
	if strings.Contains(css, "#aiModelsOut") {
		t.Fatalf("admin css should not reference retired AI models raw output")
	}
	for _, needle := range []string{`body.is-fetching::after`, `@keyframes network-progress`, `.field:focus-within label`, `env(safe-area-inset-top)`, `@media print`} {
		if !strings.Contains(css, needle) {
			t.Fatalf("admin css missing interaction polish marker %s", needle)
		}
	}
	for _, needle := range []string{`::selection`, `@media (prefers-contrast: more)`, `@media (forced-colors: active)`, `forced-color-adjust: auto`} {
		if !strings.Contains(css, needle) {
			t.Fatalf("admin css missing contrast support marker %s", needle)
		}
	}
	for _, needle := range []string{
		`@media (prefers-color-scheme: dark)`,
		`color-scheme: dark`,
		`--bg: #0e141a`,
		`.empty-state`,
	} {
		if !strings.Contains(css, needle) {
			t.Fatalf("admin css missing dark mode marker %s", needle)
		}
	}
	for _, needle := range []string{
		`/* Light-first admin system. Keep the console calm even on dark OS themes. */`,
		`--sidebar: #f9fbfd`,
		`--sidebar-soft: #eef4f8`,
		`--sidebar-line: #dce6ef`,
		`.ops-main-grid`,
		`.ops-snapshot-card`,
		`.ops-risk-card`,
		`grid-column: 1 / -1;`,
		`.ops-risk-table .table-wrap`,
		`max-height: clamp(260px, calc(100dvh - 500px), 620px);`,
		`overscroll-behavior: contain;`,
		`position: sticky;`,
		`@media (max-width: 1120px)`,
		`.ops-control-grid,
  .ops-risk-meta { grid-template-columns: 1fr; }`,
		`.auth-screen,`,
		`.app-shell.auth-only .main`,
		`.admin-auth-shell`,
		`.admin-auth-hero`,
		`background: #ffffff;`,
		`color: var(--text);`,
		`@media (max-width: 820px)`,
		`.app-shell { grid-template-columns: 1fr; }`,
		`.nav { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }`,
		`@media (max-width: 560px)`,
		`table { min-width: 620px; }`,
	} {
		if !strings.Contains(css, needle) {
			t.Fatalf("admin css missing light ops layout marker %s", needle)
		}
	}
	darkModeIndex := strings.Index(css, `@media (prefers-color-scheme: dark)`)
	lightFirstIndex := strings.Index(css, `/* Light-first admin system. Keep the console calm even on dark OS themes. */`)
	if darkModeIndex < 0 || lightFirstIndex < 0 || lightFirstIndex < darkModeIndex {
		t.Fatalf("admin light-first overrides must come after dark mode styles")
	}
	if strings.Contains(css, `@media (max-width: 1320px) {
  .ops-main-grid { grid-template-columns: 1fr; }
}`) {
		t.Fatalf("admin ops page should keep two-column layout on normal desktop widths")
	}
	bodyBytes, err := fs.ReadFile(adminWebFS, "admin_web/app.js")
	if err != nil {
		t.Fatalf("read admin app: %v", err)
	}
	body := string(bodyBytes)
	riskIndex := strings.Index(body, `ops-risk-card`)
	snapshotIndex := strings.Index(body, `ops-snapshot-card`)
	controlIndex := strings.Index(body, `ops-control-grid`)
	if riskIndex < 0 || snapshotIndex < 0 || controlIndex < 0 || !(riskIndex < snapshotIndex && snapshotIndex < controlIndex) {
		t.Fatalf("admin ops DOM order should be risk events, snapshots, then controls")
	}
	for _, stale := range []string{
		`@media (max-width: 1040px) {
  .ops-main-grid,`,
		`@media (max-width: 960px) {
  .ops-control-grid,`,
	} {
		if strings.Contains(css, stale) {
			t.Fatalf("admin ops responsive layout should be centralized at 1120px; found stale marker %s", stale)
		}
	}
}
