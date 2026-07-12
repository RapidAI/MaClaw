package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestResolveStaticDirFromBasesPrefersExistingBase(t *testing.T) {
	root := t.TempDir()
	deployDir := filepath.Join(root, "deploy")
	adminDir := filepath.Join(deployDir, "web", "admin")
	if err := os.MkdirAll(adminDir, 0755); err != nil {
		t.Fatalf("mkdir admin dir: %v", err)
	}

	got := resolveStaticDirFromBases("./web/admin", []string{
		filepath.Join(root, "missing"),
		deployDir,
	})
	want := filepath.Clean(adminDir)
	if got != want {
		t.Fatalf("resolveStaticDirFromBases() = %q, want %q", got, want)
	}
}

func TestRegisterPWAStaticRoutesServesIndexAndAssets(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("index-page"), 0644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log('ok');"), 0644); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "icons"), 0755); err != nil {
		t.Fatalf("mkdir icons: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "icons", "favicon-32x32.png"), []byte("png-bytes"), 0644); err != nil {
		t.Fatalf("write favicon: %v", err)
	}

	mux := http.NewServeMux()
	registerPWAStaticRoutes(mux, dir, "/app")

	indexReq := httptest.NewRequest(http.MethodGet, "/app", nil)
	indexRec := httptest.NewRecorder()
	mux.ServeHTTP(indexRec, indexReq)
	if indexRec.Code != http.StatusOK {
		t.Fatalf("index status = %d", indexRec.Code)
	}
	if body := indexRec.Body.String(); body != "index-page" {
		t.Fatalf("index body = %q", body)
	}

	assetReq := httptest.NewRequest(http.MethodGet, "/app/app.js", nil)
	assetRec := httptest.NewRecorder()
	mux.ServeHTTP(assetRec, assetReq)
	if assetRec.Code != http.StatusOK {
		t.Fatalf("asset status = %d", assetRec.Code)
	}
	if body := assetRec.Body.String(); body != "console.log('ok');" {
		t.Fatalf("asset body = %q", body)
	}

	spaReq := httptest.NewRequest(http.MethodGet, "/app/session/123", nil)
	spaRec := httptest.NewRecorder()
	mux.ServeHTTP(spaRec, spaReq)
	if spaRec.Code != http.StatusOK {
		t.Fatalf("spa fallback status = %d", spaRec.Code)
	}
	if body := spaRec.Body.String(); body != "index-page" {
		t.Fatalf("spa fallback body = %q", body)
	}

	faviconReq := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
	faviconRec := httptest.NewRecorder()
	mux.ServeHTTP(faviconRec, faviconReq)
	if faviconRec.Code != http.StatusOK {
		t.Fatalf("favicon status = %d", faviconRec.Code)
	}
	if body := faviconRec.Body.String(); body != "png-bytes" {
		t.Fatalf("favicon body = %q", body)
	}
	if got := faviconRec.Header().Get("Content-Type"); !strings.Contains(got, "image/png") {
		t.Fatalf("favicon content-type = %q", got)
	}
}

func TestRegisterAdminStaticRoutesServesIndexAndAssets(t *testing.T) {
	dir := t.TempDir()
	// Admin is served as external script assets (not inlined into index).
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<title>MaClaw Hub Admin</title><script src="/admin/admin.js"></script>`), 0644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "admin.js"), []byte("console.log('admin');"), 0644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	mux := http.NewServeMux()
	registerAdminStaticRoutes(mux, dir, "/admin")

	indexReq := httptest.NewRequest(http.MethodGet, "/admin", nil)
	indexRec := httptest.NewRecorder()
	mux.ServeHTTP(indexRec, indexReq)
	if indexRec.Code != http.StatusOK {
		t.Fatalf("index status = %d", indexRec.Code)
	}
	body := indexRec.Body.String()
	if !strings.Contains(body, "<title>MaClaw Hub Admin</title>") {
		t.Fatalf("index body missing title: %q", body)
	}
	if !strings.Contains(body, `src="/admin/admin.js"`) {
		t.Fatalf("index body missing external admin script ref: %q", body)
	}

	assetReq := httptest.NewRequest(http.MethodGet, "/admin/admin.js", nil)
	assetRec := httptest.NewRecorder()
	mux.ServeHTTP(assetRec, assetReq)
	if assetRec.Code != http.StatusOK {
		t.Fatalf("asset status = %d", assetRec.Code)
	}
	if body := assetRec.Body.String(); body != "console.log('admin');" {
		t.Fatalf("asset body = %q", body)
	}

	spaReq := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	spaRec := httptest.NewRecorder()
	mux.ServeHTTP(spaRec, spaReq)
	if spaRec.Code != http.StatusOK {
		t.Fatalf("spa fallback status = %d", spaRec.Code)
	}
	body = spaRec.Body.String()
	if !strings.Contains(body, "<title>MaClaw Hub Admin</title>") {
		t.Fatalf("spa fallback body missing title: %q", body)
	}
	if !strings.Contains(body, `src="/admin/admin.js"`) {
		t.Fatalf("spa fallback body missing external admin script ref: %q", body)
	}
}

func TestRegisterAdminStaticRoutesServesExternalScriptAssets(t *testing.T) {
	// Admin JS is loaded as external assets; no fragile inline-injection path.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<body><script src="/admin/admin.js"></script></body>`), 0644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	js := `window.msg = "</script><script>throw new Error('truncated')</script>";`
	if err := os.WriteFile(filepath.Join(dir, "admin.js"), []byte(js), 0644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	mux := http.NewServeMux()
	registerAdminStaticRoutes(mux, dir, "/admin")

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("index status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `src="/admin/admin.js"`) {
		t.Fatalf("index should keep external script ref: %q", body)
	}
	// Asset is served raw (not inlined), so </script> in the JS body is fine.
	assetReq := httptest.NewRequest(http.MethodGet, "/admin/admin.js", nil)
	assetRec := httptest.NewRecorder()
	mux.ServeHTTP(assetRec, assetReq)
	if assetRec.Code != http.StatusOK {
		t.Fatalf("asset status = %d", assetRec.Code)
	}
	if got := assetRec.Body.String(); got != js {
		t.Fatalf("asset body = %q, want raw JS", got)
	}
}

func TestRegisterGetCreditsStaticRoutesServesPage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("credits-page"), 0644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	mux := http.NewServeMux()
	registerGetCreditsStaticRoutes(mux, dir, "/get-credits")

	req := httptest.NewRequest(http.MethodGet, "/get-credits", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("index status = %d", rec.Code)
	}
	if body := rec.Body.String(); body != "credits-page" {
		t.Fatalf("index body = %q", body)
	}
}

func TestProfessionalStylesheetsServedByStaticRoutes(t *testing.T) {
	cases := []struct {
		name     string
		prefix   string
		register func(*http.ServeMux, string, string)
	}{
		{name: "admin", prefix: "/admin", register: registerAdminStaticRoutes},
		{name: "bind", prefix: "/bind", register: registerBindStaticRoutes},
		{name: "get-credits", prefix: "/get-credits", register: registerGetCreditsStaticRoutes},
		{name: "approval_workflow", prefix: "/approval_workflow", register: registerStaticRoutes},
		{name: "connector", prefix: "/connector", register: registerStaticRoutes},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("index-page"), 0644); err != nil {
				t.Fatalf("write index: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "professional.css"), []byte("body{color:#172033}"), 0644); err != nil {
				t.Fatalf("write css: %v", err)
			}

			mux := http.NewServeMux()
			tc.register(mux, dir, tc.prefix)

			req := httptest.NewRequest(http.MethodGet, tc.prefix+"/professional.css", nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("css status = %d", rec.Code)
			}
			if body := rec.Body.String(); body != "body{color:#172033}" {
				t.Fatalf("css body = %q", body)
			}
		})
	}
}

func TestAdminLegacyMirrorTreeRemoved(t *testing.T) {
	legacyDir := filepath.Join("..", "..", "web", "admin", "js")
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Fatalf("legacy admin mirror directory should stay deleted: %v", err)
	}
}

func TestHubAdminPageIncludesFailureLogsUI(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "web", "admin", "index.html"))
	if err != nil {
		t.Fatalf("read admin index: %v", err)
	}
	content := string(body)
	for _, want := range []string{
		`data-tab="failurelogs"`,
		`data-tab="knowledge"`,
		`id="tab-failurelogs"`,
		`id="tab-knowledge"`,
		`/admin/failure-logs-tab.js`,
		`/admin/knowledge-management-tab.js`,
		`loadFailureLogs()`,
		`loadKnowledgeShares()`,
		`id="centerCorporateEmailDomains"`,
		`id="centerAcceptPublicSignup"`,
		`id="centerCorporateEmailDomainsHero"`,
		`id="centerAcceptPublicSignupHero"`,
		`id="subtab-workflows" type="button" onclick="switchMarketplaceSubtab('workflows')" data-i18n="marketplaceSubtabWorkflowReviews"`,
		`id="marketplace-subtab-workflows"`,
		`id="marketplaceWorkflowReviewsList"`,
		`id="marketplaceWorkflowReviewDetail"`,
		`id="workflowRejectOverlay" class="session-modal-overlay"`,
		`id="workflowRejectReason" maxlength="2000"`,
		`id="workflowRejectReasonError" class="hint" role="alert"`,
		`<option value="approval_workflow">approval_workflow</option>`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("admin index missing %s", want)
		}
	}
}

func TestKnowledgeSharesPageUsesAutoViewerSessionFlow(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "web", "knowledge_shares", "index.html"))
	if err != nil {
		t.Fatalf("read knowledge shares index: %v", err)
	}
	content := string(body)
	for _, want := range []string{
		`const SESSION_STORAGE_KEY = 'hubKnowledgeViewerToken'`,
		`const PAGE_SIZE = 20;`,
		`function tokenFromLocation()`,
		`queryParams.get('token')`,
		`cleanTokenFromURL()`,
		`params.delete(key)`,
		`const dirtyShareIDs = new Set();`,
		`if (dirtyShareIDs.size > 0) return true;`,
		`grid-template-columns: repeat(2, minmax(0, 1fr));`,
		`id="pager" class="pager" hidden`,
		`function goToSharePage(page)`,
		`Open this page from MaClaw GUI`,
		`Sliding renewal`,
		`resp.status === 401`,
		`sessionSavedTitle`,
		`authFailedTitle`,
		`id="clearSession"`,
		`localStorage.removeItem(SESSION_STORAGE_KEY)`,
		`window.setInterval(() => {`,
		`/api/knowledge/shares/mine?limit=${PAGE_SIZE}&offset=${offset}`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("knowledge shares page missing %s", want)
		}
	}
	if strings.Contains(content, `id="token"`) || strings.Contains(content, `saveToken`) {
		t.Fatalf("knowledge shares page should not expose manual viewer token input anymore")
	}
}

func TestMaClawComputeModuleShowsModuleAuthorizationBadge(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "web", "admin", "maclaw-compute-module.js"))
	if err != nil {
		t.Fatalf("read MaClaw compute module: %v", err)
	}
	content := string(body)
	for _, want := range []string{
		`hasComputeModuleAuthorization()`,
		`isTenantAdminComputeContext()`,
		`computeCreditsRemaining(item)`,
		`hasAvailableOfficialComputeCredits()`,
		`shouldShowGlobalComputeAlert()`,
		`updateGlobalComputeAlert()`,
		`document.getElementById('maclawComputeTopAlert')`,
		`noAvailableCompute: 'No available compute'`,
		`noAvailableComputeAction: 'Click to purchase'`,
		`noAvailableCompute: '\u65e0\u53ef\u7528\u7b97\u529b'`,
		`noAvailableComputeAction: '\u70b9\u51fb\u8d2d\u4e70'`,
		`return !!_computeAuthStatus.allow_external_providers;`,
		`if (!isTenantAdminComputeContext()) return false;`,
		`return computeCreditsRemaining(item) > 0;`,
		`refreshComputeAuthorizationIfStale(3000);`,
		`_computeAuthCheckedAt = Date.now();`,
		`observeLLMProviderListForBanner()`,
		`document.getElementById('llmProviderList') && !document.getElementById('maclawOfficialBanner')`,
		`authStatusError: 'Compute Auth Sync Failed'`,
		`authStatusError: '\u7b97\u529b\u6388\u6743\u540c\u6b65\u5931\u8d25'`,
		`noComputeCredits: 'Compute module authorized. No active compute credits yet.'`,
		`noComputeCredits: '\u7b97\u529b\u6a21\u5757\u5df2\u6388\u6743\uff0c\u6682\u65e0\u53ef\u7528\u7b97\u529b\u989d\u5ea6\u3002'`,
		`hasComputeModuleAuthorization() ? t('noComputeCredits') : t('noActiveAuthorizations')`,
		`_computeAuthStatus.authorization_error`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("MaClaw compute module missing %s", want)
		}
	}
	if !strings.Contains(content, `} else if (hasComputeModuleAuthorization()) {
        badge.className = 'badge ok';`) {
		t.Fatalf("MaClaw compute badge must depend on module authorization, not active credit cards")
	}
	if strings.Contains(content, `hasActiveComputeAuthorization()`) {
		t.Fatalf("MaClaw compute module must not keep active-credit helper for module authorization")
	}

	admin, err := os.ReadFile(filepath.Join("..", "..", "web", "admin", "index.html"))
	if err != nil {
		t.Fatalf("read admin index: %v", err)
	}
	if !strings.Contains(string(admin), `id="maclawComputeTopAlert"`) {
		t.Fatalf("admin topbar must include MaClaw compute warning placeholder")
	}

	css, err := os.ReadFile(filepath.Join("..", "..", "web", "admin", "professional.css"))
	if err != nil {
		t.Fatalf("read admin professional css: %v", err)
	}
	for _, want := range []string{
		`.compute-top-alert{display:inline-flex`,
		`.compute-top-alert button{height:26px`,
	} {
		if !strings.Contains(string(css), want) {
			t.Fatalf("admin css missing compute top alert contract %s", want)
		}
	}
}

func TestTenantComputeSummariesDoNotTreatCreditCardsAsModuleAuthorization(t *testing.T) {
	for _, file := range []string{"overview-tenant-info.js", "tenant-tab.js"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "web", "admin", file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		content := string(body)
		for _, want := range []string{
			`computeCardIsActive`,
			`formatComputeCredits`,
			`sumComputeCredits`,
			`var computeAuthorizations = Array.isArray`,
			`activeComputeCards`,
			`active: !!compute`,
			`__external_compute_permission__`,
		} {
			if !strings.Contains(content, want) {
				t.Fatalf("%s missing %s", file, want)
			}
		}
		for _, forbidden := range []string{
			`authorizations.some(function(a) { return a.active; })`,
			`authorization_count: computeData.authorizations.length`,
			`authorization_count: computeRaw.authorizations.length`,
			`computeData && computeData.authorizations && computeData.authorizations.length > 0`,
			`computeRaw && computeRaw.authorizations && computeRaw.authorizations.length > 0`,
			`Math.round(Number(compute.remaining_credits) || 0)`,
			`Math.round(Number(compute.total_credits) || 0)`,
		} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("%s must not contain stale compute summary pattern %s", file, forbidden)
			}
		}
	}
}

func TestMaClawComputeCreditDisplayPreservesFractionalCredits(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "web", "admin", "maclaw-compute-module.js"))
	if err != nil {
		t.Fatalf("read maclaw-compute-module.js: %v", err)
	}
	content := string(body)
	for _, want := range []string{
		`n = Math.round(n * 10000) / 10000`,
		`maximumFractionDigits: 4`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("MaClaw compute module must preserve fractional credit display, missing %s", want)
		}
	}
	for _, forbidden := range []string{
		`Math.round(n).toLocaleString()`,
		`return String(Math.round(n))`,
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("MaClaw compute module must not round compute credits to whole numbers: %s", forbidden)
		}
	}
}

func TestAdminModelServiceCreditDisplaysPreserveFractionalCredits(t *testing.T) {
	for _, file := range []string{"governance-tab.js", "security-tab.js"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "web", "admin", file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		content := string(body)
		for _, want := range []string{
			`Math.round(n * 10000) / 10000`,
			`maximumFractionDigits: 4`,
		} {
			if !strings.Contains(content, want) {
				t.Fatalf("%s must preserve fractional credit display, missing %s", file, want)
			}
		}
		for _, forbidden := range []string{
			`Math.round(n * 100) / 100`,
			`maximumFractionDigits: 2`,
		} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("%s must not round model service credits to low precision: %s", file, forbidden)
			}
		}
	}
}

func TestGovernanceAdminExposesVerifiedPhoneRouteSync(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "web", "admin", "governance-tab.js"))
	if err != nil {
		t.Fatalf("read governance-tab.js: %v", err)
	}
	content := string(body)
	for _, want := range []string{
		`syncVerifiedPhoneRoutes`,
		`/api/admin/routing/sync-verified-phone-routes`,
		`syncPhoneRoutes`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("governance-tab.js must expose verified phone route sync, missing %s", want)
		}
	}
}

func TestCenterAdminExposesGlobalVerifiedPhoneRouteSync(t *testing.T) {
	centerBody, err := os.ReadFile(filepath.Join("..", "..", "web", "admin", "center-tab.js"))
	if err != nil {
		t.Fatalf("read center-tab.js: %v", err)
	}
	centerContent := string(centerBody)
	for _, want := range []string{
		`syncVerifiedPhoneRoutesFromCenter`,
		`/api/admin/routing/sync-verified-phone-routes`,
		`syncPhoneRoutes`,
	} {
		if !strings.Contains(centerContent, want) {
			t.Fatalf("center-tab.js must expose global verified phone route sync, missing %s", want)
		}
	}

	indexBody, err := os.ReadFile(filepath.Join("..", "..", "web", "admin", "index.html"))
	if err != nil {
		t.Fatalf("read admin index.html: %v", err)
	}
	indexContent := string(indexBody)
	for _, want := range []string{
		`id="centerSyncPhoneRoutesBtn"`,
		`onclick="syncVerifiedPhoneRoutesFromCenter(this)"`,
	} {
		if !strings.Contains(indexContent, want) {
			t.Fatalf("admin center page must expose global phone route sync button, missing %s", want)
		}
	}
}

func TestAdminMarketplaceWorkflowReviewContracts(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "web", "admin", "marketplace-tab.js"))
	if err != nil {
		t.Fatalf("read marketplace tab: %v", err)
	}
	marketplace := string(body)
	for _, want := range []string{
		`marketplaceSubtabWorkflowReviews: 'Workflow Reviews'`,
		`workflowReviewsTitle: 'Approval Workflow Reviews'`,
		`workflowReviewsTitle: '\u5ba1\u6279\u5de5\u4f5c\u6d41\u5ba1\u6838'`,
		`workflowReviewOpenDesigner: 'Open Designer'`,
		`workflowReviewOpenDesigner: '\u6253\u5f00\u8bbe\u8ba1\u5668'`,
		`workflowReviewRejectTitle: 'Reject workflow submission'`,
		`workflowReviewRejectReasonInvalid: 'Reason must be 10-2000 characters.'`,
		`marketplaceCancel: 'Cancel'`,
		`async function loadWorkflowReviewsInternal()`,
		`api('/api/v1/admin/reviews?page=1')`,
		`api('/api/v1/admin/reviews/' + encodeURIComponent(id))`,
		`global.openWorkflowRejectDialog = function(id)`,
		`global.closeWorkflowRejectDialog = function()`,
		`global.submitWorkflowRejectDialog = async function()`,
		`function validateWorkflowRejectReason()`,
		`reason.length < 10 || reason.length > 2000`,
		`href="/approval_workflow/?workflow_id=' + encodeURIComponent(workflowId) + '"`,
		`function metadataOf(item)`,
		`JSON.parse(item.metadata_json)`,
		`metadata.workflow_id || item.capability_id`,
		`function isMaclawAppCapability(item, metadata)`,
		`function maclawAppEvidenceSummary(item, metadata)`,
		`metadata.product_kind === 'maclaw_app_skill'`,
		`metadata.x_maclaw_apps_preview`,
		`maclaw_app_test_evidence`,
		`class="item-meta maclaw-app-evidence"`,
		`approveMaclawAppCapability`,
		`rejectMaclawAppCapability`,
		`/api/admin/capabilities/maclaw-apps/`,
		`href="/approval_workflow/?review_version_id=' + encodeURIComponent(ver.id) + '"`,
		`href="/approval_workflow/?review_version_id=' + encodeURIComponent(detail.version.id) + '"`,
		`item.capability_type === 'approval_workflow'`,
		`'/approve'`,
		`'/reject'`,
		`loadCapabilities()`,
		`state.workflowReviews = Array.isArray(data.submissions) ? data.submissions : [];`,
		`global.loadWorkflowReviews = async function()`,
		`workflowsPanel.style.display = '';`,
		`var mcpAction = item.capability_type === 'mcp'`,
	} {
		if !strings.Contains(marketplace, want) {
			t.Fatalf("admin marketplace workflow review contract missing %q", want)
		}
	}
	if strings.Contains(marketplace, `global.prompt(mp('workflowReviewRejectPrompt'))`) {
		t.Fatal("workflow review reject should use inline dialog instead of browser prompt")
	}
}

func TestAdminMarketplaceSkillCardNameContracts(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "web", "admin", "marketplace-tab.js"))
	if err != nil {
		t.Fatalf("read marketplace tab: %v", err)
	}
	marketplace := string(body)
	for _, want := range []string{
		`function looksLikeCapabilityReference(value)`,
		`function looksLikeTechnicalSkillSlug(value)`,
		`function humanizeIdentifier(value)`,
		`function shortVersionLabel(versionKey)`,
		`function isRedundantSkillId(id, name)`,
		`function skillIdForCard(item)`,
		`function isInternalCapabilityRecordId(value)`,
		`function bareSkillId(value)`,
		`metadata.skill_name`,
		`manifest.name`,
		`mp-cap-card-title-stack`,
		`class="mp-cap-card-id"`,
		`class="mp-cap-card-name"`,
		`backfillCapabilityDisplayNames`,
		`/api/admin/capabilities/backfill-display-names`,
	} {
		if !strings.Contains(marketplace, want) {
			t.Fatalf("admin marketplace skill name contract missing %q", want)
		}
	}
	// Title stack must render ID above name (user-facing layout contract).
	idIdx := strings.Index(marketplace, `class="mp-cap-card-id"`)
	nameIdx := strings.Index(marketplace, `class="mp-cap-card-name"`)
	if idIdx < 0 || nameIdx < 0 || idIdx > nameIdx {
		t.Fatalf("skill card must place mp-cap-card-id before mp-cap-card-name in markup (idIdx=%d nameIdx=%d)", idIdx, nameIdx)
	}
	if !strings.Contains(marketplace, `/^[0-9a-f]{8,64}$/i.test(value)`) {
		t.Fatal("shortVersionLabel should hide bare hex package digests")
	}
	cssBody, err := os.ReadFile(filepath.Join("..", "..", "web", "admin", "professional.css"))
	if err != nil {
		t.Fatalf("read professional.css: %v", err)
	}
	css := string(cssBody)
	if !strings.Contains(css, `.mp-cap-card-name{font-size:18px`) {
		t.Fatal("skill card name font-size contract missing (expected 18px)")
	}
	if !strings.Contains(css, `.mp-cap-type-badge`) {
		t.Fatal("skill card type badge class contract missing")
	}
	body, err = os.ReadFile("router.go")
	if err != nil {
		t.Fatalf("read router: %v", err)
	}
	if !strings.Contains(string(body), `POST /api/admin/capabilities/backfill-display-names`) {
		t.Fatal("router missing backfill-display-names route")
	}
}

func TestMarketplaceMaclawAppSubmitRouteContract(t *testing.T) {
	body, err := os.ReadFile("router.go")
	if err != nil {
		t.Fatalf("read router: %v", err)
	}
	router := string(body)
	for _, want := range []string{
		`POST /api/capabilities/maclaw-apps/submit`,
		`GET /api/capabilities/maclaw-apps/{id}/package`,
		`CapabilityMaclawAppSubmitHandler(capabilitySvc, identity)`,
		`CapabilityMaclawAppPackageHandler(capabilitySvc, identity)`,
		`POST /api/admin/capabilities/maclaw-apps/{id}/approve`,
		`AdminCapabilityMaclawAppReviewHandler(capabilitySvc, "approve")`,
		`POST /api/admin/capabilities/maclaw-apps/{id}/reject`,
		`AdminCapabilityMaclawAppReviewHandler(capabilitySvc, "reject")`,
	} {
		if !strings.Contains(router, want) {
			t.Fatalf("maclaw app submit route contract missing %q", want)
		}
	}
}

func TestAdminIndexScriptRefsExist(t *testing.T) {
	indexPath := filepath.Join("..", "..", "web", "admin", "index.html")
	body, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read admin index: %v", err)
	}
	content := string(body)
	if strings.Contains(content, "/admin/js/") {
		t.Fatal("admin index should not reference legacy /admin/js/ assets")
	}
	pattern := regexp.MustCompile(`<script\s+src="/admin/([^"]+)"`)
	matches := pattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		t.Fatal("no /admin script refs found in admin index")
	}
	for _, match := range matches {
		name := match[1]
		// Strip cache-busting query string (?v=...).
		if q := strings.IndexByte(name, '?'); q >= 0 {
			name = name[:q]
		}
		if strings.Contains(name, "/") || strings.Contains(name, `\\`) {
			t.Fatalf("script ref should point to top-level admin asset, got %q", name)
		}
		assetPath := filepath.Join("..", "..", "web", "admin", name)
		if _, err := os.Stat(assetPath); err != nil {
			t.Fatalf("script asset %q missing: %v", name, err)
		}
	}
}

func TestHubProfessionalStylesheetsLinkedAndExist(t *testing.T) {
	// href may include a cache-busting query string.
	pages := map[string]string{
		"admin":             "/admin/professional.css",
		"bind":              "/bind/professional.css",
		"get-credits":       "/get-credits/professional.css",
		"approval_workflow": "/approval_workflow/professional.css",
		"connector":         "/connector/professional.css",
	}
	for dir, href := range pages {
		indexPath := filepath.Join("..", "..", "web", dir, "index.html")
		body, err := os.ReadFile(indexPath)
		if err != nil {
			t.Fatalf("read %s index: %v", dir, err)
		}
		content := string(body)
		if !strings.Contains(content, `href="`+href) {
			t.Fatalf("%s index missing professional stylesheet %s", dir, href)
		}
		cssPath := filepath.Join("..", "..", "web", dir, "professional.css")
		if _, err := os.Stat(cssPath); err != nil {
			t.Fatalf("%s professional stylesheet missing: %v", dir, err)
		}
	}
}

func TestHubProfessionalStylesheetsAreAsciiAndTokenized(t *testing.T) {
	for _, dir := range []string{"admin", "bind", "get-credits", "approval_workflow", "connector"} {
		cssPath := filepath.Join("..", "..", "web", dir, "professional.css")
		body, err := os.ReadFile(cssPath)
		if err != nil {
			t.Fatalf("read %s professional stylesheet: %v", dir, err)
		}
		for i, b := range body {
			if b > 127 {
				t.Fatalf("%s professional stylesheet contains non-ASCII byte at offset %d", dir, i)
			}
		}
		content := string(body)
		for _, want := range []string{"#2563eb", ":focus-visible"} {
			if !strings.Contains(content, want) {
				t.Fatalf("%s professional stylesheet missing %s", dir, want)
			}
		}
	}
}

func TestConnectorPageDocumentsServerOwnedMediaProtocol(t *testing.T) {
	indexPath := filepath.Join("..", "..", "web", "connector", "index.html")
	body, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read connector index: %v", err)
	}
	content := string(body)
	for _, want := range []string{
		`/media/upload-url`,
		`server_media_upload`,
		`server_media_download`,
		`maxDirectBytes`,
		`maxBodyBytes`,
		`maxMediaBytes`,
		`mediaToken`,
		`message.id`,
		`attachments[].id`,
		`message.fileName`,
		`does not replace <code>id</code>, <code>data</code>, or server <code>url</code>`,
		`Client Tool Execution Extension`,
		`client_tools`,
		`tool_call`,
		`tool_plan`,
		`POST /tool-result`,
		`resultId`,
		`idempotencyKey`,
		`requiresApproval`,
		`"risk": "write"`,
		`success</code>, <code>error</code>, <code>rejected</code>, <code>cancelled</code>, or <code>timeout`,
		`replyToMessageId`,
		`nextCursor`,
		`The client must not provide its own download URL for the server to fetch.`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("connector page missing protocol contract %q", want)
		}
	}
	for _, stale := range []string{
		`"protocolVersion": "1.0"`,
		`"sender":`,
		`"timestamp":`,
		`"replyToEventId":`,
		`riskLevel`,
		`stopOnError`,
	} {
		if strings.Contains(content, stale) {
			t.Fatalf("connector page still contains stale protocol field %q", stale)
		}
	}

	cssPath := filepath.Join("..", "..", "web", "connector", "professional.css")
	css, err := os.ReadFile(cssPath)
	if err != nil {
		t.Fatalf("read connector css: %v", err)
	}
	cssContent := string(css)
	for _, want := range []string{
		`body{margin:0`,
		`main{max-width:1040px;margin:0 auto`,
		`.lang-panel{display:none}`,
		`.lang-panel.active{display:block}`,
		`pre{white-space:pre-wrap;word-break:break-word}`,
	} {
		if !strings.Contains(cssContent, want) {
			t.Fatalf("connector css missing contract %q", want)
		}
	}
}

func TestHubStaticPagesKeepAccessibilityContracts(t *testing.T) {
	read := func(t *testing.T, parts ...string) string {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(append([]string{"..", "..", "web"}, parts...)...))
		if err != nil {
			t.Fatalf("read static asset: %v", err)
		}
		return string(body)
	}

	bind := read(t, "bind", "index.html")
	for _, want := range []string{
		`role="tablist" aria-label="Binding workflows"`,
		`type="button" role="tab" data-tab="bind" id="tabButton-bind"`,
		`aria-controls="tab-query"`,
		`role="tabpanel" aria-labelledby="tabButton-unbind"`,
		`role="radiogroup" aria-label="Verification channel"`,
		`type="button" role="radio" aria-checked="true"`,
		`role="status" aria-live="polite"`,
		`function selectTab(btn)`,
		`b.setAttribute('aria-selected', active ? 'true' : 'false')`,
		`b.tabIndex = active ? 0 : -1`,
		`function selectChannel(btn)`,
		`b.setAttribute('aria-checked', active ? 'true' : 'false')`,
	} {
		if !strings.Contains(bind, want) {
			t.Fatalf("bind page missing accessibility contract %q", want)
		}
	}

	approval := read(t, "approval_workflow", "index.html")
	for _, want := range []string{
		`<button type="button" id="btnNew"><span data-i18n="newWorkflow">New</span></button>`,
		`<button type="button" id="btnValidate">`,
		`<button type="button" id="btnSubmit" class="btn-primary">`,
		`<input class="workflow-meta-field name" id="workflowName" data-i18n-placeholder="workflowNamePlaceholder"`,
		`<input class="workflow-meta-field description" id="workflowDescription" data-i18n-placeholder="workflowDescriptionPlaceholder"`,
		`<span class="workflow-status" id="workflowStatus" role="status" aria-live="polite" data-i18n="statusDraft">`,
		`<section class="workflow-library" aria-label="Workflow designs" data-i18n-aria="workflowLibrary">`,
		`id="btnRefreshWorkflows" data-i18n="refreshWorkflows"`,
		`id="workflowSearch" data-i18n-placeholder="workflowSearchPlaceholder"`,
		`id="workflowStatusFilter" aria-label="Workflow status" data-i18n-aria="workflowStatusFilter"`,
		`<option value="published" data-i18n="statusPublishedShort">Published</option>`,
		`<option value="superseded" data-i18n="statusSupersededShort">Superseded</option>`,
		`<option value="unknown" data-i18n="statusUnknownShort">Unknown</option>`,
		`id="workflowList" role="list" aria-live="polite"`,
		`<button type="button" class="canvas-tool-btn active" id="toolSelect" data-tool-mode="select" data-i18n="selectTool">`,
		`<button type="button" class="canvas-tool-btn" id="toolConnect" data-tool-mode="connect" data-i18n="connectTool">`,
		`<button type="button" class="canvas-tool-btn" id="toolDeleteEdge" data-tool-mode="delete_edge" data-i18n="deleteEdgeTool">`,
		`<div class="review-preview-banner" id="reviewPreviewBanner" hidden data-i18n="reviewPreviewMode">`,
		`.canvas-tool-btn:disabled`,
		`<script src="/approval_workflow/i18n.js"></script>`,
		`<div class="lang-switch" role="group" aria-label="Language" data-i18n-aria="language">`,
		`data-set-lang="zh"`,
		`data-set-lang="en"`,
		`role="button" tabindex="0" data-node-type="trigger"`,
		`role="button" tabindex="0" data-node-type="terminal"`,
		`aria-label="Close node configuration"`,
		`data-i18n="nodeTypes"`,
		`.config-field textarea.config-field-invalid`,
	} {
		if !strings.Contains(approval, want) {
			t.Fatalf("approval workflow page missing accessibility contract %q", want)
		}
	}

	approvalI18n := read(t, "approval_workflow", "i18n.js")
	for _, want := range []string{
		`maclaw-approval-workflow-lang`,
		`pageTitle: 'Approval Workflow Designer'`,
		`edgeConnectorLabel: 'Connector from {source} to {target}'`,
		`reviewPreviewMode: 'Review preview mode. This workflow is read-only here.'`,
		`adminAuthRequired: 'Admin authorization required. Sign in to the Hub admin console first.'`,
		`pageTitle: '审批工作流设计器'`,
		`newWorkflowConfirm: '要新建工作流吗？未保存的更改将会丢失。'`,
		`openWorkflowConfirm: '要打开这个工作流设计吗？未保存的更改将会丢失。'`,
		`workflowLibrary: '我的工作流'`,
		`workflowSearchPlaceholder: '搜索工作流'`,
		`workflowStatusAll: '全部状态'`,
		`workflowListNoMatches: '没有符合筛选条件的工作流设计。'`,
		`statusUnknownShort: '未知'`,
		`deleteWorkflowConfirm: '要删除工作流设计“{name}”吗？这里不会删除已发布到能力市场的 skill。'`,
		`deleteWorkflowBlocked: '已发布或曾经发布的工作流不能在设计器中删除。'`,
		`deleteWorkflowUnavailable: '无法确认发布历史。删除前请先刷新。'`,
		`workflowVersionUnknown: '版本历史不可用'`,
		`submitReview: '提交审核'`,
		`workflowNamePlaceholder: '工作流名称'`,
		`statusLoaded: '已加载 {version}'`,
		`statusUnsaved: '有未保存更改'`,
		`statusPendingReview: '待审核 {version}'`,
		`authRequired: '需要机器授权。请使用 machine_id 和 token 查询参数打开，或先写入 localStorage。'`,
		`nodeTypes: '节点类型'`,
		`mustBeBetween: '必须介于 {min} 和 {max} 之间'`,
		`window.ApprovalWorkflowI18n`,
		`hasTranslation: hasTranslation`,
	} {
		if !strings.Contains(approvalI18n, want) {
			t.Fatalf("approval workflow i18n missing contract %q", want)
		}
	}

	terminalConfig := read(t, "approval_workflow", "terminal-node-config.js")
	for _, want := range []string{
		`window.attachTerminalNodeConfigListeners = function (node, searchUsers, onChange)`,
		`var markChanged = typeof onChange === 'function' ? onChange : function () {};`,
		`attachItemListeners(node, 'executor', markChanged);`,
		`parseInt(btn.getAttribute('data-index'), 10)`,
		`markChanged();`,
		`var items = type === 'executor' ? config.result_executors : config.notifiers;`,
		`input.setAttribute('aria-invalid', error !== '' ? 'true' : 'false');`,
	} {
		if !strings.Contains(terminalConfig, want) {
			t.Fatalf("approval workflow terminal config missing contract %q", want)
		}
	}

	approvalCSS := read(t, "approval_workflow", "professional.css")
	for _, want := range []string{
		`.workflow-app{grid-template-columns:260px 1fr auto`,
		`.workflow-header,.node-palette,.config-panel`,
		`.config-panel{box-shadow`,
	} {
		if !strings.Contains(approvalCSS, want) {
			t.Fatalf("approval workflow professional css missing contract %q", want)
		}
	}
	if strings.Contains(approvalCSS, `.properties-panel`) {
		t.Fatal("approval workflow professional css should target config-panel, not stale properties-panel")
	}

	workflowEditorTest := read(t, "approval_workflow", "workflow-editor.test.js")
	for _, want := range []string{
		`selects highest semantic version before timestamp`,
		`compares numeric patch values`,
		`uses timestamp only as same-version tie breaker`,
		`shows unknown status when version history failed`,
		`blocks delete when published history exists`,
	} {
		if !strings.Contains(workflowEditorTest, want) {
			t.Fatalf("approval workflow editor test missing contract %q", want)
		}
	}

	editor := read(t, "approval_workflow", "workflow-editor.js")
	for _, want := range []string{
		`function addNodeToCanvas(nodeType, position)`,
		`el.setAttribute('role', 'button');`,
		`el.tabIndex = 0;`,
		`el.setAttribute('aria-pressed', 'false');`,
		`el.setAttribute('aria-label', node.label + ' ' + nodeTypeLabel(node.type));`,
		`pageTitle: 'Approval Workflow Designer'`,
		`state.reviewVersionId = getUrlParam('review_version_id') || null;`,
		`async function loadWorkflowReviewPreview()`,
		`adminWorkflowApi('/api/v1/admin/reviews/' + encodeURIComponent(state.reviewVersionId))`,
		`function setReadOnlyPreviewMode(enabled)`,
		`function isReadOnlyPreview()`,
		`getAdminToken()`,
		`maclawHubAdminToken`,
		`tr('reviewPreviewStatus', { version: version })`,
		`function resetWorkflowDesigner()`,
		`storageRemove('maclaw-approval-workflow-id');`,
		`btnNew.addEventListener('click', function ()`,
		`confirm(tr('newWorkflowConfirm'))`,
		`confirm(tr('openWorkflowConfirm'))`,
		`if (hasUnsavedChanges() && !confirm(tr('newWorkflowConfirm'))) return;`,
		`if (hasUnsavedChanges() && !confirm(tr('openWorkflowConfirm'))) return;`,
		`async function workflowApi(path, options)`,
		`'X-Machine-ID': auth.machineID`,
		`headers.Authorization = 'Bearer ' + auth.token;`,
		`apiErr.code = data.code || '';`,
		`if (err && err.code === 'VALIDATION_FAILED')`,
		`async function ensureWorkflowDefinition()`,
		`async function syncWorkflowDefinition()`,
		`method: 'PUT'`,
		`async function loadWorkflowFromApi(workflowId)`,
		`var targetWorkflowId = workflowId || state.workflowId;`,
		`state.workflowId = targetWorkflowId;`,
		`storageSet('maclaw-approval-workflow-id', targetWorkflowId);`,
		`return false;`,
		`return true;`,
		`async function loadWorkflowLibrary()`,
		`async function openWorkflowDesign(workflowId)`,
		`async function deleteWorkflowDesign(workflowId)`,
		`function workflowHasPublishedHistory(workflowId)`,
		`function workflowVersionBlocksDesignerDelete(status)`,
		`function workflowVersionHistoryUnavailable(workflowId)`,
		`if (workflowVersionHistoryUnavailable(workflowId)) return 'unknown';`,
		`unknown: 'statusUnknownShort'`,
		`versionsById[wf.id] = null;`,
		`alert(tr('deleteWorkflowUnavailable'))`,
		`status === 'published' || status === 'superseded' || status === 'unpublished'`,
		`function workflowStatusLabel(status)`,
		`function workflowPrimaryActionLabel(status)`,
		`function filteredWorkflowSummaries()`,
		`<div role="listitem" class="workflow-library-empty">`,
		`<div role="listitem" class="' + (isError ? 'workflow-library-error' : 'workflow-library-empty') + '">`,
		`function markDirty()`,
		`function clearDirty(savedRevision)`,
		`function hasUnsavedChanges()`,
		`isBusy: false`,
		`isLibraryLoading: false`,
		`libraryRequestId: 0`,
		`state.isBusy = !!isBusy;`,
		`function setControlDisabled(control, disabled)`,
		`control.setAttribute('aria-disabled', disabled ? 'true' : 'false');`,
		`function workflowLibraryControlsDisabled()`,
		`return !!(state.isBusy || state.isLibraryLoading || isReadOnlyPreview());`,
		`var requestId = ++state.libraryRequestId;`,
		`if (requestId !== state.libraryRequestId) return;`,
		`var versionsById = {};`,
		`if (workflowLibraryControlsDisabled()) return;`,
		`var openIsDisabled = workflowLibraryControlsDisabled();`,
		`var openDisabled = openIsDisabled ? ' disabled aria-disabled="true"' : ' aria-disabled="false"';`,
		`function updateDocumentTitle()`,
		`document.title = (state.isDirty ? '* ' : '') + tr('pageTitle');`,
		`state.isDirty = true;`,
		`dirtyRevision: 0`,
		`state.dirtyRevision++;`,
		`if (savedRevision !== undefined && savedRevision !== state.dirtyRevision)`,
		`var savedRevision = state.dirtyRevision;`,
		`var graph = getWorkflowGraph();`,
		`clearDirty(savedRevision);`,
		`alert(tr('submitChangedDuringSave'))`,
		`invalidConfigFields: {}`,
		`state.invalidConfigFields[fieldKey] = el.previousElementSibling && el.previousElementSibling.textContent || id;`,
		`function clearInvalidConfigFieldsForNode(nodeId)`,
		`clearInvalidConfigFieldsForNode(nodeId);`,
		`function getInvalidConfigErrors()`,
		`tr('invalidJsonField', { field: state.invalidConfigFields[key] || key })`,
		`invalidConfigField: 'Invalid value in {field}.'`,
		`? 'invalidJsonField' : 'invalidConfigField'`,
		`configPanelBody.querySelectorAll('[aria-invalid="true"], .terminal-field-invalid, .config-field-invalid')`,
		`function configFieldLabel(el)`,
		`updateVersionStatus('dirty');`,
		`dirty: 'statusUnsaved'`,
		`workflowStatusLabel(latest.status || 'draft')`,
		`historyUnavailable ? tr('workflowVersionUnknown')`,
		`return tr('reviseWorkflow');`,
		`return tr('continueWorkflow');`,
		`workflow-library-status`,
		`workflowSearchInput.addEventListener('input', function ()`,
		`workflowStatusFilter.addEventListener('change', function ()`,
		`state.workflowStatusFilter = workflowStatusFilter.value || '';`,
		`workflowApi('/api/v1/workflows');`,
		`await workflowApi('/api/v1/workflows/' + encodeURIComponent(workflowId), { method: 'DELETE' });`,
		`workflowList.addEventListener('click', function (e)`,
		`if (workflowHasPublishedHistory(workflowId))`,
		`await workflowApi('/api/v1/workflows/' + encodeURIComponent(targetWorkflowId));`,
		`await workflowApi('/api/v1/workflows/' + encodeURIComponent(targetWorkflowId) + '/versions');`,
		`applyWorkflowVersion(ver);`,
		`applyWorkflowGraph(ver.graph);`,
		`function applyWorkflowGraph(graph)`,
		`state.nextNodeId = nextNumberFromIds(state.nodes, 'node');`,
		`loadWorkflowFromApi();`,
		`function compareWorkflowVersions(a, b)`,
		`function parseVersionNumber(version)`,
		`parseInt(part, 10)`,
		`await workflowApi('/api/v1/workflows'`,
		`await workflowApi('/api/v1/workflows/' + encodeURIComponent(workflowID) + '/versions'`,
		`'/submit'`,
		`function updateVersionStatus(statusKey)`,
		`function reachableNodeIds(triggerId)`,
		`var reachable = reachableNodeIds(triggerNodes[0].id);`,
		`if (edge.source_id !== current || reachable[edge.target_id]) return;`,
		`el.addEventListener('keydown', function (e)`,
		`if (e.key !== 'Enter' && e.key !== ' ') return;`,
		`handleConnectNodeClick(node.id, el);`,
		`addNodeToCanvas(el.getAttribute('data-node-type')`,
		`window.addEventListener('approval-workflow-language-change', refreshLocalizedUI);`,
		`workflowNameInput.addEventListener('input', markDirty);`,
		`workflowDescriptionInput.addEventListener('input', markDirty);`,
		`window.addEventListener('beforeunload', function (e)`,
		`if (!hasUnsavedChanges()) return;`,
		`e.returnValue = tr('newWorkflowConfirm');`,
		`window.attachTerminalNodeConfigListeners(node, null, markDirty);`,
		`renderWorkflowLibrary();`,
		`function setToolMode(mode)`,
		`if (isToolModeDisabled(mode)) mode = 'select';`,
		`if (isToolModeDisabled(state.toolMode)) {`,
		`if (mode === 'connect') return state.nodes.length < 2;`,
		`if (mode === 'delete_edge') return state.edges.length === 0;`,
		`if (mode !== 'select') clearSelectedNode();`,
		`function selectNode(nodeId)`,
		`if (state.selectedNodeId && state.selectedNodeId !== nodeId) clearInvalidConfigFieldsForNode(state.selectedNodeId);`,
		`state.selectedEdgeId = null;`,
		`function updateCanvasNodeSelection()`,
		`el.setAttribute('aria-pressed', selected ? 'true' : 'false');`,
		`nodeFrame.setAttribute('aria-label', node.label + ' ' + nodeTypeLabel(node.type));`,
		`if (state.nodes.length < 2) return;`,
		`btn.disabled = disabled;`,
		`if (state.toolMode !== 'select') setToolMode('select');`,
		`function clearSelectedNode()`,
		`function deselectNode()`,
		`if (state.selectedNodeId) clearInvalidConfigFieldsForNode(state.selectedNodeId);`,
		`clearConnectingState();`,
		`function deleteEdge(edgeId)`,
		`if (state.connectingFrom === nodeId) clearConnectingState();`,
		`function isEditingField()`,
		`function bindJsonTextarea(id, cb)`,
		`el.classList.add('config-field-invalid');`,
		`el.classList.remove('config-field-invalid');`,
		`el.setAttribute('aria-invalid', 'true');`,
		`el.setAttribute('aria-invalid', 'false');`,
		`parseInt(v, 10)`,
		`e.preventDefault();`,
		`edge-hit-path`,
		`hitPath.setAttribute('role', 'button');`,
		`hitPath.setAttribute('tabindex', state.toolMode === 'connect' ? '-1' : '0');`,
		`hitPath.setAttribute('aria-disabled', state.toolMode === 'connect' ? 'true' : 'false');`,
		`hitPath.setAttribute('aria-label', edgeAriaLabel(edge));`,
		`hitPath.addEventListener('keydown', function (e)`,
		`if (state.toolMode === 'connect') return;`,
		`if (state.toolMode === 'delete_edge')`,
		`if (state.toolMode === 'delete_edge') return;`,
	} {
		if !strings.Contains(editor, want) {
			t.Fatalf("approval workflow editor missing keyboard contract %q", want)
		}
	}

	admin := read(t, "admin", "index.html")
	for _, want := range []string{
		`<nav class="nav" aria-label="Admin sections">`,
		`<main class="main" tabindex="-1">`,
		`<div class="lang-switch" aria-label="Language">`,
	} {
		if !strings.Contains(admin, want) {
			t.Fatalf("hub admin shell missing accessibility contract %q", want)
		}
	}

	adminUI := read(t, "admin", "admin-ui.js")
	for _, want := range []string{
		`if (!nextAttrs.type) nextAttrs.type = 'button';`,
		`function enhanceButtonTypes(root)`,
		`scope.querySelectorAll('button:not([type])').forEach`,
		`function enhanceLanguageSwitchStates(root)`,
		`button.setAttribute('aria-pressed', button.classList.contains('active') ? 'true' : 'false')`,
		`function enhanceAdminNavigation(root)`,
		`button.setAttribute('aria-current', active ? 'page' : 'false')`,
		`button.tabIndex = active ? 0 : -1`,
		`function bindAdminNavigationKeyboard()`,
		`event.target.closest('.nav')`,
		`buttons[next].click()`,
		`observer.observe(global.document.documentElement, { childList: true, subtree: true, attributes: true, attributeFilter: ['class'] })`,
	} {
		if !strings.Contains(adminUI, want) {
			t.Fatalf("hub admin UI helper missing accessibility contract %q", want)
		}
	}
}
