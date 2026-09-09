package agentservice

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	_ "modernc.org/sqlite"
)

// SQLiteDistributedRateLimiter is a file-backed tenant token bucket. Processes
// that share the same database file observe one admission stream. Remote
// multi-host fairness still needs an external limiter in front of this port.
type SQLiteDistributedRateLimiter struct {
	mu     sync.Mutex
	db     *sql.DB
	rate   float64
	burst  float64
	closed bool
}

var _ DistributedRateLimiter = (*SQLiteDistributedRateLimiter)(nil)

func NewSQLiteDistributedRateLimiter(path string, rate float64, burst int) (*SQLiteDistributedRateLimiter, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return nil, fmt.Errorf("distributed rate limiter path is required")
	}
	if rate <= 0 {
		return nil, fmt.Errorf("distributed rate limiter rate must be positive")
	}
	if burst <= 0 {
		burst = 1
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	dsn := path + "?_pragma=busy_timeout%3d5000&_pragma=journal_mode%3dWAL&_pragma=synchronous%3dFULL"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS runtime_rate_limit (
		tenant_key TEXT PRIMARY KEY,
		tokens REAL NOT NULL,
		last_unix_nano INTEGER NOT NULL
	)`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLiteDistributedRateLimiter{db: db, rate: rate, burst: float64(burst)}, nil
}

func (l *SQLiteDistributedRateLimiter) Allow(ctx context.Context, tenantID string) (bool, time.Duration, error) {
	if l == nil {
		return true, 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, 0, err
	}
	key := agentruntime.TenantMetricsHash(strings.TrimSpace(tenantID))
	if key == "" {
		return true, 0, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.db == nil {
		return false, 0, fmt.Errorf("distributed rate limiter is closed")
	}
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return false, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var tokens float64
	var lastNano int64
	err = tx.QueryRowContext(ctx, `SELECT tokens, last_unix_nano FROM runtime_rate_limit WHERE tenant_key=?`, key).Scan(&tokens, &lastNano)
	now := time.Now().UTC()
	if err == sql.ErrNoRows {
		tokens = l.burst
		lastNano = 0
	} else if err != nil {
		return false, 0, err
	}
	var last time.Time
	if lastNano > 0 {
		last = time.Unix(0, lastNano).UTC()
	}
	next, allowed, retryAfter := agentruntime.ConsumeTokenBucket(tokens, l.burst, l.rate, last, now)
	if _, err := tx.ExecContext(ctx, `INSERT INTO runtime_rate_limit(tenant_key, tokens, last_unix_nano) VALUES(?,?,?)
		ON CONFLICT(tenant_key) DO UPDATE SET tokens=excluded.tokens, last_unix_nano=excluded.last_unix_nano`,
		key, next, now.UnixNano()); err != nil {
		return false, 0, err
	}
	if err := tx.Commit(); err != nil {
		return false, 0, err
	}
	return allowed, retryAfter, nil
}

func (l *SQLiteDistributedRateLimiter) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.db == nil {
		return nil
	}
	err := l.db.Close()
	l.db = nil
	return err
}
