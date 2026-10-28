package agentservice

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Benchmarks against a copy of a real service.db
// (MACLAW_BENCH_DB=/path/copy.db). Only the public Store API is used so the
// same file runs against the previous blob implementation.
func openBenchStore(b *testing.B) (*SQLiteStore, []Run) {
	src := os.Getenv("MACLAW_BENCH_DB")
	if src == "" {
		b.Skip("set MACLAW_BENCH_DB")
	}
	path := filepath.Join(b.TempDir(), "service.db")
	in, err := os.Open(src)
	if err != nil {
		b.Fatal(err)
	}
	out, _ := os.Create(path)
	if _, err := io.Copy(out, in); err != nil {
		b.Fatal(err)
	}
	in.Close()
	out.Close()
	s, err := NewSQLiteStore(path)
	if err != nil {
		b.Fatal(err)
	}
	var runs []Run
	tenants, _ := s.ListTenants()
	for _, t := range tenants {
		users, _ := s.ListUsers(t.ID)
		for _, u := range users {
			insts, _ := s.ListInstances(t.ID, u.ID)
			for _, i := range insts {
				rs, _ := s.ListRuns(t.ID, u.ID, i.ID)
				runs = append(runs, rs...)
			}
		}
	}
	if len(runs) == 0 {
		b.Fatal("no runs in bench db")
	}
	return s, runs
}

func benchRead(b *testing.B, s *SQLiteStore, r Run) {
	if _, err := s.GetRun(r.TenantID, r.UserID, r.InstanceID, r.ID); err != nil {
		b.Fatal(err)
	}
}

func benchWrite(b *testing.B, s *SQLiteStore, r Run) {
	if _, err := s.Append(RunEvent{TenantID: r.TenantID, UserID: r.UserID, RunID: r.ID, Type: "bench.delta", Payload: map[string]any{"text": "x"}}); err != nil {
		b.Fatal(err)
	}
	if err := s.SaveRun(r); err != nil {
		b.Fatal(err)
	}
}

func BenchmarkPapersRead(b *testing.B) {
	s, runs := openBenchStore(b)
	defer s.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchRead(b, s, runs[i%len(runs)])
	}
}

func BenchmarkPapersWrite(b *testing.B) {
	s, runs := openBenchStore(b)
	defer s.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchWrite(b, s, runs[i%len(runs)])
	}
}

// 9 reads : 1 write (event append + run save).
func BenchmarkPapersMixed(b *testing.B) {
	s, runs := openBenchStore(b)
	defer s.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := runs[i%len(runs)]
		if i%10 == 9 {
			benchWrite(b, s, r)
		} else {
			benchRead(b, s, r)
		}
	}
}
