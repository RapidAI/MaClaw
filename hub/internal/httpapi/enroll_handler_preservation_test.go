package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

// **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5**
//
// Preservation tests: capture the CURRENT behavior of non-buggy code paths
// (no feishu auto-enroll) so we can verify no regressions after the fix.

// newPreservationTestIdentity creates a real IdentityService backed by an
// in-memory SQLite DB for preservation testing.
func newPreservationTestIdentity(t *testing.T) (*auth.IdentityService, *store.Store, *sqlite.Provider) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "preservation-test.db")
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               dbPath,
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	st := sqlite.NewStore(provider)
	identity := auth.NewIdentityService(
		st.Users, st.Enrollments, st.EmailBlocks, st.Machines,
		st.ViewerTokens, st.LoginTokens, st.System,
		nil,    // no invitation code validator
		"open", // enrollment mode
		true,   // allow self-enroll
		nil,    // no mailer
		"http://127.0.0.1:9399",
	)
	return identity, st, provider
}

// stubInvitationValidator implements auth.InvitationCodeValidator for testing
// invitation-code-related error paths.
type stubInvitationValidator struct {
	required   bool
	consumeErr error
}

func (s *stubInvitationValidator) IsRequired(_ context.Context) (bool, error) {
	return s.required, nil
}

func (s *stubInvitationValidator) IsRequiredForTenant(_ context.Context, _ string) (bool, error) {
	return s.required, nil
}

func (s *stubInvitationValidator) ValidateAndConsume(_ context.Context, code string, email string) error {
	if s.consumeErr != nil {
		return s.consumeErr
	}
	return nil
}

func (s *stubInvitationValidator) ValidateAndConsumeForTenant(ctx context.Context, tenantID string, code string, email string) error {
	return s.ValidateAndConsume(ctx, code, email)
}

func (s *stubInvitationValidator) CheckExpiry(_ context.Context, email string) (bool, *time.Time, error) {
	return false, nil, nil
}

func (s *stubInvitationValidator) CheckExpiryForTenant(ctx context.Context, tenantID string, email string) (bool, *time.Time, error) {
	return s.CheckExpiry(ctx, email)
}

type countingInvitationRepo struct {
	mu              sync.Mutex
	item            *store.InvitationCode
	getByEmailCalls int
}

func (r *countingInvitationRepo) Create(_ context.Context, item *store.InvitationCode) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.item = item
	return nil
}

func (r *countingInvitationRepo) GetByID(_ context.Context, id string) (*store.InvitationCode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.item != nil && r.item.ID == id {
		copied := *r.item
		return &copied, nil
	}
	return nil, nil
}

func (r *countingInvitationRepo) GetByCode(_ context.Context, code string) (*store.InvitationCode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.item != nil && r.item.Code == code {
		copied := *r.item
		return &copied, nil
	}
	return nil, nil
}

func (r *countingInvitationRepo) GetByTenantCode(ctx context.Context, tenantID, code string) (*store.InvitationCode, error) {
	return r.GetByCode(ctx, code)
}

func (r *countingInvitationRepo) GetByEmail(_ context.Context, email string) (*store.InvitationCode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.getByEmailCalls++
	if r.item != nil && r.item.UsedByEmail == email {
		copied := *r.item
		return &copied, nil
	}
	return nil, nil
}

func (r *countingInvitationRepo) GetByTenantEmail(ctx context.Context, tenantID, email string) (*store.InvitationCode, error) {
	return r.GetByEmail(ctx, email)
}

func (r *countingInvitationRepo) List(_ context.Context, status string, search string) ([]*store.InvitationCode, error) {
	return nil, nil
}

func (r *countingInvitationRepo) ListPaged(_ context.Context, status string, search string, offset, limit int) ([]*store.InvitationCode, int, error) {
	return nil, 0, nil
}

func (r *countingInvitationRepo) ListPagedByTenant(ctx context.Context, tenantID string, status string, search string, offset, limit int) ([]*store.InvitationCode, int, error) {
	return r.ListPaged(ctx, status, search, offset, limit)
}

func (r *countingInvitationRepo) MarkUsed(_ context.Context, id string, email string, usedAt time.Time) error {
	return nil
}

func (r *countingInvitationRepo) Unbind(_ context.Context, id string) error {
	return nil
}

func (r *countingInvitationRepo) DeleteByID(_ context.Context, id string) error {
	return nil
}

func (r *countingInvitationRepo) DeleteByEmail(_ context.Context, email string) (int64, error) {
	return 0, nil
}

func (r *countingInvitationRepo) DeleteByTenantEmail(ctx context.Context, tenantID, email string) (int64, error) {
	return r.DeleteByEmail(ctx, email)
}

func (r *countingInvitationRepo) ListUnused(_ context.Context, exportedFilter string, vipOnly ...bool) ([]*store.InvitationCode, error) {
	return nil, nil
}

func (r *countingInvitationRepo) ListUnusedByTenant(ctx context.Context, tenantID, exportedFilter string, vipOnly ...bool) ([]*store.InvitationCode, error) {
	return r.ListUnused(ctx, exportedFilter, vipOnly...)
}

func (r *countingInvitationRepo) MarkExported(_ context.Context, ids []string) error {
	return nil
}

func (r *countingInvitationRepo) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.getByEmailCalls
}

// TestEnrollStartHandler_Preservation_TableDriven verifies that the
// EnrollStartHandler produces the expected HTTP status codes and response
// shapes for all non-feishu code paths. These tests MUST PASS on unfixed code.
func TestEnrollStartHandler_Preservation_TableDriven(t *testing.T) {
	tests := []struct {
		name            string
		body            string
		setupBlockEmail string // if non-empty, block this email before the request
		invValidator    auth.InvitationCodeValidator
		wantHTTPStatus  int
		wantOK          *bool  // nil = don't check "ok" field
		wantCode        string // expected "code" field in error responses
		wantStatus      string // expected "status" field in success responses
	}{
		{
			name:           "normal enrollment - email valid, no feishu",
			body:           `{"email":"alice@example.com","machine_name":"test","platform":"darwin","client_id":"c1"}`,
			wantHTTPStatus: http.StatusOK,
			wantStatus:     "approved",
		},
		{
			name:           "empty body / invalid JSON",
			body:           `{invalid json`,
			wantHTTPStatus: http.StatusBadRequest,
			wantOK:         boolPtr(false),
			wantCode:       "INVALID_JSON",
		},
		{
			name:           "missing email",
			body:           `{"machine_name":"test","platform":"darwin","client_id":"c1"}`,
			wantHTTPStatus: http.StatusBadRequest,
			wantOK:         boolPtr(false),
			wantCode:       "INVALID_INPUT",
		},
		{
			name:           "empty email",
			body:           `{"email":"","machine_name":"test","platform":"darwin","client_id":"c1"}`,
			wantHTTPStatus: http.StatusBadRequest,
			wantOK:         boolPtr(false),
			wantCode:       "INVALID_INPUT",
		},
		{
			name:            "blocked email",
			body:            `{"email":"blocked@example.com","machine_name":"test","platform":"darwin","client_id":"c1"}`,
			setupBlockEmail: "blocked@example.com",
			wantHTTPStatus:  http.StatusForbidden,
			wantOK:          boolPtr(false),
			wantCode:        "EMAIL_BLOCKED",
		},
		{
			name: "invitation code required but not provided",
			body: `{"email":"newuser@example.com","machine_name":"test","platform":"darwin","client_id":"c2"}`,
			invValidator: &stubInvitationValidator{
				required: true,
			},
			wantHTTPStatus: http.StatusBadRequest,
			wantOK:         boolPtr(false),
			wantCode:       "INVITATION_CODE_REQUIRED",
		},
		{
			name: "invalid invitation code",
			body: `{"email":"newuser2@example.com","machine_name":"test","platform":"darwin","client_id":"c3","invitation_code":"BADCODE"}`,
			invValidator: &stubInvitationValidator{
				required:   true,
				consumeErr: auth.ErrInvalidInvitationCode,
			},
			wantHTTPStatus: http.StatusBadRequest,
			wantOK:         boolPtr(false),
			wantCode:       "INVALID_INVITATION_CODE",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			identity, st, _ := newPreservationTestIdentity(t)

			// If this test case needs a custom invitation validator, rebuild identity.
			if tc.invValidator != nil {
				identity = auth.NewIdentityService(
					st.Users, st.Enrollments, st.EmailBlocks, st.Machines,
					st.ViewerTokens, st.LoginTokens, st.System,
					tc.invValidator,
					"open",
					true,
					nil,
					"http://127.0.0.1:9399",
				)
			}

			// Block email if needed.
			if tc.setupBlockEmail != "" {
				ctx := context.Background()
				if err := identity.AddBlockedEmail(ctx, tc.setupBlockEmail, "test block"); err != nil {
					t.Fatalf("failed to block email: %v", err)
				}
			}

			handler := EnrollStartHandler(identity, nil, nil)

			req := httptest.NewRequest(http.MethodPost, "/api/enroll/start", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			// Check HTTP status code.
			if rr.Code != tc.wantHTTPStatus {
				t.Fatalf("HTTP status: got %d, want %d; body=%s", rr.Code, tc.wantHTTPStatus, rr.Body.String())
			}

			// Parse response JSON.
			var result map[string]any
			if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
				t.Fatalf("failed to parse response JSON: %v; body=%s", err, rr.Body.String())
			}

			// Check "ok" field for error responses.
			if tc.wantOK != nil {
				okVal, exists := result["ok"]
				if !exists {
					t.Fatalf("expected 'ok' field in response, got: %s", rr.Body.String())
				}
				if okBool, isBool := okVal.(bool); isBool {
					if okBool != *tc.wantOK {
						t.Fatalf("ok: got %v, want %v", okBool, *tc.wantOK)
					}
				}
			}

			// Check "code" field for error responses.
			if tc.wantCode != "" {
				codeVal, exists := result["code"]
				if !exists {
					t.Fatalf("expected 'code' field in response, got: %s", rr.Body.String())
				}
				if codeStr, isStr := codeVal.(string); isStr {
					if codeStr != tc.wantCode {
						t.Fatalf("code: got %q, want %q", codeStr, tc.wantCode)
					}
				}
			}

			// Check "status" field for success responses.
			if tc.wantStatus != "" {
				statusVal, exists := result["status"]
				if !exists {
					t.Fatalf("expected 'status' field in response, got: %s", rr.Body.String())
				}
				if statusStr, isStr := statusVal.(string); isStr {
					if statusStr != tc.wantStatus {
						t.Fatalf("status: got %q, want %q", statusStr, tc.wantStatus)
					}
				}
			}
		})
	}
}

// TestEnrollStartHandler_Preservation_SuccessResponseFields verifies that a
// successful enrollment (no feishu) returns all expected fields.
func TestEnrollStartHandler_Preservation_SuccessResponseFields(t *testing.T) {
	identity, _, _ := newPreservationTestIdentity(t)
	handler := EnrollStartHandler(identity, nil, nil)

	body := `{"email":"fields-test@example.com","machine_name":"my-mac","platform":"darwin","client_id":"cid-001"}`
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/start", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("HTTP status: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var result map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Verify expected fields exist in a successful enrollment response.
	requiredFields := []string{"status", "user_id", "email", "sn", "machine_id", "machine_token"}
	for _, field := range requiredFields {
		if _, ok := result[field]; !ok {
			t.Errorf("missing expected field %q in response: %s", field, rr.Body.String())
		}
	}

	if result["status"] != "approved" {
		t.Errorf("status: got %q, want %q", result["status"], "approved")
	}
	if result["email"] != "fields-test@example.com" {
		t.Errorf("email: got %q, want %q", result["email"], "fields-test@example.com")
	}
}

func TestEnrollStartAutoResolvesUniqueTenantByEmail(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	ctx := context.Background()
	user, err := identity.ManualBindForTenant(auth.WithTenant(ctx, "tenant_acme"), "tenant_acme", "device@example.com")
	if err != nil {
		t.Fatalf("manual bind tenant user: %v", err)
	}

	handler := EnrollStartHandler(identity, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/start", strings.NewReader(`{"email":"device@example.com","machine_name":"desktop","platform":"windows","client_id":"legacy-client"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("enroll status=%d body=%s", rr.Code, rr.Body.String())
	}
	var result struct {
		TenantID  string `json:"tenant_id"`
		MachineID string `json:"machine_id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.TenantID != "tenant_acme" || result.MachineID == "" {
		t.Fatalf("expected tenant_acme enrollment response, got %s", rr.Body.String())
	}
	machine, err := st.Machines.GetByID(ctx, result.MachineID)
	if err != nil {
		t.Fatalf("load machine: %v", err)
	}
	if machine == nil || machine.TenantID != "tenant_acme" || machine.UserID != user.ID {
		t.Fatalf("expected tenant_acme machine for bound user, got %+v", machine)
	}
	defaultMachines, err := st.Machines.ListByTenant(ctx, store.DefaultTenantID)
	if err != nil {
		t.Fatalf("list default machines: %v", err)
	}
	if len(defaultMachines) != 0 {
		t.Fatalf("default tenant should not receive machine, got %+v", defaultMachines)
	}
}

func TestEnrollStartRejectsAmbiguousTenantEmailWithoutHint(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	ctx := context.Background()
	for _, tenantID := range []string{"tenant_acme", "tenant_beta"} {
		time.Sleep(time.Nanosecond)
		if _, err := identity.ManualBindForTenant(auth.WithTenant(ctx, tenantID), tenantID, "ambiguous-device@example.com"); err != nil {
			t.Fatalf("manual bind %s: %v", tenantID, err)
		}
	}

	handler := EnrollStartHandler(identity, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/start", strings.NewReader(`{"email":"ambiguous-device@example.com","machine_name":"desktop","platform":"windows","client_id":"legacy-client"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "TENANT_AMBIGUOUS") {
		t.Fatalf("expected tenant ambiguous response, status=%d body=%s", rr.Code, rr.Body.String())
	}
	for _, tenantID := range []string{"tenant_acme", "tenant_beta", store.DefaultTenantID} {
		machines, err := st.Machines.ListByTenant(ctx, tenantID)
		if err != nil {
			t.Fatalf("list machines for %s: %v", tenantID, err)
		}
		if len(machines) != 0 {
			t.Fatalf("ambiguous request should not create machine for %s, got %+v", tenantID, machines)
		}
	}

	req = httptest.NewRequest(http.MethodPost, "/api/enroll/start?tenant_id=tenant_beta", strings.NewReader(`{"email":"ambiguous-device@example.com","machine_name":"desktop","platform":"windows","client_id":"legacy-client"}`))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("tenant hinted enroll status=%d body=%s", rr.Code, rr.Body.String())
	}
	var hinted struct {
		TenantID string `json:"tenant_id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &hinted); err != nil {
		t.Fatalf("decode hinted response: %v", err)
	}
	if hinted.TenantID != "tenant_beta" {
		t.Fatalf("expected hinted tenant_beta response, got %s", rr.Body.String())
	}
	machines, err := st.Machines.ListByTenant(ctx, "tenant_beta")
	if err != nil {
		t.Fatalf("list tenant_beta machines: %v", err)
	}
	if len(machines) != 1 {
		t.Fatalf("expected tenant_beta machine after hint, got %+v", machines)
	}
	machines, err = st.Machines.ListByTenant(ctx, "tenant_acme")
	if err != nil {
		t.Fatalf("list tenant_acme machines: %v", err)
	}
	if len(machines) != 0 {
		t.Fatalf("tenant hint should not create tenant_acme machine, got %+v", machines)
	}
}

func TestEmailRequestLoginAutoResolvesUniqueTenantByEmail(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	ctx := context.Background()
	if _, err := identity.ManualBindForTenant(auth.WithTenant(ctx, "tenant_acme"), "tenant_acme", "legacy@example.com"); err != nil {
		t.Fatalf("manual bind tenant user: %v", err)
	}

	handler := EmailRequestLoginHandler(identity)
	req := httptest.NewRequest(http.MethodPost, "/api/email/request-login", strings.NewReader(`{"email":"legacy@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("request login status=%d body=%s", rr.Code, rr.Body.String())
	}
	if token, err := st.LoginTokens.GetPendingByTenantEmail(ctx, "tenant_acme", "legacy@example.com"); err != nil || token == nil {
		t.Fatalf("expected tenant_acme pending token, token=%+v err=%v", token, err)
	}
	var result struct {
		TenantID string `json:"tenant_id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.TenantID != "tenant_acme" {
		t.Fatalf("expected resolved tenant_id in login response, got %q body=%s", result.TenantID, rr.Body.String())
	}
	if token, err := st.LoginTokens.GetPendingByTenantEmail(ctx, store.DefaultTenantID, "legacy@example.com"); err != nil || token != nil {
		t.Fatalf("default tenant should not receive token, token=%+v err=%v", token, err)
	}
}

func TestEmailRequestLoginRejectsAmbiguousTenantEmailWithoutHint(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	ctx := context.Background()
	for _, tenantID := range []string{"tenant_acme", "tenant_beta"} {
		time.Sleep(time.Nanosecond)
		if _, err := identity.ManualBindForTenant(auth.WithTenant(ctx, tenantID), tenantID, "shared@example.com"); err != nil {
			t.Fatalf("manual bind %s: %v", tenantID, err)
		}
	}

	handler := EmailRequestLoginHandler(identity)
	req := httptest.NewRequest(http.MethodPost, "/api/email/request-login", strings.NewReader(`{"email":"shared@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "TENANT_AMBIGUOUS") {
		t.Fatalf("expected tenant ambiguous response, status=%d body=%s", rr.Code, rr.Body.String())
	}
	for _, tenantID := range []string{"tenant_acme", "tenant_beta", store.DefaultTenantID} {
		if token, err := st.LoginTokens.GetPendingByTenantEmail(ctx, tenantID, "shared@example.com"); err != nil || token != nil {
			t.Fatalf("ambiguous request should not create token for %s, token=%+v err=%v", tenantID, token, err)
		}
	}

	req = httptest.NewRequest(http.MethodPost, "/api/email/request-login?tenant_id=tenant_beta", strings.NewReader(`{"email":"shared@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("tenant hinted request status=%d body=%s", rr.Code, rr.Body.String())
	}
	if token, err := st.LoginTokens.GetPendingByTenantEmail(ctx, "tenant_beta", "shared@example.com"); err != nil || token == nil {
		t.Fatalf("expected tenant_beta token after hint, token=%+v err=%v", token, err)
	}
	var hintedResult struct {
		TenantID string `json:"tenant_id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &hintedResult); err != nil {
		t.Fatalf("decode hinted response: %v", err)
	}
	if hintedResult.TenantID != "tenant_beta" {
		t.Fatalf("expected hinted tenant_id in login response, got %q body=%s", hintedResult.TenantID, rr.Body.String())
	}
	if token, err := st.LoginTokens.GetPendingByTenantEmail(ctx, "tenant_acme", "shared@example.com"); err != nil || token != nil {
		t.Fatalf("tenant hint should not create tenant_acme token, token=%+v err=%v", token, err)
	}
}

func TestEmailConfirmAndPollResponsesIncludeTenantID(t *testing.T) {
	identity, _, _ := newPreservationTestIdentity(t)
	ctx := context.Background()
	if _, err := identity.ManualBindForTenant(auth.WithTenant(ctx, "tenant_acme"), "tenant_acme", "confirm@example.com"); err != nil {
		t.Fatalf("manual bind tenant user: %v", err)
	}

	reqLogin, err := identity.RequestEmailLogin(auth.WithTenant(ctx, "tenant_acme"), "confirm@example.com")
	if err != nil {
		t.Fatalf("request login: %v", err)
	}
	if reqLogin.TenantID != "tenant_acme" {
		t.Fatalf("expected request login tenant_id, got %+v", reqLogin)
	}
	rawToken := extractDevConfirmToken(t, reqLogin.Message)

	confirmReq := httptest.NewRequest(http.MethodPost, "/api/email/confirm-login", strings.NewReader(`{"token":"`+rawToken+`"}`))
	confirmReq.Header.Set("Content-Type", "application/json")
	confirmRec := httptest.NewRecorder()
	EmailConfirmLoginHandler(identity).ServeHTTP(confirmRec, confirmReq)
	if confirmRec.Code != http.StatusOK {
		t.Fatalf("confirm status=%d body=%s", confirmRec.Code, confirmRec.Body.String())
	}
	var confirmResp struct {
		TenantID string `json:"tenant_id"`
		User     struct {
			TenantID string `json:"tenant_id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(confirmRec.Body.Bytes(), &confirmResp); err != nil {
		t.Fatalf("decode confirm response: %v", err)
	}
	if confirmResp.TenantID != "tenant_acme" || confirmResp.User.TenantID != "tenant_acme" {
		t.Fatalf("expected confirm response tenant_id, got %s", confirmRec.Body.String())
	}

	reqLogin, err = identity.RequestEmailLogin(auth.WithTenant(ctx, "tenant_acme"), "confirm@example.com")
	if err != nil {
		t.Fatalf("request login for poll: %v", err)
	}
	rawToken = extractDevConfirmToken(t, reqLogin.Message)
	if _, _, err := identity.ConfirmEmailLogin(ctx, rawToken); err != nil {
		t.Fatalf("confirm for poll: %v", err)
	}
	pollReq := httptest.NewRequest(http.MethodPost, "/api/email/poll-login", strings.NewReader(`{"poll_id":"`+reqLogin.PollID+`"}`))
	pollReq.Header.Set("Content-Type", "application/json")
	pollRec := httptest.NewRecorder()
	EmailPollLoginHandler(identity).ServeHTTP(pollRec, pollReq)
	if pollRec.Code != http.StatusOK {
		t.Fatalf("poll status=%d body=%s", pollRec.Code, pollRec.Body.String())
	}
	var pollResp struct {
		Status   string `json:"status"`
		TenantID string `json:"tenant_id"`
	}
	if err := json.Unmarshal(pollRec.Body.Bytes(), &pollResp); err != nil {
		t.Fatalf("decode poll response: %v", err)
	}
	if pollResp.Status != "confirmed" || pollResp.TenantID != "tenant_acme" {
		t.Fatalf("expected poll response tenant_id, got %s", pollRec.Body.String())
	}
}

func extractDevConfirmToken(t *testing.T, message string) string {
	t.Helper()
	const prefix = "Use this confirm URL for development: "
	if !strings.HasPrefix(message, prefix) {
		t.Fatalf("unexpected confirm message: %q", message)
	}
	parsed, err := url.Parse(strings.TrimPrefix(message, prefix))
	if err != nil {
		t.Fatalf("parse confirm URL: %v", err)
	}
	token := parsed.Query().Get("token")
	if token == "" {
		t.Fatalf("missing token in confirm URL: %s", message)
	}
	return token
}

func TestEnrollStartHandler_Preservation_VIPLookupUsesCache(t *testing.T) {
	identity, _, _ := newPreservationTestIdentity(t)
	repo := &countingInvitationRepo{
		item: &store.InvitationCode{
			ID:          "ic-1",
			Code:        "VIP-CODE",
			Status:      "used",
			UsedByEmail: "vip-cache@example.com",
			VIP:         true,
			CreatedAt:   time.Now(),
		},
	}
	invSvc := invitation.NewService(repo, nil)
	handler := EnrollStartHandler(identity, invSvc, nil)

	body := `{"email":"vip-cache@example.com","machine_name":"my-mac","platform":"darwin","client_id":"cid-vip"}`

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/enroll/start", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("HTTP status: got %d, want 200; body=%s", rr.Code, rr.Body.String())
		}

		var result map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		vipFlag, ok := result["vip_flag"].(bool)
		if !ok || !vipFlag {
			t.Fatalf("expected vip_flag=true, got %v; body=%s", result["vip_flag"], rr.Body.String())
		}
	}

	if got := repo.callCount(); got != 1 {
		t.Fatalf("expected cached VIP lookup to hit repo once, got %d", got)
	}
}

func TestEnrollStartHandler_Preservation_OrgMetadataWithoutTree(t *testing.T) {
	identity, st, provider := newPreservationTestIdentity(t)
	ctx := context.Background()
	if err := st.System.Set(ctx, "security_settings", `{"org_structure_enabled":true,"default_group_id":"group-default"}`); err != nil {
		t.Fatalf("failed to set security settings: %v", err)
	}

	securityStore := security.NewSecurityStore(provider.Write)
	handler := EnrollStartHandler(identity, nil, security.NewSecurityService(securityStore, st.System, nil))
	body := `{"email":"org-meta@example.com","machine_name":"my-mac","platform":"darwin","client_id":"cid-org"}`
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/start", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("HTTP status: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var result map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if enabled, ok := result["org_structure_enabled"].(bool); !ok || !enabled {
		t.Fatalf("expected org_structure_enabled=true, got %v", result["org_structure_enabled"])
	}
	if got := result["default_group_id"]; got != "group-default" {
		t.Fatalf("expected default_group_id=group-default, got %v", got)
	}
	if _, exists := result["org_group_tree"]; exists {
		t.Fatalf("expected org_group_tree to be absent, body=%s", rr.Body.String())
	}
}

func boolPtr(v bool) *bool { return &v }
