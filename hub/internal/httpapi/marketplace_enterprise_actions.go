package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/capability"
)

// --- Delete Enterprise Capability ---

// AdminCapabilityDeleteHandler soft-deletes an enterprise capability using a
// transactional operation that atomically marks status="deleted" and disables
// all associated managed deployments and recommendations.
// Only the owning tenant admin can perform this operation.
func AdminCapabilityDeleteHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			writeError(w, http.StatusBadRequest, "CAPABILITY_ID_REQUIRED", "capability id is required")
			return
		}
		ctx := capabilityAdminContext(r)

		// Transactional delete: status → "deleted" + disable deployments/recommendations.
		// DeleteCapability returns ErrNotFound if capability doesn't exist or doesn't
		// belong to this tenant — no separate Get is needed (eliminates TOCTOU race).
		if err := svc.DeleteCapability(ctx, id); err != nil {
			if errors.Is(err, capability.ErrNotFound) {
				writeError(w, http.StatusNotFound, "CAPABILITY_NOT_FOUND", "capability not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "CAPABILITY_DELETE_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"deleted":       true,
			"capability_id": id,
		})
	}
}

// --- Upload Skill to HubCenter Market ---

// AdminCapabilityUploadToMarketHandler uploads an enterprise skill's package
// to HubCenter's skill market for global distribution. After accept, Hub polls
// briefly so package validation failures surface to the admin instead of a
// false "uploaded" toast.
//
// Idempotency: rejects only while an in-flight submission exists
// (uploading/pending/processing). Published/failed records do not block
// version upgrades or retries.
// Authentication: passes hub_id + tenant_id for Hub-level identity at HubCenter.
func AdminCapabilityUploadToMarketHandler(svc *capability.Service, centerStatus capabilityMarketCenterStatusProvider, dataDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			writeError(w, http.StatusBadRequest, "CAPABILITY_ID_REQUIRED", "capability id is required")
			return
		}
		// Single tenant source for Get/package path/claim so paths cannot diverge
		// from capability-market scoping (AdminTenantID vs X-Tenant-ID/request).
		tenantID := AdminTenantID(r.Context())
		ctx := capability.WithTenant(r.Context(), tenantID)

		// 1. Verify capability exists.
		item, err := svc.Get(ctx, id)
		if errors.Is(err, capability.ErrNotFound) {
			writeError(w, http.StatusNotFound, "CAPABILITY_NOT_FOUND", "capability not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_GET_FAILED", err.Error())
			return
		}

		// 2. Only skill type can be uploaded.
		if !strings.EqualFold(item.CapabilityType, "skill") {
			writeError(w, http.StatusBadRequest, "NOT_A_SKILL", "only skill capabilities can be uploaded to HubCenter market")
			return
		}

		// 3. Find the skill package zip on disk.
		zipPath, err := resolveSkillPackagePath(item, dataDir, tenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "PACKAGE_NOT_FOUND", err.Error())
			return
		}

		// 3b. Strip runtime/cache artifacts and enforce HubCenter zip entry limit
		// before reserving an in-flight submission (local failures should not
		// block later retries with ALREADY_SUBMITTED).
		uploadZip, cleanupUploadZip, err := prepareSkillZipForHubCenterMarket(zipPath)
		if err != nil {
			writeError(w, http.StatusBadRequest, "PACKAGE_TOO_LARGE", err.Error())
			return
		}
		defer cleanupUploadZip()

		// 4. Get HubCenter base URL and hub identity.
		baseURL, err := hubCenterMarketplaceBaseURL(ctx, centerStatus)
		if err != nil || baseURL == "" {
			writeError(w, http.StatusBadGateway, "HUBCENTER_UNAVAILABLE", "HubCenter is not configured or unreachable")
			return
		}

		// Reconcile in-flight local records with HubCenter so vanished/failed
		// remote submissions do not keep blocking re-upload.
		if err := refreshActiveCapabilitySubmissions(ctx, svc, baseURL, item.ID); err != nil {
			writeError(w, http.StatusBadGateway, "HUBCENTER_STATUS_CHECK_FAILED", err.Error())
			return
		}
		hubID := hubCenterStateHubIDFromProvider(ctx, centerStatus)

		// 5. Determine publisher email from metadata / admin context.
		// HubCenter SubmitSkill requires a non-empty email; non-email publishers
		// (e.g. display names) cause confusing accounts and failed ownership paths.
		email := resolveMarketUploadEmail(r.Context(), item)
		if email == "" {
			writeError(w, http.StatusBadRequest, "PUBLISHER_EMAIL_REQUIRED",
				"publisher email is required to upload to HubCenter market; set publisher_email in capability metadata or use an admin account with email")
			return
		}

		// 6. Atomically reserve an in-flight row BEFORE the network upload so
		// concurrent admin clicks cannot both pass a check-then-insert window.
		sub := newMarketSubmissionRecord(tenantID, item, "", "uploading", "")
		if err := svc.ClaimMarketUpload(ctx, sub); err != nil {
			if errors.Is(err, capability.ErrMarketSubmissionInFlight) {
				writeError(w, http.StatusConflict, "ALREADY_SUBMITTED", "this skill already has an in-flight submission to HubCenter (uploading/pending/processing)")
				return
			}
			writeError(w, http.StatusInternalServerError, "LOCAL_SUBMISSION_RECORD_FAILED", err.Error())
			return
		}

		// 7. Upload to HubCenter's skill market submit endpoint.
		uploadOpts := hubCenterUploadOpts{
			BaseURL:  baseURL,
			ZipPath:  uploadZip,
			Email:    email,
			HubID:    hubID,
			TenantID: tenantID,
		}
		submissionID, err := uploadSkillToHubCenter(ctx, uploadOpts)
		if err != nil {
			_ = svc.UpdateMarketSubmissionStatus(ctx, sub.ID, "failed", err.Error())
			writeError(w, http.StatusBadGateway, "HUBCENTER_UPLOAD_FAILED", err.Error())
			return
		}
		// Bind remote id as soon as we have it. On failure leave status "uploading"
		// (still in-flight, empty remote id is expected) — do NOT fall back to
		// pending-without-id, or reconcile would clear the guard and allow duplicates.
		_ = svc.AttachMarketSubmissionRemote(ctx, sub.ID, submissionID, "pending", "")

		// 8. Wait briefly for async processor so validation failures surface here
		// instead of a false "uploaded successfully" while the skill never appears.
		finalStatus, errMsg, skillID := waitHubCenterSubmission(ctx, baseURL, submissionID, 20*time.Second)
		localStatus := mapHubCenterSubmissionStatus(finalStatus)
		if localStatus == "" {
			localStatus = "pending"
		}
		// Persist remote id + final status together (retry bind if early attach failed).
		if err := svc.AttachMarketSubmissionRemote(ctx, sub.ID, submissionID, localStatus, errMsg); err != nil {
			// Only status-update when bind fails for terminal outcomes; keep "uploading"
			// reservation if we still cannot store the remote id while in-flight.
			if localStatus == "failed" || localStatus == "published" {
				_ = svc.UpdateMarketSubmissionStatus(ctx, sub.ID, localStatus, firstNonEmpty(errMsg, "bind hubcenter submission id failed: "+err.Error()))
			}
		}

		if localStatus == "failed" {
			msg := errMsg
			if msg == "" {
				msg = "HubCenter rejected the skill package during processing"
			}
			writeError(w, http.StatusBadGateway, "HUBCENTER_PROCESSING_FAILED", humanizeHubCenterPackageError(msg))
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"submission_id":       submissionID,
			"local_submission_id": sub.ID,
			"status":              localStatus,
			"skill_id":            skillID,
		})
	}
}

// newMarketSubmissionRecord builds a local Hub tracking row for a HubCenter submit.
func newMarketSubmissionRecord(tenantID string, item *capability.CapabilitySummary, hubCenterSubmissionID, status, rejectReason string) *capability.MarketSubmission {
	now := time.Now().UTC().Format(time.RFC3339)
	name := ""
	capRef := ""
	if item != nil {
		name = item.DisplayName
		capRef = item.ID
	}
	return &capability.MarketSubmission{
		TenantID:              tenantID,
		ID:                    capability.NewID("mkt_sub"),
		CapabilityRef:         capRef,
		CapabilityName:        name,
		HubCenterSubmissionID: hubCenterSubmissionID,
		Status:                status,
		RejectReason:          rejectReason,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
}

// AdminCapabilityMarketSubmissionsHandler lists the upload submissions for
// the current tenant, showing approval progress from HubCenter.
// Status refresh is bounded and concurrent: at most 5 in-flight (pending/
// processing) submissions are checked in parallel with a 5-second overall timeout.
func AdminCapabilityMarketSubmissionsHandler(svc *capability.Service, centerStatus capabilityMarketCenterStatusProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		subs, err := svc.ListMarketSubmissions(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SUBMISSIONS_LIST_FAILED", err.Error())
			return
		}

		// Concurrent bounded status refresh with 5s overall timeout.
		baseURL, _ := hubCenterMarketplaceBaseURL(ctx, centerStatus)
		if baseURL != "" {
			const maxRefresh = 5
			refreshCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			// Collect in-flight submissions to refresh (pending + processing).
			var pendingIdxs []int
			for i, sub := range subs {
				if len(pendingIdxs) >= maxRefresh {
					break
				}
				st := strings.ToLower(strings.TrimSpace(sub.Status))
				if (st == "pending" || st == "processing") && sub.HubCenterSubmissionID != "" {
					pendingIdxs = append(pendingIdxs, i)
				}
			}

			// Refresh concurrently; normalize HubCenter statuses to local labels.
			// Only apply updates when the remote query succeeds — empty/failed polls
			// must not clobber a real "processing" row.
			type refreshResult struct {
				idx    int
				status string
				errMsg string
				ok     bool
			}
			results := make(chan refreshResult, len(pendingIdxs))
			for _, idx := range pendingIdxs {
				idx := idx
				go func() {
					status, errMsg, _, err := queryHubCenterSubmissionDetail(refreshCtx, baseURL, subs[idx].HubCenterSubmissionID)
					if err != nil || strings.TrimSpace(status) == "" {
						results <- refreshResult{idx: idx}
						return
					}
					results <- refreshResult{
						idx:    idx,
						status: mapHubCenterSubmissionStatus(status),
						errMsg: errMsg,
						ok:     true,
					}
				}()
			}
			for range pendingIdxs {
				res := <-results
				if !res.ok || res.status == "" {
					continue
				}
				if res.status != subs[res.idx].Status || (res.status == "failed" && res.errMsg != "" && res.errMsg != subs[res.idx].RejectReason) {
					subs[res.idx].Status = res.status
					subs[res.idx].RejectReason = res.errMsg
					_ = svc.UpdateMarketSubmissionStatus(ctx, subs[res.idx].ID, res.status, res.errMsg)
				}
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{"submissions": subs})
	}
}

// --- Helper types and functions ---

// hubCenterUploadOpts bundles all parameters needed to upload a skill to HubCenter.
type hubCenterUploadOpts struct {
	BaseURL  string
	ZipPath  string
	Email    string
	HubID    string // Hub's registered ID at HubCenter (for identity)
	TenantID string // Originating tenant ID
}

// refreshActiveCapabilitySubmissions reconciles in-flight local records with
// HubCenter so a vanished or failed remote submission does not permanently
// block re-upload. Terminal remote outcomes clear the in-flight guard.
func refreshActiveCapabilitySubmissions(ctx context.Context, svc *capability.Service, baseURL, capabilityRef string) error {
	subs, err := svc.ActiveMarketSubmissions(ctx, capabilityRef)
	if err != nil {
		return err
	}
	for _, sub := range subs {
		if strings.TrimSpace(sub.HubCenterSubmissionID) == "" {
			// "uploading" rows intentionally have no remote id yet (pre-network
			// reservation). Only expire stale reservations so a live upload is
			// not clobbered by a concurrent reconcile.
			if strings.EqualFold(strings.TrimSpace(sub.Status), "uploading") {
				// Must exceed hubCenter upload client timeout (5m) so large packages
				// are not expired mid-stream by a concurrent reconcile.
				if marketSubmissionTimestampStale(sub.UpdatedAt, marketUploadReservationMaxAge) {
					if err := svc.UpdateMarketSubmissionStatus(ctx, sub.ID, "failed", "upload reservation timed out"); err != nil {
						return err
					}
				}
				continue
			}
			if err := svc.UpdateMarketSubmissionStatus(ctx, sub.ID, "failed", "missing HubCenter submission id"); err != nil {
				return err
			}
			continue
		}
		status, errMsg, _, err := queryHubCenterSubmissionDetail(ctx, baseURL, sub.HubCenterSubmissionID)
		if err != nil {
			var statusErr *hubCenterSubmissionStatusError
			if errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusNotFound {
				if updateErr := svc.UpdateMarketSubmissionStatus(ctx, sub.ID, "failed", "submission no longer exists in HubCenter"); updateErr != nil {
					return updateErr
				}
				continue
			}
			// Transient HubCenter blip: leave the local in-flight row alone.
			// ClaimMarketUpload will still block re-upload with ALREADY_SUBMITTED
			// instead of failing the whole request as STATUS_CHECK_FAILED.
			continue
		}
		localStatus := mapHubCenterSubmissionStatus(status)
		if localStatus == "" {
			continue
		}
		if localStatus == sub.Status && (localStatus != "failed" || errMsg == "" || errMsg == sub.RejectReason) {
			continue
		}
		if err := svc.UpdateMarketSubmissionStatus(ctx, sub.ID, localStatus, errMsg); err != nil {
			return err
		}
	}
	return nil
}

// marketUploadReservationMaxAge is how long a local "uploading" row may block
// re-upload without a HubCenter submission id. Kept above the multipart upload
// client timeout (5m) with headroom for slow links and retries.
const marketUploadReservationMaxAge = 15 * time.Minute

// marketSubmissionTimestampStale reports whether an RFC3339 timestamp is older
// than maxAge (used to expire abandoned "uploading" reservations).
func marketSubmissionTimestampStale(raw string, maxAge time.Duration) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || maxAge <= 0 {
		return false
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		// Unknown format: treat as stale so a bad row cannot block uploads forever.
		return true
	}
	return time.Since(ts) > maxAge
}

// resolveSkillPackagePath finds the zip file for a capability on disk.
func resolveSkillPackagePath(item *capability.CapabilitySummary, dataDir, tenantID string) (string, error) {
	if item == nil {
		return "", fmt.Errorf("capability is nil")
	}
	// First check metadata_json for package_file field.
	var metadata map[string]any
	if item.MetadataJSON != "" {
		_ = json.Unmarshal([]byte(item.MetadataJSON), &metadata)
	}

	root := enterpriseSkillPackageRoot(dataDir, tenantID)
	if metadata != nil {
		if packageFile := strings.TrimSpace(stringFromMap(metadata, "package_file")); packageFile != "" {
			// Reject path traversal / non-zip package_file values from metadata.
			if path, err := enterpriseSkillPackagePath(dataDir, tenantID, packageFile); err == nil {
				return path, nil
			}
		}
	}

	// Fallback: scan the package directory for any zip matching capability_id / skill_id.
	// File names follow the pattern: {skillID}_{digest}_{checksum}.zip
	// Use prefix match (capID + separator) to avoid substring collisions
	// (e.g. "pdf" matching "pdf-converter_abc.zip").
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("cannot read skill package directory: %w", err)
	}
	prefixes := make([]string, 0, 3)
	for _, raw := range []string{
		item.CapabilityID,
		stringFromMap(metadata, "skill_id"),
		stringFromMap(metadata, "hub_skill_id"),
	} {
		raw = strings.ToLower(strings.TrimSpace(raw))
		if raw == "" {
			continue
		}
		// Strip reference wrappers so "enterprise_hub:skill:ppt-master@abc" -> "ppt-master".
		if idx := strings.LastIndex(raw, ":"); idx >= 0 {
			raw = raw[idx+1:]
		}
		if idx := strings.Index(raw, "@"); idx >= 0 {
			raw = raw[:idx]
		}
		if raw == "" {
			continue
		}
		dup := false
		for _, existing := range prefixes {
			if existing == raw {
				dup = true
				break
			}
		}
		if !dup {
			prefixes = append(prefixes, raw)
		}
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if !strings.HasSuffix(name, ".zip") {
			continue
		}
		for _, capID := range prefixes {
			if strings.HasPrefix(name, capID+"_") || strings.HasPrefix(name, capID+"-") {
				return filepath.Join(root, entry.Name()), nil
			}
		}
	}

	return "", fmt.Errorf("skill package zip not found for capability %s", item.ID)
}

// extractPublisherEmail extracts the publisher email from capability metadata.
func extractPublisherEmail(item *capability.CapabilitySummary) string {
	if item == nil || item.MetadataJSON == "" {
		return ""
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(item.MetadataJSON), &metadata); err != nil {
		return ""
	}
	if email, ok := metadata["publisher_email"].(string); ok {
		return strings.TrimSpace(email)
	}
	if email, ok := metadata["uploader_email"].(string); ok {
		return strings.TrimSpace(email)
	}
	return ""
}

// looksLikeEmail is a light check for SkillMarket identity (must contain @).
func looksLikeEmail(value string) bool {
	value = strings.TrimSpace(value)
	at := strings.Index(value, "@")
	if at <= 0 || at >= len(value)-1 {
		return false
	}
	return strings.Contains(value[at+1:], ".")
}

// resolveMarketUploadEmail picks a valid email for HubCenter submission identity.
// Order: metadata publisher_email → admin email → publisher if email-shaped.
func resolveMarketUploadEmail(ctx context.Context, item *capability.CapabilitySummary) string {
	if email := strings.TrimSpace(extractPublisherEmail(item)); looksLikeEmail(email) {
		return email
	}
	if admin := AdminFromContext(ctx); admin != nil {
		if email := strings.TrimSpace(admin.Email); looksLikeEmail(email) {
			return email
		}
	}
	if item != nil {
		if email := strings.TrimSpace(item.Publisher); looksLikeEmail(email) {
			return email
		}
	}
	return ""
}

// mapHubCenterSubmissionStatus normalizes HubCenter processor statuses into Hub local records.
// Empty input returns empty (caller must treat as "unknown / do not update"), not "pending".
func mapHubCenterSubmissionStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "":
		return ""
	case "success":
		return "published"
	case "failed":
		return "failed"
	case "processing":
		return "processing"
	case "pending":
		return "pending"
	default:
		return strings.ToLower(strings.TrimSpace(status))
	}
}

// waitHubCenterSubmission polls HubCenter until the submission reaches a terminal
// state (success/failed) or the timeout elapses. Returns the last known status.
func waitHubCenterSubmission(ctx context.Context, baseURL, submissionID string, timeout time.Duration) (status, errMsg, skillID string) {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	// Bound wait by both explicit timeout and parent context deadline.
	started := time.Now()
	deadline := started.Add(timeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	status = "pending"
	client := &http.Client{Timeout: 5 * time.Second}
	for {
		if err := ctx.Err(); err != nil {
			return status, errMsg, skillID
		}
		st, msg, sid, err := queryHubCenterSubmissionDetailWithClient(ctx, client, baseURL, submissionID)
		if err == nil && st != "" {
			status = st
			errMsg = msg
			if sid != "" {
				skillID = sid
			}
			switch strings.ToLower(st) {
			case "success", "failed":
				return status, errMsg, skillID
			}
		}
		if !time.Now().Before(deadline) {
			return status, errMsg, skillID
		}
		// Short polls first (validation often finishes in <2s), then ease off.
		wait := 250 * time.Millisecond
		elapsed := time.Since(started)
		if elapsed > 3*time.Second {
			wait = 500 * time.Millisecond
		}
		if elapsed > 10*time.Second {
			wait = 800 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return status, errMsg, skillID
		case <-time.After(wait):
		}
	}
}

// humanizeHubCenterPackageError maps HubCenter processor errors to clearer
// admin-facing messages (especially zip entry limits and zip bombs).
func humanizeHubCenterPackageError(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return msg
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "too many files") {
		return msg + "。条目数已触及防滥用上限（非“禁止必要资源”）。请去掉 node_modules/.git/venv，或合并/外置过多碎片文件后重试。"
	}
	if strings.Contains(lower, "zip bomb") || strings.Contains(lower, "total uncompressed size") || strings.Contains(lower, "file too large") {
		return msg + "。必要资源可以很多文件，但总体积与单文件大小需在限制内；请精简或拆分后重试。"
	}
	return msg
}

// uploadSkillToHubCenter uploads a skill zip to HubCenter's skill market
// using streaming I/O (io.Pipe) to avoid buffering the entire zip in memory.
// The goroutine respects context cancellation to avoid reading the entire file
// after the HTTP request has already been abandoned.
// Returns the submission_id from HubCenter.
func uploadSkillToHubCenter(ctx context.Context, opts hubCenterUploadOpts) (string, error) {
	file, err := os.Open(opts.ZipPath)
	if err != nil {
		return "", fmt.Errorf("open skill package: %w", err)
	}
	defer file.Close()

	// Use io.Pipe for streaming multipart upload — avoids buffering entire zip in memory.
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	// Write form fields and file in a goroutine.
	go func() {
		defer pw.Close()

		// Add email field.
		_ = writer.WriteField("email", opts.Email)
		// Add source field to identify this as a hub upload.
		_ = writer.WriteField("source", "enterprise_hub")
		// Add hub_id for Hub-level identity at HubCenter.
		if opts.HubID != "" {
			_ = writer.WriteField("hub_id", opts.HubID)
		}
		// Add tenant_id for traceability.
		if opts.TenantID != "" {
			_ = writer.WriteField("tenant_id", opts.TenantID)
		}

		// Add zip file.
		part, err := writer.CreateFormFile("zip", filepath.Base(opts.ZipPath))
		if err != nil {
			pw.CloseWithError(fmt.Errorf("create form file: %w", err))
			return
		}
		// Use context-aware copy: if context is cancelled (e.g. HTTP handler timeout),
		// stop reading the file immediately instead of reading the entire 100MB zip.
		if _, err := copyWithContext(ctx, part, file); err != nil {
			pw.CloseWithError(fmt.Errorf("copy zip to form: %w", err))
			return
		}
		if err := writer.Close(); err != nil {
			pw.CloseWithError(fmt.Errorf("close multipart writer: %w", err))
			return
		}
	}()

	// POST to HubCenter using the pipe reader as body (streaming, no memory buffer).
	submitURL := strings.TrimRight(opts.BaseURL, "/") + "/api/v1/skills/submit"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, submitURL, pr)
	if err != nil {
		pr.Close()
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 300 * time.Second} // 5 min for large skill packages
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload to HubCenter: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("HubCenter returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		SubmissionID string `json:"submission_id"`
		Status       string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode HubCenter response: %w", err)
	}
	if result.SubmissionID == "" {
		return "", errors.New("HubCenter returned empty submission_id")
	}
	return result.SubmissionID, nil
}

// copyWithContext copies from src to dst, checking context cancellation
// every 32KB. This ensures the goroutine stops promptly when the parent
// context is cancelled, rather than reading an entire 100MB file.
func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 32*1024)
	var total int64
	for {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			nw, writeErr := dst.Write(buf[:n])
			total += int64(nw)
			if writeErr != nil {
				return total, writeErr
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return total, nil
			}
			return total, readErr
		}
	}
}

// queryHubCenterSubmissionStatus queries HubCenter for the current status
// of a skill submission.
type hubCenterSubmissionStatusError struct{ StatusCode int }

func (e *hubCenterSubmissionStatusError) Error() string {
	return fmt.Sprintf("status check returned %d", e.StatusCode)
}

func queryHubCenterSubmissionStatus(ctx context.Context, baseURL, submissionID string) (string, error) {
	status, _, _, err := queryHubCenterSubmissionDetail(ctx, baseURL, submissionID)
	return status, err
}

// queryHubCenterSubmissionDetail returns status, error_msg, and skill_id from HubCenter.
func queryHubCenterSubmissionDetail(ctx context.Context, baseURL, submissionID string) (status, errMsg, skillID string, err error) {
	return queryHubCenterSubmissionDetailWithClient(ctx, &http.Client{Timeout: 10 * time.Second}, baseURL, submissionID)
}

func queryHubCenterSubmissionDetailWithClient(ctx context.Context, client *http.Client, baseURL, submissionID string) (status, errMsg, skillID string, err error) {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	statusURL := strings.TrimRight(baseURL, "/") + "/api/v1/skill-submissions/" + submissionID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
	if err != nil {
		return "", "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", "", &hubCenterSubmissionStatusError{StatusCode: resp.StatusCode}
	}
	var result struct {
		Status   string `json:"status"`
		ErrorMsg string `json:"error_msg"`
		SkillID  string `json:"skill_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", "", err
	}
	return result.Status, result.ErrorMsg, result.SkillID, nil
}

// hubCenterStateHubIDFromProvider extracts the Hub's registered HubID from
// the center status provider (used for Hub-level identity in uploads).
func hubCenterStateHubIDFromProvider(ctx context.Context, centerStatus capabilityMarketCenterStatusProvider) string {
	if centerStatus == nil {
		return ""
	}
	state, err := centerStatus.Status(ctx)
	if err != nil {
		return ""
	}
	return hubCenterStateHubID(state)
}
