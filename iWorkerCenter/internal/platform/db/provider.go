package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Provider holds read and write database connections for iWorkerCenter.
type Provider struct {
	Write *sql.DB
	Read  *sql.DB
}

// Open creates a new Provider with separate read/write connections.
func Open(dsn string) (*Provider, error) {
	if err := ensureParentDir(dsn); err != nil {
		return nil, err
	}

	// For in-memory databases, use shared cache so read/write see the same data.
	effectiveDSN := dsn
	if dsn == ":memory:" {
		effectiveDSN = "file::memory:?cache=shared"
	}

	writeDB, err := sql.Open("sqlite", effectiveDSN)
	if err != nil {
		return nil, fmt.Errorf("open write db: %w", err)
	}

	readDB, err := sql.Open("sqlite", effectiveDSN)
	if err != nil {
		_ = writeDB.Close()
		return nil, fmt.Errorf("open read db: %w", err)
	}

	writeDB.SetMaxOpenConns(1)
	writeDB.SetMaxIdleConns(1)
	writeDB.SetConnMaxLifetime(30 * time.Minute)

	readDB.SetMaxOpenConns(4)
	readDB.SetMaxIdleConns(2)
	readDB.SetConnMaxLifetime(30 * time.Minute)

	for _, conn := range []*sql.DB{writeDB, readDB} {
		if err := applyPragmas(conn); err != nil {
			_ = readDB.Close()
			_ = writeDB.Close()
			return nil, err
		}
	}

	return &Provider{Write: writeDB, Read: readDB}, nil
}

// Close releases both database connections.
func (p *Provider) Close() error {
	if p == nil {
		return nil
	}
	if p.Read != nil {
		_ = p.Read.Close()
	}
	if p.Write != nil {
		_ = p.Write.Close()
	}
	return nil
}

// RunInTx executes fn inside a serializable transaction on the write connection.
// If fn returns an error the transaction is rolled back; otherwise it is committed.
func (p *Provider) RunInTx(fn func(tx *sql.Tx) error) error {
	tx, err := p.Write.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func applyPragmas(conn *sql.DB) error {
	stmts := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA temp_store = MEMORY;",
	}
	for _, stmt := range stmts {
		if _, err := conn.Exec(stmt); err != nil {
			return fmt.Errorf("pragma %q: %w", stmt, err)
		}
	}
	return nil
}

func ensureParentDir(dsn string) error {
	if dsn == "" || dsn == ":memory:" {
		return nil
	}
	parent := filepath.Dir(dsn)
	if parent == "." || parent == "" {
		return nil
	}
	return os.MkdirAll(parent, 0o755)
}
