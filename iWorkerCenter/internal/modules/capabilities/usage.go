package capabilities

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
)

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
	CreatedAt              string `json:"created_at"`
}

func (h *Handler) RecordCapabilityUsage(ctx context.Context, tenantID, capabilityID, colleagueID, workflowInstanceID, workflowStepInstanceID, status, resultSummary, errorMessage string, latencyMs int64) error {
	capabilityID = strings.TrimSpace(capabilityID)
	if capabilityID == "" {
		return nil
	}
	status = normalizeUsageStatus(status, errorMessage)
	if latencyMs < 0 {
		latencyMs = 0
	}
	now := time.Now().Format(time.RFC3339)
	res, err := h.write.ExecContext(ctx, `INSERT INTO capability_usage_events (
		tenant_id, id, capability_id, colleague_id, workflow_instance_id, workflow_step_instance_id,
		status, result_summary, error_message, latency_ms, created_at
	)
	SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
	WHERE EXISTS (SELECT 1 FROM capability_packages WHERE tenant_id=? AND id=?)`,
		tenantID, idgen.New("capuse"), capabilityID, strings.TrimSpace(colleagueID), strings.TrimSpace(workflowInstanceID), strings.TrimSpace(workflowStepInstanceID),
		status, strings.TrimSpace(resultSummary), strings.TrimSpace(errorMessage), latencyMs, now,
		tenantID, capabilityID)
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
	rows, err := h.read.QueryContext(ctx, `SELECT id, capability_id, colleague_id, workflow_instance_id, workflow_step_instance_id, status, result_summary, error_message, latency_ms, created_at
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
		if err := rows.Scan(&item.ID, &item.CapabilityID, &item.ColleagueID, &item.WorkflowInstanceID, &item.WorkflowStepInstanceID, &item.Status, &item.ResultSummary, &item.ErrorMessage, &item.LatencyMs, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (h *Handler) capabilityUsageScore(ctx context.Context, tenantID, capabilityID, colleagueID string) int {
	rows, err := h.read.QueryContext(ctx, `SELECT status FROM capability_usage_events
		WHERE tenant_id=? AND capability_id=? AND colleague_id=?
		ORDER BY created_at DESC LIMIT 20`, tenantID, capabilityID, colleagueID)
	if err != nil {
		return 0
	}
	defer rows.Close()
	successes := 0
	failures := 0
	count := 0
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			continue
		}
		count++
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
	if successes >= 3 && successes >= failures*2 {
		return 3
	}
	if successes > failures {
		return 1
	}
	return 0
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
