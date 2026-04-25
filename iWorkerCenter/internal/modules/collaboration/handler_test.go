package collaboration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/audit"
	colleagueDomain "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/domain"
	colleagueRepo "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/repo"
	roleDomain "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/domain"
	roleRepo "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/repo"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
)

func newTenantRequest(method, target string, body []byte) *http.Request {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader([]byte{})
	} else {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	req.Header.Set("Content-Type", "application/json")
	return req.WithContext(tenant.WithTenantID(req.Context(), testTenantID))
}

func TestHandleSettingsReturnsRoutingOverview(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)
	handler := NewHandler(svc, audit.NewRepo(p.Write, p.Read))

	if err := svc.SaveRoutingSettings(testTenantID, RoutingSettings{
		HeartbeatTimeoutSeconds: 45,
		RuntimeStateByColleague: map[string]string{"col-a": RuntimeStateStandby},
	}); err != nil {
		t.Fatalf("save routing settings: %v", err)
	}

	mux := http.NewServeMux()
	handler.RegisterAdminRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTenantRequest(http.MethodGet, "/admin/collaborations-settings", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Settings struct {
			HeartbeatTimeoutSeconds int `json:"heartbeat_timeout_seconds"`
		} `json:"settings"`
		ActiveCount       int `json:"active_count"`
		StandbyCount      int `json:"standby_count"`
		UnhealthyCount    int `json:"unhealthy_count"`
		StatusByColleague map[string]struct {
			EffectiveState string `json:"effective_state"`
			Reason         string `json:"reason"`
		} `json:"status_by_colleague"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Settings.HeartbeatTimeoutSeconds != 45 {
		t.Fatalf("heartbeat timeout = %d, want 45", resp.Settings.HeartbeatTimeoutSeconds)
	}
	if resp.StandbyCount != 1 || resp.ActiveCount != 3 || resp.UnhealthyCount != 0 {
		t.Fatalf("counts = active:%d standby:%d unhealthy:%d", resp.ActiveCount, resp.StandbyCount, resp.UnhealthyCount)
	}
	if got := resp.StatusByColleague["col-a"]; got.EffectiveState != RuntimeStateStandby || got.Reason != "manual_standby" {
		t.Fatalf("col-a status = %+v", got)
	}
}

func TestHandleHeartbeatRecordsTimestamp(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)
	handler := NewHandler(svc, audit.NewRepo(p.Write, p.Read))

	mux := http.NewServeMux()
	handler.RegisterClientRoutes(mux)

	observedAt := time.Now().UTC().Truncate(time.Second)
	payload, err := json.Marshal(map[string]string{
		"colleague_id": "col-d",
		"observed_at":  observedAt.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTenantRequest(http.MethodPost, "/runtime/collaboration/heartbeat", payload))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}

	settings, err := svc.GetRoutingSettings(testTenantID)
	if err != nil {
		t.Fatalf("get routing settings: %v", err)
	}
	if got := settings.LastHeartbeatByColleague["col-d"]; got != observedAt.Format(time.RFC3339) {
		t.Fatalf("heartbeat = %q, want %q", got, observedAt.Format(time.RFC3339))
	}
}

func TestHandleHeartbeatRejectsUnknownColleague(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)
	handler := NewHandler(svc, audit.NewRepo(p.Write, p.Read))

	mux := http.NewServeMux()
	handler.RegisterClientRoutes(mux)

	payload := []byte(`{"colleague_id":"col-missing"}`)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTenantRequest(http.MethodPost, "/runtime/collaboration/heartbeat", payload))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", w.Code, w.Body.String())
	}
}

func TestHandleRoleActionExecutesAndAudits(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)
	auditRepo := audit.NewRepo(p.Write, p.Read)
	handler := NewHandler(svc, auditRepo)

	roleRp := roleRepo.New(p.Write, p.Read)
	now := time.Now()
	_ = roleRp.Insert(testTenantID, &roleDomain.Role{
		ID: "role-action", Name: "Action Ops", Code: "action-ops",
		DefaultStrengths: []string{}, ApplicableTasks: []string{},
		Status: "active", CreatedAt: now, UpdatedAt: now,
	})
	colRp := colleagueRepo.New(p.Write, p.Read)
	_ = colRp.Insert(testTenantID, &colleagueDomain.Colleague{ID: "col-action-a", Name: "Primary", RoleID: "role-action", Strengths: []string{}, Tasks: []string{}, Status: "active", CreatedAt: now, UpdatedAt: now})
	_ = colRp.Insert(testTenantID, &colleagueDomain.Colleague{ID: "col-action-b", Name: "Standby", RoleID: "role-action", Strengths: []string{}, Tasks: []string{}, Status: "active", CreatedAt: now, UpdatedAt: now})
	if err := svc.SaveRoutingSettings(testTenantID, RoutingSettings{RuntimeStateByColleague: map[string]string{"col-action-b": RuntimeStateStandby}}); err != nil {
		t.Fatalf("save routing settings: %v", err)
	}

	mux := http.NewServeMux()
	handler.RegisterAdminRoutes(mux)
	payload := []byte(`{"role_code":"action-ops","action":"promote_standby","actor_id":"board_console"}`)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTenantRequest(http.MethodPost, "/admin/collaborations-settings/actions", payload))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}

	settings, err := svc.GetRoutingSettings(testTenantID)
	if err != nil {
		t.Fatalf("get routing settings: %v", err)
	}
	if settings.RuntimeStateByColleague["col-action-b"] != RuntimeStateActive {
		t.Fatalf("standby candidate not promoted: %+v", settings.RuntimeStateByColleague)
	}
	logs, err := auditRepo.ListRecent(testTenantID, 5)
	if err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	if len(logs) == 0 || logs[0].WorkType != "role_routing_action" {
		t.Fatalf("expected role_routing_action audit log, got %+v", logs)
	}
	if logs[0].Summary == "" {
		t.Fatal("expected audit summary")
	}
}
