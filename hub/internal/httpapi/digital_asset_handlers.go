package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/digitalasset"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const maxDigitalAssetUploadFileBytes int64 = 50 << 20
const maxDigitalAssetLibraryJSONBytes int64 = 1 << 20

type digitalAssetLibraryPatchRequest struct {
	Name           *string  `json:"name"`
	Description    *string  `json:"description"`
	ACLMode        *string  `json:"acl_mode"`
	Departments    []string `json:"departments"`
	SyncEnabled    *bool    `json:"sync_enabled"`
	SetACL         bool     `json:"set_acl"`
	DepartmentsSet bool     `json:"-"`
}

type digitalAssetLibraryCreateRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	ACLMode     string   `json:"acl_mode"`
	Departments []string `json:"departments"`
	SyncEnabled *bool    `json:"sync_enabled"`
}

func readDigitalAssetLibraryJSON(r io.Reader) ([]byte, error) {
	payload, err := io.ReadAll(io.LimitReader(r, maxDigitalAssetLibraryJSONBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > maxDigitalAssetLibraryJSONBytes {
		return nil, fmt.Errorf("request body exceeds %d bytes", maxDigitalAssetLibraryJSONBytes)
	}
	return payload, nil
}

func decodeDigitalAssetJSONPayload(payload []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func decodeDigitalAssetJSON(r io.Reader, dst any) error {
	payload, err := readDigitalAssetLibraryJSON(r)
	if err != nil {
		return err
	}
	return decodeDigitalAssetJSONPayload(payload, dst)
}

func decodeDigitalAssetLibraryPatch(r io.Reader) (digitalAssetLibraryPatchRequest, error) {
	var req digitalAssetLibraryPatchRequest
	var raw map[string]json.RawMessage
	payload, err := readDigitalAssetLibraryJSON(r)
	if err != nil {
		return req, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&raw); err != nil || raw == nil {
		if err == nil {
			err = errors.New("JSON object required")
		}
		return req, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values are not allowed")
		}
		return req, err
	}
	payload, err = json.Marshal(raw)
	if err != nil {
		return req, err
	}
	if err := decodeDigitalAssetJSONPayload(payload, &req); err != nil {
		return req, err
	}
	_, req.DepartmentsSet = raw["departments"]
	if err := validateDigitalAssetACLDepartments(req.Departments); err != nil {
		return req, err
	}
	return req, nil
}

func validateDigitalAssetACLDepartments(departments []string) error {
	uniqueDepartments := make(map[string]struct{}, len(departments))
	for _, department := range departments {
		department = strings.TrimSpace(department)
		if department != "" {
			uniqueDepartments[department] = struct{}{}
		}
	}
	if len(uniqueDepartments) > digitalasset.MaxACLDepartments {
		return fmt.Errorf("departments exceeds %d entries", digitalasset.MaxACLDepartments)
	}
	return nil
}

// readDigitalAssetUploadFile reads one complete upload and rejects, rather than
// silently truncating, files above the per-file limit.
func readDigitalAssetUploadFile(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxDigitalAssetUploadFileBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxDigitalAssetUploadFileBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", maxDigitalAssetUploadFileBytes)
	}
	return data, nil
}

// digitalAssetsFeatureGate returns 404 when feature disabled for the admin tenant.
func digitalAssetsFeatureGate(svc *digitalasset.Service, w http.ResponseWriter, r *http.Request) bool {
	if svc == nil {
		writeError(w, http.StatusNotFound, "FEATURE_DISABLED", "digital assets feature is disabled")
		return false
	}
	tenantID := resolveDigitalAssetTenant(r)
	if !svc.IsFeatureEnabled(r.Context(), tenantID) {
		writeError(w, http.StatusNotFound, "FEATURE_DISABLED", "digital assets feature is disabled")
		return false
	}
	return true
}

// digitalAssetsFeatureGateForTenant returns 404 when feature disabled for a known tenant.
func digitalAssetsFeatureGateForTenant(svc *digitalasset.Service, w http.ResponseWriter, r *http.Request, tenantID string) bool {
	if svc == nil {
		writeError(w, http.StatusNotFound, "FEATURE_DISABLED", "digital assets feature is disabled")
		return false
	}
	if !svc.IsFeatureEnabled(r.Context(), tenantID) {
		writeError(w, http.StatusNotFound, "FEATURE_DISABLED", "digital assets feature is disabled")
		return false
	}
	return true
}

func resolveDigitalAssetTenant(r *http.Request) string {
	if t := AdminTenantID(r.Context()); t != "" {
		return t
	}
	return store.DefaultTenantID
}

// ListDigitalAssetLibrariesAdminHandler GET /api/admin/digital-assets/libraries
func ListDigitalAssetLibrariesAdminHandler(svc *digitalasset.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !digitalAssetsFeatureGate(svc, w, r) {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		items, total, err := svc.ListLibraries(r.Context(), store.DigitalAssetLibraryFilter{
			TenantID: resolveDigitalAssetTenant(r),
			Keyword:  strings.TrimSpace(r.URL.Query().Get("keyword")),
			Status:   strings.TrimSpace(r.URL.Query().Get("status")),
			Limit:    limit,
			Offset:   offset,
		})
		if err != nil {
			writeDigitalAssetError(w, err)
			return
		}
		views := make([]digitalasset.LibraryView, 0, len(items))
		for _, lib := range items {
			views = append(views, digitalasset.LibraryToView(lib))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": views, "total": total, "offset": offset, "limit": limit})
	}
}

// CreateDigitalAssetLibraryAdminHandler POST /api/admin/digital-assets/libraries
func CreateDigitalAssetLibraryAdminHandler(svc *digitalasset.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !digitalAssetsFeatureGate(svc, w, r) {
			return
		}
		var req digitalAssetLibraryCreateRequest
		if err := decodeDigitalAssetJSON(r.Body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		if err := validateDigitalAssetACLDepartments(req.Departments); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			return
		}
		actor := ""
		if a := AdminFromContext(r.Context()); a != nil {
			actor = a.Email
			if actor == "" {
				actor = a.Username
			}
		}
		lib, err := svc.CreateLibrary(r.Context(), digitalasset.CreateLibraryInput{
			TenantID:    resolveDigitalAssetTenant(r),
			Name:        req.Name,
			Description: req.Description,
			ACL: digitalasset.ACL{
				Mode:        req.ACLMode,
				Departments: req.Departments,
			},
			SyncEnabled: req.SyncEnabled,
			Actor:       actor,
		})
		if err != nil {
			writeDigitalAssetError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, digitalasset.LibraryToView(lib))
	}
}

// PatchDigitalAssetLibraryAdminHandler PATCH /api/admin/digital-assets/libraries/{id}
func PatchDigitalAssetLibraryAdminHandler(svc *digitalasset.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !digitalAssetsFeatureGate(svc, w, r) {
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		req, err := decodeDigitalAssetLibraryPatch(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid digital asset update payload: "+err.Error())
			return
		}
		actor := ""
		if a := AdminFromContext(r.Context()); a != nil {
			actor = a.Email
		}
		var acl *digitalasset.ACL
		if req.SetACL || req.ACLMode != nil || req.DepartmentsSet {
			if req.ACLMode == nil || !req.DepartmentsSet {
				writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "acl_mode and departments are required when updating access control")
				return
			}
			acl = &digitalasset.ACL{Mode: *req.ACLMode, Departments: req.Departments}
		}
		lib, err := svc.UpdateLibraryMeta(r.Context(), resolveDigitalAssetTenant(r), id, req.Name, req.Description, acl, req.SyncEnabled, actor)
		if err != nil {
			writeDigitalAssetError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, digitalasset.LibraryToView(lib))
	}
}

// DeleteDigitalAssetLibraryAdminHandler DELETE /api/admin/digital-assets/libraries/{id}
func DeleteDigitalAssetLibraryAdminHandler(svc *digitalasset.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !digitalAssetsFeatureGate(svc, w, r) {
			return
		}
		actor := ""
		if a := AdminFromContext(r.Context()); a != nil {
			actor = a.Email
		}
		if err := svc.SoftDeleteLibrary(r.Context(), resolveDigitalAssetTenant(r), r.PathValue("id"), actor); err != nil {
			writeDigitalAssetError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// SearchDigitalAssetLibraryAdminHandler GET .../libraries/{id}/search
func SearchDigitalAssetLibraryAdminHandler(svc *digitalasset.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !digitalAssetsFeatureGate(svc, w, r) {
			return
		}
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		hits, err := svc.SearchLibrary(r.Context(), resolveDigitalAssetTenant(r), r.PathValue("id"), q, limit)
		if err != nil {
			writeDigitalAssetError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": hits})
	}
}

// ImportDigitalAssetUploadAdminHandler POST .../import/upload
func ImportDigitalAssetUploadAdminHandler(svc *digitalasset.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !digitalAssetsFeatureGate(svc, w, r) {
			return
		}
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_MULTIPART", err.Error())
			return
		}
		files := map[string][]byte{}
		for _, fhs := range r.MultipartForm.File {
			for _, fh := range fhs {
				f, err := fh.Open()
				if err != nil {
					writeError(w, http.StatusBadRequest, "OPEN_FILE_FAILED", err.Error())
					return
				}
				data, err := readDigitalAssetUploadFile(f)
				_ = f.Close()
				if err != nil {
					code := "READ_FILE_FAILED"
					if strings.Contains(err.Error(), "exceeds") {
						code = "FILE_TOO_LARGE"
					}
					writeError(w, http.StatusBadRequest, code, err.Error())
					return
				}
				name := fh.Filename
				if rel := fh.Header.Get("X-Relative-Path"); rel != "" {
					name = rel
				}
				// webkitdirectory provides FileHeader.Filename sometimes as base only;
				// also accept form field relative_paths parallel — for simplicity use Filename.
				if _, exists := files[name]; exists {
					writeError(w, http.StatusBadRequest, "DUPLICATE_FILE_PATH", "duplicate uploaded file path: "+name)
					return
				}
				files[name] = data
			}
		}
		if len(files) == 0 {
			writeError(w, http.StatusBadRequest, "NO_FILES", "no files uploaded")
			return
		}
		actor := ""
		if a := AdminFromContext(r.Context()); a != nil {
			actor = a.Email
		}
		job, err := svc.ImportUploadFiles(r.Context(), resolveDigitalAssetTenant(r), r.PathValue("id"), actor, files, "upload")
		if err != nil {
			writeDigitalAssetError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, digitalasset.JobToView(job))
	}
}

// ImportDigitalAssetArchiveAdminHandler POST .../import/archive
func ImportDigitalAssetArchiveAdminHandler(svc *digitalasset.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !digitalAssetsFeatureGate(svc, w, r) {
			return
		}
		if err := r.ParseMultipartForm(int64(svc.Settings.MaxArchiveUploadBytes) + (1 << 20)); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_MULTIPART", err.Error())
			return
		}
		file, hdr, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "FILE_REQUIRED", "file field required")
			return
		}
		defer file.Close()
		if !strings.HasSuffix(strings.ToLower(hdr.Filename), ".zip") {
			writeError(w, http.StatusBadRequest, "INVALID_ARCHIVE", "only .zip is supported")
			return
		}
		tmp, err := os.CreateTemp("", "dal-archive-*.zip")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TEMP_FAILED", err.Error())
			return
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath)
		maxUpload := svc.Settings.MaxArchiveUploadBytes
		if maxUpload <= 0 {
			maxUpload = 200 << 20
		}
		written, err := io.Copy(tmp, io.LimitReader(file, maxUpload+1))
		if err != nil {
			_ = tmp.Close()
			writeError(w, http.StatusBadRequest, "READ_FAILED", err.Error())
			return
		}
		_ = tmp.Close()
		if written > maxUpload {
			writeError(w, http.StatusBadRequest, "ARCHIVE_TOO_LARGE", "archive exceeds max_archive_upload_bytes")
			return
		}
		actor := ""
		if a := AdminFromContext(r.Context()); a != nil {
			actor = a.Email
		}
		job, err := svc.ImportArchiveZip(r.Context(), resolveDigitalAssetTenant(r), r.PathValue("id"), actor, tmpPath)
		if err != nil {
			writeDigitalAssetError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, digitalasset.JobToView(job))
	}
}

// ImportDigitalAssetLocalDirAdminHandler POST .../import/local-dir
func ImportDigitalAssetLocalDirAdminHandler(svc *digitalasset.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !digitalAssetsFeatureGate(svc, w, r) {
			return
		}
		var req struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		path := filepath.Clean(strings.TrimSpace(req.Path))
		if path == "" || !filepath.IsAbs(path) {
			writeError(w, http.StatusBadRequest, "INVALID_PATH", "absolute path required")
			return
		}
		tenantID := resolveDigitalAssetTenant(r)
		cfg := svc.LoadTenantSettings(r.Context(), tenantID)
		allowlist := cfg.LocalDirAllowlist
		if len(allowlist) == 0 {
			allowlist = svc.Settings.LocalDirAllowlist
		}
		if len(allowlist) == 0 {
			writeError(w, http.StatusForbidden, "PATH_NOT_ALLOWED", "local_dir_allowlist is empty; configure digital_assets.local_dir_allowlist in hub config or tenant settings before importing a server directory")
			return
		}
		allowed := false
		for _, prefix := range allowlist {
			prefix = filepath.Clean(prefix)
			if prefix != "" && (path == prefix || strings.HasPrefix(path, prefix+string(filepath.Separator))) {
				allowed = true
				break
			}
		}
		if !allowed {
			writeError(w, http.StatusForbidden, "PATH_NOT_ALLOWED", "path not in local_dir_allowlist")
			return
		}
		actor := ""
		if a := AdminFromContext(r.Context()); a != nil {
			actor = a.Email
		}
		job, err := svc.BeginImportLocalDir(r.Context(), tenantID, r.PathValue("id"), path, actor)
		if err != nil {
			writeDigitalAssetError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, digitalasset.JobToView(job))
	}
}

// ImportDigitalAssetKnowledgeShareAdminHandler POST .../import/knowledge-share
func ImportDigitalAssetKnowledgeShareAdminHandler(svc *digitalasset.Service, loader digitalasset.SharePackageLoader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !digitalAssetsFeatureGate(svc, w, r) {
			return
		}
		var req struct {
			ShareRef   string `json:"share_ref"`
			ImportMode string `json:"import_mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		actor, email := digitalAssetAdminActor(r)
		job, err := svc.ImportKnowledgeShare(r.Context(), loader, digitalasset.ImportKnowledgeShareInput{
			TenantID:                resolveDigitalAssetTenant(r),
			LibraryID:               r.PathValue("id"),
			ShareRef:                req.ShareRef,
			ImportMode:              req.ImportMode,
			Actor:                   actor,
			ActorEmail:              email,
			AllowAdminImportPrivate: svc.Settings.AllowAdminImportPrivate,
		})
		if err != nil {
			// Job may still exist (failed) for history/polling; prefer JobView when present.
			if job != nil {
				writeJSON(w, http.StatusAccepted, digitalasset.JobToView(job))
				return
			}
			writeDigitalAssetError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, digitalasset.JobToView(job))
	}
}

// ImportDigitalAssetBrowserDirAdminHandler POST .../import/browser-dir
// Accepts multipart files with webkitRelativePath (or relative_paths[] + files).
func ImportDigitalAssetBrowserDirAdminHandler(svc *digitalasset.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !digitalAssetsFeatureGate(svc, w, r) {
			return
		}
		maxBytes := svc.Settings.MaxBrowserDirBytes
		if maxBytes <= 0 {
			maxBytes = 500 << 20
		}
		if err := r.ParseMultipartForm(maxBytes + (1 << 20)); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_MULTIPART", err.Error())
			return
		}
		files := map[string][]byte{}
		var total int64
		maxFiles := svc.Settings.MaxBrowserDirFiles
		if maxFiles <= 0 {
			maxFiles = 2000
		}
		for _, fhs := range r.MultipartForm.File {
			for _, fh := range fhs {
				if len(files) >= maxFiles {
					writeError(w, http.StatusBadRequest, "TOO_MANY_FILES", "exceeds max_browser_dir_files")
					return
				}
				name := fh.Filename
				if rel := fh.Header.Get("Content-Disposition"); strings.Contains(rel, "filename=") {
					// prefer explicit relative path field if present
				}
				if rp := strings.TrimSpace(fh.Header.Get("X-Relative-Path")); rp != "" {
					name = rp
				}
				// Also accept form field webkitRelativePath via Filename when browser sends full relative path
				f, err := fh.Open()
				if err != nil {
					writeError(w, http.StatusBadRequest, "OPEN_FILE_FAILED", err.Error())
					return
				}
				data, err := readDigitalAssetUploadFile(f)
				_ = f.Close()
				if err != nil {
					code := "READ_FILE_FAILED"
					if strings.Contains(err.Error(), "exceeds") {
						code = "FILE_TOO_LARGE"
					}
					writeError(w, http.StatusBadRequest, code, err.Error())
					return
				}
				total += int64(len(data))
				if total > maxBytes {
					writeError(w, http.StatusBadRequest, "TOO_LARGE", "exceeds max_browser_dir_bytes")
					return
				}
				if _, exists := files[name]; exists {
					writeError(w, http.StatusBadRequest, "DUPLICATE_FILE_PATH", "duplicate uploaded file path: "+name)
					return
				}
				files[name] = data
			}
		}
		// relative_paths[] parallel array optional
		if r.MultipartForm != nil {
			if rels := r.MultipartForm.Value["relative_paths"]; len(rels) > 0 {
				// rebuild keys if lengths match files values order — skip for simplicity
				_ = rels
			}
		}
		if len(files) == 0 {
			writeError(w, http.StatusBadRequest, "NO_FILES", "no files uploaded")
			return
		}
		actor, _ := digitalAssetAdminActor(r)
		job, err := svc.ImportUploadFiles(r.Context(), resolveDigitalAssetTenant(r), r.PathValue("id"), actor, files, "browser_dir")
		if err != nil {
			writeDigitalAssetError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, digitalasset.JobToView(job))
	}
}

// MergeDigitalAssetLibrariesAdminHandler POST /api/admin/digital-assets/libraries/merge
func MergeDigitalAssetLibrariesAdminHandler(svc *digitalasset.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !digitalAssetsFeatureGate(svc, w, r) {
			return
		}
		var req struct {
			TargetLibraryID  string   `json:"target_library_id"`
			SourceLibraryIDs []string `json:"source_library_ids"`
			ArchiveSources   *bool    `json:"archive_sources"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		actor, _ := digitalAssetAdminActor(r)
		job, err := svc.MergeLibraries(r.Context(), digitalasset.MergeLibrariesInput{
			TenantID: resolveDigitalAssetTenant(r), TargetLibraryID: req.TargetLibraryID,
			SourceLibraryIDs: req.SourceLibraryIDs, ArchiveSources: req.ArchiveSources, Actor: actor,
		})
		if err != nil {
			if job != nil {
				writeJSON(w, http.StatusAccepted, digitalasset.JobToView(job))
				return
			}
			writeDigitalAssetError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, digitalasset.JobToView(job))
	}
}

// ExportDigitalAssetBackupAdminHandler POST /api/admin/digital-assets/export
func ExportDigitalAssetBackupAdminHandler(svc *digitalasset.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !digitalAssetsFeatureGate(svc, w, r) {
			return
		}
		var req struct {
			LibraryIDs []string `json:"library_ids"`
			All        bool     `json:"all"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && r.ContentLength != 0 {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		actor, _ := digitalAssetAdminActor(r)
		res, err := svc.ExportBackup(r.Context(), digitalasset.ExportBackupInput{
			TenantID: resolveDigitalAssetTenant(r), LibraryIDs: req.LibraryIDs, All: req.All || len(req.LibraryIDs) == 0, Actor: actor,
		})
		if err != nil {
			writeDigitalAssetError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{
			"job_id": res.JobID, "status": res.Status,
			"download_path": "/api/admin/digital-assets/export/jobs/" + res.JobID + "/download",
		})
	}
}

// DownloadDigitalAssetBackupAdminHandler GET .../export/jobs/{job_id}/download
func DownloadDigitalAssetBackupAdminHandler(svc *digitalasset.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !digitalAssetsFeatureGate(svc, w, r) {
			return
		}
		tenantID := resolveDigitalAssetTenant(r)
		jobID := r.PathValue("job_id")
		path, err := svc.BackupDownloadPath(tenantID, jobID)
		if err != nil {
			writeError(w, http.StatusNotFound, "BACKUP_NOT_FOUND", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+jobID+".zip\"")
		http.ServeFile(w, r, path)
	}
}

// ImportDigitalAssetBackupAdminHandler POST /api/admin/digital-assets/import/backup
func ImportDigitalAssetBackupAdminHandler(svc *digitalasset.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !digitalAssetsFeatureGate(svc, w, r) {
			return
		}
		if err := r.ParseMultipartForm(512 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_MULTIPART", err.Error())
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "FILE_REQUIRED", "file field required")
			return
		}
		defer file.Close()
		tmp, err := os.CreateTemp("", "dal-backup-*.zip")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TEMP_FAILED", err.Error())
			return
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath)
		if _, err := io.Copy(tmp, file); err != nil {
			_ = tmp.Close()
			writeError(w, http.StatusBadRequest, "READ_FAILED", err.Error())
			return
		}
		_ = tmp.Close()
		mode := r.FormValue("mode")
		target := r.FormValue("target_library_id")
		restoreACL := r.FormValue("restore_acl") != "false"
		actor, _ := digitalAssetAdminActor(r)
		job, err := svc.ImportBackup(r.Context(), digitalasset.ImportBackupInput{
			TenantID: resolveDigitalAssetTenant(r), ZipPath: tmpPath, Mode: mode,
			TargetLibraryID: target, RestoreACL: restoreACL, Actor: actor,
		})
		if err != nil {
			writeDigitalAssetError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"job_id": job.ID, "status": job.Status, "error": job.ErrorMessage})
	}
}

func digitalAssetAdminActor(r *http.Request) (actor, email string) {
	if a := AdminFromContext(r.Context()); a != nil {
		email = strings.TrimSpace(a.Email)
		actor = email
		if actor == "" {
			actor = a.Username
		}
	}
	return actor, email
}

// GetDigitalAssetImportJobAdminHandler GET .../import/jobs/{job_id}
func GetDigitalAssetImportJobAdminHandler(svc *digitalasset.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !digitalAssetsFeatureGate(svc, w, r) {
			return
		}
		view, err := svc.GetImportJob(r.Context(), resolveDigitalAssetTenant(r), r.PathValue("job_id"))
		if err != nil {
			writeDigitalAssetError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// ListDigitalAssetLibrarySourcesAdminHandler GET .../libraries/{id}/sources
// Query: limit (default 200, max 1000), offset (default 0), q (optional filter).
func ListDigitalAssetLibrarySourcesAdminHandler(svc *digitalasset.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !digitalAssetsFeatureGate(svc, w, r) {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		page, err := svc.ListLibrarySources(r.Context(), resolveDigitalAssetTenant(r), r.PathValue("id"),
			strings.TrimSpace(r.URL.Query().Get("q")), limit, offset)
		if err != nil {
			writeDigitalAssetError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items":    page.Items,
			"total":    page.Total,
			"limit":    page.Limit,
			"offset":   page.Offset,
			"count":    len(page.Items),
			"has_more": page.HasMore,
		})
	}
}

// DeleteDigitalAssetLibrarySourceAdminHandler DELETE .../libraries/{id}/sources/{source_id}
func DeleteDigitalAssetLibrarySourceAdminHandler(svc *digitalasset.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !digitalAssetsFeatureGate(svc, w, r) {
			return
		}
		actor, _ := digitalAssetAdminActor(r)
		res, err := svc.DeleteLibrarySource(r.Context(), resolveDigitalAssetTenant(r),
			r.PathValue("id"), r.PathValue("source_id"), actor)
		if err != nil {
			// Partial delete with package advanced: still return body (includes error field).
			if res != nil && res.Deleted > 0 {
				if strings.TrimSpace(res.Error) == "" {
					res.Error = err.Error()
				}
				writeJSON(w, http.StatusOK, res)
				return
			}
			writeDigitalAssetError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

// DeleteDigitalAssetLibrarySourcesBatchAdminHandler POST .../libraries/{id}/sources/delete
// Body: { "source_ids": ["...", "..."] } - deletes multiple sources in one content_rev bump.
func DeleteDigitalAssetLibrarySourcesBatchAdminHandler(svc *digitalasset.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !digitalAssetsFeatureGate(svc, w, r) {
			return
		}
		var req struct {
			SourceIDs []string `json:"source_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		actor, _ := digitalAssetAdminActor(r)
		res, err := svc.DeleteLibrarySources(r.Context(), resolveDigitalAssetTenant(r),
			r.PathValue("id"), req.SourceIDs, actor)
		if err != nil {
			if res != nil && res.Deleted > 0 {
				if strings.TrimSpace(res.Error) == "" {
					res.Error = err.Error()
				}
				writeJSON(w, http.StatusOK, res)
				return
			}
			writeDigitalAssetError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

// ListDigitalAssetLibraryImportJobsAdminHandler GET .../libraries/{id}/import-jobs
func ListDigitalAssetLibraryImportJobsAdminHandler(svc *digitalasset.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !digitalAssetsFeatureGate(svc, w, r) {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		items, err := svc.ListImportJobs(r.Context(), resolveDigitalAssetTenant(r), r.PathValue("id"), limit)
		if err != nil {
			writeDigitalAssetError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
	}
}

// GetDigitalAssetSettingsAdminHandler GET /api/admin/digital-assets/settings
// Always available to admins so the System Settings UI can toggle the feature when it is off.
func GetDigitalAssetSettingsAdminHandler(svc *digitalasset.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusNotFound, "FEATURE_DISABLED", "digital assets feature is disabled")
			return
		}
		tenantID := resolveDigitalAssetTenant(r)
		settings := svc.LoadTenantSettings(r.Context(), tenantID)
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":      settings.Enabled,
			"sync_enabled": settings.SyncEnabled,
			"settings":     settings,
		})
	}
}

// PutDigitalAssetSettingsAdminHandler PUT /api/admin/digital-assets/settings
// Updates tenant-level enabled / sync_enabled (persisted; no hub config restart required).
func PutDigitalAssetSettingsAdminHandler(svc *digitalasset.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusNotFound, "FEATURE_DISABLED", "digital assets feature is disabled")
			return
		}
		if svc.System == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "digital assets settings store is unavailable")
			return
		}
		var req struct {
			Enabled     *bool `json:"enabled"`
			SyncEnabled *bool `json:"sync_enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		if req.Enabled == nil && req.SyncEnabled == nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "enabled or sync_enabled is required")
			return
		}
		tenantID := resolveDigitalAssetTenant(r)
		settings, err := svc.UpdateTenantSettings(r.Context(), tenantID, digitalasset.SettingsUpdate{
			Enabled:     req.Enabled,
			SyncEnabled: req.SyncEnabled,
		})
		if err != nil {
			writeDigitalAssetError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":      settings.Enabled,
			"sync_enabled": settings.SyncEnabled,
			"settings":     settings,
		})
	}
}

// --- User / Viewer sync APIs ---

// ListDigitalAssetLibrariesUserHandler GET /api/digital-assets/libraries
func ListDigitalAssetLibrariesUserHandler(svc *digitalasset.Service, identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		viewer, err := authenticateViewerRequest(r, identity)
		if err != nil || viewer == nil {
			writeError(w, http.StatusUnauthorized, "VIEWER_UNAUTHORIZED", "viewer authorization required")
			return
		}
		if !digitalAssetsFeatureGateForTenant(svc, w, r, viewer.TenantID) {
			return
		}
		tenantOn, libs, err := svc.BuildManifest(r.Context(), viewer.TenantID, viewer.Email)
		if err != nil {
			writeDigitalAssetError(w, err)
			return
		}
		cfg := svc.LoadTenantSettings(r.Context(), viewer.TenantID)
		writeJSON(w, http.StatusOK, map[string]any{
			"tenant_sync_enabled": tenantOn && cfg.SyncEnabled,
			"libraries":           libs,
		})
	}
}

// DigitalAssetSyncManifestHandler GET /api/digital-assets/sync/manifest
func DigitalAssetSyncManifestHandler(svc *digitalasset.Service, identity *auth.IdentityService) http.HandlerFunc {
	return ListDigitalAssetLibrariesUserHandler(svc, identity)
}

// DigitalAssetSyncPullHandler POST /api/digital-assets/sync/pull
func DigitalAssetSyncPullHandler(svc *digitalasset.Service, identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		viewer, err := authenticateViewerRequest(r, identity)
		if err != nil || viewer == nil {
			writeError(w, http.StatusUnauthorized, "VIEWER_UNAUTHORIZED", "viewer authorization required")
			return
		}
		if !digitalAssetsFeatureGateForTenant(svc, w, r, viewer.TenantID) {
			return
		}
		var req struct {
			LibraryID string `json:"library_id"`
			SinceRev  int64  `json:"since_rev"`
			DeviceID  string `json:"device_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		reason, ops, err := svc.Pull(r.Context(), viewer.TenantID, req.LibraryID, viewer.Email, viewer.UserID, req.DeviceID, req.SinceRev)
		if err != nil {
			writeDigitalAssetError(w, err)
			return
		}
		if strings.HasPrefix(reason, "rate_limited:") {
			retry, _ := strconv.Atoi(strings.TrimPrefix(reason, "rate_limited:"))
			w.Header().Set("Retry-After", strconv.Itoa(max(1, retry/1000)))
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"reason": "rate_limited", "retry_after_ms": retry})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"reason":   reason,
			"ops":      ops,
			"complete": true,
		})
	}
}

// DigitalAssetSyncPackageHandler GET /api/digital-assets/libraries/{id}/sync/packages/{rev}
func DigitalAssetSyncPackageHandler(svc *digitalasset.Service, identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		viewer, err := authenticateViewerRequest(r, identity)
		if err != nil || viewer == nil {
			writeError(w, http.StatusUnauthorized, "VIEWER_UNAUTHORIZED", "viewer authorization required")
			return
		}
		if !digitalAssetsFeatureGateForTenant(svc, w, r, viewer.TenantID) {
			return
		}
		libID := r.PathValue("id")
		rev, _ := strconv.ParseInt(r.PathValue("rev"), 10, 64)
		// ACL check via GetLibrary + CanAccess
		lib, err := svc.GetLibrary(r.Context(), viewer.TenantID, libID)
		if err != nil {
			writeDigitalAssetError(w, err)
			return
		}
		if svc.ACL != nil {
			ok, aerr := svc.ACL.CanAccessLibrary(r.Context(), lib, viewer.Email)
			if aerr != nil || !ok {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "not found")
				return
			}
		}
		path, err := svc.PackagePathForRev(r.Context(), viewer.TenantID, libID, rev)
		if err != nil {
			writeError(w, http.StatusNotFound, "PACKAGE_NOT_READY", err.Error())
			return
		}
		http.ServeFile(w, r, path)
	}
}

func writeDigitalAssetError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, digitalasset.ErrFeatureDisabled):
		writeError(w, http.StatusNotFound, "FEATURE_DISABLED", err.Error())
	case errors.Is(err, digitalasset.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
	case errors.Is(err, digitalasset.ErrForbidden):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "not found") // 404-on-deny
	case errors.Is(err, digitalasset.ErrInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
	default:
		writeError(w, http.StatusBadRequest, "DIGITAL_ASSET_ERROR", err.Error())
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
