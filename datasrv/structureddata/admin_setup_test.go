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

	"golang.org/x/crypto/bcrypt"
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

func TestTenantAdminCannotGrantGlobalAdminScope(t *testing.T) {
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
	global := Principal{TenantID: initResult.TenantID, UserID: "admin", Role: "data_admin", AdminScope: "global"}
	created, err := svc.CreateAdminAccount(t.Context(), global, CreateAdminAccountInput{TenantID: "tenant-a", AdminScope: "tenant", Username: "tenant-admin", Password: "tenant-password-123", Role: "data_admin"})
	if err != nil {
		t.Fatalf("CreateAdminAccount tenant admin: %v", err)
	}
	tenantAdmin := Principal{TenantID: "tenant-a", UserID: created.Account.ID, Role: "data_admin", AdminScope: "tenant"}
	if _, err := svc.CreateAdminAccount(t.Context(), tenantAdmin, CreateAdminAccountInput{TenantID: "tenant-a", AdminScope: "global", Username: "bad-global", Password: "bad-global-password-123", Role: "data_admin"}); err == nil {
		t.Fatal("tenant admin should not create a global administrator")
	}
	if _, err := svc.UpdateAdminAccount(t.Context(), tenantAdmin, "tenant-a", "tenant-admin", UpdateAdminAccountInput{AdminScope: "global"}); err == nil {
		t.Fatal("tenant admin should not promote an administrator to global scope")
	}
}

func TestTenantAdminCreateAccountDefaultsToOwnTenant(t *testing.T) {
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
	global := Principal{TenantID: initResult.TenantID, UserID: "admin", Role: "data_admin", AdminScope: "global"}
	created, err := svc.CreateAdminAccount(t.Context(), global, CreateAdminAccountInput{TenantID: "tenant-a", AdminScope: "tenant", Username: "tenant-admin", Password: "tenant-password-123", Role: "data_admin"})
	if err != nil {
		t.Fatalf("CreateAdminAccount tenant admin: %v", err)
	}
	tenantAdmin := Principal{TenantID: "tenant-a", UserID: created.Account.ID, Role: "data_admin", AdminScope: "tenant"}
	out, err := svc.CreateAdminAccount(t.Context(), tenantAdmin, CreateAdminAccountInput{Username: "assistant-admin", Password: "assistant-password-123", Role: "data_admin"})
	if err != nil {
		t.Fatalf("tenant admin create own-tenant account without tenant_id: %v", err)
	}
	if out.Account.TenantID != "tenant-a" || out.Account.AdminScope != "tenant" {
		t.Fatalf("tenant admin blank tenant_id should default to own tenant: %#v", out.Account)
	}
}

func TestTenantAdminMutationsDefaultToOwnTenant(t *testing.T) {
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
	global := Principal{TenantID: initResult.TenantID, UserID: "admin", Role: "data_admin", AdminScope: "global"}
	created, err := svc.CreateAdminAccount(t.Context(), global, CreateAdminAccountInput{TenantID: "tenant-a", AdminScope: "tenant", Username: "tenant-admin", Password: "tenant-password-123", Role: "data_admin"})
	if err != nil {
		t.Fatalf("CreateAdminAccount tenant admin: %v", err)
	}
	tenantLogin, err := svc.Login(t.Context(), LoginInput{TenantID: "tenant-a", Username: "tenant-admin", Password: "tenant-password-123"})
	if err != nil {
		t.Fatalf("Login tenant admin: %v", err)
	}
	tenantAdmin := Principal{TenantID: "tenant-a", UserID: created.Account.ID, Role: "data_admin", AdminScope: "tenant", Policy: &APIKeyPolicy{ID: tenantLogin.Token, AllowAdmin: true}}
	displayName := "Tenant Operator"
	updated, err := svc.UpdateAdminAccount(t.Context(), tenantAdmin, "", "tenant-admin", UpdateAdminAccountInput{DisplayName: &displayName})
	if err != nil {
		t.Fatalf("tenant admin update own account without tenant query: %v", err)
	}
	if updated.Account.TenantID != "tenant-a" || updated.Account.DisplayName != displayName {
		t.Fatalf("tenant admin update should default to own tenant: %#v", updated.Account)
	}
	sessions, err := svc.ListAdminSessionsForPrincipal(t.Context(), tenantAdmin, "")
	if err != nil {
		t.Fatalf("ListAdminSessionsForPrincipal tenant: %v", err)
	}
	if len(sessions.Items) != 1 {
		t.Fatalf("expected one tenant session: %#v", sessions)
	}
	patchedSession, err := svc.UpdateAdminSession(t.Context(), tenantAdmin, "", sessions.Items[0].ID, UpdateAdminSessionInput{ExpiresHours: 1})
	if err != nil {
		t.Fatalf("tenant admin update own session without tenant query: %v", err)
	}
	if patchedSession.Session.TenantID != "tenant-a" {
		t.Fatalf("tenant admin session update should default to own tenant: %#v", patchedSession.Session)
	}
	if _, err := svc.RevokeAdminSession(t.Context(), tenantAdmin, "", sessions.Items[0].ID); err != nil {
		t.Fatalf("tenant admin revoke own session without tenant query: %v", err)
	}
}

func TestCannotRemoveLastEnabledGlobalAdministrator(t *testing.T) {
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
	global := Principal{TenantID: initResult.TenantID, UserID: "admin", Role: "data_admin", AdminScope: "global"}
	if _, err := svc.CreateAdminAccount(t.Context(), global, CreateAdminAccountInput{TenantID: "tenant-a", AdminScope: "tenant", Username: "tenant-admin", Password: "tenant-password-123", Role: "data_admin"}); err != nil {
		t.Fatalf("CreateAdminAccount tenant admin: %v", err)
	}
	disabled := false
	if _, err := svc.UpdateAdminAccount(t.Context(), global, "default", "admin", UpdateAdminAccountInput{Enabled: &disabled}); err == nil || !strings.Contains(err.Error(), "last enabled global") {
		t.Fatalf("expected tenant admin not to satisfy last global guard, got %v", err)
	}
	if _, err := svc.UpdateAdminAccount(t.Context(), global, "default", "admin", UpdateAdminAccountInput{AdminScope: "tenant"}); err == nil || !strings.Contains(err.Error(), "last enabled global") {
		t.Fatalf("expected demoting last global admin to be rejected, got %v", err)
	}
	if _, err := svc.CreateAdminAccount(t.Context(), global, CreateAdminAccountInput{TenantID: "default", AdminScope: "global", Username: "global-two", Password: "global-two-password-123", Role: "data_admin"}); err != nil {
		t.Fatalf("CreateAdminAccount second global admin: %v", err)
	}
	login, err := svc.Login(t.Context(), LoginInput{TenantID: "tenant-a", Username: "admin", Password: "change-me-123"})
	if err != nil {
		t.Fatalf("global admin cross-tenant login: %v", err)
	}
	if login.TenantID != "tenant-a" || login.AdminScope != "global" {
		t.Fatalf("unexpected cross-tenant global login: %#v", login)
	}
	if _, err := svc.UpdateAdminAccount(t.Context(), global, "default", "admin", UpdateAdminAccountInput{AdminScope: "tenant"}); err != nil {
		t.Fatalf("demote original global admin after second global exists: %v", err)
	}
	if _, err := svc.FindAdminSessionBySecret(t.Context(), login.Token); err == nil {
		t.Fatal("demoting a global admin should revoke cross-tenant global sessions")
	}
}

func TestDefaultTenantScopedAdminCannotFallbackLoginOtherTenant(t *testing.T) {
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
	global := Principal{TenantID: initResult.TenantID, UserID: "admin", Role: "data_admin", AdminScope: "global"}
	if _, err := svc.CreateAdminAccount(t.Context(), global, CreateAdminAccountInput{TenantID: "default", AdminScope: "tenant", Username: "local-admin", Password: "local-admin-password-123", Role: "data_admin"}); err != nil {
		t.Fatalf("CreateAdminAccount default tenant admin: %v", err)
	}
	if _, err := svc.Login(t.Context(), LoginInput{TenantID: "tenant-a", Username: "admin", Password: "change-me-123"}); err != nil {
		t.Fatalf("global admin should fallback across tenants: %v", err)
	}
	if _, err := svc.Login(t.Context(), LoginInput{TenantID: "tenant-a", Username: "local-admin", Password: "local-admin-password-123"}); err == nil {
		t.Fatal("default tenant-scoped admin should not fallback-login into another tenant")
	}
	login, err := svc.Login(t.Context(), LoginInput{TenantID: "default", Username: "local-admin", Password: "local-admin-password-123"})
	if err != nil {
		t.Fatalf("default tenant-scoped admin should login to default tenant: %v", err)
	}
	if login.TenantID != "default" || login.AdminScope != "tenant" {
		t.Fatalf("unexpected default tenant login: %#v", login)
	}
}

func TestGlobalAdminLoginFallbackFindsNonDefaultGlobalAdmin(t *testing.T) {
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
	global := Principal{TenantID: initResult.TenantID, UserID: "admin", Role: "data_admin", AdminScope: "global"}
	if _, err := svc.CreateAdminAccount(t.Context(), global, CreateAdminAccountInput{TenantID: "tenant-a", AdminScope: "global", Username: "global-a", Password: "global-a-password-123", Role: "data_admin"}); err != nil {
		t.Fatalf("CreateAdminAccount non-default global admin: %v", err)
	}
	login, err := svc.Login(t.Context(), LoginInput{TenantID: "tenant-b", Username: "global-a", Password: "global-a-password-123"})
	if err != nil {
		t.Fatalf("non-default global admin should login across tenants: %v", err)
	}
	if login.TenantID != "tenant-b" || login.AdminScope != "global" {
		t.Fatalf("unexpected non-default global login: %#v", login)
	}
}

func TestGlobalAdminLoginFallbackSurvivesLegacyTenantUsernameShadow(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	initResult, err := svc.InitializeAdmin(t.Context(), InitializeAdminInput{Username: "admin", Password: "global-password-123"})
	if err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	shadowHash, err := bcrypt.GenerateFromPassword([]byte("tenant-password-123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword: %v", err)
	}
	now := time.Now().UTC()
	if _, err := store.CreateAdminUser(t.Context(), adminUserRecord{ID: "admin_shadow", TenantID: "tenant-a", Username: initResult.Username, DisplayName: "Shadow", Role: "data_admin", AdminScope: "tenant", Enabled: true, PasswordHash: string(shadowHash), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateAdminUser shadow: %v", err)
	}

	login, err := svc.Login(t.Context(), LoginInput{TenantID: "tenant-a", Username: "admin", Password: "global-password-123"})
	if err != nil {
		t.Fatalf("global admin should login even when legacy tenant admin shadows username: %v", err)
	}
	if login.TenantID != "tenant-a" || login.AdminScope != "global" {
		t.Fatalf("unexpected shadowed global login: %#v", login)
	}
	tenantLogin, err := svc.Login(t.Context(), LoginInput{TenantID: "tenant-a", Username: "admin", Password: "tenant-password-123"})
	if err != nil {
		t.Fatalf("legacy tenant shadow admin should still login with its own password: %v", err)
	}
	if tenantLogin.TenantID != "tenant-a" || tenantLogin.AdminScope != "tenant" {
		t.Fatalf("unexpected shadow tenant login: %#v", tenantLogin)
	}
}

func TestGlobalAdminUsernameMustBeUniqueAcrossTenants(t *testing.T) {
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
	global := Principal{TenantID: initResult.TenantID, UserID: "admin", Role: "data_admin", AdminScope: "global"}
	if _, err := svc.CreateAdminAccount(t.Context(), global, CreateAdminAccountInput{TenantID: "tenant-a", AdminScope: "tenant", Username: "shared-admin", Password: "shared-admin-password-123", Role: "data_admin"}); err != nil {
		t.Fatalf("CreateAdminAccount tenant admin: %v", err)
	}
	if _, err := svc.CreateAdminAccount(t.Context(), global, CreateAdminAccountInput{TenantID: "tenant-b", AdminScope: "global", Username: "shared-admin", Password: "global-password-123", Role: "data_admin"}); err == nil || !strings.Contains(err.Error(), "unique across tenants") {
		t.Fatalf("global admin username collision should be rejected, got %v", err)
	}
	if _, err := svc.UpdateAdminAccount(t.Context(), global, "tenant-a", "shared-admin", UpdateAdminAccountInput{AdminScope: "global"}); err != nil {
		t.Fatalf("promoting same account to global should be allowed: %v", err)
	}
	if _, err := svc.CreateAdminAccount(t.Context(), global, CreateAdminAccountInput{TenantID: "tenant-c", AdminScope: "tenant", Username: "shared-admin", Password: "tenant-password-123", Role: "data_admin"}); err == nil || !strings.Contains(err.Error(), "shadow") {
		t.Fatalf("tenant admin username should not shadow global admin, got %v", err)
	}
}

func TestGlobalAdminListsAllTenantAdministratorsByDefault(t *testing.T) {
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
	global := Principal{TenantID: initResult.TenantID, UserID: "admin", Role: "data_admin", AdminScope: "global"}
	if _, err := svc.CreateAdminAccount(t.Context(), global, CreateAdminAccountInput{TenantID: "tenant-a", AdminScope: "tenant", Username: "tenant-admin", Password: "tenant-password-123", Role: "data_admin"}); err != nil {
		t.Fatalf("CreateAdminAccount tenant admin: %v", err)
	}
	allAccounts, err := svc.ListAdminAccountsForPrincipal(t.Context(), global, "")
	if err != nil {
		t.Fatalf("ListAdminAccountsForPrincipal global default: %v", err)
	}
	if len(allAccounts.Items) != 2 {
		t.Fatalf("global admin default list should include all tenants: %#v", allAccounts)
	}
	tenantAdmin := Principal{TenantID: "tenant-a", UserID: "tenant-admin", Role: "data_admin", AdminScope: "tenant"}
	tenantAccounts, err := svc.ListAdminAccountsForPrincipal(t.Context(), tenantAdmin, "")
	if err != nil {
		t.Fatalf("ListAdminAccountsForPrincipal tenant default: %v", err)
	}
	if len(tenantAccounts.Items) != 1 || tenantAccounts.Items[0].TenantID != "tenant-a" {
		t.Fatalf("tenant admin default list should stay tenant-scoped: %#v", tenantAccounts)
	}
	if _, err := svc.ListAdminAccountsForPrincipal(t.Context(), tenantAdmin, "all"); err == nil {
		t.Fatal("tenant admin should not list all tenants")
	}
	if _, err := svc.Login(t.Context(), LoginInput{TenantID: "tenant-a", Username: "tenant-admin", Password: "tenant-password-123"}); err != nil {
		t.Fatalf("tenant admin login: %v", err)
	}
	allSessions, err := svc.ListAdminSessionsForPrincipal(t.Context(), global, "")
	if err != nil {
		t.Fatalf("ListAdminSessionsForPrincipal global default: %v", err)
	}
	if len(allSessions.Items) != 2 {
		t.Fatalf("global admin default sessions should include all tenants: %#v", allSessions)
	}
	tenantSessions, err := svc.ListAdminSessionsForPrincipal(t.Context(), tenantAdmin, "")
	if err != nil {
		t.Fatalf("ListAdminSessionsForPrincipal tenant default: %v", err)
	}
	if len(tenantSessions.Items) != 1 || tenantSessions.Items[0].TenantID != "tenant-a" {
		t.Fatalf("tenant admin default sessions should stay tenant-scoped: %#v", tenantSessions)
	}
}

func TestGlobalAdminCanMutateSessionsUsingAllTenantSelector(t *testing.T) {
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
	global := Principal{TenantID: initResult.TenantID, UserID: "admin", Role: "data_admin", AdminScope: "global"}
	if _, err := svc.CreateAdminAccount(t.Context(), global, CreateAdminAccountInput{TenantID: "tenant-a", AdminScope: "tenant", Username: "tenant-admin", Password: "tenant-password-123", Role: "data_admin"}); err != nil {
		t.Fatalf("CreateAdminAccount tenant admin: %v", err)
	}
	tenantLogin, err := svc.Login(t.Context(), LoginInput{TenantID: "tenant-a", Username: "tenant-admin", Password: "tenant-password-123"})
	if err != nil {
		t.Fatalf("tenant admin login: %v", err)
	}
	sessions, err := svc.ListAdminSessionsForPrincipal(t.Context(), global, "all")
	if err != nil {
		t.Fatalf("ListAdminSessionsForPrincipal all: %v", err)
	}
	var tenantSessionID string
	for _, item := range sessions.Items {
		if item.TenantID == "tenant-a" && item.Username == "tenant-admin" {
			tenantSessionID = item.ID
		}
	}
	if tenantSessionID == "" {
		t.Fatalf("tenant session missing from all sessions: %#v", sessions)
	}
	updated, err := svc.UpdateAdminSession(t.Context(), global, "all", tenantSessionID, UpdateAdminSessionInput{ExpiresHours: 1})
	if err != nil {
		t.Fatalf("UpdateAdminSession tenant=all: %v", err)
	}
	if updated.Session.TenantID != "tenant-a" || updated.Session.ID != tenantSessionID {
		t.Fatalf("unexpected updated session: %#v", updated.Session)
	}
	if _, err := svc.RevokeAdminSession(t.Context(), global, "all", tenantSessionID); err != nil {
		t.Fatalf("RevokeAdminSession tenant=all: %v", err)
	}
	if _, err := svc.FindAdminSessionBySecret(t.Context(), tenantLogin.Token); err == nil {
		t.Fatal("revoked tenant session should no longer authenticate")
	}
}

func TestGlobalAdminHTTPManagesCrossTenantAdministrators(t *testing.T) {
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

	req = jsonRequest(http.MethodPost, "/api/v1/data/admin/accounts", CreateAdminAccountInput{TenantID: "tenant-a", AdminScope: "tenant", Username: "tenant-admin", Password: "tenant-password-123", Role: "data_admin"})
	req.Header.Set("Authorization", "Bearer "+initResult.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create tenant admin status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/admin/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+initResult.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("global list accounts status=%d body=%s", w.Code, w.Body.String())
	}
	var accounts ListAdminAccountsResult
	if err := json.NewDecoder(w.Body).Decode(&accounts); err != nil {
		t.Fatalf("decode accounts: %v", err)
	}
	if len(accounts.Items) != 2 {
		t.Fatalf("global HTTP account list should include all tenants: %#v", accounts)
	}

	req = jsonRequest(http.MethodPost, "/api/v1/login", LoginInput{TenantID: "tenant-a", Username: "tenant-admin", Password: "tenant-password-123"})
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tenant admin login status=%d body=%s", w.Code, w.Body.String())
	}
	var tenantLogin LoginResult
	if err := json.NewDecoder(w.Body).Decode(&tenantLogin); err != nil {
		t.Fatalf("decode tenant login: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/admin/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+tenantLogin.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tenant list accounts status=%d body=%s", w.Code, w.Body.String())
	}
	accounts = ListAdminAccountsResult{}
	if err := json.NewDecoder(w.Body).Decode(&accounts); err != nil {
		t.Fatalf("decode tenant accounts: %v", err)
	}
	if len(accounts.Items) != 1 || accounts.Items[0].TenantID != "tenant-a" {
		t.Fatalf("tenant HTTP account list should stay tenant-scoped: %#v", accounts)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/admin/accounts?tenant=all", nil)
	req.Header.Set("Authorization", "Bearer "+tenantLogin.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("tenant admin tenant=all should be forbidden status=%d body=%s", w.Code, w.Body.String())
	}

	displayName := "Tenant Operator"
	req = jsonRequest(http.MethodPatch, "/api/v1/data/admin/accounts/tenant-admin?tenant=tenant-a", UpdateAdminAccountInput{DisplayName: &displayName})
	req.Header.Set("Authorization", "Bearer "+initResult.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("global update tenant admin status=%d body=%s", w.Code, w.Body.String())
	}
	var updated AdminAccountResult
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated tenant admin: %v", err)
	}
	if updated.Account.TenantID != "tenant-a" || updated.Account.DisplayName != "Tenant Operator" {
		t.Fatalf("unexpected updated tenant admin: %#v", updated)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/admin/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+initResult.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("global list sessions status=%d body=%s", w.Code, w.Body.String())
	}
	var sessions ListAdminSessionsResult
	if err := json.NewDecoder(w.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	var tenantSessionID string
	for _, item := range sessions.Items {
		if item.TenantID == "tenant-a" && item.Username == "tenant-admin" {
			tenantSessionID = item.ID
		}
	}
	if tenantSessionID == "" {
		t.Fatalf("global HTTP sessions should include tenant session: %#v", sessions)
	}

	req = jsonRequest(http.MethodPatch, "/api/v1/data/admin/sessions/"+tenantSessionID+"?tenant=tenant-a", UpdateAdminSessionInput{ExpiresHours: 1})
	req.Header.Set("Authorization", "Bearer "+initResult.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("global update tenant session status=%d body=%s", w.Code, w.Body.String())
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
	p := Principal{TenantID: initResult.TenantID, UserID: "admin", Role: "data_admin", AdminScope: "global"}
	if _, err := svc.CreateAdminAccount(t.Context(), p, CreateAdminAccountInput{Username: "analyst", Password: "analyst-password-123", Role: "data_user"}); err != nil {
		t.Fatalf("CreateAdminAccount data_user: %v", err)
	}
	if _, err := svc.CreateAdminAccount(t.Context(), p, CreateAdminAccountInput{Username: "owner", Password: "owner-password-123", Role: "owner"}); err == nil {
		t.Fatal("invalid explicit admin role should be rejected instead of defaulting to data_admin")
	}
	disabled := false
	if _, err := svc.UpdateAdminAccount(t.Context(), p, "default", "admin", UpdateAdminAccountInput{Enabled: &disabled}); err == nil {
		t.Fatal("disabling the only enabled global data_admin should be rejected even when another enabled data_user exists")
	}
	if _, err := svc.UpdateAdminAccount(t.Context(), p, "default", "admin", UpdateAdminAccountInput{Role: "data_user"}); err == nil {
		t.Fatal("demoting the only enabled global data_admin should be rejected")
	}
	if _, err := svc.CreateAdminAccount(t.Context(), p, CreateAdminAccountInput{Username: "ops-admin", Password: "ops-password-123", Role: "data_admin", AdminScope: "global"}); err != nil {
		t.Fatalf("CreateAdminAccount second global data_admin: %v", err)
	}
	if _, err := svc.UpdateAdminAccount(t.Context(), p, "default", "admin", UpdateAdminAccountInput{Role: "data_user"}); err != nil {
		t.Fatalf("demoting one global data_admin should work when another enabled global data_admin remains: %v", err)
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

func TestAdminScopesAndHubTenantSync(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	initResult, err := svc.InitializeAdmin(t.Context(), InitializeAdminInput{Username: "root", Password: "change-me-123"})
	if err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	if initResult.AdminScope != "global" {
		t.Fatalf("first admin scope=%q, want global", initResult.AdminScope)
	}
	global := Principal{TenantID: initResult.TenantID, UserID: "root", Role: "data_admin", AdminScope: "global", Policy: &APIKeyPolicy{AllowAdmin: true}}
	synced, err := svc.SyncHubTenants(t.Context(), global, SyncHubTenantsInput{Tenants: []DataTenantInfo{{ID: "tenant_a", Name: "Tenant A", Status: "active", PrimaryDomain: "Acme.Example"}}})
	if err != nil {
		t.Fatalf("SyncHubTenants: %v", err)
	}
	if synced.Synced != 1 || synced.Tenants[0].PrimaryDomain != "acme.example" {
		t.Fatalf("unexpected tenant sync result: %#v", synced)
	}
	if _, err := svc.CreateAdminAccount(t.Context(), global, CreateAdminAccountInput{TenantID: "tenant_a", AdminScope: "tenant", Username: "tenant-admin", Password: "change-me-123", Role: "data_admin"}); err != nil {
		t.Fatalf("Create tenant admin: %v", err)
	}
	login, err := svc.Login(t.Context(), LoginInput{TenantID: "tenant_a", Username: "tenant-admin", Password: "change-me-123"})
	if err != nil {
		t.Fatalf("tenant admin login: %v", err)
	}
	if login.AdminScope != "tenant" || login.TenantID != "tenant_a" {
		t.Fatalf("unexpected tenant admin login: %#v", login)
	}
	tenantAdmin := Principal{TenantID: "tenant_a", UserID: "tenant-admin", Role: "data_admin", AdminScope: "tenant", Policy: &APIKeyPolicy{AllowAdmin: true}}
	if _, err := svc.CreateAdminAccount(t.Context(), tenantAdmin, CreateAdminAccountInput{TenantID: "tenant_b", Username: "bad", Password: "change-me-123", Role: "data_admin"}); err == nil {
		t.Fatal("tenant admin should not create admins outside own tenant")
	}
	if _, err := svc.SyncHubTenants(t.Context(), tenantAdmin, SyncHubTenantsInput{Tenants: []DataTenantInfo{{ID: "tenant_b"}}}); err == nil {
		t.Fatal("tenant admin should not sync Hub tenants")
	}
	items, err := svc.ListDataTenants(t.Context(), tenantAdmin)
	if err != nil {
		t.Fatalf("ListDataTenants tenant admin: %v", err)
	}
	if len(items) != 1 || items[0].ID != "tenant_a" {
		t.Fatalf("tenant admin should see only own tenant, got %#v", items)
	}
}
