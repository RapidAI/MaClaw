package httpapi

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// Optional system settings for resolving paid document quota on write paths.
// Configured from NewRouter; nil-safe (falls back to free 100MiB).
var (
	mobileQuotaSystem   store.SystemSettingsRepository
	mobileQuotaSecurity *security.SecurityService
)

// ConfigureMobileDocumentQuota wires service-grant resolution into document
// write enforcement so free/paid limits match bootstrap.
func ConfigureMobileDocumentQuota(system store.SystemSettingsRepository, securitySvc *security.SecurityService) {
	mobileQuotaSystem = system
	mobileQuotaSecurity = securitySvc
}

// mobileDocumentQuotaUsedBytes sums stored emergency document material for an owner
// in one tenant:
// draft markdown bytes + upload source bytes (raw files still on Hub).
// Dead blob paths are repaired so missing originals do not inflate used quota.
func mobileDocumentQuotaUsedBytes(ownerID, tenantID string) int64 {
	used, repaired := mobileDocumentQuotaScan(ownerID, tenantID, true)
	if repaired {
		// Persist outside the scan lock (scan already unlocked).
		mobilePersistState()
	}
	return used
}

// mobileDocumentQuotaScan counts quota while optionally repairing dead original paths.
// When repair is false, metadata is trusted (fast path for write checks that are under limit).
// Caller must not hold mobileDocuments.Lock.
func mobileDocumentQuotaScan(ownerID, tenantID string, repair bool) (used int64, repaired bool) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return 0, false
	}
	mobileDocuments.Lock()
	defer mobileDocuments.Unlock()
	draftsWithOriginal := make(map[string]struct{})
	for id, draft := range mobileDocuments.drafts {
		if draft.OwnerID != ownerID || !mobileMeetingRecordingTenantMatches(tenantID, draft.TenantID) {
			continue
		}
		if repair && mobileDraftRepairSourceMeta(&draft) {
			mobileDocuments.drafts[id] = draft
			repaired = true
		}
		used += int64(len(draft.Markdown))
		if n := mobileDraftSourceSize(draft); n > 0 {
			used += int64(n)
			draftsWithOriginal[draft.ID] = struct{}{}
		}
	}
	for id, upload := range mobileDocuments.uploads {
		if upload.OwnerID != ownerID || !mobileMeetingRecordingTenantMatches(tenantID, upload.TenantID) {
			continue
		}
		if repair && mobileUploadRepairSourceMeta(&upload) {
			mobileDocuments.uploads[id] = upload
			repaired = true
		}
		// Avoid double-counting when the same original bytes already live on the draft.
		if upload.DraftID != "" {
			if _, ok := draftsWithOriginal[upload.DraftID]; ok {
				continue
			}
		}
		used += int64(mobileUploadSourceSize(upload))
	}
	return used, repaired
}

// mobileDocumentQuotaLimitForPrincipal returns the effective quota bytes for bootstrap parity.
// Callers that already computed paid quota may pass override > 0.
func mobileDocumentQuotaLimitForPrincipal(principal *auth.ViewerPrincipal, paidBoost bool) int64 {
	if paidBoost {
		return mobileCapDocPaidBytes()
	}
	return mobileCapDocFreeBytes()
}

// mobileEffectiveDocumentQuota resolves free vs paid limit using the plan caps matrix.
func mobileEffectiveDocumentQuota(ctx context.Context, principal *auth.ViewerPrincipal) int64 {
	if principal == nil {
		return mobileCapDocFreeBytes()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	grant := mobileResolveServiceGrantSnapshot(ctx, principal, mobileQuotaSystem, mobileQuotaSecurity, "")
	llmAccess := mobileLlmAccessPayload(ctx, principal)
	plan := mobilePlanForAccessWithGrant(llmAccess, grant)
	caps := mobilePlanCapsFor(plan, grant, mobileOfficialEntitled(llmAccess))
	return caps.DocumentQuotaBytes
}

func mobileCheckDocumentQuota(ownerID, tenantID string, additionalBytes int64, limit int64) error {
	if limit <= 0 {
		limit = 100 * 1024 * 1024
	}
	if additionalBytes < 0 {
		additionalBytes = 0
	}
	// Fast path: trust in-memory size metadata (no disk stats).
	used, _ := mobileDocumentQuotaScan(ownerID, tenantID, false)
	if used+additionalBytes <= limit {
		return nil
	}
	// Over limit: re-scan with repair so ghost blobs cannot block uploads.
	used, repaired := mobileDocumentQuotaScan(ownerID, tenantID, true)
	if repaired {
		mobilePersistState()
	}
	if used+additionalBytes > limit {
		return errString("document storage quota exceeded")
	}
	return nil
}

// mobileCheckDocumentQuotaForPrincipal uses grant-aware limit (paid → 500MiB).
func mobileCheckDocumentQuotaForPrincipal(ctx context.Context, principal *auth.ViewerPrincipal, additionalBytes int64) error {
	if principal == nil {
		return errString("principal required")
	}
	ownerID := mobilePrincipalOwnerID(principal)
	limit := mobileEffectiveDocumentQuota(ctx, principal)
	return mobileCheckDocumentQuota(ownerID, principal.TenantID, additionalBytes, limit)
}

// MobileDocumentQuotaHandler returns current used/limit for the viewer.
//
//	GET /api/mobile/documents/quota
func MobileDocumentQuotaHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		ownerID := mobilePrincipalOwnerID(principal)
		used := mobileDocumentQuotaUsedBytes(ownerID, principal.TenantID)
		limit := mobileEffectiveDocumentQuota(r.Context(), principal)
		writeJSON(w, http.StatusOK, map[string]any{
			"document_quota_bytes":      limit,
			"document_quota_used_bytes": used,
			"document_quota_remaining":  maxInt64(0, limit-used),
			// Helper for clients that show human sizes.
			"draft_rune_estimate": mobileDocumentDraftRuneEstimate(ownerID, principal.TenantID),
		})
	}
}

func mobileDocumentDraftRuneEstimate(ownerID, tenantID string) int64 {
	var n int64
	mobileDocuments.Lock()
	defer mobileDocuments.Unlock()
	for _, draft := range mobileDocuments.drafts {
		if draft.OwnerID != ownerID || !mobileMeetingRecordingTenantMatches(tenantID, draft.TenantID) {
			continue
		}
		n += int64(utf8.RuneCountInString(draft.Markdown))
	}
	return n
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
