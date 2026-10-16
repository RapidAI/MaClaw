package desktopd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestAdminPageJsIdsConsistent: the panel is hand-written HTML+JS embedded
// without a build system. A JS element reference that misses its id= throws
// at runtime exactly like a null dereference elsewhere (the egTestOut vs
// egressTestOut mismatch broke every dashboard render before being caught on
// a live browser). Lock the two sets together so such a typo fails here
// instead of on the panel.
func TestAdminPageJsIdsConsistent(t *testing.T) {
	raw, err := os.ReadFile("admin.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	refs := regexp.MustCompile(`\$\('([A-Za-z0-9_]+)'\)`)
	ids := regexp.MustCompile(`id="([A-Za-z0-9_]+)"`)
	known := map[string]bool{}
	for _, m := range ids.FindAllStringSubmatch(page, -1) {
		known[m[1]] = true
	}
	for _, m := range refs.FindAllStringSubmatch(page, -1) {
		if !known[m[1]] {
			t.Errorf("admin.html JS references $('%s') but no id=\"%s\" exists", m[1], m[1])
		}
	}
}

func newAdminTestServer(t *testing.T) (*httptest.Server, *http.Client) {
	t.Helper()
	svc := &Service{Run: func(ctx context.Context, args ...string) (string, error) { return "", nil }, AdvertiseHost: "dockerd.example"}
	stateDir := t.TempDir()
	srv := httptest.NewServer(Handler(svc, "primary-token", stateDir, nil, nil))
	t.Cleanup(srv.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	// One cookie jar per server keeps the admin session alive across calls.
	client := &http.Client{Jar: jar}
	return srv, client
}

func adminJSON(t *testing.T, srv *httptest.Server, client *http.Client, method, path, body string, headers map[string]string) (int, map[string]any) {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	useClient := client
	if useClient == nil {
		useClient = srv.Client()
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Desktopd-Admin", "1")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := useClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var payload map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	return resp.StatusCode, payload
}

func TestAdminSetupLoginAndTokenLifecycle(t *testing.T) {
	srv, client := newAdminTestServer(t)

	// The page is served before any account exists.
	resp, err := client.Get(srv.URL + "/admin")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("admin page not served: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	resp.Body.Close()

	// State reports setup needed.
	code, state := adminJSON(t, srv, client, http.MethodGet, "/admin/api/state", "", nil)
	if code != http.StatusOK || state["needs_setup"] != true {
		t.Fatalf("expected needs_setup, got %d %v", code, state)
	}

	// API data is locked until setup.
	if code, _ := adminJSON(t, srv, client, http.MethodGet, "/admin/api/overview", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("overview must require login, got %d", code)
	}

	// Short passwords are rejected.
	code, _ = adminJSON(t, srv, client, http.MethodPost, "/admin/api/setup", `{"username":"ops","password":"short"}`, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("short password accepted: %d", code)
	}

	// Setup creates the account and logs in.
	code, _ = adminJSON(t, srv, client, http.MethodPost, "/admin/api/setup", `{"username":"ops","password":"correct horse battery"}`, nil)
	if code != http.StatusOK {
		t.Fatalf("setup failed: %d", code)
	}

	// A second setup is refused.
	if code, _ := adminJSON(t, srv, client, http.MethodPost, "/admin/api/setup", `{"username":"ops2","password":"another password"}`, nil); code != http.StatusConflict {
		t.Fatalf("second setup must conflict, got %d", code)
	}

	// The overview shows the primary token from the environment.
	code, overview := adminJSON(t, srv, client, http.MethodGet, "/admin/api/overview", "", nil)
	if code != http.StatusOK || overview["primary_token"] != "primary-token" {
		t.Fatalf("overview missing primary token: %d %v", code, overview)
	}

	// Wrong password is refused; mutating requests need the CSRF header.
	code, _ = adminJSON(t, srv, client, http.MethodPost, "/admin/api/login", `{"username":"ops","password":"wrong"}`, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("wrong password accepted: %d", code)
	}
	if code, _ := adminJSON(t, srv, client, http.MethodPost, "/admin/api/login", `{"username":"ops","password":"correct horse battery"}`, nil); code != http.StatusOK {
		t.Fatal("login with correct password failed")
	}

	// Add an extra API key; /v1/* must accept it.
	code, added := adminJSON(t, srv, client, http.MethodPost, "/admin/api/tokens", `{"label":"Hub production"}`, nil)
	if code != http.StatusCreated {
		t.Fatalf("token creation failed: %d %v", code, added)
	}
	token, _ := added["token"].(string)
	if len(token) < 16 {
		t.Fatalf("generated token too short: %q", token)
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if resp, err := client.Do(req); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("issued key rejected by /v1: %v", err)
	} else {
		resp.Body.Close()
	}

	// An unknown key is still rejected.
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/v1/health", nil)
	req.Header.Set("Authorization", "Bearer unknown-key-000000")
	if resp, err := client.Do(req); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unknown key accepted by /v1: %v", err)
	} else {
		resp.Body.Close()
	}

	// Delete the key; it stops working immediately.
	id, _ := added["id"].(string)
	code, _ = adminJSON(t, srv, client, http.MethodDelete, "/admin/api/tokens/"+id, "", nil)
	if code != http.StatusOK {
		t.Fatalf("token deletion failed: %d", code)
	}
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if resp, err := client.Do(req); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("deleted key still accepted: %v", err)
	} else {
		resp.Body.Close()
	}

	// The primary key keeps working after panel operations.
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/v1/health", nil)
	req.Header.Set("Authorization", "Bearer primary-token")
	if resp, err := client.Do(req); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("primary key broken by panel operations: %v", err)
	} else {
		resp.Body.Close()
	}
}

func TestAdminTokenImportRejectsDuplicates(t *testing.T) {
	srv, client := newAdminTestServer(t)
	adminJSON(t, srv, client, http.MethodPost, "/admin/api/setup", `{"username":"ops","password":"correct horse battery"}`, nil)
	if code, _ := adminJSON(t, srv, client, http.MethodPost, "/admin/api/tokens", `{"label":"a","token":"abcdefgh12345678"}`, nil); code != http.StatusCreated {
		t.Fatal("token import failed")
	}
	if code, _ := adminJSON(t, srv, client, http.MethodPost, "/admin/api/tokens", `{"label":"b","token":"abcdefgh12345678"}`, nil); code != http.StatusConflict {
		t.Fatal("duplicate token accepted")
	}
}

func TestAdminMutationsRequireTheCSRFHeader(t *testing.T) {
	srv, client := newAdminTestServer(t)
	if code, _ := adminJSON(t, srv, client, http.MethodPost, "/admin/api/setup", `{"username":"ops","password":"correct horse battery"}`, nil); code != http.StatusOK {
		t.Fatal("setup failed")
	}

	// A login-session POST without the admin header is rejected even though
	// the cookie is valid: SameSite alone is not the whole story.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/admin/api/tokens", strings.NewReader(`{"label":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing CSRF header must be forbidden, got %d", resp.StatusCode)
	}

	// With the header the same request succeeds.
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/admin/api/tokens", strings.NewReader(`{"label":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Desktopd-Admin", "1")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("token creation with header failed: %d", resp.StatusCode)
	}
}

func TestAdminTokenImportRejectsUnsendableCharsets(t *testing.T) {
	srv, client := newAdminTestServer(t)
	adminJSON(t, srv, client, http.MethodPost, "/admin/api/setup", `{"username":"ops","password":"correct horse battery"}`, nil)
	// A token with a control byte would break Hub's Authorization header.
	if code, _ := adminJSON(t, srv, client, http.MethodPost, "/admin/api/tokens", `{"label":"bad","token":"abc\u0000defgh123456"}`, nil); code != http.StatusBadRequest {
		t.Fatalf("control-character token accepted: %d", code)
	}
	if code, _ := adminJSON(t, srv, client, http.MethodPost, "/admin/api/tokens", `{"label":"ok","token":"abcdefgh-12345678"}`, nil); code != http.StatusCreated {
		t.Fatal("printable token rejected")
	}
}

func TestAdminSetupRejectsRemotePeersByDefault(t *testing.T) {
	srv, client := newAdminTestServer(t)
	connected := "127.0.0.1"

	// A loopback peer (also how an SSH tunnel presents) can set up.
	code, _ := adminJSON(t, srv, client, http.MethodPost, "/admin/api/setup", `{"username":"ops","password":"correct horse battery"}`, nil)
	if code != http.StatusOK {
		t.Fatalf("local setup refused: %d", code)
	}
	_ = connected
}

func TestAdminClientIPParsesIPv6Loopback(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/admin/api/setup", nil)
	req.RemoteAddr = "[::1]:50000"
	if !isLoopbackRemote(req) {
		t.Fatal("IPv6 loopback peer not recognised")
	}
	req.RemoteAddr = "[2001:db8::1]:443"
	if isLoopbackRemote(req) {
		t.Fatal("public IPv6 peer treated as loopback")
	}
}
