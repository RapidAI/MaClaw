package executive

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/audit"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

// Handler provides a lightweight executive console API for iWorkerCenter.
type Handler struct {
	read  *sql.DB
	audit *audit.Repo
}

func NewHandler(read *sql.DB, auditRepo *audit.Repo) *Handler {
	return &Handler{read: read, audit: auditRepo}
}

func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/executive/overview", h.handleOverview)
	mux.HandleFunc("/admin/executive/skills", h.handleSkills)
	mux.HandleFunc("/admin/executive/skills/run", h.handleRunSkill)
	mux.HandleFunc("/admin/executive/management-decisions", h.handleRecordManagementDecision)
}

type metric struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Hint  string `json:"hint"`
}

type item struct {
	Title          string `json:"title"`
	Description    string `json:"description"`
	Status         string `json:"status"`
	RoleCode       string `json:"role_code,omitempty"`
	RoleLabel      string `json:"role_label,omitempty"`
	SignalPriority int    `json:"signal_priority"`
}

type action struct {
	Title            string `json:"title"`
	Owner            string `json:"owner"`
	OwnerRoleCode    string `json:"owner_role_code"`
	OwnerRoleLabel   string `json:"owner_role_label"`
	Description      string `json:"description"`
	LinkedTaskID     string `json:"linked_task_id,omitempty"`
	LinkedTaskStatus string `json:"linked_task_status,omitempty"`
	LinkedTaskResult string `json:"linked_task_result,omitempty"`
}

type boardFocus struct {
	Title          string `json:"title"`
	Summary        string `json:"summary"`
	Description    string `json:"description"`
	Status         string `json:"status"`
	RoleCode       string `json:"role_code,omitempty"`
	RoleLabel      string `json:"role_label,omitempty"`
	SignalPriority int    `json:"signal_priority"`
}

type historyNavigationTarget struct {
	TaskID   string `json:"task_id,omitempty"`
	RoleCode string `json:"role_code,omitempty"`
	Source   string `json:"source,omitempty"`
}

type boardHistoryItem struct {
	ID                     string                   `json:"id"`
	Title                  string                   `json:"title"`
	Detail                 string                   `json:"detail"`
	ClusterTaskID          string                   `json:"clusterTaskId,omitempty"`
	Timestamp              string                   `json:"timestamp"`
	Tone                   string                   `json:"tone"`
	NavigationTarget       *historyNavigationTarget `json:"navigationTarget,omitempty"`
	DetailLines            []string                 `json:"detailLines,omitempty"`
	IsCluster              bool                     `json:"isCluster,omitempty"`
	ClusterSkillTitle      string                   `json:"clusterSkillTitle,omitempty"`
	ClusterFocusTitle      string                   `json:"clusterFocusTitle,omitempty"`
	ClusterTaskTitle       string                   `json:"clusterTaskTitle,omitempty"`
	ClusterRoleCode        string                   `json:"clusterRoleCode,omitempty"`
	ClusterExecutionStatus string                   `json:"clusterExecutionStatus,omitempty"`
	ClusterExecutionResult string                   `json:"clusterExecutionResult,omitempty"`
}

type executiveAuditCluster struct {
	ID         string
	SkillID    string
	SkillTitle string
	FocusTitle string
	TaskID     string
	TaskTitle  string
	RoleCode   string
	Timestamp  time.Time
	Tone       string
	HasSkill   bool
	HasTask    bool
}

type historyTaskSnapshot struct {
	ID         string
	Title      string
	Status     string
	Result     string
	ToRoleCode string
	CreatedAt  string
	UpdatedAt  string
}

type skill struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Question    string `json:"question"`
	Description string `json:"description"`
}

type skillResult struct {
	SkillID         string     `json:"skill_id"`
	Title           string     `json:"title"`
	Summary         string     `json:"summary"`
	Focus           boardFocus `json:"focus"`
	Findings        []string   `json:"findings"`
	Recommendations []action   `json:"recommendations"`
}

type overviewStats struct {
	ActiveColleagues int
	Roles            int
	ActiveMemories   int
	Capabilities     int
	WorkflowDefs     int
	RunningWorkflows int
	PendingTasks     int
	ActiveTasks      int
	CompletedTasks   int
}

func (h *Handler) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}

	stats, err := h.collectOverviewStats(r.Context())
	if err != nil {
		response.Internal(w, err.Error())
		return
	}

	history, err := h.collectBoardHistory(r.Context(), 6)
	if err != nil {
		history = nil
	}

	response.OK(w, buildOverviewPayload(stats, history))
}

func (h *Handler) handleSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}

	response.OK(w, map[string]any{"skills": executiveSkills})
}

func (h *Handler) handleRunSkill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}

	var req struct {
		SkillID string `json:"skill_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
		return
	}

	stats, err := h.collectOverviewStats(r.Context())
	if err != nil {
		response.Internal(w, err.Error())
		return
	}

	skillID := strings.TrimSpace(req.SkillID)
	result, ok := buildSkillResult(skillID, stats)
	if !ok {
		response.NotFound(w, "SKILL_NOT_FOUND", "executive skill not found")
		return
	}

	h.recordSkillAudit(r.Context(), skillID, result)
	response.OK(w, result)
}

func (h *Handler) handleRecordManagementDecision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}

	var req struct {
		RoleCode     string `json:"role_code"`
		DecisionType string `json:"decision_type"`
		Detail       string `json:"detail"`
		DisplayTime  string `json:"display_time"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
		return
	}

	req.RoleCode = strings.TrimSpace(req.RoleCode)
	req.DecisionType = strings.TrimSpace(req.DecisionType)
	req.Detail = strings.TrimSpace(req.Detail)
	req.DisplayTime = strings.TrimSpace(req.DisplayTime)
	if req.RoleCode == "" || req.Detail == "" || req.DisplayTime == "" {
		response.BadRequest(w, "INVALID_MANAGEMENT_DECISION", "role_code, detail, and display_time are required")
		return
	}
	if req.DecisionType != "review" && req.DecisionType != "deferred" {
		response.BadRequest(w, "INVALID_MANAGEMENT_DECISION", "decision_type must be review or deferred")
		return
	}

	if err := h.recordManagementDecisionAudit(r.Context(), req.RoleCode, req.DecisionType, req.Detail, req.DisplayTime); err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]any{"ok": true})
}

func (h *Handler) recordManagementDecisionAudit(ctx context.Context, roleCode, decisionType, detail, displayTime string) error {
	if h.audit == nil {
		return nil
	}
	tenantID := tenant.TenantIDFromContext(ctx)
	if tenantID == "" {
		return nil
	}
	roleCode = strings.TrimSpace(roleCode)
	decisionType = strings.TrimSpace(decisionType)
	detail = strings.TrimSpace(detail)
	displayTime = strings.TrimSpace(displayTime)
	summary := fmt.Sprintf("Management review opened for %s", strings.ToUpper(roleCode))
	if decisionType == "deferred" {
		summary = fmt.Sprintf("Management follow-up deferred for %s", strings.ToUpper(roleCode))
	}
	return h.audit.Insert(tenantID, &audit.ProxyLog{
		RequestID:   fmt.Sprintf("management-decision-%s-%d", roleCode, time.Now().UnixNano()),
		ProviderID:  "iworkercenter",
		Model:       "management-decision",
		WorkType:    "management_decision",
		CostTier:    "internal",
		Status:      "ok",
		LatencyMs:   0,
		InputTokens: 0,
		Summary:     summary,
		ErrorMsg:    fmt.Sprintf("decision_type: %s | role_code: %s | detail: %s | display_time: %s", decisionType, roleCode, detail, displayTime),
	})
}

func (h *Handler) recordSkillAudit(ctx context.Context, skillID string, result skillResult) {
	if h.audit == nil {
		return
	}
	tenantID := tenant.TenantIDFromContext(ctx)
	if tenantID == "" {
		return
	}
	summary := fmt.Sprintf("Executive skill %s reviewed", result.Title)
	detail := fmt.Sprintf("focus: %s | role_code: %s | role: %s | summary: %s", result.Focus.Title, result.Focus.RoleCode, result.Focus.RoleLabel, result.Focus.Summary)
	_ = h.audit.Insert(tenantID, &audit.ProxyLog{
		RequestID:   fmt.Sprintf("executive-skill-%s", skillID),
		ProviderID:  "iworkercenter",
		Model:       "executive-skill",
		WorkType:    "executive_skill",
		CostTier:    "internal",
		Status:      "ok",
		LatencyMs:   0,
		InputTokens: 0,
		Summary:     summary,
		ErrorMsg:    detail,
	})
}

func buildOverviewPayload(stats overviewStats, history []boardHistoryItem) map[string]any {
	reuseRatio := calculateReuseRatio(stats)
	decisionLatency := 2.0 + float64(stats.PendingTasks)/5.0
	now := time.Now().Format("2006-01-02 15:04")

	boardSummary := buildBoardSummary(stats, reuseRatio)
	boardFocus := buildBoardFocus(stats, reuseRatio)
	priorityDecision := buildPriorityDecision(stats, reuseRatio)
	prioritySummary := buildPrioritySummary(stats, reuseRatio)

	boardSummary = deriveDecisionBoardSummary(boardSummary, history)
	boardFocus = deriveDecisionBoardFocus(boardFocus, history)
	priorityDecision = deriveDecisionBoardFocus(priorityDecision, history)
	prioritySummary = deriveDecisionBoardSummary(prioritySummary, history)

	return map[string]any{
		"briefing": item{
			Title:       "Today's operating brief",
			Description: fmt.Sprintf("iWorkerCenter is managing %d active digital employees, %d workflow templates, and %d pending cross-role tasks. The immediate focus is whether organizational know-how is being deposited into reusable center assets instead of remaining in people-dependent execution.", stats.ActiveColleagues, stats.WorkflowDefs, stats.PendingTasks),
			Status:      overviewStatus(stats),
		},
		"metrics": []metric{
			{Label: "Digital workforce", Value: fmt.Sprintf("%d", stats.ActiveColleagues), Hint: fmt.Sprintf("%d active roles available for orchestration", stats.Roles)},
			{Label: "Active workflows", Value: fmt.Sprintf("%d", stats.RunningWorkflows), Hint: fmt.Sprintf("%d templates are configured in the center", stats.WorkflowDefs)},
			{Label: "Knowledge reuse", Value: fmt.Sprintf("%d%%", reuseRatio), Hint: fmt.Sprintf("%d active memories and %d capability packages", stats.ActiveMemories, stats.Capabilities)},
			{Label: "Decision latency", Value: fmt.Sprintf("%.1fh", decisionLatency), Hint: fmt.Sprintf("%d pending collaboration tasks are waiting for movement", stats.PendingTasks)},
		},
		"board_summary":     boardSummary,
		"board_focus":       boardFocus,
		"priority_decision": priorityDecision,
		"priority_summary":  prioritySummary,
		"board_signals":     buildBoardSignals(stats, reuseRatio),
		"board_history":     history,
		"risks":             buildRisks(stats, reuseRatio),
		"actions":           deriveDecisionActions(buildActions(stats, reuseRatio), history),
		"updated_at":        now,
	}
}

func pickPriorityHistoryCluster(history []boardHistoryItem) *boardHistoryItem {
	for i := range history {
		if history[i].IsCluster {
			return &history[i]
		}
	}
	return nil
}

func pickLatestManagementDecision(history []boardHistoryItem) *boardHistoryItem {
	for i := range history {
		if strings.HasPrefix(history[i].ID, "management-") {
			return &history[i]
		}
	}
	return nil
}

func historyRoleCode(item *boardHistoryItem) string {
	if item == nil {
		return ""
	}
	if item.NavigationTarget != nil && strings.TrimSpace(item.NavigationTarget.RoleCode) != "" {
		return strings.TrimSpace(item.NavigationTarget.RoleCode)
	}
	for _, line := range item.DetailLines {
		if strings.HasPrefix(line, "Role: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Role: "))
		}
	}
	return ""
}

func managementDecisionType(item *boardHistoryItem) string {
	if item == nil {
		return ""
	}
	for _, line := range item.DetailLines {
		if strings.HasPrefix(line, "Decision type: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Decision type: "))
		}
	}
	return ""
}

func shouldPrioritizeManagementDecision(cluster, management *boardHistoryItem) bool {
	if management == nil {
		return false
	}
	if cluster == nil {
		return true
	}
	return management.Timestamp >= cluster.Timestamp
}

func deriveDecisionBoardSummary(base string, history []boardHistoryItem) string {
	latestCluster := pickPriorityHistoryCluster(history)
	latestManagement := pickLatestManagementDecision(history)
	if shouldPrioritizeManagementDecision(latestCluster, latestManagement) {
		roleCode := historyRoleCode(latestManagement)
		roleLabel := firstNonEmpty(roleLabelForCode(roleCode), strings.ToUpper(roleCode), "the escalated role")
		switch managementDecisionType(latestManagement) {
		case "deferred":
			return fmt.Sprintf("Management has deferred direct intervention for %s until the next review window. The organization should keep running inside delegated policy while coordination risk is monitored closely.", roleLabel)
		default:
			return fmt.Sprintf("%s is already under active management review. The next move is to clear blockers, adjust resources, or return the role to delegated execution.", roleLabel)
		}
	}
	latest := latestCluster
	if latest == nil {
		return base
	}
	title := firstNonEmpty(latest.ClusterSkillTitle, "A board decision")
	focus := firstNonEmpty(latest.ClusterFocusTitle, "The current focus")
	switch latest.ClusterExecutionStatus {
	case "rejected":
		return fmt.Sprintf("%s has encountered execution resistance. %s should be reviewed immediately before more work queues behind it.", title, focus)
	case "done", "completed":
		return fmt.Sprintf("%s has already produced a completed operating action. Leadership can now verify whether the result should settle into the system as a reusable rule or asset.", title)
	case "in_progress":
		return fmt.Sprintf("%s is now in active execution. Management should keep attention on delivery evidence instead of opening too many new fronts.", title)
	case "accepted", "pending":
		return fmt.Sprintf("%s has been translated into an execution task and is waiting to move further. The current focus is follow-through quality, not more diagnosis.", title)
	default:
		return base
	}
}

func deriveDecisionBoardFocus(base boardFocus, history []boardHistoryItem) boardFocus {
	latestCluster := pickPriorityHistoryCluster(history)
	latestManagement := pickLatestManagementDecision(history)
	if shouldPrioritizeManagementDecision(latestCluster, latestManagement) {
		roleCode := historyRoleCode(latestManagement)
		if roleCode == "" {
			return base
		}
		roleLabel := firstNonEmpty(roleLabelForCode(roleCode), strings.ToUpper(roleCode), roleCode)
		switch managementDecisionType(latestManagement) {
		case "deferred":
			return newBoardFocus(
				fmt.Sprintf("Review %s at next window", roleLabel),
				fmt.Sprintf("%s remains inside organizational observation until the deferred review window.", roleLabel),
				"Management explicitly deferred direct intervention. iWorkerCenter should keep the role inside delegated coordination, surface new evidence, and re-open escalation only if operating risk continues to rise.",
				"info",
				roleCode,
				roleLabel,
			)
		default:
			return newBoardFocus(
				fmt.Sprintf("Hold management attention on %s", roleLabel),
				fmt.Sprintf("%s is already under active management review.", roleLabel),
				"Management has already taken this exception into direct review. The priority is now blocker removal, resource adjustment, and deciding when the role can safely return to delegated execution.",
				"warn",
				roleCode,
				roleLabel,
			)
		}
	}
	latest := latestCluster
	if latest == nil || strings.TrimSpace(latest.ClusterRoleCode) == "" {
		return base
	}
	titleBase := firstNonEmpty(latest.ClusterFocusTitle, latest.ClusterSkillTitle, "Board follow-through")
	switch latest.ClusterExecutionStatus {
	case "rejected":
		description := "The linked task was rejected before completion. Review ownership, routing, and acceptance criteria."
		if strings.TrimSpace(latest.ClusterExecutionResult) != "" {
			description = fmt.Sprintf("The linked task was rejected with result: %s", strings.TrimSpace(latest.ClusterExecutionResult))
		}
		return newBoardFocus(
			fmt.Sprintf("Recover %s", titleBase),
			fmt.Sprintf("%s has stalled in execution and now needs management intervention.", titleBase),
			description,
			"warn",
			latest.ClusterRoleCode,
			latest.ClusterRoleCode,
		)
	case "done", "completed":
		description := "The linked task completed successfully. The next move is to capture the result as a reusable organizational asset."
		if strings.TrimSpace(latest.ClusterExecutionResult) != "" {
			description = fmt.Sprintf("The linked task completed with result: %s", strings.TrimSpace(latest.ClusterExecutionResult))
		}
		return newBoardFocus(
			fmt.Sprintf("Institutionalize %s", titleBase),
			fmt.Sprintf("%s has produced a completed execution result.", titleBase),
			description,
			"ok",
			latest.ClusterRoleCode,
			latest.ClusterRoleCode,
		)
	case "in_progress":
		return newBoardFocus(
			fmt.Sprintf("Monitor %s", titleBase),
			fmt.Sprintf("%s is already being executed by the organization.", titleBase),
			"Leadership should monitor evidence of progress and clear blockers before opening a new executive thread.",
			"info",
			latest.ClusterRoleCode,
			latest.ClusterRoleCode,
		)
	case "accepted", "pending":
		return newBoardFocus(
			fmt.Sprintf("Push %s into motion", titleBase),
			fmt.Sprintf("%s has been converted into a task but still needs stronger follow-through.", titleBase),
			"The current executive priority is to move the linked task from queue to active execution with clear ownership.",
			"info",
			latest.ClusterRoleCode,
			latest.ClusterRoleCode,
		)
	default:
		return base
	}
}
func deriveDecisionActions(base []action, history []boardHistoryItem) []action {
	latest := pickPriorityHistoryCluster(history)
	if latest == nil || strings.TrimSpace(latest.ClusterRoleCode) == "" {
		return base
	}
	titleBase := firstNonEmpty(latest.ClusterFocusTitle, latest.ClusterSkillTitle, "board follow-through")
	roleCode := strings.TrimSpace(latest.ClusterRoleCode)
	roleLabel := roleLabelForCode(roleCode)
	linkedTaskID := firstNonEmpty(latest.ClusterTaskID, linkedTaskIDFromHistory(latest))
	priorityAction := action{}
	switch latest.ClusterExecutionStatus {
	case "rejected":
		description := "The linked task was rejected before completion. Leadership should reset ownership, routing, and acceptance criteria before sending it back into execution."
		if strings.TrimSpace(latest.ClusterExecutionResult) != "" {
			description = fmt.Sprintf("The linked task was rejected with result: %s. Leadership should repair the operating path before retrying execution.", strings.TrimSpace(latest.ClusterExecutionResult))
		}
		priorityAction = newActionForRoleCode("Recover blocked board action", roleCode, roleLabel, description)
	case "done", "completed":
		priorityAction = newActionForRoleCode(
			fmt.Sprintf("Deposit %s into system assets", titleBase),
			roleCode,
			roleLabel,
			"The decision has already produced execution evidence. Capture the outcome as reusable memory, workflow logic, or operating policy before it falls back into people-dependent work.",
		)
	case "in_progress":
		priorityAction = newActionForRoleCode(
			fmt.Sprintf("Track execution evidence for %s", titleBase),
			roleCode,
			roleLabel,
			"The work is already underway. Keep management attention on progress evidence, blocker removal, and closing the loop into durable operating standards.",
		)
	case "accepted", "pending":
		priorityAction = newActionForRoleCode(
			fmt.Sprintf("Move %s into active execution", titleBase),
			roleCode,
			roleLabel,
			"The decision has already been converted into a task. The immediate management move is to get it out of queue and into active execution with clear ownership.",
		)
	default:
		return base
	}
	priorityAction.LinkedTaskID = linkedTaskID
	priorityAction.LinkedTaskStatus = latest.ClusterExecutionStatus
	priorityAction.LinkedTaskResult = latest.ClusterExecutionResult
	result := []action{priorityAction}
	for _, item := range base {
		if item.Title == priorityAction.Title && item.OwnerRoleCode == priorityAction.OwnerRoleCode {
			continue
		}
		result = append(result, item)
	}
	return result
}

func newActionForRoleCode(title, roleCode, roleLabel, description string) action {
	return action{
		Title:          title,
		Owner:          roleLabel,
		OwnerRoleCode:  roleCode,
		OwnerRoleLabel: roleLabel,
		Description:    description,
	}
}

func linkedTaskIDFromHistory(item *boardHistoryItem) string {
	if item == nil || item.NavigationTarget == nil {
		return ""
	}
	return strings.TrimSpace(item.NavigationTarget.TaskID)
}
func roleLabelForCode(roleCode string) string {
	switch strings.TrimSpace(strings.ToLower(roleCode)) {
	case "delivery":
		return "Delivery"
	case "management":
		return "Management"
	case "product":
		return "Product"
	case "organization":
		return "Organization"
	case "operations":
		return "Operations"
	case "management-systems":
		return "Management Systems"
	case "ceo":
		return "CEO"
	case "coo":
		return "COO"
	default:
		return strings.TrimSpace(roleCode)
	}
}
func buildPriorityDecision(stats overviewStats, reuseRatio int) boardFocus {
	if stats.ActiveColleagues == 0 {
		return newBoardFocus(
			"Restore operating capacity first",
			"No active digital employee is currently online, so the organization has no live operating body to carry decisions.",
			"Before leadership opens new operating threads, the center should restore an executable workforce footprint and confirm failover coverage.",
			"warn",
			"operations",
			"Operations",
		)
	}
	if stats.PendingTasks > stats.ActiveColleagues {
		return newBoardFocus(
			"Clear handoff backlog",
			fmt.Sprintf("%d pending collaboration tasks already exceed the active workforce footprint of %d digital employees.", stats.PendingTasks, stats.ActiveColleagues),
			"This is the most immediate operating priority because execution delay is already forming at cross-role boundaries. Tighten routing, escalation, and done criteria before starting more initiatives.",
			"warn",
			"operations",
			"Operations",
		)
	}
	if reuseRatio < 70 {
		return newBoardFocus(
			"Deposit critical judgment into the system",
			fmt.Sprintf("Knowledge reuse is currently %d%%, so too much organizational value still depends on people instead of durable AI system assets.", reuseRatio),
			"Choose the next human-dependent decisions to settle into memories, capability packages, and executive skills so talent rotation does not interrupt operating continuity.",
			"warn",
			"management-systems",
			"Management Systems",
		)
	}
	if stats.WorkflowDefs == 0 || stats.RunningWorkflows == 0 {
		return newBoardFocus(
			"Expand workflow coverage",
			"The center can observe the organization, but too little execution is flowing through reusable workflows.",
			"Package repeated business motions as workflows so the system, not local improvisation, becomes the default operating path.",
			"info",
			"organization",
			"Organization",
		)
	}
	return newBoardFocus(
		"Deepen executive operating skills",
		"The base operating layer is available, so the next leverage point is leadership-grade review and decision cadence.",
		"Turn recurring CEO and board questions into callable executive skills so strategy review becomes a system behavior instead of a manual reporting ritual.",
		"ok",
		"management-systems",
		"Management Systems",
	)
}

func buildPrioritySummary(stats overviewStats, reuseRatio int) string {
	priority := buildPriorityDecision(stats, reuseRatio)
	switch priority.RoleCode {
	case "operations":
		if stats.ActiveColleagues == 0 {
			return "The board should treat operating capacity restoration as the only immediate priority. Without an active digital workforce body, the organization cannot reliably execute strategy, absorb new work, or preserve continuity."
		}
		return fmt.Sprintf("The board should treat execution flow as the top operating priority. %d pending handoffs are already pressing against a workforce of %d active digital employees, so coordination delay is becoming the main constraint on delivery and decision follow-through.", stats.PendingTasks, stats.ActiveColleagues)
	case "management-systems":
		return fmt.Sprintf("The board should push capability deposition before opening too many new fronts. At %d%% knowledge reuse, too much enterprise value still depends on human judgment remaining in people instead of settling into reusable AI system assets.", reuseRatio)
	case "organization":
		return "The board should expand workflow coverage next. Leadership can already see the organization, but repeated business motion still needs to be packaged into workflows so execution defaults to system design rather than local improvisation."
	default:
		return "The board can now shift from stabilizing fundamentals to deepening leadership-grade operating skills, so recurring executive review becomes part of the system instead of a manual reporting ritual."
	}
}
func buildBoardSummary(stats overviewStats, reuseRatio int) string {
	if stats.ActiveColleagues == 0 {
		return "The organization has not yet established an active digital workforce, so the center should first restore live operating capacity."
	}
	if reuseRatio < 50 {
		return fmt.Sprintf("Execution is live, but only %d%% of visible know-how has been deposited into reusable system assets, so continuity risk remains materially people-dependent.", reuseRatio)
	}
	if stats.PendingTasks > stats.ActiveColleagues {
		return fmt.Sprintf("Capability deposition is improving, but %d pending handoffs now exceed the available active workforce, so coordination flow is becoming the near-term operating constraint.", stats.PendingTasks)
	}
	if stats.WorkflowDefs == 0 || stats.RunningWorkflows == 0 {
		return "The center can observe the organization, but core execution is still not flowing through enough reusable workflows to count as mature AI Native operations."
	}
	return "The center is beginning to operate like an AI Native management system: workforce, workflow, and knowledge assets are all visible, and leadership can keep pushing for deeper institutionalization."
}

func buildBoardFocus(stats overviewStats, reuseRatio int) boardFocus {
	if stats.ActiveColleagues == 0 {
		return newBoardFocus(
			"Restore operating capacity",
			"No active digital employee is currently online.",
			"The first board priority is to restore active operating bodies so the organization can execute at all.",
			"warn",
			"operations",
			"Operations",
		)
	}
	if reuseRatio < 50 {
		return newBoardFocus(
			"Deposit human-dependent judgment",
			fmt.Sprintf("Knowledge reuse is only %d%%, so organizational value is still leaking into people rather than settling into the AI system.", reuseRatio),
			"Management should choose the next high-value decision patterns to deposit into memories, packages, and executive skills.",
			"warn",
			"management-systems",
			"Management Systems",
		)
	}
	if stats.PendingTasks > stats.ActiveColleagues {
		return newBoardFocus(
			"Relieve cross-role handoff pressure",
			fmt.Sprintf("%d pending collaboration tasks now exceed the active workforce footprint of %d digital employees.", stats.PendingTasks, stats.ActiveColleagues),
			"The board should review escalation thresholds, routing ownership, and completion criteria before queue pressure hardens into delivery delay.",
			"warn",
			"operations",
			"Operations",
		)
	}
	if stats.WorkflowDefs == 0 || stats.RunningWorkflows == 0 {
		return newBoardFocus(
			"Expand workflow coverage",
			"The center still lacks enough live workflow movement to make execution consistently organization-designed.",
			"Package repeated business motions as workflows so the system, not local improvisation, becomes the default operating path.",
			"info",
			"organization",
			"Organization",
		)
	}
	return newBoardFocus(
		"Deepen executive operating skills",
		"The basic operating layer is visible; the next step is to make leadership review more callable and more comparable.",
		"Turn recurring CEO and board questions into standard skills so strategy review becomes part of the system instead of a manual reporting ritual.",
		"ok",
		"management-systems",
		"Management Systems",
	)
}

func newBoardFocus(title, summary, description, status, roleCode, roleLabel string) boardFocus {
	return boardFocus{
		Title:       title,
		Summary:     summary,
		Description: description,
		Status:      status,
		RoleCode:    roleCode,
		RoleLabel:   roleLabel,
	}
}

func buildBoardSignals(stats overviewStats, reuseRatio int) []item {
	workflowUtilization := 0
	if stats.WorkflowDefs > 0 {
		workflowUtilization = stats.RunningWorkflows * 100 / stats.WorkflowDefs
		if workflowUtilization > 100 {
			workflowUtilization = 100
		}
	}

	healthStatus := "ok"
	if stats.PendingTasks > stats.ActiveColleagues && stats.ActiveColleagues > 0 {
		healthStatus = "warn"
	} else if stats.PendingTasks > 0 {
		healthStatus = "info"
	}

	depositionStatus := "ok"
	if reuseRatio < 50 {
		depositionStatus = "warn"
	} else if reuseRatio < 75 {
		depositionStatus = "info"
	}

	orchestrationStatus := "ok"
	if stats.WorkflowDefs == 0 || stats.RunningWorkflows == 0 {
		orchestrationStatus = "warn"
	} else if workflowUtilization < 50 {
		orchestrationStatus = "info"
	}

	priority := buildPriorityDecision(stats, reuseRatio)
	signals := []item{
		newRisk(
			"Operational load posture",
			"Operations",
			fmt.Sprintf("The center is carrying %d active collaboration tasks, including %d pending handoffs. This is the immediate indicator of whether role-to-role movement is staying healthy.", stats.ActiveTasks, stats.PendingTasks),
			healthStatus,
		),
		newRisk(
			"Capability deposition posture",
			"Management Systems",
			fmt.Sprintf("%d%% of the visible organization footprint is currently represented by active memories and capability packages. This shows how much execution logic is becoming durable system capital instead of remaining person-dependent.", reuseRatio),
			depositionStatus,
		),
		newRisk(
			"Workflow orchestration posture",
			"Organization",
			fmt.Sprintf("%d of %d workflow templates are currently live. The center should keep increasing how much core execution runs through explicit organizational design rather than manual coordination.", stats.RunningWorkflows, stats.WorkflowDefs),
			orchestrationStatus,
		),
	}

	for i := range signals {
		signals[i].SignalPriority = boardSignalPriority(signals[i], priority)
	}
	sort.SliceStable(signals, func(i, j int) bool {
		if signals[i].SignalPriority != signals[j].SignalPriority {
			return signals[i].SignalPriority < signals[j].SignalPriority
		}
		if signalStatusPriority(signals[i].Status) != signalStatusPriority(signals[j].Status) {
			return signalStatusPriority(signals[i].Status) < signalStatusPriority(signals[j].Status)
		}
		return signals[i].Title < signals[j].Title
	})
	return signals
}

func boardSignalPriority(signal item, priority boardFocus) int {
	if signal.RoleCode == priority.RoleCode {
		return 0
	}
	return signalStatusPriority(signal.Status) + 1
}

func signalStatusPriority(status string) int {
	switch status {
	case "warn":
		return 0
	case "info":
		return 1
	default:
		return 2
	}
}
func calculateReuseRatio(stats overviewStats) int {
	reuseRatio := 0
	if stats.ActiveColleagues+stats.Roles > 0 {
		reuseRatio = (stats.ActiveMemories + stats.Capabilities) * 100 / (stats.ActiveColleagues + stats.Roles)
		if reuseRatio > 100 {
			reuseRatio = 100
		}
	}
	return reuseRatio
}

func buildSkillResult(skillID string, stats overviewStats) (skillResult, bool) {
	reuseRatio := calculateReuseRatio(stats)
	focus := buildSkillFocus(skillID, stats, reuseRatio)
	switch skillID {
	case "revenue-gap":
		return skillResult{
			SkillID: "revenue-gap",
			Title:   "Revenue gap diagnosis",
			Summary: fmt.Sprintf("The current operating picture suggests growth is more likely being limited by execution confidence and organizational throughput than by the absence of activity. There are %d running workflows and %d pending collaboration tasks in the center.", stats.RunningWorkflows, stats.PendingTasks),
			Focus:   focus,
			Findings: []string{
				fmt.Sprintf("The center currently shows %d pending cross-role tasks, which implies a meaningful amount of work is waiting at handoff boundaries.", stats.PendingTasks),
				fmt.Sprintf("Only %d%% of the current organization footprint is reflected in active memories and capability packages, which suggests important delivery logic may still be person-dependent.", reuseRatio),
				"When operating confidence is low, management and frontline teams often become cautious in committing to more aggressive growth motions.",
			},
			Recommendations: []action{
				newAction("Stabilize delivery promise rules", "Delivery", "Turn delivery qualification judgment into reusable policies and checklists before commercial commitments are made."),
				newAction("Reduce decision bottlenecks", "Management", "Escalate only high-value exceptions and let routine operating decisions flow through standard center rules."),
			},
		}, true
	case "org-risk":
		return skillResult{
			SkillID: "org-risk",
			Title:   "Organization fragility scan",
			Summary: fmt.Sprintf("The main fragility signal is organizational dependence on undeclared know-how. The center has %d active digital employees but only %d active memories and %d capability packages.", stats.ActiveColleagues, stats.ActiveMemories, stats.Capabilities),
			Focus:   focus,
			Findings: []string{
				fmt.Sprintf("Knowledge reuse is currently measured at %d%%, which is still low for an AI Native organization that aims to survive talent rotation without disruption.", reuseRatio),
				fmt.Sprintf("%d pending collaboration tasks suggest some coordination still depends on local judgment rather than mature workflow design.", stats.PendingTasks),
				"This creates continuity risk whenever key operators are replaced, overloaded, or temporarily unavailable.",
			},
			Recommendations: []action{
				newAction("Codify exception handling", "Product", "Capture the top exception branches as explicit workflow steps and skill prompts."),
				newAction("Separate judgment from memory", "Organization", "Move reusable context and decisions into center memory so people are no longer the only repository."),
			},
		}, true
	case "exec-focus":
		return skillResult{
			SkillID: "exec-focus",
			Title:   "CEO focus agenda",
			Summary: fmt.Sprintf("The CEO should focus on capability deposition, handoff quality, and management visibility. The center currently has %d workflow templates, %d running workflows, and %d pending collaboration tasks.", stats.WorkflowDefs, stats.RunningWorkflows, stats.PendingTasks),
			Focus:   focus,
			Findings: []string{
				"Execution is present, but it still needs stronger organizational reuse and less dependence on ad hoc coordination.",
				fmt.Sprintf("There are %d active memories and %d capability packages; management should decide which missing high-value capabilities must be deposited next.", stats.ActiveMemories, stats.Capabilities),
				"Management visibility should move from passive dashboard watching to active skill-driven operating review.",
			},
			Recommendations: []action{
				newAction("Choose the top 3 deposition targets", "CEO", "Mandate which human-dependent capabilities must be converted into center assets this month."),
				newAction("Review handoff failures weekly", "COO", "Use concrete cross-role misses to sharpen workflow ownership and completion criteria."),
			},
		}, true
	case "delivery-bottleneck":
		return skillResult{
			SkillID: "delivery-bottleneck",
			Title:   "Delivery bottleneck review",
			Summary: fmt.Sprintf("Delivery drag currently looks more like a coordination and flow-control issue than a pure staffing issue. There are %d active tasks and %d running workflows in the system.", stats.ActiveTasks, stats.RunningWorkflows),
			Focus:   focus,
			Findings: []string{
				fmt.Sprintf("%d pending collaboration tasks indicate execution is queuing at handoff boundaries.", stats.PendingTasks),
				fmt.Sprintf("Only %d workflow templates are configured, so too much operating variation may still be absorbed manually instead of by process.", stats.WorkflowDefs),
				"This slows down both customer delivery and management decision speed when exceptions occur.",
			},
			Recommendations: []action{
				newAction("Standardize done criteria", "Delivery", "Require each workflow step to declare output artifact, owner, and acceptance evidence."),
				newAction("Set earlier escalation triggers", "Operations", "Push high-risk cases into management review before downstream teams are blocked."),
			},
		}, true
	case "system-deposition":
		return skillResult{
			SkillID: "system-deposition",
			Title:   "Capability deposition priorities",
			Summary: fmt.Sprintf("The best first deposits are the recurring high-value decisions that the center still cannot represent well. Right now there are %d active memories, %d capability packages, and %d active digital employees.", stats.ActiveMemories, stats.Capabilities, stats.ActiveColleagues),
			Focus:   focus,
			Findings: []string{
				fmt.Sprintf("A %d%% reuse ratio suggests the center still has room to convert tacit operating logic into reusable system assets.", reuseRatio),
				"Delivery shaping, exception triage, and executive reporting remain strong candidates for early structured deposition.",
				"These areas are repeated often enough to matter and expensive enough to justify turning into durable center assets.",
			},
			Recommendations: []action{
				newAction("Deposit delivery exception playbooks", "Product", "Capture common exception handling into templates, routing rules, and reusable prompts."),
				newAction("Build executive reporting skills", "Management Systems", "Make recurring management questions callable and comparable inside iWorkerCenter."),
			},
		}, true
	default:
		return skillResult{}, false
	}
}

func buildSkillFocus(skillID string, stats overviewStats, reuseRatio int) boardFocus {
	switch skillID {
	case "revenue-gap":
		return newBoardFocus(
			"Stabilize growth confidence",
			fmt.Sprintf("%d pending collaboration tasks are reducing execution confidence behind revenue commitments.", stats.PendingTasks),
			"Management should tighten delivery promise rules and remove approval bottlenecks before pushing for more aggressive topline movement.",
			"warn",
			"management",
			"Management",
		)
	case "org-risk":
		if reuseRatio < 70 {
			return newBoardFocus(
				"Move critical know-how into the system",
				fmt.Sprintf("Only %d%% of visible organizational logic is currently captured as reusable system assets.", reuseRatio),
				"The highest-leverage continuity move is to deposit exception handling and operating judgment into memories, packages, and structured workflows.",
				"warn",
				"management-systems",
				"Management Systems",
			)
		}
		return newBoardFocus(
			"Reduce fragile coordination links",
			fmt.Sprintf("%d pending handoffs suggest some delivery continuity still depends on local judgment and informal coordination.", stats.PendingTasks),
			"Use role-level review to find where the organization still depends on individuals instead of durable routing and completion rules.",
			"info",
			"operations",
			"Operations",
		)
	case "exec-focus":
		return buildBoardFocus(stats, reuseRatio)
	case "delivery-bottleneck":
		return newBoardFocus(
			"Unblock delivery flow",
			fmt.Sprintf("%d active tasks and %d pending handoffs point to coordination drag inside the delivery chain.", stats.ActiveTasks, stats.PendingTasks),
			"The next management move is to sharpen handoff criteria, escalation timing, and role ownership before downstream queues compound.",
			"warn",
			"operations",
			"Operations",
		)
	case "system-deposition":
		return newBoardFocus(
			"Choose the next deposition targets",
			fmt.Sprintf("At %d%% reuse, the center still has room to absorb more high-value human judgment into durable AI system assets.", reuseRatio),
			"Prioritize repeated executive reporting, delivery exception handling, and triage logic as the next institutional deposits.",
			"info",
			"management-systems",
			"Management Systems",
		)
	default:
		return buildBoardFocus(stats, reuseRatio)
	}
}

func (h *Handler) collectBoardHistory(ctx context.Context, limit int) ([]boardHistoryItem, error) {
	if h.audit == nil {
		return nil, nil
	}
	tenantID := tenant.TenantIDFromContext(ctx)
	if tenantID == "" {
		return nil, nil
	}
	logs, err := h.audit.ListRecent(tenantID, 24)
	if err != nil || len(logs) == 0 {
		return nil, err
	}
	tasksByID, err := h.loadHistoryTasks(ctx, logs)
	if err != nil {
		return nil, err
	}
	items := buildBoardHistoryFromAudit(logs, tasksByID)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (h *Handler) loadHistoryTasks(ctx context.Context, logs []*audit.ProxyLog) (map[string]historyTaskSnapshot, error) {
	result := map[string]historyTaskSnapshot{}
	if h.read == nil {
		return result, nil
	}
	ids := make([]string, 0)
	seen := map[string]bool{}
	for _, entry := range logs {
		if entry == nil || entry.WorkType != "executive_action_task" {
			continue
		}
		taskID := strings.TrimSpace(extractAuditField(entry.ErrorMsg, "task_id"))
		if taskID == "" || seen[taskID] {
			continue
		}
		seen[taskID] = true
		ids = append(ids, taskID)
	}
	if len(ids) == 0 {
		return result, nil
	}
	placeholders := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids)+1)
	for _, id := range ids {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	tenantID := tenant.TenantIDFromContext(ctx)
	args = append(args, tenantID)
	query := fmt.Sprintf(`SELECT id, title, status, result, to_role_code, created_at, updated_at
		FROM collaboration_tasks WHERE id IN (%s) AND tenant_id=?`, strings.Join(placeholders, ","))
	rows, err := h.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var snap historyTaskSnapshot
		if err := rows.Scan(&snap.ID, &snap.Title, &snap.Status, &snap.Result, &snap.ToRoleCode, &snap.CreatedAt, &snap.UpdatedAt); err != nil {
			return nil, err
		}
		result[snap.ID] = snap
	}
	return result, rows.Err()
}

func buildManagementDecisionHistory(logs []*audit.ProxyLog) []boardHistoryItem {
	items := make([]boardHistoryItem, 0, 4)
	for _, entry := range logs {
		if entry == nil || entry.WorkType != "management_decision" {
			continue
		}
		roleCode := strings.TrimSpace(extractAuditField(entry.ErrorMsg, "role_code"))
		decisionType := strings.TrimSpace(extractAuditField(entry.ErrorMsg, "decision_type"))
		detail := firstNonEmpty(strings.TrimSpace(extractAuditField(entry.ErrorMsg, "detail")), strings.TrimSpace(entry.Summary), "Management intervention was recorded.")
		displayTime := firstNonEmpty(strings.TrimSpace(extractAuditField(entry.ErrorMsg, "display_time")), entry.CreatedAt.Format(time.RFC3339))
		title := "Management review opened"
		tone := "warn"
		if decisionType == "deferred" {
			title = "Management follow-up deferred"
			tone = "info"
		}
		if roleCode != "" {
			title = fmt.Sprintf("%s for %s", title, strings.ToUpper(roleCode))
		}
		item := boardHistoryItem{
			ID:        fmt.Sprintf("management-%s", entry.ID),
			Title:     title,
			Detail:    detail,
			Timestamp: entry.CreatedAt.Format(time.RFC3339),
			Tone:      tone,
			DetailLines: []string{
				fmt.Sprintf("Decision type: %s", firstNonEmpty(decisionType, "review")),
				fmt.Sprintf("Role: %s", firstNonEmpty(roleCode, "No direct role attached")),
				fmt.Sprintf("Recorded at: %s", displayTime),
			},
		}
		if roleCode != "" {
			item.NavigationTarget = &historyNavigationTarget{
				RoleCode: roleCode,
				Source:   "organization_history",
			}
		}
		items = append(items, item)
		if len(items) >= 4 {
			break
		}
	}
	return items
}
func buildBoardHistoryFromAudit(logs []*audit.ProxyLog, tasksByID map[string]historyTaskSnapshot) []boardHistoryItem {
	clusters := map[string]*executiveAuditCluster{}
	for _, entry := range logs {
		if entry == nil || (entry.WorkType != "executive_skill" && entry.WorkType != "executive_action_task") {
			continue
		}
		detail := strings.TrimSpace(entry.ErrorMsg)
		skillID := strings.TrimSpace(extractAuditField(detail, "skill_id"))
		if skillID == "" {
			skillID = strings.TrimSpace(strings.TrimPrefix(entry.RequestID, "executive-skill-"))
		}
		if skillID == "" {
			skillID = entry.ID
		}
		cluster := clusters[skillID]
		if cluster == nil {
			cluster = &executiveAuditCluster{
				ID:        fmt.Sprintf("executive-cluster-%s", skillID),
				SkillID:   skillID,
				Timestamp: entry.CreatedAt,
				Tone:      auditTone(entry.Status),
			}
			clusters[skillID] = cluster
		}
		cluster.SkillTitle = firstNonEmpty(cluster.SkillTitle, deriveSkillTitle(entry.Summary))
		cluster.FocusTitle = firstNonEmpty(cluster.FocusTitle, extractAuditField(detail, "focus"))
		cluster.TaskID = firstNonEmpty(cluster.TaskID, extractAuditField(detail, "task_id"))
		cluster.TaskTitle = firstNonEmpty(cluster.TaskTitle, extractAuditField(detail, "task"))
		cluster.RoleCode = firstNonEmpty(cluster.RoleCode, extractAuditField(detail, "role_code"))
		cluster.Tone = mergeAuditTone(cluster.Tone, auditTone(entry.Status))
		if entry.CreatedAt.After(cluster.Timestamp) {
			cluster.Timestamp = entry.CreatedAt
		}
		if entry.WorkType == "executive_skill" {
			cluster.HasSkill = true
		}
		if entry.WorkType == "executive_action_task" {
			cluster.HasTask = true
		}
	}
	managementItems := buildManagementDecisionHistory(logs)
	items := make([]boardHistoryItem, 0, len(clusters)+len(managementItems))
	for _, cluster := range clusters {
		items = append(items, buildBoardHistoryCluster(*cluster, tasksByID[cluster.TaskID]))
	}
	items = append(items, managementItems...)
	sort.Slice(items, func(i, j int) bool {
		left := decisionStatusPriority(items[i].ClusterExecutionStatus)
		right := decisionStatusPriority(items[j].ClusterExecutionStatus)
		if left != right {
			return left < right
		}
		return items[i].Timestamp > items[j].Timestamp
	})
	return items
}

func buildBoardHistoryCluster(cluster executiveAuditCluster, task historyTaskSnapshot) boardHistoryItem {
	executionLine := "Execution: No task has been created from this decision yet"
	if cluster.HasTask {
		executionLine = "Execution: Task exists but current runtime status is not available in this view"
	}
	if task.ID != "" {
		executionLine = fmt.Sprintf("Execution: %s", task.Status)
		if strings.TrimSpace(task.Result) != "" {
			executionLine += fmt.Sprintf(" | Result: %s", strings.TrimSpace(task.Result))
		}
	}
	tone := cluster.Tone
	switch task.Status {
	case "rejected":
		tone = "warn"
	case "done", "completed":
		tone = "ok"
	case "in_progress", "accepted", "pending":
		tone = "info"
	}
	detail := fmt.Sprintf("%s was raised for management follow-through.", firstNonEmpty(cluster.FocusTitle, "A board focus"))
	if cluster.HasTask {
		detail = fmt.Sprintf("%s has already been converted into task %s.", firstNonEmpty(cluster.FocusTitle, "A board focus"), firstNonEmpty(task.Title, cluster.TaskTitle, "execution"))
	}
	item := boardHistoryItem{
		ID:        cluster.ID,
		Title:     fmt.Sprintf("%s reviewed", firstNonEmpty(cluster.SkillTitle, "Executive skill")),
		Detail:    detail,
		Timestamp: cluster.Timestamp.Format(time.RFC3339),
		Tone:      tone,
		DetailLines: []string{
			fmt.Sprintf("Skill: %s", firstNonEmpty(cluster.SkillTitle, "Executive skill")),
			fmt.Sprintf("Focus: %s", firstNonEmpty(cluster.FocusTitle, "No explicit focus captured")),
			fmt.Sprintf("Task: %s", firstNonEmpty(task.Title, cluster.TaskTitle, map[bool]string{true: "Execution task created", false: "No task has been created from this decision yet"}[cluster.HasTask])),
			fmt.Sprintf("Role: %s", firstNonEmpty(cluster.RoleCode, task.ToRoleCode, "No direct role attached")),
			executionLine,
		},
		IsCluster:              true,
		ClusterSkillTitle:      firstNonEmpty(cluster.SkillTitle, "Executive skill"),
		ClusterFocusTitle:      cluster.FocusTitle,
		ClusterTaskID:          cluster.TaskID,
		ClusterTaskTitle:       firstNonEmpty(task.Title, cluster.TaskTitle),
		ClusterRoleCode:        firstNonEmpty(cluster.RoleCode, task.ToRoleCode),
		ClusterExecutionStatus: task.Status,
		ClusterExecutionResult: task.Result,
	}
	if cluster.HasTask {
		item.Title = fmt.Sprintf("%s dispatched an action", firstNonEmpty(cluster.SkillTitle, "Executive skill"))
	}
	if cluster.TaskID != "" || item.ClusterRoleCode != "" {
		item.NavigationTarget = &historyNavigationTarget{TaskID: cluster.TaskID, RoleCode: item.ClusterRoleCode}
		if cluster.HasTask {
			item.NavigationTarget.Source = "skill_task_cluster"
		} else {
			item.NavigationTarget.Source = "skill_history"
		}
	}
	return item
}

func decisionStatusPriority(status string) int {
	switch strings.TrimSpace(status) {
	case "rejected":
		return 0
	case "pending", "accepted":
		return 1
	case "in_progress":
		return 2
	case "done", "completed":
		return 3
	default:
		return 4
	}
}

func deriveSkillTitle(summary string) string {
	title := strings.TrimSpace(summary)
	title = strings.TrimPrefix(title, "Executive skill ")
	title = strings.TrimSuffix(title, " reviewed")
	title = strings.TrimPrefix(title, "Task created from executive skill ")
	return strings.TrimSpace(title)
}

func extractAuditField(text, field string) string {
	for _, segment := range strings.Split(text, "|") {
		part := strings.TrimSpace(segment)
		prefix := field + ":"
		if strings.HasPrefix(strings.ToLower(part), strings.ToLower(prefix)) {
			return strings.TrimSpace(part[len(prefix):])
		}
	}
	return ""
}

func auditTone(status string) string {
	if strings.TrimSpace(status) == "ok" {
		return "ok"
	}
	return "warn"
}

func mergeAuditTone(current, next string) string {
	if current == "warn" || next == "warn" {
		return "warn"
	}
	if current == "info" || next == "info" {
		return "info"
	}
	return "ok"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
func (h *Handler) collectOverviewStats(ctx context.Context) (overviewStats, error) {
	if h.read == nil {
		return overviewStats{}, nil
	}

	stats := overviewStats{}
	var err error
	if stats.ActiveColleagues, err = h.countRows(ctx, "colleagues", "status='active'"); err != nil {
		return stats, err
	}
	if stats.Roles, err = h.countRows(ctx, "roles", "status='active'"); err != nil {
		return stats, err
	}
	if stats.ActiveMemories, err = h.countRows(ctx, "shared_memories", "status='active'"); err != nil {
		return stats, err
	}
	if stats.Capabilities, err = h.countRows(ctx, "capability_packages", "status='active'"); err != nil {
		return stats, err
	}
	if stats.WorkflowDefs, err = h.countRows(ctx, "workflow_definitions", "status!='archived'"); err != nil {
		return stats, err
	}
	if stats.RunningWorkflows, err = h.countRows(ctx, "workflow_instances", "status='running'"); err != nil {
		return stats, err
	}
	if stats.PendingTasks, err = h.countRows(ctx, "collaboration_tasks", "status='pending'"); err != nil {
		return stats, err
	}
	if stats.ActiveTasks, err = h.countRows(ctx, "collaboration_tasks", "status IN ('pending','running')"); err != nil {
		return stats, err
	}
	if stats.CompletedTasks, err = h.countRows(ctx, "collaboration_tasks", "status='done'"); err != nil {
		return stats, err
	}
	return stats, nil
}

func (h *Handler) countRows(ctx context.Context, table string, extraCondition string) (int, error) {
	query := "SELECT COUNT(*) FROM " + table
	args := []any{}
	conditions := []string{}
	if extraCondition != "" {
		conditions = append(conditions, extraCondition)
	}

	tenantID := tenant.TenantIDFromContext(ctx)
	if tenantID != "" {
		conditions = append(conditions, "tenant_id=?")
		args = append(args, tenantID)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	var count int
	if err := h.read.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func overviewStatus(stats overviewStats) string {
	if stats.PendingTasks >= 8 || stats.ActiveColleagues == 0 {
		return "warn"
	}
	if stats.ActiveMemories == 0 || stats.Capabilities == 0 {
		return "warn"
	}
	return "info"
}

func buildRisks(stats overviewStats, reuseRatio int) []item {
	risks := []item{}
	if reuseRatio < 50 {
		risks = append(risks, newRisk(
			"Capability deposition is still uneven",
			"Product",
			fmt.Sprintf("Only %d%% of the current organization footprint is reflected through active memories and capability packages, which suggests too much knowledge may still sit in people or ad hoc work.", reuseRatio),
			"warn",
		))
	}
	if stats.PendingTasks > stats.ActiveColleagues && stats.ActiveColleagues > 0 {
		risks = append(risks, newRisk(
			"Cross-role handoff pressure is rising",
			"Operations",
			fmt.Sprintf("There are %d pending collaboration tasks for %d active digital employees, which suggests execution flow may be slowing at handoff boundaries.", stats.PendingTasks, stats.ActiveColleagues),
			"warn",
		))
	}
	if stats.WorkflowDefs == 0 || stats.RunningWorkflows == 0 {
		risks = append(risks, newRisk(
			"Workflow orchestration is underused",
			"Organization",
			"The center has not yet accumulated enough active workflow movement, which means organizational capability may still depend on manual coordination more than designed process.",
			"info",
		))
	}
	if len(risks) == 0 {
		risks = append(risks, newRisk(
			"Executive visibility is improving",
			"Management Systems",
			"The center has enough live organizational data to support management review. The next step is to deepen skill outputs and decision follow-through.",
			"info",
		))
	}
	return risks
}

func newRisk(title, owner, description, status string) item {
	roleCode, roleLabel := inferOwnerRole(owner)
	return item{
		Title:       title,
		Description: description,
		Status:      status,
		RoleCode:    roleCode,
		RoleLabel:   roleLabel,
	}
}

func buildActions(stats overviewStats, reuseRatio int) []action {
	actions := []action{}
	if reuseRatio < 70 {
		actions = append(actions, newAction(
			"Promote more operating knowledge into center memory",
			"Product",
			"Convert recurring delivery judgment, exception handling, and management reporting into reusable memories and capabilities.",
		))
	}
	if stats.PendingTasks > 0 {
		actions = append(actions, newAction(
			"Review delayed handoffs this week",
			"Operations",
			fmt.Sprintf("Use the %d pending collaboration tasks to identify where role-to-role completion criteria and escalation thresholds still need tightening.", stats.PendingTasks),
		))
	}
	if stats.WorkflowDefs < 3 {
		actions = append(actions, newAction(
			"Expand reusable workflow coverage",
			"Organization",
			"Package the top recurring business motions as workflows so execution can scale without depending on specific people.",
		))
	}
	if len(actions) == 0 {
		actions = append(actions, newAction(
			"Promote executive skills usage",
			"Management Systems",
			"Turn recurring CEO and board questions into skill-driven operating reviews instead of one-off reporting work.",
		))
	}
	return actions
}

func newAction(title, owner, description string) action {
	roleCode, roleLabel := inferOwnerRole(owner)
	return action{
		Title:          title,
		Owner:          owner,
		OwnerRoleCode:  roleCode,
		OwnerRoleLabel: roleLabel,
		Description:    description,
	}
}

func inferOwnerRole(owner string) (string, string) {
	switch strings.ToLower(strings.TrimSpace(owner)) {
	case "delivery":
		return "delivery", "Delivery"
	case "management":
		return "management", "Management"
	case "product":
		return "product", "Product"
	case "organization":
		return "organization", "Organization"
	case "operations":
		return "operations", "Operations"
	case "management systems":
		return "management-systems", "Management Systems"
	case "ceo":
		return "ceo", "CEO"
	case "coo":
		return "coo", "COO"
	default:
		fallback := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(owner), " ", "-"))
		return fallback, strings.TrimSpace(owner)
	}
}

var executiveSkills = []skill{
	{ID: "revenue-gap", Title: "Revenue gap", Question: "Why is revenue below target this month?", Description: "Summarize likely causes, structural issues, and the first actions management should push."},
	{ID: "org-risk", Title: "Org risk", Question: "Where is the organization becoming fragile?", Description: "Surface person-dependent links, overloaded nodes, and risks to continuity."},
	{ID: "exec-focus", Title: "CEO focus", Question: "What are the three things the CEO should push now?", Description: "Compress the current operating state into a short executive agenda."},
	{ID: "delivery-bottleneck", Title: "Delivery bottleneck", Question: "What is currently slowing execution quality?", Description: "Identify workflow bottlenecks and suggest ownership changes or automation."},
	{ID: "system-deposition", Title: "Capability deposition", Question: "Which capabilities must be deposited into the AI system first?", Description: "Prioritize where human know-how should be turned into reusable skills and workflows."},
}
