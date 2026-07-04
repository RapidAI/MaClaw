package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func AdminSyncVerifiedPhoneRoutesHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if identity == nil {
			writeError(w, http.StatusServiceUnavailable, "IDENTITY_SERVICE_UNAVAILABLE", "identity service is unavailable")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		tenantID := ""
		if IsGlobalAdmin(r.Context()) {
			tenantID = strings.TrimSpace(r.URL.Query().Get("tenant_id"))
		} else {
			tenantID = strings.TrimSpace(AdminTenantID(r.Context()))
		}
		count, err := identity.SyncVerifiedPhoneRoutesForTenant(ctx, tenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "PHONE_ROUTE_SYNC_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"synced_count": count,
			"tenant_id":    tenantID,
		})
	}
}
