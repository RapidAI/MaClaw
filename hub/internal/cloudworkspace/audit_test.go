package cloudworkspace

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func TestCloudWorkspaceAuditRecordIsIdempotentAndTenantScoped(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "audit", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{Workspaces: st, Now: func() time.Time { return now }}
	principal := auth.MachinePrincipal{TenantID: "t1", UserID: "u1", MachineID: "m1", ClientInstanceID: "cwi_1"}
	event := AuditEvent{EventID: "event-1", Operation: "push", Outcome: "ok", Revision: "rev_1", Files: 2, Bytes: 42}
	first, err := svc.RecordAudit(ctx, principal, ws.ID, event)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.RecordAudit(ctx, principal, ws.ID, event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordAudit(ctx, principal, ws.ID, AuditEvent{EventID: "event-1", Operation: "push", Outcome: "failed"}); err != ErrAuditEventIDReused {
		t.Fatalf("reused event id err=%v, want %v", err, ErrAuditEventIDReused)
	}
	if first.ID == 0 || second.ID != first.ID || second.EventID != "event-1" {
		t.Fatalf("idempotency mismatch first=%+v second=%+v", first, second)
	}
	items, err := svc.ListAudit(ctx, principal, ws.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Operation != "push" || items[0].Bytes != 42 {
		t.Fatalf("items=%+v", items)
	}
	if _, err := svc.RecordAudit(ctx, auth.MachinePrincipal{TenantID: "t1", UserID: "other"}, ws.ID, event); err != ErrNotFound {
		t.Fatalf("foreign owner err=%v", err)
	}
}

func TestCloudWorkspaceAuditRejectsFreeFormSecrets(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "audit", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{Workspaces: st}
	principal := auth.MachinePrincipal{TenantID: "t1", UserID: "u1", MachineID: "m1"}
	if _, err := svc.RecordAudit(ctx, principal, ws.ID, AuditEvent{Operation: "push", Outcome: "failed", Detail: "token=secret"}); err != ErrInvalidAuditEvent {
		t.Fatalf("secret detail err=%v", err)
	}
	if _, err := svc.RecordAudit(ctx, principal, ws.ID, AuditEvent{Operation: "../../read", Outcome: "ok"}); err != ErrInvalidAuditEvent {
		t.Fatalf("operation path err=%v", err)
	}
}

func TestCloudWorkspaceAuditRejectedAfterHardPurge(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "purged-audit", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SoftDelete(ctx, "t1", "u1", "m1", ws.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := st.HardDeleteDeleted(ctx, "t1", "u1", ws.ID); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Workspaces: st, Now: func() time.Time { return now.Add(2 * time.Second) }}
	owner := auth.MachinePrincipal{TenantID: "t1", UserID: "u1", MachineID: "m1", ClientInstanceID: "cwi_1"}
	// After a hard purge the workspace row is gone; late audit uploads are
	// rejected and the device-local JSONL log remains the evidence of record.
	if _, err := svc.RecordAudit(ctx, owner, ws.ID, AuditEvent{
		EventID: "purge-after-delete", Operation: "remote_purge", Outcome: "already_gone",
	}); err != ErrNotFound {
		t.Fatalf("post-purge audit err=%v, want ErrNotFound", err)
	}
	if _, err := svc.ListAudit(ctx, owner, ws.ID, 0, 10); err != ErrNotFound {
		t.Fatalf("post-purge list err=%v, want ErrNotFound", err)
	}
	if _, err := svc.RecordAudit(ctx, auth.MachinePrincipal{TenantID: "t1", UserID: "other"}, ws.ID, AuditEvent{
		EventID: "foreign-after-delete", Operation: "remote_purge",
	}); err != ErrNotFound {
		t.Fatalf("foreign post-purge audit err=%v", err)
	}
}

func TestCloudWorkspaceAuditAdminExportIsTenantScopedAndPaginated(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws1, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "export-a", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	ws2, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u2", Name: "export-b", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	wsOther, err := st.Create(ctx, CreateParams{TenantID: "t2", UserID: "u9", Name: "other", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{Workspaces: st, Now: func() time.Time { return now }}
	for i, item := range []struct {
		principal auth.MachinePrincipal
		ws        string
		eventID   string
	}{
		{auth.MachinePrincipal{TenantID: "t1", UserID: "u1", MachineID: "m1"}, ws1.ID, "a1"},
		{auth.MachinePrincipal{TenantID: "t1", UserID: "u2", MachineID: "m2"}, ws2.ID, "b1"},
		{auth.MachinePrincipal{TenantID: "t2", UserID: "u9", MachineID: "m9"}, wsOther.ID, "x1"},
	} {
		if _, err := svc.RecordAudit(ctx, item.principal, item.ws, AuditEvent{EventID: item.eventID, Operation: "push", Files: i + 1}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := st.ListAuditForTenant(ctx, "t1", "", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || !page.HasMore || page.NextAfterID == 0 || page.Events[0].TenantID != "t1" {
		t.Fatalf("first page=%+v", page)
	}
	page2, err := st.ListAuditForTenant(ctx, "t1", "", page.NextAfterID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Events) != 1 || page2.HasMore || page2.Events[0].UserID != "u2" {
		t.Fatalf("second page=%+v", page2)
	}
	filtered, err := st.ListAuditForTenant(ctx, "t1", ws2.ID, 0, 10)
	if err != nil || len(filtered.Events) != 1 || filtered.Events[0].WorkspaceID != ws2.ID {
		t.Fatalf("filtered=%+v err=%v", filtered, err)
	}
	if _, err := st.ListAuditForTenant(ctx, "", "", 0, 10); err != ErrInvalidAuditEvent {
		t.Fatalf("empty tenant err=%v", err)
	}
}

func TestCloudWorkspaceMetricsTenantProjectionDoesNotLeakAuditRows(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws1, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "metrics-a", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	ws2, err := st.Create(ctx, CreateParams{TenantID: "t2", UserID: "u2", Name: "metrics-b", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{Workspaces: st, Now: func() time.Time { return now }}
	if _, err := svc.RecordAudit(ctx, auth.MachinePrincipal{TenantID: "t1", UserID: "u1", MachineID: "m1"}, ws1.ID, AuditEvent{EventID: "t1-event", Operation: "push"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordAudit(ctx, auth.MachinePrincipal{TenantID: "t2", UserID: "u2", MachineID: "m2"}, ws2.ID, AuditEvent{EventID: "t2-event", Operation: "push"}); err != nil {
		t.Fatal(err)
	}
	got := svc.CollectMetricsForTenant(ctx, "t1")
	if got.AuditEvents != 1 {
		t.Fatalf("tenant audit events=%d want 1", got.AuditEvents)
	}
	all := svc.CollectMetrics(ctx)
	if all.AuditEvents != 2 {
		t.Fatalf("all audit events=%d want 2", all.AuditEvents)
	}
	if empty := svc.CollectMetricsForTenant(ctx, ""); empty.AuditEvents != 0 || empty.UsedBytes != 0 || empty.OpenLeases != 0 {
		t.Fatalf("empty tenant unexpectedly fell back to default scope: %+v", empty)
	}
}
