package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/notification"
)

// NotificationHandlers holds the notification service dependency for HTTP handlers.
type NotificationHandlers struct {
	service *notification.Service
}

// NewNotificationHandlers creates a new NotificationHandlers instance.
func NewNotificationHandlers(service *notification.Service) *NotificationHandlers {
	return &NotificationHandlers{service: service}
}

// CreateNotification handles POST /api/v1/admin/notifications.
// Creates a cross-Hub notification and triggers cascade dispatch to target Hubs.
func (h *NotificationHandlers) CreateNotification(w http.ResponseWriter, r *http.Request) {
	var req notification.CreateRequest
	if err := decodeLimitedJSON(w, r, &req, largeJSONBodyLimit); err != nil {
		writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
		return
	}

	// Populate created_by from the authenticated admin context.
	admin := AdminFromContext(r.Context())
	if admin != nil && req.CreatedBy == "" {
		req.CreatedBy = admin.Username
	}

	notif, err := h.service.CreateNotification(r.Context(), req)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"notification": notif,
	})
}

// ListNotifications handles GET /api/v1/admin/notifications.
// Returns a paginated list of notifications with cascade status.
func (h *NotificationHandlers) ListNotifications(w http.ResponseWriter, r *http.Request) {
	filter := notification.ListFilter{
		Offset: 0,
		Limit:  20,
	}

	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			filter.Offset = n
		}
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			filter.Limit = n
		}
	}
	if v := strings.TrimSpace(r.URL.Query().Get("status")); v != "" {
		filter.Status = notification.Status(v)
	}
	if v := strings.TrimSpace(r.URL.Query().Get("category")); v != "" {
		filter.Category = notification.Category(v)
	}

	results, total, err := h.service.ListNotifications(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"notifications": results,
		"total":         total,
	})
}

// GetNotification handles GET /api/v1/admin/notifications/{id}.
// Returns notification details along with cascade delivery status for each Hub.
func (h *NotificationHandlers) GetNotification(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Notification ID is required")
		return
	}

	result, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"notification":    result.Notification,
		"cascade_results": result.CascadeResults,
	})
}

// RevokeNotification handles POST /api/v1/admin/notifications/{id}/revoke.
// Revokes a published notification and cascades the revoke signal to all target Hubs.
func (h *NotificationHandlers) RevokeNotification(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Notification ID is required")
		return
	}

	if err := h.service.RevokeNotification(r.Context(), id); err != nil {
		h.handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
}

// DeleteNotification handles DELETE /api/v1/admin/notifications/{id}.
// Only inactive notifications (draft, expired, revoked) can be deleted.
func (h *NotificationHandlers) DeleteNotification(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Notification ID is required")
		return
	}

	if err := h.service.DeleteNotification(r.Context(), id); err != nil {
		h.handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
}

// handleServiceError maps notification service errors to appropriate HTTP responses.
func (h *NotificationHandlers) handleServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, notification.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Notification not found")
	case errors.Is(err, notification.ErrCannotRevoke):
		writeError(w, http.StatusBadRequest, "CANNOT_REVOKE", "Only published notifications can be revoked")
	case errors.Is(err, notification.ErrCannotDelete):
		writeError(w, http.StatusBadRequest, "CANNOT_DELETE", "Published notifications must be revoked before delete")
	case errors.Is(err, notification.ErrTitleRequired):
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	case errors.Is(err, notification.ErrTitleTooLong):
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	case errors.Is(err, notification.ErrContentRequired):
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	case errors.Is(err, notification.ErrContentTooLong):
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	case errors.Is(err, notification.ErrInvalidCategory):
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	case errors.Is(err, notification.ErrInvalidAudience):
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	case errors.Is(err, notification.ErrInvalidPriority):
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	case errors.Is(err, notification.ErrInvalidStatus):
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	case errors.Is(err, notification.ErrAudienceIDsRequired):
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
	}
}
