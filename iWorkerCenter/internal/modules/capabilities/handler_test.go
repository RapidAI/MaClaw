package capabilities

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/audit"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/db"
)

func TestApproveCapabilityWritesAuditAndPromotesApprovedStatus(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().Format(time.RFC3339)
	if _, err := provider.Write.Exec(`INSERT INTO capability_packages (id, tenant_id, name, description, category, version, source, risk_level, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "cap-approve", "tenant-a", "Management Systems recovery handling", "draft", "recovery", "1.0.0", "executive", "medium", "active", now, now); err != nil {
		t.Fatalf("seed capability: %v", err)
	}

	auditRepo := audit.NewRepo(provider.Write, provider.Read)
	h := NewHandler(provider.Write, provider.Read)
	h.SetAuditRepo(auditRepo)

	req := httptest.NewRequest(http.MethodPost, "/admin/capabilities/cap-approve/approve", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()

	h.approveCapability(rr, req, "cap-approve")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}

	var status string
	if err := provider.Read.QueryRow(`SELECT status FROM capability_packages WHERE tenant_id=? AND id=?`, "tenant-a", "cap-approve").Scan(&status); err != nil {
		t.Fatalf("query capability status: %v", err)
	}
	if status != "approved" {
		t.Fatalf("status = %q, want approved", status)
	}

	logs, err := auditRepo.ListRecent("tenant-a", 10)
	if err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	if len(logs) == 0 {
		t.Fatalf("expected capability approval audit log")
	}
	if logs[0].WorkType != "executive_capability_approved" {
		t.Fatalf("work_type = %q", logs[0].WorkType)
	}
}

func TestClientRuntimeEntriesReturnsBoundInstalledCapabilities(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	payload := []byte("---\nname: Finance Skill\ndescription: Finance helper\n---\n# Finance Skill")
	sum := sha256.Sum256(payload)
	now := time.Now().Format(time.RFC3339)
	_, err = provider.Write.Exec(`INSERT INTO capability_packages (id, tenant_id, name, description, category, version, source, risk_level, status, package_status, package_format, package_sha256, package_size, package_content, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"cap-runtime", "tenant-a", "Finance Skill", "Finance helper", "finance", "1.0.0", "iworkercloud:finance", "low", "approved", "package_cached", "skill.md", fmt.Sprintf("%x", sum[:]), len(payload), base64.StdEncoding.EncodeToString(payload), now, now)
	if err != nil {
		t.Fatalf("seed capability: %v", err)
	}
	_, err = provider.Write.Exec(`INSERT INTO roles (id, tenant_id, name, code, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "role-worker", "tenant-a", "Worker", "worker", "active", now, now)
	if err != nil {
		t.Fatalf("seed role: %v", err)
	}
	_, err = provider.Write.Exec(`INSERT INTO colleagues (id, tenant_id, name, role_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "worker-a", "tenant-a", "Worker A", "role-worker", "active", now, now)
	if err != nil {
		t.Fatalf("seed colleague: %v", err)
	}
	h := NewHandler(provider.Write, provider.Read)
	if _, err := h.installCapabilityPackage(context.Background(), "tenant-a", "cap-runtime"); err != nil {
		t.Fatalf("install capability: %v", err)
	}
	_, err = provider.Write.Exec(`INSERT INTO colleague_capability_bindings (id, tenant_id, colleague_id, capability_id, bound_at) VALUES (?, ?, ?, ?, ?)`, "bind-1", "tenant-a", "worker-a", "cap-runtime", now)
	if err != nil {
		t.Fatalf("bind capability: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/client/capabilities?colleague_id=worker-a&runtime=1", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()
	h.handleClientCapabilities(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		RuntimeEntries []RuntimeCapabilityEntry `json:"runtime_entries"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.RuntimeEntries) != 1 || body.RuntimeEntries[0].CapabilityID != "cap-runtime" || body.RuntimeEntries[0].Entry.Name != "Finance Skill" {
		t.Fatalf("runtime entries = %+v", body.RuntimeEntries)
	}
}

func TestSelectWorkerForTaskMatchesInstalledRuntimeSkill(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().Format(time.RFC3339)
	_, err = provider.Write.Exec(`INSERT INTO roles (id, tenant_id, name, code, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "role-finance", "tenant-a", "Finance", "finance", "active", now, now)
	if err != nil {
		t.Fatalf("seed role: %v", err)
	}
	for _, id := range []string{"worker-a", "worker-b"} {
		_, err = provider.Write.Exec(`INSERT INTO colleagues (id, tenant_id, name, role_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, id, "tenant-a", id, "role-finance", "active", now, now)
		if err != nil {
			t.Fatalf("seed colleague %s: %v", id, err)
		}
	}
	h := NewHandler(provider.Write, provider.Read)
	seedInstalledCapability := func(id, name, worker string) {
		t.Helper()
		payload := []byte(fmt.Sprintf("---\nname: %s\ndescription: %s helper\ntriggers:\n  - %s\n---\n# %s", name, name, strings.ToLower(strings.ReplaceAll(name, " ", "-")), name))
		sum := sha256.Sum256(payload)
		_, err = provider.Write.Exec(`INSERT INTO capability_packages (id, tenant_id, name, description, category, version, source, risk_level, status, package_status, package_format, package_sha256, package_size, package_content, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, "tenant-a", name, name+" helper", "finance", "1.0.0", "iworkercloud:"+id, "low", "approved", "package_cached", "skill.md", fmt.Sprintf("%x", sum[:]), len(payload), base64.StdEncoding.EncodeToString(payload), now, now)
		if err != nil {
			t.Fatalf("seed capability %s: %v", id, err)
		}
		if _, err := h.installCapabilityPackage(context.Background(), "tenant-a", id); err != nil {
			t.Fatalf("install %s: %v", id, err)
		}
		_, err = provider.Write.Exec(`INSERT INTO colleague_capability_bindings (id, tenant_id, colleague_id, capability_id, bound_at) VALUES (?, ?, ?, ?, ?)`, "bind-"+id, "tenant-a", worker, id, now)
		if err != nil {
			t.Fatalf("bind %s: %v", id, err)
		}
	}
	seedInstalledCapability("cap-revenue", "Revenue Forecast", "worker-a")
	seedInstalledCapability("cap-contract", "Contract Review", "worker-b")

	selected, ok, err := h.SelectWorkerForTask(context.Background(), "tenant-a", "finance", "quarterly revenue forecast planning")
	if err != nil {
		t.Fatalf("select worker: %v", err)
	}
	if !ok || selected != "worker-a" {
		t.Fatalf("selected=%q ok=%v, want worker-a/true", selected, ok)
	}
}

func TestRecordCapabilityUsagePersistsTenantScopedFeedback(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().Format(time.RFC3339)
	_, err = provider.Write.Exec(`INSERT INTO capability_packages (id, tenant_id, name, description, category, version, source, risk_level, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "cap-usage", "tenant-a", "Usage Skill", "", "ops", "1.0.0", "local", "low", "approved", now, now)
	if err != nil {
		t.Fatalf("seed capability: %v", err)
	}
	h := NewHandler(provider.Write, provider.Read)
	if err := h.RecordCapabilityUsage(context.Background(), "tenant-a", "cap-usage", "worker-a", "wf-1", "step-1", "", "done", "", 42, 0, ""); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	if err := h.RecordCapabilityUsage(context.Background(), "tenant-b", "cap-usage", "worker-b", "wf-2", "step-2", "", "done", "", 0, 0, ""); err == nil {
		t.Fatalf("expected tenant-b usage to be rejected because capability is tenant-scoped")
	}

	items, err := h.listCapabilityUsageEvents(context.Background(), "tenant-a", "cap-usage", 10)
	if err != nil {
		t.Fatalf("list usage: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("usage event count = %d, want 1", len(items))
	}
	if items[0].Status != "success" || items[0].ColleagueID != "worker-a" || items[0].WorkflowStepInstanceID != "step-1" || items[0].LatencyMs != 42 || items[0].QualityScore != 80 {
		t.Fatalf("usage event = %+v", items[0])
	}
}

func TestRecordCapabilityUsagePreservesReportedQualityScore(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().Format(time.RFC3339)
	_, err = provider.Write.Exec(`INSERT INTO capability_packages (id, tenant_id, name, description, category, version, source, risk_level, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "cap-quality", "tenant-a", "Quality Skill", "", "ops", "1.0.0", "local", "low", "approved", now, now)
	if err != nil {
		t.Fatalf("seed capability: %v", err)
	}
	h := NewHandler(provider.Write, provider.Read)
	if err := h.RecordCapabilityUsage(context.Background(), "tenant-a", "cap-quality", "worker-a", "wf-1", "step-1", "success", "done", "", 10, 95, "human approved"); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	items, err := h.listCapabilityUsageEvents(context.Background(), "tenant-a", "cap-quality", 10)
	if err != nil {
		t.Fatalf("list usage: %v", err)
	}
	if len(items) != 1 || items[0].QualityScore != 95 || items[0].QualityReason != "human approved" {
		t.Fatalf("usage events = %+v", items)
	}
}

func TestCapabilityUsageSummaryAndQualityUpdate(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().Format(time.RFC3339)
	_, err = provider.Write.Exec(`INSERT INTO capability_packages (id, tenant_id, name, description, category, version, source, risk_level, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "cap-summary", "tenant-a", "Summary Skill", "", "ops", "1.0.0", "local", "low", "approved", now, now)
	if err != nil {
		t.Fatalf("seed capability: %v", err)
	}
	h := NewHandler(provider.Write, provider.Read)
	if err := h.RecordCapabilityUsage(context.Background(), "tenant-a", "cap-summary", "worker-a", "wf-1", "step-1", "success", "done", "", 100, 90, "validated"); err != nil {
		t.Fatalf("record success: %v", err)
	}
	if err := h.RecordCapabilityUsage(context.Background(), "tenant-a", "cap-summary", "worker-a", "wf-2", "step-2", "failure", "", "boom", 300, 0, ""); err != nil {
		t.Fatalf("record failure: %v", err)
	}

	summary, err := h.capabilityUsageSummary(context.Background(), "tenant-a", "cap-summary")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.Total != 2 || summary.Successes != 1 || summary.Failures != 1 || summary.SuccessRate != 0.5 {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.AverageQuality != 55 || summary.AverageLatencyMs != 200 {
		t.Fatalf("summary averages = %+v", summary)
	}

	items, err := h.listCapabilityUsageEvents(context.Background(), "tenant-a", "cap-summary", 10)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("event count = %d", len(items))
	}
	if err := h.UpdateCapabilityUsageQuality(context.Background(), "tenant-a", "cap-summary", items[0].ID, 75, "manual review"); err != nil {
		t.Fatalf("update quality: %v", err)
	}
	items, err = h.listCapabilityUsageEvents(context.Background(), "tenant-a", "cap-summary", 10)
	if err != nil {
		t.Fatalf("list after update: %v", err)
	}
	found := false
	for _, item := range items {
		if item.ID == items[0].ID && item.QualityScore == 75 && item.QualityReason == "manual review" {
			found = true
		}
	}
	if !found {
		t.Fatalf("updated event not found: %+v", items)
	}
}

func TestInstallCapabilityBuildsRuntimeEntry(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	payload := []byte("---\nname: Revenue Watch\ndescription: Watch revenue signals\ntriggers:\n  - revenue-watch\n---\n# Revenue Watch\nCheck revenue movement.")
	sum := sha256.Sum256(payload)
	now := time.Now().Format(time.RFC3339)
	_, err = provider.Write.Exec(`INSERT INTO capability_packages (id, tenant_id, name, description, category, version, source, risk_level, status, package_status, package_format, package_sha256, package_size, package_content, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"cap-install", "tenant-a", "Revenue Watch", "Watch revenue signals", "ops", "1.0.0", "iworkercloud:revenue-watch", "low", "pending_review", "package_cached", "skill.md", fmt.Sprintf("%x", sum[:]), len(payload), base64.StdEncoding.EncodeToString(payload), now, now)
	if err != nil {
		t.Fatalf("seed capability: %v", err)
	}

	h := NewHandler(provider.Write, provider.Read)
	req := httptest.NewRequest(http.MethodPost, "/admin/capabilities/cap-install/install", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()
	h.installCapability(rr, req, "cap-install")
	if rr.Code != http.StatusOK {
		t.Fatalf("install status = %d body=%s", rr.Code, rr.Body.String())
	}

	var packageStatus, installedJSON string
	if err := provider.Read.QueryRow(`SELECT package_status, installed_entry_json FROM capability_packages WHERE tenant_id=? AND id=?`, "tenant-a", "cap-install").Scan(&packageStatus, &installedJSON); err != nil {
		t.Fatalf("query installed capability: %v", err)
	}
	if packageStatus != "installed" || installedJSON == "" {
		t.Fatalf("packageStatus=%q installedJSON=%q", packageStatus, installedJSON)
	}

	clientReq := httptest.NewRequest(http.MethodGet, "/client/capabilities/cap-install/runtime-entry", nil)
	clientReq = clientReq.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	clientRR := httptest.NewRecorder()
	h.getClientRuntimeEntry(clientRR, clientReq, "cap-install")
	if clientRR.Code != http.StatusOK {
		t.Fatalf("runtime entry status = %d body=%s", clientRR.Code, clientRR.Body.String())
	}
	if !strings.Contains(clientRR.Body.String(), "Revenue Watch") {
		t.Fatalf("runtime entry body = %s", clientRR.Body.String())
	}
}
