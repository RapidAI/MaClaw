package workflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
)

func TestPublishWorkflowHTTPSupportsEscapedDefinitionID(t *testing.T) {
	p := setupTestDB(t)
	repo := NewRepo(p.Write, p.Read)
	now := time.Now()
	def := &Definition{
		ID:          "wf/team a",
		Name:        "Team Workflow",
		TriggerType: "manual",
		Status:      DefStatusDraft,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := repo.InsertDefinition(testTenantID, def); err != nil {
		t.Fatalf("insert definition: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(newTestService(t, p)).RegisterAdminRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/admin/workflows/wf%2Fteam%20a/publish", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), testTenantID))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	updated, err := repo.GetDefinition(testTenantID, "wf/team a")
	if err != nil {
		t.Fatalf("get definition: %v", err)
	}
	if updated.Status != DefStatusPublished {
		t.Fatalf("status = %q, want %q", updated.Status, DefStatusPublished)
	}
}

func TestStepActionRejectsInvalidJSONWithoutStartingStep(t *testing.T) {
	p := setupTestDB(t)
	seedRolesAndColleagues(t, p)
	svc := newTestService(t, p)
	def, err := svc.CreateDefinition(testTenantID, CreateDefinitionRequest{Name: "Bad JSON path", Steps: []CreateStepDefRequest{{StepName: "Review", AssigneeRoleCode: "quality"}}})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}
	if err := svc.PublishDefinition(testTenantID, def.ID); err != nil {
		t.Fatalf("publish definition: %v", err)
	}
	inst, err := svc.StartInstance(testTenantID, StartInstanceRequest{DefinitionID: def.ID, InitiatorID: "col-xiaozhou"})
	if err != nil {
		t.Fatalf("start instance: %v", err)
	}
	steps, err := svc.ListStepInstances(testTenantID, inst.ID)
	if err != nil || len(steps) == 0 {
		t.Fatalf("steps = %+v err=%v", steps, err)
	}

	mux := http.NewServeMux()
	NewHandler(svc).RegisterRuntimeRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/runtime/workflows/steps/"+steps[0].ID+"/resume", strings.NewReader(`{"actor_id":`))
	req = req.WithContext(tenant.WithTenantID(context.Background(), testTenantID))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}

	steps, err = svc.ListStepInstances(testTenantID, inst.ID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	if steps[0].Status != StepPending {
		t.Fatalf("step status = %q, want %q", steps[0].Status, StepPending)
	}
}

func TestStepActionReturnsAuthoritativeStepAndInstance(t *testing.T) {
	p := setupTestDB(t)
	seedRolesAndColleagues(t, p)
	svc := newTestService(t, p)
	def, err := svc.CreateDefinition(testTenantID, CreateDefinitionRequest{Name: "Authoritative runtime response", Steps: []CreateStepDefRequest{{StepName: "Review", AssigneeRoleCode: "quality"}}})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}
	if err := svc.PublishDefinition(testTenantID, def.ID); err != nil {
		t.Fatalf("publish definition: %v", err)
	}
	inst, err := svc.StartInstance(testTenantID, StartInstanceRequest{DefinitionID: def.ID, InitiatorID: "col-xiaozhou"})
	if err != nil {
		t.Fatalf("start instance: %v", err)
	}
	steps, err := svc.ListStepInstances(testTenantID, inst.ID)
	if err != nil || len(steps) != 1 {
		t.Fatalf("steps = %+v err=%v", steps, err)
	}

	mux := http.NewServeMux()
	NewHandler(svc).RegisterRuntimeRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/runtime/workflows/steps/"+steps[0].ID+"/resume", strings.NewReader(`{"actor_id":"col-xiaozhou","note":"resume from iWorker"}`))
	req = req.WithContext(tenant.WithTenantID(context.Background(), testTenantID))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Status   string      `json:"status"`
		Step     stepInstDTO `json:"step"`
		Instance instDTO     `json:"instance"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Status != "ok" || resp.Step.ID != steps[0].ID || resp.Step.InstanceID != inst.ID || resp.Step.Status != StepInProgress {
		t.Fatalf("response = %+v", resp)
	}
	if resp.Instance.ID != inst.ID || resp.Instance.Status != InstStatusRunning || resp.Instance.CurrentStepID != steps[0].ID {
		t.Fatalf("instance response = %+v", resp.Instance)
	}
}

func TestStepActionRejectsUnassignedActorWithForbidden(t *testing.T) {
	p := setupTestDB(t)
	seedRolesAndColleagues(t, p)
	svc := newTestService(t, p)
	def, err := svc.CreateDefinition(testTenantID, CreateDefinitionRequest{
		Name:  "Forbidden runtime response",
		Steps: []CreateStepDefRequest{{StepName: "Review", AssigneeMode: "fixed_colleague", AssigneeColleagueID: "col-xiaozhou", AssigneeRoleCode: "quality"}},
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}
	if err := svc.PublishDefinition(testTenantID, def.ID); err != nil {
		t.Fatalf("publish definition: %v", err)
	}
	inst, err := svc.StartInstance(testTenantID, StartInstanceRequest{DefinitionID: def.ID, InitiatorID: "col-laochen"})
	if err != nil {
		t.Fatalf("start instance: %v", err)
	}
	steps, err := svc.ListStepInstances(testTenantID, inst.ID)
	if err != nil || len(steps) != 1 {
		t.Fatalf("steps = %+v err=%v", steps, err)
	}

	mux := http.NewServeMux()
	NewHandler(svc).RegisterRuntimeRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/runtime/workflows/steps/"+steps[0].ID+"/resume", strings.NewReader(`{"actor_id":"col-laochen","note":"wrong worker"}`))
	req = req.WithContext(tenant.WithTenantID(context.Background(), testTenantID))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "STEP_ACTOR_FORBIDDEN") {
		t.Fatalf("body = %s, want STEP_ACTOR_FORBIDDEN", rec.Body.String())
	}
	unchanged, err := svc.GetStepInstance(testTenantID, steps[0].ID)
	if err != nil {
		t.Fatalf("get step: %v", err)
	}
	if unchanged.Status != StepPending {
		t.Fatalf("step status = %s, want pending", unchanged.Status)
	}
}

func TestClientWorkflowInstancesFiltersByColleague(t *testing.T) {
	p := setupTestDB(t)
	seedRolesAndColleagues(t, p)
	svc := newTestService(t, p)

	qualityDef, err := svc.CreateDefinition(testTenantID, CreateDefinitionRequest{Name: "Quality workflow", Steps: []CreateStepDefRequest{{StepName: "Quality review", AssigneeMode: "fixed_colleague", AssigneeColleagueID: "col-xiaozhou", AssigneeRoleCode: "quality"}}})
	if err != nil {
		t.Fatalf("create quality definition: %v", err)
	}
	if err := svc.PublishDefinition(testTenantID, qualityDef.ID); err != nil {
		t.Fatalf("publish quality definition: %v", err)
	}
	qualityInst, err := svc.StartInstance(testTenantID, StartInstanceRequest{DefinitionID: qualityDef.ID, InitiatorID: "col-laochen"})
	if err != nil {
		t.Fatalf("start quality instance: %v", err)
	}

	officeDef, err := svc.CreateDefinition(testTenantID, CreateDefinitionRequest{Name: "Office workflow", Steps: []CreateStepDefRequest{{StepName: "Office archive", AssigneeMode: "fixed_colleague", AssigneeColleagueID: "col-xiaodi", AssigneeRoleCode: "office"}}})
	if err != nil {
		t.Fatalf("create office definition: %v", err)
	}
	if err := svc.PublishDefinition(testTenantID, officeDef.ID); err != nil {
		t.Fatalf("publish office definition: %v", err)
	}
	if _, err := svc.StartInstance(testTenantID, StartInstanceRequest{DefinitionID: officeDef.ID, InitiatorID: "col-laochen"}); err != nil {
		t.Fatalf("start office instance: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(svc).RegisterClientRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/client/workflow-instances?colleague_id=col-xiaozhou", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), testTenantID))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Instances []instDTO `json:"instances"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Instances) != 1 || resp.Instances[0].ID != qualityInst.ID {
		t.Fatalf("instances = %+v, want only assigned quality workflow %s", resp.Instances, qualityInst.ID)
	}
	if resp.Instances[0].CurrentStepAssigneeColleagueID != "col-xiaozhou" {
		t.Fatalf("current step assignee = %q, want col-xiaozhou", resp.Instances[0].CurrentStepAssigneeColleagueID)
	}
}
