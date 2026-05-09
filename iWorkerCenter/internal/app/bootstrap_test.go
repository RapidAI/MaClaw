package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	colleagueSvc "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/service"
	roleSvc "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/service"
)

func TestBootstrapWiresGoalWatchRecoverToWorkflowService(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("IWORKER_CLOUD_URL", "")
	t.Setenv("IWORKER_CENTER_CLOUD_URL", "")

	center, err := Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	t.Cleanup(center.Close)

	tenantID := "tenant-recover"
	role, err := center.Roles.Create(tenantID, roleSvc.CreateRequest{Name: "Quality", Code: "quality"})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	colleague, err := center.Colleagues.Create(tenantID, colleagueSvc.CreateRequest{Name: "Worker A", RoleID: role.ID})
	if err != nil {
		t.Fatalf("create colleague: %v", err)
	}

	defID := postJSONID(t, center.Mux, tenantID, http.MethodPost, "/admin/workflows", map[string]any{
		"name": "Recover route",
		"steps": []map[string]any{{
			"step_name":             "Recoverable step",
			"assignee_role_code":    "quality",
			"assignee_colleague_id": colleague.ID,
			"assignee_mode":         "fixed_colleague",
			"timeout_minutes":       5,
			"reject_rule":           "manual",
		}},
	})
	postJSONStatus(t, center.Mux, tenantID, http.MethodPost, "/admin/workflows/"+defID+"/publish", nil, http.StatusOK)
	instID := postJSONID(t, center.Mux, tenantID, http.MethodPost, "/runtime/workflows/start", map[string]any{
		"definition_id": defID,
		"title":         "Recoverable work",
		"initiator_id":  colleague.ID,
	})
	stepID := firstStepID(t, center.Mux, tenantID, instID)
	staleAt := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	if _, err := center.DB.Write.Exec(`UPDATE collaboration_tasks SET updated_at=? WHERE tenant_id=?`, staleAt, tenantID); err != nil {
		t.Fatalf("age collaboration task: %v", err)
	}

	check, err := center.GoalWatch.CheckTenant(tenantID, time.Now().UTC())
	if err != nil {
		t.Fatalf("goalwatch check: %v", err)
	}
	if check.Pushed != 1 || len(check.Pushes) != 1 || check.Pushes[0].RecoveryAction != "start_workflow_step" {
		t.Fatalf("unexpected goalwatch push: %+v", check)
	}
	pushes := listGoalPushes(t, center.Mux, tenantID, colleague.ID)
	if len(pushes) != 1 || pushes[0].EventID == "" {
		t.Fatalf("unexpected listed pushes: %+v", pushes)
	}
	eventID := pushes[0].EventID
	postJSONStatus(t, center.Mux, tenantID, http.MethodPost, "/client/goalwatch/pushes/"+eventID+"/recover", map[string]any{
		"colleague_id": colleague.ID,
		"note":         "human approved from iWorker",
	}, http.StatusOK)

	events := workflowEvents(t, center.Mux, tenantID, instID)
	if !eventsContain(events, stepID, "step_started") {
		t.Fatalf("workflow events after recover = %+v, want step_started for %s", events, stepID)
	}
	pushes = listGoalPushes(t, center.Mux, tenantID, colleague.ID)
	if len(pushes) != 0 {
		t.Fatalf("expected recovered push to be hidden, got %+v", pushes)
	}

}

func TestBootstrapGoalWatchBlockAckHidesPushWithoutWorkflowRecovery(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("IWORKER_CLOUD_URL", "")
	t.Setenv("IWORKER_CENTER_CLOUD_URL", "")

	center, err := Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	t.Cleanup(center.Close)

	tenantID := "tenant-block"
	role, err := center.Roles.Create(tenantID, roleSvc.CreateRequest{Name: "Quality", Code: "quality"})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	colleague, err := center.Colleagues.Create(tenantID, colleagueSvc.CreateRequest{Name: "Worker B", RoleID: role.ID})
	if err != nil {
		t.Fatalf("create colleague: %v", err)
	}

	defID := postJSONID(t, center.Mux, tenantID, http.MethodPost, "/admin/workflows", map[string]any{
		"name": "Blocked route",
		"steps": []map[string]any{{
			"step_name":             "Blocked step",
			"assignee_role_code":    "quality",
			"assignee_colleague_id": colleague.ID,
			"assignee_mode":         "fixed_colleague",
			"timeout_minutes":       5,
			"reject_rule":           "manual",
		}},
	})
	postJSONStatus(t, center.Mux, tenantID, http.MethodPost, "/admin/workflows/"+defID+"/publish", nil, http.StatusOK)
	instID := postJSONID(t, center.Mux, tenantID, http.MethodPost, "/runtime/workflows/start", map[string]any{
		"definition_id": defID,
		"title":         "Blocked work",
		"initiator_id":  colleague.ID,
	})
	stepID := firstStepID(t, center.Mux, tenantID, instID)
	staleAt := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	if _, err := center.DB.Write.Exec(`UPDATE collaboration_tasks SET updated_at=? WHERE tenant_id=?`, staleAt, tenantID); err != nil {
		t.Fatalf("age collaboration task: %v", err)
	}

	check, err := center.GoalWatch.CheckTenant(tenantID, time.Now().UTC())
	if err != nil {
		t.Fatalf("goalwatch check: %v", err)
	}
	if check.Pushed != 1 || len(check.Pushes) != 1 {
		t.Fatalf("unexpected goalwatch push: %+v", check)
	}
	pushes := listGoalPushes(t, center.Mux, tenantID, colleague.ID)
	if len(pushes) != 1 || pushes[0].EventID == "" {
		t.Fatalf("unexpected listed pushes: %+v", pushes)
	}

	postJSONStatus(t, center.Mux, tenantID, http.MethodPost, "/client/goalwatch/pushes/"+pushes[0].EventID+"/ack", map[string]any{
		"colleague_id": colleague.ID,
		"status":       "blocked",
		"note":         "human blocked from iWorker",
	}, http.StatusOK)

	events := workflowEvents(t, center.Mux, tenantID, instID)
	if eventsContain(events, stepID, "step_started") {
		t.Fatalf("workflow events after block = %+v, did not expect step_started for %s", events, stepID)
	}
	pushes = listGoalPushes(t, center.Mux, tenantID, colleague.ID)
	if len(pushes) != 0 {
		t.Fatalf("expected blocked push to be hidden, got %+v", pushes)
	}
}

func TestBootstrapDoesNotExposeCloudTenantManagementRoutes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("IWORKER_CLOUD_URL", "")
	t.Setenv("IWORKER_CENTER_CLOUD_URL", "")

	center, err := Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	t.Cleanup(center.Close)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/cloud/tenants"},
		{http.MethodPost, "/api/cloud/tenants"},
		{http.MethodPut, "/api/cloud/tenants/tnt_1"},
		{http.MethodDelete, "/api/cloud/tenants/tnt_1"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		res := httptest.NewRecorder()
		center.Mux.ServeHTTP(res, req)
		if res.Code != http.StatusNotFound && res.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s status = %d body=%s, want removed Cloud tenant management route", tc.method, tc.path, res.Code, res.Body.String())
		}
	}
}

func TestBootstrapCollaborationCompleteAdvancesWorkflowStep(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("IWORKER_CLOUD_URL", "")
	t.Setenv("IWORKER_CENTER_CLOUD_URL", "")

	center, err := Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	t.Cleanup(center.Close)

	tenantID := "tenant-collab-complete"
	role, err := center.Roles.Create(tenantID, roleSvc.CreateRequest{Name: "Quality", Code: "quality"})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	colleague, err := center.Colleagues.Create(tenantID, colleagueSvc.CreateRequest{Name: "Worker C", RoleID: role.ID})
	if err != nil {
		t.Fatalf("create colleague: %v", err)
	}

	defID := postJSONID(t, center.Mux, tenantID, http.MethodPost, "/admin/workflows", map[string]any{
		"name": "Collaboration completion route",
		"steps": []map[string]any{{
			"step_name":             "Complete from iWorker",
			"assignee_role_code":    "quality",
			"assignee_colleague_id": colleague.ID,
			"assignee_mode":         "fixed_colleague",
			"timeout_minutes":       5,
			"reject_rule":           "manual",
		}},
	})
	postJSONStatus(t, center.Mux, tenantID, http.MethodPost, "/admin/workflows/"+defID+"/publish", nil, http.StatusOK)
	instID := postJSONID(t, center.Mux, tenantID, http.MethodPost, "/runtime/workflows/start", map[string]any{
		"definition_id": defID,
		"title":         "iWorker handoff work",
		"initiator_id":  colleague.ID,
	})
	step := firstStepInfo(t, center.Mux, tenantID, instID)
	if step.CollaborationTaskID == "" {
		t.Fatalf("started workflow step has no collaboration task: %+v", step)
	}

	postJSONStatus(t, center.Mux, tenantID, http.MethodPost, "/runtime/collaboration/"+step.CollaborationTaskID+"/complete", map[string]any{
		"actor_id": colleague.ID,
		"result":   "completed from iWorker task workspace",
		"note":     "submitted by iWorker",
	}, http.StatusOK)

	events := workflowEvents(t, center.Mux, tenantID, instID)
	if !eventsContain(events, step.ID, "step_completed") {
		t.Fatalf("workflow events after collaboration complete = %+v, want step_completed for %s", events, step.ID)
	}
	updated := firstStepInfo(t, center.Mux, tenantID, instID)
	if updated.Status != "completed" || updated.Result != "completed from iWorker task workspace" {
		t.Fatalf("workflow step after collaboration complete = %+v, want completed with result", updated)
	}
	task := collaborationTask(t, center.Mux, tenantID, step.CollaborationTaskID)
	if task.Status != "completed" || task.Result != "completed from iWorker task workspace" {
		t.Fatalf("collaboration task after complete = %+v, want completed with result", task)
	}
}

func postJSONID(t *testing.T, mux *http.ServeMux, tenantID, method, path string, body any) string {
	t.Helper()
	var result struct {
		ID string `json:"id"`
	}
	postJSONDecode(t, mux, tenantID, method, path, body, http.StatusCreated, &result)
	if result.ID == "" {
		t.Fatalf("%s returned empty id", path)
	}
	return result.ID
}

func postJSONStatus(t *testing.T, mux *http.ServeMux, tenantID, method, path string, body any, wantStatus int) {
	t.Helper()
	postJSONDecode(t, mux, tenantID, method, path, body, wantStatus, nil)
}

func postJSONDecode(t *testing.T, mux *http.ServeMux, tenantID, method, path string, body any, wantStatus int, out any) {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("X-Tenant-ID", tenantID)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != wantStatus {
		t.Fatalf("%s %s status=%d body=%s, want %d", method, path, res.Code, res.Body.String(), wantStatus)
	}
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
}

func getJSONDecode(t *testing.T, mux *http.ServeMux, tenantID, path string, out any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Tenant-ID", tenantID)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, res.Code, res.Body.String())
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func firstStepID(t *testing.T, mux *http.ServeMux, tenantID, instanceID string) string {
	t.Helper()
	return firstStepInfo(t, mux, tenantID, instanceID).ID
}

type workflowStepInfo struct {
	ID                  string `json:"id"`
	CollaborationTaskID string `json:"collaboration_task_id"`
	Status              string `json:"status"`
	Result              string `json:"result"`
}

func firstStepInfo(t *testing.T, mux *http.ServeMux, tenantID, instanceID string) workflowStepInfo {
	t.Helper()
	var result struct {
		Steps []workflowStepInfo `json:"steps"`
	}
	getJSONDecode(t, mux, tenantID, "/admin/workflow-instances/"+instanceID+"/steps", &result)
	if len(result.Steps) != 1 || result.Steps[0].ID == "" {
		t.Fatalf("unexpected steps: %+v", result.Steps)
	}
	return result.Steps[0]
}

func workflowEvents(t *testing.T, mux *http.ServeMux, tenantID, instanceID string) []struct {
	StepID string `json:"step_id"`
	Event  string `json:"event"`
} {
	t.Helper()
	var result struct {
		Events []struct {
			StepID string `json:"step_id"`
			Event  string `json:"event"`
		} `json:"events"`
	}
	getJSONDecode(t, mux, tenantID, "/admin/workflow-instances/"+instanceID+"/events", &result)
	return result.Events
}

func eventsContain(events []struct {
	StepID string `json:"step_id"`
	Event  string `json:"event"`
}, stepID, eventName string) bool {
	for _, event := range events {
		if event.StepID == stepID && event.Event == eventName {
			return true
		}
	}
	return false
}

func listGoalPushes(t *testing.T, mux *http.ServeMux, tenantID, colleagueID string) []struct {
	EventID string `json:"event_id"`
} {
	t.Helper()
	var result struct {
		Pushes []struct {
			EventID string `json:"event_id"`
		} `json:"pushes"`
	}
	getJSONDecode(t, mux, tenantID, "/client/goalwatch/pushes?colleague_id="+colleagueID, &result)
	return result.Pushes
}

func collaborationTask(t *testing.T, mux *http.ServeMux, tenantID, taskID string) struct {
	Status string `json:"status"`
	Result string `json:"result"`
} {
	t.Helper()
	var result struct {
		Status string `json:"status"`
		Result string `json:"result"`
	}
	getJSONDecode(t, mux, tenantID, "/admin/collaborations/"+taskID, &result)
	return result
}
