package sqlite

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
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
	CoalesceFlushMS       int // write-coalescer flush interval (default 5000ms)
	CoalesceMaxBatch      int // max statements per coalescer flush tx (default 512)
}

type Provider struct {
	Write    *sql.DB
	Read     *sql.DB
	batch    *writeBatcher
	coalesce *WriteCoalescer
	stopCkpt chan struct{}
	doneCkpt chan struct{}
}

func NewProvider(cfg Config) (*Provider, error) {
	if err := ensureParentDir(cfg.DSN); err != nil {
		return nil, err
	}

	// Pragmas passed to db.Exec apply to one pooled connection. database/sql
	// opens the rest later, and replaces every connection after
	// ConnMaxLifetime. The driver applies _pragma on each new connection.
	writeDB, err := sql.Open("sqlite", dsnWithConnectionPragmas(cfg.DSN, cfg, true))
	if err != nil {
		return nil, fmt.Errorf("open write db: %w", err)
	}

	readDB, err := sql.Open("sqlite", dsnWithConnectionPragmas(cfg.DSN, cfg, false))
	if err != nil {
		_ = writeDB.Close()
		return nil, fmt.Errorf("open read db: %w", err)
	}

	writeDB.SetMaxOpenConns(cfg.MaxWriteOpenConns)
	writeDB.SetMaxIdleConns(cfg.MaxWriteIdleConns)
	writeDB.SetConnMaxLifetime(30 * time.Minute)

	readDB.SetMaxOpenConns(cfg.MaxReadOpenConns)
	readDB.SetMaxIdleConns(cfg.MaxReadIdleConns)
	readDB.SetConnMaxLifetime(30 * time.Minute)

	if err := applyPragmas(writeDB, cfg, true); err != nil {
		_ = readDB.Close()
		_ = writeDB.Close()
		return nil, err
	}
	if err := applyPragmas(readDB, cfg, false); err != nil {
		_ = readDB.Close()
		_ = writeDB.Close()
		return nil, err
	}

	p := &Provider{
		Write: writeDB,
		Read:  readDB,
		batch: newWriteBatcher(writeDB, cfg),
		coalesce: NewWriteCoalescer(writeDB, WriteCoalescerConfig{
			FlushIntervalMS: cfg.CoalesceFlushMS,
			MaxBatchSize:    cfg.CoalesceMaxBatch,
		}),
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
	if p.coalesce != nil {
		p.coalesce.Close()
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
			}
		}
	}()
}

// dsnWithConnectionPragmas attaches the per-connection pragmas from cfg.
// journal_mode stays out of this list: it is persistent, and setting it on a
// read connection contends with an in-flight write. wal_autocheckpoint is
// writer-only for the same reason. cache_size and mmap_size stay out so each
// of the read connections does not reserve the writer's cache.
//
// An existing setting counts only when it is a _pragma value in the DSN query.
// The driver ignores the query on a non-file path and applies _pragma after
// open, including for file: URIs. A directory named synchronous must not
// suppress PRAGMA synchronous.
func dsnWithConnectionPragmas(dsn string, cfg Config, writer bool) string {
	if strings.TrimSpace(dsn) == "" {
		return dsn
	}
	existing := connectionPragmaSet(dsnQuery(dsn))
	params := make([]string, 0, 5)
	if cfg.BusyTimeoutMS > 0 && !existing["busy_timeout"] {
		params = append(params, fmt.Sprintf("_pragma=busy_timeout(%d)", cfg.BusyTimeoutMS))
	}
	if !existing["foreign_keys"] {
		params = append(params, "_pragma=foreign_keys(1)")
	}
	if !existing["synchronous"] {
		params = append(params, "_pragma=synchronous(NORMAL)")
	}
	if !existing["temp_store"] {
		params = append(params, "_pragma=temp_store(MEMORY)")
	}
	if writer && cfg.WAL && !existing["wal_autocheckpoint"] {
		params = append(params, "_pragma=wal_autocheckpoint(2000)")
	}
	if len(params) == 0 {
		return dsn
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + strings.Join(params, "&")
}

// dsnQuery is the part the sqlite driver parses as query parameters.
// A "?" at index 0 is not a query: modernc only splits when pos >= 1.
func dsnQuery(dsn string) string {
	pos := strings.IndexRune(dsn, '?')
	if pos < 1 {
		return ""
	}
	return dsn[pos+1:]
}

func connectionPragmaSet(query string) map[string]bool {
	set := make(map[string]bool)
	if query == "" {
		return set
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		lower := strings.ToLower(query)
		for _, name := range []string{"busy_timeout", "foreign_keys", "synchronous", "temp_store", "wal_autocheckpoint"} {
			if strings.Contains(lower, name) {
				set[name] = true
			}
		}
		return set
	}
	for _, raw := range values["_pragma"] {
		if name := pragmaSettingName(raw); name != "" {
			set[name] = true
		}
	}
	return set
}

func pragmaSettingName(raw string) string {
	name := strings.TrimSpace(strings.ToLower(raw))
	if i := strings.IndexAny(name, "(= \t"); i >= 0 {
		name = name[:i]
	}
	return name
}

func applyPragmas(db *sql.DB, cfg Config, isWriter bool) error {
	stmts := []string{
		"PRAGMA foreign_keys = ON;",
		fmt.Sprintf("PRAGMA busy_timeout = %d;", cfg.BusyTimeoutMS),
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA temp_store = MEMORY;",
	}
	// journal_mode and wal_autocheckpoint are database-level settings.
	// Only apply on the write connection to avoid redundant work and potential
	// lock contention with an in-flight write transaction.
	if cfg.WAL && isWriter {
		stmts = append(stmts, "PRAGMA journal_mode = WAL;")
		stmts = append(stmts, "PRAGMA wal_autocheckpoint = 2000;")
	}
	if cfg.CacheSizeKB > 0 {
		stmts = append(stmts, fmt.Sprintf("PRAGMA cache_size = -%d;", cfg.CacheSizeKB))
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("apply pragma %q: %w", stmt, err)
		}
	}

	// mmap_size may fail on some platforms/drivers (e.g. modernc/sqlite on Windows)
	// without affecting correctness — it's a best-effort optimization.
	// Apply separately so failure doesn't abort startup.
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
