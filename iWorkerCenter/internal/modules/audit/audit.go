package audit

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
)

// ProxyLog represents a local Center audit record, including model calls and governance events.
type ProxyLog struct {
	ID          string
	RequestID   string
	ProviderID  string
	Model       string
	WorkType    string
	CostTier    string
	Status      string // ok, error
	LatencyMs   int
	InputTokens int
	Summary     string
	ErrorMsg    string
	CreatedAt   time.Time
}

// LogFilter scopes audit log listing without exposing enterprise business tables.
type LogFilter struct {
	Limit    int
	Status   string
	WorkType string
	Query    string
	Category string
}

// Repo provides persistence for audit logs.
type Repo struct {
	write *sql.DB
	read  *sql.DB
}

// NewRepo creates a Repo.
func NewRepo(write, read *sql.DB) *Repo {
	return &Repo{write: write, read: read}
}

// Insert records a proxy audit log entry.
func (r *Repo) Insert(tenantID string, log *ProxyLog) error {
	if log.ID == "" {
		log.ID = idgen.New("pxlog")
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now().UTC()
	}
	_, err := r.write.Exec(`INSERT INTO proxy_audit_log
		(id, tenant_id, request_id, provider_id, model, work_type, cost_tier, status, latency_ms, input_tokens, summary, error_msg, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		log.ID, tenantID, log.RequestID, log.ProviderID, log.Model, log.WorkType, log.CostTier,
		log.Status, log.LatencyMs, log.InputTokens, log.Summary, log.ErrorMsg,
		log.CreatedAt.Format(time.RFC3339))
	return err
}

// Stats holds aggregated usage statistics.
type Stats struct {
	TotalRequests       int    `json:"total_requests"`
	OKCount             int    `json:"ok_count"`
	ErrorCount          int    `json:"error_count"`
	AvgLatencyMs        int    `json:"avg_latency_ms"`
	MCPEvents           int    `json:"mcp_events"`
	SkillEvents         int    `json:"skill_events"`
	CollaborationEvents int    `json:"collaboration_events"`
	ModelEvents         int    `json:"model_events"`
	MCPErrors           int    `json:"mcp_errors"`
	SkillErrors         int    `json:"skill_errors"`
	CollaborationErrors int    `json:"collaboration_errors"`
	ModelErrors         int    `json:"model_errors"`
	TopProvider         string `json:"top_provider"`
	TopWorkType         string `json:"top_work_type"`
	TopErrorWorkType    string `json:"top_error_work_type"`
	LastErrorAt         string `json:"last_error_at"`
}

// GetStats returns aggregated stats for the last N hours.
func (r *Repo) GetStats(tenantID string, hours int) (*Stats, error) {
	if hours <= 0 {
		hours = 24
	}
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour).Format(time.RFC3339)

	var stats Stats
	row := r.read.QueryRow(`SELECT
		COUNT(*),
		COALESCE(SUM(CASE WHEN status='ok' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status='error' THEN 1 ELSE 0 END), 0),
		CAST(COALESCE(AVG(latency_ms), 0) AS INTEGER),
		COALESCE(SUM(CASE WHEN work_type LIKE 'mcp_%' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN work_type LIKE 'skill_%' OR work_type LIKE '%capability%' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN work_type LIKE '%collaboration%' OR work_type LIKE '%role_routing%' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN (model != '' OR provider_id != '')
			AND work_type NOT LIKE 'mcp_%'
			AND work_type NOT LIKE 'skill_%'
			AND work_type NOT LIKE '%capability%'
			AND work_type NOT LIKE '%collaboration%'
			AND work_type NOT LIKE '%role_routing%'
			THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status='error' AND work_type LIKE 'mcp_%' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status='error' AND (work_type LIKE 'skill_%' OR work_type LIKE '%capability%') THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status='error' AND (work_type LIKE '%collaboration%' OR work_type LIKE '%role_routing%') THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status='error'
			AND (model != '' OR provider_id != '')
			AND work_type NOT LIKE 'mcp_%'
			AND work_type NOT LIKE 'skill_%'
			AND work_type NOT LIKE '%capability%'
			AND work_type NOT LIKE '%collaboration%'
			AND work_type NOT LIKE '%role_routing%'
			THEN 1 ELSE 0 END), 0)
		FROM proxy_audit_log WHERE tenant_id=? AND created_at >= ?`, tenantID, cutoff)
	if err := row.Scan(&stats.TotalRequests, &stats.OKCount, &stats.ErrorCount, &stats.AvgLatencyMs, &stats.MCPEvents, &stats.SkillEvents, &stats.CollaborationEvents, &stats.ModelEvents, &stats.MCPErrors, &stats.SkillErrors, &stats.CollaborationErrors, &stats.ModelErrors); err != nil {
		return nil, err
	}

	// Top provider
	_ = r.read.QueryRow(`SELECT provider_id FROM proxy_audit_log WHERE tenant_id=? AND created_at >= ?
		GROUP BY provider_id ORDER BY COUNT(*) DESC LIMIT 1`, tenantID, cutoff).Scan(&stats.TopProvider)

	// Top work type
	_ = r.read.QueryRow(`SELECT work_type FROM proxy_audit_log WHERE tenant_id=? AND created_at >= ? AND work_type != ''
		GROUP BY work_type ORDER BY COUNT(*) DESC LIMIT 1`, tenantID, cutoff).Scan(&stats.TopWorkType)

	_ = r.read.QueryRow(`SELECT work_type FROM proxy_audit_log WHERE tenant_id=? AND created_at >= ? AND status='error' AND work_type != ''
		GROUP BY work_type ORDER BY COUNT(*) DESC LIMIT 1`, tenantID, cutoff).Scan(&stats.TopErrorWorkType)

	_ = r.read.QueryRow(`SELECT created_at FROM proxy_audit_log WHERE tenant_id=? AND created_at >= ? AND status='error'
		ORDER BY created_at DESC LIMIT 1`, tenantID, cutoff).Scan(&stats.LastErrorAt)

	return &stats, nil
}

// ListRecent returns the most recent N audit log entries.
func (r *Repo) ListRecent(tenantID string, limit int) ([]*ProxyLog, error) {
	return r.ListRecentFiltered(tenantID, LogFilter{Limit: limit})
}

// ListRecentFiltered returns recent audit records matching optional status, work type, and text filters.
func (r *Repo) ListRecentFiltered(tenantID string, filter LogFilter) ([]*ProxyLog, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	where := []string{"tenant_id=?"}
	args := []any{tenantID}
	if status := strings.TrimSpace(filter.Status); status != "" && status != "all" {
		where = append(where, "status=?")
		args = append(args, status)
	}
	if workType := strings.TrimSpace(filter.WorkType); workType != "" && workType != "all" {
		where = append(where, "work_type=?")
		args = append(args, workType)
	}
	switch strings.TrimSpace(filter.Category) {
	case "mcp":
		where = append(where, "work_type LIKE 'mcp_%'")
	case "skill":
		where = append(where, "(work_type LIKE 'skill_%' OR work_type LIKE '%capability%')")
	case "collaboration":
		where = append(where, "(work_type LIKE '%collaboration%' OR work_type LIKE '%role_routing%')")
	case "model":
		where = append(where, "(model != '' OR provider_id != '')")
		where = append(where, "work_type NOT LIKE 'mcp_%'")
		where = append(where, "work_type NOT LIKE 'skill_%'")
		where = append(where, "work_type NOT LIKE '%capability%'")
		where = append(where, "work_type NOT LIKE '%collaboration%'")
		where = append(where, "work_type NOT LIKE '%role_routing%'")
	case "errors":
		where = append(where, "status='error'")
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		like := "%" + query + "%"
		where = append(where, "(summary LIKE ? OR error_msg LIKE ? OR request_id LIKE ? OR provider_id LIKE ? OR work_type LIKE ?)")
		args = append(args, like, like, like, like, like)
	}
	args = append(args, limit)
	rows, err := r.read.Query(fmt.Sprintf(`SELECT id, request_id, provider_id, model, work_type, cost_tier,
		status, latency_ms, input_tokens, summary, error_msg, created_at
		FROM proxy_audit_log WHERE %s ORDER BY created_at DESC LIMIT ?`, strings.Join(where, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*ProxyLog
	for rows.Next() {
		var l ProxyLog
		var ca string
		if err := rows.Scan(&l.ID, &l.RequestID, &l.ProviderID, &l.Model, &l.WorkType, &l.CostTier,
			&l.Status, &l.LatencyMs, &l.InputTokens, &l.Summary, &l.ErrorMsg, &ca); err != nil {
			return nil, err
		}
		l.CreatedAt, _ = time.Parse(time.RFC3339, ca)
		result = append(result, &l)
	}
	return result, rows.Err()
}
