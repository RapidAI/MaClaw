// corelib/embedding/modelfetch.go — first-run background download of the
// local embedding model (Qwen3-Embedding-0.6B Q8_0 GGUF).
//
// Contract (Daniel, 2026-10-07): the model is downloaded on first run, never
// bundled. Startup must not block on it: callers Start() the fetcher and keep
// running on L1 rules + remote L3 / BM25 until OnReady fires with a verified
// path, then hot-load. Downloads go to "<file>.part" and resume with HTTP
// Range; size + SHA-256 are verified before the file is renamed into place,
// so a partially written or corrupt file is never handed to the loader.
package embedding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ModelSpec identifies one downloadable model file.
type ModelSpec struct {
	Filename string
	URLs     []string // tried in order on each attempt
	Size     int64    // exact byte size (0 = unchecked)
	SHA256   string   // lowercase hex (""= unchecked)
}

// Qwen3ModelSpec is the shipped embedding model, hosted on the GitHub
// release Model_Release (see release_assets.json; same URL form as the Gemma
// DefaultModelDownloadURL). Callers append the hub mirror
// (<hub>/api/v1/models/<file>) with WithHubFallback. Size / SHA-256 equal the
// Hugging Face LFS values of Qwen/Qwen3-Embedding-0.6B-GGUF.
var Qwen3ModelSpec = mustReleaseAsset(Qwen3ModelFilename)

// ModelURLEnv overrides the download URL list (comma separated).
const ModelURLEnv = "MACLAW_EMBED_MODEL_URL"

// FetchState is the coarse model-availability state exposed to the UI.
type FetchState int

const (
	FetchMissing FetchState = iota
	FetchDownloading
	FetchVerifying
	FetchReady
	FetchFailed
)

func (s FetchState) String() string {
	switch s {
	case FetchMissing:
		return "missing"
	case FetchDownloading:
		return "downloading"
	case FetchVerifying:
		return "verifying"
	case FetchReady:
		return "ready"
	case FetchFailed:
		return "failed"
	}
	return "unknown"
}

// FetchStatus is a snapshot of the fetcher.
type FetchStatus struct {
	State      FetchState
	Path       string
	Downloaded int64
	Total      int64
	Attempt    int
	Err        string
	Source     string // URL the verified file came from ("" = already on disk)
}

// Percent returns 0..100 (or -1 when the total is unknown).
func (s FetchStatus) Percent() int {
	if s.Total <= 0 {
		return -1
	}
	p := int(s.Downloaded * 100 / s.Total)
	if p > 100 {
		p = 100
	}
	return p
}

// String is the user-facing status line ("local model downloading 42%").
func (s FetchStatus) String() string {
	switch s.State {
	case FetchDownloading:
		if p := s.Percent(); p >= 0 {
			return fmt.Sprintf("local model downloading %d%%", p)
		}
		return "local model downloading"
	case FetchVerifying:
		return "local model verifying"
	case FetchReady:
		return "local model ready"
	case FetchFailed:
		return "local model unavailable: " + s.Err
	default:
		return "local model missing"
	}
}

// ErrModelCorrupt is returned when size or SHA-256 do not match the spec.
var ErrModelCorrupt = errors.New("embedding model file corrupt")

// VerifyModelFile checks size then SHA-256. A successful full hash is cached
// in "<path>.verified" (size, mtime, sha) so later starts only stat the file.
func VerifyModelFile(path string, spec ModelSpec) error {
	fi, err := os.Stat(path)
	if err != nil {
		clearVerifyMiss(path)
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not a regular file", ErrModelCorrupt, path)
	}
	if spec.Size > 0 && fi.Size() != spec.Size {
		return fmt.Errorf("%w: %s size %d, want %d", ErrModelCorrupt, path, fi.Size(), spec.Size)
	}
	if spec.SHA256 == "" {
		return nil
	}
	mtime := fi.ModTime().UnixNano()
	marker := verifiedMarker(fi, spec.SHA256)
	if b, err := os.ReadFile(path + ".verified"); err == nil && strings.TrimSpace(string(b)) == marker {
		clearVerifyMiss(path)
		return nil
	}
	// A failed hash is remembered for this path, size, and mtime. Repair
	// checks the pin again before the replacement is visible, and the layout
	// weights are large enough that a second full read is the wrong retry.
	if cached, ok := cachedVerifyMiss(path, fi.Size(), mtime); ok {
		return cached
	}
	atomic.AddInt64(&verifyHashReads, 1)
	sum, err := fileSHA256(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(sum, spec.SHA256) {
		err = fmt.Errorf("%w: %s sha256 %s, want %s", ErrModelCorrupt, path, sum, spec.SHA256)
		storeVerifyMiss(path, fi.Size(), mtime, err)
		return err
	}
	clearVerifyMiss(path)
	_ = os.WriteFile(path+".verified", []byte(marker+"\n"), 0o644)
	return nil
}

var (
	verifyHashReads int64
	verifyMissMu    sync.Mutex
	verifyMissCache = map[string]verifyMiss{}
)

type verifyMiss struct {
	size  int64
	mtime int64
	err   error
}

func cachedVerifyMiss(path string, size, mtime int64) (error, bool) {
	verifyMissMu.Lock()
	defer verifyMissMu.Unlock()
	miss, ok := verifyMissCache[path]
	if !ok || miss.size != size || miss.mtime != mtime {
		return nil, false
	}
	return miss.err, true
}

func storeVerifyMiss(path string, size, mtime int64, err error) {
	verifyMissMu.Lock()
	verifyMissCache[path] = verifyMiss{size: size, mtime: mtime, err: err}
	verifyMissMu.Unlock()
}

func clearVerifyMiss(path string) {
	verifyMissMu.Lock()
	delete(verifyMissCache, path)
	verifyMissMu.Unlock()
}

func verifiedMarker(fi os.FileInfo, sha string) string {
	return fmt.Sprintf("%d %d %s", fi.Size(), fi.ModTime().UnixNano(), strings.ToLower(sha))
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ModelFetcher downloads + verifies one model in the background. All methods
// are safe for concurrent use; Start is single-flight.
type ModelFetcher struct {
	spec   ModelSpec
	dir    string
	Client *http.Client
	// Retries per Ensure call (each retry walks all URLs); Backoff between.
	Retries int
	Backoff time.Duration
	// OnProgress, if set, is called (throttled to ~1%/step) with snapshots.
	OnProgress func(FetchStatus)
	// KeepExisting: an existing non-empty final file is used as-is even if
	// its hash differs (heads recalibrated locally must never be overwritten).
	KeepExisting bool
	// Logf, if set, receives one line per source outcome (which source won).
	Logf func(format string, args ...any)

	mu      sync.Mutex
	status  FetchStatus
	running bool
	ready   []func(string)
	lastPct int
}

// NewModelFetcher targets <dir>/<spec.Filename>. MACLAW_EMBED_MODEL_URL
// replaces the URL list.
func NewModelFetcher(dir string, spec ModelSpec) *ModelFetcher {
	if v := strings.TrimSpace(os.Getenv(ModelURLEnv)); v != "" {
		var urls []string
		for _, u := range strings.Split(v, ",") {
			if u = strings.TrimSpace(u); u != "" {
				urls = append(urls, u)
			}
		}
		spec.URLs = urls
	}
	f := &ModelFetcher{spec: spec, dir: dir, Client: &http.Client{Timeout: 0}, Retries: 3, Backoff: 5 * time.Second, lastPct: -1}
	f.status = FetchStatus{State: FetchMissing, Path: f.Path(), Total: spec.Size}
	return f
}

// Path is the final (verified) model path.
func (f *ModelFetcher) Path() string { return filepath.Join(f.dir, f.spec.Filename) }

// PartPath is the in-progress download path.
func (f *ModelFetcher) PartPath() string { return f.Path() + ".part" }

// Status returns a snapshot.
func (f *ModelFetcher) Status() FetchStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func (f *ModelFetcher) set(mut func(*FetchStatus)) {
	f.mu.Lock()
	mut(&f.status)
	st := f.status
	cb := f.OnProgress
	emit := true
	if st.State == FetchDownloading {
		p := st.Percent()
		emit = p != f.lastPct
		f.lastPct = p
	}
	f.mu.Unlock()
	if cb != nil && emit {
		cb(st)
	}
}

// Start runs Ensure in a goroutine (no-op if already running or ready) and
// calls onReady with the verified path exactly once per successful Ensure.
// It never blocks the caller.
func (f *ModelFetcher) Start(ctx context.Context, onReady func(path string)) {
	f.mu.Lock()
	if onReady != nil {
		if f.status.State == FetchReady {
			p := f.status.Path
			f.mu.Unlock()
			go onReady(p)
			return
		}
		f.ready = append(f.ready, onReady)
	}
	if f.running {
		f.mu.Unlock()
		return
	}
	f.running = true
	f.mu.Unlock()
	go func() {
		p, err := f.Ensure(ctx)
		f.mu.Lock()
		f.running = false
		cbs := f.ready
		if err == nil {
			f.ready = nil
		}
		f.mu.Unlock()
		if err != nil {
			return
		}
		for _, cb := range cbs {
			cb(p)
		}
	}()
}

// Ensure makes the verified model available, downloading if needed. It
// returns the final path. A corrupt final file is moved aside to
// "<file>.corrupt" and re-downloaded; a corrupt .part restarts from zero.
func (f *ModelFetcher) Ensure(ctx context.Context) (string, error) {
	final := f.Path()
	if fi, err := os.Stat(final); err == nil && f.KeepExisting && fi.Mode().IsRegular() && fi.Size() > 0 {
		if verr := VerifyModelFile(final, f.spec); verr != nil && f.Logf != nil {
			f.Logf("[embedding] %s: keeping local file (differs from release: %v)", f.spec.Filename, verr)
		}
		f.set(func(s *FetchStatus) { s.State = FetchReady; s.Downloaded = s.Total })
		return final, nil
	}
	if _, err := os.Stat(final); err == nil {
		f.set(func(s *FetchStatus) { s.State = FetchVerifying; s.Err = "" })
		if err := VerifyModelFile(final, f.spec); err == nil {
			f.set(func(s *FetchStatus) { s.State = FetchReady; s.Downloaded = s.Total })
			return final, nil
		} else if errors.Is(err, ErrModelCorrupt) {
			_ = os.Remove(final + ".corrupt")
			_ = os.Rename(final, final+".corrupt")
			_ = os.Remove(final + ".verified")
		} else {
			f.fail(err)
			return "", err
		}
	}
	if len(f.spec.URLs) == 0 {
		err := errors.New("no download URL for " + f.spec.Filename)
		f.fail(err)
		return "", err
	}
	if err := os.MkdirAll(f.dir, 0o755); err != nil {
		f.fail(err)
		return "", err
	}
	var last error
	for attempt := 1; attempt <= max(1, f.Retries); attempt++ {
		for _, u := range f.spec.URLs {
			if ctx.Err() != nil {
				f.fail(ctx.Err())
				return "", ctx.Err()
			}
			f.set(func(s *FetchStatus) { s.Attempt = attempt; s.Err = "" })
			err := f.downloadOnce(ctx, u)
			if err == nil {
				f.set(func(s *FetchStatus) { s.State = FetchVerifying })
				if verr := VerifyModelFile(f.PartPath(), f.spec); verr != nil {
					if f.Logf != nil {
						f.Logf("[embedding] %s from %s failed verification: %v (trying next source)", f.spec.Filename, u, verr)
					}
					_ = os.Remove(f.PartPath())
					_ = os.Remove(f.PartPath() + ".verified")
					last = verr
					continue
				}
				_ = os.Remove(f.PartPath() + ".verified")
				if err := os.Rename(f.PartPath(), final); err != nil {
					last = err
					continue
				}
				_ = VerifyModelFile(final, f.spec) // writes the marker for the final name
				src := u
				f.set(func(s *FetchStatus) { s.State = FetchReady; s.Downloaded = s.Total; s.Err = ""; s.Source = src })
				if f.Logf != nil {
					f.Logf("[embedding] %s downloaded and verified from %s", f.spec.Filename, src)
				}
				return final, nil
			}
			if f.Logf != nil {
				f.Logf("[embedding] %s: source %s failed: %v (trying next source)", f.spec.Filename, u, err)
			}
			last = err
		}
		if attempt < f.Retries && f.Backoff > 0 {
			select {
			case <-ctx.Done():
				f.fail(ctx.Err())
				return "", ctx.Err()
			case <-time.After(f.Backoff * time.Duration(attempt)):
			}
		}
	}
	f.fail(last)
	return "", last
}

func (f *ModelFetcher) fail(err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	f.set(func(s *FetchStatus) { s.State = FetchFailed; s.Err = msg })
}

// downloadOnce appends to .part using Range when it already has bytes.
func (f *ModelFetcher) downloadOnce(ctx context.Context, url string) error {
	part := f.PartPath()
	var off int64
	if fi, err := os.Stat(part); err == nil {
		off = fi.Size()
		if f.spec.Size > 0 && off > f.spec.Size { // garbage: restart
			_ = os.Remove(part)
			off = 0
		}
	}
	if f.spec.Size > 0 && off == f.spec.Size {
		return nil // complete .part from an earlier run; verify decides
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if off > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", off))
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	flags := os.O_CREATE | os.O_WRONLY
	switch {
	case off > 0 && resp.StatusCode == http.StatusPartialContent:
		if cr := resp.Header.Get("Content-Range"); cr != "" && !strings.HasPrefix(cr, "bytes "+strconv.FormatInt(off, 10)+"-") {
			return fmt.Errorf("unexpected Content-Range %q for offset %d", cr, off)
		}
		flags |= os.O_APPEND
	case resp.StatusCode == http.StatusOK:
		off = 0 // server ignored Range: start over
		flags |= os.O_TRUNC
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		_ = os.Remove(part)
		return fmt.Errorf("range not satisfiable at %d; restarting", off)
	default:
		return fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	total := f.spec.Size
	if total <= 0 && resp.ContentLength > 0 {
		total = off + resp.ContentLength
	}
	out, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return err
	}
	f.set(func(s *FetchStatus) { s.State = FetchDownloading; s.Downloaded = off; s.Total = total })
	buf := make([]byte, 256<<10)
	n := off
	for {
		k, rerr := resp.Body.Read(buf)
		if k > 0 {
			if _, werr := out.Write(buf[:k]); werr != nil {
				out.Close()
				return werr
			}
			n += int64(k)
			if f.spec.Size > 0 && n > f.spec.Size {
				out.Close()
				_ = os.Remove(part)
				return fmt.Errorf("%w: download exceeds %d bytes", ErrModelCorrupt, f.spec.Size)
			}
			cur := n
			f.set(func(s *FetchStatus) { s.Downloaded = cur })
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			return rerr // keep .part for resume
		}
	}
	if err := out.Close(); err != nil {
		return err
	}
	if f.spec.Size > 0 && n != f.spec.Size {
		return fmt.Errorf("short download: %d of %d bytes", n, f.spec.Size)
	}
	return nil
}

// Running reports whether a Start/EnsureShared download is in progress.
func (f *ModelFetcher) Running() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running
}

// EnsureShared is Ensure for a caller that wants to block (e.g. a UI
// "download" button): it waits for an in-flight Start to finish, then runs
// Ensure itself (instant when the verified file is present), so the .part is
// never written by two goroutines. Pending Start callbacks fire on success.
func (f *ModelFetcher) EnsureShared(ctx context.Context) (string, error) {
	for {
		f.mu.Lock()
		if !f.running {
			f.running = true
			f.mu.Unlock()
			break
		}
		f.mu.Unlock()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	p, err := f.Ensure(ctx)
	f.mu.Lock()
	f.running = false
	cbs := f.ready
	if err == nil {
		f.ready = nil
	}
	f.mu.Unlock()
	if err == nil {
		for _, cb := range cbs {
			cb(p)
		}
	}
	return p, err
}

// Forget removes the installed file, its .verified marker and any .part, and
// resets the status to missing (UI "delete model"). It refuses while a
// download is running.
func (f *ModelFetcher) Forget() error {
	f.mu.Lock()
	if f.running {
		f.mu.Unlock()
		return errors.New("download in progress")
	}
	f.mu.Unlock()
	for _, p := range []string{f.Path(), f.Path() + ".verified", f.PartPath(), f.PartPath() + ".verified"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	f.set(func(s *FetchStatus) {
		*s = FetchStatus{State: FetchMissing, Path: f.Path(), Total: f.spec.Size}
	})
	f.mu.Lock()
	f.lastPct = -1
	f.mu.Unlock()
	return nil
}

// Spec returns the fetcher's model spec (file name, URLs, size, sha256).
func (f *ModelFetcher) Spec() ModelSpec { return f.spec }
