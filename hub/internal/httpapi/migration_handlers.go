package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	migrationSettingMaxCompressedBytes = "migration_max_compressed_bytes"
	migrationDefaultMaxCompressedBytes = int64(100 * 1024 * 1024)
	migrationMinCompressedBytes        = int64(100 * 1024 * 1024)
	migrationMaxCompressedBytes        = int64(1024 * 1024 * 1024)
	migrationMinUploadChunkSize        = int64(256 * 1024)
	migrationMaxUploadChunkSize        = int64(8 * 1024 * 1024)
	migrationMaxUploadChunks           = 8192
	migrationMaxCreateBodyBytes        = int64(18 << 20)
	migrationClaimLease                = 2 * time.Hour
)

var errMigrationExportNotReady = errors.New("migration export is not ready")

type migrationMachineLister interface {
	ListByUserID(ctx context.Context, userID string) ([]*store.Machine, error)
	GetByID(ctx context.Context, id string) (*store.Machine, error)
}

type migrationAPI struct {
	db       *sql.DB
	rootDir  string
	identity *auth.IdentityService
	machines migrationMachineLister
	system   store.SystemSettingsRepository
}

type migrationPrincipal struct {
	TenantID    string
	UserID      string
	Email       string
	MachineID   string
	MachineName string
}

type migrationExportRow struct {
	ID                 string
	TenantID           string
	UserID             string
	SourceMachineID    string
	SourceMachineName  string
	Status             string
	CompressedSize     int64
	EncryptedSize      int64
	EncryptedSHA256    string
	PlainSHA256        string
	ChunkSize          int64
	ChunkCount         int
	ManifestJSON       string
	ClaimedByMachineID string
	ClaimedAt          sql.NullString
	ClaimExpiresAt     sql.NullString
	CreatedAt          string
	UpdatedAt          string
	ImportedAt         sql.NullString
	DeletedAt          sql.NullString
}

type migrationPublicManifest struct {
	Version          string                              `json:"version"`
	CreatedAt        time.Time                           `json:"created_at"`
	TenantID         string                              `json:"tenant_id,omitempty"`
	TenantName       string                              `json:"tenant_name,omitempty"`
	UserID           string                              `json:"user_id,omitempty"`
	Email            string                              `json:"email,omitempty"`
	MachineID        string                              `json:"machine_id,omitempty"`
	MachineName      string                              `json:"machine_name,omitempty"`
	MemoryEntries    int                                 `json:"memory_entries"`
	KnowledgeBytes   int64                               `json:"knowledge_bytes"`
	AssetBytes       int64                               `json:"asset_bytes"`
	PetPackBytes     int64                               `json:"pet_pack_bytes"`
	PetPacksIncluded bool                                `json:"pet_packs_included,omitempty"`
	ConfigSchema     string                              `json:"config_schema_version,omitempty"`
	ConfigSections   int                                 `json:"config_section_count,omitempty"`
	SecretCount      int                                 `json:"secret_count,omitempty"`
	ExcludedConfig   []string                            `json:"excluded_config_paths,omitempty"`
	Files            []migrationPublicManifestFileDigest `json:"files"`
	Meta             *migrationPublicManifestMeta        `json:"meta,omitempty"`
}

type migrationPublicManifestFileDigest struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type migrationPublicManifestMeta struct {
	Host     string   `json:"host,omitempty"`
	Contains []string `json:"contains,omitempty"`
}

func NewMigrationAPI(db *sql.DB, rootDir string, identity *auth.IdentityService, machines migrationMachineLister, system store.SystemSettingsRepository) *migrationAPI {
	return &migrationAPI{db: db, rootDir: filepath.Join(rootDir, "user-data-migrations"), identity: identity, machines: machines, system: system}
}

func (api *migrationAPI) RegisterRoutes(mux *http.ServeMux, requireTenantAdmin func(http.HandlerFunc) http.HandlerFunc) {
	if api == nil || mux == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/migration/instances", api.handleInstances)
	mux.HandleFunc("GET /api/v1/migration/exports/current", api.handleCurrentExport)
	mux.HandleFunc("POST /api/v1/migration/exports", api.handleCreateExport)
	mux.HandleFunc("GET /api/v1/migration/exports/{exportID}/chunks/{index}/status", api.handleChunkStatus)
	mux.HandleFunc("PUT /api/v1/migration/exports/{exportID}/chunks/{index}", api.handlePutChunk)
	mux.HandleFunc("POST /api/v1/migration/exports/{exportID}/complete-upload", api.handleCompleteUpload)
	mux.HandleFunc("POST /api/v1/migration/imports/{exportID}/claim", api.handleClaimImport)
	mux.HandleFunc("GET /api/v1/migration/imports/{exportID}/chunks/{index}", api.handleGetChunk)
	mux.HandleFunc("POST /api/v1/migration/imports/{exportID}/complete", api.handleCompleteImport)
	mux.HandleFunc("POST /api/v1/migration/imports/{exportID}/abort", api.handleAbortImport)
	if requireTenantAdmin != nil {
		mux.HandleFunc("GET /api/admin/migration/settings", requireTenantAdmin(api.handleAdminSettings))
		mux.HandleFunc("PUT /api/admin/migration/settings", requireTenantAdmin(api.handleAdminSettings))
	}
}

func (api *migrationAPI) principalFromRequest(w http.ResponseWriter, r *http.Request) (*migrationPrincipal, bool) {
	if api == nil || api.identity == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Hub login is required")
		return nil, false
	}
	if machineID := strings.TrimSpace(r.Header.Get("X-Machine-ID")); machineID != "" {
		token := extractBearerToken(r)
		if token != "" {
			principal, err := api.identity.AuthenticateMachine(r.Context(), machineID, token)
			if err == nil && principal != nil {
				name := api.machineName(r.Context(), principal.TenantID, principal.UserID, principal.MachineID)
				return &migrationPrincipal{TenantID: principal.TenantID, UserID: principal.UserID, MachineID: principal.MachineID, MachineName: name}, true
			}
		}
	}
	viewer, err := authenticateViewerRequest(r, api.identity)
	if err != nil || viewer == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
		return nil, false
	}
	machineID := strings.TrimSpace(r.Header.Get("X-MaClaw-Machine-ID"))
	if machineID == "" {
		machineID = strings.TrimSpace(r.URL.Query().Get("machine_id"))
	}
	if machineID == "" {
		writeError(w, http.StatusBadRequest, "MACHINE_REQUIRED", "machine id is required")
		return nil, false
	}
	machine, err := api.machines.GetByID(r.Context(), machineID)
	if err != nil || machine == nil || !sameTenantUser(machine.TenantID, machine.UserID, viewer.TenantID, viewer.UserID) {
		writeError(w, http.StatusForbidden, "MACHINE_FORBIDDEN", "machine is outside current tenant/user")
		return nil, false
	}
	return &migrationPrincipal{TenantID: viewer.TenantID, UserID: viewer.UserID, Email: viewer.Email, MachineID: machine.ID, MachineName: firstMigrationString(machine.Name, machine.Hostname, machine.Alias)}, true
}

func (api *migrationAPI) handleInstances(w http.ResponseWriter, r *http.Request) {
	p, ok := api.principalFromRequest(w, r)
	if !ok {
		return
	}
	machines, err := api.machines.ListByUserID(store.WithTenant(r.Context(), p.TenantID), p.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}
	current := api.currentExport(r.Context(), p.TenantID, p.UserID)
	items := make([]map[string]any, 0, len(machines))
	for _, m := range machines {
		if m == nil || !sameTenantUser(m.TenantID, m.UserID, p.TenantID, p.UserID) {
			continue
		}
		item := map[string]any{
			"instance_id":    m.ID,
			"machine_id":     m.ID,
			"machine_name":   firstMigrationString(m.Name, m.Hostname, m.Alias),
			"instance_name":  firstMigrationString(m.Alias, m.Name),
			"os":             m.Platform,
			"maclaw_version": m.AppVersion,
			"status":         m.Status,
		}
		if m.LastSeenAt != nil {
			item["last_seen_at"] = m.LastSeenAt.Format(time.RFC3339)
		}
		if current != nil && current.SourceMachineID == m.ID && current.Status != "deleted" {
			item["has_export"] = true
			item["export_id"] = current.ID
			item["export_status"] = current.Status
			item["export_updated_at"] = current.UpdatedAt
			item["export_size"] = current.CompressedSize
			item["export_claimed_by_machine_id"] = current.ClaimedByMachineID
			item["export_manifest"] = json.RawMessage(firstMigrationString(current.ManifestJSON, "{}"))
		} else {
			item["has_export"] = false
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"instances": items, "max_compressed_bytes": api.maxCompressedBytes(r.Context(), p.TenantID)})
}

func (api *migrationAPI) handleCurrentExport(w http.ResponseWriter, r *http.Request) {
	p, ok := api.principalFromRequest(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"export": exportDTO(api.currentExport(r.Context(), p.TenantID, p.UserID)), "max_compressed_bytes": api.maxCompressedBytes(r.Context(), p.TenantID)})
}

func (api *migrationAPI) handleCreateExport(w http.ResponseWriter, r *http.Request) {
	p, ok := api.principalFromRequest(w, r)
	if !ok {
		return
	}
	var req struct {
		CompressedSize  int64           `json:"compressed_size"`
		EncryptedSize   int64           `json:"encrypted_size"`
		EncryptedSHA256 string          `json:"encrypted_sha256"`
		PlainSHA256     string          `json:"plain_sha256"`
		ChunkSize       int64           `json:"chunk_size"`
		ChunkCount      int             `json:"chunk_count"`
		Manifest        json.RawMessage `json:"manifest"`
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, migrationMaxCreateBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	if err := validateMigrationJSONStructure(body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	if req.CompressedSize <= 0 || req.EncryptedSize <= 0 || req.EncryptedSize < req.CompressedSize || req.ChunkSize <= 0 || req.ChunkCount <= 0 || !validSHA256(req.EncryptedSHA256) || !validSHA256(req.PlainSHA256) {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "size, hashes, chunk_size and chunk_count are required")
		return
	}
	expectedChunks := req.EncryptedSize / req.ChunkSize
	if req.EncryptedSize%req.ChunkSize != 0 {
		expectedChunks++
	}
	if req.ChunkSize < migrationMinUploadChunkSize || req.ChunkSize > migrationMaxUploadChunkSize || expectedChunks != int64(req.ChunkCount) || req.ChunkCount > migrationMaxUploadChunks {
		writeError(w, http.StatusBadRequest, "INVALID_CHUNKS", "chunk_size and chunk_count do not match encrypted_size")
		return
	}
	limit := api.maxCompressedBytes(r.Context(), p.TenantID)
	if req.CompressedSize > limit || req.EncryptedSize > req.CompressedSize+migrationMaxEncryptedOverhead(req.ChunkCount) {
		writeError(w, http.StatusRequestEntityTooLarge, "MIGRATION_TOO_LARGE", fmt.Sprintf("compressed migration package exceeds %d bytes or encrypted package overhead is invalid", limit))
		return
	}
	manifest := strings.TrimSpace(string(req.Manifest))
	if manifest == "" {
		manifest = "{}"
	}
	if err := validateMigrationPublicManifest(req.Manifest); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_MANIFEST", err.Error())
		return
	}
	if err := validateMigrationPublicManifestIdentity(req.Manifest, p); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_MANIFEST_IDENTITY", err.Error())
		return
	}
	now := time.Now().UTC()
	exportID := newMigrationID("mig")
	tx, err := api.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}
	defer tx.Rollback()
	nowText := now.Format(time.RFC3339)
	oldIDs, err := visibleMigrationIDsTx(r.Context(), tx, p.TenantID, p.UserID, nowText)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE user_data_migration_exports
		SET status = 'replaced', updated_at = ?
		WHERE tenant_id = ? AND user_id = ? AND (
			status IN ('uploading','ready','failed','aborted','imported','deleting')
			OR (status = 'importing' AND claim_expires_at IS NOT NULL AND claim_expires_at < ?)
		)`, nowText, p.TenantID, p.UserID, nowText); err != nil {
		writeError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}
	for _, oldID := range oldIDs {
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM user_data_migration_chunks WHERE export_id = ?`, oldID); err != nil {
			writeError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
			return
		}
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO user_data_migration_exports
		(id, tenant_id, user_id, source_machine_id, source_machine_name, status, compressed_size, encrypted_size, encrypted_sha256, plain_sha256, chunk_size, chunk_count, manifest_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'uploading', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		exportID, p.TenantID, p.UserID, p.MachineID, firstMigrationString(p.MachineName, p.MachineID), req.CompressedSize, req.EncryptedSize, strings.ToLower(req.EncryptedSHA256), strings.ToLower(req.PlainSHA256), req.ChunkSize, req.ChunkCount, manifest, nowText, nowText)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}
	for _, id := range oldIDs {
		_ = os.RemoveAll(api.exportDir(p.TenantID, p.UserID, id))
	}
	_ = os.MkdirAll(api.exportDir(p.TenantID, p.UserID, exportID), 0o700)
	writeJSON(w, http.StatusOK, map[string]any{"export_id": exportID, "status": "uploading", "max_compressed_bytes": limit})
}

func (api *migrationAPI) handleChunkStatus(w http.ResponseWriter, r *http.Request) {
	p, row, idx, ok := api.requireExportChunkAccess(w, r, false)
	if !ok {
		return
	}
	_ = p
	var size int64
	var sha string
	err := api.db.QueryRowContext(r.Context(), `SELECT size, sha256 FROM user_data_migration_chunks WHERE export_id = ? AND chunk_index = ?`, row.ID, idx).Scan(&size, &sha)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"uploaded": false})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STATUS_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"uploaded": true, "size": size, "sha256": sha})
}

func (api *migrationAPI) handlePutChunk(w http.ResponseWriter, r *http.Request) {
	p, row, idx, ok := api.requireExportChunkAccess(w, r, false)
	if !ok {
		return
	}
	if row.SourceMachineID != p.MachineID {
		writeError(w, http.StatusForbidden, "SOURCE_MACHINE_REQUIRED", "only the source machine can upload this export")
		return
	}
	if row.Status != "uploading" {
		writeError(w, http.StatusConflict, "INVALID_STATUS", "export is not uploading")
		return
	}
	expected := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Chunk-SHA256")))
	if expected == "" {
		expected = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sha256")))
	}
	if !validSHA256(expected) {
		writeError(w, http.StatusBadRequest, "INVALID_HASH", "chunk sha256 is required")
		return
	}
	if idx < 0 || idx >= row.ChunkCount {
		writeError(w, http.StatusBadRequest, "INVALID_CHUNK", "chunk index is out of range")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, row.ChunkSize+1024))
	if err != nil {
		writeError(w, http.StatusBadRequest, "READ_FAILED", err.Error())
		return
	}
	if int64(len(body)) > row.ChunkSize {
		writeError(w, http.StatusRequestEntityTooLarge, "CHUNK_TOO_LARGE", "chunk exceeds configured chunk_size")
		return
	}
	if want := migrationExpectedChunkSize(row, idx); want <= 0 || int64(len(body)) != want {
		writeError(w, http.StatusBadRequest, "CHUNK_SIZE_MISMATCH", "chunk size does not match export metadata")
		return
	}
	got := migrationSHA256Hex(body)
	if got != expected {
		writeError(w, http.StatusBadRequest, "HASH_MISMATCH", "chunk sha256 mismatch")
		return
	}
	if err := os.MkdirAll(api.chunksDir(row.TenantID, row.UserID, row.ID), 0o700); err != nil {
		writeError(w, http.StatusInternalServerError, "WRITE_FAILED", err.Error())
		return
	}
	if err := os.WriteFile(api.chunkPath(row.TenantID, row.UserID, row.ID, idx), body, 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, "WRITE_FAILED", err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = api.db.ExecContext(r.Context(), `INSERT INTO user_data_migration_chunks (export_id, tenant_id, user_id, chunk_index, size, sha256, uploaded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(export_id, chunk_index) DO UPDATE SET size = excluded.size, sha256 = excluded.sha256, uploaded_at = excluded.uploaded_at`,
		row.ID, p.TenantID, p.UserID, idx, len(body), got, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sha256": got, "size": len(body)})
}

func (api *migrationAPI) handleCompleteUpload(w http.ResponseWriter, r *http.Request) {
	p, row, _, ok := api.requireExportChunkAccess(w, r, true)
	if !ok {
		return
	}
	if row.SourceMachineID != p.MachineID {
		writeError(w, http.StatusForbidden, "SOURCE_MACHINE_REQUIRED", "only the source machine can complete this export")
		return
	}
	if row.Status != "uploading" {
		writeError(w, http.StatusConflict, "INVALID_STATUS", "export is not uploading")
		return
	}
	limit := api.maxCompressedBytes(r.Context(), row.TenantID)
	if row.CompressedSize > limit || row.EncryptedSize > row.CompressedSize+migrationMaxEncryptedOverhead(row.ChunkCount) {
		writeError(w, http.StatusRequestEntityTooLarge, "MIGRATION_TOO_LARGE", fmt.Sprintf("compressed migration package exceeds %d bytes or encrypted package overhead is invalid", limit))
		return
	}
	hash := sha256.New()
	var total int64
	for i := 0; i < row.ChunkCount; i++ {
		data, err := os.ReadFile(api.chunkPath(row.TenantID, row.UserID, row.ID, i))
		if err != nil {
			writeError(w, http.StatusConflict, "MISSING_CHUNK", fmt.Sprintf("missing chunk %d", i))
			return
		}
		hash.Write(data)
		total += int64(len(data))
	}
	if total != row.EncryptedSize {
		writeError(w, http.StatusBadRequest, "SIZE_MISMATCH", "encrypted package size mismatch")
		return
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if got != row.EncryptedSHA256 {
		_, _ = api.db.ExecContext(r.Context(), `UPDATE user_data_migration_exports SET status = 'failed', updated_at = ? WHERE id = ?`, time.Now().UTC().Format(time.RFC3339), row.ID)
		writeError(w, http.StatusBadRequest, "HASH_MISMATCH", "encrypted package sha256 mismatch")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := api.db.ExecContext(r.Context(), `UPDATE user_data_migration_exports SET status = 'ready', updated_at = ? WHERE id = ?`, now, row.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "COMPLETE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "ready"})
}

func (api *migrationAPI) handleClaimImport(w http.ResponseWriter, r *http.Request) {
	p, row, _, ok := api.requireExportChunkAccess(w, r, true)
	if !ok {
		return
	}
	if (row.Status == "imported" || row.Status == "deleting" || row.Status == "deleted") && row.ClaimedByMachineID == p.MachineID {
		writeJSON(w, http.StatusOK, map[string]any{"export": exportDTO(row)})
		return
	}
	leaseText, err := api.claimImportForMachine(r.Context(), p, row, time.Now().UTC())
	if errors.Is(err, errMigrationExportNotReady) {
		writeError(w, http.StatusConflict, "INVALID_STATUS", errMigrationExportNotReady.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "CLAIM_FAILED", err.Error())
		return
	}
	resp := map[string]any{"export": exportDTO(row)}
	if leaseText != "" {
		resp["lease_expires_at"] = leaseText
	}
	writeJSON(w, http.StatusOK, resp)
}

func (api *migrationAPI) claimImportForMachine(ctx context.Context, p *migrationPrincipal, row *migrationExportRow, now time.Time) (string, error) {
	nowText := now.Format(time.RFC3339)
	if row.Status == "importing" && row.ClaimedByMachineID == p.MachineID {
		lease := now.Add(migrationClaimLease)
		leaseText := lease.Format(time.RFC3339)
		result, err := api.db.ExecContext(ctx, `UPDATE user_data_migration_exports
			SET claim_expires_at = ?, updated_at = ?
			WHERE id = ? AND tenant_id = ? AND user_id = ? AND status = 'importing' AND claimed_by_machine_id = ?`,
			leaseText, nowText, row.ID, p.TenantID, p.UserID, p.MachineID)
		if err != nil {
			return "", err
		}
		if affected, err := result.RowsAffected(); err == nil && affected == 0 {
			return "", errMigrationExportNotReady
		}
		row.ClaimExpiresAt = sql.NullString{String: leaseText, Valid: true}
		row.UpdatedAt = nowText
		return leaseText, nil
	}
	if row.Status != "ready" && !(row.Status == "importing" && row.ClaimExpiresAt.Valid && parseMigrationTime(row.ClaimExpiresAt.String).Before(now)) {
		return "", errMigrationExportNotReady
	}
	lease := now.Add(migrationClaimLease)
	leaseText := lease.Format(time.RFC3339)
	result, err := api.db.ExecContext(ctx, `UPDATE user_data_migration_exports
		SET status = 'importing', claimed_by_machine_id = ?, claimed_at = ?, claim_expires_at = ?, updated_at = ?
		WHERE id = ? AND tenant_id = ? AND user_id = ? AND (
			status = 'ready'
			OR (status = 'importing' AND claim_expires_at IS NOT NULL AND claim_expires_at < ?)
		)`,
		p.MachineID, nowText, leaseText, nowText, row.ID, p.TenantID, p.UserID, nowText)
	if err != nil {
		return "", err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return "", errMigrationExportNotReady
	}
	row.Status = "importing"
	row.ClaimedByMachineID = p.MachineID
	row.ClaimedAt = sql.NullString{String: nowText, Valid: true}
	row.ClaimExpiresAt = sql.NullString{String: leaseText, Valid: true}
	row.UpdatedAt = nowText
	return leaseText, nil
}

func (api *migrationAPI) handleGetChunk(w http.ResponseWriter, r *http.Request) {
	p, row, idx, ok := api.requireExportChunkAccess(w, r, false)
	if !ok {
		return
	}
	if row.Status != "importing" || row.ClaimedByMachineID != p.MachineID {
		writeError(w, http.StatusConflict, "INVALID_STATUS", "export is not downloadable")
		return
	}
	http.ServeFile(w, r, api.chunkPath(row.TenantID, row.UserID, row.ID, idx))
}

func (api *migrationAPI) handleCompleteImport(w http.ResponseWriter, r *http.Request) {
	p, row, _, ok := api.requireExportChunkAccess(w, r, true)
	if !ok {
		return
	}
	if row.ClaimedByMachineID != p.MachineID {
		writeError(w, http.StatusConflict, "INVALID_STATUS", "export is not claimed by this machine")
		return
	}
	if row.Status == "deleted" {
		if _, err := api.db.ExecContext(r.Context(), `DELETE FROM user_data_migration_chunks WHERE export_id = ?`, row.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "deleted"})
		return
	}
	if row.Status != "importing" && row.Status != "imported" && row.Status != "deleting" {
		writeError(w, http.StatusConflict, "INVALID_STATUS", "export is not claimed by this machine")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if row.Status == "importing" {
		if _, err := api.db.ExecContext(r.Context(), `UPDATE user_data_migration_exports SET status = 'imported', imported_at = ?, updated_at = ? WHERE id = ?`, now, now, row.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "COMPLETE_FAILED", err.Error())
			return
		}
	}
	if _, err := api.db.ExecContext(r.Context(), `UPDATE user_data_migration_exports SET status = 'deleting', updated_at = ? WHERE id = ?`, now, row.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "COMPLETE_FAILED", err.Error())
		return
	}
	deleteErr := os.RemoveAll(api.exportDir(row.TenantID, row.UserID, row.ID))
	if deleteErr != nil {
		writeError(w, http.StatusInternalServerError, "DELETE_FAILED", deleteErr.Error())
		return
	}
	if _, err := api.db.ExecContext(r.Context(), `DELETE FROM user_data_migration_chunks WHERE export_id = ?`, row.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
		return
	}
	if _, err := api.db.ExecContext(r.Context(), `UPDATE user_data_migration_exports SET status = 'deleted', deleted_at = ?, updated_at = ? WHERE id = ?`, now, now, row.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "COMPLETE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "deleted"})
}

func (api *migrationAPI) handleAbortImport(w http.ResponseWriter, r *http.Request) {
	p, row, _, ok := api.requireExportChunkAccess(w, r, true)
	if !ok {
		return
	}
	if row.Status != "importing" || row.ClaimedByMachineID != p.MachineID {
		writeError(w, http.StatusConflict, "INVALID_STATUS", "export is not claimed by this machine")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := api.db.ExecContext(r.Context(), `UPDATE user_data_migration_exports SET status = 'ready', claimed_by_machine_id = '', claimed_at = NULL, claim_expires_at = NULL, updated_at = ? WHERE id = ?`, now, row.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ABORT_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "ready"})
}

func (api *migrationAPI) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	tenantID := RequestTenantID(r)
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"max_compressed_bytes": api.maxCompressedBytes(r.Context(), tenantID), "min_bytes": migrationMinCompressedBytes, "max_bytes": migrationMaxCompressedBytes})
	case http.MethodPut:
		var req struct {
			MaxCompressedBytes int64 `json:"max_compressed_bytes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		value := clampMigrationLimit(req.MaxCompressedBytes)
		data, _ := json.Marshal(map[string]int64{"value": value})
		if err := scopedSystemSettingsForTenant(tenantID, api.system).Set(r.Context(), migrationSettingMaxCompressedBytes, string(data)); err != nil {
			writeError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"max_compressed_bytes": value, "min_bytes": migrationMinCompressedBytes, "max_bytes": migrationMaxCompressedBytes})
	default:
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or PUT")
	}
}

func (api *migrationAPI) requireExportChunkAccess(w http.ResponseWriter, r *http.Request, wholeExport bool) (*migrationPrincipal, *migrationExportRow, int, bool) {
	p, ok := api.principalFromRequest(w, r)
	if !ok {
		return nil, nil, 0, false
	}
	exportID := strings.TrimSpace(r.PathValue("exportID"))
	row, err := api.getExport(r.Context(), p.TenantID, p.UserID, exportID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LOOKUP_FAILED", err.Error())
		return nil, nil, 0, false
	}
	if row == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "migration export not found")
		return nil, nil, 0, false
	}
	idx := 0
	if !wholeExport {
		parsed, err := strconv.Atoi(strings.TrimSpace(r.PathValue("index")))
		if err != nil || parsed < 0 || parsed >= row.ChunkCount {
			writeError(w, http.StatusBadRequest, "INVALID_CHUNK", "chunk index is out of range")
			return nil, nil, 0, false
		}
		idx = parsed
	}
	return p, row, idx, true
}

func (api *migrationAPI) getExport(ctx context.Context, tenantID, userID, exportID string) (*migrationExportRow, error) {
	row := api.db.QueryRowContext(ctx, `SELECT id, tenant_id, user_id, source_machine_id, source_machine_name, status, compressed_size, encrypted_size, encrypted_sha256, plain_sha256, chunk_size, chunk_count, manifest_json, claimed_by_machine_id, claimed_at, claim_expires_at, created_at, updated_at, imported_at, deleted_at
		FROM user_data_migration_exports WHERE id = ? AND tenant_id = ? AND user_id = ?`, exportID, tenantID, userID)
	return scanMigrationExport(row)
}

func (api *migrationAPI) currentExport(ctx context.Context, tenantID, userID string) *migrationExportRow {
	row := api.db.QueryRowContext(ctx, `SELECT id, tenant_id, user_id, source_machine_id, source_machine_name, status, compressed_size, encrypted_size, encrypted_sha256, plain_sha256, chunk_size, chunk_count, manifest_json, claimed_by_machine_id, claimed_at, claim_expires_at, created_at, updated_at, imported_at, deleted_at
		FROM user_data_migration_exports WHERE tenant_id = ? AND user_id = ? AND status IN ('uploading','ready','importing','imported','failed','deleting') ORDER BY updated_at DESC LIMIT 1`, tenantID, userID)
	out, _ := scanMigrationExport(row)
	return out
}

func scanMigrationExport(row *sql.Row) (*migrationExportRow, error) {
	var e migrationExportRow
	err := row.Scan(&e.ID, &e.TenantID, &e.UserID, &e.SourceMachineID, &e.SourceMachineName, &e.Status, &e.CompressedSize, &e.EncryptedSize, &e.EncryptedSHA256, &e.PlainSHA256, &e.ChunkSize, &e.ChunkCount, &e.ManifestJSON, &e.ClaimedByMachineID, &e.ClaimedAt, &e.ClaimExpiresAt, &e.CreatedAt, &e.UpdatedAt, &e.ImportedAt, &e.DeletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func visibleMigrationIDsTx(ctx context.Context, tx *sql.Tx, tenantID, userID, nowText string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM user_data_migration_exports
		WHERE tenant_id = ? AND user_id = ? AND (
			status IN ('uploading','ready','failed','aborted','imported','deleting')
			OR (status = 'importing' AND claim_expires_at IS NOT NULL AND claim_expires_at < ?)
		)`, tenantID, userID, nowText)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func exportDTO(e *migrationExportRow) map[string]any {
	if e == nil {
		return nil
	}
	return map[string]any{
		"export_id":             e.ID,
		"source_instance_id":    e.SourceMachineID,
		"source_machine_id":     e.SourceMachineID,
		"source_machine_name":   e.SourceMachineName,
		"claimed_by_machine_id": e.ClaimedByMachineID,
		"status":                e.Status,
		"compressed_size":       e.CompressedSize,
		"encrypted_size":        e.EncryptedSize,
		"encrypted_sha256":      e.EncryptedSHA256,
		"plain_sha256":          e.PlainSHA256,
		"chunk_size":            e.ChunkSize,
		"chunk_count":           e.ChunkCount,
		"manifest":              json.RawMessage(firstMigrationString(e.ManifestJSON, "{}")),
		"created_at":            e.CreatedAt,
		"updated_at":            e.UpdatedAt,
	}
}

func (api *migrationAPI) maxCompressedBytes(ctx context.Context, tenantID string) int64 {
	if api == nil || api.system == nil {
		return migrationDefaultMaxCompressedBytes
	}
	raw, err := scopedSystemSettingsForTenant(tenantID, api.system).Get(ctx, migrationSettingMaxCompressedBytes)
	if err != nil || strings.TrimSpace(raw) == "" {
		return migrationDefaultMaxCompressedBytes
	}
	var payload struct {
		Value int64 `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return migrationDefaultMaxCompressedBytes
	}
	return clampMigrationLimit(payload.Value)
}

func clampMigrationLimit(v int64) int64 {
	if v < migrationMinCompressedBytes {
		return migrationMinCompressedBytes
	}
	if v > migrationMaxCompressedBytes {
		return migrationMaxCompressedBytes
	}
	return v
}

func migrationMaxEncryptedOverhead(chunkCount int) int64 {
	if chunkCount < 1 {
		chunkCount = 1
	}
	if chunkCount > migrationMaxUploadChunks {
		chunkCount = migrationMaxUploadChunks
	}
	return int64(1<<20) + int64(chunkCount)*64
}

func (api *migrationAPI) machineName(ctx context.Context, tenantID, userID, machineID string) string {
	if api == nil || api.machines == nil || strings.TrimSpace(machineID) == "" {
		return ""
	}
	m, err := api.machines.GetByID(ctx, machineID)
	if err != nil || m == nil || !sameTenantUser(m.TenantID, m.UserID, tenantID, userID) {
		return ""
	}
	return firstMigrationString(m.Name, m.Hostname, m.Alias)
}

func (api *migrationAPI) exportDir(tenantID, userID, exportID string) string {
	return filepath.Join(api.rootDir, safeMigrationSegment(tenantID), safeMigrationSegment(userID), safeMigrationSegment(exportID))
}

func (api *migrationAPI) chunksDir(tenantID, userID, exportID string) string {
	return filepath.Join(api.exportDir(tenantID, userID, exportID), "chunks")
}

func (api *migrationAPI) chunkPath(tenantID, userID, exportID string, idx int) string {
	return filepath.Join(api.chunksDir(tenantID, userID, exportID), fmt.Sprintf("%06d.part", idx))
}

func sameTenantUser(aTenant, aUser, bTenant, bUser string) bool {
	return store.NormalizeTenantID(aTenant) == store.NormalizeTenantID(bTenant) && strings.TrimSpace(aUser) == strings.TrimSpace(bUser)
}

func validSHA256(v string) bool {
	v = strings.TrimSpace(v)
	if len(v) != 64 {
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil
}

func validateMigrationPublicManifest(raw json.RawMessage) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := validateMigrationJSONStructure(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var manifest migrationPublicManifest
	if err := decoder.Decode(&manifest); err != nil {
		return fmt.Errorf("manifest must match the public migration schema")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("manifest must contain one JSON object")
	}
	if manifest.MemoryEntries < 0 || manifest.KnowledgeBytes < 0 || manifest.AssetBytes < 0 || manifest.PetPackBytes < 0 || manifest.ConfigSections < 0 || manifest.SecretCount < 0 {
		return fmt.Errorf("manifest contains invalid counts")
	}
	if !manifest.PetPacksIncluded && manifest.PetPackBytes != 0 {
		return fmt.Errorf("manifest pet-pack bytes require pet_packs_included")
	}
	seen := make(map[string]struct{}, len(manifest.Files))
	var petPackBytes int64
	petPackFileCount := 0
	for _, file := range manifest.Files {
		clean, key, err := canonicalMigrationManifestPath(file.Path)
		if err != nil || file.Bytes < 0 || !validSHA256(file.SHA256) {
			return fmt.Errorf("manifest contains invalid file metadata")
		}
		if key == "manifest.json" {
			return fmt.Errorf("manifest must not list itself")
		}
		if key == "pet_packs" {
			return fmt.Errorf("manifest pet-pack root must be a directory")
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("manifest contains duplicate file path: %s", clean)
		}
		seen[key] = struct{}{}
		if strings.HasPrefix(key, "pet_packs/") {
			if file.Bytes > int64(^uint64(0)>>1)-petPackBytes {
				return fmt.Errorf("manifest pet-pack byte count overflow")
			}
			petPackBytes += file.Bytes
			petPackFileCount++
		}
	}
	if petPackBytes != manifest.PetPackBytes {
		return fmt.Errorf("manifest pet-pack byte count mismatch")
	}
	if !manifest.PetPacksIncluded && petPackFileCount != 0 {
		return fmt.Errorf("manifest contains pet-pack files without declaring them")
	}
	return nil
}

func validateMigrationPublicManifestIdentity(raw json.RawMessage, principal *migrationPrincipal) error {
	if len(bytes.TrimSpace(raw)) == 0 || principal == nil {
		return nil
	}
	var manifest migrationPublicManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return fmt.Errorf("manifest identity is invalid")
	}
	checks := []struct {
		name string
		got  string
		want string
	}{
		{name: "tenant_id", got: strings.TrimSpace(manifest.TenantID), want: store.NormalizeTenantID(principal.TenantID)},
		{name: "user_id", got: strings.TrimSpace(manifest.UserID), want: strings.TrimSpace(principal.UserID)},
		{name: "machine_id", got: strings.TrimSpace(manifest.MachineID), want: strings.TrimSpace(principal.MachineID)},
	}
	for _, check := range checks {
		// Older packages may omit identity metadata. When present, it must be
		// bound to the authenticated uploader rather than trusted client input.
		got := check.got
		if check.name == "tenant_id" && got != "" {
			got = store.NormalizeTenantID(got)
		}
		if got != "" && got != check.want {
			return fmt.Errorf("manifest %s does not match authenticated source", check.name)
		}
	}
	return nil
}

func validateMigrationJSONStructure(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := walkMigrationJSONValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("manifest must contain one JSON object")
	}
	return nil
}

func walkMigrationJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 128 {
		return fmt.Errorf("manifest JSON nesting exceeds 128 levels")
	}
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("manifest must contain valid JSON")
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("manifest must contain valid JSON")
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("manifest must contain valid JSON")
			}
			identity := strings.ToLower(key)
			if _, exists := seen[identity]; exists {
				return fmt.Errorf("manifest contains duplicate field %q", key)
			}
			seen[identity] = struct{}{}
			if err := walkMigrationJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := walkMigrationJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("manifest must contain valid JSON")
	}
	closing, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("manifest must contain valid JSON")
	}
	want := json.Delim('}')
	if delim == '[' {
		want = ']'
	}
	if closing != want {
		return fmt.Errorf("manifest must contain valid JSON")
	}
	return nil
}

func canonicalMigrationManifestPath(name string) (string, string, error) {
	if name == "" || strings.TrimSpace(name) != name || strings.Contains(name, "\\") || strings.ContainsRune(name, 0) || strings.HasPrefix(name, "/") {
		return "", "", fmt.Errorf("path must be canonical and relative")
	}
	clean := pathpkg.Clean(name)
	if clean == "." || clean != name || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", "", fmt.Errorf("path must be canonical and relative")
	}
	if !portableMigrationManifestPathSegments(clean) {
		return "", "", fmt.Errorf("path contains a segment unsupported on Windows")
	}
	return clean, strings.ToLower(clean), nil
}

func portableMigrationManifestPathSegments(name string) bool {
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || strings.Contains(segment, ":") || strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") {
			return false
		}
		for _, r := range segment {
			if r < 0x20 {
				return false
			}
		}
		base := strings.ToUpper(strings.SplitN(segment, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" ||
			(len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9') {
			return false
		}
	}
	return true
}

func migrationExpectedChunkSize(row *migrationExportRow, idx int) int64 {
	if row == nil || idx < 0 || idx >= row.ChunkCount || row.ChunkSize <= 0 || row.EncryptedSize <= 0 {
		return 0
	}
	offset := int64(idx) * row.ChunkSize
	remaining := row.EncryptedSize - offset
	if remaining <= 0 {
		return 0
	}
	if remaining < row.ChunkSize {
		return remaining
	}
	return row.ChunkSize
}

func migrationSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func newMigrationID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func safeMigrationSegment(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "_"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "..", "_")
	return replacer.Replace(v)
}

func firstMigrationString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func parseMigrationTime(raw string) time.Time {
	t, _ := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	return t
}
