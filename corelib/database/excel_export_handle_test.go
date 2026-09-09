package database

import (
	"context"
	"strings"
	"testing"
)

func TestExportExcelFromResultHandle(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(nil, nil)
	defer m.Close()
	m.SetResultStoreDir(dir)
	m.SetWorkspaceRoot(dir)
	handle := m.storeResultAtWithConnection(QueryResult{
		Columns: []Column{{Name: "n"}},
		Rows:    [][]interface{}{{"a"}},
		allRows: [][]interface{}{{"a"}, {"b"}},
	}, "owner", "session", "", 0)
	if handle == "" {
		t.Fatal("missing handle")
	}
	ctx := WithRequestScope(context.Background(), RequestScope{OwnerID: "owner", SessionID: "session"})
	preview := HandleTool(ctx, m, map[string]interface{}{
		"action": "export_excel", "result_handle": handle, "file_path": "out.xlsx", "dry_run": true,
	})
	if !strings.Contains(preview, `"row_count":2`) {
		t.Fatalf("handle export preview = %s", preview)
	}
	ignored := HandleTool(ctx, m, map[string]interface{}{
		"action": "export_excel", "result_handle": handle,
		"rows": []interface{}{[]interface{}{"injected"}}, "file_path": "out.xlsx", "dry_run": true,
	})
	if !strings.Contains(ignored, `"row_count":2`) || strings.Contains(ignored, "injected") {
		t.Fatalf("client rows must not override handle: %s", ignored)
	}
	other := WithRequestScope(context.Background(), RequestScope{OwnerID: "other", SessionID: "session"})
	denied := HandleTool(other, m, map[string]interface{}{
		"action": "export_excel", "result_handle": handle, "file_path": "out.xlsx", "dry_run": true,
	})
	if !strings.Contains(denied, "permission") && !strings.Contains(denied, "another session") {
		t.Fatalf("cross-owner export = %s", denied)
	}
	if _, err := m.SnapshotResultHandle(handle, "owner", "session", ""); err != nil {
		t.Fatalf("export must not consume the handle: %v", err)
	}
}

func TestExportExcelFromHandleAppliesProfilePolicy(t *testing.T) {
	dir := t.TempDir()
	readOnly := Profile{ID: "crm", Type: SourcePostgres, Host: "db", Database: "app", ReadOnly: true}
	writable := Profile{ID: "crm", Type: SourcePostgres, Host: "db", Database: "app", WriteEnabled: true}
	ctx := WithRequestScope(context.Background(), RequestScope{OwnerID: "owner", SessionID: "session"})

	denied := NewManager([]Profile{readOnly}, nil)
	defer denied.Close()
	denied.SetResultStoreDir(dir)
	denied.SetWorkspaceRoot(dir)
	handle := denied.storeResultAtWithConnection(QueryResult{
		ProfileID: "crm",
		Columns:   []Column{{Name: "n"}},
		allRows:   [][]interface{}{{"a"}},
	}, "owner", "session", "", 0)
	got := HandleTool(ctx, denied, map[string]interface{}{
		"action": "export_excel", "result_handle": handle, "file_path": "out.xlsx", "dry_run": true,
	})
	if !strings.Contains(got, "permission") {
		t.Fatalf("read-only handle export = %s", got)
	}

	allowed := NewManager([]Profile{writable}, nil)
	defer allowed.Close()
	allowed.SetResultStoreDir(dir)
	allowed.SetWorkspaceRoot(dir)
	handle = allowed.storeResultAtWithConnection(QueryResult{
		ProfileID: "crm",
		Columns:   []Column{{Name: "n"}},
		allRows:   [][]interface{}{{"a"}},
	}, "owner", "session", "", 0)
	got = HandleTool(ctx, allowed, map[string]interface{}{
		"action": "export_excel", "result_handle": handle, "file_path": "out.xlsx", "dry_run": true, "profile_id": "spoof",
	})
	if !strings.Contains(got, `"row_count":1`) {
		t.Fatalf("write-enabled handle export = %s", got)
	}

	classified := NewManager([]Profile{{ID: "crm", Type: SourcePostgres, Host: "db", Database: "app", WriteEnabled: true, DataClassification: "confidential", Disabled: true}}, nil)
	defer classified.Close()
	classified.SetResultStoreDir(dir)
	classified.SetWorkspaceRoot(dir)
	handle = classified.storeResultAtWithConnection(QueryResult{
		ProfileID: "crm",
		Columns:   []Column{{Name: "n"}},
		allRows:   [][]interface{}{{"a"}},
	}, "owner", "session", "", 0)
	got = HandleTool(ctx, classified, map[string]interface{}{
		"action": "export_excel", "result_handle": handle, "file_path": "out.xlsx", "dry_run": true,
	})
	if !strings.Contains(got, "disabled") && !strings.Contains(got, "permission") {
		t.Fatalf("disabled classified handle export = %s", got)
	}
}

func TestExportExcelFromHandleCommitKeepsSnapshotProfile(t *testing.T) {
	dir := t.TempDir()
	m := NewManager([]Profile{{ID: "crm", Type: SourcePostgres, Host: "db", Database: "app", WriteEnabled: true, SchemaVersion: 4}}, nil)
	defer m.Close()
	m.SetResultStoreDir(dir)
	m.SetWorkspaceRoot(dir)
	handle := m.storeResultAtWithConnection(QueryResult{
		ProfileID: "crm",
		Columns:   []Column{{Name: "n"}},
		allRows:   [][]interface{}{{"a"}},
	}, "owner", "session", "", 0)
	ctx := WithApprovalContext(
		WithRequestScope(context.Background(), RequestScope{OwnerID: "owner", SessionID: "session"}),
		ApprovalContext{Token: "trusted-token", ID: "approval-export-1", ProfileID: "crm", SchemaVersion: 4, SQLFingerprint: sqlFingerprint("export_excel:out.xlsx")},
	)
	got := HandleTool(ctx, m, map[string]interface{}{
		"action": "export_excel", "result_handle": handle, "file_path": "out.xlsx", "dry_run": false,
	})
	if !strings.Contains(got, `"ok":true`) {
		t.Fatalf("handle export commit = %s", got)
	}
}

func TestSnapshotResultHandleAllowsEmptyConnectionID(t *testing.T) {
	m := NewManager(nil, nil)
	defer m.Close()
	m.items["conn-1"] = &toolTestAdapter{}
	m.bindings["conn-1"] = connectionBinding{ownerID: "owner", sessionID: "session"}
	handle := m.storeResultAtWithConnection(QueryResult{
		Columns: []Column{{Name: "n"}},
		allRows: [][]interface{}{{"a"}, {"b"}},
	}, "owner", "session", "conn-1", 0)
	if handle == "" {
		t.Fatal("missing handle")
	}
	got, err := m.SnapshotResultHandle(handle, "owner", "session", "")
	if err != nil || got.RowCount != 2 {
		t.Fatalf("empty connection_id should use the handle binding: %+v err=%v", got, err)
	}
	if _, err := m.SnapshotResultHandle(handle, "owner", "session", "conn-other"); err == nil || !strings.Contains(err.Error(), "another connection") {
		t.Fatalf("mismatched connection_id = %v", err)
	}
	page, err := m.readResultPage(handle, "owner", "session", 10)
	if err != nil || page.RowCount != 2 {
		t.Fatalf("paging without connection_id = %+v err=%v", page, err)
	}
}

func TestSnapshotResultHandleRejectsClosedConnection(t *testing.T) {
	m := NewManager(nil, nil)
	defer m.Close()
	handle := m.storeResultAtWithConnection(QueryResult{
		Columns: []Column{{Name: "n"}},
		allRows: [][]interface{}{{1}, {2}},
	}, "owner", "session", "conn-missing", 0)
	_, err := m.SnapshotResultHandle(handle, "owner", "session", "conn-missing")
	if err == nil || !strings.Contains(err.Error(), "closed") && !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}
