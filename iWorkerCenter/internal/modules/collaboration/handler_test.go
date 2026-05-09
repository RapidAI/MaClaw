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

func TestClientListReturnsTenantScopedColleagueInbox(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)
	handler := NewHandler(svc, audit.NewRepo(p.Write, p.Read))

	target, err := svc.Create(testTenantID, CreateRequest{Title: "target handoff", FromColleagueID: "col-a", ToColleagueID: "col-d", Priority: 3})
	if err != nil {
		t.Fatalf("create target task: %v", err)
	}
	if _, err := p.Write.Exec(`UPDATE collaboration_tasks SET workflow_step_instance_id=? WHERE tenant_id=? AND id=?`, "wf-step-client-1", testTenantID, target.ID); err != nil {
		t.Fatalf("mark target workflow-backed: %v", err)
	}
	if _, err := svc.Create(testTenantID, CreateRequest{Title: "other worker handoff", FromColleagueID: "col-a", ToColleagueID: "col-b", Priority: 3}); err != nil {
		t.Fatalf("create other worker task: %v", err)
	}
	completed, err := svc.Create(testTenantID, CreateRequest{Title: "completed handoff", FromColleagueID: "col-a", ToColleagueID: "col-d", Priority: 3})
	if err != nil {
		t.Fatalf("create completed task: %v", err)
	}
	if err := svc.Transition(testTenantID, completed.ID, StatusCompleted, "col-d", "done", "done"); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	rejected, err := svc.Create(testTenantID, CreateRequest{Title: "rejected handoff", FromColleagueID: "col-a", ToColleagueID: "col-d", Priority: 3})
	if err != nil {
		t.Fatalf("create rejected task: %v", err)
	}
	if err := svc.Transition(testTenantID, rejected.ID, StatusRejected, "col-d", "", "not needed"); err != nil {
		t.Fatalf("reject task: %v", err)
	}

	otherTenantID := "tenant-other-001"
	now := time.Now()
	roleRp := roleRepo.New(p.Write, p.Read)
	_ = roleRp.Insert(otherTenantID, &roleDomain.Role{ID: "role-other", Name: "Other Role", Code: "other", DefaultStrengths: []string{}, ApplicableTasks: []string{}, Status: "active", CreatedAt: now, UpdatedAt: now})
	colRp := colleagueRepo.New(p.Write, p.Read)
	_ = colRp.Insert(otherTenantID, &colleagueDomain.Colleague{ID: "col-a", Name: "Other A", RoleID: "role-other", Strengths: []string{}, Tasks: []string{}, Status: "active", CreatedAt: now, UpdatedAt: now})
	_ = colRp.Insert(otherTenantID, &colleagueDomain.Colleague{ID: "col-d", Name: "Other D", RoleID: "role-other", Strengths: []string{}, Tasks: []string{}, Status: "active", CreatedAt: now, UpdatedAt: now})
	if _, err := svc.Create(otherTenantID, CreateRequest{Title: "other tenant handoff", FromColleagueID: "col-a", ToColleagueID: "col-d", Priority: 3}); err != nil {
		t.Fatalf("create other tenant task: %v", err)
	}

	mux := http.NewServeMux()
	handler.RegisterClientRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTenantRequest(http.MethodGet, "/client/collaborations?colleague_id=col-d", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Tasks []taskDTO `json:"tasks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Tasks) != 1 {
		t.Fatalf("tasks = %+v, want exactly target colleague task", resp.Tasks)
	}
	if resp.Tasks[0].Title != "target handoff" || resp.Tasks[0].ToColleagueID != "col-d" {
		t.Fatalf("unexpected task returned: %+v", resp.Tasks[0])
	}
	if resp.Tasks[0].WorkflowStepID != "wf-step-client-1" {
		t.Fatalf("workflow_step_instance_id = %q, want wf-step-client-1", resp.Tasks[0].WorkflowStepID)
	}
}

func TestClientListWithoutColleagueReturnsReadFailure(t *testing.T) {
	p := setupTestDB(t)
	svc := newTestService(p)
	handler := NewHandler(svc, audit.NewRepo(p.Write, p.Read))
	if err := p.Read.Close(); err != nil {
		t.Fatalf("close read db: %v", err)
	}

	mux := http.NewServeMux()
	handler.RegisterClientRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTenantRequest(http.MethodGet, "/client/collaborations", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body: %s", w.Code, w.Body.String())
	}
}

func TestRuntimeTransitionUpdatesOnlyRequestTenantTask(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)
	handler := NewHandler(svc, audit.NewRepo(p.Write, p.Read))

	target, err := svc.Create(testTenantID, CreateRequest{Title: "accept this handoff", FromColleagueID: "col-a", ToColleagueID: "col-d", Priority: 3})
	if err != nil {
		t.Fatalf("create target task: %v", err)
	}
	otherTenantID := "tenant-other-001"
	now := time.Now()
	if err := svc.repo.InsertTask(otherTenantID, &Task{
		ID:              "collab-other-tenant",
		Title:           "other tenant handoff",
		FromColleagueID: "col-a",
		ToColleagueID:   "col-d",
		Status:          StatusPending,
		Priority:        1,
		CreatedAt:       now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("insert other tenant task: %v", err)
	}

	mux := http.NewServeMux()
	handler.RegisterClientRoutes(mux)
	payload := []byte(`{"actor_id":"worker-ops","note":"accepted from iWorker"}`)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTenantRequest(http.MethodPost, "/runtime/collaboration/"+target.ID+"/accept", payload))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Status string  `json:"status"`
		Task   taskDTO `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal transition response: %v", err)
	}
	if resp.Status != "ok" || resp.Task.ID != target.ID || resp.Task.Status != StatusAccepted {
		t.Fatalf("transition response = %+v", resp)
	}

	updated, err := svc.GetByID(testTenantID, target.ID)
	if err != nil {
		t.Fatalf("get updated task: %v", err)
	}
	if updated.Status != StatusAccepted {
		t.Fatalf("target status = %q, want accepted", updated.Status)
	}
	other, err := svc.GetByID(otherTenantID, "collab-other-tenant")
	if err != nil {
		t.Fatalf("get other tenant task: %v", err)
	}
	if other.Status != StatusPending {
		t.Fatalf("other tenant status = %q, want pending", other.Status)
	}
	events, err := svc.ListEvents(testTenantID, target.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) < 2 || events[len(events)-1].Event != StatusAccepted || events[len(events)-1].ActorID != "worker-ops" {
		t.Fatalf("events = %+v, want accepted event by worker-ops", events)
	}
}

func TestRuntimeTransitionRejectsMissingActorID(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)
	handler := NewHandler(svc, audit.NewRepo(p.Write, p.Read))

	target, err := svc.Create(testTenantID, CreateRequest{Title: "missing actor handoff", FromColleagueID: "col-a", ToColleagueID: "col-d", Priority: 3})
	if err != nil {
		t.Fatalf("create target task: %v", err)
	}

	mux := http.NewServeMux()
	handler.RegisterClientRoutes(mux)
	payload := []byte(`{"actor_id":" ","note":"accepted from unknown actor"}`)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTenantRequest(http.MethodPost, "/runtime/collaboration/"+target.ID+"/accept", payload))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", w.Code, w.Body.String())
	}
	unchanged, err := svc.GetByID(testTenantID, target.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if unchanged.Status != StatusPending {
		t.Fatalf("status = %q, want pending", unchanged.Status)
	}
}

func TestRuntimeTransitionRejectsInvalidJSONWithoutChangingTask(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)
	handler := NewHandler(svc, audit.NewRepo(p.Write, p.Read))

	target, err := svc.Create(testTenantID, CreateRequest{Title: "bad json handoff", FromColleagueID: "col-a", ToColleagueID: "col-d", Priority: 3})
	if err != nil {
		t.Fatalf("create target task: %v", err)
	}

	mux := http.NewServeMux()
	handler.RegisterClientRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTenantRequest(http.MethodPost, "/runtime/collaboration/"+target.ID+"/accept", []byte(`{"actor_id":`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", w.Code, w.Body.String())
	}
	unchanged, err := svc.GetByID(testTenantID, target.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if unchanged.Status != StatusPending {
		t.Fatalf("status = %q, want pending", unchanged.Status)
	}
}

func TestRuntimeTransitionRejectsTrailingJSONWithoutChangingTask(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)
	handler := NewHandler(svc, audit.NewRepo(p.Write, p.Read))

	target, err := svc.Create(testTenantID, CreateRequest{Title: "trailing json handoff", FromColleagueID: "col-a", ToColleagueID: "col-d", Priority: 3})
	if err != nil {
		t.Fatalf("create target task: %v", err)
	}

	mux := http.NewServeMux()
	handler.RegisterClientRoutes(mux)
	payload := []byte(`{"actor_id":"worker-ops","note":"accept this"} {"actor_id":"worker-ops","note":"ignore this"}`)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTenantRequest(http.MethodPost, "/runtime/collaboration/"+target.ID+"/accept", payload))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", w.Code, w.Body.String())
	}
	unchanged, err := svc.GetByID(testTenantID, target.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if unchanged.Status != StatusPending {
		t.Fatalf("status = %q, want pending", unchanged.Status)
	}
	events, err := svc.ListEvents(testTenantID, target.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 || events[0].Event == StatusAccepted {
		t.Fatalf("unexpected events after rejected transition: %+v", events)
	}
}

func TestHandleSettingsRejectsOversizedJSONWithoutSaving(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)
	handler := NewHandler(svc, audit.NewRepo(p.Write, p.Read))

	mux := http.NewServeMux()
	handler.RegisterAdminRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTenantRequest(http.MethodPost, "/admin/collaborations-settings", bytes.Repeat([]byte{'x'}, maxCollaborationJSONBodyBytes+1)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", w.Code, w.Body.String())
	}
	settings, err := svc.GetRoutingSettings(testTenantID)
	if err != nil {
		t.Fatalf("get routing settings: %v", err)
	}
	if len(settings.RuntimeStateByColleague) != 0 || len(settings.RoleStrategies) != 0 {
		t.Fatalf("expected settings to remain empty, got %+v", settings)
	}
}

func TestRuntimeTransitionSupportsEscapedTaskID(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)
	handler := NewHandler(svc, audit.NewRepo(p.Write, p.Read))

	now := time.Now()
	target := &Task{
		ID:              "collab/a",
		Title:           "escaped id handoff",
		FromColleagueID: "col-a",
		ToColleagueID:   "col-d",
		Status:          StatusAccepted,
		Priority:        3,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := svc.repo.InsertTask(testTenantID, target); err != nil {
		t.Fatalf("insert target task: %v", err)
	}

	mux := http.NewServeMux()
	handler.RegisterClientRoutes(mux)
	payload := []byte(`{"actor_id":"worker-ops","note":"started from iWorker"}`)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTenantRequest(http.MethodPost, "/runtime/collaboration/collab%2Fa/start", payload))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	updated, err := svc.GetByID(testTenantID, "collab/a")
	if err != nil {
		t.Fatalf("get updated task: %v", err)
	}
	if updated.Status != StatusInProgress {
		t.Fatalf("status = %q, want in_progress", updated.Status)
	}
}

func TestAdminCollaborationSupportsEscapedTaskIDAndEvents(t *testing.T) {
	p := setupTestDB(t)
	seedColleagues(t, p)
	svc := newTestService(p)
	handler := NewHandler(svc, audit.NewRepo(p.Write, p.Read))

	now := time.Now()
	target := &Task{
		ID:              "collab/a",
		Title:           "escaped admin handoff",
		FromColleagueID: "col-a",
		ToColleagueID:   "col-d",
		Status:          StatusPending,
		Priority:        3,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := svc.repo.InsertTask(testTenantID, target); err != nil {
		t.Fatalf("insert target task: %v", err)
	}
	if err := svc.repo.InsertEvent(testTenantID, &TaskEvent{
		ID:        "cevt-escaped-admin",
		TaskID:    target.ID,
		Event:     "created",
		ActorID:   "col-a",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("insert target event: %v", err)
	}

	mux := http.NewServeMux()
	handler.RegisterAdminRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTenantRequest(http.MethodGet, "/admin/collaborations/collab%2Fa", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	var task taskDTO
	if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
		t.Fatalf("unmarshal task response: %v", err)
	}
	if task.ID != target.ID {
		t.Fatalf("task id = %q, want %q", task.ID, target.ID)
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, newTenantRequest(http.MethodGet, "/admin/collaborations/collab%2Fa/events", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("events status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	var eventsResp struct {
		Events []eventDTO `json:"events"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &eventsResp); err != nil {
		t.Fatalf("unmarshal events response: %v", err)
	}
	if len(eventsResp.Events) != 1 || eventsResp.Events[0].TaskID != target.ID {
		t.Fatalf("events = %+v, want one event for %q", eventsResp.Events, target.ID)
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
