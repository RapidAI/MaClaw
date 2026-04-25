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
