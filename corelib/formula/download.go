package formula

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

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// ReleaseBaseURL is the GitHub release that hosts FormulaNet, the same
// Model_Release tag as the embedding model. Hosts pass the hub mirror
// (<hub>/api/v1/models/<file>) to EnsureModels; an empty hub URL falls
// back to MACLAW_HUB_URL.
const ReleaseBaseURL = "https://github.com/RapidAI/MaClaw/releases/download/Model_Release"

// On-disk names match the GitHub release assets. The hub cache is flat, so
// the names are prefixed. Open still accepts the older Hugging Face names
// when a directory already has them.
const (
	fileEncoder   = "formulanet-encoder.onnx"
	fileDecoder   = "formulanet-decoder.onnx"
	filePast      = "formulanet-decoder-past.onnx"
	fileTokenizer = "formulanet-tokenizer.json"

	legacyEncoder   = "encoder_model.onnx"
	legacyDecoder   = "decoder_model.onnx"
	legacyPast      = "decoder_with_past_model.onnx"
	legacyTokenizer = "tokenizer.json"
)

const hubURLEnv = "MACLAW_HUB_URL"

type modelAsset struct {
	Filename string
	Legacy   string
	Size     int64
	SHA256   string
}

// Pins match the bytes published on Model_Release (alephpi/FormulaNet ONNX,
// AGPL-3.0). corelib/embedding/release_assets.json carries the same rows so
// the hub download script verifies them.
var modelAssets = []modelAsset{
	{fileEncoder, legacyEncoder, 54168561, "fbd69cf63cf833db1e2ef40013d859b560671c1253278441a01bde4516b624ae"},
	{fileDecoder, legacyDecoder, 25872372, "00f07add62d74c78750cfda60ce8f1cff3d1774f913ca3f412e2e72f4189d7d4"},
	{filePast, legacyPast, 20337816, "cf6838e89f96e33cd81ec8eb885bf78953506e340ff0485aaea13a784af2e804"},
	{fileTokenizer, legacyTokenizer, 17318, "1240f9d178e1ad2a0076fe95ba62e332871c702accdd5ce3ae3ef33ffd6c3a1e"},
}

// ErrCorrupt means a FormulaNet file is present but does not match its pin.
// Missing files are os.ErrNotExist, not ErrCorrupt.
var ErrCorrupt = errors.New("formula model corrupt")

// DownloadReleaseBase is the GitHub release prefix EnsureModels tries before
// the hub mirror. It defaults to ReleaseBaseURL. Tests may replace it.
var DownloadReleaseBase = ReleaseBaseURL

// DownloadBackoff is the pause between EnsureModels attempts. Tests may set it
// to zero. Production keeps the default.
var DownloadBackoff = 5 * time.Second

type fetchOpt struct {
	retries     int
	backoff     time.Duration
	releaseBase string
	client      *http.Client
	// onBytes reports how many bytes of the current file are on disk.
	onBytes func(n int64)
}

func defaultFetchOpt() fetchOpt {
	base := strings.TrimSpace(DownloadReleaseBase)
	if base == "" {
		base = ReleaseBaseURL
	}
	backoff := DownloadBackoff
	if backoff < 0 {
		backoff = 0
	}
	return fetchOpt{retries: 3, backoff: backoff, releaseBase: base, client: &http.Client{}}
}

// DefaultModelDir is <Maclaw base>/models/formula.
func DefaultModelDir() (string, error) {
	base := strings.TrimSpace(maclawpath.BaseDir())
	if base == "" {
		return "", errors.New("formula: maclaw base dir unavailable")
	}
	return filepath.Join(base, "models", "formula"), nil
}

// EnsureModels downloads any missing FormulaNet file into dir.
// An empty dir uses DefaultModelDir. Each file is fetched from the GitHub
// release first. When that source fails or the bytes do not match the pin,
// the hub mirror is tried. A file that already matches size and SHA-256 is
// left in place. Downloads go to "<file>.part" and resume with HTTP Range.
func EnsureModels(ctx context.Context, dir, hubURL string) error {
	return ensureModels(ctx, dir, hubURL, nil)
}

// EnsureModelsProgress is EnsureModels plus a byte callback. total is the
// sum of the pinned file sizes. downloaded counts finished files plus the
// bytes of the file currently being written.
func EnsureModelsProgress(ctx context.Context, dir, hubURL string, progress func(downloaded, total int64)) error {
	return ensureModels(ctx, dir, hubURL, progress)
}

func ensureModels(ctx context.Context, dir, hubURL string, progress func(downloaded, total int64)) error {
	if dir == "" {
		var err error
		dir, err = DefaultModelDir()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var total int64
	for _, a := range modelAssets {
		total += a.Size
	}
	opt := defaultFetchOpt()
	var base int64
	for _, a := range modelAssets {
		if progress != nil {
			assetBase := base
			assetSize := a.Size
			var last time.Time
			opt.onBytes = func(n int64) {
				if n > assetSize {
					n = assetSize
				}
				now := time.Now()
				if n < assetSize && !last.IsZero() && now.Sub(last) < 200*time.Millisecond {
					return
				}
				last = now
				progress(assetBase+n, total)
			}
		} else {
			opt.onBytes = nil
		}
		if err := ensureOne(ctx, dir, hubURL, a, opt); err != nil {
			return err
		}
		base += a.Size
		if progress != nil {
			progress(base, total)
		}
	}
	return nil
}

// FilesPresent reports the combined size when every file Open would load
// has its pinned size. That is the release name when it matches, otherwise
// the legacy Hugging Face name. A short release stub does not hide a
// pin-sized legacy file. This does not hash; VerifyLoaded and EnsureModels do.
func FilesPresent(dir string) (int64, bool) {
	return filesPresent(dir, modelAssets)
}

func filesPresent(dir string, assets []modelAsset) (int64, bool) {
	if strings.TrimSpace(dir) == "" {
		return 0, false
	}
	var sum int64
	for _, a := range assets {
		p := pinnedFile(dir, a)
		fi, err := os.Stat(p)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() != a.Size {
			return 0, false
		}
		sum += fi.Size()
	}
	return sum, len(assets) > 0
}

// modelFile returns the release asset when it is present, otherwise a
// non-empty legacy Hugging Face file in the same directory.
// Open and FilesPresent use pinnedFile so a short stub cannot win.
func modelFile(dir, name, legacy string) string {
	p := filepath.Join(dir, name)
	if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() && st.Size() > 0 {
		return p
	}
	if legacy != "" {
		lp := filepath.Join(dir, legacy)
		if st, err := os.Stat(lp); err == nil && st.Mode().IsRegular() && st.Size() > 0 {
			return lp
		}
	}
	return p
}

// pinnedFile is the path Open loads. A release file of the pinned size
// wins. Otherwise a legacy file of that size wins. Any other existing
// file is returned so the caller can reject it.
func pinnedFile(dir string, a modelAsset) string {
	release := filepath.Join(dir, a.Filename)
	if n, ok := regularSize(release); ok && n == a.Size {
		return release
	}
	if a.Legacy != "" {
		legacy := filepath.Join(dir, a.Legacy)
		if n, ok := regularSize(legacy); ok && n == a.Size {
			return legacy
		}
	}
	if _, ok := regularSize(release); ok {
		return release
	}
	if a.Legacy != "" {
		legacy := filepath.Join(dir, a.Legacy)
		if _, ok := regularSize(legacy); ok {
			return legacy
		}
	}
	return release
}

func regularSize(path string) (int64, bool) {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() <= 0 {
		return 0, false
	}
	return st.Size(), true
}

// VerifyLoaded checks the SHA-256 pin of each file Open would load.
// A matching "<file>.verified" marker skips the hash. A size or hash
// mismatch returns ErrCorrupt. A missing set returns os.ErrNotExist.
// A short release stub beside a verified legacy file is quarantined so
// the next load keeps using the legacy bytes.
func VerifyLoaded(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("%w: empty model dir", os.ErrNotExist)
	}
	for _, a := range modelAssets {
		p := pinnedFile(dir, a)
		if err := verifyFile(p, a); err != nil {
			return err
		}
		release := filepath.Join(dir, a.Filename)
		if p == release {
			continue
		}
		if n, ok := regularSize(release); ok && n != a.Size {
			quarantine(release)
		}
	}
	return nil
}

func assetNamed(name string) (modelAsset, bool) {
	for _, a := range modelAssets {
		if a.Filename == name {
			return a, true
		}
	}
	return modelAsset{}, false
}

func quarantine(path string) {
	_ = os.Remove(path + ".corrupt")
	_ = os.Rename(path, path+".corrupt")
	_ = os.Remove(path + ".verified")
}

func sourceURLs(releaseBase, hubURL, filename string) []string {
	base := strings.TrimRight(strings.TrimSpace(releaseBase), "/")
	urls := []string{base + "/" + filename}
	hubURL = strings.TrimRight(strings.TrimSpace(hubURL), "/")
	if hubURL == "" {
		hubURL = strings.TrimRight(strings.TrimSpace(os.Getenv(hubURLEnv)), "/")
	}
	if hubURL != "" {
		urls = append(urls, hubURL+"/api/v1/models/"+filename)
	}
	return urls
}

func ensureOne(ctx context.Context, dir, hubURL string, a modelAsset, opt fetchOpt) error {
	if opt.client == nil {
		opt.client = &http.Client{}
	}
	if opt.retries < 1 {
		opt.retries = 1
	}
	if opt.releaseBase == "" {
		opt.releaseBase = ReleaseBaseURL
	}
	final := filepath.Join(dir, a.Filename)
	err := verifyFile(final, a)
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrCorrupt) {
		quarantine(final)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// A pin-matching legacy file is enough once the release name is gone or
	// is not the pinned size. A release file that still has the pinned size
	// survived quarantine and has to be replaced.
	if a.Legacy != "" {
		if lerr := verifyFile(filepath.Join(dir, a.Legacy), a); lerr == nil {
			if n, ok := regularSize(final); !ok || n != a.Size {
				return nil
			}
		}
	}
	urls := sourceURLs(opt.releaseBase, hubURL, a.Filename)
	var last error
	for attempt := 1; attempt <= opt.retries; attempt++ {
		for _, u := range urls {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			part := final + ".part"
			err := downloadOnce(ctx, opt.client, u, part, a.Size, opt.onBytes)
			if err != nil {
				last = err
				continue
			}
			if verr := verifyFile(part, a); verr != nil {
				_ = os.Remove(part)
				_ = os.Remove(part + ".verified")
				last = verr
				continue
			}
			_ = os.Remove(part + ".verified")
			if err := os.Remove(final); err != nil && !os.IsNotExist(err) {
				last = err
				continue
			}
			if err := os.Rename(part, final); err != nil {
				last = err
				continue
			}
			if err := verifyFile(final, a); err != nil {
				last = err
				continue
			}
			return nil
		}
		if attempt < opt.retries && opt.backoff > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(opt.backoff * time.Duration(attempt)):
			}
		}
	}
	if last == nil {
		last = errors.New("no download URL")
	}
	return fmt.Errorf("formula: download %s: %w", a.Filename, last)
}

func verifyFile(path string, a modelAsset) error {
	fi, err := os.Stat(path)
	if err != nil {
		clearVerifyMiss(path)
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not a regular file", ErrCorrupt, a.Filename)
	}
	if fi.Size() != a.Size {
		return fmt.Errorf("%w: %s size %d, want %d", ErrCorrupt, a.Filename, fi.Size(), a.Size)
	}
	mtime := fi.ModTime().UnixNano()
	marker := fmt.Sprintf("%d %d %s", fi.Size(), mtime, strings.ToLower(a.SHA256))
	if b, err := os.ReadFile(path + ".verified"); err == nil && strings.TrimSpace(string(b)) == marker {
		clearVerifyMiss(path)
		return nil
	}
	if err, ok := cachedVerifyMiss(path, fi.Size(), mtime); ok {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	atomic.AddInt64(&verifyHashReads, 1)
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(sum, a.SHA256) {
		err := fmt.Errorf("%w: %s sha256 %s, want %s", ErrCorrupt, a.Filename, sum, a.SHA256)
		storeVerifyMiss(path, fi.Size(), mtime, err)
		return err
	}
	clearVerifyMiss(path)
	_ = os.WriteFile(path+".verified", []byte(marker+"\n"), 0o644)
	return nil
}

// A failed hash is remembered for the same path, size, and mtime. Repair
// checks the pin several times in a row, and each check would otherwise
// read the whole file again. A later write changes mtime, so the next
// check hashes the new bytes.
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
	if !ok {
		return nil, false
	}
	if miss.size != size || miss.mtime != mtime {
		delete(verifyMissCache, path)
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

// downloadOnce appends to part using Range when it already has bytes.
// A completed body that is not the pinned size is left for the caller to
// reject; a dropped connection keeps part so the next attempt can resume.
func downloadOnce(ctx context.Context, client *http.Client, url, part string, size int64, onBytes func(int64)) error {
	var off int64
	if fi, err := os.Stat(part); err == nil {
		off = fi.Size()
		if size > 0 && off > size {
			_ = os.Remove(part)
			off = 0
		}
	}
	report := func(n int64) {
		if onBytes != nil {
			onBytes(n)
		}
	}
	if size > 0 && off == size {
		report(off)
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "maclaw-formula/1.0")
	if off > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", off))
	}
	resp, err := client.Do(req)
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
		off = 0
		flags |= os.O_TRUNC
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		_ = os.Remove(part)
		return fmt.Errorf("range not satisfiable at %d", off)
	default:
		return fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	out, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return err
	}
	n := off
	buf := make([]byte, 32<<10)
	var lastReport time.Time
	for {
		k, rerr := resp.Body.Read(buf)
		if k > 0 {
			if _, werr := out.Write(buf[:k]); werr != nil {
				out.Close()
				return werr
			}
			n += int64(k)
			if size > 0 && n > size {
				out.Close()
				_ = os.Remove(part)
				return fmt.Errorf("%w: download exceeds %d bytes", ErrCorrupt, size)
			}
			if onBytes != nil && time.Since(lastReport) >= 200*time.Millisecond {
				onBytes(n)
				lastReport = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			return rerr
		}
	}
	if err := out.Close(); err != nil {
		return err
	}
	if size > 0 && n != size {
		return fmt.Errorf("short download: %d of %d bytes", n, size)
	}
	report(n)
	return nil
}
