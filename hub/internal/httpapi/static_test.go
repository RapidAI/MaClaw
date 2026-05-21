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
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<title>MaClaw Hub Admin</title>"), 0644); err != nil {
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
	if !strings.Contains(body, "console.log('admin');") {
		t.Fatalf("index body missing injected admin js: %q", body)
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
	if !strings.Contains(body, "console.log('admin');") {
		t.Fatalf("spa fallback body missing injected admin js: %q", body)
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
		`id="tab-failurelogs"`,
		`/admin/failure-logs-tab.js`,
		`loadFailureLogs()`,
		`id="centerCorporateEmailDomains"`,
		`id="centerAcceptPublicSignup"`,
		`id="centerCorporateEmailDomainsHero"`,
		`id="centerAcceptPublicSignupHero"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("admin index missing %s", want)
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
		if !strings.Contains(string(body), `href="`+href+`"`) {
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
		`<button type="button" id="btnValidate">`,
		`<button type="button" id="btnSubmit" class="btn-primary">`,
		`role="button" tabindex="0" data-node-type="trigger"`,
		`role="button" tabindex="0" data-node-type="terminal"`,
		`aria-label="Close node configuration"`,
	} {
		if !strings.Contains(approval, want) {
			t.Fatalf("approval workflow page missing accessibility contract %q", want)
		}
	}

	editor := read(t, "approval_workflow", "workflow-editor.js")
	for _, want := range []string{
		`function addNodeToCanvas(nodeType, position)`,
		`el.addEventListener('keydown', function (e)`,
		`if (e.key !== 'Enter' && e.key !== ' ') return;`,
		`addNodeToCanvas(el.getAttribute('data-node-type')`,
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
