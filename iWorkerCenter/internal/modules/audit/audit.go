package audit

import (
	"database/sql"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
)

// ProxyLog represents a single LLM proxy request audit record.
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
		log.CreatedAt = time.Now()
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
	TotalRequests  int    `json:"total_requests"`
	OKCount        int    `json:"ok_count"`
	ErrorCount     int    `json:"error_count"`
	AvgLatencyMs   int    `json:"avg_latency_ms"`
	TopProvider    string `json:"top_provider"`
	TopWorkType    string `json:"top_work_type"`
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
		COALESCE(AVG(latency_ms), 0)
		FROM proxy_audit_log WHERE tenant_id=? AND created_at >= ?`, tenantID, cutoff)
	if err := row.Scan(&stats.TotalRequests, &stats.OKCount, &stats.ErrorCount, &stats.AvgLatencyMs); err != nil {
		return nil, err
	}

	// Top provider
	_ = r.read.QueryRow(`SELECT provider_id FROM proxy_audit_log WHERE tenant_id=? AND created_at >= ?
		GROUP BY provider_id ORDER BY COUNT(*) DESC LIMIT 1`, tenantID, cutoff).Scan(&stats.TopProvider)

	// Top work type
	_ = r.read.QueryRow(`SELECT work_type FROM proxy_audit_log WHERE tenant_id=? AND created_at >= ? AND work_type != ''
		GROUP BY work_type ORDER BY COUNT(*) DESC LIMIT 1`, tenantID, cutoff).Scan(&stats.TopWorkType)

	return &stats, nil
}

// ListRecent returns the most recent N audit log entries.
func (r *Repo) ListRecent(tenantID string, limit int) ([]*ProxyLog, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.read.Query(`SELECT id, request_id, provider_id, model, work_type, cost_tier,
		status, latency_ms, input_tokens, summary, error_msg, created_at
		FROM proxy_audit_log WHERE tenant_id=? ORDER BY created_at DESC LIMIT ?`, tenantID, limit)
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
