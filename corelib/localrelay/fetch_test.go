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
	"sync"
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
	var events atomic.Int32
	a := &Agent{Home: home, Load: func() Settings { return DefaultSettings(home) }, Fetch: HTTPFetcher(srv.Client(), srv.URL, nil), Notify: func(TransferEvent) { events.Add(1) }}
	path, err := a.SaveDelivered(context.Background(), "b1", "Writer", "", FetchSpec{URL: "/f", Ref: "bf_6", Name: "r.pdf", Size: int64(len(data)), SHA256: shaHex(data)})
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(home, BaseFolder, "Writer", "r.pdf") {
		t.Fatalf("path %s", path)
	}
	if events.Load() != 0 {
		t.Fatalf("a chat delivery reported %d transfers", events.Load())
	}
	if _, err := a.SaveDelivered(context.Background(), "b1", "Writer", "", FetchSpec{URL: "/f", Ref: "bf_bad", Name: "bad.pdf", Size: int64(len(data)), SHA256: strings.Repeat("0", 64)}); err == nil {
		t.Fatal("a mismatched chat file was kept")
	}
	if events.Load() != 0 {
		t.Fatalf("a failed chat delivery reported %d transfers", events.Load())
	}
}

// Two saves of one chat file must share the download. A second copy, or a
// transfer event, is the extra download chip under the composer.
func TestSaveDeliveredSharesOneDownload(t *testing.T) {
	data := []byte("shared-bytes-0123456789")
	var calls atomic.Int32
	release := make(chan struct{})
	entered := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	}))
	defer srv.Close()
	home := t.TempDir()
	var events atomic.Int32
	a := &Agent{Home: home, Load: func() Settings { return DefaultSettings(home) }, Fetch: HTTPFetcher(srv.Client(), srv.URL, nil), Notify: func(TransferEvent) { events.Add(1) }}
	spec := FetchSpec{URL: "/f", Ref: "bf_share", Name: "r.pdf", Size: int64(len(data)), SHA256: shaHex(data)}
	var path1, path2 string
	var err1, err2 error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		path1, err1 = a.SaveDelivered(context.Background(), "b1", "Writer", "", spec)
	}()
	<-entered
	other := &Agent{Home: home, Load: a.Load, Fetch: a.Fetch, Notify: a.Notify}
	renamed := spec
	renamed.Name = "other.pdf"
	go func() {
		defer wg.Done()
		path2, err2 = other.SaveDelivered(context.Background(), "b1", "Writer", "", renamed)
	}()
	key := fetchFlightKey(a.settings().BotFolder("Writer", "b1"), spec)
	deadline := time.Now().Add(2 * time.Second)
	for {
		fetchFlights.Lock()
		flight := fetchFlights.m[key]
		waiting := flight != nil && flight.waiters > 0
		fetchFlights.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second save did not join the download")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() != 1 {
		t.Fatalf("downloads started: %d", calls.Load())
	}
	close(release)
	wg.Wait()
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	if path1 == "" || path1 != path2 {
		t.Fatalf("paths %q %q", path1, path2)
	}
	if calls.Load() != 1 || events.Load() != 0 {
		t.Fatalf("calls %d events %d", calls.Load(), events.Load())
	}
	again, err := a.SaveDelivered(context.Background(), "b1", "Writer", "", spec)
	if err != nil || again != path1 || calls.Load() != 1 || events.Load() != 0 {
		t.Fatalf("again %q err %v calls %d events %d", again, err, calls.Load(), events.Load())
	}
	folder := a.settings().BotFolder("Writer", "b1")
	shown, err := other.fetchInto(context.Background(), folder, "b1", "Writer", "run", "remote.pdf", spec, fetchShown)
	if err != nil || shown != path1 || calls.Load() != 1 || events.Load() != 0 {
		t.Fatalf("shown reuse %q err %v calls %d events %d", shown, err, calls.Load(), events.Load())
	}
	bad := spec
	bad.SHA256 = strings.Repeat("ab", 32)
	if _, err := other.SaveDelivered(context.Background(), "b1", "Writer", "", bad); err == nil || calls.Load() != 1 {
		t.Fatalf("bad checksum err=%v calls=%d", err, calls.Load())
	}
	if _, err := os.Stat(path1); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path1))
	if len(entries) != 1 {
		t.Fatalf("files: %v", entries)
	}
}

func TestReuseKeepsFileWhenAskedSizeDiffers(t *testing.T) {
	data := []byte("size-locked-bytes")
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	}))
	defer srv.Close()
	home := t.TempDir()
	a := &Agent{Home: home, Load: func() Settings { return DefaultSettings(home) }, Fetch: HTTPFetcher(srv.Client(), srv.URL, nil)}
	spec := FetchSpec{URL: "/f", Ref: "bf_size", Name: "r.pdf", Size: int64(len(data)), SHA256: shaHex(data)}
	path, err := a.SaveDelivered(context.Background(), "b1", "Writer", "", spec)
	if err != nil {
		t.Fatal(err)
	}
	wrong := spec
	wrong.Size = int64(len(data) + 10)
	if _, err := a.SaveDelivered(context.Background(), "b1", "Writer", "", wrong); err == nil {
		t.Fatal("a different size reused the saved file")
	}
	again, err := a.SaveDelivered(context.Background(), "b1", "Writer", "", spec)
	if err != nil || again != path || calls.Load() != 1 {
		t.Fatalf("again %q err %v calls %d", again, err, calls.Load())
	}
}

func TestMissingSizeLeavesPartialDownload(t *testing.T) {
	data := []byte("partial-bytes-0123456789")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	}))
	defer srv.Close()
	home := t.TempDir()
	a := &Agent{Home: home, Load: func() Settings { return DefaultSettings(home) }, Fetch: HTTPFetcher(srv.Client(), srv.URL, nil)}
	folder := a.settings().BotFolder("Writer", "b1")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := FetchSpec{URL: "/f", Ref: "bf_part", Name: "a.pdf", Size: int64(len(data))}
	part := filepath.Join(folder, partName(spec))
	if err := os.WriteFile(part, data[:8], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SaveDelivered(context.Background(), "b1", "Writer", "", FetchSpec{URL: spec.URL, Ref: spec.Ref, Name: spec.Name}); err == nil {
		t.Fatal("a sized file was saved without a size")
	}
	got, err := os.ReadFile(part)
	if err != nil || string(got) != string(data[:8]) {
		t.Fatalf("partial = %q err %v", got, err)
	}
}

func TestMissingSizeDoesNotSaveEmptyFile(t *testing.T) {
	data := []byte("not-empty")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	}))
	defer srv.Close()
	home := t.TempDir()
	a := &Agent{Home: home, Load: func() Settings { return DefaultSettings(home) }, Fetch: HTTPFetcher(srv.Client(), srv.URL, nil)}
	if _, err := a.SaveDelivered(context.Background(), "b1", "Writer", "", FetchSpec{URL: "/f", Ref: "bf_nosize", Name: "a.pdf"}); err == nil {
		t.Fatal("a sized file was saved without a size")
	}
	folder := a.settings().BotFolder("Writer", "b1")
	entries, _ := os.ReadDir(folder)
	if len(entries) != 0 {
		t.Fatalf("left files: %v", entries)
	}
	path, err := a.SaveDelivered(context.Background(), "b1", "Writer", "", FetchSpec{URL: "/f", Ref: "bf_nosize", Name: "a.pdf", Size: int64(len(data)), SHA256: shaHex(data)})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(data) {
		t.Fatalf("got %q", got)
	}
}

func TestWaiterResumesWhenLeaderStops(t *testing.T) {
	data := []byte("resumed-bytes")
	var calls atomic.Int32
	entered := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	}))
	defer srv.Close()
	home := t.TempDir()
	a := &Agent{Home: home, Load: func() Settings { return DefaultSettings(home) }, Fetch: HTTPFetcher(srv.Client(), srv.URL, nil)}
	spec := FetchSpec{URL: "/f", Ref: "bf_resume", Name: "r.pdf", Size: int64(len(data)), SHA256: shaHex(data)}
	leaderCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, _ = a.SaveDelivered(leaderCtx, "b1", "Writer", "", spec) }()
	<-entered
	var path string
	var err error
	done := make(chan struct{})
	go func() {
		defer close(done)
		path, err = a.SaveDelivered(context.Background(), "b1", "Writer", "", spec)
	}()
	key := fetchFlightKey(a.settings().BotFolder("Writer", "b1"), spec)
	deadline := time.Now().Add(2 * time.Second)
	for {
		fetchFlights.Lock()
		flight := fetchFlights.m[key]
		waiting := flight != nil && flight.waiters > 0
		fetchFlights.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second save did not join")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("waiter did not finish")
	}
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(data) || calls.Load() < 2 {
		t.Fatalf("path bytes %q calls %d", got, calls.Load())
	}
}
