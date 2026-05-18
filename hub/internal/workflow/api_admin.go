package workflow

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// AdminReviewAPI provides HTTP handlers for admin review operations.
// These endpoints require admin authentication (enforced by adminMiddleware).
type AdminReviewAPI struct {
	reviewService *AdminReviewService
}

// NewAdminReviewAPI creates a new AdminReviewAPI with the given review service.
func NewAdminReviewAPI(reviewService *AdminReviewService) *AdminReviewAPI {
	return &AdminReviewAPI{
		reviewService: reviewService,
	}
}

// RegisterRoutes registers all admin review API routes on the given mux.
// The adminMiddleware should verify that the caller has admin privileges.
func (api *AdminReviewAPI) RegisterRoutes(mux *http.ServeMux, adminMiddleware func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/admin/reviews", adminMiddleware(api.handleListPendingReviews))
	mux.HandleFunc("GET /api/v1/admin/reviews/{id}", adminMiddleware(api.handleGetSubmissionDetail))
	mux.HandleFunc("POST /api/v1/admin/reviews/{id}/approve", adminMiddleware(api.handleApproveSubmission))
	mux.HandleFunc("POST /api/v1/admin/reviews/{id}/reject", adminMiddleware(api.handleRejectSubmission))
	mux.HandleFunc("POST /api/v1/admin/reviews/{id}/unpublish", adminMiddleware(api.handleUnpublishVersion))
}

// handleListPendingReviews returns the queue of pending workflow submissions
// sorted by submission date (oldest first), paginated at 50 items per page.
//
// GET /api/v1/admin/reviews?page=1
//
// Response:
//
//	{
//	  "submissions": [...],
//	  "total": 123,
//	  "page": 1,
//	  "page_size": 50
//	}
func (api *AdminReviewAPI) handleListPendingReviews(w http.ResponseWriter, r *http.Request) {
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	result, err := api.reviewService.ListPendingSubmissions(r.Context(), page)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "LIST_FAILED", "failed to list pending reviews: "+err.Error())
		return
	}

	apiWriteJSON(w, http.StatusOK, result)
}

// handleGetSubmissionDetail returns the complete workflow graph and configurations
// for admin inspection during review.
//
// GET /api/v1/admin/reviews/:id
func (api *AdminReviewAPI) handleGetSubmissionDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "submission version id is required")
		return
	}

	detail, err := api.reviewService.GetSubmissionForReview(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "submission not found")
			return
		}
		if strings.Contains(err.Error(), "not pending review") {
			apiWriteError(w, http.StatusConflict, "NOT_PENDING", err.Error())
			return
		}
		apiWriteError(w, http.StatusInternalServerError, "GET_FAILED", "failed to get submission detail: "+err.Error())
		return
	}

	apiWriteJSON(w, http.StatusOK, detail)
}

// handleApproveSubmission transitions a pending_review version to "published",
// supersedes the previous published version, and registers in the Capability Market.
//
// POST /api/v1/admin/reviews/:id/approve
func (api *AdminReviewAPI) handleApproveSubmission(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "submission version id is required")
		return
	}

	if err := api.reviewService.ApproveSubmission(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "submission not found")
			return
		}
		if strings.Contains(err.Error(), "not pending review") {
			apiWriteError(w, http.StatusConflict, "NOT_PENDING", err.Error())
			return
		}
		apiWriteError(w, http.StatusInternalServerError, "APPROVE_FAILED", "failed to approve submission: "+err.Error())
		return
	}

	apiWriteJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "submission approved and published",
	})
}

// rejectSubmissionRequest is the request body for rejecting a submission.
type rejectSubmissionRequest struct {
	Reason string `json:"reason"`
}

// handleRejectSubmission transitions a pending_review version to "rejected"
// with a required rejection reason (10-2000 characters).
//
// POST /api/v1/admin/reviews/:id/reject
// Body: {"reason": "..."}
func (api *AdminReviewAPI) handleRejectSubmission(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "submission version id is required")
		return
	}

	var req rejectSubmissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apiWriteError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body: "+err.Error())
		return
	}

	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "rejection reason is required")
		return
	}

	if err := api.reviewService.RejectSubmission(r.Context(), id, reason); err != nil {
		if strings.Contains(err.Error(), "not found") {
			apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "submission not found")
			return
		}
		if strings.Contains(err.Error(), "not pending review") {
			apiWriteError(w, http.StatusConflict, "NOT_PENDING", err.Error())
			return
		}
		if strings.Contains(err.Error(), "at least 10 characters") {
			apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
			return
		}
		if strings.Contains(err.Error(), "must not exceed 2000 characters") {
			apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
			return
		}
		apiWriteError(w, http.StatusInternalServerError, "REJECT_FAILED", "failed to reject submission: "+err.Error())
		return
	}

	apiWriteJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "submission rejected",
	})
}

// handleUnpublishVersion transitions a published version to "unpublished".
// This prevents new workflow instances from being created but does NOT
// terminate running instances.
//
// POST /api/v1/admin/reviews/:id/unpublish
func (api *AdminReviewAPI) handleUnpublishVersion(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "version id is required")
		return
	}

	if err := api.reviewService.UnpublishVersion(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "version not found")
			return
		}
		if strings.Contains(err.Error(), "not published") {
			apiWriteError(w, http.StatusConflict, "NOT_PUBLISHED", err.Error())
			return
		}
		apiWriteError(w, http.StatusInternalServerError, "UNPUBLISH_FAILED", "failed to unpublish version: "+err.Error())
		return
	}

	apiWriteJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "version unpublished",
	})
}
