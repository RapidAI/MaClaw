package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// RuntimeExecutor defines the interface for starting workflow instances at runtime.
// This extends the existing WorkflowExecutor with runtime-specific parameters
// (initiatorID, formData, channel) that are persisted alongside the instance.
type RuntimeExecutor interface {
	StartInstance(ctx context.Context, workflowID, initiatorID string, formData map[string]interface{}, channel string) (*WorkflowInstance, error)
}

// RuntimeAPI provides HTTP handlers for workflow runtime operations:
// initiation, withdrawal, confirmation, and directory queries.
type RuntimeAPI struct {
	executor           RuntimeExecutor
	instanceStore      InstanceStore
	auditStore         AuditStore
	formValidator      *FormValidator
	workflowStore      WorkflowStore
	withdrawalHandler  *WithdrawalHandler
	directoryService   *DirectoryService
}

// NewRuntimeAPI creates a new RuntimeAPI with the given dependencies.
func NewRuntimeAPI(
	executor RuntimeExecutor,
	instanceStore InstanceStore,
	auditStore AuditStore,
	formValidator *FormValidator,
	workflowStore WorkflowStore,
) *RuntimeAPI {
	return &RuntimeAPI{
		executor:      executor,
		instanceStore: instanceStore,
		auditStore:    auditStore,
		formValidator: formValidator,
		workflowStore: workflowStore,
	}
}

// SetWithdrawalHandler sets the withdrawal handler for the RuntimeAPI.
// This must be called before RegisterRoutes if withdrawal functionality is needed.
func (api *RuntimeAPI) SetWithdrawalHandler(wh *WithdrawalHandler) {
	api.withdrawalHandler = wh
}

// SetDirectoryService sets the directory service for the RuntimeAPI.
// This must be called before RegisterRoutes if directory functionality is needed.
func (api *RuntimeAPI) SetDirectoryService(ds *DirectoryService) {
	api.directoryService = ds
}

// RegisterRoutes registers runtime API routes on the given mux.
// The auth middleware extracts the authenticated user ID and sets it
// in the X-Owner-ID header.
func (api *RuntimeAPI) RegisterRoutes(mux *http.ServeMux, auth func(http.HandlerFunc) http.HandlerFunc) {
	// Initiation
	mux.HandleFunc("POST /api/v1/workflows/{id}/initiate", auth(api.handleInitiateWorkflow))

	// Withdrawal
	mux.HandleFunc("POST /api/v1/instances/{id}/withdraw", auth(api.handleWithdrawInstance))

	// Confirmations
	mux.HandleFunc("POST /api/v1/confirmations/{id}/confirm", auth(api.handleConfirm))
	mux.HandleFunc("GET /api/v1/confirmations/pending", auth(api.handleListPendingConfirmations))

	// Directory views
	mux.HandleFunc("GET /api/v1/directory/initiated", auth(api.handleMyInitiated))
	mux.HandleFunc("GET /api/v1/directory/pending-action", auth(api.handlePendingMyAction))
	mux.HandleFunc("GET /api/v1/directory/pending-confirmation", auth(api.handlePendingMyConfirmation))
	mux.HandleFunc("GET /api/v1/directory/completed", auth(api.handleCompleted))
}

// --- Request/Response types for Initiation ---

// InitiateWorkflowRequest is the payload for creating a new workflow instance.
type InitiateWorkflowRequest struct {
	FormData map[string]interface{} `json:"form_data"`
	Channel  string                 `json:"channel,omitempty"` // "hub_page", "im_feishu", "im_wechat", "api"
}

// InitiateWorkflowResponse is returned on successful instance creation.
type InitiateWorkflowResponse struct {
	InstanceID string    `json:"instance_id"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	VersionID  string    `json:"version_id"`
}

// --- Handler: handleInitiateWorkflow ---

// handleInitiateWorkflow creates a new workflow instance from submitted form data.
//
// Flow:
//  1. Extract workflow ID from URL path parameter {id}
//  2. Extract initiator_id from auth context (X-Owner-ID header)
//  3. Parse request body as InitiateWorkflowRequest
//  4. Load the workflow's published version to get the form schema
//  5. Validate form_data against the schema using FormValidator
//  6. Call RuntimeExecutor.StartInstance to create the instance
//  7. Return 201 with InitiateWorkflowResponse
func (api *RuntimeAPI) handleInitiateWorkflow(w http.ResponseWriter, r *http.Request) {
	// 1. Extract workflow ID from URL path
	workflowID := r.PathValue("id")
	if workflowID == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "workflow id is required")
		return
	}

	// 2. Extract initiator_id from auth context
	initiatorID := getUserIDFromContext(r)
	if initiatorID == "" {
		apiWriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	// 3. Parse request body
	var req InitiateWorkflowRequest
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber() // preserve number precision for form validation
	if err := decoder.Decode(&req); err != nil {
		apiWriteError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body: "+err.Error())
		return
	}

	if req.FormData == nil {
		req.FormData = make(map[string]interface{})
	}

	// Default channel to "api" if not specified
	if req.Channel == "" {
		req.Channel = "api"
	}

	// 4. Load the workflow's published version to get the form schema
	ver, err := api.workflowStore.GetPublishedVersion(r.Context(), workflowID)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "LOAD_FAILED", "failed to load workflow: "+err.Error())
		return
	}
	if ver == nil {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "no published version found for workflow")
		return
	}

	// Extract form schema from the workflow graph
	schema, err := ExtractFormSchema(&ver.Graph)
	if err != nil {
		// If no form node exists, allow initiation with empty schema (no validation)
		schema = nil
	}

	// 5. Validate form_data against the schema
	if schema != nil && api.formValidator != nil {
		validationErrors := api.formValidator.Validate(req.FormData, schema)
		if len(validationErrors) > 0 {
			apiWriteJSON(w, http.StatusBadRequest, map[string]interface{}{
				"ok":     false,
				"code":   "VALIDATION_FAILED",
				"errors": validationErrors,
			})
			return
		}
	}

	// 6. Call RuntimeExecutor.StartInstance to create the instance
	// The executor is responsible for persisting the complete Form_Data
	// (including initiator_id, submission_timestamp UTC ms, version_id, channel)
	inst, err := api.executor.StartInstance(r.Context(), workflowID, initiatorID, req.FormData, req.Channel)
	if err != nil {
		// Distinguish known errors
		switch {
		case err == ErrNoPublishedVersion:
			apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "no published version for workflow")
		default:
			apiWriteError(w, http.StatusInternalServerError, "START_FAILED",
				fmt.Sprintf("failed to create workflow instance: %s", err.Error()))
		}
		return
	}

	// 7. Return 201 with InitiateWorkflowResponse
	resp := InitiateWorkflowResponse{
		InstanceID: inst.ID,
		Status:     string(inst.Status),
		CreatedAt:  inst.CreatedAt,
		VersionID:  inst.VersionID,
	}

	apiWriteJSON(w, http.StatusCreated, resp)
}

// --- Placeholder handlers for other routes (to be implemented in subsequent tasks) ---

func (api *RuntimeAPI) handleWithdrawInstance(w http.ResponseWriter, r *http.Request) {
	// 1. Extract instance_id from URL path parameter {id}.
	instanceID := r.PathValue("id")
	if instanceID == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "instance id is required")
		return
	}

	// 2. Extract authenticated user_id from request context.
	userID := getUserIDFromContext(r)
	if userID == "" {
		apiWriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	// 3. Check that withdrawal handler is configured.
	if api.withdrawalHandler == nil {
		apiWriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "withdrawal handler not configured")
		return
	}

	// 4. Call WithdrawalHandler.Withdraw.
	err := api.withdrawalHandler.Withdraw(r.Context(), instanceID, userID)
	if err == nil {
		// 5. Success → HTTP 200 with instance_id and status.
		apiWriteJSON(w, http.StatusOK, map[string]interface{}{
			"ok":          true,
			"instance_id": instanceID,
			"status":      "withdrawn",
		})
		return
	}

	// 6. Handle specific errors with appropriate HTTP status codes.
	switch {
	case errors.Is(err, ErrNotInitiator):
		apiWriteJSON(w, http.StatusForbidden, map[string]interface{}{
			"error": "only the initiator can withdraw",
		})
	case errors.Is(err, ErrAlreadyCompleted):
		apiWriteJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "instance has already completed",
		})
	case errors.Is(err, ErrAlreadyWithdrawn):
		apiWriteJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "instance has already been withdrawn",
		})
	case errors.Is(err, ErrInstanceNotFound):
		apiWriteJSON(w, http.StatusNotFound, map[string]interface{}{
			"error": "instance not found",
		})
	case errors.Is(err, ErrInstanceNotRunning):
		apiWriteJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "instance is not in running status",
		})
	default:
		apiWriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
	}
}

func (api *RuntimeAPI) handleConfirm(w http.ResponseWriter, r *http.Request) {
	apiWriteError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "confirmation not yet implemented")
}

func (api *RuntimeAPI) handleListPendingConfirmations(w http.ResponseWriter, r *http.Request) {
	apiWriteError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "pending confirmations not yet implemented")
}

func (api *RuntimeAPI) handleMyInitiated(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromContext(r)
	if userID == "" {
		apiWriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	if api.directoryService == nil {
		apiWriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "directory service not configured")
		return
	}

	filter := parseDirectoryFilter(r)
	items, total, err := api.directoryService.MyInitiated(r.Context(), userID, filter)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "QUERY_FAILED", "failed to query directory: "+err.Error())
		return
	}

	if items == nil {
		items = []DirectoryItem{}
	}
	apiWriteJSON(w, http.StatusOK, DirectoryResponse{
		Items:    items,
		Total:    total,
		Page:     filter.Page,
		PageSize: filter.PageSize,
	})
}

func (api *RuntimeAPI) handlePendingMyAction(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromContext(r)
	if userID == "" {
		apiWriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	if api.directoryService == nil {
		apiWriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "directory service not configured")
		return
	}

	filter := parseDirectoryFilter(r)
	items, err := api.directoryService.PendingMyAction(r.Context(), userID)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "QUERY_FAILED", "failed to query directory: "+err.Error())
		return
	}

	if items == nil {
		items = []DirectoryItem{}
	}

	// Apply pagination in-memory since PendingMyAction returns all items sorted by urgency.
	total := len(items)
	offset := (filter.Page - 1) * filter.PageSize
	end := offset + filter.PageSize
	if offset >= total {
		items = []DirectoryItem{}
	} else {
		if end > total {
			end = total
		}
		items = items[offset:end]
	}

	apiWriteJSON(w, http.StatusOK, DirectoryResponse{
		Items:    items,
		Total:    total,
		Page:     filter.Page,
		PageSize: filter.PageSize,
	})
}

func (api *RuntimeAPI) handlePendingMyConfirmation(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromContext(r)
	if userID == "" {
		apiWriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	if api.directoryService == nil {
		apiWriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "directory service not configured")
		return
	}

	filter := parseDirectoryFilter(r)
	items, total, err := api.directoryService.PendingMyConfirmation(r.Context(), userID, filter)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "QUERY_FAILED", "failed to query directory: "+err.Error())
		return
	}

	if items == nil {
		items = []DirectoryItem{}
	}
	apiWriteJSON(w, http.StatusOK, DirectoryResponse{
		Items:    items,
		Total:    total,
		Page:     filter.Page,
		PageSize: filter.PageSize,
	})
}

func (api *RuntimeAPI) handleCompleted(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromContext(r)
	if userID == "" {
		apiWriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	if api.directoryService == nil {
		apiWriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "directory service not configured")
		return
	}

	filter := parseDirectoryFilter(r)
	items, total, err := api.directoryService.Completed(r.Context(), userID, filter)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "QUERY_FAILED", "failed to query directory: "+err.Error())
		return
	}

	if items == nil {
		items = []DirectoryItem{}
	}
	apiWriteJSON(w, http.StatusOK, DirectoryResponse{
		Items:    items,
		Total:    total,
		Page:     filter.Page,
		PageSize: filter.PageSize,
	})
}

// parseDirectoryFilter extracts DirectoryFilter from HTTP query parameters.
func parseDirectoryFilter(r *http.Request) DirectoryFilter {
	q := r.URL.Query()

	filter := DirectoryFilter{
		Status:       q.Get("status"),
		WorkflowType: q.Get("workflow_type"),
		Role:         q.Get("role"),
		Result:       q.Get("result"),
	}

	// Parse date_from as RFC3339
	if dateFrom := q.Get("date_from"); dateFrom != "" {
		if t, err := time.Parse(time.RFC3339, dateFrom); err == nil {
			filter.DateFrom = &t
		}
	}

	// Parse date_to as RFC3339
	if dateTo := q.Get("date_to"); dateTo != "" {
		if t, err := time.Parse(time.RFC3339, dateTo); err == nil {
			filter.DateTo = &t
		}
	}

	// Parse page (default 1)
	if pageStr := q.Get("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			filter.Page = p
		}
	}
	if filter.Page < 1 {
		filter.Page = 1
	}

	// Parse page_size (default 20, max 100)
	if pageSizeStr := q.Get("page_size"); pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 {
			filter.PageSize = ps
		}
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}

	return filter
}

// Note: getUserIDFromContext is defined in api_middleware.go and extracts
// the authenticated user ID from the request context (set by AuthMiddleware).
