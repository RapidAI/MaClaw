package structureddata

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestExtremeConcurrency_100kReads_5kWrites simulates an extreme load scenario:
//   - 5,000 concurrent writers (each creating 1 record)
//   - 100,000 concurrent readers (each performing 1 query)
//
// All goroutines start simultaneously via sync.WaitGroup barrier.
// This tests the theoretical limits of SQLite WAL mode with our read/write pool separation.
//
// On a typical machine this should complete within 120 seconds with zero errors.
//
// Opt-in only: launching 105k goroutines is a stress probe, not a unit gate.
// Run with DATASRV_EXTREME_CONCURRENCY=1 when intentionally load-testing SQLite.
func TestExtremeConcurrency_100kReads_5kWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extreme concurrency test in short mode")
	}
	if strings.TrimSpace(os.Getenv("DATASRV_EXTREME_CONCURRENCY")) == "" {
		t.Skip("skipping extreme concurrency stress test; set DATASRV_EXTREME_CONCURRENCY=1 to run")
	}

	// Use high-performance options: 32 read connections, 256 MB cache, 1 GB mmap.
	// This simulates a machine with >=16 cores and >=4 GB RAM.
	store, err := NewSQLiteStoreWithOptions(filepath.Join(t.TempDir(), "extreme.db"), SQLiteStoreOptions{
		MaxReadConns: 32,
		CacheSizeMB:  256,
		MmapSizeMB:   1024,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_ext", UserID: "admin", Role: "data_admin"}

	// Create dataset with indexed fields.
	ds, err := svc.CreateDataset(context.Background(), p, CreateDatasetInput{
		Domain: "ext", Name: "records", Title: "Extreme Concurrency Records",
	})
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}
	if _, err := svc.UpsertFields(context.Background(), p, ds.ID, UpsertFieldsInput{Fields: []FieldDefinition{
		{Key: "amount", Type: "number", Indexed: true},
		{Key: "category", Type: "string", Indexed: true},
	}}); err != nil {
		t.Fatalf("UpsertFields: %v", err)
	}

	// Seed 1000 records so readers always have data to query.
	base := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	seedRecords := make([]Record, 1000)
	for i := range seedRecords {
		seedRecords[i] = Record{
			ID:        fmt.Sprintf("seed_%05d", i),
			TenantID:  p.TenantID,
			DatasetID: ds.ID,
			Title:     fmt.Sprintf("Seed record %05d renewal contract item", i),
			Tags:      []string{"seed", extremeCategories[i%len(extremeCategories)]},
			Data: map[string]any{
				"amount":   i * 7,
				"category": extremeCategories[i%len(extremeCategories)],
			},
			CreatedBy: p.UserID, UpdatedBy: p.UserID,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
			UpdatedAt: base.Add(time.Duration(i) * time.Second),
		}
	}
	if _, err := store.ImportRecords(context.Background(), seedRecords); err != nil {
		t.Fatalf("ImportRecords seed: %v", err)
	}

	const numWriters = 5000
	const numReaders = 100000

	var (
		writeOK     atomic.Int64
		writeErr    atomic.Int64
		readOK      atomic.Int64
		readErr     atomic.Int64
		busyErrors  atomic.Int64
		readLatSum  atomic.Int64 // nanoseconds
		writeLatSum atomic.Int64 // nanoseconds
	)

	total := numWriters + numReaders
	var wg sync.WaitGroup
	wg.Add(total)

	// Barrier: all goroutines wait here until everyone is ready, then fire together.
	var barrier sync.WaitGroup
	barrier.Add(1)

	t.Logf("Launching %d goroutines (%d writers + %d readers)...", total, numWriters, numReaders)
	launchStart := time.Now()

	// --- Launch writers ---
	for i := 0; i < numWriters; i++ {
		go func(idx int) {
			defer wg.Done()
			barrier.Wait() // wait for barrier release

			ctx := context.Background()
			wp := Principal{TenantID: p.TenantID, UserID: fmt.Sprintf("w_%05d", idx), Role: "data_user"}

			t0 := time.Now()
			_, err := svc.CreateRecord(ctx, wp, ds.ID, CreateRecordInput{
				ID:    fmt.Sprintf("ext_w_%05d", idx),
				Title: fmt.Sprintf("Extreme write %05d item", idx),
				Tags:  []string{"extreme", extremeCategories[idx%len(extremeCategories)]},
				Data: map[string]any{
					"amount":   idx * 3,
					"category": extremeCategories[idx%len(extremeCategories)],
				},
			})
			lat := time.Since(t0).Nanoseconds()
			writeLatSum.Add(lat)

			if err != nil {
				writeErr.Add(1)
				if isExtremeBusyError(err) {
					busyErrors.Add(1)
				}
			} else {
				writeOK.Add(1)
			}
		}(i)
	}

	// --- Launch readers ---
	for i := 0; i < numReaders; i++ {
		go func(idx int) {
			defer wg.Done()
			barrier.Wait() // wait for barrier release

			ctx := context.Background()
			rp := Principal{TenantID: p.TenantID, UserID: fmt.Sprintf("r_%06d", idx), Role: "data_user"}

			t0 := time.Now()
			var queryErr error
			switch idx % 4 {
			case 0:
				// FTS search
				_, queryErr = svc.QueryRecords(ctx, rp, ds.ID, QueryRecordsInput{
					Q: "renewal", Limit: 10,
				})
			case 1:
				// Tag filter
				_, queryErr = svc.QueryRecords(ctx, rp, ds.ID, QueryRecordsInput{
					Tag: extremeCategories[idx%len(extremeCategories)], Limit: 10,
				})
			case 2:
				// Number range filter
				_, queryErr = svc.QueryRecords(ctx, rp, ds.ID, QueryRecordsInput{
					Filter: map[string]any{"field": "amount", "op": "gte", "value": idx % 500},
					Limit:  10,
				})
			case 3:
				// Sort + limit
				_, queryErr = svc.QueryRecords(ctx, rp, ds.ID, QueryRecordsInput{
					Sort:  []SortSpec{{Field: "amount", Direction: "desc"}},
					Limit: 10,
				})
			}
			lat := time.Since(t0).Nanoseconds()
			readLatSum.Add(lat)

			if queryErr != nil {
				readErr.Add(1)
				if isExtremeBusyError(queryErr) {
					busyErrors.Add(1)
				}
			} else {
				readOK.Add(1)
			}
		}(i)
	}

	t.Logf("All goroutines launched in %v. Releasing barrier...", time.Since(launchStart))

	// Release all goroutines simultaneously.
	startTime := time.Now()
	barrier.Done()

	// Wait for completion.
	wg.Wait()
	elapsed := time.Since(startTime)

	// --- Calculate stats ---
	totalWrites := writeOK.Load() + writeErr.Load()
	totalReads := readOK.Load() + readErr.Load()
	avgWriteLatMs := float64(0)
	avgReadLatMs := float64(0)
	if totalWrites > 0 {
		avgWriteLatMs = float64(writeLatSum.Load()) / float64(totalWrites) / 1e6
	}
	if totalReads > 0 {
		avgReadLatMs = float64(readLatSum.Load()) / float64(totalReads) / 1e6
	}

	// --- Report ---
	t.Logf("")
	t.Logf("╔══════════════════════════════════════════════════════════════╗")
	t.Logf("║  EXTREME CONCURRENCY TEST: 100K Reads + 5K Writes          ║")
	t.Logf("╠══════════════════════════════════════════════════════════════╣")
	t.Logf("║  Duration:            %-38v ║", elapsed.Round(time.Millisecond))
	t.Logf("║                                                              ║")
	t.Logf("║  WRITES                                                      ║")
	t.Logf("║    Success:           %d / %d %s", writeOK.Load(), numWriters, passOrFail(writeErr.Load() == 0))
	t.Logf("║    Errors:            %d", writeErr.Load())
	t.Logf("║    Throughput:        %.0f ops/sec", float64(writeOK.Load())/elapsed.Seconds())
	t.Logf("║    Avg latency:       %.1f ms", avgWriteLatMs)
	t.Logf("║                                                              ║")
	t.Logf("║  READS                                                       ║")
	t.Logf("║    Success:           %d / %d %s", readOK.Load(), numReaders, passOrFail(readErr.Load() == 0))
	t.Logf("║    Errors:            %d", readErr.Load())
	t.Logf("║    Throughput:        %.0f ops/sec", float64(readOK.Load())/elapsed.Seconds())
	t.Logf("║    Avg latency:       %.1f ms", avgReadLatMs)
	t.Logf("║                                                              ║")
	t.Logf("║  SQLITE_BUSY errors:  %d %s", busyErrors.Load(), passOrFail(busyErrors.Load() == 0))
	t.Logf("╚══════════════════════════════════════════════════════════════╝")

	// --- Assertions ---
	if elapsed > 180*time.Second {
		t.Errorf("TIMEOUT: test took %v (max 180s)", elapsed)
	}
	if writeErr.Load() > 0 {
		t.Errorf("WRITE ERRORS: %d (expected 0)", writeErr.Load())
	}
	if readErr.Load() > 0 {
		t.Errorf("READ ERRORS: %d (expected 0)", readErr.Load())
	}
	if busyErrors.Load() > 0 {
		t.Errorf("SQLITE_BUSY: %d (expected 0)", busyErrors.Load())
	}

	// Spot-check data integrity: verify 100 random records exist.
	verifyCount := 100
	if numWriters < verifyCount {
		verifyCount = numWriters
	}
	var missing int
	for i := 0; i < verifyCount; i++ {
		// Check evenly spaced records across the write range.
		idx := i * (numWriters / verifyCount)
		recordID := fmt.Sprintf("ext_w_%05d", idx)
		if _, err := store.GetRecord(context.Background(), p.TenantID, ds.ID, recordID); err != nil {
			missing++
		}
	}
	if missing > 0 {
		t.Errorf("DATA INTEGRITY: %d/%d spot-check records missing", missing, verifyCount)
	} else {
		t.Logf("Data integrity: %d/%d spot-check records verified", verifyCount, verifyCount)
	}
}

// --- helpers ---

var extremeCategories = []string{"electronics", "clothing", "food", "software", "hardware", "services", "finance", "medical"}

func isExtremeBusyError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return contains(s, "busy") || contains(s, "locked") || contains(s, "SQLITE_BUSY")
}

func passOrFail(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}
