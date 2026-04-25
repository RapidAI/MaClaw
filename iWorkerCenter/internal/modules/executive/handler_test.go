package executive

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/audit"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/db"
)

func TestHandleOverviewUsesRealStats(t *testing.T) {
	provider := openExecTestDB(t)
	defer provider.Close()
	seedExecutiveData(t, provider)

	auditRepo := audit.NewRepo(provider.Write, provider.Read)
	seedExecutiveHistory(t, auditRepo)
	h := NewHandler(provider.Read, auditRepo)
	req := httptest.NewRequest(http.MethodGet, "/admin/executive/overview", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()

	h.handleOverview(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	metrics, ok := body["metrics"].([]any)
	if !ok || len(metrics) != 4 {
		t.Fatalf("metrics = %#v, want 4 entries", body["metrics"])
	}

	boardSummary, ok := body["board_summary"].(string)
	if !ok || !strings.Contains(boardSummary, "translated into an execution task") {
		t.Fatalf("board_summary = %#v", body["board_summary"])
	}

	boardFocus, ok := body["board_focus"].(map[string]any)
	if !ok {
		t.Fatalf("board_focus = %#v", body["board_focus"])
	}
	if boardFocus["role_code"] != "management-systems" {
		t.Fatalf("board focus role_code = %#v", boardFocus["role_code"])
	}
	if boardFocus["title"] != "Push Move critical know-how into the system into motion" {
		t.Fatalf("board focus title = %#v", boardFocus["title"])
	}

	priorityDecision, ok := body["priority_decision"].(map[string]any)
	if !ok {
		t.Fatalf("priority_decision = %#v", body["priority_decision"])
	}
	if priorityDecision["role_code"] != "management-systems" {
		t.Fatalf("priority_decision.role_code = %#v, want management-systems", priorityDecision["role_code"])
	}
	if priorityDecision["title"] != "Push Move critical know-how into the system into motion" {
		t.Fatalf("priority_decision.title = %#v", priorityDecision["title"])
	}

	prioritySummary, ok := body["priority_summary"].(string)
	if !ok || prioritySummary == "" {
		t.Fatalf("priority_summary = %#v", body["priority_summary"])
	}
	if !strings.Contains(prioritySummary, "translated into an execution task") {
		t.Fatalf("priority_summary = %q", prioritySummary)
	}

	boardSignals, ok := body["board_signals"].([]any)
	if !ok || len(boardSignals) != 3 {
		t.Fatalf("board_signals = %#v, want 3 entries", body["board_signals"])
	}
	firstBoardSignal, ok := boardSignals[0].(map[string]any)
	if !ok {
		t.Fatalf("first board signal = %#v", boardSignals[0])
	}
	if firstBoardSignal["role_code"] == "" {
		t.Fatalf("board signal role_code should be populated: %#v", firstBoardSignal)
	}
	if firstBoardSignal["role_code"] != priorityDecision["role_code"] {
		t.Fatalf("first board signal role_code = %#v, want priority decision role %#v", firstBoardSignal["role_code"], priorityDecision["role_code"])
	}
	if firstBoardSignal["signal_priority"] != float64(0) {
		t.Fatalf("first board signal signal_priority = %#v, want 0", firstBoardSignal["signal_priority"])
	}

	boardHistory, ok := body["board_history"].([]any)
	if !ok || len(boardHistory) == 0 {
		t.Fatalf("board_history = %#v, want at least 1 entry", body["board_history"])
	}
	firstHistory, ok := boardHistory[0].(map[string]any)
	if !ok {
		t.Fatalf("first board history = %#v", boardHistory[0])
	}
	if firstHistory["isCluster"] != true {
		t.Fatalf("first board history should be a cluster: %#v", firstHistory)
	}
	if firstHistory["clusterRoleCode"] != "management-systems" {
		t.Fatalf("first history clusterRoleCode = %#v", firstHistory["clusterRoleCode"])
	}
	if firstHistory["clusterExecutionStatus"] != "pending" {
		t.Fatalf("first history clusterExecutionStatus = %#v", firstHistory["clusterExecutionStatus"])
	}

	actions, ok := body["actions"].([]any)
	if !ok || len(actions) == 0 {
		t.Fatalf("actions = %#v", body["actions"])
	}
	firstAction, ok := actions[0].(map[string]any)
	if !ok {
		t.Fatalf("first action = %#v", actions[0])
	}
	if firstAction["owner_role_code"] != "management-systems" {
		t.Fatalf("first action owner_role_code = %#v", firstAction["owner_role_code"])
	}
	if firstAction["linked_task_status"] != "pending" {
		t.Fatalf("first action linked_task_status = %#v", firstAction["linked_task_status"])
	}

	briefing, ok := body["briefing"].(map[string]any)
	if !ok {
		t.Fatalf("briefing = %#v", body["briefing"])
	}
	if !strings.Contains(briefing["description"].(string), "2 active digital employees") {
		t.Fatalf("briefing description = %q", briefing["description"])
	}
	risks, ok := body["risks"].([]any)
	if !ok || len(risks) == 0 {
		t.Fatalf("risks = %#v", body["risks"])
	}
	firstRisk, ok := risks[0].(map[string]any)
	if !ok {
		t.Fatalf("first risk = %#v", risks[0])
	}
	if firstRisk["role_code"] == "" {
		t.Fatalf("risk role_code should be populated: %#v", firstRisk)
	}

}

func TestHandleRunSkillBuildsDynamicResult(t *testing.T) {
	provider := openExecTestDB(t)
	defer provider.Close()
	seedExecutiveData(t, provider)

	auditRepo := audit.NewRepo(provider.Write, provider.Read)
	h := NewHandler(provider.Read, auditRepo)
	payload := bytes.NewBufferString(`{"skill_id":"org-risk"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/executive/skills/run", payload)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()

	h.handleRunSkill(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}

	var body skillResult
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.SkillID != "org-risk" {
		t.Fatalf("skill_id = %q", body.SkillID)
	}
	if len(body.Findings) < 3 {
		t.Fatalf("findings = %#v", body.Findings)
	}
	joined := strings.Join(body.Findings, " ")
	if !strings.Contains(joined, "66%") {
		t.Fatalf("dynamic findings missing reuse ratio: %q", joined)
	}
	if body.Focus.RoleCode == "" {
		t.Fatalf("focus role_code should be populated: %#v", body.Focus)
	}
	if len(body.Recommendations) == 0 {
		t.Fatalf("recommendations = %#v", body.Recommendations)
	}
	if body.Recommendations[0].OwnerRoleCode == "" {
		t.Fatalf("owner_role_code should be populated: %#v", body.Recommendations[0])
	}

	logs, err := auditRepo.ListRecent("tenant-a", 10)
	if err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	if len(logs) == 0 {
		t.Fatalf("expected executive skill audit log to be recorded")
	}
	if logs[0].WorkType != "executive_skill" {
		t.Fatalf("work_type = %q", logs[0].WorkType)
	}
	if !strings.Contains(logs[0].Summary, "Organization fragility scan") {
		t.Fatalf("summary = %q", logs[0].Summary)
	}
}

func openExecTestDB(t *testing.T) *db.Provider {
	t.Helper()
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return provider
}

func seedExecutiveData(t *testing.T, provider *db.Provider) {
	t.Helper()
	now := time.Now().Format(time.RFC3339)
	mustExec(t, provider, `INSERT INTO roles (id, tenant_id, name, code, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "role-1", "tenant-a", "Sales", "sales", "active", now, now)
	mustExec(t, provider, `INSERT INTO colleagues (id, tenant_id, name, role_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "col-1", "tenant-a", "Alice", "role-1", "active", now, now)
	mustExec(t, provider, `INSERT INTO colleagues (id, tenant_id, name, role_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "col-2", "tenant-a", "Bob", "role-1", "active", now, now)
	mustExec(t, provider, `INSERT INTO shared_memories (id, tenant_id, title, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, "mem-1", "tenant-a", "Playbook", "active", now, now)
	mustExec(t, provider, `INSERT INTO capability_packages (id, tenant_id, name, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, "cap-1", "tenant-a", "Qualification", "active", now, now)
	mustExec(t, provider, `INSERT INTO workflow_definitions (id, tenant_id, name, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, "wf-def-1", "tenant-a", "Pipeline Follow-up", "active", now, now)
	mustExec(t, provider, `INSERT INTO workflow_instances (id, tenant_id, definition_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, "wf-inst-1", "tenant-a", "wf-def-1", "running", now, now)
	mustExec(t, provider, `INSERT INTO collaboration_tasks (id, tenant_id, title, from_colleague_id, to_colleague_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "task-1", "tenant-a", "handoff", "col-1", "col-2", "pending", now, now)
	mustExec(t, provider, `INSERT INTO collaboration_tasks (id, tenant_id, title, from_colleague_id, to_colleague_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "task-2", "tenant-a", "done", "col-2", "col-1", "done", now, now)
}

func mustExec(t *testing.T, provider *db.Provider, query string, args ...any) {
	t.Helper()
	if _, err := provider.Write.Exec(query, args...); err != nil {
		t.Fatalf("exec failed: %v\nquery=%s", err, query)
	}
}

func seedExecutiveHistory(t *testing.T, auditRepo *audit.Repo) {
	t.Helper()
	if err := auditRepo.Insert("tenant-a", &audit.ProxyLog{
		RequestID:  "executive-skill-org-risk",
		ProviderID: "iworkercenter",
		Model:      "executive-skill",
		WorkType:   "executive_skill",
		CostTier:   "internal",
		Status:     "ok",
		Summary:    "Executive skill Organization fragility scan reviewed",
		ErrorMsg:   "focus: Move critical know-how into the system | role_code: management-systems | role: Management Systems | summary: Only 66% of visible organizational logic is currently captured as reusable system assets.",
		CreatedAt:  time.Now().Add(-2 * time.Minute),
	}); err != nil {
		t.Fatalf("insert executive skill audit: %v", err)
	}
	if err := auditRepo.Insert("tenant-a", &audit.ProxyLog{
		ProviderID: "iworkercenter",
		Model:      "executive_action",
		WorkType:   "executive_action_task",
		CostTier:   "internal",
		Status:     "ok",
		Summary:    "Task created from executive skill Organization fragility scan",
		ErrorMsg:   "task_id: task-1 | role_code: management-systems | skill_id: org-risk | focus: Move critical know-how into the system | task: handoff",
		CreatedAt:  time.Now().Add(-1 * time.Minute),
	}); err != nil {
		t.Fatalf("insert executive action audit: %v", err)
	}
}

