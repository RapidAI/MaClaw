package delivery

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

// Bundle represents a configuration package for delivery.
type Bundle struct {
	ID          string
	Version     int
	ContentType string // full, incremental
	Payload     string // JSON payload
	Status      string // draft, published
	Note        string
	CreatedAt   time.Time
	PublishedAt time.Time
}

// ApplyRecord records an iWorker acknowledgement for a delivered bundle.
type ApplyRecord struct {
	TenantID     string
	BundleID     string
	Version      int
	WorkerID     string
	DepartmentID string
	Status       string
	Message      string
	AppliedAt    time.Time
}

// ErrBundleNotPublished is returned when an iWorker reports a draft bundle.
var ErrBundleNotPublished = errors.New("config bundle is not published")

// Repo provides persistence for config bundles.
type Repo struct {
	write *sql.DB
	read  *sql.DB
}

// NewRepo creates a Repo.
func NewRepo(write, read *sql.DB) *Repo {
	return &Repo{write: write, read: read}
}

// Insert creates a new config bundle.
func (r *Repo) Insert(b *Bundle, tenantID string) error {
	pubAt := ""
	if !b.PublishedAt.IsZero() {
		pubAt = b.PublishedAt.Format(time.RFC3339)
	}
	_, err := r.write.Exec(`INSERT INTO config_bundles (id, tenant_id, version, content_type, payload, status, note, created_at, published_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		b.ID, tenantID, b.Version, b.ContentType, b.Payload, b.Status, b.Note,
		b.CreatedAt.Format(time.RFC3339), pubAt)
	return err
}

// Publish marks a bundle as published.
func (r *Repo) Publish(id string, tenantID string) error {
	now := time.Now().Format(time.RFC3339)
	res, err := r.write.Exec(`UPDATE config_bundles SET status='published', published_at=? WHERE id=? AND tenant_id=?`, now, id, tenantID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("bundle %s not found", id)
	}
	return nil
}

// GetLatestPublished returns the most recently published bundle.
func (r *Repo) GetLatestPublished(tenantID string) (*Bundle, error) {
	row := r.read.QueryRow(`SELECT id, version, content_type, payload, status, note, created_at, published_at
		FROM config_bundles WHERE status='published' AND tenant_id=? ORDER BY version DESC LIMIT 1`, tenantID)
	return scanBundle(row)
}

// GetByID returns one bundle scoped to a tenant.
func (r *Repo) GetByID(tenantID, id string) (*Bundle, error) {
	row := r.read.QueryRow(`SELECT id, version, content_type, payload, status, note, created_at, published_at
		FROM config_bundles WHERE tenant_id=? AND id=? LIMIT 1`, tenantID, id)
	return scanBundle(row)
}

// GetLatestVersion returns the highest version number.
func (r *Repo) GetLatestVersion(tenantID string) (int, error) {
	var v int
	err := r.read.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM config_bundles WHERE tenant_id=?`, tenantID).Scan(&v)
	return v, err
}

// List returns all bundles ordered by version desc.
func (r *Repo) List(tenantID string) ([]*Bundle, error) {
	rows, err := r.read.Query(`SELECT id, version, content_type, payload, status, note, created_at, published_at
		FROM config_bundles WHERE tenant_id=? ORDER BY version DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Bundle
	for rows.Next() {
		b, err := scanBundleRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

// RecordApply upserts a client-side apply acknowledgement.
func (r *Repo) RecordApply(record ApplyRecord) error {
	bundle, err := r.GetByID(record.TenantID, record.BundleID)
	if err != nil {
		return fmt.Errorf("config apply bundle %s not found for tenant %s: %w", record.BundleID, record.TenantID, err)
	}
	if bundle.Status != "published" {
		return fmt.Errorf("config apply bundle %s is %s: %w", record.BundleID, bundle.Status, ErrBundleNotPublished)
	}
	record.Version = bundle.Version
	now := record.AppliedAt
	if now.IsZero() {
		now = time.Now()
	}
	_, err = r.write.Exec(`INSERT INTO config_apply_records (tenant_id, bundle_id, version, worker_id, department_id, status, message, applied_at)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(tenant_id, bundle_id, worker_id) DO UPDATE SET
			version=excluded.version, department_id=excluded.department_id, status=excluded.status, message=excluded.message, applied_at=excluded.applied_at`,
		record.TenantID, record.BundleID, record.Version, record.WorkerID, record.DepartmentID, normalizeApplyStatus(record.Status), strings.TrimSpace(record.Message), now.Format(time.RFC3339))
	return err
}

// ListApplyRecords returns recent acknowledgements for admin visibility.
func (r *Repo) ListApplyRecords(tenantID string, limit int) ([]ApplyRecord, error) {
	if limit <= 0 {
		limit = 500
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := r.read.Query(`SELECT tenant_id, bundle_id, version, worker_id, department_id, status, message, applied_at
		FROM config_apply_records WHERE tenant_id=? ORDER BY applied_at DESC LIMIT ?`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ApplyRecord
	for rows.Next() {
		var rec ApplyRecord
		var appliedAt string
		if err := rows.Scan(&rec.TenantID, &rec.BundleID, &rec.Version, &rec.WorkerID, &rec.DepartmentID, &rec.Status, &rec.Message, &appliedAt); err != nil {
			return nil, err
		}
		rec.AppliedAt, _ = time.Parse(time.RFC3339, appliedAt)
		out = append(out, rec)
	}
	return out, rows.Err()
}

func scanBundle(row *sql.Row) (*Bundle, error) {
	var b Bundle
	var ca, pa string
	if err := row.Scan(&b.ID, &b.Version, &b.ContentType, &b.Payload, &b.Status, &b.Note, &ca, &pa); err != nil {
		return nil, err
	}
	b.CreatedAt, _ = time.Parse(time.RFC3339, ca)
	b.PublishedAt, _ = time.Parse(time.RFC3339, pa)
	return &b, nil
}

func scanBundleRows(rows *sql.Rows) (*Bundle, error) {
	var b Bundle
	var ca, pa string
	if err := rows.Scan(&b.ID, &b.Version, &b.ContentType, &b.Payload, &b.Status, &b.Note, &ca, &pa); err != nil {
		return nil, err
	}
	b.CreatedAt, _ = time.Parse(time.RFC3339, ca)
	b.PublishedAt, _ = time.Parse(time.RFC3339, pa)
	return &b, nil
}

// --- Handler ---

// Handler exposes HTTP endpoints for config delivery.
type Handler struct {
	repo *Repo
}

// NewHandler creates a Handler.
func NewHandler(write, read *sql.DB) *Handler {
	return &Handler{repo: NewRepo(write, read)}
}

// RegisterAdminRoutes registers admin-facing routes.
func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/config-bundles", h.handleBundles)
	mux.HandleFunc("/admin/config-bundles/", h.handleBundleByID)
}

// RegisterClientRoutes registers client-facing routes for DiWorker.
func (h *Handler) RegisterClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/client/config/version", h.handleClientVersion)
	mux.HandleFunc("/client/config/latest", h.handleClientLatest)
	mux.HandleFunc("/client/config/apply-result", h.handleClientApplyResult)
}

func (h *Handler) handleBundles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tenantID := tenant.RequestTenantID(r)
		bundles, err := h.repo.List(tenantID)
		if err != nil {
			response.Internal(w, err.Error())
			return
		}
		records, err := h.repo.ListApplyRecords(tenantID, 1000)
		if err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]any{"bundles": toBundleDTOs(bundles), "apply_records": toApplyRecordDTOs(records)})
	case http.MethodPost:
		var req struct {
			ContentType string `json:"content_type"`
			Payload     any    `json:"payload"`
			Note        string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.BadRequest(w, "INVALID_BODY", "invalid JSON")
			return
		}
		tenantID := tenant.RequestTenantID(r)
		latestVer, _ := h.repo.GetLatestVersion(tenantID)
		payloadBytes, _ := json.Marshal(req.Payload)
		now := time.Now()
		bundle := &Bundle{
			ID:          idgen.New("cfgb"),
			Version:     latestVer + 1,
			ContentType: defaultStr(req.ContentType, "full"),
			Payload:     string(payloadBytes),
			Status:      "draft",
			Note:        strings.TrimSpace(req.Note),
			CreatedAt:   now,
		}
		if err := h.repo.Insert(bundle, tenantID); err != nil {
			response.BadRequest(w, "CREATE_FAILED", err.Error())
			return
		}
		response.Created(w, toBundleDTO(bundle))
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *Handler) handleBundleByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/admin/config-bundles/")
	rest = strings.TrimRight(rest, "/")
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]

	if len(parts) == 2 && parts[1] == "publish" && r.Method == http.MethodPost {
		tenantID := tenant.RequestTenantID(r)
		if err := h.repo.Publish(id, tenantID); err != nil {
			response.BadRequest(w, "PUBLISH_FAILED", err.Error())
			return
		}
		response.OK(w, map[string]string{"status": "published"})
		return
	}

	response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST .../publish")
}

func (h *Handler) handleClientVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	bundle, err := h.repo.GetLatestPublished(tenant.RequestTenantID(r))
	if err != nil {
		response.OK(w, map[string]any{"version": 0, "available": false})
		return
	}
	response.OK(w, map[string]any{
		"version":   bundle.Version,
		"available": true,
		"bundle_id": bundle.ID,
	})
}

func (h *Handler) handleClientLatest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	bundle, err := h.repo.GetLatestPublished(tenant.RequestTenantID(r))
	if err != nil {
		response.NotFound(w, "NOT_FOUND", "no published config bundle")
		return
	}
	response.OK(w, toBundleDTO(bundle))
}

func (h *Handler) handleClientApplyResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	var req struct {
		BundleID     string `json:"bundle_id"`
		Version      int    `json:"version"`
		WorkerID     string `json:"worker_id"`
		DepartmentID string `json:"department_id"`
		Status       string `json:"status"`
		Message      string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	if strings.TrimSpace(req.BundleID) == "" {
		response.BadRequest(w, "MISSING_BUNDLE", "bundle_id is required")
		return
	}
	if strings.TrimSpace(req.WorkerID) == "" {
		response.BadRequest(w, "MISSING_WORKER", "worker_id is required")
		return
	}
	record := ApplyRecord{
		TenantID:     tenant.RequestTenantID(r),
		BundleID:     strings.TrimSpace(req.BundleID),
		Version:      req.Version,
		WorkerID:     strings.TrimSpace(req.WorkerID),
		DepartmentID: strings.TrimSpace(req.DepartmentID),
		Status:       normalizeApplyStatus(req.Status),
		Message:      strings.TrimSpace(req.Message),
	}
	if err := h.repo.RecordApply(record); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.BadRequest(w, "UNKNOWN_BUNDLE", "config bundle is not found for this tenant")
			return
		}
		if errors.Is(err, ErrBundleNotPublished) {
			response.BadRequest(w, "BUNDLE_NOT_PUBLISHED", "config bundle is not published")
			return
		}
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]string{"status": "recorded"})
}

// --- DTOs ---

type bundleDTO struct {
	ID          string `json:"id"`
	Version     int    `json:"version"`
	ContentType string `json:"content_type"`
	Payload     string `json:"payload"`
	Status      string `json:"status"`
	Note        string `json:"note"`
	CreatedAt   string `json:"created_at"`
	PublishedAt string `json:"published_at"`
}

type applyRecordDTO struct {
	BundleID     string `json:"bundle_id"`
	Version      int    `json:"version"`
	WorkerID     string `json:"worker_id"`
	DepartmentID string `json:"department_id"`
	Status       string `json:"status"`
	Message      string `json:"message"`
	AppliedAt    string `json:"applied_at"`
}

func toBundleDTO(b *Bundle) bundleDTO {
	pubAt := ""
	if !b.PublishedAt.IsZero() {
		pubAt = b.PublishedAt.Format("2006-01-02T15:04:05Z")
	}
	return bundleDTO{
		ID: b.ID, Version: b.Version, ContentType: b.ContentType,
		Payload: b.Payload, Status: b.Status, Note: b.Note,
		CreatedAt:   b.CreatedAt.Format("2006-01-02T15:04:05Z"),
		PublishedAt: pubAt,
	}
}

func toBundleDTOs(bundles []*Bundle) []bundleDTO {
	out := make([]bundleDTO, 0, len(bundles))
	for _, b := range bundles {
		out = append(out, toBundleDTO(b))
	}
	return out
}

func toApplyRecordDTOs(records []ApplyRecord) []applyRecordDTO {
	out := make([]applyRecordDTO, 0, len(records))
	for _, rec := range records {
		appliedAt := ""
		if !rec.AppliedAt.IsZero() {
			appliedAt = rec.AppliedAt.Format("2006-01-02T15:04:05Z")
		}
		out = append(out, applyRecordDTO{BundleID: rec.BundleID, Version: rec.Version, WorkerID: rec.WorkerID, DepartmentID: rec.DepartmentID, Status: rec.Status, Message: rec.Message, AppliedAt: appliedAt})
	}
	return out
}

func normalizeApplyStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "success", "applied", "ok":
		return "success"
	case "failed", "failure", "error":
		return "failed"
	case "skipped", "skip":
		return "skipped"
	default:
		return "failed"
	}
}

func defaultStr(val, def string) string {
	if strings.TrimSpace(val) == "" {
		return def
	}
	return strings.TrimSpace(val)
}
