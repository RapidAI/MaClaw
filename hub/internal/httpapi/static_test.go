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
