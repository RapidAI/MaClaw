package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type KnowledgeShareAdminView struct {
	KnowledgeID         string   `json:"knowledge_id"`
	TenantID            string   `json:"tenant_id"`
	OwnerUserID         string   `json:"owner_user_id"`
	OwnerUserEmail      string   `json:"owner_user_email"`
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	VisibilityScope     string   `json:"visibility_scope"`
	VisibilityUsers     []string `json:"visibility_users,omitempty"`
	ShareURL            string   `json:"share_url"`
	HubID               string   `json:"hub_id"`
	Status              string   `json:"status"`
	ViewCount           int64    `json:"view_count"`
	ImportCount         int64    `json:"import_count"`
	CreatedAt           string   `json:"created_at"`
	UpdatedAt           string   `json:"updated_at"`
	PublishedAt         string   `json:"published_at"`
	ExpiresAt           string   `json:"expires_at,omitempty"`
	ForcedDeletedBy     string   `json:"forced_deleted_by,omitempty"`
	ForcedDeletedReason string   `json:"forced_deleted_reason,omitempty"`
	ForcedDeletedAt     string   `json:"forced_deleted_at,omitempty"`
}

func ListKnowledgeSharesAdminHandler(repo store.KnowledgeShareRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if repo == nil {
			writeError(w, http.StatusNotImplemented, "KNOWLEDGE_SHARES_UNAVAILABLE", "Knowledge shares are unavailable")
			return
		}
		limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
		if limit <= 0 || limit > 50 {
			limit = 50
		}
		offset, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset")))
		if offset < 0 {
			offset = 0
		}
		filter := store.KnowledgeShareFilter{
			User:    strings.TrimSpace(r.URL.Query().Get("user")),
			Keyword: strings.TrimSpace(r.URL.Query().Get("keyword")),
			Sort:    strings.TrimSpace(r.URL.Query().Get("sort")),
			Offset:  offset,
			Limit:   limit,
		}
		if IsGlobalAdmin(r.Context()) {
			if tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantID != "" {
				filter.TenantID = tenantID
				filter.TenantScoped = true
			}
		} else {
			filter.TenantID = AdminTenantID(r.Context())
			filter.TenantScoped = true
		}
		items, total, err := repo.List(r.Context(), filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_KNOWLEDGE_SHARES_FAILED", err.Error())
			return
		}
		views := make([]KnowledgeShareAdminView, 0, len(items))
		for _, item := range items {
			if item == nil {
				continue
			}
			views = append(views, knowledgeShareAdminView(item))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": views, "total": total, "offset": offset, "limit": limit})
	}
}

func ForceDeleteKnowledgeShareAdminHandler(repo store.KnowledgeShareRepository, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if repo == nil {
			writeError(w, http.StatusNotImplemented, "KNOWLEDGE_SHARES_UNAVAILABLE", "Knowledge shares are unavailable")
			return
		}
		knowledgeID := strings.TrimSpace(r.PathValue("knowledgeID"))
		if knowledgeID == "" {
			writeError(w, http.StatusBadRequest, "KNOWLEDGE_ID_REQUIRED", "Knowledge ID is required")
			return
		}
		var req struct {
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		reason := strings.TrimSpace(req.Reason)
		if reason == "" {
			writeError(w, http.StatusBadRequest, "DELETE_REASON_REQUIRED", "Delete reason is required")
			return
		}
		item, err := repo.Get(r.Context(), knowledgeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "GET_KNOWLEDGE_SHARE_FAILED", err.Error())
			return
		}
		if item == nil {
			writeError(w, http.StatusNotFound, "KNOWLEDGE_SHARE_NOT_FOUND", "Knowledge share not found")
			return
		}
		if !IsGlobalAdmin(r.Context()) && !strings.EqualFold(strings.TrimSpace(item.TenantID), AdminTenantID(r.Context())) {
			writeError(w, http.StatusForbidden, "TENANT_FORBIDDEN", "Tenant access denied")
			return
		}
		admin := AdminFromContext(r.Context())
		adminID := ""
		if admin != nil {
			adminID = admin.ID
		}
		now := time.Now().UTC()
		if err := repo.ForceDelete(r.Context(), store.KnowledgeShareForceDeleteRequest{
			KnowledgeID: knowledgeID,
			AdminUserID: adminID,
			Reason:      reason,
			DeletedAt:   now,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "DELETE_KNOWLEDGE_SHARE_FAILED", err.Error())
			return
		}
		writeKnowledgeShareAdminAudit(r, audit, item, adminID, reason, now)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "knowledge_id": knowledgeID, "status": "deleted"})
	}
}

func knowledgeShareAdminView(item *store.KnowledgeShare) KnowledgeShareAdminView {
	visibilityUsers := []string{}
	if strings.TrimSpace(item.VisibilityUsersJSON) != "" {
		_ = json.Unmarshal([]byte(item.VisibilityUsersJSON), &visibilityUsers)
	}
	view := KnowledgeShareAdminView{
		KnowledgeID:     item.KnowledgeID,
		TenantID:        item.TenantID,
		OwnerUserID:     item.OwnerUserID,
		OwnerUserEmail:  item.OwnerUserEmail,
		Title:           item.Title,
		Description:     item.Description,
		VisibilityScope: item.VisibilityScope,
		VisibilityUsers: visibilityUsers,
		ShareURL:        item.ShareURL,
		HubID:           item.HubID,
		Status:          item.Status,
		ViewCount:       item.ViewCount,
		ImportCount:     item.ImportCount,
		CreatedAt:       item.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       item.UpdatedAt.Format(time.RFC3339),
		PublishedAt:     item.PublishedAt.Format(time.RFC3339),
	}
	if strings.TrimSpace(item.ForcedDeletedBy) != "" {
		view.ForcedDeletedBy = item.ForcedDeletedBy
	}
	if item.ExpiresAt != nil && !item.ExpiresAt.IsZero() {
		view.ExpiresAt = item.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if strings.TrimSpace(item.ForcedDeletedReason) != "" {
		view.ForcedDeletedReason = item.ForcedDeletedReason
	}
	if item.ForcedDeletedAt != nil {
		view.ForcedDeletedAt = item.ForcedDeletedAt.Format(time.RFC3339)
	}
	return view
}

func writeKnowledgeShareAdminAudit(r *http.Request, audit store.AdminAuditRepository, item *store.KnowledgeShare, adminID, reason string, at time.Time) {
	if audit == nil || item == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"knowledge_id": item.KnowledgeID,
		"tenant_id":    item.TenantID,
		"owner_email":  item.OwnerUserEmail,
		"reason":       reason,
		"deleted_at":   at.Format(time.RFC3339),
	})
	_ = audit.Create(r.Context(), &store.AdminAuditLog{
		ID:          newKnowledgeAdminAuditID(),
		TenantID:    item.TenantID,
		AdminUserID: adminID,
		Action:      "knowledge_share.force_delete",
		PayloadJSON: string(payload),
		CreatedAt:   at,
	})
}

func newKnowledgeAdminAuditID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return "aa_" + hex.EncodeToString(buf[:])
	}
	return "aa_" + strconv.FormatInt(time.Now().UnixNano(), 36)
}
