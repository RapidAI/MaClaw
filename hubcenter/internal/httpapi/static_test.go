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
	if cacheControl := rec.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "max-age=300") {
		t.Fatalf("cache-control = %q, want max-age=300", cacheControl)
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

	admin := read(t, "admin", "index.html")
	if !strings.Contains(admin, `id="toastStack" class="toast-stack" aria-live="polite" aria-atomic="true"`) {
		t.Fatalf("admin toast stack must announce status changes")
	}
	if !strings.Contains(admin, `class="lang-switch" aria-label="Language"`) || !strings.Contains(admin, `btn.setAttribute('aria-pressed'`) {
		t.Fatalf("admin language switcher must expose pressed state")
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
	for _, id := range []string{"login-email", "reg-email", "current-password", "credits-amount", "apikey-skill-id", "apikey-bulk"} {
		if !strings.Contains(user, `for="`+id+`"`) {
			t.Fatalf("missing form label for %s", id)
		}
	}

	css := read(t, "pro-ui.css")
	if !strings.Contains(css, `[role="button"]`) {
		t.Fatalf("shared css must size and focus custom button roles")
	}
}
