package entry

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

// **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5**
//
// Preservation tests for the entry.Service.ProbeByEmail method.
// These capture the CURRENT behavior so we can verify no regressions after the fix.

// newPreservationTestService creates a real entry.Service backed by an
// in-memory SQLite DB for preservation testing.
func newPreservationTestService(t *testing.T) *Service {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "entry-preservation-test.db")
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
	return NewService(identity, nil, nil)
}

// TestProbeByEmail_Preservation_EmptyEmail verifies that an empty email
// returns status "invalid_email".
func TestProbeByEmail_Preservation_EmptyEmail(t *testing.T) {
	svc := newPreservationTestService(t)
	ctx := context.Background()

	result, err := svc.ProbeByEmail(ctx, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "invalid_email" {
		t.Fatalf("status: got %q, want %q", result.Status, "invalid_email")
	}
	if result.Email != "" {
		t.Fatalf("email: got %q, want empty", result.Email)
	}
	if result.Message == "" {
		t.Fatal("expected non-empty message for invalid email")
	}
}

// TestProbeByEmail_Preservation_UnknownEmail verifies that an unknown email
// returns status "not_found" with bound=false and can_login=false.
func TestProbeByEmail_Preservation_UnknownEmail(t *testing.T) {
	svc := newPreservationTestService(t)
	ctx := context.Background()

	result, err := svc.ProbeByEmail(ctx, "unknown@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "not_found" {
		t.Fatalf("status: got %q, want %q", result.Status, "not_found")
	}
	if result.Bound != false {
		t.Fatalf("bound: got %v, want false", result.Bound)
	}
	if result.CanLogin != false {
		t.Fatalf("can_login: got %v, want false", result.CanLogin)
	}
	if result.Email != "unknown@example.com" {
		t.Fatalf("email: got %q, want %q", result.Email, "unknown@example.com")
	}
	if result.InvitationCodeRequired != false {
		t.Fatalf("invitation_code_required: got %v, want false", result.InvitationCodeRequired)
	}
}

// TestProbeByEmail_Preservation_AllFields verifies that ProbeResult contains
// all expected fields with correct values for each scenario.
func TestProbeByEmail_Preservation_AllFields(t *testing.T) {
	tests := []struct {
		name                   string
		email                  string
		wantStatus             string
		wantBound              bool
		wantCanLogin           bool
		wantInvCodeRequired    bool
	}{
		{
			name:                "empty email",
			email:               "",
			wantStatus:          "invalid_email",
			wantBound:           false,
			wantCanLogin:        false,
			wantInvCodeRequired: false,
		},
		{
			name:                "whitespace-only email",
			email:               "   ",
			wantStatus:          "invalid_email",
			wantBound:           false,
			wantCanLogin:        false,
			wantInvCodeRequired: false,
		},
		{
			name:                "unknown email",
			email:               "nobody@example.com",
			wantStatus:          "not_found",
			wantBound:           false,
			wantCanLogin:        false,
			wantInvCodeRequired: false,
		},
		{
			name:                "another unknown email",
			email:               "test123@domain.org",
			wantStatus:          "not_found",
			wantBound:           false,
			wantCanLogin:        false,
			wantInvCodeRequired: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := newPreservationTestService(t)
			ctx := context.Background()

			result, err := svc.ProbeByEmail(ctx, tc.email)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Status != tc.wantStatus {
				t.Errorf("status: got %q, want %q", result.Status, tc.wantStatus)
			}
			if result.Bound != tc.wantBound {
				t.Errorf("bound: got %v, want %v", result.Bound, tc.wantBound)
			}
			if result.CanLogin != tc.wantCanLogin {
				t.Errorf("can_login: got %v, want %v", result.CanLogin, tc.wantCanLogin)
			}
			if result.InvitationCodeRequired != tc.wantInvCodeRequired {
				t.Errorf("invitation_code_required: got %v, want %v", result.InvitationCodeRequired, tc.wantInvCodeRequired)
			}
		})
	}
}

// TestProbeByEmail_Preservation_BoundUser verifies that a bound (active) user
// returns status "bound" with bound=true and can_login=true.
func TestProbeByEmail_Preservation_BoundUser(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "entry-bound-test.db")
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
		nil, "open", true, nil, "http://127.0.0.1:9399",
	)

	// Create a bound user via ManualBind.
	ctx := context.Background()
	_, err = identity.ManualBind(ctx, "bound@example.com")
	if err != nil {
		t.Fatalf("ManualBind failed: %v", err)
	}

	svc := NewService(identity, nil, nil)
	result, err := svc.ProbeByEmail(ctx, "bound@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "bound" {
		t.Errorf("status: got %q, want %q", result.Status, "bound")
	}
	if !result.Bound {
		t.Errorf("bound: got false, want true")
	}
	if !result.CanLogin {
		t.Errorf("can_login: got false, want true")
	}
	if result.Email != "bound@example.com" {
		t.Errorf("email: got %q, want %q", result.Email, "bound@example.com")
	}
	if result.PWAURL == "" {
		t.Error("pwa_url: expected non-empty for bound user")
	}
}
