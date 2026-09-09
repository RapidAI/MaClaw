package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/database"
	"github.com/RapidAI/CodeClaw/corelib/excel"
)

type fakeDatabaseRefreshCall struct {
	tenantID string
	userID   string
	profiles []database.Profile
}

type fakeDatabaseProfileRefresher struct {
	mu    sync.Mutex
	calls []fakeDatabaseRefreshCall
}

func (f *fakeDatabaseProfileRefresher) RefreshDatabaseProfiles(tenantID, userID string, profiles []database.Profile) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeDatabaseRefreshCall{tenantID: tenantID, userID: userID, profiles: append([]database.Profile(nil), profiles...)})
}

func (f *fakeDatabaseProfileRefresher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeDatabaseProfileRefresher) last() fakeDatabaseRefreshCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

func newAdminDatabaseTestServer(t *testing.T) (*agentservice.Service, *HTTPServer, agentservice.Principal) {
	t.Helper()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	t.Cleanup(server.Close)
	return svc, server, agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
}

func doAdminDatabaseRequest(server *HTTPServer, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	return w
}

func adminDatabaseScopeQuery(p agentservice.Principal) string {
	return "?tenant_id=" + p.TenantID + "&user_id=" + p.UserID
}

const adminDatabaseTestMySQLProfile = `{"id":"crm","name":"CRM","type":"mysql","host":"db.internal","port":3306,"database":"crm","username":"reader","secret_ref":"keyring:crm","read_only":true}`

func TestAdminDatabaseProfilesUnauthorized(t *testing.T) {
	_, server, p := newAdminDatabaseTestServer(t)
	target := "/api/v1/admin/database/profiles" + adminDatabaseScopeQuery(p)
	w := doAdminDatabaseRequest(server, http.MethodGet, target, "", nil)
	assertAdminSecurityHeaders(t, w.Result())
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	w = doAdminDatabaseRequest(server, http.MethodPost, target, adminDatabaseTestMySQLProfile, nil)
	assertAdminSecurityHeaders(t, w.Result())
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminDatabaseProfilesRequireOwner(t *testing.T) {
	svc, server, p := newAdminDatabaseTestServer(t)
	now := time.Now().UTC()
	viewer := adminUserRecord{ID: "admin_user_viewer", Username: "viewer", Role: "viewer", Status: "active", PasswordHash: "unused", CreatedAt: now, UpdatedAt: now}
	if err := saveAdminUsers(svc.DataRoot(), []adminUserRecord{viewer}); err != nil {
		t.Fatalf("saveAdminUsers: %v", err)
	}
	token, session, err := newAdminSession(viewer, "", "", now)
	if err != nil {
		t.Fatalf("newAdminSession: %v", err)
	}
	if err := saveAdminSessions(svc.DataRoot(), []adminSessionRecord{session}); err != nil {
		t.Fatalf("saveAdminSessions: %v", err)
	}
	headers := map[string]string{"X-MaClaw-Admin-Secret": token}

	// A non-owner admin session may neither list nor test profiles: both leak
	// host/database names, and test additionally dials the target (SSRF).
	target := "/api/v1/admin/database/profiles" + adminDatabaseScopeQuery(p)
	w := doAdminDatabaseRequest(server, http.MethodGet, target, "", headers)
	assertAdminSecurityHeaders(t, w.Result())
	if w.Code != http.StatusForbidden {
		t.Fatalf("list status = %d body = %s", w.Code, w.Body.String())
	}
	// Writes and probes are owner-only as well.
	for _, item := range []struct {
		method string
		target string
		body   string
	}{
		{http.MethodPost, target, adminDatabaseTestMySQLProfile},
		{http.MethodPost, "/api/v1/admin/database/profiles/crm/test" + adminDatabaseScopeQuery(p), ""},
		{http.MethodPost, "/api/v1/admin/database/profiles/crm/rotate-secret" + adminDatabaseScopeQuery(p), `{"secret_ref":"keyring:crm-v2"}`},
		{http.MethodDelete, "/api/v1/admin/database/profiles/crm" + adminDatabaseScopeQuery(p) + "&confirm=true", ""},
	} {
		w = doAdminDatabaseRequest(server, item.method, item.target, item.body, headers)
		assertAdminSecurityHeaders(t, w.Result())
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s %s status = %d body = %s", item.method, item.target, w.Code, w.Body.String())
		}
	}
}

func TestAdminDatabaseProfilesCRUDFlow(t *testing.T) {
	svc, server, p := newAdminDatabaseTestServer(t)
	refresher := &fakeDatabaseProfileRefresher{}
	server.SetDatabaseProfileRuntime(refresher, nil)
	headers := map[string]string{"X-MaClaw-Admin-Secret": "admin-secret"}
	base := "/api/v1/admin/database/profiles" + adminDatabaseScopeQuery(p)

	// Create with an Idempotency-Key.
	createHeaders := map[string]string{"X-MaClaw-Admin-Secret": "admin-secret", "Idempotency-Key": "create-crm-1"}
	w := doAdminDatabaseRequest(server, http.MethodPost, base, adminDatabaseTestMySQLProfile, createHeaders)
	assertAdminSecurityHeaders(t, w.Result())
	if w.Code != http.StatusOK {
		t.Fatalf("create status = %d body = %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "keyring:crm") || strings.Contains(w.Body.String(), svc.DataRoot()) {
		t.Fatalf("response leaks secret_ref or dataRoot: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"has_secret_ref":true`) {
		t.Fatalf("expected has_secret_ref in response: %s", w.Body.String())
	}
	if refresher.callCount() != 1 {
		t.Fatalf("refresh calls = %d", refresher.callCount())
	}
	if got := refresher.last(); got.tenantID != p.TenantID || got.userID != p.UserID || len(got.profiles) != 1 || got.profiles[0].ID != "crm" {
		t.Fatalf("unexpected refresh call: %#v", got)
	}

	// Replaying the same key with the same body returns the first result and
	// must not create a duplicate profile.
	w = doAdminDatabaseRequest(server, http.MethodPost, base, adminDatabaseTestMySQLProfile, createHeaders)
	if w.Code != http.StatusOK || w.Header().Get("Idempotent-Replay") != "true" {
		t.Fatalf("replay status = %d replay = %q body = %s", w.Code, w.Header().Get("Idempotent-Replay"), w.Body.String())
	}
	// Same key with a different body conflicts.
	conflictBody := strings.Replace(adminDatabaseTestMySQLProfile, `"CRM"`, `"CRM v2"`, 1)
	w = doAdminDatabaseRequest(server, http.MethodPost, base, conflictBody, createHeaders)
	if w.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d body = %s", w.Code, w.Body.String())
	}

	// Ambiguous idempotency keys are rejected.
	req := httptest.NewRequest(http.MethodPost, base, strings.NewReader(adminDatabaseTestMySQLProfile))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Add("Idempotency-Key", "a")
	req.Header.Add("Idempotency-Key", "b")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("multi idempotency key status = %d body = %s", w.Code, w.Body.String())
	}
	w = doAdminDatabaseRequest(server, http.MethodPost, base, adminDatabaseTestMySQLProfile, map[string]string{"X-MaClaw-Admin-Secret": "admin-secret", "Idempotency-Key": "   "})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("blank idempotency key status = %d body = %s", w.Code, w.Body.String())
	}

	// List shows one non-sensitive summary.
	w = doAdminDatabaseRequest(server, http.MethodGet, base, "", headers)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Count(body, `"id":"crm"`) != 1 || !strings.Contains(body, `"host":"db.internal"`) || !strings.Contains(body, `"status":"configured"`) {
		t.Fatalf("unexpected list body: %s", body)
	}
	if strings.Contains(body, "keyring:crm") || strings.Contains(body, `"dsn"`) || strings.Contains(body, `"secret_ref":`) {
		t.Fatalf("list leaks sensitive fields: %s", body)
	}

	// Update in place (same id) without an idempotency key.
	updated := strings.Replace(adminDatabaseTestMySQLProfile, `"CRM"`, `"CRM v2"`, 1)
	w = doAdminDatabaseRequest(server, http.MethodPost, base, updated, headers)
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d body = %s", w.Code, w.Body.String())
	}
	w = doAdminDatabaseRequest(server, http.MethodGet, base, "", headers)
	if strings.Count(w.Body.String(), `"id":"crm"`) != 1 || !strings.Contains(w.Body.String(), `"CRM v2"`) {
		t.Fatalf("update did not replace profile: %s", w.Body.String())
	}

	// Config file persisted under the user's config directory.
	stored, err := svc.GetRawUserConfig(context.Background(), p)
	if err != nil {
		t.Fatalf("GetRawUserConfig: %v", err)
	}
	if len(stored.AppConfig.DatabaseProfiles) != 1 || stored.AppConfig.DatabaseProfiles[0].SecretRef != "keyring:crm" {
		t.Fatalf("stored profiles = %#v", stored.AppConfig.DatabaseProfiles)
	}

	// An update that omits secret_ref (the list API never returns it) must
	// keep the bound credential instead of wiping it.
	withoutSecret := `{"id":"crm","name":"CRM keep secret","type":"mysql","host":"db.internal","port":3306,"database":"crm","username":"reader","read_only":true}`
	w = doAdminDatabaseRequest(server, http.MethodPost, base, withoutSecret, headers)
	if w.Code != http.StatusOK {
		t.Fatalf("update without secret_ref status = %d body = %s", w.Code, w.Body.String())
	}
	stored, err = svc.GetRawUserConfig(context.Background(), p)
	if err != nil {
		t.Fatalf("GetRawUserConfig after omit secret: %v", err)
	}
	if got := stored.AppConfig.DatabaseProfiles[0]; got.SecretRef != "keyring:crm" || got.Name != "CRM keep secret" {
		t.Fatalf("omitted secret_ref wiped credential: %#v", got)
	}

	// Rotate the secret: schema version bumps, runtime refresh fires, and the
	// new reference still never appears in responses.
	w = doAdminDatabaseRequest(server, http.MethodPost, "/api/v1/admin/database/profiles/crm/rotate-secret"+adminDatabaseScopeQuery(p), `{"secret_ref":"keyring:crm-v2"}`, headers)
	if w.Code != http.StatusOK {
		t.Fatalf("rotate status = %d body = %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "keyring:crm-v2") || !strings.Contains(w.Body.String(), `"schema_version":1`) {
		t.Fatalf("rotate response = %s", w.Body.String())
	}
	stored, err = svc.GetRawUserConfig(context.Background(), p)
	if err != nil {
		t.Fatalf("GetRawUserConfig: %v", err)
	}
	if got := stored.AppConfig.DatabaseProfiles[0]; got.SecretRef != "keyring:crm-v2" || got.SchemaVersion != 1 {
		t.Fatalf("rotated profile = %#v", got)
	}
	if got := refresher.last(); len(got.profiles) != 1 || got.profiles[0].SchemaVersion != 1 {
		t.Fatalf("rotate refresh call = %#v", got)
	}

	// Delete requires confirm=true.
	deleteTarget := "/api/v1/admin/database/profiles/crm" + adminDatabaseScopeQuery(p)
	w = doAdminDatabaseRequest(server, http.MethodDelete, deleteTarget, "", headers)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("delete without confirm status = %d body = %s", w.Code, w.Body.String())
	}
	w = doAdminDatabaseRequest(server, http.MethodDelete, deleteTarget+"&confirm=true", "", headers)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status = %d body = %s", w.Code, w.Body.String())
	}
	w = doAdminDatabaseRequest(server, http.MethodGet, base, "", headers)
	if strings.Contains(w.Body.String(), `"id":"crm"`) {
		t.Fatalf("profile still listed after delete: %s", w.Body.String())
	}
	if got := refresher.last(); len(got.profiles) != 0 {
		t.Fatalf("delete refresh call = %#v", got)
	}
	// Deleting a missing profile is a 404.
	w = doAdminDatabaseRequest(server, http.MethodDelete, deleteTarget+"&confirm=true", "", headers)
	if w.Code != http.StatusNotFound {
		t.Fatalf("delete missing status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminDatabaseProfileConcurrentMutationsSerialized(t *testing.T) {
	svc, server, p := newAdminDatabaseTestServer(t)
	refresher := &fakeDatabaseProfileRefresher{}
	server.SetDatabaseProfileRuntime(refresher, nil)
	headers := map[string]string{"X-MaClaw-Admin-Secret": "admin-secret"}
	base := "/api/v1/admin/database/profiles" + adminDatabaseScopeQuery(p)
	rotateTarget := "/api/v1/admin/database/profiles/crm/rotate-secret" + adminDatabaseScopeQuery(p)

	w := doAdminDatabaseRequest(server, http.MethodPost, base, adminDatabaseTestMySQLProfile, headers)
	if w.Code != http.StatusOK {
		t.Fatalf("create status = %d body = %s", w.Code, w.Body.String())
	}

	// Concurrent rotates only: each reads, bumps schema_version and persists
	// under the shared mutex, so every rotation must commit. Without the
	// lock, interleaved read-modify-write cycles silently drop bumps.
	const rotates = 8
	var wg sync.WaitGroup
	codes := make(chan int, rotates)
	for i := 0; i < rotates; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"secret_ref":"keyring:crm-v%d"}`, i+2)
			codes <- doAdminDatabaseRequest(server, http.MethodPost, rotateTarget, body, headers).Code
		}(i)
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != http.StatusOK {
			t.Fatalf("rotate status = %d", code)
		}
	}
	stored, err := svc.GetRawUserConfig(context.Background(), p)
	if err != nil {
		t.Fatalf("GetRawUserConfig: %v", err)
	}
	if len(stored.AppConfig.DatabaseProfiles) != 1 {
		t.Fatalf("profiles = %#v", stored.AppConfig.DatabaseProfiles)
	}
	if got := stored.AppConfig.DatabaseProfiles[0].SchemaVersion; got != rotates {
		t.Fatalf("schema_version = %d, want %d (every rotate must commit)", got, rotates)
	}

	// Mixed rotate+upsert: an upsert is a full-profile replace, so whichever
	// mutation commits last wins wholesale — but the final state must be one
	// coherent serialized outcome, never a torn mix of both.
	codes = make(chan int, 2*rotates)
	for i := 0; i < rotates; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"secret_ref":"keyring:crm-r%d"}`, i)
			codes <- doAdminDatabaseRequest(server, http.MethodPost, rotateTarget, body, headers).Code
		}(i)
		go func(i int) {
			defer wg.Done()
			body := strings.Replace(adminDatabaseTestMySQLProfile, `"CRM"`, fmt.Sprintf(`"CRM u%d"`, i), 1)
			codes <- doAdminDatabaseRequest(server, http.MethodPost, base, body, headers).Code
		}(i)
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != http.StatusOK {
			t.Fatalf("mixed mutation status = %d", code)
		}
	}
	stored, err = svc.GetRawUserConfig(context.Background(), p)
	if err != nil {
		t.Fatalf("GetRawUserConfig: %v", err)
	}
	profiles := stored.AppConfig.DatabaseProfiles
	if len(profiles) != 1 {
		t.Fatalf("profiles = %#v", profiles)
	}
	got := profiles[0]
	if got.SecretRef == "keyring:crm" {
		// The last committed mutation was an upsert (full replace): no rotate
		// may have landed after it, so the version must be the upsert's zero.
		if got.SchemaVersion != 0 {
			t.Fatalf("torn state: upsert secret_ref with schema_version %d", got.SchemaVersion)
		}
	} else {
		// A rotate committed last: it must carry a bumped version on top of
		// whatever it read.
		if !strings.HasPrefix(got.SecretRef, "keyring:crm-r") || got.SchemaVersion < 1 {
			t.Fatalf("torn state: profile = %#v", got)
		}
	}
}

func TestAdminDatabaseProfileRejectsInlineSecrets(t *testing.T) {
	_, server, p := newAdminDatabaseTestServer(t)
	headers := map[string]string{"X-MaClaw-Admin-Secret": "admin-secret"}
	target := "/api/v1/admin/database/profiles" + adminDatabaseScopeQuery(p)
	for _, body := range []string{
		`{"id":"a","type":"mysql","host":"h","database":"d","password":"pwned"}`,
		`{"id":"a","type":"mysql","host":"h","database":"d","secret":"pwned"}`,
		`{"id":"a","type":"mysql","host":"h","database":"d","access_token":"pwned"}`,
		`{"id":"a","type":"mysql","host":"h","database":"d","unknown_field":1}`,
		`{"id":"a","type":"mysql","host":"h","database":"d"}, trailing`,
	} {
		w := doAdminDatabaseRequest(server, http.MethodPost, target, body, headers)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %s status = %d body = %s", body, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "pwned") {
			t.Fatalf("error echoes credential value: %s", w.Body.String())
		}
	}
	// Struct validation failures are rejected too.
	for _, body := range []string{
		`{"id":"","type":"mysql","host":"h","database":"d"}`,
		`{"id":"a","type":"mysql","host":"","database":"d"}`,
		`{"id":"a","type":"oracle","host":"h","database":"d"}`,
		`{"id":"../escape","type":"mysql","host":"h","database":"d"}`,
	} {
		w := doAdminDatabaseRequest(server, http.MethodPost, target, body, headers)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %s status = %d body = %s", body, w.Code, w.Body.String())
		}
	}
}

func TestAdminDatabaseProfileTestEndpoint(t *testing.T) {
	svc, server, p := newAdminDatabaseTestServer(t)
	headers := map[string]string{"X-MaClaw-Admin-Secret": "admin-secret"}
	base := "/api/v1/admin/database/profiles" + adminDatabaseScopeQuery(p)

	// A SQL profile referencing a secret cannot be probed when the host has
	// no resolver wired: fail closed with the authentication class.
	w := doAdminDatabaseRequest(server, http.MethodPost, base, adminDatabaseTestMySQLProfile, headers)
	if w.Code != http.StatusOK {
		t.Fatalf("create status = %d body = %s", w.Code, w.Body.String())
	}
	w = doAdminDatabaseRequest(server, http.MethodPost, "/api/v1/admin/database/profiles/crm/test"+adminDatabaseScopeQuery(p), "", headers)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"error_class":"authentication"`) {
		t.Fatalf("sql test without resolver = %d body = %s", w.Code, w.Body.String())
	}

	// Excel profiles need no secret and are testable end to end.
	workbookPath := filepath.Join(svc.DataRoot(), "fixtures", "orders.xlsx")
	if err := excel.WriteFile(workbookPath, excel.WriteData{Sheets: []excel.WriteSheet{{Name: "Sheet1", Rows: [][]excel.WriteCell{{{Value: "id"}, {Value: "amount"}}, {{Value: 1}, {Value: 9.5}}}}}}); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	excelProfile := fmt.Sprintf(`{"id":"orders","name":"Orders","type":"excel","file_path":%q,"read_only":true}`, filepath.ToSlash(workbookPath))
	w = doAdminDatabaseRequest(server, http.MethodPost, base, excelProfile, headers)
	if w.Code != http.StatusOK {
		t.Fatalf("create excel status = %d body = %s", w.Code, w.Body.String())
	}
	w = doAdminDatabaseRequest(server, http.MethodPost, "/api/v1/admin/database/profiles/orders/test"+adminDatabaseScopeQuery(p), "", headers)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"ok"`) || !strings.Contains(w.Body.String(), `"capabilities"`) {
		t.Fatalf("excel test = %d body = %s", w.Code, w.Body.String())
	}

	// A missing file fails with a stable class and never leaks the data root.
	missingProfile := fmt.Sprintf(`{"id":"missing","type":"excel","file_path":%q}`, filepath.ToSlash(filepath.Join(svc.DataRoot(), "fixtures", "missing.xlsx")))
	w = doAdminDatabaseRequest(server, http.MethodPost, base, missingProfile, headers)
	if w.Code != http.StatusOK {
		t.Fatalf("create missing status = %d body = %s", w.Code, w.Body.String())
	}
	w = doAdminDatabaseRequest(server, http.MethodPost, "/api/v1/admin/database/profiles/missing/test"+adminDatabaseScopeQuery(p), "", headers)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"failed"`) {
		t.Fatalf("missing file test = %d body = %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), svc.DataRoot()) || strings.Contains(w.Body.String(), filepath.ToSlash(svc.DataRoot())) {
		t.Fatalf("error leaks data root: %s", w.Body.String())
	}

	// Unknown profiles are a 404.
	w = doAdminDatabaseRequest(server, http.MethodPost, "/api/v1/admin/database/profiles/ghost/test"+adminDatabaseScopeQuery(p), "", headers)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown profile test = %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminDatabaseProfilesValidationAndRateLimit(t *testing.T) {
	_, server, p := newAdminDatabaseTestServer(t)
	headers := map[string]string{"X-MaClaw-Admin-Secret": "admin-secret"}

	// tenant_id/user_id are required and must be path-safe.
	for _, target := range []string{
		"/api/v1/admin/database/profiles",
		"/api/v1/admin/database/profiles?tenant_id=../x&user_id=u",
		"/api/v1/admin/database/profiles?tenant_id=t&user_id=",
	} {
		w := doAdminDatabaseRequest(server, http.MethodGet, target, "", headers)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("GET %s status = %d body = %s", target, w.Code, w.Body.String())
		}
	}
	// Unknown tenant/user is a 404.
	w := doAdminDatabaseRequest(server, http.MethodGet, "/api/v1/admin/database/profiles?tenant_id=nope&user_id=nope", "", headers)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown user status = %d body = %s", w.Code, w.Body.String())
	}

	// The dedicated limiter throttles bursts beyond 30/min per identity.
	server.databaseAdminLimiter = newAuthLimiter(2, time.Minute)
	target := "/api/v1/admin/database/profiles" + adminDatabaseScopeQuery(p)
	for i := 0; i < 2; i++ {
		w = doAdminDatabaseRequest(server, http.MethodGet, target, "", headers)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d status = %d body = %s", i, w.Code, w.Body.String())
		}
	}
	w = doAdminDatabaseRequest(server, http.MethodGet, target, "", headers)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("rate limit status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminDatabaseProfilesRoundTripReplicaSSH(t *testing.T) {
	_, server, p := newAdminDatabaseTestServer(t)
	headers := map[string]string{"X-MaClaw-Admin-Secret": "admin-secret"}
	base := "/api/v1/admin/database/profiles" + adminDatabaseScopeQuery(p)
	create := `{"id":"crm","name":"CRM","type":"mysql","host":"db.internal","port":3306,"database":"crm","username":"reader","secret_ref":"keyring:crm","read_only":true,"ssh_session_id":"ssh-primary","replica_host":"replica.internal","replica_port":3307,"replica_ssh_session_id":"ssh-replica"}`
	w := doAdminDatabaseRequest(server, http.MethodPost, base, create, headers)
	if w.Code != http.StatusOK {
		t.Fatalf("create status = %d body = %s", w.Code, w.Body.String())
	}
	w = doAdminDatabaseRequest(server, http.MethodGet, base, "", headers)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d body = %s", w.Code, w.Body.String())
	}
	got := w.Body.String()
	for _, want := range []string{
		`"replica_host":"replica.internal"`,
		`"replica_port":3307`,
		`"ssh_session_id":"ssh-primary"`,
		`"replica_ssh_session_id":"ssh-replica"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("list missing %s: %s", want, got)
		}
	}
	if strings.Contains(got, "keyring:crm") || strings.Contains(got, `"secret_ref":`) {
		t.Fatalf("list leaks secret_ref: %s", got)
	}

	// Visible tunnel fields are echoed by list, so omitting them on update
	// clears them. secret_ref is not echoed and must still be preserved.
	w = doAdminDatabaseRequest(server, http.MethodPost, base, `{"id":"crm","name":"CRM v2","type":"mysql","host":"db.internal","port":3306,"database":"crm","username":"reader","read_only":true,"replica_host":"replica.internal","replica_port":3307}`, headers)
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d body = %s", w.Code, w.Body.String())
	}
	w = doAdminDatabaseRequest(server, http.MethodGet, base, "", headers)
	got = w.Body.String()
	if !strings.Contains(got, `"name":"CRM v2"`) || !strings.Contains(got, `"replica_host":"replica.internal"`) {
		t.Fatalf("update lost name or replica host: %s", got)
	}
	if strings.Contains(got, `"replica_ssh_session_id"`) || strings.Contains(got, `"ssh_session_id"`) {
		t.Fatalf("omitted tunnel ids must be cleared: %s", got)
	}
	if !strings.Contains(got, `"has_secret_ref":true`) {
		t.Fatalf("secret_ref must be preserved: %s", got)
	}
}

func TestMergeDatabaseProfileUpdatePreservesSecretsNotReplica(t *testing.T) {
	existing := database.Profile{
		ID: "crm", Type: database.SourceMySQL, Host: "db.internal", Database: "crm",
		SecretRef: "keyring:crm", FilePath: "hidden.xlsx", DSN: "server=db",
		SSHSessionID: "ssh-primary", ReplicaHost: "replica.internal", ReplicaPort: 3307,
		ReplicaSSHSessionID: "ssh-replica", SchemaVersion: 3,
	}
	got := mergeDatabaseProfileUpdate(existing, database.Profile{
		ID: "crm", Type: database.SourceMySQL, Host: "db.internal", Database: "crm", Name: "CRM v2",
	})
	if got.SecretRef != "keyring:crm" || got.FilePath != "hidden.xlsx" || got.DSN != "server=db" || got.SchemaVersion != 3 {
		t.Fatalf("secrets/paths dropped: %+v", got)
	}
	if got.SSHSessionID != "" || got.ReplicaHost != "" || got.ReplicaPort != 0 || got.ReplicaSSHSessionID != "" {
		t.Fatalf("visible replica/ssh fields must clear when omitted: %+v", got)
	}
	if got.Name != "CRM v2" {
		t.Fatalf("name = %q", got.Name)
	}
}

func TestAdminDatabaseExcelUpdatePreservesFilePath(t *testing.T) {
	svc, server, p := newAdminDatabaseTestServer(t)
	headers := map[string]string{"X-MaClaw-Admin-Secret": "admin-secret"}
	base := "/api/v1/admin/database/profiles" + adminDatabaseScopeQuery(p)
	workbookPath := filepath.Join(svc.DataRoot(), "orders.xlsx")
	if err := excel.WriteFile(workbookPath, excel.WriteData{Sheets: []excel.WriteSheet{{Name: "Sheet1", Rows: [][]excel.WriteCell{{{Value: "id"}}}}}}); err != nil {
		t.Fatalf("write excel: %v", err)
	}
	create := fmt.Sprintf(`{"id":"orders","name":"Orders","type":"excel","file_path":%q,"read_only":true}`, filepath.ToSlash(workbookPath))
	w := doAdminDatabaseRequest(server, http.MethodPost, base, create, headers)
	if w.Code != http.StatusOK {
		t.Fatalf("create status = %d body = %s", w.Code, w.Body.String())
	}
	w = doAdminDatabaseRequest(server, http.MethodPost, base, `{"id":"orders","name":"Orders v2","type":"excel","read_only":true}`, headers)
	if w.Code != http.StatusOK {
		t.Fatalf("update without file_path status = %d body = %s", w.Code, w.Body.String())
	}
	w = doAdminDatabaseRequest(server, http.MethodGet, base, "", headers)
	got := w.Body.String()
	if !strings.Contains(got, `"name":"Orders v2"`) {
		t.Fatalf("name not updated: %s", got)
	}
	if strings.Contains(got, `"file_path"`) || strings.Contains(got, workbookPath) {
		t.Fatalf("list leaked file_path: %s", got)
	}
	w = doAdminDatabaseRequest(server, http.MethodPost, "/api/v1/admin/database/profiles/orders/test"+adminDatabaseScopeQuery(p), "", headers)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Fatalf("test after nameless file_path update = %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminDatabaseProfileRuntimeRefreshReachesExecutor(t *testing.T) {
	// The real executor bridge must accept refresh calls safely even when no
	// manager is cached for the tenant/user yet (and reject blank IDs).
	executor := &agentservice.CoreAgentExecutor{}
	t.Cleanup(func() { _ = executor.Close() })
	old := database.Profile{ID: "crm", Type: database.SourceMySQL, Host: "db.internal", Database: "crm", ReadOnly: true}
	executor.RefreshDatabaseProfiles("tenant-1", "user-1", []database.Profile{old})
	executor.RefreshDatabaseProfiles("", "user-1", []database.Profile{old})
	executor.RefreshDatabaseProfiles("tenant-1", "", []database.Profile{old})

	// Wiring the real executor through the setter keeps the CRUD path working.
	_, server, p := newAdminDatabaseTestServer(t)
	server.SetDatabaseProfileRuntime(executor, nil)
	headers := map[string]string{"X-MaClaw-Admin-Secret": "admin-secret"}
	target := "/api/v1/admin/database/profiles" + adminDatabaseScopeQuery(p)
	w := doAdminDatabaseRequest(server, http.MethodPost, target, adminDatabaseTestMySQLProfile, headers)
	if w.Code != http.StatusOK {
		t.Fatalf("create status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestDatabaseAuditEventMappingStaysMetadataOnly(t *testing.T) {
	event := databaseAuditEventToService(
		agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"},
		database.AuditEvent{
			Action:         "execute",
			ProfileID:      "crm-prod",
			SessionID:      "sess-1",
			ConnectionID:   "conn-1",
			SQLFingerprint: "fp-abc",
			ParameterCount: 2,
			AffectedRows:   3,
			Risk:           "elevated",
			ResultClass:    "ok",
			ApprovalID:     "db-appr-1",
			ReceiptID:      "db-receipt-1",
			Timestamp:      time.Date(2026, 9, 2, 1, 0, 0, 0, time.UTC),
		},
	)
	if event.TenantID != "tenant-a" || event.UserID != "user-1" {
		t.Fatalf("tenant binding lost: %+v", event)
	}
	if event.Action != "database.execute" || event.ResourceType != "database_profile" || event.ResourceID != "crm-prod" {
		t.Fatalf("mapping = %+v", event)
	}
	for _, want := range []string{"session_id", "connection_id", "sql_fingerprint", "parameter_count", "affected_rows", "risk", "result_class", "approval_id", "receipt_id"} {
		if event.Metadata[want] == "" {
			t.Fatalf("metadata missing %s: %+v", want, event.Metadata)
		}
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"token", "password", "secret"} {
		if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
			t.Fatalf("audit event carries %s: %s", forbidden, encoded)
		}
	}
}

func TestWireDatabaseAuditSinkIntoCentralChain(t *testing.T) {
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(
		agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"},
		agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	executor := &agentservice.CoreAgentExecutor{}
	wireDatabaseAuditSink(svc, executor)
	if executor.DatabaseAuditSink == nil {
		t.Fatal("sink not wired")
	}
	executor.DatabaseAuditSink(context.Background(),
		agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"},
		database.AuditEvent{Action: "query", ProfileID: "p", ResultClass: "ok", SQLFingerprint: "fp"})
	// The central store must now hold the event; a sink error must not panic.
	executor.DatabaseAuditSink(context.Background(),
		agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"},
		database.AuditEvent{Action: "query", ProfileID: "p", ResultClass: "ok"})
}

func TestAdminDatabaseReceiptLookupFromAuditChain(t *testing.T) {
	svc, server, p := newAdminDatabaseTestServer(t)
	if err := svc.RecordAuditEvent(context.Background(), databaseAuditEventToService(p, database.AuditEvent{
		Action:         "execute",
		ProfileID:      "crm",
		ReceiptID:      "db-receipt-xyz",
		ResultClass:    "ok",
		ApprovalID:     "db-appr-1",
		SQLFingerprint: "fp-1",
		AffectedRows:   2,
		Timestamp:      time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC),
	})); err != nil {
		t.Fatalf("RecordAuditEvent: %v", err)
	}
	headers := map[string]string{"X-MaClaw-Admin-Secret": "admin-secret"}
	w := doAdminDatabaseRequest(server, http.MethodGet, "/api/v1/admin/database/receipts/db-receipt-xyz"+adminDatabaseScopeQuery(p), "", headers)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var payload struct {
		Receipt map[string]any `json:"receipt"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v body = %s", err, w.Body.String())
	}
	if payload.Receipt["receipt_id"] != "db-receipt-xyz" || payload.Receipt["profile_id"] != "crm" || payload.Receipt["action"] != "execute" {
		t.Fatalf("receipt = %#v", payload.Receipt)
	}
	encoded := strings.ToLower(w.Body.String())
	for _, forbidden := range []string{"password", "token", "secret"} {
		if strings.Contains(encoded, forbidden) && forbidden != "admin-secret" {
			t.Fatalf("receipt body contains %s: %s", forbidden, w.Body.String())
		}
	}
	missing := doAdminDatabaseRequest(server, http.MethodGet, "/api/v1/admin/database/receipts/missing"+adminDatabaseScopeQuery(p), "", headers)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d body = %s", missing.Code, missing.Body.String())
	}
}
