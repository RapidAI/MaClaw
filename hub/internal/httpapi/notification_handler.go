package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/notification"
)

// NotificationHandler provides HTTP handlers for admin notification endpoints.
type NotificationHandler struct {
	svc *notification.Service
}

// NewNotificationHandler creates a new NotificationHandler.
func NewNotificationHandler(svc *notification.Service) *NotificationHandler {
	return &NotificationHandler{svc: svc}
}

// ---------------------------------------------------------------------------
// POST /api/v1/admin/notifications — Create notification (requireAdmin)
// ---------------------------------------------------------------------------

type createNotificationRequest struct {
	Title        string   `json:"title"`
	Content      string   `json:"content"`
	Category     string   `json:"category"`
	Priority     string   `json:"priority"`
	AudienceType string   `json:"audience_type"`
	AudienceIDs  []string `json:"audience_ids"`
	IMPush       bool     `json:"im_push"`
	PublishAt    *string  `json:"publish_at,omitempty"`
	ExpireAt     *string  `json:"expire_at,omitempty"`
}

// HandleCreateNotification handles POST /api/v1/admin/notifications.
func (h *NotificationHandler) HandleCreateNotification(w http.ResponseWriter, r *http.Request) {
	// Limit request body to 1MB to prevent OOM from oversized payloads.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req createNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body: "+err.Error())
		return
	}

	// Resolve admin identity for created_by field.
	createdBy := "admin"
	if admin := AdminFromContext(r.Context()); admin != nil && strings.TrimSpace(admin.ID) != "" {
		createdBy = admin.ID
	}

	// Parse optional time fields.
	var publishAt *time.Time
	if req.PublishAt != nil && strings.TrimSpace(*req.PublishAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.PublishAt))
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_PUBLISH_AT", "publish_at must be RFC3339 format")
			return
		}
		publishAt = &t
	}

	var expireAt *time.Time
	if req.ExpireAt != nil && strings.TrimSpace(*req.ExpireAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.ExpireAt))
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_EXPIRE_AT", "expire_at must be RFC3339 format")
			return
		}
		expireAt = &t
	}

	createReq := notification.CreateRequest{
		Title:        req.Title,
		Content:      req.Content,
		Category:     notification.Category(req.Category),
		Priority:     notification.Priority(req.Priority),
		AudienceType: notification.AudienceType(req.AudienceType),
		AudienceIDs:  req.AudienceIDs,
		IMPush:       req.IMPush,
		CreatedBy:    createdBy,
		PublishAt:    publishAt,
		ExpireAt:     expireAt,
	}

	n, err := h.svc.CreateNotification(r.Context(), createReq)
	if err != nil {
		if isNotificationValidationError(err) {
			writeError(w, http.StatusBadRequest, "NOTIFICATION_VALIDATION_FAILED", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "NOTIFICATION_CREATE_FAILED", err.Error())
		return
	}

	// If publish_at is nil or in the past, publish immediately.
	if publishAt == nil || !publishAt.After(time.Now().UTC()) {
		_ = h.svc.PublishNotification(r.Context(), n.ID)
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"ok":           true,
		"notification": n,
	})
}

// ---------------------------------------------------------------------------
// GET /api/v1/admin/notifications — List notifications with stats (requireAdmin)
// ---------------------------------------------------------------------------

// HandleListNotifications handles GET /api/v1/admin/notifications.
func (h *NotificationHandler) HandleListNotifications(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	status := notification.Status(strings.TrimSpace(q.Get("status")))
	category := notification.Category(strings.TrimSpace(q.Get("category")))

	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 50
	}

	filter := notification.ListFilter{
		Status:   status,
		Category: category,
		Offset:   offset,
		Limit:    limit,
	}

	notifications, err := h.svc.ListNotifications(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "NOTIFICATION_LIST_FAILED", err.Error())
		return
	}

	// Collect notification IDs for batched read stats query.
	type notificationWithStats struct {
		*notification.Notification
		Stats *notification.ReadStats `json:"stats,omitempty"`
	}

	items := make([]notificationWithStats, 0, len(notifications))
	for _, n := range notifications {
		item := notificationWithStats{Notification: n}
		// GetReadStats per notification is acceptable for admin list (typically
		// small page sizes 10-50). A batched query could optimize further but
		// adds complexity for marginal gain on admin-only endpoints.
		if n.Status == notification.StatusPublished {
			stats, err := h.svc.GetReadStats(r.Context(), n.ID)
			if err == nil {
				item.Stats = stats
			}
		}
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"items": items,
		"total": len(items),
	})
}

// ---------------------------------------------------------------------------
// GET /api/v1/admin/notifications/{id} — Get notification detail + stats (requireAdmin)
// ---------------------------------------------------------------------------

// HandleGetNotification handles GET /api/v1/admin/notifications/{id}.
func (h *NotificationHandler) HandleGetNotification(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "NOTIFICATION_ID_REQUIRED", "notification id is required")
		return
	}

	n, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "NOTIFICATION_GET_FAILED", err.Error())
		return
	}
	if n == nil {
		writeError(w, http.StatusNotFound, "NOTIFICATION_NOT_FOUND", "notification not found")
		return
	}

	stats, _ := h.svc.GetReadStats(r.Context(), id)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"notification": n,
		"stats":        stats,
	})
}

// ---------------------------------------------------------------------------
// POST /api/v1/admin/notifications/{id}/revoke — Revoke notification (requireAdmin)
// ---------------------------------------------------------------------------

// HandleRevokeNotification handles POST /api/v1/admin/notifications/{id}/revoke.
func (h *NotificationHandler) HandleRevokeNotification(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "NOTIFICATION_ID_REQUIRED", "notification id is required")
		return
	}

	if err := h.svc.RevokeNotification(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "NOTIFICATION_NOT_FOUND", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "NOTIFICATION_REVOKE_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "notification revoked",
	})
}

// ---------------------------------------------------------------------------
// GET /api/v1/notifications/unread — Client pull unread (machine auth)
// ---------------------------------------------------------------------------

// HandleUnread handles GET /api/v1/notifications/unread.
func (h *NotificationHandler) HandleUnread(w http.ResponseWriter, r *http.Request) {
	machineID := MachineIDFromContext(r.Context())
	if machineID == "" {
		writeError(w, http.StatusUnauthorized, "MACHINE_UNAUTHORIZED", "machine auth required")
		return
	}

	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 10 {
			limit = v
		}
	}

	notifications, err := h.svc.GetUnreadForMachine(r.Context(), machineID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "NOTIFICATION_UNREAD_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, notifications)
}

// ---------------------------------------------------------------------------
// POST /api/v1/notifications/{id}/read — Client mark single as read (machine auth)
// ---------------------------------------------------------------------------

// HandleMarkRead handles POST /api/v1/notifications/{id}/read.
func (h *NotificationHandler) HandleMarkRead(w http.ResponseWriter, r *http.Request) {
	machineID := MachineIDFromContext(r.Context())
	if machineID == "" {
		writeError(w, http.StatusUnauthorized, "MACHINE_UNAUTHORIZED", "machine auth required")
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "NOTIFICATION_ID_REQUIRED", "notification id is required")
		return
	}

	if err := h.svc.MarkRead(r.Context(), machineID, id); err != nil {
		writeError(w, http.StatusInternalServerError, "NOTIFICATION_READ_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------------------------------------------------------------------------
// POST /api/v1/notifications/read-all — Client mark all as read (machine auth)
// ---------------------------------------------------------------------------

// HandleMarkAllRead handles POST /api/v1/notifications/read-all.
func (h *NotificationHandler) HandleMarkAllRead(w http.ResponseWriter, r *http.Request) {
	machineID := MachineIDFromContext(r.Context())
	if machineID == "" {
		writeError(w, http.StatusUnauthorized, "MACHINE_UNAUTHORIZED", "machine auth required")
		return
	}

	if err := h.svc.MarkAllRead(r.Context(), machineID); err != nil {
		writeError(w, http.StatusInternalServerError, "NOTIFICATION_READ_ALL_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------------------------------------------------------------------------
// POST /api/v1/notifications/cascade — HubCenter cascade push (requireGlobalAdmin)
// ---------------------------------------------------------------------------

// HandleCascade handles POST /api/v1/notifications/cascade.
// This is the HubCenter cascade push entry point. HubCenter uses a global admin
// token to push notifications to individual Hub instances. The handler delegates
// to Service.CreateFromCascade which handles idempotency internally: if a
// notification with the same source+source_id already exists, it is updated
// rather than duplicated.
func (h *NotificationHandler) HandleCascade(w http.ResponseWriter, r *http.Request) {
	// Limit request body to 1MB to prevent OOM from oversized cascade payloads.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req notification.CascadeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body: "+err.Error())
		return
	}

	if req.Notification == nil {
		writeError(w, http.StatusBadRequest, "NOTIFICATION_REQUIRED", "notification field is required")
		return
	}

	if err := h.svc.CreateFromCascade(r.Context(), req); err != nil {
		writeError(w, http.StatusInternalServerError, "CASCADE_FAILED", "cascade push failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// isNotificationValidationError checks whether the error is a service-layer
// validation error (title/content/category/audience/priority constraints).
func isNotificationValidationError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "is required") ||
		strings.Contains(msg, "exceeds") ||
		strings.Contains(msg, "invalid category") ||
		strings.Contains(msg, "invalid audience_type") ||
		strings.Contains(msg, "invalid priority")
}
