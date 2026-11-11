package botfiles

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUploadResumeCompleteAndOwnerScope(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	data := bytes.Repeat([]byte("ab"), 5000)
	m, tok, err := s.Create("t1", "u1", "b1", "Bot", "../../etc/r.pdf", "application/pdf", int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "r.pdf" {
		t.Fatalf("name %q", m.Name)
	}
	if _, err := s.Append(m.ID, "wrong", 0, bytes.NewReader(data)); !errors.Is(err, ErrDenied) {
		t.Fatalf("wrong token: %v", err)
	}
	got, err := s.Append(m.ID, tok, 0, bytes.NewReader(data[:4000]))
	if err != nil || got.Received != 4000 || got.Complete {
		t.Fatalf("first part %+v %v", got, err)
	}
	if _, err := s.Append(m.ID, tok, 0, bytes.NewReader(data)); !errors.Is(err, ErrOffset) {
		t.Fatalf("stale offset: %v", err)
	}
	got, err = s.Append(m.ID, tok, 4000, bytes.NewReader(data[4000:]))
	if err != nil || !got.Complete || got.SHA256 == "" {
		t.Fatalf("complete %+v %v", got, err)
	}
	if _, err := s.Get("t1", "u2", m.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other owner: %v", err)
	}
	if _, err := s.Get("t1", "u1", m.ID); err != nil {
		t.Fatal(err)
	}
}

func TestTooLongAndTooLarge(t *testing.T) {
	s := &Store{Dir: t.TempDir(), MaxFileBytes: 100}
	if _, _, err := s.Create("t", "u", "", "", "a", "", 101); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("cap: %v", err)
	}
	m, tok, _ := s.Create("t", "u", "", "", "a", "", 10)
	if _, err := s.Append(m.ID, tok, 0, strings.NewReader(strings.Repeat("x", 11))); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("long: %v", err)
	}
	m2, _ := s.readMeta(m.ID)
	if m2.Received != 0 {
		t.Fatalf("received %d", m2.Received)
	}
}

func TestSignedPathAndExpiry(t *testing.T) {
	now := time.Now()
	s := &Store{Dir: t.TempDir(), URLTTL: time.Minute, Retention: time.Hour, Now: func() time.Time { return now }}
	m, tok, _ := s.Create("t", "u", "", "", "a.bin", "", 3)
	m, _ = s.Append(m.ID, tok, 0, strings.NewReader("abc"))
	path, _ := s.SignedPath(m)
	q := path[strings.Index(path, "?")+1:]
	exp := strings.TrimPrefix(strings.Split(q, "&")[0], "exp=")
	sig := strings.TrimPrefix(strings.Split(q, "&")[1], "sig=")
	if err := s.CheckSigned(m, exp, sig); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckSigned(m, exp, strings.Repeat("0", 40)); !errors.Is(err, ErrDenied) {
		t.Fatalf("bad sig: %v", err)
	}
	other := m
	other.Owner = "u2"
	if err := s.CheckSigned(other, exp, sig); !errors.Is(err, ErrDenied) {
		t.Fatalf("other owner sig: %v", err)
	}
	now = now.Add(2 * time.Minute)
	if err := s.CheckSigned(m, exp, sig); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired link: %v", err)
	}
	now = now.Add(2 * time.Hour)
	if _, err := s.Get("t", "u", m.ID); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired file: %v", err)
	}
	if n := s.Sweep(); n != 1 {
		t.Fatalf("swept %d", n)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, m.ID)); !os.IsNotExist(err) {
		t.Fatal("file not removed")
	}
}

func TestTotalCapEvictsOldestFinished(t *testing.T) {
	now := time.Now()
	s := &Store{Dir: t.TempDir(), MaxTotalBytes: 25, Now: func() time.Time { return now }}
	a, ta, _ := s.Create("t", "u", "", "", "a", "", 10)
	s.Append(a.ID, ta, 0, strings.NewReader(strings.Repeat("a", 10)))
	now = now.Add(time.Second)
	b, tb, _ := s.Create("t", "u", "", "", "b", "", 10)
	s.Append(b.ID, tb, 0, strings.NewReader(strings.Repeat("b", 10)))
	now = now.Add(time.Second)
	if _, _, err := s.Create("t", "u", "", "", "c", "", 10); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("t", "u", a.ID); err == nil {
		t.Fatal("oldest should be evicted")
	}
	if _, err := s.Get("t", "u", b.ID); err != nil {
		t.Fatal("newer file evicted")
	}
	if _, _, err := s.Create("t", "u", "", "", "d", "", 30); !errors.Is(err, ErrFull) {
		t.Fatalf("full: %v", err)
	}
}
