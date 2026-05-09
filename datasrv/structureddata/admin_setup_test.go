package structureddata

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFirstLoginAdminInitializationFlow(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	server := NewHTTPServer(NewService(store, "sqlite"), "", "test")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("setup status before init status=%d body=%s", w.Code, w.Body.String())
	}
	var status SetupStatus
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("decode setup status: %v", err)
	}
	if status.Initialized {
		t.Fatal("fresh store should not be initialized")
	}
	if status.PasswordPolicy == nil || status.PasswordPolicy.MinLength != defaultAdminPasswordMinLength || !status.PasswordPolicy.OfflineResetAvailable {
		t.Fatalf("unexpected setup password policy: %#v", status.PasswordPolicy)
	}

	req = jsonRequest(http.MethodPost, "/api/v1/setup/admin", InitializeAdminInput{
		TenantID:    "default",
		Username:    "Admin",
		Password:    "change-me-123",
		DisplayName: "Data Administrator",
	})
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("initialize admin status=%d body=%s", w.Code, w.Body.String())
	}
	var initResult InitializeAdminResult
	if err := json.NewDecoder(w.Body).Decode(&initResult); err != nil {
		t.Fatalf("decode init result: %v", err)
	}
	if !initResult.Initialized || initResult.Username != "admin" || initResult.Token == "" {
		t.Fatalf("unexpected init result: %#v", initResult)
	}

	req = jsonRequest(http.MethodPost, "/api/v1/setup/admin", InitializeAdminInput{Username: "other", Password: "change-me-123"})
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("second initialize should conflict status=%d body=%s", w.Code, w.Body.String())
	}

	req = jsonRequest(http.MethodPost, "/api/v1/login", LoginInput{Username: "admin", Password: "wrong-password"})
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong login should be unauthorized status=%d body=%s", w.Code, w.Body.String())
	}

	req = jsonRequest(http.MethodPost, "/api/v1/login", LoginInput{Username: "admin", Password: "change-me-123"})
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", w.Code, w.Body.String())
	}
	var login LoginResult
	if err := json.NewDecoder(w.Body).Decode(&login); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if login.Token == "" || login.Role != "data_admin" {
		t.Fatalf("unexpected login result: %#v", login)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/capabilities", nil)
	req.Header.Set("Authorization", "Bearer "+login.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("session token should access data API status=%d body=%s", w.Code, w.Body.String())
	}

	req = jsonRequest(http.MethodPost, "/api/v1/data/datasets", CreateDatasetInput{Domain: "ops", Name: "tickets", Title: "Operations Tickets"})
	req.Header.Set("Authorization", "Bearer "+login.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create dataset with session token status=%d body=%s", w.Code, w.Body.String())
	}
	var dataset Dataset
	if err := json.NewDecoder(w.Body).Decode(&dataset); err != nil {
		t.Fatalf("decode dataset: %v", err)
	}
	if dataset.ID != "ops.tickets" {
		t.Fatalf("unexpected dataset: %#v", dataset)
	}

	req = jsonRequest(http.MethodPost, "/api/v1/data/datasets/ops.tickets/records", CreateRecordInput{
		Title: "First support ticket",
		Tags:  []string{"support", "urgent"},
		Data:  map[string]any{"ticket_no": "T-001", "customer": "Acme"},
	})
	req.Header.Set("Authorization", "Bearer "+login.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create record with session token status=%d body=%s", w.Code, w.Body.String())
	}

	req = jsonRequest(http.MethodPost, "/api/v1/data/datasets/ops.tickets/records/query", QueryRecordsInput{Q: "Acme", Tag: "urgent", Limit: 10})
	req.Header.Set("Authorization", "Bearer "+login.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("query records with session token status=%d body=%s", w.Code, w.Body.String())
	}
	var recordsResp ListResponse[Record]
	if err := json.NewDecoder(w.Body).Decode(&recordsResp); err != nil {
		t.Fatalf("decode queried records: %v", err)
	}
	if len(recordsResp.Items) != 1 || recordsResp.Items[0].Title != "First support ticket" {
		t.Fatalf("unexpected queried records: %#v", recordsResp)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/governance/evidence-summary.txt?lang=zh-CN", nil)
	req.Header.Set("Authorization", "Bearer "+login.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "治理证据摘要") || !strings.Contains(w.Body.String(), "证据 ID:") {
		t.Fatalf("Chinese evidence summary with session token status=%d body=%s", w.Code, w.Body.String())
	}

	accounts, err := NewService(store, "sqlite").ListAdminAccounts(t.Context(), "default")
	if err != nil {
		t.Fatalf("ListAdminAccounts: %v", err)
	}
	if len(accounts.Items) != 1 || accounts.Items[0].Username != "admin" {
		t.Fatalf("unexpected admin accounts: %#v", accounts)
	}

	svc := NewService(store, "sqlite")
	if _, err := svc.ResetAdminPassword(t.Context(), ResetAdminPasswordInput{Username: "admin", Password: "new-password-456"}); err != nil {
		t.Fatalf("ResetAdminPassword: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/capabilities", nil)
	req.Header.Set("Authorization", "Bearer "+login.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("old session token should be revoked after password reset status=%d body=%s", w.Code, w.Body.String())
	}
	req = jsonRequest(http.MethodPost, "/api/v1/login", LoginInput{Username: "admin", Password: "change-me-123"})
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("old password should fail after reset status=%d body=%s", w.Code, w.Body.String())
	}
	req = jsonRequest(http.MethodPost, "/api/v1/login", LoginInput{Username: "admin", Password: "new-password-456"})
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("new password login failed status=%d body=%s", w.Code, w.Body.String())
	}
	audit, err := svc.QueryAuditLogs(t.Context(), Principal{TenantID: "default", UserID: "admin", Role: "data_admin"}, QueryAuditLogsInput{TargetType: "admin_user", Limit: 20})
	if err != nil {
		t.Fatalf("QueryAuditLogs: %v", err)
	}
	for _, action := range []string{"admin.setup_initialize", "admin.login", "admin.password_reset"} {
		if !containsAuditAction(audit, action) {
			t.Fatalf("expected audit action %q in %#v", action, audit)
		}
	}
}

func TestAdminAccountManagementHTTPFlow(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	server := NewHTTPServer(NewService(store, "sqlite"), "", "test")

	req := jsonRequest(http.MethodPost, "/api/v1/setup/admin", InitializeAdminInput{Username: "admin", Password: "change-me-123"})
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("initialize admin status=%d body=%s", w.Code, w.Body.String())
	}
	var initResult InitializeAdminResult
	if err := json.NewDecoder(w.Body).Decode(&initResult); err != nil {
		t.Fatalf("decode init result: %v", err)
	}

	req = jsonRequest(http.MethodPost, "/api/v1/data/admin/accounts", CreateAdminAccountInput{
		Username:    "ops-admin",
		Password:    "ops-password-123",
		DisplayName: "Operations Admin",
		Role:        "data_admin",
	})
	req.Header.Set("Authorization", "Bearer "+initResult.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create admin account status=%d body=%s", w.Code, w.Body.String())
	}
	var created AdminAccountResult
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created admin: %v", err)
	}
	if created.Account.Username != "ops-admin" || !created.Account.Enabled {
		t.Fatalf("unexpected created admin: %#v", created)
	}

	req = jsonRequest(http.MethodPost, "/api/v1/data/admin/accounts", CreateAdminAccountInput{
		Username: "bad-role-admin",
		Password: "bad-role-password-123",
		Role:     "owner",
	})
	req.Header.Set("Authorization", "Bearer "+initResult.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid admin role should be rejected status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/admin/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+initResult.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list admin sessions status=%d body=%s", w.Code, w.Body.String())
	}
	var sessions ListAdminSessionsResult
	if err := json.NewDecoder(w.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode admin sessions: %v", err)
	}
	if len(sessions.Items) != 1 || !sessions.Items[0].Current || sessions.Items[0].Username != "admin" {
		t.Fatalf("unexpected initial admin sessions: %#v", sessions)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/admin/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+initResult.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list admin accounts status=%d body=%s", w.Code, w.Body.String())
	}
	var listed ListAdminAccountsResult
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode listed admins: %v", err)
	}
	if len(listed.Items) != 2 {
		t.Fatalf("expected two administrators, got %#v", listed)
	}

	loginReq := func() *http.Request {
		return jsonRequest(http.MethodPost, "/api/v1/login", LoginInput{Username: "ops-admin", Password: "ops-password-123"})
	}
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, loginReq())
	if w.Code != http.StatusOK {
		t.Fatalf("new admin login status=%d body=%s", w.Code, w.Body.String())
	}
	var opsLogin LoginResult
	if err := json.NewDecoder(w.Body).Decode(&opsLogin); err != nil {
		t.Fatalf("decode ops login: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/admin/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+initResult.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list two admin sessions status=%d body=%s", w.Code, w.Body.String())
	}
	sessions = ListAdminSessionsResult{}
	if err := json.NewDecoder(w.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode two admin sessions: %v", err)
	}
	var opsSessionID string
	for _, item := range sessions.Items {
		if item.Username == "ops-admin" {
			opsSessionID = item.ID
		}
	}
	if opsSessionID == "" {
		t.Fatalf("ops-admin session not found in %#v", sessions)
	}

	req = jsonRequest(http.MethodPatch, "/api/v1/data/admin/sessions/"+opsSessionID, UpdateAdminSessionInput{ExpiresHours: 1})
	req.Header.Set("Authorization", "Bearer "+initResult.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update admin session status=%d body=%s", w.Code, w.Body.String())
	}
	var updatedSession AdminSessionResult
	if err := json.NewDecoder(w.Body).Decode(&updatedSession); err != nil {
		t.Fatalf("decode updated session: %v", err)
	}
	if updatedSession.Session.ID != opsSessionID || updatedSession.Session.Username != "ops-admin" {
		t.Fatalf("unexpected updated session: %#v", updatedSession)
	}

	req = jsonRequest(http.MethodPatch, "/api/v1/data/admin/sessions/"+opsSessionID, UpdateAdminSessionInput{ExpiresHours: 169})
	req.Header.Set("Authorization", "Bearer "+initResult.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("oversized session ttl should be rejected status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/data/admin/sessions/"+opsSessionID, nil)
	req.Header.Set("Authorization", "Bearer "+initResult.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke admin session status=%d body=%s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/capabilities", nil)
	req.Header.Set("Authorization", "Bearer "+opsLogin.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked admin session should be unauthorized status=%d body=%s", w.Code, w.Body.String())
	}

	disabled := false
	req = jsonRequest(http.MethodPatch, "/api/v1/data/admin/accounts/ops-admin", UpdateAdminAccountInput{Enabled: &disabled})
	req.Header.Set("Authorization", "Bearer "+initResult.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("disable admin account status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, loginReq())
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("disabled admin login should be unauthorized status=%d body=%s", w.Code, w.Body.String())
	}

	req = jsonRequest(http.MethodPatch, "/api/v1/data/admin/accounts/admin", UpdateAdminAccountInput{Enabled: &disabled})
	req.Header.Set("Authorization", "Bearer "+initResult.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("disabling last enabled admin should be rejected status=%d body=%s", w.Code, w.Body.String())
	}
}

func jsonRequest(method, path string, body any) *http.Request {
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func containsAuditAction(items []AuditLog, action string) bool {
	for _, item := range items {
		if item.Action == action {
			return true
		}
	}
	return false
}

func TestAdminLoginCleansExpiredSessions(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	if _, err := svc.InitializeAdmin(t.Context(), InitializeAdminInput{Username: "admin", Password: "change-me-123"}); err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	expired := adminSessionRecord{
		ID:        "sess_expired",
		TenantID:  "default",
		UserID:    "expired_user",
		Username:  "admin",
		Role:      "data_admin",
		TokenHash: apiKeyHash("expired-token"),
		ExpiresAt: time.Now().Add(-time.Hour).UTC(),
		CreatedAt: time.Now().Add(-2 * time.Hour).UTC(),
	}
	if _, err := store.CreateAdminSession(t.Context(), expired); err != nil {
		t.Fatalf("CreateAdminSession expired: %v", err)
	}
	var before int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM admin_sessions WHERE id = ?`, expired.ID).Scan(&before); err != nil {
		t.Fatalf("count expired before login: %v", err)
	}
	if before != 1 {
		t.Fatalf("expired session setup count=%d, want 1", before)
	}
	if _, err := svc.Login(t.Context(), LoginInput{Username: "admin", Password: "change-me-123"}); err != nil {
		t.Fatalf("Login: %v", err)
	}
	var after int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM admin_sessions WHERE id = ?`, expired.ID).Scan(&after); err != nil {
		t.Fatalf("count expired after login: %v", err)
	}
	if after != 0 {
		t.Fatalf("expired session should be cleaned after login, count=%d", after)
	}
}

func TestAdminAccountUpdateKeepsEnabledDataAdmin(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	initResult, err := svc.InitializeAdmin(t.Context(), InitializeAdminInput{Username: "admin", Password: "change-me-123"})
	if err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	p := Principal{TenantID: initResult.TenantID, UserID: "admin", Role: "data_admin"}
	if _, err := svc.CreateAdminAccount(t.Context(), p, CreateAdminAccountInput{Username: "analyst", Password: "analyst-password-123", Role: "data_user"}); err != nil {
		t.Fatalf("CreateAdminAccount data_user: %v", err)
	}
	if _, err := svc.CreateAdminAccount(t.Context(), p, CreateAdminAccountInput{Username: "owner", Password: "owner-password-123", Role: "owner"}); err == nil {
		t.Fatal("invalid explicit admin role should be rejected instead of defaulting to data_admin")
	}
	disabled := false
	if _, err := svc.UpdateAdminAccount(t.Context(), p, "default", "admin", UpdateAdminAccountInput{Enabled: &disabled}); err == nil {
		t.Fatal("disabling the only enabled data_admin should be rejected even when another enabled data_user exists")
	}
	if _, err := svc.UpdateAdminAccount(t.Context(), p, "default", "admin", UpdateAdminAccountInput{Role: "data_user"}); err == nil {
		t.Fatal("demoting the only enabled data_admin should be rejected")
	}
	if _, err := svc.CreateAdminAccount(t.Context(), p, CreateAdminAccountInput{Username: "ops-admin", Password: "ops-password-123", Role: "data_admin"}); err != nil {
		t.Fatalf("CreateAdminAccount second data_admin: %v", err)
	}
	if _, err := svc.UpdateAdminAccount(t.Context(), p, "default", "admin", UpdateAdminAccountInput{Role: "data_user"}); err != nil {
		t.Fatalf("demoting one data_admin should work when another enabled data_admin remains: %v", err)
	}
}

func TestAdminPasswordMinimumLengthCanBeConfigured(t *testing.T) {
	t.Setenv("MACLAW_DATA_ADMIN_PASSWORD_MIN_LENGTH", "16")
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	if _, err := svc.InitializeAdmin(t.Context(), InitializeAdminInput{Username: "admin", Password: "short-123"}); err == nil {
		t.Fatal("expected configured password minimum to reject short password")
	}
	if _, err := svc.InitializeAdmin(t.Context(), InitializeAdminInput{Username: "admin", Password: "long-password-123"}); err != nil {
		t.Fatalf("InitializeAdmin with configured password minimum: %v", err)
	}
	if _, err := svc.ResetAdminPassword(t.Context(), ResetAdminPasswordInput{Username: "admin", Password: "tiny-123"}); err == nil {
		t.Fatal("expected configured password minimum to reject reset password")
	}
	status, err := svc.SetupStatus(t.Context())
	if err != nil {
		t.Fatalf("SetupStatus: %v", err)
	}
	if status.PasswordPolicy == nil || status.PasswordPolicy.MinLength != 16 || status.PasswordPolicy.LockoutEnabled {
		t.Fatalf("unexpected configured password policy: %#v", status.PasswordPolicy)
	}
}

func TestAdminLoginFailureLockoutCanBeConfigured(t *testing.T) {
	t.Setenv("MACLAW_DATA_ADMIN_LOGIN_MAX_FAILURES", "2")
	t.Setenv("MACLAW_DATA_ADMIN_LOGIN_LOCKOUT_MINUTES", "1")
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	now := time.Date(2026, 5, 7, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.InitializeAdmin(t.Context(), InitializeAdminInput{Username: "admin", Password: "change-me-123"}); err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	status, err := svc.SetupStatus(t.Context())
	if err != nil {
		t.Fatalf("SetupStatus: %v", err)
	}
	if status.PasswordPolicy == nil || !status.PasswordPolicy.LockoutEnabled || status.PasswordPolicy.LoginMaxFailures != 2 || status.PasswordPolicy.LoginLockoutMinutes != 1 {
		t.Fatalf("unexpected lockout password policy: %#v", status.PasswordPolicy)
	}
	for i := 0; i < 2; i++ {
		if _, err := svc.Login(t.Context(), LoginInput{Username: "admin", Password: "wrong-password"}); err == nil {
			t.Fatalf("wrong password attempt %d should fail", i+1)
		}
	}
	if _, err := svc.Login(t.Context(), LoginInput{Username: "admin", Password: "change-me-123"}); err == nil {
		t.Fatal("correct password should be rejected while account is locked")
	}
	now = now.Add(2 * time.Minute)
	if _, err := svc.Login(t.Context(), LoginInput{Username: "admin", Password: "change-me-123"}); err != nil {
		t.Fatalf("correct password should work after lockout expires: %v", err)
	}
}

func TestAdminLoginFailureLockoutPersistsAcrossServiceRestart(t *testing.T) {
	t.Setenv("MACLAW_DATA_ADMIN_LOGIN_MAX_FAILURES", "2")
	t.Setenv("MACLAW_DATA_ADMIN_LOGIN_LOCKOUT_MINUTES", "10")
	path := filepath.Join(t.TempDir(), "data.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	svc := NewService(store, "sqlite")
	now := time.Date(2026, 5, 7, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.InitializeAdmin(t.Context(), InitializeAdminInput{Username: "admin", Password: "change-me-123"}); err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := svc.Login(t.Context(), LoginInput{Username: "admin", Password: "wrong-password"}); err == nil {
			t.Fatalf("wrong password attempt %d should fail", i+1)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("reopen NewSQLiteStore: %v", err)
	}
	defer reopened.Close()
	restarted := NewService(reopened, "sqlite")
	restarted.now = func() time.Time { return now.Add(1 * time.Minute) }
	if _, err := restarted.Login(t.Context(), LoginInput{Username: "admin", Password: "change-me-123"}); err == nil {
		t.Fatal("correct password should remain rejected after service restart while persisted lockout is active")
	}
	restarted.now = func() time.Time { return now.Add(11 * time.Minute) }
	if _, err := restarted.Login(t.Context(), LoginInput{Username: "admin", Password: "change-me-123"}); err != nil {
		t.Fatalf("correct password should work after persisted lockout expires: %v", err)
	}
	user, err := reopened.FindAdminUser(t.Context(), "default", "admin")
	if err != nil {
		t.Fatalf("FindAdminUser: %v", err)
	}
	if user.LoginFailureCount != 0 || !user.LoginLockedUntil.IsZero() {
		t.Fatalf("successful login should clear persisted failure state: count=%d locked_until=%s", user.LoginFailureCount, user.LoginLockedUntil)
	}
}

func TestAdminLoginFailureClearsAfterSuccessfulLogin(t *testing.T) {
	t.Setenv("MACLAW_DATA_ADMIN_LOGIN_MAX_FAILURES", "2")
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	if _, err := svc.InitializeAdmin(t.Context(), InitializeAdminInput{Username: "admin", Password: "change-me-123"}); err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	if _, err := svc.Login(t.Context(), LoginInput{Username: "admin", Password: "wrong-password"}); err == nil {
		t.Fatal("wrong password should fail")
	}
	if _, err := svc.Login(t.Context(), LoginInput{Username: "admin", Password: "change-me-123"}); err != nil {
		t.Fatalf("correct password should clear failure count: %v", err)
	}
	if _, err := svc.Login(t.Context(), LoginInput{Username: "admin", Password: "wrong-password"}); err == nil {
		t.Fatal("second wrong password should fail")
	}
	if _, err := svc.Login(t.Context(), LoginInput{Username: "admin", Password: "change-me-123"}); err != nil {
		t.Fatalf("successful login should have cleared previous failure count: %v", err)
	}
}
