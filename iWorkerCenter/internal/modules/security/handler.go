package security

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

// Handler provides HTTP endpoints for security policy management.
type Handler struct {
	repo    *Repo
	checker *Checker
}

// NewHandler creates a security Handler.
func NewHandler(repo *Repo, checker *Checker) *Handler {
	return &Handler{repo: repo, checker: checker}
}

// RegisterAdminRoutes registers admin-facing routes.
func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/security/policies", h.handlePolicies)
	mux.HandleFunc("/admin/security/policies/", h.handlePolicyByID)
	mux.HandleFunc("/admin/security/hits", h.handleHits)
	mux.HandleFunc("/admin/security/check", h.handleCheck)
}

func (h *Handler) handlePolicies(w http.ResponseWriter, r *http.Request) {
	tid := tenant.TenantIDFromContext(r.Context())
	switch r.Method {
	case http.MethodGet:
		policies, err := h.repo.ListAllPolicies(tid)
		if err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]any{"policies": policies})
	case http.MethodPost:
		h.createPolicy(w, r)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *Handler) handlePolicyByID(w http.ResponseWriter, r *http.Request) {
	tid := tenant.TenantIDFromContext(r.Context())
	id := extractID(r.URL.Path, "/admin/security/policies/")
	if id == "" {
		response.BadRequest(w, "MISSING_ID", "policy id required")
		return
	}
	switch r.Method {
	case http.MethodGet:
		p, err := h.repo.GetPolicy(tid, id)
		if err != nil {
			response.NotFound(w, "NOT_FOUND", "policy not found")
			return
		}
		response.OK(w, p)
	case http.MethodPut:
		h.updatePolicy(w, r, id)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or PUT")
	}
}

func (h *Handler) createPolicy(w http.ResponseWriter, r *http.Request) {
	tid := tenant.TenantIDFromContext(r.Context())
	var req struct {
		Name        string `json:"name"`
		PolicyType  string `json:"policy_type"`
		Description string `json:"description"`
		Rules       any    `json:"rules"`
		Scope       string `json:"scope"`
		Priority    int    `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		response.BadRequest(w, "MISSING_NAME", "name is required")
		return
	}
	policyType := strings.TrimSpace(req.PolicyType)
	if policyType == "" {
		policyType = PolicyTypeKeywordBlock
	}
	scope := strings.TrimSpace(req.Scope)
	if scope == "" {
		scope = "all"
	}
	rulesJSON, _ := json.Marshal(req.Rules)
	now := time.Now().Format(time.RFC3339)

	p := &Policy{
		ID:          idgen.New("spol"),
		Name:        name,
		PolicyType:  policyType,
		Description: strings.TrimSpace(req.Description),
		Rules:       string(rulesJSON),
		Scope:       scope,
		Priority:    req.Priority,
		Status:      "active",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := h.repo.InsertPolicy(tid, p); err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.Created(w, p)
}

func (h *Handler) updatePolicy(w http.ResponseWriter, r *http.Request, id string) {
	tid := tenant.TenantIDFromContext(r.Context())
	var req struct {
		Name        string `json:"name"`
		PolicyType  string `json:"policy_type"`
		Description string `json:"description"`
		Rules       any    `json:"rules"`
		Scope       string `json:"scope"`
		Priority    int    `json:"priority"`
		Status      string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	rulesJSON, _ := json.Marshal(req.Rules)
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "active"
	}
	now := time.Now().Format(time.RFC3339)

	p := &Policy{
		ID:          id,
		Name:        strings.TrimSpace(req.Name),
		PolicyType:  strings.TrimSpace(req.PolicyType),
		Description: strings.TrimSpace(req.Description),
		Rules:       string(rulesJSON),
		Scope:       strings.TrimSpace(req.Scope),
		Priority:    req.Priority,
		Status:      status,
		UpdatedAt:   now,
	}
	if err := h.repo.UpdatePolicy(tid, p); err != nil {
		response.NotFound(w, "NOT_FOUND", "policy not found")
		return
	}
	response.OK(w, map[string]string{"status": "ok"})
}

func (h *Handler) handleHits(w http.ResponseWriter, r *http.Request) {
	tid := tenant.TenantIDFromContext(r.Context())
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	hits, err := h.repo.ListRecentHits(tid, 100)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]any{"hits": hits})
}

func (h *Handler) handleCheck(w http.ResponseWriter, r *http.Request) {
	tid := tenant.TenantIDFromContext(r.Context())
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	var req CheckInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	result := h.checker.Check(tid, req)
	response.OK(w, result)
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
