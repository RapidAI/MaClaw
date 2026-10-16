package database

import (
	"path/filepath"
	"testing"
	"time"
)

func TestResultHandleSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(nil, nil)
	m.SetResultStoreDir(dir)
	id := m.storeResultForConnection(QueryResult{
		Columns: []Column{{Name: "n"}},
		Rows:    [][]interface{}{{1}},
		allRows: [][]interface{}{{1}, {2}, {3}},
	}, "owner", "session", "conn", 1)
	if id == "" {
		t.Fatal("expected handle")
	}
	m2 := NewManager(nil, nil)
	m2.SetResultStoreDir(dir)
	page, err := m2.readResultPageFor(id, "owner", "session", "conn", 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.RowCount != 2 {
		t.Fatalf("row_count=%d want remaining rows", page.RowCount)
	}
}

func TestClassifiedExportBlocksExternal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.xlsx")
	NoteClassifiedExport(path, "confidential")
	class, ok := ClassifiedExport(path)
	if !ok || class != "confidential" {
		t.Fatalf("got %q %v", class, ok)
	}
	if !BlocksExternalDelivery(class) {
		t.Fatal("confidential must block IM upload")
	}
	if BlocksExternalDelivery("public") {
		t.Fatal("public must not block")
	}
}

func TestLookupReceipt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database_audit.jsonl")
	store, err := NewFileAuditStore(path)
	if err != nil {
		t.Fatal(err)
	}
	store.Sink()(t.Context(), AuditEvent{Action: "execute", ResultClass: "ok", ReceiptID: "db-receipt-1", Timestamp: time.Now()})
	got, err := store.LookupReceipt("db-receipt-1")
	if err != nil || got["receipt_id"] != "db-receipt-1" {
		t.Fatalf("got %#v err=%v", got, err)
	}
}
