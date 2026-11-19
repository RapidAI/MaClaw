package guiapp

import (
	"errors"
	"strings"
	"testing"
)

func TestMemorySettingTransactionRollsBackApplyFailure(t *testing.T) {
	applyErr := errors.New("apply failed")
	rolledBack := false
	persisted := false
	err := runMemorySettingTransaction(
		func() error { return applyErr },
		func() error { persisted = true; return nil },
		func() error { rolledBack = true; return nil },
	)
	if !errors.Is(err, applyErr) {
		t.Fatalf("transaction error = %v", err)
	}
	if persisted || !rolledBack {
		t.Fatalf("persisted=%v rolledBack=%v", persisted, rolledBack)
	}
}

func TestMemorySettingTransactionRollsBackPersistFailure(t *testing.T) {
	persistErr := errors.New("persist failed")
	rolledBack := false
	err := runMemorySettingTransaction(
		func() error { return nil },
		func() error { return persistErr },
		func() error { rolledBack = true; return nil },
	)
	if !errors.Is(err, persistErr) || !rolledBack {
		t.Fatalf("err=%v rolledBack=%v", err, rolledBack)
	}
}

func TestMemorySettingTransactionSuccessDoesNotRollback(t *testing.T) {
	rolledBack := false
	if err := runMemorySettingTransaction(
		func() error { return nil },
		func() error { return nil },
		func() error { rolledBack = true; return nil },
	); err != nil {
		t.Fatal(err)
	}
	if rolledBack {
		t.Fatal("successful transaction invoked rollback")
	}
}

func TestMemorySettingTransactionReportsRollbackFailure(t *testing.T) {
	applyErr := errors.New("apply failed")
	rollbackErr := errors.New("rollback failed")
	err := runMemorySettingTransaction(
		func() error { return applyErr },
		func() error { return nil },
		func() error { return rollbackErr },
	)
	if !errors.Is(err, applyErr) || !strings.Contains(err.Error(), "rollback failed") {
		t.Fatalf("transaction error = %v", err)
	}
}
