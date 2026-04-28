package modelrouting

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

// Endpoint represents a model endpoint (provider).
type Endpoint struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Protocol  string `json:"protocol"` // openai, anthropic
	BaseURL   string `json:"base_url"`
	APIKey    string `json:"api_key"`
	Model     string `json:"model"`
	CostTier  string `json:"cost_tier"` // high, medium, low
	Priority  int    `json:"priority"`
	Features  string `json:"features"` // JSON array of feature keywords
	Status    string `json:"status"`   // active, disabled
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// RoutingPolicy represents a model routing rule.
type RoutingPolicy struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	WorkType     string `json:"work_type"`     // target work type or "*"
	RoleCode     string `json:"role_code"`     // target role or "*"
	EndpointID   string `json:"endpoint_id"`   // preferred endpoint
	FallbackMode string `json:"fallback_mode"` // next_priority, any_tier
	Priority     int    `json:"priority"`
	Status       string `json:"status"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// Handler provides HTTP endpoints for model routing management.
type Handler struct {
	write *sql.DB
	read  *sql.DB
}

// NewHandler creates a model routing Handler.
func NewHandler(write, read *sql.DB) *Handler {
	return &Handler{write: write, read: read}
}

// RegisterAdminRoutes registers admin-facing routes.
func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/model-endpoints", h.handleEndpoints)
	mux.HandleFunc("/admin/model-endpoints/", h.handleEndpointByID)
	mux.HandleFunc("/admin/model-routing-policies", h.handlePolicies)
	mux.HandleFunc("/admin/model-routing-policies/", h.handlePolicyByID)
}

// --- Endpoints ---

func (h *Handler) handleEndpoints(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listEndpoints(w, r)
	case http.MethodPost:
		h.createEndpoint(w, r)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *Handler) handleEndpointByID(w http.ResponseWriter, r *http.Request) {
	id := extractID(r.URL.Path, "/admin/model-endpoints/")
	if id == "" {
		response.BadRequest(w, "MISSING_ID", "endpoint id required")
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.getEndpoint(w, r, id)
	case http.MethodPut:
		h.updateEndpoint(w, r, id)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or PUT")
	}
}

func (h *Handler) listEndpoints(w http.ResponseWriter, r *http.Request) {
	tenantID := tenant.RequestTenantID(r)
	rows, err := h.read.Query(`SELECT id, name, protocol, base_url, api_key, model, cost_tier, priority, features, status, created_at, updated_at
		FROM model_endpoints WHERE tenant_id=? ORDER BY priority DESC`, tenantID)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	defer rows.Close()
	var endpoints []Endpoint
	for rows.Next() {
		var e Endpoint
		if err := rows.Scan(&e.ID, &e.Name, &e.Protocol, &e.BaseURL, &e.APIKey, &e.Model, &e.CostTier, &e.Priority, &e.Features, &e.Status, &e.CreatedAt, &e.UpdatedAt); err != nil {
			continue
		}
		// Mask API key for display
		if len(e.APIKey) > 8 {
			e.APIKey = e.APIKey[:4] + "****" + e.APIKey[len(e.APIKey)-4:]
		} else if e.APIKey != "" {
			e.APIKey = "****"
		}
		endpoints = append(endpoints, e)
	}
	if endpoints == nil {
		endpoints = []Endpoint{}
	}
	response.OK(w, map[string]any{"endpoints": endpoints})
}

func (h *Handler) getEndpoint(w http.ResponseWriter, r *http.Request, id string) {
	tenantID := tenant.RequestTenantID(r)
	var e Endpoint
	err := h.read.QueryRow(`SELECT id, name, protocol, base_url, api_key, model, cost_tier, priority, features, status, created_at, updated_at
		FROM model_endpoints WHERE id=? AND tenant_id=?`, id, tenantID).Scan(&e.ID, &e.Name, &e.Protocol, &e.BaseURL, &e.APIKey, &e.Model, &e.CostTier, &e.Priority, &e.Features, &e.Status, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		response.NotFound(w, "NOT_FOUND", "endpoint not found")
		return
	}
	if len(e.APIKey) > 8 {
		e.APIKey = e.APIKey[:4] + "****" + e.APIKey[len(e.APIKey)-4:]
	} else if e.APIKey != "" {
		e.APIKey = "****"
	}
	response.OK(w, e)
}

func (h *Handler) createEndpoint(w http.ResponseWriter, r *http.Request) {
	var req Endpoint
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		response.BadRequest(w, "MISSING_NAME", "name is required")
		return
	}
	now := time.Now().Format(time.RFC3339)
	tenantID := tenant.RequestTenantID(r)
	e := Endpoint{
		ID:        idgen.New("mep"),
		Name:      strings.TrimSpace(req.Name),
		Protocol:  defaultStr(req.Protocol, "openai"),
		BaseURL:   strings.TrimSpace(req.BaseURL),
		APIKey:    strings.TrimSpace(req.APIKey),
		Model:     strings.TrimSpace(req.Model),
		CostTier:  defaultStr(req.CostTier, "medium"),
		Priority:  req.Priority,
		Features:  defaultStr(req.Features, "[]"),
		Status:    "active",
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := h.write.Exec(`INSERT INTO model_endpoints (id, tenant_id, name, protocol, base_url, api_key, model, cost_tier, priority, features, status, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, tenantID, e.Name, e.Protocol, e.BaseURL, e.APIKey, e.Model, e.CostTier, e.Priority, e.Features, e.Status, e.CreatedAt, e.UpdatedAt)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	if len(e.APIKey) > 8 {
		e.APIKey = e.APIKey[:4] + "****" + e.APIKey[len(e.APIKey)-4:]
	} else if e.APIKey != "" {
		e.APIKey = "****"
	}
	response.Created(w, e)
}

func (h *Handler) updateEndpoint(w http.ResponseWriter, r *http.Request, id string) {
	var req Endpoint
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	now := time.Now().Format(time.RFC3339)
	status := defaultStr(req.Status, "active")
	tenantID := tenant.RequestTenantID(r)
	res, err := h.write.Exec(`UPDATE model_endpoints SET name=?, protocol=?, base_url=?, api_key=?, model=?, cost_tier=?, priority=?, features=?, status=?, updated_at=? WHERE id=? AND tenant_id=?`,
		strings.TrimSpace(req.Name), defaultStr(req.Protocol, "openai"),
		strings.TrimSpace(req.BaseURL), strings.TrimSpace(req.APIKey),
		strings.TrimSpace(req.Model), defaultStr(req.CostTier, "medium"),
		req.Priority, defaultStr(req.Features, "[]"), status, now, id, tenantID)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		response.NotFound(w, "NOT_FOUND", "endpoint not found")
		return
	}
	response.OK(w, map[string]string{"status": "ok"})
}

// --- Routing Policies ---

func (h *Handler) handlePolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listPolicies(w, r)
	case http.MethodPost:
		h.createPolicy(w, r)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *Handler) handlePolicyByID(w http.ResponseWriter, r *http.Request) {
	id := extractID(r.URL.Path, "/admin/model-routing-policies/")
	if id == "" {
		response.BadRequest(w, "MISSING_ID", "policy id required")
		return
	}
	switch r.Method {
	case http.MethodPut:
		h.updatePolicy(w, r, id)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PUT")
	}
}

func (h *Handler) listPolicies(w http.ResponseWriter, r *http.Request) {
	tenantID := tenant.RequestTenantID(r)
	rows, err := h.read.Query(`SELECT id, name, description, work_type, role_code, endpoint_id, fallback_mode, priority, status, created_at, updated_at
		FROM model_routing_policies WHERE tenant_id=? ORDER BY priority DESC`, tenantID)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	defer rows.Close()
	var policies []RoutingPolicy
	for rows.Next() {
		var p RoutingPolicy
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.WorkType, &p.RoleCode, &p.EndpointID, &p.FallbackMode, &p.Priority, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
			continue
		}
		policies = append(policies, p)
	}
	if policies == nil {
		policies = []RoutingPolicy{}
	}
	response.OK(w, map[string]any{"policies": policies})
}

func (h *Handler) createPolicy(w http.ResponseWriter, r *http.Request) {
	var req RoutingPolicy
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		response.BadRequest(w, "MISSING_NAME", "name is required")
		return
	}
	now := time.Now().Format(time.RFC3339)
	tenantID := tenant.RequestTenantID(r)
	p := RoutingPolicy{
		ID:           idgen.New("mrp"),
		Name:         strings.TrimSpace(req.Name),
		Description:  strings.TrimSpace(req.Description),
		WorkType:     defaultStr(req.WorkType, "*"),
		RoleCode:     defaultStr(req.RoleCode, "*"),
		EndpointID:   strings.TrimSpace(req.EndpointID),
		FallbackMode: defaultStr(req.FallbackMode, "next_priority"),
		Priority:     req.Priority,
		Status:       "active",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	_, err := h.write.Exec(`INSERT INTO model_routing_policies (id, tenant_id, name, description, work_type, role_code, endpoint_id, fallback_mode, priority, status, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, tenantID, p.Name, p.Description, p.WorkType, p.RoleCode, p.EndpointID, p.FallbackMode, p.Priority, p.Status, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.Created(w, p)
}

func (h *Handler) updatePolicy(w http.ResponseWriter, r *http.Request, id string) {
	var req RoutingPolicy
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	now := time.Now().Format(time.RFC3339)
	status := defaultStr(req.Status, "active")
	tenantID := tenant.RequestTenantID(r)
	res, err := h.write.Exec(`UPDATE model_routing_policies SET name=?, description=?, work_type=?, role_code=?, endpoint_id=?, fallback_mode=?, priority=?, status=?, updated_at=? WHERE id=? AND tenant_id=?`,
		strings.TrimSpace(req.Name), strings.TrimSpace(req.Description),
		defaultStr(req.WorkType, "*"), defaultStr(req.RoleCode, "*"),
		strings.TrimSpace(req.EndpointID), defaultStr(req.FallbackMode, "next_priority"),
		req.Priority, status, now, id, tenantID)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		response.NotFound(w, "NOT_FOUND", "policy not found")
		return
	}
	response.OK(w, map[string]string{"status": "ok"})
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

func defaultStr(val, def string) string {
	if strings.TrimSpace(val) == "" {
		return def
	}
	return strings.TrimSpace(val)
}
