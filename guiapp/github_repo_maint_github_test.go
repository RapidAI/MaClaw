package guiapp

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

func TestGitHubRepoMaintClientAndGhostDir(t *testing.T) {
	if oauth.GitHubAccountScopes != "read:user repo" {
		t.Fatalf("oauth scope = %q", oauth.GitHubAccountScopes)
	}
	t.Logf("scope=%s", oauth.GitHubAccountScopes)

	const secret = "hubSecret_base64url-Value"
	var hits, evilHits int
	var mu sync.Mutex
	var lastBody []byte
	var lastMethod, lastPath string

	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		evilHits++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer evil.Close()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		hits++
		lastBody = append([]byte(nil), body...)
		lastMethod = r.Method
		lastPath = r.URL.Path
		mu.Unlock()
		switch r.URL.Query().Get("mode") {
		case "redir":
			http.Redirect(w, r, evil.URL+"/steal", http.StatusFound)
		case "403":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"token is invalid or revoked"}`))
		case "404":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		case "401plain":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
		case "401revoked":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"token is invalid or revoked"}`))
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":42}`))
		}
	}))
	defer srv.Close()

	var apiDials, otherDials int
	prevClient := newGitHubHTTPClient
	newGitHubHTTPClient = func() *http.Client {
		client := defaultGitHubHTTPClient()
		client.Transport = &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if addr == "api.github.com:443" {
					apiDials++
					return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
				}
				otherDials++
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		return client
	}
	t.Cleanup(func() { newGitHubHTTPClient = prevClient })

	app := &App{credentialStore: oauth.NewFileCredentialStore(filepath.Join(t.TempDir(), "credentials.json"))}
	saveToken := func() {
		t.Helper()
		if err := app.credentialStore.Modify(oauth.GitHubAccountProviderID, func(old *oauth.StoredCredential) (*oauth.StoredCredential, error) {
			return &oauth.StoredCredential{Type: "oauth", AccessToken: "ghp_test_token", Scopes: oauth.GitHubAccountScopes}, nil
		}); err != nil {
			t.Fatal(err)
		}
		app.setGitHubTokenCache("ghp_test_token")
	}
	tokenPresent := func() bool {
		t.Helper()
		return app.loadGitHubAccessToken() != ""
	}
	saveToken()

	if _, _, err := app.doGitHubJSON(http.MethodGet, srv.URL+"/repos/yucejade/jjquant-v2", nil); err == nil {
		t.Fatal("non-api.github.com URL was accepted")
	}
	if _, _, err := app.doGitHubJSON(http.MethodGet, "https://api.github.com.evil.com/repos/yucejade/jjquant-v2", nil); err == nil {
		t.Fatal("api.github.com.evil.com was accepted")
	}
	if _, _, err := app.doGitHubJSON(http.MethodGet, "http://api.github.com/repos/yucejade/jjquant-v2", nil); err == nil {
		t.Fatal("http api.github.com was accepted")
	}
	if hits != 0 || apiDials != 0 || otherDials != 0 {
		t.Fatalf("rejected URL opened a connection hits=%d apiDials=%d otherDials=%d", hits, apiDials, otherDials)
	}
	t.Logf("non-api host refused before dial hits=%d", hits)

	if _, _, err := app.doGitHubJSON(http.MethodGet, "https://api.github.com/user?mode=redir", nil); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("redirect err=%v", err)
	}
	if evilHits != 0 || otherDials != 0 || apiDials != 1 {
		t.Fatalf("redirect followed evilHits=%d otherDials=%d apiDials=%d", evilHits, otherDials, apiDials)
	}
	t.Logf("redirect not followed evilHits=%d", evilHits)

	spec := gitHubHookSpec{
		URL:    "https://hub.example/api/github/repo-maint/hooks/token",
		Secret: secret,
		Events: []string{"push", "issues"},
		Active: true,
	}
	id, err := app.createGitHubRepoHook("yucejade", "jjquant-v2", spec)
	if err != nil || id != 42 {
		t.Fatalf("create id=%d err=%v", id, err)
	}
	if err := app.updateGitHubRepoHook("yucejade", "jjquant-v2", 42, spec); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	method, path, body := lastMethod, lastPath, append([]byte(nil), lastBody...)
	mu.Unlock()
	if method != http.MethodPatch || path != "/repos/yucejade/jjquant-v2/hooks/42" {
		t.Fatalf("patch %s %s", method, path)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	cfg, _ := parsed["config"].(map[string]any)
	if cfg["secret"] != secret {
		t.Fatalf("patch secret=%v body=%s", cfg["secret"], body)
	}
	if cfg["insecure_ssl"] != "0" || cfg["content_type"] != "json" {
		t.Fatalf("config=%v", cfg)
	}
	t.Logf("patch resent the same secret string")

	if _, _, err := app.doGitHubJSON(http.MethodGet, "https://api.github.com/user?mode=403", nil); err == nil {
		t.Fatal("403")
	}
	if !tokenPresent() {
		t.Fatal("403 cleared the github credential")
	}
	if _, _, err := app.doGitHubJSON(http.MethodGet, "https://api.github.com/user?mode=404", nil); err == nil {
		t.Fatal("404")
	}
	if !tokenPresent() {
		t.Fatal("404 cleared the github credential")
	}
	if _, _, err := app.doGitHubJSON(http.MethodGet, "https://api.github.com/user?mode=401plain", nil); err == nil {
		t.Fatal("401")
	}
	if !tokenPresent() {
		t.Fatal("401 without invalid or revoked cleared the credential")
	}
	if _, _, err := app.doGitHubJSON(http.MethodGet, "https://api.github.com/user?mode=401revoked", nil); err == nil || !strings.Contains(err.Error(), "invalid or revoked") {
		t.Fatalf("revoked err=%v", err)
	}
	if tokenPresent() {
		t.Fatal("401 invalid or revoked left the credential in place")
	}
	t.Logf("credential cleared only for 401 invalid or revoked")

	if same, other := sameGitHubRepo("git@github.com:YuceJade/JJQuant-V2.git", "yucejade", "jjquant-v2"), sameGitHubRepo("https://github.com/other/JJQuant-V2.git", "yucejade", "jjquant-v2"); !same || other {
		t.Fatalf("owner/repo compare same=%v other=%v", same, other)
	}

	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	class, normalized, err := classifyGhostDir(missing, "yucejade", "jjquant-v2")
	if err != nil || class != ghostDirMissing {
		t.Fatalf("missing class=%s err=%v", class, err)
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Fatal("missing path was created")
	}
	withSlash, err := normalizeGhostDir(normalized + string(os.PathSeparator))
	if err != nil || withSlash != normalized {
		t.Fatalf("slash normalize %q vs %q err=%v", withSlash, normalized, err)
	}

	empty := filepath.Join(root, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if class, _, err = classifyGhostDir(empty, "yucejade", "jjquant-v2"); err != nil || class != ghostDirEmpty {
		t.Fatalf("empty class=%s err=%v", class, err)
	}
	left, _ := os.ReadDir(empty)
	if len(left) != 0 {
		t.Fatal("empty directory was written")
	}

	filePath := filepath.Join(root, "not-a-dir.txt")
	if err := os.WriteFile(filePath, []byte("keep-file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if class, _, err = classifyGhostDir(filePath, "yucejade", "jjquant-v2"); err != nil || class != ghostDirForeign {
		t.Fatalf("file class=%s err=%v", class, err)
	}
	if raw, _ := os.ReadFile(filePath); string(raw) != "keep-file" {
		t.Fatal("file was overwritten")
	}

	occupied := filepath.Join(root, "occupied")
	if err := os.Mkdir(occupied, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(occupied, "keep.txt"), []byte("keep-tree"), 0o644); err != nil {
		t.Fatal(err)
	}
	if class, _, err = classifyGhostDir(occupied, "yucejade", "jjquant-v2"); err != nil || class != ghostDirForeign {
		t.Fatalf("non-git class=%s err=%v", class, err)
	}
	if raw, _ := os.ReadFile(filepath.Join(occupied, "keep.txt")); string(raw) != "keep-tree" {
		t.Fatal("non-git tree was overwritten")
	}

	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is required to classify a checkout")
	}
	checkout := filepath.Join(root, "checkout")
	if err := os.Mkdir(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := runGit(checkout, "init"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(checkout, "remote", "add", "origin", "https://github.com/YuceJade/JJQuant-V2.git"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "keep.txt"), []byte("keep-repo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if class, _, err = classifyGhostDir(checkout, "yucejade", "jjquant-v2"); err != nil || class != ghostDirReady {
		t.Fatalf("ready class=%s err=%v", class, err)
	}
	if class, _, err = classifyGhostDir(checkout, "someoneelse", "jjquant-v2"); err != nil || class != ghostDirForeign {
		t.Fatalf("other owner class=%s err=%v", class, err)
	}
	if raw, _ := os.ReadFile(filepath.Join(checkout, "keep.txt")); string(raw) != "keep-repo" {
		t.Fatal("matching checkout was overwritten")
	}
	t.Logf("ghost dir missing/empty/ready/foreign and owner+repo compare")
}
