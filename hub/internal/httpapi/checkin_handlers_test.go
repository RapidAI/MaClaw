package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func enableTestCheckin(t *testing.T, system *testLLMServiceSystemSettings, tenantID string, credits float64) {
	t.Helper()
	data, err := json.Marshal(CheckinConfig{Enabled: true, Credits: credits})
	if err != nil {
		t.Fatal(err)
	}
	if err := scopedSystemSettingsForTenant(tenantID, system).Set(context.Background(), checkinConfigKey, string(data)); err != nil {
		t.Fatal(err)
	}
}

func TestPostLLMServiceCheckinGrantsCreditsOncePerDay(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken := issueViewerTokenForTenant(t, identity, "tenant_checkin", "checkin@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	tenantSystem := scopedSystemSettingsForTenant("tenant_checkin", system)
	enableTestCheckin(t, system, "tenant_checkin", 10)
	if err := llmservice.SaveRegistry(ctx, tenantSystem, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
	}); err != nil {
		t.Fatal(err)
	}

	post := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/llm/service/checkin", nil)
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		rec := httptest.NewRecorder()
		PostLLMServiceCheckinHandler(identity, system, nil).ServeHTTP(rec, req)
		return rec
	}

	rec := post()
	if rec.Code != http.StatusOK {
		t.Fatalf("first check-in status = %d body=%s", rec.Code, rec.Body.String())
	}
	var first hubCheckinResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if !first.Success || first.AlreadyCheckedIn || first.CreditsAwarded != 10 {
		t.Fatalf("unexpected first check-in response: %+v", first)
	}
	if first.Checkin == nil || !first.Checkin.CheckedInToday {
		t.Fatalf("expected checked_in_today in first response: %+v", first.Checkin)
	}
	reg, err := llmservice.LoadRegistry(ctx, tenantSystem)
	if err != nil {
		t.Fatal(err)
	}
	granted := 0.0
	permanent := 0
	for _, grant := range reg.Grants {
		if grant.Source == checkinGrantSource {
			granted += grant.CreditsTotal
			if grant.Permanent {
				permanent++
			}
		}
	}
	if granted != 10 || permanent == 0 {
		t.Fatalf("expected 10 permanent check-in credits granted, got %v (permanent=%d)", granted, permanent)
	}

	// A repeat check-in on the same day is idempotent and grants nothing.
	rec = post()
	if rec.Code != http.StatusOK {
		t.Fatalf("repeat check-in status = %d body=%s", rec.Code, rec.Body.String())
	}
	var second hubCheckinResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if !second.Success || !second.AlreadyCheckedIn || second.CreditsAwarded != 0 {
		t.Fatalf("unexpected repeat check-in response: %+v", second)
	}
	reg, err = llmservice.LoadRegistry(ctx, tenantSystem)
	if err != nil {
		t.Fatal(err)
	}
	totals := 0.0
	for _, grant := range reg.Grants {
		if grant.Source == checkinGrantSource {
			totals += grant.CreditsTotal
		}
	}
	if totals != 10 {
		t.Fatalf("repeat check-in must not double-grant, got %v", totals)
	}
}

func TestPostLLMServiceCheckinDisabledRejected(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken := issueViewerTokenForTenant(t, identity, "tenant_checkin_off", "off@example.com")
	system := newTestLLMServiceSystemSettings()
	req := httptest.NewRequest(http.MethodPost, "/api/llm/service/checkin", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()
	PostLLMServiceCheckinHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("disabled check-in status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetLLMServiceStatusIncludesCheckinState(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken := issueViewerTokenForTenant(t, identity, "tenant_checkin_status", "status@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	tenantSystem := scopedSystemSettingsForTenant("tenant_checkin_status", system)
	enableTestCheckin(t, system, "tenant_checkin_status", 5)
	now := time.Now().UTC()
	if err := llmservice.SaveRegistry(ctx, tenantSystem, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		Grants: []llmservice.Grant{{
			ID: "g1", Email: "status@example.com", ServiceGroupID: "coding-basic", Source: "card",
			StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CreatedAt: now, CreditsTotal: 50,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	fetchStatus := func() hubCheckinInfo {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/llm/service/status", nil)
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		rec := httptest.NewRecorder()
		GetLLMServiceStatusHandler(identity, system, nil).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		var payload struct {
			*llmservice.ServiceStatus
			Checkin *hubCheckinInfo `json:"checkin"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Checkin == nil {
			t.Fatalf("status response is missing checkin info: %s", rec.Body.String())
		}
		return *payload.Checkin
	}

	before := fetchStatus()
	if !before.Enabled || before.Credits != 5 || before.CheckedInToday {
		t.Fatalf("unexpected pre check-in info: %+v", before)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/llm/service/checkin", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()
	PostLLMServiceCheckinHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("check-in status = %d body=%s", rec.Code, rec.Body.String())
	}

	after := fetchStatus()
	if !after.CheckedInToday {
		t.Fatalf("expected checked_in_today after check-in: %+v", after)
	}
}

func TestGetCheckinRecordsPeriodsPaginationAndStats(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken := issueViewerTokenForTenant(t, identity, "tenant_checkin_records", "records@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	tenantSystem := scopedSystemSettingsForTenant("tenant_checkin_records", system)
	enableTestCheckin(t, system, "tenant_checkin_records", 3)
	if err := llmservice.SaveRegistry(ctx, tenantSystem, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
	}); err != nil {
		t.Fatal(err)
	}

	// Seed the log directly: 3 entries today across 2 users plus one yesterday.
	now := time.Now().UTC()
	appendCheckinLog(ctx, tenantSystem, checkinLogEntry{UserID: "u1", Email: "u1@example.com", Credits: 3, At: now.Add(-time.Hour)})
	appendCheckinLog(ctx, tenantSystem, checkinLogEntry{UserID: "u2", Email: "u2@example.com", Credits: 3, At: now.Add(-2 * time.Hour)})
	appendCheckinLog(ctx, tenantSystem, checkinLogEntry{UserID: "u1", Email: "u1@example.com", Credits: 3, At: now.Add(-3 * time.Hour)})
	appendCheckinLog(ctx, tenantSystem, checkinLogEntry{UserID: "u1", Email: "u1@example.com", Credits: 3, At: now.AddDate(0, 0, -1)})

	fetch := func(period string, page int) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/admin/checkin/records?period="+period+"&page="+strconv.Itoa(page), nil)
		req = req.WithContext(WithRequestTenant(req.Context(), "tenant_checkin_records"))
		rec := httptest.NewRecorder()
		GetCheckinRecordsHandler(system).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("records status = %d body=%s", rec.Code, rec.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}

	dayStats := fetch("day", 1)["stats"].(map[string]any)
	if dayStats["users"].(float64) != 2 || dayStats["credits"].(float64) != 9 {
		t.Fatalf("unexpected day stats: %+v", dayStats)
	}
	weekStats := fetch("week", 1)["stats"].(map[string]any)
	if weekStats["users"].(float64) != 2 || weekStats["credits"].(float64) != 12 {
		t.Fatalf("unexpected week stats: %+v", weekStats)
	}

	records := fetch("day", 1)["records"].([]any)
	if len(records) != 3 {
		t.Fatalf("expected 3 day records, got %d", len(records))
	}
	first := records[0].(map[string]any)
	// Newest first: the -1h entry leads.
	if first["email"].(string) != "u1@example.com" || first["credits"].(float64) != 3 {
		t.Fatalf("unexpected first record: %+v", first)
	}

	// The viewer endpoint still works (records handler is tenant-admin only;
	// this call just verifies the tenant config remains enabled).
	req := httptest.NewRequest(http.MethodPost, "/api/llm/service/checkin", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()
	PostLLMServiceCheckinHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("check-in status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Invalid period is rejected.
	req = httptest.NewRequest(http.MethodGet, "/api/admin/checkin/records?period=year", nil)
	rec = httptest.NewRecorder()
	GetCheckinRecordsHandler(system).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid period status = %d", rec.Code)
	}

	// A hostile huge page must clamp (not overflow into a negative slice index
	// and panic) and return an empty page.
	req = httptest.NewRequest(http.MethodGet, "/api/admin/checkin/records?period=day&page=9223372036854775807", nil)
	req = req.WithContext(WithRequestTenant(req.Context(), "tenant_checkin_records"))
	rec = httptest.NewRecorder()
	GetCheckinRecordsHandler(system).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("huge page status = %d body=%s", rec.Code, rec.Body.String())
	}
	var hugePage map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &hugePage); err != nil {
		t.Fatal(err)
	}
	if records, ok := hugePage["records"].([]any); !ok || len(records) != 0 {
		t.Fatalf("expected empty records for huge page, got %v", hugePage["records"])
	}
}
