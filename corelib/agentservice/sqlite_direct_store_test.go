package agentservice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

func assertSQLiteReadEqual[T any](t *testing.T, name string, want func() (T, error), got func() (T, error)) {
	t.Helper()
	oldValue, oldErr := want()
	newValue, newErr := got()
	if fmt.Sprint(oldErr) != fmt.Sprint(newErr) {
		t.Fatalf("%s error: old=%v new=%v", name, oldErr, newErr)
	}
	oldJSON, err := json.Marshal(oldValue)
	if err != nil {
		t.Fatal(err)
	}
	newJSON, err := json.Marshal(newValue)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(oldJSON, newJSON) {
		t.Fatalf("%s differs:\nold=%s\nnew=%s", name, oldJSON, newJSON)
	}
}

func assertSQLiteMigrationReadsEqual(t *testing.T, old *MemoryStore, current *SQLiteStore) {
	t.Helper()
	assertSQLiteReadEqual(t, "GetTenant", func() (Tenant, error) { return old.GetTenant("t1") }, func() (Tenant, error) { return current.GetTenant("t1") })
	assertSQLiteReadEqual(t, "ListTenants", old.ListTenants, current.ListTenants)
	assertSQLiteReadEqual(t, "GetUser", func() (User, error) { return old.GetUser("t2", "u2") }, func() (User, error) { return current.GetUser("t2", "u2") })
	assertSQLiteReadEqual(t, "ListUsers t1", func() ([]User, error) { return old.ListUsers("t1") }, func() ([]User, error) { return current.ListUsers("t1") })
	assertSQLiteReadEqual(t, "ListUsers t2", func() ([]User, error) { return old.ListUsers("t2") }, func() ([]User, error) { return current.ListUsers("t2") })
	assertSQLiteReadEqual(t, "GetCredential", func() (Credential, error) { return old.GetCredential("t1", "u1", "c1") }, func() (Credential, error) { return current.GetCredential("t1", "u1", "c1") })
	assertSQLiteReadEqual(t, "ListCredentials", func() ([]Credential, error) { return old.ListCredentials("t1", "u1") }, func() ([]Credential, error) { return current.ListCredentials("t1", "u1") })
	assertSQLiteReadEqual(t, "GetCredentialByAPIKey", func() (Credential, error) { return old.GetCredentialByAPIKey("legacy-key") }, func() (Credential, error) { return current.GetCredentialByAPIKey("legacy-key") })
	assertSQLiteReadEqual(t, "GetUserConfig", func() (UserConfig, error) { return old.GetUserConfig("t2", "u2") }, func() (UserConfig, error) { return current.GetUserConfig("t2", "u2") })
	assertSQLiteReadEqual(t, "GetInstance", func() (Instance, error) { return old.GetInstance("t2", "u2", "i2") }, func() (Instance, error) { return current.GetInstance("t2", "u2", "i2") })
	assertSQLiteReadEqual(t, "ListInstances", func() ([]Instance, error) { return old.ListInstances("t2", "u2") }, func() ([]Instance, error) { return current.ListInstances("t2", "u2") })
	assertSQLiteReadEqual(t, "GetSession", func() (Session, error) { return old.GetSession("t1", "u1", "i1", "s1") }, func() (Session, error) { return current.GetSession("t1", "u1", "i1", "s1") })
	assertSQLiteReadEqual(t, "ListSessions", func() ([]Session, error) { return old.ListSessions("t2", "u2", "i2") }, func() ([]Session, error) { return current.ListSessions("t2", "u2", "i2") })
	assertSQLiteReadEqual(t, "GetMessage", func() (Message, error) { return old.GetMessage("s1", "m2") }, func() (Message, error) { return current.GetMessage("s1", "m2") })
	assertSQLiteReadEqual(t, "GetMessage missing", func() (Message, error) { return old.GetMessage("s1", "missing") }, func() (Message, error) { return current.GetMessage("s1", "missing") })
	assertSQLiteReadEqual(t, "ListMessages s1", func() ([]Message, error) { return old.ListMessages("s1") }, func() ([]Message, error) { return current.ListMessages("s1") })
	assertSQLiteReadEqual(t, "ListMessages s2", func() ([]Message, error) { return old.ListMessages("s2") }, func() ([]Message, error) { return current.ListMessages("s2") })
	assertSQLiteReadEqual(t, "ListMessages missing", func() ([]Message, error) { return old.ListMessages("missing") }, func() ([]Message, error) { return current.ListMessages("missing") })
	assertSQLiteReadEqual(t, "GetRun", func() (Run, error) { return old.GetRun("t2", "u2", "i2", "r2") }, func() (Run, error) { return current.GetRun("t2", "u2", "i2", "r2") })
	assertSQLiteReadEqual(t, "ListRuns", func() ([]Run, error) { return old.ListRuns("t1", "u1", "i1") }, func() ([]Run, error) { return current.ListRuns("t1", "u1", "i1") })
	assertSQLiteReadEqual(t, "GetRunByUserMessageID", func() (Run, error) { return old.GetRunByUserMessageID("t1", "u1", "i1", "m1") }, func() (Run, error) { return current.GetRunByUserMessageID("t1", "u1", "i1", "m1") })
	assertSQLiteReadEqual(t, "ListAuditEvents scoped", func() ([]AuditEvent, error) { return old.ListAuditEvents("t2", "u2") }, func() ([]AuditEvent, error) { return current.ListAuditEvents("t2", "u2") })
	assertSQLiteReadEqual(t, "ListAuditEvents all", func() ([]AuditEvent, error) { return old.ListAuditEvents("", "") }, func() ([]AuditEvent, error) { return current.ListAuditEvents("", "") })
	assertSQLiteReadEqual(t, "ListAfter page", func() ([]RunEvent, error) { return old.ListAfter("t1", "u1", "r1", 0, 1) }, func() ([]RunEvent, error) { return current.ListAfter("t1", "u1", "r1", 0, 1) })
	assertSQLiteReadEqual(t, "ListAfter rest", func() ([]RunEvent, error) { return old.ListAfter("t1", "u1", "r1", 1, 100) }, func() ([]RunEvent, error) { return current.ListAfter("t1", "u1", "r1", 1, 100) })
	oldSeq, oldFound, oldErr := old.SequenceForID("t1", "u1", "r1", "e2")
	newSeq, newFound, newErr := current.SequenceForID("t1", "u1", "r1", "e2")
	if oldSeq != newSeq || oldFound != newFound || fmt.Sprint(oldErr) != fmt.Sprint(newErr) {
		t.Fatalf("SequenceForID differs: old=(%d,%v,%v) new=(%d,%v,%v)", oldSeq, oldFound, oldErr, newSeq, newFound, newErr)
	}
}

func TestSQLiteStoreCascadeDeletesMatchMemoryStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.db")
	payload := writeLegacyBlobDB(t, path, sampleLegacyState())
	old, err := memoryStoreFromJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	current, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	steps := []struct {
		name string
		old  func() error
		rows func() error
	}{
		{"session", func() error { return old.DeleteSession("t1", "u1", "i1", "s1") }, func() error { return current.DeleteSession("t1", "u1", "i1", "s1") }},
		{"instance", func() error { return old.DeleteInstance("t2", "u2", "i2") }, func() error { return current.DeleteInstance("t2", "u2", "i2") }},
		{"user", func() error { return old.DeleteUser("t1", "u1") }, func() error { return current.DeleteUser("t1", "u1") }},
		{"tenant", func() error { return old.DeleteTenant("t2") }, func() error { return current.DeleteTenant("t2") }},
	}
	for _, step := range steps {
		oldErr, newErr := step.old(), step.rows()
		if fmt.Sprint(oldErr) != fmt.Sprint(newErr) {
			t.Fatalf("%s delete: old=%v new=%v", step.name, oldErr, newErr)
		}
		wantJSON, _ := canonicalStateJSON(old)
		gotJSON, _ := canonicalStateJSON(sqliteTestMemory(t, current))
		if !bytes.Equal(wantJSON, gotJSON) {
			t.Fatalf("%s cascade differs:\nold=%s\nnew=%s", step.name, wantJSON, gotJSON)
		}
	}
}

func TestSQLiteStoreCrossHandleWritesVisibleForMessagesConfigsAndAudits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.db")
	first, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := first.SaveUserConfig(UserConfig{TenantID: "t", UserID: "u"}); err != nil {
		t.Fatal(err)
	}
	if _, err := second.GetUserConfig("t", "u"); err != nil {
		t.Fatalf("second handle missed config: %v", err)
	}
	message := Message{ID: "m", SessionID: "s", TenantID: "t", UserID: "u", InstanceID: "i", Role: MessageRoleUser, Content: "visible"}
	if err := second.SaveMessage(message); err != nil {
		t.Fatal(err)
	}
	assertSQLiteReadEqual(t, "cross-handle message", func() (Message, error) { return first.GetMessage("s", "m") }, func() (Message, error) { return second.GetMessage("s", "m") })
	if err := first.SaveAuditEvent(AuditEvent{ID: "a", TenantID: "t", UserID: "u"}); err != nil {
		t.Fatal(err)
	}
	events, err := second.ListAuditEvents("t", "u")
	if err != nil || len(events) != 1 || events[0].ID != "a" {
		t.Fatalf("second handle audits=%#v err=%v", events, err)
	}
}
