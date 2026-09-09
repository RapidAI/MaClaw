package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func newPendingTestManager(t *testing.T) (*Manager, *PendingStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "database_pending.json")
	store, err := NewPendingStore(path)
	if err != nil {
		t.Fatalf("NewPendingStore: %v", err)
	}
	m := NewManager([]Profile{{ID: "p", Type: SourcePostgres, Host: "db", Database: "crm", SchemaVersion: 3, WriteEnabled: true, AllowedOperations: []string{"inspect", "query", "execute", "batch_execute"}}}, nil)
	m.SetPendingStore(store)
	return m, store, path
}

func TestIssueApprovalHonorsPolicyGate(t *testing.T) {
	m, _, _ := newPendingTestManager(t)
	m.SetIssueGate(func(req ApprovalRequest) error {
		return fmt.Errorf("permission: denied by policy engine")
	})
	_, err := m.IssueApproval(ApprovalRequest{ProfileID: "p", SQLFingerprint: sqlFingerprint("s")})
	if err == nil || !strings.Contains(err.Error(), "policy engine") {
		t.Fatalf("got %v", err)
	}
}

func TestIssueApprovalConsumesOnce(t *testing.T) {
	m, store, _ := newPendingTestManager(t)
	defer m.Close()
	fingerprint := sqlFingerprint("update orders set x=:x where id=:id")
	approval, err := m.IssueApproval(ApprovalRequest{ProfileID: "p", SQLFingerprint: fingerprint})
	if err != nil {
		t.Fatalf("IssueApproval: %v", err)
	}
	if approval.Token == "" || approval.ID == "" {
		t.Fatalf("approval missing token/id: %+v", approval)
	}
	if approval.SchemaVersion != 3 {
		t.Fatalf("schema version must come from the live profile, got %d", approval.SchemaVersion)
	}
	if err := m.consumeApproval(context.Background(), approval, fingerprint); err != nil {
		t.Fatalf("consume: %v", err)
	}
	if err := m.consumeApproval(context.Background(), approval, fingerprint); err == nil {
		t.Fatal("approval replayed")
	}
	if store.Len() != 0 {
		t.Fatal("consumed approval still pending")
	}
}

func TestIssueApprovalRejectsTampering(t *testing.T) {
	m, _, _ := newPendingTestManager(t)
	defer m.Close()
	fingerprint := sqlFingerprint("update orders set x=:x where id=:id")
	approval, err := m.IssueApproval(ApprovalRequest{ProfileID: "p", SQLFingerprint: fingerprint})
	if err != nil {
		t.Fatalf("IssueApproval: %v", err)
	}
	tampered := approval
	tampered.Token = approval.Token + "ff"
	if err := m.consumeApproval(context.Background(), tampered, fingerprint); err == nil || !strings.Contains(err.Error(), "token mismatch") {
		t.Fatalf("tampered token accepted: %v", err)
	}
	wrongSQL := approval
	wrongSQL.SQLFingerprint = sqlFingerprint("delete from orders")
	if err := m.consumeApproval(context.Background(), wrongSQL, fingerprint); err == nil {
		t.Fatal("fingerprint mismatch accepted")
	}
	// The failed attempts must not have consumed the legitimate approval.
	if err := m.consumeApproval(context.Background(), approval, fingerprint); err != nil {
		t.Fatalf("legitimate approval destroyed by tampering attempts: %v", err)
	}
}

func TestPendingApprovalExpires(t *testing.T) {
	m, store, _ := newPendingTestManager(t)
	defer m.Close()
	approval, err := m.IssueApproval(ApprovalRequest{ProfileID: "p", SQLFingerprint: sqlFingerprint("s"), TTL: time.Millisecond})
	if err != nil {
		t.Fatalf("IssueApproval: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := m.consumeApproval(context.Background(), approval, approval.SQLFingerprint); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired approval accepted: %v", err)
	}
	if store.Len() != 0 {
		t.Fatal("expired approval still pending")
	}
}

func TestUpdateProfilesDestroysPendingMutations(t *testing.T) {
	m, store, _ := newPendingTestManager(t)
	defer m.Close()
	approval, err := m.IssueApproval(ApprovalRequest{ProfileID: "p", SQLFingerprint: sqlFingerprint("s")})
	if err != nil {
		t.Fatalf("IssueApproval: %v", err)
	}
	m.UpdateProfiles([]Profile{{ID: "p", Type: SourcePostgres, Host: "db", Database: "crm", SchemaVersion: 4}})
	if store.Len() != 0 {
		t.Fatal("pending mutation survived profile change")
	}
	if err := m.consumeApproval(context.Background(), approval, approval.SQLFingerprint); err == nil {
		t.Fatal("approval issued against old profile configuration committed")
	}
}

func TestPendingStoreSurvivesRestart(t *testing.T) {
	m, _, path := newPendingTestManager(t)
	defer m.Close()
	fingerprint := sqlFingerprint("update orders set x=1 where id=1")
	approval, err := m.IssueApproval(ApprovalRequest{ProfileID: "p", SQLFingerprint: fingerprint})
	if err != nil {
		t.Fatalf("IssueApproval: %v", err)
	}

	// Simulate a process restart: drop the in-process singleton so the store
	// genuinely reloads from disk, then a new store over the same file must
	// still honor the approval exactly once (it is bound to the same profile
	// and schema version, which a fresh manager would reload from config).
	resetPendingStoresForTest()
	reopened, err := NewPendingStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.Len() != 1 {
		t.Fatalf("pending mutation lost across restart, len=%d", reopened.Len())
	}
	m2 := NewManager([]Profile{{ID: "p", Type: SourcePostgres, Host: "db", Database: "crm", SchemaVersion: 3}}, nil)
	defer m2.Close()
	m2.SetPendingStore(reopened)
	if err := m2.consumeApproval(context.Background(), approval, fingerprint); err != nil {
		t.Fatalf("consume after restart: %v", err)
	}
	if err := m2.consumeApproval(context.Background(), approval, fingerprint); err == nil {
		t.Fatal("approval replayed after restart")
	}
}

func TestPendingStoreQuarantinesCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database_pending.json")
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := NewPendingStore(path)
	if err != nil {
		t.Fatalf("corrupt file must fail closed into an empty store: %v", err)
	}
	if store.Len() != 0 {
		t.Fatal("corrupt entries loaded")
	}
	matches, _ := filepath.Glob(path + ".corrupt-*")
	if len(matches) != 1 {
		t.Fatalf("corrupt file was not quarantined: %v", matches)
	}
}

func TestPendingStoreNeverPersistsToken(t *testing.T) {
	m, _, path := newPendingTestManager(t)
	defer m.Close()
	approval, err := m.IssueApproval(ApprovalRequest{ProfileID: "p", SQLFingerprint: sqlFingerprint("s"), OwnerID: "owner", SessionID: "session"})
	if err != nil {
		t.Fatalf("IssueApproval: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store: %v", err)
	}
	if strings.Contains(string(data), approval.Token) {
		t.Fatal("approval token persisted in plaintext")
	}
	if !strings.Contains(string(data), approval.ID) {
		t.Fatal("pending mutation missing from store")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("store permissions = %v, want 0600", info.Mode().Perm())
	}
}

// TestStrictModeRejectsForeignTokens locks the behavioral difference between
// legacy host-injected tokens and unified issuance: once a PendingStore is
// configured, tokens the manager never minted must fail closed.
func TestStrictModeRejectsForeignTokens(t *testing.T) {
	m, _, _ := newPendingTestManager(t)
	defer m.Close()
	foreign := WithApprovalContext(context.Background(), ApprovalContext{Token: "host-made", ID: "db-appr-foreign"})
	a := &toolTestAdapter{}
	m.items["conn"] = a
	m.lastUsed["conn"] = time.Now()
	m.profileIDs["conn"] = "p"
	result := HandleTool(foreign, m, map[string]interface{}{
		"action": "execute", "connection_id": "conn", "sql": "update t set x=1 where id=1", "dry_run": false,
	})
	if !strings.Contains(result, "approval") {
		t.Fatalf("foreign token committed in strict mode: %q", result)
	}
	if a.executeRequest.ApprovalToken != "" {
		t.Fatal("rejected approval reached the adapter")
	}
}

func TestMutationFingerprintsMatchHandlerValidation(t *testing.T) {
	sqlFP, paramsFP, err := MutationFingerprints(map[string]interface{}{
		"action": "execute", "sql": "update t set x=:x where id=:id",
		"params": map[string]interface{}{"x": 1, "id": 2},
	})
	if err != nil || sqlFP == "" || paramsFP == "" {
		t.Fatalf("execute fingerprints: %q %q %v", sqlFP, paramsFP, err)
	}

	batchArgs := map[string]interface{}{
		"action": "batch_execute",
		"statements": []interface{}{
			map[string]interface{}{"sql": "update t set x=:x where id=:id", "params": map[string]interface{}{"x": 1, "id": 1}},
			map[string]interface{}{"sql": "delete from t where id=:id", "params": map[string]interface{}{"id": 2}},
		},
	}
	batchFP, batchParamsFP, err := MutationFingerprints(batchArgs)
	if err != nil || batchFP == "" || batchParamsFP == "" {
		t.Fatalf("batch fingerprints: %v", err)
	}

	fileFP, fileParamsFP, err := MutationFingerprints(map[string]interface{}{
		"action": "write_table", "file_path": "reports/out.xlsx",
		"sheet": "Sheet1", "range": "A1", "rows": []interface{}{[]interface{}{"a"}},
		"source_sha256": "abc",
	})
	if err != nil || fileFP == "" || fileParamsFP == "" {
		t.Fatalf("file fingerprints: %q %q %v", fileFP, fileParamsFP, err)
	}
	// The file params fingerprint must track the content: changing rows or the
	// source hash changes it, so an approval cannot be re-aimed after preview.
	_, tamperedFP, err := MutationFingerprints(map[string]interface{}{
		"action": "write_table", "file_path": "reports/out.xlsx",
		"sheet": "Sheet1", "range": "A1", "rows": []interface{}{[]interface{}{"b"}},
		"source_sha256": "abc",
	})
	if err != nil || tamperedFP == fileParamsFP {
		t.Fatalf("rows change did not change the file params fingerprint: %q %q %v", fileParamsFP, tamperedFP, err)
	}

	if _, _, err := MutationFingerprints(map[string]interface{}{"action": "query", "sql": "select 1"}); err == nil {
		t.Fatal("read action must not produce a mutation fingerprint")
	}
	if _, _, err := MutationFingerprints(map[string]interface{}{"action": "execute"}); err == nil {
		t.Fatal("missing sql must fail")
	}
}

// TestStrictModeHandlerEndToEnd drives the full handler path the GUI uses:
// fingerprint the args, issue an approval, inject it into the request context
// and commit through HandleTool.
func TestStrictModeHandlerEndToEnd(t *testing.T) {
	m, _, _ := newPendingTestManager(t)
	defer m.Close()
	a := &toolTestAdapter{}
	m.items["conn"] = a
	m.lastUsed["conn"] = time.Now()
	m.profileIDs["conn"] = "p"
	args := map[string]interface{}{
		"action": "execute", "connection_id": "conn",
		"sql": "update t set x=:x where id=:id", "params": map[string]interface{}{"x": 1, "id": 2},
		"dry_run": false,
	}
	sqlFP, paramsFP, err := MutationFingerprints(args)
	if err != nil {
		t.Fatalf("MutationFingerprints: %v", err)
	}
	approval, err := m.IssueApproval(ApprovalRequest{
		ProfileID:         "p",
		SQLFingerprint:    sqlFP,
		ParamsFingerprint: paramsFP,
		OwnerID:           "owner", SessionID: "session",
	})
	if err != nil {
		t.Fatalf("IssueApproval: %v", err)
	}
	scopeCtx := WithRequestScope(context.Background(), RequestScope{OwnerID: "owner", SessionID: "session"})
	ctx := WithApprovalContext(scopeCtx, approval)
	result := HandleTool(ctx, m, args)
	if !strings.Contains(result, `"dry_run":false`) {
		t.Fatalf("strict commit failed: %q", result)
	}
	if a.executeRequest.ApprovalToken != approval.Token {
		t.Fatal("issued token was not delivered to the adapter")
	}
	// The same approval context cannot commit a second time.
	replay := HandleTool(WithApprovalContext(scopeCtx, approval), m, map[string]interface{}{
		"action": "execute", "connection_id": "conn",
		"sql": "update t set x=:x where id=:id", "params": map[string]interface{}{"x": 1, "id": 2},
		"dry_run": false,
	})
	if !strings.Contains(replay, "approval") {
		t.Fatalf("replayed approval committed: %q", replay)
	}
}
