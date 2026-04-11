package memories

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

// MemoryEntry represents a shared memory record.
type MemoryEntry struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Level     string   `json:"level"`  // enterprise, role, team
	Scope     string   `json:"scope"`  // all, office, data, production, quality, team_x
	Tags      []string `json:"tags"`
	Version   int      `json:"version"`
	Status    string   `json:"status"` // active, disabled
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// Handler provides HTTP endpoints for shared memory management.
type Handler struct {
	write *sql.DB
	read  *sql.DB
}

// NewHandler creates a memories Handler.
func NewHandler(write, read *sql.DB) *Handler {
	return &Handler{write: write, read: read}
}

// RegisterAdminRoutes registers admin-facing routes.
func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/memories", h.handleAdminMemories)
	mux.HandleFunc("/admin/memories/", h.handleAdminMemoryByID)
}

// RegisterClientRoutes registers client-facing routes (for DiWorker).
func (h *Handler) RegisterClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/client/memories", h.handleClientMemories)
}

func (h *Handler) handleAdminMemories(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listMemories(w, r, false)
	case http.MethodPost:
		h.createMemory(w, r)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *Handler) handleAdminMemoryByID(w http.ResponseWriter, r *http.Request) {
	id := extractID(r.URL.Path, "/admin/memories/")
	if id == "" {
		response.BadRequest(w, "MISSING_ID", "memory id is required")
		return
	}
	switch r.Method {
	case http.MethodPut:
		h.updateMemory(w, r, id)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PUT")
	}
}

func (h *Handler) handleClientMemories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	h.listMemories(w, r, true)
}

func (h *Handler) listMemories(w http.ResponseWriter, r *http.Request, activeOnly bool) {
	roleCode := r.URL.Query().Get("role_code")

	query := "SELECT id, title, content, level, scope, tags, version, status, created_at, updated_at FROM shared_memories"
	var conditions []string
	var args []interface{}

	if activeOnly {
		conditions = append(conditions, "status='active'")
	}

	// Filter: enterprise memories + role-specific memories matching scope
	if roleCode != "" {
		conditions = append(conditions, "(scope='all' OR scope=?)")
		args = append(args, roleCode)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY level, title"

	rows, err := h.read.Query(query, args...)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	defer rows.Close()

	var memories []MemoryEntry
	for rows.Next() {
		var m MemoryEntry
		var tags, createdAt, updatedAt string
		if err := rows.Scan(&m.ID, &m.Title, &m.Content, &m.Level, &m.Scope, &tags, &m.Version, &m.Status, &createdAt, &updatedAt); err != nil {
			response.Internal(w, err.Error())
			return
		}
		_ = json.Unmarshal([]byte(tags), &m.Tags)
		if m.Tags == nil {
			m.Tags = []string{}
		}
		m.CreatedAt = createdAt
		m.UpdatedAt = updatedAt
		memories = append(memories, m)
	}
	if memories == nil {
		memories = []MemoryEntry{}
	}
	response.OK(w, map[string]any{"memories": memories})
}

func (h *Handler) createMemory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title   string   `json:"title"`
		Content string   `json:"content"`
		Level   string   `json:"level"`
		Scope   string   `json:"scope"`
		Tags    []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		response.BadRequest(w, "MISSING_TITLE", "title is required")
		return
	}
	level := strings.TrimSpace(req.Level)
	if level == "" {
		level = "enterprise"
	}
	scope := strings.TrimSpace(req.Scope)
	if scope == "" {
		scope = "all"
	}
	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, _ := json.Marshal(tags)
	now := time.Now().Format(time.RFC3339)
	id := idgen.New("mem")

	_, err := h.write.Exec(`INSERT INTO shared_memories (id, title, content, level, scope, tags, version, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 1, 'active', ?, ?)`,
		id, title, strings.TrimSpace(req.Content), level, scope, string(tagsJSON), now, now)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}

	response.Created(w, MemoryEntry{
		ID: id, Title: title, Content: strings.TrimSpace(req.Content),
		Level: level, Scope: scope, Tags: tags, Version: 1, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	})
}

func (h *Handler) updateMemory(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Title   string   `json:"title"`
		Content string   `json:"content"`
		Level   string   `json:"level"`
		Scope   string   `json:"scope"`
		Tags    []string `json:"tags"`
		Status  string   `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
		return
	}

	now := time.Now().Format(time.RFC3339)
	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, _ := json.Marshal(tags)
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "active"
	}

	res, err := h.write.Exec(`UPDATE shared_memories SET title=?, content=?, level=?, scope=?, tags=?, status=?, version=version+1, updated_at=? WHERE id=?`,
		strings.TrimSpace(req.Title), strings.TrimSpace(req.Content),
		strings.TrimSpace(req.Level), strings.TrimSpace(req.Scope),
		string(tagsJSON), status, now, id)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		response.NotFound(w, "NOT_FOUND", "memory not found")
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
