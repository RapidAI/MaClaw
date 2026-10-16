package httpapi

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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
		ModelServiceGroups:    []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic", AccessPolicy: llmservice.AccessPolicyGrantRequired}},
		GlobalServiceGroupIDs: []string{"coding-basic"},
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
	if first.Checkin == nil || !first.Checkin.CheckedInToday || first.Checkin.DurationDays != checkinDefaultDurationDays {
		t.Fatalf("expected checked_in_today and default duration in first response: %+v", first.Checkin)
	}
	reg, err := llmservice.LoadRegistry(ctx, tenantSystem)
	if err != nil {
		t.Fatal(err)
	}
	granted := 0.0
	var expiresAt time.Time
	for _, grant := range reg.Grants {
		if grant.Source == checkinGrantSource {
			granted += grant.CreditsTotal
			if grant.Permanent {
				t.Fatalf("check-in credits must expire, grant %+v", grant)
			}
			expiresAt = grant.ExpiresAt
		}
	}
	if granted != 10 {
		t.Fatalf("expected 10 check-in credits granted, got %v", granted)
	}
	wantExpiry := time.Now().UTC().AddDate(0, 0, checkinDefaultDurationDays)
	if d := expiresAt.Sub(wantExpiry); d < -2*time.Minute || d > 2*time.Minute {
		t.Fatalf("expected default %d-day expiry near %s, got %s", checkinDefaultDurationDays, wantExpiry.Format(time.RFC3339), expiresAt.Format(time.RFC3339))
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

func TestSplitCheckinCreditsKeepsTheAwardedTotal(t *testing.T) {
	shares := splitCheckinCredits(10, 3)
	if len(shares) != 3 {
		t.Fatalf("shares = %v", shares)
	}
	sum := 0.0
	for _, share := range shares {
		sum += share
	}
	if math.Abs(sum-10) > 0.0001 {
		t.Fatalf("shares %v sum to %v, want 10", shares, sum)
	}
	if shares[0] != 3.333 || shares[1] != 3.333 || shares[2] != 3.334 {
		t.Fatalf("unexpected milli-credit split: %v", shares)
	}
	if got := splitCheckinCredits(6, 1); len(got) != 1 || got[0] != 6 {
		t.Fatalf("single group = %v", got)
	}
	// Rounding every earlier share up must not pay out more than the reward.
	tiny := splitCheckinCredits(0.005, 7)
	tinySum := 0.0
	for _, share := range tiny {
		if share < 0 {
			t.Fatalf("negative share in %v", tiny)
		}
		tinySum += share
	}
	if math.Abs(tinySum-0.005) > 0.0001 {
		t.Fatalf("tiny shares %v sum to %v, want 0.005", tiny, tinySum)
	}
}

func TestCheckinConfigDurationDefaultsToOneMonth(t *testing.T) {
	if got := normalizeCheckinConfig(CheckinConfig{Enabled: true, Credits: 10}); got.DurationDays != checkinDefaultDurationDays {
		t.Fatalf("missing duration = %d, want %d", got.DurationDays, checkinDefaultDurationDays)
	}
	var legacy CheckinConfig
	if err := json.Unmarshal([]byte(`{"enabled":true,"credits":8}`), &legacy); err != nil {
		t.Fatal(err)
	}
	legacy = normalizeCheckinConfig(legacy)
	if legacy.DurationDays != checkinDefaultDurationDays || legacy.Credits != 8 {
		t.Fatalf("legacy config = %+v", legacy)
	}
	if err := validateCheckinConfig(CheckinConfig{Enabled: true, Credits: 10, DurationDays: 0}); err == nil {
		t.Fatal("expected an explicit zero duration to be rejected")
	}
	if err := validateCheckinConfig(CheckinConfig{Enabled: true, Credits: 10, DurationDays: checkinMaxDurationDays + 1}); err == nil {
		t.Fatal("expected duration above the max to be rejected")
	}
	if err := validateCheckinConfig(CheckinConfig{Enabled: true, Credits: 10, DurationDays: -1}); err == nil {
		t.Fatal("expected a negative duration to be rejected")
	}
	if _, _, err := checkinDurationFromUpdate(nil); err != nil {
		t.Fatal(err)
	}
	fraction := 30.5
	if _, _, err := checkinDurationFromUpdate(&fraction); err == nil {
		t.Fatal("expected a fractional duration to be rejected")
	}

	system := newTestLLMServiceSystemSettings()
	put := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/admin/settings/checkin", strings.NewReader(body))
		rec := httptest.NewRecorder()
		UpdateCheckinConfigHandler(system).ServeHTTP(rec, req)
		return rec
	}
	rec := put(`{"enabled":true,"credits":12}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("default duration save status = %d body=%s", rec.Code, rec.Body.String())
	}
	var saved CheckinConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.DurationDays != checkinDefaultDurationDays {
		t.Fatalf("saved duration = %d, want %d", saved.DurationDays, checkinDefaultDurationDays)
	}
	rec = put(`{"enabled":true,"credits":12,"duration_days":45}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("custom duration save status = %d body=%s", rec.Code, rec.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/settings/checkin", nil)
	rec = httptest.NewRecorder()
	GetCheckinConfigHandler(system).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.DurationDays != 45 || saved.Credits != 12 {
		t.Fatalf("loaded config = %+v", saved)
	}
	// An older client that omits duration must not reset a custom validity.
	rec = put(`{"enabled":true,"credits":12}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("omit duration status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.DurationDays != 45 {
		t.Fatalf("omitted duration reset stored value to %d", saved.DurationDays)
	}
	rec = put(`{"enabled":true,"credits":12,"duration_days":0}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("zero duration status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = put(`{"enabled":true,"credits":12,"duration_days":30.5}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("fractional duration status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = put(`{"enabled":true,"credits":12,"duration_days":4000}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid duration status = %d body=%s", rec.Code, rec.Body.String())
	}
	// Over-max credits stay clamped, matching the previous save behavior.
	rec = put(`{"enabled":false,"credits":1000001,"duration_days":45}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("credit clamp status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Credits != checkinMaxCredits || saved.DurationDays != 45 {
		t.Fatalf("clamped config = %+v", saved)
	}
}

func TestPostLLMServiceCheckinHonorsConfiguredDuration(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken := issueViewerTokenForTenant(t, identity, "tenant_checkin_days", "days@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	tenantSystem := scopedSystemSettingsForTenant("tenant_checkin_days", system)
	data, err := json.Marshal(CheckinConfig{Enabled: true, Credits: 6, DurationDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	if err := tenantSystem.Set(ctx, checkinConfigKey, string(data)); err != nil {
		t.Fatal(err)
	}
	if err := llmservice.SaveRegistry(ctx, tenantSystem, &llmservice.Registry{
		ModelServiceGroups:    []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic", AccessPolicy: llmservice.AccessPolicyGrantRequired}},
		GlobalServiceGroupIDs: []string{"coding-basic"},
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/service/checkin", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()
	PostLLMServiceCheckinHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("check-in status = %d body=%s", rec.Code, rec.Body.String())
	}
	reg, err := llmservice.LoadRegistry(ctx, tenantSystem)
	if err != nil {
		t.Fatal(err)
	}
	var grant *llmservice.Grant
	for i := range reg.Grants {
		if reg.Grants[i].Source == checkinGrantSource {
			grant = &reg.Grants[i]
			break
		}
	}
	if grant == nil {
		t.Fatal("expected a check-in grant")
	}
	want := time.Now().UTC().AddDate(0, 0, 7)
	if grant.Permanent || grant.ExpiresAt.Sub(want) < -2*time.Minute || grant.ExpiresAt.Sub(want) > 2*time.Minute {
		t.Fatalf("expected 7-day expiry near %s, got permanent=%v expires=%s", want.Format(time.RFC3339), grant.Permanent, grant.ExpiresAt.Format(time.RFC3339))
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
		ModelServiceGroups:    []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic", AccessPolicy: llmservice.AccessPolicyGrantRequired}},
		GlobalServiceGroupIDs: []string{"coding-basic"},
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
		ModelServiceGroups:    []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic", AccessPolicy: llmservice.AccessPolicyGrantRequired}},
		GlobalServiceGroupIDs: []string{"coding-basic"},
	}); err != nil {
		t.Fatal(err)
	}

	// Seed inside the same UTC windows the handler queries. Offsets from
	// time.Now() fall into yesterday during the first hours of a UTC day, and
	// a Monday has no earlier day inside the current week.
	now := time.Now().UTC()
	dayStart, _, ok := checkinPeriodRange("day", now)
	if !ok {
		t.Fatal("day period")
	}
	weekStart, _, ok := checkinPeriodRange("week", now)
	if !ok {
		t.Fatal("week period")
	}
	appendCheckinLog(ctx, tenantSystem, checkinLogEntry{UserID: "u1", Email: "u1@example.com", Credits: 3, At: dayStart.Add(3 * time.Hour)})
	appendCheckinLog(ctx, tenantSystem, checkinLogEntry{UserID: "u2", Email: "u2@example.com", Credits: 3, At: dayStart.Add(2 * time.Hour)})
	appendCheckinLog(ctx, tenantSystem, checkinLogEntry{UserID: "u1", Email: "u1@example.com", Credits: 3, At: dayStart.Add(time.Hour)})
	olderInThisWeek := weekStart.Before(dayStart)
	olderAt := dayStart.Add(-time.Hour)
	if olderInThisWeek {
		olderAt = weekStart.Add(time.Hour)
	}
	appendCheckinLog(ctx, tenantSystem, checkinLogEntry{UserID: "u1", Email: "u1@example.com", Credits: 3, At: olderAt})

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
	weekCredits := 9.0
	if olderInThisWeek {
		weekCredits = 12
	}
	weekStats := fetch("week", 1)["stats"].(map[string]any)
	if weekStats["users"].(float64) != 2 || weekStats["credits"].(float64) != weekCredits {
		t.Fatalf("unexpected week stats: %+v", weekStats)
	}

	records := fetch("day", 1)["records"].([]any)
	if len(records) != 3 {
		t.Fatalf("expected 3 day records, got %d", len(records))
	}
	first := records[0].(map[string]any)
	// Newest first: the 03:00 UTC entry leads.
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

// Concurrent same-day check-ins must grant exactly once: the process mutex
// serializes the load-mark-grant-save sequence.
func TestPostLLMServiceCheckinConcurrentGrantsOnce(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken := issueViewerTokenForTenant(t, identity, "tenant_checkin_race", "race@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	tenantSystem := scopedSystemSettingsForTenant("tenant_checkin_race", system)
	enableTestCheckin(t, system, "tenant_checkin_race", 20)
	if err := llmservice.SaveRegistry(ctx, tenantSystem, &llmservice.Registry{
		ModelServiceGroups:    []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic", AccessPolicy: llmservice.AccessPolicyGrantRequired}},
		GlobalServiceGroupIDs: []string{"coding-basic"},
	}); err != nil {
		t.Fatal(err)
	}

	const racers = 8
	start := make(chan struct{})
	done := make(chan *httptest.ResponseRecorder, racers)
	for i := 0; i < racers; i++ {
		go func() {
			<-start
			req := httptest.NewRequest(http.MethodPost, "/api/llm/service/checkin", nil)
			req.Header.Set("Authorization", "Bearer "+viewerToken)
			rec := httptest.NewRecorder()
			PostLLMServiceCheckinHandler(identity, system, nil).ServeHTTP(rec, req)
			done <- rec
		}()
	}
	close(start)
	// The mutex must serialize every phase (including the marker write) so
	// exactly one request awards and the rest see the idempotent reply.
	totalAwarded := 0.0
	winners := 0
	for i := 0; i < racers; i++ {
		rec := <-done
		if rec.Code != http.StatusOK {
			t.Fatalf("concurrent check-in status = %d body=%s", rec.Code, rec.Body.String())
		}
		var result hubCheckinResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if !result.Success {
			t.Fatalf("unexpected unsuccessful response: %+v", result)
		}
		if result.CreditsAwarded != 0 {
			winners++
			if result.AlreadyCheckedIn {
				t.Fatalf("awarding response flagged already_checked_in: %+v", result)
			}
		} else if !result.AlreadyCheckedIn {
			t.Fatalf("zero-award response must be already_checked_in: %+v", result)
		}
		totalAwarded += result.CreditsAwarded
	}
	if winners != 1 || totalAwarded != 20 {
		t.Fatalf("expected exactly 1 winner awarding 20 credits across %d concurrent check-ins, got %d winners totaling %v", racers, winners, totalAwarded)
	}
	reg, err := llmservice.LoadRegistry(ctx, tenantSystem)
	if err != nil {
		t.Fatal(err)
	}
	grants := 0
	for _, grant := range reg.Grants {
		if grant.Source == checkinGrantSource {
			grants++
		}
	}
	if grants != 1 {
		t.Fatalf("expected exactly 1 check-in grant, got %d", grants)
	}
}
