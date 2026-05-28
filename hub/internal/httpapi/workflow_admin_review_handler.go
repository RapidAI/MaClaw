package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/capability"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
)

func workflowAdminReviewContext(r *http.Request) context.Context {
	tenantID := RequestTenantID(r)
	return capability.WithTenant(store.WithTenant(r.Context(), tenantID), tenantID)
}

// WorkflowAdminReviewListHandler handles GET /api/v1/admin/reviews.
// Returns the pending submissions queue, paginated at 50 per page.
func WorkflowAdminReviewListHandler(reviewSvc *workflow.AdminReviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}

		page := 1
		if p := strings.TrimSpace(r.URL.Query().Get("page")); p != "" {
			if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
				page = parsed
			}
		}

		result, err := reviewSvc.ListPendingSubmissions(workflowAdminReviewContext(r), page)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}

// WorkflowAdminReviewDetailHandler handles GET /api/v1/admin/reviews/{id}.
// Returns the complete workflow graph and configurations for admin inspection.
func WorkflowAdminReviewDetailHandler(reviewSvc *workflow.AdminReviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}

		versionID := strings.TrimSpace(r.PathValue("id"))
		if versionID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "version id is required")
			return
		}

		detail, err := reviewSvc.GetSubmissionForReview(workflowAdminReviewContext(r), versionID)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
				return
			}
			if strings.Contains(err.Error(), "not pending review") {
				writeError(w, http.StatusConflict, "INVALID_STATUS", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "GET_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, detail)
	}
}

// WorkflowAdminReviewApproveHandler handles POST /api/v1/admin/reviews/{id}/approve.
// Transitions the version from pending_review to published.
func WorkflowAdminReviewApproveHandler(reviewSvc *workflow.AdminReviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}

		versionID := strings.TrimSpace(r.PathValue("id"))
		if versionID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "version id is required")
			return
		}

		if err := reviewSvc.ApproveSubmission(workflowAdminReviewContext(r), versionID); err != nil {
			if strings.Contains(err.Error(), "not found") {
				writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
				return
			}
			if strings.Contains(err.Error(), "not pending review") {
				writeError(w, http.StatusConflict, "INVALID_STATUS", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "APPROVE_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"message": "submission approved and published",
		})
	}
}

// WorkflowAdminReviewRejectHandler handles POST /api/v1/admin/reviews/{id}/reject.
// Transitions the version from pending_review to rejected with a reason.
func WorkflowAdminReviewRejectHandler(reviewSvc *workflow.AdminReviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}

		versionID := strings.TrimSpace(r.PathValue("id"))
		if versionID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "version id is required")
			return
		}

		var req struct {
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}

		if err := reviewSvc.RejectSubmission(workflowAdminReviewContext(r), versionID, req.Reason); err != nil {
			if strings.Contains(err.Error(), "not found") {
				writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
				return
			}
			if strings.Contains(err.Error(), "not pending review") {
				writeError(w, http.StatusConflict, "INVALID_STATUS", err.Error())
				return
			}
			if strings.Contains(err.Error(), "at least 10 characters") || strings.Contains(err.Error(), "not exceed 2000") {
				writeError(w, http.StatusBadRequest, "INVALID_REASON", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "REJECT_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"message": "submission rejected",
		})
	}
}

// WorkflowAdminReviewUnpublishHandler handles POST /api/v1/admin/reviews/{id}/unpublish.
// Transitions a published version to unpublished, preventing new instances.
func WorkflowAdminReviewUnpublishHandler(reviewSvc *workflow.AdminReviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}

		versionID := strings.TrimSpace(r.PathValue("id"))
		if versionID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "version id is required")
			return
		}

		if err := reviewSvc.UnpublishVersion(workflowAdminReviewContext(r), versionID); err != nil {
			if strings.Contains(err.Error(), "not found") {
				writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
				return
			}
			if strings.Contains(err.Error(), "not published") {
				writeError(w, http.StatusConflict, "INVALID_STATUS", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "UNPUBLISH_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"message": "version unpublished",
		})
	}
}
