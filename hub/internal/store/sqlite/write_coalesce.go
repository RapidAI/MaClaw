package sqlite

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"
)

// writeCoalescer absorbs high-frequency idempotent writes (heartbeats, status
// updates, preview deltas) into an in-memory buffer and flushes them to SQLite
// in bulk at a configurable interval. This reduces the number of physical write
// transactions from O(N_machines / heartbeat_interval) to O(1 per flush), which
// is the key mechanism for supporting 10K+ connected machines on a single SQLite
// file.
//
// Design:
//   - Callers submit "coalesce keys" (e.g. machineID) + latest value.
//   - Only the LAST value per key is persisted — intermediate heartbeats are
//     silently dropped (same semantics as the existing 5s dedup in device.Service).
//   - Flush executes all pending writes in a single transaction.
//   - On shutdown, all pending data is flushed synchronously.
//
// This is NOT a general write cache. It is specifically designed for writes
// where only the latest state matters (last_seen_at, status, preview_text).
// For append-only writes (audit logs, heartbeat_log), use the writeBatcher.

// CoalesceEntry holds the latest pending write for a given key.
type CoalesceEntry struct {
	Query string
	Args  []any
}

// WriteCoalescer batches last-writer-wins updates into periodic bulk flushes.
type WriteCoalescer struct {
	db            *sql.DB
	flushInterval time.Duration
	maxBatchSize  int
	flushTimeout  time.Duration

	mu      sync.Mutex
	pending map[string]CoalesceEntry // key → latest write

	stop chan struct{}
	done chan struct{}
}

// WriteCoalescerConfig configures the coalescer behavior.
type WriteCoalescerConfig struct {
	FlushIntervalMS int // how often to flush (default 5000ms)
	MaxBatchSize    int // max statements per flush transaction (default 512)
}

// NewWriteCoalescer creates and starts a write coalescer.
func NewWriteCoalescer(db *sql.DB, cfg WriteCoalescerConfig) *WriteCoalescer {
	interval := time.Duration(cfg.FlushIntervalMS) * time.Millisecond
	if interval <= 0 {
		interval = 5 * time.Second
	}
	maxBatch := cfg.MaxBatchSize
	if maxBatch <= 0 {
		maxBatch = 512
	}

	wc := &WriteCoalescer{
		db:            db,
		flushInterval: interval,
		maxBatchSize:  maxBatch,
		flushTimeout:  30 * time.Second,
		pending:       make(map[string]CoalesceEntry, 256),
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
	}
	go wc.run()
	return wc
}

// Set submits a coalesced write. If a write for the same key is already pending,
// it is silently replaced — only the latest value is flushed to disk.
// This is O(1) and never blocks on disk I/O.
//
// If called after Close(), the write is silently dropped. This is safe for the
// intended use cases (heartbeats, previews) because the data source is
// disconnecting and will not send further updates.
func (wc *WriteCoalescer) Set(key string, query string, args ...any) {
	select {
	case <-wc.stop:
		return // coalescer is shut down; silently drop
	default:
	}
	wc.mu.Lock()
	wc.pending[key] = CoalesceEntry{Query: query, Args: args}
	wc.mu.Unlock()
}

// Close stops the background flusher and synchronously flushes all remaining
// pending entries. Safe to call multiple times.
func (wc *WriteCoalescer) Close() {
	select {
	case <-wc.stop:
		return
	default:
		close(wc.stop)
		<-wc.done
	}
}

// PendingCount returns the number of coalesced entries waiting to be flushed.
// Useful for metrics/testing.
func (wc *WriteCoalescer) PendingCount() int {
	wc.mu.Lock()
	n := len(wc.pending)
	wc.mu.Unlock()
	return n
}

func (wc *WriteCoalescer) run() {
	defer close(wc.done)
	ticker := time.NewTicker(wc.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-wc.stop:
			wc.flush()
			return
		case <-ticker.C:
			wc.flush()
		}
	}
}

func (wc *WriteCoalescer) flush() {
	wc.mu.Lock()
	if len(wc.pending) == 0 {
		wc.mu.Unlock()
		return
	}
	// Swap out the pending map so new writes don't block on the flush.
	batch := wc.pending
	wc.pending = make(map[string]CoalesceEntry, 256)
	wc.mu.Unlock()

	// Execute all pending writes in a single transaction.
	// If the batch exceeds maxBatchSize, split into multiple transactions
	// to avoid holding the write lock for too long.
	entries := make([]CoalesceEntry, 0, len(batch))
	for _, entry := range batch {
		entries = append(entries, entry)
	}

	for i := 0; i < len(entries); i += wc.maxBatchSize {
		end := i + wc.maxBatchSize
		if end > len(entries) {
			end = len(entries)
		}
		wc.flushBatch(entries[i:end])
	}
}

func (wc *WriteCoalescer) flushBatch(entries []CoalesceEntry) {
	if len(entries) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), wc.flushTimeout)
	defer cancel()

	tx, err := wc.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("[write-coalescer] begin tx failed (%d entries): %v", len(entries), err)
		return
	}

	for _, entry := range entries {
		if _, err := tx.ExecContext(ctx, entry.Query, entry.Args...); err != nil {
			// Single statement failure: rollback and retry non-failing entries
			// in a second transaction.
			_ = tx.Rollback()
			wc.flushBatchSkipFailing(ctx, entries)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("[write-coalescer] commit failed (%d entries): %v", len(entries), err)
	}
}

// flushBatchSkipFailing retries entries in a single transaction, skipping any
// that fail individually. This preserves transactional efficiency while isolating
// the poison-pill entry that caused the batch to fail.
func (wc *WriteCoalescer) flushBatchSkipFailing(ctx context.Context, entries []CoalesceEntry) {
	tx, err := wc.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("[write-coalescer] retry begin tx failed: %v", err)
		return
	}

	executed := 0
	skipped := 0
	var firstErr error
	for _, entry := range entries {
		if ctx.Err() != nil {
			break // timeout — stop retrying
		}
		if _, err := tx.ExecContext(ctx, entry.Query, entry.Args...); err != nil {
			skipped++
			if firstErr == nil {
				firstErr = err
				log.Printf("[write-coalescer] skip failing entry (first): %v", err)
			}
			// Must rollback and start a new tx because the failed statement
			// may have left the tx in an error state (SQLite aborts tx on error).
			_ = tx.Rollback()
			tx, err = wc.db.BeginTx(ctx, nil)
			if err != nil {
				log.Printf("[write-coalescer] re-begin tx failed: %v", err)
				return
			}
			continue
		}
		executed++
	}

	if executed > 0 {
		if err := tx.Commit(); err != nil {
			log.Printf("[write-coalescer] retry commit failed (%d entries): %v", executed, err)
		}
	} else {
		_ = tx.Rollback()
	}
	if skipped > 1 {
		log.Printf("[write-coalescer] skipped %d failing entries total (first error: %v)", skipped, firstErr)
	}
}
