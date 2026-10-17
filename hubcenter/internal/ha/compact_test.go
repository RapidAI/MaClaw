package ha

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

func TestCoalesceOpsKeepsLatestAndAppendOnly(t *testing.T) {
	now := time.Now().UTC()
	ops := []*store.HASyncOp{
		{Seq: 1, EntityType: EntityNewsArticle, EntityID: "n-1", OpType: OpUpsert, EntityVersion: 1, OccurredAt: now},
		{Seq: 2, EntityType: EntityLLMUsageBatch, EntityID: "batch-1", OpType: OpUpsert, EntityVersion: 1, OccurredAt: now},
		{Seq: 3, EntityType: EntityNewsArticle, EntityID: "n-1", OpType: OpUpsert, EntityVersion: 2, OccurredAt: now.Add(time.Second)},
		{Seq: 4, EntityType: EntityLLMUsageBatch, EntityID: "batch-2", OpType: OpUpsert, EntityVersion: 1, OccurredAt: now},
		{Seq: 5, EntityType: EntityHubInstance, EntityID: "h-1", OpType: OpUpsert, EntityVersion: 1, OccurredAt: now},
	}
	out, dropped := CoalesceOps(ops)
	if dropped != 1 {
		t.Fatalf("dropped = %d, want 1", dropped)
	}
	if len(out) != 4 {
		t.Fatalf("len = %d, want 4", len(out))
	}
	if out[0].Seq != 2 || out[1].Seq != 3 || out[2].Seq != 4 || out[3].Seq != 5 {
		t.Fatalf("seqs = %d %d %d %d, want 2,3,4,5", out[0].Seq, out[1].Seq, out[2].Seq, out[3].Seq)
	}
	if out[len(out)-1].Seq != ops[len(ops)-1].Seq {
		t.Fatalf("cursor seq = %d, want %d", out[len(out)-1].Seq, ops[len(ops)-1].Seq)
	}
}

func TestCoalesceOpsLeavesSingleOp(t *testing.T) {
	ops := []*store.HASyncOp{{Seq: 7, EntityType: EntityNewsArticle, EntityID: "n"}}
	out, dropped := CoalesceOps(ops)
	if dropped != 0 || len(out) != 1 || out[0].Seq != 7 {
		t.Fatalf("out=%v dropped=%d", out, dropped)
	}
}

func TestCoalesceOpsBatchDropsDuplicates(t *testing.T) {
	const n = 20000
	const keys = 500
	payload := strings.Repeat("p", 128)
	ops := make([]*store.HASyncOp, n)
	for i := 0; i < n; i++ {
		ops[i] = &store.HASyncOp{
			Seq:         int64(i + 1),
			EntityType:  EntityNewsArticle,
			EntityID:    fmt.Sprintf("n-%d", i%keys),
			OpType:      OpUpsert,
			PayloadJSON: payload,
		}
	}
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	started := time.Now()
	out, dropped := CoalesceOps(ops)
	elapsed := time.Since(started)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if dropped != n-keys || len(out) != keys {
		t.Fatalf("dropped=%d len=%d, want dropped %d len %d", dropped, len(out), n-keys, keys)
	}
	if out[len(out)-1].Seq != int64(n) {
		t.Fatalf("highest seq = %d, want %d", out[len(out)-1].Seq, n)
	}
	alloc := after.TotalAlloc - before.TotalAlloc
	t.Logf("coalesce %d ops -> %d in %s (%.0f ops/s), heap alloc during call about %d bytes", n, len(out), elapsed, float64(n)/elapsed.Seconds(), alloc)
}
