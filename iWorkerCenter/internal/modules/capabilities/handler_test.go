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

func TestPublishCapabilityToCloudUsesRuleAndAdminEmail(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var received struct {
		AuthorEmail    string `json:"author_email"`
		SourceCenterID string `json:"source_center_id"`
		Price          int64  `json:"price"`
	}
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/centers/center-a/skills" || r.Header.Get("X-Center-Secret") != "secret-a" {
			t.Fatalf("unexpected cloud request path=%s secret=%s", r.URL.Path, r.Header.Get("X-Center-Secret"))
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode cloud request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"center-center-a-cap-local","name":"Local Skill","author_email":"admin@example.com","source_center_id":"center-a","price":25}`))
	}))
	defer cloud.Close()
	now := time.Now().Format(time.RFC3339)
	_, err = provider.Write.Exec(`INSERT INTO tenants (id, company_name, email, cloud_center_id, cloud_secret, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'active', ?, ?)`, "tenant-a", "Tenant A", "admin@example.com", "center-a", "secret-a", now, now)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	payload := []byte("---\nname: Local Skill\ndescription: Local helper\n---\n# Local Skill")
	sum := sha256.Sum256(payload)
	_, err = provider.Write.Exec(`INSERT INTO capability_packages (id, tenant_id, name, description, category, version, source, risk_level, status, package_status, package_format, package_sha256, package_size, package_content, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "cap-local", "tenant-a", "Local Skill", "Local helper", "ops", "1.0.0", "iworker:self_summary", "low", "approved", "package_cached", "skill.md", fmt.Sprintf("%x", sum[:]), len(payload), base64.StdEncoding.EncodeToString(payload), now, now)
	if err != nil {
		t.Fatalf("seed capability: %v", err)
	}
	auditRepo := audit.NewRepo(provider.Write, provider.Read)
	h := NewHandler(provider.Write, provider.Read)
	h.SetAuditRepo(auditRepo)
	h.SetCloudImporterResolver(cloud.URL, nil)
	rule, err := h.SetCloudPublishRule(context.Background(), "tenant-a", CloudPublishRule{Enabled: true, MinUsageCount: 1, MinSuccessRate: 0.5, MinAverageQuality: 70, DefaultPricing: "paid", DefaultPrice: 25, RequirePackageCached: true})
	if err != nil {
		t.Fatalf("set rule: %v", err)
	}
	if !rule.Enabled || rule.DefaultPrice != 25 {
		t.Fatalf("rule = %+v", rule)
	}
	if err := h.RecordCapabilityUsage(context.Background(), "tenant-a", "cap-local", "worker-a", "wf-1", "step-1", "success", "done", "", 100, 90, "validated"); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	skill, err := h.PublishCapabilityToCloud(context.Background(), "tenant-a", "cap-local", CloudPublishRequest{})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if skill.ID != "center-center-a-cap-local" || received.AuthorEmail != "admin@example.com" || received.SourceCenterID != "center-a" || received.Price != 25 {
		t.Fatalf("skill=%+v received=%+v", skill, received)
	}
	var status string
	if err := provider.Read.QueryRow(`SELECT cloud_publish_status FROM capability_packages WHERE tenant_id=? AND id=?`, "tenant-a", "cap-local").Scan(&status); err != nil {
		t.Fatalf("query publish status: %v", err)
	}
	if status != "published" {
		t.Fatalf("publish status = %q", status)
	}
}

func TestPublishCapabilityToCloudRejectsImmatureSkill(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().Format(time.RFC3339)
	_, _ = provider.Write.Exec(`INSERT INTO tenants (id, company_name, email, cloud_center_id, cloud_secret, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'active', ?, ?)`, "tenant-a", "Tenant A", "admin@example.com", "center-a", "secret-a", now, now)
	_, _ = provider.Write.Exec(`INSERT INTO capability_packages (id, tenant_id, name, description, category, version, source, risk_level, status, package_status, package_format, package_content, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "cap-immature", "tenant-a", "Immature", "", "ops", "1.0.0", "local", "low", "approved", "package_cached", "skill.md", base64.StdEncoding.EncodeToString([]byte("# Immature")), now, now)
	h := NewHandler(provider.Write, provider.Read)
	h.SetCloudImporterResolver("http://cloud.invalid", nil)
	_, _ = h.SetCloudPublishRule(context.Background(), "tenant-a", CloudPublishRule{Enabled: true, MinUsageCount: 2, MinSuccessRate: 0.8, MinAverageQuality: 80, RequirePackageCached: true})
	if _, err := h.PublishCapabilityToCloud(context.Background(), "tenant-a", "cap-immature", CloudPublishRequest{}); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("expected immature usage error, got %v", err)
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

func TestAdminSkillMarketFiltersAndMaturity(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().Format(time.RFC3339)
	payload := []byte("---\nname: Mature Summary\n---\n# Mature Summary")
	sum := sha256.Sum256(payload)
	_, err = provider.Write.Exec(`INSERT INTO capability_packages (id, tenant_id, name, description, category, version, source, risk_level, status, package_status, package_format, package_sha256, package_size, package_content, local_skill_origin, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "cap-mature", "tenant-a", "Mature Summary", "self learned", "ops", "1.0.0", "iworker:self_summary", "low", "approved", "package_cached", "skill.md", fmt.Sprintf("%x", sum[:]), len(payload), base64.StdEncoding.EncodeToString(payload), "iworker_summary", now, now)
	if err != nil {
		t.Fatalf("seed mature capability: %v", err)
	}
	_, err = provider.Write.Exec(`INSERT INTO capability_packages (id, tenant_id, name, description, category, version, source, risk_level, status, package_status, package_format, package_content, local_skill_origin, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "cap-cloud", "tenant-a", "Cloud Imported", "from cloud", "ops", "1.0.0", "iworkercloud:cloud-skill", "low", "pending_review", "metadata_only", "", "", "cloud_imported", now, now)
	if err != nil {
		t.Fatalf("seed cloud capability: %v", err)
	}
	h := NewHandler(provider.Write, provider.Read)
	_, err = h.SetCloudPublishRule(context.Background(), "tenant-a", CloudPublishRule{Enabled: true, MinUsageCount: 1, MinSuccessRate: 0.8, MinAverageQuality: 80, RequirePackageCached: true})
	if err != nil {
		t.Fatalf("set rule: %v", err)
	}
	if err := h.RecordCapabilityUsage(context.Background(), "tenant-a", "cap-mature", "worker-a", "wf-1", "step-1", "success", "done", "", 20, 95, "validated"); err != nil {
		t.Fatalf("record usage: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/skillmarket?origin=iworker_summary&mature=true", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()
	h.handleAdminSkillMarket(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Skills []SkillMarketItem `json:"skills"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Skills) != 1 || body.Skills[0].ID != "cap-mature" || !body.Skills[0].Mature || body.Skills[0].UsageSummary.Total != 1 {
		t.Fatalf("skills = %+v", body.Skills)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/skillmarket?origin=cloud_imported&mature=false", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr = httptest.NewRecorder()
	h.handleAdminSkillMarket(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	body.Skills = nil
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode cloud response: %v", err)
	}
	if len(body.Skills) != 1 || body.Skills[0].ID != "cap-cloud" || body.Skills[0].Mature {
		t.Fatalf("cloud skills = %+v", body.Skills)
	}
	if len(body.Skills[0].MaturityReasons) == 0 {
		t.Fatalf("expected immature reasons")
	}
}

func TestAdminSkillMarketDetailIncludesMaturity(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().Format(time.RFC3339)
	payload := []byte("# Detail Skill")
	_, err = provider.Write.Exec(`INSERT INTO capability_packages (id, tenant_id, name, description, category, version, source, risk_level, status, package_status, package_format, package_content, local_skill_origin, cloud_publish_status, cloud_skill_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "cap-detail", "tenant-a", "Detail Skill", "detail", "ops", "1.0.0", "local", "low", "approved", "package_cached", "skill.md", base64.StdEncoding.EncodeToString(payload), "local", "published", "cloud-cap-detail", now, now)
	if err != nil {
		t.Fatalf("seed detail capability: %v", err)
	}
	h := NewHandler(provider.Write, provider.Read)
	_, err = h.SetCloudPublishRule(context.Background(), "tenant-a", CloudPublishRule{Enabled: true, MinUsageCount: 0, MinSuccessRate: 0, MinAverageQuality: 0, RequirePackageCached: true})
	if err != nil {
		t.Fatalf("set rule: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/skillmarket/cap-detail", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var item SkillMarketItem
	if err := json.NewDecoder(rr.Body).Decode(&item); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if item.ID != "cap-detail" || item.CloudPublishStatus != "published" || item.CloudSkillID != "cloud-cap-detail" || !item.Mature {
		t.Fatalf("item = %+v", item)
	}
}

func TestAdminSkillMarketSafetyActionsQuarantineRestoreAndDelete(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().Format(time.RFC3339)
	_, err = provider.Write.Exec(`INSERT INTO capability_packages (id, tenant_id, name, description, category, version, source, risk_level, status, package_status, package_format, package_content, local_skill_origin, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "cap-risk", "tenant-a", "Risk Skill", "risk", "ops", "1.0.0", "local", "high", "approved", "package_cached", "skill.md", base64.StdEncoding.EncodeToString([]byte("# Risk")), "local", now, now)
	if err != nil {
		t.Fatalf("seed skill: %v", err)
	}
	_, err = provider.Write.Exec(`INSERT INTO roles (id, tenant_id, name, code, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "role-risk", "tenant-a", "Risk", "risk", "active", now, now)
	if err != nil {
		t.Fatalf("seed role: %v", err)
	}
	_, err = provider.Write.Exec(`INSERT INTO colleagues (id, tenant_id, name, role_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "worker-a", "tenant-a", "Worker A", "role-risk", "active", now, now)
	if err != nil {
		t.Fatalf("seed colleague: %v", err)
	}
	_, err = provider.Write.Exec(`INSERT INTO colleague_capability_bindings (id, tenant_id, colleague_id, capability_id, bound_at) VALUES (?, ?, ?, ?, ?)`, "bind-risk", "tenant-a", "worker-a", "cap-risk", now)
	if err != nil {
		t.Fatalf("seed binding: %v", err)
	}
	h := NewHandler(provider.Write, provider.Read)
	_, _ = h.SetCloudPublishRule(context.Background(), "tenant-a", CloudPublishRule{Enabled: true, MinUsageCount: 0, MinSuccessRate: 0, MinAverageQuality: 0, RequirePackageCached: true})

	req := httptest.NewRequest(http.MethodPost, "/admin/skillmarket/cap-risk/safety", strings.NewReader(`{"action":"quarantine","reason":"unsafe browser operation"}`))
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("quarantine status=%d body=%s", rr.Code, rr.Body.String())
	}
	item, err := h.getInternalSkillMarketItem(httptest.NewRequest(http.MethodGet, "/admin/skillmarket/cap-risk", nil), "tenant-a", "cap-risk")
	if err != nil {
		t.Fatalf("get quarantined skill: %v", err)
	}
	if item.SafetyStatus != "quarantined" || item.Mature || !containsString(item.MaturityReasons, "skill_quarantined_by_human_safety_review") {
		t.Fatalf("quarantined item = %+v", item)
	}
	if _, err := h.PublishCapabilityToCloud(context.Background(), "tenant-a", "cap-risk", CloudPublishRequest{}); err == nil || !strings.Contains(err.Error(), "quarantined") {
		t.Fatalf("expected quarantined publish rejection, got %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/admin/skillmarket/cap-risk/safety", strings.NewReader(`{"action":"restore"}`))
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr = httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("restore status=%d body=%s", rr.Code, rr.Body.String())
	}
	var safetyStatus string
	if err := provider.Read.QueryRow(`SELECT safety_status FROM capability_packages WHERE tenant_id=? AND id=?`, "tenant-a", "cap-risk").Scan(&safetyStatus); err != nil {
		t.Fatalf("query restored skill: %v", err)
	}
	if safetyStatus != "" {
		t.Fatalf("safety status after restore = %q", safetyStatus)
	}

	result, err := h.ApplySkillSafetyAction(context.Background(), "tenant-a", "cap-risk", SkillSafetyRequest{Action: "delete", Reason: "confirmed harmful"})
	if err != nil {
		t.Fatalf("delete skill: %v", err)
	}
	if result["status"] != "deleted" {
		t.Fatalf("delete result = %+v", result)
	}
	var count int
	if err := provider.Read.QueryRow(`SELECT COUNT(*) FROM capability_packages WHERE tenant_id=? AND id=?`, "tenant-a", "cap-risk").Scan(&count); err != nil {
		t.Fatalf("count skill: %v", err)
	}
	if count != 0 {
		t.Fatalf("skill count after delete = %d", count)
	}
	if err := provider.Read.QueryRow(`SELECT COUNT(*) FROM colleague_capability_bindings WHERE tenant_id=? AND capability_id=?`, "tenant-a", "cap-risk").Scan(&count); err != nil {
		t.Fatalf("count bindings: %v", err)
	}
	if count != 0 {
		t.Fatalf("binding count after delete = %d", count)
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func TestSkillMarketEvolutionCandidatesRecommendAutonomousActions(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().Format(time.RFC3339)
	payload := base64.StdEncoding.EncodeToString([]byte("# Skill"))
	seedSkill := func(id, name, safetyStatus, cloudStatus string) {
		t.Helper()
		_, err := provider.Write.Exec(`INSERT INTO capability_packages (id, tenant_id, name, description, category, version, source, risk_level, status, package_status, package_format, package_content, local_skill_origin, safety_status, safety_reason, cloud_publish_status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, "tenant-a", name, name, "ops", "1.0.0", "local", "low", "approved", "package_cached", "skill.md", payload, "local", safetyStatus, safetyStatus, cloudStatus, now, now)
		if err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seedSkill("cap-ready", "Ready", "", "")
	seedSkill("cap-learning", "Learning", "", "")
	seedSkill("cap-safe", "Blocked", "quarantined", "blocked")
	seedSkill("cap-published", "Published", "", "published")
	h := NewHandler(provider.Write, provider.Read)
	_, _ = h.SetCloudPublishRule(context.Background(), "tenant-a", CloudPublishRule{Enabled: true, MinUsageCount: 1, MinSuccessRate: 0.8, MinAverageQuality: 80, RequirePackageCached: true})
	if err := h.RecordCapabilityUsage(context.Background(), "tenant-a", "cap-ready", "worker-a", "wf-1", "step-1", "success", "done", "", 10, 95, "validated"); err != nil {
		t.Fatalf("record ready usage: %v", err)
	}
	if err := h.RecordCapabilityUsage(context.Background(), "tenant-a", "cap-published", "worker-a", "wf-2", "step-2", "success", "done", "", 10, 90, "validated"); err != nil {
		t.Fatalf("record published usage: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/skillmarket/evolution-candidates", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Candidates []SkillEvolutionCandidate `json:"candidates"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode candidates: %v", err)
	}
	byID := map[string]SkillEvolutionCandidate{}
	for _, candidate := range body.Candidates {
		byID[candidate.ID] = candidate
	}
	if byID["cap-ready"].Recommendation != "publish_to_cloud_candidate" || !byID["cap-ready"].Autonomous || byID["cap-ready"].HumanInterventionRequired {
		t.Fatalf("ready candidate = %+v", byID["cap-ready"])
	}
	if byID["cap-learning"].Recommendation != "continue_learning" || !byID["cap-learning"].Autonomous {
		t.Fatalf("learning candidate = %+v", byID["cap-learning"])
	}
	if byID["cap-safe"].Recommendation != "blocked_by_human_safety_review" || !byID["cap-safe"].HumanInterventionRequired {
		t.Fatalf("safe candidate = %+v", byID["cap-safe"])
	}
	if byID["cap-published"].Recommendation != "monitor_market_feedback" {
		t.Fatalf("published candidate = %+v", byID["cap-published"])
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/skillmarket/evolution-candidates?ready=true", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr = httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("ready status=%d body=%s", rr.Code, rr.Body.String())
	}
	body.Candidates = nil
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode ready candidates: %v", err)
	}
	if len(body.Candidates) != 1 || body.Candidates[0].ID != "cap-ready" {
		t.Fatalf("ready candidates = %+v", body.Candidates)
	}
}

func TestSkillMarketEvolutionRunPublishesReadyCandidates(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	publishedCalls := 0
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/centers/center-a/skills" || r.Header.Get("X-Center-Secret") != "secret-a" {
			t.Fatalf("unexpected cloud request path=%s secret=%s", r.URL.Path, r.Header.Get("X-Center-Secret"))
		}
		publishedCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"center-center-a-cap-ready","name":"Ready"}`))
	}))
	defer cloud.Close()

	now := time.Now().Format(time.RFC3339)
	_, err = provider.Write.Exec(`INSERT INTO tenants (id, company_name, email, cloud_center_id, cloud_secret, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'active', ?, ?)`, "tenant-a", "Tenant A", "admin@example.com", "center-a", "secret-a", now, now)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	payload := []byte("# Ready")
	sum := sha256.Sum256(payload)
	_, err = provider.Write.Exec(`INSERT INTO capability_packages (id, tenant_id, name, description, category, version, source, risk_level, status, package_status, package_format, package_sha256, package_size, package_content, local_skill_origin, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "cap-ready", "tenant-a", "Ready", "ready", "ops", "1.0.0", "local", "low", "approved", "package_cached", "skill.md", fmt.Sprintf("%x", sum[:]), len(payload), base64.StdEncoding.EncodeToString(payload), "local", now, now)
	if err != nil {
		t.Fatalf("seed skill: %v", err)
	}
	h := NewHandler(provider.Write, provider.Read)
	h.SetCloudImporterResolver(cloud.URL, nil)
	_, _ = h.SetCloudPublishRule(context.Background(), "tenant-a", CloudPublishRule{Enabled: true, MinUsageCount: 1, MinSuccessRate: 0.8, MinAverageQuality: 80, DefaultPricing: "free", RequirePackageCached: true})
	if err := h.RecordCapabilityUsage(context.Background(), "tenant-a", "cap-ready", "worker-a", "wf-1", "step-1", "success", "done", "", 10, 95, "validated"); err != nil {
		t.Fatalf("record usage: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/skillmarket/evolution-run", strings.NewReader(`{"dry_run":true,"limit":5}`))
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("dry run status=%d body=%s", rr.Code, rr.Body.String())
	}
	var dry SkillEvolutionRunSummary
	if err := json.NewDecoder(rr.Body).Decode(&dry); err != nil {
		t.Fatalf("decode dry run: %v", err)
	}
	if !dry.DryRun || dry.Attempted != 1 || len(dry.Results) != 1 || dry.Results[0].Status != "would_publish" || publishedCalls != 0 || dry.StartedAt == "" || dry.FinishedAt == "" {
		t.Fatalf("dry run=%+v calls=%d", dry, publishedCalls)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/skillmarket/evolution-status", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr = httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("dry status=%d body=%s", rr.Code, rr.Body.String())
	}
	var statusBeforeRun SkillEvolutionStatus
	if err := json.NewDecoder(rr.Body).Decode(&statusBeforeRun); err != nil {
		t.Fatalf("decode status before run: %v", err)
	}
	if statusBeforeRun.LeaseActive || statusBeforeRun.LastRun != nil {
		t.Fatalf("status before real run = %+v", statusBeforeRun)
	}

	req = httptest.NewRequest(http.MethodPost, "/admin/skillmarket/evolution-run", strings.NewReader(`{"limit":5}`))
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr = httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("run status=%d body=%s", rr.Code, rr.Body.String())
	}
	var summary SkillEvolutionRunSummary
	if err := json.NewDecoder(rr.Body).Decode(&summary); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if summary.Published != 1 || summary.Failed != 0 || publishedCalls != 1 || summary.Results[0].CloudSkillID != "center-center-a-cap-ready" {
		t.Fatalf("summary=%+v calls=%d", summary, publishedCalls)
	}
	var status string
	if err := provider.Read.QueryRow(`SELECT cloud_publish_status FROM capability_packages WHERE tenant_id=? AND id=?`, "tenant-a", "cap-ready").Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "published" {
		t.Fatalf("publish status = %q", status)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/skillmarket/evolution-status", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr = httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status after run=%d body=%s", rr.Code, rr.Body.String())
	}
	var evolutionStatus SkillEvolutionStatus
	if err := json.NewDecoder(rr.Body).Decode(&evolutionStatus); err != nil {
		t.Fatalf("decode status after run: %v", err)
	}
	if evolutionStatus.LeaseActive || evolutionStatus.LastRun == nil || evolutionStatus.LastRun.Published != 1 || evolutionStatus.LastRun.StartedAt == "" || evolutionStatus.LastRun.FinishedAt == "" {
		t.Fatalf("evolution status after run = %+v", evolutionStatus)
	}
}

func TestSkillEvolutionStatusReportsActiveLease(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h := NewHandler(provider.Write, provider.Read)

	req := httptest.NewRequest(http.MethodGet, "/admin/skillmarket/evolution-status", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("initial status=%d body=%s", rr.Code, rr.Body.String())
	}
	var initial SkillEvolutionStatus
	if err := json.NewDecoder(rr.Body).Decode(&initial); err != nil {
		t.Fatalf("decode initial status: %v", err)
	}
	if initial.LeaseActive || initial.LastRun != nil {
		t.Fatalf("initial status = %+v", initial)
	}

	owner, acquired, err := h.acquireSkillEvolutionRunLease(context.Background(), "tenant-a", time.Minute)
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
	if !acquired {
		t.Fatalf("expected lease acquisition")
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/skillmarket/evolution-status", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr = httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("active status=%d body=%s", rr.Code, rr.Body.String())
	}
	var active SkillEvolutionStatus
	if err := json.NewDecoder(rr.Body).Decode(&active); err != nil {
		t.Fatalf("decode active status: %v", err)
	}
	if !active.LeaseActive || active.LeaseOwner != owner || active.LeaseExpiresAt == "" {
		t.Fatalf("active status = %+v owner=%q", active, owner)
	}
}

type fakeSkillEvolutionTenantLister struct {
	tenants []*tenant.Tenant
	err     error
}

func (f fakeSkillEvolutionTenantLister) ListActiveTenants(ctx context.Context) ([]*tenant.Tenant, error) {
	return f.tenants, f.err
}

func TestSkillEvolutionMonitorPublishesReadySkillsAndReportsStatus(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	publishedCalls := 0
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/centers/center-a/skills" || r.Header.Get("X-Center-Secret") != "secret-a" {
			t.Fatalf("unexpected cloud request path=%s secret=%s", r.URL.Path, r.Header.Get("X-Center-Secret"))
		}
		publishedCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"center-center-a-cap-monitor","name":"Monitor Skill"}`))
	}))
	defer cloud.Close()

	now := time.Now().Format(time.RFC3339)
	_, err = provider.Write.Exec(`INSERT INTO tenants (id, company_name, email, cloud_center_id, cloud_secret, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'active', ?, ?)`, "tenant-a", "Tenant A", "admin@example.com", "center-a", "secret-a", now, now)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	payload := []byte("# Monitor Skill")
	sum := sha256.Sum256(payload)
	_, err = provider.Write.Exec(`INSERT INTO capability_packages (id, tenant_id, name, description, category, version, source, risk_level, status, package_status, package_format, package_sha256, package_size, package_content, local_skill_origin, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "cap-monitor", "tenant-a", "Monitor Skill", "ready", "ops", "1.0.0", "local", "low", "approved", "package_cached", "skill.md", fmt.Sprintf("%x", sum[:]), len(payload), base64.StdEncoding.EncodeToString(payload), "local", now, now)
	if err != nil {
		t.Fatalf("seed skill: %v", err)
	}
	auditRepo := audit.NewRepo(provider.Write, provider.Read)
	h := NewHandler(provider.Write, provider.Read)
	h.SetAuditRepo(auditRepo)
	h.SetCloudImporterResolver(cloud.URL, nil)
	_, _ = h.SetCloudPublishRule(context.Background(), "tenant-a", CloudPublishRule{Enabled: true, MinUsageCount: 1, MinSuccessRate: 0.8, MinAverageQuality: 80, RequirePackageCached: true})
	if err := h.RecordCapabilityUsage(context.Background(), "tenant-a", "cap-monitor", "worker-a", "wf-1", "step-1", "success", "done", "", 10, 95, "validated"); err != nil {
		t.Fatalf("record usage: %v", err)
	}

	_, _ = h.SetSkillEvolutionAutomationRule(context.Background(), "tenant-a", SkillEvolutionAutomationRule{Enabled: true, IntervalSeconds: 60, Limit: 5})
	monitor := NewSkillEvolutionMonitor(h, fakeSkillEvolutionTenantLister{tenants: []*tenant.Tenant{{ID: "tenant-a"}}}, SkillEvolutionMonitorConfig{Interval: time.Hour, Limit: 5})
	monitor.runAll(time.Now().UTC())
	status := monitor.Status()
	if status.Running || status.Config.DefaultLimit != 5 || len(status.Tenants) != 1 {
		t.Fatalf("monitor status = %+v", status)
	}
	tenantStatus := status.Tenants[0]
	if tenantStatus.TenantID != "tenant-a" || !tenantStatus.AutomationEnabled || tenantStatus.Limit != 5 || tenantStatus.Published != 1 || tenantStatus.Attempted != 1 || tenantStatus.Error != "" || publishedCalls != 1 {
		t.Fatalf("tenant status=%+v calls=%d", tenantStatus, publishedCalls)
	}

	var publishStatus string
	if err := provider.Read.QueryRow(`SELECT cloud_publish_status FROM capability_packages WHERE tenant_id=? AND id=?`, "tenant-a", "cap-monitor").Scan(&publishStatus); err != nil {
		t.Fatalf("query publish status: %v", err)
	}
	if publishStatus != "published" {
		t.Fatalf("publish status = %q", publishStatus)
	}
	logs, err := auditRepo.ListRecent("tenant-a", 5)
	if err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	foundMonitorAudit := false
	for _, log := range logs {
		if log.WorkType == "skill_evolution_monitor" && log.Status == "ok" && strings.Contains(log.ErrorMsg, "published=1") {
			foundMonitorAudit = true
			break
		}
	}
	if !foundMonitorAudit {
		t.Fatalf("audit logs = %+v", logs)
	}
}

func TestSkillEvolutionMonitorStatusEndpoint(t *testing.T) {
	h := NewHandler(nil, nil)
	monitor := NewSkillEvolutionMonitor(h, fakeSkillEvolutionTenantLister{}, SkillEvolutionMonitorConfig{Interval: time.Hour, Limit: 7})
	now := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	monitor.recordStatus(SkillEvolutionTenantStatus{TenantID: "tenant-a", StartedAt: now, FinishedAt: now.Add(time.Second), Scanned: 3, Attempted: 1, Published: 1})
	h.SetSkillEvolutionMonitor(monitor)

	req := httptest.NewRequest(http.MethodGet, "/admin/skillmarket/evolution-monitor-status", nil)
	rr := httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body SkillEvolutionMonitorStatus
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Config.DefaultLimit != 7 || len(body.Tenants) != 1 || body.Tenants[0].Published != 1 {
		t.Fatalf("body = %+v", body)
	}
}

func TestSkillEvolutionAutomationRuleEndpoint(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h := NewHandler(provider.Write, provider.Read)

	req := httptest.NewRequest(http.MethodGet, "/admin/skillmarket/evolution-automation-rule", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rr.Code, rr.Body.String())
	}
	var initial SkillEvolutionAutomationRule
	if err := json.NewDecoder(rr.Body).Decode(&initial); err != nil {
		t.Fatalf("decode initial: %v", err)
	}
	if !initial.Enabled || initial.IntervalSeconds <= 0 || initial.Limit != 20 {
		t.Fatalf("initial rule = %+v", initial)
	}

	req = httptest.NewRequest(http.MethodPut, "/admin/skillmarket/evolution-automation-rule", strings.NewReader(`{"enabled":false,"interval_seconds":30,"limit":999}`))
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr = httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", rr.Code, rr.Body.String())
	}
	var saved SkillEvolutionAutomationRule
	if err := json.NewDecoder(rr.Body).Decode(&saved); err != nil {
		t.Fatalf("decode saved: %v", err)
	}
	if saved.Enabled || saved.IntervalSeconds != 60 || saved.Limit != 20 {
		t.Fatalf("saved rule = %+v", saved)
	}
}

func TestSkillEvolutionMonitorSkipsDisabledAndIntervalNotReached(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	auditRepo := audit.NewRepo(provider.Write, provider.Read)
	h := NewHandler(provider.Write, provider.Read)
	h.SetAuditRepo(auditRepo)
	monitor := NewSkillEvolutionMonitor(h, fakeSkillEvolutionTenantLister{tenants: []*tenant.Tenant{{ID: "tenant-a"}}}, SkillEvolutionMonitorConfig{Interval: time.Hour, Limit: 5})

	_, _ = h.SetSkillEvolutionAutomationRule(context.Background(), "tenant-a", SkillEvolutionAutomationRule{Enabled: false, IntervalSeconds: 60, Limit: 5})
	monitor.runAll(now)
	status := monitor.Status()
	if len(status.Tenants) != 1 || status.Tenants[0].SkipReason != "automation_disabled" || status.Tenants[0].AutomationEnabled {
		t.Fatalf("disabled status = %+v", status)
	}

	_, _ = h.SetSkillEvolutionAutomationRule(context.Background(), "tenant-a", SkillEvolutionAutomationRule{Enabled: true, IntervalSeconds: 3600, Limit: 5})
	_ = h.storeLastSkillEvolutionRun(context.Background(), "tenant-a", SkillEvolutionRunSummary{StartedAt: now.Add(-time.Minute).Format(time.RFC3339Nano), FinishedAt: now.Add(-time.Minute).Format(time.RFC3339Nano)})
	monitor.runAll(now)
	status = monitor.Status()
	if len(status.Tenants) != 1 || status.Tenants[0].SkipReason != "interval_not_reached" || !status.Tenants[0].AutomationEnabled {
		t.Fatalf("interval status = %+v", status)
	}
	logs, err := auditRepo.ListRecent("tenant-a", 5)
	if err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	if len(logs) < 2 || logs[0].WorkType != "skill_evolution_monitor" || !strings.Contains(logs[0].Summary, "skipped") {
		t.Fatalf("skip audit logs = %+v", logs)
	}
}

func TestSkillEvolutionHistoryEndpointFiltersEvolutionAudit(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	auditRepo := audit.NewRepo(provider.Write, provider.Read)
	h := NewHandler(provider.Write, provider.Read)
	h.SetAuditRepo(auditRepo)
	if err := auditRepo.Insert("tenant-a", &audit.ProxyLog{RequestID: "other", ProviderID: "iworkercenter", Model: "other", WorkType: "other_work", CostTier: "internal", Status: "ok", Summary: "ignore me"}); err != nil {
		t.Fatalf("insert unrelated audit: %v", err)
	}
	h.recordSkillEvolutionRunAudit("tenant-a", SkillEvolutionRunSummary{Scanned: 3, Attempted: 1, Published: 1, Skipped: 2})
	monitor := NewSkillEvolutionMonitor(h, fakeSkillEvolutionTenantLister{}, SkillEvolutionMonitorConfig{Interval: time.Hour, Limit: 7})
	monitor.recordAudit("tenant-a", SkillEvolutionTenantStatus{TenantID: "tenant-a", AutomationEnabled: true, IntervalSeconds: 60, Limit: 7, SkipReason: "interval_not_reached"})

	req := httptest.NewRequest(http.MethodGet, "/admin/skillmarket/evolution-history?limit=10", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		History []SkillEvolutionHistoryItem `json:"history"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(body.History) != 2 {
		t.Fatalf("history = %+v", body.History)
	}
	bySource := map[string]SkillEvolutionHistoryItem{}
	for _, item := range body.History {
		bySource[item.Source] = item
		if item.Source == "unknown" || item.Summary == "ignore me" {
			t.Fatalf("unexpected history item: %+v", item)
		}
	}
	if bySource["manual_run"].Source == "" || bySource["manual_run"].DetailFields["published"] != "1" || bySource["monitor"].Source == "" || bySource["monitor"].DetailFields["skip_reason"] != "interval_not_reached" {
		t.Fatalf("history sources = %+v", bySource)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/skillmarket/evolution-history?source=monitor&status=ok", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr = httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("filtered status=%d body=%s", rr.Code, rr.Body.String())
	}
	body.History = nil
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode filtered history: %v", err)
	}
	if len(body.History) != 1 || body.History[0].Source != "monitor" || body.History[0].DetailFields["limit"] != "7" {
		t.Fatalf("filtered history = %+v", body.History)
	}
}

func TestSkillEvolutionMetricsEndpointAggregatesHistory(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	auditRepo := audit.NewRepo(provider.Write, provider.Read)
	h := NewHandler(provider.Write, provider.Read)
	h.SetAuditRepo(auditRepo)
	h.recordSkillEvolutionRunAudit("tenant-a", SkillEvolutionRunSummary{Scanned: 5, Attempted: 2, Published: 1, Skipped: 3, Failed: 1})
	monitor := NewSkillEvolutionMonitor(h, fakeSkillEvolutionTenantLister{}, SkillEvolutionMonitorConfig{Interval: time.Hour, Limit: 7})
	monitor.recordAudit("tenant-a", SkillEvolutionTenantStatus{TenantID: "tenant-a", AutomationEnabled: true, IntervalSeconds: 60, Limit: 7, SkipReason: "interval_not_reached"})

	req := httptest.NewRequest(http.MethodGet, "/admin/skillmarket/evolution-metrics?limit=10", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var metrics SkillEvolutionMetrics
	if err := json.NewDecoder(rr.Body).Decode(&metrics); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	if metrics.Count != 2 || metrics.BySource["manual_run"] != 1 || metrics.BySource["monitor"] != 1 || metrics.ByStatus["ok"] != 1 || metrics.ByStatus["error"] != 1 {
		t.Fatalf("metrics = %+v", metrics)
	}
	if metrics.Scanned != 5 || metrics.Attempted != 2 || metrics.Published != 1 || metrics.Skipped != 3 || metrics.Failed != 1 || metrics.SkipReasons["interval_not_reached"] != 1 {
		t.Fatalf("metric totals = %+v", metrics)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/skillmarket/evolution-metrics?source=monitor&status=ok", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr = httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("filtered status=%d body=%s", rr.Code, rr.Body.String())
	}
	metrics = SkillEvolutionMetrics{}
	if err := json.NewDecoder(rr.Body).Decode(&metrics); err != nil {
		t.Fatalf("decode filtered metrics: %v", err)
	}
	if metrics.Count != 1 || metrics.BySource["monitor"] != 1 || metrics.SkipReasons["interval_not_reached"] != 1 || metrics.Published != 0 {
		t.Fatalf("filtered metrics = %+v", metrics)
	}
}

func TestSkillEvolutionHealthEndpointSummarizesOperatingState(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	auditRepo := audit.NewRepo(provider.Write, provider.Read)
	h := NewHandler(provider.Write, provider.Read)
	h.SetAuditRepo(auditRepo)

	req := httptest.NewRequest(http.MethodGet, "/admin/skillmarket/evolution-health", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("empty status=%d body=%s", rr.Code, rr.Body.String())
	}
	var health SkillEvolutionHealth
	if err := json.NewDecoder(rr.Body).Decode(&health); err != nil {
		t.Fatalf("decode empty health: %v", err)
	}
	if health.Level != "warning" || !containsString(health.Reasons, "no_recent_skill_evolution_history") {
		t.Fatalf("empty health = %+v", health)
	}

	h.recordSkillEvolutionRunAudit("tenant-a", SkillEvolutionRunSummary{Scanned: 5, Attempted: 2, Published: 1, Failed: 1})
	req = httptest.NewRequest(http.MethodGet, "/admin/skillmarket/evolution-health", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr = httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("failed status=%d body=%s", rr.Code, rr.Body.String())
	}
	health = SkillEvolutionHealth{}
	if err := json.NewDecoder(rr.Body).Decode(&health); err != nil {
		t.Fatalf("decode failed health: %v", err)
	}
	if health.Level != "critical" || health.Metrics.Failed != 1 || health.LastRun == nil || !containsString(health.Reasons, "skill_evolution_failures_detected") {
		t.Fatalf("failed health = %+v", health)
	}
}

func TestSkillEvolutionHealthWarnsWhenAllRunsSkippedByInterval(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	auditRepo := audit.NewRepo(provider.Write, provider.Read)
	h := NewHandler(provider.Write, provider.Read)
	h.SetAuditRepo(auditRepo)
	monitor := NewSkillEvolutionMonitor(h, fakeSkillEvolutionTenantLister{}, SkillEvolutionMonitorConfig{Interval: time.Hour, Limit: 7})
	monitor.recordAudit("tenant-a", SkillEvolutionTenantStatus{TenantID: "tenant-a", AutomationEnabled: true, IntervalSeconds: 60, Limit: 7, SkipReason: "interval_not_reached"})

	req := httptest.NewRequest(http.MethodGet, "/admin/skillmarket/evolution-health", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var health SkillEvolutionHealth
	if err := json.NewDecoder(rr.Body).Decode(&health); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if health.Level != "warning" || health.Metrics.SkipReasons["interval_not_reached"] != 1 || !containsString(health.Reasons, "all_recent_runs_skipped_by_interval") {
		t.Fatalf("health = %+v", health)
	}
}

func TestSkillEvolutionHealthWarnsWhenAutomationIsStale(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	auditRepo := audit.NewRepo(provider.Write, provider.Read)
	h := NewHandler(provider.Write, provider.Read)
	h.SetAuditRepo(auditRepo)
	_, _ = h.SetSkillEvolutionAutomationRule(context.Background(), "tenant-a", SkillEvolutionAutomationRule{Enabled: true, IntervalSeconds: 60, Limit: 5})
	if err := auditRepo.Insert("tenant-a", &audit.ProxyLog{
		RequestID:  "old-monitor-run",
		ProviderID: "iworkercenter",
		Model:      "skill-evolution-monitor",
		WorkType:   "skill_evolution_monitor",
		CostTier:   "internal",
		Status:     "ok",
		Summary:    "skill evolution monitor completed",
		ErrorMsg:   "tenant=tenant-a scanned=1 attempted=0 published=0 skipped=1 failed=0",
		CreatedAt:  time.Now().UTC().Add(-2 * time.Hour),
	}); err != nil {
		t.Fatalf("insert old monitor audit: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/skillmarket/evolution-health", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "tenant-a"))
	rr := httptest.NewRecorder()
	h.handleAdminSkillMarketByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var health SkillEvolutionHealth
	if err := json.NewDecoder(rr.Body).Decode(&health); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if health.Level != "critical" || !containsString(health.Reasons, "skill_evolution_stale") || health.LastRunAgeSeconds <= health.StaleThresholdSeconds || health.ExpectedIntervalSeconds != 60 {
		t.Fatalf("health = %+v", health)
	}
}

func TestSkillEvolutionRunLeasePreventsDuplicateRealRuns(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h := NewHandler(provider.Write, provider.Read)
	owner, acquired, err := h.acquireSkillEvolutionRunLease(context.Background(), "tenant-a", time.Minute)
	if err != nil {
		t.Fatalf("acquire first lease: %v", err)
	}
	if !acquired || owner == "" {
		t.Fatalf("first lease acquired=%v owner=%q", acquired, owner)
	}
	_, acquired, err = h.acquireSkillEvolutionRunLease(context.Background(), "tenant-a", time.Minute)
	if err != nil {
		t.Fatalf("acquire second lease: %v", err)
	}
	if acquired {
		t.Fatalf("expected second lease acquisition to be rejected")
	}
	h.releaseSkillEvolutionRunLease(context.Background(), "tenant-a", owner)
	_, acquired, err = h.acquireSkillEvolutionRunLease(context.Background(), "tenant-a", time.Minute)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	if !acquired {
		t.Fatalf("expected lease acquisition after release")
	}
}
