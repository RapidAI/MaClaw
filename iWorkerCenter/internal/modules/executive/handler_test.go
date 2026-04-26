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
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/collaboration"
	colleagueRepo "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/repo"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/workflow"
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
	foundManagementDecision := false
	for _, raw := range boardHistory {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		title, _ := item["title"].(string)
		if strings.Contains(title, "Management review opened for MANAGEMENT-SYSTEMS") {
			foundManagementDecision = true
			break
		}
	}
	if !foundManagementDecision {
		t.Fatalf("board_history should include management decision events: %#v", boardHistory)
	}
	foundRecoveryDispatch := false
	for _, raw := range boardHistory {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		title, _ := item["title"].(string)
		if strings.Contains(title, "Recovery action dispatched for MANAGEMENT-SYSTEMS") {
			foundRecoveryDispatch = true
			break
		}
	}
	if !foundRecoveryDispatch {
		t.Fatalf("board_history should include recovery dispatch events: %#v", boardHistory)
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

func TestHandleRecordManagementDecisionWritesAuditLog(t *testing.T) {
	provider := openExecTestDB(t)
	defer provider.Close()

	auditRepo := audit.NewRepo(provider.Write, provider.Read)
	h := NewHandler(provider.Read, auditRepo)
	payload := bytes.NewBufferString(`{"role_code":"management-systems","decision_type":"deferred","detail":"Deferred until next review: 04/27 18:30. Revisit MANAGEMENT-SYSTEMS if coordination risk continues to rise.","display_time":"04/26 18:30"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/executive/management-decisions", payload)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()

	h.handleRecordManagementDecision(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}

	logs, err := auditRepo.ListRecent("tenant-a", 10)
	if err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	if len(logs) == 0 {
		t.Fatalf("expected management decision audit log to be recorded")
	}
	if logs[0].WorkType != "management_decision" {
		t.Fatalf("work_type = %q", logs[0].WorkType)
	}
	if !strings.Contains(logs[0].ErrorMsg, "decision_type: deferred") {
		t.Fatalf("error_msg = %q", logs[0].ErrorMsg)
	}
	if !strings.Contains(logs[0].Summary, "deferred for MANAGEMENT-SYSTEMS") {
		t.Fatalf("summary = %q", logs[0].Summary)
	}
}

func TestHandleConfirmAutonomyReturnWritesAuditLog(t *testing.T) {
	provider := openExecTestDB(t)
	defer provider.Close()

	auditRepo := audit.NewRepo(provider.Write, provider.Read)
	h := NewHandler(provider.Read, auditRepo)
	payload := bytes.NewBufferString(`{"role_code":"management-systems","detail":"Autonomy return confirmed at 04/26 19:10. MANAGEMENT-SYSTEMS can leave active management attention and continue inside delegated execution.","display_time":"04/26 19:10"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/executive/autonomy-return", payload)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()

	h.handleConfirmAutonomyReturn(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}

	logs, err := auditRepo.ListRecent("tenant-a", 10)
	if err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	if len(logs) == 0 {
		t.Fatalf("expected autonomy return audit log to be recorded")
	}
	if logs[0].WorkType != "management_autonomy_return" {
		t.Fatalf("work_type = %q", logs[0].WorkType)
	}
	if !strings.Contains(logs[0].ErrorMsg, "decision_type: autonomy_return") {
		t.Fatalf("error_msg = %q", logs[0].ErrorMsg)
	}
	if !strings.Contains(logs[0].Summary, "Return to autonomy confirmed for MANAGEMENT-SYSTEMS") {
		t.Fatalf("summary = %q", logs[0].Summary)
	}
}

func TestHandleOverviewPrioritizesAutonomyReturn(t *testing.T) {
	provider := openExecTestDB(t)
	defer provider.Close()
	seedExecutiveData(t, provider)

	auditRepo := audit.NewRepo(provider.Write, provider.Read)
	seedExecutiveHistory(t, auditRepo)
	if err := auditRepo.Insert("tenant-a", &audit.ProxyLog{
		RequestID:  "management-autonomy-return-management-systems",
		ProviderID: "iworkercenter",
		Model:      "management-autonomy-return",
		WorkType:   "management_autonomy_return",
		CostTier:   "internal",
		Status:     "ok",
		Summary:    "Return to autonomy confirmed for MANAGEMENT-SYSTEMS",
		ErrorMsg:   "decision_type: autonomy_return | role_code: management-systems | detail: Autonomy return confirmed at 04/26 19:10. MANAGEMENT-SYSTEMS can leave active management attention and continue inside delegated execution. | display_time: 04/26 19:10",
		CreatedAt:  time.Now().Add(-10 * time.Second),
	}); err != nil {
		t.Fatalf("insert autonomy return audit: %v", err)
	}

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

	boardHistory, ok := body["board_history"].([]any)
	if !ok || len(boardHistory) == 0 {
		t.Fatalf("board_history = %#v", body["board_history"])
	}
	foundAutonomyReturn := false
	for _, raw := range boardHistory {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		title, _ := item["title"].(string)
		if strings.Contains(title, "Return to autonomy confirmed for MANAGEMENT-SYSTEMS") {
			foundAutonomyReturn = true
			break
		}
	}
	if !foundAutonomyReturn {
		t.Fatalf("board_history should include autonomy return events: %#v", boardHistory)
	}
}

func TestHandleGenerateDepositionDraftsCreatesArtifacts(t *testing.T) {
	provider := openExecTestDB(t)
	defer provider.Close()
	seedExecutiveData(t, provider)

	auditRepo := audit.NewRepo(provider.Write, provider.Read)
	collabRepo := collaboration.NewRepo(provider.Write, provider.Read)
	colRepo := colleagueRepo.New(provider.Write, provider.Read)
	wfRepo := workflow.NewRepo(provider.Write, provider.Read)
	wfSvc := workflow.NewService(wfRepo, provider, collabRepo, colRepo)

	h := NewHandler(provider.Read, auditRepo)
	h.SetWriteDB(provider.Write)
	h.SetWorkflowService(wfSvc)

	payload := bytes.NewBufferString(`{"role_code":"management-systems","action_title":"Deposit recovery learning into system assets","detail":"The role has already been cleared to return to autonomous execution. The next move is to capture the recovery path as reusable memory, workflow logic, and operating policy so the same exception does not need fresh management attention next time."}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/executive/deposition-drafts", payload)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()

	h.handleGenerateDepositionDrafts(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body["memory_id"] == "" || body["capability_id"] == "" || body["workflow_id"] == "" {
		t.Fatalf("draft response missing ids: %#v", body)
	}

	var memoryCount int
	if err := provider.Read.QueryRow("SELECT COUNT(*) FROM shared_memories WHERE tenant_id=? AND title=?", "tenant-a", "Management Systems recovery playbook").Scan(&memoryCount); err != nil {
		t.Fatalf("count memories: %v", err)
	}
	if memoryCount != 1 {
		t.Fatalf("memoryCount = %d, want 1", memoryCount)
	}

	var capabilityCount int
	if err := provider.Read.QueryRow("SELECT COUNT(*) FROM capability_packages WHERE tenant_id=? AND name=?", "tenant-a", "Management Systems recovery handling").Scan(&capabilityCount); err != nil {
		t.Fatalf("count capabilities: %v", err)
	}
	if capabilityCount != 1 {
		t.Fatalf("capabilityCount = %d, want 1", capabilityCount)
	}

	var workflowCount int
	if err := provider.Read.QueryRow("SELECT COUNT(*) FROM workflow_definitions WHERE tenant_id=? AND name=?", "tenant-a", "Management Systems recovery deposition loop").Scan(&workflowCount); err != nil {
		t.Fatalf("count workflows: %v", err)
	}
	if workflowCount != 1 {
		t.Fatalf("workflowCount = %d, want 1", workflowCount)
	}

	logs, err := auditRepo.ListRecent("tenant-a", 10)
	if err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	if len(logs) == 0 {
		t.Fatalf("expected deposition draft audit log to be recorded")
	}
	if logs[0].WorkType != "executive_deposition_draft" {
		t.Fatalf("work_type = %q", logs[0].WorkType)
	}
	if !strings.Contains(logs[0].Summary, "Deposition drafts generated for MANAGEMENT-SYSTEMS") {
		t.Fatalf("summary = %q", logs[0].Summary)
	}
}

func TestHandlePublishDepositionRolloutWritesAuditLog(t *testing.T) {
	provider := openExecTestDB(t)
	defer provider.Close()
	seedExecutiveData(t, provider)

	auditRepo := audit.NewRepo(provider.Write, provider.Read)
	collabRepo := collaboration.NewRepo(provider.Write, provider.Read)
	colRepo := colleagueRepo.New(provider.Write, provider.Read)
	wfRepo := workflow.NewRepo(provider.Write, provider.Read)
	wfSvc := workflow.NewService(wfRepo, provider, collabRepo, colRepo)

	h := NewHandler(provider.Read, auditRepo)
	h.SetWorkflowService(wfSvc)

	payload := bytes.NewBufferString(`{"role_code":"management-systems","workflow_id":"wf-def-1","detail":"The reviewed recovery workflow is ready to become the live organizational standard."}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/executive/deposition-rollout/publish", payload)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()

	h.handlePublishDepositionRollout(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}

	var status string
	if err := provider.Read.QueryRow("SELECT status FROM workflow_definitions WHERE tenant_id=? AND id=?", "tenant-a", "wf-def-1").Scan(&status); err != nil {
		t.Fatalf("query workflow status: %v", err)
	}
	if status != workflow.DefStatusPublished {
		t.Fatalf("workflow status = %q, want %q", status, workflow.DefStatusPublished)
	}

	logs, err := auditRepo.ListRecent("tenant-a", 10)
	if err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	if len(logs) == 0 {
		t.Fatalf("expected standard publish audit log to be recorded")
	}
	if logs[0].WorkType != "executive_standard_published" {
		t.Fatalf("work_type = %q", logs[0].WorkType)
	}
	if !strings.Contains(logs[0].Summary, "Recovery standard published for MANAGEMENT-SYSTEMS") {
		t.Fatalf("summary = %q", logs[0].Summary)
	}
}

func TestHandleOverviewPrioritizesCapabilityApprovalBeforeWorkflowPublish(t *testing.T) {
	provider := openExecTestDB(t)
	defer provider.Close()
	seedExecutiveData(t, provider)

	auditRepo := audit.NewRepo(provider.Write, provider.Read)
	seedExecutiveHistory(t, auditRepo)
	if err := auditRepo.Insert("tenant-a", &audit.ProxyLog{
		RequestID:  "management-autonomy-return-management-systems",
		ProviderID: "iworkercenter",
		Model:      "management-autonomy-return",
		WorkType:   "management_autonomy_return",
		CostTier:   "internal",
		Status:     "ok",
		Summary:    "Return to autonomy confirmed for MANAGEMENT-SYSTEMS",
		ErrorMsg:   "decision_type: autonomy_return | role_code: management-systems | detail: Autonomy return confirmed. | display_time: 04/26 19:10",
		CreatedAt:  time.Now().Add(-20 * time.Second),
	}); err != nil {
		t.Fatalf("insert autonomy return audit: %v", err)
	}
	if err := auditRepo.Insert("tenant-a", &audit.ProxyLog{
		RequestID:  "executive-capability-approved-management-systems",
		ProviderID: "iworkercenter",
		Model:      "executive-capability-approved",
		WorkType:   "executive_capability_approved",
		CostTier:   "internal",
		Status:     "ok",
		Summary:    "Recovery capability package approved for MANAGEMENT-SYSTEMS",
		ErrorMsg:   "role_code: management-systems | capability_id: cap-draft | capability_name: Management Systems recovery handling",
		CreatedAt:  time.Now(),
	}); err != nil {
		t.Fatalf("insert capability approval audit: %v", err)
	}

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

	boardHistory, ok := body["board_history"].([]any)
	if !ok || len(boardHistory) == 0 {
		t.Fatalf("board_history = %#v", body["board_history"])
	}
	foundCapabilityApproval := false
	for _, raw := range boardHistory {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		title, _ := item["title"].(string)
		if strings.Contains(title, "Recovery capability package approved for MANAGEMENT-SYSTEMS") {
			foundCapabilityApproval = true
			break
		}
	}
	if !foundCapabilityApproval {
		t.Fatalf("board_history should include capability approval events: %#v", boardHistory)
	}
}
func TestHandleOverviewPrioritizesPublishedStandard(t *testing.T) {
	provider := openExecTestDB(t)
	defer provider.Close()
	seedExecutiveData(t, provider)

	auditRepo := audit.NewRepo(provider.Write, provider.Read)
	seedExecutiveHistory(t, auditRepo)
	if err := auditRepo.Insert("tenant-a", &audit.ProxyLog{
		RequestID:  "management-autonomy-return-management-systems",
		ProviderID: "iworkercenter",
		Model:      "management-autonomy-return",
		WorkType:   "management_autonomy_return",
		CostTier:   "internal",
		Status:     "ok",
		Summary:    "Return to autonomy confirmed for MANAGEMENT-SYSTEMS",
		ErrorMsg:   "decision_type: autonomy_return | role_code: management-systems | detail: Autonomy return confirmed. | display_time: 04/26 19:10",
		CreatedAt:  time.Now().Add(-20 * time.Second),
	}); err != nil {
		t.Fatalf("insert autonomy return audit: %v", err)
	}
	if err := auditRepo.Insert("tenant-a", &audit.ProxyLog{
		RequestID:  "executive-deposition-draft-management-systems",
		ProviderID: "iworkercenter",
		Model:      "executive-deposition-draft",
		WorkType:   "executive_deposition_draft",
		CostTier:   "internal",
		Status:     "ok",
		Summary:    "Deposition drafts generated for MANAGEMENT-SYSTEMS",
		ErrorMsg:   "role_code: management-systems | detail: Recovery learning was converted into institutional drafts. | memory_id: mem-draft | capability_id: cap-draft | workflow_id: wf-def-1",
		CreatedAt:  time.Now().Add(-10 * time.Second),
	}); err != nil {
		t.Fatalf("insert deposition draft audit: %v", err)
	}
	if err := auditRepo.Insert("tenant-a", &audit.ProxyLog{
		RequestID:  "executive-standard-published-management-systems",
		ProviderID: "iworkercenter",
		Model:      "executive-standard-published",
		WorkType:   "executive_standard_published",
		CostTier:   "internal",
		Status:     "ok",
		Summary:    "Recovery standard published for MANAGEMENT-SYSTEMS",
		ErrorMsg:   "role_code: management-systems | workflow_id: wf-def-1 | detail: The reviewed recovery workflow is now the published organizational standard.",
		CreatedAt:  time.Now(),
	}); err != nil {
		t.Fatalf("insert standard publish audit: %v", err)
	}

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

	boardHistory, ok := body["board_history"].([]any)
	if !ok || len(boardHistory) == 0 {
		t.Fatalf("board_history = %#v", body["board_history"])
	}
	foundPublishedStandard := false
	for _, raw := range boardHistory {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		title, _ := item["title"].(string)
		if strings.Contains(title, "Recovery standard published for MANAGEMENT-SYSTEMS") {
			foundPublishedStandard = true
			break
		}
	}
	if !foundPublishedStandard {
		t.Fatalf("board_history should include standard publish events: %#v", boardHistory)
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
	mustExec(t, provider, `INSERT INTO roles (id, tenant_id, name, code, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "role-1", "tenant-a", "Sales", "management-systems", "active", now, now)
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
	if err := auditRepo.Insert("tenant-a", &audit.ProxyLog{
		RequestID:  "management-decision-management-systems",
		ProviderID: "iworkercenter",
		Model:      "management-decision",
		WorkType:   "management_decision",
		CostTier:   "internal",
		Status:     "ok",
		Summary:    "Management review opened for MANAGEMENT-SYSTEMS",
		ErrorMsg:   "decision_type: review | role_code: management-systems | detail: Taken into management review at 04/26 18:30. The role is now under active management attention. | display_time: 04/26 18:30",
		CreatedAt:  time.Now().Add(-30 * time.Second),
	}); err != nil {
		t.Fatalf("insert management decision audit: %v", err)
	}
}
