package localrelay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// flakyStore serves one file with Range; the first full request breaks
// after half the bytes, so the client must resume.
type flakyStore struct {
	data     []byte
	breaks   atomic.Int32
	requests atomic.Int32
	ranges   atomic.Int32
	expired  atomic.Bool
}

func (f *flakyStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.requests.Add(1)
	if f.expired.Load() && !strings.Contains(r.URL.RawQuery, "fresh") {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if r.Header.Get("Range") != "" {
		f.ranges.Add(1)
	}
	if r.Header.Get("Range") == "" && f.breaks.Add(-1) >= 0 {
		hj := w.(http.Hijacker)
		conn, buf, _ := hj.Hijack()
		buf.WriteString("HTTP/1.1 200 OK\r\nContent-Length: " + itoa(len(f.data)) + "\r\n\r\n")
		buf.Write(f.data[:len(f.data)/2])
		buf.Flush()
		conn.Close()
		return
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(f.data))
}

func itoa(n int) string { return strconv.Itoa(n) }

func shaHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestFetchResumesAndNeverOverwrites(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789abcdef"), 1<<16) // 1 MiB
	st := &flakyStore{data: data}
	st.breaks.Store(1)
	srv := httptest.NewServer(st)
	defer srv.Close()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "report.pdf"), []byte("mine"), 0o644)
	fetch := HTTPFetcher(srv.Client(), srv.URL, nil)
	var last int64
	path, err := FetchToFolder(context.Background(), fetch, dir, FetchSpec{URL: "/f", Ref: "bf_1", Name: "report.pdf", Size: int64(len(data)), SHA256: shaHex(data)}, func(d int64) { last = d })
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "report (1).pdf" {
		t.Fatalf("saved as %s", path)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, data) || last != int64(len(data)) {
		t.Fatalf("content mismatch or progress %d", last)
	}
	if mine, _ := os.ReadFile(filepath.Join(dir, "report.pdf")); string(mine) != "mine" {
		t.Fatal("existing file was overwritten")
	}
	if st.ranges.Load() == 0 {
		t.Fatal("download did not resume with Range")
	}
	if _, err := os.Stat(filepath.Join(dir, ".maclaw-bf_1.part")); !os.IsNotExist(err) {
		t.Fatal("partial file left behind")
	}
}

func TestFetchChecksumMismatchDiscards(t *testing.T) {
	data := []byte("hello world")
	srv := httptest.NewServer(&flakyStore{data: data})
	defer srv.Close()
	dir := t.TempDir()
	_, err := FetchToFolder(context.Background(), HTTPFetcher(srv.Client(), srv.URL, nil), dir, FetchSpec{URL: "/f", Ref: "bf_2", Name: "a.txt", Size: int64(len(data)), SHA256: strings.Repeat("0", 64)}, nil)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("want checksum error, got %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("left files: %v", entries)
	}
}

func TestFetchRefreshesExpiredLink(t *testing.T) {
	data := []byte("fresh bytes")
	st := &flakyStore{data: data}
	st.expired.Store(true)
	srv := httptest.NewServer(st)
	defer srv.Close()
	dir := t.TempDir()
	spec := FetchSpec{URL: "/f?old", Ref: "bf_3", Name: "a.txt", Size: int64(len(data)), Refresh: func(context.Context) (string, error) { return "/f?fresh", nil }}
	path, err := FetchToFolder(context.Background(), HTTPFetcher(srv.Client(), srv.URL, nil), dir, spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(data) {
		t.Fatal("wrong content")
	}
}

func TestFetchGoneIsTruthful(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusGone) }))
	defer srv.Close()
	_, err := FetchToFolder(context.Background(), HTTPFetcher(srv.Client(), srv.URL, nil), t.TempDir(), FetchSpec{URL: "/f", Ref: "bf_4", Name: "a", Size: 3}, nil)
	if CodeOf(err) != CodeExpired {
		t.Fatalf("got %v", err)
	}
}

func TestBrokerFetchThroughGUI(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 300_000)
	srv := httptest.NewServer(&flakyStore{data: data})
	defer srv.Close()
	r := newRig(t, Settings{}, nil)
	r.agent.Load = func() Settings { return DefaultSettings(r.home) }
	r.agent.Fetch = HTTPFetcher(srv.Client(), srv.URL, nil)
	var events atomic.Int32
	r.agent.Notify = func(TransferEvent) { events.Add(1) }
	resp, err := r.broker.Fetch(context.Background(), r.key, Request{BotID: "b1", BotName: "Bot 4", Name: "big.bin", Size: int64(len(data)), URL: "/f", Ref: "bf_5", SHA256: shaHex(data)}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(r.home, BaseFolder, "Bot 4", "big.bin")
	if resp.Path != want || resp.State != "done" {
		t.Fatalf("resp %+v", resp)
	}
	if got, _ := os.ReadFile(want); !bytes.Equal(got, data) {
		t.Fatal("content")
	}
	if events.Load() < 2 {
		t.Fatal("no transfer events")
	}
	// Turned off: refused before anything is fetched.
	r.agent.Load = func() Settings { s := DefaultSettings(r.home); s.FilesEnabled = false; return s }
	if _, err := r.broker.Fetch(context.Background(), r.key, Request{BotID: "b1", Name: "x", Size: 1, URL: "/f"}, time.Minute); CodeOf(err) != CodeDisabled {
		t.Fatalf("disabled: %v", err)
	}
}

func TestSaveDeliveredUsesBotFolder(t *testing.T) {
	data := []byte("delivered")
	srv := httptest.NewServer(&flakyStore{data: data})
	defer srv.Close()
	home := t.TempDir()
	a := &Agent{Home: home, Load: func() Settings { return DefaultSettings(home) }, Fetch: HTTPFetcher(srv.Client(), srv.URL, nil)}
	path, err := a.SaveDelivered(context.Background(), "b1", "Writer", "", FetchSpec{URL: "/f", Ref: "bf_6", Name: "r.pdf", Size: int64(len(data)), SHA256: shaHex(data)})
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(home, BaseFolder, "Writer", "r.pdf") {
		t.Fatalf("path %s", path)
	}
}
