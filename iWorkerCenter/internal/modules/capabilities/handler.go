package capabilities

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

// CapabilityPackage represents a skill / "会做的事".
type CapabilityPackage struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Version     string `json:"version"`
	Source      string `json:"source"`
	RiskLevel   string `json:"risk_level"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// Binding represents a colleague-capability binding.
type Binding struct {
	ID           string `json:"id"`
	ColleagueID  string `json:"colleague_id"`
	CapabilityID string `json:"capability_id"`
	BoundAt      string `json:"bound_at"`
}

// Handler provides HTTP endpoints for capability management.
type Handler struct {
	write *sql.DB
	read  *sql.DB
}

// NewHandler creates a capabilities Handler.
func NewHandler(write, read *sql.DB) *Handler {
	return &Handler{write: write, read: read}
}

// RegisterAdminRoutes registers admin-facing routes.
func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/capabilities", h.handleAdminCapabilities)
	mux.HandleFunc("/admin/capabilities/", h.handleAdminCapabilityByID)
}

// RegisterClientRoutes registers client-facing routes (for DiWorker).
func (h *Handler) RegisterClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/client/capabilities", h.handleClientCapabilities)
}

func (h *Handler) handleAdminCapabilities(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listCapabilities(w, r)
	case http.MethodPost:
		h.createCapability(w, r)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *Handler) handleAdminCapabilityByID(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	id := extractID(path, "/admin/capabilities/")
	if id == "" {
		response.BadRequest(w, "MISSING_ID", "capability id is required")
		return
	}

	// /admin/capabilities/{id}/bind
	if strings.HasSuffix(path, "/bind") {
		id = strings.TrimSuffix(id, "/bind")
		if r.Method == http.MethodPost {
			h.bindToColleague(w, r, id)
			return
		}
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}

	// /admin/capabilities/{id}/unbind
	if strings.HasSuffix(path, "/unbind") {
		id = strings.TrimSuffix(id, "/unbind")
		if r.Method == http.MethodPost {
			h.unbindFromColleague(w, r, id)
			return
		}
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getCapability(w, id)
	case http.MethodPut:
		h.updateCapability(w, r, id)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or PUT")
	}
}

func (h *Handler) handleClientCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	colleagueID := r.URL.Query().Get("colleague_id")
	if colleagueID != "" {
		h.listColleagueCapabilities(w, colleagueID)
		return
	}
	h.listActiveCapabilities(w)
}

func (h *Handler) listCapabilities(w http.ResponseWriter, _ *http.Request) {
	rows, err := h.read.Query("SELECT id, name, description, category, version, source, risk_level, status, created_at, updated_at FROM capability_packages ORDER BY name")
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	defer rows.Close()
	caps := scanCapabilities(rows)
	response.OK(w, map[string]any{"capabilities": caps})
}

func (h *Handler) listActiveCapabilities(w http.ResponseWriter) {
	rows, err := h.read.Query("SELECT id, name, description, category, version, source, risk_level, status, created_at, updated_at FROM capability_packages WHERE status='active' ORDER BY name")
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	defer rows.Close()
	caps := scanCapabilities(rows)
	response.OK(w, map[string]any{"capabilities": caps})
}

func (h *Handler) listColleagueCapabilities(w http.ResponseWriter, colleagueID string) {
	rows, err := h.read.Query(`SELECT cp.id, cp.name, cp.description, cp.category, cp.version, cp.source, cp.risk_level, cp.status, cp.created_at, cp.updated_at
		FROM capability_packages cp
		JOIN colleague_capability_bindings ccb ON cp.id = ccb.capability_id
		WHERE ccb.colleague_id = ? AND cp.status = 'active'
		ORDER BY cp.name`, colleagueID)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	defer rows.Close()
	caps := scanCapabilities(rows)
	response.OK(w, map[string]any{"capabilities": caps})
}

func (h *Handler) createCapability(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Category    string `json:"category"`
		Version     string `json:"version"`
		Source      string `json:"source"`
		RiskLevel   string `json:"risk_level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		response.BadRequest(w, "MISSING_NAME", "name is required")
		return
	}
	category := strings.TrimSpace(req.Category)
	if category == "" {
		category = "general"
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		version = "1.0.0"
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "local"
	}
	riskLevel := strings.TrimSpace(req.RiskLevel)
	if riskLevel == "" {
		riskLevel = "low"
	}
	now := time.Now().Format(time.RFC3339)
	id := idgen.New("cap")

	_, err := h.write.Exec(`INSERT INTO capability_packages (id, name, description, category, version, source, risk_level, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)`,
		id, name, strings.TrimSpace(req.Description), category, version, source, riskLevel, now, now)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.Created(w, CapabilityPackage{
		ID: id, Name: name, Description: strings.TrimSpace(req.Description),
		Category: category, Version: version, Source: source, RiskLevel: riskLevel,
		Status: "active", CreatedAt: now, UpdatedAt: now,
	})
}

func (h *Handler) getCapability(w http.ResponseWriter, id string) {
	row := h.read.QueryRow("SELECT id, name, description, category, version, source, risk_level, status, created_at, updated_at FROM capability_packages WHERE id=?", id)
	var cp CapabilityPackage
	if err := row.Scan(&cp.ID, &cp.Name, &cp.Description, &cp.Category, &cp.Version, &cp.Source, &cp.RiskLevel, &cp.Status, &cp.CreatedAt, &cp.UpdatedAt); err != nil {
		response.NotFound(w, "NOT_FOUND", "capability not found")
		return
	}
	response.OK(w, cp)
}

func (h *Handler) updateCapability(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Category    string `json:"category"`
		Version     string `json:"version"`
		RiskLevel   string `json:"risk_level"`
		Status      string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
		return
	}
	now := time.Now().Format(time.RFC3339)
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "active"
	}
	res, err := h.write.Exec(`UPDATE capability_packages SET name=?, description=?, category=?, version=?, risk_level=?, status=?, updated_at=? WHERE id=?`,
		strings.TrimSpace(req.Name), strings.TrimSpace(req.Description),
		strings.TrimSpace(req.Category), strings.TrimSpace(req.Version),
		strings.TrimSpace(req.RiskLevel), status, now, id)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		response.NotFound(w, "NOT_FOUND", "capability not found")
		return
	}
	response.OK(w, map[string]string{"status": "ok"})
}

func (h *Handler) bindToColleague(w http.ResponseWriter, r *http.Request, capabilityID string) {
	var req struct {
		ColleagueID string `json:"colleague_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.ColleagueID) == "" {
		response.BadRequest(w, "MISSING_COLLEAGUE_ID", "colleague_id is required")
		return
	}
	id := idgen.New("bind")
	now := time.Now().Format(time.RFC3339)
	_, err := h.write.Exec(`INSERT OR IGNORE INTO colleague_capability_bindings (id, colleague_id, capability_id, bound_at) VALUES (?, ?, ?, ?)`,
		id, strings.TrimSpace(req.ColleagueID), capabilityID, now)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]string{"status": "ok"})
}

func (h *Handler) unbindFromColleague(w http.ResponseWriter, r *http.Request, capabilityID string) {
	var req struct {
		ColleagueID string `json:"colleague_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
		return
	}
	_, _ = h.write.Exec("DELETE FROM colleague_capability_bindings WHERE colleague_id=? AND capability_id=?",
		strings.TrimSpace(req.ColleagueID), capabilityID)
	response.OK(w, map[string]string{"status": "ok"})
}

func scanCapabilities(rows *sql.Rows) []CapabilityPackage {
	var result []CapabilityPackage
	for rows.Next() {
		var cp CapabilityPackage
		if err := rows.Scan(&cp.ID, &cp.Name, &cp.Description, &cp.Category, &cp.Version, &cp.Source, &cp.RiskLevel, &cp.Status, &cp.CreatedAt, &cp.UpdatedAt); err != nil {
			continue
		}
		result = append(result, cp)
	}
	if result == nil {
		result = []CapabilityPackage{}
	}
	return result
}

func extractID(path, prefix string) string {
	rest := strings.TrimPrefix(path, prefix)
	rest = strings.TrimRight(rest, "/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
