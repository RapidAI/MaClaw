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

func TestDSNWithConnectionPragmasIgnoresNamesInFilePath(t *testing.T) {
	path := filepath.Join("data", "synchronous", "foreign_keys", "busy_timeout", "temp_store", "wal_autocheckpoint.db")
	out := dsnWithConnectionPragmas(path, Config{BusyTimeoutMS: 1000, WAL: true}, true)
	for _, want := range []string{
		"?_pragma=busy_timeout(1000)",
		"_pragma=foreign_keys(1)",
		"_pragma=synchronous(NORMAL)",
		"_pragma=temp_store(MEMORY)",
		"_pragma=wal_autocheckpoint(2000)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s in %s", want, out)
		}
	}
	kept := dsnWithConnectionPragmas(path+"?_pragma=foreign_keys(0)", Config{BusyTimeoutMS: 1000, WAL: true}, true)
	if strings.Count(kept, "_pragma=foreign_keys") != 1 {
		t.Fatalf("foreign_keys duplicated: %s", kept)
	}
	if !strings.Contains(kept, "_pragma=synchronous(NORMAL)") || strings.Contains(kept, "foreign_keys(1)") {
		t.Fatalf("path word or existing pragma handled wrong: %s", kept)
	}
}

func TestFileURIDSNWithConnectionPragmasOpens(t *testing.T) {
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "pragma.db"))
	dsn := dsnWithConnectionPragmas("file:"+path+"?mode=rwc", Config{BusyTimeoutMS: 12345, WAL: true}, true)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open %s: %v", dsn, err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("conn %s: %v", dsn, err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var timeout, foreignKeys, synchronous, tempStore, checkpoint int
	if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout); err != nil {
		t.Fatal(err)
	}
	if timeout != 12345 {
		t.Fatalf("busy_timeout = %d", timeout)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d", foreignKeys)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous); err != nil {
		t.Fatal(err)
	}
	if synchronous != 1 {
		t.Fatalf("synchronous = %d, want NORMAL (1)", synchronous)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA temp_store").Scan(&tempStore); err != nil {
		t.Fatal(err)
	}
	if tempStore != 2 {
		t.Fatalf("temp_store = %d, want MEMORY (2)", tempStore)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA wal_autocheckpoint").Scan(&checkpoint); err != nil {
		t.Fatal(err)
	}
	if checkpoint != 2000 {
		t.Fatalf("wal_autocheckpoint = %d", checkpoint)
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
