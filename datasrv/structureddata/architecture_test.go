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
		`document.documentElement.lang = currentLanguage() === "zh" ? "zh-CN" : "en"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("web console bilingual localization missing %q", want)
		}
	}
	for _, forbidden := range []string{"�", "涓", "鎶", "閲", "鐘"} {
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
		`function handleLanguageChange()`,
		`applyI18n(document.body);`,
		`loadGovernanceEvidencePack().catch(err => setStatus(err.message, "err"))`,
		`reviewParams.set("lang", currentLanguage());`,
		`language: $("language").value || "en"`,
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
		`.topbar { display: grid; grid-template-columns: minmax(200px, 1.4fr) minmax(160px, 1fr) 120px 130px 120px 112px 112px; gap: 10px; align-items: end; min-width: 780px; }`,
		`.layout { display: grid; grid-template-columns: 320px minmax(0, 1fr); gap: 18px; align-items: start; }`,
		`.setup-panel { max-width: 1180px; margin: 16px auto 0; display: grid; grid-template-columns: minmax(220px, 1fr) minmax(260px, 1.2fr) minmax(260px, 1.2fr); gap: 14px; align-items: start; }`,
		`.workspace-shell { display: grid; grid-template-columns: 220px minmax(0, 1fr); min-height: calc(100vh - 132px); }`,
		`.summary-bar { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 10px; margin-bottom: 14px; }`,
		`@media (max-width: 960px)`,
		`.topbar, .layout, .workspace-shell, .grid-2, .grid-3 { grid-template-columns: 1fr; min-width: 0; }`,
		`.setup-panel { grid-template-columns: 1fr; margin: 12px; }`,
		`.tabs { display: flex; border-right: 0; overflow-x: auto; }`,
		`.summary-bar { grid-template-columns: 1fr 1fr; }`,
		`data-testid="language-switch"`,
		`data-testid="admin-setup-panel"`,
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
		`document.querySelector('[data-testid="admin-password-policy"]').textContent.includes('Password policy')`,
		`document.querySelector('#serviceStatus').textContent.includes('Service online')`,
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
