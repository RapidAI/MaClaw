package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
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
}

type Provider struct {
	Write    *sql.DB
	Read     *sql.DB
	batch    *writeBatcher
	stopCkpt chan struct{}
	doneCkpt chan struct{}
}

func NewProvider(cfg Config) (*Provider, error) {
	if err := ensureParentDir(cfg.DSN); err != nil {
		return nil, err
	}

	writeDB, err := sql.Open("sqlite", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("open write db: %w", err)
	}

	readDB, err := sql.Open("sqlite", cfg.DSN)
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
			}
		}
	}()
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
