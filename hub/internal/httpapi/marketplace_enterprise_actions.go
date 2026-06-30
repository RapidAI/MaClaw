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
// to HubCenter's skill market for global distribution. The HubCenter admin
// must approve it before it becomes visible to other tenants.
//
// Idempotency: rejects if the same capability already has a pending/approved submission.
// Authentication: passes hub_id + tenant_id for Hub-level identity at HubCenter.
func AdminCapabilityUploadToMarketHandler(svc *capability.Service, centerStatus capabilityMarketCenterStatusProvider, dataDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			writeError(w, http.StatusBadRequest, "CAPABILITY_ID_REQUIRED", "capability id is required")
			return
		}
		ctx := capabilityAdminContext(r)
		tenantID := AdminTenantID(r.Context())

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

		// 3. Idempotency guard: reject if already submitted and active.
		if hasActive, _ := svc.HasActiveSubmission(ctx, item.ID); hasActive {
			writeError(w, http.StatusConflict, "ALREADY_SUBMITTED", "this skill already has a pending or approved submission to HubCenter")
			return
		}

		// 4. Find the skill package zip on disk.
		zipPath, err := resolveSkillPackagePath(item, dataDir, tenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "PACKAGE_NOT_FOUND", err.Error())
			return
		}

		// 5. Get HubCenter base URL and hub identity.
		baseURL, err := hubCenterMarketplaceBaseURL(ctx, centerStatus)
		if err != nil || baseURL == "" {
			writeError(w, http.StatusBadGateway, "HUBCENTER_UNAVAILABLE", "HubCenter is not configured or unreachable")
			return
		}
		hubID := hubCenterStateHubIDFromProvider(ctx, centerStatus)

		// 6. Determine publisher email from metadata.
		email := extractPublisherEmail(item)
		if email == "" {
			email = item.Publisher
		}

		// 7. Upload to HubCenter's skill market submit endpoint.
		uploadOpts := hubCenterUploadOpts{
			BaseURL:  baseURL,
			ZipPath:  zipPath,
			Email:    email,
			HubID:    hubID,
			TenantID: tenantID,
		}
		submissionID, err := uploadSkillToHubCenter(ctx, uploadOpts)
		if err != nil {
			writeError(w, http.StatusBadGateway, "HUBCENTER_UPLOAD_FAILED", err.Error())
			return
		}

		// 8. Record the submission in local DB for status tracking.
		sub := &capability.MarketSubmission{
			TenantID:              tenantID,
			ID:                    capability.NewID("mkt_sub"),
			CapabilityRef:         item.ID,
			CapabilityName:        item.DisplayName,
			HubCenterSubmissionID: submissionID,
			Status:                "pending",
			CreatedAt:             time.Now().UTC().Format(time.RFC3339),
			UpdatedAt:             time.Now().UTC().Format(time.RFC3339),
		}
		if err := svc.CreateMarketSubmission(ctx, sub); err != nil {
			// Upload succeeded but local record failed — not fatal, still return success.
			writeJSON(w, http.StatusOK, map[string]any{
				"submission_id":      submissionID,
				"status":             "pending",
				"local_record_error": err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"submission_id":       submissionID,
			"local_submission_id": sub.ID,
			"status":              "pending",
		})
	}
}

// AdminCapabilityMarketSubmissionsHandler lists the upload submissions for
// the current tenant, showing approval progress from HubCenter.
// Status refresh is bounded and concurrent: at most 5 pending submissions are
// checked in parallel with a 5-second overall timeout.
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

			// Collect indices of pending submissions to refresh.
			var pendingIdxs []int
			for i, sub := range subs {
				if len(pendingIdxs) >= maxRefresh {
					break
				}
				if sub.Status == "pending" && sub.HubCenterSubmissionID != "" {
					pendingIdxs = append(pendingIdxs, i)
				}
			}

			// Refresh concurrently.
			type refreshResult struct {
				idx    int
				status string
			}
			results := make(chan refreshResult, len(pendingIdxs))
			for _, idx := range pendingIdxs {
				idx := idx
				go func() {
					status, _ := queryHubCenterSubmissionStatus(refreshCtx, baseURL, subs[idx].HubCenterSubmissionID)
					results <- refreshResult{idx: idx, status: status}
				}()
			}
			for range pendingIdxs {
				res := <-results
				if res.status != "" && res.status != subs[res.idx].Status {
					subs[res.idx].Status = res.status
					_ = svc.UpdateMarketSubmissionStatus(ctx, subs[res.idx].ID, res.status, "")
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

// resolveSkillPackagePath finds the zip file for a capability on disk.
func resolveSkillPackagePath(item *capability.CapabilitySummary, dataDir, tenantID string) (string, error) {
	// First check metadata_json for package_file field.
	var metadata map[string]any
	if item.MetadataJSON != "" {
		_ = json.Unmarshal([]byte(item.MetadataJSON), &metadata)
	}

	if metadata != nil {
		if packageFile, ok := metadata["package_file"].(string); ok && packageFile != "" {
			root := enterpriseSkillPackageRoot(dataDir, tenantID)
			path := filepath.Join(root, packageFile)
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}

	// Fallback: scan the package directory for any zip matching capability_id.
	// File names follow the pattern: {skillID}_{digest}_{checksum}.zip
	// Use prefix match (capID + separator) to avoid substring collisions
	// (e.g. "pdf" matching "pdf-converter_abc.zip").
	root := enterpriseSkillPackageRoot(dataDir, tenantID)
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("cannot read skill package directory: %w", err)
	}
	capID := strings.ToLower(item.CapabilityID)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if strings.HasSuffix(name, ".zip") && (strings.HasPrefix(name, capID+"_") || strings.HasPrefix(name, capID+"-")) {
			return filepath.Join(root, entry.Name()), nil
		}
	}

	return "", fmt.Errorf("skill package zip not found for capability %s", item.ID)
}

// extractPublisherEmail extracts the publisher email from capability metadata.
func extractPublisherEmail(item *capability.CapabilitySummary) string {
	if item.MetadataJSON == "" {
		return ""
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(item.MetadataJSON), &metadata); err != nil {
		return ""
	}
	if email, ok := metadata["publisher_email"].(string); ok {
		return email
	}
	return ""
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
func queryHubCenterSubmissionStatus(ctx context.Context, baseURL, submissionID string) (string, error) {
	statusURL := strings.TrimRight(baseURL, "/") + "/api/v1/skill-submissions/" + submissionID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status check returned %d", resp.StatusCode)
	}
	var result struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Status, nil
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
