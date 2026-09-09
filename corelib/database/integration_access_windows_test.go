//go:build windows && cgo

package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIntegrationAccessAccdb exercises the real Access ODBC path against the
// checked-in fixture testdata/integration_fixture.mdb (one People table with
// id/name/amount/born columns and four rows, generated with ADOX via the ACE
// provider in Jet 4.x format so the ODBC Admin user can be granted
// MSysObjects read access; .accdb dropped user-level security and cannot
// grant it). It runs as part of the default `go test` on Windows cgo builds
// and skips cleanly wherever the ACE driver or the fixture is unavailable
// (e.g. CI runners without the Access Database Engine installed).
func TestIntegrationAccessAccdb(t *testing.T) {
	fixture := filepath.Join("testdata", "integration_fixture.mdb")
	abs, err := filepath.Abs(fixture)
	if err != nil {
		t.Fatalf("resolve fixture path: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Skipf("Access fixture %s not available: %v", fixture, err)
	}
	if !ODBCDriverLinked() {
		t.Skip("ODBC adapter is not linked in this build")
	}
	if detection := DetectAccessODBC(); !detection.DriverInstalled || !detection.MatchingBitness {
		t.Skipf("Access ACE ODBC driver unavailable for this process bitness: %+v (hint: %s)", detection, detection.Hint())
	}

	// Read-only profile: write_enabled stays off so execute must fail closed.
	profile := Profile{
		ID:            "it-access",
		Name:          "integration access",
		SchemaVersion: 1,
		Type:          SourceAccess,
		FilePath:      abs,
		ReadOnly:      true,
		AllowedTables: []string{"People"},
	}
	m := NewManager([]Profile{profile}, nil)
	defer m.Close()
	ctx := WithRequestScope(context.Background(), RequestScope{OwnerID: "owner-it", SessionID: "session-it"})

	connID, caps, err := m.ConnectFor(ctx, profile.ID, "owner-it", "session-it")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = m.DisconnectFor(connID, "owner-it", "session-it") }()
	if !caps.Read || caps.Write {
		t.Fatalf("read-only profile must report read without write, got %+v", caps)
	}

	adapter, ok := m.AdapterFor(connID, "owner-it", "session-it")
	if !ok {
		t.Fatal("adapter not bound to owner/session")
	}

	// inspect lists the fixture table and its columns. MSysObjects reads are a
	// user-level-security grant: .mdb files can only grant it through a Jet
	// workgroup information file, and .accdb dropped ULS entirely, so on
	// machines without a workgroup (ACE redistributable, Office without
	// Access) the adapter reports unsupported_capability. That path is the
	// designed degradation; assert the full listing only when the engine
	// allows it.
	info, err := adapter.Inspect(ctx, InspectRequest{Table: "People"})
	if err != nil {
		if strings.Contains(err.Error(), "unsupported_capability") {
			t.Logf("inspect degraded as designed without an MSysObjects grant: %v", err)
		} else {
			t.Fatalf("inspect: %v", err)
		}
	} else {
		if len(info.Tables) != 1 || !strings.EqualFold(info.Tables[0].Name, "People") {
			t.Fatalf("inspect returned %+v, want the People table", info.Tables)
		}
		columns := map[string]bool{}
		for _, c := range info.Tables[0].Columns {
			columns[strings.ToLower(c.Name)] = true
		}
		for _, want := range []string{"id", "name", "amount", "born"} {
			if !columns[want] {
				t.Fatalf("inspect missing column %q in %+v", want, info.Tables[0].Columns)
			}
		}
	}

	// parameterized query over the fixture rows.
	raw := HandleTool(ctx, m, map[string]interface{}{
		"action": "query", "connection_id": connID,
		"sql":    "SELECT id, name, amount FROM People WHERE name = :name",
		"params": map[string]interface{}{"name": "beta"},
		"limit":  10,
	})
	page := mustToolResult[QueryResult](t, raw)
	if page.RowCount != 1 || fmt.Sprint(page.Rows[0][1]) != "beta" || fmt.Sprint(page.Rows[0][2]) != "20.25" {
		t.Fatalf("parameterized query returned %+v", page)
	}

	all := mustToolResult[QueryResult](t, HandleTool(ctx, m, map[string]interface{}{
		"action": "query", "connection_id": connID,
		"sql": "SELECT id, name FROM People ORDER BY id", "limit": 100,
	}))
	if all.RowCount != 4 {
		t.Fatalf("fixture must contain 4 rows, got %+v", all.Rows)
	}

	// the read-only profile rejects execute before any SQL runs.
	denied := HandleTool(ctx, m, map[string]interface{}{
		"action": "execute", "connection_id": connID,
		"sql":    "UPDATE People SET amount = :amount WHERE id = :id",
		"params": map[string]interface{}{"amount": 1, "id": 1}, "dry_run": true,
	})
	if !strings.Contains(denied, "permission") {
		t.Fatalf("read-only profile must reject execute, got: %s", denied)
	}
}

func TestIntegrationAccessWrite(t *testing.T) {
	fixture := filepath.Join("testdata", "integration_fixture.mdb")
	abs, err := filepath.Abs(fixture)
	if err != nil {
		t.Fatalf("resolve fixture path: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Skipf("Access fixture %s not available: %v", fixture, err)
	}
	if !ODBCDriverLinked() {
		t.Skip("ODBC adapter is not linked in this build")
	}
	if detection := DetectAccessODBC(); !detection.DriverInstalled || !detection.MatchingBitness {
		t.Skipf("Access ACE ODBC driver unavailable: %+v", detection)
	}
	work := filepath.Join(t.TempDir(), "write.mdb")
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(work, data, 0600); err != nil {
		t.Fatal(err)
	}
	profile := Profile{
		ID:            "it-access-write",
		SchemaVersion: 1,
		Type:          SourceAccess,
		FilePath:      work,
		ReadOnly:      false,
		WriteEnabled:  true,
		AllowedTables: []string{"People"},
	}
	m := NewManager([]Profile{profile}, nil)
	defer m.Close()
	store, err := NewPendingStore(filepath.Join(t.TempDir(), "database_pending.json"))
	if err != nil {
		t.Fatal(err)
	}
	m.SetPendingStore(store)
	ctx := WithRequestScope(context.Background(), RequestScope{OwnerID: "owner-it", SessionID: "session-it"})
	connID, caps, err := m.ConnectFor(ctx, profile.ID, "owner-it", "session-it")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if caps.Transactions {
		t.Fatal("Access must not advertise transactions")
	}
	if !caps.Write {
		t.Fatal("write profile must advertise write")
	}
	args := map[string]interface{}{
		"action": "execute", "connection_id": connID,
		"sql":    "UPDATE People SET amount = :amount WHERE id = :id",
		"params": map[string]interface{}{"amount": 99.5, "id": 1}, "dry_run": false,
	}
	fp, paramsFP, err := MutationFingerprints(args)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := m.IssueApproval(ApprovalRequest{ProfileID: profile.ID, SQLFingerprint: fp, ParamsFingerprint: paramsFP, OwnerID: "owner-it", SessionID: "session-it"})
	if err != nil {
		t.Fatalf("IssueApproval: %v", err)
	}
	preview := HandleTool(ctx, m, map[string]interface{}{
		"action": "execute", "connection_id": connID,
		"sql":    "UPDATE People SET amount = :amount WHERE id = :id",
		"params": map[string]interface{}{"amount": 99.5, "id": 1}, "dry_run": true,
	})
	if !strings.Contains(preview, `"dry_run_guarantee":"policy_only"`) {
		t.Fatalf("access dry-run = %s", preview)
	}
	raw := HandleTool(WithApprovalContext(ctx, approval), m, args)
	if !strings.Contains(raw, `"ok":true`) && !strings.Contains(raw, `"affected_rows"`) {
		t.Fatalf("access write failed: %s", raw)
	}
	if _, err := os.Stat(work + accessPrewriteBackupSuffix); err != nil {
		t.Fatalf("pre-write backup missing: %v", err)
	}
	got := mustToolResult[QueryResult](t, HandleTool(ctx, m, map[string]interface{}{
		"action": "query", "connection_id": connID,
		"sql":    "SELECT amount FROM People WHERE id = :id",
		"params": map[string]interface{}{"id": 1}, "limit": 1,
	}))
	if got.RowCount != 1 || !strings.Contains(fmt.Sprint(got.Rows[0][0]), "99.5") {
		t.Fatalf("written row = %+v", got)
	}
	compact := HandleTool(ctx, m, map[string]interface{}{
		"action": "execute", "connection_id": connID,
		"sql": "COMPACT DATABASE", "dry_run": true,
	})
	if !strings.Contains(compact, "permission") && !strings.Contains(compact, "unsupported") && !strings.Contains(compact, "unknown") {
		t.Fatalf("compact/repair must be rejected, got: %s", compact)
	}
}
