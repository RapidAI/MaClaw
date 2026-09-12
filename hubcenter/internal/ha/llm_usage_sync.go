package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

// LLMUsageBatch is the replicated unit for tenant usage records. Usage is
// recorded only on the node that owns the hub/tenant LLM binding, so without
// replication every other HubCenter node would show zero consumption on the
// model access page. Records are batched to keep the op log small: one op
// carries up to a few hundred records instead of one op per LLM request.
type LLMUsageBatch struct {
	BatchID      string                          `json:"batch_id"`
	OriginNodeID string                          `json:"origin_node_id"`
	Records      []*llmservice.TenantUsageRecord `json:"records"`
	CreatedAt    time.Time                       `json:"created_at"`
}

func (s *Service) AttachLLMUsage(repo llmservice.UsageBatchRepository) {
	if s == nil {
		return
	}
	s.llmUsage = repo
}

// AppendLLMUsageBatch replicates one batch of locally-recorded usage rows to
// every peer. Batches are append-only; peers apply them idempotently by
// batch ID, so redelivery never double-counts consumption. The error return
// lets the caller requeue the batch instead of losing it to a transient op
// log failure.
func (s *Service) AppendLLMUsageBatch(ctx context.Context, batch *LLMUsageBatch) error {
	if s == nil || batch == nil || batch.BatchID == "" || len(batch.Records) == 0 {
		return nil
	}
	if batch.OriginNodeID == "" {
		return fmt.Errorf("llm usage batch %s has no origin node id", batch.BatchID)
	}
	createdAt := batch.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	if err := s.AppendUpsert(ctx, EntityLLMUsageBatch, batch.BatchID, batch, createdAt); err != nil {
		log.Printf("[hubcenter][ha] append llm usage batch: %v", err)
		s.recordFailure(ctx, "ha_sync", "append_llm_usage_batch_failed", err.Error(), batch.BatchID, map[string]any{"records": len(batch.Records)})
		return err
	}
	return nil
}

func (s *Service) applyLLMUsageBatchOp(ctx context.Context, op *store.HASyncOp) error {
	if s.llmUsage == nil {
		return fmt.Errorf("%w: llm usage batches", ErrReplicaNotReady)
	}
	if op.OpType != OpUpsert {
		return fmt.Errorf("unsupported llm usage batch op: %s", op.OpType)
	}
	var batch LLMUsageBatch
	if err := json.Unmarshal([]byte(op.PayloadJSON), &batch); err != nil {
		return err
	}
	if batch.BatchID == "" {
		batch.BatchID = op.EntityID
	}
	if batch.BatchID != op.EntityID {
		return fmt.Errorf("llm usage batch identity mismatch")
	}
	// Rows applied with an empty origin would look locally-originated to this
	// node's own sync seed and get republished back to the cluster.
	if batch.OriginNodeID == "" {
		return fmt.Errorf("llm usage batch %s has no origin node id", op.EntityID)
	}
	_, err := s.llmUsage.InsertBatch(ctx, batch.BatchID, batch.OriginNodeID, batch.Records)
	return err
}
