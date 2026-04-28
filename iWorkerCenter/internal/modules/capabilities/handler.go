package capabilities

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/audit"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
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

type cloudCredentialResolver interface {
	CloudCredentials(ctx context.Context, tenantID string) (string, string, error)
}

// Handler provides HTTP endpoints for capability management.
type Handler struct {
	write                   *sql.DB
	read                    *sql.DB
	importer                *Importer
	cloudURL                string
	cloudCredentialResolver cloudCredentialResolver
	audit                   *audit.Repo
}

// NewHandler creates a capabilities Handler.
func NewHandler(write, read *sql.DB) *Handler {
	return &Handler{write: write, read: read}
}

// SetImporter attaches an iWorkerCloud skill market importer (optional).
func (h *Handler) SetImporter(imp *Importer) {
	h.importer = imp
}

// SetCloudImporterResolver lets the handler build a fresh iWorkerCloud importer
// from the current tenant registration, so post-setup cloud registration works
// without restarting iWorkerCenter.
func (h *Handler) SetCloudImporterResolver(cloudURL string, resolver cloudCredentialResolver) {
	h.cloudURL = strings.TrimRight(strings.TrimSpace(cloudURL), "/")
	h.cloudCredentialResolver = resolver
}

func (h *Handler) importerForRequest(r *http.Request) (*Importer, error) {
	if h.importer != nil {
		return h.importer, nil
	}
	if h.cloudURL == "" || h.cloudCredentialResolver == nil {
		return nil, fmt.Errorf("iWorkerCloud skill import not configured")
	}
	centerID, centerSecret, err := h.cloudCredentialResolver.CloudCredentials(r.Context(), tenant.RequestTenantID(r))
	if err != nil {
		return nil, err
	}
	return NewImporter(h.write, h.cloudURL, centerID, centerSecret), nil
}
func (h *Handler) SetAuditRepo(repo *audit.Repo) {
	h.audit = repo
}

// RegisterAdminRoutes registers admin-facing routes.
func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/capabilities", h.handleAdminCapabilities)
	mux.HandleFunc("/admin/capabilities/", h.handleAdminCapabilityByID)
	mux.HandleFunc("/admin/capabilities-import/search", h.handleImportSearch)
	mux.HandleFunc("/admin/capabilities-import/import", h.handleImportFromCloud)
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

	// /admin/capabilities/{id}/approve
	if strings.HasSuffix(path, "/approve") {
		id = strings.TrimSuffix(id, "/approve")
		if r.Method == http.MethodPost {
			h.approveCapability(w, r, id)
			return
		}
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}

	// /admin/capabilities/{id}/reject
	if strings.HasSuffix(path, "/reject") {
		id = strings.TrimSuffix(id, "/reject")
		if r.Method == http.MethodPost {
			h.rejectCapability(w, r, id)
			return
		}
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getCapability(w, r, id)
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
		h.listColleagueCapabilities(w, r, colleagueID)
		return
	}
	h.listActiveCapabilities(w, r)
}

func (h *Handler) listCapabilities(w http.ResponseWriter, r *http.Request) {
	tenantID := tenant.RequestTenantID(r)
	rows, err := h.read.Query("SELECT id, name, description, category, version, source, risk_level, status, created_at, updated_at FROM capability_packages WHERE tenant_id=? ORDER BY name", tenantID)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	defer rows.Close()
	caps := scanCapabilities(rows)
	response.OK(w, map[string]any{"capabilities": caps})
}

func (h *Handler) listActiveCapabilities(w http.ResponseWriter, r *http.Request) {
	tenantID := tenant.RequestTenantID(r)
	rows, err := h.read.Query("SELECT id, name, description, category, version, source, risk_level, status, created_at, updated_at FROM capability_packages WHERE status IN ('active','approved') AND tenant_id=? ORDER BY name", tenantID)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	defer rows.Close()
	caps := scanCapabilities(rows)
	response.OK(w, map[string]any{"capabilities": caps})
}

func (h *Handler) listColleagueCapabilities(w http.ResponseWriter, r *http.Request, colleagueID string) {
	tenantID := tenant.RequestTenantID(r)
	rows, err := h.read.Query(`SELECT cp.id, cp.name, cp.description, cp.category, cp.version, cp.source, cp.risk_level, cp.status, cp.created_at, cp.updated_at
		FROM capability_packages cp
		JOIN colleague_capability_bindings ccb ON cp.id = ccb.capability_id
		WHERE ccb.colleague_id = ? AND cp.status IN ('active','approved') AND ccb.tenant_id = ?
		ORDER BY cp.name`, colleagueID, tenantID)
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
	tenantID := tenant.RequestTenantID(r)

	_, err := h.write.Exec(`INSERT INTO capability_packages (id, tenant_id, name, description, category, version, source, risk_level, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)`,
		id, tenantID, name, strings.TrimSpace(req.Description), category, version, source, riskLevel, now, now)
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

func (h *Handler) getCapability(w http.ResponseWriter, r *http.Request, id string) {
	tenantID := tenant.RequestTenantID(r)
	row := h.read.QueryRow("SELECT id, name, description, category, version, source, risk_level, status, created_at, updated_at FROM capability_packages WHERE id=? AND tenant_id=?", id, tenantID)
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
	tenantID := tenant.RequestTenantID(r)
	res, err := h.write.Exec(`UPDATE capability_packages SET name=?, description=?, category=?, version=?, risk_level=?, status=?, updated_at=? WHERE id=? AND tenant_id=?`,
		strings.TrimSpace(req.Name), strings.TrimSpace(req.Description),
		strings.TrimSpace(req.Category), strings.TrimSpace(req.Version),
		strings.TrimSpace(req.RiskLevel), status, now, id, tenantID)
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
	tenantID := tenant.RequestTenantID(r)
	_, err := h.write.Exec(`INSERT OR IGNORE INTO colleague_capability_bindings (id, tenant_id, colleague_id, capability_id, bound_at) VALUES (?, ?, ?, ?, ?)`,
		id, tenantID, strings.TrimSpace(req.ColleagueID), capabilityID, now)
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
	_, _ = h.write.Exec("DELETE FROM colleague_capability_bindings WHERE colleague_id=? AND capability_id=? AND tenant_id=?",
		strings.TrimSpace(req.ColleagueID), capabilityID, tenant.RequestTenantID(r))
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

func (h *Handler) handleImportSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	imp, err := h.importerForRequest(r)
	if err != nil {
		response.BadRequest(w, "NOT_CONFIGURED", err.Error())
		return
	}
	query := r.URL.Query().Get("q")
	skills, err := imp.SearchCloud(query)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]any{"results": skills})
}

func (h *Handler) handleImportFromCloud(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	imp, err := h.importerForRequest(r)
	if err != nil {
		response.BadRequest(w, "NOT_CONFIGURED", err.Error())
		return
	}
	var req struct {
		SkillID string `json:"skill_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	if strings.TrimSpace(req.SkillID) == "" {
		response.BadRequest(w, "MISSING_SKILL_ID", "skill_id is required")
		return
	}
	cap, err := imp.ImportFromCloud(strings.TrimSpace(req.SkillID), tenant.RequestTenantID(r))
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.Created(w, cap)
}

func (h *Handler) approveCapability(w http.ResponseWriter, r *http.Request, id string) {
	tenantID := tenant.RequestTenantID(r)
	capabilityName, currentStatus, err := h.lookupCapabilityApprovalContext(r.Context(), tenantID, id)
	if err == sql.ErrNoRows {
		response.NotFound(w, "NOT_FOUND", "capability not found")
		return
	}
	if err != nil {
		response.Internal(w, err.Error())
		return
	}

	if h.importer == nil {
		now := time.Now().Format(time.RFC3339)
		if currentStatus == "approved" {
			response.OK(w, map[string]string{"status": "approved"})
			return
		}
		res, err := h.write.Exec(`UPDATE capability_packages SET status='approved', updated_at=? WHERE id=? AND status IN ('pending_review','active') AND tenant_id=?`, now, id, tenantID)
		if err != nil {
			response.Internal(w, err.Error())
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			response.NotFound(w, "NOT_FOUND", "capability not found or not approvable")
			return
		}
		if err := h.recordCapabilityApprovedAudit(r.Context(), tenantID, id, capabilityName); err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]string{"status": "approved"})
		return
	}
	imp, err := h.importerForRequest(r)
	if err != nil {
		response.BadRequest(w, "NOT_CONFIGURED", err.Error())
		return
	}
	if err := imp.ApproveCapability(id, tenantID); err != nil {
		response.BadRequest(w, "APPROVE_FAILED", err.Error())
		return
	}
	_ = h.recordCapabilityApprovedAudit(r.Context(), tenantID, id, capabilityName)
	response.OK(w, map[string]string{"status": "approved"})
}

func (h *Handler) lookupCapabilityApprovalContext(ctx context.Context, tenantID, id string) (string, string, error) {
	var name string
	var status string
	err := h.read.QueryRowContext(ctx, `SELECT name, status FROM capability_packages WHERE id=? AND tenant_id=?`, id, tenantID).Scan(&name, &status)
	return name, status, err
}

func capabilityRoleCodeFromName(name string) string {
	trimmed := strings.TrimSpace(strings.TrimSuffix(strings.ToLower(name), " recovery handling"))
	return strings.ReplaceAll(trimmed, " ", "-")
}

func (h *Handler) recordCapabilityApprovedAudit(ctx context.Context, tenantID, capabilityID, capabilityName string) error {
	if h.audit == nil || tenantID == "" {
		return nil
	}
	roleCode := capabilityRoleCodeFromName(capabilityName)
	return h.audit.Insert(tenantID, &audit.ProxyLog{
		RequestID:   fmt.Sprintf("executive-capability-approved-%s-%d", capabilityID, time.Now().UnixNano()),
		ProviderID:  "iworkercenter",
		Model:       "executive-capability-approved",
		WorkType:    "executive_capability_approved",
		CostTier:    "internal",
		Status:      "ok",
		LatencyMs:   0,
		InputTokens: 0,
		Summary:     fmt.Sprintf("Recovery capability package approved for %s", strings.ToUpper(roleCode)),
		ErrorMsg:    fmt.Sprintf("role_code: %s | capability_id: %s | capability_name: %s", roleCode, capabilityID, capabilityName),
	})
}
func (h *Handler) rejectCapability(w http.ResponseWriter, r *http.Request, id string) {
	if h.importer == nil {
		now := time.Now().Format(time.RFC3339)
		tenantID := tenant.RequestTenantID(r)
		res, err := h.write.Exec(`UPDATE capability_packages SET status='rejected', updated_at=? WHERE id=? AND status='pending_review' AND tenant_id=?`, now, id, tenantID)
		if err != nil {
			response.Internal(w, err.Error())
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			response.NotFound(w, "NOT_FOUND", "capability not found or not pending")
			return
		}
		response.OK(w, map[string]string{"status": "rejected"})
		return
	}
	imp, err := h.importerForRequest(r)
	if err != nil {
		response.BadRequest(w, "NOT_CONFIGURED", err.Error())
		return
	}
	if err := imp.RejectCapability(id, tenant.RequestTenantID(r)); err != nil {
		response.BadRequest(w, "REJECT_FAILED", err.Error())
		return
	}
	response.OK(w, map[string]string{"status": "rejected"})
}
