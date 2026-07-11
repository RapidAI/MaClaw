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

// mobileDocumentQuotaUsedBytes sums stored emergency document material for an owner:
// draft markdown bytes + upload source bytes (raw files still on Hub).
func mobileDocumentQuotaUsedBytes(ownerID string) int64 {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return 0
	}
	var used int64
	mobileDocuments.Lock()
	defer mobileDocuments.Unlock()
	draftsWithOriginal := make(map[string]struct{})
	for _, draft := range mobileDocuments.drafts {
		if draft.OwnerID != ownerID {
			continue
		}
		used += int64(len(draft.Markdown))
		if n := len(draft.SourceBytes); n > 0 {
			used += int64(n)
			draftsWithOriginal[draft.ID] = struct{}{}
		}
	}
	for _, upload := range mobileDocuments.uploads {
		if upload.OwnerID != ownerID {
			continue
		}
		// Avoid double-counting when the same original bytes already live on the draft.
		if upload.DraftID != "" {
			if _, ok := draftsWithOriginal[upload.DraftID]; ok {
				continue
			}
		}
		used += int64(len(upload.SourceBytes))
	}
	return used
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

func mobileCheckDocumentQuota(ownerID string, additionalBytes int64, limit int64) error {
	if limit <= 0 {
		limit = 100 * 1024 * 1024
	}
	if additionalBytes < 0 {
		additionalBytes = 0
	}
	used := mobileDocumentQuotaUsedBytes(ownerID)
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
	return mobileCheckDocumentQuota(ownerID, additionalBytes, limit)
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
		used := mobileDocumentQuotaUsedBytes(ownerID)
		limit := mobileEffectiveDocumentQuota(r.Context(), principal)
		writeJSON(w, http.StatusOK, map[string]any{
			"document_quota_bytes":      limit,
			"document_quota_used_bytes": used,
			"document_quota_remaining":  maxInt64(0, limit-used),
			// Helper for clients that show human sizes.
			"draft_rune_estimate": mobileDocumentDraftRuneEstimate(ownerID),
		})
	}
}

func mobileDocumentDraftRuneEstimate(ownerID string) int64 {
	var n int64
	mobileDocuments.Lock()
	defer mobileDocuments.Unlock()
	for _, draft := range mobileDocuments.drafts {
		if draft.OwnerID != ownerID {
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
