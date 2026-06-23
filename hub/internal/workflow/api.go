package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// WorkflowAPI provides HTTP handlers for workflow CRUD operations.
// It enforces owner isolation: users can only access their own workflows.
type WorkflowAPI struct {
	store          WorkflowStore
	versionManager *VersionManager
}

// NewWorkflowAPI creates a new WorkflowAPI with the given store and version manager.
func NewWorkflowAPI(store WorkflowStore, vm *VersionManager) *WorkflowAPI {
	return &WorkflowAPI{
		store:          store,
		versionManager: vm,
	}
}

// RegisterRoutes registers all workflow API routes on the given mux.
// The authMiddleware extracts the owner ID from the request and sets it
// in the X-Owner-ID header. If authentication fails, it writes an error
// response and returns false.
func (api *WorkflowAPI) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("POST /api/v1/workflows", authMiddleware(api.handleCreateWorkflow))
	mux.HandleFunc("GET /api/v1/workflows", authMiddleware(api.handleListWorkflows))
	mux.HandleFunc("GET /api/v1/workflows/{id}", authMiddleware(api.handleGetWorkflow))
	mux.HandleFunc("PUT /api/v1/workflows/{id}", authMiddleware(api.handleUpdateWorkflow))
	mux.HandleFunc("DELETE /api/v1/workflows/{id}", authMiddleware(api.handleDeleteWorkflow))

	mux.HandleFunc("POST /api/v1/workflows/{id}/versions", authMiddleware(api.handleCreateVersion))
	mux.HandleFunc("GET /api/v1/workflows/{id}/versions", authMiddleware(api.handleListVersions))

	mux.HandleFunc("POST /api/v1/workflows/{id}/versions/{vid}/submit", authMiddleware(api.handleSubmitForReview))
	mux.HandleFunc("POST /api/v1/workflows/{id}/versions/{vid}/validate", authMiddleware(api.handleValidateVersion))
}

// --- Request/Response types ---

type createWorkflowRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type updateWorkflowRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type createVersionRequest struct {
	Graph WorkflowGraph `json:"graph"`
}

// --- Handlers ---

func (api *WorkflowAPI) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	ownerID := r.Header.Get("X-Owner-ID")
	if ownerID == "" {
		apiWriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "owner identification required")
		return
	}

	var req createWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apiWriteError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body: "+err.Error())
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "name is required")
		return
	}

	now := time.Now().UTC()
	def := &WorkflowDefinition{
		ID:          generateID("wf"),
		OwnerID:     ownerID,
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := api.store.CreateWorkflow(r.Context(), def); err != nil {
		apiWriteError(w, http.StatusInternalServerError, "CREATE_FAILED", "failed to create workflow: "+err.Error())
		return
	}

	apiWriteJSON(w, http.StatusCreated, def)
}

func (api *WorkflowAPI) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	ownerID := r.Header.Get("X-Owner-ID")
	if ownerID == "" {
		apiWriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "owner identification required")
		return
	}

	workflows, err := api.store.ListWorkflows(r.Context(), ownerID)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "LIST_FAILED", "failed to list workflows: "+err.Error())
		return
	}

	if workflows == nil {
		workflows = []WorkflowDefinition{}
	}

	apiWriteJSON(w, http.StatusOK, map[string]any{
		"workflows": workflows,
		"total":     len(workflows),
	})
}

func (api *WorkflowAPI) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	ownerID := r.Header.Get("X-Owner-ID")
	if ownerID == "" {
		apiWriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "owner identification required")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "workflow id is required")
		return
	}

	def, err := api.store.GetWorkflow(r.Context(), id)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "GET_FAILED", "failed to get workflow: "+err.Error())
		return
	}
	if def == nil {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "workflow not found")
		return
	}

	// Owner isolation: only the owner can access their own workflows
	if def.OwnerID != ownerID {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "workflow not found")
		return
	}

	apiWriteJSON(w, http.StatusOK, def)
}

func (api *WorkflowAPI) handleUpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	ownerID := r.Header.Get("X-Owner-ID")
	if ownerID == "" {
		apiWriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "owner identification required")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "workflow id is required")
		return
	}

	def, err := api.store.GetWorkflow(r.Context(), id)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "GET_FAILED", "failed to get workflow: "+err.Error())
		return
	}
	if def == nil {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "workflow not found")
		return
	}

	// Owner isolation
	if def.OwnerID != ownerID {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "workflow not found")
		return
	}

	var req updateWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apiWriteError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body: "+err.Error())
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "name is required")
		return
	}

	def.Name = name
	def.Description = strings.TrimSpace(req.Description)
	def.UpdatedAt = time.Now().UTC()

	// The WorkflowStore interface doesn't have an UpdateWorkflow method,
	// so we use CreateWorkflow which in the PG implementation does an upsert
	// or we need to add one. For now, we'll use the store's CreateWorkflow
	// which should handle the update case via the existing ID.
	// Actually, looking at the store interface, there's no Update method.
	// We'll need to work with what we have. The PG store likely does INSERT
	// and would fail on duplicate ID. Let's add an UpdateWorkflow to the interface.
	// But since we shouldn't modify existing interfaces without checking,
	// let's use a pattern that works: delete + recreate, or just update fields
	// that the store supports.
	//
	// Looking at the existing code, the store only has CreateWorkflow.
	// For the API layer, we'll document that UpdateWorkflow needs to be added
	// to the store interface. For now, we implement the handler assuming
	// the store will support it.
	if updater, ok := api.store.(WorkflowUpdater); ok {
		if err := updater.UpdateWorkflow(r.Context(), def); err != nil {
			apiWriteError(w, http.StatusInternalServerError, "UPDATE_FAILED", "failed to update workflow: "+err.Error())
			return
		}
	} else {
		apiWriteError(w, http.StatusInternalServerError, "UPDATE_FAILED", "workflow update not supported by store")
		return
	}

	apiWriteJSON(w, http.StatusOK, def)
}

func (api *WorkflowAPI) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	ownerID := r.Header.Get("X-Owner-ID")
	if ownerID == "" {
		apiWriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "owner identification required")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "workflow id is required")
		return
	}

	def, err := api.store.GetWorkflow(r.Context(), id)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "GET_FAILED", "failed to get workflow: "+err.Error())
		return
	}
	if def == nil {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "workflow not found")
		return
	}

	// Owner isolation
	if def.OwnerID != ownerID {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "workflow not found")
		return
	}

	versions, err := api.store.ListVersions(r.Context(), id)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "LIST_VERSIONS_FAILED", "failed to list workflow versions: "+err.Error())
		return
	}
	for _, ver := range versions {
		if workflowVersionBlocksDesignerDelete(ver.Status) {
			apiWriteError(w, http.StatusConflict, "WORKFLOW_PUBLISHED", "published or previously published workflow designs cannot be deleted from the designer")
			return
		}
	}

	if deleter, ok := api.store.(WorkflowDeleter); ok {
		if err := deleter.DeleteWorkflow(r.Context(), id); err != nil {
			apiWriteError(w, http.StatusInternalServerError, "DELETE_FAILED", "failed to delete workflow: "+err.Error())
			return
		}
	} else {
		apiWriteError(w, http.StatusInternalServerError, "DELETE_FAILED", "workflow deletion not supported by store")
		return
	}

	apiWriteJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

func workflowVersionBlocksDesignerDelete(status VersionStatus) bool {
	switch status {
	case VersionPublished, VersionSuperseded, VersionUnpublished:
		return true
	default:
		return false
	}
}

func (api *WorkflowAPI) handleCreateVersion(w http.ResponseWriter, r *http.Request) {
	ownerID := r.Header.Get("X-Owner-ID")
	if ownerID == "" {
		apiWriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "owner identification required")
		return
	}

	workflowID := r.PathValue("id")
	if workflowID == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "workflow id is required")
		return
	}

	// Verify ownership
	def, err := api.store.GetWorkflow(r.Context(), workflowID)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "GET_FAILED", "failed to get workflow: "+err.Error())
		return
	}
	if def == nil {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "workflow not found")
		return
	}
	if def.OwnerID != ownerID {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "workflow not found")
		return
	}

	var req createVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apiWriteError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body: "+err.Error())
		return
	}

	ver, err := api.versionManager.SaveDraft(r.Context(), workflowID, req.Graph)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "CREATE_VERSION_FAILED", "failed to create version: "+err.Error())
		return
	}

	apiWriteJSON(w, http.StatusCreated, ver)
}

func (api *WorkflowAPI) handleListVersions(w http.ResponseWriter, r *http.Request) {
	ownerID := r.Header.Get("X-Owner-ID")
	if ownerID == "" {
		apiWriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "owner identification required")
		return
	}

	workflowID := r.PathValue("id")
	if workflowID == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "workflow id is required")
		return
	}

	// Verify ownership
	def, err := api.store.GetWorkflow(r.Context(), workflowID)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "GET_FAILED", "failed to get workflow: "+err.Error())
		return
	}
	if def == nil {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "workflow not found")
		return
	}
	if def.OwnerID != ownerID {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "workflow not found")
		return
	}

	versions, err := api.store.ListVersions(r.Context(), workflowID)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "LIST_FAILED", "failed to list versions: "+err.Error())
		return
	}

	if versions == nil {
		versions = []WorkflowVersion{}
	}

	apiWriteJSON(w, http.StatusOK, map[string]any{
		"versions": versions,
		"total":    len(versions),
	})
}

func (api *WorkflowAPI) handleSubmitForReview(w http.ResponseWriter, r *http.Request) {
	ownerID := r.Header.Get("X-Owner-ID")
	if ownerID == "" {
		apiWriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "owner identification required")
		return
	}

	workflowID := r.PathValue("id")
	versionID := r.PathValue("vid")
	if workflowID == "" || versionID == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "workflow id and version id are required")
		return
	}

	// Verify ownership
	def, err := api.store.GetWorkflow(r.Context(), workflowID)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "GET_FAILED", "failed to get workflow: "+err.Error())
		return
	}
	if def == nil {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "workflow not found")
		return
	}
	if def.OwnerID != ownerID {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "workflow not found")
		return
	}

	// Verify the version belongs to this workflow
	ver, err := api.store.GetVersion(r.Context(), versionID)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "GET_FAILED", "failed to get version: "+err.Error())
		return
	}
	if ver == nil || ver.WorkflowID != workflowID {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "version not found")
		return
	}

	if err := api.versionManager.SubmitForReview(r.Context(), versionID); err != nil {
		switch {
		case errors.Is(err, ErrVersionNotDraft):
			apiWriteError(w, http.StatusConflict, "NOT_DRAFT", "version is not in draft status")
		case isGraphValidationError(err):
			apiWriteError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error())
		default:
			apiWriteError(w, http.StatusInternalServerError, "SUBMIT_FAILED", "failed to submit for review: "+err.Error())
		}
		return
	}

	// Reload the version to return updated status
	ver, _ = api.store.GetVersion(r.Context(), versionID)
	apiWriteJSON(w, http.StatusOK, map[string]any{
		"submitted": true,
		"version":   ver,
	})
}

func isGraphValidationError(err error) bool {
	return errors.Is(err, ErrNoNodes) ||
		errors.Is(err, ErrNoTriggerNode) ||
		errors.Is(err, ErrMultipleTriggers) ||
		errors.Is(err, ErrTriggerHasIncoming) ||
		errors.Is(err, ErrDisconnectedNodes) ||
		errors.Is(err, ErrTerminalHasOutgoing) ||
		errors.Is(err, ErrApprovalApproverRequired) ||
		errors.Is(err, ErrConditionBranchInvalid)
}

func (api *WorkflowAPI) handleValidateVersion(w http.ResponseWriter, r *http.Request) {
	ownerID := r.Header.Get("X-Owner-ID")
	if ownerID == "" {
		apiWriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "owner identification required")
		return
	}

	workflowID := r.PathValue("id")
	versionID := r.PathValue("vid")
	if workflowID == "" || versionID == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "workflow id and version id are required")
		return
	}

	// Verify ownership
	def, err := api.store.GetWorkflow(r.Context(), workflowID)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "GET_FAILED", "failed to get workflow: "+err.Error())
		return
	}
	if def == nil {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "workflow not found")
		return
	}
	if def.OwnerID != ownerID {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "workflow not found")
		return
	}

	// Verify the version belongs to this workflow
	ver, err := api.store.GetVersion(r.Context(), versionID)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "GET_FAILED", "failed to get version: "+err.Error())
		return
	}
	if ver == nil || ver.WorkflowID != workflowID {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "version not found")
		return
	}

	// Run detailed validation
	validationErrors := ValidateWorkflowGraphDetailed(ver.Graph)
	if validationErrors == nil {
		validationErrors = []ValidationError{}
	}

	valid := len(validationErrors) == 0
	apiWriteJSON(w, http.StatusOK, map[string]any{
		"valid":  valid,
		"errors": validationErrors,
	})
}

// --- Optional store interfaces for update/delete ---

// WorkflowUpdater is an optional interface for stores that support updating workflows.
type WorkflowUpdater interface {
	UpdateWorkflow(ctx context.Context, def *WorkflowDefinition) error
}

// WorkflowDeleter is an optional interface for stores that support deleting workflows.
type WorkflowDeleter interface {
	DeleteWorkflow(ctx context.Context, id string) error
}

// --- Response helpers (package-local to avoid conflicts with httpapi package) ---

func apiWriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func apiWriteError(w http.ResponseWriter, status int, code string, message string) {
	apiWriteJSON(w, status, map[string]any{
		"ok":      false,
		"code":    code,
		"message": message,
	})
}
