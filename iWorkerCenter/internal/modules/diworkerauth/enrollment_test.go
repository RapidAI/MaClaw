package diworkerauth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/db"
)

func newEnrollmentTestHandler(t *testing.T) (*Handler, func()) {
	t.Helper()
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(provider.Write); err != nil {
		_ = provider.Close()
		t.Fatalf("migrate db: %v", err)
	}
	return NewHandler(provider.Write, provider.Read), func() { _ = provider.Close() }
}

func seedEnrollmentAccount(t *testing.T, h *Handler, tenantID, username, password, identifier string) {
	t.Helper()
	salt := generateSalt()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := h.write.Exec(`INSERT INTO diworker_accounts (id, tenant_id, username, password_hash, salt, identifier, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"acct-"+username, tenantID, username, hashPwd(password, salt), salt, identifier, now, now)
	if err != nil {
		t.Fatalf("seed account: %v", err)
	}
}

func TestEnrollmentVerifyAllowsAccountBoundWorker(t *testing.T) {
	h, cleanup := newEnrollmentTestHandler(t)
	defer cleanup()
	seedEnrollmentAccount(t, h, "tenant-a", "alice", "secret", "worker-ops")

	body := bytes.NewBufferString(`{"method":"local","username":"alice","password":"secret","worker_id":"worker-ops"}`)
	req := httptest.NewRequest(http.MethodPost, "/diworker-auth/enrollment/verify", body)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	w := httptest.NewRecorder()
	h.handleEnrollmentVerify(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["verified"] != true || resp["authenticated"] != true || resp["worker_id"] != "worker-ops" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestEnrollmentVerifyRejectsUnauthorizedWorker(t *testing.T) {
	h, cleanup := newEnrollmentTestHandler(t)
	defer cleanup()
	seedEnrollmentAccount(t, h, "tenant-a", "alice", "secret", "worker-ops")

	body := bytes.NewBufferString(`{"method":"local","username":"alice","password":"secret","worker_id":"worker-finance"}`)
	req := httptest.NewRequest(http.MethodPost, "/diworker-auth/enrollment/verify", body)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	w := httptest.NewRecorder()
	h.handleEnrollmentVerify(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["verified"] != false || resp["authenticated"] != true {
		t.Fatalf("response = %+v", resp)
	}
}

func TestImportCSVWithHeaderCreatesLocalEnrollmentAccounts(t *testing.T) {
	h, cleanup := newEnrollmentTestHandler(t)
	defer cleanup()

	body := bytes.NewBufferString("username,password,identifier,expiry_days\ncarol,secret,worker-sales,0\ndave,secret,*,30\n")
	req := httptest.NewRequest(http.MethodPost, "/admin/diworker-auth/import-csv", body)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	w := httptest.NewRecorder()
	h.handleImportCSV(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var imported map[string]any
	if err := json.NewDecoder(w.Body).Decode(&imported); err != nil {
		t.Fatalf("decode import response: %v", err)
	}
	if imported["created"] != float64(2) || imported["skipped"] != float64(0) {
		t.Fatalf("import response = %+v", imported)
	}

	verifyBody := bytes.NewBufferString(`{"method":"local","username":"carol","password":"secret","worker_id":"worker-sales"}`)
	verifyReq := httptest.NewRequest(http.MethodPost, "/diworker-auth/enrollment/verify", verifyBody)
	verifyReq.Header.Set("X-Tenant-ID", "tenant-a")
	verifyW := httptest.NewRecorder()
	h.handleEnrollmentVerify(verifyW, verifyReq)

	if verifyW.Code != http.StatusOK {
		t.Fatalf("verify status = %d body = %s", verifyW.Code, verifyW.Body.String())
	}
	var verified map[string]any
	if err := json.NewDecoder(verifyW.Body).Decode(&verified); err != nil {
		t.Fatalf("decode verify response: %v", err)
	}
	if verified["verified"] != true || verified["authenticated"] != true {
		t.Fatalf("verify response = %+v", verified)
	}
}

func TestImportCSVStopsAtAccountRowLimit(t *testing.T) {
	h, cleanup := newEnrollmentTestHandler(t)
	defer cleanup()

	var body strings.Builder
	body.WriteString("username,password,identifier,expiry_days\n")
	for i := 0; i < maxAccountImportRows+1; i++ {
		body.WriteString("user")
		body.WriteString(strconv.Itoa(i))
		body.WriteString(",secret,*,0\n")
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/diworker-auth/import-csv", strings.NewReader(body.String()))
	req.Header.Set("X-Tenant-ID", "tenant-a")
	w := httptest.NewRecorder()
	h.handleImportCSV(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var imported map[string]any
	if err := json.NewDecoder(w.Body).Decode(&imported); err != nil {
		t.Fatalf("decode import response: %v", err)
	}
	if imported["created"] != float64(maxAccountImportRows) || imported["skipped"] != float64(1) {
		t.Fatalf("import response = %+v", imported)
	}
	var count int
	if err := h.read.QueryRow(`SELECT COUNT(*) FROM diworker_accounts WHERE tenant_id=?`, "tenant-a").Scan(&count); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if count != maxAccountImportRows {
		t.Fatalf("created accounts = %d, want %d", count, maxAccountImportRows)
	}
}

func TestCreateAccountRejectsInvalidJSONWithoutCreatingAccount(t *testing.T) {
	h, cleanup := newEnrollmentTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/admin/diworker-auth/accounts", strings.NewReader(`{"username":`))
	req.Header.Set("X-Tenant-ID", "tenant-a")
	w := httptest.NewRecorder()
	h.handleAccounts(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var count int
	if err := h.read.QueryRow(`SELECT COUNT(*) FROM diworker_accounts WHERE tenant_id=?`, "tenant-a").Scan(&count); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if count != 0 {
		t.Fatalf("created accounts after invalid JSON = %d", count)
	}
}

func TestAuthenticateRejectsOversizedJSONBeforeAuth(t *testing.T) {
	h, cleanup := newEnrollmentTestHandler(t)
	defer cleanup()

	body := `{"method":"local","username":"alice","password":"` + strings.Repeat("x", maxDiWorkerAuthJSONBodyBytes+1024) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/diworker-auth/authenticate", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "tenant-a")
	w := httptest.NewRecorder()
	h.handleAuthenticate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestEnrollmentVerifyRejectsInvalidJSONBeforeAuth(t *testing.T) {
	h, cleanup := newEnrollmentTestHandler(t)
	defer cleanup()
	seedEnrollmentAccount(t, h, "tenant-a", "alice", "secret", "worker-ops")

	req := httptest.NewRequest(http.MethodPost, "/diworker-auth/enrollment/verify", strings.NewReader(`{"method":`))
	req.Header.Set("X-Tenant-ID", "tenant-a")
	w := httptest.NewRecorder()
	h.handleEnrollmentVerify(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestOIDCConfigRejectsInvalidJSONWithoutChangingSecret(t *testing.T) {
	h, cleanup := newEnrollmentTestHandler(t)
	defer cleanup()
	if err := h.saveOIDCConfig(OIDCConfig{Enabled: true, IssuerURL: "https://idp.example.com", ClientID: "client-a", ClientSecret: "keep-me"}); err != nil {
		t.Fatalf("save oidc config: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/diworker-auth/oidc", strings.NewReader(`{"enabled":`))
	w := httptest.NewRecorder()
	h.handleOIDC(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	cfg := h.loadOIDCConfig()
	if cfg.ClientSecret != "keep-me" || cfg.ClientID != "client-a" {
		t.Fatalf("oidc config changed after invalid JSON: %+v", cfg)
	}
}
func TestAuthMethodsExposeProviderRegistry(t *testing.T) {
	h, cleanup := newEnrollmentTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/admin/diworker-auth/methods", nil)
	w := httptest.NewRecorder()
	h.handleMethods(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Methods []AuthMethodStatus `json:"methods"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	seen := map[string]AuthMethodStatus{}
	for _, method := range resp.Methods {
		seen[method.Method] = method
	}
	if !seen["local"].Implemented || !seen["ldap"].Implemented {
		t.Fatalf("expected local and ldap implemented methods, got %+v", seen)
	}
	if seen["oidc"].Implemented || seen["oidc"].Status != "reserved" {
		t.Fatalf("expected oidc reserved method, got %+v", seen["oidc"])
	}
}

func TestEnrollmentVerifyRoutesOIDCAliasToReservedProvider(t *testing.T) {
	h, cleanup := newEnrollmentTestHandler(t)
	defer cleanup()

	body := bytes.NewBufferString(`{"method":"oauth","username":"alice","password":"token","worker_id":"worker-ops"}`)
	req := httptest.NewRequest(http.MethodPost, "/diworker-auth/enrollment/verify", body)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	w := httptest.NewRecorder()
	h.handleEnrollmentVerify(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["verified"] != false || resp["method"] != "oidc" {
		t.Fatalf("response = %+v", resp)
	}
}
func TestAuthenticateRoutesOAuthAliasToOIDCProvider(t *testing.T) {
	h, cleanup := newEnrollmentTestHandler(t)
	defer cleanup()

	if err := h.saveOIDCConfig(OIDCConfig{Enabled: true, IssuerURL: "https://idp.example.com", ClientID: "client-a"}); err != nil {
		t.Fatalf("save oidc config: %v", err)
	}

	body := bytes.NewBufferString(`{"method":"oauth2","username":"alice","password":"token"}`)
	req := httptest.NewRequest(http.MethodPost, "/diworker-auth/authenticate", body)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	w := httptest.NewRecorder()
	h.handleAuthenticate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["authenticated"] != false || resp["method"] != "oidc" {
		t.Fatalf("response = %+v", resp)
	}
	if resp["error"] != "OIDC/OAuth SSO is a reserved adapter; live sign-in is not enabled in this version" {
		t.Fatalf("error = %v", resp["error"])
	}
}

func TestPublicAuthMethodsDiscoveryUsesProviderRegistry(t *testing.T) {
	h, cleanup := newEnrollmentTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/diworker-auth/methods", nil)
	w := httptest.NewRecorder()
	h.handleMethods(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Methods []AuthMethodStatus `json:"methods"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Methods) < 3 {
		t.Fatalf("expected public method discovery to expose registered providers, got %+v", resp.Methods)
	}
}
