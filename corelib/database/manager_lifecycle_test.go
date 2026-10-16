package database

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestAcquireProfileConcurrencyQuota(t *testing.T) {
	m := NewManager([]Profile{{ID: "p", Type: SourceExcel, FilePath: "x.xlsx"}}, nil)
	ctx := context.Background()
	releases := make([]func(), 0, 4)
	for i := 0; i < 4; i++ {
		release, err := m.acquireProfile(ctx, "p")
		if err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
		releases = append(releases, release)
	}
	blocked, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := m.acquireProfile(blocked, "p"); err == nil || !strings.Contains(err.Error(), "quota_exceeded") {
		t.Fatalf("expected quota_exceeded on fifth concurrent acquire, got %v", err)
	}
	releases[0]()
	release, err := m.acquireProfile(ctx, "p")
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	release()
	for _, r := range releases[1:] {
		r()
	}
}

func TestResetSessionFailureFailsClosed(t *testing.T) {
	// The postgres dialect issues RESET ALL on checkout. Backed by SQLite that
	// statement is a syntax error, which must fail the whole operation closed
	// before any user SQL runs — the mutation must not execute.
	a := newDryRunTestAdapter(t, Profile{ID: "t", WriteEnabled: true})
	a.dialect = "postgres"
	_, err := a.ExecuteBatch(context.Background(), BatchExecuteRequest{
		Statements: []BatchStatement{
			{SQL: "UPDATE orders SET amount = :amount WHERE id = :id", Params: map[string]interface{}{"amount": 99, "id": 1}},
		},
		DryRun: true,
	})
	if err == nil {
		t.Fatal("expected session reset failure to reject the batch")
	}
	if got := orderAmounts(t, a.db); got[1] != 10 {
		t.Fatalf("mutation executed despite session reset failure: %v", got)
	}
	if _, err := a.Query(context.Background(), QueryRequest{SQL: "SELECT id FROM orders"}); err == nil {
		t.Fatal("expected session reset failure to reject the query")
	}
}

func TestDisconnectInvalidatesCursor(t *testing.T) {
	m := NewManager([]Profile{{ID: "p", Type: SourceExcel, FilePath: "x.xlsx", SchemaVersion: 1}}, nil)
	a := &toolTestAdapter{}
	m.items["conn"] = a
	m.lastUsed["conn"] = time.Now()
	m.bindings["conn"] = connectionBinding{ownerID: "owner", sessionID: "session"}
	m.profileIDs["conn"] = "p"
	result := QueryResult{
		ProfileID: "p",
		Columns:   []Column{{Name: "id"}},
		Rows:      [][]interface{}{{1}},
		allRows:   [][]interface{}{{1}, {2}, {3}},
	}
	token := m.storeResultForConnection(result, "owner", "session", "conn", 1)
	if token == "" {
		t.Fatal("expected a cursor token for the unread rows")
	}
	page, err := m.readResultPageFor(token, "owner", "session", "conn", 1)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if page.RowCount != 1 || !page.Truncated {
		t.Fatalf("unexpected first page: %+v", page)
	}
	if err := m.DisconnectFor("conn", "owner", "session"); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if _, err := m.readResultPageFor(token, "owner", "session", "conn", 1); err == nil {
		t.Fatal("cursor survived disconnect")
	}
	if len(m.results) != 0 {
		t.Fatalf("cursor storage leaked after disconnect: %d entries", len(m.results))
	}
}

func TestCursorExpiresAfterTTL(t *testing.T) {
	m := NewManager(nil, nil)
	result := QueryResult{
		Columns: []Column{{Name: "id"}},
		Rows:    [][]interface{}{{1}},
		allRows: [][]interface{}{{1}, {2}},
	}
	token := m.storeResultAt(result, "owner", "session", 1)
	if token == "" {
		t.Fatal("expected a cursor token")
	}
	m.mu.Lock()
	stored := m.results[token]
	stored.Expires = time.Now().Add(-time.Second)
	m.results[token] = stored
	m.mu.Unlock()
	if _, err := m.readResultPageFor(token, "owner", "session", "", 1); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expiry error, got %v", err)
	}
	if len(m.results) != 0 {
		t.Fatal("expired cursor was not removed")
	}
}
