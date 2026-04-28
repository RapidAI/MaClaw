package delivery

import (
	"database/sql"
	"encoding/json"
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
		response.OK(w, map[string]any{"bundles": toBundleDTOs(bundles)})
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

func defaultStr(val, def string) string {
	if strings.TrimSpace(val) == "" {
		return def
	}
	return strings.TrimSpace(val)
}
