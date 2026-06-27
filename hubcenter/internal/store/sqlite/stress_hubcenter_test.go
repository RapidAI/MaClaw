package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// TestStress_HubCenter_10K_Hub_Heartbeats simulates 10,000 Hub instances
// sending heartbeats to HubCenter every 30 seconds (staggered). This models
// the realistic HubCenter workload:
//
//   - Each heartbeat: 1 READ (GetByID) + 1 conditional WRITE (UpdateHeartbeat,
//     throttled to 1 per 5min per hub in production — we test WITHOUT throttle
//     to stress the worst case)
//   - Concurrent reads: admin dashboard queries, resolve requests
//   - HA sync ops: INSERT into ha_sync_ops for replication
//
// At 10K hubs with 30s heartbeat interval: ~333 heartbeats/sec (worst case
// without the 5-min service-level throttle).
func TestStress_HubCenter_10K_Hub_Heartbeats(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping HubCenter 10K stress test in short mode")
	}

	db := openHubCenterStressDB(t)
	setupHubCenterSchema(t, db)

	const numHubs = 10000
	const testDuration = 10 * time.Second

	t.Logf("Seeding %d hub instances...", numHubs)
	seedHubInstances(t, db, numHubs)
	t.Logf("Seed complete. Running %v HubCenter stress (10K hubs)...", testDuration)

	// Use batcher for writes (hubcenter doesn't have coalescer — heartbeat
	// writes are throttled at service level to 1 per 5min per hub).
	batch := newWriteBatcher(db, Config{
		BatchFlushMS:   100,
		BatchMaxSize:   128,
		BatchQueueSize: 8192,
	})

	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	var stats hubCenterStats
	var wg sync.WaitGroup

	// --- Workload 1: Hub heartbeats (READ GetByID + WRITE UpdateHeartbeat) ---
	// 10K hubs, each every 30s staggered. Without service throttle = worst case.
	wg.Add(numHubs)
	for i := 0; i < numHubs; i++ {
		go func(idx int) {
			defer wg.Done()
			hubID := fmt.Sprintf("hub_%d", idx)
			// Stagger within 30s window.
			time.Sleep(time.Duration(rand.Intn(10000)) * time.Millisecond)
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				// READ: GetByID (simulates hub lookup + secret verification).
				var status string
				_ = db.QueryRowContext(ctx,
					`SELECT status FROM hub_instances WHERE id = ?`, hubID).Scan(&status)
				stats.heartbeatReads.Add(1)

				// WRITE: UpdateHeartbeat.
				now := time.Now().Format(time.RFC3339)
				writeCtx, writeCancel := context.WithTimeout(context.Background(), 30*time.Second)
				err := batch.ExecContext(writeCtx,
					`UPDATE hub_instances SET status = 'online', last_seen_at = ?, updated_at = ? WHERE id = ?`,
					now, now, hubID,
				)
				writeCancel()
				if err != nil {
					stats.writeFailures.Add(1)
				} else {
					stats.heartbeatWrites.Add(1)
				}

				// Next heartbeat in 30s (most won't fire again in 10s test).
				time.Sleep(30 * time.Second)
			}
		}(i)
	}

	// --- Workload 2: Concurrent reads (resolve, admin dashboard) ---
	// Simulates user "resolve" requests: find hub by email domain.
	const numReaders = 1000
	wg.Add(numReaders)
	for i := 0; i < numReaders; i++ {
		go func(idx int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				// Resolve by domain (uses index).
				domain := fmt.Sprintf("hub%d.example.com", rand.Intn(numHubs))
				var hubID string
				_ = db.QueryRowContext(ctx,
					`SELECT hub_id FROM hub_domain_routes WHERE domain = ? AND enabled = 1 ORDER BY priority LIMIT 1`,
					domain).Scan(&hubID)
				stats.resolveReads.Add(1)

				// List hubs by status (admin dashboard).
				rows, err := db.QueryContext(ctx,
					`SELECT id, name, status FROM hub_instances WHERE status = 'online' LIMIT 20`)
				if err == nil {
					count := 0
					for rows.Next() {
						var id, name, st string
						_ = rows.Scan(&id, &name, &st)
						count++
					}
					rows.Close()
				}
				stats.listReads.Add(1)

				time.Sleep(2 * time.Millisecond)
			}
		}(i)
	}

	// --- Workload 3: HA sync ops (INSERT for replication) ---
	// Each heartbeat write generates 1 HA sync op in production.
	const numHAWriters = 100
	wg.Add(numHAWriters)
	for i := 0; i < numHAWriters; i++ {
		go func(idx int) {
			defer wg.Done()
			seq := 0
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				seq++
				writeCtx, writeCancel := context.WithTimeout(context.Background(), 30*time.Second)
				err := batch.ExecContext(writeCtx,
					`INSERT INTO ha_sync_ops (op_id, source_node_id, entity_type, entity_id, op_type, entity_version, occurred_at, payload_json, payload_hash)
					 VALUES (?, 'node-1', 'hub_instance', ?, 'heartbeat', ?, ?, '{}', '')`,
					fmt.Sprintf("op_%d_%d", idx, seq),
					fmt.Sprintf("hub_%d", rand.Intn(numHubs)),
					seq,
					time.Now().Format(time.RFC3339),
				)
				writeCancel()
				if err != nil {
					stats.writeFailures.Add(1)
				} else {
					stats.haWrites.Add(1)
				}
				time.Sleep(time.Duration(5+rand.Intn(10)) * time.Millisecond)
			}
		}(i)
	}

	// --- Workload 4: Hub user link lookups (resolve user → hub) ---
	const numLinkReaders = 500
	wg.Add(numLinkReaders)
	for i := 0; i < numLinkReaders; i++ {
		go func(idx int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				email := fmt.Sprintf("user%d@hub%d.example.com", rand.Intn(100), rand.Intn(numHubs))
				var hubID string
				_ = db.QueryRowContext(ctx,
					`SELECT hub_id FROM hub_user_links WHERE email = ? AND is_default = 1 LIMIT 1`,
					email).Scan(&hubID)
				stats.linkReads.Add(1)
				time.Sleep(3 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
	batch.Close()

	// --- Verification ---
	var onlineCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM hub_instances WHERE status = 'online'`).Scan(&onlineCount)
	var haOpCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM ha_sync_ops`).Scan(&haOpCount)

	totalReads := stats.heartbeatReads.Load() + stats.resolveReads.Load() + stats.listReads.Load() + stats.linkReads.Load()
	totalWrites := stats.heartbeatWrites.Load() + stats.haWrites.Load()

	t.Logf("")
	t.Logf("═══════════════════════════════════════════════════════════")
	t.Logf("  HUBCENTER 10K HUB HEARTBEAT STRESS TEST RESULTS")
	t.Logf("═══════════════════════════════════════════════════════════")
	t.Logf("  Duration:            %v", testDuration)
	t.Logf("  Hub instances:       %d", numHubs)
	t.Logf("  ─── Writes ───")
	t.Logf("  Heartbeat writes:    %d  (~%d/sec)", stats.heartbeatWrites.Load(),
		stats.heartbeatWrites.Load()/int64(testDuration.Seconds()))
	t.Logf("  HA sync ops:         %d  (~%d/sec)", stats.haWrites.Load(),
		stats.haWrites.Load()/int64(testDuration.Seconds()))
	t.Logf("  Write failures:      %d", stats.writeFailures.Load())
	t.Logf("  Total writes:        %d  (~%d/sec)", totalWrites, totalWrites/int64(testDuration.Seconds()))
	t.Logf("  ─── Reads ───")
	t.Logf("  Heartbeat reads:     %d  (GetByID)", stats.heartbeatReads.Load())
	t.Logf("  Resolve reads:       %d  (domain route)", stats.resolveReads.Load())
	t.Logf("  List reads:          %d  (admin dashboard)", stats.listReads.Load())
	t.Logf("  Link reads:          %d  (user→hub)", stats.linkReads.Load())
	t.Logf("  Total reads:         %d  (~%.0f/sec)", totalReads, float64(totalReads)/testDuration.Seconds())
	t.Logf("  ─── State ───")
	t.Logf("  Hubs online:         %d / %d", onlineCount, numHubs)
	t.Logf("  HA ops in DB:        %d", haOpCount)
	t.Logf("═══════════════════════════════════════════════════════════")

	if stats.writeFailures.Load() > 0 {
		t.Fatalf("FAILED: %d write failures", stats.writeFailures.Load())
	}
	if totalReads < 10000 {
		t.Fatalf("FAILED: only %d reads — reads may be starved", totalReads)
	}
	readRate := float64(totalReads) / testDuration.Seconds()
	t.Logf("PASS — HubCenter 10K hub heartbeats: zero failures, %.0f reads/sec, %d writes/sec",
		readRate, totalWrites/int64(testDuration.Seconds()))
}

type hubCenterStats struct {
	heartbeatReads  atomic.Int64
	heartbeatWrites atomic.Int64
	resolveReads    atomic.Int64
	listReads       atomic.Int64
	linkReads       atomic.Int64
	haWrites        atomic.Int64
	writeFailures   atomic.Int64
}

func openHubCenterStressDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := t.TempDir() + "/hubcenter_stress.db"
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA busy_timeout = 30000;",
		"PRAGMA temp_store = MEMORY;",
		"PRAGMA cache_size = -16384;",
		"PRAGMA wal_autocheckpoint = 2000;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			t.Fatalf("pragma %s: %v", p, err)
		}
	}
	return db
}

func setupHubCenterSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS hub_instances (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'offline',
			is_disabled INTEGER NOT NULL DEFAULT 0,
			last_seen_at TEXT,
			updated_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_hub_status ON hub_instances(status, is_disabled)`,
		`CREATE TABLE IF NOT EXISTS hub_domain_routes (
			id TEXT PRIMARY KEY,
			hub_id TEXT NOT NULL,
			domain TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			priority INTEGER NOT NULL DEFAULT 100,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_hub_domain_routes_lookup ON hub_domain_routes(domain, enabled, priority)`,
		`CREATE TABLE IF NOT EXISTS hub_user_links (
			id TEXT PRIMARY KEY,
			hub_id TEXT NOT NULL,
			email TEXT NOT NULL,
			is_default INTEGER NOT NULL DEFAULT 1,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_hub_user_links_email ON hub_user_links(email, is_default)`,
		`CREATE TABLE IF NOT EXISTS ha_sync_ops (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			op_id TEXT NOT NULL UNIQUE,
			source_node_id TEXT NOT NULL,
			entity_type TEXT NOT NULL,
			entity_id TEXT NOT NULL,
			op_type TEXT NOT NULL,
			entity_version INTEGER NOT NULL,
			occurred_at TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			payload_hash TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ha_sync_ops_entity ON ha_sync_ops(entity_type, entity_id)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup schema: %v", err)
		}
	}
}

func seedHubInstances(t *testing.T, db *sql.DB, count int) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}

	now := time.Now().Format(time.RFC3339)
	hubStmt, _ := tx.Prepare(`INSERT INTO hub_instances (id, name, status, last_seen_at, updated_at, created_at) VALUES (?, ?, 'offline', '', ?, ?)`)
	routeStmt, _ := tx.Prepare(`INSERT INTO hub_domain_routes (id, hub_id, domain, enabled, priority, updated_at) VALUES (?, ?, ?, 1, 100, ?)`)
	linkStmt, _ := tx.Prepare(`INSERT INTO hub_user_links (id, hub_id, email, is_default, updated_at) VALUES (?, ?, ?, 1, ?)`)

	for i := 0; i < count; i++ {
		hubID := fmt.Sprintf("hub_%d", i)
		hubStmt.Exec(hubID, fmt.Sprintf("Hub %d", i), now, now)
		routeStmt.Exec(fmt.Sprintf("route_%d", i), hubID, fmt.Sprintf("hub%d.example.com", i), now)
		linkStmt.Exec(fmt.Sprintf("link_%d", i), hubID, fmt.Sprintf("owner@hub%d.example.com", i), now)
	}

	hubStmt.Close()
	routeStmt.Close()
	linkStmt.Close()

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed: %v", err)
	}
}
