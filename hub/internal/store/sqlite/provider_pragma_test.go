package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestDSNWithConnectionPragmasAppendsWithoutDuplicating(t *testing.T) {
	plain := dsnWithConnectionPragmas(filepath.Join("data", "hub.db"), Config{BusyTimeoutMS: 1000, WAL: true}, false)
	if !strings.Contains(plain, "?_pragma=busy_timeout(1000)") || strings.Contains(plain, "wal_autocheckpoint") {
		t.Fatalf("plain dsn = %s", plain)
	}
	withQuery := dsnWithConnectionPragmas("file:hub.db?mode=rwc", Config{BusyTimeoutMS: 1000, WAL: true}, true)
	if !strings.Contains(withQuery, "&_pragma=busy_timeout(1000)") || !strings.Contains(withQuery, "wal_autocheckpoint(2000)") || strings.Contains(withQuery, "??") {
		t.Fatalf("file dsn = %s", withQuery)
	}
	again := dsnWithConnectionPragmas(withQuery, Config{BusyTimeoutMS: 9999, WAL: true}, true)
	if strings.Count(strings.ToLower(again), "busy_timeout") != 1 || strings.Count(again, "wal_autocheckpoint") != 1 {
		t.Fatalf("duplicated pragmas: %s", again)
	}
}

func TestReadConnectionsInheritBusyTimeout(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "pragma.db"),
		WAL:               true,
		BusyTimeoutMS:     12345,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  4,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	ctx := context.Background()
	held := make([]*sql.Conn, 0, 4)
	t.Cleanup(func() {
		for _, conn := range held {
			_ = conn.Close()
		}
	})
	for i := 0; i < 4; i++ {
		conn, err := provider.Read.Conn(ctx)
		if err != nil {
			t.Fatalf("conn %d: %v", i, err)
		}
		held = append(held, conn)
		var timeout int
		if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout); err != nil {
			t.Fatalf("conn %d busy_timeout: %v", i, err)
		}
		if timeout != 12345 {
			t.Fatalf("conn %d busy_timeout = %d, want 12345", i, timeout)
		}
		var foreignKeys int
		if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatalf("conn %d foreign_keys: %v", i, err)
		}
		if foreignKeys != 1 {
			t.Fatalf("conn %d foreign_keys = %d, want 1", i, foreignKeys)
		}
	}
}
