package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

func TestHASyncOpRepoListAfterSeqWithoutLimitReturnsAllRows(t *testing.T) {
	provider, err := NewProvider(Config{DSN: "file::memory:?cache=shared", WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	repo := &haSyncOpRepo{db: provider.Write, readDB: provider.Read, batch: newWriteBatcher(provider.Write, 0, 1)}
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
