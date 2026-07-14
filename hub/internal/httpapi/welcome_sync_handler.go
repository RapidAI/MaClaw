package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

const (
	// Welcome sync is a small UX preference blob (templates / role / recent).
	welcomeSyncMaxBytes int64 = 512 << 10 // 512 KiB
	// Expected export kind from maclaw GUI welcomeTaskMemory.ts.
	welcomeSyncExpectedKind = "maclaw-welcome-custom-templates"
)

// WelcomeSyncView is returned by status / upload endpoints.
type WelcomeSyncView struct {
	OwnerUserID     string `json:"owner_user_id,omitempty"`
	OwnerUserEmail  string `json:"owner_user_email,omitempty"`
	TenantID        string `json:"tenant_id,omitempty"`
	HasDocument     bool   `json:"has_document"`
	Revision        string `json:"revision,omitempty"`
	StoredSizeBytes int64  `json:"stored_size_bytes,omitempty"`
	TemplateCount   int    `json:"template_count,omitempty"`
	Kind            string `json:"kind,omitempty"`
	ExportedAt      string `json:"exported_at,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
	LimitBytes      int64  `json:"limit_bytes"`
	Message         string `json:"message,omitempty"`
}

type welcomeSyncMeta struct {
	OwnerUserID     string `json:"owner_user_id"`
	OwnerUserEmail  string `json:"owner_user_email,omitempty"`
	TenantID        string `json:"tenant_id"`
	Revision        string `json:"revision"`
	StoredSizeBytes int64  `json:"stored_size_bytes"`
	TemplateCount   int    `json:"template_count,omitempty"`
	Kind            string `json:"kind,omitempty"`
	ExportedAt      string `json:"exported_at,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type welcomeSyncUploadRequest struct {
	// Payload is the full welcome export JSON object (preferred).
	Payload json.RawMessage `json:"payload"`
	// PayloadJSON accepts a pre-stringified document (fallback).
	PayloadJSON string `json:"payload_json,omitempty"`
	// IfMatchRevision enables optimistic concurrency: 409 when server differs.
	IfMatchRevision string `json:"if_match_revision,omitempty"`
	// ClientUpdatedAt is recorded for diagnostics only.
	ClientUpdatedAt string `json:"client_updated_at,omitempty"`
}

// WelcomeSyncStatusHandler returns whether the signed-in user has a cloud welcome document.
func WelcomeSyncStatusHandler(identity *auth.IdentityService, baseDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := requireKnowledgeShareViewer(w, r, identity)
		if !ok {
			return
		}
		meta, path := loadWelcomeSyncMeta(baseDir, principal)
		writeJSON(w, http.StatusOK, welcomeSyncViewFromMeta(meta, path != "", principal))
	}
}

// UploadWelcomeSyncHandler stores (or overwrites) the welcome document for the viewer.
func UploadWelcomeSyncHandler(identity *auth.IdentityService, baseDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := requireKnowledgeShareViewer(w, r, identity)
		if !ok {
			return
		}
		var req welcomeSyncUploadRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, welcomeSyncMaxBytes*2)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		raw, err := normalizeWelcomeSyncPayload(req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error())
			return
		}
		if int64(len(raw)) > welcomeSyncMaxBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "WELCOME_SYNC_TOO_LARGE",
				fmt.Sprintf("welcome sync document exceeds %d bytes", welcomeSyncMaxBytes))
			return
		}
		if err := validateWelcomeSyncPayload(raw); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error())
			return
		}

		existing, existingPath := loadWelcomeSyncMeta(baseDir, principal)
		ifMatch := strings.TrimSpace(req.IfMatchRevision)
		if ifMatch != "" && existing != nil && existingPath != "" && strings.TrimSpace(existing.Revision) != "" && existing.Revision != ifMatch {
			writeErrorWithFields(w, http.StatusConflict, "WELCOME_SYNC_CONFLICT",
				"cloud welcome document was updated on another device",
				map[string]any{"status": welcomeSyncViewFromMeta(existing, true, principal)})
			return
		}

		summary := summarizeWelcomeSyncPayload(raw)
		now := time.Now().UTC().Format(time.RFC3339)
		rev := welcomeSyncRevision(raw)
		meta := &welcomeSyncMeta{
			OwnerUserID:     principal.UserID,
			OwnerUserEmail:  principal.Email,
			TenantID:        principal.TenantID,
			Revision:        rev,
			StoredSizeBytes: int64(len(raw)),
			TemplateCount:   summary.templateCount,
			Kind:            summary.kind,
			ExportedAt:      summary.exportedAt,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if existing != nil && existingPath != "" && strings.TrimSpace(existing.CreatedAt) != "" {
			meta.CreatedAt = existing.CreatedAt
		}
		if err := saveWelcomeSyncDocument(baseDir, principal, raw, meta); err != nil {
			writeError(w, http.StatusInternalServerError, "WELCOME_SYNC_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, welcomeSyncViewFromMeta(meta, true, principal))
	}
}

// DownloadWelcomeSyncHandler returns the stored welcome JSON document.
func DownloadWelcomeSyncHandler(identity *auth.IdentityService, baseDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := requireKnowledgeShareViewer(w, r, identity)
		if !ok {
			return
		}
		meta, path := loadWelcomeSyncMeta(baseDir, principal)
		if meta == nil || path == "" {
			writeError(w, http.StatusNotFound, "WELCOME_SYNC_NOT_FOUND", "no cloud welcome document")
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "WELCOME_SYNC_READ_FAILED", err.Error())
			return
		}
		// Cap download size to the same quota used on upload.
		if int64(len(data)) > welcomeSyncMaxBytes*2 {
			writeError(w, http.StatusInternalServerError, "WELCOME_SYNC_CORRUPT", "stored document exceeds safe size")
			return
		}
		revision := strings.TrimSpace(meta.Revision)
		if revision == "" {
			revision = welcomeSyncRevision(data)
		}
		templateCount := meta.TemplateCount
		if templateCount <= 0 {
			templateCount = summarizeWelcomeSyncPayload(data).templateCount
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Welcome-Sync-Revision", revision)
		w.Header().Set("X-Welcome-Sync-Updated-At", meta.UpdatedAt)
		if templateCount > 0 {
			w.Header().Set("X-Welcome-Sync-Template-Count", fmt.Sprintf("%d", templateCount))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}

// DeleteWelcomeSyncHandler removes the cloud welcome document.
func DeleteWelcomeSyncHandler(identity *auth.IdentityService, baseDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := requireKnowledgeShareViewer(w, r, identity)
		if !ok {
			return
		}
		if err := deleteWelcomeSyncDocument(baseDir, principal); err != nil {
			writeError(w, http.StatusInternalServerError, "WELCOME_SYNC_DELETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "deleted"})
	}
}

type welcomeSyncSummary struct {
	kind          string
	exportedAt    string
	templateCount int
}

func normalizeWelcomeSyncPayload(req welcomeSyncUploadRequest) ([]byte, error) {
	if len(req.Payload) > 0 {
		var probe any
		if err := json.Unmarshal(req.Payload, &probe); err != nil {
			return nil, fmt.Errorf("payload is not valid JSON")
		}
		// Re-marshal for stable storage (compact).
		return json.Marshal(probe)
	}
	text := strings.TrimSpace(req.PayloadJSON)
	if text == "" {
		return nil, fmt.Errorf("payload or payload_json is required")
	}
	var probe any
	if err := json.Unmarshal([]byte(text), &probe); err != nil {
		return nil, fmt.Errorf("payload_json is not valid JSON")
	}
	return json.Marshal(probe)
}

func validateWelcomeSyncPayload(raw []byte) error {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("payload is not a JSON object")
	}
	// Accept official kind, or a bare templates list object without kind (lenient).
	kind, _ := obj["kind"].(string)
	kind = strings.TrimSpace(kind)
	if kind != "" && kind != welcomeSyncExpectedKind {
		return fmt.Errorf("unexpected kind %q (want %s)", kind, welcomeSyncExpectedKind)
	}
	templatesRaw, hasTemplates := obj["templates"]
	if !hasTemplates && kind == "" {
		// Bare objects without kind must still look like a template backup.
		return fmt.Errorf("payload must include templates[] or kind=%s", welcomeSyncExpectedKind)
	}
	if hasTemplates {
		if _, ok := templatesRaw.([]any); !ok {
			return fmt.Errorf("templates must be an array")
		}
	}
	return nil
}

func summarizeWelcomeSyncPayload(raw []byte) welcomeSyncSummary {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return welcomeSyncSummary{}
	}
	out := welcomeSyncSummary{}
	if kind, _ := obj["kind"].(string); kind != "" {
		out.kind = kind
	} else {
		out.kind = welcomeSyncExpectedKind
	}
	if exportedAt, _ := obj["exportedAt"].(string); exportedAt != "" {
		out.exportedAt = exportedAt
	} else if exportedAt, _ := obj["exported_at"].(string); exportedAt != "" {
		out.exportedAt = exportedAt
	}
	if templates, ok := obj["templates"].([]any); ok {
		out.templateCount = len(templates)
	}
	return out
}

func welcomeSyncRevision(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func welcomeSyncViewFromMeta(meta *welcomeSyncMeta, hasDocument bool, principal *auth.ViewerPrincipal) WelcomeSyncView {
	view := WelcomeSyncView{
		LimitBytes: welcomeSyncMaxBytes,
		Message:    "引导页模板云同步：按账号保存一份（角色/最近任务/自定义模板）。手动上传或拉取，冲突时可选择合并或替换。",
	}
	if principal != nil {
		view.OwnerUserID = principal.UserID
		view.OwnerUserEmail = principal.Email
		view.TenantID = principal.TenantID
	}
	if meta == nil || !hasDocument {
		return view
	}
	view.HasDocument = true
	view.Revision = meta.Revision
	view.StoredSizeBytes = meta.StoredSizeBytes
	view.TemplateCount = meta.TemplateCount
	view.Kind = meta.Kind
	view.ExportedAt = meta.ExportedAt
	view.CreatedAt = meta.CreatedAt
	view.UpdatedAt = meta.UpdatedAt
	return view
}

func welcomeSyncUserDir(baseDir string, principal *auth.ViewerPrincipal) string {
	user := sanitizeKnowledgeSyncPathPart(knowledgeSyncUserStorageKey(principal))
	return filepath.Join(baseDir, "_email", user)
}

func loadWelcomeSyncMeta(baseDir string, principal *auth.ViewerPrincipal) (*welcomeSyncMeta, string) {
	if principal == nil || strings.TrimSpace(baseDir) == "" {
		return nil, ""
	}
	dir := welcomeSyncUserDir(baseDir, principal)
	metaPath := filepath.Join(dir, "meta.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, ""
	}
	var meta welcomeSyncMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, ""
	}
	docPath := filepath.Join(dir, "document.json")
	if _, err := os.Stat(docPath); err != nil {
		// Orphan meta without document — treat as no document.
		return &meta, ""
	}
	return &meta, docPath
}

func saveWelcomeSyncDocument(baseDir string, principal *auth.ViewerPrincipal, raw []byte, meta *welcomeSyncMeta) error {
	dir := welcomeSyncUserDir(baseDir, principal)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Atomic-ish write: temp then replace. On Windows, Rename cannot overwrite, so
	// remove the destination first when needed.
	if err := writeWelcomeSyncFileAtomic(filepath.Join(dir, "document.json"), raw); err != nil {
		return err
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return writeWelcomeSyncFileAtomic(filepath.Join(dir, "meta.json"), metaBytes)
}

func writeWelcomeSyncFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err == nil {
		return nil
	}
	// Windows (and some NFS setups): rename fails when dest exists.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func deleteWelcomeSyncDocument(baseDir string, principal *auth.ViewerPrincipal) error {
	if principal == nil {
		return nil
	}
	return os.RemoveAll(welcomeSyncUserDir(baseDir, principal))
}
