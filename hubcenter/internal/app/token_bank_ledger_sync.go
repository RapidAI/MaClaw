package app

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

const (
	tokenBankLedgerSyncBufferCapacity = 4096
	tokenBankLedgerSyncBatchSize      = 200
	tokenBankLedgerSyncFlushInterval  = 15 * time.Second
	tokenBankLedgerSyncSeedPageSize   = 300
)

// tokenBankLedgerSyncBuffer batches locally-appended ledger rows into HA ops.
//
// The ledger is the balance, so an unreplicated row is a balance that differs
// from the peer's. This is the producer half of that link: the receiving half
// (InsertLedgerBatch) has been in place since the store layer landed, but
// without something calling it the ledger only ever replicated in one
// direction — which is to say, not at all.
//
// Batching follows the usage-recorder pattern for the same reason: settlement
// appends one row per proxied request, and one HA op per request would swamp
// the op log. A batch is also the unit of retry, so a transient op-log failure
// requeues a whole batch rather than losing rows one at a time.
type tokenBankLedgerSyncBuffer struct {
	nodeID   string
	appendFn func(ctx context.Context, batch *sqlite.TokenBankLedgerBatch) error

	mu      sync.Mutex
	pending []sqlite.TokenBankLedgerEntry
	seq     uint64
	dropped int64
	notify  chan struct{}
}

func newTokenBankLedgerSyncBuffer(nodeID string, appendFn func(ctx context.Context, batch *sqlite.TokenBankLedgerBatch) error) *tokenBankLedgerSyncBuffer {
	return &tokenBankLedgerSyncBuffer{
		nodeID:   strings.TrimSpace(nodeID),
		appendFn: appendFn,
		notify:   make(chan struct{}, 1),
	}
}

// Add enqueues one persisted ledger row for replication. It never blocks the
// settlement path: a full batch only signals the Run loop, and when the buffer
// is full the oldest row is dropped and counted in the periodic drop log.
//
// Dropping is safe for the balance — the row is already committed locally and
// the startup seed pass will republish it — but it must be visible, because a
// peer that never receives the row shows a different balance until the seed runs.
func (b *tokenBankLedgerSyncBuffer) Add(entry sqlite.TokenBankLedgerEntry) {
	if b == nil || strings.TrimSpace(entry.ID) == "" {
		return
	}
	b.mu.Lock()
	if len(b.pending) >= tokenBankLedgerSyncBufferCapacity {
		b.pending = b.pending[1:]
		atomic.AddInt64(&b.dropped, 1)
	}
	b.pending = append(b.pending, entry)
	ready := len(b.pending) >= tokenBankLedgerSyncBatchSize
	b.mu.Unlock()
	if ready {
		select {
		case b.notify <- struct{}{}:
		default:
		}
	}
}

// Flush publishes all buffered rows as one HA op. On failure the rows go back
// to the front of the buffer, because anything queued since is newer.
func (b *tokenBankLedgerSyncBuffer) Flush() {
	if b == nil || b.appendFn == nil {
		return
	}
	b.mu.Lock()
	if len(b.pending) == 0 {
		b.mu.Unlock()
		return
	}
	entries := b.pending
	b.pending = nil
	b.seq++
	seq := b.seq
	b.mu.Unlock()

	now := time.Now().UTC()
	err := b.appendFn(context.Background(), &sqlite.TokenBankLedgerBatch{
		BatchID:      fmt.Sprintf("tbk-live-%s-%d-%d", b.nodeID, now.UnixNano(), seq),
		OriginNodeID: b.nodeID,
		Entries:      entries,
		CreatedAt:    now,
	})
	if err == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	combined := make([]sqlite.TokenBankLedgerEntry, 0, len(entries)+len(b.pending))
	combined = append(combined, entries...)
	combined = append(combined, b.pending...)
	if overflow := len(combined) - tokenBankLedgerSyncBufferCapacity; overflow > 0 {
		combined = combined[overflow:]
		atomic.AddInt64(&b.dropped, int64(overflow))
	}
	b.pending = combined
}

// Run flushes on a fixed cadence, or immediately when a full batch is ready,
// and reports rows dropped to buffer overflow.
func (b *tokenBankLedgerSyncBuffer) Run() {
	if b == nil {
		return
	}
	ticker := time.NewTicker(tokenBankLedgerSyncFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if dropped := atomic.SwapInt64(&b.dropped, 0); dropped > 0 {
				log.Printf("[llm-init] token bank ledger sync buffer overflow: dropped %d entries (node=%s); the startup seed pass will republish them", dropped, b.nodeID)
			}
		case <-b.notify:
		}
		b.Flush()
	}
}

type tokenBankLedgerSeedSource interface {
	ListLedgerAfter(ctx context.Context, after time.Time, afterID string, limit int) ([]sqlite.TokenBankLedgerEntry, error)
}

// seedTokenBankLedgerHAOps republishes ledger rows that were written before this
// node started, or while a peer was unreachable.
//
// Batch IDs are deterministic per row set, so a repeated seed pass republishes
// the same batches and peers ignore them at both the batch and row level. The
// cutoff is the moment the live buffer starts, so a row is published exactly
// once: by the seed if it predates the cutoff, by the live buffer otherwise.
//
// Rows still sitting in the live buffer when the process dies become visible to
// the next startup's seed pass, so the cutoff does not lose data across restarts.
func seedTokenBankLedgerHAOps(ctx context.Context, haSvc *ha.Service, repo tokenBankLedgerSeedSource, nodeID string, cutoff time.Time) {
	if haSvc == nil || repo == nil {
		return
	}
	nodeID = strings.TrimSpace(nodeID)
	// The cursor is the (created_at, id) pair of the last row of the previous
	// page. created_at has second precision, and one second can outgrow a
	// page, so a timestamp-only cursor would skip rows and leave a permanent
	// hole in the peer's ledger.
	after := time.Time{}
	afterID := ""
	for {
		entries, err := repo.ListLedgerAfter(ctx, after, afterID, tokenBankLedgerSyncSeedPageSize)
		if err != nil {
			log.Printf("[llm-init] seed token bank ledger HA ops failed: %v", err)
			return
		}
		if len(entries) == 0 {
			return
		}
		// Advance the cursor past the whole page before filtering, or a page
		// made entirely of post-cutoff rows would be fetched again forever.
		lastSeen := entries[len(entries)-1]

		// A row created at or after the cutoff is owned by the live buffer.
		filtered := make([]sqlite.TokenBankLedgerEntry, 0, len(entries))
		for _, entry := range entries {
			if entry.CreatedAt.Before(cutoff) {
				filtered = append(filtered, entry)
			}
		}
		if len(filtered) > 0 {
			batchID := sqlite.TokenBankLedgerBatchID(nodeID, filtered)
			exists, err := haSvc.HasEntityVersion(ctx, ha.EntityTokenBankLedgerBatch, batchID)
			if err != nil {
				log.Printf("[llm-init] inspect token bank ledger HA entity version failed: batch=%s err=%v", batchID, err)
				return
			}
			if !exists {
				if err := haSvc.AppendTokenBankLedgerBatch(ctx, &sqlite.TokenBankLedgerBatch{
					BatchID:      batchID,
					OriginNodeID: nodeID,
					Entries:      filtered,
					CreatedAt:    filtered[len(filtered)-1].CreatedAt,
				}); err != nil {
					log.Printf("[llm-init] seed token bank ledger batch failed: batch=%s err=%v", batchID, err)
					return
				}
			}
		}
		// Everything from here on is at or after the cutoff, so seeding is done.
		if len(filtered) == 0 || len(entries) < tokenBankLedgerSyncSeedPageSize {
			return
		}
		after = lastSeen.CreatedAt
		afterID = lastSeen.ID
	}
}

const (
	tokenBankCacheReconcileInterval = 30 * time.Minute
	tokenBankCacheReconcilePageSize = 200
)

// runTokenBankCacheReconcile rebuilds cached balances that have stopped
// matching the ledger SUM.
//
// RebuildAccount has existed since the ledger was introduced, but nothing ever
// called it. That made the "the cache is a recoverable mirror" claim in the
// design doc unfalsifiable: a cache that drifted — a dropped replication batch,
// a bump that raced an HA replay — would have stayed wrong indefinitely and
// silently, and the only symptom would have been a user reporting a balance
// that disagreed with their own history.
//
// The sweep is cheap enough to run often because it does not rebuild
// everything: FindDriftedAccounts compares the cache and the ledger in one SQL
// scan and returns only the accounts that disagree, so a steady-state pass
// costs one query and rebuilds nothing.
//
// Repairs are logged individually rather than counted silently. Drift is not
// expected to be routine, so a rising log volume is a signal about replication,
// not about this job being busy.
func runTokenBankCacheReconcile(ctx context.Context, repo *sqlite.TokenBankRepo) {
	if repo == nil {
		return
	}
	reconcile := func() int {
		users, err := repo.FindDriftedAccounts(ctx, tokenBankCacheReconcilePageSize)
		if err != nil {
			log.Printf("[llm-init] token bank cache reconcile: find drifted accounts failed: %v", err)
			return 0
		}
		repaired := 0
		for _, userID := range users {
			if err := repo.RebuildAccount(ctx, userID); err != nil {
				log.Printf("[llm-init] token bank cache reconcile: rebuild failed: user=%s err=%v", userID, err)
				continue
			}
			repaired++
			log.Printf("[llm-init] token bank cache reconcile: rebuilt drifted account user=%s", userID)
		}
		return repaired
	}
	// One pass at startup, before the first request reads a balance: drift left
	// behind by an earlier crash or by a replication gap should not be visible
	// to a user who happens to load their card first.
	if n := reconcile(); n > 0 {
		log.Printf("[llm-init] token bank cache reconcile: startup pass repaired %d account(s)", n)
	}
	ticker := time.NewTicker(tokenBankCacheReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n := reconcile(); n > 0 {
				log.Printf("[llm-init] token bank cache reconcile: repaired %d account(s)", n)
			}
		}
	}
}
