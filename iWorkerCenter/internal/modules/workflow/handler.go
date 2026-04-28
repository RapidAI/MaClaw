package workflow

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

// Handler exposes HTTP endpoints for workflow management and runtime.
type Handler struct {
	svc      *Service
	designer *Designer
}

// NewHandler creates a Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// SetDesigner sets the optional AI workflow designer.
func (h *Handler) SetDesigner(d *Designer) {
	h.designer = d
}

// RegisterAdminRoutes registers admin-facing routes.
func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/workflows", h.handleDefinitions)
	mux.HandleFunc("/admin/workflows/", h.handleDefinitionByID)
	mux.HandleFunc("/admin/workflow-design", h.handleDesign)
	mux.HandleFunc("/admin/workflow-instances", h.handleInstances)
	mux.HandleFunc("/admin/workflow-instances/", h.handleInstanceByID)
}

// RegisterRuntimeRoutes registers runtime routes for workflow execution.
func (h *Handler) RegisterRuntimeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/runtime/workflows/start", h.handleStart)
	mux.HandleFunc("/runtime/workflows/steps/", h.handleStepAction)
}

// RegisterClientRoutes registers client-facing routes.
func (h *Handler) RegisterClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/client/workflows", h.handleClientWorkflows)
	mux.HandleFunc("/client/workflow-instances", h.handleClientInstances)
}

// --- Definition endpoints ---

func (h *Handler) handleDefinitions(w http.ResponseWriter, r *http.Request) {
	tid := tenant.RequestTenantID(r)
	switch r.Method {
	case http.MethodGet:
		defs, err := h.svc.ListDefinitions(tid)
		if err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]any{"workflows": toDefDTOs(defs)})
	case http.MethodPost:
		var req CreateDefinitionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.BadRequest(w, "INVALID_BODY", "invalid JSON")
			return
		}
		def, err := h.svc.CreateDefinition(tid, req)
		if err != nil {
			response.BadRequest(w, "CREATE_FAILED", err.Error())
			return
		}
		response.Created(w, toDefDTO(def))
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *Handler) handleDefinitionByID(w http.ResponseWriter, r *http.Request) {
	tid := tenant.RequestTenantID(r)
	rest := strings.TrimPrefix(r.URL.Path, "/admin/workflows/")
	rest = strings.TrimRight(rest, "/")
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		response.BadRequest(w, "MISSING_ID", "workflow id required")
		return
	}

	// /admin/workflows/{id}/publish
	if len(parts) == 2 && parts[1] == "publish" && r.Method == http.MethodPost {
		if err := h.svc.PublishDefinition(tid, id); err != nil {
			response.BadRequest(w, "PUBLISH_FAILED", err.Error())
			return
		}
		response.OK(w, map[string]string{"status": "published"})
		return
	}

	// /admin/workflows/{id}/steps
	if len(parts) == 2 && parts[1] == "steps" && r.Method == http.MethodGet {
		steps, err := h.svc.ListStepDefinitions(tid, id)
		if err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]any{"steps": toStepDefDTOs(steps)})
		return
	}

	// /admin/workflows/{id}
	if r.Method == http.MethodGet {
		def, err := h.svc.GetDefinition(tid, id)
		if err != nil {
			response.NotFound(w, "NOT_FOUND", "workflow not found")
			return
		}
		steps, _ := h.svc.ListStepDefinitions(tid, id)
		dto := toDefDTO(def)
		response.OK(w, map[string]any{"workflow": dto, "steps": toStepDefDTOs(steps)})
		return
	}

	response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
}

// --- Instance endpoints ---

func (h *Handler) handleInstances(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	tid := tenant.RequestTenantID(r)
	instances, err := h.svc.ListInstances(tid)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]any{"instances": toInstDTOs(instances)})
}

func (h *Handler) handleInstanceByID(w http.ResponseWriter, r *http.Request) {
	tid := tenant.RequestTenantID(r)
	rest := strings.TrimPrefix(r.URL.Path, "/admin/workflow-instances/")
	rest = strings.TrimRight(rest, "/")
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]

	// /admin/workflow-instances/{id}/steps
	if len(parts) == 2 && parts[1] == "steps" {
		steps, err := h.svc.ListStepInstances(tid, id)
		if err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]any{"steps": toStepInstDTOs(steps)})
		return
	}

	// /admin/workflow-instances/{id}/events
	if len(parts) == 2 && parts[1] == "events" {
		events, err := h.svc.ListEvents(tid, id)
		if err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]any{"events": toEventDTOs(events)})
		return
	}

	inst, err := h.svc.GetInstance(tid, id)
	if err != nil {
		response.NotFound(w, "NOT_FOUND", "instance not found")
		return
	}
	response.OK(w, toInstDTO(inst))
}

// --- Runtime endpoints ---

func (h *Handler) handleDesign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	if h.designer == nil {
		response.Error(w, http.StatusServiceUnavailable, "DESIGNER_NOT_AVAILABLE", "AI workflow designer is not configured")
		return
	}
	tid := tenant.RequestTenantID(r)
	var req DesignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	result, err := h.designer.Design(tid, req)
	if err != nil {
		response.BadRequest(w, "DESIGN_FAILED", err.Error())
		return
	}
	response.Created(w, map[string]any{
		"workflow":  toDefDTO(result.Definition),
		"steps":     toStepDefDTOs(result.Steps),
		"published": result.Published,
	})
}

func (h *Handler) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	tid := tenant.RequestTenantID(r)
	var req StartInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	inst, err := h.svc.StartInstance(tid, req)
	if err != nil {
		response.BadRequest(w, "START_FAILED", err.Error())
		return
	}
	response.Created(w, toInstDTO(inst))
}

func (h *Handler) handleStepAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	tid := tenant.RequestTenantID(r)
	// /runtime/workflows/steps/{id}/{action}
	rest := strings.TrimPrefix(r.URL.Path, "/runtime/workflows/steps/")
	rest = strings.TrimRight(rest, "/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) < 2 {
		response.BadRequest(w, "INVALID_PATH", "expected /runtime/workflows/steps/{id}/{action}")
		return
	}
	stepID, action := parts[0], parts[1]

	var body struct {
		ActorID string `json:"actor_id"`
		Result  string `json:"result"`
		Note    string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	switch action {
	case "complete":
		if err := h.svc.CompleteStep(tid, stepID, body.ActorID, body.Result); err != nil {
			response.BadRequest(w, "COMPLETE_FAILED", err.Error())
			return
		}
	case "reject":
		if err := h.svc.RejectStep(tid, stepID, body.ActorID, body.Note); err != nil {
			response.BadRequest(w, "REJECT_FAILED", err.Error())
			return
		}
	default:
		response.BadRequest(w, "INVALID_ACTION", "valid actions: complete, reject")
		return
	}
	response.OK(w, map[string]string{"status": "ok"})
}

// --- Client endpoints ---

func (h *Handler) handleClientWorkflows(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	tid := tenant.RequestTenantID(r)
	defs, err := h.svc.ListDefinitions(tid)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	// Only return published workflows to clients
	var published []*Definition
	for _, d := range defs {
		if d.Status == DefStatusPublished {
			published = append(published, d)
		}
	}
	response.OK(w, map[string]any{"workflows": toDefDTOs(published)})
}

func (h *Handler) handleClientInstances(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	tid := tenant.RequestTenantID(r)
	instances, err := h.svc.ListInstances(tid)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]any{"instances": toInstDTOs(instances)})
}

// --- DTOs ---

type defDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	TriggerType string `json:"trigger_type"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type stepDefDTO struct {
	ID                  string `json:"id"`
	StepCode            string `json:"step_code"`
	StepName            string `json:"step_name"`
	StepType            string `json:"step_type"`
	AssigneeMode        string `json:"assignee_mode"`
	AssigneeRoleCode    string `json:"assignee_role_code"`
	AssigneeColleagueID string `json:"assignee_colleague_id"`
	TimeoutMinutes      int    `json:"timeout_minutes"`
	RejectRule          string `json:"reject_rule"`
	SortOrder           int    `json:"sort_order"`
}

type instDTO struct {
	ID            string `json:"id"`
	DefinitionID  string `json:"definition_id"`
	Title         string `json:"title"`
	InitiatorID   string `json:"initiator_id"`
	CurrentStepID string `json:"current_step_id"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type stepInstDTO struct {
	ID                  string `json:"id"`
	StepDefinitionID    string `json:"step_definition_id"`
	AssigneeColleagueID string `json:"assignee_colleague_id"`
	CollaborationTaskID string `json:"collaboration_task_id"`
	Status              string `json:"status"`
	Result              string `json:"result"`
	SortOrder           int    `json:"sort_order"`
	CreatedAt           string `json:"created_at"`
}

type eventDTO struct {
	ID        string `json:"id"`
	StepID    string `json:"step_id"`
	Event     string `json:"event"`
	ActorID   string `json:"actor_id"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
}

func toDefDTO(d *Definition) defDTO {
	return defDTO{ID: d.ID, Name: d.Name, Description: d.Description,
		TriggerType: d.TriggerType, Status: d.Status,
		CreatedAt: d.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt: d.UpdatedAt.Format("2006-01-02T15:04:05Z")}
}

func toDefDTOs(defs []*Definition) []defDTO {
	out := make([]defDTO, 0, len(defs))
	for _, d := range defs {
		out = append(out, toDefDTO(d))
	}
	return out
}

func toStepDefDTOs(steps []*StepDefinition) []stepDefDTO {
	out := make([]stepDefDTO, 0, len(steps))
	for _, s := range steps {
		out = append(out, stepDefDTO{
			ID: s.ID, StepCode: s.StepCode, StepName: s.StepName, StepType: s.StepType,
			AssigneeMode: s.AssigneeMode, AssigneeRoleCode: s.AssigneeRoleCode,
			AssigneeColleagueID: s.AssigneeColleagueID,
			TimeoutMinutes:      s.TimeoutMinutes, RejectRule: s.RejectRule, SortOrder: s.SortOrder,
		})
	}
	return out
}

func toInstDTO(inst *Instance) instDTO {
	return instDTO{ID: inst.ID, DefinitionID: inst.DefinitionID, Title: inst.Title,
		InitiatorID: inst.InitiatorID, CurrentStepID: inst.CurrentStepID, Status: inst.Status,
		CreatedAt: inst.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt: inst.UpdatedAt.Format("2006-01-02T15:04:05Z")}
}

func toInstDTOs(instances []*Instance) []instDTO {
	out := make([]instDTO, 0, len(instances))
	for _, inst := range instances {
		out = append(out, toInstDTO(inst))
	}
	return out
}

func toStepInstDTOs(steps []*StepInstance) []stepInstDTO {
	out := make([]stepInstDTO, 0, len(steps))
	for _, s := range steps {
		out = append(out, stepInstDTO{
			ID: s.ID, StepDefinitionID: s.StepDefinitionID,
			AssigneeColleagueID: s.AssigneeColleagueID,
			CollaborationTaskID: s.CollaborationTaskID,
			Status:              s.Status, Result: s.Result, SortOrder: s.SortOrder,
			CreatedAt: s.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	return out
}

func toEventDTOs(events []*InstanceEvent) []eventDTO {
	out := make([]eventDTO, 0, len(events))
	for _, e := range events {
		out = append(out, eventDTO{
			ID: e.ID, StepID: e.StepID, Event: e.Event,
			ActorID: e.ActorID, Note: e.Note,
			CreatedAt: e.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	return out
}
