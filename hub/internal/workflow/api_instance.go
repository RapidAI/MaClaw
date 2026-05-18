package workflow

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

// InstanceAPI provides HTTP handlers for workflow instance operations.
// It exposes endpoints to trigger new instances, query instance status,
// and retrieve instance audit trails.
type InstanceAPI struct {
	executor      *WorkflowExecutor
	instanceStore InstanceStore
	auditStore    AuditStore
}

// NewInstanceAPI creates a new InstanceAPI with the given dependencies.
func NewInstanceAPI(executor *WorkflowExecutor, instanceStore InstanceStore, auditStore AuditStore) *InstanceAPI {
	return &InstanceAPI{
		executor:      executor,
		instanceStore: instanceStore,
		auditStore:    auditStore,
	}
}

// RegisterRoutes registers all workflow instance API routes on the given mux.
// The authMiddleware extracts the user ID from the request and sets it
// in the X-Owner-ID header.
func (api *InstanceAPI) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("POST /api/v1/workflows/{id}/trigger", authMiddleware(api.handleTriggerWorkflow))
	mux.HandleFunc("GET /api/v1/instances/{id}", authMiddleware(api.handleGetInstance))
	mux.HandleFunc("GET /api/v1/instances/{id}/audit", authMiddleware(api.handleGetInstanceAudit))
}

// --- Request types ---

type triggerWorkflowRequest struct {
	TriggerData json.RawMessage `json:"trigger_data,omitempty"`
}

// --- Handlers ---

// handleTriggerWorkflow starts a new workflow instance by triggering a published workflow.
//
// POST /api/v1/workflows/:id/trigger
// Body: {"trigger_data": {...}} (optional)
//
// The user ID is extracted from the X-Owner-ID header (set by auth middleware).
// Calls WorkflowExecutor.TriggerFromMarket(workflowID, userID, triggerData).
func (api *InstanceAPI) handleTriggerWorkflow(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-Owner-ID")
	if userID == "" {
		apiWriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user identification required")
		return
	}

	workflowID := r.PathValue("id")
	if workflowID == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "workflow id is required")
		return
	}

	var req triggerWorkflowRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apiWriteError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body: "+err.Error())
			return
		}
	}

	triggerData := ""
	if len(req.TriggerData) > 0 {
		triggerData = string(req.TriggerData)
	}

	instance, err := api.executor.TriggerFromMarket(r.Context(), workflowID, userID, triggerData)
	if err != nil {
		switch {
		case errors.Is(err, ErrMissingUserID):
			apiWriteError(w, http.StatusBadRequest, "MISSING_USER_ID", err.Error())
		case errors.Is(err, ErrWorkflowNotPublished), errors.Is(err, ErrNoPublishedVersion):
			apiWriteError(w, http.StatusConflict, "NOT_PUBLISHED", "workflow does not have a published version")
		case errors.Is(err, ErrWorkflowNotFound):
			apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "workflow not found")
		default:
			apiWriteError(w, http.StatusInternalServerError, "TRIGGER_FAILED", "failed to trigger workflow: "+err.Error())
		}
		return
	}

	apiWriteJSON(w, http.StatusCreated, map[string]any{
		"instance": instance,
	})
}

// handleGetInstance returns the current status and details of a workflow instance.
//
// GET /api/v1/instances/:id
//
// Returns the instance with status, current node, trigger data, timestamps, etc.
func (api *InstanceAPI) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-Owner-ID")
	if userID == "" {
		apiWriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user identification required")
		return
	}

	instanceID := r.PathValue("id")
	if instanceID == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "instance id is required")
		return
	}

	instance, err := api.instanceStore.Get(r.Context(), instanceID)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "GET_FAILED", "failed to get instance: "+err.Error())
		return
	}
	if instance == nil {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "instance not found")
		return
	}

	// Verify the requesting user has access to this instance.
	requesterID, _ := instance.InstanceData["requester_id"].(string)
	if requesterID != "" && requesterID != userID {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "instance not found")
		return
	}

	apiWriteJSON(w, http.StatusOK, instance)
}

// handleGetInstanceAudit returns the paginated audit trail for a workflow instance.
//
// GET /api/v1/instances/:id/audit?page=1&page_size=100
//
// Returns the chronological sequence of audit events for the instance,
// paginated at 100 records per page (default and maximum).
func (api *InstanceAPI) handleGetInstanceAudit(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-Owner-ID")
	if userID == "" {
		apiWriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user identification required")
		return
	}

	instanceID := r.PathValue("id")
	if instanceID == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "instance id is required")
		return
	}

	// Verify the requesting user has access to this instance.
	instance, err := api.instanceStore.Get(r.Context(), instanceID)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "GET_FAILED", "failed to get instance: "+err.Error())
		return
	}
	if instance == nil {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "instance not found")
		return
	}
	requesterID, _ := instance.InstanceData["requester_id"].(string)
	if requesterID != "" && requesterID != userID {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "instance not found")
		return
	}

	// Parse pagination parameters.
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	pageSize := DefaultAuditPageSize // 100
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 {
			pageSize = parsed
		}
	}
	// Cap page size at DefaultAuditPageSize (100).
	if pageSize > DefaultAuditPageSize {
		pageSize = DefaultAuditPageSize
	}

	entries, total, err := api.auditStore.QueryByInstance(r.Context(), instanceID, page, pageSize)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "QUERY_FAILED", "failed to query audit trail: "+err.Error())
		return
	}

	if entries == nil {
		entries = []AuditEntry{}
	}

	apiWriteJSON(w, http.StatusOK, map[string]any{
		"entries":   entries,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}
