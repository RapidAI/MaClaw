package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

type capturedUsageBatches struct {
	mu      sync.Mutex
	batches []*ha.LLMUsageBatch
	err     error
}

func (c *capturedUsageBatches) append(ctx context.Context, batch *ha.LLMUsageBatch) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	c.batches = append(c.batches, batch)
	return nil
}

func (c *capturedUsageBatches) all() []*ha.LLMUsageBatch {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*ha.LLMUsageBatch, len(c.batches))
	copy(out, c.batches)
	return out
}

func TestLLMUsageSyncBufferSignalsAtBatchSize(t *testing.T) {
	captured := &capturedUsageBatches{}
	buf := newLLMUsageSyncBuffer("hc-1", captured.append)
	now := time.Now().UTC()
	for i := 0; i < llmUsageSyncBatchSize; i++ {
		buf.Add(&llmservice.TenantUsageRecord{HubID: "hub-1", TenantID: "tenant-a", Model: "m", ProviderID: "p", InputTokens: int64(i), CreatedAt: now})
	}
	// Add never flushes inline (that would put a SQLite op write on the
	// request path); it only signals the Run loop.
	if got := len(captured.all()); got != 0 {
		t.Fatalf("batches before Run-loop signal = %d, want 0", got)
	}
	select {
	case <-buf.notify:
	default:
		t.Fatal("full batch did not signal the Run loop")
	}
	buf.Flush()
	batches := captured.all()
	if len(batches) != 1 {
		t.Fatalf("batches after flush = %d, want 1", len(batches))
	}
	if len(batches[0].Records) != llmUsageSyncBatchSize {
		t.Fatalf("batch records = %d, want %d", len(batches[0].Records), llmUsageSyncBatchSize)
	}
	if batches[0].OriginNodeID != "hc-1" || batches[0].BatchID == "" {
		t.Fatalf("batch identity = %+v", batches[0])
	}

	buf.Add(&llmservice.TenantUsageRecord{HubID: "hub-1", TenantID: "tenant-a", Model: "m", ProviderID: "p", CreatedAt: now})
	select {
	case <-buf.notify:
		t.Fatal("partial batch signalled the Run loop")
	default:
	}
	buf.Flush()
	batches = captured.all()
	if len(batches) != 2 {
		t.Fatalf("batches after manual flush = %d, want 2", len(batches))
	}
	if len(batches[1].Records) != 1 {
		t.Fatalf("second batch records = %d, want 1", len(batches[1].Records))
	}
	if batches[0].BatchID == batches[1].BatchID {
		t.Fatalf("batch IDs must be unique, got %q twice", batches[0].BatchID)
	}
}

func TestLLMUsageSyncBufferDropsOldestWhenFull(t *testing.T) {
	// A nil appendFn leaves Flush() inert, so pending rows accumulate and the
	// capacity guard is exercised directly.
	buf := newLLMUsageSyncBuffer("hc-1", nil)
	now := time.Now().UTC()
	overflow := 5
	for i := 0; i < llmUsageSyncBufferCapacity+overflow; i++ {
		buf.Add(&llmservice.TenantUsageRecord{HubID: "hub-1", TenantID: "tenant-a", Model: "m", ProviderID: "p", CreatedAt: now})
	}
	buf.mu.Lock()
	pending := len(buf.pending)
	buf.mu.Unlock()
	if pending != llmUsageSyncBufferCapacity {
		t.Fatalf("pending = %d, want capacity %d", pending, llmUsageSyncBufferCapacity)
	}
	if dropped := atomic.LoadInt64(&buf.dropped); dropped != int64(overflow) {
		t.Fatalf("dropped = %d, want %d", dropped, overflow)
	}
}

func TestLLMUsageSyncBufferRequeuesOnAppendFailure(t *testing.T) {
	captured := &capturedUsageBatches{err: errors.New("op log busy")}
	buf := newLLMUsageSyncBuffer("hc-1", captured.append)
	now := time.Now().UTC()
	first := &llmservice.TenantUsageRecord{HubID: "hub-1", TenantID: "tenant-a", Model: "m", ProviderID: "p", SyncID: "u-1", CreatedAt: now}
	second := &llmservice.TenantUsageRecord{HubID: "hub-1", TenantID: "tenant-a", Model: "m", ProviderID: "p", SyncID: "u-2", CreatedAt: now}

	buf.Add(first)
	buf.Flush()
	if got := len(captured.all()); got != 0 {
		t.Fatalf("failed append published %d batches, want 0", got)
	}
	// The failed batch must stay buffered; records added afterwards queue
	// behind it so publication order is preserved.
	buf.Add(second)
	captured.err = nil
	buf.Flush()
	batches := captured.all()
	if len(batches) != 1 {
		t.Fatalf("batches after retry = %d, want 1", len(batches))
	}
	if len(batches[0].Records) != 2 || batches[0].Records[0].SyncID != "u-1" || batches[0].Records[1].SyncID != "u-2" {
		t.Fatalf("retried batch records = %+v, want [u-1 u-2] in order", batches[0].Records)
	}
}
