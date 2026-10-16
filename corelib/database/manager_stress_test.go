package database

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestUpdateProfilesConcurrentStress hammers UpdateProfiles while other
// goroutines read profile state, acquire concurrency slots and create/consume
// cursors. Run with -race: the test passes when nothing panics, no data race
// fires and the manager stays usable after the churn.
func TestUpdateProfilesConcurrentStress(t *testing.T) {
	base := Profile{ID: "p", Type: SourceExcel, FilePath: "x.xlsx"}
	m := NewManager([]Profile{base}, nil)
	defer m.Close()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			version := 1
			for {
				select {
				case <-stop:
					return
				default:
				}
				version++
				changed := base
				changed.SchemaVersion = version + i
				m.UpdateProfiles([]Profile{changed})
			}
		}(i)
	}

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = m.ProfileSummaries()
				release, err := m.acquireProfile(ctx, "p")
				if err == nil {
					release()
				}
				result := QueryResult{Columns: []Column{{Name: "id"}}, Rows: [][]interface{}{{1}}, allRows: [][]interface{}{{1}, {2}}}
				if token := m.storeResultAt(result, "owner", "session", 1); token != "" {
					_, _ = m.readResultPageFor(token, "owner", "session", "", 10)
				}
			}
		}()
	}

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()

	// The manager must still be fully functional after the churn.
	m.UpdateProfiles([]Profile{base})
	if got := m.ProfileSummaries(); len(got) != 1 || got[0].ID != "p" {
		t.Fatalf("manager state corrupted: %+v", got)
	}
	release, err := m.acquireProfile(context.Background(), "p")
	if err != nil {
		t.Fatalf("acquire after stress: %v", err)
	}
	release()
}

// TestCancelledQueryDoesNotLeakProfileSlot verifies that a request whose
// context is already cancelled is rejected at the concurrency gate without
// consuming a slot, so later requests still acquire capacity.
func TestCancelledQueryDoesNotLeakProfileSlot(t *testing.T) {
	m := NewManager([]Profile{{ID: "p", Type: SourceExcel, FilePath: "x.xlsx"}}, nil)
	defer m.Close()
	a := &toolTestAdapter{}
	m.items["conn"] = a
	m.lastUsed["conn"] = time.Now()
	m.bindings["conn"] = connectionBinding{ownerID: "owner", sessionID: "session"}
	m.profileIDs["conn"] = "p"

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled = WithRequestScope(cancelled, RequestScope{OwnerID: "owner", SessionID: "session"})
	for i := 0; i < 8; i++ {
		result := HandleTool(cancelled, m, map[string]interface{}{
			"action": "query", "connection_id": "conn", "sql": "select 1",
		})
		if !strings.Contains(result, "rejected") || !strings.Contains(result, "cancelled") {
			t.Fatalf("cancelled call %d should be rejected at the gate, got %q", i, result)
		}
	}

	// All four slots must still be available.
	releases := make([]func(), 0, 4)
	for i := 0; i < 4; i++ {
		release, err := m.acquireProfile(context.Background(), "p")
		if err != nil {
			t.Fatalf("slot %d leaked by cancelled calls: %v", i, err)
		}
		releases = append(releases, release)
	}
	for _, release := range releases {
		release()
	}
}
