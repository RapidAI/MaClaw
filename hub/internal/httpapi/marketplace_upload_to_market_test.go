package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/capability"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestLooksLikeEmail(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"admin@example.com", true},
		{"  a@b.co  ", true},
		{"enterprise", false},
		{"not-an-email", false},
		{"@nodomain", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := looksLikeEmail(tc.in); got != tc.want {
			t.Fatalf("looksLikeEmail(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestMapHubCenterSubmissionStatus(t *testing.T) {
	t.Parallel()
	if got := mapHubCenterSubmissionStatus("success"); got != "published" {
		t.Fatalf("success -> %q", got)
	}
	if got := mapHubCenterSubmissionStatus("failed"); got != "failed" {
		t.Fatalf("failed -> %q", got)
	}
	if got := mapHubCenterSubmissionStatus("pending"); got != "pending" {
		t.Fatalf("pending -> %q", got)
	}
	// Empty must stay empty so failed polls do not clobber local "processing".
	if got := mapHubCenterSubmissionStatus(""); got != "" {
		t.Fatalf("empty -> %q, want empty", got)
	}
	if got := mapHubCenterSubmissionStatus("  "); got != "" {
		t.Fatalf("blank -> %q, want empty", got)
	}
}

func TestMarketSubmissionTimestampStale(t *testing.T) {
	t.Parallel()
	if marketSubmissionTimestampStale("", time.Minute) {
		t.Fatal("empty timestamp should not be stale")
	}
	fresh := time.Now().UTC().Format(time.RFC3339)
	if marketSubmissionTimestampStale(fresh, marketUploadReservationMaxAge) {
		t.Fatal("fresh timestamp should not be stale")
	}
	// Still within upload client timeout window — must not expire.
	mid := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	if marketSubmissionTimestampStale(mid, marketUploadReservationMaxAge) {
		t.Fatal("10m-old uploading reservation should not be stale under reservation maxAge")
	}
	old := time.Now().UTC().Add(-20 * time.Minute).Format(time.RFC3339)
	if !marketSubmissionTimestampStale(old, marketUploadReservationMaxAge) {
		t.Fatal("old timestamp should be stale")
	}
	if !marketSubmissionTimestampStale("not-a-time", time.Minute) {
		t.Fatal("unparseable timestamp should be treated as stale")
	}
}

func TestClaimMarketUploadRejectsSecondInFlight(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	ctx := capability.WithTenant(context.Background(), "tenant_a")

	first := &capability.MarketSubmission{
		ID:             capability.NewID("mkt_sub"),
		CapabilityRef:  "cap-1",
		CapabilityName: "Skill One",
		Status:         "uploading",
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	if err := svc.ClaimMarketUpload(ctx, first); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	second := &capability.MarketSubmission{
		ID:             capability.NewID("mkt_sub"),
		CapabilityRef:  "cap-1",
		CapabilityName: "Skill One",
		Status:         "uploading",
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	err := svc.ClaimMarketUpload(ctx, second)
	if !errors.Is(err, capability.ErrMarketSubmissionInFlight) {
		t.Fatalf("second claim err=%v, want ErrMarketSubmissionInFlight", err)
	}
	// Different capability can still claim.
	other := &capability.MarketSubmission{
		ID:             capability.NewID("mkt_sub"),
		CapabilityRef:  "cap-2",
		CapabilityName: "Skill Two",
		Status:         "uploading",
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	if err := svc.ClaimMarketUpload(ctx, other); err != nil {
		t.Fatalf("other capability claim: %v", err)
	}
}

func TestClaimMarketUploadSerializesConcurrentClaims(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	ctx := capability.WithTenant(context.Background(), "tenant_a")

	const n = 8
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			sub := &capability.MarketSubmission{
				ID:             capability.NewID("mkt_sub"),
				CapabilityRef:  "cap-race",
				CapabilityName: "Race Skill",
				Status:         "uploading",
				CreatedAt:      time.Now().UTC().Format(time.RFC3339),
				UpdatedAt:      time.Now().UTC().Format(time.RFC3339),
			}
			errs <- svc.ClaimMarketUpload(ctx, sub)
		}()
	}
	var won, blocked, other int
	for i := 0; i < n; i++ {
		err := <-errs
		switch {
		case err == nil:
			won++
		case errors.Is(err, capability.ErrMarketSubmissionInFlight):
			blocked++
		default:
			other++
			t.Errorf("unexpected claim error: %v", err)
		}
	}
	if won != 1 {
		t.Fatalf("winners=%d, want exactly 1", won)
	}
	if blocked != n-1 {
		t.Fatalf("blocked=%d, want %d", blocked, n-1)
	}
	if other != 0 {
		t.Fatalf("other errors=%d", other)
	}
}

func TestResolveMarketUploadEmailPrefersMetadataThenAdmin(t *testing.T) {
	t.Parallel()
	item := &capability.CapabilitySummary{
		Publisher:    "acme-corp",
		MetadataJSON: `{"publisher_email":"seller@example.com"}`,
	}
	if got := resolveMarketUploadEmail(context.Background(), item); got != "seller@example.com" {
		t.Fatalf("metadata email = %q", got)
	}

	// No metadata email: use admin from context.
	adminCtx := context.WithValue(context.Background(), adminUserContextKey, &store.AdminUser{
		Email: "admin@hub.local",
		Scope: "tenant",
	})
	item2 := &capability.CapabilitySummary{Publisher: "not-email"}
	if got := resolveMarketUploadEmail(adminCtx, item2); got != "admin@hub.local" {
		t.Fatalf("admin email = %q", got)
	}

	// Publisher as email-shaped fallback.
	item3 := &capability.CapabilitySummary{Publisher: "pub@example.com"}
	if got := resolveMarketUploadEmail(context.Background(), item3); got != "pub@example.com" {
		t.Fatalf("publisher email = %q", got)
	}

	// Nothing usable.
	item4 := &capability.CapabilitySummary{Publisher: "enterprise"}
	if got := resolveMarketUploadEmail(context.Background(), item4); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestWaitHubCenterSubmissionReturnsTerminalStatus(t *testing.T) {
	t.Parallel()
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		status := "processing"
		if calls >= 2 {
			status = "success"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":   status,
			"skill_id": "skill-xyz",
		})
	}))
	defer srv.Close()

	status, errMsg, skillID := waitHubCenterSubmission(context.Background(), srv.URL, "sub-1", 3*time.Second)
	if status != "success" {
		t.Fatalf("status=%q err=%q", status, errMsg)
	}
	if skillID != "skill-xyz" {
		t.Fatalf("skill_id=%q", skillID)
	}
	if calls < 2 {
		t.Fatalf("expected polling, calls=%d", calls)
	}
}

func TestWaitHubCenterSubmissionSurfacesFailure(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "failed",
			"error_msg": "description is required",
		})
	}))
	defer srv.Close()

	status, errMsg, _ := waitHubCenterSubmission(context.Background(), srv.URL, "sub-fail", 2*time.Second)
	if status != "failed" {
		t.Fatalf("status=%q", status)
	}
	if !strings.Contains(errMsg, "description is required") {
		t.Fatalf("errMsg=%q", errMsg)
	}
}
