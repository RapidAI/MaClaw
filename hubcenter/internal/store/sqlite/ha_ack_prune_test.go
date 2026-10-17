package sqlite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

func TestPruneHistoryAckDeletesAppliedHeadsAndKeepsBusinessRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ha-ack.db")
	provider, err := NewProvider(Config{DSN: dbPath, WAL: true, BusyTimeoutMS: 2000, MaxReadOpenConns: 1, MaxReadIdleConns: 1, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	ctx := context.Background()
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO system_settings (key, value_json, updated_at) VALUES ('keep-me', '{"ok":true}', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert setting: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO hub_instances (id, owner_email, name, base_url, visibility, enrollment_mode, hub_secret_hash, created_at, updated_at) VALUES ('hub-1', 'a@example.com', 'hub', 'http://127.0.0.1', 'private', 'closed', 'hash', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert hub: %v", err)
	}
	repo := &haSyncOpRepo{db: provider.Write, readDB: provider.Read}
	now := time.Now().UTC()
	for i := 0; i < 6; i++ {
		op := &store.HASyncOp{
			OpID: "op-ack-" + string(rune('a'+i)), SourceNodeID: "hc-1", EntityType: "news_article",
			EntityID: "news-" + string(rune('a'+i)), OpType: "upsert", EntityVersion: 1,
			OccurredAt: now.Add(-time.Duration(6-i) * time.Minute), PayloadJSON: `{}`, PayloadHash: "hash",
		}
		if err := repo.Append(ctx, op); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := repo.RecordPeerAck(ctx, "hc-2", 6, 2); err != nil {
		t.Fatalf("ack hc-2: %v", err)
	}
	if err := repo.RecordPeerAck(ctx, "hc-3", 6, 2); err != nil {
		t.Fatalf("ack hc-3: %v", err)
	}
	floor, err := repo.MinPeerAck(ctx, []string{"hc-2", "hc-3"}, time.Hour)
	if err != nil {
		t.Fatalf("MinPeerAck: %v", err)
	}
	if !floor.Known || floor.PinAck != 6 || floor.Checkpoint {
		t.Fatalf("floor = %+v, want known pin 6 and no checkpoint", floor)
	}
	started := time.Now()
	result, err := repo.PruneHistoryWithAck(ctx, time.Time{}, 2, 2, floor)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	elapsed := time.Since(started)
	if result.DeletedOps != 4 || result.RemainingOps != 2 || result.MaxSeq != 6 {
		t.Fatalf("result = %+v, want deleted 4 remaining 2 max 6", result)
	}
	items, err := repo.ListAfterSeq(ctx, 0, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 || items[0].EntityID != "news-e" || items[1].EntityID != "news-f" {
		t.Fatalf("remaining = %+v", items)
	}
	var settings, hubs int
	if err := provider.Write.QueryRowContext(ctx, `SELECT COUNT(1) FROM system_settings`).Scan(&settings); err != nil || settings != 1 {
		t.Fatalf("system_settings count=%d err=%v", settings, err)
	}
	if err := provider.Write.QueryRowContext(ctx, `SELECT COUNT(1) FROM hub_instances`).Scan(&hubs); err != nil || hubs != 1 {
		t.Fatalf("hub_instances count=%d err=%v", hubs, err)
	}
	heads, err := repo.ListSnapshotHeads(ctx, 0, 10)
	if err != nil {
		t.Fatalf("heads: %v", err)
	}
	if len(heads) != 4 {
		t.Fatalf("snapshot heads = %d, want the 4 last-write-wins entities removed from the op log", len(heads))
	}
	t.Logf("ack prune deleted %d ops in %s (%.0f ops/s)", result.DeletedOps, elapsed, float64(result.DeletedOps)/elapsed.Seconds())
}

func TestAckPruneKeepsSnapshotAndShrinksFile(t *testing.T) {
	const entities = 2000
	const versions = 3
	const retained = 200
	payload := `{"body":"` + strings.Repeat("x", 1024) + `"}`
	dbPath := filepath.Join(t.TempDir(), "ha-shrink.db")
	provider, err := NewProvider(Config{DSN: dbPath, WAL: true, BusyTimeoutMS: 5000, MaxReadOpenConns: 1, MaxReadIdleConns: 1, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	ctx := context.Background()
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO system_settings (key, value_json, updated_at) VALUES ('keep-me', '{"ok":true}', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert setting: %v", err)
	}
	tx, err := provider.Write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO ha_sync_ops (op_id, source_node_id, entity_type, entity_id, op_type, entity_version, occurred_at, payload_json, payload_hash) VALUES (?, 'hc-1', 'news_article', ?, 'upsert', ?, '2026-04-01T00:00:00Z', ?, 'hash')`)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	inserted := 0
	for version := 1; version <= versions; version++ {
		for entity := 0; entity < entities; entity++ {
			if _, err := stmt.ExecContext(ctx, fmt.Sprintf("op-%d-%d", version, entity), fmt.Sprintf("news-%d", entity), version, payload); err != nil {
				t.Fatalf("insert: %v", err)
			}
			inserted++
		}
	}
	if err := stmt.Close(); err != nil {
		t.Fatalf("close stmt: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	before := fileBytes(dbPath)
	repo := &haSyncOpRepo{db: provider.Write, readDB: provider.Read}
	if err := repo.RecordPeerAck(ctx, "hc-2", int64(inserted), 2); err != nil {
		t.Fatalf("ack: %v", err)
	}
	floor, err := repo.MinPeerAck(ctx, []string{"hc-2"}, time.Hour)
	if err != nil {
		t.Fatalf("floor: %v", err)
	}
	started := time.Now()
	result, err := repo.PruneHistoryWithAck(ctx, time.Time{}, retained, 500, floor)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	elapsed := time.Since(started)
	if result.RemainingOps != retained {
		t.Fatalf("remaining = %d, want %d (result=%+v)", result.RemainingOps, retained, result)
	}
	var heads int
	if err := provider.Write.QueryRowContext(ctx, `SELECT COUNT(1) FROM ha_entity_heads`).Scan(&heads); err != nil {
		t.Fatalf("count heads: %v", err)
	}
	if heads != entities-retained {
		t.Fatalf("heads = %d, want %d", heads, entities-retained)
	}
	var settings int
	if err := provider.Write.QueryRowContext(ctx, `SELECT COUNT(1) FROM system_settings WHERE key = 'keep-me'`).Scan(&settings); err != nil || settings != 1 {
		t.Fatalf("system_settings = %d err=%v", settings, err)
	}
	if err := Vacuum(ctx, provider.Write); err != nil {
		t.Fatalf("vacuum: %v", err)
	}
	after := fileBytes(dbPath)
	if after >= before {
		t.Fatalf("file size %d -> %d, want shrink after vacuum", before, after)
	}
	t.Logf("inserted %d ops (%d entities x %d versions, 1KiB payload)", inserted, entities, versions)
	t.Logf("ack prune deleted %d ops, checkpointed %d heads, remaining %d, in %s (%.0f deleted ops/s)", result.DeletedOps, result.CheckpointedHeads, result.RemainingOps, elapsed, float64(result.DeletedOps)/elapsed.Seconds())
	t.Logf("sqlite file %d bytes -> %d bytes after vacuum (%.0f%% of original)", before, after, 100*float64(after)/float64(before))
}

func fileBytes(path string) int64 {
	var total int64
	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, err := os.Stat(path + suffix)
		if err != nil {
			continue
		}
		total += info.Size()
	}
	return total
}

func TestPruneHistoryWithoutAckKeepsUniqueEntities(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ha-no-ack.db")
	provider, err := NewProvider(Config{DSN: dbPath, WAL: false, BusyTimeoutMS: 2000, MaxReadOpenConns: 1, MaxReadIdleConns: 1, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	ctx := context.Background()
	repo := &haSyncOpRepo{db: provider.Write, readDB: provider.Read}
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if err := repo.Append(ctx, &store.HASyncOp{
			OpID: "op-na-" + string(rune('a'+i)), SourceNodeID: "hc-1", EntityType: "news_article",
			EntityID: "news-" + string(rune('a'+i)), OpType: "upsert", EntityVersion: 1,
			OccurredAt: now, PayloadJSON: `{}`, PayloadHash: "hash",
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	floor, err := repo.MinPeerAck(ctx, []string{"hc-2"}, time.Hour)
	if err != nil {
		t.Fatalf("MinPeerAck: %v", err)
	}
	if floor.Known {
		t.Fatalf("floor = %+v, want unknown while a peer has not acked", floor)
	}
	result, err := repo.PruneHistoryWithAck(ctx, time.Time{}, 1, 10, floor)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if result.DeletedOps != 0 || result.RemainingOps != 3 || result.AckKnown {
		t.Fatalf("result = %+v, want all 3 unique ops kept", result)
	}
}

func TestPruneHistoryLaggingPeerUsesSnapshotHeads(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ha-lag.db")
	provider, err := NewProvider(Config{DSN: dbPath, WAL: false, BusyTimeoutMS: 2000, MaxReadOpenConns: 1, MaxReadIdleConns: 1, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	ctx := context.Background()
	repo := &haSyncOpRepo{db: provider.Write, readDB: provider.Read}
	now := time.Now().UTC()
	ops := []store.HASyncOp{
		{OpID: "op-1", EntityType: "news_article", EntityID: "news-1"},
		{OpID: "op-2", EntityType: "llm_usage_batch", EntityID: "batch-1"},
		{OpID: "op-3", EntityType: "news_article", EntityID: "news-2"},
		{OpID: "op-4", EntityType: "news_article", EntityID: "news-3"},
	}
	for i, op := range ops {
		op.SourceNodeID = "hc-1"
		op.OpType = "upsert"
		op.EntityVersion = 1
		op.OccurredAt = now
		op.PayloadJSON = `{"id":"` + op.EntityID + `"}`
		op.PayloadHash = "hash"
		if err := repo.Append(ctx, &op); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if err := repo.RecordPeerAck(ctx, "hc-healthy", 4, 2); err != nil {
		t.Fatalf("ack healthy: %v", err)
	}
	if err := repo.RecordPeerAck(ctx, "hc-lag", 1, 2); err != nil {
		t.Fatalf("ack lag: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `UPDATE ha_peer_acks SET acked_at = ? WHERE peer_node_id = 'hc-lag'`, now.Add(-48*time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatalf("age lag ack: %v", err)
	}
	floor, err := repo.MinPeerAck(ctx, []string{"hc-healthy", "hc-lag"}, time.Hour)
	if err != nil {
		t.Fatalf("MinPeerAck: %v", err)
	}
	if !floor.Known || !floor.Checkpoint || floor.PinAck != 4 || floor.StrictAck != 1 {
		t.Fatalf("floor = %+v, want pin 4 strict 1 checkpoint", floor)
	}
	result, err := repo.PruneHistoryWithAck(ctx, time.Time{}, 100, 10, floor)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	items, err := repo.ListAfterSeq(ctx, 0, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 || items[0].EntityType != "llm_usage_batch" {
		t.Fatalf("remaining ops = %+v, want only the unacked usage batch (deleted=%d)", items, result.DeletedOps)
	}
	heads, err := repo.ListSnapshotHeads(ctx, 1, 10)
	if err != nil {
		t.Fatalf("heads: %v", err)
	}
	if len(heads) != 2 || heads[0].EntityID != "news-2" || heads[1].EntityID != "news-3" {
		t.Fatalf("heads = %+v, want news-2 and news-3", heads)
	}
}

func TestPruneHistoryForceWindowCheckpointsUnackedHeads(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ha-force.db")
	provider, err := NewProvider(Config{DSN: dbPath, WAL: false, BusyTimeoutMS: 2000, MaxReadOpenConns: 1, MaxReadIdleConns: 1, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	ctx := context.Background()
	repo := &haSyncOpRepo{db: provider.Write, readDB: provider.Read}
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		entityType := "news_article"
		if i == 4 {
			entityType = "llm_usage_batch"
		}
		if err := repo.Append(ctx, &store.HASyncOp{
			OpID: "op-f-" + string(rune('a'+i)), SourceNodeID: "hc-1", EntityType: entityType,
			EntityID: "e-" + string(rune('a'+i)), OpType: "upsert", EntityVersion: 1,
			OccurredAt: now, PayloadJSON: `{}`, PayloadHash: "hash",
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	result, err := repo.pruneHistory(ctx, historyPruneOptions{
		MaxRetainedOps: 2,
		BatchSize:      2,
		ForceWindow:    true,
	})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if result.RemainingOps != 2 {
		t.Fatalf("remaining = %d, want 2 (newest last-write-wins op plus the append-only row)", result.RemainingOps)
	}
	heads, err := repo.ListSnapshotHeads(ctx, 0, 10)
	if err != nil {
		t.Fatalf("heads: %v", err)
	}
	if len(heads) != 3 {
		t.Fatalf("heads = %d, want the 3 last-write-wins ops that fell outside the window", len(heads))
	}
	var usageLeft int
	if err := provider.Write.QueryRowContext(ctx, `SELECT COUNT(1) FROM ha_sync_ops WHERE entity_type = 'llm_usage_batch'`).Scan(&usageLeft); err != nil || usageLeft != 1 {
		t.Fatalf("usage rows = %d err=%v", usageLeft, err)
	}
}

func TestRecordPeerAckDoesNotMoveBackward(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ha-ack-mono.db")
	provider, err := NewProvider(Config{DSN: dbPath, WAL: false, BusyTimeoutMS: 2000, MaxReadOpenConns: 1, MaxReadIdleConns: 1, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	ctx := context.Background()
	repo := &haSyncOpRepo{db: provider.Write, readDB: provider.Read}
	if err := repo.RecordPeerAck(ctx, "hc-2", 50, 2); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if err := repo.RecordPeerAck(ctx, "hc-2", 10, 2); err != nil {
		t.Fatalf("ack back: %v", err)
	}
	floor, err := repo.MinPeerAck(ctx, []string{"hc-2"}, time.Hour)
	if err != nil {
		t.Fatalf("MinPeerAck: %v", err)
	}
	if floor.PinAck != 50 {
		t.Fatalf("pin = %d, want 50", floor.PinAck)
	}
}
