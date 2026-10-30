package tool

import (
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestUsageTrackerDebouncedSaveLands pins the coalescing writer: recording a
// tool outcome must reach the SQLite history without an explicit Save(), and
// keep every record written within one debounce window.
func TestUsageTrackerDebouncedSaveLands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.json")
	tr, err := NewUsageTracker(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"bash", "ssh", "read"} {
		tr.Record(tool, []string{"run"}, true)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		n, err := usageRowCount(path)
		if err == nil && n == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("debounced save never wrote 3 rows (count=%d err=%v)", n, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file left behind: %v", err)
	}

	reloaded, err := NewUsageTracker(path)
	if err != nil {
		t.Fatal(err)
	}
	reloaded.mu.RLock()
	n := len(reloaded.records)
	reloaded.mu.RUnlock()
	if n != 3 {
		t.Fatalf("reloaded %d records, want 3 (coalesced write dropped some)", n)
	}
}

// TestUsageTrackerSaveWaitsForPendingWrite checks that an explicit Save()
// cancels the pending timer and returns after the file is current.
func TestUsageTrackerSaveCancelsPendingWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	tr, err := NewUsageTracker(path)
	if err != nil {
		t.Fatal(err)
	}
	tr.Record("bash", []string{"run"}, true)
	if err := tr.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewUsageTracker(path)
	if err != nil {
		t.Fatal(err)
	}
	reloaded.mu.RLock()
	n := len(reloaded.records)
	reloaded.mu.RUnlock()
	if n != 1 {
		t.Fatalf("Save wrote %d records, want 1", n)
	}
}

// TestUsageTrackerConcurrentRecordAndSave stresses the debounced writer
// against concurrent recorders and explicit saves: every record must survive
// and the reloaded store must match the in-memory count.
func TestUsageTrackerConcurrentRecordAndSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	tr, err := NewUsageTracker(path)
	if err != nil {
		t.Fatal(err)
	}
	const writers = 8
	const perWriter = 25
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				tr.Record("bash", []string{"run", "test"}, i%4 != 0)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			if err := tr.Save(); err != nil {
				t.Errorf("Save: %v", err)
				return
			}
			time.Sleep(3 * time.Millisecond)
		}
	}()
	wg.Wait()
	if err := tr.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewUsageTracker(path)
	if err != nil {
		t.Fatal(err)
	}
	reloaded.mu.RLock()
	n := len(reloaded.records)
	reloaded.mu.RUnlock()
	if n != writers*perWriter {
		t.Fatalf("reloaded %d records, want %d", n, writers*perWriter)
	}
}

func usageRowCount(path string) (int, error) {
	db, err := sql.Open("sqlite", usageDBPath(path)+"?_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var n int
	err = db.QueryRow(`SELECT COUNT(*) FROM usage_records`).Scan(&n)
	return n, err
}
