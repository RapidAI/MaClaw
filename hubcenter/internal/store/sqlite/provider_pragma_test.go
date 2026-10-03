package sqlite

import (
	"context"
	"database/sql/driver"
	"path/filepath"
	"testing"
	"time"
)

// Two checked-out writer connections must both see busy_timeout and
// foreign_keys. Those pragmas are connection-scoped; a single db.Exec only
// reaches one connection in the pool.
func TestConnectionPragmasApplyToEveryPooledConnection(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "pragma-pool.db"),
		WAL:               true,
		BusyTimeoutMS:     4321,
		MaxWriteOpenConns: 2,
		MaxWriteIdleConns: 2,
		MaxReadOpenConns:  1,
		MaxReadIdleConns:  1,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := provider.Write.Conn(ctx)
	if err != nil {
		t.Fatalf("first conn: %v", err)
	}
	defer first.Close()
	second, err := provider.Write.Conn(ctx)
	if err != nil {
		t.Fatalf("second conn: %v", err)
	}
	defer second.Close()

	var busy1, busy2, fk1, fk2 int
	if err := first.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy1); err != nil {
		t.Fatalf("busy_timeout on first: %v", err)
	}
	if err := second.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy2); err != nil {
		t.Fatalf("busy_timeout on second: %v", err)
	}
	if err := first.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk1); err != nil {
		t.Fatalf("foreign_keys on first: %v", err)
	}
	if err := second.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk2); err != nil {
		t.Fatalf("foreign_keys on second: %v", err)
	}
	if busy1 != 4321 || busy2 != 4321 {
		t.Fatalf("busy_timeout = %d and %d, want 4321 on both connections", busy1, busy2)
	}
	if fk1 != 1 || fk2 != 1 {
		t.Fatalf("foreign_keys = %d and %d, want 1 on both connections", fk1, fk2)
	}
}

// A raw BEGIN whose ROLLBACK never reaches SQLite (the request context is
// already canceled) used to put the only write connection back in the pool
// still inside a transaction. The next BeginTx then failed with the error
// users see on model share: "cannot start a transaction within a transaction".
func TestAbandonedBeginDoesNotPoisonNextTransaction(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "tx-reset.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
		MaxReadOpenConns:  1,
		MaxReadIdleConns:  1,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	ctx := context.Background()
	if _, err := provider.Write.ExecContext(ctx, `CREATE TABLE poison (id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	workCtx, cancel := context.WithCancel(ctx)
	conn, err := provider.Write.Conn(workCtx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	// busy_timeout is per connection. A discarded connection is reopened with
	// the connector's 5000; a rolled-back one keeps this value.
	if _, err := conn.ExecContext(workCtx, "PRAGMA busy_timeout = 7777"); err != nil {
		cancel()
		_ = conn.Close()
		t.Fatalf("busy_timeout: %v", err)
	}
	if _, err := conn.ExecContext(workCtx, "BEGIN IMMEDIATE"); err != nil {
		cancel()
		_ = conn.Close()
		t.Fatalf("BEGIN IMMEDIATE: %v", err)
	}
	if _, err := conn.ExecContext(workCtx, `INSERT INTO poison (id, v) VALUES (1, 'abandoned')`); err != nil {
		cancel()
		_ = conn.Close()
		t.Fatalf("insert: %v", err)
	}
	cancel()
	if _, err := conn.ExecContext(workCtx, "ROLLBACK"); err == nil {
		_ = conn.Close()
		t.Fatal("ROLLBACK with a canceled context succeeded; the test no longer reproduces a skipped rollback")
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	tx, err := provider.Write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx after abandoned BEGIN: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO poison (id, v) VALUES (2, 'kept')`); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert in new transaction: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var abandoned, kept int
	if err := provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM poison WHERE v = 'abandoned'`).Scan(&abandoned); err != nil {
		t.Fatalf("count abandoned: %v", err)
	}
	if err := provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM poison WHERE v = 'kept'`).Scan(&kept); err != nil {
		t.Fatalf("count kept: %v", err)
	}
	if abandoned != 0 || kept != 1 {
		t.Fatalf("abandoned=%d kept=%d, want 0 and 1", abandoned, kept)
	}
	var busy int
	if err := provider.Write.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatalf("busy_timeout after reuse: %v", err)
	}
	if busy != 7777 {
		t.Fatalf("busy_timeout = %d, want 7777 (the write connection was discarded instead of rolled back)", busy)
	}
}

// rollbackRawTx must finish the transaction on the connection that is still
// checked out. ResetSession has not run yet, so a second BEGIN is the
// observation that SQLite itself left the transaction.
func TestRollbackRawTxClearsTheOpenTransaction(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "raw-rollback.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
		MaxReadOpenConns:  1,
		MaxReadIdleConns:  1,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	workCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, err := provider.Write.Conn(workCtx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(workCtx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("BEGIN IMMEDIATE: %v", err)
	}
	cancel()
	rollbackRawTx(conn)
	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("BEGIN after rollbackRawTx: %v", err)
	}
	rollbackRawTx(conn)
}

// A BEGIN that bypasses the wrapper (openTx stays false) must not fail the
// call that next uses the connection. The abandoned work is rolled back and
// the same connection is kept.
func TestUntrackedBeginDoesNotFailTheNextTransaction(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "untracked-tx.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
		MaxReadOpenConns:  1,
		MaxReadIdleConns:  1,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	ctx := context.Background()
	if _, err := provider.Write.ExecContext(ctx, `CREATE TABLE poison (id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	conn, err := provider.Write.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout = 7777"); err != nil {
		_ = conn.Close()
		t.Fatalf("busy_timeout: %v", err)
	}
	if err := conn.Raw(func(dc any) error {
		wrapper := dc.(*txResetConn)
		execer := wrapper.Conn.(driver.ExecerContext)
		if _, err := execer.ExecContext(ctx, "BEGIN IMMEDIATE", nil); err != nil {
			return err
		}
		if _, err := execer.ExecContext(ctx, `INSERT INTO poison (id, v) VALUES (1, 'abandoned')`, nil); err != nil {
			return err
		}
		wrapper.openTx = false
		return nil
	}); err != nil {
		_ = conn.Close()
		t.Fatalf("raw begin: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	tx, err := provider.Write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx after untracked BEGIN: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO poison (id, v) VALUES (2, 'kept')`); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var abandoned, kept, busy int
	if err := provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM poison WHERE v = 'abandoned'`).Scan(&abandoned); err != nil {
		t.Fatalf("count abandoned: %v", err)
	}
	if err := provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM poison WHERE v = 'kept'`).Scan(&kept); err != nil {
		t.Fatalf("count kept: %v", err)
	}
	if abandoned != 0 || kept != 1 {
		t.Fatalf("abandoned=%d kept=%d, want 0 and 1", abandoned, kept)
	}
	if err := provider.Write.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatalf("busy_timeout: %v", err)
	}
	if busy != 7777 {
		t.Fatalf("busy_timeout = %d, want 7777 (the write connection was discarded)", busy)
	}
}

// A second BEGIN on a connection that is already inside a tracked
// transaction must fail and leave the first transaction's rows in place.
func TestNestedBeginDoesNotDiscardTrackedWork(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "tracked-begin.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
		MaxReadOpenConns:  1,
		MaxReadIdleConns:  1,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	ctx := context.Background()
	if _, err := provider.Write.ExecContext(ctx, `CREATE TABLE poison (id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	conn, err := provider.Write.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("BEGIN: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO poison (id, v) VALUES (1, 'kept')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err == nil || !sqliteErrNestedTx(err) {
		t.Fatalf("second BEGIN err = %v, want nested transaction", err)
	}
	if _, err := conn.BeginTx(ctx, nil); err == nil || !sqliteErrNestedTx(err) {
		t.Fatalf("BeginTx err = %v, want nested transaction", err)
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var n int
	if err := provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM poison WHERE v = 'kept'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("kept rows = %d, want 1", n)
	}
}

// The next raw BEGIN has to clear an untracked leftover itself. Heartbeat
// writes use BEGIN IMMEDIATE, not database/sql BeginTx, and a failed BEGIN
// returns that connection to the pool still inside the transaction.
func TestUntrackedBeginSelfHealsOnRawBegin(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "untracked-raw-begin.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
		MaxReadOpenConns:  1,
		MaxReadIdleConns:  1,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	ctx := context.Background()
	if _, err := provider.Write.ExecContext(ctx, `CREATE TABLE poison (id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	conn, err := provider.Write.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout = 7777"); err != nil {
		_ = conn.Close()
		t.Fatalf("busy_timeout: %v", err)
	}
	if err := conn.Raw(func(dc any) error {
		wrapper := dc.(*txResetConn)
		execer := wrapper.Conn.(driver.ExecerContext)
		if _, err := execer.ExecContext(ctx, "BEGIN IMMEDIATE", nil); err != nil {
			return err
		}
		if _, err := execer.ExecContext(ctx, `INSERT INTO poison (id, v) VALUES (1, 'abandoned')`, nil); err != nil {
			return err
		}
		wrapper.openTx = false
		return nil
	}); err != nil {
		_ = conn.Close()
		t.Fatalf("raw begin: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	conn, err = provider.Write.Conn(ctx)
	if err != nil {
		t.Fatalf("second conn: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		_ = conn.Close()
		t.Fatalf("BEGIN after untracked: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO poison (id, v) VALUES (2, 'kept')`); err != nil {
		_ = conn.Close()
		t.Fatalf("insert: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		_ = conn.Close()
		t.Fatalf("commit: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close kept: %v", err)
	}

	var abandoned, kept, busy int
	if err := provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM poison WHERE v = 'abandoned'`).Scan(&abandoned); err != nil {
		t.Fatalf("count abandoned: %v", err)
	}
	if err := provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM poison WHERE v = 'kept'`).Scan(&kept); err != nil {
		t.Fatalf("count kept: %v", err)
	}
	if abandoned != 0 || kept != 1 {
		t.Fatalf("abandoned=%d kept=%d, want 0 and 1", abandoned, kept)
	}
	if err := provider.Write.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatalf("busy_timeout: %v", err)
	}
	if busy != 7777 {
		t.Fatalf("busy_timeout = %d, want 7777 (the write connection was discarded)", busy)
	}
}

// A BEGIN script that fails after the transaction has started must not leave
// that transaction in the pool. The next plain write has to commit on its own.
func TestFailedBeginScriptDoesNotSwallowTheNextWrite(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "failed-begin-script.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
		MaxReadOpenConns:  1,
		MaxReadIdleConns:  1,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	ctx := context.Background()
	if _, err := provider.Write.ExecContext(ctx, `CREATE TABLE poison (id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	conn, err := provider.Write.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout = 7777"); err != nil {
		_ = conn.Close()
		t.Fatalf("busy_timeout: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, err = provider.Write.ExecContext(ctx, `
		BEGIN IMMEDIATE;
		INSERT INTO poison (id, v) VALUES (1, 'abandoned');
		SELECT * FROM no_such_table
	`)
	if err == nil {
		t.Fatal("script succeeded, want an error after BEGIN")
	}

	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO poison (id, v) VALUES (2, 'kept')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var abandoned, kept, busy int
	if err := provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM poison WHERE v = 'abandoned'`).Scan(&abandoned); err != nil {
		t.Fatalf("count abandoned: %v", err)
	}
	if err := provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM poison WHERE v = 'kept'`).Scan(&kept); err != nil {
		t.Fatalf("count kept: %v", err)
	}
	if abandoned != 0 || kept != 1 {
		t.Fatalf("abandoned=%d kept=%d, want 0 and 1", abandoned, kept)
	}
	if err := provider.Write.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatalf("busy_timeout: %v", err)
	}
	if busy != 7777 {
		t.Fatalf("busy_timeout = %d, want 7777 (the write connection was discarded)", busy)
	}
}

// An untracked transaction makes the next BEGIN fail with the nested
// error. Recovery retries the whole script, and that retry can open a new
// transaction before a later statement fails. The following plain write
// still has to commit on its own.
func TestRetriedBeginScriptDoesNotSwallowTheNextWrite(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "retried-begin-script.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
		MaxReadOpenConns:  1,
		MaxReadIdleConns:  1,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	ctx := context.Background()
	if _, err := provider.Write.ExecContext(ctx, `CREATE TABLE poison (id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	conn, err := provider.Write.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout = 7777"); err != nil {
		_ = conn.Close()
		t.Fatalf("busy_timeout: %v", err)
	}
	if err := conn.Raw(func(dc any) error {
		wrapper := dc.(*txResetConn)
		execer := wrapper.Conn.(driver.ExecerContext)
		if _, err := execer.ExecContext(ctx, "BEGIN IMMEDIATE", nil); err != nil {
			return err
		}
		if _, err := execer.ExecContext(ctx, `INSERT INTO poison (id, v) VALUES (1, 'abandoned')`, nil); err != nil {
			return err
		}
		wrapper.openTx = false
		return nil
	}); err != nil {
		_ = conn.Close()
		t.Fatalf("raw begin: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, err = provider.Write.ExecContext(ctx, `
		BEGIN IMMEDIATE;
		INSERT INTO poison (id, v) VALUES (2, 'script');
		SELECT * FROM no_such_table
	`)
	if err == nil {
		t.Fatal("script succeeded, want an error after the retried BEGIN")
	}

	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO poison (id, v) VALUES (3, 'kept')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var abandoned, script, kept, busy int
	if err := provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM poison WHERE v = 'abandoned'`).Scan(&abandoned); err != nil {
		t.Fatalf("count abandoned: %v", err)
	}
	if err := provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM poison WHERE v = 'script'`).Scan(&script); err != nil {
		t.Fatalf("count script: %v", err)
	}
	if err := provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM poison WHERE v = 'kept'`).Scan(&kept); err != nil {
		t.Fatalf("count kept: %v", err)
	}
	if abandoned != 0 || script != 0 || kept != 1 {
		t.Fatalf("abandoned=%d script=%d kept=%d, want 0, 0, and 1", abandoned, script, kept)
	}
	if err := provider.Write.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatalf("busy_timeout: %v", err)
	}
	if busy != 7777 {
		t.Fatalf("busy_timeout = %d, want 7777 (the write connection was discarded)", busy)
	}
}

func TestSQLiteTxKindOf(t *testing.T) {
	cases := []struct {
		query string
		want  sqliteTxKind
	}{
		{query: "BEGIN IMMEDIATE", want: sqliteTxBegin},
		{query: "  begin deferred", want: sqliteTxBegin},
		{query: "COMMIT", want: sqliteTxEnd},
		{query: "rollback", want: sqliteTxEnd},
		{query: "END;", want: sqliteTxEnd},
		{query: "INSERT INTO begin_log VALUES (1)", want: sqliteTxNone},
		{query: "UPDATE t SET v = 'BEGIN'", want: sqliteTxNone},
		{query: "BEGINNING", want: sqliteTxNone},
		{query: "ENDING", want: sqliteTxNone},
	}
	for _, tc := range cases {
		if got := sqliteTxKindOf(tc.query); got != tc.want {
			t.Errorf("sqliteTxKindOf(%q) = %d, want %d", tc.query, got, tc.want)
		}
	}
}
