package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
	"github.com/go-lark/lark/v2"
)

// TestEnrollStartHandler_FeishuStatusInResponse is a bug condition exploration test.
//
// **Validates: Requirements 1.4, 1.5, 2.4**
//
// Bug Condition: EnrollStartHandler runs AddToFeishuOrg in a goroutine and
// returns the response BEFORE feishu enrollment completes. The response JSON
// does NOT contain a "feishu_status" field.
//
// Expected (correct) behavior: The response JSON MUST contain a "feishu_status"
// field (value "ok", "failed", or "disabled") so the client knows the feishu
// enrollment outcome.
//
// This test MUST FAIL on unfixed code — failure confirms the bug exists.
func TestEnrollStartHandler_FeishuStatusInResponse(t *testing.T) {
	// --- Set up a real IdentityService backed by an in-memory SQLite DB ---
	dbPath := filepath.Join(t.TempDir(), "enroll-handler-test.db")
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
		"open", // enrollment mode: open (auto-approve)
		true,   // allow self-enroll
		nil,    // no mailer
		"http://127.0.0.1:8080",
	)

	// --- Set up a feishu Notifier with an enabled AutoEnroller ---
	// The AutoEnroller's bot returns a real lark.Bot (API calls will fail,
	// but that's fine — we only care about whether the handler includes
	// feishu_status in the response).
	notifier := feishu.New("test_app_id", "test_app_secret", st.Users, st.System, nil)
	ae := feishu.NewAutoEnroller(
		func() *lark.Bot { return lark.NewChatBot("test_app_id", "test_app_secret") },
		func(email, openID string) { /* no-op binder */ },
	)
	ae.SetConfig(feishu.AutoEnrollConfig{
		Enabled:      true,
		DepartmentID: "0",
	})
	notifier.SetAutoEnroller(ae)

	// --- Build the handler and send a POST /api/enroll/start request ---
	handler := EnrollStartHandler(identity, notifier)

	body := `{
		"email": "feishu-test@example.com",
		"mobile": "+8613800138000",
		"machine_name": "test-machine",
		"platform": "darwin",
		"client_id": "test-client-001"
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/enroll/start", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// --- Verify HTTP 200 (enrollment itself should succeed) ---
	if rr.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d; body=%s", rr.Code, rr.Body.String())
	}

	// --- Parse the response JSON ---
	var result map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response JSON: %v; body=%s", err, rr.Body.String())
	}

	// --- Assert: response MUST contain "feishu_status" field ---
	// On unfixed code, the response is just the EnrollmentResult struct
	// (status, message, user_id, etc.) with NO feishu_status field.
	// This assertion will FAIL, confirming the bug exists.
	feishuStatus, ok := result["feishu_status"]
	if !ok {
		t.Fatalf("BUG CONFIRMED: response JSON does not contain 'feishu_status' field.\n"+
			"Response: %s\n"+
			"The handler returns the enrollment result BEFORE feishu auto-enroll completes (goroutine).",
			rr.Body.String())
	}

	// If we get here, feishu_status exists — verify it has a valid value.
	statusStr, ok := feishuStatus.(string)
	if !ok {
		t.Fatalf("feishu_status is not a string: %v", feishuStatus)
	}
	validStatuses := map[string]bool{"ok": true, "failed": true, "disabled": true, "skipped": true}
	if !validStatuses[statusStr] {
		t.Fatalf("feishu_status has unexpected value: %q (expected ok/failed/disabled/skipped)", statusStr)
	}

	t.Logf("feishu_status=%s — feishu enrollment result is included in response (bug is fixed)", statusStr)
}
