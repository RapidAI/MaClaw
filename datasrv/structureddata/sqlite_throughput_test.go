package structureddata

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestThroughputCeiling measures the maximum sustained throughput of the current
// hardware for both reads and writes independently, then combined.
//
// Methodology:
//   - Pure write test: saturate the single writer for 10 seconds, count ops.
//   - Pure read test: saturate the read pool for 10 seconds, count ops.
//   - Mixed test: saturate both simultaneously for 10 seconds.
//
// This gives the actual ceiling on the current machine without goroutine
// queuing distortion (unlike the 100K concurrent test which measures latency
// under extreme queuing, not throughput).
func TestThroughputCeiling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping throughput ceiling test in short mode")
	}

	t.Logf("Hardware: %d CPU cores, GOMAXPROCS=%d", runtime.NumCPU(), runtime.GOMAXPROCS(0))

	// Use optimal settings for this machine.
	numCPU := runtime.NumCPU()
	readConns := numCPU * 2
	if readConns < 4 {
		readConns = 4
	}
	if readConns > 64 {
		readConns = 64
	}

	store, err := NewSQLiteStoreWithOptions(filepath.Join(t.TempDir(), "throughput.db"), SQLiteStoreOptions{
		MaxReadConns: readConns,
		CacheSizeMB:  128,
		MmapSizeMB:   512,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	svc := NewService(store, "sqlite")
	defer svc.Close()
	p := Principal{TenantID: "tenant_tp", UserID: "admin", Role: "data_admin"}

	ds, err := svc.CreateDataset(context.Background(), p, CreateDatasetInput{
		Domain: "tp", Name: "bench", Title: "Throughput Bench",
	})
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}
	if _, err := svc.UpsertFields(context.Background(), p, ds.ID, UpsertFieldsInput{Fields: []FieldDefinition{
		{Key: "value", Type: "number", Indexed: true},
		{Key: "label", Type: "string", Indexed: true},
	}}); err != nil {
		t.Fatalf("UpsertFields: %v", err)
	}

	// Seed data for reads.
	base := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	seedRecords := make([]Record, 2000)
	for i := range seedRecords {
		seedRecords[i] = Record{
			ID: fmt.Sprintf("tp_seed_%05d", i), TenantID: p.TenantID, DatasetID: ds.ID,
			Title: fmt.Sprintf("Throughput seed %05d contract renewal order", i),
			Tags:  []string{"seed", tpLabels[i%len(tpLabels)]},
			Data:  map[string]any{"value": i * 11, "label": tpLabels[i%len(tpLabels)]},
			CreatedBy: p.UserID, UpdatedBy: p.UserID,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
			UpdatedAt: base.Add(time.Duration(i) * time.Second),
		}
	}
	if _, err := store.ImportRecords(context.Background(), seedRecords); err != nil {
		t.Fatalf("ImportRecords: %v", err)
	}

	const testDuration = 10 * time.Second

	// ─── TEST 1: Pure Write Throughput ───
	t.Run("PureWrite", func(t *testing.T) {
		var ops atomic.Int64
		var errors atomic.Int64
		ctx, cancel := context.WithTimeout(context.Background(), testDuration)
		defer cancel()

		// Use numCPU writers to keep the write conn saturated.
		writers := numCPU
		if writers < 4 {
			writers = 4
		}
		var wg sync.WaitGroup
		wg.Add(writers)
		start := time.Now()

		for w := 0; w < writers; w++ {
			go func(workerID int) {
				defer wg.Done()
				for i := 0; ; i++ {
					if ctx.Err() != nil {
						return
					}
					id := fmt.Sprintf("pw_%d_%06d", workerID, i)
					_, err := svc.CreateRecord(ctx, p, ds.ID, CreateRecordInput{
						ID:    id,
						Title: fmt.Sprintf("Pure write %s", id),
						Tags:  []string{"purewrite"},
						Data:  map[string]any{"value": i, "label": "write"},
					})
					if err != nil {
						if ctx.Err() != nil {
							return
						}
						errors.Add(1)
					} else {
						ops.Add(1)
					}
				}
			}(w)
		}

		wg.Wait()
		elapsed := time.Since(start)
		t.Logf("Pure Write: %d ops in %v = %.0f ops/sec (errors: %d, workers: %d)",
			ops.Load(), elapsed.Round(time.Millisecond), float64(ops.Load())/elapsed.Seconds(), errors.Load(), writers)
	})

	// ─── TEST 2: Pure Read Throughput ───
	t.Run("PureRead", func(t *testing.T) {
		var ops atomic.Int64
		var errors atomic.Int64
		ctx, cancel := context.WithTimeout(context.Background(), testDuration)
		defer cancel()

		readers := readConns * 2 // over-subscribe to keep pool saturated
		var wg sync.WaitGroup
		wg.Add(readers)
		start := time.Now()

		for r := 0; r < readers; r++ {
			go func(workerID int) {
				defer wg.Done()
				rp := Principal{TenantID: p.TenantID, UserID: fmt.Sprintf("reader_%d", workerID), Role: "data_user"}
				for i := 0; ; i++ {
					if ctx.Err() != nil {
						return
					}
					var queryErr error
					switch i % 4 {
					case 0:
						_, queryErr = svc.QueryRecords(ctx, rp, ds.ID, QueryRecordsInput{Q: "renewal", Limit: 10})
					case 1:
						_, queryErr = svc.QueryRecords(ctx, rp, ds.ID, QueryRecordsInput{Tag: tpLabels[i%len(tpLabels)], Limit: 10})
					case 2:
						_, queryErr = svc.QueryRecords(ctx, rp, ds.ID, QueryRecordsInput{
							Filter: map[string]any{"field": "value", "op": "gte", "value": i % 1000}, Limit: 10})
					case 3:
						_, queryErr = svc.QueryRecords(ctx, rp, ds.ID, QueryRecordsInput{
							Sort: []SortSpec{{Field: "value", Direction: "desc"}}, Limit: 10})
					}
					if queryErr != nil {
						if ctx.Err() != nil {
							return
						}
						errors.Add(1)
					} else {
						ops.Add(1)
					}
				}
			}(r)
		}

		wg.Wait()
		elapsed := time.Since(start)
		t.Logf("Pure Read:  %d ops in %v = %.0f ops/sec (errors: %d, readers: %d, pool: %d)",
			ops.Load(), elapsed.Round(time.Millisecond), float64(ops.Load())/elapsed.Seconds(), errors.Load(), readers, readConns)
	})

	// ─── TEST 2b: Pure Fast Read (no FTS, cursor pagination only) ───
	t.Run("PureFastRead", func(t *testing.T) {
		var ops atomic.Int64
		var errors atomic.Int64
		ctx, cancel := context.WithTimeout(context.Background(), testDuration)
		defer cancel()

		readers := readConns * 2
		var wg sync.WaitGroup
		wg.Add(readers)
		start := time.Now()

		for r := 0; r < readers; r++ {
			go func(workerID int) {
				defer wg.Done()
				sqlStore := store
				for i := 0; ; i++ {
					if ctx.Err() != nil {
						return
					}
					// Simple cursor-paginated list with optional tag filter — no FTS, no JOINs.
					var queryErr error
					switch i % 2 {
					case 0:
						_, queryErr = sqlStore.QueryRecordsFast(ctx, p.TenantID, ds.ID, QueryRecordsInput{Limit: 25})
					case 1:
						_, queryErr = sqlStore.QueryRecordsFast(ctx, p.TenantID, ds.ID, QueryRecordsInput{
							Tag: tpLabels[i%len(tpLabels)], Limit: 25,
						})
					}
					if queryErr != nil {
						if ctx.Err() != nil {
							return
						}
						errors.Add(1)
					} else {
						ops.Add(1)
					}
				}
			}(r)
		}

		wg.Wait()
		elapsed := time.Since(start)
		t.Logf("Fast Read:  %d ops in %v = %.0f ops/sec (errors: %d, readers: %d, pool: %d)",
			ops.Load(), elapsed.Round(time.Millisecond), float64(ops.Load())/elapsed.Seconds(), errors.Load(), readers, readConns)
	})

	// ─── TEST 3: Mixed Read+Write Throughput ───
	t.Run("Mixed", func(t *testing.T) {
		var writeOps atomic.Int64
		var readOps atomic.Int64
		var writeErrors atomic.Int64
		var readErrors atomic.Int64
		ctx, cancel := context.WithTimeout(context.Background(), testDuration)
		defer cancel()

		writers := numCPU
		if writers < 4 {
			writers = 4
		}
		readers := readConns * 2

		var wg sync.WaitGroup
		wg.Add(writers + readers)
		start := time.Now()

		// Writers
		for w := 0; w < writers; w++ {
			go func(workerID int) {
				defer wg.Done()
				for i := 0; ; i++ {
					if ctx.Err() != nil {
						return
					}
					id := fmt.Sprintf("mx_%d_%06d", workerID, i)
					_, err := svc.CreateRecord(ctx, p, ds.ID, CreateRecordInput{
						ID:    id,
						Title: fmt.Sprintf("Mixed write %s", id),
						Tags:  []string{"mixed"},
						Data:  map[string]any{"value": i, "label": "mixed"},
					})
					if err != nil {
						if ctx.Err() != nil {
							return
						}
						writeErrors.Add(1)
					} else {
						writeOps.Add(1)
					}
				}
			}(w)
		}

		// Readers
		for r := 0; r < readers; r++ {
			go func(workerID int) {
				defer wg.Done()
				rp := Principal{TenantID: p.TenantID, UserID: fmt.Sprintf("mr_%d", workerID), Role: "data_user"}
				for i := 0; ; i++ {
					if ctx.Err() != nil {
						return
					}
					_, err := svc.QueryRecords(ctx, rp, ds.ID, QueryRecordsInput{Q: "renewal", Limit: 10})
					if err != nil {
						if ctx.Err() != nil {
							return
						}
						readErrors.Add(1)
					} else {
						readOps.Add(1)
					}
				}
			}(r)
		}

		wg.Wait()
		elapsed := time.Since(start)
		t.Logf("Mixed Write: %d ops in %v = %.0f ops/sec (errors: %d)",
			writeOps.Load(), elapsed.Round(time.Millisecond), float64(writeOps.Load())/elapsed.Seconds(), writeErrors.Load())
		t.Logf("Mixed Read:  %d ops in %v = %.0f ops/sec (errors: %d)",
			readOps.Load(), elapsed.Round(time.Millisecond), float64(readOps.Load())/elapsed.Seconds(), readErrors.Load())
		t.Logf("Mixed Total: %.0f ops/sec",
			float64(writeOps.Load()+readOps.Load())/elapsed.Seconds())
	})
}

var tpLabels = []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
