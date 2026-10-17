package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestIsBusyMatchesSQLiteText(t *testing.T) {
	if !IsBusy(errors.New("write monitor lease failed: database is locked (SQLITE_BUSY)")) {
		t.Fatal("expected busy")
	}
	if IsBusy(errors.New("no such table: ha_sync_ops")) {
		t.Fatal("schema error is not busy")
	}
	if IsBusy(nil) {
		t.Fatal("nil is not busy")
	}
}

func TestSystemSetRetriesWhileDatabaseIsLocked(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "busy.db")
	provider, err := NewProvider(Config{
		DSN: dbPath, WAL: true, BusyTimeoutMS: 30,
		MaxReadOpenConns: 1, MaxReadIdleConns: 1, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	holder, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open holder: %v", err)
	}
	defer holder.Close()
	holder.SetMaxOpenConns(1)
	if _, err := holder.Exec(`PRAGMA busy_timeout = 50`); err != nil {
		t.Fatalf("holder pragma: %v", err)
	}
	if _, err := holder.Exec(`BEGIN IMMEDIATE`); err != nil {
		t.Fatalf("begin: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- (&systemRepo{db: provider.Write, readDB: provider.Read}).Set(context.Background(), "lease", `{"owner":"hc-1"}`)
	}()
	time.Sleep(80 * time.Millisecond)
	if _, err := holder.Exec(`COMMIT`); err != nil {
		t.Fatalf("commit: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Set after unlock: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Set did not return")
	}
	var got string
	if err := provider.Write.QueryRow(`SELECT value_json FROM system_settings WHERE key = 'lease'`).Scan(&got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != `{"owner":"hc-1"}` {
		t.Fatalf("value = %s", got)
	}
}
