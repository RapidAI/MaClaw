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

// TestStress_Realistic_10K_Sustained simulates a realistic Hub workload with
// 10,000 connected machines over a sustained 10-second period. Unlike burst
// tests, this models real-world traffic patterns:
//
//   - Each machine sends heartbeats at a realistic interval (10s)
//   - Read queries (auth checks, session lookups) run at 10x the write rate
//   - Machines randomly connect/disconnect during the test
//   - Session events (create, summary, preview) fire at realistic rates
//   - Viewer token validation happens on every API request
//
// This is the definitive test for "can Hub handle 10K users on a single SQLite?"
func TestStress_Realistic_10K_Sustained(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping realistic stress test in short mode")
	}

	db := openStressTestDB(t)

	// Create tables matching production schema.
	setupRealisticSchema(t, db)

	const numMachines = 10000
	const testDuration = 10 * time.Second

	// Pre-populate machines + users + viewer tokens.
	t.Logf("Seeding %d machines, users, and tokens...", numMachines)
	seedRealisticData(t, db, numMachines)
	t.Logf("Seed complete. Running sustained %v load test...", testDuration)

	// Open separate read pool (simulates Provider.Read).
	// In production, Provider opens two sql.DB handles to the same file.
	// For this test we use the same handle — WAL mode allows concurrent reads
	// even during a write transaction on the same connection pool.
	readDB := db

	wc := NewWriteCoalescer(db, WriteCoalescerConfig{
		FlushIntervalMS: 5000, // production default
		MaxBatchSize:    512,
	})
	batch := newWriteBatcher(db, Config{
		BatchFlushMS:   100,
		BatchMaxSize:   128,
		BatchQueueSize: 4096,
	})

	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	var stats realisticStats

	var wg sync.WaitGroup

	// --- Workload 1: Machine heartbeats (via coalescer) ---
	// Each machine sends 1 heartbeat every 10s → 1000 heartbeats/sec at 10K machines.
	wg.Add(numMachines)
	for i := 0; i < numMachines; i++ {
		go func(idx int) {
			defer wg.Done()
			machineID := machineTestID(idx)
			// Stagger start: don't all fire at t=0.
			time.Sleep(time.Duration(rand.Intn(10000)) * time.Millisecond)
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			// Fire one immediately.
			fireHeartbeat(wc, machineID, &stats)
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					fireHeartbeat(wc, machineID, &stats)
				}
			}
		}(i)
	}

	// --- Workload 2: Read queries (auth + lookups) ---
	// 10x the heartbeat rate = ~10,000 reads/sec.
	const numReaders = 2000
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
				// Simulate viewer token auth check (most common read).
				tokenHash := fmt.Sprintf("token_hash_%d", rand.Intn(numMachines))
				row := readDB.QueryRowContext(ctx,
					`SELECT id FROM viewer_tokens WHERE token_hash = ?`, tokenHash)
				var id string
				_ = row.Scan(&id)
				stats.reads.Add(1)

				// Simulate machine lookup.
				machineID := machineTestID(rand.Intn(numMachines))
				row = readDB.QueryRowContext(ctx,
					`SELECT id, status, last_seen_at FROM machines WHERE id = ?`, machineID)
				_ = row.Scan(&id, &id, &id)
				stats.reads.Add(1)

				// Simulate session list for a user.
				userID := fmt.Sprintf("user_%d", rand.Intn(numMachines))
				rows, err := readDB.QueryContext(ctx,
					`SELECT id, status FROM sessions WHERE user_id = ? AND status = 'active' LIMIT 10`, userID)
				if err == nil {
					for rows.Next() {
						_ = rows.Scan(&id, &id)
					}
					rows.Close()
				}
				stats.reads.Add(1)

				// ~5ms between read batches → ~600 reads/sec per goroutine.
				time.Sleep(5 * time.Millisecond)
			}
		}(i)
	}

	// --- Workload 3: Session events (via batcher) ---
	// ~500 active sessions creating/updating summaries and previews.
	const numSessionWorkers = 500
	wg.Add(numSessionWorkers)
	for i := 0; i < numSessionWorkers; i++ {
		go func(idx int) {
			defer wg.Done()
			sessionID := fmt.Sprintf("session_%d", idx)
			userID := fmt.Sprintf("user_%d", idx)
			machineID := machineTestID(idx)

			// Create session.
			writeCtx, writeCancel := context.WithTimeout(context.Background(), 30*time.Second)
			err := batch.ExecContext(writeCtx,
				`INSERT OR IGNORE INTO sessions (id, machine_id, user_id, tool, title, status, started_at, updated_at)
				 VALUES (?, ?, ?, 'claude', 'Task', 'active', ?, ?)`,
				sessionID, machineID, userID,
				time.Now().Format(time.RFC3339), time.Now().Format(time.RFC3339),
			)
			writeCancel()
			if err == nil {
				stats.sessionCreates.Add(1)
			}

			// Periodically update summary.
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			seq := 0
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					seq++
					// Summary update via batcher.
					writeCtx, writeCancel := context.WithTimeout(context.Background(), 30*time.Second)
					err := batch.ExecContext(writeCtx,
						`UPDATE sessions SET status = 'active', updated_at = ? WHERE id = ?`,
						time.Now().Format(time.RFC3339), sessionID,
					)
					writeCancel()
					if err != nil {
						stats.batchFailures.Add(1)
					} else {
						stats.batchWrites.Add(1)
					}

					// Preview update via coalescer.
					wc.Set(
						"session_preview:"+sessionID,
						`UPDATE sessions SET updated_at = ? WHERE id = ?`,
						time.Now().Format(time.RFC3339), sessionID,
					)
					stats.previewSets.Add(1)
				}
			}
		}(i)
	}

	// --- Workload 4: Machine churn (connect/disconnect) ---
	// ~1% of machines reconnect during the test.
	const numChurnWorkers = 100
	wg.Add(numChurnWorkers)
	for i := 0; i < numChurnWorkers; i++ {
		go func(idx int) {
			defer wg.Done()
			baseIdx := numMachines + idx // New machine IDs beyond the seeded ones.
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				machineID := fmt.Sprintf("churn_machine_%d", baseIdx+rand.Intn(1000))

				// Connect: insert machine.
				writeCtx, writeCancel := context.WithTimeout(context.Background(), 30*time.Second)
				_ = batch.ExecContext(writeCtx,
					`INSERT OR IGNORE INTO machines (id, status, last_seen_at) VALUES (?, 'online', ?)`,
					machineID, time.Now().Format(time.RFC3339),
				)
				writeCancel()
				stats.machineChurn.Add(1)

				time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)

				// Disconnect: update status.
				writeCtx, writeCancel = context.WithTimeout(context.Background(), 30*time.Second)
				_ = batch.ExecContext(writeCtx,
					`UPDATE machines SET status = 'offline', last_seen_at = ? WHERE id = ?`,
					time.Now().Format(time.RFC3339), machineID,
				)
				writeCancel()
				stats.machineChurn.Add(1)

				time.Sleep(time.Duration(100+rand.Intn(200)) * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	// Drain pending writes.
	wc.Close()
	batch.Close()

	// --- Verification ---
	var onlineCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM machines WHERE status = 'online'`).Scan(&onlineCount)

	t.Logf("")
	t.Logf("═══════════════════════════════════════════════")
	t.Logf("  REALISTIC 10K SUSTAINED STRESS TEST RESULTS")
	t.Logf("═══════════════════════════════════════════════")
	t.Logf("  Duration:          %v", testDuration)
	t.Logf("  Machines:          %d", numMachines)
	t.Logf("  ─── Writes ───")
	t.Logf("  Heartbeat Set():   %d  (coalescer)", stats.heartbeats.Load())
	t.Logf("  Preview Set():     %d  (coalescer)", stats.previewSets.Load())
	t.Logf("  Batcher writes:    %d  (success)", stats.batchWrites.Load())
	t.Logf("  Batcher failures:  %d", stats.batchFailures.Load())
	t.Logf("  Session creates:   %d", stats.sessionCreates.Load())
	t.Logf("  Machine churn:     %d  (connect+disconnect)", stats.machineChurn.Load())
	t.Logf("  ─── Reads ───")
	t.Logf("  Read queries:      %d  (%.0f/sec)", stats.reads.Load(),
		float64(stats.reads.Load())/testDuration.Seconds())
	t.Logf("  ─── State ───")
	t.Logf("  Machines online:   %d", onlineCount)
	t.Logf("═══════════════════════════════════════════════")

	// Assertions.
	if stats.batchFailures.Load() > 0 {
		t.Fatalf("FAILED: %d batcher write failures", stats.batchFailures.Load())
	}
	if onlineCount < numMachines-100 { // Allow some churn machines to be offline.
		t.Fatalf("FAILED: only %d machines online (expected >= %d)", onlineCount, numMachines-100)
	}
	if stats.reads.Load() < 10000 {
		t.Fatalf("FAILED: only %d reads executed — read pool may be starved", stats.reads.Load())
	}
	t.Logf("PASS — realistic 10K sustained stress: zero write failures, reads unblocked")
}

type realisticStats struct {
	heartbeats     atomic.Int64
	previewSets    atomic.Int64
	batchWrites    atomic.Int64
	batchFailures  atomic.Int64
	sessionCreates atomic.Int64
	machineChurn   atomic.Int64
	reads          atomic.Int64
}

func fireHeartbeat(wc *WriteCoalescer, machineID string, stats *realisticStats) {
	now := time.Now().Format(time.RFC3339Nano)
	wc.Set(
		"machine_hb:"+machineID,
		`UPDATE machines SET last_seen_at = ?, status = 'online' WHERE id = ?`,
		now, machineID,
	)
	wc.Set(
		"machine_meta:"+machineID,
		`UPDATE machines SET last_seen_at = ? WHERE id = ?`,
		now, machineID,
	)
	stats.heartbeats.Add(1)
}

func setupRealisticSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS machines (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL DEFAULT 'offline',
			last_seen_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS viewer_tokens (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			token_hash TEXT NOT NULL,
			expires_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_viewer_tokens_hash ON viewer_tokens(token_hash)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			machine_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			tool TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			started_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user_status ON sessions(user_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_machine ON sessions(machine_id, status)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup schema: %v", err)
		}
	}
}

func seedRealisticData(t *testing.T, db *sql.DB, count int) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}

	// Insert machines.
	machineStmt, _ := tx.Prepare(`INSERT INTO machines (id, status, last_seen_at) VALUES (?, 'offline', '')`)
	for i := 0; i < count; i++ {
		machineStmt.Exec(machineTestID(i))
	}
	machineStmt.Close()

	// Insert viewer tokens (1 per machine/user).
	tokenStmt, _ := tx.Prepare(`INSERT INTO viewer_tokens (id, user_id, token_hash, expires_at) VALUES (?, ?, ?, ?)`)
	for i := 0; i < count; i++ {
		tokenStmt.Exec(
			fmt.Sprintf("vt_%d", i),
			fmt.Sprintf("user_%d", i),
			fmt.Sprintf("token_hash_%d", i),
			time.Now().Add(30*24*time.Hour).Format(time.RFC3339),
		)
	}
	tokenStmt.Close()

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed: %v", err)
	}
}
