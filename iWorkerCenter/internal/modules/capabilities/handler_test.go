package capabilities

import (
	"context"
	"net/http"
	"net/http/httptest"
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
