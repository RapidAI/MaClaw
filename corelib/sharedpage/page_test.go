package sharedpage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSharedPageCRUDAndVersions(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	n := 0
	s.now = func() time.Time {
		n++
		return base.Add(time.Duration(n) * time.Second)
	}
	s.newID = func() string { return "page-1" }

	p, err := s.Create("Report", "g1", "user")
	if err != nil || p.Version != 1 {
		t.Fatalf("create=%+v err=%v", p, err)
	}
	p, err = s.AppendSection(p.ID, "Intro", "hello", "bot-a")
	if err != nil || p.Version != 2 || len(p.Sections) != 1 {
		t.Fatalf("append=%+v err=%v", p, err)
	}
	secID := p.Sections[0].ID
	p, err = s.EditSection(p.ID, secID, "Intro", "hello world", "bot-b")
	if err != nil || p.Version != 3 || p.Sections[0].Body != "hello world" {
		t.Fatalf("edit=%+v err=%v", p, err)
	}
	if len(p.Versions) != 3 {
		t.Fatalf("versions=%d", len(p.Versions))
	}
	got, err := s.Get(p.ID)
	if err != nil || got.Title != "Report" {
		t.Fatalf("get=%+v err=%v", got, err)
	}
	list := s.List("g1")
	if len(list) != 1 {
		t.Fatalf("list=%d", len(list))
	}
	if _, err := os.Stat(filepath.Join(dir, storeFileName)); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err = s2.Get("page-1")
	if err != nil || got.Version != 3 {
		t.Fatalf("reload=%+v err=%v", got, err)
	}
	if len(ToolSpecs()) < 3 {
		t.Fatal("tool specs missing")
	}
	if err := s2.Delete("page-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Get("page-1"); err == nil {
		t.Fatal("expected missing after delete")
	}
}
