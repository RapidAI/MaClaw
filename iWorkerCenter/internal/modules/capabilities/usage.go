package capabilities

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
)

type CapabilityUsageSummary struct {
	CapabilityID     string  `json:"capability_id"`
	Total            int     `json:"total"`
	Successes        int     `json:"successes"`
	Failures         int     `json:"failures"`
	SuccessRate      float64 `json:"success_rate"`
	AverageQuality   float64 `json:"average_quality"`
	AverageLatencyMs float64 `json:"average_latency_ms"`
}

type CapabilityUsageEvent struct {
	ID                     string `json:"id"`
	CapabilityID           string `json:"capability_id"`
	ColleagueID            string `json:"colleague_id"`
	WorkflowInstanceID     string `json:"workflow_instance_id"`
	WorkflowStepInstanceID string `json:"workflow_step_instance_id"`
	Status                 string `json:"status"`
	ResultSummary          string `json:"result_summary"`
	ErrorMessage           string `json:"error_message"`
	LatencyMs              int64  `json:"latency_ms"`
	QualityScore           int    `json:"quality_score"`
	QualityReason          string `json:"quality_reason"`
	CreatedAt              string `json:"created_at"`
}

func (h *Handler) RecordCapabilityUsage(ctx context.Context, tenantID, capabilityID, colleagueID, workflowInstanceID, workflowStepInstanceID, status, resultSummary, errorMessage string, latencyMs int64, qualityScore int, qualityReason string) error {
	capabilityID = strings.TrimSpace(capabilityID)
	if capabilityID == "" {
		return nil
	}
	status = normalizeUsageStatus(status, errorMessage)
	if latencyMs < 0 {
		latencyMs = 0
	}
	qualityScore, qualityReason = normalizeQualitySignal(status, resultSummary, errorMessage, qualityScore, qualityReason)
	now := time.Now().Format(time.RFC3339)
	res, err := h.write.ExecContext(ctx, `INSERT INTO capability_usage_events (
		tenant_id, id, capability_id, colleague_id, workflow_instance_id, workflow_step_instance_id,
		status, result_summary, error_message, latency_ms, quality_score, quality_reason, created_at
	)
	SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
	WHERE EXISTS (SELECT 1 FROM capability_packages WHERE tenant_id=? AND id=?)`,
		tenantID, idgen.New("capuse"), capabilityID, strings.TrimSpace(colleagueID), strings.TrimSpace(workflowInstanceID), strings.TrimSpace(workflowStepInstanceID),
		status, strings.TrimSpace(resultSummary), strings.TrimSpace(errorMessage), latencyMs, qualityScore, strings.TrimSpace(qualityReason), now,
		tenantID, capabilityID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (h *Handler) capabilityUsageSummary(ctx context.Context, tenantID, capabilityID string) (CapabilityUsageSummary, error) {
	capabilityID = strings.TrimSpace(capabilityID)
	var summary CapabilityUsageSummary
	summary.CapabilityID = capabilityID
	var qualityTotal sql.NullFloat64
	var latencyTotal sql.NullFloat64
	err := h.read.QueryRowContext(ctx, `SELECT
		COUNT(*),
		COALESCE(SUM(CASE WHEN status='success' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status='failure' THEN 1 ELSE 0 END), 0),
		AVG(CASE WHEN quality_score > 0 THEN quality_score END),
		AVG(CASE WHEN latency_ms > 0 THEN latency_ms END)
		FROM capability_usage_events WHERE tenant_id=? AND capability_id=?`, tenantID, capabilityID).Scan(&summary.Total, &summary.Successes, &summary.Failures, &qualityTotal, &latencyTotal)
	if err != nil {
		return summary, err
	}
	if summary.Total > 0 {
		summary.SuccessRate = float64(summary.Successes) / float64(summary.Total)
	}
	if qualityTotal.Valid {
		summary.AverageQuality = qualityTotal.Float64
	}
	if latencyTotal.Valid {
		summary.AverageLatencyMs = latencyTotal.Float64
	}
	return summary, nil
}

func (h *Handler) UpdateCapabilityUsageQuality(ctx context.Context, tenantID, capabilityID, eventID string, qualityScore int, qualityReason string) error {
	qualityScore, qualityReason = normalizeQualitySignal("success", "", "", qualityScore, qualityReason)
	res, err := h.write.ExecContext(ctx, `UPDATE capability_usage_events SET quality_score=?, quality_reason=? WHERE tenant_id=? AND capability_id=? AND id=?`, qualityScore, strings.TrimSpace(qualityReason), tenantID, strings.TrimSpace(capabilityID), strings.TrimSpace(eventID))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (h *Handler) listCapabilityUsageEvents(ctx context.Context, tenantID, capabilityID string, limit int) ([]CapabilityUsageEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := h.read.QueryContext(ctx, `SELECT id, capability_id, colleague_id, workflow_instance_id, workflow_step_instance_id, status, result_summary, error_message, latency_ms, quality_score, quality_reason, created_at
		FROM capability_usage_events
		WHERE tenant_id=? AND capability_id=?
		ORDER BY created_at DESC
		LIMIT ?`, tenantID, strings.TrimSpace(capabilityID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CapabilityUsageEvent{}
	for rows.Next() {
		var item CapabilityUsageEvent
		if err := rows.Scan(&item.ID, &item.CapabilityID, &item.ColleagueID, &item.WorkflowInstanceID, &item.WorkflowStepInstanceID, &item.Status, &item.ResultSummary, &item.ErrorMessage, &item.LatencyMs, &item.QualityScore, &item.QualityReason, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (h *Handler) capabilityUsageScore(ctx context.Context, tenantID, capabilityID, colleagueID string) int {
	rows, err := h.read.QueryContext(ctx, `SELECT status, quality_score FROM capability_usage_events
		WHERE tenant_id=? AND capability_id=? AND colleague_id=?
		ORDER BY created_at DESC LIMIT 20`, tenantID, capabilityID, colleagueID)
	if err != nil {
		return 0
	}
	defer rows.Close()
	successes := 0
	failures := 0
	qualityTotal := 0
	qualityCount := 0
	count := 0
	for rows.Next() {
		var status string
		var qualityScore int
		if err := rows.Scan(&status, &qualityScore); err != nil {
			continue
		}
		count++
		if qualityScore > 0 {
			qualityTotal += qualityScore
			qualityCount++
		}
		switch strings.ToLower(strings.TrimSpace(status)) {
		case "success":
			successes++
		case "failure":
			failures++
		}
	}
	if count == 0 {
		return 0
	}
	if failures >= 3 && failures > successes {
		return -4
	}
	if qualityCount > 0 {
		avg := qualityTotal / qualityCount
		if avg >= 85 {
			return 4
		}
		if avg < 50 {
			return -3
		}
	}
	if successes >= 3 && successes >= failures*2 {
		return 3
	}
	if successes > failures {
		return 1
	}
	return 0
}

func normalizeQualitySignal(status, resultSummary, errorMessage string, qualityScore int, qualityReason string) (int, string) {
	if qualityScore < 0 {
		qualityScore = 0
	}
	if qualityScore > 100 {
		qualityScore = 100
	}
	qualityReason = strings.TrimSpace(qualityReason)
	if qualityScore > 0 {
		if qualityReason == "" {
			qualityReason = "reported_by_executor"
		}
		return qualityScore, qualityReason
	}
	if normalizeUsageStatus(status, errorMessage) == "failure" {
		if qualityReason == "" {
			qualityReason = "derived_from_failure"
		}
		return 20, qualityReason
	}
	if strings.TrimSpace(resultSummary) == "" {
		if qualityReason == "" {
			qualityReason = "success_without_result_summary"
		}
		return 60, qualityReason
	}
	if qualityReason == "" {
		qualityReason = "derived_from_successful_completion"
	}
	return 80, qualityReason
}

func normalizeUsageStatus(status string, errorMessage string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "success", "succeeded", "ok", "completed":
		return "success"
	case "failure", "failed", "error":
		return "failure"
	}
	if strings.TrimSpace(errorMessage) != "" {
		return "failure"
	}
	return "success"
}
