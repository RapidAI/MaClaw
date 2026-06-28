package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

const (
	knowledgeSyncNormalLimitBytes    int64 = 100 << 20
	knowledgeSyncOfficialLimitBytes  int64 = 500 << 20
	knowledgeSyncMaxUploadBytes            = knowledgeSyncOfficialLimitBytes + (1 << 20)
	knowledgeSyncNormalRetentionDays       = 7
)

type KnowledgeSyncView struct {
	OwnerUserID         string         `json:"owner_user_id,omitempty"`
	OwnerUserEmail      string         `json:"owner_user_email,omitempty"`
	TenantID            string         `json:"tenant_id,omitempty"`
	PackageID           string         `json:"package_id,omitempty"`
	PackageVersion      int            `json:"package_version,omitempty"`
	CompressedSizeBytes int64          `json:"compressed_size_bytes,omitempty"`
	StoredSizeBytes     int64          `json:"stored_size_bytes,omitempty"`
	CreatedAt           string         `json:"created_at,omitempty"`
	UpdatedAt           string         `json:"updated_at,omitempty"`
	ExpiresAt           string         `json:"expires_at,omitempty"`
	ServiceStatus       string         `json:"service_status"`
	ReadonlyReason      string         `json:"readonly_reason,omitempty"`
	LimitBytes          int64          `json:"limit_bytes"`
	RetentionDays       int            `json:"retention_days,omitempty"`
	Encryption          map[string]any `json:"encryption,omitempty"`
	HasPackage          bool           `json:"has_package"`
	Message             string         `json:"message,omitempty"`
}

type knowledgeSyncMeta struct {
	OwnerUserID         string         `json:"owner_user_id"`
	OwnerUserEmail      string         `json:"owner_user_email,omitempty"`
	TenantID            string         `json:"tenant_id"`
	PackageID           string         `json:"package_id"`
	PackageVersion      int            `json:"package_version"`
	CompressedSizeBytes int64          `json:"compressed_size_bytes"`
	StoredSizeBytes     int64          `json:"stored_size_bytes"`
	CreatedAt           string         `json:"created_at"`
	UpdatedAt           string         `json:"updated_at"`
	ExpiresAt           string         `json:"expires_at,omitempty"`
	OfficialExpiredAt   string         `json:"official_expired_at,omitempty"`
	ServiceStatus       string         `json:"service_status"`
	ReadonlyReason      string         `json:"readonly_reason,omitempty"`
	Encryption          map[string]any `json:"encryption,omitempty"`
}

type knowledgeSyncUploadRequest struct {
	PackageID           string         `json:"package_id"`
	PackageVersion      int            `json:"package_version"`
	CompressedSizeBytes int64          `json:"compressed_size_bytes"`
	Encryption          map[string]any `json:"encryption"`
	PayloadBase64       string         `json:"payload_base64"`
}

func KnowledgeSyncStatusHandler(identity *auth.IdentityService, baseDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := requireKnowledgeShareViewer(w, r, identity)
		if !ok {
			return
		}
		status := knowledgeSyncServiceStatus(r, principal)
		meta, _ := loadKnowledgeSyncMeta(baseDir, principal)
		if meta != nil && status.ServiceStatus == "official_expired" && strings.TrimSpace(meta.OfficialExpiredAt) == "" {
			meta.OfficialExpiredAt = time.Now().UTC().Format(time.RFC3339)
			meta.ServiceStatus = "official_expired"
			meta.ReadonlyReason = status.ReadonlyReason
			_ = saveKnowledgeSyncPackageMeta(baseDir, principal, meta)
		}
		view := knowledgeSyncViewFromMeta(meta, principal, status)
		writeJSON(w, http.StatusOK, view)
	}
}

func UploadKnowledgeSyncPackageHandler(identity *auth.IdentityService, baseDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := requireKnowledgeShareViewer(w, r, identity)
		if !ok {
			return
		}
		status := knowledgeSyncServiceStatus(r, principal)
		if status.ServiceStatus == "official_expired" {
			writeError(w, http.StatusPaymentRequired, "KNOWLEDGE_SYNC_READONLY", "maclaw 官方服务已过期：你仍可下载已有同步数据，但无法上传新版本。续费后可继续更新。")
			return
		}
		var req knowledgeSyncUploadRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, knowledgeSyncMaxUploadBytes*2)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		if strings.TrimSpace(req.PayloadBase64) == "" {
			writeError(w, http.StatusBadRequest, "PAYLOAD_REQUIRED", "encrypted sync payload is required")
			return
		}
		payload, err := base64.StdEncoding.DecodeString(req.PayloadBase64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "payload_base64 is invalid")
			return
		}
		storedSize := int64(len(payload))
		if storedSize > status.LimitBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "KNOWLEDGE_SYNC_QUOTA_EXCEEDED", fmt.Sprintf("上传失败：同步包超过当前空间上限 %dMB。", status.LimitBytes>>20))
			return
		}
		now := time.Now().UTC()
		expiresAt := ""
		if status.ServiceStatus == "normal" {
			expiresAt = now.AddDate(0, 0, knowledgeSyncNormalRetentionDays).Format(time.RFC3339)
		}
		meta := &knowledgeSyncMeta{
			OwnerUserID:         principal.UserID,
			OwnerUserEmail:      principal.Email,
			TenantID:            principal.TenantID,
			PackageID:           firstNonEmptyKnowledgeShare(req.PackageID, "ksync_"+now.Format("20060102150405")),
			PackageVersion:      req.PackageVersion,
			CompressedSizeBytes: req.CompressedSizeBytes,
			StoredSizeBytes:     storedSize,
			CreatedAt:           now.Format(time.RFC3339),
			UpdatedAt:           now.Format(time.RFC3339),
			ExpiresAt:           expiresAt,
			ServiceStatus:       status.ServiceStatus,
			Encryption:          req.Encryption,
		}
		if meta.PackageVersion <= 0 {
			meta.PackageVersion = 1
		}
		if previous, _ := loadKnowledgeSyncMeta(baseDir, principal); previous != nil && strings.TrimSpace(previous.CreatedAt) != "" {
			meta.CreatedAt = previous.CreatedAt
		}
		if err := saveKnowledgeSyncPackage(baseDir, principal, payload, meta); err != nil {
			writeError(w, http.StatusInternalServerError, "KNOWLEDGE_SYNC_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, knowledgeSyncViewFromMeta(meta, principal, status))
	}
}

func DownloadKnowledgeSyncPackageHandler(identity *auth.IdentityService, baseDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := requireKnowledgeShareViewer(w, r, identity)
		if !ok {
			return
		}
		status := knowledgeSyncServiceStatus(r, principal)
		meta, path := loadKnowledgeSyncMeta(baseDir, principal)
		if meta == nil || strings.TrimSpace(path) == "" {
			writeError(w, http.StatusNotFound, "KNOWLEDGE_SYNC_NOT_FOUND", "knowledge sync package not found")
			return
		}
		if status.ServiceStatus == "official_expired" && strings.TrimSpace(meta.OfficialExpiredAt) == "" {
			meta.OfficialExpiredAt = time.Now().UTC().Format(time.RFC3339)
			meta.ServiceStatus = "official_expired"
			meta.ReadonlyReason = status.ReadonlyReason
			_ = saveKnowledgeSyncPackageMeta(baseDir, principal, meta)
		}
		if knowledgeSyncMetaExpired(meta, time.Now().UTC()) {
			_ = deleteKnowledgeSyncPackage(baseDir, principal)
			writeError(w, http.StatusNotFound, "KNOWLEDGE_SYNC_EXPIRED", "knowledge sync package has expired")
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=\"knowledge-sync.mksync\"")
		http.ServeFile(w, r, path)
	}
}

func DeleteKnowledgeSyncPackageHandler(identity *auth.IdentityService, baseDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := requireKnowledgeShareViewer(w, r, identity)
		if !ok {
			return
		}
		if err := deleteKnowledgeSyncPackage(baseDir, principal); err != nil {
			writeError(w, http.StatusInternalServerError, "KNOWLEDGE_SYNC_DELETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "deleted"})
	}
}

func StartKnowledgeSyncCleanup(baseDir string) {
	if strings.TrimSpace(baseDir) == "" {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			_ = cleanupKnowledgeSyncPackages(baseDir, time.Now().UTC())
			<-ticker.C
		}
	}()
}

func knowledgeSyncServiceStatus(r *http.Request, principal *auth.ViewerPrincipal) KnowledgeSyncView {
	view := KnowledgeSyncView{
		ServiceStatus: "normal",
		LimitBytes:    knowledgeSyncNormalLimitBytes,
		RetentionDays: knowledgeSyncNormalRetentionDays,
		Message:       "当前为临时同步：同步数据将在 7 天后自动删除，服务器空间上限 100MB。升级 maclaw 官方服务后，可获得 500MB 同步空间，并在服务有效期内不设固定有效期。",
	}
	if principal == nil {
		return view
	}
	if ac := GetMaClawAccessControl(); ac != nil {
		if status := ac.GetAuthorizationStatus(r.Context(), principal.TenantID); status != nil {
			active := false
			hadOfficial := false
			for _, grant := range status.Authorizations {
				if strings.EqualFold(strings.TrimSpace(grant.ServiceGroupID), llmservice.MaClawOfficialServiceGroupID) || strings.Contains(strings.ToLower(grant.Source), "maclaw") {
					hadOfficial = true
					if grant.Active && strings.EqualFold(strings.TrimSpace(grant.Status), "active") {
						active = true
						break
					}
				}
			}
			if active {
				view.ServiceStatus = "official_active"
				view.LimitBytes = knowledgeSyncOfficialLimitBytes
				view.RetentionDays = 0
				view.Message = "maclaw 官方服务有效中：同步数据不设固定有效期，空间上限 500MB。"
			} else if hadOfficial || len(status.Authorizations) > 0 {
				view.ServiceStatus = "official_expired"
				view.LimitBytes = knowledgeSyncOfficialLimitBytes
				view.RetentionDays = knowledgeSyncNormalRetentionDays
				view.ReadonlyReason = "maclaw 官方服务已过期：你仍可下载已有同步数据，但无法上传新版本。若连续 7 天未恢复服务，同步数据将自动删除。续费后可继续更新并保留同步能力。"
				view.Message = view.ReadonlyReason
			}
		}
	}
	return view
}

func knowledgeSyncViewFromMeta(meta *knowledgeSyncMeta, principal *auth.ViewerPrincipal, status KnowledgeSyncView) KnowledgeSyncView {
	view := status
	if principal != nil {
		view.OwnerUserID = principal.UserID
		view.OwnerUserEmail = principal.Email
		view.TenantID = principal.TenantID
	}
	if meta == nil {
		return view
	}
	view.HasPackage = true
	view.PackageID = meta.PackageID
	view.PackageVersion = meta.PackageVersion
	view.CompressedSizeBytes = meta.CompressedSizeBytes
	view.StoredSizeBytes = meta.StoredSizeBytes
	view.CreatedAt = meta.CreatedAt
	view.UpdatedAt = meta.UpdatedAt
	view.ExpiresAt = meta.ExpiresAt
	view.Encryption = meta.Encryption
	return view
}

func knowledgeSyncUserDir(baseDir string, principal *auth.ViewerPrincipal) string {
	tenant := sanitizeKnowledgeSyncPathPart(principal.TenantID)
	user := sanitizeKnowledgeSyncPathPart(firstNonEmptyKnowledgeShare(principal.Email, principal.UserID))
	return filepath.Join(baseDir, tenant, user)
}

func legacyKnowledgeSyncUserDir(baseDir string, principal *auth.ViewerPrincipal) string {
	tenant := sanitizeKnowledgeSyncPathPart(principal.TenantID)
	user := sanitizeKnowledgeSyncPathPart(principal.UserID)
	return filepath.Join(baseDir, tenant, user)
}

func sanitizeKnowledgeSyncPathPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_", "..", "_").Replace(value)
}

func loadKnowledgeSyncMeta(baseDir string, principal *auth.ViewerPrincipal) (*knowledgeSyncMeta, string) {
	if principal == nil || strings.TrimSpace(baseDir) == "" {
		return nil, ""
	}
	dir := knowledgeSyncUserDir(baseDir, principal)
	if legacy := legacyKnowledgeSyncUserDir(baseDir, principal); legacy != dir {
		if _, err := os.Stat(filepath.Join(dir, "meta.json")); err != nil {
			if _, legacyErr := os.Stat(filepath.Join(legacy, "meta.json")); legacyErr == nil {
				_ = os.MkdirAll(filepath.Dir(dir), 0o755)
				_ = os.Rename(legacy, dir)
			}
		}
	}
	metaPath := filepath.Join(dir, "meta.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, ""
	}
	var meta knowledgeSyncMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, ""
	}
	packagePath := filepath.Join(dir, "package.mksync")
	if _, err := os.Stat(packagePath); err != nil {
		return &meta, ""
	}
	return &meta, packagePath
}

func saveKnowledgeSyncPackage(baseDir string, principal *auth.ViewerPrincipal, payload []byte, meta *knowledgeSyncMeta) error {
	dir := knowledgeSyncUserDir(baseDir, principal)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "package.mksync"), payload, 0o600); err != nil {
		return err
	}
	return saveKnowledgeSyncPackageMeta(baseDir, principal, meta)
}

func saveKnowledgeSyncPackageMeta(baseDir string, principal *auth.ViewerPrincipal, meta *knowledgeSyncMeta) error {
	dir := knowledgeSyncUserDir(baseDir, principal)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "meta.json"), data, 0o600)
}

func deleteKnowledgeSyncPackage(baseDir string, principal *auth.ViewerPrincipal) error {
	if principal == nil {
		return nil
	}
	return os.RemoveAll(knowledgeSyncUserDir(baseDir, principal))
}

func knowledgeSyncMetaExpired(meta *knowledgeSyncMeta, now time.Time) bool {
	if meta == nil {
		return false
	}
	if raw := strings.TrimSpace(meta.ExpiresAt); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil && !parsed.After(now) {
			return true
		}
	}
	if strings.EqualFold(meta.ServiceStatus, "official_expired") {
		if raw := strings.TrimSpace(meta.OfficialExpiredAt); raw != "" {
			if parsed, err := time.Parse(time.RFC3339, raw); err == nil && !parsed.AddDate(0, 0, knowledgeSyncNormalRetentionDays).After(now) {
				return true
			}
		}
	}
	return false
}

func cleanupKnowledgeSyncPackages(baseDir string, now time.Time) error {
	return filepath.WalkDir(baseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() || d.Name() != "meta.json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var meta knowledgeSyncMeta
		if json.Unmarshal(data, &meta) != nil {
			return nil
		}
		if knowledgeSyncMetaExpired(&meta, now) {
			_ = os.RemoveAll(filepath.Dir(path))
		}
		return nil
	})
}
