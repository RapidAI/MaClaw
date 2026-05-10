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
