package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

// **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5**
//
// Preservation tests: capture the CURRENT behavior of non-buggy code paths
// (no feishu auto-enroll) so we can verify no regressions after the fix.

// newPreservationTestIdentity creates a real IdentityService backed by an
// in-memory SQLite DB for preservation testing.
func newPreservationTestIdentity(t *testing.T) (*auth.IdentityService, *store.Store) {
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
	return identity, st
}

// stubInvitationValidator implements auth.InvitationCodeValidator for testing
// invitation-code-related error paths.
type stubInvitationValidator struct {
	required bool
	consumeErr error
}

func (s *stubInvitationValidator) IsRequired(_ context.Context) (bool, error) {
	return s.required, nil
}

func (s *stubInvitationValidator) ValidateAndConsume(_ context.Context, code string, email string) error {
	if s.consumeErr != nil {
		return s.consumeErr
	}
	return nil
}

func (s *stubInvitationValidator) CheckExpiry(_ context.Context, email string) (bool, *time.Time, error) {
	return false, nil, nil
}

// TestEnrollStartHandler_Preservation_TableDriven verifies that the
// EnrollStartHandler produces the expected HTTP status codes and response
// shapes for all non-feishu code paths. These tests MUST PASS on unfixed code.
func TestEnrollStartHandler_Preservation_TableDriven(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		setupBlockEmail string // if non-empty, block this email before the request
		invValidator   auth.InvitationCodeValidator
		wantHTTPStatus int
		wantOK         *bool   // nil = don't check "ok" field
		wantCode       string  // expected "code" field in error responses
		wantStatus     string  // expected "status" field in success responses
	}{
		{
			name: "normal enrollment - email valid, no feishu",
			body: `{"email":"alice@example.com","machine_name":"test","platform":"darwin","client_id":"c1"}`,
			wantHTTPStatus: http.StatusOK,
			wantStatus:     "approved",
		},
		{
			name: "empty body / invalid JSON",
			body: `{invalid json`,
			wantHTTPStatus: http.StatusBadRequest,
			wantOK:         boolPtr(false),
			wantCode:       "INVALID_JSON",
		},
		{
			name: "missing email",
			body: `{"machine_name":"test","platform":"darwin","client_id":"c1"}`,
			wantHTTPStatus: http.StatusBadRequest,
			wantOK:         boolPtr(false),
			wantCode:       "INVALID_INPUT",
		},
		{
			name: "empty email",
			body: `{"email":"","machine_name":"test","platform":"darwin","client_id":"c1"}`,
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
			identity, st := newPreservationTestIdentity(t)

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

			// feishuNotifier is nil — preservation tests cover non-feishu paths.
			handler := EnrollStartHandler(identity, nil)

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
	identity, _ := newPreservationTestIdentity(t)
	handler := EnrollStartHandler(identity, nil)

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

func boolPtr(v bool) *bool { return &v }
