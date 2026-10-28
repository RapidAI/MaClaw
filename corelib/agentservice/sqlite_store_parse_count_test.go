package agentservice

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// Reads with no intervening write must not re-parse the state document; a
// write from another handle must still be seen by the next read.
func TestSQLiteStoreReadsReuseParsedStateUntilRowChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.db")
	a, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := a.SaveTenant(Tenant{ID: "t1", Name: "one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.GetTenant("t1"); err != nil {
		t.Fatal(err)
	}
	start := sqliteStateParses.Load()
	for i := 0; i < 200; i++ {
		if _, err := b.GetTenant("t1"); err != nil {
			t.Fatal(err)
		}
		if _, err := a.ListTenants(); err != nil {
			t.Fatal(err)
		}
	}
	if got := sqliteStateParses.Load() - start; got != 0 {
		t.Fatalf("400 quiet reads parsed state %d times, want 0", got)
	}
	if err := a.SaveTenant(Tenant{ID: "t1", Name: "renamed"}); err != nil {
		t.Fatal(err)
	}
	got, err := b.GetTenant("t1")
	if err != nil || got.Name != "renamed" {
		t.Fatalf("other handle missed write: %#v err=%v", got, err)
	}
	if n := sqliteStateParses.Load() - start; n != 1 {
		t.Fatalf("parses after foreign write = %d, want 1", n)
	}
	if _, err := a.GetTenant("t1"); err != nil {
		t.Fatal(err)
	}
	if n := sqliteStateParses.Load() - start; n != 1 {
		t.Fatalf("writer re-parsed its own write: %d", n)
	}
}

func BenchmarkSQLiteStoreQuietRead(b *testing.B) {
	path := filepath.Join(b.TempDir(), "service.db")
	s, err := NewSQLiteStore(path)
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 5000; i++ {
		if err := s.SaveTenant(Tenant{ID: fmt.Sprintf("t%05d", i), Name: fmt.Sprintf("tenant %d with some padding text", i)}); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.GetTenant("t00042"); err != nil {
			b.Fatal(err)
		}
	}
}

// Two commits in the same clock tick with identical length must not leave a
// reader on the first one.
func TestSQLiteStoreSameTickSameLengthWritesAreNotStale(t *testing.T) {
	fixed := time.Date(2026, 10, 28, 9, 0, 0, 123456789, time.UTC)
	old := sqliteNow
	sqliteNow = func() time.Time { return fixed }
	defer func() { sqliteNow = old }()
	path := filepath.Join(t.TempDir(), "service.db")
	a, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	for _, name := range []string{"aaa", "bbb", "ccc"} {
		if err := a.SaveTenant(Tenant{ID: "t1", Name: name}); err != nil {
			t.Fatal(err)
		}
		got, err := b.GetTenant("t1")
		if err != nil || got.Name != name {
			t.Fatalf("reader stale after same-tick write %q: %#v err=%v", name, got, err)
		}
	}
}
