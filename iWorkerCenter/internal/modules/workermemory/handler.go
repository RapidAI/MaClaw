package workermemory

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

const (
	ScopeCompany = "company"
	// ScopeDepartment is a virtual organization-unit memory scope.
	// In AI native operations it does not imply a human middle-management layer;
	// it is a capability/domain boundary for recall, permissions, and routing.
	ScopeDepartment = "department"
	ScopePersonal   = "personal"
)

// Handler exposes iWorker memory APIs. iWorkerCenter is the source of truth;
// iWorker clients may cache responses locally but must not own canonical memory.
type Handler struct {
	store *corememory.Store
}

// NewHandler creates a worker memory handler backed by corelib memory.Store.
func NewHandler(store *corememory.Store) *Handler {
	return &Handler{store: store}
}

// RegisterClientRoutes registers iWorker-facing memory routes.
func (h *Handler) RegisterClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/client/iworker/memory-stats", h.handleStats)
	mux.HandleFunc("/client/iworker/memories", h.handleMemories)
	mux.HandleFunc("/client/iworker/memories/", h.handleMemoryByID)
}

func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	ctx := requestMemoryContext(r)
	if err := ctx.validateForRead(); err != nil {
		response.BadRequest(w, err.code, err.message)
		return
	}
	allowed := ctx.allowedOwnerIDs()
	stats := MemoryStats{
		TenantID:      ctx.TenantID,
		OrgUnitID:     ctx.DepartmentID,
		DepartmentID:  ctx.DepartmentID,
		WorkerID:      ctx.WorkerID,
		ByScope:       map[string]int{ScopeCompany: 0, ScopeDepartment: 0, ScopePersonal: 0},
		ByCategory:    map[string]int{},
		VisibleScopes: []string{ScopeCompany},
	}
	if strings.TrimSpace(ctx.DepartmentID) != "" {
		stats.VisibleScopes = append(stats.VisibleScopes, ScopeDepartment)
	}
	if strings.TrimSpace(ctx.WorkerID) != "" {
		stats.VisibleScopes = append(stats.VisibleScopes, ScopePersonal)
	}
	for _, entry := range h.store.Search("", "", 0) {
		if !allowed[entry.OwnerID] || !entry.IsActive() {
			continue
		}
		dto, ok := toDTO(entry)
		if !ok {
			continue
		}
		stats.Total++
		stats.ByScope[dto.Scope]++
		if dto.Category != "" {
			stats.ByCategory[dto.Category]++
		}
	}
	response.OK(w, stats)
}
func (h *Handler) handleMemories(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listOrRecall(w, r)
	case http.MethodPost:
		h.save(w, r)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *Handler) handleMemoryByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use DELETE")
		return
	}
	ctx := requestMemoryContext(r)
	if err := ctx.validateForRead(); err != nil {
		response.BadRequest(w, err.code, err.message)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/client/iworker/memories/")
	id = strings.Trim(id, "/")
	if id == "" {
		response.BadRequest(w, "MISSING_ID", "memory id is required")
		return
	}
	allowed := ctx.allowedOwnerIDs()
	items := h.store.Search("", "", 0)
	found := false
	for _, item := range items {
		if item.ID == id && allowed[item.OwnerID] {
			found = true
			break
		}
	}
	if !found {
		response.NotFound(w, "NOT_FOUND", "memory not found")
		return
	}
	if err := h.store.Delete(id); err != nil {
		response.Internal(w, err.Error())
		return
	}
	_ = h.store.Flush()
	response.OK(w, map[string]string{"status": "deleted"})
}

func (h *Handler) listOrRecall(w http.ResponseWriter, r *http.Request) {
	ctx := requestMemoryContext(r)
	if err := ctx.validateForRead(); err != nil {
		response.BadRequest(w, err.code, err.message)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	category := corememory.Category(strings.TrimSpace(r.URL.Query().Get("category")))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	entries := h.recallEntries(ctx, query, category)
	out := make([]MemoryDTO, 0, minInt(limit, len(entries)))
	for _, entry := range entries {
		dto, ok := toDTO(entry)
		if !ok {
			continue
		}
		out = append(out, dto)
		if len(out) >= limit {
			break
		}
	}
	response.OK(w, map[string]any{"memories": out})
}

func (h *Handler) recallEntries(ctx memoryContext, query string, category corememory.Category) []corememory.Entry {
	ownerIDs := ctx.recallOwnerIDs()
	seen := map[string]bool{}
	var entries []corememory.Entry
	if strings.TrimSpace(query) != "" {
		for _, ownerID := range ownerIDs {
			for _, entry := range h.store.RecallDynamic(query, category, "", ownerID) {
				if entry.OwnerID != ownerID || seen[entry.ID] {
					continue
				}
				seen[entry.ID] = true
				entries = append(entries, entry)
			}
		}
		return entries
	}
	allowed := ctx.allowedOwnerIDs()
	for _, entry := range h.store.Search(category, "", 0) {
		if !allowed[entry.OwnerID] || seen[entry.ID] {
			continue
		}
		seen[entry.ID] = true
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].UpdatedAt.After(entries[j].UpdatedAt)
	})
	return entries
}

func (h *Handler) save(w http.ResponseWriter, r *http.Request) {
	var req saveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
		return
	}
	ctx := requestMemoryContext(r)
	ctx.TenantID = firstNonEmpty(req.TenantID, ctx.TenantID)
	ctx.DepartmentID = firstNonEmpty(req.OrgUnitID, req.DepartmentID, ctx.DepartmentID)
	ctx.WorkerID = firstNonEmpty(req.WorkerID, ctx.WorkerID)
	scope := normalizeScope(req.Scope)
	if scope == "" {
		scope = ScopePersonal
	}
	if err := ctx.validateForScope(scope); err != nil {
		response.BadRequest(w, err.code, err.message)
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		response.BadRequest(w, "MISSING_CONTENT", "content is required")
		return
	}
	category := corememory.Category(strings.TrimSpace(req.Category))
	if category == "" {
		category = corememory.CategoryProjectKnowledge
	}
	ownerID := ctx.ownerIDForScope(scope)
	now := time.Now()
	entry := corememory.Entry{
		ID:         strings.TrimSpace(req.ID),
		Content:    content,
		Category:   category,
		Tags:       req.Tags,
		CreatedAt:  now,
		UpdatedAt:  now,
		SourceType: firstNonEmpty(strings.TrimSpace(req.SourceType), "iworker"),
		OwnerID:    ownerID,
	}
	if err := h.store.SaveForUser(entry, ownerID); err != nil {
		response.BadRequest(w, "SAVE_REJECTED", err.Error())
		return
	}
	_ = h.store.Flush()

	matches := h.store.Search(category, content, 0)
	for _, match := range matches {
		if match.OwnerID == ownerID {
			if dto, ok := toDTO(match); ok {
				response.Created(w, dto)
				return
			}
		}
	}
	if dto, ok := toDTO(entry); ok {
		response.Created(w, dto)
		return
	}
	response.Created(w, map[string]string{"status": "saved"})
}

type saveRequest struct {
	ID           string   `json:"id"`
	TenantID     string   `json:"tenant_id"`
	OrgUnitID    string   `json:"org_unit_id"`
	DepartmentID string   `json:"department_id"`
	WorkerID     string   `json:"worker_id"`
	Scope        string   `json:"scope"`
	Content      string   `json:"content"`
	Category     string   `json:"category"`
	Tags         []string `json:"tags"`
	SourceType   string   `json:"source_type"`
}

// MemoryStats summarizes the memory footprint visible to one iWorker context.
type MemoryStats struct {
	TenantID      string         `json:"tenant_id"`
	OrgUnitID     string         `json:"org_unit_id,omitempty"`
	DepartmentID  string         `json:"department_id,omitempty"`
	WorkerID      string         `json:"worker_id,omitempty"`
	Total         int            `json:"total"`
	ByScope       map[string]int `json:"by_scope"`
	ByCategory    map[string]int `json:"by_category"`
	VisibleScopes []string       `json:"visible_scopes"`
}

// MemoryDTO is the stable wire format between iWorker and iWorkerCenter.
type MemoryDTO struct {
	ID           string   `json:"id"`
	TenantID     string   `json:"tenant_id"`
	OrgUnitID    string   `json:"org_unit_id,omitempty"`
	DepartmentID string   `json:"department_id,omitempty"`
	WorkerID     string   `json:"worker_id,omitempty"`
	Scope        string   `json:"scope"`
	Content      string   `json:"content"`
	Category     string   `json:"category"`
	Tags         []string `json:"tags"`
	SourceType   string   `json:"source_type,omitempty"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

func toDTO(entry corememory.Entry) (MemoryDTO, bool) {
	owner, ok := parseOwnerID(entry.OwnerID)
	if !ok {
		return MemoryDTO{}, false
	}
	tags := entry.Tags
	if tags == nil {
		tags = []string{}
	}
	return MemoryDTO{
		ID:           entry.ID,
		TenantID:     owner.TenantID,
		OrgUnitID:    owner.DepartmentID,
		DepartmentID: owner.DepartmentID,
		WorkerID:     owner.WorkerID,
		Scope:        owner.Scope,
		Content:      entry.Content,
		Category:     string(entry.Category),
		Tags:         tags,
		SourceType:   entry.SourceType,
		CreatedAt:    formatTime(entry.CreatedAt),
		UpdatedAt:    formatTime(entry.UpdatedAt),
	}, true
}

type memoryContext struct {
	TenantID     string
	DepartmentID string
	WorkerID     string
}

type validationError struct {
	code    string
	message string
}

func requestMemoryContext(r *http.Request) memoryContext {
	q := r.URL.Query()
	return memoryContext{
		TenantID:     firstNonEmpty(q.Get("tenant_id"), tenant.TenantIDFromContext(r.Context()), "default"),
		DepartmentID: strings.TrimSpace(firstNonEmpty(q.Get("org_unit_id"), q.Get("department_id"))),
		WorkerID:     strings.TrimSpace(q.Get("worker_id")),
	}
}

func (c memoryContext) validateForRead() *validationError {
	if strings.TrimSpace(c.TenantID) == "" {
		return &validationError{code: "MISSING_TENANT_ID", message: "tenant_id is required"}
	}
	if strings.TrimSpace(c.WorkerID) == "" {
		return &validationError{code: "MISSING_WORKER_ID", message: "worker_id is required"}
	}
	return nil
}

func (c memoryContext) validateForScope(scope string) *validationError {
	if strings.TrimSpace(c.TenantID) == "" {
		return &validationError{code: "MISSING_TENANT_ID", message: "tenant_id is required"}
	}
	switch normalizeScope(scope) {
	case ScopeCompany:
		return nil
	case ScopeDepartment:
		if strings.TrimSpace(c.DepartmentID) == "" {
			return &validationError{code: "MISSING_ORG_UNIT_ID", message: "org_unit_id is required for virtual organization-unit memory"}
		}
		return nil
	case ScopePersonal:
		if strings.TrimSpace(c.WorkerID) == "" {
			return &validationError{code: "MISSING_WORKER_ID", message: "worker_id is required for personal memory"}
		}
		return nil
	default:
		return &validationError{code: "INVALID_SCOPE", message: "scope must be company, department, or personal"}
	}
}

func (c memoryContext) recallOwnerIDs() []string {
	owners := []string{companyOwnerID(c.TenantID)}
	if strings.TrimSpace(c.DepartmentID) != "" {
		owners = append(owners, departmentOwnerID(c.TenantID, c.DepartmentID))
	}
	if strings.TrimSpace(c.WorkerID) != "" {
		owners = append(owners, personalOwnerID(c.TenantID, c.WorkerID))
	}
	return owners
}

func (c memoryContext) allowedOwnerIDs() map[string]bool {
	out := map[string]bool{}
	for _, ownerID := range c.recallOwnerIDs() {
		out[ownerID] = true
	}
	return out
}

func (c memoryContext) ownerIDForScope(scope string) string {
	switch normalizeScope(scope) {
	case ScopeCompany:
		return companyOwnerID(c.TenantID)
	case ScopeDepartment:
		return departmentOwnerID(c.TenantID, c.DepartmentID)
	default:
		return personalOwnerID(c.TenantID, c.WorkerID)
	}
}

type parsedOwner struct {
	TenantID     string
	DepartmentID string
	WorkerID     string
	Scope        string
}

func parseOwnerID(ownerID string) (parsedOwner, bool) {
	parts := strings.Split(ownerID, ":")
	if len(parts) == 2 && parts[0] == "tenant" && parts[1] != "" {
		return parsedOwner{TenantID: parts[1], Scope: ScopeCompany}, true
	}
	if len(parts) == 4 && parts[0] == "tenant" && parts[2] == "department" && parts[1] != "" && parts[3] != "" {
		return parsedOwner{TenantID: parts[1], DepartmentID: parts[3], Scope: ScopeDepartment}, true
	}
	if len(parts) == 4 && parts[0] == "tenant" && parts[2] == "worker" && parts[1] != "" && parts[3] != "" {
		return parsedOwner{TenantID: parts[1], WorkerID: parts[3], Scope: ScopePersonal}, true
	}
	return parsedOwner{}, false
}

func companyOwnerID(tenantID string) string {
	return "tenant:" + normalizeOwnerPart(tenantID)
}

func departmentOwnerID(tenantID, departmentID string) string {
	return "tenant:" + normalizeOwnerPart(tenantID) + ":department:" + normalizeOwnerPart(departmentID)
}

func personalOwnerID(tenantID, workerID string) string {
	return "tenant:" + normalizeOwnerPart(tenantID) + ":worker:" + normalizeOwnerPart(workerID)
}

func normalizeScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "", ScopePersonal, "worker", "user":
		return ScopePersonal
	case ScopeDepartment, "dept", "team", "org_unit", "unit", "domain", "capability_domain":
		return ScopeDepartment
	case ScopeCompany, "enterprise", "tenant", "org", "organization":
		return ScopeCompany
	default:
		return ""
	}
}

func normalizeOwnerPart(v string) string {
	return strings.ReplaceAll(strings.TrimSpace(v), ":", "_")
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
