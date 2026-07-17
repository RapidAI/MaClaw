package lansengergroupsummary

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreAppendLoadMark(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	now := time.Now().UTC()

	m1, err := s.Append("g1", "Team", "mid-1", "u1", "Alice", "hello", now)
	if err != nil {
		t.Fatal(err)
	}
	if m1.Seq != 1 {
		t.Fatalf("seq=%d want 1", m1.Seq)
	}
	m2, err := s.Append("g1", "Team", "mid-2", "u2", "Bob", "world", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if m2.Seq != 2 {
		t.Fatalf("seq=%d want 2", m2.Seq)
	}

	newMsgs, st, err := s.LoadNew("g1")
	if err != nil {
		t.Fatal(err)
	}
	if st.LastSummarySeq != 0 || len(newMsgs) != 2 {
		t.Fatalf("new=%d lastSeq=%d", len(newMsgs), st.LastSummarySeq)
	}

	if err := s.MarkSummarized("g1", MaxSeq(newMsgs), time.Now()); err != nil {
		t.Fatal(err)
	}
	newMsgs, st, err = s.LoadNew("g1")
	if err != nil {
		t.Fatal(err)
	}
	if st.LastSummarySeq != 2 || len(newMsgs) != 0 {
		t.Fatalf("after mark: new=%d lastSeq=%d", len(newMsgs), st.LastSummarySeq)
	}

	// New content after cursor.
	if _, err := s.Append("g1", "Team", "mid-3", "u1", "Alice", "again", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	newMsgs, _, err = s.LoadNew("g1")
	if err != nil {
		t.Fatal(err)
	}
	if len(newMsgs) != 1 || newMsgs[0].Text != "again" {
		t.Fatalf("expected only new message, got %#v", newMsgs)
	}

	// Store lives under base/lansenger_group_summary
	if filepath.Base(s.Root()) != StoreDirName {
		t.Fatalf("root=%s", s.Root())
	}
}

func TestStoreIgnoresEmpty(t *testing.T) {
	s := NewStore(t.TempDir())
	m, err := s.Append("g1", "", "", "u1", "A", "  ", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if m.Seq != 0 {
		t.Fatalf("empty text should not allocate seq, got %d", m.Seq)
	}
}

func TestStoreDedupMessageID(t *testing.T) {
	s := NewStore(t.TempDir())
	now := time.Now()
	m1, err := s.Append("g1", "T", "same-id", "u1", "A", "first", now)
	if err != nil {
		t.Fatal(err)
	}
	if m1.Seq != 1 {
		t.Fatalf("seq=%d", m1.Seq)
	}
	m2, err := s.Append("g1", "T", "same-id", "u1", "A", "duplicate redelivery", now)
	if err != nil {
		t.Fatal(err)
	}
	if m2.Seq != 0 {
		t.Fatalf("duplicate should be ignored, got seq=%d", m2.Seq)
	}
	msgs, _, err := s.LoadNew("g1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Text != "first" {
		t.Fatalf("got %#v", msgs)
	}
}

func TestStorePeriodicPruneDoesNotBlockEveryWrite(t *testing.T) {
	s := NewStore(t.TempDir())
	s.PruneEveryNAppends = 1000 // effectively never during this test
	s.MaxMessagesPerGroup = 10
	for i := 0; i < 5; i++ {
		if _, err := s.Append("g1", "T", "", "u", "A", "msg", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	// Still have all 5 until an explicit MarkSummarized prune or counter trip.
	msgs, _, err := s.LoadNew("g1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 5 {
		t.Fatalf("len=%d", len(msgs))
	}
}

func TestStoreLoadNewReclaimsExpiredUnsummarized(t *testing.T) {
	s := NewStore(t.TempDir())
	s.MaxMessageAge = time.Hour
	old := time.Now().UTC().Add(-2 * time.Hour)
	if _, err := s.Append("g1", "T", "e1", "u", "A", "expired-1", old); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append("g1", "T", "e2", "u", "A", "expired-2", old.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	// All messages expired → LoadNew returns empty and advances cursor past them.
	msgs, st, err := s.LoadNew("g1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no live messages, got %d", len(msgs))
	}
	if st.LastSummarySeq < 2 {
		t.Fatalf("expected reclaim LastSummarySeq>=2, got %d", st.LastSummarySeq)
	}
	// Second load stays empty without further growth.
	msgs, st2, err := s.LoadNew("g1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 || st2.LastSummarySeq != st.LastSummarySeq {
		t.Fatalf("second load msgs=%d seq=%d", len(msgs), st2.LastSummarySeq)
	}
}

func TestStoreLoadNewKeepsLiveAfterExpired(t *testing.T) {
	s := NewStore(t.TempDir())
	s.MaxMessageAge = time.Hour
	old := time.Now().UTC().Add(-2 * time.Hour)
	now := time.Now().UTC()
	if _, err := s.Append("g1", "T", "e1", "u", "A", "expired", old); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append("g1", "T", "n1", "u", "B", "fresh", now); err != nil {
		t.Fatal(err)
	}
	msgs, st, err := s.LoadNew("g1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Text != "fresh" {
		t.Fatalf("msgs=%#v", msgs)
	}
	// Expired seq=1 reclaimed; live seq=2 still new (LastSummarySeq should be 1).
	if st.LastSummarySeq != 1 {
		t.Fatalf("LastSummarySeq=%d want 1", st.LastSummarySeq)
	}
}
