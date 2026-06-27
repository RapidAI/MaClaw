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

// --- Parameterized burst stress tests ---
// These verify correctness under extreme contention at various scales.

func TestStress_WriteCoalescer_10K(t *testing.T)  { stressCoalescer(t, 10000) }
func TestStress_WriteCoalescer_50K(t *testing.T)  { stressCoalescer(t, 50000) }
func TestStress_WriteCoalescer_100K(t *testing.T) { stressCoalescer(t, 100000) }

func TestStress_WriteBatcher_10K(t *testing.T)  { stressBatcher(t, 10000) }
func TestStress_WriteBatcher_50K(t *testing.T)  { stressBatcher(t, 50000) }
func TestStress_WriteBatcher_100K(t *testing.T) { stressBatcher(t, 100000) }

func TestStress_Combined_10K(t *testing.T) { stressCombined(t, 10000, 3*time.Second) }
func TestStress_Combined_50K(t *testing.T) { stressCombined(t, 50000, 3*time.Second) }

func TestStress_WriteBatcher_PoisonPill(t *testing.T) { stressPoisonPill(t) }

func TestStress_WriteCoalescer_ShutdownFlush(t *testing.T) { stressShutdownFlush(t) }

// --- Core implementations ---

func stressCoalescer(t *testing.T, numMachines int) {
	if numMachines > 10000 && testing.Short() {
		t.Skipf("skipping %dK stress in short mode", numMachines/1000)
	}

	db := openStressTestDB(t)
	createMachinesTable(t, db)
	insertMachines(t, db, numMachines)

	wc := NewWriteCoalescer(db, WriteCoalescerConfig{
		FlushIntervalMS: 1000,
		MaxBatchSize:    2048,
	})

	const heartbeatsPerMachine = 3
	var wg sync.WaitGroup
	var totalSets atomic.Int64
	wg.Add(numMachines)

	start := time.Now()
	for i := 0; i < numMachines; i++ {
		go func(idx int) {
			defer wg.Done()
			machineID := machineTestID(idx)
			for h := 0; h < heartbeatsPerMachine; h++ {
				wc.Set("machine_hb:"+machineID,
					`UPDATE machines SET last_seen_at = ?, status = 'online' WHERE id = ?`,
					time.Now().Format(time.RFC3339Nano), machineID)
				totalSets.Add(1)
				if h < heartbeatsPerMachine-1 {
					time.Sleep(time.Duration(rand.Intn(3)) * time.Millisecond)
				}
			}
		}(i)
	}
	wg.Wait()
	submitDur := time.Since(start)
	wc.Close()
	totalDur := time.Since(start)

	var onlineCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM machines WHERE status = 'online'`).Scan(&onlineCount)

	t.Logf("Coalescer %dK: %d Set() in %v, flush total %v, online=%d/%d",
		numMachines/1000, totalSets.Load(), submitDur, totalDur, onlineCount, numMachines)

	if onlineCount != numMachines {
		t.Fatalf("FAILED: expected %d online, got %d", numMachines, onlineCount)
	}
}

func stressBatcher(t *testing.T, numWriters int) {
	if numWriters > 10000 && testing.Short() {
		t.Skipf("skipping %dK stress in short mode", numWriters/1000)
	}

	db := openStressTestDB(t)
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS events (id INTEGER PRIMARY KEY AUTOINCREMENT, src TEXT, ts TEXT)`)

	batch := newWriteBatcher(db, Config{
		BatchFlushMS:   30,
		BatchMaxSize:   512,
		BatchQueueSize: max(numWriters, 4096),
	})

	var wg sync.WaitGroup
	var success, failures atomic.Int64
	wg.Add(numWriters)

	start := time.Now()
	for i := 0; i < numWriters; i++ {
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			if err := batch.ExecContext(ctx, `INSERT INTO events (src, ts) VALUES (?, ?)`,
				fmt.Sprintf("w_%d", idx), time.Now().Format(time.RFC3339Nano)); err != nil {
				failures.Add(1)
			} else {
				success.Add(1)
			}
		}(i)
	}
	wg.Wait()
	batch.Close()
	dur := time.Since(start)

	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&count)

	t.Logf("Batcher %dK: %d success, %d fail, %d rows, %v (%.0f writes/sec)",
		numWriters/1000, success.Load(), failures.Load(), count, dur,
		float64(success.Load())/dur.Seconds())

	if failures.Load() > 0 {
		t.Fatalf("FAILED: %d writes failed", failures.Load())
	}
	if count != numWriters {
		t.Fatalf("FAILED: expected %d rows, got %d", numWriters, count)
	}
}

func stressCombined(t *testing.T, numMachines int, duration time.Duration) {
	if numMachines > 10000 && testing.Short() {
		t.Skipf("skipping %dK combined stress in short mode", numMachines/1000)
	}

	db := openStressTestDB(t)
	createMachinesTable(t, db)
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS session_log (id INTEGER PRIMARY KEY AUTOINCREMENT, sid TEXT, ts TEXT)`)
	insertMachines(t, db, numMachines)

	wc := NewWriteCoalescer(db, WriteCoalescerConfig{FlushIntervalMS: 1000, MaxBatchSize: 1024})
	batch := newWriteBatcher(db, Config{BatchFlushMS: 50, BatchMaxSize: 256, BatchQueueSize: 16384})

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var wg sync.WaitGroup
	var coalesceSets, batchSuccess, batchFail atomic.Int64

	wg.Add(numMachines)
	for i := 0; i < numMachines; i++ {
		go func(idx int) {
			defer wg.Done()
			machineID := machineTestID(idx)
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				wc.Set("machine_hb:"+machineID,
					`UPDATE machines SET last_seen_at = ?, status = 'online' WHERE id = ?`,
					time.Now().Format(time.RFC3339Nano), machineID)
				coalesceSets.Add(1)
				time.Sleep(time.Duration(1+rand.Intn(3)) * time.Millisecond)
			}
		}(i)
	}

	numSessionWriters := numMachines / 5
	wg.Add(numSessionWriters)
	for i := 0; i < numSessionWriters; i++ {
		go func(idx int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				writeCtx, writeCancel := context.WithTimeout(context.Background(), 30*time.Second)
				if err := batch.ExecContext(writeCtx, `INSERT INTO session_log (sid, ts) VALUES (?, ?)`,
					fmt.Sprintf("s_%d", idx), time.Now().Format(time.RFC3339Nano)); err != nil {
					batchFail.Add(1)
				} else {
					batchSuccess.Add(1)
				}
				writeCancel()
				time.Sleep(time.Duration(2+rand.Intn(3)) * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
	wc.Close()
	batch.Close()

	var onlineCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM machines WHERE status = 'online'`).Scan(&onlineCount)
	var rows int
	_ = db.QueryRow(`SELECT COUNT(*) FROM session_log`).Scan(&rows)

	t.Logf("Combined %dK: coalesce=%d, batch=%d (fail=%d), online=%d/%d, rows=%d",
		numMachines/1000, coalesceSets.Load(), batchSuccess.Load(), batchFail.Load(), onlineCount, numMachines, rows)

	if batchFail.Load() > 0 {
		t.Fatalf("FAILED: %d batch failures", batchFail.Load())
	}
	if onlineCount != numMachines {
		t.Fatalf("FAILED: expected %d online, got %d", numMachines, onlineCount)
	}
}

func stressPoisonPill(t *testing.T) {
	db := openStressTestDB(t)
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS strict_table (id TEXT PRIMARY KEY, value TEXT NOT NULL)`)

	batch := newWriteBatcher(db, Config{BatchFlushMS: 50, BatchMaxSize: 64, BatchQueueSize: 1024})
	defer batch.Close()

	const validCount = 100
	const poisonCount = 5

	for i := 0; i < poisonCount; i++ {
		_, _ = db.Exec(`INSERT INTO strict_table (id, value) VALUES (?, ?)`, fmt.Sprintf("poison_%d", i), "existing")
	}

	var wg sync.WaitGroup
	var validOK, validFail, poisonOK, poisonFail atomic.Int64
	wg.Add(validCount + poisonCount)

	for i := 0; i < validCount; i++ {
		go func(idx int) {
			defer wg.Done()
			if err := batch.ExecContext(context.Background(),
				`INSERT INTO strict_table (id, value) VALUES (?, ?)`,
				fmt.Sprintf("valid_%04d", idx), "data"); err != nil {
				validFail.Add(1)
			} else {
				validOK.Add(1)
			}
		}(i)
	}
	for i := 0; i < poisonCount; i++ {
		go func(idx int) {
			defer wg.Done()
			if err := batch.ExecContext(context.Background(),
				`INSERT INTO strict_table (id, value) VALUES (?, ?)`,
				fmt.Sprintf("poison_%d", idx), "dup"); err != nil {
				poisonFail.Add(1)
			} else {
				poisonOK.Add(1)
			}
		}(i)
	}

	wg.Wait()
	batch.Close()

	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM strict_table WHERE id LIKE 'valid_%'`).Scan(&count)

	t.Logf("PoisonPill: valid=%d ok/%d fail, poison=%d ok/%d fail, rows=%d",
		validOK.Load(), validFail.Load(), poisonOK.Load(), poisonFail.Load(), count)

	if validFail.Load() > 0 {
		t.Fatalf("FAILED: %d valid writes lost to collateral damage", validFail.Load())
	}
	if poisonOK.Load() > 0 {
		t.Fatalf("FAILED: %d poison pills succeeded", poisonOK.Load())
	}
	if count != validCount {
		t.Fatalf("FAILED: expected %d rows, got %d", validCount, count)
	}
}

func stressShutdownFlush(t *testing.T) {
	db := openStressTestDB(t)
	createMachinesTable(t, db)
	const n = 2000
	insertMachines(t, db, n)

	wc := NewWriteCoalescer(db, WriteCoalescerConfig{FlushIntervalMS: 60000, MaxBatchSize: 512})

	for i := 0; i < n; i++ {
		wc.Set("machine_hb:"+machineTestID(i),
			`UPDATE machines SET last_seen_at = ?, status = 'online' WHERE id = ?`,
			time.Now().Format(time.RFC3339Nano), machineTestID(i))
	}

	wc.Close()

	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM machines WHERE status = 'online'`).Scan(&count)
	if count != n {
		t.Fatalf("FAILED: shutdown lost data, expected %d got %d", n, count)
	}
	t.Logf("ShutdownFlush: all %d entries persisted", n)
}

// --- Helpers ---

func openStressTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", t.TempDir()+"/stress.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, p := range []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA busy_timeout = 30000;",
		"PRAGMA temp_store = MEMORY;",
		"PRAGMA cache_size = -16384;",
		"PRAGMA wal_autocheckpoint = 2000;",
	} {
		if _, err := db.Exec(p); err != nil {
			t.Fatalf("pragma: %v", err)
		}
	}
	return db
}

func createMachinesTable(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS machines (id TEXT PRIMARY KEY, status TEXT NOT NULL DEFAULT 'offline', last_seen_at TEXT NOT NULL DEFAULT '')`)
	if err != nil {
		t.Fatalf("create machines: %v", err)
	}
}

func insertMachines(t *testing.T, db *sql.DB, count int) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	stmt, _ := tx.Prepare(`INSERT INTO machines (id) VALUES (?)`)
	for i := 0; i < count; i++ {
		stmt.Exec(machineTestID(i))
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func machineTestID(idx int) string {
	return fmt.Sprintf("machine_%d", idx)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
