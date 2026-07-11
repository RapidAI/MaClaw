package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// MobileEntitlementsCapsHandler returns or updates effective plan caps.
//
//	GET  /api/mobile/entitlements/caps   (viewer auth)
//	PUT  /api/mobile/entitlements/caps   (ops: header X-Maclaw-Caps-Admin-Token
//	                                      must match MACLAW_MOBILE_CAPS_ADMIN_TOKEN)
func MobileEntitlementsCapsHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, securitySvc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			mobileEntitlementsCapsGet(w, r, identity, system, securitySvc)
		case http.MethodPut:
			mobileEntitlementsCapsPut(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or PUT")
		}
	}
}

func mobileEntitlementsCapsGet(w http.ResponseWriter, r *http.Request, identity *auth.IdentityService, system store.SystemSettingsRepository, securitySvc *security.SecurityService) {
	principal, err := authenticateViewerRequest(r, identity)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
		return
	}
	ctx := r.Context()
	hubURL := mobileRequestBaseURL(r)
	llmAccess := mobileLlmAccessPayload(ctx, principal)
	grant := mobileResolveServiceGrantSnapshot(ctx, principal, system, securitySvc, hubURL)
	plan := mobilePlanForAccessWithGrant(llmAccess, grant)
	entitled := mobileOfficialEntitled(llmAccess) || grant.Active
	caps := mobilePlanCapsFor(plan, grant, mobileOfficialEntitled(llmAccess))
	writeJSON(w, http.StatusOK, map[string]any{
		"plan":         caps.Plan,
		"entitlements": caps.toEntitlementMap(grant, entitled),
		"caps": map[string]any{
			"document_quota_bytes":        caps.DocumentQuotaBytes,
			"max_upload_bytes":            caps.MaxUploadBytes,
			"max_export_jobs":             caps.MaxExportJobs,
			"shared_employees":            caps.SharedEmployees,
			"hub_ssh_exec":                caps.HubSSHExec,
			"mobile_agent":                caps.MobileAgent,
			"document_ai":                 caps.DocumentAI,
			"hub_file_download_max_bytes": caps.HubFileDownloadMaxBytes,
			"hub_file_download_chunked":   true,
			"hub_file_single_shot_bytes":  mobileHubFileSingleShotBytes,
			"hub_file_chunk_raw_bytes":    mobileHubFileChunkRawBytes,
		},
		"env_overrides": map[string]any{
			"doc_free_mib":          "MACLAW_MOBILE_CAP_DOC_FREE_MIB",
			"doc_paid_mib":          "MACLAW_MOBILE_CAP_DOC_PAID_MIB",
			"export_free":           "MACLAW_MOBILE_CAP_EXPORT_FREE",
			"export_paid":           "MACLAW_MOBILE_CAP_EXPORT_PAID",
			"hub_file_download_mib": "MACLAW_MOBILE_CAP_HUB_FILE_DOWNLOAD_MIB",
			"admin_token":           "MACLAW_MOBILE_CAPS_ADMIN_TOKEN",
		},
		"runtime_overrides": mobileCapsRuntimeSnapshot(),
		"server_time":       time.Now().UTC().Format(time.RFC3339),
	})
}

type mobileCapsPutRequest struct {
	DocFreeMib         int64 `json:"doc_free_mib"`
	DocPaidMib         int64 `json:"doc_paid_mib"`
	ExportFree         int64 `json:"export_free"`
	ExportPaid         int64 `json:"export_paid"`
	HubFileDownloadMib int64 `json:"hub_file_download_mib"`
	// Clear: when true, clears all runtime overrides (env/default take over again).
	Clear bool `json:"clear,omitempty"`
}

func mobileEntitlementsCapsPut(w http.ResponseWriter, r *http.Request) {
	expected := strings.TrimSpace(os.Getenv("MACLAW_MOBILE_CAPS_ADMIN_TOKEN"))
	if expected == "" {
		writeError(w, http.StatusServiceUnavailable, "CAPS_ADMIN_DISABLED",
			"set MACLAW_MOBILE_CAPS_ADMIN_TOKEN to enable runtime caps PUT")
		return
	}
	got := strings.TrimSpace(r.Header.Get("X-Maclaw-Caps-Admin-Token"))
	if got == "" || got != expected {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid or missing X-Maclaw-Caps-Admin-Token")
		return
	}
	var req mobileCapsPutRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
		return
	}
	if req.Clear {
		mobileCapsRuntimeApply(-1, -1, -1, -1, -1)
	} else {
		// Only apply fields that are non-zero in request (partial update).
		// Use negative sentinels only via clear.
		mobileCapsRuntime.Lock()
		if req.DocFreeMib > 0 {
			mobileCapsRuntime.DocFreeMib = req.DocFreeMib
		}
		if req.DocPaidMib > 0 {
			mobileCapsRuntime.DocPaidMib = req.DocPaidMib
		}
		if req.ExportFree > 0 {
			mobileCapsRuntime.ExportFree = req.ExportFree
		}
		if req.ExportPaid > 0 {
			mobileCapsRuntime.ExportPaid = req.ExportPaid
		}
		if req.HubFileDownloadMib > 0 {
			mobileCapsRuntime.HubFileDownloadMib = req.HubFileDownloadMib
		}
		mobileCapsRuntime.Unlock()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"runtime_overrides": mobileCapsRuntimeSnapshot(),
		"effective": map[string]any{
			"doc_free_bytes":          mobileCapDocFreeBytes(),
			"doc_paid_bytes":          mobileCapDocPaidBytes(),
			"export_free":             mobileCapExportFreeN(),
			"export_paid":             mobileCapExportPaidN(),
			"hub_file_download_bytes": mobileCapHubFileDownloadBytes(),
		},
		"server_time": time.Now().UTC().Format(time.RFC3339),
	})
}
