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
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

const (
	llmUsageSyncBufferCapacity = 4096
	llmUsageSyncBatchSize      = 200
	llmUsageSyncFlushInterval  = 15 * time.Second
	llmUsageSyncSeedPageSize   = 300
)

// llmUsageSyncBuffer batches locally-recorded usage rows into HA ops so the
// op log carries one op per batch instead of one op per LLM request.
type llmUsageSyncBuffer struct {
	nodeID   string
	appendFn func(ctx context.Context, batch *ha.LLMUsageBatch) error

	mu      sync.Mutex
	pending []*llmservice.TenantUsageRecord
	seq     uint64
	dropped int64
	notify  chan struct{}
}

func newLLMUsageSyncBuffer(nodeID string, appendFn func(ctx context.Context, batch *ha.LLMUsageBatch) error) *llmUsageSyncBuffer {
	return &llmUsageSyncBuffer{nodeID: strings.TrimSpace(nodeID), appendFn: appendFn, notify: make(chan struct{}, 1)}
}

// Add enqueues one persisted usage record for replication. It never blocks
// the request path: a full batch only signals the Run loop, and when the
// buffer is full the oldest record is dropped and accounted for in the
// periodic drop log.
func (b *llmUsageSyncBuffer) Add(record *llmservice.TenantUsageRecord) {
	if b == nil || record == nil {
		return
	}
	b.mu.Lock()
	if len(b.pending) >= llmUsageSyncBufferCapacity {
		b.pending = b.pending[1:]
		atomic.AddInt64(&b.dropped, 1)
	}
	b.pending = append(b.pending, record)
	ready := len(b.pending) >= llmUsageSyncBatchSize
	b.mu.Unlock()
	if ready {
		select {
		case b.notify <- struct{}{}:
		default:
		}
	}
}

// Flush publishes all buffered records as one HA usage-batch op. When the
// append fails the records go back to the front of the buffer (they are older
// than anything queued since) so a transient op log failure does not lose a
// whole batch; the capacity guard still bounds the backlog.
func (b *llmUsageSyncBuffer) Flush() {
	if b == nil || b.appendFn == nil {
		return
	}
	b.mu.Lock()
	if len(b.pending) == 0 {
		b.mu.Unlock()
		return
	}
	records := b.pending
	b.pending = nil
	b.seq++
	seq := b.seq
	b.mu.Unlock()
	now := time.Now().UTC()
	err := b.appendFn(context.Background(), &ha.LLMUsageBatch{
		BatchID:      fmt.Sprintf("live-%s-%d-%d", b.nodeID, now.UnixNano(), seq),
		OriginNodeID: b.nodeID,
		Records:      records,
		CreatedAt:    now,
	})
	if err == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	combined := make([]*llmservice.TenantUsageRecord, 0, len(records)+len(b.pending))
	combined = append(combined, records...)
	combined = append(combined, b.pending...)
	if overflow := len(combined) - llmUsageSyncBufferCapacity; overflow > 0 {
		combined = combined[overflow:]
		atomic.AddInt64(&b.dropped, int64(overflow))
	}
	b.pending = combined
}

// Run flushes on a fixed cadence (or immediately when a full batch is ready)
// until the process exits and reports any records dropped because the buffer
// overflowed.
func (b *llmUsageSyncBuffer) Run() {
	if b == nil {
		return
	}
	ticker := time.NewTicker(llmUsageSyncFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if dropped := atomic.SwapInt64(&b.dropped, 0); dropped > 0 {
				log.Printf("[llm-init] llm usage sync buffer overflow: dropped %d records (node=%s)", dropped, b.nodeID)
			}
		case <-b.notify:
		}
		b.Flush()
	}
}

// localOriginUsageRepo marks locally-recorded usage rows with this node's ID
// so the HA sync seed pass never echoes replicated rows back to the cluster.
type localOriginUsageRepo struct {
	llmservice.UsageRepository
	local  usageLocalInserter
	nodeID string
}

type usageLocalInserter interface {
	InsertLocal(ctx context.Context, record *llmservice.TenantUsageRecord, nodeID string) error
}

func (r *localOriginUsageRepo) Insert(ctx context.Context, record *llmservice.TenantUsageRecord) error {
	return r.local.InsertLocal(ctx, record, r.nodeID)
}

type llmUsageSyncSource interface {
	MaxUsageID(ctx context.Context) (int64, error)
	ListLocalForSync(ctx context.Context, nodeID string, afterID, maxID int64, limit int) ([]*llmservice.TenantUsageRecord, error)
}

// seedLLMUsageHAOps backfills usage rows recorded before usage replication
// existed (or while a peer was unreachable). Batch IDs are deterministic per
// ID range, so a restarted seed pass skips batches it already published and
// peers deduplicate on the batch ledger regardless.
//
// The scan is bounded by the max row ID captured before the live replicator
// started: rows above the cutoff flow through the live buffer, and seeding
// them here as well would publish them under a second batch ID that the
// peers' batch ledger cannot deduplicate. Rows still in the live buffer when
// the process dies become visible to the next startup's seed pass, so the
// cutoff does not lose data across restarts.
func seedLLMUsageHAOps(ctx context.Context, haSvc *ha.Service, repo llmUsageSyncSource, nodeID string, maxID int64) {
	if haSvc == nil || repo == nil {
		return
	}
	nodeID = strings.TrimSpace(nodeID)
	var afterID int64
	seeded := 0
	for {
		records, err := repo.ListLocalForSync(ctx, nodeID, afterID, maxID, llmUsageSyncSeedPageSize)
		if err != nil {
			log.Printf("[llm-init] seed llm usage HA ops failed: %v", err)
			break
		}
		if len(records) == 0 {
			break
		}
		first, last := records[0], records[len(records)-1]
		afterID = last.ID
		// Rows predating sync IDs get a deterministic one derived from the
		// primary key, so a seed pass repeated after a restart republishes
		// them under the same ID and peers ignore the duplicates.
		for _, record := range records {
			if record != nil && strings.TrimSpace(record.SyncID) == "" {
				record.SyncID = fmt.Sprintf("legacy-%s-%d", nodeID, record.ID)
			}
		}
		batchID := fmt.Sprintf("seed-%s-%d-%d", nodeID, first.ID, last.ID)
		exists, err := haSvc.HasEntityVersion(ctx, ha.EntityLLMUsageBatch, batchID)
		if err != nil {
			log.Printf("[llm-init] inspect llm usage HA entity version failed: batch=%s err=%v", batchID, err)
			continue
		}
		if !exists {
			haSvc.AppendLLMUsageBatch(ctx, &ha.LLMUsageBatch{
				BatchID:      batchID,
				OriginNodeID: nodeID,
				Records:      records,
				CreatedAt:    last.CreatedAt,
			})
			seeded++
		}
		if len(records) < llmUsageSyncSeedPageSize {
			break
		}
	}
	if seeded > 0 {
		log.Printf("[llm-init] seeded llm usage HA ops: batches=%d", seeded)
	}
}

// seedLLMRegistryHAOp republishes the model access registry when the cluster
// has never seen its op (e.g. the original append failed after the local
// write succeeded). AppendSystemSetting skips unchanged payloads, so this is
// a no-op on a healthy cluster.
//
// A node that has never applied any system_setting op is still joining the
// cluster and will pull the registry from its peers; publishing its local
// (possibly default-seeded) registry then could regress the cluster value, so
// the heal is skipped in that state.
func seedLLMRegistryHAOp(ctx context.Context, haSvc *ha.Service, system store.SystemSettingsRepository) {
	if haSvc == nil || system == nil {
		return
	}
	seenSettings, err := haSvc.HasEntityTypeOps(ctx, ha.EntitySystemSetting)
	if err != nil {
		log.Printf("[llm-init] inspect system setting HA ops failed: %v", err)
		return
	}
	if !seenSettings {
		return
	}
	exists, err := haSvc.HasEntityVersion(ctx, ha.EntitySystemSetting, llmservice.RegistrySettingKey)
	if err != nil {
		log.Printf("[llm-init] inspect llm registry HA entity version failed: %v", err)
		return
	}
	if exists {
		return
	}
	raw, err := system.Get(ctx, llmservice.RegistrySettingKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return
	}
	haSvc.AppendSystemSetting(ctx, llmservice.RegistrySettingKey, raw)
	log.Printf("[llm-init] re-published llm service registry HA op")
}
