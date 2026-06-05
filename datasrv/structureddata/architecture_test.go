package structureddata

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDataSrvStructuredDataKeepsExportedContractsInCorelib(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	allowedImplementationTypes := map[string]bool{
		"HTTPServer":      true,
		"Service":         true,
		"SQLiteStore":     true,
		"Store":           true,
		"DatasetStore":    true,
		"RecordStore":     true,
		"EventStore":      true,
		"ConnectorStore":  true,
		"GovernanceStore": true,
		"AdminStore":      true,
	}
	typeDecl := regexp.MustCompile(`(?m)^type ([A-Z][A-Za-z0-9_]*)`)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || strings.HasSuffix(file, "_alias.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range typeDecl.FindAllStringSubmatch(string(data), -1) {
			name := match[1]
			if !allowedImplementationTypes[name] {
				t.Fatalf("%s declares exported contract type %s; shared DTOs belong in corelib/structureddata and should be aliased from types_alias.go", file, name)
			}
		}
	}
}

func TestDataSrvStructuredDataKeepsNarrowExportedConstructors(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	allowedFunctions := map[string]bool{
		"NewHTTPServer":            true,
		"NewHTTPServerWithAPIKeys": true,
		"NewService":               true,
		"NewSQLiteStore":           true,
		"ParseAPIKeyPolicies":      true,
	}
	functionDecl := regexp.MustCompile(`(?m)^func ([A-Z][A-Za-z0-9_]*)\(`)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range functionDecl.FindAllStringSubmatch(string(data), -1) {
			name := match[1]
			if !allowedFunctions[name] {
				t.Fatalf("%s declares exported function %s; datasrv/structureddata should keep a narrow construction surface", file, name)
			}
		}
	}
}

func TestDataSrvStructuredDataDoesNotExposePackageState(t *testing.T) {
	allowedExportedState := dataSrvAllowedExportedState()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	exportedStateDecl := regexp.MustCompile(`(?m)^(?:var|const)\s+([A-Z][A-Za-z0-9_]*)\b|^\s+([A-Z][A-Za-z0-9_]*)\s*=`)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range exportedStateDecl.FindAllStringSubmatch(string(data), -1) {
			name := firstNonEmptyTestMatch(match[1], match[2])
			if !allowedExportedState[name] {
				t.Fatalf("%s declares exported package state %s; datasrv/structureddata should expose only constructors, sentinel errors, and aliased DTO types", file, name)
			}
		}
	}
}

func TestDataSrvExportedSentinelErrorsHaveHTTPStatusMappings(t *testing.T) {
	data, err := os.ReadFile("http.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for name := range dataSrvAllowedExportedState() {
		if !strings.Contains(text, "errors.Is(err, "+name+")") {
			t.Fatalf("%s must be handled by httpStatusForError", name)
		}
	}
}

func dataSrvAllowedExportedState() map[string]bool {
	return map[string]bool{
		"ErrAdminNotFound":   true,
		"ErrAlreadyExists":   true,
		"ErrBackupNotFound":  true,
		"ErrDatasetNotFound": true,
		"ErrForbidden":       true,
		"ErrInvalidInput":    true,
		"ErrRecordNotFound":  true,
		"ErrSessionNotFound": true,
		"ErrUnauthorized":    true,
	}
}

func TestDataSrvStructuredDataAliasesAllCorelibContracts(t *testing.T) {
	corelibTypes := exportedTypeNames(t, filepath.Join("..", "..", "corelib", "structureddata"), true)
	aliasTypes := exportedTypeNames(t, ".", false)
	for name := range corelibTypes {
		if !aliasTypes[name] {
			t.Fatalf("datasrv/structureddata/types_alias.go missing alias for corelib/structureddata.%s", name)
		}
	}
}

func TestDataSrvStructuredDataAliasFilesOnlyAliasCorelibContracts(t *testing.T) {
	files, err := filepath.Glob("*_alias.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("datasrv/structureddata must keep corelib contract aliases in *_alias.go files")
	}
	aliasDecl := regexp.MustCompile(`^type\s+([A-Z][A-Za-z0-9_]*)(?:\[[^\]]+\])?\s*=\s*contract\.([A-Z][A-Za-z0-9_]*)(?:\[[^\]]+\])?\s*$`)
	exportedTypeDecl := regexp.MustCompile(`^type\s+[A-Z]`)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if !strings.Contains(text, `contract "github.com/RapidAI/CodeClaw/corelib/structureddata"`) {
			t.Fatalf("%s must import corelib/structureddata as contract", file)
		}
		for lineNo, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if !exportedTypeDecl.MatchString(line) {
				continue
			}
			match := aliasDecl.FindStringSubmatch(line)
			if len(match) == 0 {
				t.Fatalf("%s:%d declares an exported type that is not a direct corelib alias: %s", file, lineNo+1, line)
			}
			if match[1] != match[2] {
				t.Fatalf("%s:%d aliases %s to contract.%s; alias names must match corelib contract names", file, lineNo+1, match[1], match[2])
			}
		}
	}
}

func TestDataSrvStructuredDataImportsCorelibContractsOnlyFromAliasFiles(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse imports for %s: %v", file, err)
		}
		for _, importSpec := range parsed.Imports {
			path := strings.Trim(importSpec.Path.Value, `"`)
			if path != "github.com/RapidAI/CodeClaw/corelib/structureddata" {
				continue
			}
			if !strings.HasSuffix(file, "_alias.go") {
				t.Fatalf("%s imports corelib/structureddata directly; implementation files should use local aliases from *_alias.go", file)
			}
			if importSpec.Name == nil || importSpec.Name.Name != "contract" {
				t.Fatalf("%s must import corelib/structureddata with alias name contract", file)
			}
		}
	}
}

func TestDataSrvHTTPRoutesAreDocumentedInOpenAPI(t *testing.T) {
	registeredRoutes := dataSrvHTTPRoutes(t)
	documentedRoutes := dataSrvOpenAPIRoutes(t)
	for route := range registeredRoutes {
		if !documentedRoutes[route] {
			t.Fatalf("HTTP route %s is missing from openAPISpec", route)
		}
	}
	for route := range documentedRoutes {
		if !registeredRoutes[route] {
			t.Fatalf("openAPISpec documents %s but http.go does not register it", route)
		}
	}
}

func TestDataSrvDownloadRoutesDeclareOpenAPIMetadata(t *testing.T) {
	metadata := downloadOpenAPIMetadataByRoute()
	for route := range dataSrvHTTPRoutes(t) {
		if !isDownloadRoute(route) {
			continue
		}
		if _, ok := metadata[route]; !ok {
			t.Fatalf("download route %s must be listed in downloadOpenAPIMetadataByRoute", route)
		}
	}
	for route := range metadata {
		if !isDownloadRoute(route) {
			t.Fatalf("downloadOpenAPIMetadataByRoute contains non-download route %s", route)
		}
	}
}

func TestDataSrvDataAPIRoutesRequireAuth(t *testing.T) {
	data, err := os.ReadFile("http.go")
	if err != nil {
		t.Fatal(err)
	}
	routeDecl := regexp.MustCompile(`s\.mux\.HandleFunc\("([A-Z]+) (/api/v1/data/[^"]+)",\s*(.+)\)`)
	matches := routeDecl.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		t.Fatal("http.go data API auth scan found no routes")
	}
	for _, match := range matches {
		route := match[1] + " " + match[2]
		handlerExpr := match[3]
		if !strings.Contains(handlerExpr, "s.withAuth(") {
			t.Fatalf("data API route %s must be registered through s.withAuth", route)
		}
	}
}

func TestWebConsoleBusinessViewLoadMoreRequiresFullCursor(t *testing.T) {
	data, err := os.ReadFile("webui.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"result.has_more && result.next_before && result.next_before_id",
		"state.businessViewHasMore && state.businessViewNextBefore && state.businessViewNextBeforeID",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("web console business view pagination should require full before/before_id cursor; missing %q", want)
		}
	}
}

func TestWebConsoleHasProfessionalBilingualLocalization(t *testing.T) {
	data, err := os.ReadFile("webui.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`const i18n = { zh:`,
		`const placeholderI18n = { zh:`,
		`MaClawDataSrv MIS 管理控制台`,
		`企业结构化数据运营工作台`,
		`运营健康`,
		`访问控制`,
		`治理证据摘要已刷新`,
		`暂无数据集`,
		`备份已恢复`,
		`请先选择数据集`,
		`#serviceStatus::before`,
		`const prefixed = [`,
		`const direct = i18n[lang] && i18n[lang][text];`,
		`if (direct) return direct;`,
		`"Registered": "已注册"`,
		`"No Hub tenants synced yet.": "尚未同步 Hub 租户。"`,
		`"Tenant registry requires a global data_admin session.": "租户注册表需要全局 data_admin 会话。"`,
		`"Password policy: minimum": "密码策略：最少"`,
		`"Unavailable": "不可用"`,
		`"Open domain": "打开业务域"`,
		`button.textContent = translateText(busyText || originalText);`,
		`function tableHead(labels)`,
		`"Agent onboarding checklist": "Agent 接入清单"`,
		`"No access review findings for the current filter.": "当前筛选条件下无访问复核发现。"`,
		`"Governance controls": "治理控制项"`,
		`"No remediation actions for the current filter.": "当前筛选条件下无整改动作。"`,
		`"No schema proposals": "暂无结构提案"`,
		`"No import jobs": "暂无导入任务"`,
		`"No approvals": "暂无审批"`,
		`"No audit logs": "暂无审计日志"`,
		`"No operation plans": "暂无操作计划"`,
		`"No backups": "暂无备份"`,
		`document.documentElement.lang = currentLanguage() === "zh" ? "zh-CN" : "en"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("web console bilingual localization missing %q", want)
		}
	}
	for _, forbidden := range []string{string(utf8.RuneError), "涓", "鎶", "閲", "鐘"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("web console contains likely mojibake marker %q", forbidden)
		}
	}
}

func TestWebConsoleAdminSetupAndLanguageInteractionsAreWired(t *testing.T) {
	data, err := os.ReadFile("webui.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`$("initializeAdmin").onclick = initializeAdmin`,
		`$("loginAdmin").onclick = loginAdmin`,
		`$("language").onchange = handleLanguageChange`,
		`const result = await publicApiJSON("/api/v1/setup/admin"`,
		`const result = await publicApiJSON("/api/v1/login"`,
		`$("token").value = result.token || ""`,
		`$("initPassword").value = ""`,
		`$("loginPassword").value = ""`,
		`saveSettings();`,
		`data-testid="admin-password-policy"`,
		`function renderAdminPasswordPolicy(policy)`,
		`status && status.password_policy`,
		`Offline reset-password command is available.`,
		`function handleLanguageChange(event)`,
		`const lang = (event && event.target && event.target.value) || currentLanguage();`,
		`syncLanguageControls(lang);`,
		`applyI18n(document.body);`,
		`loadGovernanceEvidencePack().catch(err => setStatus(err.message, "err"))`,
		`reviewParams.set("lang", currentLanguage());`,
		`language: currentLanguage()`,
		`$("language").value = saved.language || "en"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("web console admin/language interaction missing %q", want)
		}
	}
}

func TestWebConsoleResponsiveLayoutAndFirstScreenContracts(t *testing.T) {
	data, err := os.ReadFile("webui.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`body.auth-mode .app-shell { display: none; }`,
		`body.app-mode .auth-screen { display: none; }`,
		`.auth-screen { min-height: 100vh; display: grid; grid-template-columns: minmax(320px, .92fr) minmax(420px, 1.08fr); background: #eef2f6; }`,
		`.auth-card { width: min(100%, 560px); margin: 0 auto; background: var(--panel); border: 1px solid var(--line); border-radius: 8px; box-shadow: var(--shadow-md); overflow: hidden; }`,
		`.topbar { display: flex; align-items: center; justify-content: flex-end; gap: 10px; min-width: 0; }`,
		`.topbar-language { display: inline-flex; align-items: center; gap: 6px; min-height: 30px; padding: 0 8px; border: 1px solid var(--line); border-radius: 999px; background: #fff; color: var(--muted); font-size: 12px; font-weight: 650; }`,
		`.topbar-language select { width: auto; min-width: 82px; min-height: 26px; padding: 2px 22px 2px 6px; border: 0; background: transparent; color: var(--text); font-size: 12px; }`,
		`.layout { display: grid; grid-template-columns: 300px minmax(0, 1fr); gap: 16px; align-items: start; }`,
		`.setup-panel { border: 0; padding: 0; box-shadow: none; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; }`,
		`.setup-copy { grid-column: 1 / -1; padding: 12px 0 2px; border-bottom: 1px solid var(--line-2); }`,
		`.resource-sidebar .action-toolbar { display: grid; grid-template-columns: 1fr; gap: 7px; padding: 8px; background: var(--panel-2); }`,
		`.resource-sidebar .action-toolbar button, .resource-sidebar section > button { width: 100%; min-height: 36px; }`,
		`.workspace-shell { display: grid; grid-template-columns: 204px minmax(0, 1fr); min-height: calc(100vh - 132px); }`,
		`.nav-group { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin: 12px 8px 4px; color: #8795a6; font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; }`,
		`.nav-group::after { content: attr(data-count); min-width: 22px; padding: 1px 6px; border: 1px solid rgba(255,255,255,.12); border-radius: 999px; color: #cbd5e1; background: rgba(255,255,255,.06); text-align: center; letter-spacing: 0; }`,
		`.summary-bar { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 10px; margin-bottom: 14px; }`,
		`.table-wrap:empty { min-height: 56px; display: grid; place-items: center; color: var(--muted); font-size: 12px; background: #fbfcfe; }`,
		`.table-wrap:empty::before { content: ""; }`,
		`.section-title-row { justify-content: space-between; min-height: 36px; }`,
		`.section-title-row h2, .section-title-row h3 { margin: 0; }`,
		`.admin-ops-grid { display: grid; grid-template-columns: minmax(0, 1fr) minmax(320px, .86fr); gap: 12px; align-items: start; }`,
		`.textarea-tool textarea { min-height: 170px; background: #fbfcfe; }`,
		`.action-toolbar.compact-groups { display: grid; grid-template-columns: repeat(4, minmax(180px, 1fr)); gap: 10px; align-items: stretch; }`,
		`.toolbar-cluster { display: grid; grid-template-columns: 1fr; gap: 7px; align-content: start; min-width: 0; padding: 8px; border: 1px solid var(--line-2); border-radius: 8px; background: var(--panel-2); }`,
		`.access-filter-grid { display: grid; grid-template-columns: minmax(0, 1fr) minmax(280px, .86fr); gap: 12px; align-items: start; }`,
		`.filter-panel { display: grid; gap: 10px; padding: 12px; border: 1px solid var(--line-2); border-radius: 8px; background: #fff; }`,
		`.access-results-grid { display: grid; grid-template-columns: minmax(320px, .8fr) minmax(0, 1.2fr); gap: 12px; align-items: start; }`,
		`.access-policy-editor textarea { min-height: 280px; }`,
		`.dataset-workbench { display: grid; grid-template-columns: minmax(320px, 1fr) minmax(280px, .82fr); gap: 12px; align-items: start; }`,
		`.dataset-create-panel { display: grid; gap: 10px; padding: 12px; border: 1px solid var(--line-2); border-radius: 8px; background: #fff; }`,
		`@media (max-width: 1180px)`,
		`.workspace-shell { grid-template-columns: 196px minmax(0, 1fr); }`,
		`@media (max-width: 960px)`,
		`.topbar, .layout, .workspace-shell, .grid-2, .grid-3 { grid-template-columns: 1fr; min-width: 0; }`,
		`.admin-ops-grid { grid-template-columns: 1fr; }`,
		`.action-toolbar.compact-groups { grid-template-columns: repeat(2, minmax(0, 1fr)); }`,
		`.access-filter-grid { grid-template-columns: 1fr; }`,
		`.access-results-grid { grid-template-columns: 1fr; }`,
		`.dataset-workbench { grid-template-columns: 1fr; }`,
		`.auth-screen { grid-template-columns: 1fr; }`,
		`.tabs { display: flex; border-right: 0; border-bottom: 1px solid #101722; overflow-x: auto; }`,
		`.summary-bar { grid-template-columns: 1fr 1fr; }`,
		`@media (max-width: 640px)`,
		`.setup-panel { grid-template-columns: 1fr; }`,
		`@media (pointer: coarse)`,
		`button.small, .tab-panel > .row:not(.section-title-row) button, .action-toolbar button, .resource-sidebar section > button { min-height: 44px; }`,
		`@media (prefers-reduced-motion: reduce)`,
		`data-testid="auth-screen"`,
		`data-testid="app-shell"`,
		`role="status" aria-live="polite"`,
		`data-testid="app-service-status"`,
		`function statusElements()`,
		`parent.id === "serviceStatus" || parent.id === "appServiceStatus"`,
		`role="tablist" aria-label="Administration modules"`,
		`function syncTabAccessibility(name)`,
		`const tabID = "module-tab-" + (btn.dataset.tab || "unknown");`,
		`btn.setAttribute("aria-selected", selected ? "true" : "false");`,
		`panel.setAttribute("aria-labelledby", "module-tab-" + panel.id);`,
		`function handleTabKeydown(event)`,
		`$("tabs").onkeydown = handleTabKeydown;`,
		`data-testid="language-switch"`,
		`data-testid="app-language-switch"`,
		`function syncLanguageControls(lang)`,
		`data-testid="admin-setup-panel"`,
		`data-testid="sign-out"`,
		`function showAppShell()`,
		`function signOut()`,
		`data-testid="setup-checklist"`,
		`data-testid="overview-health"`,
		`data-testid="governance-evidence-summary"`,
		`data-testid="download-evidence-summary"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("web console responsive layout/first-screen contract missing %q", want)
		}
	}
}

func TestWebConsoleBrowserVisualRegressionUsesRealServerAndScripts(t *testing.T) {
	data, err := os.ReadFile("webui_browser_visual_test.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`httptest.NewServer(NewHTTPServer(NewService(store, "sqlite"), "", "visual-test").Handler())`,
		`client.call("Runtime.enable", nil)`,
		`typeof document.querySelector('[data-testid="app-language-switch"]').oninput === 'function'`,
		`document.documentElement.lang === 'zh-CN'`,
		`!summaryLabel.includes('Engine')`,
		`!['Registered', 'Configured', 'Not configured'].includes(hubState)`,
		`document.querySelector('[data-testid="admin-password-policy"]').textContent.trim().length > 10`,
		`document.querySelector('#serviceStatus').classList.contains('ok')`,
		`Page.captureScreenshot`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("browser visual regression must exercise real server, scripts, and screenshot capture; missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"visual_test",
		"visualTestMode",
		"stripWebConsoleScripts",
		"file:///",
		"about:blank\" + filepath",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("browser visual regression contains workaround marker %q", forbidden)
		}
	}
	webui, err := os.ReadFile("webui.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(webui), "visualTestMode") || strings.Contains(string(webui), "visual_test") {
		t.Fatal("web console product code must not contain visual test bypasses")
	}
}

func TestHTTPDownloadsUseSharedSecurityHeaders(t *testing.T) {
	data, err := os.ReadFile("http.go")
	if err != nil {
		t.Fatal(err)
	}
	contentDisposition := regexp.MustCompile(`Header\(\)\.Set\("Content-Disposition"`)
	matches := contentDisposition.FindAllStringIndex(string(data), -1)
	if len(matches) != 1 {
		t.Fatalf("download responses should set Content-Disposition only through writeDownloadHeaders; found %d direct setters", len(matches))
	}
	if !strings.Contains(string(data), `func writeDownloadHeaders`) || !strings.Contains(string(data), `"X-Content-Type-Options", "nosniff"`) {
		t.Fatal("writeDownloadHeaders must set shared download security headers")
	}
}

func exportedTypeNames(t *testing.T, dir string, includeAllFiles bool) map[string]bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	typeDecl := regexp.MustCompile(`(?m)^type ([A-Z][A-Za-z0-9_]*)`)
	for _, file := range files {
		name := filepath.Base(file)
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		if !includeAllFiles && !strings.HasSuffix(name, "_alias.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range typeDecl.FindAllStringSubmatch(string(data), -1) {
			out[match[1]] = true
		}
	}
	return out
}

func firstNonEmptyTestMatch(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "<unknown>"
}

func dataSrvHTTPRoutes(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile("http.go")
	if err != nil {
		t.Fatal(err)
	}
	routeDecl := regexp.MustCompile(`HandleFunc\("([A-Z]+) ([^"]+)"`)
	publicNonAPIRoutes := map[string]bool{
		"GET /":                    true,
		"GET /ui":                  true,
		"GET /api/v1/openapi.json": true,
	}
	out := map[string]bool{}
	for _, match := range routeDecl.FindAllStringSubmatch(string(data), -1) {
		route := strings.ToLower(match[1]) + " " + match[2]
		if publicNonAPIRoutes[match[1]+" "+match[2]] {
			continue
		}
		out[route] = true
	}
	if len(out) == 0 {
		t.Fatal("http.go route scan found no API routes")
	}
	return out
}

func isDownloadRoute(route string) bool {
	_, path, ok := strings.Cut(route, " ")
	if !ok {
		return false
	}
	return strings.Contains(path, "/download") ||
		strings.HasSuffix(path, "/export.csv") ||
		strings.HasSuffix(path, "/export.jsonl") ||
		strings.HasSuffix(path, "/import-template.csv") ||
		strings.HasSuffix(path, "/evidence-summary.txt")
}

func dataSrvOpenAPIRoutes(t *testing.T) map[string]bool {
	t.Helper()
	spec := openAPISpec("test")
	rawPaths, ok := spec["paths"].(map[string]interface{})
	if !ok {
		t.Fatalf("openAPISpec paths has invalid shape: %#v", spec["paths"])
	}
	out := map[string]bool{}
	for path, rawPathItem := range rawPaths {
		pathItem, ok := rawPathItem.(map[string]interface{})
		if !ok {
			t.Fatalf("openAPISpec path %s has invalid shape: %#v", path, rawPathItem)
		}
		for method := range pathItem {
			switch method {
			case "get", "post", "put", "patch", "delete":
				out[fmt.Sprintf("%s %s", method, path)] = true
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("openAPISpec route scan found no documented routes")
	}
	return out
}
