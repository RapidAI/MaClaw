package capabilities

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/audit"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

type SkillMarketItem struct {
	CapabilityPackage
	UsageSummary    CapabilityUsageSummary `json:"usage_summary"`
	Mature          bool                   `json:"mature"`
	MaturityReasons []string               `json:"maturity_reasons"`
}

type SkillEvolutionCandidate struct {
	SkillMarketItem
	Recommendation            string `json:"recommendation"`
	Reason                    string `json:"reason"`
	Autonomous                bool   `json:"autonomous"`
	HumanInterventionRequired bool   `json:"human_intervention_required"`
}

type skillMarketFilters struct {
	Query              string
	Origin             string
	Status             string
	PackageStatus      string
	CloudPublishStatus string
	SafetyStatus       string
	Mature             string
}

type SkillSafetyRequest struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}

type SkillEvolutionRunRequest struct {
	DryRun bool `json:"dry_run"`
	Limit  int  `json:"limit"`
}

type SkillEvolutionAutomationRule struct {
	Enabled         bool  `json:"enabled"`
	IntervalSeconds int64 `json:"interval_seconds"`
	Limit           int   `json:"limit"`
}

type skillEvolutionRunLease struct {
	Owner     string `json:"owner"`
	ExpiresAt string `json:"expires_at"`
}

type SkillEvolutionRunResult struct {
	CapabilityID   string `json:"capability_id"`
	Recommendation string `json:"recommendation"`
	Status         string `json:"status"`
	CloudSkillID   string `json:"cloud_skill_id,omitempty"`
	Error          string `json:"error,omitempty"`
}

type SkillEvolutionRunSummary struct {
	DryRun     bool                      `json:"dry_run"`
	StartedAt  string                    `json:"started_at,omitempty"`
	FinishedAt string                    `json:"finished_at,omitempty"`
	Scanned    int                       `json:"scanned"`
	Attempted  int                       `json:"attempted"`
	Published  int                       `json:"published"`
	Skipped    int                       `json:"skipped"`
	Failed     int                       `json:"failed"`
	Results    []SkillEvolutionRunResult `json:"results"`
}

type SkillEvolutionStatus struct {
	LeaseActive    bool                      `json:"lease_active"`
	LeaseOwner     string                    `json:"lease_owner,omitempty"`
	LeaseExpiresAt string                    `json:"lease_expires_at,omitempty"`
	LastRun        *SkillEvolutionRunSummary `json:"last_run,omitempty"`
}

type SkillEvolutionHistoryItem struct {
	ID           string            `json:"id"`
	RequestID    string            `json:"request_id"`
	Source       string            `json:"source"`
	Status       string            `json:"status"`
	Summary      string            `json:"summary"`
	Detail       string            `json:"detail"`
	DetailFields map[string]string `json:"detail_fields"`
	CreatedAt    string            `json:"created_at"`
}

type SkillEvolutionMetrics struct {
	Count       int            `json:"count"`
	BySource    map[string]int `json:"by_source"`
	ByStatus    map[string]int `json:"by_status"`
	SkipReasons map[string]int `json:"skip_reasons"`
	Scanned     int            `json:"scanned"`
	Attempted   int            `json:"attempted"`
	Published   int            `json:"published"`
	Skipped     int            `json:"skipped"`
	Failed      int            `json:"failed"`
}

type SkillEvolutionHealth struct {
	Level                   string                       `json:"level"`
	Reasons                 []string                     `json:"reasons"`
	RecommendedActions      []string                     `json:"recommended_actions"`
	Metrics                 SkillEvolutionMetrics        `json:"metrics"`
	AutomationRule          SkillEvolutionAutomationRule `json:"automation_rule"`
	ExpectedIntervalSeconds int64                        `json:"expected_interval_seconds"`
	LastRunAgeSeconds       int64                        `json:"last_run_age_seconds,omitempty"`
	StaleThresholdSeconds   int64                        `json:"stale_threshold_seconds,omitempty"`
	LastRun                 *SkillEvolutionHistoryItem   `json:"last_run,omitempty"`
}

func (h *Handler) handleAdminSkillMarket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	items, err := h.listInternalSkillMarket(r, tenant.RequestTenantID(r), skillMarketFilters{
		Query:              r.URL.Query().Get("q"),
		Origin:             r.URL.Query().Get("origin"),
		Status:             r.URL.Query().Get("status"),
		PackageStatus:      r.URL.Query().Get("package_status"),
		CloudPublishStatus: r.URL.Query().Get("cloud_publish_status"),
		SafetyStatus:       r.URL.Query().Get("safety_status"),
		Mature:             r.URL.Query().Get("mature"),
	})
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]any{"skills": items})
}

func (h *Handler) handleAdminSkillMarketByID(w http.ResponseWriter, r *http.Request) {
	if strings.TrimRight(r.URL.Path, "/") == "/admin/skillmarket/evolution-automation-rule" {
		h.handleAdminSkillMarketEvolutionAutomationRule(w, r)
		return
	}
	if strings.TrimRight(r.URL.Path, "/") == "/admin/skillmarket/evolution-monitor-status" {
		h.handleAdminSkillMarketEvolutionMonitorStatus(w, r)
		return
	}
	if strings.TrimRight(r.URL.Path, "/") == "/admin/skillmarket/evolution-history" {
		h.handleAdminSkillMarketEvolutionHistory(w, r)
		return
	}
	if strings.TrimRight(r.URL.Path, "/") == "/admin/skillmarket/evolution-metrics" {
		h.handleAdminSkillMarketEvolutionMetrics(w, r)
		return
	}
	if strings.TrimRight(r.URL.Path, "/") == "/admin/skillmarket/evolution-health" {
		h.handleAdminSkillMarketEvolutionHealth(w, r)
		return
	}
	if strings.TrimRight(r.URL.Path, "/") == "/admin/skillmarket/evolution-status" {
		h.handleAdminSkillMarketEvolutionStatus(w, r)
		return
	}
	if strings.TrimRight(r.URL.Path, "/") == "/admin/skillmarket/evolution-run" {
		h.handleAdminSkillMarketEvolutionRun(w, r)
		return
	}
	if strings.TrimRight(r.URL.Path, "/") == "/admin/skillmarket/evolution-candidates" {
		h.handleAdminSkillMarketEvolutionCandidates(w, r)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/safety") {
		h.handleAdminSkillMarketSafety(w, r)
		return
	}
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	id := extractID(r.URL.Path, "/admin/skillmarket/")
	if id == "" {
		response.BadRequest(w, "MISSING_ID", "skill id is required")
		return
	}
	item, err := h.getInternalSkillMarketItem(r, tenant.RequestTenantID(r), id)
	if err == sql.ErrNoRows {
		response.NotFound(w, "NOT_FOUND", "skill not found")
		return
	}
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, item)
}

func (h *Handler) handleAdminSkillMarketEvolutionAutomationRule(w http.ResponseWriter, r *http.Request) {
	tenantID := tenant.RequestTenantID(r)
	switch r.Method {
	case http.MethodGet:
		rule, err := h.GetSkillEvolutionAutomationRule(r.Context(), tenantID)
		if err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, rule)
	case http.MethodPut:
		var rule SkillEvolutionAutomationRule
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
			return
		}
		saved, err := h.SetSkillEvolutionAutomationRule(r.Context(), tenantID, rule)
		if err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, saved)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or PUT")
	}
}

func (h *Handler) handleAdminSkillMarketEvolutionHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	items, err := h.listSkillEvolutionHistory(tenant.RequestTenantID(r), limit, r.URL.Query().Get("source"), r.URL.Query().Get("status"))
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]any{"history": items})
}

func (h *Handler) handleAdminSkillMarketEvolutionMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	metrics, err := h.skillEvolutionMetrics(tenant.RequestTenantID(r), limit, r.URL.Query().Get("source"), r.URL.Query().Get("status"))
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, metrics)
}

func (h *Handler) handleAdminSkillMarketEvolutionHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	health, err := h.skillEvolutionHealth(tenant.RequestTenantID(r), limit)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, health)
}

func (h *Handler) handleAdminSkillMarketEvolutionMonitorStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	if h.skillEvolutionMonitor == nil {
		response.OK(w, SkillEvolutionMonitorStatus{})
		return
	}
	response.OK(w, h.skillEvolutionMonitor.Status())
}

func (h *Handler) handleAdminSkillMarketEvolutionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	status, err := h.GetSkillEvolutionStatus(r.Context(), tenant.RequestTenantID(r))
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, status)
}
func (h *Handler) handleAdminSkillMarketEvolutionRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	var req SkillEvolutionRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
		return
	}
	summary, err := h.RunSkillEvolution(r, tenant.RequestTenantID(r), req)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, summary)
}
func (h *Handler) handleAdminSkillMarketEvolutionCandidates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	candidates, err := h.listSkillEvolutionCandidates(r, tenant.RequestTenantID(r))
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	filtered := []SkillEvolutionCandidate{}
	readyOnly := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("ready")), "true") || r.URL.Query().Get("ready") == "1"
	recommendation := strings.TrimSpace(r.URL.Query().Get("recommendation"))
	for _, candidate := range candidates {
		if readyOnly && candidate.Recommendation != "publish_to_cloud_candidate" {
			continue
		}
		if recommendation != "" && candidate.Recommendation != recommendation {
			continue
		}
		filtered = append(filtered, candidate)
	}
	response.OK(w, map[string]any{"candidates": filtered})
}
func (h *Handler) handleAdminSkillMarketSafety(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	id := extractID(r.URL.Path, "/admin/skillmarket/")
	if id == "" {
		response.BadRequest(w, "MISSING_ID", "skill id is required")
		return
	}
	var req SkillSafetyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
		return
	}
	result, err := h.ApplySkillSafetyAction(r.Context(), tenant.RequestTenantID(r), id, req)
	if err == sql.ErrNoRows {
		response.NotFound(w, "NOT_FOUND", "skill not found")
		return
	}
	if err != nil {
		response.BadRequest(w, "SAFETY_ACTION_FAILED", err.Error())
		return
	}
	response.OK(w, result)
}

func (h *Handler) getInternalSkillMarketItem(r *http.Request, tenantID, id string) (SkillMarketItem, error) {
	var cap CapabilityPackage
	err := h.read.QueryRow(capabilitySelectSQL+" WHERE tenant_id=? AND id=?", tenantID, strings.TrimSpace(id)).Scan(&cap.ID, &cap.Name, &cap.Description, &cap.Category, &cap.Version, &cap.Source, &cap.RiskLevel, &cap.Status, &cap.PackageStatus, &cap.PackageFormat, &cap.PackageSHA256, &cap.PackageSize, &cap.LocalSkillOrigin, &cap.CloudPublishStatus, &cap.CloudSkillID, &cap.CloudPublishedAt, &cap.CloudPublishError, &cap.SafetyStatus, &cap.SafetyReason, &cap.SafetyReviewedAt, &cap.CreatedAt, &cap.UpdatedAt)
	if err != nil {
		return SkillMarketItem{}, err
	}
	summary, err := h.capabilityUsageSummary(r.Context(), tenantID, cap.ID)
	if err != nil && err != sql.ErrNoRows {
		return SkillMarketItem{}, err
	}
	rule, err := h.GetCloudPublishRule(r.Context(), tenantID)
	if err != nil {
		return SkillMarketItem{}, err
	}
	mature, reasons := capabilityMaturityForMarket(cap, summary, rule)
	return SkillMarketItem{CapabilityPackage: cap, UsageSummary: summary, Mature: mature, MaturityReasons: reasons}, nil
}

func (h *Handler) listInternalSkillMarket(r *http.Request, tenantID string, filters skillMarketFilters) ([]SkillMarketItem, error) {
	where := []string{"tenant_id=?"}
	args := []any{tenantID}
	if v := strings.TrimSpace(filters.Query); v != "" {
		where = append(where, "(name LIKE ? OR description LIKE ? OR category LIKE ?)")
		like := "%" + v + "%"
		args = append(args, like, like, like)
	}
	if v := strings.TrimSpace(filters.Origin); v != "" {
		where = append(where, "local_skill_origin=?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(filters.Status); v != "" {
		where = append(where, "status=?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(filters.PackageStatus); v != "" {
		where = append(where, "package_status=?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(filters.CloudPublishStatus); v != "" {
		where = append(where, "cloud_publish_status=?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(filters.SafetyStatus); v != "" {
		where = append(where, "safety_status=?")
		args = append(args, v)
	}

	rows, err := h.read.Query(capabilitySelectSQL+" WHERE "+strings.Join(where, " AND ")+" ORDER BY updated_at DESC, name", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rule, err := h.GetCloudPublishRule(r.Context(), tenantID)
	if err != nil {
		return nil, err
	}
	items := []SkillMarketItem{}
	for rows.Next() {
		var cap CapabilityPackage
		if err := scanCapabilityRow(rows, &cap); err != nil {
			return nil, err
		}
		summary, err := h.capabilityUsageSummary(r.Context(), tenantID, cap.ID)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		mature, reasons := capabilityMaturityForMarket(cap, summary, rule)
		if !matchesMatureFilter(filters.Mature, mature) {
			continue
		}
		items = append(items, SkillMarketItem{CapabilityPackage: cap, UsageSummary: summary, Mature: mature, MaturityReasons: reasons})
	}
	return items, rows.Err()
}

func (h *Handler) ApplySkillSafetyAction(ctx context.Context, tenantID, id string, req SkillSafetyRequest) (map[string]string, error) {
	action := strings.ToLower(strings.TrimSpace(req.Action))
	reason := strings.TrimSpace(req.Reason)
	now := time.Now().Format(time.RFC3339)
	switch action {
	case "quarantine", "block", "harmful":
		res, err := h.write.ExecContext(ctx, `UPDATE capability_packages SET status='rejected', safety_status='quarantined', safety_reason=?, safety_reviewed_at=?, cloud_publish_status='blocked', cloud_publish_error=?, updated_at=? WHERE tenant_id=? AND id=?`, reason, now, firstNonEmpty(reason, "blocked by human safety review"), now, tenantID, strings.TrimSpace(id))
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, sql.ErrNoRows
		}
		return map[string]string{"status": "quarantined"}, nil
	case "restore", "allow":
		res, err := h.write.ExecContext(ctx, `UPDATE capability_packages SET status='active', safety_status='', safety_reason='', safety_reviewed_at=?, cloud_publish_status='', cloud_publish_error='', updated_at=? WHERE tenant_id=? AND id=?`, now, now, tenantID, strings.TrimSpace(id))
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, sql.ErrNoRows
		}
		return map[string]string{"status": "restored"}, nil
	case "delete", "remove":
		return h.deleteSkillForSafety(ctx, tenantID, id)
	default:
		return nil, fmt.Errorf("unsupported safety action: %s", req.Action)
	}
}

func (h *Handler) deleteSkillForSafety(ctx context.Context, tenantID, id string) (map[string]string, error) {
	tx, err := h.write.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	_, _ = tx.ExecContext(ctx, `DELETE FROM colleague_capability_bindings WHERE tenant_id=? AND capability_id=?`, tenantID, strings.TrimSpace(id))
	_, _ = tx.ExecContext(ctx, `DELETE FROM capability_usage_events WHERE tenant_id=? AND capability_id=?`, tenantID, strings.TrimSpace(id))
	res, err := tx.ExecContext(ctx, `DELETE FROM capability_packages WHERE tenant_id=? AND id=?`, tenantID, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return map[string]string{"status": "deleted"}, nil
}

func matchesMatureFilter(filter string, mature bool) bool {
	switch strings.ToLower(strings.TrimSpace(filter)) {
	case "1", "true", "yes", "mature":
		return mature
	case "0", "false", "no", "immature":
		return !mature
	default:
		return true
	}
}

func capabilityMaturityForMarket(cap CapabilityPackage, summary CapabilityUsageSummary, rule CloudPublishRule) (bool, []string) {
	reasons := []string{}
	if strings.TrimSpace(cap.SafetyStatus) == "quarantined" {
		reasons = append(reasons, "skill_quarantined_by_human_safety_review")
	}
	if rule.RequirePackageCached && cap.PackageStatus != "package_cached" && cap.PackageStatus != "installed" {
		reasons = append(reasons, "package_not_cached")
	}
	if cap.LocalSkillOrigin == "cloud_imported" && !rule.AllowCloudImported {
		reasons = append(reasons, "cloud_imported_reupload_not_allowed")
	}
	if summary.Total < rule.MinUsageCount {
		reasons = append(reasons, "usage_below_threshold")
	}
	if summary.SuccessRate < rule.MinSuccessRate {
		reasons = append(reasons, "success_rate_below_threshold")
	}
	if summary.AverageQuality < rule.MinAverageQuality {
		reasons = append(reasons, "quality_below_threshold")
	}
	return len(reasons) == 0, reasons
}

func (h *Handler) listSkillEvolutionCandidates(r *http.Request, tenantID string) ([]SkillEvolutionCandidate, error) {
	items, err := h.listInternalSkillMarket(r, tenantID, skillMarketFilters{})
	if err != nil {
		return nil, err
	}
	rule, err := h.GetCloudPublishRule(r.Context(), tenantID)
	if err != nil {
		return nil, err
	}
	candidates := make([]SkillEvolutionCandidate, 0, len(items))
	for _, item := range items {
		candidates = append(candidates, skillEvolutionCandidateForItem(item, rule))
	}
	return candidates, nil
}

func skillEvolutionCandidateForItem(item SkillMarketItem, rule CloudPublishRule) SkillEvolutionCandidate {
	candidate := SkillEvolutionCandidate{SkillMarketItem: item}
	if strings.TrimSpace(item.SafetyStatus) == "quarantined" {
		candidate.Recommendation = "blocked_by_human_safety_review"
		candidate.Reason = firstNonEmpty(item.SafetyReason, "skill is quarantined")
		candidate.Autonomous = false
		candidate.HumanInterventionRequired = true
		return candidate
	}
	if item.CloudPublishStatus == "published" {
		candidate.Recommendation = "monitor_market_feedback"
		candidate.Reason = "already published; continue monitoring quality and market feedback"
		candidate.Autonomous = true
		return candidate
	}
	if item.Mature {
		if rule.Enabled {
			candidate.Recommendation = "publish_to_cloud_candidate"
			candidate.Reason = "maturity rule passed; eligible for autonomous cloud publishing"
			candidate.Autonomous = true
			return candidate
		}
		candidate.Recommendation = "ready_but_publish_disabled"
		candidate.Reason = "maturity rule passed, but cloud publishing rule is disabled"
		candidate.Autonomous = false
		return candidate
	}
	candidate.Recommendation = "continue_learning"
	candidate.Reason = strings.Join(item.MaturityReasons, ",")
	candidate.Autonomous = true
	return candidate
}

func (h *Handler) RunSkillEvolution(r *http.Request, tenantID string, req SkillEvolutionRunRequest) (SkillEvolutionRunSummary, error) {
	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 20
	}
	leaseOwner := ""
	if !req.DryRun {
		owner, acquired, err := h.acquireSkillEvolutionRunLease(r.Context(), tenantID, 10*time.Minute)
		if err != nil {
			return SkillEvolutionRunSummary{}, err
		}
		if !acquired {
			return SkillEvolutionRunSummary{}, fmt.Errorf("skill evolution run already in progress")
		}
		leaseOwner = owner
		defer h.releaseSkillEvolutionRunLease(r.Context(), tenantID, leaseOwner)
	}
	candidates, err := h.listSkillEvolutionCandidates(r, tenantID)
	if err != nil {
		return SkillEvolutionRunSummary{}, err
	}
	summary := SkillEvolutionRunSummary{DryRun: req.DryRun, StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Scanned: len(candidates), Results: []SkillEvolutionRunResult{}}
	for _, candidate := range candidates {
		if candidate.Recommendation != "publish_to_cloud_candidate" {
			summary.Skipped++
			continue
		}
		if summary.Attempted >= req.Limit {
			summary.Skipped++
			continue
		}
		summary.Attempted++
		result := SkillEvolutionRunResult{CapabilityID: candidate.ID, Recommendation: candidate.Recommendation}
		if req.DryRun {
			result.Status = "would_publish"
			summary.Results = append(summary.Results, result)
			continue
		}
		skill, err := h.PublishCapabilityToCloud(r.Context(), tenantID, candidate.ID, CloudPublishRequest{})
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			summary.Failed++
			summary.Results = append(summary.Results, result)
			continue
		}
		result.Status = "published"
		result.CloudSkillID = skill.ID
		summary.Published++
		summary.Results = append(summary.Results, result)
	}
	summary.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if !req.DryRun {
		_ = h.storeLastSkillEvolutionRun(r.Context(), tenantID, summary)
		h.recordSkillEvolutionRunAudit(tenantID, summary)
	}
	return summary, nil
}

func (h *Handler) listSkillEvolutionHistory(tenantID string, limit int, sourceFilter, statusFilter string) ([]SkillEvolutionHistoryItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if h.audit == nil {
		return []SkillEvolutionHistoryItem{}, nil
	}
	logs, err := h.audit.ListRecent(tenantID, limit*4)
	if err != nil {
		return nil, err
	}
	items := make([]SkillEvolutionHistoryItem, 0, limit)
	for _, log := range logs {
		if log == nil || !isSkillEvolutionWorkType(log.WorkType) {
			continue
		}
		source := skillEvolutionSource(log.WorkType)
		if !matchesOptionalFilter(sourceFilter, source) || !matchesOptionalFilter(statusFilter, log.Status) {
			continue
		}
		items = append(items, SkillEvolutionHistoryItem{ID: log.ID, RequestID: log.RequestID, Source: source, Status: log.Status, Summary: log.Summary, Detail: log.ErrorMsg, DetailFields: parseAuditDetailFields(log.ErrorMsg), CreatedAt: log.CreatedAt.UTC().Format(time.RFC3339)})
		if len(items) >= limit {
			break
		}
	}
	return items, nil
}

func (h *Handler) skillEvolutionMetrics(tenantID string, limit int, sourceFilter, statusFilter string) (SkillEvolutionMetrics, error) {
	items, err := h.listSkillEvolutionHistory(tenantID, limit, sourceFilter, statusFilter)
	if err != nil {
		return SkillEvolutionMetrics{}, err
	}
	metrics := SkillEvolutionMetrics{Count: len(items), BySource: map[string]int{}, ByStatus: map[string]int{}, SkipReasons: map[string]int{}}
	for _, item := range items {
		metrics.BySource[item.Source]++
		metrics.ByStatus[item.Status]++
		metrics.Scanned += detailFieldInt(item.DetailFields, "scanned")
		metrics.Attempted += detailFieldInt(item.DetailFields, "attempted")
		metrics.Published += detailFieldInt(item.DetailFields, "published")
		metrics.Skipped += detailFieldInt(item.DetailFields, "skipped")
		metrics.Failed += detailFieldInt(item.DetailFields, "failed")
		if reason := strings.TrimSpace(item.DetailFields["skip_reason"]); reason != "" {
			metrics.SkipReasons[reason]++
		}
	}
	return metrics, nil
}

func (h *Handler) skillEvolutionHealth(tenantID string, limit int) (SkillEvolutionHealth, error) {
	rule, err := h.GetSkillEvolutionAutomationRule(context.Background(), tenantID)
	if err != nil {
		return SkillEvolutionHealth{}, err
	}
	items, err := h.listSkillEvolutionHistory(tenantID, limit, "", "")
	if err != nil {
		return SkillEvolutionHealth{}, err
	}
	metrics, err := h.skillEvolutionMetrics(tenantID, limit, "", "")
	if err != nil {
		return SkillEvolutionHealth{}, err
	}
	health := SkillEvolutionHealth{Level: "healthy", Reasons: []string{}, RecommendedActions: []string{}, Metrics: metrics, AutomationRule: rule, ExpectedIntervalSeconds: rule.IntervalSeconds}
	if len(items) > 0 {
		health.LastRun = &items[0]
		if createdAt, err := time.Parse(time.RFC3339, items[0].CreatedAt); err == nil {
			age := int64(time.Since(createdAt).Seconds())
			if age < 0 {
				age = 0
			}
			health.LastRunAgeSeconds = age
		}
	}
	if !rule.Enabled {
		health.Level = "warning"
		health.Reasons = append(health.Reasons, "skill_evolution_automation_disabled")
		health.RecommendedActions = append(health.RecommendedActions, "enable_evolution_automation_rule_when_ready")
	}
	if metrics.Count == 0 {
		if health.Level == "healthy" {
			health.Level = "warning"
		}
		health.Reasons = append(health.Reasons, "no_recent_skill_evolution_history")
		health.RecommendedActions = append(health.RecommendedActions, "check_evolution_automation_rule_and_monitor_status")
		return health, nil
	}
	if rule.Enabled && health.LastRunAgeSeconds > 0 {
		threshold := skillEvolutionStaleThreshold(rule.IntervalSeconds)
		health.StaleThresholdSeconds = threshold
		if health.LastRunAgeSeconds > threshold {
			if health.LastRunAgeSeconds > threshold*3 {
				health.Level = "critical"
			} else if health.Level == "healthy" {
				health.Level = "warning"
			}
			health.Reasons = append(health.Reasons, "skill_evolution_stale")
			health.RecommendedActions = append(health.RecommendedActions, "check_skill_evolution_monitor_scheduler_and_lease_status")
		}
	}
	if metrics.Failed > 0 || metrics.ByStatus["error"] > 0 {
		health.Level = "critical"
		health.Reasons = append(health.Reasons, "skill_evolution_failures_detected")
		health.RecommendedActions = append(health.RecommendedActions, "inspect_evolution_history_errors_and_cloud_publish_configuration")
	}
	if metrics.Published == 0 && metrics.SkipReasons["interval_not_reached"] >= metrics.Count {
		if health.Level == "healthy" {
			health.Level = "warning"
		}
		health.Reasons = append(health.Reasons, "all_recent_runs_skipped_by_interval")
		health.RecommendedActions = append(health.RecommendedActions, "wait_for_next_interval_or_adjust_evolution_automation_rule")
	}
	if metrics.Published == 0 && metrics.SkipReasons["automation_disabled"] >= metrics.Count && !containsSkillEvolutionReason(health.Reasons, "skill_evolution_automation_disabled") {
		if health.Level == "healthy" {
			health.Level = "warning"
		}
		health.Reasons = append(health.Reasons, "skill_evolution_automation_disabled")
		health.RecommendedActions = append(health.RecommendedActions, "enable_evolution_automation_rule_when_ready")
	}
	return health, nil
}

func skillEvolutionStaleThreshold(intervalSeconds int64) int64 {
	if intervalSeconds <= 0 {
		intervalSeconds = int64(DefaultSkillEvolutionInterval.Seconds())
	}
	threshold := intervalSeconds * 3
	minimum := intervalSeconds + 300
	if threshold < minimum {
		threshold = minimum
	}
	return threshold
}

func containsSkillEvolutionReason(reasons []string, target string) bool {
	for _, reason := range reasons {
		if reason == target {
			return true
		}
	}
	return false
}

func (h *Handler) recordSkillEvolutionRunAudit(tenantID string, summary SkillEvolutionRunSummary) {
	if h == nil || h.audit == nil || strings.TrimSpace(tenantID) == "" {
		return
	}
	status := "ok"
	if summary.Failed > 0 {
		status = "error"
	}
	detail := fmt.Sprintf("tenant=%s dry_run=%v scanned=%d attempted=%d published=%d skipped=%d failed=%d", tenantID, summary.DryRun, summary.Scanned, summary.Attempted, summary.Published, summary.Skipped, summary.Failed)
	_ = h.audit.Insert(tenantID, &audit.ProxyLog{RequestID: fmt.Sprintf("skill-evolution-run-%s-%d", tenantID, time.Now().UnixNano()), ProviderID: "iworkercenter", Model: "skill-evolution-run", WorkType: "skill_evolution_run", CostTier: "internal", Status: status, Summary: "skill evolution run", ErrorMsg: detail, CreatedAt: time.Now().UTC()})
}

func parseAuditDetailFields(detail string) map[string]string {
	fields := map[string]string{}
	for _, part := range strings.Fields(detail) {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		fields[key] = strings.TrimSpace(value)
	}
	return fields
}

func detailFieldInt(fields map[string]string, key string) int {
	if fields == nil {
		return 0
	}
	value, err := strconv.Atoi(strings.TrimSpace(fields[key]))
	if err != nil {
		return 0
	}
	return value
}

func matchesOptionalFilter(filter, value string) bool {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return true
	}
	return strings.EqualFold(filter, strings.TrimSpace(value))
}

func isSkillEvolutionWorkType(workType string) bool {
	switch strings.TrimSpace(workType) {
	case "skill_evolution_monitor", "skill_evolution_run":
		return true
	default:
		return false
	}
}

func skillEvolutionSource(workType string) string {
	switch strings.TrimSpace(workType) {
	case "skill_evolution_monitor":
		return "monitor"
	case "skill_evolution_run":
		return "manual_run"
	default:
		return "unknown"
	}
}

func (h *Handler) GetSkillEvolutionStatus(ctx context.Context, tenantID string) (SkillEvolutionStatus, error) {
	status := SkillEvolutionStatus{}
	var raw string
	err := h.read.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key=?`, skillEvolutionRunLeaseKey(tenantID)).Scan(&raw)
	if err != nil && err != sql.ErrNoRows {
		return status, err
	}
	if err == nil && strings.TrimSpace(raw) != "" {
		var lease skillEvolutionRunLease
		if json.Unmarshal([]byte(raw), &lease) == nil {
			status.LeaseOwner = lease.Owner
			status.LeaseExpiresAt = lease.ExpiresAt
			if expires, err := time.Parse(time.RFC3339Nano, lease.ExpiresAt); err == nil && expires.After(time.Now().UTC()) {
				status.LeaseActive = true
			}
		}
	}
	last, err := h.lastSkillEvolutionRun(ctx, tenantID)
	if err != nil {
		return status, err
	}
	status.LastRun = last
	return status, nil
}

func (h *Handler) storeLastSkillEvolutionRun(ctx context.Context, tenantID string, summary SkillEvolutionRunSummary) error {
	data, _ := json.Marshal(summary)
	key := skillEvolutionLastRunKey(tenantID)
	res, err := h.write.ExecContext(ctx, `UPDATE system_settings SET value_json=?, updated_at=datetime('now') WHERE key=?`, string(data), key)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = h.write.ExecContext(ctx, `INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, datetime('now'))`, key, string(data))
	return err
}

func (h *Handler) lastSkillEvolutionRun(ctx context.Context, tenantID string) (*SkillEvolutionRunSummary, error) {
	var raw string
	err := h.read.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key=?`, skillEvolutionLastRunKey(tenantID)).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var summary SkillEvolutionRunSummary
	if err := json.Unmarshal([]byte(raw), &summary); err != nil {
		return nil, nil
	}
	return &summary, nil
}

func (h *Handler) acquireSkillEvolutionRunLease(ctx context.Context, tenantID string, ttl time.Duration) (string, bool, error) {
	owner := fmt.Sprintf("skill-evolution-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	expiresAt := now.Add(ttl).Format(time.RFC3339Nano)
	key := skillEvolutionRunLeaseKey(tenantID)
	tx, err := h.write.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key=?`, key).Scan(&raw)
	if err == sql.ErrNoRows {
		data, _ := json.Marshal(skillEvolutionRunLease{Owner: owner, ExpiresAt: expiresAt})
		if _, err := tx.ExecContext(ctx, `INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, datetime('now'))`, key, string(data)); err != nil {
			return "", false, err
		}
		if err := tx.Commit(); err != nil {
			return "", false, err
		}
		return owner, true, nil
	}
	if err != nil {
		return "", false, err
	}
	var current skillEvolutionRunLease
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &current)
	}
	if expires, err := time.Parse(time.RFC3339Nano, current.ExpiresAt); err == nil && expires.After(now) {
		return "", false, nil
	}
	data, _ := json.Marshal(skillEvolutionRunLease{Owner: owner, ExpiresAt: expiresAt})
	if _, err := tx.ExecContext(ctx, `UPDATE system_settings SET value_json=?, updated_at=datetime('now') WHERE key=?`, string(data), key); err != nil {
		return "", false, err
	}
	if err := tx.Commit(); err != nil {
		return "", false, err
	}
	return owner, true, nil
}

func (h *Handler) releaseSkillEvolutionRunLease(ctx context.Context, tenantID, owner string) {
	if strings.TrimSpace(owner) == "" {
		return
	}
	key := skillEvolutionRunLeaseKey(tenantID)
	var raw string
	if err := h.read.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key=?`, key).Scan(&raw); err != nil {
		return
	}
	var current skillEvolutionRunLease
	if err := json.Unmarshal([]byte(raw), &current); err != nil || current.Owner != owner {
		return
	}
	data, _ := json.Marshal(skillEvolutionRunLease{Owner: owner, ExpiresAt: time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano)})
	_, _ = h.write.ExecContext(ctx, `UPDATE system_settings SET value_json=?, updated_at=datetime('now') WHERE key=?`, string(data), key)
}

func defaultSkillEvolutionAutomationRule() SkillEvolutionAutomationRule {
	return SkillEvolutionAutomationRule{Enabled: true, IntervalSeconds: int64(DefaultSkillEvolutionInterval.Seconds()), Limit: 20}
}

func (h *Handler) GetSkillEvolutionAutomationRule(ctx context.Context, tenantID string) (SkillEvolutionAutomationRule, error) {
	rule := defaultSkillEvolutionAutomationRule()
	var raw string
	err := h.read.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key=?`, skillEvolutionAutomationRuleKey(tenantID)).Scan(&raw)
	if err == sql.ErrNoRows {
		return rule, nil
	}
	if err != nil {
		return rule, err
	}
	if strings.TrimSpace(raw) == "" {
		return rule, nil
	}
	if err := json.Unmarshal([]byte(raw), &rule); err != nil {
		return defaultSkillEvolutionAutomationRule(), nil
	}
	return normalizeSkillEvolutionAutomationRule(rule), nil
}

func (h *Handler) SetSkillEvolutionAutomationRule(ctx context.Context, tenantID string, rule SkillEvolutionAutomationRule) (SkillEvolutionAutomationRule, error) {
	rule = normalizeSkillEvolutionAutomationRule(rule)
	data, _ := json.Marshal(rule)
	key := skillEvolutionAutomationRuleKey(tenantID)
	res, err := h.write.ExecContext(ctx, `UPDATE system_settings SET value_json=?, updated_at=datetime('now') WHERE key=?`, string(data), key)
	if err != nil {
		return rule, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return rule, nil
	}
	_, err = h.write.ExecContext(ctx, `INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, datetime('now'))`, key, string(data))
	return rule, err
}

func normalizeSkillEvolutionAutomationRule(rule SkillEvolutionAutomationRule) SkillEvolutionAutomationRule {
	if rule.IntervalSeconds <= 0 {
		rule.IntervalSeconds = int64(DefaultSkillEvolutionInterval.Seconds())
	}
	if rule.IntervalSeconds < 60 {
		rule.IntervalSeconds = 60
	}
	if rule.Limit <= 0 || rule.Limit > 100 {
		rule.Limit = 20
	}
	return rule
}

func skillEvolutionAutomationRuleKey(tenantID string) string {
	return "skill_evolution_automation_rule:" + strings.TrimSpace(tenantID)
}

func skillEvolutionRunLeaseKey(tenantID string) string {
	return "skill_evolution_run_lease:" + strings.TrimSpace(tenantID)
}

func skillEvolutionLastRunKey(tenantID string) string {
	return "skill_evolution_last_run:" + strings.TrimSpace(tenantID)
}
