package agentservice

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSQLiteDynamicOperationLedgerPersistsAndReconcilesStaleRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.db")
	ledger, err := NewSQLiteDynamicOperationLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	record := DynamicOperationRecord{Key: "op-1", TenantID: "tenant", UserID: "user", SessionID: "task", AdapterKind: "mcp", BindingID: "binding", RequestDigest: "request", CreatedAt: time.Now().UTC().Add(-2 * DynamicOperationRunningLease)}
	if _, execute, err := ledger.Acquire(record); err != nil || !execute {
		t.Fatalf("initial acquire execute=%v err=%v", execute, err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewSQLiteDynamicOperationLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if existing, execute, err := reopened.Acquire(record); err != nil || execute || existing.State != DynamicOperationRunning {
		t.Fatalf("persisted acquire record=%#v execute=%v err=%v", existing, execute, err)
	}
	if changed, err := reopened.ReconcileStaleRunning(time.Now().UTC(), DynamicOperationRunningLease); err != nil || changed != 1 {
		t.Fatalf("reconcile changed=%d err=%v", changed, err)
	}
	got, err := reopened.Get(record.Key)
	if err != nil || got.State != DynamicOperationUnknown || got.ReasonCode != "operation_lease_expired" {
		t.Fatalf("reconciled record=%#v err=%v", got, err)
	}
}

func TestDynamicOperationLedgerAcquireIsAtomic(t *testing.T) {
	ledger := NewMemoryDynamicOperationLedger()
	record := DynamicOperationRecord{Key: "operation", TenantID: "tenant", UserID: "user", SessionID: "task", AdapterKind: "skill", BindingID: "binding", RequestDigest: "request"}
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := 0
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, execute, err := ledger.Acquire(record)
			if err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			if execute {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if winners != 1 {
		t.Fatalf("acquire winners=%d, want 1", winners)
	}
}

func TestSQLiteDynamicOperationLedgerMigratesReceiptDigestAndRequiresItForSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-operations.db")
	ledger, err := NewSQLiteDynamicOperationLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.db.Exec(`DROP TABLE dynamic_operation_ledger`); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.db.Exec(`CREATE TABLE dynamic_operation_ledger (
		operation_key TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, user_id TEXT NOT NULL,
		session_id TEXT NOT NULL, adapter_kind TEXT NOT NULL, binding_id TEXT NOT NULL,
		request_digest TEXT NOT NULL, state TEXT NOT NULL, result_digest TEXT NOT NULL DEFAULT '',
		reason_code TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	migrated, err := NewSQLiteDynamicOperationLedger(path)
	if err != nil {
		t.Fatalf("migrate legacy ledger: %v", err)
	}
	defer migrated.Close()
	record := DynamicOperationRecord{Key: "operation", TenantID: "tenant", UserID: "user", SessionID: "task", AdapterKind: "semantic:mcp:need", BindingID: "binding", RequestDigest: "request"}
	if _, execute, err := migrated.Acquire(record); err != nil || !execute {
		t.Fatalf("acquire execute=%v err=%v", execute, err)
	}
	if _, err := migrated.Complete(record.Key, DynamicOperationAwaitingReceipt, "result", "prepared", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := migrated.Settle(record.Key, DynamicOperationSucceeded, "", "", "accepted", time.Now().UTC()); err == nil {
		t.Fatal("success settlement without receipt digest succeeded")
	}
	settled, err := migrated.Settle(record.Key, DynamicOperationSucceeded, "", "receipt-digest", "accepted", time.Now().UTC())
	if err != nil || settled.State != DynamicOperationSucceeded || settled.ReceiptDigest != "receipt-digest" {
		t.Fatalf("settled record=%#v err=%v", settled, err)
	}
}

func TestSQLiteDynamicOperationLedgerMigratesSemanticReconciliationBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-operations.db")
	ledger, err := NewSQLiteDynamicOperationLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.db.Exec(`DROP TABLE dynamic_operation_ledger`); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.db.Exec(`CREATE TABLE dynamic_operation_ledger (
		operation_key TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, user_id TEXT NOT NULL,
		session_id TEXT NOT NULL, adapter_kind TEXT NOT NULL, binding_id TEXT NOT NULL,
		request_digest TEXT NOT NULL, state TEXT NOT NULL, result_digest TEXT NOT NULL DEFAULT '',
		reason_code TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	migrated, err := NewSQLiteDynamicOperationLedger(path)
	if err != nil {
		t.Fatalf("migrate legacy ledger: %v", err)
	}
	defer migrated.Close()
	record := DynamicOperationRecord{Key: "operation", TenantID: "tenant", UserID: "user", SessionID: "root", AdapterKind: "semantic:mcp:need", BindingID: "binding", RequestDigest: "request", InvocationPlanID: "plan", InvocationSessionID: "session", InvocationTurnID: "turn", InvocationPrincipalID: "tenant:user", SelectionID: "selection", SelectionDigest: "selection-digest"}
	if _, execute, err := migrated.Acquire(record); err != nil || !execute {
		t.Fatalf("acquire execute=%v err=%v", execute, err)
	}
	got, err := migrated.Get(record.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.InvocationPlanID != record.InvocationPlanID || got.InvocationSessionID != record.InvocationSessionID || got.InvocationTurnID != record.InvocationTurnID || got.InvocationPrincipalID != record.InvocationPrincipalID || got.SelectionID != record.SelectionID || got.SelectionDigest != record.SelectionDigest {
		t.Fatalf("migrated reconciliation binding=%+v", got)
	}
}
