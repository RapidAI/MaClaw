package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	sqlite3 "modernc.org/sqlite"
)

type Config struct {
	DSN                   string
	WAL                   bool
	BusyTimeoutMS         int
	MaxReadOpenConns      int
	MaxReadIdleConns      int
	MaxWriteOpenConns     int
	MaxWriteIdleConns     int
	BatchFlushMS          int
	BatchMaxSize          int
	BatchQueueSize        int
	CacheSizeKB           int
	MmapSizeBytes         int64
	CheckpointIntervalSec int
	// AutoVacuum selects the database-level auto_vacuum mode: "", "none",
	// "full", or "incremental". Empty leaves the persistent setting untouched
	// (backward compatible for callers that do not set it). "incremental" is
	// the recommended mode: DELETEs move pages to a freelist that the
	// background checkpointer releases with PRAGMA incremental_vacuum instead
	// of the file growing without bound between manual VACUUMs.
	AutoVacuum string
}

type Provider struct {
	Write    *sql.DB
	Read     *sql.DB
	batch    *writeBatcher
	stopCkpt chan struct{}
	doneCkpt chan struct{}
	// incrementalVacuum mirrors cfg.AutoVacuum == "incremental" so the
	// checkpointer knows whether PRAGMA incremental_vacuum is meaningful.
	incrementalVacuum bool
}

func NewProvider(cfg Config) (*Provider, error) {
	if err := ensureParentDir(cfg.DSN); err != nil {
		return nil, err
	}

	// Connection-scoped pragmas (foreign_keys, busy_timeout, cache) are applied
	// inside every Connect. database/sql runs a PRAGMA Exec on one pooled
	// connection only, so a later connection would otherwise keep the defaults.
	pragmas := connectionPragmas(cfg)
	writeDB := openSQLiteDB(cfg.DSN, pragmas, cfg.MaxWriteOpenConns, cfg.MaxWriteIdleConns)
	if err := applyDatabasePragmas(writeDB, cfg); err != nil {
		_ = writeDB.Close()
		return nil, err
	}
	readDB := openSQLiteDB(cfg.DSN, pragmas, cfg.MaxReadOpenConns, cfg.MaxReadIdleConns)

	p := &Provider{
		Write:             writeDB,
		Read:              readDB,
		batch:             newWriteBatcher(writeDB, cfg),
		incrementalVacuum: strings.EqualFold(strings.TrimSpace(cfg.AutoVacuum), "incremental"),
	}

	// Start background WAL checkpointer if WAL mode is enabled.
	if cfg.WAL {
		interval := time.Duration(cfg.CheckpointIntervalSec) * time.Second
		if interval <= 0 {
			interval = 60 * time.Second
		}
		p.startCheckpointer(interval)
	}

	return p, nil
}

func (p *Provider) Close() error {
	if p == nil {
		return nil
	}
	if p.stopCkpt != nil {
		select {
		case <-p.stopCkpt:
			// already closed
		default:
			close(p.stopCkpt)
			<-p.doneCkpt
		}
	}
	if p.batch != nil {
		p.batch.Close()
	}
	if p.Read != nil {
		_ = p.Read.Close()
	}
	if p.Write != nil {
		_ = p.Write.Close()
	}
	return nil
}

func (p *Provider) startCheckpointer(interval time.Duration) {
	p.stopCkpt = make(chan struct{})
	p.doneCkpt = make(chan struct{})
	go func() {
		defer close(p.doneCkpt)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-p.stopCkpt:
				return
			case <-ticker.C:
				// PASSIVE checkpoint: moves committed WAL pages to DB
				// without blocking concurrent readers or writers.
				// Execute on the Read pool to avoid contending with the
				// single Write connection used by the batcher.
				_, _ = p.Read.Exec("PRAGMA wal_checkpoint(PASSIVE);")
				if p.incrementalVacuum {
					// Release freed pages (from DELETE churn) back to the OS in
					// bounded batches. With auto_vacuum=INCREMENTAL this is cheap
					// periodic housekeeping: an instant no-op while the freelist
					// is empty, and it never takes an exclusive lock.
					//
					// The pragma emits one row per moved page, and the driver
					// only performs the work while the row set is being drained —
					// db.Exec/QueryRow leave it unfinished, so Query + full
					// iteration is required.
					if rows, err := p.Read.Query("PRAGMA incremental_vacuum(1024);"); err == nil {
						for rows.Next() {
						}
						_ = rows.Close()
					}
				}
			}
		}
	}()
}

// pragmaConnector runs connection-scoped pragmas on every new connection.
// database/sql's DB.Exec only reaches one connection in the pool.
type pragmaConnector struct {
	dsn     string
	drv     driver.Driver
	pragmas []string
}

func (c pragmaConnector) Connect(ctx context.Context) (driver.Conn, error) {
	raw, err := c.drv.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	if err := execConnPragmas(ctx, raw, c.pragmas); err != nil {
		_ = raw.Close()
		return nil, err
	}
	// Raw BEGIN on a *sql.Conn is invisible to database/sql. The pointer tracks
	// that transaction so a skipped ROLLBACK cannot be handed out again.
	return &txResetConn{Conn: raw}, nil
}

func (c pragmaConnector) Driver() driver.Driver { return c.drv }

func execConnPragmas(ctx context.Context, conn driver.Conn, pragmas []string) error {
	if len(pragmas) == 0 {
		return nil
	}
	if execer, ok := conn.(driver.ExecerContext); ok {
		for _, stmt := range pragmas {
			if _, err := execer.ExecContext(ctx, stmt, nil); err != nil {
				return fmt.Errorf("apply pragma %q: %w", stmt, err)
			}
		}
		return nil
	}
	queryer, ok := conn.(driver.QueryerContext)
	if !ok {
		return fmt.Errorf("sqlite connection cannot apply pragmas")
	}
	for _, stmt := range pragmas {
		rows, err := queryer.QueryContext(ctx, stmt, nil)
		if err != nil {
			return fmt.Errorf("apply pragma %q: %w", stmt, err)
		}
		_ = rows.Close()
	}
	return nil
}

// txResetConn records a SQLite transaction that database/sql did not finish.
//
// Raw BEGIN/COMMIT on a pooled *sql.Conn never becomes a driver.Tx, and
// modernc.org/sqlite ResetSession only rejects an interrupted connection.
// The write pool has one connection, so the next BeginTx then fails with
// "cannot start a transaction within a transaction". openTx is set when a
// transaction starts and cleared only when COMMIT or ROLLBACK succeeds, so
// a clean checkout does not pay for a ROLLBACK.
type txResetConn struct {
	driver.Conn
	openTx bool
}

var (
	_ driver.Conn               = (*txResetConn)(nil)
	_ driver.ConnBeginTx        = (*txResetConn)(nil)
	_ driver.ConnPrepareContext = (*txResetConn)(nil)
	_ driver.ExecerContext      = (*txResetConn)(nil)
	_ driver.QueryerContext     = (*txResetConn)(nil)
	_ driver.SessionResetter    = (*txResetConn)(nil)
	_ driver.Validator          = (*txResetConn)(nil)
	_ driver.Pinger             = (*txResetConn)(nil)
)

func (c *txResetConn) ResetSession(ctx context.Context) error {
	if rs, ok := c.Conn.(driver.SessionResetter); ok {
		if err := rs.ResetSession(ctx); err != nil {
			return err
		}
	}
	if !c.openTx {
		return nil
	}
	return c.rollbackAbandoned()
}

func (c *txResetConn) IsValid() bool {
	if v, ok := c.Conn.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

func (c *txResetConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *txResetConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.beginDriverTx(ctx, opts)
	if err == nil {
		c.openTx = true
		return &trackedTx{Tx: tx, conn: c}, nil
	}
	// openTx means the caller is inside a transaction this wrapper started.
	// Leave that work in place. Otherwise a failed BEGIN can still have
	// opened a transaction, or an older untracked one can still be active.
	// End it before the connection returns to the pool. Retry only when the
	// failure was the nested-transaction error the caller can survive.
	if c.openTx {
		return nil, err
	}
	if rbErr := c.rollbackAbandoned(); rbErr != nil {
		return nil, rbErr
	}
	if !sqliteErrNestedTx(err) {
		return nil, err
	}
	tx, err = c.beginDriverTx(ctx, opts)
	if err == nil {
		c.openTx = true
		return &trackedTx{Tx: tx, conn: c}, nil
	}
	// The retry can open a transaction and still fail. End it before the
	// connection is reused.
	return nil, c.releaseFailedBegin(err)
}

func (c *txResetConn) beginDriverTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if b, ok := c.Conn.(driver.ConnBeginTx); ok {
		return b.BeginTx(ctx, opts)
	}
	return c.Conn.Begin()
}

func (c *txResetConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if p, ok := c.Conn.(driver.ConnPrepareContext); ok {
		return p.PrepareContext(ctx, query)
	}
	return c.Conn.Prepare(query)
}

func (c *txResetConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	res, err := execer.ExecContext(ctx, query, args)
	// openTx means this BEGIN is nested inside a transaction the caller
	// still holds. Return that error and keep their rows. An untracked
	// failure can leave SQLite inside a transaction anyway: roll it back
	// before the connection is reused, and retry only the nested-transaction
	// case the caller can still complete.
	if err != nil && !c.openTx && sqliteTxKindOf(query) == sqliteTxBegin {
		if rbErr := c.rollbackAbandoned(); rbErr != nil {
			return nil, rbErr
		}
		if sqliteErrNestedTx(err) {
			res, err = execer.ExecContext(ctx, query, args)
			if err != nil {
				// A retried script can get past BEGIN and then fail. That
				// BEGIN has already opened a new transaction.
				return nil, c.releaseFailedBegin(err)
			}
		}
	}
	c.noteSQLTx(query, err)
	return res, err
}

func (c *txResetConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	rows, err := queryer.QueryContext(ctx, query, args)
	if err != nil && sqliteTxKindOf(query) == sqliteTxBegin {
		if rbErr := c.endUntrackedBegin(err); rbErr != nil {
			return nil, rbErr
		}
	}
	c.noteSQLTx(query, err)
	return rows, err
}

func (c *txResetConn) Ping(ctx context.Context) error {
	if p, ok := c.Conn.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return driver.ErrSkip
}

func (c *txResetConn) noteSQLTx(query string, err error) {
	if err != nil {
		return
	}
	switch sqliteTxKindOf(query) {
	case sqliteTxBegin:
		c.openTx = true
	case sqliteTxEnd:
		c.openTx = false
	}
}

// trackedTx clears openTx only when the driver actually finished the
// transaction. A failed ROLLBACK leaves SQLite inside it; ResetSession then
// tries again. modernc's Commit already attempts that rollback, and a
// leftover flag costs one extra ROLLBACK on the next checkout.
type trackedTx struct {
	driver.Tx
	conn *txResetConn
}

func (t *trackedTx) Commit() error {
	err := t.Tx.Commit()
	if err == nil {
		t.conn.openTx = false
	}
	return err
}

func (t *trackedTx) Rollback() error {
	err := t.Tx.Rollback()
	if err == nil {
		t.conn.openTx = false
	}
	return err
}

type sqliteTxKind int

const (
	sqliteTxNone sqliteTxKind = iota
	sqliteTxBegin
	sqliteTxEnd
)

func sqliteTxKindOf(query string) sqliteTxKind {
	if startsWithSQLKeyword(query, "begin") {
		return sqliteTxBegin
	}
	if startsWithSQLKeyword(query, "commit") || startsWithSQLKeyword(query, "rollback") || startsWithSQLKeyword(query, "end") {
		return sqliteTxEnd
	}
	return sqliteTxNone
}

func startsWithSQLKeyword(query, word string) bool {
	i := 0
	for i < len(query) {
		switch query[i] {
		case ' ', '\n', '\t', '\r', '\f', '\v':
			i++
			continue
		}
		break
	}
	if len(query)-i < len(word) || !strings.EqualFold(query[i:i+len(word)], word) {
		return false
	}
	end := i + len(word)
	return end == len(query) || !isSQLIdent(query[end])
}

func isSQLIdent(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// releaseFailedBegin rolls back a transaction left by a BEGIN attempt that
// returned an error. A nested-transaction error after that rollback means
// the connection still cannot be reused.
func (c *txResetConn) releaseFailedBegin(err error) error {
	if err == nil {
		return nil
	}
	if rbErr := c.rollbackAbandoned(); rbErr != nil {
		return rbErr
	}
	if sqliteErrNestedTx(err) {
		return driver.ErrBadConn
	}
	return err
}

// endUntrackedBegin rolls back after a BEGIN that returned an error when
// this wrapper does not have the transaction marked open. modernc can leave
// the connection inside a transaction in that case: a script's BEGIN runs,
// then a later statement fails, or a canceled context replaces a successful
// BEGIN with ctx.Err(). The caller's error is preserved.
func (c *txResetConn) endUntrackedBegin(err error) error {
	if err == nil || c.openTx {
		return nil
	}
	return c.rollbackAbandoned()
}

// rollbackAbandoned ends a leftover SQLite transaction and clears openTx.
// The connection stays in the pool. driver.ErrBadConn means the rollback
// itself failed and database/sql must discard the connection.
func (c *txResetConn) rollbackAbandoned() error {
	if err := rollbackLeftoverSQLiteTx(c.Conn); err != nil {
		return driver.ErrBadConn
	}
	c.openTx = false
	return nil
}

// rollbackLeftoverSQLiteTx ends a transaction that was opened outside
// database/sql. ROLLBACK outside a transaction is an error and leaves
// autocommit on; any other failure means the connection cannot be reused.
func rollbackLeftoverSQLiteTx(conn driver.Conn) error {
	execer, ok := conn.(driver.ExecerContext)
	if !ok {
		return nil
	}
	_, err := execer.ExecContext(context.Background(), "ROLLBACK", nil)
	if err == nil || sqliteErrNoTransaction(err) {
		return nil
	}
	return err
}

func sqliteErrNoTransaction(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no transaction is active")
}

func sqliteErrNestedTx(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "cannot start a transaction within a transaction")
}

// rollbackRawTx ends a BEGIN issued on a pooled *sql.Conn. The caller's
// context is often already canceled, and ExecContext then returns before
// SQLite sees the statement. This pool has one write connection, so a
// skipped rollback makes the next BeginTx fail.
func rollbackRawTx(conn interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}) {
	_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
}

func openSQLiteDB(dsn string, pragmas []string, maxOpen, maxIdle int) *sql.DB {
	db := sql.OpenDB(pragmaConnector{dsn: dsn, drv: &sqlite3.Driver{}, pragmas: pragmas})
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(30 * time.Minute)
	return db
}

func connectionPragmas(cfg Config) []string {
	stmts := []string{
		"PRAGMA foreign_keys = ON;",
		fmt.Sprintf("PRAGMA busy_timeout = %d;", cfg.BusyTimeoutMS),
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA temp_store = MEMORY;",
	}
	if cfg.CacheSizeKB > 0 {
		stmts = append(stmts, fmt.Sprintf("PRAGMA cache_size = -%d;", cfg.CacheSizeKB))
	}
	// wal_autocheckpoint is per connection. journal_mode is not: it is set once
	// on the writer in applyDatabasePragmas, after auto_vacuum.
	if cfg.WAL {
		stmts = append(stmts, "PRAGMA wal_autocheckpoint = 2000;")
	}
	return stmts
}

// applyDatabasePragmas sets the persistent database options on the writer.
//
// auto_vacuum is a persistent database-level setting. It takes effect
// immediately on an empty database; on an existing one it becomes active
// after the next VACUUM rebuild (sqlite.Vacuum / `maintenance vacuum`
// persists it).
//
// ORDER MATTERS: auto_vacuum must run before "PRAGMA journal_mode = WAL" —
// once the WAL mode change has touched the database header, SQLite treats
// the database as non-empty and silently rejects an auto_vacuum change
// (verified empirically against modernc.org/sqlite v1.46.1).
func applyDatabasePragmas(db *sql.DB, cfg Config) error {
	var stmts []string
	switch mode := strings.ToLower(strings.TrimSpace(cfg.AutoVacuum)); mode {
	case "incremental", "full", "none":
		stmts = append(stmts, fmt.Sprintf("PRAGMA auto_vacuum = %s;", mode))
	case "", "off":
		// Leave the persistent auto_vacuum setting untouched.
	}
	if cfg.WAL {
		stmts = append(stmts, "PRAGMA journal_mode = WAL;")
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("apply pragma %q: %w", stmt, err)
		}
	}
	// mmap_size may fail on some platforms/drivers (e.g. modernc/sqlite on Windows)
	// without affecting correctness — it's a best-effort optimization.
	if cfg.MmapSizeBytes > 0 {
		_, _ = db.Exec(fmt.Sprintf("PRAGMA mmap_size = %d;", cfg.MmapSizeBytes))
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

	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create sqlite data dir: %w", err)
	}
	return nil
}
