package httpapi

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

var (
	errMobileDocumentDraftNotFound = errString("document draft not found")
	errMobileDocumentQuotaExceeded = errString("document storage quota exceeded")
	errMobileDocumentDraftChanged  = errString("document draft changed during processing")
)

// Optional system settings for resolving paid document quota on write paths.
// Configured from NewRouter; nil-safe (falls back to free 100MiB).
var (
	mobileQuotaSystem   store.SystemSettingsRepository
	mobileQuotaSecurity *security.SecurityService
	// Serializes quota check + document insertion so concurrent uploads cannot
	// each pass against the same pre-upload usage snapshot.
	mobileDocumentQuotaAdmissionMu sync.Mutex
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
	draftSourcePaths := make(map[string]struct{})
	for id, draft := range mobileDocuments.drafts {
		if draft.OwnerID != ownerID || !mobileMeetingRecordingTenantMatches(tenantID, draft.TenantID) {
			continue
		}
		if repair && mobileDraftRepairSourceMeta(&draft) {
			mobileDocuments.drafts[id] = draft
			repaired = true
		}
		used += int64(len(draft.Markdown))
		for _, image := range draft.Images {
			if image.SourceSize > 0 {
				used += int64(image.SourceSize)
			}
		}
		if n := mobileDraftSourceSize(draft); n > 0 {
			used += int64(n)
			if path := strings.TrimSpace(draft.SourcePath); path != "" {
				draftSourcePaths[path] = struct{}{}
			}
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
		// Avoid double-counting only when draft/upload reference the same durable
		// immutable blob. DraftID alone is insufficient: legacy records may have a
		// separately persisted upload source that still consumes real storage.
		if path := strings.TrimSpace(upload.SourcePath); path != "" {
			if _, shared := draftSourcePaths[path]; shared {
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

// mobileTransformDocumentDraftWithinQuota serializes a body transform with all
// other quota-increasing document writes. The transform is computed outside the
// document lock; a bounded retry prevents a concurrent self-heal from turning a
// previously checked delta into an unchecked larger write.
func mobileTransformDocumentDraftWithinQuota(
	ctx context.Context,
	principal *auth.ViewerPrincipal,
	draftID string,
	transform func(string) string,
) (mobileDocumentDraftRecord, error) {
	if principal == nil || transform == nil {
		return mobileDocumentDraftRecord{}, errMobileDocumentDraftNotFound
	}
	ownerID := mobilePrincipalOwnerID(principal)
	if ownerID == "" {
		return mobileDocumentDraftRecord{}, errMobileDocumentDraftNotFound
	}

	mobileDocumentQuotaAdmissionMu.Lock()
	defer mobileDocumentQuotaAdmissionMu.Unlock()
	for attempt := 0; attempt < 3; attempt++ {
		mobileDocuments.Lock()
		current, ok := mobileDocuments.drafts[draftID]
		mobileDocuments.Unlock()
		if !ok || current.OwnerID != ownerID || !mobileMeetingRecordingTenantMatches(principal.TenantID, current.TenantID) {
			return mobileDocumentDraftRecord{}, errMobileDocumentDraftNotFound
		}

		nextMarkdown := transform(current.Markdown)
		delta := int64(len(nextMarkdown)) - int64(len(current.Markdown))
		if delta > 0 {
			if err := mobileCheckDocumentQuotaForPrincipal(ctx, principal, delta); err != nil {
				return mobileDocumentDraftRecord{}, errMobileDocumentQuotaExceeded
			}
		}

		mobileDocuments.Lock()
		latest, stillExists := mobileDocuments.drafts[draftID]
		if !stillExists || latest.OwnerID != ownerID || !mobileMeetingRecordingTenantMatches(principal.TenantID, latest.TenantID) {
			mobileDocuments.Unlock()
			return mobileDocumentDraftRecord{}, errMobileDocumentDraftNotFound
		}
		if latest.Markdown != current.Markdown {
			mobileDocuments.Unlock()
			continue
		}
		latest.Markdown = nextMarkdown
		latest.UpdatedAt = time.Now().UTC()
		mobileDocuments.drafts[draftID] = latest
		mobileDocuments.Unlock()
		return latest, nil
	}
	return mobileDocumentDraftRecord{}, errMobileDocumentDraftChanged
}

func mobileDraftHealQuotaDelta(current mobileDocumentDraftRecord, heal mobileDraftHealResult) int64 {
	before := int64(len(current.Markdown))
	for _, image := range current.Images {
		before += int64(max(image.SourceSize, 0))
	}
	after := int64(len(current.Markdown))
	display := strings.TrimSpace(heal.Display)
	stored := strings.TrimSpace(current.Markdown)
	if display != "" && display != stored {
		allow := mobileDraftRecordBodyUnreadable(current, current.Markdown) ||
			(mobileDraftSourceIsPDF(current) && mobilePDFTextLooksOverSpaced(stored)) ||
			heal.ReplaceImages
		if allow {
			after = int64(len(display))
		}
	}
	images := current.Images
	if heal.ReplaceImages && len(heal.Images) > 0 {
		images = heal.Images
	}
	for _, image := range images {
		after += int64(max(image.SourceSize, 0))
	}
	return after - before
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
