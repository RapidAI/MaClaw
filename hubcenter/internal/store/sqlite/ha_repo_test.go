package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

func TestHASyncOpRepoListAfterSeqWithoutLimitReturnsAllRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ha-repo-test.db")
	provider, err := NewProvider(Config{DSN: dbPath, WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	repo := &haSyncOpRepo{db: provider.Write, readDB: provider.Read, batch: newWriteBatcher(provider.Write, Config{BatchFlushMS: 1, BatchMaxSize: 1})}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		err := repo.Append(ctx, &store.HASyncOp{
			OpID:          "op-test-" + string(rune('a'+i)),
			SourceNodeID:  "hc-1",
			EntityType:    "news_article",
			EntityID:      "news-1",
			OpType:        "upsert",
			EntityVersion: int64(i + 1),
			OccurredAt:    time.Now().UTC().Add(time.Duration(i) * time.Second),
			PayloadJSON:   "{}",
			PayloadHash:   "hash",
		})
		if err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}
	items, err := repo.ListAfterSeq(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListAfterSeq() error = %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("len(ListAfterSeq(...,0)) = %d, want 3", len(items))
	}
}

func TestHASyncOpRepoHasEntityTypeOps(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ha-repo-test.db")
	provider, err := NewProvider(Config{DSN: dbPath, WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	repo := &haSyncOpRepo{db: provider.Write, readDB: provider.Read, batch: newWriteBatcher(provider.Write, Config{BatchFlushMS: 1, BatchMaxSize: 1})}
	ctx := context.Background()
	if err := repo.Append(ctx, &store.HASyncOp{
		OpID:          "op-test",
		SourceNodeID:  "hc-1",
		EntityType:    "skillmarket_snapshot",
		EntityID:      "skillmarket",
		OpType:        "upsert",
		EntityVersion: 1,
		OccurredAt:    time.Now().UTC(),
		PayloadJSON:   "{}",
		PayloadHash:   "hash",
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	ok, err := repo.HasEntityTypeOps(ctx, []string{"missing", "skillmarket_snapshot"})
	if err != nil {
		t.Fatalf("HasEntityTypeOps() error = %v", err)
	}
	if !ok {
		t.Fatal("HasEntityTypeOps() = false, want true")
	}
	ok, err = repo.HasEntityTypeOps(ctx, []string{"missing"})
	if err != nil {
		t.Fatalf("HasEntityTypeOps() error = %v", err)
	}
	if ok {
		t.Fatal("HasEntityTypeOps() = true, want false")
	}
}

func TestHASyncOpRepoAppendRemoteIfMissingRecordsOnce(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ha-repo-test.db")
	provider, err := NewProvider(Config{DSN: dbPath, WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	repo := &haSyncOpRepo{db: provider.Write, readDB: provider.Read, batch: newWriteBatcher(provider.Write, Config{BatchFlushMS: 1, BatchMaxSize: 1})}
	ctx := context.Background()
	op := &store.HASyncOp{
		Seq:           99,
		OpID:          "op-remote",
		SourceNodeID:  "hc-2",
		EntityType:    "news_article",
		EntityID:      "news-1",
		OpType:        "upsert",
		EntityVersion: 4,
		OccurredAt:    time.Now().UTC(),
		PayloadJSON:   "{}",
		PayloadHash:   "hash",
	}
	if err := repo.AppendRemoteIfMissing(ctx, op); err != nil {
		t.Fatalf("AppendRemoteIfMissing() error = %v", err)
	}
	if err := repo.AppendRemoteIfMissing(ctx, op); err != nil {
		t.Fatalf("AppendRemoteIfMissing() duplicate error = %v", err)
	}
	items, err := repo.ListAfterSeq(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListAfterSeq() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	if items[0].OpID != op.OpID || items[0].SourceNodeID != op.SourceNodeID || items[0].Seq == op.Seq {
		t.Fatalf("stored op = %+v", items[0])
	}
}

func TestHASyncOpRepoAppendLocalSkipsUnchangedRoutingPayload(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ha-dedupe-test.db")
	provider, err := NewProvider(Config{DSN: dbPath, WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	repo := &haSyncOpRepo{db: provider.Write, readDB: provider.Read}
	ctx := context.Background()
	first := `{"id":"link-1","hub_id":"hub-1","tenant_id":"","email":"user@example.com","is_default":true,"created_at":"2026-05-28T00:00:00Z","updated_at":"2026-05-28T00:00:00Z"}`
	second := `{"id":"link-1","hub_id":"hub-1","tenant_id":"","email":"user@example.com","is_default":true,"created_at":"2026-05-28T00:00:00Z","updated_at":"2026-05-28T00:05:00Z"}`

	version, err := repo.AppendLocalWithVersion(ctx, &store.HASyncOp{OpID: "op-link-1", SourceNodeID: "hc-1", EntityType: "hub_user_link", EntityID: "link-1", OpType: "upsert", OccurredAt: time.Now().UTC(), PayloadJSON: first, PayloadHash: "hash-1"})
	if err != nil || version != 1 {
		t.Fatalf("first AppendLocalWithVersion() version=%d err=%v, want version=1", version, err)
	}
	version, err = repo.AppendLocalWithVersion(ctx, &store.HASyncOp{OpID: "op-link-2", SourceNodeID: "hc-1", EntityType: "hub_user_link", EntityID: "link-1", OpType: "upsert", OccurredAt: time.Now().UTC().Add(time.Minute), PayloadJSON: second, PayloadHash: "hash-2"})
	if err != nil || version != 0 {
		t.Fatalf("duplicate AppendLocalWithVersion() version=%d err=%v, want skipped version=0", version, err)
	}

	items, err := repo.ListAfterSeq(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListAfterSeq() error = %v", err)
	}
	if len(items) != 1 || items[0].OpID != "op-link-1" {
		t.Fatalf("items = %+v, want one original op", items)
	}
	entityVersion, err := (&haEntityVersionRepo{db: provider.Write, readDB: provider.Read}).Get(ctx, "hub_user_link", "link-1")
	if err != nil {
		t.Fatalf("entity version Get() error = %v", err)
	}
	if entityVersion == nil || entityVersion.Version != 1 {
		t.Fatalf("entityVersion = %+v, want version 1", entityVersion)
	}
}

func TestHASyncOpRepoAppendLocalKeepsChangedRoutingPayload(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ha-dedupe-change-test.db")
	provider, err := NewProvider(Config{DSN: dbPath, WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	repo := &haSyncOpRepo{db: provider.Write, readDB: provider.Read}
	ctx := context.Background()
	first := `{"id":"route-1","hub_id":"hub-1","tenant_id":"","domain":"example.com","enabled":true,"priority":100,"created_at":"2026-05-28T00:00:00Z","updated_at":"2026-05-28T00:00:00Z"}`
	second := `{"id":"route-1","hub_id":"hub-1","tenant_id":"","domain":"example.com","enabled":true,"priority":50,"created_at":"2026-05-28T00:00:00Z","updated_at":"2026-05-28T00:05:00Z"}`

	if version, err := repo.AppendLocalWithVersion(ctx, &store.HASyncOp{OpID: "op-route-1", SourceNodeID: "hc-1", EntityType: "hub_domain_route", EntityID: "route-1", OpType: "upsert", OccurredAt: time.Now().UTC(), PayloadJSON: first, PayloadHash: "hash-1"}); err != nil || version != 1 {
		t.Fatalf("first AppendLocalWithVersion() version=%d err=%v, want version=1", version, err)
	}
	if version, err := repo.AppendLocalWithVersion(ctx, &store.HASyncOp{OpID: "op-route-2", SourceNodeID: "hc-1", EntityType: "hub_domain_route", EntityID: "route-1", OpType: "upsert", OccurredAt: time.Now().UTC().Add(time.Minute), PayloadJSON: second, PayloadHash: "hash-2"}); err != nil || version != 2 {
		t.Fatalf("changed AppendLocalWithVersion() version=%d err=%v, want version=2", version, err)
	}

	items, err := repo.ListAfterSeq(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListAfterSeq() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items len = %d, want 2", len(items))
	}
}

func TestHASyncOpRepoAppendLocalSkipsUnchangedLLMCardOrderPayload(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ha-card-order-dedupe-test.db")
	provider, err := NewProvider(Config{DSN: dbPath, WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	repo := &haSyncOpRepo{db: provider.Write, readDB: provider.Read}
	ctx := context.Background()
	first := `{"order_no":"HC-1","product_id":"quarter_100k","email":"owner@example.com","amount":5150,"status":"pending","created_at":"2026-06-19T00:00:00Z","updated_at":"2026-06-19T00:00:00Z","hub_id":"hub-1","tenant_id":"tenant_default","card_type_id":"quarter_100k","service_group_id":"redeem","credits":520000,"period":"quarter"}`
	second := `{"order_no":"HC-1","product_id":"quarter_100k","email":"owner@example.com","amount":5150,"status":"pending","created_at":"2026-06-19T00:00:00Z","updated_at":"2026-06-19T00:05:00Z","hub_id":"hub-1","tenant_id":"tenant_default","card_type_id":"quarter_100k","service_group_id":"redeem","credits":520000,"period":"quarter"}`

	if version, err := repo.AppendLocalWithVersion(ctx, &store.HASyncOp{OpID: "op-order-1", SourceNodeID: "hc-1", EntityType: "llm_card_order", EntityID: "HC-1", OpType: "upsert", OccurredAt: time.Now().UTC(), PayloadJSON: first, PayloadHash: "hash-1"}); err != nil || version != 1 {
		t.Fatalf("first AppendLocalWithVersion() version=%d err=%v, want version=1", version, err)
	}
	if version, err := repo.AppendLocalWithVersion(ctx, &store.HASyncOp{OpID: "op-order-2", SourceNodeID: "hc-1", EntityType: "llm_card_order", EntityID: "HC-1", OpType: "upsert", OccurredAt: time.Now().UTC().Add(time.Minute), PayloadJSON: second, PayloadHash: "hash-2"}); err != nil || version != 0 {
		t.Fatalf("unchanged AppendLocalWithVersion() version=%d err=%v, want skipped version=0", version, err)
	}

	items, err := repo.ListAfterSeq(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListAfterSeq() error = %v", err)
	}
	if len(items) != 1 || items[0].OpID != "op-order-1" {
		t.Fatalf("items = %+v, want one original order op", items)
	}
}

func TestHASyncOpRepoAppendLocalKeepsChangedLLMCardOrderPayload(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ha-card-order-change-test.db")
	provider, err := NewProvider(Config{DSN: dbPath, WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	repo := &haSyncOpRepo{db: provider.Write, readDB: provider.Read}
	ctx := context.Background()
	first := `{"order_no":"HC-1","product_id":"quarter_100k","email":"owner@example.com","amount":5150,"status":"pending","created_at":"2026-06-19T00:00:00Z","updated_at":"2026-06-19T00:00:00Z","hub_id":"hub-1","tenant_id":"tenant_default","card_type_id":"quarter_100k","service_group_id":"redeem","credits":520000,"period":"quarter"}`
	second := `{"order_no":"HC-1","product_id":"quarter_100k","email":"owner@example.com","amount":5150,"status":"activated","created_at":"2026-06-19T00:00:00Z","updated_at":"2026-06-19T00:05:00Z","hub_id":"hub-1","tenant_id":"tenant_default","card_type_id":"quarter_100k","service_group_id":"redeem","credits":520000,"period":"quarter"}`

	if version, err := repo.AppendLocalWithVersion(ctx, &store.HASyncOp{OpID: "op-order-1", SourceNodeID: "hc-1", EntityType: "llm_card_order", EntityID: "HC-1", OpType: "upsert", OccurredAt: time.Now().UTC(), PayloadJSON: first, PayloadHash: "hash-1"}); err != nil || version != 1 {
		t.Fatalf("first AppendLocalWithVersion() version=%d err=%v, want version=1", version, err)
	}
	if version, err := repo.AppendLocalWithVersion(ctx, &store.HASyncOp{OpID: "op-order-2", SourceNodeID: "hc-1", EntityType: "llm_card_order", EntityID: "HC-1", OpType: "upsert", OccurredAt: time.Now().UTC().Add(time.Minute), PayloadJSON: second, PayloadHash: "hash-2"}); err != nil || version != 2 {
		t.Fatalf("changed AppendLocalWithVersion() version=%d err=%v, want version=2", version, err)
	}

	items, err := repo.ListAfterSeq(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListAfterSeq() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items len = %d, want 2", len(items))
	}
}

func TestHASyncOpRepoAppendLocalDoesNotSkipAfterDelete(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ha-recreate-after-delete-test.db")
	provider, err := NewProvider(Config{DSN: dbPath, WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	repo := &haSyncOpRepo{db: provider.Write, readDB: provider.Read}
	ctx := context.Background()
	payload := `{"id":"news-1","title":"hello","content":"body","category":"notice","pinned":false,"created_at":"2026-05-29T00:00:00Z","updated_at":"2026-05-29T00:00:00Z"}`

	if version, err := repo.AppendLocalWithVersion(ctx, &store.HASyncOp{OpID: "op-news-upsert-1", SourceNodeID: "hc-1", EntityType: "news_article", EntityID: "news-1", OpType: "upsert", OccurredAt: time.Now().UTC(), PayloadJSON: payload, PayloadHash: "hash-news"}); err != nil || version != 1 {
		t.Fatalf("first upsert version=%d err=%v, want version=1", version, err)
	}
	if version, err := repo.AppendLocalWithVersion(ctx, &store.HASyncOp{OpID: "op-news-delete", SourceNodeID: "hc-1", EntityType: "news_article", EntityID: "news-1", OpType: "delete", OccurredAt: time.Now().UTC().Add(time.Minute), PayloadJSON: `{"id":"news-1"}`, PayloadHash: "hash-delete"}); err != nil || version != 2 {
		t.Fatalf("delete version=%d err=%v, want version=2", version, err)
	}
	if version, err := repo.AppendLocalWithVersion(ctx, &store.HASyncOp{OpID: "op-news-upsert-2", SourceNodeID: "hc-1", EntityType: "news_article", EntityID: "news-1", OpType: "upsert", OccurredAt: time.Now().UTC().Add(2 * time.Minute), PayloadJSON: payload, PayloadHash: "hash-news"}); err != nil || version != 3 {
		t.Fatalf("recreate upsert version=%d err=%v, want version=3", version, err)
	}

	items, err := repo.ListAfterSeq(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListAfterSeq() error = %v", err)
	}
	if len(items) != 3 || items[2].OpID != "op-news-upsert-2" {
		t.Fatalf("items = %+v, want recreate upsert after delete", items)
	}
}

func TestHASyncOpRepoAppendLocalSkipsUnchangedNewsUpsert(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ha-news-idempotent-test.db")
	provider, err := NewProvider(Config{DSN: dbPath, WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	repo := &haSyncOpRepo{db: provider.Write, readDB: provider.Read}
	ctx := context.Background()
	payload := `{"id":"news-1","title":"hello","content":"body","category":"notice","pinned":false,"created_at":"2026-05-29T00:00:00Z","updated_at":"2026-05-29T00:00:00Z"}`

	if version, err := repo.AppendLocalWithVersion(ctx, &store.HASyncOp{OpID: "op-news-1", SourceNodeID: "hc-1", EntityType: "news_article", EntityID: "news-1", OpType: "upsert", OccurredAt: time.Now().UTC(), PayloadJSON: payload, PayloadHash: "hash-news"}); err != nil || version != 1 {
		t.Fatalf("first upsert version=%d err=%v, want version=1", version, err)
	}
	if version, err := repo.AppendLocalWithVersion(ctx, &store.HASyncOp{OpID: "op-news-2", SourceNodeID: "hc-1", EntityType: "news_article", EntityID: "news-1", OpType: "upsert", OccurredAt: time.Now().UTC().Add(time.Minute), PayloadJSON: payload, PayloadHash: "hash-news"}); err != nil || version != 0 {
		t.Fatalf("unchanged upsert version=%d err=%v, want skipped version=0", version, err)
	}

	items, err := repo.ListAfterSeq(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListAfterSeq() error = %v", err)
	}
	if len(items) != 1 || items[0].OpID != "op-news-1" {
		t.Fatalf("items = %+v, want one unchanged news upsert", items)
	}
}

func TestHASyncOpRepoPruneHistoryKeepsLatestEntityOps(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ha-prune-test.db")
	provider, err := NewProvider(Config{DSN: dbPath, WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	repo := &haSyncOpRepo{db: provider.Write, readDB: provider.Read}
	ctx := context.Background()
	old := time.Now().UTC().Add(-48 * time.Hour)
	recent := time.Now().UTC()
	for i, item := range []struct {
		entityID string
		at       time.Time
	}{
		{entityID: "news-1", at: old},
		{entityID: "news-1", at: old.Add(time.Minute)},
		{entityID: "news-2", at: old},
		{entityID: "news-2", at: recent},
	} {
		opID := "op-prune-" + string(rune('a'+i))
		if err := repo.Append(ctx, &store.HASyncOp{OpID: opID, SourceNodeID: "hc-1", EntityType: "news_article", EntityID: item.entityID, OpType: "upsert", EntityVersion: int64(i + 1), OccurredAt: item.at, PayloadJSON: "{}", PayloadHash: "hash"}); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
		if err := repo.MarkApplied(ctx, &store.HAAppliedOp{OpID: opID, SourceNodeID: "hc-2", EntityType: "news_article", EntityID: item.entityID, AppliedAt: old}); err != nil {
			t.Fatalf("MarkApplied() error = %v", err)
		}
	}

	result, err := repo.PruneHistory(ctx, time.Now().UTC().Add(-24*time.Hour), 0, 2)
	if err != nil {
		t.Fatalf("PruneHistory() error = %v", err)
	}
	if result.DeletedOps != 2 || result.DeletedAppliedOps != 4 || result.RemainingOps != 2 {
		t.Fatalf("PruneHistory() result = %+v, want deleted_ops=2 deleted_applied_ops=4 remaining_ops=2", result)
	}
	items, err := repo.ListAfterSeq(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListAfterSeq() error = %v", err)
	}
	if len(items) != 2 || items[0].OpID != "op-prune-b" || items[1].OpID != "op-prune-d" {
		t.Fatalf("remaining ops = %+v", items)
	}
}

func TestHASyncOpRepoPruneHistoryCapsRetainedOps(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ha-prune-count-test.db")
	provider, err := NewProvider(Config{DSN: dbPath, WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	repo := &haSyncOpRepo{db: provider.Write, readDB: provider.Read}
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 6; i++ {
		opID := "op-count-" + string(rune('a'+i))
		if err := repo.Append(ctx, &store.HASyncOp{OpID: opID, SourceNodeID: "hc-1", EntityType: "news_article", EntityID: "news-1", OpType: "upsert", EntityVersion: int64(i + 1), OccurredAt: base.Add(time.Duration(i) * time.Minute), PayloadJSON: "{}", PayloadHash: "hash"}); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	result, err := repo.PruneHistory(ctx, time.Time{}, 3, 2)
	if err != nil {
		t.Fatalf("PruneHistory() error = %v", err)
	}
	if result.DeletedOps != 3 || result.RemainingOps != 3 || result.MaxSeq != 6 {
		t.Fatalf("PruneHistory() result = %+v, want deleted_ops=3 remaining_ops=3 max_seq=6", result)
	}
	items, err := repo.ListAfterSeq(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListAfterSeq() error = %v", err)
	}
	if len(items) != 3 || items[0].OpID != "op-count-d" || items[2].OpID != "op-count-f" {
		t.Fatalf("remaining ops = %+v", items)
	}
}

func TestHASyncOpRepoPruneHistoryCountsRemainingWhenOnlyAppliedRowsDeleted(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ha-prune-applied-test.db")
	provider, err := NewProvider(Config{DSN: dbPath, WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	repo := &haSyncOpRepo{db: provider.Write, readDB: provider.Read}
	ctx := context.Background()
	old := time.Now().UTC().Add(-48 * time.Hour)
	opID := "op-applied-only"
	if err := repo.Append(ctx, &store.HASyncOp{OpID: opID, SourceNodeID: "hc-1", EntityType: "news_article", EntityID: "news-1", OpType: "upsert", EntityVersion: 1, OccurredAt: old, PayloadJSON: "{}", PayloadHash: "hash"}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := repo.MarkApplied(ctx, &store.HAAppliedOp{OpID: opID, SourceNodeID: "hc-2", EntityType: "news_article", EntityID: "news-1", AppliedAt: old}); err != nil {
		t.Fatalf("MarkApplied() error = %v", err)
	}

	result, err := repo.PruneHistory(ctx, time.Now().UTC().Add(-24*time.Hour), 0, 2)
	if err != nil {
		t.Fatalf("PruneHistory() error = %v", err)
	}
	if result.DeletedOps != 0 || result.DeletedAppliedOps != 1 || result.RemainingOps != 1 {
		t.Fatalf("PruneHistory() result = %+v, want deleted_ops=0 deleted_applied_ops=1 remaining_ops=1", result)
	}
}
