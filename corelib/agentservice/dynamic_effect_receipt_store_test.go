package agentservice

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteDynamicEffectReceiptStorePersistsAndRejectsConflictingReceipt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipts.db")
	first, err := NewSQLiteDynamicEffectReceiptStore(path)
	if err != nil {
		t.Fatal(err)
	}
	record, err := first.Accept("operation-1", "trusted-receipt", time.Now().UTC())
	if err != nil || record.OperationID != "operation-1" || record.ReceiptDigest == "" {
		t.Fatalf("accepted record=%#v err=%v", record, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := NewSQLiteDynamicEffectReceiptStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	loaded, err := second.Get("operation-1")
	if err != nil || loaded.ReceiptDigest != record.ReceiptDigest {
		t.Fatalf("loaded record=%#v err=%v", loaded, err)
	}
	if _, err := second.Accept("operation-1", "trusted-receipt", time.Now().UTC()); err != nil {
		t.Fatalf("idempotent receipt rejected: %v", err)
	}
	if _, err := second.Accept("operation-1", "different-receipt", time.Now().UTC()); err == nil {
		t.Fatal("conflicting receipt accepted")
	}
}
