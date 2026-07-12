package structureddata

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestConcurrency1000Users simulates 1000 concurrent users performing mixed
// read/write operations against a single SQLiteStore instance. This validates
// that the read/write connection pool separation and WAL mode can handle high
// concurrency without database locking errors or data corruption.
//
// Workload per goroutine (simulated user):
//   - 1 write: CreateRecord (INSERT + index rebuild)
//   - 3 reads: QueryRecords (FTS + tag filter + field index join)
//
// Success criteria:
//   - Zero SQLITE_BUSY errors (busy_timeout handles contention)
//   - Zero data corruption (all created records retrievable)
//   - All 1000 writes complete within 60 seconds
//   - Read throughput: >1000 concurrent reads without blocking on writes
func TestConcurrency1000Users(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrency stress test in short mode")
	}

	// Setup: create store with pre-seeded data for realistic query load.
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "concurrency.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_conc", UserID: "admin", Role: "data_admin"}

	// Create dataset with indexed fields.
	ds, err := svc.CreateDataset(context.Background(), p, CreateDatasetInput{
		Domain: "conc", Name: "orders", Title: "Concurrency Test Orders",
	})
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}
	if _, err := svc.UpsertFields(context.Background(), p, ds.ID, UpsertFieldsInput{Fields: []FieldDefinition{
		{Key: "amount", Type: "number", Indexed: true},
		{Key: "customer", Type: "string", Indexed: true},
		{Key: "region", Type: "string", Indexed: true},
	}}); err != nil {
		t.Fatalf("UpsertFields: %v", err)
	}

	// Pre-seed 500 records for reads to have data to query.
	seedRecords := make([]Record, 500)
	base := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	for i := range seedRecords {
		seedRecords[i] = Record{
			ID:        fmt.Sprintf("seed_%04d", i),
			TenantID:  p.TenantID,
			DatasetID: ds.ID,
			Title:     fmt.Sprintf("Seed order %04d renewal contract", i),
			Tags:      []string{"seed", regions[i%len(regions)]},
			Data: map[string]any{
				"amount":   i * 10,
				"customer": fmt.Sprintf("Customer_%03d", i%50),
				"region":   regions[i%len(regions)],
			},
			CreatedBy: p.UserID,
			UpdatedBy: p.UserID,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
			UpdatedAt: base.Add(time.Duration(i) * time.Second),
		}
	}
	if _, err := store.ImportRecords(context.Background(), seedRecords); err != nil {
		t.Fatalf("ImportRecords seed: %v", err)
	}

	// --- Concurrency test: 1000 goroutines ---
	const numUsers = 1000
	const readsPerUser = 3

	var (
		writeErrors  atomic.Int64
		readErrors   atomic.Int64
		writeSuccess atomic.Int64
		readSuccess  atomic.Int64
		busyErrors   atomic.Int64
	)

	var wg sync.WaitGroup
	wg.Add(numUsers)

	start := time.Now()

	for i := 0; i < numUsers; i++ {
		go func(userIdx int) {
			defer wg.Done()
			ctx := context.Background()
			userP := Principal{
				TenantID: p.TenantID,
				UserID:   fmt.Sprintf("user_%04d", userIdx),
				Role:     "data_user",
			}

			// --- Write: create one record per user ---
			recordID := fmt.Sprintf("conc_%04d", userIdx)
			_, err := svc.CreateRecord(ctx, userP, ds.ID, CreateRecordInput{
				ID:    recordID,
				Title: fmt.Sprintf("Concurrent order %04d renewal", userIdx),
				Tags:  []string{"concurrent", regions[userIdx%len(regions)]},
				Data: map[string]any{
					"amount":   userIdx * 5,
					"customer": fmt.Sprintf("ConcCustomer_%03d", userIdx%100),
					"region":   regions[userIdx%len(regions)],
				},
			})
			if err != nil {
				writeErrors.Add(1)
				if isBusyError(err) {
					busyErrors.Add(1)
				}
			} else {
				writeSuccess.Add(1)
			}

			// --- Reads: 3 queries per user (varied patterns) ---
			for r := 0; r < readsPerUser; r++ {
				var queryErr error
				switch r % 3 {
				case 0:
					// FTS query
					_, queryErr = svc.QueryRecords(ctx, userP, ds.ID, QueryRecordsInput{
						Q: "renewal", Limit: 25,
					})
				case 1:
					// Tag + number filter
					_, queryErr = svc.QueryRecords(ctx, userP, ds.ID, QueryRecordsInput{
						Tag:    regions[userIdx%len(regions)],
						Filter: map[string]any{"field": "amount", "op": "gte", "value": userIdx * 2},
						Limit:  25,
					})
				case 2:
					// Sort query
					_, queryErr = svc.QueryRecords(ctx, userP, ds.ID, QueryRecordsInput{
						Sort:  []SortSpec{{Field: "amount", Direction: "desc"}},
						Limit: 50,
					})
				}
				if queryErr != nil {
					readErrors.Add(1)
					if isBusyError(queryErr) {
						busyErrors.Add(1)
					}
				} else {
					readSuccess.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	// --- Report ---
	t.Logf("=== Concurrency Test Results (1000 users) ===")
	t.Logf("Duration:        %v", elapsed)
	t.Logf("Write success:   %d / %d", writeSuccess.Load(), numUsers)
	t.Logf("Write errors:    %d", writeErrors.Load())
	t.Logf("Read success:    %d / %d", readSuccess.Load(), int64(numUsers*readsPerUser))
	t.Logf("Read errors:     %d", readErrors.Load())
	t.Logf("SQLITE_BUSY:     %d", busyErrors.Load())
	t.Logf("Write throughput: %.0f ops/sec", float64(writeSuccess.Load())/elapsed.Seconds())
	t.Logf("Read throughput:  %.0f ops/sec", float64(readSuccess.Load())/elapsed.Seconds())

	// --- Assertions ---
	if elapsed > 60*time.Second {
		t.Errorf("test took too long: %v (max 60s)", elapsed)
	}
	if writeErrors.Load() > 0 {
		t.Errorf("write errors: %d (expected 0)", writeErrors.Load())
	}
	if readErrors.Load() > 0 {
		t.Errorf("read errors: %d (expected 0)", readErrors.Load())
	}
	if busyErrors.Load() > 0 {
		t.Errorf("SQLITE_BUSY errors: %d (expected 0 with busy_timeout=10s)", busyErrors.Load())
	}

	// Verify all records are retrievable (data integrity).
	verifyStart := time.Now()
	var verifyErrors int
	for i := 0; i < numUsers; i++ {
		recordID := fmt.Sprintf("conc_%04d", i)
		_, err := store.GetRecord(context.Background(), p.TenantID, ds.ID, recordID)
		if err != nil {
			verifyErrors++
			if verifyErrors <= 5 {
				t.Logf("verify error record %s: %v", recordID, err)
			}
		}
	}
	t.Logf("Verification:    %d/%d records found (%v)", numUsers-verifyErrors, numUsers, time.Since(verifyStart))
	if verifyErrors > 0 {
		t.Errorf("data integrity: %d records missing after concurrent writes", verifyErrors)
	}
}

// TestConcurrencyReadHeavy simulates a read-heavy workload: 100 writers + 900 readers
// running simultaneously, verifying that reads are not blocked by writes.
func TestConcurrencyReadHeavy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrency stress test in short mode")
	}

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "readheavy.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_rh", UserID: "admin", Role: "data_admin"}

	ds, err := svc.CreateDataset(context.Background(), p, CreateDatasetInput{
		Domain: "rh", Name: "items", Title: "Read Heavy Items",
	})
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}
	if _, err := svc.UpsertFields(context.Background(), p, ds.ID, UpsertFieldsInput{Fields: []FieldDefinition{
		{Key: "score", Type: "number", Indexed: true},
	}}); err != nil {
		t.Fatalf("UpsertFields: %v", err)
	}

	// Seed some data.
	seedRecords := make([]Record, 200)
	base := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	for i := range seedRecords {
		seedRecords[i] = Record{
			ID: fmt.Sprintf("rh_seed_%04d", i), TenantID: p.TenantID, DatasetID: ds.ID,
			Title: fmt.Sprintf("Read heavy item %04d contract renewal", i),
			Tags:  []string{"readheavy"},
			Data:  map[string]any{"score": i * 3},
			CreatedBy: p.UserID, UpdatedBy: p.UserID,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
			UpdatedAt: base.Add(time.Duration(i) * time.Second),
		}
	}
	if _, err := store.ImportRecords(context.Background(), seedRecords); err != nil {
		t.Fatalf("ImportRecords: %v", err)
	}

	const numWriters = 100
	const numReaders = 900
	const readsPerReader = 5

	var (
		writeOK    atomic.Int64
		writeErr   atomic.Int64
		readOK     atomic.Int64
		readErr    atomic.Int64
		readLatSum atomic.Int64 // nanoseconds sum for average calculation
	)

	var wg sync.WaitGroup
	wg.Add(numWriters + numReaders)

	start := time.Now()

	// Writers
	for i := 0; i < numWriters; i++ {
		go func(idx int) {
			defer wg.Done()
			ctx := context.Background()
			wp := Principal{TenantID: p.TenantID, UserID: fmt.Sprintf("writer_%03d", idx), Role: "data_user"}
			_, err := svc.CreateRecord(ctx, wp, ds.ID, CreateRecordInput{
				ID:    fmt.Sprintf("rh_w_%04d", idx),
				Title: fmt.Sprintf("Writer %04d item", idx),
				Tags:  []string{"written"},
				Data:  map[string]any{"score": idx * 7},
			})
			if err != nil {
				writeErr.Add(1)
			} else {
				writeOK.Add(1)
			}
		}(i)
	}

	// Readers
	for i := 0; i < numReaders; i++ {
		go func(idx int) {
			defer wg.Done()
			ctx := context.Background()
			rp := Principal{TenantID: p.TenantID, UserID: fmt.Sprintf("reader_%03d", idx), Role: "data_user"}
			for r := 0; r < readsPerReader; r++ {
				t0 := time.Now()
				_, err := svc.QueryRecords(ctx, rp, ds.ID, QueryRecordsInput{
					Q: "item", Limit: 25,
				})
				lat := time.Since(t0).Nanoseconds()
				readLatSum.Add(lat)
				if err != nil {
					readErr.Add(1)
				} else {
					readOK.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	totalReads := readOK.Load() + readErr.Load()
	avgLatMs := float64(0)
	if totalReads > 0 {
		avgLatMs = float64(readLatSum.Load()) / float64(totalReads) / 1e6
	}

	t.Logf("=== Read-Heavy Concurrency Results ===")
	t.Logf("Duration:          %v", elapsed)
	t.Logf("Writers:           %d OK / %d errors (of %d)", writeOK.Load(), writeErr.Load(), numWriters)
	t.Logf("Readers:           %d OK / %d errors (of %d)", readOK.Load(), readErr.Load(), int64(numReaders*readsPerReader))
	t.Logf("Avg read latency:  %.2f ms", avgLatMs)
	t.Logf("Read throughput:   %.0f ops/sec", float64(readOK.Load())/elapsed.Seconds())
	t.Logf("Write throughput:  %.0f ops/sec", float64(writeOK.Load())/elapsed.Seconds())

	if writeErr.Load() > 0 {
		t.Errorf("write errors: %d", writeErr.Load())
	}
	if readErr.Load() > 0 {
		t.Errorf("read errors: %d", readErr.Load())
	}
	if elapsed > 60*time.Second {
		t.Errorf("test took too long: %v", elapsed)
	}
	// Soft latency budget under concurrent suite load (disk/CPU contention on shared CI).
	// Local idle runs typically measure ~100-200ms avg; 1000ms still fails true regressions
	// while avoiding flake when many packages run in parallel on the same machine.
	if avgLatMs > 1000 {
		t.Errorf("average read latency too high: %.2f ms (max 1000ms)", avgLatMs)
	}
}

// TestConcurrencyWriteContention tests that 1000 concurrent writes don't produce
// SQLITE_BUSY errors thanks to the busy_timeout pragma and single-writer serialization.
func TestConcurrencyWriteContention(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrency stress test in short mode")
	}

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "writecontention.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_wc", UserID: "admin", Role: "data_admin"}

	ds, err := svc.CreateDataset(context.Background(), p, CreateDatasetInput{
		Domain: "wc", Name: "logs", Title: "Write Contention Logs",
	})
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}

	const numWriters = 1000
	var (
		success atomic.Int64
		errors  atomic.Int64
		busy    atomic.Int64
	)

	var wg sync.WaitGroup
	wg.Add(numWriters)

	start := time.Now()

	for i := 0; i < numWriters; i++ {
		go func(idx int) {
			defer wg.Done()
			ctx := context.Background()
			wp := Principal{TenantID: p.TenantID, UserID: fmt.Sprintf("w_%04d", idx), Role: "data_user"}
			_, err := svc.CreateRecord(ctx, wp, ds.ID, CreateRecordInput{
				ID:    fmt.Sprintf("wc_%04d", idx),
				Title: fmt.Sprintf("Write contention record %04d", idx),
				Tags:  []string{"contention"},
				Data:  map[string]any{"seq": idx},
			})
			if err != nil {
				errors.Add(1)
				if isBusyError(err) {
					busy.Add(1)
				}
			} else {
				success.Add(1)
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	t.Logf("=== Write Contention Results (1000 concurrent writes) ===")
	t.Logf("Duration:        %v", elapsed)
	t.Logf("Success:         %d / %d", success.Load(), numWriters)
	t.Logf("Errors:          %d", errors.Load())
	t.Logf("SQLITE_BUSY:     %d", busy.Load())
	t.Logf("Throughput:      %.0f writes/sec", float64(success.Load())/elapsed.Seconds())

	if errors.Load() > 0 {
		t.Errorf("write errors: %d (expected 0)", errors.Load())
	}
	if busy.Load() > 0 {
		t.Errorf("SQLITE_BUSY: %d (busy_timeout should handle contention)", busy.Load())
	}
	if elapsed > 60*time.Second {
		t.Errorf("took too long: %v", elapsed)
	}

	// Verify all records exist.
	for i := 0; i < numWriters; i++ {
		_, err := store.GetRecord(context.Background(), p.TenantID, ds.ID, fmt.Sprintf("wc_%04d", i))
		if err != nil {
			t.Fatalf("missing record wc_%04d after concurrent write: %v", i, err)
		}
	}
}

// --- helpers ---

var regions = []string{"north", "south", "east", "west", "central"}

func isBusyError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return contains(s, "busy") || contains(s, "locked") || contains(s, "SQLITE_BUSY")
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
