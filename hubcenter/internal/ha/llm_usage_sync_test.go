package ha

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type fakeLLMUsageBatchRepo struct {
	batches []*LLMUsageBatch
}

func (f *fakeLLMUsageBatchRepo) InsertBatch(ctx context.Context, batchID, sourceNodeID string, records []*llmservice.TenantUsageRecord) (bool, error) {
	f.batches = append(f.batches, &LLMUsageBatch{BatchID: batchID, OriginNodeID: sourceNodeID, Records: records})
	return true, nil
}

func TestApplyRemoteLLMUsageBatchUpsert(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	versionsRepo := &fakeHAEntityVersionRepo{items: make(map[string]*store.HAEntityVersion)}
	usageRepo := &fakeLLMUsageBatchRepo{}
	svc := &Service{
		nodeID:   "hc-1",
		ops:      opsRepo,
		versions: versionsRepo,
		llmUsage: usageRepo,
	}
	now := time.Now().UTC().Truncate(time.Second)
	batch := &LLMUsageBatch{
		BatchID:      "live-hc-2-1-1",
		OriginNodeID: "hc-2",
		CreatedAt:    now,
		Records: []*llmservice.TenantUsageRecord{
			{HubID: "hub-1", TenantID: "tenant-a", Model: "model-a", ProviderID: "provider-a", InputTokens: 10, OutputTokens: 20, CreatedAt: now},
			{HubID: "hub-1", TenantID: "tenant-a", Model: "model-b", ProviderID: "provider-b", InputTokens: 1, OutputTokens: 2, CreatedAt: now},
		},
	}
	payloadBytes, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("Marshal(batch) error = %v", err)
	}
	op := &store.HASyncOp{
		OpID:          "op-usage-batch-1",
		SourceNodeID:  "hc-2",
		EntityType:    EntityLLMUsageBatch,
		EntityID:      batch.BatchID,
		OpType:        OpUpsert,
		EntityVersion: 1,
		OccurredAt:    now,
		PayloadJSON:   string(payloadBytes),
		PayloadHash:   testPayloadHash(string(payloadBytes)),
	}
	if err := svc.ApplyRemoteOp(context.Background(), op); err != nil {
		t.Fatalf("ApplyRemoteOp() error = %v", err)
	}
	if len(usageRepo.batches) != 1 {
		t.Fatalf("applied batches = %d, want 1", len(usageRepo.batches))
	}
	got := usageRepo.batches[0]
	if got.BatchID != batch.BatchID || got.OriginNodeID != "hc-2" || len(got.Records) != 2 {
		t.Fatalf("applied batch = %+v", got)
	}
	if got.Records[0].InputTokens != 10 || got.Records[0].OutputTokens != 20 {
		t.Fatalf("applied record = %+v", got.Records[0])
	}
}

func TestApplyRemoteLLMUsageBatchRequiresAttachedRepository(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	versionsRepo := &fakeHAEntityVersionRepo{items: make(map[string]*store.HAEntityVersion)}
	svc := &Service{nodeID: "hc-1", ops: opsRepo, versions: versionsRepo}
	payload := `{"batch_id":"live-hc-2-9-1","origin_node_id":"hc-2","records":[{"hub_id":"hub-1","tenant_id":"tenant-a","model":"m","provider_id":"p","created_at":"2026-01-01T00:00:00Z"}]}`
	op := &store.HASyncOp{
		OpID:          "op-usage-batch-not-ready",
		SourceNodeID:  "hc-2",
		EntityType:    EntityLLMUsageBatch,
		EntityID:      "live-hc-2-9-1",
		OpType:        OpUpsert,
		EntityVersion: 1,
		OccurredAt:    time.Now().UTC(),
		PayloadJSON:   payload,
		PayloadHash:   testPayloadHash(payload),
	}

	err := svc.ApplyRemoteOp(context.Background(), op)
	if !errors.Is(err, ErrReplicaNotReady) {
		t.Fatalf("ApplyRemoteOp() error = %v, want ErrReplicaNotReady", err)
	}
	if applied, err := opsRepo.HasApplied(context.Background(), op.OpID); err != nil {
		t.Fatalf("HasApplied() error = %v", err)
	} else if applied {
		t.Fatal("operation without a repository was marked applied")
	}
}

func TestApplyRemoteLLMUsageBatchRejectsEmptyOriginNode(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	versionsRepo := &fakeHAEntityVersionRepo{items: make(map[string]*store.HAEntityVersion)}
	usageRepo := &fakeLLMUsageBatchRepo{}
	svc := &Service{nodeID: "hc-1", ops: opsRepo, versions: versionsRepo, llmUsage: usageRepo}
	// An empty origin would make the applied rows look locally-originated to
	// this node's own sync seed and echo them back into the cluster.
	payload := `{"batch_id":"live-hc-2-1-1","records":[{"hub_id":"hub-1","tenant_id":"tenant-a","model":"m","provider_id":"p","created_at":"2026-01-01T00:00:00Z"}]}`
	op := &store.HASyncOp{
		OpID:          "op-usage-batch-no-origin",
		SourceNodeID:  "hc-2",
		EntityType:    EntityLLMUsageBatch,
		EntityID:      "live-hc-2-1-1",
		OpType:        OpUpsert,
		EntityVersion: 1,
		OccurredAt:    time.Now().UTC(),
		PayloadJSON:   payload,
		PayloadHash:   testPayloadHash(payload),
	}
	if err := svc.ApplyRemoteOp(context.Background(), op); err == nil {
		t.Fatal("ApplyRemoteOp() succeeded, want empty origin error")
	}
	if len(usageRepo.batches) != 0 {
		t.Fatalf("batch without origin was applied: %+v", usageRepo.batches[0])
	}
}

func TestApplyRemoteLLMUsageBatchRejectsMismatchedBatchID(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	versionsRepo := &fakeHAEntityVersionRepo{items: make(map[string]*store.HAEntityVersion)}
	usageRepo := &fakeLLMUsageBatchRepo{}
	svc := &Service{nodeID: "hc-1", ops: opsRepo, versions: versionsRepo, llmUsage: usageRepo}
	payload := `{"batch_id":"live-hc-2-1-OTHER","origin_node_id":"hc-2","records":[{"hub_id":"hub-1","tenant_id":"tenant-a","model":"m","provider_id":"p","created_at":"2026-01-01T00:00:00Z"}]}`
	op := &store.HASyncOp{
		OpID:          "op-usage-batch-bad-id",
		SourceNodeID:  "hc-2",
		EntityType:    EntityLLMUsageBatch,
		EntityID:      "live-hc-2-1-EXPECTED",
		OpType:        OpUpsert,
		EntityVersion: 1,
		OccurredAt:    time.Now().UTC(),
		PayloadJSON:   payload,
		PayloadHash:   testPayloadHash(payload),
	}
	if err := svc.ApplyRemoteOp(context.Background(), op); err == nil {
		t.Fatal("ApplyRemoteOp() succeeded, want identity mismatch error")
	}
	if len(usageRepo.batches) != 0 {
		t.Fatalf("mismatched batch was applied: %+v", usageRepo.batches[0])
	}
}
