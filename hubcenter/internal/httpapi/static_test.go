package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveStaticDirFromBasesPrefersExistingBase(t *testing.T) {
	root := t.TempDir()
	exeDir := filepath.Join(root, "bin")
	adminDir := filepath.Join(exeDir, "web", "admin")
	if err := os.MkdirAll(adminDir, 0o755); err != nil {
		t.Fatalf("mkdir admin dir: %v", err)
	}

	got := resolveStaticDirFromBases("./web/admin", []string{
		filepath.Join(root, "missing"),
		exeDir,
	})
	want := filepath.Clean(adminDir)
	if got != want {
		t.Fatalf("resolveStaticDirFromBases() = %q, want %q", got, want)
	}
}

func TestRegisterSharedStaticAssetsServesProUI(t *testing.T) {
	root := t.TempDir()
	css := filepath.Join(root, "pro-ui.css")
	if err := os.WriteFile(css, []byte("body{color:#202938}"), 0o644); err != nil {
		t.Fatalf("write css: %v", err)
	}

	mux := http.NewServeMux()
	registerSharedStaticAssets(mux, root)

	req := httptest.NewRequest(http.MethodGet, "/pro-ui.css", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "body{color:#202938}" {
		t.Fatalf("unexpected css body %q", rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/css") {
		t.Fatalf("content-type = %q, want text/css", contentType)
	}
	assertStaticAssetHeaders(t, rec)
}

func TestAdminStaticRoutesServeSplitAssets(t *testing.T) {
	root := t.TempDir()
	assetsDir := filepath.Join(root, "assets")
	if err := os.MkdirAll(filepath.Join(assetsDir, "css"), 0o755); err != nil {
		t.Fatalf("mkdir css assets: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(assetsDir, "js"), 0o755); err != nil {
		t.Fatalf("mkdir js assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html>admin shell</html>"), 0o644); err != nil {
		t.Fatalf("write admin index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "settings.html"), []byte("<html>settings</html>"), 0o644); err != nil {
		t.Fatalf("write admin html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "css", "admin-shell.css"), []byte(".hub-route-list{display:grid}"), 0o644); err != nil {
		t.Fatalf("write admin css: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "js", "admin-core.js"), []byte("const hubRoutePreviewPageSize=20;"), 0o644); err != nil {
		t.Fatalf("write admin js: %v", err)
	}

	mux := http.NewServeMux()
	registerAdminStaticRoutes(mux, root, "/admin")

	for _, tc := range []struct {
		path            string
		want            string
		wantContentType string
	}{
		{path: "/admin/assets/css/admin-shell.css", want: ".hub-route-list{display:grid}", wantContentType: "text/css"},
		{path: "/admin/assets/js/admin-core.js", want: "const hubRoutePreviewPageSize=20;", wantContentType: "text/javascript"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertStatus(t, rec, http.StatusOK)
			if body := rec.Body.String(); body != tc.want {
				t.Fatalf("body = %q, want split asset %q", body, tc.want)
			}
			if contentType := rec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, tc.wantContentType) {
				t.Fatalf("content-type = %q, want %s", contentType, tc.wantContentType)
			}
			assertAdminStaticAssetHeaders(t, rec)
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/settings.html", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assertStatus(t, rec, http.StatusOK)
	assertStaticHTMLHeaders(t, rec)

	req = httptest.NewRequest(http.MethodGet, "/admin/assets/js/missing.js", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assertStatus(t, rec, http.StatusNotFound)

	req = httptest.NewRequest(http.MethodGet, "/admin/deep/link", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assertStatus(t, rec, http.StatusOK)
	if body := rec.Body.String(); body != "<html>admin shell</html>" {
		t.Fatalf("spa fallback body = %q, want index html", body)
	}
	assertStaticHTMLHeaders(t, rec)
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d", rec.Code, want)
	}
}

func assertStaticAssetHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if cacheControl := rec.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "max-age=300") {
		t.Fatalf("cache-control = %q, want max-age=300", cacheControl)
	}
	if nosniff := rec.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("x-content-type-options = %q, want nosniff", nosniff)
	}
}

func assertAdminStaticAssetHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != staticHTMLCacheControl {
		t.Fatalf("admin asset cache-control = %q, want %q", cacheControl, staticHTMLCacheControl)
	}
	if nosniff := rec.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("x-content-type-options = %q, want nosniff", nosniff)
	}
}

func assertStaticHTMLHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != staticHTMLCacheControl {
		t.Fatalf("html cache-control = %q, want %q", cacheControl, staticHTMLCacheControl)
	}
	if nosniff := rec.Header().Get("X-Content-Type-Options"); nosniff != "" {
		t.Fatalf("html x-content-type-options = %q, want empty", nosniff)
	}
}

func TestWebPagesIncludeSharedProUI(t *testing.T) {
	webRoot := filepath.Clean(filepath.Join("..", "..", "web"))
	cssPath := filepath.Join(webRoot, "pro-ui.css")
	css, err := os.ReadFile(cssPath)
	if err != nil {
		t.Fatalf("read shared css: %v", err)
	}
	if len(strings.TrimSpace(string(css))) == 0 {
		t.Fatalf("shared css %s is empty", cssPath)
	}

	pages := []string{
		filepath.Join("admin", "index.html"),
		filepath.Join("gossip", "index.html"),
		filepath.Join("skillhub", "index.html"),
		filepath.Join("skillmarket", "index.html"),
		filepath.Join("skillmarket", "user", "index.html"),
	}
	for _, page := range pages {
		t.Run(page, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(webRoot, page))
			if err != nil {
				t.Fatalf("read page: %v", err)
			}
			if !strings.Contains(string(body), `href="/pro-ui.css"`) {
				t.Fatalf("page does not include shared pro UI stylesheet")
			}
		})
	}
}

func TestPackageScriptCopiesFullWebTree(t *testing.T) {
	scriptPath := filepath.Clean(filepath.Join("..", "..", "scripts", "package.ps1"))
	body, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read package script: %v", err)
	}
	script := string(body)
	if !strings.Contains(script, `Join-Path $root "web"`) || !strings.Contains(script, `Join-Path $pkgRoot "web"`) {
		t.Fatalf("package script must copy the full web tree")
	}
	if strings.Contains(script, `web\admin`) {
		t.Fatalf("package script must not package only web admin assets")
	}
}

func TestWebPagesKeepInteractiveAccessibilityContracts(t *testing.T) {
	webRoot := filepath.Clean(filepath.Join("..", "..", "web"))

	read := func(t *testing.T, parts ...string) string {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(append([]string{webRoot}, parts...)...))
		if err != nil {
			t.Fatalf("read page: %v", err)
		}
		return string(body)
	}

	admin := readAdminPageBundle(t)
	if !strings.Contains(admin, `id="toastStack" class="toast-stack" aria-live="polite" aria-atomic="true"`) {
		t.Fatalf("admin toast stack must announce status changes")
	}
	if !strings.Contains(admin, `class="lang-switch" aria-label="Language"`) || !strings.Contains(admin, `btn.setAttribute('aria-pressed'`) {
		t.Fatalf("admin language switcher must expose pressed state")
	}
	for _, required := range []string{
		`aria-pressed="'+(mode==='daily'?'true':'false')+'"`,
		`aria-pressed="'+(mode==='monthly'?'true':'false')+'"`,
		`document.getElementById('gossipFilterAll').setAttribute('aria-pressed'`,
		`document.getElementById('gossipFilterFlagged').setAttribute('aria-pressed'`,
		`document.getElementById('catalogSubTabSkill').setAttribute('aria-pressed'`,
		`document.getElementById('catalogSubTabMCP').setAttribute('aria-pressed'`,
		`document.getElementById('mcpTypeRemoteBtn').setAttribute('aria-pressed'`,
		`document.getElementById('mcpTypeLocalBtn').setAttribute('aria-pressed'`,
	} {
		if !strings.Contains(admin, required) {
			t.Fatalf("admin segmented control missing pressed state contract %q", required)
		}
	}
	if !strings.Contains(admin, `v.setAttribute('aria-current','page')`) || !strings.Contains(admin, `v.removeAttribute('aria-current')`) {
		t.Fatalf("admin navigation must expose the current page")
	}
	if !strings.Contains(admin, `toast.setAttribute('role',type==='error'?'alert':'status')`) {
		t.Fatalf("admin toasts must expose status/alert roles")
	}
	for _, id := range []string{"loginOutput", "output"} {
		if !strings.Contains(admin, `id="`+id+`" class="console" role="status" aria-live="polite"`) {
			t.Fatalf("admin console %s must announce async status text", id)
		}
	}
	if !strings.Contains(admin, `function enhanceFormAccessibility`) {
		t.Fatalf("admin page must keep generated form controls labeled")
	}
	if !strings.Contains(admin, `function enhanceButtonTypes`) || !strings.Contains(admin, `root.querySelectorAll('button:not([type])').forEach`) || !strings.Contains(admin, `enhanceFormAccessibility(node);enhanceButtonTypes(node)`) || !strings.Contains(admin, `applyI18n();enhanceFormAccessibility();enhanceButtonTypes();`) || !strings.Contains(admin, `btn.type='button';`) || !strings.Contains(admin, `if(typeof enhanceButtonTypes==='function')enhanceButtonTypes();`) {
		t.Fatalf("admin page must normalize implicit submit buttons")
	}
	for _, required := range []string{
		`id="stageToggle" class="stage-toggle hidden" role="tablist"`,
		`id="setupStageButton" class="btn-secondary" type="button" role="tab" aria-controls="setupStage" aria-selected="true" aria-pressed="true"`,
		`id="loginStageButton" class="btn-ghost" type="button" role="tab" aria-controls="loginStage" aria-selected="false" aria-pressed="false"`,
		`id="setupStage" class="stage-card hidden" role="tabpanel" aria-labelledby="setupStageButton"`,
		`id="loginStage" class="stage-card hidden" role="tabpanel" aria-labelledby="loginStageButton"`,
		`setupBtn.setAttribute('aria-selected',setup?'true':'false')`,
		`loginBtn.setAttribute('aria-pressed',setup?'false':'true')`,
		`function enhanceAuthStageTabs`,
		`enhanceStatusHints();enhanceAuthStageTabs();`,
		`id="gossipFilterAll" aria-pressed="true"`,
		`id="gossipFilterFlagged" aria-pressed="false"`,
		`document.getElementById('gossipFilterAll').setAttribute('aria-pressed',f===''?'true':'false')`,
		`id="catalogSubTabSkill" aria-pressed="true"`,
		`id="catalogSubTabMCP" aria-pressed="false"`,
		`document.getElementById('catalogSubTabSkill').setAttribute('aria-pressed', tab === 'skill' ? 'true' : 'false')`,
		`id="mcpTypeRemoteBtn" aria-pressed="true"`,
		`id="mcpTypeLocalBtn" aria-pressed="false"`,
		`document.getElementById('mcpTypeRemoteBtn').setAttribute('aria-pressed', type === 'remote' ? 'true' : 'false')`,
		`aria-pressed="'+(mode==='daily'?'true':'false')+'" onclick="setUserMgmtReportMode`,
		`class="user-mgmt-layout"`,
		`class="user-mgmt-migration-grid"`,
		`class="user-mgmt-card-grid`,
		`const userMgmtPageSize=8`,
		`function userMgmtPager`,
		`function renderUserMgmtMatchedHubs`,
		`const tenantScoped=!!(item&&item.tenant_id)`,
		`else if(tenantScoped){domains=Array.isArray(item&&item.corporate_email_domains)?item.corporate_email_domains.filter(Boolean):[]}`,
		`if(item&&(item.accept_public_signup||item.signup_mode==='public_signup'))`,
		`function renderHubTenantPanel`,
		`function hubTenantsForAdmin`,
		`tenant_id:'tenant_default'`,
		`hub_id:String(id||'')`,
		`api('/api/admin/hubs/visibility'`,
		`api('/api/admin/hubs/registration-policy'`,
		`api('/api/admin/hubs/digital-employee-authorization'`,
		`'/api/admin/hubs/disable':'/api/admin/hubs/enable'`,
		`api('/api/admin/hubs/confirm'`,
		`api('/api/admin/hubs',{method:'DELETE'`,
		`data.error&&data.error.message`,
		`No tenant data yet. Enter a tenant ID to grant digital employee authorization before first user inventory sync.`,
		`function updateHubVEAuthManual(id)`,
		`if(!tenantID){showToast(tr('veAuthTenantRequired'),'error');return}`,
		`var body={hub_id:String(id||''),tenant_id:tenantID,quota:quotaVal,years:yearsVal,enabled:true}`,
		`var body={hub_id:String(id||''),tenant_id:tenantID,enabled:false}`,
		`Array.isArray(data.logs)?data.logs`,
		`item.tenant_id==='tenant_default'?tr('defaultTenant'):item.tenant_id`,
		`function hubMailDomains`,
		`if(!single)return []`,
		`function hubTenantDomainText`,
		`function hubTenantOptionLabel`,
		`function setHubTenantSelection`,
		`id="hubTenantSelect-`,
		`select.disabled=shown===0`,
		`Mail domains are shown per tenant.`,
		`Digital employee authorization applies to the selected tenant.`,
		`h.tenant_name||h.tenant_id||'-'`,
		`routeQueryHubTenant:'Tenant'`,
		`${tr('routeQueryHubCardTitle')} ${idx+1}`,
		`${tr('routeQueryHubTenant')}:</strong> ${escapeHtml(tenant)}`,
		`function enhanceUserMgmtRegions`,
		`'userMgmtRegistrationReport','userMgmtDashboard','userMgmtFromHub','userMgmtResult'`,
		`el.setAttribute('role','status');el.setAttribute('aria-live','polite');el.setAttribute('aria-busy','false')`,
		`function setUserMgmtBusy`,
		`setUserMgmtBusy(['userMgmtRegistrationReport','userMgmtDashboard','userMgmtFromHub'],true)`,
		`setUserMgmtBusy(['userMgmtFromHub','userMgmtResult'],true)`,
		`setUserMgmtBusy(['userMgmtResult'],true)`,
	} {
		if !strings.Contains(admin, required) {
			t.Fatalf("admin user management async region missing accessibility contract %q", required)
		}
	}
	for _, required := range []string{
		`function enhanceStatusHints`,
		`'haConfigReadinessHint','haRuntimeConfigHint','haClusterSecretHint'`,
		`el.setAttribute('role','status');el.setAttribute('aria-live','polite')`,
		`applyI18n();enhanceFormAccessibility();enhanceButtonTypes();enhanceStatusHints();`,
		`id="haOverviewGrid" class="ha-overview-grid" role="status" aria-live="polite" aria-busy="false"`,
		`root.setAttribute('aria-busy', 'false');`,
		`if(overview) overview.setAttribute('aria-busy', 'true');`,
		`id="haSummaryList" role="status" aria-live="polite" aria-busy="false"`,
		`id="haPeerList" class="ha-peer-list" role="status" aria-live="polite" aria-busy="false"`,
		`id="haSyncDetailList" class="ha-sync-list" role="status" aria-live="polite" aria-busy="false"`,
		`[summary, peers, syncDetails].forEach(el => el.setAttribute('aria-busy', 'false'))`,
		`[summary, peers, syncDetails].forEach(el => { if(el) el.setAttribute('aria-busy', 'true'); })`,
		`id="routingDiagnosticsGrid" class="grid3" role="status" aria-live="polite" aria-busy="false"`,
		`root.setAttribute('aria-busy','false');const snapshot=data.snapshot`,
		`root.setAttribute('aria-busy','true');root.innerHTML='<div class="hint">'+tr('haLoading')+'</div>'`,
		`id="hubs" class="list" role="status" aria-live="polite" aria-busy="false"`,
		`const root=document.getElementById('hubs');if(root)root.setAttribute('aria-busy','true')`,
		`const root=document.getElementById('hubs');if(root)root.setAttribute('aria-busy','false')`,
		`id="blockedEmails" class="list" role="status" aria-live="polite" aria-busy="false"`,
		`id="blockedIPs" class="list" role="status" aria-live="polite" aria-busy="false"`,
		`const root=document.getElementById('blockedEmails');if(root)root.setAttribute('aria-busy','true')`,
		`const root=document.getElementById('blockedIPs');if(root)root.setAttribute('aria-busy','true')`,
		`id="routeQueryResult" class="route-query-empty hint" role="status" aria-live="polite" aria-busy="false"`,
		`id="failureLogsList" role="status" aria-live="polite" aria-busy="false"`,
		`id="failureLogsPagerMeta" class="pager-meta" aria-live="polite"`,
	} {
		if !strings.Contains(admin, required) {
			t.Fatalf("admin async result region missing accessibility contract %q", required)
		}
	}
	for _, required := range []string{
		`id="routeQueryResult" class="route-query-empty hint" role="status" aria-live="polite" aria-busy="false"`,
		`root.setAttribute('aria-busy','true');root.textContent=tr('haLoading')`,
		`root.setAttribute('aria-busy','false');const hubs=`,
		`id="failureLogsList" role="status" aria-live="polite" aria-busy="false"`,
		`if(root)root.setAttribute('aria-busy','true');try{const data=await api('/api/admin/failure-logs?'+params.toString())`,
		`id="failureLogsPagerMeta" class="pager-meta" aria-live="polite"`,
		`id="gossipPrevBtn" type="button"`,
		`aria-label="Previous gossip page"`,
		`id="gossipPageInfo" aria-live="polite"`,
		`id="skillhubPrevBtn" type="button"`,
		`aria-label="Next SkillHub page"`,
		`id="skillhubPageInfo" aria-live="polite"`,
		`id="smPurchasePrevBtn" type="button"`,
		`aria-label="Next purchases page"`,
		`id="smPurchasePageInfo" aria-live="polite"`,
		`id="newsPrevBtn" type="button"`,
		`aria-label="Next news page"`,
		`id="newsPageInfo" aria-live="polite"`,
		`aria-label="Previous comments page"`,
		`aria-label="Next comments page"`,
	} {
		if !strings.Contains(admin, required) {
			t.Fatalf("admin pager icon button missing accessibility contract %q", required)
		}
	}
	for _, required := range []string{
		`role="dialog" aria-modal="true" aria-labelledby="`,
		`overlay.addEventListener('keydown'`,
		`e.key==='Escape'`,
		`veAuthModalReturnFocus.focus()`,
		`enhanceFormAccessibility(overlay)`,
	} {
		if !strings.Contains(admin, required) {
			t.Fatalf("admin VE auth modal missing accessibility contract %q", required)
		}
	}

	user := read(t, "skillmarket", "user", "index.html")
	if strings.Contains(user, `<span class="captcha-refresh"`) {
		t.Fatalf("captcha refresh controls must be real buttons")
	}
	if !strings.Contains(user, `document.querySelectorAll('.auth-toggle').forEach`) {
		t.Fatalf("auth mode links must keep keyboard activation support")
	}
	if !strings.Contains(user, `function bindTablistKeyboard`) || !strings.Contains(user, `bindTablistKeyboard('[aria-label="Authentication modes"]','[data-auth-tab]')`) || !strings.Contains(user, `bindTablistKeyboard('[aria-label="Workspace sections"]','.tab')`) {
		t.Fatalf("user console tablists must keep arrow-key keyboard navigation")
	}
	for _, required := range []string{
		`id="auth-tab-register" class="auth-tab" type="button" data-auth-tab="register" onclick="showAuthMode('register')" data-i18n="auth.register.short" role="tab" aria-selected="false" aria-controls="auth-mode-register" tabindex="-1"`,
		`id="workspace-tab-myskills" class="tab" type="button" data-panel="myskills" data-i18n="tab.myskills" role="tab" aria-selected="false" aria-controls="panel-myskills" tabindex="-1"`,
		`tab.tabIndex = active ? 0 : -1`,
		`b.tabIndex = active ? 0 : -1`,
	} {
		if !strings.Contains(user, required) {
			t.Fatalf("user tablist missing roving tabindex contract %q", required)
		}
	}
	if !strings.Contains(user, `id="workspace-tab-account" class="tab active" type="button"`) || !strings.Contains(user, `id="workspace-tab-myskills" class="tab" type="button"`) {
		t.Fatalf("workspace tabs must be non-submit buttons")
	}
	if !strings.Contains(user, `function enhanceButtonTypes(root=document)`) || !strings.Contains(user, `root.querySelectorAll('button:not([type])').forEach`) || !strings.Contains(user, `applyI18n();
  enhanceButtonTypes();`) {
		t.Fatalf("user console must normalize implicit submit buttons")
	}
	if !strings.Contains(user, `id="tx-prev-btn" type="button"`) || !strings.Contains(user, `aria-label="Previous transactions page"`) || !strings.Contains(user, `id="tx-next-btn" type="button"`) || !strings.Contains(user, `aria-label="Next transactions page"`) {
		t.Fatalf("transaction pager icon buttons must expose accessible names")
	}
	if !strings.Contains(user, `id="tx-page-info" aria-live="polite"`) {
		t.Fatalf("transaction pager status must announce page changes")
	}
	for _, required := range []string{
		`<table aria-label="Credit transactions">`,
		`<tbody id="tx-list" aria-live="polite">`,
		`<table id="myskills-table" aria-label="Uploaded skills">`,
		`<tbody id="myskills-list" aria-live="polite">`,
		`<td colspan="5" class="empty" role="status">`,
		`<th scope="row">' + esc(tx.created_at||'') + '</th>`,
		`<th scope="row">${esc(s.name)}</th>`,
		`class="btn btn-outline" type="button" aria-label="${esc(t('action.copy') + ' ' + (s.name || ''))}"`,
		`class="btn btn-danger" type="button" aria-label="${esc(t('sk.withdraw') + ' ' + (s.name || ''))}"`,
	} {
		if !strings.Contains(user, required) {
			t.Fatalf("user tables missing accessibility contract %q", required)
		}
	}
	for _, header := range []string{"tx.time", "tx.type", "tx.amount", "tx.balance", "tx.desc", "sk.name", "sk.version", "sk.status", "sk.rating", "sk.downloads", "sk.actions"} {
		if !strings.Contains(user, `scope="col" data-i18n="`+header+`"`) {
			t.Fatalf("table header %s must expose column scope", header)
		}
	}
	for _, id := range []string{"login-email", "reg-email", "current-password", "credits-amount", "apikey-skill-id", "apikey-bulk"} {
		if !strings.Contains(user, `for="`+id+`"`) {
			t.Fatalf("missing form label for %s", id)
		}
	}

	gossip := read(t, "gossip", "index.html")
	if !strings.Contains(gossip, `class="toolbar" role="search" aria-label="Gossip filters"`) || !strings.Contains(gossip, `id="sortSelect" onchange="applyView()" aria-label="Sort posts"`) {
		t.Fatalf("gossip toolbar must expose search/filter semantics")
	}
	if !strings.Contains(gossip, `id="pageInfo" aria-live="polite"`) {
		t.Fatalf("gossip pager status must announce page changes")
	}
	for _, required := range []string{
		`id="feed" class="feed" role="feed" aria-label="Gossip posts" aria-live="polite" aria-busy="false"`,
		`feed.setAttribute('role','status')`,
		`feed.setAttribute('role','feed')`,
		`<article class="post" aria-label="`,
	} {
		if !strings.Contains(gossip, required) {
			t.Fatalf("gossip feed missing semantic contract %q", required)
		}
	}

	skillhub := read(t, "skillhub", "index.html")
	if !strings.Contains(skillhub, `class="toolbar" role="search" aria-label="Skill search"`) {
		t.Fatalf("skillhub toolbar must expose search semantics")
	}
	if !strings.Contains(skillhub, `id="pageInfo" aria-live="polite"`) {
		t.Fatalf("skillhub pager status must announce page changes")
	}
	for _, required := range []string{
		`id="content" class="state" role="status" aria-live="polite" aria-busy="false"`,
		`content.setAttribute('role', 'status')`,
		`content.setAttribute('role', 'list')`,
		`<article class="card" role="listitem" aria-label="`,
	} {
		if !strings.Contains(skillhub, required) {
			t.Fatalf("skillhub content missing semantic contract %q", required)
		}
	}

	market := read(t, "skillmarket", "index.html")
	for _, id := range []string{"capTabSkills", "capTabMCP", "tabSearch", "tabRating", "tabDownloads", "tabNewest"} {
		if !strings.Contains(market, `id="`+id+`" type="button"`) {
			t.Fatalf("market tab %s must be a non-submit button", id)
		}
	}
	if !strings.Contains(market, `function bindTablistKeyboard`) || !strings.Contains(market, `bindTablistKeyboard('[aria-label="Skill views"]','.tab')`) {
		t.Fatalf("market tablists must keep arrow-key keyboard navigation")
	}
	for _, required := range []string{
		`id="capTabMCP" type="button" onclick="switchCapTab('mcp')" role="tab" aria-selected="false" aria-pressed="false" aria-controls="capMCPView" tabindex="-1"`,
		`id="tabRating" type="button" role="tab" aria-selected="false" aria-controls="view-top-rating" tabindex="-1"`,
		`btn.tabIndex=active?0:-1`,
		`skills.setAttribute('aria-pressed', tab === 'skills' ? 'true' : 'false')`,
		`mcp.setAttribute('aria-pressed', tab === 'mcp' ? 'true' : 'false')`,
		`skills.tabIndex = tab === 'skills' ? 0 : -1`,
	} {
		if !strings.Contains(market, required) {
			t.Fatalf("market tablist missing pressed or roving tabindex contract %q", required)
		}
	}
	for _, required := range []string{
		`id="searchResults" class="grid" aria-busy="false" aria-live="polite"`,
		`id="ratingList" class="grid" aria-busy="false" aria-live="polite"`,
		`id="downloadsList" class="grid" aria-busy="false" aria-live="polite"`,
		`id="newestList" class="grid" aria-busy="false" aria-live="polite"`,
		`id="mcpMarketList" class="grid" aria-busy="false" aria-live="polite"`,
		`class="state" role="status" style="display:none" aria-live="polite"`,
		`id="ratingRow" class="rating-row" role="group" aria-label="Skill rating score"`,
		`btn.setAttribute('aria-label',t('rating')+' '+btn.textContent)`,
		`btn.setAttribute('aria-pressed','false')`,
		`btn.setAttribute('aria-pressed',selected?'true':'false')`,
	} {
		if !strings.Contains(market, required) {
			t.Fatalf("market dynamic regions missing accessibility contract %q", required)
		}
	}
	for _, required := range []string{
		`if(!detailOverlay.classList.contains('active'))return`,
		`if(e.key==='Escape'){closeDetail();return}`,
		`e.key!=='Tab'`,
		`last.focus()`,
	} {
		if !strings.Contains(market, required) {
			t.Fatalf("market detail dialog missing focus-management contract %q", required)
		}
	}

	css := read(t, "pro-ui.css")
	if !strings.Contains(css, `[role="button"]`) {
		t.Fatalf("shared css must size and focus custom button roles")
	}
	if !strings.Contains(css, `.user-mgmt-card-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr));`) {
		t.Fatalf("shared css must keep user management cards in a two-column grid")
	}
	if !strings.Contains(css, `#hubs.list { grid-template-columns: repeat(2, minmax(0, 1fr)) !important; }`) || !strings.Contains(css, `.hub-tenant-controls { display: grid;`) {
		t.Fatalf("shared css must keep registered hubs in two columns with tenant controls")
	}
	if !strings.Contains(css, `.user-mgmt-migration-grid { display: grid; grid-template-columns: minmax(0, 1fr);`) || !strings.Contains(css, `.user-mgmt-form-panel, .user-mgmt-result-panel { min-width: 0; display: contents; }`) {
		t.Fatalf("shared css must keep user migration as a full-width vertical workflow")
	}
	if !strings.Contains(css, `.route-query-kv .item-meta { min-width: 0; overflow-wrap: anywhere; }`) {
		t.Fatalf("shared css must keep long route query values inside cards")
	}
	if !strings.Contains(css, `.user-mgmt-layout, .user-mgmt-migration-grid, .user-mgmt-card-grid, .user-mgmt-stat-grid { grid-template-columns: 1fr; }`) {
		t.Fatalf("shared css must collapse user management grids on mobile")
	}
}
