package agentservice

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteRecordStorePersistsAndFiltersRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.db")
	store, err := NewSQLiteRecordStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteRecordStore: %v", err)
	}
	defer store.Close()
	now := time.Now().UTC()
	record := StructuredRecord{
		ID:         "record_1",
		TenantID:   "tenant_1",
		UserID:     "user_1",
		Collection: "finance",
		Title:      "March payroll",
		Tags:       []string{"finance", "payroll"},
		Data:       map[string]any{"amount": 12000, "department": "R&D"},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := store.SaveStructuredRecord(record); err != nil {
		t.Fatalf("SaveStructuredRecord: %v", err)
	}

	got, err := store.GetStructuredRecord("tenant_1", "user_1", "finance", "record_1")
	if err != nil {
		t.Fatalf("GetStructuredRecord: %v", err)
	}
	if got.Title != record.Title || got.Data["department"] != "R&D" || len(got.Tags) != 2 {
		t.Fatalf("unexpected record: %#v", got)
	}

	items, err := store.ListStructuredRecords("tenant_1", "user_1", ListStructuredRecordsInput{Collection: "finance", Tag: "payroll", Q: "march", Limit: 10})
	if err != nil {
		t.Fatalf("ListStructuredRecords: %v", err)
	}
	if len(items) != 1 || items[0].ID != record.ID {
		t.Fatalf("unexpected filtered records: %#v", items)
	}

	reopened, err := NewSQLiteRecordStore(path)
	if err != nil {
		t.Fatalf("reopen NewSQLiteRecordStore: %v", err)
	}
	defer reopened.Close()
	got, err = reopened.GetStructuredRecord("tenant_1", "user_1", "finance", "record_1")
	if err != nil {
		t.Fatalf("GetStructuredRecord after reopen: %v", err)
	}
	if got.Data["amount"] != float64(12000) {
		t.Fatalf("unexpected reopened record: %#v", got)
	}
}

func TestSQLiteRecordStoreDeletesRecords(t *testing.T) {
	store, err := NewSQLiteRecordStore(filepath.Join(t.TempDir(), "records.db"))
	if err != nil {
		t.Fatalf("NewSQLiteRecordStore: %v", err)
	}
	defer store.Close()
	now := time.Now().UTC()
	record := StructuredRecord{ID: "record_1", TenantID: "tenant_1", UserID: "user_1", Collection: "hr", Data: map[string]any{"name": "Alice"}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveStructuredRecord(record); err != nil {
		t.Fatalf("SaveStructuredRecord: %v", err)
	}
	if err := store.DeleteStructuredRecord("tenant_1", "user_1", "hr", "record_1"); err != nil {
		t.Fatalf("DeleteStructuredRecord: %v", err)
	}
	if _, err := store.GetStructuredRecord("tenant_1", "user_1", "hr", "record_1"); err != ErrRecordNotFound {
		t.Fatalf("GetStructuredRecord after delete error = %v, want ErrRecordNotFound", err)
	}
}
