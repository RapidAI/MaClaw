package httpapi

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/mcp"
	coreskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/capability"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const capabilityMarketPolicySettingKey = "capability_market_policy"

var errCapabilityRefAmbiguous = errors.New("capability_ref matches multiple capabilities")

const enterpriseSkillUploadMaxBytes = 10 << 20
const enterpriseSkillPackageExportMaxBytes = enterpriseSkillUploadMaxBytes
const enterpriseMaclawAppPackageSubmitMaxBytes = 2 << 20

func MarketplacePageHandler(product string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><title>Capability Marketplace</title><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{font-family:Segoe UI,Arial,sans-serif;margin:0;background:#f7f8fb;color:#20242c}.wrap{max-width:880px;margin:48px auto;padding:0 24px}.panel{background:#fff;border:1px solid #e3e7ef;border-radius:8px;padding:28px;box-shadow:0 10px 30px rgba(24,35,60,.06)}h1{margin:0 0 12px;font-size:28px}.muted{color:#657084;line-height:1.6}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:12px;margin-top:22px}.item{border:1px solid #e6eaf2;border-radius:8px;padding:14px;background:#fbfcff}.item b{display:block;margin-bottom:6px}</style></head><body><main class="wrap"><section class="panel"><h1>Capability Marketplace</h1><p class="muted">This endpoint is reserved for the unified Skill and MCP marketplace. The API is available under <code>/api/capabilities</code>; the full web UI can be mounted here.</p><div class="grid"><div class="item"><b>Skill</b><span class="muted">Packages and executable abilities.</span></div><div class="item"><b>MCP</b><span class="muted">Server configs, tools, and secret bindings.</span></div><div class="item"><b>Enterprise</b><span class="muted">Managed deployments and recommendations.</span></div></div></section></main></body></html>`))
	}
}

func CapabilityListHandler(svc *capability.Service, identityOpt ...viewerAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := marketplaceRequestContext(r, identityOpt...)
		items, err := svc.List(ctx, r.URL.Query().Get("type"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func AdminCapabilityListHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		items, err := svc.List(ctx, r.URL.Query().Get("type"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func CapabilityDetailHandler(svc *capability.Service, identityOpt ...viewerAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := marketplaceRequestContext(r, identityOpt...)
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_CAPABILITY_ID", "capability id is required")
			return
		}
		item, err := svc.Get(ctx, id)
		if errors.Is(err, capability.ErrNotFound) {
			writeError(w, http.StatusNotFound, "CAPABILITY_NOT_FOUND", "capability not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_GET_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}

func CapabilityVersionsHandler(svc *capability.Service, identityOpt ...viewerAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := marketplaceRequestContext(r, identityOpt...)
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_CAPABILITY_ID", "capability id is required")
			return
		}
		items, err := svc.ListVersions(ctx, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_VERSIONS_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

type capabilityInstallIntentRequest struct {
	CapabilityID   string          `json:"capability_id"`
	CapabilityType string          `json:"capability_type"`
	DisplayName    string          `json:"display_name,omitempty"`
	Description    string          `json:"description,omitempty"`
	Version        string          `json:"version,omitempty"`
	Source         string          `json:"source"`
	Pricing        string          `json:"pricing,omitempty"`
	Price          json.RawMessage `json:"price,omitempty"`
	License        json.RawMessage `json:"license,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	UserReason     string          `json:"user_reason,omitempty"`
}

type capabilityUpsertRequest struct {
	CapabilityType    string          `json:"capability_type"`
	Publisher         string          `json:"publisher"`
	CapabilityID      string          `json:"capability_id"`
	DisplayName       string          `json:"display_name"`
	Description       string          `json:"description,omitempty"`
	Source            string          `json:"source,omitempty"`
	ManagedBy         string          `json:"managed_by,omitempty"`
	Status            string          `json:"status,omitempty"`
	RelationToOrigin  string          `json:"relation_to_origin,omitempty"`
	GlobalKey         string          `json:"global_key,omitempty"`
	OriginKey         string          `json:"origin_key,omitempty"`
	Origin            json.RawMessage `json:"origin,omitempty"`
	Provenance        json.RawMessage `json:"provenance,omitempty"`
	Metadata          json.RawMessage `json:"metadata,omitempty"`
	Version           string          `json:"version,omitempty"`
	VersionKey        string          `json:"version_key,omitempty"`
	PackageURL        string          `json:"package_url,omitempty"`
	PackageChecksum   string          `json:"package_checksum,omitempty"`
	PackageSignature  string          `json:"package_signature,omitempty"`
	Manifest          json.RawMessage `json:"manifest,omitempty"`
	TypeConfig        json.RawMessage `json:"type_config,omitempty"`
	Permissions       json.RawMessage `json:"permissions,omitempty"`
	Pricing           json.RawMessage `json:"pricing,omitempty"`
	License           json.RawMessage `json:"license,omitempty"`
	Compatibility     json.RawMessage `json:"compatibility,omitempty"`
	VersionStatus     string          `json:"version_status,omitempty"`
	SetCurrentVersion bool            `json:"set_current_version"`
}

func AdminCapabilityUpsertHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		var req capabilityUpsertRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if strings.TrimSpace(req.CapabilityID) == "" || strings.TrimSpace(req.DisplayName) == "" {
			writeError(w, http.StatusBadRequest, "INVALID_CAPABILITY", "capability_id and display_name are required")
			return
		}
		item, err := svc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
			CapabilityType:    req.CapabilityType,
			Publisher:         req.Publisher,
			CapabilityID:      req.CapabilityID,
			DisplayName:       req.DisplayName,
			Description:       req.Description,
			Source:            req.Source,
			ManagedBy:         req.ManagedBy,
			Status:            req.Status,
			RelationToOrigin:  req.RelationToOrigin,
			GlobalKey:         req.GlobalKey,
			OriginKey:         req.OriginKey,
			OriginJSON:        rawJSONOrDefault(req.Origin),
			ProvenanceJSON:    rawJSONOrDefault(req.Provenance),
			MetadataJSON:      rawJSONOrDefault(req.Metadata),
			Version:           req.Version,
			VersionKey:        req.VersionKey,
			PackageURL:        req.PackageURL,
			PackageChecksum:   req.PackageChecksum,
			PackageSignature:  req.PackageSignature,
			ManifestJSON:      rawJSONOrDefault(req.Manifest),
			TypeConfigJSON:    rawJSONOrDefault(req.TypeConfig),
			PermissionsJSON:   rawJSONOrDefault(req.Permissions),
			PricingJSON:       rawJSONOrDefault(req.Pricing),
			LicenseJSON:       rawJSONOrDefault(req.License),
			CompatibilityJSON: rawJSONOrDefault(req.Compatibility),
			VersionStatus:     req.VersionStatus,
			SetCurrentVersion: req.SetCurrentVersion || req.Version != "",
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}

type enterpriseSkillPackageMeta struct {
	SkillID     string
	Name        string
	Description string
	Version     string
	Manifest    string
	Files       map[string]string
	Steps       []corelib.NLSkillStep
	Triggers    []string
}

type enterpriseMaclawAppPackageEntry struct {
	Entry       map[string]any
	App         map[string]any
	Governance  map[string]any
	ID          string
	Name        string
	Description string
	Kind        string
}

type maclawAppReviewIssue struct {
	Path       string `json:"path,omitempty"`
	Severity   string `json:"severity,omitempty"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

func CapabilitySkillSubmitHandler(svc *capability.Service, identity viewerAuthenticator, dataDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateMarketplaceViewer(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, enterpriseSkillUploadMaxBytes+1)
		if err := r.ParseMultipartForm(enterpriseSkillUploadMaxBytes); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_UPLOAD", "invalid multipart skill upload")
			return
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		file, header, err := r.FormFile("zip")
		if err != nil {
			writeError(w, http.StatusBadRequest, "MISSING_ZIP", "zip file is required")
			return
		}
		defer file.Close()
		if header != nil && header.Size > enterpriseSkillUploadMaxBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "UPLOAD_TOO_LARGE", "skill package is too large")
			return
		}
		root := enterpriseSkillPackageRoot(dataDir, principal.TenantID)
		if err := os.MkdirAll(root, 0o755); err != nil {
			writeError(w, http.StatusInternalServerError, "UPLOAD_STORE_FAILED", err.Error())
			return
		}
		tmp, err := os.CreateTemp(root, "upload-*.zip")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "UPLOAD_STORE_FAILED", err.Error())
			return
		}
		h := sha256.New()
		written, copyErr := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(file, enterpriseSkillUploadMaxBytes+1))
		closeErr := tmp.Close()
		if copyErr != nil || closeErr != nil {
			_ = os.Remove(tmp.Name())
			writeError(w, http.StatusInternalServerError, "UPLOAD_STORE_FAILED", firstNonEmpty(errorString(copyErr), errorString(closeErr)))
			return
		}
		if written > enterpriseSkillUploadMaxBytes {
			_ = os.Remove(tmp.Name())
			writeError(w, http.StatusRequestEntityTooLarge, "UPLOAD_TOO_LARGE", "skill package is too large")
			return
		}
		checksum := hex.EncodeToString(h.Sum(nil))
		meta, err := readEnterpriseSkillPackageMeta(tmp.Name())
		if err != nil {
			_ = os.Remove(tmp.Name())
			writeError(w, http.StatusBadRequest, "INVALID_SKILL_PACKAGE", err.Error())
			return
		}
		finalName := safeEnterpriseSkillPackageName(meta.SkillID, checksum) + ".zip"
		finalPath := filepath.Join(root, finalName)
		if err := moveEnterpriseSkillPackageIntoPlace(tmp.Name(), finalPath, checksum); err != nil {
			_ = os.Remove(tmp.Name())
			writeError(w, http.StatusInternalServerError, "UPLOAD_STORE_FAILED", err.Error())
			return
		}
		ctx := capability.WithTenant(r.Context(), principal.TenantID)
		versionKey := corelib.CapabilitySourceEnterpriseHub + ":" + corelib.CapabilityTypeSkill + ":" + meta.SkillID + "@" + checksum[:12]
		metadata := map[string]any{"skill_id": meta.SkillID, "hub_skill_id": meta.SkillID, "hub_url": enterpriseHubPublicBaseURL(r), "publisher_email": principal.Email, "uploaded_by": principal.UserID, "package_file": finalName}
		item, err := svc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
			CapabilityType:    corelib.CapabilityTypeSkill,
			Publisher:         firstNonEmpty(principal.Email, principal.UserID, "enterprise"),
			CapabilityID:      meta.SkillID,
			GlobalKey:         corelib.CapabilitySourceEnterpriseHub + ":" + corelib.CapabilityTypeSkill + ":" + meta.SkillID,
			DisplayName:       meta.Name,
			Description:       meta.Description,
			Source:            corelib.CapabilitySourceEnterpriseHub,
			ManagedBy:         "user_upload",
			Status:            "approved",
			MetadataJSON:      jsonObjectString(metadata),
			Version:           firstNonEmpty(meta.Version, checksum[:12]),
			VersionKey:        versionKey,
			PackageURL:        "/api/v1/skills/" + url.PathEscape(meta.SkillID) + "/download",
			PackageChecksum:   checksum,
			ManifestJSON:      firstNonEmpty(meta.Manifest, jsonObjectString(map[string]any{"name": meta.Name, "description": meta.Description, "type": corelib.CapabilityTypeSkill})),
			TypeConfigJSON:    jsonObjectString(map[string]any{"package_format": "maclaw-skill-market", "skill_id": meta.SkillID}),
			SetCurrentVersion: true,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"submission_id": item.CurrentVersionKey, "capability_id": item.ID, "version_key": item.CurrentVersionKey})
	}
}

func CapabilityMaclawAppSubmitHandler(svc *capability.Service, identity viewerAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateMarketplaceViewer(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, enterpriseMaclawAppPackageSubmitMaxBytes+1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_MACLAW_APP_PACKAGE", "read maclaw app package failed")
			return
		}
		if len(body) > enterpriseMaclawAppPackageSubmitMaxBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "MACLAW_APP_PACKAGE_TOO_LARGE", "maclaw app package is too large")
			return
		}
		pkg, sourceSubmissionID, err := decodeMaclawAppSubmitPackage(body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_MACLAW_APP_PACKAGE", err.Error())
			return
		}
		entries, err := parseEnterpriseMaclawAppPackageEntries(pkg)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_MACLAW_APP_PACKAGE", err.Error())
			return
		}
		checksum, err := canonicalJSONSHA256(pkg)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_MACLAW_APP_PACKAGE", err.Error())
			return
		}
		ctx := capability.WithTenant(r.Context(), principal.TenantID)
		submissions := make([]map[string]any, 0, len(entries))
		for _, entry := range entries {
			version := firstNonEmpty(stringFromMap(entry.App, "version"), checksum[:12])
			versionKey := corelib.CapabilitySourceEnterpriseHub + ":" + corelib.CapabilityTypeSkill + ":maclaw-app:" + entry.ID + "@" + checksum[:12]
			metadata := enterpriseMaclawAppCapabilityMetadata(pkg, entry, checksum, principal, sourceSubmissionID)
			item, err := svc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
				CapabilityType:    corelib.CapabilityTypeSkill,
				Publisher:         firstNonEmpty(principal.Email, principal.UserID, "enterprise"),
				CapabilityID:      entry.ID,
				GlobalKey:         corelib.CapabilitySourceEnterpriseHub + ":" + corelib.CapabilityTypeSkill + ":maclaw-app:" + entry.ID,
				DisplayName:       entry.Name,
				Description:       entry.Description,
				Source:            corelib.CapabilitySourceEnterpriseHub,
				ManagedBy:         "maclaw_app_upload",
				Status:            "pending_review",
				MetadataJSON:      jsonObjectString(metadata),
				Version:           version,
				VersionKey:        versionKey,
				PackageChecksum:   checksum,
				ManifestJSON:      jsonObjectString(entry.Entry),
				TypeConfigJSON:    jsonObjectString(map[string]any{"package_format": "maclaw.app.pack.v1", "app_id": entry.ID, "app_kind": entry.Kind}),
				CompatibilityJSON: jsonObjectString(map[string]any{"requires_maclaw_app_runtime": true}),
				VersionStatus:     "pending_review",
				SetCurrentVersion: true,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "MACLAW_APP_SUBMIT_FAILED", err.Error())
				return
			}
			submissions = append(submissions, map[string]any{
				"submission_id": item.CurrentVersionKey,
				"capability_id": item.ID,
				"app_id":        entry.ID,
				"app_name":      entry.Name,
				"status":        item.Status,
				"version_key":   item.CurrentVersionKey,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"schema":         "maclaw.app.hub_submission.v1",
			"status":         "pending_review",
			"package_sha256": checksum,
			"app_count":      len(entries),
			"submissions":    submissions,
		})
	}
}

func AdminCapabilityMaclawAppReviewHandler(svc *capability.Service, decision string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		capabilityID := strings.TrimSpace(r.PathValue("id"))
		if capabilityID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_CAPABILITY_ID", "capability id is required")
			return
		}
		var req struct {
			Reason         string                 `json:"reason,omitempty"`
			Reviewer       string                 `json:"reviewer,omitempty"`
			RiskLevel      string                 `json:"risk_level,omitempty"`
			ApprovedScopes []string               `json:"approved_scopes,omitempty"`
			ReviewIssues   []maclawAppReviewIssue `json:"review_issues,omitempty"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		item, err := svc.Get(ctx, capabilityID)
		if errors.Is(err, capability.ErrNotFound) {
			writeError(w, http.StatusNotFound, "CAPABILITY_NOT_FOUND", "capability not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_GET_FAILED", err.Error())
			return
		}
		metadata := mapFromRawJSON(json.RawMessage(item.MetadataJSON))
		if !isEnterpriseMaclawAppCapability(*item, metadata) {
			writeError(w, http.StatusBadRequest, "NOT_MACLAW_APP_CAPABILITY", "capability is not a MaClaw App submission")
			return
		}
		currentStatus := strings.TrimSpace(item.Status)
		if currentStatus == "approved" || currentStatus == "published" || currentStatus == "revoked" {
			writeError(w, http.StatusConflict, "MACLAW_APP_REVIEW_TERMINAL", "reviewed MaClaw App capability cannot be changed here")
			return
		}
		nextStatus := "approved"
		reviewState := "approved"
		if decision == "reject" {
			nextStatus = "review_failed"
			reviewState = "review_failed"
			reason := strings.TrimSpace(req.Reason)
			if reason == "" && len(req.ReviewIssues) == 0 {
				writeError(w, http.StatusBadRequest, "REJECTION_REASON_REQUIRED", "rejection reason or review_issues is required")
				return
			}
			if reason != "" && (len([]rune(reason)) < 10 || len([]rune(reason)) > 2000) {
				writeError(w, http.StatusBadRequest, "INVALID_REJECTION_REASON", "rejection reason must be 10-2000 characters")
				return
			}
		}
		reviewer := firstNonEmpty(strings.TrimSpace(req.Reviewer), "hub-admin")
		reviewedAt := time.Now().UTC().Format(time.RFC3339)
		metadata["review_state"] = reviewState
		metadata["reviewed_at"] = reviewedAt
		metadata["reviewer"] = reviewer
		if strings.TrimSpace(req.RiskLevel) != "" {
			metadata["risk_level"] = strings.TrimSpace(req.RiskLevel)
		}
		if len(req.ApprovedScopes) > 0 {
			metadata["approved_scopes"] = compactStringList(req.ApprovedScopes)
		}
		if decision == "reject" {
			issues := req.ReviewIssues
			if len(issues) == 0 {
				issues = []maclawAppReviewIssue{{
					Path:       "app.governance",
					Severity:   "error",
					Message:    strings.TrimSpace(req.Reason),
					Suggestion: "revise the MaClaw App package and resubmit",
				}}
			}
			metadata["review_issues"] = maclawAppReviewIssuesToMaps(issues)
			metadata["review_reason"] = strings.TrimSpace(req.Reason)
		} else {
			metadata["approved_at"] = reviewedAt
			delete(metadata, "review_issues")
			delete(metadata, "review_reason")
		}
		updated, err := svc.ReviewCapabilityVersion(ctx, item.ID, nextStatus, jsonObjectString(compactMetadata(metadata)))
		if errors.Is(err, capability.ErrNotFound) {
			writeError(w, http.StatusNotFound, "CAPABILITY_NOT_FOUND", "capability not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MACLAW_APP_REVIEW_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			"status":     nextStatus,
			"capability": updated,
			"review": map[string]any{
				"state":       reviewState,
				"reviewer":    reviewer,
				"reviewed_at": reviewedAt,
			},
		})
	}
}

func isEnterpriseMaclawAppCapability(item capability.CapabilitySummary, metadata map[string]any) bool {
	if metadata == nil {
		metadata = map[string]any{}
	}
	return metadata["is_maclaw_app"] == true ||
		strings.EqualFold(stringFromAny(metadata["product_kind"]), "maclaw_app_skill") ||
		strings.TrimSpace(stringFromAny(metadata["maclaw_app_id"])) != "" ||
		strings.EqualFold(strings.TrimSpace(item.ManagedBy), "maclaw_app_upload")
}

func compactStringList(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func maclawAppReviewIssuesToMaps(issues []maclawAppReviewIssue) []map[string]any {
	out := make([]map[string]any, 0, len(issues))
	for _, issue := range issues {
		message := strings.TrimSpace(issue.Message)
		if message == "" {
			continue
		}
		severity := strings.TrimSpace(issue.Severity)
		if severity == "" {
			severity = "error"
		}
		out = append(out, compactMetadata(map[string]any{
			"path":       strings.TrimSpace(issue.Path),
			"severity":   severity,
			"message":    message,
			"suggestion": strings.TrimSpace(issue.Suggestion),
		}))
	}
	return out
}

func decodeMaclawAppSubmitPackage(body []byte) (map[string]any, string, error) {
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, "", fmt.Errorf("decode maclaw app package: %w", err)
	}
	sourceSubmissionID := stringFromMap(doc, "source_submission_id")
	if pkg, ok := doc["package"].(map[string]any); ok {
		return pkg, sourceSubmissionID, nil
	}
	return doc, sourceSubmissionID, nil
}

func parseEnterpriseMaclawAppPackageEntries(pkg map[string]any) ([]enterpriseMaclawAppPackageEntry, error) {
	switch stringFromMap(pkg, "schema") {
	case "maclaw.app.pack.v1":
		if stringFromMap(pkg, "privateMarker") != "x_maclaw_apps" {
			return nil, fmt.Errorf("maclaw app package privateMarker must be x_maclaw_apps")
		}
		rawApps, ok := pkg["apps"].([]any)
		if !ok || len(rawApps) == 0 {
			return nil, fmt.Errorf("maclaw app package apps must be a non-empty array")
		}
		entries := make([]enterpriseMaclawAppPackageEntry, 0, len(rawApps))
		seen := map[string]bool{}
		for i, raw := range rawApps {
			entry, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("maclaw app package apps[%d] must be an object", i)
			}
			parsed, err := parseEnterpriseMaclawAppEntry(entry, fmt.Sprintf("maclaw app package apps[%d]", i))
			if err != nil {
				return nil, err
			}
			if seen[strings.ToLower(parsed.ID)] {
				return nil, fmt.Errorf("duplicate maclaw app id %q", parsed.ID)
			}
			seen[strings.ToLower(parsed.ID)] = true
			entries = append(entries, parsed)
		}
		return entries, nil
	case "maclaw.app.v1":
		parsed, err := parseEnterpriseMaclawAppEntry(pkg, "maclaw app")
		if err != nil {
			return nil, err
		}
		return []enterpriseMaclawAppPackageEntry{parsed}, nil
	default:
		return nil, fmt.Errorf("maclaw app package schema must be maclaw.app.v1 or maclaw.app.pack.v1")
	}
}

func parseEnterpriseMaclawAppEntry(entry map[string]any, path string) (enterpriseMaclawAppPackageEntry, error) {
	if stringFromMap(entry, "schema") != "maclaw.app.v1" {
		return enterpriseMaclawAppPackageEntry{}, fmt.Errorf("%s schema must be maclaw.app.v1", path)
	}
	if stringFromMap(entry, "privateMarker") != "x_maclaw_apps" {
		return enterpriseMaclawAppPackageEntry{}, fmt.Errorf("%s privateMarker must be x_maclaw_apps", path)
	}
	app, ok := entry["app"].(map[string]any)
	if !ok {
		return enterpriseMaclawAppPackageEntry{}, fmt.Errorf("%s app must be an object", path)
	}
	id := stringFromMap(app, "id")
	name := stringFromMap(app, "name")
	if id == "" || name == "" {
		return enterpriseMaclawAppPackageEntry{}, fmt.Errorf("%s app.id and app.name are required", path)
	}
	governance, _ := app["governance"].(map[string]any)
	return enterpriseMaclawAppPackageEntry{
		Entry:       entry,
		App:         app,
		Governance:  governance,
		ID:          id,
		Name:        name,
		Description: stringFromMap(app, "description"),
		Kind:        firstNonEmpty(stringFromMap(app, "kind"), stringFromMap(entry, "entryKind"), "tool_app"),
	}, nil
}

func enterpriseMaclawAppCapabilityMetadata(pkg map[string]any, entry enterpriseMaclawAppPackageEntry, checksum string, principal *marketplaceViewerPrincipal, sourceSubmissionID string) map[string]any {
	binding := enterpriseMaclawAppBindingForEntry(entry)
	metadata := map[string]any{
		"is_maclaw_app":                true,
		"product_kind":                 "maclaw_app_skill",
		"maclaw_app_id":                entry.ID,
		"maclaw_app_name":              entry.Name,
		"maclaw_app_description":       entry.Description,
		"maclaw_app_kind":              entry.Kind,
		"maclaw_app_definition_sha256": checksum,
		"package_schema":               firstNonEmpty(stringFromMap(pkg, "schema"), "maclaw.app.pack.v1"),
		"package_sha256":               checksum,
		"source_submission_id":         sourceSubmissionID,
		"publisher_email":              principal.Email,
		"uploaded_by":                  principal.UserID,
		"review_state":                 "pending_review",
		"x_maclaw_apps_preview":        []map[string]any{{"id": entry.ID, "name": entry.Name, "kind": entry.Kind, "description": entry.Description}},
		"submitted_package_app_count":  len(anySliceFromMap(pkg, "apps")),
	}
	if workspaceLayout := enterpriseMaclawAppWorkspaceLayoutForEntry(entry); workspaceLayout != nil {
		metadata["workspace_layout"] = workspaceLayout
		if primary := firstNonEmpty(stringFromAny(workspaceLayout["primaryRegion"]), stringFromAny(workspaceLayout["primary_region"])); primary != "" {
			metadata["workspace_layout_primary_region"] = primary
		}
		if output := firstNonEmpty(stringFromAny(workspaceLayout["outputRegion"]), stringFromAny(workspaceLayout["output_region"])); output != "" {
			metadata["workspace_layout_output_region"] = output
		}
		if regions := anySliceFromMap(workspaceLayout, "regions"); len(regions) > 0 {
			metadata["workspace_layout_region_count"] = len(regions)
			ids := make([]string, 0, len(regions))
			for _, raw := range regions {
				if region, ok := raw.(map[string]any); ok {
					if id := stringFromMap(region, "id"); id != "" {
						ids = append(ids, id)
					}
				}
			}
			if len(ids) > 0 {
				metadata["workspace_layout_region_ids"] = ids
			}
		}
	}
	if appSkill := anyMapFromMap(binding, "appSkill", "app_skill"); appSkill != nil {
		metadata["app_skill"] = appSkill
		if id := stringFromMap(appSkill, "id"); id != "" {
			metadata["app_skill_id"] = id
		}
		if version := stringFromMap(appSkill, "version"); version != "" {
			metadata["app_skill_version"] = version
		}
		if source := stringFromMap(appSkill, "source"); source != "" {
			metadata["app_skill_source"] = source
		}
	}
	if dependencies := enterpriseMaclawAppSkillDependenciesForEntry(entry); len(dependencies) > 0 {
		metadata["dependencies"] = dependencies
		metadata["skill_dependency_count"] = len(dependencies)
		requiredCount := 0
		workflowIDs := make([]string, 0, len(dependencies))
		for _, dep := range dependencies {
			if required, ok := dep["required"].(bool); !ok || required {
				requiredCount++
			}
			if strings.EqualFold(stringFromAny(dep["kind"]), "workflow_skill") {
				if id := stringFromAny(dep["id"]); id != "" {
					workflowIDs = append(workflowIDs, id)
				}
			}
		}
		metadata["required_skill_dependency_count"] = requiredCount
		if len(workflowIDs) > 0 {
			metadata["workflow_skill_ids"] = compactStringList(workflowIDs)
		}
	}
	if entry.Governance != nil {
		if resultContract := anyMapFromMap(entry.Governance, "resultContract", "result_contract"); resultContract != nil {
			metadata["result_contract"] = resultContract
		}
		if workflowContract := anyMapFromMap(entry.Governance, "workflowContract", "workflow_contract"); workflowContract != nil {
			metadata["workflow_contract"] = workflowContract
		}
		if dependencyVerification := anyMapFromMap(entry.Governance, "dependencyVerification", "dependency_verification"); dependencyVerification != nil {
			metadata["dependency_verification"] = dependencyVerification
			applyEnterpriseMaclawAppDependencyVerificationMetadata(metadata, dependencyVerification)
		}
		if testEvidence := anyMapFromMap(entry.Governance, "testEvidence", "test_evidence"); testEvidence != nil {
			metadata["test_evidence"] = testEvidence
			metadata["maclaw_app_test_evidence"] = testEvidence
			applyEnterpriseMaclawAppTestEvidenceMetadata(metadata, testEvidence)
		}
	}
	if _, ok := metadata["result_contract"]; !ok {
		if resultContract := anyMapFromMap(binding, "resultContract", "result_contract"); resultContract != nil {
			metadata["result_contract"] = resultContract
		}
	}
	if _, ok := metadata["workflow_contract"]; !ok {
		if workflowContract := anyMapFromMap(binding, "workflowContract", "workflow_contract"); workflowContract != nil {
			metadata["workflow_contract"] = workflowContract
		}
	}
	if workflowMapping := enterpriseMaclawAppWorkflowMappingForEntry(entry); workflowMapping != nil {
		metadata["workflow_mapping"] = workflowMapping
		for _, pair := range []struct {
			key  string
			meta string
		}{
			{"submitNode", "workflow_submit_node"},
			{"submit_node", "workflow_submit_node"},
			{"approvalNode", "workflow_approval_node"},
			{"approval_node", "workflow_approval_node"},
			{"resultNode", "workflow_result_node"},
			{"result_node", "workflow_result_node"},
			{"attentionNode", "workflow_attention_node"},
			{"attention_node", "workflow_attention_node"},
		} {
			if _, exists := metadata[pair.meta]; exists {
				continue
			}
			if value := stringFromMap(workflowMapping, pair.key); value != "" {
				metadata[pair.meta] = value
			}
		}
	}
	return compactMetadata(metadata)
}

func applyEnterpriseMaclawAppDependencyVerificationMetadata(metadata, verification map[string]any) {
	if metadata == nil || verification == nil {
		return
	}
	for _, pair := range []struct {
		keys []string
		meta string
	}{
		{[]string{"schema"}, "dependency_verification_schema"},
		{[]string{"verifiedAt", "verified_at"}, "dependency_verification_verified_at"},
		{[]string{"runId", "run_id"}, "dependency_verification_run_id"},
	} {
		if value := enterpriseMaclawAppFirstString(verification, pair.keys...); value != "" {
			metadata[pair.meta] = value
		}
	}
	for _, pair := range []struct {
		keys []string
		meta string
	}{
		{[]string{"dependencyCount", "dependency_count"}, "dependency_verification_dependency_count"},
		{[]string{"requiredCount", "required_count"}, "dependency_verification_required_count"},
		{[]string{"installedCount", "installed_count"}, "dependency_verification_installed_count"},
		{[]string{"missingCount", "missing_count"}, "dependency_verification_missing_count"},
		{[]string{"blockedCount", "blocked_count"}, "dependency_verification_blocked_count"},
	} {
		if value, ok := enterpriseMaclawAppFirstNumber(verification, pair.keys...); ok {
			metadata[pair.meta] = value
		}
	}
	if value, ok := enterpriseMaclawAppFirstBool(verification, "ok", "verified", "dependencyCheckPassed", "dependency_check_passed"); ok {
		metadata["dependency_verification_ok"] = value
	}
	if value, ok := enterpriseMaclawAppFirstBool(verification, "blocked", "hasBlockedDependency", "has_blocked_dependency"); ok {
		metadata["dependency_verification_blocked"] = value
	}
	skills := anySliceFromMap(verification, "skills")
	if len(skills) == 0 {
		skills = anySliceFromMap(verification, "dependencies")
	}
	if len(skills) > 0 {
		metadata["dependency_verification_skills"] = skills
		metadata["dependency_verification_skill_count"] = len(skills)
	}
	if installPlan := anyMapFromMap(verification, "installPlan", "install_plan"); installPlan != nil {
		metadata["dependency_verification_install_plan"] = installPlan
	}
}
func applyEnterpriseMaclawAppTestEvidenceMetadata(metadata, testEvidence map[string]any) {
	if metadata == nil || testEvidence == nil {
		return
	}
	for _, pair := range []struct {
		keys []string
		meta string
	}{
		{[]string{"runId", "run_id"}, "test_evidence_run_id"},
		{[]string{"verifiedAt", "verified_at"}, "test_evidence_verified_at"},
		{[]string{"definitionFingerprint", "definition_fingerprint", "definitionHash", "definition_hash"}, "test_evidence_definition_fingerprint"},
		{[]string{"testProtocolFingerprint", "test_protocol_fingerprint", "testProtocolHash", "test_protocol_hash"}, "test_evidence_test_protocol_fingerprint"},
		{[]string{"artifactName", "artifact_name"}, "test_evidence_artifact_name"},
		{[]string{"primaryResult", "primary_result"}, "test_evidence_primary_result"},
	} {
		if value := enterpriseMaclawAppFirstString(testEvidence, pair.keys...); value != "" {
			metadata[pair.meta] = value
		}
	}
	if value, ok := enterpriseMaclawAppFirstBool(testEvidence, "artifactPresent", "artifact_present"); ok {
		metadata["test_evidence_artifact_present"] = value
	}
	if value, ok := enterpriseMaclawAppFirstNumber(testEvidence, "artifactCount", "artifact_count"); ok {
		metadata["test_evidence_artifact_count"] = value
	} else if artifacts := anySliceFromMap(testEvidence, "artifacts"); len(artifacts) > 0 {
		metadata["test_evidence_artifact_count"] = len(artifacts)
	}
	if value, ok := enterpriseMaclawAppFirstNumber(testEvidence, "outputCount", "output_count"); ok {
		metadata["test_evidence_output_count"] = value
	} else {
		outputs := anySliceFromMap(testEvidence, "outputs")
		if len(outputs) == 0 {
			outputs = anySliceFromMap(testEvidence, "output_blocks")
		}
		if len(outputs) > 0 {
			metadata["test_evidence_output_count"] = len(outputs)
		}
	}
	if payload := anyMapFromMap(testEvidence, "resultPayload", "result_payload"); payload != nil {
		metadata["test_evidence_result_payload"] = payload
	}
	if outputs := anySliceFromMap(testEvidence, "outputs"); len(outputs) > 0 {
		metadata["test_evidence_outputs"] = outputs
	} else if outputs := anySliceFromMap(testEvidence, "output_blocks"); len(outputs) > 0 {
		metadata["test_evidence_outputs"] = outputs
	}
	if artifacts := anySliceFromMap(testEvidence, "artifacts"); len(artifacts) > 0 {
		metadata["test_evidence_artifacts"] = artifacts
	}
	if coverage := anyMapFromMap(testEvidence, "resultCoverage", "result_coverage"); coverage != nil {
		metadata["test_evidence_result_coverage"] = coverage
		if value, ok := enterpriseMaclawAppFirstBool(coverage, "ok"); ok {
			metadata["test_evidence_result_coverage_ok"] = value
		}
		if primary := enterpriseMaclawAppFirstString(coverage, "primary"); primary != "" {
			metadata["test_evidence_result_coverage_primary"] = primary
		}
		if covered := enterpriseMaclawAppStringList(firstEnterpriseMaclawAppPresent(coverage, "coveredTypes", "covered_types")); len(covered) > 0 {
			metadata["test_evidence_covered_types"] = covered
		}
		if missing := enterpriseMaclawAppStringList(firstEnterpriseMaclawAppPresent(coverage, "missingTypes", "missing_types")); len(missing) > 0 {
			metadata["test_evidence_missing_types"] = missing
		}
	}
	if approval := anyMapFromMap(testEvidence, "approvalInstance", "approval_instance", "approval"); approval != nil {
		metadata["test_evidence_approval_instance"] = approval
		if instanceID := firstNonEmpty(
			enterpriseMaclawAppFirstString(approval, "instanceId", "instance_id", "approvalInstanceId", "approval_instance_id", "workflowInstanceId", "workflow_instance_id"),
			enterpriseMaclawAppFirstString(approval, "approvalID", "approvalId", "approval_id"),
		); instanceID != "" {
			metadata["test_evidence_approval_instance_id"] = instanceID
		}
		if approvalID := enterpriseMaclawAppFirstString(approval, "approvalID", "approvalId", "approval_id", "recordApprovalID", "record_approval_id"); approvalID != "" {
			metadata["test_evidence_approval_id"] = approvalID
		}
		if recordID := enterpriseMaclawAppFirstString(approval, "recordID", "record_id"); recordID != "" {
			metadata["test_evidence_record_id"] = recordID
		}
		for _, pair := range []struct {
			keys []string
			meta string
		}{
			{[]string{"currentNode", "current_node"}, "test_evidence_approval_current_node"},
			{[]string{"workflowSkillId", "workflow_skill_id"}, "test_evidence_workflow_skill_id"},
			{[]string{"workflowVersion", "workflow_version"}, "test_evidence_workflow_version"},
			{[]string{"businessStatus", "business_status"}, "test_evidence_business_status"},
			{[]string{"resultStatus", "result_status"}, "test_evidence_result_status"},
			{[]string{"datasetID", "datasetId", "dataset_id"}, "test_evidence_dataset_id"},
			{[]string{"blueprintID", "blueprintId", "blueprint_id"}, "test_evidence_blueprint_id"},
			{[]string{"objectRole", "object_role", "approvalObjectRole", "approval_object_role"}, "test_evidence_object_role"},
			{[]string{"approvalEvent", "approval_event"}, "test_evidence_approval_event"},
			{[]string{"approvalWorkflowID", "approvalWorkflowId", "approval_workflow_id"}, "test_evidence_approval_workflow_id"},
			{[]string{"detailURL", "detailUrl", "detail_url"}, "test_evidence_detail_url"},
		} {
			if value := enterpriseMaclawAppFirstString(approval, pair.keys...); value != "" {
				metadata[pair.meta] = value
			}
		}
		if status := enterpriseMaclawAppFirstString(approval, "status", "approvalStatus", "approval_status", "resultStatus", "result_status"); status != "" {
			metadata["test_evidence_approval_status"] = status
		}
		if verified, ok := enterpriseMaclawAppFirstBool(approval, "approvalInstanceViewVerified", "approval_instance_view_verified", "approvalViewVerified", "approval_view_verified", "viewVerified", "view_verified"); ok {
			metadata["test_evidence_approval_view_verified"] = verified
		}
	}
}

func enterpriseMaclawAppWorkspaceLayoutForEntry(entry enterpriseMaclawAppPackageEntry) map[string]any {
	if entry.Governance != nil {
		if workspaceLayout := anyMapFromMap(entry.Governance, "workspaceLayout", "workspace_layout"); workspaceLayout != nil {
			return workspaceLayout
		}
	}
	ui := anyMapFromMap(entry.App, "ui")
	if ui == nil {
		ui = anyMapFromMap(enterpriseMaclawAppBindingForEntry(entry), "ui")
	}
	if ui == nil {
		return nil
	}
	entryName := stringFromMap(ui, "entry")
	layouts := anyMapFromMap(ui, "layouts")
	layout := anyMapFromMap(layouts, entryName)
	if layout == nil {
		return nil
	}
	out := map[string]any{
		"schema": stringFromMap(ui, "schema"),
		"entry":  entryName,
	}
	if generated, ok := ui["generated"].(bool); ok {
		out["generated"] = generated
	}
	for _, key := range []string{"template", "density", "primaryRegion", "primary_region", "outputRegion", "output_region", "navigation", "list", "regions"} {
		if value, ok := layout[key]; ok {
			out[key] = value
		}
	}
	return compactMetadata(out)
}

func enterpriseMaclawAppBindingForEntry(entry enterpriseMaclawAppPackageEntry) map[string]any {
	return anyMapFromMap(entry.App, "binding")
}

func enterpriseMaclawAppSkillDependenciesForEntry(entry enterpriseMaclawAppPackageEntry) []map[string]any {
	binding := enterpriseMaclawAppBindingForEntry(entry)
	candidates := []map[string]any{
		anyMapFromMap(binding, "dependencies"),
		anyMapFromMap(entry.Governance, "dependencies"),
	}
	out := []map[string]any{}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		for _, raw := range anySliceFromMap(candidate, "skills") {
			dep, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id := stringFromAny(dep["id"])
			if id == "" || seen[strings.ToLower(id)] {
				continue
			}
			seen[strings.ToLower(id)] = true
			out = append(out, compactMetadata(map[string]any{
				"id":           id,
				"version":      stringFromAny(dep["version"]),
				"kind":         stringFromAny(dep["kind"]),
				"required":     dep["required"],
				"source":       stringFromAny(dep["source"]),
				"install_ref":  firstNonEmpty(stringFromAny(dep["install_ref"]), stringFromAny(dep["installRef"])),
				"capabilities": dep["capabilities"],
			}))
		}
	}
	return out
}

func enterpriseMaclawAppResolvedDependenciesForEntries(entries []enterpriseMaclawAppPackageEntry) []map[string]any {
	out := []map[string]any{}
	byKey := map[string]int{}
	for _, entry := range entries {
		for _, dep := range enterpriseMaclawAppSkillDependenciesForEntry(entry) {
			id := stringFromAny(dep["id"])
			if id == "" {
				continue
			}
			kind := stringFromAny(dep["kind"])
			key := strings.ToLower(kind + ":" + id)
			if index, ok := byKey[key]; ok {
				appIDs, _ := out[index]["app_ids"].([]string)
				if !enterpriseMaclawAppStringListContains(appIDs, entry.ID) {
					out[index]["app_ids"] = append(appIDs, entry.ID)
				}
				continue
			}
			next := compactMetadata(map[string]any{
				"id":           id,
				"version":      stringFromAny(dep["version"]),
				"kind":         kind,
				"required":     dep["required"],
				"source":       stringFromAny(dep["source"]),
				"install_ref":  firstNonEmpty(stringFromAny(dep["install_ref"]), stringFromAny(dep["installRef"])),
				"capabilities": dep["capabilities"],
				"app_ids":      []string{entry.ID},
			})
			byKey[key] = len(out)
			out = append(out, next)
		}
	}
	return out
}

func enterpriseMaclawAppStringListContains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func enterpriseMaclawAppWorkflowMappingForEntry(entry enterpriseMaclawAppPackageEntry) map[string]any {
	if entry.Governance != nil {
		if workflow := anyMapFromMap(entry.Governance, "workflowMapping", "workflow_mapping"); workflow != nil {
			return workflow
		}
	}
	return anyMapFromMap(enterpriseMaclawAppBindingForEntry(entry), "workflow")
}

func CapabilityMaclawAppPackageHandler(svc *capability.Service, identity viewerAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateMarketplaceViewer(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		capabilityID := strings.TrimSpace(r.PathValue("id"))
		if capabilityID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_CAPABILITY_ID", "capability id is required")
			return
		}
		ctx := capability.WithTenant(r.Context(), principal.TenantID)
		item, err := svc.Get(ctx, capabilityID)
		if errors.Is(err, capability.ErrNotFound) {
			writeError(w, http.StatusNotFound, "CAPABILITY_NOT_FOUND", "capability not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_GET_FAILED", err.Error())
			return
		}
		metadata := mapFromRawJSON(json.RawMessage(item.MetadataJSON))
		if !isEnterpriseMaclawAppCapability(*item, metadata) {
			writeError(w, http.StatusBadRequest, "NOT_MACLAW_APP_CAPABILITY", "capability is not a MaClaw App")
			return
		}
		if !isInstallableMaclawAppStatus(item.Status) {
			writeError(w, http.StatusForbidden, "MACLAW_APP_NOT_INSTALLABLE", "MaClaw App capability is not approved for install")
			return
		}
		versions, err := svc.ListVersions(ctx, item.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_VERSIONS_FAILED", err.Error())
			return
		}
		version := currentCapabilityVersion(versions, item.CurrentVersionKey)
		if version == nil || strings.TrimSpace(version.ManifestJSON) == "" {
			writeError(w, http.StatusNotFound, "MACLAW_APP_PACKAGE_NOT_FOUND", "MaClaw App package manifest not found")
			return
		}
		entry := mapFromRawJSON(json.RawMessage(version.ManifestJSON))
		if stringFromMap(entry, "schema") != "maclaw.app.v1" {
			writeError(w, http.StatusInternalServerError, "INVALID_MACLAW_APP_PACKAGE", "stored MaClaw App manifest is invalid")
			return
		}
		applyMaclawAppReviewMetadataToEntry(entry, *item, *version, metadata)
		parsedEntry, err := parseEnterpriseMaclawAppEntry(entry, "stored maclaw app")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INVALID_MACLAW_APP_PACKAGE", err.Error())
			return
		}
		resolvedDependencies := enterpriseMaclawAppResolvedDependenciesForEntries([]enterpriseMaclawAppPackageEntry{parsedEntry})
		pkg := map[string]any{
			"schema":        "maclaw.app.pack.v1",
			"privateMarker": "x_maclaw_apps",
			"source":        "enterprise_hub",
			"capability": map[string]any{
				"id":                  item.ID,
				"capability_id":       item.CapabilityID,
				"display_name":        item.DisplayName,
				"status":              item.Status,
				"current_version_key": item.CurrentVersionKey,
			},
			"package_sha256":        firstNonEmpty(version.PackageChecksum, stringFromAny(metadata["package_sha256"])),
			"resolved_dependencies": resolvedDependencies,
			"apps":                  []any{entry},
		}
		writeJSON(w, http.StatusOK, pkg)
	}
}

func currentCapabilityVersion(versions []capability.VersionSummary, currentVersionKey string) *capability.VersionSummary {
	currentVersionKey = strings.TrimSpace(currentVersionKey)
	for i := range versions {
		if currentVersionKey != "" && strings.TrimSpace(versions[i].VersionKey) == currentVersionKey {
			return &versions[i]
		}
	}
	if len(versions) == 0 {
		return nil
	}
	return &versions[0]
}

func isInstallableMaclawAppStatus(status string) bool {
	status = strings.TrimSpace(strings.ToLower(status))
	return status == "approved" || status == "published"
}

func applyMaclawAppReviewMetadataToEntry(entry map[string]any, item capability.CapabilitySummary, version capability.VersionSummary, metadata map[string]any) {
	app := anyMapFromMap(entry, "app")
	if app == nil {
		return
	}
	governance := anyMapFromMap(app, "governance")
	if governance == nil {
		governance = map[string]any{}
		app["governance"] = governance
	}
	submission := anyMapFromMap(governance, "submission")
	if submission == nil {
		submission = map[string]any{}
		governance["submission"] = submission
	}
	submission["channel"] = "hub"
	submission["capability_id"] = item.ID
	submission["market_capability_id"] = item.CapabilityID
	submission["submission_id"] = firstNonEmpty(item.CurrentVersionKey, version.VersionKey)
	submission["version_key"] = firstNonEmpty(version.VersionKey, item.CurrentVersionKey)
	submission["status"] = firstNonEmpty(stringFromAny(metadata["review_state"]), item.Status)
	for _, key := range []string{"reviewer", "reviewed_at", "approved_at", "published_at", "risk_level", "approved_scopes", "review_issues", "review_reason"} {
		if value, ok := metadata[key]; ok {
			submission[key] = value
		}
	}
	if checksum := firstNonEmpty(version.PackageChecksum, stringFromAny(metadata["package_sha256"])); checksum != "" {
		submission["package_sha256"] = checksum
	}
}
func CapabilitySkillDownloadHandler(svc *capability.Service, identity viewerAuthenticator, dataDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateMarketplaceViewer(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		skillID := strings.TrimSpace(r.PathValue("id"))
		if skillID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_SKILL_ID", "skill id is required")
			return
		}
		packageFiles, err := enterpriseSkillPackageCandidatesForDownload(svc, r.Context(), principal.TenantID, skillID)
		if err != nil {
			writeError(w, http.StatusNotFound, "SKILL_NOT_FOUND", "skill package not found")
			return
		}
		var meta *enterpriseSkillPackageMeta
		var readErr error
		for _, packageFile := range packageFiles {
			path, err := enterpriseSkillPackagePath(dataDir, principal.TenantID, packageFile)
			if err != nil {
				continue
			}
			candidate, err := readEnterpriseSkillPackageMeta(path)
			if err != nil {
				readErr = err
				continue
			}
			if strings.EqualFold(strings.TrimSpace(candidate.SkillID), skillID) {
				meta = candidate
				break
			}
		}
		if meta == nil {
			if readErr != nil {
				writeError(w, http.StatusInternalServerError, "SKILL_PACKAGE_READ_FAILED", readErr.Error())
				return
			}
			writeError(w, http.StatusNotFound, "SKILL_NOT_FOUND", "skill package not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": meta.SkillID, "name": meta.Name, "description": meta.Description, "version": meta.Version, "trust_level": "trusted", "triggers": meta.Triggers, "steps": meta.Steps, "files": meta.Files})
	}
}

func enterpriseSkillPackageCandidatesForDownload(svc *capability.Service, ctx context.Context, tenantID, skillID string) ([]string, error) {
	if svc == nil {
		return nil, os.ErrNotExist
	}
	items, err := svc.List(capability.WithTenant(ctx, tenantID), corelib.CapabilityTypeSkill)
	if err != nil {
		return nil, err
	}
	packageFiles := []string{}
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.CapabilityID), strings.TrimSpace(skillID)) && item.Source == corelib.CapabilitySourceEnterpriseHub && item.Status == "approved" {
			metadata := mapFromRawJSON(json.RawMessage(item.MetadataJSON))
			packageFile := strings.TrimSpace(stringFromMap(metadata, "package_file"))
			if packageFile == "" {
				continue
			}
			packageFiles = append(packageFiles, packageFile)
		}
	}
	if len(packageFiles) == 0 {
		return nil, os.ErrNotExist
	}
	return packageFiles, nil
}

func enterpriseSkillPackagePath(dataDir, tenantID, packageFile string) (string, error) {
	packageFile = strings.TrimSpace(packageFile)
	if packageFile == "" || filepath.Base(packageFile) != packageFile || filepath.Ext(packageFile) != ".zip" {
		return "", os.ErrNotExist
	}
	root := enterpriseSkillPackageRoot(dataDir, tenantID)
	path := filepath.Join(root, packageFile)
	if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(root)+string(os.PathSeparator)) {
		return "", os.ErrNotExist
	}
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}

type adminMCPMarketplaceUpsertRequest struct {
	Publisher          string                          `json:"publisher"`
	CapabilityID       string                          `json:"capability_id"`
	DisplayName        string                          `json:"display_name"`
	Description        string                          `json:"description,omitempty"`
	Version            string                          `json:"version,omitempty"`
	VersionKey         string                          `json:"version_key,omitempty"`
	Status             string                          `json:"status,omitempty"`
	MCP                corelib.MCPServerEntry          `json:"mcp"`
	Pricing            json.RawMessage                 `json:"pricing,omitempty"`
	License            json.RawMessage                 `json:"license,omitempty"`
	SecretRequirements []adminMCPSecretRequirementSpec `json:"secret_requirements,omitempty"`
}

type adminMCPSecretRequirementSpec struct {
	Name          string          `json:"name"`
	Label         string          `json:"label,omitempty"`
	Scope         string          `json:"scope,omitempty"`
	StoragePolicy string          `json:"storage_policy,omitempty"`
	Required      *bool           `json:"required,omitempty"`
	HelpURL       string          `json:"help_url,omitempty"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
}

func AdminMCPMarketplaceUpsertHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		var req adminMCPMarketplaceUpsertRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if strings.TrimSpace(req.CapabilityID) == "" {
			req.CapabilityID = firstNonEmpty(req.MCP.ID, req.MCP.Name)
		}
		if strings.TrimSpace(req.DisplayName) == "" {
			req.DisplayName = firstNonEmpty(req.MCP.Name, req.CapabilityID)
		}
		if strings.TrimSpace(req.CapabilityID) == "" || strings.TrimSpace(req.DisplayName) == "" {
			writeError(w, http.StatusBadRequest, "INVALID_MCP_CAPABILITY", "capability_id or mcp.id/name is required")
			return
		}
		if strings.TrimSpace(req.MCP.EndpointURL) == "" {
			writeError(w, http.StatusBadRequest, "INVALID_MCP_CAPABILITY", "mcp.endpoint_url is required for capability market MCP capabilities")
			return
		}
		if strings.TrimSpace(req.Version) == "" {
			req.Version = "1.0.0"
		}
		metadata := map[string]any{
			"server_id":    firstNonEmpty(req.MCP.ID, req.CapabilityID),
			"name":         firstNonEmpty(req.MCP.Name, req.DisplayName),
			"endpoint_url": strings.TrimSpace(req.MCP.EndpointURL),
			"auth_type":    firstNonEmpty(req.MCP.AuthType, "none"),
		}
		if len(req.MCP.Headers) > 0 {
			metadata["headers"] = req.MCP.Headers
		}
		manifest := map[string]any{
			"name":        req.DisplayName,
			"description": req.Description,
			"type":        corelib.CapabilityTypeMCP,
		}
		item, err := svc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
			CapabilityType:    corelib.CapabilityTypeMCP,
			Publisher:         req.Publisher,
			CapabilityID:      req.CapabilityID,
			DisplayName:       req.DisplayName,
			Description:       req.Description,
			Source:            corelib.CapabilitySourceEnterpriseHub,
			ManagedBy:         "admin",
			Status:            firstNonEmpty(req.Status, "approved"),
			MetadataJSON:      jsonObjectString(metadata),
			Version:           req.Version,
			VersionKey:        req.VersionKey,
			ManifestJSON:      jsonObjectString(manifest),
			TypeConfigJSON:    jsonObjectString(req.MCP),
			PricingJSON:       rawJSONOrDefault(req.Pricing),
			LicenseJSON:       rawJSONOrDefault(req.License),
			SetCurrentVersion: true,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_CAPABILITY_SAVE_FAILED", err.Error())
			return
		}
		for _, spec := range req.SecretRequirements {
			if strings.TrimSpace(spec.Name) == "" {
				continue
			}
			required := true
			if spec.Required != nil {
				required = *spec.Required
			}
			if _, err := svc.UpsertMCPSecretRequirement(ctx, capability.MCPSecretRequirementInput{CapabilityRef: item.ID, VersionKey: item.CurrentVersionKey, Name: spec.Name, Label: spec.Label, Scope: spec.Scope, StoragePolicy: spec.StoragePolicy, Required: required, HelpURL: spec.HelpURL, MetadataJSON: rawJSONOrDefault(spec.Metadata)}); err != nil {
				writeError(w, http.StatusInternalServerError, "MCP_SECRET_REQUIREMENT_SAVE_FAILED", err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, item)
	}
}

func CapabilityInstallIntentHandler(svc *capability.Service, settings store.SystemSettingsRepository, deps ...any) http.HandlerFunc {
	var identity viewerAuthenticator
	var centerStatuses []capabilityMarketCenterStatusProvider
	for _, dep := range deps {
		switch v := dep.(type) {
		case nil:
		case viewerAuthenticator:
			identity = v
		case capabilityMarketCenterStatusProvider:
			centerStatuses = append(centerStatuses, v)
		}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := marketplaceRequestContext(r, identity)
		settings = scopedSystemSettingsForTenant(capabilityTenantIDFromRequest(r, identity), settings)
		var req capabilityInstallIntentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if req.CapabilityID == "" {
			req.CapabilityID = r.PathValue("id")
		}
		normalizeCapabilityInstallIntentRequest(&req)
		policy := corelib.DefaultCapabilityMarketPolicy()
		if settings != nil {
			if loaded, err := loadCapabilityMarketPolicy(r, settings); err == nil {
				policy = loaded
			}
		}
		var enterpriseItem *capability.CapabilitySummary
		if item, err := findEnterpriseCapabilityForInstallIntent(ctx, svc, req); err == nil {
			enterpriseItem = item
		}
		existsInEnterprise := enterpriseItem != nil && enterpriseItem.ID != ""
		decision := corelib.DecideCapabilityInstall(corelib.CapabilityInstallDecisionInput{
			Policy:             policy,
			ExistsInEnterprise: existsInEnterprise,
			Source:             req.Source,
			Pricing:            req.Pricing,
			ExternalInstallOK:  true,
			ExternalPurchaseOK: true,
		})
		resp := map[string]any{"action": decision.Action, "reason": decision.Reason}
		switch decision.Action {
		case corelib.CapabilityInstallFromEnterpriseHub:
			if enterpriseItem != nil {
				resp["capability"] = enterpriseItem
			}
			writeJSON(w, http.StatusOK, resp)
			return
		case corelib.CapabilityInstallCreateImportRequest, corelib.CapabilityInstallCreatePurchaseRequest:
			kind := "import"
			if decision.Action == corelib.CapabilityInstallCreatePurchaseRequest {
				kind = "purchase"
			}
			if existing := findOpenAcquisitionRequest(ctx, svc, req, kind); existing != nil {
				resp["request_id"] = existing.ID
				writeJSON(w, http.StatusAccepted, resp)
				return
			}
			requestID, err := svc.CreateAcquisitionRequest(ctx, capability.AcquisitionRequestInput{
				CapabilityType:      req.CapabilityType,
				Source:              req.Source,
				SourceCapabilityKey: req.CapabilityID,
				SourceVersionKey:    req.Version,
				RequestKind:         kind,
				Reason:              req.UserReason,
				PriceJSON:           rawJSONOrDefault(req.Price),
				LicenseJSON:         rawJSONOrDefault(req.License),
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "ACQUISITION_REQUEST_FAILED", err.Error())
				return
			}
			resp["request_id"] = requestID
			if decision.Action == corelib.CapabilityInstallCreateImportRequest && req.Source == corelib.CapabilitySourceHubCenter {
				var centerStatus capabilityMarketCenterStatusProvider
				if len(centerStatuses) > 0 {
					centerStatus = centerStatuses[0]
				}
				created, err := importFreeHubCenterCapabilityForIntent(ctx, svc, centerStatus, req)
				if err != nil {
					markAcquisitionImportFailed(ctx, svc, requestID, err)
					writeError(w, http.StatusBadGateway, "HUBCENTER_IMPORT_FAILED", err.Error())
					return
				}
				if created != nil {
					if err := svc.CompleteAcquisitionRequest(ctx, requestID, created.ID, jsonObjectString(map[string]any{"mode": "free_import", "source": corelib.CapabilitySourceHubCenter})); err != nil {
						writeError(w, acquisitionStatusCode(err), "ACQUISITION_COMPLETE_FAILED", err.Error())
						return
					}
					resp["capability"] = created
					writeJSON(w, http.StatusOK, resp)
					return
				}
			}
			writeJSON(w, http.StatusAccepted, resp)
			return
		case corelib.CapabilityInstallBlocked:
			writeJSON(w, http.StatusForbidden, resp)
			return
		case corelib.CapabilityInstallExternalDirect:
			if req.Source == corelib.CapabilitySourceHubCenter && len(centerStatuses) > 0 {
				var centerStatus capabilityMarketCenterStatusProvider
				centerStatus = centerStatuses[0]
				created, err := importFreeHubCenterCapabilityForIntent(ctx, svc, centerStatus, req)
				if err != nil {
					writeError(w, http.StatusBadGateway, "HUBCENTER_IMPORT_FAILED", err.Error())
					return
				}
				if created != nil {
					resp["capability"] = created
					writeJSON(w, http.StatusOK, resp)
					return
				}
			}
			writeJSON(w, http.StatusOK, resp)
			return
		default:
			writeJSON(w, http.StatusOK, resp)
			return
		}
	}
}
func normalizeCapabilityInstallIntentRequest(req *capabilityInstallIntentRequest) {
	if req == nil {
		return
	}
	req.CapabilityType = corelib.NormalizeCapabilityType(req.CapabilityType)
	req.Source = corelib.NormalizeCapabilitySource(req.Source)
	req.Pricing = strings.TrimSpace(strings.ToLower(req.Pricing))
	req.CapabilityID = strings.TrimSpace(req.CapabilityID)
	req.Version = strings.TrimSpace(req.Version)
}

func findEnterpriseCapabilityForInstallIntent(ctx context.Context, svc *capability.Service, req capabilityInstallIntentRequest) (*capability.CapabilitySummary, error) {
	if svc == nil {
		return nil, capability.ErrNotFound
	}
	capabilityID := strings.TrimSpace(req.CapabilityID)
	if capabilityID != "" {
		if item, err := svc.Get(ctx, capabilityID); err == nil && strings.TrimSpace(item.ID) != "" && installIntentMatchesEnterpriseCapability(req, *item) {
			return item, nil
		}
	}
	items, err := svc.List(ctx, strings.TrimSpace(req.CapabilityType))
	if err != nil {
		return nil, err
	}
	for i := range items {
		item := items[i]
		if !installIntentMatchesEnterpriseCapability(req, item) {
			continue
		}
		return &item, nil
	}
	return nil, capability.ErrNotFound
}

func installIntentMatchesEnterpriseCapability(req capabilityInstallIntentRequest, item capability.CapabilitySummary) bool {
	capabilityID := strings.TrimSpace(req.CapabilityID)
	if capabilityID == "" {
		return false
	}
	if strings.TrimSpace(item.Status) != "" && !isInstallableMaclawAppStatus(item.Status) {
		return false
	}
	if strings.TrimSpace(req.CapabilityType) != "" && !strings.EqualFold(strings.TrimSpace(item.CapabilityType), strings.TrimSpace(req.CapabilityType)) {
		return false
	}
	metadata := mapFromRawJSON(json.RawMessage(item.MetadataJSON))
	if !capabilityOriginMatchesInstallIntent(req, metadata) {
		return false
	}
	if strings.TrimSpace(item.CapabilityID) == capabilityID {
		return true
	}
	for _, key := range []string{"capability_id", "server_id", "skill_id", "hub_skill_id", "id"} {
		if stringFromAny(metadata[key]) == capabilityID {
			return true
		}
	}
	return false
}

func capabilityOriginMatchesInstallIntent(req capabilityInstallIntentRequest, metadata map[string]any) bool {
	source := strings.TrimSpace(strings.ToLower(req.Source))
	if source == "" || source == corelib.CapabilitySourceEnterpriseHub {
		return true
	}
	originSource := strings.TrimSpace(strings.ToLower(stringFromAny(metadata["origin_source"])))
	return originSource == source
}
func CapabilityManagedDeploymentsHandler(svc *capability.Service, identity viewerAuthenticator, groups ...userCapabilityGroupResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateMarketplaceViewer(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		ctx := capability.WithTenant(r.Context(), principal.TenantID)
		items, err := viewerEffectiveCapabilityPolicies(ctx, svc, principal.Email, firstUserCapabilityGroupResolver(groups...))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MANAGED_DEPLOYMENTS_LIST_FAILED", err.Error())
			return
		}
		deployments := make([]capability.Deployment, 0, len(items))
		for _, item := range items {
			if item.Kind != "deployment" || capability.NormalizeManagedDeploymentPolicy(item.Policy) != "required" {
				continue
			}
			deployments = append(deployments, capability.Deployment{ID: item.PolicyID, CapabilityRef: item.CapabilityRef, CapabilityVersionKey: item.CapabilityVersionKey, DeploymentPolicy: "required", ReinstallIfRemoved: item.ReinstallIfRemoved})
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": deployments})
	}
}

func CapabilityRecommendationsHandler(svc *capability.Service, identity viewerAuthenticator, groups ...userCapabilityGroupResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateMarketplaceViewer(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		ctx := capability.WithTenant(r.Context(), principal.TenantID)
		items, err := viewerEffectiveCapabilityPolicies(ctx, svc, principal.Email, firstUserCapabilityGroupResolver(groups...))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "RECOMMENDATIONS_LIST_FAILED", err.Error())
			return
		}
		recommendations := make([]capability.Recommendation, 0, len(items))
		for _, item := range items {
			if capability.NormalizeManagedDeploymentPolicy(item.Policy) != "recommended" {
				continue
			}
			recommendations = append(recommendations, capability.Recommendation{ID: item.PolicyID, CapabilityRef: item.CapabilityRef, CapabilityVersionKey: item.CapabilityVersionKey, Reason: item.RecommendationReason, AllowUserDismiss: item.AllowUserDismiss})
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": recommendations})
	}
}

func firstUserCapabilityGroupResolver(groups ...userCapabilityGroupResolver) userCapabilityGroupResolver {
	if len(groups) == 0 {
		return nil
	}
	return groups[0]
}

func viewerEffectiveCapabilityPolicies(ctx context.Context, svc *capability.Service, email string, groups userCapabilityGroupResolver) ([]adminEffectiveCapabilityPolicy, error) {
	groupChain := []string{}
	if groups != nil {
		chain, err := groups.ResolveUserGroupChain(ctx, email)
		if err != nil {
			return nil, err
		}
		groupChain = chain
	}
	return effectiveCapabilityPoliciesFor(ctx, svc, email, groupChain)
}

func UserCapabilityInventoryHandler(identity viewerAuthenticator, svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateMarketplaceViewer(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		ctx := capability.WithTenant(r.Context(), principal.TenantID)
		items, err := svc.ListUserCapabilityInventory(ctx, principal.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_INVENTORY_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func UserCapabilityInventoryUpsertHandler(identity viewerAuthenticator, svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateMarketplaceViewer(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		ctx := capability.WithTenant(r.Context(), principal.TenantID)
		type inventoryItemRequest struct {
			CapabilityRef        string          `json:"capability_ref"`
			CapabilityVersionKey string          `json:"capability_version_key,omitempty"`
			CapabilityType       string          `json:"capability_type,omitempty"`
			InstallStatus        string          `json:"install_status,omitempty"`
			Installed            *bool           `json:"installed,omitempty"`
			Metadata             json.RawMessage `json:"metadata,omitempty"`
			LastSeenAt           string          `json:"last_seen_at,omitempty"`
		}
		var req struct {
			CapabilityRef        string                 `json:"capability_ref"`
			CapabilityVersionKey string                 `json:"capability_version_key,omitempty"`
			CapabilityType       string                 `json:"capability_type,omitempty"`
			InstallStatus        string                 `json:"install_status,omitempty"`
			Installed            *bool                  `json:"installed,omitempty"`
			Metadata             json.RawMessage        `json:"metadata,omitempty"`
			LastSeenAt           string                 `json:"last_seen_at,omitempty"`
			Items                []inventoryItemRequest `json:"items,omitempty"`
			FullSnapshot         bool                   `json:"full_snapshot,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		items := req.Items
		if len(items) == 0 && req.FullSnapshot && strings.TrimSpace(req.CapabilityRef) == "" {
			if err := svc.MarkUserCapabilityInventoryMissingExcept(ctx, principal.Email, nil); err != nil {
				writeError(w, http.StatusInternalServerError, "CAPABILITY_INVENTORY_RECONCILE_FAILED", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			return
		}
		if len(items) == 0 {
			items = []inventoryItemRequest{{CapabilityRef: req.CapabilityRef, CapabilityVersionKey: req.CapabilityVersionKey, CapabilityType: req.CapabilityType, InstallStatus: req.InstallStatus, Installed: req.Installed, Metadata: req.Metadata, LastSeenAt: req.LastSeenAt}}
		}
		if len(items) > 500 {
			writeError(w, http.StatusBadRequest, "CAPABILITY_INVENTORY_TOO_LARGE", "inventory report supports at most 500 items")
			return
		}
		saved := make([]*capability.UserCapabilityInventoryItem, 0, len(items))
		seenRefs := make([]string, 0, len(items))
		for _, entry := range items {
			installed := capabilityInventoryInstalledFlag(entry.InstallStatus, entry.Installed)
			item, err := svc.UpsertUserCapabilityInventory(ctx, capability.UserCapabilityInventoryInput{UserID: principal.UserID, UserEmail: principal.Email, CapabilityRef: entry.CapabilityRef, CapabilityVersionKey: entry.CapabilityVersionKey, CapabilityType: entry.CapabilityType, InstallStatus: entry.InstallStatus, Installed: installed, MetadataJSON: rawJSONOrDefault(entry.Metadata), LastSeenAt: entry.LastSeenAt})
			if err != nil {
				writeError(w, http.StatusBadRequest, "CAPABILITY_INVENTORY_SAVE_FAILED", err.Error())
				return
			}
			saved = append(saved, item)
			seenRefs = append(seenRefs, entry.CapabilityRef)
		}
		if req.FullSnapshot {
			if err := svc.MarkUserCapabilityInventoryMissingExcept(ctx, principal.Email, seenRefs); err != nil {
				writeError(w, http.StatusInternalServerError, "CAPABILITY_INVENTORY_RECONCILE_FAILED", err.Error())
				return
			}
		}
		if len(req.Items) > 0 {
			writeJSON(w, http.StatusOK, map[string]any{"items": saved})
			return
		}
		writeJSON(w, http.StatusOK, saved[0])
	}
}

func capabilityInventoryInstalledFlag(installStatus string, explicit *bool) bool {
	if explicit != nil {
		return *explicit
	}
	switch strings.ToLower(strings.TrimSpace(installStatus)) {
	case "missing", "needs_config", "disabled", "uninstalled", "failed", "error":
		return false
	default:
		return true
	}
}

func AdminUserCapabilityInventoryHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		items, err := svc.ListUserCapabilityInventory(ctx, r.PathValue("email"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_INVENTORY_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

type userCapabilityGroupResolver interface {
	ResolveUserGroupChain(ctx context.Context, email string) ([]string, error)
}

type capabilityGroupChainResolver interface {
	ResolveGroupChain(ctx context.Context, groupID string) ([]string, error)
}

type adminEffectiveCapabilityPolicy struct {
	CapabilityRef        string                       `json:"capability_ref"`
	CapabilityVersionKey string                       `json:"capability_version_key,omitempty"`
	Policy               string                       `json:"policy"`
	Kind                 string                       `json:"kind"`
	Source               string                       `json:"source"`
	Specificity          int                          `json:"specificity"`
	Capability           capability.CapabilitySummary `json:"capability,omitempty"`
	PolicyID             string                       `json:"policy_id"`
	ReinstallIfRemoved   bool                         `json:"reinstall_if_removed,omitempty"`
	RecommendationReason string                       `json:"recommendation_reason,omitempty"`
	AllowUserDismiss     bool                         `json:"allow_user_dismiss,omitempty"`
}

type adminCapabilityComplianceItem struct {
	CapabilityRef        string                       `json:"capability_ref"`
	CapabilityVersionKey string                       `json:"capability_version_key,omitempty"`
	Policy               string                       `json:"policy"`
	Source               string                       `json:"source"`
	Status               string                       `json:"status"`
	Installed            bool                         `json:"installed"`
	InstalledVersion     string                       `json:"installed_version,omitempty"`
	InstallStatus        string                       `json:"install_status,omitempty"`
	LastSeenAt           string                       `json:"last_seen_at,omitempty"`
	Capability           capability.CapabilitySummary `json:"capability,omitempty"`
	PolicyID             string                       `json:"policy_id"`
}

type adminCapabilityComplianceSummary struct {
	Total              int `json:"total"`
	Compliant          int `json:"compliant"`
	Missing            int `json:"missing"`
	NeedsConfig        int `json:"needs_config"`
	VersionMismatch    int `json:"version_mismatch"`
	BlockedInstalled   int `json:"blocked_installed"`
	Stale              int `json:"stale"`
	UnmanagedInstalled int `json:"unmanaged_installed"`
}

func AdminUserCapabilityEffectivePoliciesHandler(svc *capability.Service, groups userCapabilityGroupResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		email := strings.ToLower(strings.TrimSpace(r.PathValue("email")))
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_EMAIL", "email is required")
			return
		}
		groupChain := []string{}
		if groups != nil {
			chain, err := groups.ResolveUserGroupChain(ctx, email)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "GROUP_CHAIN_FAILED", err.Error())
				return
			}
			groupChain = chain
		}
		items, err := effectiveCapabilityPoliciesFor(ctx, svc, email, groupChain)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "EFFECTIVE_CAPABILITY_POLICIES_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "group_chain": groupChain})
	}
}

func AdminUserCapabilityComplianceHandler(svc *capability.Service, groups userCapabilityGroupResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		email := strings.ToLower(strings.TrimSpace(r.PathValue("email")))
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_EMAIL", "email is required")
			return
		}
		groupChain := []string{}
		if groups != nil {
			chain, err := groups.ResolveUserGroupChain(ctx, email)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "GROUP_CHAIN_FAILED", err.Error())
				return
			}
			groupChain = chain
		}
		policies, err := effectiveCapabilityPoliciesFor(ctx, svc, email, groupChain)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "EFFECTIVE_CAPABILITY_POLICIES_FAILED", err.Error())
			return
		}
		inventory, err := svc.ListUserCapabilityInventory(ctx, email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_INVENTORY_LIST_FAILED", err.Error())
			return
		}
		staleAfter := complianceStaleAfter(r)
		items, summary, unmanaged := capabilityComplianceFor(policies, inventory, staleAfter)
		statusFilter := normalizeCapabilityComplianceStatusFilter(r.URL.Query().Get("status"))
		includeUnmanaged := r.URL.Query().Get("include_unmanaged")
		filteredItems := filterCapabilityComplianceItems(items, statusFilter)
		filteredUnmanaged := filterUnmanagedCapabilityInventory(unmanaged, includeUnmanaged, statusFilter)
		response := map[string]any{"items": filteredItems, "summary": summary, "unmanaged_items": filteredUnmanaged, "group_chain": groupChain, "generated_at": time.Now().UTC().Format(time.RFC3339), "stale_after_hours": int(staleAfter.Hours())}
		if capabilityComplianceFilterActive(statusFilter, includeUnmanaged) {
			response["filtered_summary"] = capabilityComplianceSummaryFor(filteredItems, filteredUnmanaged)
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func AdminGroupCapabilityEffectivePoliciesHandler(svc *capability.Service, groups capabilityGroupChainResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		groupID := strings.TrimSpace(r.PathValue("id"))
		if groupID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_GROUP", "group id is required")
			return
		}
		groupChain := []string{groupID}
		if groups != nil {
			chain, err := groups.ResolveGroupChain(ctx, groupID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "GROUP_CHAIN_FAILED", err.Error())
				return
			}
			if len(chain) > 0 {
				groupChain = chain
			}
		}
		items, err := effectiveCapabilityPoliciesFor(ctx, svc, "", groupChain)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "EFFECTIVE_CAPABILITY_POLICIES_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "group_chain": groupChain})
	}
}

func effectiveCapabilityPoliciesFor(ctx context.Context, svc *capability.Service, email string, groupChain []string) ([]adminEffectiveCapabilityPolicy, error) {
	caps, err := svc.List(ctx, "")
	if err != nil {
		return nil, err
	}
	capByID := map[string]capability.CapabilitySummary{}
	for _, cap := range caps {
		capByID[cap.ID] = cap
	}
	deployments, err := svc.ListManagedDeployments(ctx)
	if err != nil {
		return nil, err
	}
	recommendations, err := svc.ListRecommendations(ctx)
	if err != nil {
		return nil, err
	}
	byCapability := map[string]adminEffectiveCapabilityPolicy{}
	for _, item := range deployments {
		specificity, source := capabilityScopeSpecificity(item.ScopeJSON, email, groupChain)
		if specificity < 0 {
			continue
		}
		candidate := adminEffectiveCapabilityPolicy{CapabilityRef: item.CapabilityRef, CapabilityVersionKey: item.CapabilityVersionKey, Policy: capability.NormalizeManagedDeploymentPolicy(item.DeploymentPolicy), Kind: "deployment", Source: source, Specificity: specificity, Capability: capByID[item.CapabilityRef], PolicyID: item.ID, ReinstallIfRemoved: item.ReinstallIfRemoved}
		mergeEffectiveCapabilityPolicy(byCapability, candidate)
	}
	for _, item := range recommendations {
		specificity, source := capabilityScopeSpecificity(item.ScopeJSON, email, groupChain)
		if specificity < 0 {
			continue
		}
		candidate := adminEffectiveCapabilityPolicy{CapabilityRef: item.CapabilityRef, CapabilityVersionKey: item.CapabilityVersionKey, Policy: "recommended", Kind: "recommendation", Source: source, Specificity: specificity, Capability: capByID[item.CapabilityRef], PolicyID: item.ID, RecommendationReason: item.Reason, AllowUserDismiss: item.AllowUserDismiss}
		mergeEffectiveCapabilityPolicy(byCapability, candidate)
	}
	items := make([]adminEffectiveCapabilityPolicy, 0, len(byCapability))
	for _, item := range byCapability {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Specificity != items[j].Specificity {
			return items[i].Specificity > items[j].Specificity
		}
		wi := capabilityPolicyWeight(items[i].Policy)
		wj := capabilityPolicyWeight(items[j].Policy)
		if wi != wj {
			return wi > wj
		}
		return strings.TrimSpace(items[i].CapabilityRef) < strings.TrimSpace(items[j].CapabilityRef)
	})
	return items, nil
}

func capabilityComplianceFor(policies []adminEffectiveCapabilityPolicy, inventory []capability.UserCapabilityInventoryItem, staleAfter time.Duration) ([]adminCapabilityComplianceItem, adminCapabilityComplianceSummary, []capability.UserCapabilityInventoryItem) {
	invByRef := map[string]capability.UserCapabilityInventoryItem{}
	for _, item := range inventory {
		invByRef[item.CapabilityRef] = item
	}
	managedRefs := map[string]bool{}
	items := make([]adminCapabilityComplianceItem, 0, len(policies))
	summary := adminCapabilityComplianceSummary{Total: len(policies)}
	for _, policy := range policies {
		managedRefs[policy.CapabilityRef] = true
		inv, ok := invByRef[policy.CapabilityRef]
		expectedVersion := strings.TrimSpace(policy.CapabilityVersionKey)
		if expectedVersion == "" {
			expectedVersion = strings.TrimSpace(policy.Capability.CurrentVersionKey)
		}
		status := "missing"
		if strings.EqualFold(policy.Policy, "blocked") {
			if ok && inv.Installed {
				status = "blocked_installed"
			} else {
				status = "compliant"
			}
		} else if ok && strings.EqualFold(strings.TrimSpace(inv.InstallStatus), "needs_config") {
			status = "needs_config"
		} else if !ok || !inv.Installed {
			status = "missing"
		} else if inventoryIsStale(inv.LastSeenAt, staleAfter) {
			status = "stale"
		} else if expectedVersion != "" && strings.TrimSpace(inv.CapabilityVersionKey) != "" && expectedVersion != strings.TrimSpace(inv.CapabilityVersionKey) {
			status = "version_mismatch"
		} else {
			status = "compliant"
		}
		switch status {
		case "compliant":
			summary.Compliant++
		case "version_mismatch":
			summary.VersionMismatch++
		case "needs_config":
			summary.NeedsConfig++
		case "blocked_installed":
			summary.BlockedInstalled++
		case "stale":
			summary.Stale++
		default:
			summary.Missing++
		}
		items = append(items, adminCapabilityComplianceItem{CapabilityRef: policy.CapabilityRef, CapabilityVersionKey: expectedVersion, Policy: policy.Policy, Source: policy.Source, Status: status, Installed: ok && inv.Installed, InstalledVersion: inv.CapabilityVersionKey, InstallStatus: inv.InstallStatus, LastSeenAt: inv.LastSeenAt, Capability: policy.Capability, PolicyID: policy.PolicyID})
	}
	unmanaged := []capability.UserCapabilityInventoryItem{}
	for _, item := range inventory {
		if item.Installed && strings.TrimSpace(item.CapabilityRef) != "" && !managedRefs[item.CapabilityRef] {
			unmanaged = append(unmanaged, item)
		}
	}
	sort.Slice(unmanaged, func(i, j int) bool {
		return strings.TrimSpace(unmanaged[i].CapabilityRef) < strings.TrimSpace(unmanaged[j].CapabilityRef)
	})
	summary.UnmanagedInstalled = len(unmanaged)
	return items, summary, unmanaged
}

func normalizeCapabilityComplianceStatusFilter(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "", "all", "issues", "risks", "compliant", "missing", "needs_config", "version_mismatch", "blocked_installed", "stale", "unmanaged_installed":
		return status
	default:
		return ""
	}
}

func capabilityComplianceIncludeUnmanaged(include string) bool {
	include = strings.ToLower(strings.TrimSpace(include))
	switch include {
	case "false", "0", "no", "off":
		return false
	default:
		return true
	}
}

func capabilityComplianceFilterActive(status string, includeUnmanaged string) bool {
	status = normalizeCapabilityComplianceStatusFilter(status)
	if status != "" && status != "all" {
		return true
	}
	return !capabilityComplianceIncludeUnmanaged(includeUnmanaged)
}

func capabilityComplianceSummaryFor(items []adminCapabilityComplianceItem, unmanaged []capability.UserCapabilityInventoryItem) adminCapabilityComplianceSummary {
	summary := adminCapabilityComplianceSummary{Total: len(items), UnmanagedInstalled: len(unmanaged)}
	for _, item := range items {
		switch strings.ToLower(strings.TrimSpace(item.Status)) {
		case "compliant":
			summary.Compliant++
		case "version_mismatch":
			summary.VersionMismatch++
		case "needs_config":
			summary.NeedsConfig++
		case "blocked_installed":
			summary.BlockedInstalled++
		case "stale":
			summary.Stale++
		default:
			summary.Missing++
		}
	}
	return summary
}

func complianceStaleAfter(r *http.Request) time.Duration {
	hours := strings.TrimSpace(r.URL.Query().Get("stale_after_hours"))
	if hours == "" {
		return 7 * 24 * time.Hour
	}
	value, err := strconv.Atoi(hours)
	if err != nil || value <= 0 {
		return 7 * 24 * time.Hour
	}
	if value > 24*365 {
		value = 24 * 365
	}
	return time.Duration(value) * time.Hour
}

func filterCapabilityComplianceItems(items []adminCapabilityComplianceItem, status string) []adminCapabilityComplianceItem {
	status = normalizeCapabilityComplianceStatusFilter(status)
	if status == "" || status == "all" {
		return items
	}
	out := []adminCapabilityComplianceItem{}
	for _, item := range items {
		if status == "issues" || status == "risks" {
			if !strings.EqualFold(item.Status, "compliant") {
				out = append(out, item)
			}
			continue
		}
		if strings.EqualFold(item.Status, status) {
			out = append(out, item)
		}
	}
	return out
}

func filterUnmanagedCapabilityInventory(items []capability.UserCapabilityInventoryItem, include string, status string) []capability.UserCapabilityInventoryItem {
	if !capabilityComplianceIncludeUnmanaged(include) {
		return []capability.UserCapabilityInventoryItem{}
	}
	status = normalizeCapabilityComplianceStatusFilter(status)
	if status != "" && status != "all" && status != "unmanaged_installed" && status != "issues" && status != "risks" {
		return []capability.UserCapabilityInventoryItem{}
	}
	return items
}

func inventoryIsStale(lastSeenAt string, staleAfter time.Duration) bool {
	lastSeenAt = strings.TrimSpace(lastSeenAt)
	if lastSeenAt == "" {
		return false
	}
	if staleAfter <= 0 {
		staleAfter = 7 * 24 * time.Hour
	}
	ts, err := time.Parse(time.RFC3339, lastSeenAt)
	if err != nil {
		return false
	}
	return time.Since(ts) > staleAfter
}

func capabilityScopeSpecificity(scopeJSON, email string, groupChain []string) (int, string) {
	scope := mapFromRawJSON(json.RawMessage(scopeJSON))
	typeName := strings.ToLower(strings.TrimSpace(firstNonEmpty(stringFromAny(scope["type"]), stringFromAny(scope["scope"]))))
	groupIDs := stringsFromAny(scope["group_ids"])
	userEmails := stringsFromAny(scope["user_emails"])
	if v := strings.TrimSpace(stringFromAny(scope["group_id"])); v != "" {
		groupIDs = append(groupIDs, v)
	}
	if v := strings.TrimSpace(stringFromAny(scope["user_email"])); v != "" {
		userEmails = append(userEmails, v)
	}
	if typeName == "" && len(userEmails) > 0 {
		typeName = "user"
	}
	if typeName == "" && len(groupIDs) > 0 {
		typeName = "group"
	}
	if typeName == "global" || (typeName == "" && len(groupIDs) == 0 && len(userEmails) == 0) {
		return 0, "global"
	}
	if typeName == "user" {
		for _, id := range userEmails {
			if strings.EqualFold(strings.TrimSpace(id), email) {
				return 1000, "user"
			}
		}
		return -1, ""
	}
	if typeName == "group" || typeName == "department" {
		best := -1
		for _, id := range groupIDs {
			for idx, groupID := range groupChain {
				if strings.TrimSpace(id) == strings.TrimSpace(groupID) {
					score := 100 + len(groupChain) - idx
					if score > best {
						best = score
					}
				}
			}
		}
		if best >= 0 {
			return best, "group"
		}
	}
	return -1, ""
}

func mergeEffectiveCapabilityPolicy(items map[string]adminEffectiveCapabilityPolicy, candidate adminEffectiveCapabilityPolicy) {
	if strings.TrimSpace(candidate.CapabilityRef) == "" {
		return
	}
	current, ok := items[candidate.CapabilityRef]
	if !ok || effectiveCapabilityPolicyBeats(candidate, current) {
		items[candidate.CapabilityRef] = candidate
	}
}

func effectiveCapabilityPolicyBeats(candidate, current adminEffectiveCapabilityPolicy) bool {
	candidateDeploymentPolicy := candidate.Kind == "deployment" && capability.NormalizeManagedDeploymentPolicy(candidate.Policy) != "recommended"
	currentDeploymentPolicy := current.Kind == "deployment" && capability.NormalizeManagedDeploymentPolicy(current.Policy) != "recommended"
	if candidateDeploymentPolicy && !currentDeploymentPolicy {
		return true
	}
	if !candidateDeploymentPolicy && currentDeploymentPolicy {
		return false
	}
	if candidate.Specificity != current.Specificity {
		return candidate.Specificity > current.Specificity
	}
	return capabilityPolicyWeight(candidate.Policy) > capabilityPolicyWeight(current.Policy)
}

func capabilityPolicyWeight(policy string) int {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "blocked":
		return 3
	case "required":
		return 2
	default:
		return 1
	}
}

func stringsFromAny(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := []string{}
	for _, item := range items {
		if s := strings.TrimSpace(stringFromAny(item)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func findOpenAcquisitionRequest(ctx context.Context, svc *capability.Service, req capabilityInstallIntentRequest, kind string) *capability.AcquisitionRequest {
	kind = strings.TrimSpace(kind)
	if svc == nil || kind == "" {
		return nil
	}
	items, err := svc.ListAcquisitionRequests(ctx, "")
	if err != nil {
		return nil
	}
	capabilityType := strings.TrimSpace(req.CapabilityType)
	capabilityID := strings.TrimSpace(req.CapabilityID)
	source := strings.TrimSpace(req.Source)
	version := strings.TrimSpace(req.Version)
	for i := range items {
		item := items[i]
		status := strings.ToLower(strings.TrimSpace(item.Status))
		if status != "pending_review" && status != "approved" {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(item.RequestKind), kind) || !strings.EqualFold(strings.TrimSpace(item.Source), source) || strings.TrimSpace(item.SourceCapabilityKey) != capabilityID {
			continue
		}
		if capabilityType != "" && !strings.EqualFold(strings.TrimSpace(item.CapabilityType), capabilityType) {
			continue
		}
		if version != "" && strings.TrimSpace(item.SourceVersionKey) != version {
			continue
		}
		return &item
	}
	return nil
}
func markAcquisitionImportFailed(ctx context.Context, svc *capability.Service, requestID string, cause error) {
	if svc == nil || strings.TrimSpace(requestID) == "" || cause == nil {
		return
	}
	_ = svc.RejectAcquisitionRequest(ctx, requestID, "system", jsonObjectString(map[string]any{"mode": "import_failed", "error": cause.Error()}))
}
func AdminCapabilityAcquisitionRequestsHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		items, err := svc.ListAcquisitionRequests(ctx, r.URL.Query().Get("status"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ACQUISITION_REQUESTS_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func AdminCapabilityAcquisitionRequestDetailHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		item, err := svc.GetAcquisitionRequest(ctx, r.PathValue("id"))
		if errors.Is(err, capability.ErrNotFound) {
			writeError(w, http.StatusNotFound, "ACQUISITION_REQUEST_NOT_FOUND", "acquisition request not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ACQUISITION_REQUEST_GET_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}

func AdminCapabilityApproveAcquisitionHandler(svc *capability.Service, deps ...any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		var req struct {
			Approval json.RawMessage `json:"approval,omitempty"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		requestID := r.PathValue("id")
		item, getErr := svc.GetAcquisitionRequest(ctx, requestID)
		if getErr != nil && !errors.Is(getErr, capability.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "ACQUISITION_GET_FAILED", getErr.Error())
			return
		}
		if item != nil && acquisitionRequestStatusTerminal(item.Status) {
			writeError(w, http.StatusConflict, "ACQUISITION_ALREADY_TERMINAL", "terminal acquisition requests cannot be approved")
			return
		}
		if err := svc.ApproveAcquisitionRequest(ctx, requestID, "admin", rawJSONOrDefault(req.Approval)); err != nil {
			writeError(w, acquisitionStatusCode(err), "ACQUISITION_APPROVE_FAILED", err.Error())
			return
		}
		settings, centerStatus := capabilityApprovalDeps(deps...)
		settings = scopedSystemSettingsForTenant(RequestTenantID(r), settings)
		if item != nil && item.RequestKind == "purchase" && item.Source == corelib.CapabilitySourceHubCenter && strings.EqualFold(item.CapabilityType, corelib.CapabilityTypeMCP) && centerStatus != nil {
			created, purchaseJSON, err := purchaseAndImportHubCenterMCPMarketplaceEntry(ctx, svc, settings, centerStatus, *item)
			if err != nil {
				writeError(w, http.StatusBadGateway, "HUBCENTER_PURCHASE_FAILED", err.Error())
				return
			}
			if err := svc.CompleteAcquisitionRequest(ctx, requestID, created.ID, purchaseJSON); err != nil {
				writeError(w, acquisitionStatusCode(err), "ACQUISITION_COMPLETE_FAILED", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "purchased": true, "capability": created})
			return
		}
		if item != nil && item.RequestKind == "purchase" && item.Source == corelib.CapabilitySourceHubCenter && strings.EqualFold(item.CapabilityType, corelib.CapabilityTypeSkill) && centerStatus != nil {
			created, purchaseJSON, err := purchaseAndImportHubCenterSkillMarketplaceEntry(ctx, svc, settings, centerStatus, *item)
			if err != nil {
				writeError(w, http.StatusBadGateway, "HUBCENTER_PURCHASE_FAILED", err.Error())
				return
			}
			if err := svc.CompleteAcquisitionRequest(ctx, requestID, created.ID, purchaseJSON); err != nil {
				writeError(w, acquisitionStatusCode(err), "ACQUISITION_COMPLETE_FAILED", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "purchased": true, "capability": created})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func AdminCapabilityRejectAcquisitionHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		var req struct {
			Approval json.RawMessage `json:"approval,omitempty"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		requestID := r.PathValue("id")
		item, getErr := svc.GetAcquisitionRequest(ctx, requestID)
		if getErr != nil && !errors.Is(getErr, capability.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "ACQUISITION_GET_FAILED", getErr.Error())
			return
		}
		if item != nil && acquisitionRequestStatusTerminal(item.Status) {
			writeError(w, http.StatusConflict, "ACQUISITION_ALREADY_TERMINAL", "terminal acquisition requests cannot be rejected")
			return
		}
		if err := svc.RejectAcquisitionRequest(ctx, requestID, "admin", rawJSONOrDefault(req.Approval)); err != nil {
			writeError(w, acquisitionStatusCode(err), "ACQUISITION_REJECT_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func acquisitionStatusCode(err error) int {
	if errors.Is(err, capability.ErrNotFound) {
		return http.StatusNotFound
	}
	if errors.Is(err, capability.ErrInvalidState) {
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}
func acquisitionRequestStatusTerminal(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	return status == "completed" || status == "rejected"
}
func AdminCapabilityCompleteAcquisitionHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		var req struct {
			ResultCapabilityID string          `json:"result_capability_id"`
			Purchase           json.RawMessage `json:"purchase,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if strings.TrimSpace(req.ResultCapabilityID) == "" {
			writeError(w, http.StatusBadRequest, "RESULT_CAPABILITY_REQUIRED", "result_capability_id is required")
			return
		}
		requestID := r.PathValue("id")
		item, getErr := svc.GetAcquisitionRequest(ctx, requestID)
		if getErr != nil && !errors.Is(getErr, capability.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "ACQUISITION_GET_FAILED", getErr.Error())
			return
		}
		if item != nil && acquisitionRequestStatusTerminal(item.Status) {
			writeError(w, http.StatusConflict, "ACQUISITION_ALREADY_TERMINAL", "terminal acquisition requests cannot be completed")
			return
		}
		if err := svc.CompleteAcquisitionRequest(ctx, requestID, req.ResultCapabilityID, rawJSONOrDefault(req.Purchase)); err != nil {
			writeError(w, acquisitionStatusCode(err), "ACQUISITION_COMPLETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func AdminCapabilityManagedDeploymentCreateHandler(svc *capability.Service, audits ...store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		audit := firstAdminAuditRepo(audits...)
		var req struct {
			CapabilityRef        string          `json:"capability_ref"`
			CapabilityVersionKey string          `json:"capability_version_key,omitempty"`
			Scope                json.RawMessage `json:"scope,omitempty"`
			DeploymentPolicy     string          `json:"deployment_policy,omitempty"`
			ReinstallIfRemoved   *bool           `json:"reinstall_if_removed,omitempty"`
			RetryIntervalMinutes int             `json:"retry_interval_minutes,omitempty"`
			Enabled              *bool           `json:"enabled,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if strings.TrimSpace(req.CapabilityRef) == "" {
			writeError(w, http.StatusBadRequest, "INVALID_DEPLOYMENT", "capability_ref is required")
			return
		}
		capabilityRef, err := resolveCapabilityPolicyRef(ctx, svc, req.CapabilityRef)
		if errors.Is(err, capability.ErrNotFound) || errors.Is(err, errCapabilityRefAmbiguous) {
			writeError(w, http.StatusBadRequest, "INVALID_DEPLOYMENT", err.Error())
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_REF_RESOLVE_FAILED", err.Error())
			return
		}
		reinstall := true
		if req.ReinstallIfRemoved != nil {
			reinstall = *req.ReinstallIfRemoved
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		adminUserID := adminAuditUserID(r)
		scopeJSON := rawJSONOrDefault(req.Scope)
		deploymentPolicy := capability.NormalizeManagedDeploymentPolicy(req.DeploymentPolicy)
		id, err := svc.CreateManagedDeployment(ctx, capability.ManagedDeploymentInput{CapabilityRef: capabilityRef, CapabilityVersionKey: req.CapabilityVersionKey, ScopeJSON: scopeJSON, DeploymentPolicy: deploymentPolicy, ReinstallIfRemoved: reinstall, RetryIntervalMinutes: req.RetryIntervalMinutes, CreatedBy: adminUserID, Enabled: enabled})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MANAGED_DEPLOYMENT_CREATE_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminUserID, "capability.managed_deployment.create", map[string]any{"id": id, "capability_ref": capabilityRef, "capability_version_key": req.CapabilityVersionKey, "scope": json.RawMessage(scopeJSON), "deployment_policy": deploymentPolicy, "enabled": enabled})
		writeJSON(w, http.StatusCreated, map[string]any{"id": id})
	}
}

func resolveCapabilityPolicyRef(ctx context.Context, svc *capability.Service, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", capability.ErrNotFound
	}
	item, err := svc.Get(ctx, ref)
	if err == nil {
		return item.ID, nil
	}
	if !errors.Is(err, capability.ErrNotFound) {
		return "", err
	}
	items, err := svc.List(ctx, "")
	if err != nil {
		return "", err
	}
	var matched string
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.CapabilityID), ref) {
			if matched != "" && matched != item.ID {
				return "", errCapabilityRefAmbiguous
			}
			matched = item.ID
		}
	}
	if matched == "" {
		return "", capability.ErrNotFound
	}
	return matched, nil
}

func AdminCapabilityManagedDeploymentListHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		items, err := svc.ListManagedDeployments(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MANAGED_DEPLOYMENTS_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func AdminCapabilityManagedDeploymentDeleteHandler(svc *capability.Service, audits ...store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		audit := firstAdminAuditRepo(audits...)
		id := strings.TrimSpace(r.PathValue("id"))
		if err := svc.DisableManagedDeployment(ctx, id); err != nil {
			if errors.Is(err, capability.ErrNotFound) {
				writeError(w, http.StatusNotFound, "MANAGED_DEPLOYMENT_NOT_FOUND", "managed deployment not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "MANAGED_DEPLOYMENT_DELETE_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "capability.managed_deployment.delete", map[string]any{"id": id})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func AdminCapabilityRecommendationListHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		items, err := svc.ListRecommendations(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "RECOMMENDATIONS_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func AdminCapabilityRecommendationCreateHandler(svc *capability.Service, audits ...store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		audit := firstAdminAuditRepo(audits...)
		var req struct {
			CapabilityRef        string          `json:"capability_ref"`
			CapabilityVersionKey string          `json:"capability_version_key,omitempty"`
			Scope                json.RawMessage `json:"scope,omitempty"`
			Reason               string          `json:"recommendation_reason,omitempty"`
			AllowUserDismiss     *bool           `json:"allow_user_dismiss,omitempty"`
			Enabled              *bool           `json:"enabled,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if strings.TrimSpace(req.CapabilityRef) == "" {
			writeError(w, http.StatusBadRequest, "INVALID_RECOMMENDATION", "capability_ref is required")
			return
		}
		capabilityRef, err := resolveCapabilityPolicyRef(ctx, svc, req.CapabilityRef)
		if errors.Is(err, capability.ErrNotFound) || errors.Is(err, errCapabilityRefAmbiguous) {
			writeError(w, http.StatusBadRequest, "INVALID_RECOMMENDATION", err.Error())
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_REF_RESOLVE_FAILED", err.Error())
			return
		}
		allowDismiss := true
		if req.AllowUserDismiss != nil {
			allowDismiss = *req.AllowUserDismiss
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		adminUserID := adminAuditUserID(r)
		scopeJSON := rawJSONOrDefault(req.Scope)
		id, err := svc.CreateRecommendation(ctx, capability.RecommendationInput{CapabilityRef: capabilityRef, CapabilityVersionKey: req.CapabilityVersionKey, ScopeJSON: scopeJSON, Reason: req.Reason, AllowUserDismiss: allowDismiss, CreatedBy: adminUserID, Enabled: enabled})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "RECOMMENDATION_CREATE_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminUserID, "capability.recommendation.create", map[string]any{"id": id, "capability_ref": capabilityRef, "capability_version_key": req.CapabilityVersionKey, "scope": json.RawMessage(scopeJSON), "recommendation_reason": req.Reason, "enabled": enabled})
		writeJSON(w, http.StatusCreated, map[string]any{"id": id})
	}
}

func AdminCapabilityRecommendationDeleteHandler(svc *capability.Service, audits ...store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		audit := firstAdminAuditRepo(audits...)
		id := strings.TrimSpace(r.PathValue("id"))
		if err := svc.DisableRecommendation(ctx, id); err != nil {
			if errors.Is(err, capability.ErrNotFound) {
				writeError(w, http.StatusNotFound, "RECOMMENDATION_NOT_FOUND", "recommendation not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "RECOMMENDATION_DELETE_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "capability.recommendation.delete", map[string]any{"id": id})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func AdminBillingCustomerAccountHandler(deps ...any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		settings, centerStatus := capabilityApprovalDeps(deps...)
		adminEmail := hubMarketplaceAdminEmail(r.Context(), settings)
		var state *center.RegistrationState
		if centerStatus != nil {
			state, _ = centerStatus.Status(r.Context())
		}
		status := "not_configured"
		if strings.TrimSpace(adminEmail) != "" && state != nil && strings.TrimSpace(state.HubID) != "" {
			status = "configured"
		}
		account := map[string]any{
			"status":      status,
			"admin_email": adminEmail,
			"hub_id":      hubCenterStateHubID(state),
			"hubcenter":   hubCenterStateBaseURL(state),
		}
		if centerAccount, err := fetchHubCenterCustomerAccount(r.Context(), centerStatus, hubCenterStateHubID(state), adminEmail, capabilityBillingTenantIDFromRequest(r)); err == nil {
			for key, value := range centerAccount {
				account[key] = value
			}
			if _, ok := account["hub_id"]; !ok {
				account["hub_id"] = hubCenterStateHubID(state)
			}
			if _, ok := account["admin_email"]; !ok {
				account["admin_email"] = adminEmail
			}
			account["hubcenter"] = hubCenterStateBaseURL(state)
		}
		if tenantID := capabilityBillingTenantIDFromRequest(r); tenantID != "" {
			account["tenant_id"] = tenantID
		}
		writeJSON(w, http.StatusOK, account)
	}
}

func fetchHubCenterCustomerAccount(ctx context.Context, centerStatus capabilityMarketCenterStatusProvider, hubID, adminEmail, tenantID string) (map[string]any, error) {
	baseURL, err := hubCenterMarketplaceBaseURL(ctx, centerStatus)
	if err != nil {
		return nil, err
	}
	if baseURL == "" {
		return nil, errors.New("hubcenter capability market is not configured")
	}
	values := url.Values{}
	if strings.TrimSpace(hubID) != "" {
		values.Set("hub_id", strings.TrimSpace(hubID))
	}
	if strings.TrimSpace(adminEmail) != "" {
		values.Set("admin_email", strings.TrimSpace(adminEmail))
	}
	if strings.TrimSpace(tenantID) != "" {
		values.Set("tenant_id", store.NormalizeTenantID(tenantID))
	}
	endpoint := baseURL + "/api/capability-market/customer-account"
	if encoded := values.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("hubcenter customer account returned status " + resp.Status)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}
func capabilityBillingTenantIDFromContext(ctx context.Context) string {
	admin := AdminFromContext(ctx)
	if admin == nil {
		return ""
	}
	if strings.TrimSpace(admin.Scope) == "tenant" && strings.TrimSpace(admin.TenantID) != "" {
		return store.NormalizeTenantID(admin.TenantID)
	}
	return ""
}

func capabilityBillingTenantIDFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantID != "" {
		return store.NormalizeTenantID(tenantID)
	}
	return capabilityBillingTenantIDFromContext(r.Context())
}
func AdminBillingLicensesHandler(settings store.SystemSettingsRepository, centerStatus capabilityMarketCenterStatusProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		baseURL, err := hubCenterMarketplaceBaseURL(r.Context(), centerStatus)
		if err != nil {
			writeError(w, http.StatusBadGateway, "HUBCENTER_STATUS_FAILED", err.Error())
			return
		}
		if baseURL == "" {
			writeError(w, http.StatusBadGateway, "HUBCENTER_NOT_CONFIGURED", "HubCenter capability market is not configured")
			return
		}
		state, _ := centerStatus.Status(r.Context())
		adminEmail := firstNonEmpty(r.URL.Query().Get("admin_email"), r.URL.Query().Get("email"), hubMarketplaceAdminEmail(r.Context(), settings))
		values := url.Values{}
		if state != nil && strings.TrimSpace(state.HubID) != "" {
			values.Set("hub_id", strings.TrimSpace(state.HubID))
		}
		if adminEmail != "" {
			values.Set("admin_email", adminEmail)
		}
		if tenantID := capabilityBillingTenantIDFromRequest(r); tenantID != "" {
			values.Set("tenant_id", tenantID)
		}
		endpoint := baseURL + "/api/capability-market/billing/licenses"
		if encoded := values.Encode(); encoded != "" {
			endpoint += "?" + encoded
		}
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "BILLING_LICENSES_REQUEST_FAILED", err.Error())
			return
		}
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			writeError(w, http.StatusBadGateway, "HUBCENTER_LICENSES_FAILED", err.Error())
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			writeError(w, http.StatusBadGateway, "HUBCENTER_LICENSES_FAILED", "HubCenter licenses returned status "+resp.Status)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadGateway, "HUBCENTER_LICENSES_DECODE_FAILED", err.Error())
			return
		}
		payload["hub_id"] = ""
		if state != nil {
			payload["hub_id"] = state.HubID
		}
		payload["admin_email"] = adminEmail
		if tenantID := capabilityBillingTenantIDFromRequest(r); tenantID != "" {
			payload["tenant_id"] = tenantID
		}
		writeJSON(w, http.StatusOK, payload)
	}
}

func hubCenterStateHubID(state *center.RegistrationState) string {
	if state == nil {
		return ""
	}
	return strings.TrimSpace(state.HubID)
}

func hubCenterStateBaseURL(state *center.RegistrationState) string {
	if state == nil {
		return ""
	}
	return firstNonEmpty(state.ActiveBaseURL, state.BaseURL)
}

type capabilityMarketCenterStatusProvider interface {
	Status(ctx context.Context) (*center.RegistrationState, error)
}

func AdminCapabilityExternalSearchHandler(centerStatus capabilityMarketCenterStatusProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		source := corelib.NormalizeCapabilitySource(r.URL.Query().Get("source"))
		allowedSources := corelib.AdminMarketplaceSearchSources(corelib.CapabilityMarketplaceHostHub)
		if source != "" && !corelib.AdminMarketplaceCanSearchSource(corelib.CapabilityMarketplaceHostHub, source) {
			writeError(w, http.StatusForbidden, "SOURCE_NOT_ALLOWED", "source is not allowed for Hub capability market admin search")
			return
		}
		capabilityType := corelib.NormalizeCapabilityType(r.URL.Query().Get("type"))
		if capabilityType == "" {
			capabilityType = corelib.CapabilityTypeSkill
		}
		if source == corelib.CapabilitySourceHubCenter {
			var items []any
			var err error
			switch capabilityType {
			case corelib.CapabilityTypeMCP:
				items, err = searchHubCenterMCPMarketplace(r.Context(), centerStatus, r.URL.Query().Get("q"))
			case corelib.CapabilityTypeSkill:
				items, err = searchHubCenterSkillMarketplace(r.Context(), centerStatus, r.URL.Query().Get("q"))
			}
			if err != nil {
				writeError(w, http.StatusBadGateway, "HUBCENTER_SEARCH_FAILED", err.Error())
				return
			}
			if items != nil {
				writeJSON(w, http.StatusOK, map[string]any{"allowed_sources": allowedSources, "items": items})
				return
			}
		}
		if (source == corelib.CapabilitySourceClawHub || source == corelib.CapabilitySourceGitHub || source == "") && capabilityType == corelib.CapabilityTypeSkill {
			items := searchExternalSkillMarketplace(r.Context(), source, r.URL.Query().Get("q"), allowedSources)
			// When source is empty (all sources), also include HubCenter skill results
			if source == "" && centerStatus != nil {
				hubCenterItems, err := searchHubCenterSkillMarketplace(r.Context(), centerStatus, r.URL.Query().Get("q"))
				if err == nil && len(hubCenterItems) > 0 {
					items = append(hubCenterItems, items...)
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{"allowed_sources": allowedSources, "items": items})
			return
		}
		if (source == corelib.CapabilitySourceClawHub || source == corelib.CapabilitySourceGitHub || source == "") && capabilityType == corelib.CapabilityTypeMCP {
			items := searchExternalMCPMarketplace(r.Context(), source, r.URL.Query().Get("q"), allowedSources)
			// When source is empty (all sources), also include HubCenter MCP results
			if source == "" && centerStatus != nil {
				hubCenterItems, err := searchHubCenterMCPMarketplace(r.Context(), centerStatus, r.URL.Query().Get("q"))
				if err == nil && len(hubCenterItems) > 0 {
					items = append(hubCenterItems, items...)
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{"allowed_sources": allowedSources, "items": items})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"allowed_sources": allowedSources,
			"items":           []any{},
		})
	}
}

func searchExternalSkillMarketplace(ctx context.Context, source, query string, allowedSources []string) []any {
	sources := make([]string, 0, len(allowedSources))
	if source != "" {
		sources = append(sources, source)
	} else {
		for _, allowed := range allowedSources {
			if allowed == corelib.CapabilitySourceClawHub || allowed == corelib.CapabilitySourceGitHub {
				sources = append(sources, allowed)
			}
		}
	}
	results := coreskill.DefaultHubClient().SearchAllFiltered(ctx, "", query, sources)
	items := make([]any, 0, len(results))
	for _, result := range results {
		item := map[string]any{
			"id":              result.ID,
			"capability_id":   result.ID,
			"capability_type": corelib.CapabilityTypeSkill,
			"name":            result.Name,
			"display_name":    result.Name,
			"description":     result.Description,
			"version":         result.Version,
			"author":          result.Author,
			"trust_level":     result.TrustLevel,
			"avg_rating":      result.AvgRating,
			"downloads":       result.Downloads,
			"score":           result.Score,
			"source":          result.Source,
			"pricing":         corelib.CapabilityPricingFree,
		}
		if result.RepoURL != "" {
			item["repo_url"] = result.RepoURL
		}
		if result.FilePath != "" {
			item["file_path"] = result.FilePath
		}
		if result.InstallRef != "" {
			item["install_ref"] = result.InstallRef
		}
		items = append(items, item)
	}
	return items
}

// searchExternalMCPMarketplace searches ClawHub and/or GitHub for MCP Server configurations.
// When source is empty, it searches all allowed external sources (excluding hubcenter which is handled separately).
func searchExternalMCPMarketplace(ctx context.Context, source, query string, allowedSources []string) []any {
	sources := make([]string, 0, len(allowedSources))
	if source != "" {
		sources = append(sources, source)
	} else {
		for _, allowed := range allowedSources {
			if allowed == corelib.CapabilitySourceClawHub || allowed == corelib.CapabilitySourceGitHub {
				sources = append(sources, allowed)
			}
		}
	}
	results := coreskill.DefaultHubClient().SearchMCPFiltered(ctx, query, sources)
	items := make([]any, 0, len(results))
	for _, result := range results {
		item := map[string]any{
			"id":              result.ID,
			"capability_id":   result.ID,
			"capability_type": corelib.CapabilityTypeMCP,
			"name":            result.Name,
			"display_name":    result.Name,
			"description":     result.Description,
			"version":         result.Version,
			"author":          result.Author,
			"source":          result.Source,
			"pricing":         corelib.CapabilityPricingFree,
		}
		if result.RepoURL != "" {
			item["repo_url"] = result.RepoURL
		}
		if result.InstallRef != "" {
			item["install_ref"] = result.InstallRef
		}
		items = append(items, item)
	}
	return items
}

func hubCenterMarketplaceBaseURL(ctx context.Context, centerStatus capabilityMarketCenterStatusProvider) (string, error) {
	if centerStatus == nil {
		return "", nil
	}
	status, err := centerStatus.Status(ctx)
	if err != nil {
		return "", err
	}
	baseURL := firstNonEmpty(status.ActiveBaseURL, status.BaseURL)
	if baseURL == "" && len(status.BaseURLs) > 0 {
		baseURL = status.BaseURLs[0]
	}
	return strings.TrimRight(strings.TrimSpace(baseURL), "/"), nil
}

func searchHubCenterSkillMarketplace(ctx context.Context, centerStatus capabilityMarketCenterStatusProvider, query string) ([]any, error) {
	baseURL, err := hubCenterMarketplaceBaseURL(ctx, centerStatus)
	if err != nil {
		return nil, err
	}
	if baseURL == "" {
		return []any{}, nil
	}
	// Call the HubCenter capability market API directly so admin search uses the same contract as MCP search.
	endpoint := baseURL + "/api/capability-market/search"
	if strings.TrimSpace(query) != "" {
		endpoint += "?q=" + url.QueryEscape(strings.TrimSpace(query))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("hubcenter capability market returned status " + resp.Status)
	}
	var payload struct {
		Results []map[string]any `json:"results"`
		Items   []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	results := payload.Results
	if len(results) == 0 {
		results = payload.Items
	}
	items := make([]any, 0, len(results))
	for _, item := range results {
		id := firstNonEmpty(stringFromMap(item, "capability_id"), stringFromMap(item, "id"), stringFromMap(item, "slug"))
		name := firstNonEmpty(stringFromMap(item, "name"), stringFromMap(item, "display_name"), id)
		item["id"] = id
		item["capability_id"] = id
		item["name"] = name
		item["display_name"] = firstNonEmpty(stringFromMap(item, "display_name"), name)
		item["capability_type"] = corelib.CapabilityTypeSkill
		item["source"] = corelib.CapabilitySourceHubCenter
		if stringFromMap(item, "pricing") == "" {
			item["pricing"] = corelib.CapabilityPricingFree
		}
		items = append(items, item)
	}
	return items, nil
}

func stringFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	if value, ok := values[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func searchHubCenterMCPMarketplace(ctx context.Context, centerStatus capabilityMarketCenterStatusProvider, query string) ([]any, error) {
	baseURL, err := hubCenterMarketplaceBaseURL(ctx, centerStatus)
	if err != nil {
		return nil, err
	}
	if baseURL == "" {
		return []any{}, nil
	}
	endpoint := baseURL + "/api/capability-market/mcp"
	if strings.TrimSpace(query) != "" {
		endpoint += "?q=" + url.QueryEscape(strings.TrimSpace(query))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("hubcenter capability market returned status " + resp.Status)
	}
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	items := make([]any, 0, len(payload.Items))
	for _, item := range payload.Items {
		item["source"] = corelib.CapabilitySourceHubCenter
		item["capability_type"] = corelib.CapabilityTypeMCP
		items = append(items, item)
	}
	return items, nil
}

type hubCenterSkillMarketplaceEntry struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Version     string         `json:"version"`
	Author      string         `json:"author"`
	TrustLevel  string         `json:"trust_level"`
	Price       int64          `json:"price,omitempty"`
	UploaderID  string         `json:"uploader_id,omitempty"`
	Status      string         `json:"status,omitempty"`
	RequiredEnv []string       `json:"required_env,omitempty"`
	Permissions []string       `json:"permissions,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Manifest    map[string]any `json:"manifest,omitempty"`
}

type hubCenterMCPMarketplaceEntry struct {
	ID                 string                          `json:"id"`
	CapabilityType     string                          `json:"capability_type"`
	Publisher          string                          `json:"publisher"`
	CapabilityID       string                          `json:"capability_id"`
	DisplayName        string                          `json:"display_name"`
	Description        string                          `json:"description,omitempty"`
	Version            string                          `json:"version,omitempty"`
	VersionKey         string                          `json:"version_key,omitempty"`
	Pricing            json.RawMessage                 `json:"pricing,omitempty"`
	License            json.RawMessage                 `json:"license,omitempty"`
	MCP                corelib.MCPServerEntry          `json:"mcp"`
	SecretRequirements []adminMCPSecretRequirementSpec `json:"secret_requirements,omitempty"`
}

func fetchHubCenterMCPMarketplaceEntry(ctx context.Context, centerStatus capabilityMarketCenterStatusProvider, capabilityID string) (*hubCenterMCPMarketplaceEntry, error) {
	baseURL, err := hubCenterMarketplaceBaseURL(ctx, centerStatus)
	if err != nil {
		return nil, err
	}
	if baseURL == "" {
		return nil, errors.New("hubcenter capability market is not configured")
	}
	endpoint := baseURL + "/api/capability-market/mcp/" + url.PathEscape(strings.TrimSpace(capabilityID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("hubcenter capability market returned status " + resp.Status)
	}
	var item hubCenterMCPMarketplaceEntry
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, err
	}
	if strings.TrimSpace(item.CapabilityID) == "" {
		item.CapabilityID = firstNonEmpty(item.ID, item.MCP.ID, item.MCP.Name)
	}
	if strings.TrimSpace(item.DisplayName) == "" {
		item.DisplayName = firstNonEmpty(item.MCP.Name, item.CapabilityID)
	}
	return &item, nil
}

func capabilityApprovalDeps(deps ...any) (store.SystemSettingsRepository, capabilityMarketCenterStatusProvider) {
	var settings store.SystemSettingsRepository
	var centerStatus capabilityMarketCenterStatusProvider
	for _, dep := range deps {
		if dep == nil {
			continue
		}
		if v, ok := dep.(store.SystemSettingsRepository); ok {
			settings = v
		}
		if v, ok := dep.(capabilityMarketCenterStatusProvider); ok {
			centerStatus = v
		}
	}
	return settings, centerStatus
}

func fetchHubCenterSkillMarketplaceEntry(ctx context.Context, centerStatus capabilityMarketCenterStatusProvider, skillID string) (*hubCenterSkillMarketplaceEntry, error) {
	baseURL, err := hubCenterMarketplaceBaseURL(ctx, centerStatus)
	if err != nil {
		return nil, err
	}
	if baseURL == "" {
		return nil, errors.New("hubcenter capability market is not configured")
	}
	endpoint := baseURL + "/api/v1/skills/" + url.PathEscape(strings.TrimSpace(skillID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("hubcenter skill capability market returned status " + resp.Status)
	}
	var item hubCenterSkillMarketplaceEntry
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, err
	}
	if strings.TrimSpace(item.ID) == "" {
		item.ID = strings.TrimSpace(skillID)
	}
	if strings.TrimSpace(item.Name) == "" {
		item.Name = item.ID
	}
	return &item, nil
}

func importHubCenterSkillMarketplaceEntry(ctx context.Context, svc *capability.Service, centerStatus capabilityMarketCenterStatusProvider, skillID string) (*capability.CapabilitySummary, error) {
	baseURL, err := hubCenterMarketplaceBaseURL(ctx, centerStatus)
	if err != nil {
		return nil, err
	}
	item, err := fetchHubCenterSkillMarketplaceEntry(ctx, centerStatus, skillID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(item.Version) == "" {
		item.Version = "1.0.0"
	}
	publisher := firstNonEmpty(item.Author, item.UploaderID, "hubcenter")
	metadata := map[string]any{
		"skill_id":      item.ID,
		"hub_skill_id":  item.ID,
		"hub_url":       baseURL,
		"skill_hub_url": baseURL,
		"origin_source": corelib.CapabilitySourceHubCenter,
		"trust_level":   item.TrustLevel,
	}
	if item.Price > 0 {
		metadata["pricing"] = corelib.CapabilityPricingPaid
	}
	manifest := map[string]any{
		"name":        item.Name,
		"description": item.Description,
		"type":        corelib.CapabilityTypeSkill,
		"tags":        item.Tags,
	}
	origin := map[string]any{
		"source":        corelib.CapabilitySourceHubCenter,
		"capability_id": item.ID,
		"version":       item.Version,
	}
	pricing := map[string]any{"mode": corelib.CapabilityPricingFree}
	if item.Price > 0 {
		pricing = map[string]any{"mode": corelib.CapabilityPricingPaid, "credits": item.Price}
	}
	return svc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
		CapabilityType:    corelib.CapabilityTypeSkill,
		Publisher:         publisher,
		CapabilityID:      item.ID,
		DisplayName:       item.Name,
		Description:       item.Description,
		Source:            corelib.CapabilitySourceEnterpriseHub,
		ManagedBy:         "admin",
		Status:            "approved",
		RelationToOrigin:  "mirrored",
		OriginKey:         corelib.CapabilitySourceHubCenter + ":" + corelib.CapabilityTypeSkill + ":" + publisher + ":" + item.ID,
		OriginJSON:        jsonObjectString(origin),
		MetadataJSON:      jsonObjectString(metadata),
		Version:           item.Version,
		ManifestJSON:      jsonObjectString(manifest),
		TypeConfigJSON:    jsonObjectString(item),
		PricingJSON:       jsonObjectString(pricing),
		SetCurrentVersion: true,
	})
}

func purchaseAndImportHubCenterSkillMarketplaceEntry(ctx context.Context, svc *capability.Service, settings store.SystemSettingsRepository, centerStatus capabilityMarketCenterStatusProvider, req capability.AcquisitionRequest) (*capability.CapabilitySummary, string, error) {
	baseURL, err := hubCenterMarketplaceBaseURL(ctx, centerStatus)
	if err != nil {
		return nil, "", err
	}
	if baseURL == "" {
		return nil, "", errors.New("hubcenter capability market is not configured")
	}
	adminEmail := hubMarketplaceAdminEmail(ctx, settings)
	if adminEmail == "" {
		return nil, "", errors.New("admin email is required for paid Skill purchase")
	}
	endpoint := baseURL + "/api/capability-market/capabilities/" + url.PathEscape(strings.TrimSpace(req.SourceCapabilityKey)) + "/download?email=" + url.QueryEscape(adminEmail)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", errors.New("hubcenter Skill purchase returned status " + resp.Status)
	}
	var purchase map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&purchase); err != nil {
		return nil, "", err
	}
	delete(purchase, "encrypted_data")
	purchase["admin_email"] = adminEmail
	purchase["source"] = corelib.CapabilitySourceHubCenter
	created, err := importHubCenterSkillMarketplaceEntry(ctx, svc, centerStatus, req.SourceCapabilityKey)
	if err != nil {
		return nil, "", err
	}
	return created, jsonObjectString(purchase), nil
}

func purchaseAndImportHubCenterMCPMarketplaceEntry(ctx context.Context, svc *capability.Service, settings store.SystemSettingsRepository, centerStatus capabilityMarketCenterStatusProvider, req capability.AcquisitionRequest) (*capability.CapabilitySummary, string, error) {
	baseURL, err := hubCenterMarketplaceBaseURL(ctx, centerStatus)
	if err != nil {
		return nil, "", err
	}
	if baseURL == "" {
		return nil, "", errors.New("hubcenter capability market is not configured")
	}
	status, _ := centerStatus.Status(ctx)
	adminEmail := hubMarketplaceAdminEmail(ctx, settings)
	if adminEmail == "" {
		return nil, "", errors.New("admin email is required for paid MCP purchase")
	}
	payload := map[string]any{
		"hub_id":      "",
		"admin_email": adminEmail,
		"request_id":  req.ID,
	}
	if status != nil {
		payload["hub_id"] = status.HubID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	endpoint := baseURL + "/api/capability-market/mcp/" + url.PathEscape(strings.TrimSpace(req.SourceCapabilityKey)) + "/purchase"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", errors.New("hubcenter MCP purchase returned status " + resp.Status)
	}
	var purchase map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&purchase); err != nil {
		return nil, "", err
	}
	created, err := importHubCenterMCPMarketplaceEntry(ctx, svc, centerStatus, req.SourceCapabilityKey)
	if err != nil {
		return nil, "", err
	}
	return created, jsonObjectString(purchase), nil
}

func hubMarketplaceAdminEmail(ctx context.Context, settings store.SystemSettingsRepository) string {
	if settings == nil {
		return ""
	}
	raw, err := settings.Get(ctx, "admin_email")
	if err != nil {
		return ""
	}
	var wrapped struct {
		Value string `json:"value"`
	}
	if json.Unmarshal([]byte(raw), &wrapped) == nil && strings.TrimSpace(wrapped.Value) != "" {
		return strings.TrimSpace(wrapped.Value)
	}
	var value string
	if json.Unmarshal([]byte(raw), &value) == nil {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(raw)
}

func importHubCenterMCPMarketplaceEntry(ctx context.Context, svc *capability.Service, centerStatus capabilityMarketCenterStatusProvider, capabilityID string) (*capability.CapabilitySummary, error) {
	item, err := fetchHubCenterMCPMarketplaceEntry(ctx, centerStatus, capabilityID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(item.CapabilityID) == "" || strings.TrimSpace(item.DisplayName) == "" {
		return nil, errors.New("hubcenter MCP capability is missing capability_id/display_name")
	}
	if strings.TrimSpace(item.Version) == "" {
		item.Version = "1.0.0"
	}
	if strings.TrimSpace(item.Publisher) == "" {
		item.Publisher = "hubcenter"
	}
	metadata := map[string]any{
		"server_id":     firstNonEmpty(item.MCP.ID, item.CapabilityID),
		"name":          firstNonEmpty(item.MCP.Name, item.DisplayName),
		"endpoint_url":  strings.TrimSpace(item.MCP.EndpointURL),
		"auth_type":     firstNonEmpty(item.MCP.AuthType, "none"),
		"origin_source": corelib.CapabilitySourceHubCenter,
		"pricing":       corelib.CapabilityPricingFree,
	}
	if len(item.Pricing) > 0 {
		metadata["pricing"] = item.Pricing
	}
	if len(item.MCP.Headers) > 0 {
		metadata["headers"] = item.MCP.Headers
	}
	manifest := map[string]any{
		"name":        item.DisplayName,
		"description": item.Description,
		"type":        corelib.CapabilityTypeMCP,
	}
	origin := map[string]any{
		"source":        corelib.CapabilitySourceHubCenter,
		"capability_id": item.CapabilityID,
		"version_key":   item.VersionKey,
	}
	created, err := svc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
		CapabilityType:    corelib.CapabilityTypeMCP,
		Publisher:         item.Publisher,
		CapabilityID:      item.CapabilityID,
		DisplayName:       item.DisplayName,
		Description:       item.Description,
		Source:            corelib.CapabilitySourceEnterpriseHub,
		ManagedBy:         "admin",
		Status:            "approved",
		RelationToOrigin:  "mirrored",
		OriginKey:         firstNonEmpty(item.VersionKey, corelib.CapabilitySourceHubCenter+":"+corelib.CapabilityTypeMCP+":"+item.Publisher+":"+item.CapabilityID),
		OriginJSON:        jsonObjectString(origin),
		MetadataJSON:      jsonObjectString(metadata),
		Version:           item.Version,
		ManifestJSON:      jsonObjectString(manifest),
		TypeConfigJSON:    jsonObjectString(item.MCP),
		PricingJSON:       rawJSONOrDefault(item.Pricing),
		LicenseJSON:       rawJSONOrDefault(item.License),
		SetCurrentVersion: true,
	})
	if err != nil {
		return nil, err
	}
	for _, spec := range item.SecretRequirements {
		if strings.TrimSpace(spec.Name) == "" {
			continue
		}
		required := true
		if spec.Required != nil {
			required = *spec.Required
		}
		if _, err := svc.UpsertMCPSecretRequirement(ctx, capability.MCPSecretRequirementInput{CapabilityRef: created.ID, VersionKey: created.CurrentVersionKey, Name: spec.Name, Label: spec.Label, Scope: spec.Scope, StoragePolicy: spec.StoragePolicy, Required: required, HelpURL: spec.HelpURL, MetadataJSON: rawJSONOrDefault(spec.Metadata)}); err != nil {
			return nil, err
		}
	}
	return created, nil
}

func importFreeExternalSkillCapability(ctx context.Context, svc *capability.Service, req capabilityInstallIntentRequest) (*capability.CapabilitySummary, error) {
	metadata := mapFromRawJSON(req.Metadata)
	metadata["origin_source"] = req.Source
	metadata["pricing"] = firstNonEmpty(req.Pricing, corelib.CapabilityPricingFree)
	metadata["skill_id"] = firstNonEmpty(stringFromAny(metadata["skill_id"]), stringFromAny(metadata["id"]), stringFromAny(metadata["capability_id"]), req.CapabilityID)
	if req.Source == corelib.CapabilitySourceGitHub {
		metadata["install_ref"] = firstNonEmpty(stringFromAny(metadata["install_ref"]), stringFromAny(metadata["InstallRef"]))
		metadata["repo_url"] = firstNonEmpty(stringFromAny(metadata["repo_url"]), stringFromAny(metadata["RepoURL"]))
		metadata["file_path"] = firstNonEmpty(stringFromAny(metadata["file_path"]), stringFromAny(metadata["FilePath"]))
	}
	capabilityID := firstNonEmpty(req.CapabilityID, stringFromAny(metadata["skill_id"]))
	if capabilityID == "" {
		return nil, errors.New("external skill capability_id is required")
	}
	displayName := firstNonEmpty(req.DisplayName, stringFromAny(metadata["display_name"]), stringFromAny(metadata["name"]), capabilityID)
	description := firstNonEmpty(req.Description, stringFromAny(metadata["description"]), stringFromAny(metadata["summary"]))
	version := firstNonEmpty(req.Version, stringFromAny(metadata["version"]), "1.0.0")
	versionKey := req.Source + ":" + corelib.CapabilityTypeSkill + ":" + capabilityID + "@" + version
	origin := map[string]any{"source": req.Source, "capability_id": capabilityID, "version_key": versionKey}
	manifest := map[string]any{"name": displayName, "description": description, "type": corelib.CapabilityTypeSkill}
	return svc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
		CapabilityType:    corelib.CapabilityTypeSkill,
		Publisher:         firstNonEmpty(stringFromAny(metadata["author"]), req.Source),
		CapabilityID:      capabilityID,
		DisplayName:       displayName,
		Description:       description,
		Source:            corelib.CapabilitySourceEnterpriseHub,
		ManagedBy:         "admin",
		Status:            "approved",
		RelationToOrigin:  "mirrored",
		OriginKey:         req.Source + ":" + corelib.CapabilityTypeSkill + ":" + capabilityID,
		OriginJSON:        jsonObjectString(origin),
		MetadataJSON:      jsonObjectString(metadata),
		Version:           version,
		VersionKey:        versionKey,
		ManifestJSON:      jsonObjectString(manifest),
		PricingJSON:       rawJSONOrDefault(req.Price),
		LicenseJSON:       rawJSONOrDefault(req.License),
		SetCurrentVersion: true,
	})
}

func importFreeHubCenterCapabilityForIntent(ctx context.Context, svc *capability.Service, centerStatus capabilityMarketCenterStatusProvider, req capabilityInstallIntentRequest) (*capability.CapabilitySummary, error) {
	if !strings.EqualFold(req.Pricing, corelib.CapabilityPricingFree) && strings.TrimSpace(req.Pricing) != "" {
		return nil, nil
	}
	if strings.EqualFold(req.CapabilityType, corelib.CapabilityTypeMCP) {
		return importHubCenterMCPMarketplaceEntry(ctx, svc, centerStatus, req.CapabilityID)
	}
	if strings.EqualFold(req.CapabilityType, corelib.CapabilityTypeSkill) {
		return importHubCenterSkillMarketplaceEntry(ctx, svc, centerStatus, req.CapabilityID)
	}
	return nil, nil
}
func AdminCapabilityImportIntentHandler(svc *capability.Service, settings store.SystemSettingsRepository, centerStatuses ...capabilityMarketCenterStatusProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		settings = scopedSystemSettingsForTenant(RequestTenantID(r), settings)
		var req capabilityInstallIntentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		normalizeCapabilityInstallIntentRequest(&req)
		if !corelib.AdminMarketplaceCanSearchSource(corelib.CapabilityMarketplaceHostHub, req.Source) {
			writeError(w, http.StatusForbidden, "SOURCE_NOT_ALLOWED", "source is not allowed for Hub capability market admin import")
			return
		}
		policy := corelib.DefaultCapabilityMarketPolicy()
		if settings != nil {
			if loaded, err := loadCapabilityMarketPolicy(r, settings); err == nil {
				policy = loaded
			}
		}
		adminSearchOnly := false
		policy.EnterpriseOnlySearch = &adminSearchOnly
		var enterpriseItem *capability.CapabilitySummary
		if item, err := findEnterpriseCapabilityForInstallIntent(ctx, svc, req); err == nil {
			enterpriseItem = item
		}
		decision := corelib.DecideCapabilityInstall(corelib.CapabilityInstallDecisionInput{
			Policy:             policy,
			Source:             req.Source,
			Pricing:            req.Pricing,
			ExistsInEnterprise: enterpriseItem != nil && strings.TrimSpace(enterpriseItem.ID) != "",
			ExternalInstallOK:  true,
			ExternalPurchaseOK: true,
		})
		if decision.Action == corelib.CapabilityInstallFromEnterpriseHub {
			writeJSON(w, http.StatusOK, map[string]any{"action": decision.Action, "reason": decision.Reason, "capability": enterpriseItem})
			return
		}
		if decision.Action == corelib.CapabilityInstallBlocked {
			writeJSON(w, http.StatusForbidden, map[string]any{"action": decision.Action, "reason": decision.Reason})
			return
		}
		kind := "import"
		if decision.Action == corelib.CapabilityInstallCreatePurchaseRequest {
			kind = "purchase"
		}
		if existing := findOpenAcquisitionRequest(ctx, svc, req, kind); existing != nil {
			writeJSON(w, http.StatusAccepted, map[string]any{"action": decision.Action, "reason": decision.Reason, "request_id": existing.ID})
			return
		}
		requestID, err := svc.CreateAcquisitionRequest(ctx, capability.AcquisitionRequestInput{
			RequesterUserID:     "admin",
			CapabilityType:      req.CapabilityType,
			Source:              req.Source,
			SourceCapabilityKey: req.CapabilityID,
			SourceVersionKey:    req.Version,
			RequestKind:         kind,
			Reason:              req.UserReason,
			PriceJSON:           rawJSONOrDefault(req.Price),
			LicenseJSON:         rawJSONOrDefault(req.License),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ACQUISITION_REQUEST_FAILED", err.Error())
			return
		}
		if (decision.Action == corelib.CapabilityInstallCreateImportRequest || decision.Action == corelib.CapabilityInstallExternalDirect) && req.Source == corelib.CapabilitySourceHubCenter && strings.EqualFold(req.CapabilityType, corelib.CapabilityTypeMCP) {
			var centerStatus capabilityMarketCenterStatusProvider
			if len(centerStatuses) > 0 {
				centerStatus = centerStatuses[0]
			}
			created, err := importHubCenterMCPMarketplaceEntry(ctx, svc, centerStatus, req.CapabilityID)
			if err != nil {
				markAcquisitionImportFailed(ctx, svc, requestID, err)
				writeError(w, http.StatusBadGateway, "HUBCENTER_IMPORT_FAILED", err.Error())
				return
			}
			if err := svc.CompleteAcquisitionRequest(ctx, requestID, created.ID, jsonObjectString(map[string]any{"mode": "free_import", "source": corelib.CapabilitySourceHubCenter})); err != nil {
				writeError(w, acquisitionStatusCode(err), "ACQUISITION_COMPLETE_FAILED", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"action": decision.Action, "reason": decision.Reason, "request_id": requestID, "capability": created})
			return
		}
		if (decision.Action == corelib.CapabilityInstallCreateImportRequest || decision.Action == corelib.CapabilityInstallExternalDirect) && req.Source == corelib.CapabilitySourceHubCenter && strings.EqualFold(req.CapabilityType, corelib.CapabilityTypeSkill) {
			var centerStatus capabilityMarketCenterStatusProvider
			if len(centerStatuses) > 0 {
				centerStatus = centerStatuses[0]
			}
			created, err := importHubCenterSkillMarketplaceEntry(ctx, svc, centerStatus, req.CapabilityID)
			if err != nil {
				markAcquisitionImportFailed(ctx, svc, requestID, err)
				writeError(w, http.StatusBadGateway, "HUBCENTER_IMPORT_FAILED", err.Error())
				return
			}
			if err := svc.CompleteAcquisitionRequest(ctx, requestID, created.ID, jsonObjectString(map[string]any{"mode": "free_import", "source": corelib.CapabilitySourceHubCenter})); err != nil {
				writeError(w, acquisitionStatusCode(err), "ACQUISITION_COMPLETE_FAILED", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"action": decision.Action, "reason": decision.Reason, "request_id": requestID, "capability": created})
			return
		}
		if (decision.Action == corelib.CapabilityInstallCreateImportRequest || decision.Action == corelib.CapabilityInstallExternalDirect) && strings.EqualFold(req.CapabilityType, corelib.CapabilityTypeSkill) && (req.Source == corelib.CapabilitySourceClawHub || req.Source == corelib.CapabilitySourceGitHub) {
			created, err := importFreeExternalSkillCapability(ctx, svc, req)
			if err != nil {
				markAcquisitionImportFailed(ctx, svc, requestID, err)
				writeError(w, http.StatusBadGateway, "EXTERNAL_SKILL_IMPORT_FAILED", err.Error())
				return
			}
			if err := svc.CompleteAcquisitionRequest(ctx, requestID, created.ID, jsonObjectString(map[string]any{"mode": "free_import", "source": req.Source})); err != nil {
				writeError(w, acquisitionStatusCode(err), "ACQUISITION_COMPLETE_FAILED", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"action": decision.Action, "reason": decision.Reason, "request_id": requestID, "capability": created})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"action": decision.Action, "reason": decision.Reason, "request_id": requestID})
	}
}

func enterpriseSkillPackageRoot(dataDir, tenantID string) string {
	tenantID = firstNonEmpty(tenantID, store.DefaultTenantID)
	return filepath.Join(dataDir, "capability-skill-packages", safeEnterpriseSkillFileName(tenantID, shortEnterpriseSkillDigest(tenantID)))
}

func safeEnterpriseSkillPackageName(skillID, checksum string) string {
	return safeEnterpriseSkillFileName(skillID, shortEnterpriseSkillDigest(skillID), checksum)
}

func moveEnterpriseSkillPackageIntoPlace(tmpPath, finalPath, expectedChecksum string) error {
	if err := os.Rename(tmpPath, finalPath); err == nil {
		return nil
	}
	if _, statErr := os.Stat(finalPath); statErr == nil {
		if checksum, err := fileSHA256Hex(finalPath); err == nil && strings.EqualFold(checksum, strings.TrimSpace(expectedChecksum)) {
			return os.Remove(tmpPath)
		}
		if err := os.Remove(finalPath); err != nil {
			return err
		}
		return os.Rename(tmpPath, finalPath)
	}
	return os.Rename(tmpPath, finalPath)
}

func fileSHA256Hex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func safeEnterpriseSkillFileName(parts ...string) string {
	text := strings.ToLower(strings.Join(parts, "-"))
	var b strings.Builder
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else if r == ' ' || r == '/' || r == '\\' || r == ':' {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-._")
	if out == "" {
		return "skill"
	}
	return out
}

func shortEnterpriseSkillDigest(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])[:12]
}

func readEnterpriseSkillPackageMeta(zipPath string) (*enterpriseSkillPackageMeta, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	stagingDir, err := os.MkdirTemp("", "enterprise-skill-preflight-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stagingDir)
	meta := &enterpriseSkillPackageMeta{Files: map[string]string{}, Version: "1.0.0"}
	exportedBytes := 0
	foundDefinition := false
	for _, f := range zr.File {
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("skill package contains symlink %s", f.Name)
		}
		if !enterpriseSkillZipPathAllowed(f.Name) {
			return nil, fmt.Errorf("skill package contains unsafe path %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			continue
		}
		name := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(f.Name)), "./")
		base := strings.ToLower(filepath.Base(name))
		isManifest := base == "skill_package_manifest.json"
		isDefinition := base == "skill.yaml" || base == "skill.yml"
		isExportable := enterpriseSkillFileExportable(name, int(f.UncompressedSize64))
		maxBytes := maxSingleEnterpriseSkillFileBytes(name)
		if f.UncompressedSize64 > uint64(maxBytes) {
			return nil, fmt.Errorf("skill package file %s exceeds %d bytes", name, maxBytes)
		}
		data, err := readEnterpriseSkillZipFile(f, maxBytes)
		if err != nil {
			return nil, fmt.Errorf("read skill package file %s: %w", name, err)
		}
		if err := writeEnterpriseSkillStagingFile(stagingDir, name, data); err != nil {
			return nil, err
		}
		if !isManifest && !isDefinition && !isExportable {
			continue
		}
		switch base {
		case "skill_package_manifest.json":
			meta.Manifest = string(data)
		case "skill.yaml", "skill.yml":
			sf, err := coreskill.ParseSkillDefinitionFile(data, "yaml")
			if err != nil {
				return nil, fmt.Errorf("invalid %s: %w", name, err)
			}
			if sf == nil {
				return nil, fmt.Errorf("invalid %s", name)
			}
			if strings.TrimSpace(sf.Name) == "" {
				return nil, fmt.Errorf("%s must declare name", name)
			}
			foundDefinition = true
			meta.Name = strings.TrimSpace(sf.Name)
			meta.SkillID = strings.TrimSpace(sf.Name)
			meta.Description = firstNonEmpty(meta.Description, sf.Description)
			meta.Triggers = append([]string(nil), sf.Triggers...)
			for _, step := range sf.Steps {
				onError := step.OnError
				if onError == "" && step.ContinueOnErr {
					onError = "continue"
				}
				meta.Steps = append(meta.Steps, corelib.NLSkillStep{Action: step.Action, Params: step.Params, OnError: onError, Name: step.Name, Condition: step.Condition})
			}
		}
		if !isManifest && enterpriseSkillFileExportable(name, len(data)) {
			exportedBytes += len(data)
			if exportedBytes > enterpriseSkillPackageExportMaxBytes {
				return nil, fmt.Errorf("skill package exportable files exceed %d bytes", enterpriseSkillPackageExportMaxBytes)
			}
			meta.Files[name] = base64.StdEncoding.EncodeToString(data)
		}
	}
	if !foundDefinition {
		return nil, errors.New("skill package must contain a valid skill.yaml or skill.yml")
	}
	if strings.TrimSpace(meta.Name) == "" {
		meta.Name = meta.SkillID
	}
	if len(meta.Files) == 0 {
		return nil, errors.New("skill package has no exportable skill files")
	}
	if err := validateEnterpriseSkillPackagePreflight(stagingDir); err != nil {
		return nil, err
	}
	return meta, nil
}

func writeEnterpriseSkillStagingFile(root, name string, data []byte) error {
	target := filepath.Join(root, filepath.FromSlash(name))
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
		return fmt.Errorf("skill package contains unsafe path %s", name)
	}
	if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(targetAbs, data, 0o644)
}

func validateEnterpriseSkillPackagePreflight(skillDir string) error {
	report, err := coreskill.ValidateSkillPortability(skillDir)
	if err != nil {
		return fmt.Errorf("skill package preflight failed: %w", err)
	}
	entry, err := loadEnterpriseSkillPackageEntryForPreflight(skillDir)
	if err != nil {
		return fmt.Errorf("skill package preflight failed: %w", err)
	}
	preflight := &coreskill.UploadPreflightResult{SkillDir: skillDir, Report: report}
	if report != nil {
		preflight.SkillName = report.SkillName
		for _, issue := range report.Issues {
			switch issue.Category {
			case "hardcoded_path", "missing_basedir":
				preflight.BlockingPaths = append(preflight.BlockingPaths, coreskill.PortabilityPathIssue{
					Path:       issue.Message,
					File:       issue.File,
					Suggestion: issue.Suggestion,
				})
			case "missing_platforms", "incomplete_metadata":
				preflight.Warnings = append(preflight.Warnings, fmt.Sprintf("[%s] %s", issue.Category, issue.Message))
			default:
				if issue.Severity == coreskill.SeverityWarning || issue.Severity == coreskill.SeverityInfo {
					preflight.Warnings = append(preflight.Warnings, fmt.Sprintf("[%s] %s", issue.Category, issue.Message))
				}
			}
		}
	}
	if entry != nil {
		preflight.MissingFiles = coreskill.CollectMissingPackageFileReferences(entry)
	}
	if !preflight.Portable() {
		return fmt.Errorf("%s", coreskill.FormatUploadPreflight(preflight))
	}
	return nil
}

func loadEnterpriseSkillPackageEntryForPreflight(skillDir string) (*corelib.NLSkillEntry, error) {
	for _, name := range []string{"skill.yaml", "skill.yml"} {
		path := filepath.Join(skillDir, name)
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		sf, err := coreskill.ParseSkillDefinitionFile(data, strings.TrimPrefix(filepath.Ext(name), "."))
		if err != nil {
			return nil, err
		}
		entry := &corelib.NLSkillEntry{
			Name:                    strings.TrimSpace(sf.Name),
			Description:             sf.Description,
			Triggers:                append([]string(nil), sf.Triggers...),
			SkillDir:                skillDir,
			Steps:                   enterpriseSkillYAMLStepsForPreflight(sf.Steps),
			Params:                  enterpriseSkillYAMLParamsForPreflight(sf.Params),
			RequiredCredentialFiles: append([]string(nil), sf.RequiredCredentialFiles...),
			Pipeline:                enterpriseSkillYAMLPipelineForPreflight(sf.Pipeline),
		}
		return entry, nil
	}
	return nil, errors.New("skill package must contain skill.yaml or skill.yml")
}

func enterpriseSkillYAMLStepsForPreflight(steps []coreskill.SkillYAMLStep) []corelib.NLSkillStep {
	out := make([]corelib.NLSkillStep, 0, len(steps))
	for _, step := range steps {
		params := step.Params
		if params == nil {
			params = map[string]interface{}{}
		}
		out = append(out, corelib.NLSkillStep{
			Action:    step.Action,
			Params:    params,
			OnError:   firstNonEmpty(step.OnError, "stop"),
			Name:      step.Name,
			Condition: step.Condition,
			When:      step.When,
			Label:     step.Label,
			Capture:   step.Capture,
		})
	}
	return out
}

func enterpriseSkillYAMLParamsForPreflight(params []coreskill.SkillYAMLParam) []corelib.NLSkillParam {
	out := make([]corelib.NLSkillParam, 0, len(params))
	for _, param := range params {
		out = append(out, corelib.NLSkillParam{
			Name:        param.Name,
			Description: param.Description,
			Aliases:     append([]string(nil), param.Aliases...),
			CLIFlag:     param.CLIFlag,
			Default:     param.Default,
			Required:    param.Required,
		})
	}
	return out
}

func enterpriseSkillYAMLPipelineForPreflight(steps []coreskill.SkillYAMLPipelineStep) []corelib.SkillPipelineStep {
	out := make([]corelib.SkillPipelineStep, 0, len(steps))
	for _, step := range steps {
		out = append(out, corelib.SkillPipelineStep{
			Skill:              step.Skill,
			Params:             step.Params,
			Checkpoint:         step.Checkpoint,
			CheckpointMessage:  step.CheckpointMessage,
			ContinueOnFail:     step.ContinueOnFail,
			TimeImpactOnReject: step.TimeImpactOnReject,
		})
	}
	return out
}

func enterpriseSkillZipPathAllowed(name string) bool {
	name = filepath.ToSlash(strings.TrimSpace(name))
	clean := filepath.Clean(name)
	clean = filepath.ToSlash(clean)
	return name != "" && clean != "." && !strings.HasPrefix(clean, "../") && !strings.HasPrefix(clean, "/") && !strings.Contains(clean, ":")
}

func readEnterpriseSkillZipFile(f *zip.File, max int64) ([]byte, error) {
	if max <= 0 {
		max = 256 << 10
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("file too large")
	}
	return data, nil
}

func maxSingleEnterpriseSkillFileBytes(name string) int64 {
	if strings.EqualFold(filepath.Base(name), "skill_package_manifest.json") {
		return 1 << 20
	}
	return enterpriseSkillUploadMaxBytes
}

func enterpriseSkillFileExportable(name string, size int) bool {
	if size <= 0 || size > enterpriseSkillUploadMaxBytes {
		return false
	}
	if enterpriseSkillPathHasRuntimeArtifact(name) {
		return false
	}
	return true
}

func enterpriseSkillPathHasRuntimeArtifact(name string) bool {
	parts := strings.Split(filepath.ToSlash(strings.TrimSpace(name)), "/")
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i == len(parts)-1 {
			return coreskill.IsSkillRuntimePackageFile(part)
		}
		if coreskill.IsSkillRuntimePackageDir(part) {
			return true
		}
	}
	return false
}

func enterpriseHubPublicBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded != "" {
		scheme = forwarded
	}
	host := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0])
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func AdminCapabilityMarketPolicyGetHandler(settings store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		settings = scopedSystemSettingsForTenant(RequestTenantID(r), settings)
		policy, err := loadCapabilityMarketPolicy(r, settings)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_MARKET_POLICY_GET_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"policy": policy})
	}
}

func AdminCapabilityMarketPolicyUpdateHandler(settings store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		settings = scopedSystemSettingsForTenant(RequestTenantID(r), settings)
		if settings == nil {
			writeError(w, http.StatusInternalServerError, "SETTINGS_UNAVAILABLE", "system settings repository is unavailable")
			return
		}
		var req struct {
			Policy *corelib.CapabilityMarketPolicy `json:"policy"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if req.Policy == nil {
			writeError(w, http.StatusBadRequest, "INVALID_POLICY", "policy is required")
			return
		}
		policy := req.Policy.WithDefaults()
		data, err := json.Marshal(policy)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_MARKET_POLICY_ENCODE_FAILED", err.Error())
			return
		}
		if err := settings.Set(r.Context(), capabilityMarketPolicySettingKey, string(data)); err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_MARKET_POLICY_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"policy": policy})
	}
}

func MCPSecretRequirementsHandler(svc *capability.Service, identityOpt ...viewerAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := marketplaceRequestContext(r, identityOpt...)
		items, err := svc.ListMCPSecretRequirements(ctx, r.PathValue("id"), r.URL.Query().Get("version_key"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_SECRET_REQUIREMENTS_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func MCPHubSecretsHandler(identity viewerAuthenticator, svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateMarketplaceViewer(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		ctx := capability.WithTenant(r.Context(), principal.TenantID)
		items, err := svc.ListMCPHubSecrets(ctx, principal.UserID, r.URL.Query().Get("mcp_server_id"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_HUB_SECRETS_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func MCPHubSecretUpsertHandler(identity viewerAuthenticator, svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateMarketplaceViewer(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		ctx := capability.WithTenant(r.Context(), principal.TenantID)
		var req struct {
			MCPServerID     string          `json:"mcp_server_id"`
			RequirementName string          `json:"requirement_name"`
			SecretValue     string          `json:"secret_value"`
			Metadata        json.RawMessage `json:"metadata,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		item, err := svc.UpsertMCPHubSecret(ctx, capability.MCPHubSecretInput{UserID: principal.UserID, MCPServerID: req.MCPServerID, RequirementName: req.RequirementName, SecretValue: req.SecretValue, MetadataJSON: rawJSONOrDefault(req.Metadata)})
		if err != nil {
			writeError(w, http.StatusBadRequest, "MCP_HUB_SECRET_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}

func MCPSecretBindingsHandler(identity viewerAuthenticator, svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateMarketplaceViewer(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		ctx := capability.WithTenant(r.Context(), principal.TenantID)
		items, err := svc.ListMCPSecretBindings(ctx, principal.UserID, r.URL.Query().Get("mcp_server_id"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_SECRET_BINDINGS_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func MCPSecretBindingUpsertHandler(identity viewerAuthenticator, svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateMarketplaceViewer(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		ctx := capability.WithTenant(r.Context(), principal.TenantID)
		var req struct {
			MCPServerID     string `json:"mcp_server_id"`
			RequirementName string `json:"requirement_name"`
			Storage         string `json:"storage"`
			HubSecretRef    string `json:"hub_secret_ref,omitempty"`
			LocalSecretRef  string `json:"local_secret_ref,omitempty"`
			Status          string `json:"status,omitempty"`
			LastVerifiedAt  string `json:"last_verified_at,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		item, err := svc.UpsertMCPSecretBinding(ctx, capability.MCPSecretBindingInput{UserID: principal.UserID, MCPServerID: req.MCPServerID, RequirementName: req.RequirementName, Storage: req.Storage, HubSecretRef: req.HubSecretRef, LocalSecretRef: req.LocalSecretRef, Status: req.Status, LastVerifiedAt: req.LastVerifiedAt})
		if err != nil {
			writeError(w, http.StatusBadRequest, "MCP_SECRET_BINDING_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}

func AdminMCPSecretRequirementUpsertHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := capabilityAdminContext(r)
		var req struct {
			CapabilityRef string          `json:"capability_ref"`
			VersionKey    string          `json:"version_key"`
			Name          string          `json:"name"`
			Label         string          `json:"label,omitempty"`
			Scope         string          `json:"scope,omitempty"`
			StoragePolicy string          `json:"storage_policy,omitempty"`
			Required      *bool           `json:"required,omitempty"`
			HelpURL       string          `json:"help_url,omitempty"`
			Metadata      json.RawMessage `json:"metadata,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if strings.TrimSpace(req.CapabilityRef) == "" || strings.TrimSpace(req.Name) == "" {
			writeError(w, http.StatusBadRequest, "INVALID_SECRET_REQUIREMENT", "capability_ref and name are required")
			return
		}
		required := true
		if req.Required != nil {
			required = *req.Required
		}
		id, err := svc.UpsertMCPSecretRequirement(ctx, capability.MCPSecretRequirementInput{CapabilityRef: req.CapabilityRef, VersionKey: req.VersionKey, Name: req.Name, Label: req.Label, Scope: req.Scope, StoragePolicy: req.StoragePolicy, Required: required, HelpURL: req.HelpURL, MetadataJSON: rawJSONOrDefault(req.Metadata)})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_SECRET_REQUIREMENT_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id})
	}
}

type marketplaceViewerPrincipal struct {
	TenantID string
	UserID   string
	Email    string
}

type viewerAuthenticator interface {
	AuthenticateViewer(ctx context.Context, rawToken string) (*auth.ViewerPrincipal, error)
}

func authenticateMarketplaceViewer(r *http.Request, identity viewerAuthenticator) (*marketplaceViewerPrincipal, error) {
	if identity == nil {
		return nil, auth.ErrInvalidUserCredentials
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return nil, auth.ErrInvalidUserCredentials
	}
	principal, err := identity.AuthenticateViewer(r.Context(), strings.TrimSpace(authz[7:]))
	if err != nil {
		return nil, err
	}
	return &marketplaceViewerPrincipal{TenantID: principal.TenantID, UserID: principal.UserID, Email: principal.Email}, nil
}

func capabilityTenantIDFromRequest(r *http.Request, identityOpt ...viewerAuthenticator) string {
	if r == nil {
		return store.DefaultTenantID
	}
	if len(identityOpt) > 0 && identityOpt[0] != nil {
		if principal, err := authenticateMarketplaceViewer(r, identityOpt[0]); err == nil {
			if tenantID := strings.TrimSpace(principal.TenantID); tenantID != "" {
				return tenantID
			}
		}
	}
	if tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID")); tenantID != "" {
		return tenantID
	}
	return RequestTenantID(r)
}

func marketplaceRequestContext(r *http.Request, identityOpt ...viewerAuthenticator) context.Context {
	return capability.WithTenant(r.Context(), capabilityTenantIDFromRequest(r, identityOpt...))
}

func capabilityAdminContext(r *http.Request) context.Context {
	return capability.WithTenant(r.Context(), capabilityTenantIDFromRequest(r))
}

func loadCapabilityMarketPolicy(r *http.Request, settings store.SystemSettingsRepository) (corelib.CapabilityMarketPolicy, error) {
	policy := corelib.DefaultCapabilityMarketPolicy()
	if settings == nil {
		return policy, nil
	}
	raw, err := settings.Get(r.Context(), capabilityMarketPolicySettingKey)
	if err != nil {
		return policy, err
	}
	if strings.TrimSpace(raw) == "" {
		return policy, nil
	}
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		return policy, err
	}
	return policy.WithDefaults(), nil
}

func mapFromRawJSON(raw json.RawMessage) map[string]any {
	out := map[string]any{}
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || string(raw) == "null" {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]any{}
	}
	return out
}

func stringFromAny(value any) string {
	s, _ := value.(string)
	return strings.TrimSpace(s)
}

func anyMapFromMap(values map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if value, ok := values[key].(map[string]any); ok {
			return value
		}
	}
	return nil
}

func anySliceFromMap(values map[string]any, key string) []any {
	if values == nil {
		return nil
	}
	out, _ := values[key].([]any)
	return out
}

func firstEnterpriseMaclawAppPresent(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if values == nil {
			return nil
		}
		if value, ok := values[key]; ok && value != nil {
			return value
		}
	}
	return nil
}

func enterpriseMaclawAppFirstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringFromAny(firstEnterpriseMaclawAppPresent(values, key)); value != "" {
			return value
		}
	}
	return ""
}

func enterpriseMaclawAppFirstBool(values map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		value := firstEnterpriseMaclawAppPresent(values, key)
		switch typed := value.(type) {
		case bool:
			return typed, true
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "true", "1", "yes", "y":
				return true, true
			case "false", "0", "no", "n":
				return false, true
			}
		case float64:
			return typed != 0, true
		case int:
			return typed != 0, true
		}
	}
	return false, false
}

func enterpriseMaclawAppFirstNumber(values map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		value := firstEnterpriseMaclawAppPresent(values, key)
		switch typed := value.(type) {
		case float64:
			return typed, true
		case int:
			return typed, true
		case int64:
			return typed, true
		case json.Number:
			if parsed, err := typed.Int64(); err == nil {
				return parsed, true
			}
			if parsed, err := typed.Float64(); err == nil {
				return parsed, true
			}
		}
	}
	return nil, false
}

func enterpriseMaclawAppStringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return compactStringList(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := stringFromAny(item); value != "" {
				out = append(out, value)
			}
		}
		return compactStringList(out)
	default:
		return nil
	}
}

func canonicalJSONSHA256(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode canonical json: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func compactMetadata(values map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range values {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				out[key] = strings.TrimSpace(typed)
			}
		case nil:
		case []map[string]any:
			if len(typed) > 0 {
				out[key] = typed
			}
		case []any:
			if len(typed) > 0 {
				out[key] = typed
			}
		case map[string]any:
			if len(typed) > 0 {
				out[key] = typed
			}
		default:
			out[key] = value
		}
	}
	return out
}

func rawJSONOrDefault(raw json.RawMessage) string {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || string(raw) == "null" {
		return "{}"
	}
	return string(raw)
}

func jsonObjectString(v any) string {
	data, err := json.Marshal(v)
	if err != nil || len(data) == 0 || string(data) == "null" {
		return "{}"
	}
	return string(data)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// AdminMCPTestConnectionHandler probes an arbitrary MCP endpoint to verify
// connectivity and list available tools. Used by the capability market MCP
// JSON editor's "Test Connection" button.
func AdminMCPTestConnectionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			EndpointURL string            `json:"endpoint_url"`
			AuthType    string            `json:"auth_type"`
			AuthSecret  string            `json:"auth_secret"`
			Headers     map[string]string `json:"headers"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "invalid request body"})
			return
		}
		if strings.TrimSpace(req.EndpointURL) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "endpoint_url is required"})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		endpoint := strings.TrimRight(strings.TrimSpace(req.EndpointURL), "/")

		// Step 1: Try MCP initialize handshake (non-fatal if unsupported).
		initBody, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "initialize",
			"params": map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{},
				"clientInfo":      map[string]any{"name": "maclaw-hub", "version": "1.0.0"},
			},
		})
		var sessionID string
		if initReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(initBody))); err == nil {
			initReq.Header.Set("Content-Type", "application/json")
			initReq.Header.Set("Accept", "application/json, text/event-stream")
			applyMCPTestAuth(initReq, req.AuthType, req.AuthSecret, req.Headers)
			if resp, err := http.DefaultClient.Do(initReq); err == nil {
				if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
					sessionID = sid
				}
				resp.Body.Close()
			}
		}

		// Step 2: Call tools/list.
		toolsBody, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{},
		})
		toolsReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(toolsBody)))
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": "failed to create request"})
			return
		}
		toolsReq.Header.Set("Content-Type", "application/json")
		toolsReq.Header.Set("Accept", "application/json, text/event-stream")
		applyMCPTestAuth(toolsReq, req.AuthType, req.AuthSecret, req.Headers)
		if sessionID != "" {
			toolsReq.Header.Set("Mcp-Session-Id", sessionID)
		}

		start := time.Now()
		resp, err := http.DefaultClient.Do(toolsReq)
		latency := time.Since(start).Milliseconds()

		if err != nil {
			msg := err.Error()
			if ctx.Err() == context.DeadlineExceeded {
				msg = "connection timed out (15s)"
			}
			writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": msg, "latency_ms": latency})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": "HTTP " + resp.Status, "latency_ms": latency})
			return
		}

		var rpcResp struct {
			Result struct {
				Tools []struct {
					Name        string `json:"name"`
					Description string `json:"description"`
				} `json:"tools"`
			} `json:"result"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "connected but could not parse tool list", "latency_ms": latency})
			return
		}

		tools := make([]map[string]string, 0, len(rpcResp.Result.Tools))
		for _, t := range rpcResp.Result.Tools {
			tools = append(tools, map[string]string{"name": t.Name, "description": t.Description})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success":    true,
			"message":    "connected",
			"tools":      tools,
			"latency_ms": latency,
		})
	}
}

func applyMCPTestAuth(req *http.Request, authType, authSecret string, headers map[string]string) {
	for k, v := range headers {
		if k != "" && v != "" && strings.ToLower(k) != "content-type" && strings.ToLower(k) != "accept" {
			req.Header.Set(k, v)
		}
	}
	if authSecret == "" {
		return
	}
	switch authType {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+authSecret)
	case "api_key":
		req.Header.Set("X-API-Key", authSecret)
	}
}

// AdminMCPValidateHandler validates an MCP Server's connectivity, tool availability,
// schema correctness, and runtime health. Returns a combined ValidationReport.
func AdminMCPValidateHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			EndpointURL string            `json:"endpoint_url"`
			Transport   string            `json:"transport"`
			Headers     map[string]string `json:"headers,omitempty"`
			APIKey      string            `json:"api_key,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if strings.TrimSpace(req.EndpointURL) == "" {
			writeError(w, http.StatusBadRequest, "MISSING_ENDPOINT", "endpoint_url is required")
			return
		}

		config := mcp.MCPServerConfig{
			EndpointURL: strings.TrimSpace(req.EndpointURL),
			Transport:   req.Transport,
			Headers:     req.Headers,
			APIKey:      req.APIKey,
		}

		validator := mcp.NewValidator()
		report, err := validator.Validate(r.Context(), config)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "VALIDATION_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, report)
	}
}
