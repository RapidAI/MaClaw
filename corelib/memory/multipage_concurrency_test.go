package memory

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Task 16.2: Concurrency and performance verification tests for multi-page
// recall features (cursor pagination, exhaustive recall, scroll sessions).
// ---------------------------------------------------------------------------

// TestConcurrentPaginatedRecalls verifies that concurrent paginated recalls
// across multiple users do not corrupt cursor state. Should be run with
// -race flag to detect data races.
func TestConcurrentPaginatedRecalls(t *testing.T) {
	store := newConcurrencyTestStore(t, 50)

	paginator := store.cursorPaginator
	if paginator == nil {
		t.Skip("CursorPaginator not available on this store")
	}

	const numUsers = 5
	const numPages = 3

	var wg sync.WaitGroup
	errs := make(chan error, numUsers*numPages)

	for u := 0; u < numUsers; u++ {
		wg.Add(1)
		go func(userID string) {
			defer wg.Done()

			// First page.
			result, err := paginator.FirstPage(store, "concurrent test", "", "", userID)
			if err != nil {
				errs <- fmt.Errorf("user %s FirstPage: %w", userID, err)
				return
			}
			if result == nil {
				errs <- fmt.Errorf("user %s FirstPage returned nil", userID)
				return
			}

			seenIDs := make(map[string]bool)
			for _, e := range result.Entries {
				if seenIDs[e.ID] {
					errs <- fmt.Errorf("user %s: duplicate entry in first page: %s", userID, e.ID)
					return
				}
				seenIDs[e.ID] = true
			}

			// Subsequent pages.
			cursor := result.Cursor
			for p := 1; p < numPages && result.HasMore; p++ {
				result, err = paginator.NextPage(cursor)
				if err != nil {
					errs <- fmt.Errorf("user %s page %d: %w", userID, p, err)
					return
				}
				for _, e := range result.Entries {
					if seenIDs[e.ID] {
						errs <- fmt.Errorf("user %s: duplicate entry on page %d: %s", userID, p, e.ID)
						return
					}
					seenIDs[e.ID] = true
				}
				cursor = result.Cursor
			}
		}(fmt.Sprintf("user-%d", u))
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}

// TestConcurrentScrollSessions verifies that concurrent scroll sessions for
// different users/loops do not interfere with each other.
func TestConcurrentScrollSessions(t *testing.T) {
	store := newConcurrencyTestStore(t, 30)

	mgr := NewScrollSessionManager()

	const numLoops = 5
	var wg sync.WaitGroup
	errs := make(chan error, numLoops*10)

	for l := 0; l < numLoops; l++ {
		wg.Add(1)
		go func(loopID, ownerID string) {
			defer wg.Done()

			mgr.GetOrCreate(loopID, store, "concurrent scroll", "", "", ownerID)

			seenIDs := make(map[string]bool)
			for attempt := 0; attempt < 20; attempt++ {
				result, err := mgr.Advance(loopID, perPageTokenBudget, ownerID)
				if err != nil {
					errs <- fmt.Errorf("loop %s advance %d: %w", loopID, attempt, err)
					return
				}
				if result.SessionExhausted {
					break
				}
				for _, e := range result.Entries {
					if seenIDs[e.ID] {
						errs <- fmt.Errorf("loop %s: duplicate entry: %s", loopID, e.ID)
						return
					}
					seenIDs[e.ID] = true
				}
			}
		}(fmt.Sprintf("loop-%d", l), fmt.Sprintf("owner-%d", l))
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}

// TestConcurrentWritesDontBlockCursorReads verifies that store writes
// (Save operations) do not block paginated cursor reads.
func TestConcurrentWritesDontBlockCursorReads(t *testing.T) {
	store := newConcurrencyTestStore(t, 30)

	paginator := store.cursorPaginator
	if paginator == nil {
		t.Skip("CursorPaginator not available on this store")
	}

	// Start paginated read.
	result, err := paginator.FirstPage(store, "concurrent write test", "", "", "reader")
	if err != nil {
		t.Fatalf("FirstPage: %v", err)
	}

	// Concurrent writer.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			_ = store.Save(Entry{
				Content:   fmt.Sprintf("new entry from writer %d at %s", i, time.Now().Format(time.RFC3339Nano)),
				Category:  CategoryProjectKnowledge,
				Tags:      []string{"writer"},
				CreatedAt: time.Now(),
			})
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// Continue reading pages while writer is active.
	cursor := result.Cursor
	for result.HasMore {
		result, err = paginator.NextPage(cursor)
		if err != nil {
			t.Fatalf("NextPage during concurrent writes: %v", err)
		}
		cursor = result.Cursor
	}

	wg.Wait()
}

// TestPaginatedRecallLatency_Small verifies that a single paginated page
// completes within a reasonable time for a small store. This is a sanity
// check, not a P95 benchmark (benchmarks require -bench flag).
func TestPaginatedRecallLatency_Small(t *testing.T) {
	store := newConcurrencyTestStore(t, 100)

	paginator := store.cursorPaginator
	if paginator == nil {
		t.Skip("CursorPaginator not available on this store")
	}

	start := time.Now()
	_, err := paginator.FirstPage(store, "latency test", "", "", "")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("FirstPage: %v", err)
	}

	// Sanity check: should complete in under 2 seconds for 100 entries.
	if elapsed > 2*time.Second {
		t.Errorf("FirstPage took %v for 100 entries (expected < 2s)", elapsed)
	}
}

// TestExhaustiveRecallLatency_Small verifies exhaustive recall latency
// for a moderate store size.
func TestExhaustiveRecallLatency_Small(t *testing.T) {
	store := newConcurrencyTestStore(t, 100)

	start := time.Now()
	result := store.RecallExhaustive("latency exhaustive test", CategoryProjectKnowledge, "")
	elapsed := time.Since(start)

	if result == nil {
		t.Fatal("RecallExhaustive returned nil")
	}

	// Sanity check: should complete in under 3 seconds for 100 entries.
	if elapsed > 3*time.Second {
		t.Errorf("RecallExhaustive took %v for 100 entries (expected < 3s)", elapsed)
	}
}

// newConcurrencyTestStore creates a Store with n entries for concurrency tests.
func newConcurrencyTestStore(t *testing.T, n int) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Stop() })

	for i := 0; i < n; i++ {
		content := fmt.Sprintf("concurrent test entry %d knowledge item with content variation number %d plus extra text to increase token size", i, i*7)
		entry := Entry{
			Content:   content,
			Category:  CategoryProjectKnowledge,
			Tags:      []string{"concurrent", "test"},
			CreatedAt: time.Now().Add(-time.Duration(n-i) * time.Minute),
			UpdatedAt: time.Now().Add(-time.Duration(n-i) * time.Minute),
		}
		_ = store.Save(entry)
	}
	return store
}
